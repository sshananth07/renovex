import Foundation

/// Converts provider-neutral captured-element inputs (`CapturedWallInput`,
/// `CapturedSurfaceInput`, `CapturedObjectInput`) into a normalized
/// `RoomDraft`, applying coordinate normalization (via
/// `RoomLocalCoordinateNormalizer`), stable Renovex ID assignment, and
/// parent-relationship resolution (RoomPlan source IDs → Renovex `WallID`s,
/// never persisted as Renovex identity themselves — design spec §8.9).
///
/// This is the deterministic core of RP2's normalization work and is fully
/// Windows testable: it has no dependency on RoomPlan or any Apple
/// framework. `RoomPlanCaptureAdapter` (in `RenovexCaptureRoomPlan`) is
/// responsible only for extracting `CapturedWallInput`/
/// `CapturedSurfaceInput`/`CapturedObjectInput` values from real
/// `CapturedRoom` data and handing them to this type — see that target's
/// extraction functions for the (Xcode-pending) Apple API bridge.
public struct RoomDraftNormalizer: Sendable {
    private let coordinateNormalizer: RoomLocalCoordinateNormalizer
    private let idFactory: @Sendable () -> String

    public init(
        coordinateNormalizer: RoomLocalCoordinateNormalizer = RoomLocalCoordinateNormalizer(),
        idFactory: @escaping @Sendable () -> String = { UUID().uuidString }
    ) {
        self.coordinateNormalizer = coordinateNormalizer
        self.idFactory = idFactory
    }

    /// Normalizes a complete set of captured inputs from one capture
    /// session into a `RoomDraft`. Wall stable IDs are assigned first so
    /// `parentSourceIdentifier` on surfaces can be resolved to the matching
    /// `WallID` in the same pass.
    public func normalize(
        walls: [CapturedWallInput],
        surfaces: [CapturedSurfaceInput],
        objects: [CapturedObjectInput],
        sourceCaptureIdentifier: String? = nil,
        sourceProvider: SpatialCaptureSourceProvider = .roomplan
    ) -> RoomDraft {
        var wallIDBySourceIdentifier: [String: WallID] = [:]
        let draftWalls: [RoomDraftWall] = walls.map { input in
            let id = WallID(idFactory())
            wallIDBySourceIdentifier[input.sourceIdentifier] = id
            return normalizeWall(input, id: id, sourceProvider: sourceProvider)
        }

        let draftOpenings: [RoomDraftOpening] = surfaces.map { input in
            normalizeSurface(input, parentWallID: input.parentSourceIdentifier.flatMap { wallIDBySourceIdentifier[$0] }, sourceProvider: sourceProvider)
        }

        let draftObjects: [RoomDraftObject] = objects.map { input in
            normalizeObject(input, sourceProvider: sourceProvider)
        }

        return RoomDraft(
            walls: draftWalls,
            openings: draftOpenings,
            objects: draftObjects,
            sourceCaptureIdentifier: sourceCaptureIdentifier,
            sourceProvider: sourceProvider
        )
    }

    // MARK: Element-level normalization

    private func normalizeWall(_ input: CapturedWallInput, id: WallID, sourceProvider: SpatialCaptureSourceProvider) -> RoomDraftWall {
        let halfWidth = input.dimensions.x / 2.0
        let localStart = SourceSpaceTransform(columns: applyingLocalOffset(input.transform, xOffset: -halfWidth))
        let localEnd = SourceSpaceTransform(columns: applyingLocalOffset(input.transform, xOffset: halfWidth))

        let start = coordinateNormalizer.convertPoint(localStart.translation)
        let end = coordinateNormalizer.convertPoint(localEnd.translation)

        return RoomDraftWall(
            id: id,
            start: start,
            end: end,
            height: input.dimensions.y,
            thickness: input.dimensions.z,
            thicknessStatus: .estimated,
            provenance: SourceProvenance(provider: sourceProvider, sourceElementIdentifier: input.sourceIdentifier)
        )
    }

    private func normalizeSurface(_ input: CapturedSurfaceInput, parentWallID: WallID?, sourceProvider: SpatialCaptureSourceProvider) -> RoomDraftOpening {
        let transform = coordinateNormalizer.convertTransform(input.transform)
        return RoomDraftOpening(
            id: OpeningID(idFactory()),
            parentWallID: parentWallID,
            kind: openingKind(for: input.kind),
            transform: transform,
            width: input.dimensions.x,
            height: input.dimensions.y,
            provenance: SourceProvenance(provider: sourceProvider, sourceElementIdentifier: input.sourceIdentifier)
        )
    }

    private func normalizeObject(_ input: CapturedObjectInput, sourceProvider: SpatialCaptureSourceProvider) -> RoomDraftObject {
        let transform = coordinateNormalizer.convertTransform(input.transform)
        return RoomDraftObject(
            id: ObjectID(idFactory()),
            category: input.providerCategory,
            transform: transform,
            dimensions: input.dimensions,
            provenance: SourceProvenance(provider: sourceProvider, sourceElementIdentifier: input.sourceIdentifier)
        )
    }

    /// RP4A: canonical `OpeningKind` is door|window|archway|other (design
    /// spec §6.1), not RP2's original door|window|opening. RoomPlan's
    /// generic-opening detection has no stronger evidence for
    /// door/window/archway, so it normalizes to `.other` — never a
    /// dedicated "opening" case, which never existed in §6.1's
    /// authoritative vocabulary to begin with.
    private func openingKind(for surfaceKind: SurfaceSemanticKind) -> OpeningKind {
        switch surfaceKind {
        case .door: return .door
        case .window: return .window
        case .opening: return .other
        }
    }

    /// Returns a copy of `transform`'s columns with the translation offset
    /// by `xOffset` along the transform's own local X axis (the transform's
    /// first column, which represents the local X basis vector under the
    /// column-major convention) — used to recover a wall's start/end points
    /// from its center transform + width, matching RP1's original
    /// `convertWall` derivation, generalized to work on the provider-neutral
    /// `SourceSpaceTransform` shape.
    private func applyingLocalOffset(_ transform: SourceSpaceTransform, xOffset: Double) -> [Double] {
        var columns = transform.columns
        let localXBasis = (columns[0], columns[1], columns[2])
        columns[12] += localXBasis.0 * xOffset
        columns[13] += localXBasis.1 * xOffset
        columns[14] += localXBasis.2 * xOffset
        return columns
    }
}
