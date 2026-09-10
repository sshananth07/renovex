package awards_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// F4 award line claims (§8E).
//
// This is the SECOND serialization point, distinct from Phase E's per-Offer
// eligibility gate (§8A.2). Its scope is CompanyID + RFQChainID +
// StableLineageID, and it is what makes "one Supplier per line, ACROSS every
// issued version" true (D4). Conflating the two is a design error.

func claimRepository(t *testing.T) *awards.MongoAwardLineClaimRepository {
	t.Helper()
	repository := awards.NewMongoAwardLineClaimRepository(setupDB(t))
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repository
}

func claimInput(lineage, operation, revision string) awards.AwardLineClaimInput {
	return awards.AwardLineClaimInput{
		CompanyID: "company-1", RFQChainID: "rfqchain-1",
		StableLineageID:     lineage,
		IssuedRFQVersionID:  "issued-1",
		AwardOperationID:    operation,
		CandidateRevisionID: revision,
	}
}

func TestClaimLineageRecordsItsOwningOperation(t *testing.T) {
	repository := claimRepository(t)

	claim, err := repository.ClaimLineage(context.Background(),
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if claim.State != awards.LineClaimClaimed {
		t.Fatalf("state = %q, want claimed", claim.State)
	}
	if claim.AwardOperationID != "op-1" {
		t.Errorf("operation = %q, want op-1", claim.AwardOperationID)
	}
}

// The unique index is what prevents two Suppliers winning one line. A second
// LIVE operation must lose with a distinct, retryable conflict.
func TestASecondOperationCannotClaimALiveLineage(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	if _, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1")); err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}

	_, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-2", "candidate-2"))
	if !errors.Is(err, awards.ErrRFQLineAwardConflict) {
		t.Fatalf("err = %v, want ErrRFQLineAwardConflict", err)
	}
}

// An already-AWARDED lineage is terminal: it fails with the distinct
// already-awarded error, because no retry can ever make it available (§8G).
func TestAnAwardedLineageRejectsEveryNewClaim(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if err := repository.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"op-1", "revision-1", claim.Revision); err != nil {
		t.Fatalf("MarkLineageAwarded: %v", err)
	}

	_, err = repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-2", "candidate-2"))
	if !errors.Is(err, awards.ErrRFQLineAlreadyAwarded) {
		t.Fatalf("err = %v, want ErrRFQLineAlreadyAwarded", err)
	}
}

// D4: the claim spans the RFQ CHAIN, so a later issued version cannot award a
// lineage an earlier version already awarded.
func TestAnAwardedLineageBlocksAnotherIssuedVersion(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if err := repository.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"op-1", "revision-1", claim.Revision); err != nil {
		t.Fatalf("MarkLineageAwarded: %v", err)
	}

	fromLaterVersion := claimInput("lineage-1", "op-2", "candidate-2")
	fromLaterVersion.IssuedRFQVersionID = "issued-2"

	if _, err := repository.ClaimLineage(ctx, fromLaterVersion); !errors.Is(
		err, awards.ErrRFQLineAlreadyAwarded) {
		t.Fatalf("err = %v, want ErrRFQLineAlreadyAwarded across issued versions",
			err)
	}
}

// Lineage IDs may repeat in another company, so the uniqueness scope must
// include CompanyID.
func TestLineClaimsAreTenantScoped(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	if _, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1")); err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}

	other := claimInput("lineage-1", "op-2", "candidate-2")
	other.CompanyID = "company-2"
	if _, err := repository.ClaimLineage(ctx, other); err != nil {
		t.Fatalf("another company must claim the same lineage freely: %v", err)
	}
}

// A same-operation retry converges on its own claim: a timeout does not prove
// the claim failed, and failing here would strand a recoverable finalisation.
func TestClaimLineageIsIdempotentForItsOwnOperation(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	first, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	second, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("same-operation retry must converge: %v", err)
	}
	if first.ID != second.ID || first.Revision != second.Revision {
		t.Fatalf("retry created or mutated a claim: %+v then %+v", first, second)
	}
}

// Concurrent finalisations over one lineage: exactly one wins. This is the
// guarantee that stops two Suppliers being told they won the same line.
func TestConcurrentLineageClaimsProduceExactlyOneWinner(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	const attempts = 8
	results := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			_, results[index] = repository.ClaimLineage(ctx, claimInput(
				"lineage-1",
				fmt.Sprintf("op-%d", index),
				fmt.Sprintf("candidate-%d", index)))
		}(index)
	}
	wait.Wait()

	winners := 0
	for index, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, awards.ErrRFQLineAwardConflict),
			errors.Is(err, awards.ErrRFQLineAlreadyAwarded):
		default:
			t.Fatalf("attempt %d: unexpected error %v", index, err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent lineage claims produced %d winners, want 1", winners)
	}
}

