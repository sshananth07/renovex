# RP4E1 Conversational Spatial Design Reasoning — Repository-Specific Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The Renovex repository explicitly forbids every Git command, so the commit steps normally required by the planning skill are intentionally omitted.

**Goal:** Add a durable, tenant-safe, single-element conversational design-planning pipeline that makes at most one GLM-5.3 inference attempt per admitted turn, validates the model response structurally in Python and authoritatively in Go, and returns an immutable, fingerprinted plan for human review without changing RoomDraft, assets, materials, or Three.js state.

**Architecture:** The authenticated Web client talks only to the existing Go/Huma spatial module. Go resolves the tenant-owned RoomDraft, target and compact neighborhood, reserves a durable session turn, and calls a new internal endpoint in the existing FastAPI service. Python makes one non-retrying GLM request and validates a turn-local proposal. Go merges that delta into the session’s current working design, resolves supported semantic operations to canonical absolute operations, performs deterministic fit checks, fingerprints the cumulative plan, and persists the terminal turn. Python remains stateless and has no MongoDB authority.

**Tech stack:** Go 1.25, Huma v2, mongo-driver v2 with replica-set transactions, SHA-256 canonical fingerprints, Python 3.12, FastAPI, Pydantic v2, direct HTTPX transport to Z.AI, Next.js 16, openapi-fetch, React Query, Vitest/MSW.

**Spec:** Approved RP4E1 design in ChatGPT conversation `6a9a7a97-fee4-83e8-9a01-b44502a69833`, plus the repository’s source-of-truth documents `phase1.md`, `CLAUDE.md`, and `docs/superpowers/specs/M8.5C-Spatial-Intelligence-Design-Spec-Refined.md`.

## Global constraints

- One session targets exactly one existing RoomDraft element. For RP4E1, narrow supported targets to `object` and `fixture`; these are the only RoomDraft element kinds with RP4D visual-asset bindings and all required category/transform/dimensions fields.
- One newly admitted live turn makes at most one outbound GLM request. There are zero provider calls for rejected, stale, replayed, in-progress, or mock turns. No provider retries, fallback model, repair call, classifier call, reviewer call, or schema-repair call.
- “Exactly once at the provider” cannot be guaranteed after a process or network failure. Renovex guarantees at-most-once dispatch per durable turn by recording `providerStartedAt` before the call and never redispatching that turn.
- Go resolves authorization, RoomDraft revision, target identity/category, context, allowed operations, spatial coordinates, fit, safety, cumulative working design, execution flags, fingerprint, persistence, and stale-plan status.
- Python performs language interpretation and strict structural validation only. It receives a bounded authorized projection, never queries MongoDB, and never receives credentials, URLs, binary assets, raw RoomPlan files, or other tenants’ data.
- RP4E1 never calls Hunyuan, creates an asset-generation job, produces a reference image or GLB, publishes/binds a `VisualAssetVersion`, applies a material, calls a RoomDraft edit method, or changes Three.js/UI scene state.
- Every terminal proposal is immutable. Refinement creates a new turn; it never overwrites prior turns.
- A session is pinned to its creation-time RoomDraft revision. A RoomDraft revision change makes it stale and read-only. RP4E1 requires a new session; it does not silently rebase or replay relative spatial changes.
- All user/provider strings and arrays have explicit byte/count limits. All spatial numbers must be finite. Unknown JSON fields fail closed at both Python and Go transport boundaries.
- Public errors contain stable machine codes and fixed contractor-safe text. Provider bodies, prompts, credentials, reasoning content, stack traces, and invalid model output are never returned or logged.
- The existing Copilot suggestion workflow, Gemini provider, retries, `ai_generation_batches`, and `ai_suggestions` remain unchanged.

---

## Repository audit and required design amendments

