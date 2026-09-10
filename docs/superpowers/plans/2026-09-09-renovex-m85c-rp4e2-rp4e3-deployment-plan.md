# Renovex M8.5C RP4E2/RP4E3 and Zero-Cost Tester Implementation Plan

> **Required implementation sub-skill:** Execute this plan with `superpowers:executing-plans`. Follow the repository's current agent restrictions; use parallel work only where those rules explicitly permit it and never allow overlapping edits to shared contracts.

**Repository audited:** `C:\Users\shananth\mvp` on 2026-09-09. This document is a plan only; it does not change the Renovex repository.

## Outcome

Finish the remaining M8.5C work as one coherent path:

1. RP4E1 keeps GLM reasoning immutable and produces a validated, revision-pinned plan.
2. RP4E2 confirms that exact plan, creates a separate generation attempt, asks the existing Python service for one isolated three-quarter reference image, stores it in R2, and submits it through the existing RP4E0 Hunyuan job machinery.
3. RP4E3 previews the result on the exact selected RoomDraft object or fixture in the existing live Three.js scene, supports Current/Concept comparison, and applies accepted changes through canonical RoomDraft operations.
4. The tester deployment uses three Vercel projects, Atlas Free/M0, one private R2 bucket, Workers AI FLUX.1-schnell, the existing Hugging Face Hunyuan Space, and Resend test-sink delivery.

The accepted lineage is:

```text
SpatialDesignSession
  -> immutable SpatialDesignTurn
  -> exact planFingerprint
  -> DesignGenerationAttempt
  -> stored reference image
  -> existing SpatialAssetGenerationJob
  -> published VisualAssetVersion
  -> immutable DesignAcceptance
  -> canonical RoomDraft edit records
```

## Repository findings and binding amendments

| Finding in the current repository | Why it conflicts | Smallest required amendment |
|---|---|---|
| `SpatialDesignSession.CurrentWorkingDesign` is updated when a proposal validates, while its comment calls it accepted state. There is no accepted snapshot. | A proposed concept and a persisted RoomDraft result cannot be represented separately. | Keep the BSON/JSON field `currentWorkingDesign` as the working proposal for compatibility and add `AcceptedDesign`, `AcceptedTurnID`, and `AcceptedAttemptID`. On successful Use Design, atomically align accepted/current semantic geometry and material, with applied spatial operations cleared because RoomDraft now owns that placement. |
| RP4E1 derives `HunyuanRequired` from the presence of any cumulative custom geometry. | A material-only refinement after accepting generated geometry would incorrectly request another Hunyuan run. | Compare working geometry with `AcceptedDesign.Geometry`. Generate only when an unaccepted replacement geometry is pending. A geometry clear previews/persists `clear_visual_asset` locally. After acceptance, clear applied spatial operations from both baselines so absolute moves/resizes are never replayed. |
| Sessions are permanently pinned to their creation-time `BasedOnRoomDraftRevision`. | A session could not continue after its own accepted design changes the draft. | Only Use Design may advance the session pin to the resulting RoomDraft revision. Any unrelated revision change remains stale and requires re-review; no confirm, regenerate, or use path rebases silently. |
| `WorkingDesignMaterial.BaseColor` and Python `MaterialSpec.baseColor` accept arbitrary text such as `dark green`. | The renderer needs deterministic color data. | Make `baseColor` a canonical uppercase or lowercase sRGB `#RRGGBB` value in Python and revalidate it in Go. Continue to carry `materialFamily`, `roughness`, and `metallic`. Named-part accents remain prompt intent only. |
| `RoomDraftObject` and `RoomDraftFixture` have `VisualAsset` but no appearance field, and the canonical operation set has no material operation. | Material-only concepts cannot persist. | Add optional `VisualAppearance` to objects and fixtures plus `set_visual_appearance` and `clear_visual_appearance` operations. Do not add a separate materials collection or PBR asset pipeline. |
| RP4E0 accepts only a capture-owned `SpatialArtifact` through `SourceArtifactID`. Generated references are session/turn/attempt-owned. | Fabricating a capture artifact would blur ownership and force the wrong R2 prefix. | Add a discriminated `AssetGenerationSourceRef` to the RP4E0 job: `capture_artifact` for the existing public route and `design_reference` for the new internal submit path. Preserve `SourceArtifactID` decoding for existing records. The RP4E0 worker still owns every Hunyuan call, checkpoint, validation, and publish step. |
| Three separate `LocalObjectStore` roots are constructed for artifacts, visual assets, and generation staging. | Vercel has no durable local filesystem and the tester target requires one R2 bucket. | Add one R2 `ObjectStore`, pass the same instance to all spatial adapters, and preserve logical prefixes. Keep the three legacy local roots only in local mode so existing development files still resolve. |
| `ProcessOneAssetGenerationJob` waits in the Hunyuan SSE path for up to nine minutes; the in-process ticker assumes a long-lived server. | Vercel Hobby functions have a configurable maximum of 60 seconds. | Retain Mongo leases and provider-start checkpoints, but add a bounded one-step worker that runs for at most 45 seconds. A Vercel Queue message is only a wake-up hint. A pending SSE read releases the job for another queue delivery without calling Hunyuan `StartShapeGeneration` again. |
| Only SMTP/Mailpit exists. | Tester delivery must use Resend and redirect actual delivery without corrupting logical recipient data. | Add a Resend implementation of the existing `mail.EmailSender` and a `TestSinkSender` decorator that rewrites only the outbound `mail.Message.To` copy and adds an intended-recipient header. Existing domain records continue to store the real email. |
| `backend/migrations` is empty; repository constructors create indexes at API startup; there are no Mongo validators. | A clean Atlas bootstrap is not reproducible as a separate deployment step. | Add an idempotent `dbbootstrap` command and a checked-in schema manifest. It creates only repo-defined collections, runs every existing `EnsureIndexes`, creates the new M8.5C indexes, records one migration ledger entry, verifies transactions, and inserts no business or demo data. |
| `SpatialEditor` defaults to 2D and uses a fixed `320px` inspector column. | RP4E3 requires the room to be the hero and a 390–440px contextual studio. | Make 3D the initial view on this route, replace the inspector column with the responsive AI studio dock, and place the existing inspector inside a compact “Object controls” disclosure in that dock. The 2D switch remains available. |
| The older M8.5C umbrella spec discusses Qwen, whole-room manifests, material records, and simultaneous A/B/C concepts. | Those directions conflict with the frozen RP4E1/RP4E2/RP4E3 decisions. | Treat the 2026-09-09 RP4E1 implementation and this plan as the amendment: GLM-5.3 reasoning, one selected object/fixture, one concept at a time, object-level appearance overrides, and no whole-room autonomous redesign. |

## Existing code to reuse exactly

### Go backend

- `internal/spatial/designsession.go`: `SpatialDesignSession`, immutable `SpatialDesignTurn`, `SpatialDesignTarget`, `AuthorizedDesignTarget`, and the existing turn statuses.
- `internal/spatial/designplan.go`: `WorkingDesign`, `WorkingDesignGeometry`, `WorkingDesignMaterial`, `ValidatedSceneEditPlan`, `ResolvedSpatialOperation`, and `DesignIntent`.
- `internal/spatial/design_service.go`: tenant target resolution, plan validation, revision checks before and after AI reasoning, provider-start at-most-once marker, turn supersession, and execution flags.
- `internal/spatial/design_repository_mongo.go`: the existing session/turn transaction boundary, client request fingerprint handling, and indexes.
- `internal/spatial/design_handler.go`: the four RP4E1 routes and public DTO mapping.
- `internal/spatial/editoperation.go`, `roomdraftedit.go`, `service.go`, and `roomdraftedit_repository_mongo.go`: canonical operation decoding/application, fingerprinting, revision CAS, immutable edit records, and Mongo transaction pattern.
- `internal/spatial/assetgeneration_service.go`, `assetgenerationjob.go`, `assetgenerationjob_repository_mongo.go`, and `jobexecution.go`: the durable RP4E0 job, provider-start checkpoint, lease fencing, generated GLB validation, staging, and publication.
- `internal/spatial/assetgenerationprovider.go` plus `internal/platform/hunyuan/client.go` and `internal/platform/composition/hunyuanprovider.go`: the only Hunyuan client/provider path.
- `internal/spatial/visualasset_service.go`, `visualasset.go`, and the visual asset repositories: publishing immutable versions and issuing authorized read access.
- `internal/platform/objectstore/objectstore.go`: `Put`, `Get`, `Exists`, and `Delete` interface.
- `internal/platform/mail/mail.go`: `Message` and `EmailSender`; all company, access-grant, RFQ, supplier verification, and award callers already depend on this interface.
- `internal/platform/composition/services.go`: the sole composition root and existing `EnsureIndexes` sequence.
- `internal/platform/config/config.go`: typed startup validation and environment guard conventions.

### Python AI service

- `app/main.py` and `app/auth.py`: FastAPI registration and the existing internal bearer-token boundary.
- `app/schemas/spatial_reasoning.py`: validated geometry/material shapes shared conceptually with RP4E1.
- `app/providers/base.py`, `glm.py`, and `spatial_mock.py`: provider-neutral protocol style, bounded `httpx` calls, and deterministic mock conventions.
- `app/prompts/spatial_reasoning.py`: deterministic prompt construction pattern.
- `app/services/spatial_reasoning.py`: provider selection and strict response validation pattern.

### Next.js Web

