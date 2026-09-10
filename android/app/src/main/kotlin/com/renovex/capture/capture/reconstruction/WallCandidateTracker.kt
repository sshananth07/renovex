package com.renovex.capture.capture.reconstruction

import kotlin.math.abs
import kotlin.math.atan2
import kotlin.math.hypot
import kotlin.math.min
import kotlin.math.sqrt

/**
 * Accumulates observations into persistent wall candidates. It never merges candidates with
 * each other, which prevents one bridge observation from creating a transitive wall merge.
 */
class WallCandidateTracker(
    private val config: WallTrackingConfig,
    private val candidateIdProvider: () -> String,
) {
    private val candidatesById = linkedMapOf<String, WallCandidate>()
    private val evidenceStatsById = mutableMapOf<String, EvidenceStats>()
    private val observationSourceStatsByCandidateId =
        mutableMapOf<String, MutableMap<String, ObservationSourceStats>>()

    fun acceptAll(observations: Iterable<WallObservation>) {
        observations
            .sortedWith(compareBy<WallObservation> { it.timestampMillis }.thenBy { it.canonicalPlaneId })
            .forEach(::accept)
    }

    fun accept(observation: WallObservation) {
        require(observation.observedStartProjectionMeters <= observation.observedEndProjectionMeters)
        val match = WallCandidateMatcher.bestMatch(observation, candidatesById.values.toList(), config)
        if (match != null) {
            val current = requireNotNull(candidatesById[match.candidateId])
            candidatesById[match.candidateId] = updateCandidate(current, observation)
            recordObservationSource(match.candidateId, observation)
            return
        }

        val length = observation.observedEndProjectionMeters - observation.observedStartProjectionMeters
        if (length + NUMERIC_EPSILON < config.minStandaloneExtentMeters) return
        val initial = WallCandidate.fromObservation(candidateIdProvider(), observation)
        val initialStats = EvidenceStats.from(observation)
        val candidate = withStability(initial, initialStats)
        require(candidate.candidateId.isNotBlank()) { "candidate ID must not be blank" }
        require(candidate.candidateId !in candidatesById) { "candidate ID must be unique" }
        candidatesById[candidate.candidateId] = candidate
        evidenceStatsById[candidate.candidateId] = initialStats
        recordObservationSource(candidate.candidateId, observation)
    }

    fun snapshot(): List<WallCandidate> = candidatesById.values.sortedBy { it.candidateId }

    fun observationSourcesByCandidate(): Map<String, List<WallObservationSourceDiagnostics>> =
        candidatesById.keys.associateWith { candidateId ->
            observationSourceStatsByCandidateId[candidateId].orEmpty()
                .toSortedMap()
                .map { (canonicalPlaneId, stats) ->
                    WallObservationSourceDiagnostics(
                        canonicalPlaneId = canonicalPlaneId,
                        observationCount = stats.observationCount,
                        firstObservedAtMillis = stats.firstObservedAtMillis,
                        lastObservedAtMillis = stats.lastObservedAtMillis,
                    )
                }
        }

    private fun recordObservationSource(candidateId: String, observation: WallObservation) {
        val sources = observationSourceStatsByCandidateId.getOrPut(candidateId) { linkedMapOf() }
        sources[observation.canonicalPlaneId] = sources[observation.canonicalPlaneId]
            ?.withObservation(observation.timestampMillis)
            ?: ObservationSourceStats(
                observationCount = 1,
                firstObservedAtMillis = observation.timestampMillis,
                lastObservedAtMillis = observation.timestampMillis,
            )
    }

    private fun updateCandidate(
        current: WallCandidate,
        observation: WallObservation,
    ): WallCandidate {
        val alignment = dot(current.unitNormal, observation.unitNormal)
        val alignedNormal = if (alignment < 0.0) -observation.unitNormal else observation.unitNormal
        val alignedOffset = if (alignment < 0.0) {
            -observation.supportingLineOffsetMeters
        } else {
            observation.supportingLineOffsetMeters
        }

        val rawNormal = VectorXZ(
            x = (1.0 - config.directionSmoothingAlpha) * current.unitNormal.x +
                config.directionSmoothingAlpha * alignedNormal.x,
            z = (1.0 - config.directionSmoothingAlpha) * current.unitNormal.z +
                config.directionSmoothingAlpha * alignedNormal.z,
        )
        val rawLength = hypot(rawNormal.x, rawNormal.z)
        require(rawLength > NUMERIC_EPSILON) { "smoothed wall normal is degenerate" }
        val smoothedNormal = VectorXZ(rawNormal.x / rawLength, rawNormal.z / rawLength)
        var smoothedDirection = VectorXZ(-smoothedNormal.z, smoothedNormal.x)
        if (dot(smoothedDirection, current.unitDirection) < 0.0) {
            smoothedDirection = -smoothedDirection
        }
        val smoothedOffset = (1.0 - config.offsetSmoothingAlpha) *
            current.supportingLineOffsetMeters + config.offsetSmoothingAlpha * alignedOffset

        val reprojectedPrevious = current.observedIntervals.map { interval ->
            reprojectInterval(
                interval = interval,
                oldNormal = current.unitNormal,
                oldDirection = current.unitDirection,
                oldOffset = current.supportingLineOffsetMeters,
                newDirection = smoothedDirection,
            )
        }
        val incoming = WallCandidateMatcher.projectObservationOnto(observation, smoothedDirection)
        val updatedStats = requireNotNull(evidenceStatsById[current.candidateId]).add(
            observation,
            alignedOffset,
        )
        evidenceStatsById[current.candidateId] = updatedStats

        val updated = current.copy(
            unitNormal = smoothedNormal,
            unitDirection = smoothedDirection,
            supportingLineOffsetMeters = smoothedOffset,
            observedIntervals = mergeIntervals(reprojectedPrevious + incoming),
            observationCount = current.observationCount + 1,
            lastObservedAtMillis = maxOf(current.lastObservedAtMillis, observation.timestampMillis),
            angleStdDevRadians = updatedStats.angleStdDev,
            offsetStdDevMeters = updatedStats.offsetStdDev,
        )
        return withStability(updated, updatedStats)
    }

    private fun withStability(candidate: WallCandidate, stats: EvidenceStats): WallCandidate {
        val duration = candidate.lastObservedAtMillis - candidate.firstObservedAtMillis
        val countScore = (candidate.observationCount.toDouble() / config.stableObservationCount).coerceIn(0.0, 1.0)
        val durationScore = ratioScore(duration.toDouble(), config.stableDurationMillis.toDouble())
        val angleScore = inverseRatioScore(stats.angleStdDev, config.maxStableAngleStdDevRadians)
        val offsetScore = inverseRatioScore(stats.offsetStdDev, config.maxStableOffsetStdDevMeters)
        val weightTotal = config.stabilityCountWeight + config.stabilityDurationWeight +
            config.stabilityAngleWeight + config.stabilityOffsetWeight
        val stability = (
            config.stabilityCountWeight * countScore +
                config.stabilityDurationWeight * durationScore +
                config.stabilityAngleWeight * angleScore +
                config.stabilityOffsetWeight * offsetScore
            ) / weightTotal
        val passesHardGates = candidate.observationCount >= config.stableObservationCount &&
            duration >= config.stableDurationMillis &&
            stats.angleStdDev <= config.maxStableAngleStdDevRadians + NUMERIC_EPSILON &&
            stats.offsetStdDev <= config.maxStableOffsetStdDevMeters + NUMERIC_EPSILON
        val state = if (candidate.state == WallCandidateState.STABLE || passesHardGates) {
            WallCandidateState.STABLE
        } else {
            WallCandidateState.CANDIDATE
        }
        return candidate.copy(
            angleStdDevRadians = stats.angleStdDev,
            offsetStdDevMeters = stats.offsetStdDev,
            stability = stability.coerceIn(0.0, 1.0),
            state = state,
        )
    }

    private fun reprojectInterval(
        interval: ProjectionInterval,
        oldNormal: VectorXZ,
        oldDirection: VectorXZ,
        oldOffset: Double,
        newDirection: VectorXZ,
    ): ProjectionInterval {
        val first = pointOnLine(oldNormal, oldOffset, oldDirection, interval.startMeters)
        val second = pointOnLine(oldNormal, oldOffset, oldDirection, interval.endMeters)
        val firstProjection = dot(first, newDirection)
        val secondProjection = dot(second, newDirection)
        return ProjectionInterval(minOf(firstProjection, secondProjection), maxOf(firstProjection, secondProjection))
    }

    private fun mergeIntervals(intervals: List<ProjectionInterval>): List<ProjectionInterval> {
        val sorted = intervals.sortedBy { it.startMeters }
        if (sorted.isEmpty()) return emptyList()
        val merged = mutableListOf(sorted.first())
        for (next in sorted.drop(1)) {
            val previous = merged.last()
            if (next.startMeters <= previous.endMeters + NUMERIC_EPSILON) {
                merged[merged.lastIndex] = ProjectionInterval(
                    previous.startMeters,
                    maxOf(previous.endMeters, next.endMeters),
                )
            } else {
                merged += next
            }
        }
        return merged
    }

    private fun ratioScore(value: Double, target: Double): Double =
        if (target <= NUMERIC_EPSILON) 1.0 else (value / target).coerceIn(0.0, 1.0)

    private fun inverseRatioScore(value: Double, maximum: Double): Double =
        if (maximum <= NUMERIC_EPSILON) {
            if (value <= NUMERIC_EPSILON) 1.0 else 0.0
        } else {
            (1.0 - value / maximum).coerceIn(0.0, 1.0)
        }

    private fun pointOnLine(
        normal: VectorXZ,
        offset: Double,
        direction: VectorXZ,
        projection: Double,
    ): PointXZ = PointXZ(
        x = normal.x * offset + direction.x * projection,
        z = normal.z * offset + direction.z * projection,
    )

    private fun dot(first: VectorXZ, second: VectorXZ): Double = first.x * second.x + first.z * second.z
    private fun dot(point: PointXZ, vector: VectorXZ): Double = point.x * vector.x + point.z * vector.z
    private operator fun VectorXZ.unaryMinus(): VectorXZ = VectorXZ(-x, -z)

    private data class EvidenceStats(
        val referenceNormal: VectorXZ,
        val count: Int,
        val meanAngle: Double,
        val angleM2: Double,
        val meanOffset: Double,
        val offsetM2: Double,
    ) {
        val angleStdDev: Double
            get() = sqrt(angleM2 / count)

        val offsetStdDev: Double
            get() = sqrt(offsetM2 / count)

        fun add(observation: WallObservation, alignedOffset: Double): EvidenceStats {
            var normal = observation.unitNormal
            if (referenceNormal.x * normal.x + referenceNormal.z * normal.z < 0.0) {
                normal = VectorXZ(-normal.x, -normal.z)
            }
            val cross = referenceNormal.x * normal.z - referenceNormal.z * normal.x
            val cosine = (referenceNormal.x * normal.x + referenceNormal.z * normal.z).coerceIn(-1.0, 1.0)
            val angle = atan2(cross, cosine)
            val nextCount = count + 1
            val angleDelta = angle - meanAngle
            val nextMeanAngle = meanAngle + angleDelta / nextCount
            val nextAngleM2 = angleM2 + angleDelta * (angle - nextMeanAngle)
            val offsetDelta = alignedOffset - meanOffset
            val nextMeanOffset = meanOffset + offsetDelta / nextCount
            val nextOffsetM2 = offsetM2 + offsetDelta * (alignedOffset - nextMeanOffset)
            return copy(
                count = nextCount,
                meanAngle = nextMeanAngle,
                angleM2 = maxOf(0.0, nextAngleM2),
                meanOffset = nextMeanOffset,
                offsetM2 = maxOf(0.0, nextOffsetM2),
            )
        }

        companion object {
            fun from(observation: WallObservation): EvidenceStats = EvidenceStats(
                referenceNormal = observation.unitNormal,
                count = 1,
                meanAngle = 0.0,
                angleM2 = 0.0,
                meanOffset = observation.supportingLineOffsetMeters,
                offsetM2 = 0.0,
            )
        }
    }

    private data class ObservationSourceStats(
        val observationCount: Int,
        val firstObservedAtMillis: Long,
        val lastObservedAtMillis: Long,
    ) {
        fun withObservation(timestampMillis: Long): ObservationSourceStats = copy(
            observationCount = observationCount + 1,
            firstObservedAtMillis = minOf(firstObservedAtMillis, timestampMillis),
            lastObservedAtMillis = maxOf(lastObservedAtMillis, timestampMillis),
        )
    }

    private companion object {
        const val NUMERIC_EPSILON = 1e-9
    }
}
