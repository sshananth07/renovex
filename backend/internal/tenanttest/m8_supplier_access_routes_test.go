package tenanttest_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestM8SupplierAccessRoutesArePublicAndApplyRevision14SecurityHeaders(
	t *testing.T) {

	router := setupRouter(t)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{
			name: "clean open", method: http.MethodGet,
			path: "/supplier-access/open", status: http.StatusOK,
		},
		{
			name: "token open", method: http.MethodGet,
			path:   "/supplier-access/open?token=malformed",
			status: http.StatusNotFound,
		},
		{
			name: "challenge", method: http.MethodPost,
			path: "/supplier-access/challenges", body: `{}`,
			status: http.StatusUnprocessableEntity,
		},
		{
			name: "resend", method: http.MethodPost,
			path: "/supplier-access/challenges/resend", body: `{}`,
			status: http.StatusUnprocessableEntity,
		},
		{
			name: "verify", method: http.MethodPost,
			path: "/supplier-access/challenges/verify", body: `{}`,
			status: http.StatusUnprocessableEntity,
		},
		{
			name: "logout", method: http.MethodPost,
			path:   "/supplier-access/session/logout",
			status: http.StatusForbidden,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(
				testCase.method, testCase.path,
				strings.NewReader(testCase.body))
			if testCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d: %s",
					response.Code, testCase.status, response.Body.String())
			}
			if response.Header().Get("Cache-Control") !=
				"no-store, max-age=0" ||
				response.Header().Get("Pragma") != "no-cache" ||
				response.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("security headers = %#v", response.Header())
			}
			if strings.Contains(response.Body.String(), "bearer") ||
				strings.Contains(response.Body.String(), "authorization") {
				t.Fatalf("public route was guarded by contractor auth: %s",
					response.Body.String())
			}
		})
	}
}
