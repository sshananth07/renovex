package http

import "github.com/shananth/renovation-platform/backend/internal/platform/config"

// DecideOriginGuard reports whether a cookie-authenticated browser mutation
// (POST /auth/refresh, POST /auth/logout) may proceed, given the request's
// Origin header (rawOrigin, "" if absent) and Sec-Fetch-Site header
// (secFetchSite, "" if absent).
//
// This is the same allowed-origin set CORS enforces, applied to a route CORS
// itself never sees the effect of (a cookie-carrying same-origin or
// no-CORS request has no preflight and no Access-Control-* response headers
// to withhold) — so this guard is the actual enforcement point for
// refresh/logout, not a redundant check.
//
// Rules, in order:
//  1. Origin present: allowed only if it exactly matches allowed. A
//     malformed or "null" Origin never matches any configured entry, so it
//     is rejected by construction, and Sec-Fetch-Site is irrelevant once an
//     Origin is present.
//  2. Origin absent, Sec-Fetch-Site absent: allowed as a non-browser client
//     (native app, curl, server-to-server) — browsers that support
//     Sec-Fetch-Site always send both headers together for a fetch/XHR to
//     these endpoints, so "neither header present" is not a spoofable
//     browser cross-site request.
//  3. Origin absent, Sec-Fetch-Site is "same-origin" or "same-site":
//     allowed — the browser itself asserts the request originated from a
//     trusted same-origin/same-site context.
//  4. Origin absent, Sec-Fetch-Site is "cross-site" or any other value
//     (including "none"): rejected.
func DecideOriginGuard(allowed config.AllowedOrigins, rawOrigin, secFetchSite string) bool {
	if rawOrigin != "" {
		return allowed.Contains(rawOrigin)
	}
	if secFetchSite == "" {
		return true
	}
	return secFetchSite == "same-origin" || secFetchSite == "same-site"
}
