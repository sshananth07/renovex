package com.renovex.capture.auth

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.preferencesDataStore
import com.renovex.capture.network.AuthApi
import com.renovex.capture.network.LoginRequest
import com.renovex.capture.network.NetworkModule
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.sessionDataStore by preferencesDataStore(name = "renovex_session")
private val HAS_SESSION_KEY = booleanPreferencesKey("has_session")

/**
 * Owns the app's session lifecycle: login, restart restoration (refresh
 * using the persisted cookie), and logout. Delegates all state-transition
 * decisions to [AuthSessionReducer] so those decisions stay unit-testable
 * without a real network or DataStore (Task 4 TDD requirement).
 */
class AuthRepository(
    private val context: Context,
    private val network: NetworkModule,
) {
    private val authApi: AuthApi get() = network.authApi
    private val dataStore = context.applicationContext.sessionDataStore

    val hasPersistedSession: Flow<Boolean> = dataStore.data.map { it[HAS_SESSION_KEY] ?: false }

    suspend fun login(email: String, password: String): AuthSessionState {
        val response = safeAuthRequest { authApi.login(LoginRequest(email, password)) }
            ?: return AuthSessionState.LoggedOut
        val body = response.body()
        if (!response.isSuccessful || body == null) {
            return AuthSessionState.LoggedOut
        }
        network.accessTokenHolder.set(body.accessToken)
        markSessionPersisted(true)
        return AuthSessionReducer.onLoginSuccess(body.accessToken)
    }

    /**
     * Called once at app start. Reads the persisted-session flag; if set,
     * attempts a refresh using the persisted refresh_token cookie
     * (RenovexCookieJar) to obtain a fresh in-memory access token — the
     * access token itself never survives process death by design.
     */
    suspend fun restoreSession(): AuthSessionState {
        val persisted = hasPersistedSession.first()
        val initial = AuthSessionReducer.onAppStart(persisted)
        if (initial !is AuthSessionState.Unknown) return initial

        val response = runCatching { authApi.refresh() }.getOrNull()
        val body = response?.takeIf { it.isSuccessful }?.body()
        return if (body != null) {
            network.accessTokenHolder.set(body.accessToken)
            AuthSessionReducer.onRefreshSuccess(body.accessToken)
        } else {
            markSessionPersisted(false)
            AuthSessionReducer.onRefreshFailed()
        }
    }

    suspend fun logout() {
        runCatching { authApi.logout() }
        network.accessTokenHolder.set(null)
        markSessionPersisted(false)
    }

    private suspend fun markSessionPersisted(value: Boolean) {
        dataStore.edit { prefs -> prefs[HAS_SESSION_KEY] = value }
    }
}
