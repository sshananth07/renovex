package http

import (
	"net/http"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
)

// corsAllowedMethods and corsAllowedHeaders are the fixed credentialed CORS
// contract for every browser-facing route in the process. They are not
// per-route configurable: every authenticated contractor route accepts the
// same method/header surface, so one static list keeps preflight responses
// simple and avoids per-operation CORS drift.
var (
	corsAllowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowedHeaders = "Authorization, Content-Type, X-Request-ID, X-CSRF-Token"
	corsExposedHeaders = "X-Request-ID, Retry-After"
)

// WrapCORS wraps next with credentialed, exact-origin CORS handling enforced
// against allowed. It wraps the fully-built router's http.Handler from the
// outside (main.go, tenanttest) rather than being registered via
// chi.Router.Use, because NewRouter/humachi already register routes on the
// chi mux before a caller has a chance to add middleware — chi panics on
// router.Use once any route exists ("all middlewares must be defined before
// routes on a mux").
//
// The origin decision is exact-match only: no wildcard, no subdomain
// matching, no reflection of an arbitrary Origin header. An origin outside
// allowed receives no permissive CORS headers at all — the browser enforces
// the same-origin policy itself; this middleware never tries to reject with
// a CORS-specific error body for a simple (non-preflight) request, only for
// a rejected preflight.
func WrapCORS(next http.Handler, allowed config.AllowedOrigins) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" {
			// Vary: Origin must be set whenever the response could differ by
			// Origin, even when this particular origin is not allowed —
			// otherwise a shared cache could serve one origin's permissive
			// response to another.
			w.Header().Add("Vary", "Origin")
		}

		isPreflight := r.Method == http.MethodOptions &&
			r.Header.Get("Access-Control-Request-Method") != ""

		if origin == "" || !allowed.Contains(origin) {
			if isPreflight {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if isPreflight {
			w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
			w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
			w.Header().Set("Access-Control-Expose-Headers", corsExposedHeaders)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Access-Control-Expose-Headers", corsExposedHeaders)
		next.ServeHTTP(w, r)
	})
}
