package com.renovex.capture.capture.reconstruction

import kotlin.math.abs
import kotlin.math.hypot

/** Builds a shell only from a real simple cycle in the supported intersection graph. */
object RoomShellBuilder {
    fun build(
        stableCandidates: List<WallCandidate>,
        intersections: List<WallIntersection>,
        cameraTrajectory: List<PointXZ>,
        config: RoomShellConfig,
    ): RoomShellBuildResult {
        val candidates = stableCandidates
            .filter { it.state == WallCandidateState.STABLE }
            .associateBy { it.candidateId }
        require(candidates.size == stableCandidates.count { it.state == WallCandidateState.STABLE }) {
            "stable candidate IDs must be unique"
        }
        val intersectionByPair = linkedMapOf<CandidatePair, WallIntersection>()
        intersections.forEach { intersection ->
            if (intersection.firstCandidateId !in candidates || intersection.secondCandidateId !in candidates) return@forEach
            if (intersection.firstCandidateId == intersection.secondCandidateId) return@forEach
            val pair = CandidatePair.of(intersection.firstCandidateId, intersection.secondCandidateId)
            val existing = intersectionByPair[pair]
            if (existing == null || intersection.confidence > existing.confidence) {
                intersectionByPair[pair] = intersection
            }
        }
        val adjacency = candidates.keys.associateWith { linkedSetOf<String>() }.toMutableMap()
        intersectionByPair.keys.forEach { pair ->
            adjacency.getValue(pair.first).add(pair.second)
            adjacency.getValue(pair.second).add(pair.first)
        }

        val scoredCycles = findCycles(adjacency).mapNotNull { cycle ->
            scoreCycle(cycle, candidates, intersectionByPair, cameraTrajectory, config)
        }
        val best = scoredCycles.sortedWith(
            compareByDescending<ScoredShell> { it.shell.confidence }
                .thenBy { it.tieKey },
        ).firstOrNull()
        if (best != null && best.shell.confidence + NUMERIC_EPSILON >= config.minimumProposedShellConfidence) {
            return RoomShellBuildResult.Complete(best.shell)
        }

        return RoomShellBuildResult.Incomplete(
            orderedCandidateChains = connectedComponents(adjacency),
            confidence = best?.shell?.confidence ?: 0.0,
        )
    }

    private fun scoreCycle(
        cycle: List<String>,
        candidates: Map<String, WallCandidate>,
        intersectionByPair: Map<CandidatePair, WallIntersection>,
        cameraTrajectory: List<PointXZ>,
        config: RoomShellConfig,
    ): ScoredShell? {
        val corners = cycle.indices.map { index ->
            val next = (index + 1) % cycle.size
            intersectionByPair[CandidatePair.of(cycle[index], cycle[next])]?.point ?: return null
        }
        if (!isSimplePolygon(corners, config.boundaryEpsilonMeters)) return null

        val insideCameraSamples = cameraTrajectory.filter { point ->
            pointInsideOrOnPolygon(point, corners, config.boundaryEpsilonMeters)
        }
        if (insideCameraSamples.isEmpty()) {
            val requiredInteriorWidth = 2.0 * config.minimumInteriorCameraClearanceMeters
            if (minimumCaliperWidth(corners) + NUMERIC_EPSILON < requiredInteriorWidth) return null
        } else {
            val credibleInteriorSampleCount = insideCameraSamples.count { point ->
                distanceToPolygonBoundary(point, corners) + NUMERIC_EPSILON >=
                    config.minimumInteriorCameraClearanceMeters
            }
            if (credibleInteriorSampleCount < config.minimumInteriorCameraEvidenceSamples) return null
        }

        val coverage = linkedMapOf<String, Double>()
        cycle.indices.forEach { index ->
            val previousCorner = corners[(index - 1 + corners.size) % corners.size]
            val nextCorner = corners[index]
            val candidate = candidates.getValue(cycle[index])
            coverage[candidate.candidateId] = observedCoverage(candidate, previousCorner, nextCorner)
        }
        val selectedCandidates = cycle.map(candidates::getValue)
        val selectedIntersections = cycle.indices.map { index ->
            intersectionByPair.getValue(CandidatePair.of(cycle[index], cycle[(index + 1) % cycle.size]))
        }
        val meanStability = selectedCandidates.map { it.stability }.average()
        val meanIntersectionConfidence = selectedIntersections.map { it.confidence }.average()
        val meanCoverage = coverage.values.average()
        val cameraPlausibility = if (cameraTrajectory.isEmpty()) {
            0.0
        } else {
            insideCameraSamples.size.toDouble() / cameraTrajectory.size
        }
        val weightTotal = config.candidateStabilityWeight + config.intersectionConfidenceWeight +
            config.observedCoverageWeight + config.cameraPathWeight
        val confidence = (
            config.candidateStabilityWeight * meanStability +
                config.intersectionConfidenceWeight * meanIntersectionConfidence +
                config.observedCoverageWeight * meanCoverage +
                config.cameraPathWeight * cameraPlausibility
            ) / weightTotal
        return ScoredShell(
            shell = RoomShell(
                orderedCandidateIds = cycle,
                orderedCorners = corners,
                observedCoverageByCandidate = coverage,
                confidence = confidence.coerceIn(0.0, 1.0),
            ),
            tieKey = cycle.sorted().joinToString("\u0000"),
        )
    }

