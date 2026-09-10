package com.renovex.capture.capture

import com.renovex.capture.geometry.RoomDraft

/** Coarse ARCore tracking quality, decoupled from ARCore's own enum so the
 * UI/state layer can be unit tested without an ARCore dependency (Task 5
 * TDD requirement: keep AR-runtime-adjacent state testable). Mapped 1:1
 * from com.google.ar.core.TrackingState/TrackingFailureReason at the
 * screen layer, the one place that touches the real ARCore types. */
enum class TrackingQuality {
    NOT_STARTED,
    GOOD,
    LIMITED_EXCESSIVE_MOTION,
    LIMITED_INSUFFICIENT_LIGHT,
    LIMITED_INSUFFICIENT_FEATURES,
    LIMITED_OTHER,
    STOPPED,
}

/**
 * Scan Mode's session state: tracking quality plus the in-progress
 * RoomDraft being built up as the contractor scans (design spec/Task 5 UX
 * acceptance: "live camera, tracking quality, room coverage, detected
 * corners/walls, measurements, opening proposals, guidance to unscanned
 * areas"). coveragePercent is a coarse heuristic score (0-100), not an
 * authoritative measurement — only server-side confirmation in Task 7
 * produces authoritative quantities.
 */
data class ScanSessionState(
    val trackingQuality: TrackingQuality = TrackingQuality.NOT_STARTED,
    val draft: RoomDraft = RoomDraft(),
    val coveragePercent: Int = 0,
)

/** Human-readable guidance text for the current tracking quality — the
 * "guidance to unscanned areas" UX requirement's textual half (the visual
 * overlay half lives in the AR-rendering screen itself, which this state
 * object has no dependency on). */
fun TrackingQuality.guidanceMessage(): String = when (this) {
    TrackingQuality.NOT_STARTED -> "Point your camera at the room to begin scanning"
    TrackingQuality.GOOD -> "Move slowly around the room to capture all walls"
    TrackingQuality.LIMITED_EXCESSIVE_MOTION -> "Move more slowly"
    TrackingQuality.LIMITED_INSUFFICIENT_LIGHT -> "Too dark — find better lighting"
    TrackingQuality.LIMITED_INSUFFICIENT_FEATURES -> "Point at a more detailed surface"
    TrackingQuality.LIMITED_OTHER -> "Tracking lost — move the camera slowly"
    TrackingQuality.STOPPED -> "Scanning stopped"
}
