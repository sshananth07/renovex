package com.renovex.capture.geometry

import java.util.UUID
import kotlin.math.sqrt

/**
 * Deterministic, pure editing operations on [RoomDraft] — the contractor
 * correction toolkit (design spec §8.2: "add/move/delete corner; split/merge
 * wall; resize/reposition openings; ..."). Deliberately kept outside any AR
 * runtime dependency so it is fully unit-testable (Task 5 TDD requirement:
 * "unit-test polygon editing, wall derivation, opening-host validation").
 *
 * Every function takes a RoomDraft and returns a new RoomDraft — no mutation,
 * no AR/Android dependency. A caller that requests an impossible edit (e.g.
 * deleting a corner from a triangle, merging non-adjacent walls) gets the
 * original draft back unchanged rather than an exception, since these are
 * routine, expected user-input rejections in an interactive editor, not
 * exceptional program states.
 */
object RoomDraftEditor {
    private const val MIN_CORNERS = 3

    fun addCorner(draft: RoomDraft, afterCornerId: String, x: Double, z: Double): RoomDraft {
        val afterIndex = draft.corners.indexOfFirst { it.id == afterCornerId }
        if (afterIndex == -1) return draft

        val newCorner = Corner(newId(), x, z)
        val newCorners = draft.corners.toMutableList().apply { add(afterIndex + 1, newCorner) }
        val draftWithNewCorner = draft.copy(corners = newCorners)

        val nextCorner = draft.corners[(afterIndex + 1) % draft.corners.size]
        val splitWallId = draft.walls.firstOrNull {
            it.startCornerId == afterCornerId && it.endCornerId == nextCorner.id
        }?.id

        return if (splitWallId != null) {
            splitWallAtCorner(draftWithNewCorner, splitWallId, newCorner.id)
        } else {
            draftWithNewCorner
        }
    }

    fun moveCorner(draft: RoomDraft, cornerId: String, x: Double, z: Double): RoomDraft {
        if (draft.corners.none { it.id == cornerId }) return draft
        return draft.copy(corners = draft.corners.map { if (it.id == cornerId) it.copy(x = x, z = z) else it })
    }

    /**
     * Removes a corner and merges its two adjacent walls into one directly
     * connecting their other endpoints. Refuses if the room would drop
     * below [MIN_CORNERS] (a closed polygon needs at least a triangle).
     * Openings hosted on either removed wall are dropped rather than left
     * dangling with a non-existent wallId — the contractor must re-add a
     * missed opening on the new merged wall if it still applies (design
     * spec §8.2: "add missed openings manually").
     */
    fun deleteCorner(draft: RoomDraft, cornerId: String): RoomDraft {
        if (draft.corners.size <= MIN_CORNERS) return draft
        if (draft.corners.none { it.id == cornerId }) return draft

        val touchingWalls = draft.walls.filter { it.startCornerId == cornerId || it.endCornerId == cornerId }
        if (touchingWalls.size != 2) return draft // non-polygon topology; refuse rather than guess

        val (wallA, wallB) = touchingWalls
        val otherOfA = if (wallA.startCornerId == cornerId) wallA.endCornerId else wallA.startCornerId
        val otherOfB = if (wallB.startCornerId == cornerId) wallB.endCornerId else wallB.startCornerId

        val mergedWall = WallDraft(
            id = newId(), startCornerId = otherOfA, endCornerId = otherOfB,
            heightMeters = wallA.heightMeters, thicknessMeters = wallA.thicknessMeters,
            structuralState = strictestOf(wallA.structuralState, wallB.structuralState),
        )

        val removedWallIds = setOf(wallA.id, wallB.id)
        return draft.copy(
            corners = draft.corners.filterNot { it.id == cornerId },
            walls = draft.walls.filterNot { it.id in removedWallIds } + mergedWall,
            openings = draft.openings.filterNot { it.wallId in removedWallIds },
        )
    }

    /**
     * Inserts a corner at the wall's midpoint, splitting it into two walls.
     * Any opening hosted on the original wall is re-hosted onto whichever
     * half now contains its offset, with its offset re-based to that half
     * — a naive drop would silently lose a correctly-placed opening on
     * every split.
     */
    fun splitWall(draft: RoomDraft, wallId: String): RoomDraft {
        val wall = draft.walls.firstOrNull { it.id == wallId } ?: return draft
        val start = draft.corners.firstOrNull { it.id == wall.startCornerId } ?: return draft
        val end = draft.corners.firstOrNull { it.id == wall.endCornerId } ?: return draft
        val midCorner = Corner(newId(), (start.x + end.x) / 2.0, (start.z + end.z) / 2.0)
        return splitWallAtCorner(draft.copy(corners = draft.corners + midCorner), wallId, midCorner.id)
    }

