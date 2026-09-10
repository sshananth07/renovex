package awards

import (
	"context"
	"errors"
	"testing"
	"time"
)

// F2 service behavior (§8C). The draft service composes the chain and draft
// repositories with the issued-RFQ capability; it never reads another module's
// collection and never accepts a Company from request content.

type fakeChainStore struct {
	chains  map[string]AwardDecisionChain
	byIssue map[string]string
	err     error
}

func newFakeChainStore() *fakeChainStore {
	return &fakeChainStore{
		chains:  map[string]AwardDecisionChain{},
		byIssue: map[string]string{},
	}
}

func (f *fakeChainStore) EnsureAwardChain(
	_ context.Context, companyID, rfqChainID, issuedRFQVersionID string,
) (AwardDecisionChain, error) {
	if f.err != nil {
		return AwardDecisionChain{}, f.err
	}
	key := companyID + "|" + issuedRFQVersionID
	if id, ok := f.byIssue[key]; ok {
		return f.chains[id], nil
	}
	chain := AwardDecisionChain{
		ID: "chain-" + issuedRFQVersionID, CompanyID: companyID,
		RFQChainID: rfqChainID, IssuedRFQVersionID: issuedRFQVersionID,
		FinalisationState: FinalisationDraft, Revision: 1,
	}
	f.chains[chain.ID] = chain
	f.byIssue[key] = chain.ID
	return chain, nil
}

func (f *fakeChainStore) FindChain(
	_ context.Context, companyID, chainID string,
) (AwardDecisionChain, bool, error) {
	chain, ok := f.chains[chainID]
	if !ok || chain.CompanyID != companyID {
		return AwardDecisionChain{}, false, f.err
	}
	return chain, true, f.err
}

func (f *fakeChainStore) FindChainByIssuedVersion(
	_ context.Context, companyID, issuedRFQVersionID string,
) (AwardDecisionChain, bool, error) {
	id, ok := f.byIssue[companyID+"|"+issuedRFQVersionID]
	if !ok {
		return AwardDecisionChain{}, false, f.err
	}
	return f.chains[id], true, f.err
}

type fakeDraftStore struct {
	drafts  map[string]AwardDraft
	byChain map[string]string
	nextID  int
	err     error
}

func newFakeDraftStore() *fakeDraftStore {
	return &fakeDraftStore{
		drafts:  map[string]AwardDraft{},
		byChain: map[string]string{},
	}
}

func (f *fakeDraftStore) EnsureOpenDraft(
	_ context.Context, candidate AwardDraft,
) (AwardDraft, bool, error) {
	if f.err != nil {
		return AwardDraft{}, false, f.err
	}
	key := candidate.CompanyID + "|" + candidate.AwardChainID
	if id, ok := f.byChain[key]; ok {
		return f.drafts[id], false, nil
	}
	f.nextID++
	draft := candidate
	draft.ID = "draft-1"
	draft.Status = AwardDraftOpen
	draft.Revision = 1
	f.drafts[draft.ID] = draft
	f.byChain[key] = draft.ID
	return draft, true, nil
}

func (f *fakeDraftStore) FindOpenDraft(
	_ context.Context, companyID, awardChainID string,
) (AwardDraft, bool, error) {
	id, ok := f.byChain[companyID+"|"+awardChainID]
	if !ok {
		return AwardDraft{}, false, f.err
	}
	return f.drafts[id], true, f.err
}

func (f *fakeDraftStore) FindDraft(
	_ context.Context, companyID, draftID string,
) (AwardDraft, bool, error) {
	draft, ok := f.drafts[draftID]
	if !ok || draft.CompanyID != companyID {
		return AwardDraft{}, false, f.err
	}
	return draft, true, f.err
}

func (f *fakeDraftStore) ReplaceDecisions(
	_ context.Context, companyID, draftID string,
	expectedRevision int64, decisions []AwardLineDecisionDraft,
) (AwardDraft, error) {
	draft, ok := f.drafts[draftID]
	if !ok || draft.CompanyID != companyID ||
		draft.Status != AwardDraftOpen || draft.Revision != expectedRevision {
		return AwardDraft{}, ErrAwardDraftConflict
	}
	draft.LineDecisions = decisions
	draft.Revision++
	f.drafts[draftID] = draft
	return draft, nil
}

