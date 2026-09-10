package awards

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// The comparison service resolves both sides through capabilities and never
// reads another module's collection. CompanyID always comes from the caller's
// authenticated principal, never from request content.

func comparisonService(
	issued IssuedRFQSnapshot,
	found bool,
	versions []OfferVersionSnapshot,
) (*Service, *fakeIssuedRFQSource, *fakeOfferVersionSource) {
	issuedSource := &fakeIssuedRFQSource{snapshot: issued, found: found}
	offerSource := &fakeOfferVersionSource{listed: versions}
	service := NewService(
		WithIssuedRFQSource(issuedSource),
		WithOfferVersionSource(offerSource),
	)
	return service, issuedSource, offerSource
}

func TestGetComparisonScopesBothLookupsToTheCallerCompany(t *testing.T) {
	issued := comparisonIssuedRFQ(t)
	version := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)})

	service, issuedSource, offerSource := comparisonService(
		issued, true, []OfferVersionSnapshot{version})

	comparison, err := service.GetComparison(context.Background(),
		"company-1", "issued-1", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetComparison: %v", err)
	}
	if issuedSource.company != "company-1" || offerSource.company != "company-1" {
		t.Fatalf("lookups not company-scoped: issued=%q offers=%q",
			issuedSource.company, offerSource.company)
	}
	if len(comparison.Offers) != 1 {
		t.Fatalf("expected one projected offer, got %d", len(comparison.Offers))
	}
}

// A foreign tenant's issued version is simply absent to this caller, so the
// service returns the same not-found another company's ID would produce. It
// must never distinguish "exists elsewhere" from "does not exist".
func TestGetComparisonReturnsNotFoundForForeignIssuedVersion(t *testing.T) {
	service, _, _ := comparisonService(IssuedRFQSnapshot{}, false, nil)

	_, err := service.GetComparison(context.Background(),
		"company-1", "issued-foreign", time.Now().UTC())
	if !errors.Is(err, ErrIssuedRFQNotFound) {
		t.Fatalf("err = %v, want ErrIssuedRFQNotFound", err)
	}
}

// A service missing a capability must refuse rather than silently projecting an
// empty comparison, which would read as "no Supplier responded".
func TestGetComparisonRequiresItsCapabilities(t *testing.T) {
	service := NewService()

	_, err := service.GetComparison(context.Background(),
		"company-1", "issued-1", time.Now().UTC())
	if !errors.Is(err, ErrAwardsNotConfigured) {
		t.Fatalf("err = %v, want ErrAwardsNotConfigured", err)
	}
}

// The projection carries no ranking, so nothing downstream can present a
// system-chosen winner (§8B).
func TestGetComparisonNeverRanksOrRecommends(t *testing.T) {
	issued := comparisonIssuedRFQ(t)
	cheap := eligibleVersion("offer-cheap", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 100)})
	pricey := eligibleVersion("offer-pricey", "supplier-b", "invitation-b", 1,
		[]OfferLineSnapshot{quotedLine("ol-2", "line-1", 900)})

	service, _, _ := comparisonService(issued, true,
		[]OfferVersionSnapshot{pricey, cheap})

	comparison, err := service.GetComparison(context.Background(),
		"company-1", "issued-1", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetComparison: %v", err)
	}
	for _, offer := range comparison.Offers {
		if offer.Rank != 0 || offer.Recommended {
			t.Fatalf("%s carries a rank or recommendation", offer.OfferVersionID)
		}
	}
	if comparison.Authoritative {
		t.Error("a comparison must never claim to be authoritative")
	}
}

// An offer version the adapter returned for a different issued version is
// still projected — it is the "previous RFQ version" label — but it may never
// be selectable, because F3 awards only against the issued version in scope.
func TestGetComparisonMarksOtherIssuedVersionsUnselectable(t *testing.T) {
	issued := comparisonIssuedRFQ(t)
	previous := eligibleVersion("offer-prev", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)})
	previous.IssuedRFQVersionID = "issued-0"

	service, _, _ := comparisonService(issued, true,
		[]OfferVersionSnapshot{previous})

	comparison, err := service.GetComparison(context.Background(),
		"company-1", "issued-1", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetComparison: %v", err)
	}
	offer := comparison.Offers[0]
	if offer.Selectable || offer.IsDefaultView {
		t.Fatal("an offer answering a previous RFQ version is never selectable")
	}
	if offer.Label != ComparisonLabelPreviousRFQVersion {
		t.Errorf("label = %q, want %q", offer.Label, ComparisonLabelPreviousRFQVersion)
	}
}

// An infrastructure failure from a capability propagates rather than becoming
// an empty comparison: reporting "no offers" when the read failed would invite
// a contractor to award against incomplete information.
func TestGetComparisonPropagatesOfferSourceFailure(t *testing.T) {
	issuedSource := &fakeIssuedRFQSource{
		snapshot: comparisonIssuedRFQ(t), found: true}
	boom := errors.New("mongo unavailable")
	offerSource := &fakeOfferVersionSource{err: boom}
	service := NewService(
		WithIssuedRFQSource(issuedSource),
		WithOfferVersionSource(offerSource),
	)

	_, err := service.GetComparison(context.Background(),
		"company-1", "issued-1", time.Now().UTC())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the underlying failure", err)
	}
}

// The indicative subtotal must survive the service boundary still labelled.
func TestGetComparisonSubtotalRemainsIndicative(t *testing.T) {
	issued := comparisonIssuedRFQ(t)
	version := eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
		[]OfferLineSnapshot{
			quotedLine("ol-1", "line-1", 1000),
			quotedLine("ol-2", "line-2", 500),
		})

	service, _, _ := comparisonService(issued, true,
		[]OfferVersionSnapshot{version})

	comparison, err := service.GetComparison(context.Background(),
		"company-1", "issued-1", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetComparison: %v", err)
	}
	subtotal := comparison.Offers[0].IndicativeQuotedSubtotal
	if !subtotal.Indicative {
		t.Fatal("subtotal lost its indicative label crossing the service boundary")
	}
	if subtotal.Amount != money.New(1500, "MYR") {
		t.Errorf("subtotal = %+v, want 1500 MYR", subtotal.Amount)
	}
}
