// Package awards owns offer-comparison projections, provisional line
// selections, immutable award revisions, outcome notifications and Supplier
// receipt acknowledgement (M8 design spec §2, §8, §9).
//
// # Module boundary
//
// This package never imports rfqissuance, supplieraccess or supplieroffers, and
// none of them imports it. It reads issued RFQ versions and submitted offer
// versions through consumer-owned capability interfaces satisfied in the
// composition root — never by reading another module's collection.
//
// # Immutability
//
// Finalising a provisional draft creates an immutable Award Revision. Changing
// a finalised award never mutates it: a new revision supersedes the previous
// one and records a required change reason. Prior revisions and the
// notifications sent against them remain exactly as they were.
//
// # Comparison is transparent, not automated
//
// The contractor sorts and filters; the platform ranks nothing and recommends
// no winner. There is no scoring model and no AI recommendation here.
//
// # One line, one Supplier
//
// Different RFQ lines may be awarded to different Suppliers, but an individual
// line is awarded in full to exactly one Supplier or left explicitly unawarded
// with a reason. M8 does not split one line's quantity.
//
// # Award is not commitment
//
// An award records the contractor's sourcing decision only. It does not create
// a Purchase Order, contractual acceptance or committed cost, and it does not
// allocate anything into the cost ledger. A selected Supplier acknowledges
// RECEIPT only — acknowledgement is explicitly not acceptance of a Purchase
// Order. Purchase Orders, acceptance, decline and change requests belong to the
// next milestone.
//
// # No email without an explicit action
//
// Finalising an award sends nothing. Both selected and unsuccessful Suppliers
// are notified only through a separate explicit contractor action, and no
// Supplier ever sees competitor names, prices, rankings, comparison notes or
// internal decision reasoning.
package awards
