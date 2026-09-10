package estimates_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

type fakeEstimateRepository struct {
	byID map[string]estimates.Estimate
	next int
}

func newFakeEstimateRepository() *fakeEstimateRepository {
	return &fakeEstimateRepository{byID: make(map[string]estimates.Estimate)}
}

func (f *fakeEstimateRepository) Create(ctx context.Context, e estimates.Estimate) (estimates.Estimate, error) {
	// Simulate the two real unique indexes (design spec §20) so unit tests
	// can exercise allocateAndCreate's retry behavior and
	// ErrDraftAlreadyExists propagation without a real MongoDB instance —
	// this is what lets TestCreateEstimateVersionConflictRetries and
	// TestCreateNewVersionDraftAlreadyExists (added below) actually prove
	// the PRODUCTION allocateAndCreate helper's behavior, not just a
	// standalone test-only reimplementation.
	for _, existing := range f.byID {
		if existing.CompanyID != e.CompanyID || existing.ProjectID != e.ProjectID {
			continue
		}
		if existing.Version == e.Version {
			return estimates.Estimate{}, estimates.ErrVersionConflict
		}
		if existing.Status == estimates.EstimateStatusDraft && e.Status == estimates.EstimateStatusDraft {
			return estimates.Estimate{}, estimates.ErrDraftAlreadyExists
		}
	}
	f.next++
	e.ID = "estimate_" + string(rune('0'+f.next))
	f.byID[e.ID] = e
	return e, nil
}

func (f *fakeEstimateRepository) FindByID(ctx context.Context, companyID, id string) (estimates.Estimate, error) {
	e, ok := f.byID[id]
	if !ok || e.CompanyID != companyID {
		return estimates.Estimate{}, estimates.ErrEstimateNotFound
	}
	return e, nil
}

func (f *fakeEstimateRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]estimates.Estimate, error) {
	var result []estimates.Estimate
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeEstimateRepository) FindLatestByProject(ctx context.Context, companyID, projectID string) (estimates.Estimate, error) {
	var latest estimates.Estimate
	found := false
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID && (!found || e.Version > latest.Version) {
			latest = e
			found = true
		}
	}
	if !found {
		return estimates.Estimate{}, estimates.ErrNoEstimatesForProject
	}
	return latest, nil
}

func (f *fakeEstimateRepository) FindMaxVersion(ctx context.Context, companyID, projectID string) (int, error) {
	max := 0
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID && e.Version > max {
			max = e.Version
		}
	}
	return max, nil
}

func (f *fakeEstimateRepository) ReplaceSnapshot(ctx context.Context, companyID, id string, expectedRevision int64, updated estimates.Estimate) (estimates.Estimate, error) {
	existing, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return estimates.Estimate{}, err
	}
	if existing.Status != estimates.EstimateStatusDraft || existing.Revision != expectedRevision {
		return estimates.Estimate{}, estimates.ErrRevisionMismatch
	}
	updated.Revision = expectedRevision + 1
	f.byID[id] = updated
	return updated, nil
}

func (f *fakeEstimateRepository) UpdatePricing(ctx context.Context, companyID, id string, expectedRevision int64, updated estimates.Estimate) (estimates.Estimate, error) {
	existing, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return estimates.Estimate{}, err
	}
	if existing.Status != estimates.EstimateStatusDraft || existing.Revision != expectedRevision {
		return estimates.Estimate{}, estimates.ErrRevisionMismatch
	}
	updated.Revision = expectedRevision + 1
	f.byID[id] = updated
	return updated, nil
}

func (f *fakeEstimateRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (estimates.Estimate, error) {
	existing, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return estimates.Estimate{}, err
	}
	if existing.Status != estimates.EstimateStatusDraft || existing.Revision != expectedRevision {
		return estimates.Estimate{}, estimates.ErrRevisionMismatch
	}
	existing.Status = estimates.EstimateStatusFinalized
	existing.FinalizedAt = &finalizedAt
	f.byID[id] = existing
	return existing, nil
}

type fakeProjectLookup struct{ belongs bool }

