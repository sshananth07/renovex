# Procurement Workflow Redesign — Design Spec

**Status:** Approved (pending user review of this written spec)
**Scope:** `apps/web/src/features/procurement/` — Material Requirement and RFQ
presentation logic. No backend changes.

## 1. Problem

The current Project Procurement page (`ProjectProcurement.tsx`) treats every
Material Requirement as immediately addable to an RFQ:

```tsx
const availableRequirements = useMemo(() => (requirements.data ?? []).filter(
  (item) => item.status === "open" || item.status === "draft"), [requirements.data]);
```

`"open"` is not a real `RequirementStatus` — the real enum is
`draft | reviewed | split | archived`
(`internal/materialrequirements/requirement.go:63-79`). Because the generated
OpenAPI type has `status: string` (not a literal union), TypeScript doesn't
catch this. In practice only `draft` requirements are ever shown as
"addable," and the backend's `IsRFQEligible()` predicate
(`requirement.go:243-289`) requires `Status == reviewed` — so **every "Add"
click was destined to 422**, exactly as reproduced against the live backend.

There is no UI call anywhere to `POST /material-requirements/{id}/review`,
`.../acknowledge-unit`, or `.../source-discrepancy/resolve`, even though the
generated OpenAPI types for all three already exist (`schema.ts`) — they are
simply unused. The requirement list also renders every record as an
indistinguishable pill (`Add cement (1 bag)` × 4), even when the four
underlying records are genuinely distinct. Separately,
`GET /rfq-chains/{id}/versions` legitimately 404s for any RFQ that hasn't
been through issuance yet, but the frontend feeds that error straight into
the page's generic error banner instead of treating it as "zero issued
versions."

## 2. Scope

**In scope:**
- Material Requirement presentation: draft / reviewed / claimed / terminal
  state rendering, driven by one pure state-derivation function.
- Requirement review action (`POST .../review`).
- Unit-mismatch acknowledgement (`POST .../acknowledge-unit`).
- Source-discrepancy detection, comparison, and resolution
  (`GET .../source-discrepancy`, `POST .../source-discrepancy/resolve`) —
  all four backend actions (`update`, `merge`, `keep_current`,
  `create_separate`).
- RFQ line addition (`POST /rfqs/{id}/lines`), correctly sequenced against
  fresh revisions after a review/acknowledge/resolve mutation.
- Requirement card/row redesign replacing the repeated-pill UI.
- Duplicate-requirement soft warning on manual creation (client-side only).
- Double-submit guards on every mutating action in this flow.
- RFQ readiness precheck (draft status, line count, delivery address) gating
  the "Mark ready" button.
- The `GET /rfq-chains/{id}/versions` 404-for-unissued-RFQ fix.
- Centralized error translation (`translateProcurementError`) for
  procurement-specific backend errors, on top of the existing field-error
  mapping (`applyFieldErrors`) from the prior request-validation hardening
  work.

**Explicitly deferred (separate follow-up work):**
- The active split-*creation* workflow (choosing batch counts, entering
  quantities, submitting a split, handling `splitState = creating`
  reconciliation via `POST .../split/reconcile`). No UI trigger exists today
  and no defect was reported against it.
- This redesign *does* render existing `split`-status and
  `splitState = creating` records correctly (read-only), so the card model
  doesn't need to be redesigned again when split creation is built later.

## 3. Architecture

```
                     BACKEND RESPONSE
                           │
          ┌────────────────┴────────────────┐
          │                                  │
MaterialRequirement                          RFQ
          │                                  │
          ▼                                  ▼
deriveRequirementUIState()        deriveRFQReadinessState()
          │                                  │
          ▼                                  ▼
RequirementActionState              RFQReadinessState
          │                                  │
          ▼                                  ▼
RequirementCard                        RFQ header/panel
          │                                  │
          ├── review mutation                ├── mark-ready mutation
          ├── acknowledge mutation            │
          ├── add-to-RFQ mutation              │
          │                                    │
          ├── resolve discrepancy               │
          │       ↓                             │
          │  ResolveDiscrepancyDialog             │
          │       ↓                              │
          │  lazy GET .../source-discrepancy       │
          │       ↓                                │
          │  resolution mutation                    │
          │                                          │
          └────────────────┬─────────────────────────┘
                            ↓
                  Query invalidation (procurementKeys)
                            ↓
                    fresh backend state
                            ↓
                   pure derivation again
```

Layer boundary:

| Layer | Owns | Never does |
|---|---|---|
| Backend | Eligibility enforcement, concurrency/revision guards, source-discrepancy computation, `availableActions`, RFQ claim invariants, RFQ readiness enforcement, issuance/versioning | — |
| Presentation derivation (`deriveRequirementUIState`, `deriveRFQReadinessState`) | Pure functions: authoritative-record → presentation state | API calls, mutation, React state, persistence |
| React components (`RequirementCard`, `RequirementBadges`, `RequirementPrimaryAction`, `ResolveDiscrepancyDialog`) | Render state, dispatch the one selected action | Business-rule decisions |
| Mutation/query layer | Call generated API client, invalidate via `procurementKeys`, map errors via `applyFieldErrors` then `translateProcurementError` | Re-implement backend eligibility logic |

