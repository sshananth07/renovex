package rfqs

import (
	"context"
	"sort"
	"time"
)

// Claim orchestration (design spec §7.2-§7.5). Every sequencing, retry and
// compensation decision lives here; materialrequirements only performs the
// conditional writes it owns.

// AddLine claims one requirement for this chain and appends its snapshot.
//
// The §7.2 sequence is deliberately fail-closed and deliberately NOT atomic —
// M7 claims no cross-document atomicity (§15):
//
//  1. load and validate this draft RFQ, and read its ProjectID
//  2. pre-generate a stable RFQLineID
//  3. claim the requirement (ONE conditional update, carrying projectID)
//  4. build the line from the returned snapshot
//  5. append it, conditional on the RFQ's revision
//  6. on append failure: compensate, or leave the claim for reconciliation
//
// The claim precedes the append so a failure can never leave a line whose
// requirement is unclaimed — a line without a claim would let a second RFQ
// claim the same requirement.
func (s *Service) AddLine(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64, requirementID string, expectedRequirementRevision int64) (RFQ, error) {

	// --- 1. This RFQ must be a draft before anything is claimed. A rejected
	// add must never claim a requirement it cannot then use. ---
	rfq, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}
	if rfq.Status != RFQStatusDraft {
		return RFQ{}, ErrRFQNotDraft
	}

	// --- The §7.3 retry matrix, evaluated by combining ReadClaim with a scan
	// of this chain's OWN lines. Only rfqs can do this. ---
	chainID, _, claimedLineID, claimRevision, claimed, err := s.requirements.ReadClaim(
		ctx, companyID, requirementID)
	if err != nil {
		return RFQ{}, err
	}

	lineID := newLineID()
	var snapshotRevision int64

	switch {
	case claimed && chainID == rfq.ChainID():
		if existing, ok := rfq.FindLineByRequirementID(requirementID); ok {
			// Claimed by THIS chain and the line exists: an uncertain response
			// was retried. Return the existing line rather than duplicating it.
			_ = existing
			return rfq, nil
		}
		// Claimed by THIS chain but the line is MISSING: repair the interrupted
		// operation using the LineID the claim recorded, NOT a fresh one. The
		// stored LineID is what makes this deterministic (design spec §7.3).
		lineID = claimedLineID
		snapshotRevision = claimRevision

	case claimed:
		// Claimed by ANOTHER chain.
		return RFQ{}, ErrMaterialRequirementAlreadyClaimed
	}

	// --- 3. Claim, unless a repair already holds one. ---
	var snap ClaimSnapshot
	if !claimed {
		snap, err = s.requirements.ClaimForRFQ(ctx, companyID, rfq.ProjectID, requirementID,
			expectedRequirementRevision, rfq.ChainID(), rfq.RFQNumber, lineID)
		if err != nil {
			return RFQ{}, err
		}
		snapshotRevision = snap.Revision
	} else {
		// Repairing: rebuild the snapshot from the requirement itself. The
		// claim already validated it, so re-claiming would be both wrong and
		// impossible (it is no longer unclaimed).
		snap, err = s.snapshotForRepair(ctx, companyID, requirementID,
			rfq.ChainID(), lineID, snapshotRevision)
		if err != nil {
			return RFQ{}, err
		}
	}

	// --- 4-5. Build and append. ---
	line, err := LineFromClaimSnapshot(lineID, snap, rfq.NextSortOrder(), time.Now())
	if err != nil {
		s.compensateClaim(ctx, companyID, requirementID, snapshotRevision, rfq.ChainID(), lineID)
		return RFQ{}, err
	}

	saved, err := s.repo.ReplaceLines(ctx, companyID, rfqID, expectedRevision,
		append(append([]RFQLine(nil), rfq.Lines...), line))
	if err != nil {
		// --- 6. Compensation (design spec §7.5). ---
		s.compensateClaim(ctx, companyID, requirementID, snapshotRevision, rfq.ChainID(), lineID)
		// The ORIGINAL append error is returned, whether or not compensation
		// succeeded — the caller needs to know why the append failed.
		return RFQ{}, err
	}

	_ = s.audit.RecordRFQLineAdded(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ChainID(), saved.RFQNumber, requirementID, line.ID)
	return saved, nil
}

