package com.renovex.capture.network

import android.content.Context
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import okhttp3.Authenticator
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.Route
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Wires the app's single OkHttpClient/Retrofit instance: persistent cookie
 * jar (for the refresh_token cookie), bearer-token injection, and an
 * Authenticator that transparently refreshes on 401 exactly once per
 * request (OkHttp's Authenticator is the purpose-built mechanism for this —
 * it natively prevents infinite refresh loops via priorResponse chaining,
 * unlike a manual Interceptor retry).
 *
 * No Origin header is ever set here deliberately: leaving it absent is what
 * lets backend/internal/platform/http/origin_guard.go's DecideOriginGuard
 * treat this app as a legitimate non-browser client (Task 1/4 inspection —
 * "Origin absent, Sec-Fetch-Site absent -> allowed").
 */
class NetworkModule(context: Context, baseUrl: String) {
    val accessTokenHolder = AccessTokenHolder()
    private val cookieJar = RenovexCookieJar(context)

    // A separate, auth-free client for the AuthApi itself avoids the
    // Authenticator recursively trying to refresh a 401 from /auth/login or
    // /auth/refresh.
    private val authHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .build()

    private val json = Json { ignoreUnknownKeys = true }
    private val contentType = "application/json".toMediaType()

    val authApi: AuthApi = Retrofit.Builder()
        .baseUrl(baseUrl)
        .client(authHttpClient)
        .addConverterFactory(json.asConverterFactory(contentType))
        .build()
        .create(AuthApi::class.java)

    private val mainAuthenticator = Authenticator { _: Route?, response: Response ->
        // priorResponse != null means this IS already a retry of a
        // refreshed request — never chain a second refresh attempt.
        if (response.priorResponse != null) return@Authenticator null

        val refreshed = runBlocking { runCatching { authApi.refresh() }.getOrNull() }
        val newToken = refreshed?.takeIf { it.isSuccessful }?.body()?.accessToken ?: run {
            accessTokenHolder.set(null)
            return@Authenticator null
        }
        accessTokenHolder.set(newToken)
        response.request.newBuilder()
            .header("Authorization", "Bearer $newToken")
            .build()
    }

    private val authHeaderInterceptor = Interceptor { chain ->
        val token = accessTokenHolder.get()
        val request: Request = if (token != null) {
            chain.request().newBuilder().header("Authorization", "Bearer $token").build()
        } else {
            chain.request()
        }
        chain.proceed(request)
    }

    private val mainHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .addInterceptor(authHeaderInterceptor)
        .authenticator(mainAuthenticator)
        .build()

    private val mainRetrofit: Retrofit = Retrofit.Builder()
        .baseUrl(baseUrl)
        .client(mainHttpClient)
        .addConverterFactory(json.asConverterFactory(contentType))
        .build()

    val projectSpaceApi: ProjectSpaceApi = mainRetrofit.create(ProjectSpaceApi::class.java)
    val spatialApi: SpatialApi = mainRetrofit.create(SpatialApi::class.java)

    // Content-PUT uses its own client with NEITHER the bearer interceptor
    // NOR the authenticator: it authenticates solely via
    // X-Spatial-Upload-Token, and must never have a stale/absent bearer
    // Authorization header trigger a spurious refresh cycle.
    private val contentHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .build()

    val spatialContentApi: SpatialContentApi = Retrofit.Builder()
        .baseUrl(baseUrl)
        .client(contentHttpClient)
        .addConverterFactory(json.asConverterFactory(contentType))
        .build()
        .create(SpatialContentApi::class.java)
}
