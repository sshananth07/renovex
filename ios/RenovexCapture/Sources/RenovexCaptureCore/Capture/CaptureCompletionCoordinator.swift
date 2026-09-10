import Foundation

/// Errors `CaptureCompletionCoordinator` may surface.
public enum CaptureCompletionError: Error, Equatable, Sendable {
    case adapterFailed(SpatialCaptureAdapterError)
    case captureRepositoryFailed(SpatialCaptureRepositoryError)
    case roomDraftRepositoryFailed(RoomDraftRepositoryError)
    case artifactStoreFailed(SpatialArtifactStoreError)
    case roomDraftEncodingFailed(reason: String)
}

/// Enforces plan §RP3's required persistence ordering for a completed
/// RoomPlan scan, extended by plan §RP3.5/§RP4B0 to also durably record the
/// artifact sync work a completed scan requires:
///
/// ```text
/// RoomPlan completion
///     -> persist source/capture result locally   (caller's responsibility —
///        see note below)
///     -> normalize RoomDraft                       (adapter)
///     -> persist RoomDraft                          (RoomDraftRepository)
///     -> record captured_room_data + roomdraft_json    (SpatialArtifactStore)
///        sync work durably
///     -> present Room Review                              (only after ALL
///                                                            of the above
///                                                            succeed)
/// ```
///
/// A completed scan must never exist only in ViewModel/process memory before
/// Room Review is presented (plan §RP3 §3) — this type is the single place
/// that enforces the ordering, so a caller cannot accidentally present Room
/// Review before persistence AND sync-eligibility recording actually
/// happened. Actual upload execution (the HTTP request/PUT/finalize
/// sequence) is deliberately NOT performed here — that is
/// `ArtifactSyncService`'s job, invoked separately and asynchronously so
/// the local-persistence success path never depends on network
/// availability (plan §RP3.5/§RP4B0: "a network failure must not lose the
/// scan").
///
/// Note on "persist source/capture result locally": that step is the raw
/// `CapturedRoomData` file write, which lives in the Apple-only
/// `RenovexCaptureRoomPlan` target (it touches real RoomPlan types) —
/// genuinely out of `RenovexCaptureCore`'s reach since this target must
/// stay Apple-framework-free. The RoomPlan-target caller is responsible for
/// having already durably written that file and supplies a
/// `CapturedSourceArtifactReference` describing it (path/size/checksum,
/// computed by the caller) so this coordinator can record its sync entry
/// without ever touching Apple types itself.
///
/// Recoverability: if `run`/`draft` persistence already succeeded on a
/// PRIOR call but recording sync work failed (or was never reached), a
/// retried `completeCapture` call for the same `run` must repair the
/// missing sync registration WITHOUT deleting or regenerating the already-
/// persisted scan. This works because `roomDraftRepository.create` and
/// `SpatialArtifactStore.recordPendingUpload` are both idempotent by
/// construction (a duplicate `create` for a capture that already has a
/// draft is rejected by the repository's own one-draft-per-capture
/// invariant and handled below by loading the existing draft instead; a
/// duplicate `recordPendingUpload` for the same deterministic ID returns
/// the existing record unchanged) — see each type's own doc comments.
public struct CaptureCompletionCoordinator: Sendable {
    private let captureRepository: any SpatialCaptureRepository
    private let roomDraftRepository: any RoomDraftRepository
    private let artifactStore: any SpatialArtifactStore
    /// Where immutable roomdraft_json snapshot files are written — a
    /// managed Application-Support-relative directory, never a temporary
    /// one (plan §RP3.5/§RP4B0: "serialize once -> persist those bytes ->
    /// SHA-256 those exact bytes -> upload those exact bytes";
    /// `ArtifactSyncService` uploads this file verbatim and must never
    /// re-encode the `RoomDraft` from `RoomDraftRepository`, since
    /// `JSONEncoder`'s key ordering is not guaranteed stable across
    /// separate `encode` calls even with `.sortedKeys` across Swift
    /// versions/platforms — only the ORIGINAL encoded bytes are ever
    /// checksummed and uploaded).
    private let roomDraftSnapshotDirectoryURL: URL

    public init(
        captureRepository: any SpatialCaptureRepository,
        roomDraftRepository: any RoomDraftRepository,
        artifactStore: any SpatialArtifactStore,
        roomDraftSnapshotDirectoryURL: URL
    ) {
        self.captureRepository = captureRepository
        self.roomDraftRepository = roomDraftRepository
        self.artifactStore = artifactStore
        self.roomDraftSnapshotDirectoryURL = roomDraftSnapshotDirectoryURL
    }

