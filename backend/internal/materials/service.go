package materials

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrNameRequired is returned when CreateMaterial/UpdateMaterial is given an
// empty name.
var ErrNameRequired = errors.New("materials: name is required")

// ErrUnitRequired is returned when CreateMaterial/UpdateMaterial is given an
// empty unit.
var ErrUnitRequired = errors.New("materials: unit is required")

// Service implements Material CRUD. It consumes no capability from any other
// module (company-level catalog only, no project/work-item linkage — design
// spec §1.1.6) and exposes MaterialLookup, consumed by costs.
type Service struct {
	repo MaterialRepository
}

// NewService constructs a Service backed by repo.
func NewService(repo MaterialRepository) *Service {
	return &Service{repo: repo}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public MaterialRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Material owned by
// companyID. Development-tool use only (demoseed reset, design spec §6.6).
// Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("materials: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateMaterial validates name and unit are non-empty and persists a new
// Material with the given reference price.
func (s *Service) CreateMaterial(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency string) (Material, error) {
	if name == "" {
		return Material{}, ErrNameRequired
	}
	if unit == "" {
		return Material{}, ErrUnitRequired
	}
	now := time.Now()
	return s.repo.Create(ctx, Material{
		CompanyID: companyID, Name: name, Category: category, Specification: specification, Unit: unit,
		ReferencePrice: money.New(referencePriceAmount, currency), ReferencePriceAsOf: now,
		CreatedAt: now, SchemaVersion: 1,
	})
}

// GetMaterial returns materialID's Material, tenant-scoped to companyID.
func (s *Service) GetMaterial(ctx context.Context, companyID, materialID string) (Material, error) {
	return s.repo.FindByID(ctx, companyID, materialID)
}

// ListMaterials returns every Material belonging to companyID.
func (s *Service) ListMaterials(ctx context.Context, companyID string) ([]Material, error) {
	return s.repo.List(ctx, companyID)
}

// UpdateMaterial updates materialID's fields, tenant-scoped to companyID.
// Updating ReferencePrice here never retroactively alters any already-created
// CostItem (design spec §1.1.5) — this method only ever writes to the
// materials collection.
func (s *Service) UpdateMaterial(ctx context.Context, companyID, materialID, name, category, specification, unit string, referencePriceAmount int64, currency string) (Material, error) {
	if name == "" {
		return Material{}, ErrNameRequired
	}
	if unit == "" {
		return Material{}, ErrUnitRequired
	}
	return s.repo.Update(ctx, companyID, materialID, func(m *Material) {
		m.Name = name
		m.Category = category
		m.Specification = specification
		m.Unit = unit
		m.ReferencePrice = money.New(referencePriceAmount, currency)
		m.ReferencePriceAsOf = time.Now()
	})
}

// CreateMaterialFromAISuggestion is the narrow internal seam the AI Resource
// Assistant's Create & Add flow uses to create a Material (M8.5B-A design
// doc §12.5). It preserves every existing Material domain validation —
// name/unit still required — because the AI-suggested name may only prefill
// the contractor-controlled catalog form; category/specification/unit/
// referencePrice remain contractor-supplied inputs exactly like a manual
// CreateMaterial call. sourceSuggestionID is backend-supplied provenance
// only. On a retried acceptance for the same sourceSuggestionID, it returns
// the already-created Material instead of creating a duplicate.
func (s *Service) CreateMaterialFromAISuggestion(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency, sourceSuggestionID string) (Material, error) {
	if name == "" {
		return Material{}, ErrNameRequired
	}
	if unit == "" {
		return Material{}, ErrUnitRequired
	}
	now := time.Now()
	created, err := s.repo.Create(ctx, Material{
		CompanyID: companyID, Name: name, Category: category, Specification: specification, Unit: unit,
		ReferencePrice: money.New(referencePriceAmount, currency), ReferencePriceAsOf: now,
		SourceSuggestionID: &sourceSuggestionID,
		CreatedAt:          now, SchemaVersion: 1,
	})
	if err != nil {
		if existing, findErr := s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID); findErr == nil {
			return existing, nil
		}
		return Material{}, err
	}
	return created, nil
}

// FindMaterialBySourceSuggestionID looks up a Material by its AI-suggestion
// provenance, tenant-scoped to companyID — the acceptance idempotency
// anchor (M8.5B-A design doc §18.2).
func (s *Service) FindMaterialBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (Material, error) {
	return s.repo.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
}

// MaterialBelongsToCompany reports whether materialID exists and belongs to
// companyID. Satisfies costs.MaterialLookup structurally (design spec §9.2).
func (s *Service) MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error) {
	_, err := s.repo.FindByID(ctx, companyID, materialID)
	if err == ErrMaterialNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetReferencePrice returns materialID's current ReferencePrice, tenant-scoped
// to companyID. Used by costs only as a creation-time pre-fill suggestion —
// never re-consulted after a CostItem is created (design spec §1.1.4, §9.2).
// GetMaterialReference returns the catalog descriptive fields a consumer
// snapshots onto its own record: name, unit, and specification. Presence is
// reported through found, so a caller can decide for itself whether a missing
// material is a 404 or a skip.
//
// It deliberately returns NO active/status value. Material has no such field
// (see the struct in material.go), and M7 must not invent one — see
// docs/superpowers/specs/2026-07-26-milestone-7-procurement-foundation-design.md
// §0.3 conflict D. Callers requiring an "active" notion must own that state on
// their own records.
//
// Satisfies materialrequirements.MaterialLookup and suppliers.MaterialLookup
// structurally (M7 design spec §1.2, §1.4). Uses only primitives — no Material
// struct crosses the module boundary (ADR 0002).
func (s *Service) GetMaterialReference(ctx context.Context, companyID, materialID string) (
	name string, catalogUnit string, specification string, found bool, err error,
) {
	m, err := s.repo.FindByID(ctx, companyID, materialID)
	if errors.Is(err, ErrMaterialNotFound) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	return m.Name, m.Unit, m.Specification, true, nil
}

func (s *Service) GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error) {
	m, err := s.repo.FindByID(ctx, companyID, materialID)
	if err != nil {
		return money.Money{}, err
	}
	return m.ReferencePrice, nil
}
