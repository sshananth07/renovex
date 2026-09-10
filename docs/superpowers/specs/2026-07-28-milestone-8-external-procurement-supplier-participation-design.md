# Milestone 8 — External Procurement and Supplier Participation: Design Specification

**Status:** Approved — Revision 21; proposed Phase H acceptance amendment pending review  
**Date:** 2026-07-28  
**Product authority:** `phase1.md`  
**Upstream implementation authority:** `docs/superpowers/specs/2026-07-26-milestone-7-procurement-foundation-design.md`, approved Revision 3 plus the approved Revision 4 `ReadClaimSnapshot` amendment  
**Scope:** Immutable RFQ issuance, Supplier Invitations, verified restricted Supplier access, Supplier Offer drafts and immutable versions, transparent comparison, provisional line selections, immutable award revisions, explicit outcome notifications, and receipt acknowledgement.  
**Implementation status:** Phases A–G are implemented and verified. The proposed
Phase H amendment below closes composed acceptance, deployment readiness and
Milestone 8 records only. It does not reopen the settled commercial or recovery
contracts.

This document is the implementation source of truth for Milestone 8 after final repository/code investigation confirms the referenced M7 capability shapes and established conventions. It records the design decisions approved during brainstorming. It must not be expanded to include Purchase Orders, committed-cost updates, invoices, payment processing, fulfilment, or Supplier account creation.

---

## Revision log

| Revision | Date | Change |
|---|---|---|
| 22 (proposed) | 2026-08-03 | Phase H acceptance and closure contract. Reconciles the implemented A–G evidence against §19H, identifies only remaining composed and deployment evidence, defines the H0–H8 inline sequence, and records two RED closure gaps: `/ready` does not yet prove withdrawal-transaction capability, and CI/production replica-set requirements are not yet explicit. The golden journey includes successful explicit notification of selected and unsuccessful outcomes. Missing acceptance evidence may pass immediately; RED is required for newly introduced behavior and genuine defects, never manufactured artificially. Adds no workflow, credential, financial semantic, recovery owner or domain module. |
| 21 | 2026-08-02 | Approved Phase G scope reconciliation and design hardening. Replaces the legacy three-item Phase G scope with §19A–§19K: removes work completed by Phases C–F; retains only deferred Supplier invitation/RFQ and own-offer projections; inventories module-owned recovery; defines external HTTP privacy, bounded validation, logging/audit/mail allowlists, error non-disclosure, retention visibility, the M8 validation/exposure matrix, composition/OpenAPI verification, and the inline G0–G9 TDD sequence. Records the observed production/`tenanttest` Award wiring drift and missing Phase E/F concrete audit composition as cross-cutting gaps, without duplicating the underlying Phase F behavior. The technical review also fixes the pre-existing superseded-withdrawal conformance gap through one shared domain evaluator and replaces the undiscoverable chain-ID reconciliation locator with a tenant-derived Invitation + Issued-RFQ-Version tuple. The G5 clarification requires two explicit real-Mongo withdrawal/submission orderings and forbids treating a stale chain read followed by an unrelated eligibility CAS as a linearization boundary; the inspected repositories expose no shared primitive today, so G5 must pause after RED unless a defensible design is separately reviewed. Approval makes the §19D values binding for Phase G and settles M8 retention scope as no physical deletion, purge, or new/modified TTL enforcement; any future physical-retention policy requires a separate approved revision. |
| 1 | 2026-07-28 | Initial approved design. |
| 2 | 2026-07-28 | Repository-investigation gate completed. Adds §1A (Revision 2 decisions A–D): route-level role authorization helper in `identity`; synchronous mail after persisted delivery intent; setter-based composition wiring for the M7↔M8 issuance cycle; stable invitations retained plus copy-link, view tracking and per-attempt delivery channel. Replaces §6.1's random invitation secret with a versioned HMAC keyring derivation (§6.1A). Adds `SecretKeyVersion`, `FirstViewedAt`, `LastViewedAt`, `DeliveryChannel`. |
| 3 | 2026-07-28 | Phase B implementation discovery: the M7 handoff projection omitted the identifiers `IssuedRFQLine` requires. Widens `rfqs.RFQLineSnapshot` with `SourceMaterialRequirementID` and `MaterialID`, adds `title` to `GetReadyRFQSnapshot` (§2.1A). Establishes that a ready M7 RFQ without a `ResponseDeadline` cannot be issued (422, §4.1A). Replaces §3.2's `IssuedRFQLine` with per-version `ID` plus stable `LineageID` and optional M7 provenance, and defines the M8-native-line copy-forward exception (§3.2A). |
| 4 | 2026-07-28 | Phase B implementation decisions approved in chat: Phase 1 RFQ issuance supports MYR only and normalizes its spelling; amendment PATCH replaces the complete optional line collection while the server preserves existing identity/provenance and generates identity for new M8-native lines; immutable-version insertion under the named company/chain/version unique index selects the one winner before the chain pointer advances, avoiding pre-increment reservation holes. |
| 5 | 2026-07-29 | Phase C reactivation contract approved in chat: revocation is terminal for the current access generation, not the stable invitation identity. An explicit owner/admin operation may reactivate a revoked or effectively expired invitation only by atomically minting a new generation under the active key, setting a future expiry, clearing `RevokedAt`, catching up to the latest issued RFQ version, and recording operation-ID idempotency. Reactivation creates and sends no delivery; only older pending attempts become obsolete. |
| 6 | 2026-07-29 | Phase D opaque-link resolution approved in chat: `AccessSecretHash` is globally unique and is the only public invitation lookup key; `supplieraccess` declares a narrow consumer-owned resolver capability implemented by an `rfqissuance` composition adapter; all invalid credential and invitation states collapse to one neutral public failure while infrastructure failures remain bounded operational failures. Rotation and reactivation replace the indexed hash, so old generations become unresolvable without retaining hash history. |
| 7 | 2026-07-29 | Phase D generation-bound authorization approved in chat: identity-wide Supplier sessions gain invitation-scoped `SupplierSessionInvitationBinding` records. Every request validates the session, current invitation identity/state, and exact binding generation. Generation changes fail closed without reverse module coupling; only fresh verification may conditionally advance a binding. |
| 8 | 2026-07-29 | Phase D browser exchange approved from the attached request: a clicked invitation-token URL is exchanged once for a 10-minute, single-use `SupplierAccessExchange`; only its random token hash and narrow validated context are persisted. An HttpOnly path-restricted cookie carries the raw exchange token through a 303 redirect to a clean URL. Challenge creation revalidates the invitation, consumes the exchange idempotently, persists delivery intent, and never turns the exchange into Supplier authorization. |
| 9 | 2026-07-29 | Phase D verification-code recovery approved in chat and attached request: exactly six ASCII digits are deterministically and without modulo bias derived from a dedicated versioned HMAC keyring; only a domain-separated keyed verifier and key version are persisted. Challenges last 10 minutes, permit five failed confirmations, and are throttled. Immutable delivery attempts provide operation-idempotent synchronous send, explicit retry, and crash recovery without changing the code, expiry, or attempt budget. |
| 10 | 2026-07-29 | Phase D client-address throttling approved in chat: challenge creation has an independent durable 20-per-rolling-hour limit per normalized client IP. Only a dedicated keyed HMAC of the IP is stored. The direct peer is authoritative unless it belongs to an explicitly configured trusted-proxy CIDR; rate-scope claims are sequential and fail closed without transactions. |
| 11 | 2026-07-29 | Phase D atomic throttling approved from the attached request: bounded per-scope `VerificationRateLimitState` aggregates use revision-guarded CAS, named uniqueness, idempotent exchange reservations, explicit rolling-window pruning and housekeeping TTL. Client-address scope is claimed before identity scope so an already-abusive address cannot consume a victim invitation's quota. |
| 12 | 2026-07-29 | Final Phase D contract approved in chat: verification atomically records its recovery target; same-browser matching sessions rotate while other devices create separate sessions; deterministic generation-bound session and CSRF credentials are hash-only; concurrent re-verification converges without extra rotation; protected activity renews both server expiry and browser cookie; logout is CSRF-protected and idempotent; challenge IDs stay in bodies; Phase D owns reusable authorization while Supplier-visible RFQ projections defer to Phase G. |
| 13 | 2026-07-30 | Dedicated verification resend throttling approved in chat during D5: explicit resends use a separate challenge-scoped HMAC fingerprint and CAS-backed `challenge_resend` rate aggregate. Distinct resends require a 60-second cooldown and permit five reservations per rolling hour; the same operation ID is idempotent. Challenge creation and its initial delivery do not consume resend allowance. |
| 14 | 2026-07-30 | Consolidated D8 HTTP and audit clarification approved in chat: verification returns one identifier-free `200` body and sets session/CSRF cookies only after complete recovery; CSRF-valid logout always returns an empty `204` and clears both cookies while `403` clears nothing; all six Phase D routes use the same no-store/no-referrer headers. Supplier-access audit events use primitive-only signatures, authoritative invitation/session subjects and Supplier actors, bounded failure codes, and emit only for actual writes—never for unresolved credentials, recovery, concurrency adoption or idempotent replay. |
| 15 | 2026-07-31 | Consolidated Phase E contract approved in chat: normalized-email offer ownership; strict discriminated tax, non-overlapping conditional charges and one fixed delivery charge; explicit immutable-version copy-forward review gates; active-draft CAS submission and recovery; and a separate mutable per-version eligibility aggregate that serializes withdrawal against final award publication without mutating an Offer Version. |
| 20 | 2026-08-01 | Second Phase F review pass. **§8G:** a correction cannot rerun ordinary F3 validation over the whole award — a baseline Offer Version legitimately has a terminal `awarded` gate, may no longer be `LatestSubmittedID`, and may be expired, so revalidating it would reject valid monotonic corrections. A correction is now a **locked baseline** (copied exactly from the current authoritative revision, never revalidated or re-claimed) plus a **correction delta** (fully validated and claimed now), computed by `CalculateCorrection` rather than the initial-award path. Adding a line from a baseline Offer Version uses a **cumulative** recalculation minus the frozen baseline contribution, so delivery is charged once and conditional/percentage groups re-trigger correctly; a cumulative contribution below the baseline is refused `422 award_correction_not_monotonic`. `offer_level` versions cannot reach that path because D1 required complete selection. **§8F/§8K:** audit is **ensured, not skipped** — a retry adopting an existing revision records a missing event under the deterministic identity `CompanyID + EventType + AwardRevisionID + FinalisationOperationID` and no-ops when present, so a crash between insert and audit can never leave an authoritative award permanently unaudited; the same rule governs correction, outcome, notification and acknowledgement events. **§8C/§8F/§8K:** employee permissions stated explicitly per Decision A — owner, admin and employee create, read, edit and discard provisional award drafts and read award records; owner and admin alone finalise, correct, notify and reconcile. |
| 19 | 2026-08-01 | Phase F review corrections. **§8G:** Award corrections are **commercially monotonic** — a correction may add previously unawarded eligible lineages and amend non-commercial metadata or unawarded reasons, but may never remove, reduce or reassign an existing award, nor turn a selected Supplier into unsuccessful. Awarded Offer eligibility gates and awarded RFQ-chain lineage claims are **terminal and never released**, correcting a Revision 18 line that contradicted D2 and would have permitted a published, already-notified award to be silently reassigned. Award cancellation and rescission require their own workflow and are **out of scope for M8**. Violations return `422 award_correction_not_monotonic`. **§8E:** distinguishes releasable in-flight claims from terminal published claims. **§8A.1A:** each checkpoint delivers its own HTTP surface with its own authorization, isolation and audit tests; F10 owns reconciliation only and verifies the composed whole. |
| 18 | 2026-08-01 | Phase F design hardening consolidated into §8A–§8K, which takes precedence over §8, §9 and §10.6. Four decisions approved in chat: (D1) `offer_level` tax is all-or-nothing per Offer Version — a contractor selects every positively quoted line or none, and Phase F never apportions or omits a Supplier-quoted whole-offer tax figure; (D2) the immutable Award Revision insert is the authoritative publication point, with chain advance, gate completion, outcomes, audit and notification as recoverable post-publication work, plus a durable `draft`/`finalising`/`published` chain state and a bounded `503 award_finalisation_pending` for crash-window reads; (D3) Supplier outcomes reuse the Phase D invitation session, scoped to Supplier + Invitation rather than the submitting recipient, with no second credential and no reactivation; (D4) each Issued RFQ Version owns an independent Award chain, and duplicate awards are prevented by a new RFQ-chain-scoped line claim keyed on `CompanyID + RFQChainID + StableLineageID`. Adds the `internal/awards` module boundary, the two distinct serialization points, and F1–F10 specifications. |
| 17 | 2026-07-31 | E4 no-draft race semantics clarified in chat (§5.3A): the concurrent guarantee is state-based, not call-ordering-based. Creation winning the insert and the existing-draft CAS then claiming that draft is a correct outcome — one workspace, claimed — because the previous recipient retains no usable workspace either way. A successful claim leaving an `active` workspace, or a second workspace row, remains forbidden. |
| 16 | 2026-07-31 | E4 recipient-replacement ordering approved in chat (§5.3A). Precondition-first ordering is confirmed, but direct archival and an unguarded "no draft is a successful no-op" are rejected as insufficient. Replacement now takes a durable `recipient_replacement_claimed` barrier that keeps occupying the unfinished-draft uniqueness slot, then performs one atomic authoritative invitation replacement, then archives. The no-draft case inserts a barrier-only claimed row contending on the same named unique index as ordinary draft creation, never a separate count-then-insert. Infrastructure failure after the claim returns a bounded `503 recipient_replacement_pending` and preserves the claim; claims never auto-release on timeout and only the owning operation ID may resume or abort. |

---

## 1A. Revision 2 decisions

These decisions were approved after the §18 repository-investigation gate. Where they conflict with an earlier section, **these take precedence** and the affected section is annotated inline.

### 1A.1 Decision A — route-level role authorization

The repository investigation established that **no role-based authorization exists on any M2–M7 resource route**. The only role check in the codebase is `companies.Service` gating member invitation. M8 therefore establishes the first shared route-level role convention.

A narrow role-authorization helper is added to `internal/identity`, alongside the existing principal and authentication helpers. This is an approved narrow addition to the otherwise frozen identity module and is the convention for M9 onward.

The §13 permission matrix applies as designed:

- **Owner and Admin** may issue RFQ versions, manage external access (send, resend, copy-link, replace recipient, rotate secret, revoke, reactivate, change expiry), finalise and revise awards, send outcome notifications, and run state-changing reconciliation.
- **Employee** may view every record and prepare editable drafts (amendment drafts, invitation drafts, provisional award drafts), but may not perform externally visible or irreversible transitions.

Implementation requirements:

1. Authorization occurs at the HTTP boundary, **before** invoking the privileged service operation.
2. Foreign-tenant access remains **404**. Valid tenant with insufficient role is **403**.
3. Tests cover owner, admin and employee for each privileged action category.
4. M2–M7 role checks are **not** retrofitted during M8.
5. M8 services remain role-agnostic unless an operation becomes reachable through another untrusted entry point. The authenticated HTTP boundary is the current authorization boundary.

### 1A.2 Decision B — synchronous mail after persisted delivery intent

`platform/jobs.InProcessQueue` discards handler errors and loses jobs on restart, so it cannot drive the delivery-status model. `platform/jobs` is **not** extended and **no** M8-owned background dispatcher is created. M8 follows the existing synchronous-mail convention.

Send sequence:

```text
persist delivery attempt as pending
→ mail.EmailSender.Send (synchronous)
→ conditional update pending → sent, or pending → failed
→ return the delivery result
```

Rules:

- The invitation, award or notification record is **never** rolled back because mail failed.
- A mail failure returns the appropriate **503**; the persisted `failed` attempt remains visible and retryable.
- A crash after persisting `pending` is recovered by reconciliation.
- Retrying with the same operation ID reuses the same logical attempt when its identity matches.
- `delivered` remains a reserved provider-dependent state and is not expected to occur with local Mailpit.
- Persist a **bounded `FailureCode`** only. Unrestricted provider error text and credentials are never persisted.

### 1A.3 Decision C — setter-based composition wiring

`rfqissuance` consumes the M7 ready-RFQ snapshot while `rfqs` consumes `RFQChainIssued`, which is a genuine construction cycle. `rfqs.Service` already exposes `SetIssuanceStatusSource` for exactly this purpose.

Composition order:

1. Construct `rfqs.Service` with `rfqs.NoExternalIssuanceSource{}`.
2. Construct `rfqissuance.Service` with a composition adapter over `rfqs.Service`.
3. Construct an `IssuanceStatusSource` adapter over `rfqissuance.Service`.
4. Call `rfqsService.SetIssuanceStatusSource(adapter)`.

The composition import-boundary tests are extended to permit the new M8 adapter file. **No M7 production capability addition is required.**

### 1A.4 Decision D — stable invitations, copy-link, view tracking, delivery channel

The stable invitation model of §3.4 is retained. M8 does **not** revert to `phase1.md` §33.2's one-invitation-per-RFQ model.

Three `phase1.md` Phase 1 behaviours are added:

**Copy-link / manual WhatsApp delivery.** A privileged contractor action returns the secure link for the invitation's **current** access generation. It does not create another invitation, does not send email, and does **not** rotate the secret. Secret rotation remains a separate explicit action. The raw secret appears in the response only and is never persisted, logged or audited. The contractor may paste the link into WhatsApp or any other channel; a direct WhatsApp API integration remains out of scope. An audit event records the copy-link action but never the raw link or token.

**View tracking.** The invitation lifecycle stays `draft / active / expired / revoked` — `viewed` is **not** a lifecycle status. Instead the invitation carries `FirstViewedAt` and `LastViewedAt`, updated only after a secure invitation link is successfully opened or exchanged. Email verification is **not** required for a secure invitation open to count. Failed, expired, revoked or obsolete-generation attempts must not update either timestamp. See §6.5.

**Delivery channel.** The channel is tracked per delivery action rather than fixed on the invitation: `email` or `copy_link`. Manual WhatsApp sharing is represented as `copy_link`; M8 never claims the system itself sent a WhatsApp message.

### 1A.5 Decision E — opaque invitation-token resolution

Supplier invitation links remain opaque token-only links. They expose no
`CompanyID`, `SupplierID`, `InvitationID`, or access generation. Phase D resolves
the public credential by the canonical hex-encoded SHA-256
`AccessSecretHash`, following the existing M6 external-access pattern.

`supplier_invitations.accessSecretHash` is globally unique because the public
open flow begins without a trusted tenant or invitation identifier. The unique
index is partial for transitional documents: it covers non-empty string values,
while the invitation write path continues to require the existing canonical
64-character lowercase hexadecimal representation. Missing or empty legacy
values therefore do not conflict with one another.

`supplieraccess` owns this narrow capability:

```go
type InvitationAccessResolver interface {
    ResolveInvitationAccess(
        ctx context.Context,
        accessSecretHash string,
        accessedAt time.Time,
    ) (InvitationAccessSnapshot, bool, error)
}

type InvitationAccessSnapshot struct {
    CompanyID                 string
    SupplierID                string
    InvitationID              string
    NormalizedRecipientEmail  string
    AccessGeneration          int64
    CurrentIssuedRFQVersionID string
}
```

`rfqissuance.Service` satisfies it through a composition adapter. No module
reads another module's collection. The returned snapshot deliberately omits the
stored hash, raw secret, contractor notes, delivery history, and all unrelated
Supplier or invitation data.

The public open sequence is:

1. validate the token's basic encoding and bounded length;
2. compute its SHA-256 hash in memory;
3. pass only the hash to `InvitationAccessResolver`;
4. globally load by `AccessSecretHash`;
5. constant-time compare the supplied hash with the stored hash after loading;
6. require an active, unexpired invitation with valid Supplier and recipient
   identity;
7. return only the narrow access snapshot;
8. record the successful view with the exact validated access generation;
9. continue into verification/session exchange; and
10. remove the raw token from the browser-visible URL.

The raw token is never passed to `rfqissuance`, persisted, logged, or audited.
Malformed tokens are rejected before MongoDB. Unknown, obsolete, rotated,
reactivated, revoked, expired, missing-Supplier, replaced-recipient, and
constant-time-comparison failures all return the same neutral public credential
failure. A real MongoDB or platform failure remains a bounded operational
failure (503) so an outage is not misrepresented as a bad credential, but its
response contains no internal details.

Rotation and reactivation replace the indexed hash:

```text
new AccessGeneration
→ new derived token
→ new AccessSecretHash replaces old hash
→ old token no longer resolves
```

Historical hashes and fallback lookups are forbidden. An unexpected unique-index
collision fails closed as a configuration, canonical-input, or implementation
defect; the server neither retries with altered inputs nor discloses the
conflicting invitation.

### 1A.6 Decision F — generation-bound Supplier session authorization

A Supplier session proves only this immutable identity:

```text
CompanyID + SupplierID + NormalizedRecipientEmail
```

It does not grant access to every invitation belonging to the Supplier.
`supplieraccess` owns one invitation-scoped permission record:

```go
type SupplierSessionInvitationBinding struct {
    ID                       string
    CompanyID                string
    SupplierSessionID        string
    SupplierID               string
    NormalizedRecipientEmail string
    InvitationID             string
    AccessGeneration         int64
    BoundAt                  time.Time
    LastValidatedAt          time.Time
    Revision                 int64
}
```

The binding grants permission for exactly:

```text
InvitationID + AccessGeneration
```

Every Supplier invitation or RFQ request validates three layers:

1. the Supplier session is active and within both expiry limits;
2. its Company, Supplier and normalized recipient email equal the current
   invitation identity; and
3. the session/invitation binding exists at the invitation's exact current
   `AccessGeneration`.

The current invitation snapshot comes through a narrow consumer-owned
capability implemented by `rfqissuance`; `supplieraccess` never reads the
invitation repository. The invitation must still be active and unexpired.

`rfqissuance` does not call back into `supplieraccess` when a generation
changes. Rotation, recipient replacement, or reactivation changes generation
`N → N+1`; a binding left at `N` fails the authoritative request-time comparison
immediately. Cleanup is reconciliation, not the security boundary. The same
session remains valid for unrelated invitations whose identity and generation
bindings still match.

A binding never advances automatically. Rebinding to `N+1` requires fresh
verification of a challenge tied to that exact invitation generation. After
successful verification, `supplieraccess` may conditionally create or update
the binding only after confirming that:

- the session identity still matches the current invitation;
- the invitation remains active and unexpired;
- the consumed challenge names that exact current generation; and
- an existing binding's `Revision` has not concurrently changed.

Challenge consumption and invitation rotation span module-owned collections and
are not transactional. The flow therefore consumes the challenge atomically,
re-reads the current invitation identity and generation, creates or conditionally
updates the binding, then revalidates before returning access. A concurrent
rotation may leave a stale binding document but can never grant access because
this generation comparison repeats on every request.

### 1A.7 Decision G — short-lived browser access exchange

Phase D separates three credentials:

```text
Invitation token
→ proves possession of the invitation link once

Access exchange
→ carries validated invitation context into challenge creation temporarily

Supplier session
→ authenticates the verified Supplier identity afterward
```

An access exchange is neither a temporary Supplier session nor permission to
list invitations, view an issued RFQ, or create an offer. `supplieraccess` owns:

```go
type SupplierAccessExchange struct {
    ID                       string
    ExchangeTokenHash        string
    CompanyID                string
    SupplierID               string
    InvitationID             string
    AccessGeneration         int64
    NormalizedRecipientEmail string
    CreatedAt                time.Time
    ExpiresAt                time.Time
    ConsumedAt               *time.Time
    ChallengeID              *string
}
```

The exchange token is at least 32 cryptographically random bytes. Only its
canonical hash is persisted. The raw invitation token, raw exchange token,
invitation URL, and verification code are never persisted.

The clicked-link flow is:

1. `GET /supplier-access/open?token=<invitation-token>` applies strict encoding
   and length validation before any repository call;
2. it hashes the token in memory and resolves the current invitation through
   `InvitationAccessResolver`;
3. unknown, obsolete, revoked, expired, or identity-invalid invitations return
   the same neutral public response;
4. a successful resolution records the view at the exact validated generation;
5. the service creates a 10-minute single-use access exchange;
6. it sets the raw exchange token in a dedicated HttpOnly cookie; and
7. it returns `303 See Other` to `/supplier-access/open`, with no credential in
   the redirect URL or response body.

The exchange cookie is distinct from the Supplier session cookie and uses:

```text
HttpOnly = true
SameSite = Lax
Secure = true outside local development
Path = /supplier-access
Max-Age <= exchange expiry
```

`POST /supplier-access/challenges` reads and hashes that cookie, resolves an
unexpired unused exchange, revalidates the invitation's Company, Supplier,
normalized recipient email, generation, active status and expiry, persists a
verification challenge and its delivery intent, associates the challenge
uniquely with the exchange, consumes the exchange, clears the cookie, and then
sends the verification code using synchronous mail. Challenge confirmation uses
the challenge's opaque identifier, never the invitation or exchange token.

The exchange is single-use. Every read explicitly enforces `now < ExpiresAt`
and `ConsumedAt == nil`, except when completing an idempotent retry. MongoDB TTL
deletion is housekeeping only. One exchange can source at most one challenge:

```text
challenge created but exchange not marked consumed
→ retry finds the challenge by ExchangeID
→ verifies identity
→ completes exchange consumption
→ does not create or send a second challenge

exchange marked consumed with ChallengeID
→ retry returns the same logical challenge result
```

A failed email send does not unconsume the exchange or delete the challenge.
The failed delivery remains explicitly retryable under the verification
challenge delivery rules.

The initial request necessarily exposes the invitation token to the browser and
upstream HTTP path. The application immediately cleans the destination URL but
cannot guarantee that an email client, browser, proxy, or hosting platform never
observed the first request. Therefore the route requires:

```text
Cache-Control: no-store
Pragma: no-cache
Referrer-Policy: no-referrer
no query-string logging
application request-log token redaction
production reverse-proxy/platform query redaction
```

No credential may appear in redirect headers, logs, audit calls, or persistence.
An exchange cannot survive rotation, recipient replacement, reactivation,
revocation, expiry, or identity change because challenge creation revalidates
the authoritative invitation. Every invalid exchange or invitation state
collapses to one public failure; real infrastructure failures retain the bounded
503 behavior.

### 1A.8 Decision H — recoverable verification code and delivery

Verification codes use a dedicated versioned keyring, separate from invitation
link secrets:

```text
SUPPLIER_VERIFICATION_CODE_ACTIVE_VERSION=1
SUPPLIER_VERIFICATION_CODE_KEY_V1=<base64-encoded random key>
```

Decoded keys contain at least 32 random bytes. A missing or malformed active key
fails startup. Tests inject deterministic keys. Old key versions remain
configured while any unexpired challenge references them. Key material is never
persisted, logged, or audited.

The code format is settled:

```text
Length:         exactly 6 characters
Alphabet:       ASCII digits 0-9
Leading zeroes: allowed
Examples:       004271, 918305
```

Codes are strings, never integers. After optional HTTP-boundary whitespace
trimming, input must match `^[0-9]{6}$`. Unicode numerals, separators, spaces,
and variable-length values are invalid.

#### Deterministic unbiased derivation

Each challenge records the active `CodeKeyVersion` at creation. Code derivation
uses domain-separated, length-prefixed canonical input:

```text
"supplier-verification-code-v1"
CodeKeyVersion
ChallengeID
CompanyID
SupplierID
InvitationID
AccessGeneration
NormalizedRecipientEmail
Counter
```

For counters beginning at zero:

1. calculate one HMAC-SHA256 block over the canonical context and counter;
2. read the block as eight unsigned 32-bit big-endian candidates;
3. accept the first candidate below `4,294,000,000`, where:

   ```text
   floor(2^32 / 1,000,000) × 1,000,000 = 4,294,000,000
   ```

4. calculate `candidate % 1,000,000`; and
5. format the remainder as a zero-padded six-character ASCII string.

If all eight candidates are rejected, increment the length-prefixed counter and
derive another block. Direct modulo over an unrestricted digest value is
forbidden because it introduces bias.

The same immutable challenge context always yields the same code. A changed
challenge, Company, Supplier, invitation, generation, or recipient identity
yields an independently derived code.

#### Keyed verifier

An ordinary SHA-256 hash of a six-digit code is forbidden because a
database-only attacker could enumerate all one million values offline. Persist
only:

```text
CodeVerifier
CodeKeyVersion
```

The keyed verifier uses a second domain:

```text
CodeVerifier = HMAC-SHA256(
    keyForVersion,
    canonical(
        "supplier-verification-verifier-v1",
        ChallengeID,
        derivedCode,
    ),
)
```

On confirmation, the server validates the exact ASCII format, derives the
expected code and verifier again, computes the candidate keyed verifier for the
submitted code, and compares in constant time. It fails closed if the persisted
verifier does not match the rederived expected verifier. The raw code, an
unkeyed code hash, and email body are never persisted, audited, or logged.

#### Challenge lifetime and online protections

```text
Challenge validity:           10 minutes
Maximum failed confirmations: 5 per challenge
Request cooldown:             60 seconds per invitation generation
Creation limit:               5 per rolling hour per
                              Company + Supplier + normalized recipient email
                              + invitation + access generation
```

At the exact boundary, `now < ExpiresAt` is potentially valid and
`now >= ExpiresAt` is expired. The fifth failed confirmation atomically consumes
the final attempt and locks the challenge. Later attempts are rejected without
revealing whether the submitted code was malformed, close, or incorrect.
Resending never resets attempts, extends expiry, or changes the code. Creating a
newer challenge supersedes the older challenge but remains subject to both rate
limits.

#### Verification delivery attempts

`supplieraccess` owns:

```go
type VerificationDeliveryAttempt struct {
    ID          string
    CompanyID   string
    ChallengeID string
    OperationID string
    Status      string
    FailureCode *string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

Statuses are `pending`, `sent`, `failed`, and `obsolete`. Attempt identity and
challenge association are immutable. Only `pending → sent`, `pending → failed`,
or `pending → obsolete` is allowed; terminal attempts are historical facts.

Delivery follows the authoritative-record-first synchronous-mail order:

1. confirm the challenge is current and unexpired;
2. persist a pending delivery attempt;
3. rederive the code with the challenge's recorded key version;
4. send synchronously; and
5. conditionally mark the attempt sent or failed.

Provider errors map to bounded failure codes; unrestricted provider text never
reaches persistence.

For the same challenge and operation ID, a `sent` retry sends nothing, a
`failed` retry does not silently resend, a `pending` attempt may be recovered,
and an identity mismatch conflicts. Explicit failed-delivery retry uses a new
operation ID and sends the same code while the challenge remains current and
unexpired. A stale pending recovery revalidates the challenge, rederives the
same code, retries delivery, and conditionally records the result. A crash after
provider acceptance may duplicate a physical email, but both copies contain the
same valid code.

When a newer challenge supersedes an older one, the old challenge becomes
unusable immediately and its pending delivery attempts become obsolete. Sent
and failed attempts remain unchanged. No retry may extend challenge expiry,
reset verification attempts, reactivate a superseded challenge, change its
code, or migrate it to a newer key version.

### 1A.9 Decision I — trusted client-address throttling

Challenge creation applies an independent limit of 20 requests per rolling hour
per normalized client IP, in addition to the stricter invitation/identity
limits from Revision 9.

The raw IP address is never persisted. `supplieraccess` stores a keyed
HMAC-SHA256 derived with a dedicated secret and domain:

```text
SUPPLIER_RATE_LIMIT_FINGERPRINT_KEY=<base64-encoded random key>
domain = "supplier-rate-limit-client-address-v1"
```

The decoded key contains at least 32 random bytes; missing or malformed
configuration fails startup. The key is separate from invitation and
verification-code keys and is never persisted, logged, or audited.

Client-address derivation is fail-safe:

- the direct TCP peer is authoritative by default;
- `TRUSTED_PROXY_CIDRS` is empty by default;
- forwarding headers are considered only when the direct peer belongs to an
  explicitly configured trusted-proxy CIDR;
- a trusted chain is walked from the application outward to select the first
  untrusted hop;
- malformed direct or trusted-chain addresses are rejected rather than hashed
  as arbitrary text; and
- IPv4 and IPv6 addresses are parsed and normalized before HMAC derivation,
  with IPv4-mapped IPv6 normalized to its equivalent IPv4 representation.

Rate-limit state contains only the resulting keyed address hash. It never
contains the raw peer value or forwarding header.

Client-address and identity/invitation quotas are claimed sequentially, in that
order, because MongoDB transactions remain out of scope. Every claim is an
atomic conditional write. An address that is already over limit therefore
cannot consume a victim invitation's identity quota. If the address claim
succeeds and the identity claim fails, the address allowance remains consumed;
this fail-closed trade-off can reduce quota but can never permit excess
challenge creation. Reconciliation does not restore rate-limit allowance.

### 1A.10 Decision J — CAS-backed verification rate limits

`supplieraccess` exclusively owns:

```go
type VerificationRateLimitState struct {
    ID           string
    Scope        VerificationRateLimitScope
    ScopeKeyHash string
    Reservations []VerificationRateReservation
    Revision     int64
    UpdatedAt    time.Time
    ExpiresAt    time.Time
}

type VerificationRateReservation struct {
    ReservationID string
    RequestedAt   time.Time
}
```

The challenge-creation scopes are:

```text
identity_invitation_generation
client_address
```

Both scopes use the dedicated Revision 10 fingerprint key with length-prefixed,
domain-separated canonical inputs:

```text
"supplier-rate-limit-identity-v1"
+ CompanyID
+ SupplierID
+ NormalizedRecipientEmail
+ InvitationID
+ AccessGeneration

"supplier-rate-limit-client-address-v1"
+ canonical IP bytes
```

Only the resulting HMAC fingerprints are persisted. The identity scope permits
five reservations per rolling hour with a 60-second cooldown between distinct
reservations. The globally scoped client-address fingerprint permits 20 per
rolling hour with no additional cooldown.

Both scopes use the same server-controlled `ReservationID`:
`SupplierAccessExchange.ID`. Claims occur in this order:

1. `client_address`;
2. `identity_invitation_generation`;
3. challenge creation, only after both claims succeed.

An existing reservation ID in the retained window succeeds idempotently without
consuming quota. If the second claim fails, the first remains consumed and the
exchange remains unconsumed. Retrying that exchange later can converge because
the first scope recognizes the same reservation.

Each claim:

1. loads state by `Scope + ScopeKeyHash`;
2. removes reservations where `RequestedAt <= now - 1 hour`;
3. returns success if the retained set already contains `ReservationID`;
4. enforces the scope limit and, for identity scope, cooldown;
5. appends the reservation;
6. sets `UpdatedAt = now` and
   `ExpiresAt = latest retained RequestedAt + 1 hour`; and
7. replaces the bounded array while matching the previous `Revision`, then
   increments the revision.

Identity arrays never exceed five entries and client-address arrays never
exceed 20. A rejected claim appends nothing.

First creation attempts an insert under the named unique
`scope + scopeKeyHash` index. A classified collision reloads the winning
document and enters the normal CAS path. CAS retries are bounded. Exhaustion
caused by persistent infrastructure contention fails closed as a bounded 503,
not a false 429.

A TTL index on `ExpiresAt` is housekeeping only. Every claim prunes and evaluates
the rolling window even if an expired state document still exists. Fingerprint
key rotation resets active defense-in-depth rate state; the key remains stable
for Phase 1 and that operational effect must be documented.

Both scopes return the same non-disclosing `429 Too Many Requests`. A bounded
`Retry-After` is derived from the earliest applicable next permitted time
without revealing scope, timestamps, or identity details.

### 1A.11 Decision K — final Phase D session, CSRF and route contract

#### Verification-to-session recovery

`POST /supplier-access/challenges/verify` accepts `challengeId`, the six-digit
`code`, and a client `operationId`. Before consuming the challenge, the service
selects the exact target session:

- a presented `supplier_session` cookie may be reused only when it resolves to
  an active session with the same Company, Supplier and normalized recipient
  email;
- the service never searches identity-wide for a session from another browser;
- a missing, foreign, expired, or revoked presented session causes creation of
  a new session with a server-generated ID; and
- multiple sessions for one Supplier identity are permitted.

A successful challenge CAS requires the challenge to be current,
non-superseded, unexpired, unlocked, unconsumed, at the expected revision, and
verified by the correct code. It atomically records:

```text
ConsumedAt
ConsumedOperationID
SupplierSessionID
TargetTokenGeneration
SessionTokenKeyVersion
```

For a new session, the target generation is 1. For a matching presented session
at generation `N`, it is `N+1`. Only one concurrent confirmation consumes the
challenge.

Same-operation recovery additionally revalidates the submitted code; an
operation ID is idempotency metadata, never an authentication credential. A
different operation receives the neutral consumed-challenge failure. Recovery
creates or finishes updating exactly the recorded session, confirms its token
generation and key version, rederives the raw token, creates or advances the
invitation binding, then re-reads the authoritative invitation before returning
access.

New sessions use the preallocated session ID and immutable
`CreatedFromChallengeID`; a partial unique index prevents one challenge from
creating two sessions. Later verification updates `LastVerifiedChallengeID` and
`LastVerifiedAt` without overwriting creation provenance.

#### Session token derivation and rotation

Supplier session tokens use a dedicated versioned keyring:

```text
SUPPLIER_SESSION_TOKEN_ACTIVE_VERSION=1
SUPPLIER_SESSION_TOKEN_KEY_V1=<base64 key of at least 32 random bytes>
```

Missing, malformed, or undersized active keys fail startup. Older versions stay
configured while active sessions reference them. Keys are never persisted,
logged, or audited.

The raw token is:

```text
canonical input:
    "supplier-session-token-v1"
    TokenKeyVersion
    SessionID
    TokenGeneration
    CompanyID
    SupplierID
    NormalizedRecipientEmail

raw bytes = HMAC-SHA256(keyForVersion, length-prefixed canonical input)
raw token = base64.RawURLEncoding(raw bytes)
TokenHash = hex-encoded SHA-256(raw token)
```

Only `TokenHash`, `TokenKeyVersion`, and `TokenGeneration` persist. Initial
generation is 1. Successful re-verification rotates to the active key and
increments generation exactly once, invalidating the old token immediately.
It resets sliding expiry to `now + 30 days` and absolute expiry to
`now + 90 days`. A revoked session is never resurrected.

Two challenges may concurrently target the same presented session at `N+1`.
Exactly one performs the rotation. The other may adopt `N+1` only after
confirming the exact session identity, key version and target generation; both
may create their independently verified invitation bindings and both return the
same rederived token. `LastVerifiedChallengeID` is a convenience projection;
each consumed challenge is authoritative history. If the session has advanced
beyond a challenge's target, that older operation returns the neutral failure
and never reproduces an obsolete token.

#### Session cookie, renewal and logout

The session cookie is:

```text
Name:     supplier_session
HttpOnly: true
SameSite: Lax
Secure:   true outside explicit local/test mode
Path:     /supplier-access
Domain:   absent
```

Its lifetime never exceeds server-side sliding expiry. Protected requests hash
the cookie, globally resolve the session, constant-time compare the stored hash,
and fail closed at `now >= SlidingExpiresAt` or
`now >= AbsoluteExpiresAt`.

Successful protected activity renews:

```text
SlidingExpiresAt = min(now + 30 days, AbsoluteExpiresAt)
```

without rotating the token. A revision-guarded CAS prevents lost updates. If a
concurrent request already stored an equal or later expiry, the caller adopts
that result. The response reissues `supplier_session` with a lifetime matching
the new server expiry. Infrastructure failure returns a bounded 503.

Logout conditionally revokes the resolved session and clears its browser
credentials. Unknown, expired, or already-revoked sessions return the same
idempotent response after the CSRF rule below is satisfied. Bindings remain
history; the session boundary invalidates all of them.

#### CSRF

Phase D establishes the CSRF mechanism for logout and every later Supplier
mutation route. The expected token is derived from:

```text
"supplier-session-csrf-v1"
TokenKeyVersion
SessionID
TokenGeneration
CompanyID
SupplierID
NormalizedRecipientEmail
```

using the session-token keyring and length-prefixed domain separation. It is set
as:

```text
Name:     supplier_csrf
HttpOnly: false
SameSite: Lax
Secure:   true outside explicit local/test mode
Path:     /supplier-access
Domain:   absent
```

Protected mutations require the `supplier_csrf` cookie,
`X-CSRF-Token` header, and server-derived expected token to match in constant
time. Initial verification and re-verification rotate both session and CSRF
credentials. Logout clears both cookies.

Even logout with an unknown, expired, or revoked session requires the header and
cookie values to match each other; it then clears both cookies and returns the
same response. For a valid session they must also match the server-derived
token. A missing or mismatched CSRF value returns 403 and does not revoke or
clear an otherwise valid session.

#### Invitation authorization and Phase D routes

Every protected invitation request validates the active session, immutable
session identity, exact generation-bound binding, and current active/unexpired
invitation through a consumer-owned capability. Phase E consumes this reusable
authorization service and never reimplements session or binding validation.

Phase D routes are:

```text
GET  /supplier-access/open?token=...
GET  /supplier-access/open
POST /supplier-access/challenges
POST /supplier-access/challenges/resend
POST /supplier-access/challenges/verify
POST /supplier-access/session/logout
```

Resend carries `challengeId` and `operationId` in the body. Challenge IDs are
cryptographically random, opaque and non-sequential handles, never credentials
or URL components. Explicit failed-delivery retry has a 60-second cooldown and
at most five explicit resend attempts per rolling hour for one challenge. Its
initial delivery is separate from this resend allowance.

Phase D implements the exchange/challenge/session routes, generation-bound
bindings, reusable protected-request authorization, CSRF, logout, and bounded
recovery methods. Supplier invitation and RFQ projection endpoints defer to
Phase G with their privacy allowlists; offer routes remain Phase E and outcome
routes remain Phase F. Privileged reconciliation HTTP routes remain Phase G.

### 1A.12 Decision L — dedicated verification resend throttling

Explicit verification-code resend has a separate durable rate scope:

```text
challenge_resend
```

It uses the existing dedicated rate-fingerprint key with a new length-prefixed,
domain-separated input:

```text
"supplier-rate-limit-challenge-resend-v1"
+ CompanyID
+ ChallengeID
```

Only the HMAC fingerprint is stored in `VerificationRateLimitState`. The raw
challenge handle is not copied into rate-limit state.

The first explicit resend is allowed only when:

```text
RequestedAt >= Challenge.CreatedAt + 60 seconds
```

After that, each distinct resend operation requires 60 seconds after the most
recent retained resend reservation. At most five distinct resend reservations
are allowed per rolling hour for one challenge. Reusing the same operation ID
is idempotent and neither sends a second message nor consumes another
reservation.

Challenge creation and its initial delivery attempt do not consume resend
allowance. Consequently, one challenge can have its initial delivery plus at
most five explicit resend attempts while it remains current and within its
ten-minute expiry. This Revision 13 rule supersedes the earlier ambiguous
wording that described five total delivery attempts.

The service validates the current, unexpired, unlocked, unconsumed challenge
and current invitation identity/generation before claiming resend capacity.
It also resolves an existing delivery operation before a new claim, so an
idempotent retry remains stable. A new pending delivery intent is persisted
only after the CAS-backed resend reservation succeeds.

Resend limits do not consume or restore either challenge-creation scope. A
rejection uses the same bounded, non-disclosing `429` and `Retry-After`
contract as the other verification rate scopes; infrastructure/CAS exhaustion
remains a bounded `503`.

### 1A.13 Decision M — final Phase D HTTP and audit boundary

All six Phase D routes, including redirects and errors, return:

```text
Cache-Control: no-store, max-age=0
Pragma: no-cache
Referrer-Policy: no-referrer
```

The invitation-token redirect remains `303 See Other`.

Successful verification returns only:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"verified"}
```

It sets `supplier_session` and `supplier_csrf` only after session recovery,
binding creation and final invitation revalidation have completed. It returns
no invitation, Supplier, session, generation or credential identifier. A
successful same-operation recovery returns the identical response.

Successful and idempotent logout returns `204 No Content`, has no body, and
clears both cookies. Active revocation and an unknown, expired or already
revoked session are indistinguishable. Missing or mismatched CSRF returns
`403`, performs no revocation, and clears neither cookie. A resolvable active
session requires the cookie, header and server-derived token to match. An
unresolvable session still requires present, canonical, constant-time-equal
cookie and header values before browser cleanup.

`supplieraccess` declares a consumer-owned, primitive-only
`SupplierAccessAuditRecorder` covering:

```text
challenge requested
challenge delivery retried
verification failed
verification succeeded
session created
session reverified
session renewed
session revoked
```

Challenge and verification events use subject type `supplier_invitation` and
the invitation ID. Session lifecycle events use subject type
`supplier_session` and the Supplier session ID. Every event uses actor type
`supplier` and the authoritative Supplier ID. Pre-verification Supplier actors
identify the Supplier attached to the authoritatively resolved invitation; they
do not assert that a Supplier session already authenticated.

Audit method signatures accept only typed primitives and timestamps. They have
no parameter capable of carrying invitation tokens or URLs, exchange/session
tokens, CSRF values, verification codes, recipient emails, provider errors,
SMTP details, request bodies or headers.

Verification failures are audited only after Company, Supplier, invitation,
generation and challenge identities are authoritative and only when a failed
attempt transition actually increments the counter or locks the challenge.
Allowed bounded reason codes are:

```text
invalid_code
expired
superseded
attempt_limit_reached
already_consumed
stale_generation
```

These codes never change the neutral public response. Malformed or unknown
invitation, exchange, session or challenge credentials and every request with
no authoritative Company produce no tenant audit event. Credential-free
aggregate operational metrics remain separate from tenant audit.

Transition events emit only for the authoritative write:

- session creation only for the inserted session;
- re-verification only for the token-generation rotation;
- renewal only for a CAS that extends `SlidingExpiresAt`;
- revocation only for active-to-revoked CAS;
- verification success only for the challenge-consumption winner; and
- verification failure only for the failed-attempt transition.

Same-operation recovery, adopted concurrent results and idempotent logout do
not manufacture duplicate audit events.

### 1A.14 Decision N — consolidated Phase E Supplier Offer contract

This approved amendment is the complete Phase E implementation contract. It
supersedes earlier Phase E wording where the two conflict, including Revision
14's statement that `SupplierOfferChain` is the submission serialization
point. Historical revision entries remain unchanged.

#### Offer ownership and authorization

`RecipientIdentity` is the invitation's normalized recipient email. It is
stable across secret rotation and reactivation for the same recipient.
`AccessGeneration` remains a separate request-time authorization proof and
never becomes offer ownership identity.

Every Supplier Offer request uses the reusable Phase D protected-request
authorization service. The authorized session identity, exact invitation
binding generation, current active/unexpired invitation, Supplier,
`RecipientIdentity`, Company, Invitation, and issued RFQ version must all
match. `supplieroffers` consumes narrow capabilities and never reads
`supplieraccess` or `rfqissuance` collections directly.

A replacement recipient is a different `RecipientIdentity`. Existing immutable
Offer Versions remain historical records of the previous recipient. A new
recipient starts a blank draft unless they explicitly invoke the approved
copy-forward operation. Recipient replacement may conditionally archive an
unfinished draft only through a narrow `supplieroffers` capability and only
while that draft remains `active`.

#### Draft state and one-unfinished-draft invariant

Draft state is explicit:

```text
active
submitting
archived
```

Only `active` drafts may be edited, acknowledged, removed, copied into, or
claimed for submission. A `submitting` draft is immutable and remains covered
by the one-unfinished-draft uniqueness invariant until completion. The partial
unique index therefore treats both `active` and `submitting` as unfinished.
There may be at most one unfinished draft for a Company and Offer Chain.

No timeout automatically returns a `submitting` draft to `active`. Stalled
claims are completed through bounded tenant-scoped reconciliation.

The earlier conceptual draft and version structs are extended by this
amendment. The draft persists:

```text
Status
DeliveryCharge
OfferTaxReviewRequired
DeliveryChargeReviewRequired
submission-claim fields named below
```

Draft lines persist stable RFQ lineage, copied-source provenance,
`ReviewRequired`, and `ConfirmationRequired`. Draft conditional groups persist
their copied-source provenance and `ReviewRequired`.

The immutable version persists `DeliveryCharge`, the exact immutable line,
tax, and conditional-group snapshots, and the source/submission fields named
below. Earlier `ContentFingerprint` wording means the canonical
`SubmissionFingerprint`; implementations store one fingerprint field, not two.

#### Explicit line responses and commercial fields

Every issued RFQ line has exactly one draft response:

```text
unanswered
quoted
no_bid
unavailable
```

Submission rejects `unanswered`. A `quoted` response covers the complete
authoritative RFQ quantity and unit; Phase 1 does not support partial-quantity
quotes. The target issued RFQ version remains authoritative for material
identity and description, specification, quantity, unit, required date, and
procurement notes.

A quoted response may contain:

```text
UnitPriceExcludingTax
Brand
SKU
ProductDescription
LeadTime
SupplierLineNotes
CommercialExceptions
LineTax (line_level mode only)
```

The server calculates the line subtotal from authoritative quantity multiplied
by unit price. Browser-supplied subtotals and totals are never authoritative.
All Money values use the issued RFQ currency, currently `MYR`, and the central
Money arithmetic and rounding policy. Floating-point arithmetic is forbidden.

Text input is trimmed at the boundary. Registration numbers are limited to 128
characters, tax basis notes and conditional-group descriptions to 500,
conditional-group names to 120, line notes and commercial exceptions to 2,000,
whole-offer Supplier notes to 4,000, and withdrawal reasons to 500 characters.
No country-specific tax-registration validation is introduced in Phase 1.

#### Strict discriminated tax contract

Exactly one tax mode is present:

```text
not_applicable
line_level
offer_level
```

Fields belonging to another mode are rejected with `422`; they are never
silently ignored.

For `not_applicable`:

- no line-level tax record is allowed;
- no offer-level tax record is allowed; and
- calculated tax total is zero.

For `line_level`, every positively quoted line contains exactly one record:

```go
type QuotedLineTax struct {
    TaxType            string
    Exempt             bool
    RateBPS            *int64
    RegistrationNumber string
    BasisNote          string
}
```

`TaxType` is `service_tax`, `sales_tax`, or `other`.

- When `Exempt` is true, `RateBPS` must be absent and calculated line tax is
  zero.
- When `Exempt` is false, `RateBPS` is required and ranges from 1 through
  10,000 inclusive.
- Non-exempt `service_tax` and `sales_tax` require a registration number.
- `other` always requires a bounded basis note; its registration number is
  optional.
- Offer-level tax fields are forbidden.

For each quoted line, the server calculates:

```text
authoritative quantity × quoted unit price
→ line subtotal
→ ApplyRateBPS
→ central Money rounding
→ calculated line tax
```

The taxable base is the quoted line subtotal only. Delivery and conditional
charges are excluded. Total line-level tax is the sum of individually rounded
line-tax amounts; it is never recalculated from an aggregate subtotal.

For `offer_level`, exactly one record contains:

```go
type QuotedOfferTax struct {
    TaxType            string
    TaxAmount          money.Money
    BasisNote          string
    RegistrationNumber string
}
```

The tax amount is positive and uses the exact RFQ currency. A bounded basis
note is always required. `service_tax` and `sales_tax` require a registration
number; registration is optional for `other`. Line tax records and rates are
forbidden. The server validates and totals the Supplier's quoted amount but
does not infer a rate or recalculate it.

An `offer_level` Offer Version may only be awarded as a complete offer: every
positively quoted line is selected together, or none is selected. It cannot be
partially selected or prorated. Line-by-line awards across Suppliers therefore
require `line_level` or `not_applicable`. Submitted versions preserve an
immutable tax snapshot, and awards reference that exact snapshot.

Registration numbers and basis notes are Supplier-quoted commercial
information, not platform verification of legal tax status.

#### Non-overlapping conditional-charge groups

Each positively quoted line belongs to zero or one conditional-charge group.
Overlap between groups is prohibited; a submitted offer that places one line
in multiple groups is rejected with `422`.

Each group contains:

```text
ID
Name
Description
ApplicableRFQLineIDs
Trigger
Calculation
```

Supported triggers are:

```text
any_selected
all_selected
selected_subtotal_at_least
```

Supported calculations are:

```text
fixed_amount
percentage_of_selected_subtotal
```

A group references at least one unique line ID. Every referenced line belongs
to the same issued RFQ version and is positively quoted. Unknown lines,
`no_bid`, `unavailable`, duplicate IDs within one group, overlapping lines
between groups, and references to delivery charges, tax records, other groups,
or any unrelated object are rejected.

The group subtotal is the sum of selected quoted-line subtotals belonging to
that group.

- `any_selected` triggers when at least one group line is selected.
- `all_selected` triggers only when every positively quoted listed line is
  selected.
- `selected_subtotal_at_least` triggers when the group subtotal is at least its
  positive threshold Money in the exact RFQ currency.
- `fixed_amount` requires one positive Money amount in the RFQ currency and
  applies it exactly once when triggered.
- `percentage_of_selected_subtotal` requires `RateBPS` from 1 through 10,000
  inclusive, applies it only to the group subtotal, and uses central rounding.

A non-triggered group contributes zero. A percentage never applies to tax,
delivery, or another conditional charge. Submitted groups and their rules are
immutable.

Phase F recalculates each Supplier's groups independently from the exact Offer
Version and exact lines selected from that Supplier. It never trusts a browser
total. The Award Revision persists the Offer Version ID, selected line IDs,
triggered group IDs, server-calculated amounts, and calculation inputs needed
for auditability.

#### Fixed delivery charge

A draft and immutable Offer Version contain zero or one delivery charge:

```go
type DeliveryCharge struct {
    Amount money.Money
}
```

Absence means no separately quoted delivery charge; it is not represented by a
zero-valued object. The amount must be positive and use the exact issued RFQ
currency through `Amount.Currency`; no duplicate currency field is persisted.
Zero, negative, percentage, rate, basis-point, trigger, line
allocation, tax, quantity, proration, and second-charge fields are rejected.

At award time:

```text
no positively quoted line selected → delivery charge zero
one or more positively quoted lines selected → fixed charge once
```

It is never multiplied by selected line count, quantity, group count, space, or
work item. It is excluded from line-level tax bases, offer-level tax,
conditional-group subtotals, percentage-charge bases, and line subtotals.
Authoritative totals add independently calculated selected-line subtotals,
applicable tax, triggered conditional charges, and delivery charge.

The Supplier may add, edit, or remove the charge while the draft is active.
Submission validates and snapshots it; a later change requires another Offer
Version. Full-offer preview assumes all positively quoted lines are selected
and applies the delivery charge once.

Phase F applies an immutable version's delivery charge at most once and
persists the applied amount and source snapshot. One Award Revision cannot
select lines from multiple Offer Versions belonging to the same Offer Chain.

#### Copy-forward source, matching, and provenance

Copy-forward reads only an immutable submitted Offer Version, never another
recipient's mutable draft.

Target lines match one-to-one:

```text
M7-backed line  → SourceMaterialRequirementID
M8-native line → LineageID
```

The target issued RFQ version always supplies quantity, unit, material identity
and description, specification, required date, and procurement notes. Copying
never overwrites them.

For a compatible quoted response, copy unit price, Supplier line notes, brand,
SKU, product description, lead time, commercial exceptions, line-level tax
configuration, and source provenance. Recalculate target line subtotal and
line tax from the target quantity and copied unit price. Previously calculated
subtotals, tax amounts, group charges, and grand totals are never copied.

Every copied line records:

```text
CopiedFromOfferVersionID
CopiedFromOfferLineID
CopiedAt
```

A copied quoted line sets `ReviewRequired = true` when any Supplier-visible
authoritative input changed: quantity, unit, specification, required date,
procurement notes, material identity, or material description. A changed
`MaterialID` makes the line incompatible; it starts `unanswered` rather than
copying pricing.

Copied `no_bid` and `unavailable` responses always set
`ConfirmationRequired = true`, even when the RFQ line did not change. New
target lines start `unanswered`; removed source lines do not appear in the new
draft.

`SupplierNotes` copies as text only, is immediately editable, and requires no
acknowledgement. It carries no source-recipient ownership metadata.
`OfferValidUntil` never copies; the target draft starts without it and
submission requires a newly supplied value later than submission time.

Tax copy rules are:

- `not_applicable` copies without a review flag;
- `line_level` copies with its quoted line and is recalculated against target
  quantity and copied price; and
- `offer_level` copies only when its currency, positive amount, tax type,
  registration requirement, and basis note remain structurally valid, and
  always sets `OfferTaxReviewRequired = true`.

A delivery charge may copy, but always sets
`DeliveryChargeReviewRequired = true`.

A conditional group copies only when every source line maps one-to-one to an
eligible target line and was copied as a quoted response. Each copied group
receives a new draft-owned ID, retains `CopiedFromChargeGroupID`, remaps its
line IDs, copies its commercial rule, and sets `ReviewRequired = true`. A
quoted line's own review requirement does not prevent group copying; both
reviews must be completed independently.

If any source group line is removed, unanswered, declined, materially
incompatible, ambiguously or multiply mapped, or already assigned to another
copied group, omit the entire group. Never silently narrow its membership. The
copy result reports only bounded user-facing reasons:

```text
missing_line
incompatible_line
non_quoted_line
ambiguous_mapping
overlapping_group
```

These are copy feedback, not submission errors.

#### Server-owned review gates

These fields are server-owned:

```text
OfferTaxReviewRequired
DeliveryChargeReviewRequired
ConditionalGroup.ReviewRequired
Line.ReviewRequired
Line.ConfirmationRequired
```

Generic draft create, replacement, or PATCH input cannot set or clear them.
They clear only through an explicit revision-guarded acknowledgement, an edit
followed by explicit acknowledgement, or removal of the associated copied
whole-offer structure.

A line acknowledgement identifies the exact draft and RFQ line, supplies
`ExpectedRevision`, and confirms the current copied response type. A
whole-offer acknowledgement identifies the exact tax, delivery, or group
structure. The CAS clears only the applicable flag and increments draft
revision. Quoted commercial fields may be edited before acknowledgement;
declines require explicit reconfirmation.

Submission is blocked while:

- `OfferValidUntil` is absent or not later than submission time;
- any line is unanswered;
- any copied quoted line requires review;
- any copied decline requires confirmation;
- copied offer-level tax requires review;
- copied delivery requires review; or
- any copied conditional group requires review.

#### Draft-CAS submission claim

The active draft, not the Offer Chain, is the submission serialization point.
Edits, submission, and recipient-replacement archival compete on the same
mutable draft.

Before claiming, the service performs complete submission validation,
recalculates every authoritative total, and creates a canonical content
fingerprint. It generates the candidate Offer Version ID and derives the
candidate version number from the chain's current latest immutable version.
Validation rechecks the Phase D authorization identity and generation,
requires the invitation to remain active and unexpired, requires
`submittedAt` to be strictly before the issued RFQ response deadline, requires
`OfferValidUntil` to be strictly later than `submittedAt`, and enforces every
line, review, currency, tax, charge, and delivery rule in this amendment.

The single MongoDB CAS matches at least:

```text
CompanyID
OfferChainID
DraftID
Status = active
Revision = ExpectedRevision
RecipientIdentity
InvitationID
IssuedRFQVersionID
```

The winning CAS atomically writes:

```text
Status = submitting
SubmissionOperationID
SubmissionBaseRevision
SubmissionFingerprint
SubmissionRecipientIdentity
SubmissionInvitationID
SubmissionRFQVersionID
SubmissionAccessGeneration
CandidateOfferVersionID
CandidateVersionNumber
SubmissionClaimedAt
Revision = previous Revision + 1
```

`SubmissionAccessGeneration` records the valid request-time proof and does not
change ownership identity. The successful draft CAS is the authoritative
submission point. Secret rotation or reactivation for the same normalized
recipient after that point does not cancel submission. Recipient replacement
conditionally archives only an `active` draft, so whichever CAS wins first
determines the outcome; replacement cannot erase a `submitting` draft.

Candidate version identity is permanently reserved for the recorded operation.
Retries and reconciliation never allocate a new candidate ID or number.
Unique indexes on Company + Offer Chain + version number, the canonical
operation-ID scope, and candidate Offer Version ID remain final safeguards.
One operation ID cannot be reused for unrelated submission content.

After the claim, completion:

1. re-reads the claimed draft;
2. verifies its operation identity and fingerprint;
3. builds the immutable Offer Version exclusively from frozen claimed content;
4. inserts it with the recorded candidate ID and number;
5. ensures the separate eligibility aggregate exists in its initial
   `eligible` state;
6. advances the Offer Chain through revision-guarded CAS; and
7. archives the exact claimed draft only after the chain points to the version.

The immutable version records at least:

```text
SourceDraftID
SourceDraftRevision
SubmissionFingerprint
SubmissionOperationID
RecipientIdentity
InvitationID
IssuedRFQVersionID
```

`SourceDraftRevision` equals `SubmissionBaseRevision`, the revision whose
commercial content was fingerprinted. It is not the incremented revision
created by changing the draft state to `submitting`.

