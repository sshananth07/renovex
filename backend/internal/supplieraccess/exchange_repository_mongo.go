package supplieraccess

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionSupplierAccessExchanges = "supplier_access_exchanges"
	indexExchangeTokenHash            = "uq_supplier_access_exchanges_token_hash"
	indexExchangeExpiresAt            = "ttl_supplier_access_exchanges_expires_at"
)

var canonicalSHA256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// MongoAccessExchangeRepository persists the ten-minute opaque browser
// exchange. Lookups intentionally begin with only a globally unique hash.
type MongoAccessExchangeRepository struct {
	collection *mongo.Collection
}

func NewMongoAccessExchangeRepository(db *mongo.Database) *MongoAccessExchangeRepository {
	return &MongoAccessExchangeRepository{
		collection: db.Collection(collectionSupplierAccessExchanges),
	}
}

// EnsureIndexes establishes global token resolution and housekeeping expiry.
// The expiry predicate in every read remains authoritative because TTL cleanup
// is asynchronous.
func (r *MongoAccessExchangeRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "exchangeTokenHash", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetName(indexExchangeTokenHash),
		},
		{
			Keys: bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().
				SetExpireAfterSeconds(0).
				SetName(indexExchangeExpiresAt),
		},
	})
	return err
}

type accessExchangeDoc struct {
	ID                       string     `bson:"_id"`
	ExchangeTokenHash        string     `bson:"exchangeTokenHash"`
	CompanyID                string     `bson:"companyId"`
	SupplierID               string     `bson:"supplierId"`
	InvitationID             string     `bson:"invitationId"`
	AccessGeneration         int64      `bson:"accessGeneration"`
	NormalizedRecipientEmail string     `bson:"normalizedRecipientEmail"`
	CreatedAt                time.Time  `bson:"createdAt"`
	ExpiresAt                time.Time  `bson:"expiresAt"`
	ConsumedAt               *time.Time `bson:"consumedAt,omitempty"`
	ChallengeID              *string    `bson:"challengeId,omitempty"`
}

func toAccessExchangeDoc(exchange SupplierAccessExchange) accessExchangeDoc {
	return accessExchangeDoc{
		ID:                       exchange.ID,
		ExchangeTokenHash:        exchange.ExchangeTokenHash,
		CompanyID:                exchange.CompanyID,
		SupplierID:               exchange.SupplierID,
		InvitationID:             exchange.InvitationID,
		AccessGeneration:         exchange.AccessGeneration,
		NormalizedRecipientEmail: exchange.NormalizedRecipientEmail,
		CreatedAt:                exchange.CreatedAt,
		ExpiresAt:                exchange.ExpiresAt,
		ConsumedAt:               exchange.ConsumedAt,
		ChallengeID:              exchange.ChallengeID,
	}
}

func fromAccessExchangeDoc(doc accessExchangeDoc) SupplierAccessExchange {
	return SupplierAccessExchange{
		ID:                       doc.ID,
		ExchangeTokenHash:        doc.ExchangeTokenHash,
		CompanyID:                doc.CompanyID,
		SupplierID:               doc.SupplierID,
		InvitationID:             doc.InvitationID,
		AccessGeneration:         doc.AccessGeneration,
		NormalizedRecipientEmail: doc.NormalizedRecipientEmail,
		CreatedAt:                doc.CreatedAt,
		ExpiresAt:                doc.ExpiresAt,
		ConsumedAt:               doc.ConsumedAt,
		ChallengeID:              doc.ChallengeID,
	}
}

func validateAccessExchange(exchange SupplierAccessExchange) error {
	if exchange.ID == "" || !canonicalSHA256Hex.MatchString(exchange.ExchangeTokenHash) ||
		exchange.CompanyID == "" || exchange.SupplierID == "" ||
		exchange.InvitationID == "" || exchange.AccessGeneration < 1 ||
		exchange.NormalizedRecipientEmail == "" || exchange.CreatedAt.IsZero() ||
		!exchange.ExpiresAt.After(exchange.CreatedAt) {
		return ErrInvalidAccessExchange
	}
	return nil
}

