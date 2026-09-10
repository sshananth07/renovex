// Package secrets owns the versioned server-side keyring that DERIVES Supplier
// Invitation secrets, plus the one-way hashing and constant-time verification
// applied to them (M8 design spec §6.1A).
//
// Why derivation rather than random generation: a randomly generated secret
// exists only in memory at generation time, so a later copy-link action could
// only ROTATE the link, never reproduce it. The approved copy-link behaviour
// returns the current generation's link WITHOUT rotating, which requires the
// secret to be reproducible from persisted, non-secret fields.
//
// What is persisted on an invitation is only:
//
//	AccessSecretHash + AccessGeneration + SecretKeyVersion
//
// The raw derived token is never stored, never logged and never audited. The
// key material lives in configuration, outside MongoDB.
//
// Module boundary (ADR 0002): this is platform infrastructure with zero domain
// knowledge. It knows nothing about invitations beyond the three opaque
// identifier fields it mixes into the canonical input, so both rfqissuance
// (derive, rotate) and supplieraccess (verify) may depend on it without either
// depending on the other.
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

// invitationSecretDomain separates this HMAC's inputs from every other use of
// the same key material. Without domain separation, a future second derivation
// using the same keyring could be made to collide with an invitation secret.
const invitationSecretDomain = "m8-invitation-secret-v1"

// MinInvitationKeyBytes is the minimum DECODED key length. A shorter key is
// rejected at construction so it fails application startup rather than silently
// weakening every link the deployment issues.
const MinInvitationKeyBytes = 32

// InvitationKeyring holds the configured invitation-secret keys by version.
//
// Multiple versions coexist deliberately: an invitation records the version
// that derived its secret, so its link stays reproducible after the active
// version advances. An old key must remain configured while any invitation
// still references it.
type InvitationKeyring struct {
	activeVersion int
	keys          map[int][]byte
}

// NewInvitationKeyring decodes and validates a base64-encoded keyring.
//
// Every failure here is a startup failure by design (§6.1A): a malformed,
// undersized or missing active key must never be papered over, because a
// deployment that silently generated its own key would make every previously
// issued link unreproducible.
func NewInvitationKeyring(activeVersion int, encodedKeys map[int]string) (*InvitationKeyring, error) {
	if len(encodedKeys) == 0 {
		return nil, fmt.Errorf("secrets: at least one invitation secret key must be configured")
	}

	keys := make(map[int][]byte, len(encodedKeys))
	for version, encoded := range encodedKeys {
		if version < 1 {
			return nil, fmt.Errorf("secrets: invitation secret key version must be >= 1, got %d", version)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secrets: invitation secret key v%d is not valid base64: %w", version, err)
		}
		if len(key) < MinInvitationKeyBytes {
			return nil, fmt.Errorf("secrets: invitation secret key v%d decodes to %d bytes; "+
				"at least %d random bytes are required", version, len(key), MinInvitationKeyBytes)
		}
		keys[version] = key
	}

	if _, ok := keys[activeVersion]; !ok {
		return nil, fmt.Errorf("secrets: active invitation secret key version %d is not configured",
			activeVersion)
	}

	return &InvitationKeyring{activeVersion: activeVersion, keys: keys}, nil
}

// ActiveVersion is the version stamped onto newly created and newly rotated
// invitations.
func (k *InvitationKeyring) ActiveVersion() int { return k.activeVersion }

// DeriveInvitationSecret reproduces the raw invitation token for exactly one
// (keyVersion, company, invitation, accessGeneration) tuple.
//
// It is deterministic, which is what lets copy-link return the current link
// without rotating, and what makes rotation a matter of incrementing
// accessGeneration rather than storing anything new.
//
// An unconfigured key version fails closed. Falling back to the active key
// would mint a token that cannot verify against the stored hash, presenting a
// broken link as a valid one.
func (k *InvitationKeyring) DeriveInvitationSecret(keyVersion int,
	companyID, invitationID string, accessGeneration int64) (string, error) {

	key, ok := k.keys[keyVersion]
	if !ok {
		return "", fmt.Errorf("secrets: invitation secret key version %d is not configured; "+
			"it must remain configured while any invitation references it", keyVersion)
	}

	mac := hmac.New(sha256.New, key)
	writeLengthPrefixed(mac, []byte(invitationSecretDomain))
	writeLengthPrefixedUint64(mac, uint64(keyVersion))
	writeLengthPrefixed(mac, []byte(companyID))
	writeLengthPrefixed(mac, []byte(invitationID))
	writeLengthPrefixedUint64(mac, uint64(accessGeneration))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// writeLengthPrefixed writes an 8-byte big-endian length followed by the field.
//
// Length prefixing rather than delimiter concatenation is what stops
// ("ab","c") and ("a","bc") producing one canonical input — which would let two
// distinct invitations resolve to the same link. A delimiter would only move
// the problem to identifiers containing the delimiter.
func writeLengthPrefixed(mac interface{ Write([]byte) (int, error) }, field []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	// hash.Hash.Write never returns an error, per its documented contract.
	_, _ = mac.Write(length[:])
	_, _ = mac.Write(field)
}

func writeLengthPrefixedUint64(mac interface{ Write([]byte) (int, error) }, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	writeLengthPrefixed(mac, encoded[:])
}

// HashInvitationSecret returns the hex-encoded SHA-256 hash of the ENCODED
// token. This is the only representation persisted.
//
// SHA-256 rather than bcrypt matches the established access.HashAccessToken
// convention and is sufficient here: the token carries 256 bits of HMAC-derived
// entropy, unlike a human-chosen password.
func HashInvitationSecret(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// VerifyInvitationSecret reports whether rawToken hashes to storedHash, using a
// constant-time comparison so a timing signal cannot be used to recover a valid
// token byte by byte.
//
// Empty inputs never verify. Without that guard, an invitation whose hash field
// was somehow blank would be openable by presenting an empty token.
func VerifyInvitationSecret(rawToken, storedHash string) bool {
	if rawToken == "" || storedHash == "" {
		return false
	}
	computed := HashInvitationSecret(rawToken)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}
