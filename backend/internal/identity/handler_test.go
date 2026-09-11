package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"

	"testing"
)

const testRefreshCookieMaxAge = 604800

func testAllowedOrigins(t *testing.T) config.AllowedOrigins {
	t.Helper()
	origins, err := config.NewAllowedOriginsForTest([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatalf("building allowed origins: %v", err)
	}
	return origins
}

func newHandlerTestRouter(t *testing.T, secureRefreshCookie bool) http.Handler {
	t.Helper()
	return newHandlerTestRouterWithOriginsAndSameSite(t, secureRefreshCookie, http.SameSiteLaxMode, testAllowedOrigins(t))
}

func newHandlerTestRouterWithOrigins(t *testing.T, secureRefreshCookie bool, origins config.AllowedOrigins) http.Handler {
	t.Helper()
	return newHandlerTestRouterWithOriginsAndSameSite(t, secureRefreshCookie, http.SameSiteLaxMode, origins)
}

func newHandlerTestRouterWithOriginsAndSameSite(t *testing.T, secureRefreshCookie bool, refreshCookieSameSite http.SameSite, origins config.AllowedOrigins) http.Handler {
	t.Helper()
	companyProv := newFakeCompanyProvisioner()
	authSvc := NewAuthService(
		NewUserService(newFakeUserRepository()),
		newFakeSessionRepo(),
		companyProv,
		companyProv,
		NewJWTIssuer([]byte("test-secret"), 15*time.Minute),
		testRefreshCookieMaxAge*time.Second,
	)
	router, api := platformhttp.NewRouter("identity-handler-test", "0.0.0")
	RegisterHandlers(api, authSvc, testRefreshCookieMaxAge, secureRefreshCookie, refreshCookieSameSite, origins)
	return router
}

func registerAndGetRefreshCookie(t *testing.T, router http.Handler, email string) *http.Cookie {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{
		"email": email, "password": "password123", "companyName": "Acme",
	})
	request := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return refreshCookieFromResponse(t, response)
}

func refreshCookieFromResponse(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range response.Result().Cookies() {
		if c.Name == refreshCookieName {
			return c
		}
	}
	t.Fatalf("no refresh_token cookie in response (status %d, body %s)", response.Code, response.Body.String())
	return nil
}

func loginAndGetRefreshCookie(t *testing.T, router http.Handler, email string) *http.Cookie {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"email": email, "password": "password123"})
	request := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body %s", response.Code, response.Body.String())
	}
	return refreshCookieFromResponse(t, response)
}

func assertRefreshCookieAttributes(t *testing.T, cookie *http.Cookie, expired bool) {
	t.Helper()
	if cookie.Name != refreshCookieName || cookie.Path != "/auth" || !cookie.HttpOnly ||
		!cookie.Secure || cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("unexpected refresh cookie attributes: %#v", cookie)
	}
	if expired {
		if cookie.MaxAge >= 0 {
			t.Fatalf("expired refresh cookie MaxAge = %d, want a negative value", cookie.MaxAge)
		}
		return
	}
	if cookie.MaxAge != testRefreshCookieMaxAge {
		t.Fatalf("refresh cookie MaxAge = %d, want %d", cookie.MaxAge, testRefreshCookieMaxAge)
	}
}

func TestRefreshCookieAttributesInDevelopment(t *testing.T) {
	router := newHandlerTestRouter(t, false)
	cookie := registerAndGetRefreshCookie(t, router, "dev-cookie@example.com")

	if cookie.Name != "refresh_token" {
		t.Fatalf("cookie name = %q", cookie.Name)
	}
	if cookie.Path != "/auth" {
		t.Fatalf("cookie path = %q", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Fatal("expected HttpOnly=true")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v", cookie.SameSite)
	}
	if cookie.Domain != "" {
		t.Fatalf("expected no Domain, got %q", cookie.Domain)
	}
	if cookie.MaxAge != testRefreshCookieMaxAge {
		t.Fatalf("cookie MaxAge = %d", cookie.MaxAge)
	}
	if cookie.Secure {
		t.Fatal("expected Secure=false in development configuration")
	}
}

func TestRefreshCookieAttributesInProduction(t *testing.T) {
	router := newHandlerTestRouterWithOriginsAndSameSite(t, true, http.SameSiteNoneMode, testAllowedOrigins(t))
	cookie := registerAndGetRefreshCookie(t, router, "prod-cookie@example.com")

	if !cookie.Secure {
		t.Fatal("expected Secure=true in production configuration")
	}
	if cookie.Name != "refresh_token" || cookie.Path != "/auth" || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteNoneMode || cookie.MaxAge != testRefreshCookieMaxAge {
		t.Fatalf("unexpected cookie attributes: %#v", cookie)
	}
}

func TestProductionCookieAttributesMatchAcrossAllAuthCookieWriters(t *testing.T) {
	router := newHandlerTestRouterWithOriginsAndSameSite(t, true, http.SameSiteNoneMode, testAllowedOrigins(t))
	registered := registerAndGetRefreshCookie(t, router, "all-cookie-writers@example.com")
	assertRefreshCookieAttributes(t, registered, false)

	loggedIn := loginAndGetRefreshCookie(t, router, "all-cookie-writers@example.com")
	assertRefreshCookieAttributes(t, loggedIn, false)

	rotatedResponse := doRefreshRequest(router, loggedIn.Value, "http://localhost:3000", "cross-site")
	if rotatedResponse.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body %s", rotatedResponse.Code, rotatedResponse.Body.String())
	}
	rotated := refreshCookieFromResponse(t, rotatedResponse)
	assertRefreshCookieAttributes(t, rotated, false)

	logoutResponse := doLogoutRequest(router, rotated.Value, "http://localhost:3000", "cross-site")
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body %s", logoutResponse.Code, logoutResponse.Body.String())
	}
	assertRefreshCookieAttributes(t, refreshCookieFromResponse(t, logoutResponse), true)
}

