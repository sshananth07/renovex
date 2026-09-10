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
	collectionInvitationDeliveryAttempts = "invitation_delivery_attempts"

	indexNameUniqueDeliveryOperation = "uq_invitation_delivery_attempts_company_invitation_operation"
	indexNameDeliveryCompanyInvite   = "idx_invitation_delivery_attempts_company_invitation"
)

// MongoDeliveryAttemptRepository is the MongoDB-backed store of invitation
// delivery attempts.
//
// An attempt is an append-only record of one send or copy. Only its status
// transitions; nothing else about it is ever rewritten, so the history of what
// was sent to whom stays intact across recipient replacements.
type MongoDeliveryAttemptRepository struct {
	collection *mongo.Collection
}

// NewMongoDeliveryAttemptRepository constructs the repository over db's
// invitation_delivery_attempts collection.
func NewMongoDeliveryAttemptRepository(db *mongo.Database) *MongoDeliveryAttemptRepository {
	return &MongoDeliveryAttemptRepository{
		collection: db.Collection(collectionInvitationDeliveryAttempts),
	}
}

// EnsureIndexes creates the §11.2 indexes. Idempotent.
func (r *MongoDeliveryAttemptRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Send idempotency (§10.2). This unique index is what actually stops a
		// retried or double-clicked send delivering the Supplier two copies of
		// the same invitation — the service's pre-check narrows the window but
		// cannot close it.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "invitationId", Value: 1},
			{Key: "deliveryOperationId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueDeliveryOperation)},
		// Serves the per-invitation delivery history.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "invitationId", Value: 1}},
			Options: options.Index().SetName(indexNameDeliveryCompanyInvite)},
	})
	return err
}

type deliveryAttemptDoc struct {
	ID                  bson.ObjectID `bson:"_id,omitempty"`
	CompanyID           string        `bson:"companyId"`
	InvitationID        string        `bson:"invitationId"`
	DeliveryOperationID string        `bson:"deliveryOperationId"`

	AccessGeneration   int64  `bson:"accessGeneration"`
	Channel            string `bson:"channel"`
	RecipientEmail     string `bson:"recipientEmail"`
	IssuedRFQVersionID string `bson:"issuedRfqVersionId,omitempty"`

	Status string `bson:"status"`
	// FailureCode is a BOUNDED token. Raw provider error text is never
	// persisted (§1A.2).
	FailureCode string `bson:"failureCode,omitempty"`

	RequestedByUserID string     `bson:"requestedByUserId,omitempty"`
	RequestedAt       time.Time  `bson:"requestedAt"`
	SentAt            *time.Time `bson:"sentAt,omitempty"`
	DeliveredAt       *time.Time `bson:"deliveredAt,omitempty"`
	SchemaVersion     int        `bson:"schemaVersion"`
}

func toDeliveryAttemptDoc(a InvitationDeliveryAttempt) deliveryAttemptDoc {
	return deliveryAttemptDoc{
		CompanyID: a.CompanyID, InvitationID: a.InvitationID,
		DeliveryOperationID: a.DeliveryOperationID,
		AccessGeneration:    a.AccessGeneration,
		Channel:             string(a.Channel),
		RecipientEmail:      a.RecipientEmail,
		IssuedRFQVersionID:  a.IssuedRFQVersionID,
		Status:              string(a.Status),
		FailureCode:         string(a.FailureCode),
		RequestedByUserID:   a.RequestedByUserID,
		RequestedAt:         a.RequestedAt,
		SentAt:              a.SentAt,
		DeliveredAt:         a.DeliveredAt,
		SchemaVersion:       a.SchemaVersion,
	}
}

func fromDeliveryAttemptDoc(doc deliveryAttemptDoc) InvitationDeliveryAttempt {
	return InvitationDeliveryAttempt{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, InvitationID: doc.InvitationID,
		DeliveryOperationID: doc.DeliveryOperationID,
		AccessGeneration:    doc.AccessGeneration,
		Channel:             DeliveryChannel(doc.Channel),
		RecipientEmail:      doc.RecipientEmail,
		IssuedRFQVersionID:  doc.IssuedRFQVersionID,
		Status:              DeliveryStatus(doc.Status),
		FailureCode:         DeliveryFailureCode(doc.FailureCode),
		RequestedByUserID:   doc.RequestedByUserID,
		RequestedAt:         doc.RequestedAt,
		SentAt:              doc.SentAt,
		DeliveredAt:         doc.DeliveredAt,
		SchemaVersion:       doc.SchemaVersion,
	}
}

