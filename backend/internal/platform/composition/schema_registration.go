package composition

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

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

// RegisterAllForSchema registers every production HTTP operation on api with
// nil service dependencies, mirroring cmd/api/main.go's route-mounting
// structure exactly (which routes are public, which require a bearer token,
// which are the unauthenticated external Client surface) without connecting
// to MongoDB, SMTP, or opening a network listener.
//
// This works because Huma's schema construction (huma.Register plus the
// request/response type reflection it performs at registration time) never
// invokes a handler closure's captured service — the nil svc is only
// dereferenced if a request is actually routed to the handler, which never
// happens here. The same pattern is already proven at smaller scope by
// TestMilestone7HandlersShareOneSchemaRegistry and
// TestMilestone8HandlersComposeWithUniqueRoutesAndDocumentedSecurity in
// handler_schema_test.go.
//
// This is the single shared registration boundary cmd/openapi uses to avoid
// a second, manually duplicated route list. cmd/api/main.go and
// tenanttest.BuildRouter still construct their own real (non-nil) services
// and call the same per-module RegisterHandlers functions directly, because
// they need working handlers, not just schema — but the set and structure of
// calls below must stay identical to main.go's, which
// compositionroot_test.go's AST-based drift guard enforces for main.go vs
// tenanttest already; this function is checked against main.go by
// TestRegisterAllForSchemaIncludesKeyF1Routes and the OpenAPI route-inventory
// comparison in Checkpoint 12.
func RegisterAllForSchema(api huma.API) {
	platformhttp.RegisterHealth(api, nil, "")

	emptyOrigins, _ := config.NewAllowedOriginsForTest(nil)
	identity.RegisterHandlers(api, nil, 0, true, http.SameSiteLaxMode, emptyOrigins)
	identity.RegisterRegistrationVerificationHandlers(api, nil)

	supplieraccess.RegisterHandlers(api, nil, false)
	rfqissuance.RegisterSupplierHandlers(api, nil)
	supplieroffers.RegisterHandlers(api, nil)

	authedAPI := huma.NewGroup(api)
	authedAPI.UseModifier(func(op *huma.Operation, next func(*huma.Operation)) {
		op.Security = []map[string][]string{{platformhttp.BearerAuthSecurityScheme: {}}}
		next(op)
	})
	awards.RegisterHandlers(authedAPI, nil)
	supplieroffers.RegisterReconciliationHandlers(authedAPI, nil)
	identity.RegisterMeHandler(authedAPI, nil)
	companies.RegisterHandlers(authedAPI, nil)
	clients.RegisterHandlers(authedAPI, nil)
	projects.RegisterHandlers(authedAPI, nil)
	properties.RegisterHandlers(authedAPI, nil)
	spaces.RegisterHandlers(authedAPI, nil)
	spatial.RegisterHandlers(authedAPI, nil)
	spatial.RegisterDesignHandlers(authedAPI, nil, "")
	spatial.RegisterDesignGenerationHandlers(authedAPI, nil)
	work.RegisterHandlers(authedAPI, nil)
	materials.RegisterHandlers(authedAPI, nil)
	costs.RegisterHandlers(authedAPI, nil)
	labour.RegisterHandlers(authedAPI, nil)
	estimates.RegisterHandlers(authedAPI, nil)
	quotations.RegisterHandlers(authedAPI, nil)
	access.RegisterHandlers(authedAPI, nil)
	materialrequirements.RegisterHandlers(authedAPI, nil)
	rfqs.RegisterHandlers(authedAPI, nil)
	rfqissuance.RegisterHandlers(authedAPI, nil)
	suppliers.RegisterHandlers(authedAPI, nil)
	workresources.RegisterHandlers(authedAPI, nil)
	ai.RegisterHandlers(authedAPI, nil)

	access.RegisterExternalHandlers(api, nil)
	spatial.RegisterExternalHandlers(api, nil)
	// Hidden: true (never appears in the generated OpenAPI schema itself)
	// but still registered here so this drift-guard mirrors httpapp.go's
	// exact Register* call inventory — a non-empty token argument is
	// irrelevant during schema generation (no request ever reaches the
	// handler closure), matching every other nil-service call above.
	spatial.RegisterInternalWorkerHandlers(api, nil, "schema-generation-placeholder")
}
