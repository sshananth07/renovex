# RP4E0 — Provider-Neutral 3D Asset Generation Jobs + Hunyuan Provider (Design Spec)

## 1. Scope and boundary

RP4E0 builds the first real implementation of the M8.5C design spec's §37
"Durable Async Job Architecture" (`SpatialAssetGenerationJob`, a sibling of
the spec's already-named `SpatialReconstructionJob`/
`SpatialDesignGenerationJob`/`SpatialAssetImportJob`), a provider-neutral
`AssetGenerationProvider` boundary, and one concrete implementation against
the private Hugging Face `renovex-hunyuan3d-runtime` Space (Hunyuan3D on
Gradio/ZeroGPU). A validated, published result becomes a new immutable RP4D
`VisualAssetVersion` — RP4D's `assign_visual_asset`/`clear_visual_asset`
operations remain the *only* way any RoomDraft fixture/object ever binds to
it. RoomDraft position/rotation/dimensions/wall-attachment/room geometry
remain canonical; generated mesh geometry is presentation only.

**Explicitly out of scope**: MapAnything (no dependency, interface,
placeholder, TODO, or compatibility layer — deferred indefinitely per
current architecture decision; RoomPlan → RoomDraft/SpatialRoomVersion is
the sole authoritative capture path), GLM orchestration, prompt-to-
reference-image generation, background removal/object isolation, Hunyuan
Paint/PBR, material editing, iOS asset rendering, provider migration,
AI-generated room geometry, RP4E1+.

## 2. Ground truth from the audit

- `internal/platform/jobs.JobQueue`/`InProcessQueue` exists, is fully
  non-durable (in-memory, fire-and-forget, lost on restart), and is
  currently unused anywhere in the codebase.
- `internal/ai`'s `AIGenerationBatch` (`{Processing,Completed,Failed}`
  status, `FindBatchByOperationID` idempotency, `MarkBatchFailed`) is the
  closest existing durable-record precedent, but every current caller
  invokes the provider **synchronously inline** within the HTTP request —
  fine for a quick LLM call, wrong for a multi-minute GPU generation.
- `internal/platform/ai.Client` is the only existing external-HTTP-service
  client in the Go backend (bearer-auth, bounded `http.Client`, typed
  sentinel errors mapped from the remote service's own error codes, no
  provider-internal detail ever leaks past the client). This is the shape
  the new Hunyuan client mirrors.
- `config.AIServiceURL`/`AIInternalToken` are optional-together: unset in
  dev, both required together when set, feature disabled cleanly when
  absent. RP4D's `VISUAL_ASSET_CAPABILITY_ACTIVE_VERSION`/`_KEY_V{n}` use
  the identical optional-at-boot/fails-closed-at-use pattern. Both are
  mirrored for Hugging Face config.
- `awards/chain_repository_mongo.go` and siblings establish the
  `FindOneAndUpdate` + `SetUpsert`/`ReturnDocument(After)` atomic-claim
  convention already used throughout this codebase — the lease-claim query
  below follows it exactly, no new locking primitive introduced.
