package awards

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Award correction publication (§8G).
//
// A correction reuses F5's publication machinery — chain CAS, immutable insert,
// recoverable completion — but calculates through CalculateCorrection, and
// claims the DELTA only. Baseline selections already hold terminal claims;
// re-acquiring them would fail, since a terminal claim is not `eligible`.

type CorrectAwardInput struct {
	IssuedRFQVersionID string
	OperationID        string
	ChangeReason       string
	Selections         []AwardLineSelection
	CorrectedAt        time.Time
}

// CorrectAward publishes a superseding immutable revision.
func (service *Service) CorrectAward(
	ctx context.Context,
	companyID, actorUserID string,
	input CorrectAwardInput,
) (AwardRevision, error) {
	if service.issuedRFQ == nil || service.offers == nil ||
		service.chains == nil || service.revisions == nil ||
		service.lineClaims == nil || service.eligibility == nil {
		return AwardRevision{}, ErrAwardsNotConfigured
	}

	// A superseding record that cannot say why it happened is not auditable,
	// so this is checked before anything else is read or written.
	if strings.TrimSpace(input.ChangeReason) == "" {
		return AwardRevision{}, ErrChangeReasonRequired
	}

	correctedAt := input.CorrectedAt
	if correctedAt.IsZero() {
		correctedAt = time.Now().UTC()
	}

	// An unknown-outcome retry resolves by operation ID first, exactly as F5
	// does: re-running would otherwise allocate revision 3 for one correction.
	if existing, found, err := service.revisions.FindRevisionByOperation(
		ctx, companyID, input.OperationID); err != nil {
		return AwardRevision{}, err
	} else if found {
		return service.completePublication(ctx, companyID, actorUserID,
			existing, AcquiredClaims{},
			FinaliseAwardInput{FinalisedAt: correctedAt})
	}

	issued, found, err := service.issuedRFQ.GetIssuedRFQForAward(
		ctx, companyID, input.IssuedRFQVersionID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrIssuedRFQNotFound
	}

	chain, found, err := service.chains.FindChainByIssuedVersion(
		ctx, companyID, input.IssuedRFQVersionID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrAwardChainNotFound
	}
	// Corrections are refused while FinalisationState != published: there must
	// be a settled award to correct, and an in-flight one must not move.
	if chain.FinalisationState != FinalisationPublished {
		if chain.FinalisationState == FinalisationFinalising {
			return AwardRevision{}, ErrAwardRevisionConflict
		}
		return AwardRevision{}, ErrAwardRevisionNotFound
	}

	baseline, found, err := service.revisions.FindLatestRevision(
		ctx, companyID, chain.ID)
	if err != nil {
		return AwardRevision{}, err
	}
	if !found {
		return AwardRevision{}, ErrAwardRevisionNotFound
	}

	// Load every referenced version. Baseline versions are loaded for their
	// frozen figures only and are never revalidated (§8G).
	versions := map[string]OfferVersionSnapshot{}
	for _, selection := range input.Selections {
		if selection.Unawarded || selection.OfferVersionID == "" {
			continue
		}
		if _, loaded := versions[selection.OfferVersionID]; loaded {
			continue
		}
		version, found, err := service.offers.GetOfferVersionForAward(
			ctx, companyID, selection.OfferVersionID)
		if err != nil {
			return AwardRevision{}, err
		}
		if !found {
			return AwardRevision{}, ErrOfferVersionNotSelectable
		}
		versions[selection.OfferVersionID] = version
	}

	correction, err := CalculateCorrection(CorrectionCalculationInput{
		IssuedRFQ:          issued,
		BaselineRevision:   baseline,
		ProposedSelections: input.Selections,
		OfferVersions:      versions,
		CalculatedAt:       correctedAt,
	})
	if err != nil {
		return AwardRevision{}, err
	}

	publisher, ok := service.chains.(AwardChainPublisher)
	if !ok {
		return AwardRevision{}, ErrAwardsNotConfigured
	}
	candidateRevisionID := bson.NewObjectID().Hex()
	chain, err = publisher.ClaimFinalisation(ctx, FinalisationClaimInput{
		CompanyID: companyID, AwardChainID: chain.ID,
		ExpectedRevision:    chain.Revision,
		OperationID:         input.OperationID,
		CandidateRevisionID: candidateRevisionID,
	})
	if err != nil {
		return AwardRevision{}, err
	}

	// Claim the DELTA only. Awarded gates and lineage claims from the baseline
	// are terminal and are never touched, released or re-acquired.
	acquired, err := service.AcquireClaims(ctx, ClaimAcquisitionRequest{
		CompanyID: companyID, RFQChainID: issued.RFQChainID,
		IssuedRFQVersionID:  issued.ID,
		OperationID:         input.OperationID,
		CandidateRevisionID: chain.FinalisingRevisionID,
		Lineages:            correction.NewlyAwardedLineages,
		OfferVersions:       correctionClaimTargets(correction, versions),
		AcquiredAt:          correctedAt,
	})
	if err != nil {
		_ = publisher.AbandonFinalisation(
			ctx, companyID, chain.ID, input.OperationID, chain.Revision)
		return AwardRevision{}, err
	}

	supersedes := baseline.ID
	revision, err := service.revisions.InsertRevision(ctx, AwardRevision{
		CompanyID:               companyID,
		AwardChainID:            chain.ID,
		RFQChainID:              issued.RFQChainID,
		IssuedRFQVersionID:      issued.ID,
		RevisionNumber:          baseline.RevisionNumber + 1,
		FinalisationOperationID: input.OperationID,
		SelectionFingerprint:    correction.SelectionFingerprint,
		ChangeReason:            input.ChangeReason,
		AwardedLines:            correction.AwardedLines,
		UnawardedLines:          correction.UnawardedLines,
		SupplierSummaries:       correction.SupplierSummaries,
		GrandAwardTotal:         correction.GrandAwardTotal,
		SupersedesRevisionID:    &supersedes,
		FinalisedByUserID:       actorUserID,
		FinalisedAt:             correctedAt,
	})
	if err != nil {
		// Nothing was published, so releasing this operation's OWN delta
		// claims is safe. Baseline claims were never acquired here.
		_ = service.ReleaseClaims(ctx, ReleaseClaimsRequest{
			CompanyID: companyID, OperationID: input.OperationID,
			Acquired: acquired, AwardRevisionExists: false,
		})
		_ = publisher.AbandonFinalisation(
			ctx, companyID, chain.ID, input.OperationID, chain.Revision)
		return AwardRevision{}, err
	}

	return service.completePublication(ctx, companyID, actorUserID,
		revision, acquired, FinaliseAwardInput{FinalisedAt: correctedAt})
}

// correctionClaimTargets names only the offer versions the DELTA newly selects.
func correctionClaimTargets(
	correction CorrectionCalculation,
	versions map[string]OfferVersionSnapshot,
) []OfferVersionClaimTarget {
	targets := make([]OfferVersionClaimTarget, 0,
		len(correction.NewlyClaimedOfferVersions))
	for _, versionID := range correction.NewlyClaimedOfferVersions {
		targets = append(targets, OfferVersionClaimTarget{
			OfferVersionID:   versionID,
			ExpectedRevision: versions[versionID].EligibilityRevision,
		})
	}
	return targets
}