func (f *fakeDraftStore) ArchiveDraft(
	_ context.Context, companyID, draftID string, expectedRevision int64,
) error {
	draft, ok := f.drafts[draftID]
	if !ok || draft.CompanyID != companyID ||
		draft.Status != AwardDraftOpen || draft.Revision != expectedRevision {
		return ErrAwardDraftConflict
	}
	draft.Status = AwardDraftArchived
	draft.Revision++
	f.drafts[draftID] = draft
	delete(f.byChain, companyID+"|"+draft.AwardChainID)
	return nil
}

func draftService(t *testing.T) (
	*Service, *fakeChainStore, *fakeDraftStore, *fakeAwardAuditRecorder) {
	t.Helper()
	chains := newFakeChainStore()
	drafts := newFakeDraftStore()
	audit := &fakeAwardAuditRecorder{}
	offers := &fakeOfferVersionSource{versions: map[string]OfferVersionSnapshot{
		"offer-1": eligibleVersion("offer-1", "supplier-a", "invitation-a", 1,
			[]OfferLineSnapshot{quotedLine("ol-1", "line-1", 1000)}),
	}}
	service := NewService(
		WithIssuedRFQSource(&fakeIssuedRFQSource{
			snapshot: comparisonIssuedRFQ(t), found: true}),
		WithOfferVersionSource(offers),
		WithAwardChainRepository(chains),
		WithAwardDraftRepository(drafts),
		WithAwardAuditRecorder(audit),
	)
	return service, chains, drafts, audit
}

// Creating a draft ensures the chain first: a draft with no chain would have
// nothing to finalise against.
func TestCreateAwardDraftEnsuresTheChainAndEmitsAudit(t *testing.T) {
	service, chains, _, audit := draftService(t)

	draft, err := service.CreateAwardDraft(
		context.Background(), "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}
	if draft.Status != AwardDraftOpen {
		t.Fatalf("draft status = %q, want open", draft.Status)
	}
	if _, found, _ := chains.FindChainByIssuedVersion(
		context.Background(), "company-1", "issued-1"); !found {
		t.Fatal("creating a draft must ensure its award chain")
	}
	if len(audit.events) != 1 || audit.events[0] != "award_draft_created" {
		t.Fatalf("audit events = %v, want [award_draft_created]", audit.events)
	}
}

// A second create adopts the existing open draft and emits NO audit event: no
// authoritative write happened, and an audit trail padded with non-events makes
// the real ones harder to find.
func TestCreateAwardDraftAdoptsWithoutASecondAuditEvent(t *testing.T) {
	service, _, _, audit := draftService(t)
	ctx := context.Background()

	first, err := service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}
	second, err := service.CreateAwardDraft(ctx, "company-1", "user-2", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft (repeat): %v", err)
	}
	if first.ID != second.ID {
		t.Fatal("a second create must adopt the open draft")
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %v, want exactly one creation", audit.events)
	}
}

// A foreign issued version is absent, so the draft cannot be created against
// it. The response must not distinguish "another tenant's" from "no such".
func TestCreateAwardDraftRefusesAnUnknownIssuedVersion(t *testing.T) {
	service := NewService(
		WithIssuedRFQSource(&fakeIssuedRFQSource{found: false}),
		WithAwardChainRepository(newFakeChainStore()),
		WithAwardDraftRepository(newFakeDraftStore()),
		WithAwardAuditRecorder(&fakeAwardAuditRecorder{}),
	)

	if _, err := service.CreateAwardDraft(
		context.Background(), "company-1", "user-1", "issued-ghost"); !errors.Is(
		err, ErrIssuedRFQNotFound) {
		t.Fatalf("err = %v, want ErrIssuedRFQNotFound", err)
	}
}

// Selecting a line records the lineage from the ISSUED version, not from the
// request: F4's cross-version claim depends on that lineage being authoritative.
func TestSelectLineRecordsTheAuthoritativeLineage(t *testing.T) {
	service, _, _, _ := draftService(t)
	ctx := context.Background()

	draft, err := service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}

	updated, err := service.SelectAwardLine(ctx, "company-1", "user-1",
		SelectAwardLineInput{
			IssuedRFQVersionID: "issued-1",
			IssuedRFQLineID:    "line-1",
			OfferVersionID:     "offer-1",
			OfferLineID:        "ol-1",
			ExpectedRevision:   draft.Revision,
		})
	if err != nil {
		t.Fatalf("SelectAwardLine: %v", err)
	}
	if len(updated.LineDecisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(updated.LineDecisions))
	}
	decision := updated.LineDecisions[0]
	if decision.StableLineageID != "lineage-1" {
		t.Errorf("lineage = %q, want lineage-1 from the issued version",
			decision.StableLineageID)
	}
	if decision.Decision != AwardDecisionSelected {
		t.Errorf("decision = %q, want selected", decision.Decision)
	}
}

