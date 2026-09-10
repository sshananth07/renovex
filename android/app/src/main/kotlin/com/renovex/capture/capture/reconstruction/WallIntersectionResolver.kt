package com.renovex.capture.capture.reconstruction

import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.acos
import kotlin.math.max

data class WallIntersection(
    val firstCandidateId: String,
    val secondCandidateId: String,
    val point: PointXZ,
    val confidence: Double,
)

/** Finds plausible architectural corners from stable infinite supporting lines. */
object WallIntersectionResolver {
    fun resolve(
        candidates: List<WallCandidate>,
        config: IntersectionConfig = IntersectionConfig(),
    ): List<WallIntersection> {
        require(candidates.map { it.candidateId }.distinct().size == candidates.size) {
            "wall candidate IDs must be unique"
        }
        val stable = candidates
            .filter { it.state == WallCandidateState.STABLE }
            .sortedBy { it.candidateId }
        val intersections = mutableListOf<WallIntersection>()
        for (firstIndex in stable.indices) {
            for (secondIndex in firstIndex + 1 until stable.size) {
                resolvePair(stable[firstIndex], stable[secondIndex], config)?.let(intersections::add)
            }
        }
        return intersections
    }

    private fun resolvePair(
        first: WallCandidate,
        second: WallCandidate,
        config: IntersectionConfig,
    ): WallIntersection? {
        val angle = acos(
            abs(dot(first.unitDirection, second.unitDirection)).coerceIn(-1.0, 1.0),
        )
        if (angle + NUMERIC_EPSILON < config.minimumAngleRadians) return null

        val determinant = first.unitNormal.x * second.unitNormal.z -
            first.unitNormal.z * second.unitNormal.x
        if (abs(determinant) <= NUMERIC_EPSILON) return null
        val rawX = (first.supportingLineOffsetMeters * second.unitNormal.z -
            first.unitNormal.z * second.supportingLineOffsetMeters) / determinant
        val rawZ = (first.unitNormal.x * second.supportingLineOffsetMeters -
            first.supportingLineOffsetMeters * second.unitNormal.x) / determinant
        val point = PointXZ(
            x = if (abs(rawX) <= NUMERIC_EPSILON) 0.0 else rawX,
            z = if (abs(rawZ) <= NUMERIC_EPSILON) 0.0 else rawZ,
        )
        if (!point.x.isFinite() || !point.z.isFinite()) return null

        val maximumExtension = max(
            extentExtension(first, point),
            extentExtension(second, point),
        )
        if (maximumExtension + NUMERIC_EPSILON >= config.maximumObservedExtentExtensionMeters) return null
        val extensionFactor = (
            1.0 - maximumExtension / config.maximumObservedExtentExtensionMeters
            ).coerceIn(0.0, 1.0)
        if (extensionFactor <= 0.0) return null

        val perpendicularCloseness = (
            1.0 - abs(PI / 2.0 - angle) / config.perpendicularBoostWindowRadians
            ).coerceIn(0.0, 1.0)
        val boost = config.maximumPerpendicularConfidenceBoost * perpendicularCloseness
        val confidence = (
            ((first.stability + second.stability) / 2.0) * extensionFactor * (1.0 + boost)
            ).coerceIn(0.0, 1.0)
        return WallIntersection(
            firstCandidateId = first.candidateId,
            secondCandidateId = second.candidateId,
            point = point,
            confidence = confidence,
        )
    }

    private fun extentExtension(candidate: WallCandidate, point: PointXZ): Double {
        val projection = point.x * candidate.unitDirection.x + point.z * candidate.unitDirection.z
        return candidate.observedIntervals.minOf { interval ->
            when {
                projection < interval.startMeters -> interval.startMeters - projection
                projection > interval.endMeters -> projection - interval.endMeters
                else -> 0.0
            }
        }
    }

    private fun dot(first: VectorXZ, second: VectorXZ): Double =
        first.x * second.x + first.z * second.z

    private const val NUMERIC_EPSILON = 1e-9
}