func (f fakeProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeCostSource struct {
	items []fakeCostSourceItem
}

type fakeCostSourceItem struct {
	costItemID  string
	workItemID  *string
	category    string
	description string
	estimated   money.Money
}

func (f fakeCostSource) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	for _, item := range f.items {
		if err := visit(item.costItemID, item.workItemID, item.category, item.description, item.estimated); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func TestCreateEstimateHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(50000, "MYR")},
		{costItemID: "c2", category: "labour", description: "Tiling labour", estimated: money.New(30000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	e, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Version != 1 || e.Status != estimates.EstimateStatusDraft {
		t.Fatalf("expected Version=1 Status=draft, got %+v", e)
	}
	if e.CostSubtotal.Amount != 80000 {
		t.Fatalf("expected CostSubtotal 80000 (50000+30000), got %+v", e.CostSubtotal)
	}
	if e.ProposedSellingPrice.Amount != 96000 {
		t.Fatalf("expected selling price 96000 (RM800 + 20%% markup), got %+v", e.ProposedSellingPrice)
	}
	if len(e.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(e.Lines))
	}
}

func TestCreateEstimateProjectNotFound(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: false}, fakeCostSource{})

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCreateEstimateNoEligibleCostItems(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{items: nil})

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrNoEstimatedCosts {
		t.Fatalf("expected ErrNoEstimatedCosts, got %v", err)
	}
}

func TestCreateEstimateMixedCurrency(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", estimated: money.New(50000, "MYR")},
		{costItemID: "c2", estimated: money.New(30000, "SGD")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrMixedCurrencyCostItems {
		t.Fatalf("expected ErrMixedCurrencyCostItems, got %v", err)
	}
}

func TestCreateEstimateDoesNotDoubleCountLabour(t *testing.T) {
	// The universal-ledger invariant from M3: a labour cost enters
	// cost_items exactly once (Category=labour), never separately summed
	// from labour_entries. estimates never touches labour_entries at all —
	// this test proves the Estimate's CostSubtotal exactly equals the sum
	// of what the cost source visits, once each.
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "labour_cost_item", category: "labour", description: "Tiler", estimated: money.New(30000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	e, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.CostSubtotal.Amount != 30000 {
		t.Fatalf("expected CostSubtotal to equal the single labour CostItem's amount exactly (30000), got %+v", e.CostSubtotal)
	}
}

func TestCreateEstimateExcludedCostItemCount(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := &fakeCostSourceWithMissingCount{
		items:        []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}},
		missingCount: 3,
	}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	e, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.ExcludedCostItemCount != 3 {
		t.Fatalf("expected ExcludedCostItemCount=3, got %d", e.ExcludedCostItemCount)
	}
}

func TestCreateEstimateRejectsWhenProjectAlreadyHasAnEstimate(t *testing.T) {
	// POST /estimates must create ONLY Version 1. Calling it again after
	// V1 exists (whether draft or finalized) must NOT silently mint V2 —
	// that is exclusively CreateNewVersion's job, and bypassing it would
	// also bypass CreateNewVersion's "source must be finalized"
	// precondition.
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
	}

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject, got %v", err)
	}

	// Also rejected when the existing version is still a draft — the rule
	// is "any version already exists," not "any FINALIZED version exists."
	repo2 := newFakeEstimateRepository()
	svc2 := estimates.NewService(repo2, fakeProjectLookup{belongs: true}, costSource)
	repo2.byID["e2"] = estimates.Estimate{
		ID: "e2", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft,
	}
	_, err = svc2.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject against an existing draft too, got %v", err)
	}
}

func TestCreateEstimateDoesNotRetryOnVersionConflict(t *testing.T) {
	// CreateEstimate must NEVER retry into Version 2+, unlike
	// CreateNewVersion. racyRepo below simulates a concurrent writer
	// winning the Version-1 race ahead of this caller — CreateEstimate
	// must translate that single collision directly into
	// ErrEstimateAlreadyExistsForProject, with exactly ONE Create attempt,
	// never re-reading MAX(version) and trying again at a higher number.
	repo := &racyEstimateRepository{fakeEstimateRepository: newFakeEstimateRepository(), failFirstNAttempts: 1, failWith: estimates.ErrVersionConflict}
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject (translated from the version-conflict collision, not retried), got %v", err)
	}
	if repo.attempts != 1 {
		t.Fatalf("expected exactly 1 Create attempt (no retry), got %d", repo.attempts)
	}
}

