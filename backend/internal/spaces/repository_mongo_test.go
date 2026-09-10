package spaces_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
)

func defaultSpaceListRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", spaces.SpaceSortFields, spaces.SpaceDefaultSort, spaces.SpaceDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

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

func TestSpaceRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, spaces.Space{
		CompanyID: "company_a", ProjectID: "project_1", Name: "Master Bathroom",
		Type: "bathroom", CreatedAt: time.Now(), SchemaVersion: 1,
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
	if found.Name != "Master Bathroom" {
		t.Fatalf("expected Master Bathroom, got %s", found.Name)
	}
}

func TestSpaceRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_tenant")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != spaces.ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestSpaceRepositoryListPaginatedOnlyReturnsOwnProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_list")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen", CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Bathroom", CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_2", Name: "Living Room", CreatedAt: time.Now(), SchemaVersion: 1})

	list, total, err := repo.ListPaginated(ctx, "company_a", "project_1", defaultSpaceListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 spaces for project_1, got %d", len(list))
	}
	for _, s := range list {
		if s.ProjectID != "project_1" {
			t.Fatalf("expected only project_1 spaces, got %s", s.ProjectID)
		}
	}
}

func TestSpaceRepositoryListPaginatedOrdersByCreatedAtAscByDefault(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_order_default")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	first, err := repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "First", CreatedAt: base, SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	second, err := repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Second", CreatedAt: base.Add(time.Minute), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, _, err := repo.ListPaginated(ctx, "company_a", "project_1", defaultSpaceListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("expected oldest-first order [%s, %s], got [%s, %s]", first.ID, second.ID, list[0].ID, list[1].ID)
	}
}

func TestSpaceRepositoryListPaginatedSearchMatchesNameTypeDescription(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_search")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Master Bathroom", Type: "bathroom", Description: "full renovation", CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Guest Bedroom", Type: "bedroom", Description: "paint only", CreatedAt: time.Now(), SchemaVersion: 1})

	req, err := pagination.ParseRequest(0, 0, "renovation", "", "", spaces.SpaceSortFields, spaces.SpaceDefaultSort, spaces.SpaceDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := repo.ListPaginated(ctx, "company_a", "project_1", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Name != "Master Bathroom" {
		t.Fatalf("total=%d list=%+v, want 1 match on Master Bathroom", total, list)
	}
}

func TestSpaceRepositoryListPaginatedTenantIsolation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_tenant_isolation")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "A", CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, spaces.Space{CompanyID: "company_b", ProjectID: "project_1", Name: "B", CreatedAt: time.Now(), SchemaVersion: 1})

	list, total, err := repo.ListPaginated(ctx, "company_a", "project_1", defaultSpaceListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1 (must exclude company_b)", total, len(list))
	}
}

func TestSpaceRepositoryUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_update")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Old Name", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(s *spaces.Space) {
		s.Name = "New Name"
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected New Name, got %s", updated.Name)
	}
}

func TestSpaceRepositoryBelongsToProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_belongs")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	belongs, err := repo.BelongsToProject(ctx, "company_a", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true for correct company+project")
	}

	// Same company, but this Space actually belongs to a different Project —
	// this is the lineage-mismatch case design spec §10.3 flags: same tenant,
	// wrong Project.
	belongs, err = repo.BelongsToProject(ctx, "company_a", created.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false — space belongs to project_1, not project_2")
	}

	belongs, err = repo.BelongsToProject(ctx, "company_b", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false for wrong company")
	}
}

func TestSpaceRepositoryNilSourceSuggestionIDWorksForManualSpaces(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_provenance_nil")
	repo := spaces.NewMongoSpaceRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, spaces.Space{
		CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen",
		Type: "kitchen", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating manual space: %v", err)
	}
	if created.SourceSuggestionID != nil {
		t.Fatalf("expected nil SourceSuggestionID for manual space, got %v", created.SourceSuggestionID)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.SourceSuggestionID != nil {
		t.Fatalf("expected nil SourceSuggestionID after round-trip, got %v", found.SourceSuggestionID)
	}
}

func TestSpaceRepositoryAICreatedSpaceRoundTripsSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_provenance_roundtrip")
	repo := spaces.NewMongoSpaceRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_1"
	created, err := repo.Create(ctx, spaces.Space{
		CompanyID: "company_a", ProjectID: "project_1", Name: "Master Bathroom",
		Type: "bathroom", SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating AI space: %v", err)
	}
	if created.SourceSuggestionID == nil || *created.SourceSuggestionID != suggestionID {
		t.Fatalf("expected SourceSuggestionID %q, got %v", suggestionID, created.SourceSuggestionID)
	}

	found, err := repo.FindBySourceSuggestionID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error finding by source suggestion id: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindBySourceSuggestionID returned wrong space: got %s, want %s", found.ID, created.ID)
	}
}

func TestSpaceRepositorySparseUniqueIndexRejectsDuplicateSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_provenance_unique")
	repo := spaces.NewMongoSpaceRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_dup"
	if _, err := repo.Create(ctx, spaces.Space{
		CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen",
		Type: "kitchen", SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("unexpected error creating first AI space: %v", err)
	}

	_, err := repo.Create(ctx, spaces.Space{
		CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen Again",
		Type: "kitchen", SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err == nil {
		t.Fatal("expected an error creating a second space with the same non-empty SourceSuggestionID")
	}
}

func TestSpaceRepositoryFindBySourceSuggestionIDIsTenantScoped(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_provenance_tenant")
	repo := spaces.NewMongoSpaceRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_tenant"
	if _, err := repo.Create(ctx, spaces.Space{
		CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen",
		Type: "kitchen", SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("unexpected error creating AI space: %v", err)
	}

	_, err := repo.FindBySourceSuggestionID(ctx, "company_b", suggestionID)
	if err != spaces.ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestSpaceRepositoryExistingRecordsWithoutSourceSuggestionIDUnaffected(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_provenance_existing")
	repo := spaces.NewMongoSpaceRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, spaces.Space{
			CompanyID: "company_a", ProjectID: "project_1", Name: "Manual Space",
			Type: "other", CreatedAt: time.Now(), SchemaVersion: 1,
		}); err != nil {
			t.Fatalf("unexpected error creating manual space %d: %v", i, err)
		}
	}

	list, total, err := repo.ListPaginated(ctx, "company_a", "project_1", defaultSpaceListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("total=%d len(list)=%d, want 3/3 — multiple nil SourceSuggestionID spaces must coexist", total, len(list))
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) since CreateSpace would otherwise
// require a real Project for projectLookup.ProjectBelongsToCompany.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_deleteall_service")
	repo := spaces.NewMongoSpaceRepository(db)
	svc := spaces.NewService(repo, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, spaces.Space{CompanyID: "company_a", ProjectID: "project_1", Name: "A1", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_a space: %v", err)
	}
	if _, err := repo.Create(ctx, spaces.Space{CompanyID: "company_b", ProjectID: "project_2", Name: "B1", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_b space: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	_, total, err := repo.ListPaginated(ctx, "company_a", "project_1", defaultSpaceListRequest(t))
	if err != nil {
		t.Fatalf("ListPaginated company_a: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 remaining company_a spaces, got %d", total)
	}

	_, total, err = repo.ListPaginated(ctx, "company_b", "project_2", defaultSpaceListRequest(t))
	if err != nil {
		t.Fatalf("ListPaginated company_b: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected company_b's space to be untouched, got %d", total)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "spaces_test_deleteall_empty")
	repo := spaces.NewMongoSpaceRepository(db)
	svc := spaces.NewService(repo, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
