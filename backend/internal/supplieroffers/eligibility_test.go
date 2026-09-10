package supplieroffers

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSupplierOfferEligibilityValidateClaimReasonByClaimType(t *testing.T) {
	claimedAt := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		eligibility SupplierOfferEligibility
		wantErr     bool
	}{
		{
			name: "withdrawal claim with bounded reason",
			eligibility: SupplierOfferEligibility{
				ID: "eligibility-1", CompanyID: "company-1", OfferChainID: "chain-1",
				OfferVersionID: "version-1", State: EligibilityWithdrawalClaimed,
				ClaimType: EligibilityClaimWithdrawal, OperationID: "operation-1",
				ClaimID: "withdrawal-1", WithdrawalReason: "Supplier revised its commercial offer",
				Revision: 2, ClaimedAt: &claimedAt,
			},
		},
		{
			name: "withdrawal claim without reason",
			eligibility: SupplierOfferEligibility{
				ID: "eligibility-1", CompanyID: "company-1", OfferChainID: "chain-1",
				OfferVersionID: "version-1", State: EligibilityWithdrawalClaimed,
				ClaimType: EligibilityClaimWithdrawal, OperationID: "operation-1",
				ClaimID: "withdrawal-1", Revision: 2, ClaimedAt: &claimedAt,
			},
			wantErr: true,
		},
		{
			name: "award claim with withdrawal reason",
			eligibility: SupplierOfferEligibility{
				ID: "eligibility-1", CompanyID: "company-1", OfferChainID: "chain-1",
				OfferVersionID: "version-1", State: EligibilityAwardClaimed,
				ClaimType: EligibilityClaimAward, OperationID: "award-operation-1",
				ClaimID: "award-revision-1", WithdrawalReason: "must not be accepted",
				Revision: 2, ClaimedAt: &claimedAt,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.eligibility.Validate()
			if tt.wantErr && !errors.Is(err, ErrInvalidOfferEligibility) {
				t.Fatalf("Validate error = %v, want ErrInvalidOfferEligibility", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate returned an unexpected error: %v", err)
			}
		})
	}
}

func TestSupplierOfferEligibilityValidateStateTimestamps(t *testing.T) {
	claimedAt := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	completedAt := claimedAt.Add(time.Minute)
	base := SupplierOfferEligibility{
		ID: "eligibility-1", CompanyID: "company-1", OfferChainID: "chain-1",
		OfferVersionID: "version-1", Revision: 1,
	}

	tests := []struct {
		name        string
		eligibility SupplierOfferEligibility
		wantErr     bool
	}{
		{
			name: "initial eligible state",
			eligibility: func() SupplierOfferEligibility {
				value := base
				value.State = EligibilityEligible
				return value
			}(),
		},
		{
			name: "eligible state forbids claim fields",
			eligibility: func() SupplierOfferEligibility {
				value := base
				value.State = EligibilityEligible
				value.OperationID = "operation-1"
				return value
			}(),
			wantErr: true,
		},
		{
			name: "claimed state forbids completed timestamp",
			eligibility: func() SupplierOfferEligibility {
				value := base
				value.State = EligibilityAwardClaimed
				value.ClaimType = EligibilityClaimAward
				value.OperationID = "operation-1"
				value.ClaimID = "award-1"
				value.ClaimedAt = &claimedAt
				value.CompletedAt = &completedAt
				return value
			}(),
			wantErr: true,
		},
		{
			name: "completed state requires completed timestamp",
			eligibility: func() SupplierOfferEligibility {
				value := base
				value.State = EligibilityAwarded
				value.ClaimType = EligibilityClaimAward
				value.OperationID = "operation-1"
				value.ClaimID = "award-1"
				value.ClaimedAt = &claimedAt
				return value
			}(),
			wantErr: true,
		},
		{
			name: "completed award state",
			eligibility: func() SupplierOfferEligibility {
				value := base
				value.State = EligibilityAwarded
				value.ClaimType = EligibilityClaimAward
				value.OperationID = "operation-1"
				value.ClaimID = "award-1"
				value.ClaimedAt = &claimedAt
				value.CompletedAt = &completedAt
				return value
			}(),
		},
		{
			name: "withdrawal reason is bounded",
			eligibility: func() SupplierOfferEligibility {
				value := base
				value.State = EligibilityWithdrawalClaimed
				value.ClaimType = EligibilityClaimWithdrawal
				value.OperationID = "operation-1"
				value.ClaimID = "withdrawal-1"
				value.WithdrawalReason = strings.Repeat("r", MaxWithdrawalReasonLength+1)
				value.ClaimedAt = &claimedAt
				return value
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.eligibility.Validate()
			if tt.wantErr && !errors.Is(err, ErrInvalidOfferEligibility) {
				t.Fatalf("Validate error = %v, want ErrInvalidOfferEligibility", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate returned an unexpected error: %v", err)
			}
		})
	}
}
