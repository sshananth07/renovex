import XCTest
@testable import RenovexCaptureCore

/// Plan §RP3.5/§RP4B0: proves Swift's `ChecksumUtility.sha256Hex` produces
/// EXACTLY the same lowercase hex digest as Go's `sha256.Sum256` +
/// `hex.EncodeToString` (the algorithm
/// `backend/internal/platform/composition/spatialartifactstoreadapter.go`
/// and `hashArtifactToken` both use) for identical bytes — golden vectors
/// computed via a one-off `go run` using the exact same
/// `crypto/sha256`/`encoding/hex` calls the backend uses in production.
final class ChecksumUtilityTests: XCTestCase {
    func test_sha256Hex_emptyInput_matchesGoGoldenVector() {
        let digest = ChecksumUtility.sha256Hex(Data())
        XCTAssertEqual(digest, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
    }

    func test_sha256Hex_simpleString_matchesGoGoldenVector() {
        let digest = ChecksumUtility.sha256Hex("hello world".data(using: .utf8)!)
        XCTAssertEqual(digest, "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
    }

    /// A representative roomdraft_json-shaped payload — proves the parity
    /// holds for JSON bytes specifically, not just arbitrary strings.
    func test_sha256Hex_roomDraftShapedJSON_matchesGoGoldenVector() {
        let json = #"{"id":"draft_1","walls":[],"openings":[],"objects":[]}"#
        let digest = ChecksumUtility.sha256Hex(json.data(using: .utf8)!)
        XCTAssertEqual(digest, "d777fda4648c4f134247f76f41bbeb3d49303db670df2f782d05bfd45aa00e0f")
    }

    func test_sha256Hex_isDeterministic() {
        let data = "deterministic".data(using: .utf8)!
        XCTAssertEqual(ChecksumUtility.sha256Hex(data), ChecksumUtility.sha256Hex(data))
    }

    func test_sha256Hex_differentInputsProduceDifferentDigests() {
        let a = ChecksumUtility.sha256Hex("a".data(using: .utf8)!)
        let b = ChecksumUtility.sha256Hex("b".data(using: .utf8)!)
        XCTAssertNotEqual(a, b)
    }

    func test_sha256Hex_isAlwaysSixtyFourLowercaseHexCharacters() {
        let digest = ChecksumUtility.sha256Hex("anything".data(using: .utf8)!)
        XCTAssertEqual(digest.count, 64)
        XCTAssertEqual(digest, digest.lowercased())
        XCTAssertTrue(digest.allSatisfy { $0.isHexDigit })
    }
}
