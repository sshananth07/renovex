package com.renovex.capture.capture.reconstruction

import kotlin.math.abs
import kotlin.math.hypot

/**
 * Converts canonical plane evidence into a stable 2D supporting-line
 * observation. Polygon shape controls observed extent only; wall orientation
 * comes exclusively from the plane normal.
 */
object PlaneObservationExtractor {
    private const val GEOMETRY_EPSILON = 1e-6

    fun extract(input: CanonicalPlaneInput): PlaneObservation? {
        if (input.planeId.isBlank() || input.canonicalPlaneId.isBlank()) return null
        if (!input.center.isFinite() || !input.worldNormal.isFinite()) return null
        if (input.worldPolygon.size < 2 || input.worldPolygon.any { !it.isFinite() }) return null

        val normalLength = hypot(input.worldNormal.x, input.worldNormal.z)
        if (!normalLength.isFinite() || normalLength <= GEOMETRY_EPSILON) return null

        var normalX = input.worldNormal.x / normalLength
        var normalZ = input.worldNormal.z / normalLength
        if (normalX < 0.0 || (abs(normalX) <= GEOMETRY_EPSILON && normalZ < 0.0)) {
            normalX = -normalX
            normalZ = -normalZ
        }
        if (abs(normalX) <= GEOMETRY_EPSILON) normalX = 0.0
        if (abs(normalZ) <= GEOMETRY_EPSILON) normalZ = 0.0

        val unitNormal = VectorXZ(normalX, normalZ)
        val unitDirection = VectorXZ(
            x = if (abs(normalZ) <= GEOMETRY_EPSILON) 0.0 else -normalZ,
            z = normalX,
        )
        val projections = input.worldPolygon.map { point ->
            point.x * unitDirection.x + point.z * unitDirection.z
        }
        val observedStart = projections.min()
        val observedEnd = projections.max()
        if (observedEnd - observedStart <= GEOMETRY_EPSILON) return null

        return PlaneObservation(
            planeId = input.planeId,
            canonicalPlaneId = input.canonicalPlaneId,
            center = input.center,
            worldPolygon = input.worldPolygon.toList(),
            unitNormal = unitNormal,
            unitDirection = unitDirection,
            supportingLineOffsetMeters = input.center.x * unitNormal.x + input.center.z * unitNormal.z,
            observedStartProjectionMeters = observedStart,
            observedEndProjectionMeters = observedEnd,
            timestampMillis = input.timestampMillis,
            trackingState = input.trackingState,
        )
    }

    private fun PointXZ.isFinite(): Boolean = x.isFinite() && z.isFinite()
    private fun VectorXZ.isFinite(): Boolean = x.isFinite() && z.isFinite()
}

/**
 * Pure identity graph used by the ARCore adapter. A null parent means "no new
 * subsumption information" and deliberately does not erase an earlier link.
 */
class PlaneCanonicalizer {
    private val parentById = mutableMapOf<String, String>()
    private val knownIds = linkedSetOf<String>()

    fun canonicalIdFor(update: PlaneIdentityUpdate): String {
        require(update.rawPlaneId.isNotBlank()) { "raw plane ID must not be blank" }
        knownIds += update.rawPlaneId

        update.subsumedByPlaneId?.let { parentId ->
            require(parentId.isNotBlank()) { "parent plane ID must not be blank" }
            require(parentId != update.rawPlaneId) { "plane cannot subsume itself" }
            knownIds += parentId
            require(resolve(parentId) != update.rawPlaneId) { "plane subsumption cycle" }
            parentById[update.rawPlaneId] = parentId
        }

        return resolve(update.rawPlaneId)
    }

    fun activeCanonicalIds(): Set<String> = knownIds.mapTo(linkedSetOf(), ::resolve)

    private fun resolve(id: String): String {
        var current = id
        val visited = linkedSetOf<String>()
        while (true) {
            require(visited.add(current)) { "plane subsumption cycle" }
            current = parentById[current] ?: return current
        }
    }
}
