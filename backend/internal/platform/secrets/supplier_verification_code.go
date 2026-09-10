package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

const (
	supplierVerificationCodeDomain     = "supplier-verification-code-v1"
	supplierVerificationVerifierDomain = "supplier-verification-verifier-v1"
	verificationCodeRange              = uint32(1_000_000)
	verificationCodeAcceptanceLimit    = uint32(4_294_000_000)
)

// SupplierVerificationCodeContext is the immutable identity captured on one
// challenge. Every field participates in derivation so a code cannot be moved
// to another tenant, Supplier, invitation, generation, recipient, or challenge.
type SupplierVerificationCodeContext struct {
	ChallengeID              string
	CompanyID                string
	SupplierID               string
	InvitationID             string
	AccessGeneration         int64
	NormalizedRecipientEmail string
}

// SupplierVerificationCodeKeyring owns the dedicated versioned keys used to
// recover short-lived email codes. It is deliberately separate from invitation
// and session credentials so compromise or rotation of one purpose does not
// silently affect another.
type SupplierVerificationCodeKeyring struct {
	activeVersion int
	keys          map[int][]byte
}

// NewSupplierVerificationCodeKeyring validates startup configuration. The
// service must never invent per-boot keys because an outstanding challenge must
// remain deliverable and verifiable after a restart.
func NewSupplierVerificationCodeKeyring(activeVersion int,
	encodedKeys map[int]string) (*SupplierVerificationCodeKeyring, error) {

	keys, err := decodeVersionedKeyring(
		"supplier verification code", activeVersion, encodedKeys)
	if err != nil {
		return nil, err
	}
	return &SupplierVerificationCodeKeyring{
		activeVersion: activeVersion,
		keys:          keys,
	}, nil
}

// ActiveVersion is recorded on every newly created challenge.
func (k *SupplierVerificationCodeKeyring) ActiveVersion() int {
	return k.activeVersion
}

// DeriveCode deterministically recovers exactly six ASCII digits. Rejection
// sampling avoids the bias introduced by taking an unrestricted uint32 modulo
// one million.
func (k *SupplierVerificationCodeKeyring) DeriveCode(keyVersion int,
	context SupplierVerificationCodeContext) (string, error) {

	key, err := k.key(keyVersion)
	if err != nil {
		return "", err
	}
	if err := validateVerificationCodeContext(context); err != nil {
		return "", err
	}

	return deriveSixDigitCode(func(counter uint64) [sha256.Size]byte {
		mac := hmac.New(sha256.New, key)
		writeLengthPrefixed(mac, []byte(supplierVerificationCodeDomain))
		writeLengthPrefixedUint64(mac, uint64(keyVersion))
		writeLengthPrefixed(mac, []byte(context.ChallengeID))
		writeLengthPrefixed(mac, []byte(context.CompanyID))
		writeLengthPrefixed(mac, []byte(context.SupplierID))
		writeLengthPrefixed(mac, []byte(context.InvitationID))
		writeLengthPrefixedUint64(mac, uint64(context.AccessGeneration))
		writeLengthPrefixed(mac, []byte(context.NormalizedRecipientEmail))
		// Counter is another canonical field. Appending decimal text would make
		// the HMAC input ambiguous and would violate Revision 9.
		writeLengthPrefixedUint64(mac, counter)
		var block [sha256.Size]byte
		copy(block[:], mac.Sum(nil))
		return block
	}), nil
}

// deriveSixDigitCode is split from HMAC construction so the otherwise very
// rare all-candidates-rejected branch can be tested deterministically.
func deriveSixDigitCode(blockForCounter func(counter uint64) [sha256.Size]byte) string {
	for counter := uint64(0); ; counter++ {
		block := blockForCounter(counter)
		for offset := 0; offset < len(block); offset += 4 {
			candidate := binary.BigEndian.Uint32(block[offset : offset+4])
			if candidate < verificationCodeAcceptanceLimit {
				return fmt.Sprintf("%06d", candidate%verificationCodeRange)
			}
		}
	}
}

