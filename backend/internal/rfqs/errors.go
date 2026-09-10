package rfqs

import "errors"

// Domain sentinels (design spec §19). Every one is matched with errors.Is at
// the handler boundary and mapped to an explicit HTTP status (design spec
// §13.5).

// Lookup errors.
var (
	ErrRFQNotFound     = errors.New("rfqs: rfq not found")
	ErrRFQLineNotFound = errors.New("rfqs: rfq line not found")
	ErrProjectNotFound = errors.New("rfqs: project not found")
	// ErrMaterialRequirementNotFound is rfqs' OWN sentinel, raised when the
	// requirement capability reports the requirement missing or belonging to
	// another Project. rfqs never imports the materialrequirements sentinel
	// (ADR 0002, design spec §7.2).
	ErrMaterialRequirementNotFound = errors.New("rfqs: material requirement not found")
)

// State errors.
var (
	// ErrRFQNotDraft covers every mutation of a ready RFQ: header edits, line
	// changes and deletion are all draft-only (design spec §6.4).
	ErrRFQNotDraft = errors.New("rfqs: rfq is not in draft status")
	ErrRFQNotReady = errors.New("rfqs: rfq is not in ready status")
	// ErrRFQNotEmpty guards deletion: an RFQ with lines is not deletable.
	ErrRFQNotEmpty = errors.New("rfqs: rfq still has lines")

	// ErrRFQHasOutstandingClaims is the §7.7 third condition. An orphaned claim
	// is a claim with NO line, so an RFQ can be line-empty while still holding
	// a requirement hostage; deleting it would strand that requirement
	// permanently with no chain left to reconcile through.
	ErrRFQHasOutstandingClaims = errors.New("rfqs: rfq still holds outstanding requirement claims")

	// ErrRFQAlreadyIssued is returned when M8 has issued this chain. Reopen is
	// refused and the RFQ REMAINS ready (design spec §6.4).
	ErrRFQAlreadyIssued = errors.New("rfqs: rfq chain has already been issued")
)

// Mark-ready validation errors (design spec §6.4).
var (
	ErrRFQNoLines                 = errors.New("rfqs: an rfq needs at least one line to be marked ready")
	ErrRFQDeliveryAddressRequired = errors.New("rfqs: deliveryAddress is required to mark an rfq ready")
	// ErrRFQLineNotEligible reports that a line's claimed requirement failed
	// ClaimedRequirementIsReadyForRFQ, or that its snapshot quantity is not a
	// positive decimal.
	ErrRFQLineNotEligible = errors.New("rfqs: an rfq line's requirement is not eligible")
	// ErrRFQDatesOutOfOrder reports requiredByDate at or before
	// responseDeadline. Checked at mark-ready (not just at M8 issuance) so an
	// invalid combination cannot reach ready in the first place — the same
	// procurementlimits.ValidateRequiredByDate rule M8 enforces at issuance.
	ErrRFQDatesOutOfOrder = errors.New("rfqs: requiredByDate must be after responseDeadline")
)

// ErrMaterialRequirementAlreadyClaimed is rfqs' own sentinel for a requirement
// held by ANOTHER chain (design spec §7.2, §7.3).
var ErrMaterialRequirementAlreadyClaimed = errors.New("rfqs: material requirement is already claimed by another rfq chain")

// ErrIssuanceStatusUnavailable is returned when the issuance seam errors.
// Reopen FAILS CLOSED: the RFQ remains ready rather than being reopened on an
// unverified assumption (design spec §6.4). Maps to 503.
var ErrIssuanceStatusUnavailable = errors.New("rfqs: issuance status is unavailable")

// Reconciliation errors (design spec §7.5).
var (
	ErrReconciliationActionInvalid = errors.New("rfqs: reconciliation action must be retry_line or release")
	// ErrNoClaimToReconcile is returned when the named requirement holds no
	// claim on this chain, so there is nothing to retry or release.
	ErrNoClaimToReconcile = errors.New("rfqs: requirement holds no claim on this rfq chain")
)

// ErrRevisionMismatch is returned when a conditional update matches no document
// because the stored Revision moved, or the document is no longer in the
// required state (design spec §10).
var ErrRevisionMismatch = errors.New("rfqs: rfq changed since it was read")

// ErrRFQNumberConflict indicates counter corruption or manual data tampering,
// NOT a losable race: NextRFQNumber uses FindOneAndUpdate + $inc and is atomic
// by construction, so two callers can never receive the same number. It is
// deliberately never retried — retrying would be precisely wrong — and
// surfaces as 500 (design spec §13.5).
var ErrRFQNumberConflict = errors.New("rfqs: rfq number conflict")
