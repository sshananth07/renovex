// Package rfqs owns the supplier-neutral Request For Quotation: the RFQ
// aggregate, its lines, the one-active-chain claim ORCHESTRATION, and the three
// read-only capabilities M8 consumes.
//
// Module boundary (ADR 0002, design spec §1.1). The dependency is one-way:
// rfqs -> materialrequirements. This package reaches a requirement ONLY through
// the narrow MaterialRequirementSource capability, importing no
// materialrequirements type. materialrequirements never imports rfqs, never
// inspects an RFQ line, and never implements retry_line — claim orchestration
// (sequencing, compensation, the retry matrix, reconciliation) lives here,
// because only this package can read its own lines.
//
// M7 is supplier-NEUTRAL: it never contacts a supplier. Invitations, secure
// links, supplier portals, submitted offers, offer comparison and RFQ issuing
// are all M8. There is no issued status and no version field, so no M7 code
// path can create one (design spec §6.4, §9.1).
package rfqs

import (
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// RFQStatus is the M7 RFQ lifecycle. There are exactly two states: the
// issued/versioned lifecycle is M8's (design spec §6.4).
type RFQStatus string

const (
	RFQStatusDraft RFQStatus = "draft"
	RFQStatusReady RFQStatus = "ready"
)

// IsValid reports whether s is one of the two defined statuses.
func (s RFQStatus) IsValid() bool {
	switch s {
	case RFQStatusDraft, RFQStatusReady:
		return true
	default:
		return false
	}
}

// RFQLine is the supplier-facing snapshot of one claimed Material Requirement.
//
// No price, no cost, no margin, no InternalNotes — an RFQ line ASKS a supplier
// to quote; it never carries what the contractor thinks it costs. This is the
// same allowlist discipline as quotations.QuotationLine (design spec §6.2).
//
// There is deliberately no SnapshotFingerprint: §6.3 removes the line-drift
// subsystem, because §2.3 freezes every contractor field while a claim exists,
// so the snapshotted fields cannot change while the line and claim validly
// coexist. The claim IS the stability guarantee.
type RFQLine struct {
	// ID is the requirement's pre-generated ActiveRFQLineID, which is what
	// makes an uncertain add-line retry deterministic (design spec §7.3).
	ID string

	SourceMaterialRequirementID string // traceability

	MaterialID       string
	MaterialName     string
	Specification    string
	Quantity         quantity.Quantity
	RequiredByDate   *time.Time
	ProcurementNotes string // supplier-visible

	SnapshotAt time.Time
	SortOrder  int
}

// RFQ is the supplier-neutral request aggregate.
//
// ID is the stable RFQ CHAIN identity: M8's issued versions reference THIS,
// never RFQNumber, so a claim attached to the chain does not move between
// versions (design spec §6.1).
type RFQ struct {
	ID        string // == RFQChainID
	CompanyID string
	ProjectID string
	RFQNumber string // "RFQ-000124" — tenant-scoped display/business identifier
	Status    RFQStatus
	Revision  int64

	Title string
	Lines []RFQLine

	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     *time.Time
	SupplierInstructions string // supplier-visible
	// InternalNotes is contractor-only and NEVER supplier-visible. It is
	// present in the aggregate but structurally absent from every projection
	// M8 reads — the privacy boundary M5 established for Quotation.Notes
	// (design spec §9).
	InternalNotes string

	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ReadyAt         *time.Time
	ReopenedAt      *time.Time
	SchemaVersion   int
}

// ChainID returns the stable cross-module identity of this RFQ chain.
//
// It is deliberately the aggregate ID rather than RFQNumber: the number is a
// tenant-scoped display identifier, while claims and M8's issued versions key
// on the chain (design spec §6.1, §7.1).
func (r RFQ) ChainID() string { return r.ID }

// NextSortOrder returns one past the highest existing SortOrder.
//
// Deliberately not len(Lines): after a removal the count and the highest order
// diverge, and reusing an order would make two lines sort ambiguously.
func (r RFQ) NextSortOrder() int {
	next := 0
	for _, l := range r.Lines {
		if l.SortOrder >= next {
			next = l.SortOrder + 1
		}
	}
	return next
}

// FindLine locates a line by its ID.
func (r RFQ) FindLine(lineID string) (RFQLine, bool) {
	for _, l := range r.Lines {
		if l.ID == lineID {
			return l, true
		}
	}
	return RFQLine{}, false
}

// FindLineByRequirementID locates the line snapshotting one requirement.
//
// A requirement may appear on at most one line of a chain — the claim enforces
// it — so this is what lets an add-line retry distinguish "already appended"
// from "claimed but the line is missing" (design spec §7.3).
func (r RFQ) FindLineByRequirementID(requirementID string) (RFQLine, bool) {
	for _, l := range r.Lines {
		if l.SourceMaterialRequirementID == requirementID {
			return l, true
		}
	}
	return RFQLine{}, false
}

// IsDeletable reports the two conditions visible on the aggregate itself.
//
// The THIRD condition of §7.7 — zero outstanding claims — requires a repository
// read and is enforced by the service. It is not redundant: an orphaned claim
// is precisely a claim with no line, so an RFQ can be line-empty while still
// holding a requirement hostage, and deleting it would strand that requirement
// permanently.
func (r RFQ) IsDeletable() bool {
	return r.Status == RFQStatusDraft && len(r.Lines) == 0
}

// ReadinessErr returns nil when the RFQ satisfies the aggregate-local
// conditions of mark-ready, or the specific sentinel explaining why not
// (design spec §6.4).
//
// The per-line claim validation is NOT here: it needs
// ClaimedRequirementIsReadyForRFQ, a capability call, so the service owns it.
func (r RFQ) ReadinessErr() error {
	if r.Status != RFQStatusDraft {
		return ErrRFQNotDraft
	}
	if len(r.Lines) == 0 {
		return ErrRFQNoLines
	}
	// A supplier cannot quote delivery to nowhere.
	if strings.TrimSpace(r.DeliveryAddress) == "" {
		return ErrRFQDeliveryAddressRequired
	}
	if r.RequiredByDate != nil {
		if r.ResponseDeadline == nil || procurementlimits.ValidateRequiredByDate(*r.ResponseDeadline, r.RequiredByDate) != nil {
			return ErrRFQDatesOutOfOrder
		}
	}
	return nil
}

// LineFromClaimSnapshot builds a line from the snapshot the successful claim
// returned, so the line reflects exactly the state the claim validated
// (design spec §6.2, §7.2 step 4).
//
// The quantity is re-parsed rather than trusted: a malformed or non-positive
// value becomes an error, never a silent zero. A line asking a supplier to
// quote "0" would be worse than a failed request.
func LineFromClaimSnapshot(lineID string, snap ClaimSnapshot, sortOrder int,
	at time.Time) (RFQLine, error) {

	q, err := quantity.New(snap.QuantityValue, snap.QuantityUnit)
	if err != nil {
		return RFQLine{}, ErrRFQLineNotEligible
	}
	if !q.Value.IsPositive() {
		return RFQLine{}, ErrRFQLineNotEligible
	}

	return RFQLine{
		ID:                          lineID,
		SourceMaterialRequirementID: snap.RequirementID,
		MaterialID:                  snap.MaterialID,
		MaterialName:                snap.MaterialName,
		Specification:               snap.Specification,
		Quantity:                    q,
		RequiredByDate:              snap.RequiredByDate,
		ProcurementNotes:            snap.ProcurementNotes,
		SnapshotAt:                  at,
		SortOrder:                   sortOrder,
	}, nil
}
