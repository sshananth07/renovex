package supplieroffers

import (
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

func qty(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("building quantity %s %s: %v", value, unit, err)
	}
	return q
}

func materialRequirement(id string) *string { return &id }

// sourceVersion is a submitted immutable version with one quoted M7-backed line.
func sourceVersion(t *testing.T) SupplierOfferVersion {
	t.Helper()
	unitPrice := money.New(2_000, Phase1Currency)
	subtotal := money.New(20_000, Phase1Currency)
	quoted := qty(t, "10", "unit")
	copiedAt := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)

	return SupplierOfferVersion{
		ID: "offer-version-1", CompanyID: "company-1", Currency: Phase1Currency,
		Lines: []SupplierOfferLine{{
			ID: "source-line-1", RFQLineID: "issued-v1-line-1",
			ResponseStatus:           OfferLineQuoted,
			QuotedQuantity:           &quoted,
			UnitPriceExcludingTax:    &unitPrice,
			LineSubtotalExcludingTax: &subtotal,
			Brand:                    "Acme", SKU: "SKU-1",
			ProductDescription: "Tile", LeadTime: "5 days",
			SupplierLineNotes:    "stocked locally",
			CommercialExceptions: "no returns",
		}},
		Tax:             SupplierOfferTax{Mode: TaxModeNotApplicable},
		SupplierNotes:   "carried over",
		OfferValidUntil: copiedAt.Add(30 * 24 * time.Hour),
	}
}

// targetRFQ is the NEW issued version the copy targets.
func targetRFQ(t *testing.T, targetQuantity quantity.Quantity) IssuedRFQSnapshot {
	t.Helper()
	return IssuedRFQSnapshot{
		ID: "issued-version-2", CompanyID: "company-1", Currency: Phase1Currency,
		Lines: []IssuedRFQLineSnapshot{{
			ID:                          "issued-v2-line-1",
			LineageID:                   "lineage-1",
			SourceMaterialRequirementID: materialRequirement("mr-1"),
			MaterialID:                  "material-1",
			MaterialName:                "Ceramic tile",
			Specification:               "300x300",
			Quantity:                    targetQuantity,
		}},
	}
}

// sourceMapping tells the copy which source line corresponds to which target
// line, mirroring what the issued-version lineage provides.
func sourceMapping() CopyForwardSourceMapping {
	return CopyForwardSourceMapping{
		LineMaterialRequirementIDs: map[string]string{"issued-v1-line-1": "mr-1"},
		LineLineageIDs:             map[string]string{"issued-v1-line-1": "lineage-1"},
	}
}

// matchingSourceRFQLine builds the source RFQ line snapshot the source was
// ACTUALLY quoted against, with every commercial field equal to targetRFQ's
// line except quantity, so CommercialFingerprintV1 matches target when
// quantity also matches. Tests use this to construct a genuine
// "nothing commercially changed" scenario rather than an accidental mismatch
// from a field the test forgot to set.
func matchingSourceRFQLine(sourceQuantity quantity.Quantity) IssuedRFQLineSnapshot {
	return IssuedRFQLineSnapshot{
		ID:                          "issued-v1-line-1",
		LineageID:                   "lineage-1",
		SourceMaterialRequirementID: materialRequirement("mr-1"),
		MaterialID:                  "material-1",
		MaterialName:                "Ceramic tile",
		Specification:               "300x300",
		Quantity:                    sourceQuantity,
	}
}

