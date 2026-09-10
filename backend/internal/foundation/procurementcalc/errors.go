package procurementcalc

import "errors"

// Calculation-kernel sentinels.
//
// These were previously declared in supplieroffers. They move with the
// functions that return them so a caller can classify a failure without
// importing a domain module. supplieroffers keeps its own names as aliases of
// these, preserving every existing errors.Is relationship.
var (
	ErrInvalidTaxMode = errors.New(
		"procurementcalc: invalid tax mode")
	ErrTaxFieldsNotAllowed = errors.New(
		"procurementcalc: tax fields are not allowed for the selected mode")
	ErrInvalidLineTax = errors.New(
		"procurementcalc: invalid line-level tax")
	ErrLineTaxRequired = errors.New(
		"procurementcalc: every quoted line requires a line tax record")
	ErrTaxRateNotAllowed = errors.New(
		"procurementcalc: a tax rate is not allowed for this record")
	ErrTaxRateRequired = errors.New(
		"procurementcalc: a tax rate is required for this record")
	ErrInvalidTaxRate = errors.New(
		"procurementcalc: tax rate must be between 1 and 10000 basis points")
	ErrTaxRegistrationRequired = errors.New(
		"procurementcalc: tax registration number is required")
	ErrTaxBasisNoteRequired = errors.New(
		"procurementcalc: tax basis note is required")
	ErrInvalidTaxType = errors.New(
		"procurementcalc: invalid tax type")
	ErrTaxCurrencyMismatch = errors.New(
		"procurementcalc: tax currency must match the issued rfq currency")
	ErrOfferTaxRequired = errors.New(
		"procurementcalc: offer-level tax record is required")
	ErrInvalidTaxAmount = errors.New(
		"procurementcalc: offer-level tax amount must be positive")
	ErrUnsupportedCurrency = errors.New(
		"procurementcalc: unsupported offer currency")
	ErrTaxTextTooLong = errors.New(
		"procurementcalc: tax text exceeds its maximum length")
	ErrInvalidTaxSelection = errors.New(
		"procurementcalc: invalid tax selection")

	// D1: a Supplier-quoted whole-offer tax amount cannot be allocated across a
	// subset without inventing commercial data the Supplier never submitted.
	ErrOfferLevelTaxRequiresCompleteSelection = errors.New(
		"procurementcalc: offer-level tax requires selecting every quoted line")

	ErrInvalidConditionalCharge = errors.New(
		"procurementcalc: invalid conditional charge group")
	ErrInvalidDeliveryCharge = errors.New(
		"procurementcalc: delivery charge must be one positive amount in the rfq currency")
)
