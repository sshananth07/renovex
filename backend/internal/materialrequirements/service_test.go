package materialrequirements_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// --- fakes ---

// fakeRequirementRepo reproduces the invariants the real repository enforces in
// its conditional filters, not merely storage: the Revision guard, the
// terminal/claimed rejection in UpdateContractorFields, and the fact that
// UpdateSyncState touches nothing but the two detection fields. A fake that
// only stored documents would let a service bug pass here and fail in B1's
// Docker tests.
type fakeRequirementRepo struct {
	byID map[string]mr.MaterialRequirement
	next int

	// syncStateCalls records every UpdateSyncState invocation so tests can
	// assert detection never runs where it should not.
	syncStateCalls int

	// createCalls records every Create attempt, including ones the unique-key
	// check rejects. It lets tests assert HOW an outcome was reached, not just
	// that the counts matched — a duplicate-key rejection and a correct
	// existing-anchor lookup can otherwise report identical results.
	createCalls int

	// resolutionCalls records every ApplyDiscrepancyResolution invocation, so a
	// test can assert that a REJECTED resolution wrote nothing at all rather
	// than merely that the stored values happened to match.
	resolutionCalls int

	// failCreate, when set, is returned from CreateWithID once failCreateAfter
	// successful inserts have happened. It lets a test interrupt a split
	// part-way and exercise §8.4's recovery path.
	failCreate      error
	failCreateAfter int
}

func newFakeRepo() *fakeRequirementRepo {
	return &fakeRequirementRepo{byID: map[string]mr.MaterialRequirement{}}
}

func (f *fakeRequirementRepo) Create(_ context.Context, r mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	f.createCalls++
	// Mirror uq_material_requirements_source_key (partial on cost_item).
	if r.SourceType == mr.SourceTypeCostItem && r.SourceAggregationKey != "" {
		for _, existing := range f.byID {
			if existing.CompanyID == r.CompanyID &&
				existing.SourceType == mr.SourceTypeCostItem &&
				existing.SourceAggregationKey == r.SourceAggregationKey {
				return mr.MaterialRequirement{}, mr.ErrSourceAggregationKeyExists
			}
		}
	}
	// Mirror uq_material_requirements_resolution_op.
	if r.ResolutionOperationID != nil {
		for _, existing := range f.byID {
			if existing.CompanyID == r.CompanyID && existing.ResolutionOperationID != nil &&
				*existing.ResolutionOperationID == *r.ResolutionOperationID {
				return mr.MaterialRequirement{}, mr.ErrResolutionOperationAlreadyApplied
			}
		}
	}
	f.next++
	r.ID = "mr_" + decimal.NewFromInt(int64(f.next)).String()
	f.byID[r.ID] = r
	return r, nil
}

func (f *fakeRequirementRepo) FindByID(_ context.Context, companyID, id string) (mr.MaterialRequirement, error) {
	r, ok := f.byID[id]
	if !ok || r.CompanyID != companyID {
		return mr.MaterialRequirement{}, mr.ErrMaterialRequirementNotFound
	}
	return r, nil
}

