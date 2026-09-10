package composition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

type fakeSupplierAccessSource struct {
	read     supplieraccess.AuthorizedInvitationAccess
	mutation supplieraccess.AuthorizedInvitationAccess
	readErr  error
	mutErr   error
}

func (f *fakeSupplierAccessSource) AuthorizeInvitationAccess(
	_ context.Context, _ supplieraccess.AuthorizeInvitationAccessInput) (
	supplieraccess.AuthorizedInvitationAccess, error) {
	return f.read, f.readErr
}

func (f *fakeSupplierAccessSource) AuthorizeInvitationMutation(
	_ context.Context, _ supplieraccess.AuthorizeInvitationMutationInput) (
	supplieraccess.AuthorizedInvitationAccess, error) {
	return f.mutation, f.mutErr
}

// The adapter carries the authorized identity across intact: supplieroffers
// derives Company, Supplier and recipient ONLY from this result, so a dropped
// field would silently widen or break tenant scoping.
func TestSupplierOfferAccessAdapterCarriesAuthorizedIdentity(t *testing.T) {
	expiry := time.Now().UTC().Add(time.Hour)
	source := &fakeSupplierAccessSource{
		read: supplieraccess.AuthorizedInvitationAccess{
			SessionID: "session-1", CompanyID: "company-1", SupplierID: "supplier-1",
			NormalizedRecipientEmail: "sales@supplier.test",
			InvitationID:             "invitation-1",
			AccessGeneration:         4, CurrentIssuedRFQVersionID: "issued-2",
			SessionCookieRenewal: supplieraccess.SupplierSessionCookieRenewal{
				Token: "session-token", ExpiresAt: expiry,
			},
		},
	}
	adapter := composition.NewSupplierOfferAccessAdapter(source)

	var _ supplieroffers.SupplierOfferAccessAuthorizer = adapter

	authorized, err := adapter.AuthorizeSupplierOfferRead(context.Background(),
		supplieroffers.SupplierOfferReadAuthorization{
			SessionToken: "session-token", InvitationID: "invitation-1",
			AccessedAt: time.Now().UTC(),
		})
	if err != nil {
		t.Fatalf("AuthorizeSupplierOfferRead: %v", err)
	}

	if authorized.CompanyID != "company-1" ||
		authorized.SupplierID != "supplier-1" ||
		authorized.RecipientIdentity != "sales@supplier.test" ||
		authorized.InvitationID != "invitation-1" ||
		authorized.AccessGeneration != 4 ||
		authorized.CurrentIssuedRFQVersionID != "issued-2" {
		t.Errorf("authorized = %+v, want every identity carried across", authorized)
	}
	if authorized.SessionCookieRenewal.Token != "session-token" ||
		!authorized.SessionCookieRenewal.ExpiresAt.Equal(expiry) {
		t.Errorf("session renewal = %+v, want the renewed cookie carried across",
			authorized.SessionCookieRenewal)
	}
}

// An invalid credential must surface as the consumer's own bounded error, so
// supplieroffers never has to interpret a supplieraccess sentinel.
func TestSupplierOfferAccessAdapterMapsInvalidAccess(t *testing.T) {
	source := &fakeSupplierAccessSource{
		readErr: supplieraccess.ErrInvalidSupplierCredential,
	}
	adapter := composition.NewSupplierOfferAccessAdapter(source)

	_, err := adapter.AuthorizeSupplierOfferRead(context.Background(),
		supplieroffers.SupplierOfferReadAuthorization{
			SessionToken: "bad", InvitationID: "invitation-1",
			AccessedAt: time.Now().UTC(),
		})
	if !errors.Is(err, supplieroffers.ErrSupplierOfferAccessInvalid) {
		t.Fatalf("error = %v, want the consumer-owned invalid-access error", err)
	}
}

func TestSupplierRFQAccessAdapterCarriesAuthoritativeInvitationScope(t *testing.T) {
	source := &fakeSupplierAccessSource{read: supplieraccess.AuthorizedInvitationAccess{
		CompanyID: "company-1", SupplierID: "supplier-1",
		InvitationID: "invitation-1", AccessGeneration: 7,
	}}
	adapter := composition.NewSupplierRFQAccessAdapter(source)
	var _ rfqissuance.SupplierRFQAccessAuthorizer = adapter

	authorized, err := adapter.AuthorizeSupplierRFQRead(context.Background(),
		rfqissuance.SupplierRFQReadAuthorization{
			SessionToken: "session-token", InvitationID: "invitation-1",
			AccessedAt: time.Now().UTC(),
		})
	if err != nil {
		t.Fatalf("AuthorizeSupplierRFQRead: %v", err)
	}
	if authorized.CompanyID != "company-1" || authorized.SupplierID != "supplier-1" ||
		authorized.InvitationID != "invitation-1" || authorized.AccessGeneration != 7 {
		t.Errorf("authorized = %+v", authorized)
	}
}

