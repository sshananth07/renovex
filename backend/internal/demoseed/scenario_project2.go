package demoseed

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materials"
)

type CostItemCreator interface {
	CreateCostItem(ctx context.Context, companyID, projectID string, workItemID *string, category costs.CostCategory, description string, quantityValue, quantityUnit *string, unitPriceAmount, estimatedAmount, committedAmount, actualAmount, paidAmount *int64, currency string, materialID *string, date time.Time, notes string) (costs.CostItem, error)
	ListCostItemsByProject(ctx context.Context, companyID, projectID string) ([]costs.CostItem, error)
}

type WorkerCreator interface {
	CreateWorker(ctx context.Context, companyID, name, trade, rateType string, defaultRateAmount int64, currency, contactPhone, contactEmail string) (labour.Worker, error)
	ListWorkers(ctx context.Context, companyID string) ([]labour.Worker, error)
}

type LabourEntryCreator interface {
	CreateLabourEntry(ctx context.Context, companyID, projectID, workItemID string, workerID *string, workerName, trade, quantityValue, quantityUnit string, rateAmount *int64, currency string, date time.Time, notes string) (labour.LabourEntry, error)
	ListLabourEntriesByProject(ctx context.Context, companyID, projectID string) ([]labour.LabourEntry, error)
}

type EstimateCreator interface {
	CreateEstimate(ctx context.Context, companyID, projectID string, pricingMode estimates.PricingMode, pricingRate money.RateBPS) (estimates.Estimate, error)
	GetLatestEstimate(ctx context.Context, companyID, projectID string) (estimates.Estimate, error)
	FinalizeEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (estimates.Estimate, error)
}

