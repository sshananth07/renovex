package quotations_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

// --- fakes ---

type fakeQuotationRepository struct {
	byID           map[string]quotations.Quotation
	next           int
	forceCreateErr error // when non-nil, Create returns this error unconditionally — simulates a duplicate-key collision this fake's own logic cannot otherwise construct (e.g. ErrUnclassifiedDuplicateKey)
	createCalls    int
	findMaxCalls   int
}

func newFakeQuotationRepository() *fakeQuotationRepository {
	return &fakeQuotationRepository{byID: make(map[string]quotations.Quotation)}
}

func (f *fakeQuotationRepository) Create(ctx context.Context, q quotations.Quotation) (quotations.Quotation, error) {
	f.createCalls++
	if f.forceCreateErr != nil {
		return quotations.Quotation{}, f.forceCreateErr
	}
	for _, existing := range f.byID {
		if existing.CompanyID != q.CompanyID || existing.QuotationNumber != q.QuotationNumber {
			continue
		}
		if existing.Version == q.Version {
			return quotations.Quotation{}, quotations.ErrVersionConflict
		}
		if existing.Status == quotations.QuotationStatusDraft && q.Status == quotations.QuotationStatusDraft {
			return quotations.Quotation{}, quotations.ErrDraftAlreadyExists
		}
	}
	f.next++
	q.ID = "quotation_" + string(rune('0'+f.next))
	f.byID[q.ID] = q
	return q, nil
}

func (f *fakeQuotationRepository) FindByID(ctx context.Context, companyID, id string) (quotations.Quotation, error) {
	q, ok := f.byID[id]
	if !ok || q.CompanyID != companyID {
		return quotations.Quotation{}, quotations.ErrQuotationNotFound
	}
	return q, nil
}

func (f *fakeQuotationRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]quotations.Quotation, error) {
	var result []quotations.Quotation
	for _, q := range f.byID {
		if q.CompanyID == companyID && q.ProjectID == projectID {
			result = append(result, q)
		}
	}
	return result, nil
}

func (f *fakeQuotationRepository) FindMaxVersion(ctx context.Context, companyID, quotationNumber string) (int, error) {
	f.findMaxCalls++
	max := 0
	for _, q := range f.byID {
		if q.CompanyID == companyID && q.QuotationNumber == quotationNumber && q.Version > max {
			max = q.Version
		}
	}
	return max, nil
}

func (f *fakeQuotationRepository) replaceConditional(companyID, id string, expectedRevision int64, apply func(*quotations.Quotation)) (quotations.Quotation, error) {
	q, ok := f.byID[id]
	if !ok || q.CompanyID != companyID {
		return quotations.Quotation{}, quotations.ErrQuotationNotFound
	}
	if q.Status != quotations.QuotationStatusDraft || q.Revision != expectedRevision {
		return quotations.Quotation{}, quotations.ErrRevisionMismatch
	}
	apply(&q)
	q.Revision++
	f.byID[id] = q
	return q, nil
}

func (f *fakeQuotationRepository) ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64, updated quotations.Quotation) (quotations.Quotation, error) {
	return f.replaceConditional(companyID, id, expectedRevision, func(q *quotations.Quotation) {
		q.Lines = updated.Lines
		q.Subtotal = updated.Subtotal
		q.TaxAmount = updated.TaxAmount
		q.Total = updated.Total
		// GeneratedSubtotal deliberately untouched.
	})
}

func (f *fakeQuotationRepository) ReplaceTerms(ctx context.Context, companyID, id string, expectedRevision int64, updated quotations.Quotation) (quotations.Quotation, error) {
	return f.replaceConditional(companyID, id, expectedRevision, func(q *quotations.Quotation) {
		q.Terms = updated.Terms
		q.PaymentSchedule = updated.PaymentSchedule
		q.Notes = updated.Notes
		q.ValidUntil = updated.ValidUntil
	})
}

