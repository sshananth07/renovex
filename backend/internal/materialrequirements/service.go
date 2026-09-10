package materialrequirements

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ProjectLookup is the capability materialrequirements needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// WorkItemLookup confirms lineage AND exposes cancellation in one call, so
// generation can skip cancelled scope and manual creation can refuse it
// (design spec §1.2).
type WorkItemLookup interface {
	WorkItemProcurementContext(ctx context.Context, companyID, workItemID, projectID string) (
		cancelled bool, found bool, err error)
}

// MaterialLookup returns catalog descriptive fields. It deliberately exposes no
// active/status value: materials.Material has no such field and M3 is frozen
// (design spec §0.3 conflict D).
type MaterialLookup interface {
	GetMaterialReference(ctx context.Context, companyID, materialID string) (
		name string, catalogUnit string, specification string, found bool, err error)
}

// AuditRecorder is the consumer-owned audit contract for this module — eight
// methods, no more. audit.Service satisfies it structurally; there is no shared
// cross-module audit interface (design spec §1.5).
type AuditRecorder interface {
	RecordMaterialRequirementsGenerated(ctx context.Context, companyID, projectID, actorUserID string,
		createdCount, unchangedCount, discrepancyCount, sourceRemovedCount, skippedCount int) error
	RecordMaterialRequirementCreated(ctx context.Context, companyID, projectID, actorUserID,
		requirementID, materialID, sourceType string) error
	RecordMaterialRequirementUpdated(ctx context.Context, companyID, projectID, actorUserID,
		requirementID string, reviewReset bool) error
	RecordMaterialRequirementReviewed(ctx context.Context, companyID, projectID, actorUserID,
		requirementID, materialID string) error
	RecordMaterialRequirementUnitAcknowledged(ctx context.Context, companyID, projectID, actorUserID,
		requirementID, procurementUnit, catalogUnit string) error
	RecordMaterialRequirementDiscrepancyResolved(ctx context.Context, companyID, projectID, actorUserID,
		requirementID, action, syncStateBefore, anchorStatus string) error
	RecordMaterialRequirementSplit(ctx context.Context, companyID, projectID, actorUserID,
		sourceRequirementID, splitGroupID string, childCount int) error
	RecordMaterialRequirementArchived(ctx context.Context, companyID, projectID, actorUserID,
		requirementID string) error
	RecordMaterialRequirementDeleted(ctx context.Context, companyID, projectID, actorUserID,
		requirementID string) error
}

// MaterialCostSource yields eligible material CostItem rows. The visitor never
// aggregates — this module owns aggregation, fingerprints and discrepancy logic.
//
// The five ineligible counters are primitives, not a named struct, so
// costs.Service satisfies this interface directly without importing this
// package (design spec §1.2).
type MaterialCostSource interface {
	VisitEligibleMaterialCostItems(ctx context.Context, companyID, projectID string,
		visit func(costItemID string, workItemID string, materialID string,
			quantity decimal.Decimal, quantityUnit string) error,
	) (nonMaterialCategory int, missingMaterialID int, missingWorkItemID int,
		missingOrZeroQuantity int, blankUnit int, err error)
}

// Service implements the Material Requirement workflows.
type Service struct {
	repo           MaterialRequirementRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	materialLookup MaterialLookup
	costSource     MaterialCostSource
	audit          AuditRecorder
}

