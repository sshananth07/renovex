package com.renovex.capture.ui.screens.scan

import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.ProposalReadinessConfig
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.ScanProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class RoomProposalReadinessTrackerTest {
    private val config = ProposalReadinessConfig(
        minimumCoveragePercent = 70,
        minimumStableProposalFrames = 3,
        minimumShellConfidence = 0.75,
        requireUsableTracking = true,
        maximumStableCornerDriftMeters = 0.05,
    )

    @Test
    fun `readiness latches exact third stable displayed proposal and ignores later callbacks`() {
        // Break caught: Review snapshots whichever reconstruction callback
        // happens to arrive after the user saw readiness.
        val tracker = RoomProposalReadinessTracker(config)
        assertNull(tracker.observe(shell(0.00), progress(), emptyList(), true))
        assertNull(tracker.observe(shell(0.02), progress(), emptyList(), true))
        val latched = tracker.observe(shell(0.03), progress(), emptyList(), true)!!

        assertEquals(0.03, latched.shell.orderedCorners.first().x, 0.0)
        val later = tracker.observe(shell(1.0), progress(), emptyList(), true)
        assertSame(latched, later)
    }

    @Test
    fun `unstable topology low coverage and unusable tracking reset readiness evidence`() {
        val tracker = RoomProposalReadinessTracker(config)
        tracker.observe(shell(0.0), progress(), emptyList(), true)
        tracker.observe(shell(0.0), progress(69), emptyList(), true)
        tracker.observe(shell(0.0), progress(), emptyList(), false)
        assertNull(tracker.observe(shell(0.0), progress(), emptyList(), true))
        assertNull(tracker.observe(shell(0.0), progress(), emptyList(), true))
        tracker.observe(shell(0.0), progress(), emptyList(), true)

        tracker.resumeScanning()
        assertNull(tracker.latched)
        assertNull(tracker.observe(shell(0.0), progress(), emptyList(), true))
    }

    private fun shell(firstCornerX: Double): RoomShellBuildResult = RoomShellBuildResult.Complete(
        RoomShell(
            listOf("a", "b", "c", "d"),
            listOf(PointXZ(firstCornerX, 0.0), PointXZ(4.0, 0.0), PointXZ(4.0, 3.0), PointXZ(0.0, 3.0)),
            mapOf("a" to 1.0, "b" to 1.0, "c" to 1.0, "d" to 1.0),
            0.90,
        ),
    )

    private fun progress(coverage: Int = 80): ScanProgress = ScanProgress(coverage, false, 4)
}
