import Foundation

/// Maintains the CURRENT-STATE representation of live capture-session
/// elements keyed by capture-provider source identifier (design spec
/// §8.10). This is the concrete, testable implementation of the rule that
/// live RoomPlan callbacks are current state, not append-only evidence —
/// the exact failure mode the frozen Android/ARCore provider had (raw plane
/// observations accumulating into duplicate walls, §8.4.1) and that RP2
/// must not reintroduce for RoomPlan.
///
/// Semantics:
/// ```text
/// didAdd (new sourceIdentifier)
///     → insert current representation
///
/// didChange / didUpdate (existing sourceIdentifier)
///     → REPLACE the existing current representation — never append a
///       second entry for the same sourceIdentifier
///
/// didRemove (existing sourceIdentifier)
///     → remove the current representation
/// ```
///
/// Deliberately provider-neutral: `LiveElementTracker` never imports
/// RoomPlan and knows nothing about `CapturedRoom` — it operates purely on
/// `LiveElementSnapshot` values, which is what makes it possible to prove
/// this dedup/replace/remove behavior deterministically on Windows (RP2
/// amendment §10/§11), independent of whatever Apple API shape turns out to
/// be correct once Xcode verification happens. `RenovexCaptureRoomPlan`'s
/// job is only to translate RoomPlan's `didAdd`/`didChange`/`didUpdate`/
/// `didRemove` callbacks into calls on this type.
///
/// NOT a `RoomShell`/`WallCandidate` reconstruction system (design spec
/// §8.10's explicit prohibition): this type does no geometric
/// clustering, intersection resolution, or shell-building — it is a plain
/// keyed current-state map. RoomPlan itself performs reconstruction; this
/// type only prevents Renovex from duplicating what RoomPlan already
/// reconciled.
public final class LiveElementTracker: @unchecked Sendable {
    private var current: [String: LiveElementSnapshot] = [:]
    private var insertionOrder: [String] = []

    public init() {}

    /// `didAdd`/`didChange`/`didUpdate`: insert-or-replace the current
    /// representation for `snapshot.sourceIdentifier`. A second call with
    /// the same source identifier replaces the prior snapshot in place —
    /// it never creates a second entry.
    public func upsert(_ snapshot: LiveElementSnapshot) {
        if current[snapshot.sourceIdentifier] == nil {
            insertionOrder.append(snapshot.sourceIdentifier)
        }
        current[snapshot.sourceIdentifier] = snapshot
    }

    /// `didRemove`: removes the current representation for
    /// `sourceIdentifier`, if any. Removing an identifier that was never
    /// added, or was already removed, is a no-op — not an error, since a
    /// capture session's callback ordering is not something this type
    /// controls or should assume is strictly disciplined.
    public func remove(sourceIdentifier: String) {
        current.removeValue(forKey: sourceIdentifier)
        insertionOrder.removeAll { $0 == sourceIdentifier }
    }

    /// The current set of live elements, in first-seen order (stable
    /// ordering makes downstream normalization/test assertions
    /// deterministic rather than dependent on `Dictionary`'s unordered
    /// iteration).
    public func snapshot() -> [LiveElementSnapshot] {
        insertionOrder.compactMap { current[$0] }
    }

    /// The current snapshot for one source identifier, if it exists.
    public func snapshot(sourceIdentifier: String) -> LiveElementSnapshot? {
        current[sourceIdentifier]
    }

    public var count: Int { current.count }

    /// Clears all tracked elements — used when starting a new capture
    /// session so a stale tracker from a prior session can't leak state
    /// into a new one (deliberately NOT persistence; RP3 owns durable
    /// multi-session history).
    public func reset() {
        current.removeAll()
        insertionOrder.removeAll()
    }
}
