package identity

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type principalContextKey struct{}

// PrincipalFromContext extracts the Principal set by RequireAuth or
// RequireAuthHuma, if any.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(Principal)
	return p, ok
}

// ContextWithPrincipal returns ctx carrying principal, exactly as the auth
// middleware would.
//
// The context key is unexported, so without this the authorization helpers
// could only be exercised through a full HTTP round trip — which would test the
// middleware rather than the authorization rule. Production code uses the
// middleware; this is the seam that makes the rule itself directly testable.
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// principalFromAuthHeader verifies an "Authorization: Bearer <accessJWT>"
// header value against issuer and returns the resulting Principal. Shared by
// both RequireAuth (net/http) and RequireAuthHuma (huma.Context) so the two
// transport bindings never drift on token-verification logic.
func principalFromAuthHeader(issuer *JWTIssuer, authHeader string) (Principal, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return Principal{}, errMissingOrMalformedHeader
	}
	tokenString := strings.TrimPrefix(authHeader, prefix)

	claims, err := issuer.VerifyAccessToken(tokenString)
	if err != nil {
		return Principal{}, errInvalidOrExpiredToken
	}

	return Principal{UserID: claims.UserID, CompanyID: claims.CompanyID, Role: claims.Role}, nil
}

var (
	errMissingOrMalformedHeader = &authHeaderError{"missing or malformed Authorization header"}
	errInvalidOrExpiredToken    = &authHeaderError{"invalid or expired access token"}
)

type authHeaderError struct{ msg string }

func (e *authHeaderError) Error() string { return e.msg }

// RequireAuth returns middleware that verifies the Authorization: Bearer
// <accessJWT> header, constructs a Principal from its claims, and stores it
// in the request context. Requests with a missing, malformed, expired, or
// invalid-signature token are rejected with 401 before reaching the wrapped
// handler.
func RequireAuth(issuer *JWTIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := principalFromAuthHeader(issuer, r.Header.Get("Authorization"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuthHuma returns a huma.Group-compatible middleware (for use with
// group.UseMiddleware) that enforces the same bearer-token check as
// RequireAuth, but operates on every operation registered on a huma.Group
// without requiring a second huma.API/OpenAPI document — unlike wrapping a
// chi sub-router in its own humachi.New, which would register a duplicate,
// independent OpenAPI spec that never surfaces the group's operations in the
// primary /docs and /openapi.json served by the top-level API. api is used
// only for content-negotiated error writing on rejection; pass the same
// huma.API the group wraps.
func RequireAuthHuma(issuer *JWTIssuer, api huma.API) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		principal, err := principalFromAuthHeader(issuer, ctx.Header("Authorization"))
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, err.Error())
			return
		}

		ctx = huma.WithValue(ctx, principalContextKey{}, principal)
		next(ctx)
	}
}
