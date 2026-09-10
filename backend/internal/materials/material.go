// Package materials owns the company-scoped Material catalog and its
// reference pricing (phase1.md §10-11). A Material is a reusable
// company-level catalog entry, never tied to a single Project or WorkItem —
// demand linkage from a WorkItem to a Material is MaterialRequirement's job,
// out of scope until the Procurement milestone. See
// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md §1.1.
//
// materials never imports projects, work, labour, or costs. It exposes
// MaterialLookup, consumed by costs. It consumes nothing — materials has no
// parent-validation dependency on any other module.
package materials

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// Material is a company-owned catalog entry with one current reference price.
// ReferencePrice is informational/pre-fill only — see CostItem.UnitPrice in
// internal/costs for the authoritative, snapshotted price on an actual cost
// record (design spec §1.1.4-5).
type Material struct {
	ID                 string      `bson:"_id,omitempty" json:"id"`
	CompanyID          string      `bson:"companyId" json:"companyId"`
	Name               string      `bson:"name" json:"name"`
	Category           string      `bson:"category,omitempty" json:"category,omitempty"`
	Specification      string      `bson:"specification,omitempty" json:"specification,omitempty"`
	Unit               string      `bson:"unit" json:"unit"`
	ReferencePrice     money.Money `bson:"referencePrice" json:"referencePrice"`
	ReferencePriceAsOf time.Time   `bson:"referencePriceAsOf" json:"referencePriceAsOf"`
	// SourceSuggestionID is a nullable internal-only provenance field set only
	// by backend AI-acceptance code (never a public Huma request field) when
	// this Material was created through the AI Resource Assistant's Create &
	// Add flow. It is the acceptance idempotency anchor (M8.5B-A design doc
	// §18.2).
	SourceSuggestionID *string   `bson:"sourceSuggestionId,omitempty" json:"-"`
	CreatedAt          time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion      int       `bson:"schemaVersion" json:"schemaVersion"`
}
