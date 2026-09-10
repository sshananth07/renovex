package estimates

import (
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrNoEstimatedCosts is returned when a Project has zero CostItems with a
// non-nil Estimated value at snapshot-creation or refresh time (design spec
// §7.1) — there is nothing to estimate.
var ErrNoEstimatedCosts = errors.New("estimates: project has no cost items with an estimated amount")

// ErrMixedCurrencyCostItems is returned when the eligible CostItems for a
// Project snapshot span more than one currency (design spec §8).
var ErrMixedCurrencyCostItems = errors.New("estimates: cost items span more than one currency")

// ErrCostSubtotalNotPositive is returned when the summed cost subtotal is
// zero or negative even though at least one eligible line exists (design
// spec §7.2) — prevents undefined margin-mode pricing (division producing
// a zero or negative ProposedSellingPrice).
var ErrCostSubtotalNotPositive = errors.New("estimates: cost subtotal must be positive")

// ErrInvalidPricingRate is returned when PricingRate is negative in either
// mode, or >= 100% (10000 bps) in margin mode (design spec §13).
var ErrInvalidPricingRate = errors.New("estimates: invalid pricing rate for the given pricing mode")

// ErrInvalidPricingMode is returned when mode is not one of the 2 defined
// PricingMode values (design spec §13).
var ErrInvalidPricingMode = errors.New("estimates: invalid pricing mode")

// SumLines computes the total CostSubtotal across lines, validating all
// lines share one currency and the result is strictly positive. Returns
// ErrNoEstimatedCosts if lines is empty, ErrMixedCurrencyCostItems if more
// than one currency is present, ErrCostSubtotalNotPositive if the sum is
// <= 0 (design spec §7.1, §7.2, §8).
func SumLines(lines []EstimateCostLine) (money.Money, error) {
	if len(lines) == 0 {
		return money.Money{}, ErrNoEstimatedCosts
	}
	currency := lines[0].SnapshottedAmount.Currency
	total := int64(0)
	for _, l := range lines {
		if l.SnapshottedAmount.Currency != currency {
			return money.Money{}, ErrMixedCurrencyCostItems
		}
		total += l.SnapshottedAmount.Amount
	}
	if total <= 0 {
		return money.Money{}, ErrCostSubtotalNotPositive
	}
	return money.New(total, currency), nil
}

// PricingResult holds the calculated outputs of applying a pricing mode/rate
// to a cost subtotal (design spec §13, §15).
type PricingResult struct {
	ProposedSellingPrice    money.Money
	ProjectedGrossProfit    money.Money
	ProjectedGrossMarginBPS money.RateBPS
}

// CalculatePricing applies markup or margin pricing to costSubtotal,
// producing ProposedSellingPrice, ProjectedGrossProfit, and
// ProjectedGrossMarginBPS in one deterministic rounding order (design spec
// §15). Validates: costSubtotal must be strictly positive
// (ErrCostSubtotalNotPositive otherwise — this is also enforced upstream
// by SumLines before the service layer ever calls CalculatePricing, but
// CalculatePricing enforces it independently too, since a zero subtotal
// would otherwise make RateBPSFromRatio divide by a zero
// ProposedSellingPrice in markup mode, or divide-by-zero represents a
// degenerate "0% margin on nothing" case that is never a valid Estimate);
// mode must be one of the 2 defined PricingMode values (ErrInvalidPricingMode
// otherwise — without this check, an invalid/typo'd mode would fall
// through with sellingPrice left at its zero-value and silently continue
// into profit/margin arithmetic against a bogus zero selling price); rate
// must be non-negative in both modes; in margin mode, rate must
// additionally be < 10000 bps (100%).
func CalculatePricing(costSubtotal money.Money, mode PricingMode, rate money.RateBPS) (PricingResult, error) {
	if costSubtotal.Amount <= 0 {
		return PricingResult{}, ErrCostSubtotalNotPositive
	}
	if !mode.IsValid() {
		return PricingResult{}, ErrInvalidPricingMode
	}
	if rate < 0 {
		return PricingResult{}, ErrInvalidPricingRate
	}
	if mode == PricingModeMargin && rate >= money.BasisPointsDenominator {
		return PricingResult{}, ErrInvalidPricingRate
	}

	var sellingPrice money.Money
	switch mode {
	case PricingModeMarkup:
		sellingPrice = money.AddRateBPS(costSubtotal, rate)
	case PricingModeMargin:
		sellingPrice = money.DivideByComplementRateBPS(costSubtotal, rate)
	}

	profit, err := sellingPrice.Subtract(costSubtotal)
	if err != nil {
		return PricingResult{}, err
	}
	marginBPS := money.RateBPSFromRatio(profit, sellingPrice)

	return PricingResult{
		ProposedSellingPrice:    sellingPrice,
		ProjectedGrossProfit:    profit,
		ProjectedGrossMarginBPS: marginBPS,
	}, nil
}
