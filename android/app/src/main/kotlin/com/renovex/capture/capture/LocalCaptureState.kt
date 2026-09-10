package com.renovex.capture.capture

/**
 * Local (on-device) capture lifecycle status. Mirrors
 * backend/internal/spatial/spatial.go's CaptureStatus string values exactly
 * for direct interop with the server contract, plus LOCALLY_CONFIRMED —
 * a status the server never has, representing design spec §10.1's offline
 * confirmation: "locally_confirmed... does NOT create authoritative server
 * state." A locally-confirmed capture still becomes CaptureStatusReview (or
 * later) once it actually reaches the server and is reviewed there.
 */
enum class LocalCaptureStatus {
    DRAFT, CAPTURING, UPLOADING, UPLOADED, REVIEW, LOCALLY_CONFIRMED, CONFIRMED, FAILED;

    val serverStatus: String?
        get() = when (this) {
            DRAFT -> "draft"
            CAPTURING -> "capturing"
            UPLOADING -> "uploading"
            UPLOADED -> "uploaded"
            REVIEW -> "review"
            CONFIRMED -> "confirmed"
            FAILED -> "failed"
            LOCALLY_CONFIRMED -> null // no server-side equivalent (design spec §10.1)
        }
}

/**
 * Legal local status transitions (design spec §4.1's failure/retry
 * branches, extended with LOCALLY_CONFIRMED per §10.1). Kept as its own
 * pure lookup, mirroring backend/internal/spatial/service.go's
 * legalCaptureTransitions map, so illegal transitions are rejected
 * identically on-device before ever reaching the network (Task 5 TDD
 * requirement: "local capture state machine").
 */
private val legalTransitions: Map<LocalCaptureStatus, Set<LocalCaptureStatus>> = mapOf(
    LocalCaptureStatus.DRAFT to setOf(LocalCaptureStatus.CAPTURING),
    LocalCaptureStatus.CAPTURING to setOf(LocalCaptureStatus.UPLOADING, LocalCaptureStatus.FAILED),
    LocalCaptureStatus.UPLOADING to setOf(LocalCaptureStatus.UPLOADED, LocalCaptureStatus.FAILED),
    LocalCaptureStatus.FAILED to setOf(LocalCaptureStatus.CAPTURING, LocalCaptureStatus.UPLOADING),
    LocalCaptureStatus.UPLOADED to setOf(LocalCaptureStatus.REVIEW),
    // Review can go two ways offline-first: reach the server (REVIEW ->
    // CONFIRMED, handled by the network layer once connectivity returns)
    // or be confirmed locally first while offline.
    LocalCaptureStatus.REVIEW to setOf(LocalCaptureStatus.CONFIRMED, LocalCaptureStatus.LOCALLY_CONFIRMED),
    // A locally-confirmed capture becomes authoritatively CONFIRMED once
    // connectivity returns and the server accepts it — never any other
    // transition, since local confirmation is not itself authoritative.
    LocalCaptureStatus.LOCALLY_CONFIRMED to setOf(LocalCaptureStatus.CONFIRMED),
)

object LocalCaptureStateMachine {
    fun canTransition(from: LocalCaptureStatus, to: LocalCaptureStatus): Boolean =
        legalTransitions[from]?.contains(to) == true

    /** Returns the new status, or null if the transition is illegal. */
    fun transition(from: LocalCaptureStatus, to: LocalCaptureStatus): LocalCaptureStatus? =
        if (canTransition(from, to)) to else null

    /** CONFIRMED is the only true terminal state — matching the server's
     * capture.go, where confirmed has no outgoing legal transitions. */
    fun isTerminal(status: LocalCaptureStatus): Boolean = status == LocalCaptureStatus.CONFIRMED
}
