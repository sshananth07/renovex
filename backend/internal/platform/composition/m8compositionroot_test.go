package composition_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M8's composition-root parity guard (design spec §1A.3).
//
// tenanttest exists so integration tests exercise the REAL composition. That
// only holds while both roots stay in sync: an M8 seam wired in main.go but not
// in tenanttest would ship exercised by nothing, and the drift would be silent.
//
// Since demo data seeding's Task 1 (docs/superpowers/plans/2026-08-18-demo-
// data-seeding.md), SERVICE-CONSTRUCTION calls in m8FoundationConstructors
// below are checked against composition/services.go (the one canonical
// composition root cmd/api/main.go and internal/tenanttest/router.go both
// delegate to), via wantedInServicesGo/assertBothEntrypointsDelegateToBuild-
// Services (compositionroot_test.go). Route-MOUNTING calls (Register*) stay
// a genuine per-entrypoint concern, checked against main.go/router.go
// directly via assertBothEntrypointsMakeEveryCall, since BuildServices does
// no HTTP wiring.
//
// Phase A wires the M8 FOUNDATIONS only — the keyring and the two adapters that
// close the M7/M8 cycle. Repositories, services and routes join this list as
// each later phase builds them.

// m8FoundationConstructors are the Phase A calls both roots must make.
var m8FoundationConstructors = []string{
	// The invitation-secret keyring. Constructed in the root because a missing
	// or malformed key must fail startup, not the first invitation (§6.1A).
	"secrets.NewInvitationKeyring",

	// The M7 -> M8 read seam.
	"composition.NewReadyRFQSourceAdapter",

	// The M8 -> M7 issuance seam, plus the setter that closes the cycle
	// without either module importing the other (§1A.3).
	"composition.NewIssuanceStatusAdapter",

	// Phase B repositories and the options that supply issuance its
	// collaborators. A root that constructs the service without these would
	// serve every issuance attempt ErrIssuanceNotConfigured.
	"rfqissuance.NewMongoIssuedRFQVersionRepository",
	"rfqissuance.NewMongoIssuanceChainRepository",
	"rfqissuance.NewMongoAmendmentDraftRepository",
	"rfqissuance.WithReadyRFQSource",
	"rfqissuance.WithIssuanceChains",
	"rfqissuance.WithAmendmentDrafts",
	"rfqissuance.WithAuditRecorder",

	// Phase B contractor routes. The service may be perfectly wired yet remain
	// unreachable if either authenticated Huma root forgets this call.
	"rfqissuance.RegisterHandlers",

	// Phase C invitations and delivery. A root missing any of these serves
	// every invitation call ErrInvitationsNotConfigured.
	"rfqissuance.NewMongoInvitationRepository",
	"rfqissuance.NewMongoDeliveryAttemptRepository",
	"rfqissuance.WithInvitations",
	"rfqissuance.WithDeliveryAttempts",
	"rfqissuance.WithSupplierLookup",
	"rfqissuance.WithInvitationKeyring",
	"rfqissuance.WithMailer",
	"rfqissuance.WithSupplierLinkBaseURL",
	"composition.NewSupplierInvitabilityAdapter",

	// Phase D's public Supplier access service. Both roots must construct the
	// same repositories, dedicated key material, narrow invitation adapter,
	// rate limiter, session security, audit recorder, and public routes.
	"supplieraccess.NewMongoAccessExchangeRepository",
	"supplieraccess.NewMongoVerificationChallengeRepository",
	"supplieraccess.NewMongoVerificationDeliveryRepository",
	"supplieraccess.NewMongoVerificationRateLimitRepository",
	"supplieraccess.NewMongoSupplierSessionRepository",
	"supplieraccess.NewMongoSessionInvitationBindingRepository",
	"secrets.NewSupplierVerificationCodeKeyring",
	"secrets.NewSupplierSessionTokenKeyring",
	"secrets.NewSupplierRateLimitFingerprinter",
	"composition.NewInvitationAccessAdapter",
	"supplieraccess.NewVerificationRateLimiter",
	"supplieraccess.NewService",
	"supplieraccess.WithInvitationAccess",
	"supplieraccess.WithAccessExchangeStore",
	"supplieraccess.WithVerificationStores",
	"supplieraccess.WithVerificationSecurity",
	"supplieraccess.WithVerificationMailer",
	"supplieraccess.WithSessionStores",
	"supplieraccess.WithSessionSecurity",
	"supplieraccess.WithTrustedProxyCIDRs",
	"supplieraccess.WithAuditRecorder",
	"supplieraccess.RegisterHandlers",

	// Phase G invitation-scoped Supplier RFQ reads close the reverse session
	// authorization seam only after both services have been constructed.
	"composition.NewSupplierRFQAccessAdapter",
	"rfqIssuanceService.SetSupplierRFQAccessAuthorizer",
	"rfqissuance.RegisterSupplierHandlers",

	// Phase E Supplier Offers. The Supplier-facing routes deliberately mount on
	// the base API because Phase D's session/CSRF middleware authenticates them.
	"supplieroffers.NewMongoOfferChainRepository",
	"supplieroffers.NewMongoOfferDraftRepository",
	"supplieroffers.NewMongoOfferVersionRepository",
	"supplieroffers.NewMongoOfferEligibilityRepository",
	"offerEligibilityRepo.VerifyWithdrawalTransactionSupport",
	"supplieroffers.NewMongoOfferWithdrawalRepository",
	"composition.NewSupplierOfferAccessAdapter",
	"composition.NewIssuedRFQOfferSourceAdapter",
	"supplieroffers.NewService",
	"supplieroffers.WithSupplierOfferAccessAuthorizer",
	"supplieroffers.WithIssuedRFQSource",
	"supplieroffers.WithSupplierOfferChainRepository",
	"supplieroffers.WithSupplierOfferDraftRepository",
	"supplieroffers.WithSupplierOfferVersionRepository",
	"supplieroffers.WithSupplierOfferEligibilityRepository",
	"supplieroffers.WithSupplierOfferWithdrawalRepository",
	"supplieroffers.WithSupplierOfferAuditRecorder",
	"composition.NewOfferWorkspaceAdapter",
	"rfqIssuanceService.SetOfferWorkspaceCoordinator",
	"supplieroffers.RegisterHandlers",
	"supplieroffers.RegisterReconciliationHandlers",

	// Phase F awards and the Phase D Supplier-outcome seam.
	"awards.NewMongoAwardChainRepository",
	"awards.NewMongoAwardDraftRepository",
	"awards.NewMongoAwardRevisionRepository",
	"awards.NewMongoAwardLineClaimRepository",
	"awards.NewMongoAwardOutcomeRepository",
	"awards.NewMongoAwardDeliveryRepository",
	"awards.NewMongoAwardAcknowledgementRepository",
	"composition.NewIssuedRFQAwardSourceAdapter",
	"composition.NewOfferVersionAwardSourceAdapter",
	"composition.NewOfferEligibilityClaimantAdapter",
	"composition.NewAwardNotificationMailerAdapter",
	"awards.NewService",
	"awards.WithIssuedRFQSource",
	"awards.WithOfferVersionSource",
	"awards.WithOfferEligibilityClaimant",
	"awards.WithAwardChainRepository",
	"awards.WithAwardDraftRepository",
	"awards.WithAwardRevisionRepository",
	"awards.WithAwardLineClaimRepository",
	"awards.WithAwardOutcomeRepository",
	"awards.WithAwardDeliveryRepository",
	"awards.WithAwardAcknowledgementRepository",
	"awards.WithAwardAuditRecorder",
	"awards.WithAwardNotificationMailer",
	"composition.NewSupplierOutcomeSourceAdapter",
	"supplierAccessService.SetSupplierOutcomeSource",
	"awards.RegisterHandlers",
}

