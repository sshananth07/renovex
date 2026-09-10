package supplieroffers_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func newOfferDraftRepository(
	t *testing.T,
	db *mongo.Database,
) *supplieroffers.MongoOfferDraftRepository {
	t.Helper()

	repository := supplieroffers.NewMongoOfferDraftRepository(db)
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring offer-draft indexes: %v", err)
	}
	return repository
}

func TestOfferDraftIndexAllowsOnlyOneUnfinishedDraftPerChain(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	active := supplieroffers.SupplierOfferDraft{
		ID:                 "draft-active",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "buyer@example.test",
		Status:             supplieroffers.DraftActive,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
		SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
	}
	if err := repository.InsertDraft(ctx, active); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}

	// A submitting claim remains unfinished. Allowing this insert would let a
	// replacement draft race the immutable-version recovery still in progress.
	submitting := active
	submitting.ID = "draft-submitting"
	submitting.Status = supplieroffers.DraftSubmitting
	if err := repository.InsertDraft(ctx, submitting); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("inserting a second unfinished draft error = %v, want duplicate key", err)
	}

	archived := active
	archived.ID = "draft-archived"
	archived.Status = supplieroffers.DraftArchived
	archivedAt := now.Add(time.Hour)
	archived.ArchivedAt = &archivedAt
	if err := repository.InsertDraft(ctx, archived); err != nil {
		t.Fatalf("archived history should not consume the unfinished-draft slot: %v", err)
	}
}

func TestOfferDraftUnfinishedIndexIsCompanyScoped(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	for _, companyID := range []string{"company-1", "company-2"} {
		draft := supplieroffers.SupplierOfferDraft{
			ID:                 "draft-" + companyID,
			CompanyID:          companyID,
			OfferChainID:       "shared-chain-value",
			InvitationID:       "invitation-1",
			IssuedRFQVersionID: "issued-rfq-version-1",
			RecipientIdentity:  "buyer@example.test",
			Status:             supplieroffers.DraftActive,
			Revision:           1,
			CreatedAt:          now,
			UpdatedAt:          now,
			SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
		}
		if err := repository.InsertDraft(ctx, draft); err != nil {
			t.Fatalf("inserting %s draft: %v", companyID, err)
		}
	}
}

