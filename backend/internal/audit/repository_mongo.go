package audit

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Explicit index names. Both lead with companyId so no audit query can be
// constructed that reads across tenants (design spec §13).
const (
	indexNameAuditEventsCompanyProjectCreated = "idx_audit_events_company_project_createdAt"
	indexNameAuditEventsCompanySubject        = "idx_audit_events_company_subject"
	indexNameAuditEventsEnsureOnce            = "uniq_audit_events_ensure_once"
)

// MongoEventRepository owns the "audit_events" collection exclusively.
// The collection is append-only: no update or delete path exists.
type MongoEventRepository struct {
	collection *mongo.Collection
}

func NewMongoEventRepository(db *mongo.Database) *MongoEventRepository {
	return &MongoEventRepository{collection: db.Collection("audit_events")}
}

// EnsureIndexes creates the project timeline index and the per-subject
// lookup index. There is no unique index — every insert is unconditionally
// accepted, so no duplicate-key classification is needed.
func (r *MongoEventRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1},
			{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameAuditEventsCompanyProjectCreated)},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "subjectType", Value: 1},
			{Key: "subjectId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameAuditEventsCompanySubject)},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "eventType", Value: 1},
			{Key: "subjectType", Value: 1}, {Key: "subjectId", Value: 1},
			{Key: "dedupeIdentity", Value: 1}},
			Options: options.Index().SetName(indexNameAuditEventsEnsureOnce).
				SetUnique(true).SetPartialFilterExpression(bson.M{
				"dedupeIdentity": bson.M{"$type": "string"},
			})},
	})
	return err
}

type eventDoc struct {
	ID             bson.ObjectID  `bson:"_id,omitempty"`
	CompanyID      string         `bson:"companyId"`
	ProjectID      string         `bson:"projectId"`
	EventType      string         `bson:"eventType"`
	SubjectType    string         `bson:"subjectType"`
	SubjectID      string         `bson:"subjectId"`
	ActorType      string         `bson:"actorType"`
	ActorID        string         `bson:"actorId,omitempty"`
	Metadata       map[string]any `bson:"metadata,omitempty"`
	DedupeIdentity string         `bson:"dedupeIdentity,omitempty"`
	CreatedAt      time.Time      `bson:"createdAt"`
	SchemaVersion  int            `bson:"schemaVersion"`
}

func (r *MongoEventRepository) Create(ctx context.Context, e Event) error {
	_, err := r.collection.InsertOne(ctx, eventDoc{
		CompanyID: e.CompanyID, ProjectID: e.ProjectID,
		EventType: e.EventType, SubjectType: e.SubjectType, SubjectID: e.SubjectID,
		ActorType: e.ActorType, ActorID: e.ActorID, Metadata: e.Metadata,
		DedupeIdentity: e.DedupeIdentity,
		CreatedAt:      e.CreatedAt, SchemaVersion: e.SchemaVersion,
	})
	// Only an explicitly ensured event may treat its named unique-index loser
	// as success. Ordinary append-only audit duplicates remain distinct writes.
	if e.DedupeIdentity != "" && mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

// ListBySubject returns every event for one subject, newest first,
// tenant-scoped. Used by tests and future contractor-facing timelines.
func (r *MongoEventRepository) ListBySubject(ctx context.Context, companyID, subjectType, subjectID string) ([]Event, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, bson.M{
		"companyId": companyID, "subjectType": subjectType, "subjectId": subjectID,
	}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var docs []eventDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	events := make([]Event, len(docs))
	for i, d := range docs {
		events[i] = Event{
			ID: d.ID.Hex(), CompanyID: d.CompanyID, ProjectID: d.ProjectID,
			EventType: d.EventType, SubjectType: d.SubjectType, SubjectID: d.SubjectID,
			ActorType: d.ActorType, ActorID: d.ActorID, Metadata: d.Metadata,
			DedupeIdentity: d.DedupeIdentity,
			CreatedAt:      d.CreatedAt, SchemaVersion: d.SchemaVersion,
		}
	}
	return events, nil
}

// ListByProject returns every event for one project, newest first,
// tenant-scoped.
// DeleteAllForCompany permanently removes every Event owned by companyID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoEventRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoEventRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]Event, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, bson.M{
		"companyId": companyID, "projectId": projectID,
	}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var docs []eventDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	events := make([]Event, len(docs))
	for i, d := range docs {
		events[i] = Event{
			ID: d.ID.Hex(), CompanyID: d.CompanyID, ProjectID: d.ProjectID,
			EventType: d.EventType, SubjectType: d.SubjectType, SubjectID: d.SubjectID,
			ActorType: d.ActorType, ActorID: d.ActorID, Metadata: d.Metadata,
			DedupeIdentity: d.DedupeIdentity,
			CreatedAt:      d.CreatedAt, SchemaVersion: d.SchemaVersion,
		}
	}
	return events, nil
}
