package com.renovex.capture.capture.reconstruction

import kotlin.math.hypot
import kotlin.math.roundToInt

data class ScanProgress(
    val coveragePercent: Int,
    val provisional: Boolean,
    val mappedWallCount: Int,
)

/** Reports reconstruction evidence, never raw ARCore plane volume. */
object ScanProgressEstimator {
    fun estimate(
        shellResult: RoomShellBuildResult,
        candidates: List<WallCandidate>,
        cameraPosition: PointXZ,
        config: ScanProgressConfig = ScanProgressConfig(),
    ): ScanProgress {
        require(cameraPosition.x.isFinite() && cameraPosition.z.isFinite())
        // Accepting config here keeps this component on the same scanner-policy seam as guidance,
        // even though its current output is an unthresholded evidence percentage.
        require(config.sufficientWallEvidence >= config.weakWallEvidence)
        val stableById = candidates
            .filter { it.state == WallCandidateState.STABLE }
            .associateBy { it.candidateId }
        return when (shellResult) {
            is RoomShellBuildResult.Complete -> {
                val shell = shellResult.shell
                val weightedCoverage = lengthWeightedCoverage(shell)
                ScanProgress(
                    coveragePercent = (weightedCoverage * 100.0).roundToInt().coerceIn(0, 100),
                    provisional = false,
                    mappedWallCount = shell.orderedCandidateIds.distinct().count { it in stableById },
                )
            }
            is RoomShellBuildResult.Incomplete -> {
                val provisionalCoverage = stableById.values
                    .map { it.stability }
                    .averageOrZero()
                    .coerceIn(0.0, 1.0)
                ScanProgress(
                    coveragePercent = (provisionalCoverage * 100.0).roundToInt(),
                    provisional = true,
                    mappedWallCount = stableById.size,
                )
            }
        }
    }

    private fun lengthWeightedCoverage(shell: RoomShell): Double {
        if (shell.orderedCandidateIds.isEmpty()) return 0.0
        if (shell.orderedCorners.size != shell.orderedCandidateIds.size) {
            return shell.observedCoverageByCandidate.values.averageOrZero().coerceIn(0.0, 1.0)
        }
        var totalLength = 0.0
        var observedLength = 0.0
        shell.orderedCandidateIds.indices.forEach { index ->
            val first = shell.orderedCorners[(index - 1 + shell.orderedCorners.size) % shell.orderedCorners.size]
            val second = shell.orderedCorners[index]
            val length = hypot(second.x - first.x, second.z - first.z)
            val coverage = shell.observedCoverageByCandidate[shell.orderedCandidateIds[index]]
                ?.coerceIn(0.0, 1.0) ?: 0.0
            totalLength += length
            observedLength += length * coverage
        }
        return if (totalLength <= NUMERIC_EPSILON) {
            shell.observedCoverageByCandidate.values.averageOrZero().coerceIn(0.0, 1.0)
        } else {
            (observedLength / totalLength).coerceIn(0.0, 1.0)
        }
    }

    private fun Collection<Double>.averageOrZero(): Double = if (isEmpty()) 0.0 else average()

    private const val NUMERIC_EPSILON = 1e-9
}
