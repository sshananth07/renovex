package costs

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrCostItemNotFound is returned when a CostItem lookup finds no match —
// including a CostItem that exists but belongs to a different company.
var ErrCostItemNotFound = errors.New("costs: cost item not found")

// ErrRevisionMismatch is returned when a conditional write's expectedRevision
// does not match the CostItem's current revision — a concurrent write
// happened first. Distinct from ErrCostItemNotFound (missing/foreign
// document): a repository re-reads on a 0-match to tell the two apart.
var ErrRevisionMismatch = errors.New("costs: revision mismatch")

// CostItemRepository persists CostItems. costs owns the cost_items
// collection exclusively; no other module may query it directly.
type CostItemRepository interface {
	Create(ctx context.Context, c CostItem) (CostItem, error)
	FindByID(ctx context.Context, companyID, id string) (CostItem, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error)
	ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error)
	UpdateLifecycleField(ctx context.Context, companyID, id string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error)
	UpdateDetails(ctx context.Context, companyID, id string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error)
	// RecordActual sets Actual for the first time (no prior value must exist
	// — the SERVICE enforces that precondition via FindByID before calling
	// this; the repository itself just performs the CAS-guarded $set). Use
	// CorrectActual once Actual is already populated.
	RecordActual(ctx context.Context, companyID, id string, expectedRevision int64, amount money.Money) (CostItem, error)
	// CorrectActual atomically (a) sets Actual to newAmount, (b) bumps
	// revision, and (c) appends correction to ActualCorrections — all in ONE
	// Mongo UpdateOne combining $set and $push, guarded by the same
	// companyId+_id+expectedRevision filter every other conditional write in
	// this package uses. No transaction and no second collection: MongoDB
	// guarantees a single document's $set+$push in one UpdateOne is atomic.
	CorrectActual(ctx context.Context, companyID, id string, expectedRevision int64, newAmount money.Money, correction ActualCorrection) (CostItem, error)
	// Delete is used only by LabourCostRecorder's best-effort compensation
	// path (design spec §22-B) — never exposed via HTTP.
	Delete(ctx context.Context, companyID, id string) error
}
