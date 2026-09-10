# Project Status — Renovation Project Intelligence Platform (Phase 1)

**Last updated:** 2026-08-11
**Audience:** Project management / stakeholder review
**Source of truth this document summarizes:** `phase1.md` (product spec),
`docs/adr/*` (architectural decisions), `docs/superpowers/specs/*` (per-milestone
design specs), `docs/superpowers/plans/*` (per-milestone implementation plans),
`tasks/done.md` (detailed completed-work log)

---

> **Frontend completion update (2026-08-10):** M8.5A is complete through
> F1.11 and F2–F5. The Contractor App now composes Resources, Costs, Estimates,
> Quotations, Supplier Directory, Procurement, scoped Client/Supplier portals,
> Project Overview, Client detail, Company Settings, and the authoritative
> Dashboard over the existing backend/OpenAPI capabilities. The one authorised
> backend exception adds session-scoped Supplier bootstrap and offer operations
> under `/supplier-access` while preserving the HttpOnly cookie at
> `Path=/supplier-access`. Desktop/tablet/mobile real-browser QA and all final
> frontend gates passed. M8.5B/AI work has not begun. This update supersedes the
> older frontend-status statements below; backend milestone history is unchanged.

> **Project Status correction (2026-08-11):** Project Overview now exposes a
> deliberate contractor-controlled status dialog backed only by
> `PATCH /projects/{id}/status`. Its selector uses the authoritative eight
> backend statuses, rejected changes remain visible for review, and successful
> changes refresh every status-bearing Project/Client/Dashboard query. Lifecycle
> status remains independent from setup progress and downstream activity. The
> Project list contract has no status query parameter, so no unsupported filter
> was added.

> **Client detail correction (2026-08-11):** Client detail now follows the
> approved read-first Figma composition with a compact authoritative summary,
> responsive Project rows, dialog-based editing, and current-Client Project
> creation. True-partial PATCH behavior and Quotation/share context are
> preserved. Authenticated navigation exposes contractor routes only; external
> Client/Supplier portals remain scoped-link surfaces. Desktop/mobile browser QA
> passed without starting the backend.

## 1. What this project is

An AI-assisted platform for renovation contractors covering the full
commercial lifecycle of a job:

```text
Customer inquiry
      |
      v
Project creation -> Space definition -> Scope of work
      |
      v
AI-assisted Work Item generation
      |
      v
Material and labour planning -> Internal cost calculation
      |
      v
Profitability analysis (internal, private to the contractor)
      |
      v
Customer-facing Quotation generation
      |
      v
Secure Client review -> Approval or change request
      |
      v
Material requirement generation -> Supplier RFQ -> Supplier selection
      |
      v
Supplier pricing response (attributed to the correct Supplier)
      |
      v
Actual project cost tracking -> Customer payment tracking
      |
      v
Final project profitability
```

**Core product principle:** *AI suggests. The system calculates. The
contractor approves.* AI accelerates scope creation, resource planning, and
documentation — it never independently produces authoritative financial
numbers, quantities, or contractual commitments.

