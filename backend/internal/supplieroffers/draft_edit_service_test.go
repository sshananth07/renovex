package supplieroffers

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// draftEditRig builds an authorized service over real MongoDB with one active
// draft already created, which is the state every edit command starts from.
type draftEditRig struct {
	service      *Service
	drafts       *MongoOfferDraftRepository
	db           *mongo.Database
	lineQuantity quantity.Quantity
	input        SupplierOfferMutationContextInput
	draft        SupplierOfferDraft
	now          time.Time
}

func newDraftEditRig(t *testing.T) *draftEditRig {
	t.Helper()
	db := setupInternalDB(t)
	chains := NewMongoOfferChainRepository(db)
	drafts := NewMongoOfferDraftRepository(db)
	ctx := context.Background()
	if err := chains.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring chain indexes: %v", err)
	}
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring draft indexes: %v", err)
	}

	now := time.Date(2026, time.July, 31, 16, 0, 0, 0, time.UTC)
	lineQuantity, _ := quantity.New("10", "unit")
	access := &fakeOfferAccessAuthorizer{
		authorized: AuthorizedSupplierOfferAccess{
			SessionID: "session-1", CompanyID: "company-1", SupplierID: "supplier-1",
			RecipientIdentity: "sales@supplier.test", InvitationID: "invitation-1",
			AccessGeneration: 4, CurrentIssuedRFQVersionID: "issued-version-2",
			SessionCookieRenewal: SupplierSessionCookieRenewal{
				Token: "session-token", ExpiresAt: now.Add(24 * time.Hour),
			},
		},
	}
	issued := &fakeIssuedRFQSource{
		found: true,
		snapshot: IssuedRFQSnapshot{
			ID: "issued-version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
			Currency: Phase1Currency, ResponseDeadline: now.Add(time.Hour),
			Lines: []IssuedRFQLineSnapshot{
				{ID: "rfq-line-1", LineageID: "lineage-1", Quantity: lineQuantity},
			},
		},
	}
	service := NewService(
		WithSupplierOfferAccessAuthorizer(access),
		WithIssuedRFQSource(issued),
		WithSupplierOfferChainRepository(chains),
		WithSupplierOfferDraftRepository(drafts),
	)
	input := SupplierOfferMutationContextInput{
		SessionToken: "session-token", InvitationID: "invitation-1",
		CSRFCookie: "csrf", CSRFHeader: "csrf", AccessedAt: now,
	}
	draft, err := service.CreateOrGetActiveDraft(ctx, input)
	if err != nil {
		t.Fatalf("creating active draft: %v", err)
	}
	return &draftEditRig{service: service, drafts: drafts, db: db, input: input,
		lineQuantity: lineQuantity,
		draft:        draft, now: now}
}

// Reading a draft resolves the Company from the authorized session, never from
// caller input, so one Supplier can never read another tenant's offer.
func TestGetActiveDraftResolvesTenantFromAuthorization(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	found, err := rig.service.GetActiveDraft(ctx, SupplierOfferReadContextInput{
		SessionToken: "session-token", InvitationID: "invitation-1",
		AccessedAt: rig.now,
	})
	if err != nil {
		t.Fatalf("GetActiveDraft: %v", err)
	}
	if found.ID != rig.draft.ID || found.CompanyID != "company-1" {
		t.Fatalf("read draft = %s/%s, want the authorized draft %s/company-1",
			found.ID, found.CompanyID, rig.draft.ID)
	}
}

