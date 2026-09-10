package com.renovex.capture.auth

import java.net.SocketTimeoutException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

class AuthNetworkRequestTest {

    @Test
    fun `socket timeout becomes a recoverable failed auth request`() = runTest {
        val result = safeAuthRequest<String> {
            throw SocketTimeoutException("backend unavailable")
        }

        assertNull(result)
    }

    @Test
    fun `successful auth request returns its value`() = runTest {
        assertEquals("token", safeAuthRequest { "token" })
    }

    @Test
    fun `coroutine cancellation is never converted into an auth failure`() {
        assertThrows(CancellationException::class.java) {
            runTest {
                safeAuthRequest<String> { throw CancellationException("cancelled") }
            }
        }
    }
}