- `src/app/(app)/projects/[projectId]/spaces/[spaceId]/spatial/[roomDraftId]/page.tsx`: keep this route; it continues to mount one `SpatialEditor`.
- `src/features/spatial/editor/components/SpatialEditor.tsx`: keep as the owning workspace controller.
- `src/features/spatial/editor/useEditorState.ts`: retain canonical selection by `{kind,id}`, shared 2D/3D selection, camera pose, and edit lifecycle.
- `src/features/spatial/designSessionApi.ts`: extend the RP4E1 client rather than creating a second transport stack.
- `src/features/spatial/queries.ts` and `mutations.ts`: retain TanStack Query keys, auth client, cache invalidation, and error normalization.
- `src/features/spatial/editor/components/Spatial3DViewport.tsx`: retain the single R3F `Canvas`, `SceneContents`, `ObjectMesh`, `FixtureMesh`, camera controls, and current wireframe selection outlines.
- `AuthorizedVisualAsset.tsx`, `VisualAsset.tsx`, `GLTFVisualAsset.tsx`, `USDVisualAsset.tsx`, and `useNormalizedAsset.ts`: retain authorized URL resolution, fallback behavior, normalization, and asset-error isolation.
- `src/features/spatial/editor/three/sceneProjection.ts`: retain RoomDraft-to-render projection and canonical coordinate mapping.
- `src/components/ui/button.tsx`, `badge.tsx`, `card.tsx`, `sheet.tsx`, `textarea.tsx`, `separator.tsx`, and `skeleton.tsx`: reuse where their behavior fits.
- `src/app/globals.css`: reuse the warm Renovex tokens: `--background`, `--card`, rust `--primary`, warm `--accent`, border/ring values, `--radius`, `.surface-card`, and `.eyebrow`.

## Canonical data contracts

### Appearance persistence

Add to `internal/spatial/appearance.go`:

```go
type VisualAppearance struct {
    BaseColor      string `bson:"baseColor" json:"baseColor"`
    MaterialFamily string `bson:"materialFamily" json:"materialFamily"`
    Roughness      string `bson:"roughness" json:"roughness"`
    Metallic       bool   `bson:"metallic" json:"metallic"`
}
```

Validation is closed and shared by both operations and design confirmation:

- `BaseColor`: `^#[0-9A-Fa-f]{6}$`, normalized to lowercase before fingerprinting.
- `MaterialFamily`: `fabric|leather|wood|metal|stone|other`.
- `Roughness`: `matte|satin|glossy`.
- `Metallic`: explicit boolean.

Add `Appearance *VisualAppearance` to `RoomDraftObject` and `RoomDraftFixture` only. Add canonical operations:

```go
SetVisualAppearanceOperation {
    TargetKind VisualAssetTargetKind `json:"targetKind"`
    TargetID   string                `json:"targetId"`
    Appearance VisualAppearance      `json:"appearance"`
}

ClearVisualAppearanceOperation {
    TargetKind VisualAssetTargetKind `json:"targetKind"`
    TargetID   string                `json:"targetId"`
}
```

Their wire kinds are `set_visual_appearance` and `clear_visual_appearance`. They use the existing `fixture|object` target vocabulary. Reset-to-Scan restores captured objects to no appearance override and removes contractor fixtures through the existing reset behavior. No named-part data is persisted.

### DesignGenerationAttempt

Add `internal/spatial/designgeneration.go` with this durable shape:

```go
type DesignGenerationKind string // material_only | geometry | mixed
type DesignGenerationStatus string
// reserved | generating_reference | reference_ready |
// asset_generation_pending | asset_generation_processing |
// concept_ready | failed | needs_attention | abandoned | accepted

type DesignReferenceImage struct {
    ObjectKey         string
    ChecksumSHA256    string
    ContentType       string // image/jpeg or image/png
    SizeBytes         int64
    Width             int
    Height            int
    Provider          string // cloudflare_flux or mock; private DTO only
    ProviderRequestID string
    Model             string
    PromptVersion     string
    Seed              int64
    CreatedAt         time.Time
}

type ConceptBindingAction string // preserve | assign | clear
type ConceptAppearanceAction string // preserve | set | clear

type DesignConcept struct {
    Target     SpatialDesignTarget
    Transform  RoomLocalTransform
    Dimensions *RoomLocalPoint
    VisualAction     ConceptBindingAction
    AppearanceAction ConceptAppearanceAction
    Appearance *VisualAppearance
    VisualAsset *VisualAssetRef // nil until geometry is published
}

type DesignGenerationAttempt struct {
    ID, CompanyID, ProjectID, SpaceID, RoomDraftID string
    SessionID, TurnID, PlanFingerprint             string
    AttemptNumber                                  int64
    ClientRequestID, RequestFingerprint            string
    BasedOnRoomDraftRevision                       int64
    Kind                                            DesignGenerationKind
    Status                                          DesignGenerationStatus
    ActiveSlot                                      string // "active" only while non-terminal
    TargetSnapshot                                  AuthorizedDesignTarget
    ReferenceProviderStartedAt                      *time.Time
    ReferenceImage                                  *DesignReferenceImage
    AssetGenerationJobID                            string
    Candidate                                       DesignConcept
    SafeFailureCode                                 string
    CancelClientRequestID, CancelRequestFingerprint string
    CreatedByUserID                                 string
    CreatedAt, UpdatedAt                            time.Time
    CompletedAt, AbandonedAt, AcceptedAt            *time.Time
    SchemaVersion                                   int
}
```

Also add `LatestGenerationAttemptID` and `LatestReadyAttemptID` to the session for deterministic restore and Use eligibility. Regenerate leaves the last ready concept viewable while the next attempt runs, but Use is disabled during that run. When the new attempt becomes ready it becomes `LatestReadyAttemptID`; older ready attempts remain historical and cannot be accepted accidentally.

`AcceptedDesign` is the last semantic geometry/material state actually persisted. `CurrentWorkingDesign` is the latest proposed semantic state. They may differ while a plan is under review. After Use, set them equal and clear `ResolvedSpatialOperations` in both because the absolute placement is now represented by RoomDraft itself. A later proposal can still see accepted geometry/material in its reasoning context, while execution compares working geometry to accepted geometry so a material-only refinement does not regenerate an already-bound mesh.

Provider/model identifiers are stored for audit but omitted from the public attempt DTO. Public progress exposes only `stage`, `status`, safe copy, and the config-driven runtime notice.

`spatial_design_generation_attempts` indexes:

1. Unique `{companyId:1, sessionId:1, clientRequestId:1}` for Confirm/Regenerate idempotency.
2. Unique `{companyId:1, turnId:1, attemptNumber:1}`.
3. Unique partial `{companyId:1, turnId:1, activeSlot:1}` where `activeSlot` exists, enforcing one active attempt per turn.
4. `{companyId:1, sessionId:1, createdAt:-1}` for restore/history.
5. Sparse `{companyId:1, assetGenerationJobId:1}` for reconciliation.

### DesignAcceptance

Add an immutable `DesignAcceptance` to the same file and store it in `spatial_design_acceptances`:

```go
type DesignAcceptance struct {
    ID, CompanyID, SessionID, TurnID, AttemptID, PlanFingerprint string
    ClientRequestID, RequestFingerprint                         string
    RoomDraftID                                                  string
    BaseRoomDraftRevision, ResultingRoomDraftRevision            int64
    AppliedOperationIDs                                          []string
    AcceptedDesign                                               WorkingDesign
    CreatedByUserID                                              string
    CreatedAt                                                    time.Time
    SchemaVersion                                                int
}
```

Indexes are unique `{companyId,sessionId,clientRequestId}`, unique `{companyId,attemptId}`, and `{companyId,roomDraftId,createdAt:-1}`.

### RP4E0 source amendment

Replace the job’s single-source assumption with:

```go
type AssetGenerationSourceKind string // capture_artifact | design_reference
type AssetGenerationSourceRef struct {
    Kind                       AssetGenerationSourceKind
    ArtifactID                 string // capture_artifact only
    DesignGenerationAttemptID  string // design_reference only
}
```

New jobs write `SourceRef`; old documents with `sourceArtifactId` decode as `capture_artifact`. The current public `POST /spatial/asset-generation-jobs` still accepts `sourceArtifactId` and calls a compatibility wrapper. A new internal `SubmitAssetGenerationJobFromDesignReference` validates the attempt-owned R2 object and calls the same repository/job creation logic. `startAndResumeAssetGeneration` resolves either source into the existing provider-ready `AssetGenerationSource{URL}` and then follows the current Hunyuan adapter unchanged.

### Python reference-image request/result

Add a protected `POST /internal/v1/spatial/reference-images/generate` route. Its JSON request is:

```json
{
  "schemaVersion": "1",
  "designSessionId": "ds_...",
  "turnId": "dst_...",
  "planFingerprint": "sha256:...",
  "target": {
    "kind": "object",
    "id": "object_...",
    "category": "sofa",
    "dimensionsMeters": {"width": 2.1, "height": 0.85, "depth": 0.9}
  },
  "assetGenerationSpec": {
    "category": "sofa",
    "shapeDescription": "Curved three-seat sofa with rounded back",
    "preserveCanonicalDimensions": true
  },
  "materialAppearance": {
    "baseColor": "#315c45",
    "materialFamily": "fabric",
    "roughness": "matte",
    "metallic": false
  },
  "renderBrief": {
    "view": "three_quarter_front",
    "isolated": true,
    "fullObjectVisible": true,
    "background": "plain_warm_white",
    "noText": true,
    "noPeople": true,
    "noRoom": true
  },
  "promptVersion": "reference-v1",
  "seed": 4815162342
}
```

The response is bounded JSON:

```json
{
  "schemaVersion": "1",
  "imageBase64": "...",
  "contentType": "image/jpeg",
  "width": 1024,
  "height": 1024,
  "provider": "cloudflare_flux",
  "model": "@cf/black-forest-labs/flux-1-schnell",
  "providerRequestId": "optional-safe-id",
  "seed": 4815162342,
  "promptVersion": "reference-v1"
}
```

`ReferenceImageProvider` has one async method `generate_reference(request) -> ReferenceImageResult`. Implement `CloudflareFluxReferenceImageProvider` and `MockReferenceImageProvider`. The deterministic prompt builder emits category, shape, appearance, canonical width/height/depth ratios, the fixed isolated-object composition, and negative constraints in a fixed order. It never includes arbitrary RoomDraft JSON, URLs, tenant data, or credentials.

The Cloudflare provider makes one bounded authenticated call to `/accounts/{account_id}/ai/run/@cf/black-forest-labs/flux-1-schnell`, passes the persisted seed, validates returned base64 as JPEG/PNG, and does no automatic retry. The mock returns deterministic fixture bytes keyed by `planFingerprint + seed`.

