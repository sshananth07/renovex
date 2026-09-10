package labour

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrNameRequired is returned when CreateWorker is given an empty name.
var ErrNameRequired = errors.New("labour: name is required")

// ErrInvalidRateType is returned when CreateWorker/UpdateWorker is given a
// rateType outside the 5 defined values.
var ErrInvalidRateType = errors.New("labour: invalid rate type")

// ErrProjectNotFound is returned when the given projectID does not belong to
// the caller's company.
var ErrProjectNotFound = errors.New("labour: project not found")

// ErrWorkItemNotFound is returned when a given workItemID does not belong to
// the caller's company, or belongs to the company but not to the given
// projectID (lineage mismatch).
var ErrWorkItemNotFound = errors.New("labour: work item not found")

// ErrInvalidQuantity is returned when a quantity value/unit fails to parse or
// is not strictly positive.
var ErrInvalidQuantity = errors.New("labour: quantity must be a positive number with a non-empty unit")

// ErrAdHocFieldsRequired is returned when workerID is nil and workerName,
// trade, or rateAmount is missing — ad-hoc labour requires all three manually
// (design spec §1.3.3).
var ErrAdHocFieldsRequired = errors.New("labour: workerName, trade, and rateAmount are required when workerId is not supplied")

// ErrInvalidDate is returned when UpdateLabourEntry is given a date string
// that fails to parse as RFC3339.
var ErrInvalidDate = errors.New("labour: date must be a valid RFC3339 timestamp")

// ProjectLookup is the capability labour needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// WorkItemLookup is the capability labour needs from work: confirming a
// workItemID belongs to both companyID and projectID in one compound check
// (used by CreateLabourEntry), and a company-ownership-only check (used by
// ListLabourEntriesByWorkItem, which has no second parent ID to cross-check).
type WorkItemLookup interface {
	WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
	WorkItemBelongsToCompany(ctx context.Context, companyID, workItemID string) (bool, error)
}

// LabourCostRecorder is the capability labour needs from costs — defined here
// (consumer-defines-interface), satisfied structurally by costs.Service
// (design spec §9.4).
type LabourCostRecorder interface {
	RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (costItemID string, err error)
	UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error
	DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error
}

// Service implements Worker and LabourEntry CRUD, including the two-module
// write (LabourEntry <-> CostItem) with cost-first-with-compensation
// ordering (design spec §22-B).
type Service struct {
	workerRepo     WorkerRepository
	entryRepo      LabourEntryRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	costRecorder   LabourCostRecorder
}

// NewService constructs a Service backed by workerRepo and entryRepo,
// consuming projectLookup, workItemLookup, and costRecorder.
func NewService(workerRepo WorkerRepository, entryRepo LabourEntryRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, costRecorder LabourCostRecorder) *Service {
	return &Service{workerRepo: workerRepo, entryRepo: entryRepo, projectLookup: projectLookup, workItemLookup: workItemLookup, costRecorder: costRecorder}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of either public repository interface. Only the real Mongo
// repositories implement it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Worker AND LabourEntry
// owned by companyID — both collections, one method, per Task 1a's
// multi-collection module guidance. Development-tool use only (demoseed
// reset, design spec §6.6). Idempotent: a partial failure is safe to
// re-call, since each underlying DeleteMany is itself a no-op on
// already-deleted data.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	workerDeleter, ok := s.workerRepo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("labour: worker repository %T does not support DeleteAllForCompany", s.workerRepo)
	}
	if err := workerDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	entryDeleter, ok := s.entryRepo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("labour: labour entry repository %T does not support DeleteAllForCompany", s.entryRepo)
	}
	return entryDeleter.DeleteAllForCompany(ctx, companyID)
}