    private fun findCycles(adjacency: Map<String, Set<String>>): List<List<String>> {
        val cyclesByKey = linkedMapOf<String, List<String>>()
        adjacency.keys.sorted().forEach { start ->
            fun visit(current: String, path: List<String>, visited: Set<String>) {
                adjacency[current].orEmpty().sorted().forEach { next ->
                    when {
                        next == start && path.size >= 3 -> {
                            val canonical = canonicalCycle(path)
                            cyclesByKey.putIfAbsent(canonical.joinToString("\u0000"), canonical)
                        }
                        next !in visited -> visit(next, path + next, visited + next)
                    }
                }
            }
            visit(start, listOf(start), setOf(start))
        }
        return cyclesByKey.values.toList()
    }

    private fun canonicalCycle(cycle: List<String>): List<String> {
        val variants = mutableListOf<List<String>>()
        listOf(cycle, cycle.reversed()).forEach { direction ->
            direction.indices.forEach { offset ->
                variants += direction.drop(offset) + direction.take(offset)
            }
        }
        return variants.minBy { it.joinToString("\u0000") }
    }

    private fun connectedComponents(adjacency: Map<String, Set<String>>): List<List<String>> {
        val remaining = adjacency.keys.toMutableSet()
        val components = mutableListOf<List<String>>()
        while (remaining.isNotEmpty()) {
            val seed = remaining.min()
            val pending = ArrayDeque<String>()
            val component = linkedSetOf<String>()
            pending.add(seed)
            while (pending.isNotEmpty()) {
                val current = pending.removeFirst()
                if (!component.add(current)) continue
                remaining.remove(current)
                adjacency[current].orEmpty().sorted().forEach(pending::addLast)
            }
            components += component.sorted()
        }
        return components.sortedBy { it.firstOrNull().orEmpty() }
    }

    private fun observedCoverage(candidate: WallCandidate, first: PointXZ, second: PointXZ): Double {
        val firstProjection = project(first, candidate.unitDirection)
        val secondProjection = project(second, candidate.unitDirection)
        val architecturalStart = minOf(firstProjection, secondProjection)
        val architecturalEnd = maxOf(firstProjection, secondProjection)
        val architecturalLength = architecturalEnd - architecturalStart
        if (architecturalLength <= NUMERIC_EPSILON) return 0.0
        val observedLength = candidate.observedIntervals.sumOf { interval ->
            val overlapStart = maxOf(architecturalStart, interval.startMeters)
            val overlapEnd = minOf(architecturalEnd, interval.endMeters)
            maxOf(0.0, overlapEnd - overlapStart)
        }
        return (observedLength / architecturalLength).coerceIn(0.0, 1.0)
    }

