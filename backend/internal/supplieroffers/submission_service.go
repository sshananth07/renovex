package supplieroffers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// Draft-CAS submission and reconciliation (spec §7).
//
// The active DRAFT, not the Offer Chain, is the submission serialization point,
// so edits, submission and recipient replacement all compete on one document.
// Completion is a seven-step sequence with a recovery entry point at every
// boundary; each step is individually idempotent, so a retry with the same
// operation ID resumes rather than duplicating.

// SetOfferValidityCommand supplies the offer's expiry date. Validity never
// copies forward, so it must be stated explicitly for each submission.
type SetOfferValidityCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
	OfferValidUntil  time.Time
}

// SubmitOfferCommand submits the active draft as an immutable Offer Version.
type SubmitOfferCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
	// OperationID makes the whole submission idempotent across retries.
	OperationID string
}

// SubmissionRepository is the narrow persistence surface submission needs.
type SubmissionRepository interface {
	FindDraft(ctx context.Context, companyID, draftID string) (
		SupplierOfferDraft, bool, error)
	ClaimDraftForSubmission(ctx context.Context, input DraftSubmissionClaimInput) (
		SupplierOfferDraft, error)
	ArchiveSubmittedDraft(ctx context.Context, companyID, draftID,
		submissionOperationID string, archivedAt time.Time) error
}

// SubmissionChainRepository advances the chain pointer.
type SubmissionChainRepository interface {
	FindChain(ctx context.Context, companyID, chainID string) (
		SupplierOfferChain, bool, error)
	AdvanceChainToVersion(ctx context.Context, companyID, chainID string,
		expectedRevision int64, versionID string, versionNumber int,
		updatedAt time.Time) (SupplierOfferChain, error)
}

// SubmissionVersionRepository inserts and reads immutable versions.
type SubmissionVersionRepository interface {
	InsertVersion(ctx context.Context, version SupplierOfferVersion) error
	FindVersion(ctx context.Context, companyID, versionID string) (
		SupplierOfferVersion, bool, error)
}

// SubmissionEligibilityRepository owns the separate mutable eligibility gate.
type SubmissionEligibilityRepository interface {
	InsertEligibility(ctx context.Context, eligibility SupplierOfferEligibility) error
	FindEligibility(ctx context.Context, companyID, offerVersionID string) (
		SupplierOfferEligibility, bool, error)
}

const maxSubmissionFenceAdvanceAttempts = 3

// SetOfferValidity records the offer's expiry through the standard edit CAS.
func (service *Service) SetOfferValidity(
	ctx context.Context,
	command SetOfferValidityCommand,
) (SupplierOfferDraft, error) {
	return service.editDraft(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			validUntil := command.OfferValidUntil
			state.OfferValidUntil = &validUntil
			return state, nil
		})
}

