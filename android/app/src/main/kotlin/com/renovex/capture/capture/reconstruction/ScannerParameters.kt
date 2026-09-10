package com.renovex.capture.capture.reconstruction

import kotlin.math.PI

/**
 * Tunable scanner policy. Defaults are initial operating values, not claims about buildings.
 * Capture-specific calibration can replace them without changing reconstruction algorithms.
 */
data class ScannerParameters(
    val wallTracking: WallTrackingConfig = WallTrackingConfig(),
    val intersections: IntersectionConfig = IntersectionConfig(),
    val shell: RoomShellConfig = RoomShellConfig(),
    val shellSimplification: RoomShellSimplificationConfig = RoomShellSimplificationConfig(),
    val proposalReadiness: ProposalReadinessConfig = ProposalReadinessConfig(),
    val progress: ScanProgressConfig = ScanProgressConfig(),
)

data class ProposalReadinessConfig(
    val minimumCoveragePercent: Int = 70,
    val minimumStableProposalFrames: Int = 3,
    val minimumShellConfidence: Double = 0.70,
    val requireUsableTracking: Boolean = true,
    val maximumStableCornerDriftMeters: Double = 0.05,
) {
    init {
        require(minimumCoveragePercent in 0..100)
        require(minimumStableProposalFrames > 0)
        require(minimumShellConfidence.isFinite() && minimumShellConfidence in 0.0..1.0)
        require(maximumStableCornerDriftMeters.isFinite() && maximumStableCornerDriftMeters >= 0.0)
    }
}

data class RoomShellSimplificationConfig(
    val maximumCollapsibleEdgeLengthMeters: Double = 0.12,
    val minimumObservedToArchitecturalExtentRatio: Double = 4.0,
    val maximumReplacementCornerDisplacementMeters: Double = 0.10,
) {
    init {
        require(maximumCollapsibleEdgeLengthMeters.isFinite() && maximumCollapsibleEdgeLengthMeters > 0.0)
        require(minimumObservedToArchitecturalExtentRatio.isFinite() && minimumObservedToArchitecturalExtentRatio > 1.0)
        require(maximumReplacementCornerDisplacementMeters.isFinite() && maximumReplacementCornerDisplacementMeters >= 0.0)
    }
}

data class IntersectionConfig(
    val minimumAngleRadians: Double = Math.toRadians(15.0),
    val maximumObservedExtentExtensionMeters: Double = 1.5,
    val perpendicularBoostWindowRadians: Double = Math.toRadians(10.0),
    val maximumPerpendicularConfidenceBoost: Double = 0.10,
) {
    init {
        require(minimumAngleRadians.isFinite() && minimumAngleRadians in 0.0..(PI / 2.0))
        require(maximumObservedExtentExtensionMeters.isFinite() && maximumObservedExtentExtensionMeters > 0.0)
        require(perpendicularBoostWindowRadians.isFinite() && perpendicularBoostWindowRadians > 0.0)
        require(maximumPerpendicularConfidenceBoost.isFinite() && maximumPerpendicularConfidenceBoost >= 0.0)
    }
}

data class RoomShellConfig(
    val candidateStabilityWeight: Double = 0.35,
    val intersectionConfidenceWeight: Double = 0.30,
    val observedCoverageWeight: Double = 0.25,
    val cameraPathWeight: Double = 0.10,
    val minimumProposedShellConfidence: Double = 0.65,
    val boundaryEpsilonMeters: Double = 0.05,
    val minimumInteriorCameraClearanceMeters: Double = 0.30,
    val minimumInteriorCameraEvidenceSamples: Int = 3,
) {
    init {
        val weights = listOf(
            candidateStabilityWeight,
            intersectionConfidenceWeight,
            observedCoverageWeight,
            cameraPathWeight,
        )
        require(weights.all { it.isFinite() && it >= 0.0 })
        require(weights.sum() > 0.0)
        require(minimumProposedShellConfidence.isFinite() && minimumProposedShellConfidence in 0.0..1.0)
        require(boundaryEpsilonMeters.isFinite() && boundaryEpsilonMeters >= 0.0)
        require(minimumInteriorCameraClearanceMeters.isFinite() && minimumInteriorCameraClearanceMeters >= 0.0)
        require(minimumInteriorCameraEvidenceSamples > 0)
    }
}