    private fun isSimplePolygon(points: List<PointXZ>, epsilon: Double): Boolean {
        if (points.size < 3 || abs(signedArea(points)) <= epsilon * epsilon) return false
        for (firstIndex in points.indices) {
            val firstNext = (firstIndex + 1) % points.size
            for (secondIndex in firstIndex + 1 until points.size) {
                val secondNext = (secondIndex + 1) % points.size
                if (firstIndex == secondIndex || firstNext == secondIndex || secondNext == firstIndex) continue
                if (segmentsIntersect(points[firstIndex], points[firstNext], points[secondIndex], points[secondNext], epsilon)) {
                    return false
                }
            }
        }
        return true
    }

    private fun signedArea(points: List<PointXZ>): Double = points.indices.sumOf { index ->
        val next = points[(index + 1) % points.size]
        points[index].x * next.z - next.x * points[index].z
    } / 2.0

    private fun segmentsIntersect(a: PointXZ, b: PointXZ, c: PointXZ, d: PointXZ, epsilon: Double): Boolean {
        val first = orientation(a, b, c)
        val second = orientation(a, b, d)
        val third = orientation(c, d, a)
        val fourth = orientation(c, d, b)
        return first * second < -epsilon && third * fourth < -epsilon
    }

    private fun orientation(a: PointXZ, b: PointXZ, c: PointXZ): Double =
        (b.x - a.x) * (c.z - a.z) - (b.z - a.z) * (c.x - a.x)

    private fun pointInsideOrOnPolygon(point: PointXZ, polygon: List<PointXZ>, epsilon: Double): Boolean {
        if (polygon.indices.any { index ->
                distanceToSegment(point, polygon[index], polygon[(index + 1) % polygon.size]) <= epsilon
            }
        ) return true
        var inside = false
        var previous = polygon.last()
        polygon.forEach { current ->
            val crosses = (current.z > point.z) != (previous.z > point.z)
            if (crosses) {
                val intersectionX = (previous.x - current.x) * (point.z - current.z) /
                    (previous.z - current.z) + current.x
                if (point.x < intersectionX) inside = !inside
            }
            previous = current
        }
        return inside
    }

    private fun distanceToSegment(point: PointXZ, start: PointXZ, end: PointXZ): Double {
        val deltaX = end.x - start.x
        val deltaZ = end.z - start.z
        val lengthSquared = deltaX * deltaX + deltaZ * deltaZ
        if (lengthSquared <= NUMERIC_EPSILON) return hypot(point.x - start.x, point.z - start.z)
        val t = (((point.x - start.x) * deltaX + (point.z - start.z) * deltaZ) / lengthSquared)
            .coerceIn(0.0, 1.0)
        return hypot(point.x - (start.x + t * deltaX), point.z - (start.z + t * deltaZ))
    }

    private fun distanceToPolygonBoundary(point: PointXZ, polygon: List<PointXZ>): Double =
        polygon.indices.minOf { index ->
            distanceToSegment(point, polygon[index], polygon[(index + 1) % polygon.size])
        }

    /**
     * Rotation-invariant necessary capacity for a clearance circle. A polygon
     * that can contain radius r must span at least 2r along every direction.
     */
    private fun minimumCaliperWidth(polygon: List<PointXZ>): Double = polygon.indices.minOf { index ->
        val start = polygon[index]
        val end = polygon[(index + 1) % polygon.size]
        val deltaX = end.x - start.x
        val deltaZ = end.z - start.z
        val edgeLength = hypot(deltaX, deltaZ)
        if (edgeLength <= NUMERIC_EPSILON) return@minOf 0.0
        val normalX = -deltaZ / edgeLength
        val normalZ = deltaX / edgeLength
        val projections = polygon.map { point -> point.x * normalX + point.z * normalZ }
        projections.max() - projections.min()
    }

    private fun project(point: PointXZ, direction: VectorXZ): Double =
        point.x * direction.x + point.z * direction.z

    private data class CandidatePair(val first: String, val second: String) {
        companion object {
            fun of(first: String, second: String): CandidatePair =
                if (first <= second) CandidatePair(first, second) else CandidatePair(second, first)
        }
    }

    private data class ScoredShell(val shell: RoomShell, val tieKey: String)

    private const val NUMERIC_EPSILON = 1e-9
}
