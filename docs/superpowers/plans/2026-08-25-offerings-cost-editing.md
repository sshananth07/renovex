# Editable Supplier Offerings & Safe Cost Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Do NOT run any git command at any point — this project is kept fully local by standing instruction. Every task ends with a verification step instead of a commit step.

**Goal:** Make Supplier Offerings editable via a click-row-to-drawer UI (backend already supports this fully), and give CostItem safe, scoped correction semantics: CAS-guarded Edit for Estimated/Committed, an auditable Correct mechanism for Actual, and Paid left explicitly read-only (no safe adjustment mechanism exists and none is invented here).

**Architecture:** Part A is almost entirely frontend — `suppliers.Service.UpdateOffering` already exists, is fully tested, and SupplierOffering.IndicativePrice is architecturally unreachable from any RFQ/offer/award code (confirmed by module-boundary comment and exhaustive grep), so no historical-mutation risk exists to fix. Part B adds `Revision int64` and `ActualCorrections []ActualCorrection` (embedded, immutable-append-only) fields to `CostItem`, converts the two existing write paths to CAS-guarded (with a legacy-document compatibility rule for pre-existing records that predate the Revision field), and adds one new service method (`CorrectCostItemActual`) that performs a single atomic `$set`+`$push` `UpdateOne` — no second collection, no Mongo transaction, no replica-set requirement. The existing lifecycle-update route is tightened so it can no longer bypass the correction path once Actual is already set. Paid stays read-only.

**Tech Stack:** Go (chi + Huma v2, mongo-driver v2) — version per `backend/go.mod`, do not hardcode a version number in code or comments. Next.js + TypeScript, React Hook Form + Zod, TanStack Query, openapi-typescript generated client.

**Spec:** This plan's own header carries the full requirement (see the original task text preserved in project conversation) — no separate design doc exists; the two investigation reports that informed this plan are not saved as files, so this plan carries all decisions inline. This plan was reviewed once and revised in place per that review's findings (embedded corrections instead of a second collection/transaction, legacy-revision compatibility, lifecycle-route bypass closure, no cross-module revision leakage into labour, exact-string money conversion, Availability field coverage, and a keyboard-reachable row-click affordance) — the review is not a separate file, its conclusions are folded directly into the tasks below.

## Global Constraints

- No git commands, ever (no `git add`, `git commit`, `git init`).
- No subagents — execute this plan inline in the current session (per the standing instruction that authored this task).
- Money is always `int64` minor units via `internal/foundation/money.Money`; quantities always `shopspring/decimal` via `internal/foundation/quantity.Quantity` — never floats, never JS `Number` division/multiplication on an authoritative amount, on either side of the wire. This includes minor→major display conversions for form pre-population, not just major→minor submission: use an exact string-based helper, never `amount / 100` as a JS number operation whose result feeds back into an editable value.
- Every write path must be tenant-scoped (`companyId` from the authenticated principal, never trusted from the request body/path beyond what the CAS filter re-validates).
- New/changed Huma operations require regenerating `apps/web/openapi/openapi.json` via `cd backend && go run ./cmd/openapi -out ../apps/web/openapi/openapi.json`, then `npm run openapi:generate` in `apps/web`, before writing any frontend code against them.
- Do not weaken any existing lifecycle, tenant, procurement, or architecture test to make new work pass.
- Do not invent Purchase Order → committed-cost behavior, and do not invent a Paid-adjustment mechanism — Paid stays explicitly read-only in this plan, reported as a deliberate limitation.
- Follow existing conventions exactly: `conditionalUpdate`-style CAS (filter includes `revision: expectedRevision`, `$set.revision = expectedRevision+1`, 0-match re-read distinguishes 404 from 409/`ErrRevisionMismatch`); RHF + Zod + generated OpenAPI types + TanStack Query + `applyFieldErrors` on the frontend; `Sheet`-based edit drawers pre-populated via `defaultValues`, matching `apps/web/src/features/work-items/components/WorkItemList.tsx`.
- New interactive UI written in this plan (not pre-existing code being left alone) must be keyboard-reachable, not click-only — this applies to the new row-click-to-drawer affordance in both Task A4 and Task B5.
- A module's own service owns its aggregate's concurrency mechanics. A capability another module consumes (e.g. `labour.LabourCostRecorder`) states what should change, never a revision/CAS parameter belonging to the module that owns the write.

---

## Part A: Editable Supplier Offerings (frontend + one backend test gap)

### Task A1: PATCH-boundary tests for `supplier-offerings-update` (backend)

**Files:**
- Modify: `backend/internal/suppliers/handler_test.go`

**Interfaces:**
- Consumes: `suppliers.RegisterHandlers`, `PATCH /supplier-offerings/{id}` (already registered at `handler.go:511`), `newSuppliersHandlerTestRouter` (already defined in this file), `createSupplier` (already defined in `service_test.go`, same package `suppliers_test`).
- Produces: nothing new consumed elsewhere — this task only adds test coverage for an already-existing route.

This route already works end-to-end (service, repo, Mongo CAS, tenant-isolation tests all exist per investigation). This task closes the one real gap: `handler_test.go` has zero PATCH/Update-specific tests, unlike Create.

- [ ] **Step 1: Write the request-boundary tests**

Add to `backend/internal/suppliers/handler_test.go`, after the last `Test...RejectedAtRequestBoundary` test and before `errorLocations`:

```go
func doPatchOfferingRequest(t *testing.T, router http.Handler, offeringID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/supplier-offerings/"+offeringID, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestUpdateOfferingChangesProductNameAndPrice(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")
	create := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "Old Name",
		"unit":        "bag",
	})
	if create.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200, body %s", create.Code, create.Body.String())
	}
	var created struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created offering: %v", err)
	}

	response := doPatchOfferingRequest(t, router, created.ID, map[string]any{
		"expectedRevision":        created.Revision,
		"productName":             "New Name",
		"indicativePriceAmount":   13000,
		"indicativePriceCurrency": "MYR",
		"indicativePriceAsOf":     "2026-08-20T00:00:00Z",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
	var updated struct {
		ProductName     string `json:"productName"`
		IndicativePrice struct {
			Amount int64 `json:"amount"`
		} `json:"indicativePrice"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated offering: %v", err)
	}
	if updated.ProductName != "New Name" {
		t.Errorf("productName = %q, want %q", updated.ProductName, "New Name")
	}
	if updated.IndicativePrice.Amount != 13000 {
		t.Errorf("indicativePrice.amount = %d, want 13000", updated.IndicativePrice.Amount)
	}
}

func TestUpdateOfferingStaleRevisionRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")
	create := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "Old Name",
		"unit":        "bag",
	})
	var created struct{ ID string `json:"id"` }
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created offering: %v", err)
	}

	response := doPatchOfferingRequest(t, router, created.ID, map[string]any{
		"expectedRevision": 999,
		"productName":      "New Name",
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", response.Code, response.Body.String())
	}
}

func TestUpdateOfferingSupplierIDMismatchRejected(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")
	other := createSupplier(t, svc, "Other Materials")
	create := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "Old Name",
		"unit":        "bag",
	})
	var created struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created offering: %v", err)
	}

	response := doPatchOfferingRequest(t, router, created.ID, map[string]any{
		"expectedRevision": created.Revision,
		"supplierId":       other.ID,
	})
	if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 422 or 409 for a supplier-id change attempt, body %s", response.Code, response.Body.String())
	}
}

func TestUpdateOfferingUnknownIDIsNotFound(t *testing.T) {
	router, _ := newSuppliersHandlerTestRouter(t)
	response := doPatchOfferingRequest(t, router, "000000000000000000000000", map[string]any{
		"expectedRevision": 0,
		"productName":      "New Name",
	})
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", response.Code, response.Body.String())
	}
}
```

- [ ] **Step 2: Run the new tests to confirm they pass against the existing implementation**

Run: `cd backend && go test ./internal/suppliers/... -run 'TestUpdateOffering' -v`
Expected: all 4 new tests PASS (the route already works — this step proves it, it does not implement anything new).

- [ ] **Step 3: Run the full suppliers package test suite**

Run: `cd backend && go test ./internal/suppliers/... -v`
Expected: PASS, no regressions.

---

### Task A2: Add `updateOffering` to the frontend API layer

**Files:**
- Modify: `apps/web/src/features/procurement/api.ts`

**Interfaces:**
- Consumes: `apiClient.PATCH`, `apiClient.POST`, `unwrapOrThrow` (all already imported or importable in this file), the generated `components["schemas"]["PatchOfferingInputBody"]` type (confirm the exact generated name in Step 1 below — Huma names it from the operation's registered input struct, likely `PatchOfferingInputBody` matching the `createOffering` naming convention already in this file, e.g. `CreateOfferingInputBody`).
- Produces: `updateOffering(id: string, body: PatchOfferingInputBody): Promise<Offering>` and `setOfferingActive(id: string, expectedRevision: number, active: boolean): Promise<Offering>` — Availability is a SEPARATE mutable field (`SupplierOffering.Active`) reached via the dedicated `POST /supplier-offerings/{id}/active` route, NOT part of `PatchOfferingInputBody` (confirmed from investigation: the PATCH body has no `active` field at all). Both consumed by Task A4.

- [ ] **Step 1: Confirm the generated type names**

Run: `grep -n "PatchOffering\|SetOfferingActive\|OfferingActive" apps/web/src/lib/api/generated/schema.ts` (from repo root). Confirm the exact PascalCase name openapi-typescript generated for both the PATCH body schema and the `POST /supplier-offerings/{id}/active` body schema (it is generated from the Huma operation's Body struct — it may already exist from the backend route that has existed all along; if `apps/web/openapi/openapi.json` predates this session's SupplierOffering work it may be stale — regenerate first per Step 1a below if either type is missing).

- [ ] **Step 1a: Regenerate the OpenAPI contract if either type is missing or stale**

Run: `cd backend && go run ./cmd/openapi -out ../apps/web/openapi/openapi.json`
Then: `cd apps/web && npm run openapi:generate`
Re-run the grep from Step 1 to confirm both types now exist.

- [ ] **Step 2: Add both wrapper functions**

Add to `apps/web/src/features/procurement/api.ts`, directly after the existing `createOffering` function:

```ts
export async function updateOffering(id: string, body: components["schemas"]["PatchOfferingInputBody"]) {
  return unwrapOrThrow(await apiClient.PATCH("/supplier-offerings/{id}", { params: { path: { id } }, body }));
}
export async function setOfferingActive(id: string, expectedRevision: number, active: boolean) {
  return unwrapOrThrow(await apiClient.POST("/supplier-offerings/{id}/active", { params: { path: { id } }, body: { expectedRevision, active } }));
}
```

(Adjust the schema key names and the `setOfferingActive` body shape to whatever Step 1 confirmed the real generated input type requires.)

- [ ] **Step 3: Typecheck**

Run: `cd apps/web && npx tsc --noEmit`
Expected: no new errors.

---

### Task A3: Add `useUpdateOffering` mutation hook

**Files:**
- Modify: `apps/web/src/features/procurement/mutations.ts`

**Interfaces:**
- Consumes: `api.updateOffering`, `api.setOfferingActive` (Task A2), `procurementKeys` (already imported in this file).
- Produces: `useUpdateOffering(supplierId: string)` returning a mutation with `mutate({ offeringId, body })`; `useSetOfferingActive(supplierId: string)` returning a mutation with `mutate({ offeringId, expectedRevision, active })`. Both consumed by Task A4.

- [ ] **Step 1: Check `procurementKeys` for an offerings key**

Run: `grep -n "offerings" apps/web/src/features/procurement/queryKeys.ts`. If no `offerings(supplierId)` key exists yet, check how `useOfferings` currently builds its query key in `apps/web/src/features/procurement/queries.ts` (grep for `useOfferings`) and reuse that exact key shape for invalidation — do not invent a second key shape for the same data.

- [ ] **Step 2: Add both mutation hooks**

Add to `apps/web/src/features/procurement/mutations.ts` (matching the file's existing hook shape, e.g. `useArchiveRequirement`):

```ts
export function useUpdateOffering(supplierId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ offeringId, body }: { offeringId: string; body: Parameters<typeof api.updateOffering>[1] }) => {
      const result = await api.updateOffering(offeringId, body);
      await queryClient.invalidateQueries({ queryKey: ["suppliers", supplierId, "offerings"] });
      return result;
    },
  });
}

