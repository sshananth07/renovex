package awards

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionAwardOutcomeAcknowledgements = "award_outcome_acknowledgements"

	indexNameUniqueAwardAcknowledgement = "uq_award_outcome_acks_company_outcome"
)

type MongoAwardAcknowledgementRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardAcknowledgementRepository(
	db *mongo.Database,
) *MongoAwardAcknowledgementRepository {
	return &MongoAwardAcknowledgementRepository{
		collection: db.Collection(collectionAwardOutcomeAcknowledgements),
	}
}

// EnsureIndexes enforces ONE acknowledgement per outcome.
//
// The key is deliberately the outcome alone, not outcome + recipient: a second
// person from the same Supplier acknowledging does not create a second receipt.
// The first receipt is the true one.
func (repository *MongoAwardAcknowledgementRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "companyId", Value: 1},
			{Key: "awardOutcomeId", Value: 1},
		},
		Options: options.Index().
			SetName(indexNameUniqueAwardAcknowledgement).
			SetUnique(true),
	})
	return err
}

type awardAcknowledgementDocument struct {
	ID                bson.ObjectID `bson:"_id,omitempty"`
	CompanyID         string        `bson:"companyId"`
	AwardOutcomeID    string        `bson:"awardOutcomeId"`
	SupplierID        string        `bson:"supplierId"`
	InvitationID      string        `bson:"invitationId"`
	SessionID         string        `bson:"sessionId"`
	RecipientIdentity string        `bson:"recipientIdentity"`
	OperationID       string        `bson:"operationId"`
	AcknowledgedAt    time.Time     `bson:"acknowledgedAt"`
	SchemaVersion     int           `bson:"schemaVersion"`
}

func acknowledgementFromDocument(
	document awardAcknowledgementDocument,
) AwardOutcomeAcknowledgement {
	return AwardOutcomeAcknowledgement{
		ID:                document.ID.Hex(),
		CompanyID:         document.CompanyID,
		AwardOutcomeID:    document.AwardOutcomeID,
		SupplierID:        document.SupplierID,
		InvitationID:      document.InvitationID,
		SessionID:         document.SessionID,
		RecipientIdentity: document.RecipientIdentity,
		OperationID:       document.OperationID,
		AcknowledgedAt:    document.AcknowledgedAt,
		SchemaVersion:     document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every AwardOutcomeAcknowledgement
// owned by companyID. Never errors when zero documents match.
// Development-tool use only.
func (repository *MongoAwardAcknowledgementRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureAcknowledgement records the first receipt and reports whether it
// created one.
//
// The FIRST receipt is the true one: a same-operation retry returns it
// unchanged, and a later attempt by a different identity also returns it
// unchanged. The original identity and time are NEVER overwritten — a second
// attempt does not rewrite history.
func (repository *MongoAwardAcknowledgementRepository) EnsureAcknowledgement(
	ctx context.Context,
	candidate AwardOutcomeAcknowledgement,
) (AwardOutcomeAcknowledgement, bool, error) {
	if candidate.AcknowledgedAt.IsZero() {
		candidate.AcknowledgedAt = time.Now().UTC()
	}
	if err := candidate.Validate(); err != nil {
		return AwardOutcomeAcknowledgement{}, false, err
	}

	document := awardAcknowledgementDocument{
		CompanyID:         candidate.CompanyID,
		AwardOutcomeID:    candidate.AwardOutcomeID,
		SupplierID:        candidate.SupplierID,
		InvitationID:      candidate.InvitationID,
		SessionID:         candidate.SessionID,
		RecipientIdentity: candidate.RecipientIdentity,
		OperationID:       candidate.OperationID,
		AcknowledgedAt:    candidate.AcknowledgedAt,
		SchemaVersion:     AwardOutcomeAcknowledgementSchemaVersion,
	}

	result, err := repository.collection.InsertOne(ctx, document)
	if err == nil {
		document.ID = result.InsertedID.(bson.ObjectID)
		// Read back rather than returning the in-memory candidate: MongoDB
		// stores timestamps at millisecond precision, so the stored receipt
		// time differs from the one submitted. The STORED value is the
		// authoritative record of when the Supplier acknowledged, and a caller
		// comparing the two must not see them disagree.
		stored, found, findErr := repository.FindAcknowledgement(
			ctx, candidate.CompanyID, candidate.AwardOutcomeID)
		if findErr != nil {
			return AwardOutcomeAcknowledgement{}, false, findErr
		}
		if found {
			return stored, true, nil
		}
		return acknowledgementFromDocument(document), true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return AwardOutcomeAcknowledgement{}, false, err
	}

	existing, found, findErr := repository.FindAcknowledgement(
		ctx, candidate.CompanyID, candidate.AwardOutcomeID)
	if findErr != nil {
		return AwardOutcomeAcknowledgement{}, false, findErr
	}
	if !found {
		return AwardOutcomeAcknowledgement{}, false, ErrAwardOutcomeNotFound
	}
	return existing, false, nil
}

// FindAcknowledgement resolves an outcome's receipt within one tenant.
func (repository *MongoAwardAcknowledgementRepository) FindAcknowledgement(
	ctx context.Context,
	companyID, awardOutcomeID string,
) (AwardOutcomeAcknowledgement, bool, error) {
	var document awardAcknowledgementDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":      companyID,
		"awardOutcomeId": awardOutcomeID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardOutcomeAcknowledgement{}, false, nil
	}
	if err != nil {
		return AwardOutcomeAcknowledgement{}, false, err
	}
	return acknowledgementFromDocument(document), true, nil
}
