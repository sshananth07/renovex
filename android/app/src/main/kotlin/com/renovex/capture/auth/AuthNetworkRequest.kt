package com.renovex.capture.auth

import kotlinx.coroutines.CancellationException

/**
 * Runs an authentication network request without letting routine transport
 * failures terminate the app. Cancellation remains structured and must reach
 * the caller rather than being mistaken for a failed login.
 */
internal suspend fun <T> safeAuthRequest(request: suspend () -> T): T? =
    try {
        request()
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        null
    }
