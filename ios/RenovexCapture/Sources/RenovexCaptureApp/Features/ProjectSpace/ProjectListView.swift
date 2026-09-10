import Foundation
import SwiftUI
import RenovexCaptureCore

struct ProjectListView: View {
    let session: AppSession
    let captureRepository: FileSpatialCaptureRepository
    let roomDraftRepository: FileRoomDraftRepository

    @State private var projects: [ProjectDTO] = []
    @State private var isLoading = true
    @State private var loadError: Error?
    @State private var searchText = ""
    @State private var selectedFilter: ProjectFilter = .all
    @State private var hasAppeared = false

    private enum ProjectFilter: String, CaseIterable, Identifiable {
        case all = "All"
        case active = "Active"

        var id: String { rawValue }
    }

    private let renovexOrange = Color(
        red: 1.0,
        green: 0.42,
        blue: 0.08
    )

    private var filteredProjects: [ProjectDTO] {
        let query = searchText
            .trimmingCharacters(
                in: .whitespacesAndNewlines
            )

        return projects.filter { project in
            let matchesFilter: Bool

            switch selectedFilter {
            case .all:
                matchesFilter = true

            case .active:
                matchesFilter = isActive(project)
            }

            let matchesSearch =
                query.isEmpty
                || project.name.localizedCaseInsensitiveContains(query)
                || project.scopeBrief.localizedCaseInsensitiveContains(query)

            return matchesFilter && matchesSearch
        }
    }

    private var featuredProject: ProjectDTO? {
        filteredProjects.first(
            where: { isActive($0) }
        ) ?? filteredProjects.first
    }

    private var secondaryProjects: [ProjectDTO] {
        guard let featuredProject else {
            return filteredProjects
        }

        return filteredProjects.filter {
            $0.id != featuredProject.id
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
                        .padding(.bottom, 36)

                    pageHeader
                        .padding(.bottom, 26)

                    searchBar
                        .padding(.bottom, 18)

                    filterBar
                        .padding(.bottom, 30)

                    content

                    Spacer(minLength: 50)
                }
                .padding(.horizontal, 22)
                .padding(.top, 12)
                .frame(maxWidth: 720)
                .frame(
                    maxWidth: .infinity,
                    alignment: .center
                )
            }
            .scrollDismissesKeyboard(
                .interactively
            )
            .refreshable {
                await load(
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
            await load()
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

// MARK: - Top Bar

private extension ProjectListView {
    var topBar: some View {
        HStack {
            VStack(
                alignment: .leading,
                spacing: 3
            ) {
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
                        size: 20,
                        weight: .semibold,
                        design: .rounded
                    )
                )
                .tracking(5)

                Text(
                    "CAPTURE  ·  PLAN  ·  BUILD"
                )
                .font(
                    .system(
                        size: 8,
                        weight: .medium
                    )
                )
                .tracking(3)
                .foregroundStyle(
                    .white.opacity(0.38)
                )
            }

            Spacer()

            Menu {
                Button(
                    role: .destructive
                ) {
                    Task {
                        await session.logout()
                    }
                } label: {
                    Label(
                        "Sign out",
                        systemImage:
                            "rectangle.portrait.and.arrow.right"
                    )
                }
            } label: {
                ZStack {
                    Circle()
                        .fill(
                            Color.white.opacity(
                                0.07
                            )
                        )

                    Circle()
                        .stroke(
                            Color.white.opacity(
                                0.13
                            ),
                            lineWidth: 1
                        )

                    Image(
                        systemName:
                            "person.crop.circle.fill"
                    )
                    .font(.system(size: 24))
                    .foregroundStyle(
                        .white.opacity(0.88)
                    )

                    Circle()
                        .fill(renovexOrange)
                        .frame(
                            width: 8,
                            height: 8
                        )
                        .overlay {
                            Circle()
                                .stroke(
                                    Color.black,
                                    lineWidth: 2
                                )
                        }
                        .offset(
                            x: 19,
                            y: -19
                        )
                }
                .frame(
                    width: 52,
                    height: 52
                )
                .contentShape(Circle())
            }
            .buttonStyle(.plain)
        }
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y: hasAppeared ? 0 : -15
        )
    }
}

// MARK: - Header

