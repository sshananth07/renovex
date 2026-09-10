import Foundation

/// File-backed `SpatialArtifactStore` (plan §RP3.5/§RP4B0), matching
/// `FileSpatialCaptureRepository`/`FileRoomDraftRepository`'s exact
/// pattern: one `JSONFileStore`-backed file per record, keyed by
/// `PendingArtifactUpload.id` (which is itself deterministic per
/// capture-run+kind — see that type's doc comment for why this makes
/// enqueue idempotent by construction).
public final class FileSpatialArtifactStore: SpatialArtifactStore {
    private let store: JSONFileStore

    public init(directoryURL: URL) throws {
        self.store = try JSONFileStore(directoryURL: directoryURL)
    }

    @discardableResult
    public func recordPendingUpload(_ upload: PendingArtifactUpload) async throws(SpatialArtifactStoreError) -> PendingArtifactUpload {
        let existing: PendingArtifactUpload?
        do {
            existing = try await store.read(PendingArtifactUpload.self, forKey: upload.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        if let existing {
            return existing
        }
        do {
            try await store.write(upload, forKey: upload.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        return upload
    }

    public func update(_ upload: PendingArtifactUpload) async throws(SpatialArtifactStoreError) {
        let existing: PendingArtifactUpload?
        do {
            existing = try await store.read(PendingArtifactUpload.self, forKey: upload.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        guard existing != nil else { throw .notFound }
        do {
            try await store.write(upload, forKey: upload.id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
    }

    public func find(id: String) async throws(SpatialArtifactStoreError) -> PendingArtifactUpload {
        let found: PendingArtifactUpload?
        do {
            found = try await store.read(PendingArtifactUpload.self, forKey: id)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        guard let found else { throw .notFound }
        return found
    }

    public func listForCaptureRun(_ captureRunID: String) async throws(SpatialArtifactStoreError) -> [PendingArtifactUpload] {
        let all: [PendingArtifactUpload]
        do {
            all = try await store.readAll(PendingArtifactUpload.self)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        return all.filter { $0.captureRunID == captureRunID }
    }

    public func listPending() async throws(SpatialArtifactStoreError) -> [PendingArtifactUpload] {
        let all: [PendingArtifactUpload]
        do {
            all = try await store.readAll(PendingArtifactUpload.self)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        return all.filter { $0.status != .finalized }
    }
}
