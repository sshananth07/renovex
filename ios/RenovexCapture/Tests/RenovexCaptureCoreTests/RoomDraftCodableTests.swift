import XCTest
@testable import RenovexCaptureCore

/// Plan §RP3 §16: "RoomDraft encode -> decode preserves semantic equality"
/// and "stable IDs survive persistence" — proven at the Codable level,
/// independent of which concrete repository later wraps it.
final class RoomDraftCodableTests: XCTestCase {
    private func encoderDecoder() -> (JSONEncoder, JSONDecoder) {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return (encoder, decoder)
    }

    func test_roomDraft_encodeDecode_preservesSemanticEquality() throws {
        let (encoder, decoder) = encoderDecoder()
        let wall = RoomDraftWall(
            id: WallID("wall_stable_1"),
            start: RoomLocalPoint(x: 0, y: 0, z: 0),
            end: RoomLocalPoint(x: 4, y: 0, z: 0),
            height: 2.4,
            thickness: 0.16,
            thicknessStatus: .estimated,
            provenance: SourceProvenance(provider: .roomplan, sourceElementIdentifier: "roomplan-uuid-1", sourceCaptureIdentifier: "session-1")
        )
        let opening = RoomDraftOpening(
            id: OpeningID("opening_stable_1"),
            parentWallID: WallID("wall_stable_1"),
            kind: .door,
            transform: RoomLocalTransform(position: RoomLocalPoint(x: 1, y: 0, z: 0), rotation: .identity),
            width: 0.9,
            height: 2.1,
            provenance: SourceProvenance(provider: .roomplan, sourceElementIdentifier: "roomplan-door-1")
        )
        let object = RoomDraftObject(
            id: ObjectID("object_stable_1"),
            category: "sofa",
            transform: RoomLocalTransform(position: RoomLocalPoint(x: 2, y: 0, z: 1)),
            dimensions: RoomLocalPoint(x: 2, y: 1, z: 0.9),
            provenance: SourceProvenance(provider: .roomplan, sourceElementIdentifier: "roomplan-object-1")
        )
        let original = RoomDraft(
            walls: [wall], openings: [opening], objects: [object],
            sourceCaptureIdentifier: "session-1", sourceProvider: .roomplan
        )

        let data = try encoder.encode(original)
        let decoded = try decoder.decode(RoomDraft.self, from: data)

        XCTAssertEqual(decoded, original)
    }

    func test_roomDraft_encodeDecode_preservesStableWallOpeningObjectIDs() throws {
        let (encoder, decoder) = encoderDecoder()
        let wall = RoomDraftWall(
            id: WallID("wall_stable_1"), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 1, y: 0, z: 0),
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: "w1")
        )
        let original = RoomDraft(walls: [wall], sourceProvider: .fixture)

        let decoded = try decoder.decode(RoomDraft.self, from: encoder.encode(original))

        XCTAssertEqual(decoded.walls.first?.id, WallID("wall_stable_1"), "reopening a persisted draft must retain the same WallID identity")
    }

    func test_roomDraft_encodeDecode_preservesParentWallRelationship() throws {
        let (encoder, decoder) = encoderDecoder()
        let opening = RoomDraftOpening(
            id: OpeningID("o1"), parentWallID: WallID("wall_stable_1"), kind: .window,
            transform: RoomLocalTransform(position: RoomLocalPoint(x: 0, y: 0, z: 0)),
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: "window-1")
        )
        let original = RoomDraft(openings: [opening], sourceProvider: .fixture)

        let decoded = try decoder.decode(RoomDraft.self, from: encoder.encode(original))

        XCTAssertEqual(decoded.openings.first?.parentWallID, WallID("wall_stable_1"))
    }

    func test_roomDraft_encodeDecode_preservesTransformAndQuaternion() throws {
        let (encoder, decoder) = encoderDecoder()
        let rotation = RoomLocalQuaternion(x: 0.1, y: 0.2, z: 0.3, w: 0.9)
        let object = RoomDraftObject(
            id: ObjectID("o1"), category: "table",
            transform: RoomLocalTransform(position: RoomLocalPoint(x: 1.5, y: 0, z: -2.25), rotation: rotation),
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: "table-1")
        )
        let original = RoomDraft(objects: [object], sourceProvider: .fixture)

        let decoded = try decoder.decode(RoomDraft.self, from: encoder.encode(original))

        XCTAssertEqual(decoded.objects.first?.transform.rotation, rotation)
        XCTAssertEqual(decoded.objects.first?.transform.position, RoomLocalPoint(x: 1.5, y: 0, z: -2.25))
    }

    func test_roomDraft_encodeDecode_preservesProvenance() throws {
        let (encoder, decoder) = encoderDecoder()
        let wall = RoomDraftWall(
            id: WallID("w1"), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 1, y: 0, z: 0),
            provenance: SourceProvenance(provider: .roomplan, sourceElementIdentifier: "roomplan-abc", sourceCaptureIdentifier: "session-xyz")
        )
        let original = RoomDraft(walls: [wall], sourceProvider: .roomplan)

        let decoded = try decoder.decode(RoomDraft.self, from: encoder.encode(original))

        XCTAssertEqual(decoded.walls.first?.provenance, wall.provenance)
    }

    // MARK: - visual asset binding (RP4D)

    /// The first explicit "decodes old JSON missing a NEW optional field"
    /// regression test in this codebase for a struct (as opposed to
    /// MeasurementStatus's enum-shape migration, a different kind of
    /// problem) — proves Swift's synthesized Codable already handles this
    /// correctly via decodeIfPresent, with zero custom decoder needed.
    func test_roomDraftFixture_decodesOldJSONWithoutVisualAsset() throws {
        let oldJSON = """
        {"id":"fixture_1","category":"boiler","transform":{"position":{"x":0,"y":0,"z":0},"rotation":{"x":0,"y":0,"z":0,"w":1}},"createdBy":"contractor"}
        """.data(using: .utf8)!
        let fixture = try JSONDecoder().decode(RoomDraftFixture.self, from: oldJSON)
        XCTAssertNil(fixture.visualAsset)
    }

    func test_roomDraftObject_decodesOldJSONWithoutVisualAsset() throws {
        let oldJSON = """
        {"id":"object_1","category":"sofa","transform":{"position":{"x":0,"y":0,"z":0},"rotation":{"x":0,"y":0,"z":0,"w":1}},"provenance":{"provider":"roomplan","sourceElementIdentifier":"o1"}}
        """.data(using: .utf8)!
        let object = try JSONDecoder().decode(RoomDraftObject.self, from: oldJSON)
        XCTAssertNil(object.visualAsset)
    }

    func test_roomDraftFixture_encodeDecode_roundTripsVisualAsset() throws {
        let (encoder, decoder) = encoderDecoder()
        let fixture = RoomDraftFixture(
            id: FixtureID("fixture_1"), category: .boiler,
            transform: RoomLocalTransform(position: .init(x: 0, y: 0, z: 0)),
            createdBy: .contractor,
            visualAsset: VisualAssetReference(assetId: "boiler-asset", version: 2)
        )
        let decoded = try decoder.decode(RoomDraftFixture.self, from: encoder.encode(fixture))
        XCTAssertEqual(decoded.visualAsset, fixture.visualAsset)
    }
}
