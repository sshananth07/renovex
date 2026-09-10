package com.renovex.capture.geometry

import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.hypot

data class RoomDraftMeasurementResult(
    val wallLengthsMeters: Map<String, Double>,
    val perimeterMeters: Double,
    val floorAreaSquareMeters: Double,
    val openingAreasSquareMeters: Map<String, Double>,
    val grossWallAreaSquareMeters: Double?,
    val netWallAreaSquareMeters: Double?,
)

/** Derived proposal quantities. No value here is independently editable. */
object RoomDraftMeasurements {
    fun calculate(draft: RoomDraft): RoomDraftMeasurementResult {
        val cornerById = draft.corners.associateBy { it.id }
        val wallLengths = linkedMapOf<String, Double>()
        draft.walls.forEach { wall ->
            val start = cornerById[wall.startCornerId]
            val end = cornerById[wall.endCornerId]
            if (start != null && end != null) {
                wallLengths[wall.id] = hypot(end.x - start.x, end.z - start.z)
            }
        }
        val openingAreas = draft.openings.associateTo(linkedMapOf()) { opening ->
            opening.id to openingArea(opening)
        }
        val grossByWall = draft.walls.map { wall ->
            val length = wallLengths[wall.id]
            val height = wall.heightMeters
            if (length == null || height == null) null else length * height
        }
        val gross = if (grossByWall.any { it == null }) null else grossByWall.filterNotNull().sum()
        val net = gross?.minus(openingAreas.values.sum())
        return RoomDraftMeasurementResult(
            wallLengthsMeters = wallLengths,
            perimeterMeters = wallLengths.values.sum(),
            floorAreaSquareMeters = polygonArea(draft.corners),
            openingAreasSquareMeters = openingAreas,
            grossWallAreaSquareMeters = gross,
            netWallAreaSquareMeters = net,
        )
    }

    private fun polygonArea(corners: List<Corner>): Double {
        if (corners.size < 3) return 0.0
        return abs(corners.indices.sumOf { index ->
            val next = corners[(index + 1) % corners.size]
            corners[index].x * next.z - next.x * corners[index].z
        }) / 2.0
    }

    private fun openingArea(opening: OpeningDraft): Double = when (opening.profile) {
        OpeningProfile.RECTANGLE -> opening.widthMeters * opening.totalHeightMeters
        OpeningProfile.ARCH -> {
            val arch = opening.archParameters ?: return 0.0
            opening.widthMeters * arch.springHeightMeters +
                PI * opening.widthMeters * arch.archRiseMeters / 4.0
        }
    }
}
