import XCTest
@testable import RenovexCaptureCore

/// Plan §RP3 §16: "edit RoomDraft -> persist -> reopen -> edits remain";
/// "Continue Review does not create a duplicate" (one draft per capture,
/// enforced by keying storage on captureID).
final class FileRoomDraftRepositoryTests: XCTestCase {
    private var tempDirectory: URL!

    override func setUp() {
        super.setUp()
        tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent("RenovexCaptureTests-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: tempDirectory)
        tempDirectory = nil
        super.tearDown()
    }

    private func makeRepository() throws -> FileRoomDraftRepository {
        try FileRoomDraftRepository(directoryURL: tempDirectory)
    }

    private func makeDraft(wallID: String = "wall_1") -> RoomDraft {
        let wall = RoomDraftWall(
            id: WallID(wallID), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 3, y: 0, z: 0),
            provenance: SourceProvenance(provider: .roomplan, sourceElementIdentifier: "src-\(wallID)")
        )
        return RoomDraft(walls: [wall], sourceProvider: .roomplan)
    }

    func test_create_thenFind_returnsTheDraft() async throws {
        let repo = try makeRepository()
        let draft = makeDraft()

        try await repo.create(draft, forCapture: "capture_1")
        let found = try await repo.find(forCapture: "capture_1")

        XCTAssertEqual(found, draft)
    }

    func test_find_unknownCapture_throwsNotFound() async throws {
        let repo = try makeRepository()

        do {
            _ = try await repo.find(forCapture: "nonexistent")
            XCTFail("expected notFound")
        } catch {
            XCTAssertEqual(error, .notFound)
        }
    }

    func test_create_secondTimeForSameCapture_throws() async throws {
        let repo = try makeRepository()
        try await repo.create(makeDraft(), forCapture: "capture_1")

        do {
            try await repo.create(makeDraft(wallID: "wall_2"), forCapture: "capture_1")
            XCTFail("expected an error — one capture must have at most one RoomDraft")
        } catch {
            // expected — use update for edits
        }
    }

    /// "edit RoomDraft -> persist -> reopen -> edits remain," proven across
    /// a fresh repository instance the same way the capture-run test proves
    /// survival across an app relaunch.
    func test_editThenReopenAcrossRepositoryRecreation_preservesEdits() async throws {
        let original = makeDraft()
        do {
            let firstInstance = try makeRepository()
            try await firstInstance.create(original, forCapture: "capture_1")

            var edited = original
            edited.walls[0].height = 2.7
            try await firstInstance.update(edited, forCapture: "capture_1")
        }

        let secondInstance = try makeRepository()
        let reopened = try await secondInstance.find(forCapture: "capture_1")

        XCTAssertEqual(reopened.walls.first?.height, 2.7)
        XCTAssertEqual(reopened.walls.first?.id, WallID("wall_1"), "editing must not regenerate the stable wall ID")
    }

    /// "reopen returns the same run/draft" + "Continue Review does not
    /// create a duplicate" — repeated find() calls for the same capture
    /// always resolve to the same underlying file, never a new one.
    func test_repeatedFind_returnsIdenticalDraft_neverCreatesADuplicate() async throws {
        let repo = try makeRepository()
        let draft = makeDraft()
        try await repo.create(draft, forCapture: "capture_1")

        let firstOpen = try await repo.find(forCapture: "capture_1")
        let secondOpen = try await repo.find(forCapture: "capture_1")

        XCTAssertEqual(firstOpen, secondOpen)
        XCTAssertEqual(firstOpen.walls.first?.id, secondOpen.walls.first?.id)
    }

    /// "multiple scans all remain" for the draft layer: two different
    /// captures' drafts must not collide or overwrite each other.
    func test_draftsForDifferentCaptures_remainDistinct() async throws {
        let repo = try makeRepository()
        let draftA = makeDraft(wallID: "wall_a")
        let draftB = makeDraft(wallID: "wall_b")

        try await repo.create(draftA, forCapture: "capture_a")
        try await repo.create(draftB, forCapture: "capture_b")

        let foundA = try await repo.find(forCapture: "capture_a")
        let foundB = try await repo.find(forCapture: "capture_b")

        XCTAssertEqual(foundA.walls.first?.id, WallID("wall_a"))
        XCTAssertEqual(foundB.walls.first?.id, WallID("wall_b"))
    }
}