// compensateClaim attempts to release a claim whose append failed, using the
// NEW revision the claim returned (design spec §7.5).
//
// A failure here is deliberately swallowed: the claim is LEFT IN PLACE. That is
// fail-closed — the requirement cannot enter another RFQ, and the orphan is
// discoverable through GET /rfqs/{id}/claim-reconciliation. It is never
// silently cleared merely because one read did not find the line.
func (s *Service) compensateClaim(ctx context.Context, companyID, requirementID string,
	revision int64, chainID, lineID string) {
	_ = s.requirements.ReleaseClaim(ctx, companyID, requirementID, revision, chainID, lineID)
}

// snapshotForRepair rebuilds a ClaimSnapshot for a requirement THIS chain
// already holds under THIS line, so an interrupted add can be completed
// without re-claiming.
//
// It never fabricates line content: when the exact claim no longer exists the
// repair fails rather than inventing values, because a line asking a supplier
// to quote something no requirement backs would be worse than a failed repair.
//
// The exact-claim filter lives in the capability itself (§1.3), so there is no
// separate pre-check here — a pre-check would be a second, weaker copy of a
// rule the read already enforces atomically.
func (s *Service) snapshotForRepair(ctx context.Context, companyID, requirementID,
	rfqChainID, lineID string, revision int64) (ClaimSnapshot, error) {

	snap, err := s.requirements.ReadClaimSnapshot(ctx, companyID, requirementID, rfqChainID, lineID)
	if err != nil {
		// The requirement is gone, unclaimed, or claimed by a different
		// chain/line. Either way there is no repairable claim here.
		return ClaimSnapshot{}, ErrNoClaimToReconcile
	}
	// The revision comes from the authoritative CLAIM, not from the read.
	snap.Revision = revision
	return snap, nil
}

// RemoveLine removes a line and then releases its claim (design spec §7.4).
//
// The order is the OPPOSITE of AddLine and deliberately so: line removal
// precedes claim release, so the system never makes a requirement available to
// another RFQ while its previous line may still exist. If the release fails the
// requirement stays temporarily blocked — fail-closed — and a retry completes it.
func (s *Service) RemoveLine(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64, lineID string) (RFQ, error) {

	rfq, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}
	if rfq.Status != RFQStatusDraft {
		return RFQ{}, ErrRFQNotDraft
	}

	line, ok := rfq.FindLine(lineID)
	if !ok {
		// A retry arriving after the line is already gone has no line to read
		// the requirement ID from. Resolve it through the claim instead, so the
		// release can still complete (design spec §7.3).
		return s.completeRemovalFromClaim(ctx, companyID, actorUserID, rfq, lineID)
	}

	remaining := make([]RFQLine, 0, len(rfq.Lines)-1)
	for _, l := range rfq.Lines {
		if l.ID != lineID {
			remaining = append(remaining, l)
		}
	}

	saved, err := s.repo.ReplaceLines(ctx, companyID, rfqID, expectedRevision, remaining)
	if err != nil {
		return RFQ{}, err
	}

	// Step 2: release, under the claim's own Revision guard.
	if err := s.releaseClaimForRequirement(ctx, companyID, line.SourceMaterialRequirementID,
		rfq.ChainID(), lineID); err != nil {
		return RFQ{}, err
	}

	_ = s.audit.RecordRFQLineRemoved(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ChainID(), saved.RFQNumber, line.SourceMaterialRequirementID, lineID)
	return saved, nil
}

