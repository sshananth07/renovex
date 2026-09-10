import Foundation

/// Errors a `RoomDraftRepository` conformer may surface.
public enum RoomDraftRepositoryError: Error, Equatable, Sendable {
    case notFound
    case underlyingStorageFailure(reason: String)
}

/// Persists `RoomDraft`s keyed by the capture run that produced them (plan
/// §RP3 §5). One capture run has at most one `RoomDraft` — reopening a
/// persisted draft must retain the same `WallID`/`OpeningID`/`ObjectID`
/// identities (plan §RP3 §5: "Do NOT regenerate stable Renovex IDs every
/// time the draft is reopened").
///
/// Lives in Core, swappable/testable the same way `SpatialCaptureRepository`
/// is.
public protocol RoomDraftRepository: Sendable {
    /// Persists `draft` for `captureID`. Throws if `captureID` already has a
    /// persisted draft — use `update` for edits, matching the backend
    /// `spatial.Service.PersistRoomDraft`/`UpdateRoomDraft` split so the two
    /// layers enforce the same one-draft-per-capture invariant.
    func create(_ draft: RoomDraft, forCapture captureID: String) async throws(RoomDraftRepositoryError)

    /// Replaces the persisted draft for `captureID` with `draft` (a
    /// contractor edit — RP4 scope wires the actual editor; RP3 only needs
    /// the persistence primitive to exist and be correct).
    func update(_ draft: RoomDraft, forCapture captureID: String) async throws(RoomDraftRepositoryError)

    /// Returns the persisted draft for `captureID`, or `.notFound` if this
    /// capture has none yet. This is the "Continue Review loads the SAME
    /// persisted draft" lookup (plan §RP3 §9) — callers use this instead of
    /// ever re-normalizing or creating a second draft for one capture.
    func find(forCapture captureID: String) async throws(RoomDraftRepositoryError) -> RoomDraft
}

/// Errors `RoomDraftRepository.resetToBaseline(forCapture:)` may surface —
/// the union of `RoomDraftRepositoryError` (persistence failure) and
/// `RoomDraftResetError` (no baseline to reset to).
public enum RoomDraftResetPersistenceError: Error, Equatable, Sendable {
    case notFound
    case noBaseline
    case underlyingStorageFailure(reason: String)
}

extension RoomDraftRepository {
    /// Loads `captureID`'s draft, resets it to its `originalBaseline`
    /// (design spec §8.14/§8.25 "Reset to Scan", RP4A — see
    /// `RoomDraft.resettingToBaseline()`), persists the result via
    /// `update`, and returns it. This is the repository-level primitive
    /// only — RP4A does not wire a UI trigger or backend-sync
    /// orchestration for it (that is later RP4 scope), matching
    /// `spatial.Service.ResetRoomDraftToBaseline`'s server-side equivalent
    /// so both platforms share the exact same reset semantics.
    public func resetToBaseline(forCapture captureID: String) async throws(RoomDraftResetPersistenceError) -> RoomDraft {
        let existing: RoomDraft
        do {
            existing = try await find(forCapture: captureID)
        } catch {
            switch error {
            case .notFound: throw RoomDraftResetPersistenceError.notFound
            case .underlyingStorageFailure(let reason): throw RoomDraftResetPersistenceError.underlyingStorageFailure(reason: reason)
            }
        }

        let reset: RoomDraft
        do {
            reset = try existing.resettingToBaseline()
        } catch {
            switch error {
            case .noBaseline: throw RoomDraftResetPersistenceError.noBaseline
            }
        }

        do {
            try await update(reset, forCapture: captureID)
        } catch {
            switch error {
            case .notFound: throw RoomDraftResetPersistenceError.notFound
            case .underlyingStorageFailure(let reason): throw RoomDraftResetPersistenceError.underlyingStorageFailure(reason: reason)
            }
        }
        return reset
    }
}
