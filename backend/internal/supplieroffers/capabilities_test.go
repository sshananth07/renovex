package supplieroffers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

type fakeOfferAccessAuthorizer struct {
	authorized AuthorizedSupplierOfferAccess
	err        error
	read       SupplierOfferReadAuthorization
	mutation   SupplierOfferMutationAuthorization
}

func (f *fakeOfferAccessAuthorizer) AuthorizeSupplierOfferRead(
	_ context.Context,
	input SupplierOfferReadAuthorization,
) (AuthorizedSupplierOfferAccess, error) {
	f.read = input
	return f.authorized, f.err
}

func (f *fakeOfferAccessAuthorizer) AuthorizeSupplierOfferMutation(
	_ context.Context,
	input SupplierOfferMutationAuthorization,
) (AuthorizedSupplierOfferAccess, error) {
	f.mutation = input
	return f.authorized, f.err
}

type fakeIssuedRFQSource struct {
	snapshot IssuedRFQSnapshot
	found    bool
	err      error
	company  string
	version  string
	calls    int
}

func (f *fakeIssuedRFQSource) GetIssuedRFQForOffer(
	_ context.Context,
	companyID string,
	versionID string,
) (IssuedRFQSnapshot, bool, error) {
	f.calls++
	f.company = companyID
	f.version = versionID
	return f.snapshot, f.found, f.err
}

func TestResolveMutationContextUsesOnlyAuthorizedTenantAndCurrentVersion(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	qty, _ := quantity.New("2", "unit")
	access := &fakeOfferAccessAuthorizer{
		authorized: AuthorizedSupplierOfferAccess{
			SessionID: "session-1", CompanyID: "company-1", SupplierID: "supplier-1",
			RecipientIdentity: "sales@supplier.test", InvitationID: "invitation-1",
			AccessGeneration: 4, CurrentIssuedRFQVersionID: "version-2",
			SessionCookieRenewal: SupplierSessionCookieRenewal{
				Token: "session-token", ExpiresAt: now.Add(24 * time.Hour),
			},
		},
	}
	issued := &fakeIssuedRFQSource{
		found: true,
		snapshot: IssuedRFQSnapshot{
			ID: "version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
			Currency: "MYR", ResponseDeadline: now.Add(time.Hour),
			Lines: []IssuedRFQLineSnapshot{{
				ID: "line-1", LineageID: "lineage-1", MaterialID: "material-1",
				MaterialName: "Cabinet", Quantity: qty,
			}},
		},
	}
	service := NewService(
		WithSupplierOfferAccessAuthorizer(access),
		WithIssuedRFQSource(issued),
	)

	resolved, err := service.ResolveMutationContext(
		context.Background(),
		SupplierOfferMutationContextInput{
			SessionToken: "session-token", InvitationID: "invitation-1",
			CSRFCookie: "csrf-cookie", CSRFHeader: "csrf-header", AccessedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("ResolveMutationContext returned an unexpected error: %v", err)
	}
	if access.mutation.SessionToken != "session-token" ||
		access.mutation.InvitationID != "invitation-1" ||
		access.mutation.CSRFCookie != "csrf-cookie" ||
		access.mutation.CSRFHeader != "csrf-header" ||
		!access.mutation.AccessedAt.Equal(now) {
		t.Fatalf("authorization input = %#v", access.mutation)
	}
	if issued.company != "company-1" || issued.version != "version-2" {
		t.Fatalf("issued RFQ lookup used %q/%q, want authorized company-1/version-2",
			issued.company, issued.version)
	}
	if resolved.Access.CompanyID != "company-1" ||
		resolved.Access.SupplierID != "supplier-1" ||
		resolved.RFQ.ID != "version-2" {
		t.Fatalf("resolved context = %#v", resolved)
	}
}

func TestResolveReadContextUsesReadAuthorizationAndCurrentVersion(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 30, 0, 0, time.UTC)
	access := &fakeOfferAccessAuthorizer{
		authorized: AuthorizedSupplierOfferAccess{
			SessionID: "session-1", CompanyID: "company-1", SupplierID: "supplier-1",
			RecipientIdentity: "sales@supplier.test", InvitationID: "invitation-1",
			AccessGeneration: 4, CurrentIssuedRFQVersionID: "version-2",
			SessionCookieRenewal: SupplierSessionCookieRenewal{
				Token: "session-token", ExpiresAt: now.Add(24 * time.Hour),
			},
		},
	}
	issued := &fakeIssuedRFQSource{
		found: true,
		snapshot: IssuedRFQSnapshot{
			ID: "version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
			Currency: "MYR", ResponseDeadline: now.Add(time.Hour),
		},
	}
	service := NewService(
		WithSupplierOfferAccessAuthorizer(access),
		WithIssuedRFQSource(issued),
	)

	resolved, err := service.ResolveReadContext(
		context.Background(),
		SupplierOfferReadContextInput{
			SessionToken: "session-token", InvitationID: "invitation-1", AccessedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("ResolveReadContext returned an unexpected error: %v", err)
	}
	if access.read.SessionToken != "session-token" ||
		access.read.InvitationID != "invitation-1" ||
		!access.read.AccessedAt.Equal(now) {
		t.Fatalf("read authorization input = %#v", access.read)
	}
	if issued.company != "company-1" || issued.version != "version-2" {
		t.Fatalf("issued RFQ lookup used %q/%q", issued.company, issued.version)
	}
	if resolved.Access.AccessGeneration != 4 || resolved.RFQ.ID != "version-2" {
		t.Fatalf("resolved context = %#v", resolved)
	}
}

func TestResolveMutationContextFailsClosedOnBoundaryIdentityMismatch(t *testing.T) {
	now := time.Date(2026, 7, 31, 13, 0, 0, 0, time.UTC)
	validAccess := AuthorizedSupplierOfferAccess{
		SessionID: "session-1", CompanyID: "company-1", SupplierID: "supplier-1",
		RecipientIdentity: "sales@supplier.test", InvitationID: "invitation-1",
		AccessGeneration: 4, CurrentIssuedRFQVersionID: "version-2",
		SessionCookieRenewal: SupplierSessionCookieRenewal{
			Token: "session-token", ExpiresAt: now.Add(24 * time.Hour),
		},
	}
	validRFQ := IssuedRFQSnapshot{
		ID: "version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
		Currency: "MYR", ResponseDeadline: now.Add(time.Hour),
	}
	tests := []struct {
		name       string
		access     AuthorizedSupplierOfferAccess
		rfq        IssuedRFQSnapshot
		wantLookup bool
	}{
		{
			name: "authorization invitation mismatch",
			access: func() AuthorizedSupplierOfferAccess {
				value := validAccess
				value.InvitationID = "invitation-foreign"
				return value
			}(),
			rfq: validRFQ,
		},
		{
			name: "renewal token mismatch",
			access: func() AuthorizedSupplierOfferAccess {
				value := validAccess
				value.SessionCookieRenewal.Token = "different-token"
				return value
			}(),
			rfq: validRFQ,
		},
		{
			name:   "foreign tenant rfq snapshot",
			access: validAccess,
			rfq: func() IssuedRFQSnapshot {
				value := validRFQ
				value.CompanyID = "company-foreign"
				return value
			}(),
			wantLookup: true,
		},
		{
			name:   "different rfq version snapshot",
			access: validAccess,
			rfq: func() IssuedRFQSnapshot {
				value := validRFQ
				value.ID = "version-foreign"
				return value
			}(),
			wantLookup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			access := &fakeOfferAccessAuthorizer{authorized: tt.access}
			issued := &fakeIssuedRFQSource{snapshot: tt.rfq, found: true}
			service := NewService(
				WithSupplierOfferAccessAuthorizer(access),
				WithIssuedRFQSource(issued),
			)

			_, err := service.ResolveMutationContext(
				context.Background(),
				SupplierOfferMutationContextInput{
					SessionToken: "session-token", InvitationID: "invitation-1",
					CSRFCookie: "csrf", CSRFHeader: "csrf", AccessedAt: now,
				},
			)
			if !errors.Is(err, ErrSupplierOfferAccessInvalid) {
				t.Fatalf("ResolveMutationContext error = %v, want access invalid", err)
			}
			if gotLookup := issued.calls > 0; gotLookup != tt.wantLookup {
				t.Fatalf("issued RFQ lookup occurred = %v, want %v", gotLookup, tt.wantLookup)
			}
		})
	}
}

