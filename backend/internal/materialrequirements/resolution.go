package materialrequirements

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ResolutionAction is one of the four ways a contractor may resolve a source
// discrepancy (design spec §5.6).
type ResolutionAction string

const (
	ResolutionActionUpdate         ResolutionAction = "update"
	ResolutionActionMerge          ResolutionAction = "merge"
	ResolutionActionKeepCurrent    ResolutionAction = "keep_current"
	ResolutionActionCreateSeparate ResolutionAction = "create_separate"
)

// IsValid reports whether a is one of the four defined actions.
func (a ResolutionAction) IsValid() bool {
	switch a {
	case ResolutionActionUpdate, ResolutionActionMerge,
		ResolutionActionKeepCurrent, ResolutionActionCreateSeparate:
		return true
	default:
		return false
	}
}

// SourceDiscrepancy is the computed proposal a contractor reads before
// resolving (design spec §5.5). It is a projection, never persisted: the
// proposal is recomputed from the live source on every read and again inside
// the resolution, so a stale proposal can be shown but never applied.
type SourceDiscrepancy struct {
	RequirementRevision int64
	SyncState           SourceSyncState
	AnchorStatus        RequirementStatus

	AcceptedQuantity    quantity.Quantity
	AcceptedCostItemIDs []string
	AcceptedFingerprint string

	ProposedQuantity    quantity.Quantity
	ProposedCostItemIDs []string
	ProposedFingerprint string

	AvailableActions []ResolutionAction
}

// ResolveDiscrepancyInput carries a resolution request. Both expectations are
// mandatory: ExpectedRevision guards the document, ExpectedProposedFingerprint
// guards the SOURCE the contractor was looking at (design spec §5.5).
type ResolveDiscrepancyInput struct {
	Action                      ResolutionAction
	ExpectedRevision            int64
	ExpectedProposedFingerprint string

	// ResolutionOperationID is required by create_separate only, where it makes
	// the creation idempotent under retry (design spec §5.7).
	ResolutionOperationID string
}

// proposal is the recomputed source state for one anchor, shared by the read
// and the resolution so both see identical arithmetic.
type proposal struct {
	quantity    quantity.Quantity
	costItemIDs []string
	fingerprint string
	rowCount    int
}

// GetSourceDiscrepancy recomputes the current source for one anchor and reports
// it alongside the accepted snapshot and the actions available (design spec
// §5.5).
func (s *Service) GetSourceDiscrepancy(ctx context.Context, companyID, requirementID string) (SourceDiscrepancy, error) {
	anchor, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return SourceDiscrepancy{}, err
	}
	if anchor.SourceType != SourceTypeCostItem {
		// A manual or split requirement has no CostItem aggregate, so it is
		// permanently clean and has nothing to resolve (design spec §8.5).
		return SourceDiscrepancy{}, ErrNoSourceDiscrepancy
	}

	p, err := s.computeProposal(ctx, anchor)
	if err != nil {
		return SourceDiscrepancy{}, err
	}

	state := anchor.SourceSyncState
	accepted := quantity.Quantity{Value: decimal.Zero, Unit: anchor.SourceAggregationUnit}
	if anchor.SourceQuantity != nil {
		accepted = *anchor.SourceQuantity
	}

	return SourceDiscrepancy{
		RequirementRevision: anchor.Revision,
		SyncState:           state,
		AnchorStatus:        anchor.Status,

		AcceptedQuantity:    accepted,
		AcceptedCostItemIDs: anchor.SourceCostItemIDs,
		AcceptedFingerprint: anchor.SourceFingerprint,

		ProposedQuantity:    p.quantity,
		ProposedCostItemIDs: p.costItemIDs,
		ProposedFingerprint: p.fingerprint,

		AvailableActions: availableActions(anchor, accepted, p),
	}, nil
}

