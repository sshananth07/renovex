package secrets_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

// M8 design spec §6.1A: the invitation secret is DERIVED from a versioned
// server-side keyring, never randomly generated and never persisted. These
// tests pin the contract that makes copy-link (return the current link without
// rotating) possible at all.

// testKey returns a deterministic, correctly sized key for version v. Tests
// inject fixed keys (§6.1A configuration rules).
func testKey(v int) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(v*31 + i)
	}
	return key
}

func encodedTestKey(v int) string {
	return base64.StdEncoding.EncodeToString(testKey(v))
}

func newTestKeyring(t *testing.T) *secrets.InvitationKeyring {
	t.Helper()
	kr, err := secrets.NewInvitationKeyring(1, map[int]string{1: encodedTestKey(1)})
	if err != nil {
		t.Fatalf("failed to construct keyring: %v", err)
	}
	return kr
}

// --- construction and validation ---

// A key shorter than 32 decoded bytes must be rejected at construction, which
// is what makes it fail application startup rather than silently weakening
// every link (§6.1A).
func TestNewInvitationKeyringRejectsUndersizedKey(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))

	_, err := secrets.NewInvitationKeyring(1, map[int]string{1: short})

	if err == nil {
		t.Fatal("expected an undersized key to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "32") {
		t.Errorf("error should state the 32-byte minimum, got %q", err)
	}
}

func TestNewInvitationKeyringRejectsMalformedBase64(t *testing.T) {
	_, err := secrets.NewInvitationKeyring(1, map[int]string{1: "not!valid!base64!"})

	if err == nil {
		t.Fatal("expected malformed base64 to be rejected, got nil error")
	}
}

// The active version must be present in the keyring, otherwise the application
// could start but be unable to derive any new secret.
func TestNewInvitationKeyringRejectsMissingActiveVersion(t *testing.T) {
	_, err := secrets.NewInvitationKeyring(2, map[int]string{1: encodedTestKey(1)})

	if err == nil {
		t.Fatal("expected a missing active version to be rejected, got nil error")
	}
}

func TestNewInvitationKeyringRejectsEmptyKeyring(t *testing.T) {
	_, err := secrets.NewInvitationKeyring(1, map[int]string{})

	if err == nil {
		t.Fatal("expected an empty keyring to be rejected, got nil error")
	}
}

// ActiveVersion is what creation and rotation stamp onto the invitation.
func TestActiveVersionReportsTheConfiguredVersion(t *testing.T) {
	kr, err := secrets.NewInvitationKeyring(2, map[int]string{
		1: encodedTestKey(1),
		2: encodedTestKey(2),
	})
	if err != nil {
		t.Fatalf("failed to construct keyring: %v", err)
	}

	if got := kr.ActiveVersion(); got != 2 {
		t.Errorf("ActiveVersion() = %d, want 2", got)
	}
}

// --- derivation ---

// Determinism is the entire point: copy-link re-derives the same token from the
// persisted generation and key version, without storing the raw token (§6.1A).
func TestDeriveIsDeterministicForTheSameInputs(t *testing.T) {
	kr := newTestKeyring(t)

	first, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 3)
	if err != nil {
		t.Fatalf("first derive failed: %v", err)
	}
	second, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 3)
	if err != nil {
		t.Fatalf("second derive failed: %v", err)
	}

	if first != second {
		t.Errorf("derivation is not deterministic: %q != %q", first, second)
	}
	if first == "" {
		t.Error("derived secret must not be empty")
	}
}

// Every field must actually participate. A field that does not change the
// output would let two different invitations share a link.
func TestDeriveVariesWithEveryCanonicalInputField(t *testing.T) {
	kr, err := secrets.NewInvitationKeyring(2, map[int]string{
		1: encodedTestKey(1),
		2: encodedTestKey(2),
	})
	if err != nil {
		t.Fatalf("failed to construct keyring: %v", err)
	}

	base, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 1)
	if err != nil {
		t.Fatalf("base derive failed: %v", err)
	}

	cases := []struct {
		name             string
		version          int
		companyID        string
		invitationID     string
		accessGeneration int64
	}{
		{"key version", 2, "company-1", "invitation-1", 1},
		{"company", 1, "company-2", "invitation-1", 1},
		{"invitation", 1, "company-1", "invitation-2", 1},
		{"access generation", 1, "company-1", "invitation-1", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := kr.DeriveInvitationSecret(tc.version, tc.companyID,
				tc.invitationID, tc.accessGeneration)
			if err != nil {
				t.Fatalf("derive failed: %v", err)
			}
			if got == base {
				t.Errorf("changing %s did not change the derived secret; that field "+
					"is not participating in the canonical input (§6.1A)", tc.name)
			}
		})
	}
}

