package workresources_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
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

func newTradeRequirement(suggestionID *string) workresources.WorkResourceRequirement {
	return workresources.WorkResourceRequirement{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_1",
		ResourceType: workresources.ResourceTypeTrade, Name: "Tiler",
		Status: workresources.StatusConfirmed,
		Source: func() workresources.Source {
			if suggestionID != nil {
				return workresources.SourceAISuggestion
			}
			return workresources.SourceManual
		}(),
		SourceSuggestionID: suggestionID,
		CreatedByUserID:    "user_1", CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}
}

func TestRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	created, err := repo.Create(ctx, newTradeRequirement(nil))
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
	if found.Name != "Tiler" {
		t.Fatalf("expected Name Tiler, got %s", found.Name)
	}
}

func TestRepositoryFindByIDCrossTenantNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_tenant")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	created, err := repo.Create(ctx, newTradeRequirement(nil))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != workresources.ErrRequirementNotFound {
		t.Fatalf("expected ErrRequirementNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestRepositoryNilSourceSuggestionIDWorksForManualRequirements(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_provenance_nil")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		r := newTradeRequirement(nil)
		r.WorkItemID = "work_manual"
		r.Name = "Painter"
		created, err := repo.Create(ctx, r)
		if err != nil {
			t.Fatalf("unexpected error creating manual requirement %d: %v", i, err)
		}
		if created.SourceSuggestionID != nil {
			t.Fatalf("expected nil SourceSuggestionID for manual requirement, got %v", created.SourceSuggestionID)
		}
	}
}

func TestRepositoryAICreatedRoundTripsSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_provenance_roundtrip")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_1"
	created, err := repo.Create(ctx, newTradeRequirement(&suggestionID))
	if err != nil {
		t.Fatalf("unexpected error creating AI requirement: %v", err)
	}

	found, err := repo.FindBySourceSuggestionID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error finding by source suggestion id: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindBySourceSuggestionID returned wrong requirement: got %s, want %s", found.ID, created.ID)
	}
}

func TestRepositorySparseUniqueIndexRejectsDuplicateSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_provenance_unique")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_dup"
	if _, err := repo.Create(ctx, newTradeRequirement(&suggestionID)); err != nil {
		t.Fatalf("unexpected error creating first AI requirement: %v", err)
	}

	r2 := newTradeRequirement(&suggestionID)
	r2.WorkItemID = "work_other"
	if _, err := repo.Create(ctx, r2); err == nil {
		t.Fatal("expected an error creating a second requirement with the same non-empty SourceSuggestionID")
	}
}

func TestRepositoryFindDuplicateKeyMaterial(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_dupkey_material")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	materialID := "material_1"
	r := workresources.WorkResourceRequirement{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_1",
		ResourceType: workresources.ResourceTypeMaterial, MaterialID: &materialID, Name: "Tile Adhesive",
		Status: workresources.StatusConfirmed, Source: workresources.SourceManual,
		CreatedByUserID: "user_1", CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}
	if _, err := repo.Create(ctx, r); err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByDuplicateKey(ctx, workresources.DuplicateKey{
		CompanyID: "company_a", WorkItemID: "work_1",
		ResourceType: workresources.ResourceTypeMaterial, MaterialID: materialID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Name != "Tile Adhesive" {
		t.Fatalf("expected Tile Adhesive, got %s", found.Name)
	}

	_, err = repo.FindByDuplicateKey(ctx, workresources.DuplicateKey{
		CompanyID: "company_a", WorkItemID: "work_1",
		ResourceType: workresources.ResourceTypeMaterial, MaterialID: "material_other",
	})
	if err != workresources.ErrRequirementNotFound {
		t.Fatalf("expected ErrRequirementNotFound for a different materialId, got %v", err)
	}
}

func TestRepositoryFindDuplicateKeyTradeNormalizedName(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_dupkey_trade")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	r := newTradeRequirement(nil)
	if _, err := repo.Create(ctx, r); err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByDuplicateKey(ctx, workresources.DuplicateKey{
		CompanyID: "company_a", WorkItemID: "work_1",
		ResourceType: workresources.ResourceTypeTrade, NormalizedName: "tiler",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Name != "Tiler" {
		t.Fatalf("expected Tiler, got %s", found.Name)
	}
}

func TestRepositoryListByProjectAndByWorkItem(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_list")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	r1 := newTradeRequirement(nil)
	r1.WorkItemID = "work_1"
	r2 := newTradeRequirement(nil)
	r2.WorkItemID = "work_2"
	r2.Name = "Painter"
	if _, err := repo.Create(ctx, r1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.Create(ctx, r2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byProject, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 requirements for project_1, got %d", len(byProject))
	}

	byWorkItem, err := repo.ListByWorkItem(ctx, "company_a", "work_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(byWorkItem) != 1 || byWorkItem[0].Name != "Tiler" {
		t.Fatalf("expected 1 requirement (Tiler) for work_1, got %+v", byWorkItem)
	}
}
