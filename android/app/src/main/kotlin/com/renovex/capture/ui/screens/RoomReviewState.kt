package com.renovex.capture.ui.screens

import com.renovex.capture.capture.LocalRoomProposal
import com.renovex.capture.capture.RoomAcquisitionStage
import com.renovex.capture.capture.RoomProposalStore
import com.renovex.capture.geometry.ArchParameters
import com.renovex.capture.geometry.CeilingHeightProposal
import com.renovex.capture.geometry.OpeningProfile
import com.renovex.capture.geometry.OpeningType
import com.renovex.capture.geometry.ObstacleType
import com.renovex.capture.geometry.RoomDraft
import com.renovex.capture.geometry.RoomDraftEditor
import com.renovex.capture.geometry.RoomDraftMeasurementResult
import com.renovex.capture.geometry.RoomDraftMeasurements
import com.renovex.capture.geometry.ServicePointType

data class RoomReviewState(
    val proposal: LocalRoomProposal,
    val workingCopy: RoomDraft,
    val measurements: RoomDraftMeasurementResult,
    val selectedCornerId: String? = null,
    val selectedWallId: String? = null,
    val selectedOpeningId: String? = null,
    val selectedObstacleId: String? = null,
    val selectedServicePointId: String? = null,
    val hasUnappliedChanges: Boolean = false,
    val confirmed: Boolean = false,
)

