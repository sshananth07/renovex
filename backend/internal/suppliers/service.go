package suppliers

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// MaterialLookup is the capability suppliers needs from materials.
//
// It deliberately exposes NO active/status value: materials.Material has no
// such field and M3 is frozen, so M7 must not invent one (design spec §0.3
// conflict D). A missing Material is reported through found=false, which is
// also how an offering whose linked Material later left the catalog degrades —
// the offering stays readable, enrichment is simply empty.
type MaterialLookup interface {
	GetMaterialReference(ctx context.Context, companyID, materialID string) (
		name string, catalogUnit string, specification string, found bool, err error)
}

// AuditRecorder is the consumer-owned audit contract for this module. There is
// no shared cross-module audit interface (design spec §1.5).
//
// The signatures mirror the A4 audit contract exactly, so audit.Service
// satisfies this STRUCTURALLY with no adapter — every parameter is a primitive.
type AuditRecorder interface {
	RecordSupplierCreated(ctx context.Context, companyID, actorUserID, supplierID, supplierName string) error
	RecordSupplierUpdated(ctx context.Context, companyID, actorUserID, supplierID string) error
	RecordSupplierActiveStateChanged(ctx context.Context, companyID, actorUserID, supplierID string,
		active bool) error
	RecordSupplierOfferingCreated(ctx context.Context, companyID, actorUserID,
		supplierID, offeringID string) error
	RecordSupplierOfferingUpdated(ctx context.Context, companyID, actorUserID,
		supplierID, offeringID string) error
	RecordSupplierOfferingActiveStateChanged(ctx context.Context, companyID, actorUserID,
		supplierID, offeringID string, active bool) error
	RecordPreferredSupplierChanged(ctx context.Context, companyID, actorUserID,
		materialID, supplierID string) error
	// previousSupplierID is recorded so the cleared preference remains
	// reconstructible from the audit trail — the record itself is physically
	// deleted (design spec §4.3).
	RecordPreferredSupplierCleared(ctx context.Context, companyID, actorUserID,
		materialID, previousSupplierID string) error
}

// Service implements the Supplier Directory workflows.
type Service struct {
	suppliers   SupplierRepository
	offerings   SupplierOfferingRepository
	preferences PreferenceRepository
	materials   MaterialLookup
	audit       AuditRecorder
}

// NewService constructs a Service from its three repositories and the two
// capabilities it consumes.
func NewService(supplierRepo SupplierRepository, offeringRepo SupplierOfferingRepository,
	preferenceRepo PreferenceRepository, materials MaterialLookup, audit AuditRecorder) *Service {
	return &Service{
		suppliers: supplierRepo, offerings: offeringRepo, preferences: preferenceRepo,
		materials: materials, audit: audit,
	}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of any of the three public repository interfaces. Only the real
// Mongo repositories implement it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Supplier, SupplierOffering,
// AND MaterialSupplierPreference owned by companyID — all three
// collections, one method, per Task 1a's multi-collection guidance.
// Development-tool use only (demoseed reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	supplierDeleter, ok := s.suppliers.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("suppliers: supplier repository %T does not support DeleteAllForCompany", s.suppliers)
	}
	if err := supplierDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	offeringDeleter, ok := s.offerings.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("suppliers: offering repository %T does not support DeleteAllForCompany", s.offerings)
	}
	if err := offeringDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	preferenceDeleter, ok := s.preferences.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("suppliers: preference repository %T does not support DeleteAllForCompany", s.preferences)
	}
	return preferenceDeleter.DeleteAllForCompany(ctx, companyID)
}

// CreateSupplierInput carries a directory entry. Name is the only mandatory
// field: M7 contacts no one, so incomplete records are permitted (§4.1).
type CreateSupplierInput struct {
	Name               string
	ContactPerson      string
	Email              string
	Phone              string
	Address            string
	MaterialCategories []string
	Notes              string
}

// UpdateSupplierInput is a sparse edit: a nil field means "leave unchanged".
type UpdateSupplierInput struct {
	Name               *string
	ContactPerson      *string
	Email              *string
	Phone              *string
	Address            *string
	MaterialCategories *[]string
	Notes              *string
}

