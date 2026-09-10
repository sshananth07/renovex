package com.renovex.capture.geometry

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomDraftEditorCornerTest {

    private fun square(): RoomDraft {
        // A simple 4x3m rectangular room, corners in winding order.
        val c = listOf(
            Corner("c1", 0.0, 0.0),
            Corner("c2", 4.0, 0.0),
            Corner("c3", 4.0, 3.0),
            Corner("c4", 0.0, 3.0),
        )
        val walls = listOf(
            WallDraft("w1", "c1", "c2", 2.4),
            WallDraft("w2", "c2", "c3", 2.4),
            WallDraft("w3", "c3", "c4", 2.4),
            WallDraft("w4", "c4", "c1", 2.4),
        )
        return RoomDraft(corners = c, walls = walls)
    }

    @Test
    fun `addCorner inserts a corner between two existing corners and splits the wall`() {
        val draft = square()
        val result = RoomDraftEditor.addCorner(draft, afterCornerId = "c1", x = 2.0, z = 0.0)

        assertEquals(5, result.corners.size)
        val newCorner = result.corners.first { it.x == 2.0 && it.z == 0.0 }
        // The original wall w1 (c1->c2) must now be split into two walls
        // through the new corner, not left dangling.
        val wallsFromC1 = result.walls.filter { it.startCornerId == "c1" || it.endCornerId == "c1" }
        assertTrue(wallsFromC1.any { it.startCornerId == "c1" && it.endCornerId == newCorner.id })
        assertTrue(result.walls.any { it.startCornerId == newCorner.id && it.endCornerId == "c2" })
        // Old direct c1->c2 wall must be gone.
        assertTrue(result.walls.none { it.startCornerId == "c1" && it.endCornerId == "c2" })
    }

    @Test
    fun `moveCorner updates the corner position and leaves wall references intact`() {
        val draft = square()
        val result = RoomDraftEditor.moveCorner(draft, cornerId = "c2", x = 5.0, z = 0.5)

        val moved = result.corners.first { it.id == "c2" }
        assertEquals(5.0, moved.x, 0.0001)
        assertEquals(0.5, moved.z, 0.0001)
        // Walls referencing c2 still reference it by ID — geometry recomputes
        // from corner positions, wall topology is untouched by a move.
        assertTrue(result.walls.any { it.startCornerId == "c2" || it.endCornerId == "c2" })
    }

    @Test
    fun `deleteCorner removes the corner and merges its two adjacent walls into one`() {
        val draft = square()
        val result = RoomDraftEditor.deleteCorner(draft, cornerId = "c2")

        assertEquals(3, result.corners.size)
        assertTrue(result.corners.none { it.id == "c2" })
        // c1 and c3 must now be connected directly by a single merged wall.
        assertTrue(result.walls.any {
            (it.startCornerId == "c1" && it.endCornerId == "c3") ||
                (it.startCornerId == "c3" && it.endCornerId == "c1")
        })
        // Both walls that touched c2 must be gone.
        assertTrue(result.walls.none { it.startCornerId == "c2" || it.endCornerId == "c2" })
    }

    @Test
    fun `deleteCorner refuses to drop a room below a triangle`() {
        val triangle = RoomDraft(
            corners = listOf(Corner("a", 0.0, 0.0), Corner("b", 2.0, 0.0), Corner("c", 1.0, 2.0)),
            walls = listOf(
                WallDraft("w1", "a", "b", 2.4),
                WallDraft("w2", "b", "c", 2.4),
                WallDraft("w3", "c", "a", 2.4),
            ),
        )
        val result = RoomDraftEditor.deleteCorner(triangle, cornerId = "a")
        // Refused: a valid closed polygon needs at least 3 corners.
        assertEquals(3, result.corners.size)
    }

    @Test
    fun `deleteCorner cascades to remove openings hosted on a deleted wall`() {
        val draft = square().let {
            it.copy(openings = it.openings + OpeningDraft(
                id = "o1", wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
                offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
            ))
        }
        val result = RoomDraftEditor.deleteCorner(draft, cornerId = "c1")
        // w1 (c1->c2) no longer exists after c1 is removed and its walls merge
        // differently, so any opening hosted on it must not silently dangle.
        assertTrue(result.openings.none { it.id == "o1" && result.walls.none { w -> w.id == it.wallId } })
    }

    @Test
    fun `splitWall inserts a corner at the midpoint and creates two walls`() {
        val draft = square()
        val result = RoomDraftEditor.splitWall(draft, wallId = "w1")

        assertEquals(5, result.corners.size)
        val midpoint = result.corners.first { it.x == 2.0 && it.z == 0.0 }
        assertTrue(result.walls.any { it.startCornerId == "c1" && it.endCornerId == midpoint.id })
        assertTrue(result.walls.any { it.startCornerId == midpoint.id && it.endCornerId == "c2" })
        assertTrue(result.walls.none { it.id == "w1" })
    }

    @Test
    fun `mergeWalls combines two adjacent walls sharing a corner into one`() {
        val draft = square()
        val split = RoomDraftEditor.splitWall(draft, wallId = "w1")
        val midpointId = split.corners.first { it.x == 2.0 && it.z == 0.0 }.id
        val wallA = split.walls.first { it.startCornerId == "c1" && it.endCornerId == midpointId }
        val wallB = split.walls.first { it.startCornerId == midpointId && it.endCornerId == "c2" }

        val merged = RoomDraftEditor.mergeWalls(split, wallA.id, wallB.id)

        assertEquals(4, merged.corners.size)
        assertTrue(merged.corners.none { it.id == midpointId })
        assertTrue(merged.walls.any { it.startCornerId == "c1" && it.endCornerId == "c2" })
    }

    @Test
    fun `mergeWalls refuses two walls that do not share a corner`() {
        val draft = square()
        val result = RoomDraftEditor.mergeWalls(draft, "w1", "w3")
        // w1 (c1-c2) and w3 (c3-c4) share no corner — refused, draft unchanged.
        assertEquals(draft, result)
    }

    @Test
    fun `enterCorrectedWallLength moves the far corner along the wall direction`() {
        val draft = square()
        // w1 is c1(0,0) -> c2(4,0); correcting to 5.0m should move c2 to (5,0).
        val result = RoomDraftEditor.enterCorrectedWallLength(draft, wallId = "w1", newLengthMeters = 5.0)

        val c2 = result.corners.first { it.id == "c2" }
        assertEquals(5.0, c2.x, 0.0001)
        assertEquals(0.0, c2.z, 0.0001)
    }
}
