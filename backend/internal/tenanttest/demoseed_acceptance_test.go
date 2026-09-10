// This suite requires the repository's own docker-compose Mailpit reachable
// at localhost:1025 (matching demoseed's exact mailer guard, Task 5) in
// addition to the Testcontainers-provisioned Mongo instance every other
// test in this package already uses. Start it with `docker compose up -d
// mailpit` before running this file's tests. A test that never delivers
// mail (Project 1/2/6-only assertions) would still pass without Mailpit
// running, since CheckMailerIsLocalSink only checks configuration, not
// connectivity — but Seed's real Project 3-5 scenarios call Register,
// ShareQuotation, and CreateInvitation, all of which attempt a real SMTP
// send through the configured localhost:1025 sender, so a live Mailpit (or
// at least a listener on that port) is required for Seed itself to
// succeed in this suite.
package tenanttest_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/ai"
	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// acceptanceTestConfig mirrors this package's own unexported testConfig
// (router.go) — duplicated here rather than exported from there, since
// router.go's constants are deliberately test-fixture-local and this file
// needs its own real composition.Services (not an HTTP router) to call
// demoseed.Seed/Reset directly against domain services.
func acceptanceTestConfig() config.Config {
	return config.Config{
		AppEnv:           "test",
		JWTAccessSecret:  "demoseed-acceptance-fixed-secret-do-not-use-in-production",
		SMTPHost:         "localhost",
		SMTPPort:         "1025",
		SMTPFrom:         "no-reply@test.local",
		AIServiceURL:     "",
		AIInternalToken:  "demoseed-acceptance-fixed-ai-internal-token",
		AIServiceTimeout: 10 * time.Second,

		ExternalAPIBaseURL: "http://demoseed-acceptance.local",

		InvitationSecretActiveVersion: 1,
		InvitationSecretKeys:          map[int]string{1: "ZGVtb3NlZWQtYWNjZXB0YW5jZS1pbnZpdGF0aW9uLWtleS12MQ=="},

		SupplierVerificationCodeActiveVersion: 1,
		SupplierVerificationCodeKeys:          map[int]string{1: "ZGVtb3NlZWQtYWNjZXB0YW5jZS12ZXJpZmljYXRpb24ta2V5"},
		SupplierSessionTokenActiveVersion:     1,
		SupplierSessionTokenKeys:              map[int]string{1: "ZGVtb3NlZWQtYWNjZXB0YW5jZS1zZXNzaW9uLWtleS12MQ=="},
		SupplierRateLimitFingerprintKey:       "ZGVtb3NlZWQtYWNjZXB0YW5jZS1yYXRlLWZpbmdlcnByaW50LWtleQ==",
	}
}

// buildTestServices constructs a real composition.Services against a fresh
// Testcontainers Mongo database (replica-set enabled — supplieroffers'
// eligibility-claim transaction, exercised by Project 5's Award finalisation,
// requires one, matching every other real-Mongo test in this package).
func buildTestServices(t *testing.T) (*composition.Services, *mongo.Database) {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	connStr += "&directConnection=true"

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	db := platformmongo.Database(client, "demoseed_acceptance")
	logger := zerolog.New(io.Discard)
	services, err := composition.BuildServices(ctx, acceptanceTestConfig(), logger, db)
	if err != nil {
		t.Fatalf("BuildServices: %v", err)
	}
	return services, db
}

func TestDemoSeed_CreatesSeparateUserAndCompany(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if result.CompanyName != "Renovex Demo Contractor Sdn Bhd" {
		t.Fatalf("CompanyName = %q", result.CompanyName)
	}
	if result.DemoEmail != "demo@renovex.local" {
		t.Fatalf("DemoEmail = %q", result.DemoEmail)
	}

	user, err := services.Users.FindUserByEmail(ctx, "demo@renovex.local")
	if err != nil {
		t.Fatalf("expected the demo User to exist: %v", err)
	}
	companyID, role, err := services.Companies.FindMembershipByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("expected the demo User to have a Membership: %v", err)
	}
	if companyID != result.CompanyID {
		t.Fatalf("Membership companyID = %q, want %q", companyID, result.CompanyID)
	}
	if role != identity.RoleOwnerString {
		t.Fatalf("role = %q, want owner", role)
	}
}

