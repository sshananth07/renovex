package materials_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/materials"
)

type fakeMaterialRepository struct {
	byID map[string]materials.Material
	next int
}

func newFakeMaterialRepository() *fakeMaterialRepository {
	return &fakeMaterialRepository{byID: make(map[string]materials.Material)}
}

var errFakeMaterialDuplicateSourceSuggestionID = errors.New("fakeMaterialRepository: duplicate sourceSuggestionId")

func (f *fakeMaterialRepository) Create(ctx context.Context, m materials.Material) (materials.Material, error) {
	if m.SourceSuggestionID != nil {
		for _, existing := range f.byID {
			if existing.CompanyID == m.CompanyID && existing.SourceSuggestionID != nil &&
				*existing.SourceSuggestionID == *m.SourceSuggestionID {
				return materials.Material{}, errFakeMaterialDuplicateSourceSuggestionID
			}
		}
	}
	f.next++
	m.ID = "material_" + string(rune('0'+f.next))
	f.byID[m.ID] = m
	return m, nil
}

func (f *fakeMaterialRepository) FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (materials.Material, error) {
	for _, m := range f.byID {
		if m.CompanyID == companyID && m.SourceSuggestionID != nil && *m.SourceSuggestionID == sourceSuggestionID {
			return m, nil
		}
	}
	return materials.Material{}, materials.ErrMaterialNotFound
}

func (f *fakeMaterialRepository) FindByID(ctx context.Context, companyID, id string) (materials.Material, error) {
	m, ok := f.byID[id]
	if !ok || m.CompanyID != companyID {
		return materials.Material{}, materials.ErrMaterialNotFound
	}
	return m, nil
}

func (f *fakeMaterialRepository) List(ctx context.Context, companyID string) ([]materials.Material, error) {
	var result []materials.Material
	for _, m := range f.byID {
		if m.CompanyID == companyID {
			result = append(result, m)
		}
	}
	return result, nil
}

func (f *fakeMaterialRepository) Update(ctx context.Context, companyID, id string, fn func(*materials.Material)) (materials.Material, error) {
	m, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return materials.Material{}, err
	}
	fn(&m)
	f.byID[id] = m
	return m, nil
}

func TestCreateMaterialRequiresName(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	_, err := svc.CreateMaterial(context.Background(), "company_a", "", "cement", "", "bag", 1850, "MYR")
	if err != materials.ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestCreateMaterialRequiresUnit(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	_, err := svc.CreateMaterial(context.Background(), "company_a", "OPC Cement", "cement", "", "", 1850, "MYR")
	if err != materials.ErrUnitRequired {
		t.Fatalf("expected ErrUnitRequired, got %v", err)
	}
}

func TestCreateMaterialSuccess(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	m, err := svc.CreateMaterial(context.Background(), "company_a", "OPC Cement 50kg", "cement", "", "bag", 1850, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ReferencePrice != money.New(1850, "MYR") {
		t.Fatalf("expected reference price 1850 MYR, got %+v", m.ReferencePrice)
	}
	if m.SchemaVersion != 1 {
		t.Fatalf("expected schemaVersion 1, got %d", m.SchemaVersion)
	}
}

func TestUpdateMaterialNeverRetroactive(t *testing.T) {
	// This test documents the invariant structurally: UpdateMaterial only
	// calls MaterialRepository.Update, which this fake never propagates
	// anywhere else. The real cross-package proof that updating a Material's
	// ReferencePrice never mutates an existing CostItem is a costs-package
	// integration test (Task 12) — this test just confirms UpdateMaterial
	// itself doesn't reach outside the materials repository.
	repo := newFakeMaterialRepository()
	svc := materials.NewService(repo)
	created, err := svc.CreateMaterial(context.Background(), "company_a", "Tile", "tile", "", "m2", 3200, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateMaterial(context.Background(), "company_a", created.ID, "Tile", "tile", "", "m2", 3500, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ReferencePrice.Amount != 3500 {
		t.Fatalf("expected updated price 3500, got %d", updated.ReferencePrice.Amount)
	}
}

func TestMaterialBelongsToCompany(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	created, err := svc.CreateMaterial(context.Background(), "company_a", "Tile", "tile", "", "m2", 3200, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	belongs, err := svc.MaterialBelongsToCompany(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	crossTenant, err := svc.MaterialBelongsToCompany(context.Background(), "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if crossTenant {
		t.Fatal("expected false for cross-tenant lookup")
	}
}

func TestGetReferencePrice(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	created, err := svc.CreateMaterial(context.Background(), "company_a", "Tile", "tile", "", "m2", 3200, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	price, err := svc.GetReferencePrice(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if price.Amount != 3200 || price.Currency != "MYR" {
		t.Fatalf("expected 3200 MYR, got %d %s", price.Amount, price.Currency)
	}
}

func TestCreateMaterialFromAISuggestionSetsProvenance(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	m, err := svc.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"Tile Spacers", "tile", "2mm spacers", "bag", 500, "USD", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.SourceSuggestionID == nil || *m.SourceSuggestionID != "suggestion_1" {
		t.Fatalf("expected SourceSuggestionID suggestion_1, got %v", m.SourceSuggestionID)
	}
}

func TestCreateMaterialFromAISuggestionRequiresNameAndUnit(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())

	_, err := svc.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"", "tile", "", "bag", 500, "USD", "suggestion_1")
	if err != materials.ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}

	_, err = svc.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"Tile Spacers", "tile", "", "", 500, "USD", "suggestion_2")
	if err != materials.ErrUnitRequired {
		t.Fatalf("expected ErrUnitRequired, got %v", err)
	}
}

func TestCreateMaterialFromAISuggestionRetryReturnsExisting(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())

	first, err := svc.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"Tile Spacers", "tile", "", "bag", 500, "USD", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on first acceptance: %v", err)
	}

	second, err := svc.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"Tile Spacers", "tile", "", "bag", 500, "USD", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on retried acceptance: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected retry to return the same Material, got first=%s second=%s", first.ID, second.ID)
	}
}

func TestFindMaterialBySourceSuggestionIDTenantScoped(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())

	created, err := svc.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"Tile Spacers", "tile", "", "bag", 500, "USD", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := svc.FindMaterialBySourceSuggestionID(context.Background(), "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected found material id %s, got %s", created.ID, found.ID)
	}

	_, err = svc.FindMaterialBySourceSuggestionID(context.Background(), "company_b", "suggestion_1")
	if err != materials.ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound for cross-tenant lookup, got %v", err)
	}
}