// Release is narrow by design: only the OWNING operation may release its own
// claim, and only while it is still `claimed`.
func TestReleaseLineageClaimRefusesAForeignOperation(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}

	if err := repository.ReleaseLineageClaim(
		ctx, "company-1", claim.ID, "op-2", claim.Revision); err == nil {
		t.Fatal("a foreign operation must never release another's claim")
	}

	// The claim survives the attempt untouched.
	current, found, err := repository.FindClaim(ctx, "company-1", claim.ID)
	if err != nil || !found {
		t.Fatalf("FindClaim: found=%v err=%v", found, err)
	}
	if current.State != awards.LineClaimClaimed ||
		current.AwardOperationID != "op-1" {
		t.Fatalf("claim was disturbed by a foreign release: %+v", current)
	}
}

// An AWARDED claim is terminal and is never released — not by its own
// operation, not by reconciliation, not by any Phase F path (§8G). Releasing it
// would let a competitor be awarded a line a Supplier was already told they won.
func TestAnAwardedLineageClaimIsNeverReleased(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if err := repository.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"op-1", "revision-1", claim.Revision); err != nil {
		t.Fatalf("MarkLineageAwarded: %v", err)
	}

	awarded, _, err := repository.FindClaim(ctx, "company-1", claim.ID)
	if err != nil {
		t.Fatalf("FindClaim: %v", err)
	}
	if err := repository.ReleaseLineageClaim(ctx, "company-1", claim.ID,
		"op-1", awarded.Revision); err == nil {
		t.Fatal("an awarded claim must never be released, even by its owner")
	}
}

// Releasing frees the lineage for a genuinely different award decision, which
// is how an abandoned finalisation is recovered.
func TestReleasingAClaimFreesTheLineage(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if err := repository.ReleaseLineageClaim(
		ctx, "company-1", claim.ID, "op-1", claim.Revision); err != nil {
		t.Fatalf("ReleaseLineageClaim: %v", err)
	}

	if _, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-2", "candidate-2")); err != nil {
		t.Fatalf("a released lineage must be claimable again: %v", err)
	}
}

// Marking awarded requires the owning operation: another operation completing
// someone else's claim would attribute an award to the wrong decision.
func TestMarkLineageAwardedRequiresTheOwningOperation(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if err := repository.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"op-2", "revision-1", claim.Revision); err == nil {
		t.Fatal("a foreign operation must not complete another's claim")
	}
}

// Completion is idempotent for its own operation: post-publication work is
// recoverable and may run more than once (D2).
func TestMarkLineageAwardedIsIdempotent(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	claim, err := repository.ClaimLineage(ctx,
		claimInput("lineage-1", "op-1", "candidate-1"))
	if err != nil {
		t.Fatalf("ClaimLineage: %v", err)
	}
	if err := repository.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"op-1", "revision-1", claim.Revision); err != nil {
		t.Fatalf("MarkLineageAwarded: %v", err)
	}

	awarded, _, err := repository.FindClaim(ctx, "company-1", claim.ID)
	if err != nil {
		t.Fatalf("FindClaim: %v", err)
	}
	if err := repository.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"op-1", "revision-1", awarded.Revision); err != nil {
		t.Fatalf("repeat completion must be idempotent: %v", err)
	}
}

// Listing an operation's own claims is what makes "release ONLY your own
// claims, in reverse order" implementable after a partial acquisition.
func TestListClaimsForOperationReturnsOnlyThatOperationsClaims(t *testing.T) {
	repository := claimRepository(t)
	ctx := context.Background()

	for _, lineage := range []string{"lineage-1", "lineage-2"} {
		if _, err := repository.ClaimLineage(ctx,
			claimInput(lineage, "op-1", "candidate-1")); err != nil {
			t.Fatalf("ClaimLineage(%s): %v", lineage, err)
		}
	}
	if _, err := repository.ClaimLineage(ctx,
		claimInput("lineage-3", "op-2", "candidate-2")); err != nil {
		t.Fatalf("ClaimLineage(lineage-3): %v", err)
	}

	claims, err := repository.ListClaimsForOperation(ctx, "company-1", "op-1")
	if err != nil {
		t.Fatalf("ListClaimsForOperation: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2", len(claims))
	}
	for _, claim := range claims {
		if claim.AwardOperationID != "op-1" {
			t.Errorf("listed a foreign claim: %+v", claim)
		}
	}
}