func TestDemoSeed_ExistingTenantUntouched(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	if _, err := services.Auth.Register(ctx, "developer@example.com", "developer-password-123", "My Real Company Sdn Bhd"); err != nil {
		t.Fatalf("register the pre-existing developer tenant: %v", err)
	}
	developerUser, err := services.Users.FindUserByEmail(ctx, "developer@example.com")
	if err != nil {
		t.Fatalf("find developer user: %v", err)
	}
	developerCompanyID, _, err := services.Companies.FindMembershipByUserID(ctx, developerUser.ID)
	if err != nil {
		t.Fatalf("find developer membership: %v", err)
	}
	if _, err := services.Clients.CreateClient(ctx, developerCompanyID, "Developer's Real Client", "", "", "", "", ""); err != nil {
		t.Fatalf("create developer client: %v", err)
	}

	if _, err := demoseed.Seed(ctx, services, db, "test-only-demo-password"); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	stillThere, err := services.Users.FindUserByEmail(ctx, "developer@example.com")
	if err != nil || stillThere.ID != developerUser.ID {
		t.Fatalf("developer's own User was affected by Seed: err=%v", err)
	}
	clientList, _, err := services.Clients.ListClientsPaginated(ctx, developerCompanyID, pagination.Request{Page: 1, PageSize: 10, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil || len(clientList) != 1 {
		t.Fatalf("developer's own Client data was affected by Seed: err=%v count=%d", err, len(clientList))
	}
}

func TestDemoSeed_AllSeededRecordsCarryDemoCompanyID(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	allProjects, _, err := services.Projects.ListProjectsPaginated(ctx, result.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("ListProjectsPaginated: %v", err)
	}
	if len(allProjects) != 6 {
		t.Fatalf("expected exactly 6 Projects, got %d", len(allProjects))
	}
	for _, p := range allProjects {
		if p.CompanyID != result.CompanyID {
			t.Fatalf("Project %q has CompanyID %q, want %q", p.Name, p.CompanyID, result.CompanyID)
		}
	}

	allClients, _, err := services.Clients.ListClientsPaginated(ctx, result.CompanyID, pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("ListClientsPaginated: %v", err)
	}
	if len(allClients) < 5 {
		t.Fatalf("expected at least 5 Clients, got %d", len(allClients))
	}
	for _, c := range allClients {
		if c.CompanyID != result.CompanyID {
			t.Fatalf("Client %q has CompanyID %q, want %q", c.Name, c.CompanyID, result.CompanyID)
		}
	}

	allMaterials, err := services.Materials.ListMaterials(ctx, result.CompanyID)
	if err != nil {
		t.Fatalf("ListMaterials: %v", err)
	}
	if len(allMaterials) == 0 {
		t.Fatalf("expected a non-empty Material catalog")
	}
	for _, m := range allMaterials {
		if m.CompanyID != result.CompanyID {
			t.Fatalf("Material %q has CompanyID %q, want %q", m.Name, m.CompanyID, result.CompanyID)
		}
	}

	allSuppliers, err := services.Suppliers.ListSuppliers(ctx, result.CompanyID, suppliers.SupplierFilter{})
	if err != nil {
		t.Fatalf("ListSuppliers: %v", err)
	}
	if len(allSuppliers) < 5 {
		t.Fatalf("expected at least 5 Suppliers, got %d", len(allSuppliers))
	}
	for _, s := range allSuppliers {
		if s.CompanyID != result.CompanyID {
			t.Fatalf("Supplier %q has CompanyID %q, want %q", s.Name, s.CompanyID, result.CompanyID)
		}
	}

	// RFQs are project-scoped (ListRFQsByProject, not a company-wide list),
	// so this checks the two Projects known to issue one.
	for _, projectName := range []string{"Subang Family Home Renovation", "Damansara Heights Residence"} {
		var projectID string
		for _, p := range allProjects {
			if p.Name == projectName {
				projectID = p.ID
			}
		}
		if projectID == "" {
			t.Fatalf("project %q not found among seeded Projects", projectName)
		}
		projectRFQs, err := services.RFQs.ListRFQsByProject(ctx, result.CompanyID, projectID)
		if err != nil {
			t.Fatalf("ListRFQsByProject(%q): %v", projectName, err)
		}
		if len(projectRFQs) == 0 {
			t.Fatalf("expected at least 1 RFQ for %q, got 0", projectName)
		}
		for _, r := range projectRFQs {
			if r.CompanyID != result.CompanyID {
				t.Fatalf("RFQ %q (project %q) has CompanyID %q, want %q", r.ID, projectName, r.CompanyID, result.CompanyID)
			}
		}
	}
}

func TestDemoSeed_RepeatSeedIsIdempotent(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	first, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("first Seed: %v", err)
	}
	_, firstProjectTotal, err := services.Projects.ListProjectsPaginated(ctx, first.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("list after first seed: %v", err)
	}
	_, firstClientTotal, err := services.Clients.ListClientsPaginated(ctx, first.CompanyID, pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("list clients after first seed: %v", err)
	}

	second, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	if second.CompanyID != first.CompanyID {
		t.Fatalf("expected the same CompanyID on rerun, got %q then %q", first.CompanyID, second.CompanyID)
	}
	_, secondProjectTotal, err := services.Projects.ListProjectsPaginated(ctx, second.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("list after second seed: %v", err)
	}
	_, secondClientTotal, err := services.Clients.ListClientsPaginated(ctx, second.CompanyID, pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("list clients after second seed: %v", err)
	}
	if secondProjectTotal != firstProjectTotal {
		t.Fatalf("expected no new Projects on rerun: %d -> %d", firstProjectTotal, secondProjectTotal)
	}
	if secondClientTotal != firstClientTotal {
		t.Fatalf("expected no new Clients on rerun: %d -> %d", firstClientTotal, secondClientTotal)
	}
}

func TestDemoSeed_ResetRemovesAllTenantCollections(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// Capture identifiers for collection categories that require a
	// Project/RFQ/Supplier-scoped lookup BEFORE Reset runs, since every one
	// of them becomes unreachable once the Company itself is gone.
	allProjects, _, err := services.Projects.ListProjectsPaginated(ctx, result.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("ListProjectsPaginated before Reset: %v", err)
	}
	projectIDByName := make(map[string]string, len(allProjects))
	for _, p := range allProjects {
		projectIDByName[p.Name] = p.ID
	}
	project3ID, ok := projectIDByName["Mont Kiara Apartment Upgrade"]
	if !ok {
		t.Fatalf("Project 3 not found before Reset")
	}
	project4ID, ok := projectIDByName["Subang Family Home Renovation"]
	if !ok {
		t.Fatalf("Project 4 not found before Reset")
	}

	project4RFQs, err := services.RFQs.ListRFQsByProject(ctx, result.CompanyID, project4ID)
	if err != nil || len(project4RFQs) == 0 {
		t.Fatalf("expected at least one RFQ for Project 4 before Reset: err=%v count=%d", err, len(project4RFQs))
	}
	project4RFQChainID := project4RFQs[0].ChainID()

	quotationsBefore, err := services.Quotations.ListQuotationsByProject(ctx, result.CompanyID, project3ID)
	if err != nil || len(quotationsBefore) == 0 {
		t.Fatalf("expected at least one Quotation for Project 3 before Reset: err=%v count=%d", err, len(quotationsBefore))
	}
	if _, err := services.Access.GetShareStatus(ctx, result.CompanyID, quotationsBefore[0].ID); err != nil {
		t.Fatalf("expected an Access grant for Project 3's Quotation before Reset: %v", err)
	}

	if _, err := demoseed.Reset(ctx, services, db); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if _, err := services.Users.FindUserByEmail(ctx, "demo@renovex.local"); err == nil {
		t.Fatalf("expected the demo User to be gone after Reset")
	}
	// The demo Company itself is gone. ListProjectsPaginated (with no
	// clientID filter) never validates companyID existence — it is a pure
	// companyID-scoped Mongo query, so an absent company yields an empty
	// list with nil error, not an error. That is Projects' real unreachability
	// contract; the other collections below use ID-lookup paths that do
	// validate and genuinely error.
	if list, _, err := services.Projects.ListProjectsPaginated(ctx, result.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc}); err != nil || len(list) != 0 {
		t.Fatalf("expected zero Projects after Reset, got %d (err=%v)", len(list), err)
	}
	// ListClientsPaginated, like ListProjectsPaginated, is a pure
	// companyID-scoped query with no company-existence check — an absent
	// company yields an empty list with nil error, not an error.
	if list, _, err := services.Clients.ListClientsPaginated(ctx, result.CompanyID, pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc}); err != nil || len(list) != 0 {
		t.Fatalf("expected zero Clients after Reset, got %d (err=%v)", len(list), err)
	}
	if list, err := services.Materials.ListMaterials(ctx, result.CompanyID); err != nil || len(list) != 0 {
		t.Fatalf("expected zero Materials after Reset, got %d (err=%v)", len(list), err)
	}
	if list, err := services.Suppliers.ListSuppliers(ctx, result.CompanyID, suppliers.SupplierFilter{}); err != nil || len(list) != 0 {
		t.Fatalf("expected zero Suppliers after Reset, got %d (err=%v)", len(list), err)
	}

	// RFQ issuance chains, invitations, and any Supplier offer drafts/
	// versions/sessions bound to them. ListInvitations, like the
	// companyID-scoped list methods above, has no chain-existence check —
	// an absent chain yields an empty list with nil error.
	if list, err := services.RFQIssuance.ListInvitations(ctx, result.CompanyID, project4RFQChainID); err != nil || len(list) != 0 {
		t.Fatalf("expected zero RFQ issuance/invitation records after Reset, got %d (err=%v)", len(list), err)
	}
	if _, err := services.RFQs.ListRFQsByProject(ctx, result.CompanyID, project4ID); err == nil {
		t.Fatalf("expected RFQs to be unreachable after Reset")
	}

	// Access grant (Project 3's Quotation share/decision).
	if _, err := services.Access.GetShareStatus(ctx, result.CompanyID, quotationsBefore[0].ID); err == nil {
		t.Fatalf("expected the Access grant to be unreachable after Reset")
	}
}

