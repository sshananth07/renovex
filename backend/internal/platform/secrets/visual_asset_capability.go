package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// visualAssetCapabilityDomain separates this HMAC's inputs from every
// other use of the same key material (RP4D).
const visualAssetCapabilityDomain = "visual-asset-read-capability-v1"

// minVisualAssetCapabilityKeyBytes matches MinInvitationKeyBytes's
// existing precedent — a shorter key is rejected at construction so it
// fails application startup rather than silently weakening every minted
// capability.
const minVisualAssetCapabilityKeyBytes = 32

// VisualAssetCapabilityKeyring mints and verifies short-lived, self-
// contained bearer tokens granting temporary read access to one published
// VisualAssetVersion's bytes (RP4D). Unlike InvitationKeyring/
// SupplierSessionTokenKeyring (which derive a value later verified by
// RE-DERIVING and comparing against a value the caller already knows),
// this keyring's tokens are self-describing: the token itself carries its
// claims (companyID, assetID, version, expiry, key version), so
// verification recovers those claims from the token with no prior
// database lookup — the server never persists anything about a minted
// capability.
type VisualAssetCapabilityKeyring struct {
	activeVersion int
	keys          map[int][]byte
}

// VisualAssetCapabilityClaims is what Verify recovers from a valid token.
type VisualAssetCapabilityClaims struct {
	CompanyID string
	AssetID   string
	Version   int
	ExpiresAt time.Time
}

// ErrVisualAssetCapabilityInvalid is returned by Verify for any failure
// mode (malformed, tampered, expired, or signed by an unconfigured key
// version) — deliberately one generic error, never distinguishing the
// exact cause in a way a caller could turn into a response detail (RP4D
// §6: "never distinguishing 'expired' from 'tampered' from 'wrong asset'
// in the response body").
var ErrVisualAssetCapabilityInvalid = errors.New("secrets: visual asset capability invalid or expired")

// NewVisualAssetCapabilityKeyring validates the configured versioned keys
// at startup — a missing, malformed, undersized, or unconfigured-active
// key fails backend startup, matching InvitationKeyring's exact
// "never silently serve unsigned content" precedent.
func NewVisualAssetCapabilityKeyring(activeVersion int, encodedKeys map[int]string) (*VisualAssetCapabilityKeyring, error) {
	if len(encodedKeys) == 0 {
		return nil, fmt.Errorf("secrets: at least one visual asset capability key must be configured")
	}
	keys := make(map[int][]byte, len(encodedKeys))
	for version, encoded := range encodedKeys {
		if version < 1 {
			return nil, fmt.Errorf("secrets: visual asset capability key version must be >= 1, got %d", version)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secrets: visual asset capability key v%d is not valid base64: %w", version, err)
		}
		if len(key) < minVisualAssetCapabilityKeyBytes {
			return nil, fmt.Errorf("secrets: visual asset capability key v%d decodes to %d bytes; "+
				"at least %d random bytes are required", version, len(key), minVisualAssetCapabilityKeyBytes)
		}
		keys[version] = key
	}
	if _, ok := keys[activeVersion]; !ok {
		return nil, fmt.Errorf("secrets: active visual asset capability key version %d is not configured", activeVersion)
	}
	return &VisualAssetCapabilityKeyring{activeVersion: activeVersion, keys: keys}, nil
}

// Mint produces a self-contained bearer token for (companyID, assetID,
// version), valid until expiresAt, signed with the active key version.
func (k *VisualAssetCapabilityKeyring) Mint(companyID, assetID string, version int, expiresAt time.Time) (string, error) {
	return k.mintWithKeyVersion(k.activeVersion, companyID, assetID, version, expiresAt)
}

func (k *VisualAssetCapabilityKeyring) mintWithKeyVersion(keyVersion int, companyID, assetID string, version int, expiresAt time.Time) (string, error) {
	key, ok := k.keys[keyVersion]
	if !ok {
		return "", fmt.Errorf("secrets: visual asset capability key version %d is not configured", keyVersion)
	}
	claims := visualAssetCapabilityClaimsBytes(keyVersion, companyID, assetID, version, expiresAt)
	mac := computeVisualAssetCapabilityMAC(key, claims)
	token := append(claims, mac...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// Verify recovers and validates the claims embedded in token: signature
// verified in constant time BEFORE any claim is trusted, then expiry
// checked. Returns ErrVisualAssetCapabilityInvalid for every failure mode.
func (k *VisualAssetCapabilityKeyring) Verify(token string) (VisualAssetCapabilityClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return VisualAssetCapabilityClaims{}, ErrVisualAssetCapabilityInvalid
	}
	if len(raw) <= sha256.Size {
		return VisualAssetCapabilityClaims{}, ErrVisualAssetCapabilityInvalid
	}
	claimsBytes := raw[:len(raw)-sha256.Size]
	presentedMAC := raw[len(raw)-sha256.Size:]

	keyVersion, companyID, assetID, version, expiresAt, ok := parseVisualAssetCapabilityClaims(claimsBytes)
	if !ok {
		return VisualAssetCapabilityClaims{}, ErrVisualAssetCapabilityInvalid
	}
	key, ok := k.keys[keyVersion]
	if !ok {
		return VisualAssetCapabilityClaims{}, ErrVisualAssetCapabilityInvalid
	}
	expectedMAC := computeVisualAssetCapabilityMAC(key, claimsBytes)
	if subtle.ConstantTimeCompare(presentedMAC, expectedMAC) != 1 {
		return VisualAssetCapabilityClaims{}, ErrVisualAssetCapabilityInvalid
	}
	if time.Now().After(expiresAt) {
		return VisualAssetCapabilityClaims{}, ErrVisualAssetCapabilityInvalid
	}
	return VisualAssetCapabilityClaims{CompanyID: companyID, AssetID: assetID, Version: version, ExpiresAt: expiresAt}, nil
}

func computeVisualAssetCapabilityMAC(key, claims []byte) []byte {
	mac := hmac.New(sha256.New, key)
	writeLengthPrefixed(mac, []byte(visualAssetCapabilityDomain))
	mac.Write(claims)
	return mac.Sum(nil)
}

// visualAssetCapabilityClaimsBytes serializes claims as:
// [8-byte keyVersion][length-prefixed companyID][length-prefixed assetID]
// [8-byte version][8-byte expiresAtUnix]. Length-prefixing (not delimiter
// concatenation) is what stops ("ab","c") and ("a","bc") producing the
// same canonical input, matching writeLengthPrefixed's existing rationale
// in keyring.go.
func visualAssetCapabilityClaimsBytes(keyVersion int, companyID, assetID string, version int, expiresAt time.Time) []byte {
	var buf []byte
	var scratch [8]byte

	binary.BigEndian.PutUint64(scratch[:], uint64(keyVersion))
	buf = append(buf, scratch[:]...)

	binary.BigEndian.PutUint64(scratch[:], uint64(len(companyID)))
	buf = append(buf, scratch[:]...)
	buf = append(buf, companyID...)

	binary.BigEndian.PutUint64(scratch[:], uint64(len(assetID)))
	buf = append(buf, scratch[:]...)
	buf = append(buf, assetID...)

	binary.BigEndian.PutUint64(scratch[:], uint64(version))
	buf = append(buf, scratch[:]...)

	binary.BigEndian.PutUint64(scratch[:], uint64(expiresAt.Unix()))
	buf = append(buf, scratch[:]...)

	return buf
}

func parseVisualAssetCapabilityClaims(buf []byte) (keyVersion int, companyID, assetID string, version int, expiresAt time.Time, ok bool) {
	read8 := func() (uint64, bool) {
		if len(buf) < 8 {
			return 0, false
		}
		v := binary.BigEndian.Uint64(buf[:8])
		buf = buf[8:]
		return v, true
	}
	readLengthPrefixed := func() (string, bool) {
		length, ok := read8()
		if !ok || uint64(len(buf)) < length {
			return "", false
		}
		s := string(buf[:length])
		buf = buf[length:]
		return s, true
	}

	kv, okKV := read8()
	if !okKV {
		return 0, "", "", 0, time.Time{}, false
	}
	c, okC := readLengthPrefixed()
	if !okC {
		return 0, "", "", 0, time.Time{}, false
	}
	a, okA := readLengthPrefixed()
	if !okA {
		return 0, "", "", 0, time.Time{}, false
	}
	v, okV := read8()
	if !okV {
		return 0, "", "", 0, time.Time{}, false
	}
	exp, okExp := read8()
	if !okExp || len(buf) != 0 {
		return 0, "", "", 0, time.Time{}, false
	}
	return int(kv), c, a, int(v), time.Unix(int64(exp), 0), true
}
