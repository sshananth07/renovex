package com.renovex.capture.ui.screens.scan

import com.renovex.capture.capture.reconstruction.Guidance
import com.renovex.capture.capture.reconstruction.ObservationTrackingState
import com.renovex.capture.capture.reconstruction.PlaneObservation
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.ReconstructionDiagnostics
import com.renovex.capture.capture.reconstruction.RoomScanGuidance
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.RoomShellBuilder
import com.renovex.capture.capture.reconstruction.ScanProgress
import com.renovex.capture.capture.reconstruction.ScanProgressEstimator
import com.renovex.capture.capture.reconstruction.ScannerParameters
import com.renovex.capture.capture.reconstruction.ShellState
import com.renovex.capture.capture.reconstruction.WallCandidate
import com.renovex.capture.capture.reconstruction.WallCandidateDiagnostics
import com.renovex.capture.capture.reconstruction.WallCandidateState
import com.renovex.capture.capture.reconstruction.WallCandidateTracker
import com.renovex.capture.capture.reconstruction.WallIntersection
import com.renovex.capture.capture.reconstruction.WallIntersectionResolver
import com.renovex.capture.capture.reconstruction.WallObservationFactory

data class ScanReconstructionState(
    val candidates: List<WallCandidate>,
    val intersections: List<WallIntersection>,
    val shellResult: RoomShellBuildResult,
    val progress: ScanProgress,
    val guidance: Guidance,
    val diagnostics: ReconstructionDiagnostics,
)

/** Owns the stateful reconstruction pipeline for one active scanner session. */
class ScanReconstructionController(
    private val parameters: ScannerParameters = ScannerParameters(),
    candidateIdProvider: (() -> String)? = null,
) {
    private var nextCandidateId = 0L
    private val tracker = WallCandidateTracker(parameters.wallTracking) {
        candidateIdProvider?.invoke() ?: "wall-${++nextCandidateId}"
    }
    private val canonicalIdByRawPlaneId = linkedMapOf<String, String>()
    private val cameraTrajectory = mutableListOf<PointXZ>()
    private var wallObservationCount = 0

    fun update(
        planeObservations: List<PlaneObservation>,
        cameraPosition: PointXZ,
        cameraHeadingRadians: Double,
    ): ScanReconstructionState {
        require(cameraPosition.x.isFinite() && cameraPosition.z.isFinite())
        require(cameraHeadingRadians.isFinite())
        planeObservations.forEach { observation ->
            canonicalIdByRawPlaneId[observation.planeId] = observation.canonicalPlaneId
        }
        val trackingEvidence = planeObservations
            .filter { it.trackingState == ObservationTrackingState.TRACKING }
            .map(WallObservationFactory::fromPlane)
        wallObservationCount += trackingEvidence.size
        tracker.acceptAll(trackingEvidence)
        cameraTrajectory += cameraPosition

        val candidates = tracker.snapshot()
        val intersections = WallIntersectionResolver.resolve(candidates, parameters.intersections)
        val shellResult = RoomShellBuilder.build(
            stableCandidates = candidates,
            intersections = intersections,
            cameraTrajectory = cameraTrajectory,
            config = parameters.shell,
        )
        val progress = ScanProgressEstimator.estimate(
            shellResult,
            candidates,
            cameraPosition,
            parameters.progress,
        )
        val guidance = RoomScanGuidance.guidanceFor(
            shellResult,
            candidates,
            cameraPosition,
            cameraHeadingRadians,
            parameters.progress,
        )
        val shellState = if (shellResult is RoomShellBuildResult.Complete) {
            ShellState.PROPOSED
        } else {
            ShellState.INCOMPLETE
        }
        val shellConfidence = when (shellResult) {
            is RoomShellBuildResult.Complete -> shellResult.shell.confidence
            is RoomShellBuildResult.Incomplete -> shellResult.confidence
        }
        val observationSourcesByCandidate = tracker.observationSourcesByCandidate()
        val candidateDiagnostics = candidates.map { candidate ->
            WallCandidateDiagnostics(
                candidateId = candidate.candidateId,
                observationCount = candidate.observationCount,
                angleVarianceRadiansSquared = candidate.angleStdDevRadians * candidate.angleStdDevRadians,
                offsetVarianceMetersSquared = candidate.offsetStdDevMeters * candidate.offsetStdDevMeters,
                observedExtentMeters = candidate.observedEndProjectionMeters -
                    candidate.observedStartProjectionMeters,
                lastSeenMillis = candidate.lastObservedAtMillis,
                stability = candidate.stability,
                stable = candidate.state == WallCandidateState.STABLE,
                observationSources = observationSourcesByCandidate[candidate.candidateId].orEmpty(),
            )
        }
        val diagnostics = ReconstructionDiagnostics.calculate(
            rawVerticalPlaneIds = canonicalIdByRawPlaneId.keys,
            canonicalPlaneIds = canonicalIdByRawPlaneId.values,
            wallObservationCount = wallObservationCount,
            candidates = candidateDiagnostics,
            shellState = shellState,
            shellConfidence = shellConfidence,
            orderedShellCandidateIds = when (shellResult) {
                is RoomShellBuildResult.Complete -> shellResult.shell.orderedCandidateIds
                is RoomShellBuildResult.Incomplete -> emptyList()
            },
        )
        return ScanReconstructionState(
            candidates = candidates,
            intersections = intersections,
            shellResult = shellResult,
            progress = progress,
            guidance = guidance,
            diagnostics = diagnostics,
        )
    }
}
