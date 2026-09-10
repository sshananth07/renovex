package supplieroffers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionSupplierOfferChains = "supplier_offer_chains"

	indexNameUniqueSupplierOfferChain = "uq_supplier_offer_chains_company_invitation_issued_version"
)

// MongoOfferChainRepository stores the submission-version serialization
// aggregate without giving callers direct access to its MongoDB document.
type MongoOfferChainRepository struct {
	collection *mongo.Collection
}

func NewMongoOfferChainRepository(db *mongo.Database) *MongoOfferChainRepository {
	return &MongoOfferChainRepository{
		collection: db.Collection(collectionSupplierOfferChains),
	}
}

// EnsureIndexes makes the full tenant-scoped commercial identity unique. All
// three fields matter: one invitation can receive multiple issued RFQ
// versions, and identifier values may repeat in another company.
func (repository *MongoOfferChainRepository) EnsureIndexes(ctx context.Context) error {
	_, err := repository.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "companyId", Value: 1},
			{Key: "invitationId", Value: 1},
			{Key: "issuedRFQVersionId", Value: 1},
		},
		Options: options.Index().
			SetName(indexNameUniqueSupplierOfferChain).
			SetUnique(true),
	})
	return err
}

type supplierOfferChainDocument struct {
	ID                     bson.ObjectID `bson:"_id,omitempty"`
	CompanyID              string        `bson:"companyId"`
	InvitationID           string        `bson:"invitationId"`
	IssuedRFQVersionID     string        `bson:"issuedRFQVersionId"`
	LatestSubmittedVersion int           `bson:"latestSubmittedVersion"`
	LatestSubmittedID      *string       `bson:"latestSubmittedId,omitempty"`
	Revision               int64         `bson:"revision"`
	CreatedAt              time.Time     `bson:"createdAt"`
	UpdatedAt              time.Time     `bson:"updatedAt"`
	SchemaVersion          int           `bson:"schemaVersion"`
}

func supplierOfferChainFromDocument(
	document supplierOfferChainDocument,
) SupplierOfferChain {
	return SupplierOfferChain{
		ID:                     document.ID.Hex(),
		CompanyID:              document.CompanyID,
		InvitationID:           document.InvitationID,
		IssuedRFQVersionID:     document.IssuedRFQVersionID,
		LatestSubmittedVersion: document.LatestSubmittedVersion,
		LatestSubmittedID:      document.LatestSubmittedID,
		Revision:               document.Revision,
		CreatedAt:              document.CreatedAt,
		UpdatedAt:              document.UpdatedAt,
		SchemaVersion:          document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every SupplierOfferChain owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoOfferChainRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureOfferChain uses one upsert rather than a create-then-read sequence.
// Concurrent first access therefore converges on the document protected by
// the unique index instead of creating competing version counters.
func (repository *MongoOfferChainRepository) EnsureOfferChain(
	ctx context.Context,
	companyID string,
	invitationID string,
	issuedRFQVersionID string,
) (SupplierOfferChain, error) {
	now := time.Now().UTC()
	options := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var document supplierOfferChainDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":          companyID,
			"invitationId":       invitationID,
			"issuedRFQVersionId": issuedRFQVersionID,
		},
		bson.M{
			"$setOnInsert": bson.M{
				"companyId":              companyID,
				"invitationId":           invitationID,
				"issuedRFQVersionId":     issuedRFQVersionID,
				"latestSubmittedVersion": 0,
				"revision":               int64(0),
				"createdAt":              now,
				"updatedAt":              now,
				"schemaVersion":          SupplierOfferChainSchemaVersion,
			},
		},
		options,
	).Decode(&document)
	if err != nil {
		return SupplierOfferChain{}, err
	}
	return supplierOfferChainFromDocument(document), nil
}

