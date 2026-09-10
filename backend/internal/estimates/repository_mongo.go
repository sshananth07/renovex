package estimates

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MongoEstimateRepository is the MongoDB-backed EstimateRepository
// implementation. It owns the "estimates" collection exclusively.
type MongoEstimateRepository struct {
	collection *mongo.Collection
}

// NewMongoEstimateRepository constructs a MongoEstimateRepository against
// db's "estimates" collection.
func NewMongoEstimateRepository(db *mongo.Database) *MongoEstimateRepository {
	return &MongoEstimateRepository{collection: db.Collection("estimates")}
}

// Explicit index names: two of the four indexes below share the key
// pattern {companyId, projectId} (one plain, one a partial unique index
// scoped to status=draft). MongoDB's default index-naming convention
// concatenates each key and its sort direction and does not account for a
// partialFilterExpression, so both would derive the exact same default
// name and index creation would fail outright with a name collision.
// Giving all four indexes explicit, mutually non-overlapping names avoids
// that collision and also makes duplicate-key error messages
// unambiguously classifiable by name in classifyCreateError below.
const (
	indexNameCompanyProject              = "idx_estimates_company_project"
	indexNameUniqueCompanyProjectVersion = "uq_estimates_company_project_version"
	indexNameUniqueOneDraftPerProject    = "uq_estimates_one_draft_per_project"
)

// EnsureIndexes creates the companyId index, the companyId+projectId
// compound index, the UNIQUE companyId+projectId+version index, and the
// UNIQUE PARTIAL companyId+projectId index (status=draft only) enforcing
// "at most one draft per Project" (design spec §20). All four carry
// explicit, distinct names (see the constants above) — required because
// two of these indexes share the same key pattern and would otherwise
// collide on MongoDB's default auto-generated name.
func (r *MongoEstimateRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameCompanyProject)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueCompanyProjectVersion)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "draft"}).
				SetName(indexNameUniqueOneDraftPerProject)},
	})
	return err
}

type estimateLineDoc struct {
	SourceCostItemID  string      `bson:"sourceCostItemId"`
	WorkItemID        *string     `bson:"workItemId,omitempty"`
	Category          string      `bson:"category"`
	Description       string      `bson:"description"`
	SnapshottedAmount money.Money `bson:"snapshottedAmount"`
}

type estimateDoc struct {
	ID                      bson.ObjectID     `bson:"_id,omitempty"`
	CompanyID               string            `bson:"companyId"`
	ProjectID               string            `bson:"projectId"`
	Version                 int               `bson:"version"`
	Status                  string            `bson:"status"`
	Revision                int64             `bson:"revision"`
	Currency                string            `bson:"currency"`
	Lines                   []estimateLineDoc `bson:"lines"`
	CostSubtotal            money.Money       `bson:"costSubtotal"`
	ExcludedCostItemCount   int               `bson:"excludedCostItemCount"`
	PricingMode             string            `bson:"pricingMode"`
	PricingRate             int64             `bson:"pricingRate"`
	ProposedSellingPrice    money.Money       `bson:"proposedSellingPrice"`
	ProjectedGrossProfit    money.Money       `bson:"projectedGrossProfit"`
	ProjectedGrossMarginBPS int64             `bson:"projectedGrossMarginBps"`
	CreatedAt               time.Time         `bson:"createdAt"`
	RefreshedAt             *time.Time        `bson:"refreshedAt,omitempty"`
	FinalizedAt             *time.Time        `bson:"finalizedAt,omitempty"`
	SchemaVersion           int               `bson:"schemaVersion"`
}

