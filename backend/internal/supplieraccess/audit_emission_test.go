package supplieraccess_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

type supplierAccessAuditCall struct {
	name               string
	companyID          string
	supplierID         string
	invitationID       string
	challengeID        string
	sessionID          string
	accessGeneration   int64
	tokenGeneration    int64
	verificationReason string
}

type recordingSupplierAccessAudit struct {
	mu    sync.Mutex
	calls []supplierAccessAuditCall
}

func (r *recordingSupplierAccessAudit) append(call supplierAccessAuditCall) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
	return nil
}

func (r *recordingSupplierAccessAudit) RecordChallengeRequested(
	_ context.Context, companyID, supplierID, invitationID, challengeID string,
	accessGeneration int64, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "challenge_requested", companyID: companyID,
		supplierID: supplierID, invitationID: invitationID,
		challengeID: challengeID, accessGeneration: accessGeneration,
	})
}

func (r *recordingSupplierAccessAudit) RecordChallengeDeliveryRetried(
	_ context.Context, companyID, supplierID, invitationID, challengeID,
	_ string, accessGeneration int64, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "challenge_delivery_retried", companyID: companyID,
		supplierID: supplierID, invitationID: invitationID,
		challengeID: challengeID, accessGeneration: accessGeneration,
	})
}

func (r *recordingSupplierAccessAudit) RecordVerificationFailed(
	_ context.Context, companyID, supplierID, invitationID, challengeID string,
	accessGeneration int64, reason string, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "verification_failed", companyID: companyID,
		supplierID: supplierID, invitationID: invitationID,
		challengeID: challengeID, accessGeneration: accessGeneration,
		verificationReason: reason,
	})
}

func (r *recordingSupplierAccessAudit) RecordVerificationSucceeded(
	_ context.Context, companyID, supplierID, invitationID, challengeID,
	sessionID string, accessGeneration int64, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "verification_succeeded", companyID: companyID,
		supplierID: supplierID, invitationID: invitationID,
		challengeID: challengeID, sessionID: sessionID,
		accessGeneration: accessGeneration,
	})
}

func (r *recordingSupplierAccessAudit) RecordSessionCreated(
	_ context.Context, companyID, supplierID, invitationID, challengeID,
	sessionID string, accessGeneration, tokenGeneration int64, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "session_created", companyID: companyID,
		supplierID: supplierID, invitationID: invitationID,
		challengeID: challengeID, sessionID: sessionID,
		accessGeneration: accessGeneration, tokenGeneration: tokenGeneration,
	})
}

func (r *recordingSupplierAccessAudit) RecordSessionReverified(
	_ context.Context, companyID, supplierID, invitationID, challengeID,
	sessionID string, accessGeneration, tokenGeneration int64, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "session_reverified", companyID: companyID,
		supplierID: supplierID, invitationID: invitationID,
		challengeID: challengeID, sessionID: sessionID,
		accessGeneration: accessGeneration, tokenGeneration: tokenGeneration,
	})
}

func (r *recordingSupplierAccessAudit) RecordSessionRenewed(
	_ context.Context, companyID, supplierID, sessionID string,
	tokenGeneration int64, _, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "session_renewed", companyID: companyID,
		supplierID: supplierID, sessionID: sessionID,
		tokenGeneration: tokenGeneration,
	})
}

func (r *recordingSupplierAccessAudit) RecordSessionRevoked(
	_ context.Context, companyID, supplierID, sessionID string,
	tokenGeneration int64, _ time.Time) error {
	return r.append(supplierAccessAuditCall{
		name: "session_revoked", companyID: companyID,
		supplierID: supplierID, sessionID: sessionID,
		tokenGeneration: tokenGeneration,
	})
}

func (r *recordingSupplierAccessAudit) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, call := range r.calls {
		if call.name == name {
			count++
		}
	}
	return count
}

func TestChallengeAuditEventsRequireAuthoritativeInserts(t *testing.T) {
	now := time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	recorder := &recordingSupplierAccessAudit{}
	rig.service = challengeServiceWithAudit(rig, recorder)

	input := supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "audit-challenge-operation",
		ClientAddress: mustAddress("198.51.100.51"),
		RequestedAt:   now,
	}
	result, err := rig.service.CreateChallenge(context.Background(), input)
	if err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	if _, err := rig.service.CreateChallenge(context.Background(), input); err != nil {
		t.Fatalf("recovering challenge: %v", err)
	}
	resend := supplieraccess.ResendChallengeInput{
		ChallengeID: result.ChallengeID, OperationID: "audit-resend-operation",
		RequestedAt: now.Add(time.Minute),
	}
	if _, err := rig.service.ResendChallenge(context.Background(), resend); err != nil {
		t.Fatalf("resending challenge: %v", err)
	}
	if _, err := rig.service.ResendChallenge(context.Background(), resend); err != nil {
		t.Fatalf("recovering resend: %v", err)
	}

	if got := recorder.count("challenge_requested"); got != 1 {
		t.Fatalf("challenge-requested events = %d, want 1", got)
	}
	if got := recorder.count("challenge_delivery_retried"); got != 1 {
		t.Fatalf("challenge-retried events = %d, want 1", got)
	}
}

