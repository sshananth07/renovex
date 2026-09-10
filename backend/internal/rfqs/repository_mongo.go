package rfqs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

const (
	collectionRFQs        = "rfqs"
	collectionRFQCounters = "rfq_counters"

	indexNameRFQsCompany              = "idx_rfqs_company"
	indexNameRFQsCompanyProject       = "idx_rfqs_company_project"
	indexNameRFQsCompanyProjectStatus = "idx_rfqs_company_project_status"
	indexNameUniqueRFQsCompanyNumber  = "uq_rfqs_company_number"
	indexNameRFQsCompanyLineReq       = "idx_rfqs_company_line_requirement"
	indexNameUniqueRFQCountersCompany = "uq_rfq_counters_company"
)

// MongoRFQRepository is the MongoDB-backed RFQRepository.
type MongoRFQRepository struct {
	collection *mongo.Collection
}

// NewMongoRFQRepository constructs the repository over db's rfqs collection.
func NewMongoRFQRepository(db *mongo.Database) *MongoRFQRepository {
	return &MongoRFQRepository{collection: db.Collection(collectionRFQs)}
}

// EnsureIndexes creates the §12.2 indexes. It is idempotent: the composition
// root calls it on every boot.
func (r *MongoRFQRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetName(indexNameRFQsCompany)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameRFQsCompanyProject)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1},
			{Key: "status", Value: 1}},
			Options: options.Index().SetName(indexNameRFQsCompanyProjectStatus)},
		// Tenant-scoped display identifier: unique per company, so two
		// companies may hold the same number.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqNumber", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueRFQsCompanyNumber)},
		// Multikey: serves the §7.3 retry matrix and §7.5 reconciliation, which
		// must find which chain holds a requirement without scanning.
		{Keys: bson.D{{Key: "companyId", Value: 1},
			{Key: "lines.sourceMaterialRequirementId", Value: 1}},
			Options: options.Index().SetName(indexNameRFQsCompanyLineReq)},
	})
	return err
}

// --- documents ---

type quantityDoc struct {
	// Value is the canonical decimal STRING. Quantities are never stored as
	// floats and never rounded in persistence (ADR 0001).
	Value string `bson:"value"`
	Unit  string `bson:"unit"`
}

func toQuantityDoc(q quantity.Quantity) quantityDoc {
	return quantityDoc{Value: q.Value.String(), Unit: q.Unit}
}

func fromQuantityDoc(doc quantityDoc) (quantity.Quantity, error) {
	return quantity.New(doc.Value, doc.Unit)
}

type rfqLineDoc struct {
	ID                          string      `bson:"id"`
	SourceMaterialRequirementID string      `bson:"sourceMaterialRequirementId"`
	MaterialID                  string      `bson:"materialId"`
	MaterialName                string      `bson:"materialName"`
	Specification               string      `bson:"specification,omitempty"`
	Quantity                    quantityDoc `bson:"quantity"`
	RequiredByDate              *time.Time  `bson:"requiredByDate,omitempty"`
	ProcurementNotes            string      `bson:"procurementNotes,omitempty"`
	SnapshotAt                  time.Time   `bson:"snapshotAt"`
	SortOrder                   int         `bson:"sortOrder"`
}

type rfqDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	CompanyID string        `bson:"companyId"`
	ProjectID string        `bson:"projectId"`
	RFQNumber string        `bson:"rfqNumber"`
	Status    string        `bson:"status"`
	Revision  int64         `bson:"revision"`

	Title string       `bson:"title,omitempty"`
	Lines []rfqLineDoc `bson:"lines,omitempty"`

	DeliveryAddress      string     `bson:"deliveryAddress,omitempty"`
	RequiredByDate       *time.Time `bson:"requiredByDate,omitempty"`
	ResponseDeadline     *time.Time `bson:"responseDeadline,omitempty"`
	SupplierInstructions string     `bson:"supplierInstructions,omitempty"`
	InternalNotes        string     `bson:"internalNotes,omitempty"`

	CreatedByUserID string     `bson:"createdByUserId,omitempty"`
	CreatedAt       time.Time  `bson:"createdAt"`
	UpdatedAt       time.Time  `bson:"updatedAt"`
	ReadyAt         *time.Time `bson:"readyAt,omitempty"`
	ReopenedAt      *time.Time `bson:"reopenedAt,omitempty"`
	SchemaVersion   int        `bson:"schemaVersion"`
}

func toLineDocs(lines []RFQLine) []rfqLineDoc {
	docs := make([]rfqLineDoc, 0, len(lines))
	for _, l := range lines {
		docs = append(docs, rfqLineDoc{
			ID: l.ID, SourceMaterialRequirementID: l.SourceMaterialRequirementID,
			MaterialID: l.MaterialID, MaterialName: l.MaterialName,
			Specification: l.Specification, Quantity: toQuantityDoc(l.Quantity),
			RequiredByDate: l.RequiredByDate, ProcurementNotes: l.ProcurementNotes,
			SnapshotAt: l.SnapshotAt, SortOrder: l.SortOrder,
		})
	}
	return docs
}

