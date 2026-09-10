package awards

import (
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// Offer comparison (§8B).
//
// This is a pure read-only projection over immutable Offer Versions and their
// eligibility gates. It owns no collection and persists nothing: a stored
// comparison would be a second source of commercial truth that could drift from
// the versions it was derived from.
//
// It performs no arithmetic beyond summing already-calculated line subtotals,
// and every such sum is labelled indicative. The authoritative figure is
// produced only by F3's CalculateAward.

// ErrComparisonForeignCompany reports an Offer Version snapshot from another
// tenant. CompanyID comes from the authenticated principal, so a mismatch is a
// composition-wiring fault, never data to display.
var ErrComparisonForeignCompany = errors.New(
	"awards: offer version belongs to another company")

// ComparisonLabel explains why a version is not in the default view. Each state
// is distinct so a contractor can tell "the Supplier pulled out" apart from
// "this quote timed out" — they call for different next actions.
type ComparisonLabel string

const (
	ComparisonLabelCurrent            ComparisonLabel = "current"
	ComparisonLabelSuperseded         ComparisonLabel = "superseded"
	ComparisonLabelWithdrawn          ComparisonLabel = "withdrawn"
	ComparisonLabelExpired            ComparisonLabel = "expired"
	ComparisonLabelPreviousRFQVersion ComparisonLabel = "previous_rfq_version"
)

// IndicativeAmount is a comparison-only figure. The flag is part of the type
// rather than documentation so a caller cannot treat it as authoritative
// without discarding a field that says otherwise.
type IndicativeAmount struct {
	Amount     money.Money
	Indicative bool
}

type ComparisonLine struct {
	IssuedRFQLineID    string
	StableLineageID    string
	OfferLineID        string
	ResponseStatus     OfferLineResponse
	UnitPrice          *money.Money
	LineSubtotal       *money.Money
	LineTaxAmount      money.Money
	Brand              string
	SKU                string
	ProductDescription string
	LeadTime           string
	SupplierLineNotes  string
	Exceptions         string
	// Selectable is the projection's view of awardability. Only a positively
	// quoted line on a default-view version qualifies; F3 remains the authority.
	Selectable bool
}

type ComparisonOffer struct {
	OfferVersionID string
	OfferChainID   string
	SupplierID     string
	SupplierName   string
	InvitationID   string
	VersionNumber  int
	Currency       string
	SubmittedAt    time.Time
	ValidUntil     time.Time
	SupplierNotes  string

	Label         ComparisonLabel
	IsDefaultView bool
	Selectable    bool

	Lines []ComparisonLine

	TaxMode                  OfferTaxMode
	OfferLevelTaxAmount      money.Money
	IndicativeQuotedSubtotal IndicativeAmount
	IndicativeGrandTotal     IndicativeAmount

	// PartialAwardUnavailable is D1 surfaced early: an offer_level version must
	// be awarded with every positively quoted line or none, so the interface can
	// warn before F3 refuses a subset.
	PartialAwardUnavailable       bool
	CompleteSelectionOfferLineIDs []string

	// Rank and Recommended exist only to be asserted zero/false. M8 ranks
	// nothing and recommends no winner (§8B); sorting is a contractor action
	// over facts, never a system judgement.
	Rank        int
	Recommended bool
}

type ComparisonInput struct {
	IssuedRFQ     IssuedRFQSnapshot
	OfferVersions []OfferVersionSnapshot
	ObservedAt    time.Time
}

type Comparison struct {
	IssuedRFQVersionID string
	RFQChainID         string
	RFQNumber          string
	VersionNumber      int
	Currency           string
	Offers             []ComparisonOffer

	// Authoritative is always false. It is a field rather than a comment so a
	// consumer that checks before trusting a total gets the right answer.
	Authoritative bool
}

// BuildComparison projects offer versions against the issued RFQ they answer.
func BuildComparison(input ComparisonInput) (Comparison, error) {
	issued := input.IssuedRFQ

	// Every version must belong to the authenticated tenant. This is a fault,
	// not a filter: silently dropping a foreign row would hide the wiring bug
	// that produced it.
	for _, version := range input.OfferVersions {
		if version.CompanyID != issued.CompanyID {
			return Comparison{}, ErrComparisonForeignCompany
		}
	}

	lineageByRFQLine := make(map[string]string, len(issued.Lines))
	for _, line := range issued.Lines {
		lineageByRFQLine[line.ID] = line.LineageID
	}

	offers := make([]ComparisonOffer, 0, len(input.OfferVersions))
	for _, version := range input.OfferVersions {
		offers = append(offers,
			projectOffer(version, issued, lineageByRFQLine, input.ObservedAt))
	}

	return Comparison{
		IssuedRFQVersionID: issued.ID,
		RFQChainID:         issued.RFQChainID,
		RFQNumber:          issued.RFQNumber,
		VersionNumber:      issued.VersionNumber,
		Currency:           issued.Currency,
		Offers:             offers,
		Authoritative:      false,
	}, nil
}

func projectOffer(
	version OfferVersionSnapshot,
	issued IssuedRFQSnapshot,
	lineageByRFQLine map[string]string,
	observedAt time.Time,
) ComparisonOffer {
	label := comparisonLabelFor(version, issued, observedAt)
	inDefaultView := label == ComparisonLabelCurrent

	subtotal := money.New(0, issued.Currency)
	taxTotal := money.New(0, issued.Currency)
	lines := make([]ComparisonLine, 0, len(version.Lines))
	var completeSelection []string

	for _, line := range version.Lines {
		quoted := line.ResponseStatus == OfferLineQuoted
		if quoted {
			// Only positively quoted lines participate in D1 completeness;
			// declines are never selectable, so requiring them would make the
			// completeness set unsatisfiable.
			completeSelection = append(completeSelection, line.ID)
			if line.LineSubtotalExcludingTax != nil {
				subtotal.Amount += line.LineSubtotalExcludingTax.Amount
			}
			taxTotal.Amount += line.LineTaxAmount.Amount
		}

		lines = append(lines, ComparisonLine{
			IssuedRFQLineID:    line.RFQLineID,
			StableLineageID:    lineageByRFQLine[line.RFQLineID],
			OfferLineID:        line.ID,
			ResponseStatus:     line.ResponseStatus,
			UnitPrice:          line.UnitPriceExcludingTax,
			LineSubtotal:       line.LineSubtotalExcludingTax,
			LineTaxAmount:      line.LineTaxAmount,
			Brand:              line.Brand,
			SKU:                line.SKU,
			ProductDescription: line.ProductDescription,
			LeadTime:           line.LeadTime,
			SupplierLineNotes:  line.SupplierLineNotes,
			Exceptions:         line.CommercialExceptions,
			Selectable:         quoted && inDefaultView,
		})
	}

	// The indicative grand total mirrors what the Supplier quoted for the whole
	// offer. It is NOT the award figure: F3 re-evaluates conditional groups and
	// delivery against the actually selected subset.
	// This is the Supplier's OWN submitted whole-offer total, read from the
	// frozen version rather than re-derived. Re-deriving it here would mean
	// summing charge rules the projection has no selected subset to evaluate
	// against, which is precisely F3's job.
	grandTotal := version.GrandTotal
	if grandTotal.Currency == "" {
		grandTotal = money.New(0, issued.Currency)
	}

	offerLevel := version.Tax.Mode == OfferTaxOfferLevel
	if !offerLevel {
		completeSelection = nil
	}

	return ComparisonOffer{
		OfferVersionID:                version.ID,
		OfferChainID:                  version.OfferChainID,
		SupplierID:                    version.SupplierID,
		SupplierName:                  version.SupplierName,
		InvitationID:                  version.InvitationID,
		VersionNumber:                 version.VersionNumber,
		Currency:                      version.Currency,
		SubmittedAt:                   version.SubmittedAt,
		ValidUntil:                    version.OfferValidUntil,
		SupplierNotes:                 version.SupplierNotes,
		Label:                         label,
		IsDefaultView:                 inDefaultView,
		Selectable:                    inDefaultView,
		Lines:                         lines,
		TaxMode:                       version.Tax.Mode,
		OfferLevelTaxAmount:           version.OfferLevelTaxAmount,
		IndicativeQuotedSubtotal:      IndicativeAmount{Amount: subtotal, Indicative: true},
		IndicativeGrandTotal:          IndicativeAmount{Amount: grandTotal, Indicative: true},
		PartialAwardUnavailable:       offerLevel,
		CompleteSelectionOfferLineIDs: completeSelection,
	}
}

// comparisonLabelFor classifies a version. The order is deliberate: a version
// answering an earlier RFQ version is labelled as such even if it is also
// expired, because "you are looking at an old RFQ" is the more useful fact.
func comparisonLabelFor(
	version OfferVersionSnapshot,
	issued IssuedRFQSnapshot,
	observedAt time.Time,
) ComparisonLabel {
	if version.IssuedRFQVersionID != issued.ID {
		return ComparisonLabelPreviousRFQVersion
	}
	switch version.EligibilityState {
	case OfferEligibilityWithdrawn, OfferEligibilityWithdrawalClaimed:
		return ComparisonLabelWithdrawn
	}
	if !version.OfferValidUntil.After(observedAt) {
		return ComparisonLabelExpired
	}
	if !version.IsLatestSubmitted {
		return ComparisonLabelSuperseded
	}
	return ComparisonLabelCurrent
}
