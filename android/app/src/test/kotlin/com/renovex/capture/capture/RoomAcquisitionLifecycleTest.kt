package com.renovex.capture.capture

import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.ProposalReadinessConfig
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomAcquisitionLifecycleTest {
    private val config = ProposalReadinessConfig(
        minimumCoveragePercent = 70,
        minimumStableProposalFrames = 3,
        minimumShellConfidence = 0.75,
        requireUsableTracking = true,
    )

    @Test
    fun `closed shell alone remains scanning until configured evidence is sufficient`() {
        // Break caught: any closed polygon immediately enables review even
        // when coverage or temporal stability is inadequate.
        val lifecycle = RoomAcquisitionLifecycle(config)

        assertFalse(lifecycle.observe(completeShell(0.90), 69, 3, trackingUsable = true))
        assertEquals(RoomAcquisitionStage.SCANNING, lifecycle.stage)
        assertFalse(lifecycle.observe(completeShell(0.90), 70, 2, trackingUsable = true))
        assertFalse(lifecycle.observe(completeShell(0.74), 70, 3, trackingUsable = true))
        assertFalse(lifecycle.observe(completeShell(0.90), 70, 3, trackingUsable = false))

        assertTrue(lifecycle.observe(completeShell(0.90), 70, 3, trackingUsable = true))
        assertEquals(RoomAcquisitionStage.READY_TO_REVIEW, lifecycle.stage)
    }

    @Test
    fun `review continue scan and local confirmation enforce legal transitions`() {
        val lifecycle = RoomAcquisitionLifecycle(config)
        assertFalse(lifecycle.beginReview())
        lifecycle.observe(completeShell(0.90), 80, 3, trackingUsable = true)

        assertTrue(lifecycle.beginReview())
        assertEquals(RoomAcquisitionStage.REVIEWING, lifecycle.stage)
        assertTrue(lifecycle.continueScanning())
        assertEquals(RoomAcquisitionStage.SCANNING, lifecycle.stage)
        assertFalse(lifecycle.confirmLocally())

        lifecycle.observe(completeShell(0.90), 80, 3, trackingUsable = true)
        lifecycle.beginReview()
        assertTrue(lifecycle.confirmLocally())
        assertEquals(RoomAcquisitionStage.LOCALLY_CONFIRMED, lifecycle.stage)
        assertFalse(lifecycle.continueScanning())
        assertFalse(lifecycle.beginReview())
    }

    private fun completeShell(confidence: Double): RoomShellBuildResult = RoomShellBuildResult.Complete(
        RoomShell(
            orderedCandidateIds = listOf("a", "b", "c"),
            orderedCorners = listOf(PointXZ(0.0, 0.0), PointXZ(2.0, 0.0), PointXZ(0.0, 2.0)),
            observedCoverageByCandidate = mapOf("a" to 1.0, "b" to 1.0, "c" to 1.0),
            confidence = confidence,
        ),
    )
}
