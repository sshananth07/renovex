# Milestone 5 — Quotations: Design Spec

**Status:** REVISED AFTER REVIEW ROUND 3 — round 1's 6 initial decisions
plus 3 corrections, round 2's 6 further corrections (mathematical
soundness of the allocation algorithm, the capability error contract, the
merge/split line-editing contract, duplicate-key error classification, tax
input validation) plus 2 documentation gaps (M6 external-field allowlist,
idempotency scope), and round 3's 3 precision fixes (closing a residual
cross-package-sentinel leak in the cost-basis-eligibility path, a stale
table cell contradicting §13.1, and a manual-adjustment formula that broke
once tax entered the picture), all applied (§31 records the full review
log for all three rounds). Pending final user sign-off before
implementation begins; no implementation code or TDD implementation plan
has been written against this document.

**Scope:** `internal/quotations` — the internal-authenticated, customer-facing
commercial document generated from a **finalized** Estimate: its domain
model, the privacy firewall separating it from `internal/estimates`, its
line-pricing model, versioning/lifecycle, and tenant-safe CRUD/version HTTP
surface. External Client-facing delivery, secure links, and Client
accept/reject actions are explicitly **M6**, not built here — see §24.

**Authorities:** `phase1.md` §1 (Core Domain Model), §3 (Multi-Tenant Company
Structure — `Default quotation terms`, `Default payment terms`), §5 (Project
Management — Project statuses `Quotation Sent`/`Quotation Approved`), §20-22
(Internal Estimate / Profitability Preview — what M4 already built), §23-31
(Customer-Facing Quotation, Internal vs Selling Price, Detail Levels, AI
Quotation Writing Assistant, Quotation Versioning, Client Quotation Portal, AI
Client-Friendly Explanations, Approval Model, Client Quotation Acceptance),
§48-57 (Dashboard, Documents, Audit History, Financial Data Integrity, Schema
Versioning, Idempotency, Optimistic Concurrency, MongoDB Design Principles,
Access Grant Data Model), §62-66 (Technical Architecture, Go Application
Architecture, End-to-End Journey, Core Product Definition); ADR 0001
(money/decimal — authoritative, not re-litigated); ADR 0002 (module
boundaries); the **actual, implemented** M4 `internal/estimates` module (not
merely its design spec) — `estimate.go`, `service.go`, `calculation.go`,
`repository.go`, `repository_mongo.go`, `handler.go`, all read in full for
this design — and the M4 design spec's own §31 ("M4 → future M5 Quotation
boundary"), which explicitly anticipated most of what follows.

**Explicitly out of scope for M5** (per task brief and phase1.md's own M6
boundary, §24 below): Access Grants, secure Client tokens/links, the Client
Quotation Portal itself, Client accept/reject/request-changes actions,
Approval records, AI Quotation Writing Assistant (text generation), PDF
generation, Documents module integration, RFQs, Supplier Offers, Material
Requirements, procurement, payments tracking (recording actual customer
payments — as opposed to *describing* a payment schedule, which is in
scope, §9.15), full project profitability/company dashboards.

---

## 0. What phase1.md actually requires for Quotations — and what it does not

This section exists for the same reason the M4 spec's §0 did: several
questions the task brief poses do not have a literal answer in phase1.md.
Naming that precisely here avoids re-litigating it in every later section.

### 0.1 What phase1.md states, verbatim in substance

- **§1 (Core Domain Model):** `Cost → Estimate → Quotation → Client Approval
  → Revenue`. Quotation sits strictly downstream of Estimate.
- **§20:** "An Estimate is an internal contractor record. It is not the same
  entity as a Quotation." (Confirms these are two distinct aggregates, not
  one entity with a `visibility` flag.)
- **§23:** "A Quotation is the Client-facing commercial document. It is
  generated from an Estimate but remains a separate entity. The Client
  should see the complete customer-facing selling-price information relevant
  to the purchase decision." May include: Scope of Work, Work Packages,
  Quantities where appropriate, Units, Selling unit prices, Line-item
  totals, Discounts, Taxes, Deposit requirements, Payment schedule, Terms
  and conditions, Quotation validity period, Grand total. Worked example is
  a flat list of Work-Package-level lines summing to a subtotal, plus tax,
  plus total — **no cost/margin information anywhere in the example.**
- **§24:** Explicit internal/external privacy boundary. Client "must not
  see": Supplier Material Cost, Worker Wages, Transport Cost, Markup,
  Expected Profit — named individually. Principle: "Client sees the
  complete price of what they are buying. Contractor retains privacy over
  how that selling price is constructed."
- **§25 (Quotation Detail Levels):** the *same* underlying commercial amount
  can be presented Detailed (many small lines), Grouped (package-level
  lines), or Fixed Package (one line, one total) — "the contractor may
  control how selling prices are presented," and "regardless of
  presentation format, the Client must see the complete commercial amount
  they are expected to pay." This is presented as a **contractor choice at
  Quotation-authoring time**, not a fixed schema shape.
- **§26 (AI Quotation Writing Assistant):** AI converts structured Work
  Items into professional Client-facing wording; contractor can
  Accept/Edit/Regenerate. "AI may generate descriptions, but all:
  Quantities, Prices, Discounts, Taxes, Totals, Payment terms must come from
  structured system data." (Confirms: line **descriptions** may eventually
  be AI-assisted text, but every numeric field is always structured/
  deterministic — never AI-computed. This is out of M5's build scope
  (§26 generation itself is deferred) but constrains the schema: a
  `Description` field must exist on each line for this to ever attach to.)
- **§27 (Quotation Versioning):** "Quotation versioning should be
  implemented in Phase 1." Worked example: `QT-0001 V1` (RM35,000) →
  Client requests changes → `QT-0001 V2` (RM38,500). "Older issued
  Quotations should remain available." "Issued Quotations should be treated
  as historically stable records. They should not silently change when:
  Material prices change, Worker rates change, Supplier prices change."
  "Quotation Items should contain pricing snapshots." This is the single
  most load-bearing sentence in phase1.md for M5's persistence model — a
  Quotation's **line-level prices**, not just its total, are explicitly
  required to be snapshotted.
- **§28 (Client Quotation Portal — M6):** a Quotation Access Grant is
  created when the contractor "sends" a Quotation; the Client receives a
  secure link and can view/download/accept/reject/request changes. This is
  the explicit trigger for the M5/M6 boundary (§24 below).
- **§30 (Approval Model):** a general-purpose Approval structure keyed by
  `subjectType`/`subjectId`/`actorType`/`status`, of which
  `subjectType: "quotation"` is the first named example — but the model
  itself, and any concrete Approval record, belongs to the M6 Client-action
  flow, not M5.
- **§31 (Client Quotation Acceptance — M6):** acceptance locks the
  Quotation version and updates Project status. Entirely downstream of the
  secure-link flow — M6.
- **§3:** every company-owned object carries `companyId`; `Quotation` is
  explicitly named in the example list. Company profile includes "Default
  quotation terms" and "Default payment terms" fields (not yet built — M1's
  `Company` has no such fields today, confirmed by inspection, §9.15).
- **§5:** Project references Quotations (never embeds them — "Do not embed
  all ... Quotations ... into one large MongoDB document"); Project statuses
  include `Quotation Sent` → `Quotation Approved` (M6-triggered transitions;
  M5 does not drive Project status changes itself, §17 below).
- **§48 (Dashboard):** "Quotations sent," "Quotations accepted," "Total
  contract value" are dashboard metrics — informational context for what
  kind of read-queries M5's data shape should support later, not an M5
  deliverable itself.
- **§49 (Documents):** `Quotation Data` (structured) is authoritative;
  `PDF` is a derived output artifact — confirms M5 never treats a PDF as the
  source of truth, and PDF generation itself is out of scope here.
- **§50 (Audit History):** `Quotation Generated`, `Quotation Sent`, `Client
  Viewed Quotation`, `Client Requested Changes`, `Client Accepted Quotation`
  are named audit events — the first is M5's concern; the rest are M6's.
- **§51 (Financial Data Integrity):** "Maintain historical Quotation
  snapshots" is named explicitly as its own bullet, independent of the
  general "avoid floating-point" guidance.
- **§54 (Optimistic Concurrency):** the generic example names `Quotation`
  explicitly alongside `Project`, `Estimate`, `Supplier Offer` as a
  document needing a `version` counter to guard concurrent edits — direct
  textual support for carrying M4's `Revision` pattern into M5.
- **§55 (MongoDB Design Principles):** `quotations` is listed as its own
  top-level collection (no `quotation_items`/`quotation_lines` sibling
  collection named).
- **§57 (Access Grant Data Model):** confirms the Access Grant shape
  (`resourceType: "quotation"`) — this is the concrete M6 mechanism M5 must
  not build, but must not structurally block either (§24).

### 0.2 What phase1.md does NOT specify — design decisions this document owns

- **Whether a Quotation line maps 1:1 to a WorkItem, a Space, an Estimate
  cost line, or is contractor-authored freeform.** §25's three presentation
  levels (Detailed/Grouped/Fixed Package) describe *possible outputs*, not
  a specific input granularity or a rule for how the underlying Estimate
  data maps onto them. **This is the central open design question — see
  §6.**
- **The exact mechanism by which `Estimate.ProposedSellingPrice` becomes
  Quotation line amounts.** phase1.md never shows this transformation
  step; it only shows the Estimate's internal blended total (§20's
  category-level example) and the Quotation's already-finished
  Work-Package-level example (§23) — with no worked arithmetic connecting
  the two. **Resolved in §6/§7 below as an explicit design decision, not a
  derivation.**
- **The exact Quotation domain-model field list** (§9.1) — phase1.md's §23
  bullet list is a list of *possible content*, not a schema.
- **Whether discounts/tax are mandatory fields or optional/deferred**
  (§9.13/§9.14) — §23 lists "Discounts" and "Taxes" as things the Quotation
  "may include," with zero worked formula, rate, or jurisdiction rule
  attached anywhere in the document.
- **Payment-terms structure** (§9.15) — "Payment schedule" and "Deposit
  requirements" are named as content categories with no field-level shape.
- **Validity-period mechanics** (§9.16) — "Quotation validity period" is
  named with no expiry-behavior specification (auto-expire? soft warning?
  simply a displayed date?).
- **Quotation-number format concurrency mechanics** — §27's `QT-0001`
  example establishes the *display convention* only; nothing about
  allocation strategy, scope (global/per-company/per-project), or
  concurrency safety.
