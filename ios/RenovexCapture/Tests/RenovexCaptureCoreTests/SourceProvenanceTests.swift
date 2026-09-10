import XCTest
@testable import RenovexCaptureCore

/// RP1 TDD requirement: "source provenance." Design spec §8.9: RoomPlan
/// identifiers are provenance only, never permanent Renovex domain
/// identity — these tests pin that distinction at the type level.
final class SourceProvenanceTests: XCTestCase {
    func test_provenance_capturesProviderAndSourceIdentifier() {
        let provenance = SourceProvenance(
            provider: .roomplan,
            sourceElementIdentifier: "ABCD-1234",
            sourceCaptureIdentifier: "session-1"
        )
        XCTAssertEqual(provenance.provider, .roomplan)
        XCTAssertEqual(provenance.sourceElementIdentifier, "ABCD-1234")
        XCTAssertEqual(provenance.sourceCaptureIdentifier, "session-1")
    }

    func test_sourceCaptureIdentifier_isOptional() {
        let provenance = SourceProvenance(provider: .fixture, sourceElementIdentifier: "wall-1")
        XCTAssertNil(provenance.sourceCaptureIdentifier)
    }

    func test_stableIDs_areDistinctTypes_notInterchangeableWithRawStrings() {
        // This is a compile-time proof more than a runtime one: WallID and
        // OpeningID both wrap String but are NOT the same type, so passing
        // one where the other is expected would fail to compile. The
        // runtime assertions below confirm equality/hashing still work
        // correctly for each distinct wrapper type.
        let wallID = WallID("wall-1")
        let openingID = OpeningID("wall-1")
        XCTAssertEqual(wallID.rawValue, openingID.rawValue)
        XCTAssertEqual(wallID, WallID("wall-1"))
        XCTAssertNotEqual(wallID.rawValue, "wall-2")
    }

    func test_stableIDs_areHashable_forUseAsDictionaryKeys() {
        var seen: Set<WallID> = []
        seen.insert(WallID("a"))
        seen.insert(WallID("b"))
        seen.insert(WallID("a")) // duplicate
        XCTAssertEqual(seen.count, 2)
    }

    func test_sourceProvider_fixtureCase_isDistinctFromRealProviders() {
        // Guards against a fixture ever being silently treated as if it
        // were real RoomPlan (or a future Android) output.
        XCTAssertNotEqual(SpatialCaptureSourceProvider.fixture, .roomplan)
        XCTAssertNotEqual(SpatialCaptureSourceProvider.fixture, .arcore)
    }
}
