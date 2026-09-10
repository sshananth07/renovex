# Milestone 6 — Client Access and Quotation Approval: Design Specification

**Status: APPROVED** (2026-07-24, after three amendment rounds plus the
final coordinator write-order addendum and product decisions). Backend-only.
Implementation may proceed against this document.

**Authoritative inputs:**
- `phase1.md` (product source of truth) — §2, §27, §28, §29, §30, §31, §50,
  §51, §52, §53, §54, §55, §57, §65
- `docs/superpowers/specs/2026-07-24-milestone-5-quotations-design.md` (M5,
  approved and implemented — authoritative for `quotations.Quotation`'s
  actual shape; not reopened here)
- The actual M5 code as implemented: `backend/internal/quotations/*`,
  `backend/internal/estimates/*`, `backend/internal/projects/*`,
  `backend/internal/work/*`, `backend/cmd/api/main.go`,
  `backend/internal/tenanttest/router.go`
- ADR 0001 (Money), ADR 0002 (module boundaries)
- Brainstorming dialogue with the user (2026-07-24)
- Three amendment rounds plus the final addendum — see §18 Amendment Log

Claim labels used below: **[phase1.md]**, **[code]**, **[decision]**
(resolved with the user), **[addendum]** (the final approved write-order
and product decisions).

---

## 0. Investigation Summary

### 0.1 What phase1.md requires

- **§2 Access Grants**: minimal shape `resourceType, resourceId, granteeType,
  granteeId, permissions, status, expiresAt`. "The original raw token should
  not be stored in plaintext... the system should store a secure token hash."
  Core rule: `External stakeholder → Specific Access Grant → Specific
  Resource → Specific Allowed Actions`.
- **§27 Quotation Versioning**: older issued Quotations remain available and
  historically stable.
- **§28 Client Quotation Portal**: contractor sends a Quotation → system
  creates a scoped Quotation Access Grant. Client can view, accept, reject,
  request changes. "The Client does not gain general access to the
  contractor's application."
- **§30 Approval Model**: generic `{subjectType, subjectId, actorType,
  status}`, designed to generalize later.
- **§31 Client Quotation Acceptance**: Approval Record Created → Quotation
  Version Locked → Project Status Updated. Records accepted Quotation ID,
  version, timestamp, Client identity where known, Access Grant used.
- **§5 Project statuses**: exactly 8 values including `quotation_sent` and
  `quotation_approved` — no rejected/changes-requested status.
- **§50 Audit History**: `Quotation Sent`, `Client Viewed Quotation`,
  `Client Requested Changes`, `Client Accepted Quotation`. "Current State +
  Audit Events," not event sourcing.
- **§53 Idempotency**: "Quotation acceptance" explicitly requires idempotency.
- **§54 Optimistic Concurrency**: conditional update semantics.
- **§55 Collections**: `access_grants`, `approvals`, `audit_events`.
- **§24 Internal/external boundary**: Client must never see cost, margin,
  markup, or profit figures.

### 0.2 What the actual code already provides [code]

- `quotations.Quotation`: `ID, CompanyID, ProjectID, ClientID, EstimateID,
  QuotationNumber, Version, Status (draft|finalized), Revision, Currency,
  Lines[], Subtotal, GeneratedSubtotal, TaxMode, TaxLabel, TaxRateBPS,
  TaxAmount, Total, Terms, PaymentSchedule, ValidUntil, Notes, CreatedAt,
  FinalizedAt, SchemaVersion`. `QuotationLine`: `ID, SourceWorkItemIDs,
  Description, Quantity, Unit, UnitPrice, Amount, SortOrder`.
- `quotations.Service.GetQuotation(ctx, companyID, quotationID)` —
  company-scoped only.
- `access`, `approvals`, `audit`, `documents` are `doc.go`-only scaffolds.
- `projects.ProjectStatus` has exactly the 8 phase1.md values.
  `projects.Service.UpdateProjectStatus` exists with **no** concurrency
  guard (unconditional `repo.UpdateStatus`).
- `identity.GenerateRefreshToken()` / `HashRefreshToken()` — the exact
  precedent for M6's token: 32 bytes `crypto/rand`, base64url
  `RawURLEncoding`, SHA-256 hex hash lookup, never a JWT.
