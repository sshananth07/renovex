package com.renovex.capture.capture.reconstruction

import kotlin.math.hypot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomShellBuilderTest {

    private val configured = RoomShellConfig(
        candidateStabilityWeight = 0.31,
        intersectionConfidenceWeight = 0.27,
        observedCoverageWeight = 0.29,
        cameraPathWeight = 0.13,
        minimumProposedShellConfidence = 0.55,
        boundaryEpsilonMeters = 0.03,
        minimumInteriorCameraClearanceMeters = 0.0,
        minimumInteriorCameraEvidenceSamples = 1,
    )

    @Test
    fun `rectangle creates four ordered walls four corners and one coherent shell`() {
        val points = listOf(point(0, 0), point(4, 0), point(4, 3), point(0, 3))
        val walls = wallsForPolygon("rect", points)

        val result = buildComplete(walls, intersectionsFor(walls, points), listOf(point(2, 1)))

        assertEquals(4, result.orderedCandidateIds.size)
        assertEquals(4, result.orderedCorners.size)
        assertPointSet(points.toSet(), result.orderedCorners)
    }

    @Test
    fun `l shaped room preserves six wall concave shell`() {
        val points = listOf(
            point(0, 0), point(4, 0), point(4, 2),
            point(2, 2), point(2, 4), point(0, 4),
        )
        val walls = wallsForPolygon("l", points)

        val result = buildComplete(walls, intersectionsFor(walls, points), listOf(point(1, 1)))

        assertEquals(6, result.orderedCandidateIds.size)
        assertEquals(6, result.orderedCorners.size)
        assertPointSet(points.toSet(), result.orderedCorners)
    }

    @Test
    fun `angled room keeps non rectangular corner geometry`() {
        val points = listOf(point(0, 0), point(4, 0), point(3, 3), point(0, 3))
        val walls = wallsForPolygon("angled", points)

        val result = buildComplete(walls, intersectionsFor(walls, points), listOf(point(1, 1)))

        assertPointSet(points.toSet(), result.orderedCorners)
        assertTrue(result.orderedCorners.contains(PointXZ(3.0, 3.0)))
    }

    @Test
    fun `incomplete evidence returns chains and never fabricates a closing wall`() {
        val points = listOf(point(0, 0), point(4, 0), point(4, 3), point(0, 3))
        val walls = wallsForPolygon("partial", points).take(3)
        val intersections = listOf(
            intersection(walls[0], walls[1], points[1]),
            intersection(walls[1], walls[2], points[2]),
        )

        val result = RoomShellBuilder.build(walls, intersections, emptyList(), configured)

        assertTrue(result is RoomShellBuildResult.Incomplete)
        val incomplete = result as RoomShellBuildResult.Incomplete
        assertTrue(incomplete.orderedCandidateChains.flatten().all { id -> walls.any { it.candidateId == id } })
        assertEquals(3, incomplete.orderedCandidateChains.flatten().distinct().size)
    }

    @Test
    fun `inferred corner extensions add no observed coverage`() {
        val points = listOf(point(0, 0), point(4, 0), point(4, 3), point(0, 3))
        val fullWalls = wallsForPolygon("coverage", points)
        val shortenedFirst = fullWalls.first().copy(
            observedIntervals = listOf(ProjectionInterval(0.20, 3.80)),
        )
        val walls = listOf(shortenedFirst) + fullWalls.drop(1)

        val result = buildComplete(walls, intersectionsFor(walls, points), listOf(point(2, 1)))

        assertEquals(0.90, result.observedCoverageByCandidate.getValue(shortenedFirst.candidateId), 1e-9)
    }

    @Test
    fun `minimum shell confidence follows supplied boundary below at and above`() {
        val points = listOf(point(0, 0), point(3, 0), point(1, 2))
        val walls = wallsForPolygon("score", points, stability = 0.60)
        val intersections = intersectionsFor(walls, points, confidence = 0.60)
        val scoreOnlyConfig = configured.copy(
            candidateStabilityWeight = 1.0,
            intersectionConfidenceWeight = 0.0,
            observedCoverageWeight = 0.0,
            cameraPathWeight = 0.0,
        )

        assertTrue(RoomShellBuilder.build(walls, intersections, emptyList(), scoreOnlyConfig.copy(minimumProposedShellConfidence = 0.60 - 1e-6)) is RoomShellBuildResult.Complete)
        assertTrue(RoomShellBuilder.build(walls, intersections, emptyList(), scoreOnlyConfig.copy(minimumProposedShellConfidence = 0.60)) is RoomShellBuildResult.Complete)
        assertTrue(RoomShellBuilder.build(walls, intersections, emptyList(), scoreOnlyConfig.copy(minimumProposedShellConfidence = 0.60 + 1e-6)) is RoomShellBuildResult.Incomplete)
    }

    @Test
    fun `configured scoring weights choose between supported cycles`() {
        val pointsA = listOf(point(0, 0), point(2, 0), point(1, 2))
        val pointsB = listOf(point(10, 0), point(12, 0), point(11, 2))
        val wallsA = wallsForPolygon("a", pointsA, stability = 0.90)
        val wallsB = wallsForPolygon("b", pointsB, stability = 0.40)
        val allWalls = wallsA + wallsB
        val allIntersections = intersectionsFor(wallsA, pointsA) + intersectionsFor(wallsB, pointsB)
        val candidateDominant = configured.copy(
            candidateStabilityWeight = 0.90,
            intersectionConfidenceWeight = 0.0,
            observedCoverageWeight = 0.0,
            cameraPathWeight = 0.10,
            minimumProposedShellConfidence = 0.0,
        )
        val cameraDominant = candidateDominant.copy(
            candidateStabilityWeight = 0.10,
            cameraPathWeight = 0.90,
        )

        val byStability = RoomShellBuilder.build(allWalls, allIntersections, listOf(point(11, 1)), candidateDominant)
        val byCamera = RoomShellBuilder.build(allWalls, allIntersections, listOf(point(11, 1)), cameraDominant)

        assertTrue((byStability as RoomShellBuildResult.Complete).shell.orderedCandidateIds.all { it.startsWith("a-") })
        assertTrue((byCamera as RoomShellBuildResult.Complete).shell.orderedCandidateIds.all { it.startsWith("b-") })
    }

    @Test
    fun `interior clearance follows configured boundary below at and above`() {
        // Break caught: a hard-coded or strict-only clearance comparison would
        // reject the configured boundary or ignore scanner tuning entirely.
        val points = listOf(PointXZ(0.0, 0.0), PointXZ(2.0, 0.0), PointXZ(2.0, 0.4), PointXZ(0.0, 0.4))
        val walls = wallsForPolygon("clearance", points)
        val intersections = intersectionsFor(walls, points)
        val trajectory = listOf(PointXZ(0.8, 0.2), PointXZ(1.0, 0.2), PointXZ(1.2, 0.2))
        val gateConfig = configured.copy(
            minimumProposedShellConfidence = 0.0,
            minimumInteriorCameraEvidenceSamples = 3,
        )

        assertTrue(RoomShellBuilder.build(walls, intersections, trajectory, gateConfig.copy(minimumInteriorCameraClearanceMeters = 0.2 - 1e-6)) is RoomShellBuildResult.Complete)
        assertTrue(RoomShellBuilder.build(walls, intersections, trajectory, gateConfig.copy(minimumInteriorCameraClearanceMeters = 0.2)) is RoomShellBuildResult.Complete)
        assertTrue(RoomShellBuilder.build(walls, intersections, trajectory, gateConfig.copy(minimumInteriorCameraClearanceMeters = 0.2 + 1e-6)) is RoomShellBuildResult.Incomplete)
    }

    @Test
    fun `one noisy interior pose cannot validate an object sized shell`() {
        // Break caught: accepting on any single camera pose lets one noisy pose
        // resurrect the furniture-loop false positive.
        val points = listOf(PointXZ(0.0, 0.0), PointXZ(2.0, 0.0), PointXZ(2.0, 0.4), PointXZ(0.0, 0.4))
        val walls = wallsForPolygon("samples", points)
        val intersections = intersectionsFor(walls, points)
        val gateConfig = configured.copy(
            minimumProposedShellConfidence = 0.0,
            minimumInteriorCameraClearanceMeters = 0.15,
            minimumInteriorCameraEvidenceSamples = 3,
        )
        val oneCrediblePose = listOf(
            PointXZ(0.8, 0.03),
            PointXZ(1.0, 0.20),
            PointXZ(1.2, 0.37),
        )
        val threeCrediblePoses = listOf(
            PointXZ(0.8, 0.20),
            PointXZ(1.0, 0.20),
            PointXZ(1.2, 0.20),
        )

        assertTrue(RoomShellBuilder.build(walls, intersections, oneCrediblePose, gateConfig) is RoomShellBuildResult.Incomplete)
        assertTrue(RoomShellBuilder.build(walls, intersections, threeCrediblePoses, gateConfig) is RoomShellBuildResult.Complete)
    }

    @Test
    fun `genuine short return remains valid inside a room sized irregular shell`() {
        // Break caught: rejecting short edges rather than testing whole-shell
        // interior plausibility would destroy real returns and irregular rooms.
        val points = listOf(
            PointXZ(0.0, 0.0),
            PointXZ(4.0, 0.0),
            PointXZ(4.0, 0.30),
            PointXZ(3.50, 0.30),
            PointXZ(3.50, 3.0),
            PointXZ(0.0, 3.0),
        )
        val walls = wallsForPolygon("short-return", points)
        val trajectory = listOf(PointXZ(1.0, 1.0), PointXZ(1.2, 1.1), PointXZ(1.4, 1.2))
        val result = RoomShellBuilder.build(
            walls,
            intersectionsFor(walls, points),
            trajectory,
            configured.copy(
                minimumInteriorCameraClearanceMeters = 0.30,
                minimumInteriorCameraEvidenceSamples = 3,
            ),
        )

        assertTrue(result is RoomShellBuildResult.Complete)
        assertEquals(6, (result as RoomShellBuildResult.Complete).shell.orderedCandidateIds.size)
    }

    @Test
    fun `camera trajectory outside shell keeps existing soft scoring behavior`() {
        // Break caught: turning the new false-loop rejection into a universal
        // requirement that contractors must stand inside every proposed shell.
        val points = listOf(point(0, 0), point(4, 0), point(4, 3), point(0, 3))
        val walls = wallsForPolygon("outside", points)
        val result = RoomShellBuilder.build(
            walls,
            intersectionsFor(walls, points),
            listOf(PointXZ(5.0, 1.0), PointXZ(5.0, 1.5), PointXZ(5.0, 2.0)),
            configured.copy(
                minimumProposedShellConfidence = 0.0,
                minimumInteriorCameraClearanceMeters = 0.30,
                minimumInteriorCameraEvidenceSamples = 3,
            ),
        )

        assertTrue(result is RoomShellBuildResult.Complete)
    }

    @Test
    fun `outside camera cannot validate shell incapable of configured interior clearance`() {
        // Break caught: treating all outside-camera cycles softly allows the
        // Xiaomi 11T's 0.18 m by 0.38 m object loop to bypass interior evidence.
        val points = listOf(
            PointXZ(0.93, -1.99),
            PointXZ(0.75, -1.98),
            PointXZ(0.75, -1.63),
            PointXZ(0.88, -1.62),
        )
        val walls = wallsForPolygon("device-object", points)
        val result = RoomShellBuilder.build(
            walls,
            intersectionsFor(walls, points),
            listOf(PointXZ(0.0, 0.0), PointXZ(0.1, 0.0), PointXZ(0.0, 0.1)),
            configured.copy(
                minimumProposedShellConfidence = 0.0,
                minimumInteriorCameraClearanceMeters = 0.30,
                minimumInteriorCameraEvidenceSamples = 3,
            ),
        )

        assertTrue(result is RoomShellBuildResult.Incomplete)
    }

    private fun buildComplete(
        walls: List<WallCandidate>,
        intersections: List<WallIntersection>,
        camera: List<PointXZ>,
    ): RoomShell = (RoomShellBuilder.build(walls, intersections, camera, configured) as RoomShellBuildResult.Complete).shell

    private fun wallsForPolygon(
        prefix: String,
        points: List<PointXZ>,
        stability: Double = 0.90,
    ): List<WallCandidate> = points.indices.map { index ->
        wall("$prefix-$index", points[index], points[(index + 1) % points.size], stability)
    }

    private fun intersectionsFor(
        walls: List<WallCandidate>,
        points: List<PointXZ>,
        confidence: Double = 0.90,
    ): List<WallIntersection> = walls.indices.map { index ->
        intersection(walls[index], walls[(index + 1) % walls.size], points[(index + 1) % points.size], confidence)
    }

    private fun wall(id: String, startPoint: PointXZ, endPoint: PointXZ, stability: Double): WallCandidate {
        val deltaX = endPoint.x - startPoint.x
        val deltaZ = endPoint.z - startPoint.z
        val length = hypot(deltaX, deltaZ)
        val direction = VectorXZ(deltaX / length, deltaZ / length)
        val normal = VectorXZ(direction.z, -direction.x)
        val offset = normal.x * startPoint.x + normal.z * startPoint.z
        val firstProjection = direction.x * startPoint.x + direction.z * startPoint.z
        val secondProjection = direction.x * endPoint.x + direction.z * endPoint.z
        return WallCandidate(
            candidateId = id,
            unitNormal = normal,
            unitDirection = direction,
            supportingLineOffsetMeters = offset,
            observedIntervals = listOf(ProjectionInterval(minOf(firstProjection, secondProjection), maxOf(firstProjection, secondProjection))),
            observationCount = 5,
            firstObservedAtMillis = 0,
            lastObservedAtMillis = 1_000,
            angleStdDevRadians = 0.01,
            offsetStdDevMeters = 0.01,
            stability = stability,
            state = WallCandidateState.STABLE,
        )
    }

    private fun intersection(
        first: WallCandidate,
        second: WallCandidate,
        point: PointXZ,
        confidence: Double = 0.90,
    ): WallIntersection = WallIntersection(first.candidateId, second.candidateId, point, confidence)

    private fun point(x: Int, z: Int): PointXZ = PointXZ(x.toDouble(), z.toDouble())

    private fun assertPointSet(expected: Set<PointXZ>, actual: List<PointXZ>) {
        assertEquals(expected, actual.toSet())
    }
}
