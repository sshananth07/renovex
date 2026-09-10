// Package quotations owns the customer-facing Quotation: a versioned,
// snapshotted selling-price document generated from a finalized Estimate.
// See phase1.md §23-31 and
// docs/superpowers/specs/2026-07-24-milestone-5-quotations-design.md.
//
// quotations never imports internal/estimates, internal/work,
// internal/projects, or internal/clients by type (ADR 0002). It defines
// ProjectLookup, WorkItemLookup, and FinalizedEstimateSource — the three
// capabilities it needs from those modules — satisfied structurally.
//
// Client-facing secure links, Access Grants, and Client accept/reject
// actions are Milestone 6, not built here — see design spec §24.
package quotations
