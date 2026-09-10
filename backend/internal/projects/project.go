// Package projects owns Project records — the root business entity a Client's
// renovation job becomes. See phase1.md §5 and
// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md §1.2.
//
// projects never imports clients, properties, spaces, or work. It defines
// ClientLookup, the one capability it needs from clients, satisfied structurally by
// clients.Service. It exposes ProjectLookup, consumed by properties, spaces, and work.
package projects

import "time"

// ProjectStatus is one of the 8 statuses phase1.md §5 enumerates.
type ProjectStatus string

const (
	ProjectStatusLead              ProjectStatus = "lead"
	ProjectStatusSiteVisit         ProjectStatus = "site_visit"
	ProjectStatusEstimating        ProjectStatus = "estimating"
	ProjectStatusQuotationSent     ProjectStatus = "quotation_sent"
	ProjectStatusQuotationApproved ProjectStatus = "quotation_approved"
	ProjectStatusInProgress        ProjectStatus = "in_progress"
	ProjectStatusCompleted         ProjectStatus = "completed"
	ProjectStatusClosed            ProjectStatus = "closed"
)

// IsValid reports whether s is one of the 8 defined statuses.
func (s ProjectStatus) IsValid() bool {
	switch s {
	case ProjectStatusLead, ProjectStatusSiteVisit, ProjectStatusEstimating,
		ProjectStatusQuotationSent, ProjectStatusQuotationApproved,
		ProjectStatusInProgress, ProjectStatusCompleted, ProjectStatusClosed:
		return true
	default:
		return false
	}
}

// Project is the root business entity: one renovation or construction job.
// Project stores no PropertyID field — see design spec §7. Any status is a valid
// destination from any other status in Milestone 2 (no transition-graph
// enforcement — see design spec §1.2).
type Project struct {
	ID        string        `bson:"_id,omitempty" json:"id"`
	CompanyID string        `bson:"companyId" json:"companyId"`
	ClientID  string        `bson:"clientId" json:"clientId"`
	Name      string        `bson:"name" json:"name"`
	Status    ProjectStatus `bson:"status" json:"status"`
	// ScopeBrief is contractor-authored descriptive project context (M8.5B-A
	// design doc §6) — normal authoritative Project data, never AI output.
	// Defaults to "" and may be explicitly cleared back to "".
	ScopeBrief    string    `bson:"scopeBrief" json:"scopeBrief"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion int       `bson:"schemaVersion" json:"schemaVersion"`
}
