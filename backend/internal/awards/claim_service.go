package awards

import (
	"context"
	"sort"
	"time"
)

// Two-tier claim acquisition (§8E).
//
// One award may select several Offer Versions and several RFQ lineages. Claims
// must be acquired without deadlock, without partial publication, and without
// ever releasing another operation's claim.
//
// The acquisition order is:
//
//  1. the Award chain draft -> finalising (done by the caller, §8F step 2)
//  2. AWARD LINE CLAIMS, ordered by StableLineageID ascending
//  3. OFFER ELIGIBILITY GATES, ordered by OfferVersionID ascending
//
// Ordering is lexicographic and total, so two concurrent finalisations request
// shared resources in the same sequence and one always wins outright — the
// standard deadlock-avoidance argument.

// AwardLineClaimRepository is the narrow lineage-claim surface finalisation
// needs.
type AwardLineClaimRepository interface {
	ClaimLineage(
		ctx context.Context,
		input AwardLineClaimInput,
	) (AwardLineClaim, error)
	FindClaim(
		ctx context.Context,
		companyID, claimID string,
	) (AwardLineClaim, bool, error)
	FindClaimByLineage(
		ctx context.Context,
		companyID, rfqChainID, stableLineageID string,
	) (AwardLineClaim, bool, error)
	ListClaimsForOperation(
		ctx context.Context,
		companyID, awardOperationID string,
	) ([]AwardLineClaim, error)
	MarkLineageAwarded(
		ctx context.Context,
		companyID, claimID, awardOperationID, awardRevisionID string,
		expectedRevision int64,
	) error
	ReleaseLineageClaim(
		ctx context.Context,
		companyID, claimID, awardOperationID string,
		expectedRevision int64,
	) error
}

func WithAwardLineClaimRepository(
	repository AwardLineClaimRepository,
) ServiceOption {
	return func(service *Service) { service.lineClaims = repository }
}

// OfferVersionClaimTarget names one Offer Version gate to acquire, with the
// eligibility revision the caller observed during validation.
type OfferVersionClaimTarget struct {
	OfferVersionID   string
	ExpectedRevision int64
}

type ClaimAcquisitionRequest struct {
	CompanyID           string
	RFQChainID          string
	IssuedRFQVersionID  string
	OperationID         string
	CandidateRevisionID string
	Lineages            []string
	OfferVersions       []OfferVersionClaimTarget
	AcquiredAt          time.Time
}

// AcquiredClaims records exactly what this operation took, so a later release
// touches its own claims and nothing else.
type AcquiredClaims struct {
	LineClaims []AwardLineClaim
	Gates      []OfferEligibilitySnapshot
}

// AcquireClaims takes both tiers, unwinding cleanly on any failure.
func (service *Service) AcquireClaims(
	ctx context.Context,
	request ClaimAcquisitionRequest,
) (AcquiredClaims, error) {
	if service.lineClaims == nil || service.eligibility == nil {
		return AcquiredClaims{}, ErrAwardsNotConfigured
	}

	// Sort copies rather than the caller's slices: mutating an argument would
	// make the caller's own retry order differ from the first attempt.
	lineages := append([]string(nil), request.Lineages...)
	sort.Strings(lineages)
	versions := append([]OfferVersionClaimTarget(nil), request.OfferVersions...)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].OfferVersionID < versions[j].OfferVersionID
	})

	acquired := AcquiredClaims{}

	for _, lineage := range lineages {
		claim, err := service.lineClaims.ClaimLineage(ctx, AwardLineClaimInput{
			CompanyID:           request.CompanyID,
			RFQChainID:          request.RFQChainID,
			StableLineageID:     lineage,
			IssuedRFQVersionID:  request.IssuedRFQVersionID,
			AwardOperationID:    request.OperationID,
			CandidateRevisionID: request.CandidateRevisionID,
		})
		if err != nil {
			// Unwind before returning, so a failed attempt leaves no partial
			// claim set behind to block the contractor's next decision.
			service.unwind(ctx, request.CompanyID, request.OperationID, acquired)
			return AcquiredClaims{}, err
		}
		acquired.LineClaims = append(acquired.LineClaims, claim)
	}

	for _, target := range versions {
		gate, err := service.eligibility.ClaimOfferForAward(ctx,
			OfferEligibilityClaimRequest{
				CompanyID:        request.CompanyID,
				OfferVersionID:   target.OfferVersionID,
				OperationID:      request.OperationID,
				ClaimID:          request.CandidateRevisionID,
				ExpectedRevision: target.ExpectedRevision,
				ClaimedAt:        request.AcquiredAt,
			})
		if err != nil {
			service.unwind(ctx, request.CompanyID, request.OperationID, acquired)
			return AcquiredClaims{}, err
		}
		acquired.Gates = append(acquired.Gates, gate)
	}

	return acquired, nil
}