    private fun splitWallAtCorner(draft: RoomDraft, wallId: String, midCornerId: String): RoomDraft {
        val wall = draft.walls.firstOrNull { it.id == wallId } ?: return draft
        val start = draft.corners.firstOrNull { it.id == wall.startCornerId } ?: return draft
        val mid = draft.corners.firstOrNull { it.id == midCornerId } ?: return draft
        val halfLength = distance(start, mid)
        val fullLength = distance(start, draft.corners.first { it.id == wall.endCornerId })

        val firstHalf = wall.copy(id = newId(), endCornerId = midCornerId)
        val secondHalf = wall.copy(id = newId(), startCornerId = midCornerId)

        val rehostedOpenings = draft.openings.map { opening ->
            if (opening.wallId != wallId) return@map opening
            if (opening.offsetAlongWallMeters <= halfLength) {
                opening.copy(wallId = firstHalf.id)
            } else {
                opening.copy(wallId = secondHalf.id, offsetAlongWallMeters = opening.offsetAlongWallMeters - halfLength)
            }
        }

        return draft.copy(
            walls = draft.walls.filterNot { it.id == wallId } + firstHalf + secondHalf,
            openings = rehostedOpenings,
        )
    }

    /**
     * Merges two walls that share exactly one corner into a single wall
     * connecting their two OTHER endpoints, removing the shared corner.
     * Refuses (returns the draft unchanged) if the walls do not share
     * exactly one corner, or if merging would collapse the room below
     * [MIN_CORNERS].
     */
    fun mergeWalls(draft: RoomDraft, wallIdA: String, wallIdB: String): RoomDraft {
        val wallA = draft.walls.firstOrNull { it.id == wallIdA } ?: return draft
        val wallB = draft.walls.firstOrNull { it.id == wallIdB } ?: return draft
        if (draft.corners.size <= MIN_CORNERS) return draft

        val endpointsA = setOf(wallA.startCornerId, wallA.endCornerId)
        val endpointsB = setOf(wallB.startCornerId, wallB.endCornerId)
        val shared = endpointsA.intersect(endpointsB)
        if (shared.size != 1) return draft
        val sharedCornerId = shared.first()

        return deleteCorner(draft, sharedCornerId).takeIf { it.corners.size < draft.corners.size } ?: draft
    }

    /**
     * Moves the wall's end corner along the wall's current direction so the
     * wall's length becomes exactly newLengthMeters (design spec §8.2:
     * "enter corrected measurements"). The start corner never moves —
     * correcting one wall's length should not silently displace the
     * opposite end of the room.
     */
    fun enterCorrectedWallLength(draft: RoomDraft, wallId: String, newLengthMeters: Double): RoomDraft {
        if (newLengthMeters <= 0.0) return draft
        val wall = draft.walls.firstOrNull { it.id == wallId } ?: return draft
        val start = draft.corners.firstOrNull { it.id == wall.startCornerId } ?: return draft
        val end = draft.corners.firstOrNull { it.id == wall.endCornerId } ?: return draft

        val currentLength = distance(start, end)
        if (currentLength <= 0.0) return draft

        val scale = newLengthMeters / currentLength
        val newX = start.x + (end.x - start.x) * scale
        val newZ = start.z + (end.z - start.z) * scale
        return moveCorner(draft, end.id, newX, newZ)
    }

    /**
     * Adds a new opening to wallId if it is geometrically valid: the wall
     * must exist, the offset must be non-negative, the opening must fit
     * entirely within the wall's length, and it must not overlap any
     * existing opening on the same wall (design spec §21: "opening
     * overlap" is architectural-validation-relevant even at the local
     * draft-editing stage, not only server-side). Silently no-ops on any
     * violation — the draft is unchanged and the caller can inspect
     * whether openings.size grew to detect rejection.
     */
    fun addOpening(
        draft: RoomDraft, wallId: String, type: OpeningType, profile: OpeningProfile,
        offsetAlongWallMeters: Double, widthMeters: Double, totalHeightMeters: Double,
        sillHeightMeters: Double? = null, archParameters: ArchParameters? = null,
    ): RoomDraft {
        val wallLength = wallLengthOf(draft, wallId) ?: return draft
        if (!fitsOnWall(offsetAlongWallMeters, widthMeters, wallLength)) return draft
        if (overlapsExistingOpening(draft, wallId, offsetAlongWallMeters, widthMeters, excludingOpeningId = null)) return draft

        val opening = OpeningDraft(
            id = newId(), wallId = wallId, type = type, profile = profile,
            offsetAlongWallMeters = offsetAlongWallMeters, widthMeters = widthMeters,
            totalHeightMeters = totalHeightMeters, sillHeightMeters = sillHeightMeters,
            archParameters = archParameters,
        )
        return draft.copy(openings = draft.openings + opening)
    }