- No `platform/clock` or `platform/random` abstraction exists — modules call
  `time.Now()` and `crypto/rand` directly. M6 follows the same pattern.
- `identity.RequireAuthHuma` applied via `huma.NewGroup(api)`; routes on the
  base `api` skip auth entirely (like `/health`, `/auth/*`).
- `identity.Principal{UserID, CompanyID, Role}`.
- `companies.Company` has only `ID, Name, CreatedAt`.

---

## 1. M6 Scope

| Item | In scope |
|---|---|
| Access Grant creation, secure token generation | **Yes** |
| `AccessGroupState` coordinator (§2.5) | **Yes** |
| External Quotation viewing API (unauthenticated) | **Yes** |
| Accept / Reject / Request Changes API | **Yes** |
| Project status updates (monotonic) | **Yes** |
| Audit events | **Yes** |
| Link revocation, expiry, rotation | **Yes** |
| **Next.js Client portal page** | **Deferred** — **[addendum]** M6 is backend-only. The production Client-facing page, quotation layout, loading/error states, and frontend action forms are a separate frontend milestone after this API contract is implemented and verified. |
| Email delivery, PDF generation, notifications, rate limiting | **Deferred** |

M6 does **not**: modify `quotations.Quotation`'s schema/lifecycle; modify
M5 behavior beyond adding one narrow read capability; add `ProjectStatus`
values; build RFQ/Supplier access; design a Variation Order workflow;
introduce Mongo multi-document transactions.

---

## 2. Access Grant and Coordinator Model

**Ownership:** `internal/access` owns `access_grants` and
`access_group_states` exclusively (ADR 0002).

### 2.1 Resolved rules

| Question | Resolution |
|---|---|
| Grant target | Exactly one `quotations.Quotation.ID`. Never re-resolved. |
| Historical grants per resource | Multiple allowed (one per share/rotation). No per-resource unique index. |
| Active-grant authority | **[addendum]** The **coordinator** (`AccessGroupState.ActiveGrantID`) — not `AccessGrant.Status` alone — determines whether a grant is externally active. |
| Permissions | `["view","accept","reject","request_changes"]`. No `download`. |
| Expiry | Every grant has a concrete `ExpiresAt`. **Never-expiring grants are not supported.** |
| Revocation | `Status="revoked"`, `RevokedAt`, `RevokedReason` (`manual`\|`superseded`\|`rotated`) |
| Concurrency | `AccessGrant.Revision` guards direct single-grant mutation (revoke, expiry-edit). `AccessGroupState.Revision` guards all cross-grant coordination (share, supersession, rotation, decisions). |

### 2.2 Domain models

```go
package access

type AccessGrantStatus string

const (
    AccessGrantStatusActive  AccessGrantStatus = "active"
    AccessGrantStatusRevoked AccessGrantStatus = "revoked"
    // "expired" is never a stored status — derived at read time.
)

type AccessGrant struct {
    ID               string
    CompanyID        string
    ProjectID        string
    ResourceType     string   // "quotation"
    ResourceID       string   // == Quotation.ID (not unique in collection)
    ResourceGroupKey string   // "quotation:" + QuotationNumber
    QuotationNumber  string
    GranteeType      string   // "client"
    GranteeID        string   // == Quotation.ClientID
    Permissions      []string
    TokenHash        string   // SHA-256 hex
    Status           AccessGrantStatus
    Revision         int64
    CreatedByUserID  string
    CreatedAt        time.Time
    ExpiresAt        time.Time // always concrete
    RevokedAt        *time.Time
    RevokedReason    string
    SchemaVersion    int
}

// AccessGroupState is the single coordination point for one commercial
// chain. Both share/rotate and Client-decision paths must win a
// conditional write against this same document.
type AccessGroupState struct {
    ID               string
    CompanyID        string
    ResourceType     string
    ResourceGroupKey string

    CurrentResourceID  string  // QuotationID currently open for this chain
    ActiveGrantID      *string // nil if no grant currently active
    AcceptedResourceID *string // set once, ever — terminal chain fact

    Revision      int64
    CreatedAt     time.Time
    SchemaVersion int
}
```

