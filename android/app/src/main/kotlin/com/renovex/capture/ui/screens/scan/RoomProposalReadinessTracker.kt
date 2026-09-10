package com.renovex.capture.ui.screens.scan

import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.ProposalReadinessConfig
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.ScanProgress
import com.renovex.capture.capture.reconstruction.WallCandidate
import kotlin.math.hypot

data class ReadyProposalEvidence(
    val shell: RoomShell,
    val candidates: List<WallCandidate>,
    val progress: ScanProgress,
)

/** Latches the exact displayed proposal after configured temporal stability. */
class RoomProposalReadinessTracker(
    private val config: ProposalReadinessConfig,
) {
    var latched: ReadyProposalEvidence? = null
        private set
    private var previousShell: RoomShell? = null
    private var stableFrames: Int = 0

    fun observe(
        shellResult: RoomShellBuildResult,
        progress: ScanProgress,
        candidates: List<WallCandidate>,
        trackingUsable: Boolean,
    ): ReadyProposalEvidence? {
        latched?.let { return it }
        val shell = (shellResult as? RoomShellBuildResult.Complete)?.shell
        val qualifies = shell != null &&
            shell.confidence + EPSILON >= config.minimumShellConfidence &&
            progress.coveragePercent >= config.minimumCoveragePercent &&
            (!config.requireUsableTracking || trackingUsable)
        if (!qualifies) {
            previousShell = null
            stableFrames = 0
            return null
        }
        stableFrames = if (previousShell?.isStableWith(shell!!) == true) stableFrames + 1 else 1
        previousShell = shell
        if (stableFrames < config.minimumStableProposalFrames) return null
        return ReadyProposalEvidence(
            shell = shell!!.deepCopy(),
            candidates = candidates.map { it.copy(observedIntervals = it.observedIntervals.toList()) },
            progress = progress.copy(),
        ).also { latched = it }
    }

    fun resumeScanning() {
        latched = null
        previousShell = null
        stableFrames = 0
    }

    private fun RoomShell.isStableWith(other: RoomShell): Boolean =
        orderedCandidateIds == other.orderedCandidateIds &&
            orderedCorners.size == other.orderedCorners.size &&
            orderedCorners.indices.all { index ->
                orderedCorners[index].distanceTo(other.orderedCorners[index]) <=
                    config.maximumStableCornerDriftMeters + EPSILON
            }

    private fun PointXZ.distanceTo(other: PointXZ): Double = hypot(other.x - x, other.z - z)

    private fun RoomShell.deepCopy(): RoomShell = copy(
        orderedCandidateIds = orderedCandidateIds.toList(),
        orderedCorners = orderedCorners.toList(),
        observedCoverageByCandidate = observedCoverageByCandidate.toMap(),
    )

    private companion object {
        const val EPSILON = 1e-9
    }
}