func (f *fakeQuotationRepository) ReplaceTax(ctx context.Context, companyID, id string, expectedRevision int64, updated quotations.Quotation) (quotations.Quotation, error) {
	return f.replaceConditional(companyID, id, expectedRevision, func(q *quotations.Quotation) {
		q.TaxMode = updated.TaxMode
		q.TaxLabel = updated.TaxLabel
		q.TaxRateBPS = updated.TaxRateBPS
		q.TaxAmount = updated.TaxAmount
		q.Total = updated.Total
	})
}

func (f *fakeQuotationRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (quotations.Quotation, error) {
	q, ok := f.byID[id]
	if !ok || q.CompanyID != companyID {
		return quotations.Quotation{}, quotations.ErrQuotationNotFound
	}
	if q.Status != quotations.QuotationStatusDraft || q.Revision != expectedRevision {
		return quotations.Quotation{}, quotations.ErrRevisionMismatch
	}
	q.Status = quotations.QuotationStatusFinalized
	q.FinalizedAt = &finalizedAt
	f.byID[id] = q
	return q, nil
}

type fakeQuotationCounterRepository struct {
	counts map[string]int64
}

func newFakeQuotationCounterRepository() *fakeQuotationCounterRepository {
	return &fakeQuotationCounterRepository{counts: map[string]int64{}}
}

func (f *fakeQuotationCounterRepository) NextQuotationNumber(ctx context.Context, companyID string) (int64, error) {
	f.counts[companyID]++
	return f.counts[companyID], nil
}

type fakeProjectLookup struct {
	belongs  bool
	clientID string
}

func (f fakeProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}
func (f fakeProjectLookup) GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error) {
	if !f.belongs {
		return "", quotations.ErrProjectNotFound
	}
	return f.clientID, nil
}

type fakeWorkItemLookup struct {
	descriptions map[string]string // workItemID -> description; absent key means not found
}

func (f fakeWorkItemLookup) GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (string, bool, error) {
	desc, ok := f.descriptions[workItemID]
	return desc, ok, nil
}

// fakeEstimateSource simulates estimates.Service.VisitQuotationSeeds
// against a fixed, pre-configured eligibility/seed script — never
// carrying any cost figure, matching the real method's return contract
// exactly.
type fakeEstimateSource struct {
	found                bool
	projectMatches       bool
	finalized            bool
	allocationEligible   bool
	proposedSellingPrice money.Money
	currency             string
	seeds                []struct {
		workItemID *string
		amount     money.Money
	}
}

func (f fakeEstimateSource) VisitQuotationSeeds(ctx context.Context, companyID, projectID, estimateID string,
	visit func(workItemID *string, allocatedSellingAmount money.Money) error,
) (money.Money, string, bool, bool, bool, bool, error) {
	if !f.found || !f.projectMatches || !f.finalized || !f.allocationEligible {
		return money.Money{}, "", f.found, f.projectMatches, f.finalized, f.allocationEligible, nil
	}
	for _, s := range f.seeds {
		if err := visit(s.workItemID, s.amount); err != nil {
			return money.Money{}, "", false, false, false, false, err
		}
	}
	return f.proposedSellingPrice, f.currency, true, true, true, true, nil
}

func workItemIDPtr(s string) *string { return &s }

func eligibleEstimateSource() fakeEstimateSource {
	return fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(62500, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(50000, "MYR")},
			{workItemID: workItemIDPtr("work_2"), amount: money.New(12500, "MYR")},
		},
	}
}

func defaultWorkItemLookup() fakeWorkItemLookup {
	return fakeWorkItemLookup{descriptions: map[string]string{
		"work_1": "Wall Tiles", "work_2": "Floor Tiles",
	}}
}

// --- CreateQuotation ---

