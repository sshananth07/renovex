package money

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

func TestRoundToMinorUnitsHalfUp(t *testing.T) {
	cases := []struct {
		name     string
		input    string // decimal string, in major currency units
		expected int64  // expected minor units after rounding
	}{
		{"exact value", "966.12", 96612},
		{"round up at .5 of a cent", "966.125", 96613},
		{"round down below .5 of a cent", "966.124", 96612},
		{"round up below .5 of a cent on the other side", "966.126", 96613},
		{"zero", "0", 0},
		{"negative exact", "-10.00", -1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decimal.NewFromString(tc.input)
			if err != nil {
				t.Fatalf("failed to parse decimal %q: %v", tc.input, err)
			}

			got := RoundToMinorUnits(d)
			if got != tc.expected {
				t.Fatalf("RoundToMinorUnits(%s) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestCalculateLineAmountBasicMultiplication(t *testing.T) {
	qty := decimal.RequireFromString("8.5")
	unitPrice := New(15000, "MYR") // RM150.00/day

	result := CalculateLineAmount(qty, unitPrice)

	if result.Amount != 127500 { // RM1,275.00
		t.Fatalf("expected 127500, got %d", result.Amount)
	}
	if result.Currency != "MYR" {
		t.Fatalf("expected MYR, got %s", result.Currency)
	}
}

func TestCalculateLineAmountRoundsHalfUp(t *testing.T) {
	qty := decimal.RequireFromString("3")
	unitPrice := New(1000, "MYR")

	qtyHalfCent := decimal.RequireFromString("1.005")
	unitPriceOneDollar := New(100, "MYR") // RM1.00

	result := CalculateLineAmount(qtyHalfCent, unitPriceOneDollar)
	if result.Amount != 101 { // RM1.005 -> RM1.01, ties away from zero
		t.Fatalf("expected 101 (half-up rounding), got %d", result.Amount)
	}

	// sanity: the non-tie case from the first block still holds
	resultPlain := CalculateLineAmount(qty, unitPrice)
	if resultPlain.Amount != 3000 {
		t.Fatalf("expected 3000, got %d", resultPlain.Amount)
	}
}

func TestCalculateLineAmountZeroQuantity(t *testing.T) {
	result := CalculateLineAmount(decimal.Zero, New(15000, "MYR"))
	if result.Amount != 0 {
		t.Fatalf("expected 0, got %d", result.Amount)
	}
	if result.Currency != "MYR" {
		t.Fatalf("expected MYR, got %s", result.Currency)
	}
}

func TestAddRateBPS(t *testing.T) {
	cost := New(10000, "MYR")        // RM100.00
	result := AddRateBPS(cost, 2000) // 20% markup
	if result.Amount != 12000 || result.Currency != "MYR" {
		t.Fatalf("expected 12000 MYR (20%% markup on RM100), got %+v", result)
	}
}

func TestAddRateBPSZeroRate(t *testing.T) {
	cost := New(10000, "MYR")
	result := AddRateBPS(cost, 0)
	if result.Amount != 10000 {
		t.Fatalf("expected 0%% markup to return the cost unchanged, got %+v", result)
	}
}

func TestDivideByComplementRateBPS(t *testing.T) {
	cost := New(10000, "MYR")                       // RM100.00
	result := DivideByComplementRateBPS(cost, 2000) // 20% margin
	if result.Amount != 12500 || result.Currency != "MYR" {
		t.Fatalf("expected 12500 MYR (20%% margin on RM100 = RM100/0.8 = RM125), got %+v", result)
	}
}

func TestDivideByComplementRateBPSZeroRate(t *testing.T) {
	cost := New(10000, "MYR")
	result := DivideByComplementRateBPS(cost, 0)
	if result.Amount != 10000 {
		t.Fatalf("expected 0%% margin to return the cost unchanged, got %+v", result)
	}
}

func TestRateBPSFromRatio(t *testing.T) {
	profit := New(2500, "MYR")        // RM25.00 profit
	sellingPrice := New(12500, "MYR") // RM125.00 selling price
	result := RateBPSFromRatio(profit, sellingPrice)
	if result != 2000 {
		t.Fatalf("expected 2000 bps (20%% margin, RM25/RM125), got %d", result)
	}
}

func TestRateBPSFromRatioRounds(t *testing.T) {
	// RM1 profit on RM3 selling price = 33.333...% -> rounds to 3333 bps
	profit := New(100, "MYR")
	sellingPrice := New(300, "MYR")
	result := RateBPSFromRatio(profit, sellingPrice)
	if result != 3333 {
		t.Fatalf("expected 3333 bps (33.33%%, rounded), got %d", result)
	}
}

func TestAllocateProportionallyEqualWeights(t *testing.T) {
	total := New(300, "MYR")
	shares, err := AllocateProportionally(total, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shares) != 3 {
		t.Fatalf("expected 3 shares, got %d", len(shares))
	}
	sum := int64(0)
	for _, s := range shares {
		if s.Currency != "MYR" {
			t.Fatalf("expected every share to carry total's currency MYR, got %s", s.Currency)
		}
		if s.Amount <= 0 {
			t.Fatalf("expected every share to be strictly positive, got %d", s.Amount)
		}
		sum += s.Amount
	}
	if sum != 300 {
		t.Fatalf("expected shares to sum to exactly 300, got %d", sum)
	}
}

func TestAllocateProportionallySkewedWeights(t *testing.T) {
	// A RM625.00 total split 1000:1 (WorkItem A) : 200 (WorkItem B) —
	// mirrors design spec §6.2's worked example shape.
	total := New(62500, "MYR")
	shares, err := AllocateProportionally(total, []int64{1000, 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum := int64(0)
	for _, s := range shares {
		sum += s.Amount
	}
	if sum != 62500 {
		t.Fatalf("expected shares to sum to exactly 62500, got %d", sum)
	}
	// Larger weight must receive the larger share.
	if shares[0].Amount <= shares[1].Amount {
		t.Fatalf("expected shares[0] (weight 1000) > shares[1] (weight 200), got %d and %d", shares[0].Amount, shares[1].Amount)
	}
}

func TestAllocateProportionallySingleWeight(t *testing.T) {
	total := New(999, "MYR")
	shares, err := AllocateProportionally(total, []int64{1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shares) != 1 || shares[0].Amount != 999 {
		t.Fatalf("expected the single weight to receive the entire total, got %+v", shares)
	}
}

func TestAllocateProportionallyResidualNotEvenlyDivisible(t *testing.T) {
	// 100 minor units across 3 equal weights -> 33/33/33 truncated, 1 unit
	// residual distributed by largest-remainder to exactly one share.
	total := New(100, "MYR")
	shares, err := AllocateProportionally(total, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum := int64(0)
	for _, s := range shares {
		sum += s.Amount
	}
	if sum != 100 {
		t.Fatalf("expected shares to sum to exactly 100 (no residual dropped or duplicated), got %d", sum)
	}
}

func TestAllocateProportionallyDeterministicTieBreak(t *testing.T) {
	// Running the same inputs twice must produce identical outputs — no
	// dependency on map iteration order (design spec §6.2 Step 3).
	total := New(100, "MYR")
	weights := []int64{1, 1, 1}
	first, err := AllocateProportionally(total, weights)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := AllocateProportionally(total, weights)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := range first {
		if first[i].Amount != second[i].Amount {
			t.Fatalf("expected deterministic output across repeated calls, got %v then %v", first, second)
		}
	}
}

func TestAllocateProportionallyRejectsNonPositiveTotal(t *testing.T) {
	_, err := AllocateProportionally(New(0, "MYR"), []int64{1, 1})
	if err != ErrNonPositiveAllocationTotal {
		t.Fatalf("expected ErrNonPositiveAllocationTotal for zero total, got %v", err)
	}
	_, err = AllocateProportionally(New(-100, "MYR"), []int64{1, 1})
	if err != ErrNonPositiveAllocationTotal {
		t.Fatalf("expected ErrNonPositiveAllocationTotal for negative total, got %v", err)
	}
}

func TestAllocateProportionallyRejectsEmptyWeights(t *testing.T) {
	_, err := AllocateProportionally(New(100, "MYR"), nil)
	if err != ErrNoAllocationWeights {
		t.Fatalf("expected ErrNoAllocationWeights for nil weights, got %v", err)
	}
	_, err = AllocateProportionally(New(100, "MYR"), []int64{})
	if err != ErrNoAllocationWeights {
		t.Fatalf("expected ErrNoAllocationWeights for empty weights, got %v", err)
	}
}

func TestAllocateProportionallyRejectsNonPositiveWeight(t *testing.T) {
	// A positive overall total does NOT imply every individual weight is
	// positive — design spec §6.2's corrected worked example
	// (WorkItem A: RM1,000, WorkItem B: -RM200, CostSubtotal: RM800).
	_, err := AllocateProportionally(New(800, "MYR"), []int64{1000, -200})
	if err != ErrInvalidAllocationWeight {
		t.Fatalf("expected ErrInvalidAllocationWeight for a negative weight, got %v", err)
	}
	// A zero weight among otherwise-positive weights must also be rejected
	// — the original design draft's own test matrix left this undefined;
	// the design spec's 2nd review round resolved it as rejection.
	_, err = AllocateProportionally(New(100, "MYR"), []int64{1, 0, 1})
	if err != ErrInvalidAllocationWeight {
		t.Fatalf("expected ErrInvalidAllocationWeight for a zero weight, got %v", err)
	}
}

func TestAllocateProportionallyRejectsZeroShare(t *testing.T) {
	// 10 equal weights against a 3-minor-unit total: at most 3 shares can
	// receive 1 minor unit each; the other 7 truncate to zero and can
	// never win a residual unit. The whole call must fail, not return a
	// result set containing a zero share (design spec §6.2 post-allocation
	// validation, §19 ErrZeroAllocationShare).
	weights := make([]int64, 10)
	for i := range weights {
		weights[i] = 1
	}
	_, err := AllocateProportionally(New(3, "MYR"), weights)
	if err != ErrZeroAllocationShare {
		t.Fatalf("expected ErrZeroAllocationShare when more weights than minor units exist, got %v", err)
	}
}

func TestApplyRateBPS(t *testing.T) {
	base := New(10000, "MYR")         // RM100
	result := ApplyRateBPS(base, 600) // 6%
	if result.Amount != 600 {
		t.Fatalf("expected 600 (6%% of 10000), got %d", result.Amount)
	}
	if result.Currency != "MYR" {
		t.Fatalf("expected currency MYR, got %s", result.Currency)
	}
}

func TestApplyRateBPSMatchesAddRateBPSDelta(t *testing.T) {
	base := New(62500, "MYR")
	rate := RateBPS(600)
	delta := ApplyRateBPS(base, rate)
	marked := AddRateBPS(base, rate)
	expectedDelta := marked.Amount - base.Amount
	if delta.Amount != expectedDelta {
		t.Fatalf("expected ApplyRateBPS delta (%d) to equal AddRateBPS(base,rate) - base (%d)", delta.Amount, expectedDelta)
	}
}

func TestAllocateProportionallyTieBreaksByInputOrder(t *testing.T) {
	shares, err := AllocateProportionally(
		New(4, "MYR"),
		[]int64{2, 2, 1},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := []int64{
		shares[0].Amount,
		shares[1].Amount,
		shares[2].Amount,
	}
	want := []int64{2, 1, 1}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestAllocateProportionallyRejectsWeightSumOverflow(t *testing.T) {
	weights := []int64{math.MaxInt64, math.MaxInt64}
	_, err := AllocateProportionally(New(100, "MYR"), weights)
	if err != ErrAllocationWeightSumOverflow {
		t.Fatalf("expected ErrAllocationWeightSumOverflow, got %v", err)
	}
}

func TestApplyRateBPSZeroRate(t *testing.T) {
	base := New(10000, "MYR")
	result := ApplyRateBPS(base, 0)
	if result.Amount != 0 {
		t.Fatalf("expected 0 for a zero rate, got %d", result.Amount)
	}
}
