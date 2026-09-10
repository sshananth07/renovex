package com.renovex.capture.capture.reconstruction

import com.renovex.capture.ui.screens.scan.ScanReconstructionController
import com.renovex.capture.ui.screens.scan.ScanReconstructionState
import kotlin.math.abs
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ScanReconstructionReplayTest {

    @Test
    fun `simple rectangle reconstructs four stable walls intersections and one shell`() {
        val state = replay("simple-rectangular-room.json")

        assertEquals(4, state.candidates.size)
        assertEquals(4, state.diagnostics.stableWallCount)
        assertEquals(4, state.intersections.size)
        val shell = (state.shellResult as RoomShellBuildResult.Complete).shell
        assertEquals(4, shell.orderedCandidateIds.size)
        assertEquals(4, shell.orderedCorners.size)
    }

    @Test
    fun `fragmented rotated offset and revisited wall fixtures converge by supporting wall`() {
        listOf(
            "fragmented-single-wall.json",
            "same-wall-rotated.json",
            "same-wall-offset.json",
            "revisited-wall.json",
        ).forEach { fixtureName ->
            val state = replay(fixtureName)
            assertEquals("$fixtureName candidates", 1, state.candidates.size)
            assertEquals("$fixtureName stable", WallCandidateState.STABLE, state.candidates.single().state)
        }
    }

    @Test
    fun `plane subsumption leaves one canonical contribution and one wall`() {
        val state = replay("plane-subsumption.json")

        assertEquals(2, state.diagnostics.rawVerticalPlaneCount)
        assertEquals(1, state.diagnostics.canonicalPlaneCount)
        assertEquals(1, state.candidates.size)
    }

    @Test
    fun `l shaped and angled rooms retain their supported irregular geometry`() {
        val lShape = replay("l-shaped-room.json")
        val lShell = (lShape.shellResult as RoomShellBuildResult.Complete).shell
        assertEquals(6, lShape.diagnostics.stableWallCount)
        assertEquals(6, lShell.orderedCandidateIds.size)
        assertEquals(6, lShell.orderedCorners.size)

        val angled = replay("angled-wall-room.json")
        val angledShell = (angled.shellResult as RoomShellBuildResult.Complete).shell
        assertEquals(4, angledShell.orderedCandidateIds.size)
        assertTrue(angled.candidates.any { candidate ->
            abs(candidate.unitDirection.x) > 0.1 && abs(candidate.unitDirection.z) > 0.1
        })
        assertTrue(angledShell.orderedCorners.any { abs(it.x - 5.0) < 1e-3 && abs(it.z - 3.0) < 1e-3 })
    }

    @Test
    fun `partial and incomplete scans never fabricate closure`() {
        listOf("partial-room-scan.json", "incomplete-shell.json").forEach { fixtureName ->
            val state = replay(fixtureName)
            assertTrue("$fixtureName must stay incomplete", state.shellResult is RoomShellBuildResult.Incomplete)
        }
    }

    @Test
    fun `open doorway stays one wall with two observed evidence intervals`() {
        val state = replay("open-doorway.json")

        assertEquals(1, state.candidates.size)
        assertEquals(2, state.candidates.single().observedIntervals.size)
        assertTrue(state.shellResult is RoomShellBuildResult.Incomplete)
    }

    @Test
    fun `noisy corner resolves one plausible architectural intersection`() {
        val state = replay("noisy-corner.json")

        assertEquals(2, state.diagnostics.stableWallCount)
        assertEquals(1, state.intersections.size)
        assertTrue(state.shellResult is RoomShellBuildResult.Incomplete)
    }

    @Test
    fun `xiaomi object sized closed loop retains wall evidence but is not proposed as a room`() {
        // Break caught: selecting any stable, high-coverage closed cycle as a room
        // allows nearby furniture surfaces to become a high-confidence RoomDraft.
        val state = replay("xiaomi-11t-object-loop.json")

        assertEquals(4, state.diagnostics.stableWallCount)
        assertEquals(4, state.candidates.size)
        assertTrue(state.intersections.size >= 4)
        assertTrue(state.shellResult is RoomShellBuildResult.Incomplete)
    }

    private fun replay(fixtureName: String): ScanReconstructionState {
        val fixture = ScanFixtureLoader.load(fixtureName)
        val canonicalizer = PlaneCanonicalizer()
        val controller = ScanReconstructionController()
        var state: ScanReconstructionState? = null
        fixture.frames.forEach { frame ->
            frame.planes.forEach { plane ->
                canonicalizer.canonicalIdFor(
                    PlaneIdentityUpdate(plane.planeId, plane.subsumedByPlaneId),
                )
            }
            val observations = frame.planes.mapNotNull { plane ->
                PlaneObservationExtractor.extract(
                    CanonicalPlaneInput(
                        planeId = plane.planeId,
                        canonicalPlaneId = canonicalizer.canonicalIdFor(PlaneIdentityUpdate(plane.planeId, null)),
                        center = plane.center,
                        worldNormal = plane.worldNormal,
                        worldPolygon = plane.worldPolygon,
                        timestampMillis = frame.timestampMillis,
                        trackingState = plane.trackingState,
                    ),
                )
            }
            state = controller.update(
                observations,
                frame.cameraPosition,
                frame.cameraHeadingRadians,
            )
        }
        return requireNotNull(state)
    }
}
