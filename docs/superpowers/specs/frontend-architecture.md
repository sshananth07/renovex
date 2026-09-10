# Frontend Architecture Contract

## Status

**Approved architecture — fixed constraint for M8.5A frontend work.**

This document defines the frontend architecture for the Renovation Project Intelligence Platform from F1 through F5.

It applies alongside:

- `phase1.md` — product authority.
- ADR 0001 / ADR 0002 — architecture authority.
- `docs/architecture/frontend-auth-session-contract.md` — authentication/session authority.
- `apps/web/openapi/openapi.json` — generated HTTP transport contract.

Do not reopen the decisions in this document during milestone planning or implementation unless a concrete repository contradiction, security issue, or explicit architecture change is approved.

---

# 1. Frontend Program Boundary

M8.5A delivers the complete M0–M8 frontend vertical slice before Python AI work begins.

Implementation order:

```text
F1  Foundation + authenticated Contractor project setup
F2  Resources + costing + estimates + quotations
F3  RFQ + contractor procurement
F4  Supplier + Client external portals
F5  Integration + dashboard summary + polish + demo hardening

Then:
M8.5B  Narrow Python AI Preview
```

Each frontend milestone gets its own:

1. design/specification;
2. approved implementation plan;
3. RED → GREEN implementation;
4. completion review.

Do not expand backend scope during M8.5A except for a narrowly justified frontend prerequisite approved separately.

---

# 2. Core Technology Stack

Frontend:

```text
Next.js 16
React 19
TypeScript
Tailwind CSS
shadcn/ui
TanStack Query
React Hook Form
Zod
openapi-typescript
openapi-fetch
Vitest
React Testing Library
Playwright
```

Do not introduce Redux, Zustand, or another global state library unless a demonstrated need emerges that cannot be handled cleanly by the approved state boundaries.

---

# 3. Application Surfaces

The frontend contains three distinct product surfaces.

## 3.1 Authenticated Contractor App

Primary operational application for:

- company users;
- Clients;
- Projects;
- Properties;
- Spaces;
- Work Items;
- Materials;
- Labour;
- Costing;
- Estimates;
- Quotations;
- RFQs;
- Supplier offers;
- Awards;
- later downstream financial workflows.

## 3.2 Client Portal

Unauthenticated restricted external surface accessed through backend-issued resource-scoped access grants.

The Client Portal must not share the Contractor app's normal authenticated navigation or assume contractor authentication.

## 3.3 Supplier Portal

Unauthenticated restricted external surface accessed through Supplier invitation/session mechanisms.

The Supplier Portal must remain operationally and visually distinct from the internal Contractor app.

Do not collapse all three surfaces into one authentication model.

---

# 4. Authentication and Session Architecture

Authentication behavior is governed by:

```text
docs/architecture/frontend-auth-session-contract.md
```

The fixed summary is:

```text
Browser → Go API directly
Access token → memory only
Refresh token → Go-owned Secure HttpOnly host-only cookie
Session restore → POST /auth/refresh
Current user → GET /auth/me
Protected UI → AuthProvider + AuthGate
401 recovery → coordinated single-flight refresh
Authorization truth → Go backend
```

No Next.js BFF/session proxy layer.

No localStorage/sessionStorage token persistence.

No Next.js middleware/proxy as authentication authority.

---

# 5. HTTP Contract and OpenAPI

The frontend must consume the generated backend OpenAPI contract.

Source:

```text
apps/web/openapi/openapi.json
```

Backend export command:

```bash
cd backend
go run ./cmd/openapi -out ../apps/web/openapi/openapi.json
```

Frontend generation uses:

```text
openapi-typescript
openapi-fetch
```

Required layering:

```text
OpenAPI JSON
    ↓
generated TypeScript paths/types
    ↓
openapi-fetch transport client
    ↓
handwritten API/auth/error wrapper
    ↓
TanStack Query hooks / mutations
    ↓
feature components
```

Generated code is for transport contracts only.

Do not put:

- business workflow logic;
- authentication refresh orchestration;
- cache policy;
- UI state;
- domain-specific presentation rules

inside generated code.

Do not hand-maintain duplicate TypeScript request/response interfaces for endpoints already represented by OpenAPI.

---

# 6. API Client Boundary

Recommended structure:

```text
src/lib/api/
├── generated/
│   └── schema.ts
├── client.ts
├── auth.ts
├── errors.ts
└── request-id.ts
```

