package spaces

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// MongoSpaceRepository is the MongoDB-backed SpaceRepository implementation. It
// owns the "spaces" collection exclusively.
type MongoSpaceRepository struct {
	collection *mongo.Collection
}

// NewMongoSpaceRepository constructs a MongoSpaceRepository against db's "spaces"
// collection.
func NewMongoSpaceRepository(db *mongo.Database) *MongoSpaceRepository {
	return &MongoSpaceRepository{collection: db.Collection("spaces")}
}

// EnsureIndexes creates the companyId index, the companyId+projectId
// compound index, a companyId+projectId+createdAt+_id compound index
// matching the default list query/sort (createdAt asc), and a unique
// companyId+sourceSuggestionId index restricted by partialFilterExpression
// to documents where sourceSuggestionId exists — the AI-acceptance
// idempotency anchor (M8.5B-A design doc §18.2). A plain `sparse` compound
// index is NOT sufficient here: MongoDB only skips a sparse compound index
// entry when EVERY indexed field is absent, but companyId is always present,
// so a bare sparse index would still index every manual Space's absent
// sourceSuggestionId as null and collide after the second one.
// partialFilterExpression correctly excludes any document lacking the field.
func (r *MongoSpaceRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1},
			{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1},
		}},
		{
			Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sourceSuggestionId", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(
				bson.D{{Key: "sourceSuggestionId", Value: bson.D{{Key: "$exists", Value: true}}}},
			),
		},
	})
	return err
}

type spaceDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	ProjectID          string        `bson:"projectId"`
	Name               string        `bson:"name"`
	Type               string        `bson:"type,omitempty"`
	Description        string        `bson:"description,omitempty"`
	SourceSuggestionID *string       `bson:"sourceSuggestionId,omitempty"`
	CreatedAt          time.Time     `bson:"createdAt"`
	SchemaVersion      int           `bson:"schemaVersion"`
}

func (r *MongoSpaceRepository) Create(ctx context.Context, s Space) (Space, error) {
	doc := toSpaceDoc(s)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Space{}, err
	}
	s.ID = res.InsertedID.(bson.ObjectID).Hex()
	return s, nil
}

func (r *MongoSpaceRepository) FindByID(ctx context.Context, companyID, id string) (Space, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Space{}, ErrSpaceNotFound
	}
	var doc spaceDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Space{}, ErrSpaceNotFound
	}
	if err != nil {
		return Space{}, err
	}
	return fromSpaceDoc(doc), nil
}

// ListPaginated builds one filter — tenant+project scope plus an optional
// case-insensitive regex search across name/type/description — and uses
// that SAME filter for both CountDocuments and the sorted, paginated Find.
func (r *MongoSpaceRepository) ListPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]Space, int, error) {
	filter := bson.M{"companyId": companyID, "projectId": projectID}
	if pattern := req.SearchRegexPattern(); pattern != "" {
		regex := bson.M{"$regex": pattern, "$options": "i"}
		filter["$or"] = bson.A{
			bson.M{"name": regex},
			bson.M{"type": regex},
			bson.M{"description": regex},
		}
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	cursor, err := r.collection.Find(ctx, filter,
		options.Find().
			SetSort(req.MongoSort()).
			SetSkip(int64(req.Offset())).
			SetLimit(int64(req.PageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []spaceDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	result := make([]Space, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromSpaceDoc(doc))
	}
	return result, int(total), nil
}

// DeleteAllForCompany permanently removes every Space owned by companyID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoSpaceRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoSpaceRepository) Update(ctx context.Context, companyID, id string, fn func(*Space)) (Space, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Space{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id) // already validated by FindByID above
	doc := toSpaceDoc(existing)
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": doc},
	)
	if err != nil {
		return Space{}, err
	}
	if res.MatchedCount == 0 {
		return Space{}, ErrSpaceNotFound
	}
	return existing, nil
}

func (r *MongoSpaceRepository) FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (Space, error) {
	var doc spaceDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "sourceSuggestionId": sourceSuggestionID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Space{}, ErrSpaceNotFound
	}
	if err != nil {
		return Space{}, err
	}
	return fromSpaceDoc(doc), nil
}

func (r *MongoSpaceRepository) BelongsToProject(ctx context.Context, companyID, id, projectID string) (bool, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return false, nil
	}
	count, err := r.collection.CountDocuments(ctx, bson.M{"_id": objID, "companyId": companyID, "projectId": projectID})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func toSpaceDoc(s Space) spaceDoc {
	doc := spaceDoc{
		CompanyID: s.CompanyID, ProjectID: s.ProjectID, Name: s.Name, Type: s.Type,
		Description: s.Description, SourceSuggestionID: s.SourceSuggestionID,
		CreatedAt: s.CreatedAt, SchemaVersion: s.SchemaVersion,
	}
	if s.ID != "" {
		objID, _ := bson.ObjectIDFromHex(s.ID)
		doc.ID = objID
	}
	return doc
}

func fromSpaceDoc(doc spaceDoc) Space {
	return Space{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, Name: doc.Name,
		Type: doc.Type, Description: doc.Description, SourceSuggestionID: doc.SourceSuggestionID,
		CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
