# Project Status Management Implementation Plan

**Completion:** Implemented and verified on 2026-08-11.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> to implement this plan task-by-task. No subagents and no Git operations.
> The product owner explicitly waived test-first execution on 2026-08-11;
> focused regression tests are added after the direct implementation.

**Goal:** Add a deliberate, backend-authoritative Project Status dialog to
Project Overview with correct cache refresh and rejection handling.

**Architecture:** Extend the existing Project feature API/mutation layer with
the dedicated status endpoint, centralize the eight selectable values beside
the existing presentation mapping, and keep the dialog in a focused component
used by Project Overview. React Query owns successful cache replacement and
invalidation; setup derivation remains untouched.

**Tech Stack:** Next.js 16 client components, TypeScript, TanStack Query,
openapi-fetch, Base UI/shadcn primitives, Vitest, Testing Library, MSW.

## Global Constraints

- Use only the eight backend Project statuses.
- Use only `PATCH /projects/{id}/status` for lifecycle status.
- Do not infer or automatically mutate status from setup or project activity.
- Preserve the existing badge labels and tones.
- Preserve the dialog and selected value when the backend rejects a request.
- Do not add a Projects status filter because `GET /projects` has no status
  parameter.
- Do not perform Git operations.

---

### Task 1: Authoritative frontend status options

**Files:**
- Modify: `apps/web/src/features/projects/statusPresentation.ts`
- Test: `apps/web/src/features/projects/statusPresentation.test.ts`

**Interfaces:**
- Produces: `PROJECT_STATUSES`, an ordered readonly collection of the eight raw
  backend values, and `ProjectStatus`, its element union type.

- [ ] Add a regression test asserting the exported options contain exactly the
  eight backend values and every option has a readable existing label.
- [ ] Export the ordered readonly tuple and derive the label record from the
  `ProjectStatus` union without adding or removing a value.
- [ ] Re-run the focused test and witness GREEN.

### Task 2: Dedicated status API and cache invalidation

**Files:**
- Modify: `apps/web/src/features/projects/api.ts`
- Modify: `apps/web/src/features/projects/mutations.ts`
- Test: `apps/web/src/features/projects/components/ProjectOverview.test.tsx`

**Interfaces:**
- Produces: `updateProjectStatus(projectId: string, status: ProjectStatus)`.
- Produces: `useUpdateProjectStatus(projectId: string)`.

- [ ] Add an MSW-backed component test that explicitly confirms a new
  status and asserts the request reaches `PATCH /projects/:id/status` with only
  `{status}`.
- [ ] Add `updateProjectStatus` through the generated OpenAPI client.
- [ ] Add `useUpdateProjectStatus`; on success set `["projects", projectId]` to
  the response and invalidate `["projects"]`, `["clients"]`, and
  `["dashboard"]` so detail/Overview, lists, Client summaries, and Dashboard
  refetch persisted status.

### Task 3: Deliberate status dialog

**Files:**
- Create: `apps/web/src/features/projects/components/ProjectStatusDialog.tsx`
- Modify: `apps/web/src/features/projects/components/ProjectOverview.tsx`
- Test: `apps/web/src/features/projects/components/ProjectOverview.test.tsx`

**Interfaces:**
- Consumes: `Project`, `PROJECT_STATUSES`, `StatusBadge`, and
  `useUpdateProjectStatus`.
- Produces: `ProjectStatusDialog({ project })` rendering the actionable badge
  trigger and controlled confirmation dialog.

- [ ] Add tests proving the badge is an accessible trigger, all eight
  options are present, same-status submission is disabled, selection alone
  sends no PATCH, and explicit confirmation sends exactly one PATCH.
- [ ] Add a success test proving the dialog closes and the persisted
  returned status appears after confirmation.
- [ ] Add a rejection test returning a normalized backend problem and
  prove the message and selected value remain visible in the open dialog.
- [ ] Implement the focused dialog with explicit Cancel/Update buttons, current
  StatusBadge, controlled Select, pending state, and normalized API error text.
- [ ] Replace the passive Overview badge with `ProjectStatusDialog`; leave
  `deriveSetupProgress` and all activity queries/mutations unchanged.
- [ ] Run the focused Overview/status tests and witness GREEN.

### Task 4: Regression verification and records

**Files:**
- Modify: `tasks/todo.md`
- Modify: `tasks/done.md`

- [ ] Run the focused Project tests.
- [ ] Run `npm run lint`.
- [ ] Run `npm run typecheck`.
- [ ] Run the full Vitest suite.
- [ ] Run `npm run build`.
- [ ] Record the Project Status correction, exact endpoint, invalidation scope,
  tests, and verification results. Record that no list filter was added because
  the backend endpoint does not support one.