func (f *fakeRequirementRepo) ListByProject(_ context.Context, companyID, projectID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRequirementRepo) ListGeneratedAnchorsByProject(_ context.Context, companyID, projectID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.ProjectID == projectID && r.SourceType == mr.SourceTypeCostItem {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRequirementRepo) UpdateContractorFields(ctx context.Context, companyID, id string,
	expectedRevision int64, updated mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	// Mirror the real conditional filter: terminal or claimed matches nothing.
	if stored.IsTerminal() || stored.IsClaimed() {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}

	// Only contractor-controlled fields are copied; the accepted snapshot and
	// claim fields survive from the stored document (design spec §5.8).
	stored.MaterialID = updated.MaterialID
	stored.MaterialName = updated.MaterialName
	stored.Specification = updated.Specification
	stored.RequiredQuantity = updated.RequiredQuantity
	stored.CatalogUnit = updated.CatalogUnit
	stored.UnitMismatch = updated.UnitMismatch
	stored.UnitMismatchAcknowledged = updated.UnitMismatchAcknowledged
	stored.RequiredByDate = updated.RequiredByDate
	stored.ProcurementNotes = updated.ProcurementNotes
	stored.InternalNotes = updated.InternalNotes
	stored.Status = updated.Status
	stored.WorkItemID = updated.WorkItemID
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()

	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRequirementRepo) Delete(ctx context.Context, companyID, id string, expectedRevision int64) error {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return err
	}
	if stored.Revision != expectedRevision {
		return mr.ErrRevisionMismatch
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeRequirementRepo) UpdateSyncState(ctx context.Context, companyID, id string,
	expectedRevision int64, state mr.SourceSyncState, checkedAt time.Time) (mr.MaterialRequirement, error) {
	f.syncStateCalls++
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	stored.SourceSyncState = state
	stored.SourceCheckedAt = &checkedAt
	stored.Revision = expectedRevision + 1
	f.byID[id] = stored
	return stored, nil
}

// ApplyDiscrepancyResolution mirrors the real conditional filter: claimed is
// rejected, terminal is NOT (keep_current is valid on a terminal anchor), and
// the $set covers the accepted snapshot plus the contractor fields a resolution
// may move — nothing else (design spec §5.6, §5.8).
func (f *fakeRequirementRepo) ApplyDiscrepancyResolution(ctx context.Context, companyID, id string,
	expectedRevision int64, resolved mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	f.resolutionCalls++
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.IsClaimed() {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}

	stored.SourceCostItemIDs = resolved.SourceCostItemIDs
	stored.SourceQuantity = resolved.SourceQuantity
	stored.SourceFingerprint = resolved.SourceFingerprint
	stored.SourceSyncedAt = resolved.SourceSyncedAt
	stored.SourceSyncState = resolved.SourceSyncState
	stored.SourceCheckedAt = resolved.SourceCheckedAt
	stored.RequiredQuantity = resolved.RequiredQuantity
	stored.UnitMismatch = resolved.UnitMismatch
	stored.UnitMismatchAcknowledged = resolved.UnitMismatchAcknowledged
	stored.Status = resolved.Status
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()

	f.byID[id] = stored
	return stored, nil
}

// CreateWithID mirrors the real primary-key behaviour: a second insert at the
// same _id is rejected, which is what makes §8.4's manifest replay idempotent.
func (f *fakeRequirementRepo) CreateWithID(_ context.Context, r mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	f.createCalls++
	if _, exists := f.byID[r.ID]; exists {
		return mr.MaterialRequirement{}, mr.ErrRequirementIDExists
	}
	if f.failCreate != nil {
		if f.failCreateAfter > 0 {
			f.failCreateAfter--
		} else {
			return mr.MaterialRequirement{}, f.failCreate
		}
	}
	f.byID[r.ID] = r
	return r, nil
}

// BeginSplit mirrors the real conditional filter: the manifest and the status
// transition land TOGETHER, and every raceable §8.3 precondition is enforced
// here rather than only in the service.
func (f *fakeRequirementRepo) BeginSplit(ctx context.Context, companyID, id string,
	expectedRevision int64, manifest []mr.SplitChildRef, splitGroupID, actorUserID string,
	splitAt time.Time) (mr.MaterialRequirement, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.IsTerminal() || stored.IsClaimed() || stored.SourceType == mr.SourceTypeSplit {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}

	stored.Status = mr.RequirementStatusSplit
	stored.SplitAt = &splitAt
	stored.SplitByUserID = &actorUserID
	stored.SplitGroupID = &splitGroupID
	stored.SplitState = mr.SplitStateCreating
	stored.SplitChildren = manifest
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()

	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRequirementRepo) CompleteSplit(ctx context.Context, companyID, id string,
	expectedRevision int64) (mr.MaterialRequirement, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.SplitState != mr.SplitStateCreating {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	stored.SplitState = mr.SplitStateCompleted
	stored.Revision = expectedRevision + 1
	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRequirementRepo) ListSplitChildren(_ context.Context,
	companyID, splitGroupID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.SourceType == mr.SourceTypeSplit &&
			r.SplitGroupID != nil && *r.SplitGroupID == splitGroupID {
			out = append(out, r)
		}
	}
	return out, nil
}

// ClaimForRFQ mirrors the real ATOMIC filter: project, revision, unclaimed and
// the whole §8.5 eligibility predicate are one condition, and the 0-match case
// is classified into the same four sentinels (design spec §7.2).
func (f *fakeRequirementRepo) ClaimForRFQ(ctx context.Context, companyID, projectID, id string,
	expectedRevision int64, rfqChainID, rfqNumber, lineID string) (mr.MaterialRequirement, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}

	matched := stored.ProjectID == projectID &&
		stored.Revision == expectedRevision &&
		!stored.IsClaimed() &&
		stored.IsRFQEligible()

	if !matched {
		// A wrong-project requirement reports not-found rather than leaking
		// that it exists in another project of the same company.
		if stored.ProjectID != projectID {
			return mr.MaterialRequirement{}, mr.ErrMaterialRequirementNotFound
		}
		if stored.IsClaimed() {
			return mr.MaterialRequirement{}, mr.ErrMaterialRequirementAlreadyClaimed
		}
		if !stored.IsRFQEligible() {
			return mr.MaterialRequirement{}, mr.ErrRequirementNotEligibleForRFQ
		}
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}

	now := time.Now()
	stored.ActiveRFQChainID = &rfqChainID
	stored.ActiveRFQNumber = &rfqNumber
	stored.ActiveRFQLineID = &lineID
	stored.RFQClaimedAt = &now
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = now

	f.byID[id] = stored
	return stored, nil
}

