package work_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

func defaultWorkItemListRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", work.WorkItemSortFields, work.WorkItemDefaultSort, work.WorkItemDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

func setupMongoDB(t *testing.T) *mongo.Client {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
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

func mustQuantity(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("unexpected error constructing quantity: %v", err)
	}
	return q
}

func TestWorkItemRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Install ceramic floor tiles",
		WorkType: "tile_installation", Quantity: mustQuantity(t, "30", "m2"),
		Status: work.WorkItemStatusPlanned, Source: work.WorkItemSourceManual,
		VerificationStatus: work.VerificationStatusConfirmed, CreatedAt: time.Now(), SchemaVersion: 1,
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
	if found.Description != "Install ceramic floor tiles" {
		t.Fatalf("expected description to match, got %s", found.Description)
	}
}

func TestWorkItemRepositoryQuantityRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_quantity")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Waterproofing",
		Quantity: mustQuantity(t, "32.7501", "m2"),
		Status:   work.WorkItemStatusPlanned, Source: work.WorkItemSourceManual,
		VerificationStatus: work.VerificationStatusConfirmed, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	expected := decimal.RequireFromString("32.7501")
	if !found.Quantity.Value.Equal(expected) {
		t.Fatalf("expected exact decimal 32.7501, got %s (precision loss through Mongo round-trip)", found.Quantity.Value.String())
	}
	if found.Quantity.Unit != "m2" {
		t.Fatalf("expected unit m2, got %s", found.Quantity.Unit)
	}
}

func TestWorkItemRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_tenant")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Demo",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != work.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestWorkItemRepositoryListPaginatedByProjectAndBySpace(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_list")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	spaceID := "space_1"
	_, _ = repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: &spaceID, Description: "With space",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Project-level, no space",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})

	byProject, projectTotal, err := repo.ListPaginated(ctx, "company_a", "project_1", "", defaultWorkItemListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing by project: %v", err)
	}
	if projectTotal != 2 {
		t.Fatalf("total = %d, want 2", projectTotal)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 work items for project_1, got %d", len(byProject))
	}

	bySpace, spaceTotal, err := repo.ListPaginated(ctx, "company_a", "", "space_1", defaultWorkItemListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing by space: %v", err)
	}
	if spaceTotal != 1 {
		t.Fatalf("total = %d, want 1", spaceTotal)
	}
	if len(bySpace) != 1 {
		t.Fatalf("expected 1 work item for space_1, got %d", len(bySpace))
	}
}

func TestWorkItemRepositoryListPaginatedOrdersByCreatedAtAscByDefault(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_order_default")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	first, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "First",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: base, SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	second, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Second",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: base.Add(time.Minute), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, _, err := repo.ListPaginated(ctx, "company_a", "project_1", "", defaultWorkItemListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("expected oldest-first order [%s, %s], got [%s, %s]", first.ID, second.ID, list[0].ID, list[1].ID)
	}
}

func TestWorkItemRepositoryListPaginatedSearchMatchesDescriptionAndWorkType(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_search")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Install ceramic tiles", WorkType: "tiling",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Paint walls", WorkType: "painting",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})

	req, err := pagination.ParseRequest(0, 0, "tiling", "", "", work.WorkItemSortFields, work.WorkItemDefaultSort, work.WorkItemDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := repo.ListPaginated(ctx, "company_a", "project_1", "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Description != "Install ceramic tiles" {
		t.Fatalf("total=%d list=%+v, want 1 match on Install ceramic tiles", total, list)
	}
}

func TestWorkItemRepositoryListPaginatedTenantIsolation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_tenant_isolation")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "A",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, work.WorkItem{
		CompanyID: "company_b", ProjectID: "project_1", Description: "B",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})

	list, total, err := repo.ListPaginated(ctx, "company_a", "project_1", "", defaultWorkItemListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1 (must exclude company_b)", total, len(list))
	}
}

func TestWorkItemRepositoryUpdateStatus(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_status")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Demo",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.UpdateStatus(ctx, "company_a", created.ID, work.WorkItemStatusCancelled)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}
	if updated.Status != work.WorkItemStatusCancelled {
		t.Fatalf("expected cancelled, got %s", updated.Status)
	}

	_, err = repo.UpdateStatus(ctx, "company_b", created.ID, work.WorkItemStatusPlanned)
	if err != work.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for cross-tenant update, got %v", err)
	}
}

func TestWorkItemRepositoryUpdatePersistsDescriptionWorkTypeAndQuantity(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_update_fields")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Old", WorkType: "old-type",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(w *work.WorkItem) {
		w.Description = "New"
		w.WorkType = "new-type"
		w.Quantity = mustQuantity(t, "12.75", "m")
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.Description != "New" {
		t.Fatalf("Description = %q, want New", updated.Description)
	}
	if updated.WorkType != "new-type" {
		t.Fatalf("WorkType = %q, want new-type", updated.WorkType)
	}
	if updated.Quantity.Value.String() != "12.75" || updated.Quantity.Unit != "m" {
		t.Fatalf("Quantity = %s %s, want 12.75 m", updated.Quantity.Value.String(), updated.Quantity.Unit)
	}

	// Round-trip via a fresh FindByID to prove the write was actually
	// persisted, not just returned in-memory.
	reloaded, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error reloading: %v", err)
	}
	if reloaded.Description != "New" || reloaded.Quantity.Value.String() != "12.75" {
		t.Fatalf("reloaded document did not reflect the update: %+v", reloaded)
	}
}

