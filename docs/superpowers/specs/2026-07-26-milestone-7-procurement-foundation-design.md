# Milestone 7 — Procurement Foundation: Design Specification

**Status:** **Approved** (revision 3, 2026-07-26)
**Date:** 2026-07-26
**Scope:** Internal procurement preparation. Material Requirements, Supplier
Directory, Supplier Offerings, supplier-neutral RFQ drafts.
**Excludes:** every externally-visible procurement step (see §20).

This document is the **implementation source of truth** for Milestone 7.
Implementation is in progress against the approved 29-checkpoint TDD sequence.

**Revision 4 applied one implementation-discovered amendment
(`ReadClaimSnapshot`, §1.3); revision 3 applied ten final corrections;
revision 2 applied nine blocking corrections.** See §23 for all change logs.

---

## 0. Investigation Summary

### 0.1 What phase1.md requires

| § | Requirement | Where it lands in M7 |
|---|---|---|
| §32 | Material Requirements reference `projectId`, `workItemId`, `materialId`, `requiredQuantity`, `unit`. "The contractor can review and modify the requirements before Procurement." | §2 model, §3 generation, §5 review state |
| §12 | Company-scoped Supplier Directory: name, contact person, email, phone, address, material categories, notes, active/inactive. Search by name, filter by category. | §4.1 Supplier |
| §12 | "A Material may optionally reference `Preferred Supplier`" | §4.3 `MaterialSupplierPreference` — a **separate M7-owned relationship**, not a field on `Material` (§0.3 conflict A) |
| §12 | Suppliers do not require platform accounts; participate via restricted RFQ access | M8 |
| §33 | RFQ contains project reference, requested materials, specifications, quantities, units, delivery location, required delivery date, contractor notes, response deadline | §6 RFQ model |
| §33 | "The RFQ is the shared request definition. The RFQ Invitation is the Supplier-specific delivery and access record." | Confirms supplier-neutral RFQ (§6); Invitation is M8 |
| §33.1 | Supplier suggestions from category + preferred + history — "advisory only" | §4.3 preference is advisory; suggestion ranking itself is deferred (§20) |
| §53 | Idempotency for "RFQ submission", "RFQ Invitation creation" | §11 idempotency model |
| §54 | Editable documents maintain a version; update succeeds only if stored version matches | `Revision` on every mutable M7 aggregate (§10) |
| §55 | Intentional MongoDB structure | §12 collections and indexes |

phase1.md §32 is **three sentences and a field list**. It does not specify
statuses, generation idempotency, re-sync, splitting, or RFQ line snapshots.
Those are this document's decisions, taken under the locked design decisions
in the M7 brief.

### 0.2 What the code already provides `[code]`

- `internal/suppliers` and `internal/procurement` exist as **empty placeholder
  packages containing only `doc.go`**. M7 creates
  `internal/materialrequirements` and `internal/rfqs`, fills
  `internal/suppliers`, and leaves the unused `internal/procurement`
  placeholder to be removed (§1.1).
- `internal/quotations` is the structural precedent M7 follows throughout:
  - Four distinct identity fields — `QuotationNumber` (chain) / `Version` /
    `Revision` (concurrency) / `SchemaVersion` — explicitly never conflated
    (`quotations/quotation.go:51-59`).
  - `MongoQuotationCounterRepository.NextQuotationNumber` allocates numbers via
    `FindOneAndUpdate` + `$inc` + `upsert:true` — "atomic by construction, no
    read-then-write race window, no retry loop needed"
    (`quotations/repository_mongo.go:371-385`).
  - Every index carries an **explicit name**, and `classifyCreateError` maps a
    duplicate-key error to a named sentinel by matching the index name in the
    write-error message, returning `ErrUnclassifiedDuplicateKey` rather than
    guessing (`quotations/repository_mongo.go:36-41, 193-209`).
  - `conditionalUpdate` gates every mutation on
    `{_id, companyId, status:draft, revision:expected}`, and on
    `MatchedCount == 0` re-reads to distinguish not-found from revision
    mismatch (`quotations/repository_mongo.go:284-307`).
- `internal/access` provides the coordinator-authoritative precedent: a single
  document both racing paths must win a conditional write against, which is
  "what makes the accepted-chain invariant hold across two different
  collections" (`access/grant.go:74-79`). M7 applies the same idea with the
  Material Requirement as the serialization point (§7).
- `access.ResourceTypeQuotation` is documented as existing so "the same
  collection/index shape generalizes to `rfq` later without a migration"
  (`access/grant.go:5-8`) — M8 alignment already anticipated.
- `audit.AuditRecorder` is one typed method per event taking **only
  primitives**, deliberately with no generic metadata map
  (`audit/audit.go:6-12, 63-69`).
- Capability interfaces are **consumer-defined** and satisfied structurally
  (`costs.ProjectLookup`, `costs.WorkItemLookup`, `costs.MaterialLookup` —
  `costs/service.go:70-89`). `internal/platform/composition` adapters exist
  only where Go's exact-return-type rule forces a conversion
  (`cmd/api/main.go:224-242`).
- `costs.Service.VisitEstimatedCostItems` is the precedent for crossing a
  module boundary with "only primitives and money.Money — no CostItem struct
  crosses the module boundary (ADR 0002)", **and for returning a bare primitive
  summary count alongside the visit** — `(int, error)`, not a named struct
  (`costs/service.go:331-357`). §1.2's five primitive ineligible counters follow
  that precedent exactly, which is what lets `costs.Service` satisfy
  `MaterialCostSource` with no import and no adapter.
- Tenant invariant, stated in code: "a foreign parent ID returns 404, never an
  empty list" (`costs/service.go:234-236`).
- `work.WorkItemStatus` is `planned | cancelled`, cancelled terminal
  (`work/work_item.go:21-33`). `work.Service` currently exposes **ownership
  checks only** — no status accessor (`work/service.go:167-180`). §1.2 adds a
  narrow one.
- `materials.Material` has `Name`, `Category`, `Specification`, `Unit`,
  `ReferencePrice` — and **no `Active` / archival field of any kind**
  (`materials/material.go:23-34`). §0.3 conflict D.
- `foundation/quantity.Quantity` is `{decimal.Decimal, Unit string}`;
  `foundation/money.Money` is `{int64 minor units, Currency}`. Per ADR 0001 all
  rounding goes through `money.RoundToMinorUnits`.

### 0.3 Genuine conflicts found, and their resolutions

**Conflict A — `Preferred Supplier` on Material.** phase1.md §12 says a
Material may reference a Preferred Supplier. The implemented M3 `Material`
(`materials/material.go:23-34`) deliberately omits it, and M7 may not modify
M3 models.

*Resolution (locked):* M7 satisfies the requirement through a company-scoped
`MaterialSupplierPreference` relationship owned by `internal/suppliers`. The
M3 `Material` model is not modified. At most one preferred Supplier per
Material, enforced by a unique `{companyId, materialId}` index. Advisory only.
Not modelled on `SupplierOffering`, because a Supplier may be preferred for a
Material even when no product/SKU record exists.

**Conflict B — `CostItem` has no `Revision`.** `CostItem`
(`costs/cost_item.go:80-99`) carries `SchemaVersion` but no `Revision`, so
re-sync cannot compare a source version.

*Resolution (locked):* M7 detects source changes by deterministic value
comparison plus a SHA-256 source fingerprint. M3 CostItems are not modified.
Re-sync recomputes the whole current aggregate for the same source key and
reports a discrepancy when the aggregate quantity or the contributing source
set changes. The stored requirement is never overwritten automatically. (§5)

**Conflict C — Material catalog unit vs CostItem quantity unit.** `Material`
has a catalog `Unit`; each material `CostItem` carries its own
`Quantity.Unit`. They can disagree.

*Resolution (locked):* Aggregation uses the **CostItem quantity unit** as part
of the key. When it differs from the catalog unit the requirement is still
generated, preserves the CostItem unit, and is marked with a unit-mismatch
condition requiring contractor review. M7 performs **no automatic unit
conversion and never sums values across different units**. An unresolved
mismatch prevents RFQ inclusion. (§3.5)

**Conflict D — Material has no active/archival state.** `materials.Material`
has no `Active` field, no `ArchivedAt`, and no status of any kind. M7 must not
invent one, and must not modify M3 to add one.

*Resolution:* `MaterialLookup.GetMaterialReference` returns
`(name, catalogUnit, specification, found, err)` — **no `active` bool**. M7
requires a Material to **exist and belong to the company** when establishing a
new link (offering, preference, manual requirement, material change). M7 has
no concept of an inactive Material, no `ErrMaterialInactive`, and no
`material_inactive` skip reason. Existing offering and preference links remain
historically readable if a Material later becomes unavailable in some future
milestone's sense, but M7 does not describe or model that as `active = false`.
Only **Supplier** and **SupplierOffering** have an `Active` flag in M7 — both
are M7-owned records where M7 defines the field.

---

## 1. Module ownership and capability interfaces

### 1.1 Three modules

| Module | Owns collections | Owns |
|---|---|---|
| `internal/materialrequirements` | `material_requirements` | Material Requirements, generation, aggregation, fingerprints, re-sync, discrepancy resolution, split, the RFQ claim **field** |
| `internal/rfqs` | `rfqs`, `rfq_counters` | RFQs, RFQ lines, snapshotting, numbering, lifecycle, **all claim orchestration and reconciliation** |
| `internal/suppliers` | `suppliers`, `supplier_offerings`, `material_supplier_preferences` | Supplier Directory, Offerings, preferred-supplier relationship |

Dependency direction is **one-way**: `rfqs → materialrequirements`.
`materialrequirements` never imports `rfqs`, never inspects an RFQ line, and
never implements `retry_line`. `suppliers` is independent of both — M7 puts no
SupplierID on an RFQ or a Material Requirement, because supplier selection is
M8.

The existing empty `internal/procurement` placeholder package is **removed**;
its `doc.go` describes a single-module design this spec supersedes. No other
M3–M6 module is modified except by adding the three narrow read capabilities in
§1.2 and the audit methods in §1.5.

### 1.2 Capabilities `materialrequirements` consumes

```go
// package materialrequirements

type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}
// satisfied by projects.Service (existing method, unchanged)

// WorkItemProcurementContext confirms lineage AND exposes cancellation, so
// generation can skip cancelled scope. work.Service exposes ownership checks
// only today (work/service.go:167-180); this is a NEW narrow method.
type WorkItemLookup interface {
    WorkItemProcurementContext(ctx context.Context, companyID, workItemID, projectID string) (
        cancelled bool, found bool, err error)
}

// GetMaterialReference returns catalog descriptive fields. It deliberately
// returns NO active/status bool: materials.Material has no such field and M3
// is frozen (§0.3 conflict D).
type MaterialLookup interface {
    GetMaterialReference(ctx context.Context, companyID, materialID string) (
        name string, catalogUnit string, specification string, found bool, err error)
}

// MaterialCostSource yields eligible material CostItem rows. The visitor
// NEVER aggregates — it exposes eligible source rows only.
// materialrequirements owns aggregation, fingerprints and discrepancy logic.
// costs owns eligibility, so eligibility rules live in exactly one place.
//
// The ineligible counters are returned as PRIMITIVES, not as a named struct.
// A named consumer-owned return type would force costs.Service to import
// materialrequirements to satisfy the interface — Go requires an exact named
// return type — which would invert the dependency direction and require a
// second adapter. Primitives keep costs.Service a direct structural
// implementation, following the precedent of VisitEstimatedCostItems returning
// a bare missingCount (costs/service.go:331-357).
type MaterialCostSource interface {
    VisitEligibleMaterialCostItems(
        ctx context.Context, companyID, projectID string,
        visit func(costItemID string, workItemID string, materialID string,
            quantity decimal.Decimal, quantityUnit string) error,
    ) (
        nonMaterialCategory int,
        missingMaterialID int,
        missingWorkItemID int,
        missingOrZeroQuantity int,
        blankUnit int,
        err error,
    )
}
```

`materialrequirements.Service` converts those primitive counters into its own
HTTP response summary (§3.8). No struct crosses this boundary in either
direction, and `costs` imports nothing from `materialrequirements`.

`workItemID` and `materialID` are **non-pointer** on the visitor: `costs`
filters out any CostItem lacking them, so `materialrequirements` never receives
a partially-populated row.

### 1.3 Capabilities `rfqs` consumes

```go
// package rfqs

type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// MaterialRequirementSource is the ONLY way rfqs reaches a requirement.
// rfqs imports NO materialrequirements package type: every method takes
// primitives and returns primitives or rfqs-owned projections (ADR 0002).
// Satisfied via the single composition adapter (§1.4.1).
type MaterialRequirementSource interface {
    // ClaimForRFQ performs the Revision-guarded conditional claim and returns
    // the snapshot fields taken from the SAME document the claim validated
    // (§7.2 step 3-4).
    //
    // projectID is the RFQ's own ProjectID and is enforced INSIDE the atomic
    // claim filter. Without it, a reviewed requirement belonging to a
    // different Project of the same company could be claimed by this RFQ.
    ClaimForRFQ(ctx context.Context, companyID, projectID, requirementID string,
        expectedRevision int64, rfqChainID, rfqNumber, lineID string) (ClaimSnapshot, error)

    // ReleaseClaim clears a claim, conditional on companyID + requirementID +
    // the exact chain ID + the exact line ID + expectedRevision.
    ReleaseClaim(ctx context.Context, companyID, requirementID string, expectedRevision int64,
        rfqChainID, lineID string) error

    // ReadClaim reports one requirement's claim state by requirement ID.
    ReadClaim(ctx context.Context, companyID, requirementID string) (
        rfqChainID string, rfqNumber string, lineID string, revision int64,
        found bool, err error)

    // ListClaimsForRFQChain enumerates every requirement currently claiming
    // this chain, WITHOUT the caller needing to know requirement IDs in
    // advance. This is what makes an orphaned claim discoverable: ReadClaim
    // alone can only confirm a claim the caller already suspects (§7.5).
    ListClaimsForRFQChain(ctx context.Context, companyID, rfqChainID string) (
        []RFQRequirementClaim, error)

    // ClaimedRequirementIsReadyForRFQ validates an ALREADY-CLAIMED requirement
    // at mark-ready time. It deliberately does NOT apply the unclaimed §8.5
    // predicate: that predicate requires activeRfqChainId == null, which is
    // false for every requirement already on a line, so applying it here would
    // make every non-empty RFQ impossible to mark ready.
    //
    // It requires: status == reviewed; requiredQuantity.value > 0; unit
    // mismatch absent or acknowledged; sourceSyncState == clean;
    // activeRfqChainId == rfqChainID; activeRfqLineId == lineID.
    ClaimedRequirementIsReadyForRFQ(ctx context.Context, companyID, requirementID,
        rfqChainID, lineID string) (bool, error)

    // ReadClaimSnapshot returns the allowlisted projection for a requirement
    // THIS chain already holds under THIS line, WITHOUT claiming it. It exists
    // solely so an interrupted claim-first add can be repaired (§7.3) and
    // retry_line can rebuild a missing line (§7.5).
    //
    // Scoped to the EXACT existing claim. The underlying read requires ALL of:
    //     companyId
    //     requirementId
    //     activeRfqChainId == rfqChainID
    //     activeRfqLineId  == lineID
    // so rfqs can never obtain a snapshot from an unclaimed requirement, one
    // claimed by another chain, one claimed under another line, or another
    // tenant. Every failing case returns ErrMaterialRequirementNotFound rather
    // than a distinct sentinel: from the caller's position there is no
    // repairable claim, and saying more would disclose the state of a claim it
    // does not hold.
    //
    // It deliberately does NOT re-run the §8.5 eligibility predicate. The claim
    // was eligible when it was created, and while it exists every
    // supplier-visible contractor field is frozen (§2.3) — only detection state
    // may move (§5.8). Requiring current eligibility would make an interrupted
    // line impossible to repair after an ordinary source change, even though
    // the snapshot being rebuilt is exactly the one the claim validated.
    ReadClaimSnapshot(ctx context.Context, companyID, requirementID,
        rfqChainID, lineID string) (ClaimSnapshot, error)
}

// ClaimSnapshot is rfqs-owned: the allowlisted projection rfqs snapshots into
// a line. It carries no cost, no margin, and no InternalNotes.
type ClaimSnapshot struct {
    RequirementID    string
    Revision         int64
    MaterialID       string
    MaterialName     string
    Specification    string
    QuantityValue    string // canonical decimal string
    QuantityUnit     string
    RequiredByDate   *time.Time
    ProcurementNotes string
}

// RFQRequirementClaim is rfqs-owned: one requirement's claim on this chain.
type RFQRequirementClaim struct {
    RequirementID string
    RFQChainID    string
    RFQNumber     string
    LineID        string
    Revision      int64
}
```

`IssuanceStatusSource` — the M8 seam, consumed by `rfqs`:

```go
// package rfqs
type IssuanceStatusSource interface {
    RFQChainIssued(ctx context.Context, companyID, rfqChainID string) (bool, error)
}

type NoExternalIssuanceSource struct{}
func (NoExternalIssuanceSource) RFQChainIssued(context.Context, string, string) (bool, error) {
    return false, nil
}
```