export function useSetOfferingActive(supplierId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ offeringId, expectedRevision, active }: { offeringId: string; expectedRevision: number; active: boolean }) => {
      const result = await api.setOfferingActive(offeringId, expectedRevision, active);
      await queryClient.invalidateQueries({ queryKey: ["suppliers", supplierId, "offerings"] });
      return result;
    },
  });
}
```

(Use whichever exact query-key array Step 1 found `useOfferings` uses — `SupplierDetail.tsx`'s existing `createProduct` mutation already invalidates `["suppliers", supplierId, "offerings"]`, confirm this matches.)

- [ ] **Step 3: Typecheck**

Run: `cd apps/web && npx tsc --noEmit`
Expected: no new errors.

---

### Task A4: Add an exact minor↔major money-string helper, then the Edit Offering drawer to `SupplierDetail.tsx`

**Files:**
- Modify: `apps/web/src/lib/formatting/money.ts`
- Modify: `apps/web/src/features/procurement/components/SupplierDetail.tsx`
- Test: create/extend `apps/web/src/lib/formatting/money.test.ts` if it exists (check via `ls apps/web/src/lib/formatting/money.test.ts`); create it if it doesn't.

**Interfaces:**
- Consumes: `useUpdateOffering`, `useSetOfferingActive` (Task A3), `Sheet`/`SheetContent`/`SheetHeader`/`SheetTitle` from `@/components/ui/sheet`, the existing `offeringSchema`/`OfferingValues` Zod schema already defined in this file (reused as-is for edit, not duplicated), `applyFieldErrors`, `majorToMinor`/`formatMoney` (already imported).
- Produces: `minorToMajorString(amountMinor: number): string` in `money.ts`, consumed by this task's `toOfferingFormValues` mapper and by Task B5.

- [ ] **Step 1: Add an exact minor→major string conversion helper**

The plan's earlier draft used `String(amount / 100)` to pre-populate a form field from a stored minor-unit integer. That is a JS floating-point division whose result feeds back into an editable value — exactly what this codebase's money rule forbids, matching how `majorToMinor` (major string → minor integer) is already implemented with `BigInt`, not `parseFloat`. Add the missing inverse direction with the same rigor:

```ts
// Exact minor-unit integer -> major-unit decimal string, using integer string
// manipulation only (BigInt), never a floating-point division — the inverse
// of majorToMinor. Used anywhere a stored minor-unit amount must pre-populate
// an editable form field as a string.
export function minorToMajorString(amountMinor: number): string {
  const negative = amountMinor < 0;
  const digits = Math.abs(amountMinor).toString().padStart(3, "0");
  const whole = digits.slice(0, -2);
  const fraction = digits.slice(-2);
  return `${negative ? "-" : ""}${whole}.${fraction}`;
}
```

- [ ] **Step 2: Write a focused test for the new helper**

Create (or extend) `apps/web/src/lib/formatting/money.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { majorToMinor, minorToMajorString } from "./money";

describe("minorToMajorString", () => {
  it("converts whole and fractional minor amounts exactly", () => {
    expect(minorToMajorString(0)).toBe("0.00");
    expect(minorToMajorString(100)).toBe("1.00");
    expect(minorToMajorString(12345)).toBe("123.45");
    expect(minorToMajorString(5)).toBe("0.05");
    expect(minorToMajorString(-850000)).toBe("-8500.00");
  });

  it("round-trips through majorToMinor for a range of values", () => {
    for (const minor of [0, 1, 50, 850000, 8500, 123456789]) {
      expect(majorToMinor(minorToMajorString(minor))).toBe(minor);
    }
  });
});
```

- [ ] **Step 3: Run the new test**

Run: `cd apps/web && npx vitest run src/lib/formatting/money.test.ts`
Expected: PASS.

- [ ] **Step 4: Add drawer-open state and a `toOfferingFormValues` mapper**

In `SupplierDetail.tsx`, add near the existing `useState` calls:

```tsx
const [editingOffering, setEditingOffering] = useState<(typeof offerings.data extends (infer T)[] | undefined ? T : never) | null>(null);
```

If that inline conditional type is awkward given the actual `useOfferings` return type, instead import the element type directly — check `apps/web/src/features/procurement/queries.ts` for `useOfferings`'s return type and use its array element type explicitly, e.g. `import type { Offering } from "../api";` then `useState<Offering | null>(null)`. Prefer the explicit import; only fall back to the inline conditional if no exported `Offering` type exists.

Add a mapper function near the top of the component (or as a module-level function like the file's own `todayDateInput`):

```tsx
function toOfferingFormValues(offering: Offering): OfferingValues {
  return {
    productName: offering.productName,
    category: offering.category ?? "",
    unit: offering.unit ?? "",
    price: offering.indicativePrice ? minorToMajorString(offering.indicativePrice.amount) : "",
    priceAsOf: offering.indicativePriceAsOf ? offering.indicativePriceAsOf.slice(0, 10) : todayDateInput(),
  };
}
```

Add `minorToMajorString` to the existing `@/lib/formatting/money` import line.

- [ ] **Step 5: Wire a second `useForm` instance for editing, plus both mutations**

```tsx
const editOfferingForm = useForm<OfferingValues>({ resolver: zodResolver(offeringSchema), defaultValues: offeringDefaults });
const updateOffering = useUpdateOffering(supplierId);
const setActive = useSetOfferingActive(supplierId);
const [editOfferingFormError, setEditOfferingFormError] = useState<string>();
```

- [ ] **Step 6: Populate the edit form when a row is clicked, and make rows both clickable and keyboard-reachable**

This is newly-authored interactive UI, not pre-existing code — it must be operable without a mouse. Change the `<tr>` in the offerings table body to a focusable, keyboard-activatable row (row semantics are preserved; the row itself becomes the interactive element via `tabIndex`/`role`/`onKeyDown`, matching how a `<button>` would behave for Enter/Space without abandoning the `<table>` structure the rest of this component already uses):

```tsx
function openOfferingEditor(entry: Offering) {
  setEditingOffering(entry);
  editOfferingForm.reset(toOfferingFormValues(entry));
  setEditOfferingFormError(undefined);
}
```

```tsx
<tr
  key={entry.id}
  tabIndex={0}
  role="button"
  aria-label={`Edit offering ${entry.productName}`}
  className="cursor-pointer border-b last:border-0 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  onClick={() => openOfferingEditor(entry)}
  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openOfferingEditor(entry); } }}
>
```

(Keep every existing `<td>` inside unchanged.)

- [ ] **Step 7: Add the submit handler and the availability toggle handler**

```tsx
const submitEditOffering = editOfferingForm.handleSubmit((values) => {
  if (!editingOffering) return;
  setEditOfferingFormError(undefined);
  updateOffering.mutate(
    {
      offeringId: editingOffering.id,
      body: {
        expectedRevision: editingOffering.revision,
        productName: values.productName,
        category: values.category || undefined,
        unit: values.unit || undefined,
        indicativePriceAmount: values.price ? majorToMinor(values.price) ?? undefined : undefined,
        indicativePriceCurrency: values.price ? "MYR" : undefined,
        indicativePriceAsOf: values.price ? new Date(values.priceAsOf).toISOString() : undefined,
        clearIndicativePrice: values.price === "",
      },
    },
    {
      onSuccess: (updated) => setEditingOffering(updated),
      onError: (error) => setEditOfferingFormError(applyFieldErrors(error as unknown as ApiError, editOfferingForm.setError, offeringFields, offeringFieldAliases)),
    }
  );
});

function toggleAvailability() {
  if (!editingOffering) return;
  setActive.mutate(
    { offeringId: editingOffering.id, expectedRevision: editingOffering.revision, active: !editingOffering.active },
    { onSuccess: (updated) => setEditingOffering(updated) }
  );
}
```

`onSuccess: (updated) => setEditingOffering(updated)` (rather than closing the drawer) matters here because Availability and the main PATCH are two SEPARATE writes against the SAME `expectedRevision`-guarded document — after either succeeds, `editingOffering` must be refreshed to the server's new revision before the other control can be used again, or the second write would be rejected with a stale-revision conflict. Confirm both `updateOffering`'s and `setActive`'s mutation functions in Task A3 actually return the updated `Offering` from the API call (they do, via `unwrapOrThrow`'s return value) so this works.

Confirm the exact field names on the generated `PatchOfferingInputBody` type match (`expectedRevision`, `clearIndicativePrice`, etc.) against what Task A2 Step 1 found in the generated schema — adjust field names in this snippet if they differ.

- [ ] **Step 8: Render the Sheet, including the Availability toggle**

Add after the existing "Add offering" `Dialog`, before the component's closing `</div>`:

```tsx
<Sheet open={editingOffering !== null} onOpenChange={(open) => { if (!open) setEditingOffering(null); }}>
  <SheetContent>
    <SheetHeader><SheetTitle>Edit offering</SheetTitle></SheetHeader>
    <div className="flex-1 overflow-y-auto px-4 pb-4">
      {editingOffering && <>
        <div className="mb-4 flex items-center justify-between rounded-lg border p-3">
          <div>
            <p className="text-sm font-medium">Availability</p>
            <p className="text-xs text-muted-foreground">{editingOffering.active ? "Available for new RFQ invitations" : "Marked unavailable"}</p>
          </div>
          <Button type="button" variant="outline" size="sm" disabled={setActive.isPending} onClick={toggleAvailability}>
            {setActive.isPending ? "Saving…" : editingOffering.active ? "Mark unavailable" : "Mark available"}
          </Button>
        </div>
        <form onSubmit={submitEditOffering} className="grid gap-4">
          {editOfferingFormError && <p className="text-sm text-destructive">{editOfferingFormError}</p>}
          <Label className="grid gap-1.5">Product name<Input {...editOfferingForm.register("productName")} />{editOfferingForm.formState.errors.productName?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.productName.message}</p>}</Label>
          <Label className="grid gap-1.5">Category<Input {...editOfferingForm.register("category")} /></Label>
          <Label className="grid gap-1.5">Unit<Input {...editOfferingForm.register("unit")} />{editOfferingForm.formState.errors.unit?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.unit.message}</p>}</Label>
          <Label className="grid gap-1.5">Indicative price (MYR)<Input inputMode="decimal" placeholder="Leave blank if unknown…" {...editOfferingForm.register("price")} />{editOfferingForm.formState.errors.price?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.price.message}</p>}</Label>
          {editOfferingForm.watch("price") !== "" && <Label className="grid gap-1.5">Price as of<Input type="date" {...editOfferingForm.register("priceAsOf")} />{editOfferingForm.formState.errors.priceAsOf?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.priceAsOf.message}</p>}</Label>}
          <div className="flex gap-2"><Button type="submit" disabled={updateOffering.isPending}>{updateOffering.isPending ? "Saving…" : "Save"}</Button><Button type="button" variant="outline" onClick={() => setEditingOffering(null)}>Cancel</Button></div>
        </form>
      </>}
    </div>
  </SheetContent>
