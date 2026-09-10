package costs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// visitedRow captures one row the visitor yielded, so tests can assert both
// what was emitted and what was filtered.
type visitedRow struct {
	costItemID   string
	workItemID   string
	materialID   string
	quantity     decimal.Decimal
	quantityUnit string
}

func newMaterialSourceService(repo costs.CostItemRepository) *costs.Service {
	return costs.NewService(repo, fakeProjectLookup{belongs: true},
		fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})
}

// visitAll runs the visitor and returns the emitted rows plus the five
// intrinsic ineligible counters.
func visitAll(t *testing.T, svc *costs.Service, companyID, projectID string) ([]visitedRow, [5]int) {
	t.Helper()
	var rows []visitedRow
	nonMaterial, missingMaterial, missingWorkItem, missingQty, blankUnit, err :=
		svc.VisitEligibleMaterialCostItems(context.Background(), companyID, projectID,
			func(costItemID, workItemID, materialID string, qty decimal.Decimal, unit string) error {
				rows = append(rows, visitedRow{costItemID, workItemID, materialID, qty, unit})
				return nil
			})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return rows, [5]int{nonMaterial, missingMaterial, missingWorkItem, missingQty, blankUnit}
}

// createMaterialCostItem inserts a fully eligible material CostItem.
func createMaterialCostItem(t *testing.T, svc *costs.Service, companyID, projectID, workItemID, materialID, qty, unit string) {
	t.Helper()
	unitPrice := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), companyID, projectID, &workItemID,
		costs.CostCategoryMaterial, "material line", &qty, &unit, &unitPrice,
		nil, nil, nil, nil, "MYR", &materialID, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating material cost item: %v", err)
	}
}

// A fully eligible material CostItem is emitted with every field populated.
func TestVisitEligibleMaterialCostItemsEmitsEligibleRows(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)
	createMaterialCostItem(t, svc, "company_a", "project_1", "work_1", "material_1", "33.5", "m2")

	rows, counters := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 emitted row, got %d", len(rows))
	}
	got := rows[0]
	if got.workItemID != "work_1" || got.materialID != "material_1" || got.quantityUnit != "m2" {
		t.Errorf("row fields wrong: %+v", got)
	}
	if !got.quantity.Equal(decimal.RequireFromString("33.5")) {
		t.Errorf("quantity = %s, want 33.5", got.quantity.String())
	}
	if got.costItemID == "" {
		t.Error("costItemID must be populated")
	}
	if counters != [5]int{0, 0, 0, 0, 0} {
		t.Errorf("expected no ineligible counts, got %v", counters)
	}
}

// A non-material category is never eligible and is counted separately. Labour
// is created through the LabourCostRecorder path, so it is covered too.
func TestVisitEligibleMaterialCostItemsCountsNonMaterialCategory(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)
	ctx := context.Background()

	estimated := int64(50000)
	for _, category := range []costs.CostCategory{
		costs.CostCategoryPermit, costs.CostCategorySubcontractor, costs.CostCategoryTransport,
	} {
		if _, err := svc.CreateCostItem(ctx, "company_a", "project_1", nil, category,
			"non-material", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), ""); err != nil {
			t.Fatalf("unexpected error creating %s cost item: %v", category, err)
		}
	}
	// Labour can only be produced via RecordLabourCost (the M3 ledger-entry-point
	// invariant), and must also be counted as non-material.
	if _, err := svc.RecordLabourCost(ctx, "company_a", "project_1", "work_1",
		money.New(25000, "MYR")); err != nil {
		t.Fatalf("unexpected error recording labour cost: %v", err)
	}

	rows, counters := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 0 {
		t.Fatalf("expected 0 emitted rows, got %d", len(rows))
	}
	if counters[0] != 4 {
		t.Errorf("nonMaterialCategory = %d, want 4", counters[0])
	}
}

// category=material with no MaterialID is ineligible: procurement cannot know
// what to buy.
func TestVisitEligibleMaterialCostItemsCountsMissingMaterialID(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)

	workItemID := "work_1"
	qty, unit, unitPrice := "10", "bag", int64(1000)
	if _, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", &workItemID,
		costs.CostCategoryMaterial, "no material link", &qty, &unit, &unitPrice,
		nil, nil, nil, nil, "MYR", nil, time.Now(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows, counters := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 0 {
		t.Fatalf("expected 0 emitted rows, got %d", len(rows))
	}
	if counters[1] != 1 {
		t.Errorf("missingMaterialID = %d, want 1", counters[1])
	}
}

