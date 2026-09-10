// Package materialrequirements owns the authoritative, contractor-controlled
// procurement requirement for a Project: generation from material CostItems,
// source discrepancy detection and resolution, splitting, and the claim FIELDS
// an RFQ chain holds (design spec §2-§3, §5, §7-§8).
//
// Module boundary (ADR 0002, design spec §1.1). M7 is three modules, not one:
// materialrequirements, rfqs and suppliers. The dependency is ONE-WAY,
// rfqs -> materialrequirements. This package never imports rfqs, never inspects
// an RFQ line, and never implements retry_line — it performs the conditional
// writes it owns and nothing else, while all claim ORCHESTRATION (sequencing,
// compensation, the retry matrix, reconciliation) is rfqs-owned, because only
// rfqs can read its own lines.
//
// Two invariants shape most of the code here:
//
//   - Detection must NEVER overwrite the accepted source snapshot (§5.8). merge
//     computes proposed − accepted, so a detection-advanced snapshot would
//     collapse the delta to zero and silently discard the contractor's
//     adjustment. The repository enforces it structurally: UpdateSyncState
//     writes two fields, UpdateContractorFields writes no snapshot field, and
//     ApplyDiscrepancyResolution is the only write that may advance it.
//
//   - Money and quantities are never floats. Quantities use shopspring/decimal
//     and persist as canonical decimal STRINGS (ADR 0001).
//
// AI suggests, the system calculates, the contractor approves: nothing in this
// package derives authoritative demand without an explicit contractor action.
package materialrequirements

