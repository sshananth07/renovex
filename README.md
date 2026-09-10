# Renovation Project Intelligence Platform

AI-assisted renovation project costing, quotation, procurement, and profitability
platform for contractors. See [`phase1.md`](phase1.md) for full Phase 1 product
scope and [`docs/superpowers/specs/2026-07-19-phase1-foundation-design.md`](docs/superpowers/specs/2026-07-19-phase1-foundation-design.md)
for the technical architecture.

## Prerequisites

- Go 1.25+
- Node.js 20+
- Docker + Docker Compose

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

- `apps/web/` — Next.js + TypeScript frontend
- `backend/cmd/api/` — application entrypoint
- `backend/internal/foundation/` — business primitives (Money, Quantity) with no infrastructure dependencies
- `backend/internal/platform/` — technical infrastructure (Mongo, config, logging, HTTP, storage, mail, jobs, AI gateway)
- `backend/internal/<domain>/` — vertically-owned domain modules (identity, projects, quotations, etc.)
- `backend/migrations/` — MongoDB schema migration scripts
- `docs/adr/` — architectural decision records
- `docs/superpowers/specs/` — design specs
- `docs/superpowers/plans/` — implementation plans
