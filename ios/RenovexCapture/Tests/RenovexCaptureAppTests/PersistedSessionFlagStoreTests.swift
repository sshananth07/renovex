import XCTest
@testable import RenovexCaptureApp

final class PersistedSessionFlagStoreTests: XCTestCase {
    private func makeIsolatedDefaults() -> UserDefaults {
        let suiteName = "PersistedSessionFlagStoreTests.\(UUID().uuidString)"
        return UserDefaults(suiteName: suiteName)!
    }

    func test_initialState_isFalse() async {
        let store = PersistedSessionFlagStore(defaults: makeIsolatedDefaults())
        let value = await store.hasPersistedSession()
        XCTAssertFalse(value)
    }

    func test_setTrue_persists() async {
        let defaults = makeIsolatedDefaults()
        let store = PersistedSessionFlagStore(defaults: defaults)
        await store.setHasPersistedSession(true)
        let value = await store.hasPersistedSession()
        XCTAssertTrue(value)
    }

    func test_setFalse_afterTrue_clears() async {
        let defaults = makeIsolatedDefaults()
        let store = PersistedSessionFlagStore(defaults: defaults)
        await store.setHasPersistedSession(true)
        await store.setHasPersistedSession(false)
        let value = await store.hasPersistedSession()
        XCTAssertFalse(value)
    }
}
