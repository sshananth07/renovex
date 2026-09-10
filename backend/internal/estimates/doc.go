// Package estimates owns Estimate — the internal contractor's private
// financial calculation (cost snapshot, markup/margin pricing, proposed
// selling price, projected profit) built from the M3 cost_items ledger.
// See phase1.md §20-22 and
// docs/superpowers/specs/2026-07-23-milestone-4-estimates-design.md.
//
// An Estimate is versioned: each Version is its own immutable-once-finalized
// document. Exactly one draft may exist per Project at a time. A draft is a
// genuinely mutable working document — refresh (re-pull cost_items) and
// pricing recalculation both mutate it in place, guarded by a Revision
// optimistic-concurrency counter distinct from the business-meaningful
// Version number. Creating the next Version requires the source Estimate to
// already be finalized.
//
// estimates never imports projects or costs by type. It defines
// ProjectLookup and EstimatedCostSource, the two capabilities it needs,
// satisfied structurally by projects.Service and costs.Service respectively.
package estimates
