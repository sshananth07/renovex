package spatial

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// computeAssetGenerationFingerprint derives a deterministic fingerprint
// from AUTHORITATIVE, server-known values only — never from anything the
// client supplies directly, per the explicit "never trust a client-
// provided fingerprint" requirement. Used to detect whether a repeated
// clientRequestId is a genuine idempotent retry (same fingerprint) or a
// conflicting reuse (different fingerprint → 409).
func computeAssetGenerationFingerprint(companyID, sourceArtifactID, sourceArtifactChecksum string, seed int64) string {
	input := fmt.Sprintf("%s|%s|%s|%d", companyID, sourceArtifactID, sourceArtifactChecksum, seed)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
