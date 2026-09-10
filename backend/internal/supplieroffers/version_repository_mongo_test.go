package supplieroffers_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func newOfferVersionRepository(
	t *testing.T,
	db *mongo.Database,
) *supplieroffers.MongoOfferVersionRepository {
	t.Helper()

	repository := supplieroffers.NewMongoOfferVersionRepository(db)
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring offer-version indexes: %v", err)
	}
	return repository
}

func offerVersionFixture() supplieroffers.SupplierOfferVersion {
	submittedAt := time.Date(2026, time.July, 31, 11, 0, 0, 0, time.UTC)
	return supplieroffers.SupplierOfferVersion{
		ID:                    "candidate-version-1",
		CompanyID:             "company-1",
		OfferChainID:          "chain-1",
		InvitationID:          "invitation-1",
		IssuedRFQVersionID:    "issued-rfq-version-1",
		VersionNumber:         1,
		RecipientIdentity:     "buyer@example.test",
		SourceDraftID:         "draft-1",
		SourceDraftRevision:   4,
		SubmissionOperationID: "submit-operation-1",
		SubmissionFingerprint: "fingerprint-1",
		SubmittedAt:           submittedAt,
		SchemaVersion:         supplieroffers.SupplierOfferVersionSchemaVersion,
	}
}

func TestOfferVersionIndexRejectsDuplicateChainVersion(t *testing.T) {
	db := setupDB(t)
	repository := newOfferVersionRepository(t, db)
	ctx := context.Background()

	first := offerVersionFixture()
	if err := repository.InsertVersion(ctx, first); err != nil {
		t.Fatalf("inserting first version: %v", err)
	}

	duplicate := first
	duplicate.ID = "candidate-version-2"
	duplicate.SubmissionOperationID = "submit-operation-2"
	if err := repository.InsertVersion(ctx, duplicate); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("duplicate chain version error = %v, want duplicate key", err)
	}
}

func TestOfferVersionIndexRejectsReusedCompanySubmissionOperation(t *testing.T) {
	db := setupDB(t)
	repository := newOfferVersionRepository(t, db)
	ctx := context.Background()

	first := offerVersionFixture()
	if err := repository.InsertVersion(ctx, first); err != nil {
		t.Fatalf("inserting first version: %v", err)
	}

	reusedOperation := first
	reusedOperation.ID = "candidate-version-2"
	reusedOperation.OfferChainID = "chain-2"
	reusedOperation.VersionNumber = 2
	if err := repository.InsertVersion(ctx, reusedOperation); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("reused submission operation error = %v, want duplicate key", err)
	}
}

func TestOfferVersionIndexRejectsReusedCandidateIDAcrossCompanies(t *testing.T) {
	db := setupDB(t)
	repository := newOfferVersionRepository(t, db)
	ctx := context.Background()

	first := offerVersionFixture()
	if err := repository.InsertVersion(ctx, first); err != nil {
		t.Fatalf("inserting first version: %v", err)
	}

	reusedCandidate := first
	reusedCandidate.CompanyID = "company-2"
	reusedCandidate.OfferChainID = "chain-2"
	reusedCandidate.SubmissionOperationID = "submit-operation-2"
	if err := repository.InsertVersion(ctx, reusedCandidate); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("reused candidate ID error = %v, want duplicate key", err)
	}
}