// NewService constructs a Service from its repository and the five capabilities
// it consumes.
func NewService(repo MaterialRequirementRepository, projectLookup ProjectLookup,
	workItemLookup WorkItemLookup, materialLookup MaterialLookup,
	costSource MaterialCostSource, audit AuditRecorder) *Service {
	return &Service{
		repo: repo, projectLookup: projectLookup, workItemLookup: workItemLookup,
		materialLookup: materialLookup, costSource: costSource, audit: audit,
	}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public MaterialRequirementRepository interface. Only the
// real Mongo repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every MaterialRequirement owned
// by companyID. Development-tool use only (demoseed reset, design spec
// §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("materialrequirements: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateManualInput carries a manual creation request. WorkItemID is optional:
// nil means project-level procurement demand not tied to any WorkItem (design
// spec §2.2).
type CreateManualInput struct {
	ProjectID        string
	WorkItemID       *string
	MaterialID       string
	QuantityValue    string
	QuantityUnit     string
	Specification    *string
	RequiredByDate   *time.Time
	ProcurementNotes string
	InternalNotes    string
}

// UpdateRequirementInput is a sparse edit: a nil field means "leave unchanged",
// which is what lets §5.4 distinguish an InternalNotes-only edit from a
// procurement-relevant one. RequiredByDate and WorkItemID are double pointers so
// a caller can distinguish "unchanged" from "clear it".
type UpdateRequirementInput struct {
	MaterialID       *string
	WorkItemID       **string
	QuantityValue    *string
	QuantityUnit     *string
	Specification    *string
	RequiredByDate   **time.Time
	ProcurementNotes *string
	InternalNotes    *string
}

// CreateManualRequirement creates a contractor-authored requirement at status
// draft. Review is the confirmation gate, so nothing created here is
// RFQ-eligible until the contractor reviews it (design spec §3.6).
func (s *Service) CreateManualRequirement(ctx context.Context, companyID, actorUserID string,
	input CreateManualInput) (MaterialRequirement, error) {

	q, err := parseQuantity(input.QuantityValue, input.QuantityUnit)
	if err != nil {
		return MaterialRequirement{}, err
	}

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, input.ProjectID)
	if err != nil {
		return MaterialRequirement{}, err
	}
	if !belongs {
		return MaterialRequirement{}, ErrProjectNotFound
	}

	if input.WorkItemID != nil {
		if err := s.validateWorkItem(ctx, companyID, *input.WorkItemID, input.ProjectID); err != nil {
			return MaterialRequirement{}, err
		}
	}

	name, catalogUnit, catalogSpec, found, err := s.materialLookup.GetMaterialReference(ctx, companyID, input.MaterialID)
	if err != nil {
		return MaterialRequirement{}, err
	}
	if !found {
		return MaterialRequirement{}, ErrMaterialNotFound
	}

	specification := catalogSpec
	if input.Specification != nil {
		specification = *input.Specification
	}

	now := time.Now()
	req := MaterialRequirement{
		CompanyID: companyID, ProjectID: input.ProjectID, WorkItemID: input.WorkItemID,
		MaterialID: input.MaterialID, MaterialName: name, Specification: specification,
		RequiredQuantity: q, CatalogUnit: catalogUnit,
		UnitMismatch:     UnitsMismatch(q.Unit, catalogUnit),
		RequiredByDate:   input.RequiredByDate,
		ProcurementNotes: input.ProcurementNotes, InternalNotes: input.InternalNotes,
		Status:     RequirementStatusDraft,
		SourceType: SourceTypeManual,
		// A manual requirement has no CostItem aggregate, so it is permanently
		// clean and never produces a discrepancy (design spec §8.5).
		SourceSyncState: SourceSyncStateClean,
		CreatedByUserID: actorUserID,
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}

	created, err := s.repo.Create(ctx, req)
	if err != nil {
		return MaterialRequirement{}, err
	}

	// Audit is best-effort: a failure here never fails the business operation
	// (the convention established in internal/audit).
	_ = s.audit.RecordMaterialRequirementCreated(ctx, companyID, created.ProjectID, actorUserID,
		created.ID, created.MaterialID, string(created.SourceType))
	return created, nil
}

// GetRequirement returns one requirement, tenant-scoped.
func (s *Service) GetRequirement(ctx context.Context, companyID, requirementID string) (MaterialRequirement, error) {
	return s.repo.FindByID(ctx, companyID, requirementID)
}

