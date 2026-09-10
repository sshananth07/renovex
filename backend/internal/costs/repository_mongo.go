package costs

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

// MongoCostItemRepository is the MongoDB-backed CostItemRepository
// implementation. It owns the "cost_items" collection exclusively.
type MongoCostItemRepository struct {
	collection *mongo.Collection
}

// NewMongoCostItemRepository constructs a MongoCostItemRepository against
// db's "cost_items" collection.
func NewMongoCostItemRepository(db *mongo.Database) *MongoCostItemRepository {
	return &MongoCostItemRepository{collection: db.Collection("cost_items")}
}

// EnsureIndexes creates the companyId index, the companyId+projectId compound
// index, and a sparse companyId+workItemId compound index (design spec §13).
func (r *MongoCostItemRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1}},
			Options: options.Index().SetSparse(true)},
	})
	return err
}

// quantityDoc mirrors internal/work/repository_mongo.go's own private copy —
// shopspring/decimal has no BSON marshaling, so Value is stored as a string,
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

type costItemDoc struct {
	ID                bson.ObjectID      `bson:"_id,omitempty"`
	CompanyID         string             `bson:"companyId"`
	ProjectID         string             `bson:"projectId"`
	WorkItemID        *string            `bson:"workItemId,omitempty"`
	Category          string             `bson:"category"`
	Description       string             `bson:"description"`
	Quantity          *quantityDoc       `bson:"quantity,omitempty"`
	UnitPrice         *money.Money       `bson:"unitPrice,omitempty"`
	MaterialID        *string            `bson:"materialId,omitempty"`
	Estimated         *money.Money       `bson:"estimated,omitempty"`
	Committed         *money.Money       `bson:"committed,omitempty"`
	Actual            *money.Money       `bson:"actual,omitempty"`
	Paid              *money.Money       `bson:"paid,omitempty"`
	Currency          string             `bson:"currency"`
	Date              time.Time          `bson:"date"`
	Notes             string             `bson:"notes,omitempty"`
	CreatedAt         time.Time          `bson:"createdAt"`
	SchemaVersion     int                `bson:"schemaVersion"`
	Revision          int64              `bson:"revision"`
	ActualCorrections []ActualCorrection `bson:"actualCorrections,omitempty"`
}

func (r *MongoCostItemRepository) Create(ctx context.Context, c CostItem) (CostItem, error) {
	doc, err := toCostItemDoc(c)
	if err != nil {
		return CostItem{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return CostItem{}, err
	}
	c.ID = res.InsertedID.(bson.ObjectID).Hex()
	return c, nil
}

func (r *MongoCostItemRepository) FindByID(ctx context.Context, companyID, id string) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	var doc costItemDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return CostItem{}, ErrCostItemNotFound
	}
	if err != nil {
		return CostItem{}, err
	}
	return fromCostItemDoc(doc)
}

func (r *MongoCostItemRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "projectId": projectID})
}

func (r *MongoCostItemRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "workItemId": workItemID})
}

func (r *MongoCostItemRepository) list(ctx context.Context, filter bson.M) ([]CostItem, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []costItemDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]CostItem, 0, len(docs))
	for _, doc := range docs {
		c, err := fromCostItemDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

// DeleteAllForCompany permanently removes every CostItem owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoCostItemRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// conditionalUpdate applies set against a document matching companyID/id/
// expectedRevision, bumping revision by one. On a 0-match it re-reads to
// distinguish a missing/foreign document (404) from a revision conflict
// (409) — the same pattern used by suppliers.MongoSupplierOfferingRepository
// and materialrequirements.MongoMaterialRequirementRepository.
//
// Legacy compatibility: when expectedRevision is 0, the filter matches a
// document whose "revision" field is either explicitly 0 OR absent entirely
// (any CostItem created before Revision existed).
func (r *MongoCostItemRepository) conditionalUpdate(ctx context.Context, companyID, id string,
	expectedRevision int64, set bson.M) (CostItem, error) {
	return r.conditionalUpdateWithPush(ctx, companyID, id, expectedRevision, set, nil)
}

// conditionalUpdateWithPush is conditionalUpdate's sibling for the one case
// that needs $push alongside $set in the same atomic operation. push may be
// nil, in which case this behaves exactly like conditionalUpdate.
func (r *MongoCostItemRepository) conditionalUpdateWithPush(ctx context.Context, companyID, id string,
	expectedRevision int64, set bson.M, push bson.M) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID}
	if expectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{"revision": bson.M{"$exists": false}},
			bson.M{"revision": int64(0)},
		}
	} else {
		filter["revision"] = expectedRevision
	}
	set["revision"] = expectedRevision + 1

	update := bson.M{"$set": set}
	if push != nil {
		update["$push"] = push
	}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return CostItem{}, err
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrCostItemNotFound) {
			return CostItem{}, ErrCostItemNotFound
		}
		return CostItem{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoCostItemRepository) UpdateLifecycleField(ctx context.Context, companyID, id string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error) {
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, bson.M{string(stage): amount})
}