func toRFQDoc(r RFQ) rfqDoc {
	return rfqDoc{
		CompanyID: r.CompanyID, ProjectID: r.ProjectID, RFQNumber: r.RFQNumber,
		Status: string(r.Status), Revision: r.Revision,
		Title: r.Title, Lines: toLineDocs(r.Lines),
		DeliveryAddress: r.DeliveryAddress, RequiredByDate: r.RequiredByDate,
		ResponseDeadline: r.ResponseDeadline, SupplierInstructions: r.SupplierInstructions,
		InternalNotes:   r.InternalNotes,
		CreatedByUserID: r.CreatedByUserID,
		CreatedAt:       r.CreatedAt, UpdatedAt: r.UpdatedAt,
		ReadyAt: r.ReadyAt, ReopenedAt: r.ReopenedAt,
		SchemaVersion: r.SchemaVersion,
	}
}

func fromRFQDoc(doc rfqDoc) (RFQ, error) {
	lines := make([]RFQLine, 0, len(doc.Lines))
	for _, l := range doc.Lines {
		q, err := fromQuantityDoc(l.Quantity)
		if err != nil {
			return RFQ{}, err
		}
		lines = append(lines, RFQLine{
			ID: l.ID, SourceMaterialRequirementID: l.SourceMaterialRequirementID,
			MaterialID: l.MaterialID, MaterialName: l.MaterialName,
			Specification: l.Specification, Quantity: q,
			RequiredByDate: l.RequiredByDate, ProcurementNotes: l.ProcurementNotes,
			SnapshotAt: l.SnapshotAt, SortOrder: l.SortOrder,
		})
	}

	return RFQ{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID,
		RFQNumber: doc.RFQNumber, Status: RFQStatus(doc.Status), Revision: doc.Revision,
		Title: doc.Title, Lines: lines,
		DeliveryAddress: doc.DeliveryAddress, RequiredByDate: doc.RequiredByDate,
		ResponseDeadline: doc.ResponseDeadline, SupplierInstructions: doc.SupplierInstructions,
		InternalNotes:   doc.InternalNotes,
		CreatedByUserID: doc.CreatedByUserID,
		CreatedAt:       doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
		ReadyAt: doc.ReadyAt, ReopenedAt: doc.ReopenedAt,
		SchemaVersion: doc.SchemaVersion,
	}, nil
}

// classifyCreateError translates a duplicate key into the named sentinel by
// matching the EXPLICIT index name.
//
// ErrRFQNumberConflict does NOT indicate a losable race: NextRFQNumber is
// atomic by construction, so two callers can never receive the same number. A
// duplicate here means counter corruption or manual tampering, where retrying
// would be precisely wrong (design spec §13.5).
func classifyCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			if strings.Contains(we.Message, indexNameUniqueRFQsCompanyNumber) {
				return ErrRFQNumberConflict
			}
		}
	}
	return err
}

// --- reads ---

func (r *MongoRFQRepository) Create(ctx context.Context, rfq RFQ) (RFQ, error) {
	doc := toRFQDoc(rfq)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return RFQ{}, classifyCreateError(err)
	}
	rfq.ID = res.InsertedID.(bson.ObjectID).Hex()
	return rfq, nil
}

func (r *MongoRFQRepository) FindByID(ctx context.Context, companyID, id string) (RFQ, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		// A malformed id is reported as not-found so the handler maps it to 404
		// rather than 500.
		return RFQ{}, ErrRFQNotFound
	}
	var doc rfqDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RFQ{}, ErrRFQNotFound
	}
	if err != nil {
		return RFQ{}, err
	}
	return fromRFQDoc(doc)
}

func (r *MongoRFQRepository) find(ctx context.Context, filter bson.M) ([]RFQ, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []rfqDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]RFQ, 0, len(docs))
	for _, doc := range docs {
		rfq, err := fromRFQDoc(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, rfq)
	}
	return out, nil
}

// DeleteAllForCompany permanently removes every RFQ owned by companyID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoRFQRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoRFQRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]RFQ, error) {
	return r.find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
}

// FindByLineRequirementID locates the chain holding one requirement on a line,
// backed by the multikey index (design spec §7.3, §7.5).
func (r *MongoRFQRepository) FindByLineRequirementID(ctx context.Context,
	companyID, requirementID string) (RFQ, bool, error) {
	var doc rfqDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "lines.sourceMaterialRequirementId": requirementID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RFQ{}, false, nil
	}
	if err != nil {
		return RFQ{}, false, err
	}
	rfq, err := fromRFQDoc(doc)
	if err != nil {
		return RFQ{}, false, err
	}
	return rfq, true, nil
}

// --- conditional writes ---