data class ScanProgressConfig(
    val sufficientWallEvidence: Double = 0.70,
    val weakWallEvidence: Double = 0.45,
    val aheadHalfAngleRadians: Double = Math.toRadians(30.0),
    val behindHalfAngleRadians: Double = Math.toRadians(30.0),
) {
    init {
        require(sufficientWallEvidence.isFinite() && sufficientWallEvidence in 0.0..1.0)
        require(weakWallEvidence.isFinite() && weakWallEvidence in 0.0..sufficientWallEvidence)
        require(aheadHalfAngleRadians.isFinite() && aheadHalfAngleRadians in 0.0..(PI / 2.0))
        require(behindHalfAngleRadians.isFinite() && behindHalfAngleRadians in 0.0..(PI / 2.0))
    }
}

data class WallTrackingConfig(
    val maxAngleDifferenceRadians: Double = Math.toRadians(8.0),
    val maxSupportingLineOffsetMeters: Double = 0.20,
    val maxExtentGapMeters: Double = 1.25,
    val minStandaloneExtentMeters: Double = 0.25,
    val stableObservationCount: Int = 3,
    val stableDurationMillis: Long = 500,
    val maxStableAngleStdDevRadians: Double = Math.toRadians(3.0),
    val maxStableOffsetStdDevMeters: Double = 0.08,
    val directionSmoothingAlpha: Double = 0.25,
    val offsetSmoothingAlpha: Double = 0.25,
    val matchAngleWeight: Double = 0.40,
    val matchOffsetWeight: Double = 0.35,
    val matchExtentWeight: Double = 0.20,
    val matchHistoryWeight: Double = 0.05,
    val stabilityCountWeight: Double = 0.25,
    val stabilityDurationWeight: Double = 0.25,
    val stabilityAngleWeight: Double = 0.25,
    val stabilityOffsetWeight: Double = 0.25,
) {
    init {
        require(maxAngleDifferenceRadians.isFinite() && maxAngleDifferenceRadians in 0.0..(PI / 2.0))
        require(maxSupportingLineOffsetMeters.isFinite() && maxSupportingLineOffsetMeters > 0.0)
        require(maxExtentGapMeters.isFinite() && maxExtentGapMeters >= 0.0)
        require(minStandaloneExtentMeters.isFinite() && minStandaloneExtentMeters > 0.0)
        require(stableObservationCount > 0)
        require(stableDurationMillis >= 0)
        require(maxStableAngleStdDevRadians.isFinite() && maxStableAngleStdDevRadians >= 0.0)
        require(maxStableOffsetStdDevMeters.isFinite() && maxStableOffsetStdDevMeters >= 0.0)
        require(directionSmoothingAlpha.isFinite() && directionSmoothingAlpha in 0.0..1.0)
        require(offsetSmoothingAlpha.isFinite() && offsetSmoothingAlpha in 0.0..1.0)
        val matchWeights = listOf(matchAngleWeight, matchOffsetWeight, matchExtentWeight, matchHistoryWeight)
        require(matchWeights.all { it.isFinite() && it >= 0.0 })
        require(matchWeights.sum() > 0.0)
        val stabilityWeights = listOf(
            stabilityCountWeight,
            stabilityDurationWeight,
            stabilityAngleWeight,
            stabilityOffsetWeight,
        )
        require(stabilityWeights.all { it.isFinite() && it >= 0.0 })
        require(stabilityWeights.sum() > 0.0)
    }
}