## 4. Types

```ts
// --- Requirement presentation ---

type RequirementPresentationKind =
  | "archived" | "split-incomplete" | "split-source" | "claimed"
  | "source-discrepancy" | "unit-mismatch" | "draft" | "rfq-eligible" | "unavailable";

type RequirementBadgeType =
  | "archived" | "split" | "split-incomplete" | "superseded"
  | "claimed" | "draft" | "reviewed" | "rfq-ready"
  | "unit-mismatch" | "source-changed" | "source-removed" | "unavailable";

type RequirementBadge = { type: RequirementBadgeType; label: string };

type RequirementBlocker =
  | "terminal" | "claimed" | "split-incomplete"
  | "source-change-unresolved" | "unit-mismatch-unresolved" | "not-reviewed";

type RequirementPrimaryAction =
  | { type: "review"; requirementId: string; expectedRevision: number }
  | { type: "acknowledge-unit"; requirementId: string; expectedRevision: number }
  | { type: "resolve-discrepancy"; requirementId: string }
  | { type: "add-to-rfq"; requirementId: string; expectedRequirementRevision: number }
  | { type: "view-rfq"; rfqChainId: string }
  | { type: "view-split-children"; requirementId: string };

type RequirementActionState = {
  kind: RequirementPresentationKind;
  primaryAction: RequirementPrimaryAction | null;
  badges: RequirementBadge[];
  blockers: RequirementBlocker[];
  message?: string;
};

type RequirementUIContext = {
  workItemName?: string; // resolved by the parent page's Work Item lookup, never fetched by the card
  // The RFQ currently open in the page, if any. Used only to decide whether
  // "Add to RFQ" is offered on a reviewed+eligible requirement — the
  // "In {activeRfqNumber}" claimed badge always comes from the requirement's
  // own authoritative activeRfqNumber, never from this field, since a
  // claimed requirement may belong to a different chain than the one
  // currently selected on screen.
  selectedRfq?: { id: string; number: string; status: string; revision: number };
};

// --- RFQ readiness ---

type RFQReadinessBlocker = "not-draft" | "no-scope" | "missing-delivery-address";

type RFQReadinessState = {
  canAttemptMarkReady: boolean;
  blockers: RFQReadinessBlocker[];
  message?: string;
};

// --- Error translation ---

type ProcurementErrorKind =
  | "network" | "requirement-not-eligible" | "unit-mismatch-unresolved"
  | "source-discrepancy-unresolved" | "already-claimed" | "stale-revision"
  | "resolution-operation-conflict" | "validation" | "conflict" | "unknown";

type TranslatedProcurementError = { kind: ProcurementErrorKind; message: string };
```

`expectedRFQRevision` deliberately does **not** appear on
`RequirementPrimaryAction["add-to-rfq"]` — the requirement presentation state
is not coupled to a particular selected RFQ. The execution layer combines
`action.expectedRequirementRevision` with the currently-selected RFQ's own
`revision` when calling `POST /rfqs/{id}/lines`.

## 5. `deriveRequirementUIState` — precedence

A pure function, `(requirement: MaterialRequirement, context: RequirementUIContext) => RequirementActionState`.
Each branch returns immediately (no fallthrough), evaluated in this exact
order:

1. **`splitState === "creating"`** (checked first — the backend sets
   `status = split` and `splitState = creating` atomically before creating
   children, so an interrupted split always has both true; ordering by
   `splitState` first is what makes the incomplete state reachable at all)
   → `kind: "split-incomplete"`, `primaryAction: null`, badges `[Split incomplete]`,
     blockers `[terminal, split-incomplete]`, message: *"The requirement
     split did not complete and requires reconciliation."*

2. **`activeRfqChainId != null`** (claimed)
   → `kind: "claimed"`, `primaryAction: { type: "view-rfq", rfqChainId: activeRfqChainId }`,
     blockers `[claimed]`. Badges compose every applicable fact:
     `In {activeRfqNumber}` always; `Reviewed`/`Draft` from status; plus
     `Source changed`/`Source removed` if `sourceSyncState !== "clean"`; plus
     `Unit mismatch` if `unitMismatch && !unitMismatchAcknowledged`. **No
     resolve/acknowledge action is ever offered here** — claimed
     requirements are immutable to contractor mutation regardless of what
     else is true. (Checked before the discrepancy branch because a claim
     freezes every contractor field — `availableActions` returns nothing at
     all for a claimed anchor, per `internal/materialrequirements/resolution.go:130-133`.)

