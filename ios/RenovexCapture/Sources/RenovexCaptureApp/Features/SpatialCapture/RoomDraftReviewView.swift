import SwiftUI
import RenovexCaptureCore

struct RoomDraftReviewView: View {
    let run: LocalCaptureRun
    let spaceName: String
    let roomDraftRepository: FileRoomDraftRepository

    @Environment(\.dismiss) private var dismiss

    @State private var wallCount = 0
    @State private var openingCount = 0
    @State private var objectCount = 0

    @State private var isLoading = true
    @State private var loadError: Error?
    @State private var hasAppeared = false

    private let renovexOrange = Color(
        red: 1.0,
        green: 0.42,
        blue: 0.08
    )

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

                    content

                    Spacer(minLength: 50)
                }
                .padding(.horizontal, 22)
                .padding(.top, 10)
                .frame(maxWidth: 720)
                .frame(maxWidth: .infinity)
            }
        }
        .navigationBarBackButtonHidden(true)
        .toolbar(
            .hidden,
            for: .navigationBar
        )
        .task {
            await loadDraft()
        }
        .onAppear {
            withAnimation(
                .spring(
                    response: 0.65,
                    dampingFraction: 0.86
                )
            ) {
                hasAppeared = true
            }
        }
    }
}

// MARK: - Header

private extension RoomDraftReviewView {
    var topBar: some View {
        HStack {
            Button {
                dismiss()
            } label: {
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
            }
            .buttonStyle(.plain)

            Spacer()

            Text(
                "SCAN #\(run.captureNumber)"
            )
            .font(
                .system(
                    size: 10,
                    weight: .bold
                )
            )
            .tracking(1.4)
            .foregroundStyle(
                renovexOrange
            )
        }
    }

    var header: some View {
        VStack(
            alignment: .leading,
            spacing: 9
        ) {
            Text("ROOM REVIEW")
                .font(
                    .system(
                        size: 11,
                        weight: .bold
                    )
                )
                .tracking(1.6)
                .foregroundStyle(
                    renovexOrange
                )

            Text(spaceName)
                .font(
                    .system(
                        size: 36,
                        weight: .bold,
                        design: .rounded
                    )
                )
                .foregroundStyle(.white)

            Text(
                "Review the persisted RoomPlan capture before it becomes confirmed spatial truth."
            )
            .font(.system(size: 16))
            .foregroundStyle(
                .white.opacity(0.5)
            )
        }
    }
}

// MARK: - Content

private extension RoomDraftReviewView {
    @ViewBuilder
    var content: some View {
        if isLoading {
            loadingView

        } else if loadError != nil {
            errorView

        } else {
            reviewContent
        }
    }

    var reviewContent: some View {
        VStack(spacing: 22) {
            reviewStatusCard

            HStack(spacing: 12) {
                metricCard(
                    value: wallCount,
                    title: "Walls",
                    icon:
                        "rectangle.split.3x1"
                )

                metricCard(
                    value: openingCount,
                    title: "Openings",
                    icon:
                        "door.left.hand.open"
                )

                metricCard(
                    value: objectCount,
                    title: "Objects",
                    icon:
                        "cube.fill"
                )
            }

            limitationNotice
        }
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y: hasAppeared ? 0 : 22
        )
    }
}

// MARK: - Review Status

private extension RoomDraftReviewView {
    var reviewStatusCard: some View {
        VStack(
            alignment: .leading,
            spacing: 18
        ) {
            HStack {
                ZStack {
                    Circle()
                        .fill(
                            renovexOrange.opacity(
                                0.12
                            )
                        )

                    Image(
                        systemName:
                            "square.and.pencil"
                    )
                    .font(
                        .system(
                            size: 23,
                            weight: .medium
                        )
                    )
                    .foregroundStyle(
                        renovexOrange
                    )
                }
                .frame(
                    width: 54,
                    height: 54
                )

                VStack(
                    alignment: .leading,
                    spacing: 5
                ) {
                    Text(
                        "AWAITING REVIEW"
                    )
                    .font(
                        .system(
                            size: 9,
                            weight: .bold
                        )
                    )
                    .tracking(1.2)
                    .foregroundStyle(
                        renovexOrange
                    )

                    Text(
                        "RoomDraft loaded"
                    )
                    .font(
                        .title3.bold()
                    )
                    .foregroundStyle(
                        .white
                    )
                }

                Spacer()
            }

            Text(
                "This is the persisted draft generated from Scan #\(run.captureNumber). Reopening this screen does not create a new capture."
            )
            .font(.subheadline)
            .foregroundStyle(
                .white.opacity(0.46)
            )
        }
        .padding(21)
        .background {
            RoundedRectangle(
                cornerRadius: 26
            )
            .fill(.ultraThinMaterial)
            .overlay {
                RoundedRectangle(
                    cornerRadius: 26
                )
                .fill(
                    Color.black.opacity(
                        0.3
                    )
                )
            }
            .overlay {
                RoundedRectangle(
                    cornerRadius: 26
                )
                .stroke(
                    renovexOrange.opacity(
                        0.32
                    ),
                    lineWidth: 1
                )
            }
        }
    }
}

