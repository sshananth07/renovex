package com.renovex.capture.auth

import org.junit.Assert.assertEquals
import org.junit.Test

class AuthSessionReducerTest {

    @Test
    fun `login success transitions to LoggedIn with the token`() {
        val state = AuthSessionReducer.onLoginSuccess("token-abc")
        assertEquals(AuthSessionState.LoggedIn("token-abc"), state)
    }

    @Test
    fun `logout transitions to LoggedOut`() {
        val state = AuthSessionReducer.onLogout()
        assertEquals(AuthSessionState.LoggedOut, state)
    }

    @Test
    fun `failed refresh transitions to LoggedOut`() {
        val state = AuthSessionReducer.onRefreshFailed()
        assertEquals(AuthSessionState.LoggedOut, state)
    }

    @Test
    fun `successful refresh transitions to LoggedIn with the new token`() {
        val state = AuthSessionReducer.onRefreshSuccess("fresh-token")
        assertEquals(AuthSessionState.LoggedIn("fresh-token"), state)
    }

    @Test
    fun `app start with a persisted session is Unknown, not assumed LoggedIn`() {
        val state = AuthSessionReducer.onAppStart(hasPersistedSession = true)
        assertEquals(AuthSessionState.Unknown, state)
    }

    @Test
    fun `app start with no persisted session is LoggedOut`() {
        val state = AuthSessionReducer.onAppStart(hasPersistedSession = false)
        assertEquals(AuthSessionState.LoggedOut, state)
    }
}
