import XCTest
@testable import RenovexCaptureCore

final class AccessTokenStoreTests: XCTestCase {
    func test_initialState_hasNoToken() async {
        let store = AccessTokenStore()
        let token = await store.get()
        XCTAssertNil(token)
    }

    func test_setThenGet_returnsStoredToken() async {
        let store = AccessTokenStore()
        await store.set("abc123")
        let token = await store.get()
        XCTAssertEqual(token, "abc123")
    }

    func test_setNil_clearsToken() async {
        let store = AccessTokenStore()
        await store.set("abc123")
        await store.set(nil)
        let token = await store.get()
        XCTAssertNil(token)
    }
}
