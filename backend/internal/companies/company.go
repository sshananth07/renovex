// Package companies owns Company (tenant) records and Company Membership
// (User-to-Company role assignment: Owner, Admin, Employee). See phase1.md §2-3
// and docs/superpowers/specs/2026-07-22-milestone-1-identity-tenancy-design.md.
//
// companies never imports identity. The one capability it needs from identity
// (UserProvisioner) is an interface defined in this package and satisfied
// structurally by identity.UserService, wired together only in cmd/api's
// composition root.
package companies

import "time"

// Company is a contractor tenant.
type Company struct {
	ID        string    `bson:"_id,omitempty" json:"id"`
	Name      string    `bson:"name" json:"name"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}