// Even when the fingerprint matches (quantity unchanged), the subtotal is
// always RECALCULATED against the target quantity and the copied unit price,
// never blindly copied from the source's stored subtotal.
func TestCopyForwardRecalculatesSubtotalRatherThanCopyingIt(t *testing.T) {
	target := targetRFQ(t, qty(t, "10", "unit"))

	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   target,
		Mapping:     sourceMapping(),
		CopiedAt:    time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		SourceRFQLines: map[string]IssuedRFQLineSnapshot{
			"issued-v1-line-1": matchingSourceRFQLine(qty(t, "10", "unit")),
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	line := result.Lines[0]
	if line.ResponseStatus != OfferLineQuoted {
		t.Fatalf("ResponseStatus = %q, want quoted", line.ResponseStatus)
	}
	if line.QuotedQuantity == nil || line.QuotedQuantity.Value.String() != "10" {
		t.Errorf("QuotedQuantity = %v, want the TARGET quantity 10", line.QuotedQuantity)
	}
	// 10 units at 2000 minor units = 20000, recalculated not copied verbatim.
	if line.LineSubtotalExcludingTax == nil ||
		*line.LineSubtotalExcludingTax != money.New(20_000, Phase1Currency) {
		t.Errorf("subtotal = %v, want the recalculated 20000",
			line.LineSubtotalExcludingTax)
	}
	if line.UnitPriceExcludingTax == nil ||
		*line.UnitPriceExcludingTax != money.New(2_000, Phase1Currency) {
		t.Errorf("unit price = %v, want the copied 2000", line.UnitPriceExcludingTax)
	}
}

// M8.1 amendment to M8 §7.1: stable lineage is necessary but no longer
// sufficient. A response is copied only when the source and target RFQ line
// snapshots share the same rfq-line-commercial-v1 fingerprint. A fingerprint
// mismatch leaves the target line unanswered — it is NOT copied with a
// review flag, because a fingerprint cannot itself prove the copied price is
// still correct for a materially different commercial ask.
func TestCopyForwardLeavesLineUnansweredWhenFingerprintDiffers(t *testing.T) {
	target := targetRFQ(t, qty(t, "25", "unit"))

	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   target,
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		SourceRFQLines: map[string]IssuedRFQLineSnapshot{
			"issued-v1-line-1": matchingSourceRFQLine(qty(t, "10", "unit")),
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	line := result.Lines[0]
	if line.ResponseStatus != OfferLineUnanswered {
		t.Errorf("ResponseStatus = %q, want unanswered when the commercial "+
			"fingerprint differs (quantity changed from 10 to 25)", line.ResponseStatus)
	}
	if line.ReviewRequired {
		t.Error("a fingerprint-mismatched line must not be copied with a review flag")
	}
	if line.UnitPriceExcludingTax != nil {
		t.Error("pricing quoted against a different fingerprint must not carry forward")
	}
}

// Identical authoritative inputs produce identical fingerprints, so the
// response copies with no review requirement: nothing the Supplier priced
// against has changed.
func TestCopyForwardCopiesWithoutReviewWhenFingerprintMatches(t *testing.T) {
	target := targetRFQ(t, qty(t, "10", "unit"))

	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   target,
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		SourceRFQLines: map[string]IssuedRFQLineSnapshot{
			"issued-v1-line-1": matchingSourceRFQLine(qty(t, "10", "unit")),
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	line := result.Lines[0]
	if line.ResponseStatus != OfferLineQuoted {
		t.Errorf("ResponseStatus = %q, want quoted when the fingerprint matches",
			line.ResponseStatus)
	}
	if line.ReviewRequired {
		t.Error("nothing changed, so the line must not require review")
	}
}

// When the source RFQ line snapshot is unknown, the copy cannot prove
// fingerprint equivalence, so it gates conservatively by leaving the line
// unanswered rather than guessing.
func TestCopyForwardLeavesLineUnansweredWhenSourceSnapshotUnknown(t *testing.T) {
	target := targetRFQ(t, qty(t, "10", "unit"))

	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   target,
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		// SourceRFQLines deliberately omits "issued-v1-line-1".
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if result.Lines[0].ResponseStatus != OfferLineUnanswered {
		t.Errorf("ResponseStatus = %q, want unanswered when the source snapshot is unknown",
			result.Lines[0].ResponseStatus)
	}
}

// A changed MaterialID makes the line incompatible: it is a different product,
// so carrying the old price forward would be wrong.
func TestCopyForwardStartsUnansweredWhenMaterialChanged(t *testing.T) {
	target := targetRFQ(t, qty(t, "10", "unit"))
	target.Lines[0].MaterialID = "material-DIFFERENT"

	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   target,
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		SourceRFQLines: map[string]IssuedRFQLineSnapshot{
			"issued-v1-line-1": {ID: "issued-v1-line-1", MaterialID: "material-1"},
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	line := result.Lines[0]
	if line.ResponseStatus != OfferLineUnanswered {
		t.Errorf("ResponseStatus = %q, want unanswered for a different material",
			line.ResponseStatus)
	}
	if line.UnitPriceExcludingTax != nil {
		t.Error("pricing was copied onto a materially incompatible line")
	}
}

// Declines always require explicit reconfirmation, even when nothing changed.
func TestCopyForwardAlwaysConfirmsCopiedDeclines(t *testing.T) {
	source := sourceVersion(t)
	source.Lines[0].ResponseStatus = OfferLineNoBid
	source.Lines[0].UnitPriceExcludingTax = nil
	source.Lines[0].LineSubtotalExcludingTax = nil
	source.Lines[0].QuotedQuantity = nil

	result, err := CopyForward(CopyForwardInput{
		Source:      source,
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		SourceRFQLines: map[string]IssuedRFQLineSnapshot{
			"issued-v1-line-1": matchingSourceRFQLine(qty(t, "10", "unit")),
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	line := result.Lines[0]
	if line.ResponseStatus != OfferLineNoBid {
		t.Errorf("ResponseStatus = %q, want the copied no_bid", line.ResponseStatus)
	}
	if !line.ConfirmationRequired {
		t.Error("a copied decline must always require reconfirmation")
	}
}

// Provenance is recorded so a contractor can trace a copied price to its source.
func TestCopyForwardRecordsProvenance(t *testing.T) {
	copiedAt := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    copiedAt,
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	line := result.Lines[0]
	if line.CopiedFromOfferVersionID != "offer-version-1" ||
		line.CopiedFromOfferLineID != "source-line-1" {
		t.Errorf("provenance = %q/%q, want the exact source identity",
			line.CopiedFromOfferVersionID, line.CopiedFromOfferLineID)
	}
	if line.CopiedAt == nil || !line.CopiedAt.Equal(copiedAt) {
		t.Errorf("CopiedAt = %v, want %v", line.CopiedAt, copiedAt)
	}
}

// OfferValidUntil never copies: submission requires a freshly supplied validity
// date later than submission time.
func TestCopyForwardNeverCopiesOfferValidity(t *testing.T) {
	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if result.OfferValidUntil != nil {
		t.Error("OfferValidUntil was copied; a stale validity date could make " +
			"an expired offer look current")
	}
	if result.SupplierNotes != "carried over" {
		t.Errorf("SupplierNotes = %q, want the copied text", result.SupplierNotes)
	}
}

// Offer-level tax copies but always requires review.
func TestCopyForwardGatesCopiedOfferLevelTax(t *testing.T) {
	source := sourceVersion(t)
	source.Tax = SupplierOfferTax{
		Mode: TaxModeOfferLevel,
		OfferLevel: &QuotedOfferTax{
			TaxType:   TaxTypeSalesTax,
			TaxAmount: money.New(1_200, Phase1Currency),
			BasisNote: "6% on the offer",
		},
	}

	result, err := CopyForward(CopyForwardInput{
		Source:      source,
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if result.Tax.Mode != TaxModeOfferLevel {
		t.Fatalf("tax mode = %q, want offer_level copied", result.Tax.Mode)
	}
	if !result.OfferTaxReviewRequired {
		t.Error("copied offer-level tax must always require review")
	}
}

// not_applicable tax copies WITHOUT a review flag: there is nothing to confirm.
func TestCopyForwardDoesNotGateNotApplicableTax(t *testing.T) {
	result, err := CopyForward(CopyForwardInput{
		Source:      sourceVersion(t),
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if result.OfferTaxReviewRequired {
		t.Error("not_applicable tax has nothing to review")
	}
}

// A delivery charge copies but always requires review.
func TestCopyForwardGatesCopiedDeliveryCharge(t *testing.T) {
	source := sourceVersion(t)
	source.DeliveryCharge = &DeliveryCharge{Amount: money.New(9_000, Phase1Currency)}

	result, err := CopyForward(CopyForwardInput{
		Source:      source,
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if result.DeliveryCharge == nil {
		t.Fatal("the delivery charge was not copied")
	}
	if !result.DeliveryChargeReviewRequired {
		t.Error("a copied delivery charge must always require review")
	}
}

// A group whose source line was not quoted is omitted WHOLE, with a bounded
// reason. Silently narrowing membership would change the commercial meaning of
// the group without the Supplier knowing.
func TestCopyForwardOmitsAWholeGroupWithANonQuotedLine(t *testing.T) {
	source := sourceVersion(t)
	source.Lines[0].ResponseStatus = OfferLineNoBid
	source.Lines[0].UnitPriceExcludingTax = nil
	source.ChargeGroups = []ConditionalChargeGroup{{
		ID: "source-group-1", Name: "Bulk handling",
		ApplicableRFQLineIDs: []string{"issued-v1-line-1"},
	}}

	result, err := CopyForward(CopyForwardInput{
		Source:      source,
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if len(result.ChargeGroups) != 0 {
		t.Fatalf("charge groups = %d, want the whole group omitted",
			len(result.ChargeGroups))
	}
	if len(result.OmittedGroups) != 1 ||
		result.OmittedGroups[0].Reason != CopyOmissionNonQuotedLine {
		t.Errorf("omissions = %+v, want one non_quoted_line reason",
			result.OmittedGroups)
	}
}

// A group whose source line is missing from the target is omitted whole.
func TestCopyForwardOmitsAWholeGroupWithAMissingLine(t *testing.T) {
	source := sourceVersion(t)
	source.ChargeGroups = []ConditionalChargeGroup{{
		ID: "source-group-1", Name: "Bulk handling",
		ApplicableRFQLineIDs: []string{"issued-v1-line-REMOVED"},
	}}

	result, err := CopyForward(CopyForwardInput{
		Source:      source,
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if len(result.ChargeGroups) != 0 {
		t.Fatal("a group with a missing source line must be omitted whole")
	}
	if len(result.OmittedGroups) != 1 ||
		result.OmittedGroups[0].Reason != CopyOmissionMissingLine {
		t.Errorf("omissions = %+v, want one missing_line reason", result.OmittedGroups)
	}
}

// A fully mappable quoted group copies, gets a NEW draft-owned ID, retains its
// source provenance, remaps its line IDs, and requires review.
func TestCopyForwardCopiesAnEligibleGroupWithRemappedLines(t *testing.T) {
	source := sourceVersion(t)
	source.ChargeGroups = []ConditionalChargeGroup{{
		ID: "source-group-1", Name: "Bulk handling",
		ApplicableRFQLineIDs: []string{"issued-v1-line-1"},
	}}

	result, err := CopyForward(CopyForwardInput{
		Source:      source,
		TargetRFQ:   targetRFQ(t, qty(t, "10", "unit")),
		Mapping:     sourceMapping(),
		CopiedAt:    time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"}},
		SourceRFQLines: map[string]IssuedRFQLineSnapshot{
			"issued-v1-line-1": matchingSourceRFQLine(qty(t, "10", "unit")),
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if len(result.ChargeGroups) != 1 {
		t.Fatalf("charge groups = %d, want the eligible group copied",
			len(result.ChargeGroups))
	}
	group := result.ChargeGroups[0]
	if group.ID == "source-group-1" || group.ID == "" {
		t.Errorf("group ID = %q, want a new draft-owned identity", group.ID)
	}
	if group.CopiedFromChargeGroupID != "source-group-1" {
		t.Errorf("CopiedFromChargeGroupID = %q, want the source group",
			group.CopiedFromChargeGroupID)
	}
	if len(group.ApplicableRFQLineIDs) != 1 ||
		group.ApplicableRFQLineIDs[0] != "issued-v2-line-1" {
		t.Errorf("applicable lines = %v, want remapped to the TARGET line IDs",
			group.ApplicableRFQLineIDs)
	}
	if !group.ReviewRequired {
		t.Error("a copied group must require review")
	}
}

// A target line with no source counterpart starts unanswered rather than being
// dropped: the Supplier must still respond to it.
func TestCopyForwardLeavesNewTargetLinesUnanswered(t *testing.T) {
	target := targetRFQ(t, qty(t, "10", "unit"))
	target.Lines = append(target.Lines, IssuedRFQLineSnapshot{
		ID: "issued-v2-line-NEW", LineageID: "lineage-new",
		SourceMaterialRequirementID: materialRequirement("mr-new"),
		MaterialID:                  "material-new",
		Quantity:                    qty(t, "4", "unit"),
	})

	result, err := CopyForward(CopyForwardInput{
		Source:    sourceVersion(t),
		TargetRFQ: target,
		Mapping:   sourceMapping(),
		CopiedAt:  time.Now().UTC(),
		TargetLines: []SupplierOfferDraftLine{
			{ID: "draft-line-1", RFQLineID: "issued-v2-line-1"},
			{ID: "draft-line-2", RFQLineID: "issued-v2-line-NEW"},
		},
	})
	if err != nil {
		t.Fatalf("CopyForward: %v", err)
	}

	if len(result.Lines) != 2 {
		t.Fatalf("lines = %d, want both target lines present", len(result.Lines))
	}
	if result.Lines[1].ResponseStatus != OfferLineUnanswered {
		t.Errorf("new line status = %q, want unanswered",
			result.Lines[1].ResponseStatus)
	}
}