    fun resizeOpening(draft: RoomDraft, openingId: String, newWidthMeters: Double): RoomDraft {
        val opening = draft.openings.firstOrNull { it.id == openingId } ?: return draft
        val wallLength = wallLengthOf(draft, opening.wallId) ?: return draft
        if (!fitsOnWall(opening.offsetAlongWallMeters, newWidthMeters, wallLength)) return draft
        if (overlapsExistingOpening(draft, opening.wallId, opening.offsetAlongWallMeters, newWidthMeters, excludingOpeningId = openingId)) return draft
        return draft.copy(openings = draft.openings.map { if (it.id == openingId) it.copy(widthMeters = newWidthMeters) else it })
    }

    fun repositionOpening(draft: RoomDraft, openingId: String, newOffsetAlongWallMeters: Double): RoomDraft {
        val opening = draft.openings.firstOrNull { it.id == openingId } ?: return draft
        val wallLength = wallLengthOf(draft, opening.wallId) ?: return draft
        if (!fitsOnWall(newOffsetAlongWallMeters, opening.widthMeters, wallLength)) return draft
        if (overlapsExistingOpening(draft, opening.wallId, newOffsetAlongWallMeters, opening.widthMeters, excludingOpeningId = openingId)) return draft
        return draft.copy(openings = draft.openings.map {
            if (it.id == openingId) it.copy(offsetAlongWallMeters = newOffsetAlongWallMeters) else it
        })
    }

    fun changeOpeningType(draft: RoomDraft, openingId: String, newType: OpeningType): RoomDraft {
        if (draft.openings.none { it.id == openingId }) return draft
        return draft.copy(openings = draft.openings.map { if (it.id == openingId) it.copy(type = newType) else it })
    }

    /**
     * Switches an opening's profile between RECTANGLE and ARCH (design
     * spec §6.4). Switching to ARCH without archParameters, or to
     * RECTANGLE while still carrying archParameters, would leave an
     * inconsistent record — this always sets both fields together so that
     * profile == ARCH iff archParameters != null is an invariant a caller
     * can rely on.
     */
    fun changeOpeningProfile(
        draft: RoomDraft, openingId: String, newProfile: OpeningProfile, archParameters: ArchParameters?,
    ): RoomDraft {
        if (draft.openings.none { it.id == openingId }) return draft
        if (newProfile == OpeningProfile.ARCH && archParameters == null) return draft
        val resolvedParams = if (newProfile == OpeningProfile.ARCH) archParameters else null
        return draft.copy(openings = draft.openings.map {
            if (it.id == openingId) it.copy(profile = newProfile, archParameters = resolvedParams) else it
        })
    }

    fun removeOpening(draft: RoomDraft, openingId: String): RoomDraft =
        draft.copy(openings = draft.openings.filterNot { it.id == openingId })

    fun updateOpening(
        draft: RoomDraft,
        openingId: String,
        type: OpeningType,
        profile: OpeningProfile,
        offsetAlongWallMeters: Double,
        widthMeters: Double,
        totalHeightMeters: Double,
        sillHeightMeters: Double? = null,
        archParameters: ArchParameters? = null,
    ): RoomDraft {
        val existing = draft.openings.firstOrNull { it.id == openingId } ?: return draft
        val wallLength = wallLengthOf(draft, existing.wallId) ?: return draft
        if (!fitsOnWall(offsetAlongWallMeters, widthMeters, wallLength)) return draft
        if (!totalHeightMeters.isFinite() || totalHeightMeters <= 0.0) return draft
        if (sillHeightMeters != null && (!sillHeightMeters.isFinite() || sillHeightMeters < 0.0)) return draft
        if ((profile == OpeningProfile.ARCH) != (archParameters != null)) return draft
        if (archParameters != null && (
                !archParameters.springHeightMeters.isFinite() || archParameters.springHeightMeters <= 0.0 ||
                    !archParameters.archRiseMeters.isFinite() || archParameters.archRiseMeters <= 0.0
                )
        ) return draft
        if (overlapsExistingOpening(draft, existing.wallId, offsetAlongWallMeters, widthMeters, openingId)) return draft
        val updated = existing.copy(
            type = type,
            profile = profile,
            offsetAlongWallMeters = offsetAlongWallMeters,
            widthMeters = widthMeters,
            totalHeightMeters = totalHeightMeters,
            sillHeightMeters = sillHeightMeters,
            archParameters = archParameters,
        )
        return draft.copy(openings = draft.openings.map { if (it.id == openingId) updated else it })
    }

