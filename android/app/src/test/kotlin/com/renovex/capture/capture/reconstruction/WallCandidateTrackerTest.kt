package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Test

class WallCandidateTrackerTest {

    private val configured = WallTrackingConfig(
        maxAngleDifferenceRadians = 0.12,
        maxSupportingLineOffsetMeters = 0.18,
        maxExtentGapMeters = 0.25,
        minStandaloneExtentMeters = 0.20,
        stableObservationCount = 6,
        stableDurationMillis = 750,
        maxStableAngleStdDevRadians = 0.03,
        maxStableOffsetStdDevMeters = 0.04,
        directionSmoothingAlpha = 0.20,
        offsetSmoothingAlpha = 0.20,
        matchAngleWeight = 0.45,
        matchOffsetWeight = 0.30,
        matchExtentWeight = 0.15,
        matchHistoryWeight = 0.10,
    )

    @Test
    fun `fragmented observations of one supporting wall accumulate into one candidate`() {
        val tracker = tracker()

        tracker.accept(observation("plane-a", 0.0, 1.5, 100))
        tracker.accept(observation("plane-b", 1.65, 3.0, 200))

        val result = tracker.snapshot()
        assertEquals(1, result.size)
        assertEquals(2, result.single().observationCount)
        assertEquals(0.0, result.single().observedStartProjectionMeters, 1e-9)
        assertEquals(3.0, result.single().observedEndProjectionMeters, 1e-9)
    }

    @Test
    fun `one bridge observation updates one candidate without merging two candidates`() {
        val tracker = tracker()
        tracker.accept(observation("plane-a", 0.0, 1.0, 100))
        tracker.accept(observation("plane-b", 1.50, 2.50, 100))
        assertEquals(2, tracker.snapshot().size)

        tracker.accept(observation("bridge", 0.90, 1.60, 200))

        val result = tracker.snapshot()
        assertEquals(2, result.size)
        assertEquals(listOf(1, 2), result.map { it.observationCount }.sorted())
    }

    @Test
    fun `same timestamp processing is deterministic by canonical plane id`() {
        val forward = tracker()
        val reverse = tracker()
        val observations = listOf(
            observation("plane-z", 0.0, 1.0, 500),
            observation("plane-a", 1.10, 2.0, 500),
        )

        forward.acceptAll(observations)
        reverse.acceptAll(observations.reversed())

        assertEquals(forward.snapshot(), reverse.snapshot())
    }

    @Test
    fun `observation shorter than configured standalone extent cannot create a candidate`() {
        val tracker = tracker()

        tracker.accept(observation("too-short", 0.0, configured.minStandaloneExtentMeters - 1e-6, 100))
        assertEquals(emptyList<WallCandidate>(), tracker.snapshot())

        tracker.accept(observation("at-boundary", 0.0, configured.minStandaloneExtentMeters, 200))
        assertEquals(1, tracker.snapshot().size)
    }

    @Test
    fun `diagnostic lineage groups contributing observations by canonical plane`() {
        // Break caught: retaining only candidate counts makes it impossible to
        // trace a selected architectural edge back to canonical plane evidence.
        val tracker = tracker()
        tracker.accept(observation("plane-a", 0.0, 1.5, 100))
        tracker.accept(observation("plane-b", 1.60, 3.0, 200))
        tracker.accept(observation("plane-a", 0.0, 1.6, 300))

        val sources = tracker.observationSourcesByCandidate().getValue("candidate-1")

        assertEquals(
            listOf(
                WallObservationSourceDiagnostics("plane-a", 2, 100, 300),
                WallObservationSourceDiagnostics("plane-b", 1, 200, 200),
            ),
            sources,
        )
    }

    private fun tracker(): WallCandidateTracker {
        var nextId = 0
        return WallCandidateTracker(configured) { "candidate-${++nextId}" }
    }

    private fun observation(
        planeId: String,
        start: Double,
        end: Double,
        timestamp: Long,
    ): WallObservation = WallObservation(
        canonicalPlaneId = planeId,
        unitNormal = VectorXZ(0.0, 1.0),
        unitDirection = VectorXZ(1.0, 0.0),
        supportingLineOffsetMeters = 2.0,
        observedStartProjectionMeters = start,
        observedEndProjectionMeters = end,
        timestampMillis = timestamp,
        trackingState = ObservationTrackingState.TRACKING,
    )
}
