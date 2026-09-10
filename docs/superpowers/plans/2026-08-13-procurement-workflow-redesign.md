# Procurement Workflow Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the broken Material Requirement "Add" pill workflow with state-aware procurement cards, review/unit/source-discrepancy actions, safe RFQ readiness and mutation sequencing, advisory duplicate protection, and correct pre-issuance versions handling without changing backend behavior.

**Architecture:** Keep the Go/Huma backend authoritative. Add pure frontend presentation derivation (`deriveRequirementUIState`, `deriveRFQReadinessState`), thin generated-OpenAPI-backed API wrappers/query hooks, dumb presentation components, and mutation orchestration that always refetches authoritative state before exposing the next action. Source-discrepancy details are fetched lazily in a dedicated dialog; exact quantity previews use a narrow BigInt+scale decimal helper.

**Tech Stack:** Next.js/React/TypeScript, TanStack Query, React Hook Form + Zod where the existing form already uses them, openapi-fetch + generated OpenAPI types, Vitest + React Testing Library, existing shadcn/ui primitives.

**Source spec:** `docs/superpowers/specs/2026-08-13-procurement-workflow-redesign-design.md`

## Global Constraints

- **Frontend-only change.** Do not modify Go backend services, handlers, schemas, or domain rules.
- Generated OpenAPI remains transport truth; do not hand-edit `apps/web/src/lib/api/generated/schema.ts`.
- Preserve the existing `unwrapOrThrow` / `applyFieldErrors` error model; field-scoped Huma errors are mapped before workflow-level translation.
- Do not add a second validation framework or duplicate backend eligibility logic.
- Do not use JavaScript `Number`/float arithmetic for authoritative or preview quantity math.
- Do not add a decimal dependency; use the narrowly scoped BigInt+scale helper specified in Task 1.
- Do not add backend uniqueness for manual Material Requirements; duplicate detection is advisory only.
- Do not implement split creation or split reconciliation UI. Existing split/split-incomplete records are display-only.
- Terminal anchors with unresolved source state remain discrepancy-resolvable where the backend permits `keep_current` / positive-delta `create_separate`.
- `create_separate` uses one stable `resolutionOperationId` per logical attempt and reuses it across ambiguous retries.
- RFQ creation has no automatic retry. Every mutating action disables its own trigger while pending.
- Add-to-RFQ is serialized per selected RFQ: while one add is pending, every Add-to-RFQ action targeting that RFQ is disabled until requirement + RFQ refetches complete.
- Query invalidations/refetches are awaited before pending state clears when the next legal action depends on a fresh revision.
- Leave all changes uncommitted. Do not run `git commit`.

---

## File Structure

Create/modify these focused units; if the repository contains an equivalent existing file under the same feature, extend that file rather than creating a parallel duplicate abstraction.

```text
apps/web/src/features/procurement/
├── ProjectProcurement.tsx                 # integration/orchestration only
├── api.ts                                 # thin generated-OpenAPI wrappers
├── queries.ts                             # list/detail/discrepancy/version queries
├── mutations.ts                           # review/ack/add/resolve/ready/create mutations
├── queryKeys.ts                           # procurementKeys factory
├── requirementPresentation.ts             # pure requirement action-state derivation
├── requirementPresentation.test.ts
├── rfqPresentation.ts                     # pure Mark-ready precheck
├── rfqPresentation.test.ts
├── procurementErrors.ts                   # workflow-level error translation
├── procurementErrors.test.ts
├── requirementSimilarity.ts               # duplicate-warning key/match logic
├── requirementSimilarity.test.ts
└── components/
    ├── RequirementBadges.tsx
    ├── RequirementPrimaryAction.tsx
    ├── RequirementCard.tsx
    ├── RequirementCard.test.tsx
    ├── ResolveDiscrepancyDialog.tsx
    └── ResolveDiscrepancyDialog.test.tsx

apps/web/src/lib/formatting/
├── quantityMath.ts
└── quantityMath.test.ts
```

`ProjectProcurement.tsx` should become smaller, not absorb the new business/presentation logic.

---

### Task 1: Exact quantity arithmetic helper

**Files:**
- Create: `apps/web/src/lib/formatting/quantityMath.ts`
- Test: `apps/web/src/lib/formatting/quantityMath.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export function addQuantity(a: string, b: string): string;
  export function subtractQuantity(a: string, b: string): string;
  ```
- Consumed by Task 6 only for discrepancy previews.

- [ ] **Step 1: Write the failing exact-arithmetic tests**