// SubmitOffer validates, claims and completes a submission.
//
// Validation runs BEFORE the claim: claiming a draft for a submission that
// cannot complete would freeze the Supplier's only workspace behind an error
// they can no longer edit their way out of.
func (service *Service) SubmitOffer(
	ctx context.Context,
	command SubmitOfferCommand,
) (SupplierOfferVersion, error) {
	if procurementlimits.ValidateID(command.DraftID) != nil ||
		procurementlimits.ValidateID(command.OperationID) != nil {
		return SupplierOfferVersion{}, ErrInputLimitExceeded
	}
	drafts, chains, versions, eligibility, err := service.submissionCapabilities()
	if err != nil {
		return SupplierOfferVersion{}, err
	}

	authorized, err := service.ResolveMutationContext(ctx, command.Context)
	if err != nil {
		return SupplierOfferVersion{}, err
	}

	draft, found, err := drafts.FindDraft(ctx, authorized.Access.CompanyID, command.DraftID)
	if err != nil {
		return SupplierOfferVersion{}, err
	}
	// Missing, foreign and other-recipient drafts collapse to one not-found.
	if !found ||
		draft.InvitationID != authorized.Access.InvitationID ||
		draft.RecipientIdentity != authorized.Access.RecipientIdentity {
		return SupplierOfferVersion{}, ErrOfferDraftNotFound
	}

	// An already-claimed draft is either OUR retry, which resumes, or another
	// operation's claim, which can never be taken over or reset.
	if draft.Status == DraftSubmitting {
		if draft.SubmissionOperationID != command.OperationID {
			return SupplierOfferVersion{}, ErrOfferDraftConflict
		}
		return service.completeSubmission(ctx, draft, authorized, drafts, chains,
			versions, eligibility)
	}
	// An archived draft whose claim belongs to THIS operation means the
	// submission already completed and the response was lost. Re-driving
	// completion converges on the existing version instead of reporting a
	// conflict for work that actually succeeded.
	if draft.Status == DraftArchived &&
		draft.SubmissionOperationID == command.OperationID &&
		command.OperationID != "" {
		return service.completeSubmission(ctx, draft, authorized, drafts, chains,
			versions, eligibility)
	}
	if draft.Status != DraftActive {
		return SupplierOfferVersion{}, ErrOfferDraftConflict
	}

	calculated, err := CalculateSubmission(SubmissionCalculationInput{
		Draft:       draft,
		RFQ:         authorized.RFQ,
		SubmittedAt: command.Context.AccessedAt,
	})
	if err != nil {
		return SupplierOfferVersion{}, err
	}

	chain, found, err := chains.FindChain(ctx, draft.CompanyID, draft.OfferChainID)
	if err != nil {
		return SupplierOfferVersion{}, err
	}
	if !found {
		return SupplierOfferVersion{}, ErrOfferChainNotFound
	}

	// Candidate identity is reserved once, at claim time. Retries and
	// reconciliation reuse it rather than allocating a second number, which
	// would leave an unidentifiable hole in the chain.
	claimed, err := drafts.ClaimDraftForSubmission(ctx, DraftSubmissionClaimInput{
		CompanyID:                   draft.CompanyID,
		DraftID:                     draft.ID,
		ExpectedRevision:            command.ExpectedRevision,
		SubmissionOperationID:       command.OperationID,
		SubmissionBaseRevision:      draft.Revision,
		SubmissionFingerprint:       calculated.Fingerprint,
		SubmissionRecipientIdentity: draft.RecipientIdentity,
		SubmissionInvitationID:      draft.InvitationID,
		SubmissionRFQVersionID:      draft.IssuedRFQVersionID,
		SubmissionAccessGeneration:  authorized.Access.AccessGeneration,
		CandidateOfferVersionID:     bson.NewObjectID().Hex(),
		CandidateVersionNumber:      chain.LatestSubmittedVersion + 1,
		ClaimedAt:                   command.Context.AccessedAt,
	})
	if err != nil {
		return SupplierOfferVersion{}, err
	}

	return service.completeSubmission(ctx, claimed, authorized, drafts, chains,
		versions, eligibility)
}

