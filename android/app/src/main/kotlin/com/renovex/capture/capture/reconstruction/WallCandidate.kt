package com.renovex.capture.capture.reconstruction

data class ProjectionInterval(
    val startMeters: Double,
    val endMeters: Double,
) {
    init {
        require(startMeters.isFinite() && endMeters.isFinite() && startMeters <= endMeters)
    }
}

enum class WallCandidateState { CANDIDATE, STABLE }

/** Stateful estimate of one physical supporting wall line. */
data class WallCandidate(
    val candidateId: String,
    val unitNormal: VectorXZ,
    val unitDirection: VectorXZ,
    val supportingLineOffsetMeters: Double,
    val observedIntervals: List<ProjectionInterval>,
    val observationCount: Int,
    val firstObservedAtMillis: Long,
    val lastObservedAtMillis: Long,
    val angleStdDevRadians: Double,
    val offsetStdDevMeters: Double,
    val stability: Double,
    val state: WallCandidateState,
) {
    val observedStartProjectionMeters: Double
        get() = observedIntervals.minOf { it.startMeters }

    val observedEndProjectionMeters: Double
        get() = observedIntervals.maxOf { it.endMeters }

    companion object {
        fun fromObservation(candidateId: String, observation: WallObservation): WallCandidate =
            WallCandidate(
                candidateId = candidateId,
                unitNormal = observation.unitNormal,
                unitDirection = observation.unitDirection,
                supportingLineOffsetMeters = observation.supportingLineOffsetMeters,
                observedIntervals = listOf(
                    ProjectionInterval(
                        observation.observedStartProjectionMeters,
                        observation.observedEndProjectionMeters,
                    ),
                ),
                observationCount = 1,
                firstObservedAtMillis = observation.timestampMillis,
                lastObservedAtMillis = observation.timestampMillis,
                angleStdDevRadians = 0.0,
                offsetStdDevMeters = 0.0,
                stability = 0.0,
                state = WallCandidateState.CANDIDATE,
            )
    }
}
