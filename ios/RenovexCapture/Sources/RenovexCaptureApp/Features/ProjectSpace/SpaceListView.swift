import SwiftUI
import RenovexCaptureCore

/// Space-level entry point for the Renovex spatial workflow.
///
/// Existing capture persistence is unchanged:
///
/// No scan
/// -> Scan Room
///
/// Pending review
/// -> Continue Review
///
/// Confirmed
/// -> retained as confirmed scan history
///
/// Superseded
/// -> historical record only
struct SpaceDetailView: View {
    let projectID: String
    let spaceID: String
    let spaceName: String
    let captureRepository: FileSpatialCaptureRepository
    let roomDraftRepository: FileRoomDraftRepository

    @Environment(\.dismiss) private var dismiss

    @State private var runs: [LocalCaptureRun] = []
    @State private var isLoading = true
    @State private var loadError: Error?
    @State private var hasAppeared = false

    private let renovexOrange = Color(
        red: 1.0,
        green: 0.42,
        blue: 0.08
    )

    private var displaySpaceName: String {
        let trimmed = spaceName.trimmingCharacters(
            in: .whitespacesAndNewlines
        )

        return trimmed.isEmpty
            ? "Space"
            : trimmed
    }

    private var latestRun: LocalCaptureRun? {
        runs.max {
            $0.captureNumber < $1.captureNumber
        }
    }

    private var pendingReviewRun: LocalCaptureRun? {
        runs
            .filter {
                $0.status == .pendingReview
            }
            .max {
                $0.captureNumber
                    < $1.captureNumber
            }
    }

    var body: some View {
        ZStack {
            background

            ScrollView {
                VStack(
                    alignment: .leading,
                    spacing: 0
                ) {
                    topBar
                        .padding(.bottom, 34)

                    header
                        .padding(.bottom, 28)

                    workspaceCard
                        .padding(.bottom, 34)

                    scanHistorySection

                    availabilityNotice
                        .padding(.top, 30)

                    Spacer(minLength: 50)
                }
                .padding(.horizontal, 22)
                .padding(.top, 10)
                .frame(maxWidth: 720)
                .frame(maxWidth: .infinity)
            }
            .refreshable {
                await loadHistory(
                    showLoadingState: false
                )
            }
        }
        .navigationBarBackButtonHidden(true)
        .toolbar(
            .hidden,
            for: .navigationBar
        )
        .task {
            await loadHistory()
        }
        .onAppear {
            withAnimation(
                .spring(
                    response: 0.7,
                    dampingFraction: 0.86
                )
            ) {
                hasAppeared = true
            }
        }
    }
}

// MARK: - Navigation

private extension SpaceDetailView {
    var topBar: some View {
        HStack {
            Button {
                dismiss()
            } label: {
                HStack(spacing: 8) {
                    ZStack {
                        Circle()
                            .fill(
                                Color.white.opacity(
                                    0.065
                                )
                            )

                        Circle()
                            .stroke(
                                Color.white.opacity(
                                    0.12
                                ),
                                lineWidth: 1
                            )

                        Image(
                            systemName:
                                "chevron.left"
                        )
                        .font(
                            .system(
                                size: 15,
                                weight: .bold
                            )
                        )
                        .foregroundStyle(.white)
                    }
                    .frame(
                        width: 42,
                        height: 42
                    )

                    Text("Spaces")
                        .font(
                            .subheadline.weight(
                                .semibold
                            )
                        )
                        .foregroundStyle(
                            .white.opacity(
                                0.7
                            )
                        )
                }
            }
            .buttonStyle(.plain)

            Spacer()

            HStack(spacing: 4) {
                Text("RENO")
                    .foregroundStyle(.white)

                Text("VEX")
                    .foregroundStyle(
                        renovexOrange
                    )
            }
            .font(
                .system(
                    size: 15,
                    weight: .semibold,
                    design: .rounded
                )
            )
            .tracking(3)
        }
    }
}

