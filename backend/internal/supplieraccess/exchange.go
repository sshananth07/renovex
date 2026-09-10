package supplieraccess

import "time"

// SupplierAccessExchange is a short-lived browser handoff. It is credential
// transport only and grants no Supplier authorization by itself.
type SupplierAccessExchange struct {
	ID                       string
	ExchangeTokenHash        string
	CompanyID                string
	SupplierID               string
	InvitationID             string
	AccessGeneration         int64
	NormalizedRecipientEmail string
	CreatedAt                time.Time
	ExpiresAt                time.Time
	ConsumedAt               *time.Time
	ChallengeID              *string
}
