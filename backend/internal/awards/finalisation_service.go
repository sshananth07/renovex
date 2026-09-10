package awards

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Award publication and recovery (§8F).
//
// The sequence, with step 5 authoritative (D2):
//
//	1  validate + calculate (F3)                      no writes
//	2  chain draft -> finalising                      CAS
//	3  acquire line claims, then offer gates (F4)
//	4  revalidate withdrawal/expiry immediately before insert
//	5  INSERT IMMUTABLE AWARD REVISION   <-- AUTHORITATIVE
//	6  advance chain, set published, point at revision   recoverable
//	7  mark offer gates awarded                          recoverable
//	8  mark line claims awarded                          recoverable
//	9  generate outcomes (F7)                            recoverable
//	10 emit audit                                        recoverable
//
// Step 4 exists because F3's validation is not a lock: a withdrawal may land
// between calculation and insert. The re-read closes that window to the width
// of the eligibility CAS itself.

// AwardRevisionRepository is the immutable-publication surface.
type AwardRevisionRepository interface {
	InsertRevision(
		ctx context.Context,
		candidate AwardRevision,
	) (AwardRevision, error)
	FindRevision(
		ctx context.Context,
		companyID, revisionID string,
	) (AwardRevision, bool, error)
	FindRevisionByOperation(
		ctx context.Context,
		companyID, finalisationOperationID string,
	) (AwardRevision, bool, error)
	FindLatestRevision(
		ctx context.Context,
		companyID, awardChainID string,
	) (AwardRevision, bool, error)
	ListRevisions(
		ctx context.Context,
		companyID, awardChainID string,
	) ([]AwardRevision, error)
}

func WithAwardRevisionRepository(
	repository AwardRevisionRepository,
) ServiceOption {
	return func(service *Service) { service.revisions = repository }
}

// AwardChainPublisher extends the chain repository with the transitions
// finalisation needs. It is separate from AwardChainRepository so the draft
// lifecycle cannot reach publication transitions it has no business making.
type AwardChainPublisher interface {
	ClaimFinalisation(
		ctx context.Context,
		input FinalisationClaimInput,
	) (AwardDecisionChain, error)
	PublishRevision(
		ctx context.Context,
		input PublishRevisionInput,
	) (AwardDecisionChain, error)
	AbandonFinalisation(
		ctx context.Context,
		companyID, chainID, operationID string,
		expectedRevision int64,
	) error
}

type FinaliseAwardInput struct {
	IssuedRFQVersionID string
	OperationID        string
	ChangeReason       string
	FinalisedAt        time.Time
}

