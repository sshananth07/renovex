package labour

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// MongoWorkerRepository is the MongoDB-backed WorkerRepository implementation.
// It owns the "workers" collection exclusively.
type MongoWorkerRepository struct {
	collection *mongo.Collection
}

// NewMongoWorkerRepository constructs a MongoWorkerRepository against db's
// "workers" collection.
func NewMongoWorkerRepository(db *mongo.Database) *MongoWorkerRepository {
	return &MongoWorkerRepository{collection: db.Collection("workers")}
}

// EnsureIndexes creates the companyId index (design spec §13).
func (r *MongoWorkerRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "companyId", Value: 1}},
	})
	return err
}

type workerDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	Name          string        `bson:"name"`
	Trade         string        `bson:"trade,omitempty"`
	RateType      string        `bson:"rateType"`
	DefaultRate   money.Money   `bson:"defaultRate"`
	ContactPhone  string        `bson:"contactPhone,omitempty"`
	ContactEmail  string        `bson:"contactEmail,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoWorkerRepository) Create(ctx context.Context, w Worker) (Worker, error) {
	doc := toWorkerDoc(w)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Worker{}, err
	}
	w.ID = res.InsertedID.(bson.ObjectID).Hex()
	return w, nil
}

func (r *MongoWorkerRepository) FindByID(ctx context.Context, companyID, id string) (Worker, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Worker{}, ErrWorkerNotFound
	}
	var doc workerDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Worker{}, ErrWorkerNotFound
	}
	if err != nil {
		return Worker{}, err
	}
	return fromWorkerDoc(doc), nil
}

func (r *MongoWorkerRepository) List(ctx context.Context, companyID string) ([]Worker, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []workerDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]Worker, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromWorkerDoc(doc))
	}
	return result, nil
}

// DeleteAllForCompany permanently removes every Worker owned by companyID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoWorkerRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoWorkerRepository) Update(ctx context.Context, companyID, id string, fn func(*Worker)) (Worker, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Worker{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id)
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{
			"name": existing.Name, "trade": existing.Trade, "rateType": string(existing.RateType),
			"defaultRate": existing.DefaultRate, "contactPhone": existing.ContactPhone, "contactEmail": existing.ContactEmail,
		}},
	)
	if err != nil {
		return Worker{}, err
	}
	if res.MatchedCount == 0 {
		return Worker{}, ErrWorkerNotFound
	}
	return existing, nil
}

func toWorkerDoc(w Worker) workerDoc {
	doc := workerDoc{
		CompanyID: w.CompanyID, Name: w.Name, Trade: w.Trade, RateType: string(w.RateType),
		DefaultRate: w.DefaultRate, ContactPhone: w.ContactPhone, ContactEmail: w.ContactEmail,
		CreatedAt: w.CreatedAt, SchemaVersion: w.SchemaVersion,
	}
	if w.ID != "" {
		objID, _ := bson.ObjectIDFromHex(w.ID)
		doc.ID = objID
	}
	return doc
}

func fromWorkerDoc(doc workerDoc) Worker {
	return Worker{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, Name: doc.Name, Trade: doc.Trade,
		RateType: RateType(doc.RateType), DefaultRate: doc.DefaultRate,
		ContactPhone: doc.ContactPhone, ContactEmail: doc.ContactEmail,
		CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// MongoLabourEntryRepository is the MongoDB-backed LabourEntryRepository
// implementation. It owns the "labour_entries" collection exclusively.
type MongoLabourEntryRepository struct {
	collection *mongo.Collection
}

// NewMongoLabourEntryRepository constructs a MongoLabourEntryRepository
// against db's "labour_entries" collection.
func NewMongoLabourEntryRepository(db *mongo.Database) *MongoLabourEntryRepository {
	return &MongoLabourEntryRepository{collection: db.Collection("labour_entries")}
}

// EnsureIndexes creates the companyId index, companyId+projectId,
// companyId+workItemId, sparse companyId+workerId compound indexes, and the
// UNIQUE companyId+costItemId index enforcing the 1:1 LabourEntry<->CostItem
// invariant at the database level (design spec §13, §22-B).
func (r *MongoLabourEntryRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workerId", Value: 1}},
			Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "costItemId", Value: 1}},
			Options: options.Index().SetUnique(true)},
	})
	return err
}

// quantityDoc mirrors internal/work's own private copy — string-backed,
// never Decimal128 (design spec §12).
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

type labourEntryDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ProjectID     string        `bson:"projectId"`
	WorkItemID    string        `bson:"workItemId"`
	WorkerID      *string       `bson:"workerId,omitempty"`
	WorkerName    string        `bson:"workerName"`
	Trade         string        `bson:"trade,omitempty"`
	Quantity      quantityDoc   `bson:"quantity"`
	Rate          money.Money   `bson:"rate"`
	Cost          money.Money   `bson:"cost"`
	CostItemID    string        `bson:"costItemId"`
	Date          time.Time     `bson:"date"`
	Notes         string        `bson:"notes,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoLabourEntryRepository) Create(ctx context.Context, e LabourEntry) (LabourEntry, error) {
	doc := toLabourEntryDoc(e)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return LabourEntry{}, err
	}
	e.ID = res.InsertedID.(bson.ObjectID).Hex()
	return e, nil
}

