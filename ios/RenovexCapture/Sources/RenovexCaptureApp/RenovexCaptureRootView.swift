import SwiftUI
import RenovexCaptureCore

/// The app's top-level navigation shell (design spec/Task 4 UX acceptance,
/// reused unchanged for RP1's iOS foundation): Projects -> Project -> Spaces
/// -> Space -> Scan Room / AR Review / Concepts. Screens are pushed by
/// human-readable NAME alongside ID so titles never present raw database
/// IDs as primary navigation — same rule the frozen Android provider's
/// `RenovexNavHost` follows.
///
/// RP1 scope: establishes the navigation shell and screens through Space
/// selection. "Scan Room" is present as a destination stub only — RP2 wires
/// the actual RoomPlan capture flow behind it.
public struct RenovexCaptureRootView: View {
    @State private var session: AppSession
    @State private var didAttemptRestore = false
    private let captureRepository: FileSpatialCaptureRepository
    private let roomDraftRepository: FileRoomDraftRepository

    public init(session: AppSession, captureRepository: FileSpatialCaptureRepository, roomDraftRepository: FileRoomDraftRepository) {
        _session = State(initialValue: session)
        self.captureRepository = captureRepository
        self.roomDraftRepository = roomDraftRepository
    }

    public var body: some View {
        NavigationStack {
            content
        }
        .task {
            guard !didAttemptRestore else { return }
            didAttemptRestore = true
            await session.restoreSession()
        }
    }

    @ViewBuilder
    private var content: some View {
        switch session.authState {
        case .unknown:
            SplashView()
        case .loggedOut:
            LoginView(session: session)
        case .loggedIn:
            ProjectListView(session: session, captureRepository: captureRepository, roomDraftRepository: roomDraftRepository)
        }
    }
}

struct SplashView: View {
    var body: some View {
        ProgressView("Renovex")
    }
}
