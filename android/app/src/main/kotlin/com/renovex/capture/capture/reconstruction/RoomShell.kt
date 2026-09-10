package com.renovex.capture.capture.reconstruction

data class RoomShell(
    val orderedCandidateIds: List<String>,
    val orderedCorners: List<PointXZ>,
    val observedCoverageByCandidate: Map<String, Double>,
    val confidence: Double,
)

sealed interface RoomShellBuildResult {
    data class Complete(val shell: RoomShell) : RoomShellBuildResult

    data class Incomplete(
        val orderedCandidateChains: List<List<String>>,
        val confidence: Double,
    ) : RoomShellBuildResult
}
