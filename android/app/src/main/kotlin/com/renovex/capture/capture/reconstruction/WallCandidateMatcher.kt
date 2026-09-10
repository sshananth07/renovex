package com.renovex.capture.capture.reconstruction

import kotlin.math.abs
import kotlin.math.acos
import kotlin.math.max
import kotlin.math.min

data class WallCandidateMatch(
    val candidateId: String,
    val score: Double,
    val angleDifferenceRadians: Double,
    val supportingLineDistanceMeters: Double,
    val extentGapMeters: Double,
    val incomingIntervalInCandidateSpace: ProjectionInterval,
)

/** Applies configurable geometric gates before ranking plausible supporting walls. */
object WallCandidateMatcher {
    fun bestMatch(
        observation: WallObservation,
        candidates: List<WallCandidate>,
        config: WallTrackingConfig,
    ): WallCandidateMatch? = candidates
        .mapNotNull { evaluate(observation, it, config) }
        .sortedWith(compareByDescending<WallCandidateMatch> { it.score }.thenBy { it.candidateId })
        .firstOrNull()

    fun evaluate(
        observation: WallObservation,
        candidate: WallCandidate,
        config: WallTrackingConfig,
    ): WallCandidateMatch? {
        val angle = undirectedAngle(observation.unitDirection, candidate.unitDirection)
        if (angle > config.maxAngleDifferenceRadians + NUMERIC_EPSILON) return null

        val observationMidpoint = pointOnLine(
            observation.unitNormal,
            observation.supportingLineOffsetMeters,
            observation.unitDirection,
            (observation.observedStartProjectionMeters + observation.observedEndProjectionMeters) / 2.0,
        )
        val candidateMidpoint = pointOnLine(
            candidate.unitNormal,
            candidate.supportingLineOffsetMeters,
            candidate.unitDirection,
            (candidate.observedStartProjectionMeters + candidate.observedEndProjectionMeters) / 2.0,
        )
        val lineDistance = max(
            pointLineDistance(observationMidpoint, candidate.unitNormal, candidate.supportingLineOffsetMeters),
            pointLineDistance(candidateMidpoint, observation.unitNormal, observation.supportingLineOffsetMeters),
        )
        if (lineDistance > config.maxSupportingLineOffsetMeters + NUMERIC_EPSILON) return null

        val incomingInterval = projectObservationOnto(observation, candidate.unitDirection)
        val extentGap = candidate.observedIntervals.minOf { gap(it, incomingInterval) }
        if (extentGap > config.maxExtentGapMeters + NUMERIC_EPSILON) return null

        val angleScore = normalizedCloseness(angle, config.maxAngleDifferenceRadians)
        val offsetScore = normalizedCloseness(lineDistance, config.maxSupportingLineOffsetMeters)
        val extentScore = normalizedCloseness(extentGap, config.maxExtentGapMeters)
        val historyScore = min(candidate.observationCount / 5.0, 1.0)
        val weightTotal = config.matchAngleWeight + config.matchOffsetWeight +
            config.matchExtentWeight + config.matchHistoryWeight
        val score = (
            config.matchAngleWeight * angleScore +
                config.matchOffsetWeight * offsetScore +
                config.matchExtentWeight * extentScore +
                config.matchHistoryWeight * historyScore
            ) / weightTotal

        return WallCandidateMatch(
            candidateId = candidate.candidateId,
            score = score,
            angleDifferenceRadians = angle,
            supportingLineDistanceMeters = lineDistance,
            extentGapMeters = extentGap,
            incomingIntervalInCandidateSpace = incomingInterval,
        )
    }

    internal fun projectObservationOnto(
        observation: WallObservation,
        direction: VectorXZ,
    ): ProjectionInterval {
        val first = pointOnLine(
            observation.unitNormal,
            observation.supportingLineOffsetMeters,
            observation.unitDirection,
            observation.observedStartProjectionMeters,
        )
        val second = pointOnLine(
            observation.unitNormal,
            observation.supportingLineOffsetMeters,
            observation.unitDirection,
            observation.observedEndProjectionMeters,
        )
        val firstProjection = dot(first, direction)
        val secondProjection = dot(second, direction)
        return ProjectionInterval(min(firstProjection, secondProjection), max(firstProjection, secondProjection))
    }

    private fun undirectedAngle(first: VectorXZ, second: VectorXZ): Double =
        acos(abs(dot(first, second)).coerceIn(-1.0, 1.0))

    private fun pointOnLine(
        normal: VectorXZ,
        offset: Double,
        direction: VectorXZ,
        projection: Double,
    ): PointXZ = PointXZ(
        x = normal.x * offset + direction.x * projection,
        z = normal.z * offset + direction.z * projection,
    )

    private fun pointLineDistance(point: PointXZ, normal: VectorXZ, offset: Double): Double =
        abs(dot(point, normal) - offset)

    private fun dot(point: PointXZ, vector: VectorXZ): Double = point.x * vector.x + point.z * vector.z
    private fun dot(first: VectorXZ, second: VectorXZ): Double = first.x * second.x + first.z * second.z

    private fun gap(first: ProjectionInterval, second: ProjectionInterval): Double = when {
        first.endMeters < second.startMeters -> second.startMeters - first.endMeters
        second.endMeters < first.startMeters -> first.startMeters - second.endMeters
        else -> 0.0
    }

    private fun normalizedCloseness(value: Double, maximum: Double): Double =
        if (maximum <= NUMERIC_EPSILON) {
            if (value <= NUMERIC_EPSILON) 1.0 else 0.0
        } else {
            (1.0 - value / maximum).coerceIn(0.0, 1.0)
        }

    private const val NUMERIC_EPSILON = 1e-9
}
