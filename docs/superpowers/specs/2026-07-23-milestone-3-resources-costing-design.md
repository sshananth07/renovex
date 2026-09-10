# Milestone 3 — Resources and Costing: Design Spec

**Status:** APPROVED — all open questions from the review round (formerly
§22) resolved below. Ready for implementation planning.

**Scope:** `internal/materials`, `internal/labour`, `internal/costs` —
models, repositories, services, Huma handlers, tenant-isolation guarantees,
and the cost-lifecycle representation that later milestones (Estimate,
Quotation, Profitability) will consume.

**Authorities:** `phase1.md` §10-19, §40-41, §55 (domain/collection source of
truth), ADR 0001 (money/decimal strategy — authoritative, not
re-litigated), ADR 0002 (module boundaries), the finalized Milestone 2
design (`2026-07-22-milestone-2-project-foundation-design.md` — pattern
this spec follows) and its implementation in `internal/work`, `internal/projects`,
`internal/spaces`.

**Explicitly out of scope for M3** (per task brief — these belong to M4+):
internal Estimate aggregate/versioning, profitability dashboard, customer
Quotations, Material Requirements, RFQs/RFQ Invitations/Supplier Offers,
Purchase Orders, supplier/customer payments, AI resource suggestion, AI
quotation generation, external Access Grants, AWS infra. M3 builds the
resource/cost foundation these consume later; it must not implement any of
their logic.

**Revision note:** this spec went through one review round (recorded in
§22). All eight flagged decisions were resolved by explicit user direction,
not silently — most notably: `POST /cost-items` now rejects
`category=labour` outright (only `CreateLabourEntry` can produce a
labour-category CostItem); the LabourEntry↔CostItem relationship is
one-directional (`LabourEntry.CostItemID` only — the earlier draft's
`CostItem.LabourEntryID` back-reference is removed as redundant); a narrow
`PATCH /labour-entries/{id}` was added (quantity/rate/date/notes only,
Estimated-only propagation, never touching Committed/Actual/Paid);
`Material.PreferredSupplierID` is removed from M3 entirely; `CostItem`
quantity×price now establishes `Estimated` only, never validated against
`Committed`/`Actual`/`Paid`; `CostItem.Category` now allows limited
correction pre-commitment; and `CostItem.Currency` is explicitly scoped as
per-record, not a claim about company-wide currency. This document
contains only the final, approved decisions — the review discussion is
preserved in §22 for traceability, not as open questions.

---

## 1. Domain models

All models include `schemaVersion int` (§52) set to `1`. All `id` fields
are Mongo ObjectID hex strings (`bson:"_id,omitempty"`), matching M1/M2.
`CompanyID` is always `string`, sourced exclusively from `Principal.CompanyID`
(never request input), matching M2 invariant §4.1-2.

### 1.1 Material (`internal/materials`) — phase1.md §10-11

```go
type Material struct {
    ID                 string
    CompanyID          string
    Name               string
    Category           string          // free-text, phase1.md does not enumerate
    Specification      string          // optional — phase1.md §10 "Specification"
    Unit               string          // e.g. "bag", "m2", "kg"
    ReferencePrice      money.Money    // phase1.md §10 calls this "Default cost"; §11 "Reference Pricing"
    ReferencePriceAsOf  time.Time      // when ReferencePrice was last set/confirmed
    CreatedAt          time.Time
    SchemaVersion      int
}
```

**No `PreferredSupplierID` field in M3** (revised per §22-F review —
overrides the earlier draft, which had included it as an unvalidated
optional string). `internal/suppliers` is M6 scope and does not exist yet;
an unvalidated foreign identifier with no `SupplierLookup` capability
behind it provides little value and risks dangling references once
`suppliers` is real. Adding `PreferredSupplierID *string` when `suppliers`
actually exists in M6 is a purely additive MongoDB schema change (new
optional field, no migration of existing documents required) — there is no
cost to deferring it. phase1.md §10/§12 both mention the concept, so it is
expected to return in M6 alongside `materials` gaining a real
`SupplierLookup`-backed validation path, not before.

**Field-by-field resolution against the brief's open questions (§4 of the
task brief):**

1. **Is Material a company-owned catalog entry?** Yes — phase1.md §10's
   example JSON has `companyId`, no `projectId`/`workItemId`. Confirmed.

2. **Is a Material reusable across multiple projects/workitems?** Yes.
   phase1.md §10: "contractors should be able to... Use company-specific
   Material prices" — a catalog, not a per-project record. Matches the
   brief's recommended assumption.

3. **Where does reference pricing live?** **Embedded directly on
   `Material`** as `ReferencePrice money.Money` + `ReferencePriceAsOf
   time.Time` — not a separate collection. phase1.md §55's collection list
   has no `material_price_history` or similar, and §11's richer example
   (reference price + observed range + contractor's last purchase price +
   "last updated") is explicitly described as a **future** capability:
   "Long term, pricing intelligence may incorporate... RFQ Responses...
   Purchase Orders... Actual Supplier Invoices" — none of which exist yet
   in M3. Building a full price-history/observed-range structure now would
   be implementing forward against data sources that don't exist. M3
   stores exactly one current reference price per Material, matching
   §10's simpler example JSON (`defaultCost` as a single embedded `Money`).

