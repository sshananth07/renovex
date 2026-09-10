package supplieroffers_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func newOfferWithdrawalRepository(
	t *testing.T,
	db *mongo.Database,
) *supplieroffers.MongoOfferWithdrawalRepository {
	t.Helper()

	repository := supplieroffers.NewMongoOfferWithdrawalRepository(db)
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring offer-withdrawal indexes: %v", err)
	}
	return repository
}

func offerWithdrawalFixture() supplieroffers.SupplierOfferWithdrawal {
	return supplieroffers.SupplierOfferWithdrawal{
		ID:                     "withdrawal-1",
		CompanyID:              "company-1",
		OfferChainID:           "chain-1",
		SupplierOfferVersionID: "version-1",
		InvitationID:           "invitation-1",
		RecipientIdentity:      "buyer@example.test",
		OperationID:            "withdraw-operation-1",
		Reason:                 "Commercial terms changed.",
		WithdrawnAt:            time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC),
		SchemaVersion:          supplieroffers.SupplierOfferWithdrawalSchemaVersion,
	}
}

func TestOfferWithdrawalIndexAllowsOneRecordPerCompanyVersion(t *testing.T) {
	db := setupDB(t)
	repository := newOfferWithdrawalRepository(t, db)
	ctx := context.Background()

	first := offerWithdrawalFixture()
	if err := repository.InsertWithdrawal(ctx, first); err != nil {
		t.Fatalf("inserting first withdrawal: %v", err)
	}

	duplicate := first
	duplicate.ID = "withdrawal-2"
	duplicate.OperationID = "withdraw-operation-2"
	if err := repository.InsertWithdrawal(ctx, duplicate); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("duplicate version withdrawal error = %v, want duplicate key", err)
	}
}

func TestOfferWithdrawalIndexRejectsReusedCompanyOperation(t *testing.T) {
	db := setupDB(t)
	repository := newOfferWithdrawalRepository(t, db)
	ctx := context.Background()

	first := offerWithdrawalFixture()
	if err := repository.InsertWithdrawal(ctx, first); err != nil {
		t.Fatalf("inserting first withdrawal: %v", err)
	}

	reusedOperation := first
	reusedOperation.ID = "withdrawal-2"
	reusedOperation.SupplierOfferVersionID = "version-2"
	if err := repository.InsertWithdrawal(ctx, reusedOperation); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("reused withdrawal operation error = %v, want duplicate key", err)
	}
}

func TestOfferWithdrawalRoundTripsExactClaimRecord(t *testing.T) {
	db := setupDB(t)
	repository := newOfferWithdrawalRepository(t, db)
	ctx := context.Background()
	withdrawal := offerWithdrawalFixture()

	if err := repository.InsertWithdrawal(ctx, withdrawal); err != nil {
		t.Fatalf("inserting withdrawal: %v", err)
	}
	loaded, found, err := repository.FindWithdrawal(
		ctx,
		"company-1",
		withdrawal.SupplierOfferVersionID,
	)
	if err != nil {
		t.Fatalf("finding withdrawal: %v", err)
	}
	if !found {
		t.Fatal("inserted withdrawal was not found")
	}
	if !reflect.DeepEqual(loaded, withdrawal) {
		t.Fatalf("withdrawal round trip mismatch:\n got: %#v\nwant: %#v", loaded, withdrawal)
	}

	_, found, err = repository.FindWithdrawal(
		ctx,
		"company-2",
		withdrawal.SupplierOfferVersionID,
	)
	if err != nil {
		t.Fatalf("foreign-tenant lookup: %v", err)
	}
	if found {
		t.Fatal("foreign tenant resolved another company's withdrawal")
	}
}
