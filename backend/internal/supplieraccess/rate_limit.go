package supplieraccess

import "time"

type VerificationRateLimitScope string

const (
	VerificationRateScopeIdentity VerificationRateLimitScope = "identity_invitation_generation"
	VerificationRateScopeClient   VerificationRateLimitScope = "client_address"
	// VerificationRateScopeResend is intentionally separate from challenge
	// creation: emailing an existing code must not consume the allowance for
	// creating a newer, superseding challenge.
	VerificationRateScopeResend VerificationRateLimitScope = "challenge_resend"
)

type VerificationRateReservation struct {
	ReservationID string
	RequestedAt   time.Time
}

// VerificationRateLimitState is a bounded CAS aggregate. Reservations are
// authoritative; TTL removes old documents only as housekeeping.
type VerificationRateLimitState struct {
	ID           string
	Scope        VerificationRateLimitScope
	ScopeKeyHash string
	Reservations []VerificationRateReservation
	Revision     int64
	UpdatedAt    time.Time
	ExpiresAt    time.Time
}
