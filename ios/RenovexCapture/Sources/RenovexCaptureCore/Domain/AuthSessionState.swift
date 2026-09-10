import Foundation

/// The app's authenticated-session state, kept deterministic and
/// unit-testable separately from any real network/Keychain I/O (RP1 TDD
/// requirement: "authenticated API state"). Mirrors the frozen Android
/// provider's `AuthSessionState`/`AuthSessionReducer` pattern
/// (android/app/src/main/kotlin/com/renovex/capture/auth/AuthSessionState.kt)
/// — same shared-backend session semantics, ported to idiomatic Swift
/// rather than translated line-by-line.
///
/// The access token lives in memory only (never persisted) — the same
/// convention `apps/web/src/lib/api/accessToken.ts` and the Android
/// `AccessTokenHolder` follow. Session survivability across app
/// termination/relaunch comes from the backend's httpOnly `refresh_token`
/// cookie (persisted by `URLSession`'s cookie storage) plus
/// `hasPersistedSession`, which becomes true as soon as a login succeeds and
/// stays true until an explicit logout — it does NOT mean the access token
/// is still valid, only that a refresh attempt is worth making on launch.
public enum AuthSessionState: Equatable, Sendable {
    case unknown
    case loggedOut
    case loggedIn(accessToken: String)
}

/// Pure state-transition logic for the session, extracted from any
/// repository/Keychain code so it is unit-testable without a real network
/// or device (RP1 TDD requirement).
public enum AuthSessionReducer {
    public static func onLoginSuccess(accessToken: String) -> AuthSessionState {
        .loggedIn(accessToken: accessToken)
    }

    public static func onLogout() -> AuthSessionState {
        .loggedOut
    }

    /// A 401 that survived a refresh attempt: the session is truly over.
    public static func onRefreshFailed() -> AuthSessionState {
        .loggedOut
    }

    public static func onRefreshSuccess(accessToken: String) -> AuthSessionState {
        .loggedIn(accessToken: accessToken)
    }

    /// App launch with a persisted-session flag but no in-memory access
    /// token yet (the process was terminated and relaunched) — the app must
    /// attempt a refresh before deciding loggedIn vs. loggedOut, so it
    /// starts `.unknown` rather than guessing either terminal state.
    public static func onAppLaunch(hasPersistedSession: Bool) -> AuthSessionState {
        hasPersistedSession ? .unknown : .loggedOut
    }
}
