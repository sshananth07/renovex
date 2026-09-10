package materialrequirements

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// Explicit index names. EVERY index carries one, with no exceptions, because
// classifyCreateError identifies a duplicate-key violation by matching the
// index name in the write-error message — an auto-generated name would make
// that matching brittle (design spec §12.1, §12.4, mirroring
// quotations/repository_mongo.go:36-41).
const (
	indexNameRequirementsCompany                 = "idx_material_requirements_company"
	indexNameRequirementsCompanyProject          = "idx_material_requirements_company_project"
	indexNameRequirementsCompanyProjectStatus    = "idx_material_requirements_company_project_status"
	indexNameRequirementsCompanyProjectSourceTyp = "idx_material_requirements_company_project_sourcetype"
	indexNameRequirementsCompanyWorkItem         = "idx_material_requirements_company_workitem"
	indexNameRequirementsCompanyMaterial         = "idx_material_requirements_company_material"
	indexNameUniqueRequirementsSourceKey         = "uq_material_requirements_source_key"
	indexNameUniqueRequirementsResolutionOp      = "uq_material_requirements_resolution_op"
	indexNameRequirementsCompanyRFQChain         = "idx_material_requirements_company_rfq_chain"
	indexNameRequirementsCompanySplitGroup       = "idx_material_requirements_company_split_group"
)

// MongoMaterialRequirementRepository is the MongoDB-backed
// MaterialRequirementRepository. It owns the "material_requirements"
// collection exclusively.
type MongoMaterialRequirementRepository struct {
	collection *mongo.Collection
}

// NewMongoMaterialRequirementRepository constructs a repository against db's
// "material_requirements" collection.
func NewMongoMaterialRequirementRepository(db *mongo.Database) *MongoMaterialRequirementRepository {
	return &MongoMaterialRequirementRepository{collection: db.Collection("material_requirements")}
}

// EnsureIndexes creates all ten indexes from design spec §12.1. Every
// tenant-scoped index leads with companyId.
func (r *MongoMaterialRequirementRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompany)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanyProject)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanyProjectStatus)},
		// Serves generation's existing-anchor load (design spec §3.4).
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}, {Key: "sourceType", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanyProjectSourceTyp)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanyWorkItem)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "materialId", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanyMaterial)},
		// The invariant that makes generation race-safe. Keyed on the IMMUTABLE
		// sourceAggregationKey, never on the contractor-editable
		// requiredQuantity.unit. Partial on sourceType=cost_item because manual
		// requirements and split children carry no aggregation key and would
		// otherwise all collide on a missing field (design spec §3.3, §12.1).
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sourceAggregationKey", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.M{"sourceType": string(SourceTypeCostItem)}).
				SetName(indexNameUniqueRequirementsSourceKey)},
		// Makes create_separate idempotent: one operation id yields at most one
		// delta child (design spec §5.7). Partial on the field existing as a
		// string, since the overwhelming majority of requirements have none.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "resolutionOperationId", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.M{"resolutionOperationId": bson.M{"$type": "string"}}).
				SetName(indexNameUniqueRequirementsResolutionOp)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "activeRfqChainId", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanyRFQChain)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "splitGroupId", Value: 1}},
			Options: options.Index().SetName(indexNameRequirementsCompanySplitGroup)},
	})
	return err
}

// quantityDoc is the explicit Mongo representation for quantity.Quantity,
// mirroring the private copies in internal/work and internal/costs.
// shopspring/decimal has no MarshalBSON/UnmarshalBSON, so Value is stored as a
// STRING — never BSON Decimal128 and never left to struct reflection, either of
// which could silently lose precision.
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

type splitChildRefDoc struct {
	RequirementID string      `bson:"requirementId"`
	Quantity      quantityDoc `bson:"quantity"`
	Sequence      int         `bson:"sequence"`
}

