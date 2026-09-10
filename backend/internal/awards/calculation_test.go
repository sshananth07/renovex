package awards

import (
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// F3 authoritative calculation (§8D).
//
// The browser never supplies an Award total. Every figure here is derived from
// immutable Offer Version snapshots and the immutable issued RFQ version,
// through the SAME procurementcalc kernel Phase E used to calculate the
// Supplier's submission.

var calcAt = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func calcIssued(t *testing.T) IssuedRFQSnapshot {
	t.Helper()
	return IssuedRFQSnapshot{
		ID: "issued-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
		RFQNumber: "RFQ-0001", VersionNumber: 1, Currency: "MYR",
		Lines: []IssuedRFQLineSnapshot{
			{ID: "line-1", LineageID: "lineage-1", MaterialName: "Tile",
				Quantity: mustQuantity(t, "10", "sqm")},
			{ID: "line-2", LineageID: "lineage-2", MaterialName: "Grout",
				Quantity: mustQuantity(t, "5", "bag")},
		},
	}
}

func calcLine(offerLineID, rfqLineID string, qty quantity.Quantity,
	unitMinor, subtotalMinor int64) OfferLineSnapshot {
	unit := money.New(unitMinor, "MYR")
	subtotal := money.New(subtotalMinor, "MYR")
	return OfferLineSnapshot{
		ID: offerLineID, RFQLineID: rfqLineID,
		ResponseStatus:           OfferLineQuoted,
		QuotedQuantity:           &qty,
		UnitPriceExcludingTax:    &unit,
		LineSubtotalExcludingTax: &subtotal,
		LineTaxAmount:            money.New(0, "MYR"),
	}
}

func calcVersion(t *testing.T, id, supplier string,
	lines []OfferLineSnapshot) OfferVersionSnapshot {
	t.Helper()
	return OfferVersionSnapshot{
		ID: id, CompanyID: "company-1", OfferChainID: "offerchain-" + supplier,
		SupplierID: supplier, SupplierName: supplier,
		InvitationID:       "invitation-" + supplier,
		IssuedRFQVersionID: "issued-1", VersionNumber: 1, Currency: "MYR",
		Lines:              lines,
		Tax:                OfferTaxRule{Mode: OfferTaxNotApplicable},
		OfferValidUntil:    calcAt.Add(24 * time.Hour),
		SubmittedAt:        calcAt.Add(-24 * time.Hour),
		IsLatestSubmitted:  true,
		EligibilityState:   OfferEligibilityEligible,
		QuotedLineSubtotal: money.New(0, "MYR"),
		QuotedTaxTotal:     money.New(0, "MYR"),
		GrandTotal:         money.New(0, "MYR"),
	}
}

func selection(rfqLineID, lineageID, versionID, offerLineID string) AwardLineSelection {
	return AwardLineSelection{
		IssuedRFQLineID: rfqLineID, StableLineageID: lineageID,
		OfferVersionID: versionID, OfferLineID: offerLineID,
	}
}

func calcInput(t *testing.T,
	selections []AwardLineSelection,
	versions ...OfferVersionSnapshot) AwardCalculationInput {
	t.Helper()
	byID := make(map[string]OfferVersionSnapshot, len(versions))
	for _, version := range versions {
		byID[version.ID] = version
	}
	return AwardCalculationInput{
		IssuedRFQ: calcIssued(t), Selections: selections,
		OfferVersions: byID, CalculatedAt: calcAt,
	}
}

// The happy path: one selected line produces that version's own frozen subtotal
// and nothing else.
func TestCalculateAwardSumsSelectedLineSubtotals(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})

	calculation, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if calculation.GrandAwardTotal != money.New(10_000, "MYR") {
		t.Fatalf("grand total = %+v, want 10000 MYR", calculation.GrandAwardTotal)
	}
	if len(calculation.AwardedLines) != 1 {
		t.Fatalf("awarded lines = %d, want 1", len(calculation.AwardedLines))
	}
	awarded := calculation.AwardedLines[0]
	if awarded.SupplierID != "supplier-a" || awarded.StableLineageID != "lineage-1" {
		t.Errorf("awarded line = %+v, want supplier-a / lineage-1", awarded)
	}
}

