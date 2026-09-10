package supplieroffers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// Submission calculation and canonical fingerprint (spec §7).
//
// AI suggests, the system calculates: every amount below is derived here from
// the draft's own quoted inputs and the authoritative issued RFQ. No client-
// supplied total is ever trusted, and validation runs BEFORE the draft CAS so a
// draft is never claimed for a submission that cannot complete.

// SubmissionCalculationInput is the complete input to submission validation.
type SubmissionCalculationInput struct {
	Draft       SupplierOfferDraft
	RFQ         IssuedRFQSnapshot
	SubmittedAt time.Time
}

// SubmissionCalculation is the authoritative, fully calculated snapshot that
// becomes an immutable Offer Version.
type SubmissionCalculation struct {
	Lines                []SupplierOfferLine
	QuotedLineSubtotal   money.Money
	QuotedTaxTotal       money.Money
	FullOfferChargeTotal money.Money
	DeliveryChargeTotal  money.Money
	GrandTotal           money.Money
	Fingerprint          string
}

// CalculateSubmission validates every submission rule and derives every total.
//
// Order matters: the response window and completeness are checked before any
// arithmetic, so a late or incomplete offer is refused without partially
// computing a total that would never be stored.
func CalculateSubmission(input SubmissionCalculationInput) (
	SubmissionCalculation, error) {
	if procurementlimits.ValidateCount(len(input.Draft.Lines),
		procurementlimits.MaxLines) != nil ||
		procurementlimits.ValidateCount(len(input.RFQ.Lines),
			procurementlimits.MaxLines) != nil ||
		procurementlimits.ValidateCount(len(input.Draft.ChargeGroups),
			procurementlimits.MaxChargeGroups) != nil {
		return SubmissionCalculation{}, ErrInputLimitExceeded
	}
	if _, err := procurementlimits.TrimText(input.Draft.SupplierNotes,
		procurementlimits.MaxLongTextRunes); err != nil {
		return SubmissionCalculation{}, ErrInputLimitExceeded
	}
	for _, line := range input.Draft.Lines {
		for _, bounded := range []struct {
			value   string
			maximum int
		}{
			{line.Brand, procurementlimits.MaxDisplayTextRunes},
			{line.SKU, procurementlimits.MaxSKUTextRunes},
			{line.ProductDescription, procurementlimits.MaxLongTextRunes},
			{line.LeadTime, procurementlimits.MaxDisplayTextRunes},
			{line.SupplierLineNotes, procurementlimits.MaxLongTextRunes},
			{line.CommercialExceptions, procurementlimits.MaxLongTextRunes},
		} {
			if _, err := procurementlimits.TrimText(
				bounded.value, bounded.maximum); err != nil {
				return SubmissionCalculation{}, ErrInputLimitExceeded
			}
		}
	}

	if input.Draft.Currency != Phase1Currency ||
		input.RFQ.Currency != Phase1Currency {
		return SubmissionCalculation{}, ErrUnsupportedCurrency
	}

	// A late offer must never enter comparison beside offers that met the
	// deadline, so the window closes strictly at the deadline.
	if !input.SubmittedAt.Before(input.RFQ.ResponseDeadline) {
		return SubmissionCalculation{}, ErrResponseWindowClosed
	}

	// Validity must outlast submission, otherwise the offer is expired the
	// moment it is submitted.
	if input.Draft.OfferValidUntil == nil ||
		!input.Draft.OfferValidUntil.After(input.SubmittedAt) {
		return SubmissionCalculation{}, ErrOfferValidityRequired
	}
	if err := procurementlimits.ValidateOfferValidity(
		input.SubmittedAt, *input.Draft.OfferValidUntil); err != nil {
		return SubmissionCalculation{}, ErrInvalidBusinessDate
	}

	if err := validateSubmissionGates(input.Draft); err != nil {
		return SubmissionCalculation{}, err
	}

	issuedByID := make(map[string]IssuedRFQLineSnapshot, len(input.RFQ.Lines))
	for _, line := range input.RFQ.Lines {
		issuedByID[line.ID] = line
	}

	lines := make([]SupplierOfferLine, 0, len(input.Draft.Lines))
	taxable := make([]TaxableQuotedLine, 0, len(input.Draft.Lines))
	chargeable := make([]QuotedLineSubtotal, 0, len(input.Draft.Lines))
	selected := make([]string, 0, len(input.Draft.Lines))
	quotedSubtotal := money.New(0, Phase1Currency)
	quotedCount := 0

	for _, draftLine := range input.Draft.Lines {
		issued, known := issuedByID[draftLine.RFQLineID]
		if !known {
			// A draft line with no authoritative counterpart cannot be priced
			// against anything the contractor actually asked for.
			return SubmissionCalculation{}, ErrInvalidQuotedLine
		}

		line := SupplierOfferLine{
			ID:                   draftLine.ID,
			RFQLineID:            draftLine.RFQLineID,
			ResponseStatus:       draftLine.ResponseStatus,
			SupplierLineNotes:    draftLine.SupplierLineNotes,
			CommercialExceptions: draftLine.CommercialExceptions,
			LineTaxAmount:        money.New(0, Phase1Currency),

			CopiedFromOfferVersionID: draftLine.CopiedFromOfferVersionID,
			CopiedFromOfferLineID:    draftLine.CopiedFromOfferLineID,
			CopiedAt:                 draftLine.CopiedAt,
		}

		if draftLine.ResponseStatus != OfferLineQuoted {
			// Declines carry no commercial values into the immutable version.
			lines = append(lines, line)
			continue
		}
		if draftLine.UnitPriceExcludingTax == nil {
			return SubmissionCalculation{}, ErrInvalidQuotedLine
		}

		// Recalculate rather than trusting the stored subtotal: the issued
		// quantity is authoritative at submission time.
		calculated, err := CalculateQuotedLine(Phase1Currency, QuotedLineInput{
			RFQLineID:             draftLine.RFQLineID,
			AuthoritativeQuantity: issued.Quantity,
			QuotedQuantity:        issued.Quantity,
			UnitPrice:             *draftLine.UnitPriceExcludingTax,
		})
		if err != nil {
			return SubmissionCalculation{}, err
		}

		quantity := calculated.QuotedQuantity
		unitPrice := calculated.UnitPrice
		subtotal := calculated.LineSubtotal

		line.QuotedQuantity = &quantity
		line.UnitPriceExcludingTax = &unitPrice
		line.LineSubtotalExcludingTax = &subtotal
		line.Brand = draftLine.Brand
		line.SKU = draftLine.SKU
		line.ProductDescription = draftLine.ProductDescription
		line.LeadTime = draftLine.LeadTime
		line.LineTax = draftLine.LineTax

		summed, err := quotedSubtotal.Add(subtotal)
		if err != nil {
			return SubmissionCalculation{}, err
		}
		quotedSubtotal = summed
		quotedCount++

		taxable = append(taxable, TaxableQuotedLine{
			RFQLineID: draftLine.RFQLineID,
			Subtotal:  subtotal,
			Tax:       draftLine.LineTax,
		})
		chargeable = append(chargeable, QuotedLineSubtotal{
			RFQLineID: draftLine.RFQLineID,
			Subtotal:  subtotal,
		})
		selected = append(selected, draftLine.RFQLineID)
		lines = append(lines, line)
	}

	tax, err := CalculateTax(TaxCalculationInput{
		Currency:    Phase1Currency,
		Tax:         input.Draft.Tax,
		QuotedLines: taxable,
	})
	if err != nil {
		return SubmissionCalculation{}, err
	}
	// Line tax lands on the exact line it belongs to, so an audit can trace
	// every minor unit back to its source.
	taxByLine := make(map[string]money.Money, len(tax.Lines))
	for _, calculated := range tax.Lines {
		taxByLine[calculated.RFQLineID] = calculated.Amount
	}
	for index := range lines {
		if amount, ok := taxByLine[lines[index].RFQLineID]; ok {
			lines[index].LineTaxAmount = amount
		}
	}

	groups := make([]ConditionalChargeGroup, 0, len(input.Draft.ChargeGroups))
	for _, group := range input.Draft.ChargeGroups {
		groups = append(groups, group.ConditionalChargeGroup)
	}
	charges, err := CalculateConditionalCharges(ConditionalChargeInput{
		Currency:           Phase1Currency,
		QuotedLines:        chargeable,
		SelectedRFQLineIDs: selected,
		Groups:             groups,
	})
	if err != nil {
		return SubmissionCalculation{}, err
	}

	delivery, err := CalculateDeliveryCharge(
		Phase1Currency, input.Draft.DeliveryCharge, quotedCount)
	if err != nil {
		return SubmissionCalculation{}, err
	}

	grand := quotedSubtotal
	for _, component := range []money.Money{tax.Total, charges.Total, delivery} {
		summed, addErr := grand.Add(component)
		if addErr != nil {
			return SubmissionCalculation{}, addErr
		}
		grand = summed
	}

	result := SubmissionCalculation{
		Lines:                lines,
		QuotedLineSubtotal:   quotedSubtotal,
		QuotedTaxTotal:       tax.Total,
		FullOfferChargeTotal: charges.Total,
		DeliveryChargeTotal:  delivery,
		GrandTotal:           grand,
	}
	result.Fingerprint = fingerprintSubmission(input, result)
	return result, nil
}

