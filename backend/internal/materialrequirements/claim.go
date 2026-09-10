package materialrequirements

import (
	"context"
	"time"
)

// The five claim primitives materialrequirements owns (design spec §1.3, §7.1).
//
// This module owns the claim FIELDS and the conditional writes on them —
// nothing more. All orchestration (sequencing, compensation, the retry matrix,
// reconciliation, retry_line) is rfqs-owned, because only rfqs can read its own
// lines. This package never inspects an RFQ line, never learns whether one
// exists, and never imports rfqs.
//
// Every method takes and returns primitives or types owned HERE, so rfqs
// satisfies its MaterialRequirementSource interface structurally without
// importing this package (ADR 0002).

// ClaimSnapshot is the allowlisted projection taken from the SAME document the
// claim validated, so the line rfqs appends reflects exactly the state that was
// checked (design spec §7.2 step 4).
//
// It carries no cost, no margin and — deliberately — no InternalNotes: those
// are contractor-only and must never reach a supplier-visible RFQ line (§9).
type ClaimSnapshot struct {
	RequirementID    string
	Revision         int64
	MaterialID       string
	MaterialName     string
	Specification    string
	QuantityValue    string // canonical decimal string; never a float
	QuantityUnit     string
	RequiredByDate   *time.Time
	ProcurementNotes string // supplier-visible
}

// RequirementClaim is one requirement's claim on a chain. Revision is included
// because rfqs releases under a Revision guard and would otherwise need a
// second read per requirement.
type RequirementClaim struct {
	RequirementID string
	RFQChainID    string
	RFQNumber     string
	LineID        string
	Revision      int64
}

// ClaimForRFQ performs the Revision-guarded conditional claim (design spec §7.2
// step 3) and returns the snapshot built from the same document.
//
// projectID is the RFQ's own ProjectID and is enforced INSIDE the atomic
// filter. Without it, a reviewed requirement belonging to a different Project
// of the same company could be pulled into this RFQ.
func (s *Service) ClaimForRFQ(ctx context.Context, companyID, projectID, requirementID string,
	expectedRevision int64, rfqChainID, rfqNumber, lineID string) (ClaimSnapshot, error) {

	claimed, err := s.repo.ClaimForRFQ(ctx, companyID, projectID, requirementID,
		expectedRevision, rfqChainID, rfqNumber, lineID)
	if err != nil {
		return ClaimSnapshot{}, err
	}
	return snapshotOf(claimed), nil
}

// ReleaseClaim clears a claim, conditional on the exact chain and line as well
// as the revision (design spec §7.4).
//
// Line removal precedes release in rfqs' sequence, so the system never makes a
// requirement available to another RFQ while its previous line may still exist.
func (s *Service) ReleaseClaim(ctx context.Context, companyID, requirementID string,
	expectedRevision int64, rfqChainID, lineID string) error {
	_, err := s.repo.ReleaseClaim(ctx, companyID, requirementID, expectedRevision, rfqChainID, lineID)
	return err
}

// ReadClaim reports one requirement's claim state. found is false when the
// requirement exists but holds no claim — distinct from the not-found error a
// missing or foreign requirement returns.
func (s *Service) ReadClaim(ctx context.Context, companyID, requirementID string) (
	rfqChainID string, rfqNumber string, lineID string, revision int64, found bool, err error) {

	req, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return "", "", "", 0, false, err
	}
	if !req.IsClaimed() {
		return "", "", "", req.Revision, false, nil
	}
	return *req.ActiveRFQChainID, derefString(req.ActiveRFQNumber),
		derefString(req.ActiveRFQLineID), req.Revision, true, nil
}

