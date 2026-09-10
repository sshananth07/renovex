package awards

import (
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
)

// F6 award corrections (§8G).
//
// A correction is a LOCKED BASELINE plus a CORRECTION DELTA. The baseline is
// copied exactly from the current authoritative revision and is NEVER
// revalidated against gate state, expiry or LatestSubmittedID — an already
// awarded version legitimately has a terminal gate, may be superseded and may
// be expired. Revalidating it would reject exactly the monotonic corrections
// F6 exists to permit.

func correctionIssued(t *testing.T) IssuedRFQSnapshot {
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

// baselineRevision is revision 1: line-1 awarded to supplier-a.
func baselineRevision(t *testing.T) AwardRevision {
	t.Helper()
	return AwardRevision{
		ID: "revision-1", CompanyID: "company-1", AwardChainID: "chain-1",
		RFQChainID: "rfqchain-1", IssuedRFQVersionID: "issued-1",
		RevisionNumber: 1, FinalisationOperationID: "op-1",
		SelectionFingerprint: "fingerprint-1",
		AwardedLines: []AwardedLine{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			MaterialName: "Tile", Quantity: mustQuantity(t, "10", "sqm"),
			SupplierID: "supplier-a", InvitationID: "invitation-supplier-a",
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
			LineSubtotal:  money.New(10_000, "MYR"),
			LineTaxAmount: money.New(0, "MYR"),
		}},
		SupplierSummaries: []AwardSupplierSummary{{
			SupplierID: "supplier-a", InvitationID: "invitation-supplier-a",
			OfferVersionID: "offer-1", AwardedLineIDs: []string{"line-1"},
			LineSubtotal:   money.New(10_000, "MYR"),
			TaxTotal:       money.New(0, "MYR"),
			ChargeTotal:    money.New(0, "MYR"),
			DeliveryCharge: money.New(0, "MYR"),
			SupplierTotal:  money.New(10_000, "MYR"),
		}},
		GrandAwardTotal:   money.New(10_000, "MYR"),
		FinalisedByUserID: "user-1",
		FinalisedAt:       calcAt.Add(-48 * time.Hour),
	}
}

// baselineSelection mirrors revision 1's decision, as a correction must.
func baselineSelection() AwardLineSelection {
	return AwardLineSelection{
		IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
		OfferVersionID: "offer-1", OfferLineID: "ol-1",
	}
}

func correctionInput(t *testing.T,
	baseline AwardRevision,
	selections []AwardLineSelection,
	versions ...OfferVersionSnapshot) CorrectionCalculationInput {
	t.Helper()
	byID := make(map[string]OfferVersionSnapshot, len(versions))
	for _, version := range versions {
		byID[version.ID] = version
	}
	return CorrectionCalculationInput{
		IssuedRFQ:          correctionIssued(t),
		BaselineRevision:   baseline,
		ProposedSelections: selections,
		OfferVersions:      byID,
		CalculatedAt:       calcAt,
	}
}

// A baseline version whose gate is TERMINALLY awarded must not block a
// correction. This is the case ordinary F3 validation would wrongly reject.
func TestCorrectionAcceptsABaselineVersionWithATerminalGate(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	// Exactly what revision 1 left behind.
	version.EligibilityState = OfferEligibilityAwarded

	calculation, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{baselineSelection()},
		version))
	if err != nil {
		t.Fatalf("a terminal baseline gate must not block a correction: %v", err)
	}
	if calculation.GrandAwardTotal != money.New(10_000, "MYR") {
		t.Errorf("total = %+v, want the unchanged 10000 MYR",
			calculation.GrandAwardTotal)
	}
}

// A baseline version the Supplier has since superseded must not block a
// correction: they may legitimately have submitted a newer quote.
func TestCorrectionAcceptsABaselineVersionThatIsNoLongerLatest(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.EligibilityState = OfferEligibilityAwarded
	version.IsLatestSubmitted = false

	if _, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{baselineSelection()},
		version)); err != nil {
		t.Fatalf("a superseded baseline version must not block a correction: %v",
			err)
	}
}