func TestCreateEstimateDoesNotRetryOnDraftAlreadyExists(t *testing.T) {
	repo := &racyEstimateRepository{fakeEstimateRepository: newFakeEstimateRepository(), failFirstNAttempts: 1, failWith: estimates.ErrDraftAlreadyExists}
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject (translated from the draft-collision), got %v", err)
	}
	if repo.attempts != 1 {
		t.Fatalf("expected exactly 1 Create attempt (no retry), got %d", repo.attempts)
	}
}

// racyEstimateRepository wraps fakeEstimateRepository, forcing Create to
// return failWith for the first failFirstNAttempts calls regardless of the
// fake's own collision logic — simulating another concurrent writer
// winning a race ahead of this caller. Used by both:
//   - CreateEstimate's tests above, to prove NO retry occurs;
//   - CreateNewVersion's TestCreateNewVersionRetriesOnVersionConflict
//     (Task 7), to prove the PRODUCTION allocateAndCreate helper DOES
//     retry — the two methods have deliberately different concurrency
//     contracts, and this fake proves both, one no-retry and one
//     retrying, against the same underlying simulated race.
type racyEstimateRepository struct {
	*fakeEstimateRepository
	failFirstNAttempts int
	failWith           error
	attempts           int
}

func (r *racyEstimateRepository) Create(ctx context.Context, e estimates.Estimate) (estimates.Estimate, error) {
	r.attempts++
	if r.attempts <= r.failFirstNAttempts {
		return estimates.Estimate{}, r.failWith
	}
	return r.fakeEstimateRepository.Create(ctx, e)
}

func TestGetLatestEstimate(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1}
	repo.byID["e2"] = estimates.Estimate{ID: "e2", CompanyID: "company_a", ProjectID: "project_1", Version: 2}

	latest, err := svc.GetLatestEstimate(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("expected version 2, got %d", latest.Version)
	}
}

type fakeCostSourceWithMissingCount struct {
	items        []fakeCostSourceItem
	missingCount int
}

func (f *fakeCostSourceWithMissingCount) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	for _, item := range f.items {
		if err := visit(item.costItemID, item.workItemID, item.category, item.description, item.estimated); err != nil {
			return 0, err
		}
	}
	return f.missingCount, nil
}

func TestRecalculatePricingHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	draft := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft, Revision: 0,
		Currency: "MYR", CostSubtotal: money.New(10000, "MYR"), PricingMode: estimates.PricingModeMarkup, PricingRate: 2000,
	}
	repo.byID["e1"] = draft

	updated, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMargin, 2000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.PricingMode != estimates.PricingModeMargin || updated.PricingRate != 2000 {
		t.Fatalf("expected pricing fields updated, got %+v", updated)
	}
	if updated.ProposedSellingPrice.Amount != 12500 {
		t.Fatalf("expected selling price 12500 (margin recalculated from stored subtotal), got %+v", updated.ProposedSellingPrice)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected Revision incremented to 1, got %d", updated.Revision)
	}
}

func TestRecalculatePricingNeverTouchesLines(t *testing.T) {
	repo := newFakeEstimateRepository()
	// A cost source that would fail the test if consulted during pricing
	// recalculation — proves RecalculatePricing never reaches into the
	// live cost source.
	costSource := failingCostSource{t: t}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	draft := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Status: estimates.EstimateStatusDraft, Revision: 0,
		Currency: "MYR", CostSubtotal: money.New(10000, "MYR"),
		Lines: []estimates.EstimateCostLine{{SourceCostItemID: "c1", SnapshottedAmount: money.New(10000, "MYR")}},
	}
	repo.byID["e1"] = draft

	updated, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMarkup, 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Lines) != 1 || updated.Lines[0].SourceCostItemID != "c1" {
		t.Fatalf("expected Lines untouched, got %+v", updated.Lines)
	}
}

func TestRecalculatePricingRejectsFinalized(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusFinalized, Revision: 3,
		CostSubtotal: money.New(10000, "MYR"),
	}

	_, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMarkup, 1000, 3)
	if err != estimates.ErrEstimateNotDraft {
		t.Fatalf("expected ErrEstimateNotDraft, got %v", err)
	}
}

func TestRecalculatePricingStaleRevisionRejected(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusDraft, Revision: 5,
		CostSubtotal: money.New(10000, "MYR"),
	}

	_, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMarkup, 1000, 4)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

func TestRefreshEstimateHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(120000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	draft := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft, Revision: 0,
		Currency:     "MYR",
		Lines:        []estimates.EstimateCostLine{{SourceCostItemID: "c1", SnapshottedAmount: money.New(50000, "MYR")}},
		CostSubtotal: money.New(50000, "MYR"), PricingMode: estimates.PricingModeMarkup, PricingRate: 2000,
	}
	repo.byID["e1"] = draft

	updated, err := svc.RefreshEstimate(context.Background(), "company_a", "e1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.CostSubtotal.Amount != 120000 {
		t.Fatalf("expected CostSubtotal updated to 120000, got %+v", updated.CostSubtotal)
	}
	if updated.Version != 1 {
		t.Fatalf("expected Version unchanged at 1, got %d", updated.Version)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected Revision incremented to 1, got %d", updated.Revision)
	}
	if updated.RefreshedAt == nil {
		t.Fatal("expected RefreshedAt to be set")
	}
	// Pricing must be recomputed against the NEW subtotal, using the
	// EXISTING PricingMode/PricingRate (20% markup carried forward).
	if updated.ProposedSellingPrice.Amount != 144000 {
		t.Fatalf("expected selling price recomputed as 144000 (RM1200 + 20%%), got %+v", updated.ProposedSellingPrice)
	}
}

func TestRefreshEstimateFailureLeavesDraftUntouched(t *testing.T) {
	repo := newFakeEstimateRepository()
	// The refreshed cost_items are now mixed-currency — refresh must fail
	// and leave the existing snapshot completely untouched.
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", estimated: money.New(50000, "MYR")},
		{costItemID: "c2", estimated: money.New(30000, "SGD")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	original := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Status: estimates.EstimateStatusDraft, Revision: 0,
		Lines:        []estimates.EstimateCostLine{{SourceCostItemID: "old", SnapshottedAmount: money.New(1000, "MYR")}},
		CostSubtotal: money.New(1000, "MYR"),
	}
	repo.byID["e1"] = original

	_, err := svc.RefreshEstimate(context.Background(), "company_a", "e1", 0)
	if err != estimates.ErrMixedCurrencyCostItems {
		t.Fatalf("expected ErrMixedCurrencyCostItems, got %v", err)
	}

	unchanged := repo.byID["e1"]
	if unchanged.CostSubtotal.Amount != 1000 || unchanged.Revision != 0 {
		t.Fatalf("expected draft completely untouched after failed refresh, got %+v", unchanged)
	}
}

func TestRefreshEstimateRejectsFinalized(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusFinalized, Revision: 2}

	_, err := svc.RefreshEstimate(context.Background(), "company_a", "e1", 2)
	if err != estimates.ErrEstimateNotDraft {
		t.Fatalf("expected ErrEstimateNotDraft, got %v", err)
	}
}

type failingCostSource struct{ t *testing.T }

func (f failingCostSource) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	f.t.Helper()
	f.t.Fatal("cost source must not be consulted during pricing recalculation")
	return 0, nil
}

func TestFinalizeEstimateHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusDraft, Revision: 4}

	updated, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Status=finalized, got %s", updated.Status)
	}
	if updated.FinalizedAt == nil {
		t.Fatal("expected FinalizedAt set")
	}
	// Invariant (design spec §16.3): finalize FREEZES Revision — Draft
	// Revision 4 -> Finalize -> Finalized Revision 4, never Revision 5.
	if updated.Revision != 4 {
		t.Fatalf("expected Revision to remain 4 after finalize (frozen, not incremented), got %d", updated.Revision)
	}
}

func TestFinalizeEstimateStaleReviewScenario(t *testing.T) {
	// The exact worked example from the second review round: contractor A
	// reads Revision 4, another mutation advances to Revision 5, A tries to
	// finalize at the stale Revision 4 -> rejected; A re-reads, retries at
	// Revision 5 -> succeeds.
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusDraft, Revision: 5}

	_, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 4)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch for stale expectedRevision=4, got %v", err)
	}
	if repo.byID["e1"].Status != estimates.EstimateStatusDraft {
		t.Fatal("expected estimate to remain draft after rejected finalize attempt")
	}

	updated, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 5)
	if err != nil {
		t.Fatalf("expected success with the current revision 5, got error: %v", err)
	}
	if updated.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Status=finalized, got %s", updated.Status)
	}
}