// A project-level material CostItem (no WorkItem) is not eligible for prefill;
// the contractor creates that demand manually (design spec §3.1).
func TestVisitEligibleMaterialCostItemsCountsMissingWorkItemID(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)

	materialID := "material_1"
	qty, unit, unitPrice := "10", "bag", int64(1000)
	if _, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil,
		costs.CostCategoryMaterial, "project-level material", &qty, &unit, &unitPrice,
		nil, nil, nil, nil, "MYR", &materialID, time.Now(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows, counters := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 0 {
		t.Fatalf("expected 0 emitted rows, got %d", len(rows))
	}
	if counters[2] != 1 {
		t.Errorf("missingWorkItemID = %d, want 1", counters[2])
	}
}

// A lump-sum material CostItem carries no quantity and cannot be aggregated.
func TestVisitEligibleMaterialCostItemsCountsMissingQuantity(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)

	workItemID, materialID := "work_1", "material_1"
	estimated := int64(50000)
	if _, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", &workItemID,
		costs.CostCategoryMaterial, "lump sum material", nil, nil, nil,
		&estimated, nil, nil, nil, "MYR", &materialID, time.Now(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows, counters := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 0 {
		t.Fatalf("expected 0 emitted rows, got %d", len(rows))
	}
	if counters[3] != 1 {
		t.Errorf("missingOrZeroQuantity = %d, want 1", counters[3])
	}
}

// Every counter is reported independently in one pass, so the caller can tell
// which costing gaps exist.
func TestVisitEligibleMaterialCostItemsReportsCountersIndependently(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)
	ctx := context.Background()

	// eligible
	createMaterialCostItem(t, svc, "company_a", "project_1", "work_1", "material_1", "5", "bag")
	// non-material
	estimated := int64(1000)
	if _, err := svc.CreateCostItem(ctx, "company_a", "project_1", nil, costs.CostCategoryPermit,
		"permit", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	// material, no material ID
	workItemID := "work_1"
	qty, unit, unitPrice := "3", "bag", int64(500)
	if _, err := svc.CreateCostItem(ctx, "company_a", "project_1", &workItemID,
		costs.CostCategoryMaterial, "unlinked", &qty, &unit, &unitPrice,
		nil, nil, nil, nil, "MYR", nil, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	// material, no work item
	materialID := "material_2"
	if _, err := svc.CreateCostItem(ctx, "company_a", "project_1", nil,
		costs.CostCategoryMaterial, "project level", &qty, &unit, &unitPrice,
		nil, nil, nil, nil, "MYR", &materialID, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	// material, lump sum
	if _, err := svc.CreateCostItem(ctx, "company_a", "project_1", &workItemID,
		costs.CostCategoryMaterial, "lump", nil, nil, nil,
		&estimated, nil, nil, nil, "MYR", &materialID, time.Now(), ""); err != nil {
		t.Fatal(err)
	}

	rows, counters := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 eligible row, got %d", len(rows))
	}
	if want := [5]int{1, 1, 1, 1, 0}; counters != want {
		t.Errorf("counters = %v, want %v", counters, want)
	}
}

// The visitor is tenant-scoped and project-scoped: another company's or
// another project's CostItems are neither emitted nor counted.
func TestVisitEligibleMaterialCostItemsIsTenantAndProjectScoped(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)

	createMaterialCostItem(t, svc, "company_a", "project_1", "work_1", "material_1", "5", "bag")
	createMaterialCostItem(t, svc, "company_b", "project_1", "work_1", "material_1", "7", "bag")
	createMaterialCostItem(t, svc, "company_a", "project_2", "work_2", "material_1", "9", "bag")

	rows, _ := visitAll(t, svc, "company_a", "project_1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row scoped to company_a/project_1, got %d", len(rows))
	}
	if !rows[0].quantity.Equal(decimal.RequireFromString("5")) {
		t.Errorf("wrong row leaked in: quantity = %s", rows[0].quantity.String())
	}
}

// A visitor error aborts the walk and propagates unchanged, so generation can
// stop on the first failure rather than silently continuing.
func TestVisitEligibleMaterialCostItemsPropagatesVisitorError(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := newMaterialSourceService(repo)
	createMaterialCostItem(t, svc, "company_a", "project_1", "work_1", "material_1", "5", "bag")
	createMaterialCostItem(t, svc, "company_a", "project_1", "work_2", "material_2", "6", "bag")

	sentinel := errors.New("visitor failed")
	calls := 0
	_, _, _, _, _, err := svc.VisitEligibleMaterialCostItems(context.Background(), "company_a", "project_1",
		func(string, string, string, decimal.Decimal, string) error {
			calls++
			return sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the visitor error to propagate, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected the walk to abort after the first error, got %d calls", calls)
	}
}

// A foreign projectID returns ErrProjectNotFound, never an empty result — the
// tenant invariant stated in costs/service.go.
func TestVisitEligibleMaterialCostItemsRejectsForeignProject(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: false},
		fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})

	_, _, _, _, _, err := svc.VisitEligibleMaterialCostItems(context.Background(), "company_a", "project_x",
		func(string, string, string, decimal.Decimal, string) error { return nil })
	if !errors.Is(err, costs.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}