// Validation 1: an offer version from another Company or answering a different
// issued version is not selectable. Identity is checked before money, so an
// invalid selection never reaches arithmetic.
func TestCalculateAwardRejectsAForeignOrMismatchedOfferVersion(t *testing.T) {
	foreign := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	foreign.CompanyID = "company-2"

	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		foreign)); !errors.Is(err, ErrOfferVersionNotSelectable) {
		t.Fatalf("foreign company err = %v, want ErrOfferVersionNotSelectable", err)
	}

	otherIssued := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	otherIssued.IssuedRFQVersionID = "issued-0"

	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		otherIssued)); !errors.Is(err, ErrOfferVersionNotSelectable) {
		t.Fatalf("other issued version err = %v, want ErrOfferVersionNotSelectable", err)
	}
}

// Validation 2: a superseded version is not selectable. Awarding it would
// commit to a quote the Supplier has already replaced.
func TestCalculateAwardRejectsASupersededOfferVersion(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.IsLatestSubmitted = false

	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		version)); !errors.Is(err, ErrOfferVersionNotSelectable) {
		t.Fatalf("err = %v, want ErrOfferVersionNotSelectable", err)
	}
}

// Validation 3: the eligibility gate must be `eligible`. A withdrawn offer or
// one claimed by another finalisation is a 409 the caller can act on.
func TestCalculateAwardRejectsAnIneligibleOfferVersion(t *testing.T) {
	for _, state := range []OfferEligibilityState{
		OfferEligibilityWithdrawn,
		OfferEligibilityWithdrawalClaimed,
		OfferEligibilityAwardClaimed,
		OfferEligibilityAwarded,
	} {
		t.Run(string(state), func(t *testing.T) {
			version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
				calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
			version.EligibilityState = state

			if _, err := CalculateAward(calcInput(t,
				[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
				version)); !errors.Is(err, ErrOfferVersionNotEligible) {
				t.Fatalf("err = %v, want ErrOfferVersionNotEligible", err)
			}
		})
	}
}

// Validation 4: validity must be STRICTLY later than the calculation time. An
// offer expiring exactly now has expired.
func TestCalculateAwardRejectsAnExpiredOfferVersion(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.OfferValidUntil = calcAt

	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		version)); !errors.Is(err, ErrOfferVersionExpired) {
		t.Fatalf("err = %v, want ErrOfferVersionExpired", err)
	}
}

// Validation 5: only a positively quoted line may be awarded. A decline is an
// answer, not an offer.
func TestCalculateAwardRejectsANonQuotedOfferLine(t *testing.T) {
	for _, status := range []OfferLineResponse{
		OfferLineNoBid, OfferLineUnavailable, OfferLineUnanswered,
	} {
		t.Run(string(status), func(t *testing.T) {
			line := calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)
			line.ResponseStatus = status
			version := calcVersion(t, "offer-1", "supplier-a",
				[]OfferLineSnapshot{line})

			if _, err := CalculateAward(calcInput(t,
				[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
				version)); !errors.Is(err, ErrOfferLineNotQuoted) {
				t.Fatalf("err = %v, want ErrOfferLineNotQuoted", err)
			}
		})
	}
}

// Validation 6: quantity AND unit must match the issued line exactly. M8 never
// splits a line's quantity, so a partial quantity is not a partial award — it
// is a different commitment.
func TestCalculateAwardRejectsAQuantityOrUnitMismatch(t *testing.T) {
	wrongQuantity := calcLine("ol-1", "line-1",
		mustQuantity(t, "9", "sqm"), 1000, 9_000)
	version := calcVersion(t, "offer-1", "supplier-a",
		[]OfferLineSnapshot{wrongQuantity})
	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		version)); !errors.Is(err, ErrQuantityOrUnitMismatch) {
		t.Fatalf("quantity err = %v, want ErrQuantityOrUnitMismatch", err)
	}

	wrongUnit := calcLine("ol-1", "line-1",
		mustQuantity(t, "10", "box"), 1000, 10_000)
	version = calcVersion(t, "offer-1", "supplier-a",
		[]OfferLineSnapshot{wrongUnit})
	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		version)); !errors.Is(err, ErrQuantityOrUnitMismatch) {
		t.Fatalf("unit err = %v, want ErrQuantityOrUnitMismatch", err)
	}
}