- RP4D's `PublishVisualAssetVersion(ctx, companyID, assetID, version,
  format, displayName, normalization, content io.Reader)` and
  `validateVisualAssetContent` (structural GLB/USDZ check: header,
  self-containment, no external buffer/image references) are unchanged and
  reused verbatim as the final publication step. RP4E0 adds geometry-level
  validation *before* this call, never replacing it.
- `SpatialArtifact` (`internal/spatial/artifact.go`) already has
  `ArtifactKindObservationPhoto`, `image/jpeg`/`image/png` in
  `allowedArtifactContentTypes`, and a tenant-scoped
  `ArtifactRepository.FindByID(ctx, companyID, id)`. A generation job's
  source image is an *existing, already-uploaded* `SpatialArtifact` — no
  new upload path is introduced.
- No Go glTF-parsing library is present. `github.com/qmuntal/gltf` (fetched
  and confirmed resolvable — network access is available in this
  environment) provides `gltf.NewDecoder(r).Decode(doc)` for structural
  parsing and a `modeler` subpackage
  (`modeler.ReadPosition(doc, accessor, nil) → [][3]float32`,
  `modeler.ReadIndices(doc, accessor, nil) → []uint32`) for typed,
  validated accessor reads — exactly what geometry-level validation needs,
  with zero hand-rolled binary parsing risk.
- The Hugging Face Space is a Gradio app. Gradio's real HTTP contract for a
  named endpoint is two-phase: `POST /call/generate_shape` returns
  `{"event_id": "..."}` immediately; the actual result is retrieved via
  `GET /call/generate_shape/{event_id}` (SSE stream, terminating in a
  `complete` event carrying the output, here a GLB). Gradio also supports
  URL-backed file inputs (`{"path": "https://..."}`) instead of raw
  multipart upload. Both facts materially change the provider contract
  from a single blocking call into a resumable two-step protocol — this is
  the single most important structural decision in this design.
- `internal/aiintegration` exists as a *separate* module only because
  `internal/ai` needs primitive-only context from several unrelated
  domains (Projects/Spaces/Work/Materials/WorkResources) it must not
  depend on directly. RP4E0 has no such cross-domain fan-out — it needs
  only `spatial`'s own artifact/visual-asset data — so it lives inside
  `internal/spatial`, matching how RP4D itself extended `spatial` rather
  than spinning off `internal/visualasset`.

## 3. Core data model

### 3.1 `JobExecution` — genuinely shared state (new `internal/spatial/jobexecution.go`)

The first real implementation of M8.5C §37's shared execution-state shape.
Deliberately minimal — shared *state*, not a generic job framework. No
`Job[T]`, no generic repository, no reflection-based dispatcher, no
cross-domain orchestration engine.

```go
type JobExecutionStatus string

const (
    JobExecutionPending    JobExecutionStatus = "pending"
    JobExecutionProcessing JobExecutionStatus = "processing"
    JobExecutionCompleted  JobExecutionStatus = "completed"
    JobExecutionFailed     JobExecutionStatus = "failed"
    // JobExecutionNeedsAttention covers every state that must never be
    // auto-retried by the system: quota exhaustion, an ambiguous
    // provider outcome, or anything else requiring a human/explicit
    // decision. The SPECIFIC reason lives in FailureCode, never as a
    // new top-level Status value — this is what keeps JobExecution
    // reusable by future job kinds (SpatialReconstructionJob,
    // SpatialDesignGenerationJob, SpatialAssetImportJob) whose own
    // failure vocabularies will differ from asset generation's.
    JobExecutionNeedsAttention JobExecutionStatus = "needs_attention"
    JobExecutionCancelled      JobExecutionStatus = "cancelled"
)

