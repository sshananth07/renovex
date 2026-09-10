import SwiftUI
import RenovexCaptureCore
import RenovexCaptureRoomPlan

/// The RP2 live RoomPlan capture screen, extended in RP3 to actually persist
/// its result instead of only summarizing it in memory. Uses RoomPlan's own
/// `RoomCaptureView` for scan guidance (design spec §8.7.1) and the existing
/// `SpatialCaptureProvider`/`SpatialCaptureSessionState` lifecycle from RP1.
///
/// RP3's required ordering (plan §RP3 §3): RoomPlan completion -> persist
/// source/raw result -> normalize RoomDraft -> persist RoomDraft -> THEN
/// present Room Review. This screen enforces the "-> present Room Review"
/// half by only reaching `.completed` after `CaptureCompletionCoordinator`
/// succeeds — a completed scan never exists only in this view's memory.
///
/// Scope boundary: this screen proves the capture pipeline reaches a
/// durably persisted `RoomDraft` end-to-end. It does NOT implement RP4's
/// interactive Room Review editor, RP5's Measure/Mark/Note tools, or the
/// raw `CapturedRoomData`/artifact upload step (RP6 backend-artifact scope)
/// — the completed `RoomDraft`'s wall/opening/object counts are shown as a
/// minimal completion confirmation only.
struct ScanRoomView: View {
    let projectID: String
    let spaceID: String
    let captureRepository: FileSpatialCaptureRepository
    let roomDraftRepository: FileRoomDraftRepository

    @State private var viewModel: ScanRoomViewModel

    init(
        projectID: String, spaceID: String,
        captureRepository: FileSpatialCaptureRepository, roomDraftRepository: FileRoomDraftRepository
    ) {
        self.projectID = projectID
        self.spaceID = spaceID
        self.captureRepository = captureRepository
        self.roomDraftRepository = roomDraftRepository
        _viewModel = State(initialValue: ScanRoomViewModel(
            projectID: projectID, spaceID: spaceID,
            captureRepository: captureRepository, roomDraftRepository: roomDraftRepository
        ))
    }

    var body: some View {
        content
            .navigationTitle("Scan Room")
            .task {
                await viewModel.checkSupport()
            }
    }

    @ViewBuilder
    private var content: some View {
        switch viewModel.sessionState {
        case .idle, .checkingSupport:
            ProgressView("Checking device support…")

        case .ready:
            VStack(spacing: 16) {
                ContentUnavailableView(
                    "Ready to scan",
                    systemImage: "camera.metering.matrix",
                    description: Text("This device supports RoomPlan. Start scanning to capture this room.")
                )
                Button("Start Scan") {
                    Task { await viewModel.startCapture() }
                }
                .buttonStyle(.borderedProminent)
            }

        case .capturing:
            ZStack(alignment: .bottom) {
                RoomPlanCaptureViewRepresentable(session: viewModel.roomPlanProvider.session)
                    .ignoresSafeArea()
                VStack(spacing: 8) {
                    Text("Walls: \(viewModel.liveWallCount)  Objects: \(viewModel.liveObjectCount)")
                        .font(.caption)
                        .padding(8)
                        .background(.thinMaterial, in: Capsule())
                    Button("Done") {
                        viewModel.finishCapture()
                    }
                    .buttonStyle(.borderedProminent)
                    .padding(.bottom)
                }
            }

        case .processing:
            ProgressView("Processing scan…")

        case .completed:
            ContentUnavailableView(
                "Scan Complete",
                systemImage: "checkmark.circle",
                description: Text(viewModel.completionSummary)
            )

        case .cancelled:
            ContentUnavailableView("Scan Cancelled", systemImage: "xmark.circle")

        case .failed(let reason):
            ContentUnavailableView(
                "Scan Failed",
                systemImage: "exclamationmark.triangle",
                description: Text(reason)
            )
        }
    }
}

@MainActor
@Observable
final class ScanRoomViewModel {
    let roomPlanProvider = RoomPlanCaptureProvider()
    private let adapter = RoomPlanCaptureAdapter()
    private let projectID: String
    private let spaceID: String
    private let captureRepository: FileSpatialCaptureRepository
    private let roomDraftRepository: FileRoomDraftRepository
    private let coordinator: CaptureCompletionCoordinator

    private(set) var sessionState: SpatialCaptureSessionState = .idle
    private(set) var completionSummary = ""

    var liveWallCount: Int { roomPlanProvider.liveCoordinator.wallTracker.count }
    var liveObjectCount: Int { roomPlanProvider.liveCoordinator.objectTracker.count }

    init(
        projectID: String, spaceID: String,
        captureRepository: FileSpatialCaptureRepository, roomDraftRepository: FileRoomDraftRepository
    ) {
        self.projectID = projectID
        self.spaceID = spaceID
        self.captureRepository = captureRepository
        self.roomDraftRepository = roomDraftRepository
        self.coordinator = CaptureCompletionCoordinator(captureRepository: captureRepository, roomDraftRepository: roomDraftRepository)
    }

    func checkSupport() async {
        sessionState = .checkingSupport
        let support = await roomPlanProvider.checkSupport()
        switch support {
        case .supported:
            sessionState = .ready
        case .unsupported(let reason):
            sessionState = .failed(reason: reason)
        case .unknown:
            sessionState = .failed(reason: "Unable to determine RoomPlan support.")
        }
    }

    /// Creates a new, durably persisted capture run BEFORE starting the live
    /// RoomPlan session — this is what makes "Scan Again" and multi-run
    /// numbering correct: every attempted scan gets its own run identity
    /// from the moment scanning begins, never only after it succeeds (plan
    /// §RP3 §7: "a new scan must create a new run").
    func startCapture() async {
        sessionState = .capturing
        do {
            let captureNumber = try await captureRepository.nextCaptureNumber(forSpace: spaceID)
            let run = LocalCaptureRun(
                id: UUID().uuidString, projectID: projectID, spaceID: spaceID,
                provider: .roomplan, captureNumber: captureNumber, status: .pendingReview,
                capturedAt: Date(), updatedAt: Date()
            )
            try await captureRepository.create(run)

            let rawResult = try await roomPlanProvider.startCapture()
            sessionState = .processing

            let updatedRun = try await coordinator.completeCapture(run: run, rawResult: rawResult, adapter: adapter)
            let draft = try await roomDraftRepository.find(forCapture: updatedRun.id)
            completionSummary = "\(draft.walls.count) walls, \(draft.openings.count) openings, \(draft.objects.count) objects captured."
            sessionState = .completed
        } catch {
            sessionState = .failed(reason: "\(error)")
        }
    }

    func finishCapture() {
        roomPlanProvider.stopSession()
    }
}
