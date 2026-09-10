# Phase 1 Foundation — Architecture & Milestone 0 Design

Status: Approved
Date: 2026-07-19
Source of truth for product scope: [`phase1.md`](../../../phase1.md) (do not silently reinterpret; see Conflicts section below)

## 1. Purpose

This document captures the architectural decisions needed to start building the Phase 1
Renovation Project Intelligence Platform, and the detailed scope for Milestone 0
(project foundation scaffold). Product scope, domain concepts, workflows, security model,
and AI boundaries are defined in `phase1.md` and are not repeated here except where an
implementation decision needs to be pinned down.

## 2. Relationship to phase1.md

`phase1.md` is the authoritative product/domain spec. This document is a technical
implementation overlay: local-only infrastructure, Go/Next.js stack choices, module
layout, and financial data representation. No requirement from `phase1.md` is removed
or reinterpreted here.

**Conflicts identified:** none substantive. One clarification: the build prompt's
folder listing under `internal/` omitted `labour/` in one enumeration while still
requiring `labour_entries` (§15) and Milestone 3 labour scope — treated as a listing
oversight; `internal/labour/` is included.

## 3. Repository Structure

```
/
├── apps/
│   └── web/                       # Next.js + TypeScript
│
├── backend/
│   ├── cmd/
│   │   └── api/                   # main.go: wiring, startup, graceful shutdown
│   │
│   ├── internal/
│   │   ├── foundation/            # business primitives, no infra dependencies
│   │   │   ├── money/             # Money, RateBPS, rounding policy
│   │   │   └── quantity/          # Quantity (decimal + unit)
│   │   │
│   │   ├── platform/              # technical infrastructure
│   │   │   ├── config/
│   │   │   ├── logging/
│   │   │   ├── mongo/
│   │   │   ├── http/              # chi/huma wiring, shared middleware
│   │   │   ├── storage/           # FileStorage interface + Local impl
│   │   │   ├── mail/              # EmailSender interface + SMTP/Mailpit impl
│   │   │   ├── jobs/              # JobQueue interface + in-process impl
│   │   │   └── ai/                # AIGateway interface + Mock provider
│   │   │
│   │   ├── identity/               companies/       access/        clients/
│   │   ├── projects/               properties/       spaces/        work/
│   │   ├── materials/              suppliers/        procurement/   labour/
│   │   ├── costs/                  estimates/        quotations/    payments/
│   │   ├── documents/              approvals/        audit/         ai/
│   │
│   ├── migrations/
│   └── go.mod
│
├── docker-compose.yml              # mongo, mailpit
├── .env.example
├── phase1.md
├── README.md
└── docs/
    ├── adr/
    └── superpowers/specs/
```

Each domain module (`identity`, `projects`, `quotations`, etc.) owns its own
`model.go`, `repository.go` (interface + Mongo impl), `service.go`, and `handler.go`
(Huma routes) from Milestone 1 onward. In Milestone 0 these directories contain only
`doc.go` describing the module's future responsibility — no speculative abstractions.

## 4. Module Boundary Rules

- A module must never directly access another module's MongoDB repository or collection.
- Cross-module interaction happens through narrow, explicitly defined capability
  interfaces exposed by the owning module's Service — not generic CRUD.
- Huma request/response DTOs stay at the HTTP boundary and are converted to
  domain/application commands before invoking a Service.
- `foundation` has no dependency on `platform` or any domain module — pure business
  primitives (Money, Quantity, Rate).
- `platform` has no dependency on domain modules — pure technical infrastructure
  (Mongo client, config, logging, storage, mail, jobs, AI gateway interfaces).

Dependency direction:

```
Chi Router → Huma API → Module Handler → Module Service → Repository Interface → MongoDB Repository
```

Cross-module:

```
Module A Service → Narrow capability interface → Module B Service
```

Never: `Module A → Module B Repository → Module B MongoDB collection`

## 5. Financial Data Representation

Canonical Money is `int64` minor units, never `float32`/`float64`, for any authoritative
financial value (material cost, labour cost, supplier offer price, estimated/committed/
actual/paid cost, quotation price, tax, discount, payment, profit).

```go
type Money struct {
    Amount   int64  `bson:"amount" json:"amount"`
    Currency string `bson:"currency" json:"currency"`
}
```

`RM18.50` is persisted as `{"amount": 1850, "currency": "MYR"}`.

Rates/percentages use fixed-point basis points, not floats:

```go
type RateBPS int64 // 600 = 6%, 2500 = 25%
```

Construction quantities (`32.75 m²`) use `shopspring/decimal`, not `float64`:

```go
type Quantity struct {
    Value decimal.Decimal
    Unit  string
}
```

**Calculation flow** (quantity × unit price → authoritative money):

```
decimal Quantity × Money unit price (converted to decimal for calculation)
    → precise decimal intermediate result
    → centralized explicit rounding (round-half-up to nearest minor unit)
    → int64 Money minor units (final, persisted)
```

Rounding is implemented once in `foundation/money` and used by every module —
never reimplemented per-module. Boundary cases (e.g. exact `.5` minor-unit results)
are covered by unit tests.

MongoDB Decimal128 is explicitly excluded as the default Phase 1 Money representation;
it may be reconsidered later only if a specific need justifies it (per `phase1.md` §51/§52
financial integrity and schema versioning principles — a change of this kind would ship
as a versioned migration, not a silent type change).

Money operations validate currency compatibility (adding MYR + SGD is an error, not a
silent bug).

## 6. Local-Only Infrastructure

No AWS services in Phase 1. All infrastructure runs on a developer machine via Docker
Compose plus local processes.