type JobExecution struct {
    Status      JobExecutionStatus
    Attempt     int
    MaxAttempts int
    AvailableAt time.Time // when this job becomes claimable again

    LeaseOwner     string     // opaque worker instance identifier
    LeaseExpiresAt *time.Time

    StartedAt   *time.Time
    CompletedAt *time.Time

    FailureCode    string // e.g. "zero_gpu_quota", "provider_outcome_unknown",
                           // "invalid_source_image", "generated_glb_invalid",
                           // "provider_rejected"
    FailureMessage string

    CancelRequestedAt *time.Time
}
```

Domain job types embed this by value and add their own payload/checkpoint
fields. `SpatialAssetGenerationJob` (below) is the only consumer built in
RP4E0; later job kinds reuse `JobExecution` and its lease-claim helper
functions without re-deriving lease semantics.

### 3.2 `SpatialAssetGenerationJob` (new `internal/spatial/assetgenerationjob.go`)

```go
type SpatialAssetGenerationJob struct {
    ID        string
    CompanyID string
    ProjectID string // for scoping/listing; not itself an authorization boundary beyond CompanyID

    ClientRequestID  string // caller-supplied idempotency key (opaque string)
    RequestFingerprint string // SERVER-COMPUTED, never trusted from the client (§4)

    SourceArtifactID string // an existing SpatialArtifact.ID (image/jpeg|image/png), tenant-verified at submit time
    Seed             int64

    // Provider-call checkpoints, in strict acquisition order. Presence of
    // each is itself the state machine — see §5's claim-query logic.
    ProviderStartedAt *time.Time // set the instant BEFORE the Gradio POST — the ZeroGPU danger-zone marker
    ProviderRequestID *string    // Gradio's event_id — set the instant AFTER the POST returns, before waiting on SSE

    // Staging checkpoint — set the instant raw bytes are durably stored,
    // BEFORE validation. Points at a STAGING key, never RP4D's final
    // immutable visual-asset key (see §7).
    GeneratedObjectKey string
    GeneratedChecksum  string
    GeneratedByteCount int64

    // Result, populated only on JobExecutionCompleted.
    ResultAssetID  string
    ResultVersion  int

    CreatedByUserID string
    CreatedAt       time.Time

    Execution JobExecution
}
```

`GeneratedObjectKey` is infrastructure-only — never serialized into any
API DTO, never logged.

## 4. Idempotency — server computes the fingerprint

The submit request carries only `clientRequestId` (an opaque string the
caller controls, e.g. a UUID generated once per logical "generate this"
action, reused verbatim across a lost-response retry — mirrors RP4B's
`PendingEdit.operationId` convention exactly). The server independently
computes `RequestFingerprint` from authoritative values it already trusts:
`companyID`, `sourceArtifactID`, the source artifact's own stored checksum
(read from `SpatialArtifact`, never re-trusted from the request),
`seed`, and any other generation parameter RP4E0 exposes (none beyond seed
in this slice). A client can never supply or influence the fingerprint.

- Same `clientRequestId` + matching server-computed fingerprint → adopt
  the existing job, return its current state (idempotent retry).
- Same `clientRequestId` + a DIFFERENT server-computed fingerprint (e.g. a
  buggy/malicious client reusing an ID for a different request) → `409`
  conflict, mirroring RP4B's `operation_id_conflict` taxonomy.
- A genuinely new generation request always uses a fresh `clientRequestId`.

Repository-level race handling mirrors RP4B/RP4D precedent: a compound
unique Mongo index on `(companyId, clientRequestId)`; `CreateProcessingJob`
attempts `Create`, and on `mongo.IsDuplicateKeyError` re-fetches and
reconciles exactly like `AIGenerationBatch`'s
`reconcileExistingBatch`/RP4D's duplicate-key-adoption pattern.

## 5. Provider boundary — two-phase, matching Gradio's real protocol

```go
// AssetGenerationSource is a resolved, provider-ready reference to a
// tenant-authorized source image — never a raw io.Reader the Go backend
// proxies, and never an arbitrary caller-supplied URL. Built internally
// from a verified SpatialArtifact via a short-lived, provider-readable
// access URL (§6).
type AssetGenerationSource struct {
    URL string // short-lived, HMAC-capability-backed, resolves to the artifact's bytes
}

type ProviderGenerationStatus string

const (
    ProviderGenerationPending   ProviderGenerationStatus = "pending"   // still processing, no result yet
    ProviderGenerationCompleted ProviderGenerationStatus = "completed" // Output is set
    ProviderGenerationQuotaBlocked ProviderGenerationStatus = "quota_blocked" // ZeroGPU exhausted
    ProviderGenerationRejected ProviderGenerationStatus = "rejected"   // provider-side deterministic rejection (bad input)
)

type ProviderGenerationRef struct {
    ID string // Gradio's event_id
}

type ProviderGenerationResult struct {
    Status ProviderGenerationStatus
    Output io.ReadCloser // set only when Status == ProviderGenerationCompleted; caller closes
    FailureMessage string
}

