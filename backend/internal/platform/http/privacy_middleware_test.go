package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newPrivacyTestRouter(t *testing.T, logger zerolog.Logger) (http.Handler, huma.API) {
	t.Helper()
	router, api := platformhttp.NewRouter("privacy-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(platformhttp.RequestPrivacyMiddleware(logger))
	huma.Register(authedAPI, huma.Operation{
		OperationID: "privacy-probe-ok", Method: http.MethodGet, Path: "/authed/ok",
	}, func(context.Context, *struct{}) (*struct{ Body map[string]string }, error) {
		return &struct{ Body map[string]string }{Body: map[string]string{"ok": "true"}}, nil
	})
	huma.Register(authedAPI, huma.Operation{
		OperationID: "privacy-probe-error", Method: http.MethodGet, Path: "/authed/error",
	}, func(context.Context, *struct{}) (*struct{ Body map[string]string }, error) {
		return nil, huma.Error404NotFound("not found")
	})
	huma.Register(api, huma.Operation{
		OperationID: "privacy-probe-public", Method: http.MethodGet, Path: "/public",
	}, func(context.Context, *struct{}) (*struct{ Body map[string]string }, error) {
		return &struct{ Body map[string]string }{Body: map[string]string{"ok": "true"}}, nil
	})
	return router, api
}

func TestPrivacyHeadersPresentOnSuccessfulAuthedResponse(t *testing.T) {
	router, _ := newPrivacyTestRouter(t, zerolog.New(new(bytes.Buffer)))
	request := httptest.NewRequest(http.MethodGet, "/authed/ok", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID missing on successful response")
	}
}

func TestPrivacyHeadersPresentOnErrorAuthedResponse(t *testing.T) {
	router, _ := newPrivacyTestRouter(t, zerolog.New(new(bytes.Buffer)))
	request := httptest.NewRequest(http.MethodGet, "/authed/error", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID missing on error response")
	}
}

func TestPrivacyHeadersAbsentOnPublicRoute(t *testing.T) {
	router, _ := newPrivacyTestRouter(t, zerolog.New(new(bytes.Buffer)))
	request := httptest.NewRequest(http.MethodGet, "/public", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Cache-Control"); got == "no-store, max-age=0" {
		t.Error("public route must not receive the authenticated-route privacy Cache-Control header")
	}
	if got := response.Header().Get("X-Request-ID"); got != "" {
		t.Error("public route must not receive an authenticated-route X-Request-ID header")
	}
}

func TestPrivacyHeadersAbsentOnOpenAPIAndDocsRoutes(t *testing.T) {
	router, _ := newPrivacyTestRouter(t, zerolog.New(new(bytes.Buffer)))
	for _, path := range []string{"/openapi.json", "/docs", "/schemas"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if got := response.Header().Get("X-Request-ID"); got != "" {
			t.Errorf("%s must not receive an authenticated-route X-Request-ID header, got %q", path, got)
		}
	}
}

func TestRequestIDAcceptsValidCallerSuppliedValue(t *testing.T) {
	router, _ := newPrivacyTestRouter(t, zerolog.New(new(bytes.Buffer)))
	request := httptest.NewRequest(http.MethodGet, "/authed/ok", nil)
	request.Header.Set("X-Request-ID", "caller-supplied-id-123")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "caller-supplied-id-123" {
		t.Fatalf("X-Request-ID = %q, want the caller-supplied value preserved", got)
	}
}

func TestRequestIDReplacesInvalidCallerSuppliedValue(t *testing.T) {
	router, _ := newPrivacyTestRouter(t, zerolog.New(new(bytes.Buffer)))
	request := httptest.NewRequest(http.MethodGet, "/authed/ok", nil)
	request.Header.Set("X-Request-ID", "bad id with spaces and a very very very very very very "+
		"very very very very very very very very very very long tail that exceeds one hundred and "+
		"twenty eight bytes of printable ascii content by a wide margin indeed")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	got := response.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("expected a generated X-Request-ID to replace the invalid caller-supplied value")
	}
	if !platformhttp.IsValidRequestID(got) {
		t.Fatalf("replacement X-Request-ID %q is not itself valid", got)
	}
}

func TestRequestIDIsLoggedInStructuredRequestLog(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	router, _ := newPrivacyTestRouter(t, logger)

	request := httptest.NewRequest(http.MethodGet, "/authed/ok", nil)
	request.Header.Set("X-Request-ID", "log-correlation-id-456")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	var logLine map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &logLine); err != nil {
		t.Fatalf("decoding log line %q: %v", buf.String(), err)
	}
	if logLine["requestId"] != "log-correlation-id-456" {
		t.Fatalf("log line requestId = %v, want log-correlation-id-456; full line: %s", logLine["requestId"], buf.String())
	}
}