// Every M8 repository must have EnsureIndexes called in the canonical
// composition root.
//
// A missing call is invisible until a unique index silently fails to protect an
// invariant — the one-draft-per-chain rule and the immutable-version identity
// both depend on it.
func TestBothCompositionRootsEnsureM8Indexes(t *testing.T) {
	repoVars := []string{
		"issuedRFQVersionRepo",
		"issuanceChainRepo",
		"amendmentDraftRepo",
		"supplierInvitationRepo",
		"invitationDeliveryRepo",
		"supplierAccessExchangeRepo",
		"verificationChallengeRepo",
		"verificationDeliveryRepo",
		"verificationRateLimitRepo",
		"supplierSessionRepo",
		"sessionInvitationBindingRepo",
		"offerChainRepo",
		"offerDraftRepo",
		"offerVersionRepo",
		"offerEligibilityRepo",
		"offerWithdrawalRepo",
		"awardChainRepo",
		"awardDraftRepo",
		"awardRevisionRepo",
		"awardLineClaimRepo",
		"awardOutcomeRepo",
		"awardDeliveryRepo",
		"awardAcknowledgementRepo",
	}

	path := servicesGoPath(t)
	calls := selectorCalls(t, path)
	for _, repo := range repoVars {
		if !calls[repo+".EnsureIndexes"] {
			t.Errorf("composition/services.go never calls %s.EnsureIndexes. Without "+
				"it the unique index does not exist and the invariant it protects "+
				"is unenforced", repo)
		}
	}
}