    /// Normalizes `rawResult` via `adapter`, persists the resulting
    /// `RoomDraft` under `run.id`, durably records `captured_room_data` (from
    /// `capturedSource`) and `roomdraft_json` (serialized from the draft
    /// just persisted) as pending artifact uploads, and returns the run
    /// with `roomDraftID` filled in — only once this returns successfully
    /// is Room Review safe to present for `run`.
    ///
    /// `run` must already be persisted (via `SpatialCaptureRepository
    /// .create`) before this is called — this method updates it, it does
    /// not create it, keeping "persist the run" and "persist its draft" as
    /// two separately observable, separately testable steps.
    ///
    /// Safe to call again for the same `run` after a prior partial failure
    /// — see this type's doc comment on recoverability.
    public func completeCapture(
        run: LocalCaptureRun,
        rawResult: SpatialCaptureRawResult,
        adapter: any SpatialCaptureAdapter,
        capturedSource: CapturedSourceArtifactReference
    ) async throws(CaptureCompletionError) -> LocalCaptureRun {
        let draft = try await existingOrNewlyPersistedDraft(run: run, rawResult: rawResult, adapter: adapter)

        // Serialize ONCE, persist those EXACT bytes to an immutable
        // snapshot file, then checksum/size that same file's contents —
        // never a value re-encoded later. .sortedKeys is defensive
        // (deterministic key order within one encode call is not the
        // guarantee that matters here); what actually makes the checksum
        // correct is that these bytes, written to disk right now, are the
        // only bytes ever hashed or uploaded for this snapshot.
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        let roomDraftJSONData: Data
        do {
            roomDraftJSONData = try encoder.encode(draft)
        } catch {
            throw .roomDraftEncodingFailed(reason: "\(error)")
        }

        let snapshotRelativePath = "\(run.id).json"
        do {
            try FileManager.default.createDirectory(at: roomDraftSnapshotDirectoryURL, withIntermediateDirectories: true)
            try roomDraftJSONData.write(
                to: roomDraftSnapshotDirectoryURL.appendingPathComponent(snapshotRelativePath),
                options: .atomic
            )
        } catch {
            throw .roomDraftEncodingFailed(reason: "failed to persist roomdraft_json snapshot: \(error)")
        }

        let now = Date()
        do {
            try await artifactStore.recordPendingUpload(PendingArtifactUpload(
                id: PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .capturedRoomData),
                kind: .capturedRoomData,
                captureRunID: run.id,
                localSourceRelativePath: capturedSource.relativePath,
                contentType: capturedSource.contentType,
                declaredSize: capturedSource.declaredSize,
                checksum: capturedSource.checksum,
                createdAt: now, updatedAt: now
            ))
            try await artifactStore.recordPendingUpload(PendingArtifactUpload(
                id: PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON),
                kind: .roomdraftJSON,
                captureRunID: run.id,
                roomDraftID: run.id,
                localSourceRelativePath: snapshotRelativePath,
                contentType: "application/json",
                declaredSize: Int64(roomDraftJSONData.count),
                checksum: ChecksumUtility.sha256Hex(roomDraftJSONData),
                createdAt: now, updatedAt: now
            ))
        } catch {
            throw .artifactStoreFailed(error)
        }

        var updated = run
        updated.roomDraftID = run.id
        updated.status = .pendingReview
        updated.updatedAt = Date()
        do {
            try await captureRepository.update(updated)
        } catch {
            throw .captureRepositoryFailed(error)
        }

        return updated
    }

    /// Returns `run.id`'s persisted `RoomDraft`, normalizing and persisting
    /// it fresh only if it does not already exist — this is what makes a
    /// retried `completeCapture` call recoverable rather than failing (or
    /// worse, silently normalizing a second time and discarding the first
    /// normalization's stable IDs) when a prior call already got as far as
    /// persisting the draft but failed on a later step.
    private func existingOrNewlyPersistedDraft(
        run: LocalCaptureRun,
        rawResult: SpatialCaptureRawResult,
        adapter: any SpatialCaptureAdapter
    ) async throws(CaptureCompletionError) -> RoomDraft {
        do {
            return try await roomDraftRepository.find(forCapture: run.id)
        } catch .notFound {
            // Fall through to normalize-and-persist below.
        } catch {
            throw .roomDraftRepositoryFailed(error)
        }

        let draft: RoomDraft
        do {
            draft = try adapter.makeRoomDraft(from: rawResult)
        } catch {
            throw .adapterFailed(error)
        }
        do {
            try await roomDraftRepository.create(draft, forCapture: run.id)
        } catch {
            throw .roomDraftRepositoryFailed(error)
        }
        return draft
    }
}
