package access

import "time"

// ResourceTypeQuotation is the only ResourceType M6 implements. The field
// exists so the same collection/index shape generalizes to "rfq" later
// without a migration (design spec §2.1).
const ResourceTypeQuotation = "quotation"

// GranteeTypeClient is the only GranteeType M6 implements.
const GranteeTypeClient = "client"

// AccessGrantStatus is the STORED status of a grant. Note that a stored
// status of "active" is NOT sufficient to make a token usable — see
// IsExternallyLive (design spec §2.3): the coordinator, not this field,
// is the authority on whether a grant is externally active.
type AccessGrantStatus string

const (
	AccessGrantStatusActive  AccessGrantStatus = "active"
	AccessGrantStatusRevoked AccessGrantStatus = "revoked"
	// "expired" is deliberately NOT a stored status — expiry is derived at
	// read time from ExpiresAt, never written by a background sweep.
)

// Revocation reasons. These drive both the rotation branch selection
// (design spec §2.5) and the contractor-facing effective status (§6.5.4).
const (
	RevokedReasonManual     = "manual"
	RevokedReasonSuperseded = "superseded"
	RevokedReasonRotated    = "rotated"
)

// Permissions granted to a Client for a shared Quotation. "download" is
// deliberately absent — M6 has no PDF route (design spec §2.1).
var ClientQuotationPermissions = []string{"view", "accept", "reject", "request_changes"}

// EffectiveStatus values, contractor-facing only. Never exposed through
// the unauthenticated external route, which returns a uniform 410 for
// every unusable state (design spec §6.5.4/§15).
const (
	EffectiveStatusActive     = "active"
	EffectiveStatusExpired    = "expired"
	EffectiveStatusRevoked    = "revoked"
	EffectiveStatusSuperseded = "superseded"
	EffectiveStatusRotated    = "rotated"
)

// AccessGrant is one issued secure link for exactly one Quotation version.
// Multiple historical grants may exist for the same ResourceID (one per
// share/rotation) — there is no per-resource uniqueness constraint.
type AccessGrant struct {
	ID               string
	CompanyID        string
	ProjectID        string
	ResourceType     string
	ResourceID       string // == quotations.Quotation.ID
	ResourceGroupKey string // ResourceGroupKeyForQuotation(quotationNumber)
	QuotationNumber  string
	GranteeType      string
	GranteeID        string // == quotations.Quotation.ClientID
	Permissions      []string
	TokenHash        string
	Status           AccessGrantStatus
	Revision         int64 // guards direct single-grant mutation only
	CreatedByUserID  string
	CreatedAt        time.Time
	ExpiresAt        time.Time // always concrete — never-expiring grants are unsupported
	RevokedAt        *time.Time
	RevokedReason    string
	SchemaVersion    int
}

// AccessGroupState is the single coordination point for one commercial
// chain (one QuotationNumber). Both the contractor share/rotate path and
// the Client decision path must win a conditional write against this same
// document, which is what makes the accepted-chain invariant hold across
// two different collections (design spec §2.5).
type AccessGroupState struct {
	ID               string
	CompanyID        string
	ResourceType     string
	ResourceGroupKey string

	CurrentResourceID  string  // the Quotation version currently open for this chain
	ActiveGrantID      *string // nil when no grant is currently active
	AcceptedResourceID *string // set exactly once, ever — the terminal chain fact

	Revision      int64
	CreatedAt     time.Time
	SchemaVersion int
}

// ResourceGroupKeyForQuotation builds the namespaced grouping key for a
// Quotation chain. The namespace prefix lets a future RFQ group
// ("rfq:RFQ-00124") share the same collection and index shape.
func ResourceGroupKeyForQuotation(quotationNumber string) string {
	return ResourceTypeQuotation + ":" + quotationNumber
}

// IsExternallyLive reports whether a token resolving to this grant may be
// used externally. ALL FIVE conditions of design spec §2.3 must hold — in
// particular, the last two make the coordinator (not AccessGrant.Status)
// the authority, so an orphaned or superseded-but-stored-active grant is
// never usable.
func IsExternallyLive(grant AccessGrant, coordinator AccessGroupState, now time.Time) bool {
	if grant.Status != AccessGrantStatusActive {
		return false
	}
	if !now.Before(grant.ExpiresAt) {
		return false
	}
	if coordinator.ActiveGrantID == nil || *coordinator.ActiveGrantID != grant.ID {
		return false
	}
	if coordinator.CurrentResourceID != grant.ResourceID {
		return false
	}
	return true
}

// EffectiveStatus derives the contractor-facing status of a grant from its
// stored status, revocation reason, expiry, and the coordinator's view.
//
// The coordinator is the immediate authority on effective access: a grant
// whose stored Status is still "active" reads as superseded or rotated the
// moment the coordinator switches away from it, without waiting for the
// best-effort stored-status cleanup write to land. The two orphan cases are
// deliberately distinguished:
//   - the chain moved to a DIFFERENT version  -> "superseded"
//   - a replacement grant was issued for the SAME version -> "rotated"
func EffectiveStatus(grant AccessGrant, coordinator AccessGroupState, now time.Time) string {
	if grant.Status == AccessGrantStatusRevoked {
		switch grant.RevokedReason {
		case RevokedReasonSuperseded:
			return EffectiveStatusSuperseded
		case RevokedReasonRotated:
			return EffectiveStatusRotated
		default:
			return EffectiveStatusRevoked
		}
	}

	// Stored-active from here on — the coordinator decides what that
	// actually means right now.
	if coordinator.CurrentResourceID != grant.ResourceID {
		return EffectiveStatusSuperseded
	}
	if coordinator.ActiveGrantID == nil || *coordinator.ActiveGrantID != grant.ID {
		return EffectiveStatusRotated
	}
	if !now.Before(grant.ExpiresAt) {
		return EffectiveStatusExpired
	}
	return EffectiveStatusActive
}
