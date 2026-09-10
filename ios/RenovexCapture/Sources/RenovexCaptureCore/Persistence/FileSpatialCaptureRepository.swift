import Foundation

/// File-backed `SpatialCaptureRepository` (plan §RP3 §17: platform-neutral
/// repository semantics, storage technology not yet finalized behind
/// Xcode/Apple persistence framework access — this stays a plain,
/// Windows-testable JSON store so the interface is proven correct now and
/// swappable for a Core Data/SwiftData adapter later without touching
/// `RoomDraft`/UI).
public final class FileSpatialCaptureRepository: SpatialCaptureRepository {
    private let store: JSONFileStore

    /// - Parameter directoryURL: where run JSON files are written. Callers
    ///   pass a real Application Support subdirectory in the app target; a
    ///   test passes an isolated temporary directory so tests never share
    ///   or leak state.
    public init(directoryURL: URL) throws {
        self.store = try JSONFileStore(directoryURL: directoryURL)
    }

    public func create(_ run: LocalCaptureRun) async throws(SpatialCaptureRepositoryError) {
        do {
            try await store.write(run, forKey: run.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
    }

    public func update(_ run: LocalCaptureRun) async throws(SpatialCaptureRepositoryError) {
        let existing: LocalCaptureRun?
        do {
            existing = try await store.read(LocalCaptureRun.self, forKey: run.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        guard existing != nil else { throw .notFound }
        do {
            try await store.write(run, forKey: run.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
    }

    public func listRuns(spaceID: String) async throws(SpatialCaptureRepositoryError) -> [LocalCaptureRun] {
        let all: [LocalCaptureRun]
        do {
            all = try await store.readAll(LocalCaptureRun.self)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        return all
            .filter { $0.spaceID == spaceID }
            .sorted { $0.captureNumber > $1.captureNumber }
    }

    public func findByID(_ id: String) async throws(SpatialCaptureRepositoryError) -> LocalCaptureRun {
        let found: LocalCaptureRun?
        do {
            found = try await store.read(LocalCaptureRun.self, forKey: id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        guard let found else { throw .notFound }
        return found
    }
}
