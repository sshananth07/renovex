package estimates

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrProjectNotFound is returned when the given projectID does not belong
// to the caller's company.
var ErrProjectNotFound = errors.New("estimates: project not found")

// ErrEstimateMustBeFinalizedBeforeNewVersion is returned by CreateNewVersion
// when the source Estimate is not yet Status=finalized (design spec §17
// step 2).
var ErrEstimateMustBeFinalizedBeforeNewVersion = errors.New("estimates: source estimate must be finalized before creating a new version")

// ErrEstimateNotDraft is returned when refresh or pricing recalculation is
// attempted against an Estimate that is not Status=draft.
var ErrEstimateNotDraft = errors.New("estimates: estimate is not a draft")

// ProjectLookup is the capability estimates needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// EstimatedCostSource is the capability estimates needs from costs (design
// spec §6). Satisfied structurally by costs.Service.
type EstimatedCostSource interface {
	VisitEstimatedCostItems(
		ctx context.Context,
		companyID string,
		projectID string,
		visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
	) (missingEstimatedCount int, err error)
}

// Service implements Estimate creation, versioning, refresh, pricing
// recalculation, and finalization.
type Service struct {
	repo          EstimateRepository
	projectLookup ProjectLookup
	costSource    EstimatedCostSource
}

// NewService constructs a Service backed by repo, consuming projectLookup
// and costSource to validate the Project and build cost snapshots.
func NewService(repo EstimateRepository, projectLookup ProjectLookup, costSource EstimatedCostSource) *Service {
	return &Service{repo: repo, projectLookup: projectLookup, costSource: costSource}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public EstimateRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Estimate owned by
// companyID. Development-tool use only (demoseed reset, design spec §6.6).
// Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("estimates: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// buildSnapshot visits every eligible CostItem under projectID via
// costSource, validates currency/positivity via SumLines, and returns the
// resulting Lines/CostSubtotal/ExcludedCostItemCount. Shared by
// CreateEstimate, CreateNewVersion, and RefreshEstimate — no
// snapshot-building logic is duplicated across entry points (design spec
// §25).
func (s *Service) buildSnapshot(ctx context.Context, companyID, projectID string) ([]EstimateCostLine, money.Money, int, error) {
	var lines []EstimateCostLine
	missingCount, err := s.costSource.VisitEstimatedCostItems(ctx, companyID, projectID,
		func(costItemID string, workItemID *string, category, description string, estimated money.Money) error {
			lines = append(lines, EstimateCostLine{
				SourceCostItemID: costItemID, WorkItemID: workItemID,
				Category: category, Description: description, SnapshottedAmount: estimated,
			})
			return nil
		})
	if err != nil {
		return nil, money.Money{}, 0, err
	}

	subtotal, err := SumLines(lines)
	if err != nil {
		return nil, money.Money{}, 0, err
	}

	return lines, subtotal, missingCount, nil
}

// ErrEstimateAlreadyExistsForProject is returned by CreateEstimate when the
// Project already has at least one Estimate version (i.e. MAX(version) > 0).
// CreateEstimate creates ONLY Version 1 for a Project — creating any
// subsequent version is exclusively CreateNewVersion's job, and that path
// requires the source to be finalized (design spec §17 step 2). Without
// this check, POST /estimates could be called repeatedly to silently mint
// V2, V3, ... bypassing the "new version requires a finalized source" rule
// entirely.
var ErrEstimateAlreadyExistsForProject = errors.New("estimates: an estimate already exists for this project; use CreateNewVersion from a finalized version instead")

const maxVersionAllocationAttempts = 5

// allocateAndCreate implements the bounded-retry version-number allocation
// strategy from design spec §21.2 steps 1-3/5-6, IN PRODUCTION CODE (not
// merely in a test helper): read MAX(version), attempt Create at MAX+1,
// and on ErrVersionConflict (another concurrent writer claimed that exact
// version number first) re-read MAX(version) and retry, bounded at
// maxVersionAllocationAttempts. A collision on the OTHER unique index
// (ErrDraftAlreadyExists — a draft already exists for this project) is NOT
// retried here; it is propagated immediately, since retrying "create a new
// draft" when one already exists is not a transient condition (design spec
// §21.2 step 4).
//
// Used ONLY by CreateNewVersion — CreateEstimate deliberately does NOT use
// this helper, and must never be changed to do so: retrying into a higher
// version number is exactly the bypass of "POST /estimates creates ONLY
// Version 1" that CreateEstimate exists to prevent. The two methods have
// genuinely different concurrency contracts — this helper's
// retry-into-the-next-version behavior is correct for CreateNewVersion and
// wrong for CreateEstimate. build is called fresh on every attempt with
// the freshly-read version number, so it can close over whatever fields
// CreateNewVersion needs to stamp onto the new document.
func (s *Service) allocateAndCreate(ctx context.Context, companyID, projectID string, build func(version int) Estimate) (Estimate, error) {
	for attempt := 0; attempt < maxVersionAllocationAttempts; attempt++ {
		maxVersion, err := s.repo.FindMaxVersion(ctx, companyID, projectID)
		if err != nil {
			return Estimate{}, err
		}
		created, err := s.repo.Create(ctx, build(maxVersion+1))
		if err == nil {
			return created, nil
		}
		if err == ErrVersionConflict {
			continue // another goroutine won this version number; retry
		}
		return Estimate{}, err // includes ErrDraftAlreadyExists, propagated immediately (not retried)
	}
	return Estimate{}, ErrVersionConflict
}

// CreateEstimate creates Version 1 for a Project (always a draft) — and
// ONLY EVER Version 1, with NO retry loop. Validates projectID via
// ProjectLookup, builds the cost snapshot via buildSnapshot, and
// calculates pricing (design spec §4, §17, §23).
//
// Why this does NOT call allocateAndCreate: an earlier design checked
// existingMax > 0 up front, but then delegated the write to a SHARED
// retrying helper, which recalculates MAX(version)+1 on every retry
// attempt. That reintroduces the exact race the check was meant to close:
//
//	Request A reads MAX(version) = 0 (via the up-front existingMax check)
//	Request B reads MAX(version) = 0 (same up-front check, same instant)
//	A proceeds, creates Version 1, it later gets finalized
//	B's CALL TO the shared retrying helper re-reads MAX(version) — now 1 —
//	  and would attempt Version 2, succeeding via POST /estimates
//
// That is a silent bypass of "POST /estimates creates ONLY Version 1" —
// exactly the invariant this method exists to enforce. The fix is to
// remove retry semantics from CreateEstimate entirely: attempt Version 1
// directly, exactly once. ANY collision on either unique index (a
// concurrent Version-1 creation racing this one, OR an existing later
// version already present) is classified as
// ErrEstimateAlreadyExistsForProject — never retried into Version 2.
// allocateAndCreate (with its MAX+1 retry semantics) remains correct and
// necessary for CreateNewVersion below, where retrying into the NEXT
// version number is exactly the desired behavior — the two methods have
// genuinely different concurrency contracts and must not share one helper.
func (s *Service) CreateEstimate(ctx context.Context, companyID, projectID string, pricingMode PricingMode, pricingRate money.RateBPS) (Estimate, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}
	if !belongs {
		return Estimate{}, ErrProjectNotFound
	}

	existingMax, err := s.repo.FindMaxVersion(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}
	if existingMax > 0 {
		return Estimate{}, ErrEstimateAlreadyExistsForProject
	}

	lines, subtotal, missingCount, err := s.buildSnapshot(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}

	pricing, err := CalculatePricing(subtotal, pricingMode, pricingRate)
	if err != nil {
		return Estimate{}, err
	}

	created, err := s.repo.Create(ctx, Estimate{
		CompanyID: companyID, ProjectID: projectID, Version: 1, Status: EstimateStatusDraft, Revision: 0,
		Currency: subtotal.Currency, Lines: lines, CostSubtotal: subtotal, ExcludedCostItemCount: missingCount,
		PricingMode: pricingMode, PricingRate: pricingRate,
		ProposedSellingPrice: pricing.ProposedSellingPrice, ProjectedGrossProfit: pricing.ProjectedGrossProfit,
		ProjectedGrossMarginBPS: pricing.ProjectedGrossMarginBPS,
		CreatedAt:               time.Now(), SchemaVersion: 1,
	})
	if err == ErrVersionConflict || err == ErrDraftAlreadyExists {
		// Either collision means "an estimate already exists for this
		// project" from CreateEstimate's point of view — a concurrent
		// request won the race to create Version 1 first (ErrVersionConflict
		// on the {companyId,projectId,version} index), or a draft already
		// exists for some other reason (ErrDraftAlreadyExists on the
		// partial index). NEITHER is retried here — retrying would either
		// recompute a higher version number (exactly the bypass this fix
		// closes) or spin uselessly against an existing draft. The correct
		// caller action in both cases is the same: this project already has
		// an Estimate; use CreateNewVersion once it's finalized.
		return Estimate{}, ErrEstimateAlreadyExistsForProject
	}
	if err != nil {
		return Estimate{}, err
	}
	return created, nil
}

