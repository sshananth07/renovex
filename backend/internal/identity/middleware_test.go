package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireAuthSetsPrincipalOnValidToken(t *testing.T) {
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	token, err := issuer.IssueAccessToken("user_1", "company_1", "owner")
	if err != nil {
		t.Fatalf("unexpected error issuing: %v", err)
	}

	var capturedPrincipal *Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if ok {
			capturedPrincipal = &p
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(issuer)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedPrincipal == nil {
		t.Fatal("expected Principal to be set in context")
	}
	if capturedPrincipal.UserID != "user_1" || capturedPrincipal.CompanyID != "company_1" || capturedPrincipal.Role != "owner" {
		t.Fatalf("unexpected principal: %+v", capturedPrincipal)
	}
}

func TestRequireAuthRejectsMissingHeader(t *testing.T) {
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(issuer)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuthRejectsInvalidToken(t *testing.T) {
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(issuer)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
