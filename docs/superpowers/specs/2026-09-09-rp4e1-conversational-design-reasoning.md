# RP4E1 — Conversational Spatial Design Reasoning: Design Amendment

**Status:** Approved. This document is the frozen design record for RP4E1,
amending `M8.5C-Spatial-Intelligence-Design-Spec-Refined.md` (whose §2, §4,
§8.6/§8.7 authority-model invariants continue to apply unchanged — see §0
below) and narrowing the general Project Copilot / whole-room Qwen concept
described in that spec's §§17-26 to a specific, smaller approved slice.

**Source plan:** `docs/superpowers/plans/2026-09-09-rp4e1-conversational-design-reasoning.md`
is the authoritative task-by-task execution plan. This document freezes the
product/architecture amendment the plan's Task 1 requires before
implementation begins.

---

## 0. Relationship to M8.5C

M8.5C names Qwen as the eventual V1 spatial design provider, working over a
confirmed `SpatialRoomVersion` and producing whole-room `ValidatedSpatialConcept`
revisions (§17-§26). RP4E1 is a narrower, earlier, approved amendment:

- provider is GLM-5.3, not Qwen;
- scope is exactly one selected mutable `RoomDraft` element (`object` or
  `fixture`), not a whole room;
- output is a confirmation-ready plan only — RP4E1 stops before any
  execution, asset generation, or RoomDraft mutation.

