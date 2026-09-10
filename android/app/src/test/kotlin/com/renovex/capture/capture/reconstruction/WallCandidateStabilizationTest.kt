package com.renovex.capture.capture.reconstruction

import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.sin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WallCandidateStabilizationTest {

    private val baseConfig = WallTrackingConfig(
        maxAngleDifferenceRadians = 0.45,
        maxSupportingLineOffsetMeters = 1.0,
        maxExtentGapMeters = 1.10,
        minStandaloneExtentMeters = 0.20,
        stableObservationCount = 5,
        stableDurationMillis = 800,
        maxStableAngleStdDevRadians = 0.08,
        maxStableOffsetStdDevMeters = 0.09,
        directionSmoothingAlpha = 0.25,
        offsetSmoothingAlpha = 0.25,
        matchAngleWeight = 0.37,
        matchOffsetWeight = 0.29,
        matchExtentWeight = 0.21,
        matchHistoryWeight = 0.13,
        stabilityCountWeight = 0.40,
        stabilityDurationWeight = 0.30,
        stabilityAngleWeight = 0.20,
        stabilityOffsetWeight = 0.10,
    )

    @Test
    fun `direction and offset use supplied smoothing alphas`() {
        val tracker = tracker(baseConfig)
        val angle = 0.20
        tracker.accept(observation(timestamp = 0, offset = 0.0))
        tracker.accept(observation(timestamp = 100, angle = angle, offset = 0.40))

        val candidate = tracker.snapshot().single()
        val rawX = baseConfig.directionSmoothingAlpha * -sin(angle)
        val rawZ = (1.0 - baseConfig.directionSmoothingAlpha) +
            baseConfig.directionSmoothingAlpha * cos(angle)
        val length = hypot(rawX, rawZ)
        assertEquals(rawX / length, candidate.unitNormal.x, 1e-9)
        assertEquals(rawZ / length, candidate.unitNormal.z, 1e-9)
        assertEquals(0.10, candidate.supportingLineOffsetMeters, 1e-9)
    }

    @Test
    fun `short revisit cannot shrink accumulated observed extent`() {
        val tracker = tracker(baseConfig)
        tracker.accept(observation(timestamp = 0, start = 0.0, end = 4.0))
        tracker.accept(observation(timestamp = 100, start = 1.0, end = 2.0))

        val candidate = tracker.snapshot().single()
        assertEquals(0.0, candidate.observedStartProjectionMeters, 1e-9)
        assertEquals(4.0, candidate.observedEndProjectionMeters, 1e-9)
        assertEquals(listOf(ProjectionInterval(0.0, 4.0)), candidate.observedIntervals)
    }

    @Test
    fun `outer extent expands while an open doorway remains two evidence intervals`() {
        val tracker = tracker(baseConfig)
        tracker.accept(observation(timestamp = 0, start = 0.0, end = 1.5))
        tracker.accept(observation(timestamp = 100, start = 2.5, end = 4.0))

        val candidate = tracker.snapshot().single()
        assertEquals(0.0, candidate.observedStartProjectionMeters, 1e-9)
        assertEquals(4.0, candidate.observedEndProjectionMeters, 1e-9)
        assertEquals(
            listOf(ProjectionInterval(0.0, 1.5), ProjectionInterval(2.5, 4.0)),
            candidate.observedIntervals,
        )
    }

    @Test
    fun `sub-minimum evidence updates a matching candidate`() {
        val tracker = tracker(baseConfig)
        tracker.accept(observation(timestamp = 0, start = 0.0, end = 1.0))
        tracker.accept(observation(timestamp = 100, start = 0.90, end = 1.05))

        val candidate = tracker.snapshot().single()
        assertEquals(2, candidate.observationCount)
        assertEquals(1.05, candidate.observedEndProjectionMeters, 1e-9)
    }

    @Test
    fun `stability count uses configured boundary below at and above`() {
        val config = permissiveStability().copy(stableObservationCount = 3)
        val tracker = tracker(config)

        tracker.accept(observation(timestamp = 0))
        tracker.accept(observation(timestamp = 1))
        assertEquals(WallCandidateState.CANDIDATE, tracker.snapshot().single().state)

        tracker.accept(observation(timestamp = 2))
        assertEquals(WallCandidateState.STABLE, tracker.snapshot().single().state)

        tracker.accept(observation(timestamp = 3))
        assertEquals(WallCandidateState.STABLE, tracker.snapshot().single().state)
    }

    @Test
    fun `stability duration uses configured boundary below at and above`() {
        val config = permissiveStability().copy(stableObservationCount = 2, stableDurationMillis = 100)

        assertEquals(WallCandidateState.CANDIDATE, twoObservationState(config, secondTimestamp = 99))
        assertEquals(WallCandidateState.STABLE, twoObservationState(config, secondTimestamp = 100))
        assertEquals(WallCandidateState.STABLE, twoObservationState(config, secondTimestamp = 101))
    }

    @Test
    fun `angular deviation uses configured boundary below at and above`() {
        val threshold = 0.05
        val config = permissiveStability().copy(
            stableObservationCount = 2,
            maxStableAngleStdDevRadians = threshold,
        )

        assertEquals(WallCandidateState.STABLE, twoObservationState(config, angle = 2.0 * threshold - 1e-6))
        assertEquals(WallCandidateState.STABLE, twoObservationState(config, angle = 2.0 * threshold))
        assertEquals(WallCandidateState.CANDIDATE, twoObservationState(config, angle = 2.0 * threshold + 1e-6))
    }

    @Test
    fun `offset deviation uses configured boundary below at and above`() {
        val threshold = 0.05
        val config = permissiveStability().copy(
            stableObservationCount = 2,
            maxStableOffsetStdDevMeters = threshold,
        )

        assertEquals(WallCandidateState.STABLE, twoObservationState(config, offset = 2.0 * threshold - 1e-6))
        assertEquals(WallCandidateState.STABLE, twoObservationState(config, offset = 2.0 * threshold))
        assertEquals(WallCandidateState.CANDIDATE, twoObservationState(config, offset = 2.0 * threshold + 1e-6))
    }

    @Test
    fun `stability is configured weighted evidence and cannot hide a failed hard gate`() {
        val config = baseConfig.copy(
            stableObservationCount = 4,
            stableDurationMillis = 100,
            maxStableAngleStdDevRadians = 0.20,
            maxStableOffsetStdDevMeters = 0.20,
        )
        val tracker = tracker(config)
        tracker.accept(observation(timestamp = 0))
        tracker.accept(observation(timestamp = 50))

        val candidate = tracker.snapshot().single()
        assertEquals(0.65, candidate.stability, 1e-9)
        assertEquals(WallCandidateState.CANDIDATE, candidate.state)
    }

    @Test
    fun `stable candidate never demotes during the active scan`() {
        val config = permissiveStability().copy(
            stableObservationCount = 2,
            maxStableOffsetStdDevMeters = 0.02,
        )
        val tracker = tracker(config)
        tracker.accept(observation(timestamp = 0))
        tracker.accept(observation(timestamp = 10))
        assertEquals(WallCandidateState.STABLE, tracker.snapshot().single().state)

        tracker.accept(observation(timestamp = 20, offset = 0.20))
        val afterNoise = tracker.snapshot().single()
        assertTrue(afterNoise.offsetStdDevMeters > config.maxStableOffsetStdDevMeters)
        assertEquals(WallCandidateState.STABLE, afterNoise.state)
    }

    private fun permissiveStability(): WallTrackingConfig = baseConfig.copy(
        stableObservationCount = 1,
        stableDurationMillis = 0,
        maxStableAngleStdDevRadians = 1.0,
        maxStableOffsetStdDevMeters = 1.0,
    )

    private fun twoObservationState(
        config: WallTrackingConfig,
        secondTimestamp: Long = 10,
        angle: Double = 0.0,
        offset: Double = 0.0,
    ): WallCandidateState {
        val tracker = tracker(config)
        tracker.accept(observation(timestamp = 0))
        tracker.accept(observation(timestamp = secondTimestamp, angle = angle, offset = offset))
        return tracker.snapshot().single().state
    }

    private fun tracker(config: WallTrackingConfig): WallCandidateTracker =
        WallCandidateTracker(config) { "candidate" }

    private fun observation(
        timestamp: Long,
        angle: Double = 0.0,
        offset: Double = 0.0,
        start: Double = 0.0,
        end: Double = 2.0,
    ): WallObservation = WallObservation(
        canonicalPlaneId = "plane-$timestamp-$angle-$offset-$start-$end",
        unitNormal = VectorXZ(-sin(angle), cos(angle)),
        unitDirection = VectorXZ(-cos(angle), -sin(angle)),
        supportingLineOffsetMeters = offset,
        observedStartProjectionMeters = start,
        observedEndProjectionMeters = end,
        timestampMillis = timestamp,
        trackingState = ObservationTrackingState.TRACKING,
    )
}
