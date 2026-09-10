package spaces

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrSpaceNotFound is returned when a Space lookup finds no match — including a
// Space that exists but belongs to a different company.
var ErrSpaceNotFound = errors.New("spaces: space not found")

// SpaceRepository persists Spaces. spaces owns the spaces collection exclusively;
// no other module may query it directly.
type SpaceRepository interface {
	Create(ctx context.Context, s Space) (Space, error)
	FindByID(ctx context.Context, companyID, id string) (Space, error)
	// ListPaginated returns companyID's Spaces for projectID matching req
	// (search on name/type/description, sorted per req.Sort/req.Order),
	// plus the total matching count using the same filter as the page query.
	ListPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]Space, int, error)
	Update(ctx context.Context, companyID, id string, fn func(*Space)) (Space, error)
	// BelongsToProject reports whether id exists, belongs to companyID, AND its
	// stored ProjectID equals projectID — a single compound-filtered query used by
	// MongoSpaceRepository to satisfy SpaceLookup.SpaceBelongsToProject without a
	// separate existence check followed by a lineage check (design spec §8.4, §10.3).
	BelongsToProject(ctx context.Context, companyID, id, projectID string) (bool, error)
	// FindBySourceSuggestionID is the acceptance idempotency anchor: it looks
	// up a Space by its AI-suggestion provenance, tenant-scoped, so a retried
	// acceptance can find and reuse an already-created Space rather than
	// creating a duplicate (M8.5B-A design doc §18.2).
	FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (Space, error)
}
