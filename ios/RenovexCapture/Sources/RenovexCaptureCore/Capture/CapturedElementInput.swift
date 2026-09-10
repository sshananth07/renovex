import Foundation

/// How confident the source provider reports being in one detected
/// element. Provider-neutral — RoomPlan's own confidence enum (whatever its
/// exact cases turn out to be) is mapped into this by the Apple-side
/// bridge, never referenced directly by Core.
public enum CaptureConfidence: Equatable, Sendable {
    case low
    case medium
    case high
    case unknown
}

/// Which architectural semantic a `CapturedSurfaceInput` represents. Driven
/// by which RoomPlan collection the surface came from
/// (`capturedRoom.doors`/`.windows`/`.openings`), not solely by RoomPlan's
/// own `Surface.category` — Apple exposes separate arrays for exactly this
/// purpose, so the Apple-side bridge should tag each surface by its source
/// collection rather than re-deriving the semantic from category alone.
public enum SurfaceSemanticKind: Equatable, Sendable {
    case door(isOpen: Bool?)
    case window
    case opening
}

/// The minimal provider-neutral input a `CapturedSurfaceInput` normalizer
/// needs to produce a `RoomDraftOpening` — NOT a mirror of RoomPlan's
/// `CapturedRoom.Surface` API. Only what deterministic normalization
/// actually requires. Constructing one of these from a real
/// `CapturedRoom.Surface` is the Apple-side bridge's entire job
/// (`RenovexCaptureRoomPlan`); everything from here down is Windows
/// testable.
public struct CapturedSurfaceInput: Equatable, Sendable {
    public let sourceIdentifier: String
    /// The RoomPlan source identifier of the wall this surface is set
    /// into, if RoomPlan reports one — a raw provider identifier, not yet
    /// resolved to a Renovex `WallID`. Resolution against already-assigned
    /// `WallID`s happens in the normalizer, not here.
    public let parentSourceIdentifier: String?
    public let kind: SurfaceSemanticKind
    public let transform: SourceSpaceTransform
    /// (width, height, thickness) in source-space units.
    public let dimensions: RoomLocalPoint
    public let confidence: CaptureConfidence

    public init(
        sourceIdentifier: String,
        parentSourceIdentifier: String?,
        kind: SurfaceSemanticKind,
        transform: SourceSpaceTransform,
        dimensions: RoomLocalPoint,
        confidence: CaptureConfidence
    ) {
        self.sourceIdentifier = sourceIdentifier
        self.parentSourceIdentifier = parentSourceIdentifier
        self.kind = kind
        self.transform = transform
        self.dimensions = dimensions
        self.confidence = confidence
    }
}

/// The minimal provider-neutral input needed to produce a
/// `RoomDraftObject` — NOT a mirror of RoomPlan's `CapturedRoom.Object`
/// API. `providerCategory` is already a plain Renovex-neutral string by the
/// time it reaches this type — mapping RoomPlan's `Object.Category` enum
/// cases to a string is the Apple-side bridge's job (an explicit switch in
/// `RenovexCaptureRoomPlan`, never `String(describing:)`, so the mapping is
/// an intentional, reviewable contract rather than an incidental Swift
/// enum-case name).
public struct CapturedObjectInput: Equatable, Sendable {
    public let sourceIdentifier: String
    public let parentSourceIdentifier: String?
    public let providerCategory: String
    public let transform: SourceSpaceTransform
    public let dimensions: RoomLocalPoint
    public let confidence: CaptureConfidence

    public init(
        sourceIdentifier: String,
        parentSourceIdentifier: String?,
        providerCategory: String,
        transform: SourceSpaceTransform,
        dimensions: RoomLocalPoint,
        confidence: CaptureConfidence
    ) {
        self.sourceIdentifier = sourceIdentifier
        self.parentSourceIdentifier = parentSourceIdentifier
        self.providerCategory = providerCategory
        self.transform = transform
        self.dimensions = dimensions
        self.confidence = confidence
    }
}

/// The minimal provider-neutral input needed to produce a `RoomDraftWall`
/// — generalizes RP1's direct `CapturedRoom.Wall` extraction inside
/// `RoomPlanCaptureAdapter` into the same snapshot-boundary pattern used
/// for surfaces/objects, so wall conversion also becomes Windows-testable
/// independent of the Apple API shape risk.
public struct CapturedWallInput: Equatable, Sendable {
    public let sourceIdentifier: String
    public let transform: SourceSpaceTransform
    /// (width, height, thickness) in source-space units.
    public let dimensions: RoomLocalPoint
    public let confidence: CaptureConfidence

    public init(
        sourceIdentifier: String,
        transform: SourceSpaceTransform,
        dimensions: RoomLocalPoint,
        confidence: CaptureConfidence
    ) {
        self.sourceIdentifier = sourceIdentifier
        self.transform = transform
        self.dimensions = dimensions
        self.confidence = confidence
    }
}
