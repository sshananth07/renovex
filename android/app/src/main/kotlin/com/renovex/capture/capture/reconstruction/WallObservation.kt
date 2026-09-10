package com.renovex.capture.capture.reconstruction

/**
 * Ephemeral evidence that a canonical ARCore plane update observed part of a wall.
 *
 * This type deliberately carries no architectural identity. Multiple observations may
 * contribute to one [WallCandidate], and one canonical plane may produce many observations
 * as ARCore refines its estimate over time.
 */
data class WallObservation(
    val canonicalPlaneId: String,
    val unitNormal: VectorXZ,
    val unitDirection: VectorXZ,
    val supportingLineOffsetMeters: Double,
    val observedStartProjectionMeters: Double,
    val observedEndProjectionMeters: Double,
    val timestampMillis: Long,
    val trackingState: ObservationTrackingState,
)

object WallObservationFactory {
    fun fromPlane(observation: PlaneObservation): WallObservation =
        WallObservation(
            canonicalPlaneId = observation.canonicalPlaneId,
            unitNormal = observation.unitNormal,
            unitDirection = observation.unitDirection,
            supportingLineOffsetMeters = observation.supportingLineOffsetMeters,
            observedStartProjectionMeters = observation.observedStartProjectionMeters,
            observedEndProjectionMeters = observation.observedEndProjectionMeters,
            timestampMillis = observation.timestampMillis,
            trackingState = observation.trackingState,
        )
}