// Quoting a line sets the server-CALCULATED subtotal. The client supplies
// quantity and unit price only; it can never post an arbitrary total.
func TestQuoteDraftLineCalculatesTheSubtotal(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	quoted, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
		DraftLineID:      rig.draft.Lines[0].ID,
		UnitPriceMinor:   2_500,
		Brand:            "Acme",
	})
	if err != nil {
		t.Fatalf("QuoteDraftLine: %v", err)
	}

	line := quoted.Lines[0]
	if line.ResponseStatus != OfferLineQuoted {
		t.Errorf("ResponseStatus = %q, want quoted", line.ResponseStatus)
	}
	// Full issued quantity (10) at 2500 minor units = 25000.
	if line.LineSubtotalExcludingTax == nil ||
		*line.LineSubtotalExcludingTax != money.New(25_000, Phase1Currency) {
		t.Errorf("LineSubtotalExcludingTax = %v, want the server-calculated 25000",
			line.LineSubtotalExcludingTax)
	}
	if quoted.Revision != rig.draft.Revision+1 {
		t.Errorf("Revision = %d, want one increment", quoted.Revision)
	}
}

// A stale revision must not overwrite a concurrent edit.
func TestQuoteDraftLineRejectsAStaleRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	if _, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision + 99,
		DraftLineID:      rig.draft.Lines[0].ID,
		UnitPriceMinor:   2_500,
	}); !errors.Is(err, ErrOfferDraftConflict) {
		t.Fatalf("stale-revision quote error = %v, want conflict", err)
	}
}

// Declining a line clears the commercial values so a stale price can never
// survive on a line the Supplier is no longer quoting.
func TestDeclineDraftLineClearsCommercialValues(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	quoted, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
		DraftLineID:      rig.draft.Lines[0].ID,
		UnitPriceMinor:   2_500,
	})
	if err != nil {
		t.Fatalf("QuoteDraftLine: %v", err)
	}

	declined, err := rig.service.DeclineDraftLine(ctx, DeclineDraftLineCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: quoted.Revision,
		DraftLineID:      rig.draft.Lines[0].ID,
		ResponseStatus:   OfferLineNoBid,
	})
	if err != nil {
		t.Fatalf("DeclineDraftLine: %v", err)
	}

	line := declined.Lines[0]
	if line.ResponseStatus != OfferLineNoBid {
		t.Errorf("ResponseStatus = %q, want no_bid", line.ResponseStatus)
	}
	if line.UnitPriceExcludingTax != nil || line.LineSubtotalExcludingTax != nil ||
		line.QuotedQuantity != nil {
		t.Error("declining left commercial values behind; a stale price could " +
			"later be read as an active quote")
	}
}

// A decline must reject a status that is not a decline, so a generic command
// cannot smuggle a line back into "quoted" without pricing.
func TestDeclineDraftLineRejectsANonDeclineStatus(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	if _, err := rig.service.DeclineDraftLine(ctx, DeclineDraftLineCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
		DraftLineID:      rig.draft.Lines[0].ID,
		ResponseStatus:   OfferLineQuoted,
	}); !errors.Is(err, ErrInvalidQuotedLine) {
		t.Fatalf("error = %v, want an invalid-line rejection", err)
	}
}

// Edits are refused once the draft is no longer active. This is the same
// fail-closed boundary a recipient-replacement claim relies on.
func TestDraftEditsAreRefusedOnAClaimedDraft(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	if _, err := rig.drafts.ClaimRecipientReplacement(ctx,
		RecipientReplacementClaimInput{
			CompanyID:                  "company-1",
			OfferChainID:               rig.draft.OfferChainID,
			InvitationID:               "invitation-1",
			IssuedRFQVersionID:         "issued-version-2",
			PreviousRecipientIdentity:  "sales@supplier.test",
			CandidateRecipientIdentity: "next@supplier.test",
			ReplacementOperationID:     "op-replace-1",
			BarrierDraftID:             "barrier-1",
			ClaimedAt:                  rig.now,
		}); err != nil {
		t.Fatalf("claiming for replacement: %v", err)
	}

	if _, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision + 1,
		DraftLineID:      rig.draft.Lines[0].ID,
		UnitPriceMinor:   2_500,
	}); !errors.Is(err, ErrOfferDraftConflict) {
		t.Fatalf("editing a claimed draft error = %v, want conflict", err)
	}
}
