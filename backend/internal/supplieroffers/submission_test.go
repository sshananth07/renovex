package supplieroffers

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// submittableDraft is a complete draft with every gate resolved: the state
// submission validation should accept.
func submittableDraft(t *testing.T) SupplierOfferDraft {
	t.Helper()
	unitPrice := money.New(2_000, Phase1Currency)
	subtotal := money.New(20_000, Phase1Currency)
	quoted := qty(t, "10", "unit")
	validUntil := time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC)

	return SupplierOfferDraft{
		ID: "draft-1", CompanyID: "company-1", OfferChainID: "chain-1",
		InvitationID: "invitation-1", IssuedRFQVersionID: "issued-version-2",
		RecipientIdentity: "sales@supplier.test",
		Currency:          Phase1Currency, Status: DraftActive, Revision: 4,
		Lines: []SupplierOfferDraftLine{{
			ID: "draft-line-1", RFQLineID: "issued-v2-line-1",
			ResponseStatus:           OfferLineQuoted,
			QuotedQuantity:           &quoted,
			UnitPriceExcludingTax:    &unitPrice,
			LineSubtotalExcludingTax: &subtotal,
		}},
		Tax:             SupplierOfferTax{Mode: TaxModeNotApplicable},
		OfferValidUntil: &validUntil,
	}
}

func submissionRFQ(t *testing.T) IssuedRFQSnapshot {
	t.Helper()
	return IssuedRFQSnapshot{
		ID: "issued-version-2", CompanyID: "company-1", Currency: Phase1Currency,
		ResponseDeadline: time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		Lines: []IssuedRFQLineSnapshot{{
			ID: "issued-v2-line-1", MaterialID: "material-1",
			Quantity: qty(t, "10", "unit"),
		}},
	}
}

func submissionInput(t *testing.T) SubmissionCalculationInput {
	t.Helper()
	return SubmissionCalculationInput{
		Draft:       submittableDraft(t),
		RFQ:         submissionRFQ(t),
		SubmittedAt: time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC),
	}
}

// A complete draft calculates authoritative totals from the draft's own lines.
func TestCalculateSubmissionDerivesAuthoritativeTotals(t *testing.T) {
	result, err := CalculateSubmission(submissionInput(t))
	if err != nil {
		t.Fatalf("CalculateSubmission: %v", err)
	}

	if result.QuotedLineSubtotal != money.New(20_000, Phase1Currency) {
		t.Errorf("QuotedLineSubtotal = %v, want 20000", result.QuotedLineSubtotal)
	}
	if result.GrandTotal != money.New(20_000, Phase1Currency) {
		t.Errorf("GrandTotal = %v, want 20000 with no tax or charges",
			result.GrandTotal)
	}
	if result.Fingerprint == "" {
		t.Error("a submission must carry a content fingerprint")
	}
}

func TestCalculateSubmissionEnforcesPhaseGCountsAndValidityHorizon(t *testing.T) {
	tooManyLines := submissionInput(t)
	line := tooManyLines.Draft.Lines[0]
	tooManyLines.Draft.Lines = make([]SupplierOfferDraftLine, 101)
	for index := range tooManyLines.Draft.Lines {
		tooManyLines.Draft.Lines[index] = line
	}
	if _, err := CalculateSubmission(tooManyLines); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("101 lines error = %v, want ErrInputLimitExceeded", err)
	}

	tooManyGroups := submissionInput(t)
	tooManyGroups.Draft.ChargeGroups = make([]SupplierChargeGroupDraft, 26)
	if _, err := CalculateSubmission(tooManyGroups); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("26 charge groups error = %v, want ErrInputLimitExceeded", err)
	}

	tooLong := submissionInput(t)
	validUntil := tooLong.SubmittedAt.Add(366*24*time.Hour + time.Nanosecond)
	tooLong.Draft.OfferValidUntil = &validUntil
	if _, err := CalculateSubmission(tooLong); !errors.Is(err, ErrInvalidBusinessDate) {
		t.Errorf("offer validity beyond 366 days error = %v, want ErrInvalidBusinessDate", err)
	}

	longText := submissionInput(t)
	longText.Draft.Lines[0].ProductDescription = strings.Repeat("界", 2001)
	if _, err := CalculateSubmission(longText); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("2001-rune product description error = %v, want ErrInputLimitExceeded", err)
	}
}

