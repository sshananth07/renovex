import Foundation

/// Errors a `SpatialCaptureRepository` conformer may surface.
public enum SpatialCaptureRepositoryError: Error, Equatable, Sendable {
    case notFound
    case underlyingStorageFailure(reason: String)
}

/// Persists `LocalCaptureRun`s (plan §RP3 §4: "the UI must not write
/// persistence files/database records directly"). A capture run is created
/// once per scan and never overwritten by a later scan — many runs may exist
/// for one Space (design spec §8.22).
///
/// This protocol lives in Core so it is swappable/testable the same way
/// `SpatialCaptureProvider` is: a fake in-memory conformer can substitute
/// for `FileSpatialCaptureRepository` in ViewModel tests without touching
/// the filesystem.
public protocol SpatialCaptureRepository: Sendable {
    /// Persists a new capture run. Callers are responsible for computing a
    /// unique `id` and the correct `captureNumber` (see
    /// `nextCaptureNumber(forSpace:)`) — this method does not invent either,
    /// so a caller cannot accidentally create two runs with the same number
    /// for one Space.
    func create(_ run: LocalCaptureRun) async throws(SpatialCaptureRepositoryError)

    /// Updates an existing capture run in place (e.g. recording
    /// `roomDraftID`/`serverCaptureID`/`status` after persistence steps
    /// complete). Throws `.notFound` if no run with this id exists yet —
    /// callers must `create` before `update`.
    func update(_ run: LocalCaptureRun) async throws(SpatialCaptureRepositoryError)

    /// Returns every capture run for spaceID, most recent first — the Scan
    /// History listing (design spec §8.22: "Space -> Scan #1, #2, #3...").
    func listRuns(spaceID: String) async throws(SpatialCaptureRepositoryError) -> [LocalCaptureRun]

    /// Returns one run by its local id, or `.notFound`.
    func findByID(_ id: String) async throws(SpatialCaptureRepositoryError) -> LocalCaptureRun

    /// The next 1-based captureNumber to assign for a new run in spaceID —
    /// `(count of existing runs for spaceID) + 1`, matching the backend's
    /// `spatial.Service.StartCapture` numbering so the two stay consistent
    /// once synced.
    func nextCaptureNumber(forSpace spaceID: String) async throws(SpatialCaptureRepositoryError) -> Int
}

public extension SpatialCaptureRepository {
    /// Default implementation in terms of `listRuns` — a conformer may
    /// override this if it can compute the count more cheaply, but no
    /// conformer is required to.
    func nextCaptureNumber(forSpace spaceID: String) async throws(SpatialCaptureRepositoryError) -> Int {
        try await listRuns(spaceID: spaceID).count + 1
    }
}
