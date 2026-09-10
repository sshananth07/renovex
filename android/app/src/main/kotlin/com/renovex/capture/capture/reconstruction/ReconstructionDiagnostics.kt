package com.renovex.capture.capture.reconstruction

enum class ShellState { INCOMPLETE, PROPOSED, CONFIRMED }

data class WallObservationSourceDiagnostics(
    val canonicalPlaneId: String,
    val observationCount: Int,
    val firstObservedAtMillis: Long,
    val lastObservedAtMillis: Long,
)

/** Stage-level evidence for tuning without treating thresholds as truth. */
data class WallCandidateDiagnostics(
    val candidateId: String,
    val observationCount: Int,
    val angleVarianceRadiansSquared: Double,
    val offsetVarianceMetersSquared: Double,
    val observedExtentMeters: Double,
    val lastSeenMillis: Long,
    val stability: Double,
    val stable: Boolean,
    val observationSources: List<WallObservationSourceDiagnostics> = emptyList(),
)

data class ReconstructionDiagnostics(
    val rawVerticalPlaneCount: Int,
    val canonicalPlaneCount: Int,
    val wallObservationCount: Int,
    val wallCandidateCount: Int,
    val stableWallCount: Int,
    val shellState: ShellState,
    val shellConfidence: Double,
    val candidates: List<WallCandidateDiagnostics>,
    val orderedShellCandidateIds: List<String> = emptyList(),
) {
    fun compactLineageSummary(): String {
        val candidatesById = candidates.associateBy { it.candidateId }
        val sources = orderedShellCandidateIds.joinToString(",") { candidateId ->
            val sourceSummary = candidatesById[candidateId]?.observationSources.orEmpty()
                .joinToString(",") { source ->
                    "${source.canonicalPlaneId}(${source.observationCount})"
                }
            "$candidateId<-$sourceSummary"
        }
        return "shellEdges=[${orderedShellCandidateIds.joinToString(",")}] sources=[$sources]"
    }

    companion object {
        /**
         * Counts trackable identity, not update volume. This is deliberately
         * where child/parent duplication becomes visible during device tuning.
         */
        fun calculate(
            rawVerticalPlaneIds: Collection<String>,
            canonicalPlaneIds: Collection<String>,
            wallObservationCount: Int,
            candidates: List<WallCandidateDiagnostics>,
            shellState: ShellState,
            shellConfidence: Double,
            orderedShellCandidateIds: List<String> = emptyList(),
        ): ReconstructionDiagnostics = ReconstructionDiagnostics(
            rawVerticalPlaneCount = rawVerticalPlaneIds.toSet().size,
            canonicalPlaneCount = canonicalPlaneIds.toSet().size,
            wallObservationCount = wallObservationCount,
            wallCandidateCount = candidates.size,
            stableWallCount = candidates.count { it.stable },
            shellState = shellState,
            shellConfidence = shellConfidence,
            candidates = candidates,
            orderedShellCandidateIds = orderedShellCandidateIds,
        )
    }
}
