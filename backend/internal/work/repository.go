package work

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrWorkItemNotFound is returned when a WorkItem lookup finds no match —
// including a WorkItem that exists but belongs to a different company.
var ErrWorkItemNotFound = errors.New("work: work item not found")

// WorkItemRepository persists WorkItems. work owns the work_items collection
// exclusively; no other module may query it directly.
type WorkItemRepository interface {
	Create(ctx context.Context, w WorkItem) (WorkItem, error)
	FindByID(ctx context.Context, companyID, id string) (WorkItem, error)
	// ListPaginated returns companyID's WorkItems matching req (search on
	// description/workType, sorted per req.Sort/req.Order), scoped to
	// exactly one of projectID or spaceID (the empty one is ignored — the
	// service layer enforces "exactly one" before calling this), plus the
	// total matching count using the same filter as the page query.
	ListPaginated(ctx context.Context, companyID, projectID, spaceID string, req pagination.Request) ([]WorkItem, int, error)
	UpdateStatus(ctx context.Context, companyID, id string, status WorkItemStatus) (WorkItem, error)
	// Update applies fn to the existing WorkItem (already loaded, tenant-
	// scoped) and persists the result — the same read-modify-write callback
	// shape as clients.ClientRepository.Update and
	// spaces.SpaceRepository.Update.
	Update(ctx context.Context, companyID, id string, fn func(*WorkItem)) (WorkItem, error)
	// BelongsToProject reports whether id exists, belongs to companyID, AND its
	// stored ProjectID equals projectID — a single compound-filtered query used by
	// MongoWorkItemRepository to satisfy WorkItemLookup.WorkItemBelongsToProject
	// without a separate existence check followed by a lineage check (M3 design
	// spec §9.3, mirroring spaces.SpaceRepository.BelongsToProject from M2).
	BelongsToProject(ctx context.Context, companyID, id, projectID string) (bool, error)
	// FindBySourceSuggestionID is the acceptance idempotency anchor: it looks
	// up a WorkItem by its AI-suggestion provenance, tenant-scoped, so a
	// retried acceptance can find and reuse an already-created WorkItem
	// rather than creating a duplicate (M8.5B-A design doc §18.2).
	FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkItem, error)
}
