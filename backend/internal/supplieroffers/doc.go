// Package supplieroffers owns Supplier Offer drafts, immutable submitted offer
// versions, withdrawals, quoted tax, structured conditional charges and
// copy-forward (M8 design spec §2, §7).
//
// # Module boundary
//
// This package never imports rfqissuance, supplieraccess or awards, and none of
// them imports it. It reads the issued RFQ version and the verified recipient
// identity through consumer-owned capability interfaces satisfied in the
// composition root — never by reading another module's collection.
//
// # Immutability
//
// At most one active draft exists per offer chain (Invitation + Issued RFQ
// Version). Submitting turns that draft into an immutable offer version and
// archives the draft. A submitted version is never edited: a revision creates a
// new draft and, on submission, another immutable version. Withdrawal is a
// separate immutable record that leaves the submitted version untouched and
// merely makes it ineligible for selection.
//
// # The server calculates
//
// Browser-supplied totals are ignored. The server recalculates every line
// subtotal, tax amount, charge and grand total at submission. All arithmetic
// uses foundation/money.Money (int64 minor units), foundation/quantity exact
// decimals and the centralised rounding helpers. Floating point is forbidden
// (ADR 0001).
//
// # Explicit line coverage
//
// Every issued RFQ line must carry exactly one response: quoted, no_bid or
// unavailable. Offers may be partial in the sense that lines may be declined,
// but no line may be left unanswered. A quoted line must cover the full
// requested quantity in the exact requested unit — M8 does not support quoting
// a partial quantity.
//
// # Quoted tax is an estimate
//
// Tax recorded here is the Supplier's QUOTED or ESTIMATED tax, captured for
// commercial comparison. The platform does not determine whether a Supplier is
// legally taxable; a later invoice or validated e-Invoice remains authoritative.
//
// # Charges are Supplier-defined
//
// Conditional charge rules are structured data the Supplier supplies. The
// platform never invents, allocates or prorates a charge. Overlapping rules
// that could double-charge one selection are rejected at submission rather than
// resolved by guesswork.
package supplieroffers
