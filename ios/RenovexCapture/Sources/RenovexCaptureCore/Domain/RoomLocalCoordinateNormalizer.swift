import Foundation

/// A provider-neutral 4x4 column-major transform expressed as 16 raw
/// doubles in the SOURCE provider's coordinate space (RoomPlan session
/// space, for the RoomPlan provider) — deliberately the same shape as
/// `RoomLocalMatrix4x4` but a distinct type, so a caller can never
/// accidentally pass an un-normalized source-space transform where a
/// canonical Renovex room-local transform is expected. This is the
/// provider-neutral "normalization input" half of the boundary described
/// in the RP2 amendment §10:
/// ```text
/// CapturedRoom / RoomPlan Surface     (Mac only)
///         ↓ thin Apple extraction
/// SourceSpaceTransform                (provider-neutral, Windows testable)
///         ↓
/// deterministic Renovex normalization
///         ↓
/// RoomLocalTransform
/// ```
/// `RenovexCaptureRoomPlan` is responsible for converting `simd_float4x4`
/// into this type (a thin, mechanical column-copy); everything past that
/// point is pure, deterministic, Apple-free math that lives here in Core.
public struct SourceSpaceTransform: Equatable, Sendable {
    /// 16 elements, column-major — matches `simd_float4x4`'s memory layout
    /// exactly, so the Apple-side extraction step is a direct field copy
    /// with no reinterpretation.
    public var columns: [Double]

    public init(columns: [Double]) {
        precondition(columns.count == 16, "SourceSpaceTransform requires exactly 16 elements")
        self.columns = columns
    }

    public static let identity = SourceSpaceTransform(columns: [
        1, 0, 0, 0,
        0, 1, 0, 0,
        0, 0, 1, 0,
        0, 0, 0, 1,
    ])

    /// Extracts the translation (4th column) as a point in source space.
    public var translation: RoomLocalPoint {
        RoomLocalPoint(x: columns[12], y: columns[13], z: columns[14])
    }
}

/// Deterministic RoomPlan-session-space → Renovex room-local frame
/// conversion (design spec §8.8), implemented as pure, provider-neutral
/// math so it is fully Windows-testable independent of any RoomPlan API
/// shape risk. `RoomPlanCaptureAdapter` (in `RenovexCaptureRoomPlan`) is
/// the only caller — this type itself never imports RoomPlan.
///
/// Convention (fixed, per design spec §8.8 and RP1's own established
/// passthrough assumption, carried forward and now made explicit and
/// origin-aware): source space is already metric, right-handed, Y-up —
/// matching what the canonical Renovex room-local frame requires — so no
/// axis remapping occurs. What RP2 adds beyond RP1's raw passthrough is
/// explicit, callable ORIGIN RE-BASING: every point is re-expressed
/// relative to a chosen room-local origin rather than the raw RoomPlan
/// session origin, per §8.8's "origin: the room-local origin is
/// adapter-defined at normalization time" requirement. If Xcode
/// verification later reveals RoomPlan's actual session space uses a
/// different handedness/up-axis than assumed, only this type's point/
/// transform conversion functions need to change — no other component
/// reinterprets coordinates (§8.8's single-conversion-point rule).
public struct RoomLocalCoordinateNormalizer: Sendable {
    /// The room-local origin, expressed in source (RoomPlan session) space
    /// — every converted point/transform is re-based relative to this.
    /// Design spec §8.8 leaves the exact origin convention ("first
    /// confirmed floor-plane point or room bounding-box corner") as an
    /// implementation decision; RP2 exposes it as an explicit, testable
    /// parameter rather than hard-coding either choice, so the decision can
    /// be finalized against real RoomPlan floor-plane data once Xcode
    /// access exists without needing to touch this type's math again.
    public let originInSourceSpace: RoomLocalPoint

    public init(originInSourceSpace: RoomLocalPoint = RoomLocalPoint(x: 0, y: 0, z: 0)) {
        self.originInSourceSpace = originInSourceSpace
    }

    /// Converts a point from source space to the canonical Renovex
    /// room-local frame: axis/handedness passthrough (per the convention
    /// above) plus origin re-basing (subtracting `originInSourceSpace`).
    public func convertPoint(_ sourceSpacePoint: RoomLocalPoint) -> RoomLocalPoint {
        RoomLocalPoint(
            x: sourceSpacePoint.x - originInSourceSpace.x,
            y: sourceSpacePoint.y - originInSourceSpace.y,
            z: sourceSpacePoint.z - originInSourceSpace.z
        )
    }

    /// Converts a full transform (position + rotation) from source space to
    /// the canonical Renovex room-local frame. Rotation itself is
    /// axis-convention-invariant under the current passthrough assumption
    /// (no axis remapping, only translation re-basing), so only the
    /// translation component of `transform` is re-based; the rotation
    /// quaternion extracted from the column-major matrix passes through
    /// unchanged, normalized to Renovex's non-negative-`w` sign convention
    /// (design spec §8.8: "unit quaternions, Renovex-normalized sign").
    public func convertTransform(_ transform: SourceSpaceTransform) -> RoomLocalTransform {
        let position = convertPoint(transform.translation)
        let rotation = Self.quaternion(fromColumnMajorRotation: transform)
        return RoomLocalTransform(position: position, rotation: rotation)
    }

    /// Extracts a Renovex-sign-normalized unit quaternion from a
    /// column-major 4x4 transform's upper-left 3x3 rotation block, using
    /// the standard trace-based extraction (Shepperd's method), which is
    /// numerically stable across all rotation angles unlike the naive
    /// square-root-of-trace-only formula.
    static func quaternion(fromColumnMajorRotation transform: SourceSpaceTransform) -> RoomLocalQuaternion {
        let m = transform.columns
        // Column-major: column c, row r is at index c*4 + r.
        let m00 = m[0], m10 = m[1], m20 = m[2]
        let m01 = m[4], m11 = m[5], m21 = m[6]
        let m02 = m[8], m12 = m[9], m22 = m[10]

        let trace = m00 + m11 + m22
        var x = 0.0, y = 0.0, z = 0.0, w = 0.0

        if trace > 0 {
            let s = (trace + 1.0).squareRoot() * 2 // s = 4 * w
            w = 0.25 * s
            x = (m21 - m12) / s
            y = (m02 - m20) / s
            z = (m10 - m01) / s
        } else if m00 > m11 && m00 > m22 {
            let s = (1.0 + m00 - m11 - m22).squareRoot() * 2 // s = 4 * x
            w = (m21 - m12) / s
            x = 0.25 * s
            y = (m01 + m10) / s
            z = (m02 + m20) / s
        } else if m11 > m22 {
            let s = (1.0 + m11 - m00 - m22).squareRoot() * 2 // s = 4 * y
            w = (m02 - m20) / s
            x = (m01 + m10) / s
            y = 0.25 * s
            z = (m12 + m21) / s
        } else {
            let s = (1.0 + m22 - m00 - m11).squareRoot() * 2 // s = 4 * z
            w = (m10 - m01) / s
            x = (m02 + m20) / s
            y = (m12 + m21) / s
            z = 0.25 * s
        }

        // Renovex-normalized sign: constrain w non-negative so a rotation
        // and its negation (mathematically equivalent quaternions) always
        // round-trip to the same canonical value (design spec §8.8).
        if w < 0 {
            x = -x; y = -y; z = -z; w = -w
        }
        return RoomLocalQuaternion(x: x, y: y, z: z, w: w)
    }
}
