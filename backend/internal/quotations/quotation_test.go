package quotations_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

func TestQuotationStatusIsValid(t *testing.T) {
	valid := []quotations.QuotationStatus{quotations.QuotationStatusDraft, quotations.QuotationStatusFinalized}
	for _, s := range valid {
		if !s.IsValid() {
			t.Fatalf("expected %q to be valid", s)
		}
	}
	if quotations.QuotationStatus("sent").IsValid() {
		t.Fatal("expected an M6 status like 'sent' to be invalid in M5")
	}
	if quotations.QuotationStatus("").IsValid() {
		t.Fatal("expected empty string to be invalid")
	}
}

func TestTaxModeIsValid(t *testing.T) {
	valid := []quotations.TaxMode{quotations.TaxModeNone, quotations.TaxModePercentage}
	for _, m := range valid {
		if !m.IsValid() {
			t.Fatalf("expected %q to be valid", m)
		}
	}
	if quotations.TaxMode("fixed").IsValid() {
		t.Fatal("expected an undefined tax mode to be invalid")
	}
}