Create `quantityMath.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { addQuantity, subtractQuantity } from "./quantityMath";

describe("quantityMath", () => {
  it.each([
    ["0.1", "0.2", "0.3"],
    ["35", "8", "43"],
    ["35", "-3", "32"],
    ["1.20", "0.03", "1.23"],
    ["100000000000000000000.1", "0.2", "100000000000000000000.3"],
  ])("adds %s + %s exactly", (a, b, expected) => {
    expect(addQuantity(a, b)).toBe(expected);
  });

  it.each([
    ["1.00", "0.75", "0.25"],
    ["41", "33", "8"],
    ["30", "33", "-3"],
    ["1.2300", "0.23", "1"],
    ["0", "0.000", "0"],
  ])("subtracts %s - %s exactly", (a, b, expected) => {
    expect(subtractQuantity(a, b)).toBe(expected);
  });
});
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run from `apps/web/`:

```bash
npm run test -- src/lib/formatting/quantityMath.test.ts
```

Expected: FAIL because `quantityMath.ts` does not exist.

- [ ] **Step 3: Implement BigInt + scale alignment, with no Number arithmetic**

Create `quantityMath.ts`:

```ts
type ParsedDecimal = { value: bigint; scale: number };

const DECIMAL_RE = /^([+-]?)(\d+)(?:\.(\d+))?$/;

function parseDecimal(input: string): ParsedDecimal {
  const text = input.trim();
  const match = DECIMAL_RE.exec(text);
  if (!match) throw new Error(`Invalid decimal quantity: ${input}`);

  const [, sign, whole, fraction = ""] = match;
  const digits = `${whole}${fraction}`.replace(/^0+(?=\d)/, "");
  const unsigned = BigInt(digits || "0");
  return {
    value: sign === "-" ? -unsigned : unsigned,
    scale: fraction.length,
  };
}

function pow10(exp: number): bigint {
  return 10n ** BigInt(exp);
}

function align(a: ParsedDecimal, b: ParsedDecimal): [bigint, bigint, number] {
  const scale = Math.max(a.scale, b.scale);
  return [
    a.value * pow10(scale - a.scale),
    b.value * pow10(scale - b.scale),
    scale,
  ];
}

function formatDecimal(value: bigint, scale: number): string {
  if (value === 0n) return "0";

  const negative = value < 0n;
  let digits = (negative ? -value : value).toString();

  if (scale > 0) {
    digits = digits.padStart(scale + 1, "0");
    const split = digits.length - scale;
    const whole = digits.slice(0, split);
    const fraction = digits.slice(split).replace(/0+$/, "");
    digits = fraction ? `${whole}.${fraction}` : whole;
  }

  digits = digits.replace(/^0+(?=\d)/, "");
  return negative ? `-${digits}` : digits;
}

export function addQuantity(a: string, b: string): string {
  const [left, right, scale] = align(parseDecimal(a), parseDecimal(b));
  return formatDecimal(left + right, scale);
}

export function subtractQuantity(a: string, b: string): string {
  const [left, right, scale] = align(parseDecimal(a), parseDecimal(b));
  return formatDecimal(left - right, scale);
}
```

- [ ] **Step 4: Run the focused tests and typecheck**

```bash
npm run test -- src/lib/formatting/quantityMath.test.ts
npm run typecheck
```

Expected: PASS.

- [ ] **Step 5: Review gate**

```bash
git diff --check -- apps/web/src/lib/formatting/quantityMath.ts apps/web/src/lib/formatting/quantityMath.test.ts
```

Expected: no whitespace errors; no commit.

---

### Task 2: Procurement query keys and pure Requirement presentation state

**Files:**
- Create: `apps/web/src/features/procurement/queryKeys.ts`
- Create: `apps/web/src/features/procurement/requirementPresentation.ts`
- Test: `apps/web/src/features/procurement/requirementPresentation.test.ts`

**Interfaces:**
- Produces `procurementKeys`:
  ```ts
  requirements(projectId)
  rfqs(projectId)
  rfq(rfqId)
  rfqVersions(rfqChainId)
  sourceDiscrepancy(requirementId)
  ```
- Produces `RequirementActionState`, `RequirementPrimaryAction`, `RequirementBadgeType`, `RequirementUIContext`, `deriveRequirementUIState()`.
- Consumed by Tasks 4-9.

- [ ] **Step 1: Add the query-key factory**

Create `queryKeys.ts`:

```ts
export const procurementKeys = {
  requirements: (projectId: string) => ["projects", projectId, "requirements"] as const,
  rfqs: (projectId: string) => ["projects", projectId, "rfqs"] as const,
  rfq: (rfqId: string) => ["rfqs", rfqId] as const,
  rfqVersions: (rfqChainId: string) => ["rfq-chains", rfqChainId, "versions"] as const,
  sourceDiscrepancy: (requirementId: string) => ["material-requirements", requirementId, "source-discrepancy"] as const,
};
```

- [ ] **Step 2: Write the full table-driven precedence tests before implementation**

Create `requirementPresentation.test.ts`. Use the existing feature `MaterialRequirement` transport type/factory used by `ProjectProcurement.tsx`; do not create a duplicate network DTO. The table must assert at least:

```ts
const draftSelectedRfq = { id: "rfq-1", number: "RFQ-000001", status: "draft", revision: 2 };