// AdvanceChainToVersion points the chain at a newly submitted version.
//
// The CAS requires the expected revision AND that the new number strictly
// exceeds the current one, so a delayed retry can never roll the chain
// backward onto an older version. Advancing to the number already recorded is
// reported as an idempotent no-op rather than an error, because recovery
// re-drives this step after a lost response.
func (repository *MongoOfferChainRepository) AdvanceChainToVersion(
	ctx context.Context,
	companyID string,
	chainID string,
	expectedRevision int64,
	versionID string,
	versionNumber int,
	updatedAt time.Time,
) (SupplierOfferChain, error) {
	objectID, idErr := bson.ObjectIDFromHex(chainID)
	if idErr != nil {
		return SupplierOfferChain{}, ErrOfferChainNotFound
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var document supplierOfferChainDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":                    objectID,
			"companyId":              companyID,
			"revision":               expectedRevision,
			"latestSubmittedVersion": bson.M{"$lt": versionNumber},
		},
		bson.M{
			"$set": bson.M{
				"latestSubmittedVersion": versionNumber,
				"latestSubmittedId":      versionID,
				"updatedAt":              updatedAt,
				"revision":               expectedRevision + 1,
			},
		},
		opts,
	).Decode(&document)
	if err == nil {
		return supplierOfferChainFromDocument(document), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferChain{}, err
	}

	// Classify the miss: an already-advanced chain is the idempotent path.
	var current supplierOfferChainDocument
	if findErr := repository.collection.FindOne(ctx, bson.M{
		"_id": objectID, "companyId": companyID,
	}).Decode(&current); findErr != nil {
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return SupplierOfferChain{}, ErrOfferChainNotFound
		}
		return SupplierOfferChain{}, findErr
	}
	if current.LatestSubmittedID != nil && *current.LatestSubmittedID == versionID {
		return supplierOfferChainFromDocument(current), nil
	}
	return SupplierOfferChain{}, ErrOfferDraftConflict
}

// FindChain reads a chain by its identity, company-scoped.
func (repository *MongoOfferChainRepository) FindChain(
	ctx context.Context,
	companyID string,
	chainID string,
) (SupplierOfferChain, bool, error) {
	objectID, idErr := bson.ObjectIDFromHex(chainID)
	if idErr != nil {
		return SupplierOfferChain{}, false, nil
	}
	var document supplierOfferChainDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"_id": objectID, "companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferChain{}, false, nil
	}
	if err != nil {
		return SupplierOfferChain{}, false, err
	}
	return supplierOfferChainFromDocument(document), true, nil
}

// ListChainsForInvitation returns every offer chain a company holds for one
// invitation, regardless of which issued RFQ version each chain answers. The
// M8.1 copy-forward source resolver uses this to search backward across
// earlier issued RFQ versions of the same invitation without reaching into
// another module's collection.
func (repository *MongoOfferChainRepository) ListChainsForInvitation(
	ctx context.Context,
	companyID string,
	invitationID string,
) ([]SupplierOfferChain, error) {
	cursor, err := repository.collection.Find(ctx, bson.M{
		"companyId": companyID, "invitationId": invitationID,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	chains := make([]SupplierOfferChain, 0)
	for cursor.Next(ctx) {
		var document supplierOfferChainDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		chains = append(chains, supplierOfferChainFromDocument(document))
	}
	return chains, cursor.Err()
}

// FindChainByScope resolves the one authoritative tenant/invitation/issued-RFQ
// tuple without upserting during a read. G5 history and G7 reconciliation share
// this indexed locator while retaining chain-ID operations internally.
func (repository *MongoOfferChainRepository) FindChainByScope(
	ctx context.Context,
	companyID string,
	invitationID string,
	issuedRFQVersionID string,
) (SupplierOfferChain, bool, error) {
	var document supplierOfferChainDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "invitationId": invitationID,
		"issuedRFQVersionId": issuedRFQVersionID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferChain{}, false, nil
	}
	if err != nil {
		return SupplierOfferChain{}, false, err
	}
	return supplierOfferChainFromDocument(document), true, nil
}