// availableActions implements the §5.6 matrix.
//
// update and merge mutate RequiredQuantity, so both are impossible on a
// terminal anchor. A claimed requirement offers nothing at all: §2.3 freezes
// every contractor field while a claim exists.
func availableActions(anchor MaterialRequirement, accepted quantity.Quantity, p proposal) []ResolutionAction {
	if anchor.IsClaimed() {
		return nil
	}
	if anchor.SourceSyncState == SourceSyncStateClean {
		return nil
	}

	// keep_current is valid on every non-claimed anchor status, including
	// terminal ones: it changes no contractor field.
	actions := []ResolutionAction{ResolutionActionKeepCurrent}

	delta := p.quantity.Value.Sub(accepted.Value)

	// create_separate needs a strictly positive delta: a zero delta from a
	// membership-only change creates nothing, and a negative one cannot become
	// a negative requirement (design spec §5.6).
	//
	// It is withheld on source_removed, where §5.6 offers keep_current and
	// archive only.
	if anchor.SourceSyncState != SourceSyncStateSourceRemoved && delta.IsPositive() {
		actions = append(actions, ResolutionActionCreateSeparate)
	}

	if anchor.IsTerminal() {
		return actions
	}
	if anchor.SourceSyncState == SourceSyncStateSourceRemoved {
		return actions
	}

	actions = append(actions, ResolutionActionUpdate)
	if mergeAvailable(anchor, accepted, p) {
		actions = append(actions, ResolutionActionMerge)
	}
	sortActions(actions)
	return actions
}

// mergeAvailable implements §5.6's three merge preconditions. M7 never converts
// between units, so adding a source-unit delta to a quantity the contractor
// converted to another unit would be arithmetically meaningless — merge is
// withheld rather than guessed at.
func mergeAvailable(anchor MaterialRequirement, accepted quantity.Quantity, p proposal) bool {
	if UnitsMismatch(anchor.RequiredQuantity.Unit, anchor.SourceAggregationUnit) {
		return false
	}
	if UnitsMismatch(accepted.Unit, p.quantity.Unit) {
		return false
	}
	return mergedQuantity(anchor, accepted, p).IsPositive()
}

// mergedQuantity is §5.6's formula, in one place so the availability check and
// the applied result can never disagree:
//
//	current RequiredQuantity + (proposed SourceQuantity − ACCEPTED SourceQuantity)
func mergedQuantity(anchor MaterialRequirement, accepted quantity.Quantity, p proposal) decimal.Decimal {
	return anchor.RequiredQuantity.Value.Add(p.quantity.Value.Sub(accepted.Value))
}

func sortActions(actions []ResolutionAction) {
	sort.Slice(actions, func(i, j int) bool { return actions[i] < actions[j] })
}