// MARK: - Header

private extension SpaceDetailView {
    var header: some View {
        VStack(
            alignment: .leading,
            spacing: 10
        ) {
            HStack(spacing: 8) {
                Image(
                    systemName: "viewfinder"
                )
                .font(.system(size: 10))

                Text("SPATIAL WORKSPACE")
                    .font(
                        .system(
                            size: 11,
                            weight: .bold
                        )
                    )
                    .tracking(1.6)
            }
            .foregroundStyle(
                renovexOrange
            )

            Text(displaySpaceName)
                .font(
                    .system(
                        size: 38,
                        weight: .bold,
                        design: .rounded
                    )
                )
                .foregroundStyle(.white)

            Text(
                "Capture and review the spatial record for this room."
            )
            .font(.system(size: 16))
            .foregroundStyle(
                .white.opacity(0.5)
            )
        }
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y: hasAppeared ? 0 : 18
        )
    }
}

// MARK: - Spatial Workspace

private extension SpaceDetailView {
    var workspaceCard: some View {
        VStack(
            alignment: .leading,
            spacing: 22
        ) {
            HStack {
                ZStack {
                    RoundedRectangle(
                        cornerRadius: 18
                    )
                    .fill(
                        renovexOrange.opacity(
                            0.13
                        )
                    )

                    Image(
                        systemName:
                            workspaceIcon
                    )
                    .font(
                        .system(
                            size: 25,
                            weight: .medium
                        )
                    )
                    .foregroundStyle(
                        renovexOrange
                    )
                }
                .frame(
                    width: 58,
                    height: 58
                )

                VStack(
                    alignment: .leading,
                    spacing: 5
                ) {
                    Text("ROOM CAPTURE")
                        .font(
                            .system(
                                size: 10,
                                weight: .bold
                            )
                        )
                        .tracking(1.5)
                        .foregroundStyle(
                            .white.opacity(
                                0.38
                            )
                        )

                    Text(workspaceTitle)
                        .font(
                            .title3.bold()
                        )
                        .foregroundStyle(.white)
                }

                Spacer()

                workspaceStatusBadge
            }

            Text(workspaceDescription)
                .font(.subheadline)
                .foregroundStyle(
                    .white.opacity(0.47)
                )
                .fixedSize(
                    horizontal: false,
                    vertical: true
                )

            workspaceAction
        }
        .padding(22)
        .background {
            RoundedRectangle(
                cornerRadius: 28
            )
            .fill(.ultraThinMaterial)
            .overlay {
                RoundedRectangle(
                    cornerRadius: 28
                )
                .fill(
                    Color.black.opacity(
                        0.3
                    )
                )
            }
            .overlay {
                RoundedRectangle(
                    cornerRadius: 28
                )
                .stroke(
                    LinearGradient(
                        colors: [
                            renovexOrange.opacity(
                                0.42
                            ),
                            Color.white.opacity(
                                0.08
                            ),
                            .clear
                        ],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    ),
                    lineWidth: 1
                )
            }
        }
        .shadow(
            color:
                renovexOrange.opacity(
                    0.08
                ),
            radius: 28,
            y: 14
        )
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y: hasAppeared ? 0 : 25
        )
    }

    var workspaceIcon: String {
        if pendingReviewRun != nil {
            return "square.and.pencil"
        }

        if latestRun?.status == .confirmed {
            return "checkmark.seal.fill"
        }

        return "camera.metering.matrix"
    }

    var workspaceTitle: String {
        if pendingReviewRun != nil {
            return "Review required"
        }

        if latestRun?.status == .confirmed {
            return "Room captured"
        }

        return "Ready to capture"
    }

    var workspaceDescription: String {
        if let pendingReviewRun {
            return """
            Scan #\(pendingReviewRun.captureNumber) has been captured and stored. Continue reviewing the persisted RoomDraft before confirmation.
            """
        }

        if latestRun?.status == .confirmed {
            return """
            The latest room capture has been confirmed. Start another scan only when you intentionally want to create a new capture version.
            """
        }

        return """
        No spatial scan has been recorded for this room yet. Use RoomPlan to capture its walls, openings and objects.
        """
    }

    @ViewBuilder
    var workspaceStatusBadge: some View {
        if pendingReviewRun != nil {
            statusPill(
                "REVIEW",
                icon:
                    "exclamationmark.circle.fill",
                highlighted: true
            )

        } else if latestRun?.status
            == .confirmed {

            statusPill(
                "CAPTURED",
                icon:
                    "checkmark.circle.fill",
                highlighted: false
            )

        } else {
            statusPill(
                "READY",
                icon:
                    "circle.dashed",
                highlighted: false
            )
        }
    }

    @ViewBuilder
    var workspaceAction: some View {
        if let pendingReviewRun {
            NavigationLink {
                RoomDraftReviewView(
                    run: pendingReviewRun,
                    spaceName:
                        displaySpaceName,
                    roomDraftRepository:
                        roomDraftRepository
                )
            } label: {
                primaryActionLabel(
                    "Continue Review",
                    icon:
                        "arrow.right"
                )
            }
            .buttonStyle(
                SpatialActionButtonStyle()
            )

        } else {
            NavigationLink {
                ScanRoomView(
                    projectID: projectID,
                    spaceID: spaceID,
                    captureRepository:
                        captureRepository,
                    roomDraftRepository:
                        roomDraftRepository
                )
            } label: {
                primaryActionLabel(
                    runs.isEmpty
                        ? "Scan Room"
                        : "Scan Again",
                    icon:
                        "camera.fill"
                )
            }
            .buttonStyle(
                SpatialActionButtonStyle()
            )
        }
    }

    func primaryActionLabel(
        _ title: String,
        icon: String
    ) -> some View {
        HStack(spacing: 10) {
            Text(title)

            Spacer()

            Image(systemName: icon)
        }
        .font(.headline)
        .foregroundStyle(.black)
        .padding(.horizontal, 20)
        .frame(
            maxWidth: .infinity
        )
        .frame(height: 56)
        .background {
            RoundedRectangle(
                cornerRadius: 17
            )
            .fill(
                LinearGradient(
                    colors: [
                        renovexOrange,
                        Color(
                            red: 1.0,
                            green: 0.55,
                            blue: 0.12
                        )
                    ],
                    startPoint: .leading,
                    endPoint: .trailing
                )
            )
        }
        .shadow(
            color:
                renovexOrange.opacity(
                    0.22
                ),
            radius: 16,
            y: 7
        )
    }
}