// A line the issued version does not have cannot be decided: it would award
// something no Supplier was asked to quote.
func TestSelectLineRefusesAnUnknownIssuedLine(t *testing.T) {
	service, _, _, _ := draftService(t)
	ctx := context.Background()

	draft, err := service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}

	if _, err := service.SelectAwardLine(ctx, "company-1", "user-1",
		SelectAwardLineInput{
			IssuedRFQVersionID: "issued-1",
			IssuedRFQLineID:    "line-ghost",
			OfferVersionID:     "offer-1",
			OfferLineID:        "ol-1",
			ExpectedRevision:   draft.Revision,
		}); err == nil {
		t.Fatal("a decision for an unknown issued line must be refused")
	}
}

// Selecting the same line twice replaces the decision rather than appending:
// one line carries exactly one decision, or a lineage claim cannot resolve it.
func TestSelectLineReplacesAnExistingDecisionForThatLine(t *testing.T) {
	service, _, _, _ := draftService(t)
	ctx := context.Background()

	draft, err := service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}

	updated, err := service.SelectAwardLine(ctx, "company-1", "user-1",
		SelectAwardLineInput{
			IssuedRFQVersionID: "issued-1", IssuedRFQLineID: "line-1",
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
			ExpectedRevision: draft.Revision,
		})
	if err != nil {
		t.Fatalf("SelectAwardLine: %v", err)
	}

	updated, err = service.UnawardLine(ctx, "company-1", "user-1",
		UnawardLineInput{
			IssuedRFQVersionID: "issued-1", IssuedRFQLineID: "line-1",
			Reason: UnawardedScopeCancelled, ExpectedRevision: updated.Revision,
		})
	if err != nil {
		t.Fatalf("UnawardLine: %v", err)
	}
	if len(updated.LineDecisions) != 1 {
		t.Fatalf("decisions = %d, want 1 after replacing", len(updated.LineDecisions))
	}
	if updated.LineDecisions[0].Decision != AwardDecisionUnawarded {
		t.Errorf("decision = %q, want unawarded", updated.LineDecisions[0].Decision)
	}
	if updated.LineDecisions[0].OfferVersionID != "" {
		t.Error("replacing with unawarded must clear the offer references")
	}
}

// Edits are refused while the chain is finalising: the decisions an in-flight
// publication is calculating from must not move underneath it.
func TestDraftEditsAreRefusedWhileTheChainIsFinalising(t *testing.T) {
	service, chains, _, _ := draftService(t)
	ctx := context.Background()

	draft, err := service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}

	chain := chains.chains["chain-issued-1"]
	chain.FinalisationState = FinalisationFinalising
	chain.FinalisingOperationID = "op-1"
	chain.FinalisingRevisionID = "candidate-1"
	chains.chains["chain-issued-1"] = chain

	if _, err := service.SelectAwardLine(ctx, "company-1", "user-1",
		SelectAwardLineInput{
			IssuedRFQVersionID: "issued-1", IssuedRFQLineID: "line-1",
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
			ExpectedRevision: draft.Revision,
		}); !errors.Is(err, ErrAwardDraftConflict) {
		t.Fatalf("err = %v, want ErrAwardDraftConflict while finalising", err)
	}
}

// Discarding archives the draft and audits it. Discard is reversible in effect
// — a new draft may be opened — but the fact that someone discarded a set of
// decisions is worth preserving.
func TestDiscardAwardDraftArchivesAndAudits(t *testing.T) {
	service, _, drafts, audit := draftService(t)
	ctx := context.Background()

	draft, err := service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}
	if err := service.DiscardAwardDraft(ctx, "company-1", "user-1",
		"issued-1", draft.Revision); err != nil {
		t.Fatalf("DiscardAwardDraft: %v", err)
	}

	stored := drafts.drafts[draft.ID]
	if stored.Status != AwardDraftArchived {
		t.Fatalf("status = %q, want archived", stored.Status)
	}
	if len(audit.events) != 2 || audit.events[1] != "award_draft_discarded" {
		t.Fatalf("audit events = %v, want a discard event", audit.events)
	}
}

// A read for a chain with no draft is a bounded not-found, not an empty draft:
// an empty draft would read as "every line deliberately undecided".
func TestGetAwardDraftReturnsNotFoundWhenNoneIsOpen(t *testing.T) {
	service, _, _, _ := draftService(t)

	if _, err := service.GetAwardDraft(
		context.Background(), "company-1", "issued-1"); !errors.Is(
		err, ErrAwardDraftNotFound) {
		t.Fatalf("err = %v, want ErrAwardDraftNotFound", err)
	}
}

