import XCTest
@testable import RenovexCaptureCore

final class CoincidentWallSanityCheckTests: XCTestCase {
    private func makeWall(id: String, start: RoomLocalPoint, end: RoomLocalPoint) -> RoomDraftWall {
        RoomDraftWall(
            id: WallID(id),
            start: start,
            end: end,
            provenance: SourceProvenance(provider: .roomplan, sourceElementIdentifier: id)
        )
    }

    func test_singleWall_producesNoPairs() {
        let walls = [makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0))]
        XCTAssertTrue(CoincidentWallSanityCheck.findCoincidentPairs(in: walls).isEmpty)
    }

    func test_distinctParallelWalls_areNotFlagged() {
        // Two genuinely distinct parallel walls 1m apart must NOT be
        // flagged — this is the explicit "not distance-only merging" rule
        // from design spec §8.10.
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 0, y: 0, z: 1), end: .init(x: 4, y: 0, z: 1)),
        ]
        XCTAssertTrue(CoincidentWallSanityCheck.findCoincidentPairs(in: walls).isEmpty)
    }

    func test_exactlyCoincidentWalls_sameOrder_areFlagged() {
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
        ]
        let pairs = CoincidentWallSanityCheck.findCoincidentPairs(in: walls)
        XCTAssertEqual(pairs.count, 1)
        XCTAssertEqual(pairs.first?.0, 0)
        XCTAssertEqual(pairs.first?.1, 1)
    }

    func test_coincidentWalls_reversedEndpointOrder_areStillFlagged() {
        // Same physical wall reported with start/end swapped — a real
        // possibility if two provider callbacks resolve endpoint order
        // differently.
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 4, y: 0, z: 0), end: .init(x: 0, y: 0, z: 0)),
        ]
        let pairs = CoincidentWallSanityCheck.findCoincidentPairs(in: walls)
        XCTAssertEqual(pairs.count, 1)
    }

    func test_withinEpsilon_isFlagged() {
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 0.01, y: 0, z: 0), end: .init(x: 4.01, y: 0, z: 0)),
        ]
        let pairs = CoincidentWallSanityCheck.findCoincidentPairs(in: walls, epsilonMeters: 0.02)
        XCTAssertEqual(pairs.count, 1)
    }

    func test_justOutsideEpsilon_isNotFlagged() {
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 0.03, y: 0, z: 0), end: .init(x: 4.03, y: 0, z: 0)),
        ]
        let pairs = CoincidentWallSanityCheck.findCoincidentPairs(in: walls, epsilonMeters: 0.02)
        XCTAssertTrue(pairs.isEmpty)
    }

    func test_exactlyAtEpsilonBoundary_isFlagged() {
        // Boundary test per the project's own tolerance-testing convention
        // (below/at/above the configured value) — "at" must be inclusive
        // (<=), matching the implementation's use of <=.
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 0.02, y: 0, z: 0), end: .init(x: 4.02, y: 0, z: 0)),
        ]
        let pairs = CoincidentWallSanityCheck.findCoincidentPairs(in: walls, epsilonMeters: 0.02)
        XCTAssertEqual(pairs.count, 1)
    }

    func test_threeWalls_onlyCoincidentPairFlagged() {
        let walls = [
            makeWall(id: "w1", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w2", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0)),
            makeWall(id: "w3", start: .init(x: 0, y: 0, z: 5), end: .init(x: 4, y: 0, z: 5)),
        ]
        let pairs = CoincidentWallSanityCheck.findCoincidentPairs(in: walls)
        XCTAssertEqual(pairs.count, 1)
        XCTAssertEqual(pairs.first?.0, 0)
        XCTAssertEqual(pairs.first?.1, 1)
    }

    func test_emptyInput_producesNoPairs() {
        XCTAssertTrue(CoincidentWallSanityCheck.findCoincidentPairs(in: []).isEmpty)
    }
}
