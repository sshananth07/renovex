package com.renovex.capture.capture

import com.renovex.capture.capture.reconstruction.ProposalReadinessConfig
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult

enum class RoomAcquisitionStage {
    SCANNING,
    READY_TO_REVIEW,
    REVIEWING,
    LOCALLY_CONFIRMED,
}

/** Pure Task 5 acquisition authority state machine. */
class RoomAcquisitionLifecycle(
    private val config: ProposalReadinessConfig,
) {
    var stage: RoomAcquisitionStage = RoomAcquisitionStage.SCANNING
        private set

    fun observe(
        shellResult: RoomShellBuildResult,
        coveragePercent: Int,
        stableProposalFrames: Int,
        trackingUsable: Boolean,
    ): Boolean {
        if (stage != RoomAcquisitionStage.SCANNING) return false
        val shell = (shellResult as? RoomShellBuildResult.Complete)?.shell ?: return false
        val ready = shell.confidence + EPSILON >= config.minimumShellConfidence &&
            coveragePercent >= config.minimumCoveragePercent &&
            stableProposalFrames >= config.minimumStableProposalFrames &&
            (!config.requireUsableTracking || trackingUsable)
        if (ready) stage = RoomAcquisitionStage.READY_TO_REVIEW
        return ready
    }

    fun beginReview(): Boolean = transition(
        RoomAcquisitionStage.READY_TO_REVIEW,
        RoomAcquisitionStage.REVIEWING,
    )

    fun continueScanning(): Boolean = transition(
        RoomAcquisitionStage.REVIEWING,
        RoomAcquisitionStage.SCANNING,
    )

    fun confirmLocally(): Boolean = transition(
        RoomAcquisitionStage.REVIEWING,
        RoomAcquisitionStage.LOCALLY_CONFIRMED,
    )

    private fun transition(from: RoomAcquisitionStage, to: RoomAcquisitionStage): Boolean {
        if (stage != from) return false
        stage = to
        return true
    }

    private companion object {
        const val EPSILON = 1e-9
    }
}
