package labour_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
)

type fakeWorkerRepository struct {
	byID map[string]labour.Worker
	next int
}

func newFakeWorkerRepository() *fakeWorkerRepository {
	return &fakeWorkerRepository{byID: make(map[string]labour.Worker)}
}

func (f *fakeWorkerRepository) Create(ctx context.Context, w labour.Worker) (labour.Worker, error) {
	f.next++
	w.ID = "worker_" + string(rune('0'+f.next))
	f.byID[w.ID] = w
	return w, nil
}

func (f *fakeWorkerRepository) FindByID(ctx context.Context, companyID, id string) (labour.Worker, error) {
	w, ok := f.byID[id]
	if !ok || w.CompanyID != companyID {
		return labour.Worker{}, labour.ErrWorkerNotFound
	}
	return w, nil
}

func (f *fakeWorkerRepository) List(ctx context.Context, companyID string) ([]labour.Worker, error) {
	var result []labour.Worker
	for _, w := range f.byID {
		if w.CompanyID == companyID {
			result = append(result, w)
		}
	}
	return result, nil
}

func (f *fakeWorkerRepository) Update(ctx context.Context, companyID, id string, fn func(*labour.Worker)) (labour.Worker, error) {
	w, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return labour.Worker{}, err
	}
	fn(&w)
	f.byID[id] = w
	return w, nil
}

type fakeLabourEntryRepository struct {
	byID           map[string]labour.LabourEntry
	byCostItemID   map[string]bool
	next           int
	failNextCreate bool
}

func newFakeLabourEntryRepository() *fakeLabourEntryRepository {
	return &fakeLabourEntryRepository{byID: make(map[string]labour.LabourEntry), byCostItemID: make(map[string]bool)}
}

func (f *fakeLabourEntryRepository) Create(ctx context.Context, e labour.LabourEntry) (labour.LabourEntry, error) {
	if f.failNextCreate {
		f.failNextCreate = false
		return labour.LabourEntry{}, errors.New("simulated LabourEntry write failure")
	}
	if f.byCostItemID[e.CostItemID] {
		return labour.LabourEntry{}, errors.New("duplicate costItemId")
	}
	f.next++
	e.ID = "labour_entry_" + string(rune('0'+f.next))
	f.byID[e.ID] = e
	f.byCostItemID[e.CostItemID] = true
	return e, nil
}

func (f *fakeLabourEntryRepository) FindByID(ctx context.Context, companyID, id string) (labour.LabourEntry, error) {
	e, ok := f.byID[id]
	if !ok || e.CompanyID != companyID {
		return labour.LabourEntry{}, labour.ErrLabourEntryNotFound
	}
	return e, nil
}

func (f *fakeLabourEntryRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]labour.LabourEntry, error) {
	var result []labour.LabourEntry
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeLabourEntryRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]labour.LabourEntry, error) {
	var result []labour.LabourEntry
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.WorkItemID == workItemID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeLabourEntryRepository) UpdateCorrection(ctx context.Context, companyID, id string, q quantity.Quantity, rate, cost money.Money, date time.Time, notes string) (labour.LabourEntry, error) {
	e, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return labour.LabourEntry{}, err
	}
	e.Quantity = q
	e.Rate = rate
	e.Cost = cost
	e.Date = date
	e.Notes = notes
	f.byID[id] = e
	return e, nil
}

type fakeLabourProjectLookup struct{ belongs bool }

func (f fakeLabourProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeLabourWorkItemLookup struct{ belongs bool }

func (f fakeLabourWorkItemLookup) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error) {
	return f.belongs, nil
}

func (f fakeLabourWorkItemLookup) WorkItemBelongsToCompany(ctx context.Context, companyID, workItemID string) (bool, error) {
	return f.belongs, nil
}

// fakeLabourCostRecorder tracks calls so tests can assert write ordering and
// compensation behavior (design spec §22-B).
type fakeLabourCostRecorder struct {
	created          map[string]money.Money // costItemID -> estimated
	next             int
	failNextRecord   bool
	deletedIDs       []string
	updatedEstimates map[string]money.Money
}

func newFakeLabourCostRecorder() *fakeLabourCostRecorder {
	return &fakeLabourCostRecorder{created: make(map[string]money.Money), updatedEstimates: make(map[string]money.Money)}
}