// Totals include delivery, so a delivery charge cannot be silently dropped from
// what the contractor is asked to pay.
func TestCalculateSubmissionIncludesDeliveryInTheGrandTotal(t *testing.T) {
	input := submissionInput(t)
	input.Draft.DeliveryCharge = &DeliveryCharge{
		Amount: money.New(5_000, Phase1Currency),
	}

	result, err := CalculateSubmission(input)
	if err != nil {
		t.Fatalf("CalculateSubmission: %v", err)
	}

	if result.DeliveryChargeTotal != money.New(5_000, Phase1Currency) {
		t.Errorf("DeliveryChargeTotal = %v, want 5000", result.DeliveryChargeTotal)
	}
	if result.GrandTotal != money.New(25_000, Phase1Currency) {
		t.Errorf("GrandTotal = %v, want 25000 including delivery", result.GrandTotal)
	}
}

// An unanswered line blocks submission: the contractor would be comparing an
// incomplete offer against complete ones.
func TestCalculateSubmissionBlocksAnUnansweredLine(t *testing.T) {
	input := submissionInput(t)
	input.Draft.Lines[0].ResponseStatus = OfferLineUnanswered

	if _, err := CalculateSubmission(input); !errors.Is(err, ErrOfferIncomplete) {
		t.Fatalf("error = %v, want an incomplete-offer rejection", err)
	}
}

// An unresolved copied-line review blocks submission, so a price quoted against
// different requirements is never submitted unconfirmed.
func TestCalculateSubmissionBlocksAnUnresolvedLineReview(t *testing.T) {
	input := submissionInput(t)
	input.Draft.Lines[0].ReviewRequired = true

	if _, err := CalculateSubmission(input); !errors.Is(err, ErrOfferReviewPending) {
		t.Fatalf("error = %v, want a pending-review rejection", err)
	}
}

// An unconfirmed copied decline blocks submission.
func TestCalculateSubmissionBlocksAnUnconfirmedDecline(t *testing.T) {
	input := submissionInput(t)
	input.Draft.Lines[0].ResponseStatus = OfferLineNoBid
	input.Draft.Lines[0].QuotedQuantity = nil
	input.Draft.Lines[0].UnitPriceExcludingTax = nil
	input.Draft.Lines[0].LineSubtotalExcludingTax = nil
	input.Draft.Lines[0].ConfirmationRequired = true

	if _, err := CalculateSubmission(input); !errors.Is(err, ErrOfferReviewPending) {
		t.Fatalf("error = %v, want a pending-review rejection", err)
	}
}

// Every whole-offer gate blocks submission independently.
func TestCalculateSubmissionBlocksEachWholeOfferGate(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*SupplierOfferDraft)
	}{
		{name: "offer tax review", apply: func(d *SupplierOfferDraft) {
			d.OfferTaxReviewRequired = true
		}},
		{name: "delivery review", apply: func(d *SupplierOfferDraft) {
			d.DeliveryCharge = &DeliveryCharge{Amount: money.New(1, Phase1Currency)}
			d.DeliveryChargeReviewRequired = true
		}},
		{name: "charge group review", apply: func(d *SupplierOfferDraft) {
			d.ChargeGroups = []SupplierChargeGroupDraft{{
				ConditionalChargeGroup: ConditionalChargeGroup{ID: "group-1"},
				ReviewRequired:         true,
			}}
		}},
	}

	for _, tt := range tests {
		input := submissionInput(t)
		tt.apply(&input.Draft)
		if _, err := CalculateSubmission(input); !errors.Is(err, ErrOfferReviewPending) {
			t.Errorf("%s: error = %v, want a pending-review rejection", tt.name, err)
		}
	}
}

// Submission after the response deadline is refused: a late offer must not
// enter comparison alongside offers that met the deadline.
func TestCalculateSubmissionRefusesAfterTheResponseDeadline(t *testing.T) {
	input := submissionInput(t)
	input.SubmittedAt = input.RFQ.ResponseDeadline.Add(time.Second)

	if _, err := CalculateSubmission(input); !errors.Is(err, ErrResponseWindowClosed) {
		t.Fatalf("error = %v, want a closed-window rejection", err)
	}
}