| Finding | Repository evidence | Plan decision |
|---|---|---|
| The “existing Project Copilot service” is an AI Scope & Resource Preview service, not a conversational Copilot. | `ai-service/app/providers/base.py` has three suggestion methods; `ai-service/app/main.py` exposes only spaces/work-items/resources routes. General Project Copilot is deferred in the M8.5C spec. | Reuse the FastAPI deployment, bearer auth, error envelope and Go HTTP boundary. Add a separate spatial reasoning protocol/service/provider rather than widening the Copilot `AIProvider`. |
| The master M8.5C design names Qwen as the V1 spatial provider and describes confirmed RoomVersion whole-room concepts. | `M8.5C-Spatial-Intelligence-Design-Spec-Refined.md` §§17–26. | RP4E1 is a narrower approved amendment: GLM-5.3, one selected mutable RoomDraft object/fixture, confirmation-ready plan only. Record this amendment in the RP4E1 design doc before implementation. |
| A scanned sofa is a `RoomDraftObject`, not a fixture. | `backend/internal/spatial/roomdraft.go` defines objects with free-form category; fixtures use the fixed AC/boiler/cabinetry/etc. vocabulary. | The sofa example resolves to `move_object`; `move_fixture` is only for fixed fixtures. |
| Existing UI selection supports six element kinds, while RP4D asset assignment supports only object/fixture. | `apps/web/src/features/spatial/editor/types.ts` defines six selection kinds and a separate `VisualAssetTargetKind = object | fixture`. | Do not reuse either enum blindly. Define `SpatialDesignTargetKind = object | fixture`; reject wall/opening/servicePoint/constraint at session creation with typed 422. |
| No backend fit/clearance engine exists. Existing edit validation mostly checks shape and target existence. | No containment/footprint/clearance helpers under `backend/internal/spatial`; `editoperation.go` directly applies coordinates. | Add deterministic XZ footprint, closed-boundary, collision and clearance helpers. Never import Web renderer fallback dimensions as authoritative. Missing dimensions block spatial operations. |
| RoomDraft has no material/colour/style truth. A visual asset reference is only immutable asset identity/version. | `RoomDraftObject`/`RoomDraftFixture` contain optional `VisualAssetRef`; `visualasset.go` excludes appearance semantics. | Initial appearance is explicitly unknown. Only accepted session working-design proposals are sent as conversational appearance context. Do not infer material from category or asset ID. |
| The approved text says a material-only follow-up retains earlier proposed geometry, while also setting `hunyuanRequired=false` and omitting the runtime notice. | This is internally contradictory when the retained geometry has not been generated. | Split turn-local and cumulative flags. `turnRequiresAssetGeneration=false` for “Actually make it beige”; `execution.hunyuanRequired=true` remains true while cumulative custom geometry is pending. The runtime notice follows cumulative execution truth. This is the safest amendment and should be approved with this plan. |
| The approved conceptual statuses include confirmed/completed/cancelled, but RP4E1 has no confirm/cancel endpoint. | RP4E1 explicitly stops at a confirmation-ready plan. | Implement only reachable statuses: session `active`; turn `reserved|reasoning|proposed|blocked|failed|needs_attention|superseded|stale`. Add confirmation/completion/cancellation states with the later execution slice that owns their transitions. |
| Existing RoomDraft edits are CAS protected, but RP4E1 may not mutate or lock RoomDraft. | `roomdraftedit_repository_mongo.go` atomically writes RoomDraft + edit history; a read-only transaction cannot fence future edits. | Check revision before reservation, immediately before provider dispatch, after provider return, and on reads. Persist `basedOnRoomDraftRevision`. Never claim this eliminates the final race; future confirmation/execution must recheck revision and fingerprint atomically with its own action. |
| Existing Go AI response decoding is unbounded and accepts unknown fields. | `backend/internal/platform/ai/client.go` uses `io.ReadAll` and `json.Unmarshal`. | Add a separate strict, bounded `SpatialClient`; do not change Copilot decoding compatibility in RP4E1. |
| Existing Gemini retries conflict with one-call reasoning. | `ai-service/app/config.py` defaults `AI_PROVIDER_MAX_RETRIES=2`; `providers/gemini.py` loops transient errors. | GLM uses direct HTTPX with redirects and transport retries disabled. It does not share Gemini’s retry helper. |
| Full Web OpenAPI regeneration will also reveal RP4E0 routes that were intentionally omitted before. | RP4E0 completion notes say its backend-only routes were not copied into Web schema. | Accept the generated schema expansion as synchronization only. Add no RP4E0 Web wrappers or UI. |

## Final public HTTP contract

All routes are registered under the existing authenticated Huma group and derive `companyId` and `createdByUserId` from `identity.PrincipalFromContext`.

### `POST /spatial/design-sessions`

Creates or idempotently replays an empty session. It does not call Python.

```json
{
  "clientSessionId": "uuid-created-once-per-create-click",
  "roomDraftId": "…",
  "expectedRoomDraftRevision": 17,
  "target": {"kind": "object", "id": "object_sofa_123"}
}
```

Go loads the tenant-owned RoomDraft, then its tenant-owned capture through `RoomDraft.CaptureID`, deriving `projectId` and `spaceId`. It confirms the target exists, its transform is finite, and the revision matches. Same `(companyId, clientSessionId)` plus same canonical request replays the stored session; changed content returns 409 `design_session_request_conflict`.

### `POST /spatial/design-sessions/{id}/turns`

Creates one immutable reasoning turn and runs it synchronously after durable reservation.

```json
{
  "clientRequestId": "uuid-created-once-per-send-click",
  "instruction": "Make this sofa curved, dark green velvet with light wooden legs and move it 20 cm away from the wall."
}
```

Limits: `clientRequestId` 1–128 bytes; trimmed instruction 1–2,000 UTF-8 bytes. The request contains no room geometry, target snapshot, provider selector, allowed operation, company/user ID, fingerprint, or fit result.

The successful/replayed response is a `SpatialDesignTurnDTO`. Same request ID plus same request fingerprint replays it with zero provider calls. Same ID with different content returns 409 `design_turn_request_conflict`. A concurrent distinct request while `activeTurnId` is set returns 409 `design_turn_in_progress`.

### `GET /spatial/design-sessions/{id}`

Returns the session, bounded current working design, latest ready turn reference, and derived stale information:

```json
{
  "session": {
    "id": "…",
    "roomDraftId": "…",
    "basedOnRoomDraftRevision": 17,
    "target": {"kind": "object", "id": "object_sofa_123"},
    "status": "active",
    "currentWorkingDesign": {},
    "latestTurnId": "…",
    "latestReadyPlanTurnId": "…",
    "revision": 3,
    "createdAt": "…",
    "updatedAt": "…"
  },
  "stale": false,
  "currentRoomDraftRevision": 17
}
```

Foreign and missing sessions are indistinguishable 404s. A stale session remains readable.

### `GET /spatial/design-sessions/{id}/turns?beforeSequence=&limit=`

Returns newest-first immutable turns. `limit` defaults to 20 and is capped at 50. `beforeSequence` is exclusive. This avoids an unbounded embedded history and supports reload/audit. GET never invokes Python or changes session/RoomDraft state.

