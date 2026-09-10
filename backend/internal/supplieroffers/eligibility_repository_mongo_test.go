package supplieroffers_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func newOfferEligibilityRepository(
	t *testing.T,
	db *mongo.Database,
) *supplieroffers.MongoOfferEligibilityRepository {
	t.Helper()

	repository := supplieroffers.NewMongoOfferEligibilityRepository(db)
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring offer-eligibility indexes: %v", err)
	}
	return repository
}

func TestOfferEligibilityIndexAllowsOneGatePerCompanyVersion(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()

	first := supplieroffers.SupplierOfferEligibility{
		ID:             "eligibility-1",
		CompanyID:      "company-1",
		OfferChainID:   "chain-1",
		OfferVersionID: "version-1",
		State:          supplieroffers.EligibilityEligible,
		Revision:       1,
	}
	if err := repository.InsertEligibility(ctx, first); err != nil {
		t.Fatalf("inserting first eligibility gate: %v", err)
	}

	duplicate := first
	duplicate.ID = "eligibility-2"
	if err := repository.InsertEligibility(ctx, duplicate); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("duplicate eligibility gate error = %v, want duplicate key", err)
	}

	otherCompany := first
	otherCompany.ID = "eligibility-3"
	otherCompany.CompanyID = "company-2"
	if err := repository.InsertEligibility(ctx, otherCompany); err != nil {
		t.Fatalf("same version value in another company should be isolated: %v", err)
	}
}

func TestOfferEligibilityRoundTripsRecoverableWithdrawalClaim(t *testing.T) {
	db := setupDB(t)
	repository := newOfferEligibilityRepository(t, db)
	ctx := context.Background()
	claimedAt := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)

	eligibility := supplieroffers.SupplierOfferEligibility{
		ID:               "eligibility-claimed",
		CompanyID:        "company-1",
		OfferChainID:     "chain-1",
		OfferVersionID:   "version-1",
		State:            supplieroffers.EligibilityWithdrawalClaimed,
		ClaimType:        supplieroffers.EligibilityClaimWithdrawal,
		OperationID:      "withdraw-operation-1",
		ClaimID:          "withdrawal-1",
		WithdrawalReason: "Commercial terms changed.",
		Revision:         2,
		ClaimedAt:        &claimedAt,
	}
	if err := repository.InsertEligibility(ctx, eligibility); err != nil {
		t.Fatalf("inserting claimed eligibility: %v", err)
	}

	loaded, found, err := repository.FindEligibility(ctx, "company-1", eligibility.OfferVersionID)
	if err != nil {
		t.Fatalf("finding claimed eligibility: %v", err)
	}
	if !found {
		t.Fatal("inserted eligibility was not found")
	}
	if !reflect.DeepEqual(loaded, eligibility) {
		t.Fatalf("eligibility round trip mismatch:\n got: %#v\nwant: %#v", loaded, eligibility)
	}

	_, found, err = repository.FindEligibility(ctx, "company-2", eligibility.OfferVersionID)
	if err != nil {
		t.Fatalf("foreign-tenant lookup: %v", err)
	}
	if found {
		t.Fatal("foreign tenant resolved another company's eligibility gate")
	}
}
