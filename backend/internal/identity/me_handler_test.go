package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/danielgtaylor/huma/v2"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"

	"testing"
)

type meTestFixture struct {
	router     http.Handler
	users      *UserService
	userRepo   *fakeUserRepository
	membership *fakeCurrentMembershipLookup
	jwt        *JWTIssuer
}

func newMeTestFixture(t *testing.T) meTestFixture {
	t.Helper()
	userRepo := newFakeUserRepository()
	users := NewUserService(userRepo)
	membership := newFakeCurrentMembershipLookup()
	currentUserSvc := NewCurrentUserService(users, membership)
	jwtIssuer := NewJWTIssuer([]byte("test-secret"), 15*time.Minute)

	router, api := platformhttp.NewRouter("me-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(RequireAuthHuma(jwtIssuer, api))
	RegisterMeHandler(authedAPI, currentUserSvc)

	return meTestFixture{router: router, users: users, userRepo: userRepo, membership: membership, jwt: jwtIssuer}
}

func (f meTestFixture) seedUserWithMembership(t *testing.T, email, companyID, companyName, role string) (User, string) {
	t.Helper()
	user, err := f.userRepo.Create(context.Background(), User{Email: email, PasswordHash: "hash", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	f.membership.byUserID[user.ID] = currentMembershipFixture{
		companyID: companyID, companyName: companyName, role: role,
	}
	token, err := f.jwt.IssueAccessToken(user.ID, companyID, role)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	return user, token
}

func doMeRequest(router http.Handler, bearerToken string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	if bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestMeReturnsAuthoritativeProjection(t *testing.T) {
	fixture := newMeTestFixture(t)
	user, token := fixture.seedUserWithMembership(t, "owner@example.com", "company-1", "Acme Renovations", "owner")

	response := doMeRequest(fixture.router, token)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}

	var body struct {
		UserID             string `json:"userId"`
		Email              string `json:"email"`
		CompanyID          string `json:"companyId"`
		CompanyName        string `json:"companyName"`
		Role               string `json:"role"`
		MustChangePassword bool   `json:"mustChangePassword"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.UserID != user.ID {
		t.Errorf("userId = %q, want %q", body.UserID, user.ID)
	}
	if body.Email != "owner@example.com" {
		t.Errorf("email = %q", body.Email)
	}
	if body.CompanyID != "company-1" {
		t.Errorf("companyId = %q", body.CompanyID)
	}
	if body.CompanyName != "Acme Renovations" {
		t.Errorf("companyName = %q", body.CompanyName)
	}
	if body.Role != "owner" {
		t.Errorf("role = %q", body.Role)
	}
}

func TestMeRejectsMissingAuthorizationHeader(t *testing.T) {
	fixture := newMeTestFixture(t)

	response := doMeRequest(fixture.router, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", response.Code, response.Body.String())
	}
}

func TestMeRejectsWhenMembershipInconsistentWithToken(t *testing.T) {
	fixture := newMeTestFixture(t)
	// The token claims company-1, but LookupCurrentMembership will report the
	// user's actual current company as company-2 — this must be rejected as
	// 401, not silently trusted from the token's claim.
	user, err := fixture.userRepo.Create(context.Background(), User{Email: "stale@example.com", PasswordHash: "hash", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	fixture.membership.byUserID[user.ID] = currentMembershipFixture{
		companyID: "company-2", companyName: "Different Co", role: "owner",
	}
	token, err := fixture.jwt.IssueAccessToken(user.ID, "company-1", "owner")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	response := doMeRequest(fixture.router, token)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", response.Code, response.Body.String())
	}
}

func TestMeRejectsWhenUserHasNoMembership(t *testing.T) {
	fixture := newMeTestFixture(t)
	user, err := fixture.userRepo.Create(context.Background(), User{Email: "orphan@example.com", PasswordHash: "hash", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := fixture.jwt.IssueAccessToken(user.ID, "company-1", "owner")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	response := doMeRequest(fixture.router, token)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", response.Code, response.Body.String())
	}
}

func TestMeResponseContainsNoPasswordHash(t *testing.T) {
	fixture := newMeTestFixture(t)
	_, token := fixture.seedUserWithMembership(t, "owner@example.com", "company-1", "Acme", "owner")

	response := doMeRequest(fixture.router, token)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	if contains(body, "passwordHash") || contains(body, "hash") {
		t.Fatalf("response body leaked password material: %s", body)
	}
}

func TestMeResponseContainsNoSessionOrTokenData(t *testing.T) {
	fixture := newMeTestFixture(t)
	_, token := fixture.seedUserWithMembership(t, "owner@example.com", "company-1", "Acme", "owner")

	response := doMeRequest(fixture.router, token)
	body := response.Body.String()
	for _, forbidden := range []string{"refreshToken", "accessToken", "sessionId", "refresh_token"} {
		if contains(body, forbidden) {
			t.Fatalf("response body leaked session/token field %q: %s", forbidden, body)
		}
	}
}

func TestMeWorksForAllThreeRoles(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee"} {
		fixture := newMeTestFixture(t)
		_, token := fixture.seedUserWithMembership(t, role+"@example.com", "company-1", "Acme", role)

		response := doMeRequest(fixture.router, token)
		if response.Code != http.StatusOK {
			t.Fatalf("role %s: status = %d, body %s", role, response.Code, response.Body.String())
		}
		var body struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("role %s: decoding response: %v", role, err)
		}
		if body.Role != role {
			t.Fatalf("role %s: got %q", role, body.Role)
		}
	}
}

func TestMeReflectsMustChangePassword(t *testing.T) {
	fixture := newMeTestFixture(t)
	user, err := fixture.userRepo.Create(context.Background(), User{
		Email: "temp@example.com", PasswordHash: "hash", MustChangePassword: true, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	fixture.membership.byUserID[user.ID] = currentMembershipFixture{companyID: "company-1", companyName: "Acme", role: "employee"}
	token, err := fixture.jwt.IssueAccessToken(user.ID, "company-1", "employee")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	response := doMeRequest(fixture.router, token)
	var body struct {
		MustChangePassword bool `json:"mustChangePassword"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !body.MustChangePassword {
		t.Error("expected mustChangePassword=true")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
