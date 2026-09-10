import Foundation

/// One wall in a `RoomDraft` — the smallest architectural element needed to
/// prove the RP1 capture-provider → adapter → domain boundary end-to-end.
///
/// Scope note: full contractor-correction editing (design spec §8.14) is
/// RP4 scope, not RP2. `RoomDraftWall` here only carries what RP2's
/// normalization can determine from RoomPlan output — no invented
/// authoritative wall thickness where RoomPlan does not provide a reliable
/// value (RP2 amendment §6).
public struct RoomDraftWall: Equatable, Sendable, Codable {
    public var id: WallID
    public var start: RoomLocalPoint
    public var end: RoomLocalPoint
    /// Wall height in metres, when the source provider reports one.
    public var height: Double?
    /// Wall thickness in metres, when the source provider reports one.
    /// RoomPlan's own thickness estimate is exactly that — an estimate, not
    /// contractor-confirmed truth (design spec §8.14's worked example:
    /// "RoomPlan estimate: 160 mm ... Applying 180 mm modifies RoomDraft
    /// wall geometry"). `thicknessStatus` records that this value is a
    /// scan estimate; it never becomes authoritative until an explicit
    /// RP4/RP5 verification step overwrites it.
    public var thickness: Double?
    public var thicknessStatus: MeasurementStatus
    public var provenance: SourceProvenance

    public init(
        id: WallID,
        start: RoomLocalPoint,
        end: RoomLocalPoint,
        height: Double? = nil,
        thickness: Double? = nil,
        thicknessStatus: MeasurementStatus = .unconfirmed,
        provenance: SourceProvenance
    ) {
        self.id = id
        self.start = start
        self.end = end
        self.height = height
        self.thickness = thickness
        self.thicknessStatus = thicknessStatus
        self.provenance = provenance
    }
}

/// How confident/authoritative a measurement value is (design spec §8.18,
/// §8.27's uncertainty/confidence states, narrowed to RP2's actual scope:
/// distinguishing a raw RoomPlan estimate from anything contractor-verified
/// — the full evidence-provenance model with AR/physical verification tiers
/// is RP5 scope, not implemented here).
///
/// RP4A correction: this enum's Codable conformance was originally
/// synthesized from a plain (non-`String`-backed) enum, which encodes as
/// `{"estimated":{}}` rather than the plain `"estimated"` string Go's
/// `MeasurementStatus` (a Go `string` type) produces — a genuine
/// cross-platform wire-contract bug, caught while building RP4A's
/// wire-contract parity tests. `rawValue`/`init(rawValue:)` now match Go's
/// values exactly, and `encode(to:)` always emits the new plain-string
/// form. `init(from:)` additionally accepts the OLD synthesized shape so a
/// `RoomDraft` persisted locally by RP2/RP3 before this fix (e.g. via
/// `FileRoomDraftRepository`) still decodes correctly — the moment it is
/// next saved, it is rewritten in the new canonical form.
public enum MeasurementStatus: String, Equatable, Sendable {
    /// A RoomPlan-reported estimate; not contractor-reviewed.
    case estimated
    /// No reliable value is available from the source provider at all —
    /// distinct from `estimated`, which still carries SOME value.
    case unconfirmed
}

