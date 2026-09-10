package secrets

import (
	"encoding/base64"
	"testing"
	"time"
)

func testAssetGenerationSourceCapabilityKeyring(t *testing.T) *AssetGenerationSourceCapabilityKeyring {
	t.Helper()
	k, err := NewAssetGenerationSourceCapabilityKeyring(1, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=", // 32+ decoded bytes
	})
	if err != nil {
		t.Fatalf("unexpected error constructing keyring: %v", err)
	}
	return k
}

func TestNewAssetGenerationSourceCapabilityKeyring_RejectsNoKeys(t *testing.T) {
	_, err := NewAssetGenerationSourceCapabilityKeyring(1, map[int]string{})
	if err == nil {
		t.Fatalf("expected an error when no keys are configured")
	}
}

func TestNewAssetGenerationSourceCapabilityKeyring_RejectsUndersizedKey(t *testing.T) {
	_, err := NewAssetGenerationSourceCapabilityKeyring(1, map[int]string{1: "dG9vc2hvcnQ="}) // "tooshort" - way under 32 bytes
	if err == nil {
		t.Fatalf("expected an error for an undersized key")
	}
}

func TestNewAssetGenerationSourceCapabilityKeyring_RejectsMalformedBase64(t *testing.T) {
	_, err := NewAssetGenerationSourceCapabilityKeyring(1, map[int]string{1: "not-valid-base64!!!"})
	if err == nil {
		t.Fatalf("expected an error for malformed base64")
	}
}

func TestNewAssetGenerationSourceCapabilityKeyring_RejectsUnconfiguredActiveVersion(t *testing.T) {
	_, err := NewAssetGenerationSourceCapabilityKeyring(2, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=",
	})
	if err == nil {
		t.Fatalf("expected an error when the active version has no configured key")
	}
}

func TestAssetGenerationSourceCapabilityKeyring_MintAndVerifyRoundTrip(t *testing.T) {
	k := testAssetGenerationSourceCapabilityKeyring(t)
	token, err := k.Mint("company_a", "artifact_1", time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	claims, err := k.Verify(token)
	if err != nil {
		t.Fatalf("unexpected error verifying: %v", err)
	}
	if claims.CompanyID != "company_a" || claims.ArtifactID != "artifact_1" {
		t.Fatalf("expected claims to round-trip exactly, got %+v", claims)
	}
}

func TestAssetGenerationSourceCapabilityKeyring_Verify_RejectsExpiredToken(t *testing.T) {
	k := testAssetGenerationSourceCapabilityKeyring(t)
	token, err := k.Mint("company_a", "artifact_1", time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	if _, err := k.Verify(token); err == nil {
		t.Fatalf("expected an error for an expired token")
	}
}

func TestAssetGenerationSourceCapabilityKeyring_Verify_RejectsTamperedToken(t *testing.T) {
	k := testAssetGenerationSourceCapabilityKeyring(t)
	token, err := k.Mint("company_a", "artifact_1", time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	tampered := token[:len(token)-4] + "AAAA" // corrupt the trailing signature bytes
	if _, err := k.Verify(tampered); err == nil {
		t.Fatalf("expected an error for a tampered token")
	}
}

func TestAssetGenerationSourceCapabilityKeyring_Verify_RejectsTamperedClaimsMidToken(t *testing.T) {
	// Proves the WHOLE claims payload is authenticated, not just the
	// trailing signature bytes.
	k := testAssetGenerationSourceCapabilityKeyring(t)
	token, err := k.Mint("company_a", "artifact_1", time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("unexpected error decoding token for tampering: %v", err)
	}
	decoded[10] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(decoded)

	if _, err := k.Verify(tampered); err == nil {
		t.Fatalf("expected an error for claims tampered mid-token")
	}
}

func TestAssetGenerationSourceCapabilityKeyring_Verify_RejectsGarbageInput(t *testing.T) {
	k := testAssetGenerationSourceCapabilityKeyring(t)
	if _, err := k.Verify("not-a-real-token"); err == nil {
		t.Fatalf("expected an error for garbage input")
	}
	if _, err := k.Verify(""); err == nil {
		t.Fatalf("expected an error for empty input")
	}
}

func TestAssetGenerationSourceCapabilityKeyring_Verify_RejectsWrongKeyVersion(t *testing.T) {
	k1, err := NewAssetGenerationSourceCapabilityKeyring(1, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	token, err := k1.Mint("company_a", "artifact_1", time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}

	k2, err := NewAssetGenerationSourceCapabilityKeyring(2, map[int]string{
		2: "OTg3NjU0MzIxMDk4NzY1NDMyMTA5ODc2NTQzMjEwOTg3Nj==",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := k2.Verify(token); err == nil {
		t.Fatalf("expected an error verifying a token signed with a key version no longer configured")
	}
}

func TestAssetGenerationSourceCapabilityKeyring_CannotBeVerifiedByVisualAssetCapabilityKeyring(t *testing.T) {
	// Domain-separation proof: a capability minted for source-image
	// access must never verify successfully against the DIFFERENT
	// VisualAssetCapabilityKeyring, even when constructed from the SAME
	// underlying key bytes — proving the two token types can never be
	// replayed against each other's verification route.
	sourceKeyring := testAssetGenerationSourceCapabilityKeyring(t)
	visualAssetKeyring, err := NewVisualAssetCapabilityKeyring(1, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	token, err := sourceKeyring.Mint("company_a", "artifact_1", time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	if _, err := visualAssetKeyring.Verify(token); err == nil {
		t.Fatalf("expected a source-image capability to be rejected by the unrelated VisualAssetCapabilityKeyring")
	}
}