// ResolveSourceDiscrepancy applies one explicit contractor resolution.
//
// This is the ONLY path that advances the accepted source snapshot. Detection
// may never do so, because merge computes proposed − accepted and a
// detection-advanced snapshot would collapse the delta to zero, silently
// discarding the contractor's adjustment (design spec §5.8).
func (s *Service) ResolveSourceDiscrepancy(ctx context.Context, companyID, actorUserID, requirementID string,
	input ResolveDiscrepancyInput) (MaterialRequirement, error) {

	if !input.Action.IsValid() {
		return MaterialRequirement{}, ErrResolutionActionNotAvailable
	}

	anchor, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return MaterialRequirement{}, err
	}
	if anchor.SourceType != SourceTypeCostItem {
		return MaterialRequirement{}, ErrNoSourceDiscrepancy
	}
	// Reported before the staleness guards so the contractor's actionable next
	// step — remove the RFQ line — is named rather than a generic conflict
	// (design spec §2.3).
	if anchor.IsClaimed() {
		return MaterialRequirement{}, ErrMaterialRequirementAlreadyClaimed
	}

	// Recompute the source and verify it still matches what the contractor saw.
	// Applied BEFORE any write, so a stale proposal applies nothing (§5.5).
	p, err := s.computeProposal(ctx, anchor)
	if err != nil {
		return MaterialRequirement{}, err
	}
	if p.fingerprint != input.ExpectedProposedFingerprint {
		return MaterialRequirement{}, ErrMaterialRequirementDiscrepancyChanged
	}

	accepted := quantity.Quantity{Value: decimal.Zero, Unit: anchor.SourceAggregationUnit}
	if anchor.SourceQuantity != nil {
		accepted = *anchor.SourceQuantity
	}

	syncStateBefore := string(anchor.SourceSyncState)

	if input.Action == ResolutionActionCreateSeparate {
		// The idempotency check runs BEFORE the availability gate, and only for
		// create_separate. A retry arrives after step 3 already cleared the
		// anchor to clean, at which point availableActions offers nothing — so
		// gating first would reject exactly the retry the operation ID exists to
		// make safe (design spec §5.7).
		//
		// Availability is still enforced for a FIRST attempt, inside
		// applyCreateSeparate, once the duplicate-key path has been ruled out.
		return s.applyCreateSeparate(ctx, companyID, actorUserID, anchor, accepted, p, input, syncStateBefore)
	}

	if !containsResolutionAction(availableActions(anchor, accepted, p), input.Action) {
		return MaterialRequirement{}, ErrResolutionActionNotAvailable
	}

	resolved := anchor
	switch input.Action {
	case ResolutionActionUpdate:
		// Quantity AND unit move to proposed; M7 converts nothing.
		resolved.RequiredQuantity = p.quantity
		resolved.Status = RequirementStatusDraft
	case ResolutionActionMerge:
		resolved.RequiredQuantity = quantity.Quantity{
			Value: mergedQuantity(anchor, accepted, p),
			Unit:  anchor.RequiredQuantity.Unit,
		}
		resolved.Status = RequirementStatusDraft
	case ResolutionActionKeepCurrent:
		// RequiredQuantity and Status both stay put: a deliberate no-op is not a
		// procurement edit, so it must not re-open review (design spec §5.6).
	}

	if input.Action != ResolutionActionKeepCurrent {
		// Either side of the comparison may have moved, so the mismatch is
		// recomputed rather than carried over, and any prior acknowledgement is
		// cleared — it was scoped to the unit in force when it was given.
		newMismatch := UnitsMismatch(resolved.RequiredQuantity.Unit, resolved.CatalogUnit)
		if newMismatch != resolved.UnitMismatch || resolved.RequiredQuantity.Unit != anchor.RequiredQuantity.Unit {
			resolved.UnitMismatchAcknowledged = false
		}
		resolved.UnitMismatch = newMismatch
	}

	s.acceptProposal(&resolved, p)

	saved, err := s.repo.ApplyDiscrepancyResolution(ctx, companyID, requirementID, input.ExpectedRevision, resolved)
	if err != nil {
		return MaterialRequirement{}, err
	}
	_ = s.audit.RecordMaterialRequirementDiscrepancyResolved(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ID, string(input.Action), syncStateBefore, string(anchor.Status))
	return saved, nil
}

// acceptProposal advances the accepted snapshot to the proposal and marks the
// anchor clean. Every resolution ends here — that is what stops a resolved
// discrepancy from warning forever (design spec §5.3).
func (s *Service) acceptProposal(r *MaterialRequirement, p proposal) {
	now := time.Now()
	r.SourceCostItemIDs = p.costItemIDs
	r.SourceQuantity = &p.quantity
	r.SourceFingerprint = p.fingerprint
	r.SourceSyncedAt = &now
	r.SourceSyncState = SourceSyncStateClean
	r.SourceCheckedAt = &now
}

