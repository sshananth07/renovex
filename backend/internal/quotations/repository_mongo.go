package quotations

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MongoQuotationRepository is the MongoDB-backed QuotationRepository
// implementation. It owns the "quotations" collection exclusively.
type MongoQuotationRepository struct {
	collection *mongo.Collection
}

// NewMongoQuotationRepository constructs a MongoQuotationRepository
// against db's "quotations" collection.
func NewMongoQuotationRepository(db *mongo.Database) *MongoQuotationRepository {
	return &MongoQuotationRepository{collection: db.Collection("quotations")}
}

// Explicit index names — EVERY index carries one, with no exceptions,
// including the plain {companyId} index (design spec §26, 2nd review
// round correction). uq_quotations_company_number_version and
// uq_quotations_one_draft_per_number both key on
// {companyId, quotationNumber} (one plain-unique, one partial-unique
// scoped to status=draft) and would otherwise collide on MongoDB's
// default auto-generated name.
const (
	indexNameQuotationsCompany                    = "idx_quotations_company"
	indexNameQuotationsCompanyProject             = "idx_quotations_company_project"
	indexNameUniqueQuotationsCompanyNumberVersion = "uq_quotations_company_number_version"
	indexNameUniqueQuotationsOneDraftPerNumber    = "uq_quotations_one_draft_per_number"
)

// EnsureIndexes creates the companyId index, the companyId+projectId
// compound index, the UNIQUE companyId+quotationNumber+version index, and
// the UNIQUE PARTIAL companyId+quotationNumber index (status=draft only)
// enforcing "at most one draft per QuotationNumber chain" (design spec
// §26).
func (r *MongoQuotationRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetName(indexNameQuotationsCompany)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameQuotationsCompanyProject)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "quotationNumber", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueQuotationsCompanyNumberVersion)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "quotationNumber", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "draft"}).
				SetName(indexNameUniqueQuotationsOneDraftPerNumber)},
	})
	return err
}

type quotationLineDoc struct {
	ID                string       `bson:"id"`
	SourceWorkItemIDs []string     `bson:"sourceWorkItemIds"`
	Description       string       `bson:"description"`
	Quantity          *string      `bson:"quantity,omitempty"`
	Unit              *string      `bson:"unit,omitempty"`
	UnitPrice         *money.Money `bson:"unitPrice,omitempty"`
	Amount            money.Money  `bson:"amount"`
	SortOrder         int          `bson:"sortOrder"`
}

type quotationDoc struct {
	ID                bson.ObjectID      `bson:"_id,omitempty"`
	CompanyID         string             `bson:"companyId"`
	ProjectID         string             `bson:"projectId"`
	ClientID          string             `bson:"clientId"`
	EstimateID        string             `bson:"estimateId"`
	QuotationNumber   string             `bson:"quotationNumber"`
	Version           int                `bson:"version"`
	Status            string             `bson:"status"`
	Revision          int64              `bson:"revision"`
	Currency          string             `bson:"currency"`
	Lines             []quotationLineDoc `bson:"lines"`
	Subtotal          money.Money        `bson:"subtotal"`
	GeneratedSubtotal money.Money        `bson:"generatedSubtotal"`
	TaxMode           string             `bson:"taxMode"`
	TaxLabel          string             `bson:"taxLabel"`
	TaxRateBPS        int64              `bson:"taxRateBps"`
	TaxAmount         money.Money        `bson:"taxAmount"`
	Total             money.Money        `bson:"total"`
	Terms             string             `bson:"terms"`
	PaymentSchedule   string             `bson:"paymentSchedule"`
	ValidUntil        *time.Time         `bson:"validUntil,omitempty"`
	Notes             string             `bson:"notes"`
	CreatedAt         time.Time          `bson:"createdAt"`
	FinalizedAt       *time.Time         `bson:"finalizedAt,omitempty"`
	SchemaVersion     int                `bson:"schemaVersion"`
}

func toQuotationLineDocs(lines []QuotationLine) []quotationLineDoc {
	docs := make([]quotationLineDoc, len(lines))
	for i, l := range lines {
		var quantityStr *string
		if l.Quantity != nil {
			s := l.Quantity.String()
			quantityStr = &s
		}
		sourceIDs := l.SourceWorkItemIDs
		if sourceIDs == nil {
			sourceIDs = []string{}
		}
		docs[i] = quotationLineDoc{
			ID: l.ID, SourceWorkItemIDs: sourceIDs, Description: l.Description,
			Quantity: quantityStr, Unit: l.Unit, UnitPrice: l.UnitPrice,
			Amount: l.Amount, SortOrder: l.SortOrder,
		}
	}
	return docs
}