4. **Is reference price informational or authoritative for cost records?**
   **Informational only, with one exception**: it may be used as a
   **default/pre-fill suggestion** when a contractor creates a `CostItem`
   referencing that Material (a plain server-side convenience — copy the
   current value into the new record's editable input), but it is never
   read or re-applied at any later point. Once a `CostItem` is created, its
   own stored `UnitPrice`/`Estimated` is authoritative and fully
   independent of `Material.ReferencePrice` from that instant on. This
   matches phase1.md §11: "The contractor may override the reference price
   when preparing the internal Estimate. **The Estimate uses the
   contractor-confirmed value.**"

5. **Does changing Material.ReferencePrice alter historical costs? Invariant: NO.**
   Confirmed and enforced structurally: `CostItem`/`LabourEntry` never store
   a live reference (`materialId` alone is not looked up for money at read
   time) — every cost record snapshots its own `Money` fields at creation.
   Updating `Material.ReferencePrice` only ever affects the pre-fill shown
   for *future* `CostItem` creation. Directly matches phase1.md §27's
   identical invariant for Quotations ("They should not silently change
   when Material prices change... Quotation Items should contain pricing
   snapshots") — extended here to the cost layer one level earlier in the
   chain, and phase1.md §51 ("Maintain Supplier Offer snapshots where
   required" — same principle).

6. **Should Material link directly to WorkItem in M3?** **No.** The
   `materials` collection has no `workItemId`/`projectId` field. Demand
   linkage (how much of a Material a given WorkItem needs) is
   `MaterialRequirement`'s job — phase1.md §32, explicitly listed as later
   procurement scope in the task brief. M3's `materials` module is a
   catalog only; the connection from a WorkItem to actual material cost in
   M3 happens through `CostItem.MaterialID` (optional reference, §1.4),
   not through `Material` itself.

### 1.2 Worker (`internal/labour`) — phase1.md §13

```go
type Worker struct {
    ID            string
    CompanyID     string
    Name          string
    Trade         string        // phase1.md §13 "Role", e.g. "Tiler" — free-text
    RateType       RateType     // hourly | daily | fixed_project | per_unit | per_square_meter
    DefaultRate    money.Money  // phase1.md §13 "Default Rate" — e.g. RM150 for RateTypeDaily
    ContactPhone  string        // optional
    ContactEmail  string        // optional
    CreatedAt     time.Time
    SchemaVersion int
}

type RateType string

const (
    RateTypeHourly        RateType = "hourly"
    RateTypeDaily         RateType = "daily"
    RateTypeFixedProject  RateType = "fixed_project"
    RateTypePerUnit       RateType = "per_unit"
    RateTypePerSquareMeter RateType = "per_square_meter"
)
```

Verbatim from phase1.md §13's "Supported rate types" list (5 values).
`DefaultRate` is a single `Money` value paired with `RateType` — e.g.
`RateType=daily, DefaultRate={15000, "MYR"}` reads as "RM150/day." No
separate `RateUnit` string field is needed; `RateType` already encodes
what the rate is per.

1. **Is Worker reusable across projects?** Yes — phase1.md §13: "Workers
   may participate in multiple Projects. Do not store Project-specific
   Labour costs directly inside the Worker record." Company-level resource,
   confirmed.

### 1.3 LabourEntry (`internal/labour`) — phase1.md §15

```go
type LabourEntry struct {
    ID            string
    CompanyID     string
    ProjectID     string           // required — stored directly, not derived via WorkItem (see §5.2 rationale)
    WorkItemID    string           // required
    WorkerID      *string          // optional — nil for ad-hoc/unnamed labour (see resolution below)
    WorkerName    string           // snapshot — see rationale below
    Trade         string           // snapshot of Worker.Trade at entry time, or manually entered if WorkerID is nil
    Quantity      quantity.Quantity // e.g. Value=8, Unit="day" — reuses internal/foundation/quantity, same as WorkItem
    Rate          money.Money      // snapshot of the unit rate actually used (see §1.3.1)
    Cost          money.Money      // = Quantity.Value × Rate, computed via ADR 0001's flow, stored (not recomputed at read time)
    CostItemID    string           // required — the one linked CostItem{Category=labour} this entry created (§1.4)
    Date          time.Time        // when the labour was performed
    Notes         string           // optional
    CreatedAt     time.Time
    SchemaVersion int
}
```

**The relationship to `CostItem` is one-directional: `LabourEntry` stores
`CostItemID`; `CostItem` stores no `LabourEntryID` back-reference** (§1.4,
revised per §22-B review — the earlier draft's mutual back-references were
a genuine inconsistency, not just redundant: it let a `CostItem` be
constructed in isolation without ever being told which `LabourEntry`
produced it, which no code path actually needs, since the only consumer of
that link direction is "given a LabourEntry, find its CostItem," never the
reverse). A unique index on `{companyId, costItemId}` on `labour_entries`
(§13) enforces 1:1 at the database level without needing the second
pointer.

**Resolution of the brief's open questions (§5 of task brief):**

1. **Worker reusable across projects** — yes, resolved in §1.2.

2. **Does LabourEntry require ProjectID as well as WorkItemID?** **Yes,
   store both directly.** Matches the brief's own reasoning exactly:
   tenant-safe querying, future M4 project-level cost aggregation without a
   parent-chain traversal through `work_items`. `work.WorkItem` already
   stores `ProjectID` directly (M2 precedent — WorkItem doesn't require a
   join to Space to know its Project), so this is consistent with the
   established pattern, not a new one. Validated at write time: `WorkItemID`
   must belong to `ProjectID` — via a **new capability the `work` module
   must expose** (§9 below), not by trusting the client-supplied pair.

3. **Should LabourEntry always reference a Worker?** **No — `WorkerID` is
   optional (`*string`).** phase1.md §16 describes Subcontractor costs as a
   distinct concept from Worker-based labour, and §14 describes the
   contractor choosing "An existing Worker... A Subcontractor... A custom
   Labour rate" — implying ad-hoc/unnamed labour without a Worker record is
   a real Phase 1 case. When `WorkerID` is nil, `WorkerName` and `Trade` are
   free-text manual input (required in that case); when `WorkerID` is set,
   `WorkerName`/`Trade` are snapshotted from the Worker at entry creation
   (next point). This keeps `LabourEntry` self-contained and readable
   without a join even when a Worker record does exist.

   **Subcontractor costs are always `CostItem`s with
   `Category=subcontractor`, never `LabourEntry`s — approved as written**
   (§22-C). The UI may still present "Existing Worker / Custom Labour /
   Subcontractor" as one entry point, but the underlying persistence path
   branches: Worker/Custom Labour → `LabourEntry` → linked labour
   `CostItem`; Subcontractor → `CostItem(category=subcontractor)` directly,
   with no `LabourEntry` involved at all. `LabourEntry`'s ad-hoc/no-`WorkerID`
   case is only for labour paid outside the Worker catalog (e.g. a one-off
   day-labourer), never for subcontracted trade work, which is inherently a
   lump-sum/negotiated cost rather than a quantity×rate calculation.

4. **How is labour quantity represented?** `quantity.Quantity` — the exact
   existing `internal/foundation/quantity` type, unit e.g. `"hour"`,
   `"day"`, `"m2"`, `"unit"` matching `RateType`. Never `float64`. Reuses
   the same type `WorkItem.Quantity` already uses — no parallel quantity
   type invented for labour.

5. **How is labour rate represented?** `money.Money` — the unit rate
   (`RM150` for a daily rate), matching ADR 0001's calculation flow exactly:
   `Quantity.Value` (decimal) × `Rate.Amount`-as-decimal → precise decimal
   intermediate → `money.RoundToMinorUnits` → final `Cost money.Money`. No
   M3 module reimplements rounding — this multiplication + rounding is a
   **new small helper added to `internal/foundation/money`** (§1.3.1)
   rather than duplicated inline in both `labour` and `costs` (they need
   the identical operation).

6. **Should LabourEntry snapshot the rate?** **Yes.** `Rate` is copied from
   `Worker.DefaultRate` at entry-creation time (or entered manually if
   `WorkerID` is nil, or overridden even when `WorkerID` is set — the
   contractor may always type a project-specific rate, matching phase1.md
   §14's "Project-Specific Rate" option). Once created, a `LabourEntry`'s
   `Rate`/`Cost` are **immutable with respect to later `Worker.DefaultRate`
   changes**, but are themselves correctable via a narrow `PATCH`
   (§1.3.2) — this is a different axis from Worker-default-rate drift.

7. **Is LabourEntry itself authoritative, or does it create/link to a
   CostItem?** **Resolved in §1.4 below — approved as the single most
   important M3 decision.**

**`Cost` field — stored, not computed at read time.** ADR 0001's flow ends
at `money.RoundToMinorUnits` producing a final `Money`; that result is
persisted on the `LabourEntry` document itself, not recalculated from
`Quantity`/`Rate` on every read. When `Quantity`/`Rate` are corrected via
`PATCH` (§1.3.2), `Cost` is recomputed at that time and re-persisted — it
is never a live-derived value computed fresh on every GET.

#### 1.3.1 New shared helper needed in `internal/foundation/money`

ADR 0001's calculation flow (`Quantity × Money unit price → precise decimal
→ money.RoundToMinorUnits → Money`) is needed identically by both `labour`
(`Quantity × Rate → Cost`) and `costs` (`Quantity × UnitPrice → Estimated`,
when a CostItem has a quantity/unit-price pair — §1.4). Rather than each
module converting `Money.Amount int64` to a `decimal.Decimal` and back
independently (a rounding-policy fork risk ADR 0001 explicitly forbids —
"No M3 module may implement its own rounding policy"), this spec proposes
one new function in `internal/foundation/money`:

```go
// CalculateLineAmount computes quantity × unitPrice, rounding once via
// RoundToMinorUnits. The only supported multiplication path for
// quantity-based cost/labour calculations — see ADR 0001.
func CalculateLineAmount(qty decimal.Decimal, unitPrice Money) Money {
    amountMajorUnits := qty.Mul(decimal.NewFromInt(unitPrice.Amount)).Div(decimal.NewFromInt(100))
    return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: unitPrice.Currency}
}
```

(Exact minor-unit divisor/shape to be nailed down against `RoundToMinorUnits`'s
existing hardcoded-2-decimal-places behavior noted in the M2 survey — flagged
as an implementation-time detail, not a design ambiguity, since
`RoundToMinorUnits` already fixes the 2-decimal-place assumption
platform-wide.) This is a **foundation-layer addition**, not a `labour`- or
`costs`-owned function — consistent with ADR 0002 ("`internal/foundation`
has no dependency on... any domain module") and avoids `labour` and `costs`
importing each other or duplicating logic.

#### 1.3.2 LabourEntry correction — `PATCH /labour-entries/{id}` (approved, §22-D)

A narrow update path exists, correcting exactly four fields: `quantity`,
`rate`, `date`, `notes`. **`workerId` reassignment and `category`-equivalent
changes are never allowed** — a correction is for fixing a typo in the
figures already entered (e.g. "80 hours" mistyped for "8 hours"), not for
re-describing what the entry represents.

When `quantity` and/or `rate` change:
1. Recompute `Cost = money.CalculateLineAmount(NewQuantity.Value, NewRate)`.
2. Update the linked `CostItem`'s `Estimated` field to the new `Cost`
   value — **`Estimated` only**. `Committed`/`Actual`/`Paid` on the linked
   `CostItem` are never touched by a `LabourEntry` correction, since they
   represent later lifecycle facts (a supplier/subcontractor commitment,
   an incurred cost, a payment) that are independent of a data-entry fix
   to the original planning-time quantity/rate.
3. Persist the updated `LabourEntry` (`Quantity`, `Rate`, `Cost`) only
   after the linked `CostItem`'s `Estimated` update succeeds — same
   write-ordering principle as creation (§1.4/§9.4): the referenced side
   (`CostItem`) is written first. If the `LabourEntry` write then fails,
   the `CostItem`'s `Estimated` value has already moved to the new figure
   even though the `LabourEntry` shows the old one — a temporary
   inconsistency window, not a corrupted state (the next successful
   `PATCH` retry converges both). This is the same best-effort
   compensation category as creation (§22-B), not a stronger guarantee;
   building distributed-transaction infrastructure for this in M3 would be
   exactly the kind of overbuilt accounting-ledger machinery the task
   brief warns against.

### 1.4 CostItem (`internal/costs`) — phase1.md §19, §39-41

**This is the foundational architecture decision for M3 (task brief §6/§7).
Approved as follows:**

phase1.md §19 states plainly: "The simple Expense concept should be
replaced with a broader `CostItem` model. **Every Project Cost** belongs to
a category," and lists categories including `Material` and `Labour`
explicitly alongside `Subcontractor`/`Equipment`/`Transport`/etc. `CostItem`
is the **universal cost ledger** — not a residual "everything except
Material and Labour" bucket.

**Decision (approved, §22-A): `CostItem` is the single authoritative
Project-cost ledger for every category, including Material and Labour.
`LabourEntry` does not independently hold authoritative cost data —
creating a `LabourEntry` creates exactly one linked `CostItem` with
`Category=labour`, and `LabourEntry.CostItemID` references it (one
direction only, §1.3).**

`LabourEntry.Cost` is a **derived/snapshotted labour calculation** — the
quantity×rate result as computed and displayed on the LabourEntry record
itself. `CostItem.Estimated` (on the linked labour CostItem) is the
**authoritative ledger value** that M4 rollups actually sum. The two values
are numerically equal at all times (kept in sync by §1.3.2's correction
flow), but they answer different questions: `LabourEntry.Cost` answers
"what does this specific labour record work out to," `CostItem.Estimated`
answers "what does the project cost ledger say," and only the latter is
what M4/M5 ever read for aggregation.

**Enforcement: `POST /cost-items` rejects `category=labour` outright**
(400/422 at the handler/service validation layer, before any repository
write). This closes the gap the earlier draft left open — without this
rejection, a contractor could create a `CostItem{Category=labour}` through
the public endpoint with no corresponding `LabourEntry`, breaking the
promised 1:1 invariant (§13's unique index only enforces uniqueness on the
`labour_entries` side; it does nothing to stop an orphaned labour
`CostItem` from being created directly). **Labour costs enter the ledger
through exactly one path: `CreateLabourEntry → LabourCostRecorder →
CostItem(category=labour)`.** No other creation path may produce a
labour-category `CostItem`.

Why this direction and not the reverse (CostItem is "everything except
labour/material"):
- phase1.md §41's Estimated-vs-Actual comparison table shows Materials,
  Labour, Subcontractors, Transport, Other **as line items of the same
  shape**, aggregated together into one "Estimated Cost" / "Current Actual
  Cost" total — implying one underlying ledger model spans all categories,
  not two structurally different models glued together at read time by M4.
- This avoids the alternative's real cost: if `LabourEntry` held its own
  independent authoritative `Cost` and `CostItem` separately held
  Material/Subcontractor/etc., M4's aggregation would need to know to sum
  `labour_entries.cost` **plus** `cost_items.amount` **excluding**
  `Category=labour` from the second sum — a fragile, easy-to-get-wrong
  join contract. A single ledger (`cost_items`) that M4 can sum by
  `{companyId, projectId}` with no category-exclusion logic is materially
  simpler.

```go
type CostItem struct {
    ID            string
    CompanyID     string
    ProjectID     string
    WorkItemID    *string        // optional — phase1.md §40 "and where possible: workItemId"
    Category      CostCategory
    Description   string
    Quantity      *quantity.Quantity // optional — nil for lump-sum costs (subcontractor, permit, professional fee)
    UnitPrice     *money.Money       // optional — required iff Quantity is set
    MaterialID    *string            // optional — set when Category=material and sourced from a catalog Material; MUST be nil unless Category=material (§1.4.1)
    Estimated     *money.Money       // nil until an estimated amount is recorded
    Committed     *money.Money       // nil until a committed amount is recorded
    Actual        *money.Money       // nil until an actual amount is recorded
    Paid          *money.Money       // nil until a paid amount is recorded — cumulative amount paid to date (§1.5)
    Currency      string             // this CostItem's own currency; all of its Money fields must share it (§1.4.2 — not a company-wide claim)
    Date          time.Time          // when this cost record was created/relevant
    Notes         string             // optional
    CreatedAt     time.Time
    SchemaVersion int
}

type CostCategory string

const (
    CostCategoryMaterial          CostCategory = "material"
    CostCategoryLabour            CostCategory = "labour"
    CostCategorySubcontractor     CostCategory = "subcontractor"
    CostCategoryEquipment         CostCategory = "equipment"
    CostCategoryTransport         CostCategory = "transport"
    CostCategoryPermit            CostCategory = "permit"
    CostCategoryProfessionalFee   CostCategory = "professional_fee"
    CostCategoryUtility           CostCategory = "utility"
    CostCategoryMiscellaneous     CostCategory = "miscellaneous"
)
```

Verbatim category list from phase1.md §19's 9-category enumeration. Note:
`LabourEntryID` is **not** a field on `CostItem` (removed — §1.3's
one-directional relationship).

#### 1.4.1 `MaterialID` requires `Category=material` — enforced at creation

**Revised from the earlier draft** (which left this as an unenforced edge
case): if `materialID != nil` at `CreateCostItem` time, `Category` **must**
equal `material`, or the request is rejected. A `CostItem` referencing a
catalog Material under any other category is not a plausible real case and
allowing it silently only invites confused reporting later.

#### 1.4.2 `Currency` scope — clarified (§22, second correction)

`CostItem.Currency` means **the canonical currency for this individual
CostItem and all of its own lifecycle Money values** — it is not yet a
claim that this equals the company's single operating currency, since
`Company` (M1) has no `Currency`/`DefaultCurrency` field today (that part
of the Company profile was deliberately deferred). M3 enforces: `Estimated`,
`Committed`, `Actual`, `Paid`, `UnitPrice` all share one `Currency` **within
one `CostItem`** — it does not enforce, or claim to enforce, that every
`CostItem` under one company shares a single currency across the company's
entire cost history. That stronger constraint can only be meaningfully
added once `Company.Currency` actually exists as a field to validate
against.

**Where `Quantity`/`UnitPrice` are used vs. a flat lifecycle-amount entry:**
phase1.md's examples show both shapes — §19's category list implies
itemized costs (Material: quantity × unit price), while §16's Subcontractor
example (`Estimated Cost: RM4,500`) and §19's Permit/Professional Fee
categories are inherently lump-sum with no natural quantity. Rather than
forcing every `CostItem` through a quantity×price calculation,
**`Quantity`/`UnitPrice` are both optional and used together or not at
all**.

**Revised scope of what `Quantity × UnitPrice` establishes (§22, first
correction — narrows the earlier draft, which incorrectly implied all four
lifecycle fields should be validated against the line calculation):**
`money.CalculateLineAmount(Quantity.Value, *UnitPrice)`, when both are
supplied, establishes **`Estimated` only** — either as a creation-time
default (if `Estimated` was not separately supplied) or validated equal to
an explicitly-supplied `Estimated` (reject a mismatched pair). It has **no
relationship to `Committed`, `Actual`, or `Paid`**, which are independently
authoritative once set: `Quantity=100, UnitPrice=RM10` implies
`Estimated=RM1,000`, but `Committed=RM950`, `Actual=RM1,080`, `Paid=RM500`
are all valid regardless of what the line calculation produced — they
represent genuinely different downstream facts (an accepted supplier
price, an actual invoice, a partial payment), not recomputations of the
same quantity×price formula.

### 1.5 Cost lifecycle model — approved (task brief §7)

**Decision: (b) — four separate, independently-nilable monetary fields on
one `CostItem` record (`Estimated`, `Committed`, `Actual`, `Paid`), not (a)
a single status field and not (c) separate immutable event records.**

Reasoning, directly from the task brief's own worked example and phase1.md:

- phase1.md §39's worked example shows all four values **coexisting** for
  the same cost line (Estimated RM4,100 → Supplier Offer Accepted RM4,350 →
  Committed RM4,350 → Invoice RM4,420 → Actual RM4,420), and §41's
  Estimated-vs-Actual table displays Estimated and Actual **side by side
  for the same category simultaneously**. A single `status` enum
  (`estimated | committed | actual | paid`) cannot represent "Estimated
  RM1,000 and Actual RM980 both known at once." Model (a) is rejected for
  losing information §41 requires.
- Model (c) (immutable event-per-stage records) is rejected as
  over-engineering for M3: phase1.md §50 explicitly says audit needs "does
  not require full Event Sourcing... Use Current State + Audit Events" —
  `CostItem` **is** the current-state record; a future `audit_events`
  collection (already scaffolded) is where event history belongs if ever
  needed.
- Model (b) directly matches the brief's own worked numeric example and
  requires no new concept beyond four nilable `Money` fields.

**What each stage means, and which M3 entities can actually populate them:**

| Stage | Meaning | Set by | Populated in M3? |
|---|---|---|---|
| `Estimated` | Contractor's planning-time cost assumption for this line, before any commitment exists. | Contractor input at `CostItem`/`LabourEntry` creation, or a Material's `ReferencePrice` used as a pre-fill suggestion (§1.1.4). | **Yes** — the only stage M3's `CreateCostItem`/`CreateLabourEntry` endpoints populate by default. |
| `Committed` | An accepted/contractual commitment to pay a specific amount (e.g. accepted Supplier Offer, signed Subcontractor agreement). phase1.md §19/§39 tie this explicitly to Supplier Offer acceptance / future PO. | A future Procurement module (M6: RFQ → Supplier Offer → accepted offer) or, for non-procurement categories (labour, subcontractor agreement), manual contractor entry. | **Representable, but M3 does not fabricate a commitment workflow.** M3 exposes a plain lifecycle-update path to set `Committed` manually, but does not build any Supplier-Offer-driven auto-population, since Suppliers/RFQs don't exist yet. |
| `Actual` | Cost genuinely incurred (invoice received, work completed and verified). phase1.md §40. | Manual contractor entry once work/purchase has actually happened. | **Yes** — a core M3 write path, matching phase1.md §40's "Actual Cost Tracking" as core M3 scope. |
| `Paid` | **Cumulative amount paid to date** for this CostItem (approved definition, §22-E) — not a single payment event. E.g. `Paid=RM500`, another RM200 paid → `Paid=RM700` (the caller supplies the new running total, not a delta). phase1.md §19 "Supplier Paid." | Manual contractor entry, overwriting the running total each time a payment is made. | **Yes, minimally** — a plain manual field update. Full payment tracking/reconciliation (phase1.md §42, "Customer Payment Tracking") is explicitly M7 scope — this is the *cost side* (money the contractor pays out), and M3 exposes no payment-record linkage, no per-payment history, no reconciliation logic; only the running total. Individual payment events are future audit/payments-infrastructure scope, not M3's. |

**All four fields are independently settable, in any order — no enforced
state-machine progression required in M3** (e.g. a contractor may set
`Actual` before ever setting `Committed`, for a simple lump-sum Permit fee
that has no commitment phase at all). This matches phase1.md §19's own
framing ("For Phase 1, the user interface may focus primarily on:
Estimated, Actual. **The underlying domain should support the complete
lifecycle**").

**Currency validation across the four fields**: all four non-nil `Money`
fields on one `CostItem`, plus `UnitPrice` if set, must share the same
`Currency` (§1.4.2's per-record scope) — validated at every write via
`Money`'s existing currency-compatibility check (ADR 0001).

This design does not implement profitability or cash-flow calculations in
M3, but the four-field shape makes both possible later without a schema
change: profitability = selling price − `Actual` (or `Estimated`
pre-construction); cash flow = tracked separately via `Paid` vs. incoming
customer payments (M7).

---

## 2. Collection ownership

| Module | Collection | Owns exclusively |
|---|---|---|
| `materials` | `materials` | Yes |
| `labour` | `workers`, `labour_entries` | Yes (both) |
| `costs` | `cost_items` | Yes |

No `resources` collection (§3). Matches phase1.md §55's collection list
exactly — every M3 collection name (`materials`, `workers`,
`labour_entries`, `cost_items`) already appears there verbatim; nothing
invented.

`labour` owning two collections (`workers` and `labour_entries`) is a
deliberate departure from M2's one-module-one-collection pattern, justified
because Worker and LabourEntry are tightly coupled (LabourEntry snapshots
*from* Worker, §1.3.6) and separating them into two modules would require a
cross-module capability interface for a relationship that is otherwise a
single atomic operation inside one service method
(`CreateLabourEntry` reading `Worker.DefaultRate` to seed the snapshot).
ADR 0002 requires a module not reach into *another module's* collection —
it does not require exactly one collection per module; `identity` already
owns two collections (`users`, `auth_sessions`) precedent-wise.

---

## 3. Resource: conceptual, not persisted — confirmed

**No `resources` collection, no polymorphic `Resource` domain type, no
generic Resource repository/CRUD.** Confirmed against phase1.md and the
existing M2 collection list:

- phase1.md §17's own diagram immediately clarifies Resource as an
  umbrella: "A Work Item may require multiple Resources... Material...
  Labour... Equipment... Transport" — describing categories, not one
  entity type with a discriminator field.
- phase1.md §55's authoritative collection list has `materials`, `workers`,
  `labour_entries`, `cost_items` — **no `resources`**.
- A polymorphic Resource type would need a `resourceType` discriminator and
  type-specific payload, exactly the shape phase1.md already uses for
  **Access Grants** (`resourceType`/`resourceId`, §2) where polymorphism is
  genuinely needed. Costs/Resources don't have that same
  cross-cutting-attachment shape — each concrete type (Material, Worker,
  LabourEntry, CostItem) has its own distinct field set and its own
  distinct owning module, which is precisely when concrete domain types
  are preferable to a premature generic abstraction.

"Resource" therefore remains a purely conceptual grouping term used in
documentation and UI copy — `Material`, `Worker`+`LabourEntry`, and
`CostItem` are the concrete persisted types that realize it.

---

## 4. Tenant ownership

Identical invariants to M2 §4 (1-7), extended to M3's three new modules
with no exceptions:

1. Every M3 document stores `companyId` directly, sourced only from
   `Principal.CompanyID`.
2. `companyId` is never accepted from request body/query/path — Huma DTOs
   never declare the field.
3. Every parent-reference field (`projectId`, `workItemId`, `materialId`
   where used for pre-fill, `workerId`) is validated at write time to
   exist and belong to `Principal.CompanyID` before the child record is
   created — via the owning module's capability interface.
4. Every direct-by-ID repository method is tenant-scoped:
   `(ctx, companyID, id)`.
5. Cross-tenant existence is indistinguishable from non-existence — same
   404, same sentinel error.
6. List endpoints are always company-scoped; parent-filtered lists
   validate the parent belongs to the caller's company **before** running
   the list query — foreign parent → 404, never `[]`.
7. ID substitution across tenants never succeeds.

**New for M3 — same-tenant lineage rule extended one level deeper:** a
`LabourEntry`/`CostItem`'s `WorkItemID` must belong to its own `ProjectID`
(not merely to the same company) — the identical pattern M2 established
for Space→WorkItem lineage (M2 §4.8), now applied one hop further down the
chain. See §9.3 for the capability call that proves this in one query.

---

## 5. Module service responsibilities

Each module follows the exact M1/M2 shape: `model.go` (or split per entity,
e.g. `worker.go` + `labour_entry.go`), `repository.go` (interface +
sentinel errors), `repository_mongo.go` (+ `EnsureIndexes`), `service.go`
(business logic + capability interfaces), `handler.go` (Huma DTOs +
routes).

### 5.1 `materials.Service`

- `CreateMaterial(ctx, companyID, name, category, specification, unit, referencePriceAmount, currency) (Material, error)`
- `GetMaterial(ctx, companyID, materialID) (Material, error)`
- `ListMaterials(ctx, companyID) ([]Material, error)`
- `UpdateMaterial(ctx, companyID, materialID, ...) (Material, error)` — may
  update `ReferencePrice`/`ReferencePriceAsOf`; never retroactively touches
  any `CostItem` (§1.1.5).
- Exposes **`MaterialLookup`** (consumed by `costs`, for the optional
  `CostItem.MaterialID` reference — validate it belongs to the company
  when supplied, and to fetch the current `ReferencePrice` as a creation-time
  pre-fill only).
- Consumes nothing — `materials` has no parent-validation dependency on
  any other M3/M2 module (company-level catalog only, no project/work-item
  linkage per §1.1.6).

### 5.2 `labour.Service`

- `CreateWorker(ctx, companyID, name, trade, rateType, defaultRateAmount, currency, contactPhone, contactEmail) (Worker, error)`
- `GetWorker(ctx, companyID, workerID) (Worker, error)`
- `ListWorkers(ctx, companyID) ([]Worker, error)`
- `UpdateWorker(ctx, companyID, workerID, ...) (Worker, error)` — may update
  `DefaultRate`; never retroactively touches any existing `LabourEntry`
  (§1.3.6, §14).
- `CreateLabourEntry(ctx, companyID, projectID, workItemID, workerID *string, workerName, trade, quantityValue, quantityUnit string, rateAmount *int64, currency string, date time.Time, notes string) (LabourEntry, error)`:
  1. Validate `projectID` exists and belongs to company (via
     `ProjectLookup`, consumed from `projects`).
  2. Validate `workItemID` belongs to `projectID` **and** the company in
     one call (via the new `WorkItemLookup` capability `work` must expose
     — §9.3).
  3. If `workerID != nil`: validate it belongs to the company (via this
     module's own repository — no cross-module call needed, Worker is
     owned by the same `labour` module); snapshot `WorkerName`/`Trade` from
     the fetched Worker; if `rateAmount` is not explicitly supplied,
     default `Rate` to `Worker.DefaultRate`, else use the explicit override
     (§1.3.6 — contractor may always override).
  4. If `workerID == nil`: `workerName`/`trade`/`rateAmount` become
     required manual inputs (ad-hoc labour, §1.3.3).
  5. Construct `quantity.New(quantityValue, quantityUnit)`; reject on parse
     failure or non-positive value (same validation shape as
     `work.CreateWorkItem`).
  6. Compute `Cost = money.CalculateLineAmount(Quantity.Value, Rate)`
     (§1.3.1).
  7. **Create the linked `CostItem` first**, via `LabourCostRecorder`
     (§9.4) — `Category=labour`, `Estimated=Cost`. Only after that write
     succeeds, persist the `LabourEntry` (with the returned `CostItemID`
     stored on it). If the `LabourEntry` write then fails, the created
     `CostItem` is a harmless orphan (§22-B) — log it clearly; do not
     attempt automatic retry-to-consistency beyond a best-effort
     `LabourCostRecorder.DeleteProvisionedLabourCost(costItemID)`
     compensation call, itself logged clearly if it also fails.
- `UpdateLabourEntry(ctx, companyID, labourEntryID, quantityValue, quantityUnit *string, rateAmount *int64, date *time.Time, notes *string) (LabourEntry, error)`
  (§1.3.2) — corrects `quantity`/`rate`/`date`/`notes` only; recomputes
  `Cost`; updates the linked `CostItem.Estimated` first (via
  `LabourCostRecorder`'s update counterpart), then the `LabourEntry`
  itself, same ordering principle as creation. Never touches `workerId` or
  the linked `CostItem`'s `Committed`/`Actual`/`Paid`.
- `GetLabourEntry(ctx, companyID, labourEntryID) (LabourEntry, error)`
- `ListLabourEntriesByProject(ctx, companyID, projectID) ([]LabourEntry, error)`
- `ListLabourEntriesByWorkItem(ctx, companyID, workItemID) ([]LabourEntry, error)`
- Exposes nothing to other M3 modules (labour is a "leaf" consumer of
  `work`/`projects`/`costs`, produces no capability anything else needs in
  M3).
- Consumes `ProjectLookup` (from `projects`), `WorkItemLookup` (new, from
  `work`, §9.3), and `LabourCostRecorder` (new, from `costs`, §9.4).

### 5.3 `costs.Service`

- `CreateCostItem(ctx, companyID, projectID, workItemID *string, category, description, quantityValue *string, quantityUnit *string, unitPriceAmount *int64, estimatedAmount, committedAmount, actualAmount, paidAmount *int64, currency string, materialID *string, date time.Time, notes string) (CostItem, error)`:
  1. **Reject `category == labour` outright** (§1.4) — labour-category
     `CostItem`s may only be created via `LabourCostRecorder`, never
     through this public path.
  2. Validate `projectID` (via `ProjectLookup`).
  3. If `workItemID != nil`: validate it belongs to `projectID` and the
     company (via `work`'s new `WorkItemLookup`, §9.3).
  4. If `materialID != nil`: validate it belongs to the company (via
     `materials`' `MaterialLookup`, §9.2), **and require `category ==
     material`** (§1.4.1 — enforced, not merely suggested).
  5. If both `quantityValue`/`unitPriceAmount` are supplied: construct
     `quantity.New`; the resulting line amount establishes/validates
     `Estimated` only (§1.4.2) — never compared against
     `Committed`/`Actual`/`Paid`.
  6. At least one of the four lifecycle `Money` fields must be non-nil (an
     empty `CostItem` with no financial data at all is rejected).
  7. Validate currency consistency across every non-nil `Money` field on
     this record (§1.4.2 — per-record only).
- `GetCostItem(ctx, companyID, costItemID) (CostItem, error)`
- `ListCostItemsByProject(ctx, companyID, projectID) ([]CostItem, error)`
- `ListCostItemsByWorkItem(ctx, companyID, workItemID) ([]CostItem, error)`
- `UpdateCostItemLifecycle(ctx, companyID, costItemID, stage CostStage, amount money.Money) (CostItem, error)`
  — sets exactly one of the four fields per call (`stage` ∈
  `{estimated, committed, actual, paid}`); validates currency matches the
  record's existing `Currency`. `Paid` amounts are the new running total,
  not a delta (§1.5). This is the one mutation path for lifecycle amounts.
- `UpdateCostItemDetails(ctx, companyID, costItemID, description, notes, category *CostCategory) (CostItem, error)`
  — non-financial fields, **plus limited `Category` correction** (§1.4.3
  below). Money fields are mutable **only** via the lifecycle-specific
  method above, never through this generic path.
- Exposes **`LabourCostRecorder`** — a narrow write-capability consumed
  only by `labour` (§9.4): `RecordLabourCost` (create) and
  `UpdateLabourCostEstimate` (correction, backing §1.3.2) plus
  `DeleteProvisionedLabourCost` (compensation). Distinct from the public
  `CreateCostItem` used by the HTTP handler — category is fixed to
  `labour` by the interface's own signature, making it structurally
  impossible for `labour` to create a cost item outside the labour
  category through this path.
- Consumes `ProjectLookup` (`projects`), `WorkItemLookup` (`work`, §9.3),
  `MaterialLookup` (`materials`, §9.2).

#### 1.4.3 `CostItem.Category` — limited correction (revised, §22-G)

**Labour-generated `CostItem`s (`Category=labour`) remain immutable** — no
path ever changes a labour CostItem's category, since it is only ever
created by `LabourCostRecorder` and its category is structurally fixed.

**Manually-created (non-labour) `CostItem`s may have their `Category`
corrected via `UpdateCostItemDetails`, but only while all three of
`Committed`, `Actual`, and `Paid` are still nil.** Once any one of them
becomes non-nil — i.e. execution or financial commitment has begun —
`Category` locks permanently. This allows a genuine data-entry mistake
(wrong category picked at creation) to be fixed before it has any
downstream financial consequence, while preventing a category change from
retroactively misrepresenting a cost that a report or rollup has already
treated as committed/incurred/paid under its original category.
`MaterialID != nil ⟹ Category == material` (§1.4.1) is re-validated on any
category-correction attempt too — a correction cannot detach `Category`
from an existing `MaterialID` reference.

---

## 6. Entity relationship diagram

```text
Project (M2)
  │
  ├──< WorkItem (M2)
  │      │
  │      ├──< LabourEntry      (labour_entries.companyId, .projectId, .workItemId,
  │      │       │               .workerId OPTIONAL → Worker, .costItemId → CostItem)
  │      │       └── 1:1 (one direction: LabourEntry.CostItemID) ──> CostItem (Category=labour)
  │      │
  │      └──< CostItem          (cost_items.companyId, .projectId,
  │                               .workItemId OPTIONAL, .materialId OPTIONAL → Material,
  │                               required Category=material when materialId set)
  │
Company (M1)
  ├──< Material                 (materials.companyId — no project/workItem link)
  └──< Worker                   (workers.companyId — no project link; reusable)
```

Cardinalities:
- Project 1—* LabourEntry (via WorkItem; LabourEntry stores ProjectID
  directly for query convenience, §5.2, but the authoritative lineage
  check is WorkItem→Project)
- Project 1—* CostItem (WorkItemID optional — a CostItem may be
  project-level with no specific WorkItem, e.g. a project-wide permit fee)
- WorkItem 1—* LabourEntry, WorkItem 1—* CostItem
- LabourEntry 1—1 CostItem, **enforced one-directionally**: every
  LabourEntry has exactly one linked CostItem with Category=labour
  (`LabourEntry.CostItemID`, unique-indexed, §13); a Category=labour
  CostItem can only exist because a LabourEntry created it, but does not
  itself point back.
- Worker 0..1—* LabourEntry (optional; ad-hoc entries have no Worker)
- Material 0..1—* CostItem (optional; when set, Category MUST be material)
- Company 1—* Material, Company 1—* Worker (both company-level, no
  Project relationship)

---

## 7. Narrow cross-module capability interfaces

Consumer defines the interface; provider's concrete `*Service` satisfies it
structurally; `cmd/api` wires concrete types together. Continues the exact
M1/M2 pattern (M2 §8) with no deviation.

### 9.1 `labour` and `costs` each define `ProjectLookup`

```go
// defined independently in internal/labour and internal/costs,
// identical shape, following M2's precedent of each consumer defining its
// own copy rather than sharing one generic type (M2 §8.3's rationale
// applies unchanged)
type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}
```

Satisfied structurally by `projects.Service` (unchanged from M2 — no
modification to `projects` needed).

### 9.2 `costs` defines `MaterialLookup`

```go
// defined in internal/costs
type MaterialLookup interface {
    MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error)
    GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error) // for pre-fill only, §1.1.4
}
```

Satisfied structurally by `materials.Service`. `GetReferencePrice` returns
a `money.Money` (a value type, not a domain struct) — narrower than
returning the full `Material`, consistent with ADR 0002's "no domain-type
leakage across module boundaries" (M2 §13).

### 9.3 `labour` and `costs` each define `WorkItemLookup` — NEW capability `work` must expose

This is the one capability that does not already exist from M2 and
requires a change to `internal/work` (explicitly anticipated and permitted
by the task brief: "If this requires adding a capability method to
work.Service, that is acceptable as an M3 extension of the owning module").

```go
// defined independently in internal/labour and internal/costs
type WorkItemLookup interface {
    // Confirms workItemID belongs to companyID AND that its stored
    // ProjectID equals projectID — one call resolves both tenant
    // ownership and Project-lineage integrity in a single compound Mongo
    // filter, exactly mirroring work.SpaceLookup.SpaceBelongsToProject's
    // shape from M2 (M2 §8.4).
    WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
}
```

**New method required on `work.Service`:**
```go
// Added in M3. Satisfies labour.WorkItemLookup and costs.WorkItemLookup
// structurally.
func (s *Service) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
```
Implemented as a single compound-filtered repository query (`{_id:
workItemID, companyId: companyID, projectId: projectID}`), matching
`spaces.SpaceBelongsToProject`'s exact implementation shape from M2 — no
separate existence check followed by a lineage check, one query. Requires
one new method on `work.WorkItemRepository` (e.g. `BelongsToProject`) or
reuse of `FindByID` plus an in-service `ProjectID` comparison (repository
method preferred, matching `spaces`' precedent of pushing the compound
filter into the query itself rather than fetching-then-comparing).

**This is the only modification to any existing M2 module this spec
requires.** `work`'s package doc comment currently states "It exposes no
capability to any other module in M2" — this sentence becomes false in M3
and must be updated to document the new `WorkItemBelongsToProject` export,
mirroring how `projects`' doc comment already documents each of its
consumers.

### 9.4 `labour` defines `LabourCostRecorder`

```go
// defined in internal/labour
type LabourCostRecorder interface {
    // Creates a CostItem with Category=labour on behalf of a LabourEntry.
    // Narrower than costs' public CreateCostItem — category is implicit,
    // no materialID, no quantity/unitPrice branch (the cost is already
    // computed by labour). Returns the new CostItem's ID for LabourEntry
    // to store as its CostItemID reference.
    RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (costItemID string, err error)

    // Updates only the Estimated field of an existing labour CostItem —
    // backs LabourEntry correction (§1.3.2). Never touches
    // Committed/Actual/Paid.
    UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error

    // Best-effort compensation: deletes a just-created labour CostItem if
    // the paired LabourEntry write subsequently fails (§22-B). Failures
    // of this call are logged, not retried indefinitely.
    DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error
}
```

Satisfied structurally by `costs.Service`. This is the interface behind
§5.2 step 7 and §1.3.2's correction flow.

---

## 8. Compile-time dependency / wiring graph

```text
                    cmd/api (composition root)
                          │
                          ▼
                      projects (M2, unchanged)
                          │
                          ▼
                        work (M2 + new WorkItemBelongsToProject method)
                    ┌─────┴─────┐
                    ▼           ▼
              materials       costs ◄──────┐
              (independent,        consumed by labour via
               no M3 deps)         LabourCostRecorder
                    │               ▲
                    └──── consumed  │
                          by costs  │
                         (Material  │
                          Lookup)   │
                                    │
                                 labour
                          (consumes ProjectLookup,
                           WorkItemLookup, LabourCostRecorder)