// minimum cases
// splitState=creating + status=split -> split-incomplete
// claimed + change_detected -> claimed/view-rfq, source badge visible, no resolve action
// archived + clean -> archived/no action
// split + clean -> split-source/view children
// archived + change_detected -> source-discrepancy with terminal blocker
// split + change_detected -> source-discrepancy with terminal blocker
// draft + change_detected -> source-discrepancy
// reviewed + source_removed -> source-discrepancy/source-removed badge
// unresolved unit mismatch -> acknowledge-unit
// draft + clean -> review
// reviewed + clean + selected draft RFQ -> add-to-rfq
// reviewed + clean + no selected RFQ -> rfq-eligible, no action, selection message
// reviewed + clean + selected ready RFQ -> rfq-eligible, no action, selection message
// fallback -> unavailable
```

For claimed state, assert the `claimed` badge label is derived from the requirement's `activeRfqNumber`, never `context.selectedRfq.number`.

- [ ] **Step 3: Run RED**

```bash
npm run test -- src/features/procurement/requirementPresentation.test.ts
```

Expected: FAIL because presentation module does not exist.

- [ ] **Step 4: Implement the presentation types and ordered derivation**

Create `requirementPresentation.ts` with these exact presentation-only unions:

```ts
export type RequirementPresentationKind =
  | "split-incomplete"
  | "claimed"
  | "source-discrepancy"
  | "archived"
  | "split-source"
  | "unit-mismatch"
  | "draft"
  | "rfq-eligible"
  | "unavailable";

export type RequirementBadgeType =
  | "archived" | "split" | "split-incomplete" | "superseded"
  | "claimed" | "draft" | "reviewed" | "rfq-ready"
  | "unit-mismatch" | "source-changed" | "source-removed" | "unavailable";

export type RequirementBlocker =
  | "terminal" | "claimed" | "split-incomplete"
  | "source-change-unresolved" | "unit-mismatch-unresolved" | "not-reviewed";

export type RequirementPrimaryAction =
  | { type: "review"; requirementId: string; expectedRevision: number }
  | { type: "acknowledge-unit"; requirementId: string; expectedRevision: number }
  | { type: "resolve-discrepancy"; requirementId: string }
  | { type: "add-to-rfq"; requirementId: string; expectedRequirementRevision: number }
  | { type: "view-rfq"; rfqChainId: string }
  | { type: "view-split-children"; requirementId: string };

export type RequirementActionState = {
  kind: RequirementPresentationKind;
  primaryAction: RequirementPrimaryAction | null;
  badges: { type: RequirementBadgeType; label: string }[];
  blockers: RequirementBlocker[];
  message?: string;
};

export type RequirementUIContext = {
  workItemName?: string;
  selectedRfq?: { id: string; number: string; status: string; revision: number };
};
```

Implement this precedence exactly:

```text
1. splitState === "creating"
2. activeRfqChainId != null
3. sourceSyncState !== "clean"
4. status === "archived"
5. status === "split"
6. unitMismatch && !unitMismatchAcknowledged
7. status === "draft"
8. status === "reviewed"
9. fallback
```

Rules for branch 3:
- `change_detected` => `source-changed` badge and changed-source copy.
- `source_removed` => `source-removed` badge and source-removed copy.
- If anchor status is `archived` or `split`, also add `terminal` blocker and corresponding terminal badge(s), but still expose `resolve-discrepancy` because the backend can return terminal-safe actions.

Rules for branch 8:
- `kind` remains `rfq-eligible` when all requirement-level blockers are clear.
- Only return `add-to-rfq` when `context.selectedRfq?.status === "draft"`.
- Otherwise return `primaryAction: null` and message `"Select a draft RFQ to add this requirement."`.

- [ ] **Step 5: Run tests and typecheck**

```bash
npm run test -- src/features/procurement/requirementPresentation.test.ts
npm run typecheck
```

Expected: PASS.

- [ ] **Step 6: Review gate**

```bash
git diff --check -- apps/web/src/features/procurement/queryKeys.ts apps/web/src/features/procurement/requirementPresentation.ts apps/web/src/features/procurement/requirementPresentation.test.ts
```

---

### Task 3: RFQ readiness derivation and procurement error translation

**Files:**
- Create: `apps/web/src/features/procurement/rfqPresentation.ts`
- Test: `apps/web/src/features/procurement/rfqPresentation.test.ts`
- Create: `apps/web/src/features/procurement/procurementErrors.ts`
- Test: `apps/web/src/features/procurement/procurementErrors.test.ts`

**Interfaces:**
- Produces `deriveRFQReadinessState(rfq)`.
- Produces side-effect-free `translateProcurementError(error)`.
- Consumed by Tasks 6-9.

- [ ] **Step 1: Write RFQ readiness tests**

Cover:

```text
ready/non-draft -> not-draft, cannot attempt
empty draft -> no-scope + "Add at least one reviewed material requirement..."
draft with line but blank/whitespace deliveryAddress -> missing-delivery-address
draft with line + address -> canAttemptMarkReady=true
```

- [ ] **Step 2: Implement `deriveRFQReadinessState`**

```ts
export type RFQReadinessBlocker = "not-draft" | "no-scope" | "missing-delivery-address";

export type RFQReadinessState = {
  canAttemptMarkReady: boolean;
  blockers: RFQReadinessBlocker[];
  message?: string;
};