// GetEstimate returns estimateID's Estimate, tenant-scoped to companyID.
func (s *Service) GetEstimate(ctx context.Context, companyID, estimateID string) (Estimate, error) {
	return s.repo.FindByID(ctx, companyID, estimateID)
}

// GetLatestEstimate returns the highest-Version Estimate for projectID,
// tenant-scoped, after validating projectID belongs to companyID.
func (s *Service) GetLatestEstimate(ctx context.Context, companyID, projectID string) (Estimate, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}
	if !belongs {
		return Estimate{}, ErrProjectNotFound
	}
	return s.repo.FindLatestByProject(ctx, companyID, projectID)
}

// ListEstimatesByProject validates projectID belongs to companyID before
// listing — a foreign projectID returns ErrProjectNotFound, never an empty
// list (design spec §19.6).
func (s *Service) ListEstimatesByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// RecalculatePricing recomputes pricing fields from the ALREADY-STORED
// CostSubtotal — never re-touches cost_items. Draft-only; rejects
// (ErrEstimateNotDraft) if Status != draft. Revision-guarded: expectedRevision
// must match the document's current Revision or ErrRevisionMismatch is
// returned (design spec §16.1).
func (s *Service) RecalculatePricing(ctx context.Context, companyID, estimateID string, pricingMode PricingMode, pricingRate money.RateBPS, expectedRevision int64) (Estimate, error) {
	existing, err := s.repo.FindByID(ctx, companyID, estimateID)
	if err != nil {
		return Estimate{}, err
	}
	if existing.Status != EstimateStatusDraft {
		return Estimate{}, ErrEstimateNotDraft
	}

	pricing, err := CalculatePricing(existing.CostSubtotal, pricingMode, pricingRate)
	if err != nil {
		return Estimate{}, err
	}

	updated := existing
	updated.PricingMode = pricingMode
	updated.PricingRate = pricingRate
	updated.ProposedSellingPrice = pricing.ProposedSellingPrice
	updated.ProjectedGrossProfit = pricing.ProjectedGrossProfit
	updated.ProjectedGrossMarginBPS = pricing.ProjectedGrossMarginBPS

	return s.repo.UpdatePricing(ctx, companyID, estimateID, expectedRevision, updated)
}

