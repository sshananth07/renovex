package composition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// Fixtures for OfferVersionAwardSourceAdapter's supplier-name resolution.
// Each fake satisfies one of the adapter's narrow, unexported dependency
// interfaces structurally — the same "real service, fake repository" spirit
// as the M7 adapter fixtures, scaled down to only what this join needs.

type fakeVersionSource struct {
	version supplieroffers.SupplierOfferVersion
}

func (f fakeVersionSource) FindVersion(_ context.Context, _, versionID string) (supplieroffers.SupplierOfferVersion, bool, error) {
	if versionID != f.version.ID {
		return supplieroffers.SupplierOfferVersion{}, false, nil
	}
	return f.version, true, nil
}

func (f fakeVersionSource) ListVersionsForIssuedRFQVersion(_ context.Context, _, _ string) ([]supplieroffers.SupplierOfferVersion, error) {
	return []supplieroffers.SupplierOfferVersion{f.version}, nil
}

type fakeChainSource struct{}

func (fakeChainSource) FindChain(_ context.Context, _, _ string) (supplieroffers.SupplierOfferChain, bool, error) {
	return supplieroffers.SupplierOfferChain{}, false, nil
}

type fakeEligibilitySource struct{}

func (fakeEligibilitySource) FindEligibility(_ context.Context, _, _ string) (supplieroffers.SupplierOfferEligibility, bool, error) {
	return supplieroffers.SupplierOfferEligibility{}, false, nil
}

// fakeInvitationSource maps an invitation ID straight to a Supplier ID —
// enough to exercise the adapter's join without rebuilding rfqissuance's own
// repository behaviour.
type fakeInvitationSource struct {
	invitations map[string]string // invitationID -> supplierID
}

func (f fakeInvitationSource) FindInvitation(_ context.Context, _, invitationID string) (rfqissuance.SupplierInvitation, error) {
	supplierID, ok := f.invitations[invitationID]
	if !ok {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
	}
	return rfqissuance.SupplierInvitation{ID: invitationID, SupplierID: supplierID}, nil
}

// fakeSupplierNameSource maps a Supplier ID to a display name — enough to
// exercise the adapter's join without a real suppliers.Service instance.
type fakeSupplierNameSource struct {
	names map[string]string // supplierID -> name
}

func (f fakeSupplierNameSource) GetSupplier(_ context.Context, _, supplierID string) (suppliers.Supplier, error) {
	name, ok := f.names[supplierID]
	if !ok {
		return suppliers.Supplier{}, suppliers.ErrSupplierNotFound
	}
	return suppliers.Supplier{ID: supplierID, Name: name}, nil
}

func TestOfferVersionAwardSourceAdapterResolvesSupplierName(t *testing.T) {
	version := supplieroffers.SupplierOfferVersion{
		ID: "version_1", CompanyID: "company_a", InvitationID: "invitation_1",
	}
	adapter := composition.NewOfferVersionAwardSourceAdapter(
		fakeVersionSource{version: version},
		fakeChainSource{},
		fakeEligibilitySource{},
		fakeInvitationSource{invitations: map[string]string{"invitation_1": "supplier_1"}},
		fakeSupplierNameSource{names: map[string]string{"supplier_1": "ABC Building Materials"}},
	)

	snapshot, found, err := adapter.GetOfferVersionForAward(context.Background(), "company_a", "version_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected the version to be found")
	}
	if snapshot.SupplierID != "supplier_1" {
		t.Fatalf("SupplierID = %q, want supplier_1", snapshot.SupplierID)
	}
	if snapshot.SupplierName != "ABC Building Materials" {
		t.Fatalf("SupplierName = %q, want ABC Building Materials", snapshot.SupplierName)
	}
}

// A Supplier directory entry can go missing (e.g. deleted after the offer was
// submitted) without that failing the whole comparison — the same tolerance
// already applied to a missing invitation. The name is left blank, not an
// error.
func TestOfferVersionAwardSourceAdapterToleratesAMissingSupplierDirectoryEntry(t *testing.T) {
	version := supplieroffers.SupplierOfferVersion{
		ID: "version_1", CompanyID: "company_a", InvitationID: "invitation_1",
	}
	adapter := composition.NewOfferVersionAwardSourceAdapter(
		fakeVersionSource{version: version},
		fakeChainSource{},
		fakeEligibilitySource{},
		fakeInvitationSource{invitations: map[string]string{"invitation_1": "supplier_1"}},
		fakeSupplierNameSource{names: map[string]string{}},
	)

	snapshot, found, err := adapter.GetOfferVersionForAward(context.Background(), "company_a", "version_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected the version to be found")
	}
	if snapshot.SupplierID != "supplier_1" {
		t.Fatalf("SupplierID = %q, want supplier_1", snapshot.SupplierID)
	}
	if snapshot.SupplierName != "" {
		t.Fatalf("SupplierName = %q, want empty (directory entry missing, not an error)", snapshot.SupplierName)
	}
}

// A genuine infrastructure failure resolving the Supplier's name must
// propagate — unlike a bounded not-found, it must not be silently swallowed
// into a blank name.
func TestOfferVersionAwardSourceAdapterPropagatesASupplierLookupFailure(t *testing.T) {
	failingNames := failingSupplierNameSource{err: errors.New("boom")}
	version := supplieroffers.SupplierOfferVersion{
		ID: "version_1", CompanyID: "company_a", InvitationID: "invitation_1",
	}
	adapter := composition.NewOfferVersionAwardSourceAdapter(
		fakeVersionSource{version: version},
		fakeChainSource{},
		fakeEligibilitySource{},
		fakeInvitationSource{invitations: map[string]string{"invitation_1": "supplier_1"}},
		failingNames,
	)

	_, _, err := adapter.GetOfferVersionForAward(context.Background(), "company_a", "version_1")
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected the underlying lookup failure to propagate, got %v", err)
	}
}

type failingSupplierNameSource struct{ err error }

func (f failingSupplierNameSource) GetSupplier(_ context.Context, _, _ string) (suppliers.Supplier, error) {
	return suppliers.Supplier{}, f.err
}