### 2.3 [addendum A] External token liveness

A token is usable only when **all** hold:
1. An `AccessGrant` exists for the token hash
2. `AccessGrant.Status == active`
3. `AccessGrant.ExpiresAt` is in the future
4. `AccessGroupState.ActiveGrantID == AccessGrant.ID`
5. `AccessGroupState.CurrentResourceID == AccessGrant.ResourceID`

Failure of **any** condition returns identical `HTTP 410 Gone` with the same
generic body and no Quotation data. Conditions 4-5 mean an orphaned or
stale-but-stored-active grant is externally unusable.

### 2.4 [addendum B] Share / supersession / rotation write order

1. Generate candidate grant ID, raw token, token hash.
2. Insert the candidate `AccessGrant` document.
3. Conditionally update `AccessGroupState` to activate that candidate.
4. **Only after** the coordinator update succeeds: return raw token + URL;
   best-effort mark the previous grant revoked; write audit events; advance
   Project status.

The coordinator conditional update matches expected `CompanyID`,
`ResourceType`, `ResourceGroupKey`, `Revision`, `CurrentResourceID`,
`ActiveGrantID`, and the `AcceptedResourceID` condition:
- Normal share/supersession requires `AcceptedResourceID == nil`.
- Rotation is allowed when `AcceptedResourceID == nil` **OR**
  `AcceptedResourceID == candidate.ResourceID`.

**If the coordinator update loses the race:** never return the raw token or
URL; leave the candidate as an undisclosed orphan; re-read the coordinator;
return the correct idempotent `200` or conflict `409`; never treat the
candidate as externally active (guaranteed by §2.3 conditions 4-5).

### 2.5 Rotation branches

| Target grant state | Branch | Result |
|---|---|---|
| `active` | 1 | Insert candidate → coordinator claim (matching `ActiveGrantID == oldGrantID`) → on success, best-effort revoke old as `rotated`, return token |
| `revoked` + `RevokedReason == "manual"` | 2 | Insert candidate → coordinator claim (matching `ActiveGrantID == nil`) → return token. Never re-revokes the already-revoked source. |
| `revoked` + `RevokedReason ∈ {superseded, rotated}` | 3 | `409 ErrGrantSuperseded` — never rotatable |
| Any grant whose `ResourceID != coordinator.CurrentResourceID` | 3 | `409 ErrGrantSuperseded` |

Rotation of the accepted version's own grant is allowed (§2.4's
`AcceptedResourceID == candidate.ResourceID` clause) and never touches the
`Approval` record or `AcceptedResourceID`.

**Recovery rule:** if Branch 1's coordinator claim fails after the candidate
insert, the candidate is an undisclosed orphan and the old grant remains
active (it was never revoked — revocation happens only *after* a successful
claim, per §2.4 step 4). The contractor re-`GET`s `/quotations/{id}/share`
and acts on current state.

### 2.6 Direct grant mutations (revoke, expiry-edit)

Single-document conditional updates against `AccessGrant.Revision`:
- **Revoke**: `status="active" AND revision=expectedRevision` → set
  `revoked`/`manual`. Then best-effort coordinator update setting
  `ActiveGrantID = nil` (conditioned on `activeGrantId == thisGrantID`).
- **Expiry-edit**: requires grant `active` and unexpired; sets `ExpiresAt`,
  increments `Revision`. Never touches the coordinator.

---

## 3. Token Security

| Concern | Design |
|---|---|
| Generation | `crypto/rand`, 32 bytes; `base64.RawURLEncoding` |
| Storage | Only `TokenHash` = SHA-256 hex. Raw token never persisted. |
| Exposure | **Each raw token is returned exactly once**, at the moment its grant document is successfully activated by the coordinator (§2.4 step 4). Never returned again; never reconstructable from `TokenHash`. |
| Lookup | Unique index on `tokenHash` |
| Liveness | §2.3's five conditions, checked at read time |
| Uniform failure | Malformed / unknown / expired / revoked / rotated / superseded / orphaned → identical `410` |
| No pre-validation | The `token` path parameter has no format/length constraint, so malformed and unknown fail identically |
| Logging | Raw token categorically forbidden in application logs, audit metadata, and (as a deployment requirement) access logs |

