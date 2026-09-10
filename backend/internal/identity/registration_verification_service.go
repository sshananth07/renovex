package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformmail "github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

// RegistrationVerificationService owns the OTP lifecycle for registration
// email verification (T2B). Resend is transport only — this service (and
// its RegistrationVerificationRepository) is the sole owner of the OTP's
// authoritative state; platformmail.EmailSender never sees the code's hash
// or any challenge state, only a rendered message to deliver.
type RegistrationVerificationService struct {
	repo   RegistrationVerificationRepository
	mailer platformmail.EmailSender
}

func NewRegistrationVerificationService(repo RegistrationVerificationRepository, mailer platformmail.EmailSender) *RegistrationVerificationService {
	return &RegistrationVerificationService{repo: repo, mailer: mailer}
}

// SendRegistrationVerificationResult is returned to the caller — never the
// plaintext code, which exists only long enough to be emailed.
type SendRegistrationVerificationResult struct {
	ExpiresAt time.Time
}

// SendRegistrationVerification issues (or, within the resend cooldown,
// rate-limits) a new verification code for email, then emails it. Codes
// are purpose-bound (RegistrationVerificationPurposeRegistration) and
// recipient-normalized exactly like User.Email (NormalizeEmail) — so an
// already-registered account's email and a mid-registration email share
// one normalization rule.
func (s *RegistrationVerificationService) SendRegistrationVerification(ctx context.Context, email string, now time.Time) (SendRegistrationVerificationResult, error) {
	recipient := NormalizeEmail(email)
	if recipient == "" {
		return SendRegistrationVerificationResult{}, ErrRegistrationVerificationInvalid
	}

	plaintext, hash, err := generateRegistrationVerificationCode()
	if err != nil {
		return SendRegistrationVerificationResult{}, err
	}

	challenge, err := s.repo.IssueOrResume(ctx, recipient, RegistrationVerificationPurposeRegistration, hash, now)
	if err != nil {
		return SendRegistrationVerificationResult{}, err
	}

	// Mail delivery happens AFTER the authoritative record is durably
	// issued — a transport failure must not be reported as a successfully
	// delivered code (T2B: "transport/provider failure does not mark OTP
	// as successfully delivered"). This service has no separate delivery-
	// status field to flip (unlike supplieraccess's own heavier delivery-
	// attempt tracking) — a Send error simply propagates to the caller,
	// who is expected to surface a generic "could not send" failure; the
	// challenge itself remains issued and CAN be resent once the resend
	// cooldown passes, which is the correct bounded retry path.
	if err := s.mailer.Send(ctx, platformmail.Message{
		To:      recipient,
		Subject: "Your Renovex verification code",
		Body: fmt.Sprintf(
			"Your Renovex verification code is %s.\n\nThis code expires in %d minutes.",
			plaintext, int(registrationVerificationLifetime.Minutes()),
		),
	}); err != nil {
		return SendRegistrationVerificationResult{}, fmt.Errorf("identity: sending registration verification email: %w", err)
	}

	return SendRegistrationVerificationResult{ExpiresAt: challenge.ExpiresAt}, nil
}

// VerifyRegistrationCode checks code against the current challenge for
// email+purpose, atomically consuming it exactly once on success. Every
// rejection reason (wrong code, expired, already consumed, attempt limit,
// no such challenge at all) collapses to the same
// ErrRegistrationVerificationInvalid — the caller must never be able to
// distinguish "no such recipient" from "wrong code" (T2B: "do not leak
// whether unrelated accounts/users exist").
func (s *RegistrationVerificationService) VerifyRegistrationCode(ctx context.Context, email, code string, now time.Time) error {
	recipient := NormalizeEmail(email)
	if recipient == "" || code == "" {
		return ErrRegistrationVerificationInvalid
	}

	challenge, err := s.repo.FindCurrent(ctx, recipient, RegistrationVerificationPurposeRegistration)
	if errors.Is(err, ErrRegistrationVerificationNotFound) {
		return ErrRegistrationVerificationInvalid
	}
	if err != nil {
		return err
	}

	if !challenge.IsActive(now) {
		return ErrRegistrationVerificationInvalid
	}

	if verifyErr := VerifyPassword(challenge.CodeHash, code); verifyErr != nil {
		// Record the failed attempt (best-effort against a concurrent
		// terminal transition — RecordFailedAttempt's own filter already
		// guards this) and always report the same generic failure.
		_, _ = s.repo.RecordFailedAttempt(ctx, challenge.ID, now)
		return ErrRegistrationVerificationInvalid
	}

	// The atomic Consume call is the real compare-and-set: even though
	// IsActive was already checked above, a concurrent verification
	// attempt could have consumed (or exhausted the attempt limit on) this
	// exact challenge in between — Consume's own database-level filter is
	// what actually guarantees exactly one caller ever succeeds here.
	if _, err := s.repo.Consume(ctx, challenge.ID, now); err != nil {
		return ErrRegistrationVerificationInvalid
	}
	return nil
}