// Validation 7: currency must match the issued RFQ. Combining currencies would
// produce a total that means nothing.
func TestCalculateAwardRejectsACurrencyMismatch(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.Currency = "SGD"

	if _, err := CalculateAward(calcInput(t,
		[]AwardLineSelection{selection("line-1", "lineage-1", "offer-1", "ol-1")},
		version)); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

// D1: an offer_level version awarded with EVERY positively quoted line carries
// its full quoted tax figure — never apportioned, never omitted.
func TestCalculateAwardAcceptsCompleteOfferLevelSelection(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	taxAmount := money.New(750, "MYR")
	version.Tax = OfferTaxRule{
		Mode: OfferTaxOfferLevel,
		OfferLevel: &procurementcalc.QuotedOfferTax{
			TaxType:   procurementcalc.TaxTypeOther,
			TaxAmount: taxAmount, BasisNote: "flat service tax",
		},
	}

	calculation, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		selection("line-2", "lineage-2", "offer-1", "ol-2"),
	}, version))
	if err != nil {
		t.Fatalf("complete offer_level selection must succeed: %v", err)
	}
	// 10000 + 2500 lines, plus the FULL quoted 750 tax.
	if calculation.GrandAwardTotal != money.New(13_250, "MYR") {
		t.Fatalf("grand total = %+v, want 13250 MYR", calculation.GrandAwardTotal)
	}
}

// D1: a strict subset of an offer_level version is refused, naming the missing
// lines so the contractor learns what to fix.
func TestCalculateAwardRejectsStrictSubsetOfOfferLevelVersion(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	taxAmount := money.New(750, "MYR")
	version.Tax = OfferTaxRule{
		Mode: OfferTaxOfferLevel,
		OfferLevel: &procurementcalc.QuotedOfferTax{
			TaxType:   procurementcalc.TaxTypeOther,
			TaxAmount: taxAmount, BasisNote: "flat service tax",
		},
	}

	_, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
	}, version))
	if !errors.Is(err, ErrOfferLevelTaxRequiresComplete) {
		t.Fatalf("err = %v, want ErrOfferLevelTaxRequiresComplete", err)
	}
}

// Two Suppliers means TWO delivery charges: each quoted one for their own
// delivery, and neither absorbs the other's.
func TestCalculateAwardAppliesOneDeliveryChargePerSelectedVersion(t *testing.T) {
	first := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	first.DeliveryCharge = &DeliveryRule{Amount: money.New(3_000, "MYR")}

	second := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500)})
	second.DeliveryCharge = &DeliveryRule{Amount: money.New(2_000, "MYR")}

	calculation, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		selection("line-2", "lineage-2", "offer-2", "ol-2"),
	}, first, second))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	// 10000 + 3000 + 2500 + 2000
	if calculation.GrandAwardTotal != money.New(17_500, "MYR") {
		t.Fatalf("grand total = %+v, want 17500 MYR (two delivery charges)",
			calculation.GrandAwardTotal)
	}
	if len(calculation.SupplierSummaries) != 2 {
		t.Fatalf("supplier summaries = %d, want 2",
			len(calculation.SupplierSummaries))
	}
}

// Delivery is charged ONCE per version even when several of its lines are
// awarded — the Supplier quoted one delivery, not one per line.
func TestCalculateAwardChargesDeliveryOncePerVersionAcrossManyLines(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	version.DeliveryCharge = &DeliveryRule{Amount: money.New(3_000, "MYR")}

	calculation, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		selection("line-2", "lineage-2", "offer-1", "ol-2"),
	}, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if calculation.GrandAwardTotal != money.New(15_500, "MYR") {
		t.Fatalf("grand total = %+v, want 15500 MYR (one delivery charge)",
			calculation.GrandAwardTotal)
	}
}

// A conditional group is RE-EVALUATED against the selected subset, not copied.
// A group needing all its lines must not trigger when only one is awarded.
func TestCalculateAwardReEvaluatesConditionalGroupsAgainstTheSelectedSubset(t *testing.T) {
	fixed := money.New(1_500, "MYR")
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	version.ChargeGroups = []OfferChargeGroupSnapshot{{
		ID: "install", Name: "Installation",
		ApplicableRFQLineIDs: []string{"line-1", "line-2"},
		Trigger:              procurementcalc.ChargeTriggerAllSelected,
		Calculation:          procurementcalc.ChargeCalculationFixedAmount,
		FixedAmount:          &fixed,
	}}

	// Only one of the two member lines: the group must NOT trigger.
	partial, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
	}, version))
	if err != nil {
		t.Fatalf("CalculateAward (partial): %v", err)
	}
	if partial.GrandAwardTotal != money.New(10_000, "MYR") {
		t.Fatalf("partial total = %+v, want 10000 MYR with no charge",
			partial.GrandAwardTotal)
	}

	// Both member lines: the group triggers and adds its fixed amount once.
	complete, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		selection("line-2", "lineage-2", "offer-1", "ol-2"),
	}, version))
	if err != nil {
		t.Fatalf("CalculateAward (complete): %v", err)
	}
	if complete.GrandAwardTotal != money.New(14_000, "MYR") {
		t.Fatalf("complete total = %+v, want 14000 MYR with the charge",
			complete.GrandAwardTotal)
	}
}