// computeProposal recomputes the live aggregate for one anchor's aggregation
// key. An anchor whose CostItems have all been deleted yields a zero-row
// proposal with a well-defined empty-aggregate fingerprint, which is what lets
// keep_current accept a removal permanently (design spec §5.2, §5.3).
func (s *Service) computeProposal(ctx context.Context, anchor MaterialRequirement) (proposal, error) {
	var rows []SourceRow
	total := decimal.Zero

	_, _, _, _, _, err := s.costSource.VisitEligibleMaterialCostItems(ctx, anchor.CompanyID, anchor.ProjectID,
		func(costItemID, workItemID, materialID string, qty decimal.Decimal, unit string) error {
			key := ComputeSourceAggregationKey(anchor.CompanyID, anchor.ProjectID,
				WorkItemRef(workItemID), materialID, unit)
			if key != anchor.SourceAggregationKey {
				return nil
			}
			rows = append(rows, SourceRow{CostItemID: costItemID, Quantity: qty})
			total = total.Add(qty)
			return nil
		})
	if err != nil {
		return proposal{}, err
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].CostItemID < rows[j].CostItemID })
	costItemIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		costItemIDs = append(costItemIDs, r.CostItemID)
	}

	return proposal{
		// The proposed unit is the immutable aggregation unit, never the
		// contractor-editable RequiredQuantity.Unit (design spec §3.3).
		quantity:    quantity.Quantity{Value: total, Unit: anchor.SourceAggregationUnit},
		costItemIDs: costItemIDs,
		fingerprint: ComputeSourceFingerprint(anchor.ProjectID, WorkItemRefFromPointer(anchor.WorkItemID),
			anchor.MaterialID, anchor.SourceAggregationUnit, rows),
		rowCount: len(rows),
	}, nil
}

// applyCreateSeparate creates a delta requirement and then refreshes the source
// anchor's accepted snapshot (design spec §5.7).
//
// The two steps are NOT atomic and M7 does not pretend otherwise: a crash
// between them leaves a draft child plus an unresolved discrepancy. It cannot
// duplicate demand — the unique partial index on
// {companyId, resolutionOperationId} forbids a second child — and the orphan is
// a draft, therefore not RFQ-eligible.
func (s *Service) applyCreateSeparate(ctx context.Context, companyID, actorUserID string,
	anchor MaterialRequirement, accepted quantity.Quantity, p proposal,
	input ResolveDiscrepancyInput, syncStateBefore string) (MaterialRequirement, error) {

	if input.ResolutionOperationID == "" {
		return MaterialRequirement{}, ErrResolutionOperationIDRequired
	}

	// A RETRY is resolved before anything else, because it cannot be judged by
	// the current state: step 3 of the previous attempt already advanced the
	// accepted snapshot, so both the §5.6 availability matrix and the
	// proposed − accepted delta now describe the resolved world, not the one the
	// operation was issued against (design spec §5.7).
	//
	// The child's own CreatedFromSourceFingerprint is what pins the operation to
	// a specific discrepancy, and it is verified below.
	existing, err := s.repo.FindByResolutionOperationID(ctx, companyID, input.ResolutionOperationID)
	switch {
	case err == nil:
		return s.completeCreateSeparateRetry(ctx, companyID, actorUserID, anchor, p, input,
			existing, syncStateBefore)
	case !errors.Is(err, ErrMaterialRequirementNotFound):
		return MaterialRequirement{}, err
	}

	// A first attempt must satisfy the §5.6 matrix.
	if !containsResolutionAction(availableActions(anchor, accepted, p), ResolutionActionCreateSeparate) {
		return MaterialRequirement{}, ErrResolutionActionNotAvailable
	}

	deltaValue := p.quantity.Value.Sub(accepted.Value)
	opID := input.ResolutionOperationID
	fingerprint := p.fingerprint
	sourceID := anchor.ID
	now := time.Now()

	child := MaterialRequirement{
		CompanyID: companyID,
		// ProjectID must be inherited: a child under the wrong project could
		// never be claimed by any RFQ in its own project (design spec §7.2).
		ProjectID:     anchor.ProjectID,
		WorkItemID:    anchor.WorkItemID,
		MaterialID:    anchor.MaterialID,
		MaterialName:  anchor.MaterialName,
		Specification: anchor.Specification,
		CatalogUnit:   anchor.CatalogUnit,
		// The delta is expressed in the PROPOSED SOURCE unit, which may differ
		// from the source requirement's contractor-edited unit.
		RequiredQuantity: quantity.Quantity{Value: deltaValue, Unit: p.quantity.Unit},
		// Recomputed for the child's own unit; an acknowledgement never
		// transfers to a new record (design spec §5.6).
		UnitMismatch:             UnitsMismatch(p.quantity.Unit, anchor.CatalogUnit),
		UnitMismatchAcknowledged: false,
		RequiredByDate:           anchor.RequiredByDate,
		Status:                   RequirementStatusDraft,
		// The child carries no SourceCostItemIDs, SourceAggregationKey or
		// SourceAggregationUnit: it must not pretend to independently own the
		// same CostItem aggregate, so it is permanently clean (design spec §8.5).
		SourceType:      SourceTypeManual,
		SourceSyncState: SourceSyncStateClean,

		CreatedFromDiscrepancyRequirementID: &sourceID,
		CreatedFromSourceFingerprint:        &fingerprint,
		ResolutionOperationID:               &opID,

		CreatedByUserID: actorUserID,
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}

	created, err := s.repo.Create(ctx, child)
	if errors.Is(err, ErrResolutionOperationAlreadyApplied) {
		// A CONCURRENT attempt won the unique index between this call's
		// pre-check and its insert. The index — not the pre-check — is the
		// authority on which attempt is first, so the loser verifies and reuses
		// the winner's child exactly as a retry would (design spec §5.7).
		winner, findErr := s.repo.FindByResolutionOperationID(ctx, companyID, opID)
		if findErr != nil {
			return MaterialRequirement{}, findErr
		}
		if !sameLogicalResolution(winner, child) {
			return MaterialRequirement{}, ErrResolutionOperationConflict
		}
		created = winner
	} else if err != nil {
		return MaterialRequirement{}, err
	}

	if err := s.refreshAnchorAfterCreateSeparate(ctx, companyID, actorUserID, anchor, p,
		input.ExpectedRevision, syncStateBefore); err != nil {
		return MaterialRequirement{}, err
	}
	return created, nil
}

