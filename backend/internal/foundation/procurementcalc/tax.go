package procurementcalc

import (
	"strings"
	"unicode/utf8"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

const (
	// Phase 1 RFQ issuance is MYR-only, so Supplier Offer calculations cannot
	// admit a currency that the comparison and award flow cannot combine.
	Phase1Currency                 = "MYR"
	MaxTaxRegistrationNumberLength = 128
	MaxTaxBasisNoteLength          = 500
)

// TaxMode is the discriminator for the mutually exclusive Phase E tax shapes.
// Fields from one mode are never interpreted under another mode.
type TaxMode string

const (
	TaxModeNotApplicable TaxMode = "not_applicable"
	TaxModeLineLevel     TaxMode = "line_level"
	TaxModeOfferLevel    TaxMode = "offer_level"
)

// TaxType records the Supplier's quoted tax classification. It is commercial
// data, not a platform assertion that the Supplier has a particular tax status.
type TaxType string

const (
	TaxTypeServiceTax TaxType = "service_tax"
	TaxTypeSalesTax   TaxType = "sales_tax"
	TaxTypeOther      TaxType = "other"
)

// QuotedLineTax is allowed only when SupplierOfferTax.Mode is line_level.
type QuotedLineTax struct {
	TaxType            TaxType        `bson:"taxType"`
	Exempt             bool           `bson:"exempt"`
	RateBPS            *money.RateBPS `bson:"rateBPS,omitempty"`
	RegistrationNumber string         `bson:"registrationNumber,omitempty"`
	BasisNote          string         `bson:"basisNote,omitempty"`
}

// QuotedOfferTax is allowed only when SupplierOfferTax.Mode is offer_level.
type QuotedOfferTax struct {
	TaxType            TaxType     `bson:"taxType"`
	TaxAmount          money.Money `bson:"taxAmount"`
	BasisNote          string      `bson:"basisNote"`
	RegistrationNumber string      `bson:"registrationNumber,omitempty"`
}

// SupplierOfferTax keeps the mode discriminator beside the sole mode-specific
// whole-offer record. Line-level records remain attached to their exact lines.
type SupplierOfferTax struct {
	Mode       TaxMode         `bson:"mode"`
	OfferLevel *QuotedOfferTax `bson:"offerLevel,omitempty"`
}

// TaxableQuotedLine is the already server-calculated subtotal that tax may use.
// Delivery and conditional charges cannot enter this narrow input by design.
type TaxableQuotedLine struct {
	RFQLineID string
	Subtotal  money.Money
	Tax       *QuotedLineTax
}

type TaxCalculationInput struct {
	Currency    string
	Tax         SupplierOfferTax
	QuotedLines []TaxableQuotedLine
}

type CalculatedLineTax struct {
	RFQLineID string
	Amount    money.Money
}

type TaxCalculation struct {
	Lines []CalculatedLineTax
	Total money.Money
}

// CalculateTax validates the selected tax shape and returns only
// server-calculated amounts.
func CalculateTax(input TaxCalculationInput) (TaxCalculation, error) {
	if input.Currency != Phase1Currency {
		return TaxCalculation{}, ErrUnsupportedCurrency
	}
	seenLines := make(map[string]struct{}, len(input.QuotedLines))
	for _, line := range input.QuotedLines {
		if strings.TrimSpace(line.RFQLineID) == "" || line.Subtotal.Amount <= 0 {
			return TaxCalculation{}, ErrInvalidLineTax
		}
		if line.Subtotal.Currency != input.Currency {
			return TaxCalculation{}, ErrTaxCurrencyMismatch
		}
		if _, duplicate := seenLines[line.RFQLineID]; duplicate {
			return TaxCalculation{}, ErrInvalidLineTax
		}
		seenLines[line.RFQLineID] = struct{}{}
	}
	switch input.Tax.Mode {
	case TaxModeNotApplicable:
		if input.Tax.OfferLevel != nil {
			return TaxCalculation{}, ErrTaxFieldsNotAllowed
		}
		for _, line := range input.QuotedLines {
			if line.Tax != nil {
				return TaxCalculation{}, ErrTaxFieldsNotAllowed
			}
		}
		return TaxCalculation{Total: money.New(0, input.Currency)}, nil
	case TaxModeLineLevel:
		if input.Tax.OfferLevel != nil {
			return TaxCalculation{}, ErrTaxFieldsNotAllowed
		}

		result := TaxCalculation{
			Lines: make([]CalculatedLineTax, 0, len(input.QuotedLines)),
			Total: money.New(0, input.Currency),
		}
		for _, line := range input.QuotedLines {
			if err := validateQuotedLineTax(line.Tax); err != nil {
				return TaxCalculation{}, err
			}

			// Apply and round per line. Reapplying the rate to the aggregate
			// would change boundary results and invent a different tax quote.
			amount := money.New(0, input.Currency)
			if !line.Tax.Exempt {
				amount = money.ApplyRateBPS(line.Subtotal, *line.Tax.RateBPS)
			}
			result.Lines = append(result.Lines, CalculatedLineTax{
				RFQLineID: line.RFQLineID,
				Amount:    amount,
			})
			total, err := result.Total.Add(amount)
			if err != nil {
				return TaxCalculation{}, ErrInvalidLineTax
			}
			result.Total = total
		}
		return result, nil
	case TaxModeOfferLevel:
		for _, line := range input.QuotedLines {
			if line.Tax != nil {
				return TaxCalculation{}, ErrTaxFieldsNotAllowed
			}
		}
		tax := input.Tax.OfferLevel
		if tax == nil {
			return TaxCalculation{}, ErrOfferTaxRequired
		}
		if tax.TaxAmount.Amount <= 0 {
			return TaxCalculation{}, ErrInvalidTaxAmount
		}
		if tax.TaxAmount.Currency != input.Currency {
			return TaxCalculation{}, ErrTaxCurrencyMismatch
		}
		if taxTextTooLong(tax.RegistrationNumber, MaxTaxRegistrationNumberLength) ||
			taxTextTooLong(tax.BasisNote, MaxTaxBasisNoteLength) {
			return TaxCalculation{}, ErrTaxTextTooLong
		}
		if strings.TrimSpace(tax.BasisNote) == "" {
			return TaxCalculation{}, ErrTaxBasisNoteRequired
		}
		switch tax.TaxType {
		case TaxTypeOther:
		case TaxTypeServiceTax, TaxTypeSalesTax:
			if strings.TrimSpace(tax.RegistrationNumber) == "" {
				return TaxCalculation{}, ErrTaxRegistrationRequired
			}
		default:
			return TaxCalculation{}, ErrInvalidTaxType
		}
		return TaxCalculation{Total: tax.TaxAmount}, nil
	default:
		return TaxCalculation{}, ErrInvalidTaxMode
	}
}

func validateQuotedLineTax(tax *QuotedLineTax) error {
	if tax == nil {
		return ErrLineTaxRequired
	}
	if taxTextTooLong(tax.RegistrationNumber, MaxTaxRegistrationNumberLength) ||
		taxTextTooLong(tax.BasisNote, MaxTaxBasisNoteLength) {
		return ErrTaxTextTooLong
	}
	switch tax.TaxType {
	case TaxTypeServiceTax, TaxTypeSalesTax, TaxTypeOther:
	default:
		return ErrInvalidTaxType
	}

	// "other" is meaningful only with the Supplier's bounded explanation,
	// including when the Supplier marks that line exempt.
	if tax.TaxType == TaxTypeOther && strings.TrimSpace(tax.BasisNote) == "" {
		return ErrTaxBasisNoteRequired
	}

	if tax.Exempt {
		if tax.RateBPS != nil {
			return ErrTaxRateNotAllowed
		}
		return nil
	}
	if tax.RateBPS == nil {
		return ErrTaxRateRequired
	}
	if *tax.RateBPS < 1 || *tax.RateBPS > money.BasisPointsDenominator {
		return ErrInvalidTaxRate
	}
	if (tax.TaxType == TaxTypeServiceTax || tax.TaxType == TaxTypeSalesTax) &&
		strings.TrimSpace(tax.RegistrationNumber) == "" {
		return ErrTaxRegistrationRequired
	}
	return nil
}

func taxTextTooLong(value string, limit int) bool {
	return utf8.RuneCountInString(strings.TrimSpace(value)) > limit
}

// ValidateTaxSelection enforces the one award-time restriction that follows
// from the tax discriminator. A Supplier-quoted offer-level amount cannot be
// safely allocated across a subset without inventing commercial data.
func ValidateTaxSelection(
	mode TaxMode,
	positivelyQuotedRFQLineIDs []string,
	selectedRFQLineIDs []string,
) error {
	switch mode {
	case TaxModeNotApplicable, TaxModeLineLevel, TaxModeOfferLevel:
	default:
		return ErrInvalidTaxMode
	}

	quoted := make(map[string]struct{}, len(positivelyQuotedRFQLineIDs))
	for _, lineID := range positivelyQuotedRFQLineIDs {
		if strings.TrimSpace(lineID) == "" {
			return ErrInvalidTaxSelection
		}
		if _, duplicate := quoted[lineID]; duplicate {
			return ErrInvalidTaxSelection
		}
		quoted[lineID] = struct{}{}
	}

	selected := make(map[string]struct{}, len(selectedRFQLineIDs))
	for _, lineID := range selectedRFQLineIDs {
		if _, exists := quoted[lineID]; !exists {
			return ErrInvalidTaxSelection
		}
		if _, duplicate := selected[lineID]; duplicate {
			return ErrInvalidTaxSelection
		}
		selected[lineID] = struct{}{}
	}

	if mode == TaxModeOfferLevel &&
		len(selected) != 0 &&
		len(selected) != len(quoted) {
		return ErrOfferLevelTaxRequiresCompleteSelection
	}
	return nil
}
