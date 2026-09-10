package com.renovex.capture.ui.screens.scan

import androidx.compose.foundation.layout.size
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertContentDescriptionEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.ProjectionInterval
import com.renovex.capture.capture.reconstruction.RoomShell
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.VectorXZ
import com.renovex.capture.capture.reconstruction.WallCandidate
import com.renovex.capture.capture.reconstruction.WallCandidateState
import org.junit.Rule
import org.junit.Test

class ReconstructionMiniMapTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun progressive_semantics_distinguish_candidates_stable_walls_and_proposed_shell() {
        val developing = candidate("developing", WallCandidateState.CANDIDATE)
        val stable = candidate("stable", WallCandidateState.STABLE)
        val shell = RoomShellBuildResult.Complete(
            RoomShell(
                orderedCandidateIds = listOf("developing", "stable", "third"),
                orderedCorners = listOf(
                    PointXZ(0.0, 0.0), PointXZ(2.0, 0.0), PointXZ(1.0, 2.0),
                ),
                observedCoverageByCandidate = mapOf(
                    "developing" to 0.5,
                    "stable" to 1.0,
                    "third" to 1.0,
                ),
                confidence = 0.8,
            ),
        )
        composeRule.setContent {
            ReconstructionMiniMap(listOf(developing, stable), shell, Modifier.size(160.dp))
        }

        composeRule.onNodeWithTag("reconstruction-mini-map")
            .assertContentDescriptionEquals("2 wall candidates, 1 stable, proposed shell")
    }

    private fun candidate(id: String, state: WallCandidateState): WallCandidate = WallCandidate(
        candidateId = id,
        unitNormal = VectorXZ(0.0, 1.0),
        unitDirection = VectorXZ(1.0, 0.0),
        supportingLineOffsetMeters = 0.0,
        observedIntervals = listOf(ProjectionInterval(0.0, 2.0)),
        observationCount = 2,
        firstObservedAtMillis = 0,
        lastObservedAtMillis = 100,
        angleStdDevRadians = 0.0,
        offsetStdDevMeters = 0.0,
        stability = if (state == WallCandidateState.STABLE) 1.0 else 0.5,
        state = state,
    )
}