### Public turn result

```json
{
  "id": "…",
  "sessionId": "…",
  "sequence": 2,
  "previousTurnId": "failed-turn-or-prior-turn",
  "parentPlanTurnId": "last-successful-plan-turn",
  "status": "proposed",
  "instruction": "Actually make it beige.",
  "basedOnRoomDraftRevision": 17,
  "intent": "material_appearance",
  "changePlan": {
    "summary": ["Change the sofa upholstery to beige."],
    "turnDelta": {
      "geometry": {"mode": "preserve"},
      "material": {"mode": "replace", "spec": {"baseColor": "beige", "materialFamily": "fabric", "roughness": "matte", "metallic": false}},
      "spatial": {"mode": "preserve"}
    },
    "workingDesign": {
      "geometry": {"category": "sofa", "shapeDescription": "Curved three-seat sofa …", "preserveCanonicalDimensions": true},
      "material": {"baseColor": "beige", "materialFamily": "fabric", "roughness": "matte", "metallic": false},
      "resolvedSpatialOperations": []
    }
  },
  "fitAnalysis": {"status": "clear", "observations": [], "warnings": [], "blockers": []},
  "execution": {
    "turnRequiresAssetGeneration": false,
    "hunyuanRequired": true,
    "requiresConfirmation": true,
    "executable": true,
    "runtimeNotice": "3D generation currently runs in a limited test GPU environment. Very complex generations may not complete within the available execution window."
  },
  "review": {"assumptions": [], "notes": [], "confidence": 0.91},
  "planFingerprint": "sha256-hex",
  "createdAt": "…",
  "completedAt": "…"
}
```

Provider/model/prompt text, chain of thought, internal context, raw output, claim token, provider timestamps and safe failure details stay out of public DTOs.

## Internal Python contract

Use `POST /internal/v1/spatial/element-proposals/reason`, protected by the existing constant-time bearer-token dependency. The request contains:

- `turnId`, `roomDraftId`, `roomDraftRevision`, and one `selectedElement`;
- selected object/fixture category, canonical transform, optional dimensions, fixture attachment state, and only `visualAssetBound: bool`;
- compact walls, openings, nearby objects/fixtures/constraints selected by Go;
- `currentWorkingDesign`, last successful plan summary, and the new instruction;
- closed `allowedSpatialOperations`, material-family and roughness enums;
- preservation defaults, all true.

Python returns a strict `ProposedSceneEditDelta`:

```python
class ProposedSceneEditDelta(StrictModel):
    schemaVersion: Literal[1]
    target: SelectedTargetRef
    intent: Literal["visual_geometry", "material_appearance", "spatial_domain", "mixed"]
    summary: Annotated[list[ShortText], Field(min_length=1, max_length=8)]
    geometry: GeometryChange
    material: MaterialChange
    spatial: SpatialChange
    blockers: Annotated[list[ProposedBlocker], Field(max_length=8)]
    assumptions: Annotated[list[ShortText], Field(max_length=8)]
    reviewNotes: Annotated[list[ShortText], Field(max_length=8)]
    confidence: Annotated[float, Field(ge=0, le=1)]
```

Each section has `mode = preserve|replace|clear`; only `replace` carries a spec. This makes “keep it,” “instead,” and “return to the original shape/material/position” deterministic. A material-only turn has geometry/spatial `preserve`, contains no `AssetGenerationSpec`, and uses intent `material_appearance`.

Spatial proposals are a closed discriminated union. RP4E1 supports:

- `move_relative_to_nearest_wall` with relationship `away_from|toward` and `distanceMeters` in `(0, 2]`;
- `resize_axis` with `axis=x|y|z` and exactly one of `deltaMeters` or `targetMeters`, resolved only when authoritative dimensions exist.

Go advertises the corresponding canonical operation per target: object uses `move_object`/`resize_object`; fixture uses `move_fixture`/`resize_fixture`. Requests outside this set produce a validated blocker, not fabricated coordinates or arbitrary operation JSON.

All Pydantic models use `ConfigDict(extra="forbid", strict=True, allow_inf_nan=False)`. Parse exactly one JSON object. Do not strip Markdown fences, extract substrings, coerce types, ignore extra fields, partially retain invalid items, or invoke a repair model.

## Go domain and persistence model

`SpatialDesignSession` is stored in `spatial_design_sessions`:

```go
type SpatialDesignSession struct {
    ID, CompanyID, ProjectID, SpaceID, RoomDraftID, CreatedByUserID string
    ClientSessionID, SessionRequestFingerprint string
    Target SpatialDesignTarget
    BasedOnRoomDraftRevision int64
    Status SpatialDesignSessionStatus // active in RP4E1
    CurrentWorkingDesign WorkingDesign
    LatestTurnID, LatestReadyPlanTurnID, ActiveTurnID string
    LastTurnSequence int64
    Revision int64
    CreatedAt, UpdatedAt time.Time
    SchemaVersion int
}
```

`SpatialDesignTurn` is stored separately in `spatial_design_turns`:

```go
type SpatialDesignTurn struct {
    ID, CompanyID, SessionID, ClientRequestID, RequestFingerprint string
    Sequence int64
    PreviousTurnID, ParentPlanTurnID string
    BaseSessionRevision, BasedOnRoomDraftRevision int64
    Instruction string
    Status SpatialDesignTurnStatus
    ProviderStartedAt *time.Time
    CallTokenHash string
    ProposedDelta *ProposedSceneEditDelta
    ValidatedPlan *ValidatedSceneEditPlan
    SafeFailureCode string
    StartedAt, CreatedAt time.Time
    CompletedAt *time.Time
    SchemaVersion int
}
```

