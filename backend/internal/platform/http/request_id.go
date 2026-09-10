package http

import (
	"crypto/rand"
	"encoding/hex"
)

// maxRequestIDBytes bounds an accepted caller-supplied request ID. Mirrors
// procurementlimits.MaxIDBytes in spirit (printable-ASCII, bounded length)
// without importing that package — request IDs are a transport-layer
// concern, not a procurement domain concept.
const maxRequestIDBytes = 128

// IsValidRequestID reports whether id is 1-128 bytes of printable ASCII
// (0x21-0x7e — no control characters, no whitespace, no non-ASCII bytes).
// This matches the visible-ASCII range used elsewhere in the codebase for
// bounded identifiers (see procurementlimits.ValidateID).
func IsValidRequestID(id string) bool {
	if len(id) < 1 || len(id) > maxRequestIDBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

// ResolveRequestID returns supplied unchanged if it is a valid request ID,
// otherwise generates and returns a fresh one. An invalid or oversized
// caller-supplied value is never echoed back — the client cannot inject
// arbitrary content into the response header or into structured request
// logs.
func ResolveRequestID(supplied string) string {
	if IsValidRequestID(supplied) {
		return supplied
	}
	return generateRequestID()
}

func generateRequestID() string {
	buf := make([]byte, 16)
	// crypto/rand.Read on a 16-byte buffer does not fail in practice on any
	// supported platform; a zero-value fallback would still be a valid (if
	// predictable) request ID rather than a panic.
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
