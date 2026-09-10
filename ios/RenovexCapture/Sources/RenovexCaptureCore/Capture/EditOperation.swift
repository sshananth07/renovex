import Foundation

/// This file implements design spec §8.13's shared geometry-edit operation
/// vocabulary (plan RP4A). The wire discriminator (a "kind" string) plus
/// each operation's typed field set is the actual canonical cross-platform
/// contract — these Swift structs are one conforming implementation,
/// mirroring `backend/internal/spatial/editoperation.go`'s Go structs, not
/// the contract's source of truth. Both sides are proven to agree via
/// matching JSON fixtures in each language's test suite (the same pattern
/// RP2's CoordinateContractTests established for the coordinate frame), not
/// a shared schema file.
///
/// RP4A scope: contract + validation + application against an in-memory
/// `RoomDraft`. No HTTP client call exists yet for submitting these
/// operations (RP4B). Validation here is target-existence and shape
/// validation only — NOT Task 14's safety classification, which belongs to
/// the separate AI design-proposal pipeline and does not apply to direct
/// contractor edits of a RoomDraft.
///
/// Explicitly NOT implemented (documented gap, not silently invented):
/// merge_wall/split_wall — see the Go file's header comment for the full
/// rationale, identical here.

/// Errors an `EditOperation` conformer may surface.
public enum EditOperationError: Error, Equatable, Sendable {
    case invalidOperation
    case targetNotFound
}

/// Every one of the 27 operation types below satisfies this: a wire-
/// discriminator identity, shape validation, and application against a
/// `RoomDraft`. This is the seam a future RP4B networking layer will
/// encode from and submit through.
public protocol EditOperation: Sendable {
    static var kind: EditOperationKind { get }
    func validate() throws(EditOperationError)
    func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft
}

/// The wire discriminator every edit operation payload carries alongside
/// its typed fields.
public enum EditOperationKind: String, Equatable, Sendable, Codable {
    case moveCorner = "move_corner"
    case moveWall = "move_wall"
    case setWallThickness = "set_wall_thickness"
    case addOpening = "add_opening"
    case removeOpening = "remove_opening"
    case moveOpening = "move_opening"
    case resizeOpening = "resize_opening"
    case reclassifyOpening = "reclassify_opening"
    case setDoorLeafCount = "set_door_leaf_count"
    case setDoorHinge = "set_door_hinge"
    case setDoorSwing = "set_door_swing"
    case setDoorOpenDirection = "set_door_open_direction"
    case addObject = "add_object"
    case moveObject = "move_object"
    case rotateObject = "rotate_object"
    case resizeObject = "resize_object"
    case reclassifyObject = "reclassify_object"
    case removeObject = "remove_object"
    case addFixture = "add_fixture"
    case moveFixture = "move_fixture"
    case resizeFixture = "resize_fixture"
    case reclassifyFixture = "reclassify_fixture"
    case removeFixture = "remove_fixture"
    case addServicePoint = "add_service_point"
    case moveServicePoint = "move_service_point"
    case removeServicePoint = "remove_service_point"
    case addConstraint = "add_constraint"
    case moveConstraint = "move_constraint"
    case removeConstraint = "remove_constraint"
    case applyVerifiedMeasurement = "apply_verified_measurement"
    case assignVisualAsset = "assign_visual_asset"
    case clearVisualAsset = "clear_visual_asset"
}

/// The project's canonical geometry tolerance for validating (never
/// auto-detecting) that a caller-supplied set of wall endpoints plausibly
/// represents one shared corner — matches
/// `CoincidentWallSanityCheck.defaultEpsilonMeters` (2cm) and Go's
/// `cornerCoincidenceEpsilonMeters`. Declared once here since this file is
/// already in the same target as `CoincidentWallSanityCheck`.
public let cornerCoincidenceEpsilonMeters: Double = CoincidentWallSanityCheck.defaultEpsilonMeters

// MARK: - Wall / geometry operations

/// Identifies one end of one wall — the atomic unit `MoveCornerOperation`
/// moves.
public struct WallEndpointRef: Equatable, Sendable, Codable {
    public enum Endpoint: String, Equatable, Sendable, Codable {
        case start
        case end
    }
    public var wallID: WallID
    public var endpoint: Endpoint

