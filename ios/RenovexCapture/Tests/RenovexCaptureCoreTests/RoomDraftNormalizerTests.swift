import XCTest
@testable import RenovexCaptureCore

/// RP2 TDD requirement: "coordinate normalization fixture tests
/// (RoomPlan-shaped input → expected canonical output); stable-ID
/// assignment and provenance preservation." Exercises the full
/// `RoomDraftNormalizer` pipeline against provider-neutral snapshot inputs
/// — genuinely Windows testable, independent of any real RoomPlan API
/// shape (per the RP2-scoped `CapturedWallInput`/`CapturedSurfaceInput`/
/// `CapturedObjectInput` boundary).
final class RoomDraftNormalizerTests: XCTestCase {
    private func makeNormalizer(nextID: @escaping @Sendable () -> String) -> RoomDraftNormalizer {
        RoomDraftNormalizer(idFactory: nextID)
    }

    private func sequentialIDFactory() -> @Sendable () -> String {
        let counter = LockedCounter()
        return { "generated-id-\(counter.increment())" }
    }

    private func identityTransform(translatedBy point: RoomLocalPoint) -> SourceSpaceTransform {
        var transform = SourceSpaceTransform.identity
        transform.columns[12] = point.x
        transform.columns[13] = point.y
        transform.columns[14] = point.z
        return transform
    }

    // MARK: Walls

    func test_normalize_wallOnly_producesExpectedStartEndFromCenterAndWidth() {
        let normalizer = makeNormalizer(nextID: { "wall-renovex-1" })
        let wallInput = CapturedWallInput(
            sourceIdentifier: "roomplan-wall-abc",
            transform: identityTransform(translatedBy: RoomLocalPoint(x: 2, y: 1.2, z: 1.5)),
            dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.16), // width, height, thickness
            confidence: .high
        )

        let draft = normalizer.normalize(walls: [wallInput], surfaces: [], objects: [])

