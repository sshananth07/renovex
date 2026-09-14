# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

This is Phase 1 of an AI-assisted renovation project costing, quotation, procurement, and
profitability platform for contractors. `phase1.md` at the repo root is the **source of
truth for product scope, domain concepts, workflows, and the security model** — read it
before making any product or domain decision. Do not silently reinterpret or drop a
requirement from it; if something conflicts with a task, flag the conflict explicitly.

Architecture and technical decisions are recorded in:
- `docs/superpowers/specs/2026-07-19-phase1-foundation-design.md` — Milestone 0 architecture
- `docs/adr/` — individual architectural decision records (Money strategy, module
  boundaries, local-first infrastructure)
- `docs/superpowers/plans/` — implementation plans, one per milestone

**Stack:** Go 1.25+ (chi + Huma v2, mongo-driver v2) modular monolith backend, Next.js +
TypeScript frontend, Python (FastAPI) AI service, MongoDB. Local development runs via
Docker Compose (MongoDB + Mailpit) with every provider defaulting to mock/offline, so the
app never requires a real AI/cloud credential to run or test locally. **The production/
tester deployment target is three independently deployed Vercel projects — `api`
(`backend/`), `web` (`apps/web/`), `ai-service` (`ai-service/`) — plus MongoDB Atlas,
Cloudflare R2, and Resend as external managed services.** See "Deployment topology"
below and [`README.md`](README.md)'s "Deploying to production" for the full per-service
provisioning/env-var walkthrough; do not assume this project has no external cloud
dependency in production, and do not assume the three services share a single deploy
step — they don't.

**Backend module boundaries** (see ADR 0002): `internal/foundation` holds business
primitives (Money, Quantity) with zero infrastructure dependencies. `internal/platform`
holds technical infrastructure (Mongo, config, logging, storage, mail, jobs, AI gateway)
behind interfaces, with zero domain knowledge. Domain modules under `internal/<name>/`
own their own model/repository/service/handler and never reach into another module's
MongoDB collection directly — cross-module calls go through narrow service interfaces
only.

**Money and quantities are never floats.** Money is `int64` minor units + currency
(`internal/foundation/money.Money`); rates are fixed-point basis points (`RateBPS`);
quantities use `shopspring/decimal`. All rounding goes through the single centralized
`money.RoundToMinorUnits` function — never reimplement rounding per-module. See ADR 0001.

**AI principle** (from `phase1.md`): AI suggests, the system calculates, the contractor
approves. AI-generated data lives in `ai_suggestions` (pending/accepted/modified/rejected)
until a human approves it — it never silently becomes authoritative domain data, and it
never performs authoritative financial or quantity calculations.

**Domain map** — `phase1.md` has 66 numbered sections; the ones an agent will most often
need are: §1 core domain model, §2 identity/access grants, §5–8 project/property/space/
work-item structure, §12 supplier directory, §17–22 resource/cost/estimate model, §23–31
customer-facing quotation + portal + acceptance, §32–39 material requirements through
Supplier RFQ/offers (§33.1–33.3 cover RFQ invitation and secure-link delivery
specifically), §40–43 actual-cost tracking and profitability, §51–54 financial-integrity/
schema-versioning/idempotency/concurrency invariants that apply repo-wide, §61–63 the AI
and technical architecture chapters. Read the specific section a task touches rather than
the whole document when context budget matters, but §51–54's invariants apply regardless
of which section a task is otherwise scoped to.

**This project must remain fully local — do not run any git command** (`init`, `add`,
`commit`, etc.) or assume a git repository exists here unless explicitly asked. Some
tooling initializes git as a side effect (e.g. `create-next-app`); remove any `.git`
directory it creates immediately.

**Current state:** this codebase is well past initial scaffolding — identity/tenancy,
projects/clients/properties/spaces/work-items, costing/estimates/quotations, RFQ
issuance, Supplier Access (OTP-gated portal, session + CSRF cookies), Supplier Offers,
Awards, spatial/3D design generation, and the M8.5C Vercel/Atlas/R2/Resend production
deployment are all implemented and live in production as of this writing. Do not assume
an early-milestone state or re-derive "what milestone are we on" from a stale note — read
`docs/superpowers/plans/` for the actual milestone-by-milestone build history, and prefer
`git log`/the current code over any single stale summary line (including this one) for
what's actually built today.