// ReleaseClaim requires the EXACT chain and line as well as the revision, so a
// stale release cannot free a requirement another RFQ holds (design spec §7.4).
func (f *fakeRequirementRepo) ReleaseClaim(ctx context.Context, companyID, id string,
	expectedRevision int64, rfqChainID, lineID string) (mr.MaterialRequirement, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.ActiveRFQChainID == nil || *stored.ActiveRFQChainID != rfqChainID ||
		stored.ActiveRFQLineID == nil || *stored.ActiveRFQLineID != lineID ||
		stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}

	stored.ActiveRFQChainID = nil
	stored.ActiveRFQNumber = nil
	stored.ActiveRFQLineID = nil
	stored.RFQClaimedAt = nil
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()

	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRequirementRepo) ListClaimsForRFQChain(_ context.Context,
	companyID, rfqChainID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.ActiveRFQChainID != nil && *r.ActiveRFQChainID == rfqChainID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRequirementRepo) FindByResolutionOperationID(_ context.Context,
	companyID, operationID string) (mr.MaterialRequirement, error) {
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.ResolutionOperationID != nil &&
			*r.ResolutionOperationID == operationID {
			return r, nil
		}
	}
	return mr.MaterialRequirement{}, mr.ErrMaterialRequirementNotFound
}

type fakeProjectLookup struct{ projects map[string]string } // projectID -> companyID

func (f fakeProjectLookup) ProjectBelongsToCompany(_ context.Context, companyID, projectID string) (bool, error) {
	return f.projects[projectID] == companyID, nil
}

type fakeWorkItemLookup struct {
	// key: companyID|workItemID|projectID
	found     map[string]bool
	cancelled map[string]bool
}

func (f fakeWorkItemLookup) WorkItemProcurementContext(_ context.Context, companyID, workItemID, projectID string) (bool, bool, error) {
	k := companyID + "|" + workItemID + "|" + projectID
	if !f.found[k] {
		return false, false, nil
	}
	return f.cancelled[k], true, nil
}

type fakeMaterialLookup struct {
	// key: companyID|materialID
	materials map[string]fakeMaterial
}

type fakeMaterial struct {
	name, unit, specification string
}

func (f fakeMaterialLookup) GetMaterialReference(_ context.Context, companyID, materialID string) (string, string, string, bool, error) {
	m, ok := f.materials[companyID+"|"+materialID]
	if !ok {
		return "", "", "", false, nil
	}
	return m.name, m.unit, m.specification, true, nil
}

type recordedAudit struct {
	created, updated, reviewed, acknowledged, archived, deleted, generated int
	lastReviewReset                                                        bool
}

type fakeAudit struct{ rec *recordedAudit }

func (f fakeAudit) RecordMaterialRequirementsGenerated(context.Context, string, string, string, int, int, int, int, int) error {
	f.rec.generated++
	return nil
}
func (f fakeAudit) RecordMaterialRequirementCreated(_ context.Context, _, _, _, _, _, _ string) error {
	f.rec.created++
	return nil
}
func (f fakeAudit) RecordMaterialRequirementUpdated(_ context.Context, _, _, _, _ string, reviewReset bool) error {
	f.rec.updated++
	f.rec.lastReviewReset = reviewReset
	return nil
}
func (f fakeAudit) RecordMaterialRequirementReviewed(context.Context, string, string, string, string, string) error {
	f.rec.reviewed++
	return nil
}
func (f fakeAudit) RecordMaterialRequirementUnitAcknowledged(context.Context, string, string, string, string, string, string) error {
	f.rec.acknowledged++
	return nil
}
func (f fakeAudit) RecordMaterialRequirementDiscrepancyResolved(context.Context, string, string, string, string, string, string, string) error {
	return nil
}
func (f fakeAudit) RecordMaterialRequirementSplit(context.Context, string, string, string, string, string, int) error {
	return nil
}
func (f fakeAudit) RecordMaterialRequirementArchived(context.Context, string, string, string, string) error {
	f.rec.archived++
	return nil
}
func (f fakeAudit) RecordMaterialRequirementDeleted(context.Context, string, string, string, string) error {
	f.rec.deleted++
	return nil
}

