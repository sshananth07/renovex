import XCTest
@testable import RenovexCaptureCore

/// Proves the provider -> adapter -> `RoomDraft` pipeline shape (design
/// spec §8.7) end-to-end using `FixtureCapturePayload`, since the real
/// `RoomPlanCaptureAdapter`'s equivalent test (against an actual
/// `CapturedRoom`) can only run once Xcode/RoomPlan compilation is
/// available (Tier B). This test stays in Core and has zero dependency on
/// RoomPlan.
final class FixtureCaptureAdapterTests: XCTestCase {
    func test_adapter_convertsFixtureWalls_toRoomDraftWalls() throws {
        let adapter = FixtureCaptureAdapter()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let draft = try adapter.makeRoomDraft(from: rawResult)

        XCTAssertEqual(draft.walls.count, 4)
        XCTAssertEqual(draft.sourceProvider, .fixture)
        XCTAssertEqual(draft.sourceCaptureIdentifier, "fixture-capture-rectangular-room")
    }

    func test_adapter_assignsStableRenovexIDs_distinctFromSourceIdentifiers() throws {
        let adapter = FixtureCaptureAdapter()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let draft = try adapter.makeRoomDraft(from: rawResult)

        let ids = draft.walls.map(\.id.rawValue)
        XCTAssertEqual(Set(ids).count, ids.count, "every wall must get a distinct Renovex-owned ID")

        for wall in draft.walls {
            // The Renovex ID must never equal the source provider's own
            // identifier — this is the concrete assertion behind design
            // spec §8.9's rule that provider identifiers are provenance
            // only, never domain identity.
            XCTAssertNotEqual(wall.id.rawValue, wall.provenance.sourceElementIdentifier)
        }
    }

    func test_adapter_preservesSourceProvenance_onEveryWall() throws {
        let adapter = FixtureCaptureAdapter()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let draft = try adapter.makeRoomDraft(from: rawResult)

        let southWall = try XCTUnwrap(draft.walls.first { $0.provenance.sourceElementIdentifier == "wall-south" })
        XCTAssertEqual(southWall.provenance.provider, .fixture)
        XCTAssertEqual(southWall.provenance.sourceCaptureIdentifier, "fixture-capture-rectangular-room")
        XCTAssertEqual(southWall.start, RoomLocalPoint(x: 0, y: 0, z: 0))
        XCTAssertEqual(southWall.end, RoomLocalPoint(x: 4, y: 0, z: 0))
        XCTAssertEqual(southWall.height, 2.4)
    }

    func test_adapter_producesCanonicalCoordinateFrame_metresRightHandedYUp() throws {
        // The fixture payload is authored directly in the canonical frame
        // (metres, right-handed, Y-up — design spec §8.8), so this test
        // proves the adapter passes those values through without silently
        // reinterpreting them, which is the RP1-scope half of the
        // coordinate contract (full RoomPlan-session-space conversion is
        // RP2 scope — see RoomPlanCaptureAdapter's own doc comment).
        let adapter = FixtureCaptureAdapter()
        let rawResult = SpatialCaptureRawResult(provider: .fixture, payload: FixtureCapturePayload.rectangularRoom)

        let draft = try adapter.makeRoomDraft(from: rawResult)

        // A 4m x 3m room: total wall length should sum to the perimeter,
        // 14m, confirming units are metres and not some other unit
        // RoomPlan/ARKit might otherwise report in.
        let totalLength = draft.walls.reduce(0.0) { sum, wall in
            let dx = wall.end.x - wall.start.x
            let dz = wall.end.z - wall.start.z
            return sum + (dx * dx + dz * dz).squareRoot()
        }
        XCTAssertEqual(totalLength, 14.0, accuracy: 0.0001)

        // Y is used for height/vertical-axis, never horizontal extent — the
        // Y-up convention.
        for wall in draft.walls {
            XCTAssertEqual(wall.start.y, 0)
            XCTAssertEqual(wall.end.y, 0)
        }
    }

    func test_adapter_rejectsPayloadFromWrongProvider() {
        let adapter = FixtureCaptureAdapter()
        let wrongResult = SpatialCaptureRawResult(provider: .roomplan, payload: FixtureCapturePayload.rectangularRoom)

        XCTAssertThrowsError(try adapter.makeRoomDraft(from: wrongResult)) { error in
            XCTAssertEqual(error as? SpatialCaptureAdapterError, .unexpectedPayload)
        }
    }

    func test_adapter_rejectsMistypedPayload() {
        let adapter = FixtureCaptureAdapter()
        let malformedResult = SpatialCaptureRawResult(provider: .fixture, payload: "not a FixtureCapturePayload")

        XCTAssertThrowsError(try adapter.makeRoomDraft(from: malformedResult)) { error in
            XCTAssertEqual(error as? SpatialCaptureAdapterError, .unexpectedPayload)
        }
    }
}
