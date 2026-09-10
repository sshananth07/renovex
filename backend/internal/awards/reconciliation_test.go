package awards

import (
	"context"
	"errors"
	"testing"
)

// F10 award reconciliation (§8K).
//
// Reconciliation NEVER invents a selection, a total, a reason, a revision
// number, a candidate identity or a commercial record; never rolls the chain
// backward; and never releases a claim once a matching revision exists.

func reconcileInput(operationID string) ReconcileAwardInput {
	return ReconcileAwardInput{
		IssuedRFQVersionID: "issued-1",
		OperationID:        operationID,
	}
}

// chain finalising, revision EXISTS -> complete forward: advance the chain,
// mark gates and claims awarded, generate missing outcomes, emit missing audit.
func TestReconcileCompletesForwardWhenARevisionExists(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	revision, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}

	// Simulate the crash window: the revision exists, but the chain never
	// advanced past `finalising`.
	chain := harness.chains.chains["chain-issued-1"]
	chain.FinalisationState = FinalisationFinalising
	chain.CurrentAwardRevisionID = nil
	chain.LatestRevisionNumber = 0
	chain.FinalisingOperationID = "op-1"
	chain.FinalisingRevisionID = revision.ID
	harness.chains.chains["chain-issued-1"] = chain
	harness.claims.released = nil

	result, err := harness.service.ReconcileAward(
		ctx, "company-1", "user-1", reconcileInput("recon-1"))
	if err != nil {
		t.Fatalf("ReconcileAward: %v", err)
	}
	if !result.CompletedForward {
		t.Fatal("a revision exists, so reconciliation must complete forward")
	}

	advanced := harness.chains.chains["chain-issued-1"]
	if advanced.FinalisationState != FinalisationPublished {
		t.Errorf("chain state = %q, want published", advanced.FinalisationState)
	}
	if advanced.CurrentAwardRevisionID == nil ||
		*advanced.CurrentAwardRevisionID != revision.ID {
		t.Error("the chain must point at the authoritative revision")
	}
	// The claims must never be released once a revision exists (D2).
	if len(harness.claims.released) != 0 {
		t.Fatalf("released %v; release is prohibited once a revision exists",
			harness.claims.released)
	}
}

// chain finalising, NO revision, operation abandoned -> verify no revision,
// release ONLY that operation's claims, return the chain to draft.
func TestReconcileReleasesAnAbandonedOperationsOwnClaims(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	// An operation that claimed but never published.
	chain := harness.chains.chains["chain-issued-1"]
	if _, err := harness.claims.ClaimLineage(ctx, AwardLineClaimInput{
		CompanyID: "company-1", RFQChainID: "rfqchain-1",
		StableLineageID: "lineage-1", AwardOperationID: "abandoned-op",
		CandidateRevisionID: "candidate-abandoned",
	}); err != nil {
		t.Fatalf("seeding claim: %v", err)
	}
	chain.FinalisationState = FinalisationFinalising
	chain.FinalisingOperationID = "abandoned-op"
	chain.FinalisingRevisionID = "candidate-abandoned"
	harness.chains.chains["chain-issued-1"] = chain
	harness.claims.released = nil

	result, err := harness.service.ReconcileAward(
		ctx, "company-1", "user-1", reconcileInput("recon-1"))
	if err != nil {
		t.Fatalf("ReconcileAward: %v", err)
	}
	if result.CompletedForward {
		t.Fatal("no revision exists, so nothing may be completed forward")
	}
	if !result.ReleasedClaims {
		t.Fatal("an abandoned operation's own claims must be released")
	}

	if len(harness.claims.released) != 1 ||
		harness.claims.released[0] != "lineage-1" {
		t.Fatalf("released %v, want the abandoned operation's own lineage",
			harness.claims.released)
	}
	returned := harness.chains.chains["chain-issued-1"]
	if returned.FinalisationState != FinalisationDraft {
		t.Errorf("chain state = %q, want draft so the contractor may re-decide",
			returned.FinalisationState)
	}
}