private extension ProjectListView {
    var pageHeader: some View {
        VStack(
            alignment: .leading,
            spacing: 9
        ) {
            Text("Projects")
                .font(
                    .system(
                        size: 40,
                        weight: .bold,
                        design: .rounded
                    )
                )
                .foregroundStyle(.white)

            Text(
                "Select a project to continue your spatial workflow."
            )
            .font(.system(size: 16))
            .foregroundStyle(
                .white.opacity(0.5)
            )
        }
        .frame(
            maxWidth: .infinity,
            alignment: .leading
        )
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y: hasAppeared ? 0 : 18
        )
    }
}

// MARK: - Search

private extension ProjectListView {
    var searchBar: some View {
        HStack(spacing: 14) {
            Image(
                systemName: "magnifyingglass"
            )
            .font(
                .system(
                    size: 18,
                    weight: .medium
                )
            )
            .foregroundStyle(
                searchText.isEmpty
                    ? Color.white.opacity(0.35)
                    : renovexOrange
            )

            TextField(
                "",
                text: $searchText,
                prompt: Text(
                    "Search projects"
                )
                .foregroundStyle(
                    .white.opacity(0.35)
                )
            )
            .foregroundStyle(.white)
            .tint(renovexOrange)
            .textInputAutocapitalization(
                .never
            )
            .autocorrectionDisabled()

            if !searchText.isEmpty {
                Button {
                    withAnimation(
                        .easeOut(
                            duration: 0.18
                        )
                    ) {
                        searchText = ""
                    }
                } label: {
                    Image(
                        systemName:
                            "xmark.circle.fill"
                    )
                    .foregroundStyle(
                        .white.opacity(0.4)
                    )
                }
                .buttonStyle(.plain)
                .transition(
                    .scale.combined(
                        with: .opacity
                    )
                )
            }
        }
        .padding(.horizontal, 18)
        .frame(height: 58)
        .background {
            RoundedRectangle(
                cornerRadius: 18
            )
            .fill(
                Color.white.opacity(0.06)
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 18
                )
                .stroke(
                    LinearGradient(
                        colors: [
                            renovexOrange.opacity(
                                searchText.isEmpty
                                    ? 0.14
                                    : 0.58
                            ),
                            Color.white.opacity(
                                0.1
                            )
                        ],
                        startPoint: .leading,
                        endPoint: .trailing
                    ),
                    lineWidth: 1
                )
            }
        }
        .animation(
            .easeInOut(duration: 0.2),
            value: searchText
        )
    }
}

// MARK: - Filters

private extension ProjectListView {
    var filterBar: some View {
        HStack(spacing: 10) {
            ForEach(
                ProjectFilter.allCases
            ) { filter in
                Button {
                    withAnimation(
                        .spring(
                            response: 0.3,
                            dampingFraction: 0.8
                        )
                    ) {
                        selectedFilter = filter
                    }
                } label: {
                    Text(filter.rawValue)
                        .font(
                            .system(
                                size: 14,
                                weight: .semibold
                            )
                        )
                        .foregroundStyle(
                            selectedFilter == filter
                                ? Color.black
                                : Color.white.opacity(
                                    0.58
                                )
                        )
                        .padding(
                            .horizontal,
                            18
                        )
                        .frame(height: 38)
                        .background {
                            Capsule()
                                .fill(
                                    selectedFilter
                                        == filter
                                    ? renovexOrange
                                    : Color.white
                                        .opacity(
                                            0.055
                                        )
                                )
                        }
                        .overlay {
                            Capsule()
                                .stroke(
                                    selectedFilter
                                        == filter
                                    ? renovexOrange
                                    : Color.white
                                        .opacity(
                                            0.1
                                        ),
                                    lineWidth: 1
                                )
                        }
                }
                .buttonStyle(.plain)
            }

            Spacer()

            Text(resultCountText)
                .font(
                    .footnote.weight(
                        .medium
                    )
                )
                .foregroundStyle(
                    .white.opacity(0.35)
                )
        }
    }

    var resultCountText: String {
        let count = filteredProjects.count

        return count == 1
            ? "1 project"
            : "\(count) projects"
    }
}

// MARK: - Content

private extension ProjectListView {
    @ViewBuilder
    var content: some View {
        if isLoading {
            loadingView

        } else if let loadError {
            errorView(loadError)

        } else if projects.isEmpty {
            emptyView

        } else if filteredProjects.isEmpty {
            noResultsView

        } else {
            loadedProjects
        }
    }
}

