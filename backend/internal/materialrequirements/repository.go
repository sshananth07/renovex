package materialrequirements

import (
	"context"
	"time"
)

// MaterialRequirementRepository persists Material Requirements.
// materialrequirements owns the material_requirements collection exclusively;
// no other module may query it directly (ADR 0002).
//
// The write methods are deliberately narrow rather than one general Update,
// because §5.8 assigns different fields to different writers:
// UpdateContractorFields may never touch the accepted source snapshot, and
// UpdateSyncState may touch nothing BUT the two detection fields. Separate
// methods make that boundary enforceable at the repository, not merely a
// service-layer convention.
type MaterialRequirementRepository interface {
	Create(ctx context.Context, r MaterialRequirement) (MaterialRequirement, error)
	FindByID(ctx context.Context, companyID, id string) (MaterialRequirement, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]MaterialRequirement, error)

	// ListGeneratedAnchorsByProject returns every sourceType=cost_item
	// requirement for the project in EVERY status, including split and
	// archived. Generation's union algorithm depends on seeing terminal anchors:
	// omitting them would silently recreate a requirement the contractor had
	// split or retired (design spec §3.4).
	ListGeneratedAnchorsByProject(ctx context.Context, companyID, projectID string) ([]MaterialRequirement, error)

	// UpdateContractorFields applies a contractor edit under a Revision guard,
	// rejecting terminal and claimed requirements. It never writes the accepted
	// source snapshot or the claim fields (design spec §5.8, §10).
	UpdateContractorFields(ctx context.Context, companyID, id string, expectedRevision int64,
		updated MaterialRequirement) (MaterialRequirement, error)

	// Delete permanently removes one requirement under the same Revision guard
	// as UpdateContractorFields — a stale revision or a missing/foreign
	// document fails closed with ErrRevisionMismatch/ErrMaterialRequirementNotFound,
	// distinguished the same way conditionalUpdate does. The service is what
	// enforces the terminal/claimed refusal (via loadEditable) before calling
	// this; the repository itself does not re-check status here since the
	// caller has already loaded and validated the current document.
	Delete(ctx context.Context, companyID, id string, expectedRevision int64) error

	// UpdateSyncState converges detection output ONLY — SourceSyncState and
	// SourceCheckedAt. It is permitted on claimed and terminal requirements,
	// and must leave the accepted snapshot byte-identical so merge can compute
	// proposed − accepted (design spec §5.8).
	UpdateSyncState(ctx context.Context, companyID, id string, expectedRevision int64,
		state SourceSyncState, checkedAt time.Time) (MaterialRequirement, error)

	// ApplyDiscrepancyResolution is the ONLY write that may advance the accepted
	// source snapshot, and it is reachable only from an explicit contractor
	// resolution (design spec §5.6, §5.8). It writes the snapshot together with
	// the contractor fields a resolution may change, so update and merge move
	// quantity and snapshot in one conditional write and cannot leave a
	// requirement whose quantity moved but whose snapshot did not.
	//
	// The filter rejects claimed requirements. Terminal anchors are NOT rejected
	// here: keep_current is valid on split and archived anchors, and the service
	// enforces which action each status permits (design spec §5.6).
	ApplyDiscrepancyResolution(ctx context.Context, companyID, id string, expectedRevision int64,
		resolved MaterialRequirement) (MaterialRequirement, error)

	// FindByResolutionOperationID loads the child a previous create_separate
	// created, so a retry can VERIFY it belongs to the same logical operation
	// before reusing it. Blind reuse would let one operation ID hand back
	// another discrepancy's child (design spec §5.7).
	FindByResolutionOperationID(ctx context.Context, companyID, operationID string) (MaterialRequirement, error)

	// CreateWithID inserts a requirement at a CALLER-SUPPLIED id, so split
	// children can be created idempotently from the manifest: a retry re-inserts
	// the same _id and is rejected by the primary key rather than duplicating
	// demand. It reports ErrRequirementIDExists when the id is already present
	// (design spec §8.4).
	CreateWithID(ctx context.Context, r MaterialRequirement) (MaterialRequirement, error)

	// BeginSplit is step 1 of §8.4: it moves the source to status=split and
	// writes the child MANIFEST in ONE conditional update. The manifest must
	// land before any child exists — otherwise an interrupted split leaves no
	// record of what it was meant to produce and no retry could be idempotent.
	//
	// The filter enforces every §8.3 precondition that can be raced: the
	// revision, a non-terminal status, sourceType != split, and no active claim.
	BeginSplit(ctx context.Context, companyID, id string, expectedRevision int64,
		manifest []SplitChildRef, splitGroupID, actorUserID string, splitAt time.Time) (MaterialRequirement, error)

	// CompleteSplit is step 3: it flips splitState creating -> completed once
	// every manifest child exists.
	CompleteSplit(ctx context.Context, companyID, id string, expectedRevision int64) (MaterialRequirement, error)

	// ListSplitChildren returns every child of one split group, which lets
	// reconciliation determine exactly which manifest entries are missing.
	ListSplitChildren(ctx context.Context, companyID, splitGroupID string) ([]MaterialRequirement, error)

	// ClaimForRFQ performs the §7.2 step-3 conditional claim. companyID,
	// projectID, the revision, activeRfqChainId = null AND the entire §8.5
	// eligibility predicate live in ONE atomic filter, so none of them can be
	// raced by a concurrent edit or a competing RFQ.
	//
	// On a 0-match it re-reads to distinguish not-found / wrong-project /
	// already-claimed / not-eligible, because those map to different statuses.
	ClaimForRFQ(ctx context.Context, companyID, projectID, id string, expectedRevision int64,
		rfqChainID, rfqNumber, lineID string) (MaterialRequirement, error)

	// ReleaseClaim clears a claim, conditional on the EXACT chain and line as
	// well as the revision. Requiring both means a stale release can never free
	// a requirement that another RFQ legitimately holds (design spec §7.4).
	ReleaseClaim(ctx context.Context, companyID, id string, expectedRevision int64,
		rfqChainID, lineID string) (MaterialRequirement, error)

	// ListClaimsForRFQChain enumerates every requirement claiming one chain,
	// which is what makes an orphaned claim discoverable without the caller
	// knowing any requirement ID in advance (design spec §7.5).
	ListClaimsForRFQChain(ctx context.Context, companyID, rfqChainID string) ([]MaterialRequirement, error)
}
