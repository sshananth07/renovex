# RoomPlan Capture — Testing Strategy

**Status:** Approved, created by the RoomPlan Migration Amendment (2026-09-03).
**Design source:** `docs/superpowers/specs/M8.5C-Spatial-Intelligence-Design-Spec-Refined.md` §0, §8.5–§8.29
**Plan source:** `docs/superpowers/plans/M8.5C-Spatial-Intelligence-Implementation-Plan-Refined.md` RP1–RP6

---

## Purpose

RoomPlan capture spans two operating systems and a hardware capability
(LiDAR) the current implementation environment does not have. This document
defines three strict verification tiers so that "written," "compiled,"
"tested," and "physically verified" are never conflated in status reporting.

**Hard rule:** If a Windows-capable test exists, it MUST be run during the
implementation session that touches it — never deferred merely because the
surrounding feature also contains Apple-specific code. See Global
Implementation Constraint 30 in the implementation plan.

---

## Tier A — Windows / Platform-Independent

Runs during current implementation, on the user's Windows laptop, with no
Mac or iPhone required.

**In scope:**

- Go backend unit and integration tests (spatial domain, capture lifecycle,
  `SpatialCaptureRun`/`SpatialRoomVersion` persistence, quantity derivation).
- Web tests (spatial workspace, RoomDraft rendering, 2D/3D sync, edit
  operations UI).
- API / OpenAPI contract tests for spatial endpoints.
- `RoomDraft` JSON/schema validation tests (schema is platform-neutral — it
  is consumed by iOS, Web, and backend alike).
- Backend geometry validation (wall/opening/fixture/service-point rules,
  collision, clearance).
- Deterministic quantity derivation (floor area, perimeter, wall area,
  opening area, net wall area) from fixture `RoomDraft`/`SpatialRoomVersion`
  inputs.
- Edit-operation validation (the shared vocabulary in design spec §8.13):
  `move_corner`, `move_wall`, opening operations, door operations,
  object/fixture operations, service-point operations, constraint
  operations, `apply_verified_measurement`.
- Backend persistence/versioning (`SpatialCaptureRun`, `SpatialRoomVersion`
  immutability, current-room CAS, multi-run non-overwrite).
- Fixture validation: RoomPlan-shaped `CapturedRoom`/`CapturedRoomData`
  fixture JSON that exercises the adapter's expected input contract, even
  though the fixture itself is authored by hand (not captured on real
  hardware) until Tier C evidence exists.
- Server sync semantics: upload manifest finalize, checksum validation,
  resumable artifact-by-artifact upload, idempotent capture creation.
- Cross-platform spatial contract fixtures (design spec §8.8.1 / plan
  Task 30, extended): a fixture proving a wall/door/window/object/opening/
  measurement produces the same position/orientation when interpreted by
  the backend geometry layer and the Web renderer. This validates the
  *contract*, not the iOS adapter itself — the iOS side of that same
  contract is Tier B/C.

