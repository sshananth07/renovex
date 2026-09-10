package identity

import (
	"crypto/rand"
	"encoding/base64"
)

// generateTempPassword returns a cryptographically random temporary password
// for a newly-provisioned user (via FindOrCreateUser). Never logged, never
// persisted in plaintext — only its bcrypt hash is stored.
func generateTempPassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
