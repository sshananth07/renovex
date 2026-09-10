package com.renovex.capture.ui.screens.scan

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import com.renovex.capture.BuildConfig
import com.renovex.capture.capture.ScanSessionState
import com.renovex.capture.capture.TrackingQuality
import com.renovex.capture.capture.guidanceMessage
import com.renovex.capture.capture.reconstruction.Direction

@Composable
fun ScanModeHud(
    spaceName: String,
    state: ScanSessionState,
    reconstruction: ScanReconstructionState?,
    onReviewProposal: (() -> Unit)? = null,
) {
    var diagnosticsExpanded by remember { mutableStateOf(false) }
    Column(modifier = Modifier.fillMaxSize()) {
        Card(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            colors = CardDefaults.cardColors(containerColor = Color.Black.copy(alpha = 0.6f)),
        ) {
            Column(modifier = Modifier.padding(16.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (state.trackingQuality == TrackingQuality.GOOD) ScanningPulseIndicator()
                    Text(spaceName, color = Color.White, style = MaterialTheme.typography.titleMedium)
                }
                Text(
                    trackingIndicatorLabel(state.trackingQuality),
                    color = trackingIndicatorColor(state.trackingQuality),
                    modifier = Modifier.padding(top = 4.dp),
                )
                val guidanceText = if (state.trackingQuality == TrackingQuality.GOOD && reconstruction != null) {
                    guidanceArrow(reconstruction.guidance.relativeDirection) + " " + reconstruction.guidance.message
                } else {
                    state.trackingQuality.guidanceMessage()
                }
                Text(guidanceText, color = Color.White, modifier = Modifier.padding(top = 4.dp))
                val mappedWalls = reconstruction?.progress?.mappedWallCount ?: 0
                val provisional = if (reconstruction?.progress?.provisional == true) " provisional" else ""
                Text(
                    "Walls mapped: $mappedWalls   Coverage: ${state.coveragePercent}%$provisional",
                    color = Color.White,
                    modifier = Modifier.padding(top = 4.dp),
                )

                if (reconstruction?.candidates?.isNotEmpty() == true) {
                    ReconstructionMiniMap(
                        candidates = reconstruction.candidates,
                        shellResult = reconstruction.shellResult,
                        modifier = Modifier
                            .padding(top = 12.dp)
                            .size(140.dp)
                            .align(Alignment.CenterHorizontally)
                            .clip(MaterialTheme.shapes.medium)
                            .background(Color.Black.copy(alpha = 0.4f)),
                    )
                }

                Button(
                    onClick = { onReviewProposal?.invoke() },
                    enabled = onReviewProposal != null,
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                ) {
                    Text("Review proposed room")
                }

                if (BuildConfig.DEBUG && reconstruction != null) {
                    TextButton(onClick = { diagnosticsExpanded = !diagnosticsExpanded }) {
                        Text(if (diagnosticsExpanded) "Hide reconstruction diagnostics" else "Show reconstruction diagnostics")
                    }
                    if (diagnosticsExpanded) {
                        val diagnostics = reconstruction.diagnostics
                        Text(
                            "planes ${diagnostics.rawVerticalPlaneCount}/${diagnostics.canonicalPlaneCount}  " +
                                "observations ${diagnostics.wallObservationCount}  " +
                                "candidates ${diagnostics.wallCandidateCount}/${diagnostics.stableWallCount} stable  " +
                                "shell ${diagnostics.shellState} ${"%.2f".format(diagnostics.shellConfidence)}",
                            color = Color.White,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ScanningPulseIndicator() {
    val transition = rememberInfiniteTransition(label = "scan-pulse")
    val alpha by transition.animateFloat(
        initialValue = 0.3f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(800), RepeatMode.Reverse),
        label = "scan-pulse-alpha",
    )
    Box(
        modifier = Modifier
            .padding(end = 8.dp)
            .size(10.dp)
            .clip(MaterialTheme.shapes.small)
            .background(Color(0xFF4CAF50).copy(alpha = alpha)),
    )
}

private fun guidanceArrow(direction: Direction): String = when (direction) {
    Direction.AHEAD -> "↑"
    Direction.LEFT -> "←"
    Direction.RIGHT -> "→"
    Direction.BEHIND -> "↓"
    Direction.NONE -> "✓"
}

private fun trackingIndicatorLabel(quality: TrackingQuality): String = when (quality) {
    TrackingQuality.GOOD -> "● Tracking"
    TrackingQuality.NOT_STARTED -> "○ Starting…"
    TrackingQuality.STOPPED -> "● Stopped"
    else -> "● Limited tracking"
}

private fun trackingIndicatorColor(quality: TrackingQuality): Color = when (quality) {
    TrackingQuality.GOOD -> Color(0xFF4CAF50)
    TrackingQuality.NOT_STARTED -> Color.Gray
    TrackingQuality.STOPPED -> Color.Red
    else -> Color(0xFFFFA000)
}