// SeedProject2 seeds "Bangsar Kitchen Renovation" — extends Project 1's
// depth with Materials, Labour, Cost Items, and a finalized Estimate
// (design spec §5, Project 2: Estimating). Every Cost Item sets ONLY
// Estimated. Idempotent.
func SeedProject2(
	ctx context.Context,
	clientsSvc ClientCreator, projectsSvc ProjectCreator, propertiesSvc PropertyCreator,
	spacesSvc SpaceCreator, workSvc WorkItemCreator, costsSvc CostItemCreator,
	workersSvc WorkerCreator, labourSvc LabourEntryCreator, estimatesSvc EstimateCreator,
	materialCatalog map[string]materials.Material,
	companyID string,
) (string, error) {
	client, err := ensureClient(ctx, clientsSvc, companyID,
		"Lim Residence", "+60 12-987 6543", "lim.residence@example.com",
		"Bangsar, 59100 Kuala Lumpur", "Kitchen renovation for a landed property; owner works from home.")
	if err != nil {
		return "", err
	}

	project, err := ensureProject(ctx, projectsSvc, companyID, client.ID, "Bangsar Kitchen Renovation")
	if err != nil {
		return "", err
	}

	if _, err := ensureProperty(ctx, propertiesSvc, companyID, project.ID,
		"14 Jalan Bangsar Utama 9, 59100 Kuala Lumpur", "terrace_house",
		"Single-storey terrace, kitchen extension at the rear."); err != nil {
		return "", err
	}

	kitchen, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Kitchen", "kitchen", "Dry and wet kitchen, open to the dining area.")
	if err != nil {
		return "", err
	}

	tileWork, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, stringPtr(kitchen.ID),
		"Re-tile kitchen floor with porcelain tile", "flooring", "22", "sqm")
	if err != nil {
		return "", err
	}
	cabinetWork, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, stringPtr(kitchen.ID),
		"Install new kitchen cabinets and countertop", "carpentry", "1", "lot")
	if err != nil {
		return "", err
	}
	plumbingWork, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, stringPtr(kitchen.ID),
		"Relocate sink plumbing and install waterproofing", "plumbing", "10", "sqm")
	if err != nil {
		return "", err
	}

	tile := materialCatalog["Porcelain Floor Tile"]
	cement := materialCatalog["Cement"]

	existingCostItems, err := costsSvc.ListCostItemsByProject(ctx, companyID, project.ID)
	if err != nil {
		return "", err
	}
	haveCostItem := func(description string) bool {
		_, found := FindByName(existingCostItems, description, func(c costs.CostItem) string { return c.Description })
		return found
	}

	if !haveCostItem("Porcelain floor tile supply and lay") {
		qtyValue, qtyUnit := "22", "sqm"
		if _, err := costsSvc.CreateCostItem(ctx, companyID, project.ID, stringPtr(tileWork.ID), costs.CostCategoryMaterial,
			"Porcelain floor tile supply and lay", &qtyValue, &qtyUnit,
			int64Ptr(4500), int64Ptr(99000), nil, nil, nil, "MYR", stringPtr(tile.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Cement and screed for tiling") {
		qtyValue, qtyUnit := "6", "bag"
		if _, err := costsSvc.CreateCostItem(ctx, companyID, project.ID, stringPtr(tileWork.ID), costs.CostCategoryMaterial,
			"Cement and screed for tiling", &qtyValue, &qtyUnit,
			int64Ptr(2200), int64Ptr(13200), nil, nil, nil, "MYR", stringPtr(cement.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Kitchen cabinets and countertop, custom build") {
		if _, err := costsSvc.CreateCostItem(ctx, companyID, project.ID, stringPtr(cabinetWork.ID), costs.CostCategorySubcontractor,
			"Kitchen cabinets and countertop, custom build", nil, nil,
			nil, int64Ptr(1200000), nil, nil, nil, "MYR", nil, time.Now(), "Quoted by carpentry subcontractor."); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Plumbing permit and inspection fee") {
		if _, err := costsSvc.CreateCostItem(ctx, companyID, project.ID, stringPtr(plumbingWork.ID), costs.CostCategoryPermit,
			"Plumbing permit and inspection fee", nil, nil,
			nil, int64Ptr(80000), nil, nil, nil, "MYR", nil, time.Now(), ""); err != nil {
			return "", err
		}
	}

	existingWorkers, err := workersSvc.ListWorkers(ctx, companyID)
	if err != nil {
		return "", err
	}
	worker, found := FindByName(existingWorkers, "Razak bin Yusof", func(w labour.Worker) string { return w.Name })
	if !found {
		worker, err = workersSvc.CreateWorker(ctx, companyID, "Razak bin Yusof", "plumber", "daily", 18000, "MYR", "+60 13-222 4455", "")
		if err != nil {
			return "", err
		}
	}

	existingLabour, err := labourSvc.ListLabourEntriesByProject(ctx, companyID, project.ID)
	if err != nil {
		return "", err
	}
	haveLabourForWorkItem := func(workItemID string) bool {
		for _, e := range existingLabour {
			if e.WorkItemID == workItemID {
				return true
			}
		}
		return false
	}
	if !haveLabourForWorkItem(plumbingWork.ID) {
		workerID := worker.ID
		if _, err := labourSvc.CreateLabourEntry(ctx, companyID, project.ID, plumbingWork.ID, &workerID,
			"", "", "3", "day", nil, "MYR", time.Now(), "Sink relocation and waterproofing."); err != nil {
			return "", err
		}
	}

	existing, err := estimatesSvc.GetLatestEstimate(ctx, companyID, project.ID)
	if err == nil && existing.Status == estimates.EstimateStatusFinalized {
		return project.ID, nil
	}
	if err == nil && existing.Status == estimates.EstimateStatusDraft {
		if _, err := estimatesSvc.FinalizeEstimate(ctx, companyID, existing.ID, existing.Revision); err != nil {
			return "", err
		}
		return project.ID, nil
	}

	created, err := estimatesSvc.CreateEstimate(ctx, companyID, project.ID, estimates.PricingModeMarkup, money.RateBPS(2500))
	if err != nil {
		return "", err
	}
	if _, err := estimatesSvc.FinalizeEstimate(ctx, companyID, created.ID, created.Revision); err != nil {
		return "", err
	}

	return project.ID, nil
}
