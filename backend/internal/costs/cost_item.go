// Package costs owns CostItem — the single authoritative Project-cost ledger
// spanning every cost category, including Material and Labour
// (phase1.md §19, §39-41). Every LabourEntry (owned by internal/labour)
// creates exactly one linked CostItem{Category=labour} via the
// LabourCostRecorder capability this package exposes; the public
// CreateCostItem path structurally rejects category=labour. See
// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md
// §1.4.
//
// costs never imports projects, work, materials, or labour by type. It
// defines ProjectLookup, WorkItemLookup, and MaterialLookup, the three
// capabilities it needs, satisfied structurally by projects.Service,
// work.Service, and materials.Service respectively. It exposes
// LabourCostRecorder, consumed by labour, and VisitEstimatedCostItems,
// consumed by estimates (M4 design spec §6).
package costs

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// CostCategory is one of the 9 categories from phase1.md §19.
type CostCategory string

const (
	CostCategoryMaterial        CostCategory = "material"
	CostCategoryLabour          CostCategory = "labour"
	CostCategorySubcontractor   CostCategory = "subcontractor"
	CostCategoryEquipment       CostCategory = "equipment"
	CostCategoryTransport       CostCategory = "transport"
	CostCategoryPermit          CostCategory = "permit"
	CostCategoryProfessionalFee CostCategory = "professional_fee"
	CostCategoryUtility         CostCategory = "utility"
	CostCategoryMiscellaneous   CostCategory = "miscellaneous"
)

// IsValid reports whether c is one of the 9 defined categories.
func (c CostCategory) IsValid() bool {
	switch c {
	case CostCategoryMaterial, CostCategoryLabour, CostCategorySubcontractor, CostCategoryEquipment,
		CostCategoryTransport, CostCategoryPermit, CostCategoryProfessionalFee, CostCategoryUtility,
		CostCategoryMiscellaneous:
		return true
	default:
		return false
	}
}

// CostStage identifies one of the 4 lifecycle Money fields on a CostItem.
type CostStage string

const (
	CostStageEstimated CostStage = "estimated"
	CostStageCommitted CostStage = "committed"
	CostStageActual    CostStage = "actual"
	CostStagePaid      CostStage = "paid"
)

// IsValid reports whether s is one of the 4 defined stages.
func (s CostStage) IsValid() bool {
	switch s {
	case CostStageEstimated, CostStageCommitted, CostStageActual, CostStagePaid:
		return true
	default:
		return false
	}
}

// CostItem is the single authoritative Project-cost ledger record. Estimated,
// Committed, Actual, and Paid are independently nilable and may all be
// non-nil simultaneously (design spec §1.5) — never collapsed into a single
// status field. WorkItemID and Quantity/UnitPrice are optional: a CostItem
// may be Project-level with no specific WorkItem (e.g. a permit fee), and
// lump-sum costs (subcontractor, permit, professional fee) have no natural
// quantity. Paid is a cumulative running total, not a per-payment event
// (design spec §1.5, §22-E).
type CostItem struct {
	ID            string             `bson:"_id,omitempty" json:"id"`
	CompanyID     string             `bson:"companyId" json:"companyId"`
	ProjectID     string             `bson:"projectId" json:"projectId"`
	WorkItemID    *string            `bson:"workItemId,omitempty" json:"workItemId,omitempty"`
	Category      CostCategory       `bson:"category" json:"category"`
	Description   string             `bson:"description" json:"description"`
	Quantity      *quantity.Quantity `bson:"-" json:"-"` // never BSON-marshaled directly — see quantityDoc in repository_mongo.go
	UnitPrice     *money.Money       `bson:"unitPrice,omitempty" json:"unitPrice,omitempty"`
	MaterialID    *string            `bson:"materialId,omitempty" json:"materialId,omitempty"`
	Estimated     *money.Money       `bson:"estimated,omitempty" json:"estimated,omitempty"`
	Committed     *money.Money       `bson:"committed,omitempty" json:"committed,omitempty"`
	Actual        *money.Money       `bson:"actual,omitempty" json:"actual,omitempty"`
	Paid          *money.Money       `bson:"paid,omitempty" json:"paid,omitempty"`
	Currency      string             `bson:"currency" json:"currency"`
	Date          time.Time          `bson:"date" json:"date"`
	Notes         string             `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt     time.Time          `bson:"createdAt" json:"createdAt"`
	SchemaVersion int                `bson:"schemaVersion" json:"schemaVersion"`
	Revision      int64              `bson:"revision" json:"revision"`

	// ActualCorrections is an append-only audit trail of every prior value
	// Actual held before being corrected. It never shrinks and is never
	// rewritten or reordered — RecordCostItemActual/CorrectCostItemActual
	// are the only writers (paid/actual financial data must retain evidence
	// of what it previously recorded, never silently overwrite it).
	ActualCorrections []ActualCorrection `bson:"actualCorrections,omitempty" json:"actualCorrections,omitempty"`
}

// ActualCorrection records that a CostItem's Actual amount was corrected:
// what it was, what it became, why, who did it, and when. Appended to
// CostItem.ActualCorrections in insertion order — never rewritten or
// removed once appended.
type ActualCorrection struct {
	PreviousAmount  money.Money `bson:"previousAmount" json:"previousAmount"`
	NewAmount       money.Money `bson:"newAmount" json:"newAmount"`
	Reason          string      `bson:"reason" json:"reason"`
	CorrectedByUser string      `bson:"correctedByUser" json:"correctedByUser"`
	CorrectedAt     time.Time   `bson:"correctedAt" json:"correctedAt"`
}