func TestOfferDraftRoundTripsCommercialAndSubmissionClaimState(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	validUntil := now.Add(72 * time.Hour)
	copiedAt := now.Add(-time.Hour)
	claimedAt := now.Add(time.Minute)
	quotedQuantity, err := quantity.New("12.500", "m2")
	if err != nil {
		t.Fatalf("constructing quantity: %v", err)
	}
	rate := money.RateBPS(600)
	threshold := money.New(50000, supplieroffers.Phase1Currency)
	fixedAmount := money.New(15000, supplieroffers.Phase1Currency)

	draft := supplieroffers.SupplierOfferDraft{
		ID:                   "draft-round-trip",
		CompanyID:            "company-1",
		OfferChainID:         "chain-1",
		InvitationID:         "invitation-1",
		IssuedRFQVersionID:   "issued-rfq-version-1",
		RecipientIdentity:    "buyer@example.test",
		SourceOfferVersionID: stringPointer("source-version-1"),
		Currency:             supplieroffers.Phase1Currency,
		Status:               supplieroffers.DraftSubmitting,
		Lines: []supplieroffers.SupplierOfferDraftLine{
			{
				ID:                       "draft-line-1",
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
				LineTax:                  &supplieroffers.QuotedLineTax{TaxType: supplieroffers.TaxTypeSalesTax, RateBPS: &rate, RegistrationNumber: "TAX-1"},
				ReviewRequired:           true,
				CopiedFromOfferVersionID: "source-version-1",
				CopiedFromOfferLineID:    "source-line-1",
				CopiedAt:                 &copiedAt,
			},
		},
		Tax:                    supplieroffers.SupplierOfferTax{Mode: supplieroffers.TaxModeLineLevel},
		OfferTaxReviewRequired: false,
		ChargeGroups: []supplieroffers.SupplierChargeGroupDraft{
			{
				ConditionalChargeGroup: supplieroffers.ConditionalChargeGroup{
					ID:                   "draft-group-1",
					Name:                 "Handling",
					Description:          "Applied when the threshold is met.",
					ApplicableRFQLineIDs: []string{"rfq-line-1"},
					Trigger:              supplieroffers.ChargeTriggerSelectedSubtotalAtLeast,
					Threshold:            &threshold,
					Calculation:          supplieroffers.ChargeCalculationFixedAmount,
					FixedAmount:          &fixedAmount,
				},
				CopiedFromChargeGroupID: "source-group-1",
				ReviewRequired:          true,
			},
		},
		DeliveryCharge:               &supplieroffers.DeliveryCharge{Amount: money.New(2500, supplieroffers.Phase1Currency)},
		DeliveryChargeReviewRequired: true,
		OfferValidUntil:              &validUntil,
		SupplierNotes:                "Whole-offer note.",
		SubmissionOperationID:        "submit-operation-1",
		SubmissionBaseRevision:       7,
		SubmissionFingerprint:        "fingerprint-1",
		SubmissionRecipientIdentity:  "buyer@example.test",
		SubmissionInvitationID:       "invitation-1",
		SubmissionRFQVersionID:       "issued-rfq-version-1",
		SubmissionAccessGeneration:   3,
		CandidateOfferVersionID:      "candidate-version-1",
		CandidateVersionNumber:       2,
		SubmissionClaimedAt:          &claimedAt,
		Revision:                     8,
		CreatedAt:                    now.Add(-2 * time.Hour),
		UpdatedAt:                    claimedAt,
		SchemaVersion:                supplieroffers.SupplierOfferDraftSchemaVersion,
	}

	if err := repository.InsertDraft(ctx, draft); err != nil {
		t.Fatalf("inserting full draft: %v", err)
	}
	loaded, found, err := repository.FindDraft(ctx, "company-1", draft.ID)
	if err != nil {
		t.Fatalf("finding full draft: %v", err)
	}
	if !found {
		t.Fatal("inserted draft was not found")
	}
	if loaded.Lines[0].QuotedQuantity == nil ||
		!loaded.Lines[0].QuotedQuantity.Value.Equal(quotedQuantity.Value) ||
		loaded.Lines[0].QuotedQuantity.Unit != "m2" {
		t.Fatalf(
			"quoted quantity = %#v, want numeric value 12.500 m2",
			loaded.Lines[0].QuotedQuantity,
		)
	}
	// Decimal scale is not commercial data: 12.500 and canonical 12.5 are the
	// same exact quantity. Compare that field semantically above, then exclude
	// shopspring/decimal's private scale metadata from the aggregate comparison.
	loaded.Lines[0].QuotedQuantity = draft.Lines[0].QuotedQuantity
	if !reflect.DeepEqual(loaded, draft) {
		t.Fatalf("draft round trip mismatch:\n got: %#v\nwant: %#v", loaded, draft)
	}

	_, found, err = repository.FindDraft(ctx, "company-2", draft.ID)
	if err != nil {
		t.Fatalf("foreign-tenant lookup: %v", err)
	}
	if found {
		t.Fatal("foreign tenant resolved another company's draft")
	}
}

func stringPointer(value string) *string {
	return &value
}

func moneyPointer(value money.Money) *money.Money {
	return &value
}

