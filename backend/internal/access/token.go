package access

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// accessTokenBytes is the entropy size of a raw client access token.
// 32 bytes (256 bits) matches identity.GenerateRefreshToken's precedent.
const accessTokenBytes = 32

// GenerateAccessToken returns a cryptographically random, base64url-encoded
// opaque token for a Client Access Grant. It is never a JWT — it carries no
// claims and is only ever resolved by looking up its hash (design spec §3).
// The raw value is returned to the contractor exactly once, at the moment
// its grant is activated, and is never persisted or reconstructable.
func GenerateAccessToken() (string, error) {
	buf := make([]byte, accessTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashAccessToken returns the hex-encoded SHA-256 hash of a raw access token.
// SHA-256 (not bcrypt) is sufficient: the token already carries 256 bits of
// its own entropy, unlike a human-chosen password (design spec §3).
func HashAccessToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
