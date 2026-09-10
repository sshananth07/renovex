package rfqissuance_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func TestNewIssuedVersionEnforcesPhaseGLimits(t *testing.T) {
	issuedAt := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	deadline := issuedAt.Add(30 * 24 * time.Hour)
	base := rfqissuance.NewIssuedVersionInput{
		CompanyID: "company-1", ProjectID: "project-1", RFQChainID: "chain-1",
		RFQNumber: "RFQ-1", VersionNumber: 1, Currency: "MYR", Title: "Materials",
		ResponseDeadline: &deadline, IssuanceOperationID: "operation-1",
		IssuedByUserID: "user-1", IssuedAt: issuedAt,
	}

	tooManyLines := base
	tooManyLines.Lines = make([]rfqissuance.IssuedRFQLine, 101)
	if _, err := rfqissuance.NewIssuedVersion(tooManyLines); !errors.Is(err, rfqissuance.ErrInputLimitExceeded) {
		t.Errorf("101 lines error = %v, want ErrInputLimitExceeded", err)
	}

	longTitle := base
	longTitle.Title = strings.Repeat("界", 201)
	if _, err := rfqissuance.NewIssuedVersion(longTitle); !errors.Is(err, rfqissuance.ErrInputLimitExceeded) {
		t.Errorf("201-rune title error = %v, want ErrInputLimitExceeded", err)
	}

	late := base
	lateDeadline := issuedAt.Add(180*24*time.Hour + time.Nanosecond)
	late.ResponseDeadline = &lateDeadline
	if _, err := rfqissuance.NewIssuedVersion(late); !errors.Is(err, rfqissuance.ErrInvalidBusinessDate) {
		t.Errorf("late response deadline error = %v, want ErrInvalidBusinessDate", err)
	}
}

// Issued-line identity and lineage (design spec §3.2A).
//
// Four identifiers with distinct meanings. Getting these wrong would either
// conflate V1's and V2's records for one logical line, or weaken copy-forward
// to Material-ID matching, which approved decision 11 forbids.

func readyLine(requirementID, materialID string) rfqissuance.ReadyRFQLineSnapshot {
	return rfqissuance.ReadyRFQLineSnapshot{
		SourceRFQLineID:             "m7-line-1",
		SourceMaterialRequirementID: requirementID,
		MaterialID:                  materialID,
		MaterialName:                "Portland Cement",
		Specification:               "OPC 50kg",
		QuantityValue:               "100",
		QuantityUnit:                "bag",
		ProcurementNotes:            "deliver to site gate",
		SortOrder:                   0,
	}
}

// Version 1 mints BOTH a per-version ID and a stable lineage ID, and never
// reuses the M7 line ID as either.
func TestNewIssuedLineFromM7MintsPerVersionAndLineageIDs(t *testing.T) {
	line, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if line.ID == "" {
		t.Error("ID must be generated: it identifies this line inside THIS version")
	}
	if line.LineageID == "" {
		t.Error("LineageID must be generated: it is the stable identity across versions")
	}
	if line.ID == line.LineageID {
		t.Error("ID and LineageID must be distinct identifiers")
	}
	if line.ID == "m7-line-1" || line.LineageID == "m7-line-1" {
		t.Error("the M7 line ID must never be reused as the issued line's own identity; " +
			"reusing it would conflate V1 and V2 records for one logical line (§3.2A)")
	}

	if line.SourceM7RFQLineID == nil || *line.SourceM7RFQLineID != "m7-line-1" {
		t.Errorf("SourceM7RFQLineID = %v, want m7-line-1", line.SourceM7RFQLineID)
	}
	if line.SourceMaterialRequirementID == nil || *line.SourceMaterialRequirementID != "mr-1" {
		t.Errorf("SourceMaterialRequirementID = %v, want mr-1", line.SourceMaterialRequirementID)
	}
	if line.MaterialID != "material-1" {
		t.Errorf("MaterialID = %q, want material-1", line.MaterialID)
	}
}

// Two lines from the same M7 snapshot must not share identities.
func TestNewIssuedLineFromM7GeneratesUniqueIdentitiesPerLine(t *testing.T) {
	first, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-2", "material-2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.ID == second.ID {
		t.Error("two issued lines share an ID")
	}
	if first.LineageID == second.LineageID {
		t.Error("two issued lines share a LineageID")
	}
}

