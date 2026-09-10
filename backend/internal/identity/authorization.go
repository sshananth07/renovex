package identity

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
)

// Route-level role authorization (M8 design spec §1A.1, §13).
//
// This is a NARROW, approved addition to the otherwise frozen identity module.
// It exists because the repository investigation established that no
// route-level role convention existed: M2-M7 gate on tenant only. M8 is the
// first milestone whose operations are externally visible and irreversible
// (issuing an RFQ to a Supplier, sending mail, finalising an award), so it is
// the first that needs one. It establishes the convention for M9 onward.
//
// Two rules shape the design:
//
//  1. Authorization runs at the HTTP boundary, BEFORE the privileged service
//     call. M8 services stay role-agnostic. The authenticated HTTP boundary is
//     the authorization boundary; if an operation ever becomes reachable
//     through another untrusted entry point, that entry point must apply its
//     own check rather than the service growing one.
//
//  2. Foreign-tenant access remains 404 and is NOT this helper's job — tenant
//     scoping already happens inside every service via CompanyID. This helper
//     answers only the second question: the caller is inside the right tenant,
//     but is their role sufficient? That answer is 403.
//
// M2-M7 are deliberately not retrofitted during M8.

// Role is a company membership role as carried on a verified access token.
//
// These values must match what companies persists on a membership exactly.
// identity does not import companies — the M1 zero-cross-import boundary — so
// the constants are declared here and pinned by test.
type Role string

// The three company membership roles.
const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleEmployee Role = "employee"
)

// ErrInsufficientRole reports an authenticated caller inside the correct tenant
// whose role does not permit the operation. Handlers map it to 403.
//
// It is deliberately distinct from a 404: a caller who may not issue an RFQ is
// still entitled to know the RFQ exists, because they can already see it.
var ErrInsufficientRole = errors.New("identity: insufficient company role")

// RequireCompanyRole reports whether principal's role is one of allowed.
//
// It FAILS CLOSED in every ambiguous case: an unknown role, an empty role, and
// an empty allow-list are all refused. An allow-list that defaulted to "permit"
// would silently open any route whose roles were forgotten, which is precisely
// the mistake this helper exists to prevent.
func RequireCompanyRole(principal Principal, allowed ...Role) error {
	for _, role := range allowed {
		// An unrecognised role can never match, because allowed only ever holds
		// the three declared constants.
		if principal.Role == string(role) {
			return nil
		}
	}
	return ErrInsufficientRole
}

// AuthorizedPrincipal is the handler-boundary form: it resolves the Principal
// from the request context and applies RequireCompanyRole, returning an error
// ready to return directly from a Huma handler.
//
// A missing Principal is 401, not 403 — it means the operation was registered
// outside the authenticated group, which is a wiring bug rather than a
// permission decision.
func AuthorizedPrincipal(ctx context.Context, allowed ...Role) (Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, huma.Error401Unauthorized("authentication required")
	}
	if err := RequireCompanyRole(principal, allowed...); err != nil {
		return Principal{}, huma.Error403Forbidden("insufficient company role for this operation")
	}
	return principal, nil
}