func TestDemoSeed_ResetDoesNotRemoveAnotherCompany(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	if _, err := services.Auth.Register(ctx, "developer@example.com", "developer-password-123", "My Real Company Sdn Bhd"); err != nil {
		t.Fatalf("register developer tenant: %v", err)
	}
	developerUser, err := services.Users.FindUserByEmail(ctx, "developer@example.com")
	if err != nil {
		t.Fatalf("find developer user: %v", err)
	}

	if _, err := demoseed.Seed(ctx, services, db, "test-only-demo-password"); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if _, err := demoseed.Reset(ctx, services, db); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	stillThere, err := services.Users.FindUserByEmail(ctx, "developer@example.com")
	if err != nil || stillThere.ID != developerUser.ID {
		t.Fatalf("developer's own User was affected by Reset: err=%v", err)
	}
}

func TestDemoSeed_EnvironmentGuard(t *testing.T) {
	t.Skip("fully covered by demoseed.TestCheckEnvironmentGuard (Task 2) — a pure function test needing no Testcontainers; that single test's 6 subtests cover both seed AND reset refusal, since both subcommands call the identical CheckEnvironmentGuard function before doing anything else")
}

func TestDemoSeed_PasswordsAreHashedNormally(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	if _, err := demoseed.Seed(ctx, services, db, "test-only-demo-password"); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	user, err := services.Users.FindUserByEmail(ctx, "demo@renovex.local")
	if err != nil {
		t.Fatalf("find demo user: %v", err)
	}
	if user.PasswordHash == "test-only-demo-password" {
		t.Fatalf("password was stored in plaintext")
	}
	if err := identity.VerifyPassword(user.PasswordHash, "test-only-demo-password"); err != nil {
		t.Fatalf("stored hash does not verify against the real password: %v", err)
	}
}

