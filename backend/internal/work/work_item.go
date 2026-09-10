// Package work owns WorkItem records — the actual renovation work performed,
// and the bridge between the physical Project/Space hierarchy and the
// Resource/Cost/Estimate/Quotation financial system. See phase1.md §8-9,
// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md §1.5,
// and docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md §9.3.
//
// work never imports projects, clients, properties, spaces, materials, labour,
// or costs. It defines ProjectLookup and SpaceLookup, the two capabilities it
// needs, satisfied structurally by projects.Service and spaces.Service
// respectively. As of M3, it exposes WorkItemBelongsToProject and
// WorkItemBelongsToCompany, consumed by labour and costs via their own
// WorkItemLookup interfaces.
package work

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// WorkItemStatus is planned or cancelled. cancelled is terminal in M2 — there is
// no cancelled -> planned transition (design spec §5.5).
type WorkItemStatus string

const (
	WorkItemStatusPlanned   WorkItemStatus = "planned"
	WorkItemStatusCancelled WorkItemStatus = "cancelled"
)

// IsValid reports whether s is one of the 2 defined statuses.
func (s WorkItemStatus) IsValid() bool {
	switch s {
	case WorkItemStatusPlanned, WorkItemStatusCancelled:
		return true
	default:
		return false
	}
}

// WorkItemSource distinguishes manually-created WorkItems from future
// AI-suggested ones (phase1.md §9). M2 only ever creates WorkItemSourceManual.
type WorkItemSource string

const (
	WorkItemSourceManual       WorkItemSource = "manual"
	WorkItemSourceAISuggestion WorkItemSource = "ai_suggestion"
)

// VerificationStatus tracks AI-suggestion review (phase1.md §9). M2 only ever
// creates WorkItems with VerificationStatusConfirmed and never transitions this
// field — no M2 endpoint reads or writes VerificationStatus after creation
// (design spec §5.5).
type VerificationStatus string

const (
	VerificationStatusPending   VerificationStatus = "pending"
	VerificationStatusConfirmed VerificationStatus = "confirmed"
	VerificationStatusRejected  VerificationStatus = "rejected"
)

// WorkItem represents actual renovation work. ProjectID is always required;
// SpaceID is optional (nil for Project-level work with no natural single Space,
// e.g. "obtain renovation permit" — design spec §1.5). Quantity uses the
// foundation/quantity primitive directly, never a reinvented decimal+unit pair.
type WorkItem struct {
	ID                 string             `bson:"_id,omitempty" json:"id"`
	CompanyID          string             `bson:"companyId" json:"companyId"`
	ProjectID          string             `bson:"projectId" json:"projectId"`
	SpaceID            *string            `bson:"spaceId,omitempty" json:"spaceId,omitempty"`
	Description        string             `bson:"description" json:"description"`
	WorkType           string             `bson:"workType,omitempty" json:"workType,omitempty"`
	Quantity           quantity.Quantity  `bson:"-" json:"-"` // never BSON-marshaled directly — see quantityDoc in repository_mongo.go
	Status             WorkItemStatus     `bson:"status" json:"status"`
	Source             WorkItemSource     `bson:"source" json:"source"`
	VerificationStatus VerificationStatus `bson:"verificationStatus" json:"verificationStatus"`
	// SourceSuggestionID is a nullable internal-only provenance field set only
	// by backend AI-acceptance code (never a public Huma request field) when
	// this WorkItem was created by accepting an AISuggestion. It is the
	// acceptance idempotency anchor (M8.5B-A design doc §18.2).
	SourceSuggestionID *string   `bson:"sourceSuggestionId,omitempty" json:"-"`
	CreatedAt          time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion      int       `bson:"schemaVersion" json:"schemaVersion"`
}
