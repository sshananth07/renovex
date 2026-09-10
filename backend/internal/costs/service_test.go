package costs_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

type fakeCostItemRepository struct {
	byID map[string]costs.CostItem
	next int
}

func newFakeCostItemRepository() *fakeCostItemRepository {
	return &fakeCostItemRepository{byID: make(map[string]costs.CostItem)}
}

func (f *fakeCostItemRepository) Create(ctx context.Context, c costs.CostItem) (costs.CostItem, error) {
	f.next++
	c.ID = "cost_item_" + string(rune('0'+f.next))
	f.byID[c.ID] = c
	return c, nil
}

func (f *fakeCostItemRepository) FindByID(ctx context.Context, companyID, id string) (costs.CostItem, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return costs.CostItem{}, costs.ErrCostItemNotFound
	}
	return c, nil
}

func (f *fakeCostItemRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]costs.CostItem, error) {
	var result []costs.CostItem
	for _, c := range f.byID {
		if c.CompanyID == companyID && c.ProjectID == projectID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (f *fakeCostItemRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]costs.CostItem, error) {
	var result []costs.CostItem
	for _, c := range f.byID {
		if c.CompanyID == companyID && c.WorkItemID != nil && *c.WorkItemID == workItemID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (f *fakeCostItemRepository) checkRevision(c costs.CostItem, expectedRevision int64) error {
	if c.Revision != expectedRevision {
		return costs.ErrRevisionMismatch
	}
	return nil
}

func (f *fakeCostItemRepository) UpdateLifecycleField(ctx context.Context, companyID, id string, expectedRevision int64, stage costs.CostStage, amount money.Money) (costs.CostItem, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return costs.CostItem{}, err
	}
	if err := f.checkRevision(c, expectedRevision); err != nil {
		return costs.CostItem{}, err
	}
	switch stage {
	case costs.CostStageEstimated:
		c.Estimated = &amount
	case costs.CostStageCommitted:
		c.Committed = &amount
	case costs.CostStageActual:
		c.Actual = &amount
	case costs.CostStagePaid:
		c.Paid = &amount
	}
	c.Revision++
	f.byID[id] = c
	return c, nil
}

func (f *fakeCostItemRepository) UpdateDetails(ctx context.Context, companyID, id string, expectedRevision int64, description, notes string, category *costs.CostCategory) (costs.CostItem, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return costs.CostItem{}, err
	}
	if err := f.checkRevision(c, expectedRevision); err != nil {
		return costs.CostItem{}, err
	}
	c.Description = description
	c.Notes = notes
	if category != nil {
		c.Category = *category
	}
	c.Revision++
	f.byID[id] = c
	return c, nil
}

func (f *fakeCostItemRepository) RecordActual(ctx context.Context, companyID, id string, expectedRevision int64, amount money.Money) (costs.CostItem, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return costs.CostItem{}, err
	}
	if err := f.checkRevision(c, expectedRevision); err != nil {
		return costs.CostItem{}, err
	}
	c.Actual = &amount
	c.Revision++
	f.byID[id] = c
	return c, nil
}

func (f *fakeCostItemRepository) CorrectActual(ctx context.Context, companyID, id string, expectedRevision int64, newAmount money.Money, correction costs.ActualCorrection) (costs.CostItem, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return costs.CostItem{}, err
	}
	if err := f.checkRevision(c, expectedRevision); err != nil {
		return costs.CostItem{}, err
	}
	c.Actual = &newAmount
	c.ActualCorrections = append(c.ActualCorrections, correction)
	c.Revision++
	f.byID[id] = c
	return c, nil
}

func (f *fakeCostItemRepository) Delete(ctx context.Context, companyID, id string) error {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return err
	}
	delete(f.byID, id)
	_ = c
	return nil
}

type fakeProjectLookup struct{ belongs bool }

func (f fakeProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeWorkItemLookup struct{ belongs bool }

func (f fakeWorkItemLookup) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error) {
	return f.belongs, nil
}

func (f fakeWorkItemLookup) WorkItemBelongsToCompany(ctx context.Context, companyID, workItemID string) (bool, error) {
	return f.belongs, nil
}