type requirementDoc struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	CompanyID  string        `bson:"companyId"`
	ProjectID  string        `bson:"projectId"`
	WorkItemID *string       `bson:"workItemId,omitempty"`

	MaterialID    string `bson:"materialId"`
	MaterialName  string `bson:"materialName"`
	Specification string `bson:"specification,omitempty"`

	RequiredQuantity quantityDoc `bson:"requiredQuantity"`
	CatalogUnit      string      `bson:"catalogUnit"`

	UnitMismatch             bool `bson:"unitMismatch"`
	UnitMismatchAcknowledged bool `bson:"unitMismatchAcknowledged"`

	RequiredByDate   *time.Time `bson:"requiredByDate,omitempty"`
	ProcurementNotes string     `bson:"procurementNotes,omitempty"`
	InternalNotes    string     `bson:"internalNotes,omitempty"`

	Status     string `bson:"status"`
	SourceType string `bson:"sourceType"`

	SourceAggregationUnit string `bson:"sourceAggregationUnit,omitempty"`
	SourceAggregationKey  string `bson:"sourceAggregationKey,omitempty"`

	// Accepted source snapshot (design spec §5.8).
	SourceCostItemIDs []string     `bson:"sourceCostItemIds,omitempty"`
	SourceQuantity    *quantityDoc `bson:"sourceQuantity,omitempty"`
	SourceFingerprint string       `bson:"sourceFingerprint,omitempty"`
	SourceSyncedAt    *time.Time   `bson:"sourceSyncedAt,omitempty"`

	// Detection output.
	SourceSyncState string     `bson:"sourceSyncState"`
	SourceCheckedAt *time.Time `bson:"sourceCheckedAt,omitempty"`

	SplitFromRequirementID *string            `bson:"splitFromRequirementId,omitempty"`
	SplitGroupID           *string            `bson:"splitGroupId,omitempty"`
	SplitSequence          *int               `bson:"splitSequence,omitempty"`
	SplitAt                *time.Time         `bson:"splitAt,omitempty"`
	SplitByUserID          *string            `bson:"splitByUserId,omitempty"`
	SplitState             string             `bson:"splitState,omitempty"`
	SplitChildren          []splitChildRefDoc `bson:"splitChildren,omitempty"`

	CreatedFromDiscrepancyRequirementID *string `bson:"createdFromDiscrepancyRequirementId,omitempty"`
	CreatedFromSourceFingerprint        *string `bson:"createdFromSourceFingerprint,omitempty"`
	ResolutionOperationID               *string `bson:"resolutionOperationId,omitempty"`

	ActiveRFQChainID *string    `bson:"activeRfqChainId,omitempty"`
	ActiveRFQNumber  *string    `bson:"activeRfqNumber,omitempty"`
	ActiveRFQLineID  *string    `bson:"activeRfqLineId,omitempty"`
	RFQClaimedAt     *time.Time `bson:"rfqClaimedAt,omitempty"`

	Revision        int64     `bson:"revision"`
	CreatedByUserID string    `bson:"createdByUserId,omitempty"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	SchemaVersion   int       `bson:"schemaVersion"`
}

func toRequirementDoc(r MaterialRequirement) (requirementDoc, error) {
	doc := requirementDoc{
		CompanyID: r.CompanyID, ProjectID: r.ProjectID, WorkItemID: r.WorkItemID,
		MaterialID: r.MaterialID, MaterialName: r.MaterialName, Specification: r.Specification,
		RequiredQuantity: toQuantityDoc(r.RequiredQuantity), CatalogUnit: r.CatalogUnit,
		UnitMismatch: r.UnitMismatch, UnitMismatchAcknowledged: r.UnitMismatchAcknowledged,
		RequiredByDate: r.RequiredByDate, ProcurementNotes: r.ProcurementNotes, InternalNotes: r.InternalNotes,
		Status: string(r.Status), SourceType: string(r.SourceType),
		SourceAggregationUnit: r.SourceAggregationUnit, SourceAggregationKey: r.SourceAggregationKey,
		SourceCostItemIDs: r.SourceCostItemIDs, SourceFingerprint: r.SourceFingerprint,
		SourceSyncedAt:  r.SourceSyncedAt,
		SourceSyncState: string(r.SourceSyncState), SourceCheckedAt: r.SourceCheckedAt,
		SplitFromRequirementID: r.SplitFromRequirementID, SplitGroupID: r.SplitGroupID,
		SplitSequence: r.SplitSequence, SplitAt: r.SplitAt, SplitByUserID: r.SplitByUserID,
		SplitState:                          string(r.SplitState),
		CreatedFromDiscrepancyRequirementID: r.CreatedFromDiscrepancyRequirementID,
		CreatedFromSourceFingerprint:        r.CreatedFromSourceFingerprint,
		ResolutionOperationID:               r.ResolutionOperationID,
		ActiveRFQChainID:                    r.ActiveRFQChainID, ActiveRFQNumber: r.ActiveRFQNumber,
		ActiveRFQLineID: r.ActiveRFQLineID, RFQClaimedAt: r.RFQClaimedAt,
		Revision: r.Revision, CreatedByUserID: r.CreatedByUserID,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, SchemaVersion: r.SchemaVersion,
	}
	if r.SourceQuantity != nil {
		qd := toQuantityDoc(*r.SourceQuantity)
		doc.SourceQuantity = &qd
	}
	for _, c := range r.SplitChildren {
		doc.SplitChildren = append(doc.SplitChildren, splitChildRefDoc{
			RequirementID: c.RequirementID, Quantity: toQuantityDoc(c.Quantity), Sequence: c.Sequence,
		})
	}
	if r.ID != "" {
		objID, err := bson.ObjectIDFromHex(r.ID)
		if err != nil {
			return requirementDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromRequirementDoc(doc requirementDoc) (MaterialRequirement, error) {
	requiredQty, err := fromQuantityDoc(doc.RequiredQuantity)
	if err != nil {
		return MaterialRequirement{}, err
	}

	r := MaterialRequirement{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, WorkItemID: doc.WorkItemID,
		MaterialID: doc.MaterialID, MaterialName: doc.MaterialName, Specification: doc.Specification,
		RequiredQuantity: requiredQty, CatalogUnit: doc.CatalogUnit,
		UnitMismatch: doc.UnitMismatch, UnitMismatchAcknowledged: doc.UnitMismatchAcknowledged,
		RequiredByDate: doc.RequiredByDate, ProcurementNotes: doc.ProcurementNotes,
		InternalNotes: doc.InternalNotes,
		Status:        RequirementStatus(doc.Status), SourceType: SourceType(doc.SourceType),
		SourceAggregationUnit: doc.SourceAggregationUnit, SourceAggregationKey: doc.SourceAggregationKey,
		SourceCostItemIDs: doc.SourceCostItemIDs, SourceFingerprint: doc.SourceFingerprint,
		SourceSyncedAt:  doc.SourceSyncedAt,
		SourceSyncState: SourceSyncState(doc.SourceSyncState), SourceCheckedAt: doc.SourceCheckedAt,
		SplitFromRequirementID: doc.SplitFromRequirementID, SplitGroupID: doc.SplitGroupID,
		SplitSequence: doc.SplitSequence, SplitAt: doc.SplitAt, SplitByUserID: doc.SplitByUserID,
		SplitState:                          SplitState(doc.SplitState),
		CreatedFromDiscrepancyRequirementID: doc.CreatedFromDiscrepancyRequirementID,
		CreatedFromSourceFingerprint:        doc.CreatedFromSourceFingerprint,
		ResolutionOperationID:               doc.ResolutionOperationID,
		ActiveRFQChainID:                    doc.ActiveRFQChainID, ActiveRFQNumber: doc.ActiveRFQNumber,
		ActiveRFQLineID: doc.ActiveRFQLineID, RFQClaimedAt: doc.RFQClaimedAt,
		Revision: doc.Revision, CreatedByUserID: doc.CreatedByUserID,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
	if doc.SourceQuantity != nil {
		q, err := fromQuantityDoc(*doc.SourceQuantity)
		if err != nil {
			return MaterialRequirement{}, err
		}
		r.SourceQuantity = &q
	}
	for _, c := range doc.SplitChildren {
		q, err := fromQuantityDoc(c.Quantity)
		if err != nil {
			return MaterialRequirement{}, err
		}
		r.SplitChildren = append(r.SplitChildren, SplitChildRef{
			RequirementID: c.RequirementID, Quantity: q, Sequence: c.Sequence,
		})
	}
	return r, nil
}

// classifyCreateError translates a raw duplicate-key error into one of the two
// named sentinels by matching the EXPLICIT index name in the write error.
//
// A duplicate key that matches neither known index returns
// ErrUnclassifiedDuplicateKey — NOT a specific sentinel. An error this function
// cannot positively identify must not be assumed safe to treat as an
// already-applied operation (design spec §12.4, mirroring
// quotations.classifyCreateError).
func classifyCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			switch {
			case strings.Contains(we.Message, indexNameUniqueRequirementsSourceKey):
				return ErrSourceAggregationKeyExists
			case strings.Contains(we.Message, indexNameUniqueRequirementsResolutionOp):
				return ErrResolutionOperationAlreadyApplied
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoMaterialRequirementRepository) Create(ctx context.Context, req MaterialRequirement) (MaterialRequirement, error) {
	doc, err := toRequirementDoc(req)
	if err != nil {
		return MaterialRequirement{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return MaterialRequirement{}, classifyCreateError(err)
	}
	req.ID = res.InsertedID.(bson.ObjectID).Hex()
	return req, nil
}

// CreateWithID inserts at a caller-supplied _id so split children are
// idempotent under retry: the primary key, not an application check, is what
// forbids a duplicate (design spec §8.4).
func (r *MongoMaterialRequirementRepository) CreateWithID(ctx context.Context, req MaterialRequirement) (MaterialRequirement, error) {
	objID, err := bson.ObjectIDFromHex(req.ID)
	if err != nil {
		return MaterialRequirement{}, ErrMaterialRequirementNotFound
	}
	doc, err := toRequirementDoc(req)
	if err != nil {
		return MaterialRequirement{}, err
	}
	doc.ID = objID

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// A duplicate _id means this child already exists — the retry case.
			// Any OTHER duplicate index is classified normally.
			if classified := classifyCreateError(err); !errors.Is(classified, ErrUnclassifiedDuplicateKey) {
				return MaterialRequirement{}, classified
			}
			return MaterialRequirement{}, ErrRequirementIDExists
		}
		return MaterialRequirement{}, err
	}
	return req, nil
}

// BeginSplit is step 1 of §8.4. Status, split provenance and the child manifest
// are written in ONE conditional update, so a crash can never leave a source
// marked split with no record of the children it owes.
//
// Every §8.3 precondition that can be raced lives in the FILTER, not in the
// service: a concurrent claim or a competing split loses here rather than
// racing past a service-level check.
func (r *MongoMaterialRequirementRepository) BeginSplit(ctx context.Context, companyID, id string,
	expectedRevision int64, manifest []SplitChildRef, splitGroupID, actorUserID string,
	splitAt time.Time) (MaterialRequirement, error) {

	refs := make([]splitChildRefDoc, 0, len(manifest))
	for _, c := range manifest {
		refs = append(refs, splitChildRefDoc{
			RequirementID: c.RequirementID,
			Quantity:      toQuantityDoc(c.Quantity),
			Sequence:      c.Sequence,
		})
	}

	extraFilter := bson.M{
		"status":           bson.M{"$in": []string{string(RequirementStatusDraft), string(RequirementStatusReviewed)}},
		"sourceType":       bson.M{"$ne": string(SourceTypeSplit)},
		"activeRfqChainId": nil,
	}
	set := bson.M{
		"status":        string(RequirementStatusSplit),
		"splitAt":       splitAt,
		"splitByUserId": actorUserID,
		"splitGroupId":  splitGroupID,
		"splitState":    string(SplitStateCreating),
		"splitChildren": refs,
		"updatedAt":     time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// CompleteSplit is step 3: creating -> completed, once every manifest child
// exists. The splitState filter makes a duplicate completion a no-match rather
// than a silent re-write.
func (r *MongoMaterialRequirementRepository) CompleteSplit(ctx context.Context, companyID, id string,
	expectedRevision int64) (MaterialRequirement, error) {
	extraFilter := bson.M{"splitState": string(SplitStateCreating)}
	set := bson.M{
		"splitState": string(SplitStateCompleted),
		"updatedAt":  time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// ListSplitChildren loads one split group, backed by the
// {companyId, splitGroupId} index.
func (r *MongoMaterialRequirementRepository) ListSplitChildren(ctx context.Context,
	companyID, splitGroupID string) ([]MaterialRequirement, error) {
	return r.find(ctx, bson.M{"companyId": companyID, "splitGroupId": splitGroupID,
		"sourceType": string(SourceTypeSplit)})
}

func (r *MongoMaterialRequirementRepository) FindByID(ctx context.Context, companyID, id string) (MaterialRequirement, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		// A malformed id is reported as not-found so the handler maps it to 404
		// rather than 500.
		return MaterialRequirement{}, ErrMaterialRequirementNotFound
	}
	var doc requirementDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return MaterialRequirement{}, ErrMaterialRequirementNotFound
	}
	if err != nil {
		return MaterialRequirement{}, err
	}
	return fromRequirementDoc(doc)
}

// FindByResolutionOperationID loads the child created by a previous
// create_separate. It is scoped by companyId as well as the operation ID, so a
// foreign tenant's operation ID can never resolve to a document here — the
// unique index is {companyId, resolutionOperationId}, and this read matches it
// (design spec §5.7, §12.1).
func (r *MongoMaterialRequirementRepository) FindByResolutionOperationID(ctx context.Context,
	companyID, operationID string) (MaterialRequirement, error) {
	var doc requirementDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "resolutionOperationId": operationID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return MaterialRequirement{}, ErrMaterialRequirementNotFound
	}
	if err != nil {
		return MaterialRequirement{}, err
	}
	return fromRequirementDoc(doc)
}

func (r *MongoMaterialRequirementRepository) find(ctx context.Context, filter bson.M) ([]MaterialRequirement, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []requirementDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]MaterialRequirement, 0, len(docs))
	for _, doc := range docs {
		req, err := fromRequirementDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, req)
	}
	return result, nil
}

// DeleteAllForCompany permanently removes every MaterialRequirement owned
// by companyID. Never errors when zero documents match. Development-tool
// use only.
func (r *MongoMaterialRequirementRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoMaterialRequirementRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]MaterialRequirement, error) {
	return r.find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
}

// ListGeneratedAnchorsByProject deliberately applies NO status filter: the
// union algorithm must see split and archived anchors, or it would recreate
// demand the contractor had already split or retired (design spec §3.4).
func (r *MongoMaterialRequirementRepository) ListGeneratedAnchorsByProject(ctx context.Context, companyID, projectID string) ([]MaterialRequirement, error) {
	return r.find(ctx, bson.M{
		"companyId": companyID, "projectId": projectID,
		"sourceType": string(SourceTypeCostItem),
	})
}

// conditionalUpdate applies set against a document matching baseFilter plus the
// expected revision, bumping revision by one. On a 0-match it re-reads to
// distinguish a missing/foreign document (404) from a state or revision
// conflict (409) — the pattern established by
// quotations/repository_mongo.go:284-307.
func (r *MongoMaterialRequirementRepository) conditionalUpdate(ctx context.Context, companyID, id string,
	expectedRevision int64, extraFilter bson.M, set bson.M) (MaterialRequirement, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return MaterialRequirement{}, ErrMaterialRequirementNotFound
	}

	filter := bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision}
	for k, v := range extraFilter {
		filter[k] = v
	}
	set["revision"] = expectedRevision + 1

	res, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return MaterialRequirement{}, err
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrMaterialRequirementNotFound) {
			return MaterialRequirement{}, ErrMaterialRequirementNotFound
		}
		return MaterialRequirement{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

// Delete permanently removes a document matching companyID/id/expectedRevision.
// The 0-match classification mirrors conditionalUpdate: a genuinely missing/
// foreign document is ErrMaterialRequirementNotFound, anything else at that
// revision is ErrRevisionMismatch (the document existed but moved under the
// caller, e.g. some other write raced this delete).
func (r *MongoMaterialRequirementRepository) Delete(ctx context.Context, companyID, id string,
	expectedRevision int64) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrMaterialRequirementNotFound
	}

	filter := bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision}
	res, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrMaterialRequirementNotFound) {
			return ErrMaterialRequirementNotFound
		}
		return ErrRevisionMismatch
	}
	return nil
}

// UpdateContractorFields writes only contractor-controlled fields. The filter
// requires a non-terminal, unclaimed requirement, so §2.1's read-only rule and
// §2.3's claimed-immutability rule are enforced by the write itself rather than
// only by the service.
//
// The accepted source snapshot and the claim fields are absent from the $set by
// construction (design spec §5.8).
func (r *MongoMaterialRequirementRepository) UpdateContractorFields(ctx context.Context, companyID, id string,
	expectedRevision int64, updated MaterialRequirement) (MaterialRequirement, error) {
	extraFilter := bson.M{
		"status":           bson.M{"$in": []string{string(RequirementStatusDraft), string(RequirementStatusReviewed)}},
		"activeRfqChainId": nil,
	}
	set := bson.M{
		"materialId":               updated.MaterialID,
		"materialName":             updated.MaterialName,
		"specification":            updated.Specification,
		"requiredQuantity":         toQuantityDoc(updated.RequiredQuantity),
		"catalogUnit":              updated.CatalogUnit,
		"unitMismatch":             updated.UnitMismatch,
		"unitMismatchAcknowledged": updated.UnitMismatchAcknowledged,
		"requiredByDate":           updated.RequiredByDate,
		"procurementNotes":         updated.ProcurementNotes,
		"internalNotes":            updated.InternalNotes,
		"status":                   string(updated.Status),
		"workItemId":               updated.WorkItemID,
		"updatedAt":                time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// UpdateSyncState converges detection output only. The $set contains exactly
// two business fields, so the accepted snapshot cannot be disturbed even by a
// caller that passes a stale requirement. No status filter applies: split and
// archived anchors remain sync anchors (design spec §3.4, §5.8).
func (r *MongoMaterialRequirementRepository) UpdateSyncState(ctx context.Context, companyID, id string,
	expectedRevision int64, state SourceSyncState, checkedAt time.Time) (MaterialRequirement, error) {
	set := bson.M{
		"sourceSyncState": string(state),
		"sourceCheckedAt": checkedAt,
		"updatedAt":       time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, nil, set)
}

// eligibilityFilter expresses the §8.5 predicate as BSON, so the claim's
// conditional update enforces it ATOMICALLY rather than after a separate read
// that a concurrent edit could invalidate.
//
// It deliberately mirrors MaterialRequirement.IsRFQEligible term for term; the
// two are asserted to agree by the claim tests.
func eligibilityFilter() bson.M {
	return bson.M{
		"status":           string(RequirementStatusReviewed),
		"activeRfqChainId": nil,
		"$or": []bson.M{
			{"unitMismatch": false},
			{"unitMismatchAcknowledged": true},
		},
		"sourceSyncState": string(SourceSyncStateClean),
		// The positive-quantity term. Write-time validation alone would leave
		// this filter weaker than the approved predicate: parseQuantity cannot
		// protect against malformed legacy documents, direct database writes,
		// migrations, or a repository defect. The claim must enforce the
		// invariant itself.
		"$expr": positiveQuantityExpr(),
	}
}

// positiveQuantityExpr converts the stored decimal STRING to Decimal128 and
// requires it to exceed zero.
//
// requiredQuantity.value is persisted as a canonical decimal string (ADR 0001),
// so an ordinary numeric comparison cannot see it. $convert with explicit
// onError/onNull results makes malformed or missing data compare as zero —
// therefore NOT claimable — rather than raising a query error that would turn a
// bad document into a 500 for an unrelated caller.
//
// The claim query already targets _id, so this expression is evaluated against
// a single document and raises no indexing concern.
func positiveQuantityExpr() bson.M {
	zero, _ := bson.ParseDecimal128("0")
	return bson.M{
		"$gt": []any{
			bson.M{"$convert": bson.M{
				"input":   "$requiredQuantity.value",
				"to":      "decimal",
				"onError": zero,
				"onNull":  zero,
			}},
			zero,
		},
	}
}

// ClaimForRFQ is §7.2 step 3: ONE conditional update carrying company, project,
// revision, the unclaimed requirement and the full eligibility predicate.
//
// projectId sits in this same filter rather than in a pre-check, so a
// requirement belonging to another Project of the same company matches zero
// documents and cross-project contamination of an RFQ is structurally
// impossible.
func (r *MongoMaterialRequirementRepository) ClaimForRFQ(ctx context.Context, companyID, projectID, id string,
	expectedRevision int64, rfqChainID, rfqNumber, lineID string) (MaterialRequirement, error) {

	extraFilter := eligibilityFilter()
	extraFilter["projectId"] = projectID

	set := bson.M{
		"activeRfqChainId": rfqChainID,
		"activeRfqNumber":  rfqNumber,
		"activeRfqLineId":  lineID,
		"rfqClaimedAt":     time.Now(),
		"updatedAt":        time.Now(),
	}

	claimed, err := r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
	if err == nil {
		return claimed, nil
	}
	if !errors.Is(err, ErrRevisionMismatch) {
		return MaterialRequirement{}, err
	}

	// 0 matched. Re-read to report WHY, because these map to different statuses
	// and the contractor's next step differs for each (design spec §7.2).
	current, findErr := r.FindByID(ctx, companyID, id)
	if errors.Is(findErr, ErrMaterialRequirementNotFound) {
		return MaterialRequirement{}, ErrMaterialRequirementNotFound
	}
	if findErr != nil {
		// The document exists but will not decode — a persisted quantity that
		// is not a valid decimal is the realistic cause, and it is exactly what
		// the $expr guard above already refused to claim. Reporting "not
		// eligible" rather than propagating a decode error keeps a malformed
		// legacy document from surfacing as a 500 on an ordinary claim attempt.
		return MaterialRequirement{}, ErrRequirementNotEligibleForRFQ
	}
	return MaterialRequirement{}, classifyClaimFailure(current, projectID, expectedRevision)
}

// classifyClaimFailure explains a 0-match claim. The order matters: a
// wrong-project requirement must look like a 404 rather than leaking that it
// exists in another project of the same company.
func classifyClaimFailure(current MaterialRequirement, projectID string, expectedRevision int64) error {
	if current.ProjectID != projectID {
		return ErrMaterialRequirementNotFound
	}
	if current.IsClaimed() {
		return ErrMaterialRequirementAlreadyClaimed
	}
	if !current.IsRFQEligible() {
		return ErrRequirementNotEligibleForRFQ
	}
	if current.Revision != expectedRevision {
		return ErrRevisionMismatch
	}
	return ErrRevisionMismatch
}

// ReleaseClaim clears a claim only when the caller names the exact chain AND
// line it believes it holds (design spec §7.4).
func (r *MongoMaterialRequirementRepository) ReleaseClaim(ctx context.Context, companyID, id string,
	expectedRevision int64, rfqChainID, lineID string) (MaterialRequirement, error) {
	extraFilter := bson.M{
		"activeRfqChainId": rfqChainID,
		"activeRfqLineId":  lineID,
	}
	set := bson.M{
		"activeRfqChainId": nil,
		"activeRfqNumber":  nil,
		"activeRfqLineId":  nil,
		"rfqClaimedAt":     nil,
		"updatedAt":        time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}

// ListClaimsForRFQChain enumerates one chain's claims, backed by the
// {companyId, activeRfqChainId} index.
func (r *MongoMaterialRequirementRepository) ListClaimsForRFQChain(ctx context.Context,
	companyID, rfqChainID string) ([]MaterialRequirement, error) {
	return r.find(ctx, bson.M{"companyId": companyID, "activeRfqChainId": rfqChainID})
}

// ApplyDiscrepancyResolution writes the accepted source snapshot together with
// the contractor fields a resolution may change, in one conditional update.
//
// This is the counterpart to UpdateSyncState: exactly one of the two may touch
// the snapshot, and it is this one (design spec §5.8). The filter rejects a
// claimed requirement but permits terminal ones, because keep_current is valid
// on a split or archived anchor (design spec §5.6).
//
// SourceAggregationKey and SourceAggregationUnit are deliberately absent: the
// aggregation identity is immutable, and rewriting the unit would orphan the
// anchor from its own key (design spec §3.3).
func (r *MongoMaterialRequirementRepository) ApplyDiscrepancyResolution(ctx context.Context, companyID, id string,
	expectedRevision int64, resolved MaterialRequirement) (MaterialRequirement, error) {
	extraFilter := bson.M{"activeRfqChainId": nil}
	set := bson.M{
		// Accepted snapshot.
		"sourceCostItemIds": resolved.SourceCostItemIDs,
		"sourceFingerprint": resolved.SourceFingerprint,
		"sourceSyncedAt":    resolved.SourceSyncedAt,
		// Detection output.
		"sourceSyncState": string(resolved.SourceSyncState),
		"sourceCheckedAt": resolved.SourceCheckedAt,
		// Contractor fields a resolution may move (design spec §5.6).
		"requiredQuantity":         toQuantityDoc(resolved.RequiredQuantity),
		"unitMismatch":             resolved.UnitMismatch,
		"unitMismatchAcknowledged": resolved.UnitMismatchAcknowledged,
		"status":                   string(resolved.Status),
		"updatedAt":                time.Now(),
	}
	if resolved.SourceQuantity != nil {
		set["sourceQuantity"] = toQuantityDoc(*resolved.SourceQuantity)
	} else {
		set["sourceQuantity"] = nil
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, extraFilter, set)
}