// unwind releases this operation's own claims in REVERSE acquisition order.
//
// It is best-effort: the originating error is what the caller must see, and a
// release failure here does not change that diagnosis. A claim left behind is
// recoverable by F10 reconciliation, whereas masking the original error is not.
func (service *Service) unwind(
	ctx context.Context,
	companyID, operationID string,
	acquired AcquiredClaims,
) {
	for index := len(acquired.Gates) - 1; index >= 0; index-- {
		gate := acquired.Gates[index]
		_, _ = service.eligibility.ReleaseOfferAwardClaim(ctx,
			OfferEligibilityReleaseRequest{
				CompanyID:        companyID,
				OfferVersionID:   gate.OfferVersionID,
				OperationID:      operationID,
				ClaimID:          gate.ClaimID,
				ExpectedRevision: gate.Revision,
			})
	}
	for index := len(acquired.LineClaims) - 1; index >= 0; index-- {
		claim := acquired.LineClaims[index]
		// The repository filter pins company, operation and state, so this can
		// only ever affect a claim this operation still holds.
		_ = service.lineClaims.ReleaseLineageClaim(
			ctx, companyID, claim.ID, operationID, claim.Revision)
	}
}

type ReleaseClaimsRequest struct {
	CompanyID   string
	OperationID string
	Acquired    AcquiredClaims

	// AwardRevisionExists is the D2 guard. The caller MUST determine this by
	// looking for a matching immutable Award Revision before asking for a
	// release.
	AwardRevisionExists bool
}

// ReleaseClaims releases an abandoned operation's own claims.
//
// The no-revision-exists verification lives HERE rather than in Phase E (§8E).
// Phase E's ReleaseAwardClaim enforces state, claim type, operation ID and
// claim ID — but it does not and must not know whether an Award Revision
// exists: supplieroffers has no award knowledge, and giving it any would
// violate ADR 0002. An implementation that assumed Phase E performed this check
// would release claims after an authoritative award, the exact failure D2
// forbids.
func (service *Service) ReleaseClaims(
	ctx context.Context,
	request ReleaseClaimsRequest,
) error {
	if service.lineClaims == nil || service.eligibility == nil {
		return ErrAwardsNotConfigured
	}
	if request.AwardRevisionExists {
		// Once the revision exists, release is prohibited and the correct
		// action is always to complete forward.
		return ErrAwardRevisionConflict
	}
	service.unwind(ctx, request.CompanyID, request.OperationID, request.Acquired)
	return nil
}

type CompleteClaimsRequest struct {
	CompanyID       string
	OperationID     string
	AwardRevisionID string
	Acquired        AcquiredClaims
	CompletedAt     time.Time
}

// CompleteClaims marks both tiers awarded after publication.
//
// This is recoverable post-publication work (D2 steps 7-8): it may run more
// than once, and each transition is idempotent for its own operation.
func (service *Service) CompleteClaims(
	ctx context.Context,
	request CompleteClaimsRequest,
) error {
	if service.lineClaims == nil || service.eligibility == nil {
		return ErrAwardsNotConfigured
	}

	completedAt := request.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}

	for _, claim := range request.Acquired.LineClaims {
		if err := service.lineClaims.MarkLineageAwarded(ctx,
			request.CompanyID, claim.ID, request.OperationID,
			request.AwardRevisionID, claim.Revision); err != nil {
			return err
		}
	}
	for _, gate := range request.Acquired.Gates {
		if _, err := service.eligibility.CompleteOfferAward(ctx,
			OfferEligibilityCompletionRequest{
				CompanyID:        request.CompanyID,
				OfferVersionID:   gate.OfferVersionID,
				OperationID:      request.OperationID,
				ExpectedRevision: gate.Revision,
				CompletedAt:      completedAt,
			}); err != nil {
			return err
		}
	}
	return nil
}
