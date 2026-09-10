import Foundation

/// A reference to raw capture-provider source bytes already durably
/// written to a managed Application-Support-relative location (plan
/// §RP3.5/§RP4B0). Apple-framework-free by design — `RenovexCaptureRoomPlan`
/// (Apple-only) owns materializing the raw `CapturedRoomData` to disk and
/// computing its checksum; this struct is how that reference crosses into
/// `RenovexCaptureCore`'s `CaptureCompletionCoordinator`, which owns
/// associating it with the authoritative `LocalCaptureRun`/`RoomDraft`
/// identities and durably recording the sync work.
///
/// Deliberately carries no capture ID or artifact kind of its own — the
/// coordinator attaches those, since this reference only describes "here
/// are some bytes I already saved," not what they mean in the sync system.
/// `relativePath` must be relative to a managed Application Support
/// subdirectory, never a temporary or absolute capture URL — RoomPlan's
/// own temporary output location is not guaranteed to survive process
/// restart, so a caller passing a temp-directory path here would silently
/// break resume-after-relaunch.
public struct CapturedSourceArtifactReference: Equatable, Sendable {
    public let relativePath: String
    public let contentType: String
    public let declaredSize: Int64
    public let checksum: String

    public init(relativePath: String, contentType: String, declaredSize: Int64, checksum: String) {
        self.relativePath = relativePath
        self.contentType = contentType
        self.declaredSize = declaredSize
        self.checksum = checksum
    }
}