// CreateSupplier adds a directory entry.
func (s *Service) CreateSupplier(ctx context.Context, companyID, actorUserID string,
	input CreateSupplierInput) (Supplier, error) {

	normalized, err := NormalizeSupplierName(input.Name)
	if err != nil {
		return Supplier{}, err
	}
	email, err := normalizeOptionalEmail(input.Email)
	if err != nil {
		return Supplier{}, err
	}

	now := time.Now()
	supplier := Supplier{
		CompanyID: companyID,
		// The display name keeps the contractor's own spacing and case; only
		// the uniqueness key is normalized.
		Name:           strings.TrimSpace(input.Name),
		NameNormalized: normalized,
		ContactPerson:  strings.TrimSpace(input.ContactPerson),
		Email:          email,
		// Phone is a trimmed STRING, never numeric: leading zeros, +, spaces
		// and extensions are all meaningful.
		Phone:              strings.TrimSpace(input.Phone),
		Address:            strings.TrimSpace(input.Address),
		MaterialCategories: NormalizeMaterialCategories(input.MaterialCategories),
		Notes:              input.Notes,
		Active:             true,
		CreatedByUserID:    actorUserID,
		CreatedAt:          now, UpdatedAt: now, SchemaVersion: 1,
	}

	created, err := s.suppliers.Create(ctx, supplier)
	if err != nil {
		return Supplier{}, err
	}
	// Audit is best-effort: a failure here never fails the business operation.
	_ = s.audit.RecordSupplierCreated(ctx, companyID, actorUserID, created.ID, created.Name)
	return created, nil
}

// GetSupplier returns one directory entry, tenant-scoped.
func (s *Service) GetSupplier(ctx context.Context, companyID, supplierID string) (Supplier, error) {
	return s.suppliers.FindByID(ctx, companyID, supplierID)
}

// ListSuppliers searches the company directory.
func (s *Service) ListSuppliers(ctx context.Context, companyID string,
	filter SupplierFilter) ([]Supplier, error) {
	return s.suppliers.List(ctx, companyID, filter)
}

// UpdateSupplier applies a sparse edit under a Revision guard.
func (s *Service) UpdateSupplier(ctx context.Context, companyID, actorUserID, supplierID string,
	expectedRevision int64, input UpdateSupplierInput) (Supplier, error) {

	existing, err := s.suppliers.FindByID(ctx, companyID, supplierID)
	if err != nil {
		return Supplier{}, err
	}

	updated := existing
	if input.Name != nil {
		normalized, err := NormalizeSupplierName(*input.Name)
		if err != nil {
			return Supplier{}, err
		}
		updated.Name = strings.TrimSpace(*input.Name)
		updated.NameNormalized = normalized
	}
	if input.ContactPerson != nil {
		updated.ContactPerson = strings.TrimSpace(*input.ContactPerson)
	}
	if input.Email != nil {
		email, err := normalizeOptionalEmail(*input.Email)
		if err != nil {
			return Supplier{}, err
		}
		updated.Email = email
	}
	if input.Phone != nil {
		updated.Phone = strings.TrimSpace(*input.Phone)
	}
	if input.Address != nil {
		updated.Address = strings.TrimSpace(*input.Address)
	}
	if input.MaterialCategories != nil {
		updated.MaterialCategories = NormalizeMaterialCategories(*input.MaterialCategories)
	}
	if input.Notes != nil {
		updated.Notes = *input.Notes
	}

	saved, err := s.suppliers.Update(ctx, companyID, supplierID, expectedRevision, updated)
	if err != nil {
		return Supplier{}, err
	}
	_ = s.audit.RecordSupplierUpdated(ctx, companyID, actorUserID, saved.ID)
	return saved, nil
}

