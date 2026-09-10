// Package clients owns Client (contractor customer) records — the first tenant-owned
// business resource in Milestone 2. See phase1.md §4 and
// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md §1.1.
//
// clients never imports projects, properties, spaces, or work. The one capability it
// exposes (ClientLookup) is an interface defined in the consuming package (projects)
// and satisfied structurally by clients.Service, wired together only in cmd/api's
// composition root.
package clients

import "time"

// Client is a contractor's customer — a returning entity that may have multiple
// Projects over time (phase1.md §4). Project history is a query
// (projects.ListProjectsByClient), never embedded on Client.
type Client struct {
	ID             string    `bson:"_id,omitempty" json:"id"`
	CompanyID      string    `bson:"companyId" json:"companyId"`
	Name           string    `bson:"name" json:"name"`
	Phone          string    `bson:"phone,omitempty" json:"phone,omitempty"`
	Email          string    `bson:"email,omitempty" json:"email,omitempty"`
	Address        string    `bson:"address,omitempty" json:"address,omitempty"`
	BillingAddress string    `bson:"billingAddress,omitempty" json:"billingAddress,omitempty"`
	Notes          string    `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt      time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion  int       `bson:"schemaVersion" json:"schemaVersion"`
}