// MARK: - Scan History

private extension SpaceDetailView {
    var scanHistorySection: some View {
        VStack(
            alignment: .leading,
            spacing: 15
        ) {
            HStack {
                HStack(spacing: 8) {
                    Image(
                        systemName:
                            "clock.arrow.circlepath"
                    )
                    .font(.system(size: 11))

                    Text("SCAN HISTORY")
                        .font(
                            .system(
                                size: 11,
                                weight: .bold
                            )
                        )
                        .tracking(1.6)
                }
                .foregroundStyle(
                    .white.opacity(0.4)
                )

                Spacer()

                if !runs.isEmpty {
                    Text(
                        runs.count == 1
                            ? "1 RUN"
                            : "\(runs.count) RUNS"
                    )
                    .font(
                        .system(
                            size: 10,
                            weight: .bold
                        )
                    )
                    .tracking(1)
                    .foregroundStyle(
                        .white.opacity(
                            0.3
                        )
                    )
                }
            }

            if isLoading {
                loadingHistory

            } else if let loadError {
                historyError(loadError)

            } else if runs.isEmpty {
                emptyHistory

            } else {
                LazyVStack(spacing: 12) {
                    ForEach(
                        sortedRuns
                    ) { run in
                        scanHistoryCard(run)
                    }
                }
            }
        }
    }