// RefreshEstimate re-pulls current cost_items into this draft, in place,
// keeping the same Version. Recomputes pricing against the new
// CostSubtotal using the currently-stored PricingMode/PricingRate. Draft-only;
// rejects (ErrEstimateNotDraft) if Status != draft. Revision-guarded, same
// contract as RecalculatePricing. On any snapshot-building failure (no
// eligible cost items, mixed currency, non-positive subtotal), the existing
// draft is left completely untouched — no partial update (design spec
// §16.2).
func (s *Service) RefreshEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (Estimate, error) {
	existing, err := s.repo.FindByID(ctx, companyID, estimateID)
	if err != nil {
		return Estimate{}, err
	}
	if existing.Status != EstimateStatusDraft {
		return Estimate{}, ErrEstimateNotDraft
	}

	lines, subtotal, missingCount, err := s.buildSnapshot(ctx, companyID, existing.ProjectID)
	if err != nil {
		return Estimate{}, err
	}

	pricing, err := CalculatePricing(subtotal, existing.PricingMode, existing.PricingRate)
	if err != nil {
		return Estimate{}, err
	}

	now := time.Now()
	updated := existing
	updated.Currency = subtotal.Currency
	updated.Lines = lines
	updated.CostSubtotal = subtotal
	updated.ExcludedCostItemCount = missingCount
	updated.ProposedSellingPrice = pricing.ProposedSellingPrice
	updated.ProjectedGrossProfit = pricing.ProjectedGrossProfit
	updated.ProjectedGrossMarginBPS = pricing.ProjectedGrossMarginBPS
	updated.RefreshedAt = &now

	return s.repo.ReplaceSnapshot(ctx, companyID, estimateID, expectedRevision, updated)
}