// The quantity must survive as an exact decimal, never a float (ADR 0001).
func TestNewIssuedLineFromM7PreservesTheExactQuantity(t *testing.T) {
	snapshot := readyLine("mr-1", "material-1")
	snapshot.QuantityValue = "12.345"
	snapshot.QuantityUnit = "m3"

	line, err := rfqissuance.NewIssuedLineFromM7(snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := line.Quantity.Value.String(); got != "12.345" {
		t.Errorf("Quantity = %q, want the exact decimal 12.345", got)
	}
	if line.Quantity.Unit != "m3" {
		t.Errorf("Unit = %q, want m3", line.Quantity.Unit)
	}
}

func TestNewIssuedLineFromM7RejectsAMalformedQuantity(t *testing.T) {
	snapshot := readyLine("mr-1", "material-1")
	snapshot.QuantityValue = "not-a-number"

	if _, err := rfqissuance.NewIssuedLineFromM7(snapshot); err == nil {
		t.Fatal("expected a malformed quantity to be rejected")
	}
}

// A cloned amendment line keeps its lineage and provenance but takes a NEW
// per-version ID (§3.2A).
func TestCloneIssuedLineKeepsLineageAndTakesANewID(t *testing.T) {
	original, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clone := rfqissuance.CloneIssuedLine(original)

	if clone.ID == original.ID {
		t.Error("a cloned line must take a NEW per-version ID: a Supplier Offer references " +
			"the exact line in the exact version it quoted against (§3.2A)")
	}
	if clone.ID == "" {
		t.Error("the clone's ID must be generated")
	}
	if clone.LineageID != original.LineageID {
		t.Errorf("LineageID = %q, want the original %q: lineage is what makes a line "+
			"traceable across versions", clone.LineageID, original.LineageID)
	}
	if clone.SourceM7RFQLineID == nil || original.SourceM7RFQLineID == nil ||
		*clone.SourceM7RFQLineID != *original.SourceM7RFQLineID {
		t.Error("SourceM7RFQLineID must be preserved across the clone")
	}
	if clone.SourceMaterialRequirementID == nil || original.SourceMaterialRequirementID == nil ||
		*clone.SourceMaterialRequirementID != *original.SourceMaterialRequirementID {
		t.Error("SourceMaterialRequirementID must be preserved across the clone")
	}
	if clone.MaterialName != original.MaterialName ||
		clone.Quantity.Value.String() != original.Quantity.Value.String() {
		t.Error("the clone must preserve the line's content")
	}
}

// An M8-native line has no M7 provenance at all — the nil case the Revision 1
// shape could not express (§3.2A).
func TestNewM8NativeLineHasNoM7Provenance(t *testing.T) {
	line, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialID:    "material-9",
		MaterialName:  "Rebar",
		Specification: "T12",
		QuantityValue: "40",
		QuantityUnit:  "length",
		SortOrder:     3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if line.ID == "" || line.LineageID == "" {
		t.Error("an M8-native line still needs both a per-version ID and a lineage ID")
	}
	if line.SourceM7RFQLineID != nil {
		t.Errorf("SourceM7RFQLineID = %v, want nil for an M8-native line", line.SourceM7RFQLineID)
	}
	if line.SourceMaterialRequirementID != nil {
		t.Errorf("SourceMaterialRequirementID = %v, want nil for an M8-native line",
			line.SourceMaterialRequirementID)
	}
	if line.MaterialID != "material-9" {
		t.Errorf("MaterialID = %q, want material-9; it is REQUIRED on an M8-native line",
			line.MaterialID)
	}
}

func TestNewM8NativeLineRequiresAMaterialID(t *testing.T) {
	_, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialName:  "Rebar",
		QuantityValue: "40",
		QuantityUnit:  "length",
	})

	if err == nil {
		t.Fatal("expected a missing MaterialID to be rejected on an M8-native line")
	}
}

func TestNewM8NativeLineRejectsAnInvalidQuantity(t *testing.T) {
	tests := []rfqissuance.M8NativeLineInput{
		{MaterialID: "material-9", QuantityValue: "not-a-number", QuantityUnit: "length"},
		{MaterialID: "material-9", QuantityValue: "0", QuantityUnit: "length"},
		{MaterialID: "material-9", QuantityValue: "40", QuantityUnit: ""},
	}
	for _, input := range tests {
		if _, err := rfqissuance.NewM8NativeLine(input); !errors.Is(
			err, rfqissuance.ErrInvalidQuantity) {
			t.Errorf("input %+v returned %v, want ErrInvalidQuantity", input, err)
		}
	}
}