// MARK: - Loaded Projects

private extension ProjectListView {
    var loadedProjects: some View {
        VStack(
            alignment: .leading,
            spacing: 30
        ) {
            if let featuredProject {
                VStack(
                    alignment: .leading,
                    spacing: 14
                ) {
                    sectionLabel(
                        title:
                            "CONTINUE PROJECT",
                        icon: "bolt.fill"
                    )

                    NavigationLink {
                        destination(
                            for: featuredProject
                        )
                    } label: {
                        featuredCard(
                            featuredProject
                        )
                    }
                    .buttonStyle(
                        ProjectCardButtonStyle()
                    )
                }
            }

            if !secondaryProjects.isEmpty {
                VStack(
                    alignment: .leading,
                    spacing: 14
                ) {
                    sectionLabel(
                        title:
                            "YOUR PROJECTS",
                        icon:
                            "square.grid.2x2.fill"
                    )

                    LazyVStack(
                        spacing: 14
                    ) {
                        ForEach(
                            Array(
                                secondaryProjects
                                    .enumerated()
                            ),
                            id: \.element.id
                        ) { index, project in

                            NavigationLink {
                                destination(
                                    for: project
                                )
                            } label: {
                                projectCard(
                                    project,
                                    index: index
                                )
                            }
                            .buttonStyle(
                                ProjectCardButtonStyle()
                            )
                        }
                    }
                }
            }
        }
    }

    func sectionLabel(
        title: String,
        icon: String
    ) -> some View {
        HStack(spacing: 8) {
            Image(systemName: icon)
                .font(.system(size: 10))

            Text(title)
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
    }

    func destination(
        for project: ProjectDTO
    ) -> some View {
        SpaceListView(
            session: session,
            projectID: project.id,
            projectName: project.name,
            captureRepository:
                captureRepository,
            roomDraftRepository:
                roomDraftRepository
        )
    }
}

// MARK: - Featured Card

private extension ProjectListView {
    func featuredCard(
        _ project: ProjectDTO
    ) -> some View {
        VStack(
            alignment: .leading,
            spacing: 0
        ) {
            ZStack(
                alignment: .bottomLeading
            ) {
                featuredArtwork

                LinearGradient(
                    colors: [
                        .clear,
                        Color.black.opacity(
                            0.82
                        )
                    ],
                    startPoint: .top,
                    endPoint: .bottom
                )

                VStack(
                    alignment: .leading,
                    spacing: 11
                ) {
                    statusBadge(
                        project.status
                    )

                    Text(project.name)
                        .font(
                            .system(
                                size: 25,
                                weight: .bold,
                                design: .rounded
                            )
                        )
                        .foregroundStyle(
                            .white
                        )
                        .lineLimit(2)
                }
                .padding(20)
            }
            .frame(height: 190)

            VStack(
                alignment: .leading,
                spacing: 18
            ) {
                if !project.scopeBrief
                    .trimmingCharacters(
                        in:
                            .whitespacesAndNewlines
                    )
                    .isEmpty {

                    Text(project.scopeBrief)
                        .font(
                            .system(
                                size: 15
                            )
                        )
                        .foregroundStyle(
                            .white.opacity(
                                0.55
                            )
                        )
                        .lineLimit(3)
                        .multilineTextAlignment(
                            .leading
                        )
                }

                HStack {
                    Label(
                        "Created \(formattedDate(project.createdAt))",
                        systemImage:
                            "calendar"
                    )
                    .font(.caption)
                    .foregroundStyle(
                        .white.opacity(0.4)
                    )

                    Spacer()

                    HStack(spacing: 8) {
                        Text(
                            "Continue project"
                        )

                        Image(
                            systemName:
                                "arrow.right"
                        )
                    }
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
            .padding(20)
        }
        .background {
            RoundedRectangle(
                cornerRadius: 28
            )
            .fill(
                .ultraThinMaterial
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 28
                )
                .fill(
                    Color.black.opacity(
                        0.32
                    )
                )
            }
        }
        .clipShape(
            RoundedRectangle(
                cornerRadius: 28
            )
        )
        .overlay {
            RoundedRectangle(
                cornerRadius: 28
            )
            .stroke(
                LinearGradient(
                    colors: [
                        renovexOrange.opacity(
                            0.45
                        ),
                        Color.white.opacity(
                            0.08
                        ),
                        Color.clear
                    ],
                    startPoint: .topLeading,
                    endPoint:
                        .bottomTrailing
                ),
                lineWidth: 1
            )
        }
        .shadow(
            color:
                renovexOrange.opacity(
                    0.09
                ),
            radius: 28,
            y: 14
        )
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y: hasAppeared ? 0 : 26
        )
    }

