import Foundation

/// Persists only a boolean "a session was established" flag — never the
/// access token itself (that stays in-memory only, see `AccessTokenStore`).
/// Mirrors the frozen Android provider's `AuthRepository`'s DataStore-backed
/// `HAS_SESSION_KEY`. `UserDefaults` is the direct iOS analog for a single
/// non-sensitive boolean flag; the actual session credential is the
/// httpOnly `refresh_token` cookie, held by `URLSession`'s cookie storage,
/// not by this store.
public actor PersistedSessionFlagStore {
    private let defaults: UserDefaults
    private static let key = "renovex.hasPersistedSession"

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    public func hasPersistedSession() -> Bool {
        defaults.bool(forKey: Self.key)
    }

    public func setHasPersistedSession(_ value: Bool) {
        defaults.set(value, forKey: Self.key)
    }
}
