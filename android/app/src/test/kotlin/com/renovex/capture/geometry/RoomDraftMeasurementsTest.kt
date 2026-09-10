package com.renovex.capture.geometry

import kotlin.math.PI
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class RoomDraftMeasurementsTest {
    @Test
    fun `unconfirmed ceiling never fabricates height dependent quantities`() {
        // Break caught: the old RoomDraft default silently reports 2.40 m and
        // produces apparently measured wall areas without evidence.
        val measurements = RoomDraftMeasurements.calculate(rectangle())

        assertEquals(listOf(4.0, 3.0, 4.0, 3.0), measurements.wallLengthsMeters.values.toList())
        assertEquals(14.0, measurements.perimeterMeters, 1e-9)
        assertEquals(12.0, measurements.floorAreaSquareMeters, 1e-9)
        assertNull(measurements.grossWallAreaSquareMeters)
        assertNull(measurements.netWallAreaSquareMeters)
    }

    @Test
    fun `corner height and rectangular opening edits deterministically recompute quantities`() {
        val base = rectangle().copy(
            ceilingHeight = CeilingHeightProposal.contractorCorrected(2.5),
            walls = rectangle().walls.map { it.copy(heightMeters = 2.5) },
        )
        val withOpening = base.copy(
            openings = listOf(
                OpeningDraft("door", "w0", OpeningType.DOOR, OpeningProfile.RECTANGLE, 0.5, 1.0, 2.0),
            ),
        )
        val moved = RoomDraftEditor.moveCorner(withOpening, "c1", 5.0, 0.0)

        val measurements = RoomDraftMeasurements.calculate(moved)

        assertEquals(13.5, measurements.floorAreaSquareMeters, 1e-9)
        assertEquals(5.0, measurements.wallLengthsMeters.getValue("w0"), 1e-9)
        assertEquals(2.0, measurements.openingAreasSquareMeters.getValue("door"), 1e-9)
        assertEquals(measurements.perimeterMeters * 2.5, measurements.grossWallAreaSquareMeters!!, 1e-9)
        assertEquals(measurements.grossWallAreaSquareMeters!! - 2.0, measurements.netWallAreaSquareMeters!!, 1e-9)
    }

    @Test
    fun `arched opening area uses spring rectangle plus semi elliptical cap`() {
        val draft = rectangle().copy(
            openings = listOf(
                OpeningDraft(
                    id = "arch",
                    wallId = "w0",
                    type = OpeningType.ARCHWAY,
                    profile = OpeningProfile.ARCH,
                    offsetAlongWallMeters = 0.5,
                    widthMeters = 2.0,
                    totalHeightMeters = 2.0,
                    archParameters = ArchParameters(springHeightMeters = 1.5, archRiseMeters = 0.5),
                ),
            ),
        )

        val area = RoomDraftMeasurements.calculate(draft).openingAreasSquareMeters.getValue("arch")

        assertEquals(3.0 + PI / 4.0, area, 1e-9)
    }

    private fun rectangle(): RoomDraft {
        val corners = listOf(
            Corner("c0", 0.0, 0.0), Corner("c1", 4.0, 0.0),
            Corner("c2", 4.0, 3.0), Corner("c3", 0.0, 3.0),
        )
        return RoomDraft(
            corners = corners,
            walls = listOf(
                WallDraft("w0", "c0", "c1", null),
                WallDraft("w1", "c1", "c2", null),
                WallDraft("w2", "c2", "c3", null),
                WallDraft("w3", "c3", "c0", null),
            ),
        )
    }
}
