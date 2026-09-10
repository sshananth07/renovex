package supplieroffers

import (
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
)

// Calculation sentinels now live with the arithmetic that returns them, in
// internal/foundation/procurementcalc. They are re-exported here by VALUE, not
// re-declared: a second errors.New would produce a distinct error that
// errors.Is could not match, silently breaking every existing classification
// in this module and in the handler's status mapping.
var (
	ErrInputLimitExceeded = errors.New(
		"supplieroffers: input exceeds an approved limit")
	ErrInvalidBusinessDate = errors.New(
		"supplieroffers: business date is outside the approved horizon")
	ErrInvalidTaxMode          = procurementcalc.ErrInvalidTaxMode
	ErrTaxFieldsNotAllowed     = procurementcalc.ErrTaxFieldsNotAllowed
	ErrInvalidLineTax          = procurementcalc.ErrInvalidLineTax
	ErrLineTaxRequired         = procurementcalc.ErrLineTaxRequired
	ErrTaxRateNotAllowed       = procurementcalc.ErrTaxRateNotAllowed
	ErrTaxRateRequired         = procurementcalc.ErrTaxRateRequired
	ErrInvalidTaxRate          = procurementcalc.ErrInvalidTaxRate
	ErrTaxRegistrationRequired = procurementcalc.ErrTaxRegistrationRequired
	ErrTaxBasisNoteRequired    = procurementcalc.ErrTaxBasisNoteRequired
	ErrInvalidTaxType          = procurementcalc.ErrInvalidTaxType
	ErrTaxCurrencyMismatch     = procurementcalc.ErrTaxCurrencyMismatch
	ErrOfferTaxRequired        = procurementcalc.ErrOfferTaxRequired
	ErrInvalidTaxAmount        = procurementcalc.ErrInvalidTaxAmount
	ErrUnsupportedCurrency     = procurementcalc.ErrUnsupportedCurrency
	ErrTaxTextTooLong          = procurementcalc.ErrTaxTextTooLong
	ErrInvalidTaxSelection     = procurementcalc.ErrInvalidTaxSelection

	ErrOfferLevelTaxRequiresCompleteSelection = procurementcalc.
							ErrOfferLevelTaxRequiresCompleteSelection

	ErrInvalidConditionalCharge = procurementcalc.ErrInvalidConditionalCharge
	ErrInvalidDeliveryCharge    = procurementcalc.ErrInvalidDeliveryCharge
)

var (
	// ErrInvalidTaxMode reports a tax discriminator outside the three settled
	// Phase E modes. Unknown modes cannot be treated as tax-free because doing
	// so would silently change the Supplier's commercial response.

	// ErrTaxFieldsNotAllowed reports a field from a different discriminated
	// tax shape. Foreign-mode data is rejected rather than silently ignored.

	// ErrInvalidLineTax reports a missing or structurally invalid line-level
	// tax record. More specific validation sentinels are added as their
	// boundary behavior is introduced.

	ErrInvalidOfferTax = errors.New(
		"supplieroffers: invalid offer-level tax")
	ErrQuotedQuantityMismatch = errors.New(
		"supplieroffers: quoted quantity and unit must match the issued rfq line")
	ErrInvalidUnitPrice = errors.New(
		"supplieroffers: quoted unit price must be positive")
	ErrOfferCurrencyMismatch = errors.New(
		"supplieroffers: quoted money must match the issued rfq currency")
	ErrInvalidOfferEligibility = errors.New(
		"supplieroffers: invalid offer-version eligibility state")
	ErrInvalidQuotedLine = errors.New(
		"supplieroffers: quoted line has no valid authoritative rfq line")
	ErrSupplierOffersNotConfigured = errors.New(
		"supplieroffers: required capabilities are not configured")
	ErrSupplierOfferAccessInvalid = errors.New(
		"supplieroffers: supplier offer access is invalid")
	// ErrSupplierOfferCSRFRejected is intentionally distinct from unusable or
	// foreign access. The HTTP boundary maps only this authenticated mutation
	// refusal to 403; invalid sessions remain a non-disclosing 404.
	ErrSupplierOfferCSRFRejected = errors.New(
		"supplieroffers: supplier offer csrf proof is invalid")
	ErrIssuedRFQNotFound = errors.New(
		"supplieroffers: issued rfq version not found")
	ErrOfferDraftNotFound = errors.New(
		"supplieroffers: offer draft not found")
	ErrOfferDraftConflict = errors.New(
		"supplieroffers: offer draft state or revision conflict")
	// Submission-blocking conditions (spec §7). Each is distinct so the
	// Supplier learns what to fix rather than receiving one opaque refusal.
	ErrOfferIncomplete = errors.New(
		"supplieroffers: every RFQ line must be answered before submission")
	ErrOfferReviewPending = errors.New(
		"supplieroffers: copied content must be reviewed before submission")
	ErrResponseWindowClosed = errors.New(
		"supplieroffers: the RFQ response window has closed")
	ErrOfferValidityRequired = errors.New(
		"supplieroffers: offer validity must be later than submission time")
	// ErrOfferVersionNotFound is tenant-safe: a missing version and another
	// company's version are indistinguishable to the caller.
	ErrOfferEligibilityConflict = errors.New(
		"supplieroffers: offer eligibility state or revision conflict")
	// ErrOfferTransactionsUnavailable reports a deployment topology that cannot
	// uphold G5's approved chain-plus-eligibility atomic boundary. It is a clear
	// readiness failure and maps to bounded service unavailability at runtime.
	ErrOfferTransactionsUnavailable = errors.New(
		"supplieroffers: withdrawal transactions require a replica set or sharded cluster")
	ErrOfferStatePending = errors.New(
		"supplieroffers: authoritative offer transition is pending")
	ErrOfferChainNotFound = errors.New(
		"supplieroffers: offer chain not found")
	ErrOfferVersionNotFound = errors.New(
		"supplieroffers: offer version not found")
	// ErrInvalidReplacementBarrier guards the barrier-only row shape: a claim
	// must carry its owning identities and no commercial content (§5.3A).
	ErrInvalidReplacementBarrier = errors.New(
		"supplieroffers: invalid recipient-replacement barrier")
	// ErrOfferDraftNotEmpty guards M8.1 automatic copy-forward: it never
	// merges into or overwrites Supplier-entered commercial content.
	ErrOfferDraftNotEmpty = errors.New(
		"supplieroffers: draft already has supplier-entered commercial content")
	// ErrCopySourceNotFound reports that no earlier issued RFQ version of this
	// invitation has an eligible, non-withdrawn submission to copy from.
	ErrCopySourceNotFound = errors.New(
		"supplieroffers: no eligible prior submission to copy forward")
)