### Browser concept state

Add a serializable `ConceptRenderState` in `src/features/spatial/design-studio/types.ts`:

```ts
type ConceptRenderState =
  | { kind: "none" }
  | { kind: "material_preview"; attemptId: string; target: Selection; appearance: VisualAppearance; transform: RoomLocalTransform; dimensions?: RoomLocalPoint }
  | { kind: "geometry_pending"; attemptId: string; target: Selection; appearance?: VisualAppearance; transform: RoomLocalTransform; dimensions?: RoomLocalPoint }
  | { kind: "concept_ready"; attemptId: string; target: Selection; assetRef?: VisualAssetRef; appearance?: VisualAppearance; transform: RoomLocalTransform; dimensions?: RoomLocalPoint }
  | { kind: "stale"; attemptId: string };
```

This is a projection of server state, never an independent source of truth. Candidate transform and dimensions come from the validated plan/attempt, target identity comes from the authoritative RoomDraft selection, and an asset reference comes only from a completed RP4E0 job.

## Public APIs and exact semantics

Keep the four existing RP4E1 routes unchanged. Add these authenticated Huma routes in `designgeneration_handler.go`:

### `POST /spatial/design-sessions/{sessionId}/turns/{turnId}/confirm`

Body:

```json
{"clientRequestId":"...","planFingerprint":"sha256:...","expectedRoomDraftRevision":12}
```

- Look up an existing request before any current-state check. Same request ID plus same server fingerprint returns the original attempt; a different body returns `409 design_request_id_conflict`.
- Require active session, exact tenant, `LatestReadyPlanTurnID == turnId`, turn status `proposed`, matching plan fingerprint, executable plan, and exact current RoomDraft revision.
- A stale revision returns `409 design_plan_stale` with current revision and requires a new review/turn. It does not rewrite the plan.
- Create one attempt. Material-only confirmation moves directly to `concept_ready` and returns `200`. Geometry/mixed returns `202` after writing a durable `reserved` attempt and emitting a wake hint.
- A partial unique index prevents two active attempts for one turn. A different request while active returns `409 design_generation_in_progress` and the active attempt summary.

### `POST /spatial/design-sessions/{sessionId}/turns/{turnId}/regenerate`

Body is the same as Confirm. It requires the same latest turn and exact fingerprint and creates a new attempt number only after the previous attempt is terminal or abandoned. It does not call `CreateDesignTurn`, does not invoke GLM, and reuses the validated plan. For geometry/mixed it chooses and persists a new seed before provider work. Same request ID replays the same new attempt.

### `GET /spatial/design-sessions/{sessionId}/generation-attempts?turnId={optional}&limit={1..50}`

Returns newest first and supports session restore. The UI normally requests `limit=10`; it identifies the latest non-abandoned attempt for the latest turn and reconciles any linked RP4E0 job before returning.

### `GET /spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}`

Returns the public attempt, candidate render state, safe failure code, stage copy, and current RoomDraft revision/staleness. While nonterminal, it reconciles the linked RP4E0 job. Provider names, R2 keys, source URLs, and tokens never leave Go.

### `POST /spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}/cancel`

Body:

```json
{"clientRequestId":"..."}
```

Mark the attempt `abandoned` and clear its active slot first. If a linked RP4E0 job exists, call the existing cancellation request path as best effort. Repeated calls return the same abandoned attempt. A later Hunyuan completion may still publish an immutable VisualAssetVersion, but the abandoned attempt can never become usable or auto-bind anything.

### `POST /spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}/use`

Body:

```json
{"clientRequestId":"...","planFingerprint":"sha256:...","expectedRoomDraftRevision":12}
```

- Look up a prior `DesignAcceptance` by request ID first; same fingerprint returns it and different input returns `409 design_request_id_conflict`.
- Require the attempt to be `concept_ready`, not abandoned, for the session’s latest valid plan, with exact fingerprint and exact RoomDraft revision.
- Build canonical operations in fixed order: resolved move/resize operations, `set_visual_appearance` or `clear_visual_appearance`, then `assign_visual_asset` for geometry. The final step remains the only persistent visual-asset binding path.
- Apply the entire batch inside one Mongo transaction. Each operation gets a deterministic ID derived from the acceptance ID and its sequence, each immutable edit record has the preceding/resulting revision, and the RoomDraft CAS is checked once at the requested base revision.
- In that same transaction insert `DesignAcceptance`, mark the attempt accepted, set session `AcceptedDesign`, `AcceptedTurnID`, `AcceptedAttemptID`, reset `CurrentWorkingDesign` to the accepted design, and advance `BasedOnRoomDraftRevision` to the final resulting revision.
- The attempt must equal `LatestReadyAttemptID`, and no newer generation may be active. Older generated concepts remain immutable history but cannot be applied. Clear applied spatial operations when writing accepted/current working baselines.
- Return `200` with the acceptance and refreshed RoomDraft. A stale base returns `409 design_plan_stale`; nothing is partially applied.

### Internal worker routes

Add routes outside the public OpenAPI group, protected by `SPATIAL_WORKER_TOKEN`:

- `POST /internal/spatial/design-generation/process-one`: advances one attempt phase for at most 45 seconds and returns `{processed, terminal, nextWakeDelaySeconds}`.
- `POST /internal/spatial/asset-generation/process-one`: advances one existing RP4E0 phase for at most 45 seconds and returns the same control fields.

These endpoints choose work from Mongo under the existing lease rules. Queue payloads contain only a company-safe attempt/job ID or an empty wake hint; they carry no authoritative state.

## Attempt and job state semantics

1. **Confirm geometry/mixed:** write attempt `reserved`, emit wake.
2. **Reference claim:** CAS to `generating_reference`, persist `ReferenceProviderStartedAt` before the Python call. A known rejection becomes `failed`; an unknown network outcome becomes `needs_attention`. Never repeat the provider call for that attempt.
3. **Reference success:** validate image signature/dimensions/size in Go, store under R2, persist checksum and metadata, set `reference_ready`.
4. **RP4E0 submit:** call the new internal design-reference wrapper with deterministic client request ID `design-attempt:{attemptId}:hunyuan`, store the returned existing/new job ID, set `asset_generation_pending`, and wake the RP4E0 topic.
5. **RP4E0 process:** claim the durable job. If never started, call the existing Hunyuan `StartShapeGeneration` once and checkpoint the event ID. Use a child context capped at 45 seconds for `ResumeShapeGeneration`. Pending/timeout releases the same job for another delivery; no new provider start occurs. Completed output follows the current staging, geometry validation, and `PublishVisualAssetVersion` code.
6. **Reconcile:** map pending/processing/validation to safe attempt stages. On job completion, copy only `{assetId,version}` into the candidate and mark `concept_ready`. `failed`, `needs_attention`, or `cancelled` map without another GPU submission.
7. **Regenerate:** a user action creates a new attempt and therefore a new reference and Hunyuan job; it keeps the exact validated plan and fingerprint and does not call GLM.
8. **Cancel:** terminal abandonment wins over any later provider result. There is no automatic bind path anywhere.

The existing RP4E0 `MaxAttempts` remains an operational claim/recovery bound. It must never cause another `StartShapeGeneration` once `ProviderStartedAt`/provider request ID exists. A provider-unknown failure remains `needs_attention`; the user can explicitly Regenerate.

## Go backend file plan

### Add

- `backend/internal/spatial/appearance.go` and `appearance_test.go`: value object, normalization, validation, and renderer-safe roughness semantics.
- `backend/internal/spatial/designgeneration.go`: attempt, concept, acceptance, status transition rules, public projection.
- `backend/internal/spatial/designgenerationfingerprint.go` and test: canonical Confirm/Regenerate/Cancel/Use fingerprints.
- `backend/internal/spatial/designgeneration_repository_mongo.go` and test: attempts, acceptances, indexes, active-slot CAS, provider-start marker, job reconciliation, and transaction methods.
- `backend/internal/spatial/designgeneration_service.go` and test: confirm, regenerate, cancel, use, list/get, worker phases, reference validation, R2 key construction, and RP4E0 submission.
- `backend/internal/spatial/designgeneration_handler.go` and test: six public routes and safe error mapping.
- `backend/internal/platform/ai/reference_image_types.go`, `reference_image_client.go`, and tests: strict Go/Python contract with maximum response bytes.
- `backend/internal/platform/composition/referenceimageadapter.go` and test: consumer-owned `spatial.ReferenceImageGenerator` adapter.
- `backend/internal/platform/objectstore/r2.go`, `r2_presign.go`, and tests: S3-compatible store plus presigned GET/PUT support using AWS SDK v2.
- `backend/internal/platform/composition/r2spatialaccess.go` and tests: R2 upload/read/source access adapters.
- `backend/internal/platform/mail/resend.go`, `test_sink.go`, and tests.
- `backend/internal/platform/composition/httpapp.go` and test: build and return the fully wrapped `http.Handler` without listening.
- `backend/api/index.go`: Vercel Go exported `Handler` with process-level lazy initialization and cached Mongo client/services.
- `backend/internal/platform/mongo/schema_manifest.go` and test: exact repo collection manifest and drift check.
- `backend/cmd/dbbootstrap/main.go`: idempotent clean bootstrap/apply/verify command.
- `backend/migrations/20260909_001_m85c_tester_bootstrap.json`: migration ID, schema version, finalized collection list, and checksum; no business documents.
- `backend/vercel.json`: Go route, region, and 60-second function maximum.

### Modify