extension MeasurementStatus: Codable {
    public init(from decoder: Decoder) throws {
        // New canonical shape: a plain string.
        if let container = try? decoder.singleValueContainer(),
           let raw = try? container.decode(String.self),
           let value = MeasurementStatus(rawValue: raw) {
            self = value
            return
        }
        // Legacy shape from the pre-RP4A synthesized Codable conformance:
        // {"estimated":{}} or {"unconfirmed":{}} — a keyed container whose
        // single key names the case.
        let keyed = try decoder.container(keyedBy: CodingKeys.self)
        if keyed.contains(.estimated) {
            self = .estimated
        } else if keyed.contains(.unconfirmed) {
            self = .unconfirmed
        } else {
            throw DecodingError.dataCorrupted(DecodingError.Context(
                codingPath: decoder.codingPath, debugDescription: "Unrecognized MeasurementStatus encoding"
            ))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }

    private enum CodingKeys: String, CodingKey {
        case estimated
        case unconfirmed
    }
}

/// Opening classification, aligned literally with design spec §6.1's
/// authoritative opening types (RP4A) — door | window | archway | other.
/// NOT RP2's original door|window|opening set: RoomPlan's generic-opening
/// detection (no stronger evidence for door/window/archway) now normalizes
/// to `.other`, not a dedicated `.opening` case that never existed in the
/// authoritative §6.1 vocabulary — see `RoomDraftNormalizer.openingKind(for:)`
/// for the exact mapping.
public enum OpeningKind: String, Equatable, Sendable, Codable {
    case door
    case window
    case archway
    case other
}

/// Orthogonal to `OpeningKind` (design spec §6.2) — a door or window may
/// have either profile; `.archway` as a `Kind` describes function (no
/// door/window purpose), while `Profile` describes the opening's actual
/// geometric shape.
public enum OpeningProfile: String, Equatable, Sendable, Codable {
    case rectangle
    case arch
}

/// Only meaningful when an opening's `profile` is `.arch` (design spec
/// §6.2).
public struct ArchParameters: Equatable, Sendable, Codable {
    public var springHeight: Double
    public var archRise: Double
    public var radius: Double?

    public init(springHeight: Double, archRise: Double, radius: Double? = nil) {
        self.springHeight = springHeight
        self.archRise = archRise
        self.radius = radius
    }
}

/// Which side of the opening a door's hinge is on, viewed from the
/// room-local frame's canonical orientation.
public enum DoorHinge: String, Equatable, Sendable, Codable {
    case left
    case right
}

/// Which direction a door swings relative to the wall plane.
public enum DoorSwing: String, Equatable, Sendable, Codable {
    case inward
    case outward
}

/// Only meaningful when a `RoomDraftOpening`'s `kind` is `.door` (design
/// spec §8.15: "leaf count, hinge side, swing direction,
/// inward/outward/open direction").
public struct DoorMetadata: Equatable, Sendable, Codable {
    public var leafCount: Int
    public var hinge: DoorHinge?
    public var swing: DoorSwing?
    public var openDirection: String?

    public init(leafCount: Int, hinge: DoorHinge? = nil, swing: DoorSwing? = nil, openDirection: String? = nil) {
        self.leafCount = leafCount
        self.hinge = hinge
        self.swing = swing
        self.openDirection = openDirection
    }
}

/// One opening (door, window, archway, or other) normalized from a
/// RoomPlan-detected element or added by a contractor (RP4A: add_opening).
/// RoomPlan's classification is preserved as-is initially, not
/// contractor-confirmed truth — reclassify_opening/door-specific operations
/// mutate this after normalization (RP4A adds the domain shape; the
/// interactive editor UI applying them is RP4B, not implemented here).
public struct RoomDraftOpening: Equatable, Sendable, Codable {
    public var id: OpeningID
    /// The wall this opening is set into, when RoomPlan reports a parent
    /// surface relationship. `nil` if RoomPlan's output does not resolve
    /// one for this element.
    public var parentWallID: WallID?
    public var kind: OpeningKind
    public var profile: OpeningProfile
    public var transform: RoomLocalTransform
    /// Width/height in metres, when available.
    public var width: Double?
    public var height: Double?
    /// The opening's position measured along `parentWallID` from its start
    /// point (design spec §6.2) — an explicit wall-relative coordinate for
    /// 2D plan editing, distinct from `transform.position`'s room-local
    /// (not wall-relative) coordinates.
    public var offsetAlongWall: Double?
    /// Only meaningful for windows (design spec §8.15).
    public var sillHeight: Double?
    /// Only meaningful when `profile` is `.arch`.
    public var archParameters: ArchParameters?
    /// Only meaningful when `kind` is `.door`.
    public var door: DoorMetadata?
    public var provenance: SourceProvenance

