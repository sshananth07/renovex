package supplieroffers

import (
	"strings"
	"time"
	"unicode/utf8"
)

const MaxWithdrawalReasonLength = 500

type EligibilityState string

const (
	EligibilityEligible          EligibilityState = "eligible"
	EligibilityWithdrawalClaimed EligibilityState = "withdrawal_claimed"
	EligibilityWithdrawn         EligibilityState = "withdrawn"
	EligibilityAwardClaimed      EligibilityState = "award_claimed"
	EligibilityAwarded           EligibilityState = "awarded"
)

type EligibilityClaimType string

const (
	EligibilityClaimWithdrawal EligibilityClaimType = "withdrawal"
	EligibilityClaimAward      EligibilityClaimType = "award"
)

// SupplierOfferEligibility is the separate mutable serialization aggregate.
// The immutable Offer Version never carries withdrawal or award state.
type SupplierOfferEligibility struct {
	ID             string
	CompanyID      string
	OfferChainID   string
	OfferVersionID string

	State            EligibilityState
	ClaimType        EligibilityClaimType
	OperationID      string
	ClaimID          string
	WithdrawalReason string
	Revision         int64

	ClaimedAt   *time.Time
	CompletedAt *time.Time
}

// Validate enforces state-shape invariants independently of repository CAS
// filters. This prevents malformed claim data from becoming recoverable state.
func (eligibility SupplierOfferEligibility) Validate() error {
	if strings.TrimSpace(eligibility.ID) == "" ||
		strings.TrimSpace(eligibility.CompanyID) == "" ||
		strings.TrimSpace(eligibility.OfferChainID) == "" ||
		strings.TrimSpace(eligibility.OfferVersionID) == "" ||
		eligibility.Revision < 1 {
		return ErrInvalidOfferEligibility
	}

	switch eligibility.State {
	case EligibilityEligible:
		if eligibility.ClaimType != "" ||
			eligibility.OperationID != "" ||
			eligibility.ClaimID != "" ||
			eligibility.WithdrawalReason != "" ||
			eligibility.ClaimedAt != nil ||
			eligibility.CompletedAt != nil {
			return ErrInvalidOfferEligibility
		}
		return nil
	case EligibilityWithdrawalClaimed, EligibilityWithdrawn:
		if eligibility.ClaimType != EligibilityClaimWithdrawal ||
			strings.TrimSpace(eligibility.OperationID) == "" ||
			strings.TrimSpace(eligibility.ClaimID) == "" ||
			strings.TrimSpace(eligibility.WithdrawalReason) == "" ||
			utf8.RuneCountInString(strings.TrimSpace(eligibility.WithdrawalReason)) >
				MaxWithdrawalReasonLength ||
			eligibility.ClaimedAt == nil {
			return ErrInvalidOfferEligibility
		}
	case EligibilityAwardClaimed, EligibilityAwarded:
		if eligibility.ClaimType != EligibilityClaimAward ||
			strings.TrimSpace(eligibility.OperationID) == "" ||
			strings.TrimSpace(eligibility.ClaimID) == "" ||
			eligibility.WithdrawalReason != "" ||
			eligibility.ClaimedAt == nil {
			return ErrInvalidOfferEligibility
		}
	default:
		return ErrInvalidOfferEligibility
	}

	completed := eligibility.State == EligibilityWithdrawn ||
		eligibility.State == EligibilityAwarded
	if completed != (eligibility.CompletedAt != nil) {
		return ErrInvalidOfferEligibility
	}
	return nil
}
