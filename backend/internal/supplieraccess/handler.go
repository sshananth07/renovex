package supplieraccess

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

const (
	AccessExchangeCookieName  = "supplier_access_exchange"
	SupplierSessionCookieName = "supplier_session"
	SupplierCSRFCookieName    = "supplier_csrf"
	supplierAccessCookiePath  = "/supplier-access"
	supplierNoStore           = platformhttp.ExternalNoStore
)

type supplierRequestNetworkKey struct{}

type supplierRequestNetwork struct {
	remoteAddress string
	forwardedFor  string
}

type openInvitationInput struct {
	// The token is optional at schema level because the clean destination uses
	// the same path. Its presence selects the one-time exchange branch.
	Token string `query:"token"`
}

type cleanOpenBody struct {
	Status string `json:"status"`
}

type openInvitationOutput struct {
	Status         int
	Location       string      `header:"Location"`
	SetCookie      http.Cookie `header:"Set-Cookie"`
	CacheControl   string      `header:"Cache-Control"`
	Pragma         string      `header:"Pragma"`
	ReferrerPolicy string      `header:"Referrer-Policy"`
	Body           *cleanOpenBody
}

type createChallengeInput struct {
	ExchangeCookie http.Cookie `cookie:"supplier_access_exchange"`
	Body           struct {
		OperationID string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type resendChallengeInput struct {
	Body struct {
		ChallengeID string `json:"challengeId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
		OperationID string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type verifyChallengeInput struct {
	SessionCookie http.Cookie `cookie:"supplier_session"`
	Body          struct {
		ChallengeID string `json:"challengeId" required:"true" maxLength:"128" pattern:"^[!-~]+$"`
		Code        string `json:"code" required:"true"`
		OperationID string `json:"operationId" required:"true" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type logoutSupplierSessionInput struct {
	SessionCookie http.Cookie `cookie:"supplier_session"`
	CSRFCookie    http.Cookie `cookie:"supplier_csrf"`
	CSRFHeader    string      `header:"X-CSRF-Token"`
}

type bootstrapSupplierSessionInput struct {
	SessionCookie http.Cookie `cookie:"supplier_session"`
}

type verificationChallengeBody struct {
	ChallengeID    string                     `json:"challengeId"`
	ExpiresAt      time.Time                  `json:"expiresAt"`
	DeliveryStatus VerificationDeliveryStatus `json:"deliveryStatus"`
}

type verificationChallengeOutput struct {
	Status         int
	SetCookie      http.Cookie `header:"Set-Cookie"`
	CacheControl   string      `header:"Cache-Control"`
	Pragma         string      `header:"Pragma"`
	ReferrerPolicy string      `header:"Referrer-Policy"`
	Body           *verificationChallengeBody
}

type verifiedOutput struct {
	Status         int
	SetCookie      []http.Cookie `header:"Set-Cookie"`
	CacheControl   string        `header:"Cache-Control"`
	Pragma         string        `header:"Pragma"`
	ReferrerPolicy string        `header:"Referrer-Policy"`
	Body           map[string]string
}

type logoutSupplierSessionOutput struct {
	Status         int
	SetCookie      []http.Cookie `header:"Set-Cookie"`
	CacheControl   string        `header:"Cache-Control"`
	Pragma         string        `header:"Pragma"`
	ReferrerPolicy string        `header:"Referrer-Policy"`
}

type bootstrapSupplierSessionOutput struct {
	Status         int
	SetCookie      http.Cookie `header:"Set-Cookie"`
	CacheControl   string      `header:"Cache-Control"`
	Pragma         string      `header:"Pragma"`
	ReferrerPolicy string      `header:"Referrer-Policy"`
	Body           map[string]string
}

// RegisterHandlers mounts Phase D's public Supplier security routes. Challenge
// handles stay in request bodies so ordinary proxy-path logs never capture
// them.
// sameSite is the SameSite policy applied to every Supplier Access cookie
// (exchange, session, CSRF). It must match identity's RefreshCookieSameSite
// convention: Renovex's tester/production topology serves Web and the Go API
// from two different Vercel domains (renovex-web.vercel.app,
// renovex-api.vercel.app), which browsers treat as cross-site. A SameSite=Lax
// cookie is never sent on the cross-site fetch() calls Web makes to the API,
// so a hardcoded Lax cookie here would silently break every Supplier Access
// flow in that exact topology while still working in same-site local dev —
// which is why this must be configurable rather than fixed.
func RegisterHandlers(api huma.API, service *Service, secureCookies bool, sameSite http.SameSite) {
	supplierAPI := platformhttp.NewExternalGroup(api)
	supplierAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		// Huma handlers receive context.Context rather than *http.Request.
		// Capture only the network metadata needed by the rate limiter.
		next(huma.WithValue(ctx, supplierRequestNetworkKey{},
			supplierRequestNetwork{
				remoteAddress: ctx.RemoteAddr(),
				forwardedFor:  ctx.Header("X-Forwarded-For"),
			}))
	})

	// Award outcome routes mount on the SAME group, so they inherit the
	// no-store, no-cache, no-referrer boundary every Phase D response uses
	// (§8H, §8J).
	registerOutcomeHandlers(supplierAPI, service)

	huma.Register(supplierAPI, huma.Operation{
		OperationID: "supplier-access-open",
		Method:      http.MethodGet,
		Path:        "/supplier-access/open",
		Summary:     "Exchange an invitation link for a short-lived browser credential",
		Security:    platformhttp.PublicSecurityRequirements(),
	}, func(ctx context.Context,
		input *openInvitationInput) (*openInvitationOutput, error) {

		if input.Token == "" {
			return &openInvitationOutput{
				Status:         http.StatusOK,
				CacheControl:   supplierNoStore,
				Pragma:         "no-cache",
				ReferrerPolicy: "no-referrer",
				Body:           &cleanOpenBody{Status: "verification_required"},
			}, nil
		}

		openedAt := time.Now().UTC()
		result, err := service.OpenInvitation(ctx, input.Token, openedAt)
		if err != nil {
			return nil, mapSupplierAccessPublicError(err)
		}
		return &openInvitationOutput{
			Status:         http.StatusSeeOther,
			Location:       "/supplier-access/open",
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			SetCookie: http.Cookie{
				Name:     AccessExchangeCookieName,
				Value:    result.ExchangeToken,
				Path:     supplierAccessCookiePath,
				Domain:   "",
				Expires:  result.ExpiresAt,
				MaxAge:   int(result.ExpiresAt.Sub(openedAt) / time.Second),
				HttpOnly: true,
				Secure:   secureCookies,
				SameSite: sameSite,
			},
		}, nil
	})

	huma.Register(supplierAPI, huma.Operation{
		OperationID: "supplier-access-create-challenge",
		Method:      http.MethodPost,
		Path:        "/supplier-access/challenges",
		Summary:     "Request a verification code for an invitation exchange",
		Security:    platformhttp.SupplierExchangeSecurityRequirements(),
	}, func(ctx context.Context,
		input *createChallengeInput) (*verificationChallengeOutput, error) {

		network, _ := ctx.Value(supplierRequestNetworkKey{}).(supplierRequestNetwork)
		clientAddress, err := ResolveSupplierClientAddress(
			network.remoteAddress, network.forwardedFor,
			service.trustedProxyCIDRs)
		if err != nil {
			return nil, mapSupplierAccessPublicError(
				ErrInvalidVerificationRequest)
		}
		requestedAt := time.Now().UTC()
		result, err := service.CreateChallenge(ctx, CreateChallengeInput{
			ExchangeToken: input.ExchangeCookie.Value,
			OperationID:   input.Body.OperationID,
			ClientAddress: clientAddress,
			RequestedAt:   requestedAt,
		})
		if err != nil {
			if errors.Is(err, ErrVerificationMailDeliveryFailed) &&
				result.ChallengeID != "" {
				// The challenge and failed delivery are authoritative even
				// though the provider is unavailable. Return the opaque handle
				// so an explicit resend can recover without restoring the
				// single-use exchange.
				return &verificationChallengeOutput{
					Status:         http.StatusServiceUnavailable,
					SetCookie:      expiredAccessExchangeCookie(secureCookies, sameSite),
					CacheControl:   supplierNoStore,
					Pragma:         "no-cache",
					ReferrerPolicy: "no-referrer",
					Body:           challengeResponseBody(result),
				}, nil
			}
			publicError := mapSupplierAccessPublicError(err)
			if errors.Is(err, ErrInvalidSupplierCredential) ||
				errors.Is(err, ErrAccessExchangeNotFound) ||
				errors.Is(err, ErrAccessExchangeConsumed) {
				// An invalid or expired exchange cannot become usable later;
				// remove its browser credential while preserving the same
				// neutral public response used for every credential failure.
				expiredCookie := expiredAccessExchangeCookie(secureCookies, sameSite)
				publicError = huma.ErrorWithHeaders(publicError, http.Header{
					"Set-Cookie": []string{expiredCookie.String()},
				})
			}
			return nil, publicError
		}
		return &verificationChallengeOutput{
			Status:         http.StatusCreated,
			SetCookie:      expiredAccessExchangeCookie(secureCookies, sameSite),
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			Body:           challengeResponseBody(result),
		}, nil
	})

	huma.Register(supplierAPI, huma.Operation{
		OperationID: "supplier-access-resend-challenge",
		Method:      http.MethodPost,
		Path:        "/supplier-access/challenges/resend",
		Summary:     "Resend the current verification challenge",
		Security:    platformhttp.PublicSecurityRequirements(),
	}, func(ctx context.Context,
		input *resendChallengeInput) (*verificationChallengeOutput, error) {

		result, err := service.ResendChallenge(ctx, ResendChallengeInput{
			ChallengeID: input.Body.ChallengeID,
			OperationID: input.Body.OperationID,
			RequestedAt: time.Now().UTC(),
		})
		if err != nil {
			return nil, mapSupplierAccessPublicError(err)
		}
		return &verificationChallengeOutput{
			Status:         http.StatusOK,
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			Body:           challengeResponseBody(result),
		}, nil
	})

	huma.Register(supplierAPI, huma.Operation{
		OperationID: "supplier-access-verify-challenge",
		Method:      http.MethodPost,
		Path:        "/supplier-access/challenges/verify",
		Summary:     "Verify an emailed code and establish a Supplier session",
		Security:    platformhttp.PublicSecurityRequirements(),
	}, func(ctx context.Context,
		input *verifyChallengeInput) (*verifiedOutput, error) {

		verifiedAt := time.Now().UTC()
		result, err := service.VerifyChallenge(ctx, VerifyChallengeInput{
			ChallengeID:           input.Body.ChallengeID,
			Code:                  input.Body.Code,
			OperationID:           input.Body.OperationID,
			PresentedSessionToken: input.SessionCookie.Value,
			VerifiedAt:            verifiedAt,
		})
		if err != nil {
			return nil, mapSupplierAccessPublicError(err)
		}
		// VerifyChallenge returns only after the session, exact-generation
		// binding and final invitation revalidation are complete. Cookies are
		// therefore never issued for a partial recovery.
		return &verifiedOutput{
			Status: http.StatusOK,
			SetCookie: []http.Cookie{
				supplierSessionCookie(
					result.SessionToken, result.SlidingExpiresAt,
					verifiedAt, secureCookies, sameSite),
				supplierCSRFCookie(
					result.CSRFToken, result.SlidingExpiresAt,
					verifiedAt, secureCookies, sameSite),
			},
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			// A map keeps Huma's schema-link transformer from injecting a
			// framework $schema field into Revision 14's exact one-field body.
			Body: map[string]string{"status": "verified"},
		}, nil
	})

	huma.Register(supplierAPI, huma.Operation{
		OperationID: "supplier-access-get-session",
		Method:      http.MethodGet,
		Path:        "/supplier-access/session",
		Summary:     "Resolve the current Supplier session invitation",
		Security:    platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context,
		input *bootstrapSupplierSessionInput) (
		*bootstrapSupplierSessionOutput, error) {

		accessedAt := time.Now().UTC()
		authorized, err := service.BootstrapSupplierSession(
			ctx, BootstrapSupplierSessionInput{
				SessionToken: input.SessionCookie.Value,
				AccessedAt:   accessedAt,
			})
		if err != nil {
			return nil, mapSupplierAccessPublicError(err)
		}
		return &bootstrapSupplierSessionOutput{
			Status: http.StatusOK,
			SetCookie: supplierSessionCookie(
				authorized.SessionCookieRenewal.Token,
				authorized.SessionCookieRenewal.ExpiresAt,
				accessedAt, secureCookies, sameSite),
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			Body: map[string]string{
				"invitationId": authorized.InvitationID,
			},
		}, nil
	})

	huma.Register(supplierAPI, huma.Operation{
		OperationID: "supplier-access-logout",
		Method:      http.MethodPost,
		Path:        "/supplier-access/session/logout",
		Summary:     "Revoke or clear the current Supplier session",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context,
		input *logoutSupplierSessionInput) (
		*logoutSupplierSessionOutput, error) {

		err := service.LogoutSupplierSession(
			ctx, LogoutSupplierSessionInput{
				SessionToken: input.SessionCookie.Value,
				CSRFCookie:   input.CSRFCookie.Value,
				CSRFHeader:   input.CSRFHeader,
				LoggedOutAt:  time.Now().UTC(),
			})
		if err != nil {
			// In particular, a CSRF rejection returns no output object, so no
			// Set-Cookie header can clear an otherwise valid browser session.
			return nil, mapSupplierAccessPublicError(err)
		}
		return &logoutSupplierSessionOutput{
			Status: http.StatusNoContent,
			SetCookie: []http.Cookie{
				expiredSupplierSessionCookie(secureCookies, sameSite),
				expiredSupplierCSRFCookie(secureCookies, sameSite),
			},
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
		}, nil
	})
}

func mapSupplierAccessPublicError(err error) error {
	headers := http.Header{
		"Cache-Control":   []string{supplierNoStore},
		"Pragma":          []string{"no-cache"},
		"Referrer-Policy": []string{"no-referrer"},
	}
	var publicError error
	switch {
	case errors.Is(err, ErrInvalidSupplierCredential),
		errors.Is(err, ErrAccessExchangeNotFound),
		errors.Is(err, ErrAccessExchangeConsumed),
		errors.Is(err, ErrVerificationChallengeNotCurrent):
		publicError = huma.Error404NotFound("supplier access unavailable")
	case errors.Is(err, ErrInvalidVerificationRequest):
		publicError = huma.Error422UnprocessableEntity(
			"verification request is invalid")
	case errors.Is(err, ErrSupplierCSRFRejected):
		publicError = huma.Error403Forbidden("supplier request forbidden")
	case errors.Is(err, ErrChallengeOperationConflict):
		publicError = huma.Error409Conflict(
			"verification operation conflicts with an existing request")
	case errors.Is(err, ErrVerificationRateLimited):
		retryAt := time.Now().UTC().Add(time.Second)
		var limited *VerificationRateLimitExceeded
		if errors.As(err, &limited) && limited.RetryAt.After(retryAt) {
			retryAt = limited.RetryAt
		}
		retrySeconds := int(math.Ceil(time.Until(retryAt).Seconds()))
		if retrySeconds < 1 {
			retrySeconds = 1
		}
		if retrySeconds > 3600 {
			retrySeconds = 3600
		}
		headers.Set("Retry-After", strconv.Itoa(retrySeconds))
		publicError = huma.Error429TooManyRequests(
			"verification request rate limited")
	default:
		// Infrastructure/configuration details remain server-side. The public
		// body intentionally carries only a bounded operational message.
		publicError = huma.Error503ServiceUnavailable(
			"supplier access is temporarily unavailable")
	}
	return huma.ErrorWithHeaders(publicError, headers)
}

func challengeResponseBody(
	result VerificationChallengeResult) *verificationChallengeBody {
	return &verificationChallengeBody{
		ChallengeID: result.ChallengeID, ExpiresAt: result.ExpiresAt,
		DeliveryStatus: result.DeliveryStatus,
	}
}

func expiredAccessExchangeCookie(secure bool, sameSite http.SameSite) http.Cookie {
	return http.Cookie{
		Name: AccessExchangeCookieName, Value: "",
		Path: supplierAccessCookiePath, Domain: "",
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: sameSite,
	}
}

func supplierSessionCookie(rawToken string, expiresAt, now time.Time,
	secure bool, sameSite http.SameSite) http.Cookie {
	return supplierCredentialCookie(
		SupplierSessionCookieName, rawToken, expiresAt, now, true, secure, sameSite)
}

func supplierCSRFCookie(rawToken string, expiresAt, now time.Time,
	secure bool, sameSite http.SameSite) http.Cookie {
	return supplierCredentialCookie(
		SupplierCSRFCookieName, rawToken, expiresAt, now, false, secure, sameSite)
}

func supplierCredentialCookie(name, value string, expiresAt, now time.Time,
	httpOnly, secure bool, sameSite http.SameSite) http.Cookie {
	maxAge := int(expiresAt.Sub(now) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	return http.Cookie{
		Name: name, Value: value,
		Path: supplierAccessCookiePath, Domain: "",
		Expires: expiresAt, MaxAge: maxAge,
		HttpOnly: httpOnly, Secure: secure,
		SameSite: sameSite,
	}
}

func expiredSupplierSessionCookie(secure bool, sameSite http.SameSite) http.Cookie {
	return expiredSupplierCredentialCookie(
		SupplierSessionCookieName, true, secure, sameSite)
}

func expiredSupplierCSRFCookie(secure bool, sameSite http.SameSite) http.Cookie {
	return expiredSupplierCredentialCookie(
		SupplierCSRFCookieName, false, secure, sameSite)
}

func expiredSupplierCredentialCookie(name string, httpOnly, secure bool, sameSite http.SameSite) http.Cookie {
	return http.Cookie{
		Name: name, Value: "",
		Path: supplierAccessCookiePath, Domain: "",
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: httpOnly, Secure: secure,
		SameSite: sameSite,
	}
}
