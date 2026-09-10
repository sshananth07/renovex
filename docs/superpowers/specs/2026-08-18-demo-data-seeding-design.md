# Demo Data Seeding — Design Spec

**Status:** APPROVED (revision 3, after two rounds of code review)
**Date:** 2026-08-18
**Scope:** Development-only demo-data seeding/reset tooling for Renovex.
No production behavior changes, no public API changes, and no new domain
features. One mechanical production composition-root extraction is
permitted so the API and demo tooling reuse the identical service graph.

## 1. Problem and goal

There is no existing seed/demo-data mechanism in the repository. The user
needs a way to demonstrate Renovex's current functionality (through
M8.5B-A / AI Scope & Resource Preview) to a Project Manager, using a
realistic-looking contractor workspace — without touching the user's own
development User, Company, or data.

## 2. Non-negotiable constraints (from the user's brief)

- Completely separate demo User + Company. The identity model allows exactly
  one `CompanyMembership` per `User` (Milestone 1 design) — this is NOT
  changed. The demo User is never attached to the existing dev Company, and
  vice versa.
- Development-only: must be impossible to invoke accidentally outside
  development (see §4.1a — a stricter, explicit-presence guard, not the
  API's own default-permissive `cfg.AppEnv` check).
- Synthetic Malaysian-style renovation data only — no real customer/supplier
  data.
- Must not create real outbound-email risk (see §4.5 — a mailer-transport
  guard ahead of any mail-capable scenario).
- Idempotent seed, safe repeatable reset, both anchored on the stable demo
  identity `demo@renovex.local`.
- Reuse existing domain services wherever they reach the needed state.
  Direct Mongo insertion only as a last resort, with an explicit justification
  each time it's used.
- No public HTTP/OpenAPI contract changes.

## 3. Repository findings that shape this design

- `cmd/api/main.go` is a single, explicit composition root: it constructs
  every repository, calls `EnsureIndexes` on all of them, then wires ~25
  services in a strictly acyclic dependency order, then registers HTTP
  handlers. There is no DI framework — it's plain Go constructor calls.
- `internal/identity.AuthService.Register(ctx, email, password, companyName)`
  already does exactly what "create a separate demo User + owner Company +
  Membership, with compensation on failure" means — it's the same path the
  real `/auth/register` HTTP handler uses. The seeder does not need to
  hand-roll user/company/membership creation at all.
- `Config.AppEnv` (env var `APP_ENV`, default `"development"`) is the
  existing, already-load-bearing environment signal (e.g. it already gates
  cookie security in `main.go`). Because it defaults to `"development"`
  when unset, it is not strict enough on its own to gate a destructive
  seed/reset tool — see §4.1a for the stricter, explicit-presence guard
  `demoseed` actually uses.
- Every domain module (clients, projects, properties, spaces, work,
  materials, costs, labour, estimates, quotations, materialrequirements,
  rfqs, suppliers) exposes tenant-scoped service methods taking `companyID`
  as a plain parameter — directly callable in-process once the composition
  root is built, exactly like the HTTP handlers call them.
- Project lifecycle status is **not** set directly by a generic "set status"
  method for the commercial stages: `ProjectStatusQuotationSent` and
  `ProjectStatusQuotationApproved` only advance via
  `access.Service.ShareQuotation` (contractor shares) and the client-facing
  external quotation-acceptance handler (`internal/access/external.go`),
  respectively. The seeder drives these through the real `access.Service`
  calls, not by force-writing a status field.
- The Milestone 8 Supplier journey (invitations → offers → awards) is gated
  by a real magic-link + email-OTP session flow
  (`internal/supplieraccess`), and Supplier Offer mutations
  (`internal/supplieroffers`) require an `AuthorizedSupplierOfferContext`
  resolved from that session — they are not directly callable with just a
  companyID. Critically, **both secrets in this flow are deterministically
  derived, not random**:
  - `rfqissuance.CreateInvitation` derives the raw invitation token via
    `secrets.InvitationKeyring.DeriveInvitationSecret(keyVersion, companyID,
    invitationID, accessGeneration)`.
  - `supplieraccess`'s `CreateChallenge` derives the OTP code via
    `secrets.SupplierVerificationCodeKeyring.DeriveCode(keyVersion,
    context)`, where `context` is built entirely from IDs/values already
    known to the caller (`ChallengeID, CompanyID, SupplierID, InvitationID,
    AccessGeneration, NormalizedRecipientEmail`).

  Both keyrings are constructed from the same `cfg.InvitationSecretKeys` /
  `cfg.SupplierVerificationCodeKeys` env vars the API itself uses. This means
  the seed command — running in-process with its own copy of the same
  keyrings — can **legitimately re-derive the same invitation link and the
  same OTP code the real email would have contained**, and then drive the
  entire Supplier journey (open invitation → verify challenge → session →
  create offer draft → quote lines → submit offer; and for the award
  project, a second competing Supplier + `awards.CreateAwardDraft` →
  `SelectAwardLine` → `FinaliseAward`) through the exact same service calls
  the real frontend uses. No Mailpit scraping, no fixture shortcuts, and no
  bypass of authorization are needed anywhere in the procurement chain.
- No existing seed/demo/fixture command exists anywhere in the repo (only an
  unrelated Go test fixture file matched a `*fixture*` search).

## 4. Architecture

### 4.1 New command: `backend/cmd/demoseed/main.go`

A single binary with two subcommands, invoked as:

```
go run ./cmd/demoseed seed
go run ./cmd/demoseed reset
```

`main.go` loads `.env` exactly like `cmd/api/main.go` (same fallback-to-repo-root
logic), then runs the environment guard in §4.1a **before** calling
`config.LoadFromEnv()` or opening any Mongo connection.

It then builds the **identical composition root** `cmd/api/main.go` builds —
same repositories, same `EnsureIndexes` calls, same acyclic service wiring —
by extracting that construction sequence into a shared, reusable function
(see §4.4) so the seeder can never silently drift from what the real API
actually wires. `cmd/demoseed` does not start an HTTP server; it only needs
the constructed services.

Based on the subcommand, it then calls into `internal/demoseed`.

### 4.1a Environment guard (fail-closed, defense in depth)

`cfg.AppEnv == "development"` is not, by itself, strict enough for a
destructive seed/reset utility: `Config.AppEnv` defaults to `"development"`
when `APP_ENV` is unset (`getEnvOrDefault("APP_ENV", "development")` in
`internal/platform/config/config.go`), so a deployment that simply forgot to
set `APP_ENV` — while still pointed at production Mongo credentials — would
silently satisfy that check. That is acceptable for the API's own cookie-
security branching; it is not acceptable for a tool that deletes tenant
data.

`cmd/demoseed` therefore checks the raw environment directly, independent of
`config.LoadFromEnv()`, requiring **both** of the following before doing
anything else:

1. `APP_ENV` must be explicitly present in the process environment (checked
   via `os.LookupEnv`, not a default-filled read) **and** equal to
   `"development"`. Unset → refuse. Set to anything else → refuse.
2. `RENOVEX_DEMO_TOOL_ENABLED` must be explicitly present **and** equal to
   `"true"`. Unset, empty, or any other value → refuse.

```
missing APP_ENV                     → refuse
APP_ENV != "development"            → refuse
missing RENOVEX_DEMO_TOOL_ENABLED   → refuse
RENOVEX_DEMO_TOOL_ENABLED != "true" → refuse
otherwise                           → proceed to config.LoadFromEnv()
```

Both `seed` and `reset` run this exact check independently, first, before
any Mongo connection is opened or `config.LoadFromEnv()` is called. Refusal
prints a clear message to stderr and exits non-zero; nothing is touched.

### 4.2 New package: `backend/internal/demoseed/`

Contains the actual seeding/reset logic, organized as:

- `manifest.go` — owns the `demo_seed_manifest` collection and its
  `provisioning`/`ready`/`resetting` state machine (§6.2); the single place
  both `seed` and `reset` go through to resolve, create, or transition the
  demo tenant's identity.
- `identity.go` — creates the demo User/Company/Membership via
  `identity.AuthService.Register`, and verifies an existing one via
  `identity.VerifyPassword` (§6.1) against the required
  `RENOVEX_DEMO_PASSWORD` env var — never via `AuthService.Login`, to avoid
  creating a real `AuthSession` on every seed run.
- `scenario_project1.go` .. `scenario_project6.go` — one function per Project
  from §5, each taking the constructed services (as narrow interfaces the
  package itself defines, matching the codebase's consumer-defines-interface
  convention) and the resolved demo `companyID`, and driving only real
  service calls.
- `catalog.go` — the shared Material catalog and Supplier directory seeding
  (used by multiple projects).
- `idempotency.go` — shared "find-or-create by natural key" helpers (e.g.
  find a Client by name within companyID before creating) plus the
  deterministic operation-ID scheme (§6.3) and observed-state inspection
  used before each scenario transition.
- `reset.go` — drives the reset half of the manifest state machine (§6.2/
  §6.4): transitions the manifest to `resetting`, deletes demo-owned records
  in dependency-safe order using the manifest's stored anchor IDs, then
  deletes the manifest last.

All exported functions take already-constructed service instances — nothing
in this package opens a Mongo connection or reads `config` itself, so it's
unit-testable with fakes/stubs satisfying the same narrow interfaces the
domain modules already define for cross-module use.

### 4.3 Supplier-journey driver (`demoseed/supplier_journey.go`)

Encapsulates the deterministic derivation described in §3: given an
`*secrets.InvitationKeyring` and `*secrets.SupplierVerificationCodeKeyring`
(constructed once in `main.go` from `cfg`, same as `cmd/api/main.go` already
does), plus a created `SupplierInvitation`, it:

1. Re-derives the raw invitation token, calls `supplierAccessService.OpenInvitation`.
2. Calls `supplierAccessService.CreateChallenge`, then re-derives the OTP
   code the same way `buildChallenge` did, and calls
   `supplierAccessService.VerifyChallenge` with it.
3. Uses the resulting session token to call
   `supplierOffersService.ResolveMutationContext`,
   `CreateOrGetActiveDraft`, `QuoteDraftLine` (per RFQ line), and
   `SubmitOffer`.

This one driver is reused for every Supplier Offer seeded across Projects 4
and 5 (each Supplier invited gets its own invitation → own deterministic
token/code — no collision).

### 4.4 Composition-root extraction (small refactor to `cmd/api`)

To satisfy "no parallel seeding architecture" and "prefer existing wiring,"
the repository/service construction block currently inline in
`cmd/api/main.go` (roughly lines 88–638) is extracted into
`internal/platform/composition.BuildServices(ctx, cfg, logger, db) (*composition.Services, error)`
returning a struct of every constructed service. `cmd/api/main.go` calls
this function and then only does HTTP registration; `cmd/demoseed/main.go`
calls the same function and then only does seeding. This guarantees the
demo tooling can never drift from the real service graph, and is the
smallest change that removes duplication rather than inventing a second copy
of ~550 lines of wiring.

This is the one non-additive change this task makes to existing code. It is
a mechanical extraction (move, not rewrite). Immediately after performing
it, and before any seeder code is written on top of it, all three of
`go build ./...`, `go vet ./...`, and `go test ./...` must pass exactly as
they did before the extraction — proving the move changed no behavior,
not just that the code compiles.

### 4.5 Mailer-transport guard (global seed preflight, not per-scenario)

Several scenarios exercise code paths that are capable of sending real
email (`access.ShareQuotation`'s notification, `rfqissuance.SendInvitation`,
`supplieraccess.CreateChallenge`'s OTP delivery). The brief is explicit that
demo seeding must not create real outbound-email risk, and re-deriving the
invitation token/OTP code locally (§4.3) must never be used as an excuse to
skip or weaken that real delivery path — it only lets the seeder *read* the
same values the email would have carried, not bypass sending it.

Since Projects 3–5 are a normal part of every `seed` run (not an optional
extra), an unsafe SMTP configuration is knowable up front — before any
tenant data is written — rather than discovered partway through seeding.
`demoseed seed`'s startup order is therefore:

```
1. environment guard (§4.1a) — before config.LoadFromEnv()
2. config.LoadFromEnv() + RENOVEX_DEMO_PASSWORD presence check
3. build composition root (Mongo connection, EnsureIndexes, services)
4. mailer-transport preflight (this section) — before any seeding begins
5. resolve/provision the demo tenant (§6.2) and run all scenarios
```

The preflight inspects the constructed `mail.EmailSender`'s configuration
(`cfg.SMTPHost`/`cfg.SMTPPort`) and requires it to resolve to the
repository's own local Mailpit instance (the `docker-compose.yml` service —
`localhost`/`127.0.0.1` on the compose-configured SMTP port) or another
value explicitly recognized as a development-only sink. If the configured
SMTP host does not match an approved local sink, `seed` refuses immediately
(exit non-zero, clear message identifying the offending host) **before
creating Project 1**, rather than leaving Projects 1–2 seeded and Project 3
onward missing because of a deterministic configuration error discovered
mid-run.

The same per-scenario check described in the first draft is kept as defense
in depth immediately before each mail-capable call site, in case the mailer
configuration is somehow reconstructed differently mid-process — but it is
no longer the only guard, and it is never the first time an unsafe
configuration could have been caught.

## 5. Seed data plan (per user's brief, confirmed against real domain rules)

| # | Project | Depth | Key real service calls |
|---|---|---|---|
| 1 | Taman Tun Condo Refresh | Spaces + Work Items only | `CreateProject`, `CreateProperty`, `CreateSpace`, `CreateWorkItem` |
| 2 | Bangsar Kitchen Renovation | + Materials, Labour, Cost Items, Estimate | + `CreateMaterial`, `CreateWorker`, `CreateLabourEntry`, `CreateCostItem`, `CreateEstimate`, `FinalizeEstimate` |
| 3 | Mont Kiara Apartment Upgrade | + Quotation, client share/approval | + `CreateQuotation`, `ReplaceLines`, `FinalizeQuotation`, `access.ShareQuotation` (→ auto-advances Project to `quotation_sent`), external accept call (→ auto-advances to `quotation_approved`) |
| 4 | Subang Family Home Renovation | + Material Requirements, RFQ, 2 Supplier Offers | + `CreateManualRequirement`, `ReviewRequirement`, `CreateRFQ`, `AddLine`, `MarkReady`, `IssueVersion`, `CreateInvitation` ×2, full Supplier journey driver ×2 (competing offers) |
| 5 | Damansara Heights Residence | + Comparison + Finalised Award | Same as Project 4, then `awards.CreateAwardDraft`, `SelectAwardLine`, `FinaliseAward` |
| 6 | KL Eco City Condo Renovation | Client/Project/Property/Scope Brief + small Material catalog only | `CreateProject`, `CreateProperty`, `UpdateProjectScopeBrief`. **No AI service calls during seeding** — `SuggestSpaces`/`SuggestWorkItems`/`SuggestResources` are left for the user to trigger live during the actual demo, per explicit instruction not to fake AI results. |

Per-project statuses use the real 8-value enum
(`lead, site_visit, estimating, quotation_sent, quotation_approved,
in_progress, completed, closed`); Projects 1/2/6 are left at their natural
`lead`/`estimating`-reachable state rather than force-set, Project 3 reaches
`quotation_approved` through the real advancement calls, Projects 4/5 are
manually advanced to `in_progress` via the existing `UpdateProjectStatus`
(a real, unconstrained transition per the M2 design) since that status has
no dedicated auto-advancement path and active procurement realistically
implies in-progress work.

Money/quantities: every amount flows through `money.Money` /
`money.RateBPS` / `shopspring/decimal` construction exactly as the
handlers require (Huma DTOs aren't involved since these are direct service
calls, but the underlying types are identical) — no float anywhere. Cost
figures are chosen so `Cost < Quotation selling value` holds naturally from
each project's own markup/margin pricing call, never hardcoded independently
of the real calculation service.

## 6. Idempotency design

Anchor identity: `demo@renovex.local`.

### 6.1 Demo password (explicit config, not random generation)

§7 of the first draft generated a random password on first seed and printed
it once, never persisting it. That breaks repeatability: a second `seed`
invocation (unattended, or in a later session) has no way to recover that
password to authenticate step 1 below. The demo password is instead a
**required** env var, `RENOVEX_DEMO_PASSWORD` — a development-only value the
user sets once in their local `.env`, exactly parallel to how every other
credential in this repo already comes from environment configuration. `seed`
refuses with a clear message if it's unset. The normal
`identity.CreateUser` → bcrypt hashing path is completely unchanged; the
only difference from a real registration is where the plaintext password
value originates.

If the demo email already exists but `RENOVEX_DEMO_PASSWORD` does not
authenticate it (wrong password, or a non-demo account that happens to use
that address), `demoseed` refuses outright: it does not reset the password,
does not take ownership, and does not guess that this is the demo account.
Verification uses the exported, side-effect-free
`identity.VerifyPassword(user.PasswordHash, RENOVEX_DEMO_PASSWORD)` against
the User found via `UserService.FindUserByEmail` — **not**
`AuthService.Login`, which would create a real `AuthSession` on every seed
run with nothing to ever clean it up mid-scenario. The interactive demo
login the user performs by hand afterward remains completely normal
`/auth/login`, sessions and all.

### 6.2 Demo seed manifest as a state machine (crash-safe provisioning and reset)

The first draft treated `demo_seed_manifest` as a single "already seeded"
marker written only after `Register` succeeded. That leaves a gap: `Register`
creates the User, Company, and Membership as one already-compensating unit
(§3), but writing the manifest afterward is a *separate* persistence step. A
crash between the two leaves a real demo User/Company with no manifest — and
since both `seed` and `reset` require the manifest to positively identify the
tenant, that state is then permanently unrecoverable by either command
without manual intervention. Reset has the mirror problem: deleting the
Membership/Company before the manifest, then crashing, leaves a live
manifest whose four-way identity check can never agree again (Company is
gone).

The fix is to make `demo_seed_manifest` a real lifecycle record with a fixed
ID, so there is exactly one row this whole flow ever contends over:

```
_id:          "renovex-demo:v1"     (fixed — natural single-writer lock)
seedVersion:  1
demoEmail:    "demo@renovex.local"
state:        provisioning | ready | resetting
demoUserId:   string (set once known, may exist before state=ready)
demoCompanyId: string (set once known, may exist before state=ready)
createdAt, updatedAt
```

**Provisioning (`seed`, no manifest exists yet):**
1. Create the manifest document with `state=provisioning` (upsert on the
   fixed `_id` — this is the serialization point; a concurrent second `seed`
   invocation loses the race and simply proceeds to the recovery branch
   below).
2. Run `Register`. On success, immediately update the manifest with the
   resulting `demoUserId`/`demoCompanyId`.
3. Update the manifest to `state=ready`.

**Recovery (`seed`, manifest already exists):**
- `state=provisioning`, and `demoUserId`/`demoCompanyId` are unset → the
  crash happened before `Register` ran (or mid-`Register`, which already
  self-compensates per §3). Resume from step 2 above — attempt `Register`
  again (or, if the User now exists because the prior `Register` actually
  succeeded before the crash, fall through to the next case).
- `state=provisioning`, and `demoUserId`/`demoCompanyId` **are** set → the
  crash happened after `Register` succeeded but before `state=ready` was
  written. Re-verify the referenced User/Company/Membership still exist and
  agree with the stored IDs (email, membership, company name all match).
  If they agree, this is a safe repair: advance the manifest to
  `state=ready` and continue. If they disagree, refuse — this is a genuine
  conflict, not an interrupted run.
- `state=ready` → normal case. Verify the demo email resolves to a User via
  `identity.VerifyPassword` (§6.1), that User's Membership resolves to the
  manifest's `demoCompanyId`, and that Company's name matches `Renovex Demo
  Contractor Sdn Bhd`. If any of these disagree, refuse. If they agree,
  proceed to scenario seeding using the manifest's `demoCompanyId`.
- `state=resetting` → a `reset` was interrupted. `seed` refuses: tell the
  user to finish or re-run `reset` first, rather than seeding on top of a
  half-deleted tenant.

**Reset, made symmetrically retry-safe:**
1. Read the manifest by its fixed `_id`.
   - No manifest, and no User exists at the demo email → reset is already
     complete; report success as a no-op.
   - No manifest, but the demo email or `Renovex Demo Contractor Sdn Bhd`
     Company still exists → refuse (ownership cannot be proven without the
     manifest; this needs manual review, not an automated guess).
   - Manifest exists with `state=ready` → conditionally transition it to
     `state=resetting` first (this transition, not the four-way identity
     check, is what every subsequent step trusts).
   - Manifest exists with `state=resetting` already → a prior reset was
     interrupted; resume directly from the deletion steps below using the
     manifest's stored `demoUserId`/`demoCompanyId` as the continuation
     anchor. A record that's already gone is expected, not an error.
   - Manifest exists with `state=provisioning` → refuse (an interrupted
     `seed`, not a `reset` situation; re-run `seed` to finish or repair it
     first).
2. With the manifest in `state=resetting` and its stored `demoUserId`/
   `demoCompanyId` as the anchor, delete tenant-owned records per §6.4/§6.5.
   Each deletion is naturally idempotent against "already removed by a
   previous attempt" (delete-by-ID/company-scope on an absent record is a
   no-op, not an error).