### 3.1 Token-in-URL leakage mitigations (required)

1. Application request-log redaction for `/client/quotations/*`.
2. Reverse-proxy / hosting access-log redaction — **documented production
   deployment requirement**, not enforceable in Go code.
3. `Cache-Control: no-store` on every external response.
4. `Referrer-Policy: no-referrer` on every external response.
5. No third-party resources or outbound links in any future Client page.

---

## 4. API Boundary

Authenticated routes register on the existing `authedAPI :=
huma.NewGroup(api)`. External routes register directly on the base `api`.
No second `huma.API`.

### 4.1 Authenticated contractor endpoints

| Method | Path | Response | Key errors |
|---|---|---|---|
| `POST` | `/quotations/{id}/share` | `201` (new grant: `token`+`url`) or `200` (idempotent, neither) | `404`, `409 ErrQuotationNotFinalized`, `409 ErrQuotationExpiredForShare`, `409 ErrQuotationChainAlreadyAccepted`, `409 ErrQuotationSuperseded`, `409 ErrActiveGrantAlreadyExistsForGroup` |
| `POST` | `/access-grants/{grantId}/rotate` | `201` (`token`+`url`) | `404`, `409 ErrGrantRevisionMismatch`, `409 ErrGrantSuperseded`, `409 ErrActiveGrantAlreadyExistsForGroup` |
| `POST` | `/access-grants/{grantId}/revoke` | `200` (no token/url) | `404`, `409` |
| `PATCH` | `/access-grants/{grantId}/expiry` | `200` (no token/url) | `404`, `409` |
| `GET` | `/quotations/{id}/share` | `200` grant + decision summary | `404` |

### 4.2 Unauthenticated external endpoints

| Method | Path | Response | Key errors |
|---|---|---|---|
| `GET` | `/client/quotations/{token}` | `200 ClientQuotationDTO` | `410` |
| `POST` | `/client/quotations/{token}/accept` | `200` | `410`, `409`, `422` |
| `POST` | `/client/quotations/{token}/reject` | `200` | `410`, `409`, `422` |
| `POST` | `/client/quotations/{token}/request-changes` | `200` | `410`, `409`, `422` |

All external responses carry `Cache-Control: no-store` and
`Referrer-Policy: no-referrer`.

### 4.3 `url` semantics [addendum]

`url` is constructed from **`EXTERNAL_API_BASE_URL`** as
`{EXTERNAL_API_BASE_URL}/client/quotations/{rawToken}` — an **external API
URL for development and integration testing**. It is explicitly **not** a
finished Client portal link; the production Client-facing page is a
separate frontend milestone. `url` and `token` appear together only on the
two `201` responses (initial share, rotation) and never on any other
response.

---

## 5. External DTO Allowlist

Never serialize `quotations.Quotation`. Excluded: `EstimateID`,
`GeneratedSubtotal`, `Notes`, `Revision`, `SourceWorkItemIDs`, `CompanyID`,
`ProjectID`, `ClientID`, Quotation/line Mongo IDs, `SchemaVersion`,
`CreatedAt`, `FinalizedAt` **[addendum: remains excluded]**.

```go
type ClientQuotationDTO struct {
    QuotationNumber string                   `json:"quotationNumber"`
    Version         int                      `json:"version"`
    CompanyName     string                   `json:"companyName"`
    ProjectName     string                   `json:"projectName"`
    Currency        string                   `json:"currency"`
    Lines           []ClientQuotationLineDTO `json:"lines"`
    Subtotal        ClientMoneyDTO           `json:"subtotal"`
    TaxMode         string                   `json:"taxMode"`
    TaxLabel        string                   `json:"taxLabel,omitempty"`
    TaxRateBPS      int64                    `json:"taxRateBps,omitempty"`
    TaxAmount       ClientMoneyDTO           `json:"taxAmount"`
    Total           ClientMoneyDTO           `json:"total"`
    Terms           string                   `json:"terms,omitempty"`
    PaymentSchedule string                   `json:"paymentSchedule,omitempty"`
    ValidUntil      *string                  `json:"validUntil,omitempty"`
    IsExpired       bool                     `json:"isExpired"` // commercial expiry
    Status          string                   `json:"status"`    // always "finalized"
    Decision        *string                  `json:"decision,omitempty"` // current decision, if any
}

type ClientQuotationLineDTO struct {
    Description string          `json:"description"`
    Quantity    *string         `json:"quantity,omitempty"`
    Unit        *string         `json:"unit,omitempty"`
    UnitPrice   *ClientMoneyDTO `json:"unitPrice,omitempty"`
    Amount      ClientMoneyDTO  `json:"amount"`
}

type ClientMoneyDTO struct {
    Amount   int64  `json:"amount"`
    Currency string `json:"currency"`
}
```