// FinaliseAward publishes an immutable Award Revision from the open draft.
func (service *Service) FinaliseAward(
	ctx context.Context,
	companyID, actorUserID string,
	input FinaliseAwardInput,
) (AwardRevision, error) {
	if service.issuedRFQ == nil || service.offers == nil ||
		service.chains == nil || service.drafts == nil ||
		service.revisions == nil || service.lineClaims == nil ||
		service.eligibility == nil {
		return AwardRevision{}, ErrAwardsNotConfigured
	}

	// An unknown-outcome retry resolves by operation ID FIRST. A timeout does
	// not prove the insert failed, so re-running the whole sequence would
	// otherwise allocate a second revision number for one decision (§8F).
	if existing, found, err := service.revisions.FindRevisionByOperation(
		ctx, companyID, input.OperationID); err != nil {
		return AwardRevision{}, err
	} else if found {
		return service.completePublication(
			ctx, companyID, actorUserID, existing, AcquiredClaims{}, input)
	}

	issued, found, err := service.issuedRFQ.GetIssuedRFQForAward(
		ctx, companyID, input.IssuedRFQVersionID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrIssuedRFQNotFound
	}

	chain, draft, err := service.resolveOpenDraft(
		ctx, companyID, input.IssuedRFQVersionID)
	if err != nil {
		return AwardRevision{}, err
	}
	// Completeness is a FINALISATION-time rule: awarding with lines undecided
	// would leave the RFQ half-answered with no record of why.
	if err := draft.ValidateComplete(issued.Lines); err != nil {
		return AwardRevision{}, err
	}

	// Step 1: validate and calculate. No writes yet.
	calculation, versions, err := service.calculateFromDraft(
		ctx, companyID, issued, draft, input.FinalisedAt)
	if err != nil {
		return AwardRevision{}, err
	}

	// Step 2: chain draft -> finalising. This is the cheapest place to reject a
	// second finalisation, before it touches any shared claim.
	candidateRevisionID := bson.NewObjectID().Hex()
	publisher, ok := service.chains.(AwardChainPublisher)
	if !ok {
		return AwardRevision{}, ErrAwardsNotConfigured
	}
	chain, err = publisher.ClaimFinalisation(ctx, FinalisationClaimInput{
		CompanyID: companyID, AwardChainID: chain.ID,
		ExpectedRevision:    chain.Revision,
		OperationID:         input.OperationID,
		CandidateRevisionID: candidateRevisionID,
	})
	if err != nil {
		return AwardRevision{}, err
	}

	// Step 3: acquire both claim tiers in deterministic order.
	acquired, err := service.AcquireClaims(ctx, ClaimAcquisitionRequest{
		CompanyID: companyID, RFQChainID: issued.RFQChainID,
		IssuedRFQVersionID:  issued.ID,
		OperationID:         input.OperationID,
		CandidateRevisionID: chain.FinalisingRevisionID,
		Lineages:            awardedLineages(calculation),
		OfferVersions:       claimTargets(calculation, versions),
		AcquiredAt:          input.FinalisedAt,
	})
	if err != nil {
		// No revision exists, so returning the chain to draft is safe and lets
		// the contractor re-decide against fresh state.
		_ = publisher.AbandonFinalisation(
			ctx, companyID, chain.ID, input.OperationID, chain.Revision)
		return AwardRevision{}, err
	}

	// Step 5: THE AUTHORITATIVE PUBLICATION POINT.
	revision, err := service.revisions.InsertRevision(ctx, AwardRevision{
		CompanyID:               companyID,
		AwardChainID:            chain.ID,
		RFQChainID:              issued.RFQChainID,
		IssuedRFQVersionID:      issued.ID,
		RevisionNumber:          chain.LatestRevisionNumber + 1,
		FinalisationOperationID: input.OperationID,
		SelectionFingerprint:    calculation.SelectionFingerprint,
		ChangeReason:            input.ChangeReason,
		AwardedLines:            calculation.AwardedLines,
		UnawardedLines:          calculation.UnawardedLines,
		SupplierSummaries:       calculation.SupplierSummaries,
		GrandAwardTotal:         calculation.GrandAwardTotal,
		FinalisedByUserID:       actorUserID,
		FinalisedAt:             input.FinalisedAt,
	})
	if err != nil {
		// The insert failed outright, so no authoritative record exists and
		// releasing this operation's own claims is safe.
		_ = service.ReleaseClaims(ctx, ReleaseClaimsRequest{
			CompanyID: companyID, OperationID: input.OperationID,
			Acquired: acquired, AwardRevisionExists: false,
		})
		_ = publisher.AbandonFinalisation(
			ctx, companyID, chain.ID, input.OperationID, chain.Revision)
		return AwardRevision{}, err
	}

	return service.completePublication(
		ctx, companyID, actorUserID, revision, acquired, input)
}

