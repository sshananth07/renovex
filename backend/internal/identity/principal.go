// Package identity owns User accounts, password credentials, authentication
// (login, JWT access/refresh tokens), and AuthSession lifecycle. See phase1.md §2
// and docs/superpowers/specs/2026-07-22-milestone-1-identity-tenancy-design.md.
//
// identity never imports companies. Cross-module capabilities it needs from
// companies (CompanyProvisioner, MembershipLookup) are interfaces defined in this
// package and satisfied structurally by companies.Service, wired together only in
// cmd/api's composition root.
package identity

// Principal is the authenticated-request context constructed by auth middleware
// from a verified access token's claims. Role is a raw string value
// ("owner"/"admin"/"employee") rather than companies.Role, preserving the
// zero-cross-import boundary between identity and companies.
type Principal struct {
	UserID    string
	CompanyID string
	Role      string
}
