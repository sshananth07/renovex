package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class WallObservationFactoryTest {

    @Test
    fun `wall observation preserves canonical supporting-line evidence exactly`() {
        // Break caught: candidate input accidentally falls back to raw plane
        // identity or recomputes direction/extent from polygon endpoints.
        val plane = PlaneObservation(
            planeId = "child-plane",
            canonicalPlaneId = "parent-plane",
            center = PointXZ(2.0, 0.08),
            worldPolygon = listOf(PointXZ(0.0, 0.08), PointXZ(4.0, 0.08)),
            unitNormal = VectorXZ(0.0, 1.0),
            unitDirection = VectorXZ(-1.0, 0.0),
            supportingLineOffsetMeters = 0.08,
            observedStartProjectionMeters = -4.0,
            observedEndProjectionMeters = 0.0,
            timestampMillis = 600,
            trackingState = ObservationTrackingState.PAUSED,
        )

        val observation = WallObservationFactory.fromPlane(plane)

        assertEquals("parent-plane", observation.canonicalPlaneId)
        assertEquals(VectorXZ(0.0, 1.0), observation.unitNormal)
        assertEquals(VectorXZ(-1.0, 0.0), observation.unitDirection)
        assertEquals(0.08, observation.supportingLineOffsetMeters, 0.0)
        assertEquals(-4.0, observation.observedStartProjectionMeters, 0.0)
        assertEquals(0.0, observation.observedEndProjectionMeters, 0.0)
        assertEquals(600, observation.timestampMillis)
        assertEquals(ObservationTrackingState.PAUSED, observation.trackingState)
    }

    @Test
    fun `repeated updates remain timestamped evidence rather than one architectural wall`() {
        // Break caught: a factory-level dedupe would erase temporal history
        // before the stateful candidate tracker can evaluate consistency.
        val first = WallObservationFactory.fromPlane(planeAt(timestampMillis = 0, end = 2.0))
        val later = WallObservationFactory.fromPlane(planeAt(timestampMillis = 600, end = 4.0))

        assertNotEquals(first, later)
        assertEquals(0, first.timestampMillis)
        assertEquals(600, later.timestampMillis)
        assertEquals(-2.0, first.observedStartProjectionMeters, 0.0)
        assertEquals(-4.0, later.observedStartProjectionMeters, 0.0)
    }

    private fun planeAt(timestampMillis: Long, end: Double): PlaneObservation = PlaneObservation(
        planeId = "plane",
        canonicalPlaneId = "plane",
        center = PointXZ(end / 2.0, 0.0),
        worldPolygon = listOf(PointXZ(0.0, 0.0), PointXZ(end, 0.0)),
        unitNormal = VectorXZ(0.0, 1.0),
        unitDirection = VectorXZ(-1.0, 0.0),
        supportingLineOffsetMeters = 0.0,
        observedStartProjectionMeters = -end,
        observedEndProjectionMeters = 0.0,
        timestampMillis = timestampMillis,
        trackingState = ObservationTrackingState.TRACKING,
    )
}
