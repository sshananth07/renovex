# RoomPlan Capture — Hardware Handoff Document

**Status:** Approved, created by the RoomPlan Migration Amendment (2026-09-03).
**Purpose:** Canonical pickup document for when a MacBook and a supported
(LiDAR-capable) iPhone become available. Read this first before doing any
Tier B or Tier C work — see `ROOMPLAN_TESTING_STRATEGY.md` for the tier
definitions this document assumes.

---

## A. Current status (as of 2026-09-04, RP4B implementation)

**RP4B note:** RP4B (backend EditOperation persistence, revisioning, and
conflict transport) is **backend-only, per explicit user scope** — it
touched zero iOS files (not even `RenovexCaptureCore`). Every iOS
verification note below is therefore identical to the RP3.5/RP4B0 entry
immediately preceding this one. RP4B explicitly does NOT implement the
interactive Web/iOS 2D/3D editor — see the RP4B ledger entry near the end
of this document for full detail, including a genuine MongoDB driver
transaction-retry bug found and fixed during implementation.

- Design spec amended: `docs/superpowers/specs/M8.5C-Spatial-Intelligence-Design-Spec-Refined.md` §0, §8.5–§8.29.
- Implementation plan amended: `docs/superpowers/plans/M8.5C-Spatial-Intelligence-Implementation-Plan-Refined.md`, RP1–RP6 inserted in place of the frozen Task 5.
- Kotlin/ARCore capture provider: FROZEN, preserved in place under
  `android/app/src/main/kotlin/com/renovex/capture/capture/`. Last known
  state: Task 5's `WallTraceBuilderComparisonTest` passing via Gradle
  (verified 2026-09-01, see project memory), physical Xiaomi 11T device
  verification previously performed on that provider.
- iOS RoomPlan implementation: **RP1 (2026-09-03) accepted; RP2 (2026-09-04)
  accepted; RP3 (2026-09-04) written.** `RenovexCaptureCore`
  (provider-neutral domain — `SpatialCaptureProvider`/
  `RoomPlanCaptureProvider`/`RoomPlanCaptureAdapter` boundary, canonical
  coordinate/stable-ID contracts, `LiveElementTracker` current-state dedup,
  `RoomLocalCoordinateNormalizer`, `RoomDraftNormalizer`,
  `CoincidentWallSanityCheck`, authenticated API client, and — new in RP3 —
  `RoomDraft`/stable-ID/provenance/transform types made `Codable`,
  `LocalCaptureRun`, `SpatialCaptureRepository`/`RoomDraftRepository`
  protocols + `FileSpatialCaptureRepository`/`FileRoomDraftRepository`
  JSON-file-backed implementations, and `CaptureCompletionCoordinator`
  enforcing the required persist-before-review ordering) is **WINDOWS
  VERIFIED**: 116/116 XCTest methods pass (92 from RP1/RP2 + 24 new RP3
  tests, zero regression) on the same Windows Swift 6.3.3 toolchain used for
  RP1/RP2. `RenovexCaptureRoomPlan` (unchanged this session) and
  `RenovexCaptureApp` (SwiftUI app shell — RP3 wired `ScanRoomView`'s
  completion path to real persistence instead of only an in-memory summary,
  and replaced `SpaceDetailView`'s static "AR Review"/"Concepts" stub rows
  with a real Scan History list) remain **NOT YET XCODE-VERIFIED** —
  structurally excluded from the Windows build graph as before. RP1/RP2
  were not reopened (no defect found); RP4 was not started. RP2-MAC-001
  through RP2-MAC-016 remain the pending Xcode compile-check items from
  RP2, unchanged by RP3 (listed in full in the Verification Ledger's RP2
  entry below and inline in
  `RoomPlanCaptureAdapter.swift`/`RoomPlanLiveSessionCoordinator.swift`).
  RP3's new `RenovexCaptureApp` changes (`ScanRoomView.swift`,
  `SpaceDetailView.swift`, `ProjectListView.swift`, `SpaceListView.swift`,
  `RenovexCaptureRootView.swift`, `RenovexCaptureApp.swift`) add no new
  Apple-framework API surface beyond what RP1/RP2 already used (SwiftUI,
  `FileManager`) — the risk here is unverified SwiftUI compilation/wiring
  correctness, not unverified Apple SDK shape assumptions.
- **RP3 also touched the Go backend and Web app**, both fully verified in
  this environment (Tier A, no Mac/hardware dependency): additive
  `ArtifactKind` values (`captured_room_data`, `roomplan_processed`,
  `roomdraft_json`, `usdz`) + `model/vnd.usdz+zip` content type;
  `SpatialCapture.Provider`/`CaptureNumber`/`RoomDraftID` fields; a new
  internal `spatial.RoomDraft` Go domain type + Mongo-backed repository
  (CAS-guarded, no new public REST endpoint — RoomDraft sync rides the
  `roomdraft_json` artifact through the existing upload pipeline, per
  explicit user direction during RP3); `SpatialEntry.tsx` extended with
  capture-number display and a "Continue Review" badge. See the RP3 ledger
  entry below for exact commands/counts.
- Mac/Xcode: **not available** in the current environment.
- LiDAR-capable iPhone: **not available** in the current environment.
- A Windows Swift 6.3.3 toolchain (`x86_64-unknown-windows-msvc`) remains
  available and has now been used across two implementation sessions (RP1
  correction, RP2) to genuinely compile and test `RenovexCaptureCore`/
  `RenovexCaptureCoreTests`. No Tier B or Tier C work has been performed —
  RoomPlan/SwiftUI code remains `NOT YET XCODE-VERIFIED` / `NOT YET LIDAR
  PHYSICALLY VERIFIED` until an actual Mac/Xcode/device run happens.

---

## B. Required Mac/Xcode prerequisites

- macOS version compatible with the Xcode version required for RoomPlan
  (RoomPlan requires iOS 16+; confirm the exact minimum Xcode/SDK pairing
  against Apple's current RoomPlan documentation at the time Mac access
  becomes available — do not assume a version here without checking, since
  SDK requirements change over Apple's release cycle).
- Xcode installed with the iOS SDK matching the deployment target chosen in
  RP1.
- Command Line Tools installed (`xcode-select --install`) so `xcodebuild`
  can run from a terminal for Tier B automated verification.
- Apple Developer account (free or paid, as required) for code signing to
  run on a physical device — required for Tier C, not strictly for
  Simulator-only Tier B runs.
- Network access from the Mac to the same backend the Windows environment
  uses (or a locally reachable instance), matching the debug `API_BASE_URL`
  pattern already used by the Android app.

---

## C. Required iPhone/iOS prerequisites

- A LiDAR-capable iPhone (iPhone 12 Pro or later Pro/Pro Max models, or an
  iPad Pro with LiDAR) — RoomPlan requires the LiDAR scanner;
  `RoomCaptureSession.isSupported` will be `false` on non-LiDAR devices, and
  no Tier C work can proceed on such a device.
- iOS version meeting RoomPlan's minimum (iOS 16+; verify exact minimum
  against the RoomPlan SDK version targeted in RP1 at implementation time).
- Device paired/trusted with the Mac's Xcode for direct-install debugging,
  or a suitable ad-hoc/TestFlight distribution if direct Xcode install is
  not convenient.
- Same LAN/network reachability to the backend as the Mac, matching the
  existing debug-build backend URL pattern used by Android.

---

## D. Exact automated Mac verification steps

Once Mac/Xcode access exists, run (exact scheme/target names to be filled in
during RP1 once the Xcode project exists — do not guess them here):

```text
xcodebuild build -project <RenovexCapture>.xcodeproj -scheme <SchemeName> -destination 'platform=iOS Simulator,name=<SimulatorName>'
xcodebuild test -project <RenovexCapture>.xcodeproj -scheme <SchemeName> -destination 'platform=iOS Simulator,name=<SimulatorName>'
```

or, if Swift Package Manager / a workspace is used instead:

```text
xcodebuild test -workspace <RenovexCapture>.xcworkspace -scheme <SchemeName> -destination 'platform=iOS Simulator,name=<SimulatorName>'
```

Record the actual commands used (they will differ from the placeholders
above once RP1 establishes the real project structure) in the Windows
Verification Ledger's Mac/Xcode entries — this document's job is to hold the
template; each real run's exact command belongs in the ledger below or in a
dedicated per-run log referenced from it.

Required passing suites before declaring "XCODE VERIFIED" for a given RP
task: the task's own XCTest target(s) as named in the implementation plan
(RP1–RP6), plus a full project build with zero warnings-as-errors violations
if that policy is adopted.

---

## E. Exact physical-device acceptance cases

Use explicit IDs. Record actual results in section G's table (or an
appendix table per case if detail exceeds this document's inline format).

- **RP-HW-001** — Simple rectangular room. Scan a plain rectangular room.
  Expect 4 walls, 4 corners, closed shell-equivalent `RoomDraft`, plausible
  dimensions.
- **RP-HW-002** — Revisiting existing wall. Scan a wall, move away, return
  and re-scan the same wall. Expect no duplicate render geometry (RP2
  requirement) and no duplicate `RoomDraft` wall entries.
- **RP-HW-003** — Doors/windows/openings. Room with at least one door and
  one window. Expect correct or correctable classification; verify the
  contractor-correction path (RP4) can fix any misclassification.
- **RP-HW-004** — Multiple RoomPlan objects. Room with several
  RoomPlan-recognized objects (e.g. table, chair, sofa, refrigerator).
  Expect objects appear in `RoomDraft`; verify contractor can add an
  unsupported object type manually (RP4/design spec §8.16).
- **RP-HW-005** — AR ruler. Use the two-point AR ruler (RP5) to measure a
  known-length physical reference; compare against the physical tape
  measurement.
- **RP-HW-006** — Wall thickness verification. Compare RoomPlan-estimated
  wall thickness against AR-verified and physically-verified measurement;
  confirm applying the verified value updates `RoomDraft`/2D/3D/quantities
  together (design spec §8.14).
- **RP-HW-007A** — Force-quit after completed scan. Complete a RoomPlan
  scan (RoomPlan has finished processing and produced `CapturedRoomData`),
  force-quit before or during Room Review, relaunch. Expect the completed
  scan/draft is fully recovered — this is the guaranteed case, since Apple
  provides `CapturedRoomData` as a serializable/decodable result specifically
  so it can be persisted and processed after the session ends (RP3, design
  spec §8.22/§8.25 amendment note).
- **RP-HW-007B** — Force-quit mid-scan (before RoomPlan completion).
  Force-quit while RoomPlan is actively scanning, before tapping
  Done/completing the session, relaunch. Expect the app does **not** claim to
  resume the same live RoomPlan tracking session — a killed RoomPlan session
  cannot be assumed to resume. Verify the actual behavior matches whatever
  RP3 implements: either "no recovery, start a new scan" or, if periodic
  recovery snapshots were implemented from live `CapturedRoom` delegate
  updates, "Recover latest draft / Rescan" is offered using the last
  snapshot taken before the crash. Do not report this case as equivalent to
  RP-HW-007A's guarantee.
- **RP-HW-008** — Multiple Scan Runs. Create two or more separate capture
  runs for the same Space; verify none overwrite each other and Scan History
  (design spec §8.24) lists all of them correctly. RP3 wrote
  `LocalCaptureRun`/`FileSpatialCaptureRepository`/`SpaceDetailView`'s Scan
  History list for exactly this case — WINDOWS VERIFIED at the repository
  level (`FileSpatialCaptureRepositoryTests
  .test_multipleRunsForOneSpace_allRemainDistinct`), NOT YET XCODE-VERIFIED
  for the actual on-device UI/RoomPlan integration.
- **RP-HW-009** — Resume editing. Leave Room Review mid-edit (unconfirmed),
  navigate away, return via Continue Review; verify the working
  edits/selection state resume correctly. RP3 wrote and Windows-verified
  the persistence half (`RoomDraftRepository.find(forCapture:)` reloading
  the identical draft/stable IDs — `FileRoomDraftRepositoryTests
  .test_repeatedFind_returnsIdenticalDraft_neverCreatesADuplicate`); the
  actual working-copy edit/selection state machine is RP4 scope, not yet
  implemented.
- **RP-HW-010** — Confirmed version remains revisitable. Confirm a
  `SpatialRoomVersion`, then attempt to view/re-open it later; verify it is
  immutable (edits create a new working draft/version, never mutate the
  confirmed one in place — design spec §8.24/§46).
- **RP-HW-011** *(reserved, add as needed)* — Double-door edge case per
  design spec §8.15's known RoomPlan limitation list.
- **RP-HW-012** *(reserved, add as needed)* — L-shaped or angled room
  footprint.

Add further RP-HW-0xx cases as they are discovered during Tier C testing;
number sequentially and never reuse a retired number.

**Per-case recording template** (copy for each executed case):

```text
Case ID:            RP-HW-0xx
Device model:
OS version:
RoomCaptureSession.isSupported:
Test date:
Room/environment:
Steps taken:
Expected result:
Actual result:
PASS/FAIL:
Screenshots/artifacts/logs:
```

---

## F. Currently pending items

- **RP1/RP2/RP3 (2026-09-03/04) are written and Windows-verified for their
  provider-neutral halves; RP4–RP6 have not started.** RP3 additionally
  landed and verified real backend/Web changes this session (see the RP3
  ledger entry) — those are not Mac/hardware-gated at all and are already
  complete, not merely "written." See
  `ROOMPLAN_TESTING_STRATEGY.md` for the precise tier split. The Windows
  Swift 6.3.3 toolchain (installed 2026-09-03) has now been used across
  three sessions — `RenovexCaptureCore`/`RenovexCaptureCoreTests` compile
  and pass (116/116, up from 92/92, up from 47/47) on it; this is genuine
  Tier A / WINDOWS VERIFIED coverage, not merely WRITTEN. `Package.swift`'s
  `#if os(macOS) || os(iOS)` split continues to exclude
  `RenovexCaptureRoomPlan`/`RenovexCaptureApp`/`RenovexCaptureAppTests` from
  the non-Apple build graph — RP2 added substantially more code there (a
  live `RoomCaptureSession` delegate coordinator, `RoomBuilder` integration,
  a `RoomCaptureView` SwiftUI wrapper) than RP1 did, and RP3 added further
  App-target wiring (persistence composition root, Scan History UI) on top,
  all still WRITTEN-only, not verified. Do not report the RoomPlan/App
  targets as "developed" or "verified" from Windows — only as WRITTEN, per
  the status vocabulary in `ROOMPLAN_TESTING_STRATEGY.md`.
- **First Xcode session TODO list** (do this before writing any RP4 code;
  supersedes the RP1/RP2-only version of this list — the RP1/RP2 items
  remain valid and are folded in below, with RP3's App-target items added
  as step 7):
  1. Open `ios/RenovexCapture/Package.swift` in Xcode (or generate an
     `.xcodeproj` from it, or use `xcodebuild`/`swift build` directly
     against the package — no `.xcodeproj` was hand-authored).
  2. Confirm the `#if os(macOS) || os(iOS)` split restores the full target
     graph (`RenovexCaptureRoomPlan`, `RenovexCaptureApp`,
     `RenovexCaptureAppTests`) on macOS as intended.
  3. Resolve compilation errors. Priority order by risk (highest first):
     (a) `RoomPlanCaptureAdapter.swift`'s `CapturedRoom.Wall`/`.Surface`/
     `.Object` property assumptions (RP2-MAC-001 through 012 — identifier,
     parentIdentifier, transform, dimensions, confidence, category enum
     cases — the exact case list for `Object.Category` in particular is a
     guess at Apple's documented set and is very likely to need
     correction); (b) `RoomPlanLiveSessionCoordinator.swift`'s
     `RoomCaptureSessionDelegate` conformance (RP2-MAC-013 — method
     signatures are confirmed correct per Apple's public docs, so this
     should compile; the risk here is runtime callback granularity, not
     compile-time shape); (c) `RoomPlanCaptureProvider.swift`'s
     `RoomBuilder.capturedRoom(from:)` call (RP2-MAC-016) and its
     `withCheckedThrowingContinuation`-based session lifecycle — this
     control flow has never been compiled, treat it as unverified logic,
     not just unverified types; (d)
     `RoomPlanCaptureViewRepresentable.swift`'s `RoomCaptureView`
     initializer/`captureSession` property (RP2-MAC-014).
  4. Run the full test suite across all 4 test-bearing areas — the 116
     already-Windows-verified `RenovexCaptureCoreTests` methods should also
     be re-run on Xcode as a cross-check, plus the 3
     `RenovexCaptureAppTests` methods that have never run anywhere; fix any
     failures.
  5. Simulator-testable items even without a physical LiDAR device: confirm
     `RoomPlanCaptureProvider.checkSupport()` correctly reports
     `.unsupported` on a non-LiDAR Simulator target (Simulator has no
     LiDAR, so `RoomCaptureSession.isSupported` should be `false` there —
     this is a real, if narrow, Tier B check available before any physical
     device is on hand).
  6. Append a real Tier B entry to the Verification Ledger below, and once
     that's done, a real Tier C entry is unlocked once a LiDAR device is
     also available (see RP-HW-001 onward in section E).
  7. **RP3-specific:** after the above, exercise the new App-target wiring
     in the Simulator (no LiDAR needed for this step, since it only
     exercises persistence/UI, not real RoomPlan capture): confirm
     `ScanRoomViewModel.startCapture()`'s mixed-typed-throws `do`/`catch`
     block compiles cleanly (watch for any typed-throws SIL-lowering issue
     similar to the one found and worked around in
     `FileSpatialCaptureRepositoryTests`/`FileRoomDraftRepositoryTests` this
     session — if the same class of crash reproduces here, apply the same
     bare-`catch` restructuring); confirm
     `RenovexCaptureApp.makeSpatialPersistence()` successfully creates its
     Application Support subdirectories on first launch; confirm
     `SpaceDetailView`'s Scan History section renders correctly for zero,
     one, and multiple persisted runs (can be exercised with
     `FixtureCaptureProvider`/a manually seeded `FileSpatialCaptureRepository`
     without needing a real RoomPlan session).
- Xcode project/scheme names in section D remain placeholders — RP1 chose
  Swift Package Manager specifically because it needs no Xcode-specific
  project file to exist yet; the exact scheme name Xcode assigns when it
  opens the package should be recorded here on first open.
- No RoomPlan fixture captures exist yet (see §32 in the amendment prompt —
  once real hardware is available, sanitized real captures should become
  permanent regression fixtures per privacy/legal review). RP1/RP2 added
  only in-Swift-code fixtures (`FixtureCapturePayload.rectangularRoom`,
  plus RP2's inline `CapturedWallInput`/`CapturedSurfaceInput`/
  `CapturedObjectInput` test constructions), not JSON-file fixtures, since
  no real RoomPlan output has ever been sampled.
