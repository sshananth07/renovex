package companies

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrCompanyNotFound is returned when a Company lookup finds no match.
var ErrCompanyNotFound = errors.New("companies: company not found")

// ErrMembershipNotFound is returned when a CompanyMembership lookup finds no match.
var ErrMembershipNotFound = errors.New("companies: membership not found")

// ErrUserAlreadyHasMembership is returned when attempting to create a second
// CompanyMembership for a User who already has one (Phase 1: one User = at most
// one Company).
var ErrUserAlreadyHasMembership = errors.New("companies: user already belongs to a company")

// CompanyRepository persists Companies. companies owns the companies collection
// exclusively; no other module may query it directly.
type CompanyRepository interface {
	Create(ctx context.Context, c Company) (Company, error)
	FindByID(ctx context.Context, id string) (Company, error)
	Delete(ctx context.Context, id string) error

	// FindByName looks up a Company by its exact display name. Returns
	// ErrCompanyNotFound if none matches. Used only by demoseed's
	// positive-identification checks (design spec §6.2) — no production
	// endpoint exposes company search by name.
	FindByName(ctx context.Context, name string) (Company, error)
}

// MembershipRepository persists CompanyMemberships. companies owns the
// company_members collection exclusively; no other module may query it directly.
type MembershipRepository interface {
	Create(ctx context.Context, m CompanyMembership) (CompanyMembership, error)
	FindByUserID(ctx context.Context, userID string) (CompanyMembership, error)
	// ListPaginated returns companyID's CompanyMemberships matching req
	// (search on userId/role, sorted per req.Sort/req.Order), plus the total
	// matching count using the same filter as the page query.
	ListPaginated(ctx context.Context, companyID string, req pagination.Request) ([]CompanyMembership, int, error)
	DeleteByUserID(ctx context.Context, userID string) error
}
