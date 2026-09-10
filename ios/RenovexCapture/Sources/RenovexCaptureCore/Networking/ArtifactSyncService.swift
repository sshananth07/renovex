import Foundation

/// Errors `ArtifactSyncService` may surface for one sync attempt.
public enum ArtifactSyncError: Error, Equatable, Sendable {
    case captureRepositoryFailed(SpatialCaptureRepositoryError)
    case artifactStoreFailed(SpatialArtifactStoreError)
    case networkFailed(reason: String)
    case sourceFileMissing(path: String)
}

/// Drives durably-recorded `PendingArtifactUpload` records
/// (`SpatialArtifactStore`) to completion against the backend's existing
/// resumable artifact-upload pipeline (plan §RP3.5/§RP4B0). UI-independent
/// and lifecycle-independent by design — this type performs real HTTP
/// upload/resume/finalize execution and deterministic retry behavior when
/// its entry points are called, but does NOT schedule itself; a future
/// `BGTaskScheduler` integration (not built here — Xcode/device-only, out
/// of RP3.5/RP4B0 scope) would call these same primitives, not a
/// second/parallel upload mechanism. Never claim automatic background
/// synchronization exists until that lifecycle integration is implemented
/// and Xcode/device verified.
///
/// Reuses the EXISTING backend flow exactly:
/// `RequestArtifactUpload`/`ResumeArtifactUpload` -> PUT bytes (token-
/// authenticated) -> `FinalizeArtifactUpload`. No second/parallel upload
/// protocol is introduced.
public struct ArtifactSyncService: Sendable {
    private let apiClient: RenovexAPIClient
    private let captureRepository: any SpatialCaptureRepository
    private let artifactStore: any SpatialArtifactStore
    private let roomDraftSnapshotDirectoryURL: URL
    private let capturedSourceDirectoryURL: URL

    public init(
        apiClient: RenovexAPIClient,
        captureRepository: any SpatialCaptureRepository,
        artifactStore: any SpatialArtifactStore,
        roomDraftSnapshotDirectoryURL: URL,
        capturedSourceDirectoryURL: URL
    ) {
        self.apiClient = apiClient
        self.captureRepository = captureRepository
        self.artifactStore = artifactStore
        self.roomDraftSnapshotDirectoryURL = roomDraftSnapshotDirectoryURL
        self.capturedSourceDirectoryURL = capturedSourceDirectoryURL
    }

    /// Drives every not-yet-finalized `PendingArtifactUpload` across every
    /// capture run to completion, one at a time. Callable on demand (app
    /// launch, a manual "Retry" action, or a future background-task
    /// trigger) — safe to call repeatedly; each record's own state
    /// determines what work (if any) remains for it. Returns a per-record
    /// result rather than throwing on the first failure, so one artifact's
    /// transient failure never blocks the rest of the worklist from
    /// syncing.
    public func syncPendingArtifacts() async -> [String: Result<Void, ArtifactSyncError>] {
        let pending: [PendingArtifactUpload]
        do {
            pending = try await artifactStore.listPending()
        } catch {
            return [:]
        }

        var results: [String: Result<Void, ArtifactSyncError>] = [:]
        // Group by captureRunID so every artifact for one run resolves the
        // same serverCaptureID once rather than racing/duplicating
        // ensureServerCapture calls per artifact.
        let byRun = Dictionary(grouping: pending, by: { $0.captureRunID })
        for (captureRunID, uploads) in byRun {
            let serverCaptureIDResult = await ensureServerCaptureID(captureRunID: captureRunID)
            switch serverCaptureIDResult {
            case .failure(let error):
                for upload in uploads { results[upload.id] = .failure(error) }
                continue
            case .success(let serverCaptureID):
                for upload in uploads {
                    results[upload.id] = await syncOne(upload, serverCaptureID: serverCaptureID)
                }
            }
        }
        return results
    }

    /// Drives every not-yet-finalized upload for exactly one capture run —
    /// used when a caller only cares about one scan's sync state (e.g.
    /// right after `CaptureCompletionCoordinator.completeCapture` returns).
    public func syncPendingArtifacts(forCaptureRun captureRunID: String) async -> [String: Result<Void, ArtifactSyncError>] {
        let pending: [PendingArtifactUpload]
        do {
            pending = try await artifactStore.listForCaptureRun(captureRunID).filter { $0.status != .finalized }
        } catch {
            return [:]
        }
        guard !pending.isEmpty else { return [:] }

        let serverCaptureIDResult = await ensureServerCaptureID(captureRunID: captureRunID)
        switch serverCaptureIDResult {
        case .failure(let error):
            return Dictionary(uniqueKeysWithValues: pending.map { ($0.id, .failure(error)) })
        case .success(let serverCaptureID):
            var results: [String: Result<Void, ArtifactSyncError>] = [:]
            for upload in pending {
                results[upload.id] = await syncOne(upload, serverCaptureID: serverCaptureID)
            }
            return results
        }
    }

    // MARK: - Capture sync

