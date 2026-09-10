package com.renovex.capture.capture.arcore

import com.google.ar.core.Plane
import com.google.ar.core.TrackingState
import com.renovex.capture.capture.reconstruction.CanonicalPlaneInput
import com.renovex.capture.capture.reconstruction.ObservationTrackingState
import com.renovex.capture.capture.reconstruction.PlaneObservation
import com.renovex.capture.capture.reconstruction.PlaneObservationExtractor
import com.renovex.capture.capture.reconstruction.PointXZ
import com.renovex.capture.capture.reconstruction.VectorXZ

/** The only layer that reads ARCore Plane/Pose/FloatBuffer objects. */
class ArCorePlaneObservationSource(
    private val registry: ArCorePlaneRegistry,
) {
    fun observationsFrom(planes: Collection<Plane>, timestampMillis: Long): List<PlaneObservation> {
        val canonicalPlanes = planes
            .filter { it.type == Plane.Type.VERTICAL }
            .map(::topCanonicalPlane)
            .distinct()

        return canonicalPlanes.mapNotNull { plane ->
            val pose = plane.centerPose
            val center = PointXZ(pose.tx().toDouble(), pose.tz().toDouble())
            val yAxis = pose.yAxis
            val polygon = plane.polygon.duplicate()
            val worldPolygon = buildList {
                val localPoint = FloatArray(3)
                while (polygon.remaining() >= 2) {
                    localPoint[0] = polygon.get()
                    localPoint[1] = 0f
                    localPoint[2] = polygon.get()
                    val world = pose.transformPoint(localPoint)
                    add(PointXZ(world[0].toDouble(), world[2].toDouble()))
                }
            }

            PlaneObservationExtractor.extract(
                CanonicalPlaneInput(
                    planeId = registry.idFor(plane),
                    canonicalPlaneId = registry.canonicalIdFor(plane),
                    center = center,
                    worldNormal = VectorXZ(yAxis[0].toDouble(), yAxis[2].toDouble()),
                    worldPolygon = worldPolygon,
                    timestampMillis = timestampMillis,
                    trackingState = plane.trackingState.toObservationTrackingState(),
                ),
            )
        }
    }

    private fun topCanonicalPlane(rawPlane: Plane): Plane {
        var current = rawPlane
        val visited = mutableSetOf<Plane>()
        while (true) {
            require(visited.add(current)) { "ARCore plane subsumption cycle" }
            current = current.subsumedBy ?: return current
        }
    }

    private fun TrackingState.toObservationTrackingState(): ObservationTrackingState = when (this) {
        TrackingState.TRACKING -> ObservationTrackingState.TRACKING
        TrackingState.PAUSED -> ObservationTrackingState.PAUSED
        TrackingState.STOPPED -> ObservationTrackingState.STOPPED
    }
}
