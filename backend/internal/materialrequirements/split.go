package materialrequirements

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// newSplitID pre-generates a stable child identifier. Split children need their
// IDs BEFORE they are inserted, because §8.4's manifest is committed in step 1
// and must fully determine the outcome of a retry.
//
// The same bson.NewObjectID().Hex() convention as quotations/service.go:148.
func newSplitID() string { return bson.NewObjectID().Hex() }

// SplitChildInput is one requested batch. The unit is carried explicitly rather
// than assumed, so a caller that means to change it is REJECTED (§8.1) instead
// of having the source unit silently substituted.
type SplitChildInput struct {
	QuantityValue    string
	QuantityUnit     string
	RequiredByDate   *time.Time
	ProcurementNotes string
	InternalNotes    string
}

// SplitResult reports a completed split: the terminal source and its children.
type SplitResult struct {
	Source   MaterialRequirement
	Children []MaterialRequirement
}

// SplitRequirement divides one requirement into two or more batches (design
// spec §8).
//
// The write order is §8.4's, and it is deliberately NOT a transaction — M7
// claims no cross-document atomicity (§15). What makes it safe is that the
// MANIFEST, including each child's pre-generated stable ID, is committed in
// step 1 before any child exists. A crash therefore leaves a source marked
// `creating` that ReconcileSplit can complete into exactly the same children,
// and the primary key forbids duplicating any child that already landed.
func (s *Service) SplitRequirement(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64, children []SplitChildInput) (SplitResult, error) {

	source, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return SplitResult{}, err
	}
	// §8.3's preconditions, each with the sentinel that names the contractor's
	// actionable next step. SplittableErr owns the ordering.
	if err := source.SplittableErr(); err != nil {
		return SplitResult{}, err
	}

	quantities, err := validateSplitChildren(source, children)
	if err != nil {
		return SplitResult{}, err
	}

	// Pre-generate every child ID so the manifest written in step 1 fully
	// determines the outcome (design spec §8.4).
	splitGroupID := newSplitID()
	manifest := make([]SplitChildRef, 0, len(quantities))
	for i, q := range quantities {
		manifest = append(manifest, SplitChildRef{
			RequirementID: newSplitID(), Quantity: q, Sequence: i,
		})
	}

	// --- Step 1: source -> split + manifest, in one conditional update. ---
	begun, err := s.repo.BeginSplit(ctx, companyID, requirementID, expectedRevision,
		manifest, splitGroupID, actorUserID, time.Now())
	if err != nil {
		return SplitResult{}, err
	}

	// --- Steps 2-4. ---
	return s.completeSplitFromManifest(ctx, companyID, actorUserID, begun, children)
}

// ReconcileSplit completes an interrupted split from its manifest (design spec
// §8.4). It is idempotent: children that already exist are reused, and a split
// that already completed is a no-op.
//
// It never invents children — the manifest committed in step 1 is the only
// source of truth for what the split owes.
func (s *Service) ReconcileSplit(ctx context.Context, companyID, actorUserID, requirementID string,
	expectedRevision int64) (SplitResult, error) {

	source, err := s.repo.FindByID(ctx, companyID, requirementID)
	if err != nil {
		return SplitResult{}, err
	}
	if len(source.SplitChildren) == 0 || source.SplitGroupID == nil {
		return SplitResult{}, ErrNoSplitToReconcile
	}
	if source.SplitState == SplitStateCompleted {
		// Already done. Return the existing children rather than an error, so a
		// duplicated recovery request is harmless.
		existing, err := s.repo.ListSplitChildren(ctx, companyID, *source.SplitGroupID)
		if err != nil {
			return SplitResult{}, err
		}
		sortBySplitSequence(existing)
		return SplitResult{Source: source, Children: existing}, nil
	}
	if source.Revision != expectedRevision {
		return SplitResult{}, ErrRevisionMismatch
	}

	// Reconciliation carries no per-child input: the manifest fixes the
	// quantities, and the descriptive fields come from the source.
	return s.completeSplitFromManifest(ctx, companyID, actorUserID, source, nil)
}

