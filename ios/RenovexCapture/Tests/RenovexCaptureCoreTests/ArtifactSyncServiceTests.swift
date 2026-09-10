import XCTest
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
@testable import RenovexCaptureCore

/// Plan §RP3.5/§RP4B0's failure/recovery test requirements: offline
/// capture completion, pending uploads persisted across restart, resume
/// after partial upload, retry after network/server failure, finalize
/// retry, duplicate prevention, stable capture-run/RoomDraft identity
/// after retry, isolation across multiple scans, missing optional
/// artifacts, corrupt/missing local state.
final class ArtifactSyncServiceTests: XCTestCase {
    private let baseURL = URL(string: "http://localhost:8080")!
    private var tempDirectory: URL!
    private var captureRepo: FileSpatialCaptureRepository!
    private var artifactStore: FileSpatialArtifactStore!
    private var snapshotDir: URL!
    private var sourceDir: URL!

    override func setUp() {
        super.setUp()
        MockURLProtocol.reset()
        tempDirectory = FileManager.default.temporaryDirectory.appendingPathComponent("RenovexSyncTests-\(UUID().uuidString)")
        snapshotDir = tempDirectory.appendingPathComponent("snapshots")
        sourceDir = tempDirectory.appendingPathComponent("sources")
        // swiftlint:disable:next force_try
        captureRepo = try! FileSpatialCaptureRepository(directoryURL: tempDirectory.appendingPathComponent("captures"))
        // swiftlint:disable:next force_try
        artifactStore = try! FileSpatialArtifactStore(directoryURL: tempDirectory.appendingPathComponent("artifacts"))
        try? FileManager.default.createDirectory(at: snapshotDir, withIntermediateDirectories: true)
        try? FileManager.default.createDirectory(at: sourceDir, withIntermediateDirectories: true)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: tempDirectory)
        tempDirectory = nil
        captureRepo = nil
        artifactStore = nil
        MockURLProtocol.reset()
        super.tearDown()
    }

    private func makeClient() -> RenovexAPIClient {
        RenovexAPIClient(baseURL: baseURL, session: .mocked())
    }

    private func makeService(apiClient: RenovexAPIClient) -> ArtifactSyncService {
        ArtifactSyncService(
            apiClient: apiClient, captureRepository: captureRepo, artifactStore: artifactStore,
            roomDraftSnapshotDirectoryURL: snapshotDir, capturedSourceDirectoryURL: sourceDir
        )
    }

    @discardableResult
    private func makeRun(id: String = UUID().uuidString) async throws -> LocalCaptureRun {
        let run = LocalCaptureRun(
            id: id, projectID: "project_1", spaceID: "space_1",
            provider: .roomplan, captureNumber: 1, status: .pendingReview,
            capturedAt: Date(), updatedAt: Date()
        )
        try await captureRepo.create(run)
        return run
    }

    @discardableResult
    private func makePendingUpload(
        captureRunID: String, kind: PendingArtifactKind, sourceBytes: Data,
        status: PendingArtifactUploadStatus = .notStarted, serverArtifactID: String? = nil
    ) async throws -> PendingArtifactUpload {
        let dir = kind == .roomdraftJSON ? snapshotDir! : sourceDir!
        let relativePath = "\(captureRunID)_\(kind.rawValue).bin"
        try sourceBytes.write(to: dir.appendingPathComponent(relativePath))

        let now = Date()
        let upload = PendingArtifactUpload(
            id: PendingArtifactUpload.deterministicID(captureRunID: captureRunID, kind: kind),
            kind: kind, captureRunID: captureRunID,
            localSourceRelativePath: relativePath,
            contentType: "application/octet-stream",
            declaredSize: Int64(sourceBytes.count),
            checksum: ChecksumUtility.sha256Hex(sourceBytes),
            status: status, serverArtifactID: serverArtifactID,
            createdAt: now, updatedAt: now
        )
        try await artifactStore.recordPendingUpload(upload)
        return upload
    }

    private func captureDTOBody(id: String, clientCaptureID: String) -> Data {
        #"{"id":"\#(id)","projectId":"project_1","spaceId":"space_1","status":"draft","provider":"roomplan","captureNumber":1,"clientCaptureId":"\#(clientCaptureID)","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"#.data(using: .utf8)!
    }

    private func artifactUploadResponseBody(artifactID: String, captureID: String, token: String) -> Data {
        #"{"artifact":{"id":"\#(artifactID)","captureId":"\#(captureID)","kind":"roomdraft_json","contentType":"application/octet-stream","declaredSize":10,"status":"pending","createdAt":"2026-01-01T00:00:00Z"},"uploadToken":"\#(token)"}"#.data(using: .utf8)!
    }

    private func finalizedArtifactBody(artifactID: String, captureID: String) -> Data {
        #"{"id":"\#(artifactID)","captureId":"\#(captureID)","kind":"roomdraft_json","contentType":"application/octet-stream","declaredSize":10,"actualSize":10,"status":"uploaded","createdAt":"2026-01-01T00:00:00Z","uploadedAt":"2026-01-01T00:00:01Z"}"#.data(using: .utf8)!
    }

    // MARK: - Happy path / offline capture completion

    /// A capture completed entirely offline (no network calls made at
    /// completion time) must still be fully syncable once syncPendingArtifacts
    /// is later invoked — proves local persistence never depended on
    /// network availability.
    func test_offlineCompletedCapture_syncsSuccessfullyWhenLaterInvoked() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_1", clientCaptureID: run.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_1", captureID: "server_capture_1", token: "token-abc"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_1", captureID: "server_capture_1"))]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected success, got \(String(describing: results))")
        }

        let finalRecord = try await artifactStore.find(id: PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON))
        XCTAssertEqual(finalRecord.status, .finalized)
        XCTAssertEqual(finalRecord.serverArtifactID, "artifact_1")

        let updatedRun = try await captureRepo.findByID(run.id)
        XCTAssertEqual(updatedRun.serverCaptureID, "server_capture_1")
    }

    // MARK: - Pending uploads persisted across restart

    func test_pendingUpload_survivesArtifactStoreRecreation() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)

        let freshStore = try FileSpatialArtifactStore(directoryURL: tempDirectory.appendingPathComponent("artifacts"))
        let reloaded = try await freshStore.listForCaptureRun(run.id)

        XCTAssertEqual(reloaded.count, 1)
        XCTAssertEqual(reloaded.first?.status, .notStarted)
    }

    // MARK: - Resume after partial upload

    /// A record already past .slotAcquired (bytes not yet confirmed
    /// uploaded) must call ResumeArtifactUpload, not RequestArtifactUpload
    /// again — proving resume reuses the existing server artifact rather
    /// than creating a second one.
    func test_resumeAfterPartialUpload_usesResumeNotRequestUpload() async throws {
        let run = try await makeRun()
        try await makePendingUpload(
            captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!,
            status: .slotAcquired, serverArtifactID: "artifact_existing"
        )
        var runWithServer = run
        runWithServer.serverCaptureID = "server_capture_1"
        try await captureRepo.update(runWithServer)

        // No /spatial/captures or /spatial/artifacts (request) stub at all —
        // if the service called either, the test fails via MockURLProtocol's
        // empty-queue-throws-fileDoesNotExist fallback.
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/resume"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_existing", captureID: "server_capture_1", token: "resumed-token"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_existing", captureID: "server_capture_1"))]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected success, got \(String(describing: results))")
        }
        XCTAssertTrue(MockURLProtocol.recordedRequests.contains { $0.url?.path == "/spatial/artifacts/artifact_existing/resume" })
        XCTAssertFalse(MockURLProtocol.recordedRequests.contains { $0.url?.path == "/spatial/captures" })
    }

    /// An expired upload token on resume is a NORMAL resume path (the
    /// backend always issues a fresh token), never treated as corruption.
    func test_resumeAfterExpiredToken_isNormalPath_notTreatedAsCorruption() async throws {
        let run = try await makeRun()
        try await makePendingUpload(
            captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!,
            status: .slotAcquired, serverArtifactID: "artifact_existing"
        )
        var runWithServer = run
        runWithServer.serverCaptureID = "server_capture_1"
        try await captureRepo.update(runWithServer)

        // ResumeArtifactUpload always succeeds with a fresh token even
        // though the client's own in-memory token (never persisted) would
        // have expired — this is exactly what makes resume "normal," not
        // an error path.
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/resume"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_existing", captureID: "server_capture_1", token: "brand-new-token"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_existing", captureID: "server_capture_1"))]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected success (expired token must not fail sync), got \(String(describing: results))")
        }
    }

    // MARK: - Retry after network/server failure

    func test_retryAfterNetworkFailure_marksFailedWithAttemptCountAndError() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_1", clientCaptureID: run.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 500, body: Data())]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        guard case .failure = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected failure, got \(String(describing: results))")
        }

        let record = try await artifactStore.find(id: PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON))
        XCTAssertEqual(record.status, .failed)
        XCTAssertEqual(record.attemptCount, 1)
        XCTAssertNotNil(record.lastError)
    }

    /// After a recorded failure, a subsequent successful sync call must
    /// still be able to complete the SAME record (not stuck permanently
    /// failed) — proving retry is possible, not just failure recording.
    func test_retryAfterFailure_eventuallySucceeds() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_1", clientCaptureID: run.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 500, body: Data())]

        let service = makeService(apiClient: makeClient())
        _ = await service.syncPendingArtifacts()

        // Second attempt: this time the request succeeds.
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_1", captureID: "server_capture_1", token: "token-abc"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_1", captureID: "server_capture_1"))]

        let results = await service.syncPendingArtifacts()
        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected eventual success, got \(String(describing: results))")
        }
    }

    // MARK: - Finalize retry / idempotent finalize

    /// A record already .uploaded (bytes PUT succeeded on a prior attempt,
    /// but finalize's response was lost) must retry ONLY finalize — never
    /// re-PUT the bytes.
    func test_finalizeRetry_doesNotReUploadBytes() async throws {
        let run = try await makeRun()
        try await makePendingUpload(
            captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!,
            status: .uploaded, serverArtifactID: "artifact_existing"
        )
        var runWithServer = run
        runWithServer.serverCaptureID = "server_capture_1"
        try await captureRepo.update(runWithServer)

        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/resume"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_existing", captureID: "server_capture_1", token: "resumed-token"))]
        // Deliberately NO stub for /spatial/artifacts/artifact_existing/content
        // — if the service PUTs bytes again, MockURLProtocol's empty-queue
        // fallback throws, failing the test.
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_existing", captureID: "server_capture_1"))]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected success, got \(String(describing: results))")
        }
        XCTAssertFalse(MockURLProtocol.recordedRequests.contains { $0.url?.path == "/spatial/artifacts/artifact_existing/content" })
    }

    // MARK: - Duplicate prevention / stable identity

    /// Two full syncPendingArtifacts passes over the SAME already-finalized
    /// record must not attempt any further network calls for it (it is
    /// excluded from listPending once finalized).
    func test_alreadyFinalizedRecord_isExcludedFromFurtherSyncAttempts() async throws {
        let run = try await makeRun()
        try await makePendingUpload(
            captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!,
            status: .finalized, serverArtifactID: "artifact_done"
        )

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        XCTAssertTrue(results.isEmpty, "a finalized record must not be re-synced")
        XCTAssertTrue(MockURLProtocol.recordedRequests.isEmpty)
    }

    /// A retried StartCapture (same clientCaptureId) must resolve to the
    /// SAME serverCaptureID, not a second one — proving sync-layer
    /// idempotency rides the backend's ClientCaptureID contract correctly.
    func test_ensureServerCaptureID_calledTwice_resolvesToSameServerCaptureIDWithoutSecondCall() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_1", clientCaptureID: run.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_1", captureID: "server_capture_1", token: "token-abc"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_1", captureID: "server_capture_1"))]

        let service = makeService(apiClient: makeClient())
        _ = await service.syncPendingArtifacts()

        // Second call: the run now has serverCaptureID persisted, so this
        // must short-circuit WITHOUT calling /spatial/captures again — but
        // nothing is left pending (already finalized), so no further
        // network calls should occur at all.
        let secondResults = await service.syncPendingArtifacts()
        XCTAssertTrue(secondResults.isEmpty)

        let captureCallCount = MockURLProtocol.recordedRequests.filter { $0.url?.path == "/spatial/captures" }.count
        XCTAssertEqual(captureCallCount, 1, "StartCapture must be called at most once per capture run across repeated sync attempts")
    }

    // MARK: - Isolation across multiple scans

    func test_multipleCaptureRuns_syncIndependently_oneFailureDoesNotBlockTheOther() async throws {
        let runA = try await makeRun()
        let runB = try await makeRun()
        try await makePendingUpload(captureRunID: runA.id, kind: .roomdraftJSON, sourceBytes: "draft-a".data(using: .utf8)!)
        try await makePendingUpload(captureRunID: runB.id, kind: .roomdraftJSON, sourceBytes: "draft-b".data(using: .utf8)!)

        // Use isolated single-run syncs (syncPendingArtifacts(forCaptureRun:))
        // rather than the batched all-runs call, so each run's stub queue is
        // deterministic regardless of dictionary iteration order — the
        // isolation guarantee this test proves does not depend on batching
        // behavior specifically.
        let service = makeService(apiClient: makeClient())
        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_A", clientCaptureID: runA.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 500, body: Data())]
        let resultsA = await service.syncPendingArtifacts(forCaptureRun: runA.id)
        guard case .failure = resultsA[PendingArtifactUpload.deterministicID(captureRunID: runA.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected runA to fail")
        }

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_B", clientCaptureID: runB.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_b", captureID: "server_capture_B", token: "token-b"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_b/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_b/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_b", captureID: "server_capture_B"))]
        let resultsB = await service.syncPendingArtifacts(forCaptureRun: runB.id)
        guard case .success = resultsB[PendingArtifactUpload.deterministicID(captureRunID: runB.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected runB to succeed independently of runA's failure")
        }

        let recordA = try await artifactStore.find(id: PendingArtifactUpload.deterministicID(captureRunID: runA.id, kind: .roomdraftJSON))
        let recordB = try await artifactStore.find(id: PendingArtifactUpload.deterministicID(captureRunID: runB.id, kind: .roomdraftJSON))
        XCTAssertEqual(recordA.status, .failed)
        XCTAssertEqual(recordB.status, .finalized)
    }

    // MARK: - Missing optional USDZ

    /// A capture run with NO usdz pending-upload record (never recorded,
    /// since USDZ is optional/not always produced) must sync successfully
    /// using only the records that DO exist — no attempt to fabricate or
    /// require one.
    func test_missingOptionalUSDZ_doesNotBlockSyncOfOtherArtifacts() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)
        // No .usdz record created at all.

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_1", clientCaptureID: run.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_1", captureID: "server_capture_1", token: "token-abc"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_1", captureID: "server_capture_1"))]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        XCTAssertEqual(results.count, 1, "only the recorded roomdraft_json artifact should be present — no phantom usdz record")
        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected success")
        }
    }

    // MARK: - Server-backed reopen foundation (plan §RP3.5/§RP4B0 §7)

    /// Proves the backend now possesses enough durable data to reconstruct
    /// the same captured draft this device synced: after a full sync, both
    /// `GetCapture` (by the resolved serverCaptureID) and `ListArtifacts`
    /// (showing the roomdraft_json artifact as uploaded) succeed using the
    /// EXISTING shipped routes — no new backend contract was required to
    /// prove this.
    func test_afterFullSync_backendHasEnoughDataForServerBackedReopen() async throws {
        let run = try await makeRun()
        try await makePendingUpload(captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!)

        MockURLProtocol.stubsByPath["/spatial/captures"] = [.init(statusCode: 200, body: captureDTOBody(id: "server_capture_1", clientCaptureID: run.id))]
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_1", captureID: "server_capture_1", token: "token-abc"))]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/content"] = [.init(statusCode: 200)]
        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_1/finalize"] = [.init(statusCode: 200, body: finalizedArtifactBody(artifactID: "artifact_1", captureID: "server_capture_1"))]

        let apiClient = makeClient()
        let service = makeService(apiClient: apiClient)
        let results = await service.syncPendingArtifacts()
        guard case .success = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected sync success as a precondition")
        }

        let updatedRun = try await captureRepo.findByID(run.id)
        let serverCaptureID = try XCTUnwrap(updatedRun.serverCaptureID)

        // Reopen path: GetCapture by the resolved server ID.
        MockURLProtocol.stubsByPath["/spatial/captures/\(serverCaptureID)"] = [.init(statusCode: 200, body: captureDTOBody(id: serverCaptureID, clientCaptureID: run.id))]
        let fetchedCapture = try await apiClient.getCapture(id: serverCaptureID)
        XCTAssertEqual(fetchedCapture.id, serverCaptureID)
        XCTAssertEqual(fetchedCapture.clientCaptureID, run.id)

        // Reopen path: ListArtifacts shows roomdraft_json as uploaded.
        let artifactsListBody = "[\(String(data: finalizedArtifactBody(artifactID: "artifact_1", captureID: serverCaptureID), encoding: .utf8)!)]".data(using: .utf8)!
        MockURLProtocol.stubsByPath["/spatial/artifacts"] = [.init(statusCode: 200, body: artifactsListBody)]
        let artifacts = try await apiClient.listArtifacts(captureID: serverCaptureID)
        XCTAssertEqual(artifacts.count, 1)
        XCTAssertEqual(artifacts.first?.status, "uploaded")
    }

    // MARK: - Corrupt/missing local artifact state

    /// If the pending-upload record's referenced source file is missing on
    /// disk (e.g. deleted out-of-band, or a corrupt/incomplete write), sync
    /// must fail that record with a specific sourceFileMissing error rather
    /// than crashing or silently finalizing with no bytes.
    func test_missingLocalSourceFile_failsWithSourceFileMissingError() async throws {
        let run = try await makeRun()
        let upload = try await makePendingUpload(
            captureRunID: run.id, kind: .roomdraftJSON, sourceBytes: "draft-bytes".data(using: .utf8)!,
            status: .slotAcquired, serverArtifactID: "artifact_existing"
        )
        // Delete the source file out from under the record.
        try FileManager.default.removeItem(at: snapshotDir.appendingPathComponent(upload.localSourceRelativePath))

        var runWithServer = run
        runWithServer.serverCaptureID = "server_capture_1"
        try await captureRepo.update(runWithServer)

        MockURLProtocol.stubsByPath["/spatial/artifacts/artifact_existing/resume"] = [.init(statusCode: 200, body: artifactUploadResponseBody(artifactID: "artifact_existing", captureID: "server_capture_1", token: "resumed-token"))]

        let service = makeService(apiClient: makeClient())
        let results = await service.syncPendingArtifacts()

        guard case .failure(let error) = results[PendingArtifactUpload.deterministicID(captureRunID: run.id, kind: .roomdraftJSON)] else {
            return XCTFail("expected failure")
        }
        guard case .sourceFileMissing = error else {
            return XCTFail("expected .sourceFileMissing, got \(error)")
        }
    }
}
