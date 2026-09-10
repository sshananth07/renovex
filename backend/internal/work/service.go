package work

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/optional"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrDescriptionRequired is returned when CreateWorkItem is given an empty description.
var ErrDescriptionRequired = errors.New("work: description is required")

// WorkItemSortFields is the sort allowlist for GET /work-items.
var WorkItemSortFields = []string{"createdAt", "status", "description"}

const (
	WorkItemDefaultSort  = "createdAt"
	WorkItemDefaultOrder = pagination.OrderAsc
)

// ErrInvalidQuantity is returned when CreateWorkItem's quantity fails to parse, is
// not strictly positive, or has an empty unit. quantity.New alone only guarantees
// a parseable decimal, not a meaningful positive quantity with a real unit
// (design spec §1.5).
var ErrInvalidQuantity = errors.New("work: quantity must be a positive number with a non-empty unit")

// ErrProjectNotFound is returned when the given projectID does not belong to the
// caller's company. A distinct sentinel value in this package, not an import of
// projects.ErrProjectNotFound.
var ErrProjectNotFound = errors.New("work: project not found")

// ErrSpaceNotFound is returned when a given spaceID does not belong to the
// caller's company, OR belongs to the company but not to the given projectID
// (the lineage-mismatch case, design spec §10.3) — both cases collapse to the
// same sentinel/404, matching the "foreign real ID behaves like nonexistent ID"
// rule applied one level deeper.
var ErrSpaceNotFound = errors.New("work: space not found")

// ErrInvalidStatus is returned when UpdateWorkItemStatus is given a status
// outside the 2 defined WorkItemStatus values.
var ErrInvalidStatus = errors.New("work: invalid work item status")

// ErrCancelledIsTerminal is returned when UpdateWorkItemStatus is asked to
// transition a WorkItem away from WorkItemStatusCancelled — cancelled is
// terminal in M2, one-directional only (design spec §5.5).
var ErrCancelledIsTerminal = errors.New("work: cancelled work items cannot change status")

// ProjectLookup is the capability work needs from projects: confirming a
// projectID belongs to the caller's company.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// SpaceLookup is the capability work needs from spaces: a company-ownership-only
// check for ListWorkItemsBySpace (no second parent ID to cross-check), and a
// combined ownership+lineage check for CreateWorkItem (design spec §8.4).
type SpaceLookup interface {
	SpaceBelongsToCompany(ctx context.Context, companyID, spaceID string) (bool, error)
	SpaceBelongsToProject(ctx context.Context, companyID, spaceID, projectID string) (bool, error)
}

// Service implements WorkItem CRUD and parent validation against both Project
// and (optionally) Space.
type Service struct {
	repo          WorkItemRepository
	projectLookup ProjectLookup
	spaceLookup   SpaceLookup
}

