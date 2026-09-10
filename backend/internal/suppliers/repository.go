package suppliers

import "context"

// SupplierFilter narrows a directory listing (design spec §13.3). An empty
// field means "no filter", so the zero value lists the whole company directory.
//
// Active is a *bool rather than a bool so "only active", "only retired" and
// "either" are all expressible; a plain bool could not distinguish the last two.
type SupplierFilter struct {
	Query    string // matched against the normalized name
	Category string
	Active   *bool
}

// OfferingFilter narrows an offering listing (design spec §13.3).
type OfferingFilter struct {
	SupplierID string
	MaterialID string
	Active     *bool
}

// SupplierRepository persists the company-scoped Supplier Directory. suppliers
// owns the suppliers collection exclusively (ADR 0002).
//
// There is deliberately no Delete: archival is retirement-only, via SetActive
// (design spec §4.1).
type SupplierRepository interface {
	Create(ctx context.Context, s Supplier) (Supplier, error)
	FindByID(ctx context.Context, companyID, id string) (Supplier, error)
	List(ctx context.Context, companyID string, filter SupplierFilter) ([]Supplier, error)

	// Update applies a contractor edit under a Revision guard. It writes
	// NameNormalized alongside Name so the uniqueness key can never drift from
	// the display name.
	Update(ctx context.Context, companyID, id string, expectedRevision int64,
		updated Supplier) (Supplier, error)

	// SetActive retires or reactivates. It is a separate method from Update so
	// a retirement cannot silently carry an unrelated field edit, and so the
	// two produce distinct audit events.
	SetActive(ctx context.Context, companyID, id string, expectedRevision int64,
		active bool) (Supplier, error)
}

// SupplierOfferingRepository persists contractor-managed offerings.
//
// Like suppliers, offerings are retirement-only: no Delete.
type SupplierOfferingRepository interface {
	Create(ctx context.Context, o SupplierOffering) (SupplierOffering, error)
	FindByID(ctx context.Context, companyID, id string) (SupplierOffering, error)
	List(ctx context.Context, companyID string, filter OfferingFilter) ([]SupplierOffering, error)

	// Update never writes SupplierID. The immutability of §4.2 is enforced by
	// the write itself rather than only by the service, so a caller that passes
	// a different supplier cannot move the offering.
	Update(ctx context.Context, companyID, id string, expectedRevision int64,
		updated SupplierOffering) (SupplierOffering, error)

	SetActive(ctx context.Context, companyID, id string, expectedRevision int64,
		active bool) (SupplierOffering, error)

	// ListBySupplier supports the service's effective-availability enrichment
	// without exposing the collection to another module.
	ListBySupplier(ctx context.Context, companyID, supplierID string) ([]SupplierOffering, error)
}

// PreferenceRepository persists the at-most-one preferred Supplier per
// Material (design spec §4.3).
//
// Unlike suppliers and offerings, Delete here MAY physically remove the record:
// the preference is advisory only, and audit preserves the history.
type PreferenceRepository interface {
	// Upsert is a Revision-guarded create-or-replace. expectedRevision is 0 for
	// a create.
	Upsert(ctx context.Context, p MaterialSupplierPreference,
		expectedRevision int64) (MaterialSupplierPreference, error)

	FindByMaterial(ctx context.Context, companyID, materialID string) (MaterialSupplierPreference, error)
	ListBySupplier(ctx context.Context, companyID, supplierID string) ([]MaterialSupplierPreference, error)

	Delete(ctx context.Context, companyID, materialID string, expectedRevision int64) error
}
