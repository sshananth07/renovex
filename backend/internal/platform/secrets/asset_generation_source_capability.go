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

// assetGenerationSourceCapabilityDomain separates this HMAC's inputs from
// every other use of the same key material (RP4E0) — deliberately
// distinct from visualAssetCapabilityDomain so a capability minted for
// one purpose can never be replayed against the other's verification
// route, even if the same underlying key bytes were ever reused (they
// are not, by convention, but domain separation removes the dependency
// on that convention holding).
const assetGenerationSourceCapabilityDomain = "asset-generation-source-read-capability-v1"

// minAssetGenerationSourceCapabilityKeyBytes matches
// minVisualAssetCapabilityKeyBytes's precedent.
const minAssetGenerationSourceCapabilityKeyBytes = 32

// AssetGenerationSourceCapabilityKeyring mints and verifies short-lived,
// self-contained bearer tokens granting the Hunyuan provider temporary
// read access to one tenant-authorized source image's bytes (RP4E0).
// Mirrors VisualAssetCapabilityKeyring's exact shape — a self-describing
// token embedding its own claims, verified by re-derivation with no
// prior database lookup.
type AssetGenerationSourceCapabilityKeyring struct {
	activeVersion int
	keys          map[int][]byte
}

// AssetGenerationSourceCapabilityClaims is what Verify recovers from a
// valid token.
type AssetGenerationSourceCapabilityClaims struct {
	CompanyID  string
	ArtifactID string
	ExpiresAt  time.Time
}

// ErrAssetGenerationSourceCapabilityInvalid is returned by Verify for
// every failure mode (malformed, tampered, expired, or signed by an
// unconfigured key version) — deliberately one generic error, matching
// VisualAssetCapabilityKeyring's "never distinguish the exact cause"
// convention.
var ErrAssetGenerationSourceCapabilityInvalid = errors.New("secrets: asset generation source capability invalid or expired")

// NewAssetGenerationSourceCapabilityKeyring validates the configured
// versioned keys at startup — a missing, malformed, undersized, or
// unconfigured-active key fails backend startup.
func NewAssetGenerationSourceCapabilityKeyring(activeVersion int, encodedKeys map[int]string) (*AssetGenerationSourceCapabilityKeyring, error) {
	if len(encodedKeys) == 0 {
		return nil, fmt.Errorf("secrets: at least one asset generation source capability key must be configured")
	}
	keys := make(map[int][]byte, len(encodedKeys))
	for version, encoded := range encodedKeys {
		if version < 1 {
			return nil, fmt.Errorf("secrets: asset generation source capability key version must be >= 1, got %d", version)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secrets: asset generation source capability key v%d is not valid base64: %w", version, err)
		}
		if len(key) < minAssetGenerationSourceCapabilityKeyBytes {
			return nil, fmt.Errorf("secrets: asset generation source capability key v%d decodes to %d bytes; "+
				"at least %d random bytes are required", version, len(key), minAssetGenerationSourceCapabilityKeyBytes)
		}
		keys[version] = key
	}
	if _, ok := keys[activeVersion]; !ok {
		return nil, fmt.Errorf("secrets: active asset generation source capability key version %d is not configured", activeVersion)
	}
	return &AssetGenerationSourceCapabilityKeyring{activeVersion: activeVersion, keys: keys}, nil
}

