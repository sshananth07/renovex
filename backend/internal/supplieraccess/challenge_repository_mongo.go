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
	collectionVerificationChallenges = "email_verification_challenges"
	indexChallengeOperation          = "uq_email_verification_challenges_company_operation"
	indexChallengeExchange           = "uq_email_verification_challenges_exchange"
	indexChallengeIdentity           = "idx_email_verification_challenges_company_invitation_generation"
)

type MongoVerificationChallengeRepository struct {
	collection *mongo.Collection
}

func NewMongoVerificationChallengeRepository(
	db *mongo.Database) *MongoVerificationChallengeRepository {
	return &MongoVerificationChallengeRepository{
		collection: db.Collection(collectionVerificationChallenges),
	}
}

func (r *MongoVerificationChallengeRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "challengeOperationId", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName(indexChallengeOperation),
		},
		{
			Keys:    bson.D{{Key: "exchangeId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexChallengeExchange),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "invitationId", Value: 1},
				{Key: "accessGeneration", Value: 1},
				{Key: "createdAt", Value: -1},
			},
			Options: options.Index().SetName(indexChallengeIdentity),
		},
	})
	return err
}

type verificationChallengeDoc struct {
	ID                       string     `bson:"_id"`
	ExchangeID               string     `bson:"exchangeId"`
	CompanyID                string     `bson:"companyId"`
	SupplierID               string     `bson:"supplierId"`
	RecipientEmailNormalized string     `bson:"recipientEmailNormalized"`
	InvitationID             string     `bson:"invitationId"`
	AccessGeneration         int64      `bson:"accessGeneration"`
	ChallengeOperationID     string     `bson:"challengeOperationId"`
	CodeVerifier             string     `bson:"codeVerifier"`
	CodeKeyVersion           int        `bson:"codeKeyVersion"`
	AttemptsRemaining        int        `bson:"attemptsRemaining"`
	ExpiresAt                time.Time  `bson:"expiresAt"`
	SupersededAt             *time.Time `bson:"supersededAt,omitempty"`
	LockedAt                 *time.Time `bson:"lockedAt,omitempty"`
	ConsumedAt               *time.Time `bson:"consumedAt,omitempty"`
	ConsumedOperationID      string     `bson:"consumedOperationId,omitempty"`
	SupplierSessionID        string     `bson:"supplierSessionId,omitempty"`
	TargetTokenGeneration    int64      `bson:"targetTokenGeneration,omitempty"`
	SessionTokenKeyVersion   int        `bson:"sessionTokenKeyVersion,omitempty"`
	Revision                 int64      `bson:"revision"`
	CreatedAt                time.Time  `bson:"createdAt"`
	SchemaVersion            int        `bson:"schemaVersion"`
}

func toVerificationChallengeDoc(
	challenge EmailVerificationChallenge) verificationChallengeDoc {
	return verificationChallengeDoc{
		ID: challenge.ID, ExchangeID: challenge.ExchangeID,
		CompanyID: challenge.CompanyID, SupplierID: challenge.SupplierID,
		RecipientEmailNormalized: challenge.RecipientEmailNormalized,
		InvitationID:             challenge.InvitationID,
		AccessGeneration:         challenge.AccessGeneration,
		ChallengeOperationID:     challenge.ChallengeOperationID,
		CodeVerifier:             challenge.CodeVerifier,
		CodeKeyVersion:           challenge.CodeKeyVersion,
		AttemptsRemaining:        challenge.AttemptsRemaining,
		ExpiresAt:                challenge.ExpiresAt,
		SupersededAt:             challenge.SupersededAt,
		LockedAt:                 challenge.LockedAt,
		ConsumedAt:               challenge.ConsumedAt,
		ConsumedOperationID:      challenge.ConsumedOperationID,
		SupplierSessionID:        challenge.SupplierSessionID,
		TargetTokenGeneration:    challenge.TargetTokenGeneration,
		SessionTokenKeyVersion:   challenge.SessionTokenKeyVersion,
		Revision:                 challenge.Revision,
		CreatedAt:                challenge.CreatedAt,
		SchemaVersion:            challenge.SchemaVersion,
	}
}

func fromVerificationChallengeDoc(
	doc verificationChallengeDoc) EmailVerificationChallenge {
	return EmailVerificationChallenge{
		ID: doc.ID, ExchangeID: doc.ExchangeID,
		CompanyID: doc.CompanyID, SupplierID: doc.SupplierID,
		RecipientEmailNormalized: doc.RecipientEmailNormalized,
		InvitationID:             doc.InvitationID,
		AccessGeneration:         doc.AccessGeneration,
		ChallengeOperationID:     doc.ChallengeOperationID,
		CodeVerifier:             doc.CodeVerifier,
		CodeKeyVersion:           doc.CodeKeyVersion,
		AttemptsRemaining:        doc.AttemptsRemaining,
		ExpiresAt:                doc.ExpiresAt,
		SupersededAt:             doc.SupersededAt,
		LockedAt:                 doc.LockedAt,
		ConsumedAt:               doc.ConsumedAt,
		ConsumedOperationID:      doc.ConsumedOperationID,
		SupplierSessionID:        doc.SupplierSessionID,
		TargetTokenGeneration:    doc.TargetTokenGeneration,
		SessionTokenKeyVersion:   doc.SessionTokenKeyVersion,
		Revision:                 doc.Revision,
		CreatedAt:                doc.CreatedAt,
		SchemaVersion:            doc.SchemaVersion,
	}
}

func validateVerificationChallenge(challenge EmailVerificationChallenge) error {
	if challenge.ID == "" || challenge.ExchangeID == "" ||
		challenge.CompanyID == "" || challenge.SupplierID == "" ||
		challenge.RecipientEmailNormalized == "" || challenge.InvitationID == "" ||
		challenge.AccessGeneration < 1 || challenge.ChallengeOperationID == "" ||
		!canonicalSHA256Hex.MatchString(challenge.CodeVerifier) ||
		challenge.CodeKeyVersion < 1 || challenge.AttemptsRemaining < 0 ||
		challenge.AttemptsRemaining > 5 || challenge.CreatedAt.IsZero() ||
		!challenge.ExpiresAt.After(challenge.CreatedAt) ||
		challenge.Revision < 1 || challenge.SchemaVersion < 1 {
		return ErrInvalidVerificationChallenge
	}
	return nil
}

// DeleteAllForCompany permanently removes every EmailVerificationChallenge
// owned by companyID. Never errors when zero documents match.
// Development-tool use only.
func (r *MongoVerificationChallengeRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// ListChallengesForCompany returns every challenge owned by companyID.
// Development-tool use only: cleanup needs the full identity of each
// challenge (SupplierID, InvitationID, AccessGeneration, ID) to re-derive the
// keyed rate-limit fingerprints it claimed, since the rate-limit collection
// itself stores only the irreversible hash, not companyID.
func (r *MongoVerificationChallengeRepository) ListChallengesForCompany(
	ctx context.Context, companyID string) ([]EmailVerificationChallenge, error) {

	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []verificationChallengeDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	challenges := make([]EmailVerificationChallenge, 0, len(docs))
	for _, doc := range docs {
		challenges = append(challenges, fromVerificationChallengeDoc(doc))
	}
	return challenges, nil
}

// CreateChallenge persists the keyed verifier and recovery projection before
// any mail is sent. No field exists for the raw code or message body.
func (r *MongoVerificationChallengeRepository) CreateChallenge(ctx context.Context,
	challenge EmailVerificationChallenge) (EmailVerificationChallenge, error) {

	if err := validateVerificationChallenge(challenge); err != nil {
		return EmailVerificationChallenge{}, err
	}
	_, err := r.collection.InsertOne(ctx, toVerificationChallengeDoc(challenge))
	if err == nil {
		return challenge, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return EmailVerificationChallenge{}, err
	}
	switch {
	case strings.Contains(err.Error(), indexChallengeOperation):
		return EmailVerificationChallenge{}, ErrChallengeOperationAlreadyUsed
	case strings.Contains(err.Error(), indexChallengeExchange):
		return EmailVerificationChallenge{}, ErrExchangeChallengeAlreadyExists
	case strings.Contains(err.Error(), "_id_"):
		return EmailVerificationChallenge{}, ErrVerificationChallengeExists
	default:
		return EmailVerificationChallenge{}, err
	}
}

// FindChallenge starts with an opaque, globally unique challenge handle. Public
// callers provide no tenant ID; the loaded record supplies the identity that
// later verification must revalidate.
func (r *MongoVerificationChallengeRepository) FindChallenge(
	ctx context.Context, challengeID string) (EmailVerificationChallenge, error) {

	if challengeID == "" {
		return EmailVerificationChallenge{}, ErrVerificationChallengeNotFound
	}
	var doc verificationChallengeDoc
	err := r.collection.FindOne(ctx, bson.M{"_id": challengeID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return EmailVerificationChallenge{}, ErrVerificationChallengeNotFound
	}
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	return fromVerificationChallengeDoc(doc), nil
}

func (r *MongoVerificationChallengeRepository) FindChallengeByExchange(
	ctx context.Context, exchangeID string) (EmailVerificationChallenge, error) {
	return r.findOneChallenge(ctx, bson.M{"exchangeId": exchangeID})
}

func (r *MongoVerificationChallengeRepository) FindChallengeByOperation(
	ctx context.Context, companyID, operationID string) (
	EmailVerificationChallenge, error) {
	return r.findOneChallenge(ctx, bson.M{
		"companyId": companyID, "challengeOperationId": operationID,
	})
}

func (r *MongoVerificationChallengeRepository) findOneChallenge(
	ctx context.Context, filter bson.M) (EmailVerificationChallenge, error) {

	var doc verificationChallengeDoc
	err := r.collection.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return EmailVerificationChallenge{}, ErrVerificationChallengeNotFound
	}
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	return fromVerificationChallengeDoc(doc), nil
}

// FindLatestChallenge selects the authoritative challenge for one invitation
// generation. The ID tie-breaker makes concurrent creations at the same clock
// instant converge on one winner.
func (r *MongoVerificationChallengeRepository) FindLatestChallenge(
	ctx context.Context, companyID, invitationID string, accessGeneration int64,
) (EmailVerificationChallenge, error) {

	var doc verificationChallengeDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "invitationId": invitationID,
		"accessGeneration": accessGeneration,
	}, options.FindOne().SetSort(bson.D{
		{Key: "createdAt", Value: -1},
		{Key: "_id", Value: -1},
	})).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return EmailVerificationChallenge{}, ErrVerificationChallengeNotFound
	}
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	return fromVerificationChallengeDoc(doc), nil
}

// SupersedeOlderChallenges records history after the newer document exists.
// Request-time authorization relies on FindLatestChallenge, so a crash before
// this projection update cannot make the older code valid again.
func (r *MongoVerificationChallengeRepository) SupersedeOlderChallenges(
	ctx context.Context, newer EmailVerificationChallenge,
	supersededAt time.Time) ([]string, error) {

	olderFilter := bson.M{
		"companyId": newer.CompanyID, "invitationId": newer.InvitationID,
		"accessGeneration": newer.AccessGeneration,
		"supersededAt":     bson.M{"$exists": false},
		"consumedAt":       bson.M{"$exists": false},
		"$or": bson.A{
			bson.M{"createdAt": bson.M{"$lt": newer.CreatedAt}},
			bson.M{
				"createdAt": newer.CreatedAt,
				"_id":       bson.M{"$lt": newer.ID},
			},
		},
	}
	cursor, err := r.collection.Find(ctx, olderFilter,
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var identities []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &identities); err != nil {
		return nil, err
	}
	if len(identities) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(identities))
	for _, identity := range identities {
		ids = append(ids, identity.ID)
	}
	_, err = r.collection.UpdateMany(ctx, bson.M{
		"_id": bson.M{"$in": ids}, "supersededAt": bson.M{"$exists": false},
	}, bson.M{"$set": bson.M{"supersededAt": supersededAt}})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// RecordFailedConfirmation consumes one online guess under the challenge