    public init(
        id: OpeningID,
        parentWallID: WallID? = nil,
        kind: OpeningKind,
        profile: OpeningProfile = .rectangle,
        transform: RoomLocalTransform,
        width: Double? = nil,
        height: Double? = nil,
        offsetAlongWall: Double? = nil,
        sillHeight: Double? = nil,
        archParameters: ArchParameters? = nil,
        door: DoorMetadata? = nil,
        provenance: SourceProvenance
    ) {
        self.id = id
        self.parentWallID = parentWallID
        self.kind = kind
        self.profile = profile
        self.transform = transform
        self.width = width
        self.height = height
        self.offsetAlongWall = offsetAlongWall
        self.sillHeight = sillHeight
        self.archParameters = archParameters
        self.door = door
        self.provenance = provenance
    }
}

/// One object normalized from a RoomPlan-detected category (design spec
/// §8.16). `category` carries RoomPlan's own reported category string
/// as-is — Apple's finite object taxonomy is a scan seed, never the
/// Renovex domain taxonomy itself.
/// The canonical, persisted per-element visual-asset binding (RP4D) —
/// identity + exact immutable version only, never a URL/storage key.
/// Property names spelled to match Go's JSON keys VERBATIM: Swift's
/// synthesized `Codable` uses the property name itself as the wire key.
public struct VisualAssetReference: Equatable, Sendable, Codable {
    public var assetId: String
    public var version: Int

    public init(assetId: String, version: Int) {
        self.assetId = assetId
        self.version = version
    }
}

public struct RoomDraftObject: Equatable, Sendable, Codable {
    public var id: ObjectID
    /// RoomPlan's own reported category for this object (e.g. "sofa",
    /// "table", "refrigerator") — a provider-neutral string, not an enum,
    /// since RoomPlan's supported category set is Apple's to define and
    /// Renovex must not hard-code assumptions about its exact members.
    public var category: String
    public var transform: RoomLocalTransform
    public var dimensions: RoomLocalPoint?
    public var provenance: SourceProvenance
    /// Optional canonical binding to a published VisualAssetVersion
    /// (RP4D) — `nil` means "no explicit binding," which the Web renderer
    /// falls back to category-default/procedural resolution for. Swift
    /// never verifies this reference against a server-side asset store;
    /// wire-contract parity and local structural validation only.
    public var visualAsset: VisualAssetReference?

    public init(
        id: ObjectID,
        category: String,
        transform: RoomLocalTransform,
        dimensions: RoomLocalPoint? = nil,
        provenance: SourceProvenance,
        visualAsset: VisualAssetReference? = nil
    ) {
        self.id = id
        self.category = category
        self.transform = transform
        self.dimensions = dimensions
        self.provenance = provenance
        self.visualAsset = visualAsset
    }
}

/// Discriminates a `RoomDraftFixture`/`RoomDraftServicePoint`/
/// `RoomDraftConstraint`'s authorship (RP4A, design spec §8.16).
/// Wall/Opening/Object elements do not carry this — §8.13's operation
/// vocabulary has no add_wall, and every V1 wall/opening/object is
/// capture-derived, so the discriminated-provenance concern is specific to
/// the three element kinds a contractor can create from nothing.
public enum ElementOrigin: String, Equatable, Sendable, Codable {
    case capture
    case contractor
}

/// Design spec §8.16's FixedFixtures vocabulary.
public enum FixtureCategory: String, Equatable, Sendable, Codable {
    case ac
    case boiler
    case builtInCabinetry = "built_in_cabinetry"
    case wallFixture = "wall_fixture"
    case electricalPanel = "electrical_panel"
    case other
}

/// A FixedFixture (design spec §8.16/§7) — a renovation-relevant fixed
/// element RoomPlan's finite object vocabulary does not detect (AC units,
/// boilers, built-in cabinetry, wall fixtures, electrical panels).
/// Distinct from `RoomDraftObject`: fixtures are never RoomPlan-detected in
/// V1 (`createdBy` is always `.contractor` today; the field exists for
/// forward-compatibility with a future capture provider that could detect
/// fixtures directly).
public struct RoomDraftFixture: Equatable, Sendable, Codable {
    public var id: FixtureID
    public var category: FixtureCategory
    public var transform: RoomLocalTransform
    public var dimensions: RoomLocalPoint?
    /// Set for a wall-mounted fixture (e.g. a wall lamp or electrical
    /// panel); `nil` for a floor-standing fixture (e.g. a boiler).
    public var parentWallID: WallID?
    public var createdBy: ElementOrigin
    /// Present only when `createdBy` is `.capture`. `nil` for a
    /// contractor-created fixture — never a synthesized provider ID.
    public var provenance: SourceProvenance?
    /// Optional canonical binding to a published VisualAssetVersion
    /// (RP4D) — see `RoomDraftObject.visualAsset`'s identical doc comment.
    public var visualAsset: VisualAssetReference?

