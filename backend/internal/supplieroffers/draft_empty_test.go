package supplieroffers

import (
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func emptyDraftWithLines(statuses ...OfferLineResponseStatus) SupplierOfferDraft {
	lines := make([]SupplierOfferDraftLine, 0, len(statuses))
	for i, status := range statuses {
		lines = append(lines, SupplierOfferDraftLine{
			ID: "line", RFQLineID: "rfq-line", ResponseStatus: status,
		})
		_ = i
	}
	return SupplierOfferDraft{Lines: lines, Tax: SupplierOfferTax{Mode: TaxModeNotApplicable}}
}

// A draft containing only system-created unanswered placeholders is
// commercially empty and eligible for copy-forward.
func TestIsCommerciallyEmptyTrueForUnansweredPlaceholdersOnly(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered, OfferLineUnanswered)
	if !draft.IsCommerciallyEmpty() {
		t.Error("draft with only unanswered lines must be commercially empty")
	}
}

func TestIsCommerciallyEmptyFalseForAnyQuotedLine(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered, OfferLineQuoted)
	if draft.IsCommerciallyEmpty() {
		t.Error("a quoted line must make the draft non-empty")
	}
}

func TestIsCommerciallyEmptyFalseForAnyDeclinedLine(t *testing.T) {
	for _, status := range []OfferLineResponseStatus{OfferLineNoBid, OfferLineUnavailable} {
		draft := emptyDraftWithLines(OfferLineUnanswered, status)
		if draft.IsCommerciallyEmpty() {
			t.Errorf("a %s line must make the draft non-empty", status)
		}
	}
}

func TestIsCommerciallyEmptyFalseForOfferLevelTax(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered)
	draft.Tax = SupplierOfferTax{
		Mode: TaxModeOfferLevel,
		OfferLevel: &QuotedOfferTax{
			TaxType: TaxTypeServiceTax, TaxAmount: money.New(100, Phase1Currency),
		},
	}
	if draft.IsCommerciallyEmpty() {
		t.Error("offer-level tax must make the draft non-empty")
	}
}

func TestIsCommerciallyEmptyFalseForChargeGroup(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered)
	draft.ChargeGroups = []SupplierChargeGroupDraft{{
		ConditionalChargeGroup: ConditionalChargeGroup{ID: "group-1", Name: "Bulk"},
	}}
	if draft.IsCommerciallyEmpty() {
		t.Error("a charge group must make the draft non-empty")
	}
}

func TestIsCommerciallyEmptyFalseForDeliveryCharge(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered)
	charge := DeliveryCharge{Amount: money.New(500, Phase1Currency)}
	draft.DeliveryCharge = &charge
	if draft.IsCommerciallyEmpty() {
		t.Error("a delivery charge must make the draft non-empty")
	}
}

func TestIsCommerciallyEmptyFalseForValidityDate(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered)
	validUntil := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	draft.OfferValidUntil = &validUntil
	if draft.IsCommerciallyEmpty() {
		t.Error("a validity date must make the draft non-empty")
	}
}

func TestIsCommerciallyEmptyFalseForSupplierNotes(t *testing.T) {
	draft := emptyDraftWithLines(OfferLineUnanswered)
	draft.SupplierNotes = "please note our lead time"
	if draft.IsCommerciallyEmpty() {
		t.Error("supplier notes must make the draft non-empty")
	}
}