type fakeMaterialLookup struct {
	belongs        bool
	referencePrice money.Money
}

func (f fakeMaterialLookup) MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error) {
	return f.belongs, nil
}

func (f fakeMaterialLookup) GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error) {
	return f.referencePrice, nil
}

func newTestService() (*costs.Service, *fakeCostItemRepository) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})
	return svc, repo
}

func TestCreateCostItemRejectsLabourCategory(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryLabour,
		"Should be rejected", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrLabourCategoryNotAllowed {
		t.Fatalf("expected ErrLabourCategoryNotAllowed, got %v", err)
	}
}

func TestCreateCostItemRequiresAtLeastOneLifecycleAmount(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryPermit,
		"Empty", nil, nil, nil, nil, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrNoLifecycleAmount {
		t.Fatalf("expected ErrNoLifecycleAmount, got %v", err)
	}
}

func TestCreateCostItemLumpSumSuccess(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(450000)
	c, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategorySubcontractor,
		"Bathroom plumbing", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Estimated == nil || c.Estimated.Amount != 450000 {
		t.Fatalf("expected estimated 450000, got %+v", c.Estimated)
	}
}

func TestCreateCostItemQuantityUnitPriceEstablishesEstimatedOnly(t *testing.T) {
	svc, _ := newTestService()
	qtyValue := "100"
	qtyUnit := "m2"
	unitPrice := int64(1000) // RM10.00
	// Estimated not explicitly supplied -> derived as 100 * RM10.00 = RM1,000.00
	committed := int64(95000) // RM950.00 -- independently different, must be accepted
	c, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, &qtyUnit, &unitPrice, nil, &committed, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Estimated == nil || c.Estimated.Amount != 100000 {
		t.Fatalf("expected derived estimated 100000, got %+v", c.Estimated)
	}
	if c.Committed == nil || c.Committed.Amount != 95000 {
		t.Fatalf("expected committed to remain independently 95000, got %+v", c.Committed)
	}
}

func TestCreateCostItemRejectsMismatchedEstimated(t *testing.T) {
	svc, _ := newTestService()
	qtyValue := "100"
	qtyUnit := "m2"
	unitPrice := int64(1000) // RM10.00 -> line amount RM1,000.00
	wrongEstimated := int64(50000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, &qtyUnit, &unitPrice, &wrongEstimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrEstimatedMismatchesLineAmount {
		t.Fatalf("expected ErrEstimatedMismatchesLineAmount, got %v", err)
	}
}

func TestCreateCostItemAcceptsMatchingEstimated(t *testing.T) {
	svc, _ := newTestService()
	qtyValue := "100"
	qtyUnit := "m2"
	unitPrice := int64(1000) // RM10.00 -> line amount RM1,000.00
	matchingEstimated := int64(100000)
	c, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, &qtyUnit, &unitPrice, &matchingEstimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Estimated == nil || c.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated 100000, got %+v", c.Estimated)
	}
}

func TestCreateCostItemRejectsIncompleteQuantityPricing(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)

	qtyValue := "100"
	qtyUnit := "m2"
	// unitPriceAmount omitted -> incomplete combination
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, &qtyUnit, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrIncompleteQuantityPricing {
		t.Fatalf("expected ErrIncompleteQuantityPricing for missing unitPrice, got %v", err)
	}

	unitPrice := int64(1000)
	// quantityUnit omitted -> incomplete combination
	_, err = svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, nil, &unitPrice, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrIncompleteQuantityPricing {
		t.Fatalf("expected ErrIncompleteQuantityPricing for missing quantityUnit, got %v", err)
	}
}

func TestCreateCostItemQuantityWithoutEstimatedSucceeds(t *testing.T) {
	// This is the exact scenario the plan originally got wrong: quantity +
	// unitPrice supplied, all four lifecycle amounts omitted -> Estimated
	// must be derived, not rejected by the no-lifecycle-amount check.
	svc, _ := newTestService()
	qtyValue := "100"
	qtyUnit := "m2"
	unitPrice := int64(1000) // RM10.00
	c, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, &qtyUnit, &unitPrice, nil, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Estimated == nil || c.Estimated.Amount != 100000 {
		t.Fatalf("expected derived estimated 100000, got %+v", c.Estimated)
	}
}

