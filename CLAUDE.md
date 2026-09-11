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
tester deployment target is Vercel (Web + Go API) + MongoDB Atlas + Cloudflare R2 +
external AI providers** — see "Deployment topology" below; do not assume this project has
no external cloud dependency in production.

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

**This project must remain fully local — do not run any git command** (`init`, `add`,
`commit`, etc.) or assume a git repository exists here unless explicitly asked. Some
tooling initializes git as a side effect (e.g. `create-next-app`); remove any `.git`
directory it creates immediately.

**Current state:** Milestone 0 (project foundation) is complete — see `tasks/done.md` for
what's built. Milestone 1 (Identity and Tenancy: User, Company, Company Membership, JWT
auth, tenant-scoped authorization) is next per the milestone ordering in the Milestone 0
plan.

---

## Deployment topology (M8.5C)

Production/tester intended topology — not yet provisioned on GitHub/Vercel as of this
writing; this describes the target the codebase is already built for:

```
Web (Next.js, Vercel)
    ↓
Go API (Vercel serverless function, api/index.go)
    ├── MongoDB Atlas
    ├── Cloudflare R2 (sole durable object store — OBJECT_STORE_PROVIDER=r2 required)
    └── Python AI service (FastAPI, its own deployment)
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

`AI_PROVIDER=mock`/local providers remain the default for **local development and tests
only** — see "Deployment env matrix" below for what production requires instead.

### Deployment env matrix

Full detail lives in each `.env.example` (root = Go, `ai-service/.env.example` = Python,
`apps/web/.env.local.example` = Web) — this is the ownership/visibility summary. "Web" here
always means public `NEXT_PUBLIC_*` config; the Go API is the only component that ever
holds Mongo/R2/AI-provider/email secrets.

| Variable | Owner | Required in prod? | Secret? | Purpose |
|---|---|---|---|---|
| `MONGO_URI` | Go | Yes (must not be localhost/127.0.0.1) | Secret | Atlas connection string |
| `MONGO_DATABASE` | Go | Yes | Public | Database name |
| `OBJECT_STORE_PROVIDER` | Go | Yes, must be `r2` | Public | Selects R2 vs local filesystem |
| `R2_ACCOUNT_ID`/`R2_ACCESS_KEY_ID`/`R2_SECRET_ACCESS_KEY`/`R2_BUCKET`/`R2_ENDPOINT` | Go | Yes (together) when `OBJECT_STORE_PROVIDER=r2` | Secret | R2 object storage credentials |
| `AI_SERVICE_URL` | Go | Optional (required together with `AI_INTERNAL_TOKEN` if AI Project Setup is enabled) | Public | Go→Python base URL |
| `AI_INTERNAL_TOKEN` | Go + Python | Optional (as above) | Secret | Go↔Python shared bearer token |
| `HUGGINGFACE_SPACE_URL`/`HUGGINGFACE_TOKEN` | Go | Optional (together; required if 3D asset generation is enabled) | Secret (token) | Go→Hunyuan Gradio Space |
| `HUNYUAN_PROVIDER_TIMEOUT` | Go | Optional (defaults 9m) | Public | Hunyuan call timeout |
| `EMAIL_PROVIDER` | Go | Should be `resend` in prod | Public | smtp vs resend transport |
| `EMAIL_DELIVERY_MODE` | Go | Must be `direct` in prod (fatal if `test_sink`) | Public | direct vs test-sink routing |
| `EMAIL_TEST_SINK_ADDRESS` | Go | Tester-only | Public (an inbox address) | test_sink transport recipient |
| `RESEND_API_KEY` | Go | Yes when `EMAIL_PROVIDER=resend` | Secret | Resend API key |
| `RESEND_FROM` | Go | Yes when `EMAIL_PROVIDER=resend` | Public | From address |
| `APP_ALLOWED_ORIGINS` | Go | Yes (https only) | Public | CORS/cookie origin allowlist |
| `AUTH_REFRESH_COOKIE_SECURE` / `AUTH_REFRESH_COOKIE_SAME_SITE` | Go | `true` / `none` for cross-site prod | Public | Refresh cookie Secure and SameSite flags |
| `JWT_ACCESS_SECRET`/`JWT_REFRESH_SECRET` | Go | Yes | Secret | Session signing |
| `INVITATION_SECRET_KEY_V*` / Phase D supplier keyrings | Go | Yes if those features are used | Secret | HMAC-derived credential keys |
| `SPATIAL_WORKER_TOKEN` | Go + Web (server-side) | Optional (enables internal worker routes) | Secret | Protects internal process-one routes |
| `SPATIAL_QUEUE_ENQUEUE_URL`/`SPATIAL_QUEUE_ENQUEUE_TOKEN` | Go + Web (server-side) | Required together for Vercel Queue wake | Secret (token) | Go↔Web Queue bridge |
| `SERVERLESS_MODE` | Go | `true` on Vercel | Public | Disables the in-process ticker |
| `NEXT_PUBLIC_API_BASE_URL` | Web | Yes | Public | Browser→Go API base URL |
| `SPATIAL_API_INTERNAL_URL` | Web (server-side) | Optional | Public | Web-server→Go internal URL |
| `ENVIRONMENT` | Python | Should be `production` in prod | Public | Enables T2D fail-fast provider guard |
| `AI_PROVIDER`/`SPATIAL_AI_PROVIDER`/`REFERENCE_IMAGE_PROVIDER` | Python | Must not be `mock` in prod | Public | Provider selectors, independent per route |
| `GEMINI_API_KEY`/`GLM_API_KEY`/`CLOUDFLARE_ACCOUNT_ID`/`CLOUDFLARE_API_TOKEN` | Python | Required when their provider is selected | Secret | Real provider credentials |
| `INTERNAL_API_TOKEN` | Python | Yes | Secret | Must match Go's `AI_INTERNAL_TOKEN` |

Critical boundary: Web never holds a Mongo password, R2 secret, AI provider secret, HF
secret, or Resend secret. Python never receives Mongo credentials — it has no direct
Mongo access in this architecture.

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
