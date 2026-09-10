package com.renovex.capture.network

import okhttp3.Cookie
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Cookie.toString()/manual-parse round-trip is the exact mechanism
 * RenovexCookieJar relies on for durable persistence across process death
 * (design spec §10: offline capture resilience implies the auth session
 * itself must also survive a restart). Tested here as pure string logic,
 * without DataStore/Context, since RenovexCookieJar's constructor requires
 * a real Android Context.
 */
class RenovexCookieJarSerializationTest {

    private fun parseBack(serialized: String): Cookie? = runCatching {
        val parts = serialized.split("; ")
        val nameValue = parts.first().split("=", limit = 2)
        val builder = Cookie.Builder().name(nameValue[0]).value(nameValue.getOrElse(1) { "" })
        for (attr in parts.drop(1)) {
            when {
                attr.startsWith("domain=") -> builder.domain(attr.removePrefix("domain="))
                attr.startsWith("path=") -> builder.path(attr.removePrefix("path="))
            }
        }
        builder.build()
    }.getOrNull()

    @Test
    fun `refresh_token cookie round-trips name value domain and path`() {
        val url = "http://10.0.2.2:8080/auth/refresh".toHttpUrl()
        val original = Cookie.Builder()
            .name("refresh_token")
            .value("some-opaque-refresh-value")
            .domain("10.0.2.2")
            .path("/auth")
            .build()

        val serialized = original.toString()
        val restored = parseBack(serialized)

        requireNotNull(restored)
        assertEquals("refresh_token", restored.name)
        assertEquals("some-opaque-refresh-value", restored.value)
        assertEquals("10.0.2.2", restored.domain)
        assertEquals("/auth", restored.path)
        assertEquals(true, restored.matches(url))
    }
}
