package com.renovex.capture.capture.reconstruction

import kotlin.math.cos
import kotlin.math.sin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class WallCandidateMatcherTest {

    private val configured = WallTrackingConfig(
        maxAngleDifferenceRadians = 0.20,
        maxSupportingLineOffsetMeters = 0.30,
        maxExtentGapMeters = 0.40,
        minStandaloneExtentMeters = 0.15,
        stableObservationCount = 7,
        stableDurationMillis = 900,
        maxStableAngleStdDevRadians = 0.04,
        maxStableOffsetStdDevMeters = 0.05,
        directionSmoothingAlpha = 0.33,
        offsetSmoothingAlpha = 0.27,
        matchAngleWeight = 0.61,
        matchOffsetWeight = 0.19,
        matchExtentWeight = 0.13,
        matchHistoryWeight = 0.07,
    )

    @Test
    fun `angle gate follows supplied value below at and above the boundary`() {
        val candidate = candidate("wall", observation())

        assertNotNull(match(rotated(configured.maxAngleDifferenceRadians - 1e-6), candidate))
        assertNotNull(match(rotated(configured.maxAngleDifferenceRadians), candidate))
        assertNull(match(rotated(configured.maxAngleDifferenceRadians + 1e-6), candidate))
    }

    @Test
    fun `supporting line gate follows supplied value below at and above the boundary`() {
        val candidate = candidate("wall", observation())

        assertNotNull(match(observation(offset = configured.maxSupportingLineOffsetMeters - 1e-6), candidate))
        assertNotNull(match(observation(offset = configured.maxSupportingLineOffsetMeters), candidate))
        assertNull(match(observation(offset = configured.maxSupportingLineOffsetMeters + 1e-6), candidate))
    }

    @Test
    fun `extent gap gate follows supplied value below at and above the boundary`() {
        val candidate = candidate("wall", observation(start = 0.0, end = 2.0))

        assertNotNull(match(observation(start = 2.0 + configured.maxExtentGapMeters - 1e-6, end = 4.0), candidate))
        assertNotNull(match(observation(start = 2.0 + configured.maxExtentGapMeters, end = 4.0), candidate))
        assertNull(match(observation(start = 2.0 + configured.maxExtentGapMeters + 1e-6, end = 4.0), candidate))
    }

    @Test
    fun `configured scoring weights select the best supporting wall`() {
        val incoming = observation()
        val angleCloser = candidate("angle", rotated(0.02, offset = 0.24))
        val offsetCloser = candidate("offset", rotated(0.14, offset = 0.01))

        val angleDominant = configured.copy(
            matchAngleWeight = 0.90,
            matchOffsetWeight = 0.05,
            matchExtentWeight = 0.03,
            matchHistoryWeight = 0.02,
        )
        val offsetDominant = configured.copy(
            matchAngleWeight = 0.05,
            matchOffsetWeight = 0.90,
            matchExtentWeight = 0.03,
            matchHistoryWeight = 0.02,
        )

        assertEquals("angle", WallCandidateMatcher.bestMatch(incoming, listOf(angleCloser, offsetCloser), angleDominant)?.candidateId)
        assertEquals("offset", WallCandidateMatcher.bestMatch(incoming, listOf(angleCloser, offsetCloser), offsetDominant)?.candidateId)
    }

    @Test
    fun `equal scores use candidate id as a deterministic tie break`() {
        val base = observation()
        val laterId = candidate("wall-z", base)
        val earlierId = candidate("wall-a", base)

        assertEquals(
            "wall-a",
            WallCandidateMatcher.bestMatch(base, listOf(laterId, earlierId), configured)?.candidateId,
        )
    }

    private fun match(incoming: WallObservation, candidate: WallCandidate): WallCandidateMatch? =
        WallCandidateMatcher.bestMatch(incoming, listOf(candidate), configured)

    private fun candidate(id: String, seed: WallObservation): WallCandidate =
        WallCandidate.fromObservation(id, seed)

    private fun rotated(angle: Double, offset: Double = 0.0): WallObservation =
        observation(
            normal = VectorXZ(-sin(angle), cos(angle)),
            direction = VectorXZ(cos(angle), sin(angle)),
            offset = offset,
        )

    private fun observation(
        normal: VectorXZ = VectorXZ(0.0, 1.0),
        direction: VectorXZ = VectorXZ(1.0, 0.0),
        offset: Double = 0.0,
        start: Double = 0.0,
        end: Double = 2.0,
        timestamp: Long = 1_000,
        planeId: String = "plane",
    ): WallObservation = WallObservation(
        canonicalPlaneId = planeId,
        unitNormal = normal,
        unitDirection = direction,
        supportingLineOffsetMeters = offset,
        observedStartProjectionMeters = start,
        observedEndProjectionMeters = end,
        timestampMillis = timestamp,
        trackingState = ObservationTrackingState.TRACKING,
    )
}
