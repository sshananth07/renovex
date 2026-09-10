import Foundation

/// Errors a `SpatialCaptureAdapter` conformer may surface while converting
/// a provider's raw result into a `RoomDraft`.
public enum SpatialCaptureAdapterError: Error, Equatable, Sendable {
    /// The raw result's `payload` was not the type this adapter expects —
    /// e.g. a `RoomPlanCaptureAdapter` received a `SpatialCaptureRawResult`
    /// whose `provider` was not `.roomplan`, or whose payload could not be
    /// downcast to the adapter's expected provider-specific type.
    case unexpectedPayload
    case malformedResult(reason: String)
}

/// The provider-neutral adapter contract: convert one provider's raw
/// capture result into the canonical Renovex `RoomDraft`. Concrete
/// conformers (e.g. `RoomPlanCaptureAdapter`) own all knowledge of their
/// provider's native types; nothing outside the adapter may reinterpret
/// provider-specific coordinates or identifiers (design spec §8.8, §8.9).
///
/// This protocol lives in Core (not RoomPlan) so app-level code can depend
/// on "some adapter for this raw result" without importing RoomPlan
/// directly — the same boundary-neutrality `SpatialCaptureProvider` gives
/// on the capture side.
public protocol SpatialCaptureAdapter: Sendable {
    /// Which provider this adapter converts results from.
    var providerIdentity: SpatialCaptureSourceProvider { get }

    /// Converts `rawResult` into a normalized `RoomDraft` with the
    /// canonical room-local coordinate frame and Renovex-owned stable IDs
    /// applied. Throws `SpatialCaptureAdapterError.unexpectedPayload` if
    /// `rawResult.provider != providerIdentity` or the payload cannot be
    /// downcast to this adapter's expected type.
    func makeRoomDraft(from rawResult: SpatialCaptureRawResult) throws(SpatialCaptureAdapterError) -> RoomDraft
}
