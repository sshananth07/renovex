package supplieroffers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// sourceResolverRig wires a Service with real Mongo chain/version/eligibility
// repositories and a fake multi-version IssuedRFQSource, which is what the
// M8.1 automatic source resolver needs: it must walk earlier issued RFQ
// versions of the SAME invitation, which live in separate offer chains.
type sourceResolverRig struct {
	service     *Service
	chains      *MongoOfferChainRepository
	versions    *MongoOfferVersionRepository
	eligibility *MongoOfferEligibilityRepository
	issuedRFQ   *fakeMultiVersionIssuedRFQSource
	now         time.Time
}

// fakeMultiVersionIssuedRFQSource resolves several issued RFQ versions by ID,
// unlike fakeIssuedRFQSource which only ever returns one fixed snapshot.
type fakeMultiVersionIssuedRFQSource struct {
	byID map[string]IssuedRFQSnapshot
}

func (f *fakeMultiVersionIssuedRFQSource) GetIssuedRFQForOffer(
	_ context.Context, companyID, versionID string,
) (IssuedRFQSnapshot, bool, error) {
	snapshot, found := f.byID[versionID]
	if !found || snapshot.CompanyID != companyID {
		return IssuedRFQSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

func newSourceResolverRig(t *testing.T) *sourceResolverRig {
	t.Helper()
	db := setupInternalDB(t)
	ctx := context.Background()

	chains := NewMongoOfferChainRepository(db)
	versions := NewMongoOfferVersionRepository(db)
	eligibility := NewMongoOfferEligibilityRepository(db)
	if err := chains.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring chain indexes: %v", err)
	}
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}

	issuedRFQ := &fakeMultiVersionIssuedRFQSource{byID: map[string]IssuedRFQSnapshot{}}
	service := NewService(
		WithSupplierOfferChainRepository(chains),
		WithSupplierOfferVersionRepository(versions),
		WithSupplierOfferEligibilityRepository(eligibility),
		WithIssuedRFQSource(issuedRFQ),
	)

	return &sourceResolverRig{
		service: service, chains: chains, versions: versions,
		eligibility: eligibility, issuedRFQ: issuedRFQ,
		now: time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC),
	}
}

// seedIssuedVersion registers an issued RFQ version's snapshot fact (company,
// RFQ chain, version number) with the fake source.
func (rig *sourceResolverRig) seedIssuedVersion(
	issuedVersionID, companyID, rfqChainID string, versionNumber int,
) {
	rig.issuedRFQ.byID[issuedVersionID] = IssuedRFQSnapshot{
		ID: issuedVersionID, CompanyID: companyID, RFQChainID: rfqChainID,
		VersionNumber: versionNumber, Currency: Phase1Currency,
	}
}

// seedSubmittedChain creates an offer chain for one issued RFQ version and
// gives it a submitted, eligible (non-withdrawn) latest version — the shape a
// prior Supplier submission leaves behind.
func (rig *sourceResolverRig) seedSubmittedChain(
	t *testing.T, companyID, invitationID, issuedVersionID, offerVersionID string,
) SupplierOfferVersion {
	t.Helper()
	ctx := context.Background()

	chain, err := rig.chains.EnsureOfferChain(ctx, companyID, invitationID, issuedVersionID)
	if err != nil {
		t.Fatalf("ensuring chain: %v", err)
	}
	unitPrice := money.New(1_000, Phase1Currency)
	subtotal := money.New(1_000, Phase1Currency)
	version := SupplierOfferVersion{
		ID: offerVersionID, CompanyID: companyID, OfferChainID: chain.ID,
		InvitationID: invitationID, IssuedRFQVersionID: issuedVersionID,
		VersionNumber: 1, Currency: Phase1Currency,
		RecipientIdentity:     "sales@supplier.test",
		SubmissionOperationID: "op-" + offerVersionID,
		Lines: []SupplierOfferLine{{
			ID: "line-1", RFQLineID: "rfq-line-1", ResponseStatus: OfferLineQuoted,
			UnitPriceExcludingTax: &unitPrice, LineSubtotalExcludingTax: &subtotal,
		}},
		Tax:         SupplierOfferTax{Mode: TaxModeNotApplicable},
		SubmittedAt: rig.now.Add(-time.Hour),
	}
	if err := rig.versions.InsertVersion(ctx, version); err != nil {
		t.Fatalf("inserting submitted version: %v", err)
	}
	if _, err := rig.chains.AdvanceChainToVersion(ctx, companyID, chain.ID, chain.Revision,
		version.ID, version.VersionNumber, rig.now.Add(-time.Hour)); err != nil {
		t.Fatalf("advancing chain: %v", err)
	}
	if err := rig.eligibility.InsertEligibility(ctx, SupplierOfferEligibility{
		ID: "eligibility-" + offerVersionID, CompanyID: companyID,
		OfferChainID: chain.ID, OfferVersionID: offerVersionID,
		State: EligibilityEligible, Revision: 1,
	}); err != nil {
		t.Fatalf("inserting eligibility: %v", err)
	}
	return version
}

