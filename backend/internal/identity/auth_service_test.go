package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSessionRepo struct {
	byID   map[string]AuthSession
	byHash map[string]string // hash -> sessionID
	nextID int
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{byID: map[string]AuthSession{}, byHash: map[string]string{}}
}

func (f *fakeSessionRepo) Create(_ context.Context, s AuthSession) (AuthSession, error) {
	f.nextID++
	s.ID = string(rune('a' + f.nextID))
	f.byID[s.ID] = s
	f.byHash[s.RefreshTokenHash] = s.ID
	return s, nil
}

func (f *fakeSessionRepo) FindActiveByHash(_ context.Context, hash string) (AuthSession, error) {
	id, ok := f.byHash[hash]
	if !ok {
		return AuthSession{}, ErrSessionNotFound
	}
	s := f.byID[id]
	if !s.IsActive(time.Now()) {
		return AuthSession{}, ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeSessionRepo) RotateHash(_ context.Context, sessionID, newHash string, lastUsedAt time.Time) error {
	s, ok := f.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	delete(f.byHash, s.RefreshTokenHash)
	s.RefreshTokenHash = newHash
	s.LastUsedAt = lastUsedAt
	f.byID[sessionID] = s
	f.byHash[newHash] = sessionID
	return nil
}

func (f *fakeSessionRepo) Revoke(_ context.Context, sessionID string, revokedAt time.Time) error {
	s, ok := f.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.RevokedAt = &revokedAt
	f.byID[sessionID] = s
	return nil
}

func (f *fakeSessionRepo) DeleteAllForUser(_ context.Context, userID string) error {
	for id, s := range f.byID {
		if s.UserID == userID {
			delete(f.byHash, s.RefreshTokenHash)
			delete(f.byID, id)
		}
	}
	return nil
}

type fakeCompanyProvisioner struct {
	created                map[string]bool // companyID -> exists
	nextID                 int
	failCreateOwnerCompany bool
}

func newFakeCompanyProvisioner() *fakeCompanyProvisioner {
	return &fakeCompanyProvisioner{created: map[string]bool{}}
}

func (f *fakeCompanyProvisioner) CreateOwnerCompany(_ context.Context, userID, companyName string) (string, error) {
	if f.failCreateOwnerCompany {
		return "", errors.New("simulated failure")
	}
	f.nextID++
	id := string(rune('A' + f.nextID))
	f.created[id] = true
	return id, nil
}

func (f *fakeCompanyProvisioner) DeleteProvisionedCompany(_ context.Context, companyID, ownerUserID string) error {
	delete(f.created, companyID)
	return nil
}

func (f *fakeCompanyProvisioner) FindMembershipByUserID(_ context.Context, userID string) (string, string, error) {
	for id := range f.created {
		return id, "owner", nil // simplistic: assumes single-company test scenarios
	}
	return "", "", errors.New("no membership")
}

func TestAuthServiceRegisterCreatesUserCompanyAndSession(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)

	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)

	result, err := authSvc.Register(context.Background(), "owner@example.com", "password123", "Acme Renovations")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
	if result.RefreshToken == "" {
		t.Fatal("expected non-empty raw refresh token for cookie-setting")
	}

	claims, err := issuer.VerifyAccessToken(result.AccessToken)
	if err != nil {
		t.Fatalf("unexpected error verifying issued token: %v", err)
	}
	if claims.Role != "owner" {
		t.Fatalf("expected role owner, got %s", claims.Role)
	}
}

func TestAuthServiceRegisterDuplicateEmailRejected(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	_, err := authSvc.Register(ctx, "dup@example.com", "password123", "Company A")
	if err != nil {
		t.Fatalf("unexpected error on first register: %v", err)
	}

	_, err = authSvc.Register(ctx, "dup@example.com", "password123", "Company B")
	if err != ErrDuplicateEmail {
		t.Fatalf("expected ErrDuplicateEmail, got %v", err)
	}
}

func TestAuthServiceRegisterCompensatesOnCompanyCreationFailure(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	companyProv.failCreateOwnerCompany = true
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	_, err := authSvc.Register(ctx, "compensate@example.com", "password123", "Doomed Co")
	if err == nil {
		t.Fatal("expected registration to fail")
	}

	_, findErr := userSvc.FindUserByEmail(ctx, "compensate@example.com")
	if findErr != ErrUserNotFound {
		t.Fatalf("expected user to be compensated (deleted), got %v", findErr)
	}
}

