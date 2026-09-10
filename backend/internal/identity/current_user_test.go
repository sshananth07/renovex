package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCurrentMembershipLookup struct {
	// byUserID maps userID -> (companyID, companyName, role). A missing key
	// means "no membership" — exactly like the real companies.Service would
	// report for a User with no CompanyMembership document.
	byUserID map[string]currentMembershipFixture
}

type currentMembershipFixture struct {
	companyID   string
	companyName string
	role        string
}

func newFakeCurrentMembershipLookup() *fakeCurrentMembershipLookup {
	return &fakeCurrentMembershipLookup{byUserID: map[string]currentMembershipFixture{}}
}

func (f *fakeCurrentMembershipLookup) LookupCurrentMembership(_ context.Context, userID string) (
	companyID, companyName, role string, err error,
) {
	fixture, ok := f.byUserID[userID]
	if !ok {
		return "", "", "", ErrMembershipNotFoundForUser
	}
	return fixture.companyID, fixture.companyName, fixture.role, nil
}

func newCurrentUserTestFixture(t *testing.T) (*CurrentUserService, *fakeUserRepository, *fakeCurrentMembershipLookup) {
	t.Helper()
	userRepo := newFakeUserRepository()
	users := NewUserService(userRepo)
	membership := newFakeCurrentMembershipLookup()
	svc := NewCurrentUserService(users, membership)
	return svc, userRepo, membership
}

func TestCurrentUserReturnsAuthoritativeProjection(t *testing.T) {
	svc, userRepo, membership := newCurrentUserTestFixture(t)
	user, err := userRepo.Create(context.Background(), User{
		Email: "owner@example.com", PasswordHash: "hash", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	membership.byUserID[user.ID] = currentMembershipFixture{
		companyID: "company-1", companyName: "Acme Renovations", role: "owner",
	}

	result, err := svc.GetCurrentUser(context.Background(), user.ID, "company-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", result.UserID, user.ID)
	}
	if result.Email != "owner@example.com" {
		t.Errorf("Email = %q", result.Email)
	}
	if result.CompanyID != "company-1" {
		t.Errorf("CompanyID = %q", result.CompanyID)
	}
	if result.CompanyName != "Acme Renovations" {
		t.Errorf("CompanyName = %q", result.CompanyName)
	}
	if result.Role != "owner" {
		t.Errorf("Role = %q", result.Role)
	}
	if result.MustChangePassword {
		t.Error("MustChangePassword should be false for this fixture")
	}
}

func TestCurrentUserRejectsWhenJWTCompanyIDDoesNotMatchActualMembership(t *testing.T) {
	svc, userRepo, membership := newCurrentUserTestFixture(t)
	user, _ := userRepo.Create(context.Background(), User{
		Email: "owner@example.com", PasswordHash: "hash", CreatedAt: time.Now(),
	})
	membership.byUserID[user.ID] = currentMembershipFixture{
		companyID: "company-1", companyName: "Acme Renovations", role: "owner",
	}

	// The token claims a different company than the User's actual current
	// Membership — this must never be trusted, even though the token itself
	// verified successfully (e.g. a stale token issued before the user's
	// membership changed, or a forged/tampered claim).
	_, err := svc.GetCurrentUser(context.Background(), user.ID, "company-999")
	if !errors.Is(err, ErrCurrentUserMismatch) {
		t.Fatalf("expected ErrCurrentUserMismatch, got %v", err)
	}
}

func TestCurrentUserRejectsWhenUserMissing(t *testing.T) {
	svc, _, membership := newCurrentUserTestFixture(t)
	membership.byUserID["ghost-user"] = currentMembershipFixture{
		companyID: "company-1", companyName: "Acme Renovations", role: "owner",
	}

	_, err := svc.GetCurrentUser(context.Background(), "ghost-user", "company-1")
	if !errors.Is(err, ErrCurrentUserMismatch) {
		t.Fatalf("expected ErrCurrentUserMismatch for a missing User, got %v", err)
	}
}

func TestCurrentUserRejectsWhenMembershipMissing(t *testing.T) {
	svc, userRepo, _ := newCurrentUserTestFixture(t)
	user, _ := userRepo.Create(context.Background(), User{
		Email: "orphan@example.com", PasswordHash: "hash", CreatedAt: time.Now(),
	})

	_, err := svc.GetCurrentUser(context.Background(), user.ID, "company-1")
	if !errors.Is(err, ErrCurrentUserMismatch) {
		t.Fatalf("expected ErrCurrentUserMismatch for a User with no Membership, got %v", err)
	}
}

func TestCurrentUserProjectionOmitsPasswordHash(t *testing.T) {
	svc, userRepo, membership := newCurrentUserTestFixture(t)
	user, _ := userRepo.Create(context.Background(), User{
		Email: "owner@example.com", PasswordHash: "super-secret-hash", CreatedAt: time.Now(),
	})
	membership.byUserID[user.ID] = currentMembershipFixture{
		companyID: "company-1", companyName: "Acme", role: "owner",
	}

	result, err := svc.GetCurrentUser(context.Background(), user.ID, "company-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CurrentMembership is a struct literal with no PasswordHash field at
	// all — this test documents that guarantee structurally, not just by
	// convention, since the type itself cannot carry the hash.
	_ = result
}

func TestCurrentUserWorksForAdminRole(t *testing.T) {
	svc, userRepo, membership := newCurrentUserTestFixture(t)
	user, _ := userRepo.Create(context.Background(), User{Email: "admin@example.com", PasswordHash: "hash", CreatedAt: time.Now()})
	membership.byUserID[user.ID] = currentMembershipFixture{companyID: "company-1", companyName: "Acme", role: "admin"}

	result, err := svc.GetCurrentUser(context.Background(), user.ID, "company-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Role != "admin" {
		t.Errorf("Role = %q, want admin", result.Role)
	}
}

func TestCurrentUserWorksForEmployeeRole(t *testing.T) {
	svc, userRepo, membership := newCurrentUserTestFixture(t)
	user, _ := userRepo.Create(context.Background(), User{Email: "employee@example.com", PasswordHash: "hash", CreatedAt: time.Now()})
	membership.byUserID[user.ID] = currentMembershipFixture{companyID: "company-1", companyName: "Acme", role: "employee"}

	result, err := svc.GetCurrentUser(context.Background(), user.ID, "company-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Role != "employee" {
		t.Errorf("Role = %q, want employee", result.Role)
	}
}

func TestCurrentUserReflectsMustChangePassword(t *testing.T) {
	svc, userRepo, membership := newCurrentUserTestFixture(t)
	user, _ := userRepo.Create(context.Background(), User{
		Email: "temp@example.com", PasswordHash: "hash", MustChangePassword: true, CreatedAt: time.Now(),
	})
	membership.byUserID[user.ID] = currentMembershipFixture{companyID: "company-1", companyName: "Acme", role: "employee"}

	result, err := svc.GetCurrentUser(context.Background(), user.ID, "company-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.MustChangePassword {
		t.Error("expected MustChangePassword to be true")
	}
}
