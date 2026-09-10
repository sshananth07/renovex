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
	collectionAwardOutcomeDeliveries = "award_outcome_deliveries"

	indexNameUniqueAwardDeliveryOperation = "uq_award_deliveries_company_operation"
	indexNameAwardDeliveryByRevision      = "ix_award_deliveries_company_revision_status"
)

type MongoAwardDeliveryRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardDeliveryRepository(
	db *mongo.Database,
) *MongoAwardDeliveryRepository {
	return &MongoAwardDeliveryRepository{
		collection: db.Collection(collectionAwardOutcomeDeliveries),
	}
}

// EnsureIndexes makes delivery idempotent per CompanyID + DeliveryOperationID.
//
// That is what lets a retry with the same operation resolve the SAME record
// instead of sending a second email — the Supplier must not be told twice
// because a response was lost.
func (repository *MongoAwardDeliveryRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "deliveryOperationId", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueAwardDeliveryOperation).
				SetUnique(true),
		},
		{
			// Supports correction-driven obsolescence, which must find the
			// PENDING records of superseded revisions cheaply.
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "awardRevisionId", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName(indexNameAwardDeliveryByRevision),
		},
	})
	return err
}

type awardDeliveryDocument struct {
	ID                  bson.ObjectID `bson:"_id,omitempty"`
	CompanyID           string        `bson:"companyId"`
	AwardOutcomeID      string        `bson:"awardOutcomeId"`
	AwardRevisionID     string        `bson:"awardRevisionId"`
	SupplierID          string        `bson:"supplierId"`
	RecipientIdentity   string        `bson:"recipientIdentity"`
	AccessGeneration    int64         `bson:"accessGeneration"`
	DeliveryOperationID string        `bson:"deliveryOperationId"`
	Channel             string        `bson:"channel"`
	Status              string        `bson:"status"`
	FailureCode         string        `bson:"failureCode,omitempty"`
	CreatedAt           time.Time     `bson:"createdAt"`
	SentAt              *time.Time    `bson:"sentAt,omitempty"`
	SchemaVersion       int           `bson:"schemaVersion"`
}

func deliveryToDocument(delivery AwardOutcomeDelivery) awardDeliveryDocument {
	return awardDeliveryDocument{
		CompanyID:           delivery.CompanyID,
		AwardOutcomeID:      delivery.AwardOutcomeID,
		AwardRevisionID:     delivery.AwardRevisionID,
		SupplierID:          delivery.SupplierID,
		RecipientIdentity:   delivery.RecipientIdentity,
		AccessGeneration:    delivery.AccessGeneration,
		DeliveryOperationID: delivery.DeliveryOperationID,
		Channel:             delivery.Channel,
		Status:              string(delivery.Status),
		FailureCode:         string(delivery.FailureCode),
		CreatedAt:           delivery.CreatedAt,
		SentAt:              delivery.SentAt,
		SchemaVersion:       AwardOutcomeDeliverySchemaVersion,
	}
}

func deliveryFromDocument(document awardDeliveryDocument) AwardOutcomeDelivery {
	return AwardOutcomeDelivery{
		ID:                  document.ID.Hex(),
		CompanyID:           document.CompanyID,
		AwardOutcomeID:      document.AwardOutcomeID,
		AwardRevisionID:     document.AwardRevisionID,
		SupplierID:          document.SupplierID,
		RecipientIdentity:   document.RecipientIdentity,
		AccessGeneration:    document.AccessGeneration,
		DeliveryOperationID: document.DeliveryOperationID,
		Channel:             document.Channel,
		Status:              DeliveryStatus(document.Status),
		FailureCode:         DeliveryFailureCode(document.FailureCode),
		CreatedAt:           document.CreatedAt,
		SentAt:              document.SentAt,
		SchemaVersion:       document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every AwardOutcomeDelivery owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoAwardDeliveryRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureDeliveryIntent persists the PENDING record before any send is
// attempted (§8I step 1), returning whether it created one.
//
// A crash after this call leaves a recoverable `pending` record rather than a
// sent email with no trace. A same-operation retry adopts the existing record
// and reports created=false, so the caller knows not to send again.
func (repository *MongoAwardDeliveryRepository) EnsureDeliveryIntent(
	ctx context.Context,
	candidate AwardOutcomeDelivery,
) (AwardOutcomeDelivery, bool, error) {
	if err := candidate.Validate(); err != nil {
		return AwardOutcomeDelivery{}, false, err
	}
	if candidate.Status != DeliveryPending {
		return AwardOutcomeDelivery{}, false, ErrInvalidAwardDelivery
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}

	document := deliveryToDocument(candidate)
	result, err := repository.collection.InsertOne(ctx, document)
	if err == nil {
		document.ID = result.InsertedID.(bson.ObjectID)
		return deliveryFromDocument(document), true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return AwardOutcomeDelivery{}, false, err
	}

	existing, found, findErr := repository.FindDeliveryByOperation(
		ctx, candidate.CompanyID, candidate.DeliveryOperationID)
	if findErr != nil {
		return AwardOutcomeDelivery{}, false, findErr
	}
	if !found {
		return AwardOutcomeDelivery{}, false, ErrAwardDeliveryNotFound
	}
	return existing, false, nil
}

func (repository *MongoAwardDeliveryRepository) FindDeliveryByOperation(
	ctx context.Context,
	companyID, deliveryOperationID string,
) (AwardOutcomeDelivery, bool, error) {
	var document awardDeliveryDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":           companyID,
		"deliveryOperationId": deliveryOperationID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardOutcomeDelivery{}, false, nil
	}
	if err != nil {
		return AwardOutcomeDelivery{}, false, err
	}
	return deliveryFromDocument(document), true, nil
}

func (repository *MongoAwardDeliveryRepository) FindDelivery(
	ctx context.Context,
	companyID, deliveryID string,
) (AwardOutcomeDelivery, bool, error) {
	objectID, err := bson.ObjectIDFromHex(deliveryID)
	if err != nil {
		return AwardOutcomeDelivery{}, false, nil
	}

	var document awardDeliveryDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardOutcomeDelivery{}, false, nil
	}
	if err != nil {
		return AwardOutcomeDelivery{}, false, err
	}
	return deliveryFromDocument(document), true, nil
}

