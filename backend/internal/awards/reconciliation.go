package awards

import (
	"context"
	"time"
)

// Award reconciliation (§8K).
//
// `awards` owns award reconciliation. It never repairs another module's state
// directly: offer gates move only through the Phase E capability.
//
// Reconciliation NEVER invents a selection, a total, a reason, a revision
// number, a candidate identity or a commercial record; never rolls the chain
// backward; and never releases a claim once a matching revision exists.

type ReconcileAwardInput struct {
	IssuedRFQVersionID string
	OperationID        string
	ReconciledAt       time.Time
}

// ReconcileAwardResult reports what reconciliation actually did, so a caller
// can tell "completed a stalled publication" from "released an abandoned one".
type ReconcileAwardResult struct {
	AwardChainID     string
	AwardRevisionID  string
	CompletedForward bool
	ReleasedClaims   bool
	AlreadyComplete  bool
}

// ReconcileAward repairs one Award chain.
//
// The decision turns on ONE question: does a matching immutable Award Revision
// exist? If it does, the award is authoritative and the only permitted action
// is to complete forward (D2). If it does not, an abandoned operation's own
// claims may be released and the chain returned to its prior state.
func (service *Service) ReconcileAward(
	ctx context.Context,
	companyID, actorUserID string,
	input ReconcileAwardInput,
) (ReconcileAwardResult, error) {
	if service.chains == nil || service.revisions == nil ||
		service.lineClaims == nil || service.eligibility == nil {
		return ReconcileAwardResult{}, ErrAwardsNotConfigured
	}

	chain, found, err := service.chains.FindChainByIssuedVersion(
		ctx, companyID, input.IssuedRFQVersionID)
	if err != nil {
		return ReconcileAwardResult{}, err
	}
	if !found {
		// Reconciliation never invents a chain or a revision.
		return ReconcileAwardResult{}, ErrAwardChainNotFound
	}

	reconciledAt := input.ReconciledAt
	if reconciledAt.IsZero() {
		reconciledAt = time.Now().UTC()
	}

	// Resolve the authoritative revision from the IMMUTABLE revisions, not from
	// the chain pointer, which is exactly what may be stale here.
	revision, hasRevision, err := service.revisions.FindLatestRevision(
		ctx, companyID, chain.ID)
	if err != nil {
		return ReconcileAwardResult{}, err
	}

	result := ReconcileAwardResult{AwardChainID: chain.ID}

	if hasRevision {
		result.AwardRevisionID = revision.ID

		// Already complete: return idempotently rather than re-running writes
		// that would only churn revisions.
		alreadyPublished := chain.FinalisationState == FinalisationPublished &&
			chain.CurrentAwardRevisionID != nil &&
			*chain.CurrentAwardRevisionID == revision.ID
		if alreadyPublished {
			result.AlreadyComplete = true
			// Audit is still ENSURED: a crash between insert and audit leaves
			// an authoritative award unaudited, and only a repair like this
			// closes that gap. The sink no-ops when the event already exists.
			service.ensureFinalisationAudit(
				ctx, companyID, actorUserID, revision)
			service.generateOutcomesForRevision(
				ctx, companyID, actorUserID, revision)
			return result, nil
		}

		// Complete forward. Claims are NEVER released here: a matching revision
		// exists, so releasing would let a competitor be awarded a line whose
		// Supplier may already have been told they won.
		if _, err := service.chainPublisher().PublishRevision(ctx,
			PublishRevisionInput{
				CompanyID: companyID, AwardChainID: chain.ID,
				ExpectedRevision: chain.Revision,
				OperationID:      chain.FinalisingOperationID,
				AwardRevisionID:  revision.ID,
				RevisionNumber:   revision.RevisionNumber,
			}); err != nil {
			return ReconcileAwardResult{}, err
		}

		// Mark both claim tiers awarded from what the revision itself records,
		// so nothing is invented.
		if err := service.completeClaimsFromRevision(
			ctx, companyID, revision, reconciledAt); err != nil {
			return ReconcileAwardResult{}, err
		}

		service.generateOutcomesForRevision(ctx, companyID, actorUserID, revision)
		service.ensureFinalisationAudit(ctx, companyID, actorUserID, revision)

		result.CompletedForward = true
		return result, nil
	}

	// No revision exists. Only a chain stuck in `finalising` needs repair.
	if chain.FinalisationState != FinalisationFinalising {
		result.AlreadyComplete = true
		return result, nil
	}

	// Release ONLY the abandoned operation's own claims. ListClaimsForOperation
	// is scoped to that operation, so a live competitor's claims are never
	// visible to this path, let alone released.
	claims, err := service.lineClaims.ListClaimsForOperation(
		ctx, companyID, chain.FinalisingOperationID)
	if err != nil {
		return ReconcileAwardResult{}, err
	}
	for _, claim := range claims {
		// An `awarded` claim is terminal and is skipped by the repository's own
		// filter; this guard states the rule at the call site too.
		if claim.State != LineClaimClaimed {
			continue
		}
		if err := service.lineClaims.ReleaseLineageClaim(ctx, companyID,
			claim.ID, chain.FinalisingOperationID, claim.Revision); err != nil {
			return ReconcileAwardResult{}, err
		}
		result.ReleasedClaims = true
	}

	// Return the chain to its prior state so the contractor may re-decide. A
	// chain that already holds a published award is restored to `published`,
	// never to `draft`, which would orphan that award's pointer.
	if err := service.chainPublisher().AbandonFinalisation(ctx, companyID,
		chain.ID, chain.FinalisingOperationID, chain.Revision); err != nil {
		return ReconcileAwardResult{}, err
	}
	return result, nil
}