    var sortedRuns: [LocalCaptureRun] {
        runs.sorted {
            $0.captureNumber
                > $1.captureNumber
        }
    }

    @ViewBuilder
    func scanHistoryCard(
        _ run: LocalCaptureRun
    ) -> some View {
        if run.status == .pendingReview {
            NavigationLink {
                RoomDraftReviewView(
                    run: run,
                    spaceName:
                        displaySpaceName,
                    roomDraftRepository:
                        roomDraftRepository
                )
            } label: {
                scanHistoryCardContent(
                    run
                )
            }
            .buttonStyle(
                SpatialHistoryButtonStyle()
            )

        } else {
            scanHistoryCardContent(run)
        }
    }

    func scanHistoryCardContent(
        _ run: LocalCaptureRun
    ) -> some View {
        HStack(spacing: 15) {
            ZStack {
                Circle()
                    .fill(
                        historyColor(
                            for: run.status
                        )
                        .opacity(0.12)
                    )

                Image(
                    systemName:
                        historyIcon(
                            for: run.status
                        )
                )
                .font(
                    .system(
                        size: 17,
                        weight: .semibold
                    )
                )
                .foregroundStyle(
                    historyColor(
                        for: run.status
                    )
                )
            }
            .frame(
                width: 46,
                height: 46
            )

            VStack(
                alignment: .leading,
                spacing: 6
            ) {
                HStack(spacing: 8) {
                    Text(
                        "Scan #\(run.captureNumber)"
                    )
                    .font(
                        .system(
                            size: 16,
                            weight: .semibold
                        )
                    )
                    .foregroundStyle(
                        .white
                    )

                    if run.id == latestRun?.id {
                        Text("CURRENT")
                            .font(
                                .system(
                                    size: 8,
                                    weight: .bold
                                )
                            )
                            .tracking(0.8)
                            .foregroundStyle(
                                renovexOrange
                            )
                            .padding(
                                .horizontal,
                                7
                            )
                            .frame(
                                height: 20
                            )
                            .background {
                                Capsule()
                                    .fill(
                                        renovexOrange
                                            .opacity(
                                                0.1
                                            )
                                    )
                            }
                    }
                }

                HStack(spacing: 7) {
                    Circle()
                        .fill(
                            historyColor(
                                for: run.status
                            )
                        )
                        .frame(
                            width: 6,
                            height: 6
                        )

                    Text(
                        statusLabel(
                            for: run.status
                        )
                    )
                    .font(
                        .caption
                            .weight(
                                .medium
                            )
                    )
                    .foregroundStyle(
                        .white.opacity(
                            0.43
                        )
                    )
                }
            }

            Spacer()

            if run.status
                == .pendingReview {

                HStack(spacing: 6) {
                    Text("Review")

                    Image(
                        systemName:
                            "chevron.right"
                    )
                }
                .font(
                    .caption.weight(
                        .semibold
                    )
                )
                .foregroundStyle(
                    renovexOrange
                )
            }
        }
        .padding(16)
        .background {
            RoundedRectangle(
                cornerRadius: 21
            )
            .fill(
                Color.white.opacity(
                    0.047
                )
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 21
                )
                .stroke(
                    run.status
                        == .pendingReview
                    ? renovexOrange
                        .opacity(0.22)
                    : Color.white
                        .opacity(0.08),
                    lineWidth: 1
                )
            }
        }
    }

    func historyColor(
        for status:
            LocalCaptureRun.Status
    ) -> Color {
        switch status {
        case .pendingReview:
            return renovexOrange

        case .confirmed:
            return Color.green

        case .superseded:
            return Color.white.opacity(
                0.35
            )
        }
    }

    func historyIcon(
        for status:
            LocalCaptureRun.Status
    ) -> String {
        switch status {
        case .pendingReview:
            return "square.and.pencil"

        case .confirmed:
            return "checkmark"

        case .superseded:
            return "clock.arrow.circlepath"
        }
    }

    func statusLabel(
        for status:
            LocalCaptureRun.Status
    ) -> String {
        switch status {
        case .pendingReview:
            return "Awaiting review"

        case .confirmed:
            return "Confirmed"

        case .superseded:
            return "Superseded"
        }
    }
}

