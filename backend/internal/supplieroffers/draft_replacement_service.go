package supplieroffers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// This file implements the consumer-owned recipient-replacement capability that
// rfqissuance declares (spec §5.3A). rfqissuance never touches a supplieroffers
// collection; it calls these three operations through a composition adapter,
// and every argument here is a primitive.

// RecipientReplacementPreparation asks for the durable replacement barrier.
type RecipientReplacementPreparation struct {
	CompanyID                  string
	InvitationID               string
	IssuedRFQVersionID         string
	PreviousRecipientIdentity  string
	CandidateRecipientIdentity string
	ReplacementOperationID     string
}

// RecipientReplacementCompletion archives the claim after the authoritative
// invitation replacement is confirmed.
type RecipientReplacementCompletion struct {
	CompanyID                    string
	InvitationID                 string
	IssuedRFQVersionID           string
	PreviousRecipientIdentity    string
	ReplacementRecipientIdentity string
	ReplacementOperationID       string
}

// RecipientReplacementAbort releases a claim that provably never became an
// authoritative replacement.
type RecipientReplacementAbort struct {
	CompanyID                 string
	InvitationID              string
	IssuedRFQVersionID        string
	PreviousRecipientIdentity string
	ReplacementOperationID    string
}

// RecipientReplacementRepository is the narrow persistence surface these three
// operations need. Keeping it separate from the drafting repository interface
// prevents the replacement path from becoming a generic escape hatch into the
// draft collection.
type RecipientReplacementRepository interface {
	ClaimRecipientReplacement(
		ctx context.Context,
		input RecipientReplacementClaimInput,
	) (SupplierOfferDraft, error)
	ArchiveClaimedRecipientReplacement(
		ctx context.Context,
		input RecipientReplacementArchivalInput,
	) (SupplierOfferDraft, error)
	ReleaseClaimedRecipientReplacement(
		ctx context.Context,
		input RecipientReplacementAbortInput,
	) error
	FindUnfinishedDraftForInvitation(
		ctx context.Context,
		companyID string,
		invitationID string,
		issuedRFQVersionID string,
	) (SupplierOfferDraft, bool, error)
}

// PrepareRecipientReplacement takes the durable barrier.
//
// This must succeed BEFORE rfqissuance performs the authoritative invitation
// write. The claim keeps occupying the unfinished-draft uniqueness slot, so the
// previous recipient cannot edit, submit or open a new draft during the
// cross-collection window. Because both the existing-draft CAS and the
// no-draft insert contend on one named unique index, a concurrent draft
// creation and this claim can never both succeed.
func (service *Service) PrepareRecipientReplacement(
	ctx context.Context,
	input RecipientReplacementPreparation,
) error {
	repository, err := service.replacementRepository()
	if err != nil {
		return err
	}

	chain, err := service.chains.EnsureOfferChain(
		ctx, input.CompanyID, input.InvitationID, input.IssuedRFQVersionID)
	if err != nil {
		return err
	}

	_, err = repository.ClaimRecipientReplacement(ctx, RecipientReplacementClaimInput{
		CompanyID:                  input.CompanyID,
		OfferChainID:               chain.ID,
		InvitationID:               input.InvitationID,
		IssuedRFQVersionID:         input.IssuedRFQVersionID,
		PreviousRecipientIdentity:  input.PreviousRecipientIdentity,
		CandidateRecipientIdentity: input.CandidateRecipientIdentity,
		ReplacementOperationID:     input.ReplacementOperationID,
		BarrierDraftID:             newReplacementBarrierID(),
		ClaimedAt:                  time.Now().UTC(),
	})
	return err
}

// CompleteRecipientReplacement archives the held claim.
//
// It runs only after the authoritative invitation replacement is confirmed,
// because archiving releases the uniqueness slot. Releasing it any earlier
// would let the previous recipient open a fresh draft while they were still
// the authoritative recipient.
//
// The replacement recipient does NOT inherit the archived draft: they start
// blank or explicitly copy a compatible immutable Offer Version.
func (service *Service) CompleteRecipientReplacement(
	ctx context.Context,
	input RecipientReplacementCompletion,
) error {
	repository, err := service.replacementRepository()
	if err != nil {
		return err
	}

	_, err = repository.ArchiveClaimedRecipientReplacement(ctx,
		RecipientReplacementArchivalInput{
			CompanyID:                    input.CompanyID,
			InvitationID:                 input.InvitationID,
			IssuedRFQVersionID:           input.IssuedRFQVersionID,
			PreviousRecipientIdentity:    input.PreviousRecipientIdentity,
			ReplacementRecipientIdentity: input.ReplacementRecipientIdentity,
			ReplacementOperationID:       input.ReplacementOperationID,
			ArchivedAt:                   time.Now().UTC(),
		})
	if err == nil {
		return nil
	}

	// Recovery: a repeat completion after a lost response finds nothing left to
	// archive. That converges only when THIS operation already archived it —
	// otherwise the conflict stands, so one replacement cannot mask another.
	if errorIsOfferDraftConflict(err) &&
		service.alreadyArchivedBy(ctx, repository, input) {
		return nil
	}
	return err
}

// AbortRecipientReplacement releases a claim.
//
// Abort is deliberately restrictive: only the exact owning operation may
// release its own barrier, and claims never auto-release on a timeout. An
// infrastructure failure does not prove the invitation write failed, so
// reconciliation must complete the replacement whenever anything is uncertain
// rather than hand access back to a replaced recipient.
func (service *Service) AbortRecipientReplacement(
	ctx context.Context,
	input RecipientReplacementAbort,
) error {
	repository, err := service.replacementRepository()
	if err != nil {
		return err
	}

	return repository.ReleaseClaimedRecipientReplacement(ctx,
		RecipientReplacementAbortInput{
			CompanyID:                 input.CompanyID,
			InvitationID:              input.InvitationID,
			IssuedRFQVersionID:        input.IssuedRFQVersionID,
			PreviousRecipientIdentity: input.PreviousRecipientIdentity,
			ReplacementOperationID:    input.ReplacementOperationID,
			AbortedAt:                 time.Now().UTC(),
		})
}

// alreadyArchivedBy reports whether this exact operation already completed the
// archival, which is what makes a repeat completion idempotent.
func (service *Service) alreadyArchivedBy(
	ctx context.Context,
	repository RecipientReplacementRepository,
	input RecipientReplacementCompletion,
) bool {
	_, stillHeld, err := repository.FindUnfinishedDraftForInvitation(
		ctx, input.CompanyID, input.InvitationID, input.IssuedRFQVersionID)
	if err != nil || stillHeld {
		return false
	}
	return true
}

func (service *Service) replacementRepository() (
	RecipientReplacementRepository, error) {
	repository, ok := service.drafts.(RecipientReplacementRepository)
	if !ok || service.chains == nil {
		return nil, ErrSupplierOffersNotConfigured
	}
	return repository, nil
}

func errorIsOfferDraftConflict(err error) bool {
	return errors.Is(err, ErrOfferDraftConflict)
}

// newReplacementBarrierID mints the candidate ID for a barrier-only row. A
// losing candidate never escapes: the claim always reads back MongoDB's winner.
func newReplacementBarrierID() string {
	return bson.NewObjectID().Hex()
}
