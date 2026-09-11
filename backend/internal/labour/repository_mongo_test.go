package labour_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

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

func TestWorkerRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker")
	repo := labour.NewMongoWorkerRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.Worker{
		CompanyID: "company_a", Name: "Ahmad", Trade: "Tiler", RateType: labour.RateTypeDaily,
		DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1,
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
	if found.Name != "Ahmad" || found.DefaultRate.Amount != 15000 {
		t.Fatalf("expected Ahmad/15000, got %s/%d", found.Name, found.DefaultRate.Amount)
	}
}

func TestWorkerRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker_tenant")
	repo := labour.NewMongoWorkerRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.Worker{
		CompanyID: "company_a", Name: "Ahmad", RateType: labour.RateTypeDaily,
		DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != labour.ErrWorkerNotFound {
		t.Fatalf("expected ErrWorkerNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestWorkerRepositoryListAndUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker_list")
	repo := labour.NewMongoWorkerRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.Worker{
		CompanyID: "company_a", Name: "Ahmad", RateType: labour.RateTypeDaily,
		DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	list, err := repo.List(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(list))
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(w *labour.Worker) {
		w.DefaultRate = money.New(18000, "MYR")
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.DefaultRate.Amount != 18000 {
		t.Fatalf("expected updated rate 18000, got %d", updated.DefaultRate.Amount)
	}
}

func TestWorkerRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker_indexes")
	repo := labour.NewMongoWorkerRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}

func TestLabourEntryRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	workerID := "worker_1"
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1", WorkerID: &workerID,
		WorkerName: "Ahmad", Trade: "Tiler", Quantity: mustQuantity(t, "8", "day"),
		Rate: money.New(15000, "MYR"), Cost: money.New(1200000, "MYR"), CostItemID: "cost_item_1",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
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
	if found.Cost.Amount != 1200000 {
		t.Fatalf("expected cost 1200000, got %d", found.Cost.Amount)
	}
	if found.CostItemID != "cost_item_1" {
		t.Fatalf("expected costItemId cost_item_1, got %s", found.CostItemID)
	}
}

func TestLabourEntryRepositoryQuantityRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_quantity")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Ad-hoc labourer", Trade: "General", Quantity: mustQuantity(t, "8.5", "hour"),
		Rate: money.New(2500, "MYR"), Cost: money.New(21250, "MYR"), CostItemID: "cost_item_2",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	expected := decimal.RequireFromString("8.5")
	if !found.Quantity.Value.Equal(expected) {
		t.Fatalf("expected exact decimal 8.5, got %s", found.Quantity.Value.String())
	}
	if found.Quantity.Unit != "hour" {
		t.Fatalf("expected unit hour, got %s", found.Quantity.Unit)
	}
}

func TestLabourEntryRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_tenant")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Demo", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(15000, "MYR"),
		Cost: money.New(15000, "MYR"), CostItemID: "cost_item_3",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != labour.ErrLabourEntryNotFound {
		t.Fatalf("expected ErrLabourEntryNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestLabourEntryRepositoryListByProjectAndByWorkItem(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_list")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "A", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "cost_item_a",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_2",
		WorkerName: "B", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "cost_item_b",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})

	byProject, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing by project: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 labour entries for project_1, got %d", len(byProject))
	}

	byWorkItem, err := repo.ListByWorkItem(ctx, "company_a", "work_item_1")
	if err != nil {
		t.Fatalf("unexpected error listing by work item: %v", err)
	}
	if len(byWorkItem) != 1 {
		t.Fatalf("expected 1 labour entry for work_item_1, got %d", len(byWorkItem))
	}
}

func TestLabourEntryRepositoryUpdateCorrection(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_correction")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Ahmad", Quantity: mustQuantity(t, "8", "hour"), Rate: money.New(2500, "MYR"),
		Cost: money.New(20000, "MYR"), CostItemID: "cost_item_correction",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	newQty := mustQuantity(t, "80", "hour") // the typo-correction scenario from the design spec
	newDate := time.Now()
	updated, err := repo.UpdateCorrection(ctx, "company_a", created.ID, newQty, money.New(2500, "MYR"), money.New(200000, "MYR"), newDate, "corrected typo")
	if err != nil {
		t.Fatalf("unexpected error correcting: %v", err)
	}
	if !updated.Quantity.Value.Equal(decimal.RequireFromString("80")) {
		t.Fatalf("expected corrected quantity 80, got %s", updated.Quantity.Value.String())
	}
	if updated.Cost.Amount != 200000 {
		t.Fatalf("expected corrected cost 200000, got %d", updated.Cost.Amount)
	}
	if updated.CostItemID != "cost_item_correction" {
		t.Fatalf("expected costItemId to remain unchanged, got %s", updated.CostItemID)
	}
}

func TestLabourEntryRepositoryCostItemIDUniqueIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_unique_costitem")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "First", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "shared_cost_item",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating first entry: %v", err)
	}

	_, err = repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_2",
		WorkerName: "Second", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "shared_cost_item", // same costItemId — must be rejected
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err == nil {
		t.Fatal("expected duplicate-key error for second LabourEntry with the same costItemId, got nil")
	}
}

func TestLabourEntryRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_indexes")
	repo := labour.NewMongoLabourEntryRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes BOTH
// Worker and LabourEntry records for companyID. Seeds via both
// repositories directly (matching every sibling test in this file).
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_deleteall_service")
	workerRepo := labour.NewMongoWorkerRepository(db)
	entryRepo := labour.NewMongoLabourEntryRepository(db)
	svc := labour.NewService(workerRepo, entryRepo, nil, nil, nil)
	ctx := context.Background()

	if _, err := workerRepo.Create(ctx, labour.Worker{CompanyID: "company_a", Name: "Worker A", RateType: labour.RateTypeDaily, DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_a worker: %v", err)
	}
	if _, err := workerRepo.Create(ctx, labour.Worker{CompanyID: "company_b", Name: "Worker B", RateType: labour.RateTypeDaily, DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_b worker: %v", err)
	}
	if _, err := entryRepo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Worker A", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(15000, "MYR"),
		Cost: money.New(15000, "MYR"), CostItemID: "cost_item_a", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("create company_a labour entry: %v", err)
	}
	if _, err := entryRepo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_b", ProjectID: "project_2", WorkItemID: "work_item_2",
		WorkerName: "Worker B", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(15000, "MYR"),
		Cost: money.New(15000, "MYR"), CostItemID: "cost_item_b", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("create company_b labour entry: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	workersA, err := workerRepo.List(ctx, "company_a")
	if err != nil {
		t.Fatalf("List workers company_a: %v", err)
	}
	if len(workersA) != 0 {
		t.Fatalf("expected 0 remaining company_a workers, got %d", len(workersA))
	}
	entriesA, err := entryRepo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(entriesA) != 0 {
		t.Fatalf("expected 0 remaining company_a labour entries, got %d", len(entriesA))
	}

	workersB, err := workerRepo.List(ctx, "company_b")
	if err != nil {
		t.Fatalf("List workers company_b: %v", err)
	}
	if len(workersB) != 1 {
		t.Fatalf("expected company_b's worker to be untouched, got %d", len(workersB))
	}
	entriesB, err := entryRepo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(entriesB) != 1 {
		t.Fatalf("expected company_b's labour entry to be untouched, got %d", len(entriesB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_deleteall_empty")
	workerRepo := labour.NewMongoWorkerRepository(db)
	entryRepo := labour.NewMongoLabourEntryRepository(db)
	svc := labour.NewService(workerRepo, entryRepo, nil, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