// A baseline version whose validity has PASSED must not block a correction: the
// award was made while it was valid, and that commitment stands.
func TestCorrectionAcceptsAnExpiredBaselineVersion(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.EligibilityState = OfferEligibilityAwarded
	version.OfferValidUntil = calcAt.Add(-time.Hour)

	if _, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{baselineSelection()},
		version)); err != nil {
		t.Fatalf("an expired baseline version must not block a correction: %v",
			err)
	}
}

// The DELTA is fully validated: a newly added line whose offer is withdrawn is
// refused with the ordinary F3 error.
func TestCorrectionRejectsAWithdrawnOfferInTheDelta(t *testing.T) {
	baseline := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baseline.EligibilityState = OfferEligibilityAwarded

	// A new Supplier for line-2 who has since withdrawn.
	added := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500)})
	added.EligibilityState = OfferEligibilityWithdrawn

	_, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				OfferVersionID: "offer-2", OfferLineID: "ol-2",
			},
		}, baseline, added))
	if !errors.Is(err, ErrOfferVersionNotEligible) {
		t.Fatalf("err = %v, want ErrOfferVersionNotEligible for the delta", err)
	}
}

// A correction MAY add a previously unawarded, currently eligible lineage.
func TestCorrectionAddsANewlyAwardedLineage(t *testing.T) {
	baseline := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baseline.EligibilityState = OfferEligibilityAwarded

	added := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500)})

	calculation, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				OfferVersionID: "offer-2", OfferLineID: "ol-2",
			},
		}, baseline, added))
	if err != nil {
		t.Fatalf("CalculateCorrection: %v", err)
	}
	if calculation.GrandAwardTotal != money.New(12_500, "MYR") {
		t.Fatalf("total = %+v, want 12500 MYR", calculation.GrandAwardTotal)
	}
	if len(calculation.AwardedLines) != 2 {
		t.Fatalf("awarded lines = %d, want 2", len(calculation.AwardedLines))
	}
	// The newly claimed lineage is reported so F4 claims the DELTA only.
	if len(calculation.NewlyAwardedLineages) != 1 ||
		calculation.NewlyAwardedLineages[0] != "lineage-2" {
		t.Fatalf("newly awarded lineages = %v, want [lineage-2]",
			calculation.NewlyAwardedLineages)
	}
}

// Baseline lineages are NOT re-claimed: their claims are already terminal, and
// re-acquiring would fail because a terminal claim is not eligible (§8A.2).
func TestCorrectionDoesNotReclaimBaselineLineages(t *testing.T) {
	baseline := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baseline.EligibilityState = OfferEligibilityAwarded

	calculation, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{baselineSelection()},
		baseline))
	if err != nil {
		t.Fatalf("CalculateCorrection: %v", err)
	}
	if len(calculation.NewlyAwardedLineages) != 0 {
		t.Fatalf("newly awarded lineages = %v, want none for an unchanged baseline",
			calculation.NewlyAwardedLineages)
	}
	if len(calculation.NewlyClaimedOfferVersions) != 0 {
		t.Fatalf("newly claimed versions = %v, want none",
			calculation.NewlyClaimedOfferVersions)
	}
}

// MONOTONICITY: removing an awarded lineage is refused. Revision 1 may already
// have told the Supplier they won.
func TestCorrectionRefusesRemovingAnAwardedLineage(t *testing.T) {
	baseline := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baseline.EligibilityState = OfferEligibilityAwarded

	// The correction simply drops line-1.
	_, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{}, baseline))
	if !errors.Is(err, ErrAwardCorrectionNotMonotonic) {
		t.Fatalf("err = %v, want ErrAwardCorrectionNotMonotonic", err)
	}
}

// MONOTONICITY: turning an awarded lineage into an unawarded one is a removal
// wearing a different hat.
func TestCorrectionRefusesUnawardingAnAwardedLineage(t *testing.T) {
	baseline := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baseline.EligibilityState = OfferEligibilityAwarded

	_, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			Unawarded: true, UnawardedReason: UnawardedScopeCancelled,
		}}, baseline))
	if !errors.Is(err, ErrAwardCorrectionNotMonotonic) {
		t.Fatalf("err = %v, want ErrAwardCorrectionNotMonotonic", err)
	}
}

