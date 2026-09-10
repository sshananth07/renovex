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
	collectionIssuanceChains = "rfq_issuance_chains"

	indexNameUniqueIssuanceChain = "uq_rfq_issuance_chains_company_chain"
)

// MongoIssuanceChainRepository is the MongoDB-backed issuance chain store.
type MongoIssuanceChainRepository struct {
	collection *mongo.Collection
}

// NewMongoIssuanceChainRepository constructs the repository over db's
// rfq_issuance_chains collection.
func NewMongoIssuanceChainRepository(db *mongo.Database) *MongoIssuanceChainRepository {
	return &MongoIssuanceChainRepository{
		collection: db.Collection(collectionIssuanceChains),
	}
}

// EnsureIndexes creates the §11.2 uniqueness invariant. It is idempotent.
func (r *MongoIssuanceChainRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqChainId", Value: 1}},
		// Unique because a SECOND chain for one RFQ would defeat the entire
		// serialization design: two issuances could each allocate "version 1"
		// from their own counter and both believe they won.
		Options: options.Index().SetUnique(true).SetName(indexNameUniqueIssuanceChain),
	})
	return err
}

type issuanceChainDoc struct {
	ID                     bson.ObjectID `bson:"_id,omitempty"`
	CompanyID              string        `bson:"companyId"`
	RFQChainID             string        `bson:"rfqChainId"`
	LatestIssuedVersion    int           `bson:"latestIssuedVersion"`
	CurrentIssuedVersionID *string       `bson:"currentIssuedVersionId,omitempty"`
	Revision               int64         `bson:"revision"`
	CreatedAt              time.Time     `bson:"createdAt"`
	UpdatedAt              time.Time     `bson:"updatedAt"`
	SchemaVersion          int           `bson:"schemaVersion"`
}

func fromChainDoc(doc issuanceChainDoc) RFQIssuanceChain {
	return RFQIssuanceChain{
		ID:                     doc.ID.Hex(),
		CompanyID:              doc.CompanyID,
		RFQChainID:             doc.RFQChainID,
		LatestIssuedVersion:    doc.LatestIssuedVersion,
		CurrentIssuedVersionID: doc.CurrentIssuedVersionID,
		Revision:               doc.Revision,
		CreatedAt:              doc.CreatedAt,
		UpdatedAt:              doc.UpdatedAt,
		SchemaVersion:          doc.SchemaVersion,
	}
}

// EnsureChain returns the chain for companyID + rfqChainID, creating it if it
// does not exist.
//
// Implemented as an upsert with $setOnInsert rather than find-then-insert:
// there is no read-then-write window, so two concurrent first issuances
// converge on ONE chain instead of racing to create two. The unique index is
// the backstop if a driver-level retry still produced a duplicate.
// DeleteAllForCompany permanently removes every IssuanceChain owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoIssuanceChainRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoIssuanceChainRepository) EnsureChain(ctx context.Context,
	companyID, rfqChainID string) (RFQIssuanceChain, error) {

	now := time.Now()
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var doc issuanceChainDoc
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{"companyId": companyID, "rfqChainId": rfqChainID},
		bson.M{"$setOnInsert": bson.M{
			"companyId":           companyID,
			"rfqChainId":          rfqChainID,
			"latestIssuedVersion": 0,
			"revision":            int64(0),
			"createdAt":           now,
			"updatedAt":           now,
			"schemaVersion":       RFQIssuanceChainSchemaVersion,
		}},
		opts,
	).Decode(&doc)
	if err != nil {
		return RFQIssuanceChain{}, err
	}
	return fromChainDoc(doc), nil
}

// AdvanceChain points the chain at a newly created immutable version, under a
// revision guard.
//
// The guard is what makes this a serialization point: of N concurrent callers
// holding the same expected revision, exactly one matches and the rest are
// refused, so a stale caller can never overwrite a newer issuance's pointer.
//
// A 0-match is disambiguated by re-reading: a missing or foreign chain is
// reported as not-found (404) rather than as a conflict, so a foreign tenant
// cannot learn the chain exists.
func (r *MongoIssuanceChainRepository) AdvanceChain(ctx context.Context,
	companyID, rfqChainID string, expectedRevision int64,
	versionNumber int, issuedVersionID string) (RFQIssuanceChain, error) {

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var doc issuanceChainDoc
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"companyId":  companyID,
			"rfqChainId": rfqChainID,
			"revision":   expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"currentIssuedVersionId": issuedVersionID,
				"revision":               expectedRevision + 1,
				"updatedAt":              time.Now(),
			},
			// $max, not $set: a pointer repair must never move the recorded
			// version number backwards if another transition already advanced
			// it. Immutable-version uniqueness remains the allocation authority.
			"$max": bson.M{"latestIssuedVersion": versionNumber},
		},
		opts,
	).Decode(&doc)

	if errors.Is(err, mongo.ErrNoDocuments) {
		current, findErr := r.FindChain(ctx, companyID, rfqChainID)
		if findErr != nil {
			return RFQIssuanceChain{}, findErr
		}
		_ = current
		return RFQIssuanceChain{}, ErrRevisionMismatch
	}
	if err != nil {
		return RFQIssuanceChain{}, err
	}
	return fromChainDoc(doc), nil
}

// FindChain reads one chain, tenant-scoped.
func (r *MongoIssuanceChainRepository) FindChain(ctx context.Context,
	companyID, rfqChainID string) (RFQIssuanceChain, error) {

	var doc issuanceChainDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "rfqChainId": rfqChainID,
	}).Decode(&doc)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return RFQIssuanceChain{}, ErrIssuanceChainNotFound
	}
	if err != nil {
		return RFQIssuanceChain{}, err
	}
	return fromChainDoc(doc), nil
}
