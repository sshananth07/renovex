package materialrequirements_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// eligible returns a requirement satisfying every §8.5 term, so each test can
// negate exactly one condition and attribute the outcome to it.
func eligible() mr.MaterialRequirement {
	workItemID := "w1"
	return mr.MaterialRequirement{
		ID:               "mr1",
		CompanyID:        "co1",
		ProjectID:        "p1",
		WorkItemID:       &workItemID,
		MaterialID:       "m1",
		MaterialName:     "Cement",
		RequiredQuantity: quantity.Quantity{Value: decimal.RequireFromString("100"), Unit: "bag"},
		CatalogUnit:      "bag",
		Status:           mr.RequirementStatusReviewed,
		SourceType:       mr.SourceTypeCostItem,
		SourceSyncState:  mr.SourceSyncStateClean,
		SchemaVersion:    1,
	}
}

// --- Enum validity (design spec §2) ---

func TestRequirementStatusIsValid(t *testing.T) {
	for _, s := range []mr.RequirementStatus{
		mr.RequirementStatusDraft, mr.RequirementStatusReviewed,
		mr.RequirementStatusSplit, mr.RequirementStatusArchived,
	} {
		if !s.IsValid() {
			t.Errorf("%q must be valid", s)
		}
	}
	for _, s := range []mr.RequirementStatus{"", "pending", "ready", "DRAFT", "cancelled"} {
		if s.IsValid() {
			t.Errorf("%q must not be valid", s)
		}
	}
}

func TestSourceTypeIsValid(t *testing.T) {
	for _, s := range []mr.SourceType{mr.SourceTypeCostItem, mr.SourceTypeManual, mr.SourceTypeSplit} {
		if !s.IsValid() {
			t.Errorf("%q must be valid", s)
		}
	}
	for _, s := range []mr.SourceType{"", "ai_suggestion", "COST_ITEM", "import"} {
		if s.IsValid() {
			t.Errorf("%q must not be valid", s)
		}
	}
}

func TestSourceSyncStateIsValid(t *testing.T) {
	for _, s := range []mr.SourceSyncState{
		mr.SourceSyncStateClean, mr.SourceSyncStateChangeDetected, mr.SourceSyncStateSourceRemoved,
	} {
		if !s.IsValid() {
			t.Errorf("%q must be valid", s)
		}
	}
	for _, s := range []mr.SourceSyncState{"", "dirty", "stale", "CLEAN"} {
		if s.IsValid() {
			t.Errorf("%q must not be valid", s)
		}
	}
}

// SplitStateNone is the empty string: a requirement never involved in a split
// carries no split state, so "" must be valid here (unlike the other enums).
func TestSplitStateIsValid(t *testing.T) {
	for _, s := range []mr.SplitState{mr.SplitStateNone, mr.SplitStateCreating, mr.SplitStateCompleted} {
		if !s.IsValid() {
			t.Errorf("%q must be valid", s)
		}
	}
	for _, s := range []mr.SplitState{"pending", "done", "CREATING"} {
		if s.IsValid() {
			t.Errorf("%q must not be valid", s)
		}
	}
}

// --- Terminal status (design spec §2.1) ---

func TestIsTerminal(t *testing.T) {
	cases := map[mr.RequirementStatus]bool{
		mr.RequirementStatusDraft:    false,
		mr.RequirementStatusReviewed: false,
		mr.RequirementStatusSplit:    true,
		mr.RequirementStatusArchived: true,
	}
	for status, wantTerminal := range cases {
		r := eligible()
		r.Status = status
		if got := r.IsTerminal(); got != wantTerminal {
			t.Errorf("status %q: IsTerminal() = %v, want %v", status, got, wantTerminal)
		}
	}
}

// --- §8.5 RFQ eligibility predicate ---

func TestIsRFQEligibleAcceptsFullySatisfiedRequirement(t *testing.T) {
	if r := eligible(); !r.IsRFQEligible() {
		t.Fatal("a requirement satisfying every §8.5 term must be eligible")
	}
}

// Each row negates exactly ONE term of the predicate.
func TestIsRFQEligibleRejectsEachFailedTerm(t *testing.T) {
	chain := "rfq1"
	cases := []struct {
		name   string
		mutate func(*mr.MaterialRequirement)
	}{
		{"status draft", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusDraft }},
		{"status split", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusSplit }},
		{"status archived", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusArchived }},
		{"already claimed", func(r *mr.MaterialRequirement) { r.ActiveRFQChainID = &chain }},
		{"zero quantity", func(r *mr.MaterialRequirement) {
			r.RequiredQuantity.Value = decimal.Zero
		}},
		{"negative quantity", func(r *mr.MaterialRequirement) {
			r.RequiredQuantity.Value = decimal.RequireFromString("-1")
		}},
		{"unresolved unit mismatch", func(r *mr.MaterialRequirement) {
			r.UnitMismatch = true
			r.UnitMismatchAcknowledged = false
		}},
		{"sync change_detected", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateChangeDetected
		}},
		{"sync source_removed", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateSourceRemoved
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := eligible()
			tc.mutate(&r)
			if r.IsRFQEligible() {
				t.Errorf("%s must make the requirement ineligible", tc.name)
			}
		})
	}
}

// An ACKNOWLEDGED unit mismatch does not block eligibility (design spec §3.5).
func TestIsRFQEligibleAllowsAcknowledgedUnitMismatch(t *testing.T) {
	r := eligible()
	r.UnitMismatch = true
	r.UnitMismatchAcknowledged = true
	if !r.IsRFQEligible() {
		t.Fatal("an acknowledged unit mismatch must not block eligibility")
	}
}

// --- Claimed state (design spec §2.3) ---

func TestIsClaimed(t *testing.T) {
	r := eligible()
	if r.IsClaimed() {
		t.Error("a requirement with a nil ActiveRFQChainID is not claimed")
	}
	chain := "rfq1"
	r.ActiveRFQChainID = &chain
	if !r.IsClaimed() {
		t.Error("a requirement with a non-nil ActiveRFQChainID is claimed")
	}
}

// --- Contractor editability (design spec §2.1, §2.3) ---

// Terminal status and an active claim each independently forbid every
// contractor edit, including InternalNotes (§2.3's conservative rule).
func TestContractorEditableRequiresNonTerminalAndUnclaimed(t *testing.T) {
	chain := "rfq1"
	cases := []struct {
		name         string
		mutate       func(*mr.MaterialRequirement)
		wantEditable bool
	}{
		{"draft unclaimed", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusDraft }, true},
		{"reviewed unclaimed", func(r *mr.MaterialRequirement) {}, true},
		{"split", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusSplit }, false},
		{"archived", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusArchived }, false},
		{"claimed", func(r *mr.MaterialRequirement) { r.ActiveRFQChainID = &chain }, false},
		{"claimed and draft", func(r *mr.MaterialRequirement) {
			r.Status = mr.RequirementStatusDraft
			r.ActiveRFQChainID = &chain
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := eligible()
			tc.mutate(&r)
			if got := r.ContractorEditable(); got != tc.wantEditable {
				t.Errorf("ContractorEditable() = %v, want %v", got, tc.wantEditable)
			}
		})
	}
}

// --- Identity-field immutability by source type (design spec §2.2) ---

// MaterialID and WorkItemID are editable ONLY on a manual requirement. A
// cost_item anchor would lose its aggregation identity; a split child inherits
// its identity from the source.
func TestIdentityFieldsEditableOnlyForManualSource(t *testing.T) {
	cases := map[mr.SourceType]bool{
		mr.SourceTypeCostItem: false,
		mr.SourceTypeManual:   true,
		mr.SourceTypeSplit:    false,
	}
	for sourceType, wantEditable := range cases {
		t.Run(string(sourceType), func(t *testing.T) {
			r := eligible()
			r.SourceType = sourceType
			if got := r.IdentityFieldsEditable(); got != wantEditable {
				t.Errorf("IdentityFieldsEditable() = %v, want %v", got, wantEditable)
			}
		})
	}
}

