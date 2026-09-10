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
	collectionVerificationRateLimits = "supplier_verification_rate_limits"
	indexVerificationRateScope       = "uq_supplier_verification_rate_limits_scope_hash"
	indexVerificationRateExpiry      = "ttl_supplier_verification_rate_limits_expires_at"
)

type MongoVerificationRateLimitRepository struct {
	collection *mongo.Collection
}

func NewMongoVerificationRateLimitRepository(
	db *mongo.Database) *MongoVerificationRateLimitRepository {
	return &MongoVerificationRateLimitRepository{
		collection: db.Collection(collectionVerificationRateLimits),
	}
}

func (r *MongoVerificationRateLimitRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "scope", Value: 1},
				{Key: "scopeKeyHash", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName(indexVerificationRateScope),
		},
		{
			Keys: bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().
				SetExpireAfterSeconds(0).
				SetName(indexVerificationRateExpiry),
		},
	})
	return err
}

type verificationRateReservationDoc struct {
	ReservationID string    `bson:"reservationId"`
	RequestedAt   time.Time `bson:"requestedAt"`
}

type verificationRateLimitDoc struct {
	ID           string                           `bson:"_id"`
	Scope        string                           `bson:"scope"`
	ScopeKeyHash string                           `bson:"scopeKeyHash"`
	Reservations []verificationRateReservationDoc `bson:"reservations"`
	Revision     int64                            `bson:"revision"`
	UpdatedAt    time.Time                        `bson:"updatedAt"`
	ExpiresAt    time.Time                        `bson:"expiresAt"`
}

func toVerificationRateLimitDoc(
	state VerificationRateLimitState) verificationRateLimitDoc {
	reservations := make([]verificationRateReservationDoc, 0, len(state.Reservations))
	for _, reservation := range state.Reservations {
		reservations = append(reservations, verificationRateReservationDoc{
			ReservationID: reservation.ReservationID,
			RequestedAt:   reservation.RequestedAt,
		})
	}
	return verificationRateLimitDoc{
		ID: state.ID, Scope: string(state.Scope), ScopeKeyHash: state.ScopeKeyHash,
		Reservations: reservations, Revision: state.Revision,
		UpdatedAt: state.UpdatedAt, ExpiresAt: state.ExpiresAt,
	}
}

func fromVerificationRateLimitDoc(
	doc verificationRateLimitDoc) VerificationRateLimitState {
	reservations := make([]VerificationRateReservation, 0, len(doc.Reservations))
	for _, reservation := range doc.Reservations {
		reservations = append(reservations, VerificationRateReservation{
			ReservationID: reservation.ReservationID,
			RequestedAt:   reservation.RequestedAt,
		})
	}
	return VerificationRateLimitState{
		ID: doc.ID, Scope: VerificationRateLimitScope(doc.Scope),
		ScopeKeyHash: doc.ScopeKeyHash, Reservations: reservations,
		Revision: doc.Revision, UpdatedAt: doc.UpdatedAt, ExpiresAt: doc.ExpiresAt,
	}
}

func validateVerificationRateLimitState(state VerificationRateLimitState) error {
	limit := 0
	switch state.Scope {
	case VerificationRateScopeIdentity, VerificationRateScopeResend:
		limit = 5
	case VerificationRateScopeClient:
		limit = 20
	default:
		return ErrInvalidVerificationRateLimitState
	}
	if state.ID == "" || !canonicalSHA256Hex.MatchString(state.ScopeKeyHash) ||
		state.Revision < 1 || state.UpdatedAt.IsZero() ||
		!state.ExpiresAt.After(state.UpdatedAt) ||
		len(state.Reservations) == 0 || len(state.Reservations) > limit {
		return ErrInvalidVerificationRateLimitState
	}
	seen := make(map[string]struct{}, len(state.Reservations))
	for _, reservation := range state.Reservations {
		if reservation.ReservationID == "" || reservation.RequestedAt.IsZero() {
			return ErrInvalidVerificationRateLimitState
		}
		if _, duplicate := seen[reservation.ReservationID]; duplicate {
			return ErrInvalidVerificationRateLimitState
		}
		seen[reservation.ReservationID] = struct{}{}
	}
	return nil
}

// DeleteByScopeAndHash permanently removes the single rate-limit document
// identified by scope and scopeKeyHash, if any. Never errors when it does not
// exist. Development-tool use only.
//
// This collection has no companyId field — every document is keyed only by
// an irreversible HMAC hash over its scope's identity inputs (see
// VerificationRateFingerprinter), so it cannot be bulk-deleted by companyID
// like every other collection in this module. Cleanup instead re-derives the
// exact hash for each identity it already knows it created (see
// Service.DeleteAllForCompany) and deletes by that hash directly.
func (r *MongoVerificationRateLimitRepository) DeleteByScopeAndHash(ctx context.Context,
	scope VerificationRateLimitScope, scopeKeyHash string) error {

	_, err := r.collection.DeleteOne(ctx, bson.M{
		"scope": string(scope), "scopeKeyHash": scopeKeyHash,
	})
	return err
}

func (r *MongoVerificationRateLimitRepository) CreateState(ctx context.Context,
	state VerificationRateLimitState) (VerificationRateLimitState, error) {

	if err := validateVerificationRateLimitState(state); err != nil {
		return VerificationRateLimitState{}, err
	}
	_, err := r.collection.InsertOne(ctx, toVerificationRateLimitDoc(state))
	if err == nil {
		return state, nil
	}
	if mongo.IsDuplicateKeyError(err) &&
		strings.Contains(err.Error(), indexVerificationRateScope) {
		return VerificationRateLimitState{},
			ErrVerificationRateLimitStateAlreadyExists
	}
	return VerificationRateLimitState{}, err
}

func (r *MongoVerificationRateLimitRepository) FindState(ctx context.Context,
	scope VerificationRateLimitScope,
	scopeKeyHash string) (VerificationRateLimitState, error) {

	var doc verificationRateLimitDoc
	err := r.collection.FindOne(ctx, bson.M{
		"scope": string(scope), "scopeKeyHash": scopeKeyHash,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return VerificationRateLimitState{}, ErrVerificationRateLimitStateNotFound
	}
	if err != nil {
		return VerificationRateLimitState{}, err
	}
	return fromVerificationRateLimitDoc(doc), nil
}

// ReplaceStateCAS replaces the complete bounded reservation window while
// fencing on Revision. This is the primitive the rate limiter retries.
func (r *MongoVerificationRateLimitRepository) ReplaceStateCAS(ctx context.Context,
	state VerificationRateLimitState,
	expectedRevision int64) (VerificationRateLimitState, error) {

	if expectedRevision < 1 || state.Revision != expectedRevision+1 {
		return VerificationRateLimitState{}, ErrInvalidVerificationRateLimitState
	}
	if err := validateVerificationRateLimitState(state); err != nil {
		return VerificationRateLimitState{}, err
	}
	doc := toVerificationRateLimitDoc(state)
	var updated verificationRateLimitDoc
	err := r.collection.FindOneAndUpdate(ctx, bson.M{
		"_id": state.ID, "scope": string(state.Scope),
		"scopeKeyHash": state.ScopeKeyHash, "revision": expectedRevision,
	}, bson.M{"$set": bson.M{
		"reservations": doc.Reservations,
		"revision":     doc.Revision,
		"updatedAt":    doc.UpdatedAt,
		"expiresAt":    doc.ExpiresAt,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return VerificationRateLimitState{}, ErrVerificationRateLimitStateConflict
	}
	if err != nil {
		return VerificationRateLimitState{}, err
	}
	return fromVerificationRateLimitDoc(updated), nil
}
