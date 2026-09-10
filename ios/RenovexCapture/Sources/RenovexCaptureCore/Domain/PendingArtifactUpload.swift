import Foundation

/// Which backend `ArtifactKind` this pending upload targets (plan
/// §RP3.5/§RP4B0 — mirrors `backend/internal/spatial/artifact.go`'s
/// closed `ArtifactKind` enum exactly; only the kinds this task's capture
/// pipeline actually produces are modeled here, not the full backend set).
public enum PendingArtifactKind: String, Equatable, Sendable, Codable {
    case capturedRoomData = "captured_room_data"
    case roomplanProcessed = "roomplan_processed"
    case roomdraftJSON = "roomdraft_json"
    case usdz
}

/// The durable client-side lifecycle of one artifact sync record. Distinct
/// from the backend's `ArtifactStatus` (pending/uploaded) — this also
/// tracks states the backend has no visibility into yet (not even
/// requested a slot; a request/PUT/finalize step failed and needs retry).
public enum PendingArtifactUploadStatus: String, Equatable, Sendable, Codable {
    /// Recorded locally; no server upload slot requested yet.
    case notStarted
    /// `RequestArtifactUpload`/`ResumeArtifactUpload` succeeded; a token is
    /// held in memory (never persisted) and bytes have not yet been PUT.
    case slotAcquired
    /// Bytes were PUT to the content endpoint; `FinalizeArtifactUpload` has
    /// not yet succeeded.
    case uploaded
    /// `FinalizeArtifactUpload` succeeded — this record's work is done.
    /// Terminal state.
    case finalized
    /// The most recent attempt failed (network/server error) and should be
    /// retried — distinct from `notStarted` so retry/backoff logic can
    /// tell "never tried" from "tried and failed" apart.
    case failed
}

/// One durably persisted record of an artifact this device needs to
/// synchronize to the backend (plan §RP3.5/§RP4B0). `SpatialArtifactStore`
/// owns durable client-side knowledge of upload state — never RoomDraft
/// domain semantics, which stay in `RoomDraftRepository`.
///
/// Identity is deterministic: `id` is always
/// `"\(captureRunID)_\(kind.rawValue)"` (see
/// `PendingArtifactUpload.deterministicID(captureRunID:kind:)`), so
/// recording a pending upload for the same capture run + kind twice
/// (e.g. a retried `CaptureCompletionCoordinator.completeCapture` call)
/// naturally resolves to the SAME record rather than creating a duplicate
/// — idempotent enqueue by construction, not by a separate dedup check.
public struct PendingArtifactUpload: Equatable, Sendable, Codable, Identifiable {
    public var id: String
    public var kind: PendingArtifactKind
    /// The capture run (`LocalCaptureRun.id`) this artifact belongs to.
    public var captureRunID: String
    /// The RoomDraft this artifact is a snapshot of, when applicable
    /// (roomdraft_json only — raw capture artifacts have no draft yet at
    /// the point they're recorded).
    public var roomDraftID: String?
    /// Where the source bytes live on disk, relative to a managed
    /// Application Support subdirectory — never a temporary/absolute
    /// capture URL, since RoomPlan's own temporary output location is not
    /// guaranteed to survive process restart.
    public var localSourceRelativePath: String
    public var contentType: String
    public var declaredSize: Int64
    /// SHA-256 hex checksum of the source bytes, computed once when the
    /// record is created — matches what `RequestArtifactUpload` expects
    /// and what `FinalizeArtifactUpload` verifies against server-side.
    public var checksum: String
    public var status: PendingArtifactUploadStatus
    /// Set once `RequestArtifactUpload`/`ResumeArtifactUpload` succeeds —
    /// the backend's `SpatialArtifact.ID`. The upload TOKEN itself is
    /// deliberately never persisted here (short-lived, security-sensitive
    /// — re-acquired via `ResumeArtifactUpload` using this ID whenever a
    /// fresh one is needed, including after an app relaunch).
    public var serverArtifactID: String?
    public var attemptCount: Int
    public var lastError: String?
    public var createdAt: Date
    public var updatedAt: Date

    public init(
        id: String,
        kind: PendingArtifactKind,
        captureRunID: String,
        roomDraftID: String? = nil,
        localSourceRelativePath: String,
        contentType: String,
        declaredSize: Int64,
        checksum: String,
        status: PendingArtifactUploadStatus = .notStarted,
        serverArtifactID: String? = nil,
        attemptCount: Int = 0,
        lastError: String? = nil,
        createdAt: Date,
        updatedAt: Date
    ) {
        self.id = id
        self.kind = kind
        self.captureRunID = captureRunID
        self.roomDraftID = roomDraftID
        self.localSourceRelativePath = localSourceRelativePath
        self.contentType = contentType
        self.declaredSize = declaredSize
        self.checksum = checksum
        self.status = status
        self.serverArtifactID = serverArtifactID
        self.attemptCount = attemptCount
        self.lastError = lastError
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    /// The deterministic ID every pending-upload record for a given
    /// capture run + artifact kind must use — this is what makes recording
    /// the same logical artifact twice idempotent rather than duplicating.
    public static func deterministicID(captureRunID: String, kind: PendingArtifactKind) -> String {
        "\(captureRunID)_\(kind.rawValue)"
    }
}