// --- copy-forward compatibility (§3.2A) ---

// An M7-backed line matches by Material Requirement, which is what approved
// decision 11 requires.
func TestCopyForwardMatchesM7BackedLinesByMaterialRequirement(t *testing.T) {
	previous, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A new version's line for the SAME requirement, with a different per-version
	// ID and even a different lineage, still matches.
	current, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !rfqissuance.LinesAreCompatible(previous, current) {
		t.Error("two lines sharing SourceMaterialRequirementID must be copy-forward compatible")
	}
}

// The precise reason decision 11 says "not Material ID alone": two lines for the
// same Material under DIFFERENT requirements are different commercial lines and
// must never cross-match.
func TestCopyForwardDoesNotCrossMatchTheSameMaterialUnderDifferentRequirements(t *testing.T) {
	previous, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	current, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-2", "material-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rfqissuance.LinesAreCompatible(previous, current) {
		t.Error("lines for the same Material under DIFFERENT Material Requirements must not " +
			"cross-match; matching on Material ID alone is exactly what approved " +
			"decision 11 forbids")
	}
}

// An M8-native line has no requirement to match on, so lineage is the rule.
func TestCopyForwardMatchesM8NativeLinesByLineage(t *testing.T) {
	original, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialID: "material-9", MaterialName: "Rebar",
		QuantityValue: "40", QuantityUnit: "length",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	clone := rfqissuance.CloneIssuedLine(original)

	if !rfqissuance.LinesAreCompatible(original, clone) {
		t.Error("an M8-native line and its clone share a LineageID and must be compatible")
	}
}

func TestCopyForwardDoesNotMatchTwoUnrelatedM8NativeLines(t *testing.T) {
	first, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialID: "material-9", MaterialName: "Rebar",
		QuantityValue: "40", QuantityUnit: "length",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialID: "material-9", MaterialName: "Rebar",
		QuantityValue: "40", QuantityUnit: "length",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rfqissuance.LinesAreCompatible(first, second) {
		t.Error("two independently created M8-native lines have different lineages and " +
			"must not match, even with identical content")
	}
}

// An M7-backed line and an M8-native line must never match: one has a
// requirement, the other does not.
func TestCopyForwardDoesNotMatchAcrossProvenanceKinds(t *testing.T) {
	backed, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-9"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	native, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialID: "material-9", MaterialName: "Rebar",
		QuantityValue: "40", QuantityUnit: "length",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rfqissuance.LinesAreCompatible(backed, native) {
		t.Error("an M7-backed line and an M8-native line must not be compatible")
	}
}

// --- issued version construction ---

func TestNewIssuedVersionRequiresAResponseDeadline(t *testing.T) {
	deadline := time.Now().Add(14 * 24 * time.Hour)

	_, err := rfqissuance.NewIssuedVersion(rfqissuance.NewIssuedVersionInput{
		CompanyID: "company-1", ProjectID: "project-1", RFQChainID: "chain-1",
		RFQNumber: "RFQ-000001", VersionNumber: 1, Currency: "MYR",
		ResponseDeadline: nil,
		IssuedByUserID:   "user-1",
	})

	if err == nil {
		t.Fatal("expected issuance to be refused without a response deadline (§4.1A)")
	}
	if err != rfqissuance.ErrResponseDeadlineRequired {
		t.Errorf("error = %v, want ErrResponseDeadlineRequired; M8 must not invent a "+
			"commercial deadline", err)
	}

	// And it succeeds once a deadline is present.
	if _, err := rfqissuance.NewIssuedVersion(rfqissuance.NewIssuedVersionInput{
		CompanyID: "company-1", ProjectID: "project-1", RFQChainID: "chain-1",
		RFQNumber: "RFQ-000001", VersionNumber: 1, Currency: "MYR",
		ResponseDeadline: &deadline,
		IssuedByUserID:   "user-1",
	}); err != nil {
		t.Errorf("a version with a deadline must be constructible, got %v", err)
	}
}
