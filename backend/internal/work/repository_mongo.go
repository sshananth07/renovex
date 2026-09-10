package work

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// MongoWorkItemRepository is the MongoDB-backed WorkItemRepository
// implementation. It owns the "work_items" collection exclusively.
type MongoWorkItemRepository struct {
	collection *mongo.Collection
}

// NewMongoWorkItemRepository constructs a MongoWorkItemRepository against db's
// "work_items" collection.
func NewMongoWorkItemRepository(db *mongo.Database) *MongoWorkItemRepository {
	return &MongoWorkItemRepository{collection: db.Collection("work_items")}
}

func sparseIndexOptions() *options.IndexOptionsBuilder {
	return options.Index().SetSparse(true)
}

// EnsureIndexes creates the companyId index, the companyId+projectId compound
// index, a sparse companyId+spaceId compound index (most WorkItems have no
// spaceId — design spec §11), and companyId+projectId+createdAt+_id /
// companyId+spaceId+createdAt+_id compound indexes matching the default
// list query/sort (createdAt asc) for each scoping dimension.
func (r *MongoWorkItemRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "spaceId", Value: 1}}, Options: sparseIndexOptions()},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1},
			{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1},
		}},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1}, {Key: "spaceId", Value: 1},
				{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1},
			},
			Options: sparseIndexOptions(),
		},
		// Unique on companyId+sourceSuggestionId, restricted by
		// partialFilterExpression to documents where the field exists — the
		// AI-acceptance idempotency anchor (M8.5B-A design doc §18.2). A bare
		// sparse compound index is NOT sufficient: MongoDB only skips a
		// sparse compound index entry when EVERY indexed field is absent,
		// but companyId is always present, so a plain sparse index would
		// still collide once two manual WorkItems both lack the field.
		{
			Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sourceSuggestionId", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(
				bson.D{{Key: "sourceSuggestionId", Value: bson.D{{Key: "$exists", Value: true}}}},
			),
		},
	})
	return err
}

// quantityDoc is the explicit Mongo persistence representation for
// quantity.Quantity. shopspring/decimal@v1.4.0 (the pinned version) has no
// MarshalBSON/UnmarshalBSON — confirmed by inspecting the module source directly,
// no .go file in that package references BSON at all. The mongo-driver's default
// struct-reflection codec therefore cannot be trusted to round-trip
// decimal.Decimal correctly, so Value is stored as a string, never as BSON
// Decimal128 and never left to automatic reflection (design spec §1.5).
type quantityDoc struct {
	Value string `bson:"value"`
	Unit  string `bson:"unit"`
}

func toQuantityDoc(q quantity.Quantity) quantityDoc {
	return quantityDoc{Value: q.Value.String(), Unit: q.Unit}
}

func fromQuantityDoc(doc quantityDoc) (quantity.Quantity, error) {
	return quantity.New(doc.Value, doc.Unit)
}

type workItemDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	ProjectID          string        `bson:"projectId"`
	SpaceID            *string       `bson:"spaceId,omitempty"`
	Description        string        `bson:"description"`
	WorkType           string        `bson:"workType,omitempty"`
	Quantity           quantityDoc   `bson:"quantity"`
	Status             string        `bson:"status"`
	Source             string        `bson:"source"`
	VerificationStatus string        `bson:"verificationStatus"`
	SourceSuggestionID *string       `bson:"sourceSuggestionId,omitempty"`
	CreatedAt          time.Time     `bson:"createdAt"`
	SchemaVersion      int           `bson:"schemaVersion"`
}

func (r *MongoWorkItemRepository) Create(ctx context.Context, w WorkItem) (WorkItem, error) {
	doc := toWorkItemDoc(w)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return WorkItem{}, err
	}
	w.ID = res.InsertedID.(bson.ObjectID).Hex()
	return w, nil
}

func (r *MongoWorkItemRepository) FindByID(ctx context.Context, companyID, id string) (WorkItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return WorkItem{}, ErrWorkItemNotFound
	}
	var doc workItemDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return WorkItem{}, ErrWorkItemNotFound
	}
	if err != nil {
		return WorkItem{}, err
	}
	return fromWorkItemDoc(doc)
}

// ListPaginated builds one filter — tenant scope, exactly one of
// project/space scope, and an optional case-insensitive regex search across
// description/workType — and uses that SAME filter for both CountDocuments
// and the sorted, paginated Find. Exactly one of projectID/spaceID must be
// non-empty; the service layer enforces this before calling.
func (r *MongoWorkItemRepository) ListPaginated(ctx context.Context, companyID, projectID, spaceID string, req pagination.Request) ([]WorkItem, int, error) {
	filter := bson.M{"companyId": companyID}
	if spaceID != "" {
		filter["spaceId"] = spaceID
	} else {
		filter["projectId"] = projectID
	}
	if pattern := req.SearchRegexPattern(); pattern != "" {
		regex := bson.M{"$regex": pattern, "$options": "i"}
		filter["$or"] = bson.A{
			bson.M{"description": regex},
			bson.M{"workType": regex},
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

	var docs []workItemDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	result := make([]WorkItem, 0, len(docs))
	for _, doc := range docs {
		w, err := fromWorkItemDoc(doc)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, w)
	}
	return result, int(total), nil
}

func (r *MongoWorkItemRepository) UpdateStatus(ctx context.Context, companyID, id string, status WorkItemStatus) (WorkItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return WorkItem{}, ErrWorkItemNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"status": string(status)}},
	)
	if err != nil {
		return WorkItem{}, err
	}
	if res.MatchedCount == 0 {
		return WorkItem{}, ErrWorkItemNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

// Update loads the existing WorkItem (tenant-scoped), applies fn, and
// persists the full document via $set — the same read-modify-write shape as
// MongoSpaceRepository.Update. Because workItemDoc's SpaceID field is
// `bson:"spaceId,omitempty"`, a nil SpaceID after fn runs is omitted from
// the marshaled doc rather than $set to an explicit null; an explicit
// $unset is used for that case so a previously-assigned Space is actually
// removed from the stored document, not merely left stale.
// DeleteAllForCompany permanently removes every WorkItem owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoWorkItemRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoWorkItemRepository) Update(ctx context.Context, companyID, id string, fn func(*WorkItem)) (WorkItem, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return WorkItem{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id) // already validated by FindByID above
	doc := toWorkItemDoc(existing)

	update := bson.M{"$set": bson.M{
		"description": doc.Description,
		"workType":    doc.WorkType,
		"quantity":    doc.Quantity,
	}}
	if doc.SpaceID != nil {
		update["$set"].(bson.M)["spaceId"] = *doc.SpaceID
	} else {
		update["$unset"] = bson.M{"spaceId": ""}
	}

	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		update,
	)
	if err != nil {
		return WorkItem{}, err
	}
	if res.MatchedCount == 0 {
		return WorkItem{}, ErrWorkItemNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoWorkItemRepository) FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkItem, error) {
	var doc workItemDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "sourceSuggestionId": sourceSuggestionID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return WorkItem{}, ErrWorkItemNotFound
	}
	if err != nil {
		return WorkItem{}, err
	}
	return fromWorkItemDoc(doc)
}

func (r *MongoWorkItemRepository) BelongsToProject(ctx context.Context, companyID, id, projectID string) (bool, error) {
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

func toWorkItemDoc(w WorkItem) workItemDoc {
	doc := workItemDoc{
		CompanyID: w.CompanyID, ProjectID: w.ProjectID, SpaceID: w.SpaceID,
		Description: w.Description, WorkType: w.WorkType, Quantity: toQuantityDoc(w.Quantity),
		Status: string(w.Status), Source: string(w.Source), VerificationStatus: string(w.VerificationStatus),
		SourceSuggestionID: w.SourceSuggestionID,
		CreatedAt:          w.CreatedAt, SchemaVersion: w.SchemaVersion,
	}
	if w.ID != "" {
		objID, _ := bson.ObjectIDFromHex(w.ID)
		doc.ID = objID
	}
	return doc
}

func fromWorkItemDoc(doc workItemDoc) (WorkItem, error) {
	q, err := fromQuantityDoc(doc.Quantity)
	if err != nil {
		return WorkItem{}, err
	}
	return WorkItem{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, SpaceID: doc.SpaceID,
		Description: doc.Description, WorkType: doc.WorkType, Quantity: q,
		Status: WorkItemStatus(doc.Status), Source: WorkItemSource(doc.Source),
		VerificationStatus: VerificationStatus(doc.VerificationStatus),
		SourceSuggestionID: doc.SourceSuggestionID,
		CreatedAt:          doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}, nil
}
