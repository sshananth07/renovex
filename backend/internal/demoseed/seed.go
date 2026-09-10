package demoseed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

// SeedResult is returned to cmd/demoseed/main.go for the final confirmation
// message.
type SeedResult struct {
	CompanyID   string
	CompanyName string
	DemoEmail   string
}

// leaseDuration is generous for a one-shot CLI run — long enough that a
// slow seed (real Mongo, real network-bound service calls across ~25
// services) never has its lease reclaimed out from under it, short enough
// that a genuinely crashed process's lease becomes reclaimable within a
// reasonable wait.
const leaseDuration = 15 * time.Minute

// newLeaseOwnerToken generates a random per-invocation identity for the
// manifest lease — unique to this process run, never reused.
func newLeaseOwnerToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// SeedDependencies bundles every capability Seed needs, as the narrow
// interfaces this package already defines.
type SeedDependencies struct {
	Auth           Registrar
	Users          UserLookup
	Memberships    MembershipLookup
	Companies      CompanyLookup
	Clients        ClientCreator
	Projects       ProjectCreator
	Properties     PropertyCreator
	Spaces         SpaceCreator
	WorkItems      WorkItemCreator
	CostItems      CostItemCreator
	Workers        WorkerCreator
	LabourEntries  LabourEntryCreator
	Estimates      EstimateCreator
	Quotations     QuotationCreator
	Access         QuotationSharer
	Requirements   RequirementCreator
	RFQs           RFQCreator
	RFQIssuance    RFQIssuer
	SupplierAccess SupplierAccessDriver
	SupplierOffers SupplierOfferDriver
	Awards         AwardDriver
	Materials      MaterialCatalog
	Suppliers      SupplierDirectory
	ScopeBrief     ScopeBriefSetter

	InvitationKeyring *secrets.InvitationKeyring
	CodeKeyring       *secrets.SupplierVerificationCodeKeyring
}

// Seed is the real entrypoint cmd/demoseed/main.go calls.
func Seed(ctx context.Context, services *composition.Services, db *mongo.Database, demoPassword string) (SeedResult, error) {
	deps := SeedDependencies{
		Auth: services.Auth, Users: services.Users, Memberships: services.Companies,
		Companies: services.Companies, Clients: services.Clients, Projects: services.Projects,
		Properties: services.Properties, Spaces: services.Spaces, WorkItems: services.Work,
		CostItems: services.Costs, Workers: services.Labour, LabourEntries: services.Labour,
		Estimates: services.Estimates, Quotations: services.Quotations, Access: services.Access,
		Requirements: services.MaterialRequirements, RFQs: services.RFQs, RFQIssuance: services.RFQIssuance,
		SupplierAccess: services.SupplierAccess, SupplierOffers: services.SupplierOffers, Awards: services.Awards,
		Materials: services.Materials, Suppliers: services.Suppliers, ScopeBrief: services.Projects,
		InvitationKeyring: services.InvitationKeyring, CodeKeyring: services.SupplierVerificationCodeKeyring,
	}
	return SeedWithDependencies(ctx, db, deps, demoPassword)
}

