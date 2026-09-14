# Renovex — Renovation Project Intelligence Platform

AI-assisted renovation project costing, quotation, procurement, and profitability
platform for contractors. See [`phase1.md`](phase1.md) for full Phase 1 product
scope and [`docs/superpowers/specs/2026-07-19-phase1-foundation-design.md`](docs/superpowers/specs/2026-07-19-phase1-foundation-design.md)
for the technical architecture. See [`CLAUDE.md`](CLAUDE.md) for architectural
invariants and rules an AI coding agent should follow when working in this repo.

## Three independently deployed services

This repo is a monorepo containing **three separately deployed services** —
each has its own Vercel project, its own `vercel.json`, and its own env vars.
There is no shared build step between them.

| Service | Directory | Stack | Vercel project (example) |
|---|---|---|---|
| **api** | `backend/` | Go 1.25+ (chi + Huma v2, mongo-driver v2) | `renovex-api` |
| **web** | `apps/web/` | Next.js + TypeScript | `renovex-web` |
| **ai-service** | `ai-service/` | Python FastAPI | a third Vercel project (e.g. `renovex-ai`) |

`api` is the only service holding Mongo/R2/email/AI-provider secrets. `web`
never holds a backend secret — only `NEXT_PUBLIC_*` values and a couple of
server-side (never `NEXT_PUBLIC_`) queue-bridge secrets. `ai-service` never
receives Mongo credentials; it has no direct database access in this
architecture.

## Prerequisites

