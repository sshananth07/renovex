import Foundation

/// A deterministic fixture/fake `SpatialCaptureProvider` for development
/// without LiDAR hardware, SwiftUI preview states, capture ViewModel/state
/// tests, and proving the `SpatialCaptureProvider` abstraction is real (RP1
/// TDD requirement: "a `SpatialCaptureProvider` swap test proving a
/// fake/mock conformer can substitute for `RoomPlanCaptureProvider` without
/// changing downstream code").
///
/// This provider does NOT prove real RoomPlan/LiDAR behavior — its results
/// are synthetic and clearly tagged `provider: .fixture` (never
/// `.roomplan`) so a fixture-sourced `RoomDraft` can never be mistaken for
/// one produced by an actual RoomPlan session. See
/// `docs/spatial/roomplan/ROOMPLAN_TESTING_STRATEGY.md`'s Tier A/B/C split:
/// tests built on this provider are Tier A/B (platform-independent /
/// Xcode-compiled), never a substitute for Tier C hardware verification.
public final class FixtureCaptureProvider: SpatialCaptureProvider, @unchecked Sendable {
    public let providerIdentity: SpatialCaptureSourceProvider = .fixture

    private let supportState: SpatialCaptureSupportState
    private let captureOutcome: CaptureOutcome
    private let fixturePayload: FixtureCapturePayload

    public enum CaptureOutcome: Sendable {
        case success
        case failure(SpatialCaptureProviderError)
    }

    public init(
        supportState: SpatialCaptureSupportState = .supported,
        captureOutcome: CaptureOutcome = .success,
        fixturePayload: FixtureCapturePayload = .rectangularRoom
    ) {
        self.supportState = supportState
        self.captureOutcome = captureOutcome
        self.fixturePayload = fixturePayload
    }

    public func checkSupport() async -> SpatialCaptureSupportState {
        supportState
    }

    public func startCapture() async throws(SpatialCaptureProviderError) -> SpatialCaptureRawResult {
        switch captureOutcome {
        case .success:
            return SpatialCaptureRawResult(provider: .fixture, payload: fixturePayload)
        case .failure(let error):
            throw error
        }
    }
}

/// A synthetic, provider-neutral capture payload shape used by
/// `FixtureCaptureProvider` and `FixtureCaptureAdapter` together. Not a
/// stand-in for `CapturedRoom` — it exists purely so Core-layer tests can
/// exercise the provider → adapter → `RoomDraft` pipeline without any
/// dependency on RoomPlan/ARKit types, which Core must never import.
public struct FixtureCapturePayload: Sendable, Equatable {
    public struct FixtureWall: Sendable, Equatable {
        public let sourceIdentifier: String
        public let start: RoomLocalPoint
        public let end: RoomLocalPoint
        public let height: Double?

        public init(sourceIdentifier: String, start: RoomLocalPoint, end: RoomLocalPoint, height: Double? = nil) {
            self.sourceIdentifier = sourceIdentifier
            self.start = start
            self.end = end
            self.height = height
        }
    }

    public let captureIdentifier: String
    public let walls: [FixtureWall]

    public init(captureIdentifier: String, walls: [FixtureWall]) {
        self.captureIdentifier = captureIdentifier
        self.walls = walls
    }

    /// A simple 4m x 3m rectangular room, four walls, already expressed
    /// directly in the canonical Renovex room-local frame (no conversion
    /// needed — this fixture is provider-neutral, not RoomPlan-shaped).
    public static let rectangularRoom = FixtureCapturePayload(
        captureIdentifier: "fixture-capture-rectangular-room",
        walls: [
            FixtureWall(sourceIdentifier: "wall-south", start: .init(x: 0, y: 0, z: 0), end: .init(x: 4, y: 0, z: 0), height: 2.4),
            FixtureWall(sourceIdentifier: "wall-east", start: .init(x: 4, y: 0, z: 0), end: .init(x: 4, y: 0, z: 3), height: 2.4),
            FixtureWall(sourceIdentifier: "wall-north", start: .init(x: 4, y: 0, z: 3), end: .init(x: 0, y: 0, z: 3), height: 2.4),
            FixtureWall(sourceIdentifier: "wall-west", start: .init(x: 0, y: 0, z: 3), end: .init(x: 0, y: 0, z: 0), height: 2.4),
        ]
    )
}

/// The `SpatialCaptureAdapter` conformer for `FixtureCapturePayload`.
/// Exists so the fixture provider's result can flow through the exact same
/// provider → adapter → `RoomDraft` pipeline shape a real
/// `RoomPlanCaptureAdapter` would use, proving the abstraction boundary
/// (design spec §8.7) without any Apple-specific dependency.
public struct FixtureCaptureAdapter: SpatialCaptureAdapter {
    public let providerIdentity: SpatialCaptureSourceProvider = .fixture

    public init() {}

    public func makeRoomDraft(from rawResult: SpatialCaptureRawResult) throws(SpatialCaptureAdapterError) -> RoomDraft {
        guard rawResult.provider == .fixture, let payload = rawResult.payload as? FixtureCapturePayload else {
            throw .unexpectedPayload
        }
        let walls = payload.walls.map { fixtureWall in
            RoomDraftWall(
                id: WallID(UUID().uuidString),
                start: fixtureWall.start,
                end: fixtureWall.end,
                height: fixtureWall.height,
                provenance: SourceProvenance(
                    provider: .fixture,
                    sourceElementIdentifier: fixtureWall.sourceIdentifier,
                    sourceCaptureIdentifier: payload.captureIdentifier
                )
            )
        }
        return RoomDraft(walls: walls, sourceCaptureIdentifier: payload.captureIdentifier, sourceProvider: .fixture)
    }
}
