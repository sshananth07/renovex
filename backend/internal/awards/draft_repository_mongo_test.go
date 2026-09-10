package awards_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// F2 persistence (§8C). The two guarantees that cannot be proven without real
// MongoDB: one Award chain per issued version, and one OPEN draft per chain.

func chainRepository(t *testing.T) *awards.MongoAwardChainRepository {
	t.Helper()
	repository := awards.NewMongoAwardChainRepository(setupDB(t))
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repository
}

func TestEnsureAwardChainIsIdempotentPerIssuedVersion(t *testing.T) {
	repository := chainRepository(t)
	ctx := context.Background()

	first, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}
	second, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain (repeat): %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("two chains created for one issued version: %s vs %s",
			first.ID, second.ID)
	}
	if first.FinalisationState != awards.FinalisationDraft {
		t.Errorf("new chain state = %q, want draft", first.FinalisationState)
	}
}

// D4: each Issued RFQ Version owns an INDEPENDENT Award chain. A later version
// never supersedes an earlier award, so the two must not share a chain.
func TestEachIssuedVersionOwnsAnIndependentAwardChain(t *testing.T) {
	repository := chainRepository(t)
	ctx := context.Background()

	first, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}
	second, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-2")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("two issued versions must not share one award chain (D4)")
	}
}

// Identifier values may repeat in another company, so the uniqueness scope must
// include CompanyID or one tenant could collide with another's chain.
func TestAwardChainsAreTenantScoped(t *testing.T) {
	repository := chainRepository(t)
	ctx := context.Background()

	ours, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}
	theirs, err := repository.EnsureAwardChain(ctx, "company-2", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}
	if ours.ID == theirs.ID {
		t.Fatal("two companies must not share an award chain")
	}

	if _, found, err := repository.FindChain(ctx, "company-2", ours.ID); err != nil {
		t.Fatalf("FindChain: %v", err)
	} else if found {
		t.Fatal("a foreign company must not resolve another tenant's chain")
	}
}

// Concurrent first access must converge on the document the unique index
// protects rather than creating competing chains.
func TestConcurrentEnsureAwardChainProducesExactlyOneChain(t *testing.T) {
	repository := chainRepository(t)
	ctx := context.Background()

	const attempts = 8
	ids := make([]string, attempts)
	errs := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			chain, err := repository.EnsureAwardChain(
				ctx, "company-1", "rfqchain-1", "issued-1")
			ids[index], errs[index] = chain.ID, err
		}(index)
	}
	wait.Wait()

	unique := map[string]bool{}
	for index, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: %v", index, err)
		}
		unique[ids[index]] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent ensure produced %d chains, want exactly 1", len(unique))
	}
}

func draftRepository(t *testing.T) (
	*awards.MongoAwardDraftRepository, *awards.MongoAwardChainRepository) {
	t.Helper()
	db := setupDB(t)
	drafts := awards.NewMongoAwardDraftRepository(db)
	chains := awards.NewMongoAwardChainRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("draft EnsureIndexes: %v", err)
	}
	if err := chains.EnsureIndexes(ctx); err != nil {
		t.Fatalf("chain EnsureIndexes: %v", err)
	}
	return drafts, chains
}

// One OPEN draft per chain, enforced by a partial unique index. Two open drafts
// would let two people award the same issued version from different decisions.
func TestEnsureOpenDraftReturnsTheExistingOpenDraft(t *testing.T) {
	drafts, _ := draftRepository(t)
	ctx := context.Background()

	first, created, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft: %v", err)
	}
	if !created {
		t.Fatal("the first EnsureOpenDraft must report creation")
	}

	second, created, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-2",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft (repeat): %v", err)
	}
	if created {
		t.Error("a second EnsureOpenDraft must adopt, not create")
	}
	if first.ID != second.ID {
		t.Fatalf("two open drafts exist: %s and %s", first.ID, second.ID)
	}
	// The adopted draft keeps its original author: adoption is not a takeover.
	if second.CreatedByUserID != "user-1" {
		t.Errorf("adopted draft author = %q, want user-1", second.CreatedByUserID)
	}
}