**Security principle:** external parties (Clients, Suppliers) never get
general access to a contractor's workspace — only scoped, resource-specific
access grants (e.g. "view and approve this one Quotation," "respond to this
one RFQ").

**Deployment model:** fully local-first for Phase 1 development — Go
backend + MongoDB + Mailpit via Docker Compose, no AWS or external cloud
dependency required to build, run, or test the system. `AI_PROVIDER=mock`
is the default, so the entire system is testable with zero external AI
API cost or dependency.

---

## 2. Technology stack

| Layer | Choice |
|---|---|
| Backend language/runtime | Go 1.26.5 |
| HTTP framework | chi (router) + Huma v2 (OpenAPI-generating REST layer) |
| Database | MongoDB (via `mongo-driver/v2`) |
| Backend architecture | Modular monolith (one deployable, strict internal module boundaries) |
| Money/decimal handling | Custom `int64` minor-unit `Money` type + `shopspring/decimal` for quantities — no floating-point anywhere in financial or quantity math |
| Frontend | Next.js 16 + TypeScript (scaffolded, not yet built out beyond the app shell) |
| Local infrastructure | Docker Compose: MongoDB + Mailpit (local email capture, no real SMTP needed in dev) |
| Testing | Go's built-in `testing` package for unit tests; `testcontainers-go` (real, ephemeral MongoDB instances) for integration tests; a dedicated real-HTTP acceptance-test harness (`internal/tenanttest`) for full tenant-isolation proof |

**Why a modular monolith and not microservices:** one deployable process,
strong internal domain boundaries enforced by Go's own package/interface
system. This avoids premature infrastructure complexity (service meshes,
distributed tracing, network-partition handling) while still keeping each
business domain's code cleanly separated and independently testable. This
can be split into real services later if scale or team-ownership ever
justifies it — nothing about the current design blocks that path.

---

## 3. Architectural foundations (decided once, binding on every milestone)

Three Architectural Decision Records (ADRs) govern the whole system and
are treated as fixed — not re-litigated milestone to milestone:

**ADR 0001 — Money and Decimal Strategy.** All money is stored as whole
minor currency units (e.g. cents) plus a currency code — never a
floating-point number. All rates/percentages use fixed-point "basis
points" (1% = 100 bps), never a floating-point percentage. All rounding
goes through one centralized function. This is the single most important
financial-correctness guarantee in the system: it structurally prevents
the classic "floating point money bug" class of defect.

**ADR 0002 — Modular Monolith Module Boundaries.** Every business domain
(Identity, Projects, Costs, Estimates, etc.) owns its own data, and no
domain is allowed to reach into another domain's database collection
directly. Cross-domain communication happens only through small, explicit
"capability" interfaces — e.g. the Estimates module doesn't ask "give me
every Cost record," it asks "tell me each cost item's estimated amount,
one at a time." This keeps domains independently understandable and
testable, and makes it possible to later extract any one domain into its
own service without a rewrite.

**ADR 0003 — Local-First Infrastructure.** The entire system runs and is
fully testable on a developer's own machine via Docker Compose, with no
dependency on AWS or any other cloud service to build or test Phase 1.

---

## 4. Multi-tenancy and security model (established Milestone 1, holds throughout)

Every contractor company is an independent tenant. Every business record
carries a `companyId`. Every database query is scoped to the
authenticated user's own company — enforced at the backend, never trusted
to frontend filtering.

**The tenant-isolation guarantee, stated precisely and verified by
automated tests at every milestone:** if Company A tries to access a
record that belongs to Company B (even using a real, valid database ID),
the system returns exactly the same "not found" response it would give
for an ID that doesn't exist at all. There is no way to distinguish "this
record exists but isn't yours" from "this record doesn't exist" — closing
a common enumeration/information-leak class of bug.

This guarantee is proven, not assumed: every milestone ships with a suite
of real end-to-end HTTP tests (against a real, ephemeral MongoDB instance)
that register two separate companies and prove neither can see, list, or
modify the other's data under any circumstance, including a deliberately
falsified request pretending to belong to the other company.

---

## 5. Milestone progress

```text
[DONE]  M0  Foundation
[DONE]  M1  Identity & Tenancy
[DONE]  M2  Project Foundation
[DONE]  M3  Resources & Costing
[DESIGNED/PLANNED, NOT YET BUILT]  M4  Estimates   <-- current position
[NOT STARTED]  M5  Quotations (customer-facing)
[NOT STARTED]  M6  Procurement (Suppliers, RFQs)
[NOT STARTED]  M7  Payments & Profitability Dashboard
```

Each completed milestone followed the same disciplined process: a written
design specification (reviewed and explicitly approved before any code was
written), a task-by-task test-driven implementation plan, then
implementation with every business rule proven by an automated test before
being marked done. Nothing has been declared complete without running
proof (build clean, tests passing, live end-to-end verification against a
real database).

### Milestone 0 — Foundation (complete)

Bootstrapped the whole local development environment: Docker Compose
(MongoDB + Mailpit), the Go backend's technical skeleton (config, logging,
database access, mail, background jobs, the AI-provider abstraction), the
Money/Quantity primitives every later milestone depends on, and the
Next.js frontend shell. Twenty domain-module directories were scaffolded
(empty placeholders) matching the full Phase-1 domain map, so later
milestones fill in already-agreed-upon slots rather than inventing new
structure each time.

### Milestone 1 — Identity and Tenancy (complete)

User accounts, password security (bcrypt hashing), JWT-based
login/session/refresh/logout, Company registration, and Company
Membership with roles (Owner/Admin/Employee). This is the milestone that
established the core tenant-isolation guarantee described in Section 4 —
every subsequent milestone inherits and re-proves it rather than
re-deriving it.

### Milestone 2 — Project Foundation (complete)

The core project hierarchy a contractor actually works in day to day:

```text
Company
  └── Client               (a contractor's customer)
       └── Project          (one renovation job)
            ├── Property    (0 or 1 — the physical address/site)
            ├── Space       (rooms/areas: "Kitchen," "Master Bathroom")
            └── Work Item   (the actual work: "Install ceramic floor tiles")
```

