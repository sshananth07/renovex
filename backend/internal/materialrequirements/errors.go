package materialrequirements

import "errors"

// Domain sentinels (design spec §19). Every one is matched with errors.Is at
// the handler boundary and mapped to an explicit HTTP status (design spec
// §13.5). Sentinels are added here as the checkpoints that raise them land;
// this file holds those the A2 pure predicates need plus the lookup/concurrency
// errors shared across the package.

// Lookup and parent-reference errors.
var (
	ErrMaterialRequirementNotFound = errors.New("materialrequirements: material requirement not found")
	ErrProjectNotFound             = errors.New("materialrequirements: project not found")
	ErrWorkItemNotFound            = errors.New("materialrequirements: work item not found")
	ErrMaterialNotFound            = errors.New("materialrequirements: material not found")
)

// State and eligibility errors.
var (
	// ErrRequirementTerminal is returned for any mutation of a split or
	// archived requirement — both are permanently read-only (design spec §2.1).
	ErrRequirementTerminal = errors.New("materialrequirements: requirement is terminal and read-only")

	// ErrMaterialRequirementAlreadyClaimed is returned when an active RFQ chain
	// holds the requirement. It covers every blocked contractor operation —
	// edit, archive, split, discrepancy resolution — and a losing concurrent
	// claim (design spec §2.3, §7.2).
	ErrMaterialRequirementAlreadyClaimed = errors.New("materialrequirements: requirement is already claimed by an active RFQ chain")

	// ErrRequirementNotEligibleForRFQ is returned when the §8.5 predicate fails.
	ErrRequirementNotEligibleForRFQ = errors.New("materialrequirements: requirement is not eligible for an RFQ")

	// ErrUnresolvedUnitMismatch and ErrUnresolvedSourceDiscrepancy name the two
	// §8.5 terms most likely to need a specific contractor-facing message.
	ErrUnresolvedUnitMismatch      = errors.New("materialrequirements: unit mismatch is unresolved")
	ErrUnresolvedSourceDiscrepancy = errors.New("materialrequirements: source discrepancy is unresolved")
)

// Identity-immutability errors (design spec §2.2).
var (
	ErrMaterialIDImmutable = errors.New("materialrequirements: materialId is immutable for this source type")
	ErrWorkItemIDImmutable = errors.New("materialrequirements: workItemId is immutable for this source type")
	// ErrWorkItemRequiredForGeneratedRequirement guards the cost_item invariant:
	// a generated anchor always has a WorkItem (design spec §2.2).
	ErrWorkItemRequiredForGeneratedRequirement = errors.New("materialrequirements: workItemId is required for a cost_item requirement")
)

// Quantity and unit validation.
var (
	ErrInvalidQuantity = errors.New("materialrequirements: quantity must be a positive decimal")
	ErrInvalidUnit     = errors.New("materialrequirements: unit must be non-empty")
)

// ErrWorkItemCancelled is returned when a manual requirement names a cancelled
// WorkItem. Generation reports the same condition as a contextual skip rather
// than an error (design spec §3.7); creating procurement demand against
// cancelled scope by hand is a mistake worth refusing outright.
var ErrWorkItemCancelled = errors.New("materialrequirements: work item is cancelled")

// ErrNoUnitMismatchToAcknowledge is returned when acknowledge-unit is called on
// a requirement whose procurement unit already matches the catalog unit.
// Setting the flag anyway would leave a meaningless acknowledgement that
// silently relaxes the §8.5 eligibility gate after a later unit change.
var ErrNoUnitMismatchToAcknowledge = errors.New("materialrequirements: there is no unit mismatch to acknowledge")

// Split errors (design spec §8.3, §8.4).
var (
	ErrSplitQuantityMismatch = errors.New("materialrequirements: split child quantities must sum exactly to the source quantity")
	ErrSplitTooFewChildren   = errors.New("materialrequirements: a split requires at least two children")
	ErrSplitUnitChanged      = errors.New("materialrequirements: split children must retain the source unit")
	ErrSplitInProgress       = errors.New("materialrequirements: a split is already in progress for this requirement")
	// ErrRecursiveSplitNotSupported enforces §20's deferral of multi-level
	// splitting: a split child may not itself be split.
	ErrRecursiveSplitNotSupported = errors.New("materialrequirements: splitting a split child is not supported")

	// ErrRequirementIDExists reports that a manifest child already exists at its
	// pre-generated id. It is internal control flow for §8.4's idempotent
	// inserts — reconciliation treats it as "already created", never an error —
	// and never reaches HTTP.
	ErrRequirementIDExists = errors.New("materialrequirements: a requirement already exists at that id")

	// ErrNoSplitToReconcile is returned when reconciliation is requested for a
	// requirement that carries no split manifest. There is nothing to complete,
	// and inventing children would fabricate procurement demand.
	ErrNoSplitToReconcile = errors.New("materialrequirements: requirement has no split to reconcile")
)

// Discrepancy-resolution errors (design spec §5.5-5.7).
var (
	ErrMaterialRequirementDiscrepancyChanged = errors.New("materialrequirements: the source changed since the proposal was computed")
	ErrResolutionActionNotAvailable          = errors.New("materialrequirements: that resolution action is not available for this requirement")
	// ErrResolutionOperationConflict is returned when a resolution operation ID
	// was already applied to a DIFFERENT logical operation. Reusing it blindly
	// would silently hand back another discrepancy's child (design spec §5.7).
	ErrResolutionOperationConflict = errors.New("materialrequirements: resolution operation id belongs to a different resolution")

	// ErrResolutionOperationIDRequired guards create_separate's idempotency:
	// without an operation ID a retry would create duplicate procurement demand,
	// which is exactly what the unique partial index exists to prevent
	// (design spec §5.7).
	ErrResolutionOperationIDRequired = errors.New("materialrequirements: resolutionOperationId is required for create_separate")

	// ErrNoSourceDiscrepancy is returned for a requirement that has no CostItem
	// aggregate at all — manual and split requirements are permanently clean and
	// have nothing to resolve (design spec §8.5).
	ErrNoSourceDiscrepancy = errors.New("materialrequirements: requirement has no source discrepancy to resolve")
)

// Repository-classified duplicate-key sentinels (design spec §12.4). The first
// two are internal control flow and never reach HTTP: generation reloads the
// winning anchor, and resolution reuses the verified existing child.
var (
	ErrSourceAggregationKeyExists        = errors.New("materialrequirements: source aggregation key already exists")
	ErrResolutionOperationAlreadyApplied = errors.New("materialrequirements: resolution operation id already applied")
	ErrUnclassifiedDuplicateKey          = errors.New("materialrequirements: unclassified duplicate-key error")
)

// ErrRevisionMismatch is returned when a conditional update matches no document
// because the stored Revision moved, or the document is no longer in the
// required state (design spec §10).
var ErrRevisionMismatch = errors.New("materialrequirements: requirement changed since it was read")
