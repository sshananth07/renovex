package supplieroffers

import (
	"testing"
	"time"
)

func TestOfferVersionStateEvaluatorWithdrawalPredicateIgnoresExpiry(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		latestID   string
		state      EligibilityState
		validUntil time.Time
		want       bool
	}{
		{name: "latest eligible", latestID: "version-1", state: EligibilityEligible,
			validUntil: now.Add(time.Hour), want: true},
		{name: "latest expired eligible", latestID: "version-1", state: EligibilityEligible,
			validUntil: now.Add(-time.Hour), want: true},
		{name: "superseded eligible", latestID: "version-2", state: EligibilityEligible,
			validUntil: now.Add(time.Hour), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := EvaluateOfferVersionState(OfferVersionStateFacts{
				OfferVersionID: "version-1", LatestSubmittedVersionID: test.latestID,
				EligibilityState: test.state, OfferValidUntil: test.validUntil,
				EvaluatedAt: now,
			})
			if projection.CanWithdraw != test.want {
				t.Errorf("CanWithdraw = %v, want %v", projection.CanWithdraw, test.want)
			}
		})
	}
}

func TestOfferVersionStateEvaluatorAppliesApprovedPrecedenceAndPendingKind(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		state      EligibilityState
		latestID   string
		validUntil time.Time
		wantStatus OfferVersionStatus
		wantClaim  EligibilityClaimType
	}{
		{name: "award pending", state: EligibilityAwardClaimed, latestID: "version-1",
			validUntil: now.Add(-time.Hour), wantStatus: OfferVersionStatusPending,
			wantClaim: EligibilityClaimAward},
		{name: "withdrawal pending", state: EligibilityWithdrawalClaimed, latestID: "version-1",
			validUntil: now.Add(-time.Hour), wantStatus: OfferVersionStatusPending,
			wantClaim: EligibilityClaimWithdrawal},
		{name: "awarded beats superseded", state: EligibilityAwarded, latestID: "version-2",
			validUntil: now.Add(-time.Hour), wantStatus: OfferVersionStatusAwarded},
		{name: "withdrawn beats superseded", state: EligibilityWithdrawn, latestID: "version-2",
			validUntil: now.Add(-time.Hour), wantStatus: OfferVersionStatusWithdrawn},
		{name: "superseded beats expired", state: EligibilityEligible, latestID: "version-2",
			validUntil: now.Add(-time.Hour), wantStatus: OfferVersionStatusSuperseded},
		{name: "expired beats eligible", state: EligibilityEligible, latestID: "version-1",
			validUntil: now.Add(-time.Hour), wantStatus: OfferVersionStatusExpired},
		{name: "eligible fallback", state: EligibilityEligible, latestID: "version-1",
			validUntil: now.Add(time.Hour), wantStatus: OfferVersionStatusEligible},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := EvaluateOfferVersionState(OfferVersionStateFacts{
				OfferVersionID: "version-1", LatestSubmittedVersionID: test.latestID,
				EligibilityState: test.state, EligibilityClaimType: test.wantClaim,
				OfferValidUntil: test.validUntil, EvaluatedAt: now,
			})
			if projection.Status != test.wantStatus ||
				projection.PendingClaimType != test.wantClaim {
				t.Errorf("projection = %+v, want status=%q claim=%q",
					projection, test.wantStatus, test.wantClaim)
			}
		})
	}
}