Chronological `PreviousTurnID` includes failed turns. `ParentPlanTurnID` points only to the last successfully validated cumulative plan. A failed turn advances history but never changes `CurrentWorkingDesign` or `LatestReadyPlanTurnID`.

Indexes:

- unique `(companyId, clientSessionId)`;
- browse `(companyId, roomDraftId, updatedAt desc, _id)`;
- unique `(companyId, sessionId, clientRequestId)`;
- unique `(companyId, sessionId, sequence)`;
- browse `(companyId, sessionId, sequence desc)`;
- recovery `(status, providerStartedAt)`.

No TTL index is allowed: plan lineage is durable.

The server-computed turn request fingerprint covers schema version, session ID, target, pinned RoomDraft revision, parent-plan turn ID, and normalized instruction. The plan fingerprint covers a typed, map-free structure containing fingerprint schema version, target, pinned revision, cumulative geometry/material state, resolved absolute canonical operations, typed fit observations/warnings/blockers with measurements, safety classifications, and execution booleans. It excludes provider/model metadata, confidence, prose-only review notes, timestamps, IDs, and runtime-notice copy. Store the exact immutable public plan alongside the fingerprint so confirmation later always reloads what the user reviewed.

## Context minimization and deterministic geometry

Go may use the full tenant-owned RoomDraft internally for validation, but sends only a bounded projection to Python:

- selected target;
- nearest 8 walls by center-to-segment distance, stable tie-break by wall ID;
- openings on those walls plus openings within the neighborhood radius, capped at 8;
- objects, fixtures and constraints whose centers are within `max(3m, 2 × selected XZ footprint diagonal)`, sorted by distance then kind then ID, capped at 16 total;
- direct parent-wall attachment and relevant opening/constraint IDs;
- no service points unless a supported operation needs them (none in RP4E1).

Fit helpers work in the room-local XZ plane:

1. Normalize/check finite position, quaternion and positive dimensions; compute the selected target’s oriented bounding rectangle by rotating its four local footprint corners.
2. Reconstruct a room polygon only when wall endpoints form one closed, non-self-intersecting loop within the existing 2 cm coincidence tolerance. If that cannot be proven, spatial movement/resize is blocked with `room_boundary_unresolvable`; geometry/material planning may still proceed.
3. Resolve nearest wall by point-to-segment distance and stable wall-ID tie-break. Derive the interior normal from the polygon winding. Apply the semantic distance to the current working absolute transform, falling back to the authoritative transform on the first spatial turn.
4. Build the candidate footprint with unchanged canonical rotation/dimensions unless an explicit supported resize is present.
5. Block if any corner/edge lies outside/intersects the room polygon, a wall band with known thickness, an opening obstruction zone, or a constraint footprint.
6. Compute exact target-to-neighbor footprint separation before and after. A reduced positive clearance is a warning containing both server-derived metres; overlap is blocked. Do not invent a global “safe circulation” threshold until the product specifies one.
7. Unknown wall thickness is an observation/warning rather than a fabricated Web fallback. Missing selected dimensions is a blocker for spatial changes. Web defaults in `sceneProjection.ts` are rendering-only and never enter Go.

All resolved operations store absolute payloads matching existing Go types. No `EditOperation.Apply`, `SubmitEditOperation`, or RoomDraft repository update is called in RP4E1.

## Failure and blocker taxonomy

Use stable RFC 9457 `ErrorModel.Type` codes:

| HTTP | Code | Meaning |
|---|---|---|
| 400/422 | `design_reasoning_invalid_request` | malformed/bounded public input; zero provider calls |
| 404 | `design_session_not_found`, `design_plan_target_not_found` | missing and foreign resources deliberately indistinguishable |
| 409 | `design_plan_stale` | RoomDraft revision differs; session remains readable |
| 409 | `design_session_request_conflict`, `design_turn_request_conflict` | reused idempotency key with changed request |
| 409 | `design_turn_in_progress` | another distinct active turn owns the session |
| 503 | `design_reasoning_not_configured` | Go/Python spatial reasoning unavailable by config |
| 503 | `design_reasoning_provider_unavailable` | provider/transport unavailable |
| 503 | `design_reasoning_provider_rejected` | rate limit/auth/provider rejection |
| 504 | `design_reasoning_timeout` | the single attempt timed out |
| 502 | `design_reasoning_invalid_output` | malformed/extra/disallowed model output or invalid service response |
| 502 | `design_plan_target_mismatch` | model returned a different target |

`design_plan_unsupported_operation` and `design_plan_spatially_blocked` are plan blocker codes returned in a 200 terminal `blocked` turn, not transport failures. Provider failure never yields a fake plan.

## File map

### Create

