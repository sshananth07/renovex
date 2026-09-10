package com.renovex.capture.ui.screens.scan

import com.renovex.capture.capture.reconstruction.CanonicalPlaneInput
import com.renovex.capture.capture.reconstruction.IntersectionConfig
import com.renovex.capture.capture.reconstruction.ObservationTrackingState
import com.renovex.capture.capture.reconstruction.PlaneObservation
import com.renovex.capture.capture.reconstruction.PlaneObservationExtractor
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.RoomShellConfig
import com.renovex.capture.capture.reconstruction.ScanProgressConfig
import com.renovex.capture.capture.reconstruction.ScannerParameters
import com.renovex.capture.capture.reconstruction.ShellState
import com.renovex.capture.capture.reconstruction.VectorXZ
import com.renovex.capture.capture.reconstruction.WallCandidateState
import com.renovex.capture.capture.reconstruction.WallTrackingConfig
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ScanReconstructionControllerTest {

    private val parameters = ScannerParameters(
        wallTracking = WallTrackingConfig(
            maxAngleDifferenceRadians = 0.16,
            maxSupportingLineOffsetMeters = 0.22,
            maxExtentGapMeters = 0.32,
            minStandaloneExtentMeters = 0.12,
            stableObservationCount = 2,
            stableDurationMillis = 100,
            maxStableAngleStdDevRadians = 0.04,
            maxStableOffsetStdDevMeters = 0.05,
            directionSmoothingAlpha = 0.30,
            offsetSmoothingAlpha = 0.30,
            matchAngleWeight = 0.43,
            matchOffsetWeight = 0.31,
            matchExtentWeight = 0.17,
            matchHistoryWeight = 0.09,
            stabilityCountWeight = 0.28,
            stabilityDurationWeight = 0.26,
            stabilityAngleWeight = 0.24,
            stabilityOffsetWeight = 0.22,
        ),
        intersections = IntersectionConfig(
            minimumAngleRadians = 0.24,
            maximumObservedExtentExtensionMeters = 0.55,
            perpendicularBoostWindowRadians = 0.21,
            maximumPerpendicularConfidenceBoost = 0.04,
        ),
        shell = RoomShellConfig(
            candidateStabilityWeight = 0.40,
            intersectionConfidenceWeight = 0.30,
            observedCoverageWeight = 0.30,
            cameraPathWeight = 0.0,
            minimumProposedShellConfidence = 0.60,
            boundaryEpsilonMeters = 0.02,
            minimumInteriorCameraClearanceMeters = 0.30,
            minimumInteriorCameraEvidenceSamples = 2,
        ),
        progress = ScanProgressConfig(
            sufficientWallEvidence = 0.74,
            weakWallEvidence = 0.39,
            aheadHalfAngleRadians = 0.44,
            behindHalfAngleRadians = 0.33,
        ),
    )

    @Test
    fun `noisy plane updates progress through candidate stable and proposed shell states`() {
        val controller = controller()

        val developing = controller.update(
            rectangle(timestamp = 0, offsetNoise = 0.0),
            cameraPosition = PointXZ(2.0, 1.5),
            cameraHeadingRadians = 0.0,
        )
        assertEquals(4, developing.candidates.size)
        assertTrue(developing.candidates.all { it.state == WallCandidateState.CANDIDATE })
        assertEquals(ShellState.INCOMPLETE, developing.diagnostics.shellState)

        val proposed = controller.update(
            rectangle(timestamp = 100, offsetNoise = 0.01),
            cameraPosition = PointXZ(2.1, 1.5),
            cameraHeadingRadians = 0.0,
        )
        assertEquals(4, proposed.candidates.size)
        assertTrue(proposed.candidates.all { it.state == WallCandidateState.STABLE })
        assertEquals(4, proposed.intersections.size)
        assertTrue(proposed.shellResult is RoomShellBuildResult.Complete)
        assertEquals(ShellState.PROPOSED, proposed.diagnostics.shellState)
        assertEquals(4, proposed.progress.mappedWallCount)
        val shell = (proposed.shellResult as RoomShellBuildResult.Complete).shell
        assertEquals(shell.orderedCandidateIds, proposed.diagnostics.orderedShellCandidateIds)
        assertTrue(proposed.diagnostics.candidates.all { candidate ->
            candidate.observationSources.size == 1 &&
                candidate.observationSources.single().observationCount == 2
        })
    }

    @Test
    fun `subsumed child and canonical parent remain one wall candidate`() {
        val controller = controller()
        val child = horizontal(
            rawId = "child",
            canonicalId = "parent",
            z = 0.0,
            timestamp = 0,
        )
        val parent = horizontal(
            rawId = "parent",
            canonicalId = "parent",
            z = 0.01,
            timestamp = 100,
        )

        controller.update(listOf(child), PointXZ(1.0, 1.0), 0.0)
        val state = controller.update(listOf(parent), PointXZ(1.0, 1.0), 0.0)

        assertEquals(2, state.diagnostics.rawVerticalPlaneCount)
        assertEquals(1, state.diagnostics.canonicalPlaneCount)
        assertEquals(1, state.candidates.size)
        assertEquals(2, state.candidates.single().observationCount)
    }

    @Test
    fun `stopped update does not delete or demote a stable wall`() {
        val controller = controller()
        controller.update(listOf(horizontal("wall", "wall", 0.0, 0)), PointXZ(0.0, 1.0), 0.0)
        controller.update(listOf(horizontal("wall", "wall", 0.0, 100)), PointXZ(0.0, 1.0), 0.0)

        val stopped = horizontal("wall", "wall", 0.0, 200).copy(
            trackingState = ObservationTrackingState.STOPPED,
        )
        val state = controller.update(listOf(stopped), PointXZ(0.0, 1.0), 0.0)

        assertEquals(1, state.candidates.size)
        assertEquals(WallCandidateState.STABLE, state.candidates.single().state)
        assertEquals(2, state.candidates.single().observationCount)
    }

    private fun controller(): ScanReconstructionController {
        var id = 0
        return ScanReconstructionController(parameters) { "candidate-${++id}" }
    }

    private fun rectangle(timestamp: Long, offsetNoise: Double): List<PlaneObservation> = listOf(
        horizontal("bottom-$timestamp", "bottom", 0.0 + offsetNoise, timestamp),
        vertical("right-$timestamp", "right", 4.0 + offsetNoise, timestamp),
        horizontal("top-$timestamp", "top", 3.0 + offsetNoise, timestamp),
        vertical("left-$timestamp", "left", 0.0 + offsetNoise, timestamp),
    )

    private fun horizontal(
        rawId: String,
        canonicalId: String,
        z: Double,
        timestamp: Long,
    ): PlaneObservation = requireNotNull(
        PlaneObservationExtractor.extract(
            CanonicalPlaneInput(
                planeId = rawId,
                canonicalPlaneId = canonicalId,
                center = PointXZ(2.0, z),
                worldNormal = VectorXZ(0.0, 1.0),
                worldPolygon = listOf(PointXZ(0.0, z), PointXZ(4.0, z)),
                timestampMillis = timestamp,
                trackingState = ObservationTrackingState.TRACKING,
            ),
        ),
    )

    private fun vertical(
        rawId: String,
        canonicalId: String,
        x: Double,
        timestamp: Long,
    ): PlaneObservation = requireNotNull(
        PlaneObservationExtractor.extract(
            CanonicalPlaneInput(
                planeId = rawId,
                canonicalPlaneId = canonicalId,
                center = PointXZ(x, 1.5),
                worldNormal = VectorXZ(1.0, 0.0),
                worldPolygon = listOf(PointXZ(x, 0.0), PointXZ(x, 3.0)),
                timestampMillis = timestamp,
                trackingState = ObservationTrackingState.TRACKING,
            ),
        ),
    )
}