// completeSubmission runs the seven-step completion from frozen claimed
// content. It is the same path a crash-recovery retry takes, so every step
// tolerates already having been applied.
func (service *Service) completeSubmission(
	ctx context.Context,
	claimed SupplierOfferDraft,
	authorized AuthorizedSupplierOfferContext,
	drafts SubmissionRepository,
	chains SubmissionChainRepository,
	versions SubmissionVersionRepository,
	eligibility SubmissionEligibilityRepository,
) (SupplierOfferVersion, error) {

	if claimed.CandidateOfferVersionID == "" || claimed.CandidateVersionNumber < 1 {
		// A claim without reserved identity cannot be completed safely: any
		// choice made now could duplicate a number another retry already used.
		return SupplierOfferVersion{}, ErrOfferDraftConflict
	}

	// Step 1-4: insert the immutable version under its RECORDED identity.
	version, exists, err := versions.FindVersion(
		ctx, claimed.CompanyID, claimed.CandidateOfferVersionID)
	if err != nil {
		return SupplierOfferVersion{}, err
	}
	if !exists {
		built, buildErr := service.buildVersionFromClaim(claimed, authorized)
		if buildErr != nil {
			return SupplierOfferVersion{}, buildErr
		}
		if insertErr := versions.InsertVersion(ctx, built); insertErr != nil {
			// A duplicate key means a concurrent retry inserted it first; the
			// read below adopts that winner rather than failing.
			reloaded, reloadedFound, reloadErr := versions.FindVersion(
				ctx, claimed.CompanyID, claimed.CandidateOfferVersionID)
			if reloadErr != nil || !reloadedFound {
				return SupplierOfferVersion{}, insertErr
			}
			built = reloaded
		}
		version = built
	}

	// Step 5: the separate eligibility aggregate starts eligible. It is a
	// distinct document so withdrawal and award never mutate frozen content.
	if _, found, findErr := eligibility.FindEligibility(
		ctx, claimed.CompanyID, version.ID); findErr != nil {
		return SupplierOfferVersion{}, findErr
	} else if !found {
		insertErr := eligibility.InsertEligibility(ctx, SupplierOfferEligibility{
			ID:             bson.NewObjectID().Hex(),
			CompanyID:      claimed.CompanyID,
			OfferChainID:   claimed.OfferChainID,
			OfferVersionID: version.ID,
			State:          EligibilityEligible,
			Revision:       1,
		})
		// A concurrent retry may have created it; the unique index makes that
		// safe and the state is identical either way.
		if insertErr != nil {
			if _, exists, checkErr := eligibility.FindEligibility(
				ctx, claimed.CompanyID, version.ID); checkErr != nil || !exists {
				return SupplierOfferVersion{}, insertErr
			}
		}
	}

	// Step 6: advance the chain. Only after this does the version become the
	// Supplier's authoritative latest offer.
	if advanceErr := advanceSubmissionChainAfterFence(ctx, chains, version); advanceErr != nil {
		return SupplierOfferVersion{}, advanceErr
	}

	// Step 7: archive the exact claimed draft, releasing the workspace slot
	// only once the chain already points at the immutable version.
	//
	if archiveErr := drafts.ArchiveSubmittedDraft(ctx, claimed.CompanyID,
		claimed.ID, claimed.SubmissionOperationID, version.SubmittedAt); archiveErr != nil {
		return SupplierOfferVersion{}, archiveErr
	}

	// The concrete recorder ensures this identity once. Calling it on every
	// completion path repairs a crash after archival but before audit without
	// manufacturing a duplicate on ordinary replay.
	if service.audit != nil {
		_ = service.audit.RecordOfferSubmitted(ctx,
			version.CompanyID, authorized.Access.SupplierID, version.InvitationID,
			version.OfferChainID, claimed.ID, version.ID, version.VersionNumber,
			version.SubmittedAt)
	}

	return version, nil
}

// advanceSubmissionChainAfterFence absorbs only the narrow stale-revision case
// introduced by G5's withdrawal fence. It rereads the chain and retries when
// the candidate is still exactly the next version; a genuinely competing or
// already-newer submission remains a terminal conflict.
func advanceSubmissionChainAfterFence(
	ctx context.Context,
	chains SubmissionChainRepository,
	version SupplierOfferVersion,
) error {
	for attempt := 0; attempt < maxSubmissionFenceAdvanceAttempts; attempt++ {
		chain, found, err := chains.FindChain(ctx, version.CompanyID, version.OfferChainID)
		if err != nil {
			return err
		}
		if !found {
			return ErrOfferChainNotFound
		}
		if chain.LatestSubmittedID != nil && *chain.LatestSubmittedID == version.ID {
			return nil
		}
		if version.VersionNumber != chain.LatestSubmittedVersion+1 {
			return ErrOfferDraftConflict
		}
		_, err = chains.AdvanceChainToVersion(ctx, version.CompanyID,
			version.OfferChainID, chain.Revision, version.ID, version.VersionNumber,
			version.SubmittedAt)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrOfferDraftConflict) {
			return err
		}
	}
	return ErrOfferDraftConflict
}

