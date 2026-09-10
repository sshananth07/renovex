package identity

import "testing"

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"John@Example.com", "john@example.com"},
		{"  spaced@example.com  ", "spaced@example.com"},
		{"already@lower.com", "already@lower.com"},
	}

	for _, tc := range cases {
		got := NormalizeEmail(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
