// Package access owns Access Grants: scoped, token-based external access
// for Clients (Quotation review), independent of Company Membership.
// See phase1.md §2, §57.
//
// It owns two collections exclusively:
//
//   - access_grants:       one document per issued secure link. Multiple
//     historical grants may exist for the same Quotation version (one per
//     share/rotation) — there is no per-resource uniqueness constraint.
//   - access_group_states: one coordinator document per commercial chain
//     (one QuotationNumber). This is the single serialization point for
//     every cross-grant race in M6 — share vs. share, share vs. accept,
//     accept vs. accept, and rotation — and it, not AccessGrant.Status
//     alone, is the authority on whether a grant is externally usable.
//
// Implemented in Milestone 6 (Client access). Supplier RFQ access reuses
// the same grant/coordinator shape in a later milestone.
package access
