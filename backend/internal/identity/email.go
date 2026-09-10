package identity

import "strings"

// NormalizeEmail lowercases and trims an email address so that
// "John@Example.com" and "john@example.com" are treated as the same account.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
