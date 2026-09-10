import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// Errors surfaced by `RenovexAPIClient` calls.
public enum RenovexAPIError: Error, Sendable {
    case invalidResponse
    case httpError(statusCode: Int)
    case decodingFailed(underlying: Error)
}

/// The app's single authenticated HTTP client: bearer-token injection,
/// cookie-based refresh-token persistence (via `URLSession`'s shared
/// `HTTPCookieStorage`, the iOS equivalent of the Android
/// `RenovexCookieJar` / Web's `credentials: "include"`), and transparent
/// one-shot 401 → refresh → retry (design spec §42-adjacent; mirrors
/// `apps/web/src/lib/api/client.ts` and the Android `NetworkModule`
/// `Authenticator`).
///
/// Deliberately sets no `Origin` header — `URLSession` does not send one by
/// default for non-browser requests, which is exactly what lets
/// `backend/internal/platform/http/origin_guard.go`'s `DecideOriginGuard`
/// treat this app as a legitimate non-browser client (Task 1 inspection,
/// same reasoning already verified for the Android client).
public final class RenovexAPIClient: Sendable {
    private let baseURL: URL
    private let session: URLSession
    private let tokenStore: AccessTokenStore
    private let refresher: SingleFlightRefresh

    public init(baseURL: URL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
        let store = AccessTokenStore()
        self.tokenStore = store
        self.refresher = SingleFlightRefresh(baseURL: baseURL, session: session, tokenStore: store)
    }

    // MARK: Auth

    public func login(email: String, password: String) async throws -> AuthResponseBody {
        let body = LoginRequestBody(email: email, password: password)
        let data = try JSONEncoder().encode(body)
        var request = URLRequest(url: baseURL.appendingPathComponent("auth/login"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = data

        let (responseData, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw RenovexAPIError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else {
            throw RenovexAPIError.httpError(statusCode: http.statusCode)
        }
        let decoded: AuthResponseBody
        do {
            decoded = try JSONDecoder().decode(AuthResponseBody.self, from: responseData)
        } catch {
            throw RenovexAPIError.decodingFailed(underlying: error)
        }
        await tokenStore.set(decoded.accessToken)
        return decoded
    }

    /// Attempts a refresh using the persisted `refresh_token` cookie —
    /// called once at app launch when `hasPersistedSession` is true.
    public func restoreSession() async -> AuthSessionState {
        do {
            let token = try await refresher.ensureFreshToken()
            return .loggedIn(accessToken: token)
        } catch {
            return .loggedOut
        }
    }

    public func logout() async {
        var request = URLRequest(url: baseURL.appendingPathComponent("auth/logout"))
        request.httpMethod = "POST"
        _ = try? await session.data(for: request)
        await tokenStore.set(nil)
    }

    // MARK: Project / Space

    public func listProjects(page: Int? = nil, pageSize: Int? = nil, search: String? = nil) async throws -> PaginatedResponseBody<ProjectDTO> {
        var components = URLComponents(url: baseURL.appendingPathComponent("projects"), resolvingAgainstBaseURL: false)!
        components.queryItems = buildQueryItems(page: page, pageSize: pageSize, extra: search.map { [("search", $0)] } ?? [])
        return try await authenticatedGet(url: components.url!)
    }

    public func listSpaces(projectID: String, page: Int? = nil, pageSize: Int? = nil) async throws -> PaginatedResponseBody<SpaceDTO> {
        var components = URLComponents(url: baseURL.appendingPathComponent("spaces"), resolvingAgainstBaseURL: false)!
        components.queryItems = buildQueryItems(page: page, pageSize: pageSize, extra: [("projectId", projectID)])
        return try await authenticatedGet(url: components.url!)
    }

    // MARK: Spatial capture / artifact sync (plan §RP3.5/§RP4B0)

    public func startCapture(_ body: StartCaptureRequestBody) async throws -> SpatialCaptureDTO {
        try await authenticatedPost(path: "spatial/captures", body: body)
    }

    public func requestArtifactUpload(_ body: RequestArtifactUploadRequestBody) async throws -> ArtifactUploadResponseBody {
        try await authenticatedPost(path: "spatial/artifacts", body: body)
    }

    public func resumeArtifactUpload(artifactID: String) async throws -> ArtifactUploadResponseBody {
        try await authenticatedPost(path: "spatial/artifacts/\(artifactID)/resume", body: Optional<String>.none)
    }

    public func finalizeArtifactUpload(artifactID: String, uploadToken: String) async throws -> SpatialArtifactDTO {
        try await authenticatedPost(path: "spatial/artifacts/\(artifactID)/finalize", body: FinalizeArtifactUploadRequestBody(uploadToken: uploadToken))
    }

    /// Fetches one capture by its SERVER id — the server-backed reopen
    /// foundation (plan §RP3.5/§RP4B0 §7): proves the backend possesses
    /// enough durable data to reconstruct the same captured draft this
    /// device synced, without requiring any new backend contract (the
    /// existing `GET /spatial/captures/{id}` route, shipped in Task 2, is
    /// already sufficient).
    public func getCapture(id: String) async throws -> SpatialCaptureDTO {
        try await authenticatedGet(url: baseURL.appendingPathComponent("spatial/captures/\(id)"))
    }

    /// Lists every artifact for a server capture ID — the other half of
    /// the server-backed reopen foundation: a caller can confirm
    /// roomdraft_json (and any other synced artifact) actually landed and
    /// finalized server-side, using the existing shipped
    /// `GET /spatial/artifacts` route.
    public func listArtifacts(captureID: String) async throws -> [SpatialArtifactDTO] {
        var components = URLComponents(url: baseURL.appendingPathComponent("spatial/artifacts"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "captureId", value: captureID)]
        return try await authenticatedGet(url: components.url!)
    }

    /// Uploads `content` to artifactID's content endpoint, authenticated
    /// solely by `uploadToken` (NOT the bearer token — matches the
    /// backend's signed-URL-style authorization for this one external
    /// route, `RegisterExternalHandlers`/`PutArtifactContent`).
    public func putArtifactContent(artifactID: String, uploadToken: String, content: Data) async throws {
        var request = URLRequest(url: baseURL.appendingPathComponent("spatial/artifacts/\(artifactID)/content"))
        request.httpMethod = "PUT"
        request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        request.setValue(uploadToken, forHTTPHeaderField: "X-Spatial-Upload-Token")
        request.httpBody = content

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw RenovexAPIError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else {
            throw RenovexAPIError.httpError(statusCode: http.statusCode)
        }
        _ = data
    }

    // MARK: Internals

    private func buildQueryItems(page: Int?, pageSize: Int?, extra: [(String, String)]) -> [URLQueryItem] {
        var items = extra.map { URLQueryItem(name: $0.0, value: $0.1) }
        if let page { items.append(URLQueryItem(name: "page", value: String(page))) }
        if let pageSize { items.append(URLQueryItem(name: "pageSize", value: String(pageSize))) }
        return items
    }

    /// Performs a GET with the current bearer token, retrying exactly once
    /// via `SingleFlightRefresh` on a 401 — mirrors the Android
    /// `Authenticator`'s `priorResponse != null` guard against chaining a
    /// second refresh attempt by construction (this method only ever
    /// retries once, never loops).
    private func authenticatedGet<T: Decodable>(url: URL) async throws -> T {
        let (data, response) = try await performAuthenticatedRequest(url: url)
        guard let http = response as? HTTPURLResponse else { throw RenovexAPIError.invalidResponse }

        if http.statusCode == 401 {
            _ = try? await refresher.ensureFreshToken()
            let (retryData, retryResponse) = try await performAuthenticatedRequest(url: url)
            return try decodeOrThrow(data: retryData, response: retryResponse)
        }

        return try decodeOrThrow(data: data, response: response)
    }

    private func performAuthenticatedRequest(url: URL) async throws -> (Data, URLResponse) {
        var request = URLRequest(url: url)
        if let token = await tokenStore.get() {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return try await session.data(for: request)
    }

    /// POSTs an Encodable body with the current bearer token, retrying
    /// exactly once via `SingleFlightRefresh` on a 401 — the write-request
    /// counterpart to `authenticatedGet`, same single-retry contract.
    private func authenticatedPost<Body: Encodable, T: Decodable>(path: String, body: Body?) async throws -> T {
        let url = baseURL.appendingPathComponent(path)
        let bodyData: Data? = try body.map { try JSONEncoder().encode($0) }

        let (data, response) = try await performAuthenticatedPost(url: url, bodyData: bodyData)
        guard let http = response as? HTTPURLResponse else { throw RenovexAPIError.invalidResponse }

        if http.statusCode == 401 {
            _ = try? await refresher.ensureFreshToken()
            let (retryData, retryResponse) = try await performAuthenticatedPost(url: url, bodyData: bodyData)
            return try decodeOrThrow(data: retryData, response: retryResponse)
        }

        return try decodeOrThrow(data: data, response: response)
    }

    private func performAuthenticatedPost(url: URL, bodyData: Data?) async throws -> (Data, URLResponse) {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        if let token = await tokenStore.get() {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let bodyData {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = bodyData
        }
        return try await session.data(for: request)
    }

    private func decodeOrThrow<T: Decodable>(data: Data, response: URLResponse) throws -> T {
        guard let http = response as? HTTPURLResponse else { throw RenovexAPIError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else {
            throw RenovexAPIError.httpError(statusCode: http.statusCode)
        }
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw RenovexAPIError.decodingFailed(underlying: error)
        }
    }
}