---

## Deployment topology (M8.5C)

This is live, provisioned infrastructure (`renovex-api`, `renovex-web`, and a third
Vercel project for `ai-service`), not a future target:

```
Web (Next.js, Vercel project rooted at apps/web/)
    ↓ browser calls the API directly — no Next.js proxy layer
Go API (Vercel project rooted at backend/, Go framework preset)
    ├── MongoDB Atlas
    ├── Cloudflare R2 (sole durable object store — OBJECT_STORE_PROVIDER=r2 required)
    └── Python AI service (Vercel project rooted at ai-service/, FastAPI)
            ├── GLM reasoning provider (spatial design reasoning)
            ├── Cloudflare Workers AI / FLUX (reference-image generation)
            └── Hugging Face Hunyuan3D Space (3D asset generation)

Email: Go -> Resend (EMAIL_PROVIDER=resend), with an EMAIL_DELIVERY_MODE=test_sink
escape hatch for tester deployments only — APP_ENV=production with test_sink is a fatal
config error, never reachable in a real deployment.

Async spatial generation: the existing JobExecution lease/checkpoint state machine +
Vercel Queue wake path (SPATIAL_QUEUE_ENQUEUE_URL/TOKEN, SERVERLESS_MODE=true). No polling
fallback — if Vercel Queue is genuinely unavailable at deployment time, that is a
deployment-time decision, not something this codebase should silently substitute.
```

**Go entrypoint note:** `backend/api/` is currently an empty directory (there is no
`api/index.go`) and `backend/vercel.json` declares no custom `functions`/`rewrites`
block — this is intentional, a deliberate fix to a prior deployment issue, and the live
`api` deployment builds and serves correctly as-is. `backend/cmd/api/main.go` remains
this repo's Go application entrypoint (used for local dev and by CI). Vercel's own
internal mechanism for building/serving this project from its Go framework preset with
no custom `api/*.go` handler is platform-managed and not verified or controlled by
anything in this repo — do not assume `api/index.go` exists, and do not restore it or
add a `functions`/`rewrites` block "to fix" the deployment without first confirming with
the user that something is actually broken, since the current empty-`api/`-directory
setup is the working, intended state.

**Cross-origin cookies:** `web` and `api` are two different Vercel domains, which
browsers treat as cross-site. Any cookie set by `api` that must be usable by a
cross-site `fetch()` from `web` (the Supplier Access session/CSRF cookies, the auth
refresh cookie) needs `SameSite=None` **and** `Secure=true` together — `SameSite=None`
without `Secure` is spec-invalid and browsers silently drop the cookie with no error.
`AUTH_REFRESH_COOKIE_SAME_SITE=none` / `AUTH_REFRESH_COOKIE_SECURE=true` in production
is not optional hardening, it is required for login and Supplier Access to function at
all in this topology. Separately: a cookie set by `api`'s origin is never readable via
`document.cookie` from JavaScript running on `web`'s origin — that is a hard, unconditional
same-origin browser rule with no `SameSite`/`Secure`/`Domain` workaround. Where the
frontend needs a cookie's value in JS (e.g. the Supplier Access CSRF double-submit
token), the value must be delivered through a JSON response body instead, and held in
frontend memory/state rather than re-read from `document.cookie`.

`AI_PROVIDER=mock`/local providers remain the default for **local development and tests
only** — see "Deployment env matrix" below for what production requires instead.

### Deployment env matrix (per service)

Full detail with local-dev defaults and inline rationale lives in each service's own
`.env.example`: root `.env.example` (`api`), `ai-service/.env.example` (`ai-service`),
`apps/web/.env.local.example` (`web`). Below is the ownership/visibility summary, split
by the three independently deployed Vercel projects. Critical boundary: `web` never
holds a Mongo password, R2 secret, AI provider secret, Hugging Face secret, or Resend
secret; `ai-service` never receives Mongo credentials at all.

#### `api` (Go, `backend/`)

