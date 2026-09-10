import XCTest
@testable import RenovexCaptureCore

/// RP2 TDD requirement: "live-update identity handling (no duplicate
/// geometry across repeated `didChange` for one identifier)." Design spec
/// §8.10 — the exact failure mode this must prevent is the frozen Android
/// provider's duplicate-wall accumulation problem, now for RoomPlan's
/// `didAdd`/`didChange`/`didUpdate`/`didRemove` callback sequence.
final class LiveElementTrackerTests: XCTestCase {
    private func makeSnapshot(id: String, x: Double = 0) -> LiveElementSnapshot {
        LiveElementSnapshot(
            sourceIdentifier: id,
            kind: .wall,
            transform: RoomLocalTransform(position: RoomLocalPoint(x: x, y: 0, z: 0))
        )
    }

    func test_didAdd_insertsNewElement() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1"))
        XCTAssertEqual(tracker.count, 1)
    }

    func test_repeatedUpdatesOfSameSourceIdentifier_produceExactlyOneCurrentElement() {
        // The core RP2 requirement: same RoomPlan source identifier updated
        // repeatedly must never accumulate into multiple current elements.
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1", x: 0))
        tracker.upsert(makeSnapshot(id: "wall-1", x: 0.1))
        tracker.upsert(makeSnapshot(id: "wall-1", x: 0.2))
        tracker.upsert(makeSnapshot(id: "wall-1", x: 0.3))

        XCTAssertEqual(tracker.count, 1, "repeated didChange for the same identifier must never create a second element")
        XCTAssertEqual(tracker.snapshot(sourceIdentifier: "wall-1")?.transform.position.x, 0.3)
    }

    func test_changedGeometry_replacesPriorCurrentGeometry() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1", x: 1.0))
        let before = tracker.snapshot(sourceIdentifier: "wall-1")
        XCTAssertEqual(before?.transform.position.x, 1.0)

        tracker.upsert(makeSnapshot(id: "wall-1", x: 2.0))
        let after = tracker.snapshot(sourceIdentifier: "wall-1")
        XCTAssertEqual(after?.transform.position.x, 2.0, "the current representation must be the latest geometry, not the first")
    }

    func test_removedSourceIdentifier_disappears() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1"))
        tracker.remove(sourceIdentifier: "wall-1")

        XCTAssertEqual(tracker.count, 0)
        XCTAssertNil(tracker.snapshot(sourceIdentifier: "wall-1"))
    }

    func test_removingUnknownIdentifier_isANoOp_notAnError() {
        let tracker = LiveElementTracker()
        tracker.remove(sourceIdentifier: "never-added")
        XCTAssertEqual(tracker.count, 0)
    }

    func test_twoDistinctSourceIdentifiers_remainDistinct() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1", x: 0))
        tracker.upsert(makeSnapshot(id: "wall-2", x: 5))

        XCTAssertEqual(tracker.count, 2)
        XCTAssertEqual(tracker.snapshot(sourceIdentifier: "wall-1")?.transform.position.x, 0)
        XCTAssertEqual(tracker.snapshot(sourceIdentifier: "wall-2")?.transform.position.x, 5)
    }

    func test_snapshot_preservesFirstSeenOrder() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-c"))
        tracker.upsert(makeSnapshot(id: "wall-a"))
        tracker.upsert(makeSnapshot(id: "wall-b"))
        // A later update to an already-seen identifier must not move its
        // position in iteration order — order reflects first-seen, not
        // last-updated, so downstream consumers get deterministic output
        // across repeated didChange bursts.
        tracker.upsert(makeSnapshot(id: "wall-a", x: 99))

        let ids = tracker.snapshot().map(\.sourceIdentifier)
        XCTAssertEqual(ids, ["wall-c", "wall-a", "wall-b"])
    }

    func test_removeThenReAdd_treatsAsNewInsertion() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1"))
        tracker.remove(sourceIdentifier: "wall-1")
        tracker.upsert(makeSnapshot(id: "wall-1", x: 42))

        XCTAssertEqual(tracker.count, 1)
        XCTAssertEqual(tracker.snapshot(sourceIdentifier: "wall-1")?.transform.position.x, 42)
    }

    func test_reset_clearsAllTrackedElements() {
        let tracker = LiveElementTracker()
        tracker.upsert(makeSnapshot(id: "wall-1"))
        tracker.upsert(makeSnapshot(id: "wall-2"))
        tracker.reset()

        XCTAssertEqual(tracker.count, 0)
        XCTAssertTrue(tracker.snapshot().isEmpty)
    }

    func test_differentElementKinds_trackIndependentlyByIdentifier() {
        // A wall and an object could in principle share the same
        // provider-issued identifier value space (unlikely for RoomPlan in
        // practice, but the tracker's contract should not silently merge
        // them if it happens) — the source identifier is the sole key
        // regardless of kind, so this test documents that behavior
        // explicitly rather than leaving it implicit.
        let tracker = LiveElementTracker()
        let wall = LiveElementSnapshot(sourceIdentifier: "shared-id", kind: .wall, transform: .init(position: .init(x: 0, y: 0, z: 0)))
        let object = LiveElementSnapshot(sourceIdentifier: "shared-id", kind: .object, transform: .init(position: .init(x: 1, y: 0, z: 0)))

        tracker.upsert(wall)
        tracker.upsert(object)

        XCTAssertEqual(tracker.count, 1, "the tracker keys purely by source identifier; a colliding ID from two kinds is the caller's responsibility to avoid")
        XCTAssertEqual(tracker.snapshot(sourceIdentifier: "shared-id")?.kind, .object)
    }
}
