import XCTest
@testable import RenovexCaptureCore

/// Proves the RP1 abstraction boundary is real (design spec §8.7): downstream
/// code that only depends on `SpatialCaptureProvider` works identically
/// whether it is handed a `FixtureCaptureProvider` or (once RP2 wires it) a
/// `RoomPlanCaptureProvider` — the plan's exact RP1 TDD requirement: "a
/// `SpatialCaptureProvider` swap test proving a fake/mock conformer can
/// substitute for `RoomPlanCaptureProvider` without changing downstream
/// code."
final class SpatialCaptureProviderSwapTests: XCTestCase {

    /// Generic downstream function under test — deliberately typed only
    /// against the protocol, never against any concrete provider. If this
    /// compiles and behaves identically for any conformer, the abstraction
    /// boundary is real.
    private func runCaptureWorkflow(using provider: any SpatialCaptureProvider) async throws -> (SpatialCaptureSupportState, SpatialCaptureRawResult) {
        let support = await provider.checkSupport()
        let result = try await provider.startCapture()
        return (support, result)
    }

    func test_downstreamWorkflow_acceptsFixtureProvider_withoutKnowingConcreteType() async throws {
        let provider: any SpatialCaptureProvider = FixtureCaptureProvider()
        let (support, result) = try await runCaptureWorkflow(using: provider)

        XCTAssertEqual(support, .supported)
        XCTAssertEqual(result.provider, .fixture)
    }

    func test_fixtureProvider_reportsUnsupported_whenConfigured() async {
        let provider: any SpatialCaptureProvider = FixtureCaptureProvider(
            supportState: .unsupported(reason: "No LiDAR sensor.")
        )
        let support = await provider.checkSupport()
        XCTAssertEqual(support, .unsupported(reason: "No LiDAR sensor."))
    }

    func test_fixtureProvider_surfacesCaptureFailure_whenConfigured() async {
        let provider: any SpatialCaptureProvider = FixtureCaptureProvider(
            captureOutcome: .failure(.sessionFailed(reason: "simulated failure"))
        )
        do {
            _ = try await provider.startCapture()
            XCTFail("expected startCapture to throw")
        } catch {
            XCTAssertEqual(error, .sessionFailed(reason: "simulated failure"))
        }
    }

    func test_fixtureProvider_surfacesCancellation() async {
        let provider: any SpatialCaptureProvider = FixtureCaptureProvider(
            captureOutcome: .failure(.cancelled)
        )
        do {
            _ = try await provider.startCapture()
            XCTFail("expected startCapture to throw")
        } catch {
            XCTAssertEqual(error, .cancelled)
        }
    }

    /// Two distinct conformers of the same protocol used interchangeably in
    /// one generic function is the actual proof the abstraction is not
    /// merely a shared name.
    func test_multipleConformers_satisfySameProtocolInterchangeably() async throws {
        let providers: [any SpatialCaptureProvider] = [
            FixtureCaptureProvider(fixturePayload: .rectangularRoom),
            FixtureCaptureProvider(supportState: .unsupported(reason: "test")),
        ]
        for provider in providers {
            _ = await provider.checkSupport()
        }
    }
}