// AssetGenerationProvider is the provider-neutral boundary — the ONLY
// place spatial ever calls out to an external generation service.
// Consumer-defined interface (matching VisualAssetReadAccessProvider's
// exact convention from RP4D) so spatial never imports the concrete
// Hunyuan HTTP client directly.
type AssetGenerationProvider interface {
    // StartShapeGeneration submits a new generation request and returns
    // as soon as the provider ACKs receipt (a single POST round-trip,
    // seconds not minutes) — it does NOT wait for the GLB. The caller
    // MUST durably persist the returned ref.ID (ProviderRequestID)
    // before doing anything else.
    StartShapeGeneration(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error)

    // ResumeShapeGeneration polls/reconnects to an ALREADY-STARTED
    // generation by its ref. Safe to call repeatedly and from a
    // different process than the one that started it — this is what
    // makes the worker resumable across restarts without ever
    // re-invoking the provider. Returns Pending if still running.
    ResumeShapeGeneration(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error)
}
```

`internal/platform/composition/hunyuanprovider.go` implements this against
the real Gradio HTTP contract: `StartShapeGeneration` POSTs to
`/call/generate_shape` with `{"data": [{"path": <source.URL>}, <seed>]}`,
bearer-authenticated with the HF token, and returns the parsed
`event_id`. `ResumeShapeGeneration` opens
`GET /call/generate_shape/{event_id}` and reads the SSE stream until a
terminal event: `complete` → downloads the referenced GLB output URL
(itself a request bearing the same bearer token) and returns
`ProviderGenerationCompleted` with `Output` streaming those bytes;
a Gradio-reported quota/queue-exhaustion error →
`ProviderGenerationQuotaBlocked`; a Gradio-reported input-validation error
→ `ProviderGenerationRejected`. Config
(`HUGGINGFACE_SPACE_URL`/`HUGGINGFACE_TOKEN`, both required together,
optional-together at boot like `AIServiceURL`/`AIInternalToken`) — unset
means `Service.SubmitAssetGenerationJob`/the worker return
`ErrAssetGenerationNotConfigured` cleanly, never fake success. The HF
token never reaches the browser — it exists only inside this composition-
root-owned client, exactly like `AIInternalToken`.

## 6. Source image access — authorized, never client-controlled

The submit request carries `sourceArtifactId` only. The service:
1. Resolves it via `ArtifactRepository.FindByID(ctx, companyID, sourceArtifactId)` —
   tenant-scoped; missing/wrong-company → 404, matching the existing
   hide-cross-tenant-existence convention.
2. Validates deterministically, before touching the provider at all:
   artifact status is `uploaded`; content type is `image/jpeg` or
   `image/png`; declared size is within a sane image bound (new, smaller
   than `maxArtifactSizeBytes`); the stored bytes actually decode as an
   image via Go's `image` package and have sane pixel dimensions
   (non-zero, below a configurable max) — genuinely opening and decoding
   the bytes, not trusting the stored content type alone, matching this
   codebase's "genuinely inspect the bytes" convention from RP4D's GLB/USDZ
   checks. Any failure here → job `failed`, terminal, zero GPU spend.
3. Only after all of the above pass does the worker mint a short-lived
   provider-readable access URL for that artifact's bytes — reusing RP4D's
   exact `LocalHMACReadAccessProvider`/capability-keyring shape (a new,
   narrowly-scoped capability distinct from the visual-asset one, signed
   over `(companyID, artifactID, expiresAt)`, verified by a new local
   content route `GET /spatial/asset-generation/source-image?cap=...`),
   with an absolute URL built from the same trusted `EXTERNAL_API_BASE_URL`
   RP4D already uses — never a bare relative path, never derived from a
   request `Host` header.

## 7. Storage — staging vs. final, and the claim-safe state machine

`asset-generation/<companyId>/<jobId>/raw/<checksum>.glb` is the staging
key (a new key prefix under the existing local object store, via a new
narrow `AssetGenerationObjectStore` interface — `Put`/`Get`, mirroring
`VisualAssetObjectStore`'s shape exactly). Raw provider output is stored
here THE INSTANT it's fully received, before any validation runs — this is
the `GeneratedObjectKey` checkpoint. Validation and publication read from
here; `PublishVisualAssetVersion` copies validated bytes into RP4D's
`visual-assets/<company>/<assetId>/v1/<checksum>.glb` — the staging object
is never itself treated as the published asset.

### Claim query enforces the safe boundary directly (not just recovery)

The atomic `FindOneAndUpdate` claim filter is not merely "lease
expired/unset" — it structurally excludes the one genuinely dangerous
state:

```text
claimable :=
  lease unset OR lease expired
  AND status IN (pending, processing)
  AND (
    ProviderStartedAt IS NULL                    -- never started: safe to StartShapeGeneration
    OR ProviderRequestID IS NOT NULL              -- event_id known: safe to ResumeShapeGeneration
    OR GeneratedObjectKey != ""                   -- GLB already staged: safe to skip provider, resume validate/publish
  )
