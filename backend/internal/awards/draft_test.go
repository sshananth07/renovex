package awards

import (
	"errors"
	"testing"
)

// F2 domain rules (§8C).
//
// A provisional draft claims nothing and has no externally visible effect, so
// completeness is validated at FINALISATION rather than on every edit — a
// contractor must be able to work incrementally.

func TestUnawardedReasonsAreBounded(t *testing.T) {
	// The four bounded reasons stand alone; only `other` takes a note, which
	// TestUnawardedOtherRequiresExplanatoryText covers.
	bounded := []UnawardedReason{
		UnawardedNoAcceptableOffer,
		UnawardedPurchaseDeferred,
		UnawardedScopeCancelled,
		UnawardedRetenderRequired,
	}
	for _, reason := range bounded {
		if err := ValidateUnawardedReason(reason, ""); err != nil {
			t.Errorf("%q must be accepted: %v", reason, err)
		}
	}

	// Case matters and a near-miss is not close enough: an unrecognised reason
	// must never be silently accepted as an explanation.
	for _, reason := range []UnawardedReason{"", "because", "no_bid", "OTHER"} {
		if err := ValidateUnawardedReason(reason, ""); !errors.Is(
			err, ErrInvalidUnawardedReason) {
			t.Errorf("%q must be refused, got %v", reason, err)
		}
	}
}

// "other" is the escape hatch, so it must carry an explanation. Without one it
// records no more than the absence of a decision, which defeats the purpose of
// preserving why something was deliberately not bought.
func TestUnawardedOtherRequiresExplanatoryText(t *testing.T) {
	if err := ValidateUnawardedReason(UnawardedOther, "   "); !errors.Is(
		err, ErrInvalidUnawardedReason) {
		t.Fatalf("other without text must be refused, got %v", err)
	}
	if err := ValidateUnawardedReason(UnawardedOther, "buying direct"); err != nil {
		t.Fatalf("other with text must be accepted: %v", err)
	}
}

// A bounded reason must NOT carry free text: allowing both would let a caller
// smuggle an explanation past the bounded set and make the reason unreliable
// for later auditing of retender_required.
func TestBoundedUnawardedReasonRejectsANote(t *testing.T) {
	if err := ValidateUnawardedReason(
		UnawardedPurchaseDeferred, "some note"); !errors.Is(
		err, ErrInvalidUnawardedReason) {
		t.Fatalf("a bounded reason must not carry a note, got %v", err)
	}
}

func selectedDecision(rfqLineID, lineageID string) AwardLineDecisionDraft {
	return AwardLineDecisionDraft{
		IssuedRFQLineID: rfqLineID,
		StableLineageID: lineageID,
		Decision:        AwardDecisionSelected,
		OfferVersionID:  "offer-1",
		OfferLineID:     "ol-1",
	}
}

// A selected decision must name the offer version and line it selects: without
// both, F3 has nothing authoritative to calculate from.
func TestSelectedDecisionRequiresItsOfferReferences(t *testing.T) {
	decision := selectedDecision("line-1", "lineage-1")
	decision.OfferVersionID = ""
	if err := decision.Validate(); err == nil {
		t.Fatal("a selected decision without an offer version must be refused")
	}

	decision = selectedDecision("line-1", "lineage-1")
	decision.OfferLineID = ""
	if err := decision.Validate(); err == nil {
		t.Fatal("a selected decision without an offer line must be refused")
	}
}

// An unawarded decision must NOT carry offer references: a line the contractor
// declined to buy has no Supplier, and recording one would make the revision
// ambiguous about whether it was awarded.
func TestUnawardedDecisionRejectsOfferReferences(t *testing.T) {
	decision := AwardLineDecisionDraft{
		IssuedRFQLineID: "line-1",
		StableLineageID: "lineage-1",
		Decision:        AwardDecisionUnawarded,
		UnawardedReason: UnawardedScopeCancelled,
		OfferVersionID:  "offer-1",
	}
	if err := decision.Validate(); err == nil {
		t.Fatal("an unawarded decision must not name an offer version")
	}
}

// Every issued line appears exactly once. A duplicate would let one lineage be
// both awarded and unawarded, and a lineage claim cannot resolve that.
func TestDraftRejectsDuplicateDecisionsForOneIssuedLine(t *testing.T) {
	draft := AwardDraft{
		ID: "draft-1", CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", Status: AwardDraftOpen, Revision: 1,
		LineDecisions: []AwardLineDecisionDraft{
			selectedDecision("line-1", "lineage-1"),
			selectedDecision("line-1", "lineage-1"),
		},
	}
	if err := draft.Validate(); err == nil {
		t.Fatal("a duplicate decision for one issued line must be refused")
	}
}

