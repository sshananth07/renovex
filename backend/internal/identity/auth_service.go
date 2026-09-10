package identity

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidCredentials is returned by Login for either an unknown email or a
// wrong password — deliberately identical in both cases so the caller cannot
// distinguish which one was wrong.
var ErrInvalidCredentials = errors.New("identity: invalid credentials")

// CompanyProvisioner is the capability identity needs from companies: creating
// an owner company during registration, and deleting a company this package's
// registration flow itself provisioned (compensation only). Defined here
// (consumer-defines-interface); satisfied structurally by companies.Service
// with no import in either direction. Both methods use primitive string types.
type CompanyProvisioner interface {
	CreateOwnerCompany(ctx context.Context, userID, companyName string) (companyID string, err error)
	DeleteProvisionedCompany(ctx context.Context, companyID, ownerUserID string) error
}

// MembershipLookup is the capability identity needs from companies: reading a
// user's current company and role. Defined here; satisfied structurally by
// companies.Service.
type MembershipLookup interface {
	FindMembershipByUserID(ctx context.Context, userID string) (companyID string, role string, err error)
}

// AuthResult is returned by Register, Login, and Refresh: an access token for
// the JSON response body, and a raw refresh token for the handler layer to set
// as an httpOnly cookie (never returned in the JSON body itself).
type AuthResult struct {
	AccessToken        string
	RefreshToken       string
	MustChangePassword bool
}

// RoleOwnerString is the literal "owner" role value assigned to a Register
// caller. Kept as a named constant (rather than importing companies.RoleOwner,
// which would violate the zero-cross-import boundary) so the literal has a
// single source of truth within identity.
const RoleOwnerString = "owner"

// AuthService owns authentication use cases: Register, Login, Refresh, Logout,
// and AuthSession lifecycle / access-refresh token issuance.
type AuthService struct {
	users            *UserService
	sessions         AuthSessionRepository
	companyProv      CompanyProvisioner
	membershipLookup MembershipLookup
	jwt              *JWTIssuer
	refreshTokenTTL  time.Duration
}

// NewAuthService constructs an AuthService.
func NewAuthService(users *UserService, sessions AuthSessionRepository, companyProv CompanyProvisioner, membershipLookup MembershipLookup, jwtIssuer *JWTIssuer, refreshTokenTTL time.Duration) *AuthService {
	return &AuthService{
		users: users, sessions: sessions, companyProv: companyProv,
		membershipLookup: membershipLookup, jwt: jwtIssuer, refreshTokenTTL: refreshTokenTTL,
	}
}

// Register creates a User, an owner Company + Membership, and an AuthSession,
// then issues tokens. On any failure after the User is created, previously
// created resources are compensated in reverse order (see design doc §9).
func (s *AuthService) Register(ctx context.Context, email, password, companyName string) (AuthResult, error) {
	user, err := s.users.CreateUser(ctx, email, password)
	if err != nil {
		return AuthResult{}, err // duplicate email or hashing failure: nothing to compensate
	}

	companyID, err := s.companyProv.CreateOwnerCompany(ctx, user.ID, companyName)
	if err != nil {
		_ = s.users.DeleteUser(ctx, user.ID) // best-effort compensation; failure is not surfaced to caller in M1
		return AuthResult{}, err
	}

	result, err := s.createSessionAndIssueTokens(ctx, user.ID, companyID, RoleOwnerString)
	if err != nil {
		_ = s.companyProv.DeleteProvisionedCompany(ctx, companyID, user.ID)
		_ = s.users.DeleteUser(ctx, user.ID)
		return AuthResult{}, err
	}
	result.MustChangePassword = user.MustChangePassword
	return result, nil
}

// Login verifies email/password and issues tokens. Unknown email and wrong
// password both return the identical ErrInvalidCredentials.
func (s *AuthService) Login(ctx context.Context, email, password string) (AuthResult, error) {
	user, err := s.users.FindUserByEmail(ctx, email)
	if err != nil {
		return AuthResult{}, ErrInvalidCredentials
	}
	if err := VerifyPassword(user.PasswordHash, password); err != nil {
		return AuthResult{}, ErrInvalidCredentials
	}

	companyID, role, err := s.membershipLookup.FindMembershipByUserID(ctx, user.ID)
	if err != nil {
		return AuthResult{}, err
	}

	result, err := s.createSessionAndIssueTokens(ctx, user.ID, companyID, role)
	if err != nil {
		return AuthResult{}, err
	}
	result.MustChangePassword = user.MustChangePassword
	return result, nil
}

// Refresh validates rawRefreshToken against an active AuthSession, re-reads
// the user's current company/role, rotates the session's refresh token, and
// issues a fresh access token. The pre-rotation token is rejected by any
// subsequent call (its hash no longer matches any session).
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (AuthResult, error) {
	hash := HashRefreshToken(rawRefreshToken)
	session, err := s.sessions.FindActiveByHash(ctx, hash)
	if err != nil {
		return AuthResult{}, err
	}

	companyID, role, err := s.membershipLookup.FindMembershipByUserID(ctx, session.UserID)
	if err != nil {
		return AuthResult{}, err
	}

	newRawToken, err := GenerateRefreshToken()
	if err != nil {
		return AuthResult{}, err
	}
	newHash := HashRefreshToken(newRawToken)
	if err := s.sessions.RotateHash(ctx, session.ID, newHash, time.Now()); err != nil {
		return AuthResult{}, err
	}

	accessToken, err := s.jwt.IssueAccessToken(session.UserID, companyID, role)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{AccessToken: accessToken, RefreshToken: newRawToken}, nil
}

// Logout revokes the AuthSession identified by rawRefreshToken. After logout,
// the token no longer resolves via FindActiveByHash.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	hash := HashRefreshToken(rawRefreshToken)
	session, err := s.sessions.FindActiveByHash(ctx, hash)
	if err != nil {
		return err
	}
	return s.sessions.Revoke(ctx, session.ID, time.Now())
}

// DeleteAllSessionsForUser permanently removes every AuthSession owned by
// userID. Development-tool use only (demoseed reset, design spec §6.6) — no
// production code path calls this. Idempotent: calling it when nothing
// remains for userID is a no-op success, not an error.
//
// This does NOT satisfy demoseed.CleanupService (different method
// name/parameter semantics: user-scoped, not company-scoped) — it is called
// directly and separately, scoped by the manifest's demoUserID, before the
// User itself is deleted (design spec §6.4).
func (s *AuthService) DeleteAllSessionsForUser(ctx context.Context, userID string) error {
	return s.sessions.DeleteAllForUser(ctx, userID)
}

func (s *AuthService) createSessionAndIssueTokens(ctx context.Context, userID, companyID, role string) (AuthResult, error) {
	rawRefreshToken, err := GenerateRefreshToken()
	if err != nil {
		return AuthResult{}, err
	}
	now := time.Now()
	_, err = s.sessions.Create(ctx, AuthSession{
		UserID:           userID,
		RefreshTokenHash: HashRefreshToken(rawRefreshToken),
		CreatedAt:        now,
		ExpiresAt:        now.Add(s.refreshTokenTTL),
		LastUsedAt:       now,
	})
	if err != nil {
		return AuthResult{}, err
	}

	accessToken, err := s.jwt.IssueAccessToken(userID, companyID, role)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{AccessToken: accessToken, RefreshToken: rawRefreshToken}, nil
}
