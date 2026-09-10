import Foundation

/// Whether the current device/provider combination can perform a live
/// capture (design spec §8.6, RoomPlan amendment §6: production code must
/// use the real RoomPlan supported-device capability check, never fake
/// support). An unsupported device must still be able to view existing
/// confirmed spatial data — it just cannot start a new capture session.
public enum SpatialCaptureSupportState: Equatable, Sendable {
    case unknown
    case supported
    case unsupported(reason: String)
}

/// The client-side capture/application lifecycle state (distinct from the
/// already-shipped backend `SpatialCapture` status machine in
/// `backend/internal/spatial/spatial.go` — draft/capturing/uploading/
/// uploaded/review/confirmed/superseded/failed). This type models what the
/// PROVIDER is doing locally on-device; mapping this to/from the backend's
/// lifecycle is an explicit later concern (RP3/RP6), not something this
/// type conflates by trying to share vocabulary with the backend enum.
public enum SpatialCaptureSessionState: Equatable, Sendable {
    case idle
    case checkingSupport
    case ready
    case capturing
    case processing
    case completed
    case cancelled
    case failed(reason: String)
}
