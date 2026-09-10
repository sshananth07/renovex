package rfqissuance

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"
)

// InvitationAccessProjection is the complete identity rfqissuance permits a
// public Supplier-access consumer to observe. Secret hashes, recipient display
// data, contractor fields and delivery history are structurally absent.
type InvitationAccessProjection struct {
	CompanyID                 string
	SupplierID                string
	InvitationID              string
	NormalizedRecipientEmail  string
	AccessGeneration          int64
	CurrentIssuedRFQVersionID string
}

// ResolveInvitationAccessByHash resolves the opaque public credential without
// accepting a tenant or invitation identifier from the caller.
//
// Credential/state mismatches return found=false with no distinguishing error.
// Infrastructure failures remain errors so the public boundary can report its
// bounded 503 rather than pretending an outage is an invalid link.
func (s *Service) ResolveInvitationAccessByHash(ctx context.Context,
	accessSecretHash string, accessedAt time.Time) (
	InvitationAccessProjection, bool, error) {

	if s.invitations == nil || s.suppliers == nil {
		return InvitationAccessProjection{}, false, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitationByAccessSecretHash(
		ctx, accessSecretHash)
	if errors.Is(err, ErrInvitationNotFound) {
		return InvitationAccessProjection{}, false, nil
	}
	if err != nil {
		return InvitationAccessProjection{}, false, err
	}

	// Mongo's indexed equality found the candidate; this second comparison
	// preserves one constant-time credential-verification rule even if a future
	// repository implementation resolves candidates differently.
	if subtle.ConstantTimeCompare(
		[]byte(accessSecretHash), []byte(invitation.AccessSecretHash)) != 1 {
		return InvitationAccessProjection{}, false, nil
	}
	if !invitation.PermitsAccess(accessedAt) ||
		invitation.CompanyID == "" ||
		invitation.SupplierID == "" ||
		invitation.ID == "" ||
		invitation.RecipientEmailNormalized == "" ||
		invitation.AccessGeneration < 1 ||
		invitation.CurrentIssuedRFQVersionID == "" {
		return InvitationAccessProjection{}, false, nil
	}

	invitable, err := s.suppliers.SupplierIsInvitable(
		ctx, invitation.CompanyID, invitation.SupplierID)
	if err != nil {
		return InvitationAccessProjection{}, false, err
	}
	if !invitable {
		return InvitationAccessProjection{}, false, nil
	}

	return InvitationAccessProjection{
		CompanyID:                 invitation.CompanyID,
		SupplierID:                invitation.SupplierID,
		InvitationID:              invitation.ID,
		NormalizedRecipientEmail:  invitation.RecipientEmailNormalized,
		AccessGeneration:          invitation.AccessGeneration,
		CurrentIssuedRFQVersionID: invitation.CurrentIssuedRFQVersionID,
	}, true, nil
}

// RecordInvitationViewed is the primitive-only capability used after a public
// link has validated. The generation remains in the repository filter so a
// replacement or rotation racing the open cannot attribute the old recipient's
// view to the new credential.
func (s *Service) RecordInvitationViewed(ctx context.Context, companyID,
	invitationID string, accessGeneration int64, viewedAt time.Time) error {

	if s.invitations == nil {
		return ErrInvitationsNotConfigured
	}
	return s.invitations.RecordViewed(
		ctx, companyID, invitationID, accessGeneration, viewedAt)
}

// ResolveInvitationAccessByIdentity revalidates a short-lived browser exchange
// against the current authoritative invitation. The exchange supplies the
// tenant-scoped identity captured during the token open; supplieraccess still
// receives only the same narrow projection and must compare every field.
func (s *Service) ResolveInvitationAccessByIdentity(ctx context.Context,
	companyID, invitationID string, accessedAt time.Time) (
	InvitationAccessProjection, bool, error) {

	if s.invitations == nil || s.suppliers == nil {
		return InvitationAccessProjection{}, false, ErrInvitationsNotConfigured
	}
	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if errors.Is(err, ErrInvitationNotFound) {
		return InvitationAccessProjection{}, false, nil
	}
	if err != nil {
		return InvitationAccessProjection{}, false, err
	}
	if !invitation.PermitsAccess(accessedAt) ||
		invitation.CompanyID == "" || invitation.SupplierID == "" ||
		invitation.ID == "" || invitation.RecipientEmailNormalized == "" ||
		invitation.AccessGeneration < 1 ||
		invitation.CurrentIssuedRFQVersionID == "" {
		return InvitationAccessProjection{}, false, nil
	}
	invitable, err := s.suppliers.SupplierIsInvitable(
		ctx, invitation.CompanyID, invitation.SupplierID)
	if err != nil {
		return InvitationAccessProjection{}, false, err
	}
	if !invitable {
		return InvitationAccessProjection{}, false, nil
	}
	return InvitationAccessProjection{
		CompanyID:                 invitation.CompanyID,
		SupplierID:                invitation.SupplierID,
		InvitationID:              invitation.ID,
		NormalizedRecipientEmail:  invitation.RecipientEmailNormalized,
		AccessGeneration:          invitation.AccessGeneration,
		CurrentIssuedRFQVersionID: invitation.CurrentIssuedRFQVersionID,
	}, true, nil
}
