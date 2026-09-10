package com.renovex.capture.connectivity

/**
 * Network reachability, kept as its own tiny pure type so UI/state code can
 * react to transitions without depending on Android's ConnectivityManager
 * directly (Task 4 TDD requirement: "offline status transitions"; design
 * spec §10.3: only SOME capabilities require network — server confirm,
 * MapAnything, Qwen, external asset import, Cloud Anchor, real-time
 * collaboration — while capture/review/local-confirm keep working offline).
 */
enum class ConnectivityState {
    Online,
    Offline;

    companion object {
        fun from(isConnected: Boolean): ConnectivityState = if (isConnected) Online else Offline
    }
}
