package supplieroffers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// The narrow award-integration surface (M8 §8A.1).
//
// Phase F consumes Phase E through consumer-owned capability interfaces. This
// module provides them, and nothing wider: only immutable Offer Version
// snapshots leave, claim state stays enforced here, and no Award model enters.

func awardIntegrationRepos(t *testing.T) (
	*supplieroffers.MongoOfferVersionRepository,
	*supplieroffers.MongoOfferEligibilityRepository,
) {
	t.Helper()
	db := setupDB(t)
	versions := supplieroffers.NewMongoOfferVersionRepository(db)
	eligibility := supplieroffers.NewMongoOfferEligibilityRepository(db)
	ctx := context.Background()
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("version EnsureIndexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("eligibility EnsureIndexes: %v", err)
	}
	return versions, eligibility
}

func awardTestVersion(
	chainID, issuedVersionID string, versionNumber int,
) supplieroffers.SupplierOfferVersion {
	subtotal := money.New(10_000, "MYR")
	return supplieroffers.SupplierOfferVersion{
		// A distinct identity per version: Offer Versions are immutable
		// records, and two of them never share one.
		ID:        fmt.Sprintf("version-%s-%d", chainID, versionNumber),
		CompanyID: "company-1", OfferChainID: chainID,
		InvitationID: "invitation-" + chainID, IssuedRFQVersionID: issuedVersionID,
		VersionNumber: versionNumber, Currency: "MYR",
		RecipientIdentity:     "buyer@example.test",
		SourceDraftID:         "draft-" + chainID,
		SubmissionOperationID: fmt.Sprintf("op-%s-%d", chainID, versionNumber),
		SubmissionFingerprint: "fingerprint",
		Lines: []supplieroffers.SupplierOfferLine{{
			ID: "ol-1", RFQLineID: "line-1",
			ResponseStatus:           supplieroffers.OfferLineQuoted,
			LineSubtotalExcludingTax: &subtotal,
			LineTaxAmount:            money.New(0, "MYR"),
		}},
		Tax:                  supplieroffers.SupplierOfferTax{Mode: supplieroffers.TaxModeNotApplicable},
		QuotedLineSubtotal:   subtotal,
		QuotedTaxTotal:       money.New(0, "MYR"),
		FullOfferChargeTotal: money.New(0, "MYR"),
		DeliveryChargeTotal:  money.New(0, "MYR"),
		GrandTotal:           subtotal,
		OfferValidUntil:      time.Now().UTC().Add(720 * time.Hour),
		SubmittedAt:          time.Now().UTC(),
		SchemaVersion:        supplieroffers.SupplierOfferVersionSchemaVersion,
	}
}