func TestSupplierRFQAccessAdapterMapsInvalidAccess(t *testing.T) {
	adapter := composition.NewSupplierRFQAccessAdapter(&fakeSupplierAccessSource{
		readErr: supplieraccess.ErrInvalidSupplierCredential,
	})
	_, err := adapter.AuthorizeSupplierRFQRead(context.Background(),
		rfqissuance.SupplierRFQReadAuthorization{
			SessionToken: "bad", InvitationID: "invitation-1", AccessedAt: time.Now().UTC(),
		})
	if !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
		t.Fatalf("error = %v, want ErrSupplierRFQAccessInvalid", err)
	}
}

// CSRF is the one authenticated refusal that must remain distinguishable from
// an unusable/foreign session so the HTTP boundary can reserve 403 for it.
func TestSupplierOfferAccessAdapterPreservesCSRFRefusal(t *testing.T) {
	source := &fakeSupplierAccessSource{
		mutErr: supplieraccess.ErrSupplierCSRFRejected,
	}
	adapter := composition.NewSupplierOfferAccessAdapter(source)

	_, err := adapter.AuthorizeSupplierOfferMutation(context.Background(),
		supplieroffers.SupplierOfferMutationAuthorization{
			SupplierOfferReadAuthorization: supplieroffers.SupplierOfferReadAuthorization{
				SessionToken: "session", InvitationID: "invitation-1", AccessedAt: time.Now().UTC(),
			},
			CSRFCookie: "csrf", CSRFHeader: "csrf",
		})
	if !errors.Is(err, supplieroffers.ErrSupplierOfferCSRFRejected) {
		t.Fatalf("error = %v, want the consumer-owned CSRF refusal", err)
	}
}

type fakeIssuedVersionSource struct {
	version rfqissuance.IssuedRFQVersion
	found   bool
	err     error
}

func (f *fakeIssuedVersionSource) GetIssuedVersion(
	_ context.Context, _, _ string) (rfqissuance.IssuedRFQVersion, error) {
	if f.err != nil {
		return rfqissuance.IssuedRFQVersion{}, f.err
	}
	if !f.found {
		return rfqissuance.IssuedRFQVersion{}, rfqissuance.ErrIssuedVersionNotFound
	}
	return f.version, nil
}

// The issued-RFQ adapter must pass through the authoritative commercial lines
// a Supplier prices against, and nothing else.
func TestIssuedRFQOfferSourceAdapterProjectsAuthoritativeLines(t *testing.T) {
	source := &fakeIssuedVersionSource{
		found: true,
		version: rfqissuance.IssuedRFQVersion{
			ID: "issued-2", CompanyID: "company-1", RFQChainID: "chain-1",
			Currency: "MYR", ResponseDeadline: time.Now().UTC().Add(time.Hour),
		},
	}
	adapter := composition.NewIssuedRFQOfferSourceAdapter(source)

	var _ supplieroffers.IssuedRFQSource = adapter

	snapshot, found, err := adapter.GetIssuedRFQForOffer(
		context.Background(), "company-1", "issued-2")
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	if snapshot.ID != "issued-2" || snapshot.CompanyID != "company-1" ||
		snapshot.Currency != "MYR" {
		t.Errorf("snapshot = %+v, want the authoritative identity", snapshot)
	}
}

// A missing version reports found=false rather than an error, so the Supplier
// sees a clean not-found instead of an infrastructure failure.
func TestIssuedRFQOfferSourceAdapterReportsMissingVersion(t *testing.T) {
	adapter := composition.NewIssuedRFQOfferSourceAdapter(
		&fakeIssuedVersionSource{found: false})

	_, found, err := adapter.GetIssuedRFQForOffer(
		context.Background(), "company-1", "issued-missing")
	if err != nil {
		t.Fatalf("a missing version must not be an error, got %v", err)
	}
	if found {
		t.Error("found = true for a missing version")
	}
}
