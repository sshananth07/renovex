package rfqissuance

import (
	"testing"
	"time"
)

func TestParseAmendmentLinePatchesBuildsTheHTTPBoundaryProjection(t *testing.T) {
	requiredBy := "2026-09-15T08:30:00+08:00"
	lines, err := parseAmendmentLinePatches([]amendmentLinePatchDTO{
		{
			ID: "existing-line", MaterialID: "material-1", MaterialName: "Cement",
			Quantity:       issuanceQuantityDTO{Value: "125.5", Unit: "bag"},
			RequiredByDate: requiredBy, SortOrder: 1,
		},
		{
			MaterialID: "material-9", MaterialName: "Rebar",
			Quantity:  issuanceQuantityDTO{Value: "40", Unit: "length"},
			SortOrder: 2,
		},
	})
	if err != nil {
		t.Fatalf("parse line patch: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].ID != "existing-line" ||
		lines[0].QuantityValue != "125.5" ||
		lines[0].QuantityUnit != "bag" {
		t.Errorf("first line = %+v, want the editable HTTP fields", lines[0])
	}
	wantDate, _ := time.Parse(timeLayout, requiredBy)
	if lines[0].RequiredByDate == nil ||
		!lines[0].RequiredByDate.Equal(wantDate) {
		t.Errorf("RequiredByDate = %v, want %v", lines[0].RequiredByDate, wantDate)
	}
	if lines[1].RequiredByDate != nil {
		t.Errorf("omitted line RequiredByDate = %v, want nil", lines[1].RequiredByDate)
	}
}

func TestParseAmendmentLinePatchesRejectsAnInvalidRequiredByDate(t *testing.T) {
	_, err := parseAmendmentLinePatches([]amendmentLinePatchDTO{{
		MaterialID:     "material-9",
		Quantity:       issuanceQuantityDTO{Value: "40", Unit: "length"},
		RequiredByDate: "next Tuesday",
	}})
	if err == nil {
		t.Fatal("expected a non-RFC3339 line requiredByDate to be rejected")
	}
}