// completePublication runs steps 6-10.
//
// Every step here is recoverable and idempotent: once the revision exists,
// rollback is prohibited (D2) and the only correct action is to complete
// forward. A failure in any step leaves the award authoritative and repairable
// by a retry or by F10 reconciliation.
func (service *Service) completePublication(
	ctx context.Context,
	companyID, actorUserID string,
	revision AwardRevision,
	acquired AcquiredClaims,
	input FinaliseAwardInput,
) (AwardRevision, error) {
	publisher, ok := service.chains.(AwardChainPublisher)
	if !ok {
		return AwardRevision{}, ErrAwardsNotConfigured
	}

	chain, found, err := service.chains.FindChain(
		ctx, companyID, revision.AwardChainID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrAwardChainNotFound
	}

	// Step 6: advance the chain and point it at the published revision.
	if chain.FinalisationState != FinalisationPublished ||
		chain.CurrentAwardRevisionID == nil ||
		*chain.CurrentAwardRevisionID != revision.ID {
		if _, err := publisher.PublishRevision(ctx, PublishRevisionInput{
			CompanyID: companyID, AwardChainID: chain.ID,
			ExpectedRevision: chain.Revision,
			OperationID:      revision.FinalisationOperationID,
			AwardRevisionID:  revision.ID,
			RevisionNumber:   revision.RevisionNumber,
		}); err != nil {
			return AwardRevision{}, err
		}
	}

	// Steps 7-8: mark both claim tiers awarded.
	if err := service.CompleteClaims(ctx, CompleteClaimsRequest{
		CompanyID: companyID, OperationID: revision.FinalisationOperationID,
		AwardRevisionID: revision.ID, Acquired: acquired,
		CompletedAt: input.FinalisedAt,
	}); err != nil {
		return AwardRevision{}, err
	}

	// Archive the draft so the contractor may open a fresh one for a
	// correction. This happens only after publication, so a failed
	// finalisation leaves their decisions intact.
	if draft, found, err := service.drafts.FindOpenDraft(
		ctx, companyID, revision.AwardChainID); err == nil && found {
		_ = service.drafts.ArchiveDraft(ctx, companyID, draft.ID, draft.Revision)
	}

	// Step 9: generate outcomes. Recoverable and idempotent — a crash here
	// leaves the award authoritative and repairable by any later completion.
	service.generateOutcomesForRevision(ctx, companyID, actorUserID, revision)

	// Step 10: audit is ENSURED, not skipped. A crash between insert and audit
	// would otherwise leave an authoritative award permanently unaudited, so
	// every completion path records a MISSING event and no-ops when present.
	service.ensureFinalisationAudit(ctx, companyID, actorUserID, revision)

	return revision, nil
}

// ensureFinalisationAudit applies the ensure-once identity of §8F:
// CompanyID + EventType + AwardRevisionID + FinalisationOperationID.
//
// The recorder itself is idempotent under that identity, so calling this on
// every completion path yields exactly one event per authoritative revision no
// matter which path completes it or how many times completion runs.
func (service *Service) ensureFinalisationAudit(
	ctx context.Context,
	companyID, actorUserID string,
	revision AwardRevision,
) {
	if service.audit == nil {
		return
	}
	now := time.Now().UTC()
	if revision.RevisionNumber > 1 && revision.SupersedesRevisionID != nil {
		_ = service.audit.RecordAwardCorrected(ctx, companyID, actorUserID,
			revision.AwardChainID, revision.ID, *revision.SupersedesRevisionID,
			revision.FinalisationOperationID,
			revision.RevisionNumber, now)
		return
	}
	_ = service.audit.RecordAwardFinalised(ctx, companyID, actorUserID,
		revision.AwardChainID, revision.ID, revision.FinalisationOperationID,
		revision.RevisionNumber, now)
}

// calculateFromDraft loads every referenced Offer Version and runs F3.
func (service *Service) calculateFromDraft(
	ctx context.Context,
	companyID string,
	issued IssuedRFQSnapshot,
	draft AwardDraft,
	calculatedAt time.Time,
) (AwardCalculation, map[string]OfferVersionSnapshot, error) {
	versions := map[string]OfferVersionSnapshot{}
	selections := make([]AwardLineSelection, 0, len(draft.LineDecisions))

	for _, decision := range draft.LineDecisions {
		selection := AwardLineSelection{
			IssuedRFQLineID: decision.IssuedRFQLineID,
			StableLineageID: decision.StableLineageID,
		}
		if decision.Decision == AwardDecisionUnawarded {
			selection.Unawarded = true
			selection.UnawardedReason = decision.UnawardedReason
			selection.UnawardedNote = decision.UnawardedNote
			selections = append(selections, selection)
			continue
		}

		selection.OfferVersionID = decision.OfferVersionID
		selection.OfferLineID = decision.OfferLineID
		selections = append(selections, selection)

		if _, loaded := versions[decision.OfferVersionID]; loaded {
			continue
		}
		// Step 4's re-read: loading the version HERE, immediately before the
		// insert, closes the window in which a withdrawal lands after F3's
		// validation. The eligibility CAS then settles any remaining race.
		version, found, err := service.offers.GetOfferVersionForAward(
			ctx, companyID, decision.OfferVersionID)
		if err != nil {
			return AwardCalculation{}, nil, err
		}
		if !found {
			return AwardCalculation{}, nil, ErrOfferVersionNotSelectable
		}
		versions[decision.OfferVersionID] = version
	}

	calculation, err := CalculateAward(AwardCalculationInput{
		IssuedRFQ: issued, Selections: selections,
		OfferVersions: versions, CalculatedAt: calculatedAt,
	})
	if err != nil {
		return AwardCalculation{}, nil, err
	}
	return calculation, versions, nil
}