// withdrawSeededVersion moves a previously seeded version's eligibility gate
// straight to withdrawn, mirroring what a completed withdrawal leaves behind.
func (rig *sourceResolverRig) withdrawSeededVersion(t *testing.T, offerVersionID string) {
	t.Helper()
	ctx := context.Background()
	found, exists, err := rig.eligibility.FindEligibility(ctx, "company-1", offerVersionID)
	if err != nil || !exists {
		t.Fatalf("finding eligibility to withdraw: found=%v err=%v", exists, err)
	}
	claimedAt := rig.now.Add(-30 * time.Minute)
	if _, err := rig.eligibility.ClaimEligibility(ctx, EligibilityClaimInput{
		CompanyID: "company-1", OfferVersionID: offerVersionID,
		ExpectedRevision: found.Revision, ClaimType: EligibilityClaimWithdrawal,
		OperationID: "withdraw-op-" + offerVersionID, ClaimID: "claim-" + offerVersionID,
		WithdrawalReason: "no longer available", ClaimedAt: claimedAt,
	}); err != nil {
		t.Fatalf("claiming withdrawal: %v", err)
	}
	if _, err := rig.eligibility.CompleteEligibilityClaim(ctx, EligibilityCompletionInput{
		CompanyID: "company-1", OfferVersionID: offerVersionID,
		ExpectedRevision: found.Revision + 1, ClaimType: EligibilityClaimWithdrawal,
		OperationID: "withdraw-op-" + offerVersionID, CompletedAt: claimedAt,
	}); err != nil {
		t.Fatalf("completing withdrawal: %v", err)
	}
}

// The resolver finds the eligible latest submission from the immediately
// preceding issued RFQ version of the same invitation.
func TestResolveLatestEligibleSourceFindsImmediatePriorVersion(t *testing.T) {
	rig := newSourceResolverRig(t)
	rig.seedIssuedVersion("issued-v1", "company-1", "rfq-chain-1", 1)
	rig.seedIssuedVersion("issued-v2", "company-1", "rfq-chain-1", 2)
	seeded := rig.seedSubmittedChain(t, "company-1", "invitation-1", "issued-v1", "offer-v1")

	source, found, err := rig.service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v2")
	if err != nil {
		t.Fatalf("resolveLatestEligibleSource: %v", err)
	}
	if !found {
		t.Fatal("resolver did not find the eligible prior submission")
	}
	if source.ID != seeded.ID {
		t.Fatalf("resolved source = %s, want %s", source.ID, seeded.ID)
	}
}

// The resolver must skip an issued RFQ version entirely when the Supplier
// never submitted against it, and keep searching further back.
func TestResolveLatestEligibleSourceSkipsVersionsWithNoSubmission(t *testing.T) {
	rig := newSourceResolverRig(t)
	rig.seedIssuedVersion("issued-v1", "company-1", "rfq-chain-1", 1)
	rig.seedIssuedVersion("issued-v2", "company-1", "rfq-chain-1", 2)
	rig.seedIssuedVersion("issued-v3", "company-1", "rfq-chain-1", 3)
	// The Supplier submitted against v1, but v2 was issued and NOTHING was
	// ever submitted for it (no chain, or a chain with no LatestSubmittedID).
	seeded := rig.seedSubmittedChain(t, "company-1", "invitation-1", "issued-v1", "offer-v1")
	if _, err := rig.chains.EnsureOfferChain(
		context.Background(), "company-1", "invitation-1", "issued-v2"); err != nil {
		t.Fatalf("ensuring empty v2 chain: %v", err)
	}

	source, found, err := rig.service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v3")
	if err != nil {
		t.Fatalf("resolveLatestEligibleSource: %v", err)
	}
	if !found || source.ID != seeded.ID {
		t.Fatalf("resolver = %+v found=%v, want it to skip v2 and land on v1's %s",
			source, found, seeded.ID)
	}
}

