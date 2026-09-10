package com.renovex.capture.network

import kotlinx.serialization.Serializable
import okhttp3.RequestBody
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.PUT
import retrofit2.http.Query

/**
 * Mirrors backend/internal/spatial/handler.go's JSON contract exactly
 * (Task 2/3). The content-PUT route is registered separately
 * (SpatialContentApi) since it authenticates via a header token rather
 * than the bearer AuthInterceptor, matching the Go route's registration on
 * the unauthenticated base API group.
 */
interface SpatialApi {
    @POST("/spatial/captures")
    suspend fun startCapture(@Body body: StartCaptureRequest): Response<SpatialCaptureDto>

    @GET("/spatial/captures/{id}")
    suspend fun getCapture(@Path("id") id: String): Response<SpatialCaptureDto>

    @GET("/spatial/captures")
    suspend fun listCaptures(@Query("spaceId") spaceId: String): Response<List<SpatialCaptureDto>>

    @POST("/spatial/captures/{id}/advance")
    suspend fun advanceCapture(@Path("id") id: String, @Body body: AdvanceCaptureRequest): Response<SpatialCaptureDto>

    @POST("/spatial/captures/{id}/confirm")
    suspend fun confirmCapture(@Path("id") id: String): Response<SpatialCaptureDto>

    @GET("/spatial/space-state")
    suspend fun getSpaceState(@Query("projectId") projectId: String, @Query("spaceId") spaceId: String): Response<SpatialSpaceStateDto>

    @POST("/spatial/artifacts")
    suspend fun requestArtifactUpload(@Body body: RequestArtifactUploadRequest): Response<ArtifactUploadResponse>

    @POST("/spatial/artifacts/{id}/resume")
    suspend fun resumeArtifactUpload(@Path("id") id: String): Response<ArtifactUploadResponse>

    @POST("/spatial/artifacts/{id}/finalize")
    suspend fun finalizeArtifactUpload(@Path("id") id: String, @Body body: FinalizeArtifactUploadRequest): Response<SpatialArtifactDto>

    @GET("/spatial/artifacts")
    suspend fun listArtifacts(@Query("captureId") captureId: String): Response<List<SpatialArtifactDto>>
}

/**
 * Separate Retrofit interface for the artifact content-proxy route: it
 * authenticates via X-Spatial-Upload-Token, not the bearer AuthInterceptor,
 * so it must bypass that interceptor's Authorization header injection
 * entirely (backend/internal/spatial/handler.go's RegisterExternalHandlers
 * is mounted on the unauthenticated base API for the same reason).
 */
interface SpatialContentApi {
    @PUT("/spatial/artifacts/{id}/content")
    suspend fun putArtifactContent(
        @Path("id") id: String,
        @Header("X-Spatial-Upload-Token") uploadToken: String,
        @Body body: RequestBody,
    ): Response<Unit>
}

@Serializable
data class StartCaptureRequest(val projectId: String, val spaceId: String)

@Serializable
data class AdvanceCaptureRequest(val status: String)

@Serializable
data class SpatialCaptureDto(
    val id: String,
    val projectId: String,
    val spaceId: String,
    val status: String,
    val roomVersionId: String = "",
    val createdAt: String,
    val updatedAt: String,
)

@Serializable
data class SpatialSpaceStateDto(
    val spaceId: String,
    val currentRoomVersionId: String = "",
)

@Serializable
data class RequestArtifactUploadRequest(
    val captureId: String,
    val kind: String,
    val contentType: String,
    val declaredSize: Long,
    val checksum: String,
)

@Serializable
data class FinalizeArtifactUploadRequest(val uploadToken: String)

@Serializable
data class ArtifactUploadResponse(
    val artifact: SpatialArtifactDto,
    val uploadToken: String,
)

@Serializable
data class SpatialArtifactDto(
    val id: String,
    val captureId: String,
    val kind: String,
    val contentType: String,
    val declaredSize: Long,
    val actualSize: Long = 0,
    val status: String,
    val createdAt: String,
    val uploadedAt: String = "",
)
