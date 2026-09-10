package http_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func testOrigins(t *testing.T) config.AllowedOrigins {
	t.Helper()
	origins, err := config.NewAllowedOriginsForTest([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatalf("building allowed origins: %v", err)
	}
	return origins
}

func TestOriginGuardAllowedOriginIsAllowedRegardlessOfSecFetchSite(t *testing.T) {
	origins := testOrigins(t)
	for _, secFetchSite := range []string{"", "same-origin", "same-site", "cross-site", "none", "bogus"} {
		if !platformhttp.DecideOriginGuard(origins, "http://localhost:3000", secFetchSite) {
			t.Errorf("allowed origin with Sec-Fetch-Site=%q should be allowed", secFetchSite)
		}
	}
}

func TestOriginGuardUnlistedOriginIsRejected(t *testing.T) {
	origins := testOrigins(t)
	for _, secFetchSite := range []string{"", "same-origin", "same-site", "cross-site"} {
		if platformhttp.DecideOriginGuard(origins, "http://evil.example.com", secFetchSite) {
			t.Errorf("unlisted origin with Sec-Fetch-Site=%q should be rejected", secFetchSite)
		}
	}
}

func TestOriginGuardMalformedOriginIsRejected(t *testing.T) {
	origins := testOrigins(t)
	if platformhttp.DecideOriginGuard(origins, "not a url", "") {
		t.Error("malformed origin should be rejected")
	}
}

func TestOriginGuardNullOriginIsRejected(t *testing.T) {
	origins := testOrigins(t)
	if platformhttp.DecideOriginGuard(origins, "null", "") {
		t.Error("null origin should be rejected")
	}
}

func TestOriginGuardAbsentOriginAbsentSecFetchSiteIsAllowedAsNonBrowser(t *testing.T) {
	origins := testOrigins(t)
	if !platformhttp.DecideOriginGuard(origins, "", "") {
		t.Error("absent Origin and absent Sec-Fetch-Site should be allowed as a non-browser client")
	}
}

func TestOriginGuardAbsentOriginSameOriginIsAllowed(t *testing.T) {
	origins := testOrigins(t)
	if !platformhttp.DecideOriginGuard(origins, "", "same-origin") {
		t.Error("absent Origin with Sec-Fetch-Site=same-origin should be allowed")
	}
}

func TestOriginGuardAbsentOriginSameSiteIsAllowed(t *testing.T) {
	origins := testOrigins(t)
	if !platformhttp.DecideOriginGuard(origins, "", "same-site") {
		t.Error("absent Origin with Sec-Fetch-Site=same-site should be allowed")
	}
}

func TestOriginGuardAbsentOriginCrossSiteIsRejected(t *testing.T) {
	origins := testOrigins(t)
	if platformhttp.DecideOriginGuard(origins, "", "cross-site") {
		t.Error("absent Origin with Sec-Fetch-Site=cross-site should be rejected")
	}
}

func TestOriginGuardAbsentOriginNoneSecFetchSiteIsRejected(t *testing.T) {
	origins := testOrigins(t)
	if platformhttp.DecideOriginGuard(origins, "", "none") {
		t.Error("absent Origin with Sec-Fetch-Site=none should be rejected")
	}
}

func TestOriginGuardAbsentOriginUnknownSecFetchSiteIsRejected(t *testing.T) {
	origins := testOrigins(t)
	if platformhttp.DecideOriginGuard(origins, "", "some-future-value") {
		t.Error("absent Origin with an unrecognized Sec-Fetch-Site should be rejected")
	}
}
