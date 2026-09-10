package identity

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// RegistrationVerificationPurpose scopes a challenge to one authoritative
// action — a code issued for one purpose must never verify another
// (T2B: "purpose-bound OTPs"). This starts with exactly one value because
// only registration email verification exists yet; adding a second purpose
// later is a one-line addition, not a redesign.
type RegistrationVerificationPurpose string

const RegistrationVerificationPurposeRegistration RegistrationVerificationPurpose = "registration"

func (p RegistrationVerificationPurpose) IsValid() bool {
	return p == RegistrationVerificationPurposeRegistration
}

const (
	registrationVerificationCodeLength = 6
	registrationVerificationCodeMax    = 1_000_000 // 10^6, exclusive upper bound for a 6-digit code

	registrationVerificationLifetime     = 10 * time.Minute
	registrationVerificationMaxAttempts  = 5
	registrationVerificationIssueWindow  = time.Hour
	registrationVerificationMaxIssuances = 5
	registrationVerificationResendCooldown = time.Minute
)

// RegistrationVerificationChallenge is the durable, single-current record
// of one recipient+purpose's email verification state (T2B). Deliberately
// named for what it actually is — registration-specific — rather than a
// generic "OTPChallenge", so it never becomes an accidental generic auth
// primitive other features reach for later.
//
// Exactly one current challenge exists per (NormalizedRecipient, Purpose):
// issuing or resending a code atomically replaces/updates this same
// document (never inserts a second unrelated one) — see
// RegistrationVerificationRepository.IssueOrResume's doc comment.
type RegistrationVerificationChallenge struct {
	ID                  string
	NormalizedRecipient string
	Purpose             RegistrationVerificationPurpose

	// CodeHash is the bcrypt hash of the current code. The plaintext code
	// is never persisted anywhere (T2B: "never persist plaintext OTP").
	CodeHash string

	CreatedAt time.Time
	ExpiresAt time.Time

	// ConsumedAt is set exactly once, atomically, by Consume. A non-nil
	// value means this challenge can never verify again (T2B: "one-time
	// consumption").
	ConsumedAt *time.Time

	FailedAttempts int

	// IssuanceCount/IssuanceWindowStartedAt bound how many codes may be
	// issued (initial send + resends) for this recipient+purpose within a
	// rolling window — enforced atomically by the same repository
	// operation that issues a new code, never counted in application code
	// from separate documents.
	IssuanceCount           int
	IssuanceWindowStartedAt time.Time
	// ResendAvailableAt enforces a short cooldown between individual
	// issuances, independent of the rolling-window count above.
	ResendAvailableAt time.Time
}

// IsActive reports whether the challenge can still be verified against —
// not consumed, not expired, and under the attempt limit.
func (c RegistrationVerificationChallenge) IsActive(now time.Time) bool {
	return c.ConsumedAt == nil && now.Before(c.ExpiresAt) && c.FailedAttempts < registrationVerificationMaxAttempts
}

var (
	ErrRegistrationVerificationNotFound = errors.New("identity: registration verification challenge not found")
	// ErrRegistrationVerificationInvalid collapses every rejection reason a
	// submitted code can hit (wrong code, expired, consumed, attempt limit,
	// purpose mismatch, cross-tenant/recipient mismatch) into one public
	// error — never distinguishable by the caller, so a submitted code
	// cannot be used as an oracle for which failure mode occurred (T2B:
	// "do not leak whether unrelated accounts/users exist").
	ErrRegistrationVerificationInvalid = errors.New("identity: registration verification code is invalid or expired")
	// ErrRegistrationVerificationRateLimited is returned when issuance
	// (initial send or resend) would exceed the bounded resend cooldown or
	// rolling issuance window.
	ErrRegistrationVerificationRateLimited = errors.New("identity: registration verification code requested too soon or too often")
)

// generateRegistrationVerificationCode returns a cryptographically random
// 6-digit numeric code (zero-padded), plus its bcrypt hash for persistence.
// The plaintext code is returned ONLY so the caller can email it — it must
// never be persisted (T2B: "never persist plaintext OTP").
func generateRegistrationVerificationCode() (plaintext, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(registrationVerificationCodeMax))
	if err != nil {
		return "", "", fmt.Errorf("identity: generate registration verification code: %w", err)
	}
	plaintext = fmt.Sprintf("%0*d", registrationVerificationCodeLength, n.Int64())
	hash, err = HashPassword(plaintext)
	if err != nil {
		return "", "", fmt.Errorf("identity: hash registration verification code: %w", err)
	}
	return plaintext, hash, nil
}
