// Package labour owns Worker (reusable company-level person/resource records)
// and LabourEntry (the financial/work record representing labour applied to
// a WorkItem — phase1.md §13-15). Every LabourEntry creates exactly one
// linked CostItem{Category=labour} via the LabourCostRecorder capability
// consumed from internal/costs; LabourEntry.Cost is a derived/snapshotted
// display value, and the linked CostItem.Estimated is the authoritative
// ledger value M4 aggregates from. See
// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md
// §1.2-1.3, §5.2, §9.
//
// labour never imports projects, work, materials, or costs by type. It
// defines ProjectLookup, WorkItemLookup, and LabourCostRecorder — the three
// capabilities it needs. LabourCostRecorder is defined here and satisfied
// structurally by costs.Service. labour exposes no capability to any other
// module.
package labour

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// RateType is one of the 5 supported rate types from phase1.md §13.
type RateType string

const (
	RateTypeHourly         RateType = "hourly"
	RateTypeDaily          RateType = "daily"
	RateTypeFixedProject   RateType = "fixed_project"
	RateTypePerUnit        RateType = "per_unit"
	RateTypePerSquareMeter RateType = "per_square_meter"
)

// IsValid reports whether r is one of the 5 defined rate types.
func (r RateType) IsValid() bool {
	switch r {
	case RateTypeHourly, RateTypeDaily, RateTypeFixedProject, RateTypePerUnit, RateTypePerSquareMeter:
		return true
	default:
		return false
	}
}

// Worker is a reusable, company-level person/resource record. Workers may
// participate in multiple Projects; Project-specific labour costs are never
// stored on Worker itself (phase1.md §13) — see LabourEntry.
type Worker struct {
	ID            string      `bson:"_id,omitempty" json:"id"`
	CompanyID     string      `bson:"companyId" json:"companyId"`
	Name          string      `bson:"name" json:"name"`
	Trade         string      `bson:"trade,omitempty" json:"trade,omitempty"`
	RateType      RateType    `bson:"rateType" json:"rateType"`
	DefaultRate   money.Money `bson:"defaultRate" json:"defaultRate"`
	ContactPhone  string      `bson:"contactPhone,omitempty" json:"contactPhone,omitempty"`
	ContactEmail  string      `bson:"contactEmail,omitempty" json:"contactEmail,omitempty"`
	CreatedAt     time.Time   `bson:"createdAt" json:"createdAt"`
	SchemaVersion int         `bson:"schemaVersion" json:"schemaVersion"`
}
