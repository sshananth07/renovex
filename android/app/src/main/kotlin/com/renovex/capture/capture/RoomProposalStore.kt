package com.renovex.capture.capture

import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.RoomShellDraftSimplifier
import com.renovex.capture.capture.reconstruction.RoomShellSimplificationConfig
import com.renovex.capture.capture.reconstruction.RoomShellToRoomDraftMapper
import com.renovex.capture.capture.reconstruction.WallCandidate
import com.renovex.capture.geometry.CeilingHeightProposal
import com.renovex.capture.geometry.RoomDraft

data class RoomProposalSnapshot(
    val sourceShell: RoomShell,
    val proposedShell: RoomShell,
    val baselineDraft: RoomDraft,
    val sourceCandidates: List<WallCandidate>,
    val collapsedCandidateIds: List<String>,
    val capturedAtMillis: Long,
)

data class LocalRoomProposal(
    val scanSessionId: String,
    val spaceId: String,
    val snapshot: RoomProposalSnapshot,
    val draft: RoomDraft,
    val stage: RoomAcquisitionStage = RoomAcquisitionStage.READY_TO_REVIEW,
)

/** In-process Task 5 proposal authority; Task 6 replaces only persistence. */
class RoomProposalStore {
    private val proposals = mutableMapOf<String, LocalRoomProposal>()

    @Synchronized
    fun get(scanSessionId: String): LocalRoomProposal? = proposals[scanSessionId]

    @Synchronized
    fun pendingForSpace(spaceId: String): LocalRoomProposal? = proposals.values
        .filter {
            it.spaceId == spaceId &&
                it.stage in setOf(RoomAcquisitionStage.READY_TO_REVIEW, RoomAcquisitionStage.REVIEWING)
        }
        .maxByOrNull { it.snapshot.capturedAtMillis }

    @Synchronized
    fun createFromShell(
        scanSessionId: String,
        spaceId: String = "",
        shellResult: RoomShellBuildResult,
        ceilingHeight: CeilingHeightProposal = CeilingHeightProposal.unconfirmed(),
        candidates: List<WallCandidate> = emptyList(),
        simplificationConfig: RoomShellSimplificationConfig = RoomShellSimplificationConfig(),
        capturedAtMillis: Long = System.currentTimeMillis(),
    ): LocalRoomProposal? {
        require(scanSessionId.isNotBlank())
        val complete = shellResult as? RoomShellBuildResult.Complete ?: return null
        val sourceShell = complete.shell.deepCopy()
        val copiedCandidates = candidates.map { it.copy(observedIntervals = it.observedIntervals.toList()) }
        val simplification = if (copiedCandidates.isEmpty()) {
            null
        } else {
            RoomShellDraftSimplifier.simplify(sourceShell, copiedCandidates, simplificationConfig)
        }
        val proposedShell = simplification?.simplifiedShell ?: sourceShell
        val baseline = createDraft(proposedShell, scanSessionId, ceilingHeight).deepCopy()
        val proposal = LocalRoomProposal(
            scanSessionId = scanSessionId,
            spaceId = spaceId,
            snapshot = RoomProposalSnapshot(
                sourceShell = sourceShell,
                proposedShell = proposedShell.deepCopy(),
                baselineDraft = baseline,
                sourceCandidates = copiedCandidates,
                collapsedCandidateIds = simplification?.collapsedCandidateIds.orEmpty().toList(),
                capturedAtMillis = capturedAtMillis,
            ),
            draft = baseline.deepCopy(),
        )
        proposals[scanSessionId] = proposal
        return proposal
    }

    fun createDraft(
        shell: RoomShell,
        scanSessionId: String,
        ceilingHeight: CeilingHeightProposal = CeilingHeightProposal.unconfirmed(),
    ): RoomDraft = RoomShellToRoomDraftMapper.map(
        shell = shell,
        ceilingHeight = ceilingHeight,
        idForCorner = { index -> "$scanSessionId-corner-$index" },
        idForWall = { candidateId -> "$scanSessionId-wall-$candidateId" },
    )

    @Synchronized
    fun beginReview(scanSessionId: String): Boolean {
        val current = proposals[scanSessionId] ?: return false
        if (current.stage != RoomAcquisitionStage.READY_TO_REVIEW) return false
        proposals[scanSessionId] = current.copy(stage = RoomAcquisitionStage.REVIEWING)
        return true
    }

    @Synchronized
    fun updateDraft(scanSessionId: String, draft: RoomDraft): LocalRoomProposal? {
        val current = proposals[scanSessionId] ?: return null
        if (current.stage != RoomAcquisitionStage.REVIEWING) return null
        return current.copy(draft = draft.deepCopy()).also { proposals[scanSessionId] = it }
    }

    @Synchronized
    fun resetDraft(scanSessionId: String): LocalRoomProposal? {
        val current = proposals[scanSessionId] ?: return null
        if (current.stage != RoomAcquisitionStage.REVIEWING) return null
        return current.copy(draft = current.snapshot.baselineDraft.deepCopy()).also {
            proposals[scanSessionId] = it
        }
    }

    @Synchronized
    fun continueScanning(scanSessionId: String): Boolean {
        val current = proposals[scanSessionId] ?: return false
        if (current.stage != RoomAcquisitionStage.REVIEWING) return false
        proposals[scanSessionId] = current.copy(stage = RoomAcquisitionStage.SCANNING)
        return true
    }

    @Synchronized
    fun consumeScanningResume(scanSessionId: String): Boolean {
        val current = proposals[scanSessionId] ?: return false
        if (current.stage != RoomAcquisitionStage.SCANNING) return false
        proposals.remove(scanSessionId)
        return true
    }

    @Synchronized
    fun confirm(scanSessionId: String): LocalRoomProposal? {
        val current = proposals[scanSessionId] ?: return null
        if (current.stage != RoomAcquisitionStage.REVIEWING) return null
        return current.copy(stage = RoomAcquisitionStage.LOCALLY_CONFIRMED).also {
            proposals[scanSessionId] = it
        }
    }

    @Synchronized
    fun isConfirmed(scanSessionId: String): Boolean =
        proposals[scanSessionId]?.stage == RoomAcquisitionStage.LOCALLY_CONFIRMED

    private fun RoomShell.deepCopy(): RoomShell = copy(
        orderedCandidateIds = orderedCandidateIds.toList(),
        orderedCorners = orderedCorners.toList(),
        observedCoverageByCandidate = observedCoverageByCandidate.toMap(),
    )

    private fun RoomDraft.deepCopy(): RoomDraft = copy(
        corners = corners.toList(),
        walls = walls.toList(),
        openings = openings.toList(),
        obstacles = obstacles.toList(),
        servicePoints = servicePoints.toList(),
    )
}