    public init(
        id: FixtureID,
        category: FixtureCategory,
        transform: RoomLocalTransform,
        dimensions: RoomLocalPoint? = nil,
        parentWallID: WallID? = nil,
        createdBy: ElementOrigin,
        provenance: SourceProvenance? = nil,
        visualAsset: VisualAssetReference? = nil
    ) {
        self.id = id
        self.category = category
        self.transform = transform
        self.dimensions = dimensions
        self.parentWallID = parentWallID
        self.createdBy = createdBy
        self.provenance = provenance
        self.visualAsset = visualAsset
    }
}

/// Design spec §7.2/§8.16's controlled-point vocabulary.
public enum ServicePointKind: String, Equatable, Sendable, Codable {
    case plumbing
    case electrical
    case drain
    case gas
    case data
}

/// A controlled point (design spec §7.2) — not a complete building service
/// network. Point-like (a single `RoomLocalPoint`, not a full transform)
/// since a service point has no meaningful rotation.
public struct RoomDraftServicePoint: Equatable, Sendable, Codable {
    public var id: ServicePointID
    public var kind: ServicePointKind
    public var position: RoomLocalPoint
    public var parentWallID: WallID?
    public var createdBy: ElementOrigin
    public var provenance: SourceProvenance?

    public init(
        id: ServicePointID,
        kind: ServicePointKind,
        position: RoomLocalPoint,
        parentWallID: WallID? = nil,
        createdBy: ElementOrigin,
        provenance: SourceProvenance? = nil
    ) {
        self.id = id
        self.kind = kind
        self.position = position
        self.parentWallID = parentWallID
        self.createdBy = createdBy
        self.provenance = provenance
    }
}

/// Design spec §7.1/§8.16's obstacle vocabulary.
public enum ConstraintKind: String, Equatable, Sendable, Codable {
    case column
    case staircase
    case immovableObstacle = "immovable_obstacle"
}

/// An obstacle (design spec §7.1) that participates in collision validation
/// and layout constraints.
public struct RoomDraftConstraint: Equatable, Sendable, Codable {
    public var id: ConstraintID
    public var kind: ConstraintKind
    public var transform: RoomLocalTransform
    public var dimensions: RoomLocalPoint?
    public var createdBy: ElementOrigin
    public var provenance: SourceProvenance?

    public init(
        id: ConstraintID,
        kind: ConstraintKind,
        transform: RoomLocalTransform,
        dimensions: RoomLocalPoint? = nil,
        createdBy: ElementOrigin,
        provenance: SourceProvenance? = nil
    ) {
        self.id = id
        self.kind = kind
        self.transform = transform
        self.dimensions = dimensions
        self.createdBy = createdBy
        self.provenance = provenance
    }
}

/// The immutable snapshot of a `RoomDraft`'s geometry at normalization
/// time, captured once before any contractor edit (design spec §8.14
/// "Reset to Scan"). It never changes after being set. Mirrors the Go
/// `RoomDraftBaseline` type exactly (`backend/internal/spatial/roomdraft.go`)
/// so Reset to Scan means the same thing on both platforms (RP4A).
public struct RoomDraftBaseline: Equatable, Sendable, Codable {
    public var walls: [RoomDraftWall]
    public var openings: [RoomDraftOpening]
    public var objects: [RoomDraftObject]