    var featuredArtwork: some View {
        ZStack {
            LinearGradient(
                colors: [
                    Color(
                        red: 0.16,
                        green: 0.095,
                        blue: 0.05
                    ),
                    Color(
                        red: 0.04,
                        green: 0.035,
                        blue: 0.03
                    )
                ],
                startPoint: .topLeading,
                endPoint:
                    .bottomTrailing
            )

            architecturalArtwork
                .opacity(0.75)

            Circle()
                .fill(
                    renovexOrange
                        .opacity(0.22)
                        .blur(radius: 45)
                )
                .frame(
                    width: 170,
                    height: 170
                )
                .offset(
                    x: 125,
                    y: -50
                )
        }
    }

    var architecturalArtwork: some View {
        GeometryReader { geometry in
            ZStack {
                Path { path in
                    let width =
                        geometry.size.width
                    let height =
                        geometry.size.height

                    path.move(
                        to: CGPoint(
                            x: width * 0.08,
                            y: height * 0.72
                        )
                    )

                    path.addLine(
                        to: CGPoint(
                            x: width * 0.33,
                            y: height * 0.35
                        )
                    )

                    path.addLine(
                        to: CGPoint(
                            x: width * 0.56,
                            y: height * 0.61
                        )
                    )

                    path.addLine(
                        to: CGPoint(
                            x: width * 0.77,
                            y: height * 0.25
                        )
                    )

                    path.addLine(
                        to: CGPoint(
                            x: width * 0.95,
                            y: height * 0.48
                        )
                    )
                }
                .stroke(
                    renovexOrange.opacity(
                        0.62
                    ),
                    style: StrokeStyle(
                        lineWidth: 1.2,
                        lineCap: .round,
                        lineJoin: .round
                    )
                )

                Image(
                    systemName:
                        "building.2.fill"
                )
                .font(
                    .system(
                        size: 75,
                        weight: .ultraLight
                    )
                )
                .foregroundStyle(
                    .white.opacity(0.1)
                )
                .offset(
                    x: 80,
                    y: -4
                )
            }
        }
    }
}

// MARK: - Project Cards

private extension ProjectListView {
    func projectCard(
        _ project: ProjectDTO,
        index: Int
    ) -> some View {
        HStack(
            alignment: .center,
            spacing: 16
        ) {
            projectIcon(
                index: index
            )

            VStack(
                alignment: .leading,
                spacing: 8
            ) {
                HStack(
                    alignment: .top,
                    spacing: 8
                ) {
                    Text(project.name)
                        .font(
                            .system(
                                size: 18,
                                weight:
                                    .semibold
                            )
                        )
                        .foregroundStyle(
                            .white
                        )
                        .lineLimit(2)
                        .multilineTextAlignment(
                            .leading
                        )

                    Spacer(
                        minLength: 4
                    )

                    statusBadge(
                        project.status,
                        compact: true
                    )
                }

                if !project.scopeBrief
                    .trimmingCharacters(
                        in:
                            .whitespacesAndNewlines
                    )
                    .isEmpty {

                    Text(project.scopeBrief)
                        .font(
                            .system(
                                size: 14
                            )
                        )
                        .foregroundStyle(
                            .white.opacity(
                                0.44
                            )
                        )
                        .lineLimit(2)
                        .multilineTextAlignment(
                            .leading
                        )
                }

                HStack(spacing: 6) {
                    Image(
                        systemName:
                            "calendar"
                    )

                    Text(
                        "Created \(formattedDate(project.createdAt))"
                    )
                }
                .font(.caption)
                .foregroundStyle(
                    .white.opacity(0.32)
                )
            }

            Image(
                systemName:
                    "chevron.right"
            )
            .font(
                .system(
                    size: 14,
                    weight: .bold
                )
            )
            .foregroundStyle(
                renovexOrange
            )
            .padding(.leading, 4)
        }
        .padding(16)
        .frame(
            maxWidth: .infinity,
            minHeight: 118,
            alignment: .leading
        )
        .background {
            RoundedRectangle(
                cornerRadius: 24
            )
            .fill(
                .ultraThinMaterial
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 24
                )
                .fill(
                    Color.black.opacity(
                        0.29
                    )
                )
            }
            .overlay {
                RoundedRectangle(
                    cornerRadius: 24
                )
                .stroke(
                    LinearGradient(
                        colors: [
                            Color.white
                                .opacity(
                                    0.11
                                ),
                            renovexOrange
                                .opacity(
                                    0.08
                                )
                        ],
                        startPoint:
                            .topLeading,
                        endPoint:
                            .bottomTrailing
                    ),
                    lineWidth: 1
                )
            }
        }
        .opacity(
            hasAppeared ? 1 : 0
        )
        .offset(
            y:
                hasAppeared
                ? 0
                : CGFloat(
                    18
                    + min(index, 6) * 4
                )
        )
        .animation(
            .spring(
                response: 0.58,
                dampingFraction: 0.88
            )
            .delay(
                Double(
                    min(index, 6)
                ) * 0.04
            ),
            value: hasAppeared
        )
    }