// Mint produces a self-contained bearer token for (companyID,
// artifactID), valid until expiresAt, signed with the active key version.
func (k *AssetGenerationSourceCapabilityKeyring) Mint(companyID, artifactID string, expiresAt time.Time) (string, error) {
	key, ok := k.keys[k.activeVersion]
	if !ok {
		return "", fmt.Errorf("secrets: asset generation source capability key version %d is not configured", k.activeVersion)
	}
	claims := assetGenerationSourceCapabilityClaimsBytes(k.activeVersion, companyID, artifactID, expiresAt)
	mac := computeAssetGenerationSourceCapabilityMAC(key, claims)
	token := append(claims, mac...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// Verify recovers and validates the claims embedded in token: signature
// verified in constant time BEFORE any claim is trusted, then expiry
// checked. Returns ErrAssetGenerationSourceCapabilityInvalid for every
// failure mode.
func (k *AssetGenerationSourceCapabilityKeyring) Verify(token string) (AssetGenerationSourceCapabilityClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return AssetGenerationSourceCapabilityClaims{}, ErrAssetGenerationSourceCapabilityInvalid
	}
	if len(raw) <= sha256.Size {
		return AssetGenerationSourceCapabilityClaims{}, ErrAssetGenerationSourceCapabilityInvalid
	}
	claimsBytes := raw[:len(raw)-sha256.Size]
	presentedMAC := raw[len(raw)-sha256.Size:]

	keyVersion, companyID, artifactID, expiresAt, ok := parseAssetGenerationSourceCapabilityClaims(claimsBytes)
	if !ok {
		return AssetGenerationSourceCapabilityClaims{}, ErrAssetGenerationSourceCapabilityInvalid
	}
	key, ok := k.keys[keyVersion]
	if !ok {
		return AssetGenerationSourceCapabilityClaims{}, ErrAssetGenerationSourceCapabilityInvalid
	}
	expectedMAC := computeAssetGenerationSourceCapabilityMAC(key, claimsBytes)
	if subtle.ConstantTimeCompare(presentedMAC, expectedMAC) != 1 {
		return AssetGenerationSourceCapabilityClaims{}, ErrAssetGenerationSourceCapabilityInvalid
	}
	if time.Now().After(expiresAt) {
		return AssetGenerationSourceCapabilityClaims{}, ErrAssetGenerationSourceCapabilityInvalid
	}
	return AssetGenerationSourceCapabilityClaims{CompanyID: companyID, ArtifactID: artifactID, ExpiresAt: expiresAt}, nil
}

func computeAssetGenerationSourceCapabilityMAC(key, claims []byte) []byte {
	mac := hmac.New(sha256.New, key)
	writeLengthPrefixed(mac, []byte(assetGenerationSourceCapabilityDomain))
	mac.Write(claims)
	return mac.Sum(nil)
}

// assetGenerationSourceCapabilityClaimsBytes serializes claims as:
// [8-byte keyVersion][length-prefixed companyID][length-prefixed
// artifactID][8-byte expiresAtUnix].
func assetGenerationSourceCapabilityClaimsBytes(keyVersion int, companyID, artifactID string, expiresAt time.Time) []byte {
	var buf []byte
	var scratch [8]byte

	binary.BigEndian.PutUint64(scratch[:], uint64(keyVersion))
	buf = append(buf, scratch[:]...)

	binary.BigEndian.PutUint64(scratch[:], uint64(len(companyID)))
	buf = append(buf, scratch[:]...)
	buf = append(buf, companyID...)

	binary.BigEndian.PutUint64(scratch[:], uint64(len(artifactID)))
	buf = append(buf, scratch[:]...)
	buf = append(buf, artifactID...)

	binary.BigEndian.PutUint64(scratch[:], uint64(expiresAt.Unix()))
	buf = append(buf, scratch[:]...)

	return buf
}

func parseAssetGenerationSourceCapabilityClaims(buf []byte) (keyVersion int, companyID, artifactID string, expiresAt time.Time, ok bool) {
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
		return 0, "", "", time.Time{}, false
	}
	c, okC := readLengthPrefixed()
	if !okC {
		return 0, "", "", time.Time{}, false
	}
	a, okA := readLengthPrefixed()
	if !okA {
		return 0, "", "", time.Time{}, false
	}
	exp, okExp := read8()
	if !okExp || len(buf) != 0 {
		return 0, "", "", time.Time{}, false
	}
	return int(kv), c, a, time.Unix(int64(exp), 0), true
}
