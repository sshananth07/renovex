# Gap Analysis: Award → Committed Cost Integration

**Status:** Undesigned, deliberately deferred. Not a bug, not scheduled for this
implementation batch. This document exists so a future design conversation
starts from accurate ground truth instead of re-discovering the same
constraints.

**Trigger:** A QA re-validation run confirmed that finalizing a procurement
Award (`POST .../award-revisions`) does not create or update any Cost Item —
the project's COMMITTED total is unaffected, and no network/service call
touches `internal/costs` during award finalization. This document investigates
why, and what a correct bridge would need to account for.

---

## 1. This is intentional, not an oversight

`internal/awards/doc.go` states the boundary explicitly:

> "**Award is not commitment** — An award records the contractor's sourcing
> decision only. It does not create a Purchase Order, contractual acceptance
> or committed cost, and it does not allocate anything into the cost ledger."
> "Purchase Orders, acceptance, decline and change requests belong to the next
> milestone."

`phase1.md` agrees. Its procurement lifecycle diagram (lines 112–138) places
`Future Purchase Order → Committed Cost → Actual Cost → Paid Cost` downstream
of Supplier Offer — note the label **"Future"** on Purchase Order, i.e.
not-yet-built. The Phase 1 MVP scope list (§66, lines 3608–3644) enumerates
what Phase 1 actually delivers — `Award` and committed-cost automation are
absent from it. Immediately after, under "Future systems extend the same
architecture" (lines 3646–3667): `Procurement ↓ Creates Committed Costs` is
listed alongside LiDAR, Computer Vision, and Digital Twin — explicitly
roadmap, not current spec.

At the composition root (`cmd/api/main.go`), `awardsService` and
`costsService` are constructed completely independently, with no adapter
connecting them — consistent with the documented boundary, not an
accidental omission.

## 2. What already exists that a bridge could build on

**On the awards side** (`internal/awards`):
- `AwardedLine` (`calculation.go:49-68`) carries `SupplierID`, `SupplierName`,
  `UnitPriceExcludingTax`, `LineSubtotal`, `LineTaxAmount`, brand/SKU/lead-time
  detail — commercially rich enough in principle to derive a committed amount.
- `FinaliseAward`/`CorrectAward` (`revision.go`, `correction_service.go`) are
  already idempotent via `OperationID` lookup (`FindRevisionByOperation`) —
  a retry short-circuits to the existing revision rather than re-running.
- The narrow-capability + composition-adapter pattern is already
  well-established (`awards/capabilities.go`: `IssuedRFQSource`,
  `OfferVersionSource`, etc., each satisfied by an adapter in
  `composition/m8adapters.go`) — a hypothetical `AwardCostSink` would follow
  the same shape.

**On the costs side** (`internal/costs`):
- `CostItem.Committed *money.Money` already exists as one of four
  independently-nilable lifecycle fields (`cost_item.go:80-99`).
- `UpdateCostItemLifecycle(ctx, companyID, costItemID, stage, amount)`
  (`service.go:254-266`) can set a `Committed` amount — but only on a
  pre-existing item by ID; there is no upsert-by-external-key.
- `RecordLabourCost`/`LabourCostRecorder` (`service.go:306-325`) is the
  module's own precedent for "a producer module gets a narrow,
  category-scoped creation capability that bypasses the generic
  `CreateCostItem` gate." A hypothetical `RecordAwardedCost` would follow
  this exact shape, the same way `CreateCostItem` already hard-rejects
  `category=labour` (`ErrLabourCategoryNotAllowed`) to keep that entry point
  reserved.
- `materialrequirements`' `SourceAggregationKey` (unique index,
  duplicate-create treated as a no-op — `generation.go:309-315`) is the
  proven idempotency pattern for a step that may legitimately run more than
  once (`FinaliseAward`'s `completePublication` is documented as
  "recoverable," meaning downstream side effects must tolerate replay). This
  is a better fit than a bare `OperationID` check, since the dedup needs to
  survive at the cost-item layer, not just the award-revision layer.

## 3. What's missing — concretely, not just "the wiring"

1. **No code path connects the two modules at all.** Confirmed both by the
   `awards/doc.go` disclaimer and by `main.go`'s independent construction of
   both services.
2. **`AwardedLine` doesn't carry `MaterialID`.** Only `MaterialName` (free
   text) survives from `IssuedRFQLineSnapshot` into `AwardedLine` —
   `calculateAward` drops `MaterialID` on the way. A bridge needs a code
   change here or a second lookup.
