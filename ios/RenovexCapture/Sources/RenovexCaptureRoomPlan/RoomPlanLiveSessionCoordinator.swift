import Foundation
import RoomPlan
import RenovexCaptureCore

// ==================================================================
// APPLE ROOMPLAN API BRIDGE — DOCUMENTED, NOT YET XCODE-COMPILED/
// RUNTIME-VERIFIED. See RoomPlanCaptureAdapter.swift's header for the full
// RP2-MAC-xxx pending-compile-check list; this file adds:
//
//   RP2-MAC-013  RoomCaptureSessionDelegate method signatures. Confirmed
//                against Apple's public RoomPlan documentation:
//                  captureSession(_:didAdd: CapturedRoom)
//                  captureSession(_:didChange: CapturedRoom)
//                  captureSession(_:didRemove: CapturedRoom)
//                  captureSession(_:didUpdate: CapturedRoom)
//                  captureSession(_:didEndWith: CapturedRoomData, error: Error?)
//                These signatures themselves are documented, not
//                speculative — the remaining uncertainty is exact
//                runtime callback granularity/timing, and RoomBuilder's
//                exact API (RP2-MAC-016), not whether these methods exist.
//   RP2-MAC-014  RoomCaptureView initializer and its `captureSession`
//                property.
//   RP2-MAC-016  RoomBuilder.capturedRoom(from: CapturedRoomData) async
//                throws -> CapturedRoom — the documented final-processing
//                step from didEndWith's raw CapturedRoomData to a
//                finished CapturedRoom.
//
// Per Apple's documented callback semantics, a callback's CapturedRoom
// argument is NOT necessarily a full-room replacement snapshot:
//   didAdd    → newly added surfaces/objects
//   didChange → surfaces/objects whose dimensions/transforms changed
//   didRemove → surfaces/objects that were removed
//   didUpdate → updated scan results
// This file's upsert/remove logic is written to be correct under this
// documented delta semantics: each callback's reported elements are
// upserted/removed by source identifier, and elements the callback does
// NOT mention are left untouched — never treated as implicitly removed by
// omission (that would be wrong under delta semantics, unlike a full-
// snapshot assumption). If real-device behavior turns out to differ, only
// this file's dispatch logic needs to change — LiveElementTracker's
// upsert/remove primitives remain correct either way.
// ==================================================================

/// Bridges RoomPlan's live `RoomCaptureSession` delegate callbacks
/// (`didAdd`/`didChange`/`didUpdate`/`didRemove`) into the provider-neutral
/// `LiveElementTracker` (design spec §8.10). This is the RP2 "no duplicate
/// current-state elements" requirement's actual live-session wiring — the
/// dedup/replace/remove LOGIC itself lives in `LiveElementTracker`
/// (Windows-verified); this coordinator's only job is translating RoomPlan
/// callback objects into `LiveElementSnapshot` upsert/remove calls
/// following Apple's documented per-callback delta semantics above.
///
/// NOT a `RoomShell`/`WallCandidate` reconstruction system — RoomPlan
/// already performs reconstruction; this type does no geometric clustering
/// or intersection resolution of its own (design spec §8.10's explicit
/// prohibition, carried over from the frozen-provider architecture this
/// replaces).
public final class RoomPlanLiveSessionCoordinator: NSObject, @unchecked Sendable {
    /// Live current-state trackers, one per element kind, so a wall source
    /// identifier colliding with an object source identifier (unlikely,
    /// but RoomPlan does not document identifier-space uniqueness across
    /// element kinds) can never cause one kind's element to silently
    /// replace another's.
    public let wallTracker = LiveElementTracker()
    public let surfaceTracker = LiveElementTracker()
    public let objectTracker = LiveElementTracker()

    /// Called after every live update, with the current full snapshot
    /// across all three trackers — a SwiftUI view model observes this to
    /// drive live UI (e.g. a running wall/object count), without RP2
    /// building any actual scanning HUD (explicitly deferred — RP2
    /// amendment §2: "do not implement rp5 measure/mark/note production
    /// tools now").
    public var onLiveUpdate: (@Sendable () -> Void)?