export function deriveRFQReadinessState(rfq: RFQ): RFQReadinessState {
  if (rfq.status !== "draft") return { canAttemptMarkReady: false, blockers: ["not-draft"] };
  if ((rfq.lines?.length ?? 0) === 0) {
    return { canAttemptMarkReady: false, blockers: ["no-scope"], message: "Add at least one reviewed material requirement to this RFQ first." };
  }
  if (!rfq.deliveryAddress?.trim()) {
    return { canAttemptMarkReady: false, blockers: ["missing-delivery-address"], message: "Add a delivery address before marking this RFQ ready." };
  }
  return { canAttemptMarkReady: true, blockers: [] };
}
```

Do not inspect/fetch underlying requirements here.

- [ ] **Step 3: Write procurement error translation tests**

Test exact precedence:

```text
non-api -> network
resolution-operation-conflict -> resolution-operation-conflict
unit mismatch detail -> unit-mismatch-unresolved
source discrepancy detail -> source-discrepancy-unresolved
already claimed detail -> already-claimed
changed-since-read detail -> stale-revision
specific cases above must win before broad requirement-not-eligible
requirement-not-eligible -> generic eligibility message
remaining 422 -> validation/detail
remaining 409 -> conflict generic
other -> unknown/detail
```

- [ ] **Step 4: Implement typed, side-effect-free translator**

```ts
export type ProcurementErrorKind =
  | "network"
  | "resolution-operation-conflict"
  | "requirement-not-eligible"
  | "unit-mismatch-unresolved"
  | "source-discrepancy-unresolved"
  | "already-claimed"
  | "stale-revision"
  | "validation"
  | "conflict"
  | "unknown";
```

Specific string checks must precede the broad eligibility check. Include an explicit `ErrResolutionOperationConflict`/equivalent-detail match with:

```text
"This source-change resolution no longer matches the current requirement. Close this review and start again."
```

The function must not invalidate/refetch or claim that a refetch occurred.

- [ ] **Step 5: Run focused tests and typecheck**

```bash
npm run test -- src/features/procurement/rfqPresentation.test.ts src/features/procurement/procurementErrors.test.ts
npm run typecheck
```

Expected: PASS.

---

### Task 4: Thin API wrappers, queries, mutation invalidation, and versions-404 normalization

**Files:**
- Modify: `apps/web/src/features/procurement/api.ts`
- Modify: `apps/web/src/features/procurement/queries.ts`
- Modify: `apps/web/src/features/procurement/mutations.ts`
- Test: add/extend the colocated procurement API/query/mutation tests already used by this feature. If none exist, create:
  - `apps/web/src/features/procurement/api.test.ts`
  - `apps/web/src/features/procurement/queries.test.tsx`
  - `apps/web/src/features/procurement/mutations.test.tsx`

**Interfaces:**
- Adds generated-contract-backed wrappers for review, acknowledge-unit, source discrepancy GET/resolve, Add RFQ line, Mark ready, and issued versions.
- Query hooks use `procurementKeys` from Task 2.
- Mutation hooks await targeted invalidations before resolving success.

- [ ] **Step 1: Write failing transport tests for the new wrappers**

Assert each wrapper calls the generated client path with the exact path/body shape from `schema.ts`:

```text
POST /material-requirements/{id}/review
POST /material-requirements/{id}/acknowledge-unit
GET  /material-requirements/{id}/source-discrepancy
POST /material-requirements/{id}/source-discrepancy/resolve
POST /rfqs/{id}/lines
POST /rfqs/{id}/ready
GET  /rfq-chains/{id}/versions
```

Use the existing `unwrapOrThrow` helper rather than bespoke response parsing.

- [ ] **Step 2: Add the thin wrappers**

Follow the existing feature `api.ts` pattern. Representative shape:

```ts
export async function reviewMaterialRequirement(id: string, expectedRevision: number) {
  return unwrapOrThrow(await client.POST("/material-requirements/{id}/review", {
    params: { path: { id } },
    body: { expectedRevision },
  }));
}

