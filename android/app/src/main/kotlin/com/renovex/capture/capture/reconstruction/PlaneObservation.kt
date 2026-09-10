package com.renovex.capture.capture.reconstruction

/** Canonical, session-local evidence extracted from one ARCore plane update. */
data class PlaneObservation(
    val planeId: String,
    val canonicalPlaneId: String,
    val center: PointXZ,
    val worldPolygon: List<PointXZ>,
    val unitNormal: VectorXZ,
    val unitDirection: VectorXZ,
    val supportingLineOffsetMeters: Double,
    val observedStartProjectionMeters: Double,
    val observedEndProjectionMeters: Double,
    val timestampMillis: Long,
    val trackingState: ObservationTrackingState,
)

data class CanonicalPlaneInput(
    val planeId: String,
    val canonicalPlaneId: String,
    val center: PointXZ,
    val worldNormal: VectorXZ,
    val worldPolygon: List<PointXZ>,
    val timestampMillis: Long,
    val trackingState: ObservationTrackingState,
)

data class PlaneIdentityUpdate(
    val rawPlaneId: String,
    val subsumedByPlaneId: String?,
)
