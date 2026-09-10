import XCTest
@testable import RenovexCaptureCore

/// Plan §RP4A: proves the shared geometry-edit operation vocabulary
/// (design spec §8.13) against the Swift-side `RoomDraft` mirror. Mirrors
/// `backend/internal/spatial/editoperation_test.go`'s coverage exactly, one
/// test suite per operation category, so both platforms are proven correct
/// against the same behavioral contract.
final class EditOperationTests: XCTestCase {
    // MARK: - fixtures

    private func draftWithOneWall() -> RoomDraft {
        RoomDraft(
            walls: [RoomDraftWall(id: WallID("wall_1"), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 4, y: 0, z: 0), provenance: fixtureProvenance())],
            sourceProvider: .fixture
        )
    }

    private func draftWithTwoAdjacentWalls() -> RoomDraft {
        RoomDraft(
            walls: [
                RoomDraftWall(id: WallID("wall_1"), start: RoomLocalPoint(x: 0, y: 0, z: 0), end: RoomLocalPoint(x: 4, y: 0, z: 0), provenance: fixtureProvenance()),
                RoomDraftWall(id: WallID("wall_2"), start: RoomLocalPoint(x: 4, y: 0, z: 0), end: RoomLocalPoint(x: 4, y: 0, z: 3), provenance: fixtureProvenance()),
            ],
            sourceProvider: .fixture
        )
    }

    private func draftWithOneOpening() -> RoomDraft {
        var draft = draftWithOneWall()
        draft.openings = [RoomDraftOpening(id: OpeningID("opening_1"), parentWallID: WallID("wall_1"), kind: .window, profile: .rectangle, transform: .init(position: .init(x: 0, y: 0, z: 0)), provenance: fixtureProvenance())]
        return draft
    }

    private func draftWithOneDoor() -> RoomDraft {
        var draft = draftWithOneWall()
        draft.openings = [RoomDraftOpening(id: OpeningID("door_1"), parentWallID: WallID("wall_1"), kind: .door, profile: .rectangle, transform: .init(position: .init(x: 0, y: 0, z: 0)), provenance: fixtureProvenance())]
        return draft
    }

    private func draftWithOneObject() -> RoomDraft {
        RoomDraft(objects: [RoomDraftObject(id: ObjectID("object_1"), category: "sofa", transform: .init(position: .init(x: 0, y: 0, z: 0)), provenance: fixtureProvenance())], sourceProvider: .fixture)
    }

    private func draftWithOneFixture() -> RoomDraft {
        RoomDraft(fixtures: [RoomDraftFixture(id: FixtureID("fixture_1"), category: .boiler, transform: .init(position: .init(x: 0, y: 0, z: 0)), createdBy: .contractor)], sourceProvider: .fixture)
    }

    private func draftWithOneServicePoint() -> RoomDraft {
        RoomDraft(servicePoints: [RoomDraftServicePoint(id: ServicePointID("sp_1"), kind: .plumbing, position: .init(x: 0, y: 0, z: 0), createdBy: .contractor)], sourceProvider: .fixture)
    }

    private func draftWithOneConstraint() -> RoomDraft {
        RoomDraft(constraints: [RoomDraftConstraint(id: ConstraintID("constraint_1"), kind: .column, transform: .init(position: .init(x: 0, y: 0, z: 0)), createdBy: .contractor)], sourceProvider: .fixture)
    }

    private func fixtureProvenance() -> SourceProvenance {
        SourceProvenance(provider: .fixture, sourceElementIdentifier: "src")
    }

    // MARK: - move_corner

    func test_moveCorner_movesExactlySuppliedEndpoints() throws {
        let draft = draftWithTwoAdjacentWalls()
        let op = MoveCornerOperation(
            endpoints: [WallEndpointRef(wallID: WallID("wall_1"), endpoint: .end), WallEndpointRef(wallID: WallID("wall_2"), endpoint: .start)],
            newPosition: RoomLocalPoint(x: 4.1, y: 0, z: 0.1)
        )
        let updated = try op.apply(to: draft)
        XCTAssertEqual(updated.walls[0].end, op.newPosition)
        XCTAssertEqual(updated.walls[1].start, op.newPosition)
    }

    func test_moveCorner_rejectsEmptyEndpointList() {
        let op = MoveCornerOperation(endpoints: [], newPosition: .init(x: 0, y: 0, z: 0))
        XCTAssertThrowsError(try op.validate()) { error in
            XCTAssertEqual(error as? EditOperationError, .invalidOperation)
        }
    }

    func test_moveCorner_rejectsUnknownWall() {
        let op = MoveCornerOperation(endpoints: [WallEndpointRef(wallID: WallID("nonexistent"), endpoint: .start)], newPosition: .init(x: 0, y: 0, z: 0))
        XCTAssertThrowsError(try op.apply(to: draftWithOneWall())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    func test_moveCorner_rejectsNonCoincidentEndpoints() {
        let draft = draftWithTwoAdjacentWalls()
        let op = MoveCornerOperation(
            endpoints: [WallEndpointRef(wallID: WallID("wall_1"), endpoint: .end), WallEndpointRef(wallID: WallID("wall_2"), endpoint: .end)],
            newPosition: RoomLocalPoint(x: 5, y: 0, z: 0)
        )
        XCTAssertThrowsError(try op.apply(to: draft)) { error in
            XCTAssertEqual(error as? EditOperationError, .invalidOperation)
        }
    }

    func test_moveCorner_acceptsEndpointsWithinTolerance() throws {
        var draft = draftWithTwoAdjacentWalls()
        draft.walls[1].start = RoomLocalPoint(x: 4.01, y: 0, z: 0)
        let op = MoveCornerOperation(
            endpoints: [WallEndpointRef(wallID: WallID("wall_1"), endpoint: .end), WallEndpointRef(wallID: WallID("wall_2"), endpoint: .start)],
            newPosition: RoomLocalPoint(x: 4.5, y: 0, z: 0)
        )
        XCTAssertNoThrow(try op.apply(to: draft))
    }

    func test_moveCorner_mutationScopeIsExactlySuppliedSet() throws {
        var draft = draftWithTwoAdjacentWalls()
        draft.walls.append(RoomDraftWall(id: WallID("wall_3_unlisted"), start: RoomLocalPoint(x: 4, y: 0, z: 0), end: RoomLocalPoint(x: 8, y: 0, z: 0), provenance: fixtureProvenance()))
        let op = MoveCornerOperation(endpoints: [WallEndpointRef(wallID: WallID("wall_1"), endpoint: .end)], newPosition: RoomLocalPoint(x: 4.2, y: 0, z: 0.1))
        let updated = try op.apply(to: draft)
        XCTAssertEqual(updated.walls[2].start, RoomLocalPoint(x: 4, y: 0, z: 0), "unlisted wall must remain untouched")
    }

    // MARK: - move_wall / set_wall_thickness

    func test_moveWall_translatesBothEndpoints() throws {
        let op = MoveWallOperation(wallID: WallID("wall_1"), delta: RoomLocalPoint(x: 1, y: 0, z: 1))
        let updated = try op.apply(to: draftWithOneWall())
        XCTAssertEqual(updated.walls[0].start, RoomLocalPoint(x: 1, y: 0, z: 1))
        XCTAssertEqual(updated.walls[0].end, RoomLocalPoint(x: 5, y: 0, z: 1))
    }

    func test_setWallThickness_setsThicknessAndStatus() throws {
        let op = SetWallThicknessOperation(wallID: WallID("wall_1"), thickness: 0.18, status: .estimated)
        let updated = try op.apply(to: draftWithOneWall())
        XCTAssertEqual(updated.walls[0].thickness, 0.18)
        XCTAssertEqual(updated.walls[0].thicknessStatus, .estimated)
    }

    func test_setWallThickness_rejectsNonPositiveThickness() {
        let op = SetWallThicknessOperation(wallID: WallID("wall_1"), thickness: 0, status: .estimated)
        XCTAssertThrowsError(try op.validate())
    }

    // MARK: - opening operations

    func test_addOpening_addsToExistingWall() throws {
        let op = AddOpeningOperation(id: OpeningID("opening_1"), parentWallID: WallID("wall_1"), openingKind: .window, profile: .rectangle, transform: .init(position: .init(x: 0, y: 0, z: 0)), width: 1, height: 1.2, offsetAlongWall: 1)
        let updated = try op.apply(to: draftWithOneWall())
        XCTAssertEqual(updated.openings.count, 1)
        XCTAssertEqual(updated.openings[0].id, OpeningID("opening_1"))
    }

    func test_addOpening_rejectsUnknownParentWall() {
        let op = AddOpeningOperation(id: OpeningID("o1"), parentWallID: WallID("nonexistent"), openingKind: .window, profile: .rectangle, transform: .init(position: .init(x: 0, y: 0, z: 0)), width: 1, height: 1, offsetAlongWall: 0)
        XCTAssertThrowsError(try op.apply(to: draftWithOneWall())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    func test_addOpening_rejectsDuplicateID() {
        let op = AddOpeningOperation(id: OpeningID("opening_1"), parentWallID: WallID("wall_1"), openingKind: .window, profile: .rectangle, transform: .init(position: .init(x: 0, y: 0, z: 0)), width: 1, height: 1, offsetAlongWall: 0)
        XCTAssertThrowsError(try op.apply(to: draftWithOneOpening())) { error in
            XCTAssertEqual(error as? EditOperationError, .invalidOperation)
        }
    }

    func test_removeOpening_removesByID() throws {
        let updated = try RemoveOpeningOperation(openingID: OpeningID("opening_1")).apply(to: draftWithOneOpening())
        XCTAssertTrue(updated.openings.isEmpty)
    }

    func test_removeOpening_rejectsUnknownOpening() {
        XCTAssertThrowsError(try RemoveOpeningOperation(openingID: OpeningID("nonexistent")).apply(to: draftWithOneOpening()))
    }

    func test_resizeOpening_setsWidthHeight() throws {
        let updated = try ResizeOpeningOperation(openingID: OpeningID("opening_1"), width: 1.5, height: 2.0).apply(to: draftWithOneOpening())
        XCTAssertEqual(updated.openings[0].width, 1.5)
        XCTAssertEqual(updated.openings[0].height, 2.0)
    }

    func test_reclassifyOpening_changesKindAndProfile() throws {
        let updated = try ReclassifyOpeningOperation(openingID: OpeningID("opening_1"), openingKind: .archway, profile: .arch).apply(to: draftWithOneOpening())
        XCTAssertEqual(updated.openings[0].kind, .archway)
        XCTAssertEqual(updated.openings[0].profile, .arch)
    }

    func test_reclassifyOpening_awayFromDoorClearsDoorMetadata() throws {
        var draft = draftWithOneDoor()
        draft.openings[0].door = DoorMetadata(leafCount: 2)
        let updated = try ReclassifyOpeningOperation(openingID: OpeningID("door_1"), openingKind: .window, profile: .rectangle).apply(to: draft)
        XCTAssertNil(updated.openings[0].door)
    }

    // MARK: - door-specific operations

    func test_setDoorLeafCount_setsOnDoorOpening() throws {
        let updated = try SetDoorLeafCountOperation(openingID: OpeningID("door_1"), leafCount: 2).apply(to: draftWithOneDoor())
        XCTAssertEqual(updated.openings[0].door?.leafCount, 2)
    }

    func test_setDoorLeafCount_rejectsNonDoorTarget() {
        XCTAssertThrowsError(try SetDoorLeafCountOperation(openingID: OpeningID("opening_1"), leafCount: 1).apply(to: draftWithOneOpening())) { error in
            XCTAssertEqual(error as? EditOperationError, .invalidOperation)
        }
    }

    func test_setDoorHinge_setsOnDoorOpening() throws {
        let updated = try SetDoorHingeOperation(openingID: OpeningID("door_1"), hinge: .left).apply(to: draftWithOneDoor())
        XCTAssertEqual(updated.openings[0].door?.hinge, .left)
    }

    func test_setDoorSwing_setsOnDoorOpening() throws {
        let updated = try SetDoorSwingOperation(openingID: OpeningID("door_1"), swing: .inward).apply(to: draftWithOneDoor())
        XCTAssertEqual(updated.openings[0].door?.swing, .inward)
    }

    func test_setDoorOpenDirection_setsOnDoorOpening() throws {
        let updated = try SetDoorOpenDirectionOperation(openingID: OpeningID("door_1"), openDirection: "north").apply(to: draftWithOneDoor())
        XCTAssertEqual(updated.openings[0].door?.openDirection, "north")
    }

    // MARK: - object operations

    func test_addObject_addsObject() throws {
        let updated = try AddObjectOperation(id: ObjectID("object_1"), category: "chair", transform: .init(position: .init(x: 0, y: 0, z: 0))).apply(to: RoomDraft(sourceProvider: .fixture))
        XCTAssertEqual(updated.objects.count, 1)
    }

    func test_addObject_rejectsDuplicateID() {
        let op = AddObjectOperation(id: ObjectID("object_1"), category: "table", transform: .init(position: .init(x: 0, y: 0, z: 0)))
        XCTAssertThrowsError(try op.apply(to: draftWithOneObject()))
    }

    func test_moveObject_updatesPosition() throws {
        let updated = try MoveObjectOperation(objectID: ObjectID("object_1"), position: RoomLocalPoint(x: 3, y: 0, z: 0)).apply(to: draftWithOneObject())
        XCTAssertEqual(updated.objects[0].transform.position.x, 3)
    }

    func test_removeObject_removesByID() throws {
        let updated = try RemoveObjectOperation(objectID: ObjectID("object_1")).apply(to: draftWithOneObject())
        XCTAssertTrue(updated.objects.isEmpty)
    }

    // MARK: - fixture operations

    func test_addFixture_createsContractorOriginFixture() throws {
        let updated = try AddFixtureOperation(id: FixtureID("fixture_1"), category: .boiler, transform: .init(position: .init(x: 0, y: 0, z: 0))).apply(to: RoomDraft(sourceProvider: .fixture))
        XCTAssertEqual(updated.fixtures.count, 1)
        XCTAssertEqual(updated.fixtures[0].createdBy, .contractor)
        XCTAssertNil(updated.fixtures[0].provenance, "contractor-created fixture must never have a synthesized provenance")
    }

    func test_addFixture_rejectsUnknownParentWall() {
        let op = AddFixtureOperation(id: FixtureID("f1"), category: .wallFixture, transform: .init(position: .init(x: 0, y: 0, z: 0)), parentWallID: WallID("nonexistent"))
        XCTAssertThrowsError(try op.apply(to: RoomDraft(sourceProvider: .fixture))) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    func test_moveFixture_updatesTransform() throws {
        let newTransform = RoomLocalTransform(position: RoomLocalPoint(x: 5, y: 0, z: 0))
        let updated = try MoveFixtureOperation(fixtureID: FixtureID("fixture_1"), transform: newTransform).apply(to: draftWithOneFixture())
        XCTAssertEqual(updated.fixtures[0].transform, newTransform)
    }

    func test_reclassifyFixture_changesCategory() throws {
        let updated = try ReclassifyFixtureOperation(fixtureID: FixtureID("fixture_1"), category: .ac).apply(to: draftWithOneFixture())
        XCTAssertEqual(updated.fixtures[0].category, .ac)
    }

    func test_removeFixture_removesByID() throws {
        let updated = try RemoveFixtureOperation(fixtureID: FixtureID("fixture_1")).apply(to: draftWithOneFixture())
        XCTAssertTrue(updated.fixtures.isEmpty)
    }

    func test_removeFixture_rejectsMissingTarget() {
        let op = RemoveFixtureOperation(fixtureID: FixtureID("nonexistent"))
        XCTAssertThrowsError(try op.apply(to: draftWithOneFixture())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    // MARK: - service point operations

    func test_addServicePoint_createsContractorOriginPoint() throws {
        let updated = try AddServicePointOperation(id: ServicePointID("sp_1"), servicePointKind: .electrical, position: .init(x: 0, y: 0, z: 0)).apply(to: RoomDraft(sourceProvider: .fixture))
        XCTAssertEqual(updated.servicePoints.count, 1)
        XCTAssertNil(updated.servicePoints[0].provenance)
    }

    func test_moveServicePoint_updatesPosition() throws {
        let newPos = RoomLocalPoint(x: 2, y: 0, z: 1)
        let updated = try MoveServicePointOperation(servicePointID: ServicePointID("sp_1"), position: newPos).apply(to: draftWithOneServicePoint())
        XCTAssertEqual(updated.servicePoints[0].position, newPos)
    }

    func test_removeServicePoint_removesByID() throws {
        let updated = try RemoveServicePointOperation(servicePointID: ServicePointID("sp_1")).apply(to: draftWithOneServicePoint())
        XCTAssertTrue(updated.servicePoints.isEmpty)
    }

    func test_removeServicePoint_rejectsMissingTarget() {
        let op = RemoveServicePointOperation(servicePointID: ServicePointID("nonexistent"))
        XCTAssertThrowsError(try op.apply(to: draftWithOneServicePoint())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    // MARK: - constraint operations

    func test_addConstraint_createsContractorOriginConstraint() throws {
        let updated = try AddConstraintOperation(id: ConstraintID("constraint_1"), constraintKind: .column, transform: .init(position: .init(x: 0, y: 0, z: 0))).apply(to: RoomDraft(sourceProvider: .fixture))
        XCTAssertEqual(updated.constraints.count, 1)
        XCTAssertNil(updated.constraints[0].provenance)
    }

    func test_moveConstraint_updatesTransform() throws {
        let newTransform = RoomLocalTransform(position: RoomLocalPoint(x: 1, y: 0, z: 1))
        let updated = try MoveConstraintOperation(constraintID: ConstraintID("constraint_1"), transform: newTransform).apply(to: draftWithOneConstraint())
        XCTAssertEqual(updated.constraints[0].transform, newTransform)
    }

    func test_removeConstraint_removesByID() throws {
        let updated = try RemoveConstraintOperation(constraintID: ConstraintID("constraint_1")).apply(to: draftWithOneConstraint())
        XCTAssertTrue(updated.constraints.isEmpty)
    }

    func test_removeConstraint_rejectsMissingTarget() {
        let op = RemoveConstraintOperation(constraintID: ConstraintID("nonexistent"))
        XCTAssertThrowsError(try op.apply(to: draftWithOneConstraint())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    // MARK: - apply_verified_measurement

    func test_applyVerifiedMeasurement_updatesWallThickness() throws {
        let op = ApplyVerifiedMeasurementOperation(targetKind: .wall, targetID: "wall_1", field: .thickness, value: 0.18, status: .estimated)
        let updated = try op.apply(to: draftWithOneWall())
        XCTAssertEqual(updated.walls[0].thickness, 0.18)
    }

    func test_applyVerifiedMeasurement_updatesOpeningWidth() throws {
        let op = ApplyVerifiedMeasurementOperation(targetKind: .opening, targetID: "opening_1", field: .width, value: 0.95, status: .estimated)
        let updated = try op.apply(to: draftWithOneOpening())
        XCTAssertEqual(updated.openings[0].width, 0.95)
    }

    func test_applyVerifiedMeasurement_rejectsWallWidthField() {
        let op = ApplyVerifiedMeasurementOperation(targetKind: .wall, targetID: "wall_1", field: .width, value: 1, status: .estimated)
        XCTAssertThrowsError(try op.validate()) { error in
            XCTAssertEqual(error as? EditOperationError, .invalidOperation)
        }
    }

    func test_applyVerifiedMeasurement_rejectsOpeningThicknessField() {
        let op = ApplyVerifiedMeasurementOperation(targetKind: .opening, targetID: "opening_1", field: .thickness, value: 1, status: .estimated)
        XCTAssertThrowsError(try op.validate())
    }

    func test_applyVerifiedMeasurement_rejectsNonPositiveValue() {
        let op = ApplyVerifiedMeasurementOperation(targetKind: .wall, targetID: "wall_1", field: .thickness, value: 0, status: .estimated)
        XCTAssertThrowsError(try op.validate())
    }

    // MARK: - visual asset binding operations (RP4D)

    func test_assignVisualAsset_assignsToFixture() throws {
        let op = AssignVisualAssetOperation(targetKind: .fixture, targetID: "fixture_1", assetId: "boiler-asset", version: 1)
        let updated = try op.apply(to: draftWithOneFixture())
        XCTAssertEqual(updated.fixtures[0].visualAsset, VisualAssetReference(assetId: "boiler-asset", version: 1))
    }

    func test_assignVisualAsset_assignsToObject() throws {
        let op = AssignVisualAssetOperation(targetKind: .object, targetID: "object_1", assetId: "sofa-asset", version: 2)
        let updated = try op.apply(to: draftWithOneObject())
        XCTAssertEqual(updated.objects[0].visualAsset, VisualAssetReference(assetId: "sofa-asset", version: 2))
    }

    func test_assignVisualAsset_rejectsMissingFixtureTarget() {
        let op = AssignVisualAssetOperation(targetKind: .fixture, targetID: "nonexistent", assetId: "a", version: 1)
        XCTAssertThrowsError(try op.apply(to: draftWithOneFixture())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    func test_assignVisualAsset_rejectsMissingObjectTarget() {
        let op = AssignVisualAssetOperation(targetKind: .object, targetID: "nonexistent", assetId: "a", version: 1)
        XCTAssertThrowsError(try op.apply(to: draftWithOneObject())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    func test_assignVisualAsset_rejectsEmptyTargetID() {
        let op = AssignVisualAssetOperation(targetKind: .fixture, targetID: "", assetId: "a", version: 1)
        XCTAssertThrowsError(try op.validate()) { error in
            XCTAssertEqual(error as? EditOperationError, .invalidOperation)
        }
    }

    func test_assignVisualAsset_rejectsEmptyAssetID() {
        let op = AssignVisualAssetOperation(targetKind: .fixture, targetID: "fixture_1", assetId: "", version: 1)
        XCTAssertThrowsError(try op.validate())
    }

    func test_assignVisualAsset_rejectsZeroVersion() {
        let op = AssignVisualAssetOperation(targetKind: .fixture, targetID: "fixture_1", assetId: "a", version: 0)
        XCTAssertThrowsError(try op.validate())
    }

    func test_clearVisualAsset_clearsFixtureBinding() throws {
        var draft = draftWithOneFixture()
        draft.fixtures[0].visualAsset = VisualAssetReference(assetId: "a", version: 1)
        let updated = try ClearVisualAssetOperation(targetKind: .fixture, targetID: "fixture_1").apply(to: draft)
        XCTAssertNil(updated.fixtures[0].visualAsset)
    }

    func test_clearVisualAsset_clearsObjectBinding() throws {
        var draft = draftWithOneObject()
        draft.objects[0].visualAsset = VisualAssetReference(assetId: "a", version: 1)
        let updated = try ClearVisualAssetOperation(targetKind: .object, targetID: "object_1").apply(to: draft)
        XCTAssertNil(updated.objects[0].visualAsset)
    }

    func test_clearVisualAsset_rejectsMissingTarget() {
        let op = ClearVisualAssetOperation(targetKind: .fixture, targetID: "nonexistent")
        XCTAssertThrowsError(try op.apply(to: draftWithOneFixture())) { error in
            XCTAssertEqual(error as? EditOperationError, .targetNotFound)
        }
    }

    func test_clearVisualAsset_rejectsEmptyTargetID() {
        let op = ClearVisualAssetOperation(targetKind: .fixture, targetID: "")
        XCTAssertThrowsError(try op.validate())
    }
}