- `ai-service/app/schemas/spatial_reasoning.py` — strict bounded internal request and proposed-delta models.
- `ai-service/app/prompts/spatial_reasoning.py` — system rules and five fixed few-shots.
- `ai-service/app/services/spatial_reasoning.py` — one-call orchestration and response cross-checks.
- `ai-service/app/providers/glm.py` — direct non-retrying GLM-5.3 JSON-mode adapter.
- `ai-service/app/providers/spatial_mock.py` — deterministic local/offline provider.
- `ai-service/tests/test_spatial_schemas.py`
- `ai-service/tests/test_spatial_prompts.py`
- `ai-service/tests/test_spatial_reasoning_service.py`
- `ai-service/tests/test_glm_provider.py`
- `ai-service/tests/test_spatial_routes.py`
- `ai-service/tests/test_spatial_config.py`
- `backend/internal/platform/ai/spatial_types.go`
- `backend/internal/platform/ai/spatial_client.go`
- `backend/internal/platform/ai/spatial_client_test.go`
- `backend/internal/platform/composition/spatialreasoningadapter.go`
- `backend/internal/platform/composition/spatialreasoningadapter_test.go`
- `backend/internal/spatial/designsession.go`
- `backend/internal/spatial/designplan.go`
- `backend/internal/spatial/designcontext.go`
- `backend/internal/spatial/designgeometry.go`
- `backend/internal/spatial/designfingerprint.go`
- `backend/internal/spatial/design_repository_mongo.go`
- `backend/internal/spatial/design_service.go`
- `backend/internal/spatial/design_handler.go`
- matching focused `*_test.go` files for every Go file above
- `backend/internal/tenanttest/spatial_design_reasoning_test.go`

### Modify

- `ai-service/app/config.py` — isolated spatial provider/GLM settings.
- `ai-service/app/providers/base.py` — add separate `SpatialReasoningProvider` protocol only.
- `ai-service/app/main.py` — build/register protected spatial route and sanitized validation handling.
- `ai-service/pyproject.toml` — promote HTTPX to runtime dependency; add offline GLM marker if a live smoke test is later added.
- `ai-service/tests/test_secret_redaction.py`, `test_auth.py`, `test_provider_errors.py` — spatial cases.
- `backend/internal/spatial/repository.go` — session/turn repository interfaces and atomic reserve/finalize capabilities.
- `backend/internal/spatial/service.go` — optional design repositories/reasoner/config setters.
- `backend/internal/spatial/handler.go` — call design-route registration and extend stable error mapping.
- `backend/internal/platform/config/config.go` and tests — spatial timeout, context/runtime notice settings.
- `backend/internal/platform/composition/services.go` — repositories, indexes, recovery, client adapter, spatial wiring.
- `backend/internal/platform/composition/schema_registration.go`, `backend/internal/tenanttest/router.go`, and composition drift tests — preserve route parity.
- `backend/cmd/openapi` route inventory tests as required by generated schema.
- root `.env.example` and `README.md` — local Python/GLM config and run instructions; no secret values.
- `apps/web/openapi/openapi.json` and `apps/web/src/lib/api/generated/schema.ts` — generated, never hand-edited.
- `apps/web/src/features/spatial/api.ts` and `api.test.ts` — four typed wrappers/contracts only.

### Do not modify

- `backend/internal/spatial/editoperation.go`, RP4D visual asset code, RP4E0 job/provider code;
- RoomDraft schema/repository documents;
- `backend/internal/ai`, `internal/aiintegration`, Gemini Copilot behavior;
- `SpatialEditor`, `ElementInspector`, scene projection, React Three Fiber, asset loaders, editor mutations/query cache;
- iOS/Android code.

---

### Task 1: Freeze the RP4E1 amendment and contract fixtures

**Files:**
- Create: `docs/superpowers/specs/2026-09-09-rp4e1-conversational-design-reasoning.md`
- Create: `ai-service/tests/fixtures/spatial_reasoning/*.json`
- Create: `backend/internal/spatial/testdata/designreasoning/*.json`

**Interfaces:**
- Produces the exact JSON request/proposed-delta/validated-plan fixtures consumed independently by Python and Go tests.

- [x] **Step 1:** Write the design amendment containing the constraints, four public routes, internal route, object/fixture target restriction, delta/working-design split, stale-session policy, at-most-once wording, and cumulative `hunyuanRequired` decision above.
- [x] **Step 2:** Add five shared semantic cases: material-only, geometry-only, spatial-only, mixed, unsupported structural request. Include a material-only refinement whose cumulative geometry remains pending.
- [x] **Step 3:** Add malformed fixtures for extra fields, wrong target, NaN/Infinity strings/numbers, direct coordinates, unsupported operation, missing geometry spec, and material-only delta carrying a geometry replacement.
- [x] **Step 4:** Review the amendment against `phase1.md`’s “AI suggests, system calculates, contractor approves” rule and M8.5C’s Go-owned safety classification.

Expected result: both language implementations work from one frozen wire contract without sharing provider types or authoritative database structs.

### Task 2: Add strict Python spatial schemas and prompts

**Files:**
- Create/modify the Python schema, prompt and tests listed in the file map.

**Interfaces:**
- Produces `SpatialReasoningRequest`, `ProposedSceneEditDelta`, `SpatialReasoningResult`, and `build_spatial_reasoning_messages(request)`.

- [x] **Step 1:** Write schema tests proving every model forbids extras/coercion/nonfinite values, enforces bounds/enums, requires section-mode/spec consistency, and supports exactly one selected target.
- [x] **Step 2:** Run `python -m pytest -q tests/test_spatial_schemas.py`; expect failures because the models do not exist.
- [x] **Step 3:** Implement a common strict model and the complete discriminated request/result types. Use field validators for trimmed non-empty text and model validators for mode/spec and intent consistency.
- [x] **Step 4:** Write prompt tests asserting all mandatory rules and five examples are present, the serialized context is bounded, and no credentials/provider/storage fields can enter the prompt.
- [x] **Step 5:** Implement the prompt so it requests exactly one JSON object and tells the model to preserve every unchanged working-design section, use only supplied IDs/operations, avoid coordinates, and return blockers for unsupported work.
- [x] **Step 6:** Run the two focused test files and `python -m ruff check app tests`.

