package com.renovex.capture.capture

import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.geometry.CeilingHeightProposal
import com.renovex.capture.geometry.RoomDraftEditor
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomProposalStoreTest {
    @Test
    fun `snapshot is a deep immutable baseline while applied draft can change and reset`() {
        // Break caught: contractor Apply accidentally rewrites the frozen
        // scanner baseline, making Reset unable to restore what was reviewed.
        val mutableCorners = mutableListOf(
            PointXZ(0.0, 0.0), PointXZ(4.0, 0.0), PointXZ(4.0, 3.0), PointXZ(0.0, 3.0),
        )
        val store = RoomProposalStore()
        val proposal = store.createFromShell(
            scanSessionId = "scan",
            spaceId = "space",
            shellResult = RoomShellBuildResult.Complete(shell(mutableCorners)),
            capturedAtMillis = 123L,
        )!!
        mutableCorners[0] = PointXZ(99.0, 99.0)
        assertTrue(store.beginReview("scan"))

        val edited = RoomDraftEditor.moveCorner(proposal.draft, proposal.draft.corners.first().id, 1.0, 1.0)
        store.updateDraft("scan", edited)

        assertEquals(0.0, store.get("scan")!!.snapshot.baselineDraft.corners.first().x, 0.0)
        assertEquals(1.0, store.get("scan")!!.draft.corners.first().x, 0.0)
        val reset = store.resetDraft("scan")!!
        assertEquals(0.0, reset.draft.corners.first().x, 0.0)
        assertEquals(123L, reset.snapshot.capturedAtMillis)
    }

    @Test
    fun `confirmation returns current applied draft and never an unapplied alternative`() {
        val store = readyStore()
        store.beginReview("scan")
        val proposal = store.get("scan")!!
        val edited = RoomDraftEditor.moveCorner(proposal.draft, proposal.draft.corners.first().id, 2.0, 1.0)
        store.updateDraft("scan", edited)

        val confirmed = store.confirm("scan")!!

        assertEquals(2.0, confirmed.draft.corners.first().x, 0.0)
        assertEquals(RoomAcquisitionStage.LOCALLY_CONFIRMED, confirmed.stage)
    }

    @Test
    fun `continue scanning discards pending review and process lookup stops offering it`() {
        val store = readyStore()
        assertEquals("scan", store.pendingForSpace("space")!!.scanSessionId)
        store.beginReview("scan")

        assertTrue(store.continueScanning("scan"))
        assertEquals(RoomAcquisitionStage.SCANNING, store.get("scan")!!.stage)
        assertNull(store.pendingForSpace("space"))
        assertFalse(store.isConfirmed("scan"))
        assertTrue(store.consumeScanningResume("scan"))
        assertNull(store.get("scan"))
    }

    private fun readyStore(): RoomProposalStore = RoomProposalStore().also {
        it.createFromShell("scan", "space", RoomShellBuildResult.Complete(shell()), capturedAtMillis = 123L)
    }

    private fun shell(corners: List<PointXZ> = listOf(
        PointXZ(0.0, 0.0), PointXZ(4.0, 0.0), PointXZ(4.0, 3.0), PointXZ(0.0, 3.0),
    )): RoomShell = RoomShell(
        orderedCandidateIds = listOf("bottom", "right", "top", "left"),
        orderedCorners = corners,
        observedCoverageByCandidate = mapOf("bottom" to 1.0, "right" to 1.0, "top" to 1.0, "left" to 1.0),
        confidence = 0.9,
    )
}