3. **`sourceSyncState !== "clean"`** (checked *before* the terminal-status
   branches below — a terminal anchor can still have a genuinely resolvable
   discrepancy: the backend's `availableActions` always offers `keep_current`
   on a terminal anchor, and offers `create_separate` too when the delta is
   positive; only `update`/`merge` are withheld because they would mutate
   terminal demand — `internal/materialrequirements/resolution.go:144-152`.
   Hiding this branch behind "archived"/"split" would make a legitimately
   resolvable discrepancy permanently unreachable in the UI.)
   → `kind: "source-discrepancy"`, `primaryAction: { type: "resolve-discrepancy", requirementId }`,
     blockers `[source-change-unresolved]` (plus `[terminal]` if
     `status` is `archived` or `split`, so the terminal fact is not lost).
     Badge/message depend on the specific sync state:
     - `change_detected` → badge `Source changed`, message *"The costing
       data behind this requirement has changed."*
     - `source_removed` → badge `Source removed`, message *"The costing
       data previously backing this requirement is no longer present."*

     Plus the current status badge (`Draft`/`Reviewed`/`Archived`/`Split`),
     so e.g. an archived anchor with a discrepancy shows `[Archived] [Source
     changed]` and the same `Review source change` action as a draft one —
     the dialog itself withholds `update`/`merge` for terminal anchors by
     rendering only `availableActions` (§7).

4. **`status === "archived"`** (discrepancy already resolved/clean)
   → `kind: "archived"`, `primaryAction: null`, badges `[Archived]`, blockers `[terminal]`

5. **`status === "split"`** (completed split source, discrepancy already
   resolved/clean)
   → `kind: "split-source"`, `primaryAction: { type: "view-split-children", requirementId }`,
     badges `[Split, Superseded]`, blockers `[terminal]`

6. **`unitMismatch && !unitMismatchAcknowledged`**
   → `kind: "unit-mismatch"`, `primaryAction: { type: "acknowledge-unit", requirementId, expectedRevision }`,
     badges `[Unit mismatch]` + status badge, blockers `[unit-mismatch-unresolved]`

7. **`status === "draft"`**
   → `kind: "draft"`, `primaryAction: { type: "review", requirementId, expectedRevision }`,
     badges `[Draft]`, blockers `[not-reviewed]`, message: *"This requirement
     needs review before procurement."*

8. **`status === "reviewed"`** (every condition above cleared)
   → `kind: "rfq-eligible"`, `primaryAction: { type: "add-to-rfq", requirementId, expectedRequirementRevision: revision }`
     when `context.selectedRfq` is a `draft`-status RFQ; otherwise
     `primaryAction: null` with message *"Select a draft RFQ to add this
     requirement."* (see §6's `RequirementUIContext` note). Badges `[Reviewed,
     Ready for RFQ]`, blockers `[]`.

9. **defensive fallback**
   → `kind: "unavailable"`, `primaryAction: null`, badges `[Unavailable]`, blockers `[]`

Table-driven unit tests must cover at minimum: every single-condition case
above; `draft + change_detected`; `reviewed + change_detected`; `reviewed +
unresolved unit mismatch`; `claimed + change_detected` (must show badge, not
action); `splitState=creating` with `status=split` (must resolve to
split-incomplete, not split-source); `source_removed` vs `change_detected`
badge/message distinction; **`archived + change_detected`** (must resolve to
`source-discrepancy` with `resolve-discrepancy` action, not `archived` with
no action); **`split + change_detected`** (same, resolves to
`source-discrepancy`, not `split-source`); **`claimed + change_detected`**
(must resolve to `claimed` — claim wins over discrepancy); `reviewed` with no
`context.selectedRfq` or a non-draft `selectedRfq` (must have `primaryAction:
null`, not `add-to-rfq`).

## 6. Component composition

```
RequirementsPage / ProcurementPanel
    │
    ├── owns the requirements query, the Work Item lookup map, mutation
    │   handlers, dialog open state, navigation
    │
    ▼
RequirementCard(requirement, context)
    │
    ├── const state = deriveRequirementUIState(requirement, context)
    ├── identity: material name, quantity + unit, specification,
    │   work item name (from context) or "Project-level requirement",
    │   source type label (see provenance rule below)
    ├── <RequirementBadges badges={state.badges} />
    ├── {state.message && <RequirementMessage>{state.message}</RequirementMessage>}
    └── <RequirementPrimaryAction action={state.primaryAction} pending={...} onAction={handleRequirementAction} />
