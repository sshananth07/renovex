package supplieroffers

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

func TestCreateOrGetActiveDraftDerivesIdentityAndAuthoritativeLines(t *testing.T) {
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
	firstQuantity, _ := quantity.New("12.5", "m2")
	secondQuantity, _ := quantity.New("3", "unit")
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
				{ID: "rfq-line-1", LineageID: "lineage-1", Quantity: firstQuantity},
				{ID: "rfq-line-2", LineageID: "lineage-2", Quantity: secondQuantity},
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
		SessionToken: "session-token",
		InvitationID: "invitation-1",
		CSRFCookie:   "csrf",
		CSRFHeader:   "csrf",
		AccessedAt:   now,
	}

	first, err := service.CreateOrGetActiveDraft(ctx, input)
	if err != nil {
		t.Fatalf("creating active draft: %v", err)
	}
	if first.ID == "" || first.OfferChainID == "" {
		t.Fatalf("created draft identity = %#v", first)
	}
	if first.CompanyID != "company-1" ||
		first.InvitationID != "invitation-1" ||
		first.IssuedRFQVersionID != "issued-version-2" ||
		first.RecipientIdentity != "sales@supplier.test" {
		t.Fatalf("created draft used untrusted identity: %#v", first)
	}
	if first.Status != DraftActive || first.Revision != 1 {
		t.Fatalf("new draft lifecycle = status %q revision %d", first.Status, first.Revision)
	}
	if len(first.Lines) != 2 {
		t.Fatalf("draft lines = %d, want one per authoritative RFQ line", len(first.Lines))
	}
	for lineIndex, rfqLineID := range []string{"rfq-line-1", "rfq-line-2"} {
		line := first.Lines[lineIndex]
		if line.ID == "" || line.RFQLineID != rfqLineID ||
			line.ResponseStatus != OfferLineUnanswered {
			t.Fatalf("draft line %d = %#v", lineIndex, line)
		}
	}

	retried, err := service.CreateOrGetActiveDraft(ctx, input)
	if err != nil {
		t.Fatalf("retrying active draft creation: %v", err)
	}
	if retried.ID != first.ID {
		t.Fatalf("creation retry returned draft %q, want %q", retried.ID, first.ID)
	}
}