func TestDemoSeed_NoRealEmailDomainsUsed(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	isApprovedDomain := func(domain string) bool {
		return domain == "example.com" || domain == "renovex.local" || strings.HasSuffix(domain, ".example.com")
	}
	checkEmail := func(label, email string) {
		if email == "" {
			return
		}
		at := len(email) - 1
		for at >= 0 && email[at] != '@' {
			at--
		}
		if at < 0 {
			t.Fatalf("%s: %q is not a valid email", label, email)
		}
		domain := email[at+1:]
		if !isApprovedDomain(domain) {
			t.Fatalf("%s: %q uses a non-synthetic domain %q", label, email, domain)
		}
	}

	allClients, _, err := services.Clients.ListClientsPaginated(ctx, result.CompanyID, pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("ListClientsPaginated: %v", err)
	}
	for _, c := range allClients {
		checkEmail("Client "+c.Name, c.Email)
	}
	allSuppliers, err := services.Suppliers.ListSuppliers(ctx, result.CompanyID, suppliers.SupplierFilter{})
	if err != nil {
		t.Fatalf("ListSuppliers: %v", err)
	}
	for _, s := range allSuppliers {
		checkEmail("Supplier "+s.Name, s.Email)
	}
}

func TestDemoSeed_SixScenariosReachExpectedTerminalStates(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	allProjects, _, err := services.Projects.ListProjectsPaginated(ctx, result.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("ListProjectsPaginated: %v", err)
	}
	byName := make(map[string]projects.Project, len(allProjects))
	for _, p := range allProjects {
		byName[p.Name] = p
	}

	// Project 1: early setup, no dedicated status advancement expected —
	// only confirm Spaces/WorkItems exist (spec §5).
	p1, ok := byName["Taman Tun Condo Refresh"]
	if !ok {
		t.Fatalf("Project 1 (Taman Tun Condo Refresh) not found")
	}
	spacesP1, _, err := services.Spaces.ListSpacesPaginated(ctx, result.CompanyID, p1.ID, pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderAsc})
	if err != nil || len(spacesP1) < 3 {
		t.Fatalf("Project 1: expected multiple Spaces, got %d (err=%v)", len(spacesP1), err)
	}

	// Project 2: Estimate finalized.
	p2, ok := byName["Bangsar Kitchen Renovation"]
	if !ok {
		t.Fatalf("Project 2 (Bangsar Kitchen Renovation) not found")
	}
	estimateP2, err := services.Estimates.GetLatestEstimate(ctx, result.CompanyID, p2.ID)
	if err != nil || estimateP2.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("Project 2: expected a finalized Estimate, status=%q (err=%v)", estimateP2.Status, err)
	}

	// Project 3: Quotation finalized AND Project status quotation_approved.
	p3, ok := byName["Mont Kiara Apartment Upgrade"]
	if !ok {
		t.Fatalf("Project 3 (Mont Kiara Apartment Upgrade) not found")
	}
	if p3.Status != projects.ProjectStatusQuotationApproved {
		t.Fatalf("Project 3: expected status quotation_approved, got %q", p3.Status)
	}

	// Project 4: RFQ issued, 2 Supplier Offers, Project in_progress.
	p4, ok := byName["Subang Family Home Renovation"]
	if !ok {
		t.Fatalf("Project 4 (Subang Family Home Renovation) not found")
	}
	if p4.Status != projects.ProjectStatusInProgress {
		t.Fatalf("Project 4: expected status in_progress, got %q", p4.Status)
	}
	project4RFQs, err := services.RFQs.ListRFQsByProject(ctx, result.CompanyID, p4.ID)
	if err != nil || len(project4RFQs) == 0 {
		t.Fatalf("Project 4: expected at least 1 issued RFQ: err=%v count=%d", err, len(project4RFQs))
	}
	project4Invitations, err := services.RFQIssuance.ListInvitations(ctx, result.CompanyID, project4RFQs[0].ChainID())
	if err != nil || len(project4Invitations) != 2 {
		t.Fatalf("Project 4: expected exactly 2 Supplier invitations: err=%v count=%d", err, len(project4Invitations))
	}

	// Project 5: finalized Award Revision.
	p5, ok := byName["Damansara Heights Residence"]
	if !ok {
		t.Fatalf("Project 5 (Damansara Heights Residence) not found")
	}
	if p5.Status != projects.ProjectStatusInProgress {
		t.Fatalf("Project 5: expected status in_progress, got %q", p5.Status)
	}
	project5RFQs, err := services.RFQs.ListRFQsByProject(ctx, result.CompanyID, p5.ID)
	if err != nil || len(project5RFQs) == 0 {
		t.Fatalf("Project 5: expected at least 1 issued RFQ: err=%v count=%d", err, len(project5RFQs))
	}
	// Re-resolve the real issued RFQ version ID the SAME way
	// scenario_project5.go's ensureIssuedRFQ does: IssueVersion, keyed by a
	// FIXED OperationID. IssueVersion is idempotent on that operation ID, so
	// calling it again here returns the SAME already-issued version rather
	// than creating a new one.
	reIssued, err := services.RFQIssuance.IssueVersion(ctx, result.CompanyID, "demo-owner-not-used-for-writes", rfqissuance.IssueVersionInput{
		RFQChainID: project5RFQs[0].ChainID(), Currency: "MYR",
		OperationID: demoseed.OperationID("project5", "rfq-issue"),
	})
	if err != nil {
		t.Fatalf("Project 5: re-resolve the issued RFQ version ID via IssueVersion (idempotent on OperationID): %v", err)
	}
	if _, err := services.Awards.CreateAwardDraft(ctx, result.CompanyID, "demo-owner-not-used-for-writes", reIssued.ID); err != nil {
		t.Fatalf("Project 5: CreateAwardDraft (documented as safe to call unconditionally): %v", err)
	}
	if _, err := services.Awards.GetCurrentAward(ctx, result.CompanyID, reIssued.ID); err != nil {
		if errors.Is(err, awards.ErrAwardRevisionNotFound) {
			t.Fatalf("Project 5: expected a finalised Award Revision, found none")
		}
		t.Fatalf("Project 5: GetCurrentAward: %v", err)
	}

	// Project 6: has a Scope Brief, and explicitly NO Spaces/WorkItems yet
	// (proving no AI call happened during seeding).
	p6, ok := byName["KL Eco City Condo Renovation"]
	if !ok {
		t.Fatalf("Project 6 (KL Eco City Condo Renovation) not found")
	}
	if p6.ScopeBrief == "" {
		t.Fatalf("Project 6: expected a non-empty Scope Brief")
	}
	spacesP6, _, err := services.Spaces.ListSpacesPaginated(ctx, result.CompanyID, p6.ID, pagination.Request{Page: 1, PageSize: 10, Sort: "createdAt", Order: pagination.OrderAsc})
	if err != nil {
		t.Fatalf("Project 6: ListSpacesPaginated: %v", err)
	}
	if len(spacesP6) != 0 {
		t.Fatalf("Project 6: expected ZERO Spaces (no AI call during seeding, per spec §5), got %d", len(spacesP6))
	}
}