**Correction (2026-09-03, after RP1):** a Windows Swift 6.3.3 toolchain
(`x86_64-unknown-windows-msvc`) was subsequently installed in this
environment and is now available. This changes the boundary below: **pure
Swift code with no Apple-only framework import (no RoomPlan, ARKit,
SwiftUI, UIKit, or any framework absent from `FoundationNetworking`/
`Foundation`/the Swift standard library) IS Tier A** when it is genuinely
compiled and its tests genuinely run via `swift build`/`swift test` on
Windows — this was proven for `ios/RenovexCapture/Sources/RenovexCaptureCore`
and `Tests/RenovexCaptureCoreTests` (47/47 tests passing on Windows, see
`ROOMPLAN_HARDWARE_HANDOFF.md`'s Verification Ledger). The package's
`Package.swift` uses `#if os(macOS) || os(iOS)` guards so that any target
requiring an Apple-only framework (`RenovexCaptureRoomPlan`,
`RenovexCaptureApp`) is excluded from the Windows build graph entirely,
keeping the platform-neutral/Apple-dependent split enforced at the build
level, not just by convention.

**Explicitly NOT Tier A:** anything that requires an Apple-only framework
import (RoomPlan, ARKit, SwiftUI, UIKit, etc.), the Xcode toolchain
specifically (as opposed to any Swift toolchain), the iOS Simulator, or a
physical device — those remain Tier B/C. "The code lives in a Swift target"
is no longer, by itself, sufficient reason to classify something as Tier
B — check whether that specific target imports an Apple-only framework.

---

## Tier B — Mac/Xcode Automated

Requires a Mac and Xcode, but not necessarily a physical LiDAR-capable
iPhone. Runs in Simulator or via `xcodebuild test` where the code under test
does not require real ARKit/LiDAR hardware.

**In scope:**

- Swift compilation of the full iOS target (`RoomPlanCaptureProvider`,
  `RoomPlanCaptureAdapter`, `RoomDraft` Swift domain model, ViewModels).
- XCTest unit tests: adapter conversion logic (RoomPlan-shaped fixture input
  → expected canonical `RoomDraft` output), coordinate normalization,
  stable-ID assignment, live-update identity handling (`didAdd`/`didChange`/
  `didUpdate`/`didRemove` deduplication) using synthetic/fixture RoomPlan
  data rather than a live capture session.
- `RoomDraft` Swift domain tests (mirrors the platform-neutral schema tests
  from Tier A, but exercised through the actual Swift types).
- SwiftUI ViewModel tests (Room Review state machine, working-copy edit/
  apply/cancel/reset, selection state).
- SwiftUI fixture/snapshot tests where useful for regression protection.
- Local iOS persistence tests (`SpatialCaptureRepository`,
  `RoomDraftRepository`, `SpatialArtifactStore`) against a local/in-memory
  or Simulator-backed store.
- Simulator-supported tests: anything not gated by
  `RoomCaptureSession.isSupported` returning true on real hardware.
- Xcode project/scheme verification (the project builds, schemes resolve,
  dependencies are pinned).
- Physical-target build (compiles and links for a real-device target,
  without necessarily running RoomPlan itself).

**Status marking before Mac access exists:** Tests in this tier may be
WRITTEN (source committed) but MUST be marked `NOT YET XCODE-VERIFIED` in
any status report, task completion note, or handoff document until they have
actually been compiled and executed via Xcode/`xcodebuild`.

---

## Tier C — LiDAR iPhone Physical

Requires a real RoomPlan-supported device (`RoomCaptureSession.isSupported
== true`), i.e. an iPhone/iPad with LiDAR.

**In scope:**

- `RoomCaptureSession.isSupported` check itself.
- Real RoomPlan `start()`/`stop()`/finish lifecycle.
- Actual LiDAR tracking behavior and quality.
- Real room scan: rectangular, L-shaped, angled, multi-wall rooms.
- Live wall/surface updates during an active session.
- Revisiting a wall/surface without duplicate render geometry accumulation
  (the RP2 requirement — this is the RoomPlan-era equivalent of the ARCore
  duplicate-wall problem the frozen provider had to solve, and it can only
  be proven with real live callback sequences).
- Real door/window/opening detection, including RoomPlan's known failure
  modes (double doors, merged/split openings, missed swing).
- Real RoomPlan object detection against RoomPlan's supported vocabulary.
- Actual `CapturedRoom`/`CapturedRoomData` output shape and values (used to
  retroactively validate/extend the Tier A/B fixtures once available — see
  §32 below).
- AR ruler: real two-point tap-to-measure against physical geometry.
- Wall thickness measurement and verification against physical reality.
- Measure/Mark/Note live-capture-tools UX on a physical device.
- Real measurement accuracy comparison (RoomPlan estimate vs. AR-verified vs.
  physically-verified, per design spec §8.18).
- Tracking/relocalization behavior where relevant to persistent AR
  localization (design spec §10 already-approved architecture, extended to
  RoomPlan capture sessions).
- Physical persistence workflow: force-quit, relaunch, background/resume,
  multiple real scans, real confirm/edit/re-open cycles (see the Multi-Scan
  Persistence Test Requirements below).

**Hard rule:** No mock, Simulator, or synthetic-fixture test may be reported
as equivalent to a Tier C result. A Tier B pass proves the Swift code
compiles and the adapter logic is correct against a fixture; it does not
prove RoomPlan actually produces that fixture shape on real hardware, or
that live tracking behaves acceptably.

---

## Multi-Scan Persistence Test Requirements

These requirements are split across tiers per the design spec's §31 (in the
amendment prompt) and are cross-referenced here for a single checklist.

**Tier A/B (automated, repeatable in CI or locally):**

- create scan → persist → repository/app state recreated → scan remains.
- edit `RoomDraft` → persist → reopen → edits remain.
- create multiple scans → all remain.
- new scan does not overwrite previous scan.
- confirm Scan #2 → Scan #1 remains accessible.
- edit confirmed `RoomVersion` → original unchanged → working draft/new
  version created.
- sync fails → local scan remains.
- discard one unconfirmed scan → unrelated runs remain.
- measurement evidence persists.
- Reset to Scan restores the normalized RoomPlan baseline.

**Tier C (hardware acceptance, one pass per case, recorded in the hardware
handoff ledger):**

Two distinct guarantees apply here — do not conflate them (see
`ROOMPLAN_HARDWARE_HANDOFF.md` RP-HW-007A/007B):

- **Completed-scan persistence (guaranteed):** real RoomPlan scan completes
  → navigate away → reopen → scan remains. Force quit *after* RoomPlan
  completion (session ended, `CapturedRoomData` produced) → relaunch → scan
  remains. This is guaranteed because Apple's `CapturedRoomData` is
  serializable/decodable specifically so it can be persisted and processed
  after the session ends, and RP3's required ordering (persist raw result →
  normalize `RoomDraft` → persist `RoomDraft` → THEN present Room Review)
  ensures this happens before the contractor ever sees Room Review.
