package supplieraccess

import "errors"

var (
	// ErrSupplierOutcomeNotFound is tenant- and Supplier-safe: another
	// Supplier's outcome and a non-existent one are indistinguishable (D3).
	ErrSupplierOutcomeNotFound = errors.New(
		"supplieraccess: award outcome not found")
	// ErrSupplierOutcomesUnavailable reports that the award capability was
	// never wired, so the route fails closed rather than reporting no outcome.
	ErrSupplierOutcomesUnavailable = errors.New(
		"supplieraccess: award outcomes are unavailable")

	ErrSupplierAccessNotConfigured = errors.New("supplier access is not configured")
	ErrInvalidSupplierCredential   = errors.New("supplier access credential is invalid")
	ErrInvitationAccessInvalid     = errors.New("supplier invitation access changed")
	ErrInvalidVerificationRequest  = errors.New(
		"supplier verification request is invalid")
	ErrChallengeOperationConflict = errors.New(
		"supplier verification operation conflicts with an existing challenge")
	ErrVerificationChallengeNotCurrent = errors.New(
		"supplier verification challenge is not current")
	ErrVerificationMailDeliveryFailed = errors.New(
		"supplier verification email could not be delivered")

	ErrInvalidAccessExchange       = errors.New("supplier access exchange is invalid")
	ErrAccessExchangeAlreadyExists = errors.New("supplier access exchange already exists")
	ErrExchangeTokenHashCollision  = errors.New("supplier access exchange token hash collision")
	ErrAccessExchangeNotFound      = errors.New("supplier access exchange not found")
	ErrAccessExchangeConsumed      = errors.New("supplier access exchange has already been consumed")

	ErrInvalidVerificationChallenge   = errors.New("supplier verification challenge is invalid")
	ErrVerificationChallengeExists    = errors.New("supplier verification challenge already exists")
	ErrChallengeOperationAlreadyUsed  = errors.New("supplier verification challenge operation already used")
	ErrExchangeChallengeAlreadyExists = errors.New(
		"supplier access exchange already has a verification challenge")
	ErrVerificationChallengeNotFound       = errors.New("supplier verification challenge not found")
	ErrVerificationChallengeNotConfirmable = errors.New(
		"supplier verification challenge cannot be confirmed")

	ErrInvalidVerificationDeliveryAttempt = errors.New(
		"supplier verification delivery attempt is invalid")
	ErrVerificationDeliveryOperationAlreadyUsed = errors.New(
		"supplier verification delivery operation already used")
	ErrVerificationDeliveryAttemptNotFound = errors.New(
		"supplier verification delivery attempt not found")
	ErrVerificationDeliveryNotPending = errors.New(
		"supplier verification delivery attempt is not pending")

	ErrInvalidVerificationRateLimitState = errors.New(
		"supplier verification rate-limit state is invalid")
	ErrVerificationRateLimitStateAlreadyExists = errors.New(
		"supplier verification rate-limit state already exists")
	ErrVerificationRateLimitStateNotFound = errors.New(
		"supplier verification rate-limit state not found")
	ErrVerificationRateLimitStateConflict = errors.New(
		"supplier verification rate-limit state changed concurrently")
	ErrVerificationRateLimited = errors.New(
		"supplier verification request rate limited")
	ErrVerificationRateLimitUnavailable = errors.New(
		"supplier verification rate limiter unavailable")

	ErrInvalidSupplierSession       = errors.New("supplier session is invalid")
	ErrSupplierSessionAlreadyExists = errors.New(
		"supplier session already exists")
	ErrSupplierSessionTokenHashCollision = errors.New(
		"supplier session token hash collision")
	ErrSupplierSessionChallengeAlreadyUsed = errors.New(
		"supplier verification challenge already created a session")
	ErrSupplierSessionNotFound = errors.New("supplier session not found")
	ErrSupplierSessionConflict = errors.New("supplier session changed concurrently")
	ErrSupplierCSRFRejected    = errors.New(
		"supplier request CSRF validation failed")

	ErrInvalidSessionInvitationBinding = errors.New(
		"supplier session invitation binding is invalid")
	ErrSessionInvitationBindingAlreadyExists = errors.New(
		"supplier session invitation binding already exists")
	ErrSessionInvitationBindingNotFound = errors.New(
		"supplier session invitation binding not found")
	ErrSessionInvitationBindingConflict = errors.New(
		"supplier session invitation binding changed concurrently")
)

// ChallengeOperationConflictError wraps ErrChallengeOperationConflict and
// names the challenge that already claims the OperationID. CreateChallenge's
// anti-replay behavior is unchanged — the conflict is still reported, and a
// caller with no legitimate claim on the prior operation still cannot use
// this challenge. What this adds is a way for a caller that DOES own that
// prior operation (e.g. re-running an idempotent seed/import routine, not a
// credential replay) to recover a session for it via VerifyChallenge, which
// already treats an already-consumed challenge idempotently through
// recoverVerificationSession.
type ChallengeOperationConflictError struct {
	ExistingChallengeID string
}

func (e *ChallengeOperationConflictError) Error() string {
	return ErrChallengeOperationConflict.Error()
}

func (e *ChallengeOperationConflictError) Unwrap() error {
	return ErrChallengeOperationConflict
}
