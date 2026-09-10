package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ScanProgressEstimatorTest {

    private val configured = ScanProgressConfig(
        sufficientWallEvidence = 0.76,
        weakWallEvidence = 0.38,
        aheadHalfAngleRadians = 0.42,
        behindHalfAngleRadians = 0.36,
    )

    @Test
    fun `duplicate plane observation volume cannot increase mapped wall count or coverage`() {
        val candidate = candidate("wall", 0.0, 4.0)
        val shell = completeShell(
            candidates = listOf(candidate),
            corners = listOf(PointXZ(0.0, 0.0)),
            coverage = mapOf("wall" to 0.50),
        )

        val once = ScanProgressEstimator.estimate(shell, listOf(candidate.copy(observationCount = 1)), PointXZ(0.0, 0.0), configured)
        val repeated = ScanProgressEstimator.estimate(shell, listOf(candidate.copy(observationCount = 100)), PointXZ(0.0, 0.0), configured)

        assertEquals(once, repeated)
        assertEquals(1, repeated.mappedWallCount)
    }

    @Test
    fun `closed shell coverage is architectural edge length weighted`() {
        val corners = listOf(
            PointXZ(4.0, 0.0), PointXZ(4.0, 1.0),
            PointXZ(0.0, 1.0), PointXZ(0.0, 0.0),
        )
        val candidates = listOf(
            candidate("long-a", 0.0, 4.0),
            candidate("short-a", 0.0, 1.0),
            candidate("long-b", 0.0, 4.0),
            candidate("short-b", 0.0, 1.0),
        )
        val shell = completeShell(
            candidates,
            corners,
            mapOf("long-a" to 0.50, "short-a" to 1.0, "long-b" to 0.50, "short-b" to 1.0),
        )

        val progress = ScanProgressEstimator.estimate(shell, candidates, PointXZ(2.0, 0.5), configured)

        assertEquals(60, progress.coveragePercent)
        assertEquals(4, progress.mappedWallCount)
        assertEquals(false, progress.provisional)
    }

    @Test
    fun `incomplete shell reports candidate confidence as provisional coverage`() {
        val candidates = listOf(
            candidate("a", 0.0, 2.0, stability = 0.80),
            candidate("b", 0.0, 2.0, stability = 0.40),
        )
        val incomplete = RoomShellBuildResult.Incomplete(listOf(listOf("a", "b")), confidence = 0.30)

        val progress = ScanProgressEstimator.estimate(incomplete, candidates, PointXZ(0.0, 0.0), configured)

        assertTrue(progress.provisional)
        assertEquals(60, progress.coveragePercent)
        assertEquals(2, progress.mappedWallCount)
    }

    private fun completeShell(
        candidates: List<WallCandidate>,
        corners: List<PointXZ>,
        coverage: Map<String, Double>,
    ): RoomShellBuildResult.Complete = RoomShellBuildResult.Complete(
        RoomShell(
            orderedCandidateIds = candidates.map { it.candidateId },
            orderedCorners = corners,
            observedCoverageByCandidate = coverage,
            confidence = 0.90,
        ),
    )

    private fun candidate(
        id: String,
        start: Double,
        end: Double,
        stability: Double = 0.90,
    ): WallCandidate = WallCandidate(
        candidateId = id,
        unitNormal = VectorXZ(0.0, 1.0),
        unitDirection = VectorXZ(1.0, 0.0),
        supportingLineOffsetMeters = 0.0,
        observedIntervals = listOf(ProjectionInterval(start, end)),
        observationCount = 5,
        firstObservedAtMillis = 0,
        lastObservedAtMillis = 1_000,
        angleStdDevRadians = 0.0,
        offsetStdDevMeters = 0.0,
        stability = stability,
        state = WallCandidateState.STABLE,
    )
}
