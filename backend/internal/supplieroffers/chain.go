package supplieroffers

import "time"

const SupplierOfferChainSchemaVersion = 1

// SupplierOfferChain serializes immutable submission version numbers for one
// invitation and one issued RFQ version. Mutable draft edits deliberately do
// not advance this aggregate.
type SupplierOfferChain struct {
	ID                     string
	CompanyID              string
	InvitationID           string
	IssuedRFQVersionID     string
	LatestSubmittedVersion int
	LatestSubmittedID      *string
	Revision               int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
	SchemaVersion          int
}