// MARK: - History States

private extension SpaceDetailView {
    var loadingHistory: some View {
        HStack(spacing: 12) {
            ProgressView()
                .tint(renovexOrange)

            Text(
                "Loading scan history…"
            )
            .font(.subheadline)
            .foregroundStyle(
                .white.opacity(0.42)
            )
        }
        .padding(.vertical, 24)
    }

    var emptyHistory: some View {
        HStack(spacing: 15) {
            Image(
                systemName:
                    "camera.metering.none"
            )
            .font(.system(size: 22))
            .foregroundStyle(
                renovexOrange
            )

            VStack(
                alignment: .leading,
                spacing: 4
            ) {
                Text("No scans yet")
                    .font(
                        .subheadline.bold()
                    )
                    .foregroundStyle(
                        .white
                    )

                Text(
                    "Your RoomPlan captures will appear here."
                )
                .font(.caption)
                .foregroundStyle(
                    .white.opacity(
                        0.38
                    )
                )
            }
        }
        .padding(18)
        .frame(
            maxWidth: .infinity,
            alignment: .leading
        )
        .background {
            RoundedRectangle(
                cornerRadius: 20
            )
            .fill(
                Color.white.opacity(
                    0.04
                )
            )
        }
    }

    func historyError(
        _ error: Error
    ) -> some View {
        VStack(
            alignment: .leading,
            spacing: 12
        ) {
            HStack(spacing: 10) {
                Image(
                    systemName:
                        "exclamationmark.triangle.fill"
                )
                .foregroundStyle(
                    Color.red.opacity(
                        0.85
                    )
                )

                Text(
                    "Could not load scan history."
                )
                .font(
                    .subheadline
                        .weight(
                            .semibold
                        )
                )
                .foregroundStyle(.white)
            }

            Button {
                Task {
                    await loadHistory()
                }
            } label: {
                Text("Try again")
                    .font(
                        .subheadline
                            .weight(
                                .semibold
                            )
                    )
                    .foregroundStyle(
                        renovexOrange
                    )
            }
        }
        .padding(18)
        .frame(
            maxWidth: .infinity,
            alignment: .leading
        )
        .background {
            RoundedRectangle(
                cornerRadius: 20
            )
            .fill(
                Color.red.opacity(
                    0.045
                )
            )
        }
    }
}

// MARK: - Future Features

private extension SpaceDetailView {
    var availabilityNotice: some View {
        HStack(
            alignment: .top,
            spacing: 13
        ) {
            Image(
                systemName:
                    "cube.transparent.fill"
            )
            .font(.system(size: 17))
            .foregroundStyle(
                renovexOrange.opacity(
                    0.7
                )
            )

            VStack(
                alignment: .leading,
                spacing: 5
            ) {
                Text(
                    "AR Review & Concepts"
                )
                .font(
                    .subheadline
                        .weight(
                            .semibold
                        )
                )
                .foregroundStyle(
                    .white.opacity(
                        0.7
                    )
                )

                Text(
                    "Advanced spatial editing and concept tools become available through the later review workflow."
                )
                .font(.caption)
                .foregroundStyle(
                    .white.opacity(
                        0.34
                    )
                )
            }
        }
        .padding(18)
        .frame(
            maxWidth: .infinity,
            alignment: .leading
        )
        .background {
            RoundedRectangle(
                cornerRadius: 20
            )
            .fill(
                Color.white.opacity(
                    0.035
                )
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 20
                )
                .stroke(
                    Color.white.opacity(
                        0.065
                    ),
                    lineWidth: 1
                )
            }
        }
    }
}

