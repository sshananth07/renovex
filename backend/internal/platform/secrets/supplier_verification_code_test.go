package secrets_test

import (
	"encoding/base64"
	"regexp"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

func encodedSupplierAccessKey(fill byte) string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill + byte(i)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func verificationContext() secrets.SupplierVerificationCodeContext {
	return secrets.SupplierVerificationCodeContext{
		ChallengeID:              "challenge-1",
		CompanyID:                "company-1",
		SupplierID:               "supplier-1",
		InvitationID:             "invitation-1",
		AccessGeneration:         3,
		NormalizedRecipientEmail: "recipient@supplier.test",
	}
}

func TestSupplierVerificationCodesAreDeterministicSixASCIIDigits(t *testing.T) {
	keyring, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(1)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}

	first, err := keyring.DeriveCode(1, verificationContext())
	if err != nil {
		t.Fatalf("deriving first code: %v", err)
	}
	second, err := keyring.DeriveCode(1, verificationContext())
	if err != nil {
		t.Fatalf("deriving second code: %v", err)
	}

	if first != second {
		t.Fatalf("same challenge derived %q then %q", first, second)
	}
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(first) {
		t.Fatalf("code %q is not exactly six ASCII digits", first)
	}
}

func TestSupplierVerificationCodeIdentityChangesItsDerivation(t *testing.T) {
	keyring, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(2)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}

	original, err := keyring.DeriveCode(1, verificationContext())
	if err != nil {
		t.Fatalf("deriving original code: %v", err)
	}
	changed := verificationContext()
	changed.InvitationID = "invitation-2"
	different, err := keyring.DeriveCode(1, changed)
	if err != nil {
		t.Fatalf("deriving changed code: %v", err)
	}

	if original == different {
		t.Fatalf("different challenge identities unexpectedly derived the same test code %q", original)
	}
}

func TestVerificationCodeValidationRejectsMalformedAndUnicodeDigits(t *testing.T) {
	for _, candidate := range []string{
		"", "12345", "1234567", "123 45", " 123456", "123456 ",
		"１２３４５６", "١٢٣٤٥٦", "12345a",
	} {
		if secrets.IsSixDigitVerificationCode(candidate) {
			t.Errorf("accepted malformed verification code %q", candidate)
		}
	}
	for _, candidate := range []string{"000000", "004271", "918305"} {
		if !secrets.IsSixDigitVerificationCode(candidate) {
			t.Errorf("rejected valid verification code %q", candidate)
		}
	}
}

func TestVerificationCodeVerifierIsKeyedAndDetectsPersistedTampering(t *testing.T) {
	keyring, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedSupplierAccessKey(3)})
	if err != nil {
		t.Fatalf("constructing keyring: %v", err)
	}
	context := verificationContext()
	code, err := keyring.DeriveCode(1, context)
	if err != nil {
		t.Fatalf("deriving code: %v", err)
	}
	verifier, err := keyring.DeriveCodeVerifier(1, context.ChallengeID, code)
	if err != nil {
		t.Fatalf("deriving keyed verifier: %v", err)
	}

	valid, err := keyring.VerifyCode(1, context, code, verifier)
	if err != nil || !valid {
		t.Fatalf("valid code/verifier = %v/%v, want true/nil", valid, err)
	}
	valid, err = keyring.VerifyCode(1, context, "999999", verifier)
	if err != nil {
		t.Fatalf("checking incorrect code: %v", err)
	}
	if valid {
		t.Fatal("an incorrect code verified")
	}
	valid, err = keyring.VerifyCode(1, context, code, "tampered-verifier")
	if err != nil {
		t.Fatalf("checking tampered verifier: %v", err)
	}
	if valid {
		t.Fatal("a tampered persisted verifier verified")
	}
}

func TestSupplierVerificationCodeKeyringRejectsBadConfiguration(t *testing.T) {
	tests := map[string]struct {
		active int
		keys   map[int]string
	}{
		"empty":            {1, nil},
		"missing active":   {2, map[int]string{1: encodedSupplierAccessKey(1)}},
		"malformed base64": {1, map[int]string{1: "not-base64"}},
		"undersized decoded": {1, map[int]string{
			1: base64.StdEncoding.EncodeToString(make([]byte, 16)),
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := secrets.NewSupplierVerificationCodeKeyring(
				test.active, test.keys); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}