    /// Set by `RoomPlanCaptureProvider` before starting a session; invoked
    /// once `captureSession(_:didEndWith:error:)` fires, carrying the raw
    /// `CapturedRoomData` (or the error) through to the provider's
    /// `startCapture()` continuation.
    public var onSessionEnd: (@Sendable (Result<CapturedRoomData, Error>) -> Void)?

    public override init() {
        super.init()
    }

    public func reset() {
        wallTracker.reset()
        surfaceTracker.reset()
        objectTracker.reset()
    }
}

// MARK: - RoomCaptureSessionDelegate

extension RoomPlanLiveSessionCoordinator: RoomCaptureSessionDelegate {
    public func captureSession(_ session: RoomCaptureSession, didAdd room: CapturedRoom) {
        upsertAll(from: room)
        onLiveUpdate?()
    }

    public func captureSession(_ session: RoomCaptureSession, didChange room: CapturedRoom) {
        upsertAll(from: room)
        onLiveUpdate?()
    }

    public func captureSession(_ session: RoomCaptureSession, didUpdate room: CapturedRoom) {
        upsertAll(from: room)
        onLiveUpdate?()
    }

    public func captureSession(_ session: RoomCaptureSession, didRemove room: CapturedRoom) {
        // Per documented delta semantics, `room` here reports the elements
        // that were REMOVED, not the elements that remain — so this is a
        // targeted remove-by-source-identifier for exactly what's listed,
        // never a "keep only these" prune.
        for wall in room.walls {
            wallTracker.remove(sourceIdentifier: wall.identifier.uuidString)
        }
        for surface in room.doors + room.windows + room.openings {
            surfaceTracker.remove(sourceIdentifier: surface.identifier.uuidString)
        }
        for object in room.objects {
            objectTracker.remove(sourceIdentifier: object.identifier.uuidString)
        }
        onLiveUpdate?()
    }

    public func captureSession(_ session: RoomCaptureSession, didEndWith data: CapturedRoomData, error: Error?) {
        if let error {
            onSessionEnd?(.failure(error))
        } else {
            onSessionEnd?(.success(data))
        }
    }

    /// `didAdd`/`didChange`/`didUpdate` share the same upsert-by-source-
    /// identifier handling: whatever elements the callback reports are
    /// inserted (if new) or replace their existing current representation
    /// (if already tracked) — `LiveElementTracker.upsert` already
    /// guarantees this is idempotent and never creates a duplicate for a
    /// repeated source identifier (Windows-verified in
    /// `LiveElementTrackerTests`). Elements NOT mentioned in `room` are
    /// left untouched, consistent with the documented delta (not
    /// full-snapshot) semantics.
    private func upsertAll(from room: CapturedRoom) {
        for wall in room.walls {
            wallTracker.upsert(LiveElementSnapshot(
                sourceIdentifier: wall.identifier.uuidString,
                kind: .wall,
                // Placeholder position: this tracker exists for
                // identity/dedup/live-count bookkeeping, not final
                // geometry. RoomDraftNormalizer computes real, precise
                // geometry from the complete CapturedRoom at session
                // completion (RoomPlanCaptureAdapter.makeRoomDraft), not
                // from these incremental live callbacks.
                transform: RoomLocalTransform(position: .init(x: 0, y: 0, z: 0))
            ))
        }
        for surface in room.doors + room.windows + room.openings {
            surfaceTracker.upsert(LiveElementSnapshot(
                sourceIdentifier: surface.identifier.uuidString,
                kind: .opening,
                transform: RoomLocalTransform(position: .init(x: 0, y: 0, z: 0))
            ))
        }
        for object in room.objects {
            objectTracker.upsert(LiveElementSnapshot(
                sourceIdentifier: object.identifier.uuidString,
                kind: .object,
                transform: RoomLocalTransform(position: .init(x: 0, y: 0, z: 0))
            ))
        }
    }
}
