package awards

import (
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// F7 outcomes and the privacy boundary (§8H).
//
// The projection is FROZEN at generation from the revision. A stored snapshot
// cannot drift, and it means a later correction cannot retroactively change
// what a Supplier was already told.

// twoSupplierRevision awards line-1 to supplier-a and line-2 to supplier-b, so
// every leakage assertion has a real competitor to leak.
func twoSupplierRevision(t *testing.T) AwardRevision {
	t.Helper()
	return AwardRevision{
		ID: "revision-1", CompanyID: "company-1", AwardChainID: "chain-1",
		RFQChainID: "rfqchain-1", IssuedRFQVersionID: "issued-1",
		RevisionNumber: 1, FinalisationOperationID: "op-1",
		SelectionFingerprint: "fingerprint-1",
		AwardedLines: []AwardedLine{
			{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				MaterialName: "Tile", Quantity: mustQuantity(t, "10", "sqm"),
				SupplierID: "supplier-a", SupplierName: "Alpha Supplies",
				InvitationID:   "invitation-a",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
				UnitPriceExcludingTax: money.New(1_000, "MYR"),
				LineSubtotal:          money.New(10_000, "MYR"),
				LineTaxAmount:         money.New(600, "MYR"),
			},
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				MaterialName: "Grout", Quantity: mustQuantity(t, "5", "bag"),
				SupplierID: "supplier-b", SupplierName: "Beta Trading",
				InvitationID:   "invitation-b",
				OfferVersionID: "offer-2", OfferLineID: "ol-2",
				UnitPriceExcludingTax: money.New(500, "MYR"),
				LineSubtotal:          money.New(2_500, "MYR"),
				LineTaxAmount:         money.New(0, "MYR"),
			},
		},
		UnawardedLines: []UnawardedLine{{
			IssuedRFQLineID: "line-3", StableLineageID: "lineage-3",
			MaterialName: "Sealant",
			// Internal sourcing rationale: it discloses the contractor's
			// commercial position and must never reach a Supplier.
			Reason: UnawardedNoAcceptableOffer,
		}},
		SupplierSummaries: []AwardSupplierSummary{
			{
				SupplierID: "supplier-a", SupplierName: "Alpha Supplies",
				InvitationID: "invitation-a", OfferVersionID: "offer-1",
				AwardedLineIDs: []string{"line-1"},
				LineSubtotal:   money.New(10_000, "MYR"),
				TaxTotal:       money.New(600, "MYR"),
				ChargeTotal:    money.New(0, "MYR"),
				DeliveryCharge: money.New(3_000, "MYR"),
				SupplierTotal:  money.New(13_600, "MYR"),
			},
			{
				SupplierID: "supplier-b", SupplierName: "Beta Trading",
				InvitationID: "invitation-b", OfferVersionID: "offer-2",
				AwardedLineIDs: []string{"line-2"},
				LineSubtotal:   money.New(2_500, "MYR"),
				TaxTotal:       money.New(0, "MYR"),
				ChargeTotal:    money.New(0, "MYR"),
				DeliveryCharge: money.New(2_000, "MYR"),
				SupplierTotal:  money.New(4_500, "MYR"),
			},
		},
		GrandAwardTotal:   money.New(18_100, "MYR"),
		FinalisedByUserID: "user-1",
		FinalisedAt:       calcAt,
	}
}

// participants lists every Supplier holding an eligible submitted offer,
// including one who won nothing.
func participants() []OutcomeParticipant {
	return []OutcomeParticipant{
		{SupplierID: "supplier-a", SupplierName: "Alpha Supplies",
			InvitationID: "invitation-a", OfferVersionID: "offer-1"},
		{SupplierID: "supplier-b", SupplierName: "Beta Trading",
			InvitationID: "invitation-b", OfferVersionID: "offer-2"},
		{SupplierID: "supplier-c", SupplierName: "Gamma Ltd",
			InvitationID: "invitation-c", OfferVersionID: "offer-3"},
	}
}