// ListClaimsForRFQChain enumerates every requirement currently claiming this
// chain, without the caller supplying any requirement ID.
//
// This is what makes an ORPHANED claim discoverable: ReadClaim can only confirm
// a claim the caller already suspects, so recovery from a failed compensation
// would be impossible without enumeration (design spec §7.5). It is also how
// §7.3's remove-line retry recovers a requirement ID from a line ID.
func (s *Service) ListClaimsForRFQChain(ctx context.Context, companyID, rfqChainID string) (
	[]RequirementClaim, error) {

	claiming, err := s.repo.ListClaimsForRFQChain(ctx, companyID, rfqChainID)
	if err != nil {
		return nil, err
	}
	claims := make([]RequirementClaim, 0, len(claiming))
	for _, r := range claiming {
		if !r.IsClaimed() {
			continue
		}
		claims = append(claims, RequirementClaim{
			RequirementID: r.ID,
			RFQChainID:    *r.ActiveRFQChainID,
			RFQNumber:     derefString(r.ActiveRFQNumber),
			LineID:        derefString(r.ActiveRFQLineID),
			Revision:      r.Revision,
		})
	}
	return claims, nil
}

// ReadClaimSnapshot returns the allowlisted projection for a requirement held
// under the EXACT claim the caller names (design spec §1.3, §7.3, §7.5).
//
// It exists solely for the recovery paths rfqs owns: repairing an interrupted
// claim-first add, and retry_line rebuilding a missing line. Neither can
// re-claim an already-claimed requirement, so without this the alternatives
// would be fabricating supplier-visible content or leaving the orphan
// permanently unrepairable.
//
// Every term of the claim is required — company, requirement, the exact chain
// AND the exact line — so this can never expose a snapshot from an unclaimed
// requirement, one claimed by a different chain, one claimed under a different
// line, or another tenant. It is a repair capability, not a general read.
//
// It deliberately does NOT re-run IsRFQEligible. The claim was eligible when it
// was created, and while it exists every contractor field is frozen (§2.3);
// detection may still move SourceSyncState (§5.8). Re-checking eligibility here
// would make an interrupted line unrepairable after an ordinary source change,
// even though the snapshot being rebuilt is exactly the one the claim
// validated.
func (s *Service) ReadClaimSnapshot(ctx context.Context, companyID, requirementID,
	rfqChainID, lineID string) (ClaimSnapshot, error) {

	req, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return ClaimSnapshot{}, err
	}
	// The claim must name THIS chain and THIS line. An unclaimed requirement
	// fails the first term. Reported as not-found rather than as a distinct
	// sentinel: from the caller's position there is no repairable claim here,
	// and saying more would disclose the state of a claim it does not hold.
	if req.ActiveRFQChainID == nil || *req.ActiveRFQChainID != rfqChainID {
		return ClaimSnapshot{}, ErrMaterialRequirementNotFound
	}
	if req.ActiveRFQLineID == nil || *req.ActiveRFQLineID != lineID {
		return ClaimSnapshot{}, ErrMaterialRequirementNotFound
	}
	return snapshotOf(req), nil
}

// ClaimedRequirementIsReadyForRFQ validates an ALREADY-CLAIMED requirement at
// mark-ready time (design spec §6.4, §8.5).
//
// It deliberately does NOT reuse IsRFQEligible. That predicate requires
// ActiveRFQChainID == nil, which is false for every requirement already on a
// line — reusing it here would make every non-empty RFQ impossible to mark
// ready. The business conditions are identical; only the claim term differs,
// and here the claim must name THIS chain and THIS line.
func (s *Service) ClaimedRequirementIsReadyForRFQ(ctx context.Context, companyID, requirementID,
	rfqChainID, lineID string) (bool, error) {

	req, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return false, err
	}
	return req.ClaimedIsReadyForRFQ(rfqChainID, lineID), nil
}

// snapshotOf builds the allowlisted projection. Adding a field here makes it
// supplier-visible, so the omission of InternalNotes is deliberate and load
// bearing (design spec §9).
func snapshotOf(r MaterialRequirement) ClaimSnapshot {
	return ClaimSnapshot{
		RequirementID: r.ID,
		Revision:      r.Revision,
		MaterialID:    r.MaterialID,
		MaterialName:  r.MaterialName,
		Specification: r.Specification,
		// The canonical decimal string — quantities are never floats (ADR 0001).
		QuantityValue:    r.RequiredQuantity.Value.String(),
		QuantityUnit:     r.RequiredQuantity.Unit,
		RequiredByDate:   r.RequiredByDate,
		ProcurementNotes: r.ProcurementNotes,
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
