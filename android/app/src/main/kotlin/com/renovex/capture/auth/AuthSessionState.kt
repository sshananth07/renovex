package com.renovex.capture.auth

/**
 * The Android app's authenticated-session state, kept deterministic and
 * unit-testable separately from any real network/DataStore I/O (Task 4 TDD
 * requirement: "Unit tests for authenticated API state").
 *
 * accessToken lives in memory only (never persisted) — the same convention
 * apps/web/src/lib/api/accessToken.ts follows. Session survivability across
 * process death/restart comes from the httpOnly refresh_token cookie
 * (persisted by RenovexCookieJar) plus [hasPersistedSession], which is
 * TRUE as soon as a login/register succeeds and stays true until an
 * explicit logout — it does NOT mean the access token is still valid, only
 * that a refresh attempt is worth making on app start.
 */
sealed interface AuthSessionState {
    data object Unknown : AuthSessionState
    data object LoggedOut : AuthSessionState
    data class LoggedIn(val accessToken: String) : AuthSessionState
}

/**
 * Pure state-transition logic for the session, extracted from any
 * repository/DataStore so it can be unit tested without Android
 * instrumentation (Task 4 TDD requirement).
 */
object AuthSessionReducer {
    fun onLoginSuccess(accessToken: String): AuthSessionState = AuthSessionState.LoggedIn(accessToken)

    fun onLogout(): AuthSessionState = AuthSessionState.LoggedOut

    /** A 401 that survived a refresh attempt: the session is truly over. */
    fun onRefreshFailed(): AuthSessionState = AuthSessionState.LoggedOut

    fun onRefreshSuccess(accessToken: String): AuthSessionState = AuthSessionState.LoggedIn(accessToken)

    /**
     * App start with a persisted session flag but no in-memory access token
     * yet (process was killed and restarted) — the app must attempt a
     * refresh before deciding LoggedIn vs LoggedOut, so it starts Unknown
     * rather than guessing either terminal state.
     */
    fun onAppStart(hasPersistedSession: Boolean): AuthSessionState =
        if (hasPersistedSession) AuthSessionState.Unknown else AuthSessionState.LoggedOut
}