// A withdrawn latest submission for the nearest prior version must be
// skipped, and the resolver must fall back to an older eligible submission.
func TestResolveLatestEligibleSourceSkipsWithdrawnAndFallsBack(t *testing.T) {
	rig := newSourceResolverRig(t)
	rig.seedIssuedVersion("issued-v1", "company-1", "rfq-chain-1", 1)
	rig.seedIssuedVersion("issued-v2", "company-1", "rfq-chain-1", 2)
	rig.seedIssuedVersion("issued-v3", "company-1", "rfq-chain-1", 3)

	older := rig.seedSubmittedChain(t, "company-1", "invitation-1", "issued-v1", "offer-v1")
	withdrawn := rig.seedSubmittedChain(t, "company-1", "invitation-1", "issued-v2", "offer-v2")
	rig.withdrawSeededVersion(t, withdrawn.ID)

	source, found, err := rig.service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v3")
	if err != nil {
		t.Fatalf("resolveLatestEligibleSource: %v", err)
	}
	if !found || source.ID != older.ID {
		t.Fatalf("resolver = %+v found=%v, want fallback to the older eligible %s",
			source, found, older.ID)
	}
}

// No eligible prior submission anywhere is a clean not-found, never an error.
func TestResolveLatestEligibleSourceReportsNotFoundWhenNoneEligible(t *testing.T) {
	rig := newSourceResolverRig(t)
	rig.seedIssuedVersion("issued-v1", "company-1", "rfq-chain-1", 1)

	_, found, err := rig.service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v1")
	if err != nil {
		t.Fatalf("resolveLatestEligibleSource: %v", err)
	}
	if found {
		t.Fatal("resolver reported found with no eligible prior submission anywhere")
	}
}

// The resolver never considers the target issued RFQ version's own chain as a
// source: copy-forward always reads an EARLIER version.
func TestResolveLatestEligibleSourceExcludesTheTargetVersionItself(t *testing.T) {
	rig := newSourceResolverRig(t)
	rig.seedIssuedVersion("issued-v1", "company-1", "rfq-chain-1", 1)
	// A submission exists for issued-v1, and issued-v1 IS the target: it must
	// never be selected as its own source.
	rig.seedSubmittedChain(t, "company-1", "invitation-1", "issued-v1", "offer-v1")

	_, found, err := rig.service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v1")
	if err != nil {
		t.Fatalf("resolveLatestEligibleSource: %v", err)
	}
	if found {
		t.Fatal("resolver selected the target's own issued version as its source")
	}
}

// A different company's chain, even for the identically-named invitation and
// issued version identifiers, must never leak into resolution.
func TestResolveLatestEligibleSourceIsCompanyScoped(t *testing.T) {
	rig := newSourceResolverRig(t)
	rig.seedIssuedVersion("issued-v1", "company-1", "rfq-chain-1", 1)
	rig.seedIssuedVersion("issued-v2", "company-1", "rfq-chain-1", 2)
	// Foreign company reuses the exact same invitation/version identifiers.
	rig.issuedRFQ.byID["issued-v1-foreign"] = IssuedRFQSnapshot{
		ID: "issued-v1-foreign", CompanyID: "company-OTHER",
		RFQChainID: "rfq-chain-1", VersionNumber: 1, Currency: Phase1Currency,
	}
	rig.seedSubmittedChain(t, "company-OTHER", "invitation-1", "issued-v1-foreign", "offer-foreign")

	_, found, err := rig.service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v2")
	if err != nil {
		t.Fatalf("resolveLatestEligibleSource: %v", err)
	}
	if found {
		t.Fatal("resolver leaked a foreign company's submission across the tenant boundary")
	}
}

func TestResolveLatestEligibleSourceRequiresConfiguredCapabilities(t *testing.T) {
	service := NewService()
	_, _, err := service.resolveLatestEligibleSource(
		context.Background(), "company-1", "invitation-1", "issued-v1")
	if !errors.Is(err, ErrSupplierOffersNotConfigured) {
		t.Fatalf("error = %v, want not configured", err)
	}
}