Only `finalized` Quotations are exposed — enforced at share time and
re-checked at view time.

---

## 6. Share Semantics

### 6.1 Share decision table

| Coordinator state vs. requested `quotationID` | Result |
|---|---|
| No coordinator yet | Create coordinator + grant → `201` |
| `AcceptedResourceID` set (any version) | `409 ErrQuotationChainAlreadyAccepted` |
| `CurrentResourceID == requested`, `ActiveGrantID` → live grant | `200`, no `url`/`token` |
| `CurrentResourceID == requested`, `ActiveGrantID` nil or dead | Create + activate → `201` |
| `CurrentResourceID` is an older version | Supersede: activate new, then best-effort revoke old → `201` |
| `CurrentResourceID` is a newer version | `409 ErrQuotationSuperseded` |

### 6.2 [addendum] Initial grant expiry

- `Quotation.ValidUntil` present and in the future → `ExpiresAt = ValidUntil`
- `Quotation.ValidUntil` absent → `ExpiresAt = now + 30 days`
- `Quotation.ValidUntil` already past → **reject** with
  `409 ErrQuotationExpiredForShare`

### 6.3 `EXTERNAL_API_BASE_URL` configuration

Required, validated as a non-empty absolute URL at startup. Local default:
`http://localhost:8080`.

### 6.4 Audit events per operation

| Operation | `Quotation Sent` | `Access Created` | `Access Revoked` |
|---|---|---|---|
| First share for a chain | Yes | Yes | — |
| Idempotent repeat share | No | No | — |
| Rotate (Branch 1) | No | Yes | Yes (`rotated`) |
| Rotate (Branch 2, manual-revoked) | No | Yes | — |
| Supersession | Yes | Yes | Yes (`superseded`) |
| Manual revoke | No | No | Yes (`manual`) |

---

## 7. Client Decision Model

### 7.1 `approvals` domain model

```go
package approvals

type ApprovalStatus string

const (
    ApprovalStatusAccepted         ApprovalStatus = "accepted"
    ApprovalStatusRejected         ApprovalStatus = "rejected"
    ApprovalStatusChangesRequested ApprovalStatus = "changes_requested"
)

type Approval struct {
    ID              string
    CompanyID       string
    SubjectType     string
    SubjectID       string
    SubjectGroupKey string
    ActorType       string
    ActorName       string
    ActorEmail      string
    Status          ApprovalStatus
    Comment         string
    AccessGrantID   string
    Revision        int64
    DecidedAt       time.Time
    SchemaVersion   int
}
```

`approvals` is subject-agnostic and has no knowledge of chains or the
coordinator. All chain coordination is orchestrated by `access.Service`.

### 7.2 [addendum C] Acceptance write order

1. Validate token liveness (§2.3), grant security expiry, Quotation
   commercial validity (`ValidUntil`), and coordinator membership.
2. **Conditionally set** `AccessGroupState.AcceptedResourceID = quotationID`
   and increment coordinator `Revision`. The claim matches:
   `CurrentResourceID == quotationID`, `ActiveGrantID == grantID`,
   `AcceptedResourceID == nil`, expected `Revision`.
3. Upsert `Approval.Status = accepted`.
4. Advance `Project.Status` monotonically to `quotation_approved`.
5. Record the audit event.

**If another acceptance already set the same `AcceptedResourceID`:** treat
as idempotent; reconcile the `Approval` record if missing or stale;
return `200`.

