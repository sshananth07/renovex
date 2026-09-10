package supplieroffers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func eligibleGate(versionID string) supplieroffers.SupplierOfferEligibility {
	return supplieroffers.SupplierOfferEligibility{
		ID:             "eligibility-" + versionID,
		CompanyID:      "company-1",
		OfferChainID:   "chain-1",
		OfferVersionID: versionID,
		State:          supplieroffers.EligibilityEligible,
		Revision:       1,
	}
}

func seedEligibleGate(
	t *testing.T,
	repository *supplieroffers.MongoOfferEligibilityRepository,
	versionID string,
) supplieroffers.SupplierOfferEligibility {
	t.Helper()
	gate := eligibleGate(versionID)
	if err := repository.InsertEligibility(context.Background(), gate); err != nil {
		t.Fatalf("seeding eligibility: %v", err)
	}
	return gate
}

// Withdrawal and award compete for the SAME gate, so exactly one can win. This
// is what stops a Supplier withdrawing an offer the contractor is awarding, or
// an award landing on an offer already withdrawn.
func TestConcurrentWithdrawalVersusAwardClaimHasOneWinner(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	gate := seedEligibleGate(t, repository, "offer-version-1")

	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	wg.Add(2)

	var withdrawErr, awardErr error

	go func() {
		defer wg.Done()
		start.Wait()
		_, withdrawErr = repository.ClaimEligibility(ctx,
			supplieroffers.EligibilityClaimInput{
				CompanyID:        gate.CompanyID,
				OfferVersionID:   gate.OfferVersionID,
				ExpectedRevision: gate.Revision,
				ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
				OperationID:      "op-withdraw-1",
				ClaimID:          "withdrawal-1",
				WithdrawalReason: "priced in error",
				ClaimedAt:        now,
			})
	}()
	go func() {
		defer wg.Done()
		start.Wait()
		_, awardErr = repository.ClaimEligibility(ctx,
			supplieroffers.EligibilityClaimInput{
				CompanyID:        gate.CompanyID,
				OfferVersionID:   gate.OfferVersionID,
				ExpectedRevision: gate.Revision,
				ClaimType:        supplieroffers.EligibilityClaimAward,
				OperationID:      "op-award-1",
				ClaimID:          "award-revision-1",
				ClaimedAt:        now,
			})
	}()

	start.Done()
	wg.Wait()

	withdrawWon := withdrawErr == nil
	awardWon := awardErr == nil
	if withdrawWon && awardWon {
		t.Fatal("both withdrawal and award claimed the same offer version: " +
			"an awarded offer could be withdrawn underneath the contractor")
	}
	if !withdrawWon && !awardWon {
		t.Fatalf("neither won: withdraw = %v, award = %v", withdrawErr, awardErr)
	}

	final, found, err := repository.FindEligibility(ctx, gate.CompanyID, gate.OfferVersionID)
	if err != nil || !found {
		t.Fatalf("reloading the gate: %v", err)
	}
	if withdrawWon && final.State != supplieroffers.EligibilityWithdrawalClaimed {
		t.Errorf("state = %q, want withdrawal_claimed", final.State)
	}
	if awardWon && final.State != supplieroffers.EligibilityAwardClaimed {
		t.Errorf("state = %q, want award_claimed", final.State)
	}
}

// A withdrawal claim persists its reason atomically with the state change, so
// a withdrawn offer always carries why.
func TestWithdrawalClaimPersistsItsReasonAtomically(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	gate := seedEligibleGate(t, repository, "offer-version-1")

	claimed, err := repository.ClaimEligibility(ctx, supplieroffers.EligibilityClaimInput{
		CompanyID:        gate.CompanyID,
		OfferVersionID:   gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
		OperationID:      "op-withdraw-1",
		ClaimID:          "withdrawal-1",
		WithdrawalReason: "priced in error",
		ClaimedAt:        now,
	})
	if err != nil {
		t.Fatalf("ClaimEligibility: %v", err)
	}

	if claimed.WithdrawalReason != "priced in error" {
		t.Errorf("WithdrawalReason = %q, want the supplied reason",
			claimed.WithdrawalReason)
	}
	if claimed.ClaimType != supplieroffers.EligibilityClaimWithdrawal ||
		claimed.OperationID != "op-withdraw-1" ||
		claimed.ClaimID != "withdrawal-1" {
		t.Errorf("claim identity = %+v, want the exact withdrawal identity", claimed)
	}
	if claimed.ClaimedAt == nil {
		t.Error("ClaimedAt must be stamped on a claim")
	}
	if claimed.Revision != gate.Revision+1 {
		t.Errorf("Revision = %d, want one increment", claimed.Revision)
	}
}

// A withdrawal without a reason is refused: an unexplained withdrawal gives the
// contractor nothing to act on.
func TestWithdrawalClaimRequiresAReason(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	gate := seedEligibleGate(t, repository, "offer-version-1")

	if _, err := repository.ClaimEligibility(ctx, supplieroffers.EligibilityClaimInput{
		CompanyID:        gate.CompanyID,
		OfferVersionID:   gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
		OperationID:      "op-withdraw-1",
		ClaimID:          "withdrawal-1",
		ClaimedAt:        time.Now().UTC(),
	}); !errors.Is(err, supplieroffers.ErrInvalidOfferEligibility) {
		t.Fatalf("reasonless withdrawal error = %v, want a validation refusal", err)
	}
}