func (f *fakeLabourCostRecorder) RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (string, error) {
	if f.failNextRecord {
		f.failNextRecord = false
		return "", errors.New("simulated CostItem write failure")
	}
	f.next++
	id := "cost_item_" + string(rune('0'+f.next))
	f.created[id] = estimated
	return id, nil
}

func (f *fakeLabourCostRecorder) UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error {
	f.updatedEstimates[costItemID] = estimated
	return nil
}

func (f *fakeLabourCostRecorder) DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error {
	f.deletedIDs = append(f.deletedIDs, costItemID)
	delete(f.created, costItemID)
	return nil
}

func newTestLabourService() (*labour.Service, *fakeWorkerRepository, *fakeLabourEntryRepository, *fakeLabourCostRecorder) {
	workerRepo := newFakeWorkerRepository()
	entryRepo := newFakeLabourEntryRepository()
	recorder := newFakeLabourCostRecorder()
	svc := labour.NewService(workerRepo, entryRepo, fakeLabourProjectLookup{belongs: true}, fakeLabourWorkItemLookup{belongs: true}, recorder)
	return svc, workerRepo, entryRepo, recorder
}

func TestCreateWorkerRequiresValidRateType(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	_, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", "not_a_real_rate_type", 15000, "MYR", "", "")
	if err != labour.ErrInvalidRateType {
		t.Fatalf("expected ErrInvalidRateType, got %v", err)
	}
}

func TestCreateWorkerSuccess(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	w, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.DefaultRate.Amount != 15000 {
		t.Fatalf("expected 15000, got %d", w.DefaultRate.Amount)
	}
}

func TestCreateLabourEntryWithWorkerSnapshotsRateAndCreatesLinkedCostItem(t *testing.T) {
	svc, _, entryRepo, recorder := newTestLabourService()
	worker, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", &worker.ID,
		"", "", "8", "day", nil, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating labour entry: %v", err)
	}
	if entry.WorkerName != "Ahmad" || entry.Trade != "Tiler" {
		t.Fatalf("expected snapshotted name/trade, got %s/%s", entry.WorkerName, entry.Trade)
	}
	if entry.Rate.Amount != 15000 {
		t.Fatalf("expected snapshotted rate 15000, got %d", entry.Rate.Amount)
	}
	if entry.Cost.Amount != 120000 { // 8 days * RM150.00 = RM1,200.00
		t.Fatalf("expected cost 120000, got %d", entry.Cost.Amount)
	}
	if entry.CostItemID == "" {
		t.Fatal("expected a linked CostItemID")
	}

	stored, err := entryRepo.FindByID(context.Background(), "company_a", entry.ID)
	if err != nil {
		t.Fatalf("unexpected error fetching stored entry: %v", err)
	}
	if stored.CostItemID != entry.CostItemID {
		t.Fatalf("expected persisted CostItemID to match")
	}
	if recorder.created[entry.CostItemID].Amount != 120000 {
		t.Fatalf("expected CostItem to have been recorded with estimated 120000, got %+v", recorder.created[entry.CostItemID])
	}
}

func TestCreateLabourEntryAdHocRequiresManualFields(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	_, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"", "", "8", "day", nil, "MYR", time.Now(), "")
	if err != labour.ErrAdHocFieldsRequired {
		t.Fatalf("expected ErrAdHocFieldsRequired, got %v", err)
	}
}

func TestCreateLabourEntryAdHocSuccess(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.WorkerName != "Day Labourer" || entry.Trade != "General" {
		t.Fatalf("expected manual name/trade, got %s/%s", entry.WorkerName, entry.Trade)
	}
	if entry.Cost.Amount != 20000 { // 8 hours * RM25.00 = RM200.00
		t.Fatalf("expected cost 20000, got %d", entry.Cost.Amount)
	}
}

func TestCreateLabourEntryRateOverride(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	worker, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	overrideRate := int64(20000) // project-specific override, higher than default
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", &worker.ID,
		"", "", "8", "day", &overrideRate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Rate.Amount != 20000 {
		t.Fatalf("expected overridden rate 20000, got %d", entry.Rate.Amount)
	}
}

func TestCreateLabourEntryCompensatesOnLabourEntryWriteFailure(t *testing.T) {
	svc, _, entryRepo, recorder := newTestLabourService()
	entryRepo.failNextCreate = true

	rate := int64(2500)
	_, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err == nil {
		t.Fatal("expected error propagated from LabourEntry write failure")
	}
	if len(recorder.deletedIDs) != 1 {
		t.Fatalf("expected exactly 1 compensation delete call, got %d", len(recorder.deletedIDs))
	}
	if len(recorder.created) != 0 {
		t.Fatalf("expected the orphaned CostItem to be removed from the recorder's view, got %d remaining", len(recorder.created))
	}
}

