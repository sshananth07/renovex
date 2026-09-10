import Foundation
import Crypto

/// The canonical SHA-256 hex checksum contract (plan §RP3.5/§RP4B0) —
/// isolated behind this one tiny utility rather than exposing `Crypto`
/// throughout the domain layer. Must match
/// `backend/internal/platform/composition/spatialartifactstoreadapter.go`'s
/// server-side checksum exactly (lowercase hex SHA-256) since
/// `RequestArtifactUpload`/`FinalizeArtifactUpload` verify the client's
/// declared checksum against it byte-for-byte.
public enum ChecksumUtility {
    /// Returns the lowercase hex SHA-256 digest of `data` — hash the EXACT
    /// bytes that will be persisted/uploaded, never a decoded/re-encoded
    /// value (JSON key ordering or formatting could otherwise produce a
    /// different checksum despite representing the same logical object).
    public static func sha256Hex(_ data: Data) -> String {
        SHA256.hash(data: data)
            .map { String(format: "%02x", $0) }
            .joined()
    }
}