`client.ts` owns:

- `NEXT_PUBLIC_API_BASE_URL`;
- `credentials: "include"` where required;
- bearer token injection from the approved in-memory token holder;
- normalized request execution;
- single retry after successful coordinated token refresh;
- request correlation;
- typed OpenAPI transport.

It must not own React rendering or feature-specific business rules.

---

# 7. Server State and Client State

## 7.1 TanStack Query owns server state

Examples:

```text
Clients
Projects
Properties
Spaces
Work Items
Materials
Workers
Labour Entries
Cost Items
Estimates
Quotations
RFQs
Supplier Offers
Awards
Dashboard summary
```

Use query keys that include all server-visible filters.

Example:

```ts
["clients", { page, pageSize, search, sort, order }]
```

## 7.2 AuthProvider owns authentication state

Auth state includes:

```text
access token
auth status
current-user projection
bootstrap/session-restore state
refresh coordination
```

## 7.3 URL search parameters own list state

List screens must synchronize state into the URL.

Example:

```text
?page=2&pageSize=25&search=kitchen&sort=name&order=asc
```

This applies to:

- pagination;
- search;
- sort;
- filter values that belong to the navigable list view.

Benefits are intentional:

- reload-safe;
- Back/Forward-safe;
- shareable;
- deterministic query keys;
- no hidden table state.

Do not make page/search/sort state live only inside a custom React hook.

## 7.4 React Hook Form owns form state

Use React Hook Form for create/edit forms.

Use Zod for frontend validation and form schemas.

Backend validation remains authoritative.

## 7.5 Local component state

Use local React state for ephemeral UI concerns such as:

- dialog open/closed;
- drawer open/closed;
- tab-local interactions;
- menu state;
- transient visual toggles.

---

# 8. Route Architecture

Use Next.js route groups to separate public and protected surfaces without changing URL paths.

Conceptual layout:

```text
src/app/
├── layout.tsx
├── (auth)/
│   ├── login/
│   └── register/
├── (app)/
│   ├── layout.tsx
│   ├── dashboard/
│   ├── clients/
│   ├── projects/
│   └── company/
├── client-portal/
└── supplier-portal/
```

The exact portal URL structure may evolve with access-grant requirements, but the surfaces remain separate.

---

# 9. Feature-Based Frontend Structure

Use feature ownership rather than a technical-layer-only folder tree.

```text
src/
├── app/
├── features/
│   ├── auth/
│   ├── company/
│   ├── clients/
│   ├── projects/
│   ├── properties/
│   ├── spaces/
│   ├── work-items/
│   ├── materials/
│   ├── labour/
│   ├── costs/
│   ├── estimates/
│   ├── quotations/
│   ├── procurement/
│   ├── supplier-portal/
│   └── client-portal/
├── components/
│   ├── ui/
│   └── layout/
└── lib/
    ├── api/
    ├── auth/
    ├── formatting/
    └── query/
```

A feature may contain:

```text
api.ts
queries.ts
mutations.ts
schemas.ts
components/
forms/
types.ts          only feature-derived/non-generated UI types
```

Do not duplicate backend domain entities into large handwritten frontend model layers unless a frontend-specific view model is needed.

---

# 10. Server Components vs Client Components

Use Server Components for:

- static route framing;
- non-authenticated static content;
- layouts that do not require browser auth state;
- presentation that can be resolved without the in-memory access token.

Use Client Components for:

- AuthProvider/AuthGate;
- authenticated fetching;
- TanStack Query;
- forms;
- interactive tables;
- mutations;
- dialogs;
- drawers;
- client-side filters and navigation.

Do not force authenticated API fetching into Server Components while the access token is intentionally memory-only in the browser.

---

# 11. Contractor App Information Architecture

Global navigation:

```text
Dashboard
Clients
Projects
Company
```

Project workspace:

```text
Overview
Property
Spaces
Work Items
```

Later milestones extend the project workspace with:

```text
Materials / Resources
Costs
Estimates
Quotations
Procurement
Documents / other approved modules
```

The global shell should remain stable as capabilities are added.

---

# 12. Application Shell

Desktop:

- persistent sidebar;
- compact top bar;
- company context;
- project workspace tabs where applicable.

Mobile:

- drawer navigation;
- compact top bar;
- horizontally scrollable project tabs where necessary;
- card/list adaptations for dense tables.

Avoid desktop-only table layouts.

---

# 13. Visual Direction