// Completeness is NOT an edit-time rule: an incomplete draft is the normal
// state while a contractor works through the lines.
func TestOpenDraftMayBeIncomplete(t *testing.T) {
	draft := AwardDraft{
		ID: "draft-1", CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", Status: AwardDraftOpen, Revision: 1,
		LineDecisions: []AwardLineDecisionDraft{
			selectedDecision("line-1", "lineage-1"),
		},
	}
	if err := draft.Validate(); err != nil {
		t.Fatalf("an incomplete open draft is valid: %v", err)
	}

	// The completeness rule exists, but only finalisation applies it.
	issued := []IssuedRFQLineSnapshot{
		{ID: "line-1", LineageID: "lineage-1"},
		{ID: "line-2", LineageID: "lineage-2"},
	}
	if err := draft.ValidateComplete(issued); err == nil {
		t.Fatal("finalisation must require every issued line to be decided")
	}
}

func TestCompleteDraftPassesFinalisationCompleteness(t *testing.T) {
	draft := AwardDraft{
		ID: "draft-1", CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", Status: AwardDraftOpen, Revision: 1,
		LineDecisions: []AwardLineDecisionDraft{
			selectedDecision("line-1", "lineage-1"),
			{
				IssuedRFQLineID: "line-2", StableLineageID: "lineage-2",
				Decision: AwardDecisionUnawarded, UnawardedReason: UnawardedScopeCancelled,
			},
		},
	}
	issued := []IssuedRFQLineSnapshot{
		{ID: "line-1", LineageID: "lineage-1"},
		{ID: "line-2", LineageID: "lineage-2"},
	}
	if err := draft.ValidateComplete(issued); err != nil {
		t.Fatalf("a complete draft must pass: %v", err)
	}
}

// A decision naming a line that is not on the issued version is refused: it
// would award something the Supplier was never asked to quote.
func TestFinalisationRejectsADecisionForAnUnknownIssuedLine(t *testing.T) {
	draft := AwardDraft{
		ID: "draft-1", CompanyID: "company-1", AwardChainID: "chain-1",
		IssuedRFQVersionID: "issued-1", Status: AwardDraftOpen, Revision: 1,
		LineDecisions: []AwardLineDecisionDraft{
			selectedDecision("line-ghost", "lineage-ghost"),
		},
	}
	issued := []IssuedRFQLineSnapshot{{ID: "line-1", LineageID: "lineage-1"}}
	if err := draft.ValidateComplete(issued); err == nil {
		t.Fatal("a decision for an unknown issued line must be refused")
	}
}

// Only an open draft mutates. The chain's finalisation state is a separate
// guard; this one keeps an archived draft from being edited after publication.
func TestOnlyOpenDraftsAllowMutation(t *testing.T) {
	open := AwardDraft{Status: AwardDraftOpen}
	if !open.AllowsMutation() {
		t.Error("an open draft must allow mutation")
	}
	archived := AwardDraft{Status: AwardDraftArchived}
	if archived.AllowsMutation() {
		t.Error("an archived draft must never allow mutation")
	}
}

// The chain's durable finalisation state is what prevents a second operation
// from reading a stale pointer and publishing a duplicate revision (D2).
func TestChainAllowsDraftMutationOnlyWhileInDraftState(t *testing.T) {
	cases := []struct {
		state FinalisationState
		want  bool
	}{
		{FinalisationDraft, true},
		{FinalisationFinalising, false},
		{FinalisationPublished, false},
	}
	for _, tc := range cases {
		chain := AwardDecisionChain{FinalisationState: tc.state}
		if got := chain.AllowsDraftMutation(); got != tc.want {
			t.Errorf("state %q allows mutation = %v, want %v",
				tc.state, got, tc.want)
		}
	}
}

// A chain in `finalising` must name the operation and candidate revision it is
// publishing: without them, a retry cannot tell its own in-flight publication
// apart from another operation's.
func TestFinalisingChainMustNameItsOperationAndCandidate(t *testing.T) {
	chain := AwardDecisionChain{
		ID: "chain-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
		IssuedRFQVersionID: "issued-1", FinalisationState: FinalisationFinalising,
		Revision: 1,
	}
	if err := chain.Validate(); err == nil {
		t.Fatal("a finalising chain without its operation ID must be refused")
	}

	chain.FinalisingOperationID = "op-1"
	if err := chain.Validate(); err == nil {
		t.Fatal("a finalising chain without its candidate revision must be refused")
	}

	chain.FinalisingRevisionID = "revision-candidate-1"
	if err := chain.Validate(); err != nil {
		t.Fatalf("a well-formed finalising chain must be accepted: %v", err)
	}
}

// A published chain must point at the revision it published. A published state
// with no pointer would make "the current award" unresolvable.
func TestPublishedChainMustPointAtItsRevision(t *testing.T) {
	chain := AwardDecisionChain{
		ID: "chain-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
		IssuedRFQVersionID: "issued-1", FinalisationState: FinalisationPublished,
		LatestRevisionNumber: 1, Revision: 2,
	}
	if err := chain.Validate(); err == nil {
		t.Fatal("a published chain with no current revision must be refused")
	}

	current := "revision-1"
	chain.CurrentAwardRevisionID = &current
	if err := chain.Validate(); err != nil {
		t.Fatalf("a well-formed published chain must be accepted: %v", err)
	}
}
