package composition

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// --- materialrequirements -> rfqs.MaterialRequirementSource ---

// MaterialRequirementSourceAdapter converts materialrequirements-owned claim
// types into the rfqs-owned ones, and translates the sentinels (design spec
// §1.4.1).
//
// It exists because Go interface satisfaction requires exact named return
// types: materialrequirements.Service naturally returns
// materialrequirements.ClaimSnapshot, while rfqs declares rfqs.ClaimSnapshot.
// Without this shim one module would have to import the other's types, which
// ADR 0002 forbids.
//
// This adapter and the M6 pair above are the ONLY non-test production code that
// imports two domain modules at once. rfqs imports no materialrequirements
// type at all, and materialrequirements imports nothing from rfqs.
//
// It converts SIX methods. ReadClaimSnapshot was added by revision 4 of the
// design spec, so an interrupted claim-first add and retry_line can rebuild a
// missing line without re-claiming or fabricating supplier-visible content.
type MaterialRequirementSourceAdapter struct {
	requirements *materialrequirements.Service
}

// NewMaterialRequirementSourceAdapter wraps a materialrequirements.Service.
func NewMaterialRequirementSourceAdapter(svc *materialrequirements.Service) *MaterialRequirementSourceAdapter {
	return &MaterialRequirementSourceAdapter{requirements: svc}
}

// translateRequirementError maps materialrequirements' sentinels onto rfqs'
// own.
//
// rfqs matches only its OWN sentinels at the handler boundary, so a
// materialrequirements sentinel that escaped here would fall through to 500
// instead of the status §13.5 assigns it.
//
// An UNRECOGNISED error passes through unchanged. Laundering an unknown
// infrastructure failure into a domain sentinel would invent a client-facing
// meaning the adapter cannot justify.
func translateRequirementError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, materialrequirements.ErrMaterialRequirementNotFound):
		// Also the answer for a wrong project, a wrong chain, a wrong line and
		// an unclaimed requirement: materialrequirements deliberately reports
		// all of them as not-found so a caller cannot learn the state of a
		// claim it does not hold (design spec §1.3, §7.2).
		return rfqs.ErrMaterialRequirementNotFound
	case errors.Is(err, materialrequirements.ErrMaterialRequirementAlreadyClaimed):
		return rfqs.ErrMaterialRequirementAlreadyClaimed
	case errors.Is(err, materialrequirements.ErrRequirementNotEligibleForRFQ),
		errors.Is(err, materialrequirements.ErrUnresolvedUnitMismatch),
		errors.Is(err, materialrequirements.ErrUnresolvedSourceDiscrepancy):
		// rfqs expresses all three as "this line's requirement is not
		// eligible"; it has no vocabulary for the specific requirement-side
		// reason, and inventing one would leak the other module's model.
		return rfqs.ErrRFQLineNotEligible
	case errors.Is(err, materialrequirements.ErrRevisionMismatch):
		return rfqs.ErrRevisionMismatch
	default:
		return err
	}
}

// toRFQClaimSnapshot performs the field-for-field conversion.
//
// The target type has NO InternalNotes field and no price field of any kind, so
// the contractor-only note and any cost have nowhere to land — the privacy
// boundary is structural, not a matter of remembering to omit them.
func toRFQClaimSnapshot(s materialrequirements.ClaimSnapshot) rfqs.ClaimSnapshot {
	return rfqs.ClaimSnapshot{
		RequirementID:    s.RequirementID,
		Revision:         s.Revision,
		MaterialID:       s.MaterialID,
		MaterialName:     s.MaterialName,
		Specification:    s.Specification,
		QuantityValue:    s.QuantityValue,
		QuantityUnit:     s.QuantityUnit,
		RequiredByDate:   s.RequiredByDate,
		ProcurementNotes: s.ProcurementNotes,
	}
}

