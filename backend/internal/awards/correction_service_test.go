package awards

import (
	"context"
	"errors"
	"testing"
)

// F6 correction service and its route contract (§8G).

// prepareCorrectionBase publishes revision 1 awarding line-1, then reopens a
// draft so a correction can be prepared against it.
func (h *finalisationHarness) prepareCorrectionBase(t *testing.T) AwardRevision {
	t.Helper()
	ctx := context.Background()

	h.prepareCompleteDraft(t)
	revision, err := h.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}
	return revision
}

// A correction supersedes without mutating: revision 1 survives byte for byte.
func TestCorrectAwardSupersedesWithoutMutatingThePriorRevision(t *testing.T) {
	harness := newFinalisationHarness(t)
	first := harness.prepareCorrectionBase(t)
	ctx := context.Background()

	corrected, err := harness.service.CorrectAward(ctx, "company-1", "user-1",
		CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "added a line missed at finalisation",
			Selections: []AwardLineSelection{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
			}},
		})
	if err != nil {
		t.Fatalf("CorrectAward: %v", err)
	}

	if corrected.RevisionNumber != 2 {
		t.Fatalf("revision number = %d, want 2", corrected.RevisionNumber)
	}
	if corrected.SupersedesRevisionID == nil ||
		*corrected.SupersedesRevisionID != first.ID {
		t.Fatal("a correction must name the revision it supersedes")
	}

	// The prior revision is untouched.
	stored := harness.revisions.revisions[first.ID]
	if stored.SelectionFingerprint != first.SelectionFingerprint ||
		stored.GrandAwardTotal != first.GrandAwardTotal {
		t.Fatal("the prior revision was mutated; revisions are immutable")
	}
}

// A change reason is required: a superseding record that cannot say why it
// happened is not auditable later.
func TestCorrectAwardRequiresAChangeReason(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)

	_, err := harness.service.CorrectAward(context.Background(),
		"company-1", "user-1", CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "   ",
			Selections: []AwardLineSelection{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
			}},
		})
	if !errors.Is(err, ErrChangeReasonRequired) {
		t.Fatalf("err = %v, want ErrChangeReasonRequired", err)
	}
}

// Monotonicity holds through the service: dropping the awarded lineage is
// refused before anything is written.
func TestCorrectAwardRefusesANonMonotonicCorrection(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)
	before := len(harness.revisions.revisions)

	_, err := harness.service.CorrectAward(context.Background(),
		"company-1", "user-1", CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "dropping the award",
			Selections:         []AwardLineSelection{},
		})
	if !errors.Is(err, ErrAwardCorrectionNotMonotonic) {
		t.Fatalf("err = %v, want ErrAwardCorrectionNotMonotonic", err)
	}
	if len(harness.revisions.revisions) != before {
		t.Fatal("a refused correction must publish nothing")
	}
}

// Awarded gates and lineage claims are TERMINAL: a correction never releases
// them, which is what stops a competitor being awarded a line already won.
func TestCorrectAwardNeverReleasesTerminalClaims(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)
	harness.claims.released = nil
	ctx := context.Background()

	if _, err := harness.service.CorrectAward(ctx, "company-1", "user-1",
		CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "metadata only",
			Selections: []AwardLineSelection{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
			}},
		}); err != nil {
		t.Fatalf("CorrectAward: %v", err)
	}

	if len(harness.claims.released) != 0 {
		t.Fatalf("released %v; awarded claims are terminal (§8G)",
			harness.claims.released)
	}
	for _, claim := range harness.claims.claims {
		if claim.State != LineClaimAwarded {
			t.Errorf("lineage %q state = %q, want it to stay awarded",
				claim.StableLineageID, claim.State)
		}
	}
}

// A correction is refused while the chain is still finalising: the decisions an
// in-flight publication is calculating from must not move underneath it.
func TestCorrectAwardIsRefusedWhileFinalising(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)

	chain := harness.chains.chains["chain-issued-1"]
	chain.FinalisationState = FinalisationFinalising
	chain.FinalisingOperationID = "op-9"
	chain.FinalisingRevisionID = "candidate-9"
	harness.chains.chains["chain-issued-1"] = chain

	if _, err := harness.service.CorrectAward(context.Background(),
		"company-1", "user-1", CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "while finalising",
			Selections: []AwardLineSelection{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
			}},
		}); !errors.Is(err, ErrAwardRevisionConflict) {
		t.Fatalf("err = %v, want ErrAwardRevisionConflict", err)
	}
}

// A correction with no published award to correct is a bounded not-found.
func TestCorrectAwardRequiresAPublishedAward(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)

	if _, err := harness.service.CorrectAward(context.Background(),
		"company-1", "user-1", CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "nothing to correct",
			Selections: []AwardLineSelection{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
			}},
		}); !errors.Is(err, ErrAwardRevisionNotFound) {
		t.Fatalf("err = %v, want ErrAwardRevisionNotFound", err)
	}
}

// The correction emits `award_corrected`, not `award_finalised`: the two are
// different events with different Supplier-facing meaning.
func TestCorrectAwardEmitsTheCorrectionAuditEvent(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)
	ctx := context.Background()

	if _, err := harness.service.CorrectAward(ctx, "company-1", "user-1",
		CorrectAwardInput{
			IssuedRFQVersionID: "issued-1",
			OperationID:        "op-2",
			ChangeReason:       "corrected metadata",
			Selections: []AwardLineSelection{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				OfferVersionID: "offer-1", OfferLineID: "ol-1",
			}},
		}); err != nil {
		t.Fatalf("CorrectAward: %v", err)
	}

	corrections := 0
	for _, event := range harness.audit.events {
		if event == "award_corrected" {
			corrections++
		}
	}
	if corrections != 1 {
		t.Fatalf("audit events = %v, want exactly one award_corrected",
			harness.audit.events)
	}
}

// Ensure-once applies to corrections too: a repeated correction records one
// event, not one per attempt.
func TestCorrectAwardAuditIsEnsuredOnce(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)
	ctx := context.Background()

	input := CorrectAwardInput{
		IssuedRFQVersionID: "issued-1",
		OperationID:        "op-2",
		ChangeReason:       "corrected metadata",
		Selections: []AwardLineSelection{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
		}},
	}
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := harness.service.CorrectAward(
			ctx, "company-1", "user-1", input); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	corrections := 0
	for _, event := range harness.audit.events {
		if event == "award_corrected" {
			corrections++
		}
	}
	if corrections != 1 {
		t.Fatalf("award_corrected recorded %d times, want exactly 1", corrections)
	}
}

// A same-operation retry adopts the existing correction rather than publishing
// revision 3: a timeout does not prove the insert failed.
func TestCorrectAwardRetryAdoptsTheExistingCorrection(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCorrectionBase(t)
	ctx := context.Background()

	input := CorrectAwardInput{
		IssuedRFQVersionID: "issued-1",
		OperationID:        "op-2",
		ChangeReason:       "corrected metadata",
		Selections: []AwardLineSelection{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
		}},
	}
	first, err := harness.service.CorrectAward(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("CorrectAward: %v", err)
	}
	second, err := harness.service.CorrectAward(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if first.ID != second.ID || second.RevisionNumber != 2 {
		t.Fatalf("retry produced %+v, want the same revision 2", second)
	}
	if len(harness.revisions.revisions) != 2 {
		t.Fatalf("revisions = %d, want 2 (original plus one correction)",
			len(harness.revisions.revisions))
	}
}
