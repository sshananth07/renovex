// Package approvals owns the general Approval model (e.g. Client Quotation
// acceptance) that later extends to Architect/Engineer/Compliance review.
// See phase1.md §30-31.
//
// This module is deliberately SUBJECT-AGNOSTIC: it knows nothing about
// Quotation chains, versions, supersession, or access grants. It stores and
// conditionally mutates exactly one Approval aggregate per exact subject,
// enforcing only the subject-local transition rules (acceptance is terminal;
// non-terminal states may transition freely under a Revision guard).
//
// All chain-level coordination — which version may be accepted, and the
// mutual exclusion between acceptance and supersession — is orchestrated by
// internal/access via its AccessGroupState coordinator. Keeping that logic
// out of this package is what lets a future non-versioned subject type
// (e.g. a Blueprint approval) reuse it unchanged.
//
// Implemented in Milestone 6.
package approvals