3. Delete the Membership, then the Company, then the demo User's
   `AuthSession` records, then the User itself — all using the manifest's
   stored IDs, not a fresh identity lookup (the Membership/Company may
   already be gone from a previous attempt).
4. Delete the manifest document **last**, only after every prior step in
   this attempt completed. A subsequent `seed` then correctly sees "no
   manifest, no demo User" and starts fresh.

This keeps the manifest as one small state machine rather than new
architecture: three states, one fixed document, and every command trusts the
manifest's own recorded state and IDs as the continuation anchor instead of
re-deriving identity from scratch on every retry.

### 6.3 Seed sequence

1. Resolve the demo tenant per the manifest state machine in §6.2 (creating
   it if absent, resuming/repairing if interrupted, refusing on disagreement
   or an in-progress `reset`).
2. For every child record (Client, Project, Space, WorkItem, Material,
   Supplier, ...), look up by a natural key scoped to the resolved demo
   `companyID` before creating (e.g. `ListMaterials` then match by name).
   If found, reuse its ID; if not, create it.
3. For every domain **operation** in a scenario — not just record creation —
   use a stable, deterministic operation identifier of the form
   `renovex-demo:v1:<project-slug>:<step-slug>` (e.g.
   `renovex-demo:v1:project5:rfq1:issue`,
   `renovex-demo:v1:project5:supplier1:offer-submit`,
   `renovex-demo:v1:project5:award:finalise`; exact format finalized in the
   implementation plan). Several real services already take an
   `actorUserID`/`operationID`-shaped input for exactly this kind of
   idempotent-retry support (e.g. `supplieraccess.CreateChallenge`'s
   `OperationID`) — the seeder supplies the same deterministic value on
   every run instead of a fresh one, so a legitimate retry is recognized as
   the same operation rather than rejected or duplicated.
