import Foundation

/// Renovex-owned stable identifiers (design spec §8.9). Each wraps a plain
/// string (never a RoomPlan/ARKit identifier) so the type system prevents a
/// provider identifier from being passed where a Renovex identity is
/// expected. Generation strategy (UUID today) is an implementation detail
/// of `RoomPlanCaptureAdapter`/persistence, not part of this contract.
// Types below are already Codable (raw-string-wrapping RawRepresentable
// types synthesize Codable via their rawValue) — kept as-is; this file's
// role in RP3 is only that these identities must round-trip unchanged
// through RoomDraft persistence (plan §RP3: "reopening a persisted draft
// must retain the same WallID/OpeningID/ObjectID identities").
public struct WallID: RawRepresentable, Equatable, Hashable, Sendable, Codable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public init(_ value: String) { self.rawValue = value }
}

public struct OpeningID: RawRepresentable, Equatable, Hashable, Sendable, Codable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public init(_ value: String) { self.rawValue = value }
}

public struct ObjectID: RawRepresentable, Equatable, Hashable, Sendable, Codable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public init(_ value: String) { self.rawValue = value }
}

public struct FixtureID: RawRepresentable, Equatable, Hashable, Sendable, Codable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public init(_ value: String) { self.rawValue = value }
}

public struct ConstraintID: RawRepresentable, Equatable, Hashable, Sendable, Codable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public init(_ value: String) { self.rawValue = value }
}

public struct ServicePointID: RawRepresentable, Equatable, Hashable, Sendable, Codable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public init(_ value: String) { self.rawValue = value }
}