func TestUpdateLabourEntryCorrectsQuantityAndPropagatesEstimatedOnly(t *testing.T) {
	svc, _, _, recorder := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newQty := "80" // typo-correction scenario from the design spec
	updated, err := svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, &newQty, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error correcting: %v", err)
	}
	if updated.Cost.Amount != 200000 { // 80 hours * RM25.00 = RM2,000.00
		t.Fatalf("expected recomputed cost 200000, got %d", updated.Cost.Amount)
	}
	if recorder.updatedEstimates[entry.CostItemID].Amount != 200000 {
		t.Fatalf("expected linked CostItem's Estimated updated to 200000, got %+v", recorder.updatedEstimates[entry.CostItemID])
	}
}

func TestUpdateLabourEntryQuantityUnitOnlyStillRebuildsQuantity(t *testing.T) {
	// Supplying only quantityUnit (not quantityValue) must still rebuild the
	// Quantity with the new unit, not silently do nothing.
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newUnit := "day"
	updated, err := svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, nil, &newUnit, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error correcting unit only: %v", err)
	}
	if updated.Quantity.Unit != "day" {
		t.Fatalf("expected unit corrected to day, got %s", updated.Quantity.Unit)
	}
	if !updated.Quantity.Value.Equal(entry.Quantity.Value) {
		t.Fatalf("expected quantity value to remain %s, got %s", entry.Quantity.Value.String(), updated.Quantity.Value.String())
	}
}

func TestUpdateLabourEntryCanClearNotesToEmptyString(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "original notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	emptyNotes := ""
	updated, err := svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, nil, nil, nil, nil, &emptyNotes)
	if err != nil {
		t.Fatalf("unexpected error clearing notes: %v", err)
	}
	if updated.Notes != "" {
		t.Fatalf("expected notes cleared to empty string, got %q", updated.Notes)
	}
}

func TestUpdateLabourEntryCorrectsDate(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	originalDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", originalDate, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newDate := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	newDateStr := newDate.Format(time.RFC3339)
	updated, err := svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, nil, nil, nil, &newDateStr, nil)
	if err != nil {
		t.Fatalf("unexpected error correcting date: %v", err)
	}
	if !updated.Date.Equal(newDate) {
		t.Fatalf("expected date corrected to %v, got %v", newDate, updated.Date)
	}
}

func TestUpdateLabourEntryNeverAcceptsWorkerIDChange(t *testing.T) {
	// UpdateLabourEntry's signature structurally has no workerID parameter —
	// this test documents that invariant by confirming the method signature
	// only accepts quantity/rate/date/notes corrections.
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	newNotes := "just a notes correction"
	_, err = svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, nil, nil, nil, nil, &newNotes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkerDefaultRateChangeNeverAffectsExistingLabourEntry(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	worker, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", &worker.ID,
		"", "", "8", "day", nil, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating entry: %v", err)
	}

	if _, err := svc.UpdateWorker(context.Background(), "company_a", worker.ID, "Ahmad", "Tiler", string(labour.RateTypeDaily), 30000, "MYR", "", ""); err != nil {
		t.Fatalf("unexpected error updating worker: %v", err)
	}

	unchanged, err := svc.GetLabourEntry(context.Background(), "company_a", entry.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unchanged.Rate.Amount != 15000 {
		t.Fatalf("expected LabourEntry.Rate to remain 15000 after Worker.DefaultRate changed, got %d", unchanged.Rate.Amount)
	}
}

func TestListLabourEntriesByWorkItemRejectsForeignWorkItem(t *testing.T) {
	workerRepo := newFakeWorkerRepository()
	entryRepo := newFakeLabourEntryRepository()
	recorder := newFakeLabourCostRecorder()
	svc := labour.NewService(workerRepo, entryRepo, fakeLabourProjectLookup{belongs: true}, fakeLabourWorkItemLookup{belongs: false}, recorder)

	_, err := svc.ListLabourEntriesByWorkItem(context.Background(), "company_a", "foreign_work_item")
	if err != labour.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for a workItemID that does not belong to the company, got %v", err)
	}
}
