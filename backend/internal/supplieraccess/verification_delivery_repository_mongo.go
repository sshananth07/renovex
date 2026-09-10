package supplieraccess

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionVerificationDeliveryAttempts = "verification_delivery_attempts"
	indexVerificationDeliveryOperation     = "uq_verification_delivery_company_challenge_operation"
	indexVerificationDeliveryStatus        = "idx_verification_delivery_company_challenge_status"
)

type MongoVerificationDeliveryRepository struct {
	collection *mongo.Collection
}

func NewMongoVerificationDeliveryRepository(
	db *mongo.Database) *MongoVerificationDeliveryRepository {
	return &MongoVerificationDeliveryRepository{
		collection: db.Collection(collectionVerificationDeliveryAttempts),
	}
}

func (r *MongoVerificationDeliveryRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "challengeId", Value: 1},
				{Key: "operationId", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName(indexVerificationDeliveryOperation),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "challengeId", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName(indexVerificationDeliveryStatus),
		},
	})
	return err
}

type verificationDeliveryDoc struct {
	ID          string    `bson:"_id"`
	CompanyID   string    `bson:"companyId"`
	ChallengeID string    `bson:"challengeId"`
	OperationID string    `bson:"operationId"`
	Status      string    `bson:"status"`
	FailureCode *string   `bson:"failureCode,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

func toVerificationDeliveryDoc(
	attempt VerificationDeliveryAttempt) verificationDeliveryDoc {
	var failureCode *string
	if attempt.FailureCode != nil {
		value := string(*attempt.FailureCode)
		failureCode = &value
	}
	return verificationDeliveryDoc{
		ID: attempt.ID, CompanyID: attempt.CompanyID,
		ChallengeID: attempt.ChallengeID, OperationID: attempt.OperationID,
		Status: string(attempt.Status), FailureCode: failureCode,
		CreatedAt: attempt.CreatedAt, UpdatedAt: attempt.UpdatedAt,
	}
}

func fromVerificationDeliveryDoc(
	doc verificationDeliveryDoc) VerificationDeliveryAttempt {
	var failureCode *VerificationDeliveryFailureCode
	if doc.FailureCode != nil {
		value := VerificationDeliveryFailureCode(*doc.FailureCode)
		failureCode = &value
	}
	return VerificationDeliveryAttempt{
		ID: doc.ID, CompanyID: doc.CompanyID,
		ChallengeID: doc.ChallengeID, OperationID: doc.OperationID,
		Status: VerificationDeliveryStatus(doc.Status), FailureCode: failureCode,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}

func validVerificationDeliveryStatus(status VerificationDeliveryStatus) bool {
	switch status {
	case VerificationDeliveryPending, VerificationDeliverySent,
		VerificationDeliveryFailed, VerificationDeliveryObsolete:
		return true
	default:
		return false
	}
}

func validateVerificationDeliveryAttempt(attempt VerificationDeliveryAttempt) error {
	if attempt.ID == "" || attempt.CompanyID == "" || attempt.ChallengeID == "" ||
		attempt.OperationID == "" || !validVerificationDeliveryStatus(attempt.Status) ||
		attempt.CreatedAt.IsZero() || attempt.UpdatedAt.IsZero() {
		return ErrInvalidVerificationDeliveryAttempt
	}
	if attempt.Status == VerificationDeliveryFailed && attempt.FailureCode == nil {
		return ErrInvalidVerificationDeliveryAttempt
	}
	if attempt.Status != VerificationDeliveryFailed && attempt.FailureCode != nil {
		return ErrInvalidVerificationDeliveryAttempt
	}
	return nil
}

// DeleteAllForCompany permanently removes every VerificationDeliveryAttempt
// owned by companyID. Never errors when zero documents match.
// Development-tool use only.
func (r *MongoVerificationDeliveryRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoVerificationDeliveryRepository) CreateAttempt(ctx context.Context,
	attempt VerificationDeliveryAttempt) (VerificationDeliveryAttempt, error) {

	if err := validateVerificationDeliveryAttempt(attempt); err != nil {
		return VerificationDeliveryAttempt{}, err
	}
	_, err := r.collection.InsertOne(ctx, toVerificationDeliveryDoc(attempt))
	if err == nil {
		return attempt, nil
	}
	if mongo.IsDuplicateKeyError(err) &&
		strings.Contains(err.Error(), indexVerificationDeliveryOperation) {
		return VerificationDeliveryAttempt{},
			ErrVerificationDeliveryOperationAlreadyUsed
	}
	return VerificationDeliveryAttempt{}, err
}

// TransitionPending is the only status mutation. Sent, failed, and obsolete
// attempts are historical facts and cannot be rewritten.
func (r *MongoVerificationDeliveryRepository) TransitionPending(ctx context.Context,
	companyID, attemptID string, target VerificationDeliveryStatus,
	failureCode *VerificationDeliveryFailureCode,
	updatedAt time.Time) (VerificationDeliveryAttempt, error) {

	if target != VerificationDeliverySent && target != VerificationDeliveryFailed &&
		target != VerificationDeliveryObsolete {
		return VerificationDeliveryAttempt{}, ErrInvalidVerificationDeliveryAttempt
	}
	if (target == VerificationDeliveryFailed) != (failureCode != nil) ||
		updatedAt.IsZero() {
		return VerificationDeliveryAttempt{}, ErrInvalidVerificationDeliveryAttempt
	}
	update := bson.M{"status": string(target), "updatedAt": updatedAt}
	if failureCode != nil {
		update["failureCode"] = string(*failureCode)
	}
	var doc verificationDeliveryDoc
	err := r.collection.FindOneAndUpdate(ctx, bson.M{
		"_id": attemptID, "companyId": companyID,
		"status": string(VerificationDeliveryPending),
	}, bson.M{"$set": update},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if err == nil {
		return fromVerificationDeliveryDoc(doc), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return VerificationDeliveryAttempt{}, err
	}
	err = r.collection.FindOne(ctx,
		bson.M{"_id": attemptID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return VerificationDeliveryAttempt{}, ErrVerificationDeliveryAttemptNotFound
	}
	if err != nil {
		return VerificationDeliveryAttempt{}, err
	}
	return VerificationDeliveryAttempt{}, ErrVerificationDeliveryNotPending
}

func (r *MongoVerificationDeliveryRepository) FindAttemptByOperation(ctx context.Context,
	companyID, challengeID, operationID string) (VerificationDeliveryAttempt, error) {

	var doc verificationDeliveryDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "challengeId": challengeID, "operationId": operationID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return VerificationDeliveryAttempt{}, ErrVerificationDeliveryAttemptNotFound
	}
	if err != nil {
		return VerificationDeliveryAttempt{}, err
	}
	return fromVerificationDeliveryDoc(doc), nil
}

// ObsoletePendingForChallenges invalidates only mail intents that have not
// completed. Sent and failed attempts remain immutable historical facts.
func (r *MongoVerificationDeliveryRepository) ObsoletePendingForChallenges(
	ctx context.Context, companyID string, challengeIDs []string,
	updatedAt time.Time) error {

	if len(challengeIDs) == 0 {
		return nil
	}
	_, err := r.collection.UpdateMany(ctx, bson.M{
		"companyId":   companyID,
		"challengeId": bson.M{"$in": challengeIDs},
		"status":      string(VerificationDeliveryPending),
	}, bson.M{"$set": bson.M{
		"status":    string(VerificationDeliveryObsolete),
		"updatedAt": updatedAt,
	}})
	return err
}
