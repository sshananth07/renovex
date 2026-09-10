// Package properties owns Property records — the physical location a Project
// renovates. See phase1.md §6 and
// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md §1.3.
//
// properties never imports projects, clients, spaces, or work. It defines
// ProjectLookup, the one capability it needs from projects, satisfied structurally
// by projects.Service. It exposes no capability to any other module — nothing in
// M2 needs to look up a Property from another module (design spec §5.3).
package properties

import "time"

// Property is the physical location a Project renovates. At most one Property
// exists per Project (enforced by a unique {companyId, projectId} Mongo index,
// see EnsureIndexes) — Project itself stores no PropertyID field; the Project's
// Property, if any, is found via ListByProject.
type Property struct {
	ID            string    `bson:"_id,omitempty" json:"id"`
	CompanyID     string    `bson:"companyId" json:"companyId"`
	ProjectID     string    `bson:"projectId" json:"projectId"`
	Address       string    `bson:"address" json:"address"`
	PropertyType  string    `bson:"propertyType,omitempty" json:"propertyType,omitempty"`
	Notes         string    `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion int       `bson:"schemaVersion" json:"schemaVersion"`
}
