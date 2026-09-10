package com.renovex.capture.capture.reconstruction

import com.renovex.capture.ui.screens.scan.ScanReconstructionController
import com.renovex.capture.ui.screens.scan.ScanReconstructionState
import kotlin.math.hypot
import org.junit.Assert.assertEquals
import org.junit.Test

class WallTraceBuilderComparisonTest {
    @Test
    fun `connected trace prototype fails go gate and RoomShellBuilder remains selected`() {
        val rawFixtures = listOf(
            "simple-rectangular-room.json",
            "fragmented-single-wall.json",
            "same-wall-rotated.json",
            "same-wall-offset.json",
            "plane-subsumption.json",
            "partial-room-scan.json",
            "l-shaped-room.json",
            "angled-wall-room.json",
            "noisy-corner.json",
            "revisited-wall.json",
            "open-doorway.json",
            "incomplete-shell.json",
            "xiaomi-11t-object-loop.json",
        )

        val comparison = rawFixtures.associateWith { fixtureName ->
            val evidence = RawFixtureComparison.replay(fixtureName)
            val traced = WallTraceBuilderPrototype.build(
                evidence.state.candidates,
                evidence.state.intersections,
                evidence.cameraTrajectory,
                evidence.config.shell,
            )
            (evidence.state.shellResult is RoomShellBuildResult.Complete) to
                (traced is RoomShellBuildResult.Complete)
        }

        // A connected greedy walk can take a plausible cross-intersection and
        // lose a valid boundary even in the basic rectangle. It therefore
        // cannot replace the global cycle scorer.
        assertEquals(true to false, comparison.getValue("simple-rectangular-room.json"))
        assertEquals(rawFixtures.toSet(), comparison.keys)
    }

    @Test
    fun `connected trace prototype does not reduce reconstructed nine edge micro chamfers`() {
        // Break caught: promoting connected tracing without proving it actually
        // improves the recorded architectural failure mode.
        val evidence = ReconstructedNineEdgeEvidence.create()

        val traced = WallTraceBuilderPrototype.build(
            evidence.candidates,
            evidence.intersections,
            evidence.cameraTrajectory,
            evidence.config,
        ) as RoomShellBuildResult.Complete

        assertEquals(9, traced.shell.orderedCandidateIds.size)
    }
}

private object RawFixtureComparison {
    data class Evidence(
        val state: ScanReconstructionState,
        val cameraTrajectory: List<PointXZ>,
        val config: ScannerParameters,
    )

    fun replay(fixtureName: String): Evidence {
        val fixture = ScanFixtureLoader.load(fixtureName)
        val canonicalizer = PlaneCanonicalizer()
        val parameters = ScannerParameters()
        val controller = ScanReconstructionController(parameters)
        var state: ScanReconstructionState? = null
        fixture.frames.forEach { frame ->
            frame.planes.forEach { plane ->
                canonicalizer.canonicalIdFor(PlaneIdentityUpdate(plane.planeId, plane.subsumedByPlaneId))
            }
            val observations = frame.planes.mapNotNull { plane ->
                PlaneObservationExtractor.extract(
                    CanonicalPlaneInput(
                        planeId = plane.planeId,
                        canonicalPlaneId = canonicalizer.canonicalIdFor(PlaneIdentityUpdate(plane.planeId, null)),
                        center = plane.center,
                        worldNormal = plane.worldNormal,
                        worldPolygon = plane.worldPolygon,
                        timestampMillis = frame.timestampMillis,
                        trackingState = plane.trackingState,
                    ),
                )
            }
            state = controller.update(observations, frame.cameraPosition, frame.cameraHeadingRadians)
        }
        return Evidence(requireNotNull(state), fixture.frames.map { it.cameraPosition }, parameters)
    }
}

/**
 * Test-only connected-trace prototype. It greedily follows the strongest
 * unused adjacent candidate from the longest stable starting wall, then asks
 * the existing shell builder to apply the same topology/camera plausibility
 * checks. It is deliberately not production architecture unless the go gate
 * proves an advantage.
 */