    func projectIcon(
        index: Int
    ) -> some View {
        ZStack {
            RoundedRectangle(
                cornerRadius: 18
            )
            .fill(
                LinearGradient(
                    colors: [
                        renovexOrange.opacity(
                            0.18
                        ),
                        Color.white.opacity(
                            0.025
                        )
                    ],
                    startPoint:
                        .topLeading,
                    endPoint:
                        .bottomTrailing
                )
            )

            RoundedRectangle(
                cornerRadius: 18
            )
            .stroke(
                renovexOrange.opacity(
                    0.18
                ),
                lineWidth: 1
            )

            Image(
                systemName:
                    projectSymbol(
                        index
                    )
            )
            .font(
                .system(
                    size: 24,
                    weight: .medium
                )
            )
            .foregroundStyle(
                renovexOrange
            )
        }
        .frame(
            width: 72,
            height: 72
        )
    }

    func projectSymbol(
        _ index: Int
    ) -> String {
        let symbols = [
            "house.fill",
            "building.2.fill",
            "hammer.fill",
            "square.3.layers.3d",
            "ruler.fill",
            "cube.transparent.fill"
        ]

        return symbols[
            index % symbols.count
        ]
    }
}

// MARK: - Status

private extension ProjectListView {
    func statusBadge(
        _ status: String,
        compact: Bool = false
    ) -> some View {
        HStack(spacing: 6) {
            Circle()
                .fill(
                    isActiveStatus(status)
                        ? renovexOrange
                        : Color.white.opacity(
                            0.4
                        )
                )
                .frame(
                    width: 6,
                    height: 6
                )

            Text(
                displayStatus(status)
            )
            .lineLimit(1)
        }
        .font(
            .system(
                size:
                    compact
                    ? 10
                    : 11,
                weight: .bold
            )
        )
        .tracking(0.6)
        .foregroundStyle(
            isActiveStatus(status)
                ? renovexOrange
                : Color.white.opacity(
                    0.55
                )
        )
        .padding(
            .horizontal,
            compact ? 9 : 11
        )
        .frame(
            height:
                compact ? 27 : 30
        )
        .background {
            Capsule()
                .fill(
                    isActiveStatus(status)
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
                    isActiveStatus(status)
                        ? renovexOrange
                            .opacity(
                                0.28
                            )
                        : Color.white
                            .opacity(
                                0.09
                            ),
                    lineWidth: 1
                )
        }
    }

    func isActive(
        _ project: ProjectDTO
    ) -> Bool {
        isActiveStatus(
            project.status
        )
    }

    func isActiveStatus(
        _ status: String
    ) -> Bool {
        status
            .trimmingCharacters(
                in: .whitespacesAndNewlines
            )
            .lowercased()
            == "active"
    }

    func displayStatus(
        _ status: String
    ) -> String {
        let cleaned = status
            .trimmingCharacters(
                in: .whitespacesAndNewlines
            )
            .replacingOccurrences(
                of: "_",
                with: " "
            )
            .replacingOccurrences(
                of: "-",
                with: " "
            )

        guard !cleaned.isEmpty else {
            return "PROJECT"
        }

        return cleaned.uppercased()
    }
}

// MARK: - Date Formatting

