package supplieraccess

import "time"

// EmailVerificationChallenge is the authoritative, single-use proof record.
// CodeVerifier is keyed; the raw six-digit code has no persisted field.
type EmailVerificationChallenge struct {
	ID                       string
	ExchangeID               string
	CompanyID                string
	SupplierID               string
	RecipientEmailNormalized string
	InvitationID             string
	AccessGeneration         int64
	ChallengeOperationID     string
	CodeVerifier             string
	CodeKeyVersion           int
	AttemptsRemaining        int
	ExpiresAt                time.Time
	SupersededAt             *time.Time
	LockedAt                 *time.Time
	ConsumedAt               *time.Time
	ConsumedOperationID      string
	SupplierSessionID        string
	TargetTokenGeneration    int64
	SessionTokenKeyVersion   int
	Revision                 int64
	CreatedAt                time.Time
	SchemaVersion            int
}

// VerificationSessionTarget is selected before challenge consumption and
// becomes the durable recovery instruction for every later session write.
// It contains no raw session credential.
type VerificationSessionTarget struct {
	OperationID       string
	SupplierSessionID string
	TokenGeneration   int64
	TokenKeyVersion   int
}

type VerificationDeliveryStatus string

const (
	VerificationDeliveryPending  VerificationDeliveryStatus = "pending"
	VerificationDeliverySent     VerificationDeliveryStatus = "sent"
	VerificationDeliveryFailed   VerificationDeliveryStatus = "failed"
	VerificationDeliveryObsolete VerificationDeliveryStatus = "obsolete"
)

// VerificationDeliveryFailureCode is a bounded module-owned token. Provider
// error text must be mapped before it reaches this type or persistence.
type VerificationDeliveryFailureCode string

// VerificationDeliveryAttempt records one synchronous mail intent. Identity is
// immutable; only a pending status may transition.
type VerificationDeliveryAttempt struct {
	ID          string
	CompanyID   string
	ChallengeID string
	OperationID string
	Status      VerificationDeliveryStatus
	FailureCode *VerificationDeliveryFailureCode
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