// newService wires a service where project_1 belongs to company_a, work_1 is a
// live work item in project_1, and material_1/material_2 exist in the catalog.
func newService(t *testing.T) (*mr.Service, *fakeRequirementRepo, *recordedAudit) {
	t.Helper()
	repo := newFakeRepo()
	rec := &recordedAudit{}
	svc := mr.NewService(repo,
		fakeProjectLookup{projects: map[string]string{"project_1": "company_a", "project_2": "company_a"}},
		fakeWorkItemLookup{
			found: map[string]bool{
				"company_a|work_1|project_1": true,
				"company_a|work_2|project_1": true,
				"company_a|work_x|project_1": true,
			},
			cancelled: map[string]bool{"company_a|work_x|project_1": true},
		},
		fakeMaterialLookup{materials: map[string]fakeMaterial{
			"company_a|material_1": {name: "Portland Cement", unit: "bag", specification: "OPC 50kg"},
			"company_a|material_2": {name: "River Sand", unit: "m3", specification: "washed"},
		}},
		&fakeCostSource{rows: map[string][]costRow{}},
		fakeAudit{rec: rec},
	)
	return svc, repo, rec
}

func mustQty(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("bad quantity: %v", err)
	}
	return q
}

// createManual is the happy-path manual creation used as a fixture.
func createManual(t *testing.T, svc *mr.Service, workItemID *string) mr.MaterialRequirement {
	t.Helper()
	got, err := svc.CreateManualRequirement(context.Background(), "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: workItemID, MaterialID: "material_1",
			QuantityValue: "100", QuantityUnit: "bag",
		})
	if err != nil {
		t.Fatalf("unexpected error creating manual requirement: %v", err)
	}
	return got
}

// --- Manual creation (design spec §2, §3.5) ---

func TestCreateManualRequirementSnapshotsCatalogFields(t *testing.T) {
	svc, _, rec := newService(t)
	workItemID := "work_1"

	got := createManual(t, svc, &workItemID)

	if got.SourceType != mr.SourceTypeManual {
		t.Errorf("SourceType = %q, want manual", got.SourceType)
	}
	if got.Status != mr.RequirementStatusDraft {
		t.Errorf("Status = %q, want draft — review is the confirmation gate", got.Status)
	}
	if got.MaterialName != "Portland Cement" {
		t.Errorf("MaterialName = %q, want the catalog name", got.MaterialName)
	}
	if got.CatalogUnit != "bag" {
		t.Errorf("CatalogUnit = %q, want bag", got.CatalogUnit)
	}
	if got.Specification != "OPC 50kg" {
		t.Errorf("Specification = %q, want the seeded catalog specification", got.Specification)
	}
	// A manual requirement has no CostItem aggregate, so it is permanently clean.
	if got.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean", got.SourceSyncState)
	}
	if got.SourceAggregationKey != "" || got.SourceAggregationUnit != "" {
		t.Error("a manual requirement must carry no aggregation identity")
	}
	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
	if rec.created != 1 {
		t.Errorf("audit created = %d, want 1", rec.created)
	}
}

// A manual project-level requirement legitimately has no WorkItem.
func TestCreateManualRequirementAllowsNoWorkItem(t *testing.T) {
	svc, _, _ := newService(t)

	got := createManual(t, svc, nil)
	if got.WorkItemID != nil {
		t.Fatalf("WorkItemID = %v, want nil for project-level demand", *got.WorkItemID)
	}
}

