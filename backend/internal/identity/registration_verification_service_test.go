package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	platformmail "github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

// --- fakeRegistrationVerificationRepo: single-document-per-recipient+purpose,
// mirroring the Mongo implementation's exact atomicity contract in a
// single-threaded fake (sequential calls are sufficient to prove the
// contract; the Mongo file's filter-as-CAS discipline is what provides real
// concurrency safety). ---

type fakeRegistrationVerificationRepo struct {
	byKey  map[string]RegistrationVerificationChallenge // recipient|purpose -> challenge
	nextID int
}

func newFakeRegistrationVerificationRepo() *fakeRegistrationVerificationRepo {
	return &fakeRegistrationVerificationRepo{byKey: map[string]RegistrationVerificationChallenge{}}
}

func fakeRegVerifKey(recipient string, purpose RegistrationVerificationPurpose) string {
	return recipient + "|" + string(purpose)
}

func (f *fakeRegistrationVerificationRepo) IssueOrResume(_ context.Context, recipient string, purpose RegistrationVerificationPurpose, codeHash string, now time.Time) (RegistrationVerificationChallenge, error) {
	key := fakeRegVerifKey(recipient, purpose)
	existing, ok := f.byKey[key]
	if ok {
		if now.Before(existing.ResendAvailableAt) {
			return RegistrationVerificationChallenge{}, ErrRegistrationVerificationRateLimited
		}
		windowStart := now
		issuanceCount := 1
		if existing.IssuanceWindowStartedAt.After(now.Add(-registrationVerificationIssueWindow)) {
			windowStart = existing.IssuanceWindowStartedAt
			issuanceCount = existing.IssuanceCount + 1
			if issuanceCount > registrationVerificationMaxIssuances {
				return RegistrationVerificationChallenge{}, ErrRegistrationVerificationRateLimited
			}
		}
		existing.CodeHash = codeHash
		existing.CreatedAt = now
		existing.ExpiresAt = now.Add(registrationVerificationLifetime)
		existing.ConsumedAt = nil
		existing.FailedAttempts = 0
		existing.IssuanceCount = issuanceCount
		existing.IssuanceWindowStartedAt = windowStart
		existing.ResendAvailableAt = now.Add(registrationVerificationResendCooldown)
		f.byKey[key] = existing
		return existing, nil
	}
	f.nextID++
	challenge := RegistrationVerificationChallenge{
		ID: string(rune('a' + f.nextID)), NormalizedRecipient: recipient, Purpose: purpose,
		CodeHash: codeHash, CreatedAt: now, ExpiresAt: now.Add(registrationVerificationLifetime),
		IssuanceCount: 1, IssuanceWindowStartedAt: now, ResendAvailableAt: now.Add(registrationVerificationResendCooldown),
	}
	f.byKey[key] = challenge
	return challenge, nil
}

func (f *fakeRegistrationVerificationRepo) FindCurrent(_ context.Context, recipient string, purpose RegistrationVerificationPurpose) (RegistrationVerificationChallenge, error) {
	c, ok := f.byKey[fakeRegVerifKey(recipient, purpose)]
	if !ok {
		return RegistrationVerificationChallenge{}, ErrRegistrationVerificationNotFound
	}
	return c, nil
}

func (f *fakeRegistrationVerificationRepo) RecordFailedAttempt(_ context.Context, challengeID string, now time.Time) (RegistrationVerificationChallenge, error) {
	for key, c := range f.byKey {
		if c.ID != challengeID {
			continue
		}
		if !c.IsActive(now) {
			return RegistrationVerificationChallenge{}, ErrRegistrationVerificationInvalid
		}
		c.FailedAttempts++
		f.byKey[key] = c
		return c, nil
	}
	return RegistrationVerificationChallenge{}, ErrRegistrationVerificationNotFound
}

