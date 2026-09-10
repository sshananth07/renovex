package supplieraccess

import (
	"context"
	"time"
)

// InvitationAccessSnapshot is the complete invitation identity Phase D needs
// after an opaque credential resolves. Secret material and contractor-only
// invitation fields have no representation here and therefore cannot cross
// the module boundary accidentally.
type InvitationAccessSnapshot struct {
	CompanyID                 string
	SupplierID                string
	InvitationID              string
	NormalizedRecipientEmail  string
	AccessGeneration          int64
	CurrentIssuedRFQVersionID string
}

// InvitationAccessResolver resolves only a canonical invitation-secret hash.
// The raw token remains inside supplieraccess and no untrusted tenant or
// invitation identifier is accepted by this public-entry capability.
type InvitationAccessResolver interface {
	ResolveInvitationAccess(
		ctx context.Context,
		accessSecretHash string,
		accessedAt time.Time,
	) (InvitationAccessSnapshot, bool, error)
}

// InvitationViewRecorder records a successful link open after credential and
// state validation. AccessGeneration guards the cross-module write against a
// concurrent rotation or recipient replacement.
type InvitationViewRecorder interface {
	RecordInvitationViewed(
		ctx context.Context,
		companyID string,
		invitationID string,
		accessGeneration int64,
		viewedAt time.Time,
	) error
}

// InvitationAccessValidator re-reads one invitation after an access exchange
// or binding supplies its captured tenant-scoped identity. The caller compares
// the returned snapshot field-for-field; no repository crosses the module
// boundary.
type InvitationAccessValidator interface {
	ValidateInvitationAccess(
		ctx context.Context,
		companyID string,
		invitationID string,
		accessedAt time.Time,
	) (InvitationAccessSnapshot, bool, error)
}