func TestCreateQuotationHappyPath(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	q, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.QuotationNumber != "QT-000001" {
		t.Fatalf("expected QT-000001, got %s", q.QuotationNumber)
	}
	if q.Version != 1 {
		t.Fatalf("expected Version 1, got %d", q.Version)
	}
	if q.Status != quotations.QuotationStatusDraft {
		t.Fatal("expected draft status")
	}
	if q.ClientID != "client_1" {
		t.Fatalf("expected ClientID client_1, got %s", q.ClientID)
	}
	if len(q.Lines) != 2 {
		t.Fatalf("expected 2 generated lines, got %d", len(q.Lines))
	}
	if q.Lines[0].Description != "Wall Tiles" || q.Lines[1].Description != "Floor Tiles" {
		t.Fatalf("expected descriptions resolved via WorkItemLookup, got %+v", q.Lines)
	}
	sum := int64(0)
	for _, l := range q.Lines {
		sum += l.Amount.Amount
	}
	if sum != q.Subtotal.Amount || sum != q.GeneratedSubtotal.Amount || sum != 62500 {
		t.Fatalf("expected Subtotal == GeneratedSubtotal == sum(lines) == 62500, got sum=%d subtotal=%d generatedSubtotal=%d",
			sum, q.Subtotal.Amount, q.GeneratedSubtotal.Amount)
	}
	if q.TaxMode != quotations.TaxModeNone {
		t.Fatal("expected TaxMode=none by default")
	}
	if q.Total.Amount != q.Subtotal.Amount {
		t.Fatal("expected Total == Subtotal when TaxMode=none")
	}
}

func TestCreateQuotationProjectNotFound(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: false}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCreateQuotationEstimateNotFound(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrEstimateNotFound {
		t.Fatalf("expected ErrEstimateNotFound, got %v", err)
	}
}

func TestCreateQuotationEstimateProjectMismatch(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: true, projectMatches: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrEstimateProjectMismatch {
		t.Fatalf("expected ErrEstimateProjectMismatch, got %v", err)
	}
}

func TestCreateQuotationEstimateNotFinalized(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: true, projectMatches: true, finalized: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrEstimateNotFinalized {
		t.Fatalf("expected ErrEstimateNotFinalized, got %v", err)
	}
}

func TestCreateQuotationIneligibleCostBasis(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: true, projectMatches: true, finalized: true, allocationEligible: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrIneligibleCostBasisForQuotation {
		t.Fatalf("expected ErrIneligibleCostBasisForQuotation, got %v", err)
	}
}

func TestCreateQuotationEmptySeedSetRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		// no seeds configured — visitor invoked zero times
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrNoQuotationSeedsGenerated {
		t.Fatalf("expected ErrNoQuotationSeedsGenerated, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationSeedCurrencyMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(50000, "USD")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrQuotationSeedCurrencyMismatch {
		t.Fatalf("expected ErrQuotationSeedCurrencyMismatch, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationSeedTotalMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(40000, "MYR")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrQuotationSeedTotalMismatch {
		t.Fatalf("expected ErrQuotationSeedTotalMismatch, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationZeroSeedAmountRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(0, "MYR")},
			{workItemID: workItemIDPtr("work_2"), amount: money.New(50000, "MYR")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrQuotationSeedAmountNotPositive {
		t.Fatalf("expected ErrQuotationSeedAmountNotPositive, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationNegativeSeedAmountRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(-1000, "MYR")},
			{workItemID: workItemIDPtr("work_2"), amount: money.New(51000, "MYR")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrQuotationSeedAmountNotPositive {
		t.Fatalf("expected ErrQuotationSeedAmountNotPositive, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationProposedSellingPriceCurrencyMismatchIsCaughtByBoundaryCheck(t *testing.T) {
	// If ProposedSellingPrice.Currency ever diverged from the returned
	// currency, the seed-total-vs-price comparison itself would fail
	// (Money.Add/Equal requires matching currencies) — proving the
	// boundary check also catches this case, not just seed-vs-seed
	// mismatches.
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "USD"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(50000, "MYR")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrQuotationSeedTotalMismatch {
		t.Fatalf("expected ErrQuotationSeedTotalMismatch, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationUnresolvedWorkItemRejected(t *testing.T) {
	// A seed carries a non-nil workItemID that GetWorkItemDescription
	// cannot resolve — e.g. the WorkItem was deleted after the Estimate
	// was finalized. Must fail with ErrWorkItemNotFound, never silently
	// fall back to the WorkItem-less default description.
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("deleted_work_item"), amount: money.New(50000, "MYR")},
		},
	}
	lookup := fakeWorkItemLookup{descriptions: map[string]string{}} // nothing resolves
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, lookup, source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound, got %v", err)
	}
	if len(repo.byID) != 0 {
		t.Fatalf("expected no quotation to be created, got %d", len(repo.byID))
	}
}

func TestCreateQuotationWorkItemLessLineDefaultDescription(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: nil, amount: money.New(50000, "MYR")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	q, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(q.Lines))
	}
	if q.Lines[0].Description != "General Project Works and Services" {
		t.Fatalf("expected default WorkItem-less description, got %q", q.Lines[0].Description)
	}
	if len(q.Lines[0].SourceWorkItemIDs) != 0 {
		t.Fatalf("expected empty SourceWorkItemIDs for a WorkItem-less line, got %v", q.Lines[0].SourceWorkItemIDs)
	}
}

func TestCreateQuotationSequentialNumbering(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	first, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := svc.CreateQuotation(context.Background(), "company_a", "project_2", "estimate_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.QuotationNumber != "QT-000001" || second.QuotationNumber != "QT-000002" {
		t.Fatalf("expected sequential numbering, got %s then %s", first.QuotationNumber, second.QuotationNumber)
	}
}

// --- CreateQuotation duplicate-key classification / no-retry behaviour ---

func TestCreateQuotationAttemptsVersion1ExactlyOnceAndDoesNotCallFindMaxVersion(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("expected exactly 1 Create call, got %d", repo.createCalls)
	}
	if repo.findMaxCalls != 0 {
		t.Fatalf("expected CreateQuotation to never call FindMaxVersion, got %d calls", repo.findMaxCalls)
	}
}

func TestCreateQuotationPreservesErrVersionConflictWithoutRetry(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	// Pre-seed a Version=1 (finalized, so it does NOT also collide on the
	// one-draft-per-number index) to force a version-number collision on
	// the next CreateQuotation attempt.
	repo.byID["existing"] = quotations.Quotation{
		CompanyID: "company_a", QuotationNumber: "QT-000001", Version: 1, Status: quotations.QuotationStatusFinalized,
	}
	// Force the counter to hand out 1 again so Create collides at Version=1.
	counterRepo.counts["company_a"] = 0

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict preserved as-is, got %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("expected CreateQuotation to attempt Create exactly once (no retry), got %d calls", repo.createCalls)
	}
}

func TestCreateQuotationPreservesErrDraftAlreadyExistsWithoutRetry(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	// Pre-seed a draft at a DIFFERENT version under the same
	// QuotationNumber, so the fake's Create hits the
	// one-draft-per-number branch rather than the exact-version branch.
	repo.byID["existing"] = quotations.Quotation{
		CompanyID: "company_a", QuotationNumber: "QT-000001", Version: 2, Status: quotations.QuotationStatusDraft,
	}
	counterRepo.counts["company_a"] = 0

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrDraftAlreadyExists {
		t.Fatalf("expected ErrDraftAlreadyExists preserved as-is (not collapsed into ErrVersionConflict), got %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("expected CreateQuotation to attempt Create exactly once (no retry), got %d calls", repo.createCalls)
	}
}

func TestCreateQuotationPreservesErrUnclassifiedDuplicateKeyWithoutRetry(t *testing.T) {
	repo := newFakeQuotationRepository()
	repo.forceCreateErr = quotations.ErrUnclassifiedDuplicateKey
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrUnclassifiedDuplicateKey {
		t.Fatalf("expected ErrUnclassifiedDuplicateKey preserved as-is, got %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("expected CreateQuotation to attempt Create exactly once (no retry), got %d calls", repo.createCalls)
	}
}

// --- CreateNewVersion / allocateAndCreateVersion retry behaviour ---

// retryFakeQuotationRepository wraps fakeQuotationRepository's Create to
// return ErrVersionConflict for the first N calls, then delegate normally —
// used to prove allocateAndCreateVersion actually retries transient
// conflicts and re-reads MAX(version) before each attempt.
type retryFakeQuotationRepository struct {
	*fakeQuotationRepository
	failFirstNCreates int
	createAttempts    int
}

func (f *retryFakeQuotationRepository) Create(ctx context.Context, q quotations.Quotation) (quotations.Quotation, error) {
	f.createAttempts++
	if f.createAttempts <= f.failFirstNCreates {
		f.fakeQuotationRepository.createCalls++
		return quotations.Quotation{}, quotations.ErrVersionConflict
	}
	return f.fakeQuotationRepository.Create(ctx, q)
}

func TestCreateNewVersionRetriesOnlyErrVersionConflictAndSucceeds(t *testing.T) {
	base := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	setupSvc := quotations.NewService(base, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	v1, err := setupSvc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	finalized := v1
	finalized.Status = quotations.QuotationStatusFinalized
	base.byID[v1.ID] = finalized

	// Only now wrap Create to force 2 transient conflicts before the
	// CreateNewVersion call — v1's own creation above went through
	// cleanly, unaffected by the wrapper.
	repo := &retryFakeQuotationRepository{fakeQuotationRepository: base, failFirstNCreates: 2}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	findMaxCallsBeforeRetry := base.findMaxCalls
	v2, err := svc.CreateNewVersion(context.Background(), "company_a", v1.ID, "estimate_1")
	if err != nil {
		t.Fatalf("expected CreateNewVersion to succeed after transient conflicts, got %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version 2, got %d", v2.Version)
	}
	// 2 forced failures + 1 success = 3 Create attempts.
	if repo.createAttempts != 3 {
		t.Fatalf("expected 3 Create attempts (2 failures + 1 success), got %d", repo.createAttempts)
	}
	// FindMaxVersion must be re-read before EACH attempt.
	if base.findMaxCalls-findMaxCallsBeforeRetry != 3 {
		t.Fatalf("expected FindMaxVersion re-read 3 times (once per attempt), got %d", base.findMaxCalls-findMaxCallsBeforeRetry)
	}
}

func TestCreateNewVersionStopsAfterRetryLimit(t *testing.T) {
	base := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	setupSvc := quotations.NewService(base, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	v1, err := setupSvc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	finalized := v1
	finalized.Status = quotations.QuotationStatusFinalized
	base.byID[v1.ID] = finalized

	repo := &retryFakeQuotationRepository{fakeQuotationRepository: base, failFirstNCreates: 1000} // always fails
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err = svc.CreateNewVersion(context.Background(), "company_a", v1.ID, "estimate_1")
	if err != quotations.ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict after exhausting the retry limit, got %v", err)
	}
	// The retry loop is bounded at 5 attempts (maxVersionAllocationAttempts).
	if repo.createAttempts != 5 {
		t.Fatalf("expected exactly 5 bounded Create attempts, got %d", repo.createAttempts)
	}
}

func TestCreateNewVersionNeverRetriesErrDraftAlreadyExists(t *testing.T) {
	base := newFakeQuotationRepository()
	base.forceCreateErr = quotations.ErrDraftAlreadyExists
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(base, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	// Seed a finalized source quotation directly (bypassing CreateQuotation,
	// since base.forceCreateErr would also break that call).
	base.byID["source"] = quotations.Quotation{
		ID: "source", CompanyID: "company_a", ProjectID: "project_1", ClientID: "client_1",
		QuotationNumber: "QT-000001", Version: 1, Status: quotations.QuotationStatusFinalized,
		Currency: "MYR",
	}

	_, err := svc.CreateNewVersion(context.Background(), "company_a", "source", "estimate_1")
	if err != quotations.ErrDraftAlreadyExists {
		t.Fatalf("expected ErrDraftAlreadyExists propagated immediately, got %v", err)
	}
	if base.createCalls != 1 {
		t.Fatalf("expected exactly 1 Create attempt (ErrDraftAlreadyExists must never be retried), got %d", base.createCalls)
	}
}

func TestCreateNewVersionNeverRetriesErrUnclassifiedDuplicateKey(t *testing.T) {
	base := newFakeQuotationRepository()
	base.forceCreateErr = quotations.ErrUnclassifiedDuplicateKey
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(base, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	base.byID["source"] = quotations.Quotation{
		ID: "source", CompanyID: "company_a", ProjectID: "project_1", ClientID: "client_1",
		QuotationNumber: "QT-000001", Version: 1, Status: quotations.QuotationStatusFinalized,
		Currency: "MYR",
	}

	_, err := svc.CreateNewVersion(context.Background(), "company_a", "source", "estimate_1")
	if err != quotations.ErrUnclassifiedDuplicateKey {
		t.Fatalf("expected ErrUnclassifiedDuplicateKey propagated immediately, got %v", err)
	}
	if base.createCalls != 1 {
		t.Fatalf("expected exactly 1 Create attempt (ErrUnclassifiedDuplicateKey must never be retried), got %d", base.createCalls)
	}
}

// --- ReplaceLines ---

func decimalPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}
func stringPtr(s string) *string { return &s }

func TestReplaceLinesHappyPath(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1", "work_2"}, Description: "Combined Tiling Works", Amount: money.New(60000, "MYR")},
	}
	updated, err := svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Lines) != 1 {
		t.Fatalf("expected 1 merged line, got %d", len(updated.Lines))
	}
	if updated.Subtotal.Amount != 60000 {
		t.Fatalf("expected Subtotal recomputed to 60000, got %d", updated.Subtotal.Amount)
	}
	if updated.GeneratedSubtotal.Amount != created.GeneratedSubtotal.Amount {
		t.Fatalf("expected GeneratedSubtotal to remain frozen at %d, got %d", created.GeneratedSubtotal.Amount, updated.GeneratedSubtotal.Amount)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("expected Revision incremented, got %d", updated.Revision)
	}
}

func TestReplaceLinesSplitAcrossTwoLines(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Split work_1's value across two lines — the same WorkItemID appears
	// on both, which must be accepted (design spec §5.3).
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1"}, Description: "Wall Tiles - Supply", Amount: money.New(30000, "MYR")},
		{SourceWorkItemIDs: []string{"work_1"}, Description: "Wall Tiles - Install", Amount: money.New(20000, "MYR")},
	}
	updated, err := svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error splitting one WorkItem across two lines: %v", err)
	}
	if len(updated.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(updated.Lines))
	}
}

func TestReplaceLinesEmptyArrayRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, nil, created.Revision)
	if err != quotations.ErrEmptyLines {
		t.Fatalf("expected ErrEmptyLines, got %v", err)
	}
}