The Contractor app should feel like a professional operational SaaS product.

Priorities:

```text
clarity
density without clutter
strong hierarchy
fast scanning
money/status/deadline visibility
clear primary actions
```

Use:

- neutral surfaces;
- restrained accent usage;
- consistent status badges;
- deliberate whitespace;
- compact but readable controls;
- shadcn/ui primitives;
- Tailwind for layout and visual composition.

Avoid unnecessary decoration, excessive gradients, large marketing-style cards, or dashboard chrome that reduces information density.

---

# 14. Project Setup Experience

F1 uses a progressive, resumable setup flow.

Conceptually:

```text
Client
  ↓
Project
  ↓
Overview
  ↓
Property
  ↓
Spaces
  ↓
Work Items
```

This is **not** a cross-module mega-wizard.

The user must be able to leave and return at any point.

The project Overview should expose setup progress such as:

```text
Client       ✓
Project      ✓
Property     ○
Spaces       ○
Work Items   ○
```

The backend remains the source of truth for whether each stage is complete.

Do not persist a duplicate frontend-only workflow state if completeness can be derived from authoritative resources.

---

# 15. Create and Edit Interaction Model

Use a hybrid interaction model.

Good candidates for dialogs/drawers:

```text
Client create/edit
Space create/edit
Work Item create/edit
simple confirmations
```

Use dedicated pages for workflows whose complexity requires more space.

Examples later may include:

```text
Estimate composition
Quotation composition
RFQ preparation
Supplier-offer comparison
```

Do not force complex workflows into small dialogs.

---

# 16. Forms and Validation

Use:

```text
React Hook Form
+
Zod
```

Frontend validation should:

- catch obvious required-field/format mistakes;
- mirror stable transport constraints where practical;
- provide immediate field feedback.

Backend validation remains authoritative.

Never use frontend validation as the security boundary.

For genuine partial PATCH:

- untouched fields must be omitted;
- explicit clearing must preserve the backend's clear semantics;
- nullable fields must distinguish omission from explicit `null` where the backend contract requires tri-state behavior.

---

# 17. Pagination, Search, Sort, and Filters

F0 established the canonical paginated contract:

```json
{
  "items": [],
  "page": 1,
  "pageSize": 25,
  "total": 0
}
```

Frontend list screens must use backend pagination rather than fetching all records and slicing locally.

Required URL model:

```text
page
pageSize
search
sort
order
resource-specific filters
```

Use debounced search input where appropriate, but the committed search value should become URL/query state.

Changing search or a filter should normally reset `page` to `1`.

Do not perform tenant-wide analytics from only the currently loaded page.

---

# 18. Shared List Components

Reusable table/list primitives are encouraged, but data fetching must remain feature-owned.

Good shared component:

```text
PaginatedTable
```

Responsibilities:

- visual rows;
- headers;
- sort controls;
- pagination controls;
- empty/loading/error presentation slots;
- responsive rendering hooks.

Not its responsibility:

- endpoint selection;
- business filters;
- API fetching;
- cache invalidation;
- domain-specific mutations.

Feature hooks use TanStack Query and pass prepared data to shared presentation components.

---

# 19. Query and Mutation Conventions

Queries:

- stable query keys;
- derive keys from URL/filter state;
- use generated transport types;
- no hidden global mutable resource cache outside TanStack Query.

Mutations:

- invalidate or update only affected query keys;
- do not broadly invalidate the entire application unless required;
- preserve user input on recoverable conflicts;
- surface backend problem details in a normalized UI format.

---

# 20. ExpectedRevision and Conflict Handling

Where backend modules expose `ExpectedRevision`, the frontend must preserve that concurrency contract.

On `409` revision conflict:

```text
1. Do not silently retry the mutation.
2. Preserve the user's current form/input.
3. Refetch authoritative server state.
4. Explain that the record changed.
5. Allow the user to review and decide how to proceed.
```

Do not implement client-side automatic conflict merging unless separately designed.

M0–M2 resources that do not currently expose revision/CAS remain last-write-wins according to their backend contract.

---

# 21. Error Model

Normalize backend RFC7807-style errors.

At minimum distinguish:

```text
401  unauthenticated / expired session
403  authenticated but unauthorized
404  missing or tenant-neutral not found
409  lifecycle/concurrency/business conflict
422  validation
429  rate limited
5xx  server/unavailable
network failure
```

Rules:

- normal business `403` must not trigger token refresh;
- only the defined authentication-expiry path triggers refresh;
- `422` should map field-level errors where the backend provides usable locations;
- `409` should preserve unsaved input;
- show retry actions only when retry is semantically safe.

---

# 22. Role-Aware UI

The backend remains authoritative for authorization.

The frontend may hide, disable, or alter actions based on the current-user role/capabilities for usability.

Do not replicate the full backend authorization engine.

Never assume hidden UI equals access control.

If an action is rejected by the backend, surface the authoritative result.

---

# 23. Malaysia-First Formatting

Centralize formatting in:

```text
src/lib/formatting/
```

Defaults:

```text
Locale:       en-MY
Currency:     MYR
Timezone:     Asia/Kuala_Lumpur
Date display: DD/MM/YYYY
```

Use:

```text
Intl.NumberFormat
Intl.DateTimeFormat
```

Money:

- backend amount remains integer minor units;
- never convert authoritative amounts to floating-point business calculations;
- formatting conversion is display-only.

Quantities:

- preserve exact decimal strings from the API;
- do not coerce exact quantity values through JavaScript floating-point calculations unless only formatting a non-authoritative display.

No full localization/i18n framework is required yet.

---

# 24. Dashboard Architecture

Build the dashboard as an analytics-ready frontend surface without fabricating unsupported metrics.

Frontend uses a stable view model conceptually named:

```text
DashboardSummary
```

Until backend aggregate support exists:

- show only metrics derivable authoritatively without misleading aggregation;
- mark unsupported metrics unavailable;
- do not calculate tenant-wide analytics from a single paginated page;
- do not fake trends/charts.

A narrow backend:

```text
GET /dashboard/summary
```

may be approved later after the first usable project journey is complete.

It is not an F1 prerequisite.

---

# 25. External Portal Architecture

Client and Supplier portals must consume their dedicated backend access/session mechanisms.

Do not reuse contractor bearer-authentication assumptions.

Portal requirements:

- restricted resource scope;
- privacy-aware responses;
- no internal contractor navigation;
- no access to arbitrary tenant resources;
- clear expired/revoked access handling.

Detailed portal designs belong to F4.

---

# 26. Testing Strategy

## 26.1 Vitest

Use for:

- pure utilities;
- formatting;
- API error normalization;
- query parameter parsing;
- safe `returnTo`;
- reducers or state helpers if introduced;
- single-flight refresh behavior where isolated testing is practical.

## 26.2 React Testing Library

Use for:

- AuthProvider;
- AuthGate;
- forms;
- validation;
- dialogs/drawers;
- loading/error/empty states;
- role-aware action visibility;
- conflict-preservation UX;
- list interactions.

## 26.3 Playwright

Use for composed browser flows.

F1 minimum journeys include:

```text
register
login
session restoration after reload
protected-route redirect
logout
Client create/edit
Project create/edit
Property create/edit
Space create/edit
Work Item create/edit
Work Item space assign/reassign/clear
cancelled Work Item edit rejection
URL pagination/search/sort persistence
single-flight refresh behavior
```

Later milestones add their own composed journeys.

Manual browser checking is supplemental, not a substitute for automated coverage.

---

# 27. Frontend TDD Workflow

For each milestone:

```text
RED
→ witness failing test
→ minimal GREEN implementation
→ focused tests
→ milestone acceptance flow
→ build/lint/typecheck
→ final frontend test gate
```

Do not build an entire feature first and add tests afterward.

Do not weaken assertions merely to obtain GREEN.

---

# 28. Standard Verification Gate

At minimum for frontend milestones:

```bash
npm run lint
npm run typecheck
npm run test
npm run build
```

When Playwright coverage exists:

```bash
npm run test:e2e
```

Exact script names may follow repository conventions, but equivalent checks are required.

Before implementation begins after backend contract changes, regenerate OpenAPI and verify generated TypeScript contracts are current.

---

# 29. OpenAPI Drift Rule

The generated frontend transport contract must stay synchronized with the backend.

Preferred flow:

```text
backend contract change
→ export openapi.json
→ regenerate schema.ts
→ compile frontend
→ update feature code/tests
```

Do not patch generated TypeScript manually to compensate for backend/schema drift.

If generated contracts appear wrong, investigate the backend OpenAPI schema first.

---

# 30. Accessibility and Interaction Baseline

Use semantic HTML and accessible shadcn/ui primitives.

Minimum expectations:

- keyboard-accessible navigation;
- labeled form controls;
- visible focus states;
- correct dialog semantics;
- actionable validation messages;
- no color-only status meaning;
- loading states that do not trap navigation.

Do not defer basic accessibility until polish.

---

# 31. Responsive Baseline

All Contractor app flows must remain usable on:

```text
desktop
tablet
mobile
```

Dense desktop tables may adapt to mobile cards or stacked row layouts.

Do not require horizontal desktop-scale scrolling for core mobile workflows unless the data genuinely cannot be represented otherwise.

---

# 32. Performance Boundaries

Default to:

- server-driven pagination;
- targeted query invalidation;
- lazy loading for heavy milestone-specific interfaces where useful;
- avoiding duplicate API calls during auth bootstrap;
- single-flight token refresh;
- stable query keys.

Do not prematurely introduce complex caching infrastructure outside TanStack Query.

---

# 33. Security Boundaries

Frontend security rules include:

- no persistent browser storage for access tokens;
- no JavaScript-readable refresh token;
- safe internal-only `returnTo`;
- backend-authoritative tenant and role enforcement;
- no wildcard credentialed CORS assumption;
- do not expose internal-only identifiers or controls unnecessarily in external portals;
- clear authenticated cached server state on logout/session loss.

The frontend is not the security boundary for tenant isolation.

---

# 34. F1 Required Product Journey

F1 must produce a usable authenticated Contractor flow:

```text
Register / Login
    ↓
Authenticated app shell
    ↓
Client
    ↓
Project
    ↓
Project Overview
    ↓
Property
    ↓
Spaces
    ↓
Work Items
```

The project setup must be resumable.

F1 should establish the architectural patterns later milestones reuse:

- generated API contract;
- query client;
- forms;
- list URLs;
- errors;
- responsive shell;
- tests;
- formatting;
- role-aware UI boundary.

Do not optimize F1 solely as a temporary demo implementation.

---

# 35. F1 Explicit Non-Goals

Unless separately approved, F1 does not implement:

```text
Materials
Labour
Cost ledger
Estimates
Quotations
RFQs
Supplier Offers
Awards
Payments
Client Portal
Supplier Portal
AI
dashboard aggregate endpoint
company/member lifecycle expansion
project status workflow redesign
```

Project status may be displayed, but workflow redesign remains deferred.

---

# 36. Decision Escalation Rules

Do not ask the user to choose between already-settled alternatives in this document.

Proceed using this contract.

Pause and report only when one of these occurs:

1. the live backend/OpenAPI contract contradicts this architecture;
2. a security or privacy conflict is discovered;
3. a required frontend capability cannot be implemented without a backend change;
4. a proposed change would alter module ownership or authoritative business semantics;
5. a major dependency cannot support the approved design;
6. an irreversible architectural choice appears that is not covered here.

Normal component naming, layout details, hook decomposition, and equivalent implementation mechanics are implementation-level decisions and do not require architecture approval unless they materially change these contracts.

---

# 37. Authority Rule for Agents

When implementing or planning frontend work, agents must treat these files as authoritative constraints:

```text
phase1.md
ADR 0001
ADR 0002
docs/architecture/frontend-auth-session-contract.md
docs/architecture/frontend-architecture.md
apps/web/openapi/openapi.json
```

For contradictions:

```text
product semantics       → phase1.md
backend architecture    → ADRs
frontend session model  → frontend-auth-session-contract.md
frontend architecture   → frontend-architecture.md
HTTP shapes             → generated OpenAPI contract
```

Do not silently invent a new architecture to reconcile a contradiction.

Stop and report the contradiction instead.

---

# 38. Architecture Summary

```text
Next.js 16
    │
    ├── AuthProvider/AuthGate
    │     └── in-memory bearer token
    │
    ├── OpenAPI generated transport
    │     └── openapi-fetch
    │
    ├── TanStack Query
    │     └── authoritative server state
    │
    ├── URL search params
    │     └── list/filter/sort/page state
    │
    ├── React Hook Form + Zod
    │     └── forms
    │
    ├── Tailwind + shadcn/ui
    │     └── presentation
    │
    └── Playwright / Vitest / RTL
          └── verification

Browser
    │
    └── direct credentialed requests
          ↓
        Go API
          ├── authentication
          ├── tenant isolation
          ├── business rules
          ├── calculations
          └── persistence
```

The frontend presents and orchestrates approved workflows.

The Go backend remains the authoritative system of record and business-rule boundary.