    public init(wallID: WallID, endpoint: Endpoint) {
        self.wallID = wallID
        self.endpoint = endpoint
    }
}

/// Moves a shared corner by moving every explicitly listed (wallID,
/// endpoint) pair to `newPosition` atomically. Endpoints are supplied by
/// the caller, never auto-detected by coincidence-searching — mutation
/// scope is exactly the supplied set. `validate` checks that the supplied
/// endpoints are plausibly coincident (within `cornerCoincidenceEpsilonMeters`
/// of each other) before `apply` moves them; this is a sanity check, not
/// scope discovery.
public struct MoveCornerOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveCorner
    public var endpoints: [WallEndpointRef]
    public var newPosition: RoomLocalPoint

    public init(endpoints: [WallEndpointRef], newPosition: RoomLocalPoint) {
        self.endpoints = endpoints
        self.newPosition = newPosition
    }

    public func validate() throws(EditOperationError) {
        if endpoints.isEmpty { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        var indices: [Int] = []
        var referencePosition: RoomLocalPoint?
        for ref in endpoints {
            guard let idx = draft.walls.firstIndex(where: { $0.id == ref.wallID }) else {
                throw .targetNotFound
            }
            let current = ref.endpoint == .start ? draft.walls[idx].start : draft.walls[idx].end
            if let reference = referencePosition {
                if distance(reference, current) > cornerCoincidenceEpsilonMeters {
                    throw .invalidOperation
                }
            } else {
                referencePosition = current
            }
            indices.append(idx)
        }
        for (i, ref) in endpoints.enumerated() {
            if ref.endpoint == .start {
                draft.walls[indices[i]].start = newPosition
            } else {
                draft.walls[indices[i]].end = newPosition
            }
        }
        return draft
    }
}

/// Translates both endpoints of one wall by `delta`.
public struct MoveWallOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveWall
    public var wallID: WallID
    public var delta: RoomLocalPoint

    public init(wallID: WallID, delta: RoomLocalPoint) {
        self.wallID = wallID
        self.delta = delta
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        guard let idx = draft.walls.firstIndex(where: { $0.id == wallID }) else {
            throw .targetNotFound
        }
        draft.walls[idx].start = draft.walls[idx].start + delta
        draft.walls[idx].end = draft.walls[idx].end + delta
        return draft
    }
}

/// Sets a wall's `thickness` under a caller-supplied `MeasurementStatus` —
/// the contractor-correction path (design spec §8.14): RoomPlan's own
/// thickness estimate never becomes authoritative merely by existing.
public struct SetWallThicknessOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .setWallThickness
    public var wallID: WallID
    public var thickness: Double
    public var status: MeasurementStatus

    public init(wallID: WallID, thickness: Double, status: MeasurementStatus) {
        self.wallID = wallID
        self.thickness = thickness
        self.status = status
    }

    public func validate() throws(EditOperationError) {
        if thickness <= 0 { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        guard let idx = draft.walls.firstIndex(where: { $0.id == wallID }) else {
            throw .targetNotFound
        }
        draft.walls[idx].thickness = thickness
        draft.walls[idx].thicknessStatus = status
        return draft
    }
}

// MARK: - Opening operations

/// Adds a new opening to `parentWallID`. Always contractor-authored (no
/// capture provenance) — RP4A's add_opening always originates from a
/// contractor action.
public struct AddOpeningOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .addOpening
    public var id: OpeningID
    public var parentWallID: WallID
    public var openingKind: OpeningKind
    public var profile: OpeningProfile
    public var transform: RoomLocalTransform
    public var width: Double
    public var height: Double
    public var offsetAlongWall: Double

    public init(
        id: OpeningID, parentWallID: WallID, openingKind: OpeningKind, profile: OpeningProfile,
        transform: RoomLocalTransform, width: Double, height: Double, offsetAlongWall: Double
    ) {
        self.id = id
        self.parentWallID = parentWallID
        self.openingKind = openingKind
        self.profile = profile
        self.transform = transform
        self.width = width
        self.height = height
        self.offsetAlongWall = offsetAlongWall
    }

    public func validate() throws(EditOperationError) {
        if width <= 0 || height <= 0 { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        guard draft.walls.contains(where: { $0.id == parentWallID }) else { throw .targetNotFound }
        guard !draft.openings.contains(where: { $0.id == id }) else { throw .invalidOperation }
        draft.openings.append(RoomDraftOpening(
            id: id, parentWallID: parentWallID, kind: openingKind, profile: profile,
            transform: transform, width: width, height: height, offsetAlongWall: offsetAlongWall,
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: id.rawValue)
        ))
        return draft
    }
}

