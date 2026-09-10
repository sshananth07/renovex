// Package procurementcalc holds the pure calculation kernel shared by supplier
// quotation (M8 Phase E) and award recalculation (M8 Phase F).
//
// # Why this package exists
//
// Phase E calculates a Supplier's submitted Offer total. Phase F recalculates
// an authoritative Award total over the subset of lines a contractor actually
// selected. Those two figures must agree line for line where the selection is
// complete — a submitted Offer total that disagrees with the Award total for
// the same lines is a commercial correctness defect, not a cosmetic one.
//
// The two consumers live in different domain modules that may not import each
// other (ADR 0002), so the arithmetic they share cannot live in either. It
// lives here, and both call the same functions with the same rounding.
//
// # Boundary
//
// This package is domain-neutral business arithmetic. It depends only on
// internal/foundation/money and knows nothing of supplieroffers, awards,
// MongoDB, HTTP, repositories, audit or mail. It holds calculation RULES and
// INPUTS only — never a persisted aggregate. SupplierOfferVersion,
// SupplierOfferDraft and AwardRevision stay in the modules that own them, and
// each maps its own persisted shape into these rule types before calculating.
//
// # Rounding
//
// All rounding goes through money.RoundToMinorUnits (ADR 0001). No rounding is
// reimplemented here, and none may be reimplemented by a caller.
package procurementcalc
