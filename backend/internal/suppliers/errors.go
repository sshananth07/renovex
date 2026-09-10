package suppliers

import "errors"

// Domain sentinels (design spec §19). Every one is matched with errors.Is at
// the handler boundary and mapped to an explicit HTTP status (design spec
// §13.5).

// Lookup errors.
var (
	ErrSupplierNotFound         = errors.New("suppliers: supplier not found")
	ErrSupplierOfferingNotFound = errors.New("suppliers: supplier offering not found")
	ErrPreferenceNotFound       = errors.New("suppliers: preferred supplier not found")
	// ErrMaterialNotFound is raised when a linked Material does not exist or
	// belongs to another company. There is deliberately no inactive-Material
	// error: M7 has no such concept (design spec §0.3 conflict D).
	ErrMaterialNotFound = errors.New("suppliers: material not found")
)

// Validation errors.
var (
	ErrSupplierNameRequired = errors.New("suppliers: supplier name is required")
	// ErrProductNameRequired guards the one mandatory offering field: an
	// offering with no product name identifies nothing.
	ErrProductNameRequired = errors.New("suppliers: offering productName is required")
	ErrInvalidEmail        = errors.New("suppliers: the supplied email address is not valid")
	// ErrInvalidURL covers every §4.4 rejection: an unparseable URL, a scheme
	// other than http/https, an empty host, EMBEDDED CREDENTIALS, or a value
	// over 2048 bytes.
	ErrInvalidURL = errors.New("suppliers: url must be an http or https address without embedded credentials")
	// ErrInvalidIndicativePrice covers every §4.2 price invariant: a
	// non-positive amount, a blank currency, a missing as-of date, an as-of
	// date without a price, or a missing unit.
	ErrInvalidIndicativePrice = errors.New("suppliers: indicative price is invalid")
)

// State errors.
var (
	// ErrSupplierNameTaken is returned whether the colliding record is ACTIVE
	// or INACTIVE. Uniqueness is not partial on Active, so retirement cannot be
	// followed by accidental duplication; the contractor reactivates or renames
	// the existing record (design spec §4.1).
	ErrSupplierNameTaken = errors.New("suppliers: a supplier with that name already exists")

	// ErrSupplierInactive is returned when an inactive Supplier would receive a
	// new offering (§4.2) or a new preference (§4.3).
	ErrSupplierInactive = errors.New("suppliers: supplier is inactive")

	// ErrSupplierIDImmutable enforces §4.2: an offering may not be moved
	// between suppliers after creation.
	ErrSupplierIDImmutable = errors.New("suppliers: supplierId is immutable after creation")

	// ErrPreferenceAlreadyExists reports the unique {companyId, materialId}
	// index. The PUT route is a create-or-replace, so this surfaces only on a
	// genuine concurrent create.
	ErrPreferenceAlreadyExists = errors.New("suppliers: a preferred supplier already exists for that material")
)

// ErrRevisionMismatch is returned when a conditional update matches no document
// because the stored Revision moved, or the document is no longer in the
// required state (design spec §10).
var ErrRevisionMismatch = errors.New("suppliers: record changed since it was read")

// ErrUnclassifiedDuplicateKey is returned for a duplicate-key error this
// package cannot positively identify. It maps to 500 rather than being assumed
// safe to treat as a known collision (design spec §12.4).
var ErrUnclassifiedDuplicateKey = errors.New("suppliers: unclassified duplicate-key error")
