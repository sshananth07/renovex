package money

import (
	"errors"
	"sort"

	"github.com/shopspring/decimal"
)

// RoundToMinorUnits applies the platform's single, centralized rounding
// policy for converting a precise decimal major-currency-unit amount into
// authoritative int64 minor units: round-half-up (ties away from zero) to
// the nearest minor unit.
//
// This is the only place authoritative monetary rounding may happen.
// Domain modules must call this function rather than reimplementing
// rounding logic.
func RoundToMinorUnits(amountMajorUnits decimal.Decimal) int64 {
	return roundHalfUpToMinorUnits(amountMajorUnits)
}

// CalculateLineAmount computes qty × unitPrice, rounding once via
// RoundToMinorUnits. This is the only supported multiplication path for
// quantity-based cost/labour calculations across the platform — see ADR
// 0001 and the M3 design spec §1.3.1. No module may reimplement this
// multiplication+rounding sequence itself.
func CalculateLineAmount(qty decimal.Decimal, unitPrice Money) Money {
	unitPriceMajorUnits := decimal.NewFromInt(unitPrice.Amount).Shift(-2)
	amountMajorUnits := qty.Mul(unitPriceMajorUnits)
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: unitPrice.Currency}
}

// AddRateBPS computes base × (1 + rate/10000), rounded once via
// RoundToMinorUnits. The markup selling-price formula (M4 design spec §13).
// Callers are responsible for rejecting negative rates before calling —
// this function performs no validation of its own, matching
// CalculateLineAmount's precedent of leaving domain validation to its caller.
func AddRateBPS(base Money, rate RateBPS) Money {
	baseMajorUnits := decimal.NewFromInt(base.Amount).Shift(-2)
	rateFraction := decimal.NewFromInt(int64(rate)).Div(decimal.NewFromInt(BasisPointsDenominator))
	amountMajorUnits := baseMajorUnits.Mul(decimal.NewFromInt(1).Add(rateFraction))
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: base.Currency}
}

// DivideByComplementRateBPS computes base / (1 - rate/10000), rounded once.
// The margin selling-price formula (M4 design spec §13). Callers MUST
// validate 0 <= rate < BasisPointsDenominator before calling — this
// function does not itself guard against rate >= BasisPointsDenominator
// (which would produce a zero or negative divisor); that validation is the
// caller's domain responsibility, matching CalculateLineAmount's precedent.
func DivideByComplementRateBPS(base Money, rate RateBPS) Money {
	baseMajorUnits := decimal.NewFromInt(base.Amount).Shift(-2)
	complement := decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(rate)).Div(decimal.NewFromInt(BasisPointsDenominator)))
	amountMajorUnits := baseMajorUnits.Div(complement)
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: base.Currency}
}

// RateBPSFromRatio computes (numerator/denominator) × 10000, rounded to the
// nearest whole basis point. Used to derive ProjectedGrossMarginBPS from
// ProjectedGrossProfit and ProposedSellingPrice (M4 design spec §15).
// numerator and denominator must share a currency; RateBPSFromRatio does
// not itself validate this (mirrors Money.Add/Subtract's own currency-check
// responsibility living on the caller side when the two values originate
// from the same already-currency-consistent Estimate).
func RateBPSFromRatio(numerator, denominator Money) RateBPS {
	num := decimal.NewFromInt(numerator.Amount)
	den := decimal.NewFromInt(denominator.Amount)
	ratio := num.Div(den).Mul(decimal.NewFromInt(BasisPointsDenominator))
	return RateBPS(ratio.Round(0).IntPart())
}

// ErrNonPositiveAllocationTotal is returned by AllocateProportionally when
// total.Amount <= 0.
var ErrNonPositiveAllocationTotal = errors.New("money: allocation total must be strictly positive")

// ErrNoAllocationWeights is returned by AllocateProportionally when weights
// is empty.
var ErrNoAllocationWeights = errors.New("money: at least one weight is required")

// ErrInvalidAllocationWeight is returned by AllocateProportionally when any
// individual weight is <= 0. A positive sum of weights does NOT imply every
// individual weight is positive (M5 design spec §6.2) — callers must not
// pass non-positive weights; this function rejects them explicitly rather
// than silently dropping or clamping them.
var ErrInvalidAllocationWeight = errors.New("money: every weight must be strictly positive")

// ErrZeroAllocationShare is returned by AllocateProportionally when the
// largest-remainder algorithm would produce a zero-minor-unit share for at
// least one weight even after residual distribution — possible when
// len(weights) is large relative to total's minor-unit count, or one
// weight is a vanishingly small fraction of the sum of weights (M5 design
// spec §6.2). The entire call fails; no partial/best-effort result with a
// zero share is ever returned.
var ErrZeroAllocationShare = errors.New("money: allocation would produce a zero share for at least one weight")

// ErrAllocationWeightSumOverflow is returned by AllocateProportionally when
// summing weights would overflow int64. Every individual weight is already
// bounded by a single Money.Amount (also int64), but a Project with enough
// WorkItems could in principle overflow the running sum; this is checked
// explicitly rather than left to wrap silently.
var ErrAllocationWeightSumOverflow = errors.New("money: sum of allocation weights overflows int64")

// AllocateProportionally splits total across len(weights) shares,
// proportional to each weight, using the largest-remainder method so the
// shares sum to EXACTLY total (never more, never less) — no residual is
// ever silently dropped or double-counted. total's Currency is copied onto
// every returned share. Ties in the remainder-ranking step are broken by
// weights' input index (deterministic, reproducible).
//
// Validates: total.Amount > 0, len(weights) > 0, every weights[i] > 0, and
// every resulting share > 0 — returning ErrNonPositiveAllocationTotal /
// ErrNoAllocationWeights / ErrInvalidAllocationWeight /
// ErrZeroAllocationShare respectively (M5 design spec §6.2/§19).
func AllocateProportionally(total Money, weights []int64) ([]Money, error) {
	if total.Amount <= 0 {
		return nil, ErrNonPositiveAllocationTotal
	}
	if len(weights) == 0 {
		return nil, ErrNoAllocationWeights
	}

	sumWeights := int64(0)
	for _, w := range weights {
		if w <= 0 {
			return nil, ErrInvalidAllocationWeight
		}
		next := sumWeights + w
		if next < sumWeights {
			return nil, ErrAllocationWeightSumOverflow
		}
		sumWeights = next
	}

	totalMajorUnits := decimal.NewFromInt(total.Amount)
	sumWeightsDecimal := decimal.NewFromInt(sumWeights)

	rawShares := make([]decimal.Decimal, len(weights))
	truncatedShares := make([]int64, len(weights))
	remainders := make([]decimal.Decimal, len(weights))
	truncatedSum := int64(0)

	for i, w := range weights {
		raw := totalMajorUnits.Mul(decimal.NewFromInt(w)).Div(sumWeightsDecimal)
		rawShares[i] = raw
		truncated := raw.Floor()
		truncatedShares[i] = truncated.IntPart()
		remainders[i] = raw.Sub(truncated)
		truncatedSum += truncatedShares[i]
	}

	residual := total.Amount - truncatedSum

	// Distribute the residual one minor unit at a time to the shares with
	// the largest fractional remainder, breaking ties by input index.
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	// sort.SliceStable is required (not a swap-based selection sort): a
	// stable sort preserves the original index order among equal
	// remainders, since order starts as [0,1,2,...]. A swap-based
	// selection sort is NOT stable — relocating one tied element can
	// reorder the remaining ties among themselves.
	sort.SliceStable(order, func(a, b int) bool {
		return remainders[order[a]].GreaterThan(remainders[order[b]])
	})

	finalShares := make([]int64, len(weights))
	copy(finalShares, truncatedShares)
	for i := int64(0); i < residual; i++ {
		finalShares[order[i]]++
	}

	result := make([]Money, len(weights))
	for i, amount := range finalShares {
		if amount <= 0 {
			return nil, ErrZeroAllocationShare
		}
		result[i] = Money{Amount: amount, Currency: total.Currency}
	}

	return result, nil
}

// ApplyRateBPS computes base × rate/10000, rounded once via
// RoundToMinorUnits — used for tax-amount calculation (M5 design spec §7).
// Distinct from AddRateBPS (which returns base PLUS the rate-computed
// delta, the markup formula): ApplyRateBPS returns ONLY the delta itself.
func ApplyRateBPS(base Money, rate RateBPS) Money {
	baseMajorUnits := decimal.NewFromInt(base.Amount).Shift(-2)
	rateFraction := decimal.NewFromInt(int64(rate)).Div(decimal.NewFromInt(BasisPointsDenominator))
	amountMajorUnits := baseMajorUnits.Mul(rateFraction)
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: base.Currency}
}

func roundHalfUpToMinorUnits(amountMajorUnits decimal.Decimal) int64 {
	scaled := amountMajorUnits.Shift(2) // move 2 decimal places -> minor units
	half := decimal.NewFromFloat(0.5)

	if scaled.Sign() >= 0 {
		return scaled.Add(half).Floor().IntPart()
	}
	// Mirror the positive-side rule for negatives: round the absolute
	// value half-up, then reapply the sign, so -10.005 rounds to -1001
	// rather than -1000 (ties away from zero in both directions).
	absScaled := scaled.Neg()
	roundedAbs := absScaled.Add(half).Floor().IntPart()
	return -roundedAbs
}
