package supplieroffers

import "time"

const SupplierOfferWithdrawalSchemaVersion = 1

// SupplierOfferWithdrawal is the immutable commercial record reconstructed
// from a winning eligibility claim. Its reason is historical fact and is not
// sourced again from a retry or reconciliation request.
type SupplierOfferWithdrawal struct {
	ID                     string
	CompanyID              string
	OfferChainID           string
	SupplierOfferVersionID string
	InvitationID           string
	RecipientIdentity      string
	OperationID            string
	Reason                 string
	WithdrawnAt            time.Time
	SchemaVersion          int
}
