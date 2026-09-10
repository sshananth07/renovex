package com.renovex.capture.network

import kotlinx.serialization.Serializable
import retrofit2.Response
import retrofit2.http.GET
import retrofit2.http.Query

/**
 * Mirrors backend/internal/projects/handler.go and
 * backend/internal/spaces/handler.go's JSON contracts exactly (Task 1
 * inspection). Only the fields Android's project/space SELECTION flow
 * needs are modeled — this is a read-only capture-companion client, not a
 * full project-management client.
 */
interface ProjectSpaceApi {
    @GET("/projects")
    suspend fun listProjects(
        @Query("page") page: Int? = null,
        @Query("pageSize") pageSize: Int? = null,
        @Query("search") search: String? = null,
    ): Response<PaginatedResponse<ProjectDto>>

    @GET("/spaces")
    suspend fun listSpaces(
        @Query("projectId") projectId: String,
        @Query("page") page: Int? = null,
        @Query("pageSize") pageSize: Int? = null,
    ): Response<PaginatedResponse<SpaceDto>>
}

@Serializable
data class PaginatedResponse<T>(
    val items: List<T>,
    val page: Int,
    val pageSize: Int,
    val total: Int,
)

@Serializable
data class ProjectDto(
    val id: String,
    val clientId: String,
    val name: String,
    val status: String,
    val scopeBrief: String = "",
    val createdAt: String,
)

@Serializable
data class SpaceDto(
    val id: String,
    val projectId: String,
    val name: String,
    val type: String = "",
    val description: String = "",
    val createdAt: String,
)
