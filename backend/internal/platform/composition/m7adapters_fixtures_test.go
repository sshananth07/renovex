package composition_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// Fixtures for the M7 adapter tests. They build a REAL
// materialrequirements.Service over an in-memory repository, so the adapter is
// exercised against the actual conversion and sentinel behaviour rather than
// against a stub of itself.

type memoryRepo struct {
	byID map[string]mr.MaterialRequirement
	next int
	// failWith, when set, is returned from every read, so the pass-through
	// behaviour for an unrecognised error can be exercised.
	failWith error
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{byID: map[string]mr.MaterialRequirement{}}
}

func (m *memoryRepo) Create(_ context.Context, r mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	if m.failWith != nil {
		return mr.MaterialRequirement{}, m.failWith
	}
	m.next++
	r.ID = "mr_" + decimal.NewFromInt(int64(m.next)).String()
	m.byID[r.ID] = r
	return r, nil
}

func (m *memoryRepo) CreateWithID(_ context.Context, r mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	if _, exists := m.byID[r.ID]; exists {
		return mr.MaterialRequirement{}, mr.ErrRequirementIDExists
	}
	m.byID[r.ID] = r
	return r, nil
}

func (m *memoryRepo) FindByID(_ context.Context, companyID, id string) (mr.MaterialRequirement, error) {
	if m.failWith != nil {
		return mr.MaterialRequirement{}, m.failWith
	}
	r, ok := m.byID[id]
	if !ok || r.CompanyID != companyID {
		return mr.MaterialRequirement{}, mr.ErrMaterialRequirementNotFound
	}
	return r, nil
}

func (m *memoryRepo) ListByProject(_ context.Context, companyID, projectID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range m.byID {
		if r.CompanyID == companyID && r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memoryRepo) ListGeneratedAnchorsByProject(_ context.Context, companyID, projectID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range m.byID {
		if r.CompanyID == companyID && r.ProjectID == projectID && r.SourceType == mr.SourceTypeCostItem {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memoryRepo) UpdateContractorFields(ctx context.Context, companyID, id string,
	expectedRevision int64, updated mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.IsTerminal() || stored.IsClaimed() || stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	stored.RequiredQuantity = updated.RequiredQuantity
	stored.Status = updated.Status
	stored.UnitMismatch = updated.UnitMismatch
	stored.UnitMismatchAcknowledged = updated.UnitMismatchAcknowledged
	stored.Revision = expectedRevision + 1
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) Delete(ctx context.Context, companyID, id string, expectedRevision int64) error {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return err
	}
	if stored.Revision != expectedRevision {
		return mr.ErrRevisionMismatch
	}
	delete(m.byID, id)
	return nil
}

func (m *memoryRepo) UpdateSyncState(ctx context.Context, companyID, id string,
	expectedRevision int64, state mr.SourceSyncState, checkedAt time.Time) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	stored.SourceSyncState = state
	stored.SourceCheckedAt = &checkedAt
	stored.Revision = expectedRevision + 1
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) ApplyDiscrepancyResolution(ctx context.Context, companyID, id string,
	expectedRevision int64, resolved mr.MaterialRequirement) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.IsClaimed() || stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	stored.Revision = expectedRevision + 1
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) FindByResolutionOperationID(_ context.Context,
	companyID, operationID string) (mr.MaterialRequirement, error) {
	for _, r := range m.byID {
		if r.CompanyID == companyID && r.ResolutionOperationID != nil &&
			*r.ResolutionOperationID == operationID {
			return r, nil
		}
	}
	return mr.MaterialRequirement{}, mr.ErrMaterialRequirementNotFound
}

func (m *memoryRepo) BeginSplit(ctx context.Context, companyID, id string, expectedRevision int64,
	manifest []mr.SplitChildRef, splitGroupID, actorUserID string,
	splitAt time.Time) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}
	stored.Status = mr.RequirementStatusSplit
	stored.SplitGroupID = &splitGroupID
	stored.SplitState = mr.SplitStateCreating
	stored.SplitChildren = manifest
	stored.Revision = expectedRevision + 1
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) CompleteSplit(ctx context.Context, companyID, id string,
	expectedRevision int64) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	stored.SplitState = mr.SplitStateCompleted
	stored.Revision = expectedRevision + 1
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) ListSplitChildren(_ context.Context, companyID, splitGroupID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range m.byID {
		if r.CompanyID == companyID && r.SplitGroupID != nil && *r.SplitGroupID == splitGroupID {
			out = append(out, r)
		}
	}
	return out, nil
}