A same-operation retry resumes only when company, chain, draft, submission base
revision, fingerprint, recipient, invitation, RFQ version, candidate ID, and
candidate number all match. It completes missing steps or returns the existing
version. A changed identity or content under the same operation ID is an
idempotency conflict. A different operation encountering `submitting` receives
`409` and cannot reset, take over, or re-claim it.

Submission reconciliation handles:

```text
claimed draft, version absent
→ insert the recorded immutable version

version inserted, eligibility absent
→ create the unique initial eligible aggregate

version and eligibility exist, chain not advanced
→ verify exact identity and advance the chain

chain advanced, draft still submitting
→ archive the exact claimed draft

all complete
→ return idempotently
```

It never generates new commercial content, chooses a new number, modifies
frozen content, adopts a mismatched fingerprint, rolls the chain backward,
releases a submitting claim, or duplicates audit events.

#### Separate mutable Offer-Version eligibility aggregate

An immutable Offer Version is never updated for withdrawal or award state.
`supplieroffers` owns this separate mutable aggregate:

```go
type SupplierOfferEligibility struct {
    ID             string
    CompanyID      string
    OfferChainID   string
    OfferVersionID string

    State            string
    ClaimType        string
    OperationID      string
    ClaimID          string
    WithdrawalReason string
    Revision         int64

    ClaimedAt   *time.Time
    CompletedAt *time.Time
}
```

An initial gate has `State = eligible`, `Revision = 1`, empty claim fields, and
nil claim/completion timestamps. The unique Company + Offer Version index makes
same-operation creation and reconciliation converge on one gate.

For `withdrawal_claimed` and `withdrawn`, `ClaimType` is `withdrawal`,
`OperationID` identifies the withdrawal operation, `ClaimID` is the immutable
withdrawal ID, and `WithdrawalReason` is non-empty. For `award_claimed` and
`awarded`, `ClaimType` is `award`, `OperationID` is the Award finalisation
operation, `ClaimID` is the candidate Award Revision ID, and
`WithdrawalReason` is empty. `ClaimedAt` is present in every claimed/completed
state; `CompletedAt` is present only in `withdrawn` or `awarded`.

Normal state transitions are:

```text
eligible
→ withdrawal_claimed
→ withdrawn

eligible
→ award_claimed
→ awarded
```

One compensating transition exists solely for a failed multi-version award
finalisation:

```text
award_claimed → eligible
```

That release CAS must match Company, Offer Version, current revision,
`ClaimType = award`, exact Award operation ID, and exact candidate Award
Revision claim ID, and must first verify that no matching immutable Award
Revision exists. It clears the award claim fields and timestamps, leaves
`WithdrawalReason` empty, and increments revision. No other claimed or
completed state can return to `eligible`.

`WithdrawalReason` is required, trimmed, non-empty, and bounded to 500
characters when `ClaimType == "withdrawal"`. It must be empty for
`ClaimType == "award"`; an award claim carrying a non-empty reason is rejected.
It is persisted atomically by the winning
`eligible → withdrawal_claimed` CAS and is immutable thereafter, including
during reconciliation.

#### Withdrawal

Withdrawal input contains:

```text
OfferVersionID
OperationID
Reason
ExpectedEligibilityRevision
```

The immutable record is:

```go
type SupplierOfferWithdrawal struct {
    ID                     string
    CompanyID              string
    OfferChainID           string
    SupplierOfferVersionID string
    InvitationID           string
    RecipientIdentity      string
    OperationID            string
    Reason                 string
    WithdrawnAt            time.Time
    SchemaVersion          int
}
```

The service confirms that the Offer Version belongs to the authenticated
Supplier identity and is the Offer Chain's latest submitted version. A newer
submitted version permanently supersedes an older version; the older version
returns `409` and does not become eligible again if the newer version is later
withdrawn.

An expired `OfferValidUntil` does not prevent withdrawal. Replacement
submission remains subject to the response window.

The authoritative withdrawal sequence is:

1. generate a candidate immutable withdrawal ID;
2. CAS the exact eligibility revision from `eligible` to
   `withdrawal_claimed`;
3. atomically record claim type, operation ID, candidate withdrawal ID,
   bounded `WithdrawalReason`, and claim time;
4. insert the immutable withdrawal record from the recorded claim; and
5. conditionally mark the gate `withdrawn` and set `CompletedAt`.

The final withdrawal record must exactly match the gate's operation ID, claim
ID, Offer Version ID, and bounded reason. Same-operation recovery must match
the stored reason exactly and converges on the recorded withdrawal. Reusing
the same operation ID with a different reason is an idempotency conflict.
Different operations cannot take over a claim.

Reconciliation copies the stored `WithdrawalReason` into the immutable
withdrawal; it never invents or reloads it from a request. Audit emits only for
the operation that won the authoritative claim, after the immutable withdrawal
exists and the gate is completed; retries and reconciliation do not manufacture
another event. The approved business reason may be included only if the
existing audit policy permits bounded business reasons; otherwise audit records
a bounded reason category and the complete reason exists solely in the
immutable withdrawal record.

#### Withdrawal versus final award publication

Provisional award-draft selections do not claim Offer-Version eligibility and
therefore do not block withdrawal. Only final award publication claims a
selected version:

```text
eligible → award_claimed
```

After the immutable Award Revision exists:

```text
award_claimed → awarded
```

Every award claim records the finalisation operation ID and candidate Award
Revision ID as its common claim identity. Award claims reject a non-empty
`WithdrawalReason`. Withdrawal after either `award_claimed` or `awarded`
returns `409`; an award claim after `withdrawal_claimed` or `withdrawn` cannot
publish that selection. The first eligibility CAS is the per-version
withdrawal-versus-award linearization point.

A final Award Revision may select several Offer Versions. Phase F therefore
uses an authoritative Award finalisation claim containing:

```text
AwardOperationID
SelectedOfferVersionIDs
SelectionFingerprint
CandidateAwardRevisionID
```

It acquires all required eligibility gates in a deterministic order, and every
gate records the same Award claim identity. If a gate is withdrawn or owned by
another operation, no immutable Award Revision is created. Gates already
claimed by this exact operation may be released only after verifying that no
matching Award Revision exists.

Crash recovery either completes acquisition and publication or safely releases
only claims owned by that exact failed operation. It can never release another
operation's gate. Partial claims cannot publish an incomplete Award Revision.
Once the immutable Award Revision exists, reconciliation marks its exact gates
`awarded`; it never releases them.

Phase E implements the eligibility aggregate, withdrawal flow, reconciliation,
and narrow award-claim/release capabilities with their per-gate real-Mongo
tests. The authoritative multi-version finalisation claim, deterministic
multi-gate acquisition, immutable Award Revision publication, and
provisional-selection boundary are implemented and integration-tested in Phase
F. This scheduling does not weaken the settled one-winner contract.
`awards` declares the narrow consumer-owned claim/release capability, which a
composition adapter over `supplieroffers.Service` implements; `awards` never
reads the eligibility collection directly.

Eligibility reconciliation handles:

```text
withdrawal_claimed, withdrawal absent
→ insert the recorded withdrawal with the exact stored reason

withdrawal exists, gate not completed
→ verify identity and mark withdrawn

award_claimed, matching Award Revision exists
→ mark awarded

partial multi-offer award claims
→ complete or safely release only claims owned by the exact operation
```

Reconciliation never invents a reason, operation ID, selection, candidate
identity, or commercial record.

#### Persistence safeguards

Phase E adds named indexes for:

```text
CompanyID + InvitationID + IssuedRFQVersionID
    one Offer Chain

CompanyID + OfferChainID + unfinished Draft status
    at most one active/submitting draft

CompanyID + OfferChainID + VersionNumber
    one immutable Offer Version number

canonical Company-scoped SubmissionOperationID
    one submission operation identity

CandidateOfferVersionID
    one immutable candidate

CompanyID + OfferVersionID
    one eligibility gate

CompanyID + SupplierOfferVersionID
    at most one immutable withdrawal

canonical Company-scoped Withdrawal OperationID
    one withdrawal operation identity
```

Real MongoDB proves each concurrency and uniqueness boundary. Unexpected
duplicate-key collisions fail closed and are never silently reassigned.

#### Required witnessed RED → GREEN coverage

Tests cover:

- every valid tax-mode combination and every forbidden cross-mode field;
- exemptions, rate boundaries, per-line rounding, aggregate-of-rounded-lines,
  MYR enforcement, registration requirements, and basis-note rules;
- complete-offer enforcement for `offer_level` tax;
- every conditional trigger and calculation, non-overlap, unknown/declined
  lines, fixed-once behavior, and isolated percentage bases;
- fixed delivery validation, at-most-once application, and isolation from tax
  and conditional calculations;
- line and whole-offer copy-forward matching, recalculation, provenance,
  omissions, review flags, explicit acknowledgements, and submission gates;
- recipient replacement ownership and active-draft archival races;
- edit versus submit, concurrent different submissions, same-operation
  convergence, claim takeover rejection, immutable claimed drafts, candidate
  recovery windows, exact fingerprint content, and tenant-scoped idempotent
  reconciliation;
- concurrent withdrawals, withdrawal versus award, expired and superseded
  behavior, one immutable withdrawal, audit winner, and crash recovery;
- crash after withdrawal claim recreates the exact stored reason;
- same withdrawal operation and reason converge, while a changed reason
  conflicts;
- award claim with a non-empty `WithdrawalReason` is rejected;
- provisional selections do not block withdrawal, award claims block it
  immediately, partial multi-offer claims cannot publish an incomplete Award
  Revision, and cleanup cannot release another operation's gate; and
- tenant, Supplier, invitation, recipient, issued-version, revision, session,
  binding-generation, CSRF, privacy, HTTP, composition, and audit boundaries.

Concurrency, uniqueness, candidate replacement, crash-window, withdrawal, and
multi-gate claim tests use real MongoDB and execute with the repository's
serialized `-p 1` integration-test convention. Production code is written only
after its focused failing test is witnessed. Explanatory comments document the
security boundary, state-machine invariants, CAS linearization points,
canonical fingerprint rules, and recovery decisions.

---

## 0. Purpose and milestone boundary

M8 implements the external sourcing and award-decision lifecycle:

```text
Ready M7 RFQ
→ immutable issued RFQ version
→ Supplier Invitations
→ verified Supplier access
→ Supplier Offer drafts and immutable submissions
→ transparent comparison
→ provisional line selections
→ immutable award revisions
→ explicit outcome notifications
→ Supplier receipt acknowledgement
```

M8 never modifies M7 Material Requirements or M7 RFQ lines. M7 remains the internal procurement-preparation foundation. Its RFQ chain identity and ready snapshot become inputs to M8.

M8 records the selected Supplier Offer lines only. It does not:

- update `CostItem.Committed`;
- create a Purchase Order;
- create a financial commitment record;
- allocate award totals into the cost ledger;
- treat an award acknowledgement as a contractual order acceptance.

The next procurement milestone will own:

```text
Award
→ Purchase Order
→ Supplier accepts / declines / requests changes
→ committed-cost update
```

---

## 1. Approved product decisions

The following decisions are closed:

1. Selecting a Supplier Offer records the award only; it does not create commitment.
2. Additional Suppliers may be invited after an RFQ version has been issued.
3. Supplier Offers may be partial, but every RFQ line must explicitly be `quoted`, `no_bid`, or `unavailable`.
4. Supplier Offer revisions create immutable versions.
5. Submissions and revisions are blocked after the response deadline; the contractor may extend or reopen through a new immutable RFQ version.
6. Supplier access uses a secure invitation link plus one-time email verification.
7. A verified Supplier session uses a 30-day sliding expiry and a 90-day absolute re-verification limit.
8. One stable Supplier Invitation advances to the newest immutable RFQ version.
9. Previous RFQ versions and the Supplier’s own earlier offers remain visible read-only.
10. A Supplier may optionally copy compatible lines from an earlier offer into a new editable draft.
11. Compatible lines match by stable `SourceMaterialRequirementID`, not Material ID alone.
12. The contractor sets one immutable RFQ currency at issuance.
13. Line prices exclude tax. Quoted tax and delivery charges are separate.
14. Each offer uses one tax mode: `not_applicable`, `line_level`, or `offer_level`.
15. Quoted tax is an estimate for commercial comparison; a later invoice or validated e-Invoice remains authoritative.
16. Submitted offers may be withdrawn while preserving the immutable version and audit history.
17. Every submitted offer requires `OfferValidUntil`.
18. Expired offers cannot be selected; the Supplier must submit a renewed immutable version.
19. Different RFQ lines may be awarded to different Suppliers.
20. One individual RFQ line must be awarded in full to one Supplier or left unawarded. M8 does not split line quantities.
21. Suppliers define structured conditional charges for partial offer selection. The platform does not invent or prorate charges.
22. Contractor selections remain provisional until finalisation.
23. Correcting a final award creates a new immutable award revision with a required reason.
24. Finalising an award sends no email automatically.
25. Selected and unsuccessful Suppliers are both notified through a separate explicit action.
26. A selected Supplier only acknowledges receipt in M8.
27. Each outcome notification may include a private per-Supplier message.
28. Each invitation snapshots one recipient name and email.
29. Recipient replacement revokes the previous recipient’s access and rotates the invitation secret.
30. A replacement recipient may optionally copy an earlier recipient’s offer into a new draft or start blank.
31. Unawarded lines require an explicit reason.
32. Invitation expiry, revocation, resend, and secret rotation are distinct controls.
33. One active offer draft exists per Invitation + RFQ Version.
34. Comparison is transparent and human-controlled; M8 does not assign an automatic winner.
35. RFQ Version 2 and later are created from M8-owned amendment drafts.
36. Records are persisted before email delivery; delivery state and retries are tracked separately.
37. Invitations are created first and sent explicitly.
38. M8 uses four bounded modules.
39. No subagents, Git operations, commits, branches, worktrees, or pull requests are part of the implementation workflow.

---

## 2. Module ownership and dependency boundaries

M8 owns four modules:

| Module | Primary responsibility |
|---|---|
| `internal/rfqissuance` | immutable RFQ versions, amendment drafts, Supplier Invitations, invitation delivery intent, M7 issuance-status capability |
| `internal/supplieraccess` | invitation-token exchange, email verification, restricted Supplier sessions, access revocation |
| `internal/supplieroffers` | one active draft, immutable offer versions, withdrawal, pricing, quoted tax, conditional charges, copy-forward |
| `internal/awards` | comparison projections, provisional selections, immutable award revisions, outcome notifications, receipt acknowledgement |

Conceptual flow:

```text
rfqissuance
    ↓
supplieraccess
    ↓
supplieroffers
    ↓
awards
```

This flow does not permit repository access across modules. Every cross-module dependency is a consumer-owned capability interface. Composition adapters exist only where Go named return types require conversion.

### 2.1 M7 integration

`rfqissuance` consumes an M8-owned capability that reads the M7 ready RFQ snapshot.

M7’s existing `IssuanceStatusSource` is wired to an `rfqissuance` capability:

```go
type IssuanceStatusSource interface {
    RFQChainIssued(ctx context.Context, companyID, rfqChainID string) (bool, error)
}
```

After the first issued version exists, this capability returns `true`. Lookup errors fail closed so an issued M7 RFQ cannot be reopened when issuance state is uncertain.

M8 never writes to:

- `rfqs`;
- `rfq_counters`;
- `material_requirements`;
- Supplier Directory records owned by M7.

### 2.1A Widened M7 handoff projection (Revision 3)

Implementation discovered that the M7 projection omitted identifiers
`IssuedRFQLine` requires: copy-forward matches on `SourceMaterialRequirementID`
(approved decision 11), and award traceability references the originating
requirement, but neither field — nor `MaterialID`, nor the RFQ `Title` — was
reachable through `GetReadyRFQSnapshot`.

This is an **approved narrow, additive M7 amendment**. It exposes internal
identifiers only. It does not weaken the supplier-visible allowlist.

```go
type RFQLineSnapshot struct {
    LineID                      string
    SourceMaterialRequirementID string
    MaterialID                  string
    MaterialName                string
    Specification               string
    QuantityValue               string
    QuantityUnit                string
    RequiredByDate              *time.Time
    ProcurementNotes            string
    SortOrder                   int
}
```

```go
func (s *Service) GetReadyRFQSnapshot(
    ctx context.Context,
    companyID string,
    rfqChainID string,
) (
    rfqNumber string,
    projectID string,
    revision int64,
    title string,
    deliveryAddress string,
    requiredBy *time.Time,
    responseDeadline *time.Time,
    supplierInstructions string,
    lines []RFQLineSnapshot,
    found bool,
    err error,
)
```

`revision` is the exact ready M7 RFQ revision persisted as
`IssuedRFQVersion.SourceM7RFQRevision`. It is provenance for source-identity,
fingerprint, audit, and reconciliation checks; it is not exposed on a Supplier
projection.

The projection must **never** gain:

- `InternalNotes`;
- costs;
- prices;
- margins;
- preferred-Supplier data;
- claim metadata unnecessary to issuance.

Those fields remain structurally absent, so the privacy boundary stays a
property of the type rather than of remembering to omit them.

---

## 3. Core aggregates and identities

### 3.1 RFQ issuance chain

```go
type RFQIssuanceChain struct {
    ID                     string
    CompanyID              string
    RFQChainID             string
    LatestIssuedVersion    int
    CurrentIssuedVersionID *string
    Revision               int64
    CreatedAt              time.Time
    UpdatedAt              time.Time
    SchemaVersion          int
}
```

Unique invariant:

```text
companyId + rfqChainId
```

The issuance chain is the serialization point for version creation.

### 3.2 Immutable issued RFQ version

```go
type IssuedRFQVersion struct {
    ID             string
    CompanyID      string
    ProjectID      string
    RFQChainID     string
    RFQNumber      string
    VersionNumber  int
    Currency       string

    Title                string
    DeliveryAddress      string
    RequiredByDate       *time.Time
    ResponseDeadline     time.Time
    SupplierInstructions string

    Lines []IssuedRFQLine

    SourceM7RFQRevision int64
    SourceFingerprint   string
    IssuedByUserID      string
    IssuedAt            time.Time
    SchemaVersion       int
}
```

> **`IssuedRFQLine` superseded by §3.2A (Revision 3).** The shape below lacked a
> stable cross-version line identity and treated M7 provenance as mandatory,
> which an M8-native amendment line cannot satisfy.

```go
type IssuedRFQLine struct {
    ID                          string
    SourceMaterialRequirementID string
    MaterialID                  string
    MaterialName                string
    Specification               string
    Quantity                    quantity.Quantity
    RequiredByDate              *time.Time
    ProcurementNotes            string
    SortOrder                   int
}
```

Issued versions are immutable. They contain no contractor-only notes, estimated cost, indicative price, margin, preferred Supplier data, or competing Supplier information.

### 3.2A Issued line identity and lineage (Revision 3)

An issued line carries **four** identifiers with distinct meanings:

```go
type IssuedRFQLine struct {
    ID                          string
    LineageID                   string
    SourceM7RFQLineID           *string
    SourceMaterialRequirementID *string

    MaterialID       string
    MaterialName     string
    Specification    string
    Quantity         quantity.Quantity
    RequiredByDate   *time.Time
    ProcurementNotes string
    SortOrder        int
}
```

| Identifier | Meaning |
|---|---|
| `ID` | the exact line record inside **one** immutable issued version |
| `LineageID` | stable logical line identity **across** M8 versions |
| `SourceM7RFQLineID` | originating M7 RFQ line, when applicable |
| `SourceMaterialRequirementID` | originating Material Requirement, when applicable |

The M7 line ID is **never** reused as the issued-line ID. Reusing it would
conflate V1's and V2's records for the same logical line, and a Supplier Offer
must reference the exact line in the exact version it quoted against.

**Version 1** (from an M7 ready RFQ):

```text
ID                          = newly generated, unique to Version 1
LineageID                   = newly generated stable lineage ID
SourceM7RFQLineID           = M7 RFQLineSnapshot.LineID
SourceMaterialRequirementID = M7 RFQLineSnapshot.SourceMaterialRequirementID
```

**A cloned amendment line** (V(N) → V(N+1)):

```text
ID                          = NEW per-version ID
LineageID                   = UNCHANGED
SourceM7RFQLineID           = UNCHANGED
SourceMaterialRequirementID = UNCHANGED
```

**An M8-native amendment line** — added in a draft, with no M7 provenance:

```text
ID                          = new per-version ID
LineageID                   = new stable lineage ID
SourceM7RFQLineID           = nil
SourceMaterialRequirementID = nil
MaterialID                  = required
```

#### Copy-forward compatibility

```text
When SourceMaterialRequirementID exists:
    match by SourceMaterialRequirementID

For M8-native lines:
    match by LineageID
```

This preserves approved decision 11 — M7-backed lines match by Material
Requirement, never by Material ID alone, so two lines for the same Material
under different requirements never cross-match — while giving M8-native lines a
matching rule that does not weaken it.

Lineage identifiers exist for **traceability and copy-forward only**. Offer and
award records must still reference the exact issued `RFQLineID`; a lineage ID is
never a substitute for exact-version identity.

Unique invariant:

```text
companyId + rfqChainId + versionNumber
```

### 3.3 RFQ amendment draft

One mutable amendment draft may exist per issuance chain.

```go
type RFQAmendmentDraft struct {
    ID                    string
    CompanyID             string
    RFQChainID            string
    BaseIssuedVersionID   string
    BaseVersionNumber     int
    Currency              string
    Header                RFQVersionHeaderDraft
    Lines                 []RFQVersionLineDraft
    Revision              int64
    CreatedByUserID       string
    CreatedAt             time.Time
    UpdatedAt             time.Time
    SchemaVersion         int
}
```

The draft clones the latest immutable version. A deadline-only extension also creates and issues a new immutable version rather than mutating an issued version.

Revision 4 fixes the amendment PATCH trust boundary. `lines` is an optional
complete replacement: omitting it leaves lines unchanged and an empty array
removes all lines. An existing draft-line `ID` is accepted only as a lookup key;
the server preserves that line's `ID`, `LineageID`, and M7 provenance. A line
without an `ID` is an M8-native addition and receives a new per-version `ID` and
stable `LineageID`; clients cannot submit lineage or provenance identifiers.

### 3.4 Stable Supplier Invitation

One stable invitation exists per:

```text
Company + RFQChain + Supplier
```

```go
type SupplierInvitation struct {
    ID                        string
    CompanyID                 string
    RFQChainID                string
    SupplierID                string
    CurrentIssuedRFQVersionID string

    RecipientName             string
    RecipientEmail            string
    RecipientEmailNormalized  string

    Status                    InvitationStatus
    ExpiresAt                 time.Time
    RevokedAt                 *time.Time

    AccessSecretHash          string
    AccessGeneration          int64
    SecretKeyVersion          int

    FirstViewedAt             *time.Time
    LastViewedAt              *time.Time

    Revision                  int64
    CreatedByUserID           string
    CreatedAt                 time.Time
    UpdatedAt                 time.Time
    SchemaVersion             int
}
```

`SecretKeyVersion` records which keyring version derived the current secret, so an
invitation's link stays reproducible after the active key version advances
(§6.1A). The raw derived token is **never** persisted.

`FirstViewedAt` and `LastViewedAt` are Revision 2 view-tracking fields (§1A.4,
§6.5). They are not lifecycle states.

Suggested statuses:

```text
draft
active
expired
revoked
```

Expiry is time-derived and may be represented either as a computed state or persisted transition, but access checks must always compare `ExpiresAt` to current time.

The invitation advances its current version pointer; it never overwrites an issued version or submitted offer.

Revision 5 permits repository-private `lastReactivation` idempotency metadata
containing only `operationId`, `expectedRevision`, and the requested expiry.
It is not part of the contractor DTO and contains no raw token, hash, or link.

### 3.5 Invitation delivery

```go
type InvitationDeliveryAttempt struct {
    ID                    string
    CompanyID             string
    InvitationID          string
    DeliveryOperationID   string
    AccessGeneration      int64
    DeliveryChannel       DeliveryChannel
    RecipientEmail        string
    IssuedRFQVersionID    string
    Status                DeliveryStatus
    FailureCode           string
    RequestedByUserID     string
    RequestedAt           time.Time
    SentAt                *time.Time
    DeliveredAt           *time.Time
    SchemaVersion         int
}
```

Delivery channel (Revision 2, §1A.4):

```text
email
copy_link
```

`copy_link` represents the contractor taking the raw link to send manually
through WhatsApp or any other channel. A `copy_link` attempt performs no mail
send and is recorded as `sent` at creation; `FailureCode` is never populated for
it. `RecipientEmail` is still snapshotted so the attempt records who the link was
intended for.

`FailureCode` is a **bounded** module-owned code (Revision 2, §1A.2). Raw provider
error text and credentials are never persisted.

Delivery status:

```text
pending
sent
failed
delivered
obsolete
```

An attempt becomes `obsolete` if the recipient or access generation changes before delivery.

### 3.6 Supplier access identity and session

Identity scope:

```text
Company + Supplier + normalised recipient email
```

```go
type SupplierAccessExchange struct {
    ID                       string
    ExchangeTokenHash        string
    CompanyID                string
    SupplierID               string
    InvitationID             string
    AccessGeneration         int64
    NormalizedRecipientEmail string
    CreatedAt                time.Time
    ExpiresAt                time.Time
    ConsumedAt               *time.Time
    ChallengeID              *string
}
```

```go
type EmailVerificationChallenge struct {
    ID                       string
    ExchangeID               string
    CompanyID                string
    SupplierID               string
    RecipientEmailNormalized string
    InvitationID             string
    AccessGeneration         int64
    ChallengeOperationID     string
    CodeVerifier             string
    CodeKeyVersion           int
    AttemptsRemaining        int
    ExpiresAt                time.Time
    SupersededAt             *time.Time
    LockedAt                 *time.Time
    ConsumedAt               *time.Time
    ConsumedOperationID      string
    SupplierSessionID        string
    TargetTokenGeneration    int64
    SessionTokenKeyVersion   int
    Revision                 int64
    CreatedAt                time.Time
    SchemaVersion            int
}
```

```go
type VerificationDeliveryAttempt struct {
    ID          string
    CompanyID   string
    ChallengeID string
    OperationID string
    Status      string
    FailureCode *string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

```go
type VerificationRateLimitState struct {
    ID           string
    Scope        VerificationRateLimitScope
    ScopeKeyHash string
    Reservations []VerificationRateReservation
    Revision     int64
    UpdatedAt    time.Time
    ExpiresAt    time.Time
}

type VerificationRateReservation struct {
    ReservationID string
    RequestedAt   time.Time
}
```

```go
type SupplierSession struct {
    ID                       string
    CompanyID                string
    SupplierID               string
    RecipientEmailNormalized string
    TokenHash                string
    TokenGeneration          int64
    TokenKeyVersion          int
    CreatedFromChallengeID   string
    LastVerifiedChallengeID  string
    LastVerifiedAt           time.Time
    SlidingExpiresAt         time.Time
    AbsoluteExpiresAt        time.Time
    LastUsedAt               time.Time
    RevokedAt                *time.Time
    Revision                 int64
    SchemaVersion            int
}
```

The session is not itself tied to one invitation. Invitation access additionally
requires this `supplieraccess`-owned binding:

```go
type SupplierSessionInvitationBinding struct {
    ID                       string
    CompanyID                string
    SupplierSessionID        string
    SupplierID               string
    NormalizedRecipientEmail string
    InvitationID             string
    AccessGeneration         int64
    BoundAt                  time.Time
    LastValidatedAt          time.Time
    Revision                 int64
}
```

The immutable session identity and the binding identity must both match the
current invitation. See Revision 7, §1A.6.

### 3.7 Supplier Offer chain

One offer chain exists per:

```text
Invitation + Issued RFQ Version
```

```go
type SupplierOfferChain struct {
    ID                     string
    CompanyID              string
    InvitationID           string
    IssuedRFQVersionID     string
    LatestSubmittedVersion int
    LatestSubmittedID      *string
    Revision               int64
    CreatedAt              time.Time
    UpdatedAt              time.Time
    SchemaVersion          int
}
```

### 3.8 Supplier Offer draft

At most one active draft exists per offer chain.

```go
type SupplierOfferDraft struct {
    ID                     string
    CompanyID              string
    OfferChainID           string
    InvitationID           string
    IssuedRFQVersionID     string
    RecipientIdentity      string
    SourceOfferVersionID   *string
    Lines                  []SupplierOfferDraftLine
    Tax                    SupplierOfferTaxDraft
    ChargeGroups           []SupplierChargeGroupDraft
    OfferValidUntil        *time.Time
    SupplierNotes          string
    Revision               int64
    CreatedAt              time.Time
    UpdatedAt              time.Time
    ArchivedAt             *time.Time
    SchemaVersion          int
}
```

A replacement recipient may start blank or optionally copy a previous offer. Copying records `SourceOfferVersionID` but creates a new draft owned by the new recipient identity.

### 3.9 Immutable Supplier Offer version

```go
type SupplierOfferVersion struct {
    ID                    string
    CompanyID             string
    OfferChainID          string
    InvitationID          string
    IssuedRFQVersionID    string
    VersionNumber         int
    Currency              string
    RecipientIdentity     string
    SubmissionOperationID string
    ContentFingerprint    string

    Lines                  []SupplierOfferLine
    Tax                    SupplierOfferTax
    ChargeGroups           []SupplierChargeGroup

    QuotedLineSubtotal     money.Money
    QuotedTaxTotal         money.Money
    FullOfferChargeTotal   money.Money
    GrandTotal             money.Money

    OfferValidUntil        time.Time
    SupplierNotes          string

    SubmittedAt            time.Time
    SchemaVersion          int
}
```

Submitted versions are immutable. Withdrawal is represented separately.

### 3.10 Offer withdrawal

```go
type SupplierOfferWithdrawal struct {
    ID                     string
    CompanyID              string
    SupplierOfferVersionID string
    InvitationID           string
    RecipientIdentity      string
    Reason                 string
    WithdrawnAt            time.Time
    SchemaVersion          int
}
```

Only one withdrawal may exist per offer version.

### 3.11 Award decision chain and draft

One award chain exists per issued RFQ version.

```go
type AwardDecisionChain struct {
    ID                     string
    CompanyID              string
    IssuedRFQVersionID     string
    CurrentAwardRevisionID *string
    LatestRevisionNumber   int
    Revision               int64
    CreatedAt              time.Time
    UpdatedAt              time.Time
    SchemaVersion          int
}
```

```go
type AwardDraft struct {
    ID                 string
    CompanyID          string
    AwardChainID       string
    IssuedRFQVersionID string
    LineDecisions      []AwardLineDecisionDraft
    Revision           int64
    CreatedByUserID    string
    CreatedAt          time.Time
    UpdatedAt          time.Time
    SchemaVersion      int
}
```

Each RFQ line must be either provisionally selected or carry an explicit unawarded reason.

### 3.12 Immutable award revision

```go
type AwardRevision struct {
    ID                      string
    CompanyID               string
    AwardChainID            string
    IssuedRFQVersionID      string
    RevisionNumber          int
    FinalisationOperationID string
    ChangeReason            string

    AwardedLines            []AwardedLine
    UnawardedLines          []UnawardedLine
    SupplierSummaries       []AwardSupplierSummary
    GrandAwardTotal         money.Money

    FinalisedByUserID       string
    FinalisedAt             time.Time
    SchemaVersion           int
}
```

Each awarded line references the exact:

```text
Issued RFQ line
Supplier
Invitation
Supplier Offer Version
Supplier Offer line
Conditional charge rules used
```

A later award revision supersedes but never mutates the prior revision.

---

## 4. RFQ issuance lifecycle

### 4.1 Version 1

```text
M7 ready RFQ
→ issue
→ immutable Issued RFQ Version 1
```

Rules:

- read the M7 ready RFQ through an `rfqissuance`-owned capability;
- require the M7 RFQ to be ready;
- set the one Phase 1 supported currency, `MYR` (case-insensitive input is
  normalized to uppercase);
- snapshot only the supplier-visible allowlist;
- allocate Version 1 atomically through the issuance chain;
- create no invitation automatically;
- send no email automatically.

Issuance fails closed when:

- the M7 RFQ is not ready;
- the M7 RFQ has no `ResponseDeadline` (§4.1A);
- issuance status cannot be determined;
- another issuance wins concurrently;
- currency validation fails;
- the operation ID belongs to another logical issuance.

### 4.1A Response deadline is required at issuance (Revision 3)

`IssuedRFQVersion.ResponseDeadline` is a concrete `time.Time`, while the M7
model treats the deadline as optional (`*time.Time`) and permits an RFQ to
become ready without one.

The stricter rule belongs to the **external issuance boundary**, where the
Supplier response window becomes commercially operative:

```text
A ready M7 RFQ without ResponseDeadline cannot be issued.