// MONOTONICITY: reassigning an awarded lineage to another Supplier is the exact
// sequence §8G forbids — Supplier X was told they won and silently did not.
func TestCorrectionRefusesReassigningAnAwardedLineage(t *testing.T) {
	baseline := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baseline.EligibilityState = OfferEligibilityAwarded

	competitor := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-1", mustQuantity(t, "10", "sqm"), 900, 9_000)})

	_, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			OfferVersionID: "offer-2", OfferLineID: "ol-2",
		}}, baseline, competitor))
	if !errors.Is(err, ErrAwardCorrectionNotMonotonic) {
		t.Fatalf("err = %v, want ErrAwardCorrectionNotMonotonic", err)
	}
}

// MONOTONICITY: reducing an awarded line's commercial contribution is refused
// even when the Supplier and lineage are unchanged.
func TestCorrectionRefusesReducingAnAwardedContribution(t *testing.T) {
	// The same offer version, but repriced downward.
	cheaper := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 800, 8_000)})
	cheaper.EligibilityState = OfferEligibilityAwarded

	_, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{baselineSelection()},
		cheaper))
	if !errors.Is(err, ErrAwardCorrectionNotMonotonic) {
		t.Fatalf("err = %v, want ErrAwardCorrectionNotMonotonic", err)
	}
}

// Adding a second line from a version ALREADY in the baseline uses a
// CUMULATIVE recalculation minus the frozen baseline contribution, so delivery
// is charged exactly ONCE across the two revisions.
func TestCorrectionChargesDeliveryOnlyOnceWhenAddingToABaselineVersion(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	version.EligibilityState = OfferEligibilityAwarded
	version.DeliveryCharge = &DeliveryRule{Amount: money.New(3_000, "MYR")}

	// Revision 1 awarded line-1 AND paid the delivery charge.
	baseline := baselineRevision(t)
	baseline.AwardedLines[0].LineSubtotal = money.New(10_000, "MYR")
	baseline.SupplierSummaries[0].DeliveryCharge = money.New(3_000, "MYR")
	baseline.SupplierSummaries[0].SupplierTotal = money.New(13_000, "MYR")
	baseline.GrandAwardTotal = money.New(13_000, "MYR")

	calculation, err := CalculateCorrection(correctionInput(t, baseline,
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				OfferVersionID: "offer-1", OfferLineID: "ol-2",
			},
		}, version))
	if err != nil {
		t.Fatalf("CalculateCorrection: %v", err)
	}
	// Cumulative: 10000 + 2500 + 3000 delivery = 15500. Delivery is charged
	// once because the cumulative recalculation applies it once.
	if calculation.GrandAwardTotal != money.New(15_500, "MYR") {
		t.Fatalf("total = %+v, want 15500 MYR with delivery charged once",
			calculation.GrandAwardTotal)
	}
	// The DELTA is what the correction newly commits: 15500 - 13000.
	if calculation.CorrectionDeltaTotal != money.New(2_500, "MYR") {
		t.Fatalf("delta = %+v, want 2500 MYR",
			calculation.CorrectionDeltaTotal)
	}
}

// A percentage conditional group is recalculated over the CUMULATIVE line set,
// so adding a line legitimately increases it.
func TestCorrectionRecalculatesPercentageGroupsOverTheCumulativeLineSet(t *testing.T) {
	rate := money.RateBPS(1_000) // 10%
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	version.EligibilityState = OfferEligibilityAwarded
	version.ChargeGroups = []OfferChargeGroupSnapshot{{
		ID: "handling", Name: "Handling",
		ApplicableRFQLineIDs: []string{"line-1", "line-2"},
		Trigger:              procurementcalc.ChargeTriggerAnySelected,
		Calculation:          procurementcalc.ChargeCalculationPercentageOfSelectedSubtotal,
		RateBPS:              &rate,
	}}

	// Revision 1: line-1 only, so the group was 10% of 10000 = 1000.
	baseline := baselineRevision(t)
	baseline.SupplierSummaries[0].ChargeTotal = money.New(1_000, "MYR")
	baseline.SupplierSummaries[0].SupplierTotal = money.New(11_000, "MYR")
	baseline.GrandAwardTotal = money.New(11_000, "MYR")

	calculation, err := CalculateCorrection(correctionInput(t, baseline,
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				OfferVersionID: "offer-1", OfferLineID: "ol-2",
			},
		}, version))
	if err != nil {
		t.Fatalf("CalculateCorrection: %v", err)
	}
	// Cumulative: 12500 lines + 10% of 12500 = 13750.
	if calculation.GrandAwardTotal != money.New(13_750, "MYR") {
		t.Fatalf("total = %+v, want 13750 MYR", calculation.GrandAwardTotal)
	}
}

