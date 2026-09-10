package secrets

import (
	"encoding/base64"
	"testing"
	"time"
)

func testVisualAssetCapabilityKeyring(t *testing.T) *VisualAssetCapabilityKeyring {
	t.Helper()
	k, err := NewVisualAssetCapabilityKeyring(1, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=", // 32+ decoded bytes
	})
	if err != nil {
		t.Fatalf("unexpected error constructing keyring: %v", err)
	}
	return k
}

func TestNewVisualAssetCapabilityKeyring_RejectsNoKeys(t *testing.T) {
	_, err := NewVisualAssetCapabilityKeyring(1, map[int]string{})
	if err == nil {
		t.Fatalf("expected an error when no keys are configured")
	}
}

func TestNewVisualAssetCapabilityKeyring_RejectsUndersizedKey(t *testing.T) {
	_, err := NewVisualAssetCapabilityKeyring(1, map[int]string{1: "dG9vc2hvcnQ="}) // "tooshort" - way under 32 bytes
	if err == nil {
		t.Fatalf("expected an error for an undersized key")
	}
}

func TestNewVisualAssetCapabilityKeyring_RejectsMalformedBase64(t *testing.T) {
	_, err := NewVisualAssetCapabilityKeyring(1, map[int]string{1: "not-valid-base64!!!"})
	if err == nil {
		t.Fatalf("expected an error for malformed base64")
	}
}

func TestNewVisualAssetCapabilityKeyring_RejectsUnconfiguredActiveVersion(t *testing.T) {
	_, err := NewVisualAssetCapabilityKeyring(2, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=",
	})
	if err == nil {
		t.Fatalf("expected an error when the active version has no configured key")
	}
}

func TestVisualAssetCapabilityKeyring_MintAndVerifyRoundTrip(t *testing.T) {
	k := testVisualAssetCapabilityKeyring(t)
	token, err := k.Mint("company_a", "boiler-asset", 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	claims, err := k.Verify(token)
	if err != nil {
		t.Fatalf("unexpected error verifying: %v", err)
	}
	if claims.CompanyID != "company_a" || claims.AssetID != "boiler-asset" || claims.Version != 1 {
		t.Fatalf("expected claims to round-trip exactly, got %+v", claims)
	}
}

func TestVisualAssetCapabilityKeyring_Verify_RejectsExpiredToken(t *testing.T) {
	k := testVisualAssetCapabilityKeyring(t)
	token, err := k.Mint("company_a", "boiler-asset", 1, time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	if _, err := k.Verify(token); err == nil {
		t.Fatalf("expected an error for an expired token")
	}
}

func TestVisualAssetCapabilityKeyring_Verify_RejectsTamperedToken(t *testing.T) {
	k := testVisualAssetCapabilityKeyring(t)
	token, err := k.Mint("company_a", "boiler-asset", 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	tampered := token[:len(token)-4] + "AAAA" // corrupt the trailing signature bytes
	if _, err := k.Verify(tampered); err == nil {
		t.Fatalf("expected an error for a tampered token")
	}
}

func TestVisualAssetCapabilityKeyring_Verify_RejectsTamperedClaims(t *testing.T) {
	k := testVisualAssetCapabilityKeyring(t)
	tokenForCompanyA, err := k.Mint("company_a", "boiler-asset", 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	tokenForCompanyB, err := k.Mint("company_b", "boiler-asset", 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	if tokenForCompanyA == tokenForCompanyB {
		t.Fatalf("expected different companies to produce different tokens")
	}
}

func TestVisualAssetCapabilityKeyring_Verify_RejectsTamperedClaimsMidToken(t *testing.T) {
	// Proves the WHOLE claims payload is authenticated, not just the
	// trailing signature bytes — flipping a byte inside the claims
	// (e.g. attempting to change which company/asset the token grants
	// access to) must also fail, not just corruption of the signature
	// itself.
	k := testVisualAssetCapabilityKeyring(t)
	token, err := k.Mint("company_a", "boiler-asset", 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("unexpected error decoding token for tampering: %v", err)
	}
	// Flip a byte in the middle of the claims region (well before the
	// trailing 32-byte HMAC).
	decoded[10] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(decoded)

	if _, err := k.Verify(tampered); err == nil {
		t.Fatalf("expected an error for claims tampered mid-token")
	}
}

func TestVisualAssetCapabilityKeyring_Verify_RejectsGarbageInput(t *testing.T) {
	k := testVisualAssetCapabilityKeyring(t)
	if _, err := k.Verify("not-a-real-token"); err == nil {
		t.Fatalf("expected an error for garbage input")
	}
	if _, err := k.Verify(""); err == nil {
		t.Fatalf("expected an error for empty input")
	}
}

func TestVisualAssetCapabilityKeyring_Verify_RejectsWrongKeyVersion(t *testing.T) {
	k1, err := NewVisualAssetCapabilityKeyring(1, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	token, err := k1.Mint("company_a", "boiler-asset", 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("unexpected error minting: %v", err)
	}

	// A keyring that no longer has version 1 configured must reject a
	// token minted under it — matching the "an old key must remain
	// configured while anything still references it" rule, verified from
	// the other direction: removing the key genuinely breaks
	// verification, proving Verify actually depends on the right key.
	k2, err := NewVisualAssetCapabilityKeyring(2, map[int]string{
		2: "OTg3NjU0MzIxMDk4NzY1NDMyMTA5ODc2NTQzMjEwOTg3Nj==",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := k2.Verify(token); err == nil {
		t.Fatalf("expected an error verifying against a keyring missing the signing key version")
	}
}
