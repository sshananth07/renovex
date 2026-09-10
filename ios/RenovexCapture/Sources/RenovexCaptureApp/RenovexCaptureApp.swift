import SwiftUI
import RenovexCaptureCore

/// Backend base URL. Matches the frozen Android provider's
/// `BuildConfig.API_BASE_URL` convention (`android/app/build.gradle.kts`) —
/// a LAN IP for physical-device testing against the local Go API, updated
/// manually per development machine. Simulator builds may instead use
/// `http://localhost:8080` directly since the Simulator shares the host's
/// network namespace (unlike a physical device, which needs the LAN IP;
/// the Android convention's `10.0.2.2` emulator alias has no iOS Simulator
/// equivalent — `localhost` already works there).
enum RenovexEnvironment {
    static let apiBaseURL = URL(string: "http://192.168.1.12:8080")!
}

/// Where RP3's local capture/RoomDraft persistence lives on-device: two
/// subdirectories of the app's Application Support directory, one JSON file
/// per capture run / per draft (see `JSONFileStore`, `RenovexCaptureCore`).
/// A dedicated function (not inlined at each call site) so the app target
/// and any future debug/reset tooling agree on exactly one location.
func makeSpatialPersistence() -> (captures: FileSpatialCaptureRepository, drafts: FileRoomDraftRepository) {
    let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        .appendingPathComponent("RenovexCapture", isDirectory: true)
    // swiftlint:disable:next force_try — Application Support must be
    // writable for the app to function at all; a failure here is not a
    // recoverable runtime condition the UI could meaningfully react to.
    let captures = try! FileSpatialCaptureRepository(directoryURL: appSupport.appendingPathComponent("captures"))
    let drafts = try! FileRoomDraftRepository(directoryURL: appSupport.appendingPathComponent("roomdrafts"))
    return (captures, drafts)
}

@main
struct RenovexCaptureApp: App {
    @State private var session: AppSession
    private let captureRepository: FileSpatialCaptureRepository
    private let roomDraftRepository: FileRoomDraftRepository

    init() {
        let client = RenovexAPIClient(baseURL: RenovexEnvironment.apiBaseURL)
        let store = PersistedSessionFlagStore()
        _session = State(initialValue: AppSession(apiClient: client, sessionStore: store))
        let persistence = makeSpatialPersistence()
        self.captureRepository = persistence.captures
        self.roomDraftRepository = persistence.drafts
    }

    var body: some Scene {
        WindowGroup {
            RenovexCaptureRootView(session: session, captureRepository: captureRepository, roomDraftRepository: roomDraftRepository)
        }
    }
}
