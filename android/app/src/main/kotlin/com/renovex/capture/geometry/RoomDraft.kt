package com.renovex.capture.geometry

/**
 * A 2D point in room-local floor-plane coordinates (meters), matching the
 * design spec's arbitrary straight-edged footprint model (§5): non-90-degree
 * walls are explicitly supported, so corners are NOT constrained to a grid.
 */
data class Corner(
    val id: String,
    val x: Double,
    val z: Double,
)

enum class WallStructuralState { UNKNOWN, NON_STRUCTURAL, STRUCTURAL }

/**
 * A wall segment between two consecutive corners in the room's polygon
 * (design spec §5.1/§5.2). Walls carry stable IDs because openings,
 * observations, AI operations, and rebase mappings reference them
 * (design spec §5.2) — an ID survives edits to the corners it spans.
 */
data class WallDraft(
    val id: String,
    val startCornerId: String,
    val endCornerId: String,
    val heightMeters: Double?,
    val thicknessMeters: Double? = null,
    val structuralState: WallStructuralState = WallStructuralState.UNKNOWN,
)

enum class OpeningType { DOOR, WINDOW, ARCHWAY, OTHER }
enum class OpeningProfile { RECTANGLE, ARCH }

/**
 * Arch profile parameters (design spec §6.2). Only meaningful when
 * [OpeningDraft.profile] is ARCH; null in every RECTANGLE opening.
 */
data class ArchParameters(
    val springHeightMeters: Double,
    val archRiseMeters: Double,
)

/**
 * A parametric opening bound to a wall (design spec §6). offsetAlongWall is
 * measured from the wall's start corner, in meters, along the wall's own
 * direction — never a room-global coordinate, since it must stay valid as
 * the room is edited.
 */
data class OpeningDraft(
    val id: String,
    val wallId: String,
    val type: OpeningType,
    val profile: OpeningProfile,
    val offsetAlongWallMeters: Double,
    val widthMeters: Double,
    val totalHeightMeters: Double,
    val sillHeightMeters: Double? = null,
    val archParameters: ArchParameters? = null,
)

enum class ObstacleType { COLUMN, FIXED_OBSTACLE }

data class ObstacleDraft(
    val id: String,
    val type: ObstacleType,
    val x: Double,
    val z: Double,
    val widthMeters: Double,
    val depthMeters: Double,
)

enum class ServicePointType { PLUMBING, ELECTRICAL, DRAIN }

data class ServicePointDraft(
    val id: String,
    val type: ServicePointType,
    val x: Double,
    val z: Double,
)

enum class CeilingHeightStatus { UNCONFIRMED, DETECTED, CONTRACTOR_CORRECTED }

data class CeilingHeightProposal(
    val meters: Double?,
    val status: CeilingHeightStatus,
) {
    init {
        require(meters == null || (meters.isFinite() && meters > 0.0))
        require((status == CeilingHeightStatus.UNCONFIRMED) == (meters == null))
    }

    companion object {
        fun unconfirmed(): CeilingHeightProposal =
            CeilingHeightProposal(null, CeilingHeightStatus.UNCONFIRMED)

        fun detected(meters: Double): CeilingHeightProposal =
            CeilingHeightProposal(meters, CeilingHeightStatus.DETECTED)

        fun contractorCorrected(meters: Double): CeilingHeightProposal =
            CeilingHeightProposal(meters, CeilingHeightStatus.CONTRACTOR_CORRECTED)
    }
}

/**
 * The full in-progress room geometry a contractor edits during/after a scan
 * (design spec §8.2's correction operations act on this). Immutable value
 * type — every edit operation in [RoomDraftEditor] returns a new instance,
 * so undo/redo and diffing stay trivial and there is never a
 * partially-applied edit visible to the UI.
 *
 * corners are stored in polygon winding order — consecutive corners
 * (including wrap-around from the last to the first) define the room's
 * straight-edged footprint (design spec §5: "arbitrary straight-edged 2D
 * footprint").
 */
data class RoomDraft(
    val corners: List<Corner> = emptyList(),
    val walls: List<WallDraft> = emptyList(),
    val openings: List<OpeningDraft> = emptyList(),
    val obstacles: List<ObstacleDraft> = emptyList(),
    val servicePoints: List<ServicePointDraft> = emptyList(),
    val ceilingHeight: CeilingHeightProposal = CeilingHeightProposal.unconfirmed(),
)
