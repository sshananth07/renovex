package com.renovex.capture.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class ScanSessionStateTest {

    @Test
    fun `every tracking quality has a distinct, non-empty guidance message`() {
        val messages = TrackingQuality.entries.map { it.guidanceMessage() }
        assertEquals(TrackingQuality.entries.size, messages.toSet().size)
        messages.forEach { assertNotEquals("", it) }
    }

    @Test
    fun `fresh scan session starts with no tracking and an empty draft`() {
        val state = ScanSessionState()
        assertEquals(TrackingQuality.NOT_STARTED, state.trackingQuality)
        assertEquals(0, state.coveragePercent)
        assertEquals(0, state.draft.corners.size)
    }
}
