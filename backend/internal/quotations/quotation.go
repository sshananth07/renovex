package quotations

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// QuotationStatus is one of the 2 lifecycle states a Quotation version can
// be in (design spec §12). No sent/approved/rejected states in M5 — those
// are M6's Client-facing states.
type QuotationStatus string

const (
	QuotationStatusDraft     QuotationStatus = "draft"
	QuotationStatusFinalized QuotationStatus = "finalized"
)

// IsValid reports whether s is one of the 2 defined statuses.
func (s QuotationStatus) IsValid() bool {
	switch s {
	case QuotationStatusDraft, QuotationStatusFinalized:
		return true
	default:
		return false
	}
}

// TaxMode selects whether a Quotation has an active percentage tax
// configured (design spec §15). TaxMode=none is the default for every
// newly-generated Quotation — the system never infers taxability.
type TaxMode string

const (
	TaxModeNone       TaxMode = "none"
	TaxModePercentage TaxMode = "percentage"
)

// IsValid reports whether m is one of the 2 defined tax modes.
func (m TaxMode) IsValid() bool {
	switch m {
	case TaxModeNone, TaxModePercentage:
		return true
	default:
		return false
	}
}

// Quotation is one immutable-once-finalized version of a customer-facing
// commercial document, generated from a specific finalized Estimate.
// QuotationNumber (e.g. "QT-000124") is stable across ALL versions of one
// commercial quotation chain; Version is a business-meaningful commercial
// revision counter (only advances via CreateNewVersion); Revision is a
// separate optimistic-concurrency counter guarding in-place draft mutation;
// SchemaVersion is the document schema version. All four are distinct
// fields with distinct meanings (design spec §9.1) — never conflate any
// two.
type Quotation struct {
	ID              string
	CompanyID       string
	ProjectID       string
	ClientID        string // copied from Project at creation time, never live-resolved — design spec §18
	EstimateID      string // the SPECIFIC finalized Estimate this version was built from — design spec §3
	QuotationNumber string
	Version         int
	Status          QuotationStatus
	Revision        int64

	Currency string // == the source Estimate's Currency, always — design spec §8

	Lines             []QuotationLine
	Subtotal          money.Money // LIVE — recomputed on every line edit, design spec §6.3
	GeneratedSubtotal money.Money // FROZEN at generation time — design spec §1/§6.3, Review Decision 3

	TaxMode    TaxMode
	TaxLabel   string
	TaxRateBPS money.RateBPS
	TaxAmount  money.Money

	Total money.Money // Subtotal + TaxAmount

	Terms           string
	PaymentSchedule string
	ValidUntil      *time.Time

	Notes string // internal-authoring notes, contractor-only — NEVER client-facing, design spec §4/§24

	CreatedAt     time.Time
	FinalizedAt   *time.Time
	SchemaVersion int
}

// QuotationLine is one customer-facing commercial line. It NEVER carries
// any internal cost/margin figure — design spec §4/§5. SourceWorkItemIDs is
// traceability metadata only: empty for a fully contractor-authored line,
// one entry for an unmerged generated line, multiple entries when the
// contractor has merged several generated lines into one grouped/
// fixed-package line. The SAME WorkItemID may appear on more than one line
// simultaneously (a split) — this is not a partition or an ownership claim
// (design spec §1/§5.3).
type QuotationLine struct {
	ID                string
	SourceWorkItemIDs []string
	Description       string
	Quantity          *decimal.Decimal // optional — only meaningful with Unit
	Unit              *string          // optional
	UnitPrice         *money.Money     // optional — only meaningful with Quantity/Unit
	Amount            money.Money      // ALWAYS present, ALWAYS strictly positive — design spec §7.2
	SortOrder         int
}
