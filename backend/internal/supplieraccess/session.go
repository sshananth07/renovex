package supplieraccess

import "time"

// SupplierSession authenticates one browser to one immutable Supplier identity.
// It deliberately carries no invitation list; authorization is granted only by
// separate generation-bound bindings.
type SupplierSession struct {
	ID                       string
	CompanyID                string
	SupplierID               string
	RecipientEmailNormalized string
	TokenHash                string
	TokenGeneration          int64
	TokenKeyVersion          int
	CreatedFromChallengeID   string
	LastVerifiedChallengeID  string
	LastVerifiedAt           time.Time
	SlidingExpiresAt         time.Time
	AbsoluteExpiresAt        time.Time
	LastUsedAt               time.Time
	RevokedAt                *time.Time
	Revision                 int64
	SchemaVersion            int
}

// SupplierSessionInvitationBinding grants one session access to one exact
// invitation generation. A session never gains Supplier-wide invitation access.
type SupplierSessionInvitationBinding struct {
	ID                       string
	CompanyID                string
	SupplierSessionID        string
	SupplierID               string
	NormalizedRecipientEmail string
	InvitationID             string
	AccessGeneration         int64
	BoundAt                  time.Time
	LastValidatedAt          time.Time
	Revision                 int64
}
