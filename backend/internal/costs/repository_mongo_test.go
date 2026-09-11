package costs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
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

func TestCostItemRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(450000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategorySubcontractor,
		Description: "Bathroom plumbing upgrade", Estimated: &estimated, Currency: "MYR",
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
	if found.Description != "Bathroom plumbing upgrade" {
		t.Fatalf("expected description to match, got %s", found.Description)
	}
	if found.Estimated == nil || found.Estimated.Amount != 450000 {
		t.Fatalf("expected estimated 450000, got %+v", found.Estimated)
	}
	if found.Committed != nil || found.Actual != nil || found.Paid != nil {
		t.Fatalf("expected only Estimated set, got Committed=%+v Actual=%+v Paid=%+v", found.Committed, found.Actual, found.Paid)
	}
}

func TestCostItemRepositoryMoneyRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_money_roundtrip")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(100000, "MYR")
	committed := money.New(95000, "MYR")
	actual := money.New(108000, "MYR")
	paid := money.New(50000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "All four stages", Estimated: &estimated, Committed: &committed, Actual: &actual, Paid: &paid,
		Currency: "MYR", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Estimated == nil || found.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated 100000, got %+v", found.Estimated)
	}
	if found.Committed == nil || found.Committed.Amount != 95000 {
		t.Fatalf("expected committed 95000, got %+v", found.Committed)
	}
	if found.Actual == nil || found.Actual.Amount != 108000 {
		t.Fatalf("expected actual 108000, got %+v", found.Actual)
	}
	if found.Paid == nil || found.Paid.Amount != 50000 {
		t.Fatalf("expected paid 50000, got %+v", found.Paid)
	}
}

func TestCostItemRepositoryQuantityRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_quantity_roundtrip")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	q := mustQuantity(t, "32.7501", "m2")
	unitPrice := money.New(3200, "MYR")
	estimated := money.New(104800, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMaterial,
		Description: "Ceramic tiles", Quantity: &q, UnitPrice: &unitPrice, Estimated: &estimated,
		Currency: "MYR", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	expected := decimal.RequireFromString("32.7501")
	if found.Quantity == nil || !found.Quantity.Value.Equal(expected) {
		t.Fatalf("expected exact decimal 32.7501, got %+v", found.Quantity)
	}
	if found.Quantity.Unit != "m2" {
		t.Fatalf("expected unit m2, got %s", found.Quantity.Unit)
	}
}

func TestCostItemRepositoryLumpSumNoQuantity(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_lumpsum")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(150000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryPermit,
		Description: "Renovation permit", Estimated: &estimated,
		Currency: "MYR", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Quantity != nil {
		t.Fatalf("expected nil Quantity for lump-sum cost item, got %+v", found.Quantity)
	}
	if found.UnitPrice != nil {
		t.Fatalf("expected nil UnitPrice for lump-sum cost item, got %+v", found.UnitPrice)
	}
}

func TestCostItemRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_tenant")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(1000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Demo", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestCostItemRepositoryListByProjectAndByWorkItem(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_list")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	workItemID := "work_item_1"
	estimated := money.New(1000, "MYR")
	_, _ = repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: &workItemID, Category: costs.CostCategoryEquipment,
		Description: "With work item", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryPermit,
		Description: "Project-level, no work item", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})

	byProject, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing by project: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 cost items for project_1, got %d", len(byProject))
	}

	byWorkItem, err := repo.ListByWorkItem(ctx, "company_a", "work_item_1")
	if err != nil {
		t.Fatalf("unexpected error listing by work item: %v", err)
	}
	if len(byWorkItem) != 1 {
		t.Fatalf("expected 1 cost item for work_item_1, got %d", len(byWorkItem))
	}
}

func TestCostItemRepositoryUpdateLifecycleField(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_lifecycle")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(100000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Demo", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if created.Revision != 0 {
		t.Fatalf("a new cost item must start at Revision 0, got %d", created.Revision)
	}

	updated, err := repo.UpdateLifecycleField(ctx, "company_a", created.ID, created.Revision, costs.CostStageActual, money.New(108000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error updating lifecycle field: %v", err)
	}
	if updated.Actual == nil || updated.Actual.Amount != 108000 {
		t.Fatalf("expected actual 108000, got %+v", updated.Actual)
	}
	if updated.Estimated == nil || updated.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated to remain 100000, got %+v", updated.Estimated)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected revision 1 after one write, got %d", updated.Revision)
	}

	_, err = repo.UpdateLifecycleField(ctx, "company_b", created.ID, updated.Revision, costs.CostStagePaid, money.New(1, "MYR"))
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound for cross-tenant update, got %v", err)
	}

	_, err = repo.UpdateLifecycleField(ctx, "company_a", created.ID, created.Revision, costs.CostStagePaid, money.New(1, "MYR"))
	if !errors.Is(err, costs.ErrRevisionMismatch) {
		t.Fatalf("expected ErrRevisionMismatch for a stale expectedRevision, got %v", err)
	}
}