**If a newer-version share won first** (claim fails because
`CurrentResourceID` moved): **do not write accepted Approval state**;
return `409 ErrQuotationSuperseded`.

**If the coordinator claim succeeds but the Approval write fails:**
`AcceptedResourceID` remains the authoritative terminal fact; a repeated
accept reconciles the `Approval` record before returning success.

There is **no compensation logic** — the coordinator is claimed *before*
any Approval write, so no backward transition is ever needed. An accepted
`Approval` never transitions back to `rejected`, `changes_requested`, or
any placeholder state.

### 7.3 [addendum D] Reject / request-changes fence

Before writing `rejected` or `changes_requested`, perform a conditional
coordinator `Revision` increment requiring: `CurrentResourceID ==
quotationID`, `ActiveGrantID == grantID`, `AcceptedResourceID == nil`,
expected `Revision`. Only after this fence succeeds is the `Approval`
decision written. This serializes non-terminal decisions against concurrent
supersession and acceptance.

### 7.4 Approval transition rules

1. No existing document → insert (`Revision` 0).
2. Current `rejected`/`changes_requested` → Revision-guarded conditional
   update to any status.
3. Current `accepted` → repeat accept is idempotent (`200`, no write);
   reject/request-changes → `409 ErrDecisionAlreadyAccepted`.
4. First-decision race → `uq_approvals_subject` collision → re-read, apply
   rule 2/3 once.

### 7.5 [addendum] Client input validation

| Field | Rule |
|---|---|
| `comment` on accept | Optional |
| `comment` on reject | **Required**, trimmed non-blank |
| `comment` on request-changes | **Required**, trimmed non-blank |
| `ClientName` | Max 200 characters |
| `ClientEmail` | Max 320 characters; syntactically validated when supplied; always treated as unverified client-provided information |
| `Comment` | Max 2,000 characters |

---

## 8. Project Status Transitions

Monotonic, via conditional current-status compare-and-set. No
`Project.Revision` field is introduced **[addendum]**.

**`AdvanceProjectToQuotationSent`**: `lead`/`site_visit`/`estimating` →
`quotation_sent`; all others no-op.

**`AdvanceProjectToQuotationApproved`**: `lead`/`site_visit`/`estimating`/
`quotation_sent` → `quotation_approved`; all others no-op.

**Reject / request-changes**: no Project status change.

Project status updates are best-effort secondary writes, retried on every
subsequent idempotent repeat call. No client-visible partial-success field.

---

## 9. Version Safety

- Coordinator `AcceptedResourceID` is the sole chain-level terminal
  authority.
- Once set, no other version may be shared (`409
  ErrQuotationChainAlreadyAccepted`).
- Superseded versions cannot be re-shared (`409 ErrQuotationSuperseded`).
- Superseded grants fail token liveness (§2.3 conditions 4-5) → `410`.
- Rotating the accepted version's own grant is allowed; rotating a
  sibling/superseded grant is `409`.
- Each `QuotationNumber` chain has its own independent coordinator.

---

## 10. ValidUntil vs. Grant Expiry

`Quotation.ValidUntil` (immutable once finalized) and `AccessGrant.ExpiresAt`
(contractor-editable while active and unexpired) are independent. Grant
liveness is checked first; commercial validity second. An expired Quotation
remains **viewable** read-only through a live grant (`IsExpired: true`) but
**no decision may be submitted** (`422 ErrQuotationExpiredForDecision`).

---

## 11. Concurrency Summary

Every cross-grant/cross-decision race resolves through a single conditional
write against the one `AccessGroupState` document: share-vs-share,
share-vs-accept, accept-vs-accept, rotate-vs-anything,
reject-vs-supersession. The loser receives `409` and must re-read.

Duplicate-key classification mirrors M4/M5's `classifyCreateError` pattern:
`uq_access_grants_token_hash` → `ErrUnclassifiedDuplicateKey` (500);
`uq_access_group_states_company_group` → internal re-read (not
caller-visible); `uq_approvals_subject` → single non-looping retry;
unclassified → `ErrUnclassifiedDuplicateKey` (500).

---

## 12. Audit