### Task 3: Add the isolated GLM-5.3 provider and Python route

**Files:**
- Create/modify the Python provider, service, config, main and tests listed above.

**Interfaces:**
- `SpatialReasoningProvider.reason_element(request) -> SpatialReasoningResult`
- `POST /internal/v1/spatial/element-proposals/reason`

- [x] **Step 1:** Write provider tests using `httpx.MockTransport`. For success, 429, 5xx, timeout, redirect, truncated/missing content, invalid JSON and invalid schema, assert outbound request count is exactly one. Assert invalid local input makes zero calls.
- [x] **Step 2:** Run `python -m pytest -q tests/test_glm_provider.py`; expect import/config failures.
- [x] **Step 3:** Add `SPATIAL_AI_PROVIDER=mock|glm`, `GLM_API_KEY`, `GLM_MODEL=glm-5.3`, `GLM_BASE_URL=https://api.z.ai/api/paas/v4`, positive timeout/token bounds, and `GLM_REASONING_EFFORT=low|high|max`. Keep `AI_PROVIDER` and Gemini retry settings untouched.
- [x] **Step 4:** Implement one non-streaming HTTP POST to `/chat/completions` with redirects/retries disabled, JSON-object response mode, no tools, bounded output, and the configured reasoning effort. Validate assistant content exactly once; discard `reasoning_content`.
- [x] **Step 5:** Write service tests proving target/context references are cross-checked after Pydantic parsing, invalid output causes no repair/fallback, and provider metadata is locally trusted rather than model-authored.
- [x] **Step 6:** Implement `SpatialReasoningService` and deterministic mock provider.
- [x] **Step 7:** Write route tests for bearer auth, fixed sanitized errors, route-scoped request validation mapped to `INVALID_AI_REQUEST`, and coexistence with all three Copilot routes.
- [x] **Step 8:** Register the route/factory. A missing/invalid optional GLM configuration must make only the spatial route unavailable, not take down Copilot or silently fall back.
- [x] **Step 9:** Run:

```powershell
python -m pytest -q -m "not gemini and not glm"
python -m ruff check app tests
```

### Task 4: Add a strict bounded Go→Python spatial client

**Files:**
- Create `backend/internal/platform/ai/spatial_types.go`, `spatial_client.go`, `spatial_client_test.go`.

**Interfaces:**
- `NewSpatialClient(baseURL, token string, timeout time.Duration) *SpatialClient`
- `ReasonElement(context.Context, SpatialReasoningRequest) (SpatialReasoningResponse, error)`

- [x] **Step 1:** Write HTTP tests for exact path/auth/body; one request only; no redirects; context/client timeout; bounded success/error bodies; exact one-value JSON; unknown-field rejection; safe stable-code mapping.
- [x] **Step 2:** Run `go test ./internal/platform/ai -run Spatial -count=1`; expect missing symbols.
- [x] **Step 3:** Implement a separate client using `io.LimitReader(max+1)`, `json.Decoder.DisallowUnknownFields()`, an EOF check, and `CheckRedirect: http.ErrUseLastResponse`. Do not modify existing Copilot `Client.post`.
- [x] **Step 4:** Map Python stable codes to existing platform sentinels where equivalent. Preserve the distinction between provider-invalid output and malformed/unrecognized service responses.
- [x] **Step 5:** Run the focused tests and the existing `./internal/platform/ai` package suite.

### Task 5: Build Go plan types, cumulative merge, context and fit validation

**Files:**
- Create `designsession.go`, `designplan.go`, `designcontext.go`, `designgeometry.go`, `designfingerprint.go` and tests.

**Interfaces:**
- `resolveDesignTarget(draft, target) (AuthorizedDesignTarget, error)`
- `buildDesignReasoningContext(draft, session, instruction) (DesignReasoningContext, error)`
- `mergeWorkingDesign(previous, delta) (WorkingDesign, error)`
- `resolveSpatialChanges(draft, previousWorking, delta) ([]ResolvedSpatialOperation, FitAnalysis, error)`
- `computeDesignPlanFingerprint(ValidatedSceneEditPlan) (string, error)`

- [x] **Step 1:** Write table tests for target resolution, including object sofa, fixture, wrong kind/ID, cross-kind collision, missing/invalid dimensions and nonfinite transforms.
- [x] **Step 2:** Write context tests with more than each cap, shuffled input order and unrelated elements. Assert deterministic nearest selection and absence of full-room/provider/storage data.
- [x] **Step 3:** Write merge tests for preserve/replace/clear in every section, failed-turn parent behavior, and material-only refinement retaining geometry.
- [x] **Step 4:** Write geometry tests for rotated footprints, closed/reversed/shuffled wall loops, self-intersection, nearest-wall ties, inward normal, room containment, known wall thickness, openings, constraints, neighbor separation, missing data, and clear/warning/blocked results.
- [x] **Step 5:** Implement pure helpers. Reuse the 2 cm endpoint tolerance value; do not reuse Web display defaults.
- [x] **Step 6:** Write validation tests for intent/section consistency, target/category match, preservation defaults, operation allowlist, finite/sane distances, blocker handling, and safety classification.
- [x] **Step 7:** Implement authoritative validation and canonical absolute `MoveObjectOperation`/`MoveFixtureOperation`/`ResizeObjectOperation`/`ResizeFixtureOperation` payload production without applying them.
- [x] **Step 8:** Write fingerprint golden tests: identical normalized plan across map/order variation is equal; target/revision/geometry/material/operation/fit/execution change alters hash; copy/confidence/runtime notice/timestamp change does not.
- [x] **Step 9:** Implement fingerprinting with a versioned typed struct and deterministic JSON encoding.
- [x] **Step 10:** Run `go test ./internal/spatial -run 'Design(Target|Context|Working|Geometry|Plan|Fingerprint)' -count=1`.

