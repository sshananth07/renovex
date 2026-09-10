import XCTest
@testable import RenovexCaptureCore

/// RP1 TDD requirement: "project/space selection state." Ported from the
/// frozen Android provider's `SelectionState` test coverage.
final class ProjectSpaceSelectionTests: XCTestCase {
    func test_empty_hasNoProjectOrSpace() {
        let selection = ProjectSpaceSelection.empty
        XCTAssertFalse(selection.hasProject)
        XCTAssertFalse(selection.hasSpace)
    }

    func test_withProject_setsProjectAndClearsAnyPriorSpace() {
        let selection = ProjectSpaceSelection.empty
            .withProject(id: "p1", name: "Kitchen Reno")
            .withSpace(id: "s1", name: "Kitchen")
            .withProject(id: "p2", name: "Bathroom Reno")

        XCTAssertEqual(selection.projectID, "p2")
        XCTAssertEqual(selection.projectName, "Bathroom Reno")
        XCTAssertFalse(selection.hasSpace, "selecting a new project must clear the stale space from the old project")
    }

    func test_withSpace_requiresProjectAlreadySelected() {
        let selection = ProjectSpaceSelection.empty.withSpace(id: "s1", name: "Kitchen")
        XCTAssertFalse(selection.hasSpace, "a space cannot be selected without a project — backend Space always belongs to exactly one Project")
    }

    func test_withSpace_afterProject_succeeds() {
        let selection = ProjectSpaceSelection.empty
            .withProject(id: "p1", name: "Kitchen Reno")
            .withSpace(id: "s1", name: "Kitchen")

        XCTAssertTrue(selection.hasSpace)
        XCTAssertEqual(selection.spaceID, "s1")
        XCTAssertEqual(selection.spaceName, "Kitchen")
    }

    func test_clearingSpace_preservesProject() {
        let selection = ProjectSpaceSelection.empty
            .withProject(id: "p1", name: "Kitchen Reno")
            .withSpace(id: "s1", name: "Kitchen")
            .clearingSpace()

        XCTAssertTrue(selection.hasProject)
        XCTAssertFalse(selection.hasSpace)
    }
}
