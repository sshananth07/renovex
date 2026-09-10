package composition

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// --- supplieroffers -> rfqissuance.OfferWorkspaceCoordinator ---
//
// Recipient replacement spans two modules: rfqissuance owns the authoritative
// invitation, supplieroffers owns the Supplier's draft workspace. Spec §5.3A
// orders them as claim -> authoritative invitation replacement -> archival, and
// this adapter is the ONLY path between them. Neither module reaches the
// other's repository, and only primitives cross.

// offerWorkspaceSource is the narrow slice of supplieroffers.Service this
// adapter needs.
type offerWorkspaceSource interface {
	PrepareRecipientReplacement(
		context.Context, supplieroffers.RecipientReplacementPreparation) error
	CompleteRecipientReplacement(
		context.Context, supplieroffers.RecipientReplacementCompletion) error
	AbortRecipientReplacement(
		context.Context, supplieroffers.RecipientReplacementAbort) error
}

// OfferWorkspaceAdapter satisfies rfqissuance.OfferWorkspaceCoordinator from
// supplieroffers' replacement-barrier operations.
type OfferWorkspaceAdapter struct {
	offers offerWorkspaceSource
}

// NewOfferWorkspaceAdapter wraps a supplieroffers.Service.
func NewOfferWorkspaceAdapter(source offerWorkspaceSource) *OfferWorkspaceAdapter {
	return &OfferWorkspaceAdapter{offers: source}
}

// PrepareRecipientReplacement takes the durable barrier before the
// authoritative invitation write.
func (a *OfferWorkspaceAdapter) PrepareRecipientReplacement(ctx context.Context,
	input rfqissuance.RecipientReplacementPreparation) error {

	return mapOfferWorkspaceError(a.offers.PrepareRecipientReplacement(ctx,
		supplieroffers.RecipientReplacementPreparation{
			CompanyID:                  input.CompanyID,
			InvitationID:               input.InvitationID,
			IssuedRFQVersionID:         input.IssuedRFQVersionID,
			PreviousRecipientIdentity:  input.PreviousRecipientIdentity,
			CandidateRecipientIdentity: input.CandidateRecipientIdentity,
			ReplacementOperationID:     input.ReplacementOperationID,
		}))
}

// CompleteRecipientReplacement archives the claim once the invitation write is
// confirmed.
func (a *OfferWorkspaceAdapter) CompleteRecipientReplacement(ctx context.Context,
	input rfqissuance.RecipientReplacementCompletion) error {

	return mapOfferWorkspaceError(a.offers.CompleteRecipientReplacement(ctx,
		supplieroffers.RecipientReplacementCompletion{
			CompanyID:                    input.CompanyID,
			InvitationID:                 input.InvitationID,
			IssuedRFQVersionID:           input.IssuedRFQVersionID,
			PreviousRecipientIdentity:    input.PreviousRecipientIdentity,
			ReplacementRecipientIdentity: input.ReplacementRecipientIdentity,
			ReplacementOperationID:       input.ReplacementOperationID,
		}))
}

// AbortRecipientReplacement releases a claim that never became authoritative.
func (a *OfferWorkspaceAdapter) AbortRecipientReplacement(ctx context.Context,
	input rfqissuance.RecipientReplacementAbort) error {

	return mapOfferWorkspaceError(a.offers.AbortRecipientReplacement(ctx,
		supplieroffers.RecipientReplacementAbort{
			CompanyID:                 input.CompanyID,
			InvitationID:              input.InvitationID,
			IssuedRFQVersionID:        input.IssuedRFQVersionID,
			PreviousRecipientIdentity: input.PreviousRecipientIdentity,
			ReplacementOperationID:    input.ReplacementOperationID,
		}))
}

// mapOfferWorkspaceError translates the one sentinel the consumer must act on.
//
// Only a genuine claim conflict becomes ErrOfferWorkspaceConflict. An
// infrastructure failure propagates unchanged: reporting it as a conflict would
// tell the caller the workspace is busy when the claim state is actually
// unknown, and the replacement must not proceed on that basis.
func mapOfferWorkspaceError(err error) error {
	if errors.Is(err, supplieroffers.ErrOfferDraftConflict) {
		return rfqissuance.ErrOfferWorkspaceConflict
	}
	return err
}
