package materials_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/materials"
)

// GetMaterialReference exposes the catalog descriptive fields M7 snapshots onto
// a Material Requirement or a Supplier Offering (M7 design spec §1.2, §1.4).
//
// It deliberately returns NO active/status bool: materials.Material has no such
// field and M3 is frozen (M7 design spec §0.3 conflict D). Presence is reported
// through `found` only.

func TestGetMaterialReferenceReturnsCatalogFields(t *testing.T) {
	repo := newFakeMaterialRepository()
	svc := materials.NewService(repo)
	ctx := context.Background()

	created, err := svc.CreateMaterial(ctx, "company_a", "Portland Cement", "cement",
		"OPC 50kg bag", "bag", 1850, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	name, catalogUnit, specification, found, err := svc.GetMaterialReference(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if name != "Portland Cement" {
		t.Errorf("name = %q, want %q", name, "Portland Cement")
	}
	if catalogUnit != "bag" {
		t.Errorf("catalogUnit = %q, want %q", catalogUnit, "bag")
	}
	if specification != "OPC 50kg bag" {
		t.Errorf("specification = %q, want %q", specification, "OPC 50kg bag")
	}
}

// An unknown material is reported through found=false, not an error, so callers
// can turn it into either a skip reason or a 404 as their context requires.
func TestGetMaterialReferenceUnknownMaterial(t *testing.T) {
	repo := newFakeMaterialRepository()
	svc := materials.NewService(repo)

	name, catalogUnit, specification, found, err := svc.GetMaterialReference(
		context.Background(), "company_a", "material_missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("found = true, want false for an unknown material")
	}
	if name != "" || catalogUnit != "" || specification != "" {
		t.Errorf("fields must be zero when not found, got (%q, %q, %q)", name, catalogUnit, specification)
	}
}

// A material belonging to another company is not found, even with a valid ID.
func TestGetMaterialReferenceIsTenantScoped(t *testing.T) {
	repo := newFakeMaterialRepository()
	svc := materials.NewService(repo)
	ctx := context.Background()

	created, err := svc.CreateMaterial(ctx, "company_a", "Portland Cement", "cement", "", "bag", 1850, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, _, found, err := svc.GetMaterialReference(ctx, "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("found = true: company_b must not see company_a's material")
	}
}

// An empty specification is a legitimate catalog value and must round-trip as
// empty rather than being reported as not-found.
func TestGetMaterialReferenceEmptySpecificationIsStillFound(t *testing.T) {
	repo := newFakeMaterialRepository()
	svc := materials.NewService(repo)
	ctx := context.Background()

	created, err := svc.CreateMaterial(ctx, "company_a", "Sand", "aggregate", "", "m3", 5000, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	name, catalogUnit, specification, found, err := svc.GetMaterialReference(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if name != "Sand" || catalogUnit != "m3" {
		t.Errorf("got (%q, %q), want (\"Sand\", \"m3\")", name, catalogUnit)
	}
	if specification != "" {
		t.Errorf("specification = %q, want empty", specification)
	}
}