// completeClaimsFromRevision marks both tiers awarded using only what the
// immutable revision records — never a recomputed selection.
func (service *Service) completeClaimsFromRevision(
	ctx context.Context,
	companyID string,
	revision AwardRevision,
	completedAt time.Time,
) error {
	for _, line := range revision.AwardedLines {
		claim, found, err := service.lineClaims.FindClaimByLineage(
			ctx, companyID, revision.RFQChainID, line.StableLineageID)
		if err != nil {
			return err
		}
		// A missing claim is not re-created: reconciliation never invents a
		// claim, and the revision is authoritative regardless.
		if !found || claim.State == LineClaimAwarded {
			continue
		}
		if err := service.lineClaims.MarkLineageAwarded(ctx, companyID,
			claim.ID, claim.AwardOperationID, revision.ID,
			claim.Revision); err != nil {
			return err
		}
	}

	for _, summary := range revision.SupplierSummaries {
		gate, found, err := service.eligibility.GetOfferEligibility(
			ctx, companyID, summary.OfferVersionID)
		if err != nil {
			return err
		}
		if !found || gate.State == OfferEligibilityAwarded {
			continue
		}
		if _, err := service.eligibility.CompleteOfferAward(ctx,
			OfferEligibilityCompletionRequest{
				CompanyID:        companyID,
				OfferVersionID:   summary.OfferVersionID,
				OperationID:      gate.OperationID,
				ExpectedRevision: gate.Revision,
				CompletedAt:      completedAt,
			}); err != nil {
			return err
		}
	}
	return nil
}

// chainPublisher narrows the chain repository to its publication transitions.
//
// It returns a no-op publisher rather than panicking when the repository does
// not implement them, so a misconfigured service fails closed on a bounded
// error instead of crashing the process.
func (service *Service) chainPublisher() AwardChainPublisher {
	if publisher, ok := service.chains.(AwardChainPublisher); ok {
		return publisher
	}
	return unconfiguredChainPublisher{}
}

type unconfiguredChainPublisher struct{}

func (unconfiguredChainPublisher) ClaimFinalisation(
	context.Context, FinalisationClaimInput,
) (AwardDecisionChain, error) {
	return AwardDecisionChain{}, ErrAwardsNotConfigured
}

func (unconfiguredChainPublisher) PublishRevision(
	context.Context, PublishRevisionInput,
) (AwardDecisionChain, error) {
	return AwardDecisionChain{}, ErrAwardsNotConfigured
}

func (unconfiguredChainPublisher) AbandonFinalisation(
	context.Context, string, string, string, int64,
) error {
	return ErrAwardsNotConfigured
}
