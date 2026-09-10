package com.renovex.capture.capture.reconstruction

import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.atan2

enum class GuidanceKind {
    BEGIN,
    SCAN_REMAINING_WALL,
    APPROACH_CORNER,
    MOVE_SIDEWAYS,
    COMPLETE,
}

enum class Direction { AHEAD, LEFT, RIGHT, BEHIND, NONE }

data class Guidance(
    val kind: GuidanceKind,
    val relativeDirection: Direction,
    val message: String,
)

/** Directs capture toward weak candidate/shell evidence instead of raw angular plane gaps. */
object RoomScanGuidance {
    fun guidanceFor(
        shellResult: RoomShellBuildResult,
        candidates: List<WallCandidate>,
        cameraPosition: PointXZ,
        cameraHeadingRadians: Double,
        config: ScanProgressConfig = ScanProgressConfig(),
    ): Guidance {
        require(cameraPosition.x.isFinite() && cameraPosition.z.isFinite())
        require(cameraHeadingRadians.isFinite())
        val candidateById = candidates.associateBy { it.candidateId }
        if (candidateById.isEmpty()) {
            return Guidance(GuidanceKind.BEGIN, Direction.AHEAD, "Point your camera at a wall to begin")
        }

        return when (shellResult) {
            is RoomShellBuildResult.Complete -> guidanceForComplete(
                shellResult.shell,
                candidateById,
                cameraPosition,
                cameraHeadingRadians,
                config,
            )
            is RoomShellBuildResult.Incomplete -> guidanceForIncomplete(
                shellResult,
                candidateById,
                cameraPosition,
                cameraHeadingRadians,
                config,
            )
        }
    }

    private fun guidanceForComplete(
        shell: RoomShell,
        candidateById: Map<String, WallCandidate>,
        cameraPosition: PointXZ,
        cameraHeadingRadians: Double,
        config: ScanProgressConfig,
    ): Guidance {
        val weakest = shell.orderedCandidateIds
            .mapIndexed { index, id -> Triple(id, index, shell.observedCoverageByCandidate[id] ?: 0.0) }
            .minWithOrNull(compareBy<Triple<String, Int, Double>> { it.third }.thenBy { it.first })
        if (weakest == null || weakest.third + NUMERIC_EPSILON >= config.sufficientWallEvidence) {
            return Guidance(GuidanceKind.COMPLETE, Direction.NONE, "Room shell has sufficient wall evidence")
        }
        val target = shellEdgeMidpoint(shell, weakest.second)
            ?: candidateById[weakest.first]?.let(::candidateMidpoint)
            ?: cameraPosition
        val kind = if (weakest.third + NUMERIC_EPSILON < config.weakWallEvidence) {
            GuidanceKind.SCAN_REMAINING_WALL
        } else {
            GuidanceKind.MOVE_SIDEWAYS
        }
        val message = if (kind == GuidanceKind.SCAN_REMAINING_WALL) {
            "Scan the remaining wall surface"
        } else {
            "Move sideways to strengthen wall coverage"
        }
        return Guidance(kind, relativeDirection(cameraPosition, cameraHeadingRadians, target, config), message)
    }

    private fun guidanceForIncomplete(
        incomplete: RoomShellBuildResult.Incomplete,
        candidateById: Map<String, WallCandidate>,
        cameraPosition: PointXZ,
        cameraHeadingRadians: Double,
        config: ScanProgressConfig,
    ): Guidance {
        val weakCandidate = candidateById.values
            .filter { it.stability + NUMERIC_EPSILON < config.weakWallEvidence }
            .minWithOrNull(compareBy<WallCandidate> { it.stability }.thenBy { it.candidateId })
        if (weakCandidate != null) {
            return Guidance(
                GuidanceKind.SCAN_REMAINING_WALL,
                relativeDirection(cameraPosition, cameraHeadingRadians, candidateMidpoint(weakCandidate), config),
                "Hold on the weak wall estimate",
            )
        }

        val endpointId = incomplete.orderedCandidateChains
            .filter { it.isNotEmpty() }
            .flatMap { listOf(it.first(), it.last()) }
            .distinct()
            .sorted()
            .firstOrNull { it in candidateById }
        val targetCandidate = endpointId?.let(candidateById::get) ?: candidateById.values.minBy { it.candidateId }
        return Guidance(
            GuidanceKind.APPROACH_CORNER,
            relativeDirection(cameraPosition, cameraHeadingRadians, candidateMidpoint(targetCandidate), config),
            "Approach the unresolved room corner",
        )
    }

    private fun shellEdgeMidpoint(shell: RoomShell, candidateIndex: Int): PointXZ? {
        if (shell.orderedCorners.size != shell.orderedCandidateIds.size || shell.orderedCorners.isEmpty()) return null
        val first = shell.orderedCorners[(candidateIndex - 1 + shell.orderedCorners.size) % shell.orderedCorners.size]
        val second = shell.orderedCorners[candidateIndex]
        return PointXZ((first.x + second.x) / 2.0, (first.z + second.z) / 2.0)
    }

    private fun candidateMidpoint(candidate: WallCandidate): PointXZ {
        val projection = (candidate.observedStartProjectionMeters + candidate.observedEndProjectionMeters) / 2.0
        return PointXZ(
            x = candidate.unitNormal.x * candidate.supportingLineOffsetMeters + candidate.unitDirection.x * projection,
            z = candidate.unitNormal.z * candidate.supportingLineOffsetMeters + candidate.unitDirection.z * projection,
        )
    }

    private fun relativeDirection(
        camera: PointXZ,
        heading: Double,
        target: PointXZ,
        config: ScanProgressConfig,
    ): Direction {
        val bearing = atan2(target.z - camera.z, target.x - camera.x)
        val relative = normalizeAngle(bearing - heading)
        return when {
            abs(relative) <= config.aheadHalfAngleRadians + NUMERIC_EPSILON -> Direction.AHEAD
            abs(relative) + NUMERIC_EPSILON >= PI - config.behindHalfAngleRadians -> Direction.BEHIND
            relative > 0.0 -> Direction.LEFT
            else -> Direction.RIGHT
        }
    }

    private fun normalizeAngle(angle: Double): Double {
        var normalized = angle % (2.0 * PI)
        if (normalized > PI) normalized -= 2.0 * PI
        if (normalized <= -PI) normalized += 2.0 * PI
        return normalized
    }

    private const val NUMERIC_EPSILON = 1e-9
}