func TestCreateCostItemMaterialIDRequiresMaterialCategory(t *testing.T) {
	svc, _ := newTestService()
	materialID := "material_1"
	estimated := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Wrong category for materialID", nil, nil, nil, &estimated, nil, nil, nil, "MYR", &materialID, time.Now(), "")
	if err != costs.ErrMaterialIDRequiresMaterialCategory {
		t.Fatalf("expected ErrMaterialIDRequiresMaterialCategory, got %v", err)
	}
}

func TestUpdateCostItemLifecycleRejectsCurrencyMismatch(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, created.Revision, costs.CostStageCommitted, money.New(1000, "SGD"))
	if err != costs.ErrCurrencyMismatch {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestUpdateCostItemLifecycleRejectsStageActual(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(100000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, created.Revision, costs.CostStageActual, money.New(108000, "MYR"))
	if err != costs.ErrActualMustUseRecordOrCorrect {
		t.Fatalf("expected ErrActualMustUseRecordOrCorrect, got %v", err)
	}
}

func TestUpdateCostItemLifecycleAllowsEstimatedCommittedPaidSimultaneously(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(100000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterCommitted, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, created.Revision, costs.CostStageCommitted, money.New(95000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error setting committed: %v", err)
	}
	afterActual, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterCommitted.Revision, money.New(108000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error setting actual: %v", err)
	}
	final, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, afterActual.Revision, costs.CostStagePaid, money.New(50000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error setting paid: %v", err)
	}
	if final.Estimated == nil || final.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated to remain 100000, got %+v", final.Estimated)
	}
	if final.Committed == nil || final.Committed.Amount != 95000 {
		t.Fatalf("expected committed 95000, got %+v", final.Committed)
	}
	if final.Actual == nil || final.Actual.Amount != 108000 {
		t.Fatalf("expected actual 108000, got %+v", final.Actual)
	}
	if final.Paid == nil || final.Paid.Amount != 50000 {
		t.Fatalf("expected paid 50000, got %+v", final.Paid)
	}
}

func TestUpdateCostItemDetailsCategoryLockedOnceCommitted(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterCommitted, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, created.Revision, costs.CostStageCommitted, money.New(1000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryTransport
	_, err = svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, afterCommitted.Revision, "New desc", "notes", &newCategory)
	if err != costs.ErrCategoryLocked {
		t.Fatalf("expected ErrCategoryLocked once Committed is set, got %v", err)
	}
}

func TestUpdateCostItemDetailsCategoryCorrectablePreCommitment(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryTransport
	updated, err := svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, created.Revision, "New desc", "notes", &newCategory)
	if err != nil {
		t.Fatalf("unexpected error correcting category pre-commitment: %v", err)
	}
	if updated.Category != costs.CostCategoryTransport {
		t.Fatalf("expected corrected category, got %s", updated.Category)
	}
}

func TestUpdateCostItemDetailsCannotDetachMaterialIDFromMaterialCategory(t *testing.T) {
	svc, _ := newTestService()
	materialID := "material_1"
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", nil, nil, nil, &estimated, nil, nil, nil, "MYR", &materialID, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryEquipment
	_, err = svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, created.Revision, "desc", "notes", &newCategory)
	if err != costs.ErrMaterialIDRequiresMaterialCategory {
		t.Fatalf("expected ErrMaterialIDRequiresMaterialCategory, got %v", err)
	}
}

func TestUpdateCostItemDetailsCannotChangeCategoryIntoLabour(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	labourCategory := costs.CostCategoryLabour
	_, err = svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, created.Revision, "desc", "notes", &labourCategory)
	if err != costs.ErrLabourCategoryNotAllowed {
		t.Fatalf("expected ErrLabourCategoryNotAllowed, got %v", err)
	}
}

