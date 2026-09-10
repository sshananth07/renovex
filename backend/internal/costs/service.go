package costs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrLabourCategoryNotAllowed is returned when CreateCostItem is called with
// category=labour. Labour-category CostItems may only be created via
// RecordLabourCost — the ledger-entry-point invariant (design spec §1.4).
var ErrLabourCategoryNotAllowed = errors.New("costs: category=labour is not allowed via CreateCostItem; use LabourEntry")

// ErrNoLifecycleAmount is returned when none of estimated/committed/actual/paid
// is supplied at creation.
var ErrNoLifecycleAmount = errors.New("costs: at least one lifecycle amount is required")

// ErrMaterialIDRequiresMaterialCategory is returned when materialID is
// supplied but category is not "material" (design spec §1.4.1), whether at
// creation or during a category correction.
var ErrMaterialIDRequiresMaterialCategory = errors.New("costs: materialId requires category=material")

// ErrCurrencyMismatch is returned when a lifecycle amount's currency does not
// match the CostItem's own Currency.
var ErrCurrencyMismatch = errors.New("costs: currency does not match this cost item's currency")

// ErrCategoryLocked is returned when a category correction is attempted after
// Committed, Actual, or Paid has already been set (design spec §1.4.3).
var ErrCategoryLocked = errors.New("costs: category is locked once committed, actual, or paid is set")

// ErrLabourCategoryImmutable is returned when a category correction attempts
// to change a labour-category CostItem away from category=labour. Only
// RecordLabourCost may produce or retire a labour-category CostItem.
var ErrLabourCategoryImmutable = errors.New("costs: category=labour cannot be changed once set")

// ErrProjectNotFound is returned when the given projectID does not belong to
// the caller's company.
var ErrProjectNotFound = errors.New("costs: project not found")

// ErrWorkItemNotFound is returned when a given workItemID does not belong to
// the caller's company, or belongs to the company but not to the given
// projectID (lineage mismatch — both cases collapse to the same sentinel).
var ErrWorkItemNotFound = errors.New("costs: work item not found")

// ErrMaterialNotFound is returned when a given materialID does not belong to
// the caller's company.
var ErrMaterialNotFound = errors.New("costs: material not found")

// ErrInvalidCategory is returned when category is not one of the 9 defined
// values.
var ErrInvalidCategory = errors.New("costs: invalid category")

// ErrIncompleteQuantityPricing is returned when only some of
// quantityValue/quantityUnit/unitPriceAmount are supplied — all three or
// none, never a partial combination.
var ErrIncompleteQuantityPricing = errors.New("costs: quantityValue, quantityUnit, and unitPriceAmount must all be supplied together or not at all")

// ErrInvalidQuantity is returned when quantityValue/quantityUnit fail to
// parse or the parsed quantity is not strictly positive with a non-empty unit.
var ErrInvalidQuantity = errors.New("costs: quantity must be a positive number with a non-empty unit")

// ErrEstimatedMismatchesLineAmount is returned when an explicitly-supplied
// estimatedAmount does not equal quantity x unitPrice's calculated line
// amount (design spec §1.4.2) — a mismatch is rejected, not silently trusted.
var ErrEstimatedMismatchesLineAmount = errors.New("costs: estimatedAmount does not match quantity x unitPrice")

// ErrActualMustUseRecordOrCorrect is returned when the general-purpose
// lifecycle route is used to write stage=actual. Actual can only be set via
// RecordCostItemActual (first write) or CorrectCostItemActual (subsequent
// corrections, which preserve the prior value) — never a blind overwrite.
var ErrActualMustUseRecordOrCorrect = errors.New("costs: actual can only be set via RecordCostItemActual or CorrectCostItemActual")

// ErrActualAlreadyRecorded is returned when RecordCostItemActual is called
// on a CostItem that already has a non-nil Actual — use CorrectCostItemActual
// instead, which preserves the previous value.
var ErrActualAlreadyRecorded = errors.New("costs: actual is already recorded; use Correct to change it")