```

Concrete construction order in `cmd/api/main.go`, extending M2's existing
order (`clientsService → projectsService → propertiesService/spacesService
→ workService`) with M3's three new services appended, respecting that
`labour` depends on `costs` (via `LabourCostRecorder`) which must therefore
be constructed **before** `labour`:

```go
// ... M2 wiring unchanged above this line ...
materialsService := materials.NewService(materialRepo)                      // no M3 deps
costsService      := costs.NewService(costItemRepo, projectsService, workService, materialsService)
                     // consumes ProjectLookup, WorkItemLookup, MaterialLookup
labourService     := labour.NewService(workerRepo, labourEntryRepo, projectsService, workService, costsService)
                     // consumes ProjectLookup, WorkItemLookup, LabourCostRecorder
```

Every service remains fully constructible in one pass, no cycle:
`materials` has zero M3-internal dependencies; `costs` depends only on
already-constructed M2 services plus `materials`; `labour` depends only on
already-constructed M2 services plus `costs`. `materials` and `costs` are
**not** siblings in the way `properties`/`spaces` were in M2 (`costs`
depends on `materials`) — this is a deliberate, narrow, one-directional
dependency (`costs → materials`, never the reverse), not a cycle risk,
since `materials.Service` never references `costs` in either its
constructor or any method signature.

**HTTP registration** appends to the existing `authedAPI` group exactly as
M2 did — no new `huma.API`/`humachi.New` instance, per the load-bearing
post-M2 decision recorded in `handoff.md` §4:

```go
materials.RegisterHandlers(authedAPI, materialsService)
costs.RegisterHandlers(authedAPI, costsService)
labour.RegisterHandlers(authedAPI, labourService)
```

Order matters only for readability here (all three are independent from
the router's perspective) — but is kept consistent with construction order.
`internal/tenanttest/router.go` must mirror this addition exactly, per its
established role as main.go's test-only twin.

---

## 9. Endpoint list

All authenticated (`RequireAuthHuma`, registered on the existing
`authedAPI` group — §8). No endpoint accepts `companyId`.

| Method | Path | Responsibility |
|---|---|---|
| POST | `/materials` | Create Material for `Principal.CompanyID` |
| GET | `/materials` | List Materials for `Principal.CompanyID` |
| GET | `/materials/{id}` | Get Material, tenant-scoped |
| PATCH | `/materials/{id}` | Update Material fields incl. reference price (never retroactive) |
| POST | `/workers` | Create Worker |
| GET | `/workers` | List Workers for `Principal.CompanyID` |
| GET | `/workers/{id}` | Get Worker, tenant-scoped |
| PATCH | `/workers/{id}` | Update Worker fields incl. default rate (never retroactive) |
| POST | `/labour-entries` | Create LabourEntry — body has `projectId`, `workItemId`, optional `workerId`; creates linked CostItem |
| GET | `/labour-entries?projectId=...` | List LabourEntries for one Project (parent validated first) |
| GET | `/labour-entries?workItemId=...` | List LabourEntries for one WorkItem (parent validated first) |
| GET | `/labour-entries/{id}` | Get LabourEntry, tenant-scoped |
| PATCH | `/labour-entries/{id}` | Correct `quantity`/`rate`/`date`/`notes` only; propagates to linked CostItem.Estimated (§1.3.2) |
| POST | `/cost-items` | Create CostItem — body has `projectId`, optional `workItemId`/`materialId`; **rejects `category=labour`** (§1.4) |
| GET | `/cost-items?projectId=...` | List CostItems for one Project (parent validated first) |
| GET | `/cost-items?workItemId=...` | List CostItems for one WorkItem (parent validated first) |
| GET | `/cost-items/{id}` | Get CostItem, tenant-scoped |
| PATCH | `/cost-items/{id}/lifecycle` | Set exactly one of `estimated`/`committed`/`actual`/`paid` (`paid` is a running total, §1.5) |
| PATCH | `/cost-items/{id}` | Update `description`/`notes`, plus limited `category` correction pre-commitment (§1.4.3) |

No delete/archive endpoints for any M3 entity, matching M2 §6's identical
deferral and reasoning (no lifecycle field phase1.md specifies for any of
these entities either).

---

## 10. Tenant-scoping strategy for every repository

Identical shape to M2 §13 — every repository interface's read/update
methods take `companyID` as an explicit parameter; `FindByID` filters
`{_id, companyId}` as one compound query; capability-interface methods
return only `(bool, error)` or narrow value types (`money.Money`), never
domain structs.

```go
// example: costs.CostItemRepository
type CostItemRepository interface {
    Create(ctx context.Context, c CostItem) (CostItem, error)
    FindByID(ctx context.Context, companyID, id string) (CostItem, error)
    ListByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error)
    ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error)
    UpdateLifecycleField(ctx context.Context, companyID, id string, stage CostStage, amount money.Money) (CostItem, error)
    UpdateDetails(ctx context.Context, companyID, id string, description, notes string, category *CostCategory) (CostItem, error)
    Delete(ctx context.Context, companyID, id string) error // used only by LabourCostRecorder's compensation path (§9.4), not exposed via HTTP
}
```

```go
// example: labour.LabourEntryRepository
type LabourEntryRepository interface {
    Create(ctx context.Context, e LabourEntry) (LabourEntry, error)
    FindByID(ctx context.Context, companyID, id string) (LabourEntry, error)
    ListByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error)
    ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error)
    UpdateCorrection(ctx context.Context, companyID, id string, quantity quantity.Quantity, rate money.Money, cost money.Money, date time.Time, notes string) (LabourEntry, error)
}
```

---

## 11. Money/Quantity calculation flows

Every M3 calculation follows ADR 0001's flow exactly, using the one new
shared helper (§1.3.1):

```text
LabourEntry.Cost:
  quantity.Quantity (hours/days/etc.)
    × Rate (Money, as decimal)
    → money.CalculateLineAmount(...)
    → precise decimal intermediate
    → money.RoundToMinorUnits (round-half-up)
    → Money{Amount int64, Currency}
    → stored on LabourEntry.Cost, and copied into the linked CostItem.Estimated
      (recomputed and re-propagated on correction, §1.3.2)

