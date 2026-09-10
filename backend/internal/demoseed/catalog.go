package demoseed

import (
	"context"

	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

type MaterialCatalog interface {
	CreateMaterial(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency string) (materials.Material, error)
	ListMaterials(ctx context.Context, companyID string) ([]materials.Material, error)
}

type SupplierDirectory interface {
	CreateSupplier(ctx context.Context, companyID, actorUserID string, input suppliers.CreateSupplierInput) (suppliers.Supplier, error)
	ListSuppliers(ctx context.Context, companyID string, filter suppliers.SupplierFilter) ([]suppliers.Supplier, error)
}

type demoMaterialSeed struct {
	Name, Category, Specification, Unit string
	ReferencePriceAmountMinor           int64 // MYR minor units (sen)
}

var demoMaterials = []demoMaterialSeed{
	{"Porcelain Floor Tile", "flooring", "600x600mm, matte finish", "sqm", 4500},
	{"Ceramic Wall Tile", "flooring", "300x600mm, glossy finish", "sqm", 2800},
	{"Tile Adhesive", "flooring", "20kg bag, C1 grade", "bag", 3200},
	{"Tile Grout", "flooring", "5kg bag, waterproof", "bag", 1800},
	{"Interior Paint", "painting", "5L, matte emulsion", "can", 8500},
	{"Primer", "painting", "5L, wall sealer", "can", 6500},
	{"Cement", "structural", "50kg bag, OPC Type I", "bag", 2200},
	{"Sand", "structural", "river sand, washed", "tonne", 8000},
	{"Plasterboard", "structural", "12mm, 1200x2400mm sheet", "sheet", 4200},
	{"Plywood", "structural", "18mm, 1220x2440mm sheet", "sheet", 9500},
	{"Kitchen Cabinet Hardware", "fittings", "soft-close hinge set", "set", 3500},
	{"Electrical Cable", "electrical", "2.5mm2 PVC, 100m roll", "roll", 25000},
	{"LED Downlight", "electrical", "9W, warm white, dimmable", "unit", 4500},
	{"Plumbing Pipe", "plumbing", "PVC 1 inch, 3m length", "length", 3800},
	{"Waterproofing Membrane", "plumbing", "liquid-applied, 20kg pail", "pail", 18000},
}

// EnsureMaterialCatalog creates every demo material not already present
// (matched by name), and returns the full catalog keyed by name. Idempotent:
// a rerun against a company that already has the catalog makes zero
// CreateMaterial calls.
func EnsureMaterialCatalog(ctx context.Context, catalog MaterialCatalog, companyID string) (map[string]materials.Material, error) {
	existing, err := catalog.ListMaterials(ctx, companyID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]materials.Material, len(demoMaterials))
	for _, m := range existing {
		result[m.Name] = m
	}
	for _, seed := range demoMaterials {
		if _, found := result[seed.Name]; found {
			continue
		}
		created, err := catalog.CreateMaterial(ctx, companyID, seed.Name, seed.Category, seed.Specification, seed.Unit, seed.ReferencePriceAmountMinor, "MYR")
		if err != nil {
			return nil, err
		}
		result[seed.Name] = created
	}
	return result, nil
}

type demoSupplierSeed struct {
	Name, ContactPerson, Email, Phone, Address string
	MaterialCategories                         []string
}

var demoSuppliers = []demoSupplierSeed{
	{"DemoBuild Materials Sdn Bhd", "Ahmad Faizal", "sales@demobuildmaterials.example.com", "+60 3-2201 4455", "Lot 12, Jalan Industri 3, 40000 Shah Alam, Selangor", []string{"structural"}},
	{"Metro Tile Supply Sdn Bhd", "Tan Wei Ming", "orders@metrotilesupply.example.com", "+60 3-7803 6621", "No. 8, Jalan PJU 5/6, 47810 Petaling Jaya, Selangor", []string{"flooring"}},
	{"ProFinish Hardware Sdn Bhd", "Siti Nurhaliza", "info@profinishhardware.example.com", "+60 3-4142 9903", "45 Jalan Meru, 41050 Klang, Selangor", []string{"fittings"}},
	{"BrightLine Electrical Supply Sdn Bhd", "Rajesh Kumar", "sales@brightlineelectrical.example.com", "+60 3-6250 1178", "22 Jalan Sungai Besi, 57100 Kuala Lumpur", []string{"electrical"}},
	{"AquaWorks Supply Sdn Bhd", "Lim Chee Keong", "orders@aquaworkssupply.example.com", "+60 3-8945 2266", "17 Jalan Kuchai Lama, 58200 Kuala Lumpur", []string{"plumbing"}},
}

// EnsureSupplierDirectory creates every demo supplier not already present
// (matched by name), and returns the full directory keyed by name.
// Idempotent: a rerun against a company that already has the directory
// makes zero CreateSupplier calls.
func EnsureSupplierDirectory(ctx context.Context, directory SupplierDirectory, companyID, actorUserID string) (map[string]suppliers.Supplier, error) {
	existing, err := directory.ListSuppliers(ctx, companyID, suppliers.SupplierFilter{})
	if err != nil {
		return nil, err
	}
	result := make(map[string]suppliers.Supplier, len(demoSuppliers))
	for _, s := range existing {
		result[s.Name] = s
	}
	for _, seed := range demoSuppliers {
		if _, found := result[seed.Name]; found {
			continue
		}
		created, err := directory.CreateSupplier(ctx, companyID, actorUserID, suppliers.CreateSupplierInput{
			Name: seed.Name, ContactPerson: seed.ContactPerson, Email: seed.Email,
			Phone: seed.Phone, Address: seed.Address, MaterialCategories: seed.MaterialCategories,
		})
		if err != nil {
			return nil, err
		}
		result[seed.Name] = created
	}
	return result, nil
}