// ErrNoActualToCorrect is returned when CorrectCostItemActual is called on a
// CostItem whose Actual has never been set — use RecordCostItemActual for
// the first entry, since there is nothing to preserve yet.
var ErrNoActualToCorrect = errors.New("costs: no actual value has been recorded yet; use Record instead of Correct")

// ErrCorrectionReasonRequired is returned when CorrectCostItemActual is
// called with a blank reason — every correction must state why.
var ErrCorrectionReasonRequired = errors.New("costs: a reason is required to correct a previously recorded actual amount")

// ProjectLookup is the capability costs needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// WorkItemLookup is the capability costs needs from work: confirming a
// workItemID belongs to both companyID and projectID in one compound check
// (used by CreateCostItem), and a company-ownership-only check (used by
// ListCostItemsByWorkItem, which has no second parent ID to cross-check —
// design spec §9.3).
type WorkItemLookup interface {
	WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
	WorkItemBelongsToCompany(ctx context.Context, companyID, workItemID string) (bool, error)
}

// MaterialLookup is the capability costs needs from materials (design spec §9.2).
type MaterialLookup interface {
	MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error)
	GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error)
}

// Service implements CostItem CRUD, the ledger-entry-point invariant, and
// exposes LabourCostRecorder, consumed by labour.
type Service struct {
	repo           CostItemRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	materialLookup MaterialLookup
}

// NewService constructs a Service backed by repo, consuming projectLookup,
// workItemLookup, and materialLookup to validate parent references.
func NewService(repo CostItemRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, materialLookup MaterialLookup) *Service {
	return &Service{repo: repo, projectLookup: projectLookup, workItemLookup: workItemLookup, materialLookup: materialLookup}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public CostItemRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every CostItem owned by
// companyID. Development-tool use only (demoseed reset, design spec §6.6).
// Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("costs: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateCostItem validates projectID/workItemID/materialID parent references,
// rejects category=labour (the ledger-entry-point invariant), enforces
// materialID⟹category=material, requires at least one lifecycle amount, and
// — when quantity+unitPrice are both supplied — establishes/validates
// Estimated only (never Committed/Actual/Paid — design spec §1.4.2).
func (s *Service) CreateCostItem(ctx context.Context, companyID, projectID string, workItemID *string, category CostCategory,
	description string, quantityValue, quantityUnit *string, unitPriceAmount, estimatedAmount, committedAmount, actualAmount, paidAmount *int64,
	currency string, materialID *string, date time.Time, notes string) (CostItem, error) {

	if category == CostCategoryLabour {
		return CostItem{}, ErrLabourCategoryNotAllowed
	}
	if !category.IsValid() {
		return CostItem{}, ErrInvalidCategory
	}
	if materialID != nil && category != CostCategoryMaterial {
		return CostItem{}, ErrMaterialIDRequiresMaterialCategory
	}

	// Quantity + Unit + UnitPrice must be supplied all together or not at
	// all — a partial combination (e.g. quantity/unit given but unitPrice
	// omitted) is rejected rather than silently ignored.
	quantitySupplied := quantityValue != nil || quantityUnit != nil || unitPriceAmount != nil
	quantityComplete := quantityValue != nil && quantityUnit != nil && unitPriceAmount != nil
	if quantitySupplied && !quantityComplete {
		return CostItem{}, ErrIncompleteQuantityPricing
	}

	var q *quantity.Quantity
	var unitPrice *money.Money
	if quantityComplete {
		parsed, err := quantity.New(*quantityValue, *quantityUnit)
		if err != nil {
			return CostItem{}, ErrInvalidQuantity
		}
		if !parsed.Value.IsPositive() || parsed.Unit == "" {
			return CostItem{}, ErrInvalidQuantity
		}
		q = &parsed
		up := money.New(*unitPriceAmount, currency)
		unitPrice = &up

		lineAmount := money.CalculateLineAmount(parsed.Value, up)
		if estimatedAmount == nil {
			derived := lineAmount.Amount
			estimatedAmount = &derived
		} else if *estimatedAmount != lineAmount.Amount {
			// An explicitly-supplied Estimated must match the calculated line
			// amount — a mismatch is rejected rather than silently trusted,
			// per the approved spec (design spec §1.4.2).
			return CostItem{}, ErrEstimatedMismatchesLineAmount
		}
	}

	if estimatedAmount == nil && committedAmount == nil && actualAmount == nil && paidAmount == nil {
		return CostItem{}, ErrNoLifecycleAmount
	}

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return CostItem{}, err
	}
	if !belongs {
		return CostItem{}, ErrProjectNotFound
	}

	if workItemID != nil {
		wiBelongs, err := s.workItemLookup.WorkItemBelongsToProject(ctx, companyID, *workItemID, projectID)
		if err != nil {
			return CostItem{}, err
		}
		if !wiBelongs {
			return CostItem{}, ErrWorkItemNotFound
		}
	}

	if materialID != nil {
		mBelongs, err := s.materialLookup.MaterialBelongsToCompany(ctx, companyID, *materialID)
		if err != nil {
			return CostItem{}, err
		}
		if !mBelongs {
			return CostItem{}, ErrMaterialNotFound
		}
	}

	item := CostItem{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID, Category: category,
		Description: description, Quantity: q, UnitPrice: unitPrice, MaterialID: materialID,
		Currency: currency, Date: date, Notes: notes, CreatedAt: time.Now(), SchemaVersion: 1,
	}
	if estimatedAmount != nil {
		m := money.New(*estimatedAmount, currency)
		item.Estimated = &m
	}
	if committedAmount != nil {
		m := money.New(*committedAmount, currency)
		item.Committed = &m
	}
	if actualAmount != nil {
		m := money.New(*actualAmount, currency)
		item.Actual = &m
	}
	if paidAmount != nil {
		m := money.New(*paidAmount, currency)
		item.Paid = &m
	}

	return s.repo.Create(ctx, item)
}