422 ErrRFQResponseDeadlineRequired
```

M8 must **not** invent a commercial deadline. M7 remains unchanged and may still
mark such an RFQ ready; only issuance refuses. A refused issuance creates no
issuance chain and no version.

### 4.2 Amendment versions

```text
Issued Version N
→ create amendment draft
→ edit draft
→ issue
→ immutable Version N+1
```

Only one amendment draft may exist per chain.

Issuing:

1. validates the draft and expected revision;
2. verifies the base version is still the current version;
3. derives the one candidate `LatestIssuedVersion + 1` from the chain snapshot;
4. creates the immutable version, whose named company/chain/version unique
   index atomically selects the winner;
5. advances the issuance chain only after that immutable version exists;
6. advances every non-revoked invitation;
7. records invitation advancement failures for reconciliation.

An invitation that temporarily remains on an earlier version continues exposing that complete earlier version. It never points to a partially created version.

---

## 5. Invitation lifecycle

```text
draft
→ explicit send
→ active
```

While active, the contractor may:

- resend;
- update expiry;
- replace recipient;
- rotate secret;
- advance it to a newer RFQ version;
- revoke it.

### 5.1 Creation

Creation validates:

- the issuance chain and current issued version;
- the Supplier exists, belongs to the company, and is active;
- recipient name and email;
- expiry is in the future;
- unique Company + RFQChain + Supplier invariant.

Creation does not contact the Supplier.

### 5.2 Send and resend

`send` persists a delivery intent before queueing email.

`resend`:

- reuses the stable invitation;
- normally reuses the current secret;
- creates a new delivery attempt;
- does not change invitation identity.

A failed email never rolls back the invitation.

### 5.3 Recipient replacement

Recipient replacement:

- requires `expectedRevision`;
- snapshots the new name and email;
- rotates the raw secret and persists only its hash;
- increments `AccessGeneration`;
- invalidates old invitation access;
- archives an unfinished draft owned by the previous recipient, through the
  durable claim barrier and ordering **§5.3A** defines;
- preserves submitted offer versions and original attribution.

The new recipient verifies separately. They may start blank or optionally copy a previous offer into a new draft.

### 5.3A Recipient-replacement ordering and the durable claim barrier

> **Revision 16:** This section resolves the cross-collection ordering §5.3 left
> open between the authoritative invitation replacement in `rfqissuance` and
> draft archival in `supplieroffers`. Where it conflicts with §5.3, this takes
> precedence.

Ordering is precondition-first, but **direct archival is not sufficient**.
Archiving releases the unfinished-draft uniqueness slot immediately, which
leaves two races:

- an unguarded "no active draft" check succeeds, the previous recipient then
  creates a draft, and replacement proceeds — leaving a live draft owned by a
  recipient who is no longer authoritative;
- a crash after archival but before invitation replacement leaves the previous
  recipient still authoritative and free to create another draft.

The approved sequence is therefore:

1. supplier-offer **replacement claim** (a durable barrier);
2. authoritative **invitation replacement**;
3. final **archival**.

The claim must keep occupying the active-draft slot until the authoritative
invitation write is confirmed. It must never be implemented as a separate
count or existence check followed by an unrelated insert.

#### The claimed draft state

`recipient_replacement_claimed` is a draft status covered by the **same** named
partial unique index that prevents a second unfinished draft. The occupied-slot
states are `active`, `submitting` and `recipient_replacement_claimed`;
`archived` never occupies the slot.

A claim records:

- `ReplacementOperationID`
- `PreviousRecipientIdentity`
- `CandidateRecipientIdentity`
- `InvitationID`
- `IssuedRFQVersionID`
- `ClaimedAt`
- `Revision`

A `DraftPurpose` discriminator separates commercial drafts from barrier-only
rows so ordinary draft logic can never mistake a barrier for offer content:

- `DraftPurpose = recipient_replacement_barrier` requires
  `Status = recipient_replacement_claimed` and **empty** commercial fields — it
  fabricates no lines, prices, taxes, delivery charges, validity dates or
  supplier notes;
- `DraftPurpose = commercial` follows the normal
  `active`/`submitting`/`archived` lifecycle.

#### Existing active draft

Atomically transition `active → recipient_replacement_claimed`. This CAS
competes directly with `active → submitting`, so:

- submission wins first — replacement receives `409`;
- replacement wins first — submission and editing receive `409`.

A `submitting` draft blocks replacement with `409`.

#### No existing draft

Insert a barrier-only claimed row under the same workspace identity and the
same named unique index that ordinary draft creation contends on. MongoDB
selects one winner atomically:

- barrier inserted first — draft creation receives a conflict;
- active draft inserted first — the barrier insert receives a conflict.

On conflict, reload the workspace row. If it belongs to the same replacement
operation, resume it. If it is an `active` or `submitting` commercial draft from
another operation, return `409`.

**The guarantee is state-based, not call-ordering-based** (approved 2026-07-31).
Draft creation may win the insert and the existing-draft CAS may then claim that
just-created draft, so both calls return success. That outcome is correct and
permitted: exactly one workspace exists and it is `recipient_replacement_claimed`,
so the previous recipient can neither edit nor submit. MongoDB cannot distinguish
a pre-existing draft from one created microseconds earlier without a fencing
token, and adding one would buy no additional safety — the previous recipient
already has no usable workspace either way.

What must never happen is a **successful claim leaving an `active` workspace**,
or a second workspace row existing. Both are prevented by the shared named
unique index and the status-guarded CAS.

#### Authoritative invitation replacement

Only after the offer-side claim succeeds may `rfqissuance` replace the recipient
in **one atomic write** that:

- requires the expected invitation revision;
- requires the exact previous normalized recipient;
- increments `AccessGeneration`;
- adopts the active secret-key version;
- persists the new secret hash;
- replaces the recipient snapshot;
- obsoletes only older pending deliveries as §5.3 already specifies.

The recipient snapshot and the secret rotation are a single conditional write.
Performing them as two sequential CAS operations is non-conforming: it leaves a
window in which the recipient has changed while the previous recipient's link
still resolves.

#### Failure after the claim

If the invitation write fails for infrastructure reasons, return a bounded
response and **keep the claim**:

```http
HTTP/1.1 503 Service Unavailable
Retry-After: 2

{ "code": "recipient_replacement_pending" }
```

No database or network detail is exposed. An infrastructure error does not prove
the invitation write did not land, so releasing the barrier is unsafe: the write
may have succeeded with a lost response, and an immediate release would hand
draft access back to a recipient who has already been replaced. The previous
recipient remains unable to edit, create or submit while the claim is pending.

#### Completion

After the authoritative invitation replacement is confirmed:
`recipient_replacement_claimed → archived`, preserving:

- `ArchivedReason = recipient_replacement`
- `ArchivedByOperationID`
- `PreviousRecipientIdentity`
- `ReplacementRecipientIdentity`
- `ArchivedAt`

A barrier-only row is archived as replacement-coordination history. It never
becomes an editable draft and never appears in ordinary draft listings. The
replacement recipient does **not** inherit the archived draft; they start blank
or explicitly copy a compatible immutable Offer Version.

#### Recovery

A same-operation retry must:

1. find the existing replacement claim;
2. verify the previous and candidate recipient identities;
3. determine whether invitation replacement already completed;
4. complete any missing invitation write;
5. finalize the draft or barrier as archived;
6. return idempotently.

A different operation ID cannot take over the claim and receives `409` while the
claim is unresolved. Claims **never** auto-release after a timeout.
Reconciliation decides from authoritative records whether to complete or abort,
and completes the replacement whenever anything is uncertain. An abort is
permitted only when all of the following hold:

- the exact operation owns the claim;
- the invitation still has the original recipient and generation, so replacement
  definitely did not occur;
- no submission or immutable Offer Version was created;
- the rollback CAS still matches the claim revision.

#### Consumer-owned capability

`rfqissuance`, as the consumer, declares a narrow capability covering
`PrepareRecipientReplacement`, `CompleteRecipientReplacement` and
`AbortRecipientReplacement`. `supplieroffers` implements it through the
composition adapter. There is no cross-module repository access.

### 5.4 Expiry and revocation

Expiry and revocation are separate:

- expiry is controlled by `ExpiresAt`;
- revocation blocks access immediately;
- extending expiry requires a revision-guarded contractor action;
- revocation preserves all historical data;
- resending does not reactivate a revoked invitation;
- revocation is terminal for the current access generation, not for the stable
  invitation identity;
- reactivation is a separate owner/admin operation accepting `expectedRevision`,
  a new future `ExpiresAt`, and `operationId`;
- only an explicitly revoked or effectively expired invitation is eligible;
  an already-active, unexpired invitation returns 409 without mutation;
- the authoritative company-scoped, revision-guarded write atomically increments
  `AccessGeneration` exactly once, adopts the active `SecretKeyVersion`, persists
  the new HMAC-token hash, sets status `active`, sets the new expiry, clears
  `RevokedAt`, advances `CurrentIssuedRFQVersionID` to the chain's latest issued
  version, records the operation identity, and increments the invitation revision;
- the same operation ID resolves the same transition without another generation
  increment, while concurrent different operations have exactly one winner;
- after the authoritative write, only older-generation `pending` delivery
  attempts become `obsolete`; `sent`, `failed`, and already-`obsolete` attempts
  remain historical facts;
- reactivation records a primitive-only audit event, but neither sends email nor
  creates a delivery attempt;
- every previous raw link remains permanently invalid, and Phase D sessions and
  bindings remain generation-bound so reactivation invalidates their older
  generation.

---

## 6. Supplier access security

### 6.1 Invitation secrets

> **Superseded by §6.1A (Revision 2).** Revision 1 specified a randomly generated
> secret. A random secret exists only in memory at generation time and cannot be
> reproduced, which makes the approved copy-link action (§1A.4) — return the
> current link *without* rotating — unsatisfiable. §6.1A replaces the generation
> mechanism with deterministic HMAC derivation. Every property below still holds.

The raw secret:

- appears only in the generated URL, the mail payload, and the copy-link response;
- is never stored;
- is never logged;
- is never written to audit metadata;
- is rotated for recipient replacement or suspected compromise.

### 6.1A Versioned HMAC invitation-secret derivation

The invitation secret is **derived deterministically** from a versioned
server-side keyring. No raw secret and no reversible secret material is ever
persisted.

#### Configuration

```text
INVITATION_SECRET_ACTIVE_VERSION=1
INVITATION_SECRET_KEY_V1=<base64-encoded random key>
```

Rules:

- the decoded key must be **at least 32 random bytes**;
- a missing, malformed or undersized key **fails application startup**;
- tests inject fixed keys;
- local development uses an explicitly configured **stable** key;
- a key is **never** auto-generated at startup — existing links would stop being
  reproducible.

Configuration is structured so further versions can be added later:

```text
INVITATION_SECRET_KEY_V2=...
INVITATION_SECRET_ACTIVE_VERSION=2
```

An old key must **remain configured** while any invitation still references its
version.

#### Derivation

The canonical input is **domain-separated and length-prefixed**. Delimiter
concatenation is forbidden — it would let two different field tuples produce the
same input.

Canonical input fields, in order:

```text
"m8-invitation-secret-v1"
SecretKeyVersion
CompanyID
InvitationID
AccessGeneration
```

Then:

```text
rawBytes = HMAC-SHA256(keyForVersion, canonicalInput)
rawToken = base64.RawURLEncoding(rawBytes)
```

#### Persisted fields

Only these are persisted on the invitation:

```text
AccessSecretHash
AccessGeneration
SecretKeyVersion
```

`AccessSecretHash` is the one-way SHA-256 hash of the **encoded token**,
consistent with the existing `access.HashAccessToken` convention. The raw derived
token is never persisted.

#### Creation

1. Use the currently active key version.
2. Derive the token.
3. Persist its hash, generation and key version.
4. Return the raw link once where required.

#### Ordinary copy-link

1. Load the invitation's current `SecretKeyVersion` and `AccessGeneration`.
2. Re-derive the same token.
3. Hash the result and compare with `AccessSecretHash`.
4. **Fail closed** if they differ.
5. Return the raw link without changing the invitation.

Copy-link must not increment `AccessGeneration`, rotate the link, or persist the
raw token.

#### Explicit rotation and recipient replacement

Both operations:

- increment `AccessGeneration`;
- use the **currently active** `SecretKeyVersion`;
- derive a new token;
- persist the new token hash and key version **atomically**;
- invalidate prior invitation-generation bindings.

Key upgrades therefore reach newly rotated links naturally, while existing links
stay reproducible through their recorded key version.

#### Verification

For a presented token:

- require the canonical base64url encoding and exact bounded length produced by
  the 256-bit HMAC derivation before any repository call;
- hash it in memory and resolve globally by that canonical hash, without a
  caller-provided tenant or invitation identifier;
- hash it and compare against `AccessSecretHash` using **constant-time**
  comparison;
- require the **exact current** `AccessGeneration`;
- enforce invitation expiry and revocation;
- never reveal whether failure came from the token, the generation, the recipient
  or the invitation state.

The resolver receives only the computed hash. A successful lookup derives the
tenant, Supplier, invitation and recipient identity from the stored invitation.
The raw token is then discarded from the browser-visible location before the
verification flow continues. See Revision 6, §1A.5.

#### Operational security trade-off

Compromise of an invitation-secret key has a **wider blast radius** than
compromise of one random invitation token: it forges every link derivable from
that key version. Accepted mitigations:

- keep keys outside MongoDB;
- never log or audit them;
- support key versions;
- rotate the active key when compromised;
- increment affected invitations' access generations to invalidate old links.

Encrypted-at-rest raw invitation secrets are **not** implemented.

### 6.1B Browser access exchange

The invitation token is accepted only by the clicked-link GET. A successful
open creates the 10-minute, single-use `SupplierAccessExchange` defined in
Revision 8, §1A.7, sets its raw token in the dedicated HttpOnly cookie, and
redirects to the clean path.

The exchange proves only that the browser recently opened one exact invitation
generation. It cannot authorize Supplier data access. Challenge creation
revalidates every exchange identity field and invitation state before consuming
it. The exchange cookie is cleared after successful challenge creation, expiry,
or invalid exchange detection.

### 6.2 Email verification

A challenge is:

- deterministically derived from the dedicated cryptographic keyring;
- stored only as a keyed verifier and key version;
- valid for exactly 10 minutes;
- single-use;
- limited to five failed confirmations;
- rate-limited by invitation, Supplier identity, recipient email, and client address;
- invalidated when a newer challenge is issued.

Codes are exactly six ASCII digits with leading zeroes allowed. Confirmation
enforces the Revision 9 keyed-verifier and atomic-attempt rules. Challenge
requests enforce a 60-second per-generation cooldown and five creations per
rolling hour under the approved composite identity key, plus 20 per rolling hour
under the trusted-proxy-aware keyed client-address hash. Delivery and retry use
`VerificationDeliveryAttempt`; retry never changes the code, expiry, or attempt
budget.

Public responses do not reveal whether a Supplier, recipient email, invitation, or challenge exists.

### 6.3 Supplier session

Successful verification creates a restricted Supplier session:

```text
Sliding inactivity expiry: 30 days
Absolute re-verification limit: 90 days
```

Activity may renew only the sliding expiry. After 90 days, email verification is required again.

Renewal reissues the browser cookie with the updated sliding lifetime and never
extends past the absolute expiry. Re-verification of the matching presented
session rotates its token generation and resets both limits; another browser
creates a separate session.

Cookie requirements outside local development:

```text
HttpOnly
Secure
SameSite=Lax
Path restricted to Supplier access routes
```

Persist only a token hash.

Phase D also issues the non-HttpOnly `supplier_csrf` cookie. Every protected
Supplier mutation uses the double-submit cookie/header comparison plus the
server-derived, session-generation-bound expected value from Revision 12,
§1A.11.

Access to an invitation requires:

- session token valid;
- session not revoked;
- sliding and absolute expiries valid;
- Company and Supplier match;
- normalised recipient email matches;
- a session/invitation binding exists;
- the binding Company, Supplier and recipient identity match the session;
- invitation `AccessGeneration` is current;
- invitation not revoked or expired.

A valid identity session may access multiple invitations for the same Company +
Supplier + recipient email only when each invitation has its own current
generation binding. A session alone confers no invitation access.

### 6.4 Supplier route boundary

Supplier routes are mounted separately from contractor JWT routes.

Public/Supplier requests never accept authoritative CompanyID or SupplierID. These are derived from the verified session and invitation.

Supplier-visible projections must never include:

- contractor internal notes;
- Material Requirement internal notes;
- M7 indicative Supplier Offering price;
- estimated, committed, actual, or paid costs;
- margins or mark-ups;
- preferred Supplier data;
- competing Suppliers;
- competing offers;
- comparison results;
- provisional selections;
- internal award reasoning.

### 6.5 Invitation view tracking

`FirstViewedAt` and `LastViewedAt` (§3.4) record whether the Supplier opened the
secure invitation. They are **timestamps, not lifecycle states** — the invitation
lifecycle remains `draft / active / expired / revoked`.

`supplieraccess` observes the successful open, but `rfqissuance` owns the
invitation record. `supplieraccess` therefore declares a consumer-owned
capability, satisfied by `rfqissuance.Service`:

```go
type InvitationViewRecorder interface {
    RecordInvitationViewed(
        ctx context.Context,
        companyID string,
        invitationID string,
        accessGeneration int64,
        viewedAt time.Time,
    ) error
}
```

`accessGeneration` is part of the contract, not an afterthought. `rfqissuance`
conditionally updates on:

```text
companyId + invitationId + accessGeneration
```

Without the generation term, a successfully validated **old-generation** request
racing a recipient replacement could mark the replacement recipient's current
invitation as viewed.

Update semantics:

```text
FirstViewedAt = earliest successful valid-link open
LastViewedAt  = latest successful valid-link open
```

Use atomic `$min` / `$max` (or the repository equivalent) so retries and
concurrent opens stay safe and order-independent.

A view is recorded **only after** the invitation secret and generation have
validated successfully. Email verification is **not** required for a secure
invitation open to count. Failed, expired, revoked or obsolete-generation
attempts must not update either timestamp.

`supplieraccess` owns the interface definition. A composition adapter is used if
sentinel translation is required; otherwise a primitive-only structural
implementation is acceptable. No cross-module repository access is permitted.

---

## 7. Supplier Offer behaviour

### 7.1 Draft lifecycle

```text
no draft
→ create blank or optional copy-forward
→ one active draft
→ submit
→ immutable offer version
```

Draft edits use optimistic concurrency.

Copy-forward compatibility is based on `SourceMaterialRequirementID`.

For a compatible line:

- supplier-entered commercial fields may be copied;
- the new RFQ version supplies the authoritative quantity, unit, specification, and dates;
- changed RFQ fields are flagged for review;
- new lines start unanswered;
- removed lines remain visible only in history;
- `no_bid` and `unavailable` require fresh confirmation.

### 7.2 Explicit line responses

Every issued RFQ line must have one response:

```text
quoted
no_bid
unavailable
```

Partial offers are allowed because some lines may be `no_bid` or `unavailable`.

A `quoted` response must cover the full requested quantity and exact unit. M8 does not support quoting a partial quantity.

### 7.3 Line pricing

Quoted line fields include:

```go
type SupplierOfferLine struct {
    RFQLineID                    string
    SourceMaterialRequirementID  string
    ResponseStatus               OfferLineResponseStatus

    QuotedQuantity               quantity.Quantity
    UnitPriceExcludingTax        money.Money
    LineSubtotalExcludingTax     money.Money

    Brand                        string
    SKU                          string
    ProductDescription           string
    LeadTime                     string
    SupplierLineNotes            string
    CommercialExceptions         string

    LineTax                      *QuotedLineTax
}
```

All calculations use `foundation/money.Money`, exact quantities, and the project’s established rounding rules. Floating-point arithmetic is forbidden.

### 7.4 Tax model

Each submitted offer selects exactly one mode:

```text
not_applicable
line_level
offer_level
```

All tax is labelled **quoted or estimated tax**.

The platform does not determine whether the Supplier is legally taxable. The later invoice or validated e-Invoice is authoritative.

#### Line-level mode

Each quoted line declares:

- tax type: `service_tax`, `sales_tax`, or `other`;
- tax rate or exempt status;
- tax registration number when applicable.

The server calculates tax per line and sums the rounded line tax amounts.

#### Offer-level mode

The Supplier enters:

- tax type;
- one quoted tax amount;
- tax registration number when applicable;
- required tax basis note.

No line-level rates may be present in this mode.

#### Not applicable

No tax rate or tax amount may be present.

### 7.5 Delivery and conditional charges

The Supplier defines structured charge groups.

Supported triggers:

```text
any_selected
all_selected
selected_subtotal_at_least
```

Supported calculations:

```text
fixed_amount
percentage_of_selected_subtotal
```

Each charge group identifies the applicable RFQ line IDs.

Rules:

- referenced lines must belong to the same offer version;
- ambiguous overlapping rules that could double-charge a selection are rejected;
- percentage charges apply to the selected subtotal of the group;
- the platform never invents, allocates, or prorates a Supplier charge;
- submitted rules are immutable.

When a proposed selected subset does not produce a valid deterministic charge result, that provisional award selection is invalid.

### 7.6 Submission

Submission requires:

- invitation active and not expired;
- response deadline open;
- verified recipient identity and access generation match;
- active draft revision match;
- every line explicitly answered;
- quoted lines complete and currency-consistent;
- tax and charge rules valid;
- `OfferValidUntil` later than submission time.

The server recalculates every total. Browser-supplied totals are ignored.

Submission creates an immutable version and archives or clears the active draft.

### 7.7 Revision, withdrawal, and expiry

A new offer revision starts a new draft and creates another immutable version on submission.

A submitted version may be withdrawn:

- withdrawal is immutable and auditable;
- the submitted version remains unchanged;
- the version becomes ineligible for selection;
- a replacement may be submitted while the response window is open.

Every submitted version requires `OfferValidUntil`.

An expired version remains readable but is not selectable. Renewal requires a new immutable offer version.

---

## 8A. Phase F consolidated contract (Revision 20)

> **Revision 18.** This section is the implementation contract for Phase F.
> Where it conflicts with §8, §9 or §10.6, **this section takes precedence**;
> those sections remain as narrative background. It settles four decisions
> approved in chat on 2026-08-01 and specifies F1–F10.
>
> Nothing here reopens a Phase E contract. Phase F consumes Phase E's immutable
> Offer Version snapshots, deterministic calculation rules and eligibility gate
> exactly as implemented.

### 8A.0 Four settled decisions

**D1 — `offer_level` tax is all-or-nothing per Offer Version.** A contractor may
select either **every positively quoted line** of that offer or none of them.
"Positively quoted" means `ResponseStatus = quoted`; `no_bid`, `unavailable` and
`unanswered` lines are excluded from the completeness requirement and are never
selectable. A strict subset fails validation with
`422 offer_level_tax_requires_complete_offer_selection`, listing the missing
line IDs. Phase F never apportions, pro-rates or omits a Supplier-quoted
whole-offer tax figure: apportionment would fabricate a financial value no
Supplier submitted (ADR 0001), and omission would understate an authoritative
commitment. A Supplier wanting partial selection must submit a new Offer Version
using `line_level` or `not_applicable` tax under the normal deadline and
supersession rules.

**D2 — the immutable Award Revision insert is the authoritative publication
point.** It mirrors Phase E submission. Chain advance, eligibility completion,
outcome generation, audit and notification are recoverable post-publication
work. Once a matching revision exists, rollback and eligibility-claim release
are prohibited.

**D3 — Supplier outcomes reuse the Phase D invitation session.** No second
credential is minted and no invitation is reactivated to grant outcome access.
Outcomes are scoped to **Supplier + Invitation**, not to the recipient identity
that submitted the offer, so a replacement recipient may read the outcome
without gaining any access to the previous recipient's draft.

**D4 — each Issued RFQ Version owns an independent Award chain.** Publishing a
later RFQ version never supersedes an earlier Award. Duplicate awards are
prevented by a separate RFQ-chain-scoped line-level claim, not by cross-version
supersession.

### 8A.1 Module ownership and boundaries

`internal/awards` is a new domain module. It owns:

```text
award_decision_chains
award_drafts
award_revisions            (immutable)
award_line_claims          (RFQ-chain-scoped lineage claims)
award_outcomes
award_outcome_deliveries
award_outcome_acknowledgements
```

It **never** reads another module's collection. Every cross-module need is a
consumer-owned capability implemented by a composition adapter:

```text
awards -> IssuedRFQSource            (rfqissuance)    issued version + lines
awards -> OfferVersionSource         (supplieroffers) immutable versions
awards -> OfferEligibilityClaimant   (supplieroffers) claim/complete/release
awards -> AwardNotificationMailer    (platform/mail)
awards -> AwardAuditRecorder         (audit)          primitive-only
supplieraccess -> SupplierOutcomeSource (awards)      Supplier outcome read
```

Per ADR 0002 only composition adapters may name two domain modules.

### 8A.1A Each checkpoint is vertically complete (Revision 19)

Every F-checkpoint that owns a behavior also delivers **its own HTTP surface**,
in the same checkpoint: route, DTO validation, role gating, tenant isolation,
status codes and audit. Routes are not deferred to a single integration batch at
the end.

```text
F1   comparison read
F2   award draft create / read / edit / discard
F5   finalise, award revision list and read
F6   correction
F7   contractor outcome list, Supplier outcome read
F8   notification send and retry, delivery list
F9   Supplier acknowledgement
F10  reconciliation ONLY, plus composition parity, acceptance and regression
```

The reason is security, not tidiness: authorization, tenant isolation and
non-disclosing not-found behavior are properties of a **route**, so deferring
routes defers the tests that prove them. F10 then verifies the *composed* whole
rather than introducing most of the attack surface at once.

### 8A.2 Two distinct serialization points

Phase F has **two** independent claim protocols. Conflating them is a design
error, so they are stated side by side:

```text
Offer eligibility gate            (Phase E, per Offer Version)
  scope:   CompanyID + OfferVersionID
  settles: withdrawal versus award for ONE offer
  states:  eligible -> award_claimed -> awarded

Award line claim                  (Phase F, per stable RFQ line lineage)
  scope:   CompanyID + RFQChainID + StableLineageID
  settles: one Supplier per line ACROSS every issued version
  states:  (absent) -> claimed -> awarded
```

An award finalisation acquires **both**: every offer gate for the versions it
selects, and every line claim for the lineages it awards.

For a **correction** (§8G), "selects" and "awards" mean the correction delta
only. Baseline selections already hold terminal claims from the revision that
awarded them; re-acquiring them would fail, since a terminal claim is not
`eligible`.

---

## 8B. F1 — Comparison projections and privacy

### Owned models

Comparison is a **read-only projection**. It owns no collection and persists
nothing; a stored comparison would be a second source of commercial truth that
could drift from the immutable Offer Versions.

### Invariants

- The projection reads only immutable Offer Versions plus their eligibility
  gates. `CompanyID` comes from the authenticated contractor principal, never
  from request content.
- By default it shows the **latest eligible submitted version** per
  Invitation + Issued RFQ Version. Historical versions are labelled
  `superseded`, `withdrawn`, `expired` or `previous RFQ version`.
- M8 does **not** rank Suppliers or recommend a winner. Sorting is a contractor
  action over facts, never a system judgement.
- Every displayed amount is read from a frozen Offer Version snapshot. The
  projection performs no arithmetic beyond summing already-calculated line
  subtotals for a candidate subset, and any such subtotal is explicitly labelled
  **indicative** — the authoritative figure is produced only by F3.

### Line-state treatment (settled)

```text
quoted        selectable; participates in comparison and totals
no_bid        shown as an explicit decline; never selectable
unavailable   shown as an explicit decline; never selectable
unanswered    cannot exist on a submitted version (E6 blocks submission)
withdrawn     version excluded from default view; labelled if shown
expired       version excluded from default view; labelled if shown
superseded    version excluded from default view; labelled if shown
```

An offer whose version carries `offer_level` tax is flagged
`partialAwardUnavailable = true` together with its complete positively-quoted
line set, so the interface can warn before F3 rejects a subset. The backend
remains authoritative regardless of interface behaviour.

### Privacy

Comparison is contractor-facing and shows every Supplier's figures — that is its
purpose. The privacy boundary runs the **opposite** way: no Supplier route ever
reaches this projection. F7 defines what a Supplier may see.

### HTTP

```text
GET /rfq-chains/{rfqChainId}/issued-versions/{versionId}/comparison   200
```

Owner, admin and employee may read: a comparison is preparation, not an
externally visible transition.

### Audit

None. Reading a comparison is not a state change; auditing reads would add noise
without adding accountability.

### Focused tests

Latest-eligible default selection; each historical label; declines shown but not
selectable; `partialAwardUnavailable` flag and its line set; indicative subtotal
labelled and never authoritative; foreign-tenant read returns 404.

### Real-Mongo tests

Withdrawal concurrent with a comparison read never yields a projection showing a
withdrawn version as selectable.

---

## 8C. F2 — Provisional award draft lifecycle

### Owned models

```go
type AwardDecisionChain struct {
    ID                     string
    CompanyID              string
    RFQChainID             string   // required for cross-version lineage claims
    IssuedRFQVersionID     string
    CurrentAwardRevisionID *string
    LatestRevisionNumber   int
    FinalisationState      string   // draft | finalising | published
    FinalisingOperationID  string   // set while FinalisationState = finalising
    FinalisingRevisionID   string   // candidate revision being published
    Revision               int64
    CreatedAt              time.Time
    UpdatedAt              time.Time
    SchemaVersion          int
}
```

`FinalisationState` is required by D2: because the chain pointer may lag an
authoritative revision insert, a durable state is the only thing preventing a
second operation from reading a stale pointer and publishing a duplicate
revision number.

```go
type AwardDraft struct {
    ID                 string
    CompanyID          string
    AwardChainID       string
    IssuedRFQVersionID string
    LineDecisions      []AwardLineDecisionDraft
    Revision           int64
    CreatedByUserID    string
    CreatedAt          time.Time
    UpdatedAt          time.Time
    SchemaVersion      int
}

