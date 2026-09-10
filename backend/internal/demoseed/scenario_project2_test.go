package demoseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

type fakeCostItemCreator struct {
	byProject   map[string][]costs.CostItem
	createCalls int
}

func (f *fakeCostItemCreator) CreateCostItem(ctx context.Context, companyID, projectID string, workItemID *string, category costs.CostCategory, description string, quantityValue, quantityUnit *string, unitPriceAmount, estimatedAmount, committedAmount, actualAmount, paidAmount *int64, currency string, materialID *string, date time.Time, notes string) (costs.CostItem, error) {
	f.createCalls++
	c := costs.CostItem{ID: projectID + ":" + description, CompanyID: companyID, ProjectID: projectID, Category: category, Description: description}
	f.byProject[projectID] = append(f.byProject[projectID], c)
	return c, nil
}
func (f *fakeCostItemCreator) ListCostItemsByProject(ctx context.Context, companyID, projectID string) ([]costs.CostItem, error) {
	return f.byProject[projectID], nil
}

type fakeWorkerCreator struct {
	byName      map[string]labour.Worker
	createCalls int
}

func (f *fakeWorkerCreator) CreateWorker(ctx context.Context, companyID, name, trade, rateType string, defaultRateAmount int64, currency, contactPhone, contactEmail string) (labour.Worker, error) {
	f.createCalls++
	w := labour.Worker{ID: name, CompanyID: companyID, Name: name, Trade: trade}
	f.byName[name] = w
	return w, nil
}
func (f *fakeWorkerCreator) ListWorkers(ctx context.Context, companyID string) ([]labour.Worker, error) {
	list := make([]labour.Worker, 0, len(f.byName))
	for _, w := range f.byName {
		list = append(list, w)
	}
	return list, nil
}

type fakeLabourEntryCreator struct {
	byProject   map[string][]labour.LabourEntry
	createCalls int
}

func (f *fakeLabourEntryCreator) CreateLabourEntry(ctx context.Context, companyID, projectID, workItemID string, workerID *string, workerName, trade, quantityValue, quantityUnit string, rateAmount *int64, currency string, date time.Time, notes string) (labour.LabourEntry, error) {
	f.createCalls++
	e := labour.LabourEntry{ID: projectID + ":" + workItemID, CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID}
	f.byProject[projectID] = append(f.byProject[projectID], e)
	return e, nil
}
func (f *fakeLabourEntryCreator) ListLabourEntriesByProject(ctx context.Context, companyID, projectID string) ([]labour.LabourEntry, error) {
	return f.byProject[projectID], nil
}

type fakeEstimateCreator struct {
	byProject     map[string]estimates.Estimate
	createCalls   int
	finalizeCalls int
}

func (f *fakeEstimateCreator) CreateEstimate(ctx context.Context, companyID, projectID string, pricingMode estimates.PricingMode, pricingRate money.RateBPS) (estimates.Estimate, error) {
	f.createCalls++
	e := estimates.Estimate{ID: "estimate-" + projectID, CompanyID: companyID, ProjectID: projectID, Status: estimates.EstimateStatusDraft, Revision: 0}
	f.byProject[projectID] = e
	return e, nil
}
func (f *fakeEstimateCreator) GetLatestEstimate(ctx context.Context, companyID, projectID string) (estimates.Estimate, error) {
	e, ok := f.byProject[projectID]
	if !ok {
		return estimates.Estimate{}, estimates.ErrEstimateNotFound
	}
	return e, nil
}
func (f *fakeEstimateCreator) FinalizeEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (estimates.Estimate, error) {
	f.finalizeCalls++
	for projectID, e := range f.byProject {
		if e.ID == estimateID {
			e.Status = estimates.EstimateStatusFinalized
			f.byProject[projectID] = e
			return e, nil
		}
	}
	return estimates.Estimate{}, estimates.ErrEstimateNotFound
}

func TestSeedProject2_CreatesEstimateFinalized(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}
	costsFake := &fakeCostItemCreator{byProject: map[string][]costs.CostItem{}}
	workersFake := &fakeWorkerCreator{byName: map[string]labour.Worker{}}
	labourFake := &fakeLabourEntryCreator{byProject: map[string][]labour.LabourEntry{}}
	estimatesFake := &fakeEstimateCreator{byProject: map[string]estimates.Estimate{}}
	catalog := map[string]materials.Material{
		"Porcelain Floor Tile": {ID: "mat-1", Name: "Porcelain Floor Tile"},
		"Cement":               {ID: "mat-2", Name: "Cement"},
	}

	projectID, err := demoseed.SeedProject2(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, catalog, "company-1")
	if err != nil {
		t.Fatalf("SeedProject2: %v", err)
	}
	if len(costsFake.byProject[projectID]) == 0 {
		t.Fatalf("expected at least one Cost Item")
	}
	if len(labourFake.byProject[projectID]) == 0 {
		t.Fatalf("expected at least one Labour Entry")
	}
	if estimatesFake.createCalls != 1 {
		t.Fatalf("expected exactly 1 Estimate created, got %d", estimatesFake.createCalls)
	}
	if estimatesFake.finalizeCalls != 1 {
		t.Fatalf("expected the Estimate to be finalized, got %d finalize calls", estimatesFake.finalizeCalls)
	}
	final := estimatesFake.byProject[projectID]
	if final.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Estimate status finalized, got %q", final.Status)
	}
}

func TestSeedProject2_Idempotent_SecondRunSkipsFinalize(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}
	costsFake := &fakeCostItemCreator{byProject: map[string][]costs.CostItem{}}
	workersFake := &fakeWorkerCreator{byName: map[string]labour.Worker{}}
	labourFake := &fakeLabourEntryCreator{byProject: map[string][]labour.LabourEntry{}}
	estimatesFake := &fakeEstimateCreator{byProject: map[string]estimates.Estimate{}}
	catalog := map[string]materials.Material{"Cement": {ID: "mat-2", Name: "Cement"}}

	_, err := demoseed.SeedProject2(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, catalog, "company-1")
	if err != nil {
		t.Fatalf("first SeedProject2: %v", err)
	}
	firstCreateCalls := estimatesFake.createCalls

	_, err = demoseed.SeedProject2(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, catalog, "company-1")
	if err != nil {
		t.Fatalf("second SeedProject2: %v", err)
	}
	if estimatesFake.createCalls != firstCreateCalls {
		t.Fatalf("expected no new Estimate on rerun (already finalized): %d -> %d", firstCreateCalls, estimatesFake.createCalls)
	}
}
