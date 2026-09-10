package quotations

import (
	"context"
	"errors"
	"time"
)

// ErrQuotationNotFound is returned when a Quotation lookup finds no match —
// including a Quotation that exists but belongs to a different company.
var ErrQuotationNotFound = errors.New("quotations: quotation not found")

// ErrProjectNotFound is returned when the given projectID does not belong
// to the caller's company.
var ErrProjectNotFound = errors.New("quotations: project not found")

// ErrEstimateNotFound is returned when the given estimateID does not
// belong to the caller's company (design spec §22 — mapped from
// FinalizedEstimateSource.VisitQuotationSeeds's found=false).
var ErrEstimateNotFound = errors.New("quotations: estimate not found")

// ErrEstimateNotFinalized is returned when the referenced Estimate is not
// Status=finalized (design spec §3/§22 — mapped from finalized=false).
var ErrEstimateNotFinalized = errors.New("quotations: source estimate must be finalized")

// ErrEstimateProjectMismatch is returned when the referenced Estimate
// belongs to a different Project than the Quotation being created (design
// spec §3/§22 — mapped from projectMatches=false).
var ErrEstimateProjectMismatch = errors.New("quotations: estimate does not belong to this project")

// ErrIneligibleCostBasisForQuotation is returned when the referenced
// Estimate's cost basis cannot be allocated into a Quotation — a WorkItem
// cost-group weight is non-positive, or the largest-remainder allocation
// would produce a zero share for at least one group (design spec §6.2 —
// mapped from FinalizedEstimateSource.VisitQuotationSeeds's
// allocationEligible=false).
var ErrIneligibleCostBasisForQuotation = errors.New("quotations: estimate's cost basis cannot be allocated into a quotation (a workitem group is non-positive or would receive a zero share)")

// ErrQuotationCurrencyMismatch is returned when a newly-referenced
// Estimate's currency differs from the source Quotation's own currency on
// POST /quotations/{id}/versions (design spec §11 step 3).
var ErrQuotationCurrencyMismatch = errors.New("quotations: referenced estimate's currency does not match this quotation chain")

// ErrLineCurrencyMismatch is returned when a submitted line's Amount or
// UnitPrice currency differs from the Quotation's own Currency (design
// spec §8, Correction B).
var ErrLineCurrencyMismatch = errors.New("quotations: line amount/unitPrice currency does not match this quotation's currency")

// ErrWorkItemNotFound is returned when a sourceWorkItemIds entry does not
// belong to both the caller's company AND this Quotation's Project (design
// spec §13.1/§22).
var ErrWorkItemNotFound = errors.New("quotations: work item not found")

// ErrDuplicateWorkItemIDInLine is returned when the same WorkItem ID
// appears twice within one line's own SourceWorkItemIDs array (design spec
// §13.1).
var ErrDuplicateWorkItemIDInLine = errors.New("quotations: sourceWorkItemIds must not contain the same work item twice")

// ErrUnknownLineID is returned when a submitted line's ID does not match
// any existing line on this Quotation (design spec §13.1).
var ErrUnknownLineID = errors.New("quotations: line id does not belong to this quotation")

// ErrDuplicateLineID is returned when two submitted lines share the same
// ID (design spec §13.1).
var ErrDuplicateLineID = errors.New("quotations: duplicate line id in submitted lines")

// ErrBlankLineDescription is returned when a line's Description is empty
// after trimming whitespace (design spec §13.1).
var ErrBlankLineDescription = errors.New("quotations: line description must not be blank")

// ErrQuotationNotDraft is returned when a draft-only mutation is attempted
// against a finalized Quotation.
var ErrQuotationNotDraft = errors.New("quotations: quotation is not a draft")

// ErrQuotationMustBeFinalizedBeforeNewVersion is returned by CreateNewVersion
// when the source Quotation is not yet Status=finalized (design spec §11
// step 1).
var ErrQuotationMustBeFinalizedBeforeNewVersion = errors.New("quotations: source quotation must be finalized before creating a new version")

// ErrIncompleteLinePricing is returned when Quantity/Unit/UnitPrice are
// partially, not fully, supplied on a line (design spec §7.1).
var ErrIncompleteLinePricing = errors.New("quotations: quantity, unit, and unitPrice must all be supplied together or not at all")