func toEstimateDoc(e Estimate) (estimateDoc, error) {
	lines := make([]estimateLineDoc, len(e.Lines))
	for i, l := range e.Lines {
		lines[i] = estimateLineDoc{
			SourceCostItemID: l.SourceCostItemID, WorkItemID: l.WorkItemID,
			Category: l.Category, Description: l.Description, SnapshottedAmount: l.SnapshottedAmount,
		}
	}
	doc := estimateDoc{
		CompanyID: e.CompanyID, ProjectID: e.ProjectID, Version: e.Version, Status: string(e.Status),
		Revision: e.Revision, Currency: e.Currency, Lines: lines, CostSubtotal: e.CostSubtotal,
		ExcludedCostItemCount: e.ExcludedCostItemCount, PricingMode: string(e.PricingMode),
		PricingRate: int64(e.PricingRate), ProposedSellingPrice: e.ProposedSellingPrice,
		ProjectedGrossProfit: e.ProjectedGrossProfit, ProjectedGrossMarginBPS: int64(e.ProjectedGrossMarginBPS),
		CreatedAt: e.CreatedAt, RefreshedAt: e.RefreshedAt, FinalizedAt: e.FinalizedAt, SchemaVersion: e.SchemaVersion,
	}
	if e.ID != "" {
		objID, err := bson.ObjectIDFromHex(e.ID)
		if err != nil {
			return estimateDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromEstimateDoc(doc estimateDoc) Estimate {
	lines := make([]EstimateCostLine, len(doc.Lines))
	for i, l := range doc.Lines {
		lines[i] = EstimateCostLine{
			SourceCostItemID: l.SourceCostItemID, WorkItemID: l.WorkItemID,
			Category: l.Category, Description: l.Description, SnapshottedAmount: l.SnapshottedAmount,
		}
	}
	return Estimate{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, Version: doc.Version,
		Status: EstimateStatus(doc.Status), Revision: doc.Revision, Currency: doc.Currency, Lines: lines,
		CostSubtotal: doc.CostSubtotal, ExcludedCostItemCount: doc.ExcludedCostItemCount,
		PricingMode: PricingMode(doc.PricingMode), PricingRate: money.RateBPS(doc.PricingRate),
		ProposedSellingPrice: doc.ProposedSellingPrice, ProjectedGrossProfit: doc.ProjectedGrossProfit,
		ProjectedGrossMarginBPS: money.RateBPS(doc.ProjectedGrossMarginBPS),
		CreatedAt:               doc.CreatedAt, RefreshedAt: doc.RefreshedAt, FinalizedAt: doc.FinalizedAt,
		SchemaVersion: doc.SchemaVersion,
	}
}

// classifyCreateError translates a raw MongoDB duplicate-key error from
// InsertOne into one of the two named domain sentinels
// (ErrVersionConflict, ErrDraftAlreadyExists) by checking which EXPLICIT
// index name (set via SetName in EnsureIndexes above) appears in the write
// error's message — MongoDB's E11000 duplicate-key error message always
// names the offending index
// (e.g. "E11000 duplicate key error collection: ... index: uq_estimates_company_project_version dup key: ...").
//
// Returns ErrUnclassifiedDuplicateKey (NOT ErrVersionConflict) if a
// duplicate-key error occurs that doesn't match either known index name —
// an error this function cannot positively identify must not be silently
// assumed safe to retry. ErrUnclassifiedDuplicateKey is deliberately NOT
// retried by allocateAndCreate and surfaces as a 500, the same as any other
// unrecognized internal error — retrying an operation whose failure mode is
// not understood risks looping on a real, non-transient problem (e.g. a
// schema/index misconfiguration) as if it were an ordinary, expected race.
func classifyCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			// strings.Contains (not exact equality) is required here — the
			// index name is embedded inside a longer MongoDB-generated
			// sentence, never the entire we.Message on its own. This is
			// safe against misclassification specifically because all
			// three explicit index names are mutually non-overlapping
			// strings — none is a substring of another.
			switch {
			case strings.Contains(we.Message, indexNameUniqueOneDraftPerProject):
				return ErrDraftAlreadyExists
			case strings.Contains(we.Message, indexNameUniqueCompanyProjectVersion):
				return ErrVersionConflict
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoEstimateRepository) Create(ctx context.Context, e Estimate) (Estimate, error) {
	doc, err := toEstimateDoc(e)
	if err != nil {
		return Estimate{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Estimate{}, classifyCreateError(err)
	}
	e.ID = res.InsertedID.(bson.ObjectID).Hex()
	return e, nil
}

func (r *MongoEstimateRepository) FindByID(ctx context.Context, companyID, id string) (Estimate, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Estimate{}, ErrEstimateNotFound
	}
	var doc estimateDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Estimate{}, ErrEstimateNotFound
	}
	if err != nil {
		return Estimate{}, err
	}
	return fromEstimateDoc(doc), nil
}

func (r *MongoEstimateRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []estimateDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]Estimate, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromEstimateDoc(doc))
	}
	return result, nil
}