func TestReplaceActiveDraftCommercialStateRequiresCurrentRevisionAndActiveStatus(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 13, 0, 0, 0, time.UTC)

	base := supplieroffers.SupplierOfferDraft{
		ID:                 "draft-cas",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "buyer@example.test",
		Currency:           supplieroffers.Phase1Currency,
		Status:             supplieroffers.DraftActive,
		Lines: []supplieroffers.SupplierOfferDraftLine{{
			ID:             "draft-line-1",
			RFQLineID:      "rfq-line-1",
			ResponseStatus: supplieroffers.OfferLineUnanswered,
			ReviewRequired: true,
		}},
		Tax:           supplieroffers.SupplierOfferTax{Mode: supplieroffers.TaxModeNotApplicable},
		Revision:      3,
		CreatedAt:     now.Add(-time.Hour),
		UpdatedAt:     now.Add(-time.Hour),
		SchemaVersion: supplieroffers.SupplierOfferDraftSchemaVersion,
	}
	if err := repository.InsertDraft(ctx, base); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}

	state := supplieroffers.SupplierOfferDraftCommercialState{
		Lines: []supplieroffers.SupplierOfferDraftLine{{
			ID:             "draft-line-1",
			RFQLineID:      "rfq-line-1",
			ResponseStatus: supplieroffers.OfferLineNoBid,
			ReviewRequired: true,
		}},
		Tax:           supplieroffers.SupplierOfferTax{Mode: supplieroffers.TaxModeNotApplicable},
		SupplierNotes: "Updated note.",
	}
	updated, err := repository.ReplaceActiveCommercialState(
		ctx,
		"company-1",
		base.ID,
		3,
		state,
		now,
	)
	if err != nil {
		t.Fatalf("replacing active draft state: %v", err)
	}
	if updated.Revision != 4 || updated.SupplierNotes != "Updated note." {
		t.Fatalf("updated draft = %#v, want revision 4 and updated note", updated)
	}
	if !updated.Lines[0].ReviewRequired {
		t.Fatal("trusted server-owned review flag was not persisted")
	}

	_, err = repository.ReplaceActiveCommercialState(
		ctx,
		"company-1",
		base.ID,
		3,
		state,
		now.Add(time.Minute),
	)
	if !errors.Is(err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("stale revision error = %v, want ErrOfferDraftConflict", err)
	}

	_, err = repository.ReplaceActiveCommercialState(
		ctx,
		"company-2",
		base.ID,
		4,
		state,
		now.Add(time.Minute),
	)
	if !errors.Is(err, supplieroffers.ErrOfferDraftNotFound) {
		t.Fatalf("foreign tenant error = %v, want ErrOfferDraftNotFound", err)
	}
}

func TestReplaceActiveDraftCommercialStateRejectsSubmittingAndArchivedDrafts(t *testing.T) {
	tests := []supplieroffers.DraftStatus{
		supplieroffers.DraftSubmitting,
		supplieroffers.DraftArchived,
	}

	for _, status := range tests {
		t.Run(string(status), func(t *testing.T) {
			db := setupDB(t)
			repository := newOfferDraftRepository(t, db)
			ctx := context.Background()
			now := time.Date(2026, time.July, 31, 13, 0, 0, 0, time.UTC)
			draft := supplieroffers.SupplierOfferDraft{
				ID:                 "draft-" + string(status),
				CompanyID:          "company-1",
				OfferChainID:       "chain-1",
				InvitationID:       "invitation-1",
				IssuedRFQVersionID: "issued-rfq-version-1",
				RecipientIdentity:  "buyer@example.test",
				Status:             status,
				Revision:           5,
				CreatedAt:          now,
				UpdatedAt:          now,
				SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
			}
			if status == supplieroffers.DraftArchived {
				draft.ArchivedAt = &now
			}
			if err := repository.InsertDraft(ctx, draft); err != nil {
				t.Fatalf("inserting %s draft: %v", status, err)
			}

			_, err := repository.ReplaceActiveCommercialState(
				ctx,
				"company-1",
				draft.ID,
				5,
				supplieroffers.SupplierOfferDraftCommercialState{},
				now.Add(time.Minute),
			)
			if !errors.Is(err, supplieroffers.ErrOfferDraftConflict) {
				t.Fatalf("%s mutation error = %v, want ErrOfferDraftConflict", status, err)
			}
		})
	}
}

func TestAcknowledgeActiveDraftLineClearsOnlyTheApplicableFlag(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 14, 0, 0, 0, time.UTC)
	draft := supplieroffers.SupplierOfferDraft{
		ID:                 "draft-acknowledge",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "buyer@example.test",
		Status:             supplieroffers.DraftActive,
		Lines: []supplieroffers.SupplierOfferDraftLine{
			{
				ID:             "draft-line-quoted",
				RFQLineID:      "rfq-line-quoted",
				ResponseStatus: supplieroffers.OfferLineQuoted,
				ReviewRequired: true,
			},
			{
				ID:                   "draft-line-declined",
				RFQLineID:            "rfq-line-declined",
				ResponseStatus:       supplieroffers.OfferLineNoBid,
				ConfirmationRequired: true,
			},
		},
		Tax:           supplieroffers.SupplierOfferTax{Mode: supplieroffers.TaxModeNotApplicable},
		Revision:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
		SchemaVersion: supplieroffers.SupplierOfferDraftSchemaVersion,
	}
	if err := repository.InsertDraft(ctx, draft); err != nil {
		t.Fatalf("inserting draft: %v", err)
	}

	quotedAcknowledged, err := repository.AcknowledgeActiveDraftLine(
		ctx,
		"company-1",
		draft.ID,
		1,
		"draft-line-quoted",
		supplieroffers.OfferLineQuoted,
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("acknowledging quoted line: %v", err)
	}
	if quotedAcknowledged.Revision != 2 ||
		quotedAcknowledged.Lines[0].ReviewRequired {
		t.Fatalf("quoted acknowledgement = %#v", quotedAcknowledged)
	}
	if !quotedAcknowledged.Lines[1].ConfirmationRequired {
		t.Fatal("acknowledging one line cleared another line's confirmation")
	}

	declineAcknowledged, err := repository.AcknowledgeActiveDraftLine(
		ctx,
		"company-1",
		draft.ID,
		2,
		"draft-line-declined",
		supplieroffers.OfferLineNoBid,
		now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("confirming declined line: %v", err)
	}
	if declineAcknowledged.Revision != 3 ||
		declineAcknowledged.Lines[1].ConfirmationRequired {
		t.Fatalf("decline acknowledgement = %#v", declineAcknowledged)
	}
}

