package awards

import (
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// F1 is a read-only projection (§8B). It owns no collection: a stored
// comparison would be a second source of commercial truth that could drift from
// the immutable Offer Versions it is derived from.

func comparisonIssuedRFQ(t *testing.T) IssuedRFQSnapshot {
	t.Helper()
	return IssuedRFQSnapshot{
		ID:            "issued-1",
		CompanyID:     "company-1",
		RFQChainID:    "rfqchain-1",
		RFQNumber:     "RFQ-0001",
		VersionNumber: 2,
		Currency:      "MYR",
		Lines: []IssuedRFQLineSnapshot{
			{ID: "line-1", LineageID: "lineage-1", MaterialName: "Tile", SortOrder: 1,
				Quantity: mustQuantity(t, "10", "sqm")},
			{ID: "line-2", LineageID: "lineage-2", MaterialName: "Grout", SortOrder: 2,
				Quantity: mustQuantity(t, "5", "bag")},
		},
	}
}

func quotedLine(id, rfqLineID string, subtotalMinor int64) OfferLineSnapshot {
	subtotal := money.New(subtotalMinor, "MYR")
	return OfferLineSnapshot{
		ID:                       id,
		RFQLineID:                rfqLineID,
		ResponseStatus:           OfferLineQuoted,
		LineSubtotalExcludingTax: &subtotal,
		LineTaxAmount:            money.New(0, "MYR"),
	}
}

func eligibleVersion(id, supplier, invitation string, versionNumber int,
	lines []OfferLineSnapshot) OfferVersionSnapshot {
	return OfferVersionSnapshot{
		ID:                 id,
		CompanyID:          "company-1",
		OfferChainID:       "offerchain-" + supplier,
		SupplierID:         supplier,
		SupplierName:       supplier,
		InvitationID:       invitation,
		IssuedRFQVersionID: "issued-1",
		VersionNumber:      versionNumber,
		Currency:           "MYR",
		Lines:              lines,
		Tax:                OfferTaxRule{Mode: OfferTaxNotApplicable},
		OfferValidUntil:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		SubmittedAt:        time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		IsLatestSubmitted:  true,
		EligibilityState:   OfferEligibilityEligible,
		QuotedLineSubtotal: money.New(0, "MYR"),
		QuotedTaxTotal:     money.New(0, "MYR"),
		GrandTotal:         money.New(0, "MYR"),
	}
}

// The default view shows the latest eligible submitted version per Invitation.
// An earlier version of the same Invitation is labelled superseded, not shown
// as a live competing quote.
func TestComparisonShowsLatestEligibleVersionPerInvitation(t *testing.T) {
	older := eligibleVersion("offer-v1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)})
	older.IsLatestSubmitted = false
	newer := eligibleVersion("offer-v2", "supplier-a", "invitation-a", 2,
		[]OfferLineSnapshot{quotedLine("ol-2", "line-1", 900)})

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{older, newer},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}

	if len(projection.Offers) != 2 {
		t.Fatalf("expected both versions represented, got %d", len(projection.Offers))
	}
	byID := map[string]ComparisonOffer{}
	for _, offer := range projection.Offers {
		byID[offer.OfferVersionID] = offer
	}
	if !byID["offer-v2"].IsDefaultView {
		t.Error("latest eligible version must be in the default view")
	}
	if byID["offer-v1"].IsDefaultView {
		t.Error("a superseded version must not be in the default view")
	}
	if byID["offer-v1"].Label != ComparisonLabelSuperseded {
		t.Errorf("older version label = %q, want %q",
			byID["offer-v1"].Label, ComparisonLabelSuperseded)
	}
}

// Each excluded-from-default state carries its own label, so a contractor can
// tell "the Supplier pulled out" apart from "this quote timed out".
func TestComparisonLabelsHistoricalVersions(t *testing.T) {
	issued := comparisonIssuedRFQ(t)
	observedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	withdrawn := eligibleVersion("offer-withdrawn", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)})
	withdrawn.EligibilityState = OfferEligibilityWithdrawn

	expired := eligibleVersion("offer-expired", "supplier-b", "invitation-b", 1,
		[]OfferLineSnapshot{quotedLine("ol-2", "line-1", 1000)})
	expired.OfferValidUntil = observedAt.Add(-time.Hour)

	previousRFQ := eligibleVersion("offer-previous", "supplier-c", "invitation-c", 1,
		[]OfferLineSnapshot{quotedLine("ol-3", "line-1", 1000)})
	previousRFQ.IssuedRFQVersionID = "issued-0"

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:  issued,
		ObservedAt: observedAt,
		OfferVersions: []OfferVersionSnapshot{
			withdrawn, expired, previousRFQ,
		},
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}

	want := map[string]ComparisonLabel{
		"offer-withdrawn": ComparisonLabelWithdrawn,
		"offer-expired":   ComparisonLabelExpired,
		"offer-previous":  ComparisonLabelPreviousRFQVersion,
	}
	for _, offer := range projection.Offers {
		if expected, ok := want[offer.OfferVersionID]; ok {
			if offer.Label != expected {
				t.Errorf("%s label = %q, want %q",
					offer.OfferVersionID, offer.Label, expected)
			}
			if offer.IsDefaultView {
				t.Errorf("%s must be excluded from the default view",
					offer.OfferVersionID)
			}
			if offer.Selectable {
				t.Errorf("%s must not be selectable", offer.OfferVersionID)
			}
		}
	}
}