Keyed on **`rfqChainID`, never `RFQNumber`** — `RFQNumber` is a tenant-scoped
display identifier; the stable aggregate ID is the cross-module identity M8
uses. M8 later supplies a composition-root adapter and **never writes into
`rfqs`, `rfq_counters` or `material_requirements`**.

### 1.4 Capabilities `suppliers` consumes

```go
// package suppliers
type MaterialLookup interface {
    GetMaterialReference(ctx context.Context, companyID, materialID string) (
        name string, catalogUnit string, specification string, found bool, err error)
}
```

Same shape as §1.2's, declared separately per ADR 0002 — not a shared
interface package. Both are satisfied structurally by the one new
`materials.Service` method.

### 1.4.1 The one required composition adapter

`materialrequirements.Service` naturally returns `materialrequirements`-owned
snapshot and claim types, while `rfqs` declares `rfqs.ClaimSnapshot` and
`rfqs.RFQRequirementClaim`. Go interface satisfaction requires exact named
return types, so `composition.NewMaterialRequirementSourceAdapter(mrService)`
performs those conversions at the leaf of the dependency graph — the same
reason `composition.NewQuotationSourceAdapter` exists
(`cmd/api/main.go:224-242`). It also translates `materialrequirements`' claim
sentinels into `rfqs`' own.

The adapter converts **six** methods, not five. `ReadClaimSnapshot` returns a
`materialrequirements.ClaimSnapshot` that must become an `rfqs.ClaimSnapshot`,
and its `ErrMaterialRequirementNotFound` — returned for a missing requirement
**and** for every exact-claim mismatch (wrong chain, wrong line, unclaimed,
foreign tenant) — must become `rfqs.ErrMaterialRequirementNotFound`. E1's
adapter tests cover the conversion and the sentinel translation for this method
alongside the other five.

**The adapter is the only package in the codebase that imports both `rfqs` and
`materialrequirements`.** `rfqs` imports no `materialrequirements` type at all,
and `materialrequirements` imports nothing from `rfqs`.

This is the **only** M7 adapter. Every other capability passes primitives only
— including `MaterialCostSource`, whose ineligible counters are primitives
precisely so `costs.Service` needs no adapter (§1.2) — so `projects.Service`,
`work.Service`, `materials.Service`, `costs.Service` and `audit.Service`
satisfy their interfaces directly.

### 1.5 Audit — three consumer-owned interfaces

There is **no shared M7 audit interface**. Each module declares only what it
records:

```go
// package materialrequirements
type AuditRecorder interface {
    RecordMaterialRequirementsGenerated(ctx context.Context, companyID, projectID, actorUserID string,
        createdCount, unchangedCount, discrepancyCount, sourceRemovedCount, skippedCount int) error
    RecordMaterialRequirementCreated(ctx context.Context, companyID, projectID, actorUserID,
        requirementID, materialID, sourceType string) error
    RecordMaterialRequirementUpdated(ctx context.Context, companyID, projectID, actorUserID,
        requirementID string, reviewReset bool) error
    RecordMaterialRequirementReviewed(ctx context.Context, companyID, projectID, actorUserID,
        requirementID, materialID string) error
    RecordMaterialRequirementUnitAcknowledged(ctx context.Context, companyID, projectID, actorUserID,
        requirementID, procurementUnit, catalogUnit string) error
    RecordMaterialRequirementDiscrepancyResolved(ctx context.Context, companyID, projectID, actorUserID,
        requirementID, action, syncStateBefore, anchorStatus string) error
    RecordMaterialRequirementSplit(ctx context.Context, companyID, projectID, actorUserID,
        sourceRequirementID, splitGroupID string, childCount int) error
    RecordMaterialRequirementArchived(ctx context.Context, companyID, projectID, actorUserID,
        requirementID string) error
}

// package rfqs
type AuditRecorder interface {
    RecordRFQCreated(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber string) error
    RecordRFQUpdated(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber string) error
    RecordRFQLineAdded(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber, requirementID, lineID string) error
    RecordRFQLineRemoved(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber, requirementID, lineID string) error
    RecordRFQMarkedReady(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber string, lineCount int) error
    RecordRFQReopened(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber string) error
    RecordRFQDeleted(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, rfqNumber string) error
    RecordRFQClaimReconciled(ctx context.Context, companyID, projectID, actorUserID,
        rfqChainID, requirementID, action string) error
}

// package suppliers
type AuditRecorder interface {
    RecordSupplierCreated(ctx context.Context, companyID, actorUserID, supplierID, supplierName string) error
    RecordSupplierUpdated(ctx context.Context, companyID, actorUserID, supplierID string) error
    RecordSupplierActiveStateChanged(ctx context.Context, companyID, actorUserID, supplierID string, active bool) error
    RecordSupplierOfferingCreated(ctx context.Context, companyID, actorUserID, supplierID, offeringID string) error
    RecordSupplierOfferingUpdated(ctx context.Context, companyID, actorUserID, supplierID, offeringID string) error
    RecordSupplierOfferingActiveStateChanged(ctx context.Context, companyID, actorUserID, supplierID, offeringID string, active bool) error
    RecordPreferredSupplierChanged(ctx context.Context, companyID, actorUserID, materialID, supplierID string) error
    RecordPreferredSupplierCleared(ctx context.Context, companyID, actorUserID, materialID, previousSupplierID string) error
}
```

**Method counts, stated as declared above:** `materialrequirements` 8, `rfqs`
8, `suppliers` 8 — **24 total**. Acceptance test 89 asserts exactly these
counts; the implementer must recount against the final interfaces rather than
trusting this sentence if the two ever disagree.

Audit-coverage decisions made explicitly, per your question: manual
requirement **creation** and **update** are audited
(`RecordMaterialRequirementCreated` / `Updated`, the latter carrying whether
review was reset); **unit acknowledgement** is audited (it relaxes an RFQ
eligibility gate); **RFQ header update** is audited (it changes
supplier-visible scope); **empty-RFQ deletion** is audited (a number is
consumed and a record disappears). `RecordRFQLineDriftResolved` is **gone**
along with the drift subsystem (§6.3).

`audit.Service` gains matching methods and satisfies all three structurally.
Every parameter is a primitive. New `audit` subject types:
`material_requirement`, `rfq`, `supplier`, `supplier_offering`,
`material_supplier_preference`.

### 1.6 Composition-root wiring

```go
// repositories
materialRequirementRepo := materialrequirements.NewMongoMaterialRequirementRepository(db)
rfqRepo                 := rfqs.NewMongoRFQRepository(db)
rfqCounterRepo          := rfqs.NewMongoRFQCounterRepository(db)
supplierRepo            := suppliers.NewMongoSupplierRepository(db)
supplierOfferingRepo    := suppliers.NewMongoSupplierOfferingRepository(db)
materialPreferenceRepo  := suppliers.NewMongoMaterialSupplierPreferenceRepository(db)
// + EnsureIndexes for each, in the existing indexCtx block

// Acyclic, one pass: materialrequirements -> rfqs. suppliers is independent.
materialRequirementsService := materialrequirements.NewService(
    materialRequirementRepo,
    projectsService,   // ProjectLookup
    workService,       // WorkItemLookup  (new narrow method)
    materialsService,  // MaterialLookup  (new narrow method)
    costsService,      // MaterialCostSource (new narrow method)
    auditService,      // AuditRecorder
)

rfqsService := rfqs.NewService(
    rfqRepo, rfqCounterRepo,
    projectsService,
    composition.NewMaterialRequirementSourceAdapter(materialRequirementsService),
    rfqs.NoExternalIssuanceSource{},
    auditService,
)

suppliersService := suppliers.NewService(
    supplierRepo, supplierOfferingRepo, materialPreferenceRepo,
    materialsService,  // MaterialLookup
    auditService,
)

materialrequirements.RegisterHandlers(authedAPI, materialRequirementsService)
rfqs.RegisterHandlers(authedAPI, rfqsService)
suppliers.RegisterHandlers(authedAPI, suppliersService)
```

`tenanttest.BuildRouter` mirrors this exactly. All three modules register on
the existing `authedAPI` group. **M7 adds no unauthenticated route and no
second `huma.API`.**

---

## 2. MaterialRequirement model

Collection `material_requirements`, owned by `internal/materialrequirements`.

```go
type SourceType string
const (
    SourceTypeCostItem SourceType = "cost_item"
    SourceTypeManual   SourceType = "manual"
    SourceTypeSplit    SourceType = "split"   // a split CHILD
)

type RequirementStatus string
const (
    RequirementStatusDraft    RequirementStatus = "draft"
    RequirementStatusReviewed RequirementStatus = "reviewed"
    RequirementStatusSplit    RequirementStatus = "split"    // terminal
    RequirementStatusArchived RequirementStatus = "archived"  // terminal
)

type SourceSyncState string
const (
    SourceSyncStateClean          SourceSyncState = "clean"
    SourceSyncStateChangeDetected SourceSyncState = "change_detected"
    SourceSyncStateSourceRemoved  SourceSyncState = "source_removed"
)

type SplitState string
const (
    SplitStateNone      SplitState = ""
    SplitStateCreating  SplitState = "creating"
    SplitStateCompleted SplitState = "completed"
)

type MaterialRequirement struct {
    ID        string
    CompanyID string
    ProjectID string

    // OPTIONAL: required when SourceType == cost_item; nilable for manual
    // project-level procurement demand.
    WorkItemID *string

    MaterialID    string
    MaterialName  string  // snapshotted from catalog
    Specification string  // seeded from catalog, contractor-editable

    RequiredQuantity quantity.Quantity  // contractor-controlled value + unit
    CatalogUnit      string             // Material.Unit at snapshot time

    UnitMismatch             bool
    UnitMismatchAcknowledged bool

    RequiredByDate   *time.Time
    ProcurementNotes string  // supplier-visible; may be snapshotted into an RFQ line
    InternalNotes    string  // contractor-only; NEVER snapshotted

    Status RequirementStatus

    // --- source provenance ---
    SourceType SourceType

    // Immutable source aggregation identity, present only when
    // SourceType == cost_item. NOT derived from RequiredQuantity.Unit, which
    // is contractor-editable (§3.4).
    SourceAggregationUnit string
    SourceAggregationKey  string

    // The ACCEPTED source snapshot. Written only at creation and by an
    // explicit discrepancy resolution — NEVER by detection (§5.8).
    SourceCostItemIDs []string           // sorted ascending
    SourceQuantity    *quantity.Quantity
    SourceFingerprint string
    SourceSyncedAt    *time.Time         // when the snapshot was last ACCEPTED

    // Detection output. Writable by generation/re-sync.
    SourceSyncState SourceSyncState
    SourceCheckedAt *time.Time           // when detection last ran

    // --- split provenance ---
    SplitFromRequirementID *string
    SplitGroupID           *string
    SplitSequence          *int
    SplitAt                *time.Time
    SplitByUserID          *string
    SplitState             SplitState
    SplitChildren          []SplitChildRef  // manifest, source side only

    // --- discrepancy provenance (create_separate children) ---
    CreatedFromDiscrepancyRequirementID *string
    CreatedFromSourceFingerprint        *string
    ResolutionOperationID               *string

    // --- RFQ claim (§7). Field owned here; ALL orchestration lives in rfqs.
    ActiveRFQChainID *string  // authoritative
    ActiveRFQNumber  *string  // display metadata
    ActiveRFQLineID  *string  // pre-generated stable line ID
    RFQClaimedAt     *time.Time

    Revision        int64
    CreatedByUserID string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    SchemaVersion   int
}

type SplitChildRef struct {
    RequirementID string  // pre-generated, stable across retries
    Quantity      quantity.Quantity
    Sequence      int
}
```

### 2.1 Status semantics

| Status | Contractor-editable | RFQ-eligible | Splittable | Notes |
|---|---|---|---|---|
| `draft` | yes | **no** | yes | every generated and manual requirement starts here |
| `reviewed` | yes (resets to `draft`, §5.4) | yes, if §8.5 holds | yes | contractor confirmed the demand |
| `split` | **no — terminal, read-only** | no | **no** | superseded by children; retained permanently |
| `archived` | **no — terminal, read-only** | no | **no** | contractor retired the demand |

`split` is distinct from `archived` deliberately: it records *why* the
requirement became inactive.

**`Status == split` or `Status == archived` overrides every source-type rule in
§2.2 and is always contractor-read-only.** Terminal requirements may still have
their **detection-only** fields converged (`SourceSyncState`,
`SourceCheckedAt`) — never their accepted snapshot, quantity, status, notes,
claim or children (§5.8).

### 2.2 Field mutability by source type

This table applies **only while `Status` is `draft` or `reviewed`** and the
requirement is unclaimed. `SourceType = split` below means a **split child**
(§8.2), not a terminal split source.

| | `cost_item` | `manual` | `split` (child) |
|---|---|---|---|
| `MaterialID` | **immutable** | editable | **immutable** |
| `WorkItemID` | **immutable** | editable | **immutable** |
| `RequiredQuantity` value/unit | editable | editable | editable |
| `Specification`, `RequiredByDate`, `ProcurementNotes` | editable | editable | editable |
| `InternalNotes` | editable | editable | editable |
| source aggregation identity | **immutable** | n/a | n/a — resolves through the source |

Changing the material on a generated requirement would invalidate its
aggregation identity and traceability, so it is forbidden; correcting the
CostItems and re-syncing is the supported path. A split child inherits an
identity from its source and may not change it either.

When a **manual** requirement's `MaterialID` changes, the service atomically
refreshes `MaterialName`, `CatalogUnit` and recomputes `UnitMismatch`.
`Specification` is preserved if contractor-authored (non-empty and different
from the previously-seeded catalog value), otherwise re-seeded.

Changing `MaterialID` **or** `RequiredQuantity.Unit` always clears
`UnitMismatchAcknowledged` — an acknowledgement is scoped to the material/unit
combination it was given for.

### 2.3 Claimed-requirement immutability

While `ActiveRFQChainID != nil`, **all contractor-controlled fields are
immutable**, including `InternalNotes`, and **archive, split and discrepancy
resolution are all rejected** with
`409 ErrMaterialRequirementAlreadyClaimed`.

This is what guarantees RFQ-line snapshot stability, and it is why M7 has **no
line-drift subsystem** (§6.3). To change a claimed requirement the contractor
removes the RFQ line, releases the claim, edits and re-reviews, then adds it
again.

---

## 3. Generation from CostItems

### 3.1 Eligibility — owned by `costs`

`costs.Service.VisitEligibleMaterialCostItems` yields a row **only when all**
hold:

1. `Category == material` (`costs.CostCategoryMaterial`)
2. `MaterialID != nil`
3. `WorkItemID != nil`
4. `Quantity != nil` and `Quantity.Value.IsPositive()`
5. `strings.TrimSpace(Quantity.Unit) != ""`

Category `labour` and the other seven categories are never eligible. A
project-level material CostItem with no `WorkItemID` is not eligible for
prefill; the contractor creates that demand manually. A lump-sum CostItem with
no quantity is not eligible.

Rows failing 1–5 are **invisible to `materialrequirements` by construction**.
They are counted in the visitor's **primitive return counters** (§1.2) and
reported separately from contextual skips (§3.8).

### 3.2 Aggregation

Rows are grouped by:

```
ProjectID + WorkItemID + MaterialID + CostItem quantity unit
```

`RequiredQuantity.Value` for a new anchor is the exact `decimal` sum of the
group. No rounding — `decimal` addition is exact, and ADR 0001's
`RoundToMinorUnits` governs Money, not quantities. Quantities are **never
summed across different units**.

### 3.3 Immutable source aggregation identity

`RequiredQuantity.Unit` is contractor-editable and therefore **must not** key
generation. Generated anchors carry:

```
SourceAggregationUnit = the CostItem quantity unit at generation time (immutable)

SourceAggregationKey = hex SHA-256 of the length-prefixed canonical encoding of
    "mrq-source-key-v1"          <-- encoding version, always first
    CompanyID
    ProjectID
    WorkItemID
    MaterialID
    SourceAggregationUnit
```

The version tag serves the same purpose as the fingerprint's (§5.2), and is
more consequential here: this key backs a **unique index**. An unversioned
encoding change could make a new-format key collide with, or fail to match, an
existing anchor — silently creating a duplicate anchor for the same demand, or
orphaning the original.

Generation lookup and uniqueness use this identity, not the current
requirement unit. The anchor remains the single sync anchor for its key **even
after it becomes `split` or `archived`**.

### 3.4 The generation algorithm — union of existing anchors and computed keys

A key with **zero** current CostItems is never *computed*, so iterating
computed keys alone could never discover `source_removed`. Generation therefore
processes the **union**:

```
existingAnchors = all sourceType=cost_item requirements for {companyId, projectId},
                  INCLUDING split and archived anchors
computedKeys    = aggregation keys built from currently eligible CostItems

for key in union(keys(existingAnchors), keys(computedKeys)):
```

| Case | Action |
|---|---|
| **current-only** (computed, no anchor) | create a `draft` anchor (§3.6) → `created` |
| **both** (anchor + computed rows) | compare current fingerprint against the anchor's **accepted** `SourceFingerprint` → `clean` / `change_detected` (§5.3) |
| **existing-only** (anchor, no current rows) | compare against the **empty-aggregate fingerprint** → `clean` (if the accepted snapshot is already the empty aggregate) or `source_removed` |