func TestOfferVersionRoundTripsImmutableCommercialSnapshot(t *testing.T) {
	db := setupDB(t)
	repository := newOfferVersionRepository(t, db)
	ctx := context.Background()
	submittedAt := time.Date(2026, time.July, 31, 11, 0, 0, 0, time.UTC)
	validUntil := submittedAt.Add(72 * time.Hour)
	copiedAt := submittedAt.Add(-24 * time.Hour)
	quotedQuantity, err := quantity.New("12.500", "m2")
	if err != nil {
		t.Fatalf("constructing quantity: %v", err)
	}
	rate := money.RateBPS(250)

	version := supplieroffers.SupplierOfferVersion{
		ID:                    "candidate-version-round-trip",
		CompanyID:             "company-1",
		OfferChainID:          "chain-1",
		InvitationID:          "invitation-1",
		IssuedRFQVersionID:    "issued-rfq-version-1",
		VersionNumber:         2,
		Currency:              supplieroffers.Phase1Currency,
		RecipientIdentity:     "buyer@example.test",
		SourceDraftID:         "draft-1",
		SourceDraftRevision:   7,
		SubmissionOperationID: "submit-operation-round-trip",
		SubmissionFingerprint: "fingerprint-round-trip",
		Lines: []supplieroffers.SupplierOfferLine{
			{
				ID:                       "offer-line-1",
				RFQLineID:                "rfq-line-1",
				ResponseStatus:           supplieroffers.OfferLineQuoted,
				QuotedQuantity:           &quotedQuantity,
				UnitPriceExcludingTax:    moneyPointer(money.New(12345, supplieroffers.Phase1Currency)),
				LineSubtotalExcludingTax: moneyPointer(money.New(154313, supplieroffers.Phase1Currency)),
				Brand:                    "Example Brand",
				SKU:                      "SKU-1",
				ProductDescription:       "Commercial product description",
				LeadTime:                 "14 days",
				SupplierLineNotes:        "Deliver on weekdays.",
				CommercialExceptions:     "Subject to site access.",
				LineTaxAmount:            money.New(0, supplieroffers.Phase1Currency),
				CopiedFromOfferVersionID: "source-version-1",
				CopiedFromOfferLineID:    "source-line-1",
				CopiedAt:                 &copiedAt,
			},
		},
		Tax: supplieroffers.SupplierOfferTax{
			Mode: supplieroffers.TaxModeOfferLevel,
			OfferLevel: &supplieroffers.QuotedOfferTax{
				TaxType:            supplieroffers.TaxTypeSalesTax,
				TaxAmount:          money.New(9000, supplieroffers.Phase1Currency),
				BasisNote:          "Supplier-calculated whole-offer tax.",
				RegistrationNumber: "TAX-1",
			},
		},
		ChargeGroups: []supplieroffers.ConditionalChargeGroup{
			{
				ID:                   "group-1",
				Name:                 "Handling",
				ApplicableRFQLineIDs: []string{"rfq-line-1"},
				Trigger:              supplieroffers.ChargeTriggerAnySelected,
				Calculation:          supplieroffers.ChargeCalculationPercentageOfSelectedSubtotal,
				RateBPS:              &rate,
			},
		},
		DeliveryCharge:       &supplieroffers.DeliveryCharge{Amount: money.New(2500, supplieroffers.Phase1Currency)},
		QuotedLineSubtotal:   money.New(154313, supplieroffers.Phase1Currency),
		QuotedTaxTotal:       money.New(9000, supplieroffers.Phase1Currency),
		FullOfferChargeTotal: money.New(3858, supplieroffers.Phase1Currency),
		DeliveryChargeTotal:  money.New(2500, supplieroffers.Phase1Currency),
		GrandTotal:           money.New(169671, supplieroffers.Phase1Currency),
		OfferValidUntil:      validUntil,
		SupplierNotes:        "Whole-offer note.",
		SubmittedAt:          submittedAt,
		SchemaVersion:        supplieroffers.SupplierOfferVersionSchemaVersion,
	}

	if err := repository.InsertVersion(ctx, version); err != nil {
		t.Fatalf("inserting full immutable version: %v", err)
	}
	loaded, found, err := repository.FindVersion(ctx, "company-1", version.ID)
	if err != nil {
		t.Fatalf("finding full immutable version: %v", err)
	}
	if !found {
		t.Fatal("inserted immutable version was not found")
	}
	if loaded.Lines[0].QuotedQuantity == nil ||
		!loaded.Lines[0].QuotedQuantity.Value.Equal(quotedQuantity.Value) ||
		loaded.Lines[0].QuotedQuantity.Unit != "m2" {
		t.Fatalf("quoted quantity = %#v, want numeric value 12.500 m2", loaded.Lines[0].QuotedQuantity)
	}
	loaded.Lines[0].QuotedQuantity = version.Lines[0].QuotedQuantity
	if !reflect.DeepEqual(loaded, version) {
		t.Fatalf("version round trip mismatch:\n got: %#v\nwant: %#v", loaded, version)
	}

	_, found, err = repository.FindVersion(ctx, "company-2", version.ID)
	if err != nil {
		t.Fatalf("foreign-tenant lookup: %v", err)
	}
	if found {
		t.Fatal("foreign tenant resolved another company's immutable offer version")
	}
}
