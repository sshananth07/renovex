package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ScanFixtureLoaderTest {

    private val requiredFixtures = listOf(
        "simple-rectangular-room.json",
        "fragmented-single-wall.json",
        "same-wall-rotated.json",
        "same-wall-offset.json",
        "plane-subsumption.json",
        "partial-room-scan.json",
        "l-shaped-room.json",
        "angled-wall-room.json",
        "noisy-corner.json",
        "revisited-wall.json",
        "open-doorway.json",
        "incomplete-shell.json",
    )

    @Test
    fun `all architectural replay fixtures load as finite ordered frames`() {
        // Break caught: a fixture is missing, malformed, time-reordered, or
        // admits non-finite geometry that would poison later reconstruction.
        requiredFixtures.forEach { resourceName ->
            val fixture = ScanFixtureLoader.load(resourceName)

            assertTrue("$resourceName must have a descriptive name", fixture.name.isNotBlank())
            assertTrue("$resourceName must contain replay evidence", fixture.frames.isNotEmpty())
            assertEquals(
                "$resourceName frames must be chronological",
                fixture.frames.map { it.timestampMillis }.sorted(),
                fixture.frames.map { it.timestampMillis },
            )
            fixture.frames.forEach { frame ->
                assertTrue(frame.cameraPosition.x.isFinite())
                assertTrue(frame.cameraPosition.z.isFinite())
                assertTrue(frame.cameraHeadingRadians.isFinite())
                frame.planes.forEach { plane ->
                    assertTrue(plane.center.x.isFinite())
                    assertTrue(plane.center.z.isFinite())
                    assertTrue(plane.worldNormal.x.isFinite())
                    assertTrue(plane.worldNormal.z.isFinite())
                    assertTrue(plane.worldPolygon.size >= 2)
                    assertTrue(plane.worldPolygon.all { it.x.isFinite() && it.z.isFinite() })
                }
            }
        }
    }

    @Test
    fun `plane subsumption fixture preserves child to parent evidence`() {
        // Break caught: the replay schema drops subsumedByPlaneId, making the
        // canonicalization regression impossible to reproduce deterministically.
        val fixture = ScanFixtureLoader.load("plane-subsumption.json")
        val updates = fixture.frames.flatMap { it.planes }

        assertEquals(listOf(0L, 300L, 600L), fixture.frames.map { it.timestampMillis })
        assertTrue(updates.any { it.planeId == "plane-child" && it.subsumedByPlaneId == "plane-parent" })
        assertTrue(updates.any { it.planeId == "plane-parent" })
    }

    @Test
    fun `ordinary plane update keeps absent subsumption as null`() {
        val fixture = ScanFixtureLoader.load("fragmented-single-wall.json")
        assertNull(fixture.frames.first().planes.first().subsumedByPlaneId)
    }

    @Test
    fun `loader rejects non-finite geometry explicitly`() {
        // Break caught: enabling permissive JSON floating-point parsing without
        // a domain validation pass would allow NaN into every later score.
        assertThrows(IllegalArgumentException::class.java) {
            ScanFixtureLoader.decode(
                """
                {
                  "name": "invalid-non-finite",
                  "frames": [{
                    "timestampMillis": 0,
                    "cameraPosition": {"x": NaN, "z": 0.0},
                    "cameraHeadingRadians": 0.0,
                    "planes": []
                  }]
                }
                """.trimIndent(),
            )
        }
    }
}
