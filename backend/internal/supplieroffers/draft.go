package supplieroffers

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

const SupplierOfferDraftSchemaVersion = 1

// DraftStatus distinguishes editable drafts from frozen submission claims and
// historical drafts. A submitting draft never times out back to active.
type DraftStatus string

const (
	DraftActive     DraftStatus = "active"
	DraftSubmitting DraftStatus = "submitting"
	DraftArchived   DraftStatus = "archived"

	// DraftRecipientReplacementClaimed is the durable recipient-replacement
	// barrier (spec §5.3A). It keeps occupying the unfinished-draft uniqueness
	// slot until the authoritative invitation replacement is confirmed, so the
	// previous recipient cannot edit, submit or create a second draft during
	// the cross-collection window — and a crash mid-replacement leaves the slot
	// held rather than released.
	DraftRecipientReplacementClaimed DraftStatus = "recipient_replacement_claimed"
)

// AllowsMutation centralizes the fail-closed state check used by every generic
// edit, acknowledgement, removal and recipient-replacement archive operation.
func (status DraftStatus) AllowsMutation() bool {
	return status == DraftActive
}

// OccupiesUnfinishedDraftSlot names the states covered by the partial unique
// index that permits only one unfinished draft per Offer Chain. Archived rows
// release the slot; every other unfinished state must hold it, which is what
// makes draft creation and recipient replacement contend on one index rather
// than through a check-then-insert race.
func (status DraftStatus) OccupiesUnfinishedDraftSlot() bool {
	return status == DraftActive ||
		status == DraftSubmitting ||
		status == DraftRecipientReplacementClaimed
}

// DraftPurpose discriminates a Supplier's commercial draft from a barrier-only
// coordination row, so ordinary draft logic and listings can never mistake
// replacement bookkeeping for an actual offer.
type DraftPurpose string

const (
	DraftPurposeCommercial                  DraftPurpose = "commercial"
	DraftPurposeRecipientReplacementBarrier DraftPurpose = "recipient_replacement_barrier"
)

// SupplierOfferDraft is the mutable commercial aggregate. E3 introduces its
// persistence identity and lifecycle fields first; later checkpoints add the
// tested commercial content and submission-claim projections.
type SupplierOfferDraft struct {
	ID                   string
	CompanyID            string
	OfferChainID         string
	InvitationID         string
	IssuedRFQVersionID   string
	RecipientIdentity    string
	SourceOfferVersionID *string
	Currency             string
	Status               DraftStatus

	Lines                        []SupplierOfferDraftLine
	Tax                          SupplierOfferTax
	OfferTaxReviewRequired       bool
	ChargeGroups                 []SupplierChargeGroupDraft
	DeliveryCharge               *DeliveryCharge
	DeliveryChargeReviewRequired bool
	OfferValidUntil              *time.Time
	SupplierNotes                string

	SubmissionOperationID       string
	SubmissionBaseRevision      int64
	SubmissionFingerprint       string
	SubmissionRecipientIdentity string
	SubmissionInvitationID      string
	SubmissionRFQVersionID      string
	SubmissionAccessGeneration  int64
	CandidateOfferVersionID     string
	CandidateVersionNumber      int
	SubmissionClaimedAt         *time.Time

	// Recipient-replacement claim state (spec §5.3A). These are populated while
	// the durable barrier is held and preserved as coordination history after
	// archival, so a same-operation retry can verify exactly which replacement
	// owns the claim before completing or aborting it.
	DraftPurpose               DraftPurpose
	ReplacementOperationID     string
	PreviousRecipientIdentity  string
	CandidateRecipientIdentity string
	ClaimedAt                  *time.Time

	ArchivedReason               DraftArchivedReason
	ArchivedByOperationID        string
	ReplacementRecipientIdentity string

	Revision      int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ArchivedAt    *time.Time
	SchemaVersion int
}

// DraftArchivedReason records why a draft left the unfinished slot. Recipient
// replacement is distinguished from ordinary submission archival so history
// shows the previous recipient lost the draft to a replacement, not a submit.
type DraftArchivedReason string

const (
	DraftArchivedReasonRecipientReplacement DraftArchivedReason = "recipient_replacement"
)

// IsRecipientReplacementBarrier reports whether this row is barrier-only
// coordination state rather than a Supplier's commercial draft.
func (draft SupplierOfferDraft) IsRecipientReplacementBarrier() bool {
	return draft.DraftPurpose == DraftPurposeRecipientReplacementBarrier
}

