import XCTest
@testable import RenovexCaptureCore

final class RoomDraftTests: XCTestCase {
    func test_emptyRoomDraft_hasNoWalls() {
        let draft = RoomDraft(sourceProvider: .fixture)
        XCTAssertTrue(draft.walls.isEmpty)
    }

    func test_roomDraft_isEquatable_forFixtureComparisonInTests() {
        let wall = RoomDraftWall(
            id: WallID("w1"),
            start: RoomLocalPoint(x: 0, y: 0, z: 0),
            end: RoomLocalPoint(x: 1, y: 0, z: 0),
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: "w1")
        )
        let draftA = RoomDraft(walls: [wall], sourceProvider: .fixture)
        let draftB = RoomDraft(walls: [wall], sourceProvider: .fixture)
        XCTAssertEqual(draftA, draftB)
    }

    func test_roomDraftWall_heightIsOptional() {
        let wall = RoomDraftWall(
            id: WallID("w1"),
            start: RoomLocalPoint(x: 0, y: 0, z: 0),
            end: RoomLocalPoint(x: 1, y: 0, z: 0),
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: "w1")
        )
        XCTAssertNil(wall.height, "height must be optional — not every capture provider reports it")
    }
}
