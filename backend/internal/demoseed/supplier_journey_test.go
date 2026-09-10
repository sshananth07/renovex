package demoseed_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// fakeSupplierAccessDriver models the REAL supplieraccess.Service behavior
// that matters for demoseed's idempotency: OpenInvitation always mints a
// fresh exchange (recorded per call, never reused), and CreateChallenge
// tracks which exchange first claimed each OperationID — a second
// CreateChallenge call for an OperationID already claimed by a DIFFERENT
// exchange returns ChallengeOperationConflictError, exactly like the real
// service. Earlier versions of this fake always succeeded unconditionally,
// which is why the demoseed-level unit tests never caught the real bug (the
// conflict only ever showed up against the actual supplieraccess.Service in
// internal/tenanttest's acceptance tests).
type fakeSupplierAccessDriver struct {
	openCalls, challengeCalls, verifyCalls int
	lastCode                               string

	nextExchangeToken int
	challengesByOpID  map[string]struct{ exchangeToken, challengeID string }
}

func (f *fakeSupplierAccessDriver) OpenInvitation(ctx context.Context, rawInvitationToken string, openedAt time.Time) (supplieraccess.OpenInvitationResult, error) {
	f.openCalls++
	f.nextExchangeToken++
	return supplieraccess.OpenInvitationResult{
		ExchangeToken: fmt.Sprintf("exchange-token-%d", f.nextExchangeToken),
		ExpiresAt:     openedAt.Add(10 * time.Minute),
	}, nil
}
func (f *fakeSupplierAccessDriver) CreateChallenge(ctx context.Context, input supplieraccess.CreateChallengeInput) (supplieraccess.VerificationChallengeResult, error) {
	f.challengeCalls++
	if f.challengesByOpID == nil {
		f.challengesByOpID = map[string]struct{ exchangeToken, challengeID string }{}
	}
	if existing, ok := f.challengesByOpID[input.OperationID]; ok && existing.exchangeToken != input.ExchangeToken {
		return supplieraccess.VerificationChallengeResult{}, &supplieraccess.ChallengeOperationConflictError{
			ExistingChallengeID: existing.challengeID,
		}
	}
	challengeID := "challenge-for-" + input.ExchangeToken
	f.challengesByOpID[input.OperationID] = struct{ exchangeToken, challengeID string }{input.ExchangeToken, challengeID}
	return supplieraccess.VerificationChallengeResult{ChallengeID: challengeID, ExpiresAt: input.RequestedAt.Add(10 * time.Minute)}, nil
}
func (f *fakeSupplierAccessDriver) VerifyChallenge(ctx context.Context, input supplieraccess.VerifyChallengeInput) (supplieraccess.VerifyChallengeResult, error) {
	f.verifyCalls++
	f.lastCode = input.Code
	return supplieraccess.VerifyChallengeResult{SessionToken: "session-token", CSRFToken: "csrf-token", SlidingExpiresAt: input.VerifiedAt.Add(time.Hour)}, nil
}

// fakeSupplierOfferDriver models the REAL lifecycle: once a version is
// submitted, it appears in ListSupplierOfferVersions, and the fake fails
// the test outright if SubmitOffer is called a second time for the same
// chain — proving the driver under test actually checks for an existing
// version before attempting to submit again, not merely that it happens
// not to double-submit by chance.
type fakeSupplierOfferDriver struct {
	draftCalls, quoteCalls, declineCalls, validityCalls, submitCalls, listCalls int
	currentRevision                                                             int64
	submittedVersions                                                           []supplieroffers.SupplierOfferVersionSummary
	t                                                                           *testing.T
}