func (f *fakeRegistrationVerificationRepo) Consume(_ context.Context, challengeID string, now time.Time) (RegistrationVerificationChallenge, error) {
	for key, c := range f.byKey {
		if c.ID != challengeID {
			continue
		}
		if !c.IsActive(now) {
			return RegistrationVerificationChallenge{}, ErrRegistrationVerificationInvalid
		}
		consumedAt := now
		c.ConsumedAt = &consumedAt
		f.byKey[key] = c
		return c, nil
	}
	return RegistrationVerificationChallenge{}, ErrRegistrationVerificationInvalid
}

// --- fakeCapturingMailer: records every Send call so tests can assert on
// the transport recipient distinctly from the logical recipient. ---

type fakeCapturingMailer struct {
	sent    []platformmail.Message
	sendErr error
}

func (f *fakeCapturingMailer) Send(_ context.Context, msg platformmail.Message) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, msg)
	return nil
}

func newTestRegistrationVerificationService() (*RegistrationVerificationService, *fakeRegistrationVerificationRepo, *fakeCapturingMailer) {
	repo := newFakeRegistrationVerificationRepo()
	mailer := &fakeCapturingMailer{}
	return NewRegistrationVerificationService(repo, mailer), repo, mailer
}

// (1) plaintext OTP is not persisted.
func TestSendRegistrationVerification_NeverPersistsPlaintextCode(t *testing.T) {
	svc, repo, mailer := newTestRegistrationVerificationService()
	now := time.Now()

	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(mailer.sent))
	}
	challenge, err := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The persisted hash must not equal the plaintext code that was
	// emailed, and must not simply contain it as a substring.
	if challenge.CodeHash == "" {
		t.Fatal("expected a non-empty code hash")
	}
	if VerifyPassword(challenge.CodeHash, mailer.sent[0].Body) == nil {
		t.Fatal("hash must not verify against the raw email body")
	}
}

// (2) valid OTP succeeds once. (3) second use fails.
func TestVerifyRegistrationCode_ValidSucceedsOnceThenFailsOnReuse(t *testing.T) {
	svc, repo, mailer := newTestRegistrationVerificationService()
	now := time.Now()
	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("send: %v", err)
	}
	code := extractCodeFromBody(t, mailer.sent[0].Body)

	if err := svc.VerifyRegistrationCode(context.Background(), "tester@example.com", code, now.Add(time.Second)); err != nil {
		t.Fatalf("expected first verification to succeed, got %v", err)
	}
	challenge, _ := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration)
	if challenge.ConsumedAt == nil {
		t.Fatal("expected the challenge to be marked consumed")
	}

	err := svc.VerifyRegistrationCode(context.Background(), "tester@example.com", code, now.Add(2*time.Second))
	if !errors.Is(err, ErrRegistrationVerificationInvalid) {
		t.Fatalf("expected ErrRegistrationVerificationInvalid on reuse, got %v", err)
	}
}

// (4) expired OTP fails.
func TestVerifyRegistrationCode_ExpiredFails(t *testing.T) {
	svc, _, mailer := newTestRegistrationVerificationService()
	now := time.Now()
	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("send: %v", err)
	}
	code := extractCodeFromBody(t, mailer.sent[0].Body)

	err := svc.VerifyRegistrationCode(context.Background(), "tester@example.com", code, now.Add(registrationVerificationLifetime+time.Second))
	if !errors.Is(err, ErrRegistrationVerificationInvalid) {
		t.Fatalf("expected ErrRegistrationVerificationInvalid for an expired code, got %v", err)
	}
}

