import Foundation

/// The canonical Renovex room-local coordinate contract (design spec §8.8).
///
/// This is the ONLY coordinate representation any Renovex domain type may
/// use. It is provider-neutral: nothing here references RoomPlan, ARKit, or
/// any other capture SDK. Conversion from a provider's native coordinate
/// space into this frame happens exclusively at that provider's adapter
/// boundary (for RoomPlan: `RoomPlanCaptureAdapter`, in the
/// RenovexCaptureRoomPlan target) — no other component may reinterpret a
/// provider's raw coordinates.
///
/// Convention (fixed here, not per-capture ad hoc):
/// - units: metres
/// - axes: right-handed, Y-up
/// - handedness: right-handed
/// - matrix convention: column-major 4x4 (`RoomLocalMatrix4x4`)
/// - rotation: unit quaternion (`RoomLocalQuaternion`)
/// - origin: adapter-defined at normalization time (e.g. first confirmed
///   floor-plane point or room bounding-box corner); this type does not
///   itself define what the origin IS, only the frame those origin-relative
///   values are expressed in.
public struct RoomLocalPoint: Equatable, Hashable, Sendable, Codable {
    public var x: Double
    public var y: Double
    public var z: Double

    public init(x: Double, y: Double, z: Double) {
        self.x = x
        self.y = y
        self.z = z
    }
}

/// A unit quaternion expressing rotation in the Renovex room-local frame.
/// Renovex-normalized sign: `w` is constrained non-negative wherever a
/// canonical (as opposed to raw provider) quaternion is constructed, so two
/// mathematically-equivalent quaternions (`q` and `-q`) never round-trip as
/// different values inside Renovex domain code. Providers are responsible
/// for normalizing their raw rotation into this convention at their adapter
/// boundary — this type does not normalize on construction, since a fixture
/// test may deliberately construct a non-canonical value to prove adapter
/// normalization behavior.
public struct RoomLocalQuaternion: Equatable, Hashable, Sendable, Codable {
    public var x: Double
    public var y: Double
    public var z: Double
    public var w: Double

    public init(x: Double, y: Double, z: Double, w: Double) {
        self.x = x
        self.y = y
        self.z = z
        self.w = w
    }

    /// The identity rotation.
    public static let identity = RoomLocalQuaternion(x: 0, y: 0, z: 0, w: 1)
}

/// A column-major 4x4 transform in the Renovex room-local frame. Stored as
/// 16 doubles in column-major order (matching `simd_float4x4`'s column-major
/// layout on the provider side and Three.js/R3F's convention on Web) so no
/// row/column transposition is needed when this value crosses into
/// RoomPlan-side conversion code or is serialized for cross-platform
/// contract tests (design spec §8.8.1).
public struct RoomLocalMatrix4x4: Equatable, Sendable, Codable {
    /// 16 elements, column-major: columns[0] is the first column
    /// (elements 0...3), etc.
    public var columns: [Double]

    public init(columns: [Double]) {
        precondition(columns.count == 16, "RoomLocalMatrix4x4 requires exactly 16 elements")
        self.columns = columns
    }

    public static let identity = RoomLocalMatrix4x4(columns: [
        1, 0, 0, 0,
        0, 1, 0, 0,
        0, 0, 1, 0,
        0, 0, 0, 1,
    ])
}

/// A position + rotation in the Renovex room-local frame — the composed
/// transform type most `RoomDraft` elements carry, rather than every
/// element separately choosing between a matrix or a position+quaternion
/// pair.
public struct RoomLocalTransform: Equatable, Sendable, Codable {
    public var position: RoomLocalPoint
    public var rotation: RoomLocalQuaternion

    public init(position: RoomLocalPoint, rotation: RoomLocalQuaternion = .identity) {
        self.position = position
        self.rotation = rotation
    }
}
