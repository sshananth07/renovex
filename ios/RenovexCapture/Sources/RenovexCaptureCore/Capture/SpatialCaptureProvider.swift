import Foundation

/// A provider-neutral, opaque handle to whatever a `SpatialCaptureProvider`
/// produced. Deliberately opaque here: `SpatialCaptureProvider`'s protocol
/// surface must not carry any provider-specific type (design spec §8.7 —
/// "`SpatialCaptureProvider` itself must not carry any Apple-specific types
/// in its signature"). Concrete providers (e.g. `RoomPlanCaptureProvider`)
/// return a `SpatialCaptureRawResult` wrapping their own provider-specific
/// payload; only that provider's own adapter (e.g.
/// `RoomPlanCaptureAdapter`, in the RenovexCaptureRoomPlan target) knows how
/// to unwrap `payload` back into its native type. `RenovexCaptureCore` never
/// inspects `payload`'s contents.
public struct SpatialCaptureRawResult: Sendable {
    /// Which provider produced this result — used by the caller to route
    /// the result to the matching adapter; not interpreted by Core itself.
    public let provider: SpatialCaptureSourceProvider
    /// Opaque provider-specific payload. `Sendable` box around `Any` so the
    /// protocol can remain provider-neutral; the concrete adapter for
    /// `provider` is responsible for downcasting this to its expected type.
    public let payload: any Sendable

    public init(provider: SpatialCaptureSourceProvider, payload: any Sendable) {
        self.provider = provider
        self.payload = payload
    }
}

/// Errors a `SpatialCaptureProvider` conformer may surface. Kept small and
/// provider-neutral; a concrete provider maps its own SDK errors into one of
/// these rather than leaking a provider-specific error type through the
/// protocol boundary.
public enum SpatialCaptureProviderError: Error, Equatable, Sendable {
    case unsupported(reason: String)
    case sessionFailed(reason: String)
    case cancelled
}

/// The provider-neutral capture-source contract (design spec §8.7).
///
/// ```text
/// SpatialCaptureProvider              (this protocol)
///     ├── RoomPlanCaptureProvider     (current — iOS, Apple RoomPlan)
///     └── AndroidCaptureProvider      (future, if the frozen Android
///                                       provider is ever revived)
/// ```
///
/// A conformer obtains a capture result and hands it off — normalization
/// into `RoomDraft` is NOT this protocol's job; that belongs to a
/// provider-specific adapter (e.g. `RoomPlanCaptureAdapter`) consuming the
/// `SpatialCaptureRawResult` this protocol returns. Keeping normalization
/// out of the provider protocol is what lets `FixtureCaptureProvider`
/// substitute for `RoomPlanCaptureProvider` in tests without needing to
/// fake RoomPlan's own types.
public protocol SpatialCaptureProvider: AnyObject, Sendable {
    /// Which provider this conformer represents.
    var providerIdentity: SpatialCaptureSourceProvider { get }

    /// Whether this provider can perform a live capture on the current
    /// device. Real providers must report this from an actual capability
    /// check (e.g. RoomPlan's `RoomCaptureSession.isSupported`) — never
    /// hardcode `.supported` in production code (RoomPlan amendment §6).
    func checkSupport() async -> SpatialCaptureSupportState

    /// Runs one capture session end-to-end and returns its raw,
    /// provider-specific result on success. RP1 does not require this to
    /// drive a live RoomPlan session (that is RP2's scope,
    /// `RoomCaptureView`-based per design spec §8.7.1) — the protocol
    /// method exists so the abstraction boundary is real and swappable now,
    /// proven by `FixtureCaptureProvider` satisfying it deterministically.
    func startCapture() async throws(SpatialCaptureProviderError) -> SpatialCaptureRawResult
}
