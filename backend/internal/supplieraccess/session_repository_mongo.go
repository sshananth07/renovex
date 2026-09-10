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
	collectionSupplierSessions    = "supplier_sessions"
	indexSupplierSessionTokenHash = "uq_supplier_sessions_token_hash"
	indexSupplierSessionChallenge = "uq_supplier_sessions_created_challenge"
)

type MongoSupplierSessionRepository struct {
	collection *mongo.Collection
}

func NewMongoSupplierSessionRepository(db *mongo.Database) *MongoSupplierSessionRepository {
	return &MongoSupplierSessionRepository{
		collection: db.Collection(collectionSupplierSessions),
	}
}

func (r *MongoSupplierSessionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "tokenHash", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexSupplierSessionTokenHash),
		},
		{
			Keys: bson.D{{Key: "createdFromChallengeId", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetName(indexSupplierSessionChallenge).
				SetPartialFilterExpression(bson.M{
					"createdFromChallengeId": bson.M{"$type": "string", "$gt": ""},
				}),
		},
	})
	return err
}

type supplierSessionDoc struct {
	ID                       string     `bson:"_id"`
	CompanyID                string     `bson:"companyId"`
	SupplierID               string     `bson:"supplierId"`
	RecipientEmailNormalized string     `bson:"recipientEmailNormalized"`
	TokenHash                string     `bson:"tokenHash"`
	TokenGeneration          int64      `bson:"tokenGeneration"`
	TokenKeyVersion          int        `bson:"tokenKeyVersion"`
	CreatedFromChallengeID   string     `bson:"createdFromChallengeId"`
	LastVerifiedChallengeID  string     `bson:"lastVerifiedChallengeId"`
	LastVerifiedAt           time.Time  `bson:"lastVerifiedAt"`
	SlidingExpiresAt         time.Time  `bson:"slidingExpiresAt"`
	AbsoluteExpiresAt        time.Time  `bson:"absoluteExpiresAt"`
	LastUsedAt               time.Time  `bson:"lastUsedAt"`
	RevokedAt                *time.Time `bson:"revokedAt,omitempty"`
	Revision                 int64      `bson:"revision"`
	SchemaVersion            int        `bson:"schemaVersion"`
}

func toSupplierSessionDoc(session SupplierSession) supplierSessionDoc {
	return supplierSessionDoc{
		ID: session.ID, CompanyID: session.CompanyID, SupplierID: session.SupplierID,
		RecipientEmailNormalized: session.RecipientEmailNormalized,
		TokenHash:                session.TokenHash,
		TokenGeneration:          session.TokenGeneration,
		TokenKeyVersion:          session.TokenKeyVersion,
		CreatedFromChallengeID:   session.CreatedFromChallengeID,
		LastVerifiedChallengeID:  session.LastVerifiedChallengeID,
		LastVerifiedAt:           session.LastVerifiedAt,
		SlidingExpiresAt:         session.SlidingExpiresAt,
		AbsoluteExpiresAt:        session.AbsoluteExpiresAt,
		LastUsedAt:               session.LastUsedAt,
		RevokedAt:                session.RevokedAt,
		Revision:                 session.Revision,
		SchemaVersion:            session.SchemaVersion,
	}
}

func fromSupplierSessionDoc(doc supplierSessionDoc) SupplierSession {
	return SupplierSession{
		ID: doc.ID, CompanyID: doc.CompanyID, SupplierID: doc.SupplierID,
		RecipientEmailNormalized: doc.RecipientEmailNormalized,
		TokenHash:                doc.TokenHash,
		TokenGeneration:          doc.TokenGeneration,
		TokenKeyVersion:          doc.TokenKeyVersion,
		CreatedFromChallengeID:   doc.CreatedFromChallengeID,
		LastVerifiedChallengeID:  doc.LastVerifiedChallengeID,
		LastVerifiedAt:           doc.LastVerifiedAt,
		SlidingExpiresAt:         doc.SlidingExpiresAt,
		AbsoluteExpiresAt:        doc.AbsoluteExpiresAt,
		LastUsedAt:               doc.LastUsedAt,
		RevokedAt:                doc.RevokedAt,
		Revision:                 doc.Revision,
		SchemaVersion:            doc.SchemaVersion,
	}
}