// MARK: - Metrics

private extension RoomDraftReviewView {
    func metricCard(
        value: Int,
        title: String,
        icon: String
    ) -> some View {
        VStack(spacing: 10) {
            Image(systemName: icon)
                .font(
                    .system(
                        size: 19,
                        weight: .medium
                    )
                )
                .foregroundStyle(
                    renovexOrange
                )

            Text("\(value)")
                .font(
                    .system(
                        size: 25,
                        weight: .bold,
                        design: .rounded
                    )
                )
                .foregroundStyle(.white)

            Text(title)
                .font(.caption)
                .foregroundStyle(
                    .white.opacity(
                        0.4
                    )
                )
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 18)
        .background {
            RoundedRectangle(
                cornerRadius: 20
            )
            .fill(
                Color.white.opacity(
                    0.05
                )
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 20
                )
                .stroke(
                    Color.white.opacity(
                        0.08
                    ),
                    lineWidth: 1
                )
            }
        }
    }
}

// MARK: - Current RP3 Boundary

private extension RoomDraftReviewView {
    var limitationNotice: some View {
        HStack(
            alignment: .top,
            spacing: 13
        ) {
            Image(
                systemName:
                    "info.circle.fill"
            )
            .foregroundStyle(
                renovexOrange
            )

            VStack(
                alignment: .leading,
                spacing: 5
            ) {
                Text(
                    "Interactive review tools"
                )
                .font(
                    .subheadline
                        .weight(
                            .semibold
                        )
                )
                .foregroundStyle(.white)

                Text(
                    "The RoomDraft is persisted and can be reopened safely. Interactive geometry editing and confirmation controls are not enabled in this iOS review screen yet."
                )
                .font(.caption)
                .foregroundStyle(
                    .white.opacity(
                        0.4
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
                renovexOrange.opacity(
                    0.045
                )
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 20
                )
                .stroke(
                    renovexOrange.opacity(
                        0.13
                    ),
                    lineWidth: 1
                )
            }
        }
    }
}

// MARK: - Loading / Error

private extension RoomDraftReviewView {
    var loadingView: some View {
        VStack(spacing: 16) {
            ProgressView()
                .controlSize(.large)
                .tint(
                    renovexOrange
                )

            Text(
                "Loading RoomDraft…"
            )
            .font(.subheadline)
            .foregroundStyle(
                .white.opacity(0.45)
            )
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 70)
    }

    var errorView: some View {
        VStack(spacing: 18) {
            Image(
                systemName:
                    "exclamationmark.triangle.fill"
            )
            .font(.system(size: 30))
            .foregroundStyle(
                Color.red.opacity(
                    0.85
                )
            )

            Text(
                "Could not load this RoomDraft."
            )
            .font(.headline)
            .foregroundStyle(.white)

            Button("Try again") {
                Task {
                    await loadDraft()
                }
            }
            .font(
                .subheadline.weight(
                    .semibold
                )
            )
            .foregroundStyle(
                renovexOrange
            )
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 70)
    }
}

// MARK: - Background

private extension RoomDraftReviewView {
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
                endPoint:
                    .bottomTrailing
            )

            Circle()
                .fill(
                    renovexOrange
                        .opacity(0.17)
                        .blur(radius: 100)
                )
                .frame(
                    width: 350,
                    height: 350
                )
                .offset(
                    x: 190,
                    y: -330
                )
        }
        .ignoresSafeArea()
    }
}

// MARK: - Repository

private extension RoomDraftReviewView {
    func loadDraft() async {
        isLoading = true

        do {
            let draft =
                try await roomDraftRepository
                    .find(
                        forCapture: run.id
                    )

            wallCount =
                draft.walls.count

            openingCount =
                draft.openings.count

            objectCount =
                draft.objects.count

            loadError = nil
        } catch {
            loadError = error
        }

        isLoading = false
    }
}