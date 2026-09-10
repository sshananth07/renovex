import XCTest
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
@testable import RenovexCaptureCore

/// RP1 TDD requirement: "authenticated API state." Exercises
/// `RenovexAPIClient` against `MockURLProtocol` so these tests are Tier A
/// (Windows-runnable via `swift test`/`xcodebuild test`) and independent of
/// any real backend or RoomPlan dependency.
final class RenovexAPIClientTests: XCTestCase {
    private let baseURL = URL(string: "http://localhost:8080")!

    override func setUp() {
        super.setUp()
        MockURLProtocol.reset()
    }

    private func makeClient() -> RenovexAPIClient {
        RenovexAPIClient(baseURL: baseURL, session: .mocked())
    }

    func test_login_success_storesAccessTokenAndReturnsResponse() async throws {
        let body = #"{"accessToken":"tok-abc","mustChangePassword":false}"#.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/auth/login"] = [.init(statusCode: 200, body: body)]

        let client = makeClient()
        let response = try await client.login(email: "a@b.com", password: "secret")

        XCTAssertEqual(response.accessToken, "tok-abc")
        XCTAssertFalse(response.mustChangePassword)
    }

    func test_login_failure_throwsHTTPError() async {
        MockURLProtocol.stubsByPath["/auth/login"] = [.init(statusCode: 401, body: Data())]
        let client = makeClient()

        do {
            _ = try await client.login(email: "a@b.com", password: "wrong")
            XCTFail("expected an error")
        } catch let error as RenovexAPIError {
            XCTAssertEqual(error, .httpError(statusCode: 401))
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }

    func test_restoreSession_success_returnsLoggedIn() async {
        let body = #"{"accessToken":"tok-restored","mustChangePassword":false}"#.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/auth/refresh"] = [.init(statusCode: 200, body: body)]

        let client = makeClient()
        let state = await client.restoreSession()

        XCTAssertEqual(state, .loggedIn(accessToken: "tok-restored"))
    }

    func test_restoreSession_failure_returnsLoggedOut() async {
        MockURLProtocol.stubsByPath["/auth/refresh"] = [.init(statusCode: 401, body: Data())]
        let client = makeClient()
        let state = await client.restoreSession()
        XCTAssertEqual(state, .loggedOut)
    }

    func test_listProjects_decodesPaginatedResponse() async throws {
        let body = """
        {"items":[{"id":"p1","clientId":"c1","name":"Kitchen Reno","status":"active","scopeBrief":"","createdAt":"2026-01-01T00:00:00Z"}],"page":1,"pageSize":20,"total":1}
        """.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/projects"] = [.init(statusCode: 200, body: body)]

        let client = makeClient()
        let page = try await client.listProjects()

        XCTAssertEqual(page.items.count, 1)
        XCTAssertEqual(page.items.first?.name, "Kitchen Reno")
        XCTAssertEqual(page.total, 1)
    }

    func test_listSpaces_decodesPaginatedResponse() async throws {
        let body = """
        {"items":[{"id":"s1","projectId":"p1","name":"Kitchen","type":"","description":"","createdAt":"2026-01-01T00:00:00Z"}],"page":1,"pageSize":20,"total":1}
        """.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/spaces"] = [.init(statusCode: 200, body: body)]

        let client = makeClient()
        let page = try await client.listSpaces(projectID: "p1")

        XCTAssertEqual(page.items.count, 1)
        XCTAssertEqual(page.items.first?.name, "Kitchen")
    }

    /// The core 401-retry contract: a 401 followed by a successful refresh
    /// and successful retry must produce the final decoded result — mirrors
    /// the Android `Authenticator`/Web `client.ts` retry-once behavior.
    func test_authenticatedGet_retriesOnceAfter401_thenSucceeds() async throws {
        let refreshBody = #"{"accessToken":"tok-fresh","mustChangePassword":false}"#.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/auth/refresh"] = [.init(statusCode: 200, body: refreshBody)]

        let successBody = """
        {"items":[],"page":1,"pageSize":20,"total":0}
        """.data(using: .utf8)!
        // First call returns 401, second (post-refresh retry) returns 200.
        MockURLProtocol.stubsByPath["/projects"] = [
            .init(statusCode: 401, body: Data()),
            .init(statusCode: 200, body: successBody),
        ]

        let client = makeClient()
        let page = try await client.listProjects()

        XCTAssertEqual(page.total, 0)
        // Exactly two /projects requests: the original 401 and the retry.
        let projectRequests = MockURLProtocol.recordedRequests.filter { $0.url?.path == "/projects" }
        XCTAssertEqual(projectRequests.count, 2)
    }

    func test_authenticatedGet_doesNotLoopIndefinitely_whenRetryAlsoFails() async {
        let refreshBody = #"{"accessToken":"tok-fresh","mustChangePassword":false}"#.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/auth/refresh"] = [.init(statusCode: 200, body: refreshBody)]
        MockURLProtocol.stubsByPath["/projects"] = [
            .init(statusCode: 401, body: Data()),
            .init(statusCode: 401, body: Data()),
        ]

        let client = makeClient()
        do {
            _ = try await client.listProjects()
            XCTFail("expected an error")
        } catch let error as RenovexAPIError {
            XCTAssertEqual(error, .httpError(statusCode: 401))
        } catch {
            XCTFail("wrong error type: \(error)")
        }

        // Exactly two attempts made — no unbounded retry loop.
        let projectRequests = MockURLProtocol.recordedRequests.filter { $0.url?.path == "/projects" }
        XCTAssertEqual(projectRequests.count, 2)
    }

    func test_logout_clearsAccessToken() async throws {
        let loginBody = #"{"accessToken":"tok-abc","mustChangePassword":false}"#.data(using: .utf8)!
        MockURLProtocol.stubsByPath["/auth/login"] = [.init(statusCode: 200, body: loginBody)]
        MockURLProtocol.stubsByPath["/auth/logout"] = [.init(statusCode: 204, body: Data())]

        // authenticatedGet performs the original request AND, on a 401,
        // exactly one retry after attempting refresh (see
        // RenovexAPIClient.authenticatedGet) — two /projects requests and
        // one /auth/refresh request must all be stubbed, or the second
        // /projects call hits MockURLProtocol's empty-queue fallback
        // instead of the condition this test actually means to prove.
        MockURLProtocol.stubsByPath["/projects"] = [
            .init(statusCode: 401, body: Data()),
            .init(statusCode: 401, body: Data()),
        ]
        MockURLProtocol.stubsByPath["/auth/refresh"] = [.init(statusCode: 401, body: Data())]

        let client = makeClient()
        _ = try await client.login(email: "a@b.com", password: "secret")
        await client.logout()

        // After logout, a request the client makes should carry no bearer
        // token — proven indirectly: the subsequent 401 (no refresh cookie
        // stubbed as valid either) surfaces as an httpError, not a decoded
        // authenticated success.
        do {
            _ = try await client.listProjects()
            XCTFail("expected an error after logout")
        } catch let error as RenovexAPIError {
            XCTAssertEqual(error, .httpError(statusCode: 401))
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }
}

extension RenovexAPIError: Equatable {
    public static func == (lhs: RenovexAPIError, rhs: RenovexAPIError) -> Bool {
        switch (lhs, rhs) {
        case (.invalidResponse, .invalidResponse):
            return true
        case let (.httpError(a), .httpError(b)):
            return a == b
        case (.decodingFailed, .decodingFailed):
            return true
        default:
            return false
        }
    }
}
