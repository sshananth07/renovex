package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newCORSTestRouter(t *testing.T, allowed ...string) http.Handler {
	t.Helper()
	origins, err := config.NewAllowedOriginsForTest(allowed)
	if err != nil {
		t.Fatalf("building allowed origins: %v", err)
	}
	chiRouter, api := platformhttp.NewRouter("cors-test", "0.0.0")
	router := platformhttp.WrapCORS(chiRouter, origins)
	huma.Register(api, huma.Operation{
		OperationID: "cors-probe", Method: http.MethodGet, Path: "/probe",
	}, func(context.Context, *struct{}) (*struct{ Body map[string]string }, error) {
		return &struct{ Body map[string]string }{Body: map[string]string{"ok": "true"}}, nil
	})
	return router
}

func TestCORSAllowedOriginGetsExactEchoAndCredentials(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q", got)
	}
	if got := response.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q", got)
	}
}

func TestCORSNeverReturnsWildcard(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatal("Access-Control-Allow-Origin must never be a wildcard")
	}
}

func TestCORSUnlistedOriginGetsNoPermissiveHeaders(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Origin", "http://evil.example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin for unlisted origin, got %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Credentials for unlisted origin, got %q", got)
	}
}

func TestCORSMissingOriginGetsNoUnnecessaryCORSHeaders(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin without an Origin header, got %q", got)
	}
}

func TestCORSAllowedPreflightSucceedsWithoutInvokingEndpoint(t *testing.T) {
	invoked := false
	origins, err := config.NewAllowedOriginsForTest([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatalf("building allowed origins: %v", err)
	}
	chiRouter, api := platformhttp.NewRouter("cors-preflight-test", "0.0.0")
	router := platformhttp.WrapCORS(chiRouter, origins)
	huma.Register(api, huma.Operation{
		OperationID: "cors-preflight-probe", Method: http.MethodPost, Path: "/probe",
	}, func(context.Context, *struct{}) (*struct{ Body map[string]string }, error) {
		invoked = true
		return &struct{ Body map[string]string }{Body: map[string]string{"ok": "true"}}, nil
	})

	request := httptest.NewRequest(http.MethodOptions, "/probe", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "Authorization,Content-Type")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("preflight status = %d, body %s", response.Code, response.Body.String())
	}
	if invoked {
		t.Fatal("preflight request must not invoke the endpoint handler")
	}

	methods := response.Header().Get("Access-Control-Allow-Methods")
	for _, want := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if !containsToken(methods, want) {
			t.Fatalf("Access-Control-Allow-Methods = %q, missing %q", methods, want)
		}
	}

	headers := response.Header().Get("Access-Control-Allow-Headers")
	for _, want := range []string{"Authorization", "Content-Type", "X-Request-ID", "X-CSRF-Token"} {
		if !containsToken(headers, want) {
			t.Fatalf("Access-Control-Allow-Headers = %q, missing %q", headers, want)
		}
	}

	exposed := response.Header().Get("Access-Control-Expose-Headers")
	for _, want := range []string{"X-Request-ID", "Retry-After"} {
		if !containsToken(exposed, want) {
			t.Fatalf("Access-Control-Expose-Headers = %q, missing %q", exposed, want)
		}
	}
}

// The supplier-offer quote/decline endpoints are PUT, unlike most other
// mutating routes (POST/PATCH) — this regression-tests the specific route
// shape that was previously blocked by a stale method allowlist, rather than
// relying solely on the generic preflight probe above.
func TestCORSAllowedPreflightForSupplierOfferQuotePUTIncludesPUT(t *testing.T) {
	origins, err := config.NewAllowedOriginsForTest([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatalf("building allowed origins: %v", err)
	}
	chiRouter, api := platformhttp.NewRouter("cors-put-preflight-test", "0.0.0")
	router := platformhttp.WrapCORS(chiRouter, origins)
	huma.Register(api, huma.Operation{
		OperationID: "supplier-offer-quote-line",
		Method:      http.MethodPut,
		Path:        "/supplier-access/invitations/{id}/offer/lines/{lineId}/quote",
	}, func(context.Context, *struct{}) (*struct{ Body map[string]string }, error) {
		return &struct{ Body map[string]string }{Body: map[string]string{"ok": "true"}}, nil
	})

	request := httptest.NewRequest(http.MethodOptions, "/supplier-access/invitations/inv-1/offer/lines/line-1/quote", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", "PUT")
	request.Header.Set("Access-Control-Request-Headers", "Authorization,Content-Type")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("preflight status = %d, body %s", response.Code, response.Body.String())
	}

	methods := response.Header().Get("Access-Control-Allow-Methods")
	if !containsToken(methods, "PUT") {
		t.Fatalf("Access-Control-Allow-Methods = %q, missing %q", methods, "PUT")
	}
}

func TestCORSRejectedPreflightReturnsBounded403(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodOptions, "/probe", nil)
	request.Header.Set("Origin", "http://evil.example.com")
	request.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("rejected preflight status = %d, want 403", response.Code)
	}
}

func TestCORSMalformedOriginGetsNoPermissiveHeaders(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Origin", "not a valid origin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin for malformed origin, got %q", got)
	}
}

func TestCORSNullOriginGetsNoPermissiveHeaders(t *testing.T) {
	router := newCORSTestRouter(t, "http://localhost:3000")

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Origin", "null")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin for the null origin, got %q", got)
	}
}

func containsToken(csv, token string) bool {
	for _, part := range splitAndTrim(csv) {
		if part == token {
			return true
		}
	}
	return false
}

func splitAndTrim(csv string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(csv); i++ {
		if i == len(csv) || csv[i] == ',' {
			part := csv[start:i]
			for len(part) > 0 && part[0] == ' ' {
				part = part[1:]
			}
			for len(part) > 0 && part[len(part)-1] == ' ' {
				part = part[:len(part)-1]
			}
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}