private object WallTraceBuilderPrototype {
    fun build(
        candidates: List<WallCandidate>,
        intersections: List<WallIntersection>,
        cameraTrajectory: List<PointXZ>,
        config: RoomShellConfig,
    ): RoomShellBuildResult {
        val stable = candidates.filter { it.state == WallCandidateState.STABLE }.associateBy { it.candidateId }
        val adjacency = stable.keys.associateWith { mutableListOf<WallIntersection>() }
        intersections.forEach { intersection ->
            adjacency[intersection.firstCandidateId]?.add(intersection)
            adjacency[intersection.secondCandidateId]?.add(intersection)
        }
        val start = stable.values.maxWithOrNull(
            compareBy<WallCandidate> { it.observedEndProjectionMeters - it.observedStartProjectionMeters }
                .thenBy { it.stability },
        ) ?: return RoomShellBuildResult.Incomplete(emptyList(), 0.0)
        val trace = mutableListOf(start.candidateId)
        while (trace.size <= stable.size) {
            val current = trace.last()
            val next = adjacency[current].orEmpty()
                .map { edge ->
                    val other = if (edge.firstCandidateId == current) edge.secondCandidateId else edge.firstCandidateId
                    other to edge
                }
                .filter { (other, _) -> other == trace.first() || other !in trace }
                .sortedWith(
                    compareByDescending<Pair<String, WallIntersection>> { (_, edge) -> edge.confidence }
                        .thenByDescending { (other, _) -> stable[other]?.stability ?: 0.0 }
                        .thenBy { (other, _) -> other },
                )
                .firstOrNull()
            if (next == null) break
            if (next.first == trace.first()) {
                if (trace.size >= 3) {
                    val selectedIds = trace.toSet()
                    return RoomShellBuilder.build(
                        stableCandidates = trace.map(stable::getValue),
                        intersections = intersections.filter {
                            it.firstCandidateId in selectedIds && it.secondCandidateId in selectedIds
                        },
                        cameraTrajectory = cameraTrajectory,
                        config = config,
                    )
                }
                break
            }
            trace += next.first
        }
        return RoomShellBuildResult.Incomplete(listOf(trace), 0.0)
    }
}

/**
 * Reconstructed stable-candidate/shell regression evidence from the recorded
 * Xiaomi nine-edge failure. This is NOT a raw ARCore replay and intentionally
 * makes no claim about the original frame-by-frame observation history.
 */
private object ReconstructedNineEdgeEvidence {
    data class Evidence(
        val candidates: List<WallCandidate>,
        val intersections: List<WallIntersection>,
        val cameraTrajectory: List<PointXZ>,
        val config: RoomShellConfig,
    )

    fun create(): Evidence {
        val points = listOf(
            PointXZ(0.072, 0.0),
            PointXZ(4.0, 0.0),
            PointXZ(4.0, 3.0),
            PointXZ(0.0, 3.0),
            PointXZ(0.0, 0.064),
            PointXZ(0.030, 0.064),
            PointXZ(0.030, 0.400),
            PointXZ(0.070, 0.400),
            PointXZ(0.070, 0.200),
        )
        val candidates = points.indices.map { index ->
            candidate("reconstructed-wall-$index", points[index], points[(index + 1) % points.size])
        }
        val intersections = points.indices.map { index ->
            WallIntersection(
                candidates[index].candidateId,
                candidates[(index + 1) % candidates.size].candidateId,
                points[(index + 1) % points.size],
                0.90,
            )
        }
        return Evidence(
            candidates,
            intersections,
            listOf(PointXZ(2.0, 1.5), PointXZ(2.1, 1.5), PointXZ(2.2, 1.5)),
            RoomShellConfig(
                minimumProposedShellConfidence = 0.0,
                minimumInteriorCameraClearanceMeters = 0.30,
                minimumInteriorCameraEvidenceSamples = 3,
            ),
        )
    }

    private fun candidate(id: String, start: PointXZ, end: PointXZ): WallCandidate {
        val dx = end.x - start.x
        val dz = end.z - start.z
        val length = hypot(dx, dz)
        val direction = VectorXZ(dx / length, dz / length)
        val normal = VectorXZ(direction.z, -direction.x)
        val offset = normal.x * start.x + normal.z * start.z
        val firstProjection = direction.x * start.x + direction.z * start.z
        val secondProjection = direction.x * end.x + direction.z * end.z
        return WallCandidate(
            candidateId = id,
            unitNormal = normal,
            unitDirection = direction,
            supportingLineOffsetMeters = offset,
            observedIntervals = listOf(ProjectionInterval(minOf(firstProjection, secondProjection), maxOf(firstProjection, secondProjection))),
            observationCount = 6,
            firstObservedAtMillis = 0,
            lastObservedAtMillis = 1_000,
            angleStdDevRadians = 0.01,
            offsetStdDevMeters = 0.01,
            stability = 0.90,
            state = WallCandidateState.STABLE,
        )
    }
}