3. **`AwardRevision` has no `ProjectID`/`WorkItemID`.** Awards is scoped to
   RFQ chains and offer chains only; it has no notion of Project. Resolving
   Project/WorkItem context would require a new cross-module lookup (likely
   via `materialrequirements`, since RFQ lines trace back to
   `MaterialRequirement.WorkItemID`) that doesn't exist today.
4. **No `costs`-side entry point reserved for procurement.** Unlike labour's
   `RecordLabourCost`, there is no `RecordAwardedCost` or
   `AwardCostRecorder` interface. `CreateCostItem`/`UpdateCostItemLifecycle`
   are the only public methods, and neither is scoped for this use.
5. **No lineage/provenance fields on `CostItem`.** Nothing like
   `materialrequirements`' `SourceType`/`SourceAggregationKey`/
   `SourceFingerprint` exists on `CostItem` today — no `SourceAwardRevisionID`,
   no `SourceAwardLineID`, nothing. This is a schema gap, not just service
   logic (`CostItem.SchemaVersion` confirms the type is already built for
   additive migration, so this is tractable, just not done).
6. **Correction semantics are undecided against the cost ledger.** Award
   corrections (`CorrectAward`) are explicitly monotonic-only — a correction
   can only add lines/suppliers or increase committed amounts, never
   decrease or reverse (`correction.go`: `ErrAwardCorrectionNotMonotonic`
   guards, "commercially MONOTONIC" doc comment). A bridge's `CorrectAward`
   counterpart must decide: increment the existing linked `CostItem.Committed`
   by exactly `CorrectionDeltaTotal`, or create one additional `CostItem` per
   correction (linked via the same proposed lineage fields, pointing at the
   correction's revision). Nothing in the current code or `phase1.md`
   dictates which. Multiple committed cost items coexisting per work
   item/material is already consistent with how `costs` works today (no
   uniqueness constraint prevents it, and totals are already summed across
   many rows) — so "new row per revision" is the lower-risk default if this
   is ever built, but it's a real design choice, not a given.

## 4. What a correct design would need to decide, before any code is written

- **Direction:** `awards` declares a narrow `AwardCostSink`-shaped capability
  (owning what it needs, primitives/money/quantity only — never a
  `costs.CostItem` struct crossing the boundary, per ADR 0002); `costs`
  exposes a new `RecordAwardedCost`-style method; a composition adapter
  wires them. Never the reverse — `costs` must not import or know about
  `awards`, matching the existing one-way-dependency precedent in
  `materialrequirements`.
- **Trigger point:** which step of `FinaliseAward`'s ten-step sequence (or a
  separate, explicit contractor action) creates the committed cost — this
  determines whether commitment is automatic-on-award or requires a
  deliberate "commit to costs" click, which is itself a product decision
  `phase1.md` doesn't make.
- **Idempotency:** a deterministic source key per created cost item, derived
  from `{companyID, projectID, awardRevisionID, awardedLineID}` (or
  `StableLineageID`), enforced by a unique index — following the
  `SourceAggregationKey` pattern, not a bare operationId check, since the
  downstream step can run more than once.
- **Lineage schema:** new nullable fields on `CostItem` (`SourceType`,
  `SourceAwardRevisionID`, `SourceAwardLineID` or similar), additive via
  `SchemaVersion`.
- **Correction handling:** increment-existing vs. new-row-per-revision, as
  above — needs an explicit product/engineering decision.
- **Missing context resolution:** how `MaterialID` and `ProjectID`/
  `WorkItemID` reach the bridge from `AwardedLine`, given neither is
  currently carried on the award side.
- **Purchase Order stage:** `phase1.md`'s own diagram inserts a PO
  acceptance step between Award and Committed Cost — worth an explicit call
  on whether Phase 1's eventual bridge skips PO entirely (Award → Committed
  directly) or whether PO is a real intermediate milestone.

## 5. Recommendation

Do not implement this bridge opportunistically as part of an unrelated bug
fix batch. It touches a schema change (`CostItem` lineage fields), a new
cross-module capability, and at least two undecided product questions
(trigger point, correction handling). Treat it as its own milestone-sized
design effort, scoped and reviewed on its own, consistent with how every
other cross-module capability in this codebase was introduced.
