# Milestone 4 — Estimates: Design Spec

**Status:** APPROVED — all 7 first-round review decisions, 3 second-round
corrections, and 2 third-round corrections resolved by explicit user
direction (recorded in §32). Ready for implementation.

**Scope:** `internal/estimates` — the internal contractor Estimate aggregate,
snapshot/versioning model, pricing calculation (markup/margin, proposed
selling price, projected profit), and tenant-safe CRUD/version operations,
consuming the M3 `cost_items` ledger through a new narrow capability on
`internal/costs`.

**Authorities:** `phase1.md` §19-22 (Cost Model, Internal Estimate, AI
Integration in Estimation, Profitability Preview) plus §24 (Internal vs
Client boundary), §27 (Quotation Versioning — the closest existing precedent
for *any* versioning semantics in phase1.md), §51 (Financial Data Integrity),
§54 (Optimistic Concurrency), §55 (collection list — `estimates` is listed,
no `estimate_items`/`estimate_lines`); ADR 0001 (money/decimal — authoritative,
not re-litigated); ADR 0002 (module boundaries); the finalized M3 design spec
and its real implementation in `internal/costs`, `internal/work`,
`internal/foundation/money`.

**Explicitly out of scope for M4** (per task brief — these belong to M5+):
customer-facing Quotation, Quotation versions, Client approval, Client secure
links, Access Grants, RFQs, Supplier Offers, procurement workflows, Material
Requirements, Purchase Orders, supplier invoice workflows, customer payment
tracking, full project profitability dashboard, cash-flow dashboard, AI
quotation generation, AI estimate generation, AWS infrastructure.

**Also explicitly out of scope for M4, per review decision (§32-6):**
contingency (`ContingencyRate`/`ContingencyAmount`/`AdjustedCost`). Contingency
does not appear anywhere in phase1.md; it is deferred entirely, not
approximated. Pricing (markup/margin) is applied directly to `CostSubtotal`.

**Revision note:** this spec went through two review rounds (recorded in
§32). The first round's seven decisions: drafts are now genuinely mutable
working documents (a `refresh` operation re-pulls live `cost_items` in
place, without bumping `Version`); exactly one draft may exist per Project
at a time (enforced by a partial unique index); a `Revision` field is added
for optimistic concurrency on draft mutations; contingency is removed from
M4 entirely; and a dedicated "latest Estimate" lookup is added alongside
the full version-history list. The second round's three corrections:
`FinalizeEstimate` is now also `Revision`-guarded (it was the one draft
mutation missing the guard, creating a stale-numbers-get-finalized hazard);
`CreateNewVersion` now explicitly requires a `finalized` source as a
first-checked precondition, rather than relying on the one-draft partial
index to reject a draft-sourced attempt after the fact; and phase1.md
§20's manual price/quantity override requirement is now given an actual
mechanism (price override via the existing M3 lifecycle-update endpoint
plus M4's `refresh`; quantity override explicitly named as a deferred M3
gap, not an M4 responsibility). The third round's two corrections, caught
during implementation planning: `CreateEstimate` must never share
`CreateNewVersion`'s retrying version-allocation strategy — it now
attempts Version 1 directly, exactly once, with no retry under any
circumstance (§21 is now split into §21.1/§21.2 for the two operations'
genuinely different concurrency contracts); and all four MongoDB indexes
now require explicit, mutually non-overlapping names, since two of them
share a key pattern that would otherwise collide on MongoDB's default
auto-generated name and, separately, would have made one index's default
name a substring of the other's, misclassifying every version conflict as
a draft collision. This document contains only the final, approved
decisions — the original review discussion is preserved in §32 for
traceability.

---

## 0. What phase1.md actually specifies about Estimates — and what it does not

This section exists because the task brief repeatedly says "derive the exact
X from phase1.md," and for several X's **phase1.md does not contain enough
information to derive an exact answer**. Naming this precisely, once, up
front, avoids re-litigating it in every later section.

**What phase1.md §20 actually says, verbatim in substance:**
- An Estimate is an internal contractor record, not the same entity as a
  Quotation (§20).
- It contains: Work Items, Resources, Material/Labour/Subcontractor/
  Equipment/Other costs, Markup, Margin calculations, Proposed selling price
  (§20).
- The contractor may manually override prices and quantities; the system
  "assists... but does not lock them into calculated values" (§20).
  **Resolved (second review round): price override is satisfied by the
  existing M3 `PATCH /cost-items/{id}/lifecycle` endpoint plus M4's
  `refresh` operation; quantity override is an explicitly deferred M3 gap,
  not an M4 mechanism — see §17.1.**
- It is private — not accessible through Client/Supplier Access Grants (§20).
- §21 (AI Integration): the Estimation Engine — not the AI — calculates
  material quantities/costs, labour costs, subcontractor costs, equipment
  costs, transport, other costs, markup, margin, proposed selling price.
- §22 (Profitability Preview): before generating a Quotation, the contractor
  sees Selling Price, Estimated Project Cost, Expected Profit, Profit Margin
  — a *category-level* breakdown example (Materials/Labour/Subcontractors/
  Equipment/Transport/Other), not a line-item-per-CostItem example.
- §55's collection list includes `estimates`, not `estimate_items` or any
  other estimate-scoped child collection.