| Variable | Required in prod? | Secret? | Purpose |
|---|---|---|---|
| `APP_ENV` | Yes — `production` | Public | Gates every fail-fast production check below |
| `MONGO_URI` | Yes (must not be localhost/127.0.0.1) | Secret | Atlas connection string |
| `MONGO_DATABASE` | Yes | Public | Database name |
| `OBJECT_STORE_PROVIDER` | Yes, must be `r2` | Public | Selects R2 vs local filesystem |
| `R2_ACCOUNT_ID`/`R2_ACCESS_KEY_ID`/`R2_SECRET_ACCESS_KEY`/`R2_BUCKET`/`R2_ENDPOINT` | Yes (together) when `OBJECT_STORE_PROVIDER=r2` | Secret | R2 object storage credentials |
| `JWT_ACCESS_SECRET`/`JWT_REFRESH_SECRET` | Yes | Secret | Session signing |
| `APP_ALLOWED_ORIGINS` | Yes (https only) | Public | CORS/cookie origin allowlist — must include `web`'s exact deployed origin |
| `AUTH_REFRESH_COOKIE_SECURE` / `AUTH_REFRESH_COOKIE_SAME_SITE` | `true` / `none` for cross-site prod (the normal case: `web` and `api` on different Vercel domains) | Public | Refresh cookie Secure and SameSite flags — see "Cross-origin cookies" above; getting this wrong silently breaks login/Supplier Access with no visible error |
| `EXTERNAL_API_BASE_URL` | Yes, must be the canonical public **web** app URL (fatal if it contains localhost/127.0.0.1) | Public | Browser-facing origin baked into emailed Supplier/Client portal links |
| `EMAIL_PROVIDER` | Should be `resend` in prod | Public | smtp vs resend transport |
| `EMAIL_DELIVERY_MODE` | Must be `direct` in prod (fatal if `test_sink`) | Public | direct vs test-sink routing |
| `EMAIL_TEST_SINK_ADDRESS` | Tester-only | Public (an inbox address) | test_sink transport recipient |
| `RESEND_API_KEY`/`RESEND_FROM` | Yes when `EMAIL_PROVIDER=resend` | Secret (key) | Resend credentials |
| `INVITATION_SECRET_ACTIVE_VERSION`/`INVITATION_SECRET_KEY_V*` | Yes | Secret (key) | Supplier Invitation link HMAC signing — see rotation notes in `.env.example` |
| `SUPPLIER_VERIFICATION_CODE_ACTIVE_VERSION`/`SUPPLIER_VERIFICATION_CODE_KEY_V*` | Yes, if the Supplier RFQ/OTP flow is used | Secret (key) | Signs Supplier email-OTP verification codes |
| `SUPPLIER_SESSION_TOKEN_ACTIVE_VERSION`/`SUPPLIER_SESSION_TOKEN_KEY_V*` | Yes, if the Supplier RFQ/OTP flow is used | Secret (key) | Signs the Supplier session cookie AND derives the CSRF double-submit token from it (same keyring, two HMAC domains — see `supplieraccess` module) |
| `SUPPLIER_RATE_LIMIT_FINGERPRINT_KEY` | Yes, if the Supplier RFQ/OTP flow is used | Secret | Rate-limit fingerprinting for Supplier verification requests |
| `TRUSTED_PROXY_CIDRS` | Optional (empty trusts the direct peer) | Public | Set only if correct per-client rate-limit scoping behind a proxy/LB matters to you |
| `VISUAL_ASSET_CAPABILITY_*` / `ASSET_GENERATION_SOURCE_CAPABILITY_*` | **Not applicable in production** | Secret (key) | Only consulted when `OBJECT_STORE_PROVIDER=local`; production always uses `r2` instead, so leave these unset in production |
| `AI_SERVICE_URL`/`AI_INTERNAL_TOKEN` | Optional (required together if AI Project Setup is enabled) | Secret (token) | Go→Python base URL + shared bearer token; `AI_INTERNAL_TOKEN` must equal `ai-service`'s `INTERNAL_API_TOKEN` |
| `HUGGINGFACE_SPACE_URL`/`HUGGINGFACE_TOKEN` | Optional (together; required if 3D asset generation is enabled) | Secret (token) | Go→Hunyuan Gradio Space |
| `HUNYUAN_PROVIDER_TIMEOUT` | Optional (defaults 110s) | Public | Hunyuan call timeout |
| `SPATIAL_WORKER_TOKEN` | Optional (enables internal worker routes; must match `web`) | Secret | Protects internal process-one routes |
| `SPATIAL_QUEUE_ENQUEUE_URL`/`SPATIAL_QUEUE_ENQUEUE_TOKEN` | Optional together (required for Vercel Queue wake; must match `web`) | Secret (token) | Go↔Web Queue bridge |
| `SERVERLESS_MODE` | Recommended `true` | Public | Disables the in-process background ticker |

