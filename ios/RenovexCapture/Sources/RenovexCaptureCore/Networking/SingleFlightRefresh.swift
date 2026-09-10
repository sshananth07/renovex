import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// Errors surfaced by a failed `/auth/refresh` attempt.
public struct RefreshFailedError: Error, Sendable {
    public let statusCode: Int?

    public init(statusCode: Int? = nil) {
        self.statusCode = statusCode
    }
}

/// Coordinates concurrent 401-triggered refresh attempts (and the initial
/// session-restore call) into exactly one `POST /auth/refresh` in flight at
/// a time — the same single-flight guarantee as
/// `apps/web/src/features/auth/singleFlightRefresh.ts` and the Android
/// `NetworkModule`'s `Authenticator` (which gets this for free from
/// OkHttp's per-request `priorResponse` chaining; Swift needs an explicit
/// coordinator since `URLSession` has no built-in equivalent).
///
/// Performs the refresh with a bare `URLSession` request, never through the
/// authenticated client, so the refresh call itself can never recursively
/// trigger its own 401 → refresh retry path.
public actor SingleFlightRefresh {
    private let baseURL: URL
    private let session: URLSession
    private let tokenStore: AccessTokenStore
    private var inFlight: Task<String, Error>?

    public init(baseURL: URL, session: URLSession = .shared, tokenStore: AccessTokenStore) {
        self.baseURL = baseURL
        self.session = session
        self.tokenStore = tokenStore
    }

    public func ensureFreshToken() async throws -> String {
        if let existing = inFlight {
            return try await existing.value
        }
        let task = Task { [baseURL, session, tokenStore] () throws -> String in
            var request = URLRequest(url: baseURL.appendingPathComponent("auth/refresh"))
            request.httpMethod = "POST"
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
                await tokenStore.set(nil)
                let status = (response as? HTTPURLResponse)?.statusCode
                throw RefreshFailedError(statusCode: status)
            }
            let body = try JSONDecoder().decode(AuthResponseBody.self, from: data)
            await tokenStore.set(body.accessToken)
            return body.accessToken
        }
        inFlight = task
        defer { inFlight = nil }
        return try await task.value
    }
}