**What phase1.md does NOT specify, and where this spec (with the user's
explicit review decisions, §32) settled on a design choice rather than a
"derived" one:**
- Whether an Estimate is versioned as one-document-per-version or one
  document with embedded version history. (§27 describes Quotation
  versioning only, and even there only at the UX level — "QT-0001 V1 /
  QT-0001 V2" — not the persistence shape.) **Resolved: one document per
  version (§32-1), approved.**
- Whether Estimate snapshot granularity is per-CostItem, per-category, or
  per-WorkItem. §22's example is category-level; §20's description list is
  also category-level ("Material costs, Labour costs..."); nothing in
  phase1.md shows a line-item-per-CostItem Estimate example. **Resolved:
  per-CostItem (§5).**
- Whether markup and margin are both supported, or how a contractor chooses
  between them. §20 lists "Markup" and "Margin calculations" as two separate
  bullet points with no worked distinction between them. **Resolved: both
  supported, mutually exclusive `PricingMode` per version (§32-5),
  approved.**
- Whether contingency is a Phase 1 concept at all. **Contingency does not
  appear anywhere in phase1.md** — not in §19-22, not in §41, not in §55's
  collection/field lists. **Resolved: removed from M4 entirely (§32-6).**
- Whether pricing is applied per-line, per-category, or once at the
  whole-Estimate level. **Resolved: whole-Estimate level (§32-5), approved.**
- Draft/finalized lifecycle mechanics, refresh vs. new-version semantics,
  concurrency strategy — none of this is in phase1.md; it is standard
  system design a contractor platform needs. **Resolved: drafts are mutable
  working documents; refresh operates in place; new versions are reserved
  for post-finalization commercial revisions (§32-2); exactly one draft per
  Project (§32-3).**

Every section below marks its claims as either "phase1.md states X"
(quoting/citing) or "phase1.md is silent; the review round settled on X for
the reasons given" — never blurring the two.

---

## 1. Estimate domain model

```go
package estimates

type EstimateStatus string

const (
    EstimateStatusDraft     EstimateStatus = "draft"
    EstimateStatusFinalized EstimateStatus = "finalized"
)

type PricingMode string

const (
    PricingModeMarkup PricingMode = "markup"
    PricingModeMargin PricingMode = "margin"
)

type Estimate struct {
    ID            string
    CompanyID     string
    ProjectID     string
    Version       int             // 1, 2, 3... — a MEANINGFUL commercial revision number, see §4
    Status        EstimateStatus  // draft | finalized — see §10
    Revision      int64           // optimistic-concurrency counter for a mutable draft — see §16.1. Meaningless/frozen once finalized.

    Currency      string          // this Estimate's canonical currency — see §6

    Lines         []EstimateCostLine // snapshot lines — see §5

    CostSubtotal  money.Money     // sum of all Lines' SnapshottedAmount
    ExcludedCostItemCount int     // count of CostItems under this Project with no Estimated value at snapshot time — see §7

    PricingMode   PricingMode     // markup | margin — see §13
    PricingRate   money.RateBPS   // meaning depends on PricingMode

    ProposedSellingPrice    money.Money
    ProjectedGrossProfit    money.Money   // ProposedSellingPrice - CostSubtotal
    ProjectedGrossMarginBPS money.RateBPS // ProjectedGrossProfit / ProposedSellingPrice, in bps

    CreatedAt     time.Time  // when this Version was first created (draft born)
    RefreshedAt   *time.Time // nil until the first refresh; updated on every subsequent refresh (§16.2)
    FinalizedAt   *time.Time // nil until finalized — see §10
    SchemaVersion int
}

type EstimateCostLine struct {
    SourceCostItemID string   // traceability only — see §5
    WorkItemID       *string  // copied from the source CostItem at snapshot time
    Category         string   // copied from the source CostItem at snapshot time (costs.CostCategory as string — see §12 on the import boundary)
    Description      string   // copied from the source CostItem at snapshot time
    SnapshottedAmount money.Money // the CostItem.Estimated value AT SNAPSHOT/REFRESH TIME — authoritative for this Estimate version, never re-read live between refreshes
}
```

All fields directly on one document; no separate `estimate_items` collection
(§5, §55 — matches the M3/M2 precedent of embedding small bounded per-parent
detail: `WorkItem.Quantity`, `CostItem.Quantity` are embedded, not referenced
collections; an Estimate's lines are exactly this shape — small, bounded by
the Project's CostItem count, always read together with the Estimate).

`SchemaVersion` (document schema, §52), `Version` (the Estimate's own
business-meaningful revision number, §4), and `Revision` (optimistic
concurrency counter, §16.1) are three **distinct** int fields with three
distinct meanings — a deliberate naming separation adopted per the review
round (§32-2), since conflating any two of these (as phase1.md's own field
lists risk doing, using "version" for both a Quotation revision, §27, and a
generic optimistic-concurrency counter, §54) would make it impossible to
answer "did the commercial numbers change" (`Version`) independently of
"did *anything* change, including a same-version refresh" (`Revision`)
independently of "did the document's on-disk shape change" (`SchemaVersion`).

---

## 2. Collection ownership

| Module | Collection | Owns exclusively |
|---|---|---|
| `estimates` | `estimates` | Yes |

No `estimate_items`/`estimate_lines` collection — matches phase1.md §55
exactly (only `estimates` is listed) and the M2/M3 precedent of embedding
small bounded child data (§1 above).

---

## 3. Source of truth — snapshot, refreshed only by explicit action

**Core invariant, revised per the review round (§32-2) from the original
draft's stricter "snapshot is fully immutable until a new version"
position:** an Estimate's cost snapshot (`Lines`/`CostSubtotal`) is never
recalculated automatically and never silently changes on GET — but while an
Estimate is a `draft`, the contractor may explicitly ask it to re-pull
current `cost_items` via `refresh`, which updates the snapshot **in place**,
without creating a new `Version`.

```
Current cost_items
  → explicit Estimate creation (POST /estimates)
  → snapshot cost inputs (CostItem.Estimated values, at that instant)
  → calculate Estimate totals
  → persist as Version 1, Status=draft

While Status == draft:
  → POST /estimates/{id}/refresh may re-pull current cost_items,
    replacing Lines/CostSubtotal/ExcludedCostItemCount IN PLACE,
    same Version, Revision incremented (§16.1/§16.2)
  → PATCH /estimates/{id}/pricing may recalculate pricing fields from the
    CURRENTLY STORED Lines/CostSubtotal, same Version, Revision incremented
  → contractor may call refresh and pricing recalculation any number of
    times, in any order, before finalizing

POST /estimates/{id}/finalize
  → Status becomes finalized
  → every field, including Lines/CostSubtotal/pricing, is now permanently
    frozen — no further refresh, pricing recalculation, or any other
    mutation of this document is possible

After finalization, later CostItem changes
  → this finalized Estimate version remains unchanged forever

A new commercial revision is needed
  → POST /estimates/{id}/versions creates the NEXT Version as a new draft,
    itself refreshable/repriceable before its own finalization
```

No GET recalculates from live `cost_items`. No `Material.ReferencePrice` or
`Worker.DefaultRate` is ever consulted when reading or recalculating an
existing Estimate — those only ever influenced the `CostItem.Estimated`
value that refresh reads.

**Resolved operations (see §4/§16/§17 for full detail):**
- First snapshot: `POST /estimates` (creates Version 1, always a `draft`).
- Refresh: `POST /estimates/{id}/refresh` — draft-only, re-reads
  `cost_items`, replaces `Lines`/`CostSubtotal`/`ExcludedCostItemCount` in
  place, same `Version`, `Revision` incremented (§16.2).
- Pricing recalculation: `PATCH /estimates/{id}/pricing` — draft-only,
  recomputes pricing fields from the currently-stored `CostSubtotal`, same
  `Version`, `Revision` incremented (§16.1).
- New version: `POST /estimates/{id}/versions` (§17) — creates the next
  `Version` as a fresh `draft`, typically used once the prior `Version` is
  already `finalized` (a genuine new commercial revision), but not
  technically restricted to that case (see §17).
- Finalized versions are immutable in every field, including pricing and
  `Lines` (§10) — the only way to change a finalized Estimate's numbers is
  to create a new version.
- Exactly one draft per Project at a time — enforced by a partial unique
  index (§20, §32-3).

---

## 4. Estimate versioning — persistence model and numbering

**Decision: one document per version** (§32-1, approved as originally
proposed, with one wording correction from the review: drafts are mutable
in place via refresh/pricing-recalculation, §3 — "one document per version"
does not mean "immutable from creation," only that a `Version` number is
never reused across documents and a `finalized` document never changes
again). Justified against the actual alternative:

- Embedding all versions in one document means every new version rewrites
  and grows an ever-larger single document, works against MongoDB's
  document-growth guidance (phase1.md §5 warns against exactly this pattern
  for Projects: "Do not embed all Work Items, Payments, Costs... into one
  large MongoDB document" — the same reasoning applies to an Estimate
  accumulating versions over a long project).
- A finalized version must never change (§10) — a single mutable document
  holding "the finalized versions" as an array makes an accidental in-place
  edit of historical data structurally possible; separate documents make it
  structurally impossible (there is no code path that opens version 1's
  document and writes to it once version 2 exists, because version 1's `_id`
  is simply never targeted by any write endpoint again once finalized).
- M5 Quotation will need to reference one specific, stable Estimate version
  — a document-per-version model gives a natural, permanently stable
  `estimateId` to reference; an embedded-array model would require M5 to
  store `(estimateId, versionNumber)` pairs instead, adding a compound key
  where a single ID would do.
- Concurrent version creation is naturally handled via a unique index on
  separate documents; enforcing uniqueness *within* one array field requires
  application-level locking on the single parent document instead (a
  strictly worse concurrency story, not a better one).

**Version numbering — now a business-meaningful commercial revision
counter, not a working-draft counter (§32-2):** integers starting at 1,
monotonically increasing per `{companyId, projectId}`. Because refresh and
pricing recalculation no longer bump `Version` (§3, §16), `Version` only
advances when `POST /estimates/{id}/versions` is called — typically once
per genuine commercial re-quote, not once per intermediate working change.

**"Latest version" determination**: two distinct concepts, both needed —
"the current draft, if one exists" (via the partial unique
`{companyId, projectId}` index on `status: draft`, §20, §32-3 — trivially at
most one document) and "the latest version overall regardless of status"
(`MAX(version)` for `{companyId, projectId}`, §20). A dedicated
`GET /estimates/latest?projectId=...` endpoint (§32-7, §23) returns the
single highest-`Version` document for a Project — which, given at most one
draft can exist and a draft's `Version` number is always `>=` any
`finalized` version's (new versions are only ever created going forward),
is unambiguous without needing to separately specify "latest draft" vs.
"latest finalized" at this endpoint (see §23 for exact tie-break behavior
and the note on what M5 will likely want instead).

**Version 1 creation**: explicit only, via `POST /estimates` — never
auto-created as a side effect of Project or CostItem creation. This matches
the AI/system principle running through all of phase1.md ("AI Suggests, the
System Calculates, the Contractor Approves" — §18/§21) applied one layer up:
an Estimate is itself something the contractor deliberately requests, not
something that materializes silently as a side effect of unrelated writes.

**Can a finalized Estimate be edited?** No — confirmed in §10.

**Does editing pricing settings on a finalized Estimate create a new
version?** N/A directly — a finalized Estimate cannot be edited at all
(§10); the only way to get new pricing numbers once finalized is
`POST /estimates/{id}/versions`, which creates a new draft with its own
fresh refresh/reprice/finalize lifecycle (§17).

**Should M5 reference `estimateId` where that ID uniquely identifies a
version?** Yes — under the one-document-per-version model, `Estimate.ID` IS
a specific version. M5's future `estimateId` field is exactly this
`Estimate.ID`, no additional version-number field needed alongside it.

---

## 5. Estimate snapshot granularity

**Decision: one snapshot line per CostItem** (task brief option A), storing
`SourceCostItemID`, `WorkItemID`, `Category`, `Description`, and the
snapshotted `Money` amount — **not** additionally storing a separate
category/WorkItem rollup structure.

Reasoning:
- Traceability requires knowing which CostItem produced which line — only
  possible at CostItem granularity (Options C/E lose this entirely; Option
  B loses which specific CostItem within a WorkItem contributed how much).
- phase1.md §41's Estimated-vs-Actual table and §22's Profitability Preview
  are both **category-level displays**, but a display grouping is a
  read-time concern — the Estimate can expose category subtotals (server
  computes `Lines` grouped by `Category` at response time, purely derived,
  not separately stored — see §9) without needing to *persist* at category
  granularity. Persisting only category rollups (Option C) would make it
  impossible to answer "which specific cost line makes up this category's
  total" once CostItems are later corrected — the audit trail phase1.md §50
  cares about would be lost one level down.
- Option D (store detailed lines AND separate WorkItem/category rollups)
  duplicates information the detailed lines already fully determine —
  rollups are pure aggregation and cost nothing to compute at read time from
  `Lines`, so persisting them separately is redundant storage with a sync
  risk for zero benefit given M4's document sizes (a Project's CostItem
  count is bounded — tens to low hundreds, well within a single Mongo
  document's practical size budget).
- Option E (project-level totals only) fails the stated requirement
  outright: "no dependency on live M3 data" for the total breakdown a
  contractor will want to review before finalizing (phase1.md §22 shows a
  category breakdown, not just one number).

**`SourceCostItemID` is retained for traceability only** — never
dereferenced at read time to fetch a live `CostItem.Estimated` value. The
line's own `SnapshottedAmount` is what the Estimate's totals are computed
from, always — refreshed only by an explicit `refresh` call (§3, §16.2).

**Relationship to WorkItem/category breakdowns**: computed at API-response
time by grouping the persisted `Lines` — not persisted as a separate
structure (see above, and §9's response DTO).

---

## 6. Cost source from M3, and the new `costs` capability required

**Cost source: `cost_items` only, using `CostItem.Estimated` exclusively as
the cost basis.** Per the task brief's own fixed default, and confirmed as
correct against phase1.md: §20/§21/§22 describe the Estimate as the
*pre-construction* internal financial calculation — the one that precedes
generating a Quotation — which is definitionally the Estimated-cost stage,
not Committed/Actual/Paid (those represent later lifecycle facts phase1.md
§39/§41 describes as happening progressively *after* estimation, during
procurement and construction). `Committed`/`Actual`/`Paid` are never
substituted, silently or otherwise.

**Never `cost_items + labour_entries`** — confirmed, matching the M3
invariant that `CostItem` is the universal ledger (M3 design spec §1.4);
`labour_entries` is never separately summed.

**New capability required on `costs.Service`** — `estimates` must not import
`internal/costs` by type (ADR 0002). Following the exact pattern M3
established for `work.WorkItemLookup` (a capability the *consuming* module
defines, satisfied structurally by the owning module's `*Service`, using
only primitives/foundation types, never a named domain struct):

```go
// defined in internal/estimates
type EstimatedCostSource interface {
    // Iterates every CostItem under projectID with a non-nil Estimated
    // value, invoking visit once per CostItem. Returns the count of
    // CostItems under this Project that have NO Estimated value set, so the
    // caller can apply the missing-Estimated policy (§7) without a second
    // round-trip. Uses primitives and foundation.money.Money only — no
    // costs.CostItem struct crosses the module boundary (ADR 0002). Called
    // by BOTH creation and refresh (§16.2) — always a full re-read of
    // current cost_items, never an incremental diff.
    VisitEstimatedCostItems(
        ctx context.Context,
        companyID string,
        projectID string,
        visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
    ) (missingEstimatedCount int, err error)
}
```

Satisfied structurally by a **new method** on `costs.Service`:

```go
// Added in M4. Satisfies estimates.EstimatedCostSource structurally.
func (s *Service) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
    visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error)
```

Implemented as `ListByProject` (already exists) plus in-service filtering —
no new repository method strictly required, since `ListByProject` already
returns every `CostItem` for a project and the filter (`Estimated != nil`)
and the missing-count tally can both happen in the service layer over that
one query result. This mirrors `work.WorkItemBelongsToCompany`'s pattern of
building a capability method on top of an already-existing repository call
rather than adding a new one where the existing query is sufficient.

**Why a visitor/callback shape instead of returning a slice of a shared
struct**: matches the brief's own suggested shape and ADR 0002's constraint
directly — a named return type (e.g. an exported "CostSnapshotEntry"
struct) would itself become a shared type between the two packages, which
is exactly the "domain-type leakage across module boundaries" M2/M3 already
rejected (M2 §13, M3 spec §9.2's `GetReferencePrice` returning a bare
`money.Money` rather than a `Material` struct for the same reason). The
visitor callback uses only `string`, `*string`, and `money.Money` (already a
cross-module-safe foundation type) — no new shared type is introduced.

**`estimates` also needs `ProjectLookup`** (validate `projectID` belongs to
`companyID` before creating/listing Estimates or CostItem snapshots) —
identical shape and rationale to every other M2/M3 module's own copy:

```go
// defined in internal/estimates
type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}
```

---

## 7. Missing-Estimated CostItem behavior

**Decision: policy (C) — generate the Estimate, but make missing-Estimated
CostItems structurally visible, not policy (A) silent-skip, not (B)
hard-reject, not (D) interactive user selection** (out of scope for a
backend-only M4 design).

Rationale against phase1.md's actual constraints:
- Silent skip (A) directly risks the exact failure mode the task brief
  names: "impossible for a contractor to unknowingly believe an Estimate
  represents all known project costs" — this is the one policy explicitly
  ruled out by the requirement itself. phase1.md §51 ("Financial Data
  Integrity... Validate all financial inputs") supports treating this as a
  data-integrity concern, not a thing to paper over.
- Hard reject (B) is too blunt for the described Phase-1 workflow: phase1.md
  §19 explicitly says costs "should eventually support four lifecycle
  stages" but "For Phase 1, the user interface may focus primarily on:
  Estimated, Actual" — implying it is expected and normal for some CostItems
  to have `Actual` set without ever having had `Estimated` set. Rejecting
  Estimate creation/refresh outright whenever *any* such CostItem exists
  under the Project would make it fail in an ordinary, expected situation.
  (Note: this is distinct from §7.1's stricter rule for the *zero-eligible*
  case, resolved in the review round — see below.)
- (C) is the resolution that matches both constraints: the Estimate is
  still created (contractor is not blocked from producing a Profitability
  Preview just because one incidental cost has no Estimated value yet), but
  the **response makes the gap visible and countable**, not silent.

**Concrete mechanism**: `EstimateCostLine`s are only created for CostItems
with a non-nil `Estimated`. The Estimate response DTO (§9) includes
`ExcludedCostItemCount` — the `missingEstimatedCount` value
`VisitEstimatedCostItems` returns — surfaced directly in the API response,
not merely logged server-side. This count is itself part of the persisted
document (updated on every refresh, §16.2), never recomputed live on GET.

### 7.1 Zero eligible CostItems — reject, per review decision (§32-4)

**Revised from the original draft's "representable as an empty Estimate"
position.** If a Project has zero CostItems with a non-nil `Estimated` at
creation or refresh time, the operation is rejected outright with
`ErrNoEstimatedCosts` — no Estimate document is created (on `POST
/estimates`) and no refresh is applied (on `POST /estimates/{id}/refresh`,
which instead leaves the draft's existing `Lines` untouched and returns the
error — see §16.2). Rationale: an Estimate whose entire purpose is
calculating expected project cost and profit has nothing to calculate
against when there is nothing eligible to sum; representing this as a valid
zero-cost Estimate invites exactly the "0/0 undefined margin" pathology
identified in the review (§7.2 below), and provides no actual value over
simply not creating the Estimate yet.

**Additionally, `CostSubtotal <= 0` after inclusion is also rejected**
(§7.2) — not just the zero-eligible-lines case. A Project could
theoretically have eligible CostItems that net to zero or a negative total
(e.g. a single `CostItem` with a negative `Estimated` value — not
structurally prevented by M3, though not a realistic contractor scenario
either); rejecting on `CostSubtotal <= 0` rather than merely
`len(Lines) == 0` closes this edge case with the same reasoning.

### 7.2 Why zero/negative `CostSubtotal` must be rejected before pricing

With `CostSubtotal <= 0`:
- Margin-mode pricing (`CostSubtotal / (1 - rate/10000)`) applied to
  `CostSubtotal = 0` yields `ProposedSellingPrice = 0` regardless of rate —
  arithmetically clean but commercially meaningless (no real Estimate has a
  RM0 cost basis).
- `ProjectedGrossMarginBPS = ProjectedGrossProfit / ProposedSellingPrice`
  becomes a division by zero when `ProposedSellingPrice = 0` — undefined,
  not just "zero," and must never be silently computed as `0` or any other
  placeholder value.

Rejecting at the `CostSubtotal <= 0` boundary — before any pricing
calculation runs — means `internal/estimates`' pricing-formula code path
(§13) never has to special-case a zero denominator; the precondition is
enforced upstream, at snapshot-acceptance time, consistent with "validate
all financial inputs" (phase1.md §51) rather than defensively guarding the
formula itself.

**No caller-supplied fallback currency or fallback cost basis is
introduced** for the zero-eligible case — per the review decision, this
case is a hard rejection, not a data-entry convenience.

---

## 8. Mixed-currency behavior

**Decision: reject at snapshot-creation/refresh time with a clear domain
error, per the task brief's own recommended invariant.** All CostItems
included in one Estimate snapshot (i.e., every CostItem with a non-nil
`Estimated` under that Project, per §7's inclusion rule) must share one
`Currency`.

- Zero eligible CostItems is no longer treated as a currency question at
  all — it is rejected outright per §7.1, before currency inference is even
  attempted.
- If two or more distinct currencies are found among eligible CostItems:
  reject with `ErrMixedCurrencyCostItems` before any calculation — no
  addition across currencies ever occurs, no partial/best-effort snapshot
  is returned.

**Currency determination for a valid (single-currency) snapshot**: inferred
from the first eligible CostItem encountered, then verified that every
subsequent eligible CostItem matches it — **not** supplied explicitly by the
caller, and **not** derived from a `Company.DefaultCurrency` field, because
no such field exists yet (`Company` (M1) has no currency field at all,
matching the M3 spec's own identical finding at §1.4.2). Claiming
company-wide currency enforcement here would misrepresent what the system
actually guarantees, exactly as the M3 spec was careful not to claim it for
`CostItem.Currency`.

**On refresh (§16.2)**: currency is re-inferred from the current eligible
CostItems, the same way — if the Project's currency situation has changed
(e.g. a previously-single-currency Project now has a second currency
introduced), the refresh is rejected with the same mixed-currency error, and
the draft's existing `Lines`/`Currency` are left untouched (refresh either
fully succeeds or fully no-ops from the caller's point of view — no partial
update).

---

## 9. Estimate financial outputs — persisted vs. derived

**All financial totals are persisted on the Estimate document at
creation/refresh/pricing-recalculation time — not recomputed from `Lines`
at GET time** (beyond simple derived groupings that are safe to compute
fresh on every read, see below), consistent with "an Estimate version is
fully self-contained between explicit mutations" and never dependent on
live M3 data during GET.

| Field | Persisted or derived at response time? |
|---|---|
| `Lines` (snapshot) | Persisted — changes only via explicit `refresh` (draft) or never (finalized) |
| `CostSubtotal` | Persisted — computed at creation/refresh time from `Lines` |
| `ExcludedCostItemCount` | Persisted — updated at creation/refresh time |
| `PricingMode`/`PricingRate` | Persisted — the contractor's chosen inputs |
| `ProposedSellingPrice` | Persisted — the calculated output |
| `ProjectedGrossProfit` | Persisted |
| `ProjectedGrossMarginBPS` | Persisted |
| Category-level rollup breakdown (for the "Profitability Preview"-style response, §22) | **Derived at response time** from `Lines` — pure grouping/summing, safe to compute on every read since `Lines` cannot change except via an explicit, separately-visible `refresh` |

Storing all top-level totals (not just `Lines`) means an Estimate version's
JSON response never requires touching `cost_items` again after
creation/refresh — matching the "avoid calculations that depend on live M3
records during GET" requirement directly, and matching the M3 precedent of
storing `LabourEntry.Cost` rather than recomputing it at read time (M3 spec
§1.3).

---

## 10. Draft/finalized lifecycle

**Two statuses only: `draft`, `finalized`.** No `sent`/`approved`/`rejected`
— those belong to the future Quotation/Client-approval domain (phase1.md
§27/§30/§31 describe exactly those concepts, but scoped to Quotation, not
Estimate).

- **Can a draft be edited?** Yes, in two distinct ways, both in place, same
  `Version`, `Revision` incremented each time (§16): (1) pricing
  recalculation (`PATCH .../pricing`) — pricing fields only, `Lines`
  untouched; (2) refresh (`POST .../refresh`) — `Lines`/`CostSubtotal`/
  `ExcludedCostItemCount` replaced from current `cost_items`, pricing fields
  recomputed against the new `CostSubtotal` using the currently-stored
  `PricingMode`/`PricingRate` (refresh always keeps the totals internally
  consistent — it never leaves stale pricing sitting on top of a fresh cost
  basis).
- **Can a finalized Estimate be edited?** No — every field is immutable
  once `Status = finalized`, including pricing and `Lines`. This matches the
  "issued Quotations should be treated as historically stable records"
  principle (§27) applied to Estimates, which is the domain object issued
  Quotations will (in M5) be built from.
- **Does finalizing lock the version?** Yes — `finalize` is the only
  transition, `draft → finalized`, one-directional. Once finalized, neither
  `refresh` nor `PATCH .../pricing` may be called against this document
  again (both return 409).
- **Is `finalize` idempotent?** Yes — `POST /estimates/{id}/finalize` on an
  Estimate that is already `finalized` returns the current (unchanged)
  Estimate with a 200, not an error, **regardless of the `expectedRevision`
  supplied on the retry** (idempotent retries by definition can't be
  expected to know the Revision the FIRST successful call left behind,
  since a finalized document's `Revision` is frozen the moment finalize
  succeeds — see below). Every *other* mutation (`refresh`, pricing
  recalculation, new-version creation) is intentionally NOT idempotent in
  the same sense — each application creates a genuinely new state (a new
  `Revision`, or a new `Version`), by design.
- **`finalize` is Revision-guarded, per the review round.** The original
  draft left `FinalizeEstimate` unguarded by `Revision`, unlike `refresh`
  and pricing recalculation — an inconsistency, since finalize is exactly
  the kind of consequential, hard-to-reverse action `Revision` exists to
  protect: without the guard, a contractor could review a draft's numbers
  at `Revision 4`, have another concurrent request (or another user/tab)
  refresh the draft to `Revision 5` with different totals, and then click
  "Finalize" — permanently locking in `Revision 5`'s numbers, which they
  never actually reviewed. `POST /estimates/{id}/finalize` now requires the
  caller to supply `expectedRevision`, exactly like `refresh`/pricing
  recalculation (§16.3). A stale `expectedRevision` against a still-`draft`
  Estimate is rejected with 409 (the document remains `draft`, untouched) —
  the caller must re-`GET`, review the actual current numbers, and retry
  with the current `Revision` if they still want to finalize. Once
  finalize succeeds, `Revision` is frozen at whatever value it held at that
  instant (finalize itself does not increment `Revision` further — there
  is no next mutation to protect once the document is immutable) and every
  subsequent idempotent retry short-circuits on `Status == finalized`
  before ever consulting `expectedRevision`.
- **Does changing pricing on a finalized Estimate require a new version?**
  N/A as a mutation — finalized is immutable outright; the only way to get
  different numbers is `POST /estimates/{id}/versions` (§17), which
  **requires** the source Estimate to already be `finalized` (revised
  per the review round — see §17) and creates a new draft with its own
  fresh refresh/reprice/finalize lifecycle.
- **Can an Estimate be reopened?** No un-finalize operation — matches
  phase1.md's explicit framing for the analogous Quotation case (§27) and
  this spec's own immutability principle. The correct action for new
  numbers is creating a new version.
- **Is there exactly one current/latest version per Project?** At most one
  version overall is "latest" by `MAX(version)`, and — per the review
  decision (§32-3) — **at most one draft may exist at any time**, enforced
  by a partial unique index (§20). This is no longer an open question.

---

## 11. Operation summary — refresh, pricing recalculation, and new version

Per the review round (§32-2), the three operations now have clearly
distinct, non-overlapping purposes:

**`PATCH /estimates/{id}/pricing`** — same stored cost snapshot, pricing
inputs change only. Draft-only. Same `Version`. `Revision` incremented.
Never touches `Lines`/`CostSubtotal`. Detailed in §16.1.

**`POST /estimates/{id}/refresh`** — draft-only, re-reads current
`cost_items`, replaces `Lines`/`CostSubtotal`/`ExcludedCostItemCount` in
place, recomputes pricing against the new `CostSubtotal` using the
currently-stored `PricingMode`/`PricingRate`. Same `Version`. `Revision`
incremented. Detailed in §16.2.

**`POST /estimates/{id}/versions`** — creates the next `Version` as a new
draft. **Requires the source Estimate to already be `finalized`** (revised
per the second review round — see §17): the service layer enforces this
explicitly with a dedicated precondition check, rather than allowing the
call to proceed against a draft source and rely on the partial
unique-draft index to reject it after the fact with an ambiguous
duplicate-key error.

This directly implements the workflow the review specified:

```
V1 Draft
├── refresh costs        (Revision++, same Version)
├── refresh costs        (Revision++, same Version)
├── change markup        (Revision++, same Version)
├── refresh costs        (Revision++, same Version)
├── change margin        (Revision++, same Version)
└── finalize             (Status=finalized, Revision frozen)
     ↓
V1 Finalized — immutable

Costs later change (a genuine commercial re-quote is needed)
     ↓
POST /estimates/{id}/versions
     ↓
V2 Draft (fresh Revision sequence, same lifecycle as V1 had)
```

`Version` therefore only advances for meaningful commercial revisions, not
for every intermediate working refresh or pricing tweak — directly
resolving the concern the review raised about `Version` numbers climbing
too fast under the original (now superseded) design.

---

## 12. Cross-module capability design and dependency graph

`estimates` must not directly query `cost_items` or `projects`' collections
(ADR 0002). Capabilities consumed:

```text
estimates.ProjectLookup       ← satisfied by projects.Service (unchanged)
estimates.EstimatedCostSource ← satisfied by costs.Service (NEW method, §6)
```

`estimates` defines both interfaces itself (consumer-defines-interface,
matching every prior module's pattern — M2 §8.3's rationale, reaffirmed in
M3 §9.1, applies unchanged: no shared generic interface package).

**Compile-time dependency graph:**

```text
                    cmd/api (composition root)
                          │
                          ▼
                      projects (M2, unchanged)
                          │
              ┌───────────┴────────────┐
              ▼                        ▼
            work (M2+M3)            (no new edge from
              │                      projects to costs;
              ▼                      costs already depends
            costs (M3 + NEW              on projects — M3 §8)
    VisitEstimatedCostItems method)
              │
              ▼
          estimates (NEW —
     consumes ProjectLookup from
     projects, EstimatedCostSource
        from costs)
```

Concrete construction order in `cmd/api/main.go`, appended after the
existing M3 block:

```go
estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)
// consumes ProjectLookup (from projectsService) and EstimatedCostSource (from costsService)
```

Acyclic: `estimates` depends on already-constructed `projects` and `costs`;
neither `projects` nor `costs` gains any dependency on `estimates`. No
import cycle; fully constructible in one pass, matching every prior
milestone's pattern exactly.

**`estimates` does not depend on `work`, `materials`, or `labour` directly**
— all cost information, including labour and material costs, already flows
through `cost_items` (the M3 universal-ledger decision, §6 above), so
`estimates` only ever needs one capability from `costs`, never separate
capabilities from `labour`/`materials`.

---

## 13. Markup vs. margin — decision and exact formulas

**Approved as originally proposed (§32-5): both are supported, contractor
selects one mode per Estimate version (`PricingMode`), never both active
simultaneously on the same Estimate.** The review round confirmed this
does not lose phase1.md's requirement that both markup AND margin
calculations exist: `PricingMode` selects the *input* strategy the
contractor supplies, while both the selling price AND the resulting gross
profit/margin are always calculated as outputs regardless of which mode was
used as input — so a markup-mode Estimate still reports its resulting
`ProjectedGrossMarginBPS`, and a margin-mode Estimate still reports its
resulting markup-equivalent relationship implicitly via
`ProjectedGrossProfit`/`CostSubtotal`. phase1.md's "Markup... Margin
calculations" bullet list (§20) is satisfied by this output completeness,
not by requiring two independent, simultaneously-active pricing inputs.

**Applied once at the whole-Estimate level, not per-line/per-category/
per-WorkItem** — approved as the right M4 scope (§32-5); per-category
markup is explicitly deferred, not implemented.

**Formulas** (using `RateBPS`, `BasisPointsDenominator = 10000`), now
applied directly to `CostSubtotal` since contingency is removed (§14):

```
Markup mode:
  ProposedSellingPrice = CostSubtotal × (1 + PricingRate/10000)

Margin mode:
  ProposedSellingPrice = CostSubtotal / (1 - PricingRate/10000)

Both modes, after ProposedSellingPrice is known:
  ProjectedGrossProfit = ProposedSellingPrice − CostSubtotal
  ProjectedGrossMarginBPS = round(ProjectedGrossProfit / ProposedSellingPrice × 10000)
```

**Validation:**
- `PricingRate >= 0` in both modes — non-negative rates only; phase1.md
  shows no example of negative markup/margin.
- Margin mode additionally requires `0 <= PricingRate < 10000` (i.e.,
  `0% <= margin < 100%`) — strictly less than 100%, since
  `PricingRate = 10000` makes the margin-mode divisor `(1 - 1) = 0`;
  rejected outright, not silently clamped.
- Markup mode has no analogous upper bound.
- `CostSubtotal <= 0` is rejected upstream at snapshot-acceptance time
  (§7.2), so these formulas never actually execute against a zero or
  negative cost base.

**No ambiguous generic fields** — confirmed: no bare `percentage` or
`profitRate` field exists on `Estimate`; `PricingMode` + `PricingRate`
together fully disambiguate what a stored rate means.

---

## 14. Contingency — removed from M4 (§32-6)

**Contingency is not part of the M4 domain model.** The original draft's
`ContingencyRate`/`ContingencyAmount`/`AdjustedCost` fields, and the
"contingency applied before markup/margin" calculation-ordering discussion,
are removed entirely — not deferred-with-a-placeholder-field, actually
absent from the schema.

Rationale, per the review decision: phase1.md contains zero mentions of
contingency anywhere (§0 above already established this). Building a
contingency model on no textual grounding risks locking in an invented
financial rule (rate-based vs. fixed, applied before vs. after markup, part
of internal cost vs. part of selling price) that would need to be
unwound or migrated later if real contractor workflow requirements turn out
to want something different. This follows the same discipline already
applied successfully in M2/M3 (e.g. deferring `Material.PreferredSupplierID`
until `suppliers` actually exists) — don't add a concept merely because it
might eventually be useful.

**Simplified calculation chain, without contingency:**

```
CostSubtotal
  ↓
Markup or Margin (§13)
  ↓
ProposedSellingPrice
  ↓
ProjectedGrossProfit
  ↓
ProjectedGrossMarginBPS
```

If contingency becomes a validated Phase-1 requirement later, it can be
added as a purely additive schema change (new optional
`ContingencyRate`/`ContingencyAmount` fields, `nil` on every M4-created
document) without migrating existing Estimates — matching the
"additive, no migration" principle already established for
`Material.PreferredSupplierID`'s eventual M6 return.

---

## 15. Rounding order

All monetary calculations use `money.RoundToMinorUnits`-style centralized
rounding — no new rounding policy is invented in `internal/estimates`, per
ADR 0001.

**Deterministic calculation/rounding order for one Estimate
(re)calculation** (simplified now that contingency is removed, §14):

```
1. CostSubtotal = Σ Lines[i].SnapshottedAmount   (exact int64 sum, no rounding needed)
2. ProposedSellingPrice =
     markup: round(CostSubtotal × (1 + PricingRate/10000))
     margin: round(CostSubtotal / (1 - PricingRate/10000))
                                                  (one rounding step, via a new foundation helper — §18)
3. ProjectedGrossProfit = ProposedSellingPrice − CostSubtotal   (exact int64 subtraction)
4. ProjectedGrossMarginBPS = round(ProjectedGrossProfit × 10000 / ProposedSellingPrice)
                                                  (one rounding step, produces a RateBPS, not a Money — distinct helper, §18)
```

Each of steps 2 and 4 rounds exactly once — never re-rounding an
already-rounded intermediate value, and never applying a different rounding
rule across line items vs. the total (there are no per-line pricing
calculations in this model — markup/margin apply once at the whole-Estimate
level, §13).

---

## 16. Draft mutation operations — pricing recalculation and refresh

### 16.1 `PATCH /estimates/{id}/pricing`

Accepts `pricingMode`, `pricingRate`. Recomputes steps 2-4 of §15 from the
**already-stored** `CostSubtotal` — never re-touches `cost_items`. Rejects
(409) if `Status != draft`.

**Optimistic concurrency (§32-2's required follow-through):** the request
must supply the `Revision` it read the draft at (`If-Match`-style body
field or header — exact transport mechanism an implementation detail, not a
design ambiguity). The update is conditioned on
`{_id, companyId, revision: suppliedRevision}` matching; on a mismatch
(another writer already advanced `Revision`), the write is rejected with a
409 and the caller must re-`GET` and retry. On success, `Revision` is
incremented by exactly 1.

This closes the gap the original draft's "last-write-wins" position left
open: once refresh (§16.2) and pricing recalculation are both real
in-place mutations against the same mutable draft document, two concurrent
writers (e.g. one request refreshing costs, another changing pricing at
the same moment) could otherwise silently overwrite one another's change
with no signal to either caller. `Revision` is scoped **per-document**, not
`{companyId, projectId}`-wide like `Version` — it has nothing to do with
version numbering or the `{companyId, projectId, version}` unique index
(§20); it is a plain optimistic-concurrency counter on one specific
document, in the same spirit as phase1.md §54's generic guidance, applied
here specifically (rather than platform-wide) because this is the one M4
document that is genuinely mutable by more than one possible concurrent
writer.

### 16.2 `POST /estimates/{id}/refresh`

Steps:
1. Reject (409) if `Status != draft`.
2. Re-run `EstimatedCostSource.VisitEstimatedCostItems` against this
   Estimate's own `ProjectID`.
3. Apply §7/§7.1's missing-Estimated and zero-CostSubtotal policy, and §8's
   mixed-currency check, against the **current** state of `cost_items` —
   these can genuinely differ from what the draft's existing snapshot
   found (new CostItems may have been added; a previously-mixed-currency
   situation may have been fixed or newly introduced). If either check now
   fails, the refresh is rejected and the draft's existing `Lines`/
   `CostSubtotal`/pricing are left completely untouched (no partial
   update) — the caller sees an error, not a silently-degraded Estimate.
4. On success: replace `Lines`, `CostSubtotal`, `ExcludedCostItemCount`.
5. Recompute pricing (steps 2-4 of §15) against the new `CostSubtotal`,
   using the **currently-stored** `PricingMode`/`PricingRate` (refresh
   never resets pricing inputs — it only recalculates their result against
   the fresh cost base, keeping the two internally consistent).
6. Set `RefreshedAt = now`.
7. Same optimistic-concurrency guard as §16.1 — the caller supplies the
   `Revision` it read at; mismatch → 409; success → `Revision` incremented
   by exactly 1.

### 16.3 `POST /estimates/{id}/finalize` — also Revision-guarded (added per
second review round)

The original draft left `finalize` as the one draft-mutating operation with
no `Revision` guard — an inconsistency once `refresh` and pricing
recalculation both carry one. `Revision` exists specifically to stop a
caller from acting on stale numbers; finalize is the single most
consequential such action (it is the one operation with no way back, §10),
so it needs the guard more than the other two, not less.

Steps:
1. Reject (409, no state change) if `Status != draft` **and** the request's
   `expectedRevision` does not match a document that is `Status ==
   finalized` — see the idempotency carve-out below.
2. If `Status == finalized` already: return the current Estimate unchanged
   with 200, **regardless of the supplied `expectedRevision`** (idempotent
   retry — a client retrying after a timeout cannot be expected to know
   what `Revision` the first successful call left the document at, since
   finalize freezes `Revision` and does not increment it further).
3. Otherwise (`Status == draft`): the update is conditioned on
   `{_id, companyId, status: "draft", revision: expectedRevision}`
   matching, exactly like §16.1/§16.2's guard. A mismatch (another
   mutation — a refresh, a pricing change, or a concurrent finalize attempt
   from a different caller — already advanced `Revision`) is rejected with
   409 and the document remains an unchanged `draft`. The caller must
   re-`GET`, review the actual current numbers, and resubmit `finalize`
   with the current `Revision` if they still intend to lock those numbers
   in.
4. On success: `Status = finalized`, `FinalizedAt = now`. `Revision` itself
   is left at its current value — finalize does not increment it, since
   there is no subsequent mutation on this document for a fresh `Revision`
   to protect against.

**Worked example proving the concurrency hazard this closes:** Contractor A
opens a draft at `Revision 4`, reviews `ProposedSellingPrice = RM50,000`.
Contractor B (or another tab/session for the same user) calls `refresh`,
which picks up a cost change and advances the document to `Revision 5` with
`ProposedSellingPrice = RM53,000`. Contractor A, still looking at the
`Revision 4` numbers on screen, clicks "Finalize" — the request carries
`expectedRevision: 4`. Without this guard, the system would finalize
`Revision 5`'s numbers (RM53,000) under the belief A confirmed RM50,000. With
the guard, the request is rejected with 409; A is forced to reload and see
RM53,000 before any finalize can succeed.

---

## 17. New-version creation

`POST /estimates/{id}/versions` — the operation that advances `Version`.
**Revised per the second review round: the source Estimate MUST already be
`finalized`, enforced by an explicit service-layer precondition check, not
left to the partial-unique-draft index to reject after the fact.** The
original draft's position — "not technically restricted to a finalized
source, but the one-draft-per-Project index will reject it anyway if a
draft source is used" — was a real inconsistency: it described an operation
as conditionally allowed while relying on an unrelated index (whose actual
job is enforcing "at most one draft," not "a version's source must be
finalized") to produce the rejection as a side effect. That leaves the
service layer proceeding through most of the create-new-version work (steps
1-6 below) only to fail at the final write with an ambiguous
duplicate-key error the caller must then interpret, rather than failing
fast with a clear, named domain error.

**Now:** step 1 below explicitly checks `source.Status == finalized` before
doing anything else, returning `ErrEstimateMustBeFinalizedBeforeNewVersion`
(mapped to 409) if the source is still a `draft`. The partial
unique-draft index remains in place as a database-level backstop (§20) —
useful defense-in-depth against a bug or a future code path that skips the
service-layer check — but it is no longer the *primary* mechanism a caller
is expected to encounter in the ordinary flow.

Steps:
1. Validate the source Estimate (`{id}`) exists and belongs to `companyID`.
2. **Reject (409, `ErrEstimateMustBeFinalizedBeforeNewVersion`) if
   `source.Status != finalized`.** This is the explicit precondition check
   added in the second review round — the operation never proceeds past
   this point against a `draft` source.
3. Copy `ProjectID` from the source Estimate (not re-supplied by the
   caller).
4. Run the exact same snapshot-building procedure as `POST /estimates`
   (§6/§7/§8) against the current state of `cost_items`.
5. Default `PricingMode`/`PricingRate` from the source Estimate's own
   values (carry forward the contractor's last pricing choice), unless the
   request body explicitly overrides them.
6. Recompute steps 2-4 of §15 against the new `CostSubtotal`.
7. Allocate `Version = MAX(version for this {companyId,projectId}) + 1`
   under the concurrency-safe strategy in §21.
8. Persist as a new document: `Status = draft`, `Revision = 0`,
   `CreatedAt = now`, `RefreshedAt = nil`, `FinalizedAt = nil`.

`POST /estimates` (first-ever creation) is a **related but distinct**
procedure — not simply "the same `MAX+1` allocation logic with no source
step," which a first draft of this section claimed and which a subsequent
implementation-planning review correctly flagged as unsafe. `POST
/estimates` attempts `Version = 1` **directly, exactly once, with no
retry** — see §21's explicit note on why sharing the `MAX+1`-with-retry
allocator between the two operations reopens the very race
`ErrEstimateAlreadyExistsForProject` exists to close.
`PricingMode`/`PricingRate` must be supplied explicitly on first creation
(no predecessor to default from).

### 17.1 Manual price/quantity override — resolved (second review round)

phase1.md §20 states: "The contractor may manually override prices and
quantities. The system assists the contractor but does not lock them into
calculated values." The original draft did not provide any path that
actually performs this override, despite discussing it in §0's gap list —
a real omission the review round correctly flagged: the spec should not
assert a phase1.md requirement is satisfied while exposing no mechanism for
it.

**Resolved interpretation: overrides happen upstream, in `internal/costs`,
not as an Estimate-local mechanism.** `internal/estimates` does not gain a
`PATCH /estimates/{id}/lines/{sourceCostItemId}`-style endpoint or any
other line-level override capability of its own. Instead:

```
Contractor wants to override a cost assumption
        ↓
PATCH /cost-items/{costItemId}/lifecycle  (stage=estimated, amount=<new value>)
        — this M3 endpoint ALREADY EXISTS and already lets the contractor
          set CostItem.Estimated directly, overriding whatever quantity ×
          unitPrice may have originally produced it (M3 design spec §5.3 —
          UpdateCostItemLifecycle sets exactly one of the four lifecycle
          fields per call; no formula re-validation is performed against
          Quantity/UnitPrice on this path, only on CreateCostItem)
        ↓
POST /estimates/{id}/refresh  (draft only)
        — pulls the corrected CostItem.Estimated into the Estimate's
          snapshot Lines, per §16.2
```

This means: **cost/price override is fully supported today, with no new
M4 code required for it** — `CostItem.Estimated` was always directly
settable via the existing M3 lifecycle-update endpoint, and M4's `refresh`
operation (added per the review round's decision 2, §32-2) is exactly the
mechanism that carries a corrected value into a draft Estimate. phase1.md's
"the Estimate uses the contractor-confirmed value" (§11, describing the
identical override principle one layer down at the Material-reference-price
level) is satisfied by this chain: the contractor confirms/overrides at the
`CostItem` layer, and the Estimate's `refresh` operation is the explicit,
visible action that pulls the confirmed value forward — never an implicit
recalculation the contractor didn't ask for.

**Quantity-level override is explicitly deferred, not silently
unsupported-and-unmentioned.** M3's `internal/costs` has no general
`quantity`/`unitPrice` correction endpoint today — `CreateCostItem`
validates and derives `Estimated` from `Quantity × UnitPrice` only at
creation time (M3 spec §1.4.2); there is no M3 equivalent of `labour`'s
narrow `PATCH /labour-entries/{id}` (quantity/rate correction, M3 spec
§1.3.2) for a general `CostItem`. Re-deriving a new `Estimated` from a
corrected quantity today requires the contractor to compute the new line
amount themselves and set it directly via `UpdateCostItemLifecycle` — the
system does not recompute `Quantity × UnitPrice` for them post-creation.
**This is a real, named gap in the M3 surface, not an M4 gap** — closing it
(a `PATCH /cost-items/{id}/quantity`-style endpoint, mirroring `labour`'s
existing correction pattern) is scoped to a future `internal/costs`
extension, explicitly out of M4's boundaries (M4 only adds
`VisitEstimatedCostItems` to `costs`, per §6 — extending `costs`' own
quantity-correction surface is a different, separately-scoped change). This
spec calls the limitation out explicitly, rather than leaving "manual
override exists" asserted with no path that actually performs the
quantity half of it.

---

## 18. Required `internal/foundation/money` additions

Per ADR 0001, no calculation-specific rounding logic may live in
`internal/estimates`. Two new small helpers are needed (contingency-related
helpers from the original draft are dropped along with contingency itself,
§14):

```go
// internal/foundation/money — proposed additions, alongside existing
// CalculateLineAmount/RoundToMinorUnits in rounding.go, or a new ratemath.go.

// AddRateBPS computes base × (1 + rate/10000), rounded once. The markup
// selling-price formula.
func AddRateBPS(base Money, rate RateBPS) Money

// DivideByComplementRateBPS computes base / (1 - rate/10000), rounded
// once. The margin selling-price formula. Callers must validate
// 0 <= rate < BasisPointsDenominator before calling — this function does
// not itself guard against rate >= BasisPointsDenominator; that validation
// is estimates' domain responsibility (§13), matching how
// CalculateLineAmount does not itself validate qty positivity.

// RateBPSFromRatio computes (numerator/denominator) × 10000, rounded to
// the nearest whole basis point, returning a RateBPS. Used for
// ProjectedGrossMarginBPS = RateBPSFromRatio(ProjectedGrossProfit, ProposedSellingPrice).
func RateBPSFromRatio(numerator, denominator Money) RateBPS
```

(`ApplyRateBPS`, proposed in the original draft for contingency-amount
calculation, is dropped — no longer needed with contingency removed.)

These are tested in `internal/foundation/money`'s own test suite (matching
`CalculateLineAmount`'s existing precedent) — `internal/estimates`' own
tests exercise *using* these helpers correctly and the domain formulas/
ordering built on top of them (§26), not re-testing the arithmetic itself.

---

## 19. Tenant ownership

Identical invariants to M1/M2/M3 (M3 spec §4), extended to `estimates` with
no exceptions:

1. Every `Estimate` document stores `companyId` directly, sourced only from
   `Principal.CompanyID`.
2. `companyId` is never accepted from request body/query/path.
3. `projectId` is validated via `ProjectLookup` before Version 1 creation;
   subsequent version creation copies `ProjectID` from the source Estimate
   (already validated at that Estimate's own creation) rather than
   re-accepting it from the request.
4. Every direct-by-ID repository method is tenant-scoped:
   `(ctx, companyID, id)`.
5. Cross-tenant existence is indistinguishable from non-existence — same
   404, same sentinel error (`ErrEstimateNotFound`).
6. `GET /estimates?projectId=...` and `GET /estimates/latest?projectId=...`
   both validate the Project belongs to `Principal.CompanyID` **before**
   querying — foreign `projectId` → 404, never `[]`/empty.
7. ID substitution across tenants never succeeds, for both the Estimate ID
   itself and any embedded `SourceCostItemID` exposed for traceability.

---

## 20. MongoDB representation and indexes

**Money**: `{amount: int64, currency: string}` — unchanged from ADR 0001.

**RateBPS**: stored as a plain `int64` (its underlying type). `Estimate`'s
domain-level `PricingRate money.RateBPS` field is stored as a plain `int64`
in the BSON document (`estimateDoc.PricingRate int64`) — an explicit type
conversion at the domain↔document boundary (`toEstimateDoc`/
`fromEstimateDoc`), matching the existing pattern of e.g. `costItemDoc`
storing `CostCategory` as a plain `string`.

**`EstimateCostLine`**: embedded array of subdocuments — no
`quantityDoc`-style indirection needed (unlike `CostItem`/`WorkItem`, an
`EstimateCostLine` has no `shopspring/decimal` quantity field).

```go
type estimateDoc struct {
    ID                      bson.ObjectID     `bson:"_id,omitempty"`
    CompanyID               string            `bson:"companyId"`
    ProjectID               string            `bson:"projectId"`
    Version                 int               `bson:"version"`
    Status                  string            `bson:"status"`
    Revision                int64             `bson:"revision"`
    Currency                string            `bson:"currency"`
    Lines                   []estimateLineDoc `bson:"lines"`
    CostSubtotal            money.Money       `bson:"costSubtotal"`
    ExcludedCostItemCount   int               `bson:"excludedCostItemCount"`
    PricingMode             string            `bson:"pricingMode"`
    PricingRate             int64             `bson:"pricingRate"`
    ProposedSellingPrice    money.Money       `bson:"proposedSellingPrice"`
    ProjectedGrossProfit    money.Money       `bson:"projectedGrossProfit"`
    ProjectedGrossMarginBPS int64             `bson:"projectedGrossMarginBps"`
    CreatedAt               time.Time         `bson:"createdAt"`
    RefreshedAt             *time.Time        `bson:"refreshedAt,omitempty"`
    FinalizedAt             *time.Time        `bson:"finalizedAt,omitempty"`
    SchemaVersion           int               `bson:"schemaVersion"`
}

type estimateLineDoc struct {
    SourceCostItemID  string      `bson:"sourceCostItemId"`
    WorkItemID        *string     `bson:"workItemId,omitempty"`
    Category          string      `bson:"category"`
    Description       string      `bson:"description"`
    SnapshottedAmount money.Money `bson:"snapshottedAmount"`
}
```

**Indexes — all four MUST carry explicit, mutually distinct names via
`SetName`:**

```go
{Keys: bson.D{{Key: "companyId", Value: 1}}}
{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
    Options: options.Index().SetName("idx_estimates_company_project")}
{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}, {Key: "version", Value: 1}},
    Options: options.Index().SetUnique(true).SetName("uq_estimates_company_project_version")}
{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
    Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "draft"}).
        SetName("uq_estimates_one_draft_per_project")}
```

**Explicit names are not optional here — an implementation-planning
review caught a real defect in an earlier draft that omitted them.** The
plain (non-unique) `{companyId, projectId}` index and the partial unique
`{companyId, projectId}` (draft-only) index share an identical key
pattern. MongoDB's default index-naming convention concatenates each key
and its sort direction and does **not** factor in a
`partialFilterExpression` — so both indexes would derive the exact same
default name (`companyId_1_projectId_1`), and index creation would fail
outright with a name collision, before the application ever serves a
single request. Beyond the creation-time failure, the two indexes' names
also need to be usable to *classify* which one produced a given
duplicate-key error at runtime (§21.2 step 7): MongoDB's duplicate-key
error message always embeds the offending index's name as a substring of
a longer sentence, so the classification logic depends on comparing
against a substring that cannot ambiguously match more than one index's
name. Under the default-naming scheme, `companyId_1_projectId_1` (the
would-be draft-index name) is itself a literal substring of
`companyId_1_projectId_1_version_1` (the would-be version-index name),
so even if the creation-time collision were somehow avoided, a substring
check would misclassify every version-number conflict as a draft
collision. The three explicit names above
(`idx_estimates_company_project`, `uq_estimates_company_project_version`,
`uq_estimates_one_draft_per_project`) are mutually non-overlapping
strings — none is a substring of another — closing both problems at
their root rather than working around them in the classification logic.

The partial-unique index (`uq_estimates_one_draft_per_project`) is new per
the review decision (§32-3): scoped to documents where `status ==
"draft"`, enforcing "at most one draft per Project" at the database
level — the same enforcement mechanism class already used for
`{companyId, projectId, version}` uniqueness, applied with a partial
filter so it only constrains draft documents (multiple `finalized`
documents for the same Project are of course expected and must not be
blocked by this index).

**Duplicate-key error classification** (used by §21.1/§21.2 above): a
`Create` call that fails with a MongoDB duplicate-key error must
translate it into one of three domain outcomes by inspecting which named
index the error message references — a collision on
`uq_estimates_company_project_version` means "version-number conflict"
(retriable only from `CreateNewVersion`'s allocation strategy, §21.2);
a collision on `uq_estimates_one_draft_per_project` means "a draft
already exists for this project" (never retried, per §21.1/§21.2); a
duplicate-key error matching neither named index must be treated as an
**unclassified** failure, not defaulted to either of the above — an error
this logic cannot positively identify must not be assumed safe to retry.

**"Latest version" query**: satisfied by the existing
`{companyId, projectId, version}` compound index (equality on the first two
fields, sort descending on `version`, limit 1) — no additional index
needed for this. **"Current draft" query**: satisfied directly by the new
partial index (equality on `{companyId, projectId}`, filtered to
`status: draft` — the index itself guarantees at most one match).

---

## 21. Version-number allocation and concurrency strategy

**MongoDB here is standalone — no multi-document transactions assumed.**
This section concerns **`Version` allocation** — it is distinct from and
unaffected by the `Revision` optimistic-concurrency counter (§16.1/§16.2),
which guards in-place draft mutation, not version-number allocation.

**This section resolves into two DIFFERENT strategies for two DIFFERENT
operations — this distinction is itself the correction of a real bug an
implementation-planning review caught, described below, and must not be
collapsed back into one shared strategy.**

### 21.1 `POST /estimates` (`CreateEstimate`) — no retry, ever

```
0. Reject (409, ErrEstimateAlreadyExistsForProject) if
   MAX(version for this {companyId,projectId}) > 0 — i.e. any Estimate
   version, draft or finalized, already exists for this Project.
1. Attempt to insert the new document with version = 1, status=draft.
2. If the insert fails on EITHER unique index (a concurrent request won
   the race to create Version 1 first, OR a draft already exists for some
   other reason): return ErrEstimateAlreadyExistsForProject immediately.
   Do NOT retry, and do NOT re-read MAX(version) and attempt a higher
   version number.
```

**Why no retry is allowed here, ever:** an earlier draft of this section
described `POST /estimates` as reusing the `MAX(version)+1`-with-retry
strategy in §21.2 below, with no source-version step. That is unsafe and
was corrected during implementation planning: if `CreateEstimate` retried
by re-reading `MAX(version)` on a collision, the following race becomes
possible even though step 0's up-front check ran first —

```
Request A reads MAX(version) = 0 (passes step 0)
Request B reads MAX(version) = 0 (passes step 0, same instant)
A proceeds, creates Version 1; it is later finalized
B's retry-driven re-read of MAX(version) now sees 1, and would
  succeed in creating Version 2 — via POST /estimates
```

This silently bypasses the entire point of `ErrEstimateAlreadyExistsForProject`
(§17) — a stray Version 2 created this way never goes through
`CreateNewVersion`'s finalized-source precondition at all. The fix is
structural, not a tighter retry bound: `POST /estimates` must never retry
into a recalculated higher version number under any circumstance. Exactly
one `Version = 1` attempt; any collision on either index maps to the same
caller-facing error, unretried.

### 21.2 `POST /estimates/{id}/versions` (`CreateNewVersion`) — bounded retry

```
0. Reject up front (409, ErrEstimateMustBeFinalizedBeforeNewVersion) if the
   source Estimate is not Status=finalized (§17 step 2) — this is the
   ordinary path for rejecting an attempt to version off a draft; it
   happens BEFORE any of the version-number allocation below is attempted.
1. Read current MAX(version) for {companyId, projectId} (sort desc, limit 1).
2. Attempt to insert the new document with version = MAX + 1, Status=draft.
3. If the insert fails with a duplicate-key error on the
   {companyId, projectId, version} unique index (another concurrent request
   won the race for the same version number):
     re-read MAX(version) again, retry the insert with the new MAX + 1.
4. If the insert instead fails on the {companyId, projectId} partial
   unique (draft-only) index — this should be rare given step 0's
   precondition check already rejected a draft source; it can still happen
   as a genuine race (e.g. two concurrent requests both pass step 0 against
   the SAME now-finalized source and both attempt to create the next
   draft simultaneously) — treat it identically to a version-number race
   for the purposes of the caller-visible error (409 Conflict), but do NOT
   retry it in a loop like step 3's case, since retrying "create a new
   draft" when one now exists is not a transient condition that resolves
   itself — the correct caller action is to fetch the draft that won the
   race and continue from there, not to blindly resubmit.
5. Bound the version-number retry loop (e.g. 5 attempts) for the case in
   step 3 only.
6. If all bounded attempts in step 3 are exhausted, return 409 Conflict.
7. If the insert fails with a duplicate-key error that matches NEITHER
   known index by name, do not assume it is safe to retry — surface it as
   an unclassified internal error instead of silently treating it as a
   step-3 version race (see §20's index-naming note: this discrimination
   requires the two unique indexes to have distinct explicit names, since
   two of this collection's four indexes would otherwise share MongoDB's
   default auto-generated name).
```

**Why `CreateEstimate` and `CreateNewVersion` cannot share one allocation
helper**, even though both ultimately call `Create` with a computed
version number: retrying into a recalculated higher version number is the
*correct* behavior for `CreateNewVersion` (a concurrent request creating
some other version should not cause a spurious failure when a fresh
number is available) and the *wrong* behavior for `CreateEstimate` (per
§21.1's worked race above). Any implementation must keep these as two
genuinely separate code paths, not one shared "allocate and create"
routine parameterized by caller — the second review round's own
implementation-planning draft made exactly this mistake once, by having
`CreateEstimate` delegate its write to the same retrying helper
`CreateNewVersion` correctly uses, and it had to be un-shared.

**No separate counters collection** — the two unique indexes together are
sufficient; no generic counters collection is introduced.

**Invariant to prove via integration test (§27)**: for one
`{companyId, projectId}` pair, concurrent `CreateNewVersion` attempts never
produce duplicate version numbers; a concurrent attempt against a `draft`
source is rejected at §21.2 step 0, never reaching version-number
allocation at all; the rare §21.2 step-4 race (two concurrent requests
both starting from the same finalized source) is correctly rejected for
exactly one of the two racers; and — separately — concurrent
`CreateEstimate` calls for the same Project never produce more than one
Version-1 document, with every loser receiving
`ErrEstimateAlreadyExistsForProject` and no retry-driven attempt at
Version 2.

---

## 22. Historical snapshot-integrity rules — explicit invariants

```
CostItem.Estimated changes
  → a DRAFT Estimate's Lines/CostSubtotal/totals remain unchanged until an
    explicit refresh is called (§16.2) — no automatic propagation
  → a FINALIZED Estimate version's Lines/CostSubtotal/totals never change,
    ever, regardless of any later CostItem change

LabourEntry correction
  → M3 CostItem.Estimated changes (already true per M3)
  → propagates to a draft Estimate ONLY on explicit refresh; a finalized
    Estimate is never affected

Material.ReferencePrice changes
  → existing CostItem unchanged (already true per M3)
  → Estimate unaffected (transitively, since CostItem itself didn't change)

Worker.DefaultRate changes
  → existing LabourEntry unchanged (already true per M3)
  → CostItem unchanged (already true per M3)
  → Estimate unaffected (transitively)

Explicit refresh (draft only)
  → intentionally pulls the latest CostItem.Estimated values into Lines

Explicit new-version creation
  → intentionally snapshots the latest CostItem.Estimated values into a
    fresh Version

Finalized Estimate versions
  → never silently change; no operation targets a finalized document for
    any field-level update, including refresh
```

---

## 23. Endpoint list

All authenticated (`RequireAuthHuma`, registered on the existing
`authedAPI` group). No endpoint accepts `companyId`.

| Method | Path | Responsibility |
|---|---|---|
| POST | `/estimates` | Create Version 1 for a Project (always `draft`) — body has `projectId`, `pricingMode`, `pricingRate` |
| GET | `/estimates?projectId=...` | List all versions for one Project (parent validated first) |
| GET | `/estimates/latest?projectId=...` | Get the single highest-`Version` Estimate for a Project (§4, §32-7) |
| GET | `/estimates/{id}` | Get one specific Estimate version, tenant-scoped |
| POST | `/estimates/{id}/refresh` | Re-pull current `cost_items` into this draft, in place, same Version — body has `expectedRevision` (§16.2) |
| PATCH | `/estimates/{id}/pricing` | Recalculate pricing in place from the stored snapshot — draft only — body has `expectedRevision` (§16.1) |
| POST | `/estimates/{id}/versions` | Create the next Version as a new draft — source must already be `finalized` (§17) |
| POST | `/estimates/{id}/finalize` | `draft → finalized`, one-directional, locks every field — body has `expectedRevision`; idempotent on retry (§16.3) |

**On `GET /estimates/latest`**: returns the single Estimate with the
highest `Version` for the Project — which, given the one-draft-per-Project
invariant and that new versions only ever move forward, is either the sole
current draft (if one exists and its Version exceeds all finalized ones,
which is always true since it was created after them) or the most recently
finalized version (if no draft currently exists). This spec does **not**
add separate "latest draft" vs. "latest finalized" query variants in M4 —
noted as a likely M5 refinement point: M5's Quotation-building need is
probably specifically "the latest **finalized** Estimate" rather than
"the latest Estimate regardless of status" (a Quotation should not be built
from a still-being-edited draft), and M5's own design spec should define
that narrower lookup explicitly when it is actually needed, rather than M4
guessing at M5's exact requirement now.

No generic `PATCH /estimates/{id}` for arbitrary field updates — every
mutation is one of the four purpose-specific operations above.

---

## 24. Parent-validation rules

- `POST /estimates`: `projectId` validated via `ProjectLookup` before any
  write.
- `GET /estimates?projectId=...` / `GET /estimates/latest?projectId=...`:
  `projectId` validated via `ProjectLookup` before querying — foreign
  `projectId` → 404, never `[]`/empty (§19.6).
- `POST /estimates/{id}/refresh`, `PATCH /estimates/{id}/pricing`,
  `POST /estimates/{id}/versions`, `POST /estimates/{id}/finalize`: no
  separate `projectId` in the request body at all — the Project is derived
  from `{id}`'s own already-validated `ProjectID`.

---

## 25. Service responsibilities

```go
type Service struct {
    repo           EstimateRepository
    projectLookup  ProjectLookup
    costSource     EstimatedCostSource
}

func NewService(repo EstimateRepository, projectLookup ProjectLookup, costSource EstimatedCostSource) *Service

func (s *Service) CreateEstimate(ctx context.Context, companyID, projectID string, pricingMode PricingMode, pricingRate money.RateBPS) (Estimate, error)
func (s *Service) GetEstimate(ctx context.Context, companyID, estimateID string) (Estimate, error)
func (s *Service) GetLatestEstimate(ctx context.Context, companyID, projectID string) (Estimate, error)
func (s *Service) ListEstimatesByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error)
func (s *Service) RefreshEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (Estimate, error)
func (s *Service) RecalculatePricing(ctx context.Context, companyID, estimateID string, pricingMode PricingMode, pricingRate money.RateBPS, expectedRevision int64) (Estimate, error)
func (s *Service) CreateNewVersion(ctx context.Context, companyID, sourceEstimateID string, pricingMode *PricingMode, pricingRate *money.RateBPS) (Estimate, error)
func (s *Service) FinalizeEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (Estimate, error)
```

`FinalizeEstimate` takes `expectedRevision`, matching `RefreshEstimate`/
`RecalculatePricing`'s signature shape — added per the second review round
(§16.3): finalize participates in the same Revision-guard contract as every
other draft mutation, rather than being the one unguarded exception.
`CreateNewVersion` internally checks `source.Status == finalized` as its
first step (§17 step 2) before doing any snapshot work, returning
`ErrEstimateMustBeFinalizedBeforeNewVersion` otherwise.

`CreateEstimate` and `CreateNewVersion` share an internal snapshot+calculate
helper (`buildSnapshot`, `calculateTotals`); `RefreshEstimate` reuses the
same `buildSnapshot` helper, then reuses `calculateTotals` against the
existing `PricingMode`/`PricingRate` — no duplicated snapshot/calculation
logic across the three entry points.

---

## 26. Unit-test matrix

- `EstimateStatus`/`PricingMode` enum validation (`IsValid()` methods).
- Version-number allocation logic (pure function: given existing versions,
  compute next version) separately from the Mongo-level race handling.
- Snapshot-total calculation: `CostSubtotal` = correct sum of `Lines`.
- No double-counting labour: proves the total matches
  `Σ CostItem.Estimated` exactly, never also summing `LabourEntry.Cost`.
- Pricing computed only from `CostSubtotal` — never reaches into a live
  cost source during pricing recalculation (fake `EstimatedCostSource` that
  would fail the test if called during `RecalculatePricing`).
- Markup formula: cost=RM100, 20% markup → RM120 exactly.
- Margin formula: cost=RM100, 20% margin → RM125 exactly.
- Invalid margin `>= 100%` rejected.
- Negative markup/margin rate rejected (both modes).
- `RateBPS` used throughout — no float in any expected-value computation.
- Centralized rounding: `estimates`' tests never reimplement rounding.
- `ProjectedGrossProfit`/`ProjectedGrossMarginBPS` calculation correctness.
- Mixed-currency snapshot rejection (fake `EstimatedCostSource` yielding two
  currencies) — both at creation and at refresh time.
- Zero-eligible-CostItems rejection (`ErrNoEstimatedCosts`) — both at
  creation and at refresh time (§7.1).
- `CostSubtotal <= 0` rejection even when `Lines` is non-empty (§7.2).
- Missing-Estimated behavior: fake source reporting a non-zero
  `missingEstimatedCount` — proves `ExcludedCostItemCount` is surfaced.
- Draft pricing recalculation from stored snapshot — proves `Lines`
  untouched, only pricing fields and `Revision` change.
- Draft refresh — proves `Lines`/`CostSubtotal`/`ExcludedCostItemCount`
  replaced, pricing recomputed against the new `CostSubtotal` using
  existing `PricingMode`/`PricingRate`, `Version` unchanged, `Revision`
  incremented, `RefreshedAt` updated.
- Refresh failure (mixed currency or zero-eligible discovered at refresh
  time) leaves the draft's existing `Lines`/pricing completely untouched —
  no partial update.
- Optimistic-concurrency rejection: `RefreshEstimate`/`RecalculatePricing`/
  `FinalizeEstimate` called with a stale `expectedRevision` returns a
  conflict error, no write occurs, document remains `draft` and unchanged
  (§16.1/§16.2/§16.3 — `FinalizeEstimate` added to this list per the second
  review round).
- **Stale-review finalize scenario (§16.3's worked example, as a concrete
  test):** draft at `Revision 4` → another mutation (refresh or pricing
  change) advances it to `Revision 5` → `FinalizeEstimate(...,
  expectedRevision: 4)` → rejected 409, document remains `draft` at
  `Revision 5` → `FinalizeEstimate(..., expectedRevision: 5)` → succeeds,
  `Status` becomes `finalized`.
- New-version creation — proves a fresh snapshot is taken and pricing
  defaults carry forward from the source unless overridden; proves the new
  document starts with `Revision = 0`; proves it only succeeds when the
  source is `finalized`.
- **New-version creation rejected when the source is still a `draft`** —
  `ErrEstimateMustBeFinalizedBeforeNewVersion`, returned as the FIRST check
  in `CreateNewVersion` (§17 step 2), before any snapshot-building or
  version-number allocation is attempted (revised per the second review
  round — this is no longer merely "the partial-unique-draft index rejects
  it," it is an explicit, immediately-returned domain error).
- Finalized-version immutability — `RefreshEstimate`/`RecalculatePricing`/
  any mutation attempt against a `finalized` Estimate returns an error
  (409, wrong status — not a Revision mismatch, since these paths reject on
  `Status != draft` before ever consulting `Revision`), no write occurs.
- Finalize idempotency — calling `FinalizeEstimate` twice on the same
  already-finalized Estimate returns the unchanged Estimate both times,
  with no error and no second write, **regardless of what
  `expectedRevision` the second call supplies** (proves the idempotency
  carve-out in §16.3 step 2 — an already-finalized document short-circuits
  before the Revision check, since a retry cannot know the frozen
  post-finalize Revision value).

---

## 27. Testcontainers integration-test matrix

- Estimate Create/FindByID/ListByProject round-trip.
- Tenant-scoped `FindByID` (cross-tenant → not-found sentinel).
- Parent-scoped list (`ListByProject`).
- Version-uniqueness index: attempt to insert a duplicate
  `{companyId, projectId, version}` directly at the repository level,
  confirm the driver returns a duplicate-key error.
- **One-draft-per-Project partial index**: attempt to insert a second
  `status: draft` document for the same `{companyId, projectId}`, confirm
  the driver returns a duplicate-key error on the partial index; confirm
  inserting a second `status: finalized` document for the same Project
  succeeds without conflict (proving the index is correctly scoped to
  drafts only, not all statuses).
- Version ordering/latest lookup: insert versions out of creation-order,
  confirm `MAX(version)` query returns the numerically highest.
- `GET /estimates/latest?projectId=...` returns the correct document in
  both states: while a draft exists (returns the draft, since its Version
  is always highest) and after the draft is finalized with no new draft yet
  created (returns that finalized version).
- **New-version-creation precondition**: `CreateNewVersion` against a
  `draft` source returns `ErrEstimateMustBeFinalizedBeforeNewVersion`
  immediately, with no document created and no version-number allocation
  attempted (proves §17 step 2 runs first, at the repository/HTTP layer,
  not just in an isolated unit test).
- **Concurrent version creation**: finalize a source Estimate, then fire N
  goroutines all calling `CreateNewVersion` against that SAME now-finalized
  source simultaneously; assert exactly ONE succeeds (creating `Version =
  max_before+1` as a new draft) and the other N-1 are rejected — per §21
  step 4, this rare race (two concurrent callers both passing the
  finalized-source precondition check before either has written its new
  draft) is resolved by the `{companyId, projectId}` partial unique-draft
  index, not retried, so the N-1 losers receive a 409 directly rather than
  eventually succeeding with a later version number. (Genuine
  same-version-number-race testing — two DIFFERENT already-finalized
  sources both attempting to create "the next version" for the same
  Project at once, which IS retried per §21 steps 3/5 — is covered
  separately: fire M goroutines each finalizing a distinct starting draft
  first, then all racing `CreateNewVersion`; assert the resulting version
  numbers are exactly `{max_before+1, ..., max_before+M}` with no
  duplicates, proving the bounded-retry-on-version-number-race path.)
- **Concurrent draft mutation**: fire two goroutines, one calling
  `RefreshEstimate` and one calling `RecalculatePricing`, against the same
  draft, both reading the same starting `Revision`; assert exactly one
  succeeds and the other receives a conflict on the stale `Revision`.
- Embedded `Lines` round-trip (multiple lines, including one with a nil
  `WorkItemID`).
- Independent `Money` values round trip (`CostSubtotal`,
  `ProposedSellingPrice`, etc.).
- `RateBPS`/`Revision` round trip (both as plain integers).
- Refresh persists `RefreshedAt` and increments `Revision` correctly.
- Finalize transition persists `Status`/`FinalizedAt` correctly.
- Cross-tenant not-found behavior for every read/write path.

Per the M1-M3 established lesson: any test relying on a unique index must
explicitly call `EnsureIndexes(ctx)` first.

---

## 28. Full HTTP tenant-isolation acceptance matrix

Company A + User A, Company B + User B:

- A creates an Estimate under Project A.
- B cannot `GET` A's Estimate, or `GET /estimates/latest?projectId=<A>` →
  404.
- B cannot `POST .../refresh`, `PATCH .../pricing`, `POST .../versions`, or
  `POST .../finalize` on A's Estimate → 404.
- B cannot `POST /estimates` under Project A → 404.
- `GET /estimates?projectId=<Project A>` as B → 404, not `[]`.
- List results never contain another company's Estimates.
- `companyId` spoofing never affects persisted ownership.
- B cannot access any historical version belonging to A.
- Version/refresh/finalize operations remain tenant-scoped: any of these on
  `{A's estimate id}` as B → 404.

---

## 29. Snapshot immutability acceptance matrix

Through real HTTP + real Mongo:

1. Create Project.
2. Create CostItems with `Estimated` values.
3. `POST /estimates` → Estimate V1 (draft).
4. Record V1's snapshotted `Lines`/`CostSubtotal`/totals and `Revision`.
5. `PATCH /cost-items/{id}/lifecycle` to change one CostItem's `Estimated`.
6. `GET /estimates/{v1 id}`.
7. Assert V1 is byte-for-byte unchanged from step 4's recorded values (a
   mere GET never picks up the live change).
8. `POST /estimates/{v1 id}/refresh` (supplying the `Revision` from step 4).
9. Assert `Lines`/`CostSubtotal` now reflect the updated `CostItem.Estimated`
   from step 5; `Version` is unchanged; `Revision` incremented by 1;
   `RefreshedAt` set.
10. `POST /estimates/{v1 id}/finalize` (supplying the `Revision` from step
    9 — the value after the step-8 refresh).
11. Change another CostItem's `Estimated` again.
12. `POST /estimates/{v1 id}/refresh` → rejected (409, wrong status —
    finalized documents reject before any Revision check).
13. `GET /estimates/{v1 id}` → still reflects step 9's values, unaffected
    by step 11's change.
14. `POST /estimates/{v1 id}/versions` → succeeds (source is `finalized`,
    satisfying §17 step 2's precondition) → Estimate V2 (draft).
15. Assert V2's `Lines`/`CostSubtotal` reflect step 11's change (the fresh
    snapshot correctly picks up costs that changed after V1 finalized);
    assert V2 starts at `Revision = 0`.
16. Re-`GET /estimates/{v1 id}` again — assert V1 (finalized) still
    unchanged.
17. Attempt `POST /estimates/{v2 id}/versions` while V2 is still a `draft`
    → rejected with `ErrEstimateMustBeFinalizedBeforeNewVersion` (409),
    proving the finalized-source precondition applies uniformly, not just
    to the very first new-version call in this sequence.

Additionally:
- `Worker.DefaultRate` change does not alter an existing Estimate's
  snapshot until an explicit refresh (draft) — and never for a finalized
  Estimate, regardless of refresh attempts (which are rejected outright).
- `Material.ReferencePrice` change does not alter an existing Estimate's
  snapshot until an explicit refresh (draft) / never (finalized).
- A `LabourEntry` correction (`PATCH /labour-entries/{id}`) updates its
  linked `CostItem.Estimated`, but an already-created Estimate snapshot is
  unaffected until refresh (draft) is explicitly called.

---

## 30. Mixed-currency acceptance test

1. Create Project.
2. Create `CostItem` A with `Estimated` in MYR.
3. Create `CostItem` B with `Estimated` in SGD, same Project.
4. `POST /estimates` for this Project.
5. Assert: rejected with a clear domain error, no Estimate document created,
   no incorrect cross-currency arithmetic occurs.
6. Separately: create a Project with only MYR CostItems, successfully
   create Estimate V1, then introduce a SGD CostItem and call
   `POST /estimates/{v1 id}/refresh` — assert rejection, and assert V1's
   existing `Lines`/`Currency`/totals are completely untouched by the
   failed refresh attempt (§16.2 step 3).

---

## 31. M4 → future M5 Quotation boundary

**What M5 (Quotation) is expected to consume from M4, without implementing
it now:**

- A capability, likely named something like `FinalizedEstimateSource`
  (final naming deferred to M5's own design spec), that lets `quotations`
  retrieve a specific **finalized** Estimate's `ProposedSellingPrice` and a
  customer-safe view of the cost-basis lines (e.g. `Description`,
  `Category`, `WorkItemID` — never the underlying `SnapshottedAmount` cost
  figures, per phase1.md §24's Client-privacy boundary).
- M5 will very likely want a narrower "latest **finalized** Estimate for a
  Project" lookup than M4's `GET /estimates/latest` provides (§23) — M4
  deliberately does not build that narrower filter now, since it isn't
  needed until M5 actually needs it, and M5's own design spec is the right
  place to specify its exact requirement.
- M5 should depend on `estimates`' stable snapshot of a *finalized* version
  — never on a draft (which can still change via refresh) and never on
  live `cost_items` directly.
- `estimates` gains a new capability-exposing method in M5 (not built now);
  no import dependency runs the other direction.
- This spec does not build the Quotation model, does not decide its exact
  fields, and does not decide how "customer-safe" filtering of Estimate
  data is expressed at the interface level beyond the general principle
  above.

---

## 32. Review round — resolved decisions

The design was reviewed by the user before implementation planning. All
seven items were resolved as follows; this spec's body reflects these
decisions throughout, not the original draft's proposals where they differ.

**1. One document per Estimate version — approved**, with one wording
correction: "one document per version," not "one immutable document per
version" — drafts are mutable in place via refresh/pricing-recalculation
(§3); only a `finalized` document is truly immutable.

**2. Draft refresh without a new version — added.** `Version` is now a
purely business-meaningful commercial-revision counter. Three distinct
operations exist: `PATCH .../pricing` (pricing only, same snapshot),
`POST .../refresh` (re-read `cost_items`, same Version), and
`POST .../versions` (new Version, primarily post-finalization). A new
`Revision int64` field was added for optimistic concurrency on mutable
drafts, kept distinct from `Version` (business revision) and
`SchemaVersion` (document schema) — see §1, §16.

**3. Multiple simultaneous drafts — rejected; exactly one active draft per
Project**, enforced by a partial unique index on `{companyId, projectId}`
filtered to `status: draft` (§20). Parallel pricing exploration is achieved
by changing `PricingMode`/`PricingRate` on the one active draft, not by
persisting multiple drafts.

**4. Zero eligible Estimated CostItems — reject.** No fallback currency, no
representable "empty" Estimate. Extended (§7.2) to also reject any
`CostSubtotal <= 0` even when `Lines` is non-empty, to prevent
undefined/degenerate margin calculations (division by a zero
`ProposedSellingPrice`). Currency inference proceeds only once at least one
eligible, positive-contributing CostItem is confirmed to exist.

**5. Markup vs. margin — approved as originally proposed.** `PricingMode`
(markup | margin) is the contractor's chosen pricing *input* strategy;
selling price, gross profit, and gross margin are always calculated
*outputs* regardless of which mode was used, satisfying phase1.md §20's
mention of both concepts without requiring them to be simultaneously
active inputs. Whole-Estimate-level pricing only, no per-category markup.

**6. Contingency — removed from M4 entirely**, not deferred-with-a-
placeholder. `ContingencyRate`/`ContingencyAmount`/`AdjustedCost` are gone
from the schema and calculation chain; pricing applies directly to
`CostSubtotal` (§13, §14, §15). If validated contractor demand for
contingency emerges later, it is added as a purely additive schema change.

**7. Latest Estimate access — added.** `GET /estimates/latest?projectId=...`
returns the single highest-`Version` Estimate for a Project, as a distinct
single-resource endpoint (not a query-flag variant of the list endpoint,
to avoid one endpoint's response shape being sometimes a collection and
sometimes a single resource). `GET /estimates?projectId=...` continues to
return the full version history. Internally, `Service.GetLatestEstimate`
and a corresponding repository method exist to support this without
requiring every "current Estimate" caller to fetch and sort the full list
client-side. M5's likely narrower need (latest **finalized** specifically)
is explicitly deferred to M5's own design (§31).

### Second review round — three additional corrections

After the first round above, a second review pass caught three further
issues before implementation planning began. All three are now reflected
throughout the spec body, not just here:

**8. `FinalizeEstimate` was the one draft-mutating operation without a
`Revision` guard — fixed; it is now Revision-guarded like `refresh` and
pricing recalculation.** Without this, a contractor could review a draft's
numbers at an old `Revision`, have a concurrent mutation (another tab,
another user, a background refresh) advance the document to a new
`Revision` with different totals, and then finalize — permanently locking
in numbers they never actually saw. `POST /estimates/{id}/finalize` now
requires `expectedRevision`; a mismatch against a still-`draft` document is
rejected (409, document remains `draft`, unchanged); an already-`finalized`
document remains idempotent on retry regardless of the supplied
`expectedRevision` (a retry cannot know the frozen post-finalize value).
See §10, §16.3, §25, §26.

**9. `CreateNewVersion` was described as "not technically restricted to a
draft source" while relying on the one-draft-per-Project partial index to
reject it anyway after the fact — an inconsistency; fixed by making
"source must be `finalized`" an explicit, first-checked service-layer
precondition.** The operation now returns
`ErrEstimateMustBeFinalizedBeforeNewVersion` (409) immediately if
`source.Status != finalized`, before any snapshot-building or
version-number allocation is attempted — rather than proceeding through
most of the operation only to fail at the final write with an ambiguous
duplicate-key error the caller would otherwise have to interpret. The
partial unique-draft index remains in place as a database-level backstop
(defense-in-depth against a bug or a future code path bypassing the
service-layer check), but is no longer the primary mechanism an ordinary
caller is expected to hit. See §17 (steps renumbered, step 2 added), §21
step 0, §26, §27.

**10. Manual price/quantity override (phase1.md §20) was discussed as an
unresolved gap in §0 but never actually given a mechanism — fixed by an
explicit resolution: overrides happen upstream in `internal/costs`, not as
an Estimate-local line-override endpoint.** Price override: the existing
M3 `PATCH /cost-items/{id}/lifecycle` endpoint already lets a contractor
set `CostItem.Estimated` directly (no formula re-validation on that path);
M4's `refresh` operation (§16.2) is the mechanism that pulls a corrected
value into a draft Estimate — no new M4 endpoint is needed for cost/price
override, since both halves of the chain already exist once `refresh` is
added. Quantity override: M3 has no general quantity/unitPrice correction
endpoint for `CostItem` today (only at creation time) — this is an
explicitly named, deferred gap in `internal/costs`' own surface (a future
extension mirroring `labour`'s existing narrow quantity/rate correction
pattern), not something M4 papers over or silently declines to mention. See
§17.1.

### Third review round — two further corrections, caught during implementation planning

A third review pass — this time against the concrete implementation plan
derived from this spec, after the plan's own earlier draft had already
been through one correction round — found the design itself still
contained two real defects that a straightforward implementation would
have faithfully (and incorrectly) reproduced. Both are now fixed in the
spec body, not left as plan-only patches, since both are genuine design
decisions, not implementation-only detail:

**11. `POST /estimates` (`CreateEstimate`) was described as sharing the
same `MAX(version)+1`-with-retry allocation strategy as
`POST /estimates/{id}/versions` (`CreateNewVersion`), with "no source-version
step" as the only stated difference — this is unsafe and has been
corrected.** Retrying a version-number allocation is the right behavior
for `CreateNewVersion` (advancing to whatever the next number legitimately
is) but the wrong behavior for `CreateEstimate`, which must create Version
1 and only Version 1. Under the retry-shared strategy, two concurrent
`POST /estimates` calls could both pass an up-front "does this Project
already have an Estimate" check at `MAX(version) = 0`, one succeeds at
Version 1 and is later finalized, and the other's *retry* (triggered by
losing the Version-1 race) would re-read `MAX(version)`, now see `1`, and
succeed in creating Version 2 — entirely bypassing
`ErrEstimateMustBeFinalizedBeforeNewVersion`'s protection, since that
stray Version 2 was never created through `CreateNewVersion` at all. Fixed
by giving `CreateEstimate` its own non-retrying allocation rule (§21.1):
attempt `Version = 1` directly, exactly once; any collision on either
unique index — for any reason — maps to
`ErrEstimateAlreadyExistsForProject`, never a retry. §21 is now split into
§21.1 (`CreateEstimate`, no retry) and §21.2 (`CreateNewVersion`, bounded
retry), explicitly stated as two different strategies that must not share
one implementation, rather than one shared strategy with a footnote. See
§17 (corrected), §21.1/§21.2.

**12. §20's four MongoDB indexes had no explicit names — a real defect,
not a stylistic omission.** Two of the four index definitions (the plain
`{companyId, projectId}` index and the partial-unique `{companyId,
projectId}` draft-only index) share an identical key pattern. MongoDB's
default index-naming convention does not account for a
`partialFilterExpression`, so both would derive the exact same default
name and index creation would fail outright with a name collision —
before the system could serve a single request. Separately, even if that
were somehow avoided, the default name for the draft-only index
(`companyId_1_projectId_1`) is a literal substring of the default name for
the version-uniqueness index (`companyId_1_projectId_1_version_1`),
meaning any runtime classification of a duplicate-key error by
substring-matching the index name would misclassify every version-number
conflict as a draft collision. Fixed by requiring all four indexes to
carry explicit, mutually non-overlapping names
(`idx_estimates_company_project`, `uq_estimates_company_project_version`,
`uq_estimates_one_draft_per_project`), and by making the duplicate-key
classification logic (§21.1/§21.2) explicitly return a distinct
**unclassified** outcome — never defaulted to the retriable
version-conflict case — when a duplicate-key error matches neither known
name. See §20 (corrected), §21.1 step 2, §21.2 step 7.