// FinalizeEstimate transitions a draft to finalized, one-directional,
// locking every field. Revision-guarded like refresh/pricing recalculation
// — finalize is the single most consequential draft mutation and needs
// this guard more than any other: a stale expectedRevision against a
// still-draft Estimate is rejected (ErrRevisionMismatch), leaving the
// document unchanged. Idempotent on retry: if the Estimate is ALREADY
// finalized, returns it unchanged regardless of the supplied
// expectedRevision, since a retry cannot know the frozen post-finalize
// Revision value (design spec §16.3).
func (s *Service) FinalizeEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (Estimate, error) {
	existing, err := s.repo.FindByID(ctx, companyID, estimateID)
	if err != nil {
		return Estimate{}, err
	}
	if existing.Status == EstimateStatusFinalized {
		return existing, nil
	}

	return s.repo.Finalize(ctx, companyID, estimateID, expectedRevision, time.Now())
}

// CreateNewVersion creates the next Version as a new draft from
// sourceEstimateID's Project. REQUIRES the source Estimate to already be
// Status=finalized (design spec §17 step 2) — rejected immediately with
// ErrEstimateMustBeFinalizedBeforeNewVersion if the source is still a
// draft, before any snapshot-building or version-number allocation is
// attempted. pricingMode/pricingRate default to the source Estimate's own
// values if nil (carrying forward the contractor's last pricing choice).
func (s *Service) CreateNewVersion(ctx context.Context, companyID, sourceEstimateID string, pricingMode *PricingMode, pricingRate *money.RateBPS) (Estimate, error) {
	source, err := s.repo.FindByID(ctx, companyID, sourceEstimateID)
	if err != nil {
		return Estimate{}, err
	}
	if source.Status != EstimateStatusFinalized {
		return Estimate{}, ErrEstimateMustBeFinalizedBeforeNewVersion
	}

	mode := source.PricingMode
	if pricingMode != nil {
		mode = *pricingMode
	}
	rate := source.PricingRate
	if pricingRate != nil {
		rate = *pricingRate
	}

	lines, subtotal, missingCount, err := s.buildSnapshot(ctx, companyID, source.ProjectID)
	if err != nil {
		return Estimate{}, err
	}

	pricing, err := CalculatePricing(subtotal, mode, rate)
	if err != nil {
		return Estimate{}, err
	}

	// Uses the SAME production allocateAndCreate helper CreateEstimate
	// uses — the bounded-retry version-number allocation strategy (design
	// spec §21.2) must live in one place, not be duplicated here as a
	// second copy that could drift from the first.
	return s.allocateAndCreate(ctx, companyID, source.ProjectID, func(version int) Estimate {
		return Estimate{
			CompanyID: companyID, ProjectID: source.ProjectID, Version: version, Status: EstimateStatusDraft, Revision: 0,
			Currency: subtotal.Currency, Lines: lines, CostSubtotal: subtotal, ExcludedCostItemCount: missingCount,
			PricingMode: mode, PricingRate: rate,
			ProposedSellingPrice: pricing.ProposedSellingPrice, ProjectedGrossProfit: pricing.ProjectedGrossProfit,
			ProjectedGrossMarginBPS: pricing.ProjectedGrossMarginBPS,
			CreatedAt:               time.Now(), SchemaVersion: 1,
		}
	})
}

