package workresources

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrProjectNotFound is returned when the given projectID does not belong
// to the caller's company (per ProjectLookup) — a distinct sentinel value
// in this package, not an import of projects.ErrProjectNotFound.
var ErrProjectNotFound = errors.New("workresources: project not found")

// ErrWorkItemNotFound is returned when the given workItemID does not belong
// to the caller's company AND projectID (per WorkItemLookup).
var ErrWorkItemNotFound = errors.New("workresources: work item not found")

// ErrMaterialNotFound is returned when a material resourceType's
// materialID does not belong to the caller's company (per MaterialLookup).
var ErrMaterialNotFound = errors.New("workresources: material not found")

// ErrRequirementDuplicate is returned when a DIFFERENT AI suggestion
// attempts to create a requirement matching an existing deterministic
// resource key (design doc §20.3) — distinct from a retry of the SAME
// suggestion, which instead returns the existing requirement.
var ErrRequirementDuplicate = errors.New("workresources: requirement duplicate")

// ProjectLookup is the capability workresources needs from projects:
// confirming a projectID belongs to the caller's company. Defined here
// (consumer-defines-interface); satisfied structurally with no import in
// either direction.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// WorkItemLookup is the capability workresources needs from work:
// confirming a workItemID belongs to the caller's company AND to a specific
// projectID in one compound-filtered check.
type WorkItemLookup interface {
	WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
}

// MaterialLookup is the capability workresources needs from materials:
// confirming a materialID belongs to the caller's company.
type MaterialLookup interface {
	MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error)
}

// Service implements WorkResourceRequirement creation and reads.
type Service struct {
	repo           WorkResourceRequirementRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	materialLookup MaterialLookup
}

// NewService constructs a Service backed by repo, consuming projectLookup,
// workItemLookup, and materialLookup to validate parent references. Note
// what is deliberately absent from this signature: no CostRecorder, no
// procurement-demand capability, no labour-rate capability — this service
// cannot create a CostItem or any commercial commitment even if asked to,
// because it has no capability to do so (design doc §11.2).
func NewService(repo WorkResourceRequirementRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, materialLookup MaterialLookup) *Service {
	return &Service{repo: repo, projectLookup: projectLookup, workItemLookup: workItemLookup, materialLookup: materialLookup}
}

// CreateFromAISuggestion is the narrow internal seam AI-suggestion
// acceptance uses to create a WorkResourceRequirement (design doc §12,
// §18.2, §20.3). It validates the parent Project and WorkItem lineage, and
// for resourceType=material, that materialID belongs to the caller's
// company. sourceSuggestionID is backend-supplied provenance only.
//
// Idempotency and duplicate detection:
//   - a retry of the SAME sourceSuggestionID returns the already-created
//     requirement instead of creating a duplicate;
//   - a DIFFERENT sourceSuggestionID landing on the same deterministic
//     resource key (companyId+workItemId+resourceType+materialId, or
//     +normalizedName for trade/equipment) returns ErrRequirementDuplicate
//     rather than silently creating a second requirement for the same
//     resource.
func (s *Service) CreateFromAISuggestion(ctx context.Context, companyID, projectID, workItemID string, resourceType ResourceType, materialID *string, name, sourceSuggestionID string) (WorkResourceRequirement, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return WorkResourceRequirement{}, err
	}
	if !belongs {
		return WorkResourceRequirement{}, ErrProjectNotFound
	}

	workItemBelongs, err := s.workItemLookup.WorkItemBelongsToProject(ctx, companyID, workItemID, projectID)
	if err != nil {
		return WorkResourceRequirement{}, err
	}
	if !workItemBelongs {
		return WorkResourceRequirement{}, ErrWorkItemNotFound
	}

	if resourceType == ResourceTypeMaterial {
		if materialID == nil {
			return WorkResourceRequirement{}, ErrMaterialIDRequired
		}
		materialBelongs, err := s.materialLookup.MaterialBelongsToCompany(ctx, companyID, *materialID)
		if err != nil {
			return WorkResourceRequirement{}, err
		}
		if !materialBelongs {
			return WorkResourceRequirement{}, ErrMaterialNotFound
		}
	}

	// Retry of the same suggestion returns the already-created requirement
	// before any duplicate-key check, so an idempotent retry never trips
	// ErrRequirementDuplicate against its own earlier write.
	if existing, findErr := s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID); findErr == nil {
		return existing, nil
	}

	dupKey := DuplicateKey{CompanyID: companyID, WorkItemID: workItemID, ResourceType: resourceType, NormalizedName: name}
	if materialID != nil {
		dupKey.MaterialID = *materialID
	}
	if _, findErr := s.repo.FindByDuplicateKey(ctx, dupKey); findErr == nil {
		return WorkResourceRequirement{}, ErrRequirementDuplicate
	}

	now := time.Now()
	created, err := s.repo.Create(ctx, WorkResourceRequirement{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID,
		ResourceType: resourceType, MaterialID: materialID, Name: name,
		Status: StatusConfirmed, Source: SourceAISuggestion,
		SourceSuggestionID: &sourceSuggestionID,
		CreatedAt:          now, UpdatedAt: now, SchemaVersion: 1,
	})
	if err != nil {
		// A concurrent request may have won the race between our checks
		// above and this write; re-check both idempotency anchors once
		// before surfacing the raw repository error.
		if existing, findErr := s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID); findErr == nil {
			return existing, nil
		}
		if _, findErr := s.repo.FindByDuplicateKey(ctx, dupKey); findErr == nil {
			return WorkResourceRequirement{}, ErrRequirementDuplicate
		}
		return WorkResourceRequirement{}, err
	}
	return created, nil
}

// FindBySourceSuggestionID looks up a requirement by its AI-suggestion
// provenance, tenant-scoped to companyID — the acceptance idempotency
// anchor (design doc §18.2).
func (s *Service) FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkResourceRequirement, error) {
	return s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
}

// ListByProject validates projectID belongs to companyID before listing —
// a foreign projectID returns ErrProjectNotFound, never an empty list.
func (s *Service) ListByProject(ctx context.Context, companyID, projectID string) ([]WorkResourceRequirement, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// ListByWorkItem validates workItemID belongs to companyID AND projectID
// before listing — a foreign/mismatched workItemID returns
// ErrWorkItemNotFound, never an empty list.
func (s *Service) ListByWorkItem(ctx context.Context, companyID, projectID, workItemID string) ([]WorkResourceRequirement, error) {
	belongs, err := s.workItemLookup.WorkItemBelongsToProject(ctx, companyID, workItemID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrWorkItemNotFound
	}
	return s.repo.ListByWorkItem(ctx, companyID, workItemID)
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public WorkResourceRequirementRepository interface. Only the
// real Mongo repository implements it; a fake used in unrelated tests simply
// does not satisfy this interface and is unaffected.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every WorkResourceRequirement
// owned by companyID. Development-tool use only (demoseed reset, design
// spec §6.6) — no production code path calls this. Idempotent: calling it
// when nothing remains for companyID is a no-op success, not an error.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("workresources: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}