func (r *MongoLabourEntryRepository) FindByID(ctx context.Context, companyID, id string) (LabourEntry, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	var doc labourEntryDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	if err != nil {
		return LabourEntry{}, err
	}
	return fromLabourEntryDoc(doc)
}

func (r *MongoLabourEntryRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "projectId": projectID})
}

func (r *MongoLabourEntryRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "workItemId": workItemID})
}

func (r *MongoLabourEntryRepository) list(ctx context.Context, filter bson.M) ([]LabourEntry, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []labourEntryDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]LabourEntry, 0, len(docs))
	for _, doc := range docs {
		e, err := fromLabourEntryDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

// DeleteAllForCompany permanently removes every LabourEntry owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoLabourEntryRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoLabourEntryRepository) UpdateCorrection(ctx context.Context, companyID, id string, q quantity.Quantity, rate, cost money.Money, date time.Time, notes string) (LabourEntry, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{
			"quantity": toQuantityDoc(q), "rate": rate, "cost": cost, "date": date, "notes": notes,
		}},
	)
	if err != nil {
		return LabourEntry{}, err
	}
	if res.MatchedCount == 0 {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func toLabourEntryDoc(e LabourEntry) labourEntryDoc {
	doc := labourEntryDoc{
		CompanyID: e.CompanyID, ProjectID: e.ProjectID, WorkItemID: e.WorkItemID, WorkerID: e.WorkerID,
		WorkerName: e.WorkerName, Trade: e.Trade, Quantity: toQuantityDoc(e.Quantity),
		Rate: e.Rate, Cost: e.Cost, CostItemID: e.CostItemID, Date: e.Date,
		Notes: e.Notes, CreatedAt: e.CreatedAt, SchemaVersion: e.SchemaVersion,
	}
	if e.ID != "" {
		objID, _ := bson.ObjectIDFromHex(e.ID)
		doc.ID = objID
	}
	return doc
}

func fromLabourEntryDoc(doc labourEntryDoc) (LabourEntry, error) {
	q, err := fromQuantityDoc(doc.Quantity)
	if err != nil {
		return LabourEntry{}, err
	}
	return LabourEntry{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, WorkItemID: doc.WorkItemID,
		WorkerID: doc.WorkerID, WorkerName: doc.WorkerName, Trade: doc.Trade, Quantity: q,
		Rate: doc.Rate, Cost: doc.Cost, CostItemID: doc.CostItemID, Date: doc.Date,
		Notes: doc.Notes, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}, nil
}