```

- `RequirementBadges` renders each badge, styled by `type` (warning tone for
  `unit-mismatch`/`source-changed`/`source-removed`; success/neutral for
  `claimed`/`reviewed`/`rfq-ready`; muted for `archived`/`split`/`superseded`/`split-incomplete`).
- `RequirementPrimaryAction` takes exactly `{ action, pending, onAction }` —
  one callback, not one prop per action type. It is a dumb switch over
  `action.type` producing the right button label/disabled-state; renders
  nothing when `action` is `null`. `pending` disables the button and swaps
  the label to a "-ing…" form (`Reviewing…`, `Acknowledging…`, `Adding…`) —
  applies to every mutating action type, not navigation types
  (`view-rfq`/`view-split-children` never show pending state).
- The card **never fetches**. `workItemName` and `selectedRfq` are resolved
  once by the parent page and passed down via `context`. The card does not
  show exact accepted/proposed source numbers — that data only exists after
  the discrepancy dialog's lazy `GET`; the card's message stays generic
  ("The costing data... has changed").
- **Source type label**, checked in this order — provenance before raw type,
  since a `create_separate` child is stored with `sourceType: "manual"` but
  is system-derived, not contractor-entered, and showing it as plain
  "Manual" would be misleading:
  1. `createdFromDiscrepancyRequirementId` present → **"Created from source
     change"**
  2. else `sourceType === "manual"` → **"Manual"**
  3. else `sourceType === "cost_item"` → **"Generated"**
  4. else `sourceType === "split"` → **"Split batch"**
- `handleRequirementAction(action)` in the parent switches on `action.type`:
  `review`/`acknowledge-unit`/`add-to-rfq` fire the corresponding mutation;
  `resolve-discrepancy` opens `ResolveDiscrepancyDialog` for that
  requirement id; `view-rfq` selects/navigates to the claiming RFQ;
  `view-split-children` filters/navigates to child requirements
  (`splitFromRequirementId === this.id`, read-only list).

## 7. `ResolveDiscrepancyDialog`

Opened by the `resolve-discrepancy` action. On open: fetch
`GET /material-requirements/{id}/source-discrepancy` (loading skeleton while
in flight).

**Layout:**
- Title "Review source change", subtitle with material + work item/project-level.
- Explanatory line depending on `syncState`: `change_detected` → *"The
  costing data behind this requirement has changed. Review the difference
  before deciding how procurement should be updated."*; `source_removed` →
  *"The costing data previously backing this requirement is no longer
  present."*
- Accepted vs current comparison. For `change_detected`: two cards, "ACCEPTED
  SOURCE" (`accepted.quantity`, `accepted.costItemIds.length` cost items) →
  "CURRENT SOURCE" (`proposed.quantity`, `proposed.costItemIds.length` cost
  items). For `source_removed`: "CURRENT SOURCE" reads "No source cost
  items" / "0 cost items" rather than implying a business decision to zero
  the demand.
- Delta line: `Change: {delta} {unit}` where `delta = proposed.quantity − accepted.quantity`
  (exact decimal, see §15), plus "Your current procurement quantity:
  `{requirement.requiredQuantity}`". **Shown only for `change_detected`.**
  For `source_removed`, omit the `Change: ...` line entirely — a negative
  "Change: -33 bags" would imply the contractor's demand deliberately
  dropped to zero, when the actual fact is that the backing source rows
  disappeared, not that anyone accepted zero demand. The distinct
  explanatory copy above already communicates this.
- Radio-card list built **only** from `discrepancy.availableActions` —
  never a hardcoded four. Each rendered from a presentation lookup table
  keyed by action name (the lookup table may define all four; only the
  ones present in `availableActions` are rendered). `merge`'s resulting-quantity
  preview is computed/shown **only when `merge` is actually present in
  `availableActions`** — it's never computed speculatively, since the
  backend already encodes exactly when merge is arithmetically safe (unit
  compatibility, positive result) via `mergeAvailable`
  (`internal/materialrequirements/resolution.go:164-174`):
  - `update`: "Update requirement" / *"Replace `{current}` with the latest
    source quantity: `{proposed.quantity}`. The requirement returns to Draft
    for review."*
  - `merge`: "Merge source change" / *"Keep your adjustment and apply the
    source change. New quantity: `{merged}`. The requirement returns to
    Draft."* where `merged = current + (proposed.quantity − accepted.quantity)` (exact decimal)
  - `keep_current`: "Keep current requirement" / *"Keep `{current}`; accept
    the latest costing data as the new source baseline."*
  - `create_separate`: "Create separate requirement" / *"Keep this
    requirement at `{current}`; create another requirement for the
    additional `{delta}`."*

  All deltas/previews are computed in the source quantity's own unit — never
  converted to or from the requirement's unit (§15; the backend never
  converts units either).
- `[Cancel]` `[Apply resolution]` — Apply disabled until a radio is selected
  and while the mutation is pending.

**Idempotency for `create_separate`:** a `resolutionOperationId` (UUID) is
generated once when the contractor *selects* `create_separate` as their
radio choice, held in dialog state, and reused across retries of the same
logical attempt (e.g. a dropped connection). It is only regenerated when the
underlying fingerprint/delta actually changes (a fresh discrepancy GET after
a 409 discrepancy-changed, or the dialog being closed and reopened as a new
attempt) — never per submit click. `update`/`merge`/`keep_current` never
send an operation id unless the generated OpenAPI type requires the field.

**Submit payload** — field names taken from the generated
`resolveDiscrepancyInput`/`sourceDiscrepancyOutput` types, confirmed against
`internal/materialrequirements/handler.go:207-219`:
```
{
  action: selectedAction,
  expectedRevision: discrepancy.requirementRevision,
  expectedProposedFingerprint: discrepancy.proposedFingerprint, // top-level field, not proposed.fingerprint
  resolutionOperationId: selectedAction === "create_separate" ? currentOperationId : undefined,
}
```

**Error handling — sentinel-specific, not "treat every 409 as stale":**

| Backend condition | UI behavior |
|---|---|
| `ErrMaterialRequirementDiscrepancyChanged` (409, proposal fingerprint stale) | Dialog stays open. Refetch the discrepancy GET. Clear the radio selection. Clear/regenerate the `create_separate` operation id. Show: *"Source data changed while you were reviewing it. Please review the latest values before applying a resolution."* Replace the accepted/current comparison with fresh values. |
| `ErrRevisionMismatch` (409, requirement revision stale) | Refetch the requirement + discrepancy. If still resolvable (not claimed/terminal in the meantime), let the contractor review the fresh state and re-choose. Otherwise close the dialog and let the card re-derive (e.g. to `claimed` or `archived`). |
| `ErrMaterialRequirementAlreadyClaimed` (409, became claimed while dialog was open) | Close the dialog. Invalidate the requirements query. The card re-derives to `claimed`, surfacing `view-rfq` as the next action. |
| `ErrResolutionOperationConflict` (409, operation id belongs to a different resolution) | Do **not** silently regenerate and retry. Surface via `translateProcurementError`'s dedicated `"resolution-operation-conflict"` kind (§9); require the contractor to cancel and reopen. |
| Any other error | `applyFieldErrors` first (if field-scoped), then `translateProcurementError` as the form-level fallback. |

**Success:** close dialog, invalidate `procurementKeys.requirements(projectId)`.
No local state is set to predict the resulting card — the next render's
`deriveRequirementUIState` call on the refetched record naturally produces
the right next action (`update`/`merge` → back to `draft` → `Review
requirement`; `keep_current` on a previously-`reviewed` record → straight to
`Add to RFQ` if nothing else blocks; `create_separate` → original requirement
becomes clean, a new sibling draft requirement appears in the list carrying
the discrepancy provenance).

## 8. `deriveRFQReadinessState`

```ts
function deriveRFQReadinessState(rfq: RFQ): RFQReadinessState {
  if (rfq.status !== "draft") {
    return { canAttemptMarkReady: false, blockers: ["not-draft"] };
  }
  if ((rfq.lines?.length ?? 0) === 0) {
    return {
      canAttemptMarkReady: false, blockers: ["no-scope"],
      message: "Add at least one reviewed material requirement to this RFQ first.",
    };
  }
  if (!rfq.deliveryAddress?.trim()) {
    return {
      canAttemptMarkReady: false, blockers: ["missing-delivery-address"],
      message: "Add a delivery address before marking this RFQ ready.",
    };
  }
  return { canAttemptMarkReady: true, blockers: [] };
}
```

Covers exactly the three preconditions locally knowable from the RFQ
projection alone (status, line count, delivery address) — matching
`RFQ.ReadinessErr()` (`internal/rfqs/rfq.go:163-181`). Deliberately does
**not** attempt to re-derive per-line eligibility (each line's underlying
requirement's claim/sync/unit-mismatch state) — that would require N extra
fetches and would duplicate `ClaimedIsReadyForRFQ`, which the backend
re-validates authoritatively at transition time regardless
(`internal/rfqs/service.go:208-249`). "Mark ready" is disabled with the
message when `canAttemptMarkReady` is false; when true, the click still goes
to the backend, and a genuine `ErrRFQLineNotEligible` 422 from a
since-changed line is handled by `translateProcurementError`, not silently
retried or hidden.

## 9. `translateProcurementError`

Applies **after** field-scoped error mapping, never instead of it:

```
API failure
   ↓
