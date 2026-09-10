package com.renovex.capture.capture.reconstruction

import kotlin.math.PI
import org.junit.Assert.assertEquals
import org.junit.Test

class RoomScanGuidanceTest {

    private val configured = ScanProgressConfig(
        sufficientWallEvidence = 0.72,
        weakWallEvidence = 0.41,
        aheadHalfAngleRadians = 0.40,
        behindHalfAngleRadians = 0.35,
    )

    @Test
    fun `no candidate evidence prompts scanner to begin`() {
        val guidance = RoomScanGuidance.guidanceFor(
            RoomShellBuildResult.Incomplete(emptyList(), 0.0),
            emptyList(),
            PointXZ(0.0, 0.0),
            0.0,
            configured,
        )

        assertEquals(GuidanceKind.BEGIN, guidance.kind)
        assertEquals(Direction.AHEAD, guidance.relativeDirection)
    }

    @Test
    fun `closed shell targets weakest wall evidence instead of raw angular gaps`() {
        val candidates = rectangleCandidates()
        val shell = rectangleShell(mapOf("bottom" to 0.90, "right" to 0.30, "top" to 0.90, "left" to 0.90))

        val guidance = RoomScanGuidance.guidanceFor(
            shell,
            candidates,
            cameraPosition = PointXZ(2.0, 1.5),
            cameraHeadingRadians = 0.0,
            config = configured,
        )

        assertEquals(GuidanceKind.SCAN_REMAINING_WALL, guidance.kind)
        assertEquals(Direction.AHEAD, guidance.relativeDirection)
    }

    @Test
    fun `incomplete candidate chain targets unresolved corner evidence`() {
        val candidates = rectangleCandidates().take(3)
        val incomplete = RoomShellBuildResult.Incomplete(
            orderedCandidateChains = listOf(candidates.map { it.candidateId }),
            confidence = 0.40,
        )

        val guidance = RoomScanGuidance.guidanceFor(
            incomplete,
            candidates,
            cameraPosition = PointXZ(2.0, 1.5),
            cameraHeadingRadians = 0.0,
            config = configured,
        )

        assertEquals(GuidanceKind.APPROACH_CORNER, guidance.kind)
    }

    @Test
    fun `sufficient evidence threshold follows configured boundary below at and above`() {
        fun kindAt(coverage: Double): GuidanceKind {
            val uniform = mapOf("bottom" to coverage, "right" to coverage, "top" to coverage, "left" to coverage)
            return RoomScanGuidance.guidanceFor(
                rectangleShell(uniform),
                rectangleCandidates(),
                PointXZ(2.0, 1.5),
                0.0,
                configured,
            ).kind
        }

        assertEquals(GuidanceKind.MOVE_SIDEWAYS, kindAt(configured.sufficientWallEvidence - 1e-6))
        assertEquals(GuidanceKind.COMPLETE, kindAt(configured.sufficientWallEvidence))
        assertEquals(GuidanceKind.COMPLETE, kindAt(configured.sufficientWallEvidence + 1e-6))
    }

    @Test
    fun `relative direction buckets use supplied angle boundaries`() {
        val candidate = rectangleCandidates().first()
        val incomplete = RoomShellBuildResult.Incomplete(listOf(listOf(candidate.candidateId)), 0.2)
        fun directionAt(relativeAngle: Double): Direction {
            val targetBearing = relativeAngle
            val distance = 2.0
            val target = PointXZ(distance * kotlin.math.cos(targetBearing), distance * kotlin.math.sin(targetBearing))
            val movedCandidate = candidate.copy(
                unitNormal = VectorXZ(-kotlin.math.sin(targetBearing), kotlin.math.cos(targetBearing)),
                unitDirection = VectorXZ(kotlin.math.cos(targetBearing), kotlin.math.sin(targetBearing)),
                supportingLineOffsetMeters = 0.0,
                observedIntervals = listOf(ProjectionInterval(distance - 0.1, distance + 0.1)),
            )
            return RoomScanGuidance.guidanceFor(
                incomplete,
                listOf(movedCandidate),
                PointXZ(0.0, 0.0),
                0.0,
                configured,
            ).relativeDirection
        }

        assertEquals(Direction.AHEAD, directionAt(configured.aheadHalfAngleRadians))
        assertEquals(Direction.LEFT, directionAt(configured.aheadHalfAngleRadians + 1e-6))
        assertEquals(Direction.BEHIND, directionAt(PI - configured.behindHalfAngleRadians))
    }

    private fun rectangleShell(coverage: Map<String, Double>): RoomShellBuildResult.Complete =
        RoomShellBuildResult.Complete(
            RoomShell(
                orderedCandidateIds = listOf("bottom", "right", "top", "left"),
                orderedCorners = listOf(
                    PointXZ(4.0, 0.0), PointXZ(4.0, 3.0),
                    PointXZ(0.0, 3.0), PointXZ(0.0, 0.0),
                ),
                observedCoverageByCandidate = coverage,
                confidence = 0.90,
            ),
        )

    private fun rectangleCandidates(): List<WallCandidate> = listOf(
        candidate("bottom", VectorXZ(1.0, 0.0), 0.0, 4.0, PointXZ(0.0, 0.0)),
        candidate("right", VectorXZ(0.0, 1.0), 0.0, 3.0, PointXZ(4.0, 0.0)),
        candidate("top", VectorXZ(-1.0, 0.0), -4.0, 0.0, PointXZ(4.0, 3.0)),
        candidate("left", VectorXZ(0.0, -1.0), -3.0, 0.0, PointXZ(0.0, 3.0)),
    )

    private fun candidate(
        id: String,
        direction: VectorXZ,
        start: Double,
        end: Double,
        pointOnLine: PointXZ,
    ): WallCandidate {
        val normal = VectorXZ(direction.z, -direction.x)
        return WallCandidate(
            candidateId = id,
            unitNormal = normal,
            unitDirection = direction,
            supportingLineOffsetMeters = normal.x * pointOnLine.x + normal.z * pointOnLine.z,
            observedIntervals = listOf(ProjectionInterval(start, end)),
            observationCount = 5,
            firstObservedAtMillis = 0,
            lastObservedAtMillis = 1_000,
            angleStdDevRadians = 0.0,
            offsetStdDevMeters = 0.0,
            stability = 0.90,
            state = WallCandidateState.STABLE,
        )
    }
}
