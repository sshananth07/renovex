import Foundation
import RoomPlan
import simd
import RenovexCaptureCore

// ==================================================================
// APPLE ROOMPLAN API BRIDGE — COMPILE VERIFICATION PENDING MAC/XCODE.
// Keep all CapturedRoom API assumptions confined to this target and,
// within it, to the extraction functions below. Do not let an uncertain
// Apple API shape spread into RenovexCaptureCore's RoomDraftNormalizer,
// which is Windows-verified and must stay that way.
//
// Pending first-Xcode-session compile checks (RP2-MAC-001 through 008):
//   RP2-MAC-001  CapturedRoom.Surface.identifier: UUID
//   RP2-MAC-002  CapturedRoom.Surface.parentIdentifier: UUID?
//   RP2-MAC-003  CapturedRoom.Surface.transform: simd_float4x4
//   RP2-MAC-004  CapturedRoom.Surface.dimensions: simd_float3
//   RP2-MAC-005  CapturedRoom.Surface.Category.door(isOpen: Bool?) case shape
//   RP2-MAC-006  CapturedRoom.Object.identifier / .parentIdentifier: UUID?
//   RP2-MAC-007  CapturedRoom.Object.category exact enum case set
//   RP2-MAC-008  CapturedRoom.Object.transform / .dimensions
//   RP2-MAC-009  CapturedRoom.Wall.identifier / .transform / .dimensions
//                (carried over from RP1, still unverified)
//   RP2-MAC-010  CapturedRoom.Surface.confidence exact enum case set
//   RP2-MAC-011  CapturedRoom.Object.confidence exact enum case set
//   RP2-MAC-012  capturedRoom.doors / .windows / .openings / .objects
//                collection property names
// If any property name/type differs from what is written here, fix this
// file first — do not silently reinterpret RoomPlan's shape elsewhere.
// ==================================================================

/// Converts Apple's `CapturedRoom`/`CapturedRoomData` into the canonical
/// Renovex `RoomDraft` (design spec §8.7, §8.8, §8.9). This is the ONLY
/// place in the codebase that reinterprets RoomPlan's coordinate system or
/// treats a RoomPlan identifier as anything but source provenance.
///
/// Architecture (RP2): this type's job is deliberately thin — extract
/// provider-neutral `CapturedWallInput`/`CapturedSurfaceInput`/
/// `CapturedObjectInput` snapshots from RoomPlan's real types, then hand
/// them to `RoomDraftNormalizer` (in `RenovexCaptureCore`, fully Windows
/// tested) for all coordinate math, stable-ID assignment, and parent
/// resolution. Extraction functions below should stay mechanical field
/// copies, not carry normalization logic themselves — that keeps the
/// Xcode-unverified surface area as small as possible.
public struct RoomPlanCaptureAdapter: SpatialCaptureAdapter {
    public let providerIdentity: SpatialCaptureSourceProvider = .roomplan

    private let normalizer: RoomDraftNormalizer

    public init(normalizer: RoomDraftNormalizer = RoomDraftNormalizer()) {
        self.normalizer = normalizer
    }

    public func makeRoomDraft(from rawResult: SpatialCaptureRawResult) throws(SpatialCaptureAdapterError) -> RoomDraft {
        guard rawResult.provider == .roomplan, let capturedRoom = rawResult.payload as? CapturedRoom else {
            throw .unexpectedPayload
        }
        return makeRoomDraft(from: capturedRoom)
    }

    /// Typed entry point (avoids the `as?` downcast) for callers that
    /// already hold a `CapturedRoom` — used by RP2's fixture-based adapter
    /// tests (once a `CapturedRoom` fixture can be constructed on a real
    /// Mac/Xcode toolchain) and by `RoomPlanCaptureProvider`'s live-session
    /// completion handler.
    public func makeRoomDraft(from capturedRoom: CapturedRoom) -> RoomDraft {
        let wallInputs = capturedRoom.walls.map(extractWallInput)
        let doorInputs = capturedRoom.doors.map { extractSurfaceInput($0, kind: .door(isOpen: doorIsOpen($0))) }
        let windowInputs = capturedRoom.windows.map { extractSurfaceInput($0, kind: .window) }
        let openingInputs = capturedRoom.openings.map { extractSurfaceInput($0, kind: .opening) }
        let objectInputs = capturedRoom.objects.map(extractObjectInput)

        return normalizer.normalize(
            walls: wallInputs,
            surfaces: doorInputs + windowInputs + openingInputs,
            objects: objectInputs,
            // RoomPlan does not expose a session-level identifier on
            // CapturedRoom itself; a session identifier is assigned by
            // RoomPlanCaptureProvider's live-session boundary and threaded
            // through separately (see RoomPlanCaptureProvider.startCapture).
            sourceCaptureIdentifier: nil,
            sourceProvider: .roomplan
        )
    }

    // MARK: Apple API extraction — RP2-MAC-001 through 012, Xcode-pending

