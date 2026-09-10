package composition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// SupplierInvitabilityAdapter (design spec §5.1, ADR 0002).
//
// It collapses a suppliers.Supplier into ONE boolean. That is deliberate on two
// counts: rfqissuance must not name a suppliers type, and a caller probing
// supplier IDs must not be able to distinguish "absent" from "another
// company's" from "archived".

type fakeSupplierSource struct {
	supplier suppliers.Supplier
	err      error

	gotCompanyID  string
	gotSupplierID string
}

func (f *fakeSupplierSource) GetSupplier(_ context.Context,
	companyID, supplierID string) (suppliers.Supplier, error) {
	f.gotCompanyID = companyID
	f.gotSupplierID = supplierID
	return f.supplier, f.err
}

func TestSupplierInvitabilityAdapterReportsTrueForAnActiveSupplier(t *testing.T) {
	source := &fakeSupplierSource{
		supplier: suppliers.Supplier{ID: "supplier-1", CompanyID: "company-1", Active: true},
	}
	adapter := composition.NewSupplierInvitabilityAdapter(source)

	invitable, err := adapter.SupplierIsInvitable(context.Background(), "company-1", "supplier-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !invitable {
		t.Error("an active supplier in this company must be invitable")
	}
}

// An ARCHIVED supplier is not invitable: retiring one must stop new RFQs going
// to them, even though their historical records remain.
func TestSupplierInvitabilityAdapterReportsFalseForAnArchivedSupplier(t *testing.T) {
	source := &fakeSupplierSource{
		supplier: suppliers.Supplier{ID: "supplier-1", CompanyID: "company-1", Active: false},
	}
	adapter := composition.NewSupplierInvitabilityAdapter(source)

	invitable, err := adapter.SupplierIsInvitable(context.Background(), "company-1", "supplier-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if invitable {
		t.Error("an archived supplier must not be invitable")
	}
}

// A missing supplier is `false`, NOT an error: the three refusal reasons
// collapse so a caller cannot probe which applies.
func TestSupplierInvitabilityAdapterReportsFalseForAMissingSupplier(t *testing.T) {
	source := &fakeSupplierSource{err: suppliers.ErrSupplierNotFound}
	adapter := composition.NewSupplierInvitabilityAdapter(source)

	invitable, err := adapter.SupplierIsInvitable(context.Background(), "company-1", "supplier-x")

	if err != nil {
		t.Fatalf("a missing supplier must not be an error, got %v", err)
	}
	if invitable {
		t.Error("a missing supplier must not be invitable")
	}
}

// An INFRASTRUCTURE failure must propagate. Reporting it as "not invitable"
// would refuse a perfectly valid Supplier whenever Mongo hiccupped, and the
// contractor would have no way to tell the difference.
func TestSupplierInvitabilityAdapterPropagatesInfrastructureFailures(t *testing.T) {
	sentinel := errors.New("mongo is unreachable")
	source := &fakeSupplierSource{err: sentinel}
	adapter := composition.NewSupplierInvitabilityAdapter(source)

	invitable, err := adapter.SupplierIsInvitable(context.Background(), "company-1", "supplier-1")

	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want the underlying failure to propagate", err)
	}
	if invitable {
		t.Error("a failed lookup must never report invitable")
	}
}

// The lookup must be scoped by the AUTHENTICATED company.
func TestSupplierInvitabilityAdapterScopesTheLookupToTheCompany(t *testing.T) {
	source := &fakeSupplierSource{
		supplier: suppliers.Supplier{ID: "supplier-1", CompanyID: "company-1", Active: true},
	}
	adapter := composition.NewSupplierInvitabilityAdapter(source)

	if _, err := adapter.SupplierIsInvitable(context.Background(), "company-1",
		"supplier-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.gotCompanyID != "company-1" || source.gotSupplierID != "supplier-1" {
		t.Errorf("lookup used %q/%q, want company-1/supplier-1",
			source.gotCompanyID, source.gotSupplierID)
	}
}

// The adapter must satisfy the interface rfqissuance declares.
func TestSupplierInvitabilityAdapterSatisfiesTheConsumerInterface(t *testing.T) {
	var _ rfqissuance.SupplierLookup = composition.NewSupplierInvitabilityAdapter(
		&fakeSupplierSource{})
}