export async function getSourceDiscrepancy(id: string) {
  return unwrapOrThrow(await client.GET("/material-requirements/{id}/source-discrepancy", {
    params: { path: { id } },
  }));
}
```

Use the generated type/property names exactly; if the generated schema differs from the design spelling, the generated type wins and the code/tests must use that exact field.

- [ ] **Step 3: Write the issued-versions 404 normalization test**

Test the query/wrapper behavior:

```text
200 -> returns actual versions array
404 -> returns [] and query is successful, not error
500/network -> throws and remains an error
```

- [ ] **Step 4: Normalize only this query's 404 to `[]`**

Implement at query-function/wrapper boundary:

```ts
export async function listIssuedVersionsOrEmpty(rfqChainId: string) {
  try {
    return await listIssuedVersions(rfqChainId);
  } catch (error) {
    if (isApiError(error) && error.status === 404) return [];
    throw error;
  }
}
```

Do not fabricate Version 1 and do not suppress non-404 failures.

- [ ] **Step 5: Add query hooks with `procurementKeys`**

Source discrepancy query:

```ts
useQuery({
  queryKey: procurementKeys.sourceDiscrepancy(requirementId),
  queryFn: () => getSourceDiscrepancy(requirementId),
  enabled: open && Boolean(requirementId),
});
```

Issued versions query uses `procurementKeys.rfqVersions` and the normalized 404 wrapper.

- [ ] **Step 6: Write mutation tests for awaited invalidation**

Verify:
- Review/Acknowledge/Resolve await `requirements(projectId)` invalidation/refetch.
- Add-to-RFQ awaits both requirements and selected RFQ/RFQ-list invalidation before `isPending` becomes false.
- Mark ready awaits selected RFQ + RFQ list invalidation.
- Create RFQ explicitly uses `retry: false`.

- [ ] **Step 7: Implement mutation hooks**

Use `await Promise.all([...queryClient.invalidateQueries(...)])` inside `onSuccess` or wrap mutation execution so the returned mutation promise does not settle before the required invalidations are complete.

For Add-to-RFQ payload, combine:

```text
action.expectedRequirementRevision
+
selectedRfq.revision at execution time
```

Never store `expectedRFQRevision` in requirement presentation state.

- [ ] **Step 8: Run focused tests**

```bash
npm run test -- src/features/procurement/api.test.ts src/features/procurement/queries.test.tsx src/features/procurement/mutations.test.tsx
npm run typecheck
```

Expected: PASS.

---

### Task 5: Requirement cards, badges, action dispatch, and split/source provenance display

**Files:**
- Create: `apps/web/src/features/procurement/components/RequirementBadges.tsx`
- Create: `apps/web/src/features/procurement/components/RequirementPrimaryAction.tsx`
- Create: `apps/web/src/features/procurement/components/RequirementCard.tsx`
- Test: `apps/web/src/features/procurement/components/RequirementCard.test.tsx`

**Interfaces:**
- `RequirementCard` consumes one Material Requirement, `RequirementUIContext`, and `{pending, onAction}`.
- No card component fetches data or decides business legality.

- [ ] **Step 1: Write component tests before implementation**

Cover at least:

```text
draft -> identity/context + Draft badge + Review button
reviewed eligible + draft selected RFQ -> Add to RFQ
eligible + no selected draft RFQ -> no add button + selection message
unit mismatch -> Acknowledge unit only
source discrepancy -> Review source change only
claimed + source change -> View claiming RFQ only; warning badge still visible
archived clean -> no primary mutation
split source -> View split batches
split-incomplete -> no primary action + reconciliation-needed copy
create_separate provenance -> label "Created from source change" even though sourceType=manual
project-level requirement -> "Project-level requirement"
work item -> supplied context name
```

- [ ] **Step 2: Implement dumb badge and action components**

`RequirementPrimaryAction` props:

```ts
type Props = {
  action: RequirementPrimaryAction | null;
  pending: boolean;
  onAction: (action: RequirementPrimaryAction) => void;
};
```

It switches only on `action.type` to choose label and calls `onAction(action)`; navigation actions do not show mutation pending labels.

- [ ] **Step 3: Implement `RequirementCard`**

Render hierarchy:

```text
material name
quantity + unit
specification when present
work item name or Project-level requirement
source provenance label
badges
state.message
one primary action
```

Source label precedence:

```ts
if (requirement.createdFromDiscrepancyRequirementId) return "Created from source change";
if (requirement.sourceType === "manual") return "Manual";
if (requirement.sourceType === "cost_item") return "Generated";
if (requirement.sourceType === "split") return "Split batch";
```

For `source_removed`, do not display a synthetic negative quantity/change on the card.

- [ ] **Step 4: Run focused component tests**

```bash
npm run test -- src/features/procurement/components/RequirementCard.test.tsx
npm run typecheck
```

Expected: PASS.

---

### Task 6: ResolveDiscrepancyDialog with exact previews and concurrency-safe retries

**Files:**
- Create: `apps/web/src/features/procurement/components/ResolveDiscrepancyDialog.tsx`
- Test: `apps/web/src/features/procurement/components/ResolveDiscrepancyDialog.test.tsx`
- Modify as needed: `apps/web/src/features/procurement/queries.ts`
- Modify as needed: `apps/web/src/features/procurement/mutations.ts`

**Interfaces:**
- Lazy-loads discrepancy via `procurementKeys.sourceDiscrepancy(requirementId)` only while open.
- Renders only backend-provided `availableActions`.
- Uses Task 1 exact arithmetic for `delta` and `merge` previews.

- [ ] **Step 1: Write dialog tests for loading and action rendering**

Cover:

```text
opening -> loading skeleton while GET pending
change_detected -> accepted/current cards + delta + current procurement quantity
source_removed -> "No source cost items"; no "Change: -N" line
availableActions=[keep_current] -> only Keep current rendered
merge not in availableActions -> merge preview is never computed/rendered
update -> proposed result copy + returns to Draft note
merge -> current + (proposed - accepted) exact preview
keep_current -> unchanged current quantity copy
create_separate -> current unchanged + positive delta child copy
Apply disabled until selection and while pending
```

- [ ] **Step 2: Write idempotency tests for `create_separate`**

Test:
- selecting `create_separate` generates one UUID.
- repeated Apply after ambiguous failure reuses the same UUID.
- switching away then back to `create_separate` for the same discrepancy may retain the same logical-attempt UUID; closing/reopening starts a new one.
- stale discrepancy refresh clears selection and invalidates the old operation ID; reselecting creates a new UUID because fingerprint/delta changed.

- [ ] **Step 3: Write concurrency/error tests**

Test UI behavior:

```text
ErrMaterialRequirementDiscrepancyChanged -> dialog stays open, source discrepancy refetched, selection cleared, stale warning shown
ErrRevisionMismatch -> requirements + discrepancy refetched; if parent says record no longer resolvable, dialog closes
ErrMaterialRequirementAlreadyClaimed -> close + requirements invalidated
ErrResolutionOperationConflict -> stays failed; no silent UUID regeneration/retry; explicit translated message
other field errors -> applyFieldErrors first
other workflow error -> translateProcurementError
```

- [ ] **Step 4: Implement action presentation lookup**

Define all four copy builders but render only `availableActions.map(...)`.

Compute:

```ts
const delta = subtractQuantity(proposed.value, accepted.value);
const merged = availableActions.includes("merge")
  ? addQuantity(requirement.requiredQuantity.value, delta)
  : null;