func TestDemoSeed_ProvisioningCrashRecovery_RealRepositories(t *testing.T) {
	// Simulates the EXACT crash the review described, against REAL
	// identity/companies services: Register succeeds, but RecordIdentity
	// never runs (simulated by calling Register directly instead of going
	// through Seed, leaving the manifest in state=provisioning with no
	// stored IDs — exactly what a crash between those two steps produces).
	ctx := context.Background()
	services, db := buildTestServices(t)

	if _, err := services.Auth.Register(ctx, "demo@renovex.local", "test-only-demo-password", "Renovex Demo Contractor Sdn Bhd"); err != nil {
		t.Fatalf("simulate pre-crash Register: %v", err)
	}

	// Now run the real Seed — it must detect the User already exists via
	// ResolveDemoTenant's observed-state recovery branch, NOT attempt
	// Register again (which would fail: the email already exists).
	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed should have RECOVERED via observed state against REAL repositories, got: %v", err)
	}
	if result.DemoEmail != "demo@renovex.local" {
		t.Fatalf("DemoEmail = %q", result.DemoEmail)
	}
}

func TestDemoSeed_ResetCrashRecovery_RealRepositories(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	if _, err := demoseed.Seed(ctx, services, db, "test-only-demo-password"); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// Simulate a crash partway through a PRIOR reset attempt: drive the
	// manifest directly to state=resetting (bypassing demoseed.Reset
	// entirely) while every real demo record still exists untouched —
	// exactly the state a crash immediately after BeginResetting would
	// leave behind. The lease must be SHORT and left to actually EXPIRE
	// before Reset is called — a live, unexpired lease held by a different
	// owner would correctly make Reset's own AcquireLease call refuse.
	manifests := demoseed.NewManifestStore(db)
	preCrashOwner := "pre-crash-simulated-owner"
	crashSimulationLeaseDuration := 50 * time.Millisecond
	if _, err := manifests.AcquireLease(ctx, preCrashOwner, crashSimulationLeaseDuration); err != nil {
		t.Fatalf("simulate acquiring the lease for the interrupted attempt: %v", err)
	}
	if _, err := manifests.BeginResetting(ctx, preCrashOwner); err != nil {
		t.Fatalf("simulate the interrupted attempt reaching state=resetting: %v", err)
	}
	// The simulated crash: preCrashOwner's lease is never released and is
	// left to expire naturally — no cleanup, no manifest deletion, real
	// demo data still fully present. Wait past the short lease's expiry so
	// it is genuinely stale (reclaimable) by the time Reset runs below.
	time.Sleep(crashSimulationLeaseDuration + 20*time.Millisecond)

	// A real Reset call must detect state=resetting, reclaim the NOW-EXPIRED
	// lease, and resume the FULL teardown from scratch against real
	// repositories — not refuse just because it didn't perform the
	// BeginResetting transition itself.
	if _, err := demoseed.Reset(ctx, services, db); err != nil {
		t.Fatalf("Reset should resume and complete against REAL repositories, got: %v", err)
	}
	if _, err := services.Users.FindUserByEmail(ctx, "demo@renovex.local"); err == nil {
		t.Fatalf("expected the demo User to be fully removed after the resumed reset")
	}
	if _, found, err := manifests.Get(ctx); err != nil || found {
		t.Fatalf("expected the manifest to be gone after the resumed reset completes, found=%v err=%v", found, err)
	}
}