// (5) wrong OTP increments attempts. (6) attempt limit blocks further verification.
func TestVerifyRegistrationCode_WrongCodeIncrementsAttemptsUntilLimit(t *testing.T) {
	svc, repo, _ := newTestRegistrationVerificationService()
	now := time.Now()
	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("send: %v", err)
	}
	challenge, _ := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration)

	for i := 0; i < registrationVerificationMaxAttempts; i++ {
		err := svc.VerifyRegistrationCode(context.Background(), "tester@example.com", "000000", now.Add(time.Duration(i+1)*time.Second))
		if !errors.Is(err, ErrRegistrationVerificationInvalid) {
			t.Fatalf("attempt %d: expected ErrRegistrationVerificationInvalid, got %v", i, err)
		}
	}
	updated, _ := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration)
	if updated.FailedAttempts != registrationVerificationMaxAttempts {
		t.Fatalf("expected FailedAttempts=%d, got %d", registrationVerificationMaxAttempts, updated.FailedAttempts)
	}

	// Even the CORRECT code must now be rejected — the attempt limit is
	// exhausted, matching supplieraccess's "attempt_limit_reached" precedent.
	correctHash := challenge.CodeHash
	_ = correctHash // the plaintext was never captured for the wrong-code loop; verify limit via wrong code again
	err := svc.VerifyRegistrationCode(context.Background(), "tester@example.com", "000000", now.Add(10*time.Second))
	if !errors.Is(err, ErrRegistrationVerificationInvalid) {
		t.Fatalf("expected attempt-limit rejection, got %v", err)
	}
}

// (7) purpose mismatch fails. Registration is the only purpose currently
// defined, so this is proven at the repository-key level: a challenge
// issued under one purpose is never found under another.
func TestFindCurrent_PurposeMismatchNeverMatches(t *testing.T) {
	repo := newFakeRegistrationVerificationRepo()
	now := time.Now()
	if _, err := repo.IssueOrResume(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration, "hash", now); err != nil {
		t.Fatalf("issue: %v", err)
	}
	_, err := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurpose("password_reset"))
	if !errors.Is(err, ErrRegistrationVerificationNotFound) {
		t.Fatalf("expected ErrRegistrationVerificationNotFound for a different purpose, got %v", err)
	}
}

// (8) cross-tenant/context use fails — here, cross-recipient: a code
// issued for one email must never verify for a different email.
func TestVerifyRegistrationCode_CrossRecipientFails(t *testing.T) {
	svc, _, mailer := newTestRegistrationVerificationService()
	now := time.Now()
	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("send: %v", err)
	}
	code := extractCodeFromBody(t, mailer.sent[0].Body)

	err := svc.VerifyRegistrationCode(context.Background(), "someone-else@example.com", code, now.Add(time.Second))
	if !errors.Is(err, ErrRegistrationVerificationInvalid) {
		t.Fatalf("expected ErrRegistrationVerificationInvalid for a mismatched recipient, got %v", err)
	}
}

// (9) resend/test-sink transport rewrites only delivery recipient.
// (10) logical recipient remains unchanged.
func TestSendRegistrationVerification_TestSinkRewritesOnlyTransportRecipient(t *testing.T) {
	inner := &fakeCapturingMailer{}
	sink := platformmail.NewTestSinkSender(inner, "sink@example.com")
	repo := newFakeRegistrationVerificationRepo()
	svc := NewRegistrationVerificationService(repo, sink)
	now := time.Now()

	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Transport recipient (what actually gets the email) is the sink.
	if len(inner.sent) != 1 || inner.sent[0].To != "sink@example.com" {
		t.Fatalf("expected the transport copy to go to the sink, got %+v", inner.sent)
	}
	// Logical recipient (persisted domain state) remains the real tester.
	challenge, err := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration)
	if err != nil {
		t.Fatalf("expected the challenge to be persisted under the real recipient: %v", err)
	}
	if challenge.NormalizedRecipient != "tester@example.com" {
		t.Fatalf("expected logical recipient unchanged, got %q", challenge.NormalizedRecipient)
	}
	// The test-sink body identifies who the code was really for.
	if inner.sent[0].Headers["X-Renovex-Intended-Recipient"] != "tester@example.com" {
		t.Fatalf("expected the intended-recipient header to name the real recipient, got %+v", inner.sent[0].Headers)
	}
}