// DeleteAllForCompany permanently removes every SupplierAccessExchange owned
// by companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoAccessExchangeRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// CreateExchange inserts an already-generated opaque ID and token hash. The raw
// token is absent from the method and document types, making persistence leaks
// structurally impossible.
func (r *MongoAccessExchangeRepository) CreateExchange(ctx context.Context,
	exchange SupplierAccessExchange) (SupplierAccessExchange, error) {

	if err := validateAccessExchange(exchange); err != nil {
		return SupplierAccessExchange{}, err
	}
	_, err := r.collection.InsertOne(ctx, toAccessExchangeDoc(exchange))
	if err == nil {
		return exchange, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return SupplierAccessExchange{}, err
	}
	switch {
	case strings.Contains(err.Error(), indexExchangeTokenHash):
		return SupplierAccessExchange{}, ErrExchangeTokenHashCollision
	case strings.Contains(err.Error(), "_id_"):
		return SupplierAccessExchange{}, ErrAccessExchangeAlreadyExists
	default:
		// An unknown future unique index must not be mislabeled as an
		// idempotent collision.
		return SupplierAccessExchange{}, err
	}
}

// FindUsableByTokenHash performs global credential resolution and enforces the
// exact expiry boundary in MongoDB: accessedAt == ExpiresAt is expired.
func (r *MongoAccessExchangeRepository) FindUsableByTokenHash(ctx context.Context,
	tokenHash string, accessedAt time.Time) (SupplierAccessExchange, error) {

	if !canonicalSHA256Hex.MatchString(tokenHash) || accessedAt.IsZero() {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	var doc accessExchangeDoc
	err := r.collection.FindOne(ctx, bson.M{
		"exchangeTokenHash": tokenHash,
		"expiresAt":         bson.M{"$gt": accessedAt},
		"consumedAt":        bson.M{"$exists": false},
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	if err != nil {
		return SupplierAccessExchange{}, err
	}
	return fromAccessExchangeDoc(doc), nil
}

// FindExchangeByTokenHash also returns a consumed exchange so a retry can
// finish a challenge that was persisted before the exchange-consumption write.
// The service still enforces ExpiresAt and validates the recorded ChallengeID.
func (r *MongoAccessExchangeRepository) FindExchangeByTokenHash(
	ctx context.Context, tokenHash string) (SupplierAccessExchange, error) {

	if !canonicalSHA256Hex.MatchString(tokenHash) {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	var doc accessExchangeDoc
	err := r.collection.FindOne(ctx,
		bson.M{"exchangeTokenHash": tokenHash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	if err != nil {
		return SupplierAccessExchange{}, err
	}
	return fromAccessExchangeDoc(doc), nil
}

// ConsumeForChallenge atomically assigns an exchange to exactly one challenge.
// A retry for that same challenge adopts the winning result without changing
// ConsumedAt; a different challenge cannot steal the exchange.
func (r *MongoAccessExchangeRepository) ConsumeForChallenge(ctx context.Context,
	exchangeID, challengeID string, consumedAt time.Time) (SupplierAccessExchange, error) {

	if exchangeID == "" || challengeID == "" || consumedAt.IsZero() {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	var doc accessExchangeDoc
	err := r.collection.FindOneAndUpdate(ctx, bson.M{
		"_id":         exchangeID,
		"expiresAt":   bson.M{"$gt": consumedAt},
		"consumedAt":  bson.M{"$exists": false},
		"challengeId": bson.M{"$exists": false},
	}, bson.M{"$set": bson.M{
		"consumedAt":  consumedAt,
		"challengeId": challengeID,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if err == nil {
		return fromAccessExchangeDoc(doc), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierAccessExchange{}, err
	}

	// The failed CAS may be an idempotent retry. Reloading is safe because the
	// stored challenge ID, not the caller's operation ID, is authoritative.
	err = r.collection.FindOne(ctx, bson.M{"_id": exchangeID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	if err != nil {
		return SupplierAccessExchange{}, err
	}
	if doc.ChallengeID != nil && *doc.ChallengeID == challengeID && doc.ConsumedAt != nil {
		// This is a read-only adoption of the transition already committed
		// before expiry. It does not make an expired exchange usable for a new
		// challenge.
		return fromAccessExchangeDoc(doc), nil
	}
	if !doc.ExpiresAt.After(consumedAt) {
		return SupplierAccessExchange{}, ErrAccessExchangeNotFound
	}
	return SupplierAccessExchange{}, ErrAccessExchangeConsumed
}
