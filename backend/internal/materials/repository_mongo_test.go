package materials_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func TestMaterialRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "OPC Cement 50kg", Category: "cement", Unit: "bag",
		ReferencePrice: money.New(1850, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Name != "OPC Cement 50kg" {
		t.Fatalf("expected name to match, got %s", found.Name)
	}
	if found.ReferencePrice.Amount != 1850 || found.ReferencePrice.Currency != "MYR" {
		t.Fatalf("expected reference price 1850 MYR, got %d %s", found.ReferencePrice.Amount, found.ReferencePrice.Currency)
	}
}

func TestMaterialRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_tenant")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Ceramic Tile", Unit: "m2",
		ReferencePrice: money.New(3200, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != materials.ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestMaterialRepositoryList(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_list")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile", Unit: "m2",
		ReferencePrice: money.New(3200, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Cement", Unit: "bag",
		ReferencePrice: money.New(1850, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, materials.Material{
		CompanyID: "company_b", Name: "Other company's material", Unit: "unit",
		ReferencePrice: money.New(100, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})

	list, err := repo.List(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 materials for company_a, got %d", len(list))
	}
}

func TestMaterialRepositoryUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_update")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile", Unit: "m2",
		ReferencePrice: money.New(3200, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	newAsOf := time.Now()
	updated, err := repo.Update(ctx, "company_a", created.ID, func(m *materials.Material) {
		m.ReferencePrice = money.New(3500, "MYR")
		m.ReferencePriceAsOf = newAsOf
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.ReferencePrice.Amount != 3500 {
		t.Fatalf("expected updated reference price 3500, got %d", updated.ReferencePrice.Amount)
	}

	_, err = repo.Update(ctx, "company_b", created.ID, func(m *materials.Material) { m.Name = "hijacked" })
	if err != materials.ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound for cross-tenant update, got %v", err)
	}
}

func TestMaterialRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_indexes")
	repo := materials.NewMongoMaterialRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}

func TestMaterialRepositoryNilSourceSuggestionIDWorksForManualMaterials(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_provenance_nil")
	repo := materials.NewMongoMaterialRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		created, err := repo.Create(ctx, materials.Material{
			CompanyID: "company_a", Name: "Tile Adhesive", Unit: "bag",
			ReferencePrice: money.New(1000, "USD"), ReferencePriceAsOf: time.Now(),
			CreatedAt: time.Now(), SchemaVersion: 1,
		})
		if err != nil {
			t.Fatalf("unexpected error creating manual material %d: %v", i, err)
		}
		if created.SourceSuggestionID != nil {
			t.Fatalf("expected nil SourceSuggestionID for manual material, got %v", created.SourceSuggestionID)
		}
	}
}

func TestMaterialRepositoryAICreatedRoundTripsSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_provenance_roundtrip")
	repo := materials.NewMongoMaterialRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_1"
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile Spacers", Unit: "bag",
		ReferencePrice: money.New(500, "USD"), ReferencePriceAsOf: time.Now(),
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating AI material: %v", err)
	}
	if created.SourceSuggestionID == nil || *created.SourceSuggestionID != suggestionID {
		t.Fatalf("expected SourceSuggestionID %q, got %v", suggestionID, created.SourceSuggestionID)
	}

	found, err := repo.FindBySourceSuggestionID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error finding by source suggestion id: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindBySourceSuggestionID returned wrong material: got %s, want %s", found.ID, created.ID)
	}
}

func TestMaterialRepositorySparseUniqueIndexRejectsDuplicateSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_provenance_unique")
	repo := materials.NewMongoMaterialRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_dup"
	if _, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile Spacers", Unit: "bag",
		ReferencePrice: money.New(500, "USD"), ReferencePriceAsOf: time.Now(),
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("unexpected error creating first AI material: %v", err)
	}

	_, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile Spacers Again", Unit: "bag",
		ReferencePrice: money.New(600, "USD"), ReferencePriceAsOf: time.Now(),
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err == nil {
		t.Fatal("expected an error creating a second material with the same non-empty SourceSuggestionID")
	}
}

func TestMaterialRepositoryFindBySourceSuggestionIDIsTenantScoped(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_provenance_tenant")
	repo := materials.NewMongoMaterialRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_tenant"
	if _, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile Spacers", Unit: "bag",
		ReferencePrice: money.New(500, "USD"), ReferencePriceAsOf: time.Now(),
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("unexpected error creating AI material: %v", err)
	}

	_, err := repo.FindBySourceSuggestionID(ctx, "company_b", suggestionID)
	if err != materials.ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound for cross-tenant lookup, got %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). materials has no parent-lookup dependency, so
// this seeds through the real Service, matching the plan's own template.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_deleteall_service")
	repo := materials.NewMongoMaterialRepository(db)
	svc := materials.NewService(repo)
	ctx := context.Background()

	if _, err := svc.CreateMaterial(ctx, "company_a", "A1", "flooring", "", "sqm", 4500, "MYR"); err != nil {
		t.Fatalf("create company_a material: %v", err)
	}
	if _, err := svc.CreateMaterial(ctx, "company_b", "B1", "flooring", "", "sqm", 4500, "MYR"); err != nil {
		t.Fatalf("create company_b material: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := svc.ListMaterials(ctx, "company_a")
	if err != nil {
		t.Fatalf("ListMaterials company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a materials, got %d", len(listA))
	}

	listB, err := svc.ListMaterials(ctx, "company_b")
	if err != nil {
		t.Fatalf("ListMaterials company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's material to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_deleteall_empty")
	repo := materials.NewMongoMaterialRepository(db)
	svc := materials.NewService(repo)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