import (
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// SourceType records where a requirement came from. SourceTypeSplit marks a
// split CHILD — never the terminal split source, which keeps its original
// source type and moves to RequirementStatusSplit instead (design spec §8.2).
type SourceType string

const (
	SourceTypeCostItem SourceType = "cost_item"
	SourceTypeManual   SourceType = "manual"
	SourceTypeSplit    SourceType = "split"
)

// IsValid reports whether s is one of the three defined source types.
func (s SourceType) IsValid() bool {
	switch s {
	case SourceTypeCostItem, SourceTypeManual, SourceTypeSplit:
		return true
	default:
		return false
	}
}

// RequirementStatus is the requirement lifecycle state (design spec §2.1).
// split and archived are both terminal and read-only; they are distinct so the
// record says WHY it became inactive.
type RequirementStatus string

const (
	RequirementStatusDraft    RequirementStatus = "draft"
	RequirementStatusReviewed RequirementStatus = "reviewed"
	RequirementStatusSplit    RequirementStatus = "split"
	RequirementStatusArchived RequirementStatus = "archived"
)

// IsValid reports whether s is one of the four defined statuses.
func (s RequirementStatus) IsValid() bool {
	switch s {
	case RequirementStatusDraft, RequirementStatusReviewed,
		RequirementStatusSplit, RequirementStatusArchived:
		return true
	default:
		return false
	}
}

// SourceSyncState is detection output: how the current CostItem aggregate
// compares against the ACCEPTED source snapshot (design spec §5.3).
type SourceSyncState string

const (
	SourceSyncStateClean          SourceSyncState = "clean"
	SourceSyncStateChangeDetected SourceSyncState = "change_detected"
	SourceSyncStateSourceRemoved  SourceSyncState = "source_removed"
)

// IsValid reports whether s is one of the three defined sync states.
func (s SourceSyncState) IsValid() bool {
	switch s {
	case SourceSyncStateClean, SourceSyncStateChangeDetected, SourceSyncStateSourceRemoved:
		return true
	default:
		return false
	}
}

// SplitState tracks an in-flight split (design spec §8.4). SplitStateNone is
// the empty string, so a requirement never involved in a split carries no split
// state at all — unlike the other enums, "" is valid here.
type SplitState string

const (
	SplitStateNone      SplitState = ""
	SplitStateCreating  SplitState = "creating"
	SplitStateCompleted SplitState = "completed"
)

// IsValid reports whether s is one of the three defined split states.
func (s SplitState) IsValid() bool {
	switch s {
	case SplitStateNone, SplitStateCreating, SplitStateCompleted:
		return true
	default:
		return false
	}
}

// SplitChildRef is one entry of the split manifest written atomically with the
// source's status transition, so an interrupted split can be completed with
// exactly the same children and never duplicates (design spec §8.4).
type SplitChildRef struct {
	RequirementID string
	Quantity      quantity.Quantity
	Sequence      int
}

// MaterialRequirement is the authoritative, contractor-controlled procurement
// requirement for one Project (design spec §2).
//
// Two field groups have restricted writers and must not be confused:
//
//   - The ACCEPTED source snapshot — SourceCostItemIDs, SourceQuantity,
//     SourceFingerprint, SourceSyncedAt — is written only at anchor creation
//     and by an explicit discrepancy resolution. Detection must never touch it,
//     because merge computes proposed − accepted (design spec §5.8).
//   - Detection output — SourceSyncState, SourceCheckedAt — is writable by
//     generation/re-sync even on claimed and terminal requirements.
//
// The RFQ claim fields are owned here but ALL orchestration lives in
// internal/rfqs; this package never inspects an RFQ line (design spec §7.1).
type MaterialRequirement struct {
	ID        string
	CompanyID string
	ProjectID string

	// WorkItemID is required when SourceType == cost_item and nil for manual
	// project-level demand (design spec §2.2).
	WorkItemID *string

	MaterialID    string
	MaterialName  string
	Specification string

	RequiredQuantity quantity.Quantity
	CatalogUnit      string

	UnitMismatch             bool
	UnitMismatchAcknowledged bool

	RequiredByDate   *time.Time
	ProcurementNotes string // supplier-visible; may be snapshotted into an RFQ line
	InternalNotes    string // contractor-only; NEVER snapshotted

	Status RequirementStatus

	SourceType SourceType

	// Immutable source aggregation identity, set only for cost_item anchors.
	// Deliberately not derived from RequiredQuantity.Unit, which the contractor
	// may edit (design spec §3.3).
	SourceAggregationUnit string
	SourceAggregationKey  string

	// Accepted source snapshot — see the type comment for who may write it.
	SourceCostItemIDs []string
	SourceQuantity    *quantity.Quantity
	SourceFingerprint string
	SourceSyncedAt    *time.Time

	// Detection output.
	SourceSyncState SourceSyncState
	SourceCheckedAt *time.Time

	// Split provenance.
	SplitFromRequirementID *string
	SplitGroupID           *string
	SplitSequence          *int
	SplitAt                *time.Time
	SplitByUserID          *string
	SplitState             SplitState
	SplitChildren          []SplitChildRef

	// Discrepancy provenance for create_separate children (design spec §5.6).
	CreatedFromDiscrepancyRequirementID *string
	CreatedFromSourceFingerprint        *string
	ResolutionOperationID               *string

	// RFQ claim (design spec §7).
	ActiveRFQChainID *string
	ActiveRFQNumber  *string
	ActiveRFQLineID  *string
	RFQClaimedAt     *time.Time

	Revision        int64
	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SchemaVersion   int
}

// IsTerminal reports whether the requirement is in a terminal, read-only state
// (design spec §2.1).
func (r MaterialRequirement) IsTerminal() bool {
	return r.Status == RequirementStatusSplit || r.Status == RequirementStatusArchived
}

// IsClaimed reports whether an RFQ chain currently holds this requirement.
func (r MaterialRequirement) IsClaimed() bool {
	return r.ActiveRFQChainID != nil
}

// ContractorEditable reports whether ANY contractor-controlled field may be
// modified. A terminal status or an active claim each independently forbid every
// edit, including InternalNotes — the conservative rule of design spec §2.3
// that makes RFQ-line snapshots stable and removes the need for drift detection.
func (r MaterialRequirement) ContractorEditable() bool {
	return !r.IsTerminal() && !r.IsClaimed()
}

// IdentityFieldsEditable reports whether MaterialID and WorkItemID may change.
// Only a manual requirement qualifies: a cost_item anchor would lose its
// aggregation identity and traceability, and a split child inherits its identity
// from the source (design spec §2.2). Terminal status and an active claim
// override the source-type rule entirely (design spec §2.1).
func (r MaterialRequirement) IdentityFieldsEditable() bool {
	return r.ContractorEditable() && r.SourceType == SourceTypeManual
}

// IsRFQEligible implements the §8.5 predicate for an UNCLAIMED requirement
// being claimed. It is enforced inside the claim's conditional update so it
// cannot be raced.
//
// It is deliberately NOT reused at mark-ready time: the ActiveRFQChainID == nil
// term is false for every requirement already on a line, so reapplying it there
// would reject every non-empty RFQ. Mark-ready uses the claimed-state check
// instead (design spec §6.4, §8.5).
func (r MaterialRequirement) IsRFQEligible() bool {
	if r.IsClaimed() {
		return false
	}
	return r.readyForRFQBusinessConditions()
}

// ClaimedIsReadyForRFQ is the mark-ready predicate for an ALREADY-CLAIMED
// requirement (design spec §6.4, §8.5).
//
// It shares every business condition with IsRFQEligible but inverts the claim
// term: instead of requiring no claim, it requires the claim to name THIS chain
// and THIS line. IsRFQEligible must not be reused here — its
// ActiveRFQChainID == nil term is false for every requirement already on a
// line, so applying it would reject every non-empty RFQ.
func (r MaterialRequirement) ClaimedIsReadyForRFQ(rfqChainID, lineID string) bool {
	if r.ActiveRFQChainID == nil || *r.ActiveRFQChainID != rfqChainID {
		return false
	}
	if r.ActiveRFQLineID == nil || *r.ActiveRFQLineID != lineID {
		return false
	}
	return r.readyForRFQBusinessConditions()
}

// readyForRFQBusinessConditions holds the §8.5 terms that do NOT concern the
// claim, so IsRFQEligible and ClaimedIsReadyForRFQ cannot drift apart.
func (r MaterialRequirement) readyForRFQBusinessConditions() bool {
	if r.Status != RequirementStatusReviewed {
		return false
	}
	if !r.RequiredQuantity.Value.IsPositive() {
		return false
	}
	if r.UnitMismatch && !r.UnitMismatchAcknowledged {
		return false
	}
	return r.SourceSyncState == SourceSyncStateClean
}

// SplittableErr returns nil when the requirement may be split, or the specific
// domain sentinel explaining why it may not (design spec §8.3).
//
// Terminal status is reported before recursion so a split child that has since
// been archived reports the more fundamental reason, and a claim is reported
// last among the blocking states because releasing it is the contractor's
// actionable next step.
func (r MaterialRequirement) SplittableErr() error {
	if r.IsTerminal() {
		return ErrRequirementTerminal
	}
	if r.SourceType == SourceTypeSplit {
		return ErrRecursiveSplitNotSupported
	}
	if r.IsClaimed() {
		return ErrMaterialRequirementAlreadyClaimed
	}
	if r.SplitState == SplitStateCreating {
		return ErrSplitInProgress
	}
	return nil
}

// RequirementEdit records which fields an edit touches, so review-reset and
// acknowledgement-clearing follow one shared rule rather than being
// re-derived per call site (design spec §2.2, §5.4).
type RequirementEdit struct {
	MaterialIDChanged       bool
	WorkItemIDChanged       bool
	QuantityValueChanged    bool
	QuantityUnitChanged     bool
	SpecificationChanged    bool
	RequiredByDateChanged   bool
	ProcurementNotesChanged bool
	InternalNotesChanged    bool
}

// ResetsReview reports whether the edit moves reviewed back to draft. Every
// supplier/procurement-relevant field does; an InternalNotes-only edit does not,
// because it cannot change what a supplier would be asked to quote (design spec
// §5.4).
func (e RequirementEdit) ResetsReview() bool {
	return e.MaterialIDChanged || e.WorkItemIDChanged ||
		e.QuantityValueChanged || e.QuantityUnitChanged ||
		e.SpecificationChanged || e.RequiredByDateChanged ||
		e.ProcurementNotesChanged
}

// ClearsUnitAcknowledgement reports whether the edit invalidates a prior
// unit-mismatch acknowledgement. An acknowledgement is scoped to the
// material/unit combination in force when it was given, so changing either
// clears it (design spec §2.2).
func (e RequirementEdit) ClearsUnitAcknowledgement() bool {
	return e.MaterialIDChanged || e.QuantityUnitChanged
}

// UnitsMismatch reports whether a procurement unit differs from the Material
// catalog unit, comparing trimmed and Unicode-case-folded (design spec §3.5).
//
// M7 never converts between units and never sums across them: a mismatch is
// flagged for contractor review, never reconciled.
func UnitsMismatch(procurementUnit, catalogUnit string) bool {
	return !strings.EqualFold(strings.TrimSpace(procurementUnit), strings.TrimSpace(catalogUnit))
}
