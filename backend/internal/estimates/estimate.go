package estimates

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// EstimateStatus is one of the 2 lifecycle states an Estimate version can be in.
type EstimateStatus string

const (
	EstimateStatusDraft     EstimateStatus = "draft"
	EstimateStatusFinalized EstimateStatus = "finalized"
)

// IsValid reports whether s is one of the 2 defined statuses.
func (s EstimateStatus) IsValid() bool {
	switch s {
	case EstimateStatusDraft, EstimateStatusFinalized:
		return true
	default:
		return false
	}
}

// PricingMode selects which pricing formula PricingRate is interpreted
// under (design spec §13). Markup and margin are never simultaneously
// active — an Estimate has exactly one PricingMode at a time.
type PricingMode string

const (
	PricingModeMarkup PricingMode = "markup"
	PricingModeMargin PricingMode = "margin"
)

// IsValid reports whether m is one of the 2 defined pricing modes.
func (m PricingMode) IsValid() bool {
	switch m {
	case PricingModeMarkup, PricingModeMargin:
		return true
	default:
		return false
	}
}

// Estimate is one immutable-once-finalized version of a Project's internal
// financial calculation. Version is a business-meaningful commercial
// revision counter (only advances via CreateNewVersion); Revision is a
// separate optimistic-concurrency counter guarding in-place draft mutation
// (refresh, pricing recalculation, finalize); SchemaVersion is the document
// schema version (§52). All three are distinct fields with distinct
// meanings (design spec §1).
type Estimate struct {
	ID        string
	CompanyID string
	ProjectID string
	Version   int
	Status    EstimateStatus
	Revision  int64

	Currency string

	Lines                 []EstimateCostLine
	CostSubtotal          money.Money
	ExcludedCostItemCount int

	PricingMode PricingMode
	PricingRate money.RateBPS

	ProposedSellingPrice    money.Money
	ProjectedGrossProfit    money.Money
	ProjectedGrossMarginBPS money.RateBPS

	CreatedAt     time.Time
	RefreshedAt   *time.Time
	FinalizedAt   *time.Time
	SchemaVersion int
}

// EstimateCostLine is one snapshotted cost line, sourced from exactly one
// CostItem at the moment the Estimate was created or last refreshed.
// SourceCostItemID is retained for traceability only — it is never
// dereferenced at read time to fetch a live CostItem.Estimated value; this
// line's own SnapshottedAmount is what the Estimate's totals are computed
// from (design spec §5).
type EstimateCostLine struct {
	SourceCostItemID  string
	WorkItemID        *string
	Category          string
	Description       string
	SnapshottedAmount money.Money
}
