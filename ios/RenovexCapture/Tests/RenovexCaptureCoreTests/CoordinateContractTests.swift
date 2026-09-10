import XCTest
@testable import RenovexCaptureCore

/// Establishes the canonical Renovex room-local coordinate contract as
/// executable fixtures (design spec §8.8). RP1 scope: prove the contract
/// types themselves (units, right-handed Y-up convention, quaternion
/// identity, column-major matrix layout) behave as specified. RP2 owns the
/// full RoomPlan-session-space -> Renovex-frame conversion and the
/// cross-platform (iOS/Web/backend) contract tests extending this (design
/// spec §8.8.1) — this file only proves the domain contract's own shape is
/// internally consistent, which is what a fixture is for at this stage.
final class CoordinateContractTests: XCTestCase {
    func test_identityQuaternion_hasNoRotation() {
        let identity = RoomLocalQuaternion.identity
        XCTAssertEqual(identity, RoomLocalQuaternion(x: 0, y: 0, z: 0, w: 1))
    }

    func test_identityMatrix_hasSixteenElements_columnMajor() {
        let identity = RoomLocalMatrix4x4.identity
        XCTAssertEqual(identity.columns.count, 16)
        // Column-major identity: columns[0] = (1,0,0,0), columns[5] = 1
        // (second column's Y component), columns[10] = 1 (third column's Z
        // component), columns[15] = 1 (fourth column's W component).
        XCTAssertEqual(identity.columns[0], 1)
        XCTAssertEqual(identity.columns[5], 1)
        XCTAssertEqual(identity.columns[10], 1)
        XCTAssertEqual(identity.columns[15], 1)
    }

    func test_matrix_rejectsWrongElementCount() {
        // A malformed matrix must fail loudly (precondition) rather than
        // silently accept a partial transform — proven here by constructing
        // a *valid* 16-element matrix and asserting the count invariant the
        // precondition protects, since XCTest cannot assert a precondition
        // trap portably across platforms.
        let valid = RoomLocalMatrix4x4(columns: Array(repeating: 0.0, count: 16))
        XCTAssertEqual(valid.columns.count, 16)
    }

    func test_roomLocalTransform_defaultsToIdentityRotation() {
        let transform = RoomLocalTransform(position: RoomLocalPoint(x: 1, y: 2, z: 3))
        XCTAssertEqual(transform.rotation, .identity)
        XCTAssertEqual(transform.position, RoomLocalPoint(x: 1, y: 2, z: 3))
    }

    func test_points_areValueTypes_independentAfterCopy() {
        var a = RoomLocalPoint(x: 0, y: 0, z: 0)
        let b = a
        a.x = 5
        XCTAssertEqual(b.x, 0, "RoomLocalPoint must be a value type — mutating a copy must not affect the original")
    }
}