// CreateAttempt persists one delivery intent.
//
// This is the FIRST step of a send: the record exists before any mail is
// attempted, so an interrupted send leaves a pending row rather than no trace
// at all (§1A.2).
// DeleteAllForCompany permanently removes every DeliveryAttempt owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoDeliveryAttemptRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoDeliveryAttemptRepository) CreateAttempt(ctx context.Context,
	attempt InvitationDeliveryAttempt) (InvitationDeliveryAttempt, error) {

	res, err := r.collection.InsertOne(ctx, toDeliveryAttemptDoc(attempt))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// The only unique index on this collection is the operation-ID
			// invariant, so this classification is unambiguous.
			return InvitationDeliveryAttempt{}, ErrDeliveryOperationAlreadyUsed
		}
		return InvitationDeliveryAttempt{}, err
	}
	attempt.ID = res.InsertedID.(bson.ObjectID).Hex()
	return attempt, nil
}

// UpdateAttemptStatus records the outcome of a send.
//
// There is no revision guard: an attempt has exactly one writer — the send that
// created it — so there is no concurrent editor to fence against. Reconciliation
// re-drives a stuck `pending` attempt through the same transition.
func (r *MongoDeliveryAttemptRepository) UpdateAttemptStatus(ctx context.Context,
	companyID, attemptID string, status DeliveryStatus,
	failureCode DeliveryFailureCode, sentAt *time.Time) error {

	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return ErrDeliveryAttemptNotFound
	}

	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{
			"status":      string(status),
			"failureCode": string(failureCode),
			"sentAt":      sentAt,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrDeliveryAttemptNotFound
	}
	return nil
}

// FindAttemptByOperationID resolves the attempt a previous send created under
// this idempotency key, which is what lets a retry return the original outcome
// instead of sending again (§10.2).
func (r *MongoDeliveryAttemptRepository) FindAttemptByOperationID(ctx context.Context,
	companyID, invitationID, operationID string) (InvitationDeliveryAttempt, bool, error) {

	var doc deliveryAttemptDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "invitationId": invitationID,
		"deliveryOperationId": operationID,
	}).Decode(&doc)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return InvitationDeliveryAttempt{}, false, nil
	}
	if err != nil {
		return InvitationDeliveryAttempt{}, false, err
	}
	return fromDeliveryAttemptDoc(doc), true, nil
}

// ListAttemptsForInvitation returns the delivery history for one invitation,
// tenant-scoped.
func (r *MongoDeliveryAttemptRepository) ListAttemptsForInvitation(ctx context.Context,
	companyID, invitationID string) ([]InvitationDeliveryAttempt, error) {

	cursor, err := r.collection.Find(ctx, bson.M{
		"companyId": companyID, "invitationId": invitationID,
	}, options.Find().SetSort(bson.D{{Key: "requestedAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []deliveryAttemptDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	out := make([]InvitationDeliveryAttempt, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromDeliveryAttemptDoc(doc))
	}
	return out, nil
}

// ObsoletePendingAttemptsBeforeGeneration retires only delivery intents whose
// links can no longer verify.
//
// Status and generation are checked by MongoDB in the same update that changes
// the status. This closes the race where a sender could mark an attempt sent
// after a service listed it but before a list-then-write cleanup overwrote that
// historical fact as obsolete.
func (r *MongoDeliveryAttemptRepository) ObsoletePendingAttemptsBeforeGeneration(
	ctx context.Context, companyID, invitationID string, currentGeneration int64,
) (int64, error) {

	res, err := r.collection.UpdateMany(ctx,
		bson.M{
			"companyId": companyID, "invitationId": invitationID,
			"status":           string(DeliveryStatusPending),
			"accessGeneration": bson.M{"$lt": currentGeneration},
		},
		bson.M{"$set": bson.M{
			"status":      string(DeliveryStatusObsolete),
			"failureCode": "",
			"sentAt":      nil,
		}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
