package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

const refreshCookieName = "refresh_token"

type registerInput struct {
	Body struct {
		Email       string `json:"email" required:"true" format:"email"`
		Password    string `json:"password" required:"true" minLength:"8"`
		CompanyName string `json:"companyName" required:"true" minLength:"1"`
	}
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" required:"true" format:"email"`
		Password string `json:"password" required:"true"`
	}
}

type refreshInput struct {
	RefreshToken http.Cookie `cookie:"refresh_token"`
	Origin       string      `header:"Origin"`
	SecFetchSite string      `header:"Sec-Fetch-Site"`
}

type logoutInput struct {
	RefreshToken http.Cookie `cookie:"refresh_token"`
	Origin       string      `header:"Origin"`
	SecFetchSite string      `header:"Sec-Fetch-Site"`
}

type authOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      struct {
		AccessToken        string `json:"accessToken"`
		MustChangePassword bool   `json:"mustChangePassword"`
	}
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

// RegisterHandlers registers /auth/register, /auth/login, /auth/refresh, and
// /auth/logout operations on api, backed by authSvc. secureRefreshCookie
// controls the refresh-token cookie's Secure attribute — the caller (cmd/api,
// tenanttest) supplies this from configuration rather than the handler
// reading an environment variable directly, so the policy is testable
// without process-global state.
//
// allowedOrigins gates /auth/refresh and /auth/logout only — the two routes
// that act on the refresh cookie. /auth/register and /auth/login carry no
// cookie credential to protect and remain reachable from any origin (CORS
// still applies at the transport layer for browser callers, but the origin
// guard here is specifically about protecting cookie-based session
// mutation, per platformhttp.DecideOriginGuard's contract).
func RegisterHandlers(api huma.API, authSvc *AuthService, refreshCookieMaxAge int, secureRefreshCookie bool, allowedOrigins config.AllowedOrigins) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-register",
		Method:      http.MethodPost,
		Path:        "/auth/register",
		Summary:     "Register a new user, company, and owner membership",
	}, func(ctx context.Context, input *registerInput) (*authOutput, error) {
		result, err := authSvc.Register(ctx, input.Body.Email, input.Body.Password, input.Body.CompanyName)
		if err != nil {
			return nil, mapAuthError(err)
		}
		return buildAuthOutput(result, refreshCookieMaxAge, secureRefreshCookie), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        "/auth/login",
		Summary:     "Authenticate and start a new session",
	}, func(ctx context.Context, input *loginInput) (*authOutput, error) {
		result, err := authSvc.Login(ctx, input.Body.Email, input.Body.Password)
		if err != nil {
			return nil, mapAuthError(err)
		}
		return buildAuthOutput(result, refreshCookieMaxAge, secureRefreshCookie), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "auth-refresh",
		Method:      http.MethodPost,
		Path:        "/auth/refresh",
		Summary:     "Rotate the refresh session and issue a new access token",
	}, func(ctx context.Context, input *refreshInput) (*authOutput, error) {
		if !platformhttp.DecideOriginGuard(allowedOrigins, input.Origin, input.SecFetchSite) {
			return nil, huma.Error403Forbidden("origin not permitted")
		}
		result, err := authSvc.Refresh(ctx, input.RefreshToken.Value)
		if err != nil {
			resp := &authOutput{}
			resp.SetCookie = expiredRefreshCookie(secureRefreshCookie)
			return resp, mapAuthError(err)
		}
		return buildAuthOutput(result, refreshCookieMaxAge, secureRefreshCookie), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "auth-logout",
		Method:      http.MethodPost,
		Path:        "/auth/logout",
		Summary:     "Revoke the current session",
	}, func(ctx context.Context, input *logoutInput) (*logoutOutput, error) {
		if !platformhttp.DecideOriginGuard(allowedOrigins, input.Origin, input.SecFetchSite) {
			return nil, huma.Error403Forbidden("origin not permitted")
		}
		_ = authSvc.Logout(ctx, input.RefreshToken.Value) // idempotent from caller's view
		return &logoutOutput{SetCookie: expiredRefreshCookie(secureRefreshCookie)}, nil
	})
}

