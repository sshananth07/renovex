package com.renovex.capture.capture.reconstruction

import com.renovex.capture.geometry.RoomDraft
import com.renovex.capture.geometry.CeilingHeightProposal
import kotlin.math.abs
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomShellToRoomDraftMapperTest {

    @Test
    fun `rectangle maps exact winding closed topology and stable ids`() {
        val shell = shell(
            ids = listOf("bottom", "right", "top", "left"),
            corners = listOf(
                PointXZ(4.0, 0.0), PointXZ(4.0, 3.0),
                PointXZ(0.0, 3.0), PointXZ(0.0, 0.0),
            ),
        )

        val draft = map(shell)

        assertEquals(shell.orderedCorners.mapIndexed { index, point -> Triple("corner-$index", point.x, point.z) }, draft.corners.map { Triple(it.id, it.x, it.z) })
        assertClosedTopology(draft)
        assertEquals(listOf("wall-right", "wall-top", "wall-left", "wall-bottom"), draft.walls.map { it.id })
        assertTrue(draft.walls.all { it.heightMeters == 2.73 })
    }

    @Test
    fun `l shaped shell remains six-corner concave draft`() {
        val points = listOf(
            PointXZ(4.0, 0.0), PointXZ(4.0, 2.0), PointXZ(2.0, 2.0),
            PointXZ(2.0, 4.0), PointXZ(0.0, 4.0), PointXZ(0.0, 0.0),
        )
        val draft = map(shell((0 until 6).map { "l-$it" }, points))

        assertEquals(6, draft.corners.size)
        assertEquals(points.map { it.x to it.z }, draft.corners.map { it.x to it.z })
        assertClosedTopology(draft)
    }

    @Test
    fun `angled shell coordinates are not coerced to right angles`() {
        val points = listOf(
            PointXZ(4.0, 0.0), PointXZ(3.15, 2.80),
            PointXZ(0.0, 3.0), PointXZ(0.0, 0.0),
        )
        val draft = map(shell(listOf("a", "b", "c", "d"), points))

        assertEquals(3.15, draft.corners[1].x, 0.0)
        assertEquals(2.80, draft.corners[1].z, 0.0)
        val firstEdge = draft.corners[0]
        val angledEdge = draft.corners[1]
        assertTrue(abs(angledEdge.x - firstEdge.x) > 0.0)
        assertTrue(abs(angledEdge.z - firstEdge.z) > 0.0)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `incomplete topology cannot be disguised as a draft`() {
        map(
            RoomShell(
                orderedCandidateIds = listOf("a", "b", "c"),
                orderedCorners = listOf(PointXZ(0.0, 0.0), PointXZ(1.0, 0.0)),
                observedCoverageByCandidate = emptyMap(),
                confidence = 0.4,
            ),
        )
    }

    private fun map(shell: RoomShell): RoomDraft = RoomShellToRoomDraftMapper.map(
        shell = shell,
        ceilingHeight = CeilingHeightProposal.detected(2.73),
        idForCorner = { index -> "corner-$index" },
        idForWall = { candidateId -> "wall-$candidateId" },
    )

    private fun shell(ids: List<String>, corners: List<PointXZ>): RoomShell = RoomShell(
        orderedCandidateIds = ids,
        orderedCorners = corners,
        observedCoverageByCandidate = ids.associateWith { 1.0 },
        confidence = 0.9,
    )

    private fun assertClosedTopology(draft: RoomDraft) {
        assertEquals(draft.corners.size, draft.walls.size)
        draft.walls.forEachIndexed { index, wall ->
            assertEquals(draft.corners[index].id, wall.startCornerId)
            assertEquals(draft.corners[(index + 1) % draft.corners.size].id, wall.endCornerId)
        }
    }
}