// IsCommerciallyEmpty reports whether this draft carries no Supplier-entered
// commercial response yet (M8.1 amendment, §7.1). System-created unanswered
// RFQ line placeholders do not count as content: a freshly created draft is
// empty even though every issued line already has a placeholder row.
//
// Copy-forward is eligible only for an empty draft; any commercial content
// here — a quoted or declined line, offer-level tax, a charge group, a
// delivery charge, a validity date or Supplier notes — blocks it so copying
// never merges into or silently overwrites what the Supplier already entered.
func (draft SupplierOfferDraft) IsCommerciallyEmpty() bool {
	for _, line := range draft.Lines {
		if line.ResponseStatus != OfferLineUnanswered {
			return false
		}
	}
	if draft.Tax.Mode != TaxModeNotApplicable && draft.Tax.Mode != "" {
		return false
	}
	return len(draft.ChargeGroups) == 0 &&
		draft.DeliveryCharge == nil &&
		draft.OfferValidUntil == nil &&
		draft.SupplierNotes == ""
}

// ValidateBarrierInvariants fails closed on any barrier row that carries
// commercial content or lacks the identities recovery depends on.
//
// A barrier exists only to hold the uniqueness slot. If it could carry lines or
// prices, ordinary draft logic might surface fabricated commercial content as a
// Supplier's offer. If it could omit its owning operation or previous
// recipient, no retry could safely prove which replacement owns the claim, and
// an unowned claim can never be completed or aborted.
func (draft SupplierOfferDraft) ValidateBarrierInvariants() error {
	if !draft.IsRecipientReplacementBarrier() {
		return nil
	}
	if draft.Status != DraftRecipientReplacementClaimed {
		return ErrInvalidReplacementBarrier
	}
	if draft.ReplacementOperationID == "" ||
		draft.PreviousRecipientIdentity == "" ||
		draft.CandidateRecipientIdentity == "" ||
		draft.InvitationID == "" ||
		draft.IssuedRFQVersionID == "" ||
		draft.ClaimedAt == nil {
		return ErrInvalidReplacementBarrier
	}
	if len(draft.Lines) > 0 ||
		len(draft.ChargeGroups) > 0 ||
		draft.DeliveryCharge != nil ||
		draft.OfferValidUntil != nil ||
		draft.SupplierNotes != "" ||
		draft.Tax.Mode != "" {
		return ErrInvalidReplacementBarrier
	}
	return nil
}

// SupplierOfferDraftLine keeps copied review state beside the exact commercial
// response it protects. Generic draft replacement cannot safely reconstruct
// these server-owned flags from client input.
type SupplierOfferDraftLine struct {
	ID                       string
	RFQLineID                string
	ResponseStatus           OfferLineResponseStatus
	QuotedQuantity           *quantity.Quantity
	UnitPriceExcludingTax    *money.Money
	LineSubtotalExcludingTax *money.Money
	Brand                    string
	SKU                      string
	ProductDescription       string
	LeadTime                 string
	SupplierLineNotes        string
	CommercialExceptions     string
	LineTax                  *QuotedLineTax
	ReviewRequired           bool
	ConfirmationRequired     bool
	CopiedFromOfferVersionID string
	CopiedFromOfferLineID    string
	CopiedAt                 *time.Time
}

// SupplierChargeGroupDraft adds copy provenance and a server-owned review gate
// without changing the immutable commercial rule shared with award calculation.
type SupplierChargeGroupDraft struct {
	ConditionalChargeGroup  `bson:",inline"`
	CopiedFromChargeGroupID string `bson:"copiedFromChargeGroupId,omitempty"`
	ReviewRequired          bool   `bson:"reviewRequired"`
}

// SupplierOfferDraftCommercialState is an internal trusted persistence shape.
// HTTP inputs use narrower command types so clients cannot directly supply the
// server-owned review and confirmation flags represented here.
type SupplierOfferDraftCommercialState struct {
	Lines                        []SupplierOfferDraftLine
	Tax                          SupplierOfferTax
	OfferTaxReviewRequired       bool
	ChargeGroups                 []SupplierChargeGroupDraft
	DeliveryCharge               *DeliveryCharge
	DeliveryChargeReviewRequired bool
	OfferValidUntil              *time.Time
	SupplierNotes                string
	// SourceOfferVersionID is the server-resolved provenance a completed
	// copy-forward pins on the draft (M8.1 amendment). Ordinary edits carry it
	// forward unchanged; only copy-forward sets it, and only once.
	SourceOfferVersionID *string
}