func TestVerificationFailureAuditRequiresAnAuthoritativeAttemptTransition(
	t *testing.T) {

	now := time.Date(2026, 7, 30, 15, 30, 0, 0, time.UTC)
	rig := newVerificationServiceRig(t, now)
	recorder := &recordingSupplierAccessAudit{}
	rig.service = verificationServiceWithAudit(rig, recorder)
	challengeID, _ := rig.createChallenge(t, now)

	for attempt := 1; attempt <= 5; attempt++ {
		_, err := rig.service.VerifyChallenge(context.Background(),
			supplieraccess.VerifyChallengeInput{
				ChallengeID: challengeID, Code: "not-six-ascii-digits",
				OperationID: "failed-verification-operation",
				VerifiedAt:  now.Add(time.Duration(attempt) * time.Second),
			})
		if err == nil {
			t.Fatalf("failed attempt %d unexpectedly succeeded", attempt)
		}
	}
	if got := recorder.count("verification_failed"); got != 5 {
		t.Fatalf("verification-failed events = %d, want 5", got)
	}
	reasons := map[string]int{}
	for _, call := range recorder.calls {
		if call.name == "verification_failed" {
			reasons[call.verificationReason]++
		}
	}
	if reasons[supplieraccess.VerificationFailureInvalidCode] != 4 ||
		reasons[supplieraccess.VerificationFailureAttemptLimitReached] != 1 {
		t.Fatalf("verification failure reasons = %v", reasons)
	}

	// Neither malformed nor unknown handles resolve an authoritative tenant,
	// so they cannot create tenant audit events.
	for _, unresolved := range []string{"malformed", opaqueToken(240)} {
		_, _ = rig.service.VerifyChallenge(context.Background(),
			supplieraccess.VerifyChallengeInput{
				ChallengeID: unresolved, Code: "123456",
				OperationID: "unresolved-operation", VerifiedAt: now,
			})
	}
	if got := recorder.count("verification_failed"); got != 5 {
		t.Fatalf("unresolved requests changed failed audit count to %d", got)
	}
}

func TestSessionAuditEventsAreEmittedOnlyByAuthoritativeWinners(t *testing.T) {
	now := time.Date(2026, 7, 30, 16, 0, 0, 0, time.UTC)
	rig := newVerificationServiceRig(t, now)
	recorder := &recordingSupplierAccessAudit{}
	rig.service = verificationServiceWithAudit(rig, recorder)
	challengeID, code := rig.createChallenge(t, now)

	firstInput := supplieraccess.VerifyChallengeInput{
		ChallengeID: challengeID, Code: code,
		OperationID: "first-verification-operation", VerifiedAt: now,
	}
	first, err := rig.service.VerifyChallenge(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("verifying first challenge: %v", err)
	}
	if _, err := rig.service.VerifyChallenge(
		context.Background(), firstInput); err != nil {
		t.Fatalf("recovering first verification: %v", err)
	}
	if recorder.count("session_created") != 1 ||
		recorder.count("verification_succeeded") != 1 {
		t.Fatalf("first verification audit calls = %+v", recorder.calls)
	}

	secondAt := now.Add(2 * time.Minute)
	secondChallengeID, secondCode := rig.createAdditionalChallenge(
		t, secondAt, 201, "second-challenge-operation")
	secondInput := supplieraccess.VerifyChallengeInput{
		ChallengeID: secondChallengeID, Code: secondCode,
		OperationID:           "second-verification-operation",
		PresentedSessionToken: first.SessionToken, VerifiedAt: secondAt,
	}
	second, err := rig.service.VerifyChallenge(
		context.Background(), secondInput)
	if err != nil {
		t.Fatalf("re-verifying session: %v", err)
	}
	if _, err := rig.service.VerifyChallenge(
		context.Background(), secondInput); err != nil {
		t.Fatalf("recovering re-verification: %v", err)
	}
	if recorder.count("session_reverified") != 1 ||
		recorder.count("verification_succeeded") != 2 {
		t.Fatalf("re-verification audit calls = %+v", recorder.calls)
	}

	accessedAt := secondAt.Add(24 * time.Hour)
	authorization, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: second.SessionToken,
			InvitationID: second.InvitationID, AccessedAt: accessedAt,
		})
	if err != nil {
		t.Fatalf("authorizing renewed session: %v", err)
	}
	if _, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: second.SessionToken,
			InvitationID: second.InvitationID, AccessedAt: accessedAt,
		}); err != nil {
		t.Fatalf("adopting renewal: %v", err)
	}
	if recorder.count("session_renewed") != 1 {
		t.Fatalf("session-renewed audit calls = %+v", recorder.calls)
	}

	logout := supplieraccess.LogoutSupplierSessionInput{
		SessionToken: second.SessionToken, CSRFCookie: second.CSRFToken,
		CSRFHeader:  second.CSRFToken,
		LoggedOutAt: authorization.SessionCookieRenewal.ExpiresAt.Add(-time.Hour),
	}
	if err := rig.service.LogoutSupplierSession(
		context.Background(), logout); err != nil {
		t.Fatalf("logging out: %v", err)
	}
	if err := rig.service.LogoutSupplierSession(
		context.Background(), logout); err != nil {
		t.Fatalf("repeating logout: %v", err)
	}
	if recorder.count("session_revoked") != 1 {
		t.Fatalf("session-revoked audit calls = %+v", recorder.calls)
	}
}

func challengeServiceWithAudit(rig *challengeServiceRig,
	recorder supplieraccess.SupplierAccessAuditRecorder) *supplieraccess.Service {
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	return supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
		supplieraccess.WithAuditRecorder(recorder),
	)
}

func verificationServiceWithAudit(rig *verificationServiceRig,
	recorder supplieraccess.SupplierAccessAuditRecorder) *supplieraccess.Service {
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	return supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
		supplieraccess.WithSessionStores(rig.sessions, rig.bindings),
		supplieraccess.WithSessionSecurity(rig.sessionKeys),
		supplieraccess.WithAuditRecorder(recorder),
	)
}