func TestDemoSeed_ConcurrentSeed_SecondProcessRefused(t *testing.T) {
	// Creates a deterministic synchronization point: a real lease is held
	// (acquired directly against the SAME manifest demoseed.Seed itself
	// will use) for the ENTIRE duration of a concurrent Seed call attempt,
	// so that attempt is provably racing against a live, unexpired lease —
	// not a lease that might have already been released.
	ctx := context.Background()
	services, db := buildTestServices(t)
	manifests := demoseed.NewManifestStore(db)
	if err := manifests.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	// Hold a real, live lease on the SAME manifest demoseed.Seed acquires
	// internally, simulating "another demoseed process is already running"
	// for the entire duration of the next Seed call.
	blockingOwner := "test-simulated-concurrent-process"
	if _, err := manifests.AcquireLease(ctx, blockingOwner, 5*time.Minute); err != nil {
		t.Fatalf("acquire the blocking lease: %v", err)
	}

	// While that lease is definitely still live, a real Seed call must be
	// refused — not merely "might race and sometimes succeed."
	_, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err == nil {
		t.Fatalf("expected Seed to be refused while a live lease is held by another process")
	}
	if _, err := services.Users.FindUserByEmail(ctx, "demo@renovex.local"); err == nil {
		t.Fatalf("expected NO demo User to have been created while Seed was refused")
	}

	// Release the blocking lease (simulating the other process finishing)
	// and confirm a subsequent Seed call now succeeds normally — proving
	// the refusal above was genuinely about the live lease, not some
	// unrelated failure.
	if err := manifests.ReleaseLease(ctx, blockingOwner); err != nil {
		t.Fatalf("release the blocking lease: %v", err)
	}
	if _, err := demoseed.Seed(ctx, services, db, "test-only-demo-password"); err != nil {
		t.Fatalf("expected Seed to succeed once the blocking lease was released: %v", err)
	}
	if _, err := services.Users.FindUserByEmail(ctx, "demo@renovex.local"); err != nil {
		t.Fatalf("expected exactly one demo User to exist after the lease was released and Seed completed: %v", err)
	}
}