### Task 6: Persist sessions and immutable turn lineage

**Files:**
- Modify `repository.go`.
- Create `design_repository_mongo.go` and tests.

**Interfaces:**
- `CreateOrGetSession`, `FindSession`, `FindTurn`, `FindTurnByClientRequestID`, `ListTurns`
- atomic `ReserveTurn`, `MarkProviderStarted`, `FinishTurn`, `RecoverInterruptedTurns`

- [x] **Step 1:** Write real Mongo replica-set tests for every named index and tenant-scoped not-found behavior.
- [x] **Step 2:** Write transaction tests proving session creation replay/conflict, turn reservation replay/conflict, one winner for concurrent distinct turns, and atomic session pointer + child turn insertion.
- [x] **Step 3:** Write state-fencing tests: only the current call token can mark started/finalize; duplicate completion adopts the stored terminal result; failed/stale turns do not update working design; successful completion clears only the matching active turn and supersedes the prior proposed turn.
- [x] **Step 4:** Write recovery tests proving an old `reasoning` turn becomes `needs_attention`, active turn is cleared atomically, and it can never be dispatched again.
- [x] **Step 5:** Implement BSON document converters, indexes and transaction callbacks. All idempotency existence checks occur inside retryable transaction callbacks; no network call or time-dependent provider action occurs inside them.
- [x] **Step 6:** Run `go test ./internal/spatial -run 'MongoSpatialDesign|DesignRepository' -count=1`.

### Task 7: Orchestrate one durable reasoning turn in the spatial service

**Files:**
- Create `design_service.go` and tests.
- Modify `service.go`.

**Interfaces:**
- `CreateDesignSession(ctx, companyID, userID string, input CreateDesignSessionInput) (DesignSessionResult, error)`
- `CreateDesignTurn(ctx, companyID, userID, sessionID string, input CreateDesignTurnInput) (SpatialDesignTurn, bool, error)`
- `GetDesignSession`, `ListDesignTurns`
- consumer-owned `ElementReasoner.ReasonElement(ctx, DesignReasoningContext) (ProposedSceneEditDelta, error)`

- [x] **Step 1:** Write service tests with counting fakes. Prove invalid/foreign/stale/replayed/concurrent requests call the reasoner zero times; a normal admitted turn calls it once; provider/model invalid output never triggers a second call.
- [x] **Step 2:** Write success tests for the main demo, material-only refinement, spatial-only resolution and mixed plan. Assert cumulative working design, execution flags, notice, fingerprint and lineage.
- [x] **Step 3:** Write failure tests for not configured, unavailable, rejected, timeout, malformed output, target mismatch and process-ambiguous `needs_attention`. Failed turns remain durable and retain the last successful plan parent.
- [x] **Step 4:** Implement service ordering:

```text
replay lookup by request ID
→ tenant session/draft/capture/target/revision validation
→ compact context build
→ atomic reserve
→ immediate draft revision recheck
→ durable providerStartedAt marker
→ exactly one reasoner call outside transaction
→ Python result validation
→ draft revision recheck
→ cumulative merge + spatial resolution + fit + fingerprint
→ atomic terminal turn/session update
```

- [x] **Step 5:** On cancellation/timeout after provider start, use a short bounded context detached from the canceled HTTP request only to persist `failed` or `needs_attention`; never use it to make another inference.
- [x] **Step 6:** Verify no success/failure path invokes `SubmitEditOperation`, RoomDraft repository `Update`, asset generation, asset publication, or visual-asset binding.
- [x] **Step 7:** Run all design service tests and existing spatial unit tests.

### Task 8: Wire config, composition, routes and recovery

**Files:**
- Create composition adapter/tests and design handler/tests.
- Modify config, services, schema registration, tenant test router, root environment example and README.

**Interfaces:**
- Go public routes and stable error codes defined above.
- `SetDesignPlanningSupport(repositories, reasoner)`
- Renovex-owned runtime notice config.

- [x] **Step 1:** Write config tests for:

```text
AI_SPATIAL_SERVICE_TIMEOUT (positive duration; default must exceed Python GLM timeout plus response-validation margin)
SPATIAL_DESIGN_CONTEXT_RADIUS_METERS (positive; default 3)
ASSET_GENERATION_RUNTIME_NOTICE_ENABLED (default true)
ASSET_GENERATION_RUNTIME_NOTICE_TEXT (fixed default copy, bounded if overridden)
```

