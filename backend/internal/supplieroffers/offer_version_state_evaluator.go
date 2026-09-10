package supplieroffers

import "time"

// OfferVersionStatus is the Supplier-visible lifecycle projection. It is
// derived from authoritative facts and is never persisted as another mutable
// status that could drift from the chain or eligibility aggregate.
type OfferVersionStatus string

const (
	OfferVersionStatusPending    OfferVersionStatus = "pending"
	OfferVersionStatusAwarded    OfferVersionStatus = "awarded"
	OfferVersionStatusWithdrawn  OfferVersionStatus = "withdrawn"
	OfferVersionStatusSuperseded OfferVersionStatus = "superseded"
	OfferVersionStatusExpired    OfferVersionStatus = "expired"
	OfferVersionStatusEligible   OfferVersionStatus = "eligible"
)

// OfferVersionStateFacts contains exactly the authoritative inputs shared by
// withdrawal mutation and Supplier projection. Expiry is retained for display
// status but deliberately does not participate in CanWithdraw.
type OfferVersionStateFacts struct {
	OfferVersionID           string
	LatestSubmittedVersionID string
	EligibilityState         EligibilityState
	EligibilityClaimType     EligibilityClaimType
	OfferValidUntil          time.Time
	EvaluatedAt              time.Time
}

type OfferVersionStateProjection struct {
	Status           OfferVersionStatus
	CanWithdraw      bool
	IsSuperseded     bool
	PendingClaimType EligibilityClaimType
}

// EvaluateOfferVersionState is the one domain-level evaluator used by reads
// and mutation. The precedence is intentional: a terminal withdrawal remains
// visible even when a later submission also makes that version superseded.
func EvaluateOfferVersionState(facts OfferVersionStateFacts) OfferVersionStateProjection {
	isLatest := facts.OfferVersionID != "" &&
		facts.OfferVersionID == facts.LatestSubmittedVersionID
	result := OfferVersionStateProjection{
		CanWithdraw:  isLatest && facts.EligibilityState == EligibilityEligible,
		IsSuperseded: !isLatest,
	}

	switch facts.EligibilityState {
	case EligibilityAwardClaimed, EligibilityWithdrawalClaimed:
		result.Status = OfferVersionStatusPending
		result.PendingClaimType = facts.EligibilityClaimType
	case EligibilityAwarded:
		result.Status = OfferVersionStatusAwarded
	case EligibilityWithdrawn:
		result.Status = OfferVersionStatusWithdrawn
	case EligibilityEligible:
		switch {
		case result.IsSuperseded:
			result.Status = OfferVersionStatusSuperseded
		case !facts.OfferValidUntil.IsZero() && !facts.EvaluatedAt.Before(facts.OfferValidUntil):
			result.Status = OfferVersionStatusExpired
		default:
			result.Status = OfferVersionStatusEligible
		}
	default:
		// Invalid persisted eligibility fails closed: it cannot be withdrawn and
		// is exposed as pending until reconciliation resolves the state.
		result.Status = OfferVersionStatusPending
		result.CanWithdraw = false
		result.PendingClaimType = facts.EligibilityClaimType
	}
	return result
}