// A cumulative contribution BELOW the frozen baseline means the correction
// would reduce a published amount, and is refused.
func TestCorrectionRefusesACumulativeContributionBelowTheBaseline(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.EligibilityState = OfferEligibilityAwarded

	// The baseline claims a higher contribution than the offer can produce.
	baseline := baselineRevision(t)
	baseline.SupplierSummaries[0].SupplierTotal = money.New(20_000, "MYR")
	baseline.GrandAwardTotal = money.New(20_000, "MYR")

	_, err := CalculateCorrection(correctionInput(t, baseline,
		[]AwardLineSelection{baselineSelection()}, version))
	if !errors.Is(err, ErrAwardCorrectionNotMonotonic) {
		t.Fatalf("err = %v, want ErrAwardCorrectionNotMonotonic", err)
	}
}

// D1 makes adding a line to an offer_level version impossible: such a version
// was awarded with EVERY positively quoted line, so none remains to add. An
// attempt is a contract violation, not a valid correction.
func TestCorrectionRefusesAddingALineToAnOfferLevelVersion(t *testing.T) {
	taxAmount := money.New(750, "MYR")
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000),
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500),
	})
	version.EligibilityState = OfferEligibilityAwarded
	version.Tax = OfferTaxRule{
		Mode: OfferTaxOfferLevel,
		OfferLevel: &procurementcalc.QuotedOfferTax{
			TaxType:   procurementcalc.TaxTypeOther,
			TaxAmount: taxAmount, BasisNote: "flat",
		},
	}

	// The baseline claims only line-1 of an offer_level version, which D1
	// forbids — so this state should never have been published.
	_, err := CalculateCorrection(correctionInput(t,
		baselineRevision(t),
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				OfferVersionID: "offer-1", OfferLineID: "ol-2",
			},
		}, version))
	if err == nil {
		t.Fatal("adding a line to an offer_level version must be refused")
	}
}

// A correction MAY amend the reason recorded against an unawarded line: that is
// explanatory metadata, not a commercial commitment.
func TestCorrectionMayAmendAnUnawardedReason(t *testing.T) {
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.EligibilityState = OfferEligibilityAwarded

	baseline := baselineRevision(t)
	baseline.UnawardedLines = []UnawardedLine{{
		IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
		Reason: UnawardedPurchaseDeferred,
	}}

	calculation, err := CalculateCorrection(correctionInput(t, baseline,
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				Unawarded: true, UnawardedReason: UnawardedRetenderRequired,
			},
		}, version))
	if err != nil {
		t.Fatalf("amending an unawarded reason must be permitted: %v", err)
	}
	if len(calculation.UnawardedLines) != 1 ||
		calculation.UnawardedLines[0].Reason != UnawardedRetenderRequired {
		t.Fatalf("unawarded lines = %+v, want the amended reason",
			calculation.UnawardedLines)
	}
}

// The correction's fingerprint differs from the baseline's whenever commercial
// content changed, which is what makes F5 refuse to conflate the two.
func TestCorrectionFingerprintDiffersFromTheBaseline(t *testing.T) {
	baselineVersion := calcVersion(t, "offer-1", "supplier-a",
		[]OfferLineSnapshot{
			calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	baselineVersion.EligibilityState = OfferEligibilityAwarded

	added := calcVersion(t, "offer-2", "supplier-b", []OfferLineSnapshot{
		calcLine("ol-2", "line-2", mustQuantity(t, "5", "bag"), 500, 2_500)})

	baseline := baselineRevision(t)
	calculation, err := CalculateCorrection(correctionInput(t, baseline,
		[]AwardLineSelection{
			baselineSelection(),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				OfferVersionID: "offer-2", OfferLineID: "ol-2",
			},
		}, baselineVersion, added))
	if err != nil {
		t.Fatalf("CalculateCorrection: %v", err)
	}
	if calculation.SelectionFingerprint == baseline.SelectionFingerprint {
		t.Fatal("a correction that changes the award must change the fingerprint")
	}
}