```go
package audit

type AuditRecorder interface {
    RecordQuotationSent(ctx context.Context, companyID, projectID, actorUserID, quotationID, quotationNumber string, version int) error
    RecordAccessCreated(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber string, version int) error
    RecordAccessRevoked(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber, revokedReason string, version int) error
    RecordClientViewedQuotation(ctx context.Context, companyID, projectID, grantID, quotationID, quotationNumber string, version int) error
    RecordClientDecision(ctx context.Context, companyID, projectID, grantID, approvalID, quotationID, quotationNumber string, version int, decisionStatus, clientName, clientEmail string) error
}

type Event struct {
    ID, CompanyID, ProjectID, EventType   string
    SubjectType, SubjectID                string
    ActorType, ActorID                    string
    Metadata                              map[string]any // built internally only
    CreatedAt                             time.Time
    SchemaVersion                         int
}
```

`AuditRecorder` is defined in and owned by `audit`; `audit.Service`
satisfies it directly with no adapter. Raw tokens, whole domain structs,
cost/margin values, and **Client comments [addendum]** are all structurally
absent from every method signature. Audit writes are synchronous
best-effort — failure never fails the request.

Events fire per §6.4 and on every Client view and every decision-changing
transition.

---

## 13. Collections and Indexes

| Collection | Owner |
|---|---|
| `access_grants` | `internal/access` |
| `access_group_states` | `internal/access` |
| `approvals` | `internal/approvals` |
| `audit_events` | `internal/audit` |

| Index | Keys | Type |
|---|---|---|
| `idx_access_grants_company_project` | `{companyId:1, projectId:1}` | plain |
| `uq_access_grants_token_hash` | `{tokenHash:1}` | unique |
| `idx_access_grants_company_resource_created_at` | `{companyId:1, resourceType:1, resourceId:1, createdAt:-1}` | plain |
| `uq_access_group_states_company_group` | `{companyId:1, resourceType:1, resourceGroupKey:1}` | unique |
| `uq_approvals_subject` | `{companyId:1, subjectType:1, subjectId:1}` | unique |
| `idx_audit_events_company_project_createdAt` | `{companyId:1, projectId:1, createdAt:-1}` | plain |
| `idx_audit_events_company_subject` | `{companyId:1, subjectType:1, subjectId:1, createdAt:-1}` | plain |

**[addendum E] Removed:** `uq_approvals_accepted_per_subject` (redundant
with `uq_approvals_subject`; never enforced chain terminality) and
`DecisionRecorder.HasAcceptedDecisionForGroup` (the coordinator's
`AcceptedResourceID` is the sole authority).

---

## 14. Module Boundaries

`access.Service` consumes narrow capability interfaces; providers are
adapted at the composition root where return types differ.

```go
// Defined in access — satisfied via quotationSourceAdapter in cmd/api
type QuotationSource interface {
    GetFinalizedQuotationForShare(ctx context.Context, companyID, quotationID string) (ShareableQuotationSnapshot, bool, error)
}

type ShareableQuotationSnapshot struct {
    QuotationID, CompanyID, ProjectID, ClientID string
    QuotationNumber                             string
    Version                                     int
    Status, Currency                            string
    Lines                                       []ShareableQuotationLine
    Subtotal                                    money.Money
    TaxMode, TaxLabel                           string
    TaxRateBPS                                  money.RateBPS
    TaxAmount, Total                            money.Money
    Terms, PaymentSchedule                      string
    ValidUntil                                  *time.Time
}

type ShareableQuotationLine struct {
    Description string
    Quantity    *decimal.Decimal
    Unit        *string
    UnitPrice   *money.Money
    Amount      money.Money
}

// Primitive-only — satisfied structurally or via a thin adapter
type ProjectStatusUpdater interface {
    AdvanceProjectToQuotationSent(ctx context.Context, companyID, projectID string) (changed bool, found bool, err error)
    AdvanceProjectToQuotationApproved(ctx context.Context, companyID, projectID string) (changed bool, found bool, err error)
    GetProjectName(ctx context.Context, companyID, projectID string) (name string, found bool, err error)
}

// Primitive-only — satisfied via approvalsAdapter in cmd/api
type DecisionRecorder interface {
    RecordDecision(ctx context.Context, companyID, subjectType, subjectID, subjectGroupKey, actorName, actorEmail, comment, status, accessGrantID string) (approvalID string, decidedAt time.Time, isNewDecision bool, err error)
    GetDecision(ctx context.Context, companyID, subjectType, subjectID string) (status, comment, actorName, actorEmail string, decidedAt time.Time, found bool, err error)
}

type CompanyNameLookup interface {
    GetCompanyName(ctx context.Context, companyID string) (string, error)
}
```