// ErrInvalidLineQuantity is returned when a line's Quantity is <= 0
// (design spec §7.1).
var ErrInvalidLineQuantity = errors.New("quotations: quantity must be strictly positive")

// ErrInvalidLineUnit is returned when a line's Unit is empty after
// trimming whitespace (design spec §7.1, hardened: a whitespace-only Unit
// is not a meaningful unit label).
var ErrInvalidLineUnit = errors.New("quotations: unit must not be blank")

// ErrInvalidLineUnitPrice is returned when a line's UnitPrice is <= 0 —
// strictly positive, not merely non-negative (design spec §7.1, corrected
// 3rd review round: a zero UnitPrice would force Amount == 0, which is
// unconditionally rejected below).
var ErrInvalidLineUnitPrice = errors.New("quotations: unitPrice must be strictly positive")

// ErrLineAmountMismatch is returned when a line's Amount does not equal
// Quantity x UnitPrice (design spec §7.1).
var ErrLineAmountMismatch = errors.New("quotations: line amount does not match quantity x unitPrice")

// ErrLineAmountNotPositive is returned when a line's Amount is <= 0
// (design spec §7.2 — no zero-valued financial line is representable).
var ErrLineAmountNotPositive = errors.New("quotations: line amount must be strictly positive")

// ErrEmptyLines is returned when a PUT /quotations/{id}/lines submission
// contains zero lines (design spec §13.1 step 8).
var ErrEmptyLines = errors.New("quotations: at least one line is required")

// ErrInvalidTaxMode is returned when TaxMode is not "none" or "percentage"
// (design spec §15.1).
var ErrInvalidTaxMode = errors.New("quotations: invalid tax mode")

// ErrInvalidTaxRate is returned when TaxRateBPS does not satisfy the rule
// for the given TaxMode (design spec §15.1: ==0 for none, 0<rate<=10000
// for percentage).
var ErrInvalidTaxRate = errors.New("quotations: invalid tax rate for the given tax mode")

// ErrInvalidTaxLabel is returned when TaxLabel does not satisfy the rule
// for the given TaxMode (design spec §15.1: must be "" for none, non-blank
// for percentage).
var ErrInvalidTaxLabel = errors.New("quotations: invalid tax label for the given tax mode")

// ErrRevisionMismatch is returned by ReplaceLines/ReplaceTerms/ReplaceTax/
// Finalize when the supplied expectedRevision does not match the
// document's current Revision (or the document is no longer a draft) —
// the caller must re-GET and retry with the current Revision (design spec
// §13/§27.4).
var ErrRevisionMismatch = errors.New("quotations: revision mismatch or quotation is no longer a draft")

// ErrVersionConflict is returned by Create when the attempted Version
// number collides with the uq_quotations_company_number_version unique
// index — i.e. another concurrent writer already claimed that exact
// version number. TRANSIENT — the caller should re-read MAX(version) and
// retry with a fresh version number (design spec §27.3 steps 3-4). ALSO
// returned when the bounded retry loop in §27.3 is exhausted — not a
// distinct sentinel (design spec §27.5).
var ErrVersionConflict = errors.New("quotations: version number conflict, retry with a fresh version")

// ErrDraftAlreadyExists is returned by Create when the attempted document
// (status=draft) collides with the uq_quotations_one_draft_per_number
// PARTIAL unique index (design spec §26) — i.e. a draft already exists for
// this QuotationNumber chain. NOT transient — never retried (design spec
// §27.3 step 5).
var ErrDraftAlreadyExists = errors.New("quotations: a draft already exists for this quotation chain")

// ErrUnclassifiedDuplicateKey is returned by Create when MongoDB reports a
// duplicate-key error that does not match either known unique index by
// name (design spec §27.3 step 6). NEVER retried — an error this function
// cannot positively identify must not be assumed safe to retry; it
// surfaces as an internal error (500) instead.
var ErrUnclassifiedDuplicateKey = errors.New("quotations: unclassified duplicate-key error")

