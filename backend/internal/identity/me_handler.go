package identity

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type meOutput struct {
	Body struct {
		UserID             string `json:"userId"`
		Email              string `json:"email"`
		CompanyID          string `json:"companyId"`
		CompanyName        string `json:"companyName"`
		Role               string `json:"role"`
		MustChangePassword bool   `json:"mustChangePassword"`
	}
}

// RegisterMeHandler registers GET /auth/me on api, backed by currentUserSvc.
// api must already require a valid bearer token (mount this on the same
// authedAPI group every other authenticated contractor route uses, via
// identity.RequireAuthHuma) — unlike /auth/register, /auth/login,
// /auth/refresh, and /auth/logout, which are registered separately by
// RegisterHandlers on the unauthenticated base API.
//
// No request ID (userId/companyId) is ever accepted from the caller: both
// come only from the verified access-token Principal already in context, and
// CurrentUserService re-derives the authoritative Company/Role from the
// database rather than trusting the token's claims beyond identifying which
// User and which Company to look up.
func RegisterMeHandler(api huma.API, currentUserSvc *CurrentUserService) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/auth/me",
		Summary:     "Get the authenticated user's current profile, company, and role",
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		principal, ok := PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		current, err := currentUserSvc.GetCurrentUser(ctx, principal.UserID, principal.CompanyID)
		if err != nil {
			if errors.Is(err, ErrCurrentUserMismatch) {
				return nil, huma.Error401Unauthorized("current user could not be resolved")
			}
			return nil, err
		}

		resp := &meOutput{}
		resp.Body.UserID = current.UserID
		resp.Body.Email = current.Email
		resp.Body.CompanyID = current.CompanyID
		resp.Body.CompanyName = current.CompanyName
		resp.Body.Role = current.Role
		resp.Body.MustChangePassword = current.MustChangePassword
		return resp, nil
	})
}