// The procurement unit may differ from the catalog unit; it is flagged, never
// converted (design spec §3.5).
func TestCreateManualRequirementFlagsUnitMismatch(t *testing.T) {
	svc, _, _ := newService(t)

	got, err := svc.CreateManualRequirement(context.Background(), "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", MaterialID: "material_1",
			QuantityValue: "500", QuantityUnit: "kg", // catalog unit is "bag"
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.UnitMismatch {
		t.Error("UnitMismatch = false, want true when the procurement unit differs from the catalog unit")
	}
	if got.UnitMismatchAcknowledged {
		t.Error("a fresh mismatch must start unacknowledged")
	}
	// No conversion: the contractor's unit and value are preserved verbatim.
	if got.RequiredQuantity.Unit != "kg" || !got.RequiredQuantity.Value.Equal(decimal.RequireFromString("500")) {
		t.Errorf("quantity = %s %s, want 500 kg preserved exactly",
			got.RequiredQuantity.Value.String(), got.RequiredQuantity.Unit)
	}
}

func TestCreateManualRequirementValidation(t *testing.T) {
	workItem := "work_1"
	cancelled := "work_x"
	foreignProjectWorkItem := "work_1"

	cases := []struct {
		name    string
		input   mr.CreateManualInput
		wantErr error
	}{
		{"foreign project", mr.CreateManualInput{
			ProjectID: "project_zzz", MaterialID: "material_1", QuantityValue: "1", QuantityUnit: "bag",
		}, mr.ErrProjectNotFound},
		{"unknown material", mr.CreateManualInput{
			ProjectID: "project_1", MaterialID: "material_zzz", QuantityValue: "1", QuantityUnit: "bag",
		}, mr.ErrMaterialNotFound},
		{"unknown work item", mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: strPtr("work_zzz"), MaterialID: "material_1",
			QuantityValue: "1", QuantityUnit: "bag",
		}, mr.ErrWorkItemNotFound},
		{"work item in another project", mr.CreateManualInput{
			ProjectID: "project_2", WorkItemID: &foreignProjectWorkItem, MaterialID: "material_1",
			QuantityValue: "1", QuantityUnit: "bag",
		}, mr.ErrWorkItemNotFound},
		{"cancelled work item", mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &cancelled, MaterialID: "material_1",
			QuantityValue: "1", QuantityUnit: "bag",
		}, mr.ErrWorkItemCancelled},
		{"zero quantity", mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &workItem, MaterialID: "material_1",
			QuantityValue: "0", QuantityUnit: "bag",
		}, mr.ErrInvalidQuantity},
		{"negative quantity", mr.CreateManualInput{
			ProjectID: "project_1", MaterialID: "material_1", QuantityValue: "-5", QuantityUnit: "bag",
		}, mr.ErrInvalidQuantity},
		{"unparseable quantity", mr.CreateManualInput{
			ProjectID: "project_1", MaterialID: "material_1", QuantityValue: "lots", QuantityUnit: "bag",
		}, mr.ErrInvalidQuantity},
		{"blank unit", mr.CreateManualInput{
			ProjectID: "project_1", MaterialID: "material_1", QuantityValue: "5", QuantityUnit: "   ",
		}, mr.ErrInvalidUnit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := newService(t)
			_, err := svc.CreateManualRequirement(context.Background(), "company_a", "user_1", tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if len(repo.byID) != 0 {
				t.Error("a rejected creation must persist nothing")
			}
		})
	}
}

// --- Edit and review reset (design spec §5.4) ---

func TestUpdateRequirementResetsReviewForProcurementFields(t *testing.T) {
	cases := []struct {
		name      string
		edit      mr.UpdateRequirementInput
		wantReset bool
	}{
		{"quantity value", mr.UpdateRequirementInput{QuantityValue: strPtr("120")}, true},
		{"quantity unit", mr.UpdateRequirementInput{QuantityUnit: strPtr("kg")}, true},
		{"specification", mr.UpdateRequirementInput{Specification: strPtr("revised spec")}, true},
		{"procurement notes", mr.UpdateRequirementInput{ProcurementNotes: strPtr("gate B")}, true},
		{"required-by date", mr.UpdateRequirementInput{RequiredByDate: timePtrPtr(time.Now().Add(48 * time.Hour))}, true},
		{"material", mr.UpdateRequirementInput{MaterialID: strPtr("material_2")}, true},
		{"internal notes only", mr.UpdateRequirementInput{InternalNotes: strPtr("private")}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, rec := newService(t)
			created := createManual(t, svc, nil)

			reviewed, err := svc.ReviewRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision)
			if err != nil {
				t.Fatalf("unexpected error reviewing: %v", err)
			}
			if reviewed.Status != mr.RequirementStatusReviewed {
				t.Fatalf("Status = %q, want reviewed", reviewed.Status)
			}

			got, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
				created.ID, reviewed.Revision, tc.edit)
			if err != nil {
				t.Fatalf("unexpected error updating: %v", err)
			}

			wantStatus := mr.RequirementStatusReviewed
			if tc.wantReset {
				wantStatus = mr.RequirementStatusDraft
			}
			if got.Status != wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, wantStatus)
			}
			if rec.lastReviewReset != tc.wantReset {
				t.Errorf("audited reviewReset = %v, want %v", rec.lastReviewReset, tc.wantReset)
			}
		})
	}
}

// Changing the material on a manual requirement refreshes the catalog snapshot
// and recomputes the mismatch (design spec §2.2).
func TestUpdateRequirementMaterialChangeRefreshesCatalogSnapshot(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil) // material_1, unit bag, spec "OPC 50kg"

	got, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{MaterialID: strPtr("material_2")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaterialName != "River Sand" {
		t.Errorf("MaterialName = %q, want River Sand", got.MaterialName)
	}
	if got.CatalogUnit != "m3" {
		t.Errorf("CatalogUnit = %q, want m3", got.CatalogUnit)
	}
	// Procurement unit is still "bag" but the new catalog unit is "m3".
	if !got.UnitMismatch {
		t.Error("UnitMismatch must be recomputed against the NEW catalog unit")
	}
}

