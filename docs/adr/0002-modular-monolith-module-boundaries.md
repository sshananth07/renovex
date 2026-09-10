# ADR 0002: Modular Monolith Module Boundaries

## Status
Accepted

## Context
Phase 1 is one Go application (a modular monolith), not microservices
(phase1.md §63). Without enforced boundaries, a monolith tends toward
tangled cross-module database access over time.

## Decision
- Each domain module (`identity`, `projects`, `quotations`, etc.) owns its
  own model, repository interface + MongoDB implementation, service, and
  Huma HTTP handlers.
- A module must never directly access another module's MongoDB repository
  or collection.
- Cross-module interaction happens only through narrow, explicitly defined
  capability interfaces exposed by the owning module's Service.
- Huma request/response DTOs stay at the HTTP boundary; handlers convert
  them to domain/application commands before invoking a Service.
- `internal/foundation` (Money, Quantity — business primitives) has no
  dependency on `internal/platform` or any domain module.
- `internal/platform` (Mongo, config, logging, storage, mail, jobs, AI
  gateway — technical infrastructure) has no dependency on domain modules
  and exposes no generic cross-domain CRUD repository.

Dependency direction: Chi Router → Huma API → Module Handler → Module
Service → Repository Interface → MongoDB Repository. Cross-module: Module
A Service → narrow capability interface → Module B Service.

## Consequences
- Slightly more boilerplate per module (each gets its own repository
  rather than sharing a generic one) in exchange for boundaries that hold
  as the codebase grows and stay compatible with the Go module structure
  Phase 1 has now.
- A module could later be extracted into a separate service without a
  data-access rewrite, if that ever becomes justified.