- `backend/internal/spatial/designsession.go`: add accepted fields and correct the working-state comment.
- `backend/internal/spatial/design_repository_mongo.go`: encode/decode accepted fields and add the acceptance transaction hook without mutating terminal turns.
- `backend/internal/spatial/designplan.go`: use `VisualAppearance` for material state and enforce canonical color.
- `backend/internal/spatial/design_service.go`: later turns start from accepted/working state as defined above, compute generation need from working-versus-accepted geometry, and return new session fields.
- `backend/internal/spatial/roomdraft.go`: add optional appearance to object/fixture.
- `backend/internal/spatial/editoperation.go`: add set/clear appearance operations and pure apply validation.
- `backend/internal/spatial/roomdraftedit.go`: decode the two new operation kinds and expose the existing pure apply path for a canonical batch.
- `backend/internal/spatial/repository.go`: add attempt/acceptance repositories and `ApplyAndRecordBatch` capability.
- `backend/internal/spatial/roomdraftedit_repository_mongo.go`: implement one-transaction ordered batch application and immutable records.
- `backend/internal/spatial/service.go`: wire dependencies and preserve the existing single-edit endpoint.
- `backend/internal/spatial/handler.go`: register new appearance operation schemas; keep public RP4E0 submission unchanged.
- `backend/internal/spatial/assetgenerationjob.go`, `assetgenerationjob_repository_mongo.go`, `assetgenerationfingerprint.go`: add source discriminator with old-record compatibility.
- `backend/internal/spatial/assetgeneration_service.go`: extract shared submit validation, add design-reference source resolution, and split long processing into bounded phases.
- `backend/internal/platform/hunyuan/client.go`: distinguish caller deadline from provider rejection; a deadline after a stored event ID reports pending/reconnectable.
- `backend/internal/platform/composition/assetgenerationsourceaccess.go`: issue R2 presigned GET for either source type in R2 mode; retain signed Go proxy locally.
- `backend/internal/platform/composition/assetgenerationdispatch.go`: local-only ticker calls the bounded method in a loop; it is never started in Vercel.
- `backend/internal/platform/composition/services.go`: construct attempts/acceptances, one shared R2 store, Resend/test-sink mailer, reference client, and wake publisher; retain local adapters in local mode.
- `backend/internal/platform/composition/schema_registration.go` and drift tests: register all new Huma schemas/routes.
- `backend/internal/platform/config/config.go` and tests: add object store, R2, email, reference, queue, worker, and serverless guards.
- `backend/cmd/api/main.go`: use `composition.NewHTTPHandler`; keep signal/listen and local dispatcher only in this binary.
- `backend/go.mod`/`go.sum`: add the minimal AWS SDK v2 S3/config/credentials dependencies.
- `backend/.env.example` or root `.env.example`: document exact variables below.

## Python AI service file plan

### Add

- `ai-service/app/schemas/reference_image.py`: strict request/result models and canonical hex color.
- `ai-service/app/providers/reference_base.py`: `ReferenceImageProvider` protocol and provider exceptions.
- `ai-service/app/providers/cloudflare_flux.py`: one-call FLUX.1-schnell implementation with no retries.
- `ai-service/app/providers/reference_mock.py`: deterministic local/test image result.
- `ai-service/app/prompts/reference_image.py`: versioned deterministic isolated-object prompt.
- `ai-service/app/services/reference_image.py`: provider selection, output decoding, byte/signature/dimension bounds.
- `ai-service/tests/test_reference_image_schema.py`, `test_reference_image_prompt.py`, `test_reference_image_provider.py`, and `test_reference_image_route.py`.
- `ai-service/api/index.py`: import and expose the existing FastAPI `app` for Vercel.
- `ai-service/vercel.json`: Python function mapping, 60-second maximum, and test/build exclusions.

### Modify

- `ai-service/app/config.py`: add `REFERENCE_IMAGE_PROVIDER`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_FLUX_MODEL`, `REFERENCE_IMAGE_TIMEOUT_SECONDS`, and `REFERENCE_IMAGE_MAX_BYTES`; enforce credentials together only for Cloudflare.
- `ai-service/app/main.py`: register the protected reference route and reuse `verify_internal_token`.
- `ai-service/app/schemas/spatial_reasoning.py`: canonical `#RRGGBB` output so RP4E1 plans are renderable.
- `ai-service/app/prompts/spatial_reasoning.py`: explicitly ask GLM for a hex base color while retaining material family/roughness/metallic.
- `ai-service/app/providers/spatial_mock.py` and golden tests: emit canonical hex values.
- `ai-service/pyproject.toml` and `ai-service/.env.example`: deployment dependencies/configuration.

## Next.js Web file plan

### Add

- `apps/web/src/features/spatial/design-studio/types.ts`: public attempt/acceptance types, `ConceptRenderState`, UI state union.
- `apps/web/src/features/spatial/design-studio/designGenerationApi.ts` and tests: Confirm, Regenerate, list/get attempt, Cancel, and Use clients.
- `apps/web/src/features/spatial/design-studio/useDesignStudio.ts` and reducer tests: selection/session restore, prompt draft, comparison mode, collapse/sheet state, active turn, and action orchestration.
- `apps/web/src/features/spatial/design-studio/conceptProjection.ts` and tests: derive the exact target render override from RoomDraft plus public attempt.
- `apps/web/src/features/spatial/design-studio/components/AIDesignStudio.tsx` and tests: responsive shell and state switch.
- `DesignStudioWelcome.tsx`: contextual welcome and selected-object idea chips.
- `DesignPromptComposer.tsx`: compact multiline prompt and “Imagine changes” primary action.
- `AIChangePlanCard.tsx`: structured Shape/Look/Placement/Fit sections, swatches, assumptions, blockers, and runtime notice only when `hunyuanRequired`.
- `DesignProgress.tsx`: staged nonblocking copy with no percentages.
- `ConceptActions.tsx`: Cancel, Regenerate, Use Design, and post-ready refinement chips.
- `CurrentConceptToggle.tsx`: floating semantic segmented control over the existing canvas.
- `SelectedObjectLabel.tsx`: DOM label/selection status associated with the canvas.
- `ObjectControlsDisclosure.tsx`: hosts the existing `ElementInspector` without creating a third column.
- `apps/web/src/features/spatial/design-studio/components/*.test.tsx`: state/accessibility/interaction tests.
- `apps/web/src/lib/server/spatialGenerationQueue.ts`: topic names and typed send helper.
- `apps/web/src/app/api/internal/spatial-generation/enqueue/route.ts`: shared-secret Go-to-Queue bridge.
- `apps/web/src/app/api/queues/spatial-generation/route.ts`: private Queue consumer that invokes one bounded Go worker step and schedules the next wake when requested.
- `apps/web/src/app/api/queues/spatial-generation/route.test.ts`.
- `apps/web/vercel.json`: queue callback/function settings and region.

### Modify

- `apps/web/src/features/spatial/designSessionApi.ts`: expose accepted/working design and revised session DTOs.
- `apps/web/src/features/spatial/queries.ts`: design session/turn/attempt query keys; poll only nonterminal attempts.
- `apps/web/src/features/spatial/mutations.ts`: six design actions with exact cache reconciliation and RoomDraft invalidation after Use.
- `apps/web/src/features/spatial/editor/types.ts`: add generated OpenAPI aliases and appearance operations.
- `apps/web/src/features/spatial/editor/operations.ts`: build set/clear appearance operations where needed by manual controls/tests.
- `apps/web/src/features/spatial/editor/useEditorState.ts`: default this spatial workspace to 3D; leave selection identity and camera lifecycle intact.
- `apps/web/src/features/spatial/editor/components/SpatialEditor.tsx`: mount the studio, derive supported selection, preserve one canvas, and replace the `320px` inspector grid.
- `apps/web/src/features/spatial/editor/components/Spatial3DViewport.tsx`: accept one `conceptPreview` and comparison mode, override only the exact selected `ObjectMesh`/`FixtureMesh`, retain selection outline, and perform cleanup on selection/session/attempt changes.
- `apps/web/src/features/spatial/editor/components/AuthorizedVisualAsset.tsx`, `VisualAsset.tsx`, `GLTFVisualAsset.tsx`, and `USDVisualAsset.tsx`: carry optional appearance to the normalized clone.
- `apps/web/src/features/spatial/editor/components/useNormalizedAsset.ts`: clone materials per mesh when an override exists, apply color/roughness/metalness, and dispose only those clones. Never mutate cached GLTF/USD materials or shared geometries.
- `apps/web/src/features/spatial/editor/components/AssetFallback.tsx`: apply the same appearance mapping to procedural fallback geometry.
- `apps/web/src/features/spatial/editor/three/sceneProjection.ts`: include persisted appearance in projected object/fixture specs.
- `apps/web/src/features/spatial/editor/components/SpatialEditor.test.tsx` and `Spatial3DViewport.test.tsx`: add integration coverage for selection, concept replacement, cleanup, and acceptance.
- `apps/web/src/app/globals.css`: add studio layout/motion utilities expressed through existing tokens; no new unrelated palette.
- `apps/web/package.json`/lock: add `@vercel/queue` only.
- `apps/web/next.config.ts` and `.env.local.example`: allowed R2 asset origins if needed and deployment variables.

## RP4E3 component tree and visual behavior

### Binding frontend design read

Read this as a spatial design product for contractors and design-conscious clients, with a welcoming, tactile studio language. It should feel closer to arranging a real room with a helpful creative partner than operating business software. Use Renovex Tailwind/shadcn primitives and Geist; do not add a second design system or a marketing-site aesthetic.

Implementation dials for this screen are fixed at:

- **Design variance 7/10:** asymmetry comes from the large living canvas beside the compact workbench, not decorative chaos.
- **Motion intensity 4/10:** motion communicates selection, arrival, comparison, and completion. Nothing loops merely for atmosphere.
- **Visual density 5/10:** enough information to review a change safely, with one dominant action and progressive disclosure.

### Mandatory component and primitive contract

Use only the existing Renovex UI primitives listed below unless a missing accessibility behavior cannot be achieved with them:

| Need | Required component | Implementation rule |
|---|---|---|
| Primary and secondary actions | `@/components/ui/button` | `Imagine changes` and `Use Design` use the existing primary variant. Cancel, Current/Concept, collapse, and Regenerate use outline/ghost variants. Do not create bespoke button CSS. |
| Prompt | `@/components/ui/textarea` | Auto-grow only between 72px and 132px; Enter inserts a newline and Ctrl/Cmd+Enter submits. Keep the primary action outside the textarea. |
| Status labels | `@/components/ui/badge` | Use only for meaningful states such as “Material preview”, “3D concept”, “Needs review”. No decorative badge cloud. |
| Mobile studio | `@/components/ui/sheet` | Bottom placement, non-modal interaction with the visible room where the primitive permits it, capped height, internal scroll. |
| Plan grouping | `@/components/ui/card` | One outer change-plan card only. Shape/Look/Placement/Fit are rows separated with `Separator`, never four nested cards. |
| Section separation | `@/components/ui/separator` | Divide plan rows, history, and object controls. Avoid borders around every child. |
| Restore/loading | `@/components/ui/skeleton` | Skeleton geometry must match selected summary, two plan rows, and composer. No centered spinner. |
| Exact manual editing | existing `ElementInspector` | Render unchanged inside `ObjectControlsDisclosure`; do not recreate its controls in the AI UI. |
| Notifications | existing `sonner` only for transient network acknowledgement | Blocked, stale, failed, and ready states remain visible inside the studio. Do not hide durable state in a toast. |

Use the already installed `lucide-react` family consistently. Allowed icons for this slice are `WandSparkles`, `Sparkles`, `Palette`, `Move3D`, `Ruler`, `ScanSearch`, `RefreshCw`, `Check`, `X`, `ChevronDown`, `PanelRightClose`, `PanelRightOpen`, and `AlertTriangle`, at 16–18px with a consistent 1.75 stroke. Do not draw custom SVG paths, mix icon families, or use emoji as UI decoration.

Do not install Motion/Framer Motion, a chat package, another sheet/dialog library, a color-picker package, or another component system. Use CSS transitions and the existing `usePrefersReducedMotion` hook. The only planned new Web dependency remains `@vercel/queue`, which is unrelated to presentation.

### Exact desktop composition

At viewport widths of 1280px and above:

- The workspace outer grid is `minmax(0, 1fr) clamp(390px, 29vw, 440px)` with a 16px gap.
- The canvas column must occupy 68–74% of the usable workspace at 1440px. The dock must never exceed 440px.
- The 3D viewport is at least 680px high on a 900px-tall viewport. Toolbar/status space sits outside that height calculation.
- The studio is one continuous warm surface with one outer border and a soft warm-tinted shadow. It is not a stack of floating white cards.
- Studio header height is 56px. Body horizontal padding is 20px below 1440px and 24px at 1440px+. Vertical rhythm uses 8, 12, 16, 24, and 32px only.
- The collapse button sits in the studio header. When collapsed, the canvas takes the full workspace width and a single `AI Design` button floats 16px from the canvas top-right edge.
- `SelectedObjectLabel` floats 16px from the canvas top-left edge. `CurrentConceptToggle` is horizontally centered 16px above the canvas bottom edge. Neither control may cover the selected object’s bounding box when the screen-space projection is known; fall back to those corners when it is not.
- Keep the existing 2D/3D switch and revision indicator in the workspace toolbar. Do not duplicate them in the studio.

The studio order is fixed:

```text
56px header
selected element summary
state title + short supporting copy
contextual idea chips when actionable
prompt composer or progress/plan/concept body
primary action area
one divider
collapsed Object controls disclosure
```

The selected summary is a horizontal row, not a card: 36px category thumbnail/icon tile, category name, object/fixture label, and a quiet dimensions line if authoritative dimensions exist. It must never display raw IDs, JSON, storage keys, model names, or provider names.

### Exact medium and small compositions

- **1024–1279px:** canvas is full-width. Studio becomes a right overlay of `min(400px, 42vw)`, inset 12px from the viewport’s top/right/bottom content edges, with a 12px radius. A light scrim may cover only the final 15% of the canvas behind the panel; do not dim the full room.
- **768–1023px:** overlay width is 360px or 46vw, whichever is larger but still leaves at least 52vw of visible canvas. The Current/Concept control shifts left of the overlay.
- **Below 768px:** use the existing bottom `Sheet`. Default snap/height is 48dvh, maximum 55dvh, with a visible drag handle and 16px body padding. Preserve at least 45dvh of the 3D room. The prompt action remains visible at the sheet bottom without covering content. Do not use `h-screen`; use dynamic viewport units.
- At every width, the canvas remains mounted when the studio changes form. Responsive changes must not reset camera pose, selection, or a loaded concept.

### Exact visual styling

- Continue using Geist from the app root. No display serif and no new font request.
- Use `--background` for the workspace, `--card` for the dock, `--foreground` for primary text, `--muted-foreground` for support text, rust `--primary` for the current primary action/selection accent, and warm `--accent` for hover/selected chips.
- Do not introduce purple, blue AI gradients, neon glows, black glass panels, or a dark cyberpunk canvas treatment.
- Use one radius rule: studio/plan card 14–16px, controls 10–12px, chips/buttons may be full-pill only when their compact semantic role warrants it. Avoid arbitrary mixtures.
- Use at most one visible shadow layer on the studio and one on floating canvas controls. Tint shadows toward the existing warm background; avoid pure black blur.
- Plan section labels use sentence case at 13–14px semibold. Body copy is 13–14px with a 20px line height. The state heading is 20–24px, maximum two lines. No oversized marketing headline inside the product workspace.
- Idea chips have a quiet accent fill, one short line, and a tactile `scale(.98)` or 1px press response. They insert/edit prompt text; they never submit automatically.
- Material swatches are 28–32px circles or rounded squares with a visible border, a text label, and an accessible name. Color is never the sole signal.
- Warnings use `AlertTriangle`, a warm low-saturation warning surface, and specific copy. Do not turn ordinary assumptions into alarming banners.
- Progress is a vertical sequence of up to three stages with one active indicator and completed checks. No progress bars, fake percentages, skeleton room, or full-screen overlay.

### Component anatomy and copy contract

`AIDesignStudio` owns layout/state selection only. It must not fetch directly or contain renderer logic. Its props are the selected canonical target, derived UI state, callbacks, and the existing inspector content.

`DesignStudioWelcome` must render:

```text
Selected: Sofa
Heading: “What should we try with this sofa?”
Support: “Describe the shape, finish, or placement you have in mind.”
Chips: “Make it softer”, “Try warm timber”, “Curve the back”, “Move it off the wall”
```

Chips are derived from a small checked-in category map with a neutral fallback. Use 3–5 chips, vary them by object category, and keep each under 28 characters. Do not call another AI service to generate chips.

`DesignPromptComposer` uses placeholder copy such as “Try a curved back in deep green fabric…” and the primary label `Imagine changes`. It must not say “Send”, show an avatar, show chat bubbles, or append a transcript beneath the field.

`AIChangePlanCard` has exactly these four possible rows:

1. **Shape:** geometry summary or “Keep the current shape”.
2. **Look:** color swatch, material family, roughness, metallic state, or “Keep the current finish”.
3. **Placement:** plain-language move/resize summary using formatted metric values, or “Stay in the current position”.
4. **Fit:** passed checks, assumptions, or blocker/warning copy. It never claims structural validation beyond the Go plan.

Omit a row only when the server contract cannot express it; do not replace rows with raw JSON. The footer presents one primary `Create concept`/`Preview change` action according to execution flags and one quiet edit-prompt action. The GPU window notice appears immediately above the confirm action only when new geometry generation is required.

`DesignProgress` uses these operator-safe stages:

- Reference: “Preparing the visual direction”
- Geometry: “Shaping the 3D concept”
- Validation: “Checking the model in your room”

Supporting copy says the room remains usable. It must not mention Cloudflare, FLUX, Hugging Face, Hunyuan, ZeroGPU, queues, workers, retries, or expected minutes.

`ConceptActions` order is fixed: `Use Design` primary, `Regenerate` outline, `Cancel` ghost/destructive-text. Current/Concept remains on the canvas, not repeated in the dock. After ready, show at most three refinement chips such as “Softer curves”, “Darker finish”, or “A little narrower”; clicking starts a new prompt draft and does not silently regenerate.

### Reference translation rules

The implementation agent must inspect the linked current public references before coding the final composition and record a short note in the PR description explaining what was borrowed:

| Reference | Borrow this principle | Do not copy |
|---|---|---|
| [IKEA Kreativ](https://www.ikea.com/us/en/newsroom/corporate-news/ikea-launches-new-ai-powered-digital-experience-empowering-customers-to-create-lifelike-room-designs-pub58c94890/) | The room remains the dominant workspace; furniture is swapped and moved in place with immediate visual consequence. | IKEA colors, catalog/product-commerce chrome, exact toolbar arrangement, or branding. |
| [Planner 5D AI Designer](https://support.planner5d.com/en/articles/9310416-ai-designer-design-with-ai-web) | Clear generate → inspect → shuffle/refine → save mental model, with a creative magic-wand entry point. | Its room-type/style form, result gallery, subscription/credit UI, or endless multi-concept layout. |
| [Apple RoomPlan](https://developer.apple.com/augmented-reality/roomplan/) | Restrained outlines and spatial feedback make the selected recognized element tangible and trustworthy. | Apple scanning coaching, AR capture chrome, translucent Apple materials, or a dollhouse duplicate. |
| [Polycam Render Mode](https://learn.poly.cam/hc/en-us/articles/28785257328276-How-to-Use-Render-Mode) | Keep 3D content visually dominant and move secondary tools into compact floating controls. | Polycam navigation, dark viewer theme, capture/export controls, or icon placement. |

These references define interaction principles, not a mood board to average together. Renovex’s warm tokens, current app shell, Geist typography, and rust accent remain the visual identity.

### Prohibited frontend outcomes

Reject the implementation during review if any of these appears:

- A generic chatbot rail with user/assistant bubbles, avatars, timestamps, or an accumulating transcript.
- More than one R3F canvas or a generated-model preview detached from the actual room.
- A full-screen modal/spinner during planning or generation.
- Four equal generic cards for Shape/Look/Placement/Fit.
- Raw operation JSON, property grids, provider status codes, or storage/model identifiers.
- Purple/blue AI gradients, neon edge glows, glassmorphism across the whole panel, or a black cyberpunk viewer.
- A permanently disabled composer as the empty state.
- More than one dominant primary-colored button visible in the studio at a time.
- Continuous decorative animation, looping sparkles, particle fields, or camera motion initiated by AI state.
- A mobile sheet that hides the full room, unmounts the canvas, or loses selection/camera state.
- A Current/Concept comparison implemented as screenshots, two canvases, or side-by-side viewers.
- New font, icon, form, modal, or component libraries without an evidenced accessibility gap in the existing primitives.

### Frontend visual acceptance gate

Before RP4E3 is accepted, capture Playwright screenshots for every major state at `1440x900`, `1180x820`, `768x1024`, and `390x844`. Store approved baselines with `apps/web/e2e/spatial-ai-studio.spec.ts-snapshots` using the repository’s existing browser naming convention.

Review must confirm:

1. At 1440px, measured canvas and dock widths fall inside the 68–74% and 26–32% targets, and the dock is 390–440px.
2. The room remains visible and interactive in planning, generation, failure, and mobile-sheet states.
3. Only the selected object changes between Current and Concept screenshots; the camera and all surrounding elements remain pixel-stable except the short transition frame.
4. Empty, supported, unsupported, loading, plan, blocked, material preview, generating, ready, stale, failed, cancelled, accepted, and continued-refinement states are visually authored rather than falling back to generic text blocks.
5. The selected target is evident through the restrained outline and DOM label without flooding the room with accent color.
6. The primary action hierarchy is unambiguous, all labels fit on one line at desktop, and text/button contrast passes WCAG AA.
7. Reduced-motion screenshots contain no transition-dependent hidden content, and keyboard-only navigation reaches the studio controls in logical order.
8. No item from the prohibited list is present.

```text
SpatialEditor
├─ SpatialWorkspaceToolbar
│  ├─ revision + existing 2D/3D switch
│  └─ existing add/reset tools
├─ SpatialCanvasStage                       68–74% desktop width
│  ├─ existing Spatial3DErrorBoundary
│  │  └─ existing Spatial3DViewport         one Canvas only
│  ├─ SelectedObjectLabel                   DOM overlay
│  ├─ CurrentConceptToggle                  DOM overlay
│  └─ compact “AI Design” reopen control
└─ AIDesignStudio                           390–440px / 26–32%
   ├─ StudioHeader + collapse control
   ├─ SelectedElementSummary
   ├─ state-specific content
   │  ├─ DesignStudioWelcome + IdeaChips
   │  ├─ DesignPromptComposer
   │  ├─ DesignProgress
   │  ├─ AIChangePlanCard
   │  └─ ConceptActions + RefinementChips
   └─ ObjectControlsDisclosure
      └─ existing ElementInspector
```

Desktop uses `grid-template-columns: minmax(0, 1fr) clamp(390px, 29vw, 440px)`. The canvas keeps a practical minimum height and receives the remaining width. Collapsing the studio leaves a small rust/warm floating reopen control, not an empty column.

At medium widths, keep the canvas full width and show a 360–400px right overlay with a soft scrim only under the panel edge. At small widths, use the existing `Sheet` as a bottom sheet capped around 48–55vh; keep at least 45vh of the room visible and allow the sheet body to scroll. Do not open a modal or navigate away.

The visual language uses the current warm off-white surfaces, rust primary action, soft apricot accent, generous rounded corners, concise display copy, small material swatches, and restrained line icons. It avoids chat bubbles and uses a studio/workbench composition: selected-object summary, creative prompt, visual plan card, and direct canvas actions.

### Explicit UX state machine

| State | Trigger | Canvas | Studio |
|---|---|---|---|
| Empty/no selection | workspace opens or selection clears | Full room; no AI overlay except reopen affordance | “Choose a piece in the room to imagine a change.” No disabled form wall. |
| Supported selection | object/fixture selected | Subtle existing outline plus DOM label with category | Warm contextual greeting, 3–5 category-specific chips, prompt, “Imagine changes”. Restore/create session for this exact `{kind,id}`. |
| Unsupported structural selection | wall/opening/service point/constraint | Existing selected outline | Explain that AI Design currently works with movable objects and fixtures; keep exact manual controls available. |
| Session restore | a session exists for RoomDraft + target | Keep current view interactive | Skeleton only in the studio; restore latest turns/attempt and accepted state. |
| Planning | turn submitted | Keep orbit/pan and selection | Stages: “Understanding your idea”, “Checking fit”, “Preparing the change plan”. No percentage. |
| Plan ready | latest turn proposed | Current view | `AIChangePlanCard` sections Shape, Look, Placement, Fit; swatches and warnings; Confirm action. GPU notice only if geometry is required. |
| Plan blocked | turn blocked | Current view | Specific blocker and editable prompt/refinement suggestions. No Confirm. |
| Confirmed/material-only | material-only attempt ready | Near-instant appearance override on selected current mesh | Current/Concept toggle appears; Cancel, Use Design, and refinement chips. |
| Confirmed/geometry generation | geometry/mixed attempt active | Current object remains; user can work/camera-move | Stages: “Creating the reference”, “Shaping the concept”, “Checking the model”. GPU execution-window notice shown. |
| Concept ready | attempt ready | Candidate replaces exact target in Concept mode with short crossfade; Current mode restores draft asset | Current/Concept toggle, Cancel, Regenerate, Use Design, and contextual refinements. |
| Regenerate | explicit button | Keep current or last concept until new one is ready; mark it as previous | New attempt, same plan/fingerprint, new progress stages; no GLM/planning stage. |
| Failure | reference/job terminal failure | Current remains authoritative | Safe retry guidance. Regenerate is offered only as explicit new attempt. |
| Stale plan | RoomDraft revision changes | Immediately force Current and remove temporary concept | Show “The room changed. Review this idea again.” Disable Confirm/Use/Regenerate until a new turn/plan is reviewed. |
| Cancel | user cancels | Remove temporary concept and force Current | Show compact cancelled state; allow a new prompt/refinement. |
| Accepted | Use transaction succeeds | Refetched authoritative RoomDraft renders persisted asset/appearance/placement | Brief success acknowledgment, then return to prompt with “Refine this design”. |
| Continued refinement | new prompt after acceptance | Accepted RoomDraft remains current | New immutable turn starts from `AcceptedDesign`; same session and selected identity. |

Keyboard focus moves to the new plan/ready/failure heading after a state transition, never into the WebGL canvas. All chips and toggle options are buttons with pressed state, progress copy uses `aria-live="polite"`, failures use an alert, and the selected element is mirrored in a DOM status node. Escape closes only the overlay/bottom sheet; it does not cancel generation. Reduced motion removes crossfades and panel transitions. Focus, contrast, and touch targets use the existing component primitives.

The interaction principles come from current public products without copying their layouts: IKEA Kreativ keeps redesign actions in the room, Planner 5D exposes generate–inspect–shuffle/refine–save, Apple RoomPlan uses clear spatial overlays and recognized-element feedback, and Polycam keeps the 3D artifact dominant with compact viewer tools.

## Renderer integration points

1. `SpatialEditor` passes `conceptPreview` and `comparisonMode` into the existing dynamic `Spatial3DViewport`; no second viewer or canvas is created.
2. `SceneContents` computes one render spec per object/fixture. If and only if `comparisonMode === "concept"` and both target kind and ID match, it substitutes candidate transform, dimensions, asset reference, and appearance. Every other element uses the authoritative RoomDraft projection.
3. `ObjectMesh` and `FixtureMesh` continue to place the group using the current room-local position/quaternion mapping. Candidate placement uses the already resolved absolute transform and dimensions; the browser does no spatial arithmetic or collision correction.
4. The selected outline remains around whichever version is visible. Its box dimensions switch with Current/Concept so spatial feedback stays honest. The DOM label identifies the canonical selected object, not the generated asset.
5. Material-only preview passes appearance into the current visual path immediately. Map `matte -> roughness 0.82`, `satin -> 0.48`, and `glossy -> 0.18`; `metallic` maps to `metalness 1` or `0`. `materialFamily` is retained for copy/future use but does not fabricate textures.
6. `useNormalizedAsset` deep-clones the object graph as today. When appearance exists, traverse meshes, clone each material (and each entry of material arrays), set supported standard/physical material fields, mark `needsUpdate`, and dispose those cloned materials on unmount/change. Geometry and loader cache remain shared.
7. When a concept first becomes ready and Concept mode is active, crossfade opacity over 180ms. With reduced motion, swap immediately. Avoid per-frame React state; invalidate R3F only during the transition.
8. On selection change, session target change, stale revision, cancel, acceptance, or unmount, clear `ConceptRenderState`, return toggle to Current, dispose override materials, and let current Suspense/error boundaries release the generated asset subtree.
9. A generated asset load failure falls back to the current category asset/procedural box and shows a studio error; it never removes the selected RoomDraft element.

## R2 storage and access plan

Use one private Standard-class bucket, for example `renovex-tester`, with these keys:

```text
spatial/{companyId}/{captureId}/{artifactId}
design-references/{companyId}/{roomDraftId}/{sessionId}/{turnId}/{attemptId}/reference.{jpg|png}
asset-generation/{companyId}/{jobId}/raw/{sha256}.glb
visual-assets/{companyId}/{assetId}/v{version}/{sha256}.glb
```

Keys remain server-generated and private. The design attempt stores its reference key; existing artifact and visual-asset Mongo records keep their current key/identity behavior.

- Implement the existing `ObjectStore` methods against R2’s S3 endpoint.
- Add provider-neutral presign interfaces for GET and PUT. R2 mode returns short-lived presigned URLs; local mode retains current HMAC proxy routes.
- Existing browser/RoomPlan artifact uploads must use direct presigned PUT in R2 mode because Vercel request bodies and ephemeral storage are not suitable for large captures. Preserve the current request-upload/finalize sequence and response shape (`url`, `method`, required headers, expiry).
- Bind content type and checksum header in the PUT signature. Finalize checks R2 object metadata/HEAD, declared checksum, and size; stream and hash only when the provider cannot return the checksum.
- VisualAsset access and Hunyuan source access return presigned GET URLs in R2 mode. The Go proxy remains local-only.
- Configure bucket CORS for the exact tester Web origin and localhost development origin, methods `GET|HEAD|PUT`, and only required headers. Do not enable a public `r2.dev` bucket.
- Apply lifecycle cleanup only to abandoned/failed design references and unclaimed raw staging objects after a conservative retention period. Published visual assets and source RoomPlan artifacts remain durable.

The local composition branch must keep today’s three physical subroots so current local files are not silently orphaned. The R2 branch passes one store to artifact, generation, visual asset, and design-reference adapters.

## Resend and test-sink plan

Extend `mail.Message` with optional `Headers map[string]string`. `ResendSender` POSTs `from`, one `to`, `subject`, `text`/`html`, and allowed custom headers to Resend’s `/emails` API using the existing `EmailSender` interface.

`TestSinkSender` receives the configured sink and wraps any sender:

```text
logical message.To = real client/supplier/user address
copied outbound message.To = EMAIL_TEST_SINK_ADDRESS
outbound header X-Renovex-Intended-Recipient = real address
outbound subject prefix = [TEST → real-address]
```

It never changes a company member, access grant, RFQ invitation, supplier challenge, award, delivery-attempt record, or any Mongo field. Existing services continue to record the logical recipient before calling the adapter.

Startup rules:

- `EMAIL_PROVIDER=smtp` for local; `EMAIL_PROVIDER=resend` for tester.
- `EMAIL_DELIVERY_MODE=direct|test_sink`.
- `APP_ENV=production` with `test_sink` is a fatal configuration error.
- `APP_ENV=tester` requires Resend, nonempty `RESEND_API_KEY`, `RESEND_FROM`, and `EMAIL_TEST_SINK_ADDRESS`; the sink must equal the inbox permitted by the tester Resend account when using `resend.dev`.
- Secrets are server-side only and never enter Next public variables or logs.

Prove company/client access-grant, RFQ invitation/resend, supplier verification challenge, and supplier award/outcome flows end to end. Registration verification remains deferred because the repository has no such workflow today.

## Atlas Free/M0 bootstrap, migrations, and indexes

Create a fresh Atlas project/Free cluster near the chosen Vercel region, one SCRAM application user scoped to the Renovex database, and the required network access. M0 currently provides a three-node replica set, so the repository’s multi-document transactions remain available. Keep Mongo clients cached across warm Vercel invocations and cap pool sizes to avoid the 500-connection free-tier limit.

The bootstrap command must:

1. Refuse `MONGO_DATABASE` values `admin`, `local`, or `config`.
2. Connect and ping.
3. Create the exact collection manifest if absent. The manifest consists of all current repository collections plus `spatial_design_generation_attempts`, `spatial_design_acceptances`, and `schema_migrations`. `_readiness_probe` remains temporary and is not part of the durable manifest.
4. Call the same repository `EnsureIndexes` sequence used by `BuildServices`; do not duplicate index definitions in the command.
5. Create the five attempt indexes and three acceptance indexes listed above.
6. Verify a short multi-document transaction using the existing readiness probe and clean up the probe document.
7. Record migration `20260909_001_m85c_tester_bootstrap` with manifest checksum, application version, and applied time.
8. Read back collection names and index names and fail on a missing required item. Extra system collections are ignored; unexpected application collections are reported and require an explicit allow flag.
9. Insert no company, user, material, demo tenant, or local-development data. `cmd/demoseed` must never run against tester/production and remains guarded.

Current repositories have no Mongo JSON Schema validators and enforce validation in Go before writes. The smallest safe bootstrap keeps validators empty rather than inventing partial validators that drift from Huma/domain rules. The schema manifest explicitly records `validators: {}` so this is a deliberate state. All M8.5C document changes are additive; existing local data needs no backfill. Old RP4E0 jobs are decoded through the source compatibility rule.

The exact manifest is:

```text
users
auth_sessions
companies
company_members
clients
projects
properties
spaces
work_items
materials
cost_items
workers
labour_entries
estimates
quotations
quotation_counters
access_grants
access_group_states
approvals
audit_events
material_requirements
rfqs
rfq_counters
suppliers
supplier_offerings
material_supplier_preferences
issued_rfq_versions
rfq_issuance_chains
rfq_amendment_drafts
supplier_invitations
invitation_delivery_attempts
supplier_access_exchanges
email_verification_challenges
verification_delivery_attempts
supplier_verification_rate_limits
supplier_sessions
supplier_session_invitation_bindings
supplier_offer_chains
supplier_offer_drafts
supplier_offer_versions
supplier_offer_eligibilities
supplier_offer_withdrawals
award_decision_chains
award_drafts
award_revisions
award_line_claims
award_outcomes
award_outcome_deliveries
award_outcome_acknowledgements
work_resource_requirements
ai_generation_batches
ai_suggestions
spatial_captures
spatial_room_versions
spatial_space_states
spatial_artifacts
spatial_room_drafts
spatial_room_draft_edits
spatial_visual_asset_versions
spatial_asset_generation_jobs
spatial_design_sessions
spatial_design_turns
spatial_design_generation_attempts
spatial_design_acceptances
schema_migrations
```

The drift test extracts repository collection names and compares them with this manifest so future repositories cannot be omitted silently. The required seed-document list is empty.

## Vercel deployment and background execution

Create three Vercel Hobby projects from the same repository:

| Project | Root directory | Runtime | Purpose |
|---|---|---|---|
| `renovex-web-tester` | `apps/web` | Next.js/Node | UI, queue enqueue bridge, and private queue consumer |
| `renovex-api-tester` | `backend` | Go runtime | Huma API and bounded internal worker endpoints |
| `renovex-ai-tester` | `ai-service` | Python 3.12/FastAPI | RP4E1 reasoning and RP4E2 reference generation |

Refactor Go router construction once so `cmd/api` and `api/index.go` use the same handler. The Vercel function lazily initializes config, Mongo, services, and the router once per warm process. It never starts the local ticker or listens on a port. The Python project exposes the existing ASGI app through `api/index.py` and excludes tests/caches from the bundle.

Use Vercel Queues as the wake/execution layer, not as state storage:

1. Go creates the attempt/job in Mongo first.
2. Go calls the protected Web enqueue route with `{kind:"design_attempt"|"asset_job", id}`. The Web route publishes to the appropriate Vercel Queue topic with `@vercel/queue`.
3. The private queue consumer calls one protected Go worker endpoint.
4. Go claims the Mongo record using existing lease fencing, works for at most 45 seconds, checkpoints, and returns whether another wake is needed.
5. The consumer acknowledges terminal/idle work or publishes a delayed follow-up. At-least-once delivery is harmless because Mongo claim/idempotency rules decide every transition.
6. When reference generation creates the RP4E0 job, it publishes the asset-job wake immediately; this matters because the existing live-verified Gradio event can expire if submission is not followed promptly.

Do not use Vercel Workflow for this slice. It would add a second durable orchestration state machine around Mongo and still cannot make the existing nine-minute SSE call fit a Hobby function. Queues give the required wake/retry behavior while Mongo remains authoritative.

If Vercel Queues Beta cannot be provisioned today, use the smallest fallback: while a generation attempt is nonterminal, the authenticated Web polling loop calls a protected-by-user-auth public `drive` action that advances one bounded Mongo phase, then reads status. Reloading the workspace resumes driving. Mongo remains durable, and no provider start is repeated. Document this as tester-only and switch it off when queues are enabled. Do not use Hobby Cron as the interactive worker; its schedule is too coarse and it has no retry guarantee.

### Environment variables

Go API:

```text
APP_ENV=tester
MONGO_URI=...
MONGO_DATABASE=renovex_tester
JWT_ACCESS_SECRET=...
JWT_REFRESH_SECRET=...
ALLOWED_ORIGINS=https://<web-project>
REFRESH_COOKIE_SECURE=true
EXTERNAL_API_BASE_URL=https://<api-project>
AI_SERVICE_URL=https://<ai-project>
AI_INTERNAL_TOKEN=...
OBJECT_STORE_PROVIDER=r2
R2_ACCOUNT_ID=...
R2_ACCESS_KEY_ID=...
R2_SECRET_ACCESS_KEY=...
R2_BUCKET=renovex-tester
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
REFERENCE_IMAGE_MAX_BYTES=8388608
HUGGINGFACE_SPACE_URL=...
HUGGINGFACE_TOKEN=...
HUNYUAN_PROVIDER_TIMEOUT=45s
SPATIAL_WORKER_TOKEN=...
SPATIAL_QUEUE_ENQUEUE_URL=https://<web-project>/api/internal/spatial-generation/enqueue
SPATIAL_QUEUE_ENQUEUE_TOKEN=...
EMAIL_PROVIDER=resend
EMAIL_DELIVERY_MODE=test_sink
EMAIL_TEST_SINK_ADDRESS=<Resend account inbox>
RESEND_API_KEY=...
RESEND_FROM=Renovex <onboarding@resend.dev>
ASSET_GENERATION_RUNTIME_NOTICE_ENABLED=true
ASSET_GENERATION_RUNTIME_NOTICE_TEXT=3D concepts use a limited test GPU execution window. If the window closes before this concept finishes, you can try again.
```

Python AI:

```text
INTERNAL_API_TOKEN=<same AI internal token>
SPATIAL_AI_PROVIDER=glm
GLM_API_KEY=...
GLM_MODEL=glm-5.3
REFERENCE_IMAGE_PROVIDER=cloudflare
CLOUDFLARE_ACCOUNT_ID=...
CLOUDFLARE_API_TOKEN=...
CLOUDFLARE_FLUX_MODEL=@cf/black-forest-labs/flux-1-schnell
REFERENCE_IMAGE_TIMEOUT_SECONDS=45
REFERENCE_IMAGE_MAX_BYTES=8388608
```

Web:

```text
NEXT_PUBLIC_API_BASE_URL=https://<api-project>
SPATIAL_API_INTERNAL_URL=https://<api-project>
SPATIAL_WORKER_TOKEN=...
SPATIAL_QUEUE_ENQUEUE_TOKEN=...
```

Add all existing invitation, supplier verification/session, rate-limit, visual-asset, and asset-source signing keyrings from `.env.example`. Never place these or provider keys in `NEXT_PUBLIC_*` variables. Set the three projects to the same supported Vercel region and choose the closest Atlas/R2 regions available; connection latency matters on M0.

## Test plan

### Go unit/domain tests

- Appearance accepts only the four fields and canonical hex; operations target only existing object/fixture and do not mutate other elements.
- Confirm accepts only the latest proposed valid turn/fingerprint/revision; blocked, superseded, stale, cross-tenant, and older turns fail.
- Confirm replay returns the same attempt; conflicting reuse fails; concurrent confirms produce one active attempt.
- Regenerate creates a new attempt/seed/reference/Hunyuan job while retaining turn/fingerprint and never calling the reasoning provider.
- Reference provider-start marker is written before dispatch; unknown outcome becomes `needs_attention`; it is never retried automatically.
- Material-only reaches ready without Python/Hunyuan and produces an appearance-only concept.
- Geometry/mixed persists reference checksum/key and submits through the existing RP4E0 service exactly once.
- Bounded RP4E0 resume releases/reclaims the same event ID after deadline and never calls Hunyuan start twice.
- Cancel wins over late completion and Use rejects abandoned attempts.
- Use applies ordered mixed operations atomically; a forced failure rolls back draft, edit records, acceptance, session, and attempt.
- Use replay returns the original acceptance; a stale revision applies nothing.
- Use binds geometry only through `assign_visual_asset` and carries `{assetId,version}`, never URL/key.
- Session pin advances only after its own accepted transaction; unrelated RoomDraft edits make it stale.
- R2 keys are tenant/session scoped; presigned access binds method/key/content type/expiry.
- Test sink redirects only the outbound copy and preserves the intended-recipient header.
- Production plus test-sink fails configuration; incomplete R2/Resend/Cloudflare config fails startup.

### Python tests

- Strict request rejects extra fields, nonpositive/oversized dimensions, arbitrary color names, unsupported content type, invalid base64, or oversized image.
- Prompt golden test verifies deterministic ordering, isolated full object, three-quarter front view, canonical proportions, no room/people/text, and no named-part persistence claim.
- Mock bytes are deterministic for fingerprint/seed.
- Cloudflare request uses the exact configured model and seed and is invoked once; timeout/unknown outcome is surfaced without retry.
- Protected route rejects missing/wrong token and never returns provider credentials.

### Web unit/integration tests

- Every required UX state renders from reducer/server fixtures.
- Selecting object A creates/restores only A’s session; selecting B clears A’s temporary concept and starts/restores B independently.
- Structural selection displays unsupported state while manual inspector remains usable.
- Change plan uses Shape/Look/Placement/Fit sections, swatches, warnings, and conditional GPU notice.
- Material-only preview updates the same mesh immediately and does not request generation polling.
- Concept mode replaces exactly one selected element; Current restores the authoritative asset/transform/dimensions.
- Stale/cancel/selection change cleanup removes the concept and disposes override materials.
- Current/Concept and chips have semantic button states; focus/live regions/reduced motion work.
- Desktop/medium/mobile layout tests preserve canvas area and use dock/overlay/bottom sheet respectively.
- Use invalidates/refetches RoomDraft and leaves the session active for a refinement prompt.
- Queue bridge validates its secret, consumer calls one bounded worker step, and duplicate deliveries are safe.

### Integration and E2E

- Go↔Python contract test with mock reference provider, real R2-compatible test double, and fake Hunyuan.
- Mongo replica-set test for concurrent confirm, atomic mixed Use, cancellation race, stale revision, and old RP4E0 source document compatibility.
- Playwright desktop and mobile flows: no selection → select sofa → prompt → plan → confirm → progress → concept compare → regenerate → use → refine.
- Playwright material-only flow proves no geometry notice and near-instant preview.
- Tester smoke uses Cloudflare FLUX and one real Hunyuan generation, confirms R2 prefixes/presigned reads, and accepts the result.
- Email smoke triggers access grant, RFQ invite, supplier code, and award; each arrives at the sink while the UI/Mongo still shows the real logical recipient.
- Cold-start/reload smoke proves session/attempt restore and queue continuation.

### Verification commands

Run from `C:\Users\shananth\mvp`:

```powershell
Set-Location backend
$env:GOTELEMETRY='off'
gofmt -w (Get-ChildItem internal,cmd,api -Recurse -Filter *.go | ForEach-Object { $_.FullName })
go test ./...
go run ./cmd/openapi -out ../apps/web/openapi/openapi.json
go run ./cmd/dbbootstrap verify

Set-Location ..\ai-service
.\.venv\Scripts\python.exe -m ruff check app tests api
.\.venv\Scripts\python.exe -m pytest -q

Set-Location ..\apps\web
npm run openapi:generate
npm test
npm run typecheck
npm run lint
npm run build
npx playwright test
```

For the deployed tester, run the repository’s composed golden journey plus the new spatial/email smoke suite against the three Vercel URLs and clean Atlas database. Compare the generated OpenAPI file after regeneration and fail on an uncommitted schema drift.

## Implementation order optimized for today

### Gate 0 — RP4E1 contract stabilization

Freeze the implemented RP4E1 public DTO and add the `AcceptedDesign`/canonical-color amendment first. Regenerate OpenAPI. No downstream work should guess these shapes.

### Gate 1 — canonical persistence and atomic acceptance

Implement `VisualAppearance`, its two operations, RoomDraft fields, batch transaction support, attempt/acceptance models, indexes, and idempotency fingerprints. This is the shared contract lane and must land before parallel integration.

### Parallel lane A — Python reference generation

Implement the strict request/result schema, deterministic prompt/mock, FLUX provider, route, config, and tests. This depends only on the frozen reference contract.

### Parallel lane B — R2, Resend, and serverless composition

Implement R2 store/presigning, Resend/test-sink, typed config guards, shared HTTP handler, Vercel entrypoints, and Atlas bootstrap. This can proceed independently once the Go interfaces in Gate 1 are fixed.

### Parallel lane C — RP4E3 visual shell against fixtures

Build the responsive studio components, reducer, accessibility behavior, plan cards, and concept projection with mocked API fixtures. Keep `SpatialEditor`/renderer integration behind the frozen `ConceptRenderState` prop until Go OpenAPI is regenerated.

### Gate 2 — RP4E2 orchestration and RP4E0 bounded execution

Connect attempts to the Python client and R2, add the RP4E0 source discriminator/internal submit wrapper, and make worker processing bounded/reconnectable. Prove no duplicate GLM, FLUX, or Hunyuan start calls under retries and races.

### Gate 3 — public API and Web integration

Register the six routes, regenerate OpenAPI, implement Web queries/mutations, wire studio state into `SpatialEditor`, and integrate temporary mesh/appearance rendering into the one existing canvas.

### Gate 4 — Vercel Queue wake path

Add the enqueue bridge/consumer, connect Go wake publisher, and run duplicate-delivery/cold-start tests. If Queue provisioning blocks the day, activate the documented tester-only UI-drive fallback without changing Mongo state semantics.

### Gate 5 — clean deployment and proof

Deploy AI, API, then Web; configure R2 CORS, run `dbbootstrap apply` against an empty Atlas database, and deploy queue topics/consumer. Run material-only first, then one geometry generation, then all Resend test-sink workflows. Record the deployed build’s config names, schema migration ID, smoke results, and known ZeroGPU quota limitation in `tasks/done.md` only after the checks pass.

## Scope guard

This plan does not introduce multi-element or whole-room redesign, simultaneous A/B/C concepts, PBR/Hunyuan Paint, generated textures, named-part persistence, provider-side cancellation guarantees, MapAnything, M9 work, or general LOD/decimation work. A real generated asset that breaks current validation/rendering may justify the smallest targeted compatibility fix; otherwise those areas stay untouched.

## Current public references used as principles and deployment facts

- [IKEA Kreativ](https://www.ikea.com/us/en/newsroom/corporate-news/ikea-launches-new-ai-powered-digital-experience-empowering-customers-to-create-lifelike-room-designs-pub58c94890/) for editing and rapidly swapping furnishings in the room itself.
- [Planner 5D AI Designer](https://support.planner5d.com/en/articles/9310416-ai-designer-design-with-ai-web) for the generate, shuffle/refine, and save mental model.
- [Apple RoomPlan](https://developer.apple.com/augmented-reality/roomplan/) for recognized-element overlays and clear spatial feedback.
- [Polycam Render Mode](https://learn.poly.cam/hc/en-us/articles/28785257328276-How-to-Use-Render-Mode) for content-dominant 3D viewing with compact controls.
- [Cloudflare FLUX.1-schnell](https://developers.cloudflare.com/workers-ai/models/flux-1-schnell) and [Workers AI pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/) for the model contract and free daily allocation.
- [Cloudflare R2 pricing](https://developers.cloudflare.com/r2/pricing/), [presigned URLs](https://developers.cloudflare.com/r2/api/s3/presigned-urls/), and [R2 CORS](https://developers.cloudflare.com/r2/buckets/cors/) for the private single-bucket design.
- [Vercel Queues](https://vercel.com/docs/queues), [Go runtime](https://vercel.com/docs/functions/runtimes/go), [Python runtime](https://vercel.com/docs/functions/runtimes/python), and [Hobby limits](https://vercel.com/docs/plans/hobby) for the three-project and bounded-worker design.
- [Atlas Free cluster limits](https://www.mongodb.com/docs/atlas/reference/free-shared-limitations/) and [MongoDB transactions](https://www.mongodb.com/docs/manual/core/transactions/) for clean bootstrap and transaction constraints.
- [Resend test-domain restriction](https://resend.com/docs/knowledge-base/403-error-resend-dev-domain), [Send Email API](https://resend.com/docs/api-reference/emails/send-email), and [free quotas](https://resend.com/docs/knowledge-base/account-quotas-and-limits) for the test-sink adapter.