// MarkDeliverySent records that the mail was handed to the transport (step 3).
//
// `pending` is in the FILTER, so a record already resolved cannot be rewritten.
func (repository *MongoAwardDeliveryRepository) MarkDeliverySent(
	ctx context.Context,
	companyID, deliveryID string,
	sentAt time.Time,
) error {
	return repository.resolveDelivery(ctx, companyID, deliveryID, bson.M{
		"$set": bson.M{
			"status": string(DeliverySent),
			"sentAt": sentAt,
		},
	})
}

// MarkDeliveryFailed records a bounded failure code (step 3).
//
// Failure never rolls back the award or the outcome: the decision stands, and
// only the attempt to communicate it did not.
func (repository *MongoAwardDeliveryRepository) MarkDeliveryFailed(
	ctx context.Context,
	companyID, deliveryID string,
	code DeliveryFailureCode,
) error {
	if !code.Valid() {
		return ErrInvalidAwardDelivery
	}
	return repository.resolveDelivery(ctx, companyID, deliveryID, bson.M{
		"$set": bson.M{
			"status":      string(DeliveryFailed),
			"failureCode": string(code),
		},
	})
}

func (repository *MongoAwardDeliveryRepository) resolveDelivery(
	ctx context.Context,
	companyID, deliveryID string,
	update bson.M,
) error {
	objectID, err := bson.ObjectIDFromHex(deliveryID)
	if err != nil {
		return ErrAwardDeliveryNotFound
	}

	result, err := repository.collection.UpdateOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
		"status":    string(DeliveryPending),
	}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrAwardDeliveryNotFound
	}
	return nil
}

// ObsoletePendingDeliveries marks a superseded revision's PENDING records
// obsolete when a correction publishes.
//
// It deliberately touches `pending` only: `sent` and `failed` are historical
// facts — a Supplier really was told — and rewriting them would falsify the
// record (§8I). It returns the obsoleted records so each can be audited.
func (repository *MongoAwardDeliveryRepository) ObsoletePendingDeliveries(
	ctx context.Context,
	companyID, awardRevisionID string,
) ([]AwardOutcomeDelivery, error) {
	filter := bson.M{
		"companyId":       companyID,
		"awardRevisionId": awardRevisionID,
		"status":          string(DeliveryPending),
	}

	cursor, err := repository.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var affected []AwardOutcomeDelivery
	for cursor.Next(ctx) {
		var document awardDeliveryDocument
		if err := cursor.Decode(&document); err != nil {
			cursor.Close(ctx)
			return nil, err
		}
		affected = append(affected, deliveryFromDocument(document))
	}
	cursor.Close(ctx)
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	if len(affected) == 0 {
		return nil, nil
	}

	if _, err := repository.collection.UpdateMany(ctx, filter, bson.M{
		"$set": bson.M{"status": string(DeliveryObsolete)},
	}); err != nil {
		return nil, err
	}
	return affected, nil
}

// ListDeliveries returns every attempt made for one revision, in creation
// order, so the history reads as the sequence of attempts it is.
func (repository *MongoAwardDeliveryRepository) ListDeliveries(
	ctx context.Context,
	companyID, awardRevisionID string,
) ([]AwardOutcomeDelivery, error) {
	cursor, err := repository.collection.Find(ctx,
		bson.M{"companyId": companyID, "awardRevisionId": awardRevisionID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	deliveries := []AwardOutcomeDelivery{}
	for cursor.Next(ctx) {
		var document awardDeliveryDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, deliveryFromDocument(document))
	}
	return deliveries, cursor.Err()
}