// GetCostItem returns costItemID's CostItem, tenant-scoped to companyID.
func (s *Service) GetCostItem(ctx context.Context, companyID, costItemID string) (CostItem, error) {
	return s.repo.FindByID(ctx, companyID, costItemID)
}

// ListCostItemsByProject validates projectID belongs to companyID before listing.
func (s *Service) ListCostItemsByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// ListCostItemsByWorkItem validates workItemID belongs to companyID before
// listing — a foreign workItemID returns 404, never an empty list (tenant
// invariant: foreign parent ID -> 404, never []).
func (s *Service) ListCostItemsByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error) {
	belongs, err := s.workItemLookup.WorkItemBelongsToCompany(ctx, companyID, workItemID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrWorkItemNotFound
	}
	return s.repo.ListByWorkItem(ctx, companyID, workItemID)
}

// UpdateCostItemLifecycle sets one of estimated/committed/paid, validating
// the new amount's currency matches the record's own Currency. Paid is a
// cumulative running total, not a delta (design spec §1.5). stage=actual is
// rejected outright — Actual can only be set via RecordCostItemActual or
// CorrectCostItemActual, so an already-recorded Actual value can never be
// silently overwritten through this general-purpose route.
func (s *Service) UpdateCostItemLifecycle(ctx context.Context, companyID, costItemID string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error) {
	if !stage.IsValid() {
		return CostItem{}, errors.New("costs: invalid lifecycle stage")
	}
	if stage == CostStageActual {
		return CostItem{}, ErrActualMustUseRecordOrCorrect
	}
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if amount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	return s.repo.UpdateLifecycleField(ctx, companyID, costItemID, expectedRevision, stage, amount)
}

// RecordCostItemActual sets Actual for the first time. Rejected with
// ErrActualAlreadyRecorded if a value is already present — that path is
// CorrectCostItemActual, which preserves the previous value instead of
// silently overwriting it.
func (s *Service) RecordCostItemActual(ctx context.Context, companyID, actorUserID, costItemID string,
	expectedRevision int64, amount money.Money) (CostItem, error) {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if existing.Actual != nil {
		return CostItem{}, ErrActualAlreadyRecorded
	}
	if amount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	return s.repo.RecordActual(ctx, companyID, costItemID, expectedRevision, amount)
}