/// Removes one opening by ID.
public struct RemoveOpeningOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .removeOpening
    public var openingID: OpeningID

    public init(openingID: OpeningID) { self.openingID = openingID }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.openings.firstIndex(where: { $0.id == openingID }) else {
            throw .targetNotFound
        }
        draft.openings.remove(at: idx)
        return draft
    }
}

/// Updates an opening's position along its parent wall and/or its
/// room-local transform.
public struct MoveOpeningOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveOpening
    public var openingID: OpeningID
    public var transform: RoomLocalTransform
    public var offsetAlongWall: Double

    public init(openingID: OpeningID, transform: RoomLocalTransform, offsetAlongWall: Double) {
        self.openingID = openingID
        self.transform = transform
        self.offsetAlongWall = offsetAlongWall
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.openings.firstIndex(where: { $0.id == openingID }) else {
            throw .targetNotFound
        }
        draft.openings[idx].transform = transform
        draft.openings[idx].offsetAlongWall = offsetAlongWall
        return draft
    }
}

/// Sets an opening's width/height.
public struct ResizeOpeningOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .resizeOpening
    public var openingID: OpeningID
    public var width: Double
    public var height: Double

    public init(openingID: OpeningID, width: Double, height: Double) {
        self.openingID = openingID
        self.width = width
        self.height = height
    }

    public func validate() throws(EditOperationError) {
        if width <= 0 || height <= 0 { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        guard let idx = draft.openings.firstIndex(where: { $0.id == openingID }) else {
            throw .targetNotFound
        }
        draft.openings[idx].width = width
        draft.openings[idx].height = height
        return draft
    }
}

/// Changes an opening's kind and/or profile (design spec §8.15: opening
/// reclassification, orthogonally to rectangle/arch profile change).
public struct ReclassifyOpeningOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .reclassifyOpening
    public var openingID: OpeningID
    public var openingKind: OpeningKind
    public var profile: OpeningProfile

    public init(openingID: OpeningID, openingKind: OpeningKind, profile: OpeningProfile) {
        self.openingID = openingID
        self.openingKind = openingKind
        self.profile = profile
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.openings.firstIndex(where: { $0.id == openingID }) else {
            throw .targetNotFound
        }
        draft.openings[idx].kind = openingKind
        draft.openings[idx].profile = profile
        // Reclassifying away from door discards stale door metadata.
        if openingKind != .door {
            draft.openings[idx].door = nil
        }
        return draft
    }
}

// MARK: - Door-specific operations
// Each targets an opening whose kind must already be .door — applying a
// door operation to a non-door opening is a target-validity error, not a
// silent no-op or an implicit reclassification.

private func findDoorIndex(in openings: [RoomDraftOpening], openingID: OpeningID) throws(EditOperationError) -> Int {
    guard let idx = openings.firstIndex(where: { $0.id == openingID }) else { throw .targetNotFound }
    guard openings[idx].kind == .door else { throw .invalidOperation }
    return idx
}

private func ensureDoorMetadata(_ opening: inout RoomDraftOpening) {
    if opening.door == nil {
        opening.door = DoorMetadata(leafCount: 0)
    }
}

public struct SetDoorLeafCountOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .setDoorLeafCount
    public var openingID: OpeningID
    public var leafCount: Int

    public init(openingID: OpeningID, leafCount: Int) {
        self.openingID = openingID
        self.leafCount = leafCount
    }

    public func validate() throws(EditOperationError) {
        if leafCount < 1 || leafCount > 2 { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        let idx = try findDoorIndex(in: draft.openings, openingID: openingID)
        ensureDoorMetadata(&draft.openings[idx])
        draft.openings[idx].door?.leafCount = leafCount
        return draft
    }
}