func TestConcurrentEnsureOpenDraftProducesExactlyOneDraft(t *testing.T) {
	drafts, _ := draftRepository(t)
	ctx := context.Background()

	const attempts = 8
	ids := make([]string, attempts)
	errs := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			draft, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
				CompanyID: "company-1", AwardChainID: "chain-1",
				IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
			})
			ids[index], errs[index] = draft.ID, err
		}(index)
	}
	wait.Wait()

	unique := map[string]bool{}
	for index, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: %v", index, err)
		}
		unique[ids[index]] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent ensure produced %d open drafts, want exactly 1",
			len(unique))
	}
}

// An archived draft frees the slot: after finalisation the contractor may open
// a fresh draft, which is how a failed finalisation is re-decided.
func TestArchivingADraftFreesTheOpenSlot(t *testing.T) {
	drafts, _ := draftRepository(t)
	ctx := context.Background()

	first, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft: %v", err)
	}
	if err := drafts.ArchiveDraft(ctx, "company-1", first.ID, first.Revision); err != nil {
		t.Fatalf("ArchiveDraft: %v", err)
	}

	second, created, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft after archive: %v", err)
	}
	if !created || second.ID == first.ID {
		t.Fatal("archiving must free the open-draft slot")
	}
}

// Every edit is an open-only, expected-revision CAS. A stale writer must lose
// cleanly rather than overwriting a concurrent decision.
func TestReplaceDecisionsRefusesAStaleRevision(t *testing.T) {
	drafts, _ := draftRepository(t)
	ctx := context.Background()

	draft, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft: %v", err)
	}

	decisions := []awards.AwardLineDecisionDraft{{
		IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
		Decision:       awards.AwardDecisionSelected,
		OfferVersionID: "offer-1", OfferLineID: "ol-1",
	}}
	updated, err := drafts.ReplaceDecisions(
		ctx, "company-1", draft.ID, draft.Revision, decisions)
	if err != nil {
		t.Fatalf("ReplaceDecisions: %v", err)
	}
	if updated.Revision != draft.Revision+1 {
		t.Fatalf("revision = %d, want %d", updated.Revision, draft.Revision+1)
	}

	// The same expected revision again is exactly the stale-writer case.
	if _, err := drafts.ReplaceDecisions(
		ctx, "company-1", draft.ID, draft.Revision, decisions); !errors.Is(
		err, awards.ErrAwardDraftConflict) {
		t.Fatalf("stale write err = %v, want ErrAwardDraftConflict", err)
	}
}

// Concurrent edits must produce exactly one winner: an award decision silently
// lost to a racing write is a commercial error, not a UI inconvenience.
func TestConcurrentDecisionEditsProduceExactlyOneWinner(t *testing.T) {
	drafts, _ := draftRepository(t)
	ctx := context.Background()

	draft, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft: %v", err)
	}

	const attempts = 8
	results := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			_, results[index] = drafts.ReplaceDecisions(
				ctx, "company-1", draft.ID, draft.Revision,
				[]awards.AwardLineDecisionDraft{{
					IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
					Decision:       awards.AwardDecisionSelected,
					OfferVersionID: "offer-1",
					OfferLineID:    fmt.Sprintf("ol-%d", index),
				}})
		}(index)
	}
	wait.Wait()

	winners := 0
	for index, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, awards.ErrAwardDraftConflict):
		default:
			t.Fatalf("attempt %d: unexpected error %v", index, err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent edits produced %d winners, want exactly 1", winners)
	}
}

// A foreign tenant must not edit a draft even holding its exact ID and
// revision: the company is part of every CAS filter, not a post-hoc check.
func TestDraftEditsAreTenantScoped(t *testing.T) {
	drafts, _ := draftRepository(t)
	ctx := context.Background()

	draft, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft: %v", err)
	}

	if _, err := drafts.ReplaceDecisions(
		ctx, "company-2", draft.ID, draft.Revision,
		[]awards.AwardLineDecisionDraft{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			Decision:       awards.AwardDecisionSelected,
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
		}}); err == nil {
		t.Fatal("a foreign company must not edit another tenant's draft")
	}

	if _, found, err := drafts.FindOpenDraft(ctx, "company-2", "chain-1"); err != nil {
		t.Fatalf("FindOpenDraft: %v", err)
	} else if found {
		t.Fatal("a foreign company must not read another tenant's draft")
	}
}

