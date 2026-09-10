package materials

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MongoMaterialRepository is the MongoDB-backed MaterialRepository
// implementation. It owns the "materials" collection exclusively.
type MongoMaterialRepository struct {
	collection *mongo.Collection
}

// NewMongoMaterialRepository constructs a MongoMaterialRepository against db's
// "materials" collection.
func NewMongoMaterialRepository(db *mongo.Database) *MongoMaterialRepository {
	return &MongoMaterialRepository{collection: db.Collection("materials")}
}

// EnsureIndexes creates the companyId index, the companyId+category
// compound index (design spec §13), and a unique companyId+sourceSuggestionId
// index restricted by partialFilterExpression to documents where the field
// exists — the AI-acceptance idempotency anchor (M8.5B-A design doc §18.2).
// A bare sparse compound index is NOT sufficient: MongoDB only skips a
// sparse compound index entry when EVERY indexed field is absent, but
// companyId is always present, so a plain sparse index would still collide
// once two manual Materials both lack the field.
func (r *MongoMaterialRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "category", Value: 1}}},
		{
			Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sourceSuggestionId", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(
				bson.D{{Key: "sourceSuggestionId", Value: bson.D{{Key: "$exists", Value: true}}}},
			),
		},
	})
	return err
}

type materialDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	Name               string        `bson:"name"`
	Category           string        `bson:"category,omitempty"`
	Specification      string        `bson:"specification,omitempty"`
	Unit               string        `bson:"unit"`
	ReferencePrice     money.Money   `bson:"referencePrice"`
	ReferencePriceAsOf time.Time     `bson:"referencePriceAsOf"`
	SourceSuggestionID *string       `bson:"sourceSuggestionId,omitempty"`
	CreatedAt          time.Time     `bson:"createdAt"`
	SchemaVersion      int           `bson:"schemaVersion"`
}

func (r *MongoMaterialRepository) Create(ctx context.Context, m Material) (Material, error) {
	doc := toMaterialDoc(m)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Material{}, err
	}
	m.ID = res.InsertedID.(bson.ObjectID).Hex()
	return m, nil
}

func (r *MongoMaterialRepository) FindByID(ctx context.Context, companyID, id string) (Material, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Material{}, ErrMaterialNotFound
	}
	var doc materialDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Material{}, ErrMaterialNotFound
	}
	if err != nil {
		return Material{}, err
	}
	return fromMaterialDoc(doc), nil
}

func (r *MongoMaterialRepository) List(ctx context.Context, companyID string) ([]Material, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []materialDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]Material, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromMaterialDoc(doc))
	}
	return result, nil
}

// DeleteAllForCompany permanently removes every Material owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoMaterialRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoMaterialRepository) Update(ctx context.Context, companyID, id string, fn func(*Material)) (Material, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Material{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id) // already validated by FindByID above
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{
			"name": existing.Name, "category": existing.Category, "specification": existing.Specification,
			"unit": existing.Unit, "referencePrice": existing.ReferencePrice, "referencePriceAsOf": existing.ReferencePriceAsOf,
		}},
	)
	if err != nil {
		return Material{}, err
	}
	if res.MatchedCount == 0 {
		return Material{}, ErrMaterialNotFound
	}
	return existing, nil
}

func (r *MongoMaterialRepository) FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (Material, error) {
	var doc materialDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "sourceSuggestionId": sourceSuggestionID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Material{}, ErrMaterialNotFound
	}
	if err != nil {
		return Material{}, err
	}
	return fromMaterialDoc(doc), nil
}

func toMaterialDoc(m Material) materialDoc {
	doc := materialDoc{
		CompanyID: m.CompanyID, Name: m.Name, Category: m.Category, Specification: m.Specification,
		Unit: m.Unit, ReferencePrice: m.ReferencePrice, ReferencePriceAsOf: m.ReferencePriceAsOf,
		SourceSuggestionID: m.SourceSuggestionID,
		CreatedAt:          m.CreatedAt, SchemaVersion: m.SchemaVersion,
	}
	if m.ID != "" {
		objID, _ := bson.ObjectIDFromHex(m.ID)
		doc.ID = objID
	}
	return doc
}

func fromMaterialDoc(doc materialDoc) Material {
	return Material{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, Name: doc.Name, Category: doc.Category,
		Specification: doc.Specification, Unit: doc.Unit, ReferencePrice: doc.ReferencePrice,
		ReferencePriceAsOf: doc.ReferencePriceAsOf, SourceSuggestionID: doc.SourceSuggestionID,
		CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
