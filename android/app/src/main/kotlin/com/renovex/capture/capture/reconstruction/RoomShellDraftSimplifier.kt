package com.renovex.capture.capture.reconstruction

import kotlin.math.abs
import kotlin.math.hypot

data class RoomShellSimplificationResult(
    val originalShell: RoomShell,
    val simplifiedShell: RoomShell,
    val collapsedCandidateIds: List<String>,
)

/**
 * Removes only provenance-supported intersection chamfers at the proposal
 * boundary. Stable observations remain untouched in [originalShell].
 */
object RoomShellDraftSimplifier {
    fun simplify(
        shell: RoomShell,
        candidates: List<WallCandidate>,
        config: RoomShellSimplificationConfig,
    ): RoomShellSimplificationResult {
        val candidateById = candidates.associateBy { it.candidateId }
        var current = shell
        val collapsed = mutableListOf<String>()
        var changed: Boolean
        do {
            changed = false
            if (current.orderedCorners.size <= MINIMUM_CORNERS) break
            for (edgeIndex in current.orderedCorners.indices) {
                val candidateIndex = (edgeIndex + 1) % current.orderedCandidateIds.size
                val candidateId = current.orderedCandidateIds[candidateIndex]
                val candidate = candidateById[candidateId] ?: continue
                val edgeStart = current.orderedCorners[edgeIndex]
                val edgeEnd = current.orderedCorners[(edgeIndex + 1) % current.orderedCorners.size]
                val architecturalLength = distance(edgeStart, edgeEnd)
                if (architecturalLength > config.maximumCollapsibleEdgeLengthMeters + EPSILON) continue
                if (architecturalLength <= EPSILON) continue
                val observedExtent = candidate.observedEndProjectionMeters - candidate.observedStartProjectionMeters
                if (observedExtent / architecturalLength + EPSILON <
                    config.minimumObservedToArchitecturalExtentRatio
                ) continue

                val previousCandidate = candidateById[current.orderedCandidateIds[edgeIndex]] ?: continue
                val nextCandidate = candidateById[
                    current.orderedCandidateIds[(edgeIndex + 2) % current.orderedCandidateIds.size]
                ] ?: continue
                val replacement = intersect(previousCandidate, nextCandidate) ?: continue
                if (maxOf(distance(replacement, edgeStart), distance(replacement, edgeEnd)) >
                    config.maximumReplacementCornerDisplacementMeters + EPSILON
                ) continue

                val newCorners = current.orderedCorners.toMutableList().apply {
                    this[edgeIndex] = replacement
                    removeAt((edgeIndex + 1) % size)
                }
                if (!isSimplePolygon(newCorners)) continue
                val newCandidateIds = current.orderedCandidateIds.toMutableList().apply {
                    removeAt(candidateIndex)
                }
                current = current.copy(
                    orderedCandidateIds = newCandidateIds,
                    orderedCorners = newCorners,
                    observedCoverageByCandidate = current.observedCoverageByCandidate - candidateId,
                )
                collapsed += candidateId
                changed = true
                break
            }
        } while (changed)
        return RoomShellSimplificationResult(shell, current, collapsed)
    }

    private fun intersect(first: WallCandidate, second: WallCandidate): PointXZ? {
        val determinant = first.unitNormal.x * second.unitNormal.z -
            second.unitNormal.x * first.unitNormal.z
        if (abs(determinant) <= EPSILON) return null
        val x = (first.supportingLineOffsetMeters * second.unitNormal.z -
            second.supportingLineOffsetMeters * first.unitNormal.z) / determinant
        val z = (first.unitNormal.x * second.supportingLineOffsetMeters -
            second.unitNormal.x * first.supportingLineOffsetMeters) / determinant
        return PointXZ(x, z).takeIf { it.x.isFinite() && it.z.isFinite() }
    }

    private fun isSimplePolygon(points: List<PointXZ>): Boolean {
        if (points.size < MINIMUM_CORNERS || abs(signedArea(points)) <= EPSILON) return false
        for (first in points.indices) {
            val firstNext = (first + 1) % points.size
            for (second in first + 1 until points.size) {
                val secondNext = (second + 1) % points.size
                if (firstNext == second || secondNext == first) continue
                if (segmentsProperlyIntersect(points[first], points[firstNext], points[second], points[secondNext])) {
                    return false
                }
            }
        }
        return true
    }

    private fun segmentsProperlyIntersect(a: PointXZ, b: PointXZ, c: PointXZ, d: PointXZ): Boolean =
        orientation(a, b, c) * orientation(a, b, d) < -EPSILON &&
            orientation(c, d, a) * orientation(c, d, b) < -EPSILON

    private fun orientation(a: PointXZ, b: PointXZ, c: PointXZ): Double =
        (b.x - a.x) * (c.z - a.z) - (b.z - a.z) * (c.x - a.x)

    private fun signedArea(points: List<PointXZ>): Double = points.indices.sumOf { index ->
        val next = points[(index + 1) % points.size]
        points[index].x * next.z - next.x * points[index].z
    } / 2.0

    private fun distance(first: PointXZ, second: PointXZ): Double =
        hypot(second.x - first.x, second.z - first.z)

    private const val MINIMUM_CORNERS = 3
    private const val EPSILON = 1e-9
}
