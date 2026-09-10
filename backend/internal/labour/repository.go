package labour

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrWorkerNotFound is returned when a Worker lookup finds no match —
// including a Worker that exists but belongs to a different company.
var ErrWorkerNotFound = errors.New("labour: worker not found")

// ErrLabourEntryNotFound is returned when a LabourEntry lookup finds no
// match — including one that exists but belongs to a different company.
var ErrLabourEntryNotFound = errors.New("labour: labour entry not found")

// WorkerRepository persists Workers. labour owns the workers collection
// exclusively; no other module may query it directly.
type WorkerRepository interface {
	Create(ctx context.Context, w Worker) (Worker, error)
	FindByID(ctx context.Context, companyID, id string) (Worker, error)
	List(ctx context.Context, companyID string) ([]Worker, error)
	Update(ctx context.Context, companyID, id string, fn func(*Worker)) (Worker, error)
}

// LabourEntryRepository persists LabourEntries. labour owns the
// labour_entries collection exclusively; no other module may query it
// directly.
type LabourEntryRepository interface {
	Create(ctx context.Context, e LabourEntry) (LabourEntry, error)
	FindByID(ctx context.Context, companyID, id string) (LabourEntry, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error)
	ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error)
	// UpdateCorrection persists a corrected Quantity/Rate/Cost/Date/Notes for
	// an existing LabourEntry — the only mutation path, backing the narrow
	// PATCH /labour-entries/{id} (design spec §1.3.2). Never changes WorkerID.
	UpdateCorrection(ctx context.Context, companyID, id string, quantity quantity.Quantity, rate money.Money, cost money.Money, date time.Time, notes string) (LabourEntry, error)
}
