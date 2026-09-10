package awards

import "errors"

// Bounded Phase F failures. Each is distinct so the HTTP layer can map it to
// the exact status code §8C–§8K fixes, and so a contractor learns what to fix
// rather than receiving one opaque refusal.
var (
	ErrAwardsNotConfigured = errors.New(
		"awards: required capabilities are not configured")

	// Tenant-safe: a missing resource and another company's resource are
	// indistinguishable to the caller, so a 404 never confirms existence.
	ErrIssuedRFQNotFound = errors.New(
		"awards: issued rfq version not found")
	ErrAwardChainNotFound = errors.New(
		"awards: award chain not found")
	ErrAwardDraftNotFound = errors.New(
		"awards: award draft not found")
	ErrAwardRevisionNotFound = errors.New(
		"awards: award revision not found")
	ErrAwardOutcomeNotFound = errors.New(
		"awards: award outcome not found")
	ErrAwardDeliveryNotFound = errors.New(
		"awards: award outcome delivery not found")

	// Shape violations. These are structural faults rather than contractor
	// mistakes, so they never carry a distinct HTTP meaning beyond 422.
	ErrInvalidAwardChain = errors.New(
		"awards: invalid award decision chain state")
	ErrInvalidAwardRevision = errors.New(
		"awards: invalid award revision")
	ErrInvalidAwardDraft = errors.New(
		"awards: invalid award draft")
	ErrInvalidAcknowledgement = errors.New(
		"awards: invalid award outcome acknowledgement")
	ErrInvalidAwardDelivery = errors.New(
		"awards: invalid award outcome delivery")
	ErrInvalidAwardOutcome = errors.New(
		"awards: invalid award outcome")
	ErrInvalidAwardDecision = errors.New(
		"awards: invalid award line decision")
	ErrAwardDraftIncomplete = errors.New(
		"awards: every issued rfq line must be decided before finalisation")

	ErrAwardDraftConflict = errors.New(
		"awards: award draft state or revision conflict")
	ErrAwardDeliveryConflict = errors.New(
		"awards: award delivery is not in a retryable state")
	ErrAwardRevisionConflict = errors.New(
		"awards: award chain state or revision conflict")

	// The ten F3 validations of §8D, in the order they are checked. Identity is
	// verified before money, so an invalid selection never reaches arithmetic.
	ErrOfferVersionNotSelectable = errors.New(
		"awards: offer version does not belong to this company and issued rfq version")
	ErrOfferVersionNotEligible = errors.New(
		"awards: offer version is withdrawn or claimed by another operation")
	ErrOfferVersionExpired = errors.New(
		"awards: offer version validity has passed")
	ErrOfferLineNotQuoted = errors.New(
		"awards: only a positively quoted offer line may be awarded")
	ErrQuantityOrUnitMismatch = errors.New(
		"awards: quoted quantity and unit must match the issued rfq line")
	ErrCurrencyMismatch = errors.New(
		"awards: offer currency must match the issued rfq currency")
	// D1: a whole-offer tax figure is never apportioned or omitted, so such an
	// offer is awarded with every positively quoted line or none.
	ErrOfferLevelTaxRequiresComplete = errors.New(
		"awards: offer-level tax requires awarding every quoted line of that offer")
	ErrConditionalChargesNotResolvable = errors.New(
		"awards: conditional charge rules do not resolve for the selected lines")

	// The F4 cross-version lineage claim (D4). Already-awarded is terminal;
	// claimed-by-a-live-operation is a transient conflict.
	ErrRFQLineAlreadyAwarded = errors.New(
		"awards: this rfq line is already awarded on this rfq chain")
	ErrRFQLineAwardConflict = errors.New(
		"awards: this rfq line is claimed by another award finalisation")

	// §8G: an award, once published, is never reduced, removed or reassigned.
	// Reversing one is a rescission, which is out of scope for M8.
	ErrAwardCorrectionNotMonotonic = errors.New(
		"awards: a correction may not remove, reduce or reassign a published award")

	// The bounded read during the F5 crash window. A read must never report
	// "no award exists" while an authoritative revision is present.
	ErrAwardFinalisationPending = errors.New(
		"awards: award finalisation is in progress")

	ErrInvalidUnawardedReason = errors.New(
		"awards: unawarded reason is outside the bounded set")
	ErrChangeReasonRequired = errors.New(
		"awards: a change reason is required from revision 2 onward")
)
