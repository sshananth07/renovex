import Foundation

/// Holds the in-memory access token. Never persisted — the same convention
/// `apps/web/src/lib/api/accessToken.ts` and the Android `AccessTokenHolder`
/// follow (design spec §42-adjacent security posture: least-exposure token
/// handling). An `actor` gives thread-safe get/set without a manual lock,
/// which the Kotlin/TS equivalents don't need to worry about but Swift
/// concurrency does.
public actor AccessTokenStore {
    private var token: String?

    public init() {}

    public func get() -> String? { token }

    public func set(_ newToken: String?) {
        token = newToken
    }
}