    fun addObstacle(draft: RoomDraft, type: ObstacleType, x: Double, z: Double, widthMeters: Double, depthMeters: Double): RoomDraft =
        draft.copy(obstacles = draft.obstacles + ObstacleDraft(newId(), type, x, z, widthMeters, depthMeters))

    fun removeObstacle(draft: RoomDraft, obstacleId: String): RoomDraft =
        draft.copy(obstacles = draft.obstacles.filterNot { it.id == obstacleId })

    fun updateObstacle(
        draft: RoomDraft,
        obstacleId: String,
        type: ObstacleType,
        x: Double,
        z: Double,
        widthMeters: Double,
        depthMeters: Double,
    ): RoomDraft {
        if (draft.obstacles.none { it.id == obstacleId }) return draft
        if (!x.isFinite() || !z.isFinite() || !widthMeters.isFinite() || widthMeters <= 0.0 ||
            !depthMeters.isFinite() || depthMeters <= 0.0
        ) return draft
        return draft.copy(obstacles = draft.obstacles.map {
            if (it.id == obstacleId) it.copy(type = type, x = x, z = z, widthMeters = widthMeters, depthMeters = depthMeters) else it
        })
    }

    fun addServicePoint(draft: RoomDraft, type: ServicePointType, x: Double, z: Double): RoomDraft =
        draft.copy(servicePoints = draft.servicePoints + ServicePointDraft(newId(), type, x, z))

    fun removeServicePoint(draft: RoomDraft, servicePointId: String): RoomDraft =
        draft.copy(servicePoints = draft.servicePoints.filterNot { it.id == servicePointId })

    fun updateServicePoint(
        draft: RoomDraft,
        servicePointId: String,
        type: ServicePointType,
        x: Double,
        z: Double,
    ): RoomDraft {
        if (draft.servicePoints.none { it.id == servicePointId }) return draft
        if (!x.isFinite() || !z.isFinite()) return draft
        return draft.copy(servicePoints = draft.servicePoints.map {
            if (it.id == servicePointId) it.copy(type = type, x = x, z = z) else it
        })
    }

    private fun wallLengthOf(draft: RoomDraft, wallId: String): Double? {
        val wall = draft.walls.firstOrNull { it.id == wallId } ?: return null
        val start = draft.corners.firstOrNull { it.id == wall.startCornerId } ?: return null
        val end = draft.corners.firstOrNull { it.id == wall.endCornerId } ?: return null
        return distance(start, end)
    }

    private fun fitsOnWall(offsetMeters: Double, widthMeters: Double, wallLengthMeters: Double): Boolean =
        offsetMeters >= 0.0 && widthMeters > 0.0 && offsetMeters + widthMeters <= wallLengthMeters

    private fun overlapsExistingOpening(
        draft: RoomDraft, wallId: String, offsetMeters: Double, widthMeters: Double, excludingOpeningId: String?,
    ): Boolean {
        val newStart = offsetMeters
        val newEnd = offsetMeters + widthMeters
        return draft.openings.any { existing ->
            existing.wallId == wallId && existing.id != excludingOpeningId &&
                newStart < existing.offsetAlongWallMeters + existing.widthMeters &&
                existing.offsetAlongWallMeters < newEnd
        }
    }

    private fun distance(a: Corner, b: Corner): Double {
        val dx = b.x - a.x
        val dz = b.z - a.z
        return sqrt(dx * dx + dz * dz)
    }

    /** UNKNOWN is never authoritative-safe to relax, so it wins over a more
     * specific value from either side when merging two walls into one
     * (AI/vision must never infer non_structural authoritatively, design
     * spec §5.1 — a merge is not the review step that would justify
     * relaxing a stricter classification either). */
    private fun strictestOf(a: WallStructuralState, b: WallStructuralState): WallStructuralState = when {
        a == WallStructuralState.STRUCTURAL || b == WallStructuralState.STRUCTURAL -> WallStructuralState.STRUCTURAL
        a == WallStructuralState.UNKNOWN || b == WallStructuralState.UNKNOWN -> WallStructuralState.UNKNOWN
        else -> WallStructuralState.NON_STRUCTURAL
    }

    private fun newId(): String = UUID.randomUUID().toString()
}