// CorrectCostItemActual corrects an ALREADY-RECORDED Actual to newAmount,
// requiring a non-blank reason and the caller's expectedRevision to match
// the stored CostItem. The previous value is appended to
// CostItem.ActualCorrections in the same atomic write that updates Actual
// itself (repo.CorrectActual: one UpdateOne with $set+$push, no
// transaction, no second collection) — a rejected write (stale revision,
// wrong tenant) appends nothing, since it never matches any document.
// Rejected with ErrNoActualToCorrect if Actual has never been set — that
// path is RecordCostItemActual.
func (s *Service) CorrectCostItemActual(ctx context.Context, companyID, actorUserID, costItemID string,
	expectedRevision int64, newAmount money.Money, reason string) (CostItem, error) {
	if strings.TrimSpace(reason) == "" {
		return CostItem{}, ErrCorrectionReasonRequired
	}
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if existing.Actual == nil {
		return CostItem{}, ErrNoActualToCorrect
	}
	if newAmount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	correction := ActualCorrection{
		PreviousAmount: *existing.Actual, NewAmount: newAmount,
		Reason: strings.TrimSpace(reason), CorrectedByUser: actorUserID,
		CorrectedAt: time.Now(),
	}
	return s.repo.CorrectActual(ctx, companyID, costItemID, expectedRevision, newAmount, correction)
}

// UpdateCostItemDetails updates description/notes, and optionally category —
// category correction is only allowed while Committed, Actual, and Paid are
// all still nil (design spec §1.4.3); it always re-validates
// materialID⟹category=material against the existing record's MaterialID.
// Category=labour is structurally immutable in both directions: a
// labour-category CostItem (always created via RecordLabourCost) can never be
// recategorized away from labour, and no other category can ever be changed
// into labour — only RecordLabourCost may produce a labour-category CostItem
// (design spec §1.4, §22-A).
func (s *Service) UpdateCostItemDetails(ctx context.Context, companyID, costItemID string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error) {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if category != nil && *category != existing.Category {
		if existing.Category == CostCategoryLabour {
			return CostItem{}, ErrLabourCategoryImmutable
		}
		if *category == CostCategoryLabour {
			return CostItem{}, ErrLabourCategoryNotAllowed
		}
		if existing.Committed != nil || existing.Actual != nil || existing.Paid != nil {
			return CostItem{}, ErrCategoryLocked
		}
		if existing.MaterialID != nil && *category != CostCategoryMaterial {
			return CostItem{}, ErrMaterialIDRequiresMaterialCategory
		}
		if !category.IsValid() {
			return CostItem{}, ErrInvalidCategory
		}
	}
	return s.repo.UpdateDetails(ctx, companyID, costItemID, expectedRevision, description, notes, category)
}

// RecordLabourCost creates a CostItem with Category=labour on behalf of a
// LabourEntry. Satisfies labour.LabourCostRecorder structurally (design spec
// §9.4). This is the ONLY code path that may produce a labour-category
// CostItem — CreateCostItem structurally rejects it.
func (s *Service) RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (string, error) {
	item := CostItem{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: &workItemID, Category: CostCategoryLabour,
		Description: "Labour cost", Estimated: &estimated, Currency: estimated.Currency,
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	created, err := s.repo.Create(ctx, item)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// UpdateLabourCostEstimate updates only the Estimated field of an existing
// labour CostItem — backs LabourEntry correction. Never touches
// Committed/Actual/Paid (design spec §1.3.2, §9.4). costs.Service owns
// CostItem's revision/CAS mechanics itself: labour (the caller, via the
// LabourCostRecorder interface) never sees or supplies a revision — this
// method reads the current revision internally and retries once on a
// concurrent-write conflict, so its public signature stays unchanged.
func (s *Service) UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return err
	}
	_, err = s.repo.UpdateLifecycleField(ctx, companyID, costItemID, existing.Revision, CostStageEstimated, estimated)
	if errors.Is(err, ErrRevisionMismatch) {
		existing, err = s.repo.FindByID(ctx, companyID, costItemID)
		if err != nil {
			return err
		}
		_, err = s.repo.UpdateLifecycleField(ctx, companyID, costItemID, existing.Revision, CostStageEstimated, estimated)
	}
	return err
}