// buildVersionFromClaim recalculates from FROZEN claimed content and verifies
// the result still matches the fingerprint recorded at claim time.
//
// Rebuilding rather than trusting stored amounts keeps one calculation path;
// re-checking the fingerprint guarantees recovery can never store content that
// differs from what was validated and claimed.
func (service *Service) buildVersionFromClaim(
	claimed SupplierOfferDraft,
	authorized AuthorizedSupplierOfferContext,
) (SupplierOfferVersion, error) {

	content := claimed
	// The claim incremented the revision; the fingerprint was taken over the
	// content at SubmissionBaseRevision.
	content.Revision = claimed.SubmissionBaseRevision

	calculated, err := CalculateSubmission(SubmissionCalculationInput{
		Draft:       content,
		RFQ:         authorized.RFQ,
		SubmittedAt: derivedSubmissionTime(claimed),
	})
	if err != nil {
		return SupplierOfferVersion{}, err
	}
	if claimed.SubmissionFingerprint != "" &&
		calculated.Fingerprint != claimed.SubmissionFingerprint {
		// Frozen content no longer reproduces its claimed identity. Storing it
		// would silently publish something other than what was validated.
		return SupplierOfferVersion{}, ErrOfferDraftConflict
	}

	return SupplierOfferVersion{
		ID:                    claimed.CandidateOfferVersionID,
		CompanyID:             claimed.CompanyID,
		OfferChainID:          claimed.OfferChainID,
		InvitationID:          claimed.SubmissionInvitationID,
		IssuedRFQVersionID:    claimed.SubmissionRFQVersionID,
		VersionNumber:         claimed.CandidateVersionNumber,
		Currency:              claimed.Currency,
		RecipientIdentity:     claimed.SubmissionRecipientIdentity,
		SourceDraftID:         claimed.ID,
		SourceDraftRevision:   claimed.SubmissionBaseRevision,
		SubmissionOperationID: claimed.SubmissionOperationID,
		SubmissionFingerprint: calculated.Fingerprint,

		Lines:          calculated.Lines,
		Tax:            claimed.Tax,
		ChargeGroups:   immutableChargeGroups(claimed.ChargeGroups),
		DeliveryCharge: claimed.DeliveryCharge,

		QuotedLineSubtotal:   calculated.QuotedLineSubtotal,
		QuotedTaxTotal:       calculated.QuotedTaxTotal,
		FullOfferChargeTotal: calculated.FullOfferChargeTotal,
		DeliveryChargeTotal:  calculated.DeliveryChargeTotal,
		GrandTotal:           calculated.GrandTotal,

		OfferValidUntil: derefTime(claimed.OfferValidUntil),
		SupplierNotes:   claimed.SupplierNotes,
		SubmittedAt:     derivedSubmissionTime(claimed),
		SchemaVersion:   SupplierOfferVersionSchemaVersion,
	}, nil
}

// derivedSubmissionTime uses the claim instant so a retry reproduces the same
// submission time rather than drifting forward on each attempt.
func derivedSubmissionTime(claimed SupplierOfferDraft) time.Time {
	if claimed.SubmissionClaimedAt != nil {
		return *claimed.SubmissionClaimedAt
	}
	return claimed.UpdatedAt
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

// immutableChargeGroups strips draft-only review bookkeeping: an immutable
// version records the commercial rule, not the editing state that produced it.
func immutableChargeGroups(
	groups []SupplierChargeGroupDraft,
) []ConditionalChargeGroup {
	immutable := make([]ConditionalChargeGroup, 0, len(groups))
	for _, group := range groups {
		immutable = append(immutable, group.ConditionalChargeGroup)
	}
	return immutable
}

func (service *Service) submissionCapabilities() (
	SubmissionRepository,
	SubmissionChainRepository,
	SubmissionVersionRepository,
	SubmissionEligibilityRepository,
	error,
) {
	drafts, draftsOK := service.drafts.(SubmissionRepository)
	chains, chainsOK := service.chains.(SubmissionChainRepository)
	versions, versionsOK := service.versions.(SubmissionVersionRepository)
	eligibility := service.eligibility
	if !draftsOK || !chainsOK || !versionsOK || eligibility == nil {
		return nil, nil, nil, nil, ErrSupplierOffersNotConfigured
	}
	return drafts, chains, versions, eligibility, nil
}
