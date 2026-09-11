// Package tenanttest provides a test-only HTTP router builder that wires
// every Milestone 1 through 8 module together exactly as cmd/api/main.go
// does, so integration tests can exercise real HTTP handlers against a real
// MongoDB instance. This package is never imported by cmd/api or any domain
// module.
//
// The domain service graph itself is built by composition.BuildServices —
// the SAME function cmd/api/main.go calls — so this file only supplies a
// test-fixed config.Config and mounts HTTP routes; it no longer maintains
// its own independent copy of the composition root. This is what makes
// TestBothCompositionRootsWireM8FoundationsIdentically and
// TestBothCompositionRootsCloseTheIssuanceCycleWithTheSetter meaningful
// again after cmd/api/main.go stopped inlining that wiring itself: both
// composition roots now delegate to the one canonical builder, so they
// cannot drift apart by construction, not merely by a comparison test.
package tenanttest

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
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
	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
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

const (
	testRefreshTokenTTL = 7 * 24 * time.Hour
	testJWTSecret       = "tenanttest-fixed-secret-do-not-use-in-production"
	// testExternalAPIBaseURL builds the dev/integration client API URL. It is
	// NOT a finished client portal link (design spec section 4.3).
	testExternalAPIBaseURL = "http://tenanttest.local"

	// TestAllowedOrigin is the single browser origin tenant tests treat as
	// allowed for CORS and the refresh/logout Origin guard. Exported so
	// acceptance tests outside this package (tenanttest_test) can exercise
	// real CORS behavior against BuildRouter without duplicating the value.
	// Journeys that need to exercise a rejected origin use a different
	// literal directly.
	TestAllowedOrigin = "http://tenanttest-frontend.local"

	// testInvitationSecretActiveVersion and testInvitationSecretKey are the
	// FIXED invitation-secret keyring the tests inject (M8 design spec §6.1A).
	//
	// Fixed rather than random for the same reason production forbids
	// auto-generation: a per-run key would make a derived invitation link
	// unreproducible within the run. This value is test-only and its presence
	// here is exactly why production keys come from configuration instead.
	testInvitationSecretActiveVersion = 1
	// 35 decoded bytes, above the §6.1A 32-byte minimum.
	testInvitationSecretKey         = "dGVuYW50dGVzdC1pbnZpdGF0aW9uLXNlY3JldC1rZXktdjE="
	testSupplierVerificationCodeKey = "dGVuYW50dGVzdC12ZXJpZmljYXRpb24tY29kZS1rZXktdjE="
	testSupplierSessionTokenKey     = "dGVuYW50dGVzdC1zdXBwbGllci1zZXNzaW9uLWtleS12MQ=="
	testSupplierRateFingerprintKey  = "dGVuYW50dGVzdC1yYXRlLWxpbWl0LWZpbmdlcnByaW50LWtleQ=="
	testVisualAssetCapabilityKey    = "dGVuYW50dGVzdC12aXN1YWwtYXNzZXQtY2FwYWJpbGl0eS1rZXktdjE="

	// TestAIInternalToken is the fixed bearer token tenant tests configure
	// the Go->Python AI client with. Tests that run a real ai-service (or a
	// stub httptest.Server) must configure it with this same token so
	// requests authenticate; tests that never call an AI route can ignore
	// it entirely.
	TestAIInternalToken = "tenanttest-fixed-ai-internal-token"
)

// BuildRouter constructs the full HTTP router — identity, companies, all five
// Milestone 2 modules, all three Milestone 3 modules, and the Milestone 4
// estimates module — against db, mirroring cmd/api/main.go's composition
// root exactly (construction order, EnsureIndexes calls, route mounting).
func BuildRouter(t *testing.T, db *mongo.Database) (chi.Router, error) {
	return BuildRouterWithMailer(t, db, nil)
}

// BuildRouterAndServicesForTest is BuildRouter plus the underlying
// *composition.Services graph the returned router's routes are actually
// registered against — see
// BuildRouterAndServicesWithMailerAndAIServiceURL's doc comment for why
// this differs from BuildServicesForTest.
func BuildRouterAndServicesForTest(t *testing.T, db *mongo.Database) (chi.Router, *composition.Services, error) {
	return BuildRouterAndServicesWithMailerAndAIServiceURL(t, db, nil, "")
}

// BuildServicesForTest constructs the SAME composition.Services graph
// BuildRouter's router is backed by, against the identical test config —
// for tests that need to call an internal-only service method directly
// (e.g. RP4D's Service.PublishVisualAssetVersion, which has no public HTTP
// route by design) against the SAME database a router built from db serves
// requests against.
func BuildServicesForTest(t *testing.T, db *mongo.Database) (*composition.Services, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return composition.BuildServices(ctx, testConfig(t, ""), zerolog.New(io.Discard), db)
}