type AwardLineDecisionDraft struct {
    IssuedRFQLineID string
    StableLineageID string   // provenance used by cross-version line claims
    Decision        string   // selected | unawarded
    OfferVersionID  string   // selected only
    OfferLineID     string   // selected only
    UnawardedReason string   // unawarded only
    UnawardedNote   string   // required when reason = other
}
```

### State machine

```text
AwardDraft:   open --finalise--> archived

AwardChain:   draft --claim--------> finalising
              finalising --revision insert--> published
              finalising --abort, no revision exists--> draft
```

### Invariants

- One open draft per Award chain, enforced by a partial unique index.
- Every issued RFQ line appears exactly once, either `selected` or `unawarded`.
  Completeness is validated at finalisation, not on every edit, so a contractor
  may work incrementally.
- **A provisional selection claims nothing.** It does not touch offer
  eligibility and therefore does not block withdrawal (existing rule, retained).
  A Supplier may withdraw an offer the contractor has provisionally selected;
  finalisation then fails and the contractor re-decides.
- `unawarded` requires a bounded reason:
  `no_acceptable_offer | purchase_deferred | scope_cancelled | retender_required | other`.
  `other` requires non-empty explanatory text.
- Draft mutation is refused while `FinalisationState != draft`.

### Repository CAS/index

```text
uq_award_chains_company_issued_version   CompanyID + IssuedRFQVersionID
uq_award_drafts_company_chain_open       CompanyID + AwardChainID
                                           (partial: open drafts only)
```

Every draft edit is an open-only, expected-revision CAS, matching Phase E's edit
boundary.

### HTTP

```text
POST   /rfq-chains/{c}/issued-versions/{v}/award-draft           201
GET    /rfq-chains/{c}/issued-versions/{v}/award-draft           200
PUT    .../award-draft/lines/{lineId}/selection                  200
PUT    .../award-draft/lines/{lineId}/unawarded                  200
DELETE .../award-draft                                           204
```

**Role policy, stated explicitly (Revision 20).** "Prepare" was previously
undefined here. Decision A (§1A.1) already settles it: employees may prepare
editable drafts, **including provisional award drafts**, but may not perform
externally visible or irreversible transitions. Applied to these routes:

```text
owner, admin, employee   create draft
                         read draft
                         set a line selection
                         set a line unawarded
                         discard draft

owner, admin ONLY        finalise      (F5)
                         correct       (F6)
                         notify        (F8)
                         reconcile     (F10)
```

A provisional draft claims nothing and changes nothing a Supplier can observe
(§8C), so employee mutation carries no external effect. The irreversible,
externally visible boundary is finalisation — that is where owner/admin begins.

Foreign tenant is 404; insufficient role is 403.

### Audit

`award_draft_created`, `award_draft_updated`, `award_draft_discarded` —
primitive-only: company, actor, chain, draft, revision, timestamp.

### Focused tests

Line completeness; bounded unawarded reasons; `other` requires text; selecting a
declined or unanswered line is refused; edits refused while `finalising`; stale
revision refused.

### Real-Mongo tests

One open draft per chain under concurrent creation; concurrent edits produce
exactly one winner.

---

## 8D. F3 — Authoritative selected-line calculations

### Ownership

Calculation is a pure function in `awards`, mirroring Phase E's
`CalculateSubmission`. **The browser never supplies an Award total.** Every
figure is derived here from immutable Offer Version snapshots and the immutable
issued RFQ version.

### Inputs and outputs

```go
type AwardCalculationInput struct {
    IssuedRFQ       IssuedRFQSnapshot        // authoritative lines
    Selections      []AwardLineSelection     // draft decisions
    OfferVersions   map[string]OfferVersionSnapshot // frozen, by version ID
    CalculatedAt    time.Time
}

type AwardCalculation struct {
    AwardedLines      []AwardedLine
    UnawardedLines    []UnawardedLine
    SupplierSummaries []AwardSupplierSummary
    GrandAwardTotal   money.Money
    SelectionFingerprint string
}
```

### Validation, in order

A selected line is valid only when **all** hold. Order matters: identity is
checked before money, so an invalid selection never reaches arithmetic.

```text
1  offer version belongs to this Company and this issued RFQ version
2  offer version is submitted and is the current version for its invitation
3  its eligibility gate is `eligible` (not withdrawn/claimed by another)
4  version is unexpired at CalculatedAt (OfferValidUntil strictly later)
5  the offer line response is `quoted`
6  quantity and unit exactly match the issued line
7  currency matches the issued RFQ currency
8  the FULL line is awarded — no quantity splitting, ever
9  offer_level completeness (D1) holds for that version
10 conditional charge rules resolve deterministically
```

Failures are bounded and distinct so the contractor learns what to fix:

```text
422 offer_version_not_selectable      (1,2)
409 offer_version_not_eligible        (3)   withdrawn or claimed elsewhere
422 offer_version_expired             (4)
422 offer_line_not_quoted             (5)
422 quantity_or_unit_mismatch         (6)
422 currency_mismatch                 (7)
422 offer_level_tax_requires_complete_offer_selection   (9)
422 conditional_charges_not_resolvable (10)
```

### Authoritative recalculation rules

Per selected Offer Version, using only that version's own frozen figures:

```text
selected line subtotals        sum of the version's own calculated subtotals
+ line tax                     recalculated per selected line (line_level)
+ offer-level tax              FULL quoted amount, only when D1 completeness holds
+ conditional charge groups    re-evaluated against the SELECTED line set only
+ delivery charge              applied once per version when >=1 line is awarded
= that version's contribution

GrandAwardTotal = sum of every selected version's contribution
```

Notes that prevent real errors:

- **Conditional groups are re-evaluated, not copied.** A group's trigger depends
  on which lines are selected, so carrying forward the submission-time amount
  would attribute a charge to a line set that was never awarded.
- **Delivery is per Offer Version, not per award.** Awarding two Suppliers means
  two delivery charges: each Supplier quoted one for their own delivery.
- All rounding goes through `money.RoundToMinorUnits` (ADR 0001). No new
  rounding is introduced.
- An offer contributing zero awarded lines contributes **zero**, including no
  delivery and no offer-level tax.

### Unawarded lines

Unawarded lines carry their reason into the revision and contribute nothing.
They are recorded because "we deliberately did not buy this" is a decision worth
preserving, and it is what makes `retender_required` auditable later.

### Selection fingerprint

A canonical SHA-256 over: company, issued version, and for each decision the
issued line, lineage, decision, offer version, offer line, unawarded reason, and
the calculated totals. Declared inline as canonical structs — never a persisted
document or DTO — for the reasons Phase E documents. It excludes timestamps and
actor identity so a retry of the same decision is recognisably the same award.

### Focused tests

Each of the ten validations independently; two Suppliers yield two delivery
charges; conditional group re-evaluated against the selected subset;
`offer_level` complete selection succeeds and strict subset fails; zero-line
offer contributes nothing; fingerprint deterministic; fingerprint changes with
any commercial change; fingerprint ignores timestamps.

### Real-Mongo tests

Withdrawal landing between validation and publication is caught at the
eligibility CAS (see F4), not by this pure function.

---

## 8E. F4 — Multi-offer eligibility claim protocol

### The problem

One award may select several Offer Versions and several RFQ lineages. Claims
must be acquired without deadlock, without partial publication, and without ever
releasing another operation's claim.

### Deterministic acquisition order

```text
1  transition the Award chain draft -> finalising
     CAS on expected revision; records operation ID and candidate revision ID

2  acquire AWARD LINE CLAIMS, ordered by StableLineageID ascending
     scope CompanyID + RFQChainID + StableLineageID

3  acquire OFFER ELIGIBILITY GATES, ordered by OfferVersionID ascending
     eligible -> award_claimed, via the Phase E capability
```

Ordering is lexicographic and total, so two concurrent finalisations request
shared resources in the same sequence and one always wins outright — the
standard deadlock-avoidance argument. The chain transition comes first because
it is the cheapest way to reject a second finalisation before it touches any
shared claim.

### Award line claim

```go
type AwardLineClaim struct {
    ID                string
    CompanyID         string
    RFQChainID        string
    StableLineageID   string
    State             string   // claimed | awarded
    AwardOperationID  string
    AwardRevisionID   string   // candidate, then final
    IssuedRFQVersionID string
    Revision          int64
    ClaimedAt         time.Time
    AwardedAt         *time.Time
}
```

```text
uq_award_line_claims_company_chain_lineage
    CompanyID + RFQChainID + StableLineageID     UNIQUE
```

This one index is what makes "one Supplier per line, across every issued
version" true. An already-`awarded` lineage rejects a new claim with
`422 rfq_line_already_awarded`; a lineage `claimed` by a live different
operation rejects with `409 rfq_line_award_conflict`.

### Partial acquisition and safe release

**Scope: this concerns only in-flight claims of a finalisation that never
published.** A claim held by a *published* revision is terminal and is never
released by any path — see §8G. The two cases must not be conflated:

```text
claimed, no revision exists     -> releasable by its OWN operation
claimed/awarded, revision exists -> TERMINAL, never released
```

If any claim fails, the operation **releases only claims whose recorded
`AwardOperationID` is its own**, in reverse acquisition order, and returns the
originating error. It may never release a claim owned by another operation.

Release is permitted **only** after verifying no matching Award Revision exists
(D2). Once the revision exists, release is prohibited and the correct action is
always to complete forward.

**Where that check lives.** Phase E's `ReleaseAwardClaim` enforces state,
`ClaimType = award`, exact operation ID and exact claim ID — but it does **not**
and must not know whether an Award Revision exists; `supplieroffers` has no
award knowledge, and giving it any would violate ADR 0002. The
no-revision-exists verification therefore belongs to the `awards` finalisation
service, immediately before it calls the release capability. An implementation
that assumes Phase E performs this check would release claims after an
authoritative award — the exact failure D2 forbids.

### Crash recovery

```text
chain finalising, no revision
  -> same operation retries: re-acquire (idempotent), publish
  -> reconciliation, operation abandoned: verify no revision, release
     ONLY that operation's claims, return chain to draft

chain finalising, revision EXISTS
  -> never release; complete forward:
     advance chain, mark gates awarded, mark line claims awarded,
     generate outcomes, emit audit
```

### Focused tests

Acquisition order is deterministic; partial failure releases only own claims;
release refused when a revision exists; foreign claim never released.

### Real-Mongo tests

Two concurrent finalisations selecting overlapping lineages → exactly one
publishes. Concurrent withdrawal versus finalisation → exactly one wins, and if
withdrawal wins no revision exists. Crash after partial acquisition → retry
completes; abandoned operation → reconciliation releases only its own claims.
Two finalisations on the same chain → the second is rejected at the chain CAS.

---

## 8F. F5 — Immutable award publication and recovery

### Owned model

```go
type AwardRevision struct {
    ID                      string
    CompanyID               string
    AwardChainID            string
    RFQChainID              string
    IssuedRFQVersionID      string
    RevisionNumber          int
    FinalisationOperationID string
    SelectionFingerprint    string
    ChangeReason            string   // required from revision 2 onward

    AwardedLines      []AwardedLine
    UnawardedLines    []UnawardedLine
    SupplierSummaries []AwardSupplierSummary
    GrandAwardTotal   money.Money

    SupersedesRevisionID *string
    FinalisedByUserID    string
    FinalisedAt          time.Time
    SchemaVersion        int
}
```

Each awarded line references the exact issued RFQ line, lineage, Supplier,
Invitation, Offer Version, Offer line, and the conditional charge rules applied.

### Publication sequence

```text
1  validate + calculate (F3)                      no writes
2  chain draft -> finalising                      CAS
3  acquire line claims, then offer gates (F4)
4  revalidate withdrawal/expiry immediately before insert
5  INSERT IMMUTABLE AWARD REVISION   <-- AUTHORITATIVE (D2)
6  advance chain, set published, point at revision   recoverable
7  mark offer gates awarded                          recoverable
8  mark line claims awarded                          recoverable
9  generate outcomes (F7)                            recoverable
10 emit audit                                        recoverable
```

Step 4 exists because F3's validation is not a lock: a withdrawal may land
between calculation and insert. The re-read closes that window to the width of
the eligibility CAS itself.

### Indexes

```text
uq_award_revisions_company_chain_number      CompanyID + AwardChainID + RevisionNumber
uq_award_revisions_company_operation         CompanyID + FinalisationOperationID
_id                                          globally unique candidate revision ID
```

The number index is the final safeguard: even if two operations somehow reached
insert, only one revision number can exist.

### Idempotency and unknown-outcome retries

A timeout does not prove the insert failed. On retry, resolve by the recorded
candidate revision ID or finalisation operation ID:

```text
matching revision exists              -> adopt it, complete steps 6-10
no revision exists                    -> retry the exact insert, same ID/number
revision exists, fingerprint differs  -> 409, never overwrite, never re-number
```

A retry never allocates a new revision number and never generates a different
revision.

### Reads during the crash window

An internal read that resolves "the current award" must recognise an inserted
revision even when the pointer is stale. It resolves the revision named by the
active finalisation claim, or returns a bounded
`503 award_finalisation_pending`. It must **never** report "no award exists"
while an authoritative revision is present.

### HTTP

```text
POST /rfq-chains/{c}/issued-versions/{v}/award-revisions   201  finalise
GET  /rfq-chains/{c}/issued-versions/{v}/award-revisions   200  list
GET  .../award-revisions/{revisionId}                      200
```

The reconciliation route belongs to F10 (§8K), not here: it repairs state across
every Phase F aggregate, so it is specified once where that whole surface is.

```text
owner, admin ONLY        POST finalise   irreversible, externally visible
owner, admin, employee   GET  list, GET read   award records are viewable
```

### Audit

`award_finalised` — company, actor, chain, revision ID, revision number,
operation ID, timestamp.

**Audit is ensured, not skipped (Revision 20).** Audit emission is
post-publication recoverable work (step 10), so a crash between insert and audit
would otherwise leave an authoritative award permanently unaudited — a replay
that "emits nothing" would never repair it. The rule is therefore idempotent
existence, not first-writer-only:

```text
deterministic audit identity:
    CompanyID + EventType + AwardRevisionID + FinalisationOperationID

audit exists   -> no-op
audit missing  -> record it exactly once
```

Every completion path applies this identically: the original request, a
same-operation retry that adopts an existing revision, and F10 reconciliation.
The result is exactly one `award_finalised` event per authoritative revision, no
matter which path completes it or how many times completion runs.

The same ensure-once identity governs `award_corrected`,
`award_outcome_generated`, `award_outcome_notified` and
`award_outcome_acknowledged`, substituting each event's own authoritative
subject for `AwardRevisionID`.

### Focused tests

Revision immutability; revision numbering; change reason required from revision
2; retry adopts existing revision; mismatched fingerprint conflicts.

Audit ensure-once: a crash between insert and audit leaves the event missing,
and a same-operation retry **records it**; a retry against a revision that
already has its event records **nothing**; reconciliation reaches the same end
state; across all three paths exactly one event exists per revision.

### Real-Mongo tests

Crash after insert before chain advance → recovery completes, gates awarded,
claims never released. Crash before insert → retry inserts the recorded
candidate. Concurrent finalisation → one revision, one winner. Withdrawal versus
publication → one winner. Read during the crash window never reports "no award".

---

## 8G. F6 — Award correction revisions

### Model

A correction is a **new immutable `AwardRevision`** on the same chain with
`RevisionNumber + 1`, `SupersedesRevisionID` pointing at the prior revision, and
a required non-empty `ChangeReason`. Prior revisions are never mutated: the
record of what was decided, and communicated to Suppliers, must survive being
changed.

### Corrections are commercially monotonic (Revision 19)

**An award, once published, is never reduced, removed or reassigned by a
correction.** A published revision is authoritative evidence that a Supplier was
selected, and an outcome may already have told them so. Releasing its claims
would permit this sequence:

```text
revision 1 awards Lineage A to Supplier X
  -> Supplier X receives a "selected" outcome
revision 2 (correction) drops Lineage A
  -> gates and lineage claim released
  -> Lineage A awarded again to Supplier Y

Supplier X was told they won, and silently did not.
```

Reversing a published award is a **rescission**, not a correction: it needs its
own outcomes, notifications, audit semantics and Supplier-facing wording.
**Rescission is out of scope for M8.** Phase F must not approximate it.

Therefore:

```text
A correction MAY:
  add a previously unawarded, currently eligible lineage
  amend non-commercial metadata and explanations
  amend the reason recorded against an unawarded line

A correction MUST NOT:
  remove an awarded lineage
  reduce an awarded line's commercial contribution
  reassign an awarded lineage to another Supplier, Offer Version or Offer line
  turn a previously selected Supplier into unsuccessful
```

Every lineage awarded by an earlier authoritative revision retains **the same**
Supplier, Offer Version, Offer line and authoritative commercial contribution in
every later revision on that chain. Violations are refused with
`422 award_correction_not_monotonic`, naming the offending lineages.

### Locked baseline versus correction delta (Revision 20)

A correction **cannot** rerun ordinary F3 validation over the whole award. F3
requires each selected Offer Version to be the current submitted version, hold
an `eligible` gate, and be unexpired. A version already awarded by the current
revision will typically satisfy **none** of those: its gate is terminally
`awarded`, the Supplier may have submitted a later version since, and the
original validity date may have passed. Revalidating it would reject exactly the
monotonic corrections F6 exists to permit.

A correction is therefore two disjoint sets:

```text
LOCKED BASELINE  = every previously awarded selection in the CURRENT
                   authoritative Award Revision
  sourced only from that revision
  copied exactly
  NOT revalidated against gate state, expiry or LatestSubmittedID
  NOT re-claimed (its claims are already terminal)
  cannot be removed, reduced, repriced or reassigned

CORRECTION DELTA = newly awarded lineages, plus permitted changes to
                   unawarded reasons and non-commercial metadata
  passes the FULL current F3 validation
  acquires new lineage claims
  acquires offer gates for newly selected Offer Versions
  calculated from current immutable Offer snapshots
```

F6 therefore calls a correction-aware calculation, not the initial-award one:

```go
CalculateCorrection(
    authoritativeBaselineRevision,   // frozen, copied, never revalidated
    proposedDecisions,               // baseline + delta
    newlyReferencedOfferVersions,    // delta only, validated now
) (AwardCalculation, error)
```

### Adding a line from an Offer Version already in the baseline

A correction may add a second line from a version the baseline already uses.
Three amounts are **not** per-line and would otherwise be double-counted or
mis-triggered:

```text
delivery charge      applied ONCE per Offer Version, never twice
conditional groups   may newly trigger, or increase, over the larger line set
percentage groups    amount changes when the selected subtotal grows
```

The rule is cumulative, not additive:

```text
cumulative contribution = authoritative recalculation over
                          (baseline selections + newly selected lines)
                          for that Offer Version

correction delta        = cumulative contribution - frozen baseline contribution
```

The cumulative contribution **must never be less than** the frozen baseline
contribution; a shortfall means the correction would reduce a published amount
and is refused with `422 award_correction_not_monotonic`. Delivery is charged
once because the cumulative recalculation applies it once, and the subtraction
removes the copy already in the baseline.

**`offer_level` tax cannot reach this path.** D1 requires that such a version was
awarded with every positively quoted line selected, so no line of it remains to
add later. An attempted addition is a contract violation, not a valid correction.

### Invariants

- A correction runs F3 validation **over the delta only**, then F4 claim
  acquisition for the delta only, then F5 publication. Baseline selections are
  copied from the current revision and never revalidated or re-claimed.
- **Awarded Offer eligibility gates and awarded RFQ-chain lineage claims are
  terminal.** They are never released — not by a correction, not by
  reconciliation, not by any Phase F path. This follows directly from D2: once a
  matching immutable revision exists, release is prohibited.
- **Cross-version line eligibility still applies (D4).** A correction cannot
  newly award a lineage another issued version has already awarded; it fails
  with `422 rfq_line_already_awarded`.
- Offers newly selected by a correction must be eligible **now** — a Supplier who
  withdrew after the original award cannot be awarded by a correction.
- Corrections are refused while `FinalisationState != published`.
- Prior revisions, prior outcomes and sent notifications are immutable. A
  correction generates **new** outcomes (F7); it never edits one already sent.

### HTTP

```text
POST /rfq-chains/{c}/issued-versions/{v}/award-revisions   201
     body carries expectedRevisionNumber, changeReason, operationId
```

Owner and admin only. `409` when the chain moved underneath the caller.

### Audit

`award_corrected` — company, actor, chain, new revision ID and number,
superseded revision ID, operation ID, timestamp.

### Focused tests

**Baseline is never revalidated** — each of these must SUCCEED, and each fails
if F3 is wrongly applied to the baseline:

```text
baseline version's gate is terminally `awarded`   -> correction succeeds
baseline version is no longer LatestSubmittedID   -> correction succeeds
baseline version's OfferValidUntil has passed     -> correction succeeds
baseline version's Supplier later withdrew a NEWER version -> correction succeeds
```

**Delta is fully validated** — a newly added lineage whose offer is withdrawn,
expired, not current, or not positively quoted is refused with the ordinary F3
error.

**Same-version addition:** delivery charged exactly once across baseline and
correction; a percentage conditional group recalculated over the cumulative line
set; a newly triggered group appears in the delta; cumulative contribution below
the frozen baseline is refused `422 award_correction_not_monotonic`; adding a
line to an `offer_level` version is refused as a contract violation.

**Monotonicity:** removing an awarded lineage refused; reducing an awarded
contribution refused; reassigning an awarded lineage to another Supplier
refused; turning a selected Supplier unsuccessful refused; awarded gates and
lineage claims never released.

**Other:** correction supersedes without mutating; change reason required;
correction may amend an unawarded reason and non-commercial metadata; correction
cannot award a lineage awarded on another issued version; prior outcomes and
sent notifications unchanged; correction refused while finalising.

### Real-Mongo tests

Concurrent corrections → one winner, contiguous revision numbers. Correction
concurrent with a withdrawal of a newly selected offer → one winner.

---

## 8H. F7 — Selected and unsuccessful outcomes

### Owned model

```go
type AwardOutcome struct {
    ID                 string
    CompanyID          string
    AwardChainID       string
    AwardRevisionID    string
    RFQChainID         string
    IssuedRFQVersionID string
    SupplierID         string
    InvitationID       string
    Result             string   // selected | unsuccessful
    Projection         OutcomeProjection   // frozen Supplier-safe snapshot
    CreatedAt          time.Time
    SchemaVersion      int
}
```

One outcome per **Supplier + Award Revision**. Outcomes are generated
automatically as recoverable post-publication work (F5 step 9), but are **not
sent** until the contractor explicitly acts (F8).

### Who receives an outcome

Every Supplier holding an eligible submitted Offer Version against that issued
RFQ version — whether or not they won. A Supplier who withdrew before
publication receives none: they removed themselves from consideration.

### Projection — the privacy boundary

```text
selected:
  awarded lines (own only), own quoted figures for those lines,
  own applicable tax and charge summary, own award total,
  contractor message, next-step wording

unsuccessful:
  neutral non-award statement, contractor message

NEVER, in either case:
  competitor names or identities
  competitor prices, totals or rankings
  how many Suppliers responded
  comparison notes or internal decision reasoning
  unawarded-line reasons (internal sourcing rationale)
```

The projection is **frozen at generation** from the revision. A stored snapshot
cannot drift, and it means a later correction cannot retroactively change what a
Supplier was already told.

Unawarded-line reasons are deliberately excluded: `no_acceptable_offer` and
`purchase_deferred` disclose the contractor's commercial position.

### HTTP

Contractor:

```text
GET /rfq-chains/{c}/issued-versions/{v}/award-revisions/{r}/outcomes   200
```

Supplier (Phase D session, D3):

```text
GET /supplier-access/outcomes/{outcomeId}   200
```

The Supplier route resolves Company, Supplier and Invitation from the session
and returns `404` when the outcome belongs to another Supplier — the same
non-disclosing not-found Phase D uses throughout.

### Audit

`award_outcome_generated` — company, actor, revision, supplier, invitation,
outcome, result, timestamp.

### Focused tests

Selected projection contains only own lines; unsuccessful projection contains no
commercial figures; no projection contains competitor data or unawarded reasons;
withdrawn Supplier gets no outcome; projection frozen against later corrections;
foreign Supplier read is 404.

### Real-Mongo tests

One outcome per Supplier + revision under concurrent generation; recovery
regenerates missing outcomes without duplicating existing ones.

---

## 8I. F8 — Notification delivery, retry and obsolescence

### Owned model

```go
type AwardOutcomeDelivery struct {
    ID                  string
    CompanyID           string
    AwardOutcomeID      string
    AwardRevisionID     string
    SupplierID          string
    RecipientIdentity   string   // snapshot at send time
    AccessGeneration    int64    // snapshot: which link was current
    DeliveryOperationID string
    Channel             string   // email
    Status              string   // pending | sent | failed | obsolete
    FailureCode         string   // bounded
    CreatedAt           time.Time
    SentAt              *time.Time
    SchemaVersion       int
}
```

`delivered` is deliberately **not** a status: SMTP acceptance is not proof of
delivery, and claiming otherwise would misrepresent evidence. `sent` means
"handed to the mail transport".

### Ordering

Mirrors Phase C exactly: **persist intent, then send.**

```text
1  insert delivery record, status pending, with operation ID
2  send synchronously
3  update status sent | failed with a bounded failure code
```

A crash after step 1 leaves a recoverable `pending` record, never a sent email
with no record.

### Rules

- Notifications are created and sent only after an **explicit contractor
  action** — never automatically on publication. An award is a decision; telling
  Suppliers is a separate, deliberate act.
- Idempotent per `CompanyID + DeliveryOperationID`: a retry with the same
  operation resolves the same record and does not re-send.
- Retry after failure is **explicit and audited**, creating a new delivery
  record for the same outcome. Failure never rolls back the award or outcome.
- **Stale-delivery obsolescence:** publishing a correction marks `pending`
  deliveries for superseded revisions `obsolete`. `sent` and `failed` records
  are historical facts and are never rewritten — a Supplier really was told.
- Recipient identity and access generation are snapshotted, so history shows
  which link was current when sent.

### HTTP

```text
POST .../award-revisions/{r}/outcomes/{outcomeId}/notifications   202
POST .../notifications/{deliveryId}/retry                         202
GET  .../award-revisions/{r}/notifications                        200
```

Owner and admin only: sending is externally visible.

### Audit

`award_outcome_notified`, `award_outcome_notification_retried`,
`award_outcome_notification_obsoleted` — primitive-only, one per authoritative
write, never on idempotent replay.

### Focused tests

Intent persisted before send; failure records a bounded code and leaves the
award intact; same-operation retry does not re-send; correction obsoletes only
pending deliveries; sent/failed never rewritten; no raw Supplier credential
appears in any delivery record or log.

### Real-Mongo tests

Concurrent send with the same operation ID → one delivery record. Crash between
intent and send → recovery resolves the pending record without duplicating mail.

---

## 8J. F9 — Supplier acknowledgement

### Owned model

```go
type AwardOutcomeAcknowledgement struct {
    ID                string
    CompanyID         string
    AwardOutcomeID    string
    SupplierID        string
    InvitationID      string
    SessionID         string
    RecipientIdentity string   // who actually acknowledged
    OperationID       string
    AcknowledgedAt    time.Time
    SchemaVersion     int
}
```

One acknowledgement per outcome, enforced by
`uq_award_outcome_acks_company_outcome`.

### Semantics — what it is not

Acknowledgement confirms **receipt only**. The API and interface must state:

```text
Acknowledgement confirms receipt only.
It is not acceptance of a Purchase Order or contractual commitment.
```

It is **not**: acceptance of a purchase order, contractual acceptance,
confirmation of delivery, invoice approval, consent to the award, or any
precondition for the award being authoritative.

Therefore an inability to acknowledge — expired or revoked credentials — never
blocks award publication, recovery, outcome creation, other Suppliers'
notifications, or later procurement stages.

### Idempotency

```text
first valid acknowledgement    records identity, session, operation, timestamp
same operation retry           returns the existing receipt unchanged
different later attempt        returns the existing receipt; original identity
                               and time are never overwritten
```

The first receipt is the true one; a second attempt does not rewrite history.

### Access (D3)

Requires a valid Phase D session, current invitation binding, matching Company,
Supplier and Invitation, current access generation, and CSRF. A replacement
recipient may acknowledge — the record captures the identity that actually did
so, which is the honest fact.

### HTTP

```text
POST /supplier-access/outcomes/{outcomeId}/acknowledgements   201 | 200
```

`201` on first record, `200` when returning an existing receipt. `403` on CSRF
failure, `404` for another Supplier's outcome.

### Audit

`award_outcome_acknowledged` — Supplier actor, company, outcome, supplier,
invitation, timestamp. Emitted once, only for the authoritative first receipt.

### Focused tests

Receipt recorded with the acknowledging identity; retry returns the same
receipt; second identity does not overwrite; CSRF failure is 403; foreign
outcome is 404; expired credentials block acknowledgement but not the award.

### Real-Mongo tests

Concurrent acknowledgements → exactly one record, one audit event.

---

## 8K. F10 — Reconciliation, races and acceptance

### Ownership

`awards` owns award reconciliation. It never repairs another module's state
directly: offer gates move only through the Phase E capability.

### Reconciliation cases

```text
chain finalising, revision exists
  -> advance chain, mark gates awarded, mark line claims awarded,
     generate missing outcomes, emit missing audit

chain finalising, no revision, operation abandoned
  -> verify no revision, release ONLY that operation's claims,
     return chain to draft

