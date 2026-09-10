package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const (
	supplierSessionTokenDomain = "supplier-session-token-v1"
	supplierSessionCSRDomain   = "supplier-session-csrf-v1"
)

// SupplierSessionTokenContext is the immutable identity and current credential
// generation of one browser session. Session and CSRF credentials are derived
// from the same context but separate HMAC domains.
type SupplierSessionTokenContext struct {
	SessionID                string
	TokenGeneration          int64
	CompanyID                string
	SupplierID               string
	NormalizedRecipientEmail string
}

// SupplierSessionTokenKeyring owns only Supplier session credentials. It does
// not share keys with invitation links or six-digit verification codes.
type SupplierSessionTokenKeyring struct {
	activeVersion int
	keys          map[int][]byte
}

// NewSupplierSessionTokenKeyring validates the configured versioned keys at
// startup so active sessions remain reproducible across process restarts.
func NewSupplierSessionTokenKeyring(activeVersion int,
	encodedKeys map[int]string) (*SupplierSessionTokenKeyring, error) {

	keys, err := decodeVersionedKeyring(
		"supplier session token", activeVersion, encodedKeys)
	if err != nil {
		return nil, err
	}
	return &SupplierSessionTokenKeyring{
		activeVersion: activeVersion,
		keys:          keys,
	}, nil
}

// ActiveVersion is stamped on initial verification and every successful
// re-verification. Ordinary sliding renewal does not change it.
func (k *SupplierSessionTokenKeyring) ActiveVersion() int {
	return k.activeVersion
}

// DeriveSessionToken reproduces the raw HttpOnly cookie credential for exactly
// one session generation. Only its SHA-256 hash may be persisted.
func (k *SupplierSessionTokenKeyring) DeriveSessionToken(keyVersion int,
	context SupplierSessionTokenContext) (string, error) {

	return k.derive(keyVersion, supplierSessionTokenDomain, context)
}

// DeriveCSRFToken derives the readable double-submit credential. A distinct
// domain prevents the CSRF cookie from being accepted as the session cookie
// even though both use the session keyring.
func (k *SupplierSessionTokenKeyring) DeriveCSRFToken(keyVersion int,
	context SupplierSessionTokenContext) (string, error) {

	return k.derive(keyVersion, supplierSessionCSRDomain, context)
}

func (k *SupplierSessionTokenKeyring) derive(keyVersion int, domain string,
	context SupplierSessionTokenContext) (string, error) {

	key, ok := k.keys[keyVersion]
	if !ok {
		return "", fmt.Errorf(
			"secrets: supplier session token key version %d is not configured", keyVersion)
	}
	if err := validateSupplierSessionTokenContext(context); err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, key)
	writeLengthPrefixed(mac, []byte(domain))
	writeLengthPrefixedUint64(mac, uint64(keyVersion))
	writeLengthPrefixed(mac, []byte(context.SessionID))
	writeLengthPrefixedUint64(mac, uint64(context.TokenGeneration))
	writeLengthPrefixed(mac, []byte(context.CompanyID))
	writeLengthPrefixed(mac, []byte(context.SupplierID))
	writeLengthPrefixed(mac, []byte(context.NormalizedRecipientEmail))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func validateSupplierSessionTokenContext(context SupplierSessionTokenContext) error {
	if context.SessionID == "" || context.TokenGeneration < 1 ||
		context.CompanyID == "" || context.SupplierID == "" ||
		context.NormalizedRecipientEmail == "" {
		return fmt.Errorf("secrets: complete supplier session token identity is required")
	}
	return nil
}

// HashSupplierSessionToken returns the canonical hex SHA-256 representation
// stored in MongoDB and indexed for token-only session lookup.
func HashSupplierSessionToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// VerifySupplierSessionToken compares the presented token to the stored hash in
// constant time. Empty values never authenticate.
func VerifySupplierSessionToken(rawToken, storedHash string) bool {
	if rawToken == "" || storedHash == "" {
		return false
	}
	computed := HashSupplierSessionToken(rawToken)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}
