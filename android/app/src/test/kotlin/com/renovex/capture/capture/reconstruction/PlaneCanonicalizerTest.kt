package com.renovex.capture.capture.reconstruction

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class PlaneCanonicalizerTest {

    @Test
    fun `subsumed child and parent resolve to one active canonical plane`() {
        // Break caught: retaining the old child as an active identity would
        // let child + replacement + parent become duplicate wall evidence.
        val canonicalizer = PlaneCanonicalizer()

        assertEquals("plane-child", canonicalizer.canonicalIdFor(PlaneIdentityUpdate("plane-child", null)))
        assertEquals(
            "plane-parent",
            canonicalizer.canonicalIdFor(PlaneIdentityUpdate("plane-child", "plane-parent")),
        )
        assertEquals("plane-parent", canonicalizer.canonicalIdFor(PlaneIdentityUpdate("plane-parent", null)))
        assertEquals(setOf("plane-parent"), canonicalizer.activeCanonicalIds())
    }

    @Test
    fun `later parent subsumption remaps the whole known chain`() {
        val canonicalizer = PlaneCanonicalizer()
        canonicalizer.canonicalIdFor(PlaneIdentityUpdate("child", "parent"))
        canonicalizer.canonicalIdFor(PlaneIdentityUpdate("parent", "grandparent"))

        assertEquals("grandparent", canonicalizer.canonicalIdFor(PlaneIdentityUpdate("child", null)))
        assertEquals(setOf("grandparent"), canonicalizer.activeCanonicalIds())
    }

    @Test
    fun `canonicalizer rejects cyclic or self subsumption`() {
        val self = PlaneCanonicalizer()
        assertThrows(IllegalArgumentException::class.java) {
            self.canonicalIdFor(PlaneIdentityUpdate("plane", "plane"))
        }

        val cycle = PlaneCanonicalizer()
        cycle.canonicalIdFor(PlaneIdentityUpdate("a", "b"))
        assertThrows(IllegalArgumentException::class.java) {
            cycle.canonicalIdFor(PlaneIdentityUpdate("b", "a"))
        }
    }
}
