import SwiftUI
import RoomPlan
import RenovexCaptureRoomPlan

// APPLE ROOMPLAN API BRIDGE — RP2-MAC-014: RoomCaptureView's initializer
// and `captureSession` property. Documented by Apple as the way to display
// RoomPlan's own live scanning UX while still being able to configure/
// observe the underlying `RoomCaptureSession` — not yet Xcode-compiled.

/// Wraps Apple's `RoomCaptureView` for SwiftUI, configured to share the
/// `RoomCaptureSession` owned by a `RoomPlanCaptureProvider` so RoomPlan's
/// own live scan guidance UI (design spec §8.7.1: "RoomCaptureView → use
/// Apple's provided capture UX as much as possible") drives what the
/// contractor sees, while `RoomPlanCaptureProvider.startCapture()`
/// independently observes the same session's completion.
///
/// This is deliberately a thin wrapper — RP2 does not rebuild Apple's
/// scanning HUD (design spec §8.17, RP2 amendment §2's explicit
/// prohibition) and does not add Measure/Mark/Note tools (RP5 scope).
struct RoomPlanCaptureViewRepresentable: UIViewRepresentable {
    let session: RoomCaptureSession

    func makeUIView(context: Context) -> RoomCaptureView {
        let view = RoomCaptureView(frame: .zero)
        view.captureSession = session
        return view
    }

    func updateUIView(_ uiView: RoomCaptureView, context: Context) {
        // RoomPlan owns all live scanning UX/guidance; there is nothing for
        // SwiftUI state changes to push into the view beyond initial setup.
    }
}