CostItem.Estimated (when Quantity/UnitPrice present):
  quantity.Quantity × UnitPrice (Money)
    → money.CalculateLineAmount(...)
    → establishes/validates Estimated ONLY — never Committed/Actual/Paid
      (§1.4.2)
```

No M3 module reimplements `RoundToMinorUnits`; no M3 module converts
`Money.Amount` to `float64` at any point; currency mismatches are rejected
via `Money`'s existing `Add`/`Subtract` error pattern extended to the new
`CalculateLineAmount` helper, with every *combination* of multiple `Money`
values on one `CostItem` validated for currency consistency in the service
layer (§1.4.2 — per-record, not company-wide).

---

## 12. Mongo BSON representations

### Money

```go
// no separate BSON doc type needed — money.Money's own bson tags
// (bson:"amount", bson:"currency") are used directly, exactly as
// materials.go's example in phase1.md §10 shows, and as ADR 0001 specifies
// (never Decimal128, never float).
```

Every `*money.Money` field (nilable lifecycle fields, optional `UnitPrice`)
is stored as `bson:"...,omitempty"` sub-document or omitted entirely when
nil — a `CostItem` with only `Estimated` set has no `committed`/`actual`/
`paid` keys in its Mongo document at all, not `null` values, keeping
partially-populated cost records lean and matching Mongo idiom.

### Quantity — reuses the exact M2 pattern, no new representation invented

```go
// defined identically in both labour/repository_mongo.go and
// costs/repository_mongo.go — each module defines its own private copy,
// exactly as work/repository_mongo.go does, no shared quantity-doc type
// exported from internal/foundation/quantity (that package remains
// infrastructure-free per ADR 0002, and each module's BSON shape is that
// module's own concern per ADR 0002's ownership rule)
type quantityDoc struct {
    Value string `bson:"value"`
    Unit  string `bson:"unit"`
}
```

Same `toQuantityDoc`/`fromQuantityDoc` conversion pattern as
`internal/work/repository_mongo.go:43-61`, same reasoning (shopspring/decimal
has no BSON marshaling, string storage avoids float precision loss). For
`CostItem`, `Quantity` is `*quantity.Quantity` (pointer, since it's
optional) — the Mongo doc's `quantity *quantityDoc` field is `omitempty`,
absent entirely for lump-sum cost items.

### Example: `labourEntryDoc`

```go
type labourEntryDoc struct {
    ID            bson.ObjectID `bson:"_id,omitempty"`
    CompanyID     string        `bson:"companyId"`
    ProjectID     string        `bson:"projectId"`
    WorkItemID    string        `bson:"workItemId"`
    WorkerID      *string       `bson:"workerId,omitempty"`
    WorkerName    string        `bson:"workerName"`
    Trade         string        `bson:"trade"`
    Quantity      quantityDoc   `bson:"quantity"`
    Rate          money.Money   `bson:"rate"`
    Cost          money.Money   `bson:"cost"`
    CostItemID    string        `bson:"costItemId"`
    Date          time.Time     `bson:"date"`
    Notes         string        `bson:"notes,omitempty"`
    CreatedAt     time.Time     `bson:"createdAt"`
    SchemaVersion int           `bson:"schemaVersion"`
}
```

`ProjectID`/`WorkItemID`/`WorkerID`/`CompanyID`/`CostItemID` are stored as
plain hex strings, never `bson.ObjectID`, matching M2's established
convention. **No `costItemDoc` field for a back-reference to LabourEntry**
— the relationship is one-directional (§1.3).

---

## 13. Required Mongo indexes

| Collection | Index | Notes |
|---|---|---|
| `materials` | `{companyId: 1}` | List/scope queries |
| `materials` | `{companyId: 1, category: 1}` | Filter catalog by category — a real query pattern per phase1.md §10 ("Category") |
| `workers` | `{companyId: 1}` | List/scope queries |
| `labour_entries` | `{companyId: 1}` | List/scope queries |
| `labour_entries` | `{companyId: 1, projectId: 1}` | List LabourEntries by Project |
| `labour_entries` | `{companyId: 1, workItemId: 1}` | List LabourEntries by WorkItem |
| `labour_entries` | `{companyId: 1, workerId: 1}` | Sparse — supports a future "labour history per Worker" query |
| `labour_entries` | `{companyId: 1, costItemId: 1}` | **UNIQUE** — enforces the 1:1 LabourEntry↔CostItem invariant at the database level (§1.3, approved per §22-B) |
| `cost_items` | `{companyId: 1}` | List/scope queries |
| `cost_items` | `{companyId: 1, projectId: 1}` | List CostItems by Project |
| `cost_items` | `{companyId: 1, workItemId: 1}` | Sparse — `workItemId` is optional |

No category/lifecycle-stage index on `cost_items` in M3 — no M3 query
filters by category or by "which lifecycle fields are set," only by
project/work-item parent, matching the brief's instruction not to add
speculative indexes without a query use case.

---

## 14. Historical financial-integrity / immutability rules

Direct answers to the task brief's §12 checklist:

1. **`Worker.DefaultRate` changes → existing `LabourEntry.Rate`/`.Cost`
   unchanged.** Enforced structurally: `LabourEntry` stores its own `Rate`
   copy at creation (§1.3.6); no read path re-derives `Rate` from the
   current `Worker` record. `labour.Service.UpdateWorker` only calls
   `WorkerRepository.Update`, never `LabourEntryRepository`.

2. **`Material.ReferencePrice` changes → historical `CostItem`s
   unchanged.** Same structural argument: `CostItem.UnitPrice` (when set)
   is copied at creation; `materials.Service.UpdateMaterial` never touches
   `cost_items`. The only live connection from `CostItem` to `Material` is
   the optional `MaterialID` reference field, informational traceability
   only.

3. **`CostItem` description/category mutable? Money amounts?** **Revised
   (§22-G): `Description`/`Notes` mutable via `UpdateCostItemDetails`.
   `Category` mutable only for manually-created CostItems, only while
   `Committed`/`Actual`/`Paid` are all still nil (§1.4.3); labour-generated
   CostItems' category is always immutable.** Money lifecycle fields are
   mutable only through `UpdateCostItemLifecycle`, one stage at a time,
   never through a generic field-level `PATCH`.

4. **Actual/Paid — editable, or require adjustment records? Approved for
   M3: editable in place, no adjustment-record trail (§22-E).** `Paid` is
   defined as a cumulative running total (§1.5), not a per-payment event —
   this is itself a form of "editable in place" by construction. phase1.md
   §50 anticipates an `audit_events` collection for accountability; a
   formal adjustment-ledger is out of M3's scope per the task brief's
   explicit warning against overbuilding accounting infrastructure here.

---

## 15. Unit-test matrix

- **`money.CalculateLineAmount`**: quantity × unit price rounds correctly
  at half-up boundaries (delegating to `RoundToMinorUnits`, not
  reimplementing); currency of the result matches the input `Money`'s
  currency; verifies M3 modules correctly delegate rather than duplicating
  `RoundToMinorUnits`'s own already-tested boundary cases.
- **LabourEntry cost calculation**: `Quantity × Rate → Cost` matches
  `money.CalculateLineAmount` exactly for a representative set of
  rate-type/quantity-unit combinations (hourly, daily, per-unit,
  per-square-meter).
- **LabourEntry correction (§1.3.2)**: `PATCH` with new
  quantity/rate recomputes `Cost` correctly; the linked `CostItem`'s
  `Estimated` is updated to match; `Committed`/`Actual`/`Paid` on the
  linked `CostItem` are unaffected by the correction (explicit assertion,
  not just absence of a code path that could touch them); `workerId` is
  never accepted as a correctable field even if supplied in the request.
- **`CreateCostItem` rejects `category=labour`**: explicit test that a
  direct attempt to `POST /cost-items` with `category: "labour"` is
  rejected (422/400), proving the ledger-entry-point invariant (§1.4) is
  enforced, not merely documented.
- **`MaterialID` requires `Category=material`**: creating/correcting a
  `CostItem` with a non-nil `MaterialID` under any other category is
  rejected (§1.4.1).
- **`Category` correction lock**: correcting `Category` on a manually
  created `CostItem` succeeds while `Committed`/`Actual`/`Paid` are all
  nil; fails once any one of them is set (three separate cases, one per
  field, §1.4.3).
- **Currency mismatch rejection**: creating a `CostItem` with `Estimated`
  in one currency and `Actual` in another is rejected; a `LabourEntry`
  whose `Rate` currency differs from the company's other records is not
  itself rejected (no cross-record/company-wide currency enforcement
  exists, §1.4.2).
- **Cost lifecycle validation**: setting each of the four stages
  independently; rejecting an `UpdateCostItemLifecycle` call whose new
  amount's currency doesn't match the record's existing `Currency`;
  confirming all four fields can be non-nil simultaneously with different
  values (proving model (b) from §1.5); `Paid` overwrite reflects a new
  running total, not additive accumulation performed server-side (the
  caller supplies the already-summed total).
- **`Quantity × UnitPrice` scope**: confirms the line calculation
  establishes/validates `Estimated` only — a `CostItem` created with
  `Quantity`/`UnitPrice` implying RM1,000 but an explicitly different
  `Committed`/`Actual`/`Paid` succeeds without any cross-field rejection
  (§1.4.2's narrowed scope, guarding against regressing to the earlier
  overly-broad validation).
- **Domain field validation**: required fields per model (Material name +
  unit; Worker name + rateType + defaultRate; LabourEntry projectID +
  workItemID + quantity + (workerID or manual name/trade/rate); CostItem
  projectID + category + at least one non-nil lifecycle amount) rejected
  when empty/zero.
- **Ad-hoc LabourEntry (no WorkerID)**: manual `workerName`/`trade`/`rate`
  required and validated when `workerID` is nil; validation error when
  `workerID` is nil and any of those three is empty.
- **Rate snapshot immutability**: constructing a `LabourEntry` from a
  `Worker`, then mutating the `Worker`'s `DefaultRate` via a fake
  repository, confirms the already-constructed `LabourEntry.Rate` value is
  unaffected (pure in-memory proof, no DB needed for this specific case —
  DB-level proof is §16).

## 16. Testcontainers integration-test matrix

- **Create/FindByID/List/Update round-trip** for Material, Worker,
  LabourEntry, CostItem.
- **`TestLabourEntryRepositoryQuantityRoundTrip`**: mirrors
  `TestWorkItemRepositoryQuantityRoundTrip` from M2 exactly — insert a
  LabourEntry with `quantity.New("8.5", "hour")`, read back, assert exact
  decimal equality and unit match.
- **`TestCostItemRepositoryMoneyRoundTrip`**: insert a `CostItem` with all
  four lifecycle `Money` fields set to different values/amounts, read
  back, assert all four survive independently and distinctly.
- **`TestLabourEntryCostItemUniqueIndex`**: attempting to insert a second
  `labour_entries` document with the same `{companyId, costItemId}` pair
  is rejected by the unique index (§13) — proving the 1:1 invariant is
  enforced at the database layer, not merely by service-layer discipline.
- **Tenant-scoped `FindByID`**: cross-company lookup returns not-found
  sentinel, for all four entity types.
- **Tenant-scoped parent lists**: `ListLabourEntriesByProject`/
  `ListCostItemsByProject`/`...ByWorkItem` scoped correctly.
- **Indexes**: `EnsureIndexes` creates all indexes listed in §13;
  confirmed via `db.RunCommand({listIndexes: ...})` or driver equivalent.
- **Cross-tenant 404 behavior**: same shape as M2 §15's bullet, applied to
  all four M3 entity types.
- **`WorkItemBelongsToProject` capability proof**: integration test against
  a real `work_items` collection confirming the new `work.Service` method
  correctly distinguishes (a) a WorkItem belonging to a different company,
  (b) a WorkItem belonging to the right company but a different Project,
  (c) a WorkItem correctly belonging to both — only (c) returns `true`.
- **`LabourCostRecorder` compensation**: simulate a `LabourEntry` write
  failure after a successful `CostItem` creation (via a fake/failing
  repository in the service-level test, or a real DB test with an induced
  duplicate-key error on the unique index from the prior bullet); confirm
  `DeleteProvisionedLabourCost` is invoked and the orphaned `CostItem` is
  removed in the success case.

## 17. Full HTTP tenant-isolation acceptance matrix

Company A + User A, Company B + User B, exercised through real HTTP
handlers, extending M2's established pattern (M2 §16):

- **Material**: A creates Material A; B's `GET`/`PATCH /materials/{A's id}`
  → 404.
- **Worker**: A creates Worker A; B's `GET`/`PATCH /workers/{A's id}` →
  404.
- **LabourEntry**: A creates LabourEntry A (under Project A / WorkItem A);
  B's `GET`/`PATCH /labour-entries/{A's id}` → 404; B's
  `POST /labour-entries` with `projectId=<Project A's id>` → 404; B's
  `POST /labour-entries` with `workItemId=<WorkItem A's id>` → 404; B's
  `POST /labour-entries` with `workerId=<Worker A's id>` → 404.
- **CostItem**: A creates CostItem A; B's `GET`/`PATCH` on CostItem A →
  404; B's `POST /cost-items` with `projectId=<Project A's id>` → 404;
  same for `workItemId` and `materialId`.
- **Parent-filtered lists reject foreign parent IDs with 404**: for every
  `?projectId=`/`?workItemId=` list endpoint across `labour-entries` and
  `cost-items`.
- **companyId spoofing never affects persisted ownership**: same shape as
  M2's `TestTenantIsolation_CompanyIDInRequestBodyIsIgnored`, applied to
  all four POST endpoints and both PATCH endpoints on LabourEntry/CostItem.

**Same-tenant lineage tests** (not cross-tenant, but proven alongside):
- WorkItem from Project A1 cannot be paired with Project A2 in a
  LabourEntry or CostItem created by User A → 404.
- A Worker belonging to Company A can be referenced in a LabourEntry for
  any of Company A's Projects/WorkItems.

**Historical snapshot tests** (HTTP-level, complementing §15's unit-level
proof):
- Changing `Worker.DefaultRate` via `PATCH /workers/{id}` does not alter
  the `rate`/`cost` fields of an already-created `LabourEntry`.
- Changing `Material.ReferencePrice` via `PATCH /materials/{id}` does not
  mutate any already-persisted `CostItem.UnitPrice`.
- `PATCH /labour-entries/{id}` correcting quantity/rate updates the linked
  CostItem's `Estimated` but leaves any previously-set `Committed`/
  `Actual`/`Paid` on that same CostItem untouched.

**Ledger-integrity tests** (new, per §22-A/§22-G approval):
- `POST /cost-items` with `category=labour` → rejected, for both User A
  and cross-tenant attempts.
- `PATCH /cost-items/{id}` attempting a `category` change while
  `Committed`/`Actual`/`Paid` is set → rejected; succeeds when all three
  are nil.

---

## 18. Final verification checklist (unchanged bar from M1/M2)

```text
go build ./...
go vet ./...
go test ./...
go mod tidy
```

All M0/M1/M2 tests must remain green — no regression. The single-Huma-API
architecture must remain intact: authenticated M3 routes register on the
existing `authedAPI` group (§8), no new `huma.API`/`humachi.New` instance,
all M3 operations appear in the same served `/openapi.json` with
`security: [{bearerAuth: []}]`, matching the post-M2 fix recorded in
`handoff.md` §4.

---

## 19. What M3 explicitly does not implement (confirmation against task brief §1)

Confirmed absent from this design: internal Estimate aggregate/document,
Estimate versioning, project profitability dashboard, customer-facing
Quotations, Quotation versions, client secure access, MaterialRequirement,
RFQs, RFQ Invitations, Supplier Offers, procurement comparison, Purchase
Orders, supplier payments, customer payments, AI resource suggestion, AI
quotation generation, external Access Grants, AWS infra. No M3 endpoint,
model field, or service method references any of these. `CostItem.Committed`
is representable but M3 builds no workflow that auto-populates it from a
Supplier Offer — the one place this spec came closest to that boundary,
and it is deliberately stopped short.

---

## 20. Summary of the one required M2 modification

For traceability: this spec requires exactly **one** change to
already-implemented, already-verified M2 code —
`internal/work/service.go` gains a new `WorkItemBelongsToProject` method
(§9.3), and `internal/work/repository.go`/`repository_mongo.go` gain a
corresponding repository method (or reuse `FindByID` plus an in-service
comparison — implementation-time choice). `internal/work`'s package doc
comment needs a one-line update reflecting that it now exposes a
capability. No other M0/M1/M2 file changes. This should be called out
explicitly in the M3 implementation plan as "Task 0" (before any new M3
module work begins), following the same pattern the post-M2 Huma fix used
of clearly separating pre-existing-code changes from new-milestone work.

---

## 21. New foundation-layer addition required

`internal/foundation/money` gains one new function, `CalculateLineAmount`
(§1.3.1) — the only other change outside the three new M3 module
directories. `internal/foundation/quantity` is unchanged (M3 reuses it
as-is, same as `work` does).

---

## 22. Review round — final decisions (record, not open questions)

This section preserves the review discussion for traceability. Every item
below is **resolved** and already reflected in §1-21 above; nothing here
is pending.

### 22-A. LabourEntry/CostItem authority — APPROVED

`CostItem` is the single authoritative project-cost ledger; M4 aggregation
is `SUM cost_items` only, never `SUM cost_items + labour_entries`. Every
`LabourEntry` creates exactly one `CostItem{Category=labour}`.
`LabourEntry.Cost` is a derived/snapshotted display value;
`CostItem.Estimated` (on the linked labour CostItem) is the authoritative
ledger value M4 reads. **`POST /cost-items` rejects `category=labour`** —
labour costs enter the ledger only via `CreateLabourEntry →
LabourCostRecorder → CostItem(category=labour)`. See §1.4, §5.3.

### 22-B. Two-module write ordering — APPROVED, cost-first with compensation

Order: (1) create `CostItem` via `LabourCostRecorder`, (2) receive
`CostItemID`, (3) create `LabourEntry{CostItemID}`. If step 3 fails, call
`LabourCostRecorder.DeleteProvisionedLabourCost(costItemID)` as best-effort
compensation; log clearly if compensation itself fails. Relationship
simplified to one stored direction: `LabourEntry.CostItemID` only,
`CostItem.LabourEntryID` removed. Database-level 1:1 enforcement via
`labour_entries: {companyId: 1, costItemId: 1}` UNIQUE index. See §1.3,
§5.2, §9.4, §13.

### 22-C. Subcontractor cost modeling — APPROVED, CostItem only

Subcontractor → `CostItem(category=subcontractor)`, never `LabourEntry`.
UI may present "Existing Worker / Custom Labour / Subcontractor" as one
entry point; persistence branches: Worker/Custom Labour → `LabourEntry` →
labour `CostItem`; Subcontractor → `CostItem` directly. See §1.3.3.

### 22-D. LabourEntry correction — CHANGED, narrow PATCH added

`PATCH /labour-entries/{id}` added for `quantity`/`rate`/`date`/`notes`
only — no Worker reassignment, no category-equivalent change. Recomputes
`Cost` via `money.CalculateLineAmount`; propagates to linked
`CostItem.Estimated` only, never `Committed`/`Actual`/`Paid`. Two-module
write again uses cost-first-with-compensation ordering (§22-B's pattern
reused). See §1.3.2, §5.2, §9.4.

### 22-E. Actual/Paid overwrite — APPROVED for M3, with `Paid` defined as cumulative

No adjustment records in M3; `Actual`/`Paid` are current-state amounts.
`Paid` is explicitly defined as the cumulative amount paid to date for the
CostItem (caller supplies the new running total, not a delta), not an
individual payment event. Future payment/audit infrastructure may preserve
individual events; M3 does not. See §1.5, §14.4.

### 22-F. `Material.PreferredSupplierID` — CHANGED, removed from M3

Field removed entirely from the M3 `Material` model. Reintroduced in M6
alongside a real `SupplierLookup`-backed validation once `internal/suppliers`
exists — purely additive, no destructive migration required. See §1.1.

### 22-G. `CostItem.Category` immutability — CHANGED, limited correction allowed

Labour-generated CostItems: category always immutable (structurally fixed
by `LabourCostRecorder`). Manually-created CostItems: category correctable
via `UpdateCostItemDetails` only while `Committed`/`Actual`/`Paid` are all
nil; locks permanently once any one of them is set.
`MaterialID != nil ⟹ Category == material` enforced at creation (not
merely suggested, as the earlier draft had it) and re-validated on any
correction attempt. See §1.4.1, §1.4.3, §14.3.

### 22-H. Sanity check — APPROVED

After the decisions above, the architecture remains consistent with ADR
0001, ADR 0002, tenant isolation, the existing single-Huma-API design, and
M2's capability-interface pattern.

### Two additional corrections adopted from review

1. **`Quantity × UnitPrice` scope narrowed** — establishes/validates
   `Estimated` only, never `Committed`/`Actual`/`Paid`. The three later
   lifecycle stages are independently authoritative facts (accepted
   supplier price, actual invoice, running paid total), not recomputations
   of the creation-time line calculation. See §1.4.2.

2. **`CostItem.Currency` scope clarified** — means the canonical currency
   for that individual CostItem and its own lifecycle Money values only,
   not a claim that it equals `Company`'s operating currency (which does
   not exist as a field in M1's `Company` model yet). M3 enforces
   same-currency **within one CostItem**, not company-wide. See §1.4.2.
