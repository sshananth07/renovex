package identity

import (
	"context"
	"time"
)

// RegistrationVerificationRepository persists RegistrationVerificationChallenges.
// Exactly one current challenge exists per (NormalizedRecipient, Purpose).
type RegistrationVerificationRepository interface {
	// IssueOrResume atomically creates or updates the ONE current challenge
	// for recipient+purpose in a single repository operation — never a
	// separate find-then-count-then-insert sequence, so two concurrent
	// issuance requests cannot both succeed and cannot both bypass the
	// resend cooldown/issuance window (T2B: "the same repository operation
	// should enforce the issuance window/cooldown"). codeHash is the new
	// code's bcrypt hash; the plaintext is never passed to the repository.
	//
	// Returns ErrRegistrationVerificationRateLimited (without mutating
	// anything) when the existing current challenge's ResendAvailableAt
	// has not yet passed, or its rolling-window IssuanceCount has already
	// reached the bound.
	IssueOrResume(ctx context.Context, recipient string, purpose RegistrationVerificationPurpose, codeHash string, now time.Time) (RegistrationVerificationChallenge, error)

	// FindCurrent returns the one current challenge for recipient+purpose,
	// or ErrRegistrationVerificationNotFound if none exists.
	FindCurrent(ctx context.Context, recipient string, purpose RegistrationVerificationPurpose) (RegistrationVerificationChallenge, error)

	// RecordFailedAttempt atomically increments FailedAttempts on the
	// named challenge, guarded by IsActive at the time of the update (a
	// challenge that has since expired/been consumed/hit the limit is left
	// untouched — the caller's own re-read after this call observes the
	// authoritative state). Returns the updated challenge.
	RecordFailedAttempt(ctx context.Context, challengeID string, now time.Time) (RegistrationVerificationChallenge, error)

	// Consume atomically marks the named challenge consumed — succeeds
	// only when it is still active (IsActive) at the moment of the
	// database-level compare-and-set, guaranteeing exactly one concurrent
	// Consume call can ever win for a given challenge (T2B: "same for
	// failed-attempt increment where practical... exactly one request
	// succeeds"). Returns ErrRegistrationVerificationInvalid if the
	// challenge is no longer active by the time this call reaches the
	// database (already consumed, expired, or over the attempt limit).
	Consume(ctx context.Context, challengeID string, now time.Time) (RegistrationVerificationChallenge, error)
}