Does the Huma response contain field-scoped errors (ErrorDetail[])?
   ↓ yes                              ↓ no
map via applyFieldErrors/setError     translateProcurementError() → form-level message
```

```ts
function translateProcurementError(error: ApiError): TranslatedProcurementError {
  if (error.kind !== "api") {
    return { kind: "network", message: "The request could not be completed. Check your connection and try again." };
  }
  const detail = error.detail ?? "";

  // Specific matches first, most specific to least — so a future, more
  // precise backend message is never swallowed by the broad "not eligible"
  // catch-all below.
  if (detail.includes("resolution operation id belongs to a different resolution")) {
    return { kind: "resolution-operation-conflict", message: "This source-change resolution no longer matches the current requirement. Close this review and start again." };
  }
  if (detail.includes("unit mismatch is unresolved")) {
    return { kind: "unit-mismatch-unresolved", message: "Acknowledge the unit mismatch before adding this requirement to the RFQ." };
  }
  if (detail.includes("source discrepancy is unresolved")) {
    return { kind: "source-discrepancy-unresolved", message: "Review the source change before adding this requirement to the RFQ." };
  }
  if (detail.includes("already claimed")) {
    return { kind: "already-claimed", message: "This requirement is already assigned to another RFQ." };
  }
  if (detail.includes("changed since it was read")) {
    return { kind: "stale-revision", message: "This record changed since you opened it. Review the latest data before retrying." };
  }
  // Broad eligibility catch-all — last among the specific matches, since
  // "not eligible" is the least specific known message.
  if (detail.includes("requirement is not eligible") || detail.includes("not eligible for an RFQ")) {
    return { kind: "requirement-not-eligible", message: "This material requirement is not currently eligible for an RFQ. Review its current status before retrying." };
  }
  if (error.status === 422) return { kind: "validation", message: error.detail ?? "This action could not be completed." };
  if (error.status === 409) return { kind: "conflict", message: "The record may have changed. Review the current state before retrying." };
  return { kind: "unknown", message: error.detail ?? "The action could not be completed." };
}
```

Design notes:
- **Specific-before-generic ordering** — every specific `detail` match is
  checked before the broad "not eligible" catch-all, so a more precise
  backend message is never accidentally matched by the generic branch first.
- **Deliberately generic for the eligibility case** — "not eligible" can
  mean not-reviewed, unresolved unit mismatch, unresolved source
  discrepancy, wrong/no claim, or non-positive quantity. The translator does
  not guess which; wherever the card already knows the specific cause (it
  almost always does, since `deriveRequirementUIState` ran on the same
  data), the card's own derived message is preferred and this generic
  fallback is only reached for errors whose cause wasn't already known
  locally (e.g. a race where the record changed between render and submit).
- **Side-effect-free** — never claims a refetch happened ("the latest data
  has been loaded"); the calling mutation handler decides whether and when
  to invalidate/refetch.
- Matches on `error.detail` (the only text the backend currently exposes —
  confirmed no stable machine-readable error codes exist, every mapping is
  `huma.Error4xx("string literal")`). This string-matching is intentionally
  isolated inside this one function so it can move to structured codes
  later without touching call sites.

## 10. Duplicate-creation warning (manual requirement creation)

Advisory only — no backend support exists or will be added (confirmed: no
`materialId` filter on `RequirementFilter`, no similarity endpoint; adding a
DB uniqueness constraint would make legitimate scenarios like "20 bags for
Kitchen" + "15 bags for Bathroom" or two required-by dates for separate
procurement batches impossible).

**Matching key:** `materialId` + normalized `workItemId` (`id ?? null`, so
`undefined`/`null` both mean project-level) + trimmed `quantityUnit`,
compared against every **non-terminal** requirement already loaded for the
project (not manual-only — a generated Cement/Kitchen/bag requirement is
still worth warning about against a new manual one with the same key). No
unit conversion — different units are always distinct demand per the
backend's own design.

**UX:** on submit, before mutating, compute the candidate key from the
current form values and check for a match. If found, show an inline warning
panel (not a blocking modal) above the submit button: *"A similar
requirement already exists"* + the existing requirement's summary (material,
quantity, specification, status) + two actions: **"View existing"** (closes
the create dialog; the requirement list already shows it) and **"Create
another anyway"** (proceeds with the original submit).

**Bypass scoping:** the "Create another anyway" approval is tied to the
exact candidate key reviewed, not the dialog session:
```ts
const duplicateCandidateKey = [materialId, workItemId ?? "", quantityUnit.trim()].join("|");
```
Store `approvedDuplicateKey` in dialog state when the contractor clicks
"Create another anyway." If any of `materialId`/`workItemId`/`quantityUnit`
changes afterward, `approvedDuplicateKey !== currentDuplicateKey` and the
check runs again — changing Material to "Tiles" after approving a Cement
duplicate does not silently also approve a Tiles duplicate.

## 11. Double-submit guards

Every mutating primary/confirmation action in this flow disables itself
while its own mutation is pending and shows an in-progress label:

| Action | Idle label | Pending label |
|---|---|---|
| Create Requirement | "Create requirement" | "Creating…" |
| Review Requirement | "Review requirement" | "Reviewing…" |
| Acknowledge Unit | "Acknowledge unit" | "Acknowledging…" |
| Add to RFQ | "Add to RFQ" | "Adding…" |
| Apply discrepancy resolution | "Apply resolution" | "Applying…" |
| Mark ready | "Mark ready" | "Marking ready…" |
| Create RFQ | "Create draft RFQ" | "Creating…" |

**Create RFQ is called out specifically**: RFQ creation is not idempotent
(a retry allocates a new RFQ number and creates a second empty draft), so
this guard plus the absence of any automatic retry-on-ambiguous-failure is
load-bearing, not cosmetic — unlike the duplicate-requirement warning, which
is UX guidance rather than a correctness guarantee.

**Add-to-RFQ must be serialized per selected RFQ, not just per requirement.**
`POST /rfqs/{id}/lines` conditionally appends a line against the supplied
`expectedRfqRevision`. Two different requirement cards targeting the *same*
RFQ can otherwise both read `revision = 4`, both submit
`expectedRfqRevision: 4`, the first succeeds and advances the RFQ to
`revision = 5`, and the second now submits a stale revision it could have
avoided — the backend rejects it safely, but the conflict was avoidable
UI-side. Rule: **while an Add-to-RFQ mutation is pending for RFQ X, disable
every other Add-to-RFQ action targeting RFQ X** (keyed on
`context.selectedRfq.id`, not on the individual requirement id — this is in
addition to, not instead of, each card's own per-requirement pending state).
On success, the mutation handler must `await` both the requirements
invalidation *and* the RFQ invalidation (so the fresh `revision` is in the
cache) before re-enabling Add-to-RFQ actions for that RFQ — do not clear the
pending lock on the mutation's own promise resolving if invalidation is
fired-and-forgotten separately, since that would let a second submit race
ahead of the refetch.

The same await-before-clearing-pending principle applies to Review /
Acknowledge Unit / Resolve Discrepancy: since a subsequent action
(`add-to-rfq`) depends on the *fresh* `expectedRequirementRevision` produced
by these mutations, the mutation's pending state must not clear until the
requirements query invalidation/refetch has actually resolved — otherwise a
card could momentarily render its *old* action (still showing "Review
requirement" or a stale revision) between the mutation settling and the
refetch completing.

## 12. `procurementKeys` — query-key factory

```ts
const procurementKeys = {
  requirements: (projectId: string) => ["projects", projectId, "requirements"] as const,
  sourceDiscrepancy: (requirementId: string) => ["material-requirements", requirementId, "source-discrepancy"] as const,
  rfqs: (projectId: string) => ["projects", projectId, "rfqs"] as const,
  rfq: (rfqId: string) => ["rfqs", rfqId] as const,
  rfqVersions: (rfqChainId: string) => ["rfq-chains", rfqChainId, "versions"] as const,
};
```

`sourceDiscrepancy(requirementId)` is the `ResolveDiscrepancyDialog`'s own
query key for its lazy `GET .../source-discrepancy` (§7) — used both for the
initial open-fetch and the refetch-on-409-stale path. No separate
single-requirement detail key is defined: nothing in this design reads one
requirement by id outside the list (`requirements(projectId)`) and the
discrepancy dialog (`sourceDiscrepancy`), so an unused `requirement(id)` key
is intentionally omitted rather than speculatively added.

Mutations invalidate according to what they actually changed, not a broad
`invalidateQueries()`:

| Mutation | Invalidates |
|---|---|
| Review / Acknowledge unit | `requirements(projectId)` |
| Resolve discrepancy | `requirements(projectId)`, `sourceDiscrepancy(requirementId)` |
| Add to RFQ | `requirements(projectId)`, `rfq(rfqId)` (or the RFQs list query the page already uses) |
| Remove RFQ line *(if/when it exists in this UI)* | `requirements(projectId)`, `rfq(rfqId)` |
| Mark ready | `rfq(rfqId)` and the RFQ list query if status is shown there |
| Create RFQ | `rfqs(projectId)` |

## 13. `GET /rfq-chains/{id}/versions` 404 fix

Confirmed backend behavior (`internal/rfqissuance/reconciliation.go:7-20`):
the `RFQIssuanceChain` document is created only inside `IssueVersion`
(`internal/rfqissuance/service.go:318`), i.e. only on first successful
issuance. A draft or ready-but-never-issued RFQ legitimately has no chain
document, so **404 is correct, expected backend behavior — not a bug to
route around on the backend.** `rfqChainId` and `RFQ.id` are the same value
everywhere (`internal/rfqs/rfq.go:112`).

The frontend fix: `useIssuedVersions` currently feeds its error straight
into the page's aggregate `queryError` banner
(`ProjectProcurement.tsx:78`), so selecting or creating any never-issued RFQ
today shows "Project procurement data could not be loaded." Fix:
- Do **not** disable/gate the query behind an `enabled: hasBeenIssued` flag
  computed from a status the frontend doesn't reliably have pre-issuance —
  the API's own 404 already communicates "no versions yet" accurately.
- **Normalize the 404 to successful empty data inside the query function
  itself**, not by retaining React Query's error state and merely hiding it
  from the banner:
  ```ts
  async function listIssuedVersions(rfqChainId: string): Promise<IssuedVersion[]> {
    try {
      return await fetchIssuedVersions(rfqChainId); // existing apiClient call
    } catch (error) {
      if (isApiError(error) && error.status === 404) return [];
      throw error;
    }
  }
  ```
  This makes `useIssuedVersions(...).data === []` the natural "no issued
  versions yet" state — the issued-versions panel renders *"No issued
  versions yet"* whenever `data` is an empty array, with no special-casing
  needed in the component, no lingering React Query error state, and no
  wasted automatic error retries against an outcome that will never change
  until the RFQ is actually issued.
- Do not fabricate a placeholder "Version 1" or any dummy version record —
  the empty state is exactly that: empty.

## 14. Non-goals (reconfirmed)

- No backend changes of any kind — every fix above is presentation-layer or
  client-side-only advisory logic.
- No backend uniqueness constraint for manual requirements.
- No active split-creation UI (batch count entry, quantity split, `POST
  .../split`, `POST .../split/reconcile`) — only read-only display of
  existing `split`/`split-incomplete` records, so the card model doesn't
  need a second redesign later.
- No re-implementation of backend RFQ-line eligibility or Mark-Ready
  authoritative checks on the frontend — `deriveRFQReadinessState` only
  covers what's locally knowable (status, line count, delivery address).
- No change to Money/quantity handling elsewhere in the app — quantity
  arithmetic inside the discrepancy dialog uses a narrowly scoped new
  exact-decimal helper (§15), not a new dependency, since only two
  operations (subtraction for delta, addition for merge) are needed and no
  decimal library exists in the frontend today.

## 15. Quantity arithmetic in the discrepancy dialog

No decimal-arithmetic library or helper currently exists in
`apps/web` (confirmed: no `decimal`/`Decimal` references anywhere in
`src/lib` or `package.json`). Quantities are exact decimal strings
end-to-end (mirroring the backend's `shopspring/decimal` usage), so the
dialog's delta/merge previews must not use `Number(...)` arithmetic —
**`Number()` must not appear anywhere in this helper's implementation.**

Add a narrowly scoped `lib/formatting/quantityMath.ts` with exactly the two
operations needed:
```ts
function subtractQuantity(a: string, b: string): string; // a - b, exact
function addQuantity(a: string, b: string): string;       // a + b, exact
```

**Implementation approach — BigInt with scale alignment, no floating point
at any step:**
1. Parse each operand into `{ sign, digits: bigint, scale: number }` by
   splitting on `.`, stripping the sign, and treating the fractional part's
   length as the scale (e.g. `"35.20"` → digits `3520n`, scale `2`; `"8"` →
   digits `8n`, scale `0`).
2. Align both operands to the larger of the two scales by multiplying the
   smaller-scale operand's digits by the appropriate power of ten (as a
   `BigInt` multiplication, e.g. `× 10n ** BigInt(scaleDiff)`).
3. Perform the add/subtract as a single `BigInt` operation on the
   scale-aligned digit values (respecting sign).
4. Format the `BigInt` result back to a canonical decimal string at the
   aligned scale — reinsert the decimal point, restore the sign (a `-0`
   result normalizes to `"0"`), and strip only the trailing zeros that are
   not significant to the aligned scale (do not strip zeros that change the
   number of decimal places below what either operand specified, to avoid
   silently changing displayed precision).

**Required unit tests** (`quantityMath.test.ts`), covering exact-arithmetic
correctness and formatting edge cases — this is the canonical list, not a
minimum-effort subset:
- `addQuantity("0.1", "0.2")` → `"0.3"` (the canonical floating-point trap:
  `0.1 + 0.2` in native JS floats is `0.30000000000000004`)
- `subtractQuantity("1.00", "0.75")` → `"0.25"`
- `subtractQuantity("41", "33")` → `"8"`
- `addQuantity("35", "8")` → `"43"`
- `subtractQuantity("30", "33")` → `"-3"`
- `addQuantity("35", "-3")` → `"32"`
- `addQuantity("100000000000000000000.1", "0.2")` → `"100000000000000000000.3"`
  (arbitrary precision — beyond `Number.MAX_SAFE_INTEGER`)
- operands with different decimal scales (e.g. `"1.5"` + `"0.125"` → `"1.625"`)
- leading/trailing zero normalization (e.g. `"007.10"` + `"0"` → `"7.1"`,
  not `"007.10"` or `"7.10"`)
- a result of exactly zero from a negative-sign input normalizes to `"0"`,
  never `"-0"`

The dialog scopes these two operations to exactly what its three preview
formulas need — `delta = subtractQuantity(proposed, accepted)`,
`merged = addQuantity(current, delta)`, `create_separate`'s new-requirement
quantity `= delta` directly — and always operates in the **source
quantity's own unit** (never the requirement's display unit, and never with
any unit conversion — the backend performs none either).
