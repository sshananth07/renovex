package com.renovex.capture.ui.screens

import com.renovex.capture.capture.RoomAcquisitionStage
import com.renovex.capture.capture.RoomProposalStore
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.geometry.OpeningProfile
import com.renovex.capture.geometry.OpeningType
import com.renovex.capture.geometry.ObstacleType
import com.renovex.capture.geometry.ServicePointType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomReviewStateTest {
    @Test
    fun `complete frozen proposal enters review while incomplete evidence is refused`() {
        val store = RoomProposalStore()
        assertNotNull(store.createFromShell("complete", "space", RoomShellBuildResult.Complete(squareShell())))
        assertNull(store.createFromShell("incomplete", "space", RoomShellBuildResult.Incomplete(emptyList(), 0.4)))

        assertEquals(RoomAcquisitionStage.REVIEWING, RoomReviewController(store, "complete").state!!.proposal.stage)
        assertNull(RoomReviewController(store, "incomplete").state)
    }

    @Test
    fun `mutation changes working copy cancel preserves applied draft and apply persists`() {
        // Break caught: selecting Move currently commits immediately, leaving
        // no meaningful Cancel boundary.
        val store = storeWithSquare()
        val review = RoomReviewController(store, SESSION)
        review.selectCorner("$SESSION-corner-0")

        review.moveSelectedCorner(1.0, 0.5)
        assertEquals(1.0, review.state!!.workingCopy.corners.first().x, 0.0)
        assertEquals(0.0, review.state!!.proposal.draft.corners.first().x, 0.0)
        assertTrue(review.state!!.hasUnappliedChanges)

        review.cancelChanges()
        assertEquals(0.0, review.state!!.workingCopy.corners.first().x, 0.0)
        assertEquals(0.0, store.get(SESSION)!!.draft.corners.first().x, 0.0)

        review.moveSelectedCorner(1.0, 0.5)
        assertTrue(review.applyChanges())
        assertEquals(1.0, store.get(SESSION)!!.draft.corners.first().x, 0.0)
        assertFalse(review.state!!.hasUnappliedChanges)
    }

    @Test
    fun `derived measurements recompute from working geometry and reset restores snapshot`() {
        val review = RoomReviewController(storeWithSquare(), SESSION)
        assertEquals(12.0, review.state!!.measurements.floorAreaSquareMeters, 0.0)
        review.selectCorner("$SESSION-corner-1")

        review.moveSelectedCorner(5.0, 0.0)

        assertEquals(13.5, review.state!!.measurements.floorAreaSquareMeters, 1e-9)
        assertEquals(5.0, review.state!!.measurements.wallLengthsMeters.values.first(), 1e-9)
        review.applyChanges()
        review.resetToSnapshot()
        assertEquals(12.0, review.state!!.measurements.floorAreaSquareMeters, 0.0)
        assertEquals(0.0, review.state!!.proposal.draft.corners.first().x, 0.0)
    }

    @Test
    fun `opening ceiling obstacle and service edits remain working until apply`() {
        val review = RoomReviewController(storeWithSquare(), SESSION)
        review.selectWall(review.state!!.workingCopy.walls.first().id)
        review.addOpeningToSelectedWall(OpeningType.DOOR, OpeningProfile.RECTANGLE, 0.5, 0.9, 2.0)
        review.addObstacle(ObstacleType.COLUMN, 1.0, 1.0, 0.3, 0.3)
        review.addServicePoint(ServicePointType.ELECTRICAL, 2.0, 0.0)
        review.setCeilingHeight(2.85)

        assertTrue(review.state!!.proposal.draft.openings.isEmpty())
        assertEquals(1, review.state!!.workingCopy.openings.size)
        assertEquals(2.85, review.state!!.workingCopy.ceilingHeight.meters!!, 0.0)
        review.applyChanges()
        assertEquals(1, review.state!!.proposal.draft.openings.size)
        assertEquals(1, review.state!!.proposal.draft.obstacles.size)
        assertEquals(1, review.state!!.proposal.draft.servicePoints.size)
    }

    @Test
    fun `confirm locally uses current applied edited draft`() {
        val store = storeWithSquare()
        val review = RoomReviewController(store, SESSION)
        review.selectCorner("$SESSION-corner-0")
        review.moveSelectedCorner(0.5, 0.5)
        review.applyChanges()

        assertTrue(review.confirmLocally())

        assertEquals(0.5, store.get(SESSION)!!.draft.corners.first().x, 0.0)
        assertEquals(RoomAcquisitionStage.LOCALLY_CONFIRMED, store.get(SESSION)!!.stage)
        assertTrue(review.state!!.confirmed)
    }

    private fun storeWithSquare(): RoomProposalStore = RoomProposalStore().also {
        it.createFromShell(SESSION, "space", RoomShellBuildResult.Complete(squareShell()))
    }

    private fun squareShell(): RoomShell = RoomShell(
        orderedCandidateIds = listOf("bottom", "right", "top", "left"),
        orderedCorners = listOf(
            PointXZ(0.0, 0.0), PointXZ(4.0, 0.0),
            PointXZ(4.0, 3.0), PointXZ(0.0, 3.0),
        ),
        observedCoverageByCandidate = mapOf("bottom" to 1.0, "right" to 1.0, "top" to 1.0, "left" to 1.0),
        confidence = 0.9,
    )

    private companion object {
        const val SESSION = "scan-123"
    }
}
