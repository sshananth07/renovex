import Foundation

/// Errors a `SpatialArtifactStore` conformer may surface.
public enum SpatialArtifactStoreError: Error, Equatable, Sendable {
    case notFound
    case underlyingStorageFailure(reason: String)
}

/// Durable client-side knowledge of artifact upload state (plan
/// §RP3.5/§RP4B0, closing the RP3 `SpatialArtifactStore` gap). Owns
/// `PendingArtifactUpload` records ONLY — never RoomDraft domain
/// semantics, which stay in `RoomDraftRepository`; never the actual HTTP
/// upload logic, which lives in `ArtifactSyncService`.
///
/// Protocol-backed/swappable in the same style as
/// `SpatialCaptureRepository`/`RoomDraftRepository` (a fake in-memory
/// conformer can substitute in ViewModel/coordinator tests without
/// touching the filesystem).
public protocol SpatialArtifactStore: Sendable {
    /// Records a new pending upload, OR — if a record with the same
    /// deterministic ID (`PendingArtifactUpload.deterministicID`) already
    /// exists — returns the EXISTING record unchanged rather than creating
    /// a duplicate or overwriting in-progress state. This is what makes
    /// recording the same logical artifact twice (e.g. a retried
    /// `CaptureCompletionCoordinator.completeCapture` call after local
    /// persistence succeeded but this step previously failed) safe and
    /// recoverable rather than destructive.
    @discardableResult
    func recordPendingUpload(_ upload: PendingArtifactUpload) async throws(SpatialArtifactStoreError) -> PendingArtifactUpload

    /// Updates an existing record in place (status/serverArtifactID/
    /// attemptCount/lastError transitions). Throws `.notFound` if no
    /// record with this id exists yet.
    func update(_ upload: PendingArtifactUpload) async throws(SpatialArtifactStoreError)

    func find(id: String) async throws(SpatialArtifactStoreError) -> PendingArtifactUpload

    /// Returns every pending-upload record for one capture run — used both
    /// to drive sync for that run and to prove one scan's upload state
    /// never attaches to another (plan §RP3.5/§RP4B0's isolation
    /// requirement).
    func listForCaptureRun(_ captureRunID: String) async throws(SpatialArtifactStoreError) -> [PendingArtifactUpload]

    /// Returns every record not yet `.finalized`, across every capture
    /// run — the full sync worklist `ArtifactSyncService.syncPendingArtifacts()`
    /// drives to completion.
    func listPending() async throws(SpatialArtifactStoreError) -> [PendingArtifactUpload]
}
