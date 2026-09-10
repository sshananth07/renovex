package projects

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrProjectNotFound is returned when a Project lookup finds no match — including a
// Project that exists but belongs to a different company.
var ErrProjectNotFound = errors.New("projects: project not found")

// ProjectRepository persists Projects. projects owns the projects collection
// exclusively; no other module may query it directly.
type ProjectRepository interface {
	Create(ctx context.Context, p Project) (Project, error)
	FindByID(ctx context.Context, companyID, id string) (Project, error)
	// ListPaginated returns companyID's Projects matching req (search on
	// name, sorted per req.Sort/req.Order), optionally filtered to
	// clientID (empty string means "all clients"), plus the total matching
	// count using the same filter as the page query.
	ListPaginated(ctx context.Context, companyID, clientID string, req pagination.Request) ([]Project, int, error)
	UpdateStatus(ctx context.Context, companyID, id string, status ProjectStatus) (Project, error)
	// UpdateName sets a Project's Name field only, leaving ClientID and
	// Status untouched — the counterpart to UpdateStatus, which sets Status
	// only.
	UpdateName(ctx context.Context, companyID, id, name string) (Project, error)

	// UpdateScopeBrief sets a Project's ScopeBrief field only, leaving every
	// other field untouched. An empty scopeBrief clears it.
	UpdateScopeBrief(ctx context.Context, companyID, id, scopeBrief string) (Project, error)

	// UpdateStatusIfCurrent conditionally sets status to target ONLY when the
	// stored status is one of eligibleFrom, reporting whether it changed. This
	// compare-and-set is what makes M6's Project transitions monotonic without
	// introducing a Project.Revision field: a project already at or past the
	// target simply reports changed=false rather than moving backward
	// (M6 design spec §8).
	UpdateStatusIfCurrent(ctx context.Context, companyID, id string, eligibleFrom []ProjectStatus, target ProjectStatus) (Project, bool, error)
}
