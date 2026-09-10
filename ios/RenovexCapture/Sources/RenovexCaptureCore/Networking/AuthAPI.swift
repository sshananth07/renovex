import Foundation

/// Mirrors `backend/internal/identity/handler.go`'s JSON contract exactly
/// (Task 1 inspection, reused from the frozen Android provider's
/// `network/AuthApi.kt`). The `refresh_token` cookie itself is never
/// modeled here — `URLSession`'s shared `HTTPCookieStorage` handles it
/// transparently, matching the same cookie-based flow
/// `apps/web/src/lib/api/client.ts` and the Android `RenovexCookieJar` use.
public struct LoginRequestBody: Encodable, Sendable {
    public let email: String
    public let password: String

    public init(email: String, password: String) {
        self.email = email
        self.password = password
    }
}

public struct AuthResponseBody: Decodable, Sendable {
    public let accessToken: String
    public let mustChangePassword: Bool
}