Every M8.5C authority-model invariant (§2: capture estimate → contractor
review → Go-authoritative confirmed truth; §2.4: renderers never
reinterpret AI instructions; safety/geometry validation is Go's job, never
the model's) applies unchanged to this narrower slice. RP4E1 does not
reopen or contradict M8.5C; it implements a smaller approved step toward it.

## 1. Global constraints (binding)

- One session targets exactly one existing RoomDraft element, restricted to
  kinds `object` and `fixture` — the only two kinds with RP4D visual-asset
  bindings and complete category/transform/dimensions fields. Sessions
  targeting `wall`, `opening`, `servicePoint`, or `constraint` are rejected
  at creation with a typed 422.
- At most one outbound GLM-5.3 inference attempt per newly admitted live
  turn. Zero provider calls for rejected, stale, replayed, in-progress, or
  mock turns. No retry, no fallback model, no repair call, no classifier or
  reviewer model.
- "Exactly once at the provider" is not achievable after a process or
  network failure. Renovex guarantees **at-most-once dispatch** per durable
  turn: `providerStartedAt` is recorded before the call, and that turn is
  never redispatched, regardless of outcome.
- Go is authoritative for: tenancy, RoomDraft revision pinning, target
  identity/category, context selection, allowed-operation resolution,
  spatial coordinates, fit/safety checks, cumulative working design,
  fingerprinting, persistence, and stale-plan handling.
- Python performs language interpretation and strict structural validation
  only. It never queries MongoDB, never receives credentials, URLs, binary
  assets, raw RoomPlan files, or another tenant's data.
- RP4E1 never calls Hunyuan, creates an asset-generation job, produces a
  reference image or GLB, publishes or binds a `VisualAssetVersion`, applies
  a material, calls a RoomDraft edit method, or mutates Three.js/UI scene
  state.
- Every terminal proposal (turn) is immutable. Refinement always creates a
  new turn; it never overwrites a prior one.
- A session is pinned to its creation-time RoomDraft revision
  (`basedOnRoomDraftRevision`). A RoomDraft revision change makes the
  session stale and read-only. RP4E1 requires starting a new session; it
  never silently rebases or replays relative spatial changes onto a new
  revision.
- All user/provider strings and arrays carry explicit byte/count limits.
  All spatial numbers must be finite (no NaN/Infinity). Unknown JSON fields
  fail closed at both the Python and Go transport boundaries.
- Public errors carry stable machine codes and fixed, contractor-safe text.
  Provider bodies, prompts, credentials, reasoning content, stack traces,
  and invalid raw model output are never returned or logged.
- The existing Copilot suggestion workflow (spaces/work-items/resources),
  the Gemini provider, its retry behavior, `ai_generation_batches`, and
  `ai_suggestions` remain unchanged. RP4E1 adds a parallel, separate
  reasoning surface; it does not widen or modify the existing one.

## 2. Turn-local vs. cumulative execution flags (approved amendment)

The originally sketched behavior — a material-only follow-up retaining
earlier proposed geometry while also reporting `hunyuanRequired=false` and
omitting the runtime notice — is internally contradictory: the retained
custom geometry has not actually been generated yet, so declaring no
generation is required would be false.

**Approved resolution:** the execution block splits turn-local and
cumulative concerns explicitly:

- `turnRequiresAssetGeneration` describes only what *this* turn's delta
  requires. A material-only turn ("Actually make it beige.") sets this to
  `false` — the turn itself proposed no new custom geometry.
- `execution.hunyuanRequired` describes the *cumulative* working design.
  If an earlier turn in the same session proposed custom geometry that has
  not yet been generated, `hunyuanRequired` remains `true` even on a later
  material-only turn, and the runtime notice continues to display.

This is the one approved deviation from the plan's originally sketched
wire example, and the plan's own file map already documents it as "the
safest amendment" to adopt with this document.

## 3. Reachable statuses (scope-limited)

The full conceptual lifecycle in M8.5C includes confirmed/completed/
cancelled states. RP4E1 implements only the states reachable within this
slice:

- Session status: `active` (the only status RP4E1 produces or reads).
- Turn status: `reserved | reasoning | proposed | blocked | failed |
  needs_attention | superseded | stale`.

Confirmation, completion, and cancellation states belong to a later
execution slice that owns those transitions; RP4E1 does not stub or
reserve dead code paths for them.

## 4. Public HTTP contract (frozen)

Four public routes are added under the existing authenticated Huma group,
all deriving `companyId`/`createdByUserId` from
`identity.PrincipalFromContext` — matching every existing spatial route's
convention:

- `POST /spatial/design-sessions` — create/idempotently replay an empty
  session; never calls Python.
- `POST /spatial/design-sessions/{id}/turns` — create one immutable
  reasoning turn, run synchronously after durable reservation.
- `GET /spatial/design-sessions/{id}` — session + bounded working design +
  stale flag; never calls Python.
- `GET /spatial/design-sessions/{id}/turns` — paginated newest-first
  immutable turn history; never calls Python.

One internal Python route is added:
`POST /internal/v1/spatial/element-proposals/reason`, behind the same
constant-time bearer-token dependency the existing three Copilot routes
use.

Exact request/response shapes, limits, replay/conflict semantics, and the
full error-code taxonomy are as specified in the implementation plan's
"Final public HTTP contract," "Internal Python contract," and "Failure and
blocker taxonomy" sections — this document does not restate them verbatim
to avoid drift between two normative copies; the plan and this amendment
are read together.

## 5. Object vs. fixture semantic distinction (confirmed)

A scanned sofa is a `RoomDraftObject` (free-form `category` string), not a
`RoomDraftFixture` (fixed AC/boiler/cabinetry/wall-fixture/electrical-panel
vocabulary — `backend/internal/spatial/roomdraft.go`). The sofa example in
the plan resolves to canonical operation `move_object`/`resize_object`;
`move_fixture`/`resize_fixture` apply only to the fixture kind. Go
advertises the correct canonical operation pair to Python based on the
resolved target's kind — Python never guesses or infers this.

## 6. Context minimization (confirmed)

Go sends Python only a bounded projection of the authoritative RoomDraft:
the selected target, nearest walls/openings/neighbors within documented
caps and radius, and the session's current cumulative working design. Full
RoomDraft geometry, storage keys, credentials, and other tenants' data
never reach Python. See the plan's "Context minimization and deterministic
geometry" section for the exact caps and selection algorithm — normative
here by reference, not restated.

## 7. Review checklist against phase1.md and M8.5C

- **phase1.md "AI suggests, the system calculates, the contractor
  approves"**: satisfied — Python only proposes a structured delta; Go
  performs all spatial/geometry/fit calculation and safety classification;
  nothing becomes authoritative RoomDraft state without a future,
  out-of-scope confirmation step that RP4E1 deliberately does not
  implement.
- **phase1.md AI Suggestion Data Model (pending/accepted/modified/
  rejected)**: RP4E1's turn lineage plays the equivalent durable-until-
  reviewed role for this narrower conversational-design domain; it does
  not reuse `ai_suggestions` because the object being proposed (a
  cumulative scene-edit plan) is a different shape, but the same principle
  — proposals never silently become authoritative — is preserved via
  immutable turns plus a required future confirmation step.
- **M8.5C §2 Authority Model**: preserved — GLM proposes, Go validates
  (schema/geometry/safety), and only a Go-derived `ValidatedSceneEditPlan`
  is returned; Python's raw output is never returned to the client, logged,
  or trusted as final.
- **M8.5C §2.4 Renderer boundary**: preserved — RP4E1 produces no
  `SceneManifest` and touches no renderer; that remains entirely
  out of scope.
- **M8.5C safety classification ownership**: preserved — RP4E1's fit/safety
  checks (room-boundary containment, collision, clearance) run in Go only;
  Python never classifies safety or asserts geometry is safe.

No conflict with `phase1.md` or M8.5C was found requiring escalation.