```

Do not convert units. Use source quantity unit for delta; backend availability determines whether merge is legal.

- [ ] **Step 5: Implement submit payload**

Use generated OpenAPI field names exactly:

```ts
{
  action: selectedAction,
  expectedRevision: discrepancy.requirementRevision,
  expectedProposedFingerprint: discrepancy.proposedFingerprint,
  ...(selectedAction === "create_separate"
    ? { resolutionOperationId: operationId }
    : {}),
}
```

- [ ] **Step 6: Implement stale proposal refresh without closing**

On the specific discrepancy-changed conflict:

```text
keep open
invalidate/refetch sourceDiscrepancy(requirementId)
clear selectedAction
clear logical create_separate operation id
show stale warning
require new selection
```

- [ ] **Step 7: Run focused dialog tests and typecheck**

```bash
npm run test -- src/features/procurement/components/ResolveDiscrepancyDialog.test.tsx
npm run typecheck
```

Expected: PASS.

---

### Task 7: Manual requirement duplicate warning and create guard

**Files:**
- Create: `apps/web/src/features/procurement/requirementSimilarity.ts`
- Test: `apps/web/src/features/procurement/requirementSimilarity.test.ts`
- Modify: `apps/web/src/features/procurement/ProjectProcurement.tsx` (the current page owns the manual requirement creation flow described by the design spec).
- Test: `apps/web/src/features/procurement/ProjectProcurement.test.tsx`

**Interfaces:**
- Produces advisory-only `buildDuplicateCandidateKey()` and `findSimilarRequirement()`.
- No backend uniqueness or create-payload change.

- [ ] **Step 1: Write similarity tests**

Required cases:

```text
same material + same workItem + same trimmed unit + draft/reviewed -> match
same material + undefined workItem vs null workItem -> project-level match
generated existing requirement can trigger warning against manual candidate
archived existing -> ignored
split terminal source -> ignored
same material/workItem but different unit -> no match
no unit conversion/case guess beyond the form/API's existing canonical unit handling
```

- [ ] **Step 2: Implement candidate key + match helper**

```ts
export function buildDuplicateCandidateKey(input: {
  materialId: string;
  workItemId?: string | null;
  quantityUnit: string;
}): string {
  return [input.materialId, input.workItemId ?? "", input.quantityUnit.trim()].join("|");
}
```

Filter existing requirements to non-terminal statuses before comparing.

- [ ] **Step 3: Write creation-form warning tests**

Cover:
- first submit with match does not mutate; shows inline warning.
- View existing closes create UI and focuses/selects existing card if the page already has a supported focus mechanism; otherwise closes and leaves the existing card visible without adding new navigation infrastructure.
- Create another anyway performs exactly one mutation.
- approved bypass key is scoped to current material/workItem/unit.
- changing any key field after approval requires warning again.
- mutation pending disables submit and shows `Creating…`.
- mutation has `retry: false` if the existing create hook can retry automatically.

- [ ] **Step 4: Implement inline advisory warning**

Keep `approvedDuplicateKey` in component state. On submit:

```text
candidate key has active match AND approvedDuplicateKey !== candidateKey
-> show warning; do not mutate

