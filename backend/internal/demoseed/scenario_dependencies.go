package demoseed

import (
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// ScenarioDependencies bundles every service a Project 4/5/6 scenario may
// need. ActorUserID is the REAL resolved demo User ID (from
// ResolveDemoTenant, Task 4/15) — every audit trail this tool produces
// attributes actions to the actual demo owner, exactly as a real
// contractor session would.
type ScenarioDependencies struct {
	Clients        ClientCreator
	Projects       ProjectCreator
	Properties     PropertyCreator
	Spaces         SpaceCreator
	WorkItems      WorkItemCreator
	CostItems      CostItemCreator
	Requirements   RequirementCreator
	RFQs           RFQCreator
	RFQIssuance    RFQIssuer
	SupplierAccess SupplierAccessDriver
	SupplierOffers SupplierOfferDriver
	Awards         AwardDriver

	InvitationKeyring *secrets.InvitationKeyring
	CodeKeyring       *secrets.SupplierVerificationCodeKeyring
	MaterialCatalog   map[string]materials.Material
	SupplierDirectory map[string]suppliers.Supplier

	// ActorUserID is the demo tenant's real owner User ID, resolved once by
	// ResolveDemoTenant and threaded through every scenario.
	ActorUserID string
	CompanyID   string
}