revision exists, gates not awarded          -> mark awarded (never release)
revision exists, line claims not awarded    -> mark awarded
revision exists, outcomes missing           -> generate missing only
delivery pending, send outcome unknown      -> leave pending; explicit retry
all complete                                -> return idempotently
```

Reconciliation **never** invents a selection, a total, a reason, a revision
number, a candidate identity or a commercial record; never rolls the chain
backward; and never releases a claim once a matching revision exists.

Audit follows the ensure-once rule (§8F): reconciliation records a missing event
under its deterministic identity and no-ops when it already exists. It neither
duplicates an event nor leaves an authoritative revision unaudited.

### HTTP

```text
POST /rfq-chains/{c}/issued-versions/{v}/award-reconciliation   200
```

Owner and admin only.

### Isolation rules

These hold for **every** Phase F route and are proven in the checkpoint that
introduces each route (§8A.1A). F10 re-verifies them across the composed
surface rather than testing them for the first time.

```text
tenant       every query is company-scoped; foreign resource -> 404
supplier     outcome routes resolve Supplier from the session; other
             Suppliers' outcomes -> 404
role         owner/admin only:  finalise, correct, notify, reconcile
             owner/admin/employee: comparison read, award draft
                                   create/read/edit/discard, award reads
```

### Acceptance (composed, `tenanttest`)

An end-to-end journey through the real router and real MongoDB:

```text
issue RFQ -> invite two Suppliers -> both verify and submit
-> compare -> draft award (split across both, one line unawarded)
-> finalise -> outcomes generated -> notify both
-> selected Supplier acknowledges
-> assert: unsuccessful Supplier sees no competitor data
-> assert: award total equals the recalculated authoritative figure
-> correct the award -> prior revision intact, new outcomes generated
```

### Full race matrix (real MongoDB)

```text
concurrent finalisations, same chain           one winner
concurrent finalisations, overlapping lineages one winner
withdrawal versus finalisation                 one winner
correction versus withdrawal                   one winner
concurrent corrections                         one winner, contiguous numbers
crash after revision insert                    recovery completes forward
crash before revision insert                   retry inserts recorded candidate
partial claim acquisition then crash           only own claims released
abandoned operation                            another operation never released
concurrent outcome generation                  one outcome per supplier+revision
concurrent notification send                   one delivery per operation
concurrent acknowledgement                     one receipt
cross-version duplicate award attempt          422/409, never two awards
```

### Phase F regression gate

Formatting, `go vet ./...`, `go build ./...`, focused suites, the race matrix
above, composed `tenanttest` acceptance, and serialized
`go test ./... -count=1 -p 1`.


---

## 8. Comparison and award decisions

### 8.1 Transparent comparison

The contractor may sort and filter by:

- line coverage;
- unit price;
- line subtotal;
- quoted tax mode and amount;
- delivery and conditional charges;
- selected-subset total;
- lead time;
- validity;
- exceptions;
- `no_bid` and `unavailable`.

M8 does not automatically rank Suppliers or recommend a winner.

By default, comparison shows the latest eligible submitted version for each Invitation + RFQ Version. Historical versions are labelled:

```text
superseded
withdrawn
expired
previous RFQ version
```

### 8.2 Provisional selection

A provisional draft may select different Suppliers for different lines.

One RFQ line is:

- awarded in full to exactly one Supplier; or
- explicitly unawarded.

M8 does not split one line’s quantity.

Every unawarded line requires a reason, such as:

```text
no_acceptable_offer
purchase_deferred
scope_cancelled
retender_required
other
```

`other` requires explanatory text.

### 8.3 Selection validation

A selected line is valid only when:

- the offer version belongs to this issued RFQ version;
- the version is submitted, current, non-withdrawn, and unexpired;
- the line response is `quoted`;
- quantity and unit exactly match the issued line;
- currency matches;
- the full line is awarded;
- applicable charge rules resolve deterministically.

### 8.4 Award finalisation

> **Revision 18:** §8F is the implementation contract. The sequence below is
> correct but incomplete — it omits the chain `finalising` transition, the
> two-tier claim acquisition (§8E), the pre-insert revalidation, and the fact
> that the **revision insert** is the authoritative point while steps after it
> are recoverable.

Finalisation:

1. loads the provisional draft under `expectedRevision`;
2. re-reads the immutable issued RFQ version;
3. re-reads every selected immutable offer version;
4. revalidates withdrawal, expiry, currency, quantity, unit, and charges;
5. recalculates totals;
6. confirms every line is awarded or explicitly unawarded;
7. creates the immutable Award Revision;
8. advances the Award Decision Chain;
9. archives the provisional draft.

Finalisation sends no email.

### 8.5 Award revision

> **Revision 19:** §8G bounds what "changing" may mean. Corrections are
> **commercially monotonic**: a later revision may add previously unawarded
> eligible lineages and amend non-commercial metadata or unawarded reasons, but
> may never remove, reduce or reassign an existing award. Awarded eligibility
> gates and lineage claims are terminal. Reversing a published award is a
> rescission, which is **out of scope for M8**.

Changing a finalised award creates a new immutable revision.

A required change reason explains why the prior award was superseded. Prior revisions and prior notifications remain unchanged.

### 8.6 Selection and commitment boundary

An award records the contractor’s sourcing decision only.

It does not:

- constitute a Purchase Order;
- create contractual acceptance;
- create committed costs;
- allocate delivery or tax into the cost ledger.

---

## 9. Award notifications and acknowledgement

### 9.1 Explicit notification action

Outcome notifications are created and sent only after an explicit contractor action.

Both selected and unsuccessful Suppliers receive an outcome.

Selected Supplier projection includes only:

- awarded lines;
- selected commercial summary;
- applicable quoted tax and charge summary;
- private contractor message;
- next-step wording.

Unsuccessful Supplier projection includes:

- neutral non-award result;
- private contractor message;
- no competitor details.

No Supplier sees competitor names, prices, rankings, comparison notes, or internal decision reasoning.

### 9.2 Delivery state

> **Revision 18:** §8I supersedes the status list below. `delivered` is **not**
> implemented: SMTP acceptance is not proof of delivery, and recording it as
> such would misrepresent the evidence. The implemented set is `pending`,
> `sent`, `failed`, `obsolete`.

Persist notification intent before queueing mail.

Statuses:

```text
pending
sent
failed
delivered
obsolete
```

Delivery failure does not roll back the award or notification record. Retry is explicit and audited.

A notification snapshots one award revision. A later award revision creates new notification records.

### 9.3 Receipt acknowledgement

A selected Supplier may acknowledge receipt.

The UI and API must state:

```text
Acknowledgement confirms receipt only.
It is not acceptance of a Purchase Order or contractual commitment.
```

Purchase Order acceptance, decline, and change requests belong to the next milestone.

---

## 10. Multi-document failure recovery and idempotency

M8 uses no MongoDB multi-document transactions unless the existing architecture has since adopted them. Authoritative serialization points and bounded reconciliation are required.

### 10.1 Issuance

Serialization point: `RFQIssuanceChain`.

```text
validate source
→ read chain and derive candidate LatestIssuedVersion + 1
→ create immutable version
→ advance chain
→ advance invitations
```

A caller operation ID prevents duplicate logical issuance. The immutable
version's company/chain/version unique index is the allocation authority:
concurrent callers contend for the same candidate and only one insert wins.
There is no pre-increment reservation that can leave an unidentifiable number
hole.

If the version exists but the chain pointer or invitations were not advanced, reconciliation completes those steps after verifying identity and source fingerprint.

### 10.2 Invitation send

```text
persist delivery intent
→ queue email
→ record result
```

Operation-ID reuse must verify:

- invitation;
- access generation;
- recipient;
- target RFQ version;
- logical send type.

Mismatched reuse returns conflict.

### 10.3 Verification and session creation

A newer challenge invalidates older active challenges.
Its creation also conditionally marks only the older challenge's pending
verification delivery attempts obsolete; sent and failed attempts remain
historical.

Challenge creation and exchange consumption are separate writes. The challenge
is uniquely linked to its source exchange. If challenge creation lands first,
retry finds that same challenge, verifies the exchange identity, and completes
exchange consumption without creating or sending another challenge. If the
exchange already records its `ChallengeID`, retry returns that same logical
result. Expiry and unused state are checked explicitly; TTL deletion is never an
authorization check.

Before challenge creation, the exchange ID claims the client-address rate scope
and then the identity/invitation/generation scope. Challenge creation begins
only after both idempotent CAS claims succeed. A partial first-scope claim is
never rolled back.

A mail failure occurs after challenge and delivery-intent persistence. It does
not delete the challenge or unconsume the exchange; explicit retry follows the
verification delivery rules.

Challenge confirmation performs one atomic conditional transition. Incorrect
codes decrement `AttemptsRemaining`; the fifth failure consumes the last
attempt and locks the challenge. Correct concurrent submissions can have only
one successful consumer, and concurrent fifth failures cannot consume more than
the one remaining attempt.

The challenge CAS records the exact target session, token generation and key
version before any session write. A same-operation retry with the correct code
creates or completes that exact session transition, rederives its token,
creates or advances the invitation binding, and revalidates the invitation.
`CreatedFromChallengeID` prevents duplicate new sessions.

Concurrent challenges targeting the same existing session generation may
converge on the same one-step rotation and create separate valid bindings. A
challenge whose target generation has already been surpassed fails neutrally.
A concurrent invitation generation change may leave a session or stale binding,
but final and request-time invitation validation deny access. Rebinding is
guarded by the existing binding `Revision`; concurrent generation updates have
exactly one winner.

### 10.4 Offer submission

> **Revision 15:** The workflow below is superseded by §1A.14. The active
> draft CAS is the authoritative submission point; the Offer Chain remains the
> latest-immutable-version pointer.

Serialization point: `SupplierOfferChain`.

```text
load draft
→ validate and recalculate
→ reserve next offer version
→ create immutable version
→ clear/archive draft
→ advance chain
```

Submission operation-ID reuse verifies:

- invitation;
- RFQ version;
- draft identity and expected revision;
- content fingerprint;
- recipient identity.

A changed draft under the same operation ID is a conflict.

### 10.5 Withdrawal and award race

> **Revision 15:** §1A.14 resolves this investigation item through the separate
> mutable `SupplierOfferEligibility` aggregate and the authoritative
> multi-version Award finalisation claim.

Withdrawal remains a separate immutable record.

Award finalisation re-reads withdrawal state immediately before its conditional chain update. A selected offer withdrawn first makes finalisation fail. A final award revision created first remains the authoritative historical decision; later PO behaviour is outside M8.

The repository investigation must define the exact one-winner mechanism and tests for this race without inventing an unsupported transaction guarantee.

### 10.6 Award finalisation

Serialization point: `AwardDecisionChain`.

Finalisation operation-ID reuse verifies the exact draft, RFQ version, selected offer versions, and content fingerprint.

A changed logical operation under the same ID returns conflict.

### 10.7 Reconciliation surfaces

Contractor-only reconciliation operations may complete only known state transitions:

```text
rfqissuance
- complete chain pointer
- advance lagging invitations

supplieraccess
- consume challenge linked to an existing session
- invalidate stale delivery/access state
- remove or mark stale session/invitation bindings

supplieroffers
- complete offer-chain pointer
- archive submitted draft
- enumerate orphan immutable versions

awards
- complete award-chain pointer
- archive finalised provisional draft
- retry notification delivery
```

Reconciliation never invents pricing, line decisions, recipients, or commercial intent.

---

## 11. Collections and indexes

### 11.1 Collection ownership

#### `internal/rfqissuance`

```text
rfq_issuance_chains
issued_rfq_versions
rfq_amendment_drafts
supplier_invitations
invitation_delivery_attempts
```

#### `internal/supplieraccess`

```text
supplier_access_exchanges
supplier_verification_challenges
verification_delivery_attempts
supplier_verification_rate_limits
supplier_sessions
supplier_session_invitation_bindings
```

#### `internal/supplieroffers`

```text
supplier_offer_chains
supplier_offer_drafts
supplier_offer_versions
supplier_offer_withdrawals
```

#### `internal/awards`

```text
award_decision_chains
award_drafts
award_revisions
supplier_outcome_notifications
outcome_delivery_attempts
supplier_outcome_acknowledgements
```

Each collection has one owner. No module queries another module’s collection directly.

### 11.2 Required uniqueness

```text
companyId + rfqChainId
    one issuance chain

companyId + rfqChainId + versionNumber
    one immutable issued version

companyId + rfqChainId
    at most one amendment draft

companyId + rfqChainId + supplierId
    one stable invitation

accessSecretHash
    one globally resolvable current invitation credential

exchangeTokenHash
    one globally resolvable browser exchange credential

companyId + invitationId + deliveryOperationId
    idempotent invitation send

companyId + challengeOperationId
    idempotent verification request

exchangeId
    one verification challenge sourced by one access exchange

companyId + challengeId + operationId
    idempotent verification-code delivery

scope + scopeKeyHash
    one atomic verification-rate state per protected scope

tokenHash
    globally unique session token

createdFromChallengeId
    at most one newly created session per verification challenge (partial)

companyId + supplierSessionId + invitationId
    one current binding per session and invitation

companyId + invitationId + issuedRfqVersionId
    one offer chain

companyId + invitationId + issuedRfqVersionId
    at most one active offer draft

companyId + offerChainId + versionNumber
    one immutable offer version

companyId + submissionOperationId
    idempotent submission

companyId + supplierOfferVersionId
    at most one withdrawal

companyId + issuedRfqVersionId
    one award chain

companyId + issuedRfqVersionId
    at most one provisional award draft

companyId + awardChainId + revisionNumber
    one immutable award revision

companyId + finalisationOperationId
    idempotent finalisation

companyId + awardRevisionId + supplierId
    one outcome notification

companyId + notificationId + deliveryOperationId
    idempotent notification delivery

companyId + notificationId
    at most one acknowledgement
```

Every index must have an explicit name. Duplicate-key classification must use the established named-index convention and return an unclassified sentinel when the index cannot be safely identified.

The global `accessSecretHash` index is partial over non-empty string values so
transitional documents without a credential do not conflict. Invitation writes
still validate the established 64-character lowercase hexadecimal hash. An
index collision fails closed and is never retried with changed derivation
inputs.

`supplier_session_invitation_bindings` additionally has named non-unique indexes
on `companyId + supplierSessionId` for session authorization and on
`companyId + invitationId + accessGeneration` for validation and reconciliation.

`supplier_sessions` has a named globally unique `tokenHash` index and a named
partial unique `createdFromChallengeId` index over non-empty string values.
There is deliberately no unique Supplier-identity index because separate
browsers and devices may hold separate sessions.

`supplier_access_exchanges` has a named TTL index on `expiresAt`, with
`expireAfterSeconds: 0`. TTL cleanup is housekeeping only: all exchange reads
still require `now < ExpiresAt`. The unique `exchangeTokenHash` and
challenge-`exchangeId` indexes enforce credential uniqueness and at-most-one
challenge per exchange.

`verification_delivery_attempts` has a named non-unique lookup index on
`companyId + challengeId + status`. Its named unique
`companyId + challengeId + operationId` index serializes same-operation
delivery creation.

`supplier_verification_rate_limits` has a named unique
`scope + scopeKeyHash` index and a named TTL index on `expiresAt` with
`expireAfterSeconds: 0`. Explicit rolling-window pruning and revision-guarded
CAS remain authoritative.

### 11.3 Optimistic concurrency

Every mutation, transition, or deletion of an existing mutable aggregate requires `expectedRevision`.

Creations do not require `expectedRevision`.

Immutable versions use business version or revision numbers and `SchemaVersion`; they are not updated in place.

---

## 12. API boundaries

Exact route names may be adjusted to existing Huma conventions during repository investigation, but the capability surface and permission boundaries are fixed.

### 12.1 Contractor RFQ issuance routes

```text
POST   /rfqs/{rfqId}/issue
GET    /rfq-chains/{rfqChainId}/versions
GET    /rfq-chains/{rfqChainId}/versions/{versionId}

POST   /rfq-chains/{rfqChainId}/amendment-draft
GET    /rfq-chains/{rfqChainId}/amendment-draft
PATCH  /rfq-chains/{rfqChainId}/amendment-draft
POST   /rfq-chains/{rfqChainId}/amendment-draft/issue
DELETE /rfq-chains/{rfqChainId}/amendment-draft
```

### 12.2 Contractor invitation routes

```text
POST   /rfq-chains/{rfqChainId}/invitations
GET    /rfq-chains/{rfqChainId}/invitations
GET    /rfq-chains/{rfqChainId}/invitations/{invitationId}
PATCH  /rfq-chains/{rfqChainId}/invitations/{invitationId}
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/send
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/resend
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/copy-link
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/replace-recipient
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/rotate-secret
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/revoke
POST   /rfq-chains/{rfqChainId}/invitations/{invitationId}/reactivate
PATCH  /rfq-chains/{rfqChainId}/invitations/{invitationId}/expiry
POST   /rfq-chains/{rfqChainId}/invitations/reconcile
```

`copy-link` (Revision 2, §1A.4, §6.1A) re-derives and returns the current
generation's raw link without rotating or sending. It is `POST` rather than `GET`
because it records a `copy_link` delivery attempt and an audit event, and because
a raw secret must never land in a URL, a browser history entry or a proxy log.

`reactivate` (Revision 5, §5.4) treats revocation as terminal for the current
access generation. It accepts `expectedRevision`, a future `expiresAt`, and
`operationId`; it never restores the revoked generation, sends email, or creates
a delivery attempt.

### 12.3 Contractor comparison and award routes

```text
GET    /rfq-versions/{versionId}/offer-comparison

POST   /rfq-versions/{versionId}/award-draft
GET    /rfq-versions/{versionId}/award-draft
PATCH  /rfq-versions/{versionId}/award-draft
POST   /rfq-versions/{versionId}/award-draft/finalise

GET    /rfq-versions/{versionId}/award-revisions
GET    /rfq-versions/{versionId}/award-revisions/{awardRevisionId}
POST   /rfq-versions/{versionId}/award-revisions/{awardRevisionId}/revise
POST   /rfq-versions/{versionId}/award-revisions/{awardRevisionId}/notifications

POST   /supplier-outcome-notifications/{notificationId}/send
POST   /supplier-outcome-notifications/{notificationId}/resend
```

### 12.4 Supplier access routes

Mounted separately from contractor JWT authentication:

```text
GET    /supplier-access/open?token=<invitation-token>
GET    /supplier-access/open
POST   /supplier-access/challenges
POST   /supplier-access/challenges/resend
POST   /supplier-access/challenges/verify
POST   /supplier-access/session/logout

GET    /supplier-access/invitations
GET    /supplier-access/invitations/{invitationId}
GET    /supplier-access/invitations/{invitationId}/rfq-versions
GET    /supplier-access/invitations/{invitationId}/rfq-versions/{versionId}

GET    /supplier-access/invitations/{invitationId}/offers
GET    /supplier-access/invitations/{invitationId}/offer-draft
POST   /supplier-access/invitations/{invitationId}/offer-draft
PATCH  /supplier-access/invitations/{invitationId}/offer-draft
POST   /supplier-access/invitations/{invitationId}/offer-draft/copy
POST   /supplier-access/invitations/{invitationId}/offer-draft/submit
POST   /supplier-access/invitations/{invitationId}/offers/{offerVersionId}/withdraw

GET    /supplier-access/invitations/{invitationId}/outcomes
POST   /supplier-access/outcomes/{notificationId}/acknowledge
```

The server derives tenant and Supplier identity from the session and invitation.

The clicked-link GET exchanges a valid invitation token for the dedicated
exchange cookie and returns `303` to the same clean path without a query string.
The clean GET response discloses no invitation identity and grants no Supplier
resource access; challenge creation remains an explicit POST using the exchange
cookie. Both responses use `Cache-Control: no-store`, `Pragma: no-cache`, and
`Referrer-Policy: no-referrer`.

Only the first six security routes are implemented in Phase D. Resend carries
its opaque high-entropy `challengeId` and `operationId` in the request body, not
the URL. Invitation/RFQ reads defer to Phase G privacy projections; offer routes
belong to Phase E and outcome routes to Phase F. All protected Supplier mutation
routes use the Revision 12 CSRF contract.

---

## 13. Permissions

Baseline contractor permissions:

| Action | Owner | Admin | Employee |
|---|---:|---:|---:|
| View issued RFQs, invitations, offers, and comparison | yes | yes | yes |
| Prepare amendment drafts | yes | yes | yes |
| Create or edit invitation drafts | yes | yes | yes |
| Build provisional award drafts | yes | yes | yes |
| Send invitations | yes | yes | no |
| Copy an invitation's secure link | yes | yes | no |
| Replace recipients or rotate secrets | yes | yes | no |
| Issue an RFQ version | yes | yes | no |
| Revoke Supplier access | yes | yes | no |
| Reactivate Supplier access or change expiry | yes | yes | no |
| Finalise or revise an award | yes | yes | no |
| Send award outcomes | yes | yes | no |
| Run state-changing reconciliation | yes | yes | no |

**Revision 2 resolution (§1A.1):** the repository investigation established that
no route-level role-authorization convention exists — M2–M7 gate on tenant only.
M8 therefore introduces the first one, as a narrow helper in `internal/identity`.
Authorization runs at the HTTP boundary before the privileged service call;
foreign tenant stays **404**, valid tenant with insufficient role is **403**; M8
services stay role-agnostic; M2–M7 are not retrofitted.

Supplier recipients may access only:

- invitations matching their verified identity;
- supplier-visible RFQ versions under those invitations;
- their Supplier’s own drafts and immutable offers;
- their Supplier’s own outcome notifications;
- acknowledgement for their own notification.

---

## 14. Error behaviour

Module-owned sentinels map to the established HTTP style.

### 404

- resource absent or outside tenant;
- invitation/session/recipient mismatch;
- Supplier access mismatch;
- outcome ownership mismatch.

Security-sensitive mismatches collapse to non-disclosing errors.

### 403

- missing or mismatched Supplier CSRF cookie/header;
- CSRF value does not match the active session generation.

A CSRF failure never revokes a valid session or clears its cookies.

### 409

- revision mismatch;
- operation ID reused for another logical operation;
- concurrent issuance winner;
- stale base RFQ version;
- duplicate stable invitation;
- active draft already exists;
- response window closed;
- invitation revoked or expired;
- invitation already active or otherwise ineligible for reactivation;
- access generation changed;
- offer withdrawn, expired, or superseded;
- selected offer became invalid;
- award changed concurrently;
- immutable chain pointer requires reconciliation.

### 422

- invalid currency;
- missing M7 response deadline at issuance (§4.1A);
- invalid recipient email or expiry;
- incomplete line responses;
- quoted partial quantity;
- malformed Money or quantity;
- invalid or contradictory tax configuration;
- ambiguous conditional charge rules;
- invalid `OfferValidUntil`;
- missing unawarded-line reason;
- unsupported transition.

### 429

- verification challenge rate limit;
- verification attempt limit;
- resend or access abuse rate limit.

### 503

- mail queue unavailable;
- M7 issuance-state source unavailable;
- required cross-module state cannot be safely established.
- Supplier credential resolution failed because MongoDB or required platform
  infrastructure is unavailable.

Mail delivery failure after intent persistence is normally represented in delivery state rather than rolling back the domain action.

---

## 15. Audit coverage

Each module declares its own typed primitive-only audit capability.

Audit events include:

### RFQ issuance

- first version issued;
- amendment draft created, updated, discarded, or issued;
- deadline-only version issued;
- issuance chain reconciled;
- invitation advancement completed or retried.

### Invitations

- invitation created;
- expiry changed;
- recipient replaced;
- secret rotated;
- send or resend requested;
- secure link copied (never the link or token itself);
- delivery succeeded or failed;
- invitation revoked;
- invitation reactivated with its new generation and issued-version identifier;
- invitation advanced to a new RFQ version;
- invitation opened by the Supplier (view recorded).

### Supplier access

- verification requested;
- verification succeeded;
- verification failed or rate-limited;
- session created;
- session renewed;
- session revoked.

### Supplier Offers

- draft created;
- draft copied;
- draft updated or archived;
- offer submitted;
- revision submitted;
- offer withdrawn;
- offer chain reconciled.

### Awards

- provisional selection changed;
- award finalised;
- award revised;
- notification created;
- send or resend requested;
- delivery result recorded;
- receipt acknowledged;
- award chain reconciled.

Forbidden audit values:

- raw invitation secrets and derived invitation links;
- raw access-exchange credentials;
- invitation-secret keyring keys or any key material;
- verification codes;
- session tokens;
- session CSRF tokens;
- password-like values;
- full unrestricted payloads;
- full email bodies;
- raw mail-provider error text.

---

## 16. Testing and implementation workflow

### 16.1 Superpowers workflow

The coding agent must use only applicable inline Superpowers skills:

```text
using-superpowers
brainstorming when new behaviour or a spec conflict appears
writing-plans only for a compact inline execution sequence when needed
test-driven-development
systematic-debugging for unexpected failures
verification-before-completion
receiving-code-review before applying review feedback
```

Do not invoke:

```text
dispatching-parallel-agents
subagent-driven-development
requesting-code-review
executing-plans
using-git-worktrees
finishing-a-development-branch
```

Do not perform Git operations.

### 16.2 Risk-based TDD

Strict RED–GREEN–REFACTOR is mandatory for:

- issuance concurrency and version allocation;
- immutable copy/fingerprint logic;
- invitation secret rotation;
- clicked-link exchange, credential redaction, expiry, and at-most-one challenge
  recovery;
- verification-code format, unbiased deterministic derivation, keyed verifier,
  key rotation, challenge attempts, supersession, and consumption;
- verification delivery idempotency, explicit retry, pending recovery, bounded
  provider failures, and credential-exposure spies;
- verification request cooldown and rolling-hour throttles, including
  concurrent boundary requests;
- client-address normalization, keyed persistence, trusted-proxy parsing, and
  the independent 20-per-hour throttle;
- rate-limit reservation idempotency, address-first partial-claim convergence,
  bounded arrays, expiry pruning, CAS contention, and non-disclosing
  `Retry-After`;
- Supplier session expiry and revocation;
- new-session recovery, same-browser re-verification, token rotation,
  multi-device isolation, and concurrent target-generation adoption;
- session-cookie renewal, exact sliding/absolute boundaries, CSRF derivation
  and enforcement, and idempotent logout;
- generation-bound session/invitation authorization, fresh-verification
  rebinding, and rotation races;
- recipient replacement;
- offer submission idempotency;
- tax and conditional charge calculations;
- offer withdrawal versus award finalisation;
- award revision finalisation;
- tenant isolation;
- Supplier-visible privacy boundaries.

Mechanical mapping and wiring may be tested alongside implementation, but tests remain mandatory.

### 16.3 Property-based tests

Use property tests where beneficial for:

- Money totals and rounding;
- charge-rule determinism;
- line-coverage completeness;
- no duplicate/missing RFQ lines;
- copy-forward compatibility;
- content-fingerprint determinism;
- operation-ID retry equivalence.

### 16.4 MongoDB/Testcontainers

Real MongoDB tests are mandatory for:

- unique and partial indexes;
- one-winner conditional updates;
- version allocation;
- one active draft;
- operation-ID uniqueness;
- token-hash uniqueness;
- exchange-token uniqueness, one-challenge-per-exchange, and concurrent exchange
  consumption;
- verification-delivery operation uniqueness and pending-to-terminal
  conditional transitions;
- concurrent fifth-attempt locking and one-consumer challenge confirmation;
- one-session-per-creation-challenge, concurrent same-session rotation, and
  global session-token-hash uniqueness;
- rate-state first-create uniqueness, six-of-five and twenty-one-of-twenty
  concurrent claims, and CAS-bounded arrays;
- session/invitation binding uniqueness and concurrent generation-update
  one-winner behavior;
- deadline and expiry filters;
- orphan and reconciliation queries;
- duplicate-key classification.

Use `-p 1` when running multiple Docker-backed packages on the current Docker Desktop setup.

### 16.5 Acceptance journeys

Required composed journeys:

```text
M7 ready RFQ
→ issue Version 1
→ create invitation
→ explicitly send
→ open secure link
→ verify recipient email
→ create a partial Supplier Offer
→ submit immutable version
→ create and submit a revised version
→ compare
→ build provisional line awards
→ finalise
→ create and send selected and unsuccessful outcomes
→ acknowledge receipt
```

Additional paths:

- invite another Supplier after issuance;
- issue Version 2 and advance stable invitations;
- view historical RFQ versions and own offers;
- optional copy-forward;
- replacement recipient invalidates old access;
- deadline expiry blocks submission;
- deadline extension creates a new issued version;
- withdrawn and expired offers cannot be selected;
- unawarded line requires reason;
- email failure preserves domain action and supports retry;
- every bounded interrupted operation is reconcilable;
- Supplier cannot see competitor or contractor-private data;
- tenant isolation holds for every contractor and Supplier route.

### 16.6 Final verification

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
./scripts/verify-format.ps1

go vet ./...
go build ./...
go test ./... -count=1 -p 1
```

Run race tests against high-risk M8 packages and concurrent workflows.

---

## 17. Explicit non-goals

M8 does not implement:

- Purchase Orders;
- Supplier acceptance, decline, or change request for a PO;
- committed-cost updates;
- CostItem allocation;
- goods receipt;
- fulfilment;
- delivery confirmation;
- Supplier invoices;
- e-Invoice validation or submission;
- payment tracking;
- inventory;
- automated Supplier ranking;
- AI recommendations;
- web scraping;
- currency conversion;
- quantity splitting within one RFQ line;
- Supplier platform accounts;
- multiple simultaneous recipient identities under one invitation;
- mobile applications;
- attachment handling unless separately designed;
- malware scanning unless file upload scope is separately approved.

---

## 18. Repository-investigation gate before implementation

Before writing M8 code, the coding agent must inspect the current implementation and report only genuine conflicts, including:

1. Exact M7 `GetReadyRFQSnapshot`, `RFQChainIsReady`, and `ReopenPermitted` capability shapes.
2. Current M7 `IssuanceStatusSource` wiring.
3. Existing M6 Access Grant/token/session precedents that may be safely reused.
4. Current mail and job interfaces and their local failure semantics.
5. Existing role-authorization helpers.
6. Existing audit method conventions.
7. Existing Mongo repository, counter, and duplicate-key classification patterns.
8. Current Huma route/schema naming constraints.
9. Whether attachments are mentioned in `phase1.md`; attachments remain excluded unless explicitly approved.
10. Any mismatch between this design’s proposed collection names and existing scaffolding.

A conflict must be reported before modifying approved behaviour or frozen modules.

---

## 19A. Phase G consolidated contract (Revision 21)