func TestWorkItemRepositoryUpdateClearsSpaceIDViaUnset(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_update_clear_space")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	spaceID := "space_1"
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: &spaceID, Description: "Demo",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(w *work.WorkItem) {
		w.SpaceID = nil
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.SpaceID != nil {
		t.Fatalf("SpaceID = %v, want cleared to nil", updated.SpaceID)
	}

	reloaded, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error reloading: %v", err)
	}
	if reloaded.SpaceID != nil {
		t.Fatalf("reloaded SpaceID = %v, want nil (field must actually be unset in Mongo)", reloaded.SpaceID)
	}
}

func TestWorkItemRepositoryUpdateCrossTenantReturnsNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_update_cross_tenant")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Demo",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.Update(ctx, "company_b", created.ID, func(w *work.WorkItem) {
		w.Description = "Hijacked"
	})
	if err != work.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for cross-tenant update, got %v", err)
	}
}

func TestWorkItemRepositoryBelongsToProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_belongs_to_project")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Demo",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	belongs, err := repo.BelongsToProject(ctx, "company_a", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true for matching company+project")
	}

	wrongProject, err := repo.BelongsToProject(ctx, "company_a", created.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wrongProject {
		t.Fatal("expected false when projectID does not match the WorkItem's own ProjectID")
	}

	wrongCompany, err := repo.BelongsToProject(ctx, "company_b", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wrongCompany {
		t.Fatal("expected false when companyID does not match")
	}
}

func TestWorkItemRepositoryNilSourceSuggestionIDWorksForManualWorkItems(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_provenance_nil")
	repo := work.NewMongoWorkItemRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		created, err := repo.Create(ctx, work.WorkItem{
			CompanyID: "company_a", ProjectID: "project_1", Description: "Manual item",
			Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
			Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
			CreatedAt: time.Now(), SchemaVersion: 1,
		})
		if err != nil {
			t.Fatalf("unexpected error creating manual work item %d: %v", i, err)
		}
		if created.SourceSuggestionID != nil {
			t.Fatalf("expected nil SourceSuggestionID for manual work item, got %v", created.SourceSuggestionID)
		}
	}
}

func TestWorkItemRepositoryAICreatedRoundTripsSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_provenance_roundtrip")
	repo := work.NewMongoWorkItemRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_1"
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Install tiles",
		Quantity: mustQuantity(t, "30", "m2"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceAISuggestion, VerificationStatus: work.VerificationStatusConfirmed,
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating AI work item: %v", err)
	}
	if created.SourceSuggestionID == nil || *created.SourceSuggestionID != suggestionID {
		t.Fatalf("expected SourceSuggestionID %q, got %v", suggestionID, created.SourceSuggestionID)
	}

	found, err := repo.FindBySourceSuggestionID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error finding by source suggestion id: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindBySourceSuggestionID returned wrong work item: got %s, want %s", found.ID, created.ID)
	}
}

func TestWorkItemRepositorySparseUniqueIndexRejectsDuplicateSourceSuggestionID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_provenance_unique")
	repo := work.NewMongoWorkItemRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_dup"
	if _, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Install tiles",
		Quantity: mustQuantity(t, "30", "m2"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceAISuggestion, VerificationStatus: work.VerificationStatusConfirmed,
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("unexpected error creating first AI work item: %v", err)
	}

	_, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Install tiles again",
		Quantity: mustQuantity(t, "10", "m2"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceAISuggestion, VerificationStatus: work.VerificationStatusConfirmed,
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err == nil {
		t.Fatal("expected an error creating a second work item with the same non-empty SourceSuggestionID")
	}
}

func TestWorkItemRepositoryFindBySourceSuggestionIDIsTenantScoped(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_provenance_tenant")
	repo := work.NewMongoWorkItemRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	suggestionID := "suggestion_tenant"
	if _, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Install tiles",
		Quantity: mustQuantity(t, "30", "m2"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceAISuggestion, VerificationStatus: work.VerificationStatusConfirmed,
		SourceSuggestionID: &suggestionID, CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("unexpected error creating AI work item: %v", err)
	}

	_, err := repo.FindBySourceSuggestionID(ctx, "company_b", suggestionID)
	if err != work.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for cross-tenant lookup, got %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) since CreateWorkItem would otherwise
// require real Project/Space lookups.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_deleteall_service")
	repo := work.NewMongoWorkItemRepository(db)
	svc := work.NewService(repo, nil, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "A1",
		Quantity: mustQuantity(t, "10", "m2"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("create company_a work item: %v", err)
	}
	if _, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_b", ProjectID: "project_2", Description: "B1",
		Quantity: mustQuantity(t, "10", "m2"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("create company_b work item: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	_, total, err := repo.ListPaginated(ctx, "company_a", "project_1", "", defaultWorkItemListRequest(t))
	if err != nil {
		t.Fatalf("ListPaginated company_a: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 remaining company_a work items, got %d", total)
	}

	_, total, err = repo.ListPaginated(ctx, "company_b", "project_2", "", defaultWorkItemListRequest(t))
	if err != nil {
		t.Fatalf("ListPaginated company_b: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected company_b's work item to be untouched, got %d", total)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_deleteall_empty")
	repo := work.NewMongoWorkItemRepository(db)
	svc := work.NewService(repo, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