func validateSupplierSession(session SupplierSession) error {
	if session.ID == "" || session.CompanyID == "" || session.SupplierID == "" ||
		session.RecipientEmailNormalized == "" ||
		!canonicalSHA256Hex.MatchString(session.TokenHash) ||
		session.TokenGeneration < 1 || session.TokenKeyVersion < 1 ||
		session.CreatedFromChallengeID == "" ||
		session.LastVerifiedChallengeID == "" ||
		session.LastVerifiedAt.IsZero() || session.LastUsedAt.IsZero() ||
		!session.SlidingExpiresAt.After(session.LastUsedAt) ||
		session.AbsoluteExpiresAt.Before(session.SlidingExpiresAt) ||
		session.Revision < 1 || session.SchemaVersion < 1 {
		return ErrInvalidSupplierSession
	}
	return nil
}

func classifySupplierSessionDuplicate(err error) error {
	switch {
	case strings.Contains(err.Error(), indexSupplierSessionTokenHash):
		return ErrSupplierSessionTokenHashCollision
	case strings.Contains(err.Error(), indexSupplierSessionChallenge):
		return ErrSupplierSessionChallengeAlreadyUsed
	case strings.Contains(err.Error(), "_id_"):
		return ErrSupplierSessionAlreadyExists
	default:
		return err
	}
}

// DeleteAllForCompany permanently removes every SupplierSession owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoSupplierSessionRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoSupplierSessionRepository) CreateSession(ctx context.Context,
	session SupplierSession) (SupplierSession, error) {

	if err := validateSupplierSession(session); err != nil {
		return SupplierSession{}, err
	}
	_, err := r.collection.InsertOne(ctx, toSupplierSessionDoc(session))
	if err == nil {
		return session, nil
	}
	if mongo.IsDuplicateKeyError(err) {
		return SupplierSession{}, classifySupplierSessionDuplicate(err)
	}
	return SupplierSession{}, err
}

// FindSessionByTokenHash is global because the browser presents no trusted
// tenant identity. The authentication service performs constant-time hash and
// expiry checks after loading.
func (r *MongoSupplierSessionRepository) FindSessionByTokenHash(ctx context.Context,
	tokenHash string) (SupplierSession, error) {

	if !canonicalSHA256Hex.MatchString(tokenHash) {
		return SupplierSession{}, ErrSupplierSessionNotFound
	}
	var doc supplierSessionDoc
	err := r.collection.FindOne(ctx, bson.M{"tokenHash": tokenHash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierSession{}, ErrSupplierSessionNotFound
	}
	if err != nil {
		return SupplierSession{}, err
	}
	return fromSupplierSessionDoc(doc), nil
}

func (r *MongoSupplierSessionRepository) FindSession(ctx context.Context,
	companyID, sessionID string) (SupplierSession, error) {

	var doc supplierSessionDoc
	err := r.collection.FindOne(ctx,
		bson.M{"_id": sessionID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierSession{}, ErrSupplierSessionNotFound
	}
	if err != nil {
		return SupplierSession{}, err
	}
	return fromSupplierSessionDoc(doc), nil
}

// ReplaceSessionCAS updates only mutable credential/lifetime projections while
// matching immutable identity and creation provenance.
func (r *MongoSupplierSessionRepository) ReplaceSessionCAS(ctx context.Context,
	session SupplierSession, expectedRevision int64) (SupplierSession, error) {

	if expectedRevision < 1 || session.Revision != expectedRevision+1 {
		return SupplierSession{}, ErrInvalidSupplierSession
	}
	if err := validateSupplierSession(session); err != nil {
		return SupplierSession{}, err
	}
	doc := toSupplierSessionDoc(session)
	var updated supplierSessionDoc
	err := r.collection.FindOneAndUpdate(ctx, bson.M{
		"_id": session.ID, "companyId": session.CompanyID,
		"supplierId":               session.SupplierID,
		"recipientEmailNormalized": session.RecipientEmailNormalized,
		"createdFromChallengeId":   session.CreatedFromChallengeID,
		"revision":                 expectedRevision,
	}, bson.M{"$set": bson.M{
		"tokenHash":               doc.TokenHash,
		"tokenGeneration":         doc.TokenGeneration,
		"tokenKeyVersion":         doc.TokenKeyVersion,
		"lastVerifiedChallengeId": doc.LastVerifiedChallengeID,
		"lastVerifiedAt":          doc.LastVerifiedAt,
		"slidingExpiresAt":        doc.SlidingExpiresAt,
		"absoluteExpiresAt":       doc.AbsoluteExpiresAt,
		"lastUsedAt":              doc.LastUsedAt,
		"revokedAt":               doc.RevokedAt,
		"revision":                doc.Revision,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updated)
	if err == nil {
		return fromSupplierSessionDoc(updated), nil
	}
	if mongo.IsDuplicateKeyError(err) {
		return SupplierSession{}, classifySupplierSessionDuplicate(err)
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierSession{}, ErrSupplierSessionConflict
	}
	return SupplierSession{}, err
}