func TestFinalizeEstimateIdempotentOnRetry(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	now := time.Now()
	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusFinalized, Revision: 7, FinalizedAt: &now,
	}

	// A retry supplies an arbitrary/stale expectedRevision — must still
	// succeed and return the unchanged Estimate, since it is already
	// finalized (idempotency carve-out, design spec §16.3 step 2).
	updated, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 0)
	if err != nil {
		t.Fatalf("expected idempotent success on an already-finalized estimate regardless of expectedRevision, got error: %v", err)
	}
	if updated.Status != estimates.EstimateStatusFinalized || updated.Revision != 7 {
		t.Fatalf("expected unchanged finalized estimate returned, got %+v", updated)
	}
}

func TestCreateNewVersionRequiresFinalizedSource(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft}

	_, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", nil, nil)
	if err != estimates.ErrEstimateMustBeFinalizedBeforeNewVersion {
		t.Fatalf("expected ErrEstimateMustBeFinalizedBeforeNewVersion, got %v", err)
	}
}

func TestCreateNewVersionHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", estimated: money.New(150000, "MYR")}, // cost has changed since v1
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
		PricingMode: estimates.PricingModeMarkup, PricingRate: 1500,
	}

	v2, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version=2, got %d", v2.Version)
	}
	if v2.Status != estimates.EstimateStatusDraft {
		t.Fatalf("expected new version to start as draft, got %s", v2.Status)
	}
	if v2.Revision != 0 {
		t.Fatalf("expected new version to start at Revision=0, got %d", v2.Revision)
	}
	if v2.CostSubtotal.Amount != 150000 {
		t.Fatalf("expected fresh snapshot reflecting the updated cost (150000), got %+v", v2.CostSubtotal)
	}
	// Pricing defaults carried forward from source (15% markup) since no
	// override was supplied.
	if v2.PricingMode != estimates.PricingModeMarkup || v2.PricingRate != 1500 {
		t.Fatalf("expected pricing defaults carried forward from source, got %+v", v2)
	}
}

func TestCreateNewVersionPricingOverride(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(100000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
		PricingMode: estimates.PricingModeMarkup, PricingRate: 1500,
	}

	newMode := estimates.PricingModeMargin
	newRate := money.RateBPS(2500)
	v2, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", &newMode, &newRate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.PricingMode != estimates.PricingModeMargin || v2.PricingRate != 2500 {
		t.Fatalf("expected overridden pricing to take effect, got %+v", v2)
	}
}

func TestCreateNewVersionRetriesOnVersionConflict(t *testing.T) {
	// Unlike CreateEstimate (which must NEVER retry — see
	// TestCreateEstimateDoesNotRetryOnVersionConflict), CreateNewVersion
	// legitimately uses allocateAndCreate's bounded-retry MAX(version)+1
	// strategy, since retrying into the next version number IS the correct
	// behavior here. Reuses racyEstimateRepository to simulate exactly 1
	// version-conflict collision before succeeding.
	repo := &racyEstimateRepository{fakeEstimateRepository: newFakeEstimateRepository(), failFirstNAttempts: 1, failWith: estimates.ErrVersionConflict}
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(100000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
		PricingMode: estimates.PricingModeMarkup, PricingRate: 1500,
	}

	v2, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", nil, nil)
	if err != nil {
		t.Fatalf("expected CreateNewVersion to succeed after retrying past one simulated version conflict, got error: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version=2, got %d", v2.Version)
	}
	if repo.attempts != 2 {
		t.Fatalf("expected exactly 2 Create attempts (1 simulated conflict + 1 success), got %d", repo.attempts)
	}
}

func TestVisitQuotationSeedsHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()

	workItem1 := "work_1"
	workItem2 := "work_2"
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItem1, category: "material", description: "Tiles", estimated: money.New(100000, "MYR")},
		{costItemID: "c2", workItemID: &workItem2, category: "labour", description: "Install", estimated: money.New(50000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)
	finalized, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err = svc.FinalizeEstimate(context.Background(), "company_a", finalized.ID, finalized.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	var seeds []struct {
		workItemID *string
		amount     money.Money
	}
	proposedSellingPrice, currency, found, projectMatches, isFinalized, allocationEligible, err :=
		svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", finalized.ID,
			func(workItemID *string, allocatedSellingAmount money.Money) error {
				seeds = append(seeds, struct {
					workItemID *string
					amount     money.Money
				}{workItemID, allocatedSellingAmount})
				return nil
			})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || !projectMatches || !isFinalized || !allocationEligible {
		t.Fatalf("expected all four eligibility booleans true, got found=%v projectMatches=%v finalized=%v allocationEligible=%v",
			found, projectMatches, isFinalized, allocationEligible)
	}
	if currency != "MYR" {
		t.Fatalf("expected currency MYR, got %s", currency)
	}
	if proposedSellingPrice.Amount != finalized.ProposedSellingPrice.Amount {
		t.Fatalf("expected proposedSellingPrice to match the Estimate's own ProposedSellingPrice %d, got %d",
			finalized.ProposedSellingPrice.Amount, proposedSellingPrice.Amount)
	}
	if len(seeds) != 2 {
		t.Fatalf("expected 2 seeds (one per WorkItem), got %d", len(seeds))
	}
	sum := int64(0)
	for _, s := range seeds {
		if s.amount.Amount <= 0 {
			t.Fatalf("expected every allocated seed amount to be strictly positive, got %d", s.amount.Amount)
		}
		sum += s.amount.Amount
	}
	if sum != proposedSellingPrice.Amount {
		t.Fatalf("expected seed amounts to sum exactly to proposedSellingPrice (%d), got %d", proposedSellingPrice.Amount, sum)
	}
}

func TestVisitQuotationSeedsNotFound(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	_, _, found, _, _, _, err := svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", "nonexistent", func(*string, money.Money) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a nonexistent estimate")
	}
}

func TestVisitQuotationSeedsProjectMismatch(t *testing.T) {
	workItem1 := "work_1"
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItem1, category: "material", estimated: money.New(100000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err := svc.FinalizeEstimate(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	_, _, found, projectMatches, _, _, err := svc.VisitQuotationSeeds(context.Background(), "company_a", "project_OTHER", finalized.ID, func(*string, money.Money) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true (the estimate does exist)")
	}
	if projectMatches {
		t.Fatal("expected projectMatches=false for a mismatched project")
	}
}

func TestVisitQuotationSeedsNotFinalized(t *testing.T) {
	workItem1 := "work_1"
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItem1, category: "material", estimated: money.New(100000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}

	_, _, found, projectMatches, isFinalized, _, err := svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", created.ID, func(*string, money.Money) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || !projectMatches {
		t.Fatalf("expected found=true, projectMatches=true, got found=%v projectMatches=%v", found, projectMatches)
	}
	if isFinalized {
		t.Fatal("expected finalized=false for a still-draft estimate")
	}
}

func TestVisitQuotationSeedsIneligibleCostBasis(t *testing.T) {
	// A WorkItem group whose summed SnapshottedAmount is non-positive
	// causes allocationEligible=false, even though the Project-wide
	// CostSubtotal is itself positive (design spec §6.2's corrected
	// worked example: WorkItem A RM1,000, WorkItem B -RM200, subtotal
	// RM800).
	workItemA := "work_a"
	workItemB := "work_b"
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItemA, category: "material", estimated: money.New(100000, "MYR")},
		{costItemID: "c2", workItemID: &workItemB, category: "material", estimated: money.New(-20000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err := svc.FinalizeEstimate(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	visitorCalled := false
	_, _, found, projectMatches, isFinalized, allocationEligible, err := svc.VisitQuotationSeeds(
		context.Background(), "company_a", "project_1", finalized.ID,
		func(*string, money.Money) error { visitorCalled = true; return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || !projectMatches || !isFinalized {
		t.Fatalf("expected found/projectMatches/finalized all true, got %v %v %v", found, projectMatches, isFinalized)
	}
	if allocationEligible {
		t.Fatal("expected allocationEligible=false for a non-positive WorkItem cost-group weight")
	}
	if visitorCalled {
		t.Fatal("expected the visit callback to never be invoked when allocationEligible=false")
	}
}

func TestVisitQuotationSeedsWorkItemLessGroup(t *testing.T) {
	// A CostItem with no WorkItemID forms its own group (design spec §5.4)
	// — represented by workItemID=nil in the visit callback.
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: nil, category: "permit", estimated: money.New(50000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err := svc.FinalizeEstimate(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	var sawNilWorkItem bool
	_, _, _, _, _, _, err = svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", finalized.ID,
		func(workItemID *string, allocatedSellingAmount money.Money) error {
			if workItemID == nil {
				sawNilWorkItem = true
			}
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawNilWorkItem {
		t.Fatal("expected exactly one seed with workItemID=nil for the WorkItem-less CostItem")
	}
}
