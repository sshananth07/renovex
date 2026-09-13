package composition_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/access"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// This is deliberately a composed-router test. Registering only one handler
// package would miss framework validation and error paths owned by a different
// external module, which is the drift the shared boundary exists to prevent.
func TestComposedExternalRoutesApplyPrivacyHeadersToRealResponses(t *testing.T) {
	router, api := platformhttp.NewRouter("external boundary composition", "test")
	access.RegisterExternalHandlers(api,
		access.NewService(nil, nil, nil, nil, nil, nil, nil, nil, ""))
	invitation := &externalInvitationStub{}
	exchanges := &externalExchangeStub{}
	tokens := &externalTokenSequence{values: []string{
		canonicalExternalToken(1), canonicalExternalToken(2),
	}}
	sessionKeys, err := secrets.NewSupplierSessionTokenKeyring(1,
		map[int]string{1: "dGVuYW50dGVzdC1zdXBwbGllci1zZXNzaW9uLWtleS12MQ=="})
	if err != nil {
		t.Fatalf("construct Supplier session keyring: %v", err)
	}
	supplierService := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(invitation, invitation),
		supplieraccess.WithAccessExchangeStore(exchanges),
		supplieraccess.WithOpaqueTokenGenerator(tokens),
		// Logout returns an idempotent 204 before dereferencing the repository
		// when the presented session credential is non-canonical. A concrete
		// repository keeps the service configuration truthful in this transport
		// test without requiring persistence.
		supplieraccess.WithSessionStores(
			externalSessionStoreStub{}, nil),
		supplieraccess.WithSessionSecurity(sessionKeys),
	)
	supplieraccess.RegisterHandlers(api, supplierService, false, http.SameSiteLaxMode)
	supplieroffers.RegisterHandlers(api, supplieroffers.NewService())

	invitationToken := canonicalExternalToken(3)
	csrfToken := canonicalExternalToken(4)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		configure  func(*http.Request)
		forbidden  string
	}{
		{
			name: "actual Supplier success", method: http.MethodGet,
			path: "/supplier-access/open", wantStatus: http.StatusOK,
		},
		{
			name: "actual Supplier mapped domain error", method: http.MethodGet,
			path:       "/supplier-access/open?token=not-a-canonical-token",
			wantStatus: http.StatusNotFound,
			forbidden:  "not-a-canonical-token",
		},
		{
			name: "actual Supplier framework validation error", method: http.MethodPost,
			path: "/supplier-access/challenges/verify", body: `{}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "actual Supplier redirect", method: http.MethodGet,
			path:       "/supplier-access/open?token=" + invitationToken,
			wantStatus: http.StatusSeeOther,
		},
		{
			name: "actual Supplier empty response", method: http.MethodPost,
			path: "/supplier-access/session/logout", wantStatus: http.StatusNoContent,
			configure: func(request *http.Request) {
				request.AddCookie(&http.Cookie{Name: supplieraccess.SupplierSessionCookieName, Value: "invalid"})
				request.AddCookie(&http.Cookie{Name: supplieraccess.SupplierCSRFCookieName, Value: csrfToken})
				request.Header.Set("X-CSRF-Token", csrfToken)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			if tc.configure != nil {
				tc.configure(request)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.wantStatus, response.Body.String())
			}
			assertExternalPrivacyHeaders(t, response)
			if response.Body.Len() > 4096 {
				t.Errorf("external response body is unbounded: %d bytes", response.Body.Len())
			}
			if tc.forbidden != "" && bytes.Contains(response.Body.Bytes(), []byte(tc.forbidden)) {
				t.Errorf("external response echoed private input %q: %s", tc.forbidden, response.Body.String())
			}
		})
	}
}

type externalInvitationStub struct{}

func (*externalInvitationStub) ResolveInvitationAccess(context.Context, string, time.Time) (
	supplieraccess.InvitationAccessSnapshot, bool, error) {
	return supplieraccess.InvitationAccessSnapshot{
		CompanyID: "company-1", SupplierID: "supplier-1", InvitationID: "invitation-1",
		NormalizedRecipientEmail: "supplier@example.test", AccessGeneration: 1,
		CurrentIssuedRFQVersionID: "issued-version-1",
	}, true, nil
}

func (*externalInvitationStub) RecordInvitationViewed(context.Context, string, string, int64, time.Time) error {
	return nil
}

type externalExchangeStub struct{}

func (*externalExchangeStub) CreateExchange(_ context.Context,
	exchange supplieraccess.SupplierAccessExchange) (supplieraccess.SupplierAccessExchange, error) {
	return exchange, nil
}

type externalSessionStoreStub struct{}

func (externalSessionStoreStub) CreateSession(_ context.Context, session supplieraccess.SupplierSession) (
	supplieraccess.SupplierSession, error) {
	return session, nil
}

func (externalSessionStoreStub) FindSessionByTokenHash(context.Context, string) (
	supplieraccess.SupplierSession, error) {
	return supplieraccess.SupplierSession{}, supplieraccess.ErrSupplierSessionNotFound
}

func (externalSessionStoreStub) FindSession(context.Context, string, string) (
	supplieraccess.SupplierSession, error) {
	return supplieraccess.SupplierSession{}, supplieraccess.ErrSupplierSessionNotFound
}

func (externalSessionStoreStub) ReplaceSessionCAS(_ context.Context,
	session supplieraccess.SupplierSession, _ int64) (supplieraccess.SupplierSession, error) {
	return session, nil
}

type externalTokenSequence struct {
	values []string
	next   int
}

func (sequence *externalTokenSequence) Generate() (string, error) {
	value := sequence.values[sequence.next]
	sequence.next++
	return value, nil
}

func canonicalExternalToken(fill byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func assertExternalPrivacyHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Cache-Control":          "no-store, max-age=0",
		"Pragma":                 "no-cache",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
	}
	for header, value := range want {
		if got := response.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q; headers=%#v", header, got, value, response.Header())
		}
	}
}
