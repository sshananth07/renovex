package awards

import (
	"context"
	"time"
)

// Outcome generation and reads (§8H).
//
// Outcomes are generated automatically as recoverable post-publication work
// (F5 step 9), but are NOT sent until the contractor explicitly acts (F8). An
// award is a decision; telling Suppliers is a separate, deliberate act.

// AwardOutcomeRepository is the narrow outcome surface.
type AwardOutcomeRepository interface {
	EnsureOutcome(
		ctx context.Context,
		candidate AwardOutcome,
	) (AwardOutcome, bool, error)
	FindOutcome(
		ctx context.Context,
		companyID, outcomeID string,
	) (AwardOutcome, bool, error)
	FindOutcomeForSupplier(
		ctx context.Context,
		companyID, outcomeID, supplierID, invitationID string,
	) (AwardOutcome, bool, error)
	ListOutcomes(
		ctx context.Context,
		companyID, awardRevisionID string,
	) ([]AwardOutcome, error)
}

func WithAwardOutcomeRepository(
	repository AwardOutcomeRepository,
) ServiceOption {
	return func(service *Service) { service.outcomes = repository }
}

// generateOutcomesForRevision is F5 step 9, called from completePublication.
//
// It regenerates MISSING outcomes without duplicating existing ones, so a
// crash between publication and generation is repaired by any later completion
// path. Failure here never fails the award: the revision is already
// authoritative, and F10 reconciliation can finish the job.
func (service *Service) generateOutcomesForRevision(
	ctx context.Context,
	companyID, actorUserID string,
	revision AwardRevision,
) {
	if service.outcomes == nil || service.offers == nil {
		return
	}

	// Participants come from the offer versions answering this issued version,
	// not from the revision: a Supplier who won nothing still receives an
	// outcome, and the revision names only winners.
	versions, err := service.offers.ListOfferVersionsForIssuedRFQVersion(
		ctx, companyID, revision.IssuedRFQVersionID)
	if err != nil {
		return
	}

	participants := make([]OutcomeParticipant, 0, len(versions))
	for _, version := range versions {
		// Only the version that is actually in play for its invitation, and
		// only for this issued version.
		if version.IssuedRFQVersionID != revision.IssuedRFQVersionID ||
			!version.IsLatestSubmitted {
			continue
		}
		participants = append(participants, OutcomeParticipant{
			SupplierID:     version.SupplierID,
			SupplierName:   version.SupplierName,
			InvitationID:   version.InvitationID,
			OfferVersionID: version.ID,
			// A Supplier who withdrew before publication removed themselves
			// from consideration and receives no outcome.
			Withdrawn: version.EligibilityState == OfferEligibilityWithdrawn ||
				version.EligibilityState == OfferEligibilityWithdrawalClaimed,
		})
	}

	outcomes, err := GenerateOutcomes(OutcomeGenerationInput{
		Revision:     revision,
		Participants: participants,
		GeneratedAt:  time.Now().UTC(),
	})
	if err != nil {
		return
	}

	for _, outcome := range outcomes {
		stored, _, err := service.outcomes.EnsureOutcome(ctx, outcome)
		if err != nil {
			continue
		}
		// Ensure on both creation and adoption so reconciliation repairs a
		// missing post-publication event. Persistence deduplicates the outcome.
		if service.audit != nil {
			_ = service.audit.RecordAwardOutcomeGenerated(ctx,
				companyID, actorUserID, revision.AwardChainID, revision.ID,
				stored.SupplierID, stored.InvitationID, stored.ID,
				string(stored.Result), time.Now().UTC())
		}
	}
}

// ListOutcomesForRevision is the contractor read.
func (service *Service) ListOutcomesForRevision(
	ctx context.Context,
	companyID, revisionID string,
) ([]AwardOutcome, error) {
	if service.outcomes == nil || service.revisions == nil {
		return nil, ErrAwardsNotConfigured
	}
	// Resolve the revision within the tenant first, so a foreign revision ID
	// yields not-found rather than an empty list that implies it exists.
	if _, found, err := service.revisions.FindRevision(
		ctx, companyID, revisionID); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrAwardRevisionNotFound
	}
	return service.outcomes.ListOutcomes(ctx, companyID, revisionID)
}

// GetSupplierOutcome is the Phase D Supplier read (D3).
//
// Company, Supplier and Invitation all come from the authorized session, never
// from request content, and all three are part of the lookup. Another
// Supplier's outcome is a bounded not-found.
func (service *Service) GetSupplierOutcome(
	ctx context.Context,
	companyID, supplierID, invitationID, outcomeID string,
) (AwardOutcome, error) {
	if service.outcomes == nil {
		return AwardOutcome{}, ErrAwardsNotConfigured
	}
	outcome, found, err := service.outcomes.FindOutcomeForSupplier(
		ctx, companyID, outcomeID, supplierID, invitationID)
	if err != nil {
		return AwardOutcome{}, err
	}
	if !found {
		return AwardOutcome{}, ErrAwardOutcomeNotFound
	}
	return outcome, nil
}