// CreateWorker validates name is non-empty and rateType is one of the 5
// defined values, then persists a new Worker.
func (s *Service) CreateWorker(ctx context.Context, companyID, name, trade, rateType string, defaultRateAmount int64, currency, contactPhone, contactEmail string) (Worker, error) {
	if name == "" {
		return Worker{}, ErrNameRequired
	}
	rt := RateType(rateType)
	if !rt.IsValid() {
		return Worker{}, ErrInvalidRateType
	}
	return s.workerRepo.Create(ctx, Worker{
		CompanyID: companyID, Name: name, Trade: trade, RateType: rt,
		DefaultRate: money.New(defaultRateAmount, currency), ContactPhone: contactPhone, ContactEmail: contactEmail,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetWorker returns workerID's Worker, tenant-scoped to companyID.
func (s *Service) GetWorker(ctx context.Context, companyID, workerID string) (Worker, error) {
	return s.workerRepo.FindByID(ctx, companyID, workerID)
}

// ListWorkers returns every Worker belonging to companyID.
func (s *Service) ListWorkers(ctx context.Context, companyID string) ([]Worker, error) {
	return s.workerRepo.List(ctx, companyID)
}

// UpdateWorker updates workerID's fields, tenant-scoped to companyID.
// Updating DefaultRate here never retroactively alters any already-created
// LabourEntry (design spec §1.3.6) — this method only ever writes to the
// workers collection.
func (s *Service) UpdateWorker(ctx context.Context, companyID, workerID, name, trade, rateType string, defaultRateAmount int64, currency, contactPhone, contactEmail string) (Worker, error) {
	if name == "" {
		return Worker{}, ErrNameRequired
	}
	rt := RateType(rateType)
	if !rt.IsValid() {
		return Worker{}, ErrInvalidRateType
	}
	return s.workerRepo.Update(ctx, companyID, workerID, func(w *Worker) {
		w.Name = name
		w.Trade = trade
		w.RateType = rt
		w.DefaultRate = money.New(defaultRateAmount, currency)
		w.ContactPhone = contactPhone
		w.ContactEmail = contactEmail
	})
}

// CreateLabourEntry validates projectID and workItemID (lineage-checked
// against projectID), resolves the Worker snapshot or ad-hoc manual fields,
// computes Cost via money.CalculateLineAmount, and writes the linked CostItem
// FIRST via LabourCostRecorder before writing the LabourEntry itself — if the
// LabourEntry write then fails, the CostItem is deleted as best-effort
// compensation (design spec §5.2 step 7, §22-B).
func (s *Service) CreateLabourEntry(ctx context.Context, companyID, projectID, workItemID string, workerID *string,
	workerName, trade, quantityValue, quantityUnit string, rateAmount *int64, currency string, date time.Time, notes string) (LabourEntry, error) {

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return LabourEntry{}, err
	}
	if !belongs {
		return LabourEntry{}, ErrProjectNotFound
	}

	wiBelongs, err := s.workItemLookup.WorkItemBelongsToProject(ctx, companyID, workItemID, projectID)
	if err != nil {
		return LabourEntry{}, err
	}
	if !wiBelongs {
		return LabourEntry{}, ErrWorkItemNotFound
	}

	q, err := quantity.New(quantityValue, quantityUnit)
	if err != nil {
		return LabourEntry{}, ErrInvalidQuantity
	}
	if !q.Value.IsPositive() || q.Unit == "" {
		return LabourEntry{}, ErrInvalidQuantity
	}

	var rate money.Money
	var resolvedName, resolvedTrade string

	if workerID != nil {
		worker, err := s.workerRepo.FindByID(ctx, companyID, *workerID)
		if err != nil {
			return LabourEntry{}, err
		}
		resolvedName = worker.Name
		resolvedTrade = worker.Trade
		if rateAmount != nil {
			rate = money.New(*rateAmount, currency)
		} else {
			rate = worker.DefaultRate
		}
	} else {
		if workerName == "" || trade == "" || rateAmount == nil {
			return LabourEntry{}, ErrAdHocFieldsRequired
		}
		resolvedName = workerName
		resolvedTrade = trade
		rate = money.New(*rateAmount, currency)
	}

	cost := money.CalculateLineAmount(q.Value, rate)

	costItemID, err := s.costRecorder.RecordLabourCost(ctx, companyID, projectID, workItemID, cost)
	if err != nil {
		return LabourEntry{}, err
	}

	entry, err := s.entryRepo.Create(ctx, LabourEntry{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID, WorkerID: workerID,
		WorkerName: resolvedName, Trade: resolvedTrade, Quantity: q, Rate: rate, Cost: cost,
		CostItemID: costItemID, Date: date, Notes: notes, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		// Best-effort compensation: the LabourEntry write failed after the
		// CostItem was already created. Delete the now-orphaned CostItem;
		// log clearly if compensation itself fails (design spec §22-B).
		if compErr := s.costRecorder.DeleteProvisionedLabourCost(ctx, companyID, costItemID); compErr != nil {
			return LabourEntry{}, errors.Join(err, compErr)
		}
		return LabourEntry{}, err
	}

	return entry, nil
}

// GetLabourEntry returns labourEntryID's LabourEntry, tenant-scoped to companyID.
func (s *Service) GetLabourEntry(ctx context.Context, companyID, labourEntryID string) (LabourEntry, error) {
	return s.entryRepo.FindByID(ctx, companyID, labourEntryID)
}

// ListLabourEntriesByProject validates projectID belongs to companyID before listing.
func (s *Service) ListLabourEntriesByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.entryRepo.ListByProject(ctx, companyID, projectID)
}

// ListLabourEntriesByWorkItem validates workItemID belongs to companyID
// before listing — a foreign workItemID returns 404, never an empty list
// (tenant invariant: foreign parent ID -> 404, never []).
func (s *Service) ListLabourEntriesByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error) {
	belongs, err := s.workItemLookup.WorkItemBelongsToCompany(ctx, companyID, workItemID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrWorkItemNotFound
	}
	return s.entryRepo.ListByWorkItem(ctx, companyID, workItemID)
}

// UpdateLabourEntry corrects quantity/rate/date/notes only — never workerId
// (design spec §1.3.2). Each parameter is a pointer: nil means "not
// supplied, leave unchanged"; a non-nil pointer to an empty string (notes)
// is a deliberate clear, distinguishable from "not supplied." quantityValue
// and quantityUnit are independent — supplying only one still rebuilds the
// Quantity, reusing the existing value for whichever field was omitted. When
// quantity or rate changes, recomputes Cost and propagates the new value to
// the linked CostItem's Estimated field FIRST (via
// LabourCostRecorder.UpdateLabourCostEstimate), then persists the
// LabourEntry itself — the same cost-first ordering principle as creation.
// Never touches Committed/Actual/Paid on the linked CostItem.
func (s *Service) UpdateLabourEntry(ctx context.Context, companyID, labourEntryID string, quantityValue, quantityUnit *string, rateAmount *int64, dateStr *string, notes *string) (LabourEntry, error) {
	existing, err := s.entryRepo.FindByID(ctx, companyID, labourEntryID)
	if err != nil {
		return LabourEntry{}, err
	}

	newQuantity := existing.Quantity
	if quantityValue != nil || quantityUnit != nil {
		value := existing.Quantity.Value.String()
		if quantityValue != nil {
			value = *quantityValue
		}
		unit := existing.Quantity.Unit
		if quantityUnit != nil {
			unit = *quantityUnit
		}
		q, err := quantity.New(value, unit)
		if err != nil {
			return LabourEntry{}, ErrInvalidQuantity
		}
		if !q.Value.IsPositive() || q.Unit == "" {
			return LabourEntry{}, ErrInvalidQuantity
		}
		newQuantity = q
	}

	newRate := existing.Rate
	if rateAmount != nil {
		newRate = money.New(*rateAmount, existing.Rate.Currency)
	}

	newDate := existing.Date
	if dateStr != nil {
		parsed, err := time.Parse(time.RFC3339, *dateStr)
		if err != nil {
			return LabourEntry{}, ErrInvalidDate
		}
		newDate = parsed
	}

	newNotes := existing.Notes
	if notes != nil {
		newNotes = *notes
	}

	newCost := money.CalculateLineAmount(newQuantity.Value, newRate)

	if err := s.costRecorder.UpdateLabourCostEstimate(ctx, companyID, existing.CostItemID, newCost); err != nil {
		return LabourEntry{}, err
	}

	return s.entryRepo.UpdateCorrection(ctx, companyID, labourEntryID, newQuantity, newRate, newCost, newDate, newNotes)
}