Every anchor is classified — never skipped. `split` and `archived` anchors are
classified exactly like active ones and reported with their terminal status
attached, so the contractor can see that a discrepancy concerns retired or
superseded demand:

```json
{ "materialRequirementId": "...", "syncState": "change_detected",
  "anchorStatus": "split" }
```

A terminal anchor is **never recreated** — its presence in `existingAnchors` is
what prevents recreation — and its detection fields may be converged while
every contractor-controlled field stays frozen (§5.8). There is no
`split_anchor_exists` or `archived_anchor_exists` skip reason; those
classifications were contradictory (an anchor cannot be both the sync anchor
and skipped) and are removed.

### 3.5 Unit mismatch

For each new anchor, compare `SourceAggregationUnit` against the Material's
catalog unit from `GetMaterialReference`:

- equal after trim + Unicode-aware case-fold → `UnitMismatch = false`
- different → `UnitMismatch = true`, `UnitMismatchAcknowledged = false`

The requirement is created either way and **preserves the CostItem unit**. No
conversion is ever attempted. An unresolved mismatch blocks RFQ eligibility
(§8.5). The contractor resolves it by correcting the source, editing the
requirement, or acknowledging the procurement unit
(`POST .../acknowledge-unit`).

### 3.6 What generation creates

```
Status                = draft
SourceType            = cost_item
SourceSyncState       = clean
SourceCostItemIDs     = sorted contributing IDs        (accepted snapshot)
SourceQuantity        = exact aggregate                (accepted snapshot)
SourceFingerprint     = computed per §5.2              (accepted snapshot)
SourceSyncedAt        = now
SourceCheckedAt       = now
RequiredQuantity      = SourceQuantity (value and unit)
CatalogUnit           = catalog unit
MaterialName          = catalog name
Specification         = catalog specification (seed)
Revision              = 0
```

Generated drafts are unreviewed, RFQ-ineligible and supplier-invisible. **The
review transition is the confirmation gate** — that is why generation creates
directly and M7 has no preview-and-confirm workflow (§20).

### 3.7 Contextual skips and idempotency

A row that reaches `materialrequirements` but cannot become an anchor is
reported as a **contextual skip**:

| Reason | Cause |
|---|---|
| `work_item_cancelled` | `WorkItemProcurementContext` reports cancelled |
| `work_item_not_found` | work item missing or not in this project |
| `material_not_found` | Material missing or not owned by this company |

There is **no `material_inactive` reason** — M7 has no inactive-Material
concept (§0.3 conflict D).

Generation is idempotent around the immutable `SourceAggregationKey`. First
call creates missing draft anchors; later calls with unchanged CostItems create
nothing and report `unchanged`. Concurrent generation is protected by the
unique partial index on `{companyId, sourceAggregationKey}` (§12.1); a
duplicate-key **loser reloads the winning record and reports it as
`unchanged`** rather than failing the command.

### 3.8 Response shape

```json
{
  "createdCount": 3, "unchangedCount": 5, "discrepancyCount": 1,
  "sourceRemovedCount": 1, "contextualSkippedCount": 2,
  "created": [
    { "materialRequirementId": "...", "materialId": "...", "workItemId": "...",
      "quantity": { "value": "25", "unit": "bag" }, "unitMismatch": false }
  ],
  "discrepancies": [
    { "materialRequirementId": "...", "syncState": "change_detected", "anchorStatus": "reviewed" },
    { "materialRequirementId": "...", "syncState": "change_detected", "anchorStatus": "split" }
  ],
  "sourceRemoved": [
    { "materialRequirementId": "...", "anchorStatus": "archived" }
  ],
  "contextualSkipped": [
    { "materialId": "...", "workItemId": "...", "reason": "work_item_cancelled" }
  ],
  "intrinsicallyIneligible": {
    "nonMaterialCategory": 12, "missingMaterialId": 3, "missingWorkItemId": 1,
    "missingOrZeroQuantity": 0, "blankUnit": 0
  }
}
```

`intrinsicallyIneligible` is assembled by `materialrequirements.Service` from
the visitor's five primitive counters (§1.2) and counts CostItems
`materialrequirements` never saw. `contextualSkipped` covers
**only emitted rows** rejected during Work Item / Material validation. The two
are reported separately because they mean different things: the first is
costing data that never qualified, the second is qualifying data blocked by
project context.

---

## 4. Supplier Directory, Offerings and Preference

Owned by `internal/suppliers`. Company-wide, never project-scoped.

### 4.1 Supplier

```go
type Supplier struct {
    ID        string
    CompanyID string

    Name           string  // required, the ONLY mandatory field
    NameNormalized string  // uniqueness key, never client-supplied

    ContactPerson string  // optional
    Email         string  // optional
    Phone         string  // optional
    Address       string  // optional

    MaterialCategories []string  // normalized, deduped, deterministic order
    Notes              string

    Active bool

    Revision        int64
    CreatedByUserID string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    SchemaVersion   int
}
```

**Uniqueness includes inactive records.** `{companyId, nameNormalized}` is
unique and **not partial on `Active`**:

- active supplier with the same normalized name → `409 ErrSupplierNameTaken`
- **inactive** supplier with the same normalized name → `409 ErrSupplierNameTaken`;
  the contractor reactivates or renames the existing record

This prevents retirement followed by accidental duplication. One Supplier
record is one commercial directory entity; distinct branches use
distinguishable names (`ABC Building Materials — Shah Alam` / `— Klang`).

Normalization: trim; collapse consecutive Unicode whitespace to a single space;
Unicode-aware lowercase; reject an empty result
(`422 ErrSupplierNameRequired`).

**Contact details are optional.** M7 is an internal directory and contacts no
one, so incomplete records are permitted. When present, `Email` passes the
existing project email normalization/validation convention and `Phone` is a
trimmed string, never numeric. M8 decides whether a Supplier has a usable
invitation contact — M7 does not require an email for future issuance.

`MaterialCategories` are trimmed, empties removed, duplicates eliminated
case-insensitively, deterministic order preserved.

**Archival is retirement-only.** `Active = false`; no hard delete, **no
cascade**. Offerings and preferences are not modified when a Supplier is
deactivated — they become *effectively* unavailable (§4.2).

`Supplier.Active` is an **M7-owned field on an M7-owned record**, unlike the
Material active state M7 must not invent (§0.3 conflict D).

### 4.2 SupplierOffering

Contractor-managed, informational. Collection `supplier_offerings`.

```go
type SupplierOffering struct {
    ID        string
    CompanyID string

    SupplierID string  // IMMUTABLE after creation
    MaterialID *string // optional catalog link

    ProductName string  // required
    Brand       string
    SKU         string
    Description string
    Category    string
    Unit        string  // required when IndicativePrice is set

    ImageURL   *string
    ProductURL *string

    IndicativePrice     *money.Money
    IndicativePriceAsOf *time.Time
    LastVerifiedAt      *time.Time

    Active bool

    Revision        int64
    CreatedByUserID string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    SchemaVersion   int
}
```

**`IndicativePrice` is not a Supplier Offer.** It is a contractor-recorded
informational preview. It never enters an RFQ, never appears on an RFQ line, is
never copied into any Money field a calculation reads, and is never presented
as a supplier quotation. Formal supplier-submitted offers are a separate M8
model with a separate collection. Structural guarantees:

- `SupplierOffering` lives in `suppliers`; RFQs live in `rfqs`; **neither
  module imports the other** in M7.
- `RFQLine` (§6.2) and `ClaimSnapshot` (§1.3) have **no price field of any
  kind**, so there is no destination an indicative price could reach.
- The M8 handoff projection (§9) exposes no price field.

**Indicative-price invariants:**

```
IndicativePrice == nil  =>  IndicativePriceAsOf MUST be nil
IndicativePrice != nil  =>  amount > 0
                            currency passes foundation Money validation
                            IndicativePriceAsOf REQUIRED
                            Unit non-empty
```

Zero never means "unknown" — `nil` already expresses that. Updating or clearing
the price updates or clears `IndicativePriceAsOf` consistently, and does not
change `LastVerifiedAt` unless the contractor explicitly marks the offering
verified.

The two dates are distinct: `IndicativePriceAsOf` is when the recorded price
applied or was observed; `LastVerifiedAt` is when the contractor last verified
the offering overall.

**Lifecycle:** soft retirement only, no hard delete.

- create → Supplier must be active; a linked Material must **exist and belong
  to the company** (no active check — §0.3 conflict D)
- change `MaterialID` → the newly linked Material must exist and belong to the company
- `SupplierID` immutable after creation
- Supplier becomes inactive → offerings and preferences unmodified

**Effective availability**, computed by the service, not persisted:

```
Offering.Active AND Supplier.Active
```

A Material link does not participate: M7 has no Material state to consult. An
offering whose linked Material is later removed from the catalog remains
readable with its snapshot fields, and `GetMaterialReference` simply reports
`found = false` for enrichment.

### 4.3 MaterialSupplierPreference

```go
type MaterialSupplierPreference struct {
    ID         string
    CompanyID  string
    MaterialID string
    SupplierID string

    Revision      int64
    CreatedAt     time.Time
    UpdatedAt     time.Time
    SchemaVersion int
}
```

Collection `material_supplier_preferences`; unique `{companyId, materialId}` —
at most one preferred Supplier per Material, while one Supplier may be
preferred for many Materials.

`PUT /materials/{materialId}/preferred-supplier` is a **Revision-guarded
create-or-replace**. Validation: the Material **exists and belongs to the
company** via `MaterialLookup`; the Supplier belongs to the same company; the
Supplier **must be active**.

`DELETE` **may physically remove** this relationship — advisory only, and audit
preserves the history. Suppliers and offerings remain retirement-only.

**Supplier archival does not erase the preference.** A preference whose
Supplier is inactive remains historically visible, is reported unavailable, and
is excluded from selectable RFQ suggestions; the contractor chooses a
replacement or clears it.

The preference is **advisory only**: it never automatically adds a Supplier to
an RFQ and never creates an invitation. Suggestion *ranking* is deferred (§20).

### 4.4 URL validation and security

`ImageURL` and `ProductURL` are validated at the service boundary:

1. parse with `net/url.Parse`; reject a parse failure
2. scheme must be exactly `http` or `https` — rejects `javascript:`, `data:`,
   `file:`, `ftp:` and every other scheme
3. host must be non-empty
4. **`u.User != nil` → `ErrInvalidURL`** — embedded credentials are rejected
5. total length ≤ 2048 bytes
6. **store the parsed canonical form** (`u.String()`), not the original text

Rule 4 rejects both forms:

```
https://user:password@example.com/product     -> ErrInvalidURL
https://user@example.com/image                -> ErrInvalidURL
```

Two reasons. A stored URL is rendered in the contractor UI and will be handed to
M8 consumers, so an embedded secret would be persisted in plaintext, written to
logs, and displayed — a credential leak with no compensating benefit. And
`user@host` forms are a well-known host-spoofing vector: a reader scanning
`https://trusted-supplier.com@evil.example/x` sees the trusted name while the
browser resolves `evil.example`.

**The key server-side guarantee: M7 never fetches, resolves, proxies or
inspects either URL.** No HTTP request, no DNS resolution, no metadata
extraction, no image download, no thumbnailing. This is what makes "no web
scraping" structural rather than aspirational, and it means there is no SSRF
surface — no code path dereferences a contractor-supplied URL. Rejecting
private/loopback hosts is therefore unnecessary and is not done.

Frontend integration guidance (presentation requirements, not API-enforced
guarantees): image URLs should be loaded without sending a referrer; product
links opened in a new browsing context must use `noopener` and `noreferrer`.

---

## 5. Re-sync, discrepancy and review state

### 5.1 Persisted state, recomputed proposal

`SourceSyncState` is **persisted**. The discrepancy *proposal* is **recomputed
on read** and never stored as a second document — there is no proposal
collection, so there is no second record to keep consistent.

### 5.2 Fingerprint

Canonical payload, fixed field order, **first field is a version tag**:

```
"mrq-source-fingerprint-v1"      <-- encoding version, always first
ProjectID
WorkItemID
MaterialID
SourceAggregationUnit
row count
for each row, sorted ascending by CostItemID:
    CostItemID
    canonical decimal quantity  (decimal.Decimal.String())
```

Encoding is **length-prefixed** — each field written as its byte length
followed by its bytes. No delimiter concatenation: a unit string could
legitimately contain `|` or a newline, which would make delimiter-based
encoding ambiguous. The encoded bytes are SHA-256 hashed and hex-encoded.

The version tag means a future encoding change produces wholly different
hashes, so a v1 fingerprint can never be silently compared against a v2 one and
read as "clean". Any encoding change is a deliberate, visible re-baseline —
every stored fingerprint mismatches at once and surfaces as a discrepancy for
contractor resolution, rather than a subset comparing equal by accident.

`decimal.String()` normalizes equivalents, so `60` and `60.00` hash alike.

An **empty aggregate** (zero rows) has a well-defined fingerprint — the
aggregation identity plus a row count of zero. §3.4 and §5.3 depend on this.

### 5.3 Sync-state classification

Populated and empty aggregates use the **same comparison**, always against the
**accepted** `SourceFingerprint`:

| Condition | State |
|---|---|
| current fingerprint **==** accepted fingerprint | `clean` |
| differs **and** current row count > 0 | `change_detected` |
| differs **and** current row count == 0 | `source_removed` |

Never set `source_removed` merely because no rows exist. After `keep_current`
accepts an empty aggregate, a later run matches and stays `clean` — otherwise
the same warning would return forever:

```
accepted: c1 + c2      current: no rows        -> source_removed
keep_current -> accepted snapshot = empty aggregate  -> clean
next run: no rows, fingerprints match                -> clean
```

**Source reversion** falls out of the same rule: a `change_detected`
requirement whose CostItems return to the accepted fingerprint automatically
returns to `clean`.

### 5.4 Review state and what resets it

Supplier/procurement-relevant edits reset `reviewed → draft`:
`MaterialID`, `WorkItemID`, `RequiredQuantity.Value`, `RequiredQuantity.Unit`,
`Specification`, `RequiredByDate`, `ProcurementNotes`.

`InternalNotes`-only edits **do not** reset review.

Any edit while claimed → 409 (§2.3). `split` and `archived` reject every
contractor edit. Changing `MaterialID` or `RequiredQuantity.Unit` also clears
`UnitMismatchAcknowledged`.

### 5.5 Proposal read and staleness guard

```
GET /material-requirements/{id}/source-discrepancy
```

```json
{
  "requirementRevision": 7,
  "proposedFingerprint": "abc123",
  "syncState": "change_detected",
  "anchorStatus": "reviewed",
  "accepted": { "quantity": {"value":"33","unit":"m2"}, "costItemIds": ["c1","c2"] },
  "proposed": { "quantity": {"value":"41","unit":"m2"}, "costItemIds": ["c1","c2","c3"] },
  "availableActions": ["update", "merge", "keep_current", "create_separate"]
}
```

A computed proposal can go stale immediately after the response is written, so
M7 does not claim a stale proposal can never be *shown* — it guarantees one can
never be *applied*. Resolution requires both expectations:

```json
{ "expectedRevision": 7, "expectedProposedFingerprint": "abc123", "action": "merge" }
```

The service **recomputes the current source before applying anything**. If the
recomputed fingerprint differs from `expectedProposedFingerprint`, it applies
nothing and returns `409 ErrMaterialRequirementDiscrepancyChanged` with the
newly computed proposal. `expectedRevision` is enforced by the same conditional
update.

### 5.6 Resolution actions — availability by anchor status

`availableActions` is computed per requirement. **`update` and `merge` mutate
`RequiredQuantity`, so they are impossible on a terminal anchor**:

| Anchor status | update | merge | keep_current | create_separate | archive |
|---|---|---|---|---|---|
| `draft` / `reviewed` | yes | yes, when units permit | yes | yes, positive delta only | yes |
| `split` | **no** | **no** | yes | yes, positive delta only | **no** — already terminal |
| `archived` | **no** | **no** | yes | yes, positive delta only | **no** — already archived |
| any, while **claimed** | no | no | no | no | no — all 409 (§2.3) |

A **negative** change against a `split` or `archived` anchor offers
`keep_current` only. M7 **never automatically mutates split children** to
absorb a source reduction; reducing already-batched demand requires the
contractor to review the children and edit or archive them individually. The
discrepancy is reported at the split-group level (via the source anchor) and
resolving it with `keep_current` accepts the new source reality without
touching any child.

**`update`**
```
RequiredQuantity         = proposed SourceQuantity (value AND proposed source unit)
accepted snapshot        = proposed (IDs, quantity, fingerprint, SourceSyncedAt)
SourceSyncState          = clean
Status                   = draft
UnitMismatch             = recomputed against catalog unit
UnitMismatchAcknowledged = false
```

**`merge`** — precise formula, and the reason detection must not overwrite the
accepted snapshot (§5.8):
```
new RequiredQuantity.Value =
    current RequiredQuantity.Value + (proposed SourceQuantity - ACCEPTED SourceQuantity)
```
```
accepted 33, contractor 35, proposed 41  ->  delta 8  ->  merged 43
```
Available **only when** the current required unit equals the source aggregation
unit, accepted and proposed source units match, and the result is > 0. When the
contractor has manually converted to another unit, M7 cannot safely add a
source-unit delta, so `merge` is withheld. Snapshot refreshes to proposed;
`SourceSyncState = clean`; `Status = draft`.

