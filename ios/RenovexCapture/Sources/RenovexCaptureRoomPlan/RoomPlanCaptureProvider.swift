import Foundation
import RoomPlan
import RenovexCaptureCore

// APPLE ROOMPLAN API BRIDGE — see RoomPlanCaptureAdapter.swift's header for
// the RP2-MAC-xxx pending-compile-check list. RoomCaptureSession.isSupported
// (RP1) and RoomCaptureSessionDelegate (RP2) are documented; RoomBuilder's
// exact async API shape used in `processCapturedData` is RP2-MAC-016.

/// The concrete iOS/RoomPlan conformer of `SpatialCaptureProvider` (design
/// spec §8.7). This is the ONLY type that both imports RoomPlan and
/// conforms to the provider protocol — everything downstream of
/// `startCapture()`'s raw result stays provider-neutral until
/// `RoomPlanCaptureAdapter` unwraps it.
///
/// RP2 scope: wires an actual live `RoomCaptureSession` through to a
/// completed, processed `CapturedRoom` result, per the documented
/// architecture:
/// ```text
/// LIVE
/// RoomCaptureSession
/// ├─ didAdd / didChange / didRemove / didUpdate
/// →  RoomPlanLiveSessionCoordinator → LiveElementTracker (dedup)
///
/// FINAL
/// didEndWith CapturedRoomData
/// →  RoomBuilder.capturedRoom(from:)
/// →  final CapturedRoom
/// →  RoomPlanCaptureAdapter.makeRoomDraft(from:)
/// →  RoomDraft
/// ```
/// Uses `RoomCaptureView` for the capture UX (design spec §8.7.1: "use
/// Apple's provided capture UX as much as possible" for the initial
/// implementation) — this provider does not manage `RoomCaptureView`
/// itself (that is a UI concern, see `RoomPlanCaptureView.swift` in
/// `RenovexCaptureApp`); it manages the underlying `RoomCaptureSession`
/// that a `RoomCaptureView` can be configured to reuse, so `startCapture()`
/// works whether or not a `RoomCaptureView` is currently on screen.
public final class RoomPlanCaptureProvider: SpatialCaptureProvider, @unchecked Sendable {
    public let providerIdentity: SpatialCaptureSourceProvider = .roomplan

    /// Exposed so a `RoomCaptureView` in the UI layer can be configured to
    /// drive/display this same session (design spec §8.7.1's "RoomCaptureView
    /// → use Apple's provided capture UX" — the view and the session are
    /// two separate objects in RoomPlan's own API).
    public let session: RoomCaptureSession
    public let liveCoordinator: RoomPlanLiveSessionCoordinator

    public init() {
        self.session = RoomCaptureSession()
        self.liveCoordinator = RoomPlanLiveSessionCoordinator()
        self.session.delegate = liveCoordinator
    }

    /// Uses RoomPlan's own supported-device capability check
    /// (`RoomCaptureSession.isSupported`) — never a hardcoded value in
    /// production code (RoomPlan amendment §6). `FixtureCaptureProvider` is
    /// the correct type to use when a test needs to simulate
    /// supported/unsupported states deterministically.
    public func checkSupport() async -> SpatialCaptureSupportState {
        if RoomCaptureSession.isSupported {
            return .supported
        }
        return .unsupported(reason: "RoomPlan requires a LiDAR-capable device.")
    }

    /// Starts a live RoomPlan capture session and suspends until the
    /// session ends (either the caller stops it, e.g. via a UI "Done"
    /// action calling `stopSession()`, or RoomPlan itself ends it), then
    /// processes the raw result through `RoomBuilder` into a final
    /// `CapturedRoom` and wraps it as a `SpatialCaptureRawResult`.
    ///
    /// Live progress during the session is available via
    /// `liveCoordinator`'s trackers/`onLiveUpdate` callback for UI
    /// binding — `startCapture()` itself only resolves once, at session
    /// completion, matching `SpatialCaptureProvider`'s single-result
    /// contract (design spec §8.7).
    public func startCapture() async throws(SpatialCaptureProviderError) -> SpatialCaptureRawResult {
        guard RoomCaptureSession.isSupported else {
            throw .unsupported(reason: "RoomPlan requires a LiDAR-capable device.")
        }

        liveCoordinator.reset()

        let rawData: CapturedRoomData
        do {
            rawData = try await withCheckedThrowingContinuation { continuation in
                liveCoordinator.onSessionEnd = { result in
                    continuation.resume(with: result)
                }
                session.run(configuration: RoomCaptureSession.Configuration())
            }
        } catch {
            throw .sessionFailed(reason: "RoomPlan session ended with an error: \(error)")
        }

        let finalRoom: CapturedRoom
        do {
            finalRoom = try await RoomBuilder(options: [.beautifyObjects]).capturedRoom(from: rawData)
        } catch {
            throw .sessionFailed(reason: "RoomBuilder failed to process captured data: \(error)")
        }

        return SpatialCaptureRawResult(provider: .roomplan, payload: finalRoom)
    }

    /// Stops the current live session, triggering
    /// `captureSession(_:didEndWith:error:)` — a UI "Done"/"Cancel" action
    /// calls this; `startCapture()`'s suspended continuation then resumes
    /// via `liveCoordinator.onSessionEnd`.
    public func stopSession() {
        session.stop()
    }
}