> **Revision 21 scope rule.** Phase G owns only remaining cross-cutting work.
> It must not rebuild Supplier Award outcomes, acknowledgement, comparison,
> Award publication/correction/notification, or any recovery already owned by
> an M8 module. Sections §8A–§8K remain authoritative for Phase F. Where the
> legacy §10.7 or §12 route lists imply duplicated work, this section narrows
> them to the actual remaining gaps.

### 19A.1 G0 repository reconciliation

The reconciliation uses `phase1.md`, ADR 0001, ADR 0002, Approved Revision 20,
the implemented Phase A–F behavior, and the Phase F completion record. The
implementation is decisive for whether a surface exists; the approved design
remains decisive for its intended semantics.

| Requirement | Existing implementation | Remaining gap | Owner | Required route or internal behavior | Security/privacy invariant | Required tests | Disposition |
|---|---|---|---|---|---|---|---|
| Current Supplier Invitation and current issued RFQ | Phase D authorizes the exact current invitation generation; `rfqissuance` owns immutable Supplier-safe issued snapshots | No Supplier-facing read route | `rfqissuance`, consuming a Phase D authorization capability | `GET /supplier-access/invitations/{invitationId}` returns the current invitation plus current issued-RFQ allowlist | Company, Supplier, Invitation and generation come only from Phase D; no token/hash/generation/internal source fields | projection allowlist, current-generation race, foreign Supplier/Invitation 404, real-Mongo chain/version lookup | keep |
| Historical issued RFQ versions | Contractor list/read exists; immutable versions persist | No Supplier history routes | `rfqissuance` | paged list and exact detail beneath the authorized invitation | requested version must belong to the same chain and not be newer than the invitation's current pointer | own history, foreign chain/version 404, expiry/revocation denial | keep |
| Supplier's current draft | Phase E ships `GET /supplier-offers/{invitationId}/draft` | Existing DTO is safe but incomplete as a full own-state projection; no derived response/access status | `supplieroffers` | retain the route and widen only to the Supplier's own stored commercial state plus derived actionable status | exact current recipient owns mutable draft; no claim/replacement/fingerprint fields | exact JSON-key allowlist and current-recipient isolation | narrow |
| Supplier's immutable submitted-version history | Immutable versions, eligibility and withdrawals persist; F10 added an award-facing version list | No Supplier list/detail HTTP surface and no invitation-scoped page index | `supplieroffers` | `GET /supplier-offers/{invitationId}/versions` and `GET .../versions/{versionId}` | only the authorized Supplier + Invitation; current replacement recipient may read the same Supplier's immutable history but never prior recipient identity | own commercial history, replacement visibility, competitor/foreign 404, page/index tests | keep |
| Withdrawal, supersession, expiry and Award eligibility status | Eligibility and withdrawal aggregates exist; comparison derives historical labels; chain advance and eligibility claim CAS different Mongo documents | No Supplier-safe derived status or shared evaluator; the current contracts have no atomic boundary joining latest-version membership to withdrawal acquisition, so a stale pre-read could claim an older still-`eligible` gate after V2 advances | `supplieroffers` | one domain evaluator derives precedence, `isSuperseded` and `canWithdraw`; mutation must re-enforce the same predicate at one defensible repository linearization boundary shared with submission advance—never by check-then-unrelated-CAS | raw claim identity is never projected; pending kind stays an internal discriminator; expiry never disables withdrawal; G5 pauses after RED if no provable primitive exists | latest/expired/superseded table, projection/mutation agreement, both forced real-Mongo orderings, no losing claim/withdrawal residue, Award race | keep; hard G5 conformance gate |
| Award outcomes and acknowledgement | F7/F9 ship Phase D-protected read and CSRF-protected acknowledgement | none | `awards` + `supplieraccess` | preserve existing routes unchanged | frozen per-Supplier allowlist; receipt only | existing F7/F9 tests remain regression gate | remove as completed |
| Privileged route roles | Phase B/C and F handlers use `identity.AuthorizedPrincipal`; owner/admin mutate privileged state, employee reads/prepares only | only whole-router coverage is missing | owning HTTP module | no new authorization service or policy | foreign tenant remains 404; authenticated insufficient role remains 403 | manifest-driven all-route role matrix | remove behavior as completed; keep verification only |
| RFQ issuance reconciliation | chain and invitation advancement services and two owner/admin routes already exist | none | `rfqissuance` | preserve `/rfq-chains/{id}/reconcile` and `/invitations/reconcile` | tenant-scoped, known transitions only | existing B/C real-Mongo tests plus composition presence | remove as completed |
| Supplier-access recovery | Phase D uses same-operation challenge/session recovery, explicit resend, current-generation authorization and fail-closed stale bindings | no correctness-relevant operator transition remains | `supplieraccess` | no generic reconciliation route; stale rows are housekeeping and never authorization | TTL is never authorization; no reactivation by reconciliation | existing D recovery/race suite | remove generic legacy item as completed/narrowed |
| Supplier-offer reconciliation | E4/E7/E8 implement idempotent replacement, submission and withdrawal completion | no contractor surface can resume a persisted `submitting`/withdrawal claim when the Supplier retry is unavailable; an Offer-chain ID is not discoverable in the earliest pre-version crash window | `supplieroffers` | owner/admin `POST /supplier-offer-reconciliations` accepts Invitation ID, Issued RFQ Version ID and the exact target operation ID; Company comes only from the principal; service resolves the unique tuple and the repository remains chain-ID-based internally | never accepts Company ID, edits another module's collections, releases Award claims or invents content | earliest crash window, tuple isolation, exact-operation conflict, idempotency, role and real-Mongo tests | keep, narrow |
| Award reconciliation | F10 ships module-owned reconciliation and owner/admin route | production composition does not mount Award routes or ensure Award indexes | `awards` behavior; composition root wiring | mount the existing route/handlers and indexes in production; do not create a second reconciler | exact F10 behavior unchanged | production/`tenanttest` parity and full-router startup | remove behavior as completed; keep wiring gap |
| External HTTP privacy policy | Supplier groups set three headers; Client success outputs set two; `nosniff` is absent and Client errors are not covered by middleware | inconsistent successes/errors/redirects and duplicated helpers | `platform/http` transport policy; module error maps | one shared external group policy, with route-specific secret-response headers retained where needed | every Client/Supplier response gets no-store/no-cache/no-referrer/nosniff; no error echoes input or infrastructure | table-driven all-route success/error/header tests | keep |
| Bounded input and collection sizes | a few 500/128/254/2000 limits and security rate limits exist | RFQ/Offer line counts, most free text, request bodies, pages, batches, IDs and business-date horizons are unbounded | owning module plus shared pure procurement limits | enforce at HTTP and domain boundaries; repository refuses malformed aggregates | limits apply to new writes only; immutable history remains readable | boundary ±1, bypass-service, property and real-Mongo persistence tests | keep |
| Privacy-safe logs, audit and mail | secret-spy tests exist for invitation/access/delivery; primitive interfaces exist | audit tests are mostly forbidden-field spot checks; Phase E/F concrete audit methods and root wiring are absent; Award audit accepts recipient/correction free text | `audit`, owning modules, composition | concrete allowlisted audit recorders, ensure-once identity, safe logger/mail/delivery sinks | no raw credentials, free text, commercial figures, mail bodies or database errors outside their explicit sink | exact-key allowlists and canary sink-spy tests | keep |
| Error non-disclosure | module-specific maps preserve many settled distinctions | Client unknown errors are unbounded; Supplier Offers collapse unusable access and CSRF into one 403; CSRF header names drift | owning HTTP module with shared bounded external error shape | narrow mapping corrections only; use canonical `X-CSRF-Token` | 403 is only authenticated authorization/CSRF refusal; unusable/foreign Supplier access is non-disclosing 404 | route/error matrix, body-size and no-echo tests | keep, narrow |
| Retention and historical visibility | immutable business records are retained; access is checked against current Phase D binding | policy is scattered and not stated as one contract | each owning module | visibility table in §19G; no new delete service | history remains truthful; current access never resurrects an old generation | lifecycle visibility tests | keep |
| M8 validation/exposure acceptance matrix | requirements are spread across sections/tests | no single Phase H gate | Phase G specification; owning modules enforce | §19H is the acceptance manifest | every invariant has validation, enforcement, mapping, projection and test owner | manifest completeness test/review | keep |
| Composition and OpenAPI | `tenanttest` mounts Awards and ensures its indexes; production currently does neither; parity guard covers only A–D | roots drift; E/F audit wiring absent; no full-router uniqueness/security assertion | `platform/composition` and both roots | parity manifest, complete-router construction, unique operation/schema names, security declarations | route documentation never substitutes for enforcement | fast registration test plus real composed startup | keep |

### 19A.2 Legacy Phase G items removed

| Removed legacy item | Completed checkpoint |
|---|---|
| Generic "reconciliation surfaces for all four modules" | Phase B/C RFQ chain + invitation advancement; D6–D8 in-band access recovery; E4/E7/E8 offer recovery; F10 Award reconciliation. Only the narrow Supplier Offer operator entry point in §19G remains. |
| Supplier-safe Award outcome projection | F7, with composed Supplier isolation in F10. |
| Supplier Award acknowledgement | F9, behind Phase D session + CSRF. |
| Award notification retry and obsolescence | F8. |
| Award correction historical behavior | F6–F8. |
| Comparison privacy and historical labels | F1. |
| Route-level privileged role authorization behavior | Decision A delivered in Phases B, C and F. Phase G verifies the complete manifest only. |
| Supplier Offer submission/withdrawal/replacement algorithms | E4, E7 and E8. Phase G may expose existing recovery but must not reimplement it. |
| Phase H composed acceptance journeys | remain Phase H; Phase G owns only structural full-router and focused cross-cutting verification. |

## 19B. Supplier-facing allowlisted projections

### 19B.1 Ownership and routes

`rfqissuance` owns Supplier Invitation/RFQ projections and declares the narrow
read-authorization capability it consumes. A composition adapter satisfies it
from `supplieraccess.Service`; neither domain module imports the other.
`supplieroffers` continues owning its existing Supplier routes and Phase D
authorization adapter.

```text
GET /supplier-access/invitations/{invitationId}
GET /supplier-access/invitations/{invitationId}/rfq-versions
GET /supplier-access/invitations/{invitationId}/rfq-versions/{versionId}

GET /supplier-offers/{invitationId}/draft                         existing
GET /supplier-offers/{invitationId}/versions
GET /supplier-offers/{invitationId}/versions/{versionId}
```

There is deliberately no session-wide `GET /supplier-access/invitations`.
Phase 1 provides invitation-scoped participation, not a Supplier portal.
Award routes remain exactly F7/F9 and are absent from this projection work.

### 19B.2 Invitation and RFQ allowlist

The current-invitation response exposes only:

```text
invitationId
status = active
expiresAt
currentRfqVersion
responseWindow.status = open | closed
responseWindow.deadline
responseWindow.canRespond
```

The embedded current RFQ and exact historical RFQ detail expose only:

```text
id, rfqNumber, versionNumber, currency, title, issuedAt
deliveryAddress, requiredByDate, responseDeadline, supplierInstructions
lines[]:
  id, materialName, specification, quantityValue, quantityUnit,
  requiredByDate, procurementNotes, sortOrder
```

History summaries expose `id`, `versionNumber`, `issuedAt`,
`responseDeadline`, and `isCurrent`. They use immutable version number as the
cursor and are newest first.

The projection structurally omits Company/Project IDs, RFQ chain ID, Material
ID, source requirement ID, lineage ID, source revision/fingerprint, issuer,
recipient identity, access generation, secret/hash/key version, delivery
attempts, internal notes, costs, Estimate/margin, Supplier directory data,
other Suppliers/Offers, comparison, Award reasoning and reconciliation state.

### 19B.3 Draft and immutable Offer allowlist

The active draft read may expose its exact own commercial state: revision,
currency, line answers and own text/figures, tax discriminator and details,
conditional-charge rules, delivery charge, offer validity, review gates and
derived `canEdit`/`canSubmit`. It never exposes submission claim fields,
replacement barrier fields, source fingerprint, operation IDs, Company ID,
recipient identity, access generation, or another Supplier's data.

An immutable Offer detail may expose:

```text
id, issuedRfqVersionId, versionNumber, currency, submittedAt, offerValidUntil
publicStatus = eligible | expired | superseded | withdrawn | awarded
isSuperseded
canWithdraw
lines[]: own response, quantity, unit price, subtotal, tax, product/lead-time/notes
tax, conditional-charge rules and calculated own amounts
delivery charge, own totals, supplierNotes
withdrawal: withdrawnAt and the Supplier-authored reason, only when withdrawn
```

List items contain identity, dates, derived status, `canWithdraw`, currency and
grand total; callers fetch detail for lines and free text.

`LatestSubmittedVersionID` below is the semantic name for the existing
`SupplierOfferChain.LatestSubmittedID`; Revision 21 does not require a persisted
field rename. One pure domain evaluator owns the rule for both projection and
mutation:

```text
canWithdraw =
    OfferVersion.ID == OfferChain.LatestSubmittedVersionID
    AND Eligibility.State == eligible
```

Expiry is deliberately absent. A latest expired version with an `eligible`
gate remains withdrawable. The formula is evaluated only after the existing
Company, Supplier, Invitation and submitting-recipient ownership checks pass;
those are access preconditions, not a second copy of the business predicate.
For a readable historical version outside the current recipient's mutation
authority, the projection returns `canWithdraw=false` without changing the
domain rule. Recipient replacement therefore preserves read access to the
Supplier's immutable history but does not transfer withdrawal authority over a
version submitted by the prior recipient.

The evaluator returns the first matching domain status in this exact order:

```text
pending -> awarded -> withdrawn -> superseded -> expired -> eligible
```

For non-pending states that domain status becomes `publicStatus`. `pending`
means only `withdrawal_claimed` or `award_claimed`. The evaluator
also returns an internal `PendingKind = withdrawal | award` so reconciliation,
error mapping and tests never conflate the two claim types. Neither the kind,
claim ID, operation ID nor claim payload is serialized to the Supplier. A read
observing pending returns bounded `503 offer_state_pending`.

`isSuperseded` is the orthogonal historical fact that the version is no longer
the chain's latest submitted version. It remains true even when a higher
precedence status is displayed. Thus a withdrawal that wins before a
concurrent submission advances the chain projects `publicStatus=withdrawn`,
`isSuperseded=true`, and `canWithdraw=false`. An unwithdrawn older version
projects `publicStatus=superseded` even if its validity later expires.

`WithdrawOffer` uses the same evaluator for deterministic validation and
projection agreement, but an ordinary chain read is not its concurrency
boundary. The authoritative mutation must atomically re-enforce both facts—
the requested version is still the chain's latest submitted version and its
eligibility is still `eligible`—at one defensible repository linearization
boundary that also contends with submission's chain advance. Only after that
boundary is won may the existing idempotent withdrawal completion continue.
A pre-read may reject obvious stale input, but it can never authorize the
write; specifically, `FindChain` followed by the existing independent
`ClaimEligibility` CAS is insufficient.

Multi-document transactions remain disallowed by default. G5 withdrawal claim
acquisition is the sole approved narrow exception because correctness requires
one atomic boundary across the Offer Chain latest-version fact and Offer
eligibility state. Its majority-write-concern transaction contains only the
tenant-scoped chain read, conditional chain revision/fence write and
`eligible -> withdrawal_claimed` CAS. Immutable withdrawal creation, audit,
mail, logs, reconciliation and calls to other domain services remain outside
the transaction and complete forward from the durable claim.

Every environment executing this workflow must use a transaction-capable
replica set or sharded cluster. Local development and real-Mongo/CI tests may
use a single-member replica set; production requires a properly managed or
redundant topology. Readiness fails clearly when transaction support is absent.
The transaction callback receives one pre-generated Claim ID, operation,
reason and timestamp so driver retries reproduce the same intended write.
Same-operation recovery runs before a new latest-version check and reuses all
stored claim fields, because a V1 claim may legitimately commit before V2 later
advances the chain.

The mandatory real-Mongo orderings are:

```text
withdrawal linearizes first
  V1 is latest
  -> V1 eligibility is successfully acquired for withdrawal
  -> V2 submission advances LatestSubmittedVersionID
  -> both operations may complete
  -> V1 publicStatus=withdrawn, isSuperseded=true

submission linearizes first
  V2 advances LatestSubmittedVersionID
  -> withdrawal attempts V1
  -> rejected as superseded
  -> V1 is not left withdrawal_claimed
  -> no immutable withdrawal exists for that attempt
```

The tests must control the ordering at the actual repository boundary, not by
sleeping or arranging only service-call start times. After either ordering,
they read the chain, eligibility and immutable-withdrawal collections from real
MongoDB and assert the complete final state.

Recipient replacement preserves the Supplier + Invitation immutable history.
The newly verified current recipient may read and explicitly copy that history,
as Approved Decision 9 and §5.3 require, but never sees the prior recipient's
name/email or mutable archived draft. The old recipient loses all access at the
generation change.

## 19C. External HTTP privacy boundary

All Client and Supplier external handlers register beneath one shared
transport-only group. The middleware sets headers before parsing or service
dispatch so framework validation errors, domain errors, redirects, `204`s and
successes all carry:

```http
Cache-Control: no-store, max-age=0
Pragma: no-cache
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
```

This strengthens the existing Client `no-store` and Phase D Supplier policy;
it does not relax either. Contractor operations returning a newly derived raw
link retain their route-specific no-store/no-referrer output as defense in
depth because they are outside the external group.

External JSON errors are at most 4 KiB, contain at most ten field-detail items,
never echo rejected values, credentials, URLs, mail/provider/database text or
stack details, and use module-owned public codes/messages. Unknown external
failures map to a generic bounded `503`. A too-large body maps to bounded `413`.

The Supplier clicked-link route alone accepts the invitation token in a query
and immediately exchanges it for the path-restricted HttpOnly exchange cookie.
Its `303 Location` is the clean `/supplier-access/open` path with no query.
Every later Supplier route uses cookies/body/header values, never a credential
in a URL. The existing M6 Client path-token contract is not redesigned by M8;
its URLs remain protected by the same no-store/no-referrer boundary.

## 19D. Bounded inputs and collections

Limits are enforced on new requests and mutations at both Huma input and
domain/service boundaries. Repository methods reject aggregates violating
count invariants. Existing immutable history is grandfathered and remains
readable; Phase G never truncates or rewrites it.

### 19D.1 Approved limit table

These values are approved and binding for Phase G. A maximum 100-line ×
100-Supplier comparison remains bounded to 10,000 cells, individual BSON/JSON
documents remain comfortably below MongoDB's 16 MiB ceiling, and ordinary
renovation RFQs retain substantial headroom. Existing immutable history remains
grandfathered exactly as stated above.

| Input/collection | Limit | Operational reason |
|---|---:|---|
| JSON body on Client/Supplier external and M8 contractor routes | 1 MiB | protects decode memory and error amplification; enough for the bounded maximum aggregate |
| Client-supplied resource/operation ID | 1–128 ASCII bytes | bounds indexes, audit identity and log noise without constraining UUID/opaque-ID formats |
| Supplier Invitations per RFQ chain | 100 | bounds comparison, outcome generation and invitation advancement fan-out |
| Issued RFQ / Offer lines | 100 | bounds document size and all line-wise calculation/projection work |
| Conditional-charge groups | 25 | bounds rule evaluation and human review |
| IDs in one collection field/request | 100 total; no duplicates | aligns with line maximum and prevents fan-out abuse |
| Page size | default 25, maximum 100 | stable bounded response and cursor work |
| Notification/reconciliation internal batch | default 25, maximum 100 | bounded retries; no new bulk-notify public route |
| Error field details | maximum 10 | prevents validation-response amplification |

Text is trimmed and counted in Unicode code points except emails/IDs, which are
byte-counted in their canonical ASCII form.

| Text class | Limit |
|---|---:|
| recipient/Supplier display name, RFQ title, material/product/brand/lead-time label | 200 |
| SKU and tax registration number | 128 |
| delivery address, tax/charge basis or description, withdrawal/change/unawarded note | 500 |
| specification, procurement notes, Supplier instructions, product description, Supplier line notes, commercial exceptions, whole-offer notes, per-Supplier contractor message | 2,000 |

Existing stricter limits remain stricter: recipient email 254 bytes; tax
registration 128; tax/charge notes 500; withdrawal/change/unawarded notes 500.

### 19D.2 Proposed business-date horizons

```text
ResponseDeadline: after issuance and no more than 180 days after issuance
Invitation ExpiresAt: after now and no more than 180 days after the mutation
OfferValidUntil: after submission and no more than 366 days after submission
RequiredByDate: absent or on/after ResponseDeadline, no more than 5 years ahead
```

Security lifetimes already settled in Phase D (10-minute exchange/challenge,
30-day sliding session, 90-day absolute re-verification) are unchanged.

## 19E. Logging, audit, delivery and mail allowlists

### 19E.1 Allowed sinks

| Sink | Allowed | Never allowed |
|---|---|---|
| Structured logs | request ID, module, bounded event/failure code, server-owned aggregate IDs, Company ID, counts, duration | tokens/hashes/codes/cookies/CSRF, recipient name/email, external operation ID, request/body/header dumps, Supplier figures/free text, mail body/URL, raw error text |
| Audit Event | exact event type, tenant, subject/actor IDs, server-owned related IDs, revisions/version numbers, counts, timestamps and bounded enums | credentials/hashes, recipient identity, correction/withdrawal/notes/message free text, commercial figures, mail/provider/database text, full aggregates |
| Invitation delivery record | recipient email snapshot, generation, version/attempt IDs, channel, status, bounded failure code, timestamps | raw link/token, mail subject/body, provider error |
| Verification delivery record | challenge/attempt IDs, status, bounded failure code, timestamps | recipient address, code/verifier, mail body, provider error |
| Award delivery record | recipient identity snapshot, Supplier/outcome/revision/attempt IDs, generation, status, bounded failure code, timestamps | outcome URL/session credential, contractor message, commercial figures, mail body, provider error |
| Mail message in flight | intended recipient, bounded subject/body; invitation link or verification code only for the mail whose purpose requires it; frozen Supplier outcome projection | persistence, audit or logging of the message; competitor/internal data |
| External Supplier response | only §19B own-transaction allowlist | every forbidden projection field in §19B |

`SupplierOfferAuditRecorder` and `AwardAuditRecorder` receive concrete methods
in `audit.Service` and are wired in both roots. The Award acknowledgement audit
signature drops recipient identity, and correction audit records only that a
correction occurred plus stable IDs/revision—not the free-text reason. Full
text remains only on its owning business record.

Ensure-once audit is an `audit`-owned persistence invariant. M8 ensured events
carry a deterministic dedupe identity, protected by a named partial unique
index scoped by Company + event type + subject + dedupe identity. A duplicate
means the event already exists; an unclassified duplicate remains an error.
This implements the settled Revision 20 audit rule rather than changing any
Phase F transition.

Tests assert exact allowed JSON/BSON key sets per event/record/projection. A
single canary fixture supplies distinct raw tokens, hashes, verification code,
session/CSRF values, recipient data, commercial values, mail body and database
error text to every reachable sink. Forbidden-value checks remain secondary;
adding any unapproved key fails even when its sample value looks harmless.

## 19F. Error non-disclosure matrix

| Condition | Contractor route | Supplier protected read/mutation | Client external route |
|---|---|---|---|
| foreign tenant/resource | 404 | 404 | unusable token 410 |
| foreign Supplier or Invitation under a valid session | n/a | 404 `supplier access unavailable` | n/a |
| expired session/invitation | n/a | 404 | expired decision remains settled 422 after a valid Client grant |
| revoked/replaced Supplier generation | n/a | 404 | revoked/superseded token remains uniform 410 |
| invalid/unknown credential | 401 only for missing/invalid contractor JWT | 404 | 410 |
| missing owned resource after valid authorization | 404 | 404 with resource-neutral body | 410 when token cannot resolve |
| missing/mismatched CSRF | n/a | 403; access is validated before CSRF | n/a |
| authenticated contractor lacks role | 403 | n/a | n/a |
| stale expected revision | 409 | 409 | settled Client terminal conflict 409 |
| idempotency/operation conflict | 409 | 409 | n/a |
| semantically invalid content/deadline | 422 | 422 | 422 |
| rate limited | where defined, 429 | 429 with bounded `Retry-After` | n/a |
| authoritative transition pending | bounded 503 | bounded 503 | bounded 503 |
| unknown infrastructure failure | bounded 503 | bounded 503 | bounded 503 |

Phase G does not install one global domain-error normalizer. Each owning module
keeps its settled distinctions. It makes only two narrow corrections:

1. Phase E's authorization adapter distinguishes unusable access (404) from a
   valid access request with missing/mismatched CSRF (403).
2. Every Supplier mutation uses the settled Phase D `X-CSRF-Token` header;
   `X-Supplier-CSRF` is removed from Phase E/F routes.

## 19G. Reconciliation ownership and historical visibility

### 19G.1 Reconciliation inventory

| Module | Existing owner behavior | Phase G disposition |
|---|---|---|
| `rfqissuance` | reconcile orphan version → chain pointer; advance lagging invitations; owner/admin routes exist | completed; verify production/tenant route presence only |
| `supplieraccess` | same-operation challenge/session recovery; delivery resend; stale generation fails closed; TTL is housekeeping | completed/narrowed; no privileged generic route |
| `supplieroffers` | same-operation replacement, submission and withdrawal recovery | add only owner/admin `POST /supplier-offer-reconciliations`; resolve Company from the principal and the chain from the submitted Invitation + Issued-RFQ-Version tuple, then resume the exact recorded operation |
| `awards` | F10 reconciliation completes published post-work and ensures audit; owner/admin route exists | completed; wire existing indexes/handlers/audit in production |

The request contract is:

```http
POST /supplier-offer-reconciliations
```

```json
{
  "invitationId": "...",
  "issuedRfqVersionId": "...",
  "operationId": "..."
}
```

`CompanyID` comes only from the authenticated principal and is never accepted
from the client. The service resolves the Offer chain through the authoritative
unique tuple `CompanyID + InvitationID + IssuedRFQVersionID`, then passes the
resolved chain ID and exact target operation ID to the existing chain-ID-based
repository/recovery logic. `operationId` identifies the Supplier submission or
withdrawal operation being recovered; it is not a new reconciliation identity.

The route returns:

| Condition | Result |
|---|---|
| same operation, already complete | idempotent `200` with bounded completed result |
| matching recoverable incomplete operation | complete forward, then `200` |
| a different live operation owns the claim | `409` |
| tuple is missing or foreign to the principal's Company | non-disclosing `404` |
| authoritative state remains infrastructure-unresolvable | bounded `503` |

Supplier Offer reconciliation may complete only: a `submitting` draft's
recorded immutable version/eligibility/chain/archive steps; a chain pointer to
the unique recorded candidate; and a withdrawal claim using its recorded ID,
reason and operation. It may report bounded counts/IDs. It may not change
commercial content, infer a missing operation, release an Award claim, touch an
Award/RFQ/access collection, or handle recipient-replacement claims (the
`rfqissuance` replacement operation remains their orchestrator).

### 19G.2 Visibility after lifecycle events

| Event | Contractor history | Supplier history |
|---|---|---|
| Invitation expiry or revocation | invitation, deliveries, offers and audit remain truthful | no access, including history or outcomes, because current Phase D authorization fails |
| Recipient replacement | prior delivery/recipient snapshots and immutable Offers remain; old draft is archived | old recipient loses access immediately; newly verified recipient sees the same Supplier + Invitation RFQ/immutable-Offer history, never old recipient identity or mutable draft |
| Offer supersession | every immutable version and eligibility history remains | own versions remain read-only; older eligible version is labelled `superseded` and cannot be withdrawn |
| Withdrawal | immutable Offer plus separate withdrawal remains | own version, withdrawal time/reason and `withdrawn` status remain visible while current access is valid; if a later submission supersedes it, `publicStatus` remains `withdrawn` and `isSuperseded=true` |
| Award correction | every Award revision, frozen outcome and sent/failed delivery remains per F6–F8 | existing F7 outcome-by-ID behavior remains; Phase G adds no Award list or rewritten outcome |
| Notification obsolescence | pending-only delivery becomes `obsolete`; sent/failed remain unchanged | delivery internals are not exposed; frozen outcome is unchanged |

No Phase G delete or purge API is introduced. Business-history records remain
retained. Existing TTL indexes remain housekeeping only and never substitute
for explicit authorization/time checks.

## 19H. M8 validation and exposure acceptance matrix

Legend: **E** existing Phase A–F behavior; **G** remaining Phase G control;
**N/A** deliberately not a repository/index concern.

| Invariant | Input validation | Domain/service enforcement | Repository/index enforcement | HTTP mapping | Privacy projection | Focused test | Real-Mongo |
|---|---|---|---|---|---|---|---|
| MYR-only Money; no floats; centralized rounding | E | E ADR 0001 + shared kernel | immutable minor units | 422 | own figures only | calculation/property parity E | round-trip E |
| positive exact quantity/unit; full-line quote | E + G count bound | E | immutable snapshot | 422 | Supplier sees requested and own quoted quantity | boundary/property E/G | round-trip E |
| tenant-scoped contractor access | path only; Company ignored | E | Company in every filter/index | 404 | no foreign body | all-route manifest G | tenant query/index E/G |
| Supplier session + Invitation + current generation | credential shape E | Phase D E | token/binding uniqueness and CAS E | 404 | no generation/token/hash | generation/race E | binding CAS E |
| CSRF on every Supplier mutation | canonical cookie/header G | Phase D E | N/A | 403 | absent from bodies/logs | route manifest G | N/A |
| secret/code/session values hash-only | exact encoding E | E | unique/partial/TTL indexes E | neutral 404/429/503 | never projected | canary sink allowlist G | persisted-spy E/G |
| one stable invitation per chain + Supplier | ID/text/date/count G | E | named unique E | 409/422 | exact invitation only | limit/state G | unique/race E |
| issued RFQ immutable, bounded and versioned | body/text/line/date G | E + G | named chain/version and operation indexes E | 413/409/422 | §19B allowlist G | ±1/fingerprint G | insert/index E/G |
| one unfinished draft; exact current recipient | body/text/line G | E | partial unique + CAS E | 404/409/422 | own active draft only | replacement/edit races E/G | unique/CAS E |
| complete explicit Offer line response | enum/count G | E | frozen version | 422 | own answer only | completeness E/G | round-trip E |
| tax/charge discriminator and non-overlap | text/group/ID bounds G | shared kernel E | immutable rules | 422 | own rules/amounts only | boundary/property E/G | round-trip E |
| Offer validity, withdrawal, supersession, Award gate | horizon G | shared latest+eligible evaluator G; mutation uses the approved narrow transaction-backed chain fence | majority transaction atomically fences the chain revision and claims eligibility; subsequent completion remains recoverable | 409/422/503 | precedence, `isSuperseded`, `canWithdraw`; pending kind internal only | latest/expired/superseded, projection/mutation agreement and callback retry G | force withdrawal-first, submission-first, rollback and uncertain-commit recovery; assert chain + eligibility + immutable-withdrawal final state G |
| operation idempotency + optimistic concurrency | ID format/expected revision G | E | named unique/CAS E | 409 | operation IDs omitted | retry/conflict E/G | one-winner E |
| Award selection/publication/correction | existing F input | F3–F6 E; unchanged | claims/revision indexes E | 409/422/503 | F1/F7 E | existing F suite | existing F races |
| Outcome/notification/ack privacy | existing F input | F7–F9 E; unchanged | unique/CAS E | 404/409/503 | frozen own-only E | F7–F9 + allowlist G | existing F tests |
| ensure-once audit | bounded IDs/enums G | owning service emits G | audit partial unique dedupe G | domain action unchanged | audit exact-key allowlist G | crash/adoption G | duplicate winner G |
| Supplier Offer operator recovery | tuple/operation bounds G | resolve principal Company + exact tuple/operation G | unique Company + Invitation + Issued-RFQ-Version lookup; chain-ID recovery E | 200/404/409/503 | no Company input or claim payload | complete/recover/conflict/isolation G | pre-version crash recovery G |
| external headers and bounded errors | body limit G | module maps G | N/A | 413/4xx/503 | no echo + four headers | all-route matrix G | N/A |
| pagination/batch bounds | page/cursor/batch G | module clamps/refuses G | supporting named indexes G | 422 | summaries only | cursor edges G | stable page/index G |
| module boundaries and complete composition | N/A | narrow capabilities/adapters | both roots ensure all indexes G | documented security | unique schemas/operations | AST + full-router G | composed startup G |

