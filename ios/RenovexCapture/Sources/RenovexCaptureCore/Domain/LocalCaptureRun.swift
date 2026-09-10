import Foundation

/// The client-local lifecycle status of one persisted capture run (plan
/// §RP3). Deliberately narrower than the backend's `spatial.CaptureStatus`
/// (draft/capturing/uploading/uploaded/review/confirmed/superseded/failed) —
/// this only distinguishes what Scan History needs to render locally before
/// a capture has necessarily synced to the server at all. Mapping this to
/// the backend's status machine is a sync-layer concern, not this type's.
public enum LocalCaptureStatus: String, Equatable, Sendable, Codable {
    /// RoomPlan completed and a RoomDraft was normalized and persisted —
    /// Room Review is presentable (plan §RP3's required ordering).
    case pendingReview
    /// The contractor confirmed this run (RP6 scope wires real server
    /// confirmation; this status exists so Scan History can render a
    /// confirmed affordance once that lands without another persistence
    /// migration).
    case confirmed
    /// A prior current run for this Space, superseded by a later confirmed
    /// run.
    case superseded
}

/// One durably persisted capture run for one Space (design spec §8.22, plan
/// §RP3's hard multi-scan persistence requirement: "every usable room scan
/// must become a durable capture run... a new scan must never overwrite the
/// previous scan"). This is the LOCAL, client-side record — distinct from,
/// but eventually linked to, the backend's `spatial.SpatialCapture` once
/// sync completes (`serverCaptureID`).
///
/// Carries enough identity/context for Scan History (plan §RP3 §8):
/// project, space, a stable local id, a 1-based captureNumber for display
/// ("#1", "#2", "#3"), the source provider, status, and timestamps.
///
/// No `companyID` field: the iOS client never has its own companyID
/// available anywhere (the JWT/`accessToken` is opaque to the client — only
/// the backend derives companyID server-side from it, the same way every
/// other RenovexAPIClient call is scoped implicitly by the bearer token
/// rather than an explicit client-held companyID). Local storage is already
/// naturally scoped to one device/one logged-in session.
public struct LocalCaptureRun: Equatable, Sendable, Codable, Identifiable {
    public var id: String
    public var projectID: String
    public var spaceID: String
    public var provider: SpatialCaptureSourceProvider
    public var captureNumber: Int
    public var status: LocalCaptureStatus
    public var capturedAt: Date
    public var updatedAt: Date
    /// Set once this run has a persisted `RoomDraft` — mirrors the backend's
    /// `SpatialCapture.RoomDraftID` linkage, kept locally so "Continue
    /// Review" can look the draft up without a round trip.
    public var roomDraftID: String?
    /// Set once this local run has been created server-side (via the
    /// existing `POST /spatial/captures` — RP1/Task 2's shipped endpoint).
    /// Nil until sync completes; a nil value here is exactly the "local
    /// only, not yet synced" state (plan §RP3 §14).
    public var serverCaptureID: String?

    public init(
        id: String,
        projectID: String,
        spaceID: String,
        provider: SpatialCaptureSourceProvider,
        captureNumber: Int,
        status: LocalCaptureStatus,
        capturedAt: Date,
        updatedAt: Date,
        roomDraftID: String? = nil,
        serverCaptureID: String? = nil
    ) {
        self.id = id
        self.projectID = projectID
        self.spaceID = spaceID
        self.provider = provider
        self.captureNumber = captureNumber
        self.status = status
        self.capturedAt = capturedAt
        self.updatedAt = updatedAt
        self.roomDraftID = roomDraftID
        self.serverCaptureID = serverCaptureID
    }
}
