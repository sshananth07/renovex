import Foundation

/// Identifies which capture provider produced an element's initial evidence
/// (design spec §8.9). This is provenance metadata only — it is never used
/// as, or substituted for, a Renovex-owned stable identity (`WallID`,
/// `ObjectID`, etc.).
public enum SpatialCaptureSourceProvider: String, Equatable, Hashable, Sendable, Codable {
    case roomplan
    /// Reserved for a possible future revived Android/ARCore provider
    /// (design spec §8.5). Not produced by any code today.
    case arcore
    /// Used by `FixtureCaptureProvider` and other deterministic test
    /// doubles so fixture-sourced elements are never confused with a real
    /// capture provider's output.
    case fixture
}

/// Provenance carried on every normalized Renovex spatial element (design
/// spec §8.9): which provider produced it, and that provider's own
/// identifier for it. `sourceElementIdentifier` is NOT the element's
/// Renovex identity — it exists so a rescan/rebase pass can use it as one
/// input signal among geometry/semantics, never as a primary key by itself
/// (a rescan may assign an entirely different RoomPlan identifier to what is
/// physically the same wall).
public struct SourceProvenance: Equatable, Hashable, Sendable, Codable {
    public var provider: SpatialCaptureSourceProvider
    /// The provider's own identifier for this element within one capture
    /// session (e.g. a RoomPlan `CapturedRoom.Wall.identifier` UUID).
    public var sourceElementIdentifier: String
    /// The provider's identifier for the overall capture session/result
    /// this element came from, if the provider exposes one distinct from
    /// the per-element identifier.
    public var sourceCaptureIdentifier: String?

    public init(
        provider: SpatialCaptureSourceProvider,
        sourceElementIdentifier: String,
        sourceCaptureIdentifier: String? = nil
    ) {
        self.provider = provider
        self.sourceElementIdentifier = sourceElementIdentifier
        self.sourceCaptureIdentifier = sourceCaptureIdentifier
    }
}