// A document written before Revision existed has no "revision" field at all
// in Mongo — not revision:0, genuinely absent. expectedRevision:0 must still
// match it, and the write must set revision:1, or every pre-existing cost
// item's first edit would incorrectly report a stale-revision conflict.
func TestCostItemRepositoryUpdateLifecycleFieldTreatsAMissingRevisionFieldAsZero(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_lifecycle_legacy")
	repo := costs.NewMongoCostItemRepository(db)
	collection := db.Collection("cost_items")

	ctx := context.Background()
	estimated := money.New(100000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Legacy row", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	objID, err := bson.ObjectIDFromHex(created.ID)
	if err != nil {
		t.Fatalf("unexpected error parsing id: %v", err)
	}
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": objID}, bson.M{"$unset": bson.M{"revision": ""}}); err != nil {
		t.Fatalf("unexpected error simulating a legacy document: %v", err)
	}

	updated, err := repo.UpdateLifecycleField(ctx, "company_a", created.ID, 0, costs.CostStageActual, money.New(85000, "MYR"))
	if err != nil {
		t.Fatalf("expectedRevision:0 must match a document with no revision field at all, got: %v", err)
	}
	if updated.Revision != 1 {
		t.Fatalf("Revision after first write on a legacy document = %d, want 1", updated.Revision)
	}
}

func TestCostItemRepositoryUpdateDetails(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_details")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(1000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Old description", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	newCategory := costs.CostCategoryEquipment
	updated, err := repo.UpdateDetails(ctx, "company_a", created.ID, created.Revision, "New description", "some notes", &newCategory)
	if err != nil {
		t.Fatalf("unexpected error updating details: %v", err)
	}
	if updated.Description != "New description" {
		t.Fatalf("expected updated description, got %s", updated.Description)
	}
	if updated.Category != costs.CostCategoryEquipment {
		t.Fatalf("expected updated category, got %s", updated.Category)
	}
}

func TestCostItemRepositoryDelete(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_delete")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(1000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryLabour,
		Description: "Orphan compensation test", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if err := repo.Delete(ctx, "company_a", created.ID); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_a", created.ID)
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound after delete, got %v", err)
	}
}

func TestMongoCorrectActualAppendsExactlyOneRecordAndUpdatesActualAtomically(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_correct_actual")
	repo := costs.NewMongoCostItemRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMaterial,
		Description: "Cement", Currency: "MYR", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstActual := money.New(85000, "MYR")
	afterRecord, err := repo.RecordActual(ctx, "company_a", created.ID, created.Revision, firstActual)
	if err != nil {
		t.Fatal(err)
	}

	correction := costs.ActualCorrection{
		PreviousAmount: firstActual, NewAmount: money.New(8500, "MYR"),
		Reason: "Decimal point error", CorrectedByUser: "user_1", CorrectedAt: time.Now(),
	}
	corrected, err := repo.CorrectActual(ctx, "company_a", created.ID, afterRecord.Revision, money.New(8500, "MYR"), correction)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if corrected.Actual == nil || corrected.Actual.Amount != 8500 {
		t.Fatalf("Actual = %v, want 8500", corrected.Actual)
	}
	if len(corrected.ActualCorrections) != 1 || corrected.ActualCorrections[0].PreviousAmount.Amount != 85000 {
		t.Fatalf("ActualCorrections = %+v, want exactly 1 entry with PreviousAmount=85000", corrected.ActualCorrections)
	}
}

func TestMongoCorrectActualRejectsStaleRevisionAndAppendsNothing(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_correct_actual_stale")
	repo := costs.NewMongoCostItemRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMaterial,
		Description: "Cement", Currency: "MYR", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstActual := money.New(85000, "MYR")
	if _, err := repo.RecordActual(ctx, "company_a", created.ID, created.Revision, firstActual); err != nil {
		t.Fatal(err)
	}

	correction := costs.ActualCorrection{
		PreviousAmount: firstActual, NewAmount: money.New(8500, "MYR"),
		Reason: "attempted with stale revision", CorrectedByUser: "user_1", CorrectedAt: time.Now(),
	}
	// created.Revision (0) is now stale — the real current revision after RecordActual is 1.
	_, err = repo.CorrectActual(ctx, "company_a", created.ID, created.Revision, money.New(8500, "MYR"), correction)
	if !errors.Is(err, costs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}

	unchanged, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.ActualCorrections) != 0 {
		t.Fatalf("a rejected correction must append NOTHING (the whole UpdateOne matches zero documents when the filter fails), got %d", len(unchanged.ActualCorrections))
	}
	if unchanged.Actual.Amount != 85000 {
		t.Fatalf("Actual must be untouched by a rejected correction, got %v", unchanged.Actual)
	}
}

func TestCostItemRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_indexes")
	repo := costs.NewMongoCostItemRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) since CreateCostItem would otherwise
// require real Project/WorkItem/Material lookups.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_deleteall_service")
	repo := costs.NewMongoCostItemRepository(db)
	svc := costs.NewService(repo, nil, nil, nil)
	ctx := context.Background()

	estimated := money.New(1000, "MYR")
	if _, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategorySubcontractor,
		Description: "A1", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("create company_a cost item: %v", err)
	}
	if _, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_b", ProjectID: "project_2", Category: costs.CostCategorySubcontractor,
		Description: "B1", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("create company_b cost item: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a cost items, got %d", len(listA))
	}

	listB, err := repo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's cost item to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_deleteall_empty")
	repo := costs.NewMongoCostItemRepository(db)
	svc := costs.NewService(repo, nil, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
