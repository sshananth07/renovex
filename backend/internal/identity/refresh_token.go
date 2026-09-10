package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateRefreshToken returns a cryptographically random, base64url-encoded
// opaque refresh token (32 bytes of entropy). It is never a JWT — only ever
// looked up by its hash, never parsed.
func GenerateRefreshToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashRefreshToken returns the hex-encoded SHA-256 hash of a raw refresh token.
// SHA-256 (not bcrypt) is sufficient here: the token already carries 256 bits
// of its own entropy, unlike a human-chosen password.
func HashRefreshToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