func fromQuotationLineDocs(docs []quotationLineDoc) ([]QuotationLine, error) {
	lines := make([]QuotationLine, len(docs))
	for i, d := range docs {
		var qty *decimal.Decimal
		if d.Quantity != nil {
			parsed, err := decimal.NewFromString(*d.Quantity)
			if err != nil {
				return nil, err
			}
			qty = &parsed
		}
		sourceIDs := d.SourceWorkItemIDs
		if sourceIDs == nil {
			sourceIDs = []string{}
		}
		lines[i] = QuotationLine{
			ID: d.ID, SourceWorkItemIDs: sourceIDs, Description: d.Description,
			Quantity: qty, Unit: d.Unit, UnitPrice: d.UnitPrice,
			Amount: d.Amount, SortOrder: d.SortOrder,
		}
	}
	return lines, nil
}

func toQuotationDoc(q Quotation) (quotationDoc, error) {
	doc := quotationDoc{
		CompanyID: q.CompanyID, ProjectID: q.ProjectID, ClientID: q.ClientID, EstimateID: q.EstimateID,
		QuotationNumber: q.QuotationNumber, Version: q.Version, Status: string(q.Status), Revision: q.Revision,
		Currency: q.Currency, Lines: toQuotationLineDocs(q.Lines),
		Subtotal: q.Subtotal, GeneratedSubtotal: q.GeneratedSubtotal,
		TaxMode: string(q.TaxMode), TaxLabel: q.TaxLabel, TaxRateBPS: int64(q.TaxRateBPS),
		TaxAmount: q.TaxAmount, Total: q.Total,
		Terms: q.Terms, PaymentSchedule: q.PaymentSchedule, ValidUntil: q.ValidUntil, Notes: q.Notes,
		CreatedAt: q.CreatedAt, FinalizedAt: q.FinalizedAt, SchemaVersion: q.SchemaVersion,
	}
	if q.ID != "" {
		objID, err := bson.ObjectIDFromHex(q.ID)
		if err != nil {
			return quotationDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromQuotationDoc(doc quotationDoc) (Quotation, error) {
	lines, err := fromQuotationLineDocs(doc.Lines)
	if err != nil {
		return Quotation{}, err
	}
	return Quotation{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, ClientID: doc.ClientID,
		EstimateID: doc.EstimateID, QuotationNumber: doc.QuotationNumber, Version: doc.Version,
		Status: QuotationStatus(doc.Status), Revision: doc.Revision, Currency: doc.Currency, Lines: lines,
		Subtotal: doc.Subtotal, GeneratedSubtotal: doc.GeneratedSubtotal,
		TaxMode: TaxMode(doc.TaxMode), TaxLabel: doc.TaxLabel, TaxRateBPS: money.RateBPS(doc.TaxRateBPS),
		TaxAmount: doc.TaxAmount, Total: doc.Total,
		Terms: doc.Terms, PaymentSchedule: doc.PaymentSchedule, ValidUntil: doc.ValidUntil, Notes: doc.Notes,
		CreatedAt: doc.CreatedAt, FinalizedAt: doc.FinalizedAt, SchemaVersion: doc.SchemaVersion,
	}, nil
}

// classifyCreateError translates a raw MongoDB duplicate-key error from
// InsertOne into one of the two named domain sentinels (ErrVersionConflict,
// ErrDraftAlreadyExists) by checking which EXPLICIT index name appears in
// the write error's message. Returns ErrUnclassifiedDuplicateKey (NOT
// ErrVersionConflict) if a duplicate-key error occurs that doesn't match
// either known index name — an error this function cannot positively
// identify must not be silently assumed safe to retry (design spec §27.3
// step 6, mirroring estimates.classifyCreateError exactly).
func classifyCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			switch {
			case strings.Contains(we.Message, indexNameUniqueQuotationsOneDraftPerNumber):
				return ErrDraftAlreadyExists
			case strings.Contains(we.Message, indexNameUniqueQuotationsCompanyNumberVersion):
				return ErrVersionConflict
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoQuotationRepository) Create(ctx context.Context, q Quotation) (Quotation, error) {
	doc, err := toQuotationDoc(q)
	if err != nil {
		return Quotation{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Quotation{}, classifyCreateError(err)
	}
	q.ID = res.InsertedID.(bson.ObjectID).Hex()
	return q, nil
}

func (r *MongoQuotationRepository) FindByID(ctx context.Context, companyID, id string) (Quotation, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Quotation{}, ErrQuotationNotFound
	}
	var doc quotationDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Quotation{}, ErrQuotationNotFound
	}
	if err != nil {
		return Quotation{}, err
	}
	return fromQuotationDoc(doc)
}

func (r *MongoQuotationRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []quotationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]Quotation, 0, len(docs))
	for _, doc := range docs {
		q, err := fromQuotationDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, q)
	}
	return result, nil
}