// A contractor-authored specification survives a material change; a
// still-seeded one is re-seeded (design spec §2.2).
func TestUpdateRequirementPreservesContractorAuthoredSpecification(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)

	authored, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{Specification: strPtr("contractor wording")})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, authored.Revision, mr.UpdateRequirementInput{MaterialID: strPtr("material_2")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Specification != "contractor wording" {
		t.Errorf("Specification = %q, want the contractor's wording preserved", got.Specification)
	}
}

func TestUpdateRequirementReSeedsUntouchedSpecification(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil) // Specification seeded as "OPC 50kg"

	got, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{MaterialID: strPtr("material_2")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Specification != "washed" {
		t.Errorf("Specification = %q, want the new catalog seed \"washed\"", got.Specification)
	}
}

// Changing material or unit clears a prior acknowledgement, because it was
// scoped to the old combination (design spec §2.2).
func TestUpdateRequirementClearsUnitAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		edit      mr.UpdateRequirementInput
		wantClear bool
	}{
		{"material changed", mr.UpdateRequirementInput{MaterialID: strPtr("material_2")}, true},
		{"unit changed", mr.UpdateRequirementInput{QuantityUnit: strPtr("tonne")}, true},
		{"value only", mr.UpdateRequirementInput{QuantityValue: strPtr("999")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newService(t)
			created, err := svc.CreateManualRequirement(context.Background(), "company_a", "user_1",
				mr.CreateManualInput{ProjectID: "project_1", MaterialID: "material_1",
					QuantityValue: "500", QuantityUnit: "kg"})
			if err != nil {
				t.Fatal(err)
			}
			acked, err := svc.AcknowledgeUnitMismatch(context.Background(), "company_a", "user_1",
				created.ID, created.Revision)
			if err != nil {
				t.Fatalf("unexpected error acknowledging: %v", err)
			}
			if !acked.UnitMismatchAcknowledged {
				t.Fatal("acknowledgement did not take effect")
			}

			got, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
				created.ID, acked.Revision, tc.edit)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnitMismatchAcknowledged == tc.wantClear {
				t.Errorf("UnitMismatchAcknowledged = %v, wantCleared = %v",
					got.UnitMismatchAcknowledged, tc.wantClear)
			}
		})
	}
}

// A cost_item anchor's identity is immutable (design spec §2.2).
func TestUpdateRequirementRejectsIdentityChangeOnGeneratedAnchor(t *testing.T) {
	svc, repo, _ := newService(t)
	anchor := mr.MaterialRequirement{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: strPtr("work_1"),
		MaterialID: "material_1", MaterialName: "Portland Cement",
		RequiredQuantity: mustQty(t, "100", "bag"), CatalogUnit: "bag",
		Status: mr.RequirementStatusDraft, SourceType: mr.SourceTypeCostItem,
		SourceAggregationUnit: "bag", SourceAggregationKey: "key_1",
		SourceSyncState: mr.SourceSyncStateClean, SchemaVersion: 1,
	}
	created, err := repo.Create(context.Background(), anchor)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{MaterialID: strPtr("material_2")})
	if !errors.Is(err, mr.ErrMaterialIDImmutable) {
		t.Fatalf("error = %v, want ErrMaterialIDImmutable", err)
	}

	_, err = svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{WorkItemID: strPtrPtr("work_2")})
	if !errors.Is(err, mr.ErrWorkItemIDImmutable) {
		t.Fatalf("error = %v, want ErrWorkItemIDImmutable", err)
	}
}

