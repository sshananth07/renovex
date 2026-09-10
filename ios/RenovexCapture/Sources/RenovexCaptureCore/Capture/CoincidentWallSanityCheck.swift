import Foundation

/// A bounded, deterministic topology sanity pass for obviously coincident/
/// redundant wall surfaces at the adapter boundary (design spec §8.10,
/// plan RP2 deliverable: "A bounded, deterministic topology sanity pass for
/// obviously coincident/redundant surfaces at the adapter boundary only —
/// explicitly not a `RoomShell` reconstruction system ... and explicitly
/// not distance-only wall merging").
///
/// This intentionally does NOT merge walls by distance alone (the frozen
/// Android provider's `RoomShellDraftSimplifier` required provenance,
/// matching supporting lines, and configured tolerance before any collapse
/// — §8.4.7). Here, RP2's much narrower job is only to flag wall pairs
/// whose endpoints coincide within a small epsilon on BOTH ends (in either
/// direction) — the signature of RoomPlan reporting the literal same
/// physical surface twice, e.g. from `didChange` racing `didAdd` in a way
/// `LiveElementTracker` did not fully dedupe (source-identifier churn), not
/// two genuinely distinct nearby parallel walls. Detection only; RP2 does
/// not decide what to do about a flagged pair (that judgment belongs to a
/// contractor in RP4, or a later deterministic collapse rule with real
/// device evidence, matching the frozen provider's own precedent of never
/// collapsing on geometry alone).
public enum CoincidentWallSanityCheck {
    /// Default coincidence tolerance: 2cm. Chosen as a bounded, explicit,
    /// tunable constant — not a hidden magic number — matching the frozen
    /// provider's own precedent of treating every geometric tolerance as
    /// scanner policy, never architectural truth (design spec §8.4's
    /// `ScannerParameters` precedent).
    public static let defaultEpsilonMeters: Double = 0.02

    /// Returns index pairs (i, j) with i < j into `walls` whose endpoints
    /// coincide within `epsilonMeters`, checked in both possible pairings
    /// (start-start/end-end, and start-end/end-start, since two reports of
    /// the same physical wall are not guaranteed to preserve a consistent
    /// start/end ordering).
    public static func findCoincidentPairs(
        in walls: [RoomDraftWall],
        epsilonMeters: Double = defaultEpsilonMeters
    ) -> [(Int, Int)] {
        guard walls.count > 1 else { return [] }
        var pairs: [(Int, Int)] = []
        for i in 0..<(walls.count - 1) {
            for j in (i + 1)..<walls.count {
                if areCoincident(walls[i], walls[j], epsilonMeters: epsilonMeters) {
                    pairs.append((i, j))
                }
            }
        }
        return pairs
    }

    private static func areCoincident(_ a: RoomDraftWall, _ b: RoomDraftWall, epsilonMeters: Double) -> Bool {
        let sameOrder = distance(a.start, b.start) <= epsilonMeters && distance(a.end, b.end) <= epsilonMeters
        let reversedOrder = distance(a.start, b.end) <= epsilonMeters && distance(a.end, b.start) <= epsilonMeters
        return sameOrder || reversedOrder
    }

    private static func distance(_ a: RoomLocalPoint, _ b: RoomLocalPoint) -> Double {
        let dx = a.x - b.x
        let dy = a.y - b.y
        let dz = a.z - b.z
        return (dx * dx + dy * dy + dz * dz).squareRoot()
    }
}
