import Foundation

/// The kind of architectural/scan element a live RoomPlan callback reports.
/// Provider-neutral — `RenovexCaptureRoomPlan` maps `CapturedRoom.Wall`/
/// `.Object`/`.Opening`/etc. into this before handing anything to
/// `LiveElementTracker`, so the tracker itself never depends on RoomPlan
/// types (design spec §8.7's "no Apple-specific types outside the adapter"
/// rule, extended to the live-session boundary).
public enum LiveElementKind: Equatable, Hashable, Sendable {
    case wall
    case opening
    case object
}

/// A provider-neutral snapshot of one live element's current geometry, as
/// reported by a capture provider's `didAdd`/`didChange`/`didUpdate`
/// callback (design spec §8.10). This is intentionally NOT `RoomDraftWall`/
/// `RoomDraftOpening`/`RoomDraftObject` — those are Renovex's normalized,
/// stable-ID-bearing domain types; `LiveElementSnapshot` is the raw,
/// source-ID-keyed evidence a live session reports moment to moment, before
/// final normalization assigns a Renovex-owned ID. Keeping these types
/// distinct is what prevents a RoomPlan source ID from ever accidentally
/// being treated as permanent Renovex identity (design spec §8.9).
public struct LiveElementSnapshot: Equatable, Sendable {
    /// The capture provider's own identifier for this element — for
    /// RoomPlan, `CapturedRoom.Wall.identifier.uuidString` etc. Opaque and
    /// provider-defined; `LiveElementTracker` only ever compares these for
    /// equality, never interprets their contents.
    public let sourceIdentifier: String
    public let kind: LiveElementKind
    /// The element's current transform in whatever coordinate space the
    /// caller is working in at this stage — for the live-tracking use case
    /// this is normalization-input space (pre- or post-canonical-frame
    /// conversion; `LiveElementTracker` is agnostic to which, since its job
    /// is identity/dedup bookkeeping, not coordinate math).
    public let transform: RoomLocalTransform
    /// Element-kind-specific dimensions (wall width/height/thickness,
    /// opening width/height, object bounding dimensions) as a generic
    /// 3-tuple; interpretation is the caller's responsibility based on
    /// `kind`.
    public let dimensions: RoomLocalPoint?
    /// RoomPlan's own category string for `.object` elements (e.g. "sofa");
    /// `nil` for `.wall`/`.opening`.
    public let category: String?

    public init(
        sourceIdentifier: String,
        kind: LiveElementKind,
        transform: RoomLocalTransform,
        dimensions: RoomLocalPoint? = nil,
        category: String? = nil
    ) {
        self.sourceIdentifier = sourceIdentifier
        self.kind = kind
        self.transform = transform
        self.dimensions = dimensions
        self.category = category
    }
}
