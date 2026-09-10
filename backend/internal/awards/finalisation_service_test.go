package awards

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// F5 publication sequence and recovery (§8F).
//
// Step 5 — the immutable revision insert — is authoritative. Steps 6-10 are
// recoverable: a crash after the insert must complete forward, never roll back.

type fakeRevisionStore struct {
	revisions   map[string]AwardRevision
	byOperation map[string]string
	nextID      int
	insertErr   error
	inserts     int
}

func newFakeRevisionStore() *fakeRevisionStore {
	return &fakeRevisionStore{
		revisions:   map[string]AwardRevision{},
		byOperation: map[string]string{},
	}
}

func (f *fakeRevisionStore) InsertRevision(
	_ context.Context, candidate AwardRevision,
) (AwardRevision, error) {
	if err := candidate.Validate(); err != nil {
		return AwardRevision{}, err
	}
	if f.insertErr != nil {
		return AwardRevision{}, f.insertErr
	}
	f.inserts++

	if id, exists := f.byOperation[candidate.CompanyID+"|"+
		candidate.FinalisationOperationID]; exists {
		existing := f.revisions[id]
		if existing.SelectionFingerprint == candidate.SelectionFingerprint &&
			existing.RevisionNumber == candidate.RevisionNumber {
			return existing, nil
		}
		return AwardRevision{}, ErrAwardRevisionConflict
	}
	for _, existing := range f.revisions {
		if existing.CompanyID == candidate.CompanyID &&
			existing.AwardChainID == candidate.AwardChainID &&
			existing.RevisionNumber == candidate.RevisionNumber {
			return AwardRevision{}, ErrAwardRevisionConflict
		}
	}

	f.nextID++
	candidate.ID = "revision-" + string(rune('0'+f.nextID))
	f.revisions[candidate.ID] = candidate
	f.byOperation[candidate.CompanyID+"|"+
		candidate.FinalisationOperationID] = candidate.ID
	return candidate, nil
}

func (f *fakeRevisionStore) FindRevision(
	_ context.Context, companyID, revisionID string,
) (AwardRevision, bool, error) {
	revision, ok := f.revisions[revisionID]
	if !ok || revision.CompanyID != companyID {
		return AwardRevision{}, false, nil
	}
	return revision, true, nil
}

func (f *fakeRevisionStore) FindRevisionByOperation(
	_ context.Context, companyID, operationID string,
) (AwardRevision, bool, error) {
	id, ok := f.byOperation[companyID+"|"+operationID]
	if !ok {
		return AwardRevision{}, false, nil
	}
	return f.revisions[id], true, nil
}

func (f *fakeRevisionStore) FindLatestRevision(
	_ context.Context, companyID, chainID string,
) (AwardRevision, bool, error) {
	var latest AwardRevision
	found := false
	for _, revision := range f.revisions {
		if revision.CompanyID != companyID || revision.AwardChainID != chainID {
			continue
		}
		if !found || revision.RevisionNumber > latest.RevisionNumber {
			latest, found = revision, true
		}
	}
	return latest, found, nil
}

func (f *fakeRevisionStore) ListRevisions(
	_ context.Context, companyID, chainID string,
) ([]AwardRevision, error) {
	var revisions []AwardRevision
	for _, revision := range f.revisions {
		if revision.CompanyID == companyID && revision.AwardChainID == chainID {
			revisions = append(revisions, revision)
		}
	}
	return revisions, nil
}

// finalisationHarness wires a service whose draft is complete and whose offer
// is awardable, so each test can perturb exactly one thing.
type finalisationHarness struct {
	service   *Service
	chains    *fakeChainStore
	drafts    *fakeDraftStore
	revisions *fakeRevisionStore
	claims    *fakeLineClaimStore
	gates     *fakeOfferEligibilityClaimant
	audit     *fakeAwardAuditRecorder
	outcomes  *fakeOutcomeStore
}

