// Package supplieraccess owns invitation-token exchange, one-time email
// verification challenges, restricted Supplier sessions and access revocation
// (M8 design spec §2, §6).
//
// # Module boundary
//
// This package never imports rfqissuance, supplieroffers or awards, and none of
// them imports it. It reaches an invitation only through consumer-owned
// capability interfaces satisfied in the composition root — never by reading
// another module's collection.
//
// One of those capabilities is InvitationViewRecorder (design spec §6.5). This
// package observes a successful secure-link open, but rfqissuance owns the
// invitation record, so the timestamp write travels back through a narrow
// primitive-only interface that includes the access generation. Without the
// generation term, a validated old-generation request racing a recipient
// replacement could mark the replacement recipient's current invitation as
// viewed.
//
// # Restricted access
//
// A Supplier never receives an account on the contractor's application. They
// receive a scoped session bound to:
//
//	Company + Supplier + normalised recipient email
//
// A session carries a 30-day sliding inactivity expiry and a 90-day absolute
// re-verification limit. Activity renews only the sliding expiry; after the
// absolute limit, email verification is required again.
//
// # Never persisted, never logged
//
// Raw invitation secrets, verification codes and session tokens exist only in
// transit. Only their hashes are stored, and none of them is ever written to a
// log line or an audit record.
//
// # Non-disclosure
//
// Public responses never reveal whether a Supplier, recipient email, invitation
// or challenge exists. Every failure mode — unknown token, wrong generation,
// revoked invitation, expired invitation, consumed challenge — collapses to one
// indistinguishable error, following the M6 external-access precedent.
//
// # Derived tenancy
//
// A Supplier request never supplies an authoritative CompanyID or SupplierID.
// Both are derived from the verified session and invitation, so neither can be
// forged by a caller.
package supplieraccess