- **Whether "Version" in §27's sense is the same field as §54's generic
  optimistic-concurrency "version"** — §27 shows `QT-0001 V1`/`V2` as
  business-meaningful commercial revisions (driven by "Client requests
  changes"), the same conceptual shape as M4's `Version`; §54 describes a
  generic optimistic-concurrency counter, the same conceptual shape as M4's
  `Revision`. phase1.md itself uses "version" for both senses without
  distinguishing them — exactly the ambiguity M4's design spec already
  flagged and resolved by keeping `Version`/`Revision`/`SchemaVersion`
  as three distinct fields (M4 design spec §1). **This document adopts the
  identical three-way split for Quotation, for the identical reason.**

Every section below marks its claims as either "phase1.md states X" or
"phase1.md is silent; this document proposes X for the reasons given" —
never blurring the two, matching the M4 spec's own discipline (its §0).

---

## 1. Quotation domain model

```go
package quotations

type QuotationStatus string

const (
    QuotationStatusDraft     QuotationStatus = "draft"
    QuotationStatusFinalized QuotationStatus = "finalized"
)

type Quotation struct {
    ID              string
    CompanyID       string
    ProjectID       string
    ClientID        string          // copied from Project at creation time — see §9.17
    EstimateID      string          // the SPECIFIC finalized Estimate this version was built from — see §3
    QuotationNumber string          // e.g. "QT-000124" — stable across ALL versions of one commercial quotation — see §9
    Version         int             // business-meaningful commercial revision counter — see §10
    Status          QuotationStatus // draft | finalized — see §12
    Revision        int64           // optimistic-concurrency counter for a mutable draft — see §13

    Currency        string          // == the source Estimate's Currency, always — see §8

    Lines           []QuotationLine // customer-facing commercial lines — see §5
    Subtotal        money.Money     // sum of all Lines' Amount, LIVE — recomputed on every edit, see §6.3
    GeneratedSubtotal money.Money   // FROZEN at generation time — the Subtotal exactly as first
                                     // produced by allocation (§6.2), before any contractor edit.
                                     // Contractor-facing only (see §4) — enables showing "generated
                                     // vs. current" divergence per Review Decision 3. Set once,
                                     // per Version, at creation/new-version time; never recomputed
                                     // or touched by any later draft edit.

    TaxMode         TaxMode         // none | percentage — see §14
    TaxLabel        string          // e.g. "SST 6%" — only meaningful if TaxMode != none
    TaxRateBPS      money.RateBPS   // only meaningful if TaxMode != none
    TaxAmount       money.Money     // computed; zero Money if TaxMode == none

    Total           money.Money     // Subtotal + TaxAmount

    Terms           string          // free text — see §15
    PaymentSchedule string          // free text — see §15
    ValidUntil      *time.Time      // optional — see §16

    Notes           string          // internal-authoring notes, contractor-only — NOT client-facing (see §4)

    CreatedAt       time.Time
    FinalizedAt     *time.Time
    SchemaVersion   int
}

// QuotationLine is one customer-facing commercial line. It NEVER carries
// any internal cost/margin figure — see §4/§5.
type QuotationLine struct {
    ID                 string   // stable within this document — lets a PATCH target one line
    SourceWorkItemIDs   []string // traceability only — see §5.3. Empty for a fully
                                  // contractor-authored line. One entry for an
                                  // unmerged generated line. Multiple entries when the
                                  // contractor has merged several generated lines into
                                  // one grouped/fixed-package line. The SAME WorkItemID
                                  // may appear on more than one line simultaneously
                                  // (a split), so this is traceability metadata, not a
                                  // partition or an ownership claim.
    Description        string   // customer-facing wording
    Quantity           *decimal.Decimal // optional — only meaningful with Unit
    Unit               *string          // optional
    UnitPrice          *money.Money     // optional — only meaningful with Quantity/Unit
    Amount             money.Money      // ALWAYS present — the customer-facing line total
    SortOrder          int
}
```

`Discount` is deliberately **absent** from this struct — see §9.13's
decision to defer it entirely, not merely to leave it unresolved.

**`SourceWorkItemIDs []string`, not a single `*string` (Review Decision
1):** the original draft's `SourceWorkItemID *string` could not survive a
contractor merging several generated lines into one grouped or
fixed-package line (§25's Grouped/Fixed-Package presentation modes) without
destroying traceability for every WorkItem except the one the merged line
happened to keep. A slice represents all three cases the editing model
actually needs:

```
Generated line from one WorkItem      SourceWorkItemIDs = ["work-1"]
Grouped/merged line from three        SourceWorkItemIDs = ["work-1", "work-2", "work-3"]
Fully contractor-authored line        SourceWorkItemIDs = []
```

Splitting one WorkItem's commercial value across two separate lines is also
representable this way — both resulting lines simply reference the same
WorkItemID; the slice is a traceability annotation, not a strict partition
of WorkItems across lines, and no invariant requires each WorkItemID to
appear on at most one line.

**`GeneratedSubtotal` (Review Decision 3):** phase1.md never names this
field, and it is not itself customer-facing — it exists solely so the
contractor-authenticated response (§25) can show the "generated vs.
current" comparison the review requested:

```
Source Estimate selling price    RM62,500   (GeneratedSubtotal)
Current Quotation subtotal       RM62,000   (Subtotal)
Manual commercial adjustment       -RM500   (Subtotal - GeneratedSubtotal,
                                               derived at response time,
                                               not stored — see §6.3 for why
                                               this uses Subtotal, not Total)
```

This is safe to store because `Estimate.ProposedSellingPrice` — the value
`GeneratedSubtotal` is seeded from, unmodified, at generation time (§6.2) —
is already a customer-facing commercial amount, never an internal cost or
profit figure (§4's firewall is about `SnapshottedAmount`/`CostSubtotal`/
margin fields specifically, none of which `GeneratedSubtotal` touches).
M6's external DTO is free to omit this field entirely if the
Client-facing view has no use for a "here's what changed" comparison —
that is an M6 DTO-shaping decision, not a firewall question.

All fields directly on one document; no separate `quotation_lines`
collection — matches phase1.md §55 exactly (only `quotations` is listed)
and the identical M4 precedent for `EstimateCostLine` (small, bounded by
the same Project's WorkItem/CostItem count, always read together with the
parent).

---

## 2. Collection ownership

| Module | Collection | Owns exclusively |
|---|---|---|
| `quotations` | `quotations` | Yes |
| `quotations` | `quotation_counters` | Yes |

No `quotation_lines`/`quotation_items` collection — matches phase1.md §55
and the M2-M4 precedent of embedding small bounded child data.
`quotation_counters` (§9.3/§27.1) is a small, purely internal auxiliary
collection — one document per Company, `{companyId, nextNumber}` — that
exists solely to make `QuotationNumber` allocation atomic; it is never
queried by any other module and never appears in any API response.

**Counter gaps are acceptable and are never repaired.** A `QuotationNumber`
can be allocated (the counter incremented) and then the surrounding
`POST /quotations` call can still fail for an unrelated reason (e.g. the
subsequent `Version = 1` insert hitting an unclassified error, or the
process crashing between the two steps) — the allocated number is simply
never used:

```
Allocate QT-000125
        ↓
Quotation creation later fails for an unrelated reason
        ↓
The NEXT successful creation receives QT-000126, not QT-000125 again
```

**No compensation, rollback, or gap-filling logic is introduced for this
case, and no multi-document MongoDB transaction wraps counter allocation
together with the `Quotation` insert.** phase1.md never requires
gapless numbering (§27's own example, `QT-0001`/`QT-0001 V2`, is about
*version* continuity within one chain, not about the absence of gaps
across different chains' numbers) — a monotonically increasing but
occasionally-gapped sequence is the standard, well-understood tradeoff for
lock-free atomic counter allocation, and introducing a transaction solely
to guarantee gaplessness would add real complexity and a real performance
cost for a property phase1.md does not ask for.

---

## 3. Estimate → Quotation boundary

**A Quotation references exactly one specific `Estimate.ID` — never
"the latest Estimate" resolved dynamically at read time.** This is stated
as a strong default in the task brief and confirmed correct against both
phase1.md and M4's own actual behavior:

- phase1.md §27: "Issued Quotations should be treated as historically
  stable records. They should not silently change when: Material prices
  change, Worker rates change, Supplier prices change." A Quotation that
  resolved "latest Estimate" live at read time would violate this directly
  — a later Estimate refresh or new version would silently change what an
  already-created Quotation *means*, even if the Quotation document's own
  bytes never change.
- M4's own `Estimate.ID` is, under the one-document-per-version model,
  already a stable, permanent identifier for one specific commercial
  revision (M4 design spec §4: "Should M5 reference `estimateId` where
  that ID uniquely identifies a version? Yes"). No compound
  `(estimateId, versionNumber)` key is needed.

**The referenced Estimate MUST, at Quotation-creation time:**
1. **Belong to the same Company** — ordinary tenant-isolation invariant,
   identical to every cross-module reference in M2-M4.
2. **Belong to the same Project** as the Quotation being created —
   otherwise a Quotation could misrepresent an unrelated Project's costs as
   this Project's commercial offer. Validated by comparing
   `estimate.ProjectID == quotation.ProjectID` (via the capability in
   §18), not merely by checking company membership.
3. **Be `Status == finalized`** — phase1.md never shows a Quotation
   generated from a still-editable internal number; §20 calls the Estimate
   "private contractor information" while it's being worked on, and §22's
   "Profitability Preview" (the pre-Quotation checkpoint) is explicitly
   framed as happening *before* "generating a Quotation" (§22: "Before
   generating a Quotation, the contractor should see..."). Building a
   Quotation from a draft Estimate would let its numbers shift out from
   under an already-shown customer price via an ordinary `refresh` call —
   directly contradicting §27's snapshot-stability requirement one step
   removed. **A draft-sourced Quotation-creation attempt is rejected**,
   mirroring M4's own `ErrEstimateMustBeFinalizedBeforeNewVersion`
   precondition pattern (M4 design spec §17).

**Snapshot vs. live dependency — resolved: snapshot, always.**
Quotation creation reads the referenced Estimate's `ProposedSellingPrice`
and customer-safe line data (§6) exactly once, at creation time, and
copies what it needs into the new Quotation document. After creation, no
Quotation operation ever re-reads `internal/estimates` or `cost_items`
again — not on `GET`, not on a draft `PATCH` (§13), not on finalize. This
is the direct extension of M4's own "an Estimate version is fully
self-contained between explicit mutations, never dependent on live M3
data during GET" principle (M4 design spec §9) one layer up, and is
**required**, not merely convenient, given §27's explicit snapshot-stability
language.

**What happens if the source Estimate later changes (refreshed, a new
version created, etc.)?** Nothing — by construction. The already-created
Quotation's `EstimateID` still points at the same immutable, `finalized`
Estimate document (finalized Estimates never change, M4 design spec §10);
even if that Estimate later gets superseded by `Estimate V2`, `Quotation
V1`'s snapshot is untouched. Producing a `Quotation V2` that reflects the
newer Estimate is always an **explicit** action (§11), never automatic.

---

## 4. Customer-facing privacy boundary

**Architectural firewall, restated precisely as an invariant the code must
enforce, not merely a principle.**

**Revised per Review Decision 2: the firewall is enforced by WHERE the
allocation computation runs, not merely by what crosses the module
boundary afterward.** The original draft proposed a `FinalizedEstimateSource`
capability whose visitor callback still carried each `EstimateCostLine`'s
raw `SnapshottedAmount` (plus `Category`) into `internal/quotations`, to be
used there only as an allocation *weight*, never stored or serialized. The
review correctly identified this as a real gap: even though the amount was
never written to any `QuotationLine` field or DTO, the raw internal cost
figure still **entered `quotations`' process memory and call stack** —
"prevented from being serialized" is a weaker guarantee than "never
received at all," and a future refactor of the allocation code inside
`quotations` could accidentally start persisting or logging a value it was
never supposed to hold in the first place.

**Corrected design: `internal/estimates` performs the entire cost-weighting
and proportional-allocation computation internally, and exposes only the
already-allocated, already-customer-safe result.** Concretely:

```
INTERNAL (internal/estimates)                    CUSTOMER-FACING (internal/quotations)
──────────────────────────────                   ──────────────────────────────────────
1. Reads EstimateCostLine.SnapshottedAmount       Never receives SnapshottedAmount
2. Groups by WorkItemID, sums each group's cost   Never receives CostSubtotal
3. Computes each group's weight/share of                    ✗
   CostSubtotal
4. Allocates ProposedSellingPrice across groups              ✗
   proportionally (§6.2's algorithm, now run
   INSIDE estimates, not quotations)
5. DISCARDS every internal cost figure —                     ✗
   SnapshottedAmount, CostSubtotal, the
   intermediate weights/shares — none of it
   survives past this point
6. Returns ONLY:                                  Receives ONLY:
   - workItemID (*string, nil for the                - workItemID (*string)
     WorkItem-less group, §5.4)                       - allocatedSellingAmount (money.Money —
   - allocatedSellingAmount (money.Money —               already a SELLING-PRICE figure,
     a SHARE of ProposedSellingPrice, never a            never a cost figure)
     cost figure)
   - proposedSellingPrice (the total, once)
   - currency
```

`PricingMode`/`PricingRate`/`ProjectedGrossProfit`/`ProjectedGrossMarginBPS`
were never candidates for crossing the boundary in either draft — restated
here for completeness: **never exposed**, in the original design or this
corrected one.

**What IS safe to carry across the boundary**, all confirmed against
phase1.md §24's explicit "must not see" list (which never names
`WorkItemID`, `Description`, or `Quantity`/`Unit` as private):

```
(computed inside estimates, from WorkItemID
 grouping — never itself a cost figure)   → QuotationLine.SourceWorkItemIDs (traceability only, §5.3)
allocatedSellingAmount (a SELLING-PRICE
 share, computed inside estimates)        → QuotationLine.Amount, seeded at generation (§6.2) —
                                              contractor-editable thereafter (§6.3)
proposedSellingPrice (the total)          → Quotation.GeneratedSubtotal, frozen at
                                              generation (§1, Review Decision 3)
Estimate.Currency                         → Quotation.Currency (§8)
```

`EstimateCostLine.Description` is **not** part of this capability at all
(corrected from the original draft) — `quotations` gets a WorkItem's
customer-facing description from `internal/work`'s own `WorkItemLookup`
(§18), never from the Estimate's CostItem-level description text, which
was always the wrong source for a WorkItem-level label to begin with
(§5.3 already noted this; §18 now reflects it structurally by removing
`Description` from `FinalizedEstimateSource` entirely).

**Enforcement mechanism (matches M2-M4's own established pattern, ADR
0002), now genuinely structural rather than merely a serialization
discipline:** `internal/quotations` never imports `internal/estimates` by
type. It defines its own narrow capability interface (§18) — now named
`VisitQuotationSeeds` — whose return shape **cannot represent** a cost
figure, a category, or a description at all: it has exactly two fields per
seed, `workItemID` and `allocatedSellingAmount`, both already
customer-safe by construction. This is a materially stronger guarantee
than the prior draft's "the interface returns a cost figure, but callers
promise not to store it" — there is no promise involved; the type itself
cannot hold what it must not carry. This mirrors
`costs.MaterialLookup.GetReferencePrice` returning a bare `money.Money`
rather than a `Material` struct (M3 precedent), applied one step further
than M4's own `EstimatedCostSource` visitor (which does still pass raw
`SnapshottedAmount`-equivalent figures across, appropriately, since its
consumer — `internal/estimates` — is itself the internal, non-customer-
facing side of that particular boundary).

**Response DTO is a second, independent layer of the same firewall** — the
`quotationDTO` (§25) is defined entirely in terms of `Quotation`/
`QuotationLine` fields, none of which can *hold* forbidden internal data
per the struct definition in §1. Belt-and-suspenders: even if a future
change accidentally serialized every field of `Quotation`, there is no
field to leak, because neither the domain struct nor the capability that
populates it ever received the forbidden values.

---

## 5. Quotation line model — resolved

### 5.1 The central question, restated

phase1.md shows two things that must connect, with no stated mechanism
between them:
- M4's **Estimate**, ending in one blended `ProposedSellingPrice` (§20's
  worked example: `RM50,000`, no per-category or per-line selling price
  anywhere).
- phase1.md §23's **Quotation** example, a flat list of Work-Package-level
  customer lines (`Demolition Works RM3,500`, `Waterproofing RM2,800`, ...)
  summing to a subtotal.

### 5.2 Options considered (task brief's own list, evaluated)

| Option | Customer readability | Cost privacy | Traceability | Manual control | Rounding | Versioning | M6 fit | AI fit |
|---|---|---|---|---|---|---|---|---|
| A. One line per WorkItem | Good — matches §23's granularity closely | Safe (WorkItemID has no cost figure) | Excellent (1:1) | Contractor can edit each line | Simple proportional split across N lines | Clean — WorkItem set is stable per Estimate snapshot | Good | Good — §26's AI wording target is exactly "structured Work Item → professional description" |
| B. One line per Space | Coarser than most examples in §23/§25 | Safe | Poor — a Space often spans many WorkItems of very different nature (demolition + tiling + plumbing), losing the "Waterproofing / Wall Tiles / Floor Tiles" granularity §23's own example shows | Contractor can edit each line, but editing is blunt (no way to correct one WorkItem's price within a Space line without also touching siblings) | Simpler (fewer lines) but loses §23's actual worked example's granularity | Clean | OK | Poor — WorkItem-to-text is AI's stated unit, not Space-to-text |
| C. One line per Estimate cost line (`EstimateCostLine`) | Poor — CostItem granularity is *internal* granularity (materials, labour, subcontractor entries), not customer vocabulary; would leak internal cost structure through line COUNT and grouping even with amounts hidden | **Risky** — CostItem granularity directly mirrors §24's forbidden breakdown ("Supplier Material Cost RM3,100 / Worker Wages RM1,200 / Transport Cost RM300") one rename away from a privacy leak | Excellent (1:1) | Poor — a contractor wanting one clean customer line per Work Package would have to manually re-merge many CostItem-level lines every time | Complex — many tiny lines, more rounding residue | Messy — CostItem set can be large and highly granular, changes shape often | Poor | Poor |
| D. Contractor-defined commercial lines (fully freeform, no seeding) | Excellent (contractor writes exactly what they want) | Safe (nothing derived from cost data at all) | **None** — no link back to what was actually estimated | Full control | N/A (contractor supplies amounts directly) | Clean | Good | Poor — nothing for AI's "structured Work Item → description" to seed from |
| E. System-generated initial lines, contractor may edit | Good (starts at §23's granularity) | Safe (uses D's traceability-safe subset, §4) | Good (`SourceWorkItemID` retained) | **Full** — contractor edits Description/Amount/order freely after generation | Handled once, at generation time (§7) | Clean | Good | **Best fit** — §26's AI assistant edits exactly these generated lines' `Description` text |
| F. Hybrid | (subsumed by E once "system-generated, contractor-editable" already permits merging/splitting by hand) | Safe | Good | Full | Same as E | Clean | Good | Best fit |

### 5.3 Decision: **Option E — one system-generated QuotationLine per
WorkItem at creation time, fully contractor-editable thereafter**

This is Option A's granularity (WorkItemID is the natural, already-existing
customer-legible unit — every WorkItem already has a `Description` a
contractor wrote for their own scope, §21.x of the M2 work-item design) with
Option E's editability layered on top, which is what actually resolves the
tension: phase1.md §25 explicitly wants the contractor to be able to
present the *same* underlying commercial content as Detailed, Grouped, or
Fixed Package — that is only achievable if lines are edit/merge/split-able
after generation, not fixed forever at WorkItem granularity. A pure Option
A (system-locked, one immutable line per WorkItem, no editing) would make
"Fixed Package" (§25's third mode — one line, one total) structurally
impossible without a special-cased "collapse mode," which is worse than
just allowing normal line editing (merge two lines into one, edit the
description to read "Complete Bathroom Renovation") to naturally produce
the same outcome.

**Why not seed one line per `EstimateCostLine` (Option C) instead of one
per WorkItem:** an Estimate can (and, per M3's ledger design, normally
does) have multiple `CostItem`s — and therefore multiple
`EstimateCostLine`s — per WorkItem (e.g. one WorkItem "Install Ceramic
Floor Tiles" might have separate CostItems for material, labour, and
equipment rental). Seeding at CostItem granularity would produce a
Quotation draft whose line *count and shape* mirrors internal cost
structure even before any dollar figure is shown — itself an information
leak by structure, the same failure mode §24 warns about for dollar
amounts. Seeding at WorkItem granularity naturally collapses this: multiple
CostItems belonging to one WorkItem become one QuotationLine, whose amount
is that WorkItem's *total* selling-price allocation (§7) — exactly
matching §23's own worked example (`Wall Tiles RM6,200` — clearly a
Work-Package-level figure covering material + labour + install for that
one item together, not three separate CostItem-derived lines).

**Concrete generation rule:** `internal/quotations` calls
`estimates.VisitQuotationSeeds` (§4/§18), which internally groups the
source Estimate's `Lines` by `WorkItemID` and returns one seed per distinct
`WorkItemID` present (plus one seed with `workItemID = nil` covering every
CostItem with no WorkItem, if any exist — Review Decision 6, §5.4). For
each returned seed, `quotations` produces one `QuotationLine`:
`SourceWorkItemIDs = []string{*workItemID}` (or `[]string{}` for the nil
case, §5.4), `Amount = allocatedSellingAmount` (already fully computed,
§6.2), and `Description` seeded via a **separate** call to
`work.GetWorkItemDescription` (§18) — never from anything
`VisitQuotationSeeds` returns, since that capability carries no
description at all (§4).

**What is persisted vs. derived vs. editable, per field:**

| Field | Persisted? | Derived at generation, then frozen until explicit edit? | Contractor-editable after generation? |
|---|---|---|---|
| `SourceWorkItemIDs` | Yes | Yes — `[]string{workItemID}` per generated line, `[]string{}` for a contractor-authored line | **Yes — corrected, 3rd review round: contractor-editable through `PUT /quotations/{id}/lines` as directly submitted, server-validated traceability metadata (§13.1), not merely a side effect of an undefined merge/split command.** A "merge" is expressed by submitting one line whose `sourceWorkItemIds` is the union of two prior lines' arrays; a "split" is expressed by submitting the same WorkItemID on two separate lines. Every submitted ID is validated against `WorkItemLookup` for Company+Project membership (§13.1/§22) before the write is accepted. |
| `Description` | Yes | Seeded from WorkItem's own description via `work.GetWorkItemDescription` | **Yes** |
| `Quantity`/`Unit` | Yes | Left `nil` at generation (§9.3 — no reliable single quantity/unit exists once multiple CostItems roll into one WorkItem line; a contractor who wants to show `30 m² × RM120/m²` per §23's second example enters it manually) | **Yes** |
| `UnitPrice` | Yes | `nil` at generation, for the same reason | **Yes** — but see §7's constraint: if `Quantity`/`Unit`/`UnitPrice` are all supplied, `Amount` must equal their product (mirrors `costs.CreateCostItem`'s own established validation pattern, M3 design spec §1.4.2) |
| `Amount` | Yes | Set from `allocatedSellingAmount`, already fully allocated inside `estimates` (§6.2) | **Yes** — see §6.3 for what happens to the total when this changes |
| `SortOrder` | Yes | Assigned in seed-return order at generation | **Yes** (reordering) |

Lines may also be **added** (a contractor-authored line with
`SourceWorkItemIDs = []`, e.g. a manually-added "Site Cleanup" line) and
**removed** entirely (not merely zeroed) — both ordinary draft edits, §13.

**Customer-facing lines never leak internal cost amounts** — structurally
guaranteed per §4; `QuotationLine` has no field capable of holding a cost
figure, and the capability that populates it (`VisitQuotationSeeds`) has no
field capable of returning one either.

### 5.4 WorkItem-less Estimate contributions (Review Decision 6)

Some `EstimateCostLine`s have `WorkItemID == nil` — M3's `CreateCostItem`
allows a project-level cost with no specific WorkItem (e.g. a permit fee).
**Decision: include them, grouped into one additional generated line, never
silently excluded from the generated total.**