        XCTAssertEqual(draft.walls.count, 1)
        let wall = draft.walls[0]
        XCTAssertEqual(wall.id, WallID("wall-renovex-1"))
        XCTAssertEqual(wall.start, RoomLocalPoint(x: 0, y: 1.2, z: 1.5))
        XCTAssertEqual(wall.end, RoomLocalPoint(x: 4, y: 1.2, z: 1.5))
        XCTAssertEqual(wall.height, 2.4)
        XCTAssertEqual(wall.thickness, 0.16)
        XCTAssertEqual(wall.thicknessStatus, .estimated, "RoomPlan-reported thickness is an estimate, never authoritative")
    }

    func test_normalize_wallProvenance_preservesSourceIdentifier() {
        let normalizer = makeNormalizer(nextID: { "wall-renovex-1" })
        let wallInput = CapturedWallInput(
            sourceIdentifier: "roomplan-wall-abc",
            transform: .identity,
            dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.1),
            confidence: .high
        )

        let draft = normalizer.normalize(walls: [wallInput], surfaces: [], objects: [])

        let wall = draft.walls[0]
        XCTAssertEqual(wall.provenance.provider, .roomplan)
        XCTAssertEqual(wall.provenance.sourceElementIdentifier, "roomplan-wall-abc")
        XCTAssertNotEqual(wall.id.rawValue, wall.provenance.sourceElementIdentifier, "the Renovex stable ID must never equal the RoomPlan source identifier")
    }

    func test_normalize_multipleWalls_eachGetsDistinctStableID() {
        let factory = sequentialIDFactory()
        let normalizer = makeNormalizer(nextID: factory)
        let walls = [
            CapturedWallInput(sourceIdentifier: "rp-1", transform: .identity, dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.1), confidence: .high),
            CapturedWallInput(sourceIdentifier: "rp-2", transform: .identity, dimensions: RoomLocalPoint(x: 3, y: 2.4, z: 0.1), confidence: .high),
        ]

        let draft = normalizer.normalize(walls: walls, surfaces: [], objects: [])

        let ids = Set(draft.walls.map(\.id))
        XCTAssertEqual(ids.count, 2)
    }

    // MARK: Openings (doors/windows/openings)

    func test_normalize_door_producesRoomDraftOpeningWithDoorKind() {
        let normalizer = makeNormalizer(nextID: { "opening-renovex-1" })
        let surfaceInput = CapturedSurfaceInput(
            sourceIdentifier: "roomplan-door-1",
            parentSourceIdentifier: nil,
            kind: .door(isOpen: false),
            transform: .identity,
            dimensions: RoomLocalPoint(x: 0.9, y: 2.0, z: 0.05),
            confidence: .medium
        )

        let draft = normalizer.normalize(walls: [], surfaces: [surfaceInput], objects: [])

        XCTAssertEqual(draft.openings.count, 1)
        let opening = draft.openings[0]
        XCTAssertEqual(opening.kind, .door)
        XCTAssertEqual(opening.width, 0.9)
        XCTAssertEqual(opening.height, 2.0)
        XCTAssertEqual(opening.provenance.sourceElementIdentifier, "roomplan-door-1")
    }

    func test_normalize_window_producesRoomDraftOpeningWithWindowKind() {
        let normalizer = makeNormalizer(nextID: { "opening-renovex-1" })
        let surfaceInput = CapturedSurfaceInput(
            sourceIdentifier: "roomplan-window-1",
            parentSourceIdentifier: nil,
            kind: .window,
            transform: .identity,
            dimensions: RoomLocalPoint(x: 1.2, y: 1.0, z: 0.05),
            confidence: .high
        )

        let draft = normalizer.normalize(walls: [], surfaces: [surfaceInput], objects: [])

        XCTAssertEqual(draft.openings[0].kind, .window)
    }

    /// RP4A: canonical OpeningKind is door|window|archway|other (design
    /// spec §6.1) — RoomPlan's generic-opening detection has no stronger
    /// evidence for door/window/archway, so it normalizes to `.other`, not
    /// a dedicated "opening" case that never existed in §6.1's
    /// authoritative vocabulary.
    func test_normalize_genericOpening_producesRoomDraftOpeningWithOtherKind() {
        let normalizer = makeNormalizer(nextID: { "opening-renovex-1" })
        let surfaceInput = CapturedSurfaceInput(
            sourceIdentifier: "roomplan-opening-1",
            parentSourceIdentifier: nil,
            kind: .opening,
            transform: .identity,
            dimensions: RoomLocalPoint(x: 1.5, y: 2.1, z: 0),
            confidence: .low
        )

        let draft = normalizer.normalize(walls: [], surfaces: [surfaceInput], objects: [])

        XCTAssertEqual(draft.openings[0].kind, .other)
    }

    func test_normalize_surfaceWithParentSourceIdentifier_resolvesToRenovexWallID() {
        // The core "provider identifier is provenance, not identity"
        // requirement applied to parent relationships (design spec §8.9 +
        // the user's approved architecture): a window's RoomPlan parent
        // wall identifier must resolve to the Renovex-owned WallID that was
        // assigned to that same wall in this normalization pass — never
        // stored as the raw RoomPlan parent UUID.
        let wallIDCounter = LockedCounter()
        let normalizer = makeNormalizer(nextID: { "id-\(wallIDCounter.increment())" })

        let wallInput = CapturedWallInput(
            sourceIdentifier: "roomplan-wall-parent",
            transform: .identity,
            dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.1),
            confidence: .high
        )
        let windowInput = CapturedSurfaceInput(
            sourceIdentifier: "roomplan-window-child",
            parentSourceIdentifier: "roomplan-wall-parent",
            kind: .window,
            transform: .identity,
            dimensions: RoomLocalPoint(x: 1, y: 1, z: 0),
            confidence: .high
        )

        let draft = normalizer.normalize(walls: [wallInput], surfaces: [windowInput], objects: [])

        let wall = draft.walls[0]
        let opening = draft.openings[0]
        XCTAssertEqual(opening.parentWallID, wall.id)
        XCTAssertNotEqual(opening.parentWallID?.rawValue, "roomplan-wall-parent")
    }

    func test_normalize_surfaceWithUnresolvableParent_hasNilParentWallID() {
        // If the parent source identifier doesn't match any wall in this
        // normalization pass (e.g. RoomPlan reports a parent RP2 doesn't
        // have wall data for), the opening must not silently attach to the
        // wrong wall or crash — parentWallID becomes nil.
        let normalizer = makeNormalizer(nextID: { "opening-1" })
        let surfaceInput = CapturedSurfaceInput(
            sourceIdentifier: "roomplan-window-orphan",
            parentSourceIdentifier: "roomplan-wall-does-not-exist",
            kind: .window,
            transform: .identity,
            dimensions: RoomLocalPoint(x: 1, y: 1, z: 0),
            confidence: .high
        )

        let draft = normalizer.normalize(walls: [], surfaces: [surfaceInput], objects: [])

        XCTAssertNil(draft.openings[0].parentWallID)
    }

    func test_normalize_surfaceWithNoParent_hasNilParentWallID() {
        let normalizer = makeNormalizer(nextID: { "opening-1" })
        let surfaceInput = CapturedSurfaceInput(
            sourceIdentifier: "roomplan-opening-standalone",
            parentSourceIdentifier: nil,
            kind: .opening,
            transform: .identity,
            dimensions: RoomLocalPoint(x: 1, y: 1, z: 0),
            confidence: .low
        )

        let draft = normalizer.normalize(walls: [], surfaces: [surfaceInput], objects: [])

        XCTAssertNil(draft.openings[0].parentWallID)
    }

    // MARK: Objects

    func test_normalize_object_preservesProviderCategoryAsString() {
        let normalizer = makeNormalizer(nextID: { "object-1" })
        let objectInput = CapturedObjectInput(
            sourceIdentifier: "roomplan-object-sofa",
            parentSourceIdentifier: nil,
            providerCategory: "sofa",
            transform: .identity,
            dimensions: RoomLocalPoint(x: 2.0, y: 0.8, z: 0.9),
            confidence: .medium
        )

        let draft = normalizer.normalize(walls: [], surfaces: [], objects: [objectInput])

        XCTAssertEqual(draft.objects.count, 1)
        XCTAssertEqual(draft.objects[0].category, "sofa")
        XCTAssertEqual(draft.objects[0].dimensions, RoomLocalPoint(x: 2.0, y: 0.8, z: 0.9))
        XCTAssertEqual(draft.objects[0].provenance.sourceElementIdentifier, "roomplan-object-sofa")
    }

    func test_normalize_object_getsDistinctStableIDFromWallsAndOpenings() {
        // ObjectID, WallID, OpeningID are distinct types (compile-time
        // guarantee already proven in SourceProvenanceTests) — this test
        // proves the normalizer's runtime output actually keeps their
        // rawValue spaces distinct rather than colliding through a shared
        // ID-generation counter.
        let counter = LockedCounter()
        let normalizer = makeNormalizer(nextID: { "shared-counter-\(counter.increment())" })

        let wallInput = CapturedWallInput(sourceIdentifier: "w1", transform: .identity, dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.1), confidence: .high)
        let objectInput = CapturedObjectInput(sourceIdentifier: "o1", parentSourceIdentifier: nil, providerCategory: "table", transform: .identity, dimensions: RoomLocalPoint(x: 1, y: 1, z: 1), confidence: .medium)

        let draft = normalizer.normalize(walls: [wallInput], surfaces: [], objects: [objectInput])

        XCTAssertNotEqual(draft.walls[0].id.rawValue, draft.objects[0].id.rawValue)
    }

    // MARK: Rescan / re-normalization

    func test_rescan_sameCapturedGeometry_producesDifferentSourceIdentifiers_geometryStillCorrespondingly_normalizable() {
        // Plan RP2 TDD requirement: "rescan producing different RoomPlan
        // source IDs while geometry-based correspondence remains possible
        // (this is setup for the rebase work in Task 23, not full rebase
        // implementation here)." This test proves normalization does not
        // depend on source identifiers being stable across scans — the
        // SAME physical wall, reported under a DIFFERENT RoomPlan source
        // ID on a second scan, still normalizes to equivalent geometry
        // (which is what a later Task 23 rebase pass would compare on).
        let firstScanNormalizer = makeNormalizer(nextID: { "rescan-wall-first-\(UUID().uuidString)" })
        let secondScanNormalizer = makeNormalizer(nextID: { "rescan-wall-second-\(UUID().uuidString)" })

        let physicalWallGeometry = (
            transform: identityTransform(translatedBy: RoomLocalPoint(x: 2, y: 1.2, z: 0)),
            dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.15)
        )

        let firstScanWall = CapturedWallInput(
            sourceIdentifier: "roomplan-scan1-wall-uuid-AAAA",
            transform: physicalWallGeometry.transform,
            dimensions: physicalWallGeometry.dimensions,
            confidence: .high
        )
        let secondScanWall = CapturedWallInput(
            sourceIdentifier: "roomplan-scan2-wall-uuid-ZZZZ", // deliberately different
            transform: physicalWallGeometry.transform,
            dimensions: physicalWallGeometry.dimensions,
            confidence: .high
        )

        let firstDraft = firstScanNormalizer.normalize(walls: [firstScanWall], surfaces: [], objects: [])
        let secondDraft = secondScanNormalizer.normalize(walls: [secondScanWall], surfaces: [], objects: [])

        // Source identifiers differ (the real-world RoomPlan behavior this
        // sets up for)...
        XCTAssertNotEqual(
            firstDraft.walls[0].provenance.sourceElementIdentifier,
            secondDraft.walls[0].provenance.sourceElementIdentifier
        )
        // ...but the normalized geometry is identical, which is exactly the
        // signal a future rebase pass would use for geometry-based
        // correspondence instead of source-ID equality.
        XCTAssertEqual(firstDraft.walls[0].start, secondDraft.walls[0].start)
        XCTAssertEqual(firstDraft.walls[0].end, secondDraft.walls[0].end)
        // And the assigned Renovex stable IDs are independent across the
        // two scans, as they must be — RP2 does not implement ID
        // continuity across rescans (that is explicitly Task 23 scope).
        XCTAssertNotEqual(firstDraft.walls[0].id, secondDraft.walls[0].id)
    }

    // MARK: Determinism

    func test_normalize_deterministicGeometry_sameInputProducesSameOutput() {
        let normalizer = makeNormalizer(nextID: { "fixed-id" })
        let wallInput = CapturedWallInput(
            sourceIdentifier: "rp-1",
            transform: identityTransform(translatedBy: RoomLocalPoint(x: 1, y: 2, z: 3)),
            dimensions: RoomLocalPoint(x: 4, y: 2.4, z: 0.1),
            confidence: .high
        )

        let firstDraft = normalizer.normalize(walls: [wallInput], surfaces: [], objects: [])
        let secondDraft = normalizer.normalize(walls: [wallInput], surfaces: [], objects: [])

        XCTAssertEqual(firstDraft, secondDraft)
    }

    // MARK: Draft-level metadata

    func test_normalize_setsDraftLevelSourceProviderAndCaptureIdentifier() {
        let normalizer = makeNormalizer(nextID: { "id" })
        let draft = normalizer.normalize(walls: [], surfaces: [], objects: [], sourceCaptureIdentifier: "session-42", sourceProvider: .roomplan)

        XCTAssertEqual(draft.sourceProvider, .roomplan)
        XCTAssertEqual(draft.sourceCaptureIdentifier, "session-42")
    }
}

/// A tiny thread-safe counter for tests that need sequential, distinct IDs
/// (closures used as `idFactory` are `@Sendable` and may in principle be
/// invoked from any isolation context).
final class LockedCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var value = 0

    func increment() -> Int {
        lock.lock()
        defer { lock.unlock() }
        value += 1
        return value
    }
}