public struct SetDoorHingeOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .setDoorHinge
    public var openingID: OpeningID
    public var hinge: DoorHinge

    public init(openingID: OpeningID, hinge: DoorHinge) {
        self.openingID = openingID
        self.hinge = hinge
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        let idx = try findDoorIndex(in: draft.openings, openingID: openingID)
        ensureDoorMetadata(&draft.openings[idx])
        draft.openings[idx].door?.hinge = hinge
        return draft
    }
}

public struct SetDoorSwingOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .setDoorSwing
    public var openingID: OpeningID
    public var swing: DoorSwing

    public init(openingID: OpeningID, swing: DoorSwing) {
        self.openingID = openingID
        self.swing = swing
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        let idx = try findDoorIndex(in: draft.openings, openingID: openingID)
        ensureDoorMetadata(&draft.openings[idx])
        draft.openings[idx].door?.swing = swing
        return draft
    }
}

public struct SetDoorOpenDirectionOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .setDoorOpenDirection
    public var openingID: OpeningID
    public var openDirection: String

    public init(openingID: OpeningID, openDirection: String) {
        self.openingID = openingID
        self.openDirection = openDirection
    }

    public func validate() throws(EditOperationError) {
        if openDirection.isEmpty { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        let idx = try findDoorIndex(in: draft.openings, openingID: openingID)
        ensureDoorMetadata(&draft.openings[idx])
        draft.openings[idx].door?.openDirection = openDirection
        return draft
    }
}

// MARK: - Object operations

public struct AddObjectOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .addObject
    public var id: ObjectID
    public var category: String
    public var transform: RoomLocalTransform
    public var dimensions: RoomLocalPoint?

    public init(id: ObjectID, category: String, transform: RoomLocalTransform, dimensions: RoomLocalPoint? = nil) {
        self.id = id
        self.category = category
        self.transform = transform
        self.dimensions = dimensions
    }

    public func validate() throws(EditOperationError) {
        if category.isEmpty { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        guard !draft.objects.contains(where: { $0.id == id }) else { throw .invalidOperation }
        draft.objects.append(RoomDraftObject(
            id: id, category: category, transform: transform, dimensions: dimensions,
            provenance: SourceProvenance(provider: .fixture, sourceElementIdentifier: id.rawValue)
        ))
        return draft
    }
}

public struct MoveObjectOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveObject
    public var objectID: ObjectID
    public var position: RoomLocalPoint

    public init(objectID: ObjectID, position: RoomLocalPoint) {
        self.objectID = objectID
        self.position = position
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.objects.firstIndex(where: { $0.id == objectID }) else { throw .targetNotFound }
        draft.objects[idx].transform.position = position
        return draft
    }
}

public struct RotateObjectOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .rotateObject
    public var objectID: ObjectID
    public var rotation: RoomLocalQuaternion

    public init(objectID: ObjectID, rotation: RoomLocalQuaternion) {
        self.objectID = objectID
        self.rotation = rotation
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.objects.firstIndex(where: { $0.id == objectID }) else { throw .targetNotFound }
        draft.objects[idx].transform.rotation = rotation
        return draft
    }
}

public struct ResizeObjectOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .resizeObject
    public var objectID: ObjectID
    public var dimensions: RoomLocalPoint

    public init(objectID: ObjectID, dimensions: RoomLocalPoint) {
        self.objectID = objectID
        self.dimensions = dimensions
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.objects.firstIndex(where: { $0.id == objectID }) else { throw .targetNotFound }
        draft.objects[idx].dimensions = dimensions
        return draft
    }
}

public struct ReclassifyObjectOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .reclassifyObject
    public var objectID: ObjectID
    public var category: String

    public init(objectID: ObjectID, category: String) {
        self.objectID = objectID
        self.category = category
    }

    public func validate() throws(EditOperationError) {
        if category.isEmpty { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        guard let idx = draft.objects.firstIndex(where: { $0.id == objectID }) else { throw .targetNotFound }
        draft.objects[idx].category = category
        return draft
    }
}

public struct RemoveObjectOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .removeObject
    public var objectID: ObjectID

    public init(objectID: ObjectID) { self.objectID = objectID }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.objects.firstIndex(where: { $0.id == objectID }) else { throw .targetNotFound }
        draft.objects.remove(at: idx)
        return draft
    }
}