Consistent with M4's own "missing-Estimated CostItem" philosophy (M4
design spec §7 — make gaps visible, don't silently drop cost coverage),
applied to the analogous Quotation-generation gap: excluding these
CostItems from allocation entirely would produce a generated
`Quotation.GeneratedSubtotal` covering less than the full
`Estimate.ProposedSellingPrice`, with nothing surfacing that gap to the
contractor. `VisitQuotationSeeds` returns one extra seed with
`workItemID = nil` when at least one such CostItem exists (omitted
entirely — no zero-amount seed — when every eligible CostItem has a
WorkItem, §7's zero-amount-line prohibition applying identically here).

**Default `Description` for this line:** `"General Project Works and
Services"` — deliberately not `"Miscellaneous"`, which reads as an
afterthought or a catch-all the Client might reasonably distrust.
"General Project Works and Services" (or the equally acceptable "Project-
Wide Works and Services") signals a legitimate category of real,
already-estimated cost, not an unexplained residual. This is only ever the
**initial, generated** wording — the contractor may rename, merge into
another line, or remove it during ordinary draft editing (§13) exactly
like any other generated line.

---

## 6. Selling-price allocation — resolved

### 6.1 The exact question

Given `Estimate.ProposedSellingPrice` (one number) and N WorkItem groups of
`EstimateCostLine`s, how is the total divided into N initial line amounts,
such that `Σ allocatedSellingAmount == ProposedSellingPrice` holds exactly
(no rounding drift), before `internal/quotations` ever sees any of it?

**Revised per Review Decision 2: this entire computation runs inside
`internal/estimates`, behind `VisitQuotationSeeds` (§4/§18) — not inside
`internal/quotations` as the original draft proposed.** The algorithm
itself (largest-remainder proportional allocation) is unchanged from the
original draft's reasoning; only *which module's code executes it* has
moved, so that the intermediate cost figures it necessarily operates on
(`SnapshottedAmount`, `CostSubtotal`, each WorkItem's cost share) never
leave `estimates`' process boundary at all. `internal/quotations` receives
only the already-computed, already-customer-safe `allocatedSellingAmount`
per WorkItem (§5.3), and performs no allocation arithmetic of its own.

### 6.2 Decision: proportional allocation by each WorkItem's share of
`CostSubtotal`, largest-remainder rounding residual assignment — computed
inside `estimates`, with corrected preconditions (2nd review round)

**The original draft's preconditions were mathematically wrong on two
points, both corrected below:**
1. It claimed "a WorkItem group with a non-positive cost weight cannot
   exist under `CostSubtotal > 0`." **False.** `CostSubtotal > 0` is a
   constraint on the *sum* of all `EstimateCostLine.SnapshottedAmount`
   values (M4 design spec §7.2); nothing in M3/M4 prevents an individual
   `CostItem.Estimated` from being negative (e.g. a corrective/credit line
   — M3's `CostItem` places no positivity constraint on `Estimated`
   itself), so a WorkItem group's summed weight can be zero or negative
   even while the Project-wide total is positive:
   ```
   WorkItem A cost share    RM1,000
   WorkItem B cost share      -RM200
   CostSubtotal                RM800   (positive — passes M4's own check)
   ```
   Under the original (unvalidated) formula, WorkItem B would receive a
   negative `allocatedSellingAmount` — an invalid selling-price share.
2. It implied largest-remainder rounding never produces a zero share. Also
   false: a group whose weight is a sufficiently small fraction of
   `CostSubtotal`, relative to `N` and `ProposedSellingPrice`'s minor-unit
   granularity, can legitimately truncate to zero at Step 2 and never win
   enough residual at Step 3 to reach even one minor unit — a mathematical
   possibility the original draft's test matrix left silently undefined
   (§28's "a zero weight among nonzero ones" case).

**Corrected: `AllocateProportionally` (§19) now validates its own
preconditions explicitly and returns an error rather than silently
producing an invalid or zero share.** `VisitQuotationSeeds` (§22) must call
it and **fail entirely — before invoking the visitor callback even once —
if any of these preconditions fail for the current Estimate.** There is no
partial/best-effort Quotation generation: either every group receives a
valid positive share, or `VisitQuotationSeeds` reports the failure via its
`allocationEligible` return value (§22 — corrected in the 3rd review
round: an *error return* here would still require `estimates` to signal
the failure using a value `quotations` recognizes, which is exactly the
cross-package-sentinel problem the 2nd review round already fixed for the
`found`/`projectMatches`/`finalized` triple; the identical fix — a plain
boolean, not a shared error — applies here too) and no `QuotationLine` is
ever created from this attempt.

**Step 1 — compute each WorkItem's cost weight (inside `estimates`).** For
each distinct `WorkItemID` group of `EstimateCostLine`s, sum their
`SnapshottedAmount`s — call this `workItemCostShare`. This intermediate
sum, and `CostSubtotal` itself, exist **only inside
`estimates.Service.VisitQuotationSeeds`'s own implementation** — neither
value is returned to any caller, logged, or persisted anywhere outside the
Estimate document itself, which already legitimately holds them (§4). Any
CostItems with `WorkItemID == nil` form their own implicit group (§5.4).
**Validated here, before allocation proceeds:** every `workItemCostShare`
must be strictly positive — if any group's summed weight is zero or
negative (the WorkItem-B scenario above), `VisitQuotationSeeds` sets
`allocationEligible = false`, `err = nil` (§22, corrected 3rd review
round), and returns immediately without invoking the visit callback and
without calling `AllocateProportionally` at all. `quotations.Service`
maps `allocationEligible == false` to its own locally-defined sentinel,
`ErrIneligibleCostBasisForQuotation` (§20) — this mapping happens entirely
inside `quotations`, so `estimates` never needs to know that sentinel
exists. This is a real, if rare, M5-visible business rule: **an Estimate
containing a negative-weight WorkItem group cannot be turned into a
Quotation until the underlying CostItem data is corrected** (via M3's
existing `PATCH /cost-items/{id}/lifecycle`, then an Estimate
refresh/re-finalize) — noted explicitly here rather than silently
mishandled.

**Step 2 — allocate each group's raw share (inside `estimates`, via
`money.AllocateProportionally`).** For each WorkItem group `i`:

```
rawAmount[i] = ProposedSellingPrice × (workItemCostShare[i] / CostSubtotal)
```

using integer minor-unit arithmetic via `shopspring/decimal`, rounded down
(truncated, not rounded) to whole minor units for this step only — the
final rounding happens once, in Step 3, via the standard mechanism.

**Step 3 — largest-remainder residual assignment (inside `estimates`).**
Because Step 2's N truncated amounts will, in general, sum to slightly
less than `ProposedSellingPrice` (never more, since truncation only ever
removes value), the leftover minor-unit residual (`ProposedSellingPrice -
Σ rawAmount[i]`, always a small non-negative integer, bounded by `N-1`
minor units) is distributed one minor unit at a time to the groups with the
**largest fractional remainder** from Step 2's division, breaking ties by
`WorkItemID`'s snapshot order (deterministic, reproducible, no dependency
on map iteration order). This is the standard "largest remainder method"
for proportional-integer allocation — the same class of problem
apportionment algorithms solve, applied here to money instead of legislative
seats.

**Post-allocation validation (new, closes the zero-share gap):** after
Steps 2-3 produce every group's final share, `AllocateProportionally`
checks that **every resulting share is strictly positive** — a share that
still truncates to zero after residual distribution (only possible when
`N` is large relative to `ProposedSellingPrice`'s minor-unit count, or one
group's weight is a vanishingly small fraction of `CostSubtotal`) causes
the **entire call to fail** with `ErrZeroAllocationShare` (§19, an
`internal/foundation/money`-local error — never crosses any module
boundary, §22) — not a silent zero-amount line, which §7.2 already
prohibits from existing at all. `VisitQuotationSeeds` catches this error
internally and, again, reports it by setting `allocationEligible = false`
(`err = nil`) rather than propagating `ErrZeroAllocationShare` itself or
any `quotations`-defined sentinel — the identical boolean-based contract
as the pre-allocation check above, so the caller-facing signal is uniform
regardless of which of the two checks actually failed. **Only the final,
per-group `allocatedSellingAmount` — the validated output of this step —
crosses into `quotations`**, via `VisitQuotationSeeds`'s visit callback,
and only when `allocationEligible == true`.

**Why this exact method, and not a simpler alternative:**
- **Equal-split** (divide `ProposedSellingPrice` by N lines evenly) was
  rejected — it actively misrepresents the commercial reality phase1.md
  §23's own example shows (a `RM3,500` Demolition line next to a `RM6,200`
  Wall Tiles line — visibly *not* equal shares), and would make the
  Quotation's line-level detail actively misleading relative to what was
  actually estimated.
- **First-line-absorbs-all-residual** (give every line its truncated
  amount, dump 100% of the residual on line 1) was rejected — it produces
  visibly odd amounts on an arbitrary line (e.g. a `RM3,500` line becoming
  `RM3,503`) with no principled reason why *that* line absorbed the
  rounding, whereas largest-remainder distributes the (always tiny, at
  most `N-1` minor units total, i.e. a few cents) residual to the lines
  where the truncation actually discarded the most value — the
  least-arbitrary rule available.
- **Re-deriving each line's UnitPrice × Quantity instead of allocating a
  lump Amount** was rejected as the *generation-time* default, because — as
  established in §5.3 — a WorkItem-level line's underlying CostItems don't
  reliably share one unit of measure or a single meaningful quantity
  (materials priced per m², labour priced per day, equipment a flat fee,
  all rolled into one WorkItem). Allocating `Amount` directly and leaving
  `Quantity`/`Unit`/`UnitPrice` optional (nil at generation) sidesteps
  inventing a synthetic, meaningless blended unit price. A contractor who
  wants §23's second example (`30 m² × RM120/m²` for one specific line)
  supplies that manually, at which point §7's product-must-equal-Amount
  validation applies.

**Invariant proven by this design:** `Σ QuotationLine.Amount ==
Quotation.Subtotal == Quotation.GeneratedSubtotal ==
Estimate.ProposedSellingPrice` **exactly**, at generation time, before any
contractor edit — no floating-point, no silent drift, the residual fully
and deterministically accounted for.

### 6.3 Manual overrides after generation — what happens to the total
(Review Decision 3)

**Resolved: line edits are free; `Quotation.Subtotal`/`Total` are always
recomputed as the actual sum of current `Lines[].Amount` — but
`Quotation.GeneratedSubtotal` is captured once, at generation time, and
never changes again for this Version, specifically so the divergence
§6.3's own freedom creates remains visible rather than merely
theoretical.** Concretely:

- At generation time (creation, §3, or new-version creation, §11):
  `Quotation.GeneratedSubtotal` is set to `Estimate.ProposedSellingPrice`
  (equivalently, `Σ allocatedSellingAmount` from `VisitQuotationSeeds`,
  §6.2 — the two are identical at this instant) and then **frozen** —
  no subsequent operation on this Quotation Version ever writes to
  `GeneratedSubtotal` again, including line edits, tax changes, or
  finalization.
- The contractor may edit any line's `Amount` (directly, or by supplying
  `Quantity`/`Unit`/`UnitPrice` whose product becomes the new `Amount`,
  §5.3's table) — no external validation ties an edited line back to its
  original allocated share.
- After any line edit, `Quotation.Subtotal` is recomputed as `Σ
  Lines[].Amount` over the **current** state of `Lines` (same principle as
  M4's `CostSubtotal` always being the actual sum of current `Lines`, never
  a separately-tracked running total that could drift from them) — this
  field is **live**, unlike `GeneratedSubtotal`.
- **This means `Quotation.Subtotal`/`Total` CAN legitimately diverge from
  `GeneratedSubtotal` (and therefore from the source Estimate's
  `ProposedSellingPrice`) after manual edits** — and this divergence is
  **intentional and fully visible**, both structurally (two distinct
  stored fields, not one field silently overwriting the other) and in the
  contractor-facing response (§25):

  ```
  Source Estimate selling price    RM62,500   (GeneratedSubtotal)
  Current Quotation subtotal       RM62,000   (Subtotal, live)
  Manual commercial adjustment       -RM500   (Subtotal - GeneratedSubtotal,
                                                 computed at response time,
                                                 never itself stored)
  ```

  **The comparison uses `Subtotal`, not `Total` — corrected, 3rd review
  round.** The original draft computed this figure as `Total -
  GeneratedSubtotal`, which becomes wrong the moment tax is configured:
  `Total` is `Subtotal + TaxAmount` (§7), so once `TaxMode = percentage`
  is active, `Total - GeneratedSubtotal` would conflate the contractor's
  manual line-price adjustment with the tax amount itself — a Quotation
  with zero manual edits but a newly-configured 6% tax would incorrectly
  display a nonzero "manual commercial adjustment," which is not what
  actually happened. `GeneratedSubtotal` was seeded from
  `ProposedSellingPrice` (a pre-tax, line-level commercial figure, §6.2) —
  the correct comparison is against `Subtotal` (the pre-tax, current sum
  of line amounts), not `Total` (which mixes in a dimension —
  tax — that `GeneratedSubtotal` was never meant to describe in the first
  place).

  phase1.md §20 already establishes the identical principle one layer up
  ("The contractor may manually override prices... The system assists the
  contractor but does not lock them into calculated values") — applying
  the same freedom at the Quotation-line layer is consistent, not a new
  liberty invented for M5. `GeneratedSubtotal` is safe to store and
  display precisely because it is itself a customer-facing selling-price
  figure (§1), never an internal cost or margin value — it does not
  reopen the §4 firewall.
- **No enforcement mechanism re-syncs `Quotation.Subtotal` back to
  `GeneratedSubtotal`** — `GeneratedSubtotal` is a read-only, informational
  baseline, never a target `Subtotal` is constrained to match. The
  alternative (rejecting any edit that would make `Subtotal !=
  GeneratedSubtotal`) would defeat the entire purpose of letting a
  contractor customize customer-facing pricing at all — a contractor who
  wants to round a customer-facing total to a cleaner number (`RM62,500` →
  `RM62,000` as a goodwill gesture) needs exactly this freedom.
- **M6's external (Client-facing) DTO may omit `GeneratedSubtotal`
  entirely** if the "here's what changed" comparison has no Client-facing
  use — that is an M6 DTO-shaping decision M5 does not need to resolve now
  (§24).

### 6.4 Rounding strategy summary

All monetary calculations use `money.RoundToMinorUnits`-style centralized
rounding (ADR 0001) — no new ad-hoc rounding is invented in either
`internal/estimates` or `internal/quotations`. The proportional-split
arithmetic in §6.2 needs **one new `internal/foundation/money` helper**,
since none of the M1-M4 additions (`CalculateLineAmount`, `AddRateBPS`,
`DivideByComplementRateBPS`, `RateBPSFromRatio`) perform a "split a total
across N weighted shares with an exact-sum guarantee" operation — proposed
in §19. **This helper is called from inside `internal/estimates`**
(specifically, from the new `VisitQuotationSeeds` method, §18/§22), not
from `internal/quotations` — a direct consequence of Review Decision 2
moving the allocation computation itself behind the privacy boundary.

---

## 7. Financial calculations — summary

```
Quotation.Subtotal = Σ Lines[i].Amount            (exact int64 sum, no rounding needed —
                                                     same principle as Estimate.CostSubtotal)

Quotation.TaxAmount =
    TaxMode == none:       zero Money (same currency, Amount = 0)
    TaxMode == percentage: round(Subtotal × TaxRateBPS / 10000)   (one rounding step,
                                                                     reuses AddRateBPS's
                                                                     underlying rate-fraction
                                                                     math, applied to compute
                                                                     the tax delta rather than
                                                                     the marked-up total —
                                                                     see §19)

Quotation.Total = Subtotal + TaxAmount             (exact int64 addition)
```

No discount term in this formula — discounts are deferred entirely (§14).
Applied identically whether the Quotation is a draft (recomputed on every
line/tax edit) or finalized (computed once, then frozen forever, §12).

### 7.1 Optional line-field validation (Correction C — precise rules)

For each `QuotationLine`, `Quantity`/`Unit`/`UnitPrice` must be either
**all three absent** or **all three present** — a partial combination is
rejected, not silently ignored, mirroring `costs.CreateCostItem`'s
identical established rule (M3 design spec §1.4.2,
`ErrIncompleteQuantityPricing`) applied here as `ErrIncompleteLinePricing`
(§20):

```
Quantity present, Unit absent, UnitPrice present    → REJECTED (ErrIncompleteLinePricing)
Quantity present, Unit present, UnitPrice absent     → REJECTED (ErrIncompleteLinePricing)
Quantity absent, Unit absent, UnitPrice absent       → OK — amount-only line
Quantity present, Unit present, UnitPrice present    → OK — validated below
```

**When all three are present (corrected — a zero `UnitPrice` is NOT
allowed, reversing the original draft's position):**
- `Quantity > 0` (strictly positive — mirrors `costs.CreateCostItem`'s
  identical `ErrInvalidQuantity` rule, M3 design spec §1.4.2).
- `Unit` is non-blank.
- `UnitPrice.Amount > 0` — **strictly positive, not merely non-negative.**
  The original draft allowed `UnitPrice.Amount == 0` as a "transparency
  line" case, reasoning that it was "distinct from the line's overall
  `Amount`, which is separately constrained to be positive." That reasoning
  was wrong: with all three fields present, `Amount` is not independently
  set — it is *derived* as `Quantity × UnitPrice` (the very next rule below),
  so `UnitPrice.Amount == 0` forces `Amount == 0` by construction, which
  §7.2 unconditionally rejects. The two rules as originally written were
  simultaneously unsatisfiable for this case. Corrected by applying the
  already-approved no-zero-line policy consistently: `UnitPrice.Amount <= 0`
  is rejected (`ErrInvalidLineUnitPrice`) for both zero and negative values.
  A contractor who wants to represent a free or included item does so
  **textually** instead (§7.2's own resolution) — there is no structured
  zero-unit-price path in M5.
- `Amount` must equal `money.CalculateLineAmount(Quantity, UnitPrice)` —
  reuses the existing M3 foundation helper unchanged, and mirrors
  `costs.CreateCostItem`'s identical mismatch-rejection rule (M3 design
  spec §1.4.2, `ErrEstimatedMismatchesLineAmount`) at the Quotation-line
  layer, named `ErrLineAmountMismatch` here (§20).

**When all three are absent** (an amount-only line — the shape every
system-generated line takes at creation, §5.3): `Amount` is supplied
directly, with no product to validate against.

### 7.2 Zero-valued lines and a zero-valued Quotation — rejected

**Decision: `QuotationLine.Amount` must be strictly positive
(`> 0`), and `Quotation.Subtotal` must be strictly positive (`> 0`) as a
consequence.** No `Amount == 0` line is representable in M5, and — since
`Subtotal` is always the live sum of `Lines[].Amount` (§6.3) — a Quotation
can never legitimately reach a zero or negative `Subtotal` either (the
`Σ Amount > 0` invariant is only satisfiable when every individual
`Amount > 0`, given at least one line exists, §29's `ErrEmptyLines`).

**Rationale:** phase1.md gives no example of a free/RM0 Quotation line
anywhere, and a zero-amount line invites exactly the kind of ambiguity M4
already rejected once for `CostSubtotal` (M4 design spec §7.2 — a zero or
negative financial figure produces meaningless or undefined downstream
math, there specifically for margin-mode pricing; here the analogous risk
is a Quotation whose `Subtotal` could reach zero, making `TaxAmount`
calculation and any future percentage-based feature degenerate). A
contractor who wants to represent a genuinely free or included item can do
so **textually**, inside another priced line's `Description` (e.g. "Wall
Tiles (includes complimentary grout sealing) — RM6,200") rather than by
creating a standalone RM0 financial line — matching phase1.md §26's own
framing that Descriptions, not prices, are where free-text customer
communication belongs.

**Enforced at every point a line's `Amount` can be set:** generation
(§6.2 — corrected below; the original draft's claim that "a WorkItem group
with a non-positive cost weight cannot exist under `CostSubtotal > 0`" was
mathematically wrong, since a positive overall subtotal does not imply
every individual group is positive — see §6.2's corrected validation), and
every subsequent manual edit (`PUT /quotations/{id}/lines`, §13 — rejected
with `ErrLineAmountNotPositive`, §20, before any line in the submitted
array is accepted).

---

## 8. Currency

**Decision: `Quotation.Currency` is always set from the referenced
Estimate's `Currency` at creation time, and is immutable thereafter — no
FX conversion in M5, and no per-line currency override.**

**Correction (Correction B): the original draft claimed a mixed-currency
Quotation was "structurally impossible" because `QuotationLine` has no
separate currency field. That claim does not hold — `QuotationLine.Amount`
and `QuotationLine.UnitPrice` are both `money.Money`, and `money.Money`
carries its own `Currency` string (§1's struct definition; confirmed
against `internal/foundation/money/money.go`). Nothing in the Go type
system prevents a line's `Amount.Currency` from being constructed or
persisted as, say, `"USD"` on a Quotation whose own `Currency` is `"MYR"`
— the type only makes a *literal second currency field on the line struct*
impossible, not a currency-valued-differently-than-the-parent's `Money`
value. The only real guarantee is runtime validation, and the original
draft was wrong to describe it as anything stronger.**

**Corrected invariant — enforced by explicit server-side validation, not
by the type system:**

```
Line.Amount.Currency          == Quotation.Currency   (every line, every write)
Line.UnitPrice.Currency       == Quotation.Currency   (when UnitPrice is supplied, §7.1)
Subtotal.Currency             == Quotation.Currency   (recomputed value, checked before persisting)
GeneratedSubtotal.Currency    == Quotation.Currency   (set once, at generation — validated at that point)
TaxAmount.Currency            == Quotation.Currency
Total.Currency                == Quotation.Currency
```

Every one of these is checked explicitly in the service layer — at line
generation (§5.3/§6.2, where every `allocatedSellingAmount`
`VisitQuotationSeeds` returns is already guaranteed same-currency as the
Estimate by `estimates`' own internal logic, but `quotations` re-validates
on receipt rather than trusting the boundary silently), at every draft
line-edit (`PUT /quotations/{id}/lines`, §13 — a submitted line whose
`Amount.Currency` or `UnitPrice.Currency` differs from the Quotation's own
`Currency` is rejected with `ErrLineCurrencyMismatch`, §20, before any
line in the batch is accepted), and at tax configuration (§15 — `TaxAmount`
is always computed via `ApplyRateBPS(Subtotal, TaxRateBPS)`, §19, which by
construction returns a `Money` sharing `Subtotal`'s own currency, so this
specific check can never actually fail in practice, but the invariant is
stated here for completeness and as a target for the acceptance test named
below).

- `Money.Add`/`Subtract` (used to compute `Total = Subtotal + TaxAmount`)
  already reject a currency mismatch defensively at the foundation layer —
  this remains true and unchanged, and is the last line of defense, not
  the only one.
- No `Company.DefaultCurrency`-based inference — confirmed absent, same
  finding as M3 and M4 both independently made (`Company` has no currency
  field in this codebase today).

**Acceptance test required** (added to §28's matrix): a currency-
substitution attempt — submitting a `PUT /quotations/{id}/lines` payload
where one line's `Amount.Currency` differs from the Quotation's own
`Currency` — must be rejected with `ErrLineCurrencyMismatch` (422), and
must leave the draft's existing `Lines` completely untouched (no partial
update, matching M4's own "refresh either fully succeeds or fully no-ops"
discipline, M4 design spec §16.2).

---

## 9. Quotation numbering

### 9.1 `QuotationNumber` vs `Version` vs `Revision` vs `SchemaVersion` —
four distinct fields, never conflated

| Field | Meaning | Changes when | Shared across versions of the same commercial quotation? |
|---|---|---|---|
| `QuotationNumber` | The customer-visible identifier, e.g. `QT-000124` | **Never**, once allocated | **Yes** — `QT-000124 V1` and `QT-000124 V2` are the SAME `QuotationNumber`, different `Version` (directly matches phase1.md §27's own worked example format) |
| `Version` | Business-meaningful commercial revision counter (mirrors M4's `Version`) | Only via `POST /quotations/{id}/versions` (§11) | No — each version has its own document, own `Version` int |
| `Revision` | Optimistic-concurrency counter for in-place draft edits (mirrors M4's `Revision`) | Every draft-line/terms/tax edit (§13) | No — resets to 0 on each new `Version` |
| `SchemaVersion` | Document schema version (§52) | Only on a schema migration | N/A |

This is the direct, deliberate carry-forward of M4's own three-way split
(`internal/estimates/estimate.go`'s doc comment: "`Version`... `Revision`...
`SchemaVersion`... are three distinct fields") — chosen for the identical
reason M4 adopted it: conflating any two would make it impossible to answer
"did the commercial price change" independently of "did anything change,
including an in-place draft edit" independently of "did the document's
on-disk shape change." phase1.md's own text uses "version" ambiguously for
both the §27 sense and the §54 sense — this design does not inherit that
ambiguity.

### 9.2 Numbering format and scope

**Decision: `QuotationNumber` is allocated **per Company**, monotonically,
zero-padded, e.g. `QT-000001`, `QT-000002`, ...** — matching phase1.md
§27's exact display format (`QT-0001`, here zero-padded to 6 digits to
comfortably outlast a busy contractor's Phase-1 lifetime without a format
change).

**Why per-Company, not global or per-Project:** phase1.md never specifies
this explicitly, but per-Company is the only option consistent with §3's
"every company operates as an independent tenant" combined with the
concrete display convention `QT-0001` (no company-identifying prefix
segment shown at all — a global sequence would either need a
company-disambiguating prefix phase1.md never shows, or would let one
company observe roughly how many total Quotations exist platform-wide from
the gaps in their own numbers, a minor but pointless cross-tenant
information leak). Per-Project was rejected because §27's own scenario
("Client requests changes" producing `V2`) is about **one client-facing
document being revised**, not about a Project accumulating many
independently-numbered Quotations — a Project legitimately can and likely
will have exactly one `QuotationNumber` chain across its lifetime in the
common case, but nothing in phase1.md restricts a Project to exactly one
QuotationNumber (a contractor could quote a small extra scope separately),
so scoping the *counter* to Project rather than Company would be an
unjustified extra restriction with no textual support.

### 9.3 Allocation concurrency — resolved

**Not `count documents + 1`** (explicitly disallowed by the task brief, and
already known-wrong from first principles: concurrent counts race
trivially). Two real options, evaluated against M4's own precedent:

- **A MongoDB `findOneAndUpdate` with `$inc` against a dedicated per-Company
  counter document** (a small `quotation_counters` collection, `{companyId,
  nextNumber}`), atomic by construction, no retry loop needed — this is the
  standard, well-known safe pattern for a strictly-monotonic external-facing
  sequence number, and is explicitly allowed by phase1.md §21's note ("No
  separate counters collection" was M4's decision for **Version**
  allocation specifically, where a unique index sufficed because collisions
  were rare/coordinated around finalization — that reasoning does not
  transfer to `QuotationNumber`, which must be allocated on **every single
  new commercial quotation's first version**, i.e. on the hot path, not
  just at an occasional finalize-then-branch event).
- **Reusing M4's own `MAX(version)+1`-with-bounded-retry pattern**, applied
  instead to `MAX(quotationNumber-suffix) + 1` scoped to `{companyId}` —
  rejected: unlike `Version` (scoped to one `{companyId, projectId}` pair,
  where contention is naturally low — one Project rarely has two people
  racing to create its first Estimate simultaneously), `QuotationNumber`
  is scoped to the whole Company, where contention across *many
  simultaneous Projects* is far more likely in ordinary multi-user
  operation (two different employees finalizing two unrelated Projects'
  first Quotations in the same minute). A retry-loop under that much higher
  contention is a worse fit than the atomic-counter-document approach,
  which has no retry loop at all.

**Decision: dedicated atomic counter document, `{companyId, nextNumber}`
in a `quotation_counters` collection**, incremented via
`FindOneAndUpdate` with `$inc: {nextNumber: 1}` and
`upsert: true`, returning the post-increment value in one atomic operation
— no read-then-write race window exists. This departs from M4's "no
separate counters collection" choice deliberately, for the concurrency-
profile reason above; the M4 spec's own §21.2 statement was scoped to
*Version* allocation specifically, not asserted as a platform-wide rule
against ever using a counter collection.

**`QuotationNumber` is allocated exactly once per commercial quotation
chain — at the FIRST version's creation only.** `POST
/quotations/{id}/versions` (§11) copies the source Quotation's own
`QuotationNumber` forward unchanged; it never allocates a new one. This is
what makes `QT-000124 V1`/`QT-000124 V2` share one number, per §27's
worked example.

---

## 10. Quotation versioning — persistence model

**Decision: one MongoDB document per version — identical model and
justification to M4's `Estimate`,** for the same four reasons M4's design
spec §4 gave (restated briefly, since they transfer unchanged): avoids an
ever-growing single document (phase1.md §5's general document-growth
warning); makes a finalized/issued version structurally impossible to
accidentally mutate (no code path ever targets an old version's `_id` again
once superseded); gives M6 a stable, permanent `quotationId` to build a
Client Access Grant against (§24); and makes concurrent version creation a
natural unique-index concern rather than requiring application-level
locking on one shared array field.

**Additional M5-specific reason, beyond M4's four:** phase1.md §27's own
worked example is already framed as two full snapshots
(`QT-0001 V1 RM35,000` and `QT-0001 V2 RM38,500`, each presumably with
their own full line breakdown) — the phrase "Quotation Items should
contain pricing snapshots" reads naturally as "each version's line data is
frozen," which a document-per-version model gives for free and an
embedded-array model would have to re-derive.

**Version numbering**: integers starting at 1, monotonically increasing
per `{companyId, quotationNumber}` (not `{companyId, projectId}` — see
§9.2's reasoning: one Project could in principle host more than one
independently-numbered Quotation chain, so the version sequence must be
scoped to the specific chain, identified by `QuotationNumber`, not the
Project). Only advances via `POST /quotations/{id}/versions` — never
bumped by an in-place draft edit (§13, same split as M4's `Revision` vs
`Version`).

---

## 11. New-version semantics

`POST /quotations/{id}/versions` — the operation that advances `Version`.

**Required precondition: the source Quotation must already be
`finalized`.** Mirrors M4's `CreateNewVersion` precondition exactly, for
the identical reason: phase1.md §27's whole scenario ("Client requests
changes" on an **already-issued/finalized** Quotation) presumes the thing
being revised was a stable, already-shown document — revising a still-being
-drafted Quotation has no need for a new *version* at all, since a draft is
already freely editable in place (§13).

**Steps:**
1. Validate the source Quotation exists, belongs to `companyID`, and
   `Status == finalized`. Reject (409,
   `ErrQuotationMustBeFinalizedBeforeNewVersion`) otherwise — same shape as
   M4's `ErrEstimateMustBeFinalizedBeforeNewVersion`.
2. **Caller supplies a new `estimateID`, explicitly — never silently
   re-resolved from "whatever the latest Estimate happens to be now."**
   This is the resolution of the task brief's §9.11 critical question: the
   scenario it describes (`Estimate V1 finalized → Quotation V1`, costs
   change, `Estimate V2 finalized`, "need Quotation V2") is explicitly
   **not** automatic. The service validates the newly-supplied
   `estimateID` exactly as fresh Quotation creation does (§3: same
   Company, same Project, `finalized`) — it may be the same Estimate the
   source Quotation used (a pure re-quote/price-tweak with no underlying
   cost change) or a genuinely different, newer Estimate version. **Why
   not auto-bind to "latest finalized Estimate":** an automatic binding
   would silently change what a Quotation-in-progress-of-being-revised
   means if a THIRD, unrelated Estimate revision happened to land between
   "contractor starts revising" and "contractor finishes revising" — the
   exact class of silent-drift hazard phase1.md §27 exists to prevent one
   layer up. Requiring an explicit `estimateID` on every new-version call
   makes the dependency visible and deliberate, matching phase1.md §18's
   "AI suggests, contractor approves" discipline applied to a human
   decision, not just an AI one: *the system does not decide which
   Estimate a new Quotation version is based on; the contractor does.*
3. Copy `ProjectID`, `ClientID`, `QuotationNumber`, `Currency` from the
   source Quotation (not re-derived, not re-supplied by the caller) — with
   one exception: `Currency` is re-validated against the **new**
   `estimateID`'s own currency (§8) and the operation is rejected
   (`ErrQuotationCurrencyMismatch`) if the newly-referenced Estimate's
   currency differs from the source Quotation's — a currency change
   mid-chain is not supported in M5 and must fail loudly, not silently
   convert or silently proceed with mismatched figures.
4. **What is copied from the source Quotation's own snapshot, vs. freshly
   regenerated from the new Estimate — RESOLVED (Review Decision 4,
   APPROVED as originally recommended):** the new version's `Lines` are
   **freshly generated** from the new `estimateID` via the exact same
   generation procedure as first-time creation (§5.3/§6.2, i.e. a fresh
   `VisitQuotationSeeds` call against the new Estimate) — **not** copied
   forward from the source Quotation's possibly-hand-edited lines.
   `GeneratedSubtotal` (§1, §6.3) is likewise **reset** to the new
   Estimate's `ProposedSellingPrice` at this step — it is not carried
   forward from the source Quotation's own (by now possibly-diverged)
   `GeneratedSubtotal`, since it exists specifically to describe *this*
   Version's own generation baseline. **Rationale (confirmed by review):**
   the source Quotation's lines may already reflect contractor hand-edits
   (§6.3) that have no necessary correspondence to the new Estimate's
   (possibly different) cost basis; blindly carrying old dollar amounts
   forward against a new cost basis risks presenting a Client with a total
   that no longer reconciles with anything real — copying forward stale
   amounts against a *new* cost basis is its own kind of silent staleness,
   the same failure mode phase1.md §27 warns against one layer up, merely
   relocated to the Quotation-to-Quotation transition instead of the
   Estimate-to-Quotation one. A fresh generation against the new Estimate,
   followed by the contractor re-applying whatever hand-edits still make
   sense (a manual, visible step — matching phase1.md §18's principle
   again), is the resolved M5 baseline. A future enhancement could attempt
   to preserve edited `Description` text where the same `WorkItemID`
   still appears in the new Estimate's line set — explicitly deferred
   (§29), not built in M5.
5. `Terms`/`PaymentSchedule`/`ValidUntil`/`Notes` **are** copied forward
   from the source Quotation unchanged (these are not derived from Estimate
   cost data at all — carrying them forward avoids the contractor re-typing
   boilerplate on every revision; each remains freely editable on the new
   draft, §13).
6. `TaxMode`/`TaxLabel`/`TaxRateBPS` are also copied forward from the
   source Quotation (the contractor's prior tax choice, not re-derived) —
   `TaxAmount`/`Total` are recomputed against the newly-generated `Lines`.
7. Allocate the next `Version` (`MAX(version) + 1` for this
   `{companyId, quotationNumber}`, bounded-retry, same concurrency shape as
   M4 §21.2 — see §27).
8. Persist as a new document: `Status = draft`, `Revision = 0`,
   `CreatedAt = now`, `FinalizedAt = nil`.

`POST /quotations` (first-ever creation for a brand-new commercial
quotation) is the related-but-distinct procedure that additionally
allocates a fresh `QuotationNumber` (§9.3) and has no source-Quotation step
— matching M4's own explicit `CreateEstimate` vs. `CreateNewVersion` split,
carried forward for the identical concurrency reasons (§22).

---

## 12. Draft / finalized lifecycle

**Two statuses only: `draft`, `finalized`.** No `sent`/`approved`/
`rejected`/`accepted` in M5 — those are M6's Client-facing/Access-Grant-
triggered states (phase1.md §27/§30/§31 describe them scoped to the
Client-interaction flow, which M5 does not build, §24). This directly
mirrors M4's own two-status decision (draft/finalized only, no
Client-facing states on Estimate either) and keeps the M5/M6 boundary
sharp: **M5 ends at "this Quotation is ready to be sent"; M6 owns
everything from "sending" onward.**

- **Can a draft be edited?** Yes — freely, in place, same `Version`,
  `Revision` incremented on every mutating call (§13): line add/edit/
  remove/reorder, `Terms`/`PaymentSchedule`/`ValidUntil`/`Notes` edits,
  tax configuration changes. No `refresh`-from-Estimate operation exists
  on a Quotation draft (unlike M4's Estimate `refresh`, which re-pulls
  live `cost_items`) — a Quotation's only path back to its Estimate is
  through the one-time creation/new-version snapshot (§3/§11); it never
  re-reads `internal/estimates` after that.
- **Can a finalized Quotation be edited?** No — every field is immutable
  once `Status = finalized`, matching phase1.md §27's "issued Quotations
  should be treated as historically stable records" and M4's identical
  rule for a finalized Estimate.
- **Does finalizing lock the version?** Yes — `finalize` is the only
  transition, `draft → finalized`, one-directional, matching M4 exactly.
- **Is `finalize` idempotent?** Yes — identical shape to M4's
  `FinalizeEstimate`: calling finalize again on an already-finalized
  Quotation returns the current document unchanged with 200, regardless of
  the supplied `expectedRevision` (§13's optimistic-concurrency contract
  freezes `Revision` the instant finalize succeeds, so a retry cannot know
  what value to supply).
- **Can a finalized Quotation be reopened?** No un-finalize operation —
  same principle as M4. The only way to get different customer-facing
  numbers is `POST /quotations/{id}/versions` (§11), which requires the
  source to already be finalized.
- **Where M5 ends, concretely — RESOLVED (Review Decision 5, APPROVED as
  originally recommended):** a `finalized` Quotation is the complete M5
  deliverable — "ready to be sent." **`Quotation.finalize` does not modify
  `Project.Status` in any way** — M5 does not itself transition
  `Project.Status` to `quotation_sent`/`quotation_approved` (phase1.md §5's
  those two statuses are driven by M6's actual send/accept actions, not by
  M5's finalize). "Finalized" means **ready to send**; it does not mean
  **sent to the Client** — those are two genuinely different, temporally
  separated business events, and phase1.md's own status name
  (`Quotation Sent`, not `Quotation Finalized`/`Quotation Ready`) already
  names the *second* event, not the first. `quotation_sent` is applied
  only once M6 actually creates/delivers the Client access workflow;
  `quotation_approved` only once M6 records genuine Client acceptance
  (phase1.md §31). Conflating "finalized" with "sent" would make
  `Project.Status` assert a business event (delivery to a real Client)
  that has not actually happened, since no delivery mechanism exists
  before M6 is built — a false claim the system should never make. **No
  write capability from `quotations` into `projects`' status field exists
  in M5** — `quotations` consumes `ProjectLookup` read-only (§22); nothing
  in this design gives it a way to mutate `Project.Status` even if it
  wanted to.

---

## 13. Draft editing — mechanics and concurrency

**What a contractor may change on a draft Quotation:**
- Line add / edit (`Description`, `Quantity`/`Unit`/`UnitPrice`, `Amount`,
  `SortOrder`) / remove / reorder
- `Terms`, `PaymentSchedule`, `Notes` (free text)
- `ValidUntil`
- `TaxMode`/`TaxLabel`/`TaxRateBPS`

**Not editable on a draft:** `CompanyID`, `ProjectID`, `ClientID`,
`EstimateID`, `QuotationNumber`, `Currency` — all fixed at
creation/new-version time (changing the underlying Project/Client/Estimate
mid-draft is not a "draft edit," it's a different commercial quotation;
changing Currency mid-draft is excluded per §8).

**Mutation shape: targeted, not full-document replacement.** Following
M4's own precedent (`PATCH /estimates/{id}/pricing` touches only pricing
fields; `refresh` touches only the snapshot fields — no generic
"replace the whole document" endpoint exists), M5 exposes purpose-specific
mutation endpoints rather than one generic `PATCH /quotations/{id}` for
arbitrary fields (§23):
- `PUT /quotations/{id}/lines` — replaces the entire `Lines` array in one
  call (chosen over N separate single-line-mutation endpoints because
  reordering/adding/removing lines together as one coherent edit is the
  natural unit of a "I rearranged my Quotation" action; the whole-array
  replace recomputes `Subtotal`/`TaxAmount`/`Total` server-side from
  whatever the caller submits, validating each line per §7 before
  accepting any of them — a partially-invalid submission changes nothing).
- `PATCH /quotations/{id}/terms` — `Terms`/`PaymentSchedule`/`Notes`/
  `ValidUntil`, independently of line edits.
- `PATCH /quotations/{id}/tax` — `TaxMode`/`TaxLabel`/`TaxRateBPS`,
  recomputing `TaxAmount`/`Total` from the currently-stored `Subtotal`.

### 13.1 `PUT /quotations/{id}/lines` — exact request contract (Issue 4,
2nd review round)

**Gap in the original draft: §5.3 asserted `SourceWorkItemIDs` changes
"only as a side effect of merge/split line operations," but no merge/split
operation was ever defined — the only line-mutation endpoint is this
whole-array `PUT`, which has no notion of "merge" or "split" as distinct
actions from an ordinary caller's point of view.** Corrected: there is no
separate merge/split command in M5. Instead, `PUT /quotations/{id}/lines`
accepts `SourceWorkItemIDs` as **plain, directly editable traceability
metadata** on each submitted line — a contractor (or a future frontend
built on this API) performs a "merge" simply by submitting one line whose
`sourceWorkItemIds` array is the union of the two lines' prior arrays (and
omitting the two original lines from the submission), and a "split" by
submitting two lines that both carry the same WorkItemID. No new endpoint
or DTO shape beyond the array-of-lines request body already implied by
§23 is required — merge/split are conventions for how a caller uses the
existing endpoint, not new server operations.

**Request DTO:**

```go
type putQuotationLinesInput struct {
    ID   string `path:"id"`
    Body struct {
        ExpectedRevision int64                `json:"expectedRevision" required:"true"`
        Lines            []quotationLineInput `json:"lines" required:"true"`
    }
}

type quotationLineInput struct {
    ID                string   `json:"id,omitempty"` // see line-ID handling below
    SourceWorkItemIDs []string `json:"sourceWorkItemIds"` // required key, may be an empty array
    Description       string   `json:"description" required:"true"`
    Quantity          *string  `json:"quantity,omitempty"`
    Unit              *string  `json:"unit,omitempty"`
    UnitPrice         *quotationMoneyDTO `json:"unitPrice,omitempty"`
    Amount            quotationMoneyDTO  `json:"amount" required:"true"`
    SortOrder         int                `json:"sortOrder"`
}
```

**Server-side validation, applied to the WHOLE submitted array before ANY
line is accepted (a partially-invalid submission changes nothing, matching
this section's own already-stated whole-array-replace principle):**

1. **`SourceWorkItemIDs` membership** — every WorkItemID appearing in any
   submitted line's `SourceWorkItemIDs` must belong to both `companyID`
   AND this Quotation's own `ProjectID`, checked via
   `WorkItemLookup.GetWorkItemDescription`'s `found` return value (§22 —
   the same call already needed to seed a description at generation time
   is reused here as the existence/ownership check; a cross-project or
   cross-tenant WorkItemID is indistinguishable from a nonexistent one,
   matching every other cross-tenant-reference invariant in this codebase,
   §21). A single failing ID rejects the entire submission with
   `ErrWorkItemNotFound` (§20).
2. **Uniqueness within one line** — the same WorkItemID must not appear
   twice in one line's own `SourceWorkItemIDs` array (a data-entry error,
   not a meaningful "double-counted" state) → `ErrDuplicateWorkItemIDInLine`.
3. **Reuse across lines is explicitly permitted** — the same WorkItemID
   MAY appear in more than one line's `SourceWorkItemIDs` simultaneously
   (this is exactly how a split is represented, §5.3) — no
   uniqueness-across-the-whole-array constraint exists.
4. **An empty `SourceWorkItemIDs` array is valid** — represents a fully
   contractor-authored line (§5.3); the JSON key must still be present
   (an explicit `[]`, not an omitted field) so a client cannot ambiguously
   signal "no change" vs. "clear the traceability".
5. **Line-ID handling:**
   - A submitted line carrying an `ID` that matches an existing line on
     THIS Quotation is treated as an edit of that line (its identity is
     preserved for any future single-line-targeted feature, though M5
     itself has no such feature yet).
   - A submitted line carrying an `ID` that does not match any existing
     line on this Quotation → rejected with `ErrUnknownLineID` (guards
     against a client accidentally — or maliciously — referencing a line
     ID copied from a different Quotation).
   - A submitted line with no `ID` (or an empty string) is treated as a
     new line — the server generates a fresh line ID for it.
   - Duplicate `ID` values within one submitted array → rejected with
     `ErrDuplicateLineID`.
6. **`Description`** — trimmed of leading/trailing whitespace before
   validation; rejected as blank (`ErrBlankLineDescription`) if the
   trimmed result is empty. A Client-facing document with an unlabeled
   line is not acceptable output, even mid-draft.
7. Every line individually re-validated per §7.1/§7.2/§8's already-stated
   rules (`Quantity`/`Unit`/`UnitPrice` all-or-nothing, positivity,
   product-matches-Amount, currency match, `Amount > 0`).
8. `len(Lines) > 0` — `ErrEmptyLines` if the submitted array is empty
   (§29).

Only once every line in the submitted array passes every check above does
the write proceed (as one atomic `Revision`-guarded update, §27.4) —
replacing `Lines` wholesale and recomputing `Subtotal`/`TaxAmount`/`Total`
from the now-current `Lines`.

**Optimistic concurrency: yes, `Revision`-guarded, identical contract to
M4's `refresh`/`RecalculatePricing`/`FinalizeEstimate`.** Every one of the
three mutation endpoints above, plus `finalize`, requires the caller to
supply `expectedRevision`; a mismatch is rejected with 409
(`ErrRevisionMismatch`) and the document is left untouched; a match
increments `Revision` by exactly 1 (except `finalize`, which freezes it,
identical to M4 §16.3). This is directly supported by phase1.md §54's
explicit mention of `Quotation` in its generic optimistic-concurrency
example, and by the Financial Data Integrity principle (§51) applying with
at least as much force to a Quotation as to an Estimate — arguably more,
since a Quotation is the document a real Client will eventually see.

---

## 14. Discounts

**Decision: deferred entirely from M5 — no `Discount` field anywhere in
the schema, not even as a nullable placeholder.**

phase1.md §23 lists "Discounts" once, as one bullet in a list of things a
Quotation "may include," with **zero** worked example, formula, or
percentage-vs-fixed-amount indication anywhere in the document. This is
the same evidentiary situation M4 faced with contingency (M4 design spec
§14) — a bare mention with no textual grounding for *how* it should work —
and this document applies the identical discipline M4 did: do not invent a
financial rule (percentage vs. fixed, pre-tax vs. post-tax, per-line vs.
quotation-level) with no basis in the source document. If discounts become
a validated near-term requirement, they are a purely additive schema
change (a new optional `DiscountMode`/`DiscountAmount` pair, mirroring
`TaxMode`'s own shape, §14 below) requiring no migration of existing
Quotations — matching the exact "additive, no migration" pattern M4 used
for its own deferred contingency field.

---

## 15. Tax / SST

**Decision: minimal, generic, contractor-configured tax support — matching
the task brief's own recommended default exactly, since it is the only
option consistent with phase1.md's silence on Malaysian tax specifics
combined with §23/§28's explicit listing of "Taxes" as required
customer-facing content.**

```go
type TaxMode string

const (
    TaxModeNone       TaxMode = "none"
    TaxModePercentage TaxMode = "percentage"
)
```

- **`TaxMode = none` is the default** for every newly-generated Quotation
  — the system never infers taxability from `Project` type, contractor
  registration status, or any other heuristic. A contractor must
  explicitly call `PATCH /quotations/{id}/tax` to configure
  `TaxMode = percentage` with an explicit `TaxLabel` (e.g. `"SST 6%"`,
  freeform, customer-facing) and `TaxRateBPS`.
- **The system's responsibility ends at deterministic calculation once
  configured** (`TaxAmount = round(Subtotal × TaxRateBPS / 10000)`, §7) —
  it never validates that the configured rate is legally correct for this
  Project/Client/scope; that determination remains the contractor's,
  exactly as the task brief specifies.
- **No residential/commercial auto-inference of any kind** — `Project` has
  no field this logic could even key off today (M2's `Project` struct has
  no property-type/residential flag, confirmed by inspection), and even if
  one existed, phase1.md gives no basis for a specific rate mapping.
- **No relationship to any tax embedded in upstream material/procurement
  costs** — `TaxAmount` here is purely a Quotation-level, customer-facing
  figure computed from `Subtotal`; it has no connection to, and does not
  attempt to reconcile against, whatever tax treatment (if any) a
  `CostItem`'s `Estimated`/`Actual` figures might separately embed
  upstream (out of scope for M5 to even inspect).
- **Company-level tax profiles, SST registration numbers, jurisdiction
  rule engines** — explicitly future scope, not M5.

### 15.1 Exact input validation (Issue 6, 2nd review round — the original
draft defined the modes and the calculation formula but never the accepted
input combinations)

`PATCH /quotations/{id}/tax` accepts `TaxMode`, `TaxLabel`, `TaxRateBPS` as
one atomic update — all three interpreted together, not independently
optional fields with their own individual defaults:

**`TaxMode = "none"`:**
- `TaxLabel` must be empty (`""`) — a non-empty label with no active tax
  mode is a contradiction (a label describing a tax that isn't applied).
- `TaxRateBPS` must be `0`.
- Violating either → `ErrInvalidTaxRate` if `TaxRateBPS != 0`,
  `ErrInvalidTaxLabel` if `TaxLabel != ""` (checked independently, both
  may fire on the same malformed request — whichever the implementation
  checks first is surfaced; both are equally valid 422s).
- Resulting `TaxAmount` is always zero `Money` in the Quotation's own
  `Currency` (§7's already-stated formula, unchanged).

**`TaxMode = "percentage"`:**
- `TaxLabel` must be non-blank (trimmed) — `ErrInvalidTaxLabel` otherwise.
  A percentage tax with no customer-facing label (e.g. `"SST 6%"`) fails
  phase1.md §23/§28's own requirement that the Client see a labeled tax
  line, not just a bare number.
- `TaxRateBPS` must satisfy `0 < TaxRateBPS <= 10000` (strictly greater
  than 0%, up to and including 100%) — `ErrInvalidTaxRate` otherwise. Zero
  is excluded here deliberately: a contractor who wants no tax should use
  `TaxMode = none`, not `TaxMode = percentage` with a `0` rate — collapsing
  the two into one representable-but-degenerate state would let the same
  real-world outcome ("no tax charged") be expressed two different ways in
  storage, which this design avoids on the same "one authoritative
  representation" principle ADR 0001 applies to money itself.
- `TaxAmount = money.ApplyRateBPS(Subtotal, TaxRateBPS)` (§7, §19,
  unchanged).

**Any `TaxMode` value other than `"none"`/`"percentage"`** →
`ErrInvalidTaxMode` (422), checked first, before either of the
mode-specific rules above are evaluated.

**A rejected `PATCH /quotations/{id}/tax` call leaves the draft's stored
`TaxMode`/`TaxLabel`/`TaxRateBPS`/`TaxAmount`/`Total` completely
untouched** — no partial update, matching every other M5 mutation
endpoint's already-stated all-or-nothing discipline (§13.1's own line
validation, M4's `refresh` precedent).

---

## 16. Payment terms

**Decision: `Terms` and `PaymentSchedule` are both free-text fields on
`Quotation` — not structured deposit/milestone records.**

phase1.md §23 lists "Deposit requirements," "Payment schedule," and "Terms
and conditions" as three separate bullets with no field-level shape given
anywhere; §3 mentions "Default quotation terms" and "Default payment
terms" at the Company level (also unstructured — `Company` has no such
fields today, confirmed by inspection, so no M5 dependency on them exists
yet; a future Company-level default-terms feature could pre-fill these
free-text fields, but building that pre-fill mechanism is not required for
M5 to be complete). A structured payment-milestone model (e.g. "30% deposit
/ 40% at material delivery / 30% on completion" as discrete, individually
trackable records) is explicitly the future **Payments module**'s
responsibility (listed as its own top-level `internal/` module in
phase1.md §63, separate from `quotations`) — M5's `PaymentSchedule` is a
**description** of the intended schedule for the Client to read, not a
record the system tracks fulfillment against. Conflating the two would
mean `quotations` reaching into Payments' future domain prematurely.

---

## 17. Validity / expiry

**Decision: `ValidUntil *time.Time`, optional, purely informational in
M5 — no automatic expiry workflow.**

phase1.md §23/§28 both list "Quotation validity period" as customer-facing
content with no behavior specified beyond display. M5:
- Stores `ValidUntil` if the contractor sets it (nullable — a Quotation
  without a set validity period is valid, matching "not specified" rather
  than defaulting to some invented duration).
- Displays it in the response DTO.
- **Does not** auto-transition `Status`, auto-reject a
  past-`ValidUntil` new-version attempt, or send any notification — all of
  that is either M6 (Client-facing expiry enforcement on the secure-link
  view) or a future scheduled-job concern, neither of which M5 builds.

---

## 18. Client relationship

**Decision: `Quotation.ClientID` is stored directly, copied from
`Project.ClientID` at creation time — not derived live through `Project` on
every read.**

- `internal/projects`' actual `Project` struct (confirmed by inspection,
  `backend/internal/projects/project.go`) already has a `ClientID string`
  field directly on it — so deriving a Quotation's Client is trivially
  possible either way at creation time.
- **Historical-stability question, resolved:** if `Project.ClientID` were
  ever changed after a Quotation exists (no such mutation endpoint exists
  in M2 today, but the field is not structurally immutable either), should
  an already-created Quotation's client identity silently change too?
  **No** — for the identical reason `EstimateID`/cost snapshots are
  copied rather than live-resolved (§3): a Quotation is a document a real
  Client may already be looking at (or will be, once M6 exists); its
  stated Client identity must not silently shift underneath an
  already-issued commercial offer. Storing `ClientID` directly, snapshotted
  at creation, guarantees this regardless of what `projects` does later.

---

## 19. Required `internal/foundation/money` additions

Per ADR 0001, no calculation-specific rounding logic may live in
`internal/quotations`. Two new small helpers are needed:

```go
// internal/foundation/money — proposed additions, alongside the existing
// CalculateLineAmount/AddRateBPS/DivideByComplementRateBPS/RateBPSFromRatio.

// ErrNonPositiveAllocationTotal is returned when total.Amount <= 0.
var ErrNonPositiveAllocationTotal = errors.New("money: allocation total must be strictly positive")

// ErrNoAllocationWeights is returned when weights is empty.
var ErrNoAllocationWeights = errors.New("money: at least one weight is required")

// ErrInvalidAllocationWeight is returned when any individual weight is
// <= 0 (design spec §6.2, corrected 2nd review round — a caller must
// exclude or reject non-positive weights BEFORE calling; this function
// does not silently drop or clamp them, since a caller receiving a
// truncated result set with no signal which weight was dropped cannot
// safely reconstruct which output corresponds to which input).
var ErrInvalidAllocationWeight = errors.New("money: every weight must be strictly positive")

// ErrZeroAllocationShare is returned when the largest-remainder algorithm
// would produce a zero-minor-unit share for at least one weight even
// after residual distribution (design spec §6.2 — possible when the
// number of weights is large relative to total's minor-unit count, or one
// weight is a vanishingly small fraction of the summed weights). The
// entire call fails rather than returning a result set containing a zero
// share, since a zero share is never a valid output for this platform's
// only current caller (internal/estimates' quotation-line allocation,
// where every resulting QuotationLine.Amount must be strictly positive,
// design spec §7.2).
var ErrZeroAllocationShare = errors.New("money: allocation would produce a zero share for at least one weight")

// AllocateProportionally splits total across len(weights) shares,
// proportional to each weight, using the largest-remainder method so the
// shares sum to EXACTLY total (never more, never less) — no residual is
// ever silently dropped or double-counted. total's Currency is copied
// onto every returned share. Ties in the remainder-ranking step are
// broken by weights' input index (deterministic, reproducible).
//
// VALIDATES (design spec §6.2, corrected 2nd review round — the original
// draft asserted these conditions could never occur and therefore never
// checked them; both assertions were false):
//   - total.Amount must be > 0 (ErrNonPositiveAllocationTotal)
//   - len(weights) must be > 0 (ErrNoAllocationWeights)
//   - every weights[i] must be > 0 (ErrInvalidAllocationWeight) — a
//     summed Project cost total being positive does NOT imply every
//     individual weight (e.g. one WorkItem's cost share) is positive;
//     a negative or zero individual weight is a real possible input this
//     function must reject explicitly, not assume away
//   - the sum of weights must be > 0 (follows from the per-weight check
//     above, but verified directly as a defensive redundancy)
//   - EVERY resulting share must be > 0 (ErrZeroAllocationShare) — the
//     largest-remainder method does not guarantee this on its own; a
//     weight that is a small enough fraction of the total, relative to
//     len(weights) and total's minor-unit granularity, can legitimately
//     truncate to zero and never receive a residual unit
//   - overflow: intermediate decimal arithmetic uses shopspring/decimal
//     (arbitrary precision, no int64 overflow risk during computation);
//     the FINAL per-share and total amounts are still int64 minor units
//     (ADR 0001) — a total or per-share value that would not fit in
//     int64 is rejected as an ordinary Go error, not silently wrapped
//
// Used by internal/estimates (NOT internal/quotations, per Review
// Decision 2, §6.2) to allocate Estimate.ProposedSellingPrice across
// generated QuotationLine seeds by each WorkItem's CostSubtotal share.
func AllocateProportionally(total Money, weights []int64) ([]Money, error)

// ApplyRateBPS computes base × rate/10000, rounded once via
// RoundToMinorUnits — the tax-amount formula (design spec §7). Distinct
// from AddRateBPS (which returns base PLUS the rate-computed delta, the
// markup formula): ApplyRateBPS returns ONLY the delta itself, since a
// Quotation's Subtotal and TaxAmount are two separately-displayed fields,
// not a single marked-up total.
func ApplyRateBPS(base Money, rate RateBPS) Money
```

Both tested in `internal/foundation/money`'s own test suite (matching
every prior helper's precedent) — `internal/estimates`' own tests exercise
*using* `AllocateProportionally` correctly (e.g. verifying `Σ shares ==
total` across a range of weight distributions, including the
all-residual-goes-to-one-share edge case, and separately verifying each of
`ErrNonPositiveAllocationTotal`/`ErrNoAllocationWeights`/
`ErrInvalidAllocationWeight`/`ErrZeroAllocationShare` is returned for its
corresponding invalid input) rather than re-proving the allocation
algorithm's own correctness from scratch. **Note the caller package
changed from `internal/quotations` to `internal/estimates`** (Review
Decision 2, §6.2) — `AllocateProportionally`'s own unit tests live in
`internal/foundation/money` regardless of which domain module calls it.

---

## 20. Domain errors (repository/service layer)

```go
var ErrQuotationNotFound = errors.New("quotations: quotation not found")
var ErrProjectNotFound = errors.New("quotations: project not found")
var ErrEstimateNotFound = errors.New("quotations: estimate not found")
var ErrEstimateNotFinalized = errors.New("quotations: source estimate must be finalized")
var ErrEstimateProjectMismatch = errors.New("quotations: estimate does not belong to this project")
var ErrIneligibleCostBasisForQuotation = errors.New("quotations: estimate's cost basis cannot be allocated into a quotation (a workitem group is non-positive or would receive a zero share)") // §6.2, 2nd review round
var ErrQuotationCurrencyMismatch = errors.New("quotations: referenced estimate's currency does not match this quotation chain")
var ErrLineCurrencyMismatch = errors.New("quotations: line amount/unitPrice currency does not match this quotation's currency") // Correction B, §8
var ErrWorkItemNotFound = errors.New("quotations: work item not found") // Issue 4, §13/§22
var ErrDuplicateWorkItemIDInLine = errors.New("quotations: sourceWorkItemIds must not contain the same work item twice") // Issue 4, §13.1
var ErrUnknownLineID = errors.New("quotations: line id does not belong to this quotation") // Issue 4, §13.1
var ErrDuplicateLineID = errors.New("quotations: duplicate line id in submitted lines") // Issue 4, §13.1
var ErrBlankLineDescription = errors.New("quotations: line description must not be blank") // Issue 4, §13.1
var ErrQuotationNotDraft = errors.New("quotations: quotation is not a draft")
var ErrQuotationMustBeFinalizedBeforeNewVersion = errors.New("quotations: source quotation must be finalized before creating a new version")
var ErrIncompleteLinePricing = errors.New("quotations: quantity, unit, and unitPrice must all be supplied together or not at all") // Correction C, §7.1
var ErrInvalidLineQuantity = errors.New("quotations: quantity must be strictly positive") // Correction C, §7.1
var ErrInvalidLineUnitPrice = errors.New("quotations: unitPrice must be strictly positive") // Correction C, §7.1 — corrected 2nd review round: was "must not be negative" (>= 0), now "> 0", since a zero UnitPrice forces Amount == 0, which is unconditionally rejected below
var ErrLineAmountMismatch = errors.New("quotations: line amount does not match quantity x unitPrice")
var ErrLineAmountNotPositive = errors.New("quotations: line amount must be strictly positive") // Correction C, §7.2
var ErrEmptyLines = errors.New("quotations: at least one line is required")
var ErrInvalidTaxMode = errors.New("quotations: invalid tax mode") // Issue 6, §15
var ErrInvalidTaxRate = errors.New("quotations: invalid tax rate for the given tax mode") // Issue 6, §15
var ErrInvalidTaxLabel = errors.New("quotations: invalid tax label for the given tax mode") // Issue 6, §15
var ErrRevisionMismatch = errors.New("quotations: revision mismatch or quotation is no longer a draft")
var ErrVersionConflict = errors.New("quotations: version number conflict, retry with a fresh version") // ALSO returned when §27.3's bounded retry loop is exhausted — not a distinct sentinel, see §27.5
var ErrDraftAlreadyExists = errors.New("quotations: a draft already exists for this quotation chain") // corrected 2nd review round — named in §27.3's prose but missing from this list in the original draft
var ErrUnclassifiedDuplicateKey = errors.New("quotations: unclassified duplicate-key error") // corrected 2nd review round — same omission
var ErrQuotationNumberAllocationFailed = errors.New("quotations: failed to allocate a quotation number") // SCOPE CORRECTED, §27.5 — infrastructure failure of the counter increment itself ONLY, never retry exhaustion or an unclassified collision
```

`ErrQuotationNotFound` also covers cross-tenant existence — same-shape 404
regardless of whether the ID is nonexistent or belongs to another company
(M1-M4 invariant, unchanged).

---

## 21. Tenant ownership

Identical invariants to M1-M4, extended to `quotations` with no exceptions:

1. Every `Quotation` document stores `companyId` directly, sourced only
   from `Principal.CompanyID`.
2. `companyId` is never accepted from request body/query/path.
3. `projectId` is validated via `ProjectLookup` (§22) before creation; the
   referenced `estimateId` is validated for both company-membership AND
   project-membership AND finalized status (§3), all three checked inside
   `estimates.VisitQuotationSeeds` itself (§22), before being consumed.
4. Every direct-by-ID repository method is tenant-scoped:
   `(ctx, companyID, id)`.
5. Cross-tenant existence is indistinguishable from non-existence — same
   404, same sentinel error (`ErrQuotationNotFound`).
6. `GET /quotations?projectId=...` validates the Project belongs to
   `Principal.CompanyID` before querying — foreign `projectId` → 404, never
   `[]`.
7. ID substitution across tenants never succeeds, for the Quotation ID
   itself, the referenced `EstimateID`, and any embedded
   `SourceWorkItemIDs` entries exposed for traceability.

---

## 22. Module boundaries and capabilities — dependency graph

`quotations` must not directly import `internal/estimates`, `internal/work`,
`internal/projects`, or `internal/clients` by type (ADR 0002). It defines
its own narrow capability interfaces, satisfied structurally:

```go
// defined in internal/quotations

// ProjectLookup — identical shape to every prior module's own copy.
type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
    // GetProjectClientID is the ONE additional method beyond the M2-M4
    // precedent's shape — quotations needs the owning ClientID at creation
    // time (design spec §18), which no prior module needed from
    // ProjectLookup. Returns only the bare ClientID string, never a
    // projects.Project struct (ADR 0002).
    GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error)
}

// FinalizedEstimateSource — the capability quotations needs from
// estimates. REVISED per Review Decision 2: the return shape now
// structurally cannot carry SnapshottedAmount/CostSubtotal/Category/
// Description/PricingMode/PricingRate/ProjectedGrossProfit/
// ProjectedGrossMarginBPS at all — not merely "does not currently use
// them." The original draft's FinalizedEstimateCostLine (WorkItemID +
// Category + Description + Amount, where Amount was the raw internal
// SnapshottedAmount) is REMOVED entirely — the cost-weighting and
// proportional-allocation computation that consumed it (§6.2) now runs
// INSIDE estimates' own implementation of this interface, never inside
// quotations.
type FinalizedEstimateSource interface {
    // VisitQuotationSeeds validates the given estimateID belongs to
    // companyID AND projectID AND is Status == finalized (all three
    // checked INSIDE estimates, behind this capability — REQUIRED
    // preconditions, not re-derived by quotations from a broader lookup),
    // then internally: reads every EstimateCostLine, groups by
    // WorkItemID, computes each group's share of CostSubtotal, and
    // allocates ProposedSellingPrice across groups proportionally with
    // largest-remainder rounding (design spec §6.2 — the algorithm itself
    // is unchanged from the original draft; only which module executes it
    // has moved). Invokes visit once per resulting group — workItemID is
    // nil for the one group covering CostItems with no WorkItem, if any
    // exist (design spec §5.4).
    //
    // ERROR CONTRACT — CORRECTED, 2nd review round: the original draft
    // had estimates return ErrEstimateNotFound/ErrEstimateNotFinalized/
    // ErrEstimateProjectMismatch, sentinel errors it claimed were "defined
    // in quotations." That is impossible without an import cycle: for
    // estimates' own implementation to return a value equal (by
    // errors.Is) to a sentinel declared in the quotations package, it
    // would have to import quotations — directly reversing the
    // established one-way dependency (§22's own dependency graph) and
    // creating a cycle, since quotations already imports estimates for
    // this very interface. Matching error TEXT is not the same as
    // matching a Go sentinel value with errors.Is, so a text-based
    // workaround would silently break the moment either message string
    // changed.
    //
    // CORRECTED: eligibility is reported via plain booleans, not shared
    // error values. found/projectMatches/finalized/allocationEligible are
    // populated ONLY when err == nil (an infrastructure failure — e.g. a
    // Mongo error mid-lookup, or an unexpected error from
    // money.AllocateProportionally that isn't one of its own documented
    // eligibility-signaling cases, §19 — is reported via err alone, with
    // all four booleans left at their zero value and not meaningful).
    // When err == nil, quotations.Service inspects the four booleans
    // itself and maps them to ITS OWN sentinels (ErrEstimateNotFound/
    // ErrEstimateNotFinalized/ErrEstimateProjectMismatch/
    // ErrIneligibleCostBasisForQuotation, §20) — no sentinel of any kind
    // crosses the module boundary in either direction. visit is NEVER
    // invoked unless found && projectMatches && finalized &&
    // allocationEligible are ALL true; when any is false,
    // proposedSellingPrice/currency are the zero value and must not be
    // used.
    //
    // allocationEligible — ADDED, 3rd review round: closes a gap the 2nd
    // review round's own fix left open. §6.2 already established that a
    // non-positive WorkItem cost-group weight, or a largest-remainder
    // allocation that would still produce a zero-minor-unit share, must
    // cause the WHOLE call to fail before the visitor is ever invoked —
    // but an earlier draft of THIS interface still described that failure
    // as estimates returning ErrIneligibleCostBasisForQuotation, a
    // sentinel §20 defines inside quotations. That recreates the exact
    // import-cycle problem the found/projectMatches/finalized triple was
    // introduced to solve one field ago. allocationEligible extends the
    // SAME boolean-based contract to cover this fourth eligibility
    // dimension: set to false (with err == nil) whenever
    // AllocateProportionally's own precondition/post-allocation checks
    // (§19 — ErrNonPositiveAllocationTotal/ErrNoAllocationWeights/
    // ErrInvalidAllocationWeight/ErrZeroAllocationShare) would otherwise
    // have fired; estimates catches these internally and never lets
    // AllocateProportionally's own error values escape this method at
    // all. quotations.Service maps allocationEligible == false to
    // ErrIneligibleCostBasisForQuotation itself (§20) — again, entirely
    // inside quotations.
    //
    // NEITHER SnapshottedAmount NOR CostSubtotal NOR any intermediate
    // cost-weight value crosses this boundary at any point — the visit
    // callback's signature has no parameter capable of holding one
    // (design spec §4).
    VisitQuotationSeeds(
        ctx context.Context,
        companyID, projectID, estimateID string,
        visit func(workItemID *string, allocatedSellingAmount money.Money) error,
    ) (
        proposedSellingPrice money.Money,
        currency string,
        found bool,
        projectMatches bool,
        finalized bool,
        allocationEligible bool,
        err error,
    )
}

// WorkItemLookup — quotations needs customer-presentable WorkItem
// descriptions for line generation (design spec §5.3), which
// VisitQuotationSeeds deliberately does not provide at all (Description
// was removed from the capability entirely per Review Decision 2 — an
// Estimate's CostItem-level description was always the wrong source for a
// WorkItem-level customer-facing label, design spec §5.3).
//
// CORRECTED (issue 4, 2nd review round): projectID added as a required
// parameter. The original draft's GetWorkItemDescription(companyID,
// workItemID) validated only company-membership, which is insufficient
// once §13 allows a contractor to submit an ARBITRARY WorkItemID into
// SourceWorkItemIDs on a manual line edit (not just IDs the system itself
// generated) — a company-scoped-only check would let a contractor
// reference a real WorkItem belonging to a DIFFERENT Project under the
// same Company, silently misattributing traceability. found is a
// separate return value (not folded into err) for the identical reason
// given above for VisitQuotationSeeds — quotations, not work, decides
// what caller-facing error a "not found" maps to.
type WorkItemLookup interface {
    GetWorkItemDescription(
        ctx context.Context,
        companyID, projectID, workItemID string,
    ) (description string, found bool, err error)
}
```

**Satisfied structurally by:**
- `projects.Service` gains **one new method**, `GetProjectClientID` —
  the sole M2 modification this milestone requires (mirrors M4's single
  `VisitEstimatedCostItems` addition to `costs.Service` as "the one prior-
  milestone modification," M4 design spec's own pattern).
- `estimates.Service` gains **one new method**, `VisitQuotationSeeds` —
  a more heavily-loaded method than the original draft's
  `GetFinalizedEstimateForQuotation` (it now performs the full allocation
  computation itself, §6.2, not just a data lookup), but this is exactly
  the method M4's own design spec §31 anticipated needing, now correctly
  scoped so the privacy boundary is enforced by the module that already
  legitimately owns the cost data, not by convention inside the consumer.
  Internally, this method reuses the new `money.AllocateProportionally`
  helper (§19) — the same helper the original draft would have called from
  inside `quotations`, now called from inside `estimates` instead.
- `work.Service` gains **one new method**, `GetWorkItemDescription`.

**Compile-time dependency graph:**

```text
                    cmd/api (composition root)
                          │
        ┌─────────────────┼──────────────────┐
        ▼                 ▼                  ▼
     projects           work              costs
   (+ GetProjectClientID) │           (unchanged from M4)
        │                 │                  │
        │                 └────────┬─────────┘
        │                          ▼
        │                     estimates
        │            (+ VisitQuotationSeeds — now performs
        │             cost-grouping + proportional allocation
        │             INTERNALLY, per Review Decision 2)
        │                          │
        └──────────────┬───────────┘
                        ▼
                   quotations (NEW —
              consumes ProjectLookup from projects,
              WorkItemLookup from work,
              FinalizedEstimateSource from estimates —
              receives only WorkItemID + allocated selling
              amounts, never a cost figure)
```

Acyclic: `quotations` depends on already-constructed `projects`, `work`,
and `estimates`; none of the three gains any dependency on `quotations`.
No import cycle; fully constructible in one pass, matching every prior
milestone.

**Concrete construction order in `cmd/api/main.go`, appended after the
existing M4 block:**

```go
quotationsService := quotations.NewService(quotationRepo, quotationCounterRepo, projectsService, workService, estimatesService)
// consumes ProjectLookup (from projectsService, extended), WorkItemLookup
// (from workService, extended), FinalizedEstimateSource (from
// estimatesService, extended — VisitQuotationSeeds now does the
// allocation work internally, Review Decision 2)
```

---

## 23. HTTP API

All authenticated (`RequireAuthHuma`, registered on the existing
`authedAPI` group — no second `huma.API`). No endpoint accepts `companyId`.

| Method | Path | Purpose | Concurrency |
|---|---|---|---|
| POST | `/quotations` | Create Version 1 for a fresh commercial quotation — body: `projectId`, `estimateId`. Allocates a new `QuotationNumber` (§9.3), generates initial `Lines` from the Estimate (§5.3/§6.2). Always `draft`. | New `QuotationNumber` + `Version=1` allocation, no retry (mirrors M4 `CreateEstimate` — §22 below) |
| GET | `/quotations?projectId=...` | List every version of every quotation chain for one Project (parent validated first) | — |
| GET | `/quotations/{id}` | Get one specific Quotation version, tenant-scoped | — |
| PUT | `/quotations/{id}/lines` | Replace the entire `Lines` array — draft only (§13) | `Revision`-guarded |
| PATCH | `/quotations/{id}/terms` | Update `Terms`/`PaymentSchedule`/`Notes`/`ValidUntil` — draft only | `Revision`-guarded |
| PATCH | `/quotations/{id}/tax` | Update `TaxMode`/`TaxLabel`/`TaxRateBPS` — draft only | `Revision`-guarded |
| POST | `/quotations/{id}/finalize` | `draft → finalized`, one-directional, locks every field. Idempotent on retry. | `Revision`-guarded |
| POST | `/quotations/{id}/versions` | Create the next `Version` as a new draft — source must already be `finalized`; body: `estimateId` (explicit, §11) | Bounded-retry `Version` allocation (mirrors M4 `CreateNewVersion`) |

**No generic `PATCH /quotations/{id}`** for arbitrary field updates —
matching M4's own "every mutation is one of the purpose-specific
operations" rule exactly.

**No `GET /quotations/latest?projectId=...`** in M5 — unlike M4's
Estimate, where "the latest Estimate regardless of status" has an
immediate, obvious use (the contractor's own working view), a Quotation's
useful "latest" query is much more likely to mean "the latest version of
THIS specific `QuotationNumber` chain" (naturally satisfied by sorting the
`GET /quotations?projectId=...` list client-side, or — if genuinely needed
— a future `GET /quotations/{quotationNumber}/latest` keyed by number, not
by Project, since a Project could in principle have more than one
`QuotationNumber` chain, §9.2). Flagged as a possible near-term addition,
not built now, since M5's own list endpoint already exposes everything
needed to compute it client-side.

### 23.1 Idempotency — explicit scope (added, 2nd review round)

**Gap in the original draft: phase1.md §53 cites Payment creation,
Quotation acceptance, RFQ submission, etc. as requiring idempotency, and
this document defined `finalize` as service-idempotent (§12) — but never
stated the scope for `POST /quotations` itself.** A network-level retry of
`POST /quotations` after a lost response (client sends the request,
server processes it and allocates a real `QuotationNumber`, the response
is lost in transit, the client retries) would create a **second, distinct
Quotation chain with its own newly-allocated `QuotationNumber`** — the
server has no way to recognize the retry as "the same logical request" and
correctly return the first chain instead of minting a second one.

**Explicitly stated for M5 (the recommended option, adopted here):**
- `POST /quotations` is **not** retry-idempotent in M5. A lost-response
  retry can and will create a second `QuotationNumber` chain for what the
  contractor intended as one commercial quotation.
- Generic `Idempotency-Key`-based request deduplication (phase1.md §53's
  general mechanism, applicable platform-wide to Payment creation, RFQ
  submission, Supplier Offer submission, and this endpoint alike) is
  **deferred to a future platform-controls milestone** (a general
  idempotency-key persistence layer is not M5-specific — every §53-named
  operation needs the identical mechanism, so building one only for
  `POST /quotations` would be scope creep in the wrong direction; the
  correct fix is one shared platform capability, not N per-module
  reinventions).
- The `quotation_counters` gap-tolerance already documented in §2 already
  covers the resulting "wasted" `QuotationNumber" from an abandoned duplicate
  chain — no additional mechanism is needed to avoid a visible gap, since
  gaps are already an accepted, documented characteristic of the numbering
  scheme.
- `POST /quotations/{id}/versions` carries an **identical** gap for the
  same structural reason (a lost-response retry could create two `Version`
  numbers off the same finalized source instead of one) — also explicitly
  not retry-idempotent in M5, also deferred to the same future mechanism.
- `finalize` **remains service-idempotent** as already stated in §12 —
  this is a narrower, cheaper guarantee than full request-level
  idempotency (it works because `finalize` is a state *transition* with an
  naturally idempotent target state, `Status == finalized`, not because
  of any request-deduplication mechanism) and requires no additional
  design here.
- Draft mutations (`PUT .../lines`, `PATCH .../terms`, `PATCH .../tax`)
  are protected against double-application by `Revision` guarding (§13,
  §27.4) — a retried request with a now-stale `expectedRevision` is
  rejected with 409, not silently double-applied. This is a different
  (and already-adequate) mechanism from request-level idempotency, and is
  not part of this gap.
- **This is a known, accepted, documented pre-production limitation, not
  a silently-unstated one** — recorded here explicitly per this review
  round, and carried into §29's deferred-scope list.

---

## 24. M6 external-access boundary

```text
M5 (this document) — internal, authenticated only
        │
        ▼
   Quotation domain (finalized Quotation = "ready to send")

M6 (future, NOT built here)
        │
        ├── AccessGrant  (phase1.md §57 — resourceType: "quotation")
        ├── Secure token/link generation + hashing (phase1.md §35's
        │     Supplier-link pattern, applied to Quotation)
        ├── External, UNAUTHENTICATED Quotation DTO — a SEPARATE DTO
        │     from M5's own quotationDTO (§25), served through a
        │     completely different, token-scoped access path with no
        │     Principal/JWT involved at all — and, per the correction
        │     below, an EXPLICIT ALLOWLIST of fields, not the full
        │     contractor-facing DTO
        ├── Approve / Reject / Request-changes actions
        │     (phase1.md §28/§30/§31)
        └── Project.Status transition to quotation_sent/quotation_approved
              (phase1.md §5 — NOT triggered by M5's own finalize, §12)
```

**Corrected, 2nd review round: the original draft's claim that "M5's
`Quotation`/`QuotationLine` structs contain nothing that isn't already
meant for Client eyes" is no longer accurate once `GeneratedSubtotal`
(Review Decision 3) and the expanded `SourceWorkItemIDs` traceability
metadata exist on the domain model.** The internal-cost/margin firewall
(§4) remains fully valid — nothing added since the first review round
reopens *that* boundary. But a **second, distinct** privacy boundary
exists within the already-customer-safe data: certain fields are
legitimate for the **contractor's own authenticated view** of their
Quotation but were never meant for the Client to see at all:

```
Quotation.Notes              — internal-authoring notes, explicitly marked
                                 contractor-only in §1's own struct comment
Quotation.GeneratedSubtotal  — the "generated vs. current" comparison is a
                                 contractor bookkeeping aid (§6.3); a Client
                                 has no need to see what the contractor's
                                 tool originally proposed before manual edits
Quotation.EstimateID         — an internal foreign-key reference; exposing
                                 it externally serves no Client-facing
                                 purpose and needlessly reveals internal
                                 document linkage
Quotation.Revision           — a pure optimistic-concurrency counter,
                                 meaningless outside the authenticated
                                 draft-editing workflow
QuotationLine.SourceWorkItemIDs — internal traceability metadata; a Client
                                 has no use for a raw internal WorkItem ID
```

**M6's external DTO MUST be built as an explicit field allowlist against
`Quotation`/`QuotationLine`, never as "the contractor DTO minus whatever
we remember to strip."** This is the same discipline §4 already applies to
the Estimate→Quotation boundary (a capability whose return shape
structurally cannot carry a forbidden value, rather than a convention of
not serializing it) — restated here because M5's own `Quotation` aggregate
itself now contains a mix of Client-appropriate and contractor-only
fields, so the *next* boundary (Quotation→External DTO, built entirely in
M6) cannot rely on "just return the struct" and must instead be a
positive, explicit projection. M5's own job is only to ensure the
`Quotation` aggregate itself never crosses the *first* boundary (§4)
incorrectly — the second, contractor-vs-Client boundary within
already-customer-safe data is entirely M6's responsibility to build
correctly, not something M5 needs to (or can) enforce from the internal
side, since M5 has no external endpoint at all.

**Why M5 is otherwise already safe for M6 to build against, without any
M5 rework:**
- M6's external DTO only ever needs to read a `finalized` Quotation via
  the same underlying data M5's `GetQuotation` (backing
  `GET /quotations/{id}`) already provides — M6 only needs to swap the
  *authorization* mechanism (AccessGrant-token-derived scope instead of
  `Principal.CompanyID`) and apply its own explicit allowlist (above) on
  top, not build a new read path.
- M6's Approve action (phase1.md §31: "Quotation Version Locked") needs no
  new state on `Quotation` beyond what already exists — a `finalized`
  Quotation is already immutable (§12); "locking" it on acceptance is
  already true by construction, before M6 even exists.

---

## 25. Response DTOs and error mapping

```go
type quotationLineDTO struct {
    ID                string             `json:"id"`
    SourceWorkItemIDs []string           `json:"sourceWorkItemIds"` // empty array, not omitted, for a fully contractor-authored line — see §1
    Description       string             `json:"description"`
    Quantity          string             `json:"quantity,omitempty"` // decimal-as-string, matching work.WorkItem's own Quantity DTO convention
    Unit              string             `json:"unit,omitempty"`
    UnitPrice         *quotationMoneyDTO `json:"unitPrice,omitempty"`
    Amount            quotationMoneyDTO  `json:"amount"`
    SortOrder         int                `json:"sortOrder"`
}

type quotationDTO struct {
    ID                string              `json:"id"`
    ProjectID         string              `json:"projectId"`
    ClientID          string              `json:"clientId"`
    EstimateID        string              `json:"estimateId"`
    QuotationNumber   string              `json:"quotationNumber"`
    Version           int                 `json:"version"`
    Status            string              `json:"status"`
    Revision          int64               `json:"revision"`
    Currency          string              `json:"currency"`
    Lines             []quotationLineDTO  `json:"lines"`
    Subtotal          quotationMoneyDTO   `json:"subtotal"`
    GeneratedSubtotal quotationMoneyDTO   `json:"generatedSubtotal"` // frozen baseline, §1/§6.3, Review Decision 3 — contractor-authenticated response only; M6's external DTO may omit this field entirely (§24)
    TaxMode           string              `json:"taxMode"`
    TaxLabel          string              `json:"taxLabel,omitempty"`
    TaxRateBPS        int64               `json:"taxRateBps,omitempty"`
    TaxAmount         quotationMoneyDTO   `json:"taxAmount"`
    Total             quotationMoneyDTO   `json:"total"`
    Terms             string              `json:"terms,omitempty"`
    PaymentSchedule   string              `json:"paymentSchedule,omitempty"`
    ValidUntil        string              `json:"validUntil,omitempty"`
    Notes             string              `json:"notes,omitempty"`
    CreatedAt         string              `json:"createdAt"`
    FinalizedAt       string              `json:"finalizedAt,omitempty"`
}
```

Package-qualified `quotationMoneyDTO` (not the bare `moneyDTO` the M3
schema-registry collision already taught this codebase to avoid) — direct
carry-forward of M4's own `estimateMoneyDTO` naming discipline.

**Error → HTTP status mapping** (mirrors M4's `mapEstimatesError` shape
exactly):

| Domain error | HTTP status |
|---|---|
| `ErrQuotationNotFound` | 404 |
| `ErrProjectNotFound` | 404 |
| `ErrEstimateNotFound` | 404 |
| `ErrWorkItemNotFound` | 404 |
| `ErrEstimateNotFinalized` | 422 |
| `ErrEstimateProjectMismatch` | 422 |
| `ErrIneligibleCostBasisForQuotation` | 422 |
| `ErrQuotationCurrencyMismatch` | 422 |
| `ErrLineCurrencyMismatch` | 422 |
| `ErrIncompleteLinePricing` | 422 |
| `ErrInvalidLineQuantity` | 422 |
| `ErrInvalidLineUnitPrice` | 422 |
| `ErrLineAmountMismatch` | 422 |
| `ErrLineAmountNotPositive` | 422 |
| `ErrEmptyLines` | 422 |
| `ErrDuplicateWorkItemIDInLine` | 422 |
| `ErrUnknownLineID` | 422 |
| `ErrDuplicateLineID` | 422 |
| `ErrBlankLineDescription` | 422 |
| `ErrInvalidTaxMode` | 422 |
| `ErrInvalidTaxRate` | 422 |
| `ErrInvalidTaxLabel` | 422 |
| `ErrQuotationNotDraft` | 409 |
| `ErrQuotationMustBeFinalizedBeforeNewVersion` | 409 |
| `ErrRevisionMismatch` | 409 |
| `ErrVersionConflict` | 409 (also returned when §27.3's bounded version-allocation retry loop is exhausted — same sentinel, not a distinct one, §27.5) |
| `ErrDraftAlreadyExists` | 409, never retried (§27.3 step 5) |
| `ErrUnclassifiedDuplicateKey` | 500, never retried (§27.3 step 6) |
| `ErrQuotationNumberAllocationFailed` | 500 — SCOPE CORRECTED (§27.5): covers ONLY a genuine infrastructure failure of the `quotation_counters` counter increment itself, never version-retry exhaustion (`ErrVersionConflict`) or an unclassified collision (`ErrUnclassifiedDuplicateKey`) |

---

## 26. MongoDB indexes

```go
// quotations collection
{Keys: bson.D{{Key: "companyId", Value: 1}},
    Options: options.Index().SetName("idx_quotations_company")} // corrected, 2nd review round — see below
{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
    Options: options.Index().SetName("idx_quotations_company_project")}
{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "quotationNumber", Value: 1}, {Key: "version", Value: 1}},
    Options: options.Index().SetUnique(true).SetName("uq_quotations_company_number_version")}
{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "quotationNumber", Value: 1}},
    Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "draft"}).
        SetName("uq_quotations_one_draft_per_number")}

// quotation_counters collection (§9.3)
{Keys: bson.D{{Key: "companyId", Value: 1}},
    Options: options.Index().SetUnique(true).SetName("uq_quotation_counters_company")}
```

**Every index carries an explicit name, with no exceptions — corrected, 2nd
review round.** The original draft's very first index
(`{companyId: 1}`, the plain per-Company lookup index) was left unnamed,
directly contradicting this section's own next paragraph, which claimed
"every index carries an explicit name." Corrected: named
`idx_quotations_company`, above. This is a direct, deliberate
carry-forward of the M4 lesson (M4 design spec §20's own retrospective:
two same-key-pattern indexes with no explicit names would collide on
MongoDB's default auto-generated name, and would additionally make
substring-based duplicate-key classification ambiguous). Here,
`idx_quotations_company`, `idx_quotations_company_project`, and
`uq_quotations_company_number_version`/`uq_quotations_one_draft_per_number`
don't share a literal key pattern the way M4's two `{companyId,projectId}`
indexes did, but the **explicit-name-always** discipline is applied
uniformly regardless, specifically so that `uq_quotations_company_number_version`
is never a substring of a longer index name (or vice versa) — the same
root-cause protection M4's retrospective called for, applied preemptively
here rather than only after a collision is discovered.

**"At most one draft per `QuotationNumber` chain at a time"** — enforced
by `uq_quotations_one_draft_per_number`, the direct M5 analogue of M4's
`uq_estimates_one_draft_per_project` partial unique index, scoped to
`{companyId, quotationNumber}` instead of `{companyId, projectId}` (since
the invariant here is per-chain, not per-Project — §9.2/§10).

---

## 27. Optimistic concurrency and allocation — full design

Mirrors M4's own two-tier structure (§21 of the M4 spec) precisely, with
one additional tier for `QuotationNumber`:

### 27.1 `QuotationNumber` allocation (§9.3) — atomic counter, no retry
loop needed
`FindOneAndUpdate({companyId}, {$inc: {nextNumber: 1}}, upsert: true)`
against `quotation_counters` is atomic by construction — no race window,
no retry logic required. A failure here (a genuine MongoDB error, not a
logical conflict) surfaces as `ErrQuotationNumberAllocationFailed` (500) —
there is no "conflict to retry," only "the write itself failed," an
ordinary infrastructure-error case.

### 27.2 `POST /quotations` (`CreateQuotation`) — no `Version` retry, ever
Mirrors M4's `CreateEstimate` exactly, substituting `quotationNumber` for
`projectId` as the version-scoping key: allocate a fresh `QuotationNumber`
(§27.1) first, then attempt `Version = 1` directly, exactly once. Any
collision on either unique index (should be near-impossible immediately
after allocating a **brand-new** `QuotationNumber`, but defended against
identically to M4's own belt-and-suspenders handling) is surfaced, never
retried into a recalculated higher version — the identical race M4's
design spec §21.1 worked through in detail applies unchanged, substituting
`quotationNumber` for `projectId` as the shared key two concurrent callers
could theoretically race on.

### 27.3 `POST /quotations/{id}/versions` (`CreateNewVersion`) — bounded
retry, identical shape to M4 §21.2
1. Reject up front (409) if the source is not `finalized`.
2. Read `MAX(version)` for `{companyId, quotationNumber}`.
3. Attempt insert at `MAX + 1`.
4. On a collision against `uq_quotations_company_number_version` (another
   concurrent request won the exact same version number): re-read
   `MAX(version)` and retry, bounded at 5 attempts (matching M4's
   `maxVersionAllocationAttempts`). Classified as `ErrVersionConflict`
   (§20) — retriable.
5. On a collision against `uq_quotations_one_draft_per_number` instead
   (a genuine, non-transient "a draft already exists for this chain"
   condition — should be rare given step 1's precondition check, but
   possible as a real race, e.g. two concurrent requests both passing
   step 1 against the same now-finalized source): classified as
   `ErrDraftAlreadyExists` (§20, corrected — the original draft named this
   condition in prose but never added the sentinel to §20's error list) —
   surfaced as 409 directly, **never retried in the bounded loop above**,
   mirroring M4 §21.2 step 4's identical distinction between a transient
   version-number race and a non-transient existing-draft conflict.
6. A duplicate-key error matching **neither** named index is classified as
   `ErrUnclassifiedDuplicateKey` (§20, corrected — also missing from the
   original draft's §20 list) and is **never** assumed safe to retry —
   surfaces as 500, identical to M4's own `ErrUnclassifiedDuplicateKey`
   handling (M4 design spec §20-21). The classification mechanism itself
   (inspecting the MongoDB write-error message for one of the two known
   index names as a substring, per M4's own established pattern) is
   identical to `internal/estimates`' `classifyCreateError` — `quotations`
   implements its own copy against its own index names, not a shared
   helper (matching every other cross-module pattern in this codebase:
   no shared "duplicate-key classifier" package is introduced merely
   because two modules happen to need similar logic).

### 27.4 Draft mutations (`PUT .../lines`, `PATCH .../terms`,
`PATCH .../tax`, `POST .../finalize`) — `Revision`-guarded, no retry
Identical contract to M4 §16.1-16.3: caller supplies `expectedRevision`;
mismatch → 409, no state change, caller must re-`GET` and retry manually
(never auto-retried server-side — a stale-numbers hazard is exactly what
this guard exists to surface to the human, not silently paper over).

### 27.5 `ErrQuotationNumberAllocationFailed` — scope corrected

**Restricted, 2nd review round: this sentinel covers ONLY a genuine
infrastructure failure of the `quotation_counters`
`FindOneAndUpdate`/`$inc` call itself (§27.1) — e.g. a Mongo connectivity
error mid-increment.** It does **not** cover version-allocation retry
exhaustion (§27.3 step 4's bounded retry loop, which — on exhaustion —
returns `ErrVersionConflict`, the same sentinel a single unretried
collision would return, not a distinct "gave up retrying" error) and does
**not** cover an unclassified duplicate-key error (§27.3 step 6, which
returns `ErrUnclassifiedDuplicateKey`). The original draft's error-mapping
table description ("unclassified/exhausted-retry allocation failure")
conflated three genuinely different failure modes under one sentinel;
corrected here and in §25's mapping table.

---

## 28. Testing and acceptance criteria

To be covered by the eventual TDD implementation plan (not written yet).
Listed here so that plan can be derived mechanically from this approved
design, matching M4's own §26-30 precedent:

**Unit tests:**
- `QuotationStatus`/`TaxMode` `IsValid()` methods.
- `money.AllocateProportionally` — happy path: `Σ shares == total` across
  varied weight distributions (equal weights, wildly skewed weights, a
  single weight, weights summing to a value not evenly dividing `total`),
  largest-remainder tie-breaking is deterministic; every returned share is
  strictly positive. These tests live in `internal/foundation/money`'s own
  suite (matching every prior helper's precedent, §19) — NOT in
  `internal/quotations`, since the helper is called from inside
  `internal/estimates` (Review Decision 2).
- **`money.AllocateProportionally` — corrected error-path coverage (Issue
  2, 2nd review round; the original draft left this undefined):**
  `total.Amount <= 0` → `ErrNonPositiveAllocationTotal`; empty `weights` →
  `ErrNoAllocationWeights`; any individual `weights[i] <= 0` (including a
  zero weight among otherwise-positive weights — the original draft's own
  test matrix included this case but never stated an expected outcome) →
  `ErrInvalidAllocationWeight`, rejected BEFORE any allocation is
  attempted; a weight distribution engineered to produce at least one
  zero-minor-unit share even after largest-remainder residual
  distribution (e.g. `total = 3` minor units, `weights = [1, 1, 1, 1, 1,
  1, 1, 1, 1, 1]` — ten equal weights against a 3-unit total) →
  `ErrZeroAllocationShare`, the entire call fails, no partial result is
  returned.
- `money.ApplyRateBPS`: matches `AddRateBPS(base, rate) - base` for the
  same inputs (cross-check against the already-tested markup helper).
- **`estimates.VisitQuotationSeeds`'s internal grouping-by-WorkItemID and
  allocation logic** (pure-function-level, isolated from the Mongo layer,
  tested inside `internal/estimates`, not `internal/quotations` — this is
  now estimates' own responsibility per Review Decision 2): given a set of
  `EstimateCostLine`s across several WorkItemIDs (plus a WorkItemID-less
  subset, §5.4), the returned seeds' `allocatedSellingAmount`s sum exactly
  to `proposedSellingPrice`, and no seed's `allocatedSellingAmount` is
  computed from anything other than that WorkItem's own cost share.
  **New (Issue 2, corrected 3rd review round):** a WorkItem group whose
  summed `SnapshottedAmount`s is zero or negative (a legitimate case even
  under a positive overall `CostSubtotal` — §6.2's corrected worked
  example) causes `VisitQuotationSeeds` to return
  `allocationEligible=false, err=nil` BEFORE invoking the visit callback
  even once — asserted directly at the `estimates`-internal level (this
  method's own return values, `allocationEligible` and the untouched
  `err`), not as `estimates` returning any `quotations`-defined sentinel
  (which it never does — see Issue 3 below, the identical import-cycle
  concern applied here).
  **New (Issue 3, extended 3rd review round):** `VisitQuotationSeeds`'s
  four eligibility booleans (`found`/`projectMatches`/`finalized`/
  `allocationEligible`) are each tested independently — a nonexistent
  `estimateID` → `found=false`; an existing Estimate belonging to a
  different Project → `found=true, projectMatches=false`; an existing,
  project-matching but still-`draft` Estimate →
  `found=true, projectMatches=true, finalized=false`; a found, project-
  matching, finalized Estimate whose cost basis is ineligible per the
  above → `found=true, projectMatches=true, finalized=true,
  allocationEligible=false`; visit is confirmed NEVER invoked unless all
  four are true. Separately, at the `internal/quotations` service layer:
  each of the four `false` cases is confirmed to map to its own correct
  sentinel (`ErrEstimateNotFound`/`ErrEstimateProjectMismatch`/
  `ErrEstimateNotFinalized`/`ErrIneligibleCostBasisForQuotation`, §20),
  proving the boolean-to-sentinel mapping itself, entirely inside
  `quotations`, is correct — not merely that `estimates` reports the right
  booleans.
- `Σ QuotationLine.Amount == Subtotal` invariant, both at generation time
  and after arbitrary line edits.
- `GeneratedSubtotal` is set once at generation (creation or new-version)
  and is never mutated by any subsequent line/terms/tax edit — proven by
  asserting it is absent from every mutation's `$set` document at the
  repository layer, not merely absent from the observed end-to-end result
  (Review Decision 3).
- Tax calculation, corrected input-combination coverage (Issue 6, §15.1):
  `TaxMode=none` with `TaxLabel != ""` → `ErrInvalidTaxLabel`; `TaxMode=none`
  with `TaxRateBPS != 0` → `ErrInvalidTaxRate`; `TaxMode=percentage` with
  blank `TaxLabel` → `ErrInvalidTaxLabel`; `TaxMode=percentage` with
  `TaxRateBPS == 0` → `ErrInvalidTaxRate` (zero is excluded from percentage
  mode, §15.1); `TaxMode=percentage` with `TaxRateBPS > 10000` →
  `ErrInvalidTaxRate`; an unrecognized `TaxMode` string → `ErrInvalidTaxMode`;
  the valid case (`TaxMode=none` → zero `TaxAmount`; valid `TaxMode=percentage`
  → matches `ApplyRateBPS` exactly) → `Total = Subtotal + TaxAmount`.
- Optional line-field validation (Correction C, §7.1, corrected further in
  this round — Issue 1): each of the three partial
  `Quantity`/`Unit`/`UnitPrice` combinations independently rejected with
  `ErrIncompleteLinePricing`; `Quantity <= 0` → `ErrInvalidLineQuantity`;
  `UnitPrice.Amount <= 0` (both zero AND negative — corrected from the
  original draft's "zero is allowed" position) → `ErrInvalidLineUnitPrice`;
  line product validation: `Quantity × UnitPrice != Amount` →
  `ErrLineAmountMismatch`.
- Zero/negative line amount rejection (Correction C, §7.2): `Amount <= 0`
  on any submitted line → `ErrLineAmountNotPositive`, regardless of whether
  the line is amount-only or has a full `Quantity`/`Unit`/`UnitPrice` triple.
- `SourceWorkItemIDs` slice semantics (Review Decision 1, §5.3, contract
  made precise in this round — Issue 4): a generated (unmerged) line
  carries exactly one ID; a manually-merged line (submitted via
  `PUT .../lines` with a `sourceWorkItemIds` array equal to the union of
  two prior lines') carries the union of both; a fully contractor-authored
  new line carries an empty slice (present as `[]`, not omitted); splitting
  one WorkItem's value across two lines is representable by both lines
  independently carrying that same WorkItemID; a duplicate WorkItemID
  within ONE line's own array → `ErrDuplicateWorkItemIDInLine`.
- `PUT /quotations/{id}/lines` line-ID handling (Issue 4, §13.1): a
  submitted line with an `ID` matching an existing line on this Quotation
  → treated as an edit; a submitted line with an `ID` not matching any
  existing line → `ErrUnknownLineID`; two submitted lines sharing the same
  `ID` → `ErrDuplicateLineID`; a submitted line with no `ID` → server
  generates a fresh one; a blank (or whitespace-only) `Description` →
  `ErrBlankLineDescription`.
- `PUT /quotations/{id}/lines` WorkItem-ID membership validation (Issue 4):
  a `sourceWorkItemIds` entry belonging to a DIFFERENT Project under the
  same Company → `ErrWorkItemNotFound` (via `WorkItemLookup`'s
  `found=false`, §22); a `sourceWorkItemIds` entry belonging to a
  different Company entirely → `ErrWorkItemNotFound`, same shape (§21).

**Testcontainers integration tests (repository layer):**
- `QuotationNumber` allocation: concurrent `FindOneAndUpdate` calls against
  the same `companyId` never produce a duplicate number (proven with real
  concurrent goroutines against real MongoDB, matching M4's own concurrency
  proof style).
- `Version` allocation: concurrent `CreateNewVersion` attempts for the same
  `{companyId, quotationNumber}` never produce duplicate version numbers.
- One-draft-per-chain partial unique index actually rejects a second draft.
- `Revision`-guarded conditional updates reject a stale `expectedRevision`.

**Full HTTP tenant-isolation acceptance matrix** (mirroring M4's own §28):
- Cross-tenant 404 on every `quotations` endpoint.
- Cross-project Estimate rejection (`estimateId` belongs to caller's
  Company but a DIFFERENT Project) → `ErrEstimateProjectMismatch`, 422.
- Cross-company Estimate rejection → `ErrEstimateNotFound`, 404 (same
  shape as every other cross-tenant reference check in this codebase).
- Non-finalized (draft) Estimate rejection → `ErrEstimateNotFinalized`, 422.
- **Internal cost information never appears in any Quotation HTTP
  response** — an explicit acceptance test that inspects the raw JSON
  response body and asserts the absence of any field name matching
  `snapshottedAmount`/`costSubtotal`/`pricingMode`/`pricingRate`/
  `projectedGrossProfit`/`projectedGrossMarginBps`/`category` (the internal
  CostItem category taxonomy) anywhere in the payload — not merely
  asserting specific known-safe fields are present, since an
  absence-of-forbidden-fields test catches an accidental future leak that
  a presence-only test would miss. Now more directly enforced than the
  original draft anticipated: since `VisitQuotationSeeds` structurally
  cannot return any of these values (Review Decision 2, §4), this test's
  role shifts from "prove the DTO layer doesn't leak a value it received"
  to "prove no future regression accidentally widens the capability
  interface's return shape back into carrying one" — still worth keeping
  as a black-box HTTP-level check, not merely a compile-time type-shape
  argument.
- **Currency-substitution rejection** (Correction B, §8): a
  `PUT /quotations/{id}/lines` payload containing one line whose
  `Amount.Currency` (or `UnitPrice.Currency`, when supplied) differs from
  the Quotation's own `Currency` → `ErrLineCurrencyMismatch`, 422, draft's
  existing `Lines` left completely untouched (no partial update).
- Quotation total correctness: `Σ Lines[].Amount == Subtotal`,
  `Subtotal + TaxAmount == Total`, for both a freshly-generated Quotation
  and one with hand-edited lines; `GeneratedSubtotal` remains equal to the
  Estimate's `ProposedSellingPrice` at the moment of generation even after
  `Subtotal` has since diverged from it via hand-edits (Review Decision 3).
- Line allocation rounding: generate a Quotation from an Estimate whose
  `ProposedSellingPrice` does NOT divide evenly across its WorkItems'
  cost-share weights, confirm the sum still matches exactly (no residual
  dropped/duplicated).
- WorkItem-less cost inclusion (Review Decision 6, §5.4): an Estimate with
  at least one `WorkItemID == nil` CostItem produces a generated Quotation
  with one extra line, `Description == "General Project Works and
  Services"`, whose `Amount` is included in `Σ Lines[].Amount ==
  GeneratedSubtotal`; an Estimate with NO such CostItems produces no extra
  line at all (no zero-amount placeholder).
- Draft editing: line replace (including a merge that produces a
  multi-element `SourceWorkItemIDs`, and a split that duplicates one
  WorkItemID across two lines), terms/tax updates, each independently.
- Optimistic concurrency: a stale `Revision` on any of the four mutating
  endpoints → 409, document unchanged.
- Finalized immutability: every mutating endpoint against a `finalized`
  Quotation → 409, document unchanged.
- Version history: `GET /quotations?projectId=...` returns every version
  of every chain for that Project.
- `QuotationNumber` uniqueness across the whole Company (not just within
  one Project).
- Concurrent version creation: proven with real concurrent HTTP requests
  against a real MongoDB-backed server, matching M4's own concurrency
  acceptance-test style (not merely a repository-layer unit proof).
- **Duplicate-key error classification (Issue 5, §27.3):** a version-
  number race between two concurrent `POST /quotations/{id}/versions`
  calls against the same finalized source → the loser transparently
  retries and succeeds at the next version number (never surfaces an
  error to the caller, §27.3 step 4); a genuine existing-draft race (two
  concurrent calls both passing the finalized-source precondition
  simultaneously) → exactly one succeeds, the other receives
  `ErrDraftAlreadyExists`, 409, NOT retried (§27.3 step 5) — asserted by
  checking the loser's response arrives promptly, not after the bounded
  retry loop's full delay, which would indicate it was incorrectly
  retried as a version conflict instead.
- **Idempotency gap, explicitly documented as expected M5 behavior, not a
  bug (§23.1):** two sequential `POST /quotations` calls with identical
  bodies (simulating a naive client-side retry with no
  `Idempotency-Key` mechanism, since none exists in M5) produce **two
  separate Quotation chains** with two distinct `QuotationNumber`s — this
  test exists to lock in the documented limitation as a known,
  intentional behavior, so a future change to this behavior (e.g. adding
  idempotency-key support) is a deliberate, visible decision rather than
  an accidental regression discovered later.
- Snapshot stability: creating `Quotation V1` from `Estimate V1`, then
  later finalizing `Estimate V2` for the same Project, does NOT alter
  `Quotation V1`'s already-persisted `Lines`/`Subtotal`/`Total` in any way.
- New-Estimate-doesn't-affect-old-Quotation-version: identical proof, one
  layer further — a `Quotation V2` explicitly created against `Estimate
  V2` coexists with `Quotation V1` (still bound to `Estimate V1`) without
  either affecting the other.
- **New-version regeneration is fresh, not carried forward** (Review
  Decision 4, §11 step 4): create `Quotation V1` from `Estimate V1`, hand-
  edit one of its lines' `Amount` away from its generated value, finalize
  `V1`; finalize a new `Estimate V2` with different underlying cost figures
  for the same Project; call `POST /quotations/{id}/versions` supplying
  `Estimate V2`'s ID. Assert `Quotation V2`'s `Lines`/`Subtotal`/
  `GeneratedSubtotal` reflect a fresh allocation from `Estimate V2` — NOT
  `V1`'s hand-edited amounts carried forward — while `Terms`/
  `PaymentSchedule`/`ValidUntil`/`Notes`/`TaxMode`/`TaxLabel`/`TaxRateBPS`
  ARE copied forward from `V1` unchanged.
- **`finalize` never touches `Project.Status`** (Review Decision 5, §12):
  `POST /quotations/{id}/finalize` against a `Project` whose `Status` is
  (e.g.) `estimating` leaves `Project.Status` unchanged — asserted by a
  direct `GET /projects/{id}` before and after the finalize call, values
  identical.
- All protected `quotations` endpoints declare `security:
  [{bearerAuth: []}]` in the served `/openapi.json` (matching every prior
  milestone's final verification step).
- Single-`huma.API` registration — `quotations.RegisterHandlers` mounted
  on the existing `authedAPI` group, no second `humachi.New(...)` anywhere
  (the exact M2-era regression this codebase already fixed once and must
  never reintroduce).

---

## 29. Explicitly deferred scope

Everything M6 owns (§24): Access Grants, secure Client links/tokens, the
Client Quotation Portal, Client accept/reject/request-changes, Approval
records, `Project.Status` transitions to `quotation_sent`/
`quotation_approved` (Review Decision 5, §12). Also deferred, independent
of M6: AI Quotation Writing Assistant (text generation itself — the schema
already has a `Description` field for it to eventually populate, §0.1),
PDF generation, Documents-module integration, discounts (§14), structured
payment milestones (§16), Company-level default quotation/payment terms
pre-filling (§16), automatic validity-expiry enforcement (§17), tax-rate
legal-correctness validation or jurisdiction rule engines (§15), a
dedicated "latest Quotation for a chain" endpoint (§23).

**Two additional items deferred per the review round (§31):**
- **Preserving edited `Description` text across new-version regeneration**
  when the same `WorkItemID` still appears in the new Estimate's line set
  (Review Decision 4, §11 step 4) — M5's baseline is a fully fresh
  regeneration on every new version; a future enhancement could detect
  "this WorkItemID's generated line existed, with contractor-edited text,
  in the prior version" and carry the *text* (never the amount) forward
  automatically. Not built in M5.
- **Any reconciliation-visibility UX for `GeneratedSubtotal` vs. `Subtotal`
  divergence** (Review Decision 3, §6.3) beyond the two raw fields
  themselves — e.g. a computed "percentage adjustment" figure, a warning
  threshold, or a contractor-facing changelog of which specific lines were
  edited. M5 stores and returns the two baseline numbers only; any richer
  presentation is a frontend/future-M5-extension concern.

**Three further items deferred per the 2nd review round:**
- **Generic `Idempotency-Key`-based request deduplication** (§23.1) — a
  platform-wide mechanism covering `POST /quotations`,
  `POST /quotations/{id}/versions`, and every other §53-named operation
  (Payment creation, RFQ submission, Supplier Offer submission) alike, not
  an M5-specific concern. `POST /quotations` and `POST .../versions` are
  both **not** retry-idempotent in M5 — a lost-response client retry can
  produce a duplicate Quotation chain or a duplicate new version. This is
  a known, accepted, documented pre-production gap (§23.1), not a silently
  unstated one.
- **Preserving edited line `Description`/`SortOrder`/merge-grouping across
  new-version regeneration** more broadly than the `Description`-only case
  already named above — any UX that reduces the manual re-editing burden
  after `POST /quotations/{id}/versions` beyond what §11 already carries
  forward (`Terms`/`PaymentSchedule`/`ValidUntil`/`Notes`/tax
  configuration) is out of M5's scope.
- **The Estimate-side business process for correcting a negative or
  non-positive WorkItem cost-group weight** (§6.2's new
  `ErrIneligibleCostBasisForQuotation` case) — M5 detects and reports this
  condition, but building any guided in-app workflow to help a contractor
  locate and fix the offending `CostItem` (beyond "use the existing M3
  `PATCH /cost-items/{id}/lifecycle` endpoint, then refresh/re-finalize
  the Estimate," already available today) is not part of M5.

---

## 30. Open design decisions requiring resolution before implementation

**None remain open.** All six decisions raised in the initial design
review draft were resolved by explicit user direction in the first review
round — see §31 for the full record of each decision and its resolution.
This document is the corrected, post-review design, ready for final
approval.

---

## 31. Review log

### Review round 1 (2026-07-24)

All six decisions from the initial draft's "DECISIONS REQUIRING USER
CONFIRMATION" section were resolved — every one in favor of "Option A" as
originally recommended, with three additional corrections identified
during review that the original draft had gotten wrong or left
insufficiently precise. Recorded here for traceability, matching M4 design
spec §32's own precedent.

**1. Quotation line granularity and generation model — APPROVED Option A,
with one model correction.** One system-generated line per WorkItem,
contractor may edit/reorder/merge/split/replace afterward — approved
exactly as recommended (§5.3). **Correction applied:** `QuotationLine`'s
traceability field changed from `SourceWorkItemID *string` to
`SourceWorkItemIDs []string` (§1, §5.3) — a single optional string cannot
represent a contractor-merged line (multiple WorkItems collapsed into one
grouped/fixed-package line, per §25's presentation modes) without
destroying traceability for every WorkItem except whichever one the merged
line happened to keep. The slice form represents all three needed cases
(one generated line → one-element slice; a merged line → multi-element
slice; a fully manual line → empty slice) and also naturally represents a
split (the same WorkItemID appearing on two separate lines).

**2. Selling-price allocation method — APPROVED Option A (proportional,
largest-remainder), with the privacy-boundary implementation corrected.**
The allocation algorithm itself (§6.2) — proportional by cost share,
largest-remainder residual assignment, exact-sum guaranteed — was approved
exactly as originally proposed; equal-split (Option B) and no-system-
allocation (Option C) were both correctly rejected in the original draft's
own reasoning. **Correction applied (the most structurally significant
change in this review round):** the original draft's `FinalizedEstimateSource`
capability still carried each `EstimateCostLine`'s raw `SnapshottedAmount`
(plus internal `Category`) across the module boundary into
`internal/quotations`, to be used there only as an allocation weight and
never stored — the review correctly identified that "never stored" is a
weaker guarantee than "never received," since the raw cost figure still
entered `quotations`' own process memory and call stack. **Resolution:**
the entire cost-weighting and proportional-allocation computation (§6.2's
Steps 1-3) now runs INSIDE `internal/estimates`, behind a renamed
capability, `VisitQuotationSeeds` (§4, §18/§22), whose return shape
structurally cannot carry a cost figure, a category, or a description at
all — it returns exactly `(workItemID *string, allocatedSellingAmount
money.Money)` per seed, both already customer-safe by construction. This
makes the privacy firewall genuinely structural (the type has no field to
misuse) rather than a "received but not persisted" convention a future
refactor could accidentally violate.

**3. Manual divergence — APPROVED Option A (divergence permanently
allowed, fully visible), with a new field added to make "fully visible"
concrete.** Approved exactly as recommended: a Quotation's `Subtotal`/
`Total` are always the live sum of current `Lines[].Amount`, and may
legitimately diverge from what generation originally produced, matching
phase1.md §20's "contractor is not locked into calculated values"
principle one layer up. **Addition applied:** a new field,
`Quotation.GeneratedSubtotal` (§1, §6.3), captures
`Estimate.ProposedSellingPrice` (equivalently, the freshly-generated
`Σ allocatedSellingAmount`) once, at generation time, and freezes — never
touched by any later line/terms/tax edit. This lets the
contractor-authenticated response show "generated vs. current" (e.g.
`RM62,500` generated vs. `RM62,000` current, a `-RM500` manual
adjustment) without inventing any new privacy exposure:
`GeneratedSubtotal` is itself a customer-facing selling-price figure
(seeded from `ProposedSellingPrice`, never from a cost or margin field),
so storing and displaying it does not reopen the §4 firewall.

**4. New-version generation — APPROVED Option A (fresh regeneration from
the newly-referenced Estimate), rejecting Option B (carry-forward of the
prior version's amounts) explicitly.** `POST /quotations/{id}/versions`
(§11) requires an explicit `estimateId` and regenerates `Lines`/
`Subtotal`/`GeneratedSubtotal` entirely fresh from that Estimate via a new
`VisitQuotationSeeds` call — the source Quotation's own (possibly
hand-edited) line amounts are never copied forward, closing the risk of a
new version being priced against outdated assumptions while nominally
referencing a newer Estimate. `Terms`/`PaymentSchedule`/`ValidUntil`/
`Notes`/tax configuration ARE carried forward unchanged, since none of
those are derived from Estimate cost data. A future enhancement
(preserving edited line *descriptions* where the same WorkItemID persists
across Estimate versions) is explicitly named and deferred (§29), not
built now.

**5. Project status — APPROVED Option A (finalize never touches
`Project.Status`).** `Quotation.finalize` means "ready to send," not
"sent to the Client" — phase1.md's own status name (`Quotation Sent`, not
`Quotation Finalized`) already names the second, distinct event, which
only M6's actual delivery mechanism performs. `quotations` gains no write
capability into `projects` in M5 at all (§12, §22) — `ProjectLookup`
remains read-only.

**6. WorkItem-less costs — APPROVED Option A (include, don't silently
exclude), with the generated line's default wording corrected.** CostItems
with `WorkItemID == nil` are grouped into one additional generated
QuotationLine at generation time (via `VisitQuotationSeeds`'s nil-
`workItemID` seed, §5.4), consistent with M4's own "make cost-coverage
gaps visible" philosophy. **Wording correction applied:** the generated
line's default `Description` is `"General Project Works and Services"`
(§5.4), not `"Miscellaneous"` — the review correctly identified
"Miscellaneous" as reading like an untrustworthy catch-all to a Client,
where "General Project Works and Services" signals a legitimate,
already-estimated cost category. Contractor-renameable like any other
generated line.

**Three additional corrections, independent of the six decisions above:**

**A. `quotation_counters` collection ownership.** The original draft's
collection-ownership table (§2) listed only `quotations`, omitting the
`quotation_counters` collection the numbering design (§9.3) already
required. Corrected: both collections now appear in §2's ownership table,
both exclusively owned by `quotations`. Also added: an explicit statement
that counter gaps are acceptable and are never repaired or rolled back —
no transaction wraps counter allocation together with the Quotation
document write, since phase1.md does not require gapless numbering and
the standard lock-free atomic-counter tradeoff (occasional gaps, no
contention) is the right one here.

**B. Currency structural-impossibility claim, corrected.** The original
draft claimed a mixed-currency Quotation was "structurally impossible"
because `QuotationLine` has no separate currency field of its own. This
was incorrect: `QuotationLine.Amount` and `.UnitPrice` are both
`money.Money`, which carries its own `Currency` string — nothing in the Go
type system prevents a line's `Amount.Currency` from being constructed
differently from the parent Quotation's own `Currency`. Corrected (§8):
the invariant (`Line.Amount.Currency == Quotation.Currency`, and the same
for `UnitPrice`/`Subtotal`/`GeneratedSubtotal`/`TaxAmount`/`Total`) is now
explicitly enforced by server-side validation at every write path
(generation, and every draft line edit), not claimed as a type-level
guarantee it never was. A new sentinel error, `ErrLineCurrencyMismatch`
(§20), and a dedicated acceptance test for a currency-substitution attempt
(§28) were added.

**C. Optional line-field and zero-amount validation, made precise.** The
original draft gestured at "Quantity/Unit/UnitPrice, all or nothing" and
"Amount is always present" without stating the exact validation rules.
Corrected (§7.1/§7.2): the all-or-nothing rule is now stated exactly
(`ErrIncompleteLinePricing` on any partial combination), together with
the specific per-field rules when all three are present (`Quantity > 0`,
`Unit` non-blank, `Amount` must equal their computed product). Also
resolved: `QuotationLine.Amount` and `Quotation.Subtotal` must both be
strictly positive (`> 0`) — no zero-valued financial line is representable
in M5; a free/included item is represented textually within another
priced line's `Description` instead. **`UnitPrice.Amount` was initially
stated as "`>= 0`, zero permitted for a transparency line" in this
document's first review-round revision — this was itself a real bug,
caught and corrected in the 2nd review round (§31's next entry, Issue 1):
a zero `UnitPrice` forces a zero `Amount` under the product-must-match
rule, which the very next section unconditionally rejects. The two rules
were simultaneously unsatisfiable. `UnitPrice.Amount` is now `> 0`,
consistently with `Amount`'s own positivity requirement.**

---

### Review round 2 (2026-07-24)

A second review pass, focused on mathematical correctness, module-boundary
soundness, and executable precision of the design produced by round 1,
found and corrected six further issues plus two documentation gaps. None
of the six decisions resolved in round 1 were reopened — all six
corrections below are consistency/precision fixes within the already-
approved design direction, not changes of direction.

**Issue 1 — `UnitPrice = 0` was simultaneously required and forbidden.**
§7.1 (as revised in round 1) permitted `UnitPrice.Amount == 0` as a
"transparency line" case while §7.2 unconditionally required `Amount > 0`
— but with `Quantity`/`Unit`/`UnitPrice` all present, `Amount` is *derived*
as their product, so `UnitPrice = 0` forces `Amount = 0`, which §7.2
rejects. **Fixed:** `UnitPrice.Amount` must now be strictly positive
(`> 0`) whenever all three fields are supplied — the "transparency line"
example and its permissive rationale were removed entirely, and
`ErrInvalidLineUnitPrice`'s doc comment corrected from "must not be
negative" to "must be strictly positive" (§7.1, §20).

**Issue 2 — the allocation algorithm's preconditions were mathematically
false, and its test matrix left the zero-weight case undefined.** The
original claim "a WorkItem group with a non-positive cost weight cannot
exist under `CostSubtotal > 0`" is false — a positive Project-wide total
does not imply every individual WorkItem group is positive (a worked
counterexample, `WorkItem A: RM1,000`, `WorkItem B: -RM200`, `CostSubtotal:
RM800`, is now recorded directly in §6.2). Separately, largest-remainder
rounding does not itself guarantee every resulting share is nonzero.
**Fixed:** `money.AllocateProportionally`'s signature now returns
`([]Money, error)` (§19) and validates: `total.Amount > 0`
(`ErrNonPositiveAllocationTotal`), `len(weights) > 0`
(`ErrNoAllocationWeights`), every `weights[i] > 0`
(`ErrInvalidAllocationWeight`), and every resulting share `> 0`
(`ErrZeroAllocationShare`) — the whole call fails rather than returning an
invalid or zero share. `estimates.VisitQuotationSeeds` (§6.2, §22)
propagates any of these as the new `ErrIneligibleCostBasisForQuotation`
(§20) BEFORE invoking its visit callback even once — there is no
partial/best-effort Quotation generation. The original draft's own test
matrix ("a zero weight among nonzero weights," §28) is now given an
explicit expected outcome: rejection, not silent success.

**Issue 3 — the capability error contract required an import cycle that
cannot exist.** The original draft had `estimates.VisitQuotationSeeds`
return sentinel errors (`ErrEstimateNotFound`/`ErrEstimateNotFinalized`/
`ErrEstimateProjectMismatch`) it described as "defined in quotations" —
impossible without `estimates` importing `quotations`, which would reverse
the established one-way dependency (§22) and create a cycle, since
`quotations` already imports `estimates` for this very interface.
**Fixed:** `VisitQuotationSeeds` now returns three plain booleans
(`found`, `projectMatches`, `finalized`) instead of any shared error value
(§22) — populated only when `err == nil`; the visit callback is invoked
only when all three are true; `quotations.Service` maps the booleans to
its own locally-defined sentinels itself. No shared error package or
adapter is introduced, and ADR 0002's one-way dependency is preserved
exactly.

**Issue 4 — merge/split line editing had no defined request contract.**
§5.3 (round 1) asserted `SourceWorkItemIDs` changes "only as a side effect
of merge/split operations," but no such operation was ever defined — the
only line-mutation endpoint is the whole-array `PUT /quotations/{id}/lines`,
which had no documented way to derive the resulting WorkItem IDs.
**Fixed:** §13.1 (new) defines `SourceWorkItemIDs` as plain, directly
editable metadata on each submitted line — a "merge" is simply submitting
one line whose array is the union of two prior lines'; a "split" is
submitting two lines sharing one WorkItemID; no new server operation
exists beyond the array-replace already documented. Full server-side
validation is specified: WorkItemID membership (Company AND Project,
via `WorkItemLookup`'s corrected `(companyID, projectID, workItemID)`
signature, §22), no duplicate WorkItemID within one line
(`ErrDuplicateWorkItemIDInLine`), reuse across lines explicitly permitted,
an empty array explicitly valid, and full line-ID handling (existing ID
must belong to this Quotation or `ErrUnknownLineID`; missing ID means
server-generated; duplicate submitted IDs rejected with
`ErrDuplicateLineID`; blank `Description` rejected with
`ErrBlankLineDescription`).

**Issue 5 — duplicate-key errors were referenced in prose but absent from
the domain-error model, and one index was unnamed.** §27.3's prose
distinguished a version-number conflict from an existing-draft conflict
from an unclassified collision, but only `ErrVersionConflict` existed in
§20's actual error list; `ErrQuotationNumberAllocationFailed`'s
description conflated three different failure modes. **Fixed:**
`ErrDraftAlreadyExists` and `ErrUnclassifiedDuplicateKey` added to §20;
§27.5 (new) restricts `ErrQuotationNumberAllocationFailed` to genuine
`quotation_counters` infrastructure failures only — version-retry
exhaustion now correctly returns `ErrVersionConflict` (the same sentinel a
single collision would return, not a distinct "gave up" error), and an
unclassified collision returns `ErrUnclassifiedDuplicateKey`. Separately,
§26's first index (`{companyId: 1}`) was left unnamed despite that same
section's own next paragraph claiming "every index carries an explicit
name" — corrected to `idx_quotations_company`.

**Issue 6 — tax input validation was undefined.** §15 stated the two
`TaxMode` values and the calculation formula but never the accepted input
combinations. **Fixed:** §15.1 (new) states the exact rule for each mode
(`none` requires an empty label and zero rate; `percentage` requires a
non-blank label and `0 < TaxRateBPS <= 10000`, deliberately excluding a
zero rate under `percentage` mode as a redundant representation of "no
tax"), with three new sentinels (`ErrInvalidTaxMode`/`ErrInvalidTaxRate`/
`ErrInvalidTaxLabel`, §20), all mapped to 422 (§25), and an explicit
all-or-nothing rejection (a rejected tax update leaves the draft
completely untouched).

**Documentation correction A — M6 needs an explicit external field
allowlist, not "the DTO already contains nothing Client-inappropriate."**
The original §24 claimed `Quotation`/`QuotationLine` "contain nothing that
isn't already meant for Client eyes" — no longer true once
`GeneratedSubtotal` and the expanded `SourceWorkItemIDs` exist. **Fixed:**
§24 now explicitly identifies the contractor-only fields
(`Notes`/`GeneratedSubtotal`/`EstimateID`/`Revision`/
`SourceWorkItemIDs`) and states that M6's external DTO **must** be built
as a positive, explicit allowlist against the internal `Quotation`
struct — never derived by "the contractor DTO minus whatever we remember
to strip." The internal-cost/margin firewall (§4) remains fully intact
and unaffected; this is a second, distinct privacy boundary within
already-customer-safe data.

**Documentation correction B — idempotency scope was never stated for
`POST /quotations`.** phase1.md §53 names Quotation-adjacent operations
among those needing idempotency, and this document defined `finalize` as
idempotent, but never addressed `POST /quotations` itself. **Fixed:**
§23.1 (new) explicitly states `POST /quotations` and
`POST /quotations/{id}/versions` are **not** retry-idempotent in M5 (a
lost-response client retry can create a duplicate chain/version); generic
`Idempotency-Key` support is deferred to a future platform-wide mechanism
(§29), since every §53-named operation needs the identical capability,
not a per-module reinvention; this is recorded as a known, accepted,
documented pre-production gap, locked in by an explicit acceptance test
(§28) rather than left silently unstated.

---

### Review round 3 (2026-07-24)

A third, narrower review pass caught one place where round 2's own fix was
incompletely applied, one stale table cell round 2 should have updated but
missed, and one formula error introduced by round 1's own
`GeneratedSubtotal` addition once tax entered the picture. All three are
precision/consistency fixes; none reopen any of the nine decisions already
resolved in rounds 1-2.

**Issue 1 — round 2's own fix still leaked a cross-package sentinel one
level down.** Round 2 correctly replaced
`ErrEstimateNotFound`/`ErrEstimateNotFinalized`/`ErrEstimateProjectMismatch`
with the `found`/`projectMatches`/`finalized` boolean triple to avoid
`estimates` needing to import `quotations` — but §6.2's own prose (and the
matching test-matrix wording, §28) still described the SEPARATE
cost-basis-eligibility failure (a non-positive WorkItem weight, or a
largest-remainder allocation producing a zero share) as
`VisitQuotationSeeds` "returning `ErrIneligibleCostBasisForQuotation`," the
exact same import-cycle mistake the boolean triple was introduced to fix,
just for a fourth eligibility dimension instead of the first three.
**Fixed:** `VisitQuotationSeeds`'s signature (§22) gains a fourth boolean,
`allocationEligible`, populated identically to the other three (`true`/
`false` when `err == nil`, meaningless when `err != nil`) — `estimates`
catches its own internal `AllocateProportionally` errors (§19 —
`ErrNonPositiveAllocationTotal`/`ErrNoAllocationWeights`/
`ErrInvalidAllocationWeight`/`ErrZeroAllocationShare`) and reports their
occurrence via this boolean rather than ever letting any error value
originating in either `money` or a `quotations`-defined sentinel escape
this method. `quotations.Service` maps `allocationEligible == false` to
`ErrIneligibleCostBasisForQuotation` itself, entirely inside `quotations`
— the sentinel's definition (§20) is unchanged, only which module
produces the mapping. §6.2's narrative and §28's test-matrix wording were
both corrected to describe the boolean, not a returned error.

**Issue 2 — §5.3's own field table still described `SourceWorkItemIDs` as
non-editable, contradicting §13.1 two sections later.** §13.1 (added in
round 2) correctly defines `SourceWorkItemIDs` as directly submittable,
server-validated metadata through `PUT /quotations/{id}/lines` — merge and
split are conventions for using that endpoint, not separate operations.
But §5.3's "what is persisted vs. derived vs. editable" table, written
before §13.1 existed, still said the field "is never hand-edited directly
by the contractor... only changes as a side effect of merge/split line
operations," describing exactly the undefined-operation model round 2's
own §13.1 fix replaced. **Fixed:** the table row (§5.3) now states
"Contractor-editable through `PUT /quotations/{id}/lines` as directly
submitted, server-validated traceability metadata," cross-referencing
§13.1, matching what the rest of the document (correctly) already said.

**Issue 3 — the "manual commercial adjustment" example used `Total`
instead of `Subtotal`, which becomes wrong once tax is configured.**
`Quotation.Total` is `Subtotal + TaxAmount` (§7); `GeneratedSubtotal` was
seeded from `Estimate.ProposedSellingPrice`, a pre-tax, line-level
commercial figure (§6.2) that has no tax dimension in it at all. The
original worked example in both §1 and §6.3 computed the "manual
commercial adjustment" figure as `Total - GeneratedSubtotal` — once
`TaxMode = percentage` is configured, this formula would incorrectly
attribute the tax amount itself to "manual adjustment," showing a nonzero
adjustment even when the contractor made no line edits at all. **Fixed:**
both worked examples (§1, §6.3) now compute this figure as `Subtotal -
GeneratedSubtotal` — the correct pre-tax-to-pre-tax comparison, with §6.3
adding an explicit paragraph explaining why `Subtotal`, not `Total`, is
the right comparator once tax exists as a separate dimension.

---
