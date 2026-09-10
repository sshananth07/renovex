import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A deterministic `URLProtocol` stub for `RenovexAPIClient` tests — no
/// external mocking dependency needed, `URLProtocol` registration is the
/// standard Foundation-only way to intercept `URLSession` requests.
final class MockURLProtocol: URLProtocol {
    struct Stub: Sendable {
        let statusCode: Int
        let body: Data
        let headers: [String: String]

        init(statusCode: Int, body: Data = Data(), headers: [String: String] = [:]) {
            self.statusCode = statusCode
            self.body = body
            self.headers = headers
        }
    }

    /// Keyed by URL path (e.g. "/auth/login") so a test can queue distinct
    /// responses for distinct requests within one session.
    nonisolated(unsafe) static var stubsByPath: [String: [Stub]] = [:]
    nonisolated(unsafe) static var recordedRequests: [URLRequest] = []

    static func reset() {
        stubsByPath = [:]
        recordedRequests = []
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.recordedRequests.append(request)
        let path = request.url?.path ?? ""
        guard var queue = Self.stubsByPath[path], !queue.isEmpty else {
            client?.urlProtocol(self, didFailWithError: URLError(.fileDoesNotExist))
            return
        }
        let stub = queue.removeFirst()
        Self.stubsByPath[path] = queue

        let response = HTTPURLResponse(
            url: request.url!,
            statusCode: stub.statusCode,
            httpVersion: "HTTP/1.1",
            headerFields: stub.headers
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: stub.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

extension URLSession {
    static func mocked() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: config)
    }
}