// ErrQuotationNumberAllocationFailed is returned ONLY by a genuine
// infrastructure failure of the quotation_counters FindOneAndUpdate/$inc
// call itself (design spec §27.1/§27.5) — never version-retry exhaustion
// (ErrVersionConflict) or an unclassified collision
// (ErrUnclassifiedDuplicateKey).
var ErrQuotationNumberAllocationFailed = errors.New("quotations: failed to allocate a quotation number")

// ErrNoQuotationSeedsGenerated is returned when
// FinalizedEstimateSource.VisitQuotationSeeds reports full eligibility but
// invokes its visitor zero times — defense-in-depth boundary validation on
// the quotations side of the privacy firewall; a finalized, allocation-
// eligible Estimate must always produce at least one seed.
var ErrNoQuotationSeedsGenerated = errors.New("quotations: estimate produced no quotation line seeds")

// ErrQuotationSeedCurrencyMismatch is returned when a seed's allocated
// amount currency does not match the currency VisitQuotationSeeds itself
// returned — defense-in-depth boundary validation; every seed must share
// one currency with the Estimate it was generated from.
var ErrQuotationSeedCurrencyMismatch = errors.New("quotations: estimate seed currency does not match the estimate's currency")

// ErrQuotationSeedAmountNotPositive is returned when a seed's allocated
// amount is <= 0 — defense-in-depth boundary validation; every allocated
// seed must be strictly positive.
var ErrQuotationSeedAmountNotPositive = errors.New("quotations: estimate seed amount must be strictly positive")

// ErrQuotationSeedTotalMismatch is returned when the exact sum of all
// seed amounts does not equal the returned ProposedSellingPrice —
// defense-in-depth boundary validation guarding against a future
// regression in estimates' own allocation guarantee; no partial Quotation
// is ever created when this check fails.
var ErrQuotationSeedTotalMismatch = errors.New("quotations: sum of estimate seed amounts does not equal the estimate's proposed selling price")

// QuotationRepository persists Quotations. quotations owns the quotations
// collection exclusively; no other module may query it directly.
type QuotationRepository interface {
	Create(ctx context.Context, q Quotation) (Quotation, error)
	FindByID(ctx context.Context, companyID, id string) (Quotation, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error)
	// FindMaxVersion returns the highest Version number for
	// {companyId, quotationNumber}, or 0 if none exists yet — used by both
	// CreateQuotation (no-retry, exactly-once Version=1 attempt) and
	// CreateNewVersion (bounded-retry MAX+1 allocation), design spec §27.2/§27.3.
	FindMaxVersion(ctx context.Context, companyID, quotationNumber string) (int, error)

	// ReplaceLines atomically replaces Lines/Subtotal/TaxAmount/Total on a
	// draft, conditioned on {_id, companyId, status: draft,
	// revision: expectedRevision} matching. Returns ErrRevisionMismatch if
	// the condition does not match an existing draft document (design spec
	// §13.1/§27.4). NEVER touches GeneratedSubtotal.
	ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error)

	// ReplaceTerms atomically replaces Terms/PaymentSchedule/Notes/
	// ValidUntil on a draft, same conditional-match semantics as
	// ReplaceLines (design spec §13/§27.4).
	ReplaceTerms(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error)

	// ReplaceTax atomically replaces TaxMode/TaxLabel/TaxRateBPS/TaxAmount/
	// Total on a draft, same conditional-match semantics as ReplaceLines
	// (design spec §15/§27.4).
	ReplaceTax(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error)

	// Finalize atomically transitions a draft to finalized, conditioned on
	// {_id, companyId, status: draft, revision: expectedRevision}
	// matching. Returns ErrRevisionMismatch on a stale expectedRevision
	// against a still-draft document (design spec §12/§27.4).
	Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Quotation, error)
}

// QuotationCounterRepository allocates monotonically increasing
// QuotationNumbers per Company via an atomic FindOneAndUpdate/$inc against
// the quotation_counters collection (design spec §9.3/§27.1). Counter gaps
// are acceptable and are never repaired (design spec §2).
type QuotationCounterRepository interface {
	// NextQuotationNumber atomically increments and returns companyID's
	// next sequence number. A failure here is a genuine infrastructure
	// error, mapped to ErrQuotationNumberAllocationFailed by the caller.
	NextQuotationNumber(ctx context.Context, companyID string) (int64, error)
}