// ClaimForRFQ mirrors the real atomic filter and its four-way classification.
func (m *memoryRepo) ClaimForRFQ(ctx context.Context, companyID, projectID, id string,
	expectedRevision int64, rfqChainID, rfqNumber, lineID string) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
	if err != nil {
		return mr.MaterialRequirement{}, err
	}
	if stored.ProjectID != projectID {
		return mr.MaterialRequirement{}, mr.ErrMaterialRequirementNotFound
	}
	if stored.IsClaimed() {
		return mr.MaterialRequirement{}, mr.ErrMaterialRequirementAlreadyClaimed
	}
	if !stored.IsRFQEligible() {
		return mr.MaterialRequirement{}, mr.ErrRequirementNotEligibleForRFQ
	}
	if stored.Revision != expectedRevision {
		return mr.MaterialRequirement{}, mr.ErrRevisionMismatch
	}

	now := time.Now()
	stored.ActiveRFQChainID = &rfqChainID
	stored.ActiveRFQNumber = &rfqNumber
	stored.ActiveRFQLineID = &lineID
	stored.RFQClaimedAt = &now
	stored.Revision = expectedRevision + 1
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) ReleaseClaim(ctx context.Context, companyID, id string,
	expectedRevision int64, rfqChainID, lineID string) (mr.MaterialRequirement, error) {
	stored, err := m.FindByID(ctx, companyID, id)
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
	m.byID[id] = stored
	return stored, nil
}

func (m *memoryRepo) ListClaimsForRFQChain(_ context.Context, companyID, rfqChainID string) ([]mr.MaterialRequirement, error) {
	var out []mr.MaterialRequirement
	for _, r := range m.byID {
		if r.CompanyID == companyID && r.ActiveRFQChainID != nil && *r.ActiveRFQChainID == rfqChainID {
			out = append(out, r)
		}
	}
	return out, nil
}

// --- capability stubs ---

type stubProjects struct{}

func (stubProjects) ProjectBelongsToCompany(_ context.Context, companyID, projectID string) (bool, error) {
	return companyID == "company_a" && (projectID == "project_1" || projectID == "project_2"), nil
}

type stubWork struct{}

func (stubWork) WorkItemProcurementContext(_ context.Context, _, workItemID, _ string) (bool, bool, error) {
	return false, workItemID == "work_1", nil
}

type stubMaterials struct{}

func (stubMaterials) GetMaterialReference(_ context.Context, _, materialID string) (
	string, string, string, bool, error) {
	if materialID != "material_1" {
		return "", "", "", false, nil
	}
	return "Portland Cement", "bag", "OPC 50kg", true, nil
}

type stubCostSource struct{}

func (stubCostSource) VisitEligibleMaterialCostItems(_ context.Context, _, _ string,
	_ func(string, string, string, decimal.Decimal, string) error) (int, int, int, int, int, error) {
	return 0, 0, 0, 0, 0, nil
}

type stubAudit struct{}

func (stubAudit) RecordMaterialRequirementsGenerated(context.Context, string, string, string, int, int, int, int, int) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementCreated(context.Context, string, string, string, string, string, string) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementUpdated(context.Context, string, string, string, string, bool) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementReviewed(context.Context, string, string, string, string, string) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementUnitAcknowledged(context.Context, string, string, string, string, string, string) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementDiscrepancyResolved(context.Context, string, string, string, string, string, string, string) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementSplit(context.Context, string, string, string, string, string, int) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementArchived(context.Context, string, string, string, string) error {
	return nil
}
func (stubAudit) RecordMaterialRequirementDeleted(context.Context, string, string, string, string) error {
	return nil
}

func newRequirementService(repo *memoryRepo) *mr.Service {
	return mr.NewService(repo, stubProjects{}, stubWork{}, stubMaterials{},
		stubCostSource{}, stubAudit{})
}

// seedEligibleRequirement returns a service holding one reviewed, unclaimed,
// RFQ-eligible requirement.
func seedEligibleRequirement(t *testing.T) (*mr.Service, mr.MaterialRequirement) {
	t.Helper()
	svc := newRequirementService(newMemoryRepo())
	ctx := context.Background()
	workItemID := "work_1"

	created, err := svc.CreateManualRequirement(ctx, "company_a", "user_1", mr.CreateManualInput{
		ProjectID: "project_1", WorkItemID: &workItemID, MaterialID: "material_1",
		QuantityValue: "100", QuantityUnit: "bag",
		ProcurementNotes: "deliver to site gate",
		InternalNotes:    "contractor-only margin note",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("setup review: %v", err)
	}
	return svc, reviewed
}

// seedClaimedRequirement returns a service whose requirement is claimed by
// chain_1 / line_1.
func seedClaimedRequirement(t *testing.T) (*mr.Service, mr.MaterialRequirement) {
	t.Helper()
	svc, req := seedEligibleRequirement(t)
	if _, err := svc.ClaimForRFQ(context.Background(), "company_a", "project_1", req.ID,
		req.Revision, "chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatalf("setup claim: %v", err)
	}
	return svc, req
}

// seedDraftRequirement returns a service holding a DRAFT requirement, which
// fails the §8.5 eligibility predicate.
func seedDraftRequirement(t *testing.T) (*mr.Service, mr.MaterialRequirement) {
	t.Helper()
	svc := newRequirementService(newMemoryRepo())
	workItemID := "work_1"

	created, err := svc.CreateManualRequirement(context.Background(), "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &workItemID, MaterialID: "material_1",
			QuantityValue: "100", QuantityUnit: "bag",
		})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return svc, created
}

// materialRequirementServiceWithFailingRepo returns a service whose repository
// fails every read with the given error, so pass-through can be exercised.
func materialRequirementServiceWithFailingRepo(t *testing.T, err error) *mr.Service {
	t.Helper()
	repo := newMemoryRepo()
	repo.failWith = err
	return newRequirementService(repo)
}