// Validity must be strictly later than submission, so an offer is never
// submitted already expired.
func TestCalculateSubmissionRequiresFutureValidity(t *testing.T) {
	input := submissionInput(t)
	sameInstant := input.SubmittedAt
	input.Draft.OfferValidUntil = &sameInstant

	if _, err := CalculateSubmission(input); !errors.Is(err, ErrOfferValidityRequired) {
		t.Fatalf("error = %v, want a validity rejection", err)
	}

	missing := submissionInput(t)
	missing.Draft.OfferValidUntil = nil
	if _, err := CalculateSubmission(missing); !errors.Is(err, ErrOfferValidityRequired) {
		t.Fatalf("absent validity error = %v, want a validity rejection", err)
	}
}

// The fingerprint is deterministic: identical content produces one identity, so
// a retried submission is recognisable as the same offer.
func TestSubmissionFingerprintIsDeterministic(t *testing.T) {
	first, err := CalculateSubmission(submissionInput(t))
	if err != nil {
		t.Fatalf("first calculation: %v", err)
	}
	second, err := CalculateSubmission(submissionInput(t))
	if err != nil {
		t.Fatalf("second calculation: %v", err)
	}

	if first.Fingerprint != second.Fingerprint {
		t.Errorf("fingerprints differ for identical content:\n%s\n%s",
			first.Fingerprint, second.Fingerprint)
	}
}

// Changing any Supplier-visible commercial value changes the fingerprint, so a
// different offer can never be mistaken for a retry of an earlier one.
func TestSubmissionFingerprintChangesWithCommercialContent(t *testing.T) {
	base, err := CalculateSubmission(submissionInput(t))
	if err != nil {
		t.Fatalf("base calculation: %v", err)
	}

	changedPrice := submissionInput(t)
	newPrice := money.New(2_500, Phase1Currency)
	newSubtotal := money.New(25_000, Phase1Currency)
	changedPrice.Draft.Lines[0].UnitPriceExcludingTax = &newPrice
	changedPrice.Draft.Lines[0].LineSubtotalExcludingTax = &newSubtotal

	priced, err := CalculateSubmission(changedPrice)
	if err != nil {
		t.Fatalf("changed-price calculation: %v", err)
	}
	if priced.Fingerprint == base.Fingerprint {
		t.Error("a changed unit price must change the fingerprint")
	}

	changedNotes := submissionInput(t)
	changedNotes.Draft.SupplierNotes = "different terms"
	noted, err := CalculateSubmission(changedNotes)
	if err != nil {
		t.Fatalf("changed-notes calculation: %v", err)
	}
	if noted.Fingerprint == base.Fingerprint {
		t.Error("changed supplier notes must change the fingerprint")
	}
}

// Submission-time metadata must NOT enter the fingerprint: the same offer
// content retried a second later is the same offer.
func TestSubmissionFingerprintIgnoresSubmissionTimestamp(t *testing.T) {
	first, err := CalculateSubmission(submissionInput(t))
	if err != nil {
		t.Fatalf("first calculation: %v", err)
	}

	later := submissionInput(t)
	later.SubmittedAt = later.SubmittedAt.Add(time.Minute)
	second, err := CalculateSubmission(later)
	if err != nil {
		t.Fatalf("second calculation: %v", err)
	}

	if first.Fingerprint != second.Fingerprint {
		t.Error("the submission timestamp must not change content identity; " +
			"a retry would otherwise look like a different offer")
	}
}

// A declined line contributes nothing to the totals.
func TestCalculateSubmissionExcludesDeclinedLinesFromTotals(t *testing.T) {
	input := submissionInput(t)
	input.RFQ.Lines = append(input.RFQ.Lines, IssuedRFQLineSnapshot{
		ID: "issued-v2-line-2", MaterialID: "material-2",
		Quantity: qty(t, "5", "unit"),
	})
	input.Draft.Lines = append(input.Draft.Lines, SupplierOfferDraftLine{
		ID: "draft-line-2", RFQLineID: "issued-v2-line-2",
		ResponseStatus: OfferLineUnavailable,
	})

	result, err := CalculateSubmission(input)
	if err != nil {
		t.Fatalf("CalculateSubmission: %v", err)
	}

	if result.QuotedLineSubtotal != money.New(20_000, Phase1Currency) {
		t.Errorf("QuotedLineSubtotal = %v, want only the quoted line's 20000",
			result.QuotedLineSubtotal)
	}
}
