package identity

import (
	"context"
	"errors"
)

// ErrMembershipNotFoundForUser is returned by CurrentMembershipLookup when
// userID has no CompanyMembership at all. Distinct from ErrCurrentUserMismatch
// (the caller-facing error): CurrentUserService collapses this and every
// other authoritative-lookup failure into ErrCurrentUserMismatch so GET
// /auth/me never distinguishes "no membership" from "membership belongs to a
// different company" from "user does not exist" at the HTTP boundary — all
// three are the identical 401, deliberately (design instruction: "missing or
// inconsistent User/Company/Membership returns 401").
var ErrMembershipNotFoundForUser = errors.New("identity: membership not found for user")

// ErrCurrentUserMismatch is returned by CurrentUserService.GetCurrentUser
// when the authoritative User+Membership+Company state cannot be assembled
// consistently for (userID, claimedCompanyID) — including a User that no
// longer exists, a User with no current Membership, and a Membership whose
// CompanyID differs from claimedCompanyID (a stale or tampered access token).
var ErrCurrentUserMismatch = errors.New("identity: current user could not be authoritatively resolved")

// CurrentMembershipLookup is the capability identity needs from companies
// for GET /auth/me: the authoritative current Company (id + name) and Role
// for a User, looked up fresh from the database rather than trusted from a
// JWT claim. Defined here (consumer-defines-interface); satisfied
// structurally by companies.Service. Primitive return values only (ADR
// 0002, matching MembershipLookup's existing convention in
// auth_service.go) — a struct return type would have to name either
// package's own type, which structural interface satisfaction cannot
// bridge across packages.
type CurrentMembershipLookup interface {
	LookupCurrentMembership(ctx context.Context, userID string) (companyID, companyName, role string, err error)
}

// CurrentUser is the GET /auth/me response projection: authoritative User,
// Company, and Role data, with no password hash or session/token material.
type CurrentUser struct {
	UserID             string
	Email              string
	CompanyID          string
	CompanyName        string
	Role               string
	MustChangePassword bool
}

// CurrentUserService assembles the authoritative CurrentUser projection for
// GET /auth/me, re-reading User, Company and Membership from their owning
// services/repositories rather than trusting the caller's JWT claims beyond
// identifying which User and which Company the token claims to represent.
type CurrentUserService struct {
	users      *UserService
	membership CurrentMembershipLookup
}

// NewCurrentUserService constructs a CurrentUserService.
func NewCurrentUserService(users *UserService, membership CurrentMembershipLookup) *CurrentUserService {
	return &CurrentUserService{users: users, membership: membership}
}

// GetCurrentUser resolves the authoritative CurrentUser for userID, requiring
// the User's actual current Membership to belong to claimedCompanyID exactly
// — claimedCompanyID comes from the caller's access-token claims, and this
// check is what stops a stale token (issued before a Membership changed) or
// a tampered claim from producing a response for the wrong company.
//
// Any inconsistency — User not found, no Membership, or Membership.CompanyID
// != claimedCompanyID — collapses to the single ErrCurrentUserMismatch,
// which the HTTP handler maps to 401 uniformly.
func (s *CurrentUserService) GetCurrentUser(ctx context.Context, userID, claimedCompanyID string) (CurrentUser, error) {
	user, err := s.users.FindUserByID(ctx, userID)
	if err != nil {
		return CurrentUser{}, ErrCurrentUserMismatch
	}

	companyID, companyName, role, err := s.membership.LookupCurrentMembership(ctx, userID)
	if err != nil {
		return CurrentUser{}, ErrCurrentUserMismatch
	}
	if companyID != claimedCompanyID {
		return CurrentUser{}, ErrCurrentUserMismatch
	}

	return CurrentUser{
		UserID:             user.ID,
		Email:              user.Email,
		CompanyID:          companyID,
		CompanyName:        companyName,
		Role:               role,
		MustChangePassword: user.MustChangePassword,
	}, nil
}
