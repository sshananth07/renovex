package supplieroffers

import (
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// A claimed draft is a durable barrier, not an editable draft. If it allowed
// mutation the previous recipient could keep editing while replacement is in
// flight, which is exactly the exposure the barrier exists to close.
func TestRecipientReplacementClaimedDraftNeverAllowsMutation(t *testing.T) {
	if DraftRecipientReplacementClaimed.AllowsMutation() {
		t.Fatal("a recipient_replacement_claimed draft must never allow mutation")
	}
}

// The claimed state occupies the unfinished-draft uniqueness slot alongside
// active and submitting. Archived does not: releasing the slot is what makes
// the replaced recipient able to create a second draft.
func TestOccupiesUnfinishedDraftSlot(t *testing.T) {
	tests := []struct {
		status DraftStatus
		want   bool
	}{
		{status: DraftActive, want: true},
		{status: DraftSubmitting, want: true},
		{status: DraftRecipientReplacementClaimed, want: true},
		{status: DraftArchived, want: false},
		{status: DraftStatus("unknown"), want: false},
	}

	for _, tt := range tests {
		if got := tt.status.OccupiesUnfinishedDraftSlot(); got != tt.want {
			t.Errorf("%q OccupiesUnfinishedDraftSlot = %v, want %v",
				tt.status, got, tt.want)
		}
	}
}

// A barrier-only row exists solely to hold the slot. It must never fabricate
// commercial content, because ordinary draft logic would otherwise be able to
// mistake coordination state for a Supplier's actual offer.
func TestBarrierPurposeRejectsFabricatedCommercialContent(t *testing.T) {
	claimedAt := time.Now().UTC()
	valid := SupplierOfferDraft{
		ID:                         "draft-1",
		CompanyID:                  "company-1",
		OfferChainID:               "chain-1",
		InvitationID:               "invitation-1",
		IssuedRFQVersionID:         "issued-1",
		RecipientIdentity:          "previous@example.com",
		Status:                     DraftRecipientReplacementClaimed,
		DraftPurpose:               DraftPurposeRecipientReplacementBarrier,
		ReplacementOperationID:     "op-1",
		PreviousRecipientIdentity:  "previous@example.com",
		CandidateRecipientIdentity: "next@example.com",
		ClaimedAt:                  &claimedAt,
		Revision:                   1,
	}

	if err := valid.ValidateBarrierInvariants(); err != nil {
		t.Fatalf("a well-formed barrier must be valid, got %v", err)
	}

	withLines := valid
	withLines.Lines = []SupplierOfferDraftLine{{ID: "line-1", RFQLineID: "rfq-line-1"}}
	if err := withLines.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier carrying offer lines must be rejected")
	}

	withDelivery := valid
	withDelivery.DeliveryCharge = &DeliveryCharge{Amount: money.New(15_000, "MYR")}
	if err := withDelivery.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier carrying a delivery charge must be rejected")
	}

	withNotes := valid
	withNotes.SupplierNotes = "we can do better"
	if err := withNotes.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier carrying supplier notes must be rejected")
	}

	validUntil := claimedAt.Add(24 * time.Hour)
	withValidity := valid
	withValidity.OfferValidUntil = &validUntil
	if err := withValidity.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier carrying an offer validity date must be rejected")
	}

	withCharges := valid
	withCharges.ChargeGroups = []SupplierChargeGroupDraft{{}}
	if err := withCharges.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier carrying conditional charge groups must be rejected")
	}
}

// The discriminator and the status must agree in both directions, so no code
// path can create a claimed row that ordinary listings treat as commercial.
func TestBarrierPurposeAndClaimedStatusRequireEachOther(t *testing.T) {
	claimedAt := time.Now().UTC()
	base := SupplierOfferDraft{
		ID:                         "draft-1",
		CompanyID:                  "company-1",
		OfferChainID:               "chain-1",
		InvitationID:               "invitation-1",
		IssuedRFQVersionID:         "issued-1",
		RecipientIdentity:          "previous@example.com",
		ReplacementOperationID:     "op-1",
		PreviousRecipientIdentity:  "previous@example.com",
		CandidateRecipientIdentity: "next@example.com",
		ClaimedAt:                  &claimedAt,
	}

	barrierWhileActive := base
	barrierWhileActive.DraftPurpose = DraftPurposeRecipientReplacementBarrier
	barrierWhileActive.Status = DraftActive
	if err := barrierWhileActive.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier-purpose row must never be active")
	}

	missingOperation := base
	missingOperation.DraftPurpose = DraftPurposeRecipientReplacementBarrier
	missingOperation.Status = DraftRecipientReplacementClaimed
	missingOperation.ReplacementOperationID = ""
	if err := missingOperation.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier without an owning operation ID must be rejected: " +
			"an unowned claim can never be safely resumed or aborted")
	}

	missingPrevious := base
	missingPrevious.DraftPurpose = DraftPurposeRecipientReplacementBarrier
	missingPrevious.Status = DraftRecipientReplacementClaimed
	missingPrevious.PreviousRecipientIdentity = ""
	if err := missingPrevious.ValidateBarrierInvariants(); err == nil {
		t.Error("a barrier without the previous recipient identity must be rejected")
	}
}