4. Reruns **inspect current state before acting**, rather than blindly
   re-invoking a transition and hoping it happens to be idempotent:

   | Observed state | Action on rerun |
   |---|---|
   | RFQ draft | perform `MarkReady` |
   | RFQ ready, not issued | perform `IssueVersion` |
   | RFQ issued | reuse the existing issued version |
   | Invitation not yet opened | run the Supplier journey driver |
   | Offer already submitted | reuse the current offer version |
   | Award already finalised | reuse the existing Award Revision |

   This makes a partially-seeded prior run (crash, interrupted run,
   deliberate re-seed) resume from wherever it actually left off, rather
   than relying on incidental idempotency in the underlying service calls.

### 6.4 Reset sequence

Reset follows the manifest state machine in §6.2 exactly (resolve →
transition to `resetting` → delete using the manifest's stored anchor IDs →
delete manifest last). Two rules apply throughout the deletion phase:

1. Every delete is scoped by the manifest's stored `demoCompanyId` (or, for
   User/Membership/AuthSession, the manifest's stored `demoUserId`) — never
   a broad collection truncation, never a date-range delete, and never a
   fresh identity re-lookup mid-reset (the whole point of anchoring on the
   manifest's stored IDs is that a Membership or Company already deleted by
   a prior interrupted attempt must not block progress).