Phase H must treat every row without a focused test—and every applicable
repository row without a real-Mongo test—as a failed acceptance gate.

## 19I. Composition and OpenAPI verification

1. Production and `tenanttest` must construct the same M8 repositories,
   keyrings, services, adapters, audit recorders and handler registrations, and
   call every M8 `EnsureIndexes`. The current observed drift—Awards mounted and
   indexed only in `tenanttest`—is a Phase G red test, not new Phase F behavior.
2. Expand the existing composition-root AST manifest through Phases E, F and G.
   It must include Supplier Offer/Award audit options and the new projection and
   reconciliation adapters.
3. Extend import-boundary guards for every new consumer-owned capability.
   No domain module may import another M8 domain module or its repository.
4. Build one complete Huma router in a fast registration test. Registration
   must not panic; operation IDs and generated schema names are globally unique.
   The same test asserts route ownership and method/path uniqueness.
5. Register OpenAPI security schemes for contractor bearer auth, Supplier
   exchange cookie and Supplier session cookie. Public open/Client routes have
   explicit empty security; protected Supplier reads require the session-cookie
   declaration; Supplier mutations additionally document the canonical CSRF
   header. Documentation is verified against the route manifest and never
   replaces runtime authorization.
6. A real composed `tenanttest` startup remains mandatory because it proves
   index construction and the actual adapters, not only route registration.

## 19J. Compact inline Phase G TDD sequence

- **G0 — reconciliation and frozen scope.** Commit no code. Accept the
  keep/narrow/remove matrix, the shared withdrawal evaluator and tuple-based
  reconciliation locator, §19D limits and §19K M8 retention scope; replace the
  legacy Phase G todo with this inline sequence. Revision 21 approval closes G0.
- **G1 — composition/OpenAPI RED.** Add failing production/`tenanttest` parity,
  complete-router startup, unique operation/schema, security declaration and
  import-boundary tests. Minimal GREEN mounts existing Award indexes/routes and
  establishes the complete M8 route manifest; no Award behavior changes.
- **G2 — shared external HTTP boundary.** RED across every Client/Supplier
  success, validation error, domain error, redirect and `204`. Minimal GREEN
  adds the four shared headers, bounded/no-echo errors and the canonical
  `X-CSRF-Token`, preserving module-specific statuses. The approved numeric
  request-body limit remains in G3 so all limits land together. Build the
  complete composed router for these tests; isolated handler tests cannot prove
  headers on framework validation errors, redirects, `204`s or routes owned by
  another module.
- **G3 — limits and validation.** RED at limit−1/limit/limit+1 and direct-service
  bypass for request bodies, IDs, text, lines, groups, pages, batches and
  approved date horizons. Minimal GREEN adds the approved external body cap,
  owning-domain validation and repository guards; immutable history is never
  rewritten.
- **G4 — Supplier Invitation/RFQ projections.** RED on exact JSON keys,
  current/historical chain membership, current-generation race, pagination and
  foreign/expired/revoked access. Minimal GREEN adds the `rfqissuance`-owned
  projection capability, adapter, routes and index usage.
- **G5 — Supplier Offer state/history and withdrawal conformance.** RED on
  complete own-draft projection, immutable history, exact status precedence,
  internal pending-kind discrimination, replacement visibility and pagination.
  RED the shared evaluator and mutation together: latest eligible succeeds;
  latest expired eligible succeeds; superseded eligible is rejected; and
  projection and mutation agree. In real MongoDB, force both authoritative
  orderings: withdrawal eligibility acquisition before V2 chain advance permits
  both and projects V1 as `withdrawn` plus `isSuperseded=true`; V2 chain advance
  before V1 withdrawal rejects V1, leaves no `withdrawal_claimed` gate and
  creates no immutable withdrawal. A stale pre-read followed by the existing
  independent eligibility CAS is not GREEN. Because the inspected repository
  contracts cannot prove this today, pause at G5 after witnessed RED and present
  a concrete linearization/recovery design for review. Do not implement an
  informal check-then-write fix. Submission, Award and existing terminal
  withdrawal semantics otherwise remain frozen.
- **G6 — audit/log/mail allowlists.** RED on exact event/record keys and canary
  sink leakage. Minimal GREEN adds concrete Phase E/F audit methods,
  privacy-safe signatures, ensure-once audit persistence and both-root wiring.
- **G7 — narrow Supplier Offer reconciliation.** RED the earliest pre-version
  submission crash and every later submission/withdrawal window through
  `POST /supplier-offer-reconciliations`: same complete operation and recovered
  incomplete operation return `200`; different live operation `409`; missing or
  foreign tuple `404`; unresolved infrastructure `503`; employee forbidden.
  Minimal GREEN resolves Company from the owner/admin principal, resolves the
  chain by Company + Invitation + Issued-RFQ-Version, and passes its ID plus the
  exact target operation to existing frozen-claim completion. No generic
  reconciler or cross-module collection access.
- **G8 — retention/error matrix acceptance.** RED lifecycle visibility and all
  error rows; minimal GREEN only for discrepancies not already resolved in
  G2–G7. Re-run all Phase C–F focused suites to prove no settled contract moved.
- **G9 — Phase G verification.** Format, vet, build, focused race tests and
  serialized `go test ./... -count=1 -p 1`; update completion records only
  after the evidence is reported. Pause before Phase H.

### 19J.1 Verification cadence

Every checkpoint runs its focused package/service/HTTP gates before it can be
marked complete. Full-repository gates are reserved for cross-cutting
boundaries:

| Boundary | Required verification |
|---|---|
| each of G1, G2 and G3 | focused RED/GREEN/regression gates for that checkpoint |
| after G3 | formatting, `go vet`, `go build`, and serialized `go test ./... -count=1 -p 1` |
| G4 | focused projection/isolation suite plus real-Mongo tenant, generation/chain-advance race, chain-membership and pagination tests |
| G5 | focused evaluator/projection/mutation suite plus both controlled real-Mongo withdrawal/submission orderings; hard pause if no linearization boundary is proven |
| after G6 | focused exact-sink/canary tests, then formatting, `go vet`, `go build`, and serialized `go test ./... -count=1 -p 1` |
| G7 | focused route/recovery suite plus real-Mongo tuple isolation, earliest crash, replay and competing-operation races |
| G8 | focused retention/error matrix and affected Phase C–F regressions; no routine full repository run |
| G9 | mandatory formatting, `go vet`, `go build`, focused race tests, and final serialized `go test ./... -count=1 -p 1` |

The complete-router G2 suite must exercise at least one actual success, mapped
domain error, framework validation error, redirect and `204` response from the
registered Client/Supplier surface. It asserts the four privacy headers and
bounded/no-echo error contract on those real composed responses.

## 19K. Approved Phase G product decisions

1. **Scale and date bounds — approved.** §19D's 100 invitations/lines,
   25 groups, 1 MiB body, text/page/batch limits and 180/366-day business
   horizons are binding for Phase G new writes. Existing immutable history is
   grandfathered and remains readable.
2. **M8 physical security-record retention scope — approved.** Phase G adds no
   physical deletion, purge API, new or modified TTL index, or security-record
   retention enforcement. Existing already-approved TTL housekeeping is left
   unchanged and is never treated as authorization. Revision 21 deliberately
   does not invent retention durations or legal-hold rules; any future physical
   retention policy is outside M8 and requires its own Product/Security-approved
   revision before implementation.

Neither decision reopens Phase F. The shared withdrawal evaluator, tuple-based
reconciliation locator, §19D values and M8 retention scope are fully approved
product contracts. This approval does not preapprove a G5 Mongo linearization
mechanism. Because the inspected repositories cannot currently prove the two
required orderings, G5 must pause after witnessed RED for a focused technical
design review before any GREEN implementation.

---

## 20. Final design status

Revision 21 is approved and Phases A–G are implemented and verified. The
transaction-backed G5 chain fence, tuple reconciliation locator, §19D limits
and approved no-deletion/no-new-TTL retention scope are now the implemented
baseline. The Phase H contract in §21 is proposed and requires review before
implementation begins.

The next step is:

```text
implemented and verified Phases A–G
→ review proposed Phase H acceptance contract
→ approve or amend the compact H0–H8 sequence
→ implementation only on an explicit subsequent instruction
```

Do not create a second large TDD-plan file. The approved specification remains the implementation source of truth.

---

## 21. Proposed Phase H acceptance and closure contract

### 21.1 Boundary and evidence strategy

Phase H is an acceptance and closure phase. It adds no commercial workflow,
Supplier credential, financial semantic, recovery owner or domain module. It
does not turn focused unit cases into composed duplicates merely to increase
coverage counts.

Three strategies were considered:

1. one very large end-to-end test, which is realistic but difficult to diagnose;
2. rerunning every Phase A–G invariant through HTTP, which duplicates focused
   evidence and makes the closure suite unnecessarily slow; and
3. a layered acceptance suite: one golden composed journey, small composed
   branch journeys, recovery journeys at authoritative crash windows, and an
   evidence manifest that points to focused/real-Mongo proofs.

The third strategy is authoritative. Each journey uses the complete
`tenanttest` router and real MongoDB. Focused tests remain the authority for
calculation properties and repository one-winner mechanics; Phase H proves
that the shipped modules, adapters, middleware, authorization and persistence
compose into the promised business result.

“Persisted result” below means an assertion against owning repositories after
the HTTP journey, not an HTTP response inferred to imply storage. Controlled
failures are injected at existing capability, mail or repository seams. Tests
must not use sleeps to order races.

### 21.2 H0 acceptance evidence ledger — business and isolation

| Requirement | Existing focused evidence | Remaining composed evidence | Owner | Acceptance journey | Failure/race variant | Expected failure / HTTP and persisted result | Disposition |
|---|---|---|---|---|---|---|---|
| Complete RFQ-to-acknowledgement journey | B–F service/repository suites; Phase F outcome, notification and acknowledgement tests | One real-router journey through M7 ready RFQ, issue, invitation send, open/verify, Offer submit, comparison, Award publication, explicit successful notification of selected and unsuccessful outcomes, each Supplier's own outcome view, and each acknowledgement | composition; domain writes remain module-owned | H1 golden journey with selected and unsuccessful Suppliers | duplicate operation at submission, publication, notification and acknowledgement | first writes use route-defined 201/202/200; repeats converge; exactly one immutable version, Award revision, outcome per participant, sent delivery per explicit notification operation, and acknowledgement | keep |
| RFQ amendment and invitation advancement | `TestM8RFQIssuance_ComposedLifecycleRolesAndTenantIsolation`; Phase C advancement races | Supplier session observes V1, owner issues V2, invitation advances, historical V1 remains readable and current view becomes V2 | `rfqissuance` | H2 amendment branch | revoked invitation and concurrent generation change | revoked generation stays 404 and is not advanced; active invitation points to V2; both immutable RFQ versions remain | narrow |
| Recipient replacement | Phase C atomic replacement plus Phase E durable replacement-claim barrier and races | Old recipient/session fails closed; new recipient verifies; mutable workspace is archived/claimed according to the settled state | `rfqissuance` + `supplieroffers` capability | H2 replacement branch | crash after replacement claim and retry of same operation | bounded 503 while pending; retry completes; previous generation cannot access; one workspace slot remains | narrow |
| Offer supersession | submission recovery and G5 projection/status tests | Submit V1 then V2 through Supplier routes and read history/detail | `supplieroffers` | H2 supersession branch | repeated V2 submission operation | retry returns same version; V1 is `superseded`, V2 is latest, no duplicate immutable version | keep |
| Offer withdrawal | G5 evaluator, transaction orderings, rollback and uncertain-commit tests | Withdraw through protected route, then read persistent own history; include withdrawn-then-submitted projection | `supplieroffers` | H2 withdrawal branch | same operation retry and later V2 submission | 200/created contract remains idempotent; one immutable withdrawal; V1 `withdrawn` and optionally `isSuperseded=true` | narrow |
| Partial Award with retender-required lines | F3 completeness/unawarded reason and F5 publication suites | Compose one selected lineage and one `retender_required` unawarded lineage through draft and publication routes | `awards` | H3 partial Award branch | omitted or unreasoned line | 422; no Award revision and no terminal claim for the refused draft; valid branch publishes frozen decision | keep |
| Monotonic Award correction | F6 locked-baseline/delta and real-Mongo correction proofs | Publish baseline, add a previously unawarded eligible lineage, read both immutable revisions and frozen outcomes | `awards` | H3 correction branch | remove/reduce/reassign baseline selection | 422 `award_correction_not_monotonic`; baseline and terminal claims unchanged; valid delta creates exactly one newer revision | keep |
| Notification failure and explicit retry | F8 bounded failure, retry, obsolescence and audit tests | Trigger provider failure through the contractor route, inspect the failed attempt, prove the Supplier can still view and acknowledge the non-authoritative notification's outcome, then retry explicitly | `awards` | H3 delivery branch | repeat retry operation and correction obsoleting a pending attempt | send action preserves Award; failed attempt has bounded code; outcome view/acknowledgement still succeed; retry has one new attempt; duplicate operation converges; only pending is obsoleted | keep |
| Company A cannot access Company B M8 resources | existing composed issuance/Award tenant checks plus all focused repository filters | Route-manifest substitution across one representative read and mutation per M8 owner | owning module | H4 contractor isolation matrix | valid low-role same-tenant caller versus privileged foreign caller | same-tenant insufficient role 403; foreign resource 404; no foreign mutation | narrow |
| Supplier A cannot access Supplier B invitation, Offer or outcome | Phase D binding tests; F7/F9 ownership tests; G4/G5 projection isolation | Two verified Supplier sessions attempt cross-invitation, cross-Offer and cross-outcome substitutions | `supplieraccess` authorization, owning read module | H4 Supplier isolation matrix | IDs copied between Suppliers | neutral 404; no view, draft, withdrawal or acknowledgement mutation | keep |
| Replaced/revoked generations fail closed | Phase C/D generation, rotation, revocation and binding races | Reuse old exchange/session after replacement and revocation through current RFQ, Offer and outcome routes | `supplieraccess` + `rfqissuance` capability | H4 generation matrix | generation changes during authorization | neutral 404 and no renewal/binding advance; fresh verification alone restores scoped access where allowed | narrow |
| Client grants remain resource-scoped | M6 composed grant, rotation and substitution suite | No duplicate M8 proof; retain M6 composed evidence in final manifest | `access` | evidence-only H0 row | quotation/grant substitution | existing neutral external failure and no cross-resource mutation | already proven |
| Contractor 403 versus foreign 404 | composed Phase B/F checks and route-focused role matrices | Manifest-driven representative checks for every privileged M8 action category, not every endpoint body | owning handler + `identity` | H4 role/non-disclosure matrix | employee versus foreign owner/admin | 403 only for known same-tenant resource; 404 for foreign resource; no writes | narrow |
| External response allowlists | G2 headers/errors; G4/G5 exact projections; F7 outcomes; canary sink tests | Capture every response in H1–H4 and assert forbidden-field/canary set globally | projection owner | all H1–H4 journeys | success, 4xx, redirect and 204 | no internal IDs, competitor facts, operation/claim IDs, hashes, tokens or forbidden fields; four privacy headers where applicable | narrow |

### 21.3 H0 acceptance evidence ledger — recovery

| Requirement | Existing focused evidence | Remaining composed evidence | Owner | Acceptance journey | Failure/race variant | Expected failure / HTTP and persisted result | Disposition |
|---|---|---|---|---|---|---|---|
| RFQ issuance advancement | Phase B insert-before-pointer recovery and chain reconciliation | Inject crash after immutable version insert; call owner/admin chain reconciliation | `rfqissuance` | H5 issuance recovery | same and competing reconciliation | 200 for complete-forward/idempotent recovery; chain points to existing version only; no invented version/content | keep |
| Invitation advancement | Phase C advancement/reconciliation tests | Issue V2 with advancement incomplete; call invitation reconciliation route | `rfqissuance` | H5 invitation recovery | revoked invitation and repeated reconcile | 200; active invitations advance, revoked invitation stays unchanged, no new invitation/delivery | keep |
| Recipient replacement | durable barrier and same-operation service recovery | Interrupt after claim through existing replacement route seam; repeat exact operation | `rfqissuance` owns route; `supplieroffers` owns workspace claim | H5 replacement recovery | changed input or competing operation | same input completes; conflict is 409; one invitation generation and one unfinished-workspace slot | keep |
| Offer submission | submission crash-window suite and G7 tuple reconciliation | Inject earliest pre-version and a later incomplete window; call tuple + exact-operation route | `supplieroffers` | H5 submission recovery | foreign tuple, different live operation, DB failure | 200 complete/idempotent; 404 foreign/missing; 409 competing; bounded 503 unresolved; exactly one immutable version and eligibility | keep |
| Offer withdrawal | G5 durable claim recovery and G7 reconciliation | Crash after transaction commit before withdrawal insert; call tuple reconciliation | `supplieroffers` | H5 withdrawal recovery | uncertain commit and changed reason | exact recorded operation completes once; conflicting input 409; terminal claim never released | keep |
| Award publication | F5 crash-window and F10 reconciliation suite | Interrupt after authoritative revision insert and invoke Award reconciliation | `awards` | H5 publication recovery | competing operation and repeated reconciliation | 200 complete/idempotent or 409 competing; chain advances only to recorded revision; claims become terminal | keep |
| Missing post-publication Award work | F10 missing outcome/audit/delivery repair tests | Remove/interrupt chain advance, outcomes or ensure-once audit after publication and reconcile publicly | `awards` | H5 post-publication recovery | partial existing outcomes and audit duplicate race | only missing records are created; existing frozen outcome/delivery unchanged; audit identity has one winner | keep |
| Supplier Offer reconciliation locator | G7 route, tuple isolation, earliest crash and concurrency tests | Include exact owner/admin route in the composed recovery suite | `supplieroffers` | H5 locator matrix | body attempts Company ID, foreign tuple, alternate operation | Company comes from principal; 200/404/409/bounded 503 as settled; no claim payload exposed | narrow |

No required recovery entry point is missing. Phase H must use the existing
routes and same-operation semantics; it must not add a generic reconciler or
cross-module repository access.

### 21.4 §19H validation/exposure closure ledger

Every §19H row remains a milestone gate. “Already proven” means Phase H records
the named evidence without recreating it. “Narrow” means only the missing
composed assertion is added.

| §19H invariant | Existing focused/real-Mongo evidence | Remaining Phase H evidence | Disposition |
|---|---|---|---|
| MYR Money, no floats, centralized rounding | ADR 0001; shared-kernel properties, Phase E/F parity and Mongo round trips | H1 asserts exact persisted and projected totals for one selected Offer | narrow |
| Positive exact quantity/unit; full-line quote | RFQ/Offer boundary properties and immutable round trips | H1 submits exact quantities; H6 records ±1 evidence without duplicating properties | narrow |
| Tenant-scoped contractor access | module repository filters, G route manifest and composed B/F checks | H4 representative read/mutation substitution across every M8 route owner | narrow |
| Supplier session + Invitation + generation | Phase D CAS/race and binding suites | H4 cross-Supplier and stale-generation composed calls | narrow |
| CSRF on Supplier mutations | Phase D runtime suite and G canonical-header manifest | H1/H4 use real cookies/header; omit/mismatch once per mutation owner | narrow |
| Hash-only secrets/codes/sessions | Phase C/D persisted spies and G sink canaries | final evidence manifest only; H4 forbidden-field scan | already proven |
| One stable invitation per chain/Supplier | Phase C unique/race tests and G limits | H2 proves stability across amendment | narrow |
| Immutable bounded issued RFQ versions | Phase B/G fingerprints, limits and indexes | H2 proves V1 remains unchanged after V2 | narrow |
| One unfinished draft; exact recipient | Phase E replacement claim/barrier CAS races | H2/H5 composed replacement and recovery | narrow |
| Complete explicit Offer responses | Phase E completeness/property and round trips | H1 valid full response; H6 records refused incomplete request | narrow |
| Tax/charge discriminators and non-overlap | shared calculation kernel, Phase E/F property tests | H1 one representative commercial shape; no HTTP duplication of every property | narrow |
| Offer validity/withdrawal/supersession/Award gate | G5 evaluator and mandatory transaction orderings | H2 protected route/history projection; H5 recovery | narrow |
| Operation idempotency/concurrency | named unique/CAS one-winner suites throughout A–G | H6 runs exactly 20 concurrent requests with one shared operation ID plus one request with a different operation ID against one representative ensure-once workflow | narrow |
| Award selection/publication/correction | complete F3–F6 focused and race suites | H1/H3 composed publication, partial Award and correction | narrow |
| Outcome/notification/ack privacy | F7–F9 focused/real-Mongo and G allowlists | H1/H3 Supplier-session outcome and acknowledgement | narrow |
| Ensure-once audit | G6 crash/adoption and duplicate-winner tests | H5 checks repaired records; H6 duplicate-operation audit count | narrow |
| Supplier Offer operator recovery | G7 focused/real-Mongo locator suite | H5 composed owner/admin call only | narrow |
| External headers and bounded errors | G2 complete-router response-class matrix | H4 globally scans H responses; no duplicate response-class matrix | already proven |
| Pagination/batch bounds | G3/G4/G5 cursor, index and stable-page tests | H6 one approved-limit page/batch baseline | narrow |
| Module boundaries and complete composition | ADR 0002; G1 AST, full-router and composed-startup tests | H7 reruns manifest/startup and proves readiness/deployment/shutdown closure | narrow |

Milestone completion is forbidden if any row lacks the focused test named by
§19H or an applicable real-Mongo test. The final record must link evidence by
test name/package; a green `go test ./...` alone is insufficient.

### 21.5 Full-router, deployment and operational acceptance

The repository audit found the following already-proven facts:

- production and `tenanttest` call every M8 `EnsureIndexes`, wire the same M8
  foundations and handlers, and verify withdrawal transaction support;
- complete-router registration pins unique paths, operation IDs, schema names
  and documented security;
- the external response-class matrix covers success, mapped error, framework
  validation error, redirect and `204` privacy headers;
- local Docker and real-Mongo tests use replica-set-capable MongoDB; and
- `cmd/api` contains bounded startup contexts, signal handling and graceful
  shutdown.

Two RED closure gaps are authoritative:

1. `/ready` currently pings MongoDB only. Startup fails on unsupported
   transactions, but a point-in-time readiness response can still report `ok`
   without checking the G5 capability. H7 must make readiness depend on both
   connectivity and the existing side-effect-free, read-only transaction-
   capability probe. The probe runs under a short bounded context. Unsupported
   or unavailable topology returns a generic external `503` with code
   `service_not_ready`; replica-set names, topology details, driver errors and
   connection strings are restricted to privacy-safe operational diagnostics.
2. `README.md` documents the local single-member replica set, but repository
   documentation does not explicitly state transaction-capable CI and
   production topology. H7 must document CI as replica-set-capable and
   production as a managed/self-hosted replica set or sharded cluster with
   appropriate redundancy. It must not promise that a single-member topology
   is production-safe.

H6 adds a basic bounded-load acceptance baseline, not a performance target. At
the approved M8 maximums it proves bounded request rejection/acceptance and
stable pagination. Its duplicate-operation baseline is exactly 20 concurrent
requests using the same operation ID plus one request using a different
operation ID against one representative ensure-once workflow. The matching
requests receive idempotent results, the different operation conflicts, and
the final state contains one authoritative business record and one ensure-once
audit record where applicable. The baseline also proves no structural panic or
unbounded response. It sets no latency or throughput SLA and authorizes no
optimization.

Operational failure expectations:

| Condition | Expected acceptance result |
|---|---|
| oversized request or over-limit collection/page/batch/date | bounded 413/422; no partial domain write |
| duplicate/retry storm | exactly 20 same-operation requests converge idempotently; one different-operation request conflicts; one authoritative record and one ensure-once audit record where applicable |
| mail provider failure | domain action and failed intent persist with bounded code; explicit retry only |
| pending authoritative transition | bounded 409/503 per owner; no claim release or invented content |
| Mongo unavailable | `/ready` and affected operation return bounded 503; no echoed driver/provider detail |
| transaction unsupported | startup fails with privacy-safe operational diagnostics; `/ready` returns generic `503 service_not_ready`; withdrawal claim cannot begin |
| shutdown signal | new work stops, in-flight HTTP receives the existing bounded shutdown window, Mongo closes after server shutdown |

### 21.6 Compact inline Phase H TDD/verification sequence

- **H0 — acceptance reconciliation, design only.** Freeze this ledger against
  Phase A–G evidence, current route/role manifests and §19H. Record already-
  proven rows, the two RED deployment gaps and the absence of missing recovery
  entry points. No production code or tests.
- **H1 — golden composed journey.** Write and run one complete real-router,
  real-Mongo Contractor/Supplier journey from M7 RFQ creation through Award
  publication, explicit successful selected and unsuccessful outcome
  notifications reaching `sent`, each Supplier viewing only its own outcome,
  and each acknowledging receipt. Correct only genuine composition/acceptance
  defects; do not add business behavior.
- **H2 — lifecycle branch journeys.** Write and run acceptance evidence for
  amendment advancement, recipient replacement, Offer supersession and
  withdrawal. Assert old generations fail closed and immutable histories
  remain visible.
- **H3 — Award branch journeys.** Write and run acceptance evidence for the
  partial retender-required Award, locked-baseline monotonic correction,
  notification failure/explicit retry,
  frozen Supplier outcomes and receipt acknowledgement. A failed notification
  does not block the Supplier from viewing or acknowledging its outcome because
  delivery is not authoritative.
- **H4 — isolation and exposure closure.** Run the manifest-driven tenant,
  Supplier, generation, role and forbidden-field matrix through the complete
  router. Reuse M6 evidence for Client grant scoping. Preserve same-tenant 403
  versus foreign-resource 404.
- **H5 — public recovery acceptance.** At deterministic existing seams, force
  each recorded crash window and recover through the owner/admin or same-
  operation entry point. Assert persisted end state and idempotency; never use
  sleeps, invent financial content or release terminal claims.
- **H6 — §19H and operational closure.** Fill only missing matrix evidence;
  exercise approved limits, the fixed 20-same-operation-plus-one-different
  concurrency case, audit dedupe, mail failure, pending transitions and
  database unavailability. Record a bounded-load baseline without performance
  tuning or SLA claims.
- **H7 — full-router and deployment readiness.** Re-witness composition/index,
  OpenAPI/security and privacy-header manifests. RED `/ready` on unsupported
  transactions, then narrowly include the existing side-effect-free read-only
  probe under a short bounded context. Return only generic external
  `503 service_not_ready`; keep topology/driver details in privacy-safe
  diagnostics. Complete local/CI/production topology documentation and verify
  graceful startup and shutdown.
- **H8 — final Milestone 8 verification and record.** Run format, vet, build,
  focused high-risk race suites and serialized `go test ./... -count=1 -p 1`.
  Review every §19H row and acceptance ledger entry by named evidence before
  updating `todo.md`, `done.md` and the specification status. Record explicit
  non-goals and stop; do not begin Purchase Orders or another milestone.

Each checkpoint begins by writing and running the missing acceptance evidence.
If it fails, witness and diagnose the RED before making the minimum GREEN
correction. If it passes immediately, record that the composed behavior was
already correct and make no production change. Actual newly introduced
behavior still requires RED first: transaction-aware `/ready`; documentation
or configuration validation implemented as executable checks; and genuine
defects exposed by the composed journeys. Full-repository verification is
mandatory only at H8 unless a cross-cutting defect justifies an earlier gate.
Real-Mongo is mandatory for H1–H6 persistence/recovery assertions and H7
readiness. Race instrumentation must control authoritative CAS, transaction or
commit boundaries rather than elapsed time.

### 21.7 Already-proven requirements not duplicated in Phase H

- ADR 0001 Money/Quantity representation, currency compatibility, fixed-point
  rates and round-half-up calculation properties;
- ADR 0002 import boundaries and consumer-owned capability direction;
- every named uniqueness/index, one-winner CAS and G5 transactional ordering;
- raw secret/code/session hashing and persisted/log/audit/mail canary absence;
- the full G2 external response-class privacy-header and bounded-error matrix;
- all tax, conditional-charge, delivery and Award correction property cases;
- Phase D rate-limit arithmetic, confirmation/session races and TTL
  housekeeping semantics;
- Client quotation Access Grant resource scoping from the existing composed M6
  suite; and
- individual role checks already pinned by handler suites, except the compact
  H4 manifest check needed to prove the complete composed surface.

### 21.8 Closure record and explicit non-goals

H8 may mark Milestone 8 complete only after all ledgers are green and the final
evidence record is written. The completion statement must not claim Purchase
Orders, committed-cost updates, Supplier PO acceptance/decline/change requests,
payments, profitability completion, rescission/cancellation, automated ranking,
full AI procurement, frontend completion, delivery confirmation, fulfilment or
invoice/e-Invoice support. Those remain outside M8.

This proposed Phase H contract is ready for review. No Phase H production code
or tests are authorized until the amendment is approved explicitly.