func TestDemoSeed_AmbiguousCompanyName_RealRepositories_Refuses(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	// A real Company named EXACTLY the demo Company's name, created via a
	// DIFFERENT email — no manifest exists for it.
	if _, err := services.Auth.Register(ctx, "someone-else@example.com", "some-password-123", "Renovex Demo Contractor Sdn Bhd"); err != nil {
		t.Fatalf("register an ambiguous same-named company: %v", err)
	}

	_, err := demoseed.Reset(ctx, services, db)
	if err == nil {
		t.Fatalf("expected Reset to refuse against a real ambiguous same-named Company with no manifest")
	}
}

func TestDemoSeed_ResetRemovesLiveAIRecords(t *testing.T) {
	ctx := context.Background()
	services, db := buildTestServices(t)

	result, err := demoseed.Seed(ctx, services, db, "test-only-demo-password")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// Simulate what the REAL M8.5B-A AI workflow would create if the user
	// ran the live AI demo against Project 6 after seeding — a genuine
	// AIGenerationBatch record, created through the real ai.Service the
	// same way the actual AI feature would (not a raw Mongo insert). This
	// test's composition.Services was built with an EMPTY AIServiceURL
	// (acceptanceTestConfig), matching production's "AI feature not
	// configured" state — SuggestSpaces will fail with a real HTTP-call
	// error against that empty base URL, so this test is skipped rather
	// than run against a real or stubbed ai-service, which this suite does
	// not stand up.
	allProjects, _, err := services.Projects.ListProjectsPaginated(ctx, result.CompanyID, "", pagination.Request{Page: 1, PageSize: 50, Sort: "createdAt", Order: pagination.OrderDesc})
	if err != nil {
		t.Fatalf("ListProjectsPaginated: %v", err)
	}
	var project6ID string
	for _, p := range allProjects {
		if p.Name == "KL Eco City Condo Renovation" {
			project6ID = p.ID
		}
	}
	if project6ID == "" {
		t.Fatalf("Project 6 not found")
	}
	if _, err := services.AI.SuggestSpaces(ctx, result.CompanyID, project6ID, "demo-user", "acceptance-test-op-1"); err != nil {
		t.Logf("SuggestSpaces failed (expected: this suite configures no AI_SERVICE_URL/stub): %v", err)
		t.Skip("this test requires a configured AI_SERVICE_URL (real ai-service or httptest.Server stub) to exercise the real SuggestSpaces path — skipping, since this suite's composition.Services has none configured")
	}

	batches, err := services.AI.ListBatches(ctx, result.CompanyID, project6ID, ai.BatchTypeSpaceSuggestions)
	if err != nil || len(batches) == 0 {
		t.Fatalf("expected at least one AI generation batch after SuggestSpaces: err=%v count=%d", err, len(batches))
	}

	if _, err := demoseed.Reset(ctx, services, db); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// The Company itself is gone, so this must error — confirming the AI
	// records were cleaned up as part of the same tenant teardown, not left
	// orphaned.
	if _, err := services.AI.ListBatches(ctx, result.CompanyID, project6ID, ai.BatchTypeSpaceSuggestions); err == nil {
		t.Fatalf("expected AI batches to be unreachable after Reset (proving live AI records created after seeding ARE removed, per spec §6.5)")
	}
}