```

A job in the state `ProviderStartedAt set AND ProviderRequestID nil AND
GeneratedObjectKey empty` (the POST was sent but the process died before
the `event_id` could be durably recorded — the one truly ambiguous window,
now shrunk to a single HTTP round-trip rather than the entire generation)
is **excluded from this claim query entirely**. It can only be moved out
of that state by an explicit reconciliation path (§8), never by an
automatic reclaim. This guarantee lives in the repository's claim query
itself, not merely as a recovery-scan policy layered on top.

Worker behavior branches on which checkpoint is present once claimed:
- `ProviderStartedAt` nil → resolve source image access (§6) FIRST
  (artifact lookup + minting the short-lived provider-readable URL — both
  purely local/infra operations, nothing provider-facing yet). Only once
  BOTH succeed does the worker set `ProviderStartedAt` (a durable write)
  and immediately call `StartShapeGeneration`. **`ProviderStartedAt` is
  set as the LAST local step before the POST, never earlier** — a
  source-artifact-lookup failure or URL-minting failure before that point
  is an ordinary local/infra failure (§8: retryable, `Attempt++`, never
  touches the provider timeline at all), not an ambiguous provider
  outcome. This is what keeps the genuinely ambiguous window to exactly
  "the POST was sent but the event_id write hasn't happened yet" — nothing
  broader.
- `ProviderRequestID` set, `GeneratedObjectKey` empty → skip straight to
  `ResumeShapeGeneration(ref)` — no new provider submission. If the result
  is `ProviderGenerationPending`, the worker keeps the lease renewed via
  the heartbeat while looping; it does not busy-block past the configured
  provider timeout. A `ProviderGenerationCompleted` result triggers the
  staging write below; `ProviderGenerationQuotaBlocked`/
  `ProviderGenerationRejected` transition the job per §8's table.
- `GeneratedObjectKey` set → skip the provider entirely, resume from
  geometry validation using the already-staged bytes.

**Lease fencing — every mutation while a claim is held is scoped by the
current lease, not just the job ID.** `SetProviderStarted`,
`SetProviderRequestID`, `SetGenerated`, `Complete`, `MarkFailed`,
`MarkNeedsAttention`, and `RequeueForRetry` all filter on
`(_id, leaseOwner=<this worker's id>)`, not `_id` alone. A worker that
lost its lease (heartbeat renewal failed, or a genuinely stuck worker
whose lease expired and was reclaimed by another worker) can never
overwrite a checkpoint the new lease-holder is now responsible for — the
write simply matches zero documents and the caller must treat that as
"stop, this claim is no longer mine," not silently continue.

While a claim is held and provider work is in flight (either waiting on
`StartShapeGeneration`'s POST or looping `ResumeShapeGeneration`), a
background heartbeat renews `LeaseExpiresAt` on an interval comfortably
shorter than the lease TTL (both configurable — e.g. lease TTL 3 minutes,
heartbeat every 60s), so a healthy multi-minute generation is never
reclaimed out from under its own worker merely because it's running long.
**If a heartbeat renewal itself fails** (the fenced update matches zero
documents — this worker's lease is already gone), **the worker must stop
processing this job immediately** — it must not proceed to the next
checkpoint write, since that write would itself be correctly rejected by
fencing, but attempting it after the provider call has already consumed
real GPU time is exactly the kind of wasted, unrecoverable work this
design exists to prevent. The worker logs the loss and returns; the job
remains wherever its last successfully-fenced checkpoint left it, safely
resumable by whichever worker now holds the lease (or by recovery).

## 8. Failure taxonomy and retry semantics

| Situation | `Status` | `FailureCode` | Retryable? |
|---|---|---|---|
| Invalid/undecodable source image, bad seed, artifact not found/wrong tenant | `failed` | `invalid_source_image` | No — terminal, no GPU spend occurred |
| Local DB/object-store/source-access error, BEFORE `ProviderStartedAt` is set (includes a failed artifact lookup or URL-minting failure — see §7's timing correction) | `pending` (or `failed` if `Attempt+1 >= MaxAttempts`) | — | Yes, bounded — `AvailableAt` = backoff, `Attempt++`, capped by `MaxAttempts`; once the cap is reached the job becomes terminal `failed` rather than looping forever |
| Gradio returns a deterministic rejection (bad input at provider) | `failed` | `provider_rejected` | No |
| Gradio/ZeroGPU reports quota/queue exhaustion | `needs_attention` | `zero_gpu_quota` | No automatic retry — user-visible retry action only |
| Timeout/connection-loss/worker-crash/expired-lease discovered with `ProviderStartedAt` set and neither `ProviderRequestID` nor `GeneratedObjectKey` present | `needs_attention` | `provider_outcome_unknown` | No automatic retry — requires reconciliation or an explicit new job |
| Generated GLB fails structural or geometry validation | `failed` | `generated_glb_invalid` | No — the specific generated output is bad; a NEW job (fresh `clientRequestId`) is required to try again, since retrying this job would never re-invoke the provider once `GeneratedObjectKey` is set (correct — the fix is a new generation, not a retry of a known-bad result) |
| Storage/publication failure AFTER `GeneratedObjectKey` is set | `pending` (or `failed` if `MaxAttempts` reached) | — | Yes, bounded — resumes from the staged GLB, never calls the provider again; still capped by `MaxAttempts` like any other retry |

**Every requeue clears the lease.** `RequeueForRetry` always clears
`LeaseOwner`/`LeaseExpiresAt` (so the job is immediately claimable by
whichever worker's `ClaimNext` runs next, not blocked behind a lease that
no longer means anything) and enforces `MaxAttempts`: if incrementing
`Attempt` would meet or exceed `MaxAttempts`, the job transitions to
terminal `failed` (with a `FailureCode` describing exhaustion) instead of
being requeued at all — a job can never retry indefinitely.

**Recovery scope, corrected**: recovery
(`RecoverStaleAssetGenerationJobs`, run on a timer) is now a strict subset
of what `ClaimNext`'s own filter already allows — recovery does not need
a narrower rule than the claim query, because the claim query's `$or`
already only allows exactly the three safe checkpoint states (§7). A
stale lease (expired, owner unresponsive) on a job whose
`ProviderRequestID` is set, or whose `GeneratedObjectKey` is set, is
SAFELY automatically reclaimable — a new worker resumes
`ResumeShapeGeneration` or resumes validation, never re-invokes
`StartShapeGeneration`. The ONLY state that must never be automatically
reclaimed by anything (claim query or a separate recovery pass) is the
one already excluded in §7: `ProviderStartedAt` set, `ProviderRequestID`
nil, `GeneratedObjectKey` empty. That state surfaces as
`needs_attention`/`provider_outcome_unknown` for a human or an explicit
new job — never an automatic reclaim-and-repeat. In practice "recovery"
in RP4E0 is nothing more than the same periodic `ClaimNext` scan any idle
worker already runs — there is no separate recovery-specific query or
code path, which is what guarantees the two can never drift apart.

## 9. HTTP surface

- `POST /spatial/asset-generation-jobs` — principal-authenticated. Body:
  `{clientRequestId, sourceArtifactId, seed}`. Returns the job's current
  DTO (id, status, failureCode if any, resultAssetId/version if
  completed) immediately — never blocks on the provider.
- `GET /spatial/asset-generation-jobs/{id}` — principal-authenticated,
  tenant-scoped. Poll target. Returns the same DTO shape.
- No separate "retry" route: a `needs_attention` job is retried by
  submitting a fresh `POST` with a new `clientRequestId` (and, if the
  source image is unchanged, the same `sourceArtifactId`/`seed`) — this is
  what makes a retry visibly and unambiguously "start a new
  paid/quota-consuming generation," per the explicit requirement that
  retry be a user-visible action, never a background loop.
- DTOs never expose `GeneratedObjectKey`, `ProviderRequestID`, or any
  storage/provider-internal detail.

## 10. Geometry validation (new `internal/spatial/assetgenerationvalidation.go`)

Runs AFTER RP4D's existing `validateVisualAssetContent` (structural GLB
check — self-contained, valid header/JSON) and BEFORE
`PublishVisualAssetVersion`. Uses `github.com/qmuntal/gltf` +
`modeler.ReadPosition`/`modeler.ReadIndices`, isolated entirely behind one
function:

```go
func validateGeneratedGeometry(content []byte) error
```

`qmuntal/gltf` types never leak past this file — `spatial`'s public domain
surface is untouched. Checks run in this order, cheapest and safest first:
1. Parses cleanly via `gltf.NewDecoder` (redundant with, but independent
   of, the existing structural check — defense in depth, cheap).
2. **Index-bounds and divisibility, checked BEFORE any position lookup by
   index**: `len(indices) % 3 == 0` (a truncated/malformed index buffer is
   rejected outright, never silently truncates the last partial
   triangle), and every index value is `< len(positions)` (an
   out-of-range index is rejected outright, never used to index into the
   positions slice — this is what prevents a malformed or adversarial GLB
   from causing an out-of-bounds panic instead of a clean validation
   error).
3. Every `POSITION` accessor's values are finite (no NaN/Inf).
4. Overall mesh bounding box has **genuine non-degenerate 3D extent**: all
   three axis extents (`dx`, `dy`, `dz`) must independently exceed a small
   positive epsilon — a flat plane or a line (one or two axes collapsed to
   ~zero while another is non-zero) is rejected, not just the
   all-three-zero single-point case. Also within a sane absolute size
   range (catches degenerate/collapsed or absurdly-scaled output).
5. Triangle count is within `[minTriangles, maxTriangles]`.
6. No degenerate triangles beyond a small tolerated fraction (three
   collinear/coincident vertices).
7. Aspect ratio of the bounding box is within a sane range (catches
   needle/pancake degenerate output).
8. **Pathological-fragmentation check, actually enforced (not
   logging-only)**: connected-component analysis (union-find over
   triangle adjacency via shared vertices) computes each component's
   triangle count. If any component's triangle count falls below a
   configurable fraction of the total triangle count (e.g. a component
   contributing less than 1% of all triangles) AND the total component
   count exceeds a configurable ceiling (e.g. more than 12 components),
   validation fails — this specifically targets shattered/noise output
   (many tiny stray fragments) rather than penalizing legitimate
   multi-part furniture (a table + a small number of substantially-sized
   separate legs/parts), which the same test suite proves is accepted.
   Both thresholds are explicit package-level configuration, not
   hardcoded inline; destructive mesh repair remains out of scope for
   RP4E0 — this check only rejects, it never repairs.
9. File size and triangle-count hard limits from the kickoff's own
   requirement.

**Bounded reads before decoding**: the raw bytes passed into this
function, and every earlier read of provider output / staged bytes in the
worker (§7), are read via `io.LimitReader(reader, maxGeneratedBytes+1)`
before `io.ReadAll` — never an unbounded `io.ReadAll` directly on a
provider response or object-store stream. A result that reaches the
declared byte ceiling is rejected as oversized rather than allowed to
exhaust memory; this applies to the provider's output download, the
staging-store write, and the staging-store read that feeds this
validation function.

A failure here sets `Status=failed, FailureCode=generated_glb_invalid` —
terminal for this job, per §8.

## 11. Publication

On full validation pass: `PublishVisualAssetVersion(ctx, companyID,
serverGeneratedAssetID, 1, VisualAssetFormatGLB, displayName,
normalization, stagedContentReader)` — a fresh, server-generated
`assetId` (e.g. `genasset_<jobID>`), always version `1` (rationale
immediately below). Sets `ResultAssetID`/`ResultVersion` on the job,
`Status=completed`. RP4D's
publication ordering (store-before-insert, compound-unique-index race
guard) is unchanged and reused as-is.

**Why always a fresh `assetId`/v1, never a computed `nextVersion`**: two
independently-claimed jobs both reading "latest version = N" and both
planning to publish `N+1` could both spend a full Hunyuan run before
either publishes, and the loser would hit RP4D's unique-index conflict
after already paying for GPU time — wasting exactly the resource this
whole design exists to conserve. Every independently generated candidate
therefore gets its own new `assetId`, version 1, with zero cross-job
coordination needed at publish time. A future slice may decide an accepted
design and a deliberate regeneration should share a stable `assetId` with
atomic version reservation *before* the provider call begins — not
attempted here.

## 12. Dispatch and deployment honesty

`jobs.InProcessQueue` is used purely as a **local/dev wake-up hint** — an
`Enqueue` call after job creation nudges a worker goroutine to attempt a
claim sooner than its next poll interval. Mongo remains the only durable
source of truth; if the hint is lost (process restart, no queue consumer
running), a periodic claim-scan (a simple ticker calling the same claim
query) still finds and processes the job — dispatch is provably just an
optimization, never a correctness dependency, per §37's own framing.

**This is explicitly NOT sufficient for a production Vercel deployment.**
The plan and code comments state this directly: an in-memory queue cannot
guarantee any wake-up survives a serverless instance disappearing, and
Vercel's function duration limits (5 min Hobby / 13 min Pro standard, a
separate longer-duration mode currently documented for Node.js/Python
only) make "one Go HTTP handler stays alive for the whole generation" an
unsound assumption regardless. The two-phase provider contract (§5) is
what actually removes this dependency — a request handler only ever needs
to survive one POST or one poll-and-persist cycle, never the full
generation — but a durable production wake-up mechanism (a real queue,
e.g. Vercel Queues, or an external cron/worker) is explicitly deferred to
a future slice, not built or assumed working in RP4E0.

## 13. Testing plan (backend only — no Web/iOS surface in RP4E0)

- `JobExecution`/claim-query unit tests: claimable/not-claimable for every
  combination of `(ProviderStartedAt, ProviderRequestID,
  GeneratedObjectKey, lease state)` — explicitly including the dangerous
  state proven NOT claimable.
- Idempotency: same `clientRequestId` + matching fingerprint adopts; same
  id + mismatched fingerprint → 409; concurrent identical submits race
  safely (real-Mongo test, mirroring RP4B's concurrent-writer tests).
- Source-image validation: valid image passes; wrong content-type, oversized,
  undecodable bytes, cross-tenant artifact, missing artifact all fail
  terminally with zero provider calls made (proven via a spy provider
  asserting zero invocations).
- Fake `AssetGenerationProvider` covering: immediate completion, pending→
  completed after N `ResumeShapeGeneration` polls, quota-blocked,
  rejected, and a simulated crash between `StartShapeGeneration` returning
  and `ProviderRequestID` being persisted (proving the resulting job lands
  in `needs_attention`/`provider_outcome_unknown` and is never reclaimed).
- Geometry validation: valid generated-shape-like GLB passes; NaN
  positions, zero/degenerate bounds, over triangle-count limit, excessive
  degenerate-triangle fraction, and pathological fragmentation each
  rejected with the right `FailureCode`; a legitimate multi-component GLB
  (e.g. two disjoint boxes) is accepted, proving the connected-component
  check is a report/threshold, not a hard reject.
- Full worker-resume tests: a job with only `ProviderStartedAt` set is
  claimed and starts the provider; a job with `ProviderRequestID` set
  resumes via `ResumeShapeGeneration` and never calls
  `StartShapeGeneration` again (spy-proven); a job with `GeneratedObjectKey`
  set resumes at validation and never calls the provider at all
  (spy-proven).
- Real-Mongo repository tests for the compound unique index, claim-query
  race safety (concurrent claim attempts), and recovery-scan scope.
- HTTP route tests: submit/poll, tenant isolation, DTO never leaks
  `GeneratedObjectKey`/`ProviderRequestID`.

Real end-to-end verification against the actual HF Space is explicitly
**not required and not attempted** in RP4E0 unless the user separately
provides live credentials and asks for it — the design and tests above
are provable entirely with a fake provider; a live-Hunyuan smoke run
(if ever done) would be a manual, opt-in, credential-gated follow-up, not
part of this slice's automated verification.