    private func extractWallInput(_ wall: CapturedRoom.Wall) -> CapturedWallInput {
        CapturedWallInput(
            sourceIdentifier: wall.identifier.uuidString,
            transform: sourceSpaceTransform(from: wall.transform),
            dimensions: point(from: wall.dimensions),
            confidence: .unknown // RP2-MAC-009: CapturedRoom.Wall does not
            // appear to expose a per-element confidence in the same way
            // Surface/Object do, per current public documentation. If
            // Xcode verification finds one, wire it here.
        )
    }

    private func extractSurfaceInput(_ surface: CapturedRoom.Surface, kind: SurfaceSemanticKind) -> CapturedSurfaceInput {
        CapturedSurfaceInput(
            sourceIdentifier: surface.identifier.uuidString,
            parentSourceIdentifier: surface.parentIdentifier?.uuidString,
            kind: kind,
            transform: sourceSpaceTransform(from: surface.transform),
            dimensions: point(from: surface.dimensions),
            confidence: confidence(from: surface.confidence)
        )
    }

    private func extractObjectInput(_ object: CapturedRoom.Object) -> CapturedObjectInput {
        CapturedObjectInput(
            sourceIdentifier: object.identifier.uuidString,
            parentSourceIdentifier: object.parentIdentifier?.uuidString,
            providerCategory: providerCategory(object.category),
            transform: sourceSpaceTransform(from: object.transform),
            dimensions: point(from: object.dimensions),
            confidence: confidence(from: object.confidence)
        )
    }

    /// RP2-MAC-005: whether `surface.category` reports `.door(isOpen:)`.
    /// Returns `nil` if the surface's category is not a door category at
    /// all (should not happen for an element from `capturedRoom.doors`,
    /// but this stays defensive rather than force-unwrapping an enum
    /// pattern match against an API shape that has not been compiled yet).
    private func doorIsOpen(_ surface: CapturedRoom.Surface) -> Bool? {
        if case let .door(isOpen) = surface.category {
            return isOpen
        }
        return nil
    }

    /// RP2-MAC-007: explicit, reviewable mapping from RoomPlan's
    /// `Object.Category` enum to a plain Renovex-neutral string — never
    /// `String(describing:)`, so the persisted category is an intentional
    /// contract, not an incidental Swift enum-case name that could change
    /// silently with an SDK update. RoomPlan's documented category cases
    /// (bed, chair, refrigerator, sink, sofa, storage, table, television,
    /// toilet, and others) are mapped 1:1 by name; `@unknown default`
    /// covers any category not yet in this list without crashing.
    private func providerCategory(_ category: CapturedRoom.Object.Category) -> String {
        switch category {
        case .storage: return "storage"
        case .refrigerator: return "refrigerator"
        case .stove: return "stove"
        case .bed: return "bed"
        case .sink: return "sink"
        case .washerDryer: return "washer_dryer"
        case .toilet: return "toilet"
        case .bathtub: return "bathtub"
        case .oven: return "oven"
        case .dishwasher: return "dishwasher"
        case .table: return "table"
        case .sofa: return "sofa"
        case .chair: return "chair"
        case .fireplace: return "fireplace"
        case .television: return "television"
        case .stairs: return "stairs"
        @unknown default: return "unknown"
        }
    }

    /// RP2-MAC-010/011: maps RoomPlan's confidence representation to the
    /// provider-neutral `CaptureConfidence`. RoomPlan's documented
    /// `Confidence` enum has `.low`/`.medium`/`.high` cases; `@unknown
    /// default` covers any future addition.
    private func confidence(from roomPlanConfidence: CapturedRoom.Confidence) -> CaptureConfidence {
        switch roomPlanConfidence {
        case .low: return .low
        case .medium: return .medium
        case .high: return .high
        @unknown default: return .unknown
        }
    }

    private func sourceSpaceTransform(from matrix: simd_float4x4) -> SourceSpaceTransform {
        // simd_float4x4's columns are accessible as [0], [1], [2], [3],
        // each a simd_float4 — this mechanical flattening into 16 doubles
        // is the entire coordinate-space assumption this function makes;
        // the actual math lives in RoomLocalCoordinateNormalizer.
        let c0 = matrix[0], c1 = matrix[1], c2 = matrix[2], c3 = matrix[3]
        return SourceSpaceTransform(columns: [
            Double(c0.x), Double(c0.y), Double(c0.z), Double(c0.w),
            Double(c1.x), Double(c1.y), Double(c1.z), Double(c1.w),
            Double(c2.x), Double(c2.y), Double(c2.z), Double(c2.w),
            Double(c3.x), Double(c3.y), Double(c3.z), Double(c3.w),
        ])
    }

    private func point(from vector: simd_float3) -> RoomLocalPoint {
        RoomLocalPoint(x: Double(vector.x), y: Double(vector.y), z: Double(vector.z))
    }
}
