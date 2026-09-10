package http

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

// RequestPrivacyMiddleware returns Huma group middleware for every
// authenticated contractor route (mounted the same way as
// identity.RequireAuthHuma — via authedAPI.UseMiddleware, not per-operation):
// it resolves/validates X-Request-ID, sets the same privacy-header contract
// external.NewExternalGroup applies to Client/Supplier routes, and logs one
// structured line per request carrying the resolved ID for correlation.
//
// Headers are set before dispatch (mirroring NewExternalGroup's own
// reasoning) so a Huma validation failure or mapped domain error still
// receives the same treatment as a successful response — this middleware
// runs for every request that reaches the group, regardless of outcome.
//
// This is deliberately the one shared boundary for M0-M8 (and later)
// authenticated routes: individual handlers must never set these headers
// themselves.
func RequestPrivacyMiddleware(logger zerolog.Logger) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		requestID := ResolveRequestID(ctx.Header("X-Request-ID"))

		ctx.SetHeader("Cache-Control", ExternalNoStore)
		ctx.SetHeader("Pragma", ExternalPragma)
		ctx.SetHeader("Referrer-Policy", ExternalReferrerPolicy)
		ctx.SetHeader("X-Content-Type-Options", ExternalNoSniff)
		ctx.SetHeader("X-Request-ID", requestID)

		logger.Info().
			Str("requestId", requestID).
			Str("method", ctx.Method()).
			Str("path", ctx.URL().Path).
			Msg("request")

		next(ctx)
	}
}