    public init(walls: [RoomDraftWall] = [], openings: [RoomDraftOpening] = [], objects: [RoomDraftObject] = []) {
        self.walls = walls
        self.openings = openings
        self.objects = objects
    }
}

/// The canonical Renovex editable spatial model (design spec §8.11).
/// `RoomDraft` is the single mutable contractor-review model shared
/// conceptually across iOS and Web — RoomPlan's rendered USDZ or built-in
/// post-scan view is never this domain model (§8.21).
public struct RoomDraft: Equatable, Sendable, Codable {
    public var walls: [RoomDraftWall]
    public var openings: [RoomDraftOpening]
    public var objects: [RoomDraftObject]
    /// RP4A additions (design spec §8.16, §7). Deliberately absent from
    /// `originalBaseline` below: Reset to Scan (design spec §8.14/§8.25)
    /// restores walls/openings/objects to their captured baseline but
    /// clears these three arrays entirely, since a contractor-created
    /// fixture/service-point/constraint never existed in the capture
    /// baseline to begin with.
    public var fixtures: [RoomDraftFixture]
    public var servicePoints: [RoomDraftServicePoint]
    public var constraints: [RoomDraftConstraint]
    /// Provenance for the draft as a whole (which capture produced it),
    /// distinct from each element's own `SourceProvenance` — a `RoomDraft`
    /// may in principle later merge/rebase evidence from more than one
    /// capture (design spec §28), so per-element provenance is preserved
    /// independently of this draft-level value.
    public var sourceCaptureIdentifier: String?
    public var sourceProvider: SpatialCaptureSourceProvider
    /// Preserves the normalized-from-capture geometry exactly as first
    /// produced, before any contractor edit (design spec §8.14 "Reset to
    /// Scan", RP4A — mirrors the backend `RoomDraft.OriginalBaseline` added
    /// in RP3). `nil` until the first edit is made — `walls`/`openings`/
    /// `objects` above ARE the baseline until then, so no redundant copy is
    /// stored for an unedited draft.
    public var originalBaseline: RoomDraftBaseline?

    public init(
        walls: [RoomDraftWall] = [],
        openings: [RoomDraftOpening] = [],
        objects: [RoomDraftObject] = [],
        fixtures: [RoomDraftFixture] = [],
        servicePoints: [RoomDraftServicePoint] = [],
        constraints: [RoomDraftConstraint] = [],
        sourceCaptureIdentifier: String? = nil,
        sourceProvider: SpatialCaptureSourceProvider,
        originalBaseline: RoomDraftBaseline? = nil
    ) {
        self.walls = walls
        self.openings = openings
        self.objects = objects
        self.fixtures = fixtures
        self.servicePoints = servicePoints
        self.constraints = constraints
        self.sourceCaptureIdentifier = sourceCaptureIdentifier
        self.sourceProvider = sourceProvider
        self.originalBaseline = originalBaseline
    }
}

/// Errors `RoomDraft.resettingToBaseline()` may surface.
public enum RoomDraftResetError: Error, Equatable, Sendable {
    /// The draft has never been edited (`originalBaseline` is `nil`) — there
    /// is nothing to reset to that would differ from the draft's current
    /// state. Mirrors the Go service's `ErrRoomDraftHasNoBaseline`.
    case noBaseline
}

extension RoomDraft {
    /// Returns a copy of this draft with walls/openings/objects restored to
    /// exactly `originalBaseline`'s contents (design spec §8.14/§8.25
    /// "Reset to Scan", RP4A) and fixtures/servicePoints/constraints
    /// cleared entirely — a contractor-created element never existed in the
    /// capture baseline, so resetting to it removes them rather than
    /// leaving them orphaned. Stable IDs of restored elements are exactly
    /// `originalBaseline`'s own (the same objects, same IDs — never
    /// regenerated). Mirrors
    /// `spatial.Service.ResetRoomDraftToBaseline`'s semantics exactly, as a
    /// pure domain operation with no repository/network dependency —
    /// persisting the result is the caller's responsibility (this is the
    /// domain/repository primitive only; RP4A does not wire reset UI or
    /// sync orchestration).
    public func resettingToBaseline() throws(RoomDraftResetError) -> RoomDraft {
        guard let baseline = originalBaseline else { throw .noBaseline }
        var reset = self
        reset.walls = baseline.walls
        reset.openings = baseline.openings
        reset.objects = baseline.objects
        reset.fixtures = []
        reset.servicePoints = []
        reset.constraints = []
        return reset
    }
}