// One outcome per Supplier + revision, for every participant — winners and
// losers alike. A Supplier who responded is entitled to know the result.
func TestGenerateOutcomesProducesOnePerParticipant(t *testing.T) {
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     twoSupplierRevision(t),
		Participants: participants(),
		GeneratedAt:  calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("outcomes = %d, want one per participant", len(outcomes))
	}

	results := map[string]OutcomeResult{}
	for _, outcome := range outcomes {
		results[outcome.SupplierID] = outcome.Result
	}
	if results["supplier-a"] != OutcomeSelected ||
		results["supplier-b"] != OutcomeSelected {
		t.Errorf("awarded Suppliers must be selected, got %v", results)
	}
	if results["supplier-c"] != OutcomeUnsuccessful {
		t.Errorf("supplier-c won nothing and must be unsuccessful, got %q",
			results["supplier-c"])
	}
}

// A selected Supplier sees ONLY their own lines. Seeing a competitor's line
// would disclose both who else bid and what they charge.
func TestSelectedOutcomeContainsOnlyItsOwnLines(t *testing.T) {
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     twoSupplierRevision(t),
		Participants: participants(),
		GeneratedAt:  calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}

	for _, outcome := range outcomes {
		if outcome.SupplierID != "supplier-a" {
			continue
		}
		if len(outcome.Projection.AwardedLines) != 1 {
			t.Fatalf("awarded lines = %d, want only its own",
				len(outcome.Projection.AwardedLines))
		}
		line := outcome.Projection.AwardedLines[0]
		if line.IssuedRFQLineID != "line-1" {
			t.Errorf("line = %q, want its own line-1", line.IssuedRFQLineID)
		}
		if outcome.Projection.AwardTotal != money.New(13_600, "MYR") {
			t.Errorf("award total = %+v, want its OWN 13600 MYR, not the grand total",
				outcome.Projection.AwardTotal)
		}
	}
}

// An unsuccessful Supplier carries NO commercial figures at all: a neutral
// non-award statement is the entire content.
func TestUnsuccessfulOutcomeCarriesNoCommercialFigures(t *testing.T) {
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     twoSupplierRevision(t),
		Participants: participants(),
		GeneratedAt:  calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}

	for _, outcome := range outcomes {
		if outcome.SupplierID != "supplier-c" {
			continue
		}
		if len(outcome.Projection.AwardedLines) != 0 {
			t.Errorf("an unsuccessful outcome must carry no awarded lines")
		}
		if outcome.Projection.AwardTotal.Amount != 0 {
			t.Errorf("award total = %+v, want zero for an unsuccessful outcome",
				outcome.Projection.AwardTotal)
		}
		if outcome.Projection.TaxTotal.Amount != 0 ||
			outcome.Projection.DeliveryCharge.Amount != 0 {
			t.Error("an unsuccessful outcome must carry no tax or delivery")
		}
	}
}

// NEVER, in either case: competitor names, prices, totals, rankings, how many
// Suppliers responded, comparison notes, or unawarded-line reasons.
func TestNoOutcomeLeaksCompetitorDataOrUnawardedReasons(t *testing.T) {
	revision := twoSupplierRevision(t)
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     revision,
		Participants: participants(),
		GeneratedAt:  calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}

	for _, outcome := range outcomes {
		rendered := renderProjection(outcome.Projection)

		// Competitor identities.
		for _, competitor := range []struct{ id, name string }{
			{"supplier-a", "Alpha Supplies"},
			{"supplier-b", "Beta Trading"},
			{"supplier-c", "Gamma Ltd"},
		} {
			if competitor.id == outcome.SupplierID {
				continue
			}
			if strings.Contains(rendered, competitor.id) ||
				strings.Contains(rendered, competitor.name) {
				t.Errorf("outcome for %s leaks competitor %q",
					outcome.SupplierID, competitor.name)
			}
		}

		// The grand total across all Suppliers reveals the whole award's size.
		if strings.Contains(rendered, "18100") {
			t.Errorf("outcome for %s leaks the grand award total",
				outcome.SupplierID)
		}

		// Unawarded reasons disclose the contractor's commercial position.
		if strings.Contains(rendered, string(UnawardedNoAcceptableOffer)) {
			t.Errorf("outcome for %s leaks an unawarded reason",
				outcome.SupplierID)
		}

		// A count of respondents is itself commercially sensitive.
		if outcome.Projection.RespondentCount != 0 {
			t.Errorf("outcome for %s discloses how many Suppliers responded",
				outcome.SupplierID)
		}
	}
}

