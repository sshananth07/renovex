package com.renovex.capture.selection

/**
 * The contractor's current Project/Space selection — pure state so
 * navigation/persistence logic is unit-testable without Android
 * instrumentation (Task 4 TDD requirement: "project/space selection
 * state").
 *
 * Selecting a project clears any previously selected space: a space
 * belongs to exactly one project (backend/internal/spaces/space.go), so a
 * stale space selection from a different project would be meaningless.
 */
data class SelectionState(
    val projectId: String? = null,
    val projectName: String? = null,
    val spaceId: String? = null,
    val spaceName: String? = null,
) {
    val hasProject: Boolean get() = projectId != null
    val hasSpace: Boolean get() = spaceId != null

    fun withProject(id: String, name: String): SelectionState =
        SelectionState(projectId = id, projectName = name, spaceId = null, spaceName = null)

    fun withSpace(id: String, name: String): SelectionState =
        if (projectId == null) this else copy(spaceId = id, spaceName = name)

    fun clearSpace(): SelectionState = copy(spaceId = null, spaceName = null)

    fun clearAll(): SelectionState = SelectionState()
}
