package workresources

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository is the MongoDB-backed WorkResourceRequirementRepository
// implementation. It owns the "work_resource_requirements" collection
// exclusively.
type MongoRepository struct {
	collection *mongo.Collection
}

// NewMongoRepository constructs a MongoRepository against db's
// "work_resource_requirements" collection.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: db.Collection("work_resource_requirements")}
}

// EnsureIndexes creates the companyId+projectId index, the
// companyId+workItemId index, a unique companyId+sourceSuggestionId index
// restricted by partialFilterExpression to documents where the field exists
// (the AI-acceptance idempotency anchor, design doc §18.2 — a bare sparse
// compound index is NOT sufficient because companyId is always present, so
// it would still collide once two manual requirements both lack the
// field), and a companyId+workItemId+resourceType+materialId /
// companyId+workItemId+resourceType+normalizedName duplicate-detection
// index (design doc §20.3).
func (r *MongoRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1}}},
		{
			Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sourceSuggestionId", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(
				bson.D{{Key: "sourceSuggestionId", Value: bson.D{{Key: "$exists", Value: true}}}},
			),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1},
				{Key: "resourceType", Value: 1}, {Key: "materialId", Value: 1},
			},
			Options: options.Index().SetPartialFilterExpression(
				bson.D{{Key: "materialId", Value: bson.D{{Key: "$exists", Value: true}}}},
			),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1},
				{Key: "resourceType", Value: 1}, {Key: "normalizedName", Value: 1},
			},
		},
	})
	return err
}

type requirementDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	ProjectID          string        `bson:"projectId"`
	WorkItemID         string        `bson:"workItemId"`
	ResourceType       string        `bson:"resourceType"`
	MaterialID         *string       `bson:"materialId,omitempty"`
	Name               string        `bson:"name"`
	NormalizedName     string        `bson:"normalizedName"`
	Status             string        `bson:"status"`
	Source             string        `bson:"source"`
	SourceSuggestionID *string       `bson:"sourceSuggestionId,omitempty"`
	CreatedByUserID    string        `bson:"createdByUserId"`
	CreatedAt          time.Time     `bson:"createdAt"`
	UpdatedAt          time.Time     `bson:"updatedAt"`
	SchemaVersion      int           `bson:"schemaVersion"`
}

// normalize matches materialrequirements.UnitsMismatch's convention: trim
// then Unicode-case-fold, so "Tiler" and " tiler " are the same dedupe key.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// DeleteAllForCompany permanently removes every WorkResourceRequirement
// owned by companyID. Never errors when zero documents match.
// Development-tool use only.
func (r *MongoRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoRepository) Create(ctx context.Context, req WorkResourceRequirement) (WorkResourceRequirement, error) {
	doc := toRequirementDoc(req)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return WorkResourceRequirement{}, err
	}
	req.ID = res.InsertedID.(bson.ObjectID).Hex()
	return req, nil
}

func (r *MongoRepository) FindByID(ctx context.Context, companyID, id string) (WorkResourceRequirement, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return WorkResourceRequirement{}, ErrRequirementNotFound
	}
	var doc requirementDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return WorkResourceRequirement{}, ErrRequirementNotFound
	}
	if err != nil {
		return WorkResourceRequirement{}, err
	}
	return fromRequirementDoc(doc), nil
}

func (r *MongoRepository) FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkResourceRequirement, error) {
	var doc requirementDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "sourceSuggestionId": sourceSuggestionID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return WorkResourceRequirement{}, ErrRequirementNotFound
	}
	if err != nil {
		return WorkResourceRequirement{}, err
	}
	return fromRequirementDoc(doc), nil
}

func (r *MongoRepository) FindByDuplicateKey(ctx context.Context, key DuplicateKey) (WorkResourceRequirement, error) {
	filter := bson.M{
		"companyId":    key.CompanyID,
		"workItemId":   key.WorkItemID,
		"resourceType": string(key.ResourceType),
	}
	if key.ResourceType == ResourceTypeMaterial {
		filter["materialId"] = key.MaterialID
	} else {
		filter["normalizedName"] = normalize(key.NormalizedName)
	}

	var doc requirementDoc
	err := r.collection.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return WorkResourceRequirement{}, ErrRequirementNotFound
	}
	if err != nil {
		return WorkResourceRequirement{}, err
	}
	return fromRequirementDoc(doc), nil
}

func (r *MongoRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]WorkResourceRequirement, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []requirementDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]WorkResourceRequirement, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromRequirementDoc(doc))
	}
	return result, nil
}

func (r *MongoRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]WorkResourceRequirement, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "workItemId": workItemID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []requirementDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]WorkResourceRequirement, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromRequirementDoc(doc))
	}
	return result, nil
}

func toRequirementDoc(r WorkResourceRequirement) requirementDoc {
	doc := requirementDoc{
		CompanyID: r.CompanyID, ProjectID: r.ProjectID, WorkItemID: r.WorkItemID,
		ResourceType: string(r.ResourceType), MaterialID: r.MaterialID,
		Name: r.Name, NormalizedName: normalize(r.Name),
		Status: string(r.Status), Source: string(r.Source),
		SourceSuggestionID: r.SourceSuggestionID,
		CreatedByUserID:    r.CreatedByUserID,
		CreatedAt:          r.CreatedAt, UpdatedAt: r.UpdatedAt, SchemaVersion: r.SchemaVersion,
	}
	if r.ID != "" {
		objID, _ := bson.ObjectIDFromHex(r.ID)
		doc.ID = objID
	}
	return doc
}

func fromRequirementDoc(doc requirementDoc) WorkResourceRequirement {
	return WorkResourceRequirement{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, WorkItemID: doc.WorkItemID,
		ResourceType: ResourceType(doc.ResourceType), MaterialID: doc.MaterialID, Name: doc.Name,
		Status: Status(doc.Status), Source: Source(doc.Source),
		SourceSuggestionID: doc.SourceSuggestionID,
		CreatedByUserID:    doc.CreatedByUserID,
		CreatedAt:          doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
