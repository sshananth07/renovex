package com.renovex.capture.ui.screens.scan

import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.RoomShellBuildResult
import com.renovex.capture.capture.reconstruction.WallCandidate
import com.renovex.capture.capture.reconstruction.WallCandidateState

/** Progressive map of candidate evidence and a supported proposed shell. */
@Composable
fun ReconstructionMiniMap(
    candidates: List<WallCandidate>,
    shellResult: RoomShellBuildResult,
    modifier: Modifier = Modifier,
) {
    val shell = (shellResult as? RoomShellBuildResult.Complete)?.shell
    val geometryPoints = buildList {
        candidates.forEach { candidate ->
            candidate.observedIntervals.forEach { interval ->
                add(candidate.pointAt(interval.startMeters))
                add(candidate.pointAt(interval.endMeters))
            }
        }
        shell?.orderedCorners?.let(::addAll)
    }
    val semanticState = if (shell == null) "incomplete shell" else "proposed shell"
    Canvas(
        modifier = modifier
            .testTag("reconstruction-mini-map")
            .semantics {
                contentDescription = "${candidates.size} wall candidates, " +
                    "${candidates.count { it.state == WallCandidateState.STABLE }} stable, $semanticState"
            },
    ) {
        if (geometryPoints.isEmpty()) return@Canvas
        val minX = geometryPoints.minOf { it.x }
        val maxX = geometryPoints.maxOf { it.x }
        val minZ = geometryPoints.minOf { it.z }
        val maxZ = geometryPoints.maxOf { it.z }
        val spanX = (maxX - minX).coerceAtLeast(0.1)
        val spanZ = (maxZ - minZ).coerceAtLeast(0.1)
        val paddingPx = size.minDimension * 0.12f
        val scale = minOf(
            (size.width - 2.0f * paddingPx) / spanX.toFloat(),
            (size.height - 2.0f * paddingPx) / spanZ.toFloat(),
        )

        fun toCanvas(point: PointXZ): Offset = Offset(
            x = paddingPx + ((point.x - minX) * scale).toFloat(),
            y = paddingPx + ((point.z - minZ) * scale).toFloat(),
        )

        candidates.forEach { candidate ->
            val stable = candidate.state == WallCandidateState.STABLE
            candidate.observedIntervals.forEach { interval ->
                drawLine(
                    color = if (stable) STABLE_WALL_COLOR else DEVELOPING_WALL_COLOR,
                    start = toCanvas(candidate.pointAt(interval.startMeters)),
                    end = toCanvas(candidate.pointAt(interval.endMeters)),
                    strokeWidth = if (stable) 4.0f else 2.0f,
                    cap = StrokeCap.Round,
                )
            }
        }

        shell?.orderedCorners?.let { corners ->
            corners.indices.forEach { index ->
                val next = corners[(index + 1) % corners.size]
                drawLine(
                    color = PROPOSED_SHELL_COLOR,
                    start = toCanvas(corners[index]),
                    end = toCanvas(next),
                    strokeWidth = 6.0f,
                    cap = StrokeCap.Round,
                )
                drawCircle(
                    color = CORNER_COLOR,
                    radius = 5.0f,
                    center = toCanvas(corners[index]),
                )
            }
        }
    }
}

private fun WallCandidate.pointAt(projection: Double): PointXZ = PointXZ(
    x = unitNormal.x * supportingLineOffsetMeters + unitDirection.x * projection,
    z = unitNormal.z * supportingLineOffsetMeters + unitDirection.z * projection,
)

private val DEVELOPING_WALL_COLOR = Color(0x664CAF50)
private val STABLE_WALL_COLOR = Color(0xFF4CAF50)
private val PROPOSED_SHELL_COLOR = Color(0xFF00BCD4)
private val CORNER_COLOR = Color.White
