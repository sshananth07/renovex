import XCTest
@testable import RenovexCaptureCore

/// RP1 TDD requirement: "authenticated API state." Ported directly from the
/// frozen Android provider's `AuthSessionReducer` test coverage — same
/// transitions, same rationale, Swift idiom.
final class AuthSessionReducerTests: XCTestCase {
    func test_onLoginSuccess_producesLoggedIn() {
        XCTAssertEqual(
            AuthSessionReducer.onLoginSuccess(accessToken: "tok"),
            .loggedIn(accessToken: "tok")
        )
    }

    func test_onLogout_producesLoggedOut() {
        XCTAssertEqual(AuthSessionReducer.onLogout(), .loggedOut)
    }

    func test_onRefreshFailed_producesLoggedOut() {
        XCTAssertEqual(AuthSessionReducer.onRefreshFailed(), .loggedOut)
    }

    func test_onRefreshSuccess_producesLoggedIn() {
        XCTAssertEqual(
            AuthSessionReducer.onRefreshSuccess(accessToken: "tok2"),
            .loggedIn(accessToken: "tok2")
        )
    }

    func test_onAppLaunch_withPersistedSession_startsUnknown_notAGuess() {
        // A persisted-session flag means "worth attempting a refresh," not
        // "definitely still logged in" — the reducer must not guess
        // loggedIn before that refresh actually succeeds.
        XCTAssertEqual(AuthSessionReducer.onAppLaunch(hasPersistedSession: true), .unknown)
    }

    func test_onAppLaunch_withoutPersistedSession_isLoggedOutImmediately() {
        XCTAssertEqual(AuthSessionReducer.onAppLaunch(hasPersistedSession: false), .loggedOut)
    }
}
