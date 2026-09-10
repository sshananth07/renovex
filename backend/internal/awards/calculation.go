package awards

import (
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// Authoritative selected-line calculation (§8D).
//
// The browser never supplies an Award total. Every figure is derived here from
// immutable Offer Version snapshots and the immutable issued RFQ version.
//
// Tax, conditional charges and delivery run through
// internal/foundation/procurementcalc — the SAME kernel that produced the
// Supplier's submitted figures in Phase E. Reimplementing that arithmetic here
// would let a submitted Offer total and an Award total for the same lines drift
// apart, which is a commercial correctness defect, and would violate ADR 0001's
// single-rounding rule.

// AwardLineSelection is one decision fed to the calculation. It is the
// calculation-facing form of an AwardLineDecisionDraft.
type AwardLineSelection struct {
	IssuedRFQLineID string
	StableLineageID string

	// Selected fields.
	OfferVersionID string
	OfferLineID    string

	// Unawarded fields. Unawarded lines contribute nothing but are recorded,
	// because "we deliberately did not buy this" is a decision worth
	// preserving and is what makes retender_required auditable later.
	Unawarded       bool
	UnawardedReason UnawardedReason
	UnawardedNote   string
}

// AwardedLine references the exact issued line, lineage, Supplier, Invitation,
// Offer Version and Offer line the award commits to.
type AwardedLine struct {
	IssuedRFQLineID string
	StableLineageID string
	MaterialName    string
	Quantity        quantity.Quantity

	SupplierID     string
	SupplierName   string
	InvitationID   string
	OfferVersionID string
	OfferLineID    string

	UnitPriceExcludingTax money.Money
	LineSubtotal          money.Money
	LineTaxAmount         money.Money
	Brand                 string
	SKU                   string
	ProductDescription    string
	LeadTime              string
}

type UnawardedLine struct {
	IssuedRFQLineID string
	StableLineageID string
	MaterialName    string
	Reason          UnawardedReason
	Note            string
}

// AppliedChargeGroup records a group that was re-evaluated against the awarded
// subset, including groups that did NOT trigger: "this charge did not apply"
// is part of explaining the total.
type AppliedChargeGroup struct {
	GroupID       string
	Name          string
	Triggered     bool
	GroupSubtotal money.Money
	Amount        money.Money
}

// AwardSupplierSummary is one Offer Version's contribution to the award.
type AwardSupplierSummary struct {
	SupplierID     string
	SupplierName   string
	InvitationID   string
	OfferVersionID string
	OfferChainID   string

	AwardedLineIDs []string
	LineSubtotal   money.Money
	TaxTotal       money.Money
	ChargeGroups   []AppliedChargeGroup
	ChargeTotal    money.Money
	DeliveryCharge money.Money
	SupplierTotal  money.Money
}

type AwardCalculationInput struct {
	IssuedRFQ     IssuedRFQSnapshot
	Selections    []AwardLineSelection
	OfferVersions map[string]OfferVersionSnapshot
	CalculatedAt  time.Time
}

type AwardCalculation struct {
	AwardedLines         []AwardedLine
	UnawardedLines       []UnawardedLine
	SupplierSummaries    []AwardSupplierSummary
	GrandAwardTotal      money.Money
	SelectionFingerprint string
}

// CalculateAward validates every selection and produces the authoritative
// award figures.
func CalculateAward(input AwardCalculationInput) (AwardCalculation, error) {
	return calculateAward(input, nil)
}

// calculateAward is the shared engine. baseline is nil for an initial award;
// F6 supplies the frozen baseline contributions for a correction (§8G).
func calculateAward(
	input AwardCalculationInput,
	baseline map[string]money.Money,
) (AwardCalculation, error) {
	issued := input.IssuedRFQ
	issuedLines := make(map[string]IssuedRFQLineSnapshot, len(issued.Lines))
	for _, line := range issued.Lines {
		issuedLines[line.ID] = line
	}

	// Group the selections by Offer Version. Delivery and conditional groups
	// are per-VERSION quantities, so they cannot be evaluated line by line.
	type versionSelection struct {
		version    OfferVersionSnapshot
		selections []AwardLineSelection
	}
	byVersion := map[string]*versionSelection{}
	var versionOrder []string

	awarded := make([]AwardedLine, 0, len(input.Selections))
	unawarded := make([]UnawardedLine, 0, len(input.Selections))
	seenLines := make(map[string]bool, len(input.Selections))

	for _, selection := range input.Selections {
		// One issued line carries exactly one decision. Two selections for one
		// lineage would both claim it, which F4's unique index cannot resolve.
		if seenLines[selection.IssuedRFQLineID] {
			return AwardCalculation{}, ErrInvalidAwardDecision
		}
		seenLines[selection.IssuedRFQLineID] = true

		issuedLine, known := issuedLines[selection.IssuedRFQLineID]
		if !known {
			// Awarding a line the Supplier was never asked to quote would
			// produce a commitment with no authoritative source.
			return AwardCalculation{}, ErrOfferVersionNotSelectable
		}

		if selection.Unawarded {
			if err := ValidateUnawardedReason(
				selection.UnawardedReason, selection.UnawardedNote); err != nil {
				return AwardCalculation{}, err
			}
			unawarded = append(unawarded, UnawardedLine{
				IssuedRFQLineID: selection.IssuedRFQLineID,
				StableLineageID: issuedLine.LineageID,
				MaterialName:    issuedLine.MaterialName,
				Reason:          selection.UnawardedReason,
				Note:            selection.UnawardedNote,
			})
			continue
		}

		version, supplied := input.OfferVersions[selection.OfferVersionID]
		if !supplied {
			// F3 never guesses a missing commercial input.
			return AwardCalculation{}, ErrOfferVersionNotSelectable
		}
		if err := validateSelectableVersion(
			version, issued, input.CalculatedAt, baseline != nil); err != nil {
			return AwardCalculation{}, err
		}

		offerLine, err := selectableOfferLine(version, selection, issuedLine)
		if err != nil {
			return AwardCalculation{}, err
		}

		if _, tracked := byVersion[version.ID]; !tracked {
			byVersion[version.ID] = &versionSelection{version: version}
			versionOrder = append(versionOrder, version.ID)
		}
		byVersion[version.ID].selections = append(
			byVersion[version.ID].selections, selection)

		awarded = append(awarded, AwardedLine{
			IssuedRFQLineID:       selection.IssuedRFQLineID,
			StableLineageID:       issuedLine.LineageID,
			MaterialName:          issuedLine.MaterialName,
			Quantity:              issuedLine.Quantity,
			SupplierID:            version.SupplierID,
			SupplierName:          version.SupplierName,
			InvitationID:          version.InvitationID,
			OfferVersionID:        version.ID,
			OfferLineID:           offerLine.ID,
			UnitPriceExcludingTax: derefMoney(offerLine.UnitPriceExcludingTax, issued.Currency),
			LineSubtotal:          derefMoney(offerLine.LineSubtotalExcludingTax, issued.Currency),
			Brand:                 offerLine.Brand,
			SKU:                   offerLine.SKU,
			ProductDescription:    offerLine.ProductDescription,
			LeadTime:              offerLine.LeadTime,
		})
	}

	// Evaluate each selected version once, in a deterministic order so the
	// fingerprint does not depend on map iteration.
	sort.Strings(versionOrder)
	summaries := make([]AwardSupplierSummary, 0, len(versionOrder))
	grandTotal := money.New(0, issued.Currency)
	taxByLine := map[string]money.Money{}

	for _, versionID := range versionOrder {
		entry := byVersion[versionID]
		summary, lineTax, err := calculateVersionContribution(
			entry.version, entry.selections, issued.Currency)
		if err != nil {
			return AwardCalculation{}, err
		}

		// A correction charges only the increment over the frozen baseline, so
		// delivery is paid once and percentage groups re-trigger correctly
		// (§8G). The cumulative figure must never fall below the baseline.
		contribution := summary.SupplierTotal
		if baseline != nil {
			frozen, existed := baseline[versionID]
			if existed {
				if contribution.Amount < frozen.Amount {
					return AwardCalculation{}, ErrAwardCorrectionNotMonotonic
				}
				contribution = money.New(
					contribution.Amount-frozen.Amount, issued.Currency)
			}
		}

		for lineID, amount := range lineTax {
			taxByLine[lineID] = amount
		}
		summaries = append(summaries, summary)
		total, err := grandTotal.Add(contribution)
		if err != nil {
			return AwardCalculation{}, ErrCurrencyMismatch
		}
		grandTotal = total
	}

	// Attach each line's recalculated tax to its awarded line, so the revision
	// records what was actually charged rather than the submission-time figure.
	for index := range awarded {
		if amount, ok := taxByLine[awarded[index].IssuedRFQLineID]; ok {
			awarded[index].LineTaxAmount = amount
		} else {
			awarded[index].LineTaxAmount = money.New(0, issued.Currency)
		}
	}

	calculation := AwardCalculation{
		AwardedLines:      awarded,
		UnawardedLines:    unawarded,
		SupplierSummaries: summaries,
		GrandAwardTotal:   grandTotal,
	}
	calculation.SelectionFingerprint = selectionFingerprint(issued, calculation)
	return calculation, nil
}

// validateSelectableVersion applies §8D validations 1-4 and 7 in order:
// identity before money, so an invalid selection never reaches arithmetic.
//
// baselineCopy marks a correction's LOCKED BASELINE, which is deliberately NOT
// revalidated against gate state, expiry or LatestSubmittedID: an already
// awarded version legitimately has a terminal gate, may be superseded and may
// be expired, and revalidating it would reject exactly the monotonic
// corrections F6 exists to permit (§8G).
func validateSelectableVersion(
	version OfferVersionSnapshot,
	issued IssuedRFQSnapshot,
	calculatedAt time.Time,
	baselineCopy bool,
) error {
	// 1: belongs to this Company and this issued RFQ version.
	if version.CompanyID != issued.CompanyID ||
		version.IssuedRFQVersionID != issued.ID {
		return ErrOfferVersionNotSelectable
	}
	// 7: currency. Combining currencies would produce a meaningless total.
	if version.Currency != issued.Currency {
		return ErrCurrencyMismatch
	}
	if baselineCopy {
		return nil
	}
	// 2: the current submitted version for its invitation.
	if !version.IsLatestSubmitted {
		return ErrOfferVersionNotSelectable
	}
	// 3: the eligibility gate is `eligible`, not withdrawn or claimed.
	if version.EligibilityState != OfferEligibilityEligible {
		return ErrOfferVersionNotEligible
	}
	// 4: unexpired — validity must be STRICTLY later than the calculation.
	if !version.OfferValidUntil.After(calculatedAt) {
		return ErrOfferVersionExpired
	}
	return nil
}

// selectableOfferLine applies §8D validations 5, 6 and 8.
func selectableOfferLine(
	version OfferVersionSnapshot,
	selection AwardLineSelection,
	issuedLine IssuedRFQLineSnapshot,
) (OfferLineSnapshot, error) {
	for _, line := range version.Lines {
		if line.ID != selection.OfferLineID {
			continue
		}
		if line.RFQLineID != selection.IssuedRFQLineID {
			return OfferLineSnapshot{}, ErrOfferVersionNotSelectable
		}
		// 5: only a positively quoted line may be awarded. A decline is an
		// answer, not an offer.
		if line.ResponseStatus != OfferLineQuoted {
			return OfferLineSnapshot{}, ErrOfferLineNotQuoted
		}
		// 6 and 8: the FULL line is awarded at the exact issued quantity and
		// unit. M8 never splits a line's quantity, so a differing quantity is
		// not a partial award — it is a different commitment.
		// Compared exactly as Phase E compares them (supplieroffers/offer.go):
		// decimal value equality plus an exact unit match.
		if line.QuotedQuantity == nil ||
			!line.QuotedQuantity.Value.Equal(issuedLine.Quantity.Value) ||
			line.QuotedQuantity.Unit != issuedLine.Quantity.Unit {
			return OfferLineSnapshot{}, ErrQuantityOrUnitMismatch
		}
		if line.LineSubtotalExcludingTax == nil {
			return OfferLineSnapshot{}, ErrOfferLineNotQuoted
		}
		return line, nil
	}
	return OfferLineSnapshot{}, ErrOfferVersionNotSelectable
}

// calculateVersionContribution recalculates one Offer Version's contribution
// over the awarded subset, using the shared kernel throughout.
func calculateVersionContribution(
	version OfferVersionSnapshot,
	selections []AwardLineSelection,
	currency string,
) (AwardSupplierSummary, map[string]money.Money, error) {
	selected := make(map[string]bool, len(selections))
	for _, selection := range selections {
		selected[selection.IssuedRFQLineID] = true
	}

	// Every POSITIVELY QUOTED line of the version, in its own order. The kernel
	// needs the full quoted set to evaluate group membership and D1
	// completeness, not just the awarded subset.
	var quotedLineIDs []string
	var taxable []procurementcalc.TaxableQuotedLine
	var quotedSubtotals []procurementcalc.QuotedLineSubtotal
	var selectedIDs []string
	lineSubtotal := money.New(0, currency)

	for _, line := range version.Lines {
		if line.ResponseStatus != OfferLineQuoted ||
			line.LineSubtotalExcludingTax == nil {
			continue
		}
		quotedLineIDs = append(quotedLineIDs, line.RFQLineID)
		quotedSubtotals = append(quotedSubtotals, procurementcalc.QuotedLineSubtotal{
			RFQLineID: line.RFQLineID,
			Subtotal:  *line.LineSubtotalExcludingTax,
		})
		if !selected[line.RFQLineID] {
			continue
		}
		selectedIDs = append(selectedIDs, line.RFQLineID)
		total, err := lineSubtotal.Add(*line.LineSubtotalExcludingTax)
		if err != nil {
			return AwardSupplierSummary{}, nil, ErrCurrencyMismatch
		}
		lineSubtotal = total
		// Tax is recalculated over the AWARDED lines only, so the awarded tax
		// matches what the Supplier quoted for those exact lines.
		taxable = append(taxable, procurementcalc.TaxableQuotedLine{
			RFQLineID: line.RFQLineID,
			Subtotal:  *line.LineSubtotalExcludingTax,
			Tax:       line.LineTax,
		})
	}

	// D1: an offer_level version is all-or-nothing. Apportioning would fabricate
	// a figure no Supplier submitted; omitting would understate a commitment.
	if err := procurementcalc.ValidateTaxSelection(
		version.Tax.Mode, quotedLineIDs, selectedIDs); err != nil {
		return AwardSupplierSummary{}, nil, mapKernelError(err)
	}

	taxResult, err := procurementcalc.CalculateTax(procurementcalc.TaxCalculationInput{
		Currency: currency, Tax: version.Tax, QuotedLines: taxable,
	})
	if err != nil {
		return AwardSupplierSummary{}, nil, mapKernelError(err)
	}

	// Groups are RE-EVALUATED against the selected set, never copied: a group's
	// trigger and its percentage base both depend on which lines are actually
	// awarded, so the submission-time amount would attribute a charge to a line
	// set that was never awarded.
	chargeResult, err := procurementcalc.CalculateConditionalCharges(
		procurementcalc.ConditionalChargeInput{
			Currency:           currency,
			QuotedLines:        quotedSubtotals,
			SelectedRFQLineIDs: selectedIDs,
			Groups:             version.ChargeGroups,
		})
	if err != nil {
		return AwardSupplierSummary{}, nil, ErrConditionalChargesNotResolvable
	}

	// Delivery applies ONCE per Offer Version, and only when at least one line
	// is awarded. Two Suppliers means two delivery charges: each quoted one for
	// their own delivery.
	delivery, err := procurementcalc.CalculateDeliveryCharge(
		currency, version.DeliveryCharge, len(selectedIDs))
	if err != nil {
		return AwardSupplierSummary{}, nil, mapKernelError(err)
	}

	groupNames := make(map[string]string, len(version.ChargeGroups))
	for _, group := range version.ChargeGroups {
		groupNames[group.ID] = group.Name
	}
	applied := make([]AppliedChargeGroup, 0, len(chargeResult.Groups))
	for _, group := range chargeResult.Groups {
		applied = append(applied, AppliedChargeGroup{
			GroupID:       group.GroupID,
			Name:          groupNames[group.GroupID],
			Triggered:     group.Triggered,
			GroupSubtotal: group.GroupSubtotal,
			Amount:        group.Amount,
		})
	}

	supplierTotal := money.New(
		lineSubtotal.Amount+taxResult.Total.Amount+
			chargeResult.Total.Amount+delivery.Amount,
		currency)

	lineTax := make(map[string]money.Money, len(taxResult.Lines))
	for _, line := range taxResult.Lines {
		lineTax[line.RFQLineID] = line.Amount
	}

	sort.Strings(selectedIDs)
	return AwardSupplierSummary{
		SupplierID:     version.SupplierID,
		SupplierName:   version.SupplierName,
		InvitationID:   version.InvitationID,
		OfferVersionID: version.ID,
		OfferChainID:   version.OfferChainID,
		AwardedLineIDs: selectedIDs,
		LineSubtotal:   lineSubtotal,
		TaxTotal:       taxResult.Total,
		ChargeGroups:   applied,
		ChargeTotal:    chargeResult.Total,
		DeliveryCharge: delivery,
		SupplierTotal:  supplierTotal,
	}, lineTax, nil
}

// mapKernelError translates a shared-kernel failure into the bounded Phase F
// sentinel §8D fixes, so the contractor learns what to fix.
func mapKernelError(err error) error {
	switch {
	case err == nil:
		return nil
	case errorsIs(err, procurementcalc.ErrOfferLevelTaxRequiresCompleteSelection):
		return ErrOfferLevelTaxRequiresComplete
	case errorsIs(err, procurementcalc.ErrTaxCurrencyMismatch),
		errorsIs(err, procurementcalc.ErrUnsupportedCurrency):
		return ErrCurrencyMismatch
	case errorsIs(err, procurementcalc.ErrInvalidConditionalCharge):
		return ErrConditionalChargesNotResolvable
	case errorsIs(err, procurementcalc.ErrInvalidDeliveryCharge):
		return ErrConditionalChargesNotResolvable
	default:
		// Any other kernel refusal is a tax-shape problem with the Supplier's
		// own submitted data, which cannot be awarded as-is.
		return ErrOfferLineNotQuoted
	}
}

func derefMoney(amount *money.Money, currency string) money.Money {
	if amount == nil {
		return money.New(0, currency)
	}
	return *amount
}

// selectionFingerprint is a canonical SHA-256 over the award's commercial
// content.
//
// It is declared inline as a canonical string rather than derived from a
// persisted document or DTO, so a later change to either cannot silently change
// the identity of an award. It EXCLUDES timestamps, actor identity and
// selection order, so a retry of the same decisions is recognisably the same
// award — which is what lets F5 adopt an existing revision instead of
// re-numbering one.
func selectionFingerprint(
	issued IssuedRFQSnapshot,
	calculation AwardCalculation,
) string {
	lines := make([]string, 0,
		len(calculation.AwardedLines)+len(calculation.UnawardedLines))

	for _, line := range calculation.AwardedLines {
		lines = append(lines, fmt.Sprintf(
			"awarded|%s|%s|%s|%s|%s|%d|%d",
			line.IssuedRFQLineID, line.StableLineageID,
			line.SupplierID, line.OfferVersionID, line.OfferLineID,
			line.LineSubtotal.Amount, line.LineTaxAmount.Amount))
	}
	for _, line := range calculation.UnawardedLines {
		lines = append(lines, fmt.Sprintf(
			"unawarded|%s|%s|%s|%s",
			line.IssuedRFQLineID, line.StableLineageID,
			line.Reason, strings.TrimSpace(line.Note)))
	}
	for _, summary := range calculation.SupplierSummaries {
		lines = append(lines, fmt.Sprintf(
			"supplier|%s|%s|%d|%d|%d|%d|%d",
			summary.SupplierID, summary.OfferVersionID,
			summary.LineSubtotal.Amount, summary.TaxTotal.Amount,
			summary.ChargeTotal.Amount, summary.DeliveryCharge.Amount,
			summary.SupplierTotal.Amount))
	}

	// Sorting makes the fingerprint independent of decision order: the same
	// decisions listed differently are the same award.
	sort.Strings(lines)

	digest := sha256.New()
	fmt.Fprintf(digest, "company|%s\nissued|%s\ncurrency|%s\ntotal|%d\n",
		issued.CompanyID, issued.ID, issued.Currency,
		calculation.GrandAwardTotal.Amount)
	for _, line := range lines {
		digest.Write([]byte(line))
		digest.Write([]byte("\n"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// errorsIs is a thin indirection so this file declares its intent to classify
// kernel errors without importing errors alongside a same-named local helper.
func errorsIs(err, target error) bool { return stderrors.Is(err, target) }
