package com.renovex.capture.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LocalCaptureStateMachineTest {

    @Test
    fun `legal sequence from draft through uploaded succeeds`() {
        var status = LocalCaptureStatus.DRAFT
        for (next in listOf(LocalCaptureStatus.CAPTURING, LocalCaptureStatus.UPLOADING, LocalCaptureStatus.UPLOADED)) {
            val result = LocalCaptureStateMachine.transition(status, next)
            assertEquals(next, result)
            status = result!!
        }
    }

    @Test
    fun `draft cannot jump directly to review`() {
        val result = LocalCaptureStateMachine.transition(LocalCaptureStatus.DRAFT, LocalCaptureStatus.REVIEW)
        assertNull(result)
    }

    @Test
    fun `capturing can fail and retry back to capturing`() {
        val failed = LocalCaptureStateMachine.transition(LocalCaptureStatus.CAPTURING, LocalCaptureStatus.FAILED)
        assertEquals(LocalCaptureStatus.FAILED, failed)
        val retried = LocalCaptureStateMachine.transition(failed!!, LocalCaptureStatus.CAPTURING)
        assertEquals(LocalCaptureStatus.CAPTURING, retried)
    }

    @Test
    fun `review can go to locally confirmed while offline`() {
        val result = LocalCaptureStateMachine.transition(LocalCaptureStatus.REVIEW, LocalCaptureStatus.LOCALLY_CONFIRMED)
        assertEquals(LocalCaptureStatus.LOCALLY_CONFIRMED, result)
    }

    @Test
    fun `locally confirmed can only advance to confirmed, nothing else`() {
        assertEquals(
            LocalCaptureStatus.CONFIRMED,
            LocalCaptureStateMachine.transition(LocalCaptureStatus.LOCALLY_CONFIRMED, LocalCaptureStatus.CONFIRMED),
        )
        assertNull(LocalCaptureStateMachine.transition(LocalCaptureStatus.LOCALLY_CONFIRMED, LocalCaptureStatus.CAPTURING))
        assertNull(LocalCaptureStateMachine.transition(LocalCaptureStatus.LOCALLY_CONFIRMED, LocalCaptureStatus.DRAFT))
    }

    @Test
    fun `confirmed is terminal with no outgoing transitions`() {
        assertTrue(LocalCaptureStateMachine.isTerminal(LocalCaptureStatus.CONFIRMED))
        assertNull(LocalCaptureStateMachine.transition(LocalCaptureStatus.CONFIRMED, LocalCaptureStatus.CAPTURING))
    }

    @Test
    fun `non-terminal statuses report isTerminal false`() {
        assertFalse(LocalCaptureStateMachine.isTerminal(LocalCaptureStatus.DRAFT))
        assertFalse(LocalCaptureStateMachine.isTerminal(LocalCaptureStatus.LOCALLY_CONFIRMED))
    }

    @Test
    fun `serverStatus maps every status except locally confirmed to the backend string`() {
        assertEquals("draft", LocalCaptureStatus.DRAFT.serverStatus)
        assertEquals("capturing", LocalCaptureStatus.CAPTURING.serverStatus)
        assertEquals("uploading", LocalCaptureStatus.UPLOADING.serverStatus)
        assertEquals("uploaded", LocalCaptureStatus.UPLOADED.serverStatus)
        assertEquals("review", LocalCaptureStatus.REVIEW.serverStatus)
        assertEquals("confirmed", LocalCaptureStatus.CONFIRMED.serverStatus)
        assertEquals("failed", LocalCaptureStatus.FAILED.serverStatus)
        assertNull(LocalCaptureStatus.LOCALLY_CONFIRMED.serverStatus)
    }
}