// MARK: - Status Pill

private extension SpaceDetailView {
    func statusPill(
        _ text: String,
        icon: String,
        highlighted: Bool
    ) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon)

            Text(text)
        }
        .font(
            .system(
                size: 9,
                weight: .bold
            )
        )
        .tracking(0.7)
        .foregroundStyle(
            highlighted
                ? renovexOrange
                : Color.white.opacity(
                    0.48
                )
        )
        .padding(
            .horizontal,
            10
        )
        .frame(height: 28)
        .background {
            Capsule()
                .fill(
                    highlighted
                        ? renovexOrange
                            .opacity(
                                0.11
                            )
                        : Color.white
                            .opacity(
                                0.05
                            )
                )
        }
        .overlay {
            Capsule()
                .stroke(
                    highlighted
                        ? renovexOrange
                            .opacity(
                                0.25
                            )
                        : Color.white
                            .opacity(
                                0.08
                            ),
                    lineWidth: 1
                )
        }
    }
}

// MARK: - Background

private extension SpaceDetailView {
    var background: some View {
        ZStack {
            Color.black

            LinearGradient(
                colors: [
                    Color.black,

                    Color(
                        red: 0.055,
                        green: 0.043,
                        blue: 0.035
                    ),

                    Color.black
                ],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )

            Circle()
                .fill(
                    renovexOrange
                        .opacity(0.18)
                        .blur(radius: 105)
                )
                .frame(
                    width: 360,
                    height: 360
                )
                .offset(
                    x: 190,
                    y: -330
                )

            blueprintPattern
                .opacity(0.17)
        }
        .ignoresSafeArea()
    }

    var blueprintPattern: some View {
        GeometryReader { geometry in
            Path { path in
                let spacing:
                    CGFloat = 52

                stride(
                    from: 0,
                    through:
                        geometry.size.width,
                    by: spacing
                ).forEach { x in
                    path.move(
                        to: CGPoint(
                            x: x,
                            y: 0
                        )
                    )

                    path.addLine(
                        to: CGPoint(
                            x: x,
                            y:
                                geometry
                                    .size.height
                        )
                    )
                }

                stride(
                    from: 0,
                    through:
                        geometry.size.height,
                    by: spacing
                ).forEach { y in
                    path.move(
                        to: CGPoint(
                            x: 0,
                            y: y
                        )
                    )

                    path.addLine(
                        to: CGPoint(
                            x:
                                geometry
                                    .size.width,
                            y: y
                        )
                    )
                }
            }
            .stroke(
                renovexOrange.opacity(
                    0.09
                ),
                lineWidth: 0.5
            )
        }
    }
}

// MARK: - Loading

private extension SpaceDetailView {
    func loadHistory(
        showLoadingState: Bool = true
    ) async {
        if showLoadingState {
            isLoading = true
        }

        do {
            runs =
                try await captureRepository
                    .listRuns(
                        spaceID: spaceID
                    )

            loadError = nil
        } catch {
            loadError = error
        }

        isLoading = false
    }
}

// MARK: - Button Styles

private struct SpatialActionButtonStyle:
    ButtonStyle {

    func makeBody(
        configuration: Configuration
    ) -> some View {
        configuration.label
            .scaleEffect(
                configuration.isPressed
                    ? 0.975
                    : 1
            )
            .animation(
                .spring(
                    response: 0.24,
                    dampingFraction: 0.76
                ),
                value:
                    configuration.isPressed
            )
    }
}

private struct SpatialHistoryButtonStyle:
    ButtonStyle {

    func makeBody(
        configuration: Configuration
    ) -> some View {
        configuration.label
            .scaleEffect(
                configuration.isPressed
                    ? 0.985
                    : 1
            )
            .brightness(
                configuration.isPressed
                    ? 0.025
                    : 0
            )
            .animation(
                .easeOut(
                    duration: 0.15
                ),
                value:
                    configuration.isPressed
            )
    }
}