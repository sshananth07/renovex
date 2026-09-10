package identity

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoUserRepository is the MongoDB-backed UserRepository implementation.
// It owns the "users" collection exclusively.
type MongoUserRepository struct {
	collection *mongo.Collection
}

// NewMongoUserRepository constructs a MongoUserRepository against db's "users"
// collection. Callers should call EnsureIndexes once at startup.
func NewMongoUserRepository(db *mongo.Database) *MongoUserRepository {
	return &MongoUserRepository{collection: db.Collection("users")}
}

// EnsureIndexes creates the unique index on email required by the tenancy design.
func (r *MongoUserRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

type userDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	Email              string        `bson:"email"`
	PasswordHash       string        `bson:"passwordHash"`
	MustChangePassword bool          `bson:"mustChangePassword"`
	CreatedAt          time.Time     `bson:"createdAt"`
}

func (r *MongoUserRepository) Create(ctx context.Context, u User) (User, error) {
	doc := userDoc{
		Email:              u.Email,
		PasswordHash:       u.PasswordHash,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		return User{}, ErrDuplicateEmail
	}
	if err != nil {
		return User{}, err
	}
	u.ID = res.InsertedID.(bson.ObjectID).Hex()
	return u, nil
}

func (r *MongoUserRepository) FindByEmail(ctx context.Context, normalizedEmail string) (User, error) {
	var doc userDoc
	err := r.collection.FindOne(ctx, bson.M{"email": normalizedEmail}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return toUser(doc), nil
}

func (r *MongoUserRepository) FindByID(ctx context.Context, id string) (User, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return User{}, ErrUserNotFound
	}
	var doc userDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return toUser(doc), nil
}

func (r *MongoUserRepository) Delete(ctx context.Context, id string) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrUserNotFound
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": objID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

func toUser(doc userDoc) User {
	return User{
		ID:                 doc.ID.Hex(),
		Email:              doc.Email,
		PasswordHash:       doc.PasswordHash,
		MustChangePassword: doc.MustChangePassword,
		CreatedAt:          doc.CreatedAt,
	}
}