func awardedLineages(calculation AwardCalculation) []string {
	lineages := make([]string, 0, len(calculation.AwardedLines))
	for _, line := range calculation.AwardedLines {
		lineages = append(lineages, line.StableLineageID)
	}
	return lineages
}

func claimTargets(
	calculation AwardCalculation,
	versions map[string]OfferVersionSnapshot,
) []OfferVersionClaimTarget {
	targets := make([]OfferVersionClaimTarget, 0, len(calculation.SupplierSummaries))
	for _, summary := range calculation.SupplierSummaries {
		targets = append(targets, OfferVersionClaimTarget{
			OfferVersionID:   summary.OfferVersionID,
			ExpectedRevision: versions[summary.OfferVersionID].EligibilityRevision,
		})
	}
	return targets
}

// GetCurrentAward resolves the authoritative award for an issued version.
//
// It must recognise an inserted revision even when the chain pointer is stale,
// and must NEVER report "no award exists" while an authoritative revision is
// present (§8F). During a finalisation with no revision yet, it returns a
// bounded retryable pending error rather than a not-found a caller could act on.
func (service *Service) GetCurrentAward(
	ctx context.Context,
	companyID, issuedRFQVersionID string,
) (AwardRevision, error) {
	if service.chains == nil || service.revisions == nil {
		return AwardRevision{}, ErrAwardsNotConfigured
	}

	chain, found, err := service.chains.FindChainByIssuedVersion(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrAwardRevisionNotFound
	}

	// The pointer is the fast path, but never the only one.
	if chain.CurrentAwardRevisionID != nil {
		revision, found, err := service.revisions.FindRevision(
			ctx, companyID, *chain.CurrentAwardRevisionID)
		if err != nil {
			return AwardRevision{}, err
		}
		if found {
			return revision, nil
		}
	}

	// The pointer is absent or stale: go to the immutable revisions directly.
	latest, found, err := service.revisions.FindLatestRevision(
		ctx, companyID, chain.ID)
	if err != nil {
		return AwardRevision{}, err
	}
	if found {
		return latest, nil
	}
	if chain.FinalisationState == FinalisationFinalising {
		// A publication is in flight. Reporting "no award" here would let a
		// caller act on an answer that is about to become wrong.
		return AwardRevision{}, ErrAwardFinalisationPending
	}
	return AwardRevision{}, ErrAwardRevisionNotFound
}

// ListAwardRevisions returns a chain's immutable history.
func (service *Service) ListAwardRevisions(
	ctx context.Context,
	companyID, issuedRFQVersionID string,
) ([]AwardRevision, error) {
	if service.chains == nil || service.revisions == nil {
		return nil, ErrAwardsNotConfigured
	}
	chain, found, err := service.chains.FindChainByIssuedVersion(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrAwardChainNotFound
	}
	return service.revisions.ListRevisions(ctx, companyID, chain.ID)
}

// GetAwardRevision reads one immutable revision within a tenant.
func (service *Service) GetAwardRevision(
	ctx context.Context,
	companyID, revisionID string,
) (AwardRevision, error) {
	if service.revisions == nil {
		return AwardRevision{}, ErrAwardsNotConfigured
	}
	revision, found, err := service.revisions.FindRevision(
		ctx, companyID, revisionID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrAwardRevisionNotFound
	}
	return revision, nil
}