2. Deletion covers **all** tenant-owned records under that anchor, including
   records created after the original seed run — by later manual use of the
   demo account, or by running the real AI workflow during a demo (Project
   6) — not merely the specific records the seed scenarios themselves
   created. See §6.5 for the concrete collection list and §6.6 for how each
   collection's delete capability is sourced.

The demo User's own `AuthSession` records (contractor login sessions) are
deleted before the User itself, per the original brief.

### 6.5 Full reset collection scope

The first draft's reset list covered the seed-time procurement/commercial
chain but missed collections that accumulate from *using* the demo account,
not just from seeding it. The complete list, deepest-dependency-first:

Award records (outcomes, deliveries, acknowledgements, revisions, drafts,
line claims, chains) → Supplier Offer records (versions, drafts,
eligibility, withdrawals, chains) → Supplier Access records (sessions,
session/invitation bindings, verification challenges, verification
deliveries, verification rate-limit records, access exchanges) → RFQ
issuance records (issued versions, issuance chains, amendment drafts,
invitations, delivery attempts) → RFQs → Material Requirements → AI records
(`AIGenerationBatch`, `AISuggestion` — populated once the live AI demo in
Project 6 is actually run) → WorkResourceRequirements → Access records
(AccessGrants, AccessGroupStates — created by `ShareQuotation` in Project 3)
→ Approvals → Audit events (demo-company-scoped) → Quotations → Estimates →
Cost Items → Labour Entries → Workers → Materials → Work Items → Spaces →
Properties → Projects → Clients → Suppliers/Offerings/Preferences → the
demo User's `AuthSession` records → Company Membership → Company →
`demo_seed_manifest` → User.