/** Transactional contractor editor over an immutable scanner snapshot. */
class RoomReviewController(
    private val store: RoomProposalStore,
    private val scanSessionId: String,
) {
    var state: RoomReviewState? = initialize()
        private set

    private fun initialize(): RoomReviewState? {
        val existing = store.get(scanSessionId) ?: return null
        if (existing.stage == RoomAcquisitionStage.READY_TO_REVIEW) store.beginReview(scanSessionId)
        val proposal = store.get(scanSessionId) ?: return null
        return reviewState(proposal, proposal.draft, confirmed = proposal.stage == RoomAcquisitionStage.LOCALLY_CONFIRMED)
    }

    fun selectCorner(cornerId: String?) {
        val current = state ?: return
        state = current.copy(
            selectedCornerId = cornerId?.takeIf { id -> current.workingCopy.corners.any { it.id == id } },
            selectedWallId = null,
            selectedOpeningId = null,
            selectedObstacleId = null,
            selectedServicePointId = null,
        )
    }

    fun selectWall(wallId: String?) {
        val current = state ?: return
        state = current.copy(
            selectedCornerId = null,
            selectedWallId = wallId?.takeIf { id -> current.workingCopy.walls.any { it.id == id } },
            selectedOpeningId = null,
            selectedObstacleId = null,
            selectedServicePointId = null,
        )
    }

    fun selectOpening(openingId: String?) = selectOnly(
        openingId?.takeIf { id -> state?.workingCopy?.openings?.any { it.id == id } == true }, null, null,
    )

    fun selectObstacle(obstacleId: String?) = selectOnly(
        null, obstacleId?.takeIf { id -> state?.workingCopy?.obstacles?.any { it.id == id } == true }, null,
    )

    fun selectServicePoint(servicePointId: String?) = selectOnly(
        null, null, servicePointId?.takeIf { id -> state?.workingCopy?.servicePoints?.any { it.id == id } == true },
    )

    private fun selectOnly(openingId: String?, obstacleId: String?, servicePointId: String?) {
        val current = state ?: return
        state = current.copy(
            selectedCornerId = null,
            selectedWallId = null,
            selectedOpeningId = openingId,
            selectedObstacleId = obstacleId,
            selectedServicePointId = servicePointId,
        )
    }

    fun moveSelectedCorner(x: Double, z: Double) = edit { draft, review ->
        review.selectedCornerId?.let { RoomDraftEditor.moveCorner(draft, it, x, z) } ?: draft
    }

    fun addCornerAfterSelection(x: Double, z: Double) = edit { draft, review ->
        review.selectedCornerId?.let { RoomDraftEditor.addCorner(draft, it, x, z) } ?: draft
    }

    fun deleteSelectedCorner() {
        edit { draft, review ->
            review.selectedCornerId?.let { RoomDraftEditor.deleteCorner(draft, it) } ?: draft
        }
        state = state?.copy(selectedCornerId = null)
    }

    fun splitSelectedWall() {
        edit { draft, review ->
            review.selectedWallId?.let { RoomDraftEditor.splitWall(draft, it) } ?: draft
        }
        state = state?.copy(selectedWallId = null)
    }

    fun mergeSelectedWallWith(otherWallId: String) {
        edit { draft, review ->
            review.selectedWallId?.let { RoomDraftEditor.mergeWalls(draft, it, otherWallId) } ?: draft
        }
        state = state?.copy(selectedWallId = null)
    }

    fun enterSelectedWallLength(lengthMeters: Double) = edit { draft, review ->
        review.selectedWallId?.let { RoomDraftEditor.enterCorrectedWallLength(draft, it, lengthMeters) } ?: draft
    }

    fun addOpeningToSelectedWall(
        type: OpeningType,
        profile: OpeningProfile,
        offsetMeters: Double,
        widthMeters: Double,
        heightMeters: Double,
        sillHeightMeters: Double? = null,
        archParameters: ArchParameters? = null,
    ) = edit { draft, review ->
        review.selectedWallId?.let { wallId ->
            RoomDraftEditor.addOpening(
                draft, wallId, type, profile, offsetMeters, widthMeters, heightMeters,
                sillHeightMeters, archParameters,
            )
        } ?: draft
    }

    fun changeOpeningProfile(
        openingId: String,
        profile: OpeningProfile,
        springHeightMeters: Double? = null,
        archRiseMeters: Double? = null,
    ) = edit { draft, _ ->
        val arch = if (profile == OpeningProfile.ARCH && springHeightMeters != null && archRiseMeters != null) {
            ArchParameters(springHeightMeters, archRiseMeters)
        } else null
        RoomDraftEditor.changeOpeningProfile(draft, openingId, profile, arch)
    }

    fun updateOpening(
        openingId: String,
        type: OpeningType,
        profile: OpeningProfile,
        offsetMeters: Double,
        widthMeters: Double,
        heightMeters: Double,
        sillHeightMeters: Double? = null,
        springHeightMeters: Double? = null,
        archRiseMeters: Double? = null,
    ) = edit { draft, _ ->
        val arch = if (profile == OpeningProfile.ARCH && springHeightMeters != null && archRiseMeters != null) {
            ArchParameters(springHeightMeters, archRiseMeters)
        } else null
        RoomDraftEditor.updateOpening(
            draft, openingId, type, profile, offsetMeters, widthMeters, heightMeters,
            sillHeightMeters, arch,
        )
    }

    fun removeOpening(openingId: String) = edit { draft, _ -> RoomDraftEditor.removeOpening(draft, openingId) }

    fun addObstacle(type: ObstacleType, x: Double, z: Double, width: Double, depth: Double) =
        edit { draft, _ -> RoomDraftEditor.addObstacle(draft, type, x, z, width, depth) }

    fun updateObstacle(id: String, type: ObstacleType, x: Double, z: Double, width: Double, depth: Double) =
        edit { draft, _ -> RoomDraftEditor.updateObstacle(draft, id, type, x, z, width, depth) }

    fun removeObstacle(id: String) = edit { draft, _ -> RoomDraftEditor.removeObstacle(draft, id) }

    fun addServicePoint(type: ServicePointType, x: Double, z: Double) =
        edit { draft, _ -> RoomDraftEditor.addServicePoint(draft, type, x, z) }

    fun updateServicePoint(id: String, type: ServicePointType, x: Double, z: Double) =
        edit { draft, _ -> RoomDraftEditor.updateServicePoint(draft, id, type, x, z) }

    fun removeServicePoint(id: String) = edit { draft, _ -> RoomDraftEditor.removeServicePoint(draft, id) }

    fun setCeilingHeight(heightMeters: Double) {
        if (!heightMeters.isFinite() || heightMeters <= 0.0) return
        edit { draft, _ ->
            draft.copy(
                ceilingHeight = CeilingHeightProposal.contractorCorrected(heightMeters),
                walls = draft.walls.map { it.copy(heightMeters = heightMeters) },
            )
        }
    }

    fun cancelChanges() {
        val current = state ?: return
        state = reviewState(
            current.proposal,
            current.proposal.draft,
            selectedCornerId = current.selectedCornerId,
            selectedWallId = current.selectedWallId,
        )
    }

    fun applyChanges(): Boolean {
        val current = state ?: return false
        val updated = store.updateDraft(scanSessionId, current.workingCopy) ?: return false
        state = reviewState(updated, updated.draft)
        return true
    }

    fun resetToSnapshot(): Boolean {
        val reset = store.resetDraft(scanSessionId) ?: return false
        state = reviewState(reset, reset.draft)
        return true
    }

    fun continueScanning(): Boolean {
        if (!store.continueScanning(scanSessionId)) return false
        state = null
        return true
    }

    fun confirmLocally(): Boolean {
        val current = state ?: return false
        if (current.hasUnappliedChanges) return false
        val confirmed = store.confirm(scanSessionId) ?: return false
        state = reviewState(confirmed, confirmed.draft, confirmed = true)
        return true
    }

    private fun edit(transform: (RoomDraft, RoomReviewState) -> RoomDraft) {
        val current = state ?: return
        val next = transform(current.workingCopy, current)
        state = current.copy(
            workingCopy = next,
            measurements = RoomDraftMeasurements.calculate(next),
            hasUnappliedChanges = next != current.proposal.draft,
        )
    }

    private fun reviewState(
        proposal: LocalRoomProposal,
        workingCopy: RoomDraft,
        selectedCornerId: String? = null,
        selectedWallId: String? = null,
        confirmed: Boolean = false,
    ): RoomReviewState = RoomReviewState(
        proposal = proposal,
        workingCopy = workingCopy,
        measurements = RoomDraftMeasurements.calculate(workingCopy),
        selectedCornerId = selectedCornerId,
        selectedWallId = selectedWallId,
        hasUnappliedChanges = workingCopy != proposal.draft,
        confirmed = confirmed,
    )
}
