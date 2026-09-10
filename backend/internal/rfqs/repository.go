package rfqs

import (
	"context"
	"time"
)

// RFQRepository persists RFQs. rfqs owns the rfqs collection exclusively; no
// other module may query it directly, and M8 never writes into it (ADR 0002,
// design spec §9).
//
// The write methods are narrow rather than one general Update, so each carries
// the state filter its operation requires: UpdateDraft and ReplaceLines are
// draft-only, MarkReady requires draft, Reopen requires ready. That puts §6.4's
// lifecycle rules in the WRITE rather than only in the service, where a
// concurrent transition could race past a service-level check.
type RFQRepository interface {
	Create(ctx context.Context, r RFQ) (RFQ, error)
	FindByID(ctx context.Context, companyID, id string) (RFQ, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]RFQ, error)

	// FindByLineRequirementID locates the RFQ whose lines snapshot one
	// requirement, backed by the multikey index. It serves the §7.3 retry
	// matrix and §7.5 reconciliation.
	FindByLineRequirementID(ctx context.Context, companyID, requirementID string) (RFQ, bool, error)

	// UpdateDraft applies a header edit. Draft-only: a ready RFQ's
	// supplier-visible scope must not change underneath a supplier.
	UpdateDraft(ctx context.Context, companyID, id string, expectedRevision int64,
		updated RFQ) (RFQ, error)

	// ReplaceLines writes the whole line array under a Revision guard. Lines
	// are a value collection inside the aggregate, so add, remove and reorder
	// all go through one conditional write.
	ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64,
		lines []RFQLine) (RFQ, error)

	// MarkReady transitions draft -> ready and INCREMENTS Revision, closing the
	// §6.4 ABA hazard.
	MarkReady(ctx context.Context, companyID, id string, expectedRevision int64,
		readyAt time.Time) (RFQ, error)

	// Reopen transitions ready -> draft and likewise increments Revision.
	Reopen(ctx context.Context, companyID, id string, expectedRevision int64,
		reopenedAt time.Time) (RFQ, error)

	// Delete removes an RFQ under a Revision guard. The caller enforces §7.7's
	// three conditions; this only guarantees the document has not moved.
	Delete(ctx context.Context, companyID, id string, expectedRevision int64) error
}

// RFQCounterRepository allocates tenant-scoped RFQ numbers.
//
// It is a SEPARATE collection and interface from RFQRepository because the
// counter is not part of the aggregate: it must be incremented atomically and
// independently of any RFQ document (design spec §6.1).
type RFQCounterRepository interface {
	// NextRFQNumber atomically increments and returns companyID's next
	// sequence value. Atomic by construction — FindOneAndUpdate + $inc +
	// upsert — so there is no read-then-write race and no retry loop.
	NextRFQNumber(ctx context.Context, companyID string) (int64, error)
}