</Sheet>
```

Note the `entry.active` field referenced above (and in Step 6's `toggleAvailability`) must exist on the `Offering` type the frontend already has generated — confirm via `grep -n "active" apps/web/src/lib/api/generated/schema.ts` under the offering DTO's schema block; the investigation found `Active bool` maps to a JSON `active` field on the domain struct, but confirm the DTO (not just the domain struct) actually surfaces it before relying on it — if the existing `offeringDTO` in the backend handler does not yet expose `active`/`revision`, add both fields to `toOfferingDTO`'s output in `backend/internal/suppliers/handler.go` as a small addition to this task (check first; the investigation's DTO field table already lists `Active`/`Revision` as present, so this is very likely already fine — verify, don't assume).

- [ ] **Step 9: Add the `Sheet` import**

Add to the top imports:

```tsx
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
```

- [ ] **Step 10: Typecheck**

Run: `cd apps/web && npx tsc --noEmit`
Expected: no errors. Fix any field-name mismatches surfaced against the real generated schema.

- [ ] **Step 11: Manual verification**

Start the backend (`cd backend && APP_ENV=development go run ./cmd/api`) and frontend (`cd apps/web && npm run dev`) against the local Docker stack. Log in, navigate to Suppliers → a supplier with offerings → click an offering row (and separately, Tab to a row and press Enter) → confirm the drawer opens with existing values pre-populated, including the correct Availability state → change the product name and price → Save → confirm the drawer's Availability toggle still shows the right state and the table row reflects the new value without a full page reload → click "Mark unavailable"/"Mark available" → confirm it updates without needing to also click Save.

---

## Part B: Cost Editing / Correction

### Task B1: Add `Revision` to `CostItem` (with legacy-document compatibility) and migrate existing write paths to CAS

**Files:**
- Modify: `backend/internal/costs/cost_item.go`
- Modify: `backend/internal/costs/repository.go`
- Modify: `backend/internal/costs/repository_mongo.go`
- Modify: `backend/internal/costs/service.go`
- Modify: `backend/internal/costs/handler.go`
- Modify: `backend/internal/labour/service.go` (or wherever `UpdateLabourCostEstimate`'s one caller lives — confirmed via grep in Step 7 below)
- Test: `backend/internal/costs/service_test.go`
- Test: `backend/internal/costs/repository_mongo_test.go`
- Test: `backend/internal/costs/handler_test.go`

**Interfaces:**
- Consumes: nothing new — this task changes existing signatures within the `costs` package only (plus one caller in `internal/labour`, whose own public signature is deliberately UNCHANGED — see Step 7).
- Produces: `CostItem.Revision int64`; `CostItemRepository.UpdateLifecycleField(ctx, companyID, id string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error)` (added `expectedRevision` param); `CostItemRepository.UpdateDetails(ctx, companyID, id string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error)` (added `expectedRevision` param); `Service.UpdateCostItemLifecycle(ctx, companyID, costItemID string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error)`; `Service.UpdateCostItemDetails(ctx, companyID, costItemID string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error)`; new sentinel `ErrRevisionMismatch = errors.New("costs: cost item changed since it was read")`. `Task B2` and `Task B3` depend on `CostItem.Revision` existing and on the `costItemDoc`/`toCostItemDoc`/`fromCostItemDoc` round-trip carrying it.

This task is the prerequisite everything else in Part B depends on: without a Revision field, neither the normal Edit path nor the Correct path can safely guard against concurrent writes.

**Legacy-document compatibility (mandatory — do not skip):** any `CostItem` document written before this task shipped has NO `revision` field in Mongo at all — not `revision: 0`, genuinely absent. A CAS filter of `{"_id": objID, "companyId": companyID, "revision": 0}` does NOT match a document where the field is simply missing, so the first edit of any pre-existing cost item would incorrectly return a stale-revision conflict on a record nobody has touched. Every conditional-write filter in this task must treat `expectedRevision == 0` as matching EITHER `revision == 0` OR `revision` absent, and the first successful write against a legacy document must set `revision: 1` (not increment a nonexistent value) exactly as it would for a real revision-0 document — the fix is purely in the filter, `$set.revision = expectedRevision + 1` already produces the correct `1` regardless of whether the prior document had an explicit `0` or no field at all.

- [ ] **Step 1: Add the failing repository test for revision-guarded lifecycle update, including the legacy-document case**

Add to `backend/internal/costs/repository_mongo_test.go` (find the existing `TestMongo...` tests for `UpdateLifecycleField`/`UpdateDetails` first via `grep -n "UpdateLifecycleField\|UpdateDetails" backend/internal/costs/repository_mongo_test.go` and update their call sites in the same edit, since the signature is changing for everyone, not just new tests):

```go
func TestMongoUpdateLifecycleFieldRejectsStaleRevision(t *testing.T) {
	db := setupDB(t)
	repo := newCostRepo(t, db) // reuse this file's existing repo-construction helper; confirm exact name via grep
	ctx := context.Background()

	created, err := repo.Create(ctx, CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: CostCategoryMaterial,
		Description: "Cement", Currency: "MYR", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 0 {
		t.Fatalf("a new cost item must start at Revision 0, got %d", created.Revision)
	}

	if _, err := repo.UpdateLifecycleField(ctx, "company_a", created.ID, 0, CostStageEstimated, money.New(50000, "MYR")); err != nil {
		t.Fatal(err)
	}
	// Revision is now 1; replaying the original expectation must fail.
	if _, err := repo.UpdateLifecycleField(ctx, "company_a", created.ID, 0, CostStageEstimated, money.New(60000, "MYR")); !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

// A document written before Revision existed has no "revision" field at all
// in Mongo — not revision:0, genuinely absent. expectedRevision:0 must still
// match it, and the write must set revision:1, or every pre-existing cost
// item's first edit would incorrectly report a stale-revision conflict.
func TestMongoUpdateLifecycleFieldTreatsAMissingRevisionFieldAsZero(t *testing.T) {
	db := setupDB(t)
	repo := newCostRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: CostCategoryMaterial,
		Description: "Legacy cement row", Currency: "MYR", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a document that predates the Revision field entirely by
	// unsetting it directly against the collection this test's repo owns —
	// find this file's existing way to reach the raw *mongo.Collection for a
	// repo under test (grep for how other tests in this file reach into the
	// collection directly, if any do; otherwise expose the collection via
	// whatever constructor/test-only accessor this file's own conventions
	// already provide, and use $unset here to remove "revision").
	// db.Collection("cost_items").UpdateOne(ctx, bson.M{"_id": ...}, bson.M{"$unset": bson.M{"revision": ""}})

	updated, err := repo.UpdateLifecycleField(ctx, "company_a", created.ID, 0, CostStageActual, money.New(85000, "MYR"))
	if err != nil {
		t.Fatalf("expectedRevision:0 must match a document with no revision field at all, got: %v", err)
	}
	if updated.Revision != 1 {
		t.Fatalf("Revision after first write on a legacy document = %d, want 1", updated.Revision)
	}
}
```

Adjust the exact repository-construction helper name to whatever this file already uses (grep first — do not invent a new helper if one already exists for these tests), and fill in the exact mechanism this test file already uses (or needs to add once) to reach the raw collection for a targeted `$unset`, matching this file's established conventions rather than inventing a new one.

- [ ] **Step 2: Run both new tests to confirm they fail**

Run: `cd backend && go test ./internal/costs/... -run 'TestMongoUpdateLifecycleFieldRejectsStaleRevision|TestMongoUpdateLifecycleFieldTreatsAMissingRevisionFieldAsZero' -v`
Expected: FAIL — compile error, since `UpdateLifecycleField` does not yet take an `expectedRevision` parameter and `ErrRevisionMismatch` does not yet exist in this package.

- [ ] **Step 3: Add `Revision` to the `CostItem` struct**

In `backend/internal/costs/cost_item.go`, add `Revision int64` to the struct, and update the doc comment:

```go
// CostItem is the single authoritative Project-cost ledger record. Estimated,
// Committed, Actual, and Paid are independently nilable and may all be
// non-nil simultaneously (design spec §1.5) — never collapsed into a single
// status field. WorkItemID and Quantity/UnitPrice are optional: a CostItem
// may be Project-level with no specific WorkItem (e.g. a permit fee), and
// lump-sum costs (subcontractor, permit, professional fee) have no natural
// quantity. Paid is a cumulative running total, not a per-payment event
// (design spec §1.5, §22-E). Revision guards every conditional write —
// UpdateLifecycleField and UpdateDetails both require the caller's
// expectedRevision to match the stored value, mirroring the same CAS idiom
// used by estimates.Estimate and materialrequirements.MaterialRequirement.
// A document written before this field existed has no "revision" key at
// all; expectedRevision:0 matches such a document too (see
// conditionalUpdate in repository_mongo.go), so a pre-existing CostItem's
// first edit under this scheme is treated exactly like a real revision-0
// document rather than rejected as a conflict.
type CostItem struct {
	ID            string             `bson:"_id,omitempty" json:"id"`
	CompanyID     string             `bson:"companyId" json:"companyId"`
	ProjectID     string             `bson:"projectId" json:"projectId"`
	WorkItemID    *string            `bson:"workItemId,omitempty" json:"workItemId,omitempty"`
	Category      CostCategory       `bson:"category" json:"category"`
	Description   string             `bson:"description" json:"description"`
	Quantity      *quantity.Quantity `bson:"-" json:"-"` // never BSON-marshaled directly — see quantityDoc in repository_mongo.go
	UnitPrice     *money.Money       `bson:"unitPrice,omitempty" json:"unitPrice,omitempty"`
	MaterialID    *string            `bson:"materialId,omitempty" json:"materialId,omitempty"`
	Estimated     *money.Money       `bson:"estimated,omitempty" json:"estimated,omitempty"`
	Committed     *money.Money       `bson:"committed,omitempty" json:"committed,omitempty"`
	Actual        *money.Money       `bson:"actual,omitempty" json:"actual,omitempty"`
	Paid          *money.Money       `bson:"paid,omitempty" json:"paid,omitempty"`
	Currency      string             `bson:"currency" json:"currency"`
	Date          time.Time          `bson:"date" json:"date"`
	Notes         string             `bson:"notes,omitempty" json:"notes,omitempty"`
	Revision      int64              `bson:"revision" json:"revision"`
	ActualCorrections []ActualCorrection `bson:"actualCorrections,omitempty" json:"actualCorrections,omitempty"`
	CreatedAt     time.Time          `bson:"createdAt" json:"createdAt"`
	SchemaVersion int                `bson:"schemaVersion" json:"schemaVersion"`
}
```

(`ActualCorrections` is defined and populated starting in Task B2 — added to the struct here so Task B1's doc-round-trip work only has to touch this file once. It is `nil`/empty for every document until Task B2 lands, which is fine — `omitempty` keeps existing documents byte-identical.)

- [ ] **Step 4: Update `costItemDoc`/`toCostItemDoc`/`fromCostItemDoc` to carry Revision (and the not-yet-populated `ActualCorrections` field, for Task B2's benefit)**

In `backend/internal/costs/repository_mongo.go`, add `Revision int64 \`bson:"revision"\`` to `costItemDoc`, and thread it through both conversion functions (add `Revision: c.Revision` in `toCostItemDoc`, `Revision: doc.Revision` in `fromCostItemDoc`). Leave `ActualCorrections` itself to Task B2 — do not add its BSON representation here, since the type doesn't exist until that task; Step 3 only added the Go struct field as a placeholder so the type declaration in this task is complete, but its actual Mongo persistence is Task B2's job. If leaving an as-yet-untyped field on `CostItem` in Step 3 is awkward given Go's compile ordering, move the `ActualCorrections []ActualCorrection` struct-field addition into Task B2 Step 1 instead of here — either ordering is fine as long as it lands before Task B2 needs it and does not require touching `cost_item.go` twice for the same field.

- [ ] **Step 5: Rewrite `UpdateLifecycleField` and `UpdateDetails` as CAS-guarded with legacy-revision compatibility, via a shared `conditionalUpdate` helper**

Replace both methods in `backend/internal/costs/repository_mongo.go` with:

```go
// conditionalUpdate applies set against a document matching companyID/id/
// expectedRevision, bumping revision by one. On a 0-match it re-reads to
// distinguish a missing/foreign document (404) from a revision conflict
// (409) — the same pattern used by suppliers.MongoSupplierOfferingRepository
// and materialrequirements.MongoMaterialRequirementRepository.
//
// Legacy compatibility: when expectedRevision is 0, the filter matches a
// document whose "revision" field is either explicitly 0 OR absent
// entirely (any CostItem created before Revision existed) — $in against
// [0] is not enough for the missing-field case, so the filter explicitly
// covers both. Any other expectedRevision value only ever matches an
// explicit equal value, since a document that has been written at least
// once always has an explicit revision by construction.
func (r *MongoCostItemRepository) conditionalUpdate(ctx context.Context, companyID, id string,
	expectedRevision int64, set bson.M) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID}
	if expectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{"revision": bson.M{"$exists": false}},
			bson.M{"revision": int64(0)},
		}
	} else {
		filter["revision"] = expectedRevision
	}
	set["revision"] = expectedRevision + 1

	res, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return CostItem{}, err
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrCostItemNotFound) {
			return CostItem{}, ErrCostItemNotFound
		}
		return CostItem{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoCostItemRepository) UpdateLifecycleField(ctx context.Context, companyID, id string,
	expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error) {
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, bson.M{string(stage): amount})
}

func (r *MongoCostItemRepository) UpdateDetails(ctx context.Context, companyID, id string,
	expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error) {
	set := bson.M{"description": description, "notes": notes}
	if category != nil {
		set["category"] = string(*category)
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set)
}
```

Confirm `FindByID`'s existing decode path already tolerates a document with no `revision` field at all — `costItemDoc.Revision` is a plain `int64` (not `*int64`), so a missing BSON field decodes to the zero value `0` automatically via mongo-driver's default unmarshal behavior; this is exactly what makes `created.Revision == 0` already correct for a legacy row read through `FindByID`, no special-casing needed there. Remove the two old, now-superseded method bodies entirely (do not leave the old unguarded versions behind under different names).

- [ ] **Step 6: Update the repository interface**

In `backend/internal/costs/repository.go`, change:

```go
UpdateLifecycleField(ctx context.Context, companyID, id string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error)
UpdateDetails(ctx context.Context, companyID, id string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error)
```

- [ ] **Step 7: Add `ErrRevisionMismatch`, update `Service.UpdateCostItemLifecycle`/`UpdateCostItemDetails`, and keep `UpdateLabourCostEstimate`'s public signature unchanged**

In `backend/internal/costs/service.go`, add near the other sentinels:

```go
// ErrRevisionMismatch is returned when a conditional write's expectedRevision
// no longer matches the stored CostItem's Revision — the record changed
// since it was read.
var ErrRevisionMismatch = errors.New("costs: cost item changed since it was read")
```

Update both service methods to thread `expectedRevision` through:

```go
func (s *Service) UpdateCostItemLifecycle(ctx context.Context, companyID, costItemID string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error) {
	if !stage.IsValid() {
		return CostItem{}, errors.New("costs: invalid lifecycle stage")
	}
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if amount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	return s.repo.UpdateLifecycleField(ctx, companyID, costItemID, expectedRevision, stage, amount)
}
```

```go
func (s *Service) UpdateCostItemDetails(ctx context.Context, companyID, costItemID string, expectedRevision int64, description, notes string, category *CostCategory) (CostItem, error) {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if category != nil && *category != existing.Category {
		if existing.Category == CostCategoryLabour {
			return CostItem{}, ErrLabourCategoryImmutable
		}
		if *category == CostCategoryLabour {
			return CostItem{}, ErrLabourCategoryNotAllowed
		}
		if existing.Committed != nil || existing.Actual != nil || existing.Paid != nil {
			return CostItem{}, ErrCategoryLocked
		}
		if existing.MaterialID != nil && *category != CostCategoryMaterial {
			return CostItem{}, ErrMaterialIDRequiresMaterialCategory
		}
		if !category.IsValid() {
			return CostItem{}, ErrInvalidCategory
		}
	}
	return s.repo.UpdateDetails(ctx, companyID, costItemID, expectedRevision, description, notes, category)
}
```

**Do NOT change `UpdateLabourCostEstimate`'s public signature.** A module that consumes a capability (here, `internal/labour` consuming `costs.LabourCostRecorder`-style capabilities) states what should change, never a revision/CAS parameter belonging to the module that owns the write — leaking `CostItem.Revision` into `labour`'s call sites would couple that module to `costs`'s internal concurrency mechanics for no reason. Keep the existing public shape and make the service read-then-write internally instead:

```go
// UpdateLabourCostEstimate updates only the Estimated field of an existing
// labour CostItem — backs LabourEntry correction. Never touches
// Committed/Actual/Paid (design spec §1.3.2, §9.4). Reads the current
// Revision internally and retries once on a genuine concurrent-write race
// (extremely rare for a labour-linked CostItem, which only this one path
// ever writes) — callers never see or supply a revision; that is this
// service's own concurrency concern; a caller in internal/labour has no
// business knowing about CostItem's CAS mechanics.
func (s *Service) UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return err
	}
	_, err = s.repo.UpdateLifecycleField(ctx, companyID, costItemID, existing.Revision, CostStageEstimated, estimated)
	if errors.Is(err, ErrRevisionMismatch) {
		// Lost a race with some other write to the same CostItem — reread
		// once and retry with the fresh revision. A second collision is not
		// retried further; it propagates, matching this method's existing
		// error-return contract (no retry loop existed before this task
		// either, since there was no concurrency guard at all).
		existing, err = s.repo.FindByID(ctx, companyID, costItemID)
		if err != nil {
			return err
		}
		_, err = s.repo.UpdateLifecycleField(ctx, companyID, costItemID, existing.Revision, CostStageEstimated, estimated)
	}
	return err
}
```

Confirm via `grep -rn "UpdateLabourCostEstimate" backend/internal/labour/*.go` that this method's one caller in `internal/labour` needs NO changes at all — its call site's argument list is unaffected by this task, since the public signature is unchanged.

- [ ] **Step 8: Update the HTTP handler DTOs and calls**

In `backend/internal/costs/handler.go`, add `ExpectedRevision int64 \`json:"expectedRevision" required:"true"\`` to both `updateCostItemLifecycleInput.Body` and `updateCostItemDetailsInput.Body`, and thread it into both handler bodies' calls to `svc.UpdateCostItemLifecycle(...)` / `svc.UpdateCostItemDetails(...)`. Add `ErrRevisionMismatch` to `mapCostsError`'s switch, mapped to `huma.Error409Conflict("cost item changed since it was read")`, placed alongside the other 409s (`ErrCategoryLocked`, `ErrLabourCategoryImmutable`).

Also add `Revision int64 \`json:"revision"\`` to the `costItemDTO` output struct and its constructor, so the frontend can read the current revision back.

- [ ] **Step 9: Fix every other call site across the backend**

Run: `cd backend && go build ./... 2>&1 | head -60` and fix every compile error surfaced — these will be test files in `internal/costs` itself (`service_test.go` — every existing `UpdateCostItemLifecycle`/`UpdateCostItemDetails` call needs an `expectedRevision` argument added; find the current revision by reading the record created earlier in the same test, which will now be `0` for a freshly created item). `internal/labour`'s call site needs NO change per Step 7.

- [ ] **Step 10: Run the originally-failing tests and the full costs test suite**

Run: `cd backend && go test ./internal/costs/... -run 'TestMongoUpdateLifecycleFieldRejectsStaleRevision|TestMongoUpdateLifecycleFieldTreatsAMissingRevisionFieldAsZero' -v`
Expected: both PASS.

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS, no regressions (every existing test updated in Step 9 to pass the now-required `expectedRevision`).

- [ ] **Step 11: Build and vet the whole backend**

Run: `cd backend && go build ./... && go vet ./...`
Expected: clean.

---

### Task B2: Embedded `ActualCorrections` and the `CorrectCostItemActual`/`RecordCostItemActual` service methods

**Files:**
- Modify: `backend/internal/costs/cost_item.go`
- Modify: `backend/internal/costs/repository.go`
- Modify: `backend/internal/costs/repository_mongo.go`
- Modify: `backend/internal/costs/service.go`
- Test: `backend/internal/costs/correction_test.go`
- Test: `backend/internal/costs/repository_mongo_test.go`

**Interfaces:**
- Consumes: `CostItem.Revision` (Task B1), `money.Money`.
- Produces: `ActualCorrection` struct (embedded on `CostItem`, not a separate collection); `CostItemRepository.RecordActual(ctx, companyID, id string, expectedRevision int64, amount money.Money) (CostItem, error)` (sets Actual for the FIRST time — no prior value exists, nothing to preserve); `CostItemRepository.CorrectActual(ctx, companyID, id string, expectedRevision int64, newAmount money.Money, correction ActualCorrection) (CostItem, error)` (one atomic `$set`+`$push` `UpdateOne` — CAS-guards the write AND appends the correction record in the same operation, no second collection, no transaction, no replica-set requirement); `Service.RecordCostItemActual(ctx, companyID, actorUserID, costItemID string, expectedRevision int64, amount money.Money) (CostItem, error)` (errors if Actual is already set — that path is `CorrectCostItemActual`, not this one); `Service.CorrectCostItemActual(ctx, companyID, actorUserID, costItemID string, expectedRevision int64, newAmount money.Money, reason string) (CostItem, error)` (errors if Actual is NOT yet set — nothing to correct). Consumed by Task B3 (HTTP handler, two separate routes) and the frontend Record/Correct flow.

This task replaces the plan's earlier draft, which used a separate `cost_corrections` collection and a Mongo transaction (`session.WithTransaction`, mirroring `supplieroffers/eligibility_repository_mongo.go`). That was unnecessary complexity for this requirement: corrections are rare, bounded in count per cost item, and never queried across cost items — an embedded, append-only array on `CostItem` itself gives the exact same guarantee (CAS + audit history + atomicity) as a single `UpdateOne` with `$set` and `$push` together, with no second collection to create/index/clean up, no replica-set requirement, and no runtime type-assertion capability check. If the correction history is ever needed as a first-class queryable entity across cost items (e.g. a company-wide "all corrections this month" report), promoting it to its own collection at that point is a normal, well-scoped follow-up — not a reason to build that infrastructure now for a feature that doesn't need it.

- [ ] **Step 1: Define `ActualCorrection` on the `CostItem` model**

In `backend/internal/costs/cost_item.go`, add the type (if Task B1 Step 3 didn't already add the `ActualCorrections` field to `CostItem` itself, add it now):

```go
// ActualCorrection records that a CostItem's Actual amount was corrected:
// what it was, what it became, why, who did it, and when. Appended to
// CostItem.ActualCorrections in insertion order — never rewritten or
// removed once appended, and the list is never reordered; ordering is
// therefore already "oldest first" as stored, with no separate sort
// required when reading it back.
type ActualCorrection struct {
	PreviousAmount  money.Money `bson:"previousAmount" json:"previousAmount"`
	NewAmount       money.Money `bson:"newAmount" json:"newAmount"`
	Reason          string      `bson:"reason" json:"reason"`
	CorrectedByUser string      `bson:"correctedByUser" json:"correctedByUser"`
	CorrectedAt     time.Time   `bson:"correctedAt" json:"correctedAt"`
}
```

Confirm `CostItem.ActualCorrections []ActualCorrection` exists on the struct (added in Task B1 Step 3, or add it here now if that step deferred it) with tags `bson:"actualCorrections,omitempty" json:"actualCorrections,omitempty"`.

- [ ] **Step 2: Write the failing service-level tests for Record and Correct**

Create `backend/internal/costs/correction_test.go`:

```go
package costs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func TestRecordCostItemActualSetsTheFirstValueWithNoCorrectionRecord(t *testing.T) {
	svc, _, _ := newService(t) // reuse this package's existing test-service constructor; confirm exact name via grep in service_test.go
	created := createCostItem(t, svc, costs.CostCategoryMaterial) // reuse or add a small helper matching this file's existing test-fixture conventions; confirm via grep

	updated, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision,
		money.New(85000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error recording the first actual: %v", err)
	}
	if updated.Actual == nil || updated.Actual.Amount != 85000 {
		t.Fatalf("Actual = %v, want 85000", updated.Actual)
	}
	if len(updated.ActualCorrections) != 0 {
		t.Fatalf("recording the FIRST actual must not create a correction record, got %d", len(updated.ActualCorrections))
	}
}

func TestRecordCostItemActualRejectsWhenActualAlreadySet(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)

	first, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, first.Revision, money.New(90000, "MYR"))
	if !errors.Is(err, costs.ErrActualAlreadyRecorded) {
		t.Fatalf("error = %v, want ErrActualAlreadyRecorded — a second attempt must use Correct, not Record", err)
	}
}

func TestCorrectCostItemActualPreservesThePreviousValue(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	corrected, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision,
		money.New(8500, "MYR"), "Original entry was a data-entry error — decimal point")
	if err != nil {
		t.Fatalf("unexpected error correcting actual: %v", err)
	}
	if corrected.Actual == nil || corrected.Actual.Amount != 8500 {
		t.Fatalf("Actual after correction = %v, want 8500", corrected.Actual)
	}
	if len(corrected.ActualCorrections) != 1 {
		t.Fatalf("expected exactly 1 correction record, got %d", len(corrected.ActualCorrections))
	}
	if corrected.ActualCorrections[0].PreviousAmount.Amount != 85000 || corrected.ActualCorrections[0].NewAmount.Amount != 8500 {
		t.Errorf("correction record = %+v, want previous=85000 new=8500", corrected.ActualCorrections[0])
	}
	if corrected.ActualCorrections[0].Reason != "Original entry was a data-entry error — decimal point" {
		t.Errorf("Reason = %q", corrected.ActualCorrections[0].Reason)
	}
}

func TestCorrectCostItemActualAppendsRatherThanReplacesEarlierCorrections(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(850000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}
	afterFirstCorrection, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision,
		money.New(85000, "MYR"), "First correction")
	if err != nil {
		t.Fatal(err)
	}
	afterSecondCorrection, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterFirstCorrection.Revision,
		money.New(8500, "MYR"), "Second correction")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterSecondCorrection.ActualCorrections) != 2 {
		t.Fatalf("expected 2 correction records after 2 corrections, got %d", len(afterSecondCorrection.ActualCorrections))
	}
	if afterSecondCorrection.ActualCorrections[0].PreviousAmount.Amount != 850000 || afterSecondCorrection.ActualCorrections[0].NewAmount.Amount != 85000 {
		t.Errorf("first correction record = %+v", afterSecondCorrection.ActualCorrections[0])
	}
	if afterSecondCorrection.ActualCorrections[1].PreviousAmount.Amount != 85000 || afterSecondCorrection.ActualCorrections[1].NewAmount.Amount != 8500 {
		t.Errorf("second correction record = %+v", afterSecondCorrection.ActualCorrections[1])
	}
}

func TestCorrectCostItemActualRejectsWhenActualNotYetSet(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)

	_, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision,
		money.New(8500, "MYR"), "some reason")
	if !errors.Is(err, costs.ErrNoActualToCorrect) {
		t.Fatalf("error = %v, want ErrNoActualToCorrect — a first entry must use Record, not Correct", err)
	}
}

func TestCorrectCostItemActualRequiresAReason(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision,
		money.New(8500, "MYR"), "")
	if !errors.Is(err, costs.ErrCorrectionReasonRequired) {
		t.Fatalf("error = %v, want ErrCorrectionReasonRequired", err)
	}
}

func TestCorrectCostItemActualRejectsStaleRevision(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision+1,
		money.New(8500, "MYR"), "some reason")
	if !errors.Is(err, costs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

func TestCorrectCostItemActualIsTenantScoped(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CorrectCostItemActual(context.Background(), "company_b", "user_1", created.ID, afterRecord.Revision,
		money.New(8500, "MYR"), "some reason")
	if !errors.Is(err, costs.ErrCostItemNotFound) {
		t.Fatalf("error = %v, want ErrCostItemNotFound", err)
	}
}
```

Before writing this, run `grep -n "func createCostItem\|func newService" backend/internal/costs/service_test.go` to confirm the exact existing helper names/signatures and adjust the test above to call them correctly rather than assuming — these are illustrative names, the real helpers in this file may differ (e.g. may need a `CostCategory` param or may build via `svc.CreateCostItem` directly with specific args). Match this test file's existing fixture style exactly.

- [ ] **Step 3: Run the new tests to confirm they fail to compile**

Run: `cd backend && go test ./internal/costs/... -run 'TestRecordCostItemActual|TestCorrectCostItemActual' -v`
Expected: FAIL — compile error, `RecordCostItemActual`/`CorrectCostItemActual`/`ErrActualAlreadyRecorded`/`ErrNoActualToCorrect`/`ErrCorrectionReasonRequired` do not exist yet.

- [ ] **Step 4: Add the two repository methods**

In `backend/internal/costs/repository.go`, add to `CostItemRepository`:

```go
// RecordActual sets Actual for the first time (no prior value must exist —
// the SERVICE enforces that precondition via FindByID before calling this;
// the repository itself just performs the CAS-guarded $set). Use
// CorrectActual once Actual is already populated.
RecordActual(ctx context.Context, companyID, id string, expectedRevision int64, amount money.Money) (CostItem, error)

// CorrectActual atomically (a) sets Actual to newAmount, (b) bumps
// revision, and (c) appends correction to ActualCorrections — all in ONE
// Mongo UpdateOne combining $set and $push, guarded by the same
// companyId+_id+expectedRevision filter every other conditional write in
// this package uses. No transaction and no second collection: MongoDB
// guarantees a single document's $set+$push in one UpdateOne is atomic.
CorrectActual(ctx context.Context, companyID, id string, expectedRevision int64, newAmount money.Money, correction ActualCorrection) (CostItem, error)
```

In `backend/internal/costs/repository_mongo.go`, implement both using the same `conditionalUpdate` helper from Task B1 — extend `conditionalUpdate` to optionally accept a `$push` clause, since `RecordActual` needs only `$set` but `CorrectActual` needs both in the same operation:

```go
// conditionalUpdateWithPush is conditionalUpdate's sibling for the one case
// that needs $push alongside $set in the same atomic operation. push may be
// nil (conditionalUpdate itself remains the $set-only path other callers
// use and is unchanged by this task).
func (r *MongoCostItemRepository) conditionalUpdateWithPush(ctx context.Context, companyID, id string,
	expectedRevision int64, set bson.M, push bson.M) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID}
	if expectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{"revision": bson.M{"$exists": false}},
			bson.M{"revision": int64(0)},
		}
	} else {
		filter["revision"] = expectedRevision
	}
	set["revision"] = expectedRevision + 1

	update := bson.M{"$set": set}
	if push != nil {
		update["$push"] = push
	}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return CostItem{}, err
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrCostItemNotFound) {
			return CostItem{}, ErrCostItemNotFound
		}
		return CostItem{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoCostItemRepository) RecordActual(ctx context.Context, companyID, id string,
	expectedRevision int64, amount money.Money) (CostItem, error) {
	return r.conditionalUpdateWithPush(ctx, companyID, id, expectedRevision, bson.M{"actual": amount}, nil)
}

func (r *MongoCostItemRepository) CorrectActual(ctx context.Context, companyID, id string,
	expectedRevision int64, newAmount money.Money, correction ActualCorrection) (CostItem, error) {
	return r.conditionalUpdateWithPush(ctx, companyID, id, expectedRevision,
		bson.M{"actual": newAmount},
		bson.M{"actualCorrections": correction})
}
```

Update `conditionalUpdate` (Task B1's version) to simply call `conditionalUpdateWithPush(ctx, companyID, id, expectedRevision, set, nil)` internally, so the legacy-revision filter logic exists in exactly one place rather than being duplicated between the two methods.

- [ ] **Step 5: Add the sentinels and the two service methods**

In `backend/internal/costs/service.go`, add near the other sentinels:

```go
// ErrActualAlreadyRecorded is returned when RecordCostItemActual is called
// on a CostItem that already has a non-nil Actual — use CorrectCostItemActual
// instead, which preserves the previous value.
var ErrActualAlreadyRecorded = errors.New("costs: actual is already recorded; use Correct to change it")

// ErrNoActualToCorrect is returned when CorrectCostItemActual is called on a
// CostItem whose Actual has never been set — use RecordCostItemActual for
// the first entry, since there is nothing to preserve yet.
var ErrNoActualToCorrect = errors.New("costs: no actual value has been recorded yet; use Record instead of Correct")

// ErrCorrectionReasonRequired is returned when CorrectCostItemActual is
// called with a blank reason — every correction must state why.
var ErrCorrectionReasonRequired = errors.New("costs: a reason is required to correct a previously recorded actual amount")
```

Add the two methods:

```go
// RecordCostItemActual sets Actual for the first time. Rejected with
// ErrActualAlreadyRecorded if a value is already present — that path is
// CorrectCostItemActual, which preserves the previous value instead of
// silently overwriting it.
func (s *Service) RecordCostItemActual(ctx context.Context, companyID, actorUserID, costItemID string,
	expectedRevision int64, amount money.Money) (CostItem, error) {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if existing.Actual != nil {
		return CostItem{}, ErrActualAlreadyRecorded
	}
	if amount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	return s.repo.RecordActual(ctx, companyID, costItemID, expectedRevision, amount)
}

// CorrectCostItemActual corrects an ALREADY-RECORDED Actual to newAmount,
// requiring a non-blank reason and the caller's expectedRevision to match
// the stored CostItem. The previous value is appended to
// CostItem.ActualCorrections in the same atomic write that updates Actual
// itself (repo.CorrectActual: one UpdateOne with $set+$push, no
// transaction, no second collection) — a rejected write (stale revision,
// wrong tenant) appends nothing, since it never matches any document.
// Rejected with ErrNoActualToCorrect if Actual has never been set — that
// path is RecordCostItemActual.
func (s *Service) CorrectCostItemActual(ctx context.Context, companyID, actorUserID, costItemID string,
	expectedRevision int64, newAmount money.Money, reason string) (CostItem, error) {

	if strings.TrimSpace(reason) == "" {
		return CostItem{}, ErrCorrectionReasonRequired
	}
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if existing.Actual == nil {
		return CostItem{}, ErrNoActualToCorrect
	}
	if newAmount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	correction := ActualCorrection{
		PreviousAmount: *existing.Actual, NewAmount: newAmount,
		Reason: strings.TrimSpace(reason), CorrectedByUser: actorUserID,
		CorrectedAt: time.Now(),
	}
	return s.repo.CorrectActual(ctx, companyID, costItemID, expectedRevision, newAmount, correction)
}
```

`"strings"` and `"time"` are already imported in this file per Task B1.

- [ ] **Step 6: Run the new tests**

Run: `cd backend && go build ./... 2>&1 | head -60` — fix any compile errors first (e.g. the in-memory/fake repository used by `newService`'s test helper, if one exists separately from the real Mongo repo, needs `RecordActual`/`CorrectActual` added too — check via `grep -rn "CostItemRepository" backend/internal/costs/*_test.go`).

Run: `cd backend && go test ./internal/costs/... -run 'TestRecordCostItemActual|TestCorrectCostItemActual' -v`
Expected: all 7 tests PASS.

- [ ] **Step 7: Add a real-Mongo test proving atomicity of the combined $set+$push, including the legacy-revision case**

Add to `backend/internal/costs/repository_mongo_test.go`:

```go
func TestMongoCorrectActualAppendsExactlyOneRecordAndUpdatesActualAtomically(t *testing.T) {
	db := setupDB(t)
	repo := newCostRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: CostCategoryMaterial,
		Description: "Cement", Currency: "MYR", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstActual := money.New(85000, "MYR")
	afterRecord, err := repo.RecordActual(ctx, "company_a", created.ID, created.Revision, firstActual)
	if err != nil {
		t.Fatal(err)
	}

	correction := ActualCorrection{
		PreviousAmount: firstActual, NewAmount: money.New(8500, "MYR"),
		Reason: "Decimal point error", CorrectedByUser: "user_1", CorrectedAt: time.Now(),
	}
	corrected, err := repo.CorrectActual(ctx, "company_a", created.ID, afterRecord.Revision, money.New(8500, "MYR"), correction)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if corrected.Actual == nil || corrected.Actual.Amount != 8500 {
		t.Fatalf("Actual = %v, want 8500", corrected.Actual)
	}
	if len(corrected.ActualCorrections) != 1 || corrected.ActualCorrections[0].PreviousAmount.Amount != 85000 {
		t.Fatalf("ActualCorrections = %+v, want exactly 1 entry with PreviousAmount=85000", corrected.ActualCorrections)
	}
}

func TestMongoCorrectActualRejectsStaleRevisionAndAppendsNothing(t *testing.T) {
	db := setupDB(t)
	repo := newCostRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: CostCategoryMaterial,
		Description: "Cement", Currency: "MYR", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstActual := money.New(85000, "MYR")
	afterRecord, err := repo.RecordActual(ctx, "company_a", created.ID, created.Revision, firstActual)
	if err != nil {
		t.Fatal(err)
	}

	correction := ActualCorrection{
		PreviousAmount: firstActual, NewAmount: money.New(8500, "MYR"),
		Reason: "attempted with stale revision", CorrectedByUser: "user_1", CorrectedAt: time.Now(),
	}
	// created.Revision (0) is now stale — the real current revision is afterRecord.Revision (1).
	_, err = repo.CorrectActual(ctx, "company_a", created.ID, created.Revision, money.New(8500, "MYR"), correction)
	if !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}

	unchanged, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.ActualCorrections) != 0 {
		t.Fatalf("a rejected correction must append NOTHING (the whole UpdateOne matches zero documents when the filter fails), got %d", len(unchanged.ActualCorrections))
	}
	if unchanged.Actual.Amount != 85000 {
		t.Fatalf("Actual must be untouched by a rejected correction, got %v", unchanged.Actual)
	}
}
```

- [ ] **Step 8: Run the new Mongo tests**

Run: `cd backend && go test ./internal/costs/... -run 'TestMongoCorrectActual' -v`
Expected: both PASS. No Testcontainers replica-set option is needed for these — a plain standalone `mongo:7` container (this package's existing default, unchanged by this task) is sufficient, since there is no transaction and no cross-collection write.

- [ ] **Step 9: Run the full costs package suite**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS, no regressions.

- [ ] **Step 10: Confirm `costs.DeleteAllForCompany` needs no change**

`ActualCorrections` lives inside the same `cost_items` document `DeleteAllForCompany` already deletes wholesale — there is no second collection for the demo-seed cleanup inventory (`internal/demoseed`) to learn about. Run `grep -n "cost_items\|CostItem" backend/internal/demoseed/*.go` to confirm nothing there references a `cost_corrections` collection or expects a second cleanup entry — there should be no changes needed in `internal/demoseed` for this task at all. If this grep reveals demoseed already lists a fixed count of collections that `costs` owns and asserts on it somewhere, confirm the count is unaffected (still one collection, `cost_items`).

---

### Task B3: HTTP routes for cost-item edit/record/correct, and closing the lifecycle-route Actual bypass

**Files:**
- Modify: `backend/internal/costs/handler.go`
- Test: `backend/internal/costs/handler_test.go`
- Test: `backend/internal/tenanttest/` — find the existing tenant-isolation test file covering `/cost-items` routes via `grep -rln "cost-items" backend/internal/tenanttest/*.go` and add to that file rather than creating a new one.

**Interfaces:**
- Consumes: `Service.RecordCostItemActual`, `Service.CorrectCostItemActual` (Task B2), `Service.UpdateCostItemLifecycle`/`UpdateCostItemDetails` with their new `expectedRevision` param (Task B1).
- Produces: `PATCH /cost-items/{id}/lifecycle` (existing route — now requires `expectedRevision` in body per Task B1, AND rejects `stage=actual` outright, see below); `POST /cost-items/{id}/record-actual` (new route, first-time Actual entry); `POST /cost-items/{id}/correct-actual` (new route, corrects an already-recorded Actual). No separate corrections-list route — `ActualCorrections` is embedded and already returned on `costItemDTO`. Consumed by the frontend in Task B5/B6.

**Closing the lifecycle bypass (mandatory — this was the biggest semantic hole in the plan's earlier draft):** as originally drafted, `PATCH /cost-items/{id}/lifecycle` with `stage=actual` could still blindly overwrite Actual, completely bypassing the new Record/Correct split and its audit trail. The service-level rule going forward:

- `stage=estimated` or `stage=committed` → ordinary CAS edit via the existing `UpdateCostItemLifecycle` — no history needed, this is mutable/draft-like data.
- `stage=actual` → **REJECTED outright** by `UpdateCostItemLifecycle` itself, at the service layer, regardless of whether Actual is currently nil or already set. The caller must use `POST /cost-items/{id}/record-actual` (nil case) or `POST /cost-items/{id}/correct-actual` (already-set case) instead — there is no bypass path left.
- `stage=paid` → left as today (no destructive correction is introduced for Paid in this task at all — the existing `UpdateCostItemLifecycle` call for Paid is UNCHANGED, since redesigning Paid's semantics is explicitly out of scope; only `stage=actual` gets the new rejection).

- [ ] **Step 1: Add the failing service-level test that `UpdateCostItemLifecycle` rejects `stage=actual`**

Add to `backend/internal/costs/service_test.go` (near the other `UpdateCostItemLifecycle` tests):

```go
func TestUpdateCostItemLifecycleRejectsActualStage(t *testing.T) {
	svc, _, _ := newService(t)
	created := createCostItem(t, svc, costs.CostCategoryMaterial)

	_, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, created.Revision,
		costs.CostStageActual, money.New(8500, "MYR"))
	if !errors.Is(err, costs.ErrActualMustUseRecordOrCorrect) {
		t.Fatalf("error = %v, want ErrActualMustUseRecordOrCorrect", err)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `cd backend && go test ./internal/costs/... -run TestUpdateCostItemLifecycleRejectsActualStage -v`
Expected: FAIL — compile error, `ErrActualMustUseRecordOrCorrect` does not exist yet.

- [ ] **Step 3: Add the sentinel and reject `stage=actual` in `UpdateCostItemLifecycle`**

In `backend/internal/costs/service.go`, add near the other sentinels:

```go
// ErrActualMustUseRecordOrCorrect is returned when UpdateCostItemLifecycle
// is called with stage=actual. Actual has its own dedicated, auditable
// entry points — RecordCostItemActual for the first value, CorrectCostItemActual
// for changing an already-recorded one — specifically so a blind lifecycle
// overwrite can never bypass the correction history. Estimated and
// Committed have no such requirement and remain reachable through this
// method unchanged.
var ErrActualMustUseRecordOrCorrect = errors.New("costs: actual cannot be set through the lifecycle endpoint; use record-actual or correct-actual")
```

Update `UpdateCostItemLifecycle` (from Task B1) to check this first:

```go
func (s *Service) UpdateCostItemLifecycle(ctx context.Context, companyID, costItemID string, expectedRevision int64, stage CostStage, amount money.Money) (CostItem, error) {
	if !stage.IsValid() {
		return CostItem{}, errors.New("costs: invalid lifecycle stage")
	}
	if stage == CostStageActual {
		return CostItem{}, ErrActualMustUseRecordOrCorrect
	}
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if amount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	return s.repo.UpdateLifecycleField(ctx, companyID, costItemID, expectedRevision, stage, amount)
}
```

- [ ] **Step 4: Run the test again**

Run: `cd backend && go test ./internal/costs/... -run TestUpdateCostItemLifecycleRejectsActualStage -v`
Expected: PASS.

- [ ] **Step 5: Write the failing handler test for the new record/correct routes and the closed lifecycle bypass**

Add to `backend/internal/costs/handler_test.go` (check this file's existing router-construction helper name first via `grep -n "func new.*Router" backend/internal/costs/handler_test.go`):

```go
func TestLifecycleRouteRejectsActualStage(t *testing.T) {
	router, svc := newCostsHandlerTestRouter(t) // confirm exact helper name/signature via grep first
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Cement", nil, nil, nil, nil, nil, int64Ptr(50000), nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}

	response := doPatchRequest(t, router, http.MethodPatch, "/cost-items/"+created.ID+"/lifecycle", map[string]any{
		"expectedRevision": created.Revision,
		"stage":            "actual",
		"amount":           8500,
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (ErrActualMustUseRecordOrCorrect), body %s", response.Code, response.Body.String())
	}
}

func TestRecordActualRequestBoundary(t *testing.T) {
	router, svc := newCostsHandlerTestRouter(t)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Cement", nil, nil, nil, nil, nil, int64Ptr(50000), nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}

	response := doPatchRequest(t, router, http.MethodPost, "/cost-items/"+created.ID+"/record-actual", map[string]any{
		"expectedRevision": created.Revision,
		"amountMinor":      850000,
		"currency":         "MYR",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestCorrectActualRequestBoundary(t *testing.T) {
	router, svc := newCostsHandlerTestRouter(t)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Cement", nil, nil, nil, nil, nil, int64Ptr(50000), nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}

	response := doPatchRequest(t, router, http.MethodPost, "/cost-items/"+created.ID+"/correct-actual", map[string]any{
		"expectedRevision": created.Revision,
		"newAmountMinor":   8500,
		"currency":         "MYR",
		"reason":           "",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank reason: status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}
```

Adjust helper names (`newCostsHandlerTestRouter`, `doPatchRequest`, `int64Ptr`) to whatever this file's existing tests already use — grep first, do not invent new helper names if equivalents exist.

- [ ] **Step 6: Run all three to confirm they fail**

Run: `cd backend && go test ./internal/costs/... -run 'TestLifecycleRouteRejectsActualStage|TestRecordActualRequestBoundary|TestCorrectActualRequestBoundary' -v`
Expected: FAIL — `TestLifecycleRouteRejectsActualStage` fails because `mapCostsError` doesn't map the new sentinel yet; the other two 404 (routes don't exist yet).

- [ ] **Step 7: Add the DTOs and routes to `handler.go`**

Add near the existing `updateCostItemLifecycleInput`:

```go
type recordActualInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
		AmountMinor      int64  `json:"amountMinor" required:"true"`
		Currency         string `json:"currency" required:"true"`
	}
}

type correctActualInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
		NewAmountMinor   int64  `json:"newAmountMinor" required:"true"`
		Currency         string `json:"currency" required:"true"`
		Reason           string `json:"reason" required:"true" minLength:"1"`
	}
}
```

Check the exact name of the money-DTO shape already used in `costItemDTO` (grep `grep -n "type.*DTO\|json:\"amount\"" backend/internal/costs/handler.go`) — `ActualCorrection` needs the same shape added to `costItemDTO`'s output (a `actualCorrections` array field mirroring the domain struct: `previousAmount`, `newAmount`, `reason`, `correctedByUser`, `correctedAt`), and `toCostItemDTO`'s constructor needs to populate it from `CostItem.ActualCorrections`. No separate list-corrections DTO or route is needed — the corrections are already part of the CostItem the frontend already fetches.

Register the two new routes inside `RegisterHandlers`, right after the existing `/cost-items/{id}/lifecycle` registration:

```go
huma.Register(api, huma.Operation{
	OperationID: "cost-items-record-actual",
	Method:      http.MethodPost,
	Path:        "/cost-items/{id}/record-actual",
	Summary:     "Record the Actual amount for the first time",
}, func(ctx context.Context, input *recordActualInput) (*costItemOutput, error) {
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	updated, err := svc.RecordCostItemActual(ctx, principal.CompanyID, principal.UserID, input.ID,
		input.Body.ExpectedRevision, money.New(input.Body.AmountMinor, input.Body.Currency))
	if err != nil {
		return nil, mapCostsError(err)
	}
	return &costItemOutput{Body: toCostItemDTO(updated)}, nil
})

huma.Register(api, huma.Operation{
	OperationID: "cost-items-correct-actual",
	Method:      http.MethodPost,
	Path:        "/cost-items/{id}/correct-actual",
	Summary:     "Correct an already-recorded Actual amount, preserving the previous value",
}, func(ctx context.Context, input *correctActualInput) (*costItemOutput, error) {
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	updated, err := svc.CorrectCostItemActual(ctx, principal.CompanyID, principal.UserID, input.ID,
		input.Body.ExpectedRevision, money.New(input.Body.NewAmountMinor, input.Body.Currency), input.Body.Reason)
	if err != nil {
		return nil, mapCostsError(err)
	}
	return &costItemOutput{Body: toCostItemDTO(updated)}, nil
})
```

Add the three new sentinels to `mapCostsError`'s switch: `ErrActualMustUseRecordOrCorrect` → `huma.Error409Conflict(...)` (alongside `ErrCategoryLocked`/`ErrLabourCategoryImmutable`); `ErrActualAlreadyRecorded` → `huma.Error409Conflict(...)`; `ErrNoActualToCorrect` → `huma.Error409Conflict(...)`; `ErrCorrectionReasonRequired` → `huma.Error422UnprocessableEntity(...)`.

- [ ] **Step 8: Run all the new handler tests**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS, no regressions.

- [ ] **Step 9: Add a happy-path handler test proving the full Record → Correct flow through HTTP**

Add to `handler_test.go`:

```go
func TestRecordThenCorrectActualSucceedsAndReturnsHistoryOnTheCostItem(t *testing.T) {
	router, svc := newCostsHandlerTestRouter(t)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Cement", nil, nil, nil, nil, nil, int64Ptr(50000), nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}

	response := doPatchRequest(t, router, http.MethodPost, "/cost-items/"+created.ID+"/record-actual", map[string]any{
		"expectedRevision": created.Revision,
		"amountMinor":      850000,
		"currency":         "MYR",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("record status = %d, body %s", response.Code, response.Body.String())
	}
	var afterRecord struct{ Revision int64 `json:"revision"` }
	if err := json.Unmarshal(response.Body.Bytes(), &afterRecord); err != nil {
		t.Fatal(err)
	}

	response2 := doPatchRequest(t, router, http.MethodPost, "/cost-items/"+created.ID+"/correct-actual", map[string]any{
		"expectedRevision": afterRecord.Revision,
		"newAmountMinor":   85000,
		"currency":         "MYR",
		"reason":           "Decimal point error in original entry",
	})
	if response2.Code != http.StatusOK {
		t.Fatalf("correct status = %d, body %s", response2.Code, response2.Body.String())
	}
	var body struct {
		Actual            struct{ Amount int64 `json:"amount"` } `json:"actual"`
		ActualCorrections []struct {
			PreviousAmount struct{ Amount int64 `json:"amount"` } `json:"previousAmount"`
			NewAmount      struct{ Amount int64 `json:"amount"` } `json:"newAmount"`
			Reason         string                                `json:"reason"`
		} `json:"actualCorrections"`
	}
	if err := json.Unmarshal(response2.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Actual.Amount != 85000 {
		t.Errorf("Actual = %d, want 85000", body.Actual.Amount)
	}
	if len(body.ActualCorrections) != 1 {
		t.Fatalf("expected 1 correction on the returned CostItem, got %d", len(body.ActualCorrections))
	}
	if body.ActualCorrections[0].PreviousAmount.Amount != 850000 {
		t.Errorf("PreviousAmount = %d, want 850000", body.ActualCorrections[0].PreviousAmount.Amount)
	}
}
```

- [ ] **Step 10: Run all handler tests once more**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS, no regressions.

- [ ] **Step 11: Add tenant-isolation coverage**

Open the tenanttest file found earlier (`grep -rln "cost-items" backend/internal/tenanttest/*.go`). Find its existing cross-tenant test for `/cost-items/{id}/lifecycle` (if one exists — the file's own test list will show this) and add equivalents for both new routes:

```go
t.Run("record actual", func(t *testing.T) {
	resp := doJSON(t, router, http.MethodPost,
		"/cost-items/"+costItemID+"/record-actual", companyB.accessToken,
		map[string]any{"expectedRevision": 0, "amountMinor": 8500, "currency": "MYR"})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
	}
})

t.Run("correct actual", func(t *testing.T) {
	resp := doJSON(t, router, http.MethodPost,
		"/cost-items/"+costItemID+"/correct-actual", companyB.accessToken,
		map[string]any{"expectedRevision": 0, "newAmountMinor": 8500, "currency": "MYR", "reason": "attempted cross-tenant"})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
	}
})
```

Place these inside whatever existing `TestTenantIsolation_CostItem...`-style test function already exists in this file, following its exact established pattern (reuse its already-created `costItemID`/`companyA`/`companyB` fixtures — do not create new ones).

If the corresponding `TestTenantIsolation_AllMilestone...RoutesRequireAuth`-style route-list test exists in this same file (matching the pattern seen in `m7_procurement_test.go` from Part A), add both new routes (`POST /cost-items/{id}/record-actual`, `POST /cost-items/{id}/correct-actual`) to its route list and update its hardcoded expected-route-count assertion (the same off-by-one fix pattern used earlier this session for the material-requirements delete route).

- [ ] **Step 12: Run the tenanttest package**

Run: `cd backend && go test ./internal/tenanttest/... -run 'CostItem\|Milestone' -v`
Expected: PASS.

- [ ] **Step 13: Regenerate the OpenAPI contract**

Run: `cd backend && go run ./cmd/openapi -out ../apps/web/openapi/openapi.json`
Run: `grep -c "cost-items-record-actual\|cost-items-correct-actual" ../apps/web/openapi/openapi.json` (from `backend/`) — expect a nonzero count confirming both new operations are present.

---

### Task B4: Frontend API layer for cost lifecycle-edit, record-actual, and correct-actual

**Files:**
- Modify: `apps/web/src/features/operations/api.ts`

**Interfaces:**
- Consumes: `apiClient`, `unwrapOrThrow`, generated types (regenerate via `npm run openapi:generate` first, per Task B3 Step 13's backend regeneration).
- Produces: `updateCostLifecycle` (existing function, body shape now requires `expectedRevision` — update its call signature), `recordCostItemActual(id, body)`, `correctCostItemActual(id, body)`, consumed by Task B5. No `listCostCorrections` — corrections are embedded on the `CostItem` DTO itself and already arrive with every `GET /cost-items` response.

- [ ] **Step 1: Regenerate the frontend TypeScript client**

Run: `cd apps/web && npm run openapi:generate`
Run: `grep -n "record-actual\|correct-actual\|actualCorrections\|expectedRevision" src/lib/api/generated/schema.ts | grep -i cost` — confirm the new/changed operations, the embedded `actualCorrections` field on the CostItem DTO, and their body shapes are present.

- [ ] **Step 2: Update `updateCostLifecycle` and add the two new wrapper functions**

In `apps/web/src/features/operations/api.ts`, the existing `updateCostLifecycle` function's `body` parameter type is generated from `UpdateCostItemLifecycleInputBody`, which now includes `expectedRevision` — no code change needed here beyond confirming the type updated (TypeScript will already require it at call sites once the schema regenerates).

Add after `updateCostLifecycle`:

```ts
export async function recordCostItemActual(id: string, body: components["schemas"]["RecordActualInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/cost-items/{id}/record-actual", { params: { path: { id } }, body }));
}
export async function correctCostItemActual(id: string, body: components["schemas"]["CorrectActualInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/cost-items/{id}/correct-actual", { params: { path: { id } }, body }));
}
```

Confirm the exact generated schema key names (`RecordActualInputBody`, `CorrectActualInputBody`, etc.) via Step 1's grep and adjust if they differ.

- [ ] **Step 3: Typecheck**

Run: `cd apps/web && npx tsc --noEmit`
Expected: errors ONLY at the existing `updateCostLifecycle` call site in `CostsView.tsx` (missing the now-required `expectedRevision`) — this is expected and fixed in Task B5. No other new errors.

---

### Task B5: Rebuild `CostsView.tsx` as click-row → detail drawer with Edit/Correct/View actions

**Files:**
- Modify: `apps/web/src/features/operations/components/CostsView.tsx`
- Test: Create `apps/web/src/features/operations/components/CostsView.test.tsx` additions (the file already exists per investigation, covering only the create dialog — add to it, do not replace existing tests)

**Interfaces:**
- Consumes: `useOperationMutation`, `api.updateCostLifecycle`, `api.recordCostItemActual`, `api.correctCostItemActual` (Task B4), `minorToMajorString` (Task A4), `applyFieldErrors`, `Sheet`/`SheetContent`/`SheetHeader`/`SheetTitle`. No `listCostCorrections` — corrections arrive embedded on the `CostItem` DTO.
- Produces: nothing consumed elsewhere — leaf UI component.

- [ ] **Step 1: Replace the "Update" button and lifecycle dialog with a row-click drawer**

Remove the existing `lifecycleItem`/`lifecycleForm`/`lifecycleMutation`/`submitLifecycle` state and the "Update lifecycle value" `Dialog` entirely. Replace with:

```tsx
const [detailItem, setDetailItem] = useState<CostItem | null>(null);
```

Change the table row rendering (`rows.map((item) => <tr ...>`) to make the row clickable AND keyboard-reachable (this is newly-authored interactive UI, not pre-existing code being left alone — it must be operable without a mouse), and remove the old `<td><Button ...>Update</Button></td>` action cell:

```tsx
function openCostDetail(item: CostItem) {
  setDetailItem(item);
}
```

```tsx
<tr
  key={item.id}
  tabIndex={0}
  role="button"
  aria-label={`View cost item ${item.description}`}
  className="cursor-pointer border-b last:border-0 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  onClick={() => openCostDetail(item)}
  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openCostDetail(item); } }}
>
  <td className="p-3 font-medium">{item.description}<span className="block text-xs font-normal text-muted-foreground">{item.notes}</span></td>
  <td className="capitalize">{item.category.replaceAll("_", " ")}</td>
  <td className="font-mono">{formatMoney(item.estimated?.amount, item.currency)}</td>
  <td className="font-mono">{formatMoney(item.committed?.amount, item.currency)}</td>
  <td className="font-mono">{formatMoney(item.actual?.amount, item.currency)}</td>
  <td className="font-mono text-emerald-700">{formatMoney(item.paid?.amount, item.currency)}</td>
  <td className="font-mono">{item.quantityValue ? `${item.quantityValue} ${item.quantityUnit ?? ""}` : "—"}</td>
</tr>
```

Remove the now-empty `<th className="w-28">Action</th>` header cell too (the table no longer has a dedicated action column since the whole row opens the drawer).

- [ ] **Step 2: Add the detail drawer with per-stage forms**

Add small local Zod schemas near the file's other schemas — one for a plain Estimated/Committed amount edit, one for the Record-actual amount, and one for the Correct-actual amount+reason:

```ts
const stageAmountSchema = z.object({ amount: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount") });
const recordActualSchema = z.object({ amount: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount") });
const correctionSchema = z.object({
  amount: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount"),
  reason: z.string().trim().min(1, "A reason is required to correct a previously recorded amount"),
});
type StageAmountValues = z.infer<typeof stageAmountSchema>;
type RecordActualValues = z.infer<typeof recordActualSchema>;
type CorrectionValues = z.infer<typeof correctionSchema>;
```

Add the `useForm` instances and the three mutations. Note there is no separate corrections-list fetch — `ActualCorrections` is embedded on the `CostItem` the drawer already has, so `detailItem.actualCorrections` is immediately available with no extra request:

```tsx
const editForm = useForm<StageAmountValues>({ resolver: zodResolver(stageAmountSchema), defaultValues: { amount: "0.00" } });
const recordForm = useForm<RecordActualValues>({ resolver: zodResolver(recordActualSchema), defaultValues: { amount: "0.00" } });
const correctForm = useForm<CorrectionValues>({ resolver: zodResolver(correctionSchema), defaultValues: { amount: "0.00", reason: "" } });
const [editingStage, setEditingStage] = useState<"estimated" | "committed" | null>(null);
const [recordingActual, setRecordingActual] = useState(false);
const [correctingActual, setCorrectingActual] = useState(false);
const [confirmingCorrection, setConfirmingCorrection] = useState(false);
const [editFormError, setEditFormError] = useState<string>();
const [recordFormError, setRecordFormError] = useState<string>();
const [correctFormError, setCorrectFormError] = useState<string>();

const editStageMutation = useOperationMutation(
  (values: StageAmountValues) => api.updateCostLifecycle(detailItem!.id, { stage: editingStage!, amount: majorToMinor(values.amount)!, expectedRevision: detailItem!.revision }),
  ["projects", projectId, "costs"]
);
const recordActualMutation = useOperationMutation(
  (values: RecordActualValues) => api.recordCostItemActual(detailItem!.id, { amountMinor: majorToMinor(values.amount)!, currency: detailItem!.currency, expectedRevision: detailItem!.revision }),
  ["projects", projectId, "costs"]
);
const correctActualMutation = useOperationMutation(
  (values: CorrectionValues) => api.correctCostItemActual(detailItem!.id, { newAmountMinor: majorToMinor(values.amount)!, currency: detailItem!.currency, expectedRevision: detailItem!.revision, reason: values.reason }),
  ["projects", projectId, "costs"]
);
```

Since `detailItem` is a snapshot from the moment the row was clicked, and after a successful mutation the underlying `costs.data` list refreshes but `detailItem` itself does not automatically update, add an effect to keep `detailItem` in sync with the freshest row so the drawer shows the just-saved value (including the newly-appended correction, if any) and a correct `revision` for the next edit:

```tsx
useEffect(() => {
  if (!detailItem) return;
  const fresh = (costs.data ?? []).find((row) => row.id === detailItem.id);
  if (fresh && fresh.revision !== detailItem.revision) setDetailItem(fresh);
}, [costs.data, detailItem]);
```

Add `useEffect` to the React import line.

Render the drawer, placed after the existing "Add cost item" `Dialog`. Note the Actual row branches between "Record actual" (nil case) and "Correct" (already-set case) — these are two different backend operations (`RecordCostItemActual` vs `CorrectCostItemActual`), not one button with two labels controlling the same call:

```tsx
<Sheet open={detailItem !== null} onOpenChange={(open) => { if (!open) { setDetailItem(null); setEditingStage(null); setRecordingActual(false); setCorrectingActual(false); } }}>
  <SheetContent>
    <SheetHeader><SheetTitle>{detailItem?.description}</SheetTitle></SheetHeader>
    {detailItem && <div className="flex-1 overflow-y-auto px-4 pb-4">
      <p className="text-xs text-muted-foreground capitalize">{detailItem.category.replaceAll("_", " ")}{detailItem.notes ? ` · ${detailItem.notes}` : ""}</p>

      <div className="mt-4 grid gap-3">
        <div className="flex items-center justify-between rounded-lg border p-3">
          <div><p className="text-xs text-muted-foreground">Estimated</p><p className="font-mono text-sm">{formatMoney(detailItem.estimated?.amount, detailItem.currency)}</p></div>
          <Button variant="outline" size="sm" onClick={() => { setEditingStage("estimated"); editForm.reset({ amount: detailItem.estimated ? minorToMajorString(detailItem.estimated.amount) : "0.00" }); setEditFormError(undefined); }}>Edit</Button>
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3">
          <div><p className="text-xs text-muted-foreground">Committed</p><p className="font-mono text-sm">{formatMoney(detailItem.committed?.amount, detailItem.currency)}</p></div>
          <Button variant="outline" size="sm" onClick={() => { setEditingStage("committed"); editForm.reset({ amount: detailItem.committed ? minorToMajorString(detailItem.committed.amount) : "0.00" }); setEditFormError(undefined); }}>Edit</Button>
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3">
          <div><p className="text-xs text-muted-foreground">Actual</p><p className="font-mono text-sm">{formatMoney(detailItem.actual?.amount, detailItem.currency)}</p></div>
          {detailItem.actual == null
            ? <Button variant="outline" size="sm" onClick={() => { setRecordingActual(true); recordForm.reset({ amount: "0.00" }); setRecordFormError(undefined); }}>Record actual</Button>
            : <Button variant="outline" size="sm" onClick={() => { setCorrectingActual(true); correctForm.reset({ amount: minorToMajorString(detailItem.actual!.amount), reason: "" }); setCorrectFormError(undefined); }}>Correct</Button>}
        </div>
        <div className="flex items-center justify-between rounded-lg border border-dashed p-3">
          <div><p className="text-xs text-muted-foreground">Paid</p><p className="font-mono text-sm text-emerald-700">{formatMoney(detailItem.paid?.amount, detailItem.currency)}</p></div>
          <span className="text-xs text-muted-foreground">View only — no adjustment mechanism yet</span>
        </div>
      </div>

      {editingStage && <form onSubmit={editForm.handleSubmit((values) => { setEditFormError(undefined); editStageMutation.mutate(values, { onSuccess: () => setEditingStage(null), onError: (error) => setEditFormError(applyFieldErrors(error as unknown as ApiError, editForm.setError, ["amount"])) }); })} className="mt-4 grid gap-3 rounded-lg border p-3">
        <p className="text-sm font-medium capitalize">Edit {editingStage}</p>
        {editFormError && <p className="text-sm text-destructive">{editFormError}</p>}
        <Label className="grid gap-1.5">Amount (MYR)<Input inputMode="decimal" {...editForm.register("amount")} />{editForm.formState.errors.amount?.message && <p className="text-xs font-normal text-destructive">{editForm.formState.errors.amount.message}</p>}</Label>
        <div className="flex gap-2"><Button type="submit" disabled={editStageMutation.isPending}>{editStageMutation.isPending ? "Saving…" : "Save"}</Button><Button type="button" variant="outline" onClick={() => setEditingStage(null)}>Cancel</Button></div>
      </form>}

      {recordingActual && <form onSubmit={recordForm.handleSubmit((values) => { setRecordFormError(undefined); recordActualMutation.mutate(values, { onSuccess: () => setRecordingActual(false), onError: (error) => setRecordFormError(applyFieldErrors(error as unknown as ApiError, recordForm.setError, ["amount"])) }); })} className="mt-4 grid gap-3 rounded-lg border p-3">
        <p className="text-sm font-medium">Record Actual</p>
        {recordFormError && <p className="text-sm text-destructive">{recordFormError}</p>}
        <Label className="grid gap-1.5">Amount (MYR)<Input inputMode="decimal" {...recordForm.register("amount")} />{recordForm.formState.errors.amount?.message && <p className="text-xs font-normal text-destructive">{recordForm.formState.errors.amount.message}</p>}</Label>
        <div className="flex gap-2"><Button type="submit" disabled={recordActualMutation.isPending}>{recordActualMutation.isPending ? "Saving…" : "Save"}</Button><Button type="button" variant="outline" onClick={() => setRecordingActual(false)}>Cancel</Button></div>
      </form>}

      {correctingActual && <form onSubmit={(e) => { e.preventDefault(); correctForm.handleSubmit(() => setConfirmingCorrection(true))(); }} className="mt-4 grid gap-3 rounded-lg border p-3">
        <p className="text-sm font-medium">Correct Actual</p>
        <p className="text-xs text-muted-foreground">The previous value is preserved in this cost item&rsquo;s correction history — it is never silently discarded.</p>
        {correctFormError && <p className="text-sm text-destructive">{correctFormError}</p>}
        <Label className="grid gap-1.5">Corrected amount (MYR)<Input inputMode="decimal" {...correctForm.register("amount")} />{correctForm.formState.errors.amount?.message && <p className="text-xs font-normal text-destructive">{correctForm.formState.errors.amount.message}</p>}</Label>
        <Label className="grid gap-1.5">Reason<Textarea {...correctForm.register("reason")} />{correctForm.formState.errors.reason?.message && <p className="text-xs font-normal text-destructive">{correctForm.formState.errors.reason.message}</p>}</Label>
        <div className="flex gap-2"><Button type="submit">Review correction</Button><Button type="button" variant="outline" onClick={() => setCorrectingActual(false)}>Cancel</Button></div>
        {detailItem.actualCorrections && detailItem.actualCorrections.length > 0 && <div className="mt-2"><p className="text-xs font-medium text-muted-foreground">Correction history</p><ul className="mt-1 grid gap-1 text-xs text-muted-foreground">{detailItem.actualCorrections.map((c, i) => <li key={i}>{formatMoney(c.previousAmount.amount, c.previousAmount.currency)} → {formatMoney(c.newAmount.amount, c.newAmount.currency)} — {c.reason}</li>)}</ul></div>}
      </form>}
    </div>}

    <Dialog open={confirmingCorrection} onOpenChange={setConfirmingCorrection}>
      <DialogContent>
        <DialogHeader><DialogTitle>Confirm correction</DialogTitle></DialogHeader>
        <p className="text-sm text-muted-foreground">
          This changes the recorded Actual amount from {formatMoney(detailItem?.actual?.amount, detailItem?.currency)} to {formatMoney(majorToMinor(correctForm.getValues("amount")) ?? undefined, detailItem?.currency)}.
          The previous value is preserved in this cost item&rsquo;s correction history, not discarded.
        </p>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirmingCorrection(false)}>Cancel</Button>
          <Button
            disabled={correctActualMutation.isPending}
            onClick={() => {
              setCorrectFormError(undefined);
              correctActualMutation.mutate(correctForm.getValues(), {
                onSuccess: () => { setConfirmingCorrection(false); setCorrectingActual(false); },
                onError: (error) => { setConfirmingCorrection(false); setCorrectFormError(applyFieldErrors(error as unknown as ApiError, correctForm.setError, ["amount", "reason"])); },
              });
            }}
          >
            {correctActualMutation.isPending ? "Saving…" : "Confirm correction"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  </SheetContent>
</Sheet>
```

Confirm the generated `CostItem` type actually names the embedded field `actualCorrections` (matching `json:"actualCorrections,omitempty"` from Task B3) via `grep -n "actualCorrections" apps/web/src/lib/api/generated/schema.ts` — adjust the property name in the snippet above if it differs.

- [ ] **Step 3: Add the `Sheet` and `minorToMajorString` imports, remove now-unused imports**

Add `Sheet, SheetContent, SheetHeader, SheetTitle` to the `@/components/ui/sheet` import (new import line). Add `minorToMajorString` to the existing `@/lib/formatting/money` import line (added in Task A4). Confirm `Select`/`SelectContent`/`SelectItem`/`SelectTrigger`/`SelectValue` are still used elsewhere in this file (the create-cost-item form's category picker) before removing that import — they should still be needed there, only the lifecycle dialog's stage-picker `<Select>` is removed.

- [ ] **Step 4: Typecheck**

Run: `cd apps/web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 5: Update/add frontend tests**

Add to `apps/web/src/features/operations/components/CostsView.test.tsx` (check its existing mock-server setup pattern first via reading the file, matching its exact `server.use(http...)` style):

```tsx
describe("CostsView cost detail drawer", () => {
  it("clicking a row opens the drawer with existing values, and Edit on Estimated saves a new amount", async () => {
    // mock GET /cost-items to return one item with estimated=50000, revision=0
    // mock PATCH /cost-items/:id/lifecycle to assert body includes expectedRevision:0 and return the updated item
    // render, click the row, click Edit on Estimated, change the amount, submit
    // assert the PATCH body's expectedRevision matched, and the drawer/table reflects the new value after refetch
  });

  it("pressing Enter on a focused row opens the drawer", async () => {
    // mock GET /cost-items to return one item
    // render, Tab to the row (or query it directly and fire a keydown), press Enter
    // assert the drawer opens with that item's values
  });

  it("Actual shows Record actual (not Correct) when Actual is nil, and the Record form has no reason field", async () => {
    // render with a cost item that has actual=null/undefined
    // open drawer, assert the button next to Actual reads "Record actual"
    // click it, assert the form has an amount field but no reason field
  });

  it("Actual shows Correct (not Record actual) when Actual is already set", async () => {
    // render with a cost item that has actual=850000
    // open drawer, assert the button next to Actual reads "Correct"
  });

  it("Correct on Actual requires a non-blank reason before the confirmation dialog appears", async () => {
    // render with a cost item that has actual=850000
    // open drawer, click Correct, click Review correction with amount changed but reason left blank
    // assert a validation message appears, the confirmation dialog never opens, and no request was sent
  });

  it("Correct on Actual shows a confirmation dialog stating the previous and new amount before submitting", async () => {
    // render with a cost item that has actual=850000
    // mock POST /cost-items/:id/correct-actual to assert body includes expectedRevision/newAmountMinor/reason and return the corrected item (with actualCorrections populated)
    // open drawer, click Correct, fill amount + reason, click Review correction
    // assert the confirmation dialog shows both the previous (850000) and new amount
    // click Confirm correction, assert the request was sent only after that click, and the drawer/table reflects the new value and the correction-history line after refetch
  });

  it("Paid shows a View-only explanation with no Edit/Correct action", async () => {
    // render with a cost item that has paid=500000
    // open drawer, assert no button next to Paid, assert the explanatory text is present
  });
});
```

Write out the actual MSW handlers and assertions following this file's existing style exactly (read the existing `describe`/`it` blocks in this file first and copy their `server.use(http.get(...)/http.patch(...))` + `renderWithProviders` + `userEvent` conventions verbatim — do not invent a different test-setup style for these new tests).

- [ ] **Step 6: Run the frontend tests**

Run: `cd apps/web && npx vitest run src/features/operations/components/CostsView.test.tsx`
Expected: PASS.

- [ ] **Step 7: Manual verification**

Start backend + frontend against the local Docker stack. Navigate to a Project → Costs. Click a cost row (and separately, Tab to a row and press Enter) → confirm the drawer opens showing Estimated/Committed/Actual/Paid with existing values. Click Edit on Estimated → change the amount → Save → confirm the drawer's value and the table row both update. For a cost item whose Actual is not yet set, confirm the button reads "Record actual" and recording a value works with no reason field. For a cost item whose Actual IS already set, confirm the button reads "Correct" → try clicking Review correction with a blank reason → confirm it's rejected client-side and no confirmation dialog appears → fill in a reason → click Review correction → confirm the confirmation dialog shows both the previous and new amount → click Confirm correction → confirm the new value shows and the correction history line appears immediately (no extra network request needed, since it's embedded on the same response). Confirm Paid shows no Edit/Correct button, only the explanatory text.

---

### Task B6: Full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: gofmt affected Go files**

Run: `cd backend && gofmt -l internal/costs internal/suppliers internal/tenanttest internal/platform/composition` — expect no output (clean). If any files are listed, run `gofmt -w` on them.

- [ ] **Step 2: Focused backend package tests**

Run: `cd backend && go test ./internal/costs/... ./internal/suppliers/... -v`
Expected: PASS.

- [ ] **Step 3: `go build` and `go vet`**

Run: `cd backend && go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 4: Full backend test suite once**

Run: `cd backend && go test ./... -count=1`
Expected: PASS except any pre-existing, already-documented unrelated failures (this repo has one known stale-hardcoded-date test in `internal/rfqissuance`, `TestIssueVersionFingerprintsTheSupplierVisibleSourceDeterministically`, unrelated to this work — confirm no NEW failures beyond that one).

- [ ] **Step 5: Frontend typecheck, focused tests, full suite**

Run: `cd apps/web && npx tsc --noEmit`
Run: `cd apps/web && npx vitest run src/features/operations src/features/procurement`
Run: `cd apps/web && npx vitest run`
Expected: PASS (allowing for the one pre-existing flaky test in `ProjectProcurement.test.tsx`'s duplicate-warning dialog, already confirmed flaky and unrelated earlier this session — re-run in isolation if it fails, to confirm no new regression).

- [ ] **Step 6: Web Interface Guidelines review**

Use the `web-design-guidelines` skill against the two modified feature components: `apps/web/src/features/procurement/components/SupplierDetail.tsx` and `apps/web/src/features/operations/components/CostsView.tsx`. This plan's own drafted snippets already apply the guidelines relevant to new drawer/form/confirmation UI (label-wraps-input association, ellipsis in loading/placeholder text, a confirm step before the destructive Correct write, `inputMode="decimal"` on money fields, and — unlike this plan's first draft — a keyboard-reachable row (`tabIndex`, `role="button"`, `onKeyDown` for Enter/Space, a visible focus ring) for both new row-click-to-drawer affordances, since this is newly-authored interactive UI and must not be click-only) — this step is the actual post-implementation check against the real files, not a re-derivation. It may still flag one pre-existing, NOT-introduced-by-this-task gap shared with the rest of the app: the `Sheet`/`Dialog` primitives have no `overscroll-behavior: contain` set anywhere in the app. Do not fix that primitive-level gap as part of this task — it would mean editing `@/components/ui/sheet.tsx`/`dialog.tsx`, a shared component well outside this task's two feature files, and is exactly the kind of unrelated refactor this task's own instructions say to avoid. Note it in the final report instead.

- [ ] **Step 7: Report**

Summarize: files changed, API/domain changes, frontend UX implemented, exact lifecycle semantics used for costs (Estimated/Committed = ordinary CAS-guarded Edit; Actual = Record for the first value / Correct for changing an already-recorded one, with the previous value preserved in an embedded, append-only `ActualCorrections` list on the same `CostItem` document — one atomic `$set`+`$push`, no second collection, no transaction; Paid = read-only/View), how historical supplier data is protected (SupplierOffering.IndicativePrice is architecturally unreachable from RFQ/offer/award code — confirmed, not merely assumed), how the existing lifecycle route's Actual-bypass was closed (`stage=actual` now rejected outright by `UpdateCostItemLifecycle`, forcing every caller through Record or Correct), the legacy-revision compatibility rule for cost items that predate the Revision field, tests executed and results, the one deliberately-left-immutable portion (Paid adjustment) with the reason (no payments/ledger concept exists in this codebase, and inventing one is out of scope per the task's own instruction), and the Web Interface Guidelines findings from Step 6 (what was fixed in the new code, and the one pre-existing shared-primitive gap observed but deliberately left alone as out of scope).