- **Mid-scan crash recovery (best-effort, not a RoomPlan-session-resume
  guarantee):** force quit *during* active RoomPlan scanning, before session
  completion → relaunch. A killed RoomPlan session is never assumed to
  resume the same live AR tracking session. If RP3 implements periodic
  recovery snapshots from live `CapturedRoom` delegate updates, verify the
  app offers "Recover latest draft / Rescan" using the last snapshot; if no
  such recovery was implemented, verify the app cleanly starts a new scan
  rather than presenting a broken/partial state as if it were live.
- create Scan #2 → Scan #1 remains.
- edit → force quit (after the edit was applied to the persisted draft) →
  reopen → edit remains.
- confirm → force quit → reopen → confirmed room accessible.

---

## Status Vocabulary — never conflate these four

```text
WRITTEN
≠ WINDOWS VERIFIED
≠ XCODE VERIFIED
≠ LIDAR PHYSICALLY VERIFIED
```

- **WRITTEN** — source/tests exist in the repository. No execution claim.
- **WINDOWS VERIFIED** — a Tier A command was actually run on Windows in
  this session, with output evidence (command + pass/fail + counts).
- **XCODE VERIFIED** — a Tier B command was actually run via Xcode/
  `xcodebuild` on a Mac, with output evidence.
- **LIDAR PHYSICALLY VERIFIED** — a Tier C case was actually executed on a
  real supported device, with the hardware handoff ledger fields filled in.

Never write "all tests pass" without naming which environment ran them. See
the Verification Ledger in
`docs/spatial/roomplan/ROOMPLAN_HARDWARE_HANDOFF.md` for the append-only
record of every Tier A, B, and C run.

**Tier B and Tier C are not optional completion gates.** Hardware being
currently unavailable does not downgrade them to "nice to have" — it means
M8.5C sits at WRITTEN/WINDOWS VERIFIED and stays PENDING XCODE/HARDWARE
VERIFICATION until those tiers actually run. The golden acceptance scenario
requires a real device capture, so Tier C is mandatory for final milestone
completion, not merely for extra confidence.

---

## Current environment status

As of this amendment (2026-09-03), the implementation environment is
Windows-only: no Mac/Xcode, no LiDAR-capable iPhone. **Updated same day:** a
Windows Swift 6.3.3 toolchain (`x86_64-unknown-windows-msvc`) was installed
and is now available and functional — confirmed by genuinely compiling and
running `ios/RenovexCapture`'s `RenovexCaptureCore`/`RenovexCaptureCoreTests`
targets (47/47 tests passing at RP1; 92/92 at RP2; **116/116 at RP3** — see
`ROOMPLAN_HARDWARE_HANDOFF.md`'s Verification Ledger for the exact RP3
breakdown). This makes pure-Swift, Apple-framework-free code a genuine
Tier A target on Windows now, not merely WRITTEN — see the Tier A
correction note above. Every Tier A test available at the time of
implementation MUST still be run — do not defer Tier A work merely because
Tier B/C cannot be exercised yet. Tier B work (anything importing RoomPlan,
ARKit, SwiftUI, or requiring Xcode specifically) may be written but stays
`NOT YET XCODE-VERIFIED` — this remains true regardless of the Windows Swift
toolchain, since Apple-only frameworks have no Windows equivalent. Tier C
work is deferred entirely until hardware is available, per
`docs/spatial/roomplan/ROOMPLAN_HARDWARE_HANDOFF.md`.

**RP3 update (2026-09-04):** the "Multi-Scan Persistence Test Requirements"
Tier A/B checklist above is now substantially covered at the Tier A level —
`FileSpatialCaptureRepositoryTests`/`FileRoomDraftRepositoryTests`/
`CaptureCompletionCoordinatorTests` prove create/persist/reopen survival
(via repository-instance recreation, simulating an app relaunch), multiple
runs per Space remaining distinct, reopen never duplicating a draft, and
Scan Again creating a new run without overwriting the prior one — all on
Windows, no Mac/hardware dependency. RP3 also touched the Go backend
(additive `ArtifactKind`/`SpatialCapture` fields, new `RoomDraft`
persistence) and Web (`SpatialEntry.tsx`) — both are Tier A by definition
and fully verified this session (`go test`, `vitest`, `tsc --noEmit`, all
passing). Not yet covered even at Tier A/B: "edit confirmed RoomVersion ->
original unchanged -> working draft/new version created" (depends on RP6's
confirmation-geometry wiring, not yet implemented) and "Reset to Scan
restores the normalized RoomPlan baseline" (the `RoomDraft.OriginalBaseline`
field exists on both the Go and Swift sides as of RP3, but no Reset-to-Scan
operation has been implemented yet — that's RP4 scope per the plan's
completion-gate rule against pulling RP4+ forward).
