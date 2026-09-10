package projects

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// MongoProjectRepository is the MongoDB-backed ProjectRepository implementation. It
// owns the "projects" collection exclusively.
type MongoProjectRepository struct {
	collection *mongo.Collection
}

// NewMongoProjectRepository constructs a MongoProjectRepository against db's
// "projects" collection.
func NewMongoProjectRepository(db *mongo.Database) *MongoProjectRepository {
	return &MongoProjectRepository{collection: db.Collection("projects")}
}

// EnsureIndexes creates the companyId index, the companyId+clientId compound
// index required by the clientId filter, the companyId+createdAt+_id compound
// index matching the default list query/sort, and the companyId+clientId+
// createdAt+_id compound index matching the default query/sort when also
// filtered by clientId.
func (r *MongoProjectRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "clientId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "clientId", Value: 1},
			{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1},
		}},
	})
	return err
}

type projectDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ClientID      string        `bson:"clientId"`
	Name          string        `bson:"name"`
	Status        string        `bson:"status"`
	ScopeBrief    string        `bson:"scopeBrief"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoProjectRepository) Create(ctx context.Context, p Project) (Project, error) {
	doc := toProjectDoc(p)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Project{}, err
	}
	p.ID = res.InsertedID.(bson.ObjectID).Hex()
	return p, nil
}

func (r *MongoProjectRepository) FindByID(ctx context.Context, companyID, id string) (Project, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Project{}, ErrProjectNotFound
	}
	var doc projectDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, err
	}
	return fromProjectDoc(doc), nil
}

// ListPaginated builds one filter — tenant scope, optional clientId, and an
// optional case-insensitive regex search on name — and uses that SAME
// filter for both CountDocuments and the sorted, paginated Find.
func (r *MongoProjectRepository) ListPaginated(ctx context.Context, companyID, clientID string, req pagination.Request) ([]Project, int, error) {
	filter := bson.M{"companyId": companyID}
	if clientID != "" {
		filter["clientId"] = clientID
	}
	if pattern := req.SearchRegexPattern(); pattern != "" {
		filter["name"] = bson.M{"$regex": pattern, "$options": "i"}
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

	var docs []projectDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	result := make([]Project, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromProjectDoc(doc))
	}
	return result, int(total), nil
}

// DeleteAllForCompany permanently removes every Project owned by companyID.
// Never errors when zero documents match — repeatable by design.
// Development-tool use only. Deliberately NOT part of the ProjectRepository
// interface — see clients.MongoClientRepository.DeleteAllForCompany's doc
// comment for why.
func (r *MongoProjectRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoProjectRepository) UpdateStatus(ctx context.Context, companyID, id string, status ProjectStatus) (Project, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Project{}, ErrProjectNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"status": string(status)}},
	)
	if err != nil {
		return Project{}, err
	}
	if res.MatchedCount == 0 {
		return Project{}, ErrProjectNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoProjectRepository) UpdateName(ctx context.Context, companyID, id, name string) (Project, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Project{}, ErrProjectNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"name": name}},
	)
	if err != nil {
		return Project{}, err
	}
	if res.MatchedCount == 0 {
		return Project{}, ErrProjectNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

// UpdateStatusIfCurrent performs the monotonic compare-and-set described in
// the M6 design spec §8: a single conditional update matching the stored
// status against eligibleFrom. When no document matches, it distinguishes
// "project does not exist" (ErrProjectNotFound) from "project exists but is
// already at or past the target" (changed=false, no error) with one
// follow-up read.
func (r *MongoProjectRepository) UpdateScopeBrief(ctx context.Context, companyID, id, scopeBrief string) (Project, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Project{}, ErrProjectNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"scopeBrief": scopeBrief}},
	)
	if err != nil {
		return Project{}, err
	}
	if res.MatchedCount == 0 {
		return Project{}, ErrProjectNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoProjectRepository) UpdateStatusIfCurrent(ctx context.Context, companyID, id string, eligibleFrom []ProjectStatus, target ProjectStatus) (Project, bool, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Project{}, false, ErrProjectNotFound
	}

	eligible := make([]string, len(eligibleFrom))
	for i, s := range eligibleFrom {
		eligible[i] = string(s)
	}

	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": bson.M{"$in": eligible}},
		bson.M{"$set": bson.M{"status": string(target)}},
	)
	if err != nil {
		return Project{}, false, err
	}
	if res.MatchedCount == 0 {
		// Either the project does not exist for this tenant, or its status is
		// not eligible (already at/past the target — a legitimate no-op).
		current, findErr := r.FindByID(ctx, companyID, id)
		if findErr != nil {
			return Project{}, false, findErr
		}
		return current, false, nil
	}

	updated, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Project{}, false, err
	}
	return updated, true, nil
}

func toProjectDoc(p Project) projectDoc {
	doc := projectDoc{
		CompanyID: p.CompanyID, ClientID: p.ClientID, Name: p.Name,
		Status: string(p.Status), ScopeBrief: p.ScopeBrief,
		CreatedAt: p.CreatedAt, SchemaVersion: p.SchemaVersion,
	}
	if p.ID != "" {
		objID, _ := bson.ObjectIDFromHex(p.ID)
		doc.ID = objID
	}
	return doc
}

func fromProjectDoc(doc projectDoc) Project {
	return Project{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ClientID: doc.ClientID, Name: doc.Name,
		Status: ProjectStatus(doc.Status), ScopeBrief: doc.ScopeBrief,
		CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