// Route mounting remains a genuine per-entrypoint concern — checked against
// main.go/router.go directly, since composition.BuildServices does no HTTP
// wiring.
func TestBothCompositionRootsMountSupplierAccessOnTheUnauthenticatedBaseAPI(
	t *testing.T) {

	root := backendRoot(t)
	for name, path := range map[string]string{
		"internal/platform/composition/httpapp.go": filepath.Join(root, "internal", "platform", "composition", "httpapp.go"),
		"internal/tenanttest/router.go":            filepath.Join(root, "internal", "tenanttest", "router.go"),
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		normalized := strings.Join(strings.Fields(string(source)), "")
		want := "supplieraccess.RegisterHandlers(api,services.SupplierAccess,"
		if !strings.Contains(normalized, want) {
			t.Errorf("%s does not mount Supplier access on the base API; "+
				"opaque-link and session-cookie routes must not require "+
				"a contractor bearer token", name)
		}
	}
}

// composition.BuildServices must make every M8 foundation SERVICE-
// CONSTRUCTION call — it is the one canonical composition root now.
// Route-mounting calls (Register*) are checked separately against the two
// HTTP entrypoints.
func TestBothCompositionRootsWireM8FoundationsIdentically(t *testing.T) {
	path := servicesGoPath(t)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("composition root services.go is missing: %v", err)
	}
	qualified := selectorCalls(t, path)
	bare := bareCallNames(t, path)

	// Setter calls on already-constructed services (e.g.
	// rfqIssuanceService.SetSupplierRFQAccessAuthorizer) still belong in
	// services.go, not main.go/router.go — they fall through to the
	// wantedInServicesGo branch below like every other construction call.
	// Only genuine Register* route-mounting calls are deferred to the two
	// HTTP entrypoints.
	var routeMountingCalls []string
	for _, want := range m8FoundationConstructors {
		if isRegisterHandlersCall(want) {
			routeMountingCalls = append(routeMountingCalls, want)
			continue
		}
		if !wantedInServicesGo(qualified, bare, want) {
			t.Errorf("composition/services.go does not call %s. The canonical "+
				"composition root must wire every M8 foundation call, or an "+
				"entrypoint delegating to it would silently lose functionality "+
				"(design spec §1A.3)", want)
		}
	}

	assertBothEntrypointsDelegateToBuildServices(t)
	assertBothEntrypointsMakeEveryCall(t, routeMountingCalls)
}

// The setter is what makes the M7/M8 cycle constructible. Without it, rfqs
// keeps NoExternalIssuanceSource forever and an issued RFQ chain stays
// reopenable underneath a Supplier who is already quoting against it.
func TestBothCompositionRootsCloseTheIssuanceCycleWithTheSetter(t *testing.T) {
	path := servicesGoPath(t)
	calls := selectorCalls(t, path)
	if !calls["rfqsService.SetIssuanceStatusSource"] {
		t.Errorf("composition/services.go never calls rfqsService.SetIssuanceStatusSource. " +
			"rfqs would keep NoExternalIssuanceSource, leaving an issued chain " +
			"reopenable (design spec §1A.3, §2.1)")
	}
}