// completeRemovalFromClaim finishes a removal whose line write already landed
// but whose release did not. The requirement ID is recovered from the CLAIM via
// ListClaimsForRFQChain, matched on LineID — the inverse lookup ReadClaim
// cannot perform (design spec §7.3, §7.5).
func (s *Service) completeRemovalFromClaim(ctx context.Context, companyID, actorUserID string,
	rfq RFQ, lineID string) (RFQ, error) {

	claims, err := s.requirements.ListClaimsForRFQChain(ctx, companyID, rfq.ChainID())
	if err != nil {
		return RFQ{}, err
	}
	for _, c := range claims {
		if c.LineID != lineID {
			continue
		}
		if err := s.requirements.ReleaseClaim(ctx, companyID, c.RequirementID, c.Revision,
			rfq.ChainID(), lineID); err != nil {
			return RFQ{}, err
		}
		_ = s.audit.RecordRFQLineRemoved(ctx, companyID, rfq.ProjectID, actorUserID,
			rfq.ChainID(), rfq.RFQNumber, c.RequirementID, lineID)
		return s.repo.FindByID(ctx, companyID, rfq.ID)
	}
	// Neither a line nor a claim names this id: there is nothing left to do.
	return RFQ{}, ErrRFQLineNotFound
}

// releaseClaimForRequirement releases under the claim's CURRENT revision, read
// from the authoritative claim rather than taken from client input.
func (s *Service) releaseClaimForRequirement(ctx context.Context, companyID, requirementID,
	chainID, lineID string) error {

	_, _, _, revision, found, err := s.requirements.ReadClaim(ctx, companyID, requirementID)
	if err != nil {
		return err
	}
	if !found {
		// Already released — a retry. Nothing to do.
		return nil
	}
	return s.requirements.ReleaseClaim(ctx, companyID, requirementID, revision, chainID, lineID)
}

// SetLineSortOrder reorders one line under a Revision guard.
func (s *Service) SetLineSortOrder(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64, lineID string, sortOrder int) (RFQ, error) {

	rfq, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}
	if rfq.Status != RFQStatusDraft {
		return RFQ{}, ErrRFQNotDraft
	}
	if _, ok := rfq.FindLine(lineID); !ok {
		return RFQ{}, ErrRFQLineNotFound
	}

	lines := append([]RFQLine(nil), rfq.Lines...)
	for i := range lines {
		if lines[i].ID == lineID {
			lines[i].SortOrder = sortOrder
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].SortOrder < lines[j].SortOrder })

	saved, err := s.repo.ReplaceLines(ctx, companyID, rfqID, expectedRevision, lines)
	if err != nil {
		return RFQ{}, err
	}
	_ = s.audit.RecordRFQUpdated(ctx, companyID, saved.ProjectID, actorUserID, saved.ChainID(), saved.RFQNumber)
	return saved, nil
}

// --- Reconciliation (design spec §7.5) ---

// Reconciliation classifications.
const (
	ReconciliationConsistent    = "consistent"
	ReconciliationOrphanedClaim = "orphaned_claim"
	ReconciliationOrphanedLine  = "orphaned_line"
)

// Reconciliation actions.
const (
	ReconcileActionRetryLine = "retry_line"
	ReconcileActionRelease   = "release"
)

// ReconciliationEntry is one requirement's claim/line consistency verdict.
type ReconciliationEntry struct {
	RequirementID  string
	LineID         string
	Revision       int64
	Classification string
}

// ReconciliationReport is the §7.5 GET result.
type ReconciliationReport struct {
	RFQChainID string
	Entries    []ReconciliationEntry
}

