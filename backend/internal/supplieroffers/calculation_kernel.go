package supplieroffers

import (
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
)

// The shared procurement calculation kernel (ADR 0001, ADR 0002).
//
// Tax, conditional-charge and delivery arithmetic moved to
// internal/foundation/procurementcalc so Phase F's award recalculation runs the
// SAME code as Phase E's submission calculation. Two implementations could
// drift, and a submitted Offer total that disagrees with the Award total for
// the same lines is a commercial correctness defect.
//
// These are type ALIASES, not conversions: the foundation rule types exactly
// represent the persisted Phase E contract, so the BSON tags, stored documents
// and every existing errors.Is relationship are preserved unchanged. Where a
// Phase E shape carries persistence or domain fields the kernel has no business
// knowing, it stays declared in this module instead.

type (
	TaxMode             = procurementcalc.TaxMode
	TaxType             = procurementcalc.TaxType
	QuotedLineTax       = procurementcalc.QuotedLineTax
	QuotedOfferTax      = procurementcalc.QuotedOfferTax
	SupplierOfferTax    = procurementcalc.SupplierOfferTax
	TaxableQuotedLine   = procurementcalc.TaxableQuotedLine
	TaxCalculationInput = procurementcalc.TaxCalculationInput
	CalculatedLineTax   = procurementcalc.CalculatedLineTax
	TaxCalculation      = procurementcalc.TaxCalculation

	ChargeTrigger               = procurementcalc.ChargeTrigger
	ChargeCalculation           = procurementcalc.ChargeCalculation
	ConditionalChargeGroup      = procurementcalc.ConditionalChargeGroup
	QuotedLineSubtotal          = procurementcalc.QuotedLineSubtotal
	ConditionalChargeInput      = procurementcalc.ConditionalChargeInput
	CalculatedConditionalCharge = procurementcalc.CalculatedConditionalCharge
	ConditionalChargeResult     = procurementcalc.ConditionalChargeResult

	DeliveryCharge = procurementcalc.DeliveryCharge
)

const (
	Phase1Currency                 = procurementcalc.Phase1Currency
	MaxTaxRegistrationNumberLength = procurementcalc.MaxTaxRegistrationNumberLength
	MaxTaxBasisNoteLength          = procurementcalc.MaxTaxBasisNoteLength

	MaxConditionalChargeNameLength        = procurementcalc.MaxConditionalChargeNameLength
	MaxConditionalChargeDescriptionLength = procurementcalc.MaxConditionalChargeDescriptionLength

	TaxModeNotApplicable = procurementcalc.TaxModeNotApplicable
	TaxModeLineLevel     = procurementcalc.TaxModeLineLevel
	TaxModeOfferLevel    = procurementcalc.TaxModeOfferLevel

	TaxTypeServiceTax = procurementcalc.TaxTypeServiceTax
	TaxTypeSalesTax   = procurementcalc.TaxTypeSalesTax
	TaxTypeOther      = procurementcalc.TaxTypeOther

	ChargeTriggerAnySelected             = procurementcalc.ChargeTriggerAnySelected
	ChargeTriggerAllSelected             = procurementcalc.ChargeTriggerAllSelected
	ChargeTriggerSelectedSubtotalAtLeast = procurementcalc.ChargeTriggerSelectedSubtotalAtLeast

	ChargeCalculationFixedAmount                  = procurementcalc.ChargeCalculationFixedAmount
	ChargeCalculationPercentageOfSelectedSubtotal = procurementcalc.ChargeCalculationPercentageOfSelectedSubtotal
)

// CalculateTax validates the selected tax shape and returns only
// server-calculated amounts.
func CalculateTax(input TaxCalculationInput) (TaxCalculation, error) {
	return procurementcalc.CalculateTax(input)
}

// CalculateConditionalCharges evaluates each immutable rule against only the
// exact selected quoted lines.
func CalculateConditionalCharges(
	input ConditionalChargeInput,
) (ConditionalChargeResult, error) {
	return procurementcalc.CalculateConditionalCharges(input)
}

// CalculateDeliveryCharge applies a valid fixed charge once whenever the
// selection contains at least one positively quoted line.
func CalculateDeliveryCharge(
	currency string,
	charge *DeliveryCharge,
	selectedQuotedLineCount int,
) (money.Money, error) {
	return procurementcalc.CalculateDeliveryCharge(
		currency, charge, selectedQuotedLineCount)
}

// ValidateTaxSelection enforces the one award-time restriction that follows
// from the tax discriminator (D1).
func ValidateTaxSelection(
	mode TaxMode,
	positivelyQuotedRFQLineIDs []string,
	selectedRFQLineIDs []string,
) error {
	return procurementcalc.ValidateTaxSelection(
		mode, positivelyQuotedRFQLineIDs, selectedRFQLineIDs)
}