// conditionalUpdate applies set against a document matching baseFilter plus the
// expected revision, bumping revision by one. On a 0-match it re-reads to
// distinguish a missing/foreign document (404) from a state or revision
// conflict (409) — the pattern established by quotations.
func (r *MongoRFQRepository) conditionalUpdate(ctx context.Context, companyID, id string,
	expectedRevision int64, extraFilter bson.M, set bson.M) (RFQ, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return RFQ{}, ErrRFQNotFound
	}

	filter := bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision}
	for k, v := range extraFilter {
		filter[k] = v
	}
	set["revision"] = expectedRevision + 1

	res, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return RFQ{}, err
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrRFQNotFound) {
			return RFQ{}, ErrRFQNotFound
		}
		return RFQ{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

// UpdateDraft writes header fields only. The draft filter is what stops a ready
// RFQ's supplier-visible scope changing underneath a supplier (design spec
// §6.4).
//
// Lines are absent from the $set by construction: they move only through
// ReplaceLines, which pairs with the claim orchestration.
func (r *MongoRFQRepository) UpdateDraft(ctx context.Context, companyID, id string,
	expectedRevision int64, updated RFQ) (RFQ, error) {
	extraFilter := bson.M{"status": string(RFQStatusDraft)}
	set := bson.M{
		"title":                updated.Title,
		"deliveryAddress":      updated.DeliveryAddress,
		"requiredByDate":       updated.RequiredByDate,
		"responseDeadline":     updated.ResponseDeadline,
		"supplierInstructions": updated.SupplierInstructions,
		"internalNotes":        updated.InternalNotes,
		"updatedAt":            time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// ReplaceLines writes the whole line array. Draft-only: a ready RFQ's lines are
// the scope a supplier will quote against.
func (r *MongoRFQRepository) ReplaceLines(ctx context.Context, companyID, id string,
	expectedRevision int64, lines []RFQLine) (RFQ, error) {
	extraFilter := bson.M{"status": string(RFQStatusDraft)}
	set := bson.M{
		"lines":     toLineDocs(lines),
		"updatedAt": time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// MarkReady transitions draft -> ready.
//
// The Revision increment (applied by conditionalUpdate) is deliberate and
// unlike quotations.Finalize: an RFQ cycles draft -> ready -> draft, so a
// frozen revision would let a stale pre-ready client's conditional update match
// and mutate a scope that was reviewed and locked in between (design spec §6.4).
func (r *MongoRFQRepository) MarkReady(ctx context.Context, companyID, id string,
	expectedRevision int64, readyAt time.Time) (RFQ, error) {
	extraFilter := bson.M{"status": string(RFQStatusDraft)}
	set := bson.M{
		"status":    string(RFQStatusReady),
		"readyAt":   readyAt,
		"updatedAt": time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// Reopen transitions ready -> draft, likewise incrementing Revision.
//
// Claims survive this transition: changing RFQ status never releases a
// requirement (design spec §6.4).
func (r *MongoRFQRepository) Reopen(ctx context.Context, companyID, id string,
	expectedRevision int64, reopenedAt time.Time) (RFQ, error) {
	extraFilter := bson.M{"status": string(RFQStatusReady)}
	set := bson.M{
		"status":     string(RFQStatusDraft),
		"reopenedAt": reopenedAt,
		"updatedAt":  time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// Delete removes an RFQ under a Revision guard.
//
// §7.7's three conditions are the SERVICE's responsibility — particularly the
// zero-outstanding-claims check, which needs the requirement capability. This
// guarantees only that the document has not moved since it was read.
func (r *MongoRFQRepository) Delete(ctx context.Context, companyID, id string,
	expectedRevision int64) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrRFQNotFound
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{
		"_id": objID, "companyId": companyID, "revision": expectedRevision,
	})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrRFQNotFound) {
			return ErrRFQNotFound
		}
		return ErrRevisionMismatch
	}
	return nil
}

// --- counter ---

// MongoRFQCounterRepository allocates tenant-scoped RFQ numbers from its own
// collection, separate from the aggregate.
type MongoRFQCounterRepository struct {
	collection *mongo.Collection
}

// NewMongoRFQCounterRepository constructs the counter repository.
func NewMongoRFQCounterRepository(db *mongo.Database) *MongoRFQCounterRepository {
	return &MongoRFQCounterRepository{collection: db.Collection(collectionRFQCounters)}
}

// EnsureIndexes creates the unique per-company counter index.
func (r *MongoRFQCounterRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "companyId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName(indexNameUniqueRFQCountersCompany),
	})
	return err
}

// NextRFQNumber atomically increments and returns companyID's next sequence
// value via FindOneAndUpdate + $inc + upsert, returning the post-increment
// document.
//
// Atomic BY CONSTRUCTION: there is no read-then-write window and no retry loop.
// Copied from MongoQuotationCounterRepository (design spec §6.1).
// DeleteAllForCompany permanently removes companyID's counter document.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoRFQCounterRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoRFQCounterRepository) NextRFQNumber(ctx context.Context, companyID string) (int64, error) {
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc struct {
		NextNumber int64 `bson:"nextNumber"`
	}
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{"companyId": companyID},
		bson.M{"$inc": bson.M{"nextNumber": int64(1)}},
		opts,
	).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.NextNumber, nil
}

// FormatRFQNumber renders a sequence value as the tenant-scoped display
// identifier. Six-digit zero padding is a minimum width, not a cap: a company
// exceeding 999999 keeps counting rather than wrapping into a collision.
func FormatRFQNumber(n int64) string {
	return fmt.Sprintf("RFQ-%06d", n)
}
