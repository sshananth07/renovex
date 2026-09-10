package supplieroffers

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

const SupplierOfferVersionSchemaVersion = 1

// SupplierOfferVersion is an immutable snapshot produced only from a claimed
// draft. E3 begins with the identity and provenance fields that participate in
// persistence safeguards; commercial snapshots are added under their own
// calculation and submission tests.
type SupplierOfferVersion struct {
	ID                    string
	CompanyID             string
	OfferChainID          string
	InvitationID          string
	IssuedRFQVersionID    string
	VersionNumber         int
	Currency              string
	RecipientIdentity     string
	SourceDraftID         string
	SourceDraftRevision   int64
	SubmissionOperationID string
	SubmissionFingerprint string

	Lines          []SupplierOfferLine
	Tax            SupplierOfferTax
	ChargeGroups   []ConditionalChargeGroup
	DeliveryCharge *DeliveryCharge

	QuotedLineSubtotal   money.Money
	QuotedTaxTotal       money.Money
	FullOfferChargeTotal money.Money
	DeliveryChargeTotal  money.Money
	GrandTotal           money.Money

	OfferValidUntil time.Time
	SupplierNotes   string
	SubmittedAt     time.Time
	SchemaVersion   int
}

// SupplierOfferLine is the immutable submitted response for one authoritative
// RFQ line. Calculated amounts are stored beside quoted inputs for auditability
// but are always rebuilt by the submission service before insertion.
type SupplierOfferLine struct {
	ID                       string
	RFQLineID                string
	ResponseStatus           OfferLineResponseStatus
	QuotedQuantity           *quantity.Quantity
	UnitPriceExcludingTax    *money.Money
	LineSubtotalExcludingTax *money.Money
	Brand                    string
	SKU                      string
	ProductDescription       string
	LeadTime                 string
	SupplierLineNotes        string
	CommercialExceptions     string
	LineTax                  *QuotedLineTax
	LineTaxAmount            money.Money
	CopiedFromOfferVersionID string
	CopiedFromOfferLineID    string
	CopiedAt                 *time.Time
}
