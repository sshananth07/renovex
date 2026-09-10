package config

import (
	"fmt"
	"net/url"
	"strings"
)

// AllowedOrigins is an immutable set of exact browser origins (scheme + host
// + optional port, no path/query/fragment/credentials) permitted to make
// credentialed cross-origin requests. Built once at startup by
// parseAllowedOrigins and shared by both CORS middleware and the
// refresh/logout Origin guard, so the two never drift on what "allowed"
// means.
type AllowedOrigins struct {
	set []string
}

// Contains reports whether origin is exactly one of the allowed origins.
func (a AllowedOrigins) Contains(origin string) bool {
	for _, o := range a.set {
		if o == origin {
			return true
		}
	}
	return false
}

// Len reports the number of distinct allowed origins.
func (a AllowedOrigins) Len() int {
	return len(a.set)
}

// NewAllowedOriginsForTest builds an AllowedOrigins from literal origin
// strings, applying the same validation LoadFromEnv uses. It exists so other
// packages' tests (CORS middleware, the refresh/logout Origin guard) can
// construct a policy without going through environment variables.
func NewAllowedOriginsForTest(origins []string) (AllowedOrigins, error) {
	return parseAllowedOrigins(strings.Join(origins, ","))
}

// parseAllowedOrigins parses a comma-separated list of exact origins from
// raw. Each entry is trimmed, validated, and de-duplicated. An empty raw
// value yields a zero-length AllowedOrigins, not an error — production
// validation (requiring at least one origin) happens separately in
// LoadFromEnv, since a test/local environment may legitimately run with none.
func parseAllowedOrigins(raw string) (AllowedOrigins, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AllowedOrigins{}, nil
	}

	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			return AllowedOrigins{}, fmt.Errorf("config: APP_ALLOWED_ORIGINS contains an empty entry")
		}
		if err := validateOrigin(value); err != nil {
			return AllowedOrigins{}, err
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		origins = append(origins, value)
	}
	return AllowedOrigins{set: origins}, nil
}

// validateOrigin rejects anything that is not an exact http(s) origin: no
// path, query, fragment, userinfo, and no wildcard/null literals.
func validateOrigin(value string) error {
	if value == "*" {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS must not contain a wildcard origin")
	}
	if strings.EqualFold(value, "null") {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS must not contain the null origin")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS contains a malformed origin %q: %w", value, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q must use http or https", value)
	}
	if parsed.Host == "" {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q is missing a host", value)
	}
	if parsed.User != nil {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q must not contain credentials", value)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q must not contain a path", value)
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q must not contain a query string", value)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q must not contain a fragment", value)
	}
	// url.Parse is lenient about bare "host:port" or values with embedded
	// whitespace; round-tripping to origin form (scheme://host) and comparing
	// catches anything url.Parse accepted but which isn't a clean origin.
	reconstructed := parsed.Scheme + "://" + parsed.Host
	if reconstructed != value && reconstructed+"/" != value {
		return fmt.Errorf("config: APP_ALLOWED_ORIGINS origin %q is not an exact origin", value)
	}
	return nil
}