private extension ProjectListView {
    func formattedDate(
        _ rawValue: String
    ) -> String {
        let fractionalFormatter =
            ISO8601DateFormatter()

        fractionalFormatter
            .formatOptions = [
                .withInternetDateTime,
                .withFractionalSeconds
            ]

        let standardFormatter =
            ISO8601DateFormatter()

        standardFormatter
            .formatOptions = [
                .withInternetDateTime
            ]

        let date =
            fractionalFormatter.date(
                from: rawValue
            )
            ?? standardFormatter.date(
                from: rawValue
            )

        guard let date else {
            return rawValue
        }

        return date.formatted(
            .dateTime
                .day()
                .month(
                    .abbreviated
                )
                .year()
        )
    }
}

// MARK: - Loading

private extension ProjectListView {
    var loadingView: some View {
        VStack(spacing: 18) {
            ZStack {
                Circle()
                    .fill(
                        renovexOrange.opacity(
                            0.1
                        )
                    )
                    .frame(
                        width: 76,
                        height: 76
                    )

                ProgressView()
                    .controlSize(.large)
                    .tint(
                        renovexOrange
                    )
            }

            VStack(spacing: 6) {
                Text(
                    "Loading projects"
                )
                .font(.headline)
                .foregroundStyle(
                    .white
                )

                Text(
                    "Preparing your Renovex workspace."
                )
                .font(.subheadline)
                .foregroundStyle(
                    .white.opacity(
                        0.42
                    )
                )
            }
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 72)
    }
}

// MARK: - Error

private extension ProjectListView {
    func errorView(
        _ error: Error
    ) -> some View {
        VStack(spacing: 20) {
            ZStack {
                Circle()
                    .fill(
                        Color.red.opacity(
                            0.1
                        )
                    )
                    .frame(
                        width: 74,
                        height: 74
                    )

                Image(
                    systemName:
                        "exclamationmark.triangle.fill"
                )
                .font(
                    .system(size: 28)
                )
                .foregroundStyle(
                    Color.red.opacity(
                        0.88
                    )
                )
            }

            VStack(spacing: 7) {
                Text(
                    "Couldn't load projects"
                )
                .font(
                    .title3.bold()
                )
                .foregroundStyle(
                    .white
                )

                Text(
                    friendlyErrorMessage(
                        for: error
                    )
                )
                .font(.subheadline)
                .multilineTextAlignment(
                    .center
                )
                .foregroundStyle(
                    .white.opacity(
                        0.46
                    )
                )
            }

            Button {
                Task {
                    await load()
                }
            } label: {
                HStack(spacing: 8) {
                    Image(
                        systemName:
                            "arrow.clockwise"
                    )

                    Text("Try again")
                }
                .font(.headline)
                .foregroundStyle(
                    .black
                )
                .padding(
                    .horizontal,
                    24
                )
                .frame(height: 50)
                .background {
                    Capsule()
                        .fill(
                            renovexOrange
                        )
                }
            }
            .buttonStyle(
                ProjectActionButtonStyle()
            )
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 58)
        .padding(.horizontal, 26)
    }

    func friendlyErrorMessage(
        for error: Error
    ) -> String {
        if let urlError =
            error as? URLError {

            switch urlError.code {
            case .notConnectedToInternet,
                 .networkConnectionLost,
                 .cannotConnectToHost,
                 .cannotFindHost,
                 .dnsLookupFailed:

                return
                    "Check your internet connection and try again."

            case .timedOut:
                return
                    "The request timed out. Please try again."

            default:
                break
            }
        }

        if let apiError =
            error as? RenovexAPIError {

            switch apiError {
            case .httpError(
                let statusCode
            ) where
                (500...599)
                .contains(
                    statusCode
                ):

                return
                    "Renovex is temporarily unavailable."

            default:
                break
            }
        }

        return
            "Something went wrong while loading your projects."
    }
}

// MARK: - Empty States

