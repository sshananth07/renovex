package com.renovex.capture.capture.reconstruction

import kotlinx.serialization.Serializable

/** Finite room-local XZ coordinate in metres. */
@Serializable
data class PointXZ(val x: Double, val z: Double)

/** Horizontal vector; normalization is enforced by the consumer that needs it. */
@Serializable
data class VectorXZ(val x: Double, val z: Double)

@Serializable
enum class ObservationTrackingState { TRACKING, PAUSED, STOPPED }

/** One deterministic substitute for an ARCore vertical-plane update. */
@Serializable
data class ReplayPlaneUpdate(
    val planeId: String,
    val subsumedByPlaneId: String? = null,
    val center: PointXZ,
    val worldNormal: VectorXZ,
    val worldPolygon: List<PointXZ>,
    val trackingState: ObservationTrackingState,
)

/** All scanner evidence delivered at one replay timestamp. */
@Serializable
data class ScanReplayFrame(
    val timestampMillis: Long,
    val cameraPosition: PointXZ,
    val cameraHeadingRadians: Double,
    val planes: List<ReplayPlaneUpdate>,
)

/** A named, chronologically ordered deterministic scanner capture. */
@Serializable
data class ScanReplayFixture(
    val name: String,
    val frames: List<ScanReplayFrame>,
)