Composition-root adapters (in `cmd/api/main.go` and
`internal/tenanttest/router.go`) import both provider and consumer packages
and perform explicit conversion — the only place this is permitted, since
the composition root is the leaf of the dependency graph.

---

## 15. Error Model

| Sentinel | HTTP |
|---|---|
| `ErrQuotationNotFound`, `ErrGrantNotFound` | `404` |
| `ErrQuotationNotFinalized` | `409` |
| `ErrQuotationExpiredForShare` | `409` |
| `ErrQuotationChainAlreadyAccepted` | `409` |
| `ErrQuotationSuperseded` | `409` |
| `ErrActiveGrantAlreadyExistsForGroup` | `409` |
| `ErrGrantRevisionMismatch`, `ErrGrantNotActive`, `ErrGrantSuperseded` | `409` |
| `ErrDecisionAlreadyAccepted` | `409` |
| `ErrExternalTokenUnusable` | **`410`** (uniform, all external failures) |
| `ErrQuotationExpiredForDecision` | `422` |
| `ErrCommentRequired`, `ErrFieldTooLong`, `ErrInvalidEmail` | `422` |
| `ErrUnclassifiedDuplicateKey` | `500` |

---

## 16. Test Coverage Requirements

Initial share creates coordinator + grant; repeat share returns `200`
without token/url; V2 supersedes V1; V1 cannot be re-shared; accepted chain
cannot share another version; rotation of accepted version's own grant
allowed; rotation of sibling/superseded rejected; manual-revoked grant can
receive a replacement; malformed/unknown/expired/revoked/rotated/orphaned/
superseded tokens return identical `410`; stored-active grant not
referenced by coordinator is unusable; candidate from a lost race is never
disclosed; share-vs-accept has one winner; accept-vs-accept idempotent;
reject/request-changes cannot land after acceptance or after concurrent
supersession; coordinator claim precedes Approval write; repeated
acceptance reconciles missing/stale Approval; Project status never moves
backward; expired Quotation viewable but not actionable; sharing a
commercially expired Quotation rejected; correct default expiry selected;
reject/request-changes require comments; field-length and email validation;
external DTO excludes every internal field; raw tokens absent from logs and
audit metadata; external security headers present; tenant isolation across
grants, coordinators, approvals, audit events.

Integration tests use real MongoDB conditional writes (Testcontainers)
where practical, not only mocked repositories.

---

## 17. Amendment Log

- **Round 1**: initial design.
- **Round 2** (six corrections): rotation/index model; Approval as a single
  mutable aggregate; accepted-chain terminality; Project status
  monotonicity; exact tenant-safe module contracts; API contract fixes.
- **Round 3** (seven corrections): `AccessGroupState` coordinator closing
  the cross-collection accept-vs-share race; three rotation branches;
  rotate-vs-accepted distinction; complete share state machine; unified
  url/token exposure rule; composition-root adapters for Go
  satisfiability; `actorUserID` on audit methods.
- **Final addendum (this round)**: coordinator-authoritative token liveness
  (§2.3); explicit share/rotate write order with candidate-insert-then-claim
  (§2.4); acceptance claim-before-Approval-write with no compensation
  (§7.2); reject/request-changes coordinator fence (§7.3); removal of
  `uq_approvals_accepted_per_subject`, `HasAcceptedDecisionForGroup`, and
  compensation logic (§13); product decisions — **backend-only (Next.js
  portal deferred)**, grant expiry defaults, comment requirements and field
  limits, `FinalizedAt` excluded, comments excluded from audit, no
  `Project.Revision`; `url` built from `EXTERNAL_API_BASE_URL` and
  explicitly not a finished portal link (§4.3).

**All open decisions are resolved. No further design review is required.**

---

*End of approved specification.*
