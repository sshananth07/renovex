import Foundation

/// File-backed `RoomDraftRepository`. Keyed directly by captureID (one
/// draft per capture, per the protocol's documented invariant) rather than a
/// separately generated draft id — this is what makes `find(forCapture:)` a
/// direct lookup instead of a scan, and makes "Continue Review never creates
/// a duplicate" structurally true: there is only ever one file a given
/// captureID can resolve to.
public final class FileRoomDraftRepository: RoomDraftRepository {
    private let store: JSONFileStore

    public init(directoryURL: URL) throws {
        self.store = try JSONFileStore(directoryURL: directoryURL)
    }

    public func create(_ draft: RoomDraft, forCapture captureID: String) async throws(RoomDraftRepositoryError) {
        let existing: RoomDraft?
        do {
            existing = try await store.read(RoomDraft.self, forKey: captureID)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        guard existing == nil else {
            throw .underlyingStorageFailure(reason: "capture \(captureID) already has a persisted RoomDraft; use update")
        }
        do {
            try await store.write(draft, forKey: captureID)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
    }

    public func update(_ draft: RoomDraft, forCapture captureID: String) async throws(RoomDraftRepositoryError) {
        do {
            try await store.write(draft, forKey: captureID)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
    }

    public func find(forCapture captureID: String) async throws(RoomDraftRepositoryError) -> RoomDraft {
        let found: RoomDraft?
        do {
            found = try await store.read(RoomDraft.self, forKey: captureID)
        } catch {
            throw .underlyingStorageFailure(reason: "\(error)")
        }
        guard let found else { throw .notFound }
        return found
    }
}