    /// Resolves `captureRunID`'s server-side `SpatialCapture.id`, calling
    /// `StartCapture` with this run's stable `LocalCaptureRun.id` as
    /// `clientCaptureId` if it has not been synced yet — idempotent by
    /// construction (a retried call after a lost response returns the SAME
    /// server capture rather than creating a duplicate, per the backend's
    /// ClientCaptureID contract). Persists the resolved `serverCaptureID`
    /// back onto the local run so subsequent calls short-circuit.
    private func ensureServerCaptureID(captureRunID: String) async -> Result<String, ArtifactSyncError> {
        let run: LocalCaptureRun
        do {
            run = try await captureRepository.findByID(captureRunID)
        } catch {
            return .failure(.captureRepositoryFailed(error))
        }

        if let existing = run.serverCaptureID {
            return .success(existing)
        }

        let dto: SpatialCaptureDTO
        do {
            dto = try await apiClient.startCapture(StartCaptureRequestBody(
                projectID: run.projectID, spaceID: run.spaceID,
                provider: run.provider.rawValue, clientCaptureID: run.id
            ))
        } catch {
            return .failure(.networkFailed(reason: "\(error)"))
        }

        var updated = run
        updated.serverCaptureID = dto.id
        updated.updatedAt = Date()
        do {
            try await captureRepository.update(updated)
        } catch {
            return .failure(.captureRepositoryFailed(error))
        }
        return .success(dto.id)
    }

    // MARK: - One artifact

    private func syncOne(_ upload: PendingArtifactUpload, serverCaptureID: String) async -> Result<Void, ArtifactSyncError> {
        var current = upload

        // 1. Ensure a server artifact slot + valid upload token exist.
        let slotResult = await ensureUploadSlot(&current, serverCaptureID: serverCaptureID)
        guard case .success(let token) = slotResult else {
            if case .failure(let error) = slotResult { return await recordFailure(current, error) }
            return .failure(.networkFailed(reason: "unreachable"))
        }

        // 2. PUT bytes, unless already past that step (status already
        //    .uploaded or .finalized from a prior attempt — resume must not
        //    re-upload bytes that already landed).
        if current.status == .slotAcquired {
            let sourceURL = sourceFileURL(for: current)
            let content: Data
            do {
                content = try Data(contentsOf: sourceURL)
            } catch {
                return await recordFailure(current, .sourceFileMissing(path: sourceURL.path))
            }
            do {
                try await apiClient.putArtifactContent(artifactID: current.serverArtifactID!, uploadToken: token, content: content)
            } catch {
                return await recordFailure(current, .networkFailed(reason: "\(error)"))
            }
            current.status = .uploaded
            current.updatedAt = Date()
            do {
                try await artifactStore.update(current)
            } catch {
                return await recordFailure(current, .artifactStoreFailed(error))
            }
        }

        // 3. Finalize — idempotent server-side even if this artifact was
        //    already finalized by a prior attempt whose response was lost.
        do {
            _ = try await apiClient.finalizeArtifactUpload(artifactID: current.serverArtifactID!, uploadToken: token)
        } catch {
            return await recordFailure(current, .networkFailed(reason: "\(error)"))
        }

        current.status = .finalized
        current.updatedAt = Date()
        do {
            try await artifactStore.update(current)
        } catch {
            return .failure(.artifactStoreFailed(error))
        }
        return .success(())
    }

    /// Ensures `upload` has a `serverArtifactID` and returns a fresh upload
    /// token for it — via `RequestArtifactUpload` (first attempt) or
    /// `ResumeArtifactUpload` (every subsequent attempt, including after
    /// the in-memory token from a prior attempt has expired or the app was
    /// relaunched — an expired token is a normal resume path, never treated
    /// as upload corruption or a reason to create a new artifact, since
    /// `ResumeArtifactUpload` always issues a fresh token for an authorized
    /// caller using the persisted `serverArtifactID`).
    private func ensureUploadSlot(_ upload: inout PendingArtifactUpload, serverCaptureID: String) async -> Result<String, ArtifactSyncError> {
        if let serverArtifactID = upload.serverArtifactID {
            do {
                let response = try await apiClient.resumeArtifactUpload(artifactID: serverArtifactID)
                if upload.status == .notStarted {
                    upload.status = .slotAcquired
                    upload.updatedAt = Date()
                    try await artifactStore.update(upload)
                }
                return .success(response.uploadToken)
            } catch {
                return .failure(.networkFailed(reason: "\(error)"))
            }
        }

        do {
            let response = try await apiClient.requestArtifactUpload(RequestArtifactUploadRequestBody(
                captureID: serverCaptureID, kind: upload.kind.rawValue, contentType: upload.contentType,
                declaredSize: upload.declaredSize, checksum: upload.checksum
            ))
            upload.serverArtifactID = response.artifact.id
            upload.status = .slotAcquired
            upload.updatedAt = Date()
            try await artifactStore.update(upload)
            return .success(response.uploadToken)
        } catch let error as ArtifactSyncError {
            return .failure(error)
        } catch {
            return .failure(.networkFailed(reason: "\(error)"))
        }
    }

    private func sourceFileURL(for upload: PendingArtifactUpload) -> URL {
        switch upload.kind {
        case .roomdraftJSON:
            return roomDraftSnapshotDirectoryURL.appendingPathComponent(upload.localSourceRelativePath)
        case .capturedRoomData, .roomplanProcessed, .usdz:
            return capturedSourceDirectoryURL.appendingPathComponent(upload.localSourceRelativePath)
        }
    }

    private func recordFailure(_ upload: PendingArtifactUpload, _ error: ArtifactSyncError) async -> Result<Void, ArtifactSyncError> {
        var failed = upload
        failed.status = .failed
        failed.attemptCount += 1
        failed.lastError = "\(error)"
        failed.updatedAt = Date()
        try? await artifactStore.update(failed)
        return .failure(error)
    }
}
