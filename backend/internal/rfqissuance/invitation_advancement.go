package rfqissuance

import (
	"context"
	"time"
)

// Invitation advancement and its reconciliation (design spec §4.2 step 6-7,
// §3.5, §10.7).
//
// The invariant that shapes all of this: an invitation ALWAYS points at a
// COMPLETE issued version. Advancement therefore happens strictly after the new
// version exists, and a failure to advance is a reconcilable gap rather than a
// failed issuance — an invitation lagging on version N keeps exposing the whole
// of version N, which is a correct if stale view, whereas rolling back the
// version would delete something a Supplier may already have opened.

// advanceInvitationsAfterIssuance moves every non-revoked invitation onto a
// newly issued version.
//
// It returns no error by design: the caller has already created the immutable
// version, and reporting a failure here would misdescribe a successful issuance
// as a failed one. The gap is repaired by ReconcileInvitationAdvancement.
func (s *Service) advanceInvitationsAfterIssuance(ctx context.Context,
	companyID, actorUserID, rfqChainID, issuedVersionID string) {

	if s.invitations == nil {
		return
	}

	advanced, err := s.invitations.AdvanceInvitationsToVersion(ctx, companyID,
		rfqChainID, issuedVersionID)
	if err != nil {
		// Recorded, not raised. The operator needs to know a reconciliation is
		// outstanding; the contractor's issuance genuinely succeeded.
		s.logEvent("invitation_advancement_failed").
			Str("companyId", companyID).
			Str("rfqChainId", rfqChainID).
			Str("issuedVersionId", issuedVersionID).
			Msg("invitations lag the newly issued version and need reconciliation")
		return
	}

	if advanced > 0 {
		_ = s.audit.RecordSupplierInvitationAdvanced(ctx, companyID, actorUserID,
			rfqChainID, issuedVersionID, advanced)
	}
}

// ReconcileInvitationAdvancement completes an interrupted advancement (§10.7).
//
// It moves invitations only onto the chain's CURRENT issued version — never a
// version it selects itself — so a repair can neither invent a target nor move
// an invitation backwards onto a superseded one.
//
// Idempotent: on an aligned chain the underlying update matches nothing and the
// call reports zero.
func (s *Service) ReconcileInvitationAdvancement(ctx context.Context,
	companyID, actorUserID, rfqChainID string) (int64, error) {

	if s.invitations == nil || s.chains == nil {
		return 0, ErrInvitationsNotConfigured
	}

	// Resolving the tenant-scoped chain first keeps a foreign identifier the
	// same 404 as an absent one.
	chain, err := s.chains.FindChain(ctx, companyID, rfqChainID)
	if err != nil {
		return 0, err
	}
	if chain.CurrentIssuedVersionID == nil {
		// Nothing has been issued, so there is no known transition to complete.
		return 0, nil
	}

	advanced, err := s.invitations.AdvanceInvitationsToVersion(ctx, companyID,
		rfqChainID, *chain.CurrentIssuedVersionID)
	if err != nil {
		return 0, err
	}

	if advanced > 0 {
		_ = s.audit.RecordSupplierInvitationAdvanced(ctx, companyID, actorUserID,
			rfqChainID, *chain.CurrentIssuedVersionID, advanced)
	}
	return advanced, nil
}

// ObsoleteSupersededDeliveryAttempts marks PENDING attempts whose access
// generation has since been superseded (§3.5).
//
// A pending attempt carries a link derived under the generation current when it
// was created. Once the secret rotates or the recipient is replaced, that link
// no longer verifies — leaving the attempt `pending` would describe a delivery
// that can never succeed.
//
// Only PENDING attempts move. A `sent` attempt is historical fact: the mail
// genuinely left the system, and rewriting it would erase the record that the
// previous recipient was contacted.
func (s *Service) ObsoleteSupersededDeliveryAttempts(ctx context.Context,
	companyID, invitationID string) (int64, error) {

	if s.invitations == nil || s.deliveries == nil {
		return 0, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return 0, err
	}

	// Status and generation are part of the repository update filter. A
	// list-then-write loop could otherwise race a sender and rewrite a
	// just-sent historical attempt as obsolete.
	return s.deliveries.ObsoletePendingAttemptsBeforeGeneration(ctx, companyID,
		invitationID, invitation.AccessGeneration)
}

// InvitationPermitsAccessNow reports whether an invitation may currently be
// opened, evaluating expiry against the CURRENT time (§5.4, §6.3).
//
// This is the read supplieraccess will consume in Phase D. It lives here
// because rfqissuance owns the invitation record; supplieraccess reaches it
// through a capability rather than a direct read.
func (s *Service) InvitationPermitsAccessNow(ctx context.Context,
	companyID, invitationID string) (bool, error) {

	if s.invitations == nil {
		return false, ErrInvitationsNotConfigured
	}
	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return false, err
	}
	return invitation.PermitsAccess(time.Now()), nil
}
