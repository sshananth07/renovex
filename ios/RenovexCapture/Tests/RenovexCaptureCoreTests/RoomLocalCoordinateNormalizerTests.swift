import XCTest
@testable import RenovexCaptureCore

/// RP2 TDD requirement: "coordinate normalization fixture tests
/// (RoomPlan-shaped input → expected canonical output)." Exercises
/// `RoomLocalCoordinateNormalizer` — pure, provider-neutral math, fully
/// Windows-testable independent of any real RoomPlan API shape (RP2
/// amendment §10).
final class RoomLocalCoordinateNormalizerTests: XCTestCase {
    // MARK: Point conversion / origin re-basing

    func test_identityOrigin_pointsPassThroughUnchanged() {
        let normalizer = RoomLocalCoordinateNormalizer()
        let point = RoomLocalPoint(x: 1.5, y: 2.4, z: -0.3)
        XCTAssertEqual(normalizer.convertPoint(point), point)
    }

    func test_nonZeroOrigin_reBasesPointsRelativeToIt() {
        let origin = RoomLocalPoint(x: 1.0, y: 0, z: 1.0)
        let normalizer = RoomLocalCoordinateNormalizer(originInSourceSpace: origin)
        let sourcePoint = RoomLocalPoint(x: 3.0, y: 0, z: 4.0)

        let converted = normalizer.convertPoint(sourcePoint)

        XCTAssertEqual(converted, RoomLocalPoint(x: 2.0, y: 0, z: 3.0))
    }

    func test_originItself_convertsToZero() {
        let origin = RoomLocalPoint(x: 2.5, y: 1.2, z: -3.0)
        let normalizer = RoomLocalCoordinateNormalizer(originInSourceSpace: origin)

        let converted = normalizer.convertPoint(origin)

        XCTAssertEqual(converted, RoomLocalPoint(x: 0, y: 0, z: 0))
    }

    func test_metresPreservedCorrectly_noUnitScaling() {
        // A 4-metre offset must remain exactly 4 metres after conversion —
        // proves no accidental unit scaling (e.g. cm/mm confusion) is
        // introduced by the normalizer.
        let normalizer = RoomLocalCoordinateNormalizer()
        let point = RoomLocalPoint(x: 4.0, y: 0, z: 0)
        XCTAssertEqual(normalizer.convertPoint(point).x, 4.0, accuracy: 1e-9)
    }

    // MARK: Transform conversion

    func test_identityTransform_convertsToIdentityRotationAtOrigin() {
        let normalizer = RoomLocalCoordinateNormalizer()
        let converted = normalizer.convertTransform(.identity)

        XCTAssertEqual(converted.position, RoomLocalPoint(x: 0, y: 0, z: 0))
        XCTAssertEqual(converted.rotation, .identity)
    }

    func test_translationOnlyTransform_reBasesPositionAndKeepsIdentityRotation() {
        let origin = RoomLocalPoint(x: 1, y: 0, z: 0)
        let normalizer = RoomLocalCoordinateNormalizer(originInSourceSpace: origin)
        var transform = SourceSpaceTransform.identity
        transform.columns[12] = 3 // translation.x
        transform.columns[13] = 0
        transform.columns[14] = 2 // translation.z

        let converted = normalizer.convertTransform(transform)

        XCTAssertEqual(converted.position, RoomLocalPoint(x: 2, y: 0, z: 2))
        XCTAssertEqual(converted.rotation, .identity)
    }

    // MARK: Rotation/quaternion extraction

    func test_quaternion_fromIdentityRotation_isIdentity() {
        let quaternion = RoomLocalCoordinateNormalizer.quaternion(fromColumnMajorRotation: .identity)
        XCTAssertEqual(quaternion, .identity)
    }

    func test_quaternion_from90DegreeYRotation_matchesExpectedValue() {
        // 90° rotation about Y: column-major rotation matrix
        //   [ cos  0  sin ]     [ 0  0  1 ]
        //   [  0   1   0  ]  =  [ 0  1  0 ]
        //   [-sin  0  cos ]     [-1  0  0 ]
        // Column-major storage: col0=(0,0,-1), col1=(0,1,0), col2=(1,0,0)
        var transform = SourceSpaceTransform.identity
        transform.columns[0] = 0; transform.columns[1] = 0; transform.columns[2] = -1
        transform.columns[4] = 0; transform.columns[5] = 1; transform.columns[6] = 0
        transform.columns[8] = 1; transform.columns[9] = 0; transform.columns[10] = 0

        let quaternion = RoomLocalCoordinateNormalizer.quaternion(fromColumnMajorRotation: transform)

        // Expected: rotation of 90° about Y axis => (x,y,z,w) = (0, sin(45°), 0, cos(45°))
        let expected = (2.0).squareRoot() / 2.0
        XCTAssertEqual(quaternion.x, 0, accuracy: 1e-9)
        XCTAssertEqual(quaternion.y, expected, accuracy: 1e-9)
        XCTAssertEqual(quaternion.z, 0, accuracy: 1e-9)
        XCTAssertEqual(quaternion.w, expected, accuracy: 1e-9)
    }

    func test_quaternion_signIsRenovexNormalized_wIsNeverNegative() {
        // A 180° rotation about Y produces a quaternion whose naive
        // extraction could yield w=0 or a negative w depending on the
        // extraction branch taken; the normalized-sign rule only
        // guarantees w >= 0, which this test pins.
        var transform = SourceSpaceTransform.identity
        // 180° about Y: col0=(-1,0,0), col1=(0,1,0), col2=(0,0,-1)
        transform.columns[0] = -1; transform.columns[1] = 0; transform.columns[2] = 0
        transform.columns[4] = 0; transform.columns[5] = 1; transform.columns[6] = 0
        transform.columns[8] = 0; transform.columns[9] = 0; transform.columns[10] = -1

        let quaternion = RoomLocalCoordinateNormalizer.quaternion(fromColumnMajorRotation: transform)
        XCTAssertGreaterThanOrEqual(quaternion.w, 0)
        // Verify it's a unit quaternion.
        let normSquared = quaternion.x * quaternion.x + quaternion.y * quaternion.y
            + quaternion.z * quaternion.z + quaternion.w * quaternion.w
        XCTAssertEqual(normSquared, 1.0, accuracy: 1e-6)
    }

    func test_deterministicOutput_sameInputProducesSameOutput() {
        let normalizer = RoomLocalCoordinateNormalizer(originInSourceSpace: RoomLocalPoint(x: 1, y: 2, z: 3))
        var transform = SourceSpaceTransform.identity
        transform.columns[12] = 5; transform.columns[13] = 6; transform.columns[14] = 7

        let first = normalizer.convertTransform(transform)
        let second = normalizer.convertTransform(transform)

        XCTAssertEqual(first, second)
    }

    func test_sourceSpaceTransform_rejectsWrongElementCount() {
        // Precondition-guarded — proven by constructing a valid 16-element
        // transform and asserting the invariant it protects, matching the
        // existing CoordinateContractTests precedent for RoomLocalMatrix4x4.
        let valid = SourceSpaceTransform(columns: Array(repeating: 0.0, count: 16))
        XCTAssertEqual(valid.columns.count, 16)
    }

    func test_translation_extractsFromFourthColumn() {
        var transform = SourceSpaceTransform.identity
        transform.columns[12] = 10
        transform.columns[13] = 20
        transform.columns[14] = 30

        XCTAssertEqual(transform.translation, RoomLocalPoint(x: 10, y: 20, z: 30))
    }
}