// The list returns every immutable version answering one issued RFQ version.
// Phase F needs the FACTS; it decides latest-eligible, labels and participation
// itself.
func TestListVersionsForIssuedRFQVersionReturnsEveryVersion(t *testing.T) {
	versions, _ := awardIntegrationRepos(t)
	ctx := context.Background()

	for _, candidate := range []supplieroffers.SupplierOfferVersion{
		awardTestVersion("chain-a", "issued-1", 1),
		awardTestVersion("chain-a", "issued-1", 2),
		awardTestVersion("chain-b", "issued-1", 1),
		// A version answering a DIFFERENT issued version must not appear.
		awardTestVersion("chain-c", "issued-2", 1),
	} {
		if err := versions.InsertVersion(ctx, candidate); err != nil {
			t.Fatalf("InsertVersion: %v", err)
		}
	}

	listed, err := versions.ListVersionsForIssuedRFQVersion(
		ctx, "company-1", "issued-1")
	if err != nil {
		t.Fatalf("ListVersionsForIssuedRFQVersion: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("versions = %d, want the 3 answering issued-1", len(listed))
	}
	for _, version := range listed {
		if version.IssuedRFQVersionID != "issued-1" {
			t.Errorf("listed a version for %q", version.IssuedRFQVersionID)
		}
	}
}

// Ordering is deterministic — chain then version number — so a comparison
// projection renders the same way on every read.
func TestListVersionsForIssuedRFQVersionIsDeterministicallyOrdered(t *testing.T) {
	versions, _ := awardIntegrationRepos(t)
	ctx := context.Background()

	// Inserted out of order on purpose.
	for _, candidate := range []supplieroffers.SupplierOfferVersion{
		awardTestVersion("chain-b", "issued-1", 1),
		awardTestVersion("chain-a", "issued-1", 2),
		awardTestVersion("chain-a", "issued-1", 1),
	} {
		if err := versions.InsertVersion(ctx, candidate); err != nil {
			t.Fatalf("InsertVersion: %v", err)
		}
	}

	listed, err := versions.ListVersionsForIssuedRFQVersion(
		ctx, "company-1", "issued-1")
	if err != nil {
		t.Fatalf("ListVersionsForIssuedRFQVersion: %v", err)
	}

	want := []struct {
		chain  string
		number int
	}{
		{"chain-a", 1}, {"chain-a", 2}, {"chain-b", 1},
	}
	if len(listed) != len(want) {
		t.Fatalf("versions = %d, want %d", len(listed), len(want))
	}
	for index, expected := range want {
		if listed[index].OfferChainID != expected.chain ||
			listed[index].VersionNumber != expected.number {
			t.Fatalf("position %d = %s/%d, want %s/%d", index,
				listed[index].OfferChainID, listed[index].VersionNumber,
				expected.chain, expected.number)
		}
	}
}

// Tenant scope is mandatory: another company's versions are never listed.
func TestListVersionsForIssuedRFQVersionIsTenantScoped(t *testing.T) {
	versions, _ := awardIntegrationRepos(t)
	ctx := context.Background()

	if err := versions.InsertVersion(
		ctx, awardTestVersion("chain-a", "issued-1", 1)); err != nil {
		t.Fatalf("InsertVersion: %v", err)
	}

	listed, err := versions.ListVersionsForIssuedRFQVersion(
		ctx, "company-2", "issued-1")
	if err != nil {
		t.Fatalf("ListVersionsForIssuedRFQVersion: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("a foreign company listed %d versions, want 0", len(listed))
	}
}

// The award claim surface enforces state, operation and claim identity HERE.
// awards never reaches the eligibility collection itself.
func TestAwardEligibilityClaimLifecycle(t *testing.T) {
	_, eligibility := awardIntegrationRepos(t)
	ctx := context.Background()

	if err := eligibility.InsertEligibility(ctx,
		supplieroffers.SupplierOfferEligibility{
			ID: "elig-1", CompanyID: "company-1", OfferChainID: "chain-a",
			OfferVersionID: "offer-1",
			State:          supplieroffers.EligibilityEligible, Revision: 1,
		}); err != nil {
		t.Fatalf("InsertEligibility: %v", err)
	}
	seeded, _, err := eligibility.FindEligibility(ctx, "company-1", "offer-1")
	if err != nil {
		t.Fatalf("FindEligibility: %v", err)
	}

	claimed, err := eligibility.ClaimEligibility(ctx,
		supplieroffers.EligibilityClaimInput{
			CompanyID: "company-1", OfferVersionID: "offer-1",
			ClaimType:   supplieroffers.EligibilityClaimAward,
			OperationID: "award-op-1", ClaimID: "candidate-1",
			ExpectedRevision: seeded.Revision,
			ClaimedAt:        time.Now().UTC(),
		})
	if err != nil {
		t.Fatalf("ClaimEligibility: %v", err)
	}
	if claimed.State != supplieroffers.EligibilityAwardClaimed {
		t.Fatalf("state = %q, want award_claimed", claimed.State)
	}

	// A DIFFERENT operation cannot complete another's claim.
	if _, err := eligibility.CompleteEligibilityClaim(ctx,
		supplieroffers.EligibilityCompletionInput{
			CompanyID: "company-1", OfferVersionID: "offer-1",
			ClaimType:        supplieroffers.EligibilityClaimAward,
			OperationID:      "intruder-op",
			ExpectedRevision: claimed.Revision,
			CompletedAt:      time.Now().UTC(),
		}); err == nil {
		t.Fatal("a foreign operation must not complete another's claim")
	}

	completed, err := eligibility.CompleteEligibilityClaim(ctx,
		supplieroffers.EligibilityCompletionInput{
			CompanyID: "company-1", OfferVersionID: "offer-1",
			ClaimType:        supplieroffers.EligibilityClaimAward,
			OperationID:      "award-op-1",
			ExpectedRevision: claimed.Revision,
			CompletedAt:      time.Now().UTC(),
		})
	if err != nil {
		t.Fatalf("CompleteEligibilityClaim: %v", err)
	}
	if completed.State != supplieroffers.EligibilityAwarded {
		t.Fatalf("state = %q, want awarded", completed.State)
	}
}

// Release is narrow: only the owning operation, holding the exact claim ID,
// while still `award_claimed`.
func TestAwardEligibilityReleaseRefusesAForeignOperation(t *testing.T) {
	_, eligibility := awardIntegrationRepos(t)
	ctx := context.Background()

	if err := eligibility.InsertEligibility(ctx,
		supplieroffers.SupplierOfferEligibility{
			ID: "elig-1", CompanyID: "company-1", OfferChainID: "chain-a",
			OfferVersionID: "offer-1",
			State:          supplieroffers.EligibilityEligible, Revision: 1,
		}); err != nil {
		t.Fatalf("InsertEligibility: %v", err)
	}
	seeded, _, err := eligibility.FindEligibility(ctx, "company-1", "offer-1")
	if err != nil {
		t.Fatalf("FindEligibility: %v", err)
	}
	claimed, err := eligibility.ClaimEligibility(ctx,
		supplieroffers.EligibilityClaimInput{
			CompanyID: "company-1", OfferVersionID: "offer-1",
			ClaimType:   supplieroffers.EligibilityClaimAward,
			OperationID: "award-op-1", ClaimID: "candidate-1",
			ExpectedRevision: seeded.Revision, ClaimedAt: time.Now().UTC(),
		})
	if err != nil {
		t.Fatalf("ClaimEligibility: %v", err)
	}

	if _, err := eligibility.ReleaseAwardClaim(ctx,
		supplieroffers.EligibilityReleaseInput{
			CompanyID: "company-1", OfferVersionID: "offer-1",
			OperationID: "intruder-op", ClaimID: "candidate-1",
			ExpectedRevision: claimed.Revision,
		}); !errors.Is(err, supplieroffers.ErrInvalidOfferEligibility) {
		t.Fatalf("err = %v, want a refusal for a foreign operation", err)
	}

	// The wrong claim ID is refused even for the right operation.
	if _, err := eligibility.ReleaseAwardClaim(ctx,
		supplieroffers.EligibilityReleaseInput{
			CompanyID: "company-1", OfferVersionID: "offer-1",
			OperationID: "award-op-1", ClaimID: "wrong-candidate",
			ExpectedRevision: claimed.Revision,
		}); err == nil {
		t.Fatal("a mismatched claim ID must be refused")
	}
}
