import XCTest
@testable import RenovexCaptureCore

/// Plan §RP4A: proves `RoomDraft.resettingToBaseline()` and
/// `RoomDraftRepository.resetToBaseline(forCapture:)` mirror the Go
/// service's `ResetRoomDraftToBaseline` semantics exactly (design spec
/// §8.14/§8.25 "Reset to Scan") — same behavior on both platforms, not a
/// platform-specific reinterpretation.
final class RoomDraftResetTests: XCTestCase {
    private func fixtureProvenance(_ id: String) -> SourceProvenance {
        SourceProvenance(provider: .fixture, sourceElementIdentifier: id)
    }

    func test_resettingToBaseline_restoresWallsOpeningsObjects() throws {
        let baselineWall = RoomDraftWall(id: WallID("wall_original"), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 4, y: 0, z: 0), provenance: fixtureProvenance("w1"))
        var draft = RoomDraft(walls: [baselineWall], sourceProvider: .fixture, originalBaseline: RoomDraftBaseline(walls: [baselineWall]))

        // Simulate a contractor edit that moved the wall.
        draft.walls[0].start = RoomLocalPoint(x: 99, y: 0, z: 0)

        let reset = try draft.resettingToBaseline()

        XCTAssertEqual(reset.walls.count, 1)
        XCTAssertEqual(reset.walls[0].id, WallID("wall_original"), "stable ID must survive reset unchanged")
        XCTAssertEqual(reset.walls[0].start, RoomLocalPoint(x: 0, y: 0, z: 0), "wall position must revert to baseline")
    }

    func test_resettingToBaseline_clearsContractorCreatedElements() throws {
        let baseline = RoomDraftBaseline()
        var draft = RoomDraft(sourceProvider: .fixture, originalBaseline: baseline)
        draft.fixtures = [RoomDraftFixture(id: FixtureID("f1"), category: .boiler, transform: .init(position: .init(x: 0, y: 0, z: 0)), createdBy: .contractor)]
        draft.servicePoints = [RoomDraftServicePoint(id: ServicePointID("sp1"), kind: .drain, position: .init(x: 0, y: 0, z: 0), createdBy: .contractor)]
        draft.constraints = [RoomDraftConstraint(id: ConstraintID("c1"), kind: .column, transform: .init(position: .init(x: 0, y: 0, z: 0)), createdBy: .contractor)]

        let reset = try draft.resettingToBaseline()

        XCTAssertTrue(reset.fixtures.isEmpty, "contractor-created fixtures must be cleared entirely on reset")
        XCTAssertTrue(reset.servicePoints.isEmpty, "contractor-created service points must be cleared entirely on reset")
        XCTAssertTrue(reset.constraints.isEmpty, "contractor-created constraints must be cleared entirely on reset")
    }

    func test_resettingToBaseline_throwsWhenNoBaselineExists() {
        let draft = RoomDraft(sourceProvider: .fixture, originalBaseline: nil)
        XCTAssertThrowsError(try draft.resettingToBaseline()) { error in
            XCTAssertEqual(error as? RoomDraftResetError, .noBaseline)
        }
    }

    // MARK: - repository-level primitive

    private var tempDirectory: URL!

    override func setUp() {
        super.setUp()
        tempDirectory = FileManager.default.temporaryDirectory.appendingPathComponent("RenovexCaptureTests-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: tempDirectory)
        tempDirectory = nil
        super.tearDown()
    }

    func test_repository_resetToBaseline_persistsResetDraft() async throws {
        // swiftlint:disable:next force_try — matches this suite's other
        // setUp-adjacent repository construction; a fresh temp directory
        // failing is an environment failure, not a test case.
        let repo = try! FileRoomDraftRepository(directoryURL: tempDirectory)
        let baselineWall = RoomDraftWall(id: WallID("wall_1"), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 3, y: 0, z: 0), provenance: fixtureProvenance("w1"))
        let original = RoomDraft(walls: [baselineWall], sourceProvider: .fixture, originalBaseline: RoomDraftBaseline(walls: [baselineWall]))
        try await repo.create(original, forCapture: "capture_1")

        var edited = original
        edited.walls[0].start = RoomLocalPoint(x: 50, y: 0, z: 0)
        edited.fixtures = [RoomDraftFixture(id: FixtureID("f1"), category: .ac, transform: .init(position: .init(x: 0, y: 0, z: 0)), createdBy: .contractor)]
        try await repo.update(edited, forCapture: "capture_1")

        let reset = try await repo.resetToBaseline(forCapture: "capture_1")

        XCTAssertEqual(reset.walls[0].start, RoomLocalPoint(x: 0, y: 0, z: 0))
        XCTAssertTrue(reset.fixtures.isEmpty)

        // Reopen to prove the reset was actually persisted, not just
        // returned in memory.
        let reopened = try await repo.find(forCapture: "capture_1")
        XCTAssertEqual(reopened.walls[0].start, RoomLocalPoint(x: 0, y: 0, z: 0))
        XCTAssertTrue(reopened.fixtures.isEmpty)
    }

    func test_repository_resetToBaseline_throwsNoBaselineWhenDraftNeverEdited() async throws {
        // swiftlint:disable:next force_try
        let repo = try! FileRoomDraftRepository(directoryURL: tempDirectory)
        let draft = RoomDraft(sourceProvider: .fixture, originalBaseline: nil)
        try await repo.create(draft, forCapture: "capture_1")

        do {
            _ = try await repo.resetToBaseline(forCapture: "capture_1")
            XCTFail("expected noBaseline error")
        } catch {
            XCTAssertEqual(error, .noBaseline)
        }
    }
}