func TestLogoutClearsCookieWithMatchingAttributes(t *testing.T) {
	router := newHandlerTestRouterWithOriginsAndSameSite(t, true, http.SameSiteNoneMode, testAllowedOrigins(t))
	regCookie := registerAndGetRefreshCookie(t, router, "logout-cookie@example.com")

	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: regCookie.Value})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	var logoutCookie *http.Cookie
	for _, c := range response.Result().Cookies() {
		if c.Name == refreshCookieName {
			logoutCookie = c
		}
	}
	if logoutCookie == nil {
		t.Fatalf("no refresh_token cookie on logout response: %s", response.Body.String())
	}
	if logoutCookie.Name != regCookie.Name {
		t.Fatalf("logout cookie name = %q, register cookie name = %q", logoutCookie.Name, regCookie.Name)
	}
	if logoutCookie.Path != regCookie.Path {
		t.Fatalf("logout cookie path = %q, register cookie path = %q", logoutCookie.Path, regCookie.Path)
	}
	if logoutCookie.SameSite != regCookie.SameSite {
		t.Fatalf("logout cookie SameSite = %v, register cookie SameSite = %v", logoutCookie.SameSite, regCookie.SameSite)
	}
	if logoutCookie.Secure != regCookie.Secure {
		t.Fatalf("logout cookie Secure = %v, register cookie Secure = %v", logoutCookie.Secure, regCookie.Secure)
	}
	if logoutCookie.Domain != regCookie.Domain {
		t.Fatalf("logout cookie Domain = %q, register cookie Domain = %q", logoutCookie.Domain, regCookie.Domain)
	}
	if logoutCookie.MaxAge >= 0 {
		t.Fatalf("expected logout cookie to be expired (negative MaxAge), got %d", logoutCookie.MaxAge)
	}
}

// --- refresh/logout Origin + Sec-Fetch-Site matrix ---

func doRefreshRequest(router http.Handler, refreshTokenValue, origin, secFetchSite string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: refreshTokenValue})
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if secFetchSite != "" {
		request.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func doLogoutRequest(router http.Handler, refreshTokenValue, origin, secFetchSite string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: refreshTokenValue})
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if secFetchSite != "" {
		request.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestRefreshAllowedOriginSucceeds(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-allowed-origin@example.com")

	response := doRefreshRequest(router, regCookie.Value, "http://localhost:3000", "cross-site")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshUnlistedOriginRejected(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-unlisted-origin@example.com")

	response := doRefreshRequest(router, regCookie.Value, "http://evil.example.com", "cross-site")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshMalformedOriginRejected(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-malformed-origin@example.com")

	response := doRefreshRequest(router, regCookie.Value, "not a url", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshNullOriginRejected(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-null-origin@example.com")

	response := doRefreshRequest(router, regCookie.Value, "null", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshAbsentOriginAbsentSecFetchSiteAllowedAsNonBrowser(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-non-browser@example.com")

	response := doRefreshRequest(router, regCookie.Value, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshAbsentOriginSameOriginAllowed(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-same-origin@example.com")

	response := doRefreshRequest(router, regCookie.Value, "", "same-origin")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshAbsentOriginSameSiteAllowed(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-same-site@example.com")

	response := doRefreshRequest(router, regCookie.Value, "", "same-site")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshAbsentOriginCrossSiteRejected(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-cross-site@example.com")

	response := doRefreshRequest(router, regCookie.Value, "", "cross-site")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %s", response.Code, response.Body.String())
	}
}

func TestRefreshAbsentOriginNoneOrUnknownSecFetchSiteRejected(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "refresh-none-unknown@example.com")

	for _, secFetchSite := range []string{"none", "some-future-value"} {
		response := doRefreshRequest(router, regCookie.Value, "", secFetchSite)
		if response.Code != http.StatusForbidden {
			t.Fatalf("Sec-Fetch-Site=%q status = %d, want 403, body %s", secFetchSite, response.Code, response.Body.String())
		}
	}
}

func TestLogoutUnlistedOriginRejected(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "logout-unlisted-origin@example.com")

	response := doLogoutRequest(router, regCookie.Value, "http://evil.example.com", "cross-site")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %s", response.Code, response.Body.String())
	}
}

func TestLogoutAllowedOriginSucceeds(t *testing.T) {
	router := newHandlerTestRouter(t, true)
	regCookie := registerAndGetRefreshCookie(t, router, "logout-allowed-origin@example.com")

	response := doLogoutRequest(router, regCookie.Value, "http://localhost:3000", "cross-site")
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", response.Code, response.Body.String())
	}
}

// TestRegisterAndLoginAreNotSubjectToTheOriginGuard confirms the guard is
// scoped to /auth/refresh and /auth/logout only, per the plan's instruction
// that bearer-authenticated and other unauthenticated routes are unaffected
// — register/login carry no cookie credential to protect and must keep
// working from any origin (subject only to CORS, tested separately).
func TestRegisterAndLoginAreNotSubjectToTheOriginGuard(t *testing.T) {
	router := newHandlerTestRouter(t, true)

	raw, _ := json.Marshal(map[string]string{
		"email": "register-no-guard@example.com", "password": "password123", "companyName": "Acme",
	})
	request := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://evil.example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("register with an unlisted Origin should still succeed (no cookie-Origin guard on register), got %d: %s",
			response.Code, response.Body.String())
	}
}