**`keep_current`**
```
RequiredQuantity   unchanged
accepted snapshot  = proposed, INCLUDING an empty aggregate
SourceSyncState    = clean
Status             unchanged  (a deliberate no-op is not a procurement edit)
```
Valid on every non-claimed anchor status, including terminal ones — it changes
no contractor field, only the accepted snapshot and detection state.

**`create_separate`** — available **only when**
`proposed SourceQuantity - accepted SourceQuantity > 0`. A zero delta from a
membership-only change creates nothing; a negative delta cannot create a
negative requirement. The new requirement:
```
SourceType                          = manual
Status                              = draft
RequiredQuantity                    = the positive source delta,
                                      IN THE PROPOSED SOURCE UNIT
CreatedFromDiscrepancyRequirementID = original requirement ID
CreatedFromSourceFingerprint        = proposed fingerprint
ResolutionOperationID               = caller-supplied operation ID
```

**Inherited verbatim from the source requirement:**

```
CompanyID
ProjectID
WorkItemID        (including nil, if the source is a manual project-level requirement)
MaterialID
MaterialName
CatalogUnit
Specification
RequiredByDate    where the source has one; nil otherwise
```

`UnitMismatch` is **recomputed** for the child against `CatalogUnit` rather than
inherited, because the child's unit is the proposed source unit, which may differ
from the source requirement's contractor-edited unit.
`UnitMismatchAcknowledged` starts false — an acknowledgement never transfers to
a new record.

Inheriting `ProjectID` matters for §7.2: a child that inherited the wrong
Project could never be claimed by any RFQ in its own Project.

It carries **no** `SourceCostItemIDs`, no `SourceAggregationKey` and no
`SourceAggregationUnit` — it must not pretend to independently own the same
CostItem aggregate. `SourceSyncState` is `clean` permanently (§8.5).

For `source_removed` on an active anchor: `keep_current` and `archive` only.

### 5.7 `create_separate` idempotency and failure window

The request must carry `ResolutionOperationID`, protected by a unique partial
index on `{companyId, resolutionOperationId}` (§12.1).

```
1. recompute source; verify expectedProposedFingerprint  (else 409, apply nothing)
2. create the delta requirement with ResolutionOperationID
3. Revision-guardedly refresh the source anchor's accepted snapshot -> clean
```

**Retry is verified, never blind.** On
`ErrResolutionOperationAlreadyApplied`, the service **loads the existing record
and confirms it belongs to the same logical operation** before reusing it. All
of these must match:

```
CompanyID
CreatedFromDiscrepancyRequirementID
CreatedFromSourceFingerprint
MaterialID
WorkItemID              (including both nil)
RequiredQuantity.Value  (decimal.Equal)
RequiredQuantity.Unit
```

| Outcome | Action |
|---|---|
| **all match** | reuse the existing child; retry step 3 (refresh the source snapshot) |
| **any differ** | `409 ErrResolutionOperationConflict` — **modify neither requirement**, create nothing |

Blind reuse would be a correctness hole: a client that reused one operation ID
against a *different* source requirement would silently receive the first
requirement's child as though its own resolution had been applied. The source
anchor it actually asked about would remain unresolved, and the two unrelated
discrepancies would appear cross-linked through a shared child. Verification
makes an operation ID identify one specific resolution of one specific
discrepancy, not merely "some resolution that already happened".

**Honest failure window:** a crash between 2 and 3 leaves a `draft` delta child
plus an unresolved discrepancy on the anchor. It **cannot** create duplicate
children — the unique index forbids it. The orphan is a `draft`, therefore not
RFQ-eligible, and the unresolved discrepancy keeps the anchor visible for
action. Bounded and recoverable; not described as harmless, because duplicate
procurement demand would become operational if duplication were possible —
which is exactly why the operation ID is mandatory.

### 5.8 Who may write which source fields

This is the rule that makes `merge` correct. **Detection must never overwrite
the accepted snapshot**, because `merge` computes
`proposed − accepted`; if detection had already advanced `SourceQuantity` to
the proposed value, the delta would collapse to zero and the contractor's
adjustment would be silently discarded.

| Field | Detection / generation | Explicit discrepancy resolution |
|---|---|---|
| `SourceSyncState` | **may write** | may write |
| `SourceCheckedAt` | **may write** | may write |
| `SourceFingerprint` | **must not write** | may write |
| `SourceCostItemIDs` | **must not write** | may write |
| `SourceQuantity` | **must not write** | may write |
| `SourceSyncedAt` | **must not write** | may write |
| `RequiredQuantity`, `Status`, notes, claim, split children | **must not write** | only as §5.6 specifies |

The single exception is **anchor creation** (§3.6), where generation writes the
initial accepted snapshot because there is no prior accepted state to preserve.

For claimed and terminal requirements, detection may still converge
`SourceSyncState` and `SourceCheckedAt` — nothing else. Without this
distinction, persisting `change_detected` on a claimed or split requirement
would contradict §2.3's immutability claim.

---

## 6. Supplier-neutral RFQ

Owned by `internal/rfqs`.

### 6.1 Identity and model

```
RFQ.ID        // stable RFQ CHAIN identity == RFQChainID. M8 issued versions
              // reference THIS, never RFQNumber.
RFQNumber     // "RFQ-000124" — tenant-scoped display/business identifier
Revision      // optimistic concurrency for draft mutation
SchemaVersion // document schema version
```

No `Version` field in M7; externally-issued versioning is M8's.

```go
type RFQStatus string
const (
    RFQStatusDraft RFQStatus = "draft"
    RFQStatusReady RFQStatus = "ready"
)

type RFQ struct {
    ID        string  // == RFQChainID
    CompanyID string
    ProjectID string
    RFQNumber string
    Status    RFQStatus
    Revision  int64

    Title string
    Lines []RFQLine

    DeliveryAddress      string
    RequiredByDate       *time.Time
    ResponseDeadline     *time.Time
    SupplierInstructions string  // supplier-visible
    InternalNotes        string  // contractor-only, NEVER supplier-visible

    CreatedByUserID string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    ReadyAt         *time.Time
    ReopenedAt      *time.Time
    SchemaVersion   int
}
```

`InternalNotes` carries the same privacy boundary M5 established for
`Quotation.Notes`: present in the aggregate, structurally absent from every
projection M8 reads (§9).

**Numbering.** Collection `rfq_counters`, one document per company, unique
`{companyId}`, allocated by `FindOneAndUpdate` + `$inc` + `upsert:true`
returning the post-increment value — atomic by construction, no read-then-write
race, no retry loop. Format `RFQ-%06d`. Copied from
`MongoQuotationCounterRepository` (`quotations/repository_mongo.go:371-385`).

**No draft-uniqueness constraint.** `quotations` has a partial-unique "one
draft per chain" index because a quotation chain is one commercial document
evolving through versions. Each M7 RFQ is a *new* chain, and a Project
legitimately holds several concurrent drafts — precisely how split batches reach
different suppliers. The one-chain invariant lives on the requirement (§7), not
the RFQ.

### 6.2 RFQ line — snapshot

```go
type RFQLine struct {
    ID string  // == the requirement's pre-generated ActiveRFQLineID

    SourceMaterialRequirementID string  // traceability

    MaterialID       string
    MaterialName     string
    Specification    string
    Quantity         quantity.Quantity
    RequiredByDate   *time.Time
    ProcurementNotes string

    SnapshotAt time.Time
    SortOrder  int
}
```

**No price, no cost, no margin, no `InternalNotes`.** An RFQ line asks a
supplier to quote; it never carries what the contractor thinks it costs — the
same allowlist discipline as `QuotationLine`
(`quotations/quotation.go:95-102`).

Every field is copied from the `ClaimSnapshot` returned by the successful claim
(§7.2 step 4), so the line reflects exactly the state the claim validated.

There is **no `SnapshotFingerprint`**, because there is no drift subsystem to
consume it (§6.3).

### 6.3 Why there is no line-drift subsystem

§2.3 freezes **every** contractor-controlled requirement field while a claim
exists, and rejects archive, split and discrepancy resolution outright. The
fields snapshotted into a line are therefore **incapable of changing while the
line and claim validly coexist** — the claim itself is the stability guarantee.

A drift-detection subsystem would monitor for a state the claim makes
unreachable. It is removed: no `GET /rfqs/{id}/line-drift`, no drift resolution
endpoint, no `RecordRFQLineDriftResolved`, no `ErrRFQLineDriftChanged`, no
`SnapshotFingerprint`, and no drift acceptance tests.

To change a snapshotted requirement, the contractor follows the explicit path:

```
remove the RFQ line  ->  claim released  ->  edit and re-review  ->  add again
```

That produces a fresh snapshot by construction.

A secondary reason to remove it: the `keep_snapshot` action was incoherent. It
refreshed `SnapshotFingerprint` to the *current requirement* while leaving the
snapshotted fields untouched, so the field named "snapshot fingerprint" would
no longer fingerprint the snapshot. Making it sound would have required a
separate `AcceptedSourceFingerprint` on every line — machinery for a state that
cannot arise.

**Consequence for a retired requirement.** A claimed requirement cannot be
split or archived, so a line's source cannot become terminal underneath it. The
only path is: remove the line (releasing the claim), then archive or split. The
line is already gone before the source can retire, so `source_retired` is
likewise unreachable and is not modelled.

### 6.4 Lifecycle

```
draft --mark-ready--> ready --reopen--> draft
```

**`mark ready`** applies under `{_id, companyId, status:draft, revision}` and
validates:

1. at least one line
2. for **every** line, `ClaimedRequirementIsReadyForRFQ(companyID,
   requirementID, rfqChainID, lineID)` returns true
3. `DeliveryAddress` non-empty — a supplier cannot quote delivery to nowhere

Check 2 is a **single** call per line that subsumes what were previously two
separate checks. It verifies `status == reviewed`, positive quantity, unit
mismatch absent or acknowledged, `sourceSyncState == clean`, **and** that the
claim names exactly this chain and this line.

It deliberately does **not** apply the unclaimed §8.5 predicate. That predicate
requires `ActiveRFQChainID == nil`, which is false for every requirement already
on a line — applying it here would make **every non-empty RFQ impossible to
mark ready**. The unclaimed predicate belongs inside `ClaimForRFQ` (§7.2) and
nowhere else.

Sets `Status = ready`, `ReadyAt = now`, **and increments `Revision`**.

**`reopen`** requires
`IssuanceStatusSource.RFQChainIssued(ctx, companyID, rfqChainID) == false`:

| Issuance source result | Outcome |
|---|---|
| `false` | reopen proceeds |
| `true` | `409 ErrRFQAlreadyIssued`; RFQ **remains `ready`** |
| error | `503 ErrIssuanceStatusUnavailable`; RFQ **remains `ready`** (fails closed) |

M7 wires `NoExternalIssuanceSource{}` (always false), so reopen always succeeds
in M7; M8 swaps in the real adapter with no change to service logic. Sets
`Status = draft`, `ReopenedAt = now`, **and increments `Revision`**, under
`{_id, companyId, status:ready, revision}`.

**Both transitions increment `Revision`** — deliberately unlike
`quotations.Finalize`, which does not
(`quotations/repository_mongo.go:277-283, 336-339`). A Quotation has no
transition back to draft, so freezing its revision is safe. An RFQ cycles
`draft → ready → draft`, which creates an ABA hazard if the revision is
unchanged:

```
client reads draft, revision 4
contractor marks ready        (revision still 4)
contractor reopens to draft   (revision still 4)
the stale pre-ready client's conditional update MATCHES and mutates
  an RFQ whose scope was reviewed and locked in between
```

Incrementing on both transitions closes it: the stale client receives
`ErrRevisionMismatch` and must re-read.

After M8 issues an RFQ, any supplier-visible change requires a new immutable
RFQ version. **That issued/versioned lifecycle is not implemented in M7** — no
`issued` status, no version field, no code path that could create one.

**Claims survive both transitions.** Changing RFQ status never releases a
requirement; release happens only via explicit line removal or claim release
(§7.4).

### 6.5 Readiness is a point-in-time validation

`mark ready` validates the **already-claimed snapshot at that moment**. It is
not a standing guarantee, and M7 has no mechanism that revisits it.

After an RFQ becomes `ready`, later CostItem changes may move a source
requirement's detection state to `change_detected` (§5.3). When that happens:

- the RFQ line is **not** mutated — the snapshot is unchanged
- the RFQ is **not** automatically demoted from `ready` to `draft`
- the supplier-visible scope is **never** silently replaced

There is deliberately **no automatic `ready → draft` transition** in M7. An
automatic demotion would let a background source change retract a scope the
contractor had reviewed and locked, which is exactly the silent mutation the
snapshot model exists to prevent.

To incorporate new source demand before issuance, the contractor takes the
explicit path:

```
reopen the RFQ  ->  remove the relevant line (claim released)
                ->  resolve the discrepancy / edit the requirement
                ->  re-review it
                ->  add it again  (fresh snapshot)
                ->  mark ready again
```

Note that the source requirement's detection state can change **only** in ways
that do not touch the line: while claimed, the requirement is fully immutable
(§2.3) and detection may write only `SourceSyncState` and `SourceCheckedAt`
(§5.8). So a `ready` RFQ's lines are stable by construction, and the
contractor's re-review path is the sole route to changing them.

---

## 7. The one-active-RFQ-chain invariant

### 7.1 Serialization point and ownership split

The **MaterialRequirement is the serialization point** — the authoritative
resource whose exclusivity is protected. It carries `Revision`, makes claim
status directly visible in the project materials view, needs no extra
collection, and follows M6's coordinator-authoritative pattern
(`access/grant.go:74-79`).

Ownership is split cleanly:

- `materialrequirements` **owns the claim fields** and the three primitive
  operations on them: `ClaimForRFQ`, `ReleaseClaim`, `ReadClaim` (§1.3). It
  performs the conditional writes and nothing else.
- `rfqs` **owns all orchestration**: sequencing, compensation, the retry
  matrix, and reconciliation including `retry_line`.

`materialrequirements` never inspects an RFQ line, never learns whether a line
exists, and never imports `rfqs`. `retry_line` lives in `rfqs`, because only
`rfqs` can read its own lines.

`ActiveRFQChainID` is authoritative; `ActiveRFQNumber` is display metadata;
`ActiveRFQLineID` is a pre-generated stable line ID. The claim attaches to the
**stable chain ID**, so when M8 introduces issued versions the claim does not
move between versions.

No Mongo transaction and no separate claim collection. A separate claims
collection would only be justified if several unrelated resource types later
needed a generalized allocation mechanism — M7 introduces no such engine.

### 7.2 Add line — fail-closed sequence, driven by `rfqs`

```
POST /rfqs/{rfqId}/lines
{ "materialRequirementId": "...", "expectedRequirementRevision": 4,
  "expectedRFQRevision": 2 }
```

```
1. rfqs loads and validates its own draft RFQ (status == draft, companyId),
   and reads its ProjectID.
2. rfqs pre-generates a stable RFQLineID.
3. rfqs calls ClaimForRFQ(ctx, companyID, rfq.ProjectID, ...).
   materialrequirements performs ONE conditional update:
     WHERE _id = requirementID
       AND companyId = companyID
       AND projectId = <the RFQ's projectID>      <-- enforced atomically
       AND revision  = expectedRequirementRevision
       AND activeRfqChainId = null
       AND <the §8.5 eligibility predicate holds>
     SET   activeRfqChainId, activeRfqNumber, activeRfqLineId,
           rfqClaimedAt = now, revision = revision + 1
   0 matched -> re-read to distinguish not-found / wrong-project /
                not-eligible / already-claimed
4. ClaimForRFQ returns the ClaimSnapshot built from the SAME document the
   claim validated, so the append uses exactly that state.
5. rfqs appends the line, conditional on
   {_id, companyId, status:draft, revision:expectedRFQRevision}.
6. On append failure: compensate or leave for reconciliation (§7.5).
```

`projectId` is part of the **same atomic filter** as `companyId`, `revision`,
eligibility and `activeRfqChainId = null` — not a separate pre-check that could
be raced. A requirement belonging to a different Project of the same company
matches zero documents and returns `rfqs`' own
`ErrMaterialRequirementNotFound`, **creating no claim**. Cross-project
contamination of an RFQ is therefore structurally impossible, not merely
validated against.

The first conditional claim wins; a competing RFQ receives
`409 ErrMaterialRequirementAlreadyClaimed`. Every cross-collection operation is
bounded to one requirement and one line.

### 7.3 Retry matrix — evaluated by `rfqs`

| Situation | Behaviour |
|---|---|
| unclaimed | claim and append |
| claimed by **this** chain **and** the line exists | return the existing line successfully |
| claimed by **this** chain but the line is **missing** | append using the `ActiveRFQLineID` from `ReadClaim`, rebuilding the line from `ReadClaimSnapshot(companyID, requirementID, rfqChainID, lineID)` — repairs an interrupted operation |
| claimed by **another** chain | `409 ErrMaterialRequirementAlreadyClaimed` |

