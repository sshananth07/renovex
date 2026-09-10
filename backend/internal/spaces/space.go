// Package spaces owns Space records — the physical areas (rooms) a contractor
// organizes renovation work by. See phase1.md §7 and
// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md §1.4.
//
// spaces never imports projects, clients, properties, or work. It defines
// ProjectLookup, the one capability it needs from projects, satisfied structurally
// by projects.Service. It exposes SpaceLookup, consumed by work.
package spaces

import "time"

// Space is a physical area (room) belonging directly to a Project — not to a
// Property (design spec §0.2/§1.4: this matches phase1.md §7's literal example
// JSON, which uses projectId, not propertyId).
type Space struct {
	ID          string `bson:"_id,omitempty" json:"id"`
	CompanyID   string `bson:"companyId" json:"companyId"`
	ProjectID   string `bson:"projectId" json:"projectId"`
	Name        string `bson:"name" json:"name"`
	Type        string `bson:"type,omitempty" json:"type,omitempty"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	// SourceSuggestionID is a nullable internal-only provenance field set only
	// by backend AI-acceptance code (never a public Huma request field) when
	// this Space was created by accepting an AISuggestion. It is the
	// acceptance idempotency anchor (M8.5B-A design doc §18.2).
	SourceSuggestionID *string   `bson:"sourceSuggestionId,omitempty" json:"-"`
	CreatedAt          time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion      int       `bson:"schemaVersion" json:"schemaVersion"`
}
