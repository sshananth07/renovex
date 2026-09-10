package identity

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoAuthSessionRepository is the MongoDB-backed AuthSessionRepository
// implementation. It owns the "auth_sessions" collection exclusively.
type MongoAuthSessionRepository struct {
	collection *mongo.Collection
}

// NewMongoAuthSessionRepository constructs a MongoAuthSessionRepository against
// db's "auth_sessions" collection.
func NewMongoAuthSessionRepository(db *mongo.Database) *MongoAuthSessionRepository {
	return &MongoAuthSessionRepository{collection: db.Collection("auth_sessions")}
}

// EnsureIndexes creates the indexes required by the tenancy design: a unique
// index on refreshTokenHash for fast rotation lookups, a userId index, and an
// expiresAt index supporting future TTL-based cleanup.
func (r *MongoAuthSessionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "refreshTokenHash", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "userId", Value: 1}}},
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}},
	})
	return err
}

type authSessionDoc struct {
	ID               bson.ObjectID `bson:"_id,omitempty"`
	UserID           string        `bson:"userId"`
	RefreshTokenHash string        `bson:"refreshTokenHash"`
	CreatedAt        time.Time     `bson:"createdAt"`
	ExpiresAt        time.Time     `bson:"expiresAt"`
	LastUsedAt       time.Time     `bson:"lastUsedAt"`
	RevokedAt        *time.Time    `bson:"revokedAt,omitempty"`
}

func (r *MongoAuthSessionRepository) Create(ctx context.Context, s AuthSession) (AuthSession, error) {
	doc := authSessionDoc{
		UserID: s.UserID, RefreshTokenHash: s.RefreshTokenHash,
		CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt, LastUsedAt: s.LastUsedAt, RevokedAt: s.RevokedAt,
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return AuthSession{}, err
	}
	s.ID = res.InsertedID.(bson.ObjectID).Hex()
	return s, nil
}

func (r *MongoAuthSessionRepository) FindActiveByHash(ctx context.Context, refreshTokenHash string) (AuthSession, error) {
	var doc authSessionDoc
	err := r.collection.FindOne(ctx, bson.M{
		"refreshTokenHash": refreshTokenHash,
		"revokedAt":        bson.M{"$exists": false},
		"expiresAt":        bson.M{"$gt": time.Now()},
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AuthSession{}, ErrSessionNotFound
	}
	if err != nil {
		return AuthSession{}, err
	}
	return toAuthSession(doc), nil
}

func (r *MongoAuthSessionRepository) RotateHash(ctx context.Context, sessionID, newHash string, lastUsedAt time.Time) error {
	objID, err := bson.ObjectIDFromHex(sessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{"refreshTokenHash": newHash, "lastUsedAt": lastUsedAt}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *MongoAuthSessionRepository) Revoke(ctx context.Context, sessionID string, revokedAt time.Time) error {
	objID, err := bson.ObjectIDFromHex(sessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{"revokedAt": revokedAt}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// DeleteAllForUser permanently removes every AuthSession owned by userID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoAuthSessionRepository) DeleteAllForUser(ctx context.Context, userID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"userId": userID})
	return err
}

func toAuthSession(doc authSessionDoc) AuthSession {
	return AuthSession{
		ID: doc.ID.Hex(), UserID: doc.UserID, RefreshTokenHash: doc.RefreshTokenHash,
		CreatedAt: doc.CreatedAt, ExpiresAt: doc.ExpiresAt, LastUsedAt: doc.LastUsedAt, RevokedAt: doc.RevokedAt,
	}
}