// MARK: - Fixture operations
// Every fixture RP4A's vocabulary can create is contractor-authored — V1
// has no capture provider that detects FixedFixtures (design spec §8.16).

public struct AddFixtureOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .addFixture
    public var id: FixtureID
    public var category: FixtureCategory
    public var transform: RoomLocalTransform
    public var dimensions: RoomLocalPoint?
    public var parentWallID: WallID?

    public init(id: FixtureID, category: FixtureCategory, transform: RoomLocalTransform, dimensions: RoomLocalPoint? = nil, parentWallID: WallID? = nil) {
        self.id = id
        self.category = category
        self.transform = transform
        self.dimensions = dimensions
        self.parentWallID = parentWallID
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        if let parentWallID, !draft.walls.contains(where: { $0.id == parentWallID }) {
            throw .targetNotFound
        }
        guard !draft.fixtures.contains(where: { $0.id == id }) else { throw .invalidOperation }
        draft.fixtures.append(RoomDraftFixture(
            id: id, category: category, transform: transform, dimensions: dimensions,
            parentWallID: parentWallID, createdBy: .contractor
        ))
        return draft
    }
}

public struct MoveFixtureOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveFixture
    public var fixtureID: FixtureID
    public var transform: RoomLocalTransform

    public init(fixtureID: FixtureID, transform: RoomLocalTransform) {
        self.fixtureID = fixtureID
        self.transform = transform
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.fixtures.firstIndex(where: { $0.id == fixtureID }) else { throw .targetNotFound }
        draft.fixtures[idx].transform = transform
        return draft
    }
}

public struct ResizeFixtureOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .resizeFixture
    public var fixtureID: FixtureID
    public var dimensions: RoomLocalPoint

    public init(fixtureID: FixtureID, dimensions: RoomLocalPoint) {
        self.fixtureID = fixtureID
        self.dimensions = dimensions
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.fixtures.firstIndex(where: { $0.id == fixtureID }) else { throw .targetNotFound }
        draft.fixtures[idx].dimensions = dimensions
        return draft
    }
}

public struct ReclassifyFixtureOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .reclassifyFixture
    public var fixtureID: FixtureID
    public var category: FixtureCategory

    public init(fixtureID: FixtureID, category: FixtureCategory) {
        self.fixtureID = fixtureID
        self.category = category
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.fixtures.firstIndex(where: { $0.id == fixtureID }) else { throw .targetNotFound }
        draft.fixtures[idx].category = category
        return draft
    }
}

/// RP4C2 addition — RP4A originally implemented only add/move/resize/
/// reclassify for fixtures.
public struct RemoveFixtureOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .removeFixture
    public var fixtureID: FixtureID

    public init(fixtureID: FixtureID) { self.fixtureID = fixtureID }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.fixtures.firstIndex(where: { $0.id == fixtureID }) else { throw .targetNotFound }
        draft.fixtures.remove(at: idx)
        return draft
    }
}

// MARK: - Service point operations
// §8.13 lists only add/move for service points.

public struct AddServicePointOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .addServicePoint
    public var id: ServicePointID
    public var servicePointKind: ServicePointKind
    public var position: RoomLocalPoint
    public var parentWallID: WallID?

    public init(id: ServicePointID, servicePointKind: ServicePointKind, position: RoomLocalPoint, parentWallID: WallID? = nil) {
        self.id = id
        self.servicePointKind = servicePointKind
        self.position = position
        self.parentWallID = parentWallID
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        if let parentWallID, !draft.walls.contains(where: { $0.id == parentWallID }) {
            throw .targetNotFound
        }
        guard !draft.servicePoints.contains(where: { $0.id == id }) else { throw .invalidOperation }
        draft.servicePoints.append(RoomDraftServicePoint(
            id: id, kind: servicePointKind, position: position, parentWallID: parentWallID, createdBy: .contractor
        ))
        return draft
    }
}

public struct MoveServicePointOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveServicePoint
    public var servicePointID: ServicePointID
    public var position: RoomLocalPoint

    public init(servicePointID: ServicePointID, position: RoomLocalPoint) {
        self.servicePointID = servicePointID
        self.position = position
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.servicePoints.firstIndex(where: { $0.id == servicePointID }) else { throw .targetNotFound }
        draft.servicePoints[idx].position = position
        return draft
    }
}

