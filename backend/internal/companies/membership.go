package companies

import "time"

// Role is a Company Membership's role. Phase 1 defines exactly three; this is
// not a generic RBAC framework.
type Role string

const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleEmployee Role = "employee"
)

// IsValid reports whether r is one of the three defined roles.
func (r Role) IsValid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleEmployee:
		return true
	default:
		return false
	}
}

// CompanyMembership links exactly one User to exactly one Company with a Role.
// Phase 1 enforces at most one Membership per User (see repository unique index).
type CompanyMembership struct {
	ID        string    `bson:"_id,omitempty" json:"id"`
	UserID    string    `bson:"userId" json:"userId"`
	CompanyID string    `bson:"companyId" json:"companyId"`
	Role      Role      `bson:"role" json:"role"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}