// validateSubmissionGates enforces completeness and every server-owned review
// gate. A gate exists because the Supplier has not yet confirmed copied content
// for THIS RFQ version; submitting past one would present unreviewed terms as
// deliberate.
func validateSubmissionGates(draft SupplierOfferDraft) error {
	for _, line := range draft.Lines {
		if line.ResponseStatus == OfferLineUnanswered ||
			line.ResponseStatus == "" {
			return ErrOfferIncomplete
		}
		if line.ReviewRequired || line.ConfirmationRequired {
			return ErrOfferReviewPending
		}
	}
	if draft.OfferTaxReviewRequired ||
		draft.DeliveryChargeReviewRequired {
		return ErrOfferReviewPending
	}
	for _, group := range draft.ChargeGroups {
		if group.ReviewRequired {
			return ErrOfferReviewPending
		}
	}
	return nil
}

// fingerprintSubmission derives the canonical content identity of an offer.
//
// The canonical structs are declared here rather than hashing a persisted
// document or transport DTO: those shapes may gain metadata over time, and
// allowing that metadata in would make identical commercial content acquire a
// different identity. Conversely every Supplier-visible commercial value is
// present, so changing the offer necessarily changes the fingerprint.
//
// Submission-time metadata is deliberately EXCLUDED. A retry of the same offer
// content is the same offer; including the timestamp would make every retry
// look like new content and defeat idempotent recovery.
func fingerprintSubmission(
	input SubmissionCalculationInput,
	calculated SubmissionCalculation,
) string {
	type canonicalLine struct {
		RFQLineID            string
		ResponseStatus       string
		QuantityValue        string
		QuantityUnit         string
		UnitPriceMinor       int64
		LineSubtotalMinor    int64
		LineTaxMinor         int64
		Brand                string
		SKU                  string
		ProductDescription   string
		LeadTime             string
		SupplierLineNotes    string
		CommercialExceptions string
	}
	type canonicalGroup struct {
		Name                 string
		ApplicableRFQLineIDs []string
		Trigger              string
		ThresholdMinor       int64
		Calculation          string
		FixedAmountMinor     int64
		RateBPS              int64
	}
	type canonicalOffer struct {
		CompanyID          string
		OfferChainID       string
		InvitationID       string
		IssuedRFQVersionID string
		RecipientIdentity  string
		Currency           string
		Lines              []canonicalLine
		TaxMode            string
		OfferTaxType       string
		OfferTaxMinor      int64
		OfferTaxBasisNote  string
		OfferTaxRegistered string
		Groups             []canonicalGroup
		DeliveryMinor      int64
		HasDelivery        bool
		OfferValidUntil    string
		SupplierNotes      string
		GrandTotalMinor    int64
	}

	lines := make([]canonicalLine, 0, len(calculated.Lines))
	for _, line := range calculated.Lines {
		canonical := canonicalLine{
			RFQLineID:            line.RFQLineID,
			ResponseStatus:       string(line.ResponseStatus),
			LineTaxMinor:         line.LineTaxAmount.Amount,
			Brand:                line.Brand,
			SKU:                  line.SKU,
			ProductDescription:   line.ProductDescription,
			LeadTime:             line.LeadTime,
			SupplierLineNotes:    line.SupplierLineNotes,
			CommercialExceptions: line.CommercialExceptions,
		}
		if line.QuotedQuantity != nil {
			canonical.QuantityValue = line.QuotedQuantity.Value.String()
			canonical.QuantityUnit = line.QuotedQuantity.Unit
		}
		if line.UnitPriceExcludingTax != nil {
			canonical.UnitPriceMinor = line.UnitPriceExcludingTax.Amount
		}
		if line.LineSubtotalExcludingTax != nil {
			canonical.LineSubtotalMinor = line.LineSubtotalExcludingTax.Amount
		}
		lines = append(lines, canonical)
	}

	groups := make([]canonicalGroup, 0, len(input.Draft.ChargeGroups))
	for _, group := range input.Draft.ChargeGroups {
		canonical := canonicalGroup{
			Name:                 group.Name,
			ApplicableRFQLineIDs: group.ApplicableRFQLineIDs,
			Trigger:              string(group.Trigger),
			Calculation:          string(group.Calculation),
		}
		if group.Threshold != nil {
			canonical.ThresholdMinor = group.Threshold.Amount
		}
		if group.FixedAmount != nil {
			canonical.FixedAmountMinor = group.FixedAmount.Amount
		}
		if group.RateBPS != nil {
			canonical.RateBPS = int64(*group.RateBPS)
		}
		groups = append(groups, canonical)
	}

	offer := canonicalOffer{
		CompanyID:          input.Draft.CompanyID,
		OfferChainID:       input.Draft.OfferChainID,
		InvitationID:       input.Draft.InvitationID,
		IssuedRFQVersionID: input.Draft.IssuedRFQVersionID,
		RecipientIdentity:  input.Draft.RecipientIdentity,
		Currency:           input.Draft.Currency,
		Lines:              lines,
		TaxMode:            string(input.Draft.Tax.Mode),
		Groups:             groups,
		SupplierNotes:      input.Draft.SupplierNotes,
		GrandTotalMinor:    calculated.GrandTotal.Amount,
	}
	if input.Draft.Tax.OfferLevel != nil {
		offer.OfferTaxType = string(input.Draft.Tax.OfferLevel.TaxType)
		offer.OfferTaxMinor = input.Draft.Tax.OfferLevel.TaxAmount.Amount
		offer.OfferTaxBasisNote = input.Draft.Tax.OfferLevel.BasisNote
		offer.OfferTaxRegistered = input.Draft.Tax.OfferLevel.RegistrationNumber
	}
	if input.Draft.DeliveryCharge != nil {
		offer.HasDelivery = true
		offer.DeliveryMinor = input.Draft.DeliveryCharge.Amount.Amount
	}
	if input.Draft.OfferValidUntil != nil {
		offer.OfferValidUntil = input.Draft.OfferValidUntil.UTC().Format(time.RFC3339Nano)
	}

	payload, err := json.Marshal(offer)
	if err != nil {
		// The canonical shape holds only JSON-infallible primitives. The guard
		// stays because silently hashing an empty payload would collapse every
		// offer onto one identity if that ever changed.
		panic("supplieroffers: canonical submission fingerprint encoding failed: " +
			err.Error())
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
