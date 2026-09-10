package com.renovex.capture.capture.reconstruction

import com.renovex.capture.geometry.Corner
import com.renovex.capture.geometry.CeilingHeightProposal
import com.renovex.capture.geometry.RoomDraft
import com.renovex.capture.geometry.WallDraft

/** Maps only a complete shell's exact ordered geometry into editable draft topology. */
object RoomShellToRoomDraftMapper {
    fun map(
        shell: RoomShell,
        ceilingHeight: CeilingHeightProposal,
        idForCorner: (index: Int) -> String,
        idForWall: (candidateId: String) -> String,
    ): RoomDraft {
        require(shell.orderedCorners.size >= MINIMUM_POLYGON_CORNERS)
        require(shell.orderedCandidateIds.size == shell.orderedCorners.size) {
            "a complete shell needs one real wall candidate per polygon edge"
        }
        require(shell.orderedCandidateIds.distinct().size == shell.orderedCandidateIds.size) {
            "shell candidate IDs must be unique"
        }
        require(shell.orderedCorners.all { it.x.isFinite() && it.z.isFinite() })

        val corners = shell.orderedCorners.mapIndexed { index, point ->
            Corner(id = idForCorner(index), x = point.x, z = point.z)
        }
        require(corners.all { it.id.isNotBlank() } && corners.map { it.id }.distinct().size == corners.size) {
            "corner IDs must be non-blank and unique"
        }

        // RoomShell corner i is the intersection of candidate i and candidate i+1.
        // Therefore the draft edge corner i -> i+1 retains candidate i+1's stable wall ID.
        val walls = corners.indices.map { cornerIndex ->
            val nextCornerIndex = (cornerIndex + 1) % corners.size
            val candidateId = shell.orderedCandidateIds[nextCornerIndex]
            WallDraft(
                id = idForWall(candidateId),
                startCornerId = corners[cornerIndex].id,
                endCornerId = corners[nextCornerIndex].id,
                heightMeters = ceilingHeight.meters,
            )
        }
        require(walls.all { it.id.isNotBlank() } && walls.map { it.id }.distinct().size == walls.size) {
            "wall IDs must be non-blank and unique"
        }

        return RoomDraft(
            corners = corners,
            walls = walls,
            ceilingHeight = ceilingHeight,
        )
    }

    private const val MINIMUM_POLYGON_CORNERS = 3
}
