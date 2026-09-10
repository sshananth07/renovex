import Foundation

/// The contractor's current Project/Space selection — pure state so
/// navigation/persistence logic is unit-testable without a device (RP1 TDD
/// requirement: "project/space selection state"). Mirrors the frozen
/// Android provider's `SelectionState`
/// (android/app/src/main/kotlin/com/renovex/capture/selection/SelectionState.kt).
///
/// Selecting a project clears any previously selected space: a space
/// belongs to exactly one project (`backend/internal/spaces/space.go`), so
/// a stale space selection from a different project would be meaningless.
public struct ProjectSpaceSelection: Equatable, Sendable {
    public var projectID: String?
    public var projectName: String?
    public var spaceID: String?
    public var spaceName: String?

    public init(
        projectID: String? = nil,
        projectName: String? = nil,
        spaceID: String? = nil,
        spaceName: String? = nil
    ) {
        self.projectID = projectID
        self.projectName = projectName
        self.spaceID = spaceID
        self.spaceName = spaceName
    }

    public var hasProject: Bool { projectID != nil }
    public var hasSpace: Bool { spaceID != nil }

    public func withProject(id: String, name: String) -> ProjectSpaceSelection {
        ProjectSpaceSelection(projectID: id, projectName: name, spaceID: nil, spaceName: nil)
    }

    public func withSpace(id: String, name: String) -> ProjectSpaceSelection {
        guard projectID != nil else { return self }
        var copy = self
        copy.spaceID = id
        copy.spaceName = name
        return copy
    }

    public func clearingSpace() -> ProjectSpaceSelection {
        var copy = self
        copy.spaceID = nil
        copy.spaceName = nil
        return copy
    }

    public static let empty = ProjectSpaceSelection()
}
