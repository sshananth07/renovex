// Services and BuildServices let cmd/api (the HTTP server) and
// cmd/demoseed (the development-only demo data tool) share the exact same
// acyclic service-construction order, so neither entrypoint can silently
// drift from the other.
package composition

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/access"
	"github.com/shananth/renovation-platform/backend/internal/ai"
	"github.com/shananth/renovation-platform/backend/internal/aiintegration"
	"github.com/shananth/renovation-platform/backend/internal/approvals"
	"github.com/shananth/renovation-platform/backend/internal/audit"
	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/platform/hunyuan"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
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
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

// Services holds every constructed service in the Renovex backend, wired in
// the same acyclic order cmd/api's HTTP server and cmd/demoseed's tenant
// seeding both depend on. Fields are exported so both entrypoints can reach
// exactly the services they need.
type Services struct {
	Auth                     *identity.AuthService
	Users                    *identity.UserService
	JWTIssuer                *identity.JWTIssuer
	RegistrationVerification *identity.RegistrationVerificationService
	Companies                *companies.Service
	Clients                  *clients.Service
	Projects                 *projects.Service
	Properties               *properties.Service
	Spaces                   *spaces.Service
	Spatial                  *spatial.Service
	Work                     *work.Service
	Materials                *materials.Service
	Costs                    *costs.Service
	Labour                   *labour.Service
	Estimates                *estimates.Service
	Quotations               *quotations.Service
	Approvals                *approvals.Service
	Audit                    *audit.Service
	Access                   *access.Service

	MaterialRequirements *materialrequirements.Service
	RFQs                 *rfqs.Service
	Suppliers            *suppliers.Service
	RFQIssuance          *rfqissuance.Service
	SupplierAccess       *supplieraccess.Service
	SupplierOffers       *supplieroffers.Service
	Awards               *awards.Service
	WorkResources        *workresources.Service
	AI                   *ai.Service
	Mailer               mail.EmailSender

	InvitationKeyring               *secrets.InvitationKeyring
	SupplierVerificationCodeKeyring *secrets.SupplierVerificationCodeKeyring
}

// buildOptions holds BuildServices's optional overrides.
type buildOptions struct {
	mailer mail.EmailSender
}

// Option customizes BuildServices without changing its required parameters.
type Option func(*buildOptions)

// WithMailer overrides the mailer BuildServices would otherwise construct
// from cfg.SMTPHost/cfg.SMTPPort/cfg.SMTPFrom. Every mail-capable service
// (companies, rfqissuance, supplieraccess, awards) is constructed with
// whichever mailer is in effect at that point, so this must be supplied via
// BuildServices itself — swapping *Services.Mailer afterward would not
// reach services that already captured the original reference.
//
// Used by internal/tenanttest, which needs to observe sent mail (a fake
// mail.EmailSender) or discard it entirely (nil) rather than dial a real
// SMTP relay tests do not run. No production caller uses this option.
func WithMailer(mailer mail.EmailSender) Option {
	return func(o *buildOptions) { o.mailer = mailer }
}

// BuildServices constructs every repository (calling EnsureIndexes on each),
// then every service, in the strictly acyclic order established across
// Milestones 1-8.5B-A. This is a verbatim extraction of what cmd/api/main.go
// built inline, with no behavioral change: the only differences from the
// original inline code are (1) EnsureIndexes failures return an error
// instead of calling logger.Fatal — this function has no authority to
// terminate the process, only its caller does — and (2) the final block
// returns a *Services struct instead of continuing on to HTTP wiring. (3)
// it accepts optional Options, the only addition beyond a verbatim
// extraction: WithMailer lets a caller (currently only
// internal/tenanttest, which needs to observe or discard sent mail rather
// than dial a real SMTP relay) override the mailer every mail-capable
// service is constructed with, since the mailer is captured by value at
// construction time and cannot be swapped afterward via the returned
// *Services.Mailer field alone.
// selectMailer builds the real mail.EmailSender BuildServices wires every
// mail-capable service with, from cfg alone (T2B/T2C — pure and
// Mongo-free so it's independently unit-testable). cfg.EmailProvider
// selects the transport ("smtp" for local Mailpit, "resend" for
// tester/production); cfg.EmailDeliveryMode then optionally wraps it in
// TestSinkSender, which rewrites ONLY the outbound transport recipient —
// every caller (company invites, RFQ issuance, supplier verification,
// identity.RegistrationVerificationService) keeps calling the same
// mail.EmailSender interface with the real logical recipient unchanged.
// LoadFromEnv already refuses APP_ENV=production with
// EmailDeliveryMode=test_sink, so this selection can trust that invariant
// rather than re-checking it here.
func selectMailer(cfg config.Config) mail.EmailSender {
	var mailer mail.EmailSender
	if cfg.EmailProvider == "resend" {
		mailer = mail.NewResendSender(cfg.ResendAPIKey, cfg.ResendFrom)
	} else {
		mailer = mail.NewSMTPSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom)
	}
	if cfg.EmailDeliveryMode == "test_sink" {
		mailer = mail.NewTestSinkSender(mailer, cfg.EmailTestSinkAddress)
	}
	return mailer
}