// DeleteProvisionedLabourCost is best-effort compensation: deletes a
// just-created labour CostItem if the paired LabourEntry write subsequently
// fails (design spec §22-B).
func (s *Service) DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error {
	return s.repo.Delete(ctx, companyID, costItemID)
}

// VisitEligibleMaterialCostItems iterates every CostItem under projectID that
// is eligible to prefill a procurement Material Requirement, invoking visit once
// per eligible row. A row is eligible only when ALL of these hold (M7 design
// spec §3.1):
//
//  1. Category == material
//  2. MaterialID != nil
//  3. WorkItemID != nil
//  4. Quantity != nil and Quantity.Value is strictly positive
//  5. Quantity.Unit is non-blank after trimming
//
// Eligibility lives HERE, in the module that owns CostItem, so the rules exist
// in exactly one place and no consumer can reinterpret a partially-populated
// record. Consequently workItemID and materialID are non-pointer in the visit
// signature: a row lacking either never reaches the consumer.
//
// The visitor NEVER aggregates. It exposes eligible source rows only;
// internal/materialrequirements owns aggregation, fingerprints and discrepancy
// logic.
//
// The five returned counters tally rows filtered out here, which the consumer
// therefore never sees, so it can still report what costing data did not
// qualify. They are PRIMITIVES rather than a named struct on purpose: a
// consumer-owned return type would force this package to import the consumer to
// satisfy its interface, inverting the dependency direction ADR 0002 mandates
// (M7 design spec §1.2). This mirrors VisitEstimatedCostItems returning a bare
// missingCount.
//
// A visitor error aborts the walk and propagates unchanged.
func (s *Service) VisitEligibleMaterialCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID string, materialID string, quantity decimal.Decimal, quantityUnit string) error,
) (
	nonMaterialCategory int,
	missingMaterialID int,
	missingWorkItemID int,
	missingOrZeroQuantity int,
	blankUnit int,
	err error,
) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	if !belongs {
		return 0, 0, 0, 0, 0, ErrProjectNotFound
	}

	items, err := s.repo.ListByProject(ctx, companyID, projectID)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	for _, item := range items {
		// Each ineligible row is counted under exactly ONE reason — the first
		// that applies, in the order the eligibility rules are stated — so the
		// counters sum to the number of ineligible rows and never double-count.
		switch {
		case item.Category != CostCategoryMaterial:
			nonMaterialCategory++
		case item.MaterialID == nil:
			missingMaterialID++
		case item.WorkItemID == nil:
			missingWorkItemID++
		case item.Quantity == nil || !item.Quantity.Value.IsPositive():
			missingOrZeroQuantity++
		case strings.TrimSpace(item.Quantity.Unit) == "":
			blankUnit++
		default:
			if err := visit(item.ID, *item.WorkItemID, *item.MaterialID,
				item.Quantity.Value, item.Quantity.Unit); err != nil {
				return 0, 0, 0, 0, 0, err
			}
		}
	}
	return nonMaterialCategory, missingMaterialID, missingWorkItemID, missingOrZeroQuantity, blankUnit, nil
}

// VisitEstimatedCostItems iterates every CostItem under projectID that has
// a non-nil Estimated value, invoking visit once per CostItem in the order
// returned by ListByProject. Returns the count of CostItems under this
// Project that have NO Estimated value set, so the caller (internal/estimates)
// can apply its own missing-Estimated policy without a second round-trip.
// Satisfies estimates.EstimatedCostSource structurally (M4 design spec §6).
// Uses only primitives and money.Money — no CostItem struct crosses the
// module boundary (ADR 0002).
func (s *Service) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	items, err := s.repo.ListByProject(ctx, companyID, projectID)
	if err != nil {
		return 0, err
	}
	missingCount := 0
	for _, item := range items {
		if item.Estimated == nil {
			missingCount++
			continue
		}
		if err := visit(item.ID, item.WorkItemID, string(item.Category), item.Description, *item.Estimated); err != nil {
			return 0, err
		}
	}
	return missingCount, nil
}
