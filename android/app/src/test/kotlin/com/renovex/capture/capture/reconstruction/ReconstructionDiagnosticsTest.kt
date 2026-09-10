package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Test

class ReconstructionDiagnosticsTest {

    @Test
    fun `diagnostics distinguishes raw planes canonical planes candidates and stable walls`() {
        // Break caught: reporting collection size instead of distinct plane
        // identity would hide the exact child-plus-parent duplication this
        // reconstruction architecture is intended to diagnose.
        val diagnostics = ReconstructionDiagnostics.calculate(
            rawVerticalPlaneIds = listOf("plane-child", "plane-child", "plane-parent"),
            canonicalPlaneIds = listOf("plane-parent", "plane-parent"),
            wallObservationCount = 5,
            candidates = listOf(
                WallCandidateDiagnostics(
                    candidateId = "wall-1",
                    observationCount = 3,
                    angleVarianceRadiansSquared = 0.001,
                    offsetVarianceMetersSquared = 0.002,
                    observedExtentMeters = 4.0,
                    lastSeenMillis = 600,
                    stability = 0.9,
                    stable = true,
                ),
                WallCandidateDiagnostics(
                    candidateId = "wall-2",
                    observationCount = 1,
                    angleVarianceRadiansSquared = 0.02,
                    offsetVarianceMetersSquared = 0.03,
                    observedExtentMeters = 0.8,
                    lastSeenMillis = 600,
                    stability = 0.3,
                    stable = false,
                ),
            ),
            shellState = ShellState.INCOMPLETE,
            shellConfidence = 0.42,
        )

        assertEquals(2, diagnostics.rawVerticalPlaneCount)
        assertEquals(1, diagnostics.canonicalPlaneCount)
        assertEquals(5, diagnostics.wallObservationCount)
        assertEquals(2, diagnostics.wallCandidateCount)
        assertEquals(1, diagnostics.stableWallCount)
        assertEquals(ShellState.INCOMPLETE, diagnostics.shellState)
        assertEquals(0.42, diagnostics.shellConfidence, 0.0)
    }

    @Test
    fun `compact lineage keeps selected shell edges traceable within device logs`() {
        // Break caught: logging the full diagnostic object can exceed Android's
        // line limit before ordered shell IDs and their plane sources appear.
        val diagnostics = ReconstructionDiagnostics(
            rawVerticalPlaneCount = 3,
            canonicalPlaneCount = 3,
            wallObservationCount = 6,
            wallCandidateCount = 2,
            stableWallCount = 2,
            shellState = ShellState.PROPOSED,
            shellConfidence = 0.8,
            candidates = listOf(
                WallCandidateDiagnostics(
                    candidateId = "wall-1",
                    observationCount = 4,
                    angleVarianceRadiansSquared = 0.0,
                    offsetVarianceMetersSquared = 0.0,
                    observedExtentMeters = 2.0,
                    lastSeenMillis = 300,
                    stability = 0.9,
                    stable = true,
                    observationSources = listOf(
                        WallObservationSourceDiagnostics("plane-a", 3, 0, 300),
                        WallObservationSourceDiagnostics("plane-b", 1, 100, 100),
                    ),
                ),
                WallCandidateDiagnostics(
                    candidateId = "wall-2",
                    observationCount = 2,
                    angleVarianceRadiansSquared = 0.0,
                    offsetVarianceMetersSquared = 0.0,
                    observedExtentMeters = 1.0,
                    lastSeenMillis = 300,
                    stability = 0.9,
                    stable = true,
                    observationSources = listOf(
                        WallObservationSourceDiagnostics("plane-c", 2, 0, 300),
                    ),
                ),
            ),
            orderedShellCandidateIds = listOf("wall-2", "wall-1"),
        )

        assertEquals(
            "shellEdges=[wall-2,wall-1] sources=[wall-2<-plane-c(2),wall-1<-plane-a(3),plane-b(1)]",
            diagnostics.compactLineageSummary(),
        )
    }
}
