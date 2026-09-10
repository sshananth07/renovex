package com.renovex.capture.network

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl

private val Context.cookieDataStore by preferencesDataStore(name = "renovex_cookies")
private val COOKIES_KEY = stringSetPreferencesKey("cookies")

/**
 * Persists the backend's httpOnly refresh_token cookie (Path=/auth) across
 * process death and device restart — required because
 * backend/internal/identity/handler.go's /auth/refresh reads the refresh
 * token ONLY from that cookie, no alternative body/header path (Task 1/4
 * inspection). Android has no browser cookie store, so OkHttp's CookieJar
 * is the mechanism, backed by DataStore for durability.
 *
 * Each cookie is serialized via Cookie.toString()/parsed back manually,
 * which round-trips name/value/domain/path/expiry but not HttpOnly/Secure —
 * acceptable since this jar is process-internal storage, not a network
 * transmission, and OkHttp only consults domain/path/expiry to decide
 * whether to attach a cookie to an outgoing request.
 */
class RenovexCookieJar(context: Context) : CookieJar {
    private val dataStore = context.applicationContext.cookieDataStore
    private val memoryCache: MutableList<Cookie> = runBlocking {
        val stored = dataStore.data.first()[COOKIES_KEY].orEmpty()
        stored.mapNotNull(::deserializeCookie).toMutableList()
    }

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        for (cookie in cookies) {
            memoryCache.removeAll { it.name == cookie.name && it.domain == cookie.domain && it.path == cookie.path }
            if (!cookie.persistent || cookie.expiresAt > System.currentTimeMillis()) {
                memoryCache.add(cookie)
            }
        }
        persist()
    }

    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val now = System.currentTimeMillis()
        val expired = memoryCache.filter { it.persistent && it.expiresAt <= now }
        if (expired.isNotEmpty()) {
            memoryCache.removeAll(expired)
            persist()
        }
        return memoryCache.filter { it.matches(url) }
    }

    private fun persist() {
        val serialized = memoryCache.map { it.toString() }.toSet()
        runBlocking { dataStore.edit { prefs -> prefs[COOKIES_KEY] = serialized } }
    }

    private fun deserializeCookie(serialized: String): Cookie? = runCatching {
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
}
