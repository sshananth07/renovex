package rfqissuance

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionAmendmentDrafts = "rfq_amendment_drafts"

	indexNameUniqueAmendmentDraft = "uq_rfq_amendment_drafts_company_chain"
)

// MongoAmendmentDraftRepository is the MongoDB-backed amendment draft store.
//
// This is the ONLY mutable collection in the module. Everything else is
// append-only immutable versions.
type MongoAmendmentDraftRepository struct {
	collection *mongo.Collection
}

// NewMongoAmendmentDraftRepository constructs the repository over db's
// rfq_amendment_drafts collection.
func NewMongoAmendmentDraftRepository(db *mongo.Database) *MongoAmendmentDraftRepository {
	return &MongoAmendmentDraftRepository{
		collection: db.Collection(collectionAmendmentDrafts),
	}
}

// EnsureIndexes creates the §11.2 one-draft-per-chain invariant. Idempotent.
func (r *MongoAmendmentDraftRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqChainId", Value: 1}},
		// Unique: two drafts would let two contractors prepare divergent
		// "next versions" of one chain, and only one could ever be issued.
		Options: options.Index().SetUnique(true).SetName(indexNameUniqueAmendmentDraft),
	})
	return err
}

type amendmentDraftDoc struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	CompanyID  string        `bson:"companyId"`
	RFQChainID string        `bson:"rfqChainId"`

	BaseIssuedVersionID string `bson:"baseIssuedVersionId"`
	BaseVersionNumber   int    `bson:"baseVersionNumber"`

	Currency             string     `bson:"currency"`
	Title                string     `bson:"title,omitempty"`
	DeliveryAddress      string     `bson:"deliveryAddress,omitempty"`
	RequiredByDate       *time.Time `bson:"requiredByDate,omitempty"`
	ResponseDeadline     *time.Time `bson:"responseDeadline,omitempty"`
	SupplierInstructions string     `bson:"supplierInstructions,omitempty"`

	Lines []issuedLineDoc `bson:"lines,omitempty"`

	Revision        int64     `bson:"revision"`
	CreatedByUserID string    `bson:"createdByUserId,omitempty"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	SchemaVersion   int       `bson:"schemaVersion"`
}

func toAmendmentDraftDoc(d RFQAmendmentDraft) amendmentDraftDoc {
	return amendmentDraftDoc{
		CompanyID: d.CompanyID, RFQChainID: d.RFQChainID,
		BaseIssuedVersionID: d.BaseIssuedVersionID,
		BaseVersionNumber:   d.BaseVersionNumber,
		Currency:            d.Currency, Title: d.Title,
		DeliveryAddress: d.DeliveryAddress, RequiredByDate: d.RequiredByDate,
		ResponseDeadline:     d.ResponseDeadline,
		SupplierInstructions: d.SupplierInstructions,
		Lines:                toIssuedLineDocs(d.Lines),
		Revision:             d.Revision,
		CreatedByUserID:      d.CreatedByUserID,
		CreatedAt:            d.CreatedAt, UpdatedAt: d.UpdatedAt,
		SchemaVersion: d.SchemaVersion,
	}
}

func fromAmendmentDraftDoc(doc amendmentDraftDoc) (RFQAmendmentDraft, error) {
	lines, err := fromIssuedLineDocs(doc.Lines)
	if err != nil {
		return RFQAmendmentDraft{}, err
	}

	return RFQAmendmentDraft{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, RFQChainID: doc.RFQChainID,
		BaseIssuedVersionID: doc.BaseIssuedVersionID,
		BaseVersionNumber:   doc.BaseVersionNumber,
		Currency:            doc.Currency, Title: doc.Title,
		DeliveryAddress: doc.DeliveryAddress, RequiredByDate: doc.RequiredByDate,
		ResponseDeadline:     doc.ResponseDeadline,
		SupplierInstructions: doc.SupplierInstructions,
		Lines:                lines,
		Revision:             doc.Revision,
		CreatedByUserID:      doc.CreatedByUserID,
		CreatedAt:            doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
		SchemaVersion: doc.SchemaVersion,
	}, nil
}

// CreateDraft persists the chain's single amendment draft.
// DeleteAllForCompany permanently removes every RFQAmendmentDraft owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoAmendmentDraftRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoAmendmentDraftRepository) CreateDraft(ctx context.Context,
	draft RFQAmendmentDraft) (RFQAmendmentDraft, error) {

	res, err := r.collection.InsertOne(ctx, toAmendmentDraftDoc(draft))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// The only unique index on this collection is the one-draft
			// invariant, so this classification is unambiguous.
			return RFQAmendmentDraft{}, ErrAmendmentDraftAlreadyExists
		}
		return RFQAmendmentDraft{}, err
	}
	draft.ID = res.InsertedID.(bson.ObjectID).Hex()
	return draft, nil
}

// FindDraft reads the chain's draft, tenant-scoped.
func (r *MongoAmendmentDraftRepository) FindDraft(ctx context.Context,
	companyID, rfqChainID string) (RFQAmendmentDraft, error) {

	var doc amendmentDraftDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "rfqChainId": rfqChainID,
	}).Decode(&doc)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return RFQAmendmentDraft{}, ErrAmendmentDraftNotFound
	}
	if err != nil {
		return RFQAmendmentDraft{}, err
	}
	return fromAmendmentDraftDoc(doc)
}

// UpdateDraft applies an edit under a revision guard.
//
// A 0-match is disambiguated by re-reading, so a missing or foreign draft is a
// 404 while a genuine concurrent edit is a 409 — the pattern established by
// quotations and rfqs.
func (r *MongoAmendmentDraftRepository) UpdateDraft(ctx context.Context,
	companyID, rfqChainID string, expectedRevision int64,
	updated RFQAmendmentDraft) (RFQAmendmentDraft, error) {

	doc := toAmendmentDraftDoc(updated)
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var result amendmentDraftDoc
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"companyId": companyID, "rfqChainId": rfqChainID,
			"revision": expectedRevision,
		},
		bson.M{"$set": bson.M{
			"title":                doc.Title,
			"deliveryAddress":      doc.DeliveryAddress,
			"requiredByDate":       doc.RequiredByDate,
			"responseDeadline":     doc.ResponseDeadline,
			"supplierInstructions": doc.SupplierInstructions,
			"lines":                doc.Lines,
			"revision":             expectedRevision + 1,
			"updatedAt":            time.Now(),
		}},
		opts,
	).Decode(&result)

	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, findErr := r.FindDraft(ctx, companyID, rfqChainID); errors.Is(findErr,
			ErrAmendmentDraftNotFound) {
			return RFQAmendmentDraft{}, ErrAmendmentDraftNotFound
		}
		return RFQAmendmentDraft{}, ErrRevisionMismatch
	}
	if err != nil {
		return RFQAmendmentDraft{}, err
	}
	return fromAmendmentDraftDoc(result)
}

// DeleteDraft removes the draft under a revision guard, leaving every issued
// version untouched.
func (r *MongoAmendmentDraftRepository) DeleteDraft(ctx context.Context,
	companyID, rfqChainID string, expectedRevision int64) error {

	res, err := r.collection.DeleteOne(ctx, bson.M{
		"companyId": companyID, "rfqChainId": rfqChainID, "revision": expectedRevision,
	})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		if _, findErr := r.FindDraft(ctx, companyID, rfqChainID); errors.Is(findErr,
			ErrAmendmentDraftNotFound) {
			return ErrAmendmentDraftNotFound
		}
		return ErrRevisionMismatch
	}
	return nil
}
