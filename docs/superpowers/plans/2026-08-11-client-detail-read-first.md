# Client Detail Read-First Implementation Plan

**Completion:** Implemented and verified on 2026-08-11.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> to implement this plan inline. The product owner explicitly waived test-first
> execution; add focused regression coverage after implementation. Do not use
> subagents or perform Git operations.

**Goal:** Replace the permanently expanded Client edit form with a compact,
read-first Client summary and Projects composition while preserving all
authoritative contracts and functionality.

**Architecture:** Keep Client reads, true-partial PATCH construction, Project
queries, and Quotation/share queries in the existing Client feature. Move
editing and current-Client Project creation into compact dialogs, and render
associated Projects as responsive operational rows rather than a wide table.
The current contractor navigation already has the correct internal-only route
inventory, so protect it with a regression test instead of changing portal
behavior.

**Tech Stack:** Next.js 16 client components, TypeScript, TanStack Query,
React Hook Form, Base UI/shadcn Dialog and form primitives, Vitest, Testing
Library, MSW.

## Global Constraints

- Preserve Client GET/PATCH and Project/Quotation/share contracts.
- Preserve true-partial Client PATCH semantics: omit unchanged, send `""` to
  clear.
- Do not show a Client-level Project status.
- Use persisted Project status and the existing `StatusBadge` mapping.
- Do not invent last activity or unsupported Project metadata.
- Keep Client/Supplier portal routes and functionality unchanged, but absent
  from authenticated contractor navigation.
- Do not perform Git operations.

---

### Task 1: Dialog-capable shared forms

**Files:**
- Modify: `apps/web/src/features/clients/components/ClientForm.tsx`
- Modify: `apps/web/src/features/projects/components/ProjectForm.tsx`

**Interfaces:**
- `ClientForm` adds optional `onCancel?: () => void` while retaining its full
  field values and submit behavior.
- `ProjectForm` adds optional `defaultClientId?: string` so Client detail can
  bind creation to the current Client without changing `CreateProjectInput`.

- [ ] Add an optional Cancel action to `ClientForm`, keeping Save as an explicit
  submit and preserving the responsive two-column form.
- [ ] Initialize `ProjectForm.clientId` from `defaultClientId` when supplied;
  preserve the existing blank selector on the global Projects page.

### Task 2: Read-first Client detail composition

**Files:**
- Modify: `apps/web/src/features/clients/components/ClientDetail.tsx`

**Interfaces:**
- Continues to consume `useClient`, `useUpdateClient`, `useClientProjects`,
  `useQuotations`, and `useQuotationShareStatus`.
- Adds `useCreateProject` for the current-Client **New Project** dialog.

- [ ] Replace `PageHeader` and the expanded form with a compact Back/name/Edit
  header and desktop `grid-cols-[minmax(0,22rem)_minmax(0,1fr)]` layout.
- [ ] Render name, email, phone, address, and notes in the left summary with
  exact fallbacks **Not provided** and **No notes provided**.
- [ ] Add a compact Edit dialog containing `ClientForm`; calculate the existing
  genuine-partial patch, close only on success, and show backend failure details
  while preserving the open form.
- [ ] Replace the wide Project table with responsive Project rows containing
  name, persisted `StatusBadge`, compact authoritative Quotation/share context,
  and an obvious Project link.
- [ ] Add compact Project loading/error/empty states.
- [ ] Add **New Project** in the Projects header and a compact ProjectForm dialog
  preselected to the current Client; close only on successful creation.

### Task 3: Regression coverage

**Files:**
- Modify: `apps/web/src/features/clients/components/ClientDetail.test.tsx`
- Modify: `apps/web/src/features/projects/components/ProjectForm.test.tsx`
- Create: `apps/web/src/components/layout/navItems.test.ts`

- [ ] Update partial-PATCH tests to open Edit Client before interacting with
  fields; retain changed-only and explicit-clear assertions.
- [ ] Test the read-first summary, friendly fallbacks, absence of a Client-level
  status, compact Project status/context/open action, and visible New Project.
- [ ] Test failed Client PATCH leaves the dialog open with the backend detail.
- [ ] Test current-Client Project creation submits the bound Client ID.
- [ ] Assert contractor navigation is exactly Dashboard, Clients, Projects,
  Suppliers, Procurement, Quotations, and Company, with no portal entries.
- [ ] Run focused Client/Project/navigation tests.

### Task 4: Verification and records

**Files:**
- Modify: `tasks/todo.md`
- Modify: `tasks/done.md`
- Modify: `docs/PROJECT_STATUS.md`

- [ ] Run `npm run lint`.
- [ ] Run `npm run typecheck`.
- [ ] Run the full Vitest suite.
- [ ] Run `npm run build`.
- [ ] Record exact files, read-first layout, dialog behavior, portal-navigation
  correction, test count, and gate results.
