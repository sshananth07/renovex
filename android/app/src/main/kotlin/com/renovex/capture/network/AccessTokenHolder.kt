package com.renovex.capture.network

import java.util.concurrent.atomic.AtomicReference

/**
 * In-memory-only holder for the current bearer access token — never
 * persisted, matching apps/web/src/lib/api/accessToken.ts's convention.
 * Thread-safe since OkHttp interceptors run on background threads.
 */
class AccessTokenHolder {
    private val ref = AtomicReference<String?>(null)

    fun get(): String? = ref.get()
    fun set(token: String?) { ref.set(token) }
}
