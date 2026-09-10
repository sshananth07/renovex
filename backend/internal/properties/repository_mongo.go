package properties

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoPropertyRepository is the MongoDB-backed PropertyRepository
// implementation. It owns the "properties" collection exclusively.
type MongoPropertyRepository struct {
	collection *mongo.Collection
}

// NewMongoPropertyRepository constructs a MongoPropertyRepository against db's
// "properties" collection.
func NewMongoPropertyRepository(db *mongo.Database) *MongoPropertyRepository {
	return &MongoPropertyRepository{collection: db.Collection("properties")}
}

// EnsureIndexes creates the companyId index and the UNIQUE {companyId, projectId}
// compound index that enforces "at most one Property per Project" (design spec §1.3,
// §11) — this is the one uniqueness constraint in Milestone 2.
func (r *MongoPropertyRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	return err
}

type propertyDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ProjectID     string        `bson:"projectId"`
	Address       string        `bson:"address"`
	PropertyType  string        `bson:"propertyType,omitempty"`
	Notes         string        `bson:"notes,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoPropertyRepository) Create(ctx context.Context, p Property) (Property, error) {
	doc := toPropertyDoc(p)
	res, err := r.collection.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		return Property{}, ErrProjectAlreadyHasProperty
	}
	if err != nil {
		return Property{}, err
	}
	p.ID = res.InsertedID.(bson.ObjectID).Hex()
	return p, nil
}

func (r *MongoPropertyRepository) FindByID(ctx context.Context, companyID, id string) (Property, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Property{}, ErrPropertyNotFound
	}
	var doc propertyDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Property{}, ErrPropertyNotFound
	}
	if err != nil {
		return Property{}, err
	}
	return fromPropertyDoc(doc), nil
}

func (r *MongoPropertyRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]Property, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []propertyDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]Property, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromPropertyDoc(doc))
	}
	return result, nil
}

// DeleteAllForCompany permanently removes every Property owned by
// companyID. Never errors when zero documents match — repeatable by
// design. Development-tool use only.
func (r *MongoPropertyRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoPropertyRepository) Update(ctx context.Context, companyID, id string, fn func(*Property)) (Property, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Property{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id) // already validated by FindByID above
	doc := toPropertyDoc(existing)
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": doc},
	)
	if err != nil {
		return Property{}, err
	}
	if res.MatchedCount == 0 {
		return Property{}, ErrPropertyNotFound
	}
	return existing, nil
}

func toPropertyDoc(p Property) propertyDoc {
	doc := propertyDoc{
		CompanyID: p.CompanyID, ProjectID: p.ProjectID, Address: p.Address,
		PropertyType: p.PropertyType, Notes: p.Notes, CreatedAt: p.CreatedAt, SchemaVersion: p.SchemaVersion,
	}
	if p.ID != "" {
		objID, _ := bson.ObjectIDFromHex(p.ID)
		doc.ID = objID
	}
	return doc
}

func fromPropertyDoc(doc propertyDoc) Property {
	return Property{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, Address: doc.Address,
		PropertyType: doc.PropertyType, Notes: doc.Notes, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