// SetSupplierActive retires or reactivates a Supplier.
//
// Archival is retirement-only and there is NO CASCADE: offerings and
// preferences are left exactly as they are. They become merely unavailable,
// which the service computes on read rather than persisting — persisting it
// would require rewriting every dependent record on each toggle, and would go
// stale the moment the supplier came back (design spec §4.1, §4.2).
func (s *Service) SetSupplierActive(ctx context.Context, companyID, actorUserID, supplierID string,
	expectedRevision int64, active bool) (Supplier, error) {

	saved, err := s.suppliers.SetActive(ctx, companyID, supplierID, expectedRevision, active)
	if err != nil {
		return Supplier{}, err
	}
	_ = s.audit.RecordSupplierActiveStateChanged(ctx, companyID, actorUserID, saved.ID, active)
	return saved, nil
}

// --- Offerings ---

// CreateOfferingInput carries a new offering.
type CreateOfferingInput struct {
	SupplierID  string
	MaterialID  *string
	ProductName string
	Brand       string
	SKU         string
	Description string
	Category    string
	Unit        string

	ImageURL   *string
	ProductURL *string

	IndicativePrice     *money.Money
	IndicativePriceAsOf *time.Time
	LastVerifiedAt      *time.Time
}

// UpdateOfferingInput is a sparse edit.
//
// SupplierID is present so a mismatched value can be REJECTED rather than
// silently ignored — §4.2 makes it immutable, and a client that believes it
// moved an offering should learn otherwise.
type UpdateOfferingInput struct {
	SupplierID *string

	MaterialID      *string
	ClearMaterialID bool

	ProductName *string
	Brand       *string
	SKU         *string
	Description *string
	Category    *string
	Unit        *string

	ImageURL        *string
	ClearImageURL   bool
	ProductURL      *string
	ClearProductURL bool

	IndicativePrice      *money.Money
	IndicativePriceAsOf  *time.Time
	ClearIndicativePrice bool

	LastVerifiedAt      *time.Time
	ClearLastVerifiedAt bool
}

// OfferingView is an offering plus the computed fields a reader needs.
//
// EffectivelyAvailable and MaterialName are never persisted: the first depends
// on another aggregate's state, and the second is a catalog lookup that may
// legitimately report nothing (design spec §4.2).
type OfferingView struct {
	Offering             SupplierOffering
	SupplierName         string
	SupplierActive       bool
	EffectivelyAvailable bool
	MaterialName         string
}

// CreateOffering records something a Supplier sells.
//
// The Supplier must be ACTIVE: recording new commercial detail against a
// retired entity is a mistake worth refusing (design spec §4.2).
func (s *Service) CreateOffering(ctx context.Context, companyID, actorUserID string,
	input CreateOfferingInput) (SupplierOffering, error) {

	supplier, err := s.suppliers.FindByID(ctx, companyID, input.SupplierID)
	if err != nil {
		return SupplierOffering{}, err
	}
	if !supplier.Active {
		return SupplierOffering{}, ErrSupplierInactive
	}

	if strings.TrimSpace(input.ProductName) == "" {
		return SupplierOffering{}, ErrProductNameRequired
	}
	if err := s.validateMaterialLink(ctx, companyID, input.MaterialID); err != nil {
		return SupplierOffering{}, err
	}
	if err := ValidateIndicativePrice(input.IndicativePrice, input.IndicativePriceAsOf,
		input.Unit); err != nil {
		return SupplierOffering{}, err
	}

	imageURL, err := validateOptionalURL(input.ImageURL)
	if err != nil {
		return SupplierOffering{}, err
	}
	productURL, err := validateOptionalURL(input.ProductURL)
	if err != nil {
		return SupplierOffering{}, err
	}

	now := time.Now()
	offering := SupplierOffering{
		CompanyID: companyID, SupplierID: input.SupplierID, MaterialID: input.MaterialID,
		ProductName: strings.TrimSpace(input.ProductName),
		Brand:       strings.TrimSpace(input.Brand),
		SKU:         strings.TrimSpace(input.SKU),
		Description: input.Description,
		Category:    strings.TrimSpace(input.Category),
		Unit:        strings.TrimSpace(input.Unit),
		ImageURL:    imageURL, ProductURL: productURL,
		IndicativePrice: input.IndicativePrice, IndicativePriceAsOf: input.IndicativePriceAsOf,
		LastVerifiedAt:  input.LastVerifiedAt,
		Active:          true,
		CreatedByUserID: actorUserID,
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}

	created, err := s.offerings.Create(ctx, offering)
	if err != nil {
		return SupplierOffering{}, err
	}
	_ = s.audit.RecordSupplierOfferingCreated(ctx, companyID, actorUserID,
		created.SupplierID, created.ID)
	return created, nil
}