// VisitQuotationSeeds validates that estimateID belongs to companyID AND
// projectID AND is Status=finalized, then internally groups this
// Estimate's Lines by WorkItemID, computes each group's share of
// CostSubtotal, and allocates ProposedSellingPrice across groups
// proportionally with largest-remainder rounding (design spec §6.2).
// Invokes visit once per resulting group — workItemID is nil for the one
// group covering CostItems with no WorkItem, if any exist (design spec
// §5.4).
//
// Eligibility is reported via four plain booleans (found, projectMatches,
// finalized, allocationEligible), never via a shared error value — a
// quotations-defined sentinel cannot be referenced here without an import
// cycle, since quotations already imports estimates for this capability
// (design spec §22). All four booleans are populated only when err==nil;
// visit is invoked ONLY when all four are true.
//
// NEITHER SnapshottedAmount NOR CostSubtotal NOR any intermediate
// cost-weight value crosses this method's return values or the visit
// callback's parameters at any point (design spec §4) — the callback
// signature has no parameter capable of holding one.
func (s *Service) VisitQuotationSeeds(
	ctx context.Context,
	companyID, projectID, estimateID string,
	visit func(workItemID *string, allocatedSellingAmount money.Money) error,
) (
	proposedSellingPrice money.Money,
	currency string,
	found bool,
	projectMatches bool,
	finalized bool,
	allocationEligible bool,
	err error,
) {
	est, findErr := s.repo.FindByID(ctx, companyID, estimateID)
	if findErr == ErrEstimateNotFound {
		return money.Money{}, "", false, false, false, false, nil
	}
	if findErr != nil {
		return money.Money{}, "", false, false, false, false, findErr
	}
	if est.ProjectID != projectID {
		return money.Money{}, "", true, false, false, false, nil
	}
	if est.Status != EstimateStatusFinalized {
		return money.Money{}, "", true, true, false, false, nil
	}

	// Group Lines by WorkItemID. A nil WorkItemID forms its own group
	// (design spec §5.4) — represented by the map key "__none__" internally,
	// which cannot collide with a real WorkItemID (always a Mongo ObjectID
	// hex string).
	type group struct {
		workItemID *string
		weight     int64
	}
	var order []string
	groups := map[string]*group{}
	for _, line := range est.Lines {
		key := "__none__"
		var wid *string
		if line.WorkItemID != nil {
			key = *line.WorkItemID
			w := *line.WorkItemID
			wid = &w
		}
		g, exists := groups[key]
		if !exists {
			g = &group{workItemID: wid, weight: 0}
			groups[key] = g
			order = append(order, key)
		}
		g.weight += line.SnapshottedAmount.Amount
	}

	weights := make([]int64, len(order))
	for i, key := range order {
		w := groups[key].weight
		if w <= 0 {
			// A non-positive WorkItem cost-group weight — report via
			// allocationEligible=false, never a returned error (design
			// spec §6.2).
			return money.Money{}, "", true, true, true, false, nil
		}
		weights[i] = w
	}

	shares, allocErr := money.AllocateProportionally(est.ProposedSellingPrice, weights)
	if allocErr != nil {
		// Every documented AllocateProportionally error (§19) represents
		// an ineligible cost basis in this context — report via
		// allocationEligible=false, never propagate money's own error
		// value (which would itself be a cross-package leak of a
		// different kind).
		return money.Money{}, "", true, true, true, false, nil
	}

	for i, key := range order {
		if visitErr := visit(groups[key].workItemID, shares[i]); visitErr != nil {
			return money.Money{}, "", false, false, false, false, visitErr
		}
	}

	return est.ProposedSellingPrice, est.Currency, true, true, true, true, nil
}