`rfqs` distinguishes these by combining `ReadClaim` (for a known requirement
ID) with a scan of its own lines. Storing `ActiveRFQLineID` makes retries
deterministic and prevents duplicate lines after an uncertain response.

**Rebuilding the missing line.** The repair case cannot re-claim — the
requirement is already claimed, so `ClaimForRFQ` would correctly refuse — and
`ClaimForRFQ`'s return value is the only other place a `ClaimSnapshot` comes
from. The line is therefore rebuilt through
`ReadClaimSnapshot(companyID, requirementID, rfqChainID, lineID)` (§1.3), which
returns the immutable snapshot the existing claim already validated. Fabricating
the supplier-visible fields instead would ask a supplier to quote content no
requirement backs.

**Remove-line retry.** A retry of `DELETE /rfqs/{id}/lines/{lineId}` arriving
after the line is already gone has no line to read the requirement ID from.
`rfqs` resolves it via `ListClaimsForRFQChain` and matches on `LineID`,
recovering the requirement ID and its current Revision, then completes the
release (§7.4 step 2). Without that capability the retry could not identify
which requirement to release.

### 7.4 Remove line — opposite order

```
1. rfqs removes the line  (conditional on {_id, companyId, status:draft, revision})
2. rfqs calls ReleaseClaim(...), which requires ALL of:
     companyId, requirementID, the exact rfqChainID, the exact lineID,
     revision = expectedRevision
```

Line removal precedes claim release so the system **never makes a requirement
available to another RFQ while its previous line may still exist**. If step 2
fails the requirement stays temporarily blocked — fail-closed. A retry detects
the line is already absent and completes the release.

### 7.5 Compensation and reconciliation — `rfqs`-owned

If the claim succeeds but the append fails, `rfqs`:

1. attempts `ReleaseClaim` immediately using the **new** Revision from the claim
2. compensation succeeds → returns the original append error
3. compensation fails → **leaves the claim in place**

The orphaned claim is **fail-closed**: the requirement cannot enter another RFQ.
It is never silently cleared merely because one read did not find the line.

Reconciliation endpoints live on the **RFQ**, not the requirement:

```
GET  /rfqs/{id}/claim-reconciliation
POST /rfqs/{id}/claim-reconciliation
     { "materialRequirementId": "...", "expectedRequirementRevision": N,
       "action": "retry_line" | "release" }
```

**How orphans are discovered.** `GET` calls
`ListClaimsForRFQChain(companyID, rfqChainID)` and joins the result against its
own lines:

| Claim on this chain | Matching line | Classification |
|---|---|---|
| present | present, same `LineID` | `consistent` |
| present | **absent** | **`orphaned_claim`** — actionable |
| absent | present | `orphaned_line` — a line whose requirement no longer claims this chain |

`ReadClaim` alone could not find an orphan: it answers only for a requirement ID
the caller already has, and an orphaned claim is by definition one whose
requirement `rfqs` cannot name from its own lines. `ListClaimsForRFQChain`
inverts the lookup, which is why it exists.

- `retry_line` — append the missing line using the `LineID` from the claim,
  rebuilding its supplier-visible fields from
  `ReadClaimSnapshot(companyID, requirementID, rfqChainID, claim.LineID)`
  (§1.3). Reading under the **claim's own** chain and line means `retry_line`
  can only ever rebuild the line that claim describes.
- `release` — call `ReleaseClaim` under Revision guard

Both actions are driven by data from `ListClaimsForRFQChain`, so both are
deterministic: the chain ID, line ID and expected Revision all come from the
authoritative claim rather than from client input.

The UI surfaces this as "RFQ claim requires reconciliation". There is **no
reconciliation endpoint under `/material-requirements`** — that would require
`materialrequirements` to reason about RFQ lines.

### 7.7 RFQ deletion requires zero outstanding claims

An RFQ is deletable **only when all three** hold:

```
Status == draft
Lines is empty
ListClaimsForRFQChain(companyID, rfqChainID) returns zero claims
```

The third condition is not redundant. An orphaned claim (§7.5) is precisely a
claim with **no** line, so an RFQ can be line-empty while still holding a
requirement hostage. Deleting it would strand that requirement permanently:
`ActiveRFQChainID` would name a chain that no longer exists, no RFQ would remain
to reconcile through, and the requirement could never be claimed again or
released — an unrecoverable state reachable by a single delete.

Attempting deletion with an outstanding claim returns
`409 ErrRFQHasOutstandingClaims`. The contractor reconciles first (`release`),
then deletes.

### 7.6 Batching requires a split

A requirement cannot enter two active chains to reach different suppliers — one
RFQ will later be sent to multiple Suppliers by M8. Dividing procurement into
batches requires splitting first:

```
100 bag cement  ->  split 60 + 40  ->  each child may enter a different chain
```

---

## 8. Split, and RFQ eligibility

### 8.1 Split invariants

```
source: 100 bag cement, status = reviewed
        |
        v  split [60, 40]
source: 100 bag cement, status = split      (terminal, read-only)
child A: 60 bag,  SourceType = split, SplitFromRequirementID = source
child B: 40 bag,  SourceType = split, SplitFromRequirementID = source
```

- `sum(child quantities) == source RequiredQuantity.Value` **exactly**
  (`decimal.Equal`, no tolerance)
- every child retains the source's `CompanyID`, `ProjectID`, `WorkItemID`,
  `MaterialID` and procurement unit
- **no automatic unit conversion** — all children share the source's unit
- at least two children; every child quantity strictly positive
- children may differ in `RequiredByDate`, notes and batch details afterwards

**Children start as `draft`**, never automatically `reviewed` — the contractor
must confirm the resulting batches.

### 8.2 Child provenance — distinct from the source

Children carry `SourceType = split` and **do not copy** the source's CostItem
IDs, source quantity, fingerprint or aggregation identity. Copying them would
make each child appear independently backed by the full original aggregate.
Provenance resolves through the source:

```
CostItems  ->  source requirement  ->  split children
```

Children inherit `MaterialName`, `Specification`, `CatalogUnit`,
`UnitMismatch` and `UnitMismatchAcknowledged` (descriptive snapshots, not
source claims), plus `SplitGroupID` and `SplitSequence`. `SplitGroupID` loads
all siblings without repeated parent lookups.

CostItem re-sync continues against the **source anchor** and surfaces
discrepancies at the split-group level (§3.4, §5.6).

### 8.3 Split preconditions

Rejected when the source is:

- `split` or `archived` → `409 ErrRequirementTerminal`
- **`SourceType == split`** → `422 ErrRecursiveSplitNotSupported`. §20 defers
  multi-level splitting, so a split **child** may not itself be split. Without
  this the preconditions would have permitted exactly what §20 defers.
- **claimed** (`ActiveRFQChainID != nil`) → `409 ErrMaterialRequirementAlreadyClaimed`.
  The contractor must remove the line and release the claim first.
- `SplitState == creating` → `409 ErrSplitInProgress`
- not owned by the authenticated company → 404
- Revision mismatch → 409

### 8.4 Split write order and idempotent recovery

```
1. Revision-guardedly move the source to status = split, atomically requiring
     _id = sourceID AND companyId = companyID
     AND revision = expectedRevision
     AND status IN (draft, reviewed)
     AND sourceType != "split"
     AND activeRfqChainId = null
   and SETTING, in the same update:
     status = split, splitAt = now, splitByUserId = actor,
     splitState = "creating",
     splitChildren = [ {pre-generated ID, quantity, sequence}, ... ],
     revision = revision + 1
2. Create each child using its pre-generated stable ID (idempotent inserts)
3. Revision-guardedly set splitState = "completed"
4. Record the audit event
```

The **manifest is written in step 1**, so a retry completes exactly the same
children without duplicating them. If child creation fails after the source
became `split`, the source is **not** automatically restored; the operation
stays `splitState = creating` and an idempotent reconciliation path creates the
missing children from the manifest:

```
POST /material-requirements/{id}/split/reconcile   { "expectedRevision": N }
```

**Honest failure window:** between steps 1 and 3 the source is terminal while
some children may not exist. Demand is temporarily under-represented and
visibly incomplete (`splitState = creating`). No duplicate child can be created
(stable IDs + unique `_id`), and no partially-split source can be claimed or
edited.

### 8.5 RFQ eligibility predicate

A requirement may be claimed **only when all** hold:

```
Status == reviewed
AND ActiveRFQChainID == nil
AND RequiredQuantity.Value > 0
AND (UnitMismatch == false OR UnitMismatchAcknowledged == true)
AND SourceSyncState == clean
```

The `SourceSyncState == clean` term is essential: without it a `reviewed`
requirement could enter an RFQ after its CostItems changed but before the
contractor chose a resolution. `keep_current` refreshes the accepted snapshot
and returns the requirement to `clean` without changing `RequiredQuantity`, so
a deliberate adjustment becomes eligible immediately.

Manual requirements and split children have `SourceSyncState = clean`
permanently — neither has a CostItem aggregate of its own to drift from.

This predicate is enforced **inside the claim's conditional update** (§7.2
step 3), so it cannot be raced. It applies **only** to an unclaimed requirement
being claimed.

It is deliberately **not** reused at mark-ready time. Its
`ActiveRFQChainID == nil` term is false for every requirement already on a
line, so reapplying it there would reject every non-empty RFQ. Mark-ready uses
`ClaimedRequirementIsReadyForRFQ` instead (§1.3, §6.4), which checks the same
business conditions but requires the claim to name **this** chain and line
rather than requiring no claim at all.

---

## 9. M8 handoff boundary

`internal/rfqs` **exposes** exactly three read-only capabilities:

```go
// package rfqs

type RFQLineSnapshot struct {
    LineID           string
    MaterialName     string
    Specification    string
    QuantityValue    string  // canonical decimal string
    QuantityUnit     string
    RequiredByDate   *time.Time
    ProcurementNotes string
    SortOrder        int
}

func (s *Service) GetReadyRFQSnapshot(ctx context.Context, companyID, rfqChainID string) (
    rfqNumber string, projectID string,
    deliveryAddress string, requiredBy *time.Time, responseDeadline *time.Time,
    supplierInstructions string, lines []RFQLineSnapshot,
    found bool, err error)

func (s *Service) RFQChainIsReady(ctx context.Context, companyID, rfqChainID string) (bool, error)

func (s *Service) ReopenPermitted(ctx context.Context, companyID, rfqChainID string) (bool, error)
```

- `InternalNotes` is **structurally absent** from the signature, so it cannot
  leak to a supplier-facing consumer even by mistake — the same
  allowlist-by-signature discipline as `audit.AuditRecorder`.
- `RFQLineSnapshot` has **no price field**, so an indicative offering price has
  no destination.
- `GetReadyRFQSnapshot` returns `found = false` for a `draft` RFQ: only `ready`
  RFQs are handoff-visible.
