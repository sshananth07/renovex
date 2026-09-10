package com.renovex.capture.capture.arcore

import com.google.ar.core.Plane
import com.renovex.capture.capture.reconstruction.PlaneCanonicalizer
import com.renovex.capture.capture.reconstruction.PlaneIdentityUpdate

/** Session-local opaque IDs around ARCore's equality-based Plane identity. */
class ArCorePlaneRegistry {
    private val rawIds = mutableMapOf<Plane, String>()
    private val canonicalizer = PlaneCanonicalizer()
    private var nextId = 1L

    fun idFor(plane: Plane): String = rawIds.getOrPut(plane) { "plane-${nextId++}" }

    fun canonicalIdFor(plane: Plane): String {
        val parent = plane.subsumedBy
        return canonicalizer.canonicalIdFor(
            PlaneIdentityUpdate(
                rawPlaneId = idFor(plane),
                subsumedByPlaneId = parent?.let(::idFor),
            ),
        )
    }

    fun activeCanonicalPlaneCount(): Int = canonicalizer.activeCanonicalIds().size
}