| Concern | Phase 1 (local) | Future |
|---|---|---|
| Database | MongoDB via Docker Compose | MongoDB Atlas |
| File storage | Local filesystem (`./data/uploads/`) behind `FileStorage` interface | S3 (`S3FileStorage`) |
| Email | Mailpit (local SMTP catcher) behind `EmailSender` interface | Managed transactional email |
| Background jobs | In-process worker behind `JobQueue` interface, non-durable | SQS-backed queue |
| Auth | Local JWT (access + refresh pair), bcrypt password hashing | Managed identity provider |
| AI | `AIGateway` interface, `AI_PROVIDER=mock` default | Real LLM/multimodal provider |

The application must build, start, and be fully testable with `AI_PROVIDER=mock` and
no external cloud credentials. AWS SDKs are not added as dependencies at this stage.

Interfaces are designed so a future AWS-backed implementation can be substituted without
changing business-domain code — but the AWS implementations themselves are out of scope
for Phase 1.

## 7. Go / Web Toolchain

- HTTP router: `github.com/go-chi/chi/v5`
- API framework: `github.com/danielgtaylor/huma/v2` via `humachi` adapter
- MongoDB driver: `go.mongodb.org/mongo-driver/v2` (not the legacy v1 driver)
- Decimal arithmetic: `github.com/shopspring/decimal`
- Frontend: Next.js (App Router) + TypeScript

Huma owns operation registration, request/response schemas, validation, and OpenAPI
generation. Chi owns cross-cutting HTTP middleware (request ID, panic recovery,
structured logging, auth, tenant context, CORS, timeouts). Business services depend on
neither Huma nor Chi types.

## 8. Authentication Model (design now, implement in Milestone 1)

Access + refresh JWT pair: short-lived access token, longer-lived refresh token stored
as an httpOnly cookie. Password hashing via bcrypt. This is the local implementation of
the `Identity/Auth abstraction` called out in the infrastructure constraint — it is
isolated enough that a managed identity provider could replace/augment it later without
touching business modules.

`.env.example` reserves `JWT_ACCESS_SECRET` / `JWT_REFRESH_SECRET` in Milestone 0, but
login, refresh, sessions, and company membership are **not** implemented until
Milestone 1. Milestone 0 ships no auth logic.

## 9. Testing Strategy

Three layers:

1. **Unit tests** — no MongoDB, no Docker dependency, fast business-logic tests
   (e.g. `foundation/money` rounding policy boundary cases).
2. **MongoDB integration tests** — via `testcontainers-go`'s MongoDB module, one
   container per test suite/package with isolated databases per test where practical.
   Never silently skipped in CI due to Mongo being unavailable — CI must provide a
   container runtime. Not dependent on the `docker-compose` Mongo instance, which is
   for local app development only.
3. **Application readiness tests** — `/health` (process liveness, `httptest`, no
   external dependency, expect 200) and `/ready` (verifies Mongo connectivity: tested
   both against a Testcontainers Mongo instance, expecting 200, and against an
   unavailable Mongo, expecting a non-2xx/503).

## 10. Milestone 0 Scope

**Backend**
- Go module init with the toolchain above.
- `cmd/api/main.go`: load config → connect Mongo → build chi router → mount Huma →
  register `/health` and `/ready` → start server with graceful shutdown.
- `internal/platform/config`: env-based config loader (`.env` via `godotenv` for local
  dev), typed `Config` struct. Includes `JWT_ACCESS_SECRET`/`JWT_REFRESH_SECRET` as
  reserved-but-unused config fields.
- `internal/platform/logging`: structured logger + chi request-ID/request-logging
  middleware.
- `internal/platform/mongo`: client factory, connection lifecycle, ping helper.
- `internal/platform/storage`: `FileStorage` interface + `LocalFileStorage` impl.
- `internal/platform/mail`: `EmailSender` interface + SMTP impl pointed at Mailpit.
- `internal/platform/jobs`: `JobQueue` interface + in-process impl.
- `internal/platform/ai`: `AIGateway` interface + `MockAIProvider`.
- `internal/foundation/money`: `Money`, `RateBPS`, centralized rounding policy + tests.
- `internal/foundation/quantity`: `Quantity` type.
- All 20 domain module directories created with `doc.go` only.
- Tests: `foundation/money` rounding unit tests (including boundary cases);
  `platform/mongo` Testcontainers integration test (write/read round trip); `/health`
  test via `httptest`; `/ready` tested against both a live Testcontainers Mongo
  (expect 200) and an unavailable Mongo (expect non-2xx).

**Frontend**
- Next.js + TypeScript app shell (App Router), minimal home page, lint/format config.
  No real features.

**Root**
- `docker-compose.yml`: mongo (named volume), mailpit.
- `.env.example`: Mongo URI, JWT secrets (reserved), `AI_PROVIDER`, SMTP host/port,
  storage path.
- `README.md`: prerequisites, `docker-compose up`, backend run, frontend run, test run
  instructions.
- ADRs:
  - `docs/adr/0001-money-and-decimal-strategy.md`
  - `docs/adr/0002-modular-monolith-module-boundaries.md`
  - `docs/adr/0003-local-first-infrastructure.md`

**Acceptance criteria**
- `docker-compose up -d` starts Mongo + Mailpit.
- `go run ./cmd/api` connects to Mongo and serves `/health` and `/ready` (200).
- `go test ./...` passes (unit + Testcontainers Mongo tests).
- `npm run dev` in `apps/web` serves a blank Next.js shell.

## 11. Explicitly Out of Scope for Milestone 0

Identity, Company, Company Membership, authentication (login/refresh/sessions),
tenant authorization, and all domain business logic — these begin at Milestone 1
per the milestone ordering in the build prompt. No AWS services. No Phase 2+
functionality (Digital Twin, LiDAR, Computer Vision, marketplace, compliance).