// completeCreateSeparateRetry verifies an existing child belongs to THIS
// resolution and, if so, re-runs step 3 — the snapshot refresh that a crashed
// or unacknowledged first attempt may never have reached (design spec §5.7).
//
// Verification compares the child's recorded provenance and quantity, never a
// delta recomputed from the current snapshot: after a successful step 3 the
// live delta is zero, so recomputing it would make every legitimate retry look
// like a different operation.
func (s *Service) completeCreateSeparateRetry(ctx context.Context, companyID, actorUserID string,
	anchor MaterialRequirement, p proposal, input ResolveDiscrepancyInput,
	existing MaterialRequirement, syncStateBefore string) (MaterialRequirement, error) {

	// --- Step 2: verify the child records THIS logical operation. ---
	//
	// Identity mismatches are ErrResolutionOperationConflict: the operation ID
	// was reused against a different resolution, which is a client error about
	// WHICH operation this is.
	if existing.CompanyID != companyID {
		return MaterialRequirement{}, ErrResolutionOperationConflict
	}
	// The operation must name THIS discrepancy on THIS requirement. Without
	// both, one operation ID could hand back another anchor's child while
	// leaving this anchor silently unresolved (design spec §5.7).
	if existing.CreatedFromDiscrepancyRequirementID == nil ||
		*existing.CreatedFromDiscrepancyRequirementID != anchor.ID {
		return MaterialRequirement{}, ErrResolutionOperationConflict
	}
	if existing.CreatedFromSourceFingerprint == nil {
		return MaterialRequirement{}, ErrResolutionOperationConflict
	}
	if existing.MaterialID != anchor.MaterialID {
		return MaterialRequirement{}, ErrResolutionOperationConflict
	}
	if !samePtr(existing.WorkItemID, anchor.WorkItemID) {
		return MaterialRequirement{}, ErrResolutionOperationConflict
	}
	if existing.RequiredQuantity.Unit != p.quantity.Unit {
		return MaterialRequirement{}, ErrResolutionOperationConflict
	}

	// --- Steps 3-4: the STALENESS guard, which the operation ID does not
	// license bypassing (design spec §5.5, §5.7). ---
	//
	// The child's CreatedFromSourceFingerprint records the source the delta was
	// computed against. Both the live source AND the caller's expectation must
	// still agree with it. These are distinct failures from an identity
	// mismatch — the operation IS this one; the world moved underneath it — so
	// they report ErrMaterialRequirementDiscrepancyChanged.
	//
	// Without this, an operation ID would let a retry refresh the anchor to a
	// source state the contractor never saw, justified by a delta child computed
	// against a different one.
	childFingerprint := *existing.CreatedFromSourceFingerprint
	if p.fingerprint != childFingerprint {
		// The CostItems changed after the child was created.
		return MaterialRequirement{}, ErrMaterialRequirementDiscrepancyChanged
	}
	if input.ExpectedProposedFingerprint != childFingerprint {
		// The caller is describing a different proposal than the child's.
		return MaterialRequirement{}, ErrMaterialRequirementDiscrepancyChanged
	}

	// --- Step 5: revision-guarded refresh of the source anchor. ---
	//
	// Idempotent in effect but still guarded: when the previous attempt already
	// completed it, the anchor is clean and its snapshot already equals the
	// proposal, so re-applying changes nothing observable. A stale revision is
	// still reported rather than forced.
	if anchor.SourceSyncState != SourceSyncStateClean ||
		anchor.SourceFingerprint != p.fingerprint {
		if err := s.refreshAnchorAfterCreateSeparate(ctx, companyID, actorUserID, anchor, p,
			input.ExpectedRevision, syncStateBefore); err != nil {
			return MaterialRequirement{}, err
		}
	}
	return existing, nil
}