// BuildRouterWithMailer wires the same composition root but lets a test supply
// the mailer.
//
// The default SMTP sender dials a relay that tenant tests do not run, so every
// send fails. That is harmless for suites that never deliver mail, but an
// invitation is activated only by a SUCCESSFUL send, so the Phase H acceptance
// journeys cannot reach a usable Supplier link without a mailer that succeeds.
// Passing nil preserves the original behaviour exactly.
//
// The AI service URL is left unconfigured (empty), matching production's
// "AI feature not configured" state — AI routes still register (so 404-vs-
// 401-vs-503 behavior is exercisable) but any real generation call fails
// with a typed service-unavailable error, never a panic. Suites that need a
// real or stubbed ai-service use BuildRouterWithMailerAndAIServiceURL.
func BuildRouterWithMailer(
	t *testing.T, db *mongo.Database, mailer mail.EmailSender) (chi.Router, error) {
	return BuildRouterWithMailerAndAIServiceURL(t, db, mailer, "")
}

var (
	sharedTestStorageRootsMu sync.Mutex
	sharedTestStorageRoots   = map[*testing.T]string{}
)

// sharedTestStorageRoot returns the ONE t.TempDir() this test uses for
// every composition.Services/router it builds, lazily creating it on first
// use. A test that independently builds more than one Services graph for
// the same t (e.g. BuildServicesForTest alongside a router from
// setupRouterWithDatabase — see spatial_visual_asset_test.go's own "the
// SAME object graph" doc comment) needs them to share one local object
// store root, or content published through one graph is invisible to the
// other's content-serving route. t.TempDir() itself still owns deleting
// the directory (via its own t.Cleanup); this map only stops a SECOND call
// for the same t from generating a SECOND, different directory. Different
// *testing.T values (including subtests) naturally get different roots.
func sharedTestStorageRoot(t *testing.T) string {
	t.Helper()
	sharedTestStorageRootsMu.Lock()
	defer sharedTestStorageRootsMu.Unlock()
	if dir, ok := sharedTestStorageRoots[t]; ok {
		return dir
	}
	dir := t.TempDir()
	sharedTestStorageRoots[t] = dir
	t.Cleanup(func() {
		sharedTestStorageRootsMu.Lock()
		defer sharedTestStorageRootsMu.Unlock()
		delete(sharedTestStorageRoots, t)
	})
	return dir
}

// testConfig builds the fixed config.Config tenant tests pass to
// composition.BuildServices — the test-only equivalent of the real
// environment variables cmd/api/main.go's config.LoadFromEnv reads. Every
// value here was previously a tenanttest-local constant/literal passed
// directly into individual service constructors; consolidating them into a
// config.Config is what lets this package delegate to the one canonical
// composition root instead of maintaining its own.
//
// StorageLocalPath uses sharedTestStorageRoot(t) — never a bare "" (which
// composition's object-store adapters would then resolve as absolute paths
// like "/spatial-artifacts" off the filesystem root, unwritable on a CI
// runner) — writable, cleaned up automatically, and shared across every
// testConfig(t, ...) call for the same t (see sharedTestStorageRoot's own
// doc comment for why that sharing matters).
func testConfig(t *testing.T, aiServiceURL string) config.Config {
	t.Helper()
	return config.Config{
		AppEnv:           "test",
		JWTAccessSecret:  testJWTSecret,
		SMTPHost:         "localhost",
		SMTPPort:         "1025",
		SMTPFrom:         "no-reply@test.local",
		StorageLocalPath: sharedTestStorageRoot(t),
		AIServiceURL:     aiServiceURL,
		AIInternalToken:  TestAIInternalToken,
		AIServiceTimeout: 10 * time.Second,

		ExternalAPIBaseURL: testExternalAPIBaseURL,

		InvitationSecretActiveVersion: testInvitationSecretActiveVersion,
		InvitationSecretKeys: map[int]string{
			testInvitationSecretActiveVersion: testInvitationSecretKey,
		},

		SupplierVerificationCodeActiveVersion: 1,
		SupplierVerificationCodeKeys:          map[int]string{1: testSupplierVerificationCodeKey},
		SupplierSessionTokenActiveVersion:     1,
		SupplierSessionTokenKeys:              map[int]string{1: testSupplierSessionTokenKey},
		SupplierRateLimitFingerprintKey:       testSupplierRateFingerprintKey,

		VisualAssetCapabilityActiveVersion: 1,
		VisualAssetCapabilityKeys:          map[int]string{1: testVisualAssetCapabilityKey},
		VisualAssetAccessTTL:               10 * time.Minute,

		// TrustedProxyCIDRs stays nil — the direct peer is always
		// authoritative in tests, matching the router's prior explicit nil.
	}
}

// BuildRouterWithMailerAndAIServiceURL wires the same composition root as
// BuildRouterWithMailer but additionally points the Go->Python AI client at
// aiServiceURL (typically an httptest.Server URL running a stub, or a real
// local ai-service process for full-stack E2E), authenticating with
// TestAIInternalToken. An empty aiServiceURL matches BuildRouterWithMailer's
// unconfigured behavior exactly.
func BuildRouterWithMailerAndAIServiceURL(
	t *testing.T, db *mongo.Database, mailer mail.EmailSender, aiServiceURL string) (chi.Router, error) {
	router, _, err := BuildRouterAndServicesWithMailerAndAIServiceURL(t, db, mailer, aiServiceURL)
	return router, err
}