This is the reset invariant: **reset removes all tenant-owned data for the
positively identified demo company, including records created after the
original seed by manual use or by the live AI workflow — not merely records
the seed command itself originally wrote.**

### 6.6 Where each collection's delete capability lives (module ownership)

The first draft's fallback — "add a narrow, demo-package-local repository
method" for a collection with no existing delete path — does not actually
preserve module ownership: a repository living inside `demoseed` that reads
or deletes another domain module's Mongo collection is cross-module
collection access no matter which package the file sits in (ADR 0002 is
about who touches a collection, not which directory the code is filed
under). The rule is corrected to a strict priority order, applied
per-collection during implementation:

1. **Reuse an existing owning-module delete/cleanup method** if one already
   exists (most modules have one, since compensation flows already need
   `Delete`/`DeleteBy*`).
2. **If absent, add the narrowest company-scoped cleanup capability to the
   owning module itself** — e.g. a `DeleteAllForCompany(ctx, companyID)
   error` method on that module's repository interface and Mongo
   implementation, following the exact same pattern the module's existing
   `Delete`/`DeleteBy*` methods already use. `demoseed` consumes it through a
   narrow interface it defines itself (consumer-defines-interface, matching
   the rest of the codebase), exactly like every other cross-module
   capability in this repository.
3. **Only if adding an owning-module method is demonstrably impractical**
   (to be judged case-by-case, expected to be rare) may the implementation
   plan propose a direct, explicitly-labeled development-tool exception —
   called out by name as an exception in the plan and in code comments,
   never disguised as normal module ownership.