// A claim owned by ANOTHER operation is never released, even while recovering.
func TestReconcileNeverReleasesAForeignOperationsClaims(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	// A different, still-live operation holds a lineage on this chain.
	if _, err := harness.claims.ClaimLineage(ctx, AwardLineClaimInput{
		CompanyID: "company-1", RFQChainID: "rfqchain-1",
		StableLineageID: "lineage-other", AwardOperationID: "other-op",
		CandidateRevisionID: "candidate-other",
	}); err != nil {
		t.Fatalf("seeding foreign claim: %v", err)
	}

	chain := harness.chains.chains["chain-issued-1"]
	chain.FinalisationState = FinalisationFinalising
	chain.FinalisingOperationID = "abandoned-op"
	chain.FinalisingRevisionID = "candidate-abandoned"
	harness.chains.chains["chain-issued-1"] = chain
	harness.claims.released = nil

	if _, err := harness.service.ReconcileAward(
		ctx, "company-1", "user-1", reconcileInput("recon-1")); err != nil {
		t.Fatalf("ReconcileAward: %v", err)
	}

	for _, lineage := range harness.claims.released {
		if lineage == "lineage-other" {
			t.Fatal("reconciliation released another operation's claim")
		}
	}
	if _, found, _ := harness.claims.FindClaimByLineage(
		ctx, "company-1", "rfqchain-1", "lineage-other"); !found {
		t.Fatal("the foreign claim was destroyed")
	}
}

// A fully complete chain reconciles idempotently: nothing changes.
func TestReconcileIsIdempotentWhenEverythingIsComplete(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	revision, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}
	before := harness.chains.chains["chain-issued-1"]
	harness.claims.released = nil

	result, err := harness.service.ReconcileAward(
		ctx, "company-1", "user-1", reconcileInput("recon-1"))
	if err != nil {
		t.Fatalf("ReconcileAward: %v", err)
	}
	if result.AwardRevisionID != revision.ID {
		t.Errorf("reconciliation resolved %q, want the current revision %q",
			result.AwardRevisionID, revision.ID)
	}

	after := harness.chains.chains["chain-issued-1"]
	if after.LatestRevisionNumber != before.LatestRevisionNumber ||
		after.FinalisationState != before.FinalisationState {
		t.Fatalf("reconciliation moved a complete chain: %+v then %+v",
			before, after)
	}
	if len(harness.claims.released) != 0 {
		t.Fatal("a complete chain must have nothing released")
	}
}

// Reconciliation never rolls the chain backward: a published chain stays
// published, and its revision number never decreases.
func TestReconcileNeverRollsThePublishedChainBackward(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}

	for attempt := 0; attempt < 3; attempt++ {
		if _, err := harness.service.ReconcileAward(ctx, "company-1", "user-1",
			reconcileInput("recon-1")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	chain := harness.chains.chains["chain-issued-1"]
	if chain.FinalisationState != FinalisationPublished {
		t.Fatalf("chain state = %q, want published", chain.FinalisationState)
	}
	if chain.LatestRevisionNumber != 1 {
		t.Fatalf("revision number = %d, want 1", chain.LatestRevisionNumber)
	}
}

// Audit follows the ensure-once rule: reconciliation records a MISSING event
// and no-ops when it already exists, so exactly one exists per revision.
func TestReconcileRecordsAMissingAuditEventExactlyOnce(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	// Publish with the audit sink down, leaving the award unaudited.
	harness.audit.err = errors.New("audit sink down")
	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}
	if len(harness.audit.recorded) != 0 {
		t.Fatal("the failing sink recorded an event")
	}

	// The sink recovers; reconciliation must repair the gap.
	harness.audit.err = nil
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := harness.service.ReconcileAward(ctx, "company-1", "user-1",
			reconcileInput("recon-1")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	finalised := 0
	for _, event := range harness.audit.events {
		if event == "award_finalised" {
			finalised++
		}
	}
	if finalised != 1 {
		t.Fatalf("award_finalised recorded %d times after reconciliation, want 1",
			finalised)
	}
}

// A chain with no award at all is a bounded not-found: reconciliation never
// invents a revision.
func TestReconcileReturnsNotFoundWithNoAwardChain(t *testing.T) {
	harness := newFinalisationHarness(t)

	if _, err := harness.service.ReconcileAward(context.Background(),
		"company-1", "user-1", reconcileInput("recon-1")); !errors.Is(
		err, ErrAwardChainNotFound) {
		t.Fatalf("err = %v, want ErrAwardChainNotFound", err)
	}
}

// Reconciliation regenerates MISSING outcomes without duplicating existing ones.
func TestReconcileGeneratesOnlyMissingOutcomes(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}
	before := len(harness.outcomes.outcomes)

	if _, err := harness.service.ReconcileAward(ctx, "company-1", "user-1",
		reconcileInput("recon-1")); err != nil {
		t.Fatalf("ReconcileAward: %v", err)
	}
	if len(harness.outcomes.outcomes) != before {
		t.Fatalf("outcomes = %d after reconciliation, want the same %d",
			len(harness.outcomes.outcomes), before)
	}
}