func TestReplaceLinesDuplicateWorkItemInOneLineRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1", "work_1"}, Description: "Bad Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrDuplicateWorkItemIDInLine {
		t.Fatalf("expected ErrDuplicateWorkItemIDInLine, got %v", err)
	}
}

func TestReplaceLinesUnknownWorkItemRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"nonexistent_work_item"}, Description: "Bad Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound, got %v", err)
	}
}

func TestReplaceLinesUnknownLineIDRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{ID: "line-that-does-not-exist", SourceWorkItemIDs: []string{}, Description: "Bad Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrUnknownLineID {
		t.Fatalf("expected ErrUnknownLineID, got %v", err)
	}
}

func TestReplaceLinesDuplicateLineIDRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	existingID := created.Lines[0].ID

	newLines := []quotations.QuotationLine{
		{ID: existingID, SourceWorkItemIDs: []string{"work_1"}, Description: "A", Amount: money.New(10000, "MYR")},
		{ID: existingID, SourceWorkItemIDs: []string{"work_2"}, Description: "B", Amount: money.New(20000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrDuplicateLineID {
		t.Fatalf("expected ErrDuplicateLineID, got %v", err)
	}
}

func TestReplaceLinesBlankDescriptionRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "   ", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrBlankLineDescription {
		t.Fatalf("expected ErrBlankLineDescription, got %v", err)
	}
}

func TestReplaceLinesIncompletePricingRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Partial", Quantity: decimalPtr("30"), Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrIncompleteLinePricing {
		t.Fatalf("expected ErrIncompleteLinePricing, got %v", err)
	}
}

func TestReplaceLinesZeroUnitPriceRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	zero := money.New(0, "MYR")
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Free Item", Quantity: decimalPtr("1"), Unit: stringPtr("unit"), UnitPrice: &zero, Amount: money.New(0, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrInvalidLineUnitPrice {
		t.Fatalf("expected ErrInvalidLineUnitPrice specifically for a zero unit price, got %v", err)
	}
}

func TestReplaceLinesBlankUnitRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	unitPrice := money.New(1000, "MYR")
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Whitespace Unit", Quantity: decimalPtr("1"), Unit: stringPtr("   "), UnitPrice: &unitPrice, Amount: money.New(1000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrInvalidLineUnit {
		t.Fatalf("expected ErrInvalidLineUnit for a whitespace-only unit, got %v", err)
	}
}

func TestReplaceLinesLineAmountMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	unitPrice := money.New(12000, "MYR") // RM120/m2
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Floor Tiles", Quantity: decimalPtr("30"), Unit: stringPtr("m2"), UnitPrice: &unitPrice, Amount: money.New(999999, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrLineAmountMismatch {
		t.Fatalf("expected ErrLineAmountMismatch, got %v", err)
	}
}

func TestReplaceLinesCurrencyMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Wrong Currency", Amount: money.New(10000, "USD")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrLineCurrencyMismatch {
		t.Fatalf("expected ErrLineCurrencyMismatch, got %v", err)
	}

	// Draft must be left completely untouched by the rejected write.
	unchanged, err := svc.GetQuotation(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error re-fetching: %v", err)
	}
	if unchanged.Revision != created.Revision {
		t.Fatal("expected the draft's Revision to be unchanged after a rejected line submission")
	}
	if len(unchanged.Lines) != len(created.Lines) {
		t.Fatal("expected the draft's Lines to be unchanged after a rejected line submission")
	}
}

func TestReplaceLinesStaleRevisionRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision+99)
	if err != quotations.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

func TestReplaceLinesFinalizedRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.FinalizeQuotation(context.Background(), "company_a", created.ID, created.Revision); err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrQuotationNotDraft {
		t.Fatalf("expected ErrQuotationNotDraft, got %v", err)
	}
}

// --- UpdateTax ---

func TestUpdateTaxPercentageHappyPath(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "SST 6%", 600, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedTax := money.ApplyRateBPS(created.Subtotal, 600)
	if updated.TaxAmount.Amount != expectedTax.Amount {
		t.Fatalf("expected TaxAmount %d, got %d", expectedTax.Amount, updated.TaxAmount.Amount)
	}
	expectedTotal := created.Subtotal.Amount + expectedTax.Amount
	if updated.Total.Amount != expectedTotal {
		t.Fatalf("expected Total %d, got %d", expectedTotal, updated.Total.Amount)
	}
}

func TestUpdateTaxNoneWithNonEmptyLabelRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModeNone, "SST 6%", 0, created.Revision)
	if err != quotations.ErrInvalidTaxLabel {
		t.Fatalf("expected ErrInvalidTaxLabel, got %v", err)
	}
}

func TestUpdateTaxNoneWithNonZeroRateRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModeNone, "", 600, created.Revision)
	if err != quotations.ErrInvalidTaxRate {
		t.Fatalf("expected ErrInvalidTaxRate, got %v", err)
	}
}

func TestUpdateTaxPercentageBlankLabelRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "   ", 600, created.Revision)
	if err != quotations.ErrInvalidTaxLabel {
		t.Fatalf("expected ErrInvalidTaxLabel, got %v", err)
	}
}

func TestUpdateTaxPercentageZeroRateRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "SST 0%", 0, created.Revision)
	if err != quotations.ErrInvalidTaxRate {
		t.Fatalf("expected ErrInvalidTaxRate for a zero rate under percentage mode, got %v", err)
	}
}

func TestUpdateTaxPercentageAboveMaxRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "Over 100%", 10001, created.Revision)
	if err != quotations.ErrInvalidTaxRate {
		t.Fatalf("expected ErrInvalidTaxRate for >100%%, got %v", err)
	}
}

func TestUpdateTaxInvalidModeRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxMode("fixed"), "Fixed Tax", 100, created.Revision)
	if err != quotations.ErrInvalidTaxMode {
		t.Fatalf("expected ErrInvalidTaxMode, got %v", err)
	}
}

// --- FinalizeQuotation ---

func TestFinalizeQuotationIdempotent(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	first, err := svc.FinalizeQuotation(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Retry with a deliberately wrong expectedRevision — idempotent
	// finalize must succeed anyway (design spec §12).
	second, err := svc.FinalizeQuotation(context.Background(), "company_a", created.ID, 999)
	if err != nil {
		t.Fatalf("unexpected error on idempotent retry: %v", err)
	}
	if second.Status != quotations.QuotationStatusFinalized || second.Revision != first.Revision {
		t.Fatal("expected idempotent finalize retry to return the unchanged finalized document")
	}
}

func TestFinalizeQuotationStaleRevisionRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.FinalizeQuotation(context.Background(), "company_a", created.ID, created.Revision+99)
	if err != quotations.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

// --- CreateNewVersion ---

func TestCreateNewVersionRequiresFinalizedSource(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.CreateNewVersion(context.Background(), "company_a", created.ID, "estimate_2")
	if err != quotations.ErrQuotationMustBeFinalizedBeforeNewVersion {
		t.Fatalf("expected ErrQuotationMustBeFinalizedBeforeNewVersion, got %v", err)
	}
}

func TestCreateNewVersionFreshRegenerationNotCarriedForward(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := eligibleEstimateSource()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	v1, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Hand-edit V1's lines away from their generated values before
	// finalizing.
	handEdited := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1", "work_2"}, Description: "Combined", Amount: money.New(99999, "MYR")},
	}
	v1, err = svc.ReplaceLines(context.Background(), "company_a", v1.ID, handEdited, v1.Revision)
	if err != nil {
		t.Fatalf("unexpected error hand-editing: %v", err)
	}
	v1, err = svc.FinalizeQuotation(context.Background(), "company_a", v1.ID, v1.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing v1: %v", err)
	}

	// A DIFFERENT Estimate, with different underlying seed amounts,
	// referenced explicitly for V2.
	source2 := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(80000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(80000, "MYR")},
		},
	}
	svc2 := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source2)

	v2, err := svc2.CreateNewVersion(context.Background(), "company_a", v1.ID, "estimate_2")
	if err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version 2, got %d", v2.Version)
	}
	if v2.EstimateID != "estimate_2" {
		t.Fatalf("expected EstimateID estimate_2, got %s", v2.EstimateID)
	}
	if v2.GeneratedSubtotal.Amount != 80000 {
		t.Fatalf("expected a FRESH GeneratedSubtotal of 80000 from the new Estimate, NOT v1's hand-edited 99999, got %d", v2.GeneratedSubtotal.Amount)
	}
	if v2.QuotationNumber != v1.QuotationNumber {
		t.Fatal("expected QuotationNumber to be carried forward unchanged")
	}
}

func TestCreateNewVersionCurrencyMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := eligibleEstimateSource()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	v1, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v1, err = svc.FinalizeQuotation(context.Background(), "company_a", v1.ID, v1.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}

	sgdSource := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(80000, "SGD"), currency: "SGD",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(80000, "SGD")},
		},
	}
	svc2 := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), sgdSource)

	_, err = svc2.CreateNewVersion(context.Background(), "company_a", v1.ID, "estimate_sgd")
	if err != quotations.ErrQuotationCurrencyMismatch {
		t.Fatalf("expected ErrQuotationCurrencyMismatch, got %v", err)
	}
}

// --- ListQuotationsByProject ---

func TestListQuotationsByProjectForeignProjectRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: false}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err := svc.ListQuotationsByProject(context.Background(), "company_a", "project_1")
	if err != quotations.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

// --- cross-tenant ---

func TestGetQuotationCrossTenant(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GetQuotation(context.Background(), "company_b", created.ID)
	if err != quotations.ErrQuotationNotFound {
		t.Fatalf("expected ErrQuotationNotFound for a cross-tenant lookup, got %v", err)
	}
}