// GetClaimReconciliation joins this chain's claims against its own lines.
//
// ListClaimsForRFQChain is what makes an ORPHANED claim discoverable: ReadClaim
// answers only for a requirement ID the caller already has, and an orphaned
// claim is by definition one this package cannot name from its own lines. That
// inverse lookup is precisely why the capability exists.
func (s *Service) GetClaimReconciliation(ctx context.Context, companyID, rfqID string) (
	ReconciliationReport, error) {

	rfq, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return ReconciliationReport{}, err
	}

	claims, err := s.requirements.ListClaimsForRFQChain(ctx, companyID, rfq.ChainID())
	if err != nil {
		return ReconciliationReport{}, err
	}

	report := ReconciliationReport{RFQChainID: rfq.ChainID()}
	claimedRequirements := map[string]struct{}{}

	for _, c := range claims {
		claimedRequirements[c.RequirementID] = struct{}{}
		entry := ReconciliationEntry{
			RequirementID: c.RequirementID, LineID: c.LineID, Revision: c.Revision,
		}
		if line, ok := rfq.FindLineByRequirementID(c.RequirementID); ok && line.ID == c.LineID {
			entry.Classification = ReconciliationConsistent
		} else {
			// A claim with no matching line — the actionable case.
			entry.Classification = ReconciliationOrphanedClaim
		}
		report.Entries = append(report.Entries, entry)
	}

	// A line whose requirement no longer claims this chain.
	for _, l := range rfq.Lines {
		if _, ok := claimedRequirements[l.SourceMaterialRequirementID]; ok {
			continue
		}
		report.Entries = append(report.Entries, ReconciliationEntry{
			RequirementID:  l.SourceMaterialRequirementID,
			LineID:         l.ID,
			Classification: ReconciliationOrphanedLine,
		})
	}

	sort.SliceStable(report.Entries, func(i, j int) bool {
		return report.Entries[i].RequirementID < report.Entries[j].RequirementID
	})
	return report, nil
}

// ReconcileClaim applies retry_line or release.
//
// Both are driven by data from ListClaimsForRFQChain — the chain ID, line ID
// and expected Revision all come from the AUTHORITATIVE claim rather than from
// client input, which is what makes them deterministic (design spec §7.5).
func (s *Service) ReconcileClaim(ctx context.Context, companyID, actorUserID, rfqID,
	requirementID, action string) (RFQ, error) {

	if action != ReconcileActionRetryLine && action != ReconcileActionRelease {
		return RFQ{}, ErrReconciliationActionInvalid
	}

	rfq, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}

	claims, err := s.requirements.ListClaimsForRFQChain(ctx, companyID, rfq.ChainID())
	if err != nil {
		return RFQ{}, err
	}
	var claim RFQRequirementClaim
	found := false
	for _, c := range claims {
		if c.RequirementID == requirementID {
			claim, found = c, true
			break
		}
	}
	if !found {
		return RFQ{}, ErrNoClaimToReconcile
	}

	switch action {
	case ReconcileActionRelease:
		if err := s.requirements.ReleaseClaim(ctx, companyID, requirementID, claim.Revision,
			rfq.ChainID(), claim.LineID); err != nil {
			return RFQ{}, err
		}

	case ReconcileActionRetryLine:
		if rfq.Status != RFQStatusDraft {
			return RFQ{}, ErrRFQNotDraft
		}
		if _, ok := rfq.FindLine(claim.LineID); ok {
			// Already appended — nothing to repair.
			return rfq, nil
		}
		// The snapshot is read under the CLAIM's own chain and line, so
		// retry_line can only ever rebuild the line that claim describes.
		snap, err := s.snapshotForRepair(ctx, companyID, requirementID,
			rfq.ChainID(), claim.LineID, claim.Revision)
		if err != nil {
			return RFQ{}, err
		}
		// The line is appended at the CLAIM's LineID, never a fresh one.
		line, err := LineFromClaimSnapshot(claim.LineID, snap, rfq.NextSortOrder(), time.Now())
		if err != nil {
			return RFQ{}, err
		}
		if _, err := s.repo.ReplaceLines(ctx, companyID, rfqID, rfq.Revision,
			append(append([]RFQLine(nil), rfq.Lines...), line)); err != nil {
			return RFQ{}, err
		}
	}

	_ = s.audit.RecordRFQClaimReconciled(ctx, companyID, rfq.ProjectID, actorUserID,
		rfq.ChainID(), requirementID, action)
	return s.repo.FindByID(ctx, companyID, rfqID)
}