// Length-prefixed encoding, not delimiter concatenation (§6.1A). Without it,
// ("ab", "c") and ("a", "bc") would collide — two different invitations
// resolving to one link.
func TestDeriveUsesLengthPrefixedInputNotConcatenation(t *testing.T) {
	kr := newTestKeyring(t)

	first, err := kr.DeriveInvitationSecret(1, "ab", "c", 1)
	if err != nil {
		t.Fatalf("first derive failed: %v", err)
	}
	second, err := kr.DeriveInvitationSecret(1, "a", "bc", 1)
	if err != nil {
		t.Fatalf("second derive failed: %v", err)
	}

	if first == second {
		t.Error("(\"ab\",\"c\") and (\"a\",\"bc\") derived the same secret: the canonical " +
			"input is concatenated rather than length-prefixed (§6.1A)")
	}
}

// An invitation stamped with an old version must stay derivable while that key
// remains configured — that is what makes key rotation non-breaking.
func TestDeriveResolvesAnOlderKeyVersionAfterTheActiveVersionAdvances(t *testing.T) {
	kr, err := secrets.NewInvitationKeyring(2, map[int]string{
		1: encodedTestKey(1),
		2: encodedTestKey(2),
	})
	if err != nil {
		t.Fatalf("failed to construct keyring: %v", err)
	}

	got, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 1)
	if err != nil {
		t.Fatalf("deriving under the older key version failed: %v", err)
	}
	if got == "" {
		t.Error("derived secret must not be empty")
	}
}

// A version whose key is no longer configured must fail closed, never fall back
// to the active key — a silent fallback would mint a link that fails
// verification against the stored hash.
func TestDeriveFailsClosedForAnUnconfiguredKeyVersion(t *testing.T) {
	kr := newTestKeyring(t)

	_, err := kr.DeriveInvitationSecret(9, "company-1", "invitation-1", 1)

	if err == nil {
		t.Fatal("expected an unconfigured key version to fail, got nil error")
	}
}

// The token travels in a URL, so it must be URL-safe and unpadded.
func TestDerivedSecretIsURLSafeBase64(t *testing.T) {
	kr := newTestKeyring(t)

	got, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 1)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}

	if strings.ContainsAny(got, "+/=") {
		t.Errorf("derived secret %q contains characters outside RawURLEncoding", got)
	}
	if _, err := base64.RawURLEncoding.DecodeString(got); err != nil {
		t.Errorf("derived secret is not valid RawURLEncoding: %v", err)
	}
}

// --- hashing and verification ---

// The persisted value is a one-way SHA-256 hash of the ENCODED token,
// consistent with the existing access.HashAccessToken convention (§6.1A).
func TestHashInvitationSecretIsDeterministicAndNotThePlaintext(t *testing.T) {
	const raw = "some-derived-token"

	first := secrets.HashInvitationSecret(raw)
	second := secrets.HashInvitationSecret(raw)

	if first != second {
		t.Errorf("hashing is not deterministic: %q != %q", first, second)
	}
	if first == raw {
		t.Error("hash must not equal the plaintext token")
	}
	if strings.Contains(first, raw) {
		t.Error("hash must not contain the plaintext token")
	}
	if len(first) != 64 {
		t.Errorf("expected a 64-character hex SHA-256 digest, got %d characters", len(first))
	}
}

func TestHashInvitationSecretDistinguishesDifferentTokens(t *testing.T) {
	if secrets.HashInvitationSecret("token-a") == secrets.HashInvitationSecret("token-b") {
		t.Error("different tokens produced the same hash")
	}
}

// Verification is constant-time (§6.1A) and is the check copy-link uses to fail
// closed before returning a link.
func TestVerifyInvitationSecretAcceptsTheMatchingToken(t *testing.T) {
	kr := newTestKeyring(t)

	raw, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 1)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	hash := secrets.HashInvitationSecret(raw)

	if !secrets.VerifyInvitationSecret(raw, hash) {
		t.Error("a freshly derived token failed verification against its own hash")
	}
}

func TestVerifyInvitationSecretRejectsANonMatchingToken(t *testing.T) {
	kr := newTestKeyring(t)

	raw, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 1)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	other, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 2)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}

	if secrets.VerifyInvitationSecret(other, secrets.HashInvitationSecret(raw)) {
		t.Error("a token from a different access generation passed verification")
	}
}

func TestVerifyInvitationSecretRejectsEmptyInput(t *testing.T) {
	if secrets.VerifyInvitationSecret("", secrets.HashInvitationSecret("")) {
		t.Error("an empty token must never verify, even against the hash of an empty string")
	}
	if secrets.VerifyInvitationSecret("token", "") {
		t.Error("an empty stored hash must never verify")
	}
}

// Rotation is generation-driven: a new generation under the same key yields a
// different token, which is what invalidates prior links (§6.1A).
func TestRotationByAccessGenerationInvalidatesThePreviousLink(t *testing.T) {
	kr := newTestKeyring(t)

	before, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 1)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	storedHash := secrets.HashInvitationSecret(before)

	after, err := kr.DeriveInvitationSecret(1, "company-1", "invitation-1", 2)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}

	if secrets.VerifyInvitationSecret(before, secrets.HashInvitationSecret(after)) {
		t.Error("the pre-rotation token still verifies against the post-rotation hash")
	}
	if secrets.VerifyInvitationSecret(after, storedHash) {
		t.Error("the post-rotation token verifies against the pre-rotation hash")
	}
}
