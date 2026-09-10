package clients

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrClientNotFound is returned when a Client lookup finds no match — including a
// Client that exists but belongs to a different company (see design spec §6).
var ErrClientNotFound = errors.New("clients: client not found")

// ClientRepository persists Clients. clients owns the clients collection
// exclusively; no other module may query it directly.
type ClientRepository interface {
	Create(ctx context.Context, c Client) (Client, error)
	FindByID(ctx context.Context, companyID, id string) (Client, error)
	// ListPaginated returns companyID's Clients matching req (search across
	// name/email/phone, sorted per req.Sort/req.Order), plus the total count
	// of matching documents across all pages — using the same tenant+search
	// filter for both the page query and the count, so total is never
	// computed from a different predicate than the page itself.
	ListPaginated(ctx context.Context, companyID string, req pagination.Request) ([]Client, int, error)
	Update(ctx context.Context, companyID, id string, fn func(*Client)) (Client, error)
}