private extension ProjectListView {
    var emptyView: some View {
        VStack(spacing: 19) {
            ZStack {
                RoundedRectangle(
                    cornerRadius: 24
                )
                .fill(
                    renovexOrange.opacity(
                        0.09
                    )
                )
                .frame(
                    width: 84,
                    height: 84
                )

                Image(
                    systemName:
                        "building.2.crop.circle"
                )
                .font(
                    .system(size: 35)
                )
                .foregroundStyle(
                    renovexOrange
                )
            }

            VStack(spacing: 7) {
                Text(
                    "No projects yet"
                )
                .font(
                    .title3.bold()
                )
                .foregroundStyle(
                    .white
                )

                Text(
                    "Your Renovex projects will appear here when they're available."
                )
                .font(.subheadline)
                .foregroundStyle(
                    .white.opacity(
                        0.44
                    )
                )
                .multilineTextAlignment(
                    .center
                )
            }
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 70)
        .padding(.horizontal, 30)
    }

    var noResultsView: some View {
        VStack(spacing: 18) {
            Image(
                systemName:
                    selectedFilter == .active
                    ? "bolt.slash"
                    : "magnifyingglass"
            )
            .font(
                .system(
                    size: 32,
                    weight: .medium
                )
            )
            .foregroundStyle(
                renovexOrange
            )

            VStack(spacing: 6) {
                Text(
                    searchText.isEmpty
                        ? "No active projects"
                        : "No matching projects"
                )
                .font(.headline)
                .foregroundStyle(
                    .white
                )

                Text(
                    searchText.isEmpty
                        ? "There are currently no projects marked as active."
                        : "Try a different project name or search term."
                )
                .font(.subheadline)
                .foregroundStyle(
                    .white.opacity(
                        0.42
                    )
                )
                .multilineTextAlignment(
                    .center
                )
            }

            if !searchText.isEmpty {
                Button(
                    "Clear search"
                ) {
                    withAnimation {
                        searchText = ""
                    }
                }
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

            if selectedFilter
                == .active {

                Button(
                    "Show all projects"
                ) {
                    withAnimation {
                        selectedFilter = .all
                    }
                }
                .font(
                    .subheadline
                        .weight(
                            .semibold
                        )
                )
                .foregroundStyle(
                    .white.opacity(
                        0.58
                    )
                )
            }
        }
        .frame(
            maxWidth: .infinity
        )
        .padding(.vertical, 65)
        .padding(.horizontal, 20)
    }
}

// MARK: - Background

private extension ProjectListView {
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
                        .opacity(0.19)
                        .blur(
                            radius: 105
                        )
                )
                .frame(
                    width: 360,
                    height: 360
                )
                .offset(
                    x: 190,
                    y: -330
                )

            Circle()
                .fill(
                    renovexOrange
                        .opacity(0.07)
                        .blur(
                            radius: 110
                        )
                )
                .frame(
                    width: 330,
                    height: 330
                )
                .offset(
                    x: -190,
                    y: 440
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
                        geometry.size
                            .width,
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
                                    .size
                                    .height
                        )
                    )
                }

                stride(
                    from: 0,
                    through:
                        geometry.size
                            .height,
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
                                    .size
                                    .width,
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
        .mask {
            LinearGradient(
                colors: [
                    .white,
                    .white.opacity(
                        0.25
                    ),
                    .clear
                ],
                startPoint:
                    .topTrailing,
                endPoint:
                    .bottomLeading
            )
        }
    }
}

// MARK: - Loading Data

private extension ProjectListView {
    func load(
        showLoadingState: Bool = true
    ) async {
        if showLoadingState {
            isLoading = true
        }

        do {
            let page =
                try await session
                    .listProjects()

            projects = page.items
            loadError = nil
        } catch {
            loadError = error
        }

        isLoading = false
    }
}

// MARK: - Interaction

private struct ProjectCardButtonStyle:
    ButtonStyle {

    func makeBody(
        configuration: Configuration
    ) -> some View {
        configuration.label
            .scaleEffect(
                configuration.isPressed
                    ? 0.978
                    : 1
            )
            .brightness(
                configuration.isPressed
                    ? 0.035
                    : 0
            )
            .animation(
                .spring(
                    response: 0.26,
                    dampingFraction: 0.78
                ),
                value:
                    configuration
                        .isPressed
            )
    }
}

private struct ProjectActionButtonStyle:
    ButtonStyle {

    func makeBody(
        configuration: Configuration
    ) -> some View {
        configuration.label
            .scaleEffect(
                configuration.isPressed
                    ? 0.96
                    : 1
            )
            .animation(
                .spring(
                    response: 0.24,
                    dampingFraction: 0.74
                ),
                value:
                    configuration
                        .isPressed
            )
    }
}