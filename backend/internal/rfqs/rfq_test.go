package rfqs_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// C1 covers the RFQ aggregate, its supplier-facing projections, and the M8
// issuance seam (design spec §6.1, §6.2, §9).

func TestRFQStatusIsValid(t *testing.T) {
	for _, s := range []rfqs.RFQStatus{rfqs.RFQStatusDraft, rfqs.RFQStatusReady} {
		if !s.IsValid() {
			t.Errorf("%q must be valid", s)
		}
	}
	// M7 has NO issued status: the issued/versioned lifecycle is M8's, and no
	// M7 code path may create one (design spec §6.4).
	for _, s := range []rfqs.RFQStatus{"", "issued", "cancelled", "DRAFT"} {
		if s.IsValid() {
			t.Errorf("%q must not be valid", s)
		}
	}
}

// The RFQ ID IS the chain identity. M8's issued versions reference this, never
// RFQNumber (design spec §6.1).
func TestRFQChainIDIsTheAggregateID(t *testing.T) {
	rfq := rfqs.RFQ{ID: "rfq_1", CompanyID: "company_a", RFQNumber: "RFQ-000124"}
	if rfq.ChainID() != "rfq_1" {
		t.Errorf("ChainID() = %q, want the aggregate id rfq_1 — RFQNumber is display metadata",
			rfq.ChainID())
	}
}

// --- The supplier-facing allowlist (design spec §6.2, §9) ---

// InternalNotes must be structurally absent from every supplier-facing
// projection, so it cannot leak even by mistake. Asserted by reflection rather
// than by reading values: a field that does not exist cannot be populated by a
// future edit.
func TestSupplierFacingProjectionsHaveNoInternalNotesField(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{"RFQLine", reflect.TypeOf(rfqs.RFQLine{})},
		{"RFQLineSnapshot", reflect.TypeOf(rfqs.RFQLineSnapshot{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < tc.typ.NumField(); i++ {
				name := strings.ToLower(tc.typ.Field(i).Name)
				if strings.Contains(name, "internal") {
					t.Errorf("%s.%s is supplier-visible and must not exist",
						tc.name, tc.typ.Field(i).Name)
				}
				// An RFQ line ASKS a supplier to quote; it never carries what
				// the contractor thinks it costs (design spec §6.2).
				for _, banned := range []string{"price", "cost", "margin", "amount", "money"} {
					if strings.Contains(name, banned) {
						t.Errorf("%s.%s carries %q — an RFQ line has no price, cost or margin",
							tc.name, tc.typ.Field(i).Name, banned)
					}
				}
			}
		})
	}
}

// There is deliberately no SnapshotFingerprint: §6.3 removes the line-drift
// subsystem entirely, so nothing would consume one.
func TestRFQLineHasNoSnapshotFingerprint(t *testing.T) {
	typ := reflect.TypeOf(rfqs.RFQLine{})
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(strings.ToLower(typ.Field(i).Name), "fingerprint") {
			t.Errorf("RFQLine.%s exists, but §6.3 removes the drift subsystem that would "+
				"consume it — the claim IS the stability guarantee", typ.Field(i).Name)
		}
	}
}

// The RFQ aggregate itself DOES carry InternalNotes: it is contractor-only and
// present in the aggregate, absent from the projections (design spec §6.1).
func TestRFQAggregateCarriesInternalNotes(t *testing.T) {
	typ := reflect.TypeOf(rfqs.RFQ{})
	if _, ok := typ.FieldByName("InternalNotes"); !ok {
		t.Error("RFQ.InternalNotes must exist — the privacy boundary is that it is absent " +
			"from PROJECTIONS, not from the aggregate")
	}
	if _, ok := typ.FieldByName("SupplierInstructions"); !ok {
		t.Error("RFQ.SupplierInstructions must exist and is supplier-visible")
	}
}

// M7 models no issued state and no version field. Their absence is what makes
// it impossible for M7 code to create one (design spec §6.4, §9.1).
func TestRFQHasNoVersionOrIssuedFields(t *testing.T) {
	typ := reflect.TypeOf(rfqs.RFQ{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		switch name {
		case "Version", "IssuedAt", "IssuedByUserID", "InvitationID", "Token":
			t.Errorf("RFQ.%s exists, but externally-issued versioning is M8's and no M7 "+
				"code path may create one", name)
		}
	}
}

// --- Line ordering (design spec §6.2) ---

func TestNextSortOrderAppendsAfterTheHighest(t *testing.T) {
	rfq := rfqs.RFQ{Lines: []rfqs.RFQLine{
		{ID: "line_1", SortOrder: 0},
		{ID: "line_2", SortOrder: 7},
		{ID: "line_3", SortOrder: 3},
	}}
	if got := rfq.NextSortOrder(); got != 8 {
		t.Errorf("NextSortOrder() = %d, want 8 (one past the highest, not the count)", got)
	}
}

func TestNextSortOrderOnAnEmptyRFQIsZero(t *testing.T) {
	if got := (rfqs.RFQ{}).NextSortOrder(); got != 0 {
		t.Errorf("NextSortOrder() = %d, want 0", got)
	}
}

func TestFindLineLocatesByID(t *testing.T) {
	rfq := rfqs.RFQ{Lines: []rfqs.RFQLine{
		{ID: "line_1", MaterialName: "Cement"},
		{ID: "line_2", MaterialName: "Sand"},
	}}

	line, ok := rfq.FindLine("line_2")
	if !ok {
		t.Fatal("FindLine did not locate an existing line")
	}
	if line.MaterialName != "Sand" {
		t.Errorf("FindLine returned %q, want Sand", line.MaterialName)
	}
	if _, ok := rfq.FindLine("line_zzz"); ok {
		t.Error("FindLine reported a missing line as found")
	}
}

// A requirement may appear on at most one line of a chain — the claim enforces
// it, and this predicate lets rfqs detect the retry case (design spec §7.3).
func TestFindLineByRequirementID(t *testing.T) {
	rfq := rfqs.RFQ{Lines: []rfqs.RFQLine{
		{ID: "line_1", SourceMaterialRequirementID: "mr_1"},
		{ID: "line_2", SourceMaterialRequirementID: "mr_2"},
	}}

	line, ok := rfq.FindLineByRequirementID("mr_2")
	if !ok || line.ID != "line_2" {
		t.Errorf("FindLineByRequirementID = %+v/%v, want line_2", line, ok)
	}
	if _, ok := rfq.FindLineByRequirementID("mr_zzz"); ok {
		t.Error("reported a missing requirement as present")
	}
}

// --- Deletion and readiness predicates (design spec §6.4, §7.7) ---

func TestIsDeletableRequiresDraftAndNoLines(t *testing.T) {
	cases := []struct {
		name string
		rfq  rfqs.RFQ
		want bool
	}{
		{"empty draft", rfqs.RFQ{Status: rfqs.RFQStatusDraft}, true},
		{"draft with a line", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: []rfqs.RFQLine{{ID: "line_1"}},
		}, false},
		{"empty ready", rfqs.RFQ{Status: rfqs.RFQStatusReady}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rfq.IsDeletable(); got != tc.want {
				t.Errorf("IsDeletable() = %v, want %v", got, tc.want)
			}
		})
	}
	// The THIRD condition — zero outstanding claims — is deliberately not part
	// of this predicate: it requires a repository read, so the service enforces
	// it (design spec §7.7). This test documents that split.
}

// --- IssuanceStatusSource: the M8 seam (design spec §1.3, §6.4) ---

// M7 wires NoExternalIssuanceSource, which always reports "not issued", so
// reopen always succeeds in M7. M8 swaps in the real adapter with NO change to
// service logic.
func TestNoExternalIssuanceSourceAlwaysReportsNotIssued(t *testing.T) {
	issued, err := rfqs.NoExternalIssuanceSource{}.RFQChainIssued(
		context.Background(), "company_a", "chain_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Error("NoExternalIssuanceSource must always report false — M7 issues nothing")
	}
}

// The seam is keyed on rfqChainID, never RFQNumber: the number is a
// tenant-scoped DISPLAY identifier, while the chain ID is the stable
// cross-module identity M8 uses (design spec §1.3).
func TestIssuanceStatusSourceIsKeyedOnChainID(t *testing.T) {
	typ := reflect.TypeOf((*rfqs.IssuanceStatusSource)(nil)).Elem()
	method, ok := typ.MethodByName("RFQChainIssued")
	if !ok {
		t.Fatal("IssuanceStatusSource must declare RFQChainIssued")
	}
	// (ctx, companyID, rfqChainID) -> (bool, error)
	if got := method.Type.NumIn(); got != 3 {
		t.Errorf("RFQChainIssued takes %d parameters, want 3", got)
	}
	if method.Type.NumOut() != 2 {
		t.Errorf("RFQChainIssued returns %d values, want (bool, error)", method.Type.NumOut())
	}
}

// --- ClaimSnapshot -> RFQLine (design spec §6.2, §7.2 step 4) ---

// Every line field is copied from the ClaimSnapshot the successful claim
// returned, so the line reflects exactly the state the claim validated.
func TestLineFromClaimSnapshotCopiesTheAllowlistedFields(t *testing.T) {
	requiredBy := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	snap := rfqs.ClaimSnapshot{
		RequirementID: "mr_1", Revision: 5,
		MaterialID: "material_1", MaterialName: "Portland Cement",
		Specification: "OPC 50kg",
		QuantityValue: "100", QuantityUnit: "bag",
		RequiredByDate: &requiredBy, ProcurementNotes: "deliver to site gate",
	}
	at := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	line, err := rfqs.LineFromClaimSnapshot("line_1", snap, 3, at)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if line.ID != "line_1" {
		t.Errorf("ID = %q, want the pre-generated line_1", line.ID)
	}
	if line.SourceMaterialRequirementID != "mr_1" {
		t.Errorf("SourceMaterialRequirementID = %q, want mr_1", line.SourceMaterialRequirementID)
	}
	if line.MaterialName != "Portland Cement" || line.Specification != "OPC 50kg" {
		t.Errorf("descriptive fields not copied: %+v", line)
	}
	if line.Quantity.Value.String() != "100" || line.Quantity.Unit != "bag" {
		t.Errorf("Quantity = %s %s, want 100 bag", line.Quantity.Value, line.Quantity.Unit)
	}
	if line.RequiredByDate == nil || !line.RequiredByDate.Equal(requiredBy) {
		t.Errorf("RequiredByDate = %v, want %v", line.RequiredByDate, requiredBy)
	}
	if line.ProcurementNotes != "deliver to site gate" {
		t.Errorf("ProcurementNotes = %q", line.ProcurementNotes)
	}
	if line.SortOrder != 3 {
		t.Errorf("SortOrder = %d, want 3", line.SortOrder)
	}
	if !line.SnapshotAt.Equal(at) {
		t.Errorf("SnapshotAt = %v, want %v", line.SnapshotAt, at)
	}
}

// A malformed quantity in the snapshot is an error, never a silent zero: a line
// asking a supplier to quote "0" would be worse than a failure.
func TestLineFromClaimSnapshotRejectsAMalformedQuantity(t *testing.T) {
	for _, value := range []string{"", "abc", "0", "-5"} {
		snap := rfqs.ClaimSnapshot{
			RequirementID: "mr_1", MaterialName: "Cement",
			QuantityValue: value, QuantityUnit: "bag",
		}
		if _, err := rfqs.LineFromClaimSnapshot("line_1", snap, 0, time.Now()); err == nil {
			t.Errorf("quantity %q was accepted; a supplier must never be asked to quote it", value)
		}
	}
}

// --- Ready-time validation (design spec §6.4) ---

func TestReadinessErrRequiresLinesAndDeliveryAddress(t *testing.T) {
	withLine := []rfqs.RFQLine{{ID: "line_1"}}

	cases := []struct {
		name    string
		rfq     rfqs.RFQ
		wantErr error
	}{
		{"no lines", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, DeliveryAddress: "12 Site Road",
		}, rfqs.ErrRFQNoLines},
		{"no delivery address", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine,
		}, rfqs.ErrRFQDeliveryAddressRequired},
		{"whitespace delivery address", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine, DeliveryAddress: "   ",
		}, rfqs.ErrRFQDeliveryAddressRequired},
		{"already ready", rfqs.RFQ{
			Status: rfqs.RFQStatusReady, Lines: withLine, DeliveryAddress: "12 Site Road",
		}, rfqs.ErrRFQNotDraft},
		{"requiredByDate before responseDeadline", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine, DeliveryAddress: "12 Site Road",
			ResponseDeadline: timePtr(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)),
			RequiredByDate:   timePtr(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		}, rfqs.ErrRFQDatesOutOfOrder},
		{"requiredByDate equal to responseDeadline", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine, DeliveryAddress: "12 Site Road",
			ResponseDeadline: timePtr(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)),
			RequiredByDate:   timePtr(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)),
		}, nil},
		{"requiredByDate set with no responseDeadline", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine, DeliveryAddress: "12 Site Road",
			RequiredByDate: timePtr(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		}, rfqs.ErrRFQDatesOutOfOrder},
		{"requiredByDate after responseDeadline", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine, DeliveryAddress: "12 Site Road",
			ResponseDeadline: timePtr(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
			RequiredByDate:   timePtr(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)),
		}, nil},
		{"valid", rfqs.RFQ{
			Status: rfqs.RFQStatusDraft, Lines: withLine, DeliveryAddress: "12 Site Road",
		}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rfq.ReadinessErr()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ReadinessErr() = %v, want nil", err)
				}
				return
			}
			if err == nil || !isSentinel(err, tc.wantErr) {
				t.Fatalf("ReadinessErr() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func isSentinel(got, want error) bool { return got == want }

func timePtr(t time.Time) *time.Time { return &t }