// Declines are shown — the contractor needs to know a Supplier answered — but
// they are never selectable, because F3 refuses to award a non-quoted line.
func TestComparisonShowsDeclinesButNeverSelectable(t *testing.T) {
	version := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{
			quotedLine("ol-1", "line-1", 1000),
			{ID: "ol-2", RFQLineID: "line-2", ResponseStatus: OfferLineNoBid,
				LineTaxAmount: money.New(0, "MYR")},
		})

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{version},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}

	offer := projection.Offers[0]
	byLine := map[string]ComparisonLine{}
	for _, line := range offer.Lines {
		byLine[line.IssuedRFQLineID] = line
	}
	if !byLine["line-1"].Selectable {
		t.Error("a quoted line must be selectable")
	}
	decline := byLine["line-2"]
	if decline.ResponseStatus != OfferLineNoBid {
		t.Fatalf("decline not shown, status = %q", decline.ResponseStatus)
	}
	if decline.Selectable {
		t.Error("a no_bid line must never be selectable (§8B)")
	}
}

// D1: an offer_level version is all-or-nothing, so the projection flags it with
// its complete positively-quoted line set. The interface can warn before F3
// rejects a subset; the backend stays authoritative regardless.
func TestComparisonFlagsPartialAwardUnavailableWithItsQuotedLineSet(t *testing.T) {
	version := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{
			quotedLine("ol-1", "line-1", 1000),
			quotedLine("ol-2", "line-2", 500),
			{ID: "ol-3", RFQLineID: "line-3", ResponseStatus: OfferLineUnavailable,
				LineTaxAmount: money.New(0, "MYR")},
		})
	version.Tax = OfferTaxRule{Mode: OfferTaxOfferLevel}
	version.OfferLevelTaxAmount = money.New(135, "MYR")

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{version},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}

	offer := projection.Offers[0]
	if !offer.PartialAwardUnavailable {
		t.Fatal("an offer_level version must be flagged partialAwardUnavailable (D1)")
	}
	// Only positively quoted lines participate in the completeness requirement.
	want := []string{"ol-1", "ol-2"}
	if len(offer.CompleteSelectionOfferLineIDs) != len(want) {
		t.Fatalf("complete selection set = %v, want %v",
			offer.CompleteSelectionOfferLineIDs, want)
	}
	for index, id := range want {
		if offer.CompleteSelectionOfferLineIDs[index] != id {
			t.Errorf("complete selection set = %v, want %v",
				offer.CompleteSelectionOfferLineIDs, want)
		}
	}
}

// A line_level version imposes no completeness requirement, so flagging it
// would make the interface warn about a restriction that does not exist.
func TestComparisonDoesNotFlagLineLevelTaxVersions(t *testing.T) {
	version := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)})
	version.Tax = OfferTaxRule{Mode: OfferTaxLineLevel}

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{version},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}
	if projection.Offers[0].PartialAwardUnavailable {
		t.Error("only offer_level tax restricts partial award (D1)")
	}
}

// The projection sums already-calculated line subtotals and does nothing else.
// The result is explicitly labelled indicative: only F3 produces an
// authoritative figure, and saying otherwise would let a UI total masquerade
// as a commercial commitment.
func TestComparisonSubtotalIsIndicativeAndNotAuthoritative(t *testing.T) {
	version := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{
			quotedLine("ol-1", "line-1", 1000),
			quotedLine("ol-2", "line-2", 500),
		})

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{version},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}

	offer := projection.Offers[0]
	if !offer.IndicativeQuotedSubtotal.Indicative {
		t.Fatal("a comparison subtotal must be labelled indicative (§8B)")
	}
	if offer.IndicativeQuotedSubtotal.Amount.Amount != 1500 {
		t.Errorf("indicative subtotal = %d, want 1500",
			offer.IndicativeQuotedSubtotal.Amount.Amount)
	}
	if projection.Authoritative {
		t.Error("the comparison projection must never claim to be authoritative")
	}
}

// M8 ranks nothing and recommends no winner (§8B): sorting is a contractor
// action over facts, never a system judgement.
func TestComparisonDoesNotRankOrRecommend(t *testing.T) {
	cheap := eligibleVersion("offer-cheap", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 100)})
	pricey := eligibleVersion("offer-pricey", "supplier-b", "invitation-b", 1,
		[]OfferLineSnapshot{quotedLine("ol-2", "line-1", 900)})

	projection, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{pricey, cheap},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildComparison: %v", err)
	}
	for _, offer := range projection.Offers {
		if offer.Rank != 0 || offer.Recommended {
			t.Fatalf("%s carries a rank or recommendation; M8 ranks nothing (§8B)",
				offer.OfferVersionID)
		}
	}
}

// An offer version belonging to another Company can never enter the
// projection: CompanyID comes from the authenticated principal, and a
// mismatched snapshot is a wiring fault rather than data to display.
func TestComparisonRejectsForeignCompanyOfferVersion(t *testing.T) {
	foreign := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)})
	foreign.CompanyID = "company-2"

	_, err := BuildComparison(ComparisonInput{
		IssuedRFQ:     comparisonIssuedRFQ(t),
		OfferVersions: []OfferVersionSnapshot{foreign},
		ObservedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("a foreign-company offer version must not be projected")
	}
}