// Terminal status overrides the source-type rule: §2.1 states it outranks
// every §2.2 entry. A manual requirement that has been split or archived is
// read-only despite being manual.
func TestTerminalStatusOverridesSourceTypeMutability(t *testing.T) {
	for _, status := range []mr.RequirementStatus{mr.RequirementStatusSplit, mr.RequirementStatusArchived} {
		r := eligible()
		r.SourceType = mr.SourceTypeManual
		r.Status = status
		if r.IdentityFieldsEditable() {
			t.Errorf("status %q must override manual source-type editability", status)
		}
	}
}

// A claim also overrides it.
func TestClaimOverridesSourceTypeMutability(t *testing.T) {
	chain := "rfq1"
	r := eligible()
	r.SourceType = mr.SourceTypeManual
	r.ActiveRFQChainID = &chain
	if r.IdentityFieldsEditable() {
		t.Fatal("an active claim must override manual source-type editability")
	}
}

// --- Splittability (design spec §8.3) ---

func TestSplittable(t *testing.T) {
	chain := "rfq1"
	cases := []struct {
		name    string
		mutate  func(*mr.MaterialRequirement)
		wantErr error
	}{
		{"draft cost_item", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusDraft }, nil},
		{"reviewed cost_item", func(r *mr.MaterialRequirement) {}, nil},
		{"reviewed manual", func(r *mr.MaterialRequirement) { r.SourceType = mr.SourceTypeManual }, nil},
		{"already split", func(r *mr.MaterialRequirement) {
			r.Status = mr.RequirementStatusSplit
		}, mr.ErrRequirementTerminal},
		{"archived", func(r *mr.MaterialRequirement) {
			r.Status = mr.RequirementStatusArchived
		}, mr.ErrRequirementTerminal},
		{"split child", func(r *mr.MaterialRequirement) {
			r.SourceType = mr.SourceTypeSplit
		}, mr.ErrRecursiveSplitNotSupported},
		{"claimed", func(r *mr.MaterialRequirement) {
			r.ActiveRFQChainID = &chain
		}, mr.ErrMaterialRequirementAlreadyClaimed},
		{"split in progress", func(r *mr.MaterialRequirement) {
			r.SplitState = mr.SplitStateCreating
		}, mr.ErrSplitInProgress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := eligible()
			tc.mutate(&r)
			if got := r.SplittableErr(); got != tc.wantErr {
				t.Errorf("SplittableErr() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

// Terminal status is reported before recursion: a split CHILD that has itself
// been archived is terminal, and the more fundamental reason must win so the
// contractor is not told to "remove the RFQ line" for a retired record.
func TestSplittableReportsTerminalBeforeRecursion(t *testing.T) {
	r := eligible()
	r.SourceType = mr.SourceTypeSplit
	r.Status = mr.RequirementStatusArchived
	if got := r.SplittableErr(); got != mr.ErrRequirementTerminal {
		t.Fatalf("SplittableErr() = %v, want ErrRequirementTerminal", got)
	}
}

// --- Review reset (design spec §5.4) ---

// Supplier/procurement-relevant edits reset reviewed -> draft; an
// InternalNotes-only edit does not.
func TestEditResetsReviewOnlyForProcurementRelevantFields(t *testing.T) {
	cases := []struct {
		name      string
		change    mr.RequirementEdit
		wantReset bool
	}{
		{"internal notes only", mr.RequirementEdit{InternalNotesChanged: true}, false},
		{"nothing changed", mr.RequirementEdit{}, false},
		{"material", mr.RequirementEdit{MaterialIDChanged: true}, true},
		{"work item", mr.RequirementEdit{WorkItemIDChanged: true}, true},
		{"quantity value", mr.RequirementEdit{QuantityValueChanged: true}, true},
		{"quantity unit", mr.RequirementEdit{QuantityUnitChanged: true}, true},
		{"specification", mr.RequirementEdit{SpecificationChanged: true}, true},
		{"required-by date", mr.RequirementEdit{RequiredByDateChanged: true}, true},
		{"procurement notes", mr.RequirementEdit{ProcurementNotesChanged: true}, true},
		{"internal notes plus quantity", mr.RequirementEdit{
			InternalNotesChanged: true, QuantityValueChanged: true,
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.change.ResetsReview(); got != tc.wantReset {
				t.Errorf("ResetsReview() = %v, want %v", got, tc.wantReset)
			}
		})
	}
}

// An acknowledgement is scoped to the material/unit combination in force when
// it was given, so either change clears it (design spec §2.2).
func TestEditClearsUnitAcknowledgement(t *testing.T) {
	cases := []struct {
		name      string
		change    mr.RequirementEdit
		wantClear bool
	}{
		{"material changed", mr.RequirementEdit{MaterialIDChanged: true}, true},
		{"quantity unit changed", mr.RequirementEdit{QuantityUnitChanged: true}, true},
		{"both changed", mr.RequirementEdit{MaterialIDChanged: true, QuantityUnitChanged: true}, true},
		{"quantity value only", mr.RequirementEdit{QuantityValueChanged: true}, false},
		{"specification only", mr.RequirementEdit{SpecificationChanged: true}, false},
		{"internal notes only", mr.RequirementEdit{InternalNotesChanged: true}, false},
		{"work item only", mr.RequirementEdit{WorkItemIDChanged: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.change.ClearsUnitAcknowledgement(); got != tc.wantClear {
				t.Errorf("ClearsUnitAcknowledgement() = %v, want %v", got, tc.wantClear)
			}
		})
	}
}

// --- Unit mismatch computation (design spec §3.5) ---

// Comparison is trim + Unicode-aware case-fold. No conversion is ever
// attempted: differing units are flagged, never reconciled.
func TestUnitMismatchComparison(t *testing.T) {
	cases := []struct {
		procurementUnit, catalogUnit string
		wantMismatch                 bool
	}{
		{"bag", "bag", false},
		{"BAG", "bag", false},
		{"Bag", "bAG", false},
		{" bag ", "bag", false},
		{"bag", "kg", true},
		{"m2", "m3", true},
		{"", "bag", true},
		{"bag", "", true},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := mr.UnitsMismatch(tc.procurementUnit, tc.catalogUnit); got != tc.wantMismatch {
			t.Errorf("UnitsMismatch(%q, %q) = %v, want %v",
				tc.procurementUnit, tc.catalogUnit, got, tc.wantMismatch)
		}
	}
}

// --- WorkItemRef bridge (design spec §2.2, A1) ---

// The model's *string must convert to the encoder's presence-marked ref, so
// generation and re-sync always hash the same identity the document holds.
func TestWorkItemRefBridgeFromModel(t *testing.T) {
	r := eligible()
	ref := mr.WorkItemRefFromPointer(r.WorkItemID)
	id, present := ref.ID()
	if !present || id != "w1" {
		t.Fatalf("ID() = (%q, %v), want (\"w1\", true)", id, present)
	}

	r.WorkItemID = nil
	if mr.WorkItemRefFromPointer(r.WorkItemID).Present() {
		t.Fatal("a nil WorkItemID must convert to an absent ref")
	}
}

// --- Model shape guards (design spec §2) ---

// WorkItemID must be a POINTER: required for cost_item, nil for manual
// project-level demand. A non-pointer field could not express absence.
func TestWorkItemIDIsOptional(t *testing.T) {
	r := eligible()
	r.SourceType = mr.SourceTypeManual
	r.WorkItemID = nil
	if r.WorkItemID != nil {
		t.Fatal("WorkItemID must be assignable to nil")
	}
	// A manual project-level requirement is still fully eligible.
	if !r.IsRFQEligible() {
		t.Fatal("a manual requirement with no WorkItem must still be RFQ-eligible")
	}
}

// The accepted source snapshot and the detection timestamp are distinct fields:
// §5.8 lets detection write SourceCheckedAt but never SourceSyncedAt.
func TestAcceptedSnapshotAndDetectionTimestampAreDistinct(t *testing.T) {
	accepted := time.Now().Add(-time.Hour)
	checked := time.Now()
	r := eligible()
	r.SourceSyncedAt = &accepted
	r.SourceCheckedAt = &checked
	if r.SourceSyncedAt.Equal(*r.SourceCheckedAt) {
		t.Fatal("SourceSyncedAt and SourceCheckedAt must be independently assignable")
	}
}