func TestUpdateCostItemDetailsCannotChangeCategoryAwayFromLabour(t *testing.T) {
	svc, _ := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryTransport
	_, err = svc.UpdateCostItemDetails(context.Background(), "company_a", costItemID, 0, "desc", "notes", &newCategory)
	if err != costs.ErrLabourCategoryImmutable {
		t.Fatalf("expected ErrLabourCategoryImmutable, got %v", err)
	}
}

func TestRecordLabourCostCreatesCostItemWithLabourCategory(t *testing.T) {
	svc, repo := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created, err := repo.FindByID(context.Background(), "company_a", costItemID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if created.Category != costs.CostCategoryLabour {
		t.Fatalf("expected labour category, got %s", created.Category)
	}
	if created.Estimated == nil || created.Estimated.Amount != 127500 {
		t.Fatalf("expected estimated 127500, got %+v", created.Estimated)
	}
}

func TestUpdateLabourCostEstimateUpdatesEstimatedOnly(t *testing.T) {
	svc, repo := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := repo.UpdateLifecycleField(context.Background(), "company_a", costItemID, 0, costs.CostStageActual, money.New(130000, "MYR")); err != nil {
		t.Fatalf("unexpected error setting actual directly via repo for test setup: %v", err)
	}

	if err := svc.UpdateLabourCostEstimate(context.Background(), "company_a", costItemID, money.New(150000, "MYR")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := repo.FindByID(context.Background(), "company_a", costItemID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Estimated == nil || updated.Estimated.Amount != 150000 {
		t.Fatalf("expected estimated updated to 150000, got %+v", updated.Estimated)
	}
	if updated.Actual == nil || updated.Actual.Amount != 130000 {
		t.Fatalf("expected actual to remain untouched at 130000, got %+v", updated.Actual)
	}
}

func TestDeleteProvisionedLabourCost(t *testing.T) {
	svc, repo := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.DeleteProvisionedLabourCost(context.Background(), "company_a", costItemID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.FindByID(context.Background(), "company_a", costItemID)
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound after compensation delete, got %v", err)
	}
}

func TestCostItemBelongsToProjectValidation(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: false}, fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})
	estimated := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "nonexistent_project", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCostItemWorkItemLineageValidation(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: false}, fakeMaterialLookup{belongs: true})
	estimated := int64(1000)
	workItemID := "work_item_mismatched"
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", &workItemID, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound, got %v", err)
	}
}

func TestListCostItemsByWorkItemRejectsForeignWorkItem(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: false}, fakeMaterialLookup{belongs: true})
	_, err := svc.ListCostItemsByWorkItem(context.Background(), "company_a", "foreign_work_item")
	if err != costs.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for a workItemID that does not belong to the company, got %v", err)
	}
}

func TestServiceVisitEstimatedCostItems(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})

	ctx := context.Background()
	desc := "Ceramic tiles"
	workItemID := "work_1"
	estimated1 := int64(500000)
	_, err := svc.CreateCostItem(ctx, "company_a", "project_1", &workItemID, costs.CostCategoryMaterial,
		desc, nil, nil, nil, &estimated1, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating cost item with Estimated: %v", err)
	}

	// A CostItem with only Actual set (no Estimated) — must be counted as
	// missing, not visited.
	actual1 := int64(300000)
	_, err = svc.CreateCostItem(ctx, "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Site cleanup", nil, nil, nil, nil, nil, &actual1, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating cost item with only Actual: %v", err)
	}

	var visited []string
	missingCount, err := svc.VisitEstimatedCostItems(ctx, "company_a", "project_1",
		func(costItemID string, workItemID *string, category, description string, estimated money.Money) error {
			visited = append(visited, description)
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(visited) != 1 || visited[0] != "Ceramic tiles" {
		t.Fatalf("expected exactly one visited CostItem (the one with Estimated set), got %v", visited)
	}
	if missingCount != 1 {
		t.Fatalf("expected missingCount=1 (the Actual-only CostItem), got %d", missingCount)
	}
}