// SeedWithDependencies runs the full seed sequence, holding the manifest
// lease for its entire duration: acquire lease -> resolve/provision the
// demo tenant -> release lease is deferred so it always runs -> seed the
// shared catalog/directory -> run all six Project scenarios in order,
// using the REAL resolved demo User ID as ActorUserID throughout (never a
// placeholder string).
func SeedWithDependencies(ctx context.Context, db *mongo.Database, deps SeedDependencies, demoPassword string) (SeedResult, error) {
	manifests := NewManifestStore(db)
	if err := manifests.EnsureIndexes(ctx); err != nil {
		return SeedResult{}, err
	}

	ownerToken, err := newLeaseOwnerToken()
	if err != nil {
		return SeedResult{}, err
	}
	if _, err := manifests.AcquireLease(ctx, ownerToken, leaseDuration); err != nil {
		return SeedResult{}, err
	}
	defer func() {
		_ = manifests.ReleaseLease(ctx, ownerToken)
	}()

	// A real seed run (real Mongo, real network-bound service calls across
	// ~25 services, six full Project scenarios) can plausibly exceed
	// leaseDuration. Without a heartbeat, another process could legitimately
	// reclaim this lease as "stale" while THIS process is still actively
	// writing domain data — the lease would then protect nothing.
	// StartLeaseHeartbeat renews on a fixed interval well inside
	// leaseDuration; if a renewal ever discovers the lease was reclaimed
	// anyway (this process hung long enough to look crashed), lostOwnership
	// fires and every subsequent step below checks it via
	// checkLeaseStillOwned before proceeding, aborting rather than
	// continuing to write with no exclusive claim on the tenant.
	stopHeartbeat, lostOwnership := manifests.StartLeaseHeartbeat(ctx, ownerToken, leaseDuration)
	defer stopHeartbeat()
	checkLeaseStillOwned := func() error {
		select {
		case <-lostOwnership:
			return fmt.Errorf("demoseed: lost the manifest lease mid-seed (another process reclaimed it) — aborting rather than continuing to write without exclusive ownership")
		default:
			return nil
		}
	}

	companyID, err := ResolveDemoTenant(ctx, ownerToken, deps.Auth, deps.Users, deps.Memberships, deps.Companies, manifests, demoPassword)
	if err != nil {
		return SeedResult{}, err
	}
	if err := checkLeaseStillOwned(); err != nil {
		return SeedResult{}, err
	}

	manifest, found, err := manifests.Get(ctx)
	if err != nil {
		return SeedResult{}, err
	}
	if !found {
		return SeedResult{}, fmt.Errorf("demoseed: manifest disappeared mid-seed")
	}
	actorUserID := manifest.DemoUserID

	companyName, err := deps.Companies.GetCompanyName(ctx, companyID)
	if err != nil {
		return SeedResult{}, err
	}

	materialCatalog, err := EnsureMaterialCatalog(ctx, deps.Materials, companyID)
	if err != nil {
		return SeedResult{}, err
	}
	supplierDirectory, err := EnsureSupplierDirectory(ctx, deps.Suppliers, companyID, actorUserID)
	if err != nil {
		return SeedResult{}, err
	}

	if _, err := SeedProject1(ctx, deps.Clients, deps.Projects, deps.Properties, deps.Spaces, deps.WorkItems, companyID); err != nil {
		return SeedResult{}, err
	}
	if err := checkLeaseStillOwned(); err != nil {
		return SeedResult{}, err
	}
	if _, err := SeedProject2(ctx, deps.Clients, deps.Projects, deps.Properties, deps.Spaces, deps.WorkItems,
		deps.CostItems, deps.Workers, deps.LabourEntries, deps.Estimates, materialCatalog, companyID); err != nil {
		return SeedResult{}, err
	}
	if err := checkLeaseStillOwned(); err != nil {
		return SeedResult{}, err
	}
	if _, err := SeedProject3(ctx, deps.Clients, deps.Projects, deps.Properties, deps.Spaces, deps.WorkItems,
		deps.CostItems, deps.Workers, deps.LabourEntries, deps.Estimates, deps.Quotations, deps.Access,
		materialCatalog, actorUserID, companyID); err != nil {
		return SeedResult{}, err
	}
	if err := checkLeaseStillOwned(); err != nil {
		return SeedResult{}, err
	}

	scenarioDeps := ScenarioDependencies{
		Clients: deps.Clients, Projects: deps.Projects, Properties: deps.Properties,
		Spaces: deps.Spaces, WorkItems: deps.WorkItems, CostItems: deps.CostItems,
		Requirements: deps.Requirements, RFQs: deps.RFQs, RFQIssuance: deps.RFQIssuance,
		SupplierAccess: deps.SupplierAccess, SupplierOffers: deps.SupplierOffers, Awards: deps.Awards,
		InvitationKeyring: deps.InvitationKeyring, CodeKeyring: deps.CodeKeyring,
		MaterialCatalog: materialCatalog, SupplierDirectory: supplierDirectory,
		ActorUserID: actorUserID, CompanyID: companyID,
	}
	if _, err := SeedProject4(ctx, scenarioDeps); err != nil {
		return SeedResult{}, err
	}
	if err := checkLeaseStillOwned(); err != nil {
		return SeedResult{}, err
	}
	if _, err := SeedProject5(ctx, scenarioDeps); err != nil {
		return SeedResult{}, err
	}
	if err := checkLeaseStillOwned(); err != nil {
		return SeedResult{}, err
	}
	if _, err := SeedProject6(ctx, deps.Clients, deps.Projects, deps.ScopeBrief, deps.Properties, companyID); err != nil {
		return SeedResult{}, err
	}

	return SeedResult{CompanyID: companyID, CompanyName: companyName, DemoEmail: demoEmail}, nil
}