// BuildRouterAndServicesWithMailerAndAIServiceURL is
// BuildRouterWithMailerAndAIServiceURL's underlying implementation,
// additionally returning the *composition.Services graph the router's
// routes were actually registered against (the SAME instance, not a
// second composition.BuildServices call) — for tests that need to
// mutate service-level wiring (e.g. RP4E0's SetAssetGenerationSupport,
// which has no config-driven test fixture, unlike every other optional
// capability) and then exercise that exact mutation through real HTTP.
// BuildServicesForTest deliberately does NOT serve this purpose — it
// builds a SEPARATE Services graph against the same database, correct
// for calling a Mongo-durable internal service method (RP4D's
// PublishVisualAssetVersion) but wrong for mutating in-memory Service
// struct fields a router elsewhere already captured a pointer to.
func BuildRouterAndServicesWithMailerAndAIServiceURL(
	t *testing.T, db *mongo.Database, mailer mail.EmailSender, aiServiceURL string) (chi.Router, *composition.Services, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger := zerolog.New(io.Discard)
	cfg := testConfig(t, aiServiceURL)

	// mailer overrides composition.BuildServices's own SMTP sender when a
	// test supplies one (e.g. a fake mail.EmailSender that records sent
	// mail for assertions). Every mail-capable service is constructed with
	// whichever mailer is in effect at construction time, so the override
	// must go through composition.WithMailer, not a post-hoc field swap —
	// see that option's doc comment. A nil mailer here (the common case)
	// passes no option at all, letting BuildServices fall back to its own
	// default SMTP sender at cfg.SMTPHost:cfg.SMTPPort — the exact same
	// never-dialed localhost:1025 sender this router previously constructed
	// directly when no mailer was supplied.
	var opts []composition.Option
	if mailer != nil {
		opts = append(opts, composition.WithMailer(mailer))
	}
	services, err := composition.BuildServices(ctx, cfg, logger, db, opts...)
	if err != nil {
		return nil, nil, err
	}

	testOrigins, err := config.NewAllowedOriginsForTest([]string{TestAllowedOrigin})
	if err != nil {
		return nil, nil, err
	}

	router, api := platformhttp.NewRouter("tenanttest", "0.0.0")
	platformhttp.RegisterHealth(api, nil, "") // health check not exercised by these tests
	// tenanttest intentionally disables Secure so httptest's plain HTTP client
	// can exercise the same cookie flow wired by the shipping root.
	identity.RegisterHandlers(api, services.Auth, int(testRefreshTokenTTL.Seconds()), false, http.SameSiteLaxMode, testOrigins)
	identity.RegisterRegistrationVerificationHandlers(api, services.RegistrationVerification)
	supplieraccess.RegisterHandlers(api, services.SupplierAccess, false)
	rfqissuance.RegisterSupplierHandlers(api, services.RFQIssuance)
	// Supplier Offer routes are protected by Phase D credentials rather than
	// contractor auth, so they mount on the same unauthenticated base API.
	supplieroffers.RegisterHandlers(api, services.SupplierOffers)

	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(identity.RequireAuthHuma(services.JWTIssuer, api))
	authedAPI.UseMiddleware(platformhttp.RequestPrivacyMiddleware(logger))
	authedAPI.UseModifier(func(op *huma.Operation, next func(*huma.Operation)) {
		op.Security = []map[string][]string{{platformhttp.BearerAuthSecurityScheme: {}}}
		next(op)
	})
	// Contractor award routes require a bearer token; the Supplier-facing
	// outcome routes mounted above authenticate with the Phase D session.
	awards.RegisterHandlers(authedAPI, services.Awards)
	supplieroffers.RegisterReconciliationHandlers(authedAPI, services.SupplierOffers)
	identity.RegisterMeHandler(authedAPI, identity.NewCurrentUserService(services.Users, services.Companies))
	companies.RegisterHandlers(authedAPI, services.Companies)
	clients.RegisterHandlers(authedAPI, services.Clients)
	projects.RegisterHandlers(authedAPI, services.Projects)
	properties.RegisterHandlers(authedAPI, services.Properties)
	spaces.RegisterHandlers(authedAPI, services.Spaces)
	spatial.RegisterHandlers(authedAPI, services.Spatial)
	spatial.RegisterDesignHandlers(authedAPI, services.Spatial, "")
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

	// External Client routes register on the BASE api, not authedAPI — they
	// carry no bearer token and are authenticated solely by the opaque access
	// token in the path (design spec §4). Registering them here rather than on
	// the authenticated group is the whole mechanism; no middleware is
	// weakened and no second huma.API is created.
	access.RegisterExternalHandlers(api, services.Access)
	spatial.RegisterExternalHandlers(api, services.Spatial)

	return router, services, nil
}
