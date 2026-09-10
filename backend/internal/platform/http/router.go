// Package http centralizes chi router construction and Huma API mounting
// so every module registers HTTP operations the same way. Business
// services must not depend on chi or huma types — those stay at this
// boundary and in each module's own handler.go.
package http

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// BearerAuthSecurityScheme is the name of the HTTP bearer security scheme
// registered on every API NewRouter constructs. Modules that mount
// authenticated operations behind identity.RequireAuthHuma reference this
// name in an operation's Security requirement so the OpenAPI document (and
// therefore /docs) advertises which routes need a token and renders an
// "Authorize" control for supplying one, instead of requiring callers to
// discover the header manually.
const BearerAuthSecurityScheme = "bearerAuth"

const (
	// SupplierAccessExchangeSecurityScheme documents the short-lived,
	// path-restricted cookie created when an opaque invitation link is opened.
	SupplierAccessExchangeSecurityScheme = "supplierAccessExchange"
	// SupplierSessionSecurityScheme documents Phase D's HttpOnly Supplier
	// session cookie. Runtime authorization still validates the invitation
	// binding and current access generation on every protected request.
	SupplierSessionSecurityScheme = "supplierSession"
	// SupplierCSRFSecurityScheme documents the canonical double-submit header
	// required in addition to the Supplier session on mutations.
	SupplierCSRFSecurityScheme = "supplierCSRF"
)

// PublicSecurityRequirements emits an explicit anonymous alternative. Using
// an empty requirement object keeps that decision visible in generated JSON,
// unlike a nil slice which merely leaves the operation unspecified.
func PublicSecurityRequirements() []map[string][]string {
	return []map[string][]string{{}}
}

func SupplierExchangeSecurityRequirements() []map[string][]string {
	return []map[string][]string{{SupplierAccessExchangeSecurityScheme: {}}}
}

func SupplierSessionSecurityRequirements() []map[string][]string {
	return []map[string][]string{{SupplierSessionSecurityScheme: {}}}
}

// SupplierMutationSecurityRequirements places both schemes in one requirement
// object, which is OpenAPI's AND form: the session cookie and CSRF header are
// both required. Separate objects would incorrectly document an OR.
func SupplierMutationSecurityRequirements() []map[string][]string {
	return []map[string][]string{{
		SupplierSessionSecurityScheme: {},
		SupplierCSRFSecurityScheme:    {},
	}}
}

// NewRouter constructs a chi.Router with baseline cross-cutting middleware
// (request ID, panic recovery) and a Huma API mounted at its root, ready
// for domain modules to register operations against. The returned API has
// BearerAuthSecurityScheme registered in its OpenAPI components; it is not
// yet required by any operation — callers opt in per-operation or per-group
// by setting Operation.Security.
func NewRouter(apiTitle, apiVersion string) (chi.Router, huma.API) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	config := huma.DefaultConfig(apiTitle, apiVersion)
	config.Transformers = append(config.Transformers,
		func(_ huma.Context, _ string, value any) (any, error) {
			model, ok := value.(*huma.ErrorModel)
			if !ok || len(model.Errors) <= procurementlimits.MaxErrorDetails {
				return value, nil
			}
			// Copy before truncating so an error object retained by server-side
			// observability is not mutated by response serialization.
			bounded := *model
			bounded.Errors = append([]*huma.ErrorDetail(nil),
				model.Errors[:procurementlimits.MaxErrorDetails]...)
			return &bounded, nil
		})
	api := humachi.New(router, config)
	api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		BearerAuthSecurityScheme: {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "Access token issued by POST /auth/register or /auth/login.",
		},
		SupplierAccessExchangeSecurityScheme: {
			Type:        "apiKey",
			In:          "cookie",
			Name:        "supplier_access_exchange",
			Description: "Short-lived invitation exchange credential.",
		},
		SupplierSessionSecurityScheme: {
			Type:        "apiKey",
			In:          "cookie",
			Name:        "supplier_session",
			Description: "Supplier session bound to the current invitation generation.",
		},
		SupplierCSRFSecurityScheme: {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-CSRF-Token",
			Description: "Double-submit CSRF token required for Supplier mutations.",
		},
	}

	return router, api
}
