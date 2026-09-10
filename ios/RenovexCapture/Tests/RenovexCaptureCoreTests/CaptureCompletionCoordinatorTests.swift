import XCTest
@testable import RenovexCaptureCore

/// Proves plan §RP3's required ordering (normalize -> persist RoomDraft ->
/// link to the capture run) and plan §RP3.5/§RP4B0's extension (durably
/// record captured_room_data + roomdraft_json sync work before reporting
/// success) using the real file-backed repositories and the existing
/// `FixtureCaptureAdapter` — no RoomPlan/Apple dependency, fully
/// Windows-testable.
final class CaptureCompletionCoordinatorTests: XCTestCase {
    private var tempDirectory: URL!
    private var captureRepo: FileSpatialCaptureRepository!
    private var draftRepo: FileRoomDraftRepository!
    private var artifactStore: FileSpatialArtifactStore!

    override func setUp() {
        super.setUp()
        tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent("RenovexCaptureTests-\(UUID().uuidString)")
        // swiftlint:disable:next force_try — a fresh temp directory failing
        // to be created is an environment failure, not a test case to
        // assert on; matches this suite's other setUp()s, which are
        // non-throwing (no setUpWithError() precedent in this test target).
        captureRepo = try! FileSpatialCaptureRepository(directoryURL: tempDirectory.appendingPathComponent("captures"))
        draftRepo = try! FileRoomDraftRepository(directoryURL: tempDirectory.appendingPathComponent("drafts"))
        artifactStore = try! FileSpatialArtifactStore(directoryURL: tempDirectory.appendingPathComponent("artifacts"))
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: tempDirectory)
        tempDirectory = nil
        captureRepo = nil
        draftRepo = nil
        artifactStore = nil
        super.tearDown()
    }

    private func makeRun(id: String = UUID().uuidString, captureNumber: Int = 1) -> LocalCaptureRun {
        LocalCaptureRun(
            id: id, projectID: "project_1", spaceID: "space_1",
            provider: .fixture, captureNumber: captureNumber, status: .pendingReview,
            capturedAt: Date(), updatedAt: Date()
        )
    }

    private func makeCoordinator() -> CaptureCompletionCoordinator {
        CaptureCompletionCoordinator(
            captureRepository: captureRepo, roomDraftRepository: draftRepo, artifactStore: artifactStore,
            roomDraftSnapshotDirectoryURL: tempDirectory.appendingPathComponent("roomdraft-snapshots")
        )
    }

    private func makeCapturedSource() -> CapturedSourceArtifactReference {
        CapturedSourceArtifactReference(
            relativePath: "raw-captures/sample.dat",
            contentType: "application/octet-stream",
            declaredSize: 1024,
            checksum: "deadbeef"
        )
    }

    func test_completeCapture_persistsDraftAndLinksItToTheRun() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let updated = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        XCTAssertEqual(updated.roomDraftID, run.id)
        XCTAssertEqual(updated.status, .pendingReview)

        let persistedDraft = try await draftRepo.find(forCapture: run.id)
        XCTAssertEqual(persistedDraft.walls.count, 4)
    }

    /// A completed scan must not exist only in memory before Room Review —
    /// proven by recreating both repositories (simulating an app relaunch)
    /// after completeCapture returns, and finding the draft still there.
    func test_completedCapture_survivesRepositoryRecreation() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)
        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let freshCaptureRepo = try FileSpatialCaptureRepository(directoryURL: tempDirectory.appendingPathComponent("captures"))
        let freshDraftRepo = try FileRoomDraftRepository(directoryURL: tempDirectory.appendingPathComponent("drafts"))

        let reloadedRun = try await freshCaptureRepo.findByID(run.id)
        let reloadedDraft = try await freshDraftRepo.find(forCapture: run.id)

        XCTAssertEqual(reloadedRun.roomDraftID, run.id)
        XCTAssertEqual(reloadedDraft.walls.count, 4)
    }

    /// "Continue Review loads the SAME persisted draft... does not create
    /// another capture." Simulated here as: after completion, looking the
    /// draft up again by captureID must never create a second draft file
    /// or change the wall's stable ID.
    func test_continueReview_reloadsSameDraft_withoutDuplication() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)
        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let firstReview = try await draftRepo.find(forCapture: run.id)
        let secondReview = try await draftRepo.find(forCapture: run.id)

        XCTAssertEqual(firstReview, secondReview)
        XCTAssertEqual(Set(firstReview.walls.map(\.id)), Set(secondReview.walls.map(\.id)))
    }

    /// "Scan Again creates a new run... does not overwrite the current/
    /// confirmed historical run." Proven at the repository level: a second
    /// completeCapture for a NEW run (simulating Scan Again) must leave the
    /// first run's draft untouched.
    func test_scanAgain_createsNewRun_doesNotOverwritePreviousRun() async throws {
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let firstRun = makeRun(captureNumber: 1)
        try await captureRepo.create(firstRun)
        _ = try await coordinator.completeCapture(run: firstRun, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let secondRun = makeRun(captureNumber: 2)
        try await captureRepo.create(secondRun)
        _ = try await coordinator.completeCapture(run: secondRun, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let allRuns = try await captureRepo.listRuns(spaceID: "space_1")
        XCTAssertEqual(allRuns.count, 2, "Scan Again must create a new run, not replace the existing one")

        let firstDraftStillIntact = try await draftRepo.find(forCapture: firstRun.id)
        XCTAssertEqual(firstDraftStillIntact.walls.count, 4)
    }

    // MARK: - RP3.5/RP4B0: artifact sync recording

    func test_completeCapture_recordsCapturedRoomDataPendingUpload() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)
        let source = makeCapturedSource()

        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: source)

        let pending = try await artifactStore.listForCaptureRun(run.id)
        let capturedRoomData = pending.first { $0.kind == .capturedRoomData }
        XCTAssertNotNil(capturedRoomData)
        XCTAssertEqual(capturedRoomData?.localSourceRelativePath, source.relativePath)
        XCTAssertEqual(capturedRoomData?.checksum, source.checksum)
        XCTAssertEqual(capturedRoomData?.status, .notStarted)
    }

    func test_completeCapture_recordsRoomDraftJSONPendingUpload() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let pending = try await artifactStore.listForCaptureRun(run.id)
        let roomDraftJSON = pending.first { $0.kind == .roomdraftJSON }
        XCTAssertNotNil(roomDraftJSON)
        XCTAssertEqual(roomDraftJSON?.roomDraftID, run.id)
        XCTAssertEqual(roomDraftJSON?.contentType, "application/json")
        XCTAssertGreaterThan(roomDraftJSON?.declaredSize ?? 0, 0)
    }

    /// Plan §RP3.5/§RP4B0: "serialize once -> persist those bytes ->
    /// SHA-256 those exact bytes -> upload those exact bytes." Proven here
    /// by reading back the ACTUAL immutable snapshot file the coordinator
    /// wrote to disk and confirming its checksum matches the recorded
    /// value — never by independently re-encoding the draft, since
    /// `JSONEncoder`'s output for equivalent Codable values is not
    /// guaranteed byte-identical across separate `encode` calls (this
    /// test's own history: an earlier version of this test that DID
    /// re-encode was flaky for exactly that reason).
    func test_completeCapture_roomDraftJSONChecksumMatchesPersistedSnapshotFile() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let pending = try await artifactStore.listForCaptureRun(run.id)
        let roomDraftJSON = try XCTUnwrap(pending.first { $0.kind == .roomdraftJSON })

        let snapshotURL = tempDirectory.appendingPathComponent("roomdraft-snapshots").appendingPathComponent(roomDraftJSON.localSourceRelativePath)
        let actualBytes = try Data(contentsOf: snapshotURL)

        XCTAssertEqual(roomDraftJSON.checksum, ChecksumUtility.sha256Hex(actualBytes))
        XCTAssertEqual(roomDraftJSON.declaredSize, Int64(actualBytes.count))
    }

    /// Recoverability: if a prior call already persisted the run/draft but
    /// never reached sync recording, a retried completeCapture call must
    /// repair the missing sync registration WITHOUT deleting/regenerating
    /// the already-persisted draft (i.e. stable IDs survive).
    func test_completeCapture_retryAfterPriorPersistence_repairsSyncRecordsWithoutRegeneratingDraft() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        // Simulate a first call that persisted the draft successfully.
        let draft = try FixtureCaptureAdapter().makeRoomDraft(from: rawResult)
        try await draftRepo.create(draft, forCapture: run.id)
        let firstPersistedDraft = try await draftRepo.find(forCapture: run.id)

        // No sync records exist yet — simulating the process being killed
        // between draft persistence and sync recording.
        let beforeRetry = try await artifactStore.listForCaptureRun(run.id)
        XCTAssertTrue(beforeRetry.isEmpty)

        // Retry: completeCapture must find the EXISTING draft rather than
        // normalizing a new one, and must now successfully record sync work.
        let coordinator = makeCoordinator()
        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let afterRetry = try await artifactStore.listForCaptureRun(run.id)
        XCTAssertEqual(afterRetry.count, 2, "expected both captured_room_data and roomdraft_json recorded after retry")

        let draftAfterRetry = try await draftRepo.find(forCapture: run.id)
        XCTAssertEqual(Set(draftAfterRetry.walls.map(\.id)), Set(firstPersistedDraft.walls.map(\.id)), "retry must not regenerate stable wall IDs")
    }

    /// Recording sync work is idempotent: calling completeCapture twice for
    /// the SAME run must never create duplicate pending-upload records.
    func test_completeCapture_calledTwice_doesNotDuplicateSyncRecords() async throws {
        let run = makeRun()
        try await captureRepo.create(run)
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())
        _ = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let pending = try await artifactStore.listForCaptureRun(run.id)
        XCTAssertEqual(pending.count, 2, "expected exactly 2 records (captured_room_data + roomdraft_json), never duplicated by a repeated call")
    }

    /// One scan's upload state must never attach to another scan (plan
    /// §RP3.5/§RP4B0's isolation requirement).
    func test_multipleScans_uploadStateStaysIsolated() async throws {
        let coordinator = makeCoordinator()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let firstRun = makeRun(captureNumber: 1)
        try await captureRepo.create(firstRun)
        _ = try await coordinator.completeCapture(run: firstRun, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let secondRun = makeRun(captureNumber: 2)
        try await captureRepo.create(secondRun)
        _ = try await coordinator.completeCapture(run: secondRun, rawResult: rawResult, adapter: FixtureCaptureAdapter(), capturedSource: makeCapturedSource())

        let firstRunPending = try await artifactStore.listForCaptureRun(firstRun.id)
        let secondRunPending = try await artifactStore.listForCaptureRun(secondRun.id)

        XCTAssertEqual(firstRunPending.count, 2)
        XCTAssertEqual(secondRunPending.count, 2)
        XCTAssertTrue(firstRunPending.allSatisfy { $0.captureRunID == firstRun.id })
        XCTAssertTrue(secondRunPending.allSatisfy { $0.captureRunID == secondRun.id })
    }
}