func newFinalisationHarness(t *testing.T) *finalisationHarness {
	t.Helper()

	issued := IssuedRFQSnapshot{
		ID: "issued-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
		RFQNumber: "RFQ-0001", VersionNumber: 1, Currency: "MYR",
		Lines: []IssuedRFQLineSnapshot{
			{ID: "line-1", LineageID: "lineage-1", MaterialName: "Tile",
				Quantity: mustQuantity(t, "10", "sqm")},
		},
	}
	version := calcVersion(t, "offer-1", "supplier-a", []OfferLineSnapshot{
		calcLine("ol-1", "line-1", mustQuantity(t, "10", "sqm"), 1000, 10_000)})
	version.EligibilityRevision = 1

	harness := &finalisationHarness{
		chains:    newFakeChainStore(),
		drafts:    newFakeDraftStore(),
		revisions: newFakeRevisionStore(),
		claims:    newFakeLineClaimStore(),
		gates:     &fakeOfferEligibilityClaimant{},
		audit:     &fakeAwardAuditRecorder{},
		outcomes:  &fakeOutcomeStore{outcomes: map[string]AwardOutcome{}},
	}
	harness.service = NewService(
		WithIssuedRFQSource(&fakeIssuedRFQSource{snapshot: issued, found: true}),
		WithOfferVersionSource(&fakeOfferVersionSource{
			versions: map[string]OfferVersionSnapshot{"offer-1": version}}),
		WithAwardChainRepository(harness.chains),
		WithAwardDraftRepository(harness.drafts),
		WithAwardRevisionRepository(harness.revisions),
		WithAwardLineClaimRepository(harness.claims),
		WithOfferEligibilityClaimant(harness.gates),
		WithAwardAuditRecorder(harness.audit),
		WithAwardOutcomeRepository(harness.outcomes),
	)
	return harness
}

// prepareCompleteDraft opens a draft and decides every issued line.
func (h *finalisationHarness) prepareCompleteDraft(t *testing.T) AwardDraft {
	t.Helper()
	ctx := context.Background()

	draft, err := h.service.CreateAwardDraft(ctx, "company-1", "user-1", "issued-1")
	if err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}
	updated, err := h.service.SelectAwardLine(ctx, "company-1", "user-1",
		SelectAwardLineInput{
			IssuedRFQVersionID: "issued-1", IssuedRFQLineID: "line-1",
			OfferVersionID: "offer-1", OfferLineID: "ol-1",
			ExpectedRevision: draft.Revision,
		})
	if err != nil {
		t.Fatalf("SelectAwardLine: %v", err)
	}
	return updated
}

func finaliseRequest(operationID string) FinaliseAwardInput {
	return FinaliseAwardInput{
		IssuedRFQVersionID: "issued-1",
		OperationID:        operationID,
		// Keep the action on the same deterministic clock as calcVersion's
		// validity window. Using wall time made the entire Phase F suite expire
		// permanently after 2 August 2026.
		FinalisedAt: calcAt,
	}
}

// The happy path publishes revision 1, advances the chain and archives the
// draft.
func TestFinaliseAwardPublishesRevisionOne(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)

	revision, err := harness.service.FinaliseAward(context.Background(),
		"company-1", "user-1", finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}
	if revision.RevisionNumber != 1 {
		t.Fatalf("revision number = %d, want 1", revision.RevisionNumber)
	}
	if revision.GrandAwardTotal != money.New(10_000, "MYR") {
		t.Errorf("total = %+v, want 10000 MYR", revision.GrandAwardTotal)
	}

	chain := harness.chains.chains["chain-issued-1"]
	if chain.FinalisationState != FinalisationPublished {
		t.Errorf("chain state = %q, want published", chain.FinalisationState)
	}
	if chain.CurrentAwardRevisionID == nil ||
		*chain.CurrentAwardRevisionID != revision.ID {
		t.Error("the chain must point at the published revision")
	}
}

// An incomplete draft cannot be finalised: completeness is a finalisation-time
// rule, and awarding with lines undecided would leave the RFQ half-answered.
func TestFinaliseAwardRefusesAnIncompleteDraft(t *testing.T) {
	harness := newFinalisationHarness(t)
	ctx := context.Background()

	if _, err := harness.service.CreateAwardDraft(
		ctx, "company-1", "user-1", "issued-1"); err != nil {
		t.Fatalf("CreateAwardDraft: %v", err)
	}

	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); !errors.Is(err, ErrAwardDraftIncomplete) {
		t.Fatalf("err = %v, want ErrAwardDraftIncomplete", err)
	}
	if harness.revisions.inserts != 0 {
		t.Fatal("an incomplete draft must never reach the insert")
	}
}

