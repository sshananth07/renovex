package composition

import (
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/access"
	"github.com/shananth/renovation-platform/backend/internal/ai"
	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
	"github.com/shananth/renovation-platform/backend/internal/work"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

// httpAppRefreshTokenTTL must match BuildServices's internal
// refreshTokenTTL constant — duplicated rather than imported to avoid a
// second exported constant nobody else needs; cmd/api's own
// refreshTokenTTL constant is a THIRD duplicate of this same value today,
// which NewHTTPHandler does not change (out of scope: reusing this exact
// value in two callers, not five).
const httpAppRefreshTokenTTL = 7 * 24 * time.Hour

// NewHTTPHandler builds and returns the fully wrapped http.Handler for the
// entire API — every route registration cmd/api/main.go performs today,
// extracted into one reusable builder so a Vercel serverless entrypoint
// (api/index.go) and the traditional long-lived binary (cmd/api/main.go)
// share EXACTLY one router-construction code path (M8.5C plan: "Refactor
// Go router construction once so cmd/api and api/index.go use the same
// handler"). This function never listens on a port and never starts the
// local in-process asset-generation dispatch ticker — those remain
// cmd/api-only concerns (StartAssetGenerationDispatchLoop assumes a
// long-lived process, which a Vercel function invocation is not).
func NewHTTPHandler(cfg config.Config, logger zerolog.Logger, mongoClient *mongo.Client, services *Services) http.Handler {
	router, api := platformhttp.NewRouter("Renovation Project Intelligence API", "0.1.0")
	platformhttp.RegisterHealth(api, mongoClient, cfg.MongoDatabase)
	identity.RegisterHandlers(api, services.Auth, int(httpAppRefreshTokenTTL.Seconds()), cfg.RefreshCookieSecure, cfg.RefreshCookieSameSite, cfg.AllowedOrigins)
	identity.RegisterRegistrationVerificationHandlers(api, services.RegistrationVerification)

	// Supplier Access cookies share identity's RefreshCookieSecure/SameSite
	// config rather than deriving Secure from AppEnv: a SameSite=None cookie
	// that isn't also Secure is spec-invalid and gets silently dropped by
	// every modern browser (Chrome and others reject it outright, with no
	// console warning), which breaks Supplier Access in Renovex's tester
	// topology (Web and API on two different Vercel domains) whenever AppEnv
	// isn't exactly "production" — including the deliberately non-production
	// tester deployments this env matrix documents. AUTH_REFRESH_COOKIE_SECURE
	// already defaults to true and is independently configurable, so reusing
	// it here needs no new environment variable.
	supplieraccess.RegisterHandlers(api, services.SupplierAccess, cfg.RefreshCookieSecure, cfg.RefreshCookieSameSite)
	rfqissuance.RegisterSupplierHandlers(api, services.RFQIssuance)
	supplieroffers.RegisterHandlers(api, services.SupplierOffers)

	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(identity.RequireAuthHuma(services.JWTIssuer, api))
	authedAPI.UseMiddleware(platformhttp.RequestPrivacyMiddleware(logger))
	authedAPI.UseModifier(func(op *huma.Operation, next func(*huma.Operation)) {
		op.Security = []map[string][]string{{platformhttp.BearerAuthSecurityScheme: {}}}
		next(op)
	})
	awards.RegisterHandlers(authedAPI, services.Awards)
	supplieroffers.RegisterReconciliationHandlers(authedAPI, services.SupplierOffers)
	identity.RegisterMeHandler(authedAPI, identity.NewCurrentUserService(services.Users, services.Companies))
	companies.RegisterHandlers(authedAPI, services.Companies)
	clients.RegisterHandlers(authedAPI, services.Clients)
	projects.RegisterHandlers(authedAPI, services.Projects)
	properties.RegisterHandlers(authedAPI, services.Properties)
	spaces.RegisterHandlers(authedAPI, services.Spaces)
	spatial.RegisterHandlers(authedAPI, services.Spatial)
	spatial.RegisterDesignHandlers(authedAPI, services.Spatial, cfg.AssetGenerationRuntimeNoticeText)
	spatial.RegisterDesignGenerationHandlers(authedAPI, services.Spatial)
	work.RegisterHandlers(authedAPI, services.Work)
	materials.RegisterHandlers(authedAPI, services.Materials)
	costs.RegisterHandlers(authedAPI, services.Costs)
	labour.RegisterHandlers(authedAPI, services.Labour)
	estimates.RegisterHandlers(authedAPI, services.Estimates)
	quotations.RegisterHandlers(authedAPI, services.Quotations)
	access.RegisterHandlers(authedAPI, services.Access)
	materialrequirements.RegisterHandlers(authedAPI, services.MaterialRequirements)
	rfqs.RegisterHandlers(authedAPI, services.RFQs)
	rfqissuance.RegisterHandlers(authedAPI, services.RFQIssuance)
	suppliers.RegisterHandlers(authedAPI, services.Suppliers)
	workresources.RegisterHandlers(authedAPI, services.WorkResources)
	ai.RegisterHandlers(authedAPI, services.AI)

	access.RegisterExternalHandlers(api, services.Access)
	spatial.RegisterExternalHandlers(api, services.Spatial)
	// RP4E2/RP4E0 Gate 2: internal bounded-worker routes, hidden from the
	// public OpenAPI schema and authenticated by a static shared secret
	// (never JWT) — see RegisterInternalWorkerHandlers' own doc comment.
	// Registered only when SPATIAL_WORKER_TOKEN is actually configured;
	// an empty token would make verifyWorkerToken reject every request
	// anyway, but registering the routes at all with no way to ever
	// authenticate them would be a confusing half-state, so this mirrors
	// every other optional-capability's "absent config == route absent"
	// convention instead.
	if cfg.SpatialWorkerToken != "" {
		spatial.RegisterInternalWorkerHandlers(api, services.Spatial, cfg.SpatialWorkerToken)
	}

	return platformhttp.WrapCORS(router, cfg.AllowedOrigins)
}