func TestResolveMutationContextPreservesNeutralAndOperationalFailures(t *testing.T) {
	now := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)
	validAccess := AuthorizedSupplierOfferAccess{
		SessionID: "session-1", CompanyID: "company-1", SupplierID: "supplier-1",
		RecipientIdentity: "sales@supplier.test", InvitationID: "invitation-1",
		AccessGeneration: 4, CurrentIssuedRFQVersionID: "version-2",
		SessionCookieRenewal: SupplierSessionCookieRenewal{
			Token: "session-token", ExpiresAt: now.Add(24 * time.Hour),
		},
	}
	validRFQ := IssuedRFQSnapshot{
		ID: "version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
		Currency: "MYR", ResponseDeadline: now.Add(time.Hour),
	}
	operationalErr := errors.New("mongo unavailable")

	tests := []struct {
		name       string
		accessErr  error
		rfqErr     error
		found      bool
		want       error
		wantLookup bool
	}{
		{
			name:      "authorization failure propagates before rfq lookup",
			accessErr: operationalErr, found: true, want: operationalErr,
		},
		{
			name:   "rfq infrastructure failure propagates",
			rfqErr: operationalErr, found: true, want: operationalErr, wantLookup: true,
		},
		{
			name:  "missing or foreign rfq is one tenant-safe not found",
			found: false, want: ErrIssuedRFQNotFound, wantLookup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			access := &fakeOfferAccessAuthorizer{authorized: validAccess, err: tt.accessErr}
			issued := &fakeIssuedRFQSource{
				snapshot: validRFQ, found: tt.found, err: tt.rfqErr,
			}
			service := NewService(
				WithSupplierOfferAccessAuthorizer(access),
				WithIssuedRFQSource(issued),
			)

			_, err := service.ResolveMutationContext(
				context.Background(),
				SupplierOfferMutationContextInput{
					SessionToken: "session-token", InvitationID: "invitation-1",
					CSRFCookie: "csrf", CSRFHeader: "csrf", AccessedAt: now,
				},
			)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ResolveMutationContext error = %v, want %v", err, tt.want)
			}
			if gotLookup := issued.calls > 0; gotLookup != tt.wantLookup {
				t.Fatalf("issued RFQ lookup occurred = %v, want %v", gotLookup, tt.wantLookup)
			}
		})
	}
}

func TestResolveContextsRequireBothCapabilities(t *testing.T) {
	now := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)
	tests := []*Service{
		NewService(),
		NewService(WithSupplierOfferAccessAuthorizer(&fakeOfferAccessAuthorizer{})),
		NewService(WithIssuedRFQSource(&fakeIssuedRFQSource{})),
	}
	for _, service := range tests {
		_, err := service.ResolveReadContext(
			context.Background(),
			SupplierOfferReadContextInput{
				SessionToken: "session-token", InvitationID: "invitation-1", AccessedAt: now,
			},
		)
		if !errors.Is(err, ErrSupplierOffersNotConfigured) {
			t.Fatalf("ResolveReadContext error = %v, want not configured", err)
		}
	}
}
