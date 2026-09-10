package spaces

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrNameRequired is returned when CreateSpace or UpdateSpace is given an empty name.
var ErrNameRequired = errors.New("spaces: name is required")

// SpaceSortFields is the sort allowlist for GET /spaces.
var SpaceSortFields = []string{"createdAt", "name"}

const (
	SpaceDefaultSort  = "createdAt"
	SpaceDefaultOrder = pagination.OrderAsc
)

// ErrProjectNotFound is returned when the given projectID does not belong to the
// caller's company (per ProjectLookup) — a distinct sentinel value in this package,
// not an import of projects.ErrProjectNotFound.
var ErrProjectNotFound = errors.New("spaces: project not found")

// ProjectLookup is the capability spaces needs from projects: confirming a
// projectID belongs to the caller's company before creating or listing by it.
// Defined here (consumer-defines-interface); satisfied structurally by
// projects.Service with no import in either direction.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// Service implements Space CRUD, project-parent validation, and exposes
// SpaceLookup, the capability work needs from this module.
type Service struct {
	repo          SpaceRepository
	projectLookup ProjectLookup
}

// NewService constructs a Service backed by repo, consuming projectLookup to
// validate parent Project references.
func NewService(repo SpaceRepository, projectLookup ProjectLookup) *Service {
	return &Service{repo: repo, projectLookup: projectLookup}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public SpaceRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Space owned by companyID.
// Development-tool use only (demoseed reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("spaces: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateSpace validates projectID belongs to companyID, validates name is
// non-empty, and persists a new Space.
func (s *Service) CreateSpace(ctx context.Context, companyID, projectID, name, spaceType, description string) (Space, error) {
	if name == "" {
		return Space{}, ErrNameRequired
	}
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Space{}, err
	}
	if !belongs {
		return Space{}, ErrProjectNotFound
	}
	return s.repo.Create(ctx, Space{
		CompanyID: companyID, ProjectID: projectID, Name: name,
		Type: spaceType, Description: description, CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetSpace returns spaceID's Space, tenant-scoped to companyID.
func (s *Service) GetSpace(ctx context.Context, companyID, spaceID string) (Space, error) {
	return s.repo.FindByID(ctx, companyID, spaceID)
}

// ListSpacesPaginated validates projectID belongs to companyID before
// listing — a foreign projectID returns ErrProjectNotFound, never an empty
// list.
func (s *Service) ListSpacesPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]Space, int, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, 0, err
	}
	if !belongs {
		return nil, 0, ErrProjectNotFound
	}
	return s.repo.ListPaginated(ctx, companyID, projectID, req)
}

// UpdateSpace updates spaceID's fields, tenant-scoped to companyID.
func (s *Service) UpdateSpace(ctx context.Context, companyID, spaceID, name, spaceType, description string) (Space, error) {
	if name == "" {
		return Space{}, ErrNameRequired
	}
	return s.repo.Update(ctx, companyID, spaceID, func(sp *Space) {
		sp.Name = name
		sp.Type = spaceType
		sp.Description = description
	})
}

// CreateSpaceFromAISuggestion is the narrow internal seam AI-suggestion
// acceptance uses to create a Space (M8.5B-A design doc §9.3). sourceSuggestionID
// is backend-supplied provenance only — never a public Huma request field —
// and preserves every existing Space domain validation. On a retried
// acceptance for the same sourceSuggestionID (e.g. after a network failure
// following a successful create), it returns the already-created Space
// instead of creating a duplicate, using the sparse-unique-index race as the
// authoritative signal rather than a read-then-write check.
func (s *Service) CreateSpaceFromAISuggestion(ctx context.Context, companyID, projectID, name, spaceType, description, sourceSuggestionID string) (Space, error) {
	if name == "" {
		return Space{}, ErrNameRequired
	}
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Space{}, err
	}
	if !belongs {
		return Space{}, ErrProjectNotFound
	}

	created, err := s.repo.Create(ctx, Space{
		CompanyID: companyID, ProjectID: projectID, Name: name, Type: spaceType,
		Description: description, SourceSuggestionID: &sourceSuggestionID,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		if existing, findErr := s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID); findErr == nil {
			return existing, nil
		}
		return Space{}, err
	}
	return created, nil
}

// FindSpaceBySourceSuggestionID looks up a Space by its AI-suggestion
// provenance, tenant-scoped to companyID — the acceptance idempotency anchor
// (M8.5B-A design doc §18.2).
func (s *Service) FindSpaceBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (Space, error) {
	return s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
}

// SpaceBelongsToCompany reports whether spaceID exists and belongs to companyID.
// Satisfies work.SpaceLookup's company-ownership-only method, used by
// ListWorkItemsBySpace which has no second parent ID to cross-check.
func (s *Service) SpaceBelongsToCompany(ctx context.Context, companyID, spaceID string) (bool, error) {
	_, err := s.repo.FindByID(ctx, companyID, spaceID)
	if err == ErrSpaceNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SpaceBelongsToProject reports whether spaceID exists, belongs to companyID, AND
// its stored ProjectID equals projectID. Satisfies work.SpaceLookup's
// lineage-checking method, used by CreateWorkItem when a spaceID is given
// (design spec §8.4, §10.3).
func (s *Service) SpaceBelongsToProject(ctx context.Context, companyID, spaceID, projectID string) (bool, error) {
	return s.repo.BelongsToProject(ctx, companyID, spaceID, projectID)
}
