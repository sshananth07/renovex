package rfqissuance

import (
	"net/mail"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// The stable Supplier Invitation (design spec §3.4, §5).
//
// ONE invitation exists per Company + RFQChain + Supplier for the life of the
// chain. It ADVANCES its version pointer as amendments are issued rather than
// being replaced, which is what keeps a Supplier's link working across versions
// and keeps their whole offer history attached to one identity.

// InvitationStatus is the lifecycle state.
//
// Deliberately NOT a status: "viewed". Whether the Supplier opened the link is
// recorded as FirstViewedAt/LastViewedAt (§1A.4, §6.5) — folding it into the
// lifecycle would conflate "we know they looked" with "this invitation is
// usable", which are independent facts.
type InvitationStatus string

// The four invitation lifecycle states (§3.4).
const (
	InvitationStatusDraft   InvitationStatus = "draft"
	InvitationStatusActive  InvitationStatus = "active"
	InvitationStatusExpired InvitationStatus = "expired"
	InvitationStatusRevoked InvitationStatus = "revoked"
)

// MaxRecipientEmailLength bounds the stored recipient address. An unbounded
// value would be an index-size and log-noise hazard on a field that is part of
// the Supplier identity key.
const MaxRecipientEmailLength = 254

// SupplierInvitation is the stable per-Supplier invitation.
type SupplierInvitation struct {
	ID                        string
	CompanyID                 string
	RFQChainID                string
	SupplierID                string
	CurrentIssuedRFQVersionID string

	RecipientName  string
	RecipientEmail string
	// RecipientEmailNormalized is the IDENTITY key: a Supplier session is bound
	// to Company + Supplier + this value, so it must be derived, never
	// client-supplied.
	RecipientEmailNormalized string

	Status    InvitationStatus
	ExpiresAt time.Time
	RevokedAt *time.Time

	// AccessSecretHash is the SHA-256 of the DERIVED invitation token. The raw
	// token is never persisted; it is re-derived on demand from the keyring
	// (§6.1A), which is what lets copy-link reproduce the current link without
	// rotating it.
	AccessSecretHash string
	// AccessGeneration increments on every rotation, recipient replacement,
	// and explicit reactivation.
	// It is what invalidates previously issued links: a token derived under an
	// older generation no longer matches the stored hash.
	AccessGeneration int64
	// SecretKeyVersion records which keyring version derived the current
	// secret, so the link stays reproducible after the active version advances.
	SecretKeyVersion int

	// FirstViewedAt and LastViewedAt record successful secure-link opens only
	// (§6.5). A failed, expired or wrong-generation attempt never touches them.
	FirstViewedAt *time.Time
	LastViewedAt  *time.Time

	Revision        int64
	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SchemaVersion   int
}

// SupplierInvitationSchemaVersion is the current persisted shape.
const SupplierInvitationSchemaVersion = 1

// PermitsAccess reports whether this invitation may currently be opened.
//
// Expiry and revocation are SEPARATE controls (§5.4) and both must pass. The
// RevokedAt TIMESTAMP is authoritative rather than the status string: a
// revocation must block access immediately, without waiting for any status
// write to land, so a stale "active" status can never resurrect a revoked
// invitation.
//
// Expiry is compared against the CURRENT time on every call rather than a
// persisted flag, which cannot go stale.
func (i SupplierInvitation) PermitsAccess(now time.Time) bool {
	if i.RevokedAt != nil {
		return false
	}
	if i.Status != InvitationStatusActive {
		return false
	}
	return now.Before(i.ExpiresAt)
}

// NormalizeRecipientEmail derives the identity form of a recipient address.
//
// Lower-cased and trimmed: the Supplier who types "Sales@Supplier.COM" into the
// verification form is the same recipient the contractor invited as
// "sales@supplier.com". Without one canonical form they would be two identities
// and the second could never gain access.
func NormalizeRecipientEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" || len(trimmed) > MaxRecipientEmailLength {
		return "", ErrInvalidRecipientEmail
	}
	if _, err := mail.ParseAddress(trimmed); err != nil {
		return "", ErrInvalidRecipientEmail
	}
	return strings.ToLower(trimmed), nil
}

// NewInvitationInput carries a new invitation.
type NewInvitationInput struct {
	CompanyID                 string
	RFQChainID                string
	SupplierID                string
	CurrentIssuedRFQVersionID string

	RecipientName  string
	RecipientEmail string
	ExpiresAt      time.Time

	CreatedByUserID string
}

// NewInvitation validates and builds a DRAFT invitation.
//
// It starts as a draft because creation must not contact the Supplier (§5.1);
// sending is a separate explicit action. The secret hash and key version are
// stamped by the service, which owns the keyring.
func NewInvitation(input NewInvitationInput) (SupplierInvitation, error) {
	name, limitErr := procurementlimits.TrimText(input.RecipientName,
		procurementlimits.MaxDisplayTextRunes)
	if limitErr != nil {
		return SupplierInvitation{}, ErrInputLimitExceeded
	}
	if name == "" {
		return SupplierInvitation{}, ErrRecipientNameRequired
	}

	normalized, err := NormalizeRecipientEmail(input.RecipientEmail)
	if err != nil {
		return SupplierInvitation{}, err
	}

	// An expiry already in the past would create an invitation that is dead on
	// arrival — the contractor would send a link that never opens.
	now := time.Now()
	if !input.ExpiresAt.After(now) {
		return SupplierInvitation{}, ErrInvitationExpiryNotInFuture
	}
	if err := procurementlimits.ValidateInvitationExpiry(now,
		input.ExpiresAt); err != nil {
		return SupplierInvitation{}, ErrInvalidBusinessDate
	}

	return SupplierInvitation{
		CompanyID:                 input.CompanyID,
		RFQChainID:                input.RFQChainID,
		SupplierID:                input.SupplierID,
		CurrentIssuedRFQVersionID: input.CurrentIssuedRFQVersionID,
		RecipientName:             name,
		RecipientEmail:            strings.TrimSpace(input.RecipientEmail),
		RecipientEmailNormalized:  normalized,
		Status:                    InvitationStatusDraft,
		ExpiresAt:                 input.ExpiresAt,
		AccessGeneration:          1,
		CreatedByUserID:           input.CreatedByUserID,
		CreatedAt:                 now,
		UpdatedAt:                 now,
		SchemaVersion:             SupplierInvitationSchemaVersion,
	}, nil
}
