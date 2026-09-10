package access

import (
	"strings"
	"testing"
)

func TestGenerateAccessTokenIsRandomAndURLSafe(t *testing.T) {
	first, err := GenerateAccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := GenerateAccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first == second {
		t.Fatal("expected two generated tokens to differ")
	}
	// 32 bytes base64url (no padding) == 43 characters.
	if len(first) != 43 {
		t.Fatalf("expected 43-character token (32 bytes base64url), got %d: %q", len(first), first)
	}
	// URL-safe alphabet only — no '+', '/', or '=' padding.
	if strings.ContainsAny(first, "+/=") {
		t.Fatalf("expected URL-safe token without padding, got %q", first)
	}
}

func TestHashAccessTokenIsStableAndNotTheRawToken(t *testing.T) {
	raw := "some-raw-token-value"
	firstHash := HashAccessToken(raw)
	secondHash := HashAccessToken(raw)

	if firstHash != secondHash {
		t.Fatal("expected hashing to be deterministic")
	}
	if firstHash == raw {
		t.Fatal("expected the hash to differ from the raw token")
	}
	// SHA-256 hex == 64 characters.
	if len(firstHash) != 64 {
		t.Fatalf("expected 64-character hex hash, got %d", len(firstHash))
	}
	if HashAccessToken("different") == firstHash {
		t.Fatal("expected different inputs to hash differently")
	}
}