// D3: outcomes are scoped to Supplier + INVITATION, not to the recipient
// identity that submitted the offer, so a replacement recipient may read the
// outcome without reaching the previous recipient's draft.
func TestOutcomesAreScopedToSupplierAndInvitation(t *testing.T) {
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     twoSupplierRevision(t),
		Participants: participants(),
		GeneratedAt:  calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}

	for _, outcome := range outcomes {
		if outcome.InvitationID == "" {
			t.Fatalf("outcome for %s carries no invitation scope (D3)",
				outcome.SupplierID)
		}
		if outcome.SupplierID == "" {
			t.Fatal("outcome carries no supplier scope")
		}
	}
}

// A Supplier who withdrew before publication gets NO outcome: they removed
// themselves from consideration.
func TestWithdrawnSupplierReceivesNoOutcome(t *testing.T) {
	withParticipants := participants()
	withParticipants = append(withParticipants, OutcomeParticipant{
		SupplierID: "supplier-d", SupplierName: "Delta Co",
		InvitationID: "invitation-d", OfferVersionID: "offer-4",
		Withdrawn: true,
	})

	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     twoSupplierRevision(t),
		Participants: withParticipants,
		GeneratedAt:  calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}
	for _, outcome := range outcomes {
		if outcome.SupplierID == "supplier-d" {
			t.Fatal("a withdrawn Supplier must receive no outcome")
		}
	}
	if len(outcomes) != 3 {
		t.Fatalf("outcomes = %d, want 3 without the withdrawn Supplier",
			len(outcomes))
	}
}

// The projection is FROZEN at generation: a later correction cannot
// retroactively change what a Supplier was already told.
func TestOutcomeProjectionIsFrozenAgainstLaterCorrections(t *testing.T) {
	revision := twoSupplierRevision(t)
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision: revision, Participants: participants(), GeneratedAt: calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}

	var before money.Money
	for _, outcome := range outcomes {
		if outcome.SupplierID == "supplier-a" {
			before = outcome.Projection.AwardTotal
		}
	}

	// A correction changes the revision this outcome came from.
	revision.SupplierSummaries[0].SupplierTotal = money.New(99_999, "MYR")
	revision.AwardedLines[0].LineSubtotal = money.New(99_999, "MYR")

	for _, outcome := range outcomes {
		if outcome.SupplierID != "supplier-a" {
			continue
		}
		if outcome.Projection.AwardTotal != before {
			t.Fatal("the projection is not frozen; a later change reached it")
		}
	}
}

// The contractor's message is carried verbatim; next-step wording accompanies a
// selected outcome so the Supplier knows what happens next.
func TestSelectedOutcomeCarriesTheContractorMessage(t *testing.T) {
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:          twoSupplierRevision(t),
		Participants:      participants(),
		ContractorMessage: "Please confirm receipt.",
		GeneratedAt:       calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}
	for _, outcome := range outcomes {
		if outcome.Projection.ContractorMessage != "Please confirm receipt." {
			t.Errorf("outcome for %s lost the contractor message",
				outcome.SupplierID)
		}
	}
}

// Acknowledgement confirms RECEIPT only. The projection must say so, because a
// Supplier could otherwise read an award as a Purchase Order.
func TestSelectedOutcomeStatesItIsNotAPurchaseOrder(t *testing.T) {
	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision: twoSupplierRevision(t), Participants: participants(),
		GeneratedAt: calcAt,
	})
	if err != nil {
		t.Fatalf("GenerateOutcomes: %v", err)
	}
	for _, outcome := range outcomes {
		if outcome.Result != OutcomeSelected {
			continue
		}
		wording := strings.ToLower(outcome.Projection.NextSteps)
		if !strings.Contains(wording, "purchase order") {
			t.Errorf("selected outcome for %s does not state that this is not "+
				"a Purchase Order", outcome.SupplierID)
		}
	}
}