func (r *MongoCostItemRepository) UpdateDetails(ctx context.Context, companyID, id string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error) {
	set := bson.M{"description": description, "notes": notes}
	if category != nil {
		set["category"] = string(*category)
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set)
}

// RecordActual sets Actual for the first time (no prior value must exist —
// the SERVICE enforces that precondition via FindByID before calling this;
// the repository itself just performs the CAS-guarded $set). Use
// CorrectActual once Actual is already populated.
func (r *MongoCostItemRepository) RecordActual(ctx context.Context, companyID, id string, expectedRevision int64, amount money.Money) (CostItem, error) {
	return r.conditionalUpdateWithPush(ctx, companyID, id, expectedRevision, bson.M{"actual": amount}, nil)
}

// CorrectActual atomically (a) sets Actual to newAmount, (b) bumps revision,
// and (c) appends correction to ActualCorrections — all in ONE Mongo
// UpdateOne combining $set and $push, guarded by the same
// companyId+_id+expectedRevision filter every other conditional write in
// this package uses. No transaction and no second collection: MongoDB
// guarantees a single document's $set+$push in one UpdateOne is atomic.
func (r *MongoCostItemRepository) CorrectActual(ctx context.Context, companyID, id string, expectedRevision int64, newAmount money.Money, correction ActualCorrection) (CostItem, error) {
	return r.conditionalUpdateWithPush(ctx, companyID, id, expectedRevision,
		bson.M{"actual": newAmount},
		bson.M{"actualCorrections": correction})
}

func (r *MongoCostItemRepository) Delete(ctx context.Context, companyID, id string) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrCostItemNotFound
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": objID, "companyId": companyID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrCostItemNotFound
	}
	return nil
}

func toCostItemDoc(c CostItem) (costItemDoc, error) {
	doc := costItemDoc{
		CompanyID: c.CompanyID, ProjectID: c.ProjectID, WorkItemID: c.WorkItemID,
		Category: string(c.Category), Description: c.Description, UnitPrice: c.UnitPrice,
		MaterialID: c.MaterialID, Estimated: c.Estimated, Committed: c.Committed,
		Actual: c.Actual, Paid: c.Paid, Currency: c.Currency, Date: c.Date,
		Notes: c.Notes, CreatedAt: c.CreatedAt, SchemaVersion: c.SchemaVersion,
		Revision: c.Revision, ActualCorrections: c.ActualCorrections,
	}
	if c.Quantity != nil {
		qd := toQuantityDoc(*c.Quantity)
		doc.Quantity = &qd
	}
	if c.ID != "" {
		objID, err := bson.ObjectIDFromHex(c.ID)
		if err != nil {
			return costItemDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromCostItemDoc(doc costItemDoc) (CostItem, error) {
	c := CostItem{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, WorkItemID: doc.WorkItemID,
		Category: CostCategory(doc.Category), Description: doc.Description, UnitPrice: doc.UnitPrice,
		MaterialID: doc.MaterialID, Estimated: doc.Estimated, Committed: doc.Committed,
		Actual: doc.Actual, Paid: doc.Paid, Currency: doc.Currency, Date: doc.Date,
		Notes: doc.Notes, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
		Revision: doc.Revision, ActualCorrections: doc.ActualCorrections,
	}
	if doc.Quantity != nil {
		q, err := fromQuantityDoc(*doc.Quantity)
		if err != nil {
			return CostItem{}, err
		}
		c.Quantity = &q
	}
	return c, nil
}