// Every contractor operation is refused while claimed, INCLUDING an
// InternalNotes-only edit (design spec §2.3's conservative rule).
func TestClaimedRequirementRefusesEveryContractorOperation(t *testing.T) {
	svc, repo, _ := newService(t)
	created := createManual(t, svc, nil)

	stored := repo.byID[created.ID]
	stored.ActiveRFQChainID = strPtr("rfq_1")
	stored.ActiveRFQLineID = strPtr("line_1")
	stored.Status = mr.RequirementStatusReviewed
	repo.byID[created.ID] = stored

	ctx := context.Background()
	rev := stored.Revision

	if _, err := svc.UpdateRequirement(ctx, "company_a", "user_1", created.ID, rev,
		mr.UpdateRequirementInput{InternalNotes: strPtr("private")}); !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Errorf("InternalNotes edit: error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
	if _, err := svc.UpdateRequirement(ctx, "company_a", "user_1", created.ID, rev,
		mr.UpdateRequirementInput{QuantityValue: strPtr("5")}); !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Errorf("quantity edit: error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
	if _, err := svc.ArchiveRequirement(ctx, "company_a", "user_1", created.ID, rev); !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Errorf("archive: error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
	if _, err := svc.AcknowledgeUnitMismatch(ctx, "company_a", "user_1", created.ID, rev); !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Errorf("acknowledge: error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
	if err := svc.DeleteRequirement(ctx, "company_a", "user_1", created.ID, rev); !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Errorf("delete: error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
}

// Terminal requirements are permanently read-only (design spec §2.1).
func TestTerminalRequirementRefusesEveryContractorOperation(t *testing.T) {
	for _, status := range []mr.RequirementStatus{mr.RequirementStatusSplit, mr.RequirementStatusArchived} {
		t.Run(string(status), func(t *testing.T) {
			svc, repo, _ := newService(t)
			created := createManual(t, svc, nil)
			stored := repo.byID[created.ID]
			stored.Status = status
			repo.byID[created.ID] = stored

			ctx := context.Background()
			if _, err := svc.UpdateRequirement(ctx, "company_a", "user_1", created.ID, stored.Revision,
				mr.UpdateRequirementInput{QuantityValue: strPtr("5")}); !errors.Is(err, mr.ErrRequirementTerminal) {
				t.Errorf("update: error = %v, want ErrRequirementTerminal", err)
			}
			if _, err := svc.ReviewRequirement(ctx, "company_a", "user_1", created.ID, stored.Revision); !errors.Is(err, mr.ErrRequirementTerminal) {
				t.Errorf("review: error = %v, want ErrRequirementTerminal", err)
			}
			if _, err := svc.ArchiveRequirement(ctx, "company_a", "user_1", created.ID, stored.Revision); !errors.Is(err, mr.ErrRequirementTerminal) {
				t.Errorf("archive: error = %v, want ErrRequirementTerminal", err)
			}
		})
	}
}

// --- Review, acknowledge, archive ---

func TestReviewRequirementTransitionsDraftToReviewed(t *testing.T) {
	svc, _, rec := newService(t)
	created := createManual(t, svc, nil)

	got, err := svc.ReviewRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != mr.RequirementStatusReviewed {
		t.Errorf("Status = %q, want reviewed", got.Status)
	}
	if got.Revision != created.Revision+1 {
		t.Errorf("Revision = %d, want %d", got.Revision, created.Revision+1)
	}
	if rec.reviewed != 1 {
		t.Errorf("audit reviewed = %d, want 1", rec.reviewed)
	}
}

// Reviewing an already-reviewed requirement is a no-op success, not an error:
// it is idempotent from the contractor's point of view.
func TestReviewRequirementIsIdempotent(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)

	first, err := svc.ReviewRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ReviewRequirement(context.Background(), "company_a", "user_1", created.ID, first.Revision)
	if err != nil {
		t.Fatalf("re-reviewing must not error: %v", err)
	}
	if second.Status != mr.RequirementStatusReviewed {
		t.Errorf("Status = %q, want reviewed", second.Status)
	}
}

// Acknowledging when there is no mismatch is rejected: it would set a flag with
// no meaning and silently relax a gate later.
func TestAcknowledgeUnitMismatchRequiresAMismatch(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil) // unit == catalog unit, so no mismatch

	_, err := svc.AcknowledgeUnitMismatch(context.Background(), "company_a", "user_1",
		created.ID, created.Revision)
	if !errors.Is(err, mr.ErrNoUnitMismatchToAcknowledge) {
		t.Fatalf("error = %v, want ErrNoUnitMismatchToAcknowledge", err)
	}
}

// Acknowledging does NOT reset review: it resolves a blocking condition rather
// than changing what a supplier would be asked to quote.
func TestAcknowledgeUnitMismatchPreservesReview(t *testing.T) {
	svc, _, rec := newService(t)
	created, err := svc.CreateManualRequirement(context.Background(), "company_a", "user_1",
		mr.CreateManualInput{ProjectID: "project_1", MaterialID: "material_1",
			QuantityValue: "500", QuantityUnit: "kg"})
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := svc.ReviewRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.AcknowledgeUnitMismatch(context.Background(), "company_a", "user_1",
		created.ID, reviewed.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != mr.RequirementStatusReviewed {
		t.Errorf("Status = %q, want reviewed preserved", got.Status)
	}
	if !got.UnitMismatchAcknowledged {
		t.Error("UnitMismatchAcknowledged = false, want true")
	}
	// Acknowledged mismatch plus reviewed status means RFQ-eligible.
	if !got.IsRFQEligible() {
		t.Error("an acknowledged mismatch on a reviewed requirement must be RFQ-eligible")
	}
	if rec.acknowledged != 1 {
		t.Errorf("audit acknowledged = %d, want 1", rec.acknowledged)
	}
}

func TestArchiveRequirementIsTerminal(t *testing.T) {
	svc, _, rec := newService(t)
	created := createManual(t, svc, nil)

	got, err := svc.ArchiveRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != mr.RequirementStatusArchived {
		t.Errorf("Status = %q, want archived", got.Status)
	}
	if !got.IsTerminal() {
		t.Error("an archived requirement must be terminal")
	}
	if got.IsRFQEligible() {
		t.Error("an archived requirement must never be RFQ-eligible")
	}
	if rec.archived != 1 {
		t.Errorf("audit archived = %d, want 1", rec.archived)
	}
}

func TestDeleteRequirementRemovesItPermanently(t *testing.T) {
	svc, repo, rec := newService(t)
	created := createManual(t, svc, nil)

	if err := svc.DeleteRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.byID[created.ID]; ok {
		t.Error("requirement still present in the repository after delete")
	}
	if _, err := svc.GetRequirement(context.Background(), "company_a", created.ID); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Errorf("GetRequirement after delete: error = %v, want ErrMaterialRequirementNotFound", err)
	}
	if rec.deleted != 1 {
		t.Errorf("audit deleted = %d, want 1", rec.deleted)
	}
}

func TestDeleteRequirementIsTerminalRefused(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)

	archived, err := svc.ArchiveRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := svc.DeleteRequirement(context.Background(), "company_a", "user_1", archived.ID, archived.Revision); !errors.Is(err, mr.ErrRequirementTerminal) {
		t.Fatalf("error = %v, want ErrRequirementTerminal", err)
	}
}

func TestDeleteRequirementRejectsStaleRevision(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)

	if err := svc.DeleteRequirement(context.Background(), "company_a", "user_1", created.ID, created.Revision+1); !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// --- Concurrency and tenancy ---

func TestUpdateRequirementRejectsStaleRevision(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)

	if _, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{InternalNotes: strPtr("first")}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{InternalNotes: strPtr("second")})
	if !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// Every read and write is tenant-scoped: a foreign company sees 404, never data.
func TestRequirementOperationsAreTenantScoped(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)
	ctx := context.Background()

	if _, err := svc.GetRequirement(ctx, "company_b", created.ID); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Errorf("get: error = %v, want not found", err)
	}
	if _, err := svc.UpdateRequirement(ctx, "company_b", "user_1", created.ID, created.Revision,
		mr.UpdateRequirementInput{InternalNotes: strPtr("x")}); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Errorf("update: error = %v, want not found", err)
	}
	if _, err := svc.ReviewRequirement(ctx, "company_b", "user_1", created.ID, created.Revision); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Errorf("review: error = %v, want not found", err)
	}
	if _, err := svc.ArchiveRequirement(ctx, "company_b", "user_1", created.ID, created.Revision); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Errorf("archive: error = %v, want not found", err)
	}
	if err := svc.DeleteRequirement(ctx, "company_b", "user_1", created.ID, created.Revision); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Errorf("delete: error = %v, want not found", err)
	}
}

