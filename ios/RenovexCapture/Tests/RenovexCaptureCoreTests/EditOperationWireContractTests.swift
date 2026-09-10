import XCTest
@testable import RenovexCaptureCore

/// Plan §RP4A: the wire discriminator (a "kind" string) plus each
/// operation's typed field set is the canonical cross-platform contract —
/// Go's `EditOperationKind` constants
/// (`backend/internal/spatial/editoperation.go`) and Swift's
/// `EditOperationKind` enum raw values must agree exactly, proven here by
/// asserting Swift's raw values match the literal strings Go declares.
/// Mirrors RP2's `CoordinateContractTests.swift` precedent: fixture-proven
/// agreement, not a shared schema file.
final class EditOperationWireContractTests: XCTestCase {
    func test_editOperationKind_rawValuesMatchGoWireDiscriminators() {
        // Each pair here must match the corresponding Go constant's string
        // literal in backend/internal/spatial/editoperation.go exactly.
        let expected: [(EditOperationKind, String)] = [
            (.moveCorner, "move_corner"),
            (.moveWall, "move_wall"),
            (.setWallThickness, "set_wall_thickness"),
            (.addOpening, "add_opening"),
            (.removeOpening, "remove_opening"),
            (.moveOpening, "move_opening"),
            (.resizeOpening, "resize_opening"),
            (.reclassifyOpening, "reclassify_opening"),
            (.setDoorLeafCount, "set_door_leaf_count"),
            (.setDoorHinge, "set_door_hinge"),
            (.setDoorSwing, "set_door_swing"),
            (.setDoorOpenDirection, "set_door_open_direction"),
            (.addObject, "add_object"),
            (.moveObject, "move_object"),
            (.rotateObject, "rotate_object"),
            (.resizeObject, "resize_object"),
            (.reclassifyObject, "reclassify_object"),
            (.removeObject, "remove_object"),
            (.addFixture, "add_fixture"),
            (.moveFixture, "move_fixture"),
            (.resizeFixture, "resize_fixture"),
            (.reclassifyFixture, "reclassify_fixture"),
            (.removeFixture, "remove_fixture"),
            (.addServicePoint, "add_service_point"),
            (.moveServicePoint, "move_service_point"),
            (.removeServicePoint, "remove_service_point"),
            (.addConstraint, "add_constraint"),
            (.moveConstraint, "move_constraint"),
            (.removeConstraint, "remove_constraint"),
            (.applyVerifiedMeasurement, "apply_verified_measurement"),
            (.assignVisualAsset, "assign_visual_asset"),
            (.clearVisualAsset, "clear_visual_asset"),
        ]
        for (kind, wireValue) in expected {
            XCTAssertEqual(kind.rawValue, wireValue, "Swift EditOperationKind.\(kind) must match Go's wire discriminator string")
        }
    }

    /// Every operation type's `kind` static property must equal the enum
    /// case its own name implies — catches a copy-paste mismatch between a
    /// struct and its declared kind (e.g. AddFixtureOperation accidentally
    /// declaring .addObject).
    func test_everyOperationType_declaresItsOwnMatchingKind() {
        XCTAssertEqual(MoveCornerOperation.kind, .moveCorner)
        XCTAssertEqual(MoveWallOperation.kind, .moveWall)
        XCTAssertEqual(SetWallThicknessOperation.kind, .setWallThickness)
        XCTAssertEqual(AddOpeningOperation.kind, .addOpening)
        XCTAssertEqual(RemoveOpeningOperation.kind, .removeOpening)
        XCTAssertEqual(MoveOpeningOperation.kind, .moveOpening)
        XCTAssertEqual(ResizeOpeningOperation.kind, .resizeOpening)
        XCTAssertEqual(ReclassifyOpeningOperation.kind, .reclassifyOpening)
        XCTAssertEqual(SetDoorLeafCountOperation.kind, .setDoorLeafCount)
        XCTAssertEqual(SetDoorHingeOperation.kind, .setDoorHinge)
        XCTAssertEqual(SetDoorSwingOperation.kind, .setDoorSwing)
        XCTAssertEqual(SetDoorOpenDirectionOperation.kind, .setDoorOpenDirection)
        XCTAssertEqual(AddObjectOperation.kind, .addObject)
        XCTAssertEqual(MoveObjectOperation.kind, .moveObject)
        XCTAssertEqual(RotateObjectOperation.kind, .rotateObject)
        XCTAssertEqual(ResizeObjectOperation.kind, .resizeObject)
        XCTAssertEqual(ReclassifyObjectOperation.kind, .reclassifyObject)
        XCTAssertEqual(RemoveObjectOperation.kind, .removeObject)
        XCTAssertEqual(AddFixtureOperation.kind, .addFixture)
        XCTAssertEqual(MoveFixtureOperation.kind, .moveFixture)
        XCTAssertEqual(ResizeFixtureOperation.kind, .resizeFixture)
        XCTAssertEqual(ReclassifyFixtureOperation.kind, .reclassifyFixture)
        XCTAssertEqual(RemoveFixtureOperation.kind, .removeFixture)
        XCTAssertEqual(AddServicePointOperation.kind, .addServicePoint)
        XCTAssertEqual(MoveServicePointOperation.kind, .moveServicePoint)
        XCTAssertEqual(RemoveServicePointOperation.kind, .removeServicePoint)
        XCTAssertEqual(AddConstraintOperation.kind, .addConstraint)
        XCTAssertEqual(MoveConstraintOperation.kind, .moveConstraint)
        XCTAssertEqual(RemoveConstraintOperation.kind, .removeConstraint)
        XCTAssertEqual(ApplyVerifiedMeasurementOperation.kind, .applyVerifiedMeasurement)
        XCTAssertEqual(AssignVisualAssetOperation.kind, .assignVisualAsset)
        XCTAssertEqual(ClearVisualAssetOperation.kind, .clearVisualAsset)
    }