// Audit failure must not fail the edit: the decision is already persisted, and
// refusing the response would invite a retry that changes nothing.
func TestDraftAuditFailureDoesNotFailTheWrite(t *testing.T) {
	chains := newFakeChainStore()
	drafts := newFakeDraftStore()
	audit := &fakeAwardAuditRecorder{err: errors.New("audit sink down")}
	service := NewService(
		WithIssuedRFQSource(&fakeIssuedRFQSource{
			snapshot: IssuedRFQSnapshot{
				ID: "issued-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
				Currency: "MYR",
				Lines: []IssuedRFQLineSnapshot{
					{ID: "line-1", LineageID: "lineage-1"}},
			},
			found: true}),
		WithAwardChainRepository(chains),
		WithAwardDraftRepository(drafts),
		WithAwardAuditRecorder(audit),
	)

	if _, err := service.CreateAwardDraft(
		context.Background(), "company-1", "user-1", "issued-1"); err != nil {
		t.Fatalf("a failing audit sink must not fail the write: %v", err)
	}
}

var _ = time.Now

// The fake chain store also plays the publisher role, so finalisation tests
// exercise the same transitions the Mongo repository implements.
func (f *fakeChainStore) ClaimFinalisation(
	_ context.Context, input FinalisationClaimInput,
) (AwardDecisionChain, error) {
	chain, ok := f.chains[input.AwardChainID]
	if !ok || chain.CompanyID != input.CompanyID {
		return AwardDecisionChain{}, ErrAwardChainNotFound
	}
	if chain.FinalisationState == FinalisationFinalising &&
		chain.FinalisingOperationID == input.OperationID {
		return chain, nil
	}
	// A correction legitimately starts from `published`; an in-flight
	// `finalising` chain is excluded so it can never be disturbed (§8G).
	if (chain.FinalisationState != FinalisationDraft &&
		chain.FinalisationState != FinalisationPublished) ||
		chain.Revision != input.ExpectedRevision {
		return AwardDecisionChain{}, ErrAwardRevisionConflict
	}
	chain.FinalisationState = FinalisationFinalising
	chain.FinalisingOperationID = input.OperationID
	chain.FinalisingRevisionID = input.CandidateRevisionID
	chain.Revision++
	f.chains[chain.ID] = chain
	return chain, nil
}

func (f *fakeChainStore) PublishRevision(
	_ context.Context, input PublishRevisionInput,
) (AwardDecisionChain, error) {
	chain, ok := f.chains[input.AwardChainID]
	if !ok || chain.CompanyID != input.CompanyID {
		return AwardDecisionChain{}, ErrAwardChainNotFound
	}
	if chain.FinalisationState == FinalisationPublished &&
		chain.CurrentAwardRevisionID != nil &&
		*chain.CurrentAwardRevisionID == input.AwardRevisionID {
		return chain, nil
	}
	if chain.FinalisationState != FinalisationFinalising ||
		chain.FinalisingOperationID != input.OperationID {
		return AwardDecisionChain{}, ErrAwardRevisionConflict
	}
	revisionID := input.AwardRevisionID
	chain.FinalisationState = FinalisationPublished
	chain.CurrentAwardRevisionID = &revisionID
	chain.LatestRevisionNumber = input.RevisionNumber
	chain.FinalisingOperationID = ""
	chain.FinalisingRevisionID = ""
	chain.Revision++
	f.chains[chain.ID] = chain
	return chain, nil
}

func (f *fakeChainStore) AbandonFinalisation(
	_ context.Context, companyID, chainID, operationID string,
	expectedRevision int64,
) error {
	chain, ok := f.chains[chainID]
	if !ok || chain.CompanyID != companyID ||
		chain.FinalisationState != FinalisationFinalising ||
		chain.FinalisingOperationID != operationID {
		return ErrAwardRevisionConflict
	}
	// Restore what the chain's history implies: a failed CORRECTION must not
	// orphan the published award it was superseding.
	chain.FinalisationState = FinalisationDraft
	if chain.CurrentAwardRevisionID != nil && *chain.CurrentAwardRevisionID != "" {
		chain.FinalisationState = FinalisationPublished
	}
	chain.FinalisingOperationID = ""
	chain.FinalisingRevisionID = ""
	chain.Revision++
	f.chains[chainID] = chain
	return nil
}