type sendRegistrationVerificationInput struct {
	Body struct {
		Email string `json:"email" required:"true" format:"email"`
	}
}

type sendRegistrationVerificationOutput struct {
	Body struct {
		ExpiresAt time.Time `json:"expiresAt"`
	}
}

type verifyRegistrationCodeInput struct {
	Body struct {
		Email string `json:"email" required:"true" format:"email"`
		Code  string `json:"code" required:"true" minLength:"6" maxLength:"6"`
	}
}

// RegisterRegistrationVerificationHandlers registers
// /auth/registration-verification/send and /auth/registration-verification/verify
// on api, backed by svc (T2B). Both routes are deliberately unauthenticated
// — they exist to establish trust in an email address BEFORE a session can
// be created, mirroring /auth/register and /auth/login's own "no cookie
// credential to protect yet" posture. Every failure (unknown recipient,
// wrong code, expired, already consumed, attempt limit, rate limited) maps
// to the SAME public response shape so a caller cannot distinguish which
// case occurred (T2B: "do not leak whether unrelated accounts/users
// exist").
func RegisterRegistrationVerificationHandlers(api huma.API, svc *RegistrationVerificationService) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-registration-verification-send",
		Method:      http.MethodPost,
		Path:        "/auth/registration-verification/send",
		Summary:     "Send (or resend) a registration email verification code",
	}, func(ctx context.Context, input *sendRegistrationVerificationInput) (*sendRegistrationVerificationOutput, error) {
		result, err := svc.SendRegistrationVerification(ctx, input.Body.Email, time.Now())
		if err != nil {
			return nil, mapRegistrationVerificationError(err)
		}
		resp := &sendRegistrationVerificationOutput{}
		resp.Body.ExpiresAt = result.ExpiresAt
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "auth-registration-verification-verify",
		Method:      http.MethodPost,
		Path:        "/auth/registration-verification/verify",
		Summary:     "Verify a registration email verification code",
	}, func(ctx context.Context, input *verifyRegistrationCodeInput) (*struct{}, error) {
		if err := svc.VerifyRegistrationCode(ctx, input.Body.Email, input.Body.Code, time.Now()); err != nil {
			return nil, mapRegistrationVerificationError(err)
		}
		return &struct{}{}, nil
	})
}

// mapRegistrationVerificationError maps every RegistrationVerification
// sentinel to a Huma HTTP error. ErrRegistrationVerificationRateLimited is
// the only case surfaced distinctly (429) — a caller legitimately needs to
// know "wait and retry" is different from "that code is wrong", and rate
// limiting reveals nothing about account existence. Every other failure
// collapses to the same 422 with no distinguishing detail.
func mapRegistrationVerificationError(err error) error {
	switch {
	case errors.Is(err, ErrRegistrationVerificationRateLimited):
		return huma.Error429TooManyRequests("please wait before requesting another code")
	default:
		return huma.Error422UnprocessableEntity("invalid or expired verification code")
	}
}

func buildAuthOutput(result AuthResult, maxAge int, secure bool) *authOutput {
	resp := &authOutput{}
	resp.Body.AccessToken = result.AccessToken
	resp.Body.MustChangePassword = result.MustChangePassword
	resp.SetCookie = http.Cookie{
		Name:     refreshCookieName,
		Value:    result.RefreshToken,
		HttpOnly: true,
		Secure:   secure,
		Path:     "/auth",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	return resp
}

func expiredRefreshCookie(secure bool) http.Cookie {
	return http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   secure,
		Path:     "/auth",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
}

func mapAuthError(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateEmail):
		return huma.Error409Conflict("email already registered")
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("invalid email or password")
	case errors.Is(err, ErrSessionNotFound):
		return huma.Error401Unauthorized("session expired or revoked")
	case errors.Is(err, ErrUserNotFound):
		return huma.Error401Unauthorized("invalid credentials")
	default:
		return err
	}
}