// completeSplitFromManifest performs steps 2-4 for both the initial split and
// reconciliation, so the two paths cannot drift apart.
//
// inputs may be nil (reconciliation), in which case only the manifest and the
// source's own fields determine each child.
func (s *Service) completeSplitFromManifest(ctx context.Context, companyID, actorUserID string,
	source MaterialRequirement, inputs []SplitChildInput) (SplitResult, error) {

	if source.SplitGroupID == nil {
		return SplitResult{}, ErrNoSplitToReconcile
	}
	groupID := *source.SplitGroupID

	// Which manifest entries already exist? Asking once up front means recovery
	// creates only what is genuinely missing, rather than re-attempting every
	// child and relying on the primary key to reject the duplicates.
	//
	// The duplicate-key path below is still handled: this read can go stale
	// under a concurrent reconciliation, and the primary key — not this map —
	// remains the authority (design spec §8.4).
	existingByID := map[string]MaterialRequirement{}
	present, err := s.repo.ListSplitChildren(ctx, companyID, groupID)
	if err != nil {
		return SplitResult{}, err
	}
	for _, c := range present {
		existingByID[c.ID] = c
	}

	// --- Step 2: idempotent inserts at the manifest's pre-generated IDs. ---
	children := make([]MaterialRequirement, 0, len(source.SplitChildren))
	for _, ref := range source.SplitChildren {
		if existing, ok := existingByID[ref.RequirementID]; ok {
			children = append(children, existing)
			continue
		}

		var input SplitChildInput
		if ref.Sequence < len(inputs) {
			input = inputs[ref.Sequence]
		}

		child := buildSplitChild(source, ref, groupID, actorUserID, input)
		created, err := s.repo.CreateWithID(ctx, child)
		if errors.Is(err, ErrRequirementIDExists) {
			// A concurrent reconciliation won this child between the read above
			// and this insert. Load it rather than failing, so both callers
			// converge on the same set instead of one erroring out.
			existing, findErr := s.repo.FindByID(ctx, companyID, ref.RequirementID)
			if findErr != nil {
				return SplitResult{}, findErr
			}
			children = append(children, existing)
			continue
		}
		if err != nil {
			// The source stays `creating` deliberately: it is NOT rolled back,
			// and ReconcileSplit is the documented recovery (design spec §8.4).
			return SplitResult{}, err
		}
		children = append(children, created)
	}

	// --- Step 3: creating -> completed. ---
	completed, err := s.repo.CompleteSplit(ctx, companyID, source.ID, source.Revision)
	if err != nil {
		return SplitResult{}, err
	}

	// --- Step 4: audit, best-effort as everywhere in this module. ---
	_ = s.audit.RecordMaterialRequirementSplit(ctx, companyID, source.ProjectID, actorUserID,
		source.ID, groupID, len(children))

	sortBySplitSequence(children)
	return SplitResult{Source: completed, Children: children}, nil
}

// buildSplitChild assembles one child from the manifest entry and the source.
//
// The child carries NO source aggregation identity: copying it would make each
// child appear independently backed by the full original aggregate, when
// provenance actually resolves CostItems -> source -> children (design spec
// §8.2).
func buildSplitChild(source MaterialRequirement, ref SplitChildRef, groupID, actorUserID string,
	input SplitChildInput) MaterialRequirement {

	sequence := ref.Sequence
	sourceID := source.ID
	now := time.Now()

	requiredBy := source.RequiredByDate
	if input.RequiredByDate != nil {
		requiredBy = input.RequiredByDate
	}
	procurementNotes := source.ProcurementNotes
	if input.ProcurementNotes != "" {
		procurementNotes = input.ProcurementNotes
	}
	internalNotes := source.InternalNotes
	if input.InternalNotes != "" {
		internalNotes = input.InternalNotes
	}

	return MaterialRequirement{
		ID:        ref.RequirementID,
		CompanyID: source.CompanyID,
		ProjectID: source.ProjectID,
		// Identity is inherited verbatim; §8.1 forbids any conversion.
		WorkItemID:       source.WorkItemID,
		MaterialID:       source.MaterialID,
		RequiredQuantity: ref.Quantity,

		// Descriptive snapshots, not source claims (design spec §8.2).
		MaterialName:             source.MaterialName,
		Specification:            source.Specification,
		CatalogUnit:              source.CatalogUnit,
		UnitMismatch:             source.UnitMismatch,
		UnitMismatchAcknowledged: source.UnitMismatchAcknowledged,

		RequiredByDate:   requiredBy,
		ProcurementNotes: procurementNotes,
		InternalNotes:    internalNotes,

		// Children start as draft: the contractor must confirm each batch
		// (design spec §8.1).
		Status:     RequirementStatusDraft,
		SourceType: SourceTypeSplit,
		// A split child has no CostItem aggregate of its own, so it is
		// permanently clean (design spec §8.5).
		SourceSyncState: SourceSyncStateClean,

		SplitFromRequirementID: &sourceID,
		SplitGroupID:           &groupID,
		SplitSequence:          &sequence,

		CreatedByUserID: actorUserID,
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}
}

// validateSplitChildren enforces §8.1's invariants before anything is written,
// so a rejected split creates nothing and leaves the source untouched.
func validateSplitChildren(source MaterialRequirement, children []SplitChildInput) ([]quantity.Quantity, error) {
	if len(children) < 2 {
		return nil, ErrSplitTooFewChildren
	}

	quantities := make([]quantity.Quantity, 0, len(children))
	total := decimal.Zero
	for _, c := range children {
		q, err := parseQuantity(c.QuantityValue, c.QuantityUnit)
		if err != nil {
			// parseQuantity already rejects zero and negative values.
			return nil, err
		}
		// M7 never converts between units, so a child in another unit could not
		// conserve the source quantity (design spec §8.1).
		if UnitsMismatch(q.Unit, source.RequiredQuantity.Unit) {
			return nil, ErrSplitUnitChanged
		}
		// The source unit is preserved verbatim rather than the caller's
		// spelling, so children are byte-identical to the source.
		q.Unit = source.RequiredQuantity.Unit
		quantities = append(quantities, q)
		total = total.Add(q.Value)
	}

	// Exact conservation — decimal.Equal, no tolerance (design spec §8.1).
	if !total.Equal(source.RequiredQuantity.Value) {
		return nil, ErrSplitQuantityMismatch
	}
	return quantities, nil
}

// sortBySplitSequence orders children by their manifest sequence, so a split's
// result is reproducible regardless of insert or query order.
func sortBySplitSequence(children []MaterialRequirement) {
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].SplitSequence == nil || children[j].SplitSequence == nil {
			return false
		}
		return *children[i].SplitSequence < *children[j].SplitSequence
	})
}
