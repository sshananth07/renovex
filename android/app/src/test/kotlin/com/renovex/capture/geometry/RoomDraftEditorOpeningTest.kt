package com.renovex.capture.geometry

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomDraftEditorOpeningTest {

    private fun roomWithWall(): RoomDraft {
        val corners = listOf(Corner("c1", 0.0, 0.0), Corner("c2", 4.0, 0.0), Corner("c3", 4.0, 3.0))
        val walls = listOf(
            WallDraft("w1", "c1", "c2", 2.4),
            WallDraft("w2", "c2", "c3", 2.4),
            WallDraft("w3", "c3", "c1", 2.4),
        )
        return RoomDraft(corners = corners, walls = walls)
    }

    @Test
    fun `addOpening hosts a new rectangle opening on a valid wall`() {
        val draft = roomWithWall()
        val result = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.5, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        assertEquals(1, result.openings.size)
        assertEquals("w1", result.openings.first().wallId)
    }

    @Test
    fun `addOpening refuses a wall that does not exist`() {
        val draft = roomWithWall()
        val result = RoomDraftEditor.addOpening(
            draft, wallId = "nonexistent", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        assertTrue(result.openings.isEmpty())
    }

    @Test
    fun `addOpening refuses an opening that would extend past the wall end`() {
        // w1 is 4m long; a 1m-wide opening at offset 3.5 would extend to 4.5.
        val draft = roomWithWall()
        val result = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 3.5, widthMeters = 1.0, totalHeightMeters = 2.0,
        )
        assertTrue(result.openings.isEmpty())
    }

    @Test
    fun `addOpening refuses a negative offset`() {
        val draft = roomWithWall()
        val result = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = -0.5, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        assertTrue(result.openings.isEmpty())
    }

    @Test
    fun `addOpening refuses two openings that overlap on the same wall`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 1.0, totalHeightMeters = 2.0,
        )
        val result = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.WINDOW, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.5, widthMeters = 0.8, totalHeightMeters = 1.2,
        )
        assertEquals(1, result.openings.size) // second, overlapping opening rejected
    }

    @Test
    fun `resizeOpening changes width when the new width still fits the wall`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        val openingId = draft.openings.first().id
        val result = RoomDraftEditor.resizeOpening(draft, openingId, newWidthMeters = 1.2)
        assertEquals(1.2, result.openings.first().widthMeters, 0.0001)
    }

    @Test
    fun `resizeOpening refuses a width that would extend past the wall end`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 3.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        val openingId = draft.openings.first().id
        val result = RoomDraftEditor.resizeOpening(draft, openingId, newWidthMeters = 2.0)
        assertEquals(0.9, result.openings.first().widthMeters, 0.0001) // unchanged
    }

    @Test
    fun `repositionOpening moves the offset when it still fits`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        val openingId = draft.openings.first().id
        val result = RoomDraftEditor.repositionOpening(draft, openingId, newOffsetAlongWallMeters = 2.0)
        assertEquals(2.0, result.openings.first().offsetAlongWallMeters, 0.0001)
    }

    @Test
    fun `changeOpeningType updates the type without touching geometry`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        val openingId = draft.openings.first().id
        val result = RoomDraftEditor.changeOpeningType(draft, openingId, OpeningType.ARCHWAY)
        assertEquals(OpeningType.ARCHWAY, result.openings.first().type)
    }

    @Test
    fun `changeOpeningProfile from rectangle to arch requires and stores arch parameters`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        val openingId = draft.openings.first().id
        val archParams = ArchParameters(springHeightMeters = 1.8, archRiseMeters = 0.3)
        val result = RoomDraftEditor.changeOpeningProfile(draft, openingId, OpeningProfile.ARCH, archParams)

        val opening = result.openings.first()
        assertEquals(OpeningProfile.ARCH, opening.profile)
        assertEquals(archParams, opening.archParameters)
    }

    @Test
    fun `changeOpeningProfile from arch to rectangle clears arch parameters`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.ARCH,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
            archParameters = ArchParameters(1.8, 0.3),
        )
        val openingId = draft.openings.first().id
        val result = RoomDraftEditor.changeOpeningProfile(draft, openingId, OpeningProfile.RECTANGLE, null)

        val opening = result.openings.first()
        assertEquals(OpeningProfile.RECTANGLE, opening.profile)
        assertNull(opening.archParameters)
    }

    @Test
    fun `removeOpening drops the opening by ID`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, wallId = "w1", type = OpeningType.DOOR, profile = OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 1.0, widthMeters = 0.9, totalHeightMeters = 2.0,
        )
        val openingId = draft.openings.first().id
        val result = RoomDraftEditor.removeOpening(draft, openingId)
        assertTrue(result.openings.isEmpty())
    }

    @Test
    fun `addObstacle places a column at the given position`() {
        val draft = roomWithWall()
        val result = RoomDraftEditor.addObstacle(draft, ObstacleType.COLUMN, x = 2.0, z = 1.5, widthMeters = 0.3, depthMeters = 0.3)
        assertEquals(1, result.obstacles.size)
        assertEquals(ObstacleType.COLUMN, result.obstacles.first().type)
    }

    @Test
    fun `addServicePoint places a plumbing point at the given position`() {
        val draft = roomWithWall()
        val result = RoomDraftEditor.addServicePoint(draft, ServicePointType.PLUMBING, x = 1.0, z = 1.0)
        assertEquals(1, result.servicePoints.size)
        assertEquals(ServicePointType.PLUMBING, result.servicePoints.first().type)
    }

    @Test
    fun `opening correction updates type profile position and dimensions atomically`() {
        var draft = roomWithWall()
        draft = RoomDraftEditor.addOpening(
            draft, "w1", OpeningType.DOOR, OpeningProfile.RECTANGLE,
            offsetAlongWallMeters = 0.5, widthMeters = 0.8, totalHeightMeters = 2.0,
        )
        val id = draft.openings.single().id

        val result = RoomDraftEditor.updateOpening(
            draft = draft,
            openingId = id,
            type = OpeningType.ARCHWAY,
            profile = OpeningProfile.ARCH,
            offsetAlongWallMeters = 1.0,
            widthMeters = 1.2,
            totalHeightMeters = 2.2,
            sillHeightMeters = 0.1,
            archParameters = ArchParameters(1.8, 0.4),
        )

        val opening = result.openings.single()
        assertEquals(OpeningType.ARCHWAY, opening.type)
        assertEquals(OpeningProfile.ARCH, opening.profile)
        assertEquals(1.0, opening.offsetAlongWallMeters, 0.0)
        assertEquals(1.2, opening.widthMeters, 0.0)
        assertEquals(2.2, opening.totalHeightMeters, 0.0)
        assertEquals(ArchParameters(1.8, 0.4), opening.archParameters)
    }

    @Test
    fun `obstacle correction updates exposed geometry and type`() {
        val draft = RoomDraftEditor.addObstacle(roomWithWall(), ObstacleType.COLUMN, 1.0, 1.0, 0.3, 0.3)
        val id = draft.obstacles.single().id

        val result = RoomDraftEditor.updateObstacle(draft, id, ObstacleType.FIXED_OBSTACLE, 2.0, 1.5, 0.8, 0.4)

        assertEquals(ObstacleDraft(id, ObstacleType.FIXED_OBSTACLE, 2.0, 1.5, 0.8, 0.4), result.obstacles.single())
    }

    @Test
    fun `service correction updates exposed type and position`() {
        val draft = RoomDraftEditor.addServicePoint(roomWithWall(), ServicePointType.PLUMBING, 1.0, 1.0)
        val id = draft.servicePoints.single().id

        val result = RoomDraftEditor.updateServicePoint(draft, id, ServicePointType.DRAIN, 2.0, 1.5)

        assertEquals(ServicePointDraft(id, ServicePointType.DRAIN, 2.0, 1.5), result.servicePoints.single())
    }
}
