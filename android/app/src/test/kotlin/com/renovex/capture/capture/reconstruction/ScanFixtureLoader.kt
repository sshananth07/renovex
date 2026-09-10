package com.renovex.capture.capture.reconstruction

import kotlinx.serialization.json.Json

/** Test-only loader: production scan code never depends on replay resources. */
object ScanFixtureLoader {
    private val json = Json {
        ignoreUnknownKeys = false
        allowSpecialFloatingPointValues = true
    }

    fun load(resourceName: String): ScanReplayFixture {
        val path = "scan-fixtures/$resourceName"
        val text = requireNotNull(javaClass.classLoader?.getResourceAsStream(path)) {
            "missing scan fixture: $path"
        }.bufferedReader().use { it.readText() }
        return decode(text)
    }

    fun decode(text: String): ScanReplayFixture =
        json.decodeFromString<ScanReplayFixture>(text).also(::validate)

    private fun validate(fixture: ScanReplayFixture) {
        require(fixture.name.isNotBlank()) { "fixture name must not be blank" }
        require(fixture.frames.isNotEmpty()) { "fixture must contain frames" }
        require(fixture.frames.zipWithNext().all { (a, b) -> a.timestampMillis <= b.timestampMillis }) {
            "fixture frames must be chronological"
        }
        fixture.frames.forEach { frame ->
            require(frame.cameraPosition.isFinite() && frame.cameraHeadingRadians.isFinite()) {
                "camera geometry must be finite"
            }
            frame.planes.forEach { plane ->
                require(plane.planeId.isNotBlank()) { "plane ID must not be blank" }
                require(plane.center.isFinite() && plane.worldNormal.isFinite()) {
                    "plane geometry must be finite"
                }
                require(plane.worldPolygon.size >= 2 && plane.worldPolygon.all { it.isFinite() }) {
                    "plane polygon must contain at least two finite points"
                }
            }
        }
    }

    private fun PointXZ.isFinite(): Boolean = x.isFinite() && z.isFinite()
    private fun VectorXZ.isFinite(): Boolean = x.isFinite() && z.isFinite()
}
