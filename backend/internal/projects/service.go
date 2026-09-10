package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrNameRequired is returned when CreateProject is given an empty name.
var ErrNameRequired = errors.New("projects: name is required")

// ProjectSortFields is the sort allowlist for GET /projects.
var ProjectSortFields = []string{"createdAt", "name", "status"}

const (
	ProjectDefaultSort  = "createdAt"
	ProjectDefaultOrder = pagination.OrderDesc
)

// ErrClientNotFound is returned when the given clientID does not belong to the
// caller's company (per ClientLookup) — deliberately reusing clients.ErrClientNotFound's
// name/shape locally (a distinct sentinel value in this package) so callers get the
// same "not found" semantics without projects importing clients' error type directly.
var ErrClientNotFound = errors.New("projects: client not found")

// ErrInvalidStatus is returned when UpdateProjectStatus is given a status outside
// the 8 defined ProjectStatus values.
var ErrInvalidStatus = errors.New("projects: invalid project status")

// ErrScopeBriefTooLong is returned when UpdateProjectScopeBrief is given a
// brief exceeding scopeBriefMaxLength Unicode characters after trimming.
var ErrScopeBriefTooLong = errors.New("projects: scope brief too long")

// scopeBriefMaxLength is the maximum Unicode-character length of a
// contractor-authored ScopeBrief (M8.5B-A design doc §6).
const scopeBriefMaxLength = 5000

// ClientLookup is the capability projects needs from clients: confirming a
// clientID belongs to the caller's company before creating or listing by it.
// Defined here (consumer-defines-interface); satisfied structurally by
// clients.Service with no import in either direction.
type ClientLookup interface {
	ClientBelongsToCompany(ctx context.Context, companyID, clientID string) (bool, error)
}

// Service implements Project CRUD, client-parent validation, and exposes
// ProjectLookup, the capability properties/spaces/work need from this module.
type Service struct {
	repo         ProjectRepository
	clientLookup ClientLookup
}