// IsSixDigitVerificationCode enforces ASCII explicitly. Unicode-aware digit
// predicates are intentionally unsuitable for this credential format.
func IsSixDigitVerificationCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// DeriveCodeVerifier produces the only code representation permitted in
// persistence. The verifier is keyed, preventing a database-only attacker from
// enumerating the one-million-value code space offline.
func (k *SupplierVerificationCodeKeyring) DeriveCodeVerifier(keyVersion int,
	challengeID, code string) (string, error) {

	key, err := k.key(keyVersion)
	if err != nil {
		return "", err
	}
	if challengeID == "" {
		return "", fmt.Errorf("secrets: supplier verification challenge ID is required")
	}
	if !IsSixDigitVerificationCode(code) {
		return "", fmt.Errorf("secrets: supplier verification code must be exactly six ASCII digits")
	}

	mac := hmac.New(sha256.New, key)
	writeLengthPrefixed(mac, []byte(supplierVerificationVerifierDomain))
	writeLengthPrefixed(mac, []byte(challengeID))
	writeLengthPrefixed(mac, []byte(code))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyCode checks both the submitted code and the persisted verifier against
// the immutable challenge context. The second comparison makes verifier
// tampering fail closed even when the caller submits the genuinely derived
// code.
func (k *SupplierVerificationCodeKeyring) VerifyCode(keyVersion int,
	context SupplierVerificationCodeContext, submittedCode, storedVerifier string) (bool, error) {

	if !IsSixDigitVerificationCode(submittedCode) || storedVerifier == "" {
		return false, nil
	}
	expectedCode, err := k.DeriveCode(keyVersion, context)
	if err != nil {
		return false, err
	}
	expectedVerifier, err := k.DeriveCodeVerifier(
		keyVersion, context.ChallengeID, expectedCode)
	if err != nil {
		return false, err
	}
	submittedVerifier, err := k.DeriveCodeVerifier(
		keyVersion, context.ChallengeID, submittedCode)
	if err != nil {
		return false, err
	}

	persistedIsExpected := subtle.ConstantTimeCompare(
		[]byte(storedVerifier), []byte(expectedVerifier))
	submittedMatches := subtle.ConstantTimeCompare(
		[]byte(submittedVerifier), []byte(storedVerifier))
	return persistedIsExpected&submittedMatches == 1, nil
}

func (k *SupplierVerificationCodeKeyring) key(version int) ([]byte, error) {
	key, ok := k.keys[version]
	if !ok {
		return nil, fmt.Errorf(
			"secrets: supplier verification code key version %d is not configured", version)
	}
	return key, nil
}

func validateVerificationCodeContext(context SupplierVerificationCodeContext) error {
	if context.ChallengeID == "" || context.CompanyID == "" ||
		context.SupplierID == "" || context.InvitationID == "" ||
		context.NormalizedRecipientEmail == "" || context.AccessGeneration < 1 {
		return fmt.Errorf("secrets: complete supplier verification challenge identity is required")
	}
	return nil
}

// decodeVersionedKeyring centralizes the startup invariant shared by Phase D's
// independent code and session keyrings.
func decodeVersionedKeyring(purpose string, activeVersion int,
	encodedKeys map[int]string) (map[int][]byte, error) {

	if len(encodedKeys) == 0 {
		return nil, fmt.Errorf("secrets: at least one %s key must be configured", purpose)
	}
	keys := make(map[int][]byte, len(encodedKeys))
	for version, encoded := range encodedKeys {
		if version < 1 {
			return nil, fmt.Errorf("secrets: %s key version must be >= 1, got %d",
				purpose, version)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secrets: %s key v%d is not valid base64: %w",
				purpose, version, err)
		}
		if len(key) < MinInvitationKeyBytes {
			return nil, fmt.Errorf("secrets: %s key v%d decodes to %d bytes; at least %d "+
				"random bytes are required", purpose, version, len(key), MinInvitationKeyBytes)
		}
		keys[version] = key
	}
	if _, ok := keys[activeVersion]; !ok {
		return nil, fmt.Errorf("secrets: active %s key version %d is not configured",
			purpose, activeVersion)
	}
	return keys, nil
}
