package supplieroffers

import (
	"context"
	"errors"
	"testing"
)

// ResetDraftLineResponse clears a quoted response back to unanswered while
// preserving the issued line and its lineage (M8.1, checkpoint 4). Reset is
// NOT deletion: the draft line row, its RFQLineID and ordering are untouched.
func TestResetDraftLineResponseClearsQuotedResponseToUnanswered(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	quoted, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID, UnitPriceMinor: 2_500, Brand: "Acme",
	})
	if err != nil {
		t.Fatalf("QuoteDraftLine: %v", err)
	}

	reset, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: quoted.Revision,
		DraftLineID: rig.draft.Lines[0].ID,
	})
	if err != nil {
		t.Fatalf("ResetDraftLineResponse: %v", err)
	}

	line := reset.Lines[0]
	if line.ID != rig.draft.Lines[0].ID {
		t.Fatalf("line ID changed: got %s, want the preserved %s", line.ID, rig.draft.Lines[0].ID)
	}
	if line.RFQLineID != rig.draft.Lines[0].RFQLineID {
		t.Errorf("RFQLineID = %q, want the preserved issued-line lineage %q",
			line.RFQLineID, rig.draft.Lines[0].RFQLineID)
	}
	if line.ResponseStatus != OfferLineUnanswered {
		t.Errorf("ResponseStatus = %q, want unanswered", line.ResponseStatus)
	}
	if line.QuotedQuantity != nil || line.UnitPriceExcludingTax != nil ||
		line.LineSubtotalExcludingTax != nil {
		t.Error("quoted values were not cleared by reset")
	}
	if line.Brand != "" {
		t.Error("supplier response data (brand) was not cleared by reset")
	}
	if reset.Revision != quoted.Revision+1 {
		t.Errorf("Revision = %d, want one increment", reset.Revision)
	}
}

// Resetting a declined line clears its decline data and confirmation gate.
func TestResetDraftLineResponseClearsDeclinedResponseToUnanswered(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	declined, err := rig.service.DeclineDraftLine(ctx, DeclineDraftLineCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID, ResponseStatus: OfferLineNoBid,
		SupplierLineNotes: "not stocked",
	})
	if err != nil {
		t.Fatalf("DeclineDraftLine: %v", err)
	}

	reset, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: declined.Revision,
		DraftLineID: rig.draft.Lines[0].ID,
	})
	if err != nil {
		t.Fatalf("ResetDraftLineResponse: %v", err)
	}

	line := reset.Lines[0]
	if line.ResponseStatus != OfferLineUnanswered {
		t.Errorf("ResponseStatus = %q, want unanswered", line.ResponseStatus)
	}
	if line.ConfirmationRequired {
		t.Error("ConfirmationRequired must clear along with the decline")
	}
	if line.SupplierLineNotes != "" {
		t.Error("decline notes were not cleared by reset")
	}
}

// Resetting an already-unanswered line is idempotent: it converges without
// error and without changing the line's shape.
func TestResetDraftLineResponseIsIdempotentOnUnansweredLine(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	reset, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID,
	})
	if err != nil {
		t.Fatalf("ResetDraftLineResponse on an already-unanswered line: %v", err)
	}
	if reset.Lines[0].ResponseStatus != OfferLineUnanswered {
		t.Errorf("ResponseStatus = %q, want unanswered", reset.Lines[0].ResponseStatus)
	}
}

// Reset never deletes the issued RFQ line row itself.
func TestResetDraftLineResponsePreservesLineCount(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	before := len(rig.draft.Lines)

	reset, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID,
	})
	if err != nil {
		t.Fatalf("ResetDraftLineResponse: %v", err)
	}
	if len(reset.Lines) != before {
		t.Fatalf("line count = %d, want unchanged %d — reset must not delete the issued line",
			len(reset.Lines), before)
	}
}

// Resetting an unknown draft line ID is the repository-precedent not-found,
// not a silent no-op.
func TestResetDraftLineResponseUnknownLineIsNotFound(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	_, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: "line-does-not-exist",
	})
	if !errors.Is(err, ErrOfferDraftNotFound) {
		t.Fatalf("unknown-line reset error = %v, want not found", err)
	}
}