- No whole `RFQ` aggregate crosses the boundary. M8 consumes these through a
  `composition` adapter (M8's work), since `RFQLineSnapshot` is an
  `rfqs`-owned type.

Direction: `rfqs` consumes `IssuanceStatusSource` (§1.3); M8 consumes these
three methods. M8 **never writes into `rfqs`, `rfq_counters` or
`material_requirements`**.

### 9.1 M8 compatibility requirements — documented, NOT implemented

Recorded so M8 need not re-derive them. **No M7 code implements any of this**,
and M7 contains no link, token, invitation, grant, offer or version code:

- one stable supplier-specific invitation link per Supplier Invitation per RFQ
  chain
- issuing RFQ V2 updates the invitation to point at V2
- RFQ V1 remains historically immutable
- Supplier Offers stay tied to the exact RFQ version they answered
- a new token is required only for rotation/revocation/security reasons — never
  for an ordinary commercial revision

`access.ResourceTypeQuotation`'s comment already anticipates `"rfq"` reusing
the same grant collection and index shape without a migration
(`access/grant.go:5-8`).

---

## 10. Optimistic concurrency

Every mutable M7 aggregate carries `Revision int64` (phase1.md §54) and is
mutated **only** through a conditional update, following
`quotations.conditionalUpdate` (`quotations/repository_mongo.go:284-307`):

| Aggregate | Filter |
|---|---|
| requirement (contractor edit) | `{_id, companyId, revision, status IN (draft, reviewed), activeRfqChainId: null}` |
| requirement (claim) | `{_id, companyId, revision, activeRfqChainId: null, <§8.5 predicate>}` |
| requirement (release) | `{_id, companyId, revision, activeRfqChainId, activeRfqLineId}` |
| requirement (split) | `{_id, companyId, revision, status IN (draft, reviewed), sourceType != "split", activeRfqChainId: null}` |
| requirement (resolution) | `{_id, companyId, revision, activeRfqChainId: null}` + per-action status guard (§5.6) |
| requirement (detection converge) | `{_id, companyId, revision}` — `SourceSyncState`/`SourceCheckedAt` only (§5.8) |
| RFQ (draft mutation) | `{_id, companyId, status: "draft", revision}` |
| RFQ (mark ready) | `{_id, companyId, status: "draft", revision}`, Revision **incremented** (§6.4 ABA hazard) |
| RFQ (reopen) | `{_id, companyId, status: "ready", revision}`, Revision **incremented** |
| `Supplier`, `SupplierOffering`, `MaterialSupplierPreference` | `{_id, companyId, revision}` |

On `MatchedCount == 0`, re-read to distinguish not-found (404) from a state or
revision conflict (409). `Revision` is never conflated with `SchemaVersion`,
`RFQNumber` or any M8 version.

---

## 11. Idempotency

| Operation | Mechanism |
|---|---|
| generation | idempotent around the immutable `SourceAggregationKey` + unique partial index; duplicate-key loser reloads the winner and reports `unchanged` (§3.7) |
| add RFQ line | pre-generated `ActiveRFQLineID` stored on the claim; retry matrix §7.3 |
| remove RFQ line | line-then-claim order; retry detects the absent line and completes release (§7.4) |
| split | manifest of pre-generated child IDs written with the status transition; retry recreates exactly those children (§8.4) |
| `create_separate` | mandatory `ResolutionOperationID` + unique partial index (§5.7) |
| other resolutions | single conditional update; a repeat fails the fingerprint/revision guard with 409 and applies nothing |
| RFQ creation | **not** idempotent — a retry allocates a new `RFQNumber` and creates a second empty draft. Accepted: an empty draft is harmless, carries no claim, and is deletable (§17). Documented rather than papered over. |
| mark ready / reopen | conditional on current status **and** `expectedRevision`; both increment `Revision`, so a repeat fails on status and a stale pre-cycle caller fails on revision (§6.4) |
| delete RFQ | conditional on `status:draft` + `expectedRevision` + zero lines + zero outstanding claims (§7.7); a repeat finds no document and returns 404 |

phase1.md §53's idempotency-key examples concern RFQ *submission* and
*Invitation creation* — both M8. M7 needs no HTTP idempotency-key header.

---

## 12. MongoDB collections and indexes

Every index carries an **explicit name**; every tenant-scoped index leads with
`companyId`.

### 12.1 `material_requirements` (owned by `materialrequirements`)

| Name | Keys | Properties |
|---|---|---|
| `idx_material_requirements_company` | `{companyId:1}` | |
| `idx_material_requirements_company_project` | `{companyId:1, projectId:1}` | |
| `idx_material_requirements_company_project_status` | `{companyId:1, projectId:1, status:1}` | |
| `idx_material_requirements_company_project_sourcetype` | `{companyId:1, projectId:1, sourceType:1}` | serves §3.4's existing-anchor load |
| `idx_material_requirements_company_workitem` | `{companyId:1, workItemId:1}` | |
| `idx_material_requirements_company_material` | `{companyId:1, materialId:1}` | |
| `uq_material_requirements_source_key` | `{companyId:1, sourceAggregationKey:1}` | **unique**, partial: `{sourceType: "cost_item"}` |
| `uq_material_requirements_resolution_op` | `{companyId:1, resolutionOperationId:1}` | **unique**, partial: `{resolutionOperationId: {$type: "string"}}` |
| `idx_material_requirements_company_rfq_chain` | `{companyId:1, activeRfqChainId:1}` | |
| `idx_material_requirements_company_split_group` | `{companyId:1, splitGroupId:1}` | |

`uq_material_requirements_source_key` makes generation race-safe. It keys on
the **immutable** `sourceAggregationKey`, never the contractor-editable
`RequiredQuantity.Unit`.

### 12.2 `rfqs`, `rfq_counters` (owned by `rfqs`)

| Name | Keys | Properties |
|---|---|---|
| `idx_rfqs_company` | `{companyId:1}` | |
| `idx_rfqs_company_project` | `{companyId:1, projectId:1}` | |
| `idx_rfqs_company_project_status` | `{companyId:1, projectId:1, status:1}` | |
| `uq_rfqs_company_number` | `{companyId:1, rfqNumber:1}` | **unique** |
| `idx_rfqs_company_line_requirement` | `{companyId:1, "lines.sourceMaterialRequirementId":1}` | multikey; serves §7.3/§7.5 line lookup |
| `uq_rfq_counters_company` | `{companyId:1}` | **unique** |

### 12.3 `suppliers`, `supplier_offerings`, `material_supplier_preferences`

| Name | Keys | Properties |
|---|---|---|
| `idx_suppliers_company` | `{companyId:1}` | |
| `uq_suppliers_company_name` | `{companyId:1, nameNormalized:1}` | **unique**, **not partial** — inactive names still collide (§4.1) |
| `idx_suppliers_company_active` | `{companyId:1, active:1}` | |
| `idx_suppliers_company_categories` | `{companyId:1, materialCategories:1}` | multikey, category filter |
| `idx_supplier_offerings_company` | `{companyId:1}` | |
| `idx_supplier_offerings_company_supplier` | `{companyId:1, supplierId:1}` | |
| `idx_supplier_offerings_company_material` | `{companyId:1, materialId:1}` | |
| `idx_supplier_offerings_company_active` | `{companyId:1, active:1}` | |
| `uq_material_supplier_preferences_material` | `{companyId:1, materialId:1}` | **unique** |
| `idx_material_supplier_preferences_supplier` | `{companyId:1, supplierId:1}` | |

### 12.4 Duplicate-key classification

Each repository implements `classifyCreateError`, matching the **explicit index
name** in the write-error message, as
`quotations/repository_mongo.go:193-209`:

| Index name matched | Sentinel |
|---|---|
| `uq_material_requirements_source_key` | `ErrSourceAggregationKeyExists` |
| `uq_material_requirements_resolution_op` | `ErrResolutionOperationAlreadyApplied` |
| `uq_rfqs_company_number` | `ErrRFQNumberConflict` |
| `uq_suppliers_company_name` | `ErrSupplierNameTaken` |
| `uq_material_supplier_preferences_material` | `ErrPreferenceAlreadyExists` |
| **any unmatched duplicate key** | `ErrUnclassifiedDuplicateKey` |

An unmatched duplicate-key error returns `ErrUnclassifiedDuplicateKey` and
never a specific sentinel: an error the code cannot positively identify must not
be assumed safe to retry.

---

## 13. HTTP API

All routes register on the **existing authenticated `authedAPI` group**. **M7
adds no unauthenticated route and no second `huma.API`.** `companyID` always
comes from `identity.PrincipalFromContext`, never from a body or query
parameter.

### 13.1 Material Requirements (`materialrequirements`)

| Method | Path | Purpose |
|---|---|---|
| POST | `/projects/{projectId}/material-requirements/generate` | union-based generation (§3.4) |
| POST | `/material-requirements` | create a manual requirement |
| GET | `/material-requirements?projectId=&workItemId=&status=&syncState=` | list, tenant-scoped |
| GET | `/material-requirements/{id}` | get one |
| PATCH | `/material-requirements/{id}` | edit; `expectedRevision` required |
| POST | `/material-requirements/{id}/review` | `draft → reviewed` |
| POST | `/material-requirements/{id}/acknowledge-unit` | acknowledge unit mismatch |
| POST | `/material-requirements/{id}/archive` | terminal archive |
| GET | `/material-requirements/{id}/source-discrepancy` | recomputed proposal (§5.5) |
| POST | `/material-requirements/{id}/source-discrepancy/resolve` | apply a resolution (§5.6) |
| POST | `/material-requirements/{id}/split` | split (§8) |
| POST | `/material-requirements/{id}/split/reconcile` | complete an interrupted split |

There is deliberately **no claim-reconciliation route here** — it lives on the
RFQ (§7.5).

### 13.2 RFQs (`rfqs`)

| Method | Path | Purpose |
|---|---|---|
| POST | `/projects/{projectId}/rfqs` | create an **empty** draft; header fields allowed, no requirement IDs |
| GET | `/rfqs?projectId=&status=` | list, tenant-scoped |
| GET | `/rfqs/{id}` | get one, including `InternalNotes` (contractor route) |
| PATCH | `/rfqs/{id}` | edit header fields; draft only |
| POST | `/rfqs/{id}/lines` | claim-and-append one requirement (§7.2) |
| DELETE | `/rfqs/{id}/lines/{lineId}` | remove line, then release claim (§7.4) |
| PATCH | `/rfqs/{id}/lines/{lineId}/sort-order` | reorder |
| GET | `/rfqs/{id}/claim-reconciliation` | inspect claim/line consistency, including orphaned claims (§7.5) |
| POST | `/rfqs/{id}/claim-reconciliation` | `retry_line` or `release` |
| POST | `/rfqs/{id}/ready` | `draft → ready`; increments `Revision` |
| POST | `/rfqs/{id}/reopen` | `ready → draft`, gated by `IssuanceStatusSource`; increments `Revision` |
| DELETE | `/rfqs/{id}` | delete an empty draft with **zero outstanding claims** (§7.7, §17) |

### 13.3 Suppliers (`suppliers`)

| Method | Path | Purpose |
|---|---|---|
| POST | `/suppliers` | create |
| GET | `/suppliers?q=&category=&active=` | search by name, filter by category/state (phase1.md §12) |
| GET | `/suppliers/{id}` | get one |
| PATCH | `/suppliers/{id}` | edit; `expectedRevision` required |
| POST | `/suppliers/{id}/active` | `{active: bool}` — retire or reactivate |
| POST | `/supplier-offerings` | create |
| GET | `/supplier-offerings?supplierId=&materialId=&active=` | list |
| GET | `/supplier-offerings/{id}` | get one |
| PATCH | `/supplier-offerings/{id}` | edit; `SupplierID` immutable |
| POST | `/supplier-offerings/{id}/active` | retire or reactivate |
| GET | `/materials/{materialId}/preferred-supplier` | read the preference |
| PUT | `/materials/{materialId}/preferred-supplier` | Revision-guarded create-or-replace |
| DELETE | `/materials/{materialId}/preferred-supplier` | clear (physical delete permitted) |

The three material-scoped preference routes are registered by the `suppliers`
handler even though the path is material-scoped — a URL design choice, not an
ownership claim. `materials` is not modified.

### 13.4 DTO rules

- Huma DTOs live only in each module's `handler.go`; no domain struct is
  serialized directly (ADR 0002).
- Quantities cross the wire as **canonical decimal strings**
  (`{"value": "33.5", "unit": "m2"}`), never JSON numbers — matching
  `costs.costItemDTO.QuantityValue` (`costs/handler.go:28-29`).
- Money crosses as `{"amount": <int64 minor units>, "currency": "MYR"}`,
  matching `costs.costMoneyDTO`. Minor-unit-to-display conversion is
  presentation-only (ADR 0001).
- Timestamps use the existing RFC 3339 `timeLayout` (`costs/handler.go:15`).
- **Every mutation, transition or deletion of an existing mutable aggregate
  takes `expectedRevision`. Creation endpoints do not** — there is no prior
  revision to expect. Discrepancy resolution additionally takes
  `expectedProposedFingerprint`.
- `InternalNotes` appears in contractor DTOs and is absent from every M8
  handoff projection (§9).

### 13.5 Status mapping

| Sentinel | HTTP |
|---|---|
| `ErrMaterialRequirementNotFound`, `ErrRFQNotFound`, `ErrRFQLineNotFound`, `ErrSupplierNotFound`, `ErrSupplierOfferingNotFound`, `ErrPreferenceNotFound` | 404 |
| `ErrProjectNotFound`, `ErrWorkItemNotFound`, `ErrMaterialNotFound` | 404 |
| `ErrInvalidQuantity`, `ErrInvalidUnit`, `ErrSupplierNameRequired`, `ErrInvalidURL`, `ErrInvalidIndicativePrice`, `ErrInvalidEmail` | 422 |
| `ErrSplitQuantityMismatch`, `ErrSplitTooFewChildren`, `ErrSplitUnitChanged`, `ErrRecursiveSplitNotSupported` | 422 |
| `ErrWorkItemRequiredForGeneratedRequirement`, `ErrMaterialIDImmutable`, `ErrWorkItemIDImmutable`, `ErrSupplierIDImmutable` | 422 |
| `ErrRFQNoLines`, `ErrRFQDeliveryAddressRequired`, `ErrRFQLineNotEligible`, `ErrResolutionActionNotAvailable` | 422 |
| `ErrSupplierInactive` | 422 — an inactive Supplier cannot receive a new offering (§4.2) or preference (§4.3) |
| `ErrRequirementNotEligibleForRFQ`, `ErrUnresolvedUnitMismatch`, `ErrUnresolvedSourceDiscrepancy` | 409 |
| `ErrMaterialRequirementAlreadyClaimed` | 409 |
| `ErrRequirementTerminal`, `ErrRFQNotDraft`, `ErrRFQNotReady`, `ErrRFQNotEmpty`, `ErrSplitInProgress` | 409 |
| `ErrRFQHasOutstandingClaims` | 409 — an orphaned claim still names this chain (§7.7) |
| `ErrRFQAlreadyIssued` | 409 — M8 has issued this chain; reopen refused, RFQ stays `ready` (§6.4) |
| `ErrRevisionMismatch` (all aggregates) | 409 |
| `ErrMaterialRequirementDiscrepancyChanged`, `ErrResolutionOperationConflict` | 409 |
| `ErrSupplierNameTaken`, `ErrPreferenceAlreadyExists` | 409 |
| `ErrIssuanceStatusUnavailable` | 503 |
| `ErrUnclassifiedDuplicateKey`, unmapped | 500 |

**Never reaches HTTP** — three sentinels are internal control flow:

| Sentinel | Handled by |
|---|---|
| `ErrSourceAggregationKeyExists` | §3.7 — reload the winning anchor, report `unchanged` |
| `ErrResolutionOperationAlreadyApplied` | §5.7 — reuse the existing delta child, retry the snapshot refresh |
| `ErrRFQNumberConflict` | **not retried** — see below |

If any reached a handler it would mean the recovery path was skipped, so
`map*Error` returns 500 rather than inventing a client-facing meaning.

`ErrRFQNumberConflict` deserves a note: because `NextRFQNumber` uses
`FindOneAndUpdate` + `$inc` and is atomic by construction (§6.1), two callers
can never receive the same number. A duplicate key on
`uq_rfqs_company_number` therefore does **not** indicate a losable race — it
indicates counter corruption or manual data tampering, where retrying would be
precisely wrong. The sentinel exists so the repository can distinguish that from
an unrelated duplicate key; it surfaces as 500 and is never retried.

---

## 14. Tenant isolation

1. `companyID` comes **only** from the authenticated principal.
2. **Every** query filter and **every** unique index leads with `companyId` —
   verified index-by-index in §12.
3. A foreign parent ID returns **404, never an empty list**
   (`costs/service.go:234-236`).
4. Cross-collection validation always re-checks company ownership: Project via
   `ProjectLookup`, WorkItem via `WorkItemProcurementContext` (compound project
   check), Material via `GetMaterialReference`, Supplier within `suppliers`.
5. Conditional updates carry `companyId` in the filter, so a cross-tenant `_id`
   guess matches zero documents.
6. `rfq_counters` is per-company; no `RFQNumber` sequence is shared.
7. The `MaterialRequirementSource` capability takes `companyID` on **every**
   method, so `rfqs` cannot reach a foreign requirement even with a valid ID.
8. `tenanttest/tenant_isolation_test.go` gains a case for **every** M7
   endpoint: company B's token against company A's resource → 404 (or 422 for a
   body-referenced foreign parent), never data and never an empty 200.

---

## 15. Failure ordering across collections

M7 introduces **no MongoDB transaction**. Every operation is an authoritative
write plus conditional updates, ordered so the observable intermediate state is
always fail-closed. No cross-document atomicity is claimed:

| Operation | Order | Window if interrupted |
|---|---|---|
| add RFQ line | claim requirement → append line | orphaned claim; requirement blocked from other RFQs; no phantom line. **Enumerable** via `ListClaimsForRFQChain` and repaired through `/rfqs/{id}/claim-reconciliation` (§7.5). The RFQ cannot be deleted while it persists (§7.7), so the claim can never be stranded by losing its chain. |
| remove RFQ line | remove line → release claim | requirement temporarily blocked; never available while its line may exist. A retry with the line already gone recovers the requirement ID from `ListClaimsForRFQChain` by `LineID` and completes the release (§7.3, §7.4). |
| split | manifest + `status=split` → create children → `splitState=completed` | source terminal while children may be missing; visibly incomplete; no duplicates possible; reconcile completes (§8.4) |
| `create_separate` | verify fingerprint → create delta child → refresh accepted snapshot | `draft` orphan child + still-unresolved discrepancy; no duplicate child possible; child not RFQ-eligible (§5.7) |
| generation | per-key independent writes | some anchors created/converged, others not; a repeat run completes the rest; idempotent (§3.7) |
| RFQ creation | allocate number → insert RFQ | a consumed number with no RFQ (a gap). `RFQNumber` is a display identifier with **no gapless guarantee**; gaps are expected and never backfilled |

Single-document operations (supplier edits, requirement edits, status
transitions, most resolutions) are atomic in MongoDB by construction and have
no window.

**A `ready` RFQ has no consistency window at all.** Its lines are snapshots, and
their source requirements are claimed and therefore fully immutable (§2.3), with
detection able to write only `SourceSyncState` and `SourceCheckedAt` (§5.8). So
no concurrent operation can change what a supplier would see. A source
requirement drifting to `change_detected` after the RFQ is `ready` is **not** an
inconsistency to reconcile — it is new demand the contractor may choose to
incorporate through the explicit reopen path (§6.5). M7 never demotes a `ready`
RFQ automatically.

---

## 16. Money and Quantity handling

- `RequiredQuantity`, `SourceQuantity` and `RFQLine.Quantity` are
  `foundation/quantity.Quantity` — `decimal` + unit. **No float anywhere.**
- Quantity aggregation and the split-conservation check use exact `decimal`
  addition and `decimal.Equal` — no epsilon, no tolerance.
- `SupplierOffering.IndicativePrice` is the **only** Money field in M7,
  `foundation/money.Money` (`int64` minor units + currency), validated by
  `money`'s own currency rules.
- M7 performs **no monetary arithmetic at all** — no sums, no allocation, no
  rate application — so it invokes `money.RoundToMinorUnits` nowhere and
  reimplements rounding nowhere. It stores and returns one indicative price
  unchanged.
- `foundation/money` and `foundation/quantity` need **no new functions**.

---

## 17. Deletion vs archive

| Record | Rule |
|---|---|
| `MaterialRequirement` | never hard-deleted. `archived` and `split` are terminal, read-only, permanently retained. |
| `Supplier` | never hard-deleted. `Active = false`, no cascade. |
| `SupplierOffering` | never hard-deleted. `Active = false`. |
| `MaterialSupplierPreference` | **may be physically deleted** — advisory only; audit preserves history. |
| `RFQ` | deletable only when `status == draft` **and** zero lines **and** `ListClaimsForRFQChain` returns zero claims (§7.7). An RFQ with any line, in `ready`, or holding an orphaned claim is never deleted; it is reopened, emptied and reconciled first. |
| `RFQLine` | removed from the draft (§7.4); the claim is released. |
| `rfq_counters` | never deleted; numbers never reused. |

Nothing in M7 deletes a record another record still references.

---

## 18. Acceptance-test matrix

### 18.1 Generation, aggregation, union algorithm

1. Eligible CostItems across two WorkItems produce one requirement per `{workItem, material, unit}` group, quantities summed exactly.
2. `category != material`, nil `MaterialID`, nil `WorkItemID`, nil quantity, zero/negative quantity, blank unit → each individually excluded, and each reflected in the correct one of the visitor's **five primitive counters**, which `materialrequirements.Service` assembles into `intrinsicallyIneligible`.
3. Two runs, unchanged CostItems → second creates nothing, reports `unchanged`.
4. Contractor edits `RequiredQuantity`; re-run does **not** overwrite it.
5. Contractor edits `RequiredQuantity.Unit`; re-run still matches the original anchor (immutable `SourceAggregationUnit`) — no duplicate anchor.
6. Concurrent generation on one key: one wins, the loser reloads and reports `unchanged`; exactly one anchor exists.
7. CostItem unit ≠ catalog unit → requirement created, CostItem unit preserved, `UnitMismatch = true`; **never** converted.
8. Two CostItems for one material in different units → two requirements, never summed.
9. **An anchor whose CostItems have all been deleted is discovered and classified `source_removed`** — the union algorithm's existing-only branch.
10. A `split` anchor is **not recreated**, is classified (not skipped), and reports `anchorStatus: "split"`.
11. An `archived` anchor is **not recreated**, is classified, and reports `anchorStatus: "archived"`.
12. A cancelled Work Item → `contextualSkipped` with `work_item_cancelled`; no requirement created.
13. A Material absent from the catalog → `contextualSkipped` with `material_not_found`.
14. No response field or skip reason mentions an inactive Material.

### 18.2 Fingerprint and sync state

15. `60` and `60.00` produce the same fingerprint.
15a. **Both canonical encodings begin with their version tag** — `"mrq-source-fingerprint-v1"` and `"mrq-source-key-v1"` — asserted against a golden hash, so a silent encoding change fails the test rather than silently re-baselining stored data.
16. A unit containing `|` or a newline does not collide with a different field split — length-prefixed encoding verified.
17. Reordered CostItem IDs produce the same fingerprint (sorted input).
18. A new contributing CostItem at the same total quantity still yields `change_detected` (membership changed).
19. Zero rows against a populated accepted snapshot → `source_removed`.
20. `keep_current` on `source_removed` → accepted snapshot becomes the empty aggregate; the next run stays `clean` (no perpetual warning).
21. `change_detected`, then CostItems revert to the accepted fingerprint → automatically `clean`.
22. **Detection does not modify `SourceQuantity`, `SourceFingerprint`, `SourceCostItemIDs` or `SourceSyncedAt`** — asserted field-by-field after a detection run that finds a discrepancy.
23. Detection **does** advance `SourceCheckedAt` and `SourceSyncState`.

### 18.3 Discrepancy resolution

24. `update` sets quantity **and unit** to proposed, recomputes `UnitMismatch`, clears acknowledgement, resets to `draft`, `clean`.
25. `merge`: accepted 33, contractor 35, proposed 41 → 43. **Verified after an intervening detection run**, proving the accepted snapshot survived (§5.8).
26. `merge` withheld when the contractor's unit ≠ the source aggregation unit.
27. `merge` rejected when the result would be ≤ 0.
28. `keep_current` leaves quantity and status untouched, refreshes the accepted snapshot, `clean`.
29. `create_separate` unavailable at zero or negative delta.
30. `create_separate` child: `SourceType = manual`, `draft`, delta quantity, no `SourceCostItemIDs`, carries the discrepancy back-reference.
31. Repeated `create_separate` with one `ResolutionOperationID` **against the same discrepancy** → exactly one child; the second call verifies all seven identity fields match and reuses it.
31a. **Reusing one `ResolutionOperationID` against a *different* source requirement → `409 ErrResolutionOperationConflict`**; no child is created, no existing child is returned or cross-linked, and **neither** requirement is modified.
31b. Reusing one `ResolutionOperationID` against the same requirement but a different proposed fingerprint or a different delta quantity → `409 ErrResolutionOperationConflict`.
31c. A `create_separate` child inherits `ProjectID`, `WorkItemID` (including nil), `MaterialID`, `MaterialName`, `CatalogUnit`, `Specification` and `RequiredByDate` from the source; its quantity is in the **proposed source unit**; `UnitMismatch` is recomputed and `UnitMismatchAcknowledged` is false.
31d. A `create_separate` child inheriting `ProjectID` can be claimed by an RFQ in that Project (guards against the §7.2 project filter rejecting it).
32. Stale `expectedProposedFingerprint` → 409, **nothing applied**, fresh proposal returned.
33. Stale `expectedRevision` → 409, nothing applied.
34. **`update` and `merge` are unavailable on a `split` anchor** → `422 ErrResolutionActionNotAvailable`; `keep_current` and positive-delta `create_separate` succeed.
35. **`update` and `merge` are unavailable on an `archived` anchor**; `archive` is also unavailable; `keep_current` succeeds.
36. A negative source change against a `split` anchor offers `keep_current` only, and **no split child is mutated**.
37. Every resolution action is rejected with 409 while the requirement is claimed.

### 18.4 Review state and mutability

38. Editing `RequiredQuantity`, `Specification`, `RequiredByDate`, `ProcurementNotes` or (manual) `MaterialID`/`WorkItemID` resets `reviewed → draft`.
39. Editing `InternalNotes` alone leaves `reviewed` intact.
40. Changing `MaterialID` on a manual requirement refreshes `MaterialName`/`CatalogUnit`, recomputes `UnitMismatch`, clears acknowledgement; a contractor-authored `Specification` is preserved.
41. Changing `MaterialID` or `WorkItemID` on a `cost_item` requirement → 422.
42. Changing `MaterialID` or `WorkItemID` on a **split child** → 422.
43. Any edit while claimed → 409, including `InternalNotes`.
44. `split` and `archived` reject every contractor edit regardless of `SourceType`.

### 18.5 Split

45. 100 → 60 + 40: source `split`; two `draft` children; sum exactly 100.
46. 60 + 41 → 422, nothing created.
47. A single child, or any non-positive child → 422.
48. Children inherit project/workItem/material/unit; a differing child unit → 422.
49. Split while claimed → 409.
50. Split of a `split` or `archived` requirement → 409.
51. **Split of a `SourceType = split` child → 422 `ErrRecursiveSplitNotSupported`** (§20 defers multi-level splitting).
52. Children carry `SourceType = split`, `SplitFromRequirementID`, a shared `SplitGroupID`, and **no** `SourceCostItemIDs`.
53. Interrupted split: manifest present, `splitState = creating`; reconcile creates exactly the missing children, no duplicates.
54. The source is excluded from RFQ eligibility and from further splitting after the split.

### 18.6 One-active-chain invariant and module split

55. Two RFQs racing one requirement: exactly one line exists; the loser gets 409.
56. Add, remove, then add to a **different** chain → succeeds.
57. Retry after an uncertain response returns the **existing** line, never a duplicate.
58. Claim present, line missing → `GET /rfqs/{id}/claim-reconciliation` **enumerates it as `orphaned_claim`** via `ListClaimsForRFQChain`, without the caller supplying a requirement ID; `retry_line` then appends using the claim's `LineID`.
59. Claim present, line missing → `release` clears the claim under Revision guard.
60. Append failure with successful compensation → claim cleared, original error returned.
61. `draft → ready → draft` leaves every claim intact.
62. A claimed requirement cannot be added to a second chain even after the first RFQ reaches `ready`.
63. **The claim's conditional update itself enforces §8.5** — a requirement made ineligible between pre-check and claim is not claimed.
64. **No `/material-requirements/*` route performs claim reconciliation**, and `materialrequirements` contains no reference to an RFQ line.
64a. **An RFQ cannot claim a requirement belonging to a different Project of the same company** → `ErrMaterialRequirementNotFound`, and the requirement's `ActiveRFQChainID` remains nil (no claim is created). Asserted with a reviewed, fully eligible requirement, so only the project filter can be rejecting it.
64b. A retry of `DELETE /rfqs/{id}/lines/{lineId}` after the line is already gone recovers the requirement via `ListClaimsForRFQChain` matched on `LineID` and completes the claim release.
64c. **An RFQ holding an orphaned claim cannot be deleted** → `409 ErrRFQHasOutstandingClaims`, even with zero lines and `status == draft`. After `release`, deletion succeeds.

### 18.7 RFQ lifecycle and lines

65. `POST /projects/{id}/rfqs` creates an empty draft with a unique `RFQNumber`; two creates produce two distinct numbers.
66. `mark ready` on an empty RFQ → 422.
66a. **`mark ready` on an RFQ with one valid claimed line SUCCEEDS.** This is the regression test for the replaced predicate: applying the unclaimed §8.5 rule (which requires `ActiveRFQChainID == nil`) would fail every non-empty RFQ, so this test fails if `ClaimedRequirementIsReadyForRFQ` is ever swapped back.
67. `mark ready` with a line whose requirement has an unresolved unit mismatch or unresolved discrepancy → 422, via `ClaimedRequirementIsReadyForRFQ`.
67a. `mark ready` with a line whose requirement's status is no longer `reviewed` → 422.
68. `mark ready` with a line whose requirement's `ActiveRFQChainID` names a **different** chain, or whose `ActiveRFQLineID` does not match the line → 422.
69. `mark ready` with a blank `DeliveryAddress` → 422.
70. `mark ready` succeeds → `ReadyAt` set, `Revision` **incremented**.
70a. **ABA guard:** a client reads a draft at revision R; the RFQ is marked ready and reopened; the client's mutation with `expectedRevision = R` → `409 ErrRevisionMismatch`. Verifies both transitions incremented `Revision`.
70b. `reopen` succeeds → `ReopenedAt` set, `Revision` **incremented**.
71. Every mutating line/header operation on a `ready` RFQ → 409.
72. `reopen` with `NoExternalIssuanceSource` succeeds → `draft`, claims intact.
73. `reopen` with a stubbed issued-chain source → **`409 ErrRFQAlreadyIssued`**, stays `ready`.
74. `reopen` with a failing issuance source → **`503 ErrIssuanceStatusUnavailable`, stays `ready`** (fail closed).
74a. **A `ready` RFQ whose source requirement drifts to `change_detected` is NOT demoted**: status stays `ready`, the line is byte-identical, and no automatic `ready → draft` occurs (§6.5).
75. **A claimed requirement cannot be edited, so a line's snapshot cannot drift** — attempting every mutating requirement operation while claimed returns 409, and the line is byte-identical afterwards.
76. `RFQLine` JSON contains no price, cost, margin, internal-notes or snapshot-fingerprint field.
77. Deleting a non-empty or `ready` RFQ → 409; deleting an empty draft succeeds.
78. No route matching `*/line-drift` or `*/drift/*` exists in the OpenAPI document.

### 18.8 Suppliers, offerings, preference

79. Duplicate normalized supplier name → 409, whether the existing record is active **or inactive**.
80. `"  ABC   Materials "` and `"abc materials"` collide; an empty normalized name → 422.
81. A supplier with only a name is creatable; contact fields optional.
82. An invalid email → 422; a valid one is normalized.
83. `MaterialCategories` trimmed, deduped case-insensitively, deterministically ordered.
84. Deactivating a supplier modifies no offering and no preference; effective availability becomes false.
85. `SupplierID` change on an offering → 422.
86. Offering creation against an inactive **supplier** → 422.
87. Offering creation referencing a **non-existent** Material → 404. There is **no inactive-Material rejection test**, because M7 has no such concept.
88. Effective availability is `Offering.Active AND Supplier.Active` — a linked Material's absence from the catalog does not make an offering unavailable.
89. `IndicativePrice` with no `IndicativePriceAsOf` → 422; zero or negative amount → 422; nil price with non-nil `AsOf` → 422; price with blank `Unit` → 422.
90. Clearing the price clears `IndicativePriceAsOf` and leaves `LastVerifiedAt` untouched.
91. `javascript:`, `data:`, `file:` URLs → 422; a valid URL is stored canonicalized.
91a. **`https://user:password@example.com/product` → 422 `ErrInvalidURL`**; **`https://user@example.com/image` → 422**. No credential is ever persisted.
92. **No HTTP request or DNS lookup is ever issued** for `ImageURL`/`ProductURL` — asserted with a network-refusing test double.
93. A second `PUT` preferred-supplier replaces the first under Revision guard; one preference per material persists.
94. Preference against an inactive supplier → 422; against a non-existent Material → 404.
95. A preference whose supplier later becomes inactive remains readable and is reported unavailable.
96. `DELETE` preference removes the record; the `Material` document is untouched.

### 18.9 Tenant isolation and audit

97. Every M7 endpoint: company B's token against company A's resource → 404 (or 422 for a body-referenced foreign parent), never data, never an empty 200.
98. `RFQNumber` sequences are independent per company.
99. Generation for a foreign `projectId` → 404, and creates nothing.
100. `rfqs` cannot claim a requirement belonging to another company even with a valid requirement ID.
101. Every audit event carries the acting user, records only primitives, and contains no Money amount, no whole domain struct and no supplier free text.
102. Every method on all three `AuditRecorder` interfaces is emitted by its operation and asserted. The count is derived from the interfaces as implemented — §1.5 declares 8 + 8 + 8 = 24; the test must fail if an interface method has no emitting operation, rather than asserting a hard-coded total.
103. Manual requirement creation, requirement update, unit acknowledgement, RFQ header update and empty-RFQ deletion each emit their audit event.

### 18.10 Architecture

104. `materialrequirements` does **not** import `rfqs` or `suppliers`.
105. **`rfqs` declares `MaterialRequirementSource` and imports no `materialrequirements` package type at all** — asserted by grepping `rfqs`' imports for the `materialrequirements` path and requiring zero matches. **The composition adapter is the only package importing both `rfqs` and `materialrequirements`.**
105a. **`costs` does not import `materialrequirements`** — `VisitEligibleMaterialCostItems` returns five primitive counters, so `costs.Service` satisfies the interface structurally with no adapter and no import.
106. `suppliers` imports neither `rfqs` nor `materialrequirements`.
107. No M7 module imports another module's repository or `mongo.Collection`.
108. No M7 module is imported by `costs`, `materials`, `projects`, `work`, `estimates`, `quotations`, `access`, `approvals` or `audit`.
109. `rfqs` compiles against `NoExternalIssuanceSource` and against a stub returning `true` — no service-logic change between them.
110. `GetReadyRFQSnapshot`'s signature contains no `InternalNotes` and no price field (compile-time).
111. `internal/procurement` no longer exists.
112. `go vet ./...` and `gofmt -l` clean; the full existing suite passes unchanged (no M3–M6 behaviour regression).

---

## 19. Domain errors

```go
// package materialrequirements
ErrMaterialRequirementNotFound
ErrProjectNotFound
ErrWorkItemNotFound
ErrMaterialNotFound
ErrInvalidQuantity
ErrInvalidUnit
ErrWorkItemRequiredForGeneratedRequirement
ErrMaterialIDImmutable
ErrWorkItemIDImmutable
ErrRequirementTerminal
ErrRequirementNotEligibleForRFQ
ErrUnresolvedUnitMismatch
ErrUnresolvedSourceDiscrepancy
ErrMaterialRequirementAlreadyClaimed
ErrMaterialRequirementDiscrepancyChanged
ErrResolutionActionNotAvailable
ErrResolutionOperationAlreadyApplied
ErrSourceAggregationKeyExists
ErrSplitQuantityMismatch
ErrSplitTooFewChildren
ErrSplitUnitChanged
ErrRecursiveSplitNotSupported
ErrSplitInProgress
ErrResolutionOperationConflict
ErrRevisionMismatch
ErrUnclassifiedDuplicateKey

// package rfqs
ErrRFQNotFound
ErrRFQNotDraft
ErrRFQNotReady
ErrRFQNoLines
ErrRFQNotEmpty
ErrRFQHasOutstandingClaims
ErrRFQAlreadyIssued
ErrRFQDeliveryAddressRequired
ErrRFQLineNotFound
ErrRFQLineNotEligible
ErrRFQNumberConflict
ErrProjectNotFound
ErrMaterialRequirementNotFound
ErrMaterialRequirementAlreadyClaimed
ErrRequirementNotEligibleForRFQ
ErrRevisionMismatch
ErrIssuanceStatusUnavailable
ErrUnclassifiedDuplicateKey

// package suppliers
ErrSupplierNotFound
ErrSupplierNameRequired
ErrSupplierNameTaken
ErrSupplierInactive
ErrInvalidEmail
ErrSupplierOfferingNotFound
ErrSupplierIDImmutable
ErrInvalidURL
ErrInvalidIndicativePrice
ErrMaterialNotFound
ErrPreferenceNotFound
ErrPreferenceAlreadyExists
ErrRevisionMismatch
ErrUnclassifiedDuplicateKey
```

`rfqs` declares its own claim sentinels; the composition adapter (§1.4.1)
translates `materialrequirements`' equivalents into them, so neither package
imports the other's errors.

**`ErrMaterialInactive` does not exist** in any package — M7 has no
inactive-Material concept (§0.3 conflict D). `ErrSupplierInactive` does exist,
because `Supplier.Active` is an M7-owned field.

Each is a package-level `errors.New` sentinel, matched with `errors.Is` in the
handler's `map*Error` function (`costs/handler.go:263-296`).

---

## 20. Explicitly deferred

**To M8 (external procurement).** Supplier invitations; `RFQInvitation`;
supplier access grants; supplier-specific secure links and tokens; the supplier
portal; supplier-submitted Offers; offer comparison; supplier selection; RFQ
issuing; issued RFQ versioning (V1/V2 immutability); email or WhatsApp
delivery; PDF generation; the stable-link/rotation model in §9.1.

**Not implemented in M7 and not scheduled here.** Web scraping or any
server-side URL fetch; assisted URL import of supplier product data;
AI-generated material requirements; AI supplier suggestion; automatic
supplier-product import; supplier scoring, ratings or performance analytics;
purchase orders; goods receipt; procurement payments; a generalized allocation
or reservation engine beyond the split operation; **multi-level splitting — a
`SourceType = split` child cannot be split again (§8.3, test 51)**;
unit-conversion tables; a generation dry-run/preview endpoint;
supplier-suggestion **ranking** (the preference data exists in M7 and is
advisory; ordering RFQ candidates is M8's); cross-project requirement
consolidation; requirement-level delivery scheduling; RFQ templates; any
Material active/archival state (M3 has none — §0.3 conflict D).

**Deliberately not done.** Mongo transactions (§15 documents each failure
window instead); a proposal or discrepancy collection (§5.1 recomputes);
`Revision` on `CostItem` (§0.3 conflict B); `PreferredSupplierID` on `Material`
(§0.3 conflict A); an `Active` field on `Material` (§0.3 conflict D); a shared
cross-module audit interface (§1.5); an RFQ line-drift subsystem (§6.3); a
gapless `RFQNumber` guarantee (§15).

---

## 21. Self-review

- **No `TODO`, `TBD` or unresolved placeholder** remains. (`...` appears only
  inside illustrative JSON and Go elisions, never for an undecided design
  point.)
- **Models, indexes, endpoints and capabilities agree.** `SourceAggregationKey`
  (§2, §3.3) is the index in §12.1 and the classification in §12.4.
  `ActiveRFQLineID` (§2) is the mechanism in §7.2–7.5 and the retry matrix
  §7.3. `IssuanceStatusSource` (§1.3) is used in §6.4 and tested in 18.7/74 and
  18.10/109. `SourceCheckedAt` (§2) is written only where §5.8 permits and
  asserted in 18.2/23.
- **Three modules, one-way dependency, no stray imports.** §1.1 states it; §1.3
  gives `rfqs` its only path to a requirement and imports **no**
  `materialrequirements` type; §1.4.1 names the single adapter as the only
  package importing both; `MaterialCostSource` returns **primitives** so `costs`
  needs no import and no second adapter (§1.2). Tests 104, 105, 105a, 106, 107
  enforce all of it. Claim reconciliation is `rfqs`-owned (§7.5) and absent from
  `/material-requirements/*` (§13.1, test 64).
- **The claim enforces Project identity atomically.** `projectId` sits in the
  same conditional-update filter as `companyId`, `revision`, eligibility and
  `activeRfqChainId = null` (§7.2), so a same-company different-Project
  requirement matches zero documents and no claim is created; test 64a.
- **Mark-ready uses the claimed predicate, not the unclaimed one.**
  `ClaimedRequirementIsReadyForRFQ` (§1.3, §6.4) requires the claim to name this
  chain and line; §8.5's `ActiveRFQChainID == nil` term applies only inside
  `ClaimForRFQ`. Test 66a is the regression guard — it fails if the unclaimed
  predicate is ever reapplied at mark-ready.
- **Orphaned claims are enumerable and cannot be stranded.**
  `ListClaimsForRFQChain` inverts the lookup `ReadClaim` cannot do (§7.5), backs
  remove-line retry recovery (§7.3), and gates deletion (§7.7). Tests 58, 64b,
  64c.
- **Both RFQ status transitions increment `Revision`**, closing the
  `draft → ready → draft` ABA hazard that copying `quotations.Finalize` would
  have left open (§6.4, §10, §11); tests 70, 70a, 70b.
- **Operation-ID reuse is verified, not blind.** Seven identity fields must match
  before an existing delta child is reused; any mismatch is
  `ErrResolutionOperationConflict` with nothing modified (§5.7); tests 31, 31a,
  31b.
- **Both canonical encodings carry a version tag**, so a future encoding change
  cannot compare incompatible formats as equal — which matters most for
  `SourceAggregationKey`, since it backs a unique index (§3.3, §5.2); test 15a.
- **URLs with embedded credentials are rejected** (`u.User != nil`), preventing
  both plaintext credential persistence and `user@host` display spoofing (§4.4);
  test 91a.
- **A ready RFQ is never silently changed or automatically demoted.** §6.5 and
  §15 state that post-ready source drift does not mutate the line, does not
  demote the RFQ, and is incorporated only through the explicit reopen path;
  test 74a.
- **No invented Material state.** `GetMaterialReference` returns no `active`
  bool (§1.2, §1.4); `ErrMaterialInactive` and `material_inactive` appear
  nowhere (§19, §3.7); effective offering availability excludes Material state
  (§4.2); tests 14, 87, 88 assert the absence.
- **Detection cannot corrupt `merge`.** §5.8's table forbids detection from
  writing the accepted snapshot; §5.6's `merge` formula names the accepted
  quantity explicitly; test 25 verifies the arithmetic across an intervening
  detection run.
- **`source_removed` is reachable.** §3.4's union includes existing-only keys;
  test 9 covers it.
- **Terminal anchors are classified, never skipped and never recreated.** §3.4
  removes both skip reasons and reports `anchorStatus`; tests 10, 11.
- **Terminal-anchor actions are constrained.** §5.6's matrix forbids `update`
  and `merge` on `split`/`archived`; children are never auto-mutated; tests
  34–36.
- **No drift subsystem exists**, because §2.3 makes drift unreachable. §6.3
  states the reasoning and the `keep_snapshot` incoherence; `RFQLine` has no
  fingerprint (§6.2); tests 75, 76, 78.
- **Cancelled Work Items are genuinely enforced**, via
  `WorkItemProcurementContext` (§1.2), reported as `work_item_cancelled`
  (§3.7), tested at 12.
- **Skip reporting is honest.** §3.8 separates `intrinsicallyIneligible`
  (counted inside `costs`) from `contextualSkipped` (emitted rows rejected on
  project context); test 2 asserts the intrinsic counts.
- **Audit counts are declared, not guessed.** §1.5 states 8 + 8 + 8 = 24 and
  test 102 requires derivation from the interfaces rather than a hard-coded
  total — the earlier revision asserted two different wrong numbers.
- **`SourceType` vs `Status` disambiguated.** §2.2's column is labelled
  "`split` (child)" and §2.1 states that terminal status overrides every
  source-type rule. Recursive splitting is rejected (§8.3) matching §20's
  deferral; test 51.
- **Generated data can never overwrite contractor-reviewed data.** §3.4 never
  touches contractor fields; §5.8 restricts detection to two fields; every
  resolution is contractor-initiated; tests 4, 5, 22.
- **One requirement cannot enter two active chains.** §7.2's claim requires
  `activeRfqChainId = null` and embeds §8.5; §6.4 keeps claims across status
  changes; tests 55, 57, 62, 63.
- **Split conserves quantity exactly.** §8.1 requires `decimal.Equal` with no
  tolerance; §8.4 rejects a claimed source; tests 45–48.
- **Ready RFQs cannot be silently changed.** §6.4 mutations require
  `status:draft`; reopen fails closed; tests 71, 73, 74.
- **Indicative prices cannot be mistaken for Supplier Offers.** §4.2 states it;
  §6.2, §1.3 and §9 make it structural — no price field exists on `RFQLine`,
  `ClaimSnapshot` or `RFQLineSnapshot`, and `suppliers` and `rfqs` never import
  each other; test 76.
- **No M8 link/invitation/offer/version code entered scope.** §9.1 is prose
  only; §6.1 has no version field; §20 lists exclusions; no token, grant,
  invitation or offer type appears in §2, §4 or §6.
- **Every tenant lookup and index includes `companyId`** — §12 index-by-index,
  §14 rule-by-rule, including the capability-level rule (§14.7); tests 97, 100.
- **No whole domain struct crosses a boundary.** §1.2–§1.4 pass primitives and
  `decimal.Decimal`; `ClaimSnapshot` and `RFQLineSnapshot` are consumer-owned
  projections reached through the §1.4.1 adapter; audit takes primitives only;
  tests 105, 110.
- **Every multi-document failure window is documented honestly.** §15 tabulates
  all six with their observable intermediate states; §5.7 and §8.4 refuse to
  call their orphans harmless. **No cross-document atomicity is claimed
  anywhere**, and no Mongo transaction is introduced.

---

## 22. Open decisions

None. Every question the code or phase1.md could not settle was resolved before
this revision:

| Question | Resolution |
|---|---|
| Module decomposition | three modules: `materialrequirements`, `rfqs`, `suppliers`; one-way dependency (§1.1) |
| Re-sync without `CostItem.Revision` | value comparison + SHA-256 fingerprint; M3 unmodified (§0.3 B, §5) |
| Catalog unit vs CostItem unit | aggregate by CostItem unit, flag mismatch, never convert, block RFQ until resolved (§0.3 C, §3.5) |
| `Preferred Supplier` on Material | separate `MaterialSupplierPreference` in `suppliers` (§0.3 A, §4.3) |
| Material active state | does not exist in M3, so M7 does not model it (§0.3 D) |
| One-active-chain enforcement | requirement is the serialization point; claim fields owned by `materialrequirements`, orchestration by `rfqs` (§7) |
| Split source disposition | terminal `split`, read-only, exact conservation, manifest recovery, no recursion (§8) |
| M8 reopen gate | consumer-defined `IssuanceStatusSource` keyed on `rfqChainID`, `NoExternalIssuanceSource` in M7, fail closed (§1.3, §6.4) |
| RFQ creation shape | empty draft, lines added individually (§6.1, §7.2) |
| Generation flow | direct create as `draft`; review is the gate; no preview (§3.6, §3.8) |
| Cancelled Work Items | narrow `WorkItemProcurementContext` capability + `work_item_cancelled` contextual skip (§1.2, §3.7) |
| Ineligible-CostItem reporting | visitor returns five primitive counters; `materialrequirements` assembles them and reports them separately from contextual skips (§1.2, §3.8) |
| Line drift | subsystem removed — the claim guarantees snapshot stability (§6.3) |
| Ineligible-counter return type | five primitives, so `costs` needs no import and no adapter (§1.2) |
| Project identity on the claim | `projectId` inside the atomic claim filter (§7.2) |
| Mark-ready validation | `ClaimedRequirementIsReadyForRFQ`, not the unclaimed §8.5 predicate (§1.3, §6.4) |
| Orphan discovery | `ListClaimsForRFQChain`, and it gates RFQ deletion (§7.5, §7.7) |
| RFQ status transitions | both increment `Revision` (§6.4) |
| Operation-ID reuse | verified against seven identity fields, else `ErrResolutionOperationConflict` (§5.7) |
| Encoding evolution | version tag first in both canonical encodings (§3.3, §5.2) |
| Post-ready source drift | reported, never auto-applied; no automatic demotion (§6.5) |

---

## 23. Revision log

### Revision 4 (2026-07-27) — one implementation-discovered amendment

1. **Added `ReadClaimSnapshot` as a sixth `MaterialRequirementSource`
   capability.** Discovered during Phase C implementation and approved as a
   specification amendment, not a deviation.

   §7.5's documented failure window explicitly expects reconciliation to repair
   a missing RFQ line, and §7.3's retry matrix requires the same for an
   interrupted claim-first add. But §1.3 originally exposed five capabilities,
   **none of which could retrieve the immutable snapshot a claimed requirement
   carries**. Re-claiming cannot work — the requirement is already claimed, so
   `ClaimForRFQ` correctly refuses — and `ClaimForRFQ`'s return value was the
   only source of a `ClaimSnapshot`. The remaining options were fabricating
   supplier-visible content or leaving every orphaned claim permanently
   unrepairable; both contradict §7.5.

   The capability is **scoped to the exact existing claim**: the read requires
   `companyId`, `requirementId`, `activeRfqChainId == rfqChainID` **and**
   `activeRfqLineId == lineID`, so it cannot serve as a general requirement
   read. It **does not re-run §8.5 eligibility** — the claim was eligible when
   created, contractor fields are frozen while it exists (§2.3), and only
   detection state may move (§5.8), so re-checking would make an interrupted
   line unrepairable after an ordinary source change.

   §1.3, §1.4.1, §7.3, §7.5. Coverage: exact chain+line → snapshot; wrong
   chain, wrong line, unclaimed, foreign tenant → rejected; post-claim
   `SourceSyncState` change → repair still available; snapshot excludes
   `InternalNotes`, cost, margin and price. E1 additionally covers adapter
   conversion and sentinel translation for this sixth method.

### Revision 3 (2026-07-26) — ten final corrections, then Approved

1. **Fixed the material-cost capability type boundary.**
   `VisitEligibleMaterialCostItems` now returns **five primitive counters**
   instead of a named `IneligibleSummary`. Returning a consumer-owned named type
   would have forced `costs` to import `materialrequirements` to satisfy the
   interface, inverting the dependency and requiring a second adapter — directly
   contradicting revision 2's own claim that one adapter suffices. §1.2, §1.4.1,
   §3.1, §3.8; tests 2, 105a.
2. **`ClaimForRFQ` now takes and atomically enforces `projectID`.** Without it a
   reviewed requirement from another Project of the same company was claimable.
   §1.3, §7.2; test 64a.
3. **Replaced `RequirementIsRFQEligible` with
   `ClaimedRequirementIsReadyForRFQ`.** The old method claimed to re-check §8.5,
   which includes `ActiveRFQChainID == nil` — true of no requirement already on a
   line, so **every non-empty RFQ would have been impossible to mark ready**. The
   new method validates the claimed state and subsumes the separate
   "claim belongs to this chain" check. §1.3, §6.4, §8.5; tests 66a, 67, 67a, 68.
4. **Added `ListClaimsForRFQChain` and `RFQRequirementClaim`.** `ReadClaim`
   cannot discover an orphan, since it answers only for a requirement ID the
   caller already has. The new capability backs orphan enumeration, remove-line
   retry recovery, deterministic `retry_line`/`release`, and a new deletion
   guard. §1.3, §7.3, §7.5, new §7.7, §13.2, §15, §17; tests 58, 64b, 64c.
5. **Both RFQ status transitions now increment `Revision`.** Copying
   `quotations.Finalize`'s non-incrementing behaviour was wrong: a Quotation never
   returns to draft, but `draft → ready → draft` creates an ABA hazard letting a
   stale pre-ready client mutate a reopened RFQ. §6.4, §10, §11; tests 70, 70a,
   70b.
6. **Added `ErrRFQAlreadyIssued`** (409), which revision 2 described behaviourally
   without declaring. §6.4, §13.5, §19; test 73.
7. **Operation-ID reuse is now verified.** On
   `ErrResolutionOperationAlreadyApplied` the service confirms seven identity
   fields match before reusing a child; any mismatch returns the new
   `ErrResolutionOperationConflict` (409) and modifies nothing. Also specified the
   full inheritance list for a `create_separate` child. §5.6, §5.7, §13.5, §19;
   tests 31, 31a–31d.
8. **Restored two encoding/URL rules.** Both canonical encodings begin with a
   version tag (`"mrq-source-fingerprint-v1"`, `"mrq-source-key-v1"`); the URL
   validator rejects embedded credentials (`u.User != nil`). §3.3, §5.2, §4.4;
   tests 15a, 91a.
9. **Corrected architecture and DTO wording.** `rfqs` imports **no**
   `materialrequirements` type; the adapter is the only package importing both.
   `expectedRevision` is required for every mutation, transition or deletion of an
   existing mutable aggregate — **not** for creation. §1.4.1, §13.4; test 105.
10. **Clarified ready-snapshot semantics.** Readiness is point-in-time validation
    of the already-claimed snapshot. Post-ready source drift does not mutate the
    line, does not demote the RFQ, and is incorporated only via the explicit
    reopen path. New §6.5, §15; test 74a.

### Revision 2 (2026-07-26) — nine blocking corrections

1. **Three modules.** `internal/materialrequirements`, `internal/rfqs`,
   `internal/suppliers` replace the single `internal/procurement`. `rfqs`
   consumes an `rfqs`-owned `MaterialRequirementSource` through the one
   composition adapter. **Claim reconciliation moved to `rfqs`** — the
   requirement module never implements `retry_line` or inspects an RFQ line.
   Rewrote §1, §2 header, §7, §9, §12, §13, §19; added tests 104–111.
2. **Removed the invented Material active state.** `GetMaterialReference` no
   longer returns `active`. Deleted `ErrMaterialInactive`, `material_inactive`,
   inactive-Material offering/preference rules and their tests. Effective
   offering availability is now `Offering.Active AND Supplier.Active`. Recorded
   as §0.3 conflict D.
3. **Narrowed the source-sync writer rule.** Detection may write only
   `SourceSyncState` and the new `SourceCheckedAt`; the **accepted** snapshot is
   writable only by explicit resolution — required for `merge`'s
   `proposed − accepted` arithmetic. New §5.8 table; tests 22, 23, 25.
4. **Fixed the generation algorithm.** It now processes the **union** of
   existing `cost_item` anchors and computed keys, so an anchor with zero
   current rows is discovered and classified `source_removed`. Removed
   `split_anchor_exists` / `archived_anchor_exists` skip reasons; terminal
   anchors are classified with `anchorStatus`. New §3.4; tests 9–11.
5. **Removed the line-drift subsystem.** §2.3's claimed-requirement
   immutability makes drift unreachable, and `keep_snapshot` was incoherent.
   Deleted both endpoints, `RecordRFQLineDriftResolved`,
   `ErrRFQLineDriftChanged`, `RFQLine.SnapshotFingerprint` and the drift tests.
   New §6.3 explains why; tests 75, 76, 78.
6. **Added the terminal-anchor action matrix.** `update` and `merge` are
   impossible on `split`/`archived` anchors; negative changes offer
   `keep_current` only and never auto-mutate children. New §5.6 matrix; tests
   34–36.
7. **Enforced cancelled Work Items.** New narrow
   `WorkItemProcurementContext` capability; `work_item_cancelled` contextual
   skip. §1.2, §3.7; test 12.
8. **Fixed skip reporting.** The visitor returns `IneligibleSummary` for rows
   filtered inside `costs`; the response separates `intrinsicallyIneligible`
   from `contextualSkipped`. §1.2, §3.8; test 2.
9. **Corrected the audit counts and interfaces.** Three consumer-owned
   interfaces, 8 + 8 + 8 = 24 methods as declared. Revision 1 asserted 20, then
   "corrected" it to 22 — both wrong. Test 102 now requires derivation from the
   interfaces rather than a hard-coded total. Added audit events for manual
   creation, requirement update, unit acknowledgement, RFQ header update and
   empty-RFQ deletion.

Also: disambiguated `SourceType = split` (child) from `Status = split`
(terminal source) in §2.1/§2.2, and rejected recursive splitting
(`ErrRecursiveSplitNotSupported`, §8.3) to match §20's deferral.

**Retained from revision 1, confirmed acceptable:**
`ErrRFQNumberConflict` surfaces as an internal error and is never retried
(§13.5); claimed requirements cannot edit even `InternalNotes` (§2.3);
`RFQNumber` gaps from allocate-before-insert remain documented (§15).