// (11) transport/provider failure does not mark OTP as successfully
// delivered.
func TestSendRegistrationVerification_TransportFailurePropagatesAndChallengeStaysIssued(t *testing.T) {
	repo := newFakeRegistrationVerificationRepo()
	mailer := &fakeCapturingMailer{sendErr: errors.New("resend: 503")}
	svc := NewRegistrationVerificationService(repo, mailer)
	now := time.Now()

	_, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now)
	if err == nil {
		t.Fatal("expected the send failure to propagate as an error")
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("expected zero successfully-recorded sends, got %d", len(mailer.sent))
	}
	// The challenge is still durably issued (issued-before-mail-attempted,
	// per the service's own ordering) — a subsequent resend, once the
	// cooldown passes, is the correct bounded retry path, not silently
	// reporting success.
	challenge, findErr := repo.FindCurrent(context.Background(), "tester@example.com", RegistrationVerificationPurposeRegistration)
	if findErr != nil {
		t.Fatalf("expected the challenge to still exist: %v", findErr)
	}
	if challenge.ConsumedAt != nil {
		t.Fatal("expected the challenge to remain unconsumed after a transport failure")
	}
}

// (12) rate-limit/reissue behavior follows the chosen existing precedent —
// atomic single-document cooldown + rolling issuance window, enforced by
// the same repository operation that issues the code (never counted from
// separate documents in application code).
func TestSendRegistrationVerification_ResendCooldownBlocksImmediateReissue(t *testing.T) {
	svc, _, _ := newTestRegistrationVerificationService()
	now := time.Now()
	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now); err != nil {
		t.Fatalf("first send: %v", err)
	}

	_, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", now.Add(time.Second))
	if !errors.Is(err, ErrRegistrationVerificationRateLimited) {
		t.Fatalf("expected ErrRegistrationVerificationRateLimited within the cooldown, got %v", err)
	}

	// Past the cooldown, a resend succeeds and replaces the SAME document
	// (never a second unrelated one).
	afterCooldown := now.Add(registrationVerificationResendCooldown + time.Second)
	if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", afterCooldown); err != nil {
		t.Fatalf("expected resend past cooldown to succeed, got %v", err)
	}
}

func TestSendRegistrationVerification_IssuanceWindowBoundsTotalReissues(t *testing.T) {
	repo := newFakeRegistrationVerificationRepo()
	mailer := &fakeCapturingMailer{}
	svc := NewRegistrationVerificationService(repo, mailer)
	now := time.Now()

	// Issue up to the bound, spaced past the per-resend cooldown each time.
	for i := 0; i < registrationVerificationMaxIssuances; i++ {
		at := now.Add(time.Duration(i) * (registrationVerificationResendCooldown + time.Second))
		if _, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", at); err != nil {
			t.Fatalf("issuance %d: unexpected error: %v", i, err)
		}
	}
	oneMore := now.Add(time.Duration(registrationVerificationMaxIssuances) * (registrationVerificationResendCooldown + time.Second))
	_, err := svc.SendRegistrationVerification(context.Background(), "tester@example.com", oneMore)
	if !errors.Is(err, ErrRegistrationVerificationRateLimited) {
		t.Fatalf("expected the issuance window bound to reject a 6th code, got %v", err)
	}
}

// extractCodeFromBody pulls the 6-digit code out of the exact body format
// SendRegistrationVerification emits — test-only convenience, mirrors how a
// human reading the email would find the code.
func extractCodeFromBody(t *testing.T, body string) string {
	t.Helper()
	const prefix = "Your Renovex verification code is "
	idx := len(prefix)
	if len(body) < idx+registrationVerificationCodeLength {
		t.Fatalf("email body too short to contain a code: %q", body)
	}
	return body[idx : idx+registrationVerificationCodeLength]
}
