package http

import "github.com/danielgtaylor/huma/v2"

const (
	// ExternalNoStore prevents both browser and intermediary reuse of Client or
	// Supplier responses containing token-scoped or commercial information.
	ExternalNoStore        = "no-store, max-age=0"
	ExternalPragma         = "no-cache"
	ExternalReferrerPolicy = "no-referrer"
	ExternalNoSniff        = "nosniff"
)

// NewExternalGroup creates the single transport-policy boundary shared by
// Client-link and Supplier-browser routes. Setting headers before dispatch is
// intentional: Huma validation failures and mapped domain errors never reach a
// successful output DTO, but must receive the same privacy treatment.
func NewExternalGroup(api huma.API) huma.API {
	externalAPI := huma.NewGroup(api)
	externalAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		ctx.SetHeader("Cache-Control", ExternalNoStore)
		ctx.SetHeader("Pragma", ExternalPragma)
		ctx.SetHeader("Referrer-Policy", ExternalReferrerPolicy)
		ctx.SetHeader("X-Content-Type-Options", ExternalNoSniff)
		next(ctx)
	})
	return externalAPI
}
