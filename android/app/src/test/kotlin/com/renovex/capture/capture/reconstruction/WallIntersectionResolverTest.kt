package com.renovex.capture.capture.reconstruction

import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WallIntersectionResolverTest {

    private val configured = IntersectionConfig(
        minimumAngleRadians = 0.20,
        maximumObservedExtentExtensionMeters = 0.50,
        perpendicularBoostWindowRadians = 0.30,
        maximumPerpendicularConfidenceBoost = 0.06,
    )

    @Test
    fun `four stable rectangle lines produce four architectural intersections`() {
        val walls = listOf(
            wall("top", normalAngle = PI / 2.0, offset = 0.0, start = -4.0, end = 0.0),
            wall("right", normalAngle = 0.0, offset = 4.0, start = 0.0, end = 3.0),
            wall("bottom", normalAngle = PI / 2.0, offset = 3.0, start = -4.0, end = 0.0),
            wall("left", normalAngle = 0.0, offset = 0.0, start = 0.0, end = 3.0),
        )

        val intersections = WallIntersectionResolver.resolve(walls, configured)

        assertEquals(4, intersections.size)
        assertPointSet(
            setOf(PointXZ(0.0, 0.0), PointXZ(4.0, 0.0), PointXZ(4.0, 3.0), PointXZ(0.0, 3.0)),
            intersections.map { it.point },
        )
    }

    @Test
    fun `angled supporting lines keep their exact intersection without ninety degree snapping`() {
        val horizontal = wall("horizontal", PI / 2.0, 0.0, -3.0, 0.0)
        val diagonalNormalAngle = PI / 4.0
        val diagonal = wall(
            "diagonal",
            diagonalNormalAngle,
            offset = 2.0 * cos(diagonalNormalAngle),
            start = -2.5,
            end = 0.0,
        )
        val originalDirection = diagonal.unitDirection

        val result = WallIntersectionResolver.resolve(listOf(horizontal, diagonal), configured).single()

        assertEquals(2.0, result.point.x, 1e-9)
        assertEquals(0.0, result.point.z, 1e-9)
        assertEquals(originalDirection, diagonal.unitDirection)
    }

    @Test
    fun `noisy observed endpoints may extend to a plausible stable-line corner`() {
        val horizontal = wall("horizontal", PI / 2.0, 0.0, -4.0, -0.20)
        val vertical = wall("vertical", 0.0, 0.0, 0.15, 3.0)

        val result = WallIntersectionResolver.resolve(listOf(horizontal, vertical), configured).single()

        assertEquals(PointXZ(0.0, 0.0), result.point)
        assertEquals(-0.20, horizontal.observedEndProjectionMeters, 1e-9)
        assertEquals(0.15, vertical.observedStartProjectionMeters, 1e-9)
    }

    @Test
    fun `minimum angle follows configured boundary below at and above`() {
        fun intersectionsAt(angle: Double): Int {
            val base = wall("base", PI / 2.0, 0.0, -1.0, 1.0)
            val rotated = wall("rotated", PI / 2.0 + angle, 0.0, -1.0, 1.0)
            return WallIntersectionResolver.resolve(listOf(base, rotated), configured).size
        }

        assertEquals(0, intersectionsAt(configured.minimumAngleRadians - 1e-6))
        assertEquals(1, intersectionsAt(configured.minimumAngleRadians))
        assertEquals(1, intersectionsAt(configured.minimumAngleRadians + 1e-6))
    }

    @Test
    fun `maximum extent extension follows configured boundary below at and above`() {
        fun intersectionsAt(extension: Double): List<WallIntersection> {
            val horizontal = wall("horizontal", PI / 2.0, 0.0, extension, extension + 1.0)
            val vertical = wall("vertical", 0.0, 0.0, 0.0, 1.0)
            return WallIntersectionResolver.resolve(listOf(horizontal, vertical), configured)
        }

        assertTrue(intersectionsAt(configured.maximumObservedExtentExtensionMeters - 1e-6).single().confidence > 0.0)
        assertEquals(emptyList<WallIntersection>(), intersectionsAt(configured.maximumObservedExtentExtensionMeters))
        assertEquals(emptyList<WallIntersection>(), intersectionsAt(configured.maximumObservedExtentExtensionMeters + 1e-6))
    }

    @Test
    fun `confidence uses supplied perpendicular boost without moving corner`() {
        val config = configured.copy(maximumPerpendicularConfidenceBoost = 0.075)
        val horizontal = wall("horizontal", PI / 2.0, 0.0, -1.0, 1.0, stability = 0.80)
        val vertical = wall("vertical", 0.0, 0.0, -1.0, 1.0, stability = 0.60)

        val result = WallIntersectionResolver.resolve(listOf(horizontal, vertical), config).single()

        assertEquals(PointXZ(0.0, 0.0), result.point)
        assertEquals(0.70 * 1.075, result.confidence, 1e-9)
    }

    @Test
    fun `non-stable candidates cannot create architectural corners`() {
        val stable = wall("stable", PI / 2.0, 0.0, -1.0, 1.0)
        val candidate = wall("candidate", 0.0, 0.0, -1.0, 1.0).copy(state = WallCandidateState.CANDIDATE)

        assertEquals(emptyList<WallIntersection>(), WallIntersectionResolver.resolve(listOf(stable, candidate), configured))
    }

    private fun wall(
        id: String,
        normalAngle: Double,
        offset: Double,
        start: Double,
        end: Double,
        stability: Double = 0.90,
    ): WallCandidate {
        val normal = VectorXZ(cos(normalAngle), sin(normalAngle))
        return WallCandidate(
            candidateId = id,
            unitNormal = normal,
            unitDirection = VectorXZ(-normal.z, normal.x),
            supportingLineOffsetMeters = offset,
            observedIntervals = listOf(ProjectionInterval(start, end)),
            observationCount = 5,
            firstObservedAtMillis = 0,
            lastObservedAtMillis = 1_000,
            angleStdDevRadians = 0.01,
            offsetStdDevMeters = 0.01,
            stability = stability,
            state = WallCandidateState.STABLE,
        )
    }

    private fun assertPointSet(expected: Set<PointXZ>, actual: List<PointXZ>) {
        assertEquals(expected.size, actual.size)
        expected.forEach { expectedPoint ->
            assertTrue(actual.any { point ->
                kotlin.math.abs(point.x - expectedPoint.x) < 1e-9 &&
                    kotlin.math.abs(point.z - expectedPoint.z) < 1e-9
            })
        }
    }
}
