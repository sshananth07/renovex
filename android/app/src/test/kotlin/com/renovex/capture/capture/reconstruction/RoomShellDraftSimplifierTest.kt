package com.renovex.capture.capture.reconstruction

import kotlin.math.hypot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomShellDraftSimplifierTest {
    @Test
    fun `redundant chamfer collapses only when provenance shows a much larger observed support`() {
        // Break caught: a short intersection artifact survives into RoomDraft
        // even though its candidate observed a much longer supporting line and
        // the neighboring supports meet at one nearby architectural corner.
        val fixture = chamferFixture(chamferObservedExtent = 1.0)

        val result = RoomShellDraftSimplifier.simplify(fixture.shell, fixture.candidates, configured)

        assertEquals(4, result.simplifiedShell.orderedCorners.size)
        assertEquals(listOf("chamfer"), result.collapsedCandidateIds)
        assertEquals(5, result.originalShell.orderedCorners.size)
        assertTrue(result.simplifiedShell.orderedCorners.contains(PointXZ(0.0, 0.0)))
    }

    @Test
    fun `genuine short return is preserved when observed and architectural extents agree`() {
        val fixture = chamferFixture(chamferObservedExtent = 0.071)

        val result = RoomShellDraftSimplifier.simplify(fixture.shell, fixture.candidates, configured)

        assertEquals(5, result.simplifiedShell.orderedCorners.size)
        assertTrue(result.collapsedCandidateIds.isEmpty())
    }

    @Test
    fun `replacement displacement follows configured below at and above boundary`() {
        val fixture = chamferFixture(chamferObservedExtent = 1.0)
        val displacement = hypot(0.05, 0.0)

        val below = RoomShellDraftSimplifier.simplify(
            fixture.shell,
            fixture.candidates,
            configured.copy(maximumReplacementCornerDisplacementMeters = displacement - 1e-6),
        )
        val at = RoomShellDraftSimplifier.simplify(
            fixture.shell,
            fixture.candidates,
            configured.copy(maximumReplacementCornerDisplacementMeters = displacement),
        )
        val above = RoomShellDraftSimplifier.simplify(
            fixture.shell,
            fixture.candidates,
            configured.copy(maximumReplacementCornerDisplacementMeters = displacement + 1e-6),
        )

        assertEquals(5, below.simplifiedShell.orderedCorners.size)
        assertEquals(4, at.simplifiedShell.orderedCorners.size)
        assertEquals(4, above.simplifiedShell.orderedCorners.size)
    }

    private fun chamferFixture(chamferObservedExtent: Double): Fixture {
        val corners = listOf(
            PointXZ(4.0, 0.0),
            PointXZ(4.0, 3.0),
            PointXZ(0.0, 3.0),
            PointXZ(0.0, 0.05),
            PointXZ(0.05, 0.0),
        )
        val edgeIds = listOf("right", "top", "left", "chamfer", "bottom")
        val edgeCandidates = corners.indices.map { index ->
            candidate(edgeIds[index], corners[index], corners[(index + 1) % corners.size],
                if (edgeIds[index] == "chamfer") chamferObservedExtent else null)
        }
        // Shell candidate i produces corner i with candidate i+1, so rotate
        // edge IDs back once to match RoomShell's representation.
        val orderedCandidateIds = listOf("bottom", "right", "top", "left", "chamfer")
        return Fixture(
            RoomShell(
                orderedCandidateIds,
                corners,
                orderedCandidateIds.associateWith { 1.0 },
                0.9,
            ),
            edgeCandidates,
        )
    }

    private fun candidate(
        id: String,
        start: PointXZ,
        end: PointXZ,
        observedExtentOverride: Double?,
    ): WallCandidate {
        val dx = end.x - start.x
        val dz = end.z - start.z
        val length = hypot(dx, dz)
        val direction = VectorXZ(dx / length, dz / length)
        val normal = VectorXZ(direction.z, -direction.x)
        val offset = normal.x * start.x + normal.z * start.z
        val startProjection = direction.x * start.x + direction.z * start.z
        val observedLength = observedExtentOverride ?: length
        return WallCandidate(
            candidateId = id,
            unitNormal = normal,
            unitDirection = direction,
            supportingLineOffsetMeters = offset,
            observedIntervals = listOf(ProjectionInterval(startProjection, startProjection + observedLength)),
            observationCount = 5,
            firstObservedAtMillis = 0,
            lastObservedAtMillis = 1_000,
            angleStdDevRadians = 0.01,
            offsetStdDevMeters = 0.01,
            stability = 0.9,
            state = WallCandidateState.STABLE,
        )
    }

    private data class Fixture(val shell: RoomShell, val candidates: List<WallCandidate>)

    private val configured = RoomShellSimplificationConfig(
        maximumCollapsibleEdgeLengthMeters = 0.10,
        minimumObservedToArchitecturalExtentRatio = 4.0,
        maximumReplacementCornerDisplacementMeters = 0.05,
    )
}