#### `web` (Next.js, `apps/web/`)

| Variable | Required in prod? | Secret? | Purpose |
|---|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | Yes | Public | Browser→Go API base URL — no Next.js proxy layer exists |
| `SPATIAL_API_INTERNAL_URL` | Optional | Public | Web-server→Go internal URL (may differ from the public URL under private networking) |
| `SPATIAL_WORKER_TOKEN` | Optional (server-side only — never `NEXT_PUBLIC_`) | Secret | Must equal `api`'s value |
| `SPATIAL_QUEUE_ENQUEUE_TOKEN` | Optional (server-side only — never `NEXT_PUBLIC_`) | Secret | Must equal `api`'s value |

#### `ai-service` (Python, `ai-service/`)

| Variable | Required in prod? | Secret? | Purpose |
|---|---|---|---|
| `ENVIRONMENT` | Should be `production` in prod | Public | Enables the fail-fast provider guard below |
| `AI_PROVIDER`/`SPATIAL_AI_PROVIDER`/`REFERENCE_IMAGE_PROVIDER` | Must not be `mock` in prod | Public | Three independent provider selectors, one per route family |
| `INTERNAL_API_TOKEN` | Yes | Secret | Must match `api`'s `AI_INTERNAL_TOKEN` |
| `GEMINI_API_KEY`/`GEMINI_MODEL` | Required when `AI_PROVIDER=gemini` | Secret (key) | Never sent to Go |
| `GLM_API_KEY`/`GLM_MODEL`/`GLM_BASE_URL`/`GLM_TIMEOUT_SECONDS`/`GLM_MAX_OUTPUT_TOKENS`/`GLM_REASONING_EFFORT` | Required when `SPATIAL_AI_PROVIDER=glm` | Secret (key) | Never sent to Go |
| `CLOUDFLARE_ACCOUNT_ID`/`CLOUDFLARE_API_TOKEN`/`CLOUDFLARE_FLUX_MODEL` | Required together when `REFERENCE_IMAGE_PROVIDER=cloudflare` | Secret (token) | Never sent to Go |
| `AI_PROVIDER_MAX_RETRIES`/`REFERENCE_IMAGE_TIMEOUT_SECONDS`/`REFERENCE_IMAGE_MAX_BYTES` | Optional | Public | Tuning knobs |

---

## Workflow & Behavior Rules

### Planning
- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions). Write a detailed spec upfront.
- If something goes sideways mid-task, STOP and re-plan immediately — don't keep pushing.
- Write the plan to `tasks/todo.md` with checkable items, then check in before starting implementation.
- Mark items complete as you go; when done, move a summary entry to `tasks/done.md`.

### Subagents
- Use subagents liberally to keep the main context window clean.
- Offload research, exploration, and parallel analysis to subagents.
- One focused task per subagent — don't bundle unrelated queries.

### Task Files
| File | Purpose |
|---|---|
| `tasks/todo.md` | Active plan — checkable items for the current task |
| `tasks/done.md` | Completed work log — append a summary after every task |
| `tasks/lessons.md` | Self-improvement log — rules derived from user corrections |

### Self-Improvement
- After ANY correction from the user, update `tasks/lessons.md` with the pattern and a rule to prevent it recurring.
- Review `tasks/lessons.md` at the start of each session for relevant lessons.

### Verification
- Never mark a task complete without proving it works (run tests, check logs, demonstrate correctness).
- When relevant, diff behavior between `main` and your changes before declaring done.
- Ask: "Would a staff engineer approve this PR?"

### Bug Fixing
- When given a bug report: fix it autonomously. Point at logs/errors/failing tests and resolve them.
- Find root causes — no temporary fixes or workarounds.

### Code Quality
- Make every change as simple as possible. Minimize code touched.
- For non-trivial changes, pause and ask "is there a more elegant solution?" before finalizing.
- If a fix feels hacky, implement the elegant solution instead.
- Skip elegance checks for simple, obvious one-liners.