func TestAcknowledgeActiveDraftLineRequiresExactCurrentResponse(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 14, 0, 0, 0, time.UTC)
	draft := supplieroffers.SupplierOfferDraft{
		ID:                 "draft-acknowledge-mismatch",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "buyer@example.test",
		Status:             supplieroffers.DraftActive,
		Lines: []supplieroffers.SupplierOfferDraftLine{{
			ID:             "draft-line-1",
			RFQLineID:      "rfq-line-1",
			ResponseStatus: supplieroffers.OfferLineQuoted,
			ReviewRequired: true,
		}},
		Revision:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
		SchemaVersion: supplieroffers.SupplierOfferDraftSchemaVersion,
	}
	if err := repository.InsertDraft(ctx, draft); err != nil {
		t.Fatalf("inserting draft: %v", err)
	}

	_, err := repository.AcknowledgeActiveDraftLine(
		ctx,
		"company-1",
		draft.ID,
		1,
		"draft-line-1",
		supplieroffers.OfferLineNoBid,
		now.Add(time.Minute),
	)
	if !errors.Is(err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("response mismatch error = %v, want ErrOfferDraftConflict", err)
	}

	loaded, found, err := repository.FindDraft(ctx, "company-1", draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading unchanged draft: found=%v error=%v", found, err)
	}
	if !loaded.Lines[0].ReviewRequired || loaded.Revision != 1 {
		t.Fatalf("mismatched acknowledgement mutated draft: %#v", loaded)
	}
}

func TestConcurrentEnsureUnfinishedDraftConvergesOnOneDraft(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 15, 0, 0, 0, time.UTC)

	const callers = 8
	results := make([]supplieroffers.SupplierOfferDraft, callers)
	created := make([]bool, callers)
	errs := make([]error, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)

	for caller := 0; caller < callers; caller++ {
		go func(caller int) {
			defer waitGroup.Done()
			candidate := supplieroffers.SupplierOfferDraft{
				ID:                 "candidate-draft-" + string(rune('a'+caller)),
				CompanyID:          "company-1",
				OfferChainID:       "chain-1",
				InvitationID:       "invitation-1",
				IssuedRFQVersionID: "issued-rfq-version-1",
				RecipientIdentity:  "buyer@example.test",
				Currency:           supplieroffers.Phase1Currency,
				Status:             supplieroffers.DraftActive,
				Tax:                supplieroffers.SupplierOfferTax{Mode: supplieroffers.TaxModeNotApplicable},
				Revision:           1,
				CreatedAt:          now,
				UpdatedAt:          now,
				SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
			}
			results[caller], created[caller], errs[caller] =
				repository.EnsureUnfinishedDraft(ctx, candidate)
		}(caller)
	}
	waitGroup.Wait()

	winners := 0
	var persistedID string
	for caller := 0; caller < callers; caller++ {
		if errs[caller] != nil {
			t.Fatalf("caller %d: %v", caller, errs[caller])
		}
		if created[caller] {
			winners++
		}
		if caller == 0 {
			persistedID = results[caller].ID
		}
		if results[caller].ID != persistedID {
			t.Fatalf(
				"caller %d resolved draft %q, want every caller to resolve %q",
				caller,
				results[caller].ID,
				persistedID,
			)
		}
	}
	if winners != 1 {
		t.Fatalf("creation winners = %d, want exactly 1", winners)
	}
}
