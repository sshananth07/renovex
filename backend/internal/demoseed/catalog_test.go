package demoseed_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

type fakeMaterialCatalog struct {
	byName      map[string]materials.Material
	createCalls int
}

func (f *fakeMaterialCatalog) CreateMaterial(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency string) (materials.Material, error) {
	f.createCalls++
	m := materials.Material{ID: name, CompanyID: companyID, Name: name, Category: category, Unit: unit}
	f.byName[name] = m
	return m, nil
}
func (f *fakeMaterialCatalog) ListMaterials(ctx context.Context, companyID string) ([]materials.Material, error) {
	list := make([]materials.Material, 0, len(f.byName))
	for _, m := range f.byName {
		list = append(list, m)
	}
	return list, nil
}

func TestEnsureMaterialCatalog_CreatesAllOnFirstRun(t *testing.T) {
	fake := &fakeMaterialCatalog{byName: map[string]materials.Material{}}
	catalog, err := demoseed.EnsureMaterialCatalog(context.Background(), fake, "company-1")
	if err != nil {
		t.Fatalf("EnsureMaterialCatalog: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatalf("expected a non-empty catalog")
	}
	if _, ok := catalog["Porcelain Floor Tile"]; !ok {
		t.Fatalf("expected Porcelain Floor Tile in the catalog, got %v", catalog)
	}
	firstRunCreates := fake.createCalls
	if firstRunCreates == 0 {
		t.Fatalf("expected at least one CreateMaterial call on a fresh company")
	}

	_, err = demoseed.EnsureMaterialCatalog(context.Background(), fake, "company-1")
	if err != nil {
		t.Fatalf("second EnsureMaterialCatalog: %v", err)
	}
	if fake.createCalls != firstRunCreates {
		t.Fatalf("expected no new CreateMaterial calls on rerun, went from %d to %d", firstRunCreates, fake.createCalls)
	}
}

type fakeSupplierDirectory struct {
	byName      map[string]suppliers.Supplier
	createCalls int
}

func (f *fakeSupplierDirectory) CreateSupplier(ctx context.Context, companyID, actorUserID string, input suppliers.CreateSupplierInput) (suppliers.Supplier, error) {
	f.createCalls++
	s := suppliers.Supplier{ID: input.Name, CompanyID: companyID, Name: input.Name}
	f.byName[input.Name] = s
	return s, nil
}
func (f *fakeSupplierDirectory) ListSuppliers(ctx context.Context, companyID string, filter suppliers.SupplierFilter) ([]suppliers.Supplier, error) {
	list := make([]suppliers.Supplier, 0, len(f.byName))
	for _, s := range f.byName {
		list = append(list, s)
	}
	return list, nil
}

func TestEnsureSupplierDirectory_IdempotentAcrossRuns(t *testing.T) {
	fake := &fakeSupplierDirectory{byName: map[string]suppliers.Supplier{}}
	_, err := demoseed.EnsureSupplierDirectory(context.Background(), fake, "company-1", "user-1")
	if err != nil {
		t.Fatalf("EnsureSupplierDirectory: %v", err)
	}
	firstRunCreates := fake.createCalls
	if firstRunCreates == 0 {
		t.Fatalf("expected at least one CreateSupplier call on a fresh company")
	}

	_, err = demoseed.EnsureSupplierDirectory(context.Background(), fake, "company-1", "user-1")
	if err != nil {
		t.Fatalf("second EnsureSupplierDirectory: %v", err)
	}
	if fake.createCalls != firstRunCreates {
		t.Fatalf("expected no new CreateSupplier calls on rerun, went from %d to %d", firstRunCreates, fake.createCalls)
	}
}