func (r *MongoQuotationRepository) FindMaxVersion(ctx context.Context, companyID, quotationNumber string) (int, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}}).SetProjection(bson.M{"version": 1})
	var doc struct {
		Version int `bson:"version"`
	}
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "quotationNumber": quotationNumber}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return doc.Version, nil
}

// conditionalUpdate applies set against a document matching
// {_id, companyId, status: draft, revision: expectedRevision}. If
// incrementRevision is true, revision is bumped to expectedRevision+1 as
// part of the same $set. If false (used by Finalize), revision is left
// untouched — frozen at whatever value it held at the moment finalize
// succeeds (design spec §12/§27.4, mirroring
// estimates.conditionalUpdate exactly).
// DeleteAllForCompany permanently removes every Quotation owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoQuotationRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoQuotationRepository) conditionalUpdate(ctx context.Context, companyID, id string, expectedRevision int64, set bson.M, incrementRevision bool) (Quotation, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Quotation{}, ErrQuotationNotFound
	}
	if incrementRevision {
		set["revision"] = expectedRevision + 1
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": string(QuotationStatusDraft), "revision": expectedRevision},
		bson.M{"$set": set},
	)
	if err != nil {
		return Quotation{}, err
	}
	if res.MatchedCount == 0 {
		_, findErr := r.FindByID(ctx, companyID, id)
		if findErr == ErrQuotationNotFound {
			return Quotation{}, ErrQuotationNotFound
		}
		return Quotation{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoQuotationRepository) ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error) {
	set := bson.M{
		"lines": toQuotationLineDocs(updated.Lines), "subtotal": updated.Subtotal,
		"taxAmount": updated.TaxAmount, "total": updated.Total,
	}
	// GeneratedSubtotal is deliberately absent from this $set — it is
	// frozen at generation time and must never be touched by a line edit
	// (design spec §6.3, Review Decision 3).
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoQuotationRepository) ReplaceTerms(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error) {
	set := bson.M{
		"terms": updated.Terms, "paymentSchedule": updated.PaymentSchedule,
		"notes": updated.Notes, "validUntil": updated.ValidUntil,
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoQuotationRepository) ReplaceTax(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error) {
	set := bson.M{
		"taxMode": string(updated.TaxMode), "taxLabel": updated.TaxLabel, "taxRateBps": int64(updated.TaxRateBPS),
		"taxAmount": updated.TaxAmount, "total": updated.Total,
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoQuotationRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Quotation, error) {
	set := bson.M{"status": string(QuotationStatusFinalized), "finalizedAt": finalizedAt}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, false)
}

// MongoQuotationCounterRepository is the MongoDB-backed
// QuotationCounterRepository implementation. It owns the
// "quotation_counters" collection exclusively (design spec §2).
type MongoQuotationCounterRepository struct {
	collection *mongo.Collection
}

// NewMongoQuotationCounterRepository constructs a
// MongoQuotationCounterRepository against db's "quotation_counters"
// collection.
func NewMongoQuotationCounterRepository(db *mongo.Database) *MongoQuotationCounterRepository {
	return &MongoQuotationCounterRepository{collection: db.Collection("quotation_counters")}
}

const indexNameUniqueQuotationCountersCompany = "uq_quotation_counters_company"

// EnsureIndexes creates the UNIQUE companyId index enforcing one counter
// document per Company (design spec §26).
func (r *MongoQuotationCounterRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueQuotationCountersCompany)},
	})
	return err
}

// NextQuotationNumber atomically increments and returns companyID's next
// sequence number via FindOneAndUpdate/$inc with upsert:true — atomic by
// construction, no read-then-write race window, no retry loop needed
// (design spec §9.3/§27.1).
// DeleteAllForCompany permanently removes companyID's counter document.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoQuotationCounterRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoQuotationCounterRepository) NextQuotationNumber(ctx context.Context, companyID string) (int64, error) {
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
