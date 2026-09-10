package spatial

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// The four RP4E2 request fingerprints below all follow
// turnRequestFingerprint's exact convention (design_service.go): a
// versioned, pipe-delimited, server-computed digest of only the fields
// that make one request the SAME logical request as an earlier one —
// never trusted from the client, always recomputed and compared against
// the persisted value on a client-request-id replay.

// confirmRequestFingerprint covers Confirm's idempotency surface: the
// session/turn identity, exact plan fingerprint, and exact RoomDraft
// revision the caller believed was current.
func confirmRequestFingerprint(sessionID, turnID, planFingerprint string, expectedRoomDraftRevision int64) string {
	input := fmt.Sprintf("confirm|1|%s|%s|%s|%d", sessionID, turnID, planFingerprint, expectedRoomDraftRevision)
	return hashHex(input)
}

// regenerateRequestFingerprint mirrors confirmRequestFingerprint's fields —
// Regenerate requires the exact same latest turn/fingerprint as Confirm,
// so the two share the same identity surface even though they map to
// different attempt-lifecycle actions.
func regenerateRequestFingerprint(sessionID, turnID, planFingerprint string, expectedRoomDraftRevision int64) string {
	input := fmt.Sprintf("regenerate|1|%s|%s|%s|%d", sessionID, turnID, planFingerprint, expectedRoomDraftRevision)
	return hashHex(input)
}

// cancelRequestFingerprint covers Cancel's idempotency surface: which
// attempt is being cancelled. Cancel carries no plan/revision fields — it
// terminates an attempt unconditionally, so nothing else about session
// state is part of "the same cancel request."
func cancelRequestFingerprint(sessionID, attemptID string) string {
	input := fmt.Sprintf("cancel|1|%s|%s", sessionID, attemptID)
	return hashHex(input)
}

// useRequestFingerprint covers Use's idempotency surface: exactly which
// attempt/plan/revision the caller believed was current and ready to
// accept.
func useRequestFingerprint(sessionID, attemptID, planFingerprint string, expectedRoomDraftRevision int64) string {
	input := fmt.Sprintf("use|1|%s|%s|%s|%d", sessionID, attemptID, planFingerprint, expectedRoomDraftRevision)
	return hashHex(input)
}

func hashHex(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
