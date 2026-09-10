package identity

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRegistrationVerificationRepository is the MongoDB-backed
// RegistrationVerificationRepository implementation. It owns the
// "registration_verification_challenges" collection exclusively — a
// distinct collection from supplieraccess's own verification-challenge
// collections, deliberately never shared (identity never imports
// supplieraccess and vice versa).
type MongoRegistrationVerificationRepository struct {
	collection *mongo.Collection
}

func NewMongoRegistrationVerificationRepository(db *mongo.Database) *MongoRegistrationVerificationRepository {
	return &MongoRegistrationVerificationRepository{collection: db.Collection("registration_verification_challenges")}
}

// EnsureIndexes creates a unique index on (normalizedRecipient, purpose) —
// this is what makes "exactly one current challenge per recipient+purpose"
// a database-enforced invariant, not just an application convention.
func (r *MongoRegistrationVerificationRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "normalizedRecipient", Value: 1}, {Key: "purpose", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	return err
}

type registrationVerificationChallengeDoc struct {
	ID                      bson.ObjectID `bson:"_id,omitempty"`
	NormalizedRecipient     string        `bson:"normalizedRecipient"`
	Purpose                 string        `bson:"purpose"`
	CodeHash                string        `bson:"codeHash"`
	CreatedAt               time.Time     `bson:"createdAt"`
	ExpiresAt               time.Time     `bson:"expiresAt"`
	ConsumedAt              *time.Time    `bson:"consumedAt,omitempty"`
	FailedAttempts          int           `bson:"failedAttempts"`
	IssuanceCount           int           `bson:"issuanceCount"`
	IssuanceWindowStartedAt time.Time     `bson:"issuanceWindowStartedAt"`
	ResendAvailableAt       time.Time     `bson:"resendAvailableAt"`
}

func toRegistrationVerificationChallenge(doc registrationVerificationChallengeDoc) RegistrationVerificationChallenge {
	return RegistrationVerificationChallenge{
		ID: doc.ID.Hex(), NormalizedRecipient: doc.NormalizedRecipient,
		Purpose: RegistrationVerificationPurpose(doc.Purpose), CodeHash: doc.CodeHash,
		CreatedAt: doc.CreatedAt, ExpiresAt: doc.ExpiresAt, ConsumedAt: doc.ConsumedAt,
		FailedAttempts: doc.FailedAttempts, IssuanceCount: doc.IssuanceCount,
		IssuanceWindowStartedAt: doc.IssuanceWindowStartedAt, ResendAvailableAt: doc.ResendAvailableAt,
	}
}

// IssueOrResume performs the entire "is a new/resend code allowed right
// now, and if so issue it" decision as ONE atomic FindOneAndUpdate: the
// filter itself encodes the cooldown/window gate (either no current
// document exists, or the existing one's ResendAvailableAt has already
// passed AND its rolling window still has room), so two concurrent
// requests can never both pass — the second one's filter simply no longer
// matches once the first has updated the document, and it falls through to
// FindCurrent to observe the authoritative (now rate-limited) state.
func (r *MongoRegistrationVerificationRepository) IssueOrResume(ctx context.Context, recipient string, purpose RegistrationVerificationPurpose, codeHash string, now time.Time) (RegistrationVerificationChallenge, error) {
	windowCutoff := now.Add(-registrationVerificationIssueWindow)

	filter := bson.M{
		"normalizedRecipient": recipient,
		"purpose":             string(purpose),
		"resendAvailableAt":   bson.M{"$lte": now},
		"$or": []bson.M{
			// The rolling issuance window has itself expired — reset it below.
			{"issuanceWindowStartedAt": bson.M{"$lte": windowCutoff}},
			// Still inside the window, but under the bound.
			{"issuanceWindowStartedAt": bson.M{"$gt": windowCutoff}, "issuanceCount": bson.M{"$lt": registrationVerificationMaxIssuances}},
		},
	}

	windowStart := now
	newIssuanceCount := 1
	// A prior read is needed only to decide whether the window resets or
	// increments — this read is advisory (used to compute the $set values),
	// the FILTER above is what actually makes the whole operation atomic;
	// if the read is stale by the time FindOneAndUpdate runs, the filter
	// simply fails to match and this falls through to the not-matched path,
	// never silently issuing an over-quota code.
	if existing, err := r.FindCurrent(ctx, recipient, purpose); err == nil {
		if existing.IssuanceWindowStartedAt.After(windowCutoff) {
			windowStart = existing.IssuanceWindowStartedAt
			newIssuanceCount = existing.IssuanceCount + 1
		}
	} else if !errors.Is(err, ErrRegistrationVerificationNotFound) {
		return RegistrationVerificationChallenge{}, err
	}

	update := bson.M{
		"$set": bson.M{
			"normalizedRecipient":     recipient,
			"purpose":                 string(purpose),
			"codeHash":                codeHash,
			"createdAt":               now,
			"expiresAt":               now.Add(registrationVerificationLifetime),
			"failedAttempts":          0,
			"issuanceCount":           newIssuanceCount,
			"issuanceWindowStartedAt": windowStart,
			"resendAvailableAt":       now.Add(registrationVerificationResendCooldown),
		},
		"$unset": bson.M{"consumedAt": ""},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc registrationVerificationChallengeDoc
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err == nil {
		return toRegistrationVerificationChallenge(doc), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		if mongo.IsDuplicateKeyError(err) {
			// Lost the upsert race against a concurrent issuance — the
			// winner's document exists but did not satisfy our own
			// cooldown/window filter; surface as rate-limited rather than
			// retrying, matching "the same repository operation should
			// enforce the issuance window/cooldown."
			return RegistrationVerificationChallenge{}, ErrRegistrationVerificationRateLimited
		}
		return RegistrationVerificationChallenge{}, err
	}
	// The filter matched zero documents: a current challenge exists but is
	// still within its resend cooldown or has exhausted its issuance
	// window — this is the rate-limited case, never a real "not found"
	// (a genuinely absent document is handled by SetUpsert(true) above).
	return RegistrationVerificationChallenge{}, ErrRegistrationVerificationRateLimited
}

func (r *MongoRegistrationVerificationRepository) FindCurrent(ctx context.Context, recipient string, purpose RegistrationVerificationPurpose) (RegistrationVerificationChallenge, error) {
	var doc registrationVerificationChallengeDoc
	err := r.collection.FindOne(ctx, bson.M{"normalizedRecipient": recipient, "purpose": string(purpose)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RegistrationVerificationChallenge{}, ErrRegistrationVerificationNotFound
	}
	if err != nil {
		return RegistrationVerificationChallenge{}, err
	}
	return toRegistrationVerificationChallenge(doc), nil
}

func (r *MongoRegistrationVerificationRepository) RecordFailedAttempt(ctx context.Context, challengeID string, now time.Time) (RegistrationVerificationChallenge, error) {
	objID, err := bson.ObjectIDFromHex(challengeID)
	if err != nil {
		return RegistrationVerificationChallenge{}, ErrRegistrationVerificationNotFound
	}
	filter := bson.M{
		"_id":            objID,
		"consumedAt":     bson.M{"$exists": false},
		"expiresAt":      bson.M{"$gt": now},
		"failedAttempts": bson.M{"$lt": registrationVerificationMaxAttempts},
	}
	update := bson.M{"$inc": bson.M{"failedAttempts": 1}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc registrationVerificationChallengeDoc
	if err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Already inactive (consumed/expired/over the limit) by the
			// time this landed — nothing to increment; the caller's own
			// subsequent verification logic will observe the terminal
			// state via Consume's identical filter and reject accordingly.
			return RegistrationVerificationChallenge{}, ErrRegistrationVerificationInvalid
		}
		return RegistrationVerificationChallenge{}, err
	}
	return toRegistrationVerificationChallenge(doc), nil
}

// Consume atomically transitions the named challenge to consumed — the
// database-level filter (consumedAt absent, not expired, under the attempt
// limit) IS the compare-and-set: only one concurrent call can ever match
// and update the document, guaranteeing exactly-once consumption.
func (r *MongoRegistrationVerificationRepository) Consume(ctx context.Context, challengeID string, now time.Time) (RegistrationVerificationChallenge, error) {
	objID, err := bson.ObjectIDFromHex(challengeID)
	if err != nil {
		return RegistrationVerificationChallenge{}, ErrRegistrationVerificationInvalid
	}
	filter := bson.M{
		"_id":            objID,
		"consumedAt":     bson.M{"$exists": false},
		"expiresAt":      bson.M{"$gt": now},
		"failedAttempts": bson.M{"$lt": registrationVerificationMaxAttempts},
	}
	update := bson.M{"$set": bson.M{"consumedAt": now}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc registrationVerificationChallengeDoc
	if err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return RegistrationVerificationChallenge{}, ErrRegistrationVerificationInvalid
		}
		return RegistrationVerificationChallenge{}, err
	}
	return toRegistrationVerificationChallenge(doc), nil
}
