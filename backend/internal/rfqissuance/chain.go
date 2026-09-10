package rfqissuance

import "time"

// RFQIssuanceChain is the SERIALIZATION POINT for version creation
// (design spec §3.1, §10.1).
//
// One chain exists per companyId + rfqChainId. Its current snapshot supplies
// the one candidate LatestIssuedVersion + 1; concurrent issuances therefore
// contend for the same immutable-version unique key and only one can win.
//
// LatestIssuedVersion records the version at the current chain pointer. It may
// temporarily lag an already-created immutable version if execution stops
// between the version insert and pointer advance; bounded reconciliation repairs
// that lag from the immutable version rather than inventing content or numbers.
type RFQIssuanceChain struct {
	ID                     string
	CompanyID              string
	RFQChainID             string
	LatestIssuedVersion    int
	CurrentIssuedVersionID *string
	Revision               int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
	SchemaVersion          int
}

// RFQIssuanceChainSchemaVersion is the current persisted shape.
const RFQIssuanceChainSchemaVersion = 1
