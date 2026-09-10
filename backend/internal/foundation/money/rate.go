package money

// RateBPS represents a fixed-point percentage rate expressed in basis
// points (1% = 100 bps). Authoritative rate/percentage calculations
// (tax, markup, margin) must use RateBPS, never a floating-point percentage.
type RateBPS int64

// BasisPointsDenominator is the divisor representing 100% in basis points.
const BasisPointsDenominator = 10000