// The chain's finalisation CAS is what stops a second operation publishing a
// duplicate revision number from a stale pointer (D2).
func TestClaimFinalisationHasExactlyOneWinner(t *testing.T) {
	repository := chainRepository(t)
	ctx := context.Background()

	chain, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}

	const attempts = 8
	results := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			_, results[index] = repository.ClaimFinalisation(ctx,
				awards.FinalisationClaimInput{
					CompanyID:           "company-1",
					AwardChainID:        chain.ID,
					ExpectedRevision:    chain.Revision,
					OperationID:         fmt.Sprintf("op-%d", index),
					CandidateRevisionID: fmt.Sprintf("candidate-%d", index),
				})
		}(index)
	}
	wait.Wait()

	winners := 0
	for index, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, awards.ErrAwardRevisionConflict):
		default:
			t.Fatalf("attempt %d: unexpected error %v", index, err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent finalisation claims produced %d winners, want 1",
			winners)
	}
}

// A same-operation retry must converge on its own claim rather than conflict:
// a timeout does not prove the claim failed, and re-numbering would violate D2.
func TestClaimFinalisationIsIdempotentForItsOwnOperation(t *testing.T) {
	repository := chainRepository(t)
	ctx := context.Background()

	chain, err := repository.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}

	input := awards.FinalisationClaimInput{
		CompanyID: "company-1", AwardChainID: chain.ID,
		ExpectedRevision: chain.Revision,
		OperationID:      "op-1", CandidateRevisionID: "candidate-1",
	}
	first, err := repository.ClaimFinalisation(ctx, input)
	if err != nil {
		t.Fatalf("ClaimFinalisation: %v", err)
	}

	// The retry carries the ORIGINAL expected revision, exactly as a crashed
	// caller would after losing the response.
	second, err := repository.ClaimFinalisation(ctx, input)
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("retry incremented the revision: %d then %d",
			first.Revision, second.Revision)
	}
	if second.FinalisingRevisionID != "candidate-1" {
		t.Errorf("retry changed the candidate revision to %q",
			second.FinalisingRevisionID)
	}
}

// Draft mutation is refused while the chain is finalising: the decisions an
// in-flight publication is calculating from must not move underneath it.
func TestDraftMutationIsRefusedWhileTheChainIsFinalising(t *testing.T) {
	drafts, chains := draftRepository(t)
	ctx := context.Background()

	chain, err := chains.EnsureAwardChain(ctx, "company-1", "rfqchain-1", "issued-1")
	if err != nil {
		t.Fatalf("EnsureAwardChain: %v", err)
	}
	draft, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
		CompanyID: "company-1", AwardChainID: chain.ID,
		IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("EnsureOpenDraft: %v", err)
	}

	if _, err := chains.ClaimFinalisation(ctx, awards.FinalisationClaimInput{
		CompanyID: "company-1", AwardChainID: chain.ID,
		ExpectedRevision: chain.Revision,
		OperationID:      "op-1", CandidateRevisionID: "candidate-1",
	}); err != nil {
		t.Fatalf("ClaimFinalisation: %v", err)
	}

	reloaded, found, err := chains.FindChain(ctx, "company-1", chain.ID)
	if err != nil || !found {
		t.Fatalf("FindChain: found=%v err=%v", found, err)
	}
	if reloaded.AllowsDraftMutation() {
		t.Fatal("a finalising chain must refuse draft mutation")
	}
	// The draft row itself is untouched — the guard is the chain state, which
	// is what makes the refusal survive a crash mid-finalisation.
	if !draft.AllowsMutation() {
		t.Error("the draft row is still open; the chain state is the guard")
	}
}