/// RP4C2 addition — RP4A originally implemented only add/move for service
/// points.
public struct RemoveServicePointOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .removeServicePoint
    public var servicePointID: ServicePointID

    public init(servicePointID: ServicePointID) { self.servicePointID = servicePointID }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.servicePoints.firstIndex(where: { $0.id == servicePointID }) else { throw .targetNotFound }
        draft.servicePoints.remove(at: idx)
        return draft
    }
}

// MARK: - Constraint operations
// §8.13 lists only add/move for constraints, matching service points.

public struct AddConstraintOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .addConstraint
    public var id: ConstraintID
    public var constraintKind: ConstraintKind
    public var transform: RoomLocalTransform
    public var dimensions: RoomLocalPoint?

    public init(id: ConstraintID, constraintKind: ConstraintKind, transform: RoomLocalTransform, dimensions: RoomLocalPoint? = nil) {
        self.id = id
        self.constraintKind = constraintKind
        self.transform = transform
        self.dimensions = dimensions
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard !draft.constraints.contains(where: { $0.id == id }) else { throw .invalidOperation }
        draft.constraints.append(RoomDraftConstraint(
            id: id, kind: constraintKind, transform: transform, dimensions: dimensions, createdBy: .contractor
        ))
        return draft
    }
}

public struct MoveConstraintOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .moveConstraint
    public var constraintID: ConstraintID
    public var transform: RoomLocalTransform

    public init(constraintID: ConstraintID, transform: RoomLocalTransform) {
        self.constraintID = constraintID
        self.transform = transform
    }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.constraints.firstIndex(where: { $0.id == constraintID }) else { throw .targetNotFound }
        draft.constraints[idx].transform = transform
        return draft
    }
}

/// RP4C2 addition — RP4A originally implemented only add/move for
/// constraints.
public struct RemoveConstraintOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .removeConstraint
    public var constraintID: ConstraintID

    public init(constraintID: ConstraintID) { self.constraintID = constraintID }

    public func validate() throws(EditOperationError) {}

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        var draft = draft
        guard let idx = draft.constraints.firstIndex(where: { $0.id == constraintID }) else { throw .targetNotFound }
        draft.constraints.remove(at: idx)
        return draft
    }
}

// MARK: - Measurement operation

/// Identifies which RoomDraft element kind `ApplyVerifiedMeasurementOperation`
/// targets.
public enum MeasurementTargetKind: String, Equatable, Sendable, Codable {
    case wall
    case opening
}

/// Identifies which numeric field on the target element the verified value
/// applies to.
public enum MeasurementField: String, Equatable, Sendable, Codable {
    case thickness
    case height
    case width
    case sillHeight
}

/// Design spec §8.13/§8.14's single generic measurement-correction
/// operation. RP4A implements this against `MeasurementStatus`
/// (estimated/unconfirmed) only — the full AR_VERIFIED/PHYSICAL_VERIFIED
/// evidence-provenance model is RP5 scope.
public struct ApplyVerifiedMeasurementOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .applyVerifiedMeasurement
    public var targetKind: MeasurementTargetKind
    public var targetID: String
    public var field: MeasurementField
    public var value: Double
    public var status: MeasurementStatus

    public init(targetKind: MeasurementTargetKind, targetID: String, field: MeasurementField, value: Double, status: MeasurementStatus) {
        self.targetKind = targetKind
        self.targetID = targetID
        self.field = field
        self.value = value
        self.status = status
    }

    public func validate() throws(EditOperationError) {
        if targetID.isEmpty || value <= 0 { throw .invalidOperation }
        switch targetKind {
        case .wall:
            if field != .thickness && field != .height { throw .invalidOperation }
        case .opening:
            switch field {
            case .width, .height, .sillHeight: break
            case .thickness: throw .invalidOperation
            }
        }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        switch targetKind {
        case .wall:
            guard let idx = draft.walls.firstIndex(where: { $0.id.rawValue == targetID }) else { throw .targetNotFound }
            switch field {
            case .thickness:
                draft.walls[idx].thickness = value
                draft.walls[idx].thicknessStatus = status
            case .height:
                draft.walls[idx].height = value
            default: break
            }
        case .opening:
            guard let idx = draft.openings.firstIndex(where: { $0.id.rawValue == targetID }) else { throw .targetNotFound }
            switch field {
            case .width: draft.openings[idx].width = value
            case .height: draft.openings[idx].height = value
            case .sillHeight: draft.openings[idx].sillHeight = value
            default: break
            }
        }
        return draft
    }
}

