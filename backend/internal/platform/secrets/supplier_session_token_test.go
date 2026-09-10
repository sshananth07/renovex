package secrets_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

func sessionTokenContext() secrets.SupplierSessionTokenContext {
	return secrets.SupplierSessionTokenContext{
		SessionID:                "session-1",
		TokenGeneration:          4,
		CompanyID:                "company-1",
		SupplierID:               "supplier-1",
		NormalizedRecipientEmail: "recipient@supplier.test",
	}
}

func TestSupplierSessionAndCSRFTokensAreDeterministicAndDomainSeparated(t *testing.T) {
	keyring, err := secrets.NewSupplierSessionTokenKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(11)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}
	context := sessionTokenContext()

	firstSession, err := keyring.DeriveSessionToken(1, context)
	if err != nil {
		t.Fatalf("deriving session token: %v", err)
	}
	secondSession, err := keyring.DeriveSessionToken(1, context)
	if err != nil {
		t.Fatalf("rederiving session token: %v", err)
	}
	csrf, err := keyring.DeriveCSRFToken(1, context)
	if err != nil {
		t.Fatalf("deriving CSRF token: %v", err)
	}

	if firstSession != secondSession {
		t.Fatalf("same session derived %q then %q", firstSession, secondSession)
	}
	if firstSession == csrf {
		t.Fatal("session and CSRF tokens must be separated by their HMAC domains")
	}
	for name, token := range map[string]string{"session": firstSession, "csrf": csrf} {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("%s token is not unpadded base64url: %v", name, err)
		}
		if len(decoded) != 32 {
			t.Fatalf("%s token decoded length = %d, want one SHA-256 HMAC block", name, len(decoded))
		}
	}
}

func TestSupplierSessionGenerationRotationInvalidatesOldTokenAndCSRF(t *testing.T) {
	keyring, err := secrets.NewSupplierSessionTokenKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(12)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}
	oldContext := sessionTokenContext()
	oldToken, _ := keyring.DeriveSessionToken(1, oldContext)
	oldCSRF, _ := keyring.DeriveCSRFToken(1, oldContext)
	oldHash := secrets.HashSupplierSessionToken(oldToken)

	newContext := oldContext
	newContext.TokenGeneration++
	newToken, _ := keyring.DeriveSessionToken(1, newContext)
	newCSRF, _ := keyring.DeriveCSRFToken(1, newContext)

	if newToken == oldToken || newCSRF == oldCSRF {
		t.Fatal("advancing TokenGeneration must rotate both browser credentials")
	}
	if secrets.VerifySupplierSessionToken(newToken, oldHash) {
		t.Fatal("the new token unexpectedly verified against the old generation hash")
	}
	if !secrets.VerifySupplierSessionToken(oldToken, oldHash) {
		t.Fatal("the original token did not verify against its stored hash")
	}
}

func TestSupplierSessionCanonicalEncodingSeparatesAmbiguousFieldPairs(t *testing.T) {
	keyring, err := secrets.NewSupplierSessionTokenKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(13)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}
	first := sessionTokenContext()
	first.CompanyID, first.SupplierID = "ab", "c"
	second := sessionTokenContext()
	second.CompanyID, second.SupplierID = "a", "bc"

	firstToken, _ := keyring.DeriveSessionToken(1, first)
	secondToken, _ := keyring.DeriveSessionToken(1, second)
	if firstToken == secondToken {
		t.Fatal("length-prefixing failed to separate (ab,c) from (a,bc)")
	}
}

func TestSupplierSessionKeyringRetainsOldVersionsAndFailsClosedWhenMissing(t *testing.T) {
	keyring, err := secrets.NewSupplierSessionTokenKeyring(2, map[int]string{
		1: encodedSupplierAccessKey(14),
		2: encodedSupplierAccessKey(15),
	})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}
	if keyring.ActiveVersion() != 2 {
		t.Fatalf("active version = %d, want 2", keyring.ActiveVersion())
	}
	if _, err := keyring.DeriveSessionToken(1, sessionTokenContext()); err != nil {
		t.Fatalf("old referenced key version must remain usable: %v", err)
	}
	if _, err := keyring.DeriveSessionToken(3, sessionTokenContext()); err == nil {
		t.Fatal("unconfigured key version did not fail closed")
	}
}

func TestSupplierSessionKeyringRejectsIncompleteIdentityAndBadConfiguration(t *testing.T) {
	keyring, err := secrets.NewSupplierSessionTokenKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(16)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}
	incomplete := sessionTokenContext()
	incomplete.NormalizedRecipientEmail = ""
	if _, err := keyring.DeriveSessionToken(1, incomplete); err == nil {
		t.Fatal("incomplete immutable session identity was accepted")
	}

	if _, err := secrets.NewSupplierSessionTokenKeyring(1, nil); err == nil {
		t.Fatal("empty session keyring was accepted")
	}
	if _, err := secrets.NewSupplierSessionTokenKeyring(1,
		map[int]string{1: strings.Repeat("!", 44)}); err == nil {
		t.Fatal("malformed session key was accepted")
	}
}
