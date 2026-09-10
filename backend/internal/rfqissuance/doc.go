// Package rfqissuance owns the immutable issued RFQ version chain, RFQ
// amendment drafts, stable Supplier Invitations, and invitation delivery
// intent (M8 design spec §2, §4, §5).
//
// It is the first of M8's four modules and the only one that touches M7. The
// conceptual M8 flow is one-way:
//
//	rfqissuance -> supplieraccess -> supplieroffers -> awards
//
// That flow is NOT import permission. Every cross-module dependency is a
// consumer-owned capability interface satisfied structurally, or through a
// composition adapter where Go's exact-return-type rule forces a conversion.
// This package therefore never imports supplieraccess, supplieroffers or
// awards, and none of them imports it.
//
// # Immutability
//
// An issued RFQ version is immutable. It is never edited, never re-snapshotted,
// and never partially rewritten. A correction — including a deadline-only
// extension — creates an amendment draft that is issued as a NEW immutable
// version. This is what lets a Supplier keep reading the exact version they
// were invited to while the contractor prepares the next one.
//
// # M7 boundary
//
// This package never writes to rfqs, rfq_counters, material_requirements or any
// Supplier Directory record. It reads the M7 ready RFQ snapshot through an
// M8-owned capability satisfied by a composition adapter (design spec §2.1), so
// it imports no M7 type at all.
//
// The reverse direction is equally guarded: rfqs learns that a chain has been
// issued through its OWN IssuanceStatusSource interface, wired by setter in the
// composition root (design spec §1A.3). rfqs does not import this package.
//
// # Supplier-visible allowlist
//
// An issued version carries only supplier-visible fields. Contractor internal
// notes, estimated cost, indicative offering price, margin, preferred-Supplier
// data and competing-Supplier information are structurally absent from the
// snapshot types, so they cannot leak even by mistake.
//
// # Invitation secrets
//
// Invitation secrets are DERIVED, never stored. See platform/secrets and design
// spec §6.1A: only the hash, access generation and key version are persisted,
// which is what lets copy-link reproduce the current link without rotating it.
package rfqissuance