- [x] **Step 2:** Implement config without reading `GLM_API_KEY` in Go. Python alone owns the provider key/model/base URL.
- [x] **Step 3:** Implement the composition adapter translating spatial-owned primitive context to platform transport DTOs and mapping every platform error to the spatial taxonomy.
- [x] **Step 4:** Construct both Mongo repositories, ensure indexes under the existing startup index context, and wire persistence even when live AI is unconfigured. Build `SpatialClient` from the existing service URL/token and dedicated timeout. Run bounded interrupted-turn recovery at startup; never redispatch.
- [x] **Step 5:** Write handler/OpenAPI tests for exact request/response allowlists, auth binding, 201 create, 200 turn/replay/read, list pagination, 404 isolation, stable 409/422/502/503/504 codes, and blocked plan as 200.
- [x] **Step 6:** Register design handlers through `spatial.RegisterHandlers`; update main/schema/tenant-test route-parity assertions without creating a separate auth surface.
- [x] **Step 7:** Update local-run docs. Keep mock spatial reasoning as default and document live GLM variables without values. Do not add secrets to Compose or browser env.
- [x] **Step 8:** Run:

```powershell
go test ./internal/platform/config ./internal/platform/ai ./internal/platform/composition ./internal/spatial -count=1
go test ./cmd/openapi -count=1
./scripts/verify-format.ps1
```

### Task 9: Add the minimal generated Web API/client surface

**Files:**
- Regenerate OpenAPI JSON/schema.
- Modify `apps/web/src/features/spatial/api.ts` and `api.test.ts`.

**Interfaces:**
- `createSpatialDesignSession(body)`
- `createSpatialDesignTurn(sessionId, body)`
- `getSpatialDesignSession(sessionId)`
- `listSpatialDesignTurns(sessionId, params)`

- [x] **Step 1:** Add failing MSW contract tests proving exact URL/body, stable errors, one selected object/fixture, immutable client request IDs, and no provider/company/geometry snapshot fields.
- [x] **Step 2:** Generate backend OpenAPI, copy it to `apps/web/openapi/openapi.json`, then run `npm run openapi:generate`. Do not hand-edit generated types or remove newly surfaced RP4E0 declarations.
- [x] **Step 3:** Add aliases/wrappers using `unwrapOrThrow`. Do not add React mutation hooks unless a UI slice actually consumes them; the wrappers are enough for RP4E1’s client contract.
- [x] **Step 4:** Assert GET never emits POST, session calls never update/invalidate the RoomDraft query cache, and 401 replay uses the same body/request ID. Durable backend idempotency remains the authoritative one-call guard.
- [x] **Step 5:** Run:

```powershell
npm run test -- src/features/spatial/api.test.ts
npm run typecheck
npm run lint
npm run build
```

### Task 10: Prove end-to-end invariants and regression safety

**Files:**
- Create `backend/internal/tenanttest/spatial_design_reasoning_test.go`.
- Extend Python/Go secret-redaction and route inventory tests.

- [x] **Step 1:** Through the real composed router with real JWT/company isolation and a counting fake reasoner, create a session and run the main mixed sofa turn. Assert one reasoner call and a confirmation-ready immutable plan.
- [x] **Step 2:** Submit “Actually make it beige.” Assert geometry is retained, turn-local generation is false, cumulative `hunyuanRequired` stays true, plan fingerprint changes, prior turn is superseded, and lineage survives service reconstruction.
- [x] **Step 3:** Cover spatial clear/warning/blocked, unsupported structural request, wrong model target, malformed model output, provider unavailable/timeout, and an interrupted call recovered as needs attention.
- [x] **Step 4:** Exercise identical concurrent request IDs and distinct concurrent request IDs. Assert at most one provider call for the admitted turn and deterministic replay/conflict.
- [x] **Step 5:** Before and after every success/failure scenario, compare the RoomDraft document/revision, RoomDraft edit-history count, visual-asset documents/bindings, and asset-generation-job count. All must be byte-for-byte/count unchanged.
- [x] **Step 6:** Prove cross-tenant session/turn/target access is an exact 404 and makes zero provider calls. Prove stale draft revision returns 409 before dispatch and that old plans remain readable but cannot accept new turns.
- [x] **Step 7:** Run the offline final gate after verifying Docker is available:

```powershell
# ai-service
python -m pytest -q -m "not gemini and not glm"
python -m ruff check app tests

# backend
go test ./... -count=1 -p 1 -timeout 60m
./scripts/verify-format.ps1

# web
npm run test
npm run typecheck
npm run lint
npm run build
```

## Migration and backfill

- Add two new schema-version-1 collections and startup-managed indexes. The repository currently has no established executable spatial migration-file pattern, so index creation belongs beside existing `EnsureIndexes` calls.
- No RoomDraft, RoomDraft edit, visual asset, asset-generation job, `ai_generation_batches`, or `ai_suggestions` document changes are required.
- No backfill is valid: existing records have no truthful DesignSession/turn lineage. Leave them untouched.
- A future execution slice may add optional `designSessionId`, `designTurnId`, and `planFingerprint` provenance to new generation jobs/edits using partial indexes. It must tolerate old records without those fields.
- If session schema evolves, add explicit read compatibility/migration then; do not use MongoDB flexibility as an implicit migration.

## Completion definition

RP4E1 is complete only when the real authenticated route can create a tenant-scoped object/fixture session, run/refine a durable turn through the existing FastAPI deployment and GLM-5.3 adapter, produce a strict Pydantic-valid proposal, pass Go target/domain/spatial validation, survive reload with exact lineage, and return a deterministic confirmation-ready cumulative plan bound to the unchanged RoomDraft revision—while tests prove no RoomDraft, visual asset, material, generation job, Three.js state, or downstream domain state changed.

No implementation task may claim provider “exactly once.” The proven guarantee is one or zero outbound attempts per durable turn, with ambiguous started turns made non-retryable and visible as `needs_attention`.