`demoseed` itself owns exactly one collection: `demo_seed_manifest`. It has
no delete authority over any other module's collection through its own
repository code.

## 7. Safety

- The §4.1a explicit-presence environment guard (`APP_ENV` present and
  `"development"` **and** `RENOVEX_DEMO_TOOL_ENABLED=true`) is checked
  first, before `config.LoadFromEnv()` and before any Mongo connection, in
  both `seed` and `reset` subcommands independently.
- The demo password comes from the required `RENOVEX_DEMO_PASSWORD` env var
  (§6.1), never generated or persisted in plaintext by `demoseed` itself. It
  goes through the exact same `identity.CreateUser` → bcrypt path every real
  registration uses; nothing new is added to the password-handling code. On
  successful first seed, `demoseed` prints a confirmation that the demo
  account is ready and the login email, but not the password (the user
  already knows it — they set it).
- The §4.5 mailer-transport guard refuses any mail-capable scenario unless
  the configured SMTP transport resolves to an approved local development
  sink (the repo's own Mailpit), so seeding can never create real outbound-
  email risk even though it drives the real invitation/OTP/notification code
  paths.
- The §6.2 seed manifest's `provisioning`/`ready`/`resetting` state machine
  makes tenant identification crash-safe in both directions: an interrupted
  `seed` recovers or refuses cleanly instead of leaving an
  unrecoverable unmanifested tenant, and an interrupted `reset` resumes from
  its own recorded anchor IDs instead of losing the ability to re-verify a
  Company it already partially deleted. Neither command ever acts on a
  guess about which company is the demo tenant.
- The §6.6 module-ownership rule keeps collection deletion inside the owning
  domain module (reusing an existing delete method, or adding a narrow
  `DeleteAllForCompany` there) rather than letting `demoseed` read or delete
  another module's collection directly — preserving the same modular-
  monolith boundary (ADR 0002) the rest of the codebase already enforces.
- No git commands are run at any point. No subagents are used. All files
  are left as ordinary working files for the user to review.

## 8. Testing plan

Go tests (fakes for unit-level idempotency/isolation logic; a
Testcontainers-backed integration test — matching the existing
`internal/tenanttest` pattern — for the 13 acceptance criteria the user
listed in §"TESTS" of the brief, notably: separate User/Company, existing
tenant untouched, all seeded records carry the demo companyID, repeat-seed
idempotency, reset scoping, non-development refusal for both seed and
reset, and hashed passwords).

Additionally, covering the manifest state machine (§6.2) specifically,
since it exists to handle failure modes normal happy-path testing would
never exercise:
- Seed interrupted between `Register` succeeding and `state=ready` being
  written (simulated by manually creating that manifest state before
  re-running seed) → recovers by re-verifying and advancing to `ready`,
  without a duplicate User/Company.
- Seed interrupted before `Register` ever ran (`state=provisioning`, no
  stored IDs) → resumes by attempting `Register` again.
- Reset interrupted after the Company is deleted but before the manifest is
  deleted (simulated the same way) → a second `reset` resumes from the
  stored anchor IDs and completes, rather than refusing because the Company
  it's looking for is already gone.
- Reset run twice in a row (second run is a true no-op: no manifest, no
  demo User) → reports success, does not error.
- `seed` while a manifest is `state=resetting` → refuses.
- `reset` while a manifest is `state=provisioning` → refuses.
- The §4.5 mailer-transport preflight refuses before any Project is
  created when SMTP points somewhere other than the approved local sink —
  asserted by checking zero demo records exist after the refusal, not just
  that the command exits non-zero.
- `identity.VerifyPassword`-based checking (§6.1) creates no new
  `AuthSession` row across repeated `seed` invocations.

## 9. Explicitly out of scope

Everything in the user's "DO NOT IMPLEMENT" list: multi-company users,
account switching, production demo mode, auth bypasses, fake AI responses,
Purchase Orders, Award→Committed Cost, supplier notification UX, and any
M9+ functionality. This task also makes no OpenAPI/public-contract changes
and no frontend changes.

## 10. Open items carried into the implementation plan

These are mechanical detail, not design decisions, and will be resolved
while writing the task-by-task plan:
- Exact input DTO shapes for `awards.CreateAwardDraft`, `SelectAwardLine`,
  `FinaliseAward`, `supplieroffers.SubmitOffer`, `QuoteDraftLine`.
- Exact `RFQChainID` / issued-version plumbing between `rfqs.CreateRFQ` →
  `rfqissuance.IssueVersion` → `CreateInvitation`.
- Per-module confirmation of which repositories already expose a
  company-scoped delete method versus which need one added for `reset`.
- **Manifest transitions must use compare-and-set/expected-state
  persistence**, not read-then-write: creating the fixed-`_id` manifest
  document, `provisioning → ready`, and `ready → resetting` all need
  conditional Mongo updates (e.g. `findOneAndUpdate` with a filter on the
  expected prior `state`, mirroring the optimistic-concurrency pattern
  `estimates`/`quotations`/`awards` already use elsewhere in this codebase)
  so two concurrent `demoseed` invocations cannot both believe they own the
  same transition.
- **Reset's terminal-case table must be implemented exactly as specified**,
  not simplified during coding:

  | Manifest state | Demo User/Company | Action |
  |---|---|---|
  | absent | absent | already reset — report success as a no-op |
  | absent | present (User or Company matches) | refuse — ownership cannot be proven without a manifest |
  | `resetting` | (stored anchor IDs are authoritative) | resume cleanup from the stored IDs, do not re-derive identity |
  | `provisioning` | — | refuse — resume/repair via `seed`, never treat an in-progress provision as ready to reset |
  | `ready` | verified via §6.2's four-way check | normal case — transition to `resetting` and proceed |