// ClaimForRFQ performs the Revision-guarded conditional claim (design spec
// §7.2 steps 3-4).
func (a *MaterialRequirementSourceAdapter) ClaimForRFQ(ctx context.Context,
	companyID, projectID, requirementID string, expectedRevision int64,
	rfqChainID, rfqNumber, lineID string) (rfqs.ClaimSnapshot, error) {

	snap, err := a.requirements.ClaimForRFQ(ctx, companyID, projectID, requirementID,
		expectedRevision, rfqChainID, rfqNumber, lineID)
	if err != nil {
		return rfqs.ClaimSnapshot{}, translateRequirementError(err)
	}
	return toRFQClaimSnapshot(snap), nil
}

// ReleaseClaim clears a claim under the exact chain, line and revision.
func (a *MaterialRequirementSourceAdapter) ReleaseClaim(ctx context.Context,
	companyID, requirementID string, expectedRevision int64, rfqChainID, lineID string) error {

	return translateRequirementError(
		a.requirements.ReleaseClaim(ctx, companyID, requirementID, expectedRevision,
			rfqChainID, lineID))
}

// ReadClaim reports one requirement's claim state.
func (a *MaterialRequirementSourceAdapter) ReadClaim(ctx context.Context,
	companyID, requirementID string) (string, string, string, int64, bool, error) {

	chainID, number, lineID, revision, found, err := a.requirements.ReadClaim(ctx,
		companyID, requirementID)
	if err != nil {
		return "", "", "", 0, false, translateRequirementError(err)
	}
	return chainID, number, lineID, revision, found, nil
}

// ListClaimsForRFQChain enumerates a chain's claims, converting each into the
// rfqs-owned type. This is what makes an orphaned claim discoverable.
func (a *MaterialRequirementSourceAdapter) ListClaimsForRFQChain(ctx context.Context,
	companyID, rfqChainID string) ([]rfqs.RFQRequirementClaim, error) {

	claims, err := a.requirements.ListClaimsForRFQChain(ctx, companyID, rfqChainID)
	if err != nil {
		return nil, translateRequirementError(err)
	}
	// An empty result is an empty slice, never nil, so the caller need not
	// special-case it.
	out := make([]rfqs.RFQRequirementClaim, 0, len(claims))
	for _, c := range claims {
		out = append(out, rfqs.RFQRequirementClaim{
			RequirementID: c.RequirementID,
			RFQChainID:    c.RFQChainID,
			RFQNumber:     c.RFQNumber,
			LineID:        c.LineID,
			Revision:      c.Revision,
		})
	}
	return out, nil
}

// ClaimedRequirementIsReadyForRFQ validates an already-claimed requirement at
// mark-ready time (design spec §6.4, §8.5).
func (a *MaterialRequirementSourceAdapter) ClaimedRequirementIsReadyForRFQ(ctx context.Context,
	companyID, requirementID, rfqChainID, lineID string) (bool, error) {

	ready, err := a.requirements.ClaimedRequirementIsReadyForRFQ(ctx, companyID,
		requirementID, rfqChainID, lineID)
	if err != nil {
		return false, translateRequirementError(err)
	}
	return ready, nil
}

// ReadClaimSnapshot returns the immutable snapshot for a requirement held under
// the EXACT claim named, so a repair can rebuild a missing line (design spec
// §1.3 revision 4, §7.3, §7.5).
//
// Every exact-claim mismatch — wrong chain, wrong line, unclaimed, foreign
// tenant — arrives as materialrequirements' not-found sentinel and is
// translated to rfqs' own, so the caller learns only that there is no
// repairable claim here.
func (a *MaterialRequirementSourceAdapter) ReadClaimSnapshot(ctx context.Context,
	companyID, requirementID, rfqChainID, lineID string) (rfqs.ClaimSnapshot, error) {

	snap, err := a.requirements.ReadClaimSnapshot(ctx, companyID, requirementID,
		rfqChainID, lineID)
	if err != nil {
		return rfqs.ClaimSnapshot{}, translateRequirementError(err)
	}
	return toRFQClaimSnapshot(snap), nil
}