// An award claim carrying a withdrawal reason is rejected: the two claim kinds
// must not be able to impersonate one another.
func TestAwardClaimRejectsAWithdrawalReason(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	gate := seedEligibleGate(t, repository, "offer-version-1")

	if _, err := repository.ClaimEligibility(ctx, supplieroffers.EligibilityClaimInput{
		CompanyID:        gate.CompanyID,
		OfferVersionID:   gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimAward,
		OperationID:      "op-award-1",
		ClaimID:          "award-revision-1",
		WithdrawalReason: "should not be here",
		ClaimedAt:        time.Now().UTC(),
	}); !errors.Is(err, supplieroffers.ErrInvalidOfferEligibility) {
		t.Fatalf("award-with-reason error = %v, want a validation refusal", err)
	}
}

// Same-operation retry converges without a second revision increment.
func TestEligibilityClaimIsIdempotentForTheSameOperation(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	gate := seedEligibleGate(t, repository, "offer-version-1")

	input := supplieroffers.EligibilityClaimInput{
		CompanyID:        gate.CompanyID,
		OfferVersionID:   gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
		OperationID:      "op-withdraw-1",
		ClaimID:          "withdrawal-1",
		WithdrawalReason: "priced in error",
		ClaimedAt:        now,
	}

	first, err := repository.ClaimEligibility(ctx, input)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	retry, err := repository.ClaimEligibility(ctx, input)
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}
	if retry.Revision != first.Revision {
		t.Errorf("retry revision = %d, want the original %d",
			retry.Revision, first.Revision)
	}
}

// Completion moves a claim to its terminal state and stamps completion.
func TestCompleteEligibilityClaimReachesTheTerminalState(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	gate := seedEligibleGate(t, repository, "offer-version-1")

	claimed, err := repository.ClaimEligibility(ctx, supplieroffers.EligibilityClaimInput{
		CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
		OperationID:      "op-withdraw-1", ClaimID: "withdrawal-1",
		WithdrawalReason: "priced in error", ClaimedAt: now,
	})
	if err != nil {
		t.Fatalf("ClaimEligibility: %v", err)
	}

	completed, err := repository.CompleteEligibilityClaim(ctx,
		supplieroffers.EligibilityCompletionInput{
			CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
			ExpectedRevision: claimed.Revision,
			ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
			OperationID:      "op-withdraw-1",
			CompletedAt:      now.Add(time.Second),
		})
	if err != nil {
		t.Fatalf("CompleteEligibilityClaim: %v", err)
	}

	if completed.State != supplieroffers.EligibilityWithdrawn {
		t.Errorf("state = %q, want withdrawn", completed.State)
	}
	if completed.CompletedAt == nil {
		t.Error("CompletedAt must be stamped on completion")
	}
	// The reason survives completion: it is immutable once claimed.
	if completed.WithdrawalReason != "priced in error" {
		t.Errorf("WithdrawalReason = %q, want it preserved",
			completed.WithdrawalReason)
	}
}

// Only an award claim may be released, and only by its exact owner. A withdrawn
// or withdrawal-claimed gate can never return to eligible.
func TestReleaseAwardClaimOnlyForItsExactOwner(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	gate := seedEligibleGate(t, repository, "offer-version-1")

	claimed, err := repository.ClaimEligibility(ctx, supplieroffers.EligibilityClaimInput{
		CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimAward,
		OperationID:      "op-award-1", ClaimID: "award-revision-1",
		ClaimedAt: now,
	})
	if err != nil {
		t.Fatalf("ClaimEligibility: %v", err)
	}

	// A different operation cannot release this claim.
	if _, err := repository.ReleaseAwardClaim(ctx,
		supplieroffers.EligibilityReleaseInput{
			CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
			ExpectedRevision: claimed.Revision,
			OperationID:      "op-award-OTHER", ClaimID: "award-revision-1",
			ReleasedAt: now,
		}); !errors.Is(err, supplieroffers.ErrInvalidOfferEligibility) {
		t.Fatalf("foreign release error = %v, want a refusal", err)
	}

	// The owning operation may release, returning the gate to eligible.
	released, err := repository.ReleaseAwardClaim(ctx,
		supplieroffers.EligibilityReleaseInput{
			CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
			ExpectedRevision: claimed.Revision,
			OperationID:      "op-award-1", ClaimID: "award-revision-1",
			ReleasedAt: now,
		})
	if err != nil {
		t.Fatalf("ReleaseAwardClaim: %v", err)
	}
	if released.State != supplieroffers.EligibilityEligible {
		t.Errorf("state = %q, want eligible after release", released.State)
	}
	if released.ClaimType != "" || released.OperationID != "" ||
		released.ClaimID != "" || released.ClaimedAt != nil {
		t.Error("release must clear every award claim field")
	}
}

// A withdrawal claim can NEVER be released back to eligible: withdrawal is the
// Supplier's decision and must not be silently undone.
func TestReleaseRefusesAWithdrawalClaim(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	gate := seedEligibleGate(t, repository, "offer-version-1")

	claimed, err := repository.ClaimEligibility(ctx, supplieroffers.EligibilityClaimInput{
		CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
		ExpectedRevision: gate.Revision,
		ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
		OperationID:      "op-withdraw-1", ClaimID: "withdrawal-1",
		WithdrawalReason: "priced in error", ClaimedAt: now,
	})
	if err != nil {
		t.Fatalf("ClaimEligibility: %v", err)
	}

	if _, err := repository.ReleaseAwardClaim(ctx,
		supplieroffers.EligibilityReleaseInput{
			CompanyID: gate.CompanyID, OfferVersionID: gate.OfferVersionID,
			ExpectedRevision: claimed.Revision,
			OperationID:      "op-withdraw-1", ClaimID: "withdrawal-1",
			ReleasedAt: now,
		}); !errors.Is(err, supplieroffers.ErrInvalidOfferEligibility) {
		t.Fatalf("withdrawal release error = %v, want a refusal", err)
	}
}