Five new business domains, full CRUD, all under the tenant-isolation
guarantee. Along the way, a real bug from Milestone 1's plumbing was found
and fixed: the authenticated API routes were accidentally being served
from a second, separate API document, meaning the public developer
documentation page didn't correctly show the authenticated endpoints or
require the correct login credentials in its "try it out" UI. Fixed so
there is exactly one API document for the whole backend, and it correctly
requires and documents authentication.

### Milestone 3 — Resources and Costing (complete)

This is the milestone where real project cost tracking becomes possible:

```text
Company
  ├── Material     (a reusable catalog item: "Ceramic Tile 600x600," with
  |                a reference/default price contractors can pre-fill from)
  └── Worker       (a reusable person/crew: "Ahmad, Tiler, RM150/day")

Project
  └── Work Item
       ├── Labour Entry     (a specific person's time logged against a
       |                     specific piece of work)
       └── Cost Item        (the universal cost ledger entry — every
                             dollar the project will ever cost, of any
                             kind, lives here)
```

**The single most important design decision of this milestone:** every
cost of every kind (material, labour, subcontractor, equipment,
transport, permits, professional fees, utilities, miscellaneous) flows
into one universal ledger record type, `Cost Item`. A `Cost Item` carries
up to four independent dollar amounts that can all be true
*simultaneously*, representing the natural lifecycle of a real cost:

```text
Estimated  ->  Committed  ->  Actual  ->  Paid

  e.g.  Estimated RM1,000   (contractor's planning-time guess)
        Committed RM950     (accepted supplier quote)
        Actual    RM1,080   (what the invoice actually said)
        Paid      RM500     (running total actually paid so far)

  All four can be known at once for the same cost line — this is not a
  single status that moves through stages, it's four independent facts.
```

A second important rule locked in here, because it directly protects
financial trustworthiness: **once a cost figure is recorded, correcting a
worker's default pay rate or a material's catalog price later never
silently rewrites history.** A `Cost Item` created last month keeps its
own recorded numbers forever, regardless of what today's catalog price or
today's worker rate happens to be. This is the same principle that will
protect issued customer quotations later (Milestone 5) and is why
Milestone 4 (Estimates, discussed next) is designed around
point-in-time snapshots rather than always-live numbers.

During implementation, a design review by the project owner caught five
real correctness issues before they shipped (a tenant-isolation gap on
one list endpoint, three validation-ordering bugs in cost creation, a
data-model gap that could have let a labour-origin cost record be
silently miscategorized, and a partial-update-field bug), plus one
independently-found technical issue (an internal naming collision in the
API-documentation generator that only appeared once all three new modules
were wired together). All were fixed and re-verified before this
milestone was marked done — none were deferred.

---

## 6. Current focus: Milestone 4 — Estimates

**Status: design and implementation plan fully written and approved.
Zero implementation code exists yet beyond an empty placeholder file.**
This is a deliberate checkpoint — the architecture below has been through
two rounds of critical review (one on the design, one on the
implementation plan itself) specifically to catch problems on paper,
where fixing them costs nothing, rather than in code or in production.

### What an "Estimate" is, in plain terms

An Estimate is the contractor's **private internal financial worksheet**
for a project — never seen by the customer. It answers: *given everything
this job is expected to cost, and the margin I want to make, what should
I charge?*

```text
  Project's recorded costs (Milestone 3's Cost Items)
                |
                v
       "Create an Estimate"
                |
                v
  +----------------------------------+
  |  Total estimated cost:  RM35,300  |
  |  Markup or margin applied         |
  |  Proposed selling price: RM50,000 |
  |  Projected profit:      RM14,700  |
  |  Projected margin:        29.4%   |
  +----------------------------------+
```

This is one of the product's headline features — it's what lets a
contractor see, before ever quoting a customer, whether a job is actually
going to be profitable and by how much.

### Why this took real design effort, not just coding

An Estimate is **not** a live, always-up-to-date calculator. If it were,
then every time a contractor corrected a material price weeks later,
every past estimate — including ones already shown to a customer as the
basis for a quote — would silently change underneath them. That's
exactly the trustworthiness problem the "never rewrite history" rule from
Milestone 3 was designed to prevent, one level up the chain.

So an Estimate is a **snapshot with an explicit refresh button** — the
contractor is always in control of exactly when new cost information is
pulled in, and old snapshots never move on their own:

```text
  V1 Draft  (a working document — safe to edit)
    |
    |-- pull in latest project costs (as many times as needed)
    |-- adjust markup/margin and preview the new numbers
    |-- (repeat freely — nothing is locked yet)
    |
    v
  "Finalize"  <-- one-way door. Once finalized, every number on
    |             this version is frozen forever. This is the version
    |             a future customer Quotation (Milestone 5) will be
    |             built from, so it must never silently change.
    v
  V1 Finalized  (permanent historical record)
    |
    |   project costs change again, a real re-quote is needed
    v
  "Create next version" (only allowed once V1 is finalized —
    |                     this cannot be used to sneak changes
    |                     into what should be a permanent record)
    v
  V2 Draft  ... same cycle repeats
```

Two safety rules worth calling out for a non-technical reader, because
they're the kind of thing that quietly prevents real business mistakes:

- **Only one working draft per project at a time.** A contractor can't
  accidentally end up with five different half-finished estimate drafts
  for the same job and lose track of which one is "the real one."
- **A stale-numbers safeguard on the "lock it in" button.** If two people
  (or two browser tabs) are looking at the same draft, and one of them
  refreshes the costs while the other is still staring at the old
  numbers, the system will not let the second person accidentally
  "finalize" numbers they never actually saw — it makes them look at the
  current numbers first. This directly prevents a contractor from
  unknowingly quoting off out-of-date figures.

### What was deliberately left out of this milestone, and why

Two features a first-pass design draft would have been tempted to
include were explicitly cut after review, on the principle of "don't
build a feature the product spec never actually asked for":

- **"Contingency" (a safety-margin buffer added to costs before pricing)**
  — this concept does not appear anywhere in the product specification.
  Rather than invent a financial rule with no grounding in what
  contractors actually said they need, it was removed entirely. It can be
  added later as a clean, additive change if real usage shows it's
  needed.
- **Per-line-item markup rates** (e.g. "20% on materials, 30% on labour")
  — the product spec's own examples show one blended markup for the
  whole job, not per-category rates. Building the more complex version
  now would be solving a problem nobody has asked for yet.

### Review discipline on this milestone specifically

Before any implementation work began, the written plan itself was put
through a second critical review pass (distinct from the design review)
and six real logic bugs were caught and fixed on paper — including one
where a database safety rule ("lock in the numbers") wasn't actually
being enforced correctly in the planned code, and one where a
retry-on-conflict strategy existed in test scaffolding but not in the
actual production logic it was supposed to prove. All six are now fixed
in the plan and will be verified again once real code is written,
following the same red-test/green-test discipline every prior milestone
used.

---

## 7. What comes after Milestone 4 (not yet designed)

```text
M5 — Quotations
     The customer-facing version of an Estimate. Strict rule already
     established in the product spec: the customer sees the complete
     price they're being asked to pay, but never sees the internal cost
     breakdown, markup, or profit margin behind it.

M6 — Procurement
     Supplier directory, Request-for-Quotation (RFQ) workflow, and
     supplier-specific secure links (a supplier only ever sees the one
     RFQ they were invited to, never the contractor's other data).

M7 — Payments & Profitability Dashboard
     Customer payment tracking (kept explicitly separate from cost
     tracking — a job can be profitable while still having unpaid
     customer balances) and the full project profitability dashboard
     that ties Estimated vs. Actual cost, contract value, and payments
     received into one view.
```

---

## 8. Process and quality discipline (why "done" means done)

Every milestone in this project has followed the same sequence, with no
exceptions:

1. **Design spec** — written, and explicitly reviewed/approved by the
   project owner before any implementation planning begins. Every design
   decision is traced back to either an explicit product-spec requirement
   or is flagged as "the product spec doesn't say — here's the proposed
   answer and why," so nothing is silently assumed.
2. **Implementation plan** — a concrete, step-by-step test-driven build
   plan derived from the approved design.
3. **Implementation** — every business rule is proven by an automated
   test *before* the code satisfying it is considered done (test-driven
   development). Nothing is marked complete on the strength of "it looks
   right" — it's proven by a failing test turning green.
4. **Verification** — a full build, static-analysis pass, and the entire
   automated test suite (unit + integration + full-stack acceptance
   tests against a real database) must pass with zero regressions in any
   previously completed milestone before a milestone is marked done.
5. **Live end-to-end check** — for every milestone so far, the actual
   running system has been manually exercised over real HTTP against a
   real database as a final sanity check, in addition to the automated
   suite.

This is why the completed-work log (`tasks/done.md`) for every prior
milestone includes not just "what was built" but "what was caught in
review and fixed before shipping" — the discipline is explicitly designed
to surface problems as early and cheaply as possible, rather than after
the fact.