- No Mac or LiDAR iPhone currently available in this environment — Tier B/C
  work is entirely blocked until hardware arrives.

---

## G. Actual result recording fields

Use the per-case template in section E for Tier C hardware acceptance cases.
Use the Verification Ledger below (renamed from "Windows Verification
Ledger" — it records Tier A, Tier B, and Tier C runs, not Windows-only runs)
for every implementation session's automated-test run, regardless of tier.

**Tier B/C are not optional completion gates just because hardware is
currently unavailable.** The milestone can reach WRITTEN and WINDOWS
VERIFIED status on Tier A alone, but M8.5C remains **PENDING XCODE/HARDWARE
VERIFICATION** until the required Tier B (Swift compiles, XCTest passes) and
Tier C (real RoomPlan/LiDAR behavior, RP-HW-00x cases) gates have actually
run and passed. The real RoomPlan golden scenario (design spec §44,
implementation plan Task 31) inherently requires real device capture, which
makes Tier C mandatory for final milestone completion — it is not a
"nice to have when hardware shows up." Do not report M8.5C as complete,
and do not treat Task 34's Final Verification Gate as satisfied, on Tier A
results alone.

---

## H. Links / references to relevant tests and fixtures

- Design spec: `docs/superpowers/specs/M8.5C-Spatial-Intelligence-Design-Spec-Refined.md`
  — §0 (amendment summary), §8.5 (freeze), §8.6–§8.29 (RoomPlan
  architecture).
- Implementation plan: `docs/superpowers/plans/M8.5C-Spatial-Intelligence-Implementation-Plan-Refined.md`
  — RP1–RP6 (current capture path), Task 5 (frozen, preserved record).
- Testing tiers: `docs/spatial/roomplan/ROOMPLAN_TESTING_STRATEGY.md`.
- Frozen Android provider source:
  `android/app/src/main/kotlin/com/renovex/capture/capture/reconstruction/`
  and `.../capture/arcore/`.
- Frozen Android provider fixtures:
  `android/app/src/test/resources/scan-fixtures/`.
- iOS RoomPlan source location: `ios/RenovexCapture/` (Swift Package
  Manager project — `Package.swift` at that root). Three source targets:
  `Sources/RenovexCaptureCore/` (provider-neutral domain — `Domain/`
  [now includes `RoomLocalCoordinateNormalizer.swift`, `LocalCaptureRun.swift`
  new in RP3], `Capture/` [now includes `LiveElementSnapshot.swift`,
  `LiveElementTracker.swift`, `CoincidentWallSanityCheck.swift`,
  `CapturedElementInput.swift`, `RoomDraftNormalizer.swift`,
  `CaptureCompletionCoordinator.swift` new in RP3], `Persistence/` [new
  directory, RP3: `JSONFileStore.swift`, `SpatialCaptureRepository.swift`,
  `FileSpatialCaptureRepository.swift`, `RoomDraftRepository.swift`,
  `FileRoomDraftRepository.swift`], `Networking/` subdirectories),
  `Sources/RenovexCaptureRoomPlan/` (the only RoomPlan-importing target —
  `RoomPlanCaptureProvider.swift`, `RoomPlanCaptureAdapter.swift`, and RP2's
  `RoomPlanLiveSessionCoordinator.swift` — unchanged by RP3),
  `Sources/RenovexCaptureApp/` (SwiftUI app shell — RP3 modified
  `RenovexCaptureApp.swift` [new `makeSpatialPersistence()` composition
  root], `RenovexCaptureRootView.swift`,
  `Features/ProjectSpace/ProjectListView.swift`,
  `Features/ProjectSpace/SpaceListView.swift` [all three: repository
  parameter threading only], `Features/SpatialCapture/ScanRoomView.swift`
  [`ScanRoomViewModel.startCapture()` now creates/persists a
  `LocalCaptureRun` and calls `CaptureCompletionCoordinator` instead of
  only building an in-memory summary], `Features/SpatialCapture/
  SpaceDetailView.swift` [static "AR Review"/"Concepts" stub rows replaced
  with a real Scan History list sourced from `SpatialCaptureRepository`]).
- iOS RoomPlan fixture location: in-Swift-code only for now —
  `FixtureCapturePayload` and `FixtureCaptureProvider` in
  `Sources/RenovexCaptureCore/Capture/FixtureCaptureProvider.swift`
  (unchanged from RP1; reused directly by RP3's
  `CaptureCompletionCoordinatorTests`). RP2's normalizer tests use inline
  `CapturedWallInput`/`CapturedSurfaceInput`/`CapturedObjectInput`
  constructions rather than a second named fixture object. No JSON-file
  fixtures exist yet (deferred until real RoomPlan output can be sampled on
  hardware, per §32).
- iOS test location: `ios/RenovexCapture/Tests/RenovexCaptureCoreTests/`
  (116 methods across 17 files, up from RP1/RP2's 92 methods/13 files — RP3
  added `RoomDraftCodableTests.swift`,
  `FileSpatialCaptureRepositoryTests.swift`,
  `FileRoomDraftRepositoryTests.swift`,
  `CaptureCompletionCoordinatorTests.swift`) and
  `ios/RenovexCapture/Tests/RenovexCaptureAppTests/` (3 methods, 1 file,
  unchanged from RP1 — still never executed, structurally excluded from
  the Windows build graph).
- Backend RP3 source: `backend/internal/spatial/artifact.go` (additive
  `ArtifactKind`/content-type values), `spatial.go` (`Provider`/
  `CaptureNumber`/`RoomDraftID` fields on `SpatialCapture`), `roomdraft.go`
  (new — `RoomDraft`/`RoomDraftWall`/`RoomDraftOpening`/`RoomDraftObject`
  Go domain types mirroring the iOS Swift shape), `repository.go`/
  `repository_mongo.go` (`RoomDraftRepository`/`MongoRoomDraftRepository`,
  `SetRoomDraft` added to `CaptureRepository`), `service.go`
  (`StartCapture` now assigns provider/captureNumber, new
  `SetRoomDraft`/`PersistRoomDraft`/`UpdateRoomDraft`/`GetRoomDraft`/
  `GetRoomDraftByCapture`), `backend/internal/platform/composition/
  services.go` (`spatialRoomDraftRepo` wired, `SetRoomDraftSupport` called).
  No new HTTP routes — RoomDraft sync rides the existing artifact upload
  pipeline via the new `roomdraft_json` `ArtifactKind`, per explicit user
  direction to keep this internal/additive rather than adding a new public
  contract.
- Web RP3 source: `apps/web/src/features/spatial/components/
  SpatialEntry.tsx` (capture-number display, "Continue Review" badge for
  `uploaded`/`review`-status captures), `apps/web/src/features/spatial/
  components/SpatialEntry.test.tsx` (fixtures updated for the now-required
  `captureNumber`/`provider` DTO fields, one new test added), `apps/web/
  openapi/openapi.json` + `apps/web/src/lib/api/generated/schema.ts`
  (regenerated from the Go DTOs, not hand-written).

---

## Verification Ledger (append-only)

Every implementation run touching this milestone — Tier A, B, or C —
appends an entry here. Never overwrite or delete a prior entry; corrections
get a new entry noting what was wrong. Never claim "all tests pass" without
naming the **Tier**, the **environment**, and the exact commands. This
single ledger replaces having separate Tier-A-only and Apple-only ledgers —
every entry states its own tier so the record stays unambiguous without
needing two documents.

### Entry template

```text
Date:
Implementation task(s):        e.g. RP1, RP3
Tier:                          A (Windows) / B (Mac/Xcode) / C (LiDAR device)
Environment:                   Tier A: OS build, Go version, Node version, etc.
                                Tier B: macOS version, Xcode version, Simulator/device target
                                Tier C: exact device model, iOS version
Device (Tier C only):          model, RoomCaptureSession.isSupported result
Commands actually executed:
  Backend unit:
  Backend integration:
  Web:
  Spatial contracts:
  Geometry:
  OpenAPI:
  Persistence/versioning:
  Fixture/schema:
  Swift build/XCTest (Tier B):
  RoomPlan/LiDAR case IDs run (Tier C, reference RP-HW-00x):
  Other:
Tests/counts (if available):
Pass/fail:
Failures and fixes:
Fixtures/regressions added:
What remains Apple-specific and unverified:
```

### Entries

```text
Date:                           2026-09-03
Implementation task(s):         RP1 — iOS Swift Foundation, Capture-Provider
                                 Abstraction, RoomPlan Integration Boundary
Tier:                           N/A — no Tier A/B/C command was executable
                                 this session; see explanation below.
Environment:                    Windows 11, bash tool, PowerShell tool.
                                 `swift --version` -> command not found.
                                 `Get-Command swift` -> not found.
                                 `wsl.exe --list --quiet` -> only
                                 `docker-desktop` distro present, no Linux
                                 distro with a Swift toolchain installed.
                                 CONFIRMED: no Swift toolchain of any kind
                                 (native Windows or WSL) exists on this
                                 machine. Package.swift also declares
                                 `platforms: [.iOS(.v16)]` with no `.macOS`
                                 entry, so even a hypothetical Windows/Linux
                                 Swift toolchain could not build this
                                 package — it is iOS-only by design (SwiftUI
                                 App target, RoomPlan dependency). This is a
                                 stronger statement than "Tier B pending" —
                                 there is no Tier A-equivalent for this
                                 package at all; Xcode/macOS is the floor.
Device (Tier C only):           N/A
Commands actually executed:
  Backend unit:                 none (RP1 touched no backend/Web files)
  Backend integration:          none
  Web:                          none
  Spatial contracts:            none
  Geometry:                     none
  OpenAPI:                      none
  Persistence/versioning:       none
  Fixture/schema:               none
  Swift build/XCTest (Tier B):  NOT RUN — no Mac/Xcode access this session.
                                 `swift build`/`swift test` also confirmed
                                 unavailable on Windows for this package
                                 (see Environment above).
  RoomPlan/LiDAR case IDs run:  none (Tier C, N/A — no RP-HW cases apply to
                                 RP1; RoomPlan capture wiring is RP2 scope)
  Other:                        Manual line-by-line review of the 401-retry
                                 logic in RenovexAPIClient.swift and the
                                 actor-isolation/defer-timing logic in
                                 SingleFlightRefresh.swift was performed by
                                 reading the source carefully against the
                                 test expectations in RenovexAPIClientTests.
                                 This is a manual code-reading check, NOT a
                                 substitute for an actual compiler/test run,
                                 and is not claimed as such.
Tests/counts (if available):    27 XCTest test methods WRITTEN across 10
                                 test files (RenovexCaptureCoreTests: 24
                                 methods in AccessTokenStoreTests,
                                 AuthSessionReducerTests,
                                 CoordinateContractTests,
                                 FixtureCaptureAdapterTests,
                                 ProjectSpaceSelectionTests,
                                 RenovexAPIClientTests,
                                 RoomDraftTests, SourceProvenanceTests,
                                 SpatialCaptureProviderSwapTests;
                                 RenovexCaptureAppTests: 3 methods in
                                 PersistedSessionFlagStoreTests). ZERO of
                                 these have been executed. All are WRITTEN
                                 only.
Pass/fail:                      NOT APPLICABLE — nothing was run.
Failures and fixes:             none (nothing run)
Fixtures/regressions added:     FixtureCapturePayload.rectangularRoom (a
                                 4m x 3m four-wall room, authored directly
                                 in the canonical Renovex frame) added to
                                 RenovexCaptureCore as the RP1 deterministic
                                 capture fixture. No JSON-file fixtures were
                                 added (all RP1 fixtures are in-Swift-code);
                                 real RoomPlan-shaped JSON replay fixtures
                                 are deferred to when actual CapturedRoom
                                 output is available to sample (see design
                                 spec §32 / plan constraint).
What remains Apple-specific
and unverified:                 EVERYTHING under ios/RenovexCapture/ —
                                 the entire package has never been compiled.
                                 Specifically highest-risk/most-likely-wrong
                                 if Apple's real API shape differs from what
                                 was written from documentation:
                                 RoomPlanCaptureAdapter.swift's use of
                                 CapturedRoom.Wall.identifier (assumed
                                 UUID), .transform (assumed simd_float4x4,
                                 column-major, wall-local space with wall
                                 spanning local +X), and .dimensions
                                 (assumed simd_float3 as
                                 width/height/thickness) — flagged inline in
                                 that file's own "NOT YET XCODE-VERIFIED"
                                 comment block. RoomCaptureSession.isSupported
                                 (used in RoomPlanCaptureProvider.checkSupport)
                                 is also unverified to compile, though this
                                 property's existence and shape is very
                                 well-documented and lower-risk than the
                                 Wall geometry properties.
```

The first implementation session with genuine Mac/Xcode access must, before
any further RP work: open this package in Xcode (or run
`swift build`/`swift test` from `ios/RenovexCapture/` if a command-line
toolchain is used instead), fix any compilation errors the RoomPlan API
assumptions above produce, run the full test suite, and append a new ledger
entry with real Tier B results — updating, not overwriting, this entry.

**Correction (2026-09-03, same day as the entry above):** the statement in
the previous entry that "no Swift toolchain of any kind exists on this
machine" is **NO LONGER TRUE**. A Windows Swift toolchain was subsequently
installed and verified functional. This correction entry does NOT replace
the entry above — it stands as an accurate record of what was true at the
time it was written. This is a genuinely new capability, not a re-run of the
same conditions.

```text
Date:                           2026-09-03 (later same day)
Implementation task(s):         RP1 — Windows toolchain verification pass
                                 (continuation; RP2 explicitly NOT started)
Tier:                           A — Windows, for RenovexCaptureCore and
                                 RenovexCaptureCoreTests ONLY.
                                 RenovexCaptureRoomPlan/RenovexCaptureApp
                                 remain Tier B/C-only — see root-cause below.
Environment:                    Windows 11. Swift version 6.3.3
                                 (swift-6.3.3-RELEASE). Target:
                                 x86_64-unknown-windows-msvc. Build config:
                                 +assertions. Confirmed via `swift --version`.
Device (Tier C only):           N/A
Commands actually executed:
  swift package describe        Ran successfully; confirmed Package.swift
                                 parses and (after the fix below) correctly
                                 excludes RenovexCaptureRoomPlan/
                                 RenovexCaptureApp/RenovexCaptureAppTests
                                 from the Windows target graph.
  swift package resolve         Ran successfully, no output (package has no
                                 external dependencies to resolve).
  swift build --target
    RenovexCaptureCore           "Build of target: 'RenovexCaptureCore'
                                 complete!" — builds cleanly standalone.
  swift build --target
    RenovexCaptureCoreTests      "Build of target: 'RenovexCaptureCoreTests'
                                 complete!" — builds cleanly standalone
                                 (after the Package.swift fix below).
  swift build                   "Build complete!" — full Windows-visible
                                 graph (Core + CoreTests only, post-fix).
  swift test                    Ran to completion. Initial run: 46/47 pass,
                                 1 failure (RenovexAPIClientTests.
                                 test_logout_clearsAccessToken — a genuine
                                 test-authoring bug, not a platform issue;
                                 see root-cause below). After fix: 47/47
                                 pass, 0 failures, re-verified with a clean
                                 rebuild.
Tests/counts (if available):    47 tests executed, 47 passed, 0 failed, 0
                                 skipped, across 9 suites:
                                 AccessTokenStoreTests, AuthSessionReducerTests,
                                 CoordinateContractTests,
                                 FixtureCaptureAdapterTests,
                                 ProjectSpaceSelectionTests,
                                 RenovexAPIClientTests, RoomDraftTests,
                                 SourceProvenanceTests,
                                 SpatialCaptureProviderSwapTests.
                                 (RenovexCaptureAppTests' 3 methods are NOT
                                 in this count — that target is excluded
                                 from the Windows build graph entirely; see
                                 "What remains Apple-specific" below.)
Pass/fail:                      PASS — RenovexCaptureCore and
                                 RenovexCaptureCoreTests are now WINDOWS
                                 VERIFIED (not Xcode-verified; see the
                                 status-vocabulary note below).
Failures and fixes:
  1. Initial `swift test` failed to build at all:
     'URLSession'/'URLResponse'/'HTTPURLResponse'/'URLRequest' unavailable
     — "This type has moved to the FoundationNetworking module." Root
     cause: on non-Darwin Swift, these types live in FoundationNetworking,
     not Foundation. Fix: added
       #if canImport(FoundationNetworking)
       import FoundationNetworking
       #endif
     immediately after `import Foundation` in the 4 files that actually
     reference these types in code (not just in doc comments):
     Sources/RenovexCaptureCore/Networking/RenovexAPIClient.swift,
     Sources/RenovexCaptureCore/Networking/SingleFlightRefresh.swift,
     Tests/RenovexCaptureCoreTests/MockURLProtocol.swift,
     Tests/RenovexCaptureCoreTests/RenovexAPIClientTests.swift. Three other
     files (PersistedSessionFlagStore.swift, AuthSessionState.swift,
     AuthAPI.swift) only mention "URLSession" inside doc comments and did
     NOT need the import. No new Windows-specific networking implementation
     was introduced — the same URLSession-based RenovexAPIClient code path
     runs on both platforms via the conditional import, exactly the
     standard cross-platform Foundation pattern.
  2. A `stored property 'session' ... has non-Sendable type` warning
     reported before the fix was NOT a genuine Swift 6 concurrency defect —
     it was a direct symptom of `URLSession` resolving to the unavailable
     `public typealias URLSession = AnyObject` stand-in (Foundation ships a
     deliberately-unavailable placeholder alias on non-Darwin platforms so
     the type name still exists for the diagnostic to reference). Once the
     real `FoundationNetworking.URLSession` was imported, this warning
     disappeared with no code change beyond the import fix — confirmed by
     its absence from every subsequent build. No `@unchecked Sendable`,
     no concurrency-safety code change, was needed or added.
  3. `swift test` still failed after the FoundationNetworking fix:
     `no such module 'RoomPlan'` from RenovexCaptureRoomPlan. Root cause:
     `swift test` links ONE combined test binary for every test target in
     the package by default in this SwiftPM version (tools-version 5.9),
     so building RenovexCaptureCoreTests transitively required building
     RenovexCaptureRoomPlan → RenovexCaptureApp → RenovexCaptureAppTests
     first, and RoomPlan is a genuinely Apple-only framework with no
     Windows equivalent — this is the real Apple-framework boundary the
     RP1 task instructions anticipated as an acceptable stopping condition,
     confirmed by testing (`--filter`, `--target`, checking for a
     per-product test flag) that no CLI flag in this SwiftPM version
     isolates one test target's build from the rest of the package graph.
     Fix (approved by the user before applying): restructured
     `Package.swift`'s `targets`/`products` arrays to be built with
     conditional `#if os(macOS) || os(iOS)` guards around
     RenovexCaptureRoomPlan, RenovexCaptureApp, and RenovexCaptureAppTests
     (and their corresponding library products) — on Windows/Linux, only
     RenovexCaptureCore and RenovexCaptureCoreTests exist in the build
     graph at all; on macOS/iOS, the full original graph is restored
     unchanged. This is a build-graph-visibility change in the package
     manifest only — no Swift source file's semantics changed, no test was
     weakened, skipped, or deleted, and the RenovexCaptureCore /
     RenovexCaptureRoomPlan / RenovexCaptureApp module boundary itself
     (which files import RoomPlan, which contain SwiftUI, etc.) is
     completely unchanged. This is standard, idiomatic SwiftPM practice for
     platform-limited targets, not a workaround unique to this project.
  4. After the Package.swift fix, `swift test` ran the full 47-test suite
     and found ONE real, genuine test-authoring bug (not caused by Windows,
     not caused by the above fixes):
     `RenovexAPIClientTests.test_logout_clearsAccessToken` failed with
     `NSURLErrorDomain Code=-1100` ("file does not exist" —
     MockURLProtocol's own empty-stub-queue fallback error). Root cause:
     `RenovexAPIClient.authenticatedGet` performs the original request AND,
     on a 401, one retry after attempting refresh — this test only queued
     ONE `/projects` stub but the client legitimately makes TWO `/projects`
     requests in this exact scenario (original 401, then a retry 401 after
     the also-401 refresh attempt). The second request hit
     MockURLProtocol's empty-queue path instead of exercising the intended
     assertion. Fix: queued two 401 stubs for `/projects` in that one test,
     matching the real two-request behavior — no production code changed,
     the 401→refresh→retry-once behavior itself was NOT modified, weakened,
     or conditionally disabled in any way; this was purely a test-fixture
     completeness bug the Windows run correctly surfaced. Re-ran clean:
     47/47 pass.
Fixtures/regressions added:     None beyond the RP1 entry above. The
                                 stub-count fix in
                                 test_logout_clearsAccessToken is a
                                 correction to an existing test, not a new
                                 fixture.
What remains Apple-specific
and unverified:                 RenovexCaptureRoomPlan (RoomPlanCaptureAdapter.swift,
                                 RoomPlanCaptureProvider.swift) and
                                 RenovexCaptureApp (all SwiftUI views,
                                 AppSession, PersistedSessionFlagStore,
                                 RenovexCaptureAppTests' 3 methods) remain
                                 completely unverified — they are now
                                 EXCLUDED from the Windows build graph
                                 entirely (see fix #3 above), so Windows
                                 verification says nothing whatsoever about
                                 whether they compile. The
                                 CapturedRoom.Wall property-shape risk
                                 flagged in the entry above is UNCHANGED and
                                 still applies in full — nothing about this
                                 session reduces that risk, since RoomPlan
                                 code was never built by any command run
                                 here.
```

**Status-vocabulary application:** `RenovexCaptureCore` and
`RenovexCaptureCoreTests` are now **WINDOWS VERIFIED** (47/47 passing, real
compiler + real test run, reproduced twice including once after a from-
scratch rebuild). This is explicitly **NOT** XCODE VERIFIED and **NOT**
LIDAR PHYSICALLY VERIFIED — `RenovexCaptureRoomPlan` and `RenovexCaptureApp`
remain entirely unbuilt by any command executed in this environment. Do not
conflate "Windows Verified" (a real, narrower, genuine achievement) with
"the iOS app works" — the app-level SwiftUI/RoomPlan code has had zero
verification of any kind beyond the original manual code review noted in
the entry above.

---

```text
Date:                           2026-09-04
Implementation task(s):         RP2 — RoomPlan Capture, CapturedRoom/
                                 CapturedRoomData Handling, Canonical
                                 Coordinate Normalization, Stable Renovex
                                 IDs. RP1 was NOT reopened (no RP1 defect
                                 was found or needed fixing). RP3 was NOT
                                 started.
Tier:                           A — Windows, for RenovexCaptureCore and
                                 RenovexCaptureCoreTests ONLY (same scope
                                 as the RP1 correction entry above).
                                 RenovexCaptureRoomPlan/RenovexCaptureApp
                                 remain Tier B/C-only.
Environment:                    Windows 11. Swift version 6.3.3
                                 (swift-6.3.3-RELEASE). Target:
                                 x86_64-unknown-windows-msvc. Same
                                 toolchain as the RP1 Windows-verification
                                 entry above — no environment change this
                                 session.
Device (Tier C only):           N/A
Commands actually executed:
  swift package describe        Ran successfully; graph still correctly
                                 shows only RenovexCaptureCore/
                                 RenovexCaptureCoreTests on Windows after
                                 RP2's additions.
  swift package resolve         Ran successfully, no output.
  swift build --target
    RenovexCaptureCore           Builds cleanly after each RP2 addition
                                 (verified incrementally: RoomDraft
                                 extension, LiveElementTracker, coordinate
                                 normalizer, coincident-wall check,
                                 CapturedElementInput snapshots,
                                 RoomDraftNormalizer — each compiled clean
                                 on first or near-first attempt).
  swift build                   "Build complete!" — full Windows-visible
                                 graph.
  swift test                    Ran to completion, reproduced multiple
                                 times across the session as each new
                                 component was added, and once more after
                                 a full clean rebuild at the end. Zero
                                 failures at any point in this session —
                                 unlike RP1's Windows-verification session,
                                 no test-authoring bug was introduced or
                                 found this time.
Tests/counts (if available):    92 tests executed, 92 passed, 0 failed, 0
                                 skipped — up from RP1's 47 (45 new RP2
                                 tests added), across these NEW suites:
                                 LiveElementTrackerTests (10),
                                 RoomLocalCoordinateNormalizerTests (12),
                                 CoincidentWallSanityCheckTests (9),
                                 RoomDraftNormalizerTests (14). All prior
                                 RP1 suites (AccessTokenStoreTests,
                                 AuthSessionReducerTests,
                                 CoordinateContractTests,
                                 FixtureCaptureAdapterTests,
                                 ProjectSpaceSelectionTests,
                                 RenovexAPIClientTests, RoomDraftTests,
                                 SourceProvenanceTests,
                                 SpatialCaptureProviderSwapTests) still
                                 pass unchanged at their original counts —
                                 no RP1 regression.
Pass/fail:                      PASS — 92/92, reproduced after a from-
                                 scratch-adjacent full `swift build` +
                                 `swift test` at session end.
Failures and fixes:             None this session. (Contrast with the RP1
                                 Windows-verification entry above, which
                                 did find and fix one genuine test-stub
                                 bug — no equivalent issue arose in RP2.)
Fixtures/regressions added:     No new named fixture objects (RP1's
                                 FixtureCapturePayload.rectangularRoom is
                                 unchanged and still used by
                                 FixtureCaptureAdapterTests). RP2 tests use
                                 inline `CapturedWallInput`/
                                 `CapturedSurfaceInput`/`CapturedObjectInput`
                                 constructions per-test rather than a
                                 shared named fixture, since RP2's
                                 normalization surface (walls + openings +
                                 objects + parent resolution + rescan
                                 identifier independence) needed enough
                                 distinct input shapes that a single shared
                                 fixture would not have covered the cases
                                 cleanly.
What remains Apple-specific
and unverified:                 RenovexCaptureRoomPlan
                                 (RoomPlanCaptureAdapter.swift,
                                 RoomPlanCaptureProvider.swift,
                                 RoomPlanLiveSessionCoordinator.swift — new
                                 this session) and RenovexCaptureApp
                                 (ScanRoomView.swift,
                                 RoomPlanCaptureViewRepresentable.swift —
                                 new this session; plus the RP1 views)
                                 remain completely unverified, structurally
                                 excluded from the Windows build graph as
                                 before. RP2-MAC-001 through RP2-MAC-016
                                 pending Xcode compile checks are listed in
                                 full inline at the top of
                                 RoomPlanCaptureAdapter.swift and
                                 RoomPlanLiveSessionCoordinator.swift —
                                 summarized:
                                   - CapturedRoom.Surface/.Object property
                                     shapes (identifier, parentIdentifier,
                                     transform, dimensions, confidence,
                                     category enum cases) — same class of
                                     risk as RP1's CapturedRoom.Wall
                                     assumptions, now extended to
                                     doors/windows/openings/objects.
                                   - RoomCaptureSessionDelegate method
                                     signatures — CONFIRMED against Apple's
                                     public documentation this session
                                     (didAdd/didChange/didRemove/didUpdate/
                                     didEndWith), so this is lower risk
                                     than the property-shape items above;
                                     remaining uncertainty is exact
                                     runtime callback granularity/timing,
                                     not whether the methods exist.
                                   - RoomBuilder.capturedRoom(from:) async
                                     throws API shape (RP2-MAC-016) —
                                     documented as the standard
                                     CapturedRoomData → CapturedRoom
                                     processing step but not compiled.
                                   - RoomCaptureView initializer/
                                     captureSession property
                                     (RP2-MAC-014).
                                 The RoomPlanCaptureProvider.startCapture()
                                 live-session continuation logic
                                 (withCheckedThrowingContinuation wired to
                                 session.run()/session.stop() and the
                                 delegate's didEndWith callback) is a
                                 reasonable, standard Swift concurrency
                                 pattern but has never been compiled, let
                                 alone run against a real RoomCaptureSession
                                 lifecycle — treat this as unverified
                                 control flow, not just unverified types.
```

**RP2 status-vocabulary application:** the same `RenovexCaptureCore`/
`RenovexCaptureCoreTests` scope remains WINDOWS VERIFIED, now at 92/92
rather than 47/47. `RenovexCaptureRoomPlan` and `RenovexCaptureApp` remain
entirely `NOT YET XCODE-VERIFIED` — RP2 added substantially more Apple-side
code (a full live-session delegate, `RoomBuilder` integration, a
`UIViewRepresentable` wrapper) than RP1 did, so the absolute amount of
unverified Apple-specific surface area has grown even though the
Windows-verified Core surface also grew. Do not read "92/92 passing" as
implying RoomPlan capture works — it implies the provider-neutral
normalization math and identity/dedup logic that RoomPlan's output will
eventually flow through is correct, which is a real but narrower claim.

---

```text
Date:                           2026-09-04
Implementation task(s):         RP3 — Persistent Multi-Run Capture
                                 Persistence, Local RoomDraft Persistence,
                                 Scan History / Continue Review. RP1/RP2
                                 were NOT reopened (no defect found). RP4
                                 was NOT started. Backend/Web work was also
                                 performed this session (see below) — this
                                 is genuinely full-stack RP3 scope per the
                                 plan, not iOS-only.
Tier:                           A — Windows, for RenovexCaptureCore/
                                 RenovexCaptureCoreTests (iOS), plus the Go
                                 backend (`internal/spatial`,
                                 `internal/platform/composition`) and the
                                 Web app (`apps/web`), all genuinely
                                 Tier A / no Mac or hardware dependency.
                                 RenovexCaptureApp's new Scan History/
                                 persistence-wiring UI remains Tier B/C-only
                                 — see "What remains Apple-specific" below.
Environment:                    Windows 11. Swift 6.3.3
                                 (swift-6.3.3-RELEASE),
                                 x86_64-unknown-windows-msvc — same iOS
                                 toolchain as RP1/RP2. Go (backend module),
                                 Node/npm (apps/web) — versions unchanged
                                 from the existing project setup, no new
                                 tooling installed this session.
Device (Tier C only):           N/A
Commands actually executed:
  Backend unit/integration:     `cd backend && go build ./...` (clean),
                                 `go vet ./...` (clean, after fixing one
                                 fake-repository interface-conformance gap
                                 — see Failures and fixes), `gofmt -l` /
                                 `gofmt -w` on 3 test files (formatting
                                 only, no behavior change),
                                 `go test ./internal/spatial/... -count=1`
                                 (ran twice: once immediately after the
                                 additive SpatialCapture/ArtifactKind
                                 changes, once after adding RoomDraft
                                 persistence — both full passes, real
                                 MongoDB via testcontainers-go),
                                 `go test ./internal/spatial/...
                                 ./internal/platform/composition/...
                                 -count=1` (composition drift-guard
                                 cross-check).
  OpenAPI:                      `cd backend && go run ./cmd/openapi -out
                                 ../apps/web/openapi/openapi.json`
                                 (regenerated; confirmed new
                                 `provider`/`captureNumber`/`roomDraftId`
                                 fields present via grep before proceeding).
  Web:                          `cd apps/web && npm run openapi:generate`
                                 (regenerated TS types from the new
                                 schema.json — no hand-written parallel
                                 types added, per plan §RP3's explicit
                                 instruction), `npx vitest run
                                 src/features/spatial/components/SpatialEntry.test.tsx`,
                                 `npx vitest run src/features/spatial`
                                 (full feature), `npx tsc --noEmit` (full
                                 app type-check).
  Swift build/XCTest (Tier A,
  iOS provider-neutral half):   `swift package describe`, `swift package
                                 resolve`, `swift build` (multiple times,
                                 incrementally as each new file was added),
                                 `swift test` (multiple times; final run
                                 after all RP3 iOS changes including the
                                 App-target wiring, since RenovexCaptureApp
                                 is excluded from the Windows build graph
                                 and its presence/absence does not affect
                                 what `swift test` can execute on Windows).
  Swift build/XCTest (Tier B):  NOT RUN — no Mac/Xcode access this session,
                                 same as RP1/RP2.
  RoomPlan/LiDAR case IDs run:  none (Tier C, N/A this session)
  Other:                        Manual line-by-line review of
                                 ScanRoomView.swift/SpaceDetailView.swift/
                                 ProjectListView.swift/SpaceListView.swift/
                                 RenovexCaptureRootView.swift/
                                 RenovexCaptureApp.swift's call-site wiring
                                 (grep-verified every call site of the
                                 changed initializers was updated, no stale
                                 signature left). This is a manual
                                 code-reading check, NOT a substitute for
                                 an actual compiler run on this SwiftUI/
                                 RoomPlan-importing code, and is not
                                 claimed as such.
Tests/counts (if available):    Backend: 21 new/extended Go tests (4 new
                                 ArtifactKind table cases in
                                 TestRequestArtifactUpload_AcceptsRoomPlanArtifactKinds,
                                 3 new StartCapture/SetRoomDraft service
                                 tests, 1 new SpatialCapture Mongo
                                 round-trip test, 4 new RoomDraft Mongo
                                 repository tests) — all passing, zero
                                 regression in the existing ~36+ spatial
                                 tests. iOS: 116 XCTest methods executed,
                                 116 passed, 0 failed, 0 skipped — up from
                                 RP1/RP2's 92 (24 new RP3 tests: 5
                                 RoomDraftCodableTests, 9
                                 FileSpatialCaptureRepositoryTests, 6
                                 FileRoomDraftRepositoryTests, 4
                                 CaptureCompletionCoordinatorTests). Web: 4
                                 SpatialEntry tests passed (3 existing + 1
                                 new "Continue Review" test), full `tsc
                                 --noEmit` clean across the whole app.
Pass/fail:                      PASS across all three layers — backend Go
                                 tests, iOS Swift tests, Web vitest +
                                 tsc — reproduced with a final full run of
                                 each after all RP3 changes were complete.
Failures and fixes:
  1. Go: after adding `SetRoomDraft` to the `CaptureRepository` interface,
     `go vet ./...` failed with "fakeCaptureRepo does not implement
     CaptureRepository (missing method SetRoomDraft)" in
     service_test.go's hand-written fake repository. Fix: added the
     missing method to the fake, matching its existing
     MarkConfirmed/UpdateStatus method style. Not a production defect —
     a test-double completeness gap the compiler correctly caught.
  2. Swift: `swift test` crashed the Swift 6.3.3 frontend with a SIL
     ownership-verifier internal compiler error ("Found outside of
     lifetime use?!") specifically in
     FileSpatialCaptureRepositoryTests.test_findByID_unknownID_throwsNotFound,
     reproducibly, whenever a `do { ... } catch SpecificError.case { }`
     pattern (binding no variable) was used around an `async
     throws(TypedError)` call inside an async XCTest method. Root cause:
     a genuine Swift 6.3.3 toolchain bug in typed-throws SIL lowering on
     Windows, not a logic error — confirmed by finding that the
     codebase's own existing working pattern
     (SpatialCaptureProviderSwapTests, which also calls an `async
     throws(TypedError)` method) uses a bare `catch { XCTAssertEqual(error,
     ...) }` with no `as`/case pattern at all. Fix: rewrote the three
     affected `catch` blocks in FileSpatialCaptureRepositoryTests.swift
     and FileRoomDraftRepositoryTests.swift to match that existing
     working convention. No production code changed; this is a test-only
     workaround for a toolchain bug, documented here so a future Xcode/Mac
     run can confirm whether the same crash reproduces there (if it does
     not, the bare-catch style is still the correct convention to keep,
     since it already matched the pre-existing codebase pattern).
  3. Swift: `override func setUp() throws` in
     CaptureCompletionCoordinatorTests.swift failed to compile — "cannot
     override non-throwing instance method with throwing instance method"
     — this Windows XCTest overlay's `setUp()` does not support the
     throwing override some other XCTest environments allow, and no
     `setUpWithError()` precedent existed elsewhere in this test target.
     Fix: made `setUp()` non-throwing and used `try!` for the two
     repository constructions (a fresh temp-directory-backed repository
     failing to construct is an environment failure, not a test case to
     assert on).
  4. Swift: two FileSpatialCaptureRepositoryTests round-trip assertions
     (`test_create_thenFindByID_returnsTheRun`,
     `test_scanPersistsAcrossRepositoryRecreation`) failed
     XCTAssertEqual despite visually-identical printed `LocalCaptureRun`
     descriptions. Root cause: `JSONFileStore`'s `.iso8601` date-encoding
     strategy truncates `Date()`'s sub-second precision on encode, so a
     decoded `Date` no longer `==` the original in-memory value even
     though both print identically to whole-second granularity. Fix:
     switched `JSONFileStore`'s `dateEncodingStrategy`/
     `dateDecodingStrategy` to `.secondsSince1970` (exact round-trip
     fidelity; this is an internal file format, not a wire format needing
     ISO8601 human-readability).
  5. Design correction (caught before implementation completed, not a
     bug found after the fact): `LocalCaptureRun` was initially designed
     with a `companyID` field mirroring the backend's `SpatialCapture`,
     but the iOS client has no source of truth for its own companyID
     anywhere (the JWT/access token is opaque to the client — only the
     backend derives companyID server-side). Corrected by removing the
     field entirely (confirmed with the user first) rather than shipping
     an always-nil/fake field — local storage is already naturally scoped
     to one device/one logged-in session.
Fixtures/regressions added:     No new named Swift fixture objects beyond
                                 RP1/RP2's FixtureCapturePayload (RP3's
                                 CaptureCompletionCoordinatorTests reuses
                                 it directly, proving the coordinator
                                 against the same rectangular-room fixture
                                 rather than inventing a parallel one). Go:
                                 no new fixture files — RoomDraft
                                 persistence tests construct fixture
                                 values inline per the existing
                                 repository_mongo_test.go convention.
What remains Apple-specific
and unverified:                 RenovexCaptureRoomPlan is unchanged this
                                 session (RP2-MAC-001 through
                                 RP2-MAC-016 remain exactly as before —
                                 see the RP2 entry above). RenovexCaptureApp
                                 gained RP3 changes across 6 files
                                 (ScanRoomView.swift's persistence-ordering
                                 wiring in ScanRoomViewModel.startCapture(),
                                 SpaceDetailView.swift's real Scan History
                                 list replacing static stub rows,
                                 ProjectListView.swift/SpaceListView.swift/
                                 RenovexCaptureRootView.swift's repository
                                 parameter threading, and
                                 RenovexCaptureApp.swift's
                                 makeSpatialPersistence() composition root)
                                 — none of this has been compiled by any
                                 command in this environment. Unlike RP2's
                                 unverified surface, RP3's App-target
                                 additions introduce no NEW Apple-framework
                                 API assumptions (no new RoomPlan/ARKit
                                 types touched) — the risk here is
                                 unverified SwiftUI view composition/
                                 `@Observable`/`@State` wiring correctness
                                 (e.g. whether `ScanRoomViewModel`'s
                                 mixed-typed-throws `do`/`catch` block in
                                 startCapture() compiles as expected under
                                 Xcode's Swift compiler, given the Windows
                                 SIL-verifier bug found and worked around
                                 in the test target this same session —
                                 that specific bug was in test code, not
                                 app code, but its existence is a signal
                                 to watch for related typed-throws
                                 compiler issues on first Xcode compile).
```

**RP3 status-vocabulary application:** `RenovexCaptureCore`/
`RenovexCaptureCoreTests` remain WINDOWS VERIFIED, now at 116/116 (up from
92/92) — provider-neutral local persistence (capture-run + RoomDraft
repositories, the required completion-ordering coordinator, and full
Codable round-trip fidelity including stable-ID/provenance/transform
preservation) is now proven correct on Windows, including cross-repository-
instance survival tests that simulate an app relaunch. The Go backend and
Web app RP3 deltas are fully verified in this same session, independent of
any Apple toolchain. `RenovexCaptureRoomPlan` and `RenovexCaptureApp` remain
entirely `NOT YET XCODE-VERIFIED` — do not read "116/116 passing" or "Go/Web
tests pass" as implying the actual iOS Scan Room screen or Scan History UI
works; those claims require an actual Xcode compile and, for real capture
behavior, a LiDAR device.

---

```text
Date:                           2026-09-04 (later same day as RP3)
Implementation task(s):         RP4A — Shared Edit-Operation Vocabulary,
                                 Domain Foundation, Validation (design spec
                                 §8.13, plan §RP4). Explicitly scoped by the
                                 user to foundation-only — NOT the
                                 interactive 2D/3D Room Review editor
                                 (RP4B+, not started). RP1-RP3 were NOT
                                 reopened (no defect found).
Tier:                           A — Windows, for RenovexCaptureCore/
                                 RenovexCaptureCoreTests AND the Go backend
                                 (`internal/spatial`). RP4A touched ONLY
                                 these two areas — RenovexCaptureRoomPlan/
                                 RenovexCaptureApp are entirely unchanged
                                 this session, so their NOT YET
                                 XCODE-VERIFIED status and the RP2-MAC-xxx
                                 pending items above are unaffected in
                                 either direction.
Environment:                    Windows 11. Swift 6.3.3
                                 (swift-6.3.3-RELEASE),
                                 x86_64-unknown-windows-msvc — same
                                 toolchain as RP1/RP2/RP3. Go (backend
                                 module) — unchanged from prior sessions.
Device (Tier C only):           N/A
Commands actually executed:
  Backend unit/integration:     `cd backend && go build ./...` (clean, run
                                 repeatedly as each new file was added),
                                 `go vet ./...` (clean), `gofmt -w` on every
                                 new/modified Go file (formatting only),
                                 `go test ./internal/spatial/... -count=1`
                                 (run multiple times through the session —
                                 after the domain-model extensions, again
                                 after the 27-operation vocabulary file,
                                 again after the service-level Reset/Apply
                                 methods, and a final full pass),
                                 `go test ./internal/platform/composition/...
                                 -count=1` (drift-guard cross-check).
  Swift build/XCTest (Tier A):  `swift build` (run repeatedly, including
                                 after the OpeningKind breaking rename and
                                 after the 9-enum Codable fix), `swift test`
                                 (run repeatedly through the session), and
                                 a FINAL genuine full clean rebuild (`.build`
                                 directory removed, all 32 RenovexCaptureCore
                                 source files recompiled from scratch,
                                 40.80s) followed by `swift test` — not a
                                 claim reused from an earlier incremental
                                 build.
  Swift build/XCTest (Tier B):  NOT RUN — no Mac/Xcode access this session,
                                 unchanged from RP1/RP2/RP3. RP4A touched no
                                 file in RenovexCaptureRoomPlan or
                                 RenovexCaptureApp.
  RoomPlan/LiDAR case IDs run:  none (Tier C, N/A — RP4A is domain/
                                 validation foundation only, no capture or
                                 UI surface)
  Other:                        None beyond normal code-reading review.
Tests/counts (if available):    Backend: 124 tests passing (up from ~62
                                 pre-RP4A) — 55 new operation-level tests
                                 (editoperation_test.go), 7 new
                                 service-level tests
                                 (roomdraft_service_test.go), 1 new real-
                                 MongoDB round-trip test proving every RP4A
                                 field survives persistence
                                 (repository_mongo_test.go). Zero
                                 regression in pre-existing tests. iOS: 167
                                 tests passing (up from 116) — 39 new
                                 operation tests (EditOperationTests.swift),
                                 6 new Reset-to-Scan tests
                                 (RoomDraftResetTests.swift), 6 new
                                 wire-contract tests
                                 (EditOperationWireContractTests.swift).
                                 Zero regression, confirmed via the genuine
                                 full clean rebuild described above (not
                                 just an incremental "still passes").
Pass/fail:                      PASS across both platforms, reproduced
                                 multiple times through the session plus
                                 one final full-suite run of each after all
                                 RP4A changes were complete.
Failures and fixes:
  1. Go: none — the 27-operation vocabulary, domain extensions, and
     service methods compiled and passed on first or near-first attempt
     for each file, following the already-established awards/draft.go
     discriminated-Validate() pattern closely.
  2. Swift: the OpeningKind breaking rename (door|window|opening|archway
     -> door|window|archway|other) broke one pre-existing RP2 test
     (RoomDraftNormalizerTests.test_normalize_genericOpening_producesRoomDraftOpeningWithOpeningKind,
     asserting `.opening`, a case that no longer exists). Root cause: a
     deliberate, planned breaking change (confirmed with the user before
     implementation), not an accidental regression. Fix: updated the test
     to assert `.other` and renamed it
     test_normalize_genericOpening_producesRoomDraftOpeningWithOtherKind
     to describe the corrected behavior.
  3. Swift: a GENUINE cross-platform wire-contract bug, found by the new
     EditOperationWireContractTests.swift (specifically
     test_openingKind_encodesToGoMatchingWireStrings), not anticipated
     when the domain types were first written. All 9 Swift domain enums
     (MeasurementStatus, OpeningKind, OpeningProfile, DoorHinge, DoorSwing,
     ElementOrigin, FixtureCategory, ServicePointKind, ConstraintKind) were
     plain (non-String-backed) enums whose synthesized Codable
     conformance encodes as {"door":{}} rather than the plain "door"
     string Go's string-typed enum produces — confirmed by the test
     literally asserting Optional("{\"door\":{}}") was NOT equal to
     Optional("\"door\""). Root cause: RP1/RP2 established the
     SpatialCaptureSourceProvider: String pattern correctly, but it was
     not consistently applied to every subsequently-added domain enum,
     including RP2's own MeasurementStatus, which predates RP4A. Fix (user
     confirmed the scope before implementation): all 9 enums made
     String-backed with raw values matching Go's exact strings
     (snake_case for multi-word cases: built_in_cabinetry, wall_fixture,
     electrical_panel, immovable_obstacle). MeasurementStatus specifically
     kept a hand-written Codable conformance (not synthesized) so
     encode(to:) always emits the new canonical plain-string form while
     init(from:) additionally accepts the OLD synthesized
     {"estimated":{}}/{"unconfirmed":{}} shape — proven by two dedicated
     legacy-decode regression tests
     (test_measurementStatus_decodesLegacySynthesizedShape) — so no
     RoomDraft a pre-RP4A build already persisted locally via
     FileRoomDraftRepository is silently invalidated by this fix. The 8
     new RP4A enums needed no legacy-decode path since nothing had ever
     persisted them before this session.
  4. Swift: two separate genuine Swift 6.3.3 typed-throws compiler issues
     were hit and fixed while writing RoomDraftRepository's new
     resetToBaseline(forCapture:) extension method (NOT the same SIL
     ownership-verifier crash found in RP3's test code, though related in
     kind): (a) `catch .notFound { throw .notFound }` inside a
     throws(RoomDraftResetPersistenceError) function body produced a
     compile error ("thrown expression type 'RoomDraftRepositoryError'
     cannot be converted to error type 'RoomDraftResetPersistenceError'")
     because the `.notFound` on the right ambiguously resolved against the
     wrong enum type given the surrounding typed-throws inference; fixed
     by using a bare `catch { switch error { ... } }` with explicit
     fully-qualified case construction
     (`RoomDraftResetPersistenceError.notFound`) instead of shorthand dot
     syntax on both sides. This was caught by the Swift compiler itself
     (a real type error, not a runtime bug), not by a test — flagged here
     because it is the same general class of typed-throws inference
     fragility as RP3's SIL crash, worth watching for on first Xcode
     compile.
Fixtures/regressions added:     Go: no new named fixture files — operation
                                 tests construct fixture RoomDraft values
                                 inline via small helper functions
                                 (draftWithOneWall, draftWithOneOpening,
                                 etc.) per the existing
                                 repository_mongo_test.go convention. Swift:
                                 no new named fixture objects — reuses the
                                 same inline-construction style as the Go
                                 side for direct behavioral parity, plus
                                 RP1's FixtureCapturePayload remains
                                 unchanged and unused by RP4A (RP4A's
                                 operations construct RoomDraft values
                                 directly, not through the capture-provider
                                 pipeline).
What remains Apple-specific
and unverified:                 Entirely unchanged from the RP3 entry
                                 above — RP4A touched zero files in
                                 RenovexCaptureRoomPlan or
                                 RenovexCaptureApp. RP2-MAC-001 through
                                 RP2-MAC-016 remain exactly as listed
                                 there. RP4A's own new Swift code
                                 (RoomDraft.swift extensions,
                                 EditOperation.swift, the RoomDraftRepository
                                 reset extension) lives entirely in
                                 RenovexCaptureCore, which IS Windows-
                                 verified — there is no Apple-specific
                                 surface introduced by RP4A at all.
```

**RP4A status-vocabulary application:** `RenovexCaptureCore`/
`RenovexCaptureCoreTests` remain WINDOWS VERIFIED, now at 167/167 (up from
116/116), confirmed via a genuine full clean rebuild rather than only an
incremental one. The Go backend's RP4A deltas are fully verified in this
same session (124 tests, up from ~62), independent of any Apple toolchain.
Because RP4A touched zero files in `RenovexCaptureRoomPlan`/
`RenovexCaptureApp`, their Xcode-verification status is unchanged from the
RP3 entry above — this is the first RP task where 100% of the session's new
code is Windows-verified with no Mac-pending residue at all. Do not read
"167/167 passing" as implying an interactive Room Review editor exists —
RP4A is explicitly the contract/domain/validation foundation only; the
actual 3D/2D editing UI (RP4B+) has not been started, per explicit user
scope.

---

```text
Date:                           2026-09-04 (same day as RP4A)
Implementation task(s):         RP3.5/RP4B0 — SpatialArtifactStore +
                                 Durable Capture/Draft Synchronization
                                 Foundation. Closes RP3's flagged
                                 SpatialArtifactStore deviation. Explicitly
                                 scoped by the user: capture/draft artifact
                                 sync only — NOT full EditOperation server
                                 sync, NOT the interactive editor, NOT
                                 merge_wall/split_wall (RP4A's own
                                 deferral, untouched). RP1-RP4A were NOT
                                 reopened except for the one additive
                                 backend change below (no defect found in
                                 any prior task; StartCapture's missing
                                 idempotency was a genuine gap identified
                                 during this session's own grounding pass,
                                 not a regression).
Tier:                           A — Windows, for RenovexCaptureCore/
                                 RenovexCaptureCoreTests AND the Go backend
                                 (internal/spatial). Touched ONLY these two
                                 areas — RenovexCaptureRoomPlan/
                                 RenovexCaptureApp are entirely unchanged,
                                 so their NOT YET XCODE-VERIFIED status and
                                 the RP2-MAC-xxx pending items are
                                 unaffected in either direction. Second
                                 consecutive RP task where 100% of new code
                                 is Windows-verified with zero Mac-pending
                                 residue.
Environment:                    Windows 11. Swift 6.3.3
                                 (swift-6.3.3-RELEASE),
                                 x86_64-unknown-windows-msvc — same
                                 toolchain as RP1-RP4A, now ALSO proven to
                                 resolve and build the new swift-crypto
                                 (3.15.1) + swift-asn1 (1.7.2) package
                                 dependencies cleanly (461 Crypto/ASN1
                                 source files compiled, 113s). Go (backend
                                 module) — unchanged from prior sessions.
Device (Tier C only):           N/A
Commands actually executed:
  Backend unit/integration:     `cd backend && go build ./...` (clean, run
                                 repeatedly through the session),
                                 `go vet ./...` (clean), `gofmt -w` on
                                 every new/modified Go file, `go test
                                 ./internal/spatial/... -count=1` (run
                                 multiple times — after the ClientCaptureID
                                 domain/repository/service changes, again
                                 after the new idempotency test suite, and
                                 a final full pass), `go test
                                 ./internal/platform/composition/...
                                 -count=1` (drift-guard cross-check), `go
                                 run ./cmd/openapi -out
                                 ../apps/web/openapi/openapi.json`
                                 (regenerated, confirmed clientCaptureId
                                 present via grep), `cd apps/web && npm run
                                 openapi:generate` then `npx tsc --noEmit`
                                 (clean).
  Swift package/build (Tier A): `swift package resolve` (after adding
                                 swift-crypto to Package.swift — resolved
                                 cleanly, 3.15.1 + swift-asn1 1.7.2), `swift
                                 build` (run repeatedly through the
                                 session, including the first build after
                                 adding the dependency — 461 Crypto/ASN1
                                 files + full RenovexCaptureCore, 113s
                                 clean), `swift test` (run repeatedly,
                                 zero regression to the pre-existing 167 at
                                 every checkpoint), and a final full-suite
                                 pass confirming 192/192.
  Swift build/XCTest (Tier B):  NOT RUN — no Mac/Xcode access this
                                 session, unchanged from every prior RP
                                 task. This slice touched no file in
                                 RenovexCaptureRoomPlan or
                                 RenovexCaptureApp. Xcode's own package
                                 resolution/build of swift-crypto has NOT
                                 been verified — flagged as a specific item
                                 to check on first Xcode session (Windows
                                 SwiftPM resolving/building a dependency
                                 cleanly is a strong signal but not a
                                 substitute for an actual Xcode build).
  RoomPlan/LiDAR case IDs run:  none (Tier C, N/A — this slice is
                                 domain/repository/networking foundation
                                 only, no capture or UI surface)
  Other:                        One-off `go run` of a throwaway script
                                 (not committed — this is a no-Git
                                 environment) computing SHA-256 golden
                                 vectors via Go's own crypto/sha256 +
                                 encoding/hex for cross-language checksum
                                 parity tests; deleted immediately after
                                 use.
Tests/counts (if available):    Backend: 131 tests passing (up from 124
                                 pre-session) — 7 new service-level
                                 StartCapture idempotency tests
                                 (service_test.go) + 3 new real-MongoDB
                                 tests (repository_mongo_test.go),
                                 including a 10-concurrent-writer race test
                                 proving exactly one document survives.
                                 Zero regression. iOS: 192 tests passing
                                 (up from 167 pre-session) — 6 new
                                 ChecksumUtilityTests (cross-language
                                 SHA-256 parity), 7 new
                                 CaptureCompletionCoordinatorTests
                                 additions (sync recording, checksum-
                                 matches-persisted-file, retry
                                 recoverability, no-duplication,
                                 isolation), 12 new
                                 ArtifactSyncServiceTests (offline
                                 completion, restart survival, resume,
                                 expired-token-is-normal, retry, finalize
                                 retry, duplicate prevention, stable
                                 identity, isolation, missing-optional-
                                 USDZ, missing-source-file, server-backed
                                 reopen foundation). Zero regression.
Pass/fail:                      PASS across both platforms, reproduced
                                 multiple times through the session plus a
                                 final full-suite run of each after all
                                 changes were complete.
Failures and fixes:
  1. Go: none on first or near-first attempt for every file — the
     ClientCaptureID idempotency design (locked in via explicit user
     confirmation before implementation, including the exact partial-
     index filter expression and the "index is the authoritative race
     guard" requirement) translated directly into working code following
     the existing awards/lineclaim_repository_mongo.go duplicate-key
     pattern.
  2. Swift: Package.swift manifest error on first `swift package resolve`
     attempt — "argument 'products' must precede argument 'dependencies'"
     (a Swift Package Manager argument-ORDER requirement for the Package()
     initializer that has nothing to do with swift-crypto itself). Fixed
     by reordering `products:` before `dependencies:` in the Package()
     call. Caught immediately by the compiler, not a runtime/test issue.
  3. Swift: a GENUINE serialize-once correctness bug, found by an
     initially-flaky test (test_completeCapture_roomDraftJSONChecksumMatchesActualEncodedBytes,
     since renamed). The bug: CaptureCompletionCoordinator computed the
     roomdraft_json checksum from JSONEncoder().encode(draft) without
     persisting those exact bytes anywhere; the test independently
     re-encoded the same persisted draft and asserted the checksums
     matched, which failed non-deterministically because JSONEncoder's key
     ordering is not guaranteed byte-identical across separate encode
     calls for the same logical Codable value. This was a real design gap
     (a future ArtifactSyncService re-encoding the draft to upload it could
     have produced bytes that failed the backend's own checksum
     verification), not merely a flaky test to patch around. Fixed per
     explicit user instruction: the draft is now encoded exactly ONCE per
     completeCapture call, those exact bytes are immediately written to an
     immutable snapshot file, and checksum/size are computed from that
     SAME file's contents — never a value re-encoded later, and
     ArtifactSyncService uploads that file verbatim. The corrected test
     reads back the actual persisted snapshot file rather than
     re-encoding independently, and is no longer flaky (confirmed by
     multiple repeated `swift test` runs in this session).
  4. Swift: two Package.swift-adjacent parameter-ordering/API-shape
     details in ArtifactSyncService's first draft required one edit-and-
     rebuild cycle each (a guard-let/switch pattern needing a minor
     restructure) — resolved during normal build-driven development, not
     flagged as a Swift-toolchain-specific issue like RP3's SIL crash or
     RP3.5's own item 2 above.
Fixtures/regressions added:     Go: no new named fixture files — the new
                                 idempotency tests construct SpatialCapture
                                 values inline via the existing
                                 newTestService()/setupDB() helper
                                 conventions, matching repository_mongo_test.go's
                                 established style. Swift: no new named
                                 fixture objects — ArtifactSyncServiceTests
                                 uses MockURLProtocol (the existing RP1
                                 mocking infrastructure) with small inline
                                 helper methods (makeRun/makePendingUpload/
                                 captureDTOBody/etc.) following
                                 RenovexAPIClientTests.swift's established
                                 pattern exactly.
What remains Apple-specific
and unverified:                 Entirely unchanged from the RP4A entry
                                 above — this slice touched zero files in
                                 RenovexCaptureRoomPlan or
                                 RenovexCaptureApp. RP2-MAC-001 through
                                 RP2-MAC-016 remain exactly as listed
                                 there. ONE NEW item to verify on first
                                 Xcode session, not present before this
                                 slice: confirm swift-crypto resolves and
                                 builds correctly under Xcode's own package
                                 resolution (SPM behavior can differ
                                 subtly between the command-line toolchain
                                 used here and Xcode's integrated
                                 resolver/build system) — this is a
                                 REAL-BUILD verification item, not merely
                                 a repeat of the Windows result.
```

**RP3.5/RP4B0 status-vocabulary application:** `RenovexCaptureCore`/
`RenovexCaptureCoreTests` remain WINDOWS VERIFIED, now at 192/192 (up from
167/167), including a new external package dependency (swift-crypto)
confirmed to resolve and build cleanly on Windows before any code was
written against it — per explicit user instruction to verify this before
proceeding further. The Go backend's deltas are fully verified in this
same session (131 tests, up from 124), independent of any Apple toolchain,
including a real 10-concurrent-writer MongoDB race test for the new
partial-unique-index idempotency guarantee. Because this slice touched
zero files in `RenovexCaptureRoomPlan`/`RenovexCaptureApp`, their
Xcode-verification status is unchanged — this is the SECOND consecutive RP
task where 100% of the session's new code is Windows-verified with no
Mac-pending residue. Do not read "192/192 passing" as implying artifacts
actually reach a real backend over a real network, or that any UI exists
to trigger sync — this slice is the durable synchronization SUBSTRATE
(protocol-backed store, real HTTP client methods, retry/resume logic, all
proven against `MockURLProtocol`), not an end-to-end device-to-server
integration test against a running backend, and not any UI wiring
(`ScanRoomView`/`SpaceDetailView` were not modified this session — that
remains open follow-up work if/when this substrate needs a visible trigger
point before RP4B's interactive editor is ready).

---

```text
Date:                           2026-09-04 (same day as RP4A/RP3.5/RP4B0)
Implementation task(s):         RP4B — Backend EditOperation Persistence,
                                 Revisioning, and Conflict Transport.
                                 Explicitly scoped by the user to
                                 BACKEND-ONLY — the authoritative HTTP
                                 transport/persistence layer for RP4A's 27
                                 EditOperations against a server-backed
                                 RoomDraft, NOT the interactive Web/iOS
                                 2D/3D editor. RP1-RP4A/RP3.5/RP4B0 were
                                 NOT reopened (no defect found in any of
                                 them; RP4A's CAS/revision model was
                                 confirmed already correct and reused
                                 as-is, not modified).
Tier:                           A — Windows, backend Go ONLY
                                 (`internal/spatial`,
                                 `internal/platform/composition`). RP4B
                                 touched ZERO iOS files — not even
                                 RenovexCaptureCore. This is the first RP
                                 task with no iOS component at all.
Environment:                    Windows 11. Go (backend module) —
                                 unchanged from prior sessions. MongoDB via
                                 testcontainers-go (mongo:7, replica set
                                 rs0, real multi-document transactions).
Device (Tier C only):           N/A
Commands actually executed:
  Backend unit/integration:     `cd backend && go build ./...` (clean, run
                                 repeatedly through the session — after
                                 the domain type, after the repository
                                 interfaces, after the Mongo repository,
                                 after the service methods, after the
                                 handler, after composition wiring, and a
                                 final full pass), `go vet ./...` (clean,
                                 same cadence), `gofmt -w` on every new/
                                 modified Go file, `go test
                                 ./internal/spatial/... -run
                                 "TestSubmitEditOperation|TestSubmitResetToScan|TestListRoomDraftEdits"
                                 -v -count=1` (17 service-level tests, run
                                 multiple times), `go test
                                 ./internal/spatial/... -run
                                 "TestMongoRoomDraftEditRepository" -v
                                 -count=1` (6 real-Mongo tests, run FOUR
                                 times total across the debugging session
                                 below — 2 initial failures, a partial fix
                                 leaving 1 failure, then the real fix
                                 confirmed clean), `go test
                                 ./internal/spatial/... -count=1` (full
                                 package regression, 66.4s, clean), `go
                                 test ./internal/platform/composition/...
                                 -count=1` (drift-guard cross-check,
                                 clean), `go build ./...`/`go vet ./...`
                                 across the WHOLE backend module (not just
                                 internal/spatial) as a final sanity check,
                                 `go run ./cmd/openapi -out
                                 ../apps/web/openapi/openapi.json`
                                 (regenerated, confirmed the 3 new
                                 operation IDs present via grep), `cd
                                 apps/web && npm run openapi:generate` then
                                 `npx tsc --noEmit` (clean).
  Swift build/XCTest:           NOT RUN — genuinely not applicable this
                                 session. RP4B touched no Swift file at
                                 all.
  RoomPlan/LiDAR case IDs run:  none (Tier C, N/A — pure backend
                                 persistence/transport work)
  Other:                        Read
                                 go.mongodb.org/mongo-driver/v2/mongo/session.go's
                                 actual WithTransaction source (local
                                 module cache,
                                 go.mongodb.org/mongo-driver/v2@v2.8.0) to
                                 confirm the transient-transaction-error
                                 silent-retry behavior described below,
                                 rather than assuming it from documentation
                                 alone.
Tests/counts (if available):    154 backend tests passing total (up from
                                 131 pre-RP4B) — 17 new service-level
                                 tests (roomdraftedit_service_test.go) + 6
                                 new real-Mongo tests
                                 (roomdraftedit_repository_mongo_test.go).
                                 Zero regression across the full
                                 internal/spatial package (154 tests) and
                                 internal/platform/composition (drift
                                 guard).
Pass/fail:                      PASS — but only after fixing a genuine bug
                                 found DURING this session's own test run,
                                 not after the fact. See "Failures and
                                 fixes" below for the full sequence; final
                                 state is 6/6 real-Mongo tests and 17/17
                                 service-level tests passing, confirmed
                                 with a full-package regression run after
                                 the fix.
Failures and fixes:
  1. Go: none for the domain type, repository interfaces, wire decode
     dispatcher, Service methods, or HTTP handler — each compiled and the
     service-level (fake-repository) tests passed on first attempt,
     because the fake repository was deliberately written to mirror the
     REAL Mongo repository's intended semantics, which is exactly what
     let the real Mongo tests below catch a bug the fake could not (a fake
     cannot reproduce actual MongoDB driver transaction-retry behavior).
  2. Go/MongoDB: a GENUINE, non-obvious driver-behavior bug, found only
     once real-Mongo tests ran. First real-Mongo test run: 5/6 passed,
     but TestMongoRoomDraftEditRepository_ApplyAndRecord_DuplicateOperationID_AdoptsExisting
     failed with "unexpected error on retry: spatial: room draft changed
     since it was read" and
     TestMongoRoomDraftEditRepository_ApplyAndRecord_ConflictingFingerprint_ReturnsConflict
     failed with "expected ErrOperationIDConflict, got spatial: room draft
     changed since it was read" — both nonsensical under the original
     design (catch mongo.IsDuplicateKeyError AFTER WithTransaction
     returns). Root cause, confirmed by reading the driver source directly:
     session.WithTransaction's retry loop contains
     `if errorHasLabel(err, driver.TransientTransactionError) { continue }`
     — it SILENTLY RE-RUNS THE ENTIRE CALLBACK when the transaction error
     is labeled transient, and a duplicate-key violation (E11000) inside
     an active transaction on a replica set commonly carries exactly that
     label. The post-transaction duplicate-key catch never executed
     because the driver retried the callback FIRST, and on that retry the
     original expectedRevision (now stale, since a concurrent/prior
     transaction had already committed) failed the CAS filter cleanly,
     surfacing ErrRoomDraftRevisionMismatch instead of the intended
     adoption/conflict resolution. Fixed exactly per explicit user
     design — NOT manual transaction lifecycle management, which was
     explicitly evaluated and rejected as "reimplementing the driver's own
     retry machinery" for one behavior: moved the FindByOperationID
     idempotency check to be the FIRST statement inside the
     WithTransaction callback itself (before any RoomDraft read/mutation),
     making the callback body itself safe to silently replay for ANY
     reason the driver chooses to replay it. Re-ran the real-Mongo tests
     after this fix: 5/6 passed (the true-concurrent-race duplicate test
     now passed), but
     TestMongoRoomDraftEditRepository_ApplyAndRecord_ConflictingFingerprint_ReturnsConflict
     STILL failed, now with "expected ErrOperationIDConflict, got <nil>"
     (a silent, undetected double-apply) — a SECOND, separate bug: the
     test itself never populated RoomDraftEditRecord.OperationFingerprint
     on either of its two constructed records, so both defaulted to the
     empty string and compared as equal ("" != "" is false) regardless of
     the operations' actual different content, defeating the fingerprint
     check by test construction, not implementation. Fixed by adding an
     exported test-only ComputeOperationFingerprintForTest helper
     (backend/internal/spatial/roomdraftedit.go, matching
     config.NewAllowedOriginsForTest's existing "exported test-only
     helper" convention already used elsewhere in this codebase) and a
     shared newTestEditRecord test constructor
     (roomdraftedit_repository_mongo_test.go) that ALWAYS computes a real
     fingerprint, then refactored every record construction in that test
     file to use it consistently rather than hand-rolling
     RoomDraftEditRecord literals with silently-empty fingerprints. Final
     re-run: 6/6 real-Mongo tests passing, confirmed with a full
     internal/spatial package regression run afterward (154/154).
  3. Go: the true-concurrent-race version of the duplicate-OperationID
     test required rewriting from "two SEQUENTIAL calls, second one
     deliberately given a stale expectedRevision to try to trigger a
     duplicate-key path" (which is not what ApplyAndRecord's actual
     contract produces — a sequential retry with a stale revision
     correctly fails CAS first, exactly as intended, since
     Service.SubmitEditOperation's own FindByOperationID pre-check is what
     handles the sequential-retry case in production, not
     ApplyAndRecord's duplicate-key path) to "two REAL GOROUTINES racing
     from the SAME base revision with the SAME OperationID/fingerprint,
     synchronized via sync.WaitGroup" — the genuine scenario
     ApplyAndRecord's duplicate-key/adoption logic exists to handle. This
     was a test-design correction, not an implementation change.
Fixtures/regressions added:     No new named fixture files. Both new test
                                 files (roomdraftedit_service_test.go,
                                 roomdraftedit_repository_mongo_test.go)
                                 construct RoomDraft/RoomDraftEditRecord
                                 values inline via small helper functions
                                 (draftWithOneWall-style patterns already
                                 established in editoperation_test.go for
                                 the service-level file; newTestRoomDraft/
                                 newTestEditRecord for the Mongo-level
                                 file), matching this package's existing
                                 test-construction conventions exactly.
What remains Apple-specific
and unverified:                 Entirely unchanged from the RP3.5/RP4B0
                                 entry above — RP4B touched zero iOS files
                                 of any kind, Core included. RP2-MAC-001
                                 through RP2-MAC-016 remain exactly as
                                 listed there. No new Apple-specific items
                                 introduced.
```

**RP4B status-vocabulary application:** Backend `internal/spatial` is
VERIFIED at 154 tests passing (up from 131), including 6 real-Mongo tests
proving actual multi-document transaction atomicity, a true 10-writer CAS
race, and a true 2-writer duplicate-idempotency-key race — all against
real MongoDB via testcontainers-go, not simulated. `internal/platform/composition`'s
drift-guard tests confirm the new repository wiring
(`spatialRoomDraftEditRepo`/`SetRoomDraftEditSupport`) is correct.
Because RP4B touched no iOS file whatsoever, this tier vocabulary does not
apply to it at all in the iOS dimension — there is nothing to mark NOT YET
XCODE-VERIFIED that wasn't already in that state from RP3.5/RP4B0. The
most notable outcome of this session is not a feature count but a real,
subtle MongoDB driver-behavior bug (`session.WithTransaction`'s silent
whole-callback retry on transient errors, interacting badly with a
duplicate-key-based idempotency design) found and fixed with evidence from
the driver's own source, not guessed at or worked around superficially —
recorded here in detail specifically so a future session touching any
other `WithTransaction`-based repository in this codebase (there is
exactly one other precedent, `supplieroffers/eligibility_repository_mongo.go`)
knows to design the callback body itself as idempotent-under-replay from
the start, rather than relying on catching a specific error class after
the transaction returns.

---

```text
Date:                           2026-09-04 (same day, post-completion
                                 review follow-up to RP4B above).
Implementation task(s):         RP4B correctness review fix — user flagged,
                                 not self-caught: Service.SubmitEditOperation
                                 and Service.SubmitResetToScan, as
                                 originally landed, each independently
                                 implemented the OperationID idempotency
                                 lookup, fingerprint comparison,
                                 RoomDraftEditRecord construction, and
                                 ApplyAndRecord call, converging only at
                                 that final shared call — not the single
                                 shared mutation pipeline the user required
                                 ("The reset endpoint is either a thin
                                 adapter ... or otherwise shares exactly
                                 the same revision/CAS/idempotency/audit
                                 implementation. There must be no
                                 independent RoomDraft mutation path.").
                                 Fixed by extracting one private method,
                                 Service.submitAgainstRoomDraft(ctx,
                                 companyID, roomDraftMutationRequest),
                                 parameterized by a
                                 `Transform func(RoomDraft) (RoomDraft,
                                 error)` closure plus the operation's
                                 Kind/Payload/OperationID/ExpectedRevision/
                                 ActorUserID. submitAgainstRoomDraft alone
                                 now owns: OperationID lookup, fingerprint
                                 computation/comparison, existing-draft
                                 fetch, calling Transform,
                                 RoomDraftEditRecord construction, and the
                                 ApplyAndRecord call/retry-adoption
                                 handling. SubmitEditOperation is now
                                 exactly: decode the wire EditOperation,
                                 call op.Validate(), then call
                                 submitAgainstRoomDraft with
                                 Transform: op.Apply. SubmitResetToScan is
                                 now exactly: build a resetTransform
                                 closure (baseline restore logic, unchanged
                                 from before), then call
                                 submitAgainstRoomDraft with
                                 Kind: "reset_to_scan", Payload: "". Per
                                 explicit user instruction, RP4A's 27-
                                 operation EditOperation vocabulary was
                                 NOT extended with a 28th
                                 "reset_to_scan"-as-EditOperation member
                                 solely to remove this duplication —
                                 reset-to-scan remains semantically
                                 distinct from the EditOperation interface
                                 (no Validate()/Apply() method set), sharing
                                 only the underlying mutation pipeline.
Tier:                           A — Windows, backend Go ONLY
                                 (`internal/spatial/service.go`). No new
                                 files; no repository/handler/wire-format
                                 changes — RoomDraftEditRecord's shape,
                                 the HTTP endpoints, and the Mongo
                                 repository's CAS/duplicate-key logic are
                                 all unchanged from the RP4B entry above.
Environment:                    Windows 11. Same Go/MongoDB toolchain as
                                 RP4B above — unchanged.
Commands actually executed:     `gofmt -w` on service.go, `go build ./...`
                                 (clean), `go vet ./...` (clean), `go test
                                 ./internal/spatial/... -run
                                 "TestSubmitEditOperation|TestSubmitResetToScan|TestListRoomDraftEdits"
                                 -v -count=1` (17/17 passing, unchanged
                                 outcomes from before the refactor — same
                                 assertions, same pass/fail per test, only
                                 the implementation under test changed),
                                 `go test ./internal/spatial/... -count=1`
                                 (full package regression, 75.172s,
                                 154/154 passing — the same 154 tests as
                                 the RP4B entry above, zero count change,
                                 confirming the refactor is behavior-
                                 preserving rather than adding or removing
                                 coverage).
Tests/counts (if available):    154 backend tests passing (unchanged count
                                 from RP4B above — this was a pure internal
                                 refactor of already-tested behavior, not
                                 new functionality, so no new tests were
                                 added; the existing 17 service-level tests
                                 already exercised both SubmitEditOperation
                                 and SubmitResetToScan's observable
                                 behavior and continued to pass unchanged
                                 against the new shared implementation).
Pass/fail:                      PASS — no new bugs found during this
                                 refactor; it was a structural extraction
                                 of already-correct, already-tested logic,
                                 not a behavior change.
Failures and fixes:             None. This was a mechanical extraction
                                 verified by full regression, not a bug
                                 fix.
Fixtures/regressions added:     None.
What remains Apple-specific
and unverified:                 Unchanged from RP4B above — this follow-up
                                 touched zero iOS files.
```

**Post-RP4B review fix status-vocabulary application:** `Service.SubmitEditOperation`
and `Service.SubmitResetToScan` now share exactly one internal mutation
pipeline (`submitAgainstRoomDraft`) with zero duplicated
idempotency/CAS/audit logic between them — there is no independent
RoomDraft mutation path anywhere in `internal/spatial`. This closes the
gap the user identified between "one persistence/mutation implementation"
(already true after RP4B, since both methods bottomed out in the same
`ApplyAndRecord`) and "one full submission pipeline" (not true until this
fix, since the surrounding idempotency-check/record-construction logic was
duplicated). Backend `internal/spatial` remains VERIFIED at 154/154 tests
passing, confirming the extraction changed structure only, not behavior.

---

```text
Date:                           2026-09-05
Implementation task(s):         RP4C1 — Web Editor Foundation +
                                 Authoritative 2D Editing Vertical Slice.
                                 First slice of the Web editor consuming
                                 RP4B's transport: load a real
                                 server-backed RoomDraft, render an
                                 interactive 2D floor plan, select
                                 elements, submit a small representative
                                 subset of canonical RP4A EditOperations
                                 (move_corner, reclassify_opening,
                                 add_service_point, move_service_point),
                                 reconcile to the authoritative server
                                 result, handle conflicts/failures
                                 distinctly. Two small additive backend
                                 prerequisites only (GET route + 409
                                 discriminator) — no new backend domain,
                                 persistence, mutation, revision, or
                                 service-layer behavior. NOT the full 2D/3D
                                 editor.
Tier:                           A — Windows, backend Go
                                 (`internal/spatial/handler.go` only) +
                                 Web/TypeScript (`apps/web`). Zero iOS
                                 files touched — second RP task after RP4B
                                 with no iOS component at all.
Environment:                    Windows 11. Go backend unchanged. Web:
                                 Next.js 16.2.11 (Turbopack), Vitest
                                 4.1.10, React Testing Library, MSW.
                                 Manual verification additionally used:
                                 Docker Compose (mongo:7 replica set +
                                 Mailpit), the real `go run ./cmd/api`
                                 server, the real `npm run dev` Next.js
                                 server, and Chrome DevTools MCP for an
                                 actual browser click-through.
Commands actually executed:
  Backend:                      `go build ./...`, `go vet ./...`, `gofmt
                                 -w` on handler.go/new test file, `go test
                                 ./internal/spatial/... -run
                                 "TestGetRoomDraft|TestMapSpatialError" -v
                                 -count=1` (5 new tests), `go test
                                 ./internal/spatial/... -count=1` (full
                                 package regression, 159/159), `go test
                                 ./internal/platform/composition/...
                                 -count=1` (drift guard, unaffected — no
                                 new repository/wiring), `go run
                                 ./cmd/openapi -out
                                 ../apps/web/openapi/openapi.json`
                                 (regenerated, confirmed
                                 spatial-room-draft-get present).
  Web:                          `npm run openapi:generate`, `npx tsc
                                 --noEmit` (clean, run after every new
                                 file and after each bugfix), `npx eslint
                                 src/features/spatial/` (clean, run
                                 repeatedly — caught 2 genuine React
                                 Compiler ref-during-render violations),
                                 `npx vitest run src/features/spatial/`
                                 (71/71, run after every new test file and
                                 after each bugfix), `npx vitest run`
                                 (full app suite, 500/500 — one
                                 pre-existing unrelated flake in
                                 ProjectProcurement.test.tsx, confirmed
                                 passing 52/52 in isolation and again
                                 clean on a second full-suite run).
  Manual verification:          `docker compose up -d` (mongo + mailpit,
                                 healthy after ~15s), `go run ./cmd/api`
                                 (background), `demoseed reset` then
                                 `demoseed seed` (fresh demo tenant,
                                 6 seeded projects/spaces), a real
                                 `POST /auth/login` + `POST
                                 /spatial/captures` via curl to create a
                                 genuine capture, a temporary one-off Go
                                 program (`backend/cmd/seedroomdraft_temp`,
                                 deleted immediately after use — calls
                                 `spatial.Service.PersistRoomDraft`
                                 directly, not hand-built BSON) to attach
                                 a 4-wall/2-opening RoomDraft to that
                                 capture, `npm run dev` (background),
                                 Chrome DevTools MCP: login, navigate to
                                 the space, click "Open floor plan",
                                 select/reclassify the door opening
                                 (revision 0→1), click "Add service point"
                                 (1→2), drag the service point twice
                                 (→3, →5), drag a shared wall corner
                                 (5→6) — all verified via
                                 list_network_requests/get_network_request
                                 showing the exact request/response
                                 bodies, not just visual inspection. All
                                 Docker containers and background
                                 processes stopped and removed after
                                 verification, per explicit user
                                 instruction ("stop everything").
Tests/counts (if available):    159 backend tests passing (up from 154,
                                 +5 for the GET route/409-discriminator
                                 addenda). 500 Web tests passing (up from
                                 ~429, +71 for RP4C1 across 8 new/modified
                                 test files: coordinates.test.ts (11),
                                 operations.test.ts (9),
                                 useEditorState.test.tsx (12),
                                 EditStatusBanner.test.tsx (10),
                                 FloorPlanViewport.test.tsx (6),
                                 ElementInspector.test.tsx (7),
                                 SpatialEditor.test.tsx (12), plus
                                 errors.test.ts fixture updates for the
                                 new `code` field). Zero regressions
                                 elsewhere.
Pass/fail:                      PASS — but only after finding and fixing
                                 two genuine bugs during this slice's OWN
                                 manual verification (not caught by
                                 automated tests, which mock the DOM/HTTP
                                 layer and can't catch real-layout or
                                 real-React-Compiler issues). See
                                 "Failures and fixes" below.
Failures and fixes:
  1. TypeScript/React: a design mistake, not caught until `npx eslint`
     was run (React Compiler's `react-hooks/refs` rule) — `SpatialEditor.tsx`
     read `lastError.current` (a `useRef`) directly in JSX to decide what
     to render, and an earlier `FloorPlanViewport.tsx` draft read
     `svgRef.current` from a plain component-body helper function invoked
     by event handlers. Both are refs read in a way the React Compiler's
     static analysis treats as "during render," which can produce stale
     UI since ref writes don't trigger re-renders. Fixed by (a) converting
     `lastError` to `useState` in `SpatialEditor.tsx`, and (b) removing
     `FloorPlanViewport`'s stored `svgRef` entirely — the SVG's
     bounding-rect is now read only from inside the actual pointer
     event-handler functions via `event.currentTarget.closest("svg")`,
     which always resolves correctly because every drag-capable child
     element (corner handle, service point) is a direct child of the
     `<svg>`. An intermediate design was tried first — a child's
     `onPointerDown` calling `setPendingDragTarget(target)` without
     `stopPropagation()`, relying on the event bubbling up to the SVG's
     own `onPointerDown` to read the rect and consume the pending target —
     and hit a SECOND, more subtle bug: React batches state updates within
     one synthetic-event dispatch, so the SVG's handler running in the
     SAME dispatch as the child's `setPendingDragTarget` call still saw
     the OLD (pre-update) `pendingDragTarget` value via closure, so drags
     silently did nothing. Confirmed via `npx vitest run
     src/features/spatial/editor/components/SpatialEditor.test.tsx`
     regressing from 12/12 to 10/12 (both drag-based tests failing) when
     this intermediate design was in place — reverted to the direct
     `event.currentTarget.closest("svg")` approach, which passed 12/12
     again. `jsdom` also doesn't implement `ResizeObserver` (needed for
     fix #2 below) — added a no-op stub to `vitest.setup.ts`, the first
     Web component in this codebase to need one.
  2. Web/React, found ONLY during manual browser verification (all
     automated tests passed throughout — MSW mocks a fixed container size
     implicitly via jsdom's default layout, which never exposed this):
     fit-to-room framing in `SpatialEditor.tsx` called `fitToRoom(walls,
     {width:800,height:600})` — a hardcoded guess, not the actual
     container size. In the real browser, the floor-plan SVG's real
     rendered size (an inspector panel narrows it, ~830×382px, a very
     different aspect ratio than 800×600) meant the fit computed a zoom
     factor from the wrong dimensions, placing two of the four walls
     (north/south) off-screen above and below the visible viewport.
     Confirmed via a screenshot showing only 2 vertical wall lines instead
     of a full rectangle, then diagnosed precisely by reading the actual
     DOM `<line>` element coordinates (`y=-64`, `y=446` against a
     ~382px-tall container) and manually recomputing what `fitToRoom`
     SHOULD produce for the real measured size in Node, confirming the
     math itself was correct — only the input container size was wrong.
     Fixed by moving fit-to-room ownership from `SpatialEditor` into
     `FloorPlanViewport` itself (the only place that actually knows the
     SVG's real size), driven by a `ResizeObserver` rather than a
     one-shot `ref` callback (a plain ref callback fires once at mount
     with whatever size exists at that instant, which can be smaller than
     the final settled layout if sibling elements like the inspector
     panel haven't finished laying out yet). The auto-fit effect re-runs
     on every `ResizeObserver`-reported size change until the user has
     genuinely taken control of the viewport (tracked via a
     `hasUserInteracted` ref set on pan-start/wheel-zoom, deliberately
     NOT on corner/service-point drags, which are targeted element edits
     rather than "I want to control the camera" gestures) — so even if
     the FIRST fit uses a still-transient size, a subsequent
     `ResizeObserver` firing corrects it automatically. Confirmed fixed
     by reloading in the browser and re-running the full manual
     click-through: all 4 walls visible as a proper rectangle, all 4
     operations round-tripped correctly with revision progressing 0→6,
     `move_corner`'s coincident-endpoint discovery correctly submitting
     both `{wall_north,start}` and `{wall_west,end}` from one drag on a
     single handle.
Fixtures/regressions added:     `vitest.setup.ts` gained a no-op
                                 `ResizeObserver` stub (jsdom doesn't
                                 implement one). `errors.test.ts`'s two
                                 exact-match fixtures updated for the new
                                 `code` field on `ApiError`. No other
                                 shared test infrastructure changed.
What remains Apple-specific
and unverified:                 Entirely unchanged from RP4B above — RP4C1
                                 touched zero iOS files of any kind.
                                 RP2-MAC-001 through RP2-MAC-016 remain
                                 exactly as listed there.
```

**RP4C1 status-vocabulary application:** Backend `internal/spatial` is
VERIFIED at 159 tests passing (up from 154), Web is VERIFIED at 500 tests
passing (up from ~429) — both including genuine manual browser
verification against a real backend/MongoDB, not just mocked automated
tests. This is the first RP4-series slice with a real end-to-end manual
click-through recorded: real capture created via the actual HTTP API, a
real RoomDraft attached via the actual `Service.PersistRoomDraft` code path
(a temporary, deleted-after-use seeder, never hand-built BSON), and all 4
supported operations exercised through Chrome DevTools MCP with network
request/response bodies inspected directly, not just visual screenshots.
Two genuine bugs were found and fixed specifically BECAUSE manual
verification was performed — both were invisible to the automated test
suite (a React Compiler static-analysis violation that only `eslint`
catches, and a real-layout sizing bug that MSW/jsdom's fixed test
environment never exposes) — reinforcing that automated coverage and
manual verification catch different classes of defects for UI work
specifically. Because RP4C1 touched no iOS file whatsoever, this tier
vocabulary does not apply to it in the iOS dimension — nothing changed
from RP4B's NOT YET XCODE-VERIFIED status.

```text
Date:                           2026-09-05
Implementation task(s):         RP4C2 — Complete 2D Editing Interactions +
                                 Fixtures/Service-Points/Constraints Editors
                                 + Reset-to-Scan UX. Extends RP4C1's proven
                                 authoritative loop from 4 to 23 of 30
                                 canonical RP4A EditOperations (move_wall,
                                 set_wall_thickness, add/remove/move/resize_
                                 opening, all 4 door-specific operations,
                                 full fixture create/move/resize/reclassify/
                                 remove, full constraint create/move/
                                 remove, plus Reset-to-Scan via the
                                 dedicated RP4B reset route). Two small
                                 additive backend prerequisites, both
                                 user-approved after being escalated as
                                 genuine scope gaps rather than worked
                                 around: (1) 3 new canonical remove
                                 operations for fixtures/service-points/
                                 constraints, added identically to Go and
                                 Swift (operation count 27->30); (2) a
                                 derived `canResetToScan` boolean on
                                 `roomDraftDTO`. No new backend persistence,
                                 mutation, revision, or service-layer
                                 behavior beyond those two additions. Also
                                 fixed two navigation/UX bugs found by the
                                 user testing the built feature afterward
                                 (previous scans had no way back into the
                                 editor; a scan with no RoomDraft yet showed
                                 no feedback when clicked).
Tier:                           A — Windows, backend Go
                                 (`internal/spatial/editoperation.go`,
                                 `roomdraftedit.go`, `handler.go` + their
                                 test files) + the Swift MIRROR of the 3 new
                                 remove operations
                                 (`EditOperation.swift` + tests — wire-parity
                                 and local validate/apply tests only, no
                                 RenovexCaptureApp/RoomPlan file touched) +
                                 Web/TypeScript (`apps/web`).
Environment:                    Windows 11. Go backend unchanged toolchain.
                                 Web: Next.js 16.2.11 (Turbopack), Vitest
                                 4.1.10, React Testing Library, MSW, sonner
                                 (existing dependency, reused for the
                                 no-roomDraftId toast). Manual verification
                                 additionally used: Docker Compose (mongo:7
                                 replica set + Mailpit), the real `go run
                                 ./cmd/api` server, the real `npm run dev`
                                 Next.js server, Chrome DevTools MCP for
                                 real browser click-throughs (two separate
                                 sessions — one for the RP4C2 operation/
                                 Reset-to-Scan verification, one afterward
                                 for the two navigation-bug fixes, since the
                                 MCP connection dropped and was restarted
                                 between them).
Commands actually executed:
  Backend:                      `go build ./...`, `go vet ./...`, `go test
                                 ./internal/spatial/... -count=1` (full
                                 package regression, 171/171, up from 159),
                                 `go test ./internal/platform/composition/...
                                 -count=1` (drift guard, unaffected). A
                                 later full `go test ./...` run (unrelated
                                 to spatial, run as general due diligence)
                                 hit a `testcontainers-go` timeout in
                                 `internal/tenanttest` because Docker
                                 Desktop had been stopped/restarted
                                 concurrently for UI verification in the
                                 same window — confirmed environmental, not
                                 a regression, by restarting Docker Desktop
                                 and re-running `internal/tenanttest` in
                                 isolation with no other Docker operation
                                 concurrent.
  Web:                          `npx tsc --noEmit` (clean, run after every
                                 new/modified file and after each bugfix),
                                 `npx eslint src/features/spatial/` (clean),
                                 `npx vitest run src/features/spatial/`
                                 (134/134, up from RP4C1's 71 — includes 2
                                 new tests added post-manual-verification
                                 for the navigation fixes), `npx vitest run`
                                 (full app suite: 561/563 on the run used
                                 for final counts; 2 failures were
                                 CostsView.test.tsx and
                                 ProjectProcurement.test.tsx on two
                                 separate isolated full-suite runs —
                                 different file each time, neither touching
                                 spatial code, both confirmed passing when
                                 re-run in isolation — consistent with the
                                 same pre-existing ProjectProcurement flake
                                 already documented in RP4C1's entry).
  Manual verification:          `docker compose up -d`, `go run ./cmd/api`
                                 (background), a temporary one-off Go
                                 program (`backend/cmd/seedroomdraft_temp`,
                                 recreated for this session, deleted again
                                 after use — calls
                                 `spatial.Service.PersistRoomDraft` directly)
                                 seeding a 4-wall room with a door (width/
                                 height set, to exercise door-specific
                                 fields) and a window, `npm run dev`
                                 (background), Chrome DevTools MCP:
                                 round-tripped add_fixture,
                                 reclassify_fixture, set_door_hinge,
                                 move_wall (x2), set_wall_thickness,
                                 add_constraint, remove_constraint,
                                 add_service_point -> remove_service_point,
                                 remove_fixture, remove_opening — each
                                 confirmed via list_network_requests/
                                 get_network_request showing exact request/
                                 response bodies. Reset-to-Scan: confirmed
                                 no request fires before the confirmation
                                 dialog is explicitly confirmed, confirmed
                                 the exact wire shape
                                 ({"operationId":...,"expectedRevision":12},
                                 no kind/payload field), confirmed the
                                 response (revision 12->13, all 4 walls
                                 reverted to their exact original seeded
                                 coordinates — including a discarded
                                 set_wall_thickness override — both
                                 openings restored including one previously
                                 removed via remove_opening,
                                 fixtures/servicePoints/constraints all
                                 null, canResetToScan still true).
                                 Independently confirmed the reset
                                 persisted in real MongoDB (not just the
                                 React Query cache) via a fresh page-load
                                 GET that fired its own /auth/refresh and
                                 returned matching state straight from the
                                 database. Exercised a genuine (not
                                 simulated) overlapping-edit stale-revision
                                 scenario by issuing two real
                                 add_service_point submissions both
                                 carrying expectedRevision:13 from inside
                                 the authenticated page context — first
                                 succeeded (200), second correctly returned
                                 409 with type:"stale_revision". All Docker
                                 containers/background processes stopped,
                                 removed, and the temporary seeder deleted
                                 after this pass. A SECOND manual pass was
                                 run later the same session (fresh Docker
                                 Compose + demoseed reset/seed + a newly
                                 created Test Room/capture, since the first
                                 pass's containers had already been torn
                                 down) specifically to verify the two
                                 navigation-bug fixes: created a space with
                                 no RoomDraft attached (guaranteed
                                 roomDraftId absent), confirmed the "Open
                                 floor plan" control renders as a button
                                 (not a link) and clicking it fires a
                                 sonner toast ("No floor plan yet for this
                                 scan...") with no console errors and no
                                 silent no-op; confirmed a scan WITH a
                                 roomDraftId renders a real navigable link
                                 in both the current- and previous-scan
                                 positions. All containers/processes
                                 stopped and removed again afterward.
Tests/counts (if available):    Backend internal/spatial: 171/171 (up from
                                 159 — 9 new for the 3 remove operations,
                                 3 new for canResetToScan). Swift
                                 RenovexCaptureCoreTests: 198/198 (up from
                                 192 — 6 new local remove-operation tests
                                 plus wire-parity extensions). Web spatial
                                 module: 134/134 (up from RP4C1's 71 across
                                 the same 8 files, now covering all 23
                                 exposed operations plus Reset-to-Scan, +2
                                 in SpatialEntry.test.tsx for the
                                 navigation fixes). Web full suite: 561/563
                                 — 2 unrelated pre-existing flakes (see
                                 above).
Pass/fail:                      PASS — three genuine bugs found and fixed
                                 during this slice's manual verification
                                 and subsequent user testing (not caught by
                                 automated tests): a pre-existing click-vs-
                                 drag bug dating back to RP4C1, plus the two
                                 navigation/UX bugs the user found testing
                                 the shipped feature. See "Failures and
                                 fixes" below.
Failures and fixes:
  1. Pre-existing click-vs-drag bug (present since RP4C1, never caught
     until RP4C2's own test-writing exposed it): a plain click on any
     draggable SVG element (fixture/servicePoint/constraint/wall corner)
     synthesizes pointerdown+pointerup at the same screen location, and
     FloorPlanViewport.tsx's handlePointerUp unconditionally treated any
     completed `drag` state as a real drag, submitting a spurious
     zero-distance move_* operation on every single click-to-select
     action. Surfaced when a new remove_fixture test (select via click,
     then click "Remove fixture") failed — the click-to-select consumed
     the mock's one queued response with an unwanted move_fixture POST,
     leaving nothing for the actual remove_fixture submission. Fixed by
     restructuring `drag` state to track `startWorldPosition` (captured
     once in requestDrag) alongside the continuously-updated
     `worldPosition`, adding a CLICK_VS_DRAG_THRESHOLD_METERS (1mm,
     world-space) check via a new `distanceMeters` helper, and only
     calling `onDragEnd` when the threshold is exceeded. Confirmed fixed
     via a full spatial-suite re-run (132/132 at the time, including every
     pre-existing drag test — all of which move well beyond 1mm and so
     were unaffected).
  2. Manual-verification tooling nuance, not a product bug: computing a
     drag's screen coordinates in one Chrome DevTools MCP evaluate_script
     call and dispatching PointerEvents in a separate call sometimes
     produced no network request at all, because the target's screen
     position had shifted between calls (an inspector-panel re-render
     changing layout). Fixed the VERIFICATION APPROACH (not the app) by
     computing coordinates and dispatching all pointer events within a
     single evaluate_script execution, which then worked reliably for the
     move_wall verification.
  3. Navigation bug #1, user-reported after testing the shipped feature:
     SpatialEntry.tsx rendered "Open floor plan" only for the current room
     scan (a separate header-level Link, not part of the row component) —
     every row in "Previous scans" had no way back into the editor at all
     once you'd navigated away. Fixed by moving the link into the shared
     CaptureRow component, rendered for any capture with a roomDraftId
     (current or previous), and removing the now-redundant header-level
     Link so both cases funnel through one code path. Verified live: a
     previous scan's row gained a working "Open floor plan" link,
     confirmed by navigating into it and back via the existing "Back to
     space" link.
  4. Navigation bug #2, user-reported as an immediate follow-up ("if it
     doesn't exist, there should be an error message"): a capture with no
     roomDraftId yet (not yet processed into a RoomDraft — e.g. a fresh
     "New scan" not yet reviewed on the mobile app) rendered NO affordance
     at all in the "Open floor plan" slot, which reads as broken rather
     than "not ready yet." Fixed by rendering a disabled-style Button (not
     a Link — there is nowhere valid to navigate) that shows a sonner
     toast on click explaining the state, reusing this codebase's existing
     sonner convention (already used elsewhere, e.g.
     ProjectProcurement.tsx) rather than inventing new toast
     infrastructure. Verified live by creating a fresh space and starting
     a new capture (guaranteed no roomDraftId): the button rendered
     correctly (not a link), and clicking it produced the toast with no
     console errors.
Fixtures/regressions added:     2 new tests in SpatialEntry.test.tsx (both
                                 current- and previous-scan links present
                                 when roomDraftId exists; a roomDraftId-less
                                 scan renders a button, not a link, and
                                 clicking it does not throw). No shared test
                                 infrastructure changed.
What remains Apple-specific
and unverified:                 Entirely unchanged from RP4C1/RP4B above —
                                 RP4C2 touched only the wire-parity/local-
                                 test layer of the Swift mirror
                                 (EditOperation.swift for the 3 new remove
                                 operations), not RenovexCaptureApp or
                                 RoomPlanCaptureAdapter. RP2-MAC-001 through
                                 RP2-MAC-016 remain exactly as listed there.
```

**RP4C2 status-vocabulary application:** Backend `internal/spatial` is
VERIFIED at 171 tests passing (up from 159), Swift
`RenovexCaptureCoreTests` is WINDOWS VERIFIED at 198 tests passing (up from
192 — wire-parity and local validate/apply tests for the 3 new remove
operations, not an App/RoomPlan-target change), Web is VERIFIED at 561/563
tests passing (up from 500, 2 failures confirmed flake/unrelated on
separate isolated reruns) — all three including genuine manual browser
verification against a real backend/MongoDB across two separate sessions,
not just mocked automated tests. This slice extended RP4C1's real-backend
click-through pattern to every one of the 19 newly exposed operations plus
a full Reset-to-Scan cycle, added an independent post-reset MongoDB
persistence check (a fresh page-load GET, deliberately not relying on the
React Query cache), and added a genuine (not simulated) overlapping-edit
stale-revision scenario — the first RP4-series entry to exercise a real
409 conflict from two actual concurrent requests rather than a mocked
error response. Three genuine bugs were found and fixed specifically
BECAUSE manual verification (and, afterward, real user testing of the
shipped feature) was performed: a pre-existing click-vs-drag bug dating
back to RP4C1 that no automated test had ever asserted against, and two
navigation/UX bugs that only became visible once a person actually tried
to use the feature end-to-end rather than exercise it through a
pre-scripted verification checklist — reinforcing, again, that automated
coverage, scripted manual verification, and actual user testing each catch
a different class of defect. Because RP4C2 touched no
RenovexCaptureApp/RoomPlanCaptureAdapter file, this tier vocabulary does
not apply to it in the iOS-hardware dimension — nothing changed from
RP4B's NOT YET XCODE-VERIFIED status.

```text
Date:                           2026-09-06
Implementation task(s):         RP4C3 — Interactive 3D Editor Foundation +
                                 Shared 2D/3D State, Selection, and
                                 Representative 3D Editing. First 3D slice:
                                 a React Three Fiber scene rendering the
                                 same authoritative RoomDraft the 2D editor
                                 renders, a shared {kind,id} selection model
                                 synced bidirectionally between 2D and 3D,
                                 a CameraControls-driven camera experience
                                 (3 presets + Fit Room + Focus Selected),
                                 and proof that a 3D drag can produce a
                                 real canonical EditOperation
                                 (move_fixture, move_service_point) through
                                 the exact same RP4B transport RP4C1/RP4C2
                                 already use. Deliberately scoped as
                                 architecture-first, not full 3D capability
                                 — no GLB/USDZ, no asset generation, no
                                 full object editing. NO backend Go source
                                 changed (only a temporary manual-
                                 verification seeder, deleted after use).
Tier:                           A — Windows, Web/TypeScript only
                                 (apps/web). Zero backend Go files and zero
                                 iOS Swift files touched — the first RP4
                                 slice to touch neither.
Environment:                    Windows 11. Web: Next.js 16.2.11
                                 (Turbopack), React 19.2.4, Vitest 4.1.10,
                                 React Testing Library, MSW. New
                                 dependencies: three@^0.185.1,
                                 @react-three/fiber@^9.7.0,
                                 @react-three/drei@^10.7.8 (caret-pinned,
                                 matching this repo's existing convention —
                                 corrects an initial plan assumption of
                                 exact pinning). Transitive:
                                 camera-controls@3.1.2, three-stdlib.
                                 Manual verification additionally used:
                                 Docker Compose (mongo:7 replica set +
                                 Mailpit), the real `go run ./cmd/api`
                                 server, the real `npm run dev` Next.js
                                 server, Chrome DevTools MCP.
Commands actually executed:
  Backend:                      `go build ./...` (unaffected — confirms no
                                 backend Go source was actually modified by
                                 this slice; the only backend-adjacent
                                 artifact was the temporary
                                 cmd/seedroomdraft_temp seeder, deleted
                                 again after manual verification).
  Web:                          `npm install three @react-three/fiber
                                 @react-three/drei` (after confirming React
                                 19.2.4 satisfies R3F's >=19 <19.3 peer
                                 range via `npm view`), `npx tsc --noEmit`
                                 (clean, run after every new/modified file
                                 and after each bugfix), `npx eslint
                                 src/features/spatial/` then full `npx
                                 eslint src/` (clean — caught 2 genuine
                                 react-hooks/set-state-in-effect violations,
                                 fixed via lazy useState initializers
                                 instead of effect-body setState calls),
                                 `npx vitest run src/features/spatial/`
                                 (183/183, up from RP4C2's 134, run after
                                 every new test file), `npx vitest run`
                                 (full app suite: 612/612 clean on one run,
                                 611/612 on a second run with the one
                                 failure confirmed as the same pre-existing
                                 ProjectProcurement.test.tsx flake already
                                 documented in every prior slice's entry —
                                 passes 52/52 in isolation; a genuinely NEW
                                 flake was found and fixed first — the 5
                                 new mode-switch tests' default 5000ms
                                 Vitest timeout proved too tight for the
                                 next/dynamic 3D-viewport import under
                                 heavy full-suite concurrent load, fixed by
                                 raising to 15000ms/20000ms, confirmed
                                 stable across 2 subsequent full-suite
                                 runs).
  Manual verification:          `docker compose up -d`, `go run ./cmd/api`
                                 (background), `demoseed reset`/`seed`, a
                                 temporary one-off Go program
                                 (backend/cmd/seedroomdraft_temp, recreated
                                 for this session, deleted again after use)
                                 seeding a 4-wall room with real
                                 height/thickness (2.5m/0.12m — the
                                 fallback path is covered by automated
                                 tests instead), a door (with hinge/swing
                                 metadata) and window, ONE free-standing +
                                 ONE wall-attached fixture, ONE
                                 free-standing + ONE wall-attached service
                                 point, and one column constraint — via the
                                 real PersistRoomDraft + SubmitEditOperation
                                 code paths (a missing
                                 SetRoomDraftEditSupport wiring call was
                                 caught and fixed in the SEEDER itself
                                 during this process — the seeder's own
                                 bug, not application code). `npm run dev`
                                 (background), Chrome DevTools MCP: logged
                                 in, opened the seeded RoomDraft, switched
                                 2D<->3D repeatedly confirming bidirectional
                                 selection persistence (selected
                                 constraint_column in 3D via a real
                                 CDP-driven click, confirmed the SAME
                                 element stayed selected after switching to
                                 2D and back), confirmed camera pose
                                 persists across a 3D->2D->3D round-trip
                                 (Overview position unchanged, not re-fit),
                                 exercised all 4 camera presets plus Fit
                                 Room (catching and fixing the Fit Room bug
                                 below), confirmed TransformControls shows
                                 only X/Z handles (no Y) on a selected
                                 free-standing service point, tested narrow
                                 viewport (390x844) rendering of both views,
                                 checked browser console for warnings on a
                                 clean reload.
Tests/counts (if available):    Web spatial-module suite: 183/183 (up from
                                 RP4C2's 134, +49 new — sceneProjection.test.ts
                                 19, cameraMath.test.ts 9,
                                 EditorViewSwitcher.test.tsx 3,
                                 useEditorState.test.tsx 21 total (+4 new),
                                 Spatial3DErrorBoundary.test.tsx 2,
                                 Spatial3DViewport.test.tsx 7,
                                 SpatialEditor.test.tsx +5 mode-switch
                                 tests). Web full app-wide suite: 612/612
                                 clean on one run, 611/612 on a second with
                                 the one pre-existing unrelated flake noted
                                 above. Backend internal/spatial: unchanged
                                 at 171/171 (no backend source modified).
                                 Swift RenovexCaptureCoreTests: unchanged
                                 at 198/198 (no Swift source modified).
Pass/fail:                      PASS — two genuine bugs found and fixed
                                 during this slice's own manual
                                 verification (not caught by any automated
                                 test), plus one genuine test-infrastructure
                                 flake found and fixed before it could
                                 recur. See "Failures and fixes" below. One
                                 verification gap disclosed rather than
                                 silently claimed as done — see the note
                                 after "Failures and fixes."
Failures and fixes:
  1. R3F crash on mount: every rendered mesh/group in
     Spatial3DViewport.tsx initially carried data-element-kind/
     data-element-id props (intended for future DOM-based test queries,
     mirroring the 2D SVG editor's established convention) — but R3F
     mesh/group elements are NOT real DOM nodes and cannot accept
     arbitrary data-* HTML attributes. R3F threw "Cannot set
     'data-element-id'. Ensure it is an object before setting
     'element-id'" and crashed the ENTIRE 3D scene on the very first
     render — confirmed via list_console_messages after the 3D view
     rendered completely blank in the browser (automated tests never
     caught this because R3F mounts cleanly in jsdom for components that
     don't hit this specific prop-setting code path, and the automated
     Spatial3DViewport.test.tsx tests were written correctly to avoid
     relying on these attributes in the first place). Fixed by removing
     all 6 data-element-kind/data-element-id pairs — they were never
     functionally needed since selection is driven entirely through
     closures in onClick/onHover handlers, not DOM attribute lookup
     (confirmed by direct DOM inspection during this same session that
     Canvas contents are genuinely Three.js scene-graph state, not
     queryable DOM at all). Confirmed fixed via a live reload: the room
     rendered correctly with all 6 element kinds visible.
  2. "Fit Room" produced a disorienting flat, close-up view instead of a
     well-framed room overview: Drei's CameraControls.fitToBox reorients
     the camera to look straight at the box's NEAREST AXIS-ALIGNED FACE
     (confirmed from the installed camera-controls package's own doc
     comment: "using the nearest axis") — from an angled Overview-style
     camera position this produced a jarring "stuck inside a wall" view
     rather than fitting the room from the current viewing angle,
     reproduced twice live in the browser before being diagnosed. Fixed
     by switching to fitToSphere, which adjusts only distance/target
     while preserving the CURRENT camera direction — confirmed fixed
     live: "Fit Room" from every preset (Overview, Top) now produces a
     clean, well-framed angled view of the whole room with all elements
     visible.
  3. (Test-infrastructure only, not a product bug) 5 new
     SpatialEditor.test.tsx mode-switch tests initially used the default
     5000ms Vitest test timeout for waiting on the 3D viewport's
     next/dynamic import — passed reliably every time in isolation, but
     failed once (a single test, "switching to 3D and back does not
     trigger a second GET room-draft request") under a heavily loaded
     full-suite run (82 files, many workers). Fixed by raising both the
     inner findByRole wait and the outer test timeout on all 5 affected
     tests (15000ms/20000ms) — confirmed stable across 2 subsequent full
     app-wide suite runs (612/612, then 611/612 with only the
     pre-existing unrelated ProjectProcurement flake).
Fixtures/regressions added:     No shared test infrastructure changed. All
                                 6 new test files are additive; no existing
                                 RP4C1/RP4C2 test was modified except the 5
                                 timeout adjustments above (test logic
                                 itself unchanged).
What remains Apple-specific
and unverified:                 Entirely unchanged from RP4C2/RP4B above —
                                 RP4C3 touched ZERO iOS/Swift files of any
                                 kind (the first RP4 slice to touch neither
                                 backend Go nor iOS Swift). RP2-MAC-001
                                 through RP2-MAC-016 remain exactly as
                                 listed there.
```

**RP4C3 status-vocabulary application:** Web is VERIFIED at 612/612 tests
passing on a clean run (up from RP4C2's 561/563), including genuine manual
browser verification against a real backend/MongoDB — not just mocked
automated tests. This is the first RP4 slice to touch neither backend Go
nor iOS Swift source at all, so Backend `internal/spatial` (171/171) and
Swift `RenovexCaptureCoreTests` (198/198) remain unchanged from RP4C2's
verified state, carried forward rather than re-verified. Two genuine bugs
were found and fixed specifically BECAUSE manual verification was
performed — an R3F crash from an invalid DOM-attribute assumption ported
over from the 2D SVG convention without checking whether it applied to
Three.js elements, and a camera-behavior bug from a library API's
documented-but-unread behavior ("fitToBox... using the nearest axis") —
both invisible to automated tests, which mock/avoid the exact code paths
where these bugs lived. This slice also disclosed, rather than silently
elided, a genuine verification-tooling limitation: this session could not
drive a pixel-accurate TransformControls drag-to-completion through the
available browser-automation primitives, despite confirming the
underlying raycast/selection pipeline DOES work correctly via the same
tooling's native click action. This is recorded as an open item for the
next session with better input-simulation tooling (or real human testing)
to close, not as a completed verification — consistent with this
project's standing rule to never mark a task complete without actually
proving it works.

```text
Date:                           2026-09-06
Implementation task(s):         RP4C4 — Deterministic Real 3D Asset
                                 Pipeline + Asset Registry + GLB/USDZ
                                 Loading + Fixture/Object Visual Binding.
                                 Replaces RP4C3's procedural placeholder
                                 fixtures/objects with a deterministic,
                                 production-shaped real-asset runtime
                                 (GLB primary, USDZ secondary) —
                                 infrastructure only, no AI generation, no
                                 new backend mutation semantics, no change
                                 to move_fixture/move_service_point's wire
                                 contract. NO backend Go application-code
                                 changed (only a temporary manual-
                                 verification seeder, recreated per
                                 established convention, deleted after
                                 use).
Tier:                           A — Windows, Web/TypeScript only
                                 (apps/web). Zero backend Go application
                                 files and zero iOS Swift files touched —
                                 the second RP4 slice to touch neither.
Environment:                    Windows 11. Web: Next.js 16.2.11
                                 (Turbopack), React 19.2.4, Vitest 4.1.10,
                                 React Testing Library. No new npm
                                 dependencies — three@0.185.1/
                                 @react-three/fiber@9.7.0/
                                 @react-three/drei@10.7.8 already ship
                                 USDLoader/USDZExporter/GLTFExporter as
                                 addons and useGLTF/useLoader as the
                                 loading hooks, all confirmed present in
                                 the already-installed versions before
                                 writing any code. Manual verification
                                 additionally used: Docker Compose
                                 (mongo:7 replica set + Mailpit), the real
                                 `go run ./cmd/api` server, the real `npm
                                 run dev` Next.js server, Chrome DevTools
                                 MCP.
Commands actually executed:
  Backend:                      `go build ./...` + `go vet ./...` (both
                                 clean — confirms no backend Go
                                 application source was modified; the
                                 only backend-adjacent artifact was the
                                 temporary cmd/seedroomdraft_temp seeder,
                                 deleted again after manual verification).
  Asset generation:             A temporary Node script
                                 (apps/web/scripts/generate-demo-assets-temp/generate.mjs,
                                 deleted after use) run via `node
                                 scripts/generate-demo-assets-temp/generate.mjs`
                                 from apps/web, using Three.js's own
                                 GLTFExporter/USDZExporter addons directly
                                 to produce 4 wholly original, self-
                                 authored assets (fixture-boiler-v1.glb,
                                 fixture-electrical-panel-v1.glb,
                                 object-sofa-v1.glb, format-proof.usdz) —
                                 zero third-party downloads, zero
                                 licensing ambiguity. Node's Blob lacks a
                                 FileReader global GLTFExporter's binary
                                 path expects; a minimal spec-accurate
                                 shim (Blob.arrayBuffer() wrapped in the
                                 one method GLTFExporter calls) was added
                                 to the temporary script only, not
                                 anywhere in application code. All 4
                                 generated files independently re-verified
                                 loadable via Three's own real
                                 GLTFLoader/USDLoader in a second temporary
                                 Node script (also deleted) before being
                                 wired into the app — bounds/dimensions
                                 inspected directly, not assumed from the
                                 generator's own inputs.
  Web:                          `npx tsc --noEmit` (clean, run after every
                                 new/modified file), `npx eslint` full
                                 project run (clean on every RP4C4 file —
                                 the only 5 warnings anywhere are
                                 pre-existing, in unrelated procurement/
                                 operations files never touched by this
                                 slice), `npx vitest run
                                 src/features/spatial` (229/229, up from
                                 RP4C3's 183, +46 new — run after every
                                 new test file, strict TDD RED-then-GREEN
                                 for every one), `npx vitest run` (full
                                 app suite: 657/658, the one failure
                                 confirmed as the same pre-existing
                                 ProjectProcurement.test.tsx flake
                                 documented in every prior slice's entry —
                                 passes 52/52 in isolation).
  Manual verification:          `docker compose up -d`, `go run
                                 ./cmd/api` (background), a fresh isolated
                                 tenant created via the real HTTP API
                                 (`POST /auth/register`, `/clients`,
                                 `/projects`, `/spaces` — deliberately NOT
                                 the pre-existing demo tenant, whose
                                 password from a prior session is unknown
                                 to this one, confirmed via a `demoseed`
                                 re-run correctly refusing without it), a
                                 temporary one-off Go program
                                 (backend/cmd/seedroomdraft_temp,
                                 recreated for this session, deleted again
                                 after use) seeding 2 boiler fixtures, 1
                                 wall-attached electrical_panel fixture, 1
                                 ac fixture (no registered asset), and 1
                                 sofa object via the real StartCapture +
                                 PersistRoomDraft + SubmitEditOperation
                                 code paths. `npm run dev` (background),
                                 Chrome DevTools MCP: logged in, opened
                                 the seeded RoomDraft, switched to 3D and
                                 confirmed procedural-to-real swap for
                                 both boilers and the electrical panel
                                 (the AC fixture correctly stayed
                                 procedural — no registered category),
                                 confirmed via list_network_requests that
                                 each of the 3 GLBs was fetched EXACTLY
                                 ONCE despite 2 boiler instances sharing
                                 one asset (useGLTF's shared-cache
                                 behavior proven, not assumed), clicked a
                                 wall-attached real-asset fixture and
                                 confirmed the non-mutating wireframe
                                 highlight overlay rendered correctly with
                                 NO TransformControls gizmo, clicked a
                                 free-standing real-asset fixture and
                                 confirmed the gizmo DID appear, refreshed
                                 and confirmed revision persisted with
                                 assets re-served as 304 Not Modified and
                                 a clean console, cycled 2D->3D->2D->3D
                                 and confirmed via network inspection zero
                                 GLB re-fetches plus a benign
                                 THREE.WebGLRenderer: Context Lost log on
                                 unmount (expected, not a leak), tested
                                 narrow viewport (390x844) rendering,
                                 deliberately renamed
                                 fixture-boiler-v1.glb to simulate a 404
                                 and confirmed both boiler instances fell
                                 back to the procedural box with zero app
                                 crash (see "Failures and fixes" below for
                                 the one nuance found here), then restored
                                 the file and confirmed full recovery.
                                 Re-attempted the RP4C3 TransformControls
                                 drag-verification gap per explicit
                                 instruction to re-check, not silently
                                 upgrade: confirmed via ToolSearch that
                                 the drag tool is still uid-to-uid only,
                                 then attempted a synthetic
                                 pointerdown/pointermove-sequence/pointerup
                                 drag on a selected free-standing
                                 fixture's gizmo arm — no move_fixture
                                 POST resulted, and the camera itself
                                 orbited/zoomed instead, the exact same
                                 root cause RP4C3 diagnosed
                                 (CameraControls consuming the events, not
                                 TransformControls' raycaster). Retained
                                 explicitly as unresolved, not claimed
                                 fixed.
Tests/counts (if available):    Web spatial-module suite: 229/229 (up
                                 from RP4C3's 183, +46 new —
                                 registry.test.ts 6, resolveAsset.test.ts
                                 5, normalization.test.ts 8,
                                 sourceUrl.test.ts 1, VisualAsset.test.tsx
                                 3, GLTFVisualAsset.test.tsx 4,
                                 USDVisualAsset.test.tsx 3,
                                 useNormalizedAsset.test.ts 6,
                                 AssetFallback.test.tsx 2,
                                 AssetErrorBoundary.test.tsx 4,
                                 Spatial3DViewport.test.tsx +4 real-asset
                                 mount smoke tests). Web full app-wide
                                 suite: 657/658, the one pre-existing
                                 unrelated flake noted above. Backend
                                 internal/spatial: unchanged (no backend
                                 application source modified). Swift
                                 RenovexCaptureCoreTests: unchanged (no
                                 Swift source modified).
Pass/fail:                      PASS — one nuance found and clarified
                                 during this slice's own manual
                                 verification (not a defect, see below),
                                 plus one verification-tooling mistake
                                 made and corrected mid-session (also
                                 below, not a product bug). One
                                 verification gap re-checked as mandated
                                 and explicitly NOT resolved — see the
                                 note after "Failures and fixes."
Failures and fixes:
  1. (Clarification, not a defect) Simulating a 404 for
     fixture-boiler-v1.glb correctly triggered the procedural fallback
     with zero app crash — but Next.js's OWN dev-mode overlay ALSO
     surfaced the underlying error as a full-screen "Runtime Error" toast
     with a "3 Issues" badge, which initially looked like the error
     boundary had failed to catch it. Confirmed via the overlay's own
     displayed call stack (VisualAsset -> FixtureMesh -> SceneContents)
     that the error WAS caught inside AssetErrorBoundary — the app never
     crashed, the fallback rendered correctly the entire time, and the
     canvas remained present and interactive. This is standard Next.js
     16 dev-mode behavior: caught render errors are still surfaced via
     the dev overlay for visibility, and will not appear in a production
     build. No code change was needed; documented here so a future
     session does not misdiagnose this overlay as an uncaught-error
     regression.
  2. (Verification-tooling mistake, not a product bug) While dismissing
     an open native <select> category dropdown via a blanket
     document.body.click(), 4 unintended reclassify_fixture edits were
     submitted (revision jumped by 4) before landing back on the
     original, correct category values — confirmed via a direct GET
     /spatial/room-drafts/{id} check that no fixture's category actually
     changed. Root cause: clicking through an open native <select>'s
     option list via a body-level click handler, not a defect in the
     reclassify flow itself. Recorded as a technique lesson: prefer
     Escape or a specific known-safe target over a blanket body-click
     when a native <select> may still be open.
Fixtures/regressions added:     No shared test infrastructure changed.
                                 All new test files are additive; no
                                 existing RP4C1/RP4C2/RP4C3 test was
                                 modified.
What remains Apple-specific
and unverified:                 Entirely unchanged from RP4C3 above —
                                 RP4C4 touched ZERO iOS/Swift files of any
                                 kind. RP2-MAC-001 through RP2-MAC-016
                                 remain exactly as listed there.
```

**RP4C4 status-vocabulary application:** Web is VERIFIED at 657/658 tests
passing on a clean run (up from RP4C3's 612/612), including genuine
manual browser verification against a real backend/MongoDB — not just
mocked automated tests. This is the second RP4 slice to touch neither
backend Go application code nor iOS Swift source at all, so Backend
`internal/spatial` and Swift `RenovexCaptureCoreTests` remain unchanged
from RP4C3's verified state, carried forward rather than re-verified.
Unlike RP4C1/RP4C2/RP4C3, this slice found no genuine PRODUCT bug during
manual verification — the one nuance found (Next.js's dev overlay
surfacing a caught error) was a documentation clarification, not a code
fix, and the one mistake made (the accidental reclassify edits) was a
verification-technique error in this session's own tooling use, not a
defect in the shipped feature. The RP4C3 `TransformControls`
drag-verification gap was explicitly re-checked per the kickoff's own
instruction not to silently upgrade it to "verified" — it was
re-confirmed unresolved with the same root cause (`CameraControls`
consuming synthetic pointer events meant for the gizmo), and is retained
as an open item for a future session with better input-simulation
tooling or real human testing to close.

```text
Date:                           2026-09-06
Implementation task(s):         RP4D — Persistent Visual Asset Identity +
                                 Authorized Asset Delivery Foundation.
                                 Adds a canonical, cross-platform,
                                 PERSISTED per-element visual-asset
                                 reference ({assetId,version}), an
                                 immutable versioned asset store, and an
                                 authorization-gated temporary read-access
                                 mechanism — riding RP4B's existing
                                 CAS/idempotency/audit edit-operation
                                 pipeline unchanged. Infrastructure only:
                                 no AI generation, no asset upload UI, no
                                 asset catalogue/browser.
Tier:                           Backend (Go, real Mongo), iOS Swift
                                 (Windows toolchain), and Web/TypeScript
                                 all touched this slice — the first RP4
                                 slice since RP4B to change backend
                                 application code.
Environment:                    Windows 11. Backend: Go 1.25+, chi+Huma
                                 v2, mongo-driver v2, mongo:7 replica set
                                 via Docker Compose. iOS:
                                 RenovexCaptureCore on the Windows Swift
                                 6.3.3 toolchain (unchanged toolchain from
                                 every prior RP slice). Web: Next.js
                                 16.2.11 (Turbopack), React 19.2.4, Vitest
                                 4.1.10. No new npm/Go module dependencies
                                 — the HMAC capability keyring reuses
                                 Go's standard crypto/hmac+crypto/sha256,
                                 matching the existing
                                 InvitationKeyring/SupplierSessionTokenKeyring
                                 convention. Manual verification used
                                 Docker Compose (mongo:7 + Mailpit), the
                                 real `go run ./cmd/api` server, the real
                                 `npm run dev` Next.js server, Chrome
                                 DevTools MCP, and direct `mongosh`/driver
                                 inspection.
Commands actually executed:
  Backend:                      `go build ./...`, `go vet ./...`, `go
                                 test ./...` — all clean/passing,
                                 including 3 new real-HTTP integration
                                 tests in
                                 internal/tenanttest/spatial_visual_asset_test.go
                                 (full lifecycle, cross-company denial,
                                 tampered-capability rejection) exercised
                                 against a real Testcontainers-backed
                                 mongo:7 instance.
  iOS:                           `swift build` (clean, zero call sites
                                 needed changes thanks to default-nil
                                 backward compatibility), `swift test` —
                                 214/214 passing (up from 198/198).
  Web:                           `npx vitest run` (694/694 passing, up
                                 from 657/658 — the prior slice's one
                                 flaky ProjectProcurement.test.tsx no
                                 longer flaked in this run), `npx tsc
                                 --noEmit` (clean), `npm run lint` (clean
                                 — 5 remaining warnings are pre-existing
                                 react-hook-form incompatible-library
                                 warnings in unrelated files, confirmed
                                 untouched by this slice), `cd backend &&
                                 go run ./cmd/openapi -out
                                 ../apps/web/openapi/openapi.json` + `npm
                                 run openapi:generate` (regenerated
                                 schema.ts to include VisualAssetRef on
                                 both RoomDraftFixture/RoomDraftObject DTOs).
  Real E2E verification:         `docker compose up -d`; a temporary
                                 backend/cmd/seedvisualasset_temp
                                 (deleted after use) registered a fresh
                                 tenant, built a full
                                 project/property/space/RoomDraft via the
                                 real capture lifecycle, added a fixture,
                                 published two e2e-test-boiler versions
                                 (v1 = RP4C4's real project-original
                                 fixture-boiler-v1.glb bytes, v2 = a
                                 freshly generated distinct deterministic
                                 GLB, both project-original per plan §42);
                                 real HTTP calls (login, assign_visual_asset,
                                 clear_visual_asset, the access-grant and
                                 content routes) via curl-equivalent
                                 scripting; direct Mongo document
                                 inspection; Chrome DevTools MCP driving
                                 the real Next.js app for Network-tab and
                                 console verification; `docker compose
                                 down` (no -v) to tear down.
Pass/fail:                      PASS — every verification step in the
                                 kickoff's real-E2E requirement succeeded
                                 against real infrastructure. One real bug
                                 caught during backend TDD (not during
                                 manual verification — see below) and
                                 fixed before any manual verification
                                 began. One benign environmental finding,
                                 not a product defect (see below).
Failures and fixes:
  1. (Real bug, caught by RED-first TDD, fixed before manual verification)
     Service.SubmitEditOperation's first draft used a value-type
     assertion (op.(AssignVisualAssetOperation)) to detect an
     assign_visual_asset operation and run its pre-flight
     asset-existence/tenant check — but decodeEditOperation actually
     returns a POINTER (*AssignVisualAssetOperation), so the assertion
     silently always failed and the authorization check never ran at
     all. A RED test (TestSubmitEditOperation_AssignVisualAsset_RejectsNonexistentAsset)
     unexpectedly passed with err == nil instead of the expected
     ErrVisualAssetVersionNotFound, exposing it immediately. Fixed by
     changing to op.(*AssignVisualAssetOperation); re-ran the same test
     suite to confirm the fix. This would have shipped as a completely
     non-functional cross-tenant authorization check without the
     RED-first discipline catching it before any manual verification
     even started.
  2. (Genuine wire-contract gap, found during Web implementation, resolved
     without expanding scope) The OpenAPI-generated VisualAssetNormalization
     DTO carries only {pivot} — intrinsicUnit/upAxis are fixed constants
     per the backend's own doc comment, never sent over the wire
     redundantly — but the Web's existing RP4C4 VisualAssetNormalization
     type requires all three fields for computeAssetNormalization.
     Resolved by having AuthorizedVisualAsset.tsx construct the complete
     normalization object by combining the server's pivot with the two
     fixed constants client-side, rather than changing either the wire
     contract or the existing RP4C4 normalization math.
  3. (Benign environmental finding, not an RP4D defect) Headless
     Chromium/ANGLE(D3D11)-over-CDP logged "THREE.WebGLRenderer: Context
     Lost" immediately after renderer init in this specific session,
     preventing a literal visual screenshot as proof of the bound asset
     rendering. Substituted with the definitive Network-tab (exact
     access-grant/content requests) + direct-Mongo evidence chain
     instead, which is more precise proof of correct behavior than a
     screenshot would have been — the identical evidence chain fired
     correctly and identically on every verification step regardless of
     this rendering-layer issue.
Fixtures/regressions added:     Backend: 3 real-HTTP integration tests in
                                 a new spatial_visual_asset_test.go, plus
                                 unit/repository tests across
                                 visualasset.go/visualasset_service.go/
                                 repository_mongo.go/secrets/config. iOS:
                                 16 new tests across EditOperationTests.swift/
                                 EditOperationWireContractTests.swift/
                                 RoomDraftCodableTests.swift. Web: new
                                 test files api.test.ts, queries.test.tsx,
                                 AuthorizedVisualAsset.test.tsx, plus
                                 additions to sceneProjection.test.ts,
                                 resolveAsset.test.ts, operations.test.ts,
                                 VisualAsset.test.tsx,
                                 Spatial3DViewport.test.tsx,
                                 ElementInspector.test.tsx. No existing
                                 test file's assertions were weakened or
                                 removed to make this slice pass.
What remains Apple-specific
and unverified:                 Entirely unchanged from RP4C4 above — RP4D
                                 is Swift-mirror-only on iOS (wire-contract
                                 parity and local structural
                                 validate/apply), with NO 3D rendering, NO
                                 asset download, and NO new iOS UI. The
                                 RP2-MAC-001 through RP2-MAC-016 Xcode
                                 compile checks remain exactly as listed
                                 there, entirely unaffected by RP4D.
```

**RP4D status-vocabulary application:** Backend, iOS, and Web are all
VERIFIED — Backend `go test ./...` clean including 3 new real-HTTP
integration tests against a real Testcontainers Mongo instance; iOS
214/214 (up from 198/198); Web 694/694 (up from 657/658). This is the
first RP4 slice since RP4B to touch backend application code, and it did
so while proving RP4B's existing edit-operation transport needed ZERO
changes to carry 2 new canonical operations — confirming the design goal
that the operation vocabulary is genuinely extensible without touching
CAS/idempotency/audit machinery. Real end-to-end verification went beyond
automated tests to prove, against real infrastructure, every hard
invariant the kickoff demanded: exact-version pinning survives a reload,
one shared access grant serves two fixtures bound to the same version,
a tampered or expired capability is rejected before any repository/
object-store call, cross-company access is denied (404, matching this
project's existing hide-cross-tenant-existence convention), and no
storage key or signed URL is ever persisted in RoomDraft, Mongo, or any
Web client-side storage. The one real bug found (the pointer/value-type
assertion mismatch above) was caught by TDD discipline before manual
verification ever began, not during it — a genuinely serious near-miss
that RED-first testing prevented from reaching even the manual-
verification stage, let alone production. The RP4C3 TransformControls
drag-verification gap was re-checked per this slice's own explicit
instruction not to silently upgrade it — it remains unresolved with the
same root cause, retained as an open item exactly as RP4C4 left it.