    /// The OpeningKind wire contract (RP4A's canonical §6.1 alignment) must
    /// match Go's exactly: door|window|archway|other. This is the type
    /// reclassify_opening's payload carries across the wire.
    func test_openingKind_encodesToGoMatchingWireStrings() throws {
        let encoder = JSONEncoder()
        let cases: [(OpeningKind, String)] = [(.door, "\"door\""), (.window, "\"window\""), (.archway, "\"archway\""), (.other, "\"other\"")]
        for (kind, expectedJSON) in cases {
            let data = try encoder.encode(kind)
            XCTAssertEqual(String(data: data, encoding: .utf8), expectedJSON)
        }
    }

    func test_measurementTargetKind_encodesToGoMatchingWireStrings() throws {
        let encoder = JSONEncoder()
        XCTAssertEqual(String(data: try encoder.encode(MeasurementTargetKind.wall), encoding: .utf8), "\"wall\"")
        XCTAssertEqual(String(data: try encoder.encode(MeasurementTargetKind.opening), encoding: .utf8), "\"opening\"")
    }

    /// RP4D's VisualAssetTargetKind must match Go's exactly: fixture|object.
    func test_visualAssetTargetKind_encodesToGoMatchingWireStrings() throws {
        let encoder = JSONEncoder()
        XCTAssertEqual(String(data: try encoder.encode(VisualAssetTargetKind.fixture), encoding: .utf8), "\"fixture\"")
        XCTAssertEqual(String(data: try encoder.encode(VisualAssetTargetKind.object), encoding: .utf8), "\"object\"")
    }

    /// RP4D's VisualAssetReference must encode with the EXACT same JSON
    /// keys as Go's VisualAssetRef struct ({"assetId":...,"version":...}) —
    /// Swift's synthesized Codable uses the property name verbatim as the
    /// wire key, so this also proves the property names themselves
    /// (assetId, version) were spelled correctly, not just their values.
    func test_visualAssetReference_encodesWithGoMatchingKeys() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let ref = VisualAssetReference(assetId: "boiler-asset", version: 1)
        let data = try encoder.encode(ref)
        XCTAssertEqual(String(data: data, encoding: .utf8), "{\"assetId\":\"boiler-asset\",\"version\":1}")
    }

    /// RP4A found and fixed a genuine cross-platform wire-contract bug:
    /// `MeasurementStatus`'s pre-RP4A Codable conformance was synthesized
    /// from a plain enum, encoding as `{"estimated":{}}` rather than Go's
    /// plain `"estimated"` string. Proves the FIX: new encodes are plain
    /// strings matching Go exactly.
    func test_measurementStatus_encodesAsGoMatchingPlainString() throws {
        let encoder = JSONEncoder()
        XCTAssertEqual(String(data: try encoder.encode(MeasurementStatus.estimated), encoding: .utf8), "\"estimated\"")
        XCTAssertEqual(String(data: try encoder.encode(MeasurementStatus.unconfirmed), encoding: .utf8), "\"unconfirmed\"")
    }

    /// Proves the fix does NOT silently invalidate a `RoomDraft` a
    /// pre-RP4A build already persisted locally (e.g. via
    /// `FileRoomDraftRepository`) — the OLD synthesized shape must still
    /// decode correctly.
    func test_measurementStatus_decodesLegacySynthesizedShape() throws {
        let decoder = JSONDecoder()
        let legacyEstimated = "{\"estimated\":{}}".data(using: .utf8)!
        let legacyUnconfirmed = "{\"unconfirmed\":{}}".data(using: .utf8)!

        XCTAssertEqual(try decoder.decode(MeasurementStatus.self, from: legacyEstimated), .estimated)
        XCTAssertEqual(try decoder.decode(MeasurementStatus.self, from: legacyUnconfirmed), .unconfirmed)
    }

    func test_measurementStatus_decodesNewPlainStringShape() throws {
        let decoder = JSONDecoder()
        let newEstimated = "\"estimated\"".data(using: .utf8)!
        XCTAssertEqual(try decoder.decode(MeasurementStatus.self, from: newEstimated), .estimated)
    }
}