otherwise
-> create mutation
```

Do not include quantity value/specification in the warning key; show them only in the warning summary.

- [ ] **Step 5: Run focused tests**

```bash
npm run test -- src/features/procurement/requirementSimilarity.test.ts src/features/procurement/ProjectProcurement.test.tsx
npm run typecheck
```

---

### Task 8: Integrate state-aware requirements into ProjectProcurement and serialize RFQ line addition

**Files:**
- Modify: `apps/web/src/features/procurement/ProjectProcurement.tsx`
- Modify as needed: `apps/web/src/features/procurement/mutations.ts`
- Test: extend/create `apps/web/src/features/procurement/ProjectProcurement.test.tsx`

**Interfaces:**
- Parent owns requirements query, Work Item lookup, selected RFQ, mutation instances, dialog selection/open state, and navigation/filtering.
- Requirement cards are pure renderers of already-loaded data/context.

- [ ] **Step 1: Write integration tests against the current broken pill behavior**

Tests must prove:

```text
current draft requirement no longer renders an "Add cement..." pill
requirements render as RequirementCard rows
Draft -> Review; after mocked review success + awaited refetch -> Add only when new server record is reviewed
Acknowledge -> refetch -> next action derived from fresh revision
Resolve -> dialog -> success -> refetch -> next action derived from fresh state
claimed -> View RFQ uses requirement.activeRfqChainId/Number
split-source -> View split batches filters children by splitFromRequirementId
split-incomplete -> no mutation action
```

- [ ] **Step 2: Remove the `status === "open" || status === "draft"` addability filter**

Replace the pill `.map()` entirely with all relevant project requirements rendered through `RequirementCard`. Terminal records remain visible with their read-only/resolve presentation; do not hide them purely because they are not RFQ-addable.

- [ ] **Step 3: Build the Work Item lookup once at parent level**

Use the already-loaded Work Item list/query; create a `Map<string, WorkItem>` with `useMemo`. Do not add N per-card queries.

- [ ] **Step 4: Pass selected RFQ context explicitly**

```ts
const requirementContext = {
  workItemName: ...,
  selectedRfq: selectedRfq
    ? { id: selectedRfq.id, number: selectedRfq.rfqNumber, status: selectedRfq.status, revision: selectedRfq.revision }
    : undefined,
};
```

Claimed badges still use requirement claim metadata, not selected RFQ values.

- [ ] **Step 5: Add one `handleRequirementAction` switch**

Implement exhaustive switch over:

```text
review
acknowledge-unit
resolve-discrepancy
add-to-rfq
view-rfq
view-split-children
```

No component-specific parallel action props.

- [ ] **Step 6: Serialize Add-to-RFQ per selected RFQ**

While the shared add-line mutation for RFQ X is pending:
- every `add-to-rfq` card targeting X receives `pending=true`/disabled.
- payload uses the action's requirement revision plus the latest selected RFQ revision.
- after success, await requirements + RFQ/RFQ-list invalidation/refetch before the mutation settles.
- do not optimistically increment RFQ revision client-side.

- [ ] **Step 7: Await review/acknowledge/resolve invalidations before next action**

Do not locally toggle status/revision. Let the refetched backend record feed `deriveRequirementUIState()`.

- [ ] **Step 8: Run integration tests**

```bash
npm run test -- src/features/procurement/ProjectProcurement.test.tsx
npm run typecheck
```

Expected: PASS.

---

### Task 9: Mark-ready UX, issued-versions empty state, and page-level error behavior

**Files:**
- Modify: `apps/web/src/features/procurement/ProjectProcurement.tsx`
- Modify as needed: `apps/web/src/features/procurement/queries.ts`
- Modify as needed: `apps/web/src/features/procurement/mutations.ts`
- Test: `apps/web/src/features/procurement/ProjectProcurement.test.tsx`

**Interfaces:**
- `deriveRFQReadinessState` controls only local precheck/UI gating.
- Backend MarkReady remains authoritative.
- Issued versions 404 is successful empty data.

- [ ] **Step 1: Add failing Mark-ready tests**

Cover:

```text
empty draft -> disabled + no-scope message
draft with lines but blank delivery address -> disabled + address message
draft with lines + address -> enabled
backend ErrRFQLineNotEligible after click -> form/workflow-level translated error; no silent retry
ready RFQ -> Mark ready unavailable/disabled based on existing page design
pending -> "Marking ready…" and disabled
```

- [ ] **Step 2: Wire `deriveRFQReadinessState`**

Do not fetch underlying requirements to predict readiness. Keep backend validation authoritative.

- [ ] **Step 3: Add issued-versions empty-state tests**

Cover:

```text
normalized [] -> "No issued versions yet"
404 never enters page aggregate queryError
500 still enters normal error handling
no placeholder Version 1 is rendered
```

- [ ] **Step 4: Remove versions-404 from aggregate page error**

Because Task 4 normalizes 404 to `[]`, `useIssuedVersions` should be successful with empty data; aggregate error logic should only see real errors.

- [ ] **Step 5: Verify all mutation buttons have double-submit guards**

Assert labels/disable behavior for:

```text
Create requirement -> Creating…
Review -> Reviewing…
Acknowledge -> Acknowledging…
Add to RFQ -> Adding…
Apply resolution -> Applying…
Mark ready -> Marking ready…
Create draft RFQ -> Creating…
```

Ensure Create RFQ mutation explicitly has `retry: false`.

- [ ] **Step 6: Run page tests**

```bash
npm run test -- src/features/procurement/ProjectProcurement.test.tsx
npm run typecheck
```

Expected: PASS.

---

### Task 10: Full verification and design-spec coverage audit

**Files:**
- No production files should be introduced in this task unless verification finds a genuine defect.
- Update the implementation log/task tracker only if the repository already requires one for completed work; do not create a new tracking convention.

**Interfaces:**
- Produces a verified, uncommitted procurement redesign ready for review.

- [ ] **Step 1: Run the entire frontend unit/component suite**

```bash
cd apps/web
npm run test
```

Expected: PASS.

- [ ] **Step 2: Run static gates**

```bash
npm run lint
npm run typecheck
npm run build
```

Expected: PASS.

- [ ] **Step 3: Verify generated OpenAPI was not hand-edited**

If no backend transport change occurred, `apps/web/src/lib/api/generated/schema.ts` and `apps/web/openapi/openapi.json` must have no procurement-redesign diff.

```bash
git diff -- apps/web/src/lib/api/generated/schema.ts apps/web/openapi/openapi.json
```

Expected: no diff attributable to this work.

- [ ] **Step 4: Run focused browser/manual procurement smoke test against the local backend**

Use one project with a draft RFQ and exercise:

```text
1. Manual draft requirement -> Review -> card becomes reviewed.
2. Reviewed eligible requirement -> Add to RFQ -> one line appears; requirement becomes claimed.
3. Empty draft RFQ -> Mark ready disabled; after line + address -> enabled.
4. Generated mismatch requirement -> acknowledge -> review/add path works.
5. Generated source change -> Review source change dialog -> one allowed resolution -> fresh next action.
6. Never-issued RFQ -> issued versions panel says "No issued versions yet" and no page-level error banner.
7. Create a similar manual requirement -> warning -> View existing / Create another anyway behavior.
8. Double-click each mutating action while network is slowed -> only one request is sent for that logical action.
```

For `create_separate`, if a reproducible discrepancy with positive delta is available, also confirm the created sibling reads `"Created from source change"` and starts draft.

- [ ] **Step 5: Spec coverage self-review**

Check each design requirement maps to code/tests:

```text
root-cause correction / no "open" pseudo-status
9-branch requirement precedence with terminal-source discrepancy reachability
one primary action + composable badges
selected draft RFQ context requirement
source_removed distinct presentation
create_separate provenance
lazy discrepancy dialog + backend availableActions only
exact BigInt quantity previews
stable resolutionOperationId + stale refresh
sentinel-specific 409 behavior
read-only split presentation, no split creation
RFQ readiness local prechecks only
central procurement error translation after field errors
duplicate warning + fingerprint-scoped bypass
per-RFQ add serialization + awaited revisions
all mutation double-submit guards
query-key factory
versions 404 -> []
no backend/OpenAPI change
```

If any item has no owning test, add the smallest focused test before declaring completion.

- [ ] **Step 6: Placeholder and type-consistency scan**

```bash
rg -n "TODO|TBD|implement later|similar to Task|not eligible for an RFQ" apps/web/src/features/procurement apps/web/src/lib/formatting/quantityMath.ts
```

Expected: no newly introduced TODO/TBD placeholders; backend-detail fallback strings are centralized only in `procurementErrors.ts`/tests.

- [ ] **Step 7: Final uncommitted diff review**

```bash
git status --short
git diff --check
git diff --stat
```

Expected: only intended frontend procurement/quantity files changed; no backend changes; no commit.

---

## Self-Review

**Spec coverage:** Tasks 1-10 cover the complete procurement redesign: exact quantity math; pure requirement and RFQ presentation state; terminal-anchor discrepancy reachability; cards; lazy source-discrepancy resolution with all backend-provided actions; stable `create_separate` idempotency; error translation; duplicate warning; selected-RFQ context; per-RFQ mutation serialization; awaited query refreshes; Mark-ready prechecks; versions-404 normalization; and read-only split presentation.

**Scope check:** This remains one coherent frontend procurement workflow project. Split creation/reconciliation is explicitly deferred and therefore does not require a second implementation plan here.

**Placeholder scan:** No implementation step contains TODO/TBD placeholders or unnamed test paths. Manual requirement duplicate-warning coverage is assigned to the existing procurement page and `ProjectProcurement.test.tsx`.

**Type consistency:** `RequirementPrimaryAction` never carries RFQ revision; Add-to-RFQ combines requirement revision with the selected RFQ's latest revision at execution time. `sourceDiscrepancy(requirementId)` is the only discrepancy-query key. `resolutionOperationId` is present only for `create_separate`. `deriveRFQReadinessState` does not claim backend-authoritative readiness.

## Execution Handoff

Plan complete. Execute with one of:

1. **Subagent-Driven (recommended):** use `superpowers:subagent-driven-development`, one fresh subagent per task with review gates.
2. **Inline Execution:** use `superpowers:executing-plans`, executing tasks in batches with checkpoints.

Do not begin implementation until this plan has been reviewed and approved.