// Both claim tiers are acquired and completed: the lineage claim and the offer
// eligibility gate (§8A.2).
func TestFinaliseAwardAcquiresAndCompletesBothClaimTiers(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)

	if _, err := harness.service.FinaliseAward(context.Background(),
		"company-1", "user-1", finaliseRequest("op-1")); err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}

	if len(harness.claims.acquired) != 1 ||
		harness.claims.acquired[0] != "lineage-1" {
		t.Fatalf("lineage claims = %v, want [lineage-1]", harness.claims.acquired)
	}
	for _, claim := range harness.claims.claims {
		if claim.State != LineClaimAwarded {
			t.Errorf("lineage claim state = %q, want awarded", claim.State)
		}
	}
	if len(harness.gates.claimed) != 1 || len(harness.gates.completed) != 1 {
		t.Fatalf("gates claimed=%v completed=%v, want one each",
			harness.gates.claimed, harness.gates.completed)
	}
}

// A same-operation retry ADOPTS the existing revision and completes steps 6-10
// again. It never re-numbers and never publishes a second revision (§8F).
func TestFinaliseAwardRetryAdoptsTheExistingRevision(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	first, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}

	second, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("same-operation retry must adopt: %v", err)
	}
	if first.ID != second.ID ||
		first.RevisionNumber != second.RevisionNumber {
		t.Fatalf("retry produced a different revision: %+v then %+v",
			first, second)
	}
	if len(harness.revisions.revisions) != 1 {
		t.Fatalf("stored revisions = %d, want 1", len(harness.revisions.revisions))
	}
}

// Audit is ENSURED, not skipped (§8F, Revision 20). A crash between insert and
// audit would otherwise leave an authoritative award permanently unaudited, so
// a retry that adopts an existing revision RECORDS the missing event.
func TestFinaliseAwardRetryRecordsAMissingAuditEvent(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	// Simulate the crash window: the audit sink is down when the award
	// publishes, so the authoritative revision exists with no audit event.
	harness.audit.err = errors.New("audit sink down")
	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}
	recordedDuringCrash := len(harness.audit.recorded)

	// The sink recovers and the caller retries.
	harness.audit.err = nil
	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if recordedDuringCrash != 0 {
		t.Fatalf("the failing sink recorded %d events", recordedDuringCrash)
	}
	if len(harness.audit.recorded) != 1 {
		t.Fatalf("after recovery the award has %d audit events, want exactly 1",
			len(harness.audit.recorded))
	}
}

// A retry against a revision that ALREADY has its event records nothing more:
// exactly one event per authoritative revision, whichever path completes it.
func TestFinaliseAwardRetryDoesNotDuplicateAnExistingAuditEvent(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	for attempt := 0; attempt < 3; attempt++ {
		if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
			finaliseRequest("op-1")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if len(harness.audit.recorded) != 1 {
		t.Fatalf("audit events = %d after three completions, want exactly 1",
			len(harness.audit.recorded))
	}
}

// A DIFFERENT operation cannot finalise a chain that is already publishing:
// the chain CAS rejects it before it touches any shared claim (§8E step 1).
func TestFinaliseAwardRejectsASecondOperationOnAPublishedChain(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1")); err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}

	if _, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-2")); err == nil {
		t.Fatal("a second operation must not finalise an already-published chain")
	}
	if len(harness.revisions.revisions) != 1 {
		t.Fatalf("revisions = %d, want 1", len(harness.revisions.revisions))
	}
}