func TestAuthServiceLoginSucceedsWithValidCredentials(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	_, err := authSvc.Register(ctx, "login@example.com", "correct-password", "Acme")
	if err != nil {
		t.Fatalf("unexpected error registering: %v", err)
	}

	result, err := authSvc.Login(ctx, "login@example.com", "correct-password")
	if err != nil {
		t.Fatalf("unexpected error logging in: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
}

func TestAuthServiceLoginWrongPasswordRejectedGenerically(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	_, err := authSvc.Register(ctx, "wrongpw@example.com", "correct-password", "Acme")
	if err != nil {
		t.Fatalf("unexpected error registering: %v", err)
	}

	_, wrongPassErr := authSvc.Login(ctx, "wrongpw@example.com", "incorrect-password")
	_, unknownEmailErr := authSvc.Login(ctx, "doesnotexist@example.com", "whatever")

	if wrongPassErr != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for wrong password, got %v", wrongPassErr)
	}
	if unknownEmailErr != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for unknown email, got %v", unknownEmailErr)
	}
}

func TestAuthServiceRefreshRotatesTokenAndRejectsOldOne(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	registerResult, err := authSvc.Register(ctx, "refresh@example.com", "password123", "Acme")
	if err != nil {
		t.Fatalf("unexpected error registering: %v", err)
	}

	refreshResult, err := authSvc.Refresh(ctx, registerResult.RefreshToken)
	if err != nil {
		t.Fatalf("unexpected error refreshing: %v", err)
	}
	if refreshResult.RefreshToken == registerResult.RefreshToken {
		t.Fatal("expected a rotated (different) refresh token")
	}

	_, err = authSvc.Refresh(ctx, registerResult.RefreshToken)
	if err != ErrSessionNotFound {
		t.Fatalf("expected the pre-rotation token to be rejected, got %v", err)
	}

	_, err = authSvc.Refresh(ctx, refreshResult.RefreshToken)
	if err != nil {
		t.Fatalf("expected the rotated token to still work, got %v", err)
	}
}

func TestAuthServiceLogoutRevokesSession(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	registerResult, err := authSvc.Register(ctx, "logout@example.com", "password123", "Acme")
	if err != nil {
		t.Fatalf("unexpected error registering: %v", err)
	}

	if err := authSvc.Logout(ctx, registerResult.RefreshToken); err != nil {
		t.Fatalf("unexpected error logging out: %v", err)
	}

	_, err = authSvc.Refresh(ctx, registerResult.RefreshToken)
	if err != ErrSessionNotFound {
		t.Fatalf("expected revoked token to be rejected, got %v", err)
	}
}

func TestAuthServiceDeleteAllSessionsForUser(t *testing.T) {
	userRepo := newFakeUserRepository()
	userSvc := NewUserService(userRepo)
	sessionRepo := newFakeSessionRepo()
	companyProv := newFakeCompanyProvisioner()
	issuer := NewJWTIssuer([]byte("test-secret"), time.Minute)
	authSvc := NewAuthService(userSvc, sessionRepo, companyProv, companyProv, issuer, 7*24*time.Hour)
	ctx := context.Background()

	registerResult, err := authSvc.Register(ctx, "delete-sessions@example.com", "password123", "Acme")
	if err != nil {
		t.Fatalf("unexpected error registering: %v", err)
	}
	user, err := userRepo.FindByEmail(ctx, "delete-sessions@example.com")
	if err != nil {
		t.Fatalf("unexpected error finding registered user: %v", err)
	}

	if err := authSvc.DeleteAllSessionsForUser(ctx, user.ID); err != nil {
		t.Fatalf("unexpected error deleting sessions: %v", err)
	}

	_, err = authSvc.Refresh(ctx, registerResult.RefreshToken)
	if err != ErrSessionNotFound {
		t.Fatalf("expected deleted session's token to be rejected, got %v", err)
	}

	if err := authSvc.DeleteAllSessionsForUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteAllSessionsForUser called twice should still succeed, got: %v", err)
	}
}