- Go 1.25+ (matches `backend/go.mod`'s `go 1.25.0`)
- Node.js 22.x (matches `apps/web/package.json`'s `engines.node`)
- Python 3.12+ (matches `ai-service/pyproject.toml`'s `requires-python`)
- Docker + Docker Compose (for local MongoDB + Mailpit)

## Local development

1. Copy environment config:

   ```
   cp .env.example .env
   ```

2. Start infrastructure (MongoDB + Mailpit):

   ```
   docker compose up -d
   ```

   The local MongoDB runs as a single-member replica set because the narrowly
   scoped Supplier withdrawal claim transaction requires transaction support.
   Wait for the `mongo` service to become healthy before starting the API.

   A single-member replica set is for LOCAL DEVELOPMENT ONLY — it has no
   redundancy and is not production-safe. See
   [MongoDB topology requirements](#mongodb-topology-requirements).

   Mailpit UI: http://localhost:8025

3. Run the backend:

   ```
   cd backend
   go run ./cmd/api
   ```

   Health check: http://localhost:8080/health
   Readiness check: http://localhost:8080/ready

4. Run the frontend:

   ```
   cd apps/web
   npm install
   npm run dev
   ```

   App: http://localhost:3000

5. (Optional) Run the AI service — only needed if you're working on AI
   Copilot, spatial design reasoning, or reference-image generation. Every
   provider defaults to `mock` (deterministic, offline), so the rest of the
   app works without this running at all:

   ```
   cd ai-service
   pip install -e ".[dev]"
   cp .env.example .env
   uvicorn app.main:app --reload --port 8000
   ```

   Then point the Go backend at it (see `.env.example`'s `AI_SERVICE_URL`/
   `AI_INTERNAL_TOKEN` — both must be set together, and `AI_INTERNAL_TOKEN`
   must equal `ai-service/.env`'s `INTERNAL_API_TOKEN`).

## Deploying to production

Renovex deploys as **three independent Vercel projects**, each rooted at a
different subdirectory of this monorepo, plus MongoDB Atlas, Cloudflare R2,
and Resend as external managed services. There is no single "deploy the
monorepo" command — each service is provisioned and deployed separately.

```
Web (Next.js, Vercel project rooted at apps/web/)
    ↓ browser calls the API directly — no Next.js proxy layer
Go API (Vercel project rooted at backend/, Go framework preset)
    ├── MongoDB Atlas (replica set — see "MongoDB topology requirements")
    ├── Cloudflare R2 (sole durable object store in production)
    ├── Resend (transactional email)
    └── Python AI service (Vercel project rooted at ai-service/, FastAPI)
            ├── GLM (Z.ai) — spatial design reasoning
            ├── Cloudflare Workers AI (FLUX) — reference-image generation
            └── Hugging Face Hunyuan3D Space — 3D asset generation
```

Each of the three Vercel projects has its own `vercel.json` at its own root
(`backend/vercel.json`, `apps/web/vercel.json`, `ai-service/vercel.json`) —
none of them declare a custom `functions`/`rewrites` block; each is deployed
with Vercel's platform-managed build/serve behavior for its respective
framework preset (Go, Next.js, Python), and the exact internal mechanism
Vercel uses to run each language's code is not something this repo's config
controls or needs to control beyond what's in those files. If you fork this
project onto a different host, the entrypoints each framework needs are:
`backend/cmd/api/main.go` (Go binary), `apps/web` (standard Next.js build),
`ai-service/app/main.py` (`app = FastAPI(...)`, served via `uvicorn`/ASGI).

### 1. Provision external services first

Do these before touching Vercel, since the API's env vars depend on them:

1. **MongoDB Atlas** — create a cluster (a genuine **replica set**, not a
   single standalone node — see "MongoDB topology requirements" below).
   Create a database user and copy the `mongodb+srv://...` connection
   string.
2. **Cloudflare R2** — create a bucket, an R2 API token (Account →
   R2 → Manage API Tokens), and note the account ID and S3-compatible
   endpoint (`https://<account-id>.r2.cloudflarestorage.com`).
3. **Resend** — create an account and API key. Without a verified custom
   sending domain, you can only send from the shared sandbox address
   `onboarding@resend.dev`, and only to recipients you've explicitly
   verified in the Resend dashboard — real production email to arbitrary
   recipients requires verifying your own domain there first.
4. **Hugging Face** (optional — only if 3D asset generation is enabled) —
   deploy or gain access to a private Hunyuan3D Gradio Space, and mint an
   access token scoped to it.
5. **Cloudflare Workers AI** (optional — only for reference-image
   generation) — an account ID + API token with Workers AI access.
6. **Z.ai (GLM)** (optional — only for spatial design reasoning) — an API
   key from Z.ai's console.

### 2. Deploy `api` (Go) — `backend/`

Create a Vercel project with **Root Directory = `backend`**. Required
production env vars (set all of these on the Vercel project, Production
environment):

| Variable | Required? | Secret? | Purpose |
|---|---|---|---|
| `APP_ENV` | Yes — `production` | Public | Enables every fail-fast production guard below |
| `MONGO_URI` | Yes | Secret | Atlas connection string; **must not** contain `localhost`/`127.0.0.1` — boot fails otherwise |
| `MONGO_DATABASE` | Yes | Public | Database name |
| `OBJECT_STORE_PROVIDER` | Yes — must be `r2` | Public | Boot fails if left as `local` in production |
| `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, `R2_ENDPOINT` | Yes, all five together | Secret | R2 object storage credentials |
| `JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET` | Yes | Secret | Session signing — use long random values, e.g. `openssl rand -base64 48` |
| `APP_ALLOWED_ORIGINS` | Yes | Public | Comma-separated exact origins, `https` only — must include your `web` deployment's URL |
| `AUTH_REFRESH_COOKIE_SECURE` | Yes — `true` | Public | Boot fails if `false` in production |
| `AUTH_REFRESH_COOKIE_SAME_SITE` | Yes — `none` if `web` and `api` are on different domains (the normal Vercel case) | Public | `lax` only works if both are same-site |
| `EXTERNAL_API_BASE_URL` | Yes | Public | The **web** deployment's canonical public URL (e.g. `https://app.example.com`) — baked into emailed Supplier/Client portal links. Must not contain `localhost`/`127.0.0.1` — boot fails otherwise. Prefer your own canonical domain over a Vercel-assigned `*.vercel.app` URL if you have one, so links keep working if the assigned URL ever changes. |
| `EMAIL_PROVIDER` | Yes — `resend` | Public | `smtp` only works for local Mailpit |
| `EMAIL_DELIVERY_MODE` | Yes — `direct` | Public | `test_sink` is a **fatal boot error** when `APP_ENV=production` — this is deliberate, not a bug |
| `RESEND_API_KEY`, `RESEND_FROM` | Yes | Secret (key) | Required together when `EMAIL_PROVIDER=resend` |
| `INVITATION_SECRET_ACTIVE_VERSION`, `INVITATION_SECRET_KEY_V1` | Yes | Secret (key) | Supplier Invitation link signing — generate the key with `openssl rand -base64 32`; see the rotation notes in `.env.example` before ever changing an existing version's key |
| `SUPPLIER_VERIFICATION_CODE_ACTIVE_VERSION`, `SUPPLIER_VERIFICATION_CODE_KEY_V1` | Yes, if the Supplier RFQ/OTP flow is used | Secret (key) | Signs Supplier email-OTP verification codes |
| `SUPPLIER_SESSION_TOKEN_ACTIVE_VERSION`, `SUPPLIER_SESSION_TOKEN_KEY_V1` | Yes, if the Supplier RFQ/OTP flow is used | Secret (key) | Signs the Supplier session + CSRF token pair (see `docs/adr/` and the Supplier Access section of `CLAUDE.md`) |
| `SUPPLIER_RATE_LIMIT_FINGERPRINT_KEY` | Yes, if the Supplier RFQ/OTP flow is used | Secret | Rate-limit fingerprinting for Supplier verification requests |
| `TRUSTED_PROXY_CIDRS` | Optional | Public | Empty is valid (trusts the direct peer). Set this if you need correct per-client rate-limit scoping behind a proxy/LB in front of the API |
| `SERVERLESS_MODE` | Recommended — `true` | Public | Disables the in-process background ticker, which a bounded serverless runtime cannot safely host |
| `AI_SERVICE_URL`, `AI_INTERNAL_TOKEN` | Optional, together | Secret (token) | Required only if AI Project Setup (scope/work-item suggestions) is enabled. `AI_INTERNAL_TOKEN` must equal the `ai-service` project's `INTERNAL_API_TOKEN` |
| `HUGGINGFACE_SPACE_URL`, `HUGGINGFACE_TOKEN` | Optional, together | Secret (token) | Required only if 3D asset generation is enabled |
| `HUNYUAN_PROVIDER_TIMEOUT` | Optional (default `110s`) | Public | Hunyuan Gradio call timeout |
| `SPATIAL_WORKER_TOKEN` | Optional | Secret | Enables internal bounded-worker routes; must match the value set on `web` |
| `SPATIAL_QUEUE_ENQUEUE_URL`, `SPATIAL_QUEUE_ENQUEUE_TOKEN` | Optional, together | Secret (token/URL) | Go → Web's internal Vercel Queue enqueue bridge |

The **local-object-store-only** keyrings (`VISUAL_ASSET_CAPABILITY_*`,
`ASSET_GENERATION_SOURCE_CAPABILITY_*`) are **not applicable in production**:
they're only ever consulted when `OBJECT_STORE_PROVIDER=local`, which
production must never use. Leave them unset in production.

Full reference with local-dev defaults and inline explanation: root
[`.env.example`](.env.example).

### 3. Deploy `web` (Next.js) — `apps/web/`

Create a Vercel project with **Root Directory = `apps/web`**.

| Variable | Required? | Secret? | Purpose |
|---|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | Yes | Public | The **api** deployment's public URL — the browser calls this directly, there is no Next.js proxy layer |
| `SPATIAL_API_INTERNAL_URL` | Optional | Public | The API's URL as reached from this project's own server code (queue consumer/enqueue bridge) — may differ from the public URL if you use private networking |
| `SPATIAL_WORKER_TOKEN` | Optional | Secret | Must equal the same variable on `api`. **Never** prefix with `NEXT_PUBLIC_` — that would ship it to the browser |
| `SPATIAL_QUEUE_ENQUEUE_TOKEN` | Optional | Secret | Must equal the same variable on `api`. **Never** prefix with `NEXT_PUBLIC_` |

Full reference: [`apps/web/.env.local.example`](apps/web/.env.local.example).

`web` never holds a Mongo password, R2 secret, AI provider secret, Hugging
Face secret, or Resend secret — only the four variables above.

### 4. Deploy `ai-service` (Python) — `ai-service/`

Create a Vercel project with **Root Directory = `ai-service`**. This service
is entirely optional in the sense that Renovex's core costing/quotation/
procurement flows do not depend on it — only AI Copilot, spatial design
reasoning, and reference-image/3D asset generation do.

| Variable | Required? | Secret? | Purpose |
|---|---|---|---|
| `ENVIRONMENT` | Yes — `production` | Public | Fails startup if any of the three provider selectors below is still `mock` |
| `INTERNAL_API_TOKEN` | Yes | Secret | Shared bearer token; must equal `api`'s `AI_INTERNAL_TOKEN` |
| `AI_PROVIDER` | Yes — not `mock` | Public | Copilot/scope-suggestion provider (currently Gemini) |
| `GEMINI_API_KEY`, `GEMINI_MODEL` | Required if `AI_PROVIDER=gemini` | Secret (key) | Gemini credentials |
| `SPATIAL_AI_PROVIDER` | Yes — not `mock`, if spatial design reasoning is used | Public | Isolated from `AI_PROVIDER` — set independently |
| `GLM_API_KEY`, `GLM_MODEL`, `GLM_BASE_URL`, `GLM_TIMEOUT_SECONDS`, `GLM_MAX_OUTPUT_TOKENS`, `GLM_REASONING_EFFORT` | Required if `SPATIAL_AI_PROVIDER=glm` | Secret (key) | Z.ai GLM credentials — never sent to Go |
| `REFERENCE_IMAGE_PROVIDER` | Yes — not `mock`, if reference-image generation is used | Public | Isolated from the other two selectors |
| `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_FLUX_MODEL` | Required together if `REFERENCE_IMAGE_PROVIDER=cloudflare` | Secret (token) | Cloudflare Workers AI credentials |
| `AI_PROVIDER_MAX_RETRIES`, `REFERENCE_IMAGE_TIMEOUT_SECONDS`, `REFERENCE_IMAGE_MAX_BYTES` | Optional | Public | Tuning knobs; see `.env.example` for defaults |

Full reference: [`ai-service/.env.example`](ai-service/.env.example).

`ai-service` never receives Mongo credentials — it has no direct database
access in this architecture, and its provider API keys never reach Go or
the browser.

### After deploying: verify

1. `GET https://<api-domain>/health` and `GET https://<api-domain>/ready` —
   the latter fails closed (`503`) if Mongo isn't reachable or isn't a
   transaction-capable topology (see "MongoDB topology requirements").
2. Regenerate and commit the OpenAPI contract if you changed any Go handler
   signature — see "Generating the OpenAPI document" below. CI's
   `contracts` job fails the build if this drifts.
3. Confirm `web`'s `NEXT_PUBLIC_API_BASE_URL` actually points at the `api`
   deployment you just created, and that `api`'s `APP_ALLOWED_ORIGINS`
   includes `web`'s exact deployed origin — a mismatch here fails CORS
   silently in the browser console, not as a visible error page.
4. If Supplier RFQ/OTP flows are in use, confirm `AUTH_REFRESH_COOKIE_SAME_SITE=none`
   and `AUTH_REFRESH_COOKIE_SECURE=true` on `api` — `web` and `api` on two
   different Vercel domains is a cross-site browser context, and a
   same-site-only cookie policy silently breaks session/CSRF cookies in
   that topology with no error message pointing at the cause.

## Browser integration configuration

The backend enforces exact-origin CORS and an Origin/Sec-Fetch-Site guard on
the cookie-authenticated `/auth/refresh` and `/auth/logout` routes. Configure
via:

```
APP_ALLOWED_ORIGINS=http://localhost:3000
AUTH_REFRESH_COOKIE_SECURE=false
```

`APP_ALLOWED_ORIGINS` is a comma-separated list of exact origins (scheme +
host + optional port; no path, query, fragment, or credentials). Production
(`APP_ENV=production`) requires at least one entry, every entry must use
`https`, and `AUTH_REFRESH_COOKIE_SECURE` must be `true`:

```
APP_ENV=production
APP_ALLOWED_ORIGINS=https://app.example.com
AUTH_REFRESH_COOKIE_SECURE=true
```

External Client and Supplier links must open the frontend application while
the frontend continues to call the Go API directly. For local development use:

```
EXTERNAL_API_BASE_URL=http://localhost:3000
```

**CORS behavior**: an allowed `Origin` gets an exact echo on
`Access-Control-Allow-Origin` (never a wildcard), plus
`Access-Control-Allow-Credentials: true` and `Vary: Origin`. An unlisted,
malformed, or `null` origin receives no permissive CORS headers. A
credentialed preflight (`OPTIONS` with `Access-Control-Request-Method`) from
an allowed origin succeeds without invoking the target handler and returns
`Access-Control-Allow-Methods`/`-Headers`/`Expose-Headers`; from a
disallowed origin it returns a bounded `403`.

**Refresh/logout Origin matrix**: `/auth/refresh` and `/auth/logout` are
cookie-authenticated and therefore protected by an additional Origin guard
beyond CORS (CORS headers alone don't stop a same-site or no-CORS request
from carrying the cookie). An `Origin` header, when present, must exactly
match an allowed origin. When `Origin` is absent, the request is allowed
only if `Sec-Fetch-Site` is also absent (non-browser client) or is
`same-origin`/`same-site`; a `cross-site` value, or any other/unknown value,
is rejected. Bearer-authenticated routes (everything else) are not subject
to this guard — only the two cookie-mutating auth routes are.

**`GET /auth/me`**: returns the authenticated user's authoritative profile —
`{userId, email, companyId, companyName, role, mustChangePassword}` — by
re-reading the User, Company, and Membership from the database on every
call rather than trusting the access token's claims beyond identifying
which user/company to look up. A token whose claimed company no longer
matches the user's actual current membership is rejected with `401`, not
silently honored.

## Generating the OpenAPI document

```
cd backend
go run ./cmd/openapi -out ../apps/web/openapi/openapi.json
```

Registers every production route with inert service dependencies purely for
schema construction — no MongoDB connection, no SMTP connection, no network
listener. `-out -` writes to stdout instead of a file. Two consecutive runs
produce byte-identical output. This is the same route inventory the real
server serves (`cmd/api/main.go` and `cmd/openapi/main.go` are checked
against each other by an automated test so they cannot silently drift), so
it is safe to regenerate a frontend TypeScript client from this file without
running the full backend stack.

## F1 API conventions

- **Pagination**: every F1 list endpoint (`GET /clients`, `/projects`,
  `/companies/me/members`, `/spaces`, `/work-items`) accepts
  `page` (default 1), `pageSize` (default 25, max 100), `search` (trimmed,
  max 200 characters, matched as an escaped literal substring — never
  interpreted as caller-supplied regex), `sort`, and `order` (`asc`/`desc`,
  each endpoint has its own allowlisted sort fields and default). The
  response envelope is always `{items, page, pageSize, total}` — this
  intentionally **replaced** the old bare-array response shape
  (`{"clients": [...]}` etc.) for every one of these five endpoints; there
  is no unpaginated variant. `GET /properties` is unaffected (a Project has
  at most one Property, so pagination doesn't apply).
- **Partial `PATCH`**: `PATCH /clients/{id}` is a genuine partial update —
  an omitted JSON field leaves the stored value unchanged, and an explicit
  empty string clears an optional field. It no longer behaves as a
  full-object replace.
- **`PATCH /projects/{projectId}`**: renames a Project only (`{"name": "..."}`,
  required and non-empty). Distinct from `PATCH /projects/{id}/status`,
  which is unchanged. Client relationship and status are untouched by this
  route.
- **`PATCH /work-items/{workItemId}`**: partially updates `description`,
  `workType`, `quantityValue`+`quantityUnit` (must be supplied together),
  and `spaceId`. `spaceId` is a genuine tri-state field: omit the key to
  leave the current Space assignment unchanged, send `"spaceId": null` to
  clear it, or send a Space ID to (re)assign it — validated to belong to
  the WorkItem's own Project. `projectId` is immutable and not part of this
  patch. A cancelled WorkItem rejects any edit with `409` (cancellation
  itself still goes through `PATCH /work-items/{id}/status`).

## Tests

Backend unit + MongoDB integration tests (spins up an ephemeral MongoDB
container via testcontainers-go — requires Docker running, no need to
`docker compose up` first):

```
cd backend
go test ./...
```

Frontend:

```
cd apps/web
npm run typecheck
npm test
```

AI service:

```
cd ai-service
ruff check app tests
pytest -q
```

## Continuous integration

`.github/workflows/ci.yml` runs on every PR/push to `main` and gates merges
with five jobs: **go** (`go test -short ./...`, `go vet`, `go build` — the
fast subset, not the full integration suite), **web** (typecheck, vitest
unit tests, build), **python** (ruff, pytest, all providers mocked so no
real API keys are needed in CI), **contracts** (regenerates the OpenAPI
document and the Web TypeScript client, and **fails if that regeneration
produces any diff** against what's committed — this is what keeps the
frontend's types honest against the backend's actual routes), and
**hygiene** (no real `.env`/`.env.local` files committed; no build/cache
artifacts tracked).

The **full** Go integration suite (`go test -count=1 ./...` against a real
ephemeral MongoDB + Mailpit) runs separately via
`.github/workflows/integration.yml`, on a nightly schedule or manual
dispatch — it's deliberately not part of the merge gate because of its
runtime.

## MongoDB topology requirements

The backend requires a **transaction-capable MongoDB topology** in every
environment. The Supplier Offer withdrawal boundary claims eligibility and
fences the offer chain inside one narrowly scoped multi-document transaction,
which MongoDB permits only on a replica set or a sharded cluster — never on a
standalone server.

| Environment | Required topology | Notes |
|---|---|---|
| Local development | Single-member replica set (`docker compose up`) | Convenient and transaction-capable, but has **no redundancy**. Not production-safe. |
| CI | Replica-set-capable MongoDB | The backend suite starts ephemeral replica-set containers via testcontainers-go, so CI runners must be able to run Docker. |
| Production | Managed or self-hosted **replica set** (minimum three voting members) or a **sharded cluster**, with redundancy appropriate to the deployment | A single-member replica set must not be used: losing its only member loses availability and unreplicated writes. |

Two independent checks enforce this:

- **Startup** fails fast with privacy-safe operational diagnostics if the
  connected topology cannot run the withdrawal transaction.
- **`GET /ready`** verifies both connectivity *and* transaction capability on
  every call, using a side-effect-free read-only transaction under a short
  bounded timeout. Any unready cause — unreachable, misconfigured, or a
  topology without transactions — returns the same generic
  `503 service_not_ready`. Replica-set names, topology details, driver errors
  and connection strings never appear in that response; they are recorded as
  operational diagnostics instead.

Because `/ready` fails closed on a non-transactional topology, point it at your
orchestrator's readiness probe so a pod that cannot serve withdrawals is never
sent traffic.

## Project structure

- `apps/web/` — Next.js + TypeScript frontend (its own Vercel project)
- `backend/cmd/api/` — Go application entrypoint (`main()`, long-lived binary; used for local dev and by CI)
- `backend/api/` — intentionally empty; Vercel's Go framework preset builds and deploys this project without a custom `api/*.go` handler or `vercel.json` `functions`/`rewrites` block, see "Deploying to production"
- `backend/internal/foundation/` — business primitives (Money, Quantity) with no infrastructure dependencies
- `backend/internal/platform/` — technical infrastructure (Mongo, config, logging, HTTP, storage, mail, jobs, AI gateway)
- `backend/internal/<domain>/` — vertically-owned domain modules (identity, projects, quotations, supplieraccess, etc.)
- `backend/migrations/` — MongoDB schema migration scripts
- `ai-service/app/` — Python FastAPI AI service (its own Vercel project)
- `docs/adr/` — architectural decision records: `0001` (Money/decimal strategy), `0002` (modular monolith module boundaries), `0003` (local-first infrastructure)
- `docs/superpowers/specs/` — design specs
- `docs/superpowers/plans/` — implementation plans, one per milestone
