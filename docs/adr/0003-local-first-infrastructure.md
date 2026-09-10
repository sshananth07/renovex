# ADR 0003: Local-First Infrastructure

## Status
Accepted

## Context
The application must run entirely on a developer machine with no AWS
account and no external cloud dependency, while still being designed so
managed/cloud implementations can be substituted later without rewriting
business logic.

## Decision

| Concern | Phase 1 (local) | Future |
|---|---|---|
| Database | MongoDB via Docker Compose | MongoDB Atlas |
| File storage | Local filesystem (`./data/uploads/`) behind `FileStorage` | S3 (`S3FileStorage`) |
| Email | Mailpit (local SMTP catcher) behind `EmailSender` | Managed transactional email |
| Background jobs | In-process worker behind `JobQueue`, non-durable | SQS-backed queue |
| Auth | Local JWT (access + refresh pair), bcrypt hashing — implemented in Milestone 1 | Managed identity provider |
| AI | `AIGateway` interface, `AI_PROVIDER=mock` default | Real LLM/multimodal provider |

No AWS SDK dependencies are added while there is no functioning local
alternative behind the same interface. `AI_PROVIDER=mock` is the default
so the application is fully testable without external AI API credentials.

## Consequences
- Onboarding a new developer requires only Docker, Go, and Node — no cloud
  account.
- Swapping any of the above for a managed implementation later means
  writing a new implementation of an existing interface, not touching
  domain modules.
- Non-durable in-process jobs mean queued work is lost on process restart
  in Phase 1; acceptable for local development, revisit before any
  production deployment that requires durability guarantees.
