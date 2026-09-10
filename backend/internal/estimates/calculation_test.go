package estimates_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func TestCalculatePricingMarkup(t *testing.T) {
	subtotal := money.New(10000, "MYR") // RM100
	result, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProposedSellingPrice.Amount != 12000 {
		t.Fatalf("expected selling price 12000 (RM100 + 20%% markup), got %+v", result.ProposedSellingPrice)
	}
	if result.ProjectedGrossProfit.Amount != 2000 {
		t.Fatalf("expected profit 2000, got %+v", result.ProjectedGrossProfit)
	}
}

func TestCalculatePricingMargin(t *testing.T) {
	subtotal := money.New(10000, "MYR") // RM100
	result, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProposedSellingPrice.Amount != 12500 {
		t.Fatalf("expected selling price 12500 (RM100 / 0.8 = RM125, the brief's own worked example), got %+v", result.ProposedSellingPrice)
	}
	if result.ProjectedGrossMarginBPS != 2000 {
		t.Fatalf("expected resulting margin to equal the requested 2000 bps, got %d", result.ProjectedGrossMarginBPS)
	}
}

func TestCalculatePricingMarginRejects100Percent(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, 10000)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for 100%% margin, got %v", err)
	}
}

func TestCalculatePricingMarginRejectsAbove100Percent(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, 10001)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for >100%% margin, got %v", err)
	}
}

func TestCalculatePricingRejectsNegativeRate(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMarkup, -1)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for negative markup rate, got %v", err)
	}
	_, err = estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, -1)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for negative margin rate, got %v", err)
	}
}

func TestCalculatePricingRejectsInvalidMode(t *testing.T) {
	// An invalid/unrecognized PricingMode must be rejected explicitly, not
	// silently fall through to a zero-value selling price that then feeds
	// bogus profit/margin arithmetic.
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingMode("banana"), 2000)
	if err != estimates.ErrInvalidPricingMode {
		t.Fatalf("expected ErrInvalidPricingMode for an unrecognized mode, got %v", err)
	}
}

func TestCalculatePricingMarkupAllowsAbove100Percent(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	result, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMarkup, 15000) // 150% markup
	if err != nil {
		t.Fatalf("expected 150%% markup to be allowed (no upper bound on markup mode), got error: %v", err)
	}
	if result.ProposedSellingPrice.Amount != 25000 {
		t.Fatalf("expected selling price 25000 (RM100 * 2.5), got %+v", result.ProposedSellingPrice)
	}
}

func TestCalculatePricingRejectsZeroOrNegativeSubtotal(t *testing.T) {
	_, err := estimates.CalculatePricing(money.New(0, "MYR"), estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrCostSubtotalNotPositive {
		t.Fatalf("expected ErrCostSubtotalNotPositive for zero subtotal, got %v", err)
	}
	_, err = estimates.CalculatePricing(money.New(-100, "MYR"), estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrCostSubtotalNotPositive {
		t.Fatalf("expected ErrCostSubtotalNotPositive for negative subtotal, got %v", err)
	}
}

func TestSumLinesNoDoubleCounting(t *testing.T) {
	lines := []estimates.EstimateCostLine{
		{SourceCostItemID: "c1", Category: "material", SnapshottedAmount: money.New(50000, "MYR")},
		{SourceCostItemID: "c2", Category: "labour", SnapshottedAmount: money.New(30000, "MYR")},
	}
	subtotal, err := estimates.SumLines(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subtotal.Amount != 80000 {
		t.Fatalf("expected 80000 (50000+30000, no double counting), got %+v", subtotal)
	}
}

func TestSumLinesEmptyRejected(t *testing.T) {
	_, err := estimates.SumLines(nil)
	if err != estimates.ErrNoEstimatedCosts {
		t.Fatalf("expected ErrNoEstimatedCosts for zero lines, got %v", err)
	}
}

func TestSumLinesMixedCurrencyRejected(t *testing.T) {
	lines := []estimates.EstimateCostLine{
		{SourceCostItemID: "c1", SnapshottedAmount: money.New(50000, "MYR")},
		{SourceCostItemID: "c2", SnapshottedAmount: money.New(30000, "SGD")},
	}
	_, err := estimates.SumLines(lines)
	if err != estimates.ErrMixedCurrencyCostItems {
		t.Fatalf("expected ErrMixedCurrencyCostItems, got %v", err)
	}
}