func (f *fakeSupplierOfferDriver) ListSupplierOfferVersions(ctx context.Context, input supplieroffers.SupplierOfferHistoryInput) (supplieroffers.SupplierOfferHistoryPage, error) {
	f.listCalls++
	return supplieroffers.SupplierOfferHistoryPage{Versions: f.submittedVersions}, nil
}
func (f *fakeSupplierOfferDriver) GetSupplierOfferVersion(ctx context.Context, input supplieroffers.SupplierOfferVersionDetailInput) (supplieroffers.SupplierOfferVersionProjection, error) {
	return supplieroffers.SupplierOfferVersionProjection{}, nil
}
func (f *fakeSupplierOfferDriver) CreateOrGetActiveDraft(ctx context.Context, input supplieroffers.SupplierOfferMutationContextInput) (supplieroffers.SupplierOfferDraft, error) {
	f.draftCalls++
	f.currentRevision = 0
	return supplieroffers.SupplierOfferDraft{
		ID: "draft-1", Currency: "MYR", Revision: f.currentRevision,
		Lines: []supplieroffers.SupplierOfferDraftLine{
			{ID: "line-1", RFQLineID: "rfqline-1"},
			{ID: "line-2", RFQLineID: "rfqline-2"},
		},
	}, nil
}
func (f *fakeSupplierOfferDriver) QuoteDraftLine(ctx context.Context, command supplieroffers.QuoteDraftLineCommand) (supplieroffers.SupplierOfferDraft, error) {
	f.quoteCalls++
	f.currentRevision++
	return supplieroffers.SupplierOfferDraft{ID: command.DraftID, Revision: f.currentRevision}, nil
}
func (f *fakeSupplierOfferDriver) DeclineDraftLine(ctx context.Context, command supplieroffers.DeclineDraftLineCommand) (supplieroffers.SupplierOfferDraft, error) {
	f.declineCalls++
	f.currentRevision++
	return supplieroffers.SupplierOfferDraft{ID: command.DraftID, Revision: f.currentRevision}, nil
}
func (f *fakeSupplierOfferDriver) SetOfferValidity(ctx context.Context, command supplieroffers.SetOfferValidityCommand) (supplieroffers.SupplierOfferDraft, error) {
	f.validityCalls++
	f.currentRevision++
	return supplieroffers.SupplierOfferDraft{ID: command.DraftID, Revision: f.currentRevision}, nil
}
func (f *fakeSupplierOfferDriver) SubmitOffer(ctx context.Context, command supplieroffers.SubmitOfferCommand) (supplieroffers.SupplierOfferVersion, error) {
	f.submitCalls++
	if len(f.submittedVersions) > 0 {
		if f.t != nil {
			f.t.Fatalf("SubmitOffer called again after a version was already submitted — the resume check did not prevent a double submission")
		}
	}
	version := supplieroffers.SupplierOfferVersion{ID: "version-1"}
	f.submittedVersions = append(f.submittedVersions, supplieroffers.SupplierOfferVersionSummary{ID: version.ID, VersionNumber: 1})
	return version, nil
}

func testKeyrings(t *testing.T) (*secrets.InvitationKeyring, *secrets.SupplierVerificationCodeKeyring) {
	t.Helper()
	testKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	invitationKeyring, err := secrets.NewInvitationKeyring(1, map[int]string{1: testKey})
	if err != nil {
		t.Fatalf("NewInvitationKeyring: %v", err)
	}
	codeKeyring, err := secrets.NewSupplierVerificationCodeKeyring(1, map[int]string{1: testKey})
	if err != nil {
		t.Fatalf("NewSupplierVerificationCodeKeyring: %v", err)
	}
	return invitationKeyring, codeKeyring
}

func testInvitation() rfqissuance.SupplierInvitation {
	return rfqissuance.SupplierInvitation{
		ID: "invitation-1", CompanyID: "company-1", SupplierID: "supplier-1",
		AccessGeneration: 1, SecretKeyVersion: 1,
		RecipientEmailNormalized: "supplier@example.com",
	}
}

func TestSubmitSupplierOffer_DrivesFullJourney(t *testing.T) {
	ctx := context.Background()
	invitationKeyring, codeKeyring := testKeyrings(t)
	access := &fakeSupplierAccessDriver{}
	offers := &fakeSupplierOfferDriver{t: t}
	prices := map[string]int64{"rfqline-1": 45000, "rfqline-2": 22000}

	_, err := demoseed.SubmitSupplierOffer(ctx, access, offers, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("SubmitSupplierOffer: %v", err)
	}
	if access.openCalls != 1 || access.challengeCalls != 1 || access.verifyCalls != 1 {
		t.Fatalf("expected exactly 1 call each to Open/Challenge/Verify, got %d/%d/%d", access.openCalls, access.challengeCalls, access.verifyCalls)
	}
	if offers.listCalls == 0 {
		t.Fatalf("expected ListSupplierOfferVersions to be checked before attempting a draft")
	}
	if offers.draftCalls != 1 || offers.submitCalls != 1 {
		t.Fatalf("expected exactly 1 draft creation and 1 submit on a fresh invitation, got %d/%d", offers.draftCalls, offers.submitCalls)
	}
}

func TestSubmitSupplierOffer_AlreadySubmitted_ResumesWithoutDoubleSubmitting(t *testing.T) {
	// This is the EXACT gap the review identified: rerun SubmitSupplierOffer
	// against an invitation that ALREADY has a submitted Offer Version.
	ctx := context.Background()
	invitationKeyring, codeKeyring := testKeyrings(t)
	access := &fakeSupplierAccessDriver{}
	offers := &fakeSupplierOfferDriver{t: t}
	prices := map[string]int64{"rfqline-1": 45000, "rfqline-2": 22000}

	// First call succeeds normally.
	first, err := demoseed.SubmitSupplierOffer(ctx, access, offers, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("first SubmitSupplierOffer: %v", err)
	}
	firstDraftCalls, firstSubmitCalls := offers.draftCalls, offers.submitCalls

	// Second call — simulating a rerun of the whole scenario after this
	// Supplier already submitted — must NOT touch the draft/quote/submit
	// path at all (the fake's SubmitOffer would t.Fatalf if called twice).
	second, err := demoseed.SubmitSupplierOffer(ctx, access, offers, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("second (resumed) SubmitSupplierOffer: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the resumed call to return the SAME already-submitted version, got %q then %q", first.ID, second.ID)
	}
	if offers.draftCalls != firstDraftCalls {
		t.Fatalf("expected NO new CreateOrGetActiveDraft call on resume, went from %d to %d", firstDraftCalls, offers.draftCalls)
	}
	if offers.submitCalls != firstSubmitCalls {
		t.Fatalf("expected NO new SubmitOffer call on resume, went from %d to %d", firstSubmitCalls, offers.submitCalls)
	}
	// The access/OTP steps ARE still safe to repeat (idempotent by
	// derivation), so they may run again — only the offer-mutation path
	// must be skipped.
}