func (r *MongoEstimateRepository) FindLatestByProject(ctx context.Context, companyID, projectID string) (Estimate, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})
	var doc estimateDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "projectId": projectID}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Estimate{}, ErrNoEstimatesForProject
	}
	if err != nil {
		return Estimate{}, err
	}
	return fromEstimateDoc(doc), nil
}

func (r *MongoEstimateRepository) FindMaxVersion(ctx context.Context, companyID, projectID string) (int, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}}).SetProjection(bson.M{"version": 1})
	var doc struct {
		Version int `bson:"version"`
	}
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "projectId": projectID}, opts).Decode(&doc)
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
// part of the same $set (used by ReplaceSnapshot/UpdatePricing, which
// produce a new mutation state that a future writer must be guarded
// against). If incrementRevision is false, revision is left untouched
// (used by Finalize — the approved design spec §16.3 explicitly freezes
// Revision at whatever value it held at the moment finalize succeeds;
// there is no subsequent draft mutation on this now-immutable document for
// a fresh Revision to protect against, so bumping it would be both
// pointless and would contradict the spec's stated invariant that
// Draft Revision N -> Finalize -> Finalized Revision N, unchanged).
// DeleteAllForCompany permanently removes every Estimate owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoEstimateRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoEstimateRepository) conditionalUpdate(ctx context.Context, companyID, id string, expectedRevision int64, set bson.M, incrementRevision bool) (Estimate, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Estimate{}, ErrEstimateNotFound
	}
	if incrementRevision {
		set["revision"] = expectedRevision + 1
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": string(EstimateStatusDraft), "revision": expectedRevision},
		bson.M{"$set": set},
	)
	if err != nil {
		return Estimate{}, err
	}
	if res.MatchedCount == 0 {
		// Distinguish "doesn't exist / wrong tenant" from "exists but
		// revision/status didn't match" so the service layer can map each
		// to the correct caller-facing error.
		_, findErr := r.FindByID(ctx, companyID, id)
		if findErr == ErrEstimateNotFound {
			return Estimate{}, ErrEstimateNotFound
		}
		return Estimate{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoEstimateRepository) ReplaceSnapshot(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error) {
	lines := make([]estimateLineDoc, len(updated.Lines))
	for i, l := range updated.Lines {
		lines[i] = estimateLineDoc{
			SourceCostItemID: l.SourceCostItemID, WorkItemID: l.WorkItemID,
			Category: l.Category, Description: l.Description, SnapshottedAmount: l.SnapshottedAmount,
		}
	}
	set := bson.M{
		"currency": updated.Currency, "lines": lines, "costSubtotal": updated.CostSubtotal,
		"excludedCostItemCount": updated.ExcludedCostItemCount,
		"pricingMode":           string(updated.PricingMode), "pricingRate": int64(updated.PricingRate),
		"proposedSellingPrice": updated.ProposedSellingPrice, "projectedGrossProfit": updated.ProjectedGrossProfit,
		"projectedGrossMarginBps": int64(updated.ProjectedGrossMarginBPS), "refreshedAt": updated.RefreshedAt,
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoEstimateRepository) UpdatePricing(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error) {
	set := bson.M{
		"pricingMode": string(updated.PricingMode), "pricingRate": int64(updated.PricingRate),
		"proposedSellingPrice": updated.ProposedSellingPrice, "projectedGrossProfit": updated.ProjectedGrossProfit,
		"projectedGrossMarginBps": int64(updated.ProjectedGrossMarginBPS),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoEstimateRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Estimate, error) {
	set := bson.M{"status": string(EstimateStatusFinalized), "finalizedAt": finalizedAt}
	// incrementRevision=false: finalize freezes Revision at its current
	// value (design spec §16.3) — see conditionalUpdate's doc comment.
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, false)
}