// GetOffering returns one offering with its computed availability.
func (s *Service) GetOffering(ctx context.Context, companyID, offeringID string) (OfferingView, error) {
	offering, err := s.offerings.FindByID(ctx, companyID, offeringID)
	if err != nil {
		return OfferingView{}, err
	}
	return s.viewOf(ctx, companyID, offering)
}

// ListOfferings returns offerings with their computed availability.
func (s *Service) ListOfferings(ctx context.Context, companyID string,
	filter OfferingFilter) ([]OfferingView, error) {

	// The optional ids are parent filters, not opaque search terms. Validate
	// them before querying so a foreign parent returns 404 rather than an empty
	// 200 that confirms the identifier exists in another tenant (§14).
	if filter.SupplierID != "" {
		if _, err := s.suppliers.FindByID(ctx, companyID, filter.SupplierID); err != nil {
			return nil, err
		}
	}
	if filter.MaterialID != "" {
		if err := s.validateMaterialLink(ctx, companyID, &filter.MaterialID); err != nil {
			return nil, err
		}
	}

	found, err := s.offerings.List(ctx, companyID, filter)
	if err != nil {
		return nil, err
	}
	// Suppliers are cached per call so a long listing does not re-read the same
	// supplier once per offering.
	cache := map[string]Supplier{}
	views := make([]OfferingView, 0, len(found))
	for _, o := range found {
		view, err := s.viewOfCached(ctx, companyID, o, cache)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) viewOf(ctx context.Context, companyID string,
	offering SupplierOffering) (OfferingView, error) {
	return s.viewOfCached(ctx, companyID, offering, map[string]Supplier{})
}

func (s *Service) viewOfCached(ctx context.Context, companyID string, offering SupplierOffering,
	cache map[string]Supplier) (OfferingView, error) {

	supplier, ok := cache[offering.SupplierID]
	if !ok {
		found, err := s.suppliers.FindByID(ctx, companyID, offering.SupplierID)
		if err != nil && !errors.Is(err, ErrSupplierNotFound) {
			return OfferingView{}, err
		}
		supplier = found
		cache[offering.SupplierID] = found
	}

	view := OfferingView{
		Offering:       offering,
		SupplierName:   supplier.Name,
		SupplierActive: supplier.Active,
		// Offering.Active AND Supplier.Active. A linked Material deliberately
		// does not participate (design spec §4.2).
		EffectivelyAvailable: offering.EffectivelyAvailable(supplier.Active),
	}

	if offering.MaterialID != nil {
		// A Material that has left the catalog simply reports found=false; the
		// offering stays readable with its own snapshot fields.
		name, _, _, found, err := s.materials.GetMaterialReference(ctx, companyID, *offering.MaterialID)
		if err != nil {
			return OfferingView{}, err
		}
		if found {
			view.MaterialName = name
		}
	}
	return view, nil
}

// UpdateOffering applies a sparse edit under a Revision guard.
func (s *Service) UpdateOffering(ctx context.Context, companyID, actorUserID, offeringID string,
	expectedRevision int64, input UpdateOfferingInput) (SupplierOffering, error) {

	existing, err := s.offerings.FindByID(ctx, companyID, offeringID)
	if err != nil {
		return SupplierOffering{}, err
	}

	// §4.2: the supplier link is immutable. Echoing the CURRENT value back is
	// not a change and is accepted; naming a different supplier is refused
	// rather than silently ignored.
	if input.SupplierID != nil && *input.SupplierID != existing.SupplierID {
		return SupplierOffering{}, ErrSupplierIDImmutable
	}

	updated := existing
	if input.ClearMaterialID && input.MaterialID != nil {
		return SupplierOffering{}, ErrMaterialNotFound
	}
	switch {
	case input.ClearMaterialID:
		updated.MaterialID = nil
	case input.MaterialID != nil:
		if err := s.validateMaterialLink(ctx, companyID, input.MaterialID); err != nil {
			return SupplierOffering{}, err
		}
		updated.MaterialID = input.MaterialID
	}

	if input.ProductName != nil {
		if strings.TrimSpace(*input.ProductName) == "" {
			return SupplierOffering{}, ErrProductNameRequired
		}
		updated.ProductName = strings.TrimSpace(*input.ProductName)
	}
	if input.Brand != nil {
		updated.Brand = strings.TrimSpace(*input.Brand)
	}
	if input.SKU != nil {
		updated.SKU = strings.TrimSpace(*input.SKU)
	}
	if input.Description != nil {
		updated.Description = *input.Description
	}
	if input.Category != nil {
		updated.Category = strings.TrimSpace(*input.Category)
	}
	if input.Unit != nil {
		updated.Unit = strings.TrimSpace(*input.Unit)
	}

	if input.ClearImageURL {
		updated.ImageURL = nil
	} else if input.ImageURL != nil {
		v, err := validateOptionalURL(input.ImageURL)
		if err != nil {
			return SupplierOffering{}, err
		}
		updated.ImageURL = v
	}
	if input.ClearProductURL {
		updated.ProductURL = nil
	} else if input.ProductURL != nil {
		v, err := validateOptionalURL(input.ProductURL)
		if err != nil {
			return SupplierOffering{}, err
		}
		updated.ProductURL = v
	}

	// Clearing the price clears its as-of date CONSISTENTLY, and deliberately
	// leaves LastVerifiedAt alone: the two dates mean different things
	// (design spec §4.2).
	switch {
	case input.ClearIndicativePrice:
		updated.IndicativePrice = nil
		updated.IndicativePriceAsOf = nil
	default:
		if input.IndicativePrice != nil {
			updated.IndicativePrice = input.IndicativePrice
		}
		if input.IndicativePriceAsOf != nil {
			updated.IndicativePriceAsOf = input.IndicativePriceAsOf
		}
	}

	if input.ClearLastVerifiedAt {
		updated.LastVerifiedAt = nil
	} else if input.LastVerifiedAt != nil {
		updated.LastVerifiedAt = input.LastVerifiedAt
	}

	if err := ValidateIndicativePrice(updated.IndicativePrice, updated.IndicativePriceAsOf,
		updated.Unit); err != nil {
		return SupplierOffering{}, err
	}

	saved, err := s.offerings.Update(ctx, companyID, offeringID, expectedRevision, updated)
	if err != nil {
		return SupplierOffering{}, err
	}
	_ = s.audit.RecordSupplierOfferingUpdated(ctx, companyID, actorUserID, saved.SupplierID, saved.ID)
	return saved, nil
}

// SetOfferingActive retires or reactivates an offering. Soft only.
func (s *Service) SetOfferingActive(ctx context.Context, companyID, actorUserID, offeringID string,
	expectedRevision int64, active bool) (SupplierOffering, error) {

	saved, err := s.offerings.SetActive(ctx, companyID, offeringID, expectedRevision, active)
	if err != nil {
		return SupplierOffering{}, err
	}
	_ = s.audit.RecordSupplierOfferingActiveStateChanged(ctx, companyID, actorUserID, saved.SupplierID, saved.ID, active)
	return saved, nil
}

// --- Preference ---

// PreferenceView is a preference plus the supplier state a reader needs.
//
// A preference whose Supplier is inactive remains historically visible and is
// reported unavailable; the contractor chooses a replacement or clears it
// (design spec §4.3).
type PreferenceView struct {
	Preference     MaterialSupplierPreference
	SupplierName   string
	SupplierActive bool
	MaterialName   string
}

// SetPreferredSupplier is the Revision-guarded create-or-replace behind PUT.
//
// The Supplier must be ACTIVE: a retired supplier cannot be newly preferred,
// though an existing preference survives its supplier's retirement.
func (s *Service) SetPreferredSupplier(ctx context.Context, companyID, actorUserID,
	materialID, supplierID string, expectedRevision int64) (MaterialSupplierPreference, error) {

	if _, _, _, found, err := s.materials.GetMaterialReference(ctx, companyID, materialID); err != nil {
		return MaterialSupplierPreference{}, err
	} else if !found {
		return MaterialSupplierPreference{}, ErrMaterialNotFound
	}

	supplier, err := s.suppliers.FindByID(ctx, companyID, supplierID)
	if err != nil {
		return MaterialSupplierPreference{}, err
	}
	if !supplier.Active {
		return MaterialSupplierPreference{}, ErrSupplierInactive
	}

	now := time.Now()
	saved, err := s.preferences.Upsert(ctx, MaterialSupplierPreference{
		CompanyID: companyID, MaterialID: materialID, SupplierID: supplierID,
		CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	}, expectedRevision)
	if err != nil {
		return MaterialSupplierPreference{}, err
	}
	_ = s.audit.RecordPreferredSupplierChanged(ctx, companyID, actorUserID, materialID, supplierID)
	return saved, nil
}

// GetPreferredSupplier reads the preference with its supplier state.
func (s *Service) GetPreferredSupplier(ctx context.Context, companyID,
	materialID string) (PreferenceView, error) {

	pref, err := s.preferences.FindByMaterial(ctx, companyID, materialID)
	if err != nil {
		return PreferenceView{}, err
	}

	view := PreferenceView{Preference: pref}
	supplier, err := s.suppliers.FindByID(ctx, companyID, pref.SupplierID)
	if err != nil && !errors.Is(err, ErrSupplierNotFound) {
		return PreferenceView{}, err
	}
	view.SupplierName = supplier.Name
	view.SupplierActive = supplier.Active

	if name, _, _, found, err := s.materials.GetMaterialReference(ctx, companyID, materialID); err != nil {
		return PreferenceView{}, err
	} else if found {
		view.MaterialName = name
	}
	return view, nil
}

// ClearPreferredSupplier removes the preference. A physical delete is permitted
// here because the relationship is advisory only and audit preserves the
// history (design spec §4.3).
func (s *Service) ClearPreferredSupplier(ctx context.Context, companyID, actorUserID,
	materialID string, expectedRevision int64) error {

	// Read BEFORE deleting: the record is physically removed, so the previous
	// supplier would otherwise be unrecoverable, and the audit trail is what
	// preserves the history a delete gives up (design spec §4.3).
	existing, err := s.preferences.FindByMaterial(ctx, companyID, materialID)
	if err != nil {
		return err
	}
	if err := s.preferences.Delete(ctx, companyID, materialID, expectedRevision); err != nil {
		return err
	}
	_ = s.audit.RecordPreferredSupplierCleared(ctx, companyID, actorUserID, materialID,
		existing.SupplierID)
	return nil
}

// --- helpers ---

// validateMaterialLink confirms a linked Material exists and belongs to the
// company. There is deliberately no active check: M7 has no Material state
// (design spec §0.3 conflict D).
func (s *Service) validateMaterialLink(ctx context.Context, companyID string, materialID *string) error {
	if materialID == nil {
		return nil
	}
	_, _, _, found, err := s.materials.GetMaterialReference(ctx, companyID, *materialID)
	if err != nil {
		return err
	}
	if !found {
		return ErrMaterialNotFound
	}
	return nil
}

// normalizeOptionalEmail applies the existing project convention: an empty
// value is absent and permitted, and a present one must parse.
func normalizeOptionalEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", nil
	}
	if _, err := mail.ParseAddress(trimmed); err != nil {
		return "", ErrInvalidEmail
	}
	return identity.NormalizeEmail(trimmed), nil
}

// validateOptionalURL applies §4.4 to an optional field, returning nil for an
// absent or empty value so the caller stores nothing.
func validateOptionalURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	canonical, err := ValidateURL(*raw)
	if err != nil {
		return nil, err
	}
	if canonical == "" {
		return nil, nil
	}
	return &canonical, nil
}
