package com.renovex.capture.network

import kotlinx.serialization.Serializable
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.POST

/**
 * Mirrors backend/internal/identity/handler.go's JSON contract exactly
 * (Task 1 inspection). The refresh_token cookie itself is never modeled
 * here — OkHttp's CookieJar (see RenovexCookieJar) handles it transparently,
 * matching the same cookie-based flow apps/web/src/lib/api/client.ts uses.
 */
interface AuthApi {
    @POST("/auth/login")
    suspend fun login(@Body body: LoginRequest): Response<AuthResponse>

    @POST("/auth/register")
    suspend fun register(@Body body: RegisterRequest): Response<AuthResponse>

    @POST("/auth/refresh")
    suspend fun refresh(): Response<AuthResponse>

    @POST("/auth/logout")
    suspend fun logout(): Response<Unit>
}

@Serializable
data class LoginRequest(val email: String, val password: String)

@Serializable
data class RegisterRequest(val email: String, val password: String, val companyName: String)

@Serializable
data class AuthResponse(val accessToken: String, val mustChangePassword: Boolean)