// NewService constructs a Service backed by repo, consuming clientLookup to
// validate parent Client references.
func NewService(repo ProjectRepository, clientLookup ClientLookup) *Service {
	return &Service{repo: repo, clientLookup: clientLookup}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public ProjectRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Project owned by companyID.
// Development-tool use only (demoseed reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("projects: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateProject validates clientID belongs to companyID, validates name is
// non-empty, and persists a new Project with default status Lead.
func (s *Service) CreateProject(ctx context.Context, companyID, clientID, name string) (Project, error) {
	if name == "" {
		return Project{}, ErrNameRequired
	}
	belongs, err := s.clientLookup.ClientBelongsToCompany(ctx, companyID, clientID)
	if err != nil {
		return Project{}, err
	}
	if !belongs {
		return Project{}, ErrClientNotFound
	}
	return s.repo.Create(ctx, Project{
		CompanyID: companyID, ClientID: clientID, Name: name,
		Status: ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetProject returns projectID's Project, tenant-scoped to companyID.
func (s *Service) GetProject(ctx context.Context, companyID, projectID string) (Project, error) {
	return s.repo.FindByID(ctx, companyID, projectID)
}

// ListProjectsPaginated returns companyID's Projects matching req. When
// clientID is non-empty, it validates the client belongs to companyID
// before listing — a foreign clientID returns ErrClientNotFound, never an
// empty list (design spec §10.4). An empty clientID lists across all
// clients.
func (s *Service) ListProjectsPaginated(ctx context.Context, companyID, clientID string, req pagination.Request) ([]Project, int, error) {
	if clientID != "" {
		belongs, err := s.clientLookup.ClientBelongsToCompany(ctx, companyID, clientID)
		if err != nil {
			return nil, 0, err
		}
		if !belongs {
			return nil, 0, ErrClientNotFound
		}
	}
	return s.repo.ListPaginated(ctx, companyID, clientID, req)
}

// UpdateProjectStatus validates status is one of the 8 defined values, then
// updates projectID's status, tenant-scoped to companyID. Any valid status is
// accepted as a destination from any other — no transition-graph enforcement in M2.
func (s *Service) UpdateProjectStatus(ctx context.Context, companyID, projectID string, status ProjectStatus) (Project, error) {
	if !status.IsValid() {
		return Project{}, ErrInvalidStatus
	}
	return s.repo.UpdateStatus(ctx, companyID, projectID, status)
}

// UpdateProjectName validates name is non-empty after trimming, then
// updates projectID's name, tenant-scoped to companyID. ClientID and Status
// are untouched — this is a narrow metadata patch, not combined with status
// transition logic (Checkpoint 10; status stays on
// PATCH /projects/{id}/status).
func (s *Service) UpdateProjectName(ctx context.Context, companyID, projectID, name string) (Project, error) {
	if strings.TrimSpace(name) == "" {
		return Project{}, ErrNameRequired
	}
	return s.repo.UpdateName(ctx, companyID, projectID, name)
}

// UpdateProjectScopeBrief validates scopeBrief after trimming (max
// scopeBriefMaxLength Unicode characters; empty after trim clears the
// brief), then updates projectID's ScopeBrief, tenant-scoped to companyID.
// Name/ClientID/Status are untouched. No AI call occurs in this service —
// ScopeBrief is normal authoritative contractor-authored data (M8.5B-A
// design doc §6).
func (s *Service) UpdateProjectScopeBrief(ctx context.Context, companyID, projectID, scopeBrief string) (Project, error) {
	trimmed := strings.TrimSpace(scopeBrief)
	if utf8.RuneCountInString(trimmed) > scopeBriefMaxLength {
		return Project{}, ErrScopeBriefTooLong
	}
	return s.repo.UpdateScopeBrief(ctx, companyID, projectID, trimmed)
}

// ProjectBelongsToCompany reports whether projectID exists and belongs to
// companyID. Satisfies properties.ProjectLookup, spaces.ProjectLookup, and
// work.ProjectLookup structurally.
func (s *Service) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	_, err := s.repo.FindByID(ctx, companyID, projectID)
	if err == ErrProjectNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetProjectClientID returns projectID's ClientID, tenant-scoped to
// companyID. Satisfies quotations.ProjectLookup structurally (M5 design
// spec §18/§22) — returns the bare ClientID string, never a Project
// struct (ADR 0002).
func (s *Service) GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error) {
	p, err := s.repo.FindByID(ctx, companyID, projectID)
	if err != nil {
		return "", err
	}
	return p.ClientID, nil
}

// --- Milestone 6 capabilities ---

// quotationSentEligibleFrom lists the only statuses from which a share may
// advance a Project. Everything later in the pipeline is deliberately absent
// so a share can never move an approved/in-progress/completed/closed Project
// backward (M6 design spec §8.1).
var quotationSentEligibleFrom = []ProjectStatus{
	ProjectStatusLead, ProjectStatusSiteVisit, ProjectStatusEstimating,
}

// quotationApprovedEligibleFrom lists the statuses from which an acceptance
// may advance a Project. It includes quotation_sent (the normal path) but,
// like the above, excludes everything later.
var quotationApprovedEligibleFrom = []ProjectStatus{
	ProjectStatusLead, ProjectStatusSiteVisit, ProjectStatusEstimating,
	ProjectStatusQuotationSent,
}

// AdvanceProjectToQuotationSent monotonically advances projectID to
// quotation_sent. It reports changed=false (not an error) when the Project is
// already at or past that status — callers treat this as success, which is
// what lets a repeated share safely retry the projection (M6 design spec §8).
// Satisfies access.ProjectStatusUpdater structurally.
func (s *Service) AdvanceProjectToQuotationSent(ctx context.Context, companyID, projectID string) (bool, bool, error) {
	return s.advanceProjectStatus(ctx, companyID, projectID, quotationSentEligibleFrom, ProjectStatusQuotationSent)
}

// AdvanceProjectToQuotationApproved monotonically advances projectID to
// quotation_approved, with the same never-move-backward guarantee.
func (s *Service) AdvanceProjectToQuotationApproved(ctx context.Context, companyID, projectID string) (bool, bool, error) {
	return s.advanceProjectStatus(ctx, companyID, projectID, quotationApprovedEligibleFrom, ProjectStatusQuotationApproved)
}

// advanceProjectStatus returns (changed, found, err). A missing project is
// reported as found=false with no error, so a best-effort projection update
// never fails a business operation over a project that has since been removed.
func (s *Service) advanceProjectStatus(ctx context.Context, companyID, projectID string, eligibleFrom []ProjectStatus, target ProjectStatus) (bool, bool, error) {
	_, changed, err := s.repo.UpdateStatusIfCurrent(ctx, companyID, projectID, eligibleFrom, target)
	if err == ErrProjectNotFound {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return changed, true, nil
}

// GetProjectName returns projectID's display name, tenant-scoped. Used to
// populate the Client-facing quotation view (M6 design spec §5.3) — returns
// the bare name, never a Project struct (ADR 0002).
func (s *Service) GetProjectName(ctx context.Context, companyID, projectID string) (string, bool, error) {
	p, err := s.repo.FindByID(ctx, companyID, projectID)
	if err == ErrProjectNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return p.Name, true, nil
}
