package com.renovex.capture.selection

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SelectionStateTest {

    @Test
    fun `fresh state has no project or space`() {
        val state = SelectionState()
        assertFalse(state.hasProject)
        assertFalse(state.hasSpace)
    }

    @Test
    fun `selecting a project sets it and has no space`() {
        val state = SelectionState().withProject("proj1", "Kitchen Reno")
        assertTrue(state.hasProject)
        assertEquals("proj1", state.projectId)
        assertEquals("Kitchen Reno", state.projectName)
        assertFalse(state.hasSpace)
    }

    @Test
    fun `selecting a space after a project sets both`() {
        val state = SelectionState().withProject("proj1", "Kitchen Reno").withSpace("space1", "Kitchen")
        assertTrue(state.hasProject)
        assertTrue(state.hasSpace)
        assertEquals("space1", state.spaceId)
        assertEquals("Kitchen", state.spaceName)
    }

    @Test
    fun `selecting a space with no project selected is a no-op`() {
        val state = SelectionState().withSpace("space1", "Kitchen")
        assertFalse(state.hasSpace)
    }

    @Test
    fun `selecting a new project clears any previously selected space`() {
        val state = SelectionState()
            .withProject("proj1", "Kitchen Reno")
            .withSpace("space1", "Kitchen")
            .withProject("proj2", "Bath Reno")

        assertEquals("proj2", state.projectId)
        assertFalse(state.hasSpace)
    }

    @Test
    fun `clearSpace keeps the project but drops the space`() {
        val state = SelectionState()
            .withProject("proj1", "Kitchen Reno")
            .withSpace("space1", "Kitchen")
            .clearSpace()

        assertTrue(state.hasProject)
        assertFalse(state.hasSpace)
    }

    @Test
    fun `clearAll resets to the fresh state`() {
        val state = SelectionState()
            .withProject("proj1", "Kitchen Reno")
            .withSpace("space1", "Kitchen")
            .clearAll()

        assertEquals(SelectionState(), state)
    }
}