// NewService constructs a Service backed by repo, consuming projectLookup and
// spaceLookup to validate parent references.
func NewService(repo WorkItemRepository, projectLookup ProjectLookup, spaceLookup SpaceLookup) *Service {
	return &Service{repo: repo, projectLookup: projectLookup, spaceLookup: spaceLookup}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public WorkItemRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every WorkItem owned by
// companyID. Development-tool use only (demoseed reset, design spec §6.6).
// Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("work: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateWorkItem validates projectID belongs to companyID; if spaceID is
// non-nil, validates it belongs to companyID AND that its own ProjectID equals
// projectID (one compound-filtered capability call — design spec §10.3);
// validates description is non-empty and quantityValue+unit form a valid
// positive Quantity; then persists with Status=planned, Source=manual,
// VerificationStatus=confirmed, all set server-side.
func (s *Service) CreateWorkItem(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit string) (WorkItem, error) {
	if description == "" {
		return WorkItem{}, ErrDescriptionRequired
	}

	q, err := quantity.New(quantityValue, unit)
	if err != nil {
		return WorkItem{}, ErrInvalidQuantity
	}
	if !q.Value.IsPositive() || q.Unit == "" {
		return WorkItem{}, ErrInvalidQuantity
	}

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return WorkItem{}, err
	}
	if !belongs {
		return WorkItem{}, ErrProjectNotFound
	}

	if spaceID != nil {
		spaceBelongs, err := s.spaceLookup.SpaceBelongsToProject(ctx, companyID, *spaceID, projectID)
		if err != nil {
			return WorkItem{}, err
		}
		if !spaceBelongs {
			return WorkItem{}, ErrSpaceNotFound
		}
	}

	return s.repo.Create(ctx, WorkItem{
		CompanyID: companyID, ProjectID: projectID, SpaceID: spaceID,
		Description: description, WorkType: workType, Quantity: q,
		Status: WorkItemStatusPlanned, Source: WorkItemSourceManual,
		VerificationStatus: VerificationStatusConfirmed, CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetWorkItem returns workItemID's WorkItem, tenant-scoped to companyID.
func (s *Service) GetWorkItem(ctx context.Context, companyID, workItemID string) (WorkItem, error) {
	return s.repo.FindByID(ctx, companyID, workItemID)
}

// ListWorkItemsPaginatedByProject validates projectID belongs to companyID
// before listing.
func (s *Service) ListWorkItemsPaginatedByProject(ctx context.Context, companyID, projectID string, req pagination.Request) ([]WorkItem, int, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, 0, err
	}
	if !belongs {
		return nil, 0, ErrProjectNotFound
	}
	return s.repo.ListPaginated(ctx, companyID, projectID, "", req)
}

// ListWorkItemsPaginatedBySpace validates spaceID belongs to companyID
// (company-ownership only — no second parent ID to cross-check here) before
// listing.
func (s *Service) ListWorkItemsPaginatedBySpace(ctx context.Context, companyID, spaceID string, req pagination.Request) ([]WorkItem, int, error) {
	belongs, err := s.spaceLookup.SpaceBelongsToCompany(ctx, companyID, spaceID)
	if err != nil {
		return nil, 0, err
	}
	if !belongs {
		return nil, 0, ErrSpaceNotFound
	}
	return s.repo.ListPaginated(ctx, companyID, "", spaceID, req)
}

// UpdateWorkItemStatus validates status is one of the 2 defined values, rejects
// any transition away from a cancelled WorkItem (cancelled is terminal), then
// updates workItemID's status, tenant-scoped to companyID.
func (s *Service) UpdateWorkItemStatus(ctx context.Context, companyID, workItemID string, status WorkItemStatus) (WorkItem, error) {
	if !status.IsValid() {
		return WorkItem{}, ErrInvalidStatus
	}

	existing, err := s.repo.FindByID(ctx, companyID, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if existing.Status == WorkItemStatusCancelled {
		return WorkItem{}, ErrCancelledIsTerminal
	}

	return s.repo.UpdateStatus(ctx, companyID, workItemID, status)
}

// WorkItemPatch carries a genuine partial update for UpdateWorkItem. There
// is deliberately no ProjectID field — Project is immutable after creation,
// and a caller cannot even express changing it through this type. Nil
// scalar fields mean "omitted, leave unchanged"; a non-nil pointer
// (including one pointing to "") means "the caller supplied this value,
// apply it exactly." SpaceID uses optional.NullableString instead of
// *string because it needs a genuine third state: Present=false (omitted,
// preserve current assignment), Present=true with Value=nil (explicit
// JSON null — clear the assignment), and Present=true with a non-nil Value
// (assign to that Space) — a plain *string cannot distinguish "omitted"
// from "explicit null".
type WorkItemPatch struct {
	Description   *string
	WorkType      *string
	QuantityValue *string
	QuantityUnit  *string
	SpaceID       optional.NullableString
}

// NullableStringPresent builds an optional.NullableString representing an
// explicitly supplied value — used by callers (tests, and any future
// non-HTTP caller) that don't go through JSON unmarshaling.
func NullableStringPresent(value string) optional.NullableString {
	return optional.NullableString{Present: true, Value: &value}
}

// NullableStringNull builds an optional.NullableString representing an
// explicit JSON null.
func NullableStringNull() optional.NullableString {
	return optional.NullableString{Present: true, Value: nil}
}

// UpdateWorkItem applies patch to workItemID's fields, tenant-scoped to
// companyID. All validation — description non-empty when supplied, quantity
// value/unit supplied together and forming a valid positive Quantity, and
// SpaceID (if present) belonging to companyID and to the WorkItem's own
// ProjectID — happens before any repository write, so a rejected patch
// never partially persists. A cancelled WorkItem cannot be edited at all
// (ErrCancelledIsTerminal, mirroring UpdateWorkItemStatus's terminal rule).
func (s *Service) UpdateWorkItem(ctx context.Context, companyID, workItemID string, patch WorkItemPatch) (WorkItem, error) {
	existing, err := s.repo.FindByID(ctx, companyID, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if existing.Status == WorkItemStatusCancelled {
		return WorkItem{}, ErrCancelledIsTerminal
	}

	if patch.Description != nil && *patch.Description == "" {
		return WorkItem{}, ErrDescriptionRequired
	}

	var newQuantity *quantity.Quantity
	if patch.QuantityValue != nil || patch.QuantityUnit != nil {
		if patch.QuantityValue == nil || patch.QuantityUnit == nil {
			return WorkItem{}, ErrInvalidQuantity
		}
		q, err := quantity.New(*patch.QuantityValue, *patch.QuantityUnit)
		if err != nil {
			return WorkItem{}, ErrInvalidQuantity
		}
		if !q.Value.IsPositive() || q.Unit == "" {
			return WorkItem{}, ErrInvalidQuantity
		}
		newQuantity = &q
	}

	if patch.SpaceID.Present && patch.SpaceID.Value != nil {
		spaceBelongs, err := s.spaceLookup.SpaceBelongsToProject(ctx, companyID, *patch.SpaceID.Value, existing.ProjectID)
		if err != nil {
			return WorkItem{}, err
		}
		if !spaceBelongs {
			return WorkItem{}, ErrSpaceNotFound
		}
	}

	return s.repo.Update(ctx, companyID, workItemID, func(w *WorkItem) {
		if patch.Description != nil {
			w.Description = *patch.Description
		}
		if patch.WorkType != nil {
			w.WorkType = *patch.WorkType
		}
		if newQuantity != nil {
			w.Quantity = *newQuantity
		}
		if patch.SpaceID.Present {
			w.SpaceID = patch.SpaceID.Value
		}
	})
}

// CreateWorkItemFromAISuggestion is the narrow internal seam AI-suggestion
// acceptance uses to create a WorkItem (M8.5B-A design doc §10.6). It
// preserves every existing WorkItem domain invariant — description
// non-empty, quantityValue+unit forming a valid positive
// foundation/quantity.Quantity, Space (when given) belonging to companyID
// AND to projectID — because the AI suggestion itself never supplies
// authoritative quantity/unit; those are contractor-confirmed at
// acceptance and passed in here exactly like a manual CreateWorkItem call.
// sourceSuggestionID is backend-supplied provenance only. Source is always
// WorkItemSourceAISuggestion and VerificationStatus is always confirmed
// (the contractor's acceptance action IS the verification). On a retried
// acceptance for the same sourceSuggestionID, it returns the
// already-created WorkItem instead of creating a duplicate.
func (s *Service) CreateWorkItemFromAISuggestion(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit, sourceSuggestionID string) (WorkItem, error) {
	if description == "" {
		return WorkItem{}, ErrDescriptionRequired
	}

	q, err := quantity.New(quantityValue, unit)
	if err != nil {
		return WorkItem{}, ErrInvalidQuantity
	}
	if !q.Value.IsPositive() || q.Unit == "" {
		return WorkItem{}, ErrInvalidQuantity
	}

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return WorkItem{}, err
	}
	if !belongs {
		return WorkItem{}, ErrProjectNotFound
	}

	if spaceID != nil {
		spaceBelongs, err := s.spaceLookup.SpaceBelongsToProject(ctx, companyID, *spaceID, projectID)
		if err != nil {
			return WorkItem{}, err
		}
		if !spaceBelongs {
			return WorkItem{}, ErrSpaceNotFound
		}
	}

	created, err := s.repo.Create(ctx, WorkItem{
		CompanyID: companyID, ProjectID: projectID, SpaceID: spaceID,
		Description: description, WorkType: workType, Quantity: q,
		Status: WorkItemStatusPlanned, Source: WorkItemSourceAISuggestion,
		VerificationStatus: VerificationStatusConfirmed,
		SourceSuggestionID: &sourceSuggestionID,
		CreatedAt:          time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		if existing, findErr := s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID); findErr == nil {
			return existing, nil
		}
		return WorkItem{}, err
	}
	return created, nil
}

// FindWorkItemBySourceSuggestionID looks up a WorkItem by its AI-suggestion
// provenance, tenant-scoped to companyID — the acceptance idempotency
// anchor (M8.5B-A design doc §18.2).
func (s *Service) FindWorkItemBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkItem, error) {
	return s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
}

// WorkItemBelongsToProject reports whether workItemID exists, belongs to
// companyID, AND its stored ProjectID equals projectID. Added in M3 — one of
// two capabilities work exposes to another module (labour, costs), satisfying
// both modules' WorkItemLookup interface structurally (M3 design spec §9.3).
func (s *Service) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error) {
	return s.repo.BelongsToProject(ctx, companyID, workItemID, projectID)
}

// WorkItemBelongsToCompany reports whether workItemID exists and belongs to
// companyID, with no ProjectID cross-check. Used by labour/costs' list-by-
// work-item endpoints to reject a foreign workItemID with 404 rather than
// silently returning an empty list (M3 design spec §9.3, tenant invariant
// "foreign parent ID -> 404, never []").
// WorkItemProcurementContext confirms that workItemID exists, belongs to
// companyID AND to projectID, and reports whether it is cancelled — in one call.
//
// The combination matters: procurement-requirement generation must skip
// cancelled scope rather than create demand for work that will never happen, and
// it needs to tell "cancelled" apart from "no such work item" because those are
// different contextual skip reasons. WorkItemBelongsToProject returns only a
// bool and cannot express that distinction.
//
// A cancelled WorkItem is still reported found=true; only a missing item, a
// foreign company, or a project-lineage mismatch yields found=false.
//
// Satisfies materialrequirements.WorkItemLookup structurally (M7 design spec
// §1.2, §3.7). Uses only primitives — no WorkItem struct crosses the module
// boundary (ADR 0002).
func (s *Service) WorkItemProcurementContext(ctx context.Context, companyID, workItemID, projectID string) (
	cancelled bool, found bool, err error,
) {
	w, err := s.repo.FindByID(ctx, companyID, workItemID)
	if errors.Is(err, ErrWorkItemNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if w.ProjectID != projectID {
		return false, false, nil
	}
	return w.Status == WorkItemStatusCancelled, true, nil
}

func (s *Service) WorkItemBelongsToCompany(ctx context.Context, companyID, workItemID string) (bool, error) {
	_, err := s.repo.FindByID(ctx, companyID, workItemID)
	if err == ErrWorkItemNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetWorkItemDescription returns workItemID's Description, requiring it to
// belong to BOTH companyID and projectID (a company-only check is
// insufficient here — quotations.WorkItemLookup uses this to validate
// contractor-submitted WorkItem IDs that may reference any WorkItem under
// the Company, not just ones the system itself generated; a cross-project
// reference under the same Company must still be rejected, M5 design spec
// §13.1/§22). found is a separate return value, not folded into err — the
// caller (quotations), not work, decides what caller-facing error a
// "not found" maps to.
func (s *Service) GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (string, bool, error) {
	belongs, err := s.repo.BelongsToProject(ctx, companyID, workItemID, projectID)
	if err != nil {
		return "", false, err
	}
	if !belongs {
		return "", false, nil
	}
	wi, err := s.repo.FindByID(ctx, companyID, workItemID)
	if err != nil {
		return "", false, err
	}
	return wi.Description, true, nil
}