func TestSubmitSupplierOffer_DeterministicCodeAcrossCalls(t *testing.T) {
	ctx := context.Background()
	invitationKeyring, codeKeyring := testKeyrings(t)
	prices := map[string]int64{"rfqline-1": 45000}

	access1 := &fakeSupplierAccessDriver{}
	_, err := demoseed.SubmitSupplierOffer(ctx, access1, &fakeSupplierOfferDriver{t: t}, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("first SubmitSupplierOffer: %v", err)
	}

	access2 := &fakeSupplierAccessDriver{}
	_, err = demoseed.SubmitSupplierOffer(ctx, access2, &fakeSupplierOfferDriver{t: t}, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("second SubmitSupplierOffer: %v", err)
	}

	if access1.lastCode != access2.lastCode {
		t.Fatalf("expected the same deterministic OTP code across runs, got %q and %q", access1.lastCode, access2.lastCode)
	}
}

// TestSubmitSupplierOffer_ChallengeOperationConflict_RecoversSessionWithoutMintingASecondChallenge
// is the regression test for the bug found in internal/tenanttest: OpenInvitation
// always mints a fresh exchange, so a second SubmitSupplierOffer call using
// the SAME operationSlug (as every real Seed rerun does) produces a
// CreateChallenge request whose OperationID legitimately collides with the
// challenge already recorded against the FIRST exchange. The real
// supplieraccess.Service correctly rejects that as
// ChallengeOperationConflictError — this fake now reproduces that behavior
// (see its doc comment above) specifically so this path is exercised without
// needing the full internal/tenanttest Docker/Mongo stack. The fix must
// recover a session for the EXISTING challenge via VerifyChallenge rather
// than treating the conflict as fatal, and must never create a second
// challenge for an operation that already has one.
func TestSubmitSupplierOffer_ChallengeOperationConflict_RecoversSessionWithoutMintingASecondChallenge(t *testing.T) {
	ctx := context.Background()
	invitationKeyring, codeKeyring := testKeyrings(t)
	access := &fakeSupplierAccessDriver{}
	offers := &fakeSupplierOfferDriver{t: t}
	prices := map[string]int64{"rfqline-1": 45000, "rfqline-2": 22000}

	first, err := demoseed.SubmitSupplierOffer(ctx, access, offers, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("first SubmitSupplierOffer: %v", err)
	}
	if access.openCalls != 1 || access.challengeCalls != 1 || access.verifyCalls != 1 {
		t.Fatalf("after the first call, expected Open/Challenge/Verify = 1/1/1, got %d/%d/%d", access.openCalls, access.challengeCalls, access.verifyCalls)
	}

	// Second call reuses the SAME access driver (a fresh OpenInvitation call
	// mints a new exchange token, but CreateChallenge's OperationID for this
	// fixed operationSlug is unchanged) — this is exactly what a second real
	// Seed invocation does.
	second, err := demoseed.SubmitSupplierOffer(ctx, access, offers, invitationKeyring, codeKeyring, testInvitation(), prices, "project4:supplier1")
	if err != nil {
		t.Fatalf("second SubmitSupplierOffer (must recover through the conflict, not fail): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the SAME already-submitted version on recovery, got %q then %q", first.ID, second.ID)
	}
	if access.openCalls != 2 {
		t.Fatalf("expected a second OpenInvitation call (each call opens its own exchange), got %d", access.openCalls)
	}
	if access.challengeCalls != 2 {
		t.Fatalf("expected CreateChallenge to be attempted again (and hit the conflict) on the second call, got %d calls", access.challengeCalls)
	}
	if access.verifyCalls != 2 {
		t.Fatalf("expected VerifyChallenge to run again — against the EXISTING challenge — to recover a session, got %d calls", access.verifyCalls)
	}
	if len(access.challengesByOpID) != 1 {
		t.Fatalf("expected exactly ONE challenge ever recorded for this operation, got %d — a second challenge was minted instead of reusing the existing one", len(access.challengesByOpID))
	}
}