// MARK: - Visual asset binding operations (RP4D)

/// Identifies which RoomDraft element kind
/// `AssignVisualAssetOperation`/`ClearVisualAssetOperation` targets. Narrow
/// by design — RP4D's visual-asset binding applies only to fixtures and
/// objects, never walls/openings/service points/constraints.
public enum VisualAssetTargetKind: String, Equatable, Sendable, Codable {
    case fixture
    case object
}

/// Sets a fixture/object's canonical `VisualAssetReference` (RP4D) —
/// identity + exact version only. This is a presentation-reference
/// mutation only: it never touches geometry, classification, provenance,
/// transform, or dimensions. `validate()` here is LOCAL/structural only
/// (shape, non-empty IDs, positive version) — it cannot verify the
/// referenced asset version actually exists or belongs to the caller's
/// company, which requires server access and is the Go backend's
/// `Service.SubmitEditOperation` pre-flight check's job, not Swift's.
public struct AssignVisualAssetOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .assignVisualAsset
    public var targetKind: VisualAssetTargetKind
    public var targetID: String
    public var assetId: String
    public var version: Int

    public init(targetKind: VisualAssetTargetKind, targetID: String, assetId: String, version: Int) {
        self.targetKind = targetKind
        self.targetID = targetID
        self.assetId = assetId
        self.version = version
    }

    public func validate() throws(EditOperationError) {
        if targetID.isEmpty || assetId.isEmpty || version < 1 { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        let ref = VisualAssetReference(assetId: assetId, version: version)
        switch targetKind {
        case .fixture:
            guard let idx = draft.fixtures.firstIndex(where: { $0.id.rawValue == targetID }) else { throw .targetNotFound }
            draft.fixtures[idx].visualAsset = ref
        case .object:
            guard let idx = draft.objects.firstIndex(where: { $0.id.rawValue == targetID }) else { throw .targetNotFound }
            draft.objects[idx].visualAsset = ref
        }
        return draft
    }
}

/// Removes a fixture/object's canonical `VisualAssetReference` (RP4D),
/// returning it to category-default/procedural resolution. No other field
/// changes.
public struct ClearVisualAssetOperation: EditOperation, Equatable, Sendable, Codable {
    public static let kind: EditOperationKind = .clearVisualAsset
    public var targetKind: VisualAssetTargetKind
    public var targetID: String

    public init(targetKind: VisualAssetTargetKind, targetID: String) {
        self.targetKind = targetKind
        self.targetID = targetID
    }

    public func validate() throws(EditOperationError) {
        if targetID.isEmpty { throw .invalidOperation }
    }

    public func apply(to draft: RoomDraft) throws(EditOperationError) -> RoomDraft {
        try validate()
        var draft = draft
        switch targetKind {
        case .fixture:
            guard let idx = draft.fixtures.firstIndex(where: { $0.id.rawValue == targetID }) else { throw .targetNotFound }
            draft.fixtures[idx].visualAsset = nil
        case .object:
            guard let idx = draft.objects.firstIndex(where: { $0.id.rawValue == targetID }) else { throw .targetNotFound }
            draft.objects[idx].visualAsset = nil
        }
        return draft
    }
}

// MARK: - shared helpers

private func distance(_ a: RoomLocalPoint, _ b: RoomLocalPoint) -> Double {
    let dx = a.x - b.x, dy = a.y - b.y, dz = a.z - b.z
    return (dx * dx + dy * dy + dz * dz).squareRoot()
}

private func + (lhs: RoomLocalPoint, rhs: RoomLocalPoint) -> RoomLocalPoint {
    RoomLocalPoint(x: lhs.x + rhs.x, y: lhs.y + rhs.y, z: lhs.z + rhs.z)
}