// A foreign projectID yields 404, never an empty list (the tenant invariant
// stated in costs/service.go).
func TestListRequirementsRejectsForeignProject(t *testing.T) {
	svc, _, _ := newService(t)

	if _, err := svc.ListRequirementsByProject(context.Background(), "company_a", "project_zzz"); !errors.Is(err, mr.ErrProjectNotFound) {
		t.Fatalf("error = %v, want ErrProjectNotFound", err)
	}
}

func TestListRequirementsByProject(t *testing.T) {
	svc, _, _ := newService(t)
	createManual(t, svc, nil)
	createManual(t, svc, nil)

	got, err := svc.ListRequirementsByProject(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 requirements, got %d", len(got))
	}
}

// B2 performs no source detection at all: manual requirements have no CostItem
// aggregate to compare against.
func TestManualOperationsNeverTouchSyncState(t *testing.T) {
	svc, repo, _ := newService(t)
	created := createManual(t, svc, nil)
	ctx := context.Background()

	if _, err := svc.UpdateRequirement(ctx, "company_a", "user_1", created.ID, created.Revision,
		mr.UpdateRequirementInput{QuantityValue: strPtr("7")}); err != nil {
		t.Fatal(err)
	}
	if repo.syncStateCalls != 0 {
		t.Errorf("UpdateSyncState was called %d times; manual operations must never converge detection state",
			repo.syncStateCalls)
	}
}

// --- helpers ---

func strPtr(s string) *string { return &s }

func strPtrPtr(s string) **string {
	p := &s
	return &p
}

func timePtrPtr(t time.Time) **time.Time {
	p := &t
	return &p
}
