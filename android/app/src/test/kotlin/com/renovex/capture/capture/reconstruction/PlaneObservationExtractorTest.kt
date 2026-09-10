package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PlaneObservationExtractorTest {

    @Test
    fun `wall direction comes from normal while irregular polygon controls only extent`() {
        // Break caught: restoring the old farthest-polygon-pair heuristic
        // would rotate this wall toward the polygon's unstable diagonal.
        val first = extract(
            normal = VectorXZ(0.0, 1.0),
            polygon = listOf(
                PointXZ(0.0, 2.20),
                PointXZ(4.0, 1.80),
                PointXZ(3.7, 2.25),
                PointXZ(0.2, 1.95),
            ),
        )
        val changedBoundary = extract(
            normal = VectorXZ(0.0, 1.0),
            polygon = listOf(
                PointXZ(0.5, 1.70),
                PointXZ(3.5, 2.35),
                PointXZ(3.9, 1.90),
                PointXZ(0.1, 2.30),
            ),
        )

        requireNotNull(first)
        requireNotNull(changedBoundary)
        assertEquals(0.0, first.unitNormal.x, 1e-9)
        assertEquals(1.0, first.unitNormal.z, 1e-9)
        assertEquals(-1.0, first.unitDirection.x, 1e-9)
        assertEquals(0.0, first.unitDirection.z, 1e-9)
        assertEquals(first.unitDirection, changedBoundary.unitDirection)
        assertEquals(-4.0, first.observedStartProjectionMeters, 1e-9)
        assertEquals(0.0, first.observedEndProjectionMeters, 1e-9)
        assertEquals(-3.9, changedBoundary.observedStartProjectionMeters, 1e-9)
        assertEquals(-0.1, changedBoundary.observedEndProjectionMeters, 1e-9)
    }

    @Test
    fun `reversed normal produces the same deterministic supporting line`() {
        // Break caught: averaging opposite ARCore normal signs would cancel
        // direction and flip the signed line offset between frames.
        val forward = extract(VectorXZ(0.0, 1.0), wallPolygon())
        val reversed = extract(VectorXZ(0.0, -1.0), wallPolygon())

        requireNotNull(forward)
        requireNotNull(reversed)
        assertEquals(forward.unitNormal, reversed.unitNormal)
        assertEquals(forward.unitDirection, reversed.unitDirection)
        assertEquals(forward.supportingLineOffsetMeters, reversed.supportingLineOffsetMeters, 1e-9)
        assertEquals(forward.observedStartProjectionMeters, reversed.observedStartProjectionMeters, 1e-9)
        assertEquals(forward.observedEndProjectionMeters, reversed.observedEndProjectionMeters, 1e-9)
    }

    @Test
    fun `supporting line offset and projected extent are derived exactly`() {
        val observation = extract(
            normal = VectorXZ(1.0, 0.0),
            polygon = listOf(PointXZ(4.0, -1.0), PointXZ(4.0, 3.0), PointXZ(4.0, 1.5)),
            center = PointXZ(4.0, 1.0),
        )

        requireNotNull(observation)
        assertEquals(4.0, observation.supportingLineOffsetMeters, 1e-9)
        assertEquals(-1.0, observation.observedStartProjectionMeters, 1e-9)
        assertEquals(3.0, observation.observedEndProjectionMeters, 1e-9)
        assertEquals(3, observation.worldPolygon.size)
    }

    @Test
    fun `degenerate or invalid plane evidence is rejected`() {
        assertNull(extract(VectorXZ(0.0, 0.0), wallPolygon()))
        assertNull(extract(VectorXZ(Double.NaN, 1.0), wallPolygon()))
        assertNull(extract(VectorXZ(0.0, 1.0), listOf(PointXZ(0.0, 0.0))))
        assertNull(extract(VectorXZ(0.0, 1.0), listOf(PointXZ(0.0, 0.0), PointXZ(Double.NaN, 0.0))))
    }

    @Test
    fun `fixture polygon jitter cannot change extracted wall direction`() {
        val fixture = ScanFixtureLoader.load("same-wall-rotated.json")
        val extracted = fixture.frames.map { frame ->
            val plane = frame.planes.single()
            PlaneObservationExtractor.extract(
                CanonicalPlaneInput(
                    planeId = plane.planeId,
                    canonicalPlaneId = plane.planeId,
                    center = plane.center,
                    worldNormal = VectorXZ(0.0, 1.0),
                    worldPolygon = plane.worldPolygon,
                    timestampMillis = frame.timestampMillis,
                    trackingState = plane.trackingState,
                ),
            )
        }

        assertTrue(extracted.all { it?.unitDirection == VectorXZ(-1.0, 0.0) })
    }

    private fun extract(
        normal: VectorXZ,
        polygon: List<PointXZ>,
        center: PointXZ = PointXZ(2.0, 2.0),
    ): PlaneObservation? = PlaneObservationExtractor.extract(
        CanonicalPlaneInput(
            planeId = "plane-raw",
            canonicalPlaneId = "plane-canonical",
            center = center,
            worldNormal = normal,
            worldPolygon = polygon,
            timestampMillis = 123,
            trackingState = ObservationTrackingState.TRACKING,
        ),
    )

    private fun wallPolygon(): List<PointXZ> =
        listOf(PointXZ(0.0, 2.0), PointXZ(4.0, 2.0))
}