// revision. The fifth failure records AttemptsRemaining=0 and LockedAt in the
// same write, so concurrent requests cannot create multiple final transitions.
func (r *MongoVerificationChallengeRepository) RecordFailedConfirmation(
	ctx context.Context, expected EmailVerificationChallenge,
	failedAt time.Time) (EmailVerificationChallenge, error) {

	if expected.ID == "" || expected.Revision < 1 ||
		expected.AttemptsRemaining < 1 ||
		!canonicalSHA256Hex.MatchString(expected.CodeVerifier) ||
		failedAt.IsZero() {
		return EmailVerificationChallenge{},
			ErrVerificationChallengeNotConfirmable
	}
	set := bson.M{
		"attemptsRemaining": expected.AttemptsRemaining - 1,
		"revision":          expected.Revision + 1,
	}
	if expected.AttemptsRemaining == 1 {
		set["lockedAt"] = failedAt
	}
	var updated verificationChallengeDoc
	err := r.collection.FindOneAndUpdate(ctx,
		confirmationChallengeFilter(expected, failedAt),
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return EmailVerificationChallenge{},
			ErrVerificationChallengeNotConfirmable
	}
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	return fromVerificationChallengeDoc(updated), nil
}

// ConsumeForVerification atomically records the exact session transition that
// recovery must finish. Operation ID alone can never recover access because
// the service revalidates the submitted code before adopting these fields.
func (r *MongoVerificationChallengeRepository) ConsumeForVerification(
	ctx context.Context, expected EmailVerificationChallenge,
	target VerificationSessionTarget,
	confirmedAt time.Time) (EmailVerificationChallenge, error) {

	if expected.ID == "" || expected.Revision < 1 ||
		expected.AttemptsRemaining < 1 ||
		!canonicalSHA256Hex.MatchString(expected.CodeVerifier) ||
		target.OperationID == "" || target.SupplierSessionID == "" ||
		target.TokenGeneration < 1 || target.TokenKeyVersion < 1 ||
		confirmedAt.IsZero() {
		return EmailVerificationChallenge{},
			ErrVerificationChallengeNotConfirmable
	}
	var updated verificationChallengeDoc
	err := r.collection.FindOneAndUpdate(ctx,
		confirmationChallengeFilter(expected, confirmedAt),
		bson.M{"$set": bson.M{
			"consumedAt":             confirmedAt,
			"consumedOperationId":    target.OperationID,
			"supplierSessionId":      target.SupplierSessionID,
			"targetTokenGeneration":  target.TokenGeneration,
			"sessionTokenKeyVersion": target.TokenKeyVersion,
			"revision":               expected.Revision + 1,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return EmailVerificationChallenge{},
			ErrVerificationChallengeNotConfirmable
	}
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	return fromVerificationChallengeDoc(updated), nil
}

func confirmationChallengeFilter(expected EmailVerificationChallenge,
	at time.Time) bson.M {
	return bson.M{
		"_id":               expected.ID,
		"revision":          expected.Revision,
		"codeVerifier":      expected.CodeVerifier,
		"attemptsRemaining": expected.AttemptsRemaining,
		"expiresAt":         bson.M{"$gt": at},
		"supersededAt":      bson.M{"$exists": false},
		"lockedAt":          bson.M{"$exists": false},
		"consumedAt":        bson.M{"$exists": false},
	}
}