// A withdrawal landing between calculation and insert is caught at the
// eligibility CAS, and the failed finalisation releases only its own claims.
func TestFinaliseAwardReleasesItsOwnClaimsWhenTheGateIsLost(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)

	// The Supplier withdrew: the gate claim now fails.
	harness.gates.err = ErrOfferVersionNotEligible

	if _, err := harness.service.FinaliseAward(context.Background(),
		"company-1", "user-1", finaliseRequest("op-1")); !errors.Is(
		err, ErrOfferVersionNotEligible) {
		t.Fatalf("err = %v, want ErrOfferVersionNotEligible", err)
	}
	if harness.revisions.inserts != 0 {
		t.Fatal("no revision may be inserted when a gate is lost")
	}
	// The lineage claim it took is released, so the contractor can re-decide.
	if len(harness.claims.released) != 1 {
		t.Fatalf("released %v, want the one lineage it claimed",
			harness.claims.released)
	}
	// The chain returns to draft so a new decision can be finalised.
	chain := harness.chains.chains["chain-issued-1"]
	if chain.FinalisationState != FinalisationDraft {
		t.Errorf("chain state = %q, want draft after a failed finalisation",
			chain.FinalisationState)
	}
}

// The draft is archived only after publication, so a failed finalisation
// leaves the contractor's decisions intact to re-try.
func TestFinaliseAwardKeepsTheDraftWhenPublicationFails(t *testing.T) {
	harness := newFinalisationHarness(t)
	draft := harness.prepareCompleteDraft(t)
	harness.gates.err = ErrOfferVersionNotEligible

	if _, err := harness.service.FinaliseAward(context.Background(),
		"company-1", "user-1", finaliseRequest("op-1")); err == nil {
		t.Fatal("expected the finalisation to fail")
	}

	stored := harness.drafts.drafts[draft.ID]
	if stored.Status != AwardDraftOpen {
		t.Fatalf("draft status = %q, want open after a failed finalisation",
			stored.Status)
	}
	if len(stored.LineDecisions) != 1 {
		t.Error("the contractor's decisions must survive a failed finalisation")
	}
}

// A read during the crash window must never report "no award exists" while an
// authoritative revision is present (§8F).
func TestGetCurrentAwardResolvesARevisionDespiteAStalePointer(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	revision, err := harness.service.FinaliseAward(ctx, "company-1", "user-1",
		finaliseRequest("op-1"))
	if err != nil {
		t.Fatalf("FinaliseAward: %v", err)
	}

	// Simulate the crash window: the revision exists but the chain pointer
	// never advanced.
	chain := harness.chains.chains["chain-issued-1"]
	chain.FinalisationState = FinalisationFinalising
	chain.CurrentAwardRevisionID = nil
	chain.FinalisingOperationID = "op-1"
	chain.FinalisingRevisionID = revision.ID
	harness.chains.chains["chain-issued-1"] = chain

	current, err := harness.service.GetCurrentAward(ctx, "company-1", "issued-1")
	if err != nil {
		t.Fatalf("a stale pointer must not hide an authoritative award: %v", err)
	}
	if current.ID != revision.ID {
		t.Fatalf("resolved %q, want the authoritative %q", current.ID, revision.ID)
	}
}

// While finalising with NO revision yet, a read is a bounded retryable 503 —
// never "no award exists", which a caller could act on wrongly.
func TestGetCurrentAwardReportsPendingDuringTheCrashWindow(t *testing.T) {
	harness := newFinalisationHarness(t)
	harness.prepareCompleteDraft(t)
	ctx := context.Background()

	chain := harness.chains.chains["chain-issued-1"]
	chain.FinalisationState = FinalisationFinalising
	chain.FinalisingOperationID = "op-1"
	chain.FinalisingRevisionID = "candidate-1"
	harness.chains.chains["chain-issued-1"] = chain

	if _, err := harness.service.GetCurrentAward(
		ctx, "company-1", "issued-1"); !errors.Is(
		err, ErrAwardFinalisationPending) {
		t.Fatalf("err = %v, want ErrAwardFinalisationPending", err)
	}
}

// A chain with no award at all is an honest not-found.
func TestGetCurrentAwardReturnsNotFoundWhenNoAwardExists(t *testing.T) {
	harness := newFinalisationHarness(t)

	if _, err := harness.service.GetCurrentAward(
		context.Background(), "company-1", "issued-1"); !errors.Is(
		err, ErrAwardRevisionNotFound) {
		t.Fatalf("err = %v, want ErrAwardRevisionNotFound", err)
	}
}
