# ADR 0001: Money and Decimal Strategy

## Status
Accepted

## Context
Financial correctness is a hard requirement (phase1.md §51). Binary
floating-point (float32/float64) cannot represent decimal currency amounts
exactly and must not be used for authoritative financial or quantity
calculations.

## Decision
- Canonical Money is `int64` minor units + currency code
  (`internal/foundation/money.Money`), e.g. RM18.50 = `{1850, "MYR"}`.
- Rates/percentages are fixed-point `RateBPS` (basis points), not floats.
- Construction quantities use `shopspring/decimal`
  (`internal/foundation/quantity.Quantity`), not `float64`.
- Calculation flow: decimal Quantity × Money unit price (as decimal) →
  precise decimal intermediate result → centralized rounding
  (`money.RoundToMinorUnits`, round-half-up to the nearest minor unit) →
  final `int64` Money.
- MongoDB Decimal128 is not used as the default Money representation in
  Phase 1.
- Money operations validate currency compatibility (adding MYR to SGD is
  an error).

## Consequences
- All modules share one rounding implementation; no per-module rounding
  logic.
- Display/formatting layers (API responses, frontend) are responsible for
  converting minor units to a human-readable major-unit string; that
  conversion is presentation-only and never feeds back into authoritative
  calculations.
