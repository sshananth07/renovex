import SwiftUI
import RenovexCaptureCore

/// The Space-level entry point to spatial capture (Task 4 UX acceptance:
/// "Projects -> Project -> Spaces -> Space -> Scan Room / AR Review /
/// Concepts"). RP3 replaces the static "AR Review"/"Concepts" stub rows
/// with a real Scan History list sourced from `SpatialCaptureRepository`
/// (design spec §8.22/§8.24, plan §RP3 §8):
///
/// ```text
/// No scan            -> [Scan Room]
/// Pending/unconfirmed -> [Continue Review]
/// Confirmed           -> [View Room] (RP4+, not implemented here)
/// ```
///
/// Do not show controls that look interactive unless they actually work
/// (plan §RP3 §8) — "View Room"/"Edit"/"AR Review" stay text-only until
/// RP4/RP5/RP6 land.
struct SpaceDetailView: View {
    let projectID: String
    let spaceID: String
    let spaceName: String
    let captureRepository: FileSpatialCaptureRepository
    let roomDraftRepository: FileRoomDraftRepository

    @State private var runs: [LocalCaptureRun] = []
    @State private var isLoading = true
    @State private var loadError: Error?

    var body: some View {
        List {
            Section {
                NavigationLink("Scan Room") {
                    ScanRoomView(
                        projectID: projectID, spaceID: spaceID,
                        captureRepository: captureRepository, roomDraftRepository: roomDraftRepository
                    )
                }
            }

            Section("Scan History") {
                if isLoading {
                    ProgressView()
                } else if let loadError {
                    Text("Could not load scan history: \(loadError.localizedDescription)")
                        .foregroundStyle(.secondary)
                } else if runs.isEmpty {
                    Text("No scan yet")
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(runs) { run in
                        ScanHistoryRow(run: run)
                    }
                }
            } footer: {
                Text("AR Review and Concepts become available after a room has been captured and confirmed.")
            }
        }
        .navigationTitle(spaceName.isEmpty ? "Space" : spaceName)
        .task { await loadHistory() }
        .refreshable { await loadHistory() }
    }

    private func loadHistory() async {
        isLoading = true
        do {
            runs = try await captureRepository.listRuns(spaceID: spaceID)
            loadError = nil
        } catch {
            loadError = error
        }
        isLoading = false
    }
}

/// One Scan History row (design spec §8.24: "#3 current", "#2 awaiting
/// review", "#1 superseded"). `status`'s wording is adapted to RP3's actual
/// implemented lifecycle (`pendingReview`/`confirmed`/`superseded`), not
/// RP4+'s eventual full editor states.
private struct ScanHistoryRow: View {
    let run: LocalCaptureRun

    var body: some View {
        HStack {
            VStack(alignment: .leading) {
                Text("Scan #\(run.captureNumber)")
                    .font(.headline)
                Text(statusLabel)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if run.status == .pendingReview {
                Text("Continue Review")
                    .font(.caption)
                    .foregroundStyle(.tint)
            }
        }
    }

    private var statusLabel: String {
        switch run.status {
        case .pendingReview: "Awaiting review"
        case .confirmed: "Confirmed"
        case .superseded: "Superseded"
        }
    }
}
