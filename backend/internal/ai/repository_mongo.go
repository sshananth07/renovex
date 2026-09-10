package ai

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository is the MongoDB-backed Repository implementation. It owns
// the "ai_generation_batches" and "ai_suggestions" collections exclusively.
type MongoRepository struct {
	batches     *mongo.Collection
	suggestions *mongo.Collection
}

// NewMongoRepository constructs a MongoRepository against db's
// "ai_generation_batches" and "ai_suggestions" collections.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{
		batches:     db.Collection("ai_generation_batches"),
		suggestions: db.Collection("ai_suggestions"),
	}
}

// EnsureIndexes creates:
//   - unique companyId+operationId on batches (generation idempotency
//     anchor, design doc §18.1);
//   - companyId+projectId+type+createdAt on batches (history listing);
//   - companyId+batchId+status on suggestions (review listing).
func (r *MongoRepository) EnsureIndexes(ctx context.Context) error {
	if _, err := r.batches.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "companyId", Value: 1}, {Key: "operationId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1},
			{Key: "type", Value: 1}, {Key: "createdAt", Value: -1},
		}},
	}); err != nil {
		return err
	}
	_, err := r.suggestions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "batchId", Value: 1}, {Key: "status", Value: 1}}},
	})
	return err
}

type batchDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	CompanyID string        `bson:"companyId"`
	ProjectID string        `bson:"projectId"`

	Type   string `bson:"type"`
	Status string `bson:"status"`

	OperationID      string `bson:"operationId"`
	Provider         string `bson:"provider"`
	Model            string `bson:"model"`
	PromptVersion    string `bson:"promptVersion"`
	SchemaVersion    int    `bson:"schemaVersion"`
	InputFingerprint string `bson:"inputFingerprint"`

	StartedAt   time.Time  `bson:"startedAt"`
	CompletedAt *time.Time `bson:"completedAt,omitempty"`
	FailedAt    *time.Time `bson:"failedAt,omitempty"`
	ErrorCode   string     `bson:"errorCode,omitempty"`

	CreatedByUserID string    `bson:"createdByUserId"`
	CreatedAt       time.Time `bson:"createdAt"`
}

// suggestedDataDoc is the discriminated BSON persistence shape for
// SuggestedData — exactly one sub-document is set, matching the parent
// suggestion's type.
type suggestedDataDoc struct {
	Space             *spaceDataDoc    `bson:"space,omitempty"`
	WorkItem          *workItemDataDoc `bson:"workItem,omitempty"`
	MaterialResource  *resourceDataDoc `bson:"materialResource,omitempty"`
	TradeResource     *resourceDataDoc `bson:"tradeResource,omitempty"`
	EquipmentResource *resourceDataDoc `bson:"equipmentResource,omitempty"`
}

type spaceDataDoc struct {
	Name      string `bson:"name"`
	SpaceType string `bson:"spaceType"`
}

type workItemDataDoc struct {
	Description string  `bson:"description"`
	WorkType    string  `bson:"workType"`
	ScopeLevel  string  `bson:"scopeLevel"`
	SpaceID     *string `bson:"spaceId,omitempty"`
	ScopeOrigin string  `bson:"scopeOrigin"`
}

type resourceDataDoc struct {
	WorkItemID          string  `bson:"workItemId"`
	Name                string  `bson:"name"`
	CandidateMaterialID *string `bson:"candidateMaterialId,omitempty"`
}

type suggestionDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	CompanyID string        `bson:"companyId"`
	ProjectID string        `bson:"projectId"`
	BatchID   string        `bson:"batchId"`

	Type   string `bson:"type"`
	Status string `bson:"status"`

	Confidence *float64 `bson:"confidence,omitempty"`
	Rationale  string   `bson:"rationale,omitempty"`

	SuggestedData suggestedDataDoc `bson:"suggestedData"`

	InputFingerprint string `bson:"inputFingerprint"`

	AcceptedDomainObjectID string     `bson:"acceptedDomainObjectId,omitempty"`
	AcceptedAt             *time.Time `bson:"acceptedAt,omitempty"`
	AcceptedByUserID       string     `bson:"acceptedByUserId,omitempty"`

	RejectedAt       *time.Time `bson:"rejectedAt,omitempty"`
	RejectedByUserID string     `bson:"rejectedByUserId,omitempty"`

	Revision  int64     `bson:"revision"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// DeleteAllForCompany permanently removes every AIGenerationBatch and
// AISuggestion owned by companyID, across both of this module's
// collections. Never errors when zero documents match. Development-tool
// use only.
func (r *MongoRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	if _, err := r.suggestions.DeleteMany(ctx, bson.M{"companyId": companyID}); err != nil {
		return err
	}
	_, err := r.batches.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoRepository) CreateProcessingBatch(ctx context.Context, batch AIGenerationBatch) (AIGenerationBatch, error) {
	doc := toBatchDoc(batch)
	res, err := r.batches.InsertOne(ctx, doc)
	if err != nil {
		return AIGenerationBatch{}, err
	}
	batch.ID = res.InsertedID.(bson.ObjectID).Hex()
	return batch, nil
}

func (r *MongoRepository) FindBatchByOperationID(ctx context.Context, companyID, operationID string) (AIGenerationBatch, error) {
	var doc batchDoc
	err := r.batches.FindOne(ctx, bson.M{"companyId": companyID, "operationId": operationID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AIGenerationBatch{}, ErrBatchNotFound
	}
	if err != nil {
		return AIGenerationBatch{}, err
	}
	return fromBatchDoc(doc), nil
}

func (r *MongoRepository) FindBatchByID(ctx context.Context, companyID, batchID string) (AIGenerationBatch, error) {
	objID, err := bson.ObjectIDFromHex(batchID)
	if err != nil {
		return AIGenerationBatch{}, ErrBatchNotFound
	}
	var doc batchDoc
	err = r.batches.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AIGenerationBatch{}, ErrBatchNotFound
	}
	if err != nil {
		return AIGenerationBatch{}, err
	}
	return fromBatchDoc(doc), nil
}

func (r *MongoRepository) ListBatchesByProject(ctx context.Context, companyID, projectID string, batchType BatchType) ([]AIGenerationBatch, error) {
	cursor, err := r.batches.Find(ctx,
		bson.M{"companyId": companyID, "projectId": projectID, "type": string(batchType)},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []batchDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]AIGenerationBatch, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromBatchDoc(doc))
	}
	return result, nil
}

func (r *MongoRepository) MarkBatchCompleted(ctx context.Context, companyID, batchID string) error {
	objID, err := bson.ObjectIDFromHex(batchID)
	if err != nil {
		return ErrBatchNotFound
	}
	now := time.Now()
	res, err := r.batches.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"status": string(BatchStatusCompleted), "completedAt": now}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrBatchNotFound
	}
	return nil
}

func (r *MongoRepository) MarkBatchFailed(ctx context.Context, companyID, batchID, errorCode string) error {
	objID, err := bson.ObjectIDFromHex(batchID)
	if err != nil {
		return ErrBatchNotFound
	}
	now := time.Now()
	res, err := r.batches.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"status": string(BatchStatusFailed), "failedAt": now, "errorCode": errorCode}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrBatchNotFound
	}
	return nil
}

func (r *MongoRepository) InsertSuggestionsForBatch(ctx context.Context, batchID string, suggestions []AISuggestion) ([]AISuggestion, error) {
	if len(suggestions) == 0 {
		return suggestions, nil
	}
	docs := make([]any, 0, len(suggestions))
	for _, s := range suggestions {
		docs = append(docs, toSuggestionDoc(s))
	}
	res, err := r.suggestions.InsertMany(ctx, docs)
	if err != nil {
		return nil, err
	}
	result := make([]AISuggestion, len(suggestions))
	copy(result, suggestions)
	for i, insertedID := range res.InsertedIDs {
		if i >= len(result) {
			break
		}
		if objID, ok := insertedID.(bson.ObjectID); ok {
			result[i].ID = objID.Hex()
		}
	}
	return result, nil
}

func (r *MongoRepository) DeleteSuggestionsByBatch(ctx context.Context, batchID string) error {
	_, err := r.suggestions.DeleteMany(ctx, bson.M{"batchId": batchID})
	return err
}

func (r *MongoRepository) ListSuggestionsByBatch(ctx context.Context, companyID, batchID string) ([]AISuggestion, error) {
	cursor, err := r.suggestions.Find(ctx,
		bson.M{"companyId": companyID, "batchId": batchID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []suggestionDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]AISuggestion, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromSuggestionDoc(doc))
	}
	return result, nil
}

func (r *MongoRepository) FindSuggestionByID(ctx context.Context, companyID, suggestionID string) (AISuggestion, error) {
	objID, err := bson.ObjectIDFromHex(suggestionID)
	if err != nil {
		return AISuggestion{}, ErrSuggestionNotFound
	}
	var doc suggestionDoc
	err = r.suggestions.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AISuggestion{}, ErrSuggestionNotFound
	}
	if err != nil {
		return AISuggestion{}, err
	}
	return fromSuggestionDoc(doc), nil
}

func (r *MongoRepository) ConditionalAccept(ctx context.Context, companyID, suggestionID string, expectedRevision int64, acceptedDomainObjectID string) error {
	return r.conditionalTerminalUpdate(ctx, companyID, suggestionID, expectedRevision, bson.M{
		"status":                 string(SuggestionStatusAccepted),
		"acceptedDomainObjectId": acceptedDomainObjectID,
		"acceptedAt":             time.Now(),
	})
}

func (r *MongoRepository) ConditionalModify(ctx context.Context, companyID, suggestionID string, expectedRevision int64, acceptedDomainObjectID string) error {
	return r.conditionalTerminalUpdate(ctx, companyID, suggestionID, expectedRevision, bson.M{
		"status":                 string(SuggestionStatusModified),
		"acceptedDomainObjectId": acceptedDomainObjectID,
		"acceptedAt":             time.Now(),
	})
}

func (r *MongoRepository) ConditionalReject(ctx context.Context, companyID, suggestionID string, expectedRevision int64) error {
	return r.conditionalTerminalUpdate(ctx, companyID, suggestionID, expectedRevision, bson.M{
		"status":     string(SuggestionStatusRejected),
		"rejectedAt": time.Now(),
	})
}

// conditionalTerminalUpdate performs the shared CAS shape for accept/
// modify/reject: match on companyId+id+status=pending+revision=expected,
// $set the terminal fields, and $inc revision. A zero MatchedCount means
// either the suggestion doesn't exist for this tenant, isn't pending, or the
// caller's expectedRevision is stale — all collapse to the same
// ErrSuggestionRevisionMismatch (design doc §10.1).
func (r *MongoRepository) conditionalTerminalUpdate(ctx context.Context, companyID, suggestionID string, expectedRevision int64, setFields bson.M) error {
	objID, err := bson.ObjectIDFromHex(suggestionID)
	if err != nil {
		return ErrSuggestionRevisionMismatch
	}
	setFields["updatedAt"] = time.Now()
	res, err := r.suggestions.UpdateOne(ctx,
		bson.M{
			"_id": objID, "companyId": companyID,
			"status": string(SuggestionStatusPending), "revision": expectedRevision,
		},
		bson.M{"$set": setFields, "$inc": bson.M{"revision": 1}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrSuggestionRevisionMismatch
	}
	return nil
}

func toBatchDoc(b AIGenerationBatch) batchDoc {
	doc := batchDoc{
		CompanyID: b.CompanyID, ProjectID: b.ProjectID,
		Type: string(b.Type), Status: string(b.Status),
		OperationID: b.OperationID, Provider: b.Provider, Model: b.Model,
		PromptVersion: b.PromptVersion, SchemaVersion: b.SchemaVersion, InputFingerprint: b.InputFingerprint,
		StartedAt: b.StartedAt, CompletedAt: b.CompletedAt, FailedAt: b.FailedAt, ErrorCode: b.ErrorCode,
		CreatedByUserID: b.CreatedByUserID, CreatedAt: b.CreatedAt,
	}
	if b.ID != "" {
		objID, _ := bson.ObjectIDFromHex(b.ID)
		doc.ID = objID
	}
	return doc
}

func fromBatchDoc(doc batchDoc) AIGenerationBatch {
	return AIGenerationBatch{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID,
		Type: BatchType(doc.Type), Status: BatchStatus(doc.Status),
		OperationID: doc.OperationID, Provider: doc.Provider, Model: doc.Model,
		PromptVersion: doc.PromptVersion, SchemaVersion: doc.SchemaVersion, InputFingerprint: doc.InputFingerprint,
		StartedAt: doc.StartedAt, CompletedAt: doc.CompletedAt, FailedAt: doc.FailedAt, ErrorCode: doc.ErrorCode,
		CreatedByUserID: doc.CreatedByUserID, CreatedAt: doc.CreatedAt,
	}
}

func toSuggestionDoc(s AISuggestion) suggestionDoc {
	doc := suggestionDoc{
		CompanyID: s.CompanyID, ProjectID: s.ProjectID, BatchID: s.BatchID,
		Type: string(s.Type), Status: string(s.Status),
		Confidence: s.Confidence, Rationale: s.Rationale,
		SuggestedData:          toSuggestedDataDoc(s.SuggestedData),
		InputFingerprint:       s.InputFingerprint,
		AcceptedDomainObjectID: s.AcceptedDomainObjectID, AcceptedAt: s.AcceptedAt, AcceptedByUserID: s.AcceptedByUserID,
		RejectedAt: s.RejectedAt, RejectedByUserID: s.RejectedByUserID,
		Revision: s.Revision, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.ID != "" {
		objID, _ := bson.ObjectIDFromHex(s.ID)
		doc.ID = objID
	}
	return doc
}

func fromSuggestionDoc(doc suggestionDoc) AISuggestion {
	return AISuggestion{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, BatchID: doc.BatchID,
		Type: SuggestionType(doc.Type), Status: SuggestionStatus(doc.Status),
		Confidence: doc.Confidence, Rationale: doc.Rationale,
		SuggestedData:          fromSuggestedDataDoc(doc.SuggestedData),
		InputFingerprint:       doc.InputFingerprint,
		AcceptedDomainObjectID: doc.AcceptedDomainObjectID, AcceptedAt: doc.AcceptedAt, AcceptedByUserID: doc.AcceptedByUserID,
		RejectedAt: doc.RejectedAt, RejectedByUserID: doc.RejectedByUserID,
		Revision: doc.Revision, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}

func toSuggestedDataDoc(d SuggestedData) suggestedDataDoc {
	var doc suggestedDataDoc
	if d.Space != nil {
		doc.Space = &spaceDataDoc{Name: d.Space.Name, SpaceType: d.Space.SpaceType}
	}
	if d.WorkItem != nil {
		doc.WorkItem = &workItemDataDoc{
			Description: d.WorkItem.Description, WorkType: d.WorkItem.WorkType,
			ScopeLevel: string(d.WorkItem.ScopeLevel), SpaceID: d.WorkItem.SpaceID,
			ScopeOrigin: string(d.WorkItem.ScopeOrigin),
		}
	}
	if d.MaterialResource != nil {
		doc.MaterialResource = &resourceDataDoc{
			WorkItemID: d.MaterialResource.WorkItemID, Name: d.MaterialResource.Name,
			CandidateMaterialID: d.MaterialResource.CandidateMaterialID,
		}
	}
	if d.TradeResource != nil {
		doc.TradeResource = &resourceDataDoc{WorkItemID: d.TradeResource.WorkItemID, Name: d.TradeResource.Name}
	}
	if d.EquipmentResource != nil {
		doc.EquipmentResource = &resourceDataDoc{WorkItemID: d.EquipmentResource.WorkItemID, Name: d.EquipmentResource.Name}
	}
	return doc
}

func fromSuggestedDataDoc(doc suggestedDataDoc) SuggestedData {
	var d SuggestedData
	if doc.Space != nil {
		d.Space = &SpaceSuggestionData{Name: doc.Space.Name, SpaceType: doc.Space.SpaceType}
	}
	if doc.WorkItem != nil {
		d.WorkItem = &WorkItemSuggestionData{
			Description: doc.WorkItem.Description, WorkType: doc.WorkItem.WorkType,
			ScopeLevel: ScopeLevel(doc.WorkItem.ScopeLevel), SpaceID: doc.WorkItem.SpaceID,
			ScopeOrigin: ScopeOrigin(doc.WorkItem.ScopeOrigin),
		}
	}
	if doc.MaterialResource != nil {
		d.MaterialResource = &ResourceSuggestionData{
			WorkItemID: doc.MaterialResource.WorkItemID, Name: doc.MaterialResource.Name,
			CandidateMaterialID: doc.MaterialResource.CandidateMaterialID,
		}
	}
	if doc.TradeResource != nil {
		d.TradeResource = &ResourceSuggestionData{WorkItemID: doc.TradeResource.WorkItemID, Name: doc.TradeResource.Name}
	}
	if doc.EquipmentResource != nil {
		d.EquipmentResource = &ResourceSuggestionData{WorkItemID: doc.EquipmentResource.WorkItemID, Name: doc.EquipmentResource.Name}
	}
	return d
}