func BuildServices(ctx context.Context, cfg config.Config, logger zerolog.Logger, db *mongo.Database, opts ...Option) (*Services, error) {
	options := buildOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	// --- Repositories (identity, companies — unchanged from Milestone 1) ---
	userRepo := identity.NewMongoUserRepository(db)
	sessionRepo := identity.NewMongoAuthSessionRepository(db)
	companyRepo := companies.NewMongoCompanyRepository(db)
	membershipRepo := companies.NewMongoMembershipRepository(db)

	// --- Repositories (Milestone 2: clients, projects, properties, spaces, work) ---
	clientRepo := clients.NewMongoClientRepository(db)
	projectRepo := projects.NewMongoProjectRepository(db)
	propertyRepo := properties.NewMongoPropertyRepository(db)
	spaceRepo := spaces.NewMongoSpaceRepository(db)
	workItemRepo := work.NewMongoWorkItemRepository(db)

	// --- Repositories (Milestone 3: materials, costs, labour) ---
	materialRepo := materials.NewMongoMaterialRepository(db)
	costItemRepo := costs.NewMongoCostItemRepository(db)
	workerRepo := labour.NewMongoWorkerRepository(db)
	labourEntryRepo := labour.NewMongoLabourEntryRepository(db)

	// --- Repositories (Milestone 4: estimates) ---
	estimateRepo := estimates.NewMongoEstimateRepository(db)

	// --- Repositories (Milestone 5: quotations) ---
	quotationRepo := quotations.NewMongoQuotationRepository(db)
	quotationCounterRepo := quotations.NewMongoQuotationCounterRepository(db)

	// --- Repositories (Milestone 6: client access, approvals, audit) ---
	accessGrantRepo := access.NewMongoAccessGrantRepository(db)
	accessGroupStateRepo := access.NewMongoAccessGroupStateRepository(db)
	approvalRepo := approvals.NewMongoApprovalRepository(db)
	auditEventRepo := audit.NewMongoEventRepository(db)

	// --- Repositories (Milestone 7: procurement foundation) ---
	materialRequirementRepo := materialrequirements.NewMongoMaterialRequirementRepository(db)
	rfqRepo := rfqs.NewMongoRFQRepository(db)
	rfqCounterRepo := rfqs.NewMongoRFQCounterRepository(db)
	supplierRepo := suppliers.NewMongoSupplierRepository(db)
	supplierOfferingRepo := suppliers.NewMongoSupplierOfferingRepository(db)
	preferenceRepo := suppliers.NewMongoPreferenceRepository(db)

	// --- Repositories (Milestone 8: external procurement) ---
	issuedRFQVersionRepo := rfqissuance.NewMongoIssuedRFQVersionRepository(db)
	issuanceChainRepo := rfqissuance.NewMongoIssuanceChainRepository(db)
	amendmentDraftRepo := rfqissuance.NewMongoAmendmentDraftRepository(db)
	supplierInvitationRepo := rfqissuance.NewMongoInvitationRepository(db)
	invitationDeliveryRepo := rfqissuance.NewMongoDeliveryAttemptRepository(db)
	supplierAccessExchangeRepo := supplieraccess.NewMongoAccessExchangeRepository(db)
	verificationChallengeRepo := supplieraccess.NewMongoVerificationChallengeRepository(db)
	verificationDeliveryRepo := supplieraccess.NewMongoVerificationDeliveryRepository(db)
	verificationRateLimitRepo := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	supplierSessionRepo := supplieraccess.NewMongoSupplierSessionRepository(db)
	sessionInvitationBindingRepo := supplieraccess.NewMongoSessionInvitationBindingRepository(db)

	// --- Repositories (Milestone 8 Phase E: Supplier Offers) ---
	offerChainRepo := supplieroffers.NewMongoOfferChainRepository(db)
	offerDraftRepo := supplieroffers.NewMongoOfferDraftRepository(db)
	offerVersionRepo := supplieroffers.NewMongoOfferVersionRepository(db)
	offerEligibilityRepo := supplieroffers.NewMongoOfferEligibilityRepository(db)
	offerWithdrawalRepo := supplieroffers.NewMongoOfferWithdrawalRepository(db)

	// --- Repositories (M8.5B-A: AI Scope & Resource Preview) ---
	workResourceRequirementRepo := workresources.NewMongoRepository(db)
	aiRepo := ai.NewMongoRepository(db)

	// --- Repositories (M8.5C: Spatial Intelligence) ---
	spatialCaptureRepo := spatial.NewMongoCaptureRepository(db)
	spatialRoomVersionRepo := spatial.NewMongoRoomVersionRepository(db)
	spatialSpaceStateRepo := spatial.NewMongoSpaceStateRepository(db)
	spatialArtifactRepo := spatial.NewMongoArtifactRepository(db)
	spatialRoomDraftRepo := spatial.NewMongoRoomDraftRepository(db)
	spatialRoomDraftEditRepo := spatial.NewMongoRoomDraftEditRepository(db)
	spatialVisualAssetVersionRepo := spatial.NewMongoVisualAssetVersionRepository(db)
	// spatialDesignRepo (RP4E1) owns BOTH spatial_design_sessions and
	// spatial_design_turns — one repository, matching
	// MongoRoomDraftEditRepository's "one type owns both concerns since
	// they share the same transaction boundary" precedent.
	spatialDesignRepo := spatial.NewMongoDesignRepository(db)
	// spatialDesignGenerationRepo (RP4E2/M8.5C) owns
	// spatial_design_generation_attempts and spatial_design_acceptances,
	// and additionally implements the atomic Use Design transaction against
	// spatial_room_drafts/spatial_room_draft_edits/spatial_design_sessions —
	// same "one type owns every collection sharing a transaction boundary"
	// precedent as spatialRoomDraftEditRepo/spatialDesignRepo above.
	spatialDesignGenerationRepo := spatial.NewMongoDesignGenerationRepository(db)

	// Milestone 8 Phase F: awards.
	awardChainRepo := awards.NewMongoAwardChainRepository(db)
	awardDraftRepo := awards.NewMongoAwardDraftRepository(db)
	awardRevisionRepo := awards.NewMongoAwardRevisionRepository(db)
	awardLineClaimRepo := awards.NewMongoAwardLineClaimRepository(db)
	awardOutcomeRepo := awards.NewMongoAwardOutcomeRepository(db)
	awardDeliveryRepo := awards.NewMongoAwardDeliveryRepository(db)
	awardAcknowledgementRepo := awards.NewMongoAwardAcknowledgementRepository(db)

	indexCtx, cancelIndexCtx := context.WithTimeout(ctx, 10*time.Second)
	defer cancelIndexCtx()
	if err := userRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure users indexes: %w", err)
	}
	if err := sessionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure auth_sessions indexes: %w", err)
	}
	if err := membershipRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure company_members indexes: %w", err)
	}
	if err := clientRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure clients indexes: %w", err)
	}
	if err := projectRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure projects indexes: %w", err)
	}
	if err := propertyRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure properties indexes: %w", err)
	}
	if err := spaceRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spaces indexes: %w", err)
	}
	if err := workItemRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure work_items indexes: %w", err)
	}
	if err := materialRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure materials indexes: %w", err)
	}
	if err := costItemRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure cost_items indexes: %w", err)
	}
	if err := workerRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure workers indexes: %w", err)
	}
	if err := labourEntryRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure labour_entries indexes: %w", err)
	}
	if err := estimateRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure estimates indexes: %w", err)
	}
	if err := quotationRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure quotations indexes: %w", err)
	}
	if err := quotationCounterRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure quotation_counters indexes: %w", err)
	}
	if err := accessGrantRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure access_grants indexes: %w", err)
	}
	if err := accessGroupStateRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure access_group_states indexes: %w", err)
	}
	if err := approvalRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure approvals indexes: %w", err)
	}
	if err := materialRequirementRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure material requirement indexes: %w", err)
	}
	if err := rfqRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure rfq indexes: %w", err)
	}
	if err := rfqCounterRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure rfq counter indexes: %w", err)
	}
	if err := supplierRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier indexes: %w", err)
	}
	if err := supplierOfferingRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier offering indexes: %w", err)
	}
	if err := preferenceRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure preferred supplier indexes: %w", err)
	}
	if err := auditEventRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure audit_events indexes: %w", err)
	}
	if err := issuedRFQVersionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure issued rfq version indexes: %w", err)
	}
	if err := issuanceChainRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure rfq issuance chain indexes: %w", err)
	}
	if err := amendmentDraftRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure rfq amendment draft indexes: %w", err)
	}
	if err := supplierInvitationRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier invitation indexes: %w", err)
	}
	if err := invitationDeliveryRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure invitation delivery attempt indexes: %w", err)
	}
	if err := supplierAccessExchangeRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier access exchange indexes: %w", err)
	}
	if err := verificationChallengeRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier verification challenge indexes: %w", err)
	}
	if err := verificationDeliveryRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier verification delivery indexes: %w", err)
	}
	if err := verificationRateLimitRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier verification rate-limit indexes: %w", err)
	}
	if err := offerChainRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier_offer_chains indexes: %w", err)
	}
	if err := offerDraftRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier_offer_drafts indexes: %w", err)
	}
	if err := offerVersionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier_offer_versions indexes: %w", err)
	}
	if err := offerEligibilityRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier_offer_eligibilities indexes: %w", err)
	}
	// G5 is the sole transaction exception in M8. Fail startup clearly instead
	// of serving a withdrawal path whose required atomic boundary cannot run.
	if err := offerEligibilityRepo.VerifyWithdrawalTransactionSupport(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: MongoDB topology does not support transactional withdrawal claims: %w", err)
	}
	if err := offerWithdrawalRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier_offer_withdrawals indexes: %w", err)
	}
	if err := awardChainRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_chains indexes: %w", err)
	}
	if err := awardDraftRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_drafts indexes: %w", err)
	}
	if err := awardRevisionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_revisions indexes: %w", err)
	}
	if err := awardLineClaimRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_line_claims indexes: %w", err)
	}
	if err := awardOutcomeRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_outcomes indexes: %w", err)
	}
	if err := awardDeliveryRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_deliveries indexes: %w", err)
	}
	if err := awardAcknowledgementRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure award_acknowledgements indexes: %w", err)
	}
	if err := supplierSessionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier session indexes: %w", err)
	}
	if err := sessionInvitationBindingRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure supplier session binding indexes: %w", err)
	}
	if err := workResourceRequirementRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure work_resource_requirements indexes: %w", err)
	}
	if err := aiRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure ai generation batch/suggestion indexes: %w", err)
	}
	if err := spatialCaptureRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_captures indexes: %w", err)
	}
	if err := spatialRoomVersionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_room_versions indexes: %w", err)
	}
	if err := spatialSpaceStateRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_space_states indexes: %w", err)
	}
	if err := spatialArtifactRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_artifacts indexes: %w", err)
	}
	if err := spatialRoomDraftRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_room_drafts indexes: %w", err)
	}
	if err := spatialVisualAssetVersionRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_visual_asset_versions indexes: %w", err)
	}
	if err := spatialRoomDraftEditRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_room_draft_edits indexes: %w", err)
	}
	// spatialDesignRepo's indexes are created unconditionally, even when
	// live GLM reasoning is not configured (mock provider still needs
	// durable session/turn persistence, RP4E1 plan Task 8 Step 4).
	if err := spatialDesignRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_design_sessions/spatial_design_turns indexes: %w", err)
	}
	if err := spatialDesignGenerationRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial_design_generation_attempts/spatial_design_acceptances indexes: %w", err)
	}

	// --- Composition root: identity/companies wiring (unchanged from Milestone 1) ---
	mailer := selectMailer(cfg)
	if options.mailer != nil {
		mailer = options.mailer
	}
	userService := identity.NewUserService(userRepo)
	companiesService := companies.NewService(companyRepo, membershipRepo, userService, mailer)

	jwtSecret := []byte(cfg.JWTAccessSecret)
	jwtIssuer := identity.NewJWTIssuer(jwtSecret, accessTokenTTL)
	authService := identity.NewAuthService(userService, sessionRepo, companiesService, companiesService, jwtIssuer, refreshTokenTTL)

	registrationVerificationRepo := identity.NewMongoRegistrationVerificationRepository(db)
	if err := registrationVerificationRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure registration_verification_challenges indexes: %w", err)
	}
	registrationVerificationService := identity.NewRegistrationVerificationService(registrationVerificationRepo, mailer)

	// --- Composition root: Milestone 2 wiring. Strictly acyclic construction
	// order — clients -> projects -> {properties, spaces} -> work — matching
	// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md
	// §9. Each service is fully constructible in one pass; no setter-injection,
	// no two-phase construction, no cycle. properties and spaces are both
	// constructed from projectsService independently and never reference each
	// other.
	clientsService := clients.NewService(clientRepo)
	projectsService := projects.NewService(projectRepo, clientsService)
	propertiesService := properties.NewService(propertyRepo, projectsService)
	spacesService := spaces.NewService(spaceRepo, projectsService)
	workService := work.NewService(workItemRepo, projectsService, spacesService)

	// --- Composition root: M8.5C wiring. spatial consumes SpaceLookup from
	// spacesService (extended with SpaceBelongsToProject, already used by
	// work) — matching
	// docs/superpowers/specs/M8.5C-Spatial-Intelligence-Design-Spec-Refined.md
	// §3. spatial never imports spaces; spacesService satisfies
	// spatial.SpaceLookup structurally.
	//
	// Object store selection (M8.5C/RP4E2 Gate 2): cfg.ObjectStoreProvider
	// picks ONE physical backend shared by every spatial adapter below
	// (artifacts, visual assets, asset-generation staging, design
	// reference images) — "local" keeps the three separate on-disk roots
	// dev has always used (LocalObjectStore has no shared-instance
	// requirement, so three physical directories is simplest); "r2" wires
	// exactly one shared R2ObjectStore/R2Presigner pair, matching R2's own
	// "sole durable object store" plan requirement and R2ObjectStore's own
	// doc comment ("every spatial adapter... shares ONE R2ObjectStore
	// instance"). cfg.LoadFromEnv already validates the R2_* fields are
	// present together when ObjectStoreProvider=r2, so no further
	// validation is needed here.
	spatialService := spatial.NewService(spatialCaptureRepo, spatialRoomVersionRepo, spatialSpaceStateRepo, spacesService)
	var (
		spatialSharedObjectStore   objectstore.ObjectStore
		spatialSourceAccessAdapter spatial.AssetGenerationSourceAccessProvider
		spatialReferenceAccess     spatial.DesignReferenceSourceAccessProvider
	)
	if cfg.ObjectStoreProvider == "r2" {
		r2Store := objectstore.NewR2ObjectStore(cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket, cfg.R2Endpoint)
		r2Presigner := objectstore.NewR2Presigner(cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket, cfg.R2Endpoint)
		spatialSharedObjectStore = r2Store
		spatialSourceAccessAdapter = NewR2AssetGenerationSourceAccessProvider(r2Presigner)
		spatialReferenceAccess = NewR2DesignReferenceSourceAccessProvider(r2Presigner)
		spatialService.SetVisualAssetReadAccessProvider(NewR2VisualAssetReadAccessProvider(r2Presigner))
	} else {
		spatialSharedObjectStore = objectstore.NewLocalObjectStore(cfg.StorageLocalPath + "/spatial-artifacts")
	}
	spatialService.SetArtifactSupport(spatialArtifactRepo, NewSpatialArtifactStoreAdapter(spatialSharedObjectStore))
	spatialService.SetRoomDraftSupport(spatialRoomDraftRepo)
	spatialService.SetRoomDraftEditSupport(spatialRoomDraftEditRepo)
	spatialVisualAssetObjectStore := spatialSharedObjectStore
	if cfg.ObjectStoreProvider != "r2" {
		spatialVisualAssetObjectStore = objectstore.NewLocalObjectStore(cfg.StorageLocalPath + "/spatial-visual-assets")
	}
	spatialService.SetVisualAssetSupport(spatialVisualAssetVersionRepo, NewSpatialVisualAssetStoreAdapter(spatialVisualAssetObjectStore))
	spatialService.SetVisualAssetAccessTTL(cfg.VisualAssetAccessTTL)
	// VisualAssetCapabilityKeys is optional-until-referenced (RP4D) —
	// unlike Phase D's supplier keyrings above, an empty configuration
	// here means "the asset-access routes simply are not available yet,"
	// not a startup failure. Once configured, a malformed/undersized key
	// DOES fail startup (NewVisualAssetCapabilityKeyring's own
	// validation), matching the "never silently serve unsigned content"
	// requirement. In R2 mode, SetVisualAssetReadAccessProvider was
	// already called above with the R2 provider — this local-HMAC path is
	// R2-mode-exclusive-alternative, never both.
	if cfg.ObjectStoreProvider != "r2" && len(cfg.VisualAssetCapabilityKeys) > 0 {
		visualAssetCapabilityKeyring, err := secrets.NewVisualAssetCapabilityKeyring(
			cfg.VisualAssetCapabilityActiveVersion, cfg.VisualAssetCapabilityKeys)
		if err != nil {
			return nil, fmt.Errorf("composition: construct the visual asset capability keyring: %w", err)
		}
		spatialService.SetVisualAssetReadAccessProvider(
			NewLocalHMACReadAccessProvider(visualAssetCapabilityKeyring, cfg.ExternalAPIBaseURL))
	}

	// --- Composition root: RP4E0 wiring. Provider-neutral asset
	// generation is wired ONLY if BOTH the Hugging Face client config AND
	// (in local mode) the source-image capability keyring are present —
	// matching RP4D's exact "empty means not configured yet,
	// malformed/undersized fails closed" precedent. In R2 mode, source
	// access uses real R2 presigned URLs instead (spatialSourceAccessAdapter,
	// constructed above) — Hunyuan's Gradio Space must be able to fetch the
	// source image directly, which only a real internet-reachable
	// presigned URL (never a local dev server's own private route)
	// actually satisfies in a deployed tester environment.
	// cfg.HuggingFaceSpaceURL empty means the whole feature stays
	// unconfigured regardless of object store mode.
	spatialAssetGenerationJobRepo := spatial.NewMongoAssetGenerationJobRepository(db)
	if err := spatialAssetGenerationJobRepo.EnsureIndexes(indexCtx); err != nil {
		return nil, fmt.Errorf("composition: ensure spatial asset generation job indexes: %w", err)
	}
	spatialAssetGenerationStagingStore := spatialSharedObjectStore
	if cfg.ObjectStoreProvider != "r2" {
		spatialAssetGenerationStagingStore = objectstore.NewLocalObjectStore(cfg.StorageLocalPath + "/spatial-asset-generation")
	}
	if cfg.ObjectStoreProvider != "r2" && len(cfg.AssetGenerationSourceCapabilityKeys) > 0 {
		sourceKeyring, err := secrets.NewAssetGenerationSourceCapabilityKeyring(
			cfg.AssetGenerationSourceCapabilityActiveVersion, cfg.AssetGenerationSourceCapabilityKeys)
		if err != nil {
			return nil, fmt.Errorf("composition: construct the asset generation source capability keyring: %w", err)
		}
		spatialSourceAccessAdapter = NewLocalAssetGenerationSourceAccessProvider(sourceKeyring, cfg.ExternalAPIBaseURL)
	}
	if cfg.HuggingFaceSpaceURL != "" && spatialSourceAccessAdapter != nil {
		hunyuanClient := hunyuan.NewClient(cfg.HuggingFaceSpaceURL, cfg.HuggingFaceToken, cfg.HunyuanProviderTimeout)
		spatialService.SetAssetGenerationSupport(
			spatialAssetGenerationJobRepo,
			NewSpatialAssetGenerationStagingStoreAdapter(spatialAssetGenerationStagingStore),
			NewHunyuanProviderAdapter(hunyuanClient),
			spatialSourceAccessAdapter,
		)
		spatialService.SetAssetGenerationTimings(cfg.AssetGenerationLeaseTTL, cfg.AssetGenerationHeartbeatInterval, cfg.HunyuanProviderTimeout)
	}

	// --- Composition root: RP4E1 wiring. Persistence is wired
	// unconditionally (mock spatial reasoning is the default, matching
	// AI_PROVIDER=mock's own "never requires a real API key" precedent).
	// The Python-backed reasoner uses the SAME AI_SERVICE_URL/
	// AI_INTERNAL_TOKEN as the existing Copilot aiClient (RP4E1's internal
	// route lives on the same FastAPI deployment) but a DEDICATED timeout
	// (AISpatialServiceTimeout) and a SEPARATE SpatialClient — Copilot's
	// decoding behavior is never touched.
	spatialReasoningClient := platformai.NewSpatialClientWithLogger(cfg.AIServiceURL, cfg.AIInternalToken, cfg.AISpatialServiceTimeout, logger)
	spatialService.SetDesignPlanningSupport(spatialDesignRepo, spatialDesignRepo, NewSpatialReasoningAdapter(spatialReasoningClient))
	// RP4E2/M8.5C Gate 1: attempt/acceptance persistence and atomic Use
	// Design are wired unconditionally, same "persistence always available,
	// even before the provider-facing pieces (Gate 2's reference/Hunyuan
	// submission) exist" precedent as SetDesignPlanningSupport above.
	spatialService.SetDesignGenerationSupport(spatialDesignGenerationRepo, spatialDesignGenerationRepo, spatialDesignGenerationRepo)
	// RP4E2 Gate 2: the worker-only reference-generation dependencies are
	// wired ONLY when both the shared AI service is configured AND R2 is
	// the object store (spatialReferenceAccess is nil in local mode — see
	// this function's object-store-selection comment above for why local
	// mode cannot meaningfully serve a real Hunyuan Space a fetchable
	// source URL). Missing either leaves ProcessOneDesignGenerationAttempt
	// returning ErrDesignGenerationWorkerNotConfigured cleanly, the same
	// "not yet configured, not a startup failure" precedent as every other
	// optional capability in this file.
	if cfg.AIServiceURL != "" && spatialReferenceAccess != nil {
		referenceImageClient := platformai.NewReferenceImageClient(cfg.AIServiceURL, cfg.AIInternalToken, cfg.ReferenceImageMaxBytes, cfg.AISpatialServiceTimeout)
		spatialService.SetDesignGenerationWorkerSupport(
			NewReferenceImageAdapter(referenceImageClient),
			NewSpatialDesignReferenceStoreAdapter(spatialSharedObjectStore),
			spatialReferenceAccess,
			cfg.AISpatialServiceTimeout,
		)
	}
	// Bounded interrupted-turn recovery runs once at startup: any turn left
	// in "reasoning" (providerStartedAt set, no terminal result) longer
	// than the recovery threshold is marked needs_attention and its
	// session's ActiveTurnID cleared — it is NEVER redispatched (RP4E1
	// plan Task 8 Step 4). A failure here does not fail startup: a stuck
	// "reasoning" turn is already safe (at-most-once dispatch already
	// happened or didn't), just not yet marked — recovery is a cleanup
	// pass, not a correctness precondition for serving traffic.
	if _, err := spatialDesignRepo.RecoverInterruptedTurns(indexCtx, cfg.AISpatialServiceTimeout*3); err != nil {
		logger.Warn().Err(err).Msg("composition: recovering interrupted spatial design turns at startup")
	}
	// RP4E2 Gate 4: the queue-wake publisher is wired only when the Web
	// project's enqueue bridge is actually configured — local development
	// has no Vercel Queue and relies on StartAssetGenerationDispatchLoop-
	// style polling instead (same "absent config == feature unavailable"
	// convention as every other optional capability here). A misconfigured
	// or unreachable Web deployment never fails a request: notifyGenerationWake
	// swallows publish errors (see its own doc comment).
	if cfg.SpatialQueueEnqueueURL != "" {
		spatialService.SetGenerationWakePublisher(NewSpatialGenerationWakeClient(cfg.SpatialQueueEnqueueURL, cfg.SpatialQueueEnqueueToken, cfg.AISpatialServiceTimeout))
	}

	// --- Composition root: Milestone 3 wiring. Acyclic construction order —
	// materials (no M3 deps) -> costs (consumes MaterialLookup, ProjectLookup,
	// WorkItemLookup) -> labour (consumes ProjectLookup, WorkItemLookup,
	// LabourCostRecorder) — matching
	// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md
	// §8. labour must be constructed after costs since it depends on
	// costsService satisfying labour.LabourCostRecorder.
	materialsService := materials.NewService(materialRepo)
	costsService := costs.NewService(costItemRepo, projectsService, workService, materialsService)
	labourService := labour.NewService(workerRepo, labourEntryRepo, projectsService, workService, costsService)

	// --- Composition root: Milestone 4 wiring. estimates consumes
	// ProjectLookup (from projectsService, unchanged) and
	// EstimatedCostSource (from costsService's VisitEstimatedCostItems
	// method) — matching
	// docs/superpowers/specs/2026-07-23-milestone-4-estimates-design.md §12.
	estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)

	// --- Composition root: Milestone 5 wiring. quotations consumes
	// ProjectLookup (from projectsService, extended with
	// GetProjectClientID), WorkItemLookup (from workService, extended with
	// GetWorkItemDescription), and FinalizedEstimateSource (from
	// estimatesService's VisitQuotationSeeds method, which performs the
	// cost-grouping and proportional-allocation computation internally —
	// matching
	// docs/superpowers/specs/2026-07-24-milestone-5-quotations-design.md §22.
	quotationsService := quotations.NewService(quotationRepo, quotationCounterRepo, projectsService, workService, estimatesService)

	// --- Composition root: Milestone 6 wiring (client access, approvals, audit).
	// approvals and audit are subject-agnostic — neither knows anything about
	// quotation chains, versions, or supersession. access owns all chain
	// coordination through its AccessGroupState coordinator document, which is
	// the single serialization point for every share/rotate/accept race.
	//
	// The two composition adapters below are the ONLY places in the codebase
	// that import two domain modules at once. They exist because Go interface
	// satisfaction requires exact return types: quotations.Service naturally
	// returns quotations.Quotation, while access declares its own
	// ShareableQuotationSnapshot (the allowlisted projection). The adapter
	// performs that conversion here, at the leaf of the dependency graph, so
	// neither domain module gains a dependency ADR 0002 forbids.
	approvalsService := approvals.NewService(approvalRepo)
	auditService := audit.NewService(auditEventRepo)
	accessService := access.NewService(
		accessGrantRepo, accessGroupStateRepo,
		NewQuotationSourceAdapter(quotationsService),
		projectsService,
		NewApprovalsAdapter(approvalsService),
		companiesService,
		auditService,
		NewCleanupLogger(logger),
		cfg.ExternalAPIBaseURL,
	)

	// --- Milestone 7: procurement foundation ---
	// Three modules, one-way: rfqs -> materialrequirements, through the narrow
	// MaterialRequirementSource capability only. The adapter below is the ONLY
	// place importing both (ADR 0002, design spec sections 1.1 and 1.4.1).
	//
	// costs, work and materials satisfy their capabilities DIRECTLY: each
	// returns primitives, which is why no further adapter is needed.
	materialRequirementsService := materialrequirements.NewService(
		materialRequirementRepo, projectsService, workService, materialsService,
		costsService, auditService,
	)
	rfqsService := rfqs.NewService(
		rfqRepo, rfqCounterRepo, projectsService,
		NewMaterialRequirementSourceAdapter(materialRequirementsService),
		// M7 issues nothing, so reopen always succeeds. M8 swaps in the real
		// adapter with no change to service logic (design spec section 6.4).
		rfqs.NoExternalIssuanceSource{},
		auditService,
	)
	suppliersService := suppliers.NewService(
		supplierRepo, supplierOfferingRepo, preferenceRepo, materialsService, auditService,
	)

	// --- Milestone 8: external procurement and Supplier participation ---
	// Phase A wires the foundations only: the invitation-secret keyring and the
	// two adapters that close the M7/M8 issuance cycle. Invitations, Supplier
	// access, offers and awards join this block in later phases.
	//
	// The keyring is constructed HERE, at startup, because a missing, malformed
	// or undersized key must fail the process rather than the first invitation
	// (M8 design spec §6.1A). It is never auto-generated: a per-boot key would
	// make every previously issued invitation link unreproducible.
	invitationKeyring, err := secrets.NewInvitationKeyring(
		cfg.InvitationSecretActiveVersion, cfg.InvitationSecretKeys)
	if err != nil {
		return nil, fmt.Errorf("composition: construct the invitation secret keyring: %w", err)
	}

	// rfqissuance reads the M7 ready RFQ through an M8-owned capability, so it
	// imports no M7 type at all (design spec §2.1).
	rfqIssuanceService := rfqissuance.NewService(issuedRFQVersionRepo,
		rfqissuance.WithReadyRFQSource(NewReadyRFQSourceAdapter(rfqsService)),
		rfqissuance.WithIssuanceChains(issuanceChainRepo),
		rfqissuance.WithAmendmentDrafts(amendmentDraftRepo),
		rfqissuance.WithAuditRecorder(auditService),
		// --- Milestone 8 Phase C: invitations and delivery ---
		rfqissuance.WithInvitations(supplierInvitationRepo),
		rfqissuance.WithDeliveryAttempts(invitationDeliveryRepo),
		// The Supplier Directory is reached through a narrow one-boolean
		// capability, so rfqissuance names no suppliers type (ADR 0002).
		rfqissuance.WithSupplierLookup(NewSupplierInvitabilityAdapter(suppliersService)),
		rfqissuance.WithInvitationKeyring(invitationKeyring),
		rfqissuance.WithMailer(mailer),
		rfqissuance.WithSupplierLinkBaseURL(cfg.ExternalAPIBaseURL),
	)

	verificationCodeKeyring, err := secrets.NewSupplierVerificationCodeKeyring(
		cfg.SupplierVerificationCodeActiveVersion,
		cfg.SupplierVerificationCodeKeys)
	if err != nil {
		return nil, fmt.Errorf("composition: construct the Supplier verification-code keyring: %w", err)
	}
	supplierSessionKeyring, err := secrets.NewSupplierSessionTokenKeyring(
		cfg.SupplierSessionTokenActiveVersion, cfg.SupplierSessionTokenKeys)
	if err != nil {
		return nil, fmt.Errorf("composition: construct the Supplier session-token keyring: %w", err)
	}
	rateFingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(
		cfg.SupplierRateLimitFingerprintKey)
	if err != nil {
		return nil, fmt.Errorf("composition: construct the Supplier rate-limit fingerprinter: %w", err)
	}
	invitationAccess := NewInvitationAccessAdapter(rfqIssuanceService)
	supplierTokens := supplieraccess.CryptographicOpaqueTokenGenerator{}
	verificationRateLimiter := supplieraccess.NewVerificationRateLimiter(
		verificationRateLimitRepo, supplierTokens)
	supplierAccessService := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(invitationAccess, invitationAccess),
		supplieraccess.WithAccessExchangeStore(supplierAccessExchangeRepo),
		supplieraccess.WithOpaqueTokenGenerator(supplierTokens),
		supplieraccess.WithVerificationStores(
			verificationChallengeRepo, verificationDeliveryRepo),
		supplieraccess.WithVerificationSecurity(
			verificationCodeKeyring, rateFingerprinter,
			verificationRateLimiter),
		supplieraccess.WithVerificationMailer(mailer),
		supplieraccess.WithSessionStores(
			supplierSessionRepo, sessionInvitationBindingRepo),
		supplieraccess.WithSessionSecurity(supplierSessionKeyring),
		supplieraccess.WithTrustedProxyCIDRs(cfg.TrustedProxyCIDRs),
		supplieraccess.WithAuditRecorder(auditService),
	)

	// Close the read-only RFQ/session cycle after both owners exist. Supplier
	// RFQ routes obtain Company and Supplier scope only through this adapter.
	rfqIssuanceService.SetSupplierRFQAccessAuthorizer(
		NewSupplierRFQAccessAdapter(supplierAccessService))

	// Closing the M7/M8 cycle (design spec §1A.3). rfqs was constructed above
	// with NoExternalIssuanceSource; now that rfqissuance exists, the real
	// adapter replaces it. Without this call an issued chain would stay
	// reopenable underneath a Supplier already quoting against it.
	rfqsService.SetIssuanceStatusSource(NewIssuanceStatusAdapter(rfqIssuanceService))

	// --- Milestone 8 Phase E: Supplier Offers ---
	supplierOffersService := supplieroffers.NewService(
		// Company, Supplier and recipient come ONLY from Phase D authorization;
		// no Supplier Offer route accepts them as input.
		supplieroffers.WithSupplierOfferAccessAuthorizer(
			NewSupplierOfferAccessAdapter(supplierAccessService)),
		supplieroffers.WithIssuedRFQSource(
			NewIssuedRFQOfferSourceAdapter(rfqIssuanceService)),
		supplieroffers.WithSupplierOfferChainRepository(offerChainRepo),
		supplieroffers.WithSupplierOfferDraftRepository(offerDraftRepo),
		supplieroffers.WithSupplierOfferVersionRepository(offerVersionRepo),
		supplieroffers.WithSupplierOfferEligibilityRepository(offerEligibilityRepo),
		supplieroffers.WithSupplierOfferWithdrawalRepository(offerWithdrawalRepo),
		// Phase G supplies the concrete primitive-only recorder; commercial
		// content cannot cross the consumer-owned audit capability.
		supplieroffers.WithSupplierOfferAuditRecorder(auditService),
	)

	// Closing the M8 issuance/offers cycle: recipient replacement must claim the
	// Supplier's offer workspace BEFORE the authoritative invitation write, so a
	// replaced recipient can never keep editing or submitting (design spec
	// §5.3A). Without this call replacement would proceed unguarded.
	rfqIssuanceService.SetOfferWorkspaceCoordinator(
		NewOfferWorkspaceAdapter(supplierOffersService))

	// --- Milestone 8 Phase F: comparison and awards ---
	//
	// awards reaches rfqissuance and supplieroffers ONLY through the
	// consumer-owned capabilities these adapters satisfy (§8A.1). It never
	// reads another module's collection.
	awardsService := awards.NewService(
		awards.WithIssuedRFQSource(
			NewIssuedRFQAwardSourceAdapter(rfqIssuanceService)),
		awards.WithOfferVersionSource(
			NewOfferVersionAwardSourceAdapter(
				offerVersionRepo, offerChainRepo, offerEligibilityRepo,
				supplierInvitationRepo, suppliersService)),
		// The no-Award-Revision-exists check lives in the awards finalisation
		// service, not in this adapter and not in supplieroffers (§8E).
		awards.WithOfferEligibilityClaimant(
			NewOfferEligibilityClaimantAdapter(offerEligibilityRepo)),
		awards.WithAwardChainRepository(awardChainRepo),
		awards.WithAwardDraftRepository(awardDraftRepo),
		awards.WithAwardRevisionRepository(awardRevisionRepo),
		awards.WithAwardLineClaimRepository(awardLineClaimRepo),
		awards.WithAwardOutcomeRepository(awardOutcomeRepo),
		awards.WithAwardDeliveryRepository(awardDeliveryRepo),
		awards.WithAwardAcknowledgementRepository(awardAcknowledgementRepo),
		awards.WithAwardAuditRecorder(auditService),
		awards.WithAwardNotificationMailer(
			NewAwardNotificationMailerAdapter(mailer, cfg.ExternalAPIBaseURL)),
		awards.WithInvitationLinkSource(
			NewInvitationLinkSourceAdapter(rfqIssuanceService)),
	)

	// Closing the Phase D/F boundary: the Supplier outcome read and
	// acknowledgement sit behind the Phase D session, so supplieraccess owns
	// those routes and reaches awards through this capability (D3, §8A.1).
	supplierAccessService.SetSupplierOutcomeSource(
		NewSupplierOutcomeSourceAdapter(awardsService))

	// --- M8.5B-A: AI Scope & Resource Preview ---
	//
	// Construction order (design doc Task 11): existing domain services ->
	// workresources service -> AI persistence repository -> platform AI HTTP
	// client -> aiintegration adapters (existing services + workresources) ->
	// internal/ai service -> Huma handlers. workresources depends on
	// projects/work/materials, all already constructed above.
	workResourcesService := workresources.NewService(
		workResourceRequirementRepo, projectsService, workService, materialsService,
	)

	// aiService is constructed unconditionally so the router always exposes a
	// consistent route set; when AI_SERVICE_URL/AI_INTERNAL_TOKEN are unset,
	// aiClient's calls fail with ErrServiceUnavailable rather than the process
	// refusing to start, matching this design doc's "AI suggestions
	// temporarily unavailable" contractor-facing failure mode instead of an
	// all-or-nothing startup gate for a preview-milestone feature.
	aiClient := platformai.NewClient(cfg.AIServiceURL, cfg.AIInternalToken, cfg.AIServiceTimeout)
	aiDomainAdapter := aiintegration.NewDomainGatewayAdapter(aiintegration.NewAdapter(
		projectsService, spacesService, workService, materialsService, workResourcesService,
	))
	aiService := ai.NewService(aiRepo, aiDomainAdapter, aiClient)
	aiService.SetSpaceCreator(aiintegration.NewSpaceAcceptanceAdapter(spacesService))
	aiService.SetWorkItemCreator(aiintegration.NewWorkItemAcceptanceAdapter(workService))
	aiService.SetResourceCreator(aiintegration.NewResourceAcceptanceAdapter(workResourcesService, materialsService))

	return &Services{
		Auth: authService, Users: userService, JWTIssuer: jwtIssuer,
		RegistrationVerification: registrationVerificationService,
		Companies:                companiesService, Clients: clientsService,
		Projects: projectsService, Properties: propertiesService,
		Spaces: spacesService, Spatial: spatialService, Work: workService, Materials: materialsService,
		Costs: costsService, Labour: labourService, Estimates: estimatesService,
		Quotations: quotationsService, Approvals: approvalsService,
		Audit: auditService, Access: accessService,
		MaterialRequirements: materialRequirementsService, RFQs: rfqsService,
		Suppliers: suppliersService, RFQIssuance: rfqIssuanceService,
		SupplierAccess: supplierAccessService, SupplierOffers: supplierOffersService,
		Awards: awardsService, WorkResources: workResourcesService, AI: aiService,
		Mailer: mailer, InvitationKeyring: invitationKeyring,
		SupplierVerificationCodeKeyring: verificationCodeKeyring,
	}, nil
}