// A percentage group's base is the SELECTED subtotal, so awarding fewer lines
// yields a smaller charge — copying the submitted amount would overcharge.
func TestCalculateAwardRecalculatesPercentageGroupsOverTheSelectedSubtotal(t *testing.T) {
	rate := money.RateBPS(1_000) // 10%
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	version.ChargeGroups = []OfferChargeGroupSnapshot{{
		ID: "handling", Name: "Handling",
		ApplicableRFQLineIDs: []string{"line-1", "line-2"},
		Trigger:              procurementcalc.ChargeTriggerAnySelected,
		Calculation:          procurementcalc.ChargeCalculationPercentageOfSelectedSubtotal,
		RateBPS:              &rate,
	}}

	calculation, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
	}, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	// 10% of the SELECTED 10000, not of the submitted 12500.
	if calculation.GrandAwardTotal != money.New(11_000, "MYR") {
		t.Fatalf("grand total = %+v, want 11000 MYR", calculation.GrandAwardTotal)
	}
}

// An offer contributing zero awarded lines contributes ZERO — no delivery, no
// offer-level tax, nothing. It was never selected.
func TestCalculateAwardGivesAZeroLineOfferNoContribution(t *testing.T) {
	selected := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})

	unselected := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500)})
	unselected.DeliveryCharge = &DeliveryRule{Amount: money.New(9_999, "MYR")}

	calculation, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
	}, selected, unselected))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if calculation.GrandAwardTotal != money.New(10_000, "MYR") {
		t.Fatalf("grand total = %+v, want 10000 MYR", calculation.GrandAwardTotal)
	}
	for _, summary := range calculation.SupplierSummaries {
		if summary.SupplierID == "supplier-b" {
			t.Fatal("an offer with no awarded lines must contribute nothing")
		}
	}
}

// Unawarded lines carry their reason into the revision and contribute nothing.
// "We deliberately did not buy this" is a decision worth preserving.
func TestCalculateAwardRecordsUnawardedLinesWithTheirReason(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})

	input := calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		{
			IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
			Unawarded: true, UnawardedReason: UnawardedRetenderRequired,
		},
	}, version)

	calculation, err := CalculateAward(input)
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if len(calculation.UnawardedLines) != 1 {
		t.Fatalf("unawarded lines = %d, want 1", len(calculation.UnawardedLines))
	}
	if calculation.UnawardedLines[0].Reason != UnawardedRetenderRequired {
		t.Errorf("reason = %q, want retender_required",
			calculation.UnawardedLines[0].Reason)
	}
	if calculation.GrandAwardTotal != money.New(10_000, "MYR") {
		t.Errorf("an unawarded line must contribute nothing, total = %+v",
			calculation.GrandAwardTotal)
	}
}

