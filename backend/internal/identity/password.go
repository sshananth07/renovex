package identity

import "golang.org/x/crypto/bcrypt"

// HashPassword bcrypt-hashes plaintext at the default cost.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword returns nil if plaintext matches hash, or a non-nil error
// (including bcrypt.ErrMismatchedHashAndPassword) otherwise.
func VerifyPassword(hash, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
}