// ListRequirementsByProject validates the project belongs to the company before
// listing, so a foreign projectID yields 404 rather than an empty list.
func (s *Service) ListRequirementsByProject(ctx context.Context, companyID, projectID string) ([]MaterialRequirement, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// RequirementFilter narrows a project listing. An empty field means "no filter",
// so the zero value lists everything in the project (design spec §13.1).
type RequirementFilter struct {
	WorkItemID string
	Status     string
	SyncState  string
}

// ListRequirements applies the §13.1 query filters in memory, after the
// tenant-scoped project read.
//
// Filtering here rather than in the repository keeps the tenant boundary in ONE
// place: the underlying query is already scoped by companyId AND projectId
// before any of this runs, so no filter combination can widen what a company
// sees.
//
// TECHNICAL DEBT (accepted for M7, scalability only): move the optional status
// and workItem filtering into MongoDB and add pagination when project-level
// Material Requirement volume becomes large enough to justify it. This is not a
// correctness or tenant-isolation concern — both are enforced by the scoped
// repository read above.
func (s *Service) ListRequirements(ctx context.Context, companyID, projectID string,
	filter RequirementFilter) ([]MaterialRequirement, error) {

	all, err := s.ListRequirementsByProject(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}

	out := make([]MaterialRequirement, 0, len(all))
	for _, r := range all {
		if filter.WorkItemID != "" && (r.WorkItemID == nil || *r.WorkItemID != filter.WorkItemID) {
			continue
		}
		if filter.Status != "" && string(r.Status) != filter.Status {
			continue
		}
		if filter.SyncState != "" && string(r.SourceSyncState) != filter.SyncState {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// UpdateRequirement applies a sparse contractor edit under a Revision guard.
//
// It resolves which fields actually changed into a RequirementEdit, then applies
// the two shared rules from §5.4 and §2.2 — review reset and acknowledgement
// clearing — rather than re-deriving them per field.
func (s *Service) UpdateRequirement(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64, input UpdateRequirementInput) (MaterialRequirement, error) {

	existing, err := s.loadEditable(ctx, companyID, requirementID)
	if err != nil {
		return MaterialRequirement{}, err
	}

	updated := existing
	var edit RequirementEdit

	if input.MaterialID != nil && *input.MaterialID != existing.MaterialID {
		if !existing.IdentityFieldsEditable() {
			return MaterialRequirement{}, ErrMaterialIDImmutable
		}
		name, catalogUnit, catalogSpec, found, err := s.materialLookup.GetMaterialReference(ctx, companyID, *input.MaterialID)
		if err != nil {
			return MaterialRequirement{}, err
		}
		if !found {
			return MaterialRequirement{}, ErrMaterialNotFound
		}
		// A contractor-authored specification survives a material change; a
		// still-seeded one is re-seeded from the new material (design spec §2.2).
		//
		// "Still seeded" is determined by comparing against the OLD material's
		// catalog specification, fetched here rather than inferred: the
		// requirement stores only the current value, so without this lookup there
		// is no way to tell an untouched seed from an identical contractor edit.
		_, _, oldCatalogSpec, oldFound, err := s.materialLookup.GetMaterialReference(ctx, companyID, existing.MaterialID)
		if err != nil {
			return MaterialRequirement{}, err
		}
		stillSeeded := existing.Specification == "" || (oldFound && existing.Specification == oldCatalogSpec)
		if stillSeeded {
			updated.Specification = catalogSpec
		}
		updated.MaterialID = *input.MaterialID
		updated.MaterialName = name
		updated.CatalogUnit = catalogUnit
		edit.MaterialIDChanged = true
	}

	if input.WorkItemID != nil && !samePtr(existing.WorkItemID, *input.WorkItemID) {
		if !existing.IdentityFieldsEditable() {
			return MaterialRequirement{}, ErrWorkItemIDImmutable
		}
		if *input.WorkItemID != nil {
			if err := s.validateWorkItem(ctx, companyID, **input.WorkItemID, existing.ProjectID); err != nil {
				return MaterialRequirement{}, err
			}
		}
		updated.WorkItemID = *input.WorkItemID
		edit.WorkItemIDChanged = true
	}

	if input.QuantityValue != nil || input.QuantityUnit != nil {
		value := updated.RequiredQuantity.Value.String()
		unit := updated.RequiredQuantity.Unit
		if input.QuantityValue != nil {
			value = *input.QuantityValue
		}
		if input.QuantityUnit != nil {
			unit = *input.QuantityUnit
		}
		q, err := parseQuantity(value, unit)
		if err != nil {
			return MaterialRequirement{}, err
		}
		if !q.Value.Equal(existing.RequiredQuantity.Value) {
			edit.QuantityValueChanged = true
		}
		if q.Unit != existing.RequiredQuantity.Unit {
			edit.QuantityUnitChanged = true
		}
		updated.RequiredQuantity = q
	}

	if input.Specification != nil && *input.Specification != existing.Specification {
		updated.Specification = *input.Specification
		edit.SpecificationChanged = true
	}
	if input.RequiredByDate != nil {
		updated.RequiredByDate = *input.RequiredByDate
		edit.RequiredByDateChanged = true
	}
	if input.ProcurementNotes != nil && *input.ProcurementNotes != existing.ProcurementNotes {
		updated.ProcurementNotes = *input.ProcurementNotes
		edit.ProcurementNotesChanged = true
	}
	if input.InternalNotes != nil && *input.InternalNotes != existing.InternalNotes {
		updated.InternalNotes = *input.InternalNotes
		edit.InternalNotesChanged = true
	}

	// The unit mismatch is always recomputed, because either side may have moved.
	updated.UnitMismatch = UnitsMismatch(updated.RequiredQuantity.Unit, updated.CatalogUnit)
	if edit.ClearsUnitAcknowledgement() {
		updated.UnitMismatchAcknowledged = false
	}
	if edit.ResetsReview() && updated.Status == RequirementStatusReviewed {
		updated.Status = RequirementStatusDraft
	}

	saved, err := s.repo.UpdateContractorFields(ctx, companyID, requirementID, expectedRevision, updated)
	if err != nil {
		return MaterialRequirement{}, err
	}
	_ = s.audit.RecordMaterialRequirementUpdated(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ID, edit.ResetsReview())
	return saved, nil
}

// ReviewRequirement transitions draft -> reviewed. Re-reviewing an already
// reviewed requirement succeeds as a no-op, so a retried request is not an
// error from the contractor's point of view.
func (s *Service) ReviewRequirement(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64) (MaterialRequirement, error) {

	existing, err := s.loadEditable(ctx, companyID, requirementID)
	if err != nil {
		return MaterialRequirement{}, err
	}

	updated := existing
	updated.Status = RequirementStatusReviewed
	saved, err := s.repo.UpdateContractorFields(ctx, companyID, requirementID, expectedRevision, updated)
	if err != nil {
		return MaterialRequirement{}, err
	}
	_ = s.audit.RecordMaterialRequirementReviewed(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ID, saved.MaterialID)
	return saved, nil
}

// AcknowledgeUnitMismatch records that the contractor accepts a procurement unit
// differing from the catalog unit, which relaxes the §8.5 eligibility gate.
//
// It does NOT reset review: acknowledging resolves a blocking condition rather
// than changing what a supplier would be asked to quote.
func (s *Service) AcknowledgeUnitMismatch(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64) (MaterialRequirement, error) {

	existing, err := s.loadEditable(ctx, companyID, requirementID)
	if err != nil {
		return MaterialRequirement{}, err
	}
	if !existing.UnitMismatch {
		return MaterialRequirement{}, ErrNoUnitMismatchToAcknowledge
	}

	updated := existing
	updated.UnitMismatchAcknowledged = true
	saved, err := s.repo.UpdateContractorFields(ctx, companyID, requirementID, expectedRevision, updated)
	if err != nil {
		return MaterialRequirement{}, err
	}
	_ = s.audit.RecordMaterialRequirementUnitAcknowledged(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ID, saved.RequiredQuantity.Unit, saved.CatalogUnit)
	return saved, nil
}

// ArchiveRequirement retires demand. archived is terminal and read-only, and
// distinct from split so the record says why it became inactive (design spec
// §2.1).
func (s *Service) ArchiveRequirement(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64) (MaterialRequirement, error) {

	existing, err := s.loadEditable(ctx, companyID, requirementID)
	if err != nil {
		return MaterialRequirement{}, err
	}

	updated := existing
	updated.Status = RequirementStatusArchived
	saved, err := s.repo.UpdateContractorFields(ctx, companyID, requirementID, expectedRevision, updated)
	if err != nil {
		return MaterialRequirement{}, err
	}
	_ = s.audit.RecordMaterialRequirementArchived(ctx, companyID, saved.ProjectID, actorUserID, saved.ID)
	return saved, nil
}

// DeleteRequirement permanently removes a requirement. Unlike
// ArchiveRequirement, this is irreversible — there is no archived record
// left behind. Refused under the same rules as every other contractor
// mutation (loadEditable): terminal (already archived/split) or claimed by
// an active RFQ chain.
func (s *Service) DeleteRequirement(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64) error {

	existing, err := s.loadEditable(ctx, companyID, requirementID)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, companyID, requirementID, expectedRevision); err != nil {
		return err
	}
	_ = s.audit.RecordMaterialRequirementDeleted(ctx, companyID, existing.ProjectID, actorUserID, existing.ID)
	return nil
}

// loadEditable fetches a requirement and rejects the two states in which every
// contractor operation is refused, with the specific sentinel each deserves.
//
// The claimed check reports ErrMaterialRequirementAlreadyClaimed rather than a
// generic conflict, because the contractor's actionable next step is to remove
// the RFQ line and release the claim (design spec §2.3).
func (s *Service) loadEditable(ctx context.Context, companyID, requirementID string) (MaterialRequirement, error) {
	existing, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return MaterialRequirement{}, err
	}
	if existing.IsTerminal() {
		return MaterialRequirement{}, ErrRequirementTerminal
	}
	if existing.IsClaimed() {
		return MaterialRequirement{}, ErrMaterialRequirementAlreadyClaimed
	}
	return existing, nil
}

// validateWorkItem confirms the WorkItem belongs to this company and project and
// is not cancelled.
func (s *Service) validateWorkItem(ctx context.Context, companyID, workItemID, projectID string) error {
	cancelled, found, err := s.workItemLookup.WorkItemProcurementContext(ctx, companyID, workItemID, projectID)
	if err != nil {
		return err
	}
	if !found {
		return ErrWorkItemNotFound
	}
	if cancelled {
		return ErrWorkItemCancelled
	}
	return nil
}

// parseQuantity validates a decimal string + unit into a Quantity. Quantities
// are never floats and never rounded here (ADR 0001).
func parseQuantity(value, unit string) (quantity.Quantity, error) {
	trimmedUnit := strings.TrimSpace(unit)
	if trimmedUnit == "" {
		return quantity.Quantity{}, ErrInvalidUnit
	}
	parsed, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return quantity.Quantity{}, ErrInvalidQuantity
	}
	if !parsed.IsPositive() {
		return quantity.Quantity{}, ErrInvalidQuantity
	}
	return quantity.Quantity{Value: parsed, Unit: trimmedUnit}, nil
}

// samePtr reports whether two optional string pointers denote the same value,
// treating nil as a distinct value from any string.
func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