// The fingerprint is deterministic: the same decisions produce the same value,
// so a retry of one award is recognisably the same award.
func TestSelectionFingerprintIsDeterministic(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	selections := []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1")}

	first, err := CalculateAward(calcInput(t, selections, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	second, err := CalculateAward(calcInput(t, selections, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if first.SelectionFingerprint == "" {
		t.Fatal("fingerprint must not be empty")
	}
	if first.SelectionFingerprint != second.SelectionFingerprint {
		t.Fatalf("fingerprint is not deterministic: %q vs %q",
			first.SelectionFingerprint, second.SelectionFingerprint)
	}
}

// Selection ORDER is not commercial content: the same decisions listed
// differently are the same award and must fingerprint identically.
func TestSelectionFingerprintIgnoresSelectionOrder(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	forward := []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		selection("line-2", "lineage-2", "offer-1", "ol-2"),
	}
	reverse := []AwardLineSelection{forward[1], forward[0]}

	first, err := CalculateAward(calcInput(t, forward, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	second, err := CalculateAward(calcInput(t, reverse, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if first.SelectionFingerprint != second.SelectionFingerprint {
		t.Fatal("reordering the same decisions must not change the fingerprint")
	}
}

// The fingerprint excludes timestamps, so calculating the same award at a
// different moment is recognisably the same award.
func TestSelectionFingerprintIgnoresTimestamps(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	selections := []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1")}

	first, err := CalculateAward(calcInput(t, selections, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}

	later := calcInput(t, selections, version)
	later.CalculatedAt = calcAt.Add(time.Hour)
	second, err := CalculateAward(later)
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	if first.SelectionFingerprint != second.SelectionFingerprint {
		t.Fatal("the fingerprint must ignore calculation time")
	}
}

// Any commercial change moves the fingerprint, which is what lets F5 refuse to
// silently overwrite a differing revision.
func TestSelectionFingerprintChangesWithCommercialContent(t *testing.T) {
	base := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	original, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1")}, base))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}

	dearer := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1100, 11_000)})
	changed, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1")}, dearer))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}

	if original.SelectionFingerprint == changed.SelectionFingerprint {
		t.Fatal("a changed award total must change the fingerprint")
	}
}

// A selection naming an offer version that was not supplied cannot be
// calculated: F3 never guesses a missing commercial input.
func TestCalculateAwardRejectsASelectionWithNoSuppliedOfferVersion(t *testing.T) {
	input := AwardCalculationInput{
		IssuedRFQ: calcIssued(t),
		Selections: []AwardLineSelection{
			selection("line-1", "lineage-1", "offer-missing", "ol-1")},
		OfferVersions: map[string]OfferVersionSnapshot{},
		CalculatedAt:  calcAt,
	}
	if _, err := CalculateAward(input); !errors.Is(
		err, ErrOfferVersionNotSelectable) {
		t.Fatalf("err = %v, want ErrOfferVersionNotSelectable", err)
	}
}

// A selection naming a line the issued version does not have would award
// something no Supplier was asked to quote.
func TestCalculateAwardRejectsASelectionForAnUnknownIssuedLine(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})

	if _, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-ghost", "lineage-ghost", "offer-1", "ol-1")},
		version)); !errors.Is(err, ErrOfferVersionNotSelectable) {
		t.Fatalf("err = %v, want ErrOfferVersionNotSelectable", err)
	}
}

// One lineage cannot be awarded twice within a single award: the two selections
// would both claim the same lineage, which F4's unique index could not resolve.
func TestCalculateAwardRejectsTwoSelectionsForOneIssuedLine(t *testing.T) {
	first := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	second := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-1", mustQuantity(t, "10", "sqm"), 900, 9_000)})

	if _, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
		selection("line-1", "lineage-1", "offer-2", "ol-2"),
	}, first, second)); err == nil {
		t.Fatal("one issued line must not be awarded to two Suppliers")
	}
}

// Line-level tax is recalculated PER SELECTED LINE through the shared kernel,
// so the awarded tax matches what the Supplier quoted for those exact lines.
func TestCalculateAwardRecalculatesLineLevelTaxOverSelectedLinesOnly(t *testing.T) {
	rate := money.RateBPS(600) // 6%
	lineTax := &OfferLineTax{
		TaxType: procurementcalc.TaxTypeServiceTax,
		RateBPS: &rate, RegistrationNumber: "SST-123",
	}

	first := calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)
	first.LineTax = lineTax
	second := calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500)
	second.LineTax = lineTax

	version := calcVersion(t, "offer-1", "supplier-a",
		[]OfferLineSnapshot{first, second})
	version.Tax = OfferTaxRule{Mode: OfferTaxLineLevel}

	calculation, err := CalculateAward(calcInput(t, []AwardLineSelection{
		selection("line-1", "lineage-1", "offer-1", "ol-1"),
	}, version))
	if err != nil {
		t.Fatalf("CalculateAward: %v", err)
	}
	// 10000 + 6% of 10000 only. The unselected line's tax is not charged.
	if calculation.GrandAwardTotal != money.New(10_600, "MYR") {
		t.Fatalf("grand total = %+v, want 10600 MYR", calculation.GrandAwardTotal)
	}
}