// refreshAnchorAfterCreateSeparate is step 3 of §5.7: the revision-guarded
// advance of the source anchor's accepted snapshot, run after the delta child
// exists. It is deliberately a separate write from the child's creation — M7
// claims no cross-document atomicity (design spec §15).
func (s *Service) refreshAnchorAfterCreateSeparate(ctx context.Context, companyID, actorUserID string,
	anchor MaterialRequirement, p proposal, expectedRevision int64, syncStateBefore string) error {

	resolved := anchor
	s.acceptProposal(&resolved, p)
	if _, err := s.repo.ApplyDiscrepancyResolution(ctx, companyID, anchor.ID,
		expectedRevision, resolved); err != nil {
		return err
	}
	_ = s.audit.RecordMaterialRequirementDiscrepancyResolved(ctx, companyID, anchor.ProjectID, actorUserID,
		anchor.ID, string(ResolutionActionCreateSeparate), syncStateBefore, string(anchor.Status))
	return nil
}

// sameLogicalResolution reports whether an existing child records the SAME
// logical operation as the one being retried. All seven identity fields must
// match; any difference means the operation ID was reused against a different
// resolution (design spec §5.7).
func sameLogicalResolution(existing, attempted MaterialRequirement) bool {
	if existing.CompanyID != attempted.CompanyID {
		return false
	}
	if !samePtr(existing.CreatedFromDiscrepancyRequirementID, attempted.CreatedFromDiscrepancyRequirementID) {
		return false
	}
	if !samePtr(existing.CreatedFromSourceFingerprint, attempted.CreatedFromSourceFingerprint) {
		return false
	}
	if existing.MaterialID != attempted.MaterialID {
		return false
	}
	if !samePtr(existing.WorkItemID, attempted.WorkItemID) {
		return false
	}
	if !existing.RequiredQuantity.Value.Equal(attempted.RequiredQuantity.Value) {
		return false
	}
	return existing.RequiredQuantity.Unit == attempted.RequiredQuantity.Unit
}

func containsResolutionAction(actions []ResolutionAction, want ResolutionAction) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}
