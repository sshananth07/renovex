package demoseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

type fakeRequirementCreator struct {
	byProject       map[string][]materialrequirements.MaterialRequirement
	createCalls     int
	reviewCalls     int
	lastActorUserID string
}

func (f *fakeRequirementCreator) CreateManualRequirement(ctx context.Context, companyID, actorUserID string, input materialrequirements.CreateManualInput) (materialrequirements.MaterialRequirement, error) {
	f.createCalls++
	f.lastActorUserID = actorUserID
	r := materialrequirements.MaterialRequirement{
		ID: input.ProjectID + ":" + input.MaterialID, CompanyID: companyID, ProjectID: input.ProjectID,
		MaterialID: input.MaterialID, Status: materialrequirements.RequirementStatusDraft,
	}
	f.byProject[input.ProjectID] = append(f.byProject[input.ProjectID], r)
	return r, nil
}
func (f *fakeRequirementCreator) ListRequirementsByProject(ctx context.Context, companyID, projectID string) ([]materialrequirements.MaterialRequirement, error) {
	return f.byProject[projectID], nil
}
func (f *fakeRequirementCreator) ReviewRequirement(ctx context.Context, companyID, actorUserID, requirementID string, expectedRevision int64) (materialrequirements.MaterialRequirement, error) {
	f.reviewCalls++
	f.lastActorUserID = actorUserID
	for projectID, list := range f.byProject {
		for i, r := range list {
			if r.ID == requirementID {
				list[i].Status = materialrequirements.RequirementStatusReviewed
				f.byProject[projectID] = list
				return list[i], nil
			}
		}
	}
	return materialrequirements.MaterialRequirement{}, materialrequirements.ErrMaterialRequirementNotFound
}

type fakeRFQCreator struct {
	byProject                                 map[string][]rfqs.RFQ
	createCalls, addLineCalls, markReadyCalls int
}

func (f *fakeRFQCreator) CreateRFQ(ctx context.Context, companyID, actorUserID, projectID string, input rfqs.CreateRFQInput) (rfqs.RFQ, error) {
	f.createCalls++
	r := rfqs.RFQ{ID: "rfq-" + projectID, CompanyID: companyID, ProjectID: projectID, Status: rfqs.RFQStatusDraft, Title: input.Title}
	f.byProject[projectID] = append(f.byProject[projectID], r)
	return r, nil
}
func (f *fakeRFQCreator) ListRFQsByProject(ctx context.Context, companyID, projectID string) ([]rfqs.RFQ, error) {
	return f.byProject[projectID], nil
}
func (f *fakeRFQCreator) AddLine(ctx context.Context, companyID, actorUserID, rfqID string, expectedRevision int64, requirementID string, expectedRequirementRevision int64) (rfqs.RFQ, error) {
	f.addLineCalls++
	for _, list := range f.byProject {
		for _, r := range list {
			if r.ID == rfqID {
				return r, nil
			}
		}
	}
	return rfqs.RFQ{}, rfqs.ErrRFQNotFound
}
func (f *fakeRFQCreator) MarkReady(ctx context.Context, companyID, actorUserID, rfqID string, expectedRevision int64) (rfqs.RFQ, error) {
	f.markReadyCalls++
	for projectID, list := range f.byProject {
		for i, r := range list {
			if r.ID == rfqID {
				list[i].Status = rfqs.RFQStatusReady
				f.byProject[projectID] = list
				return list[i], nil
			}
		}
	}
	return rfqs.RFQ{}, rfqs.ErrRFQNotFound
}

type fakeRFQIssuer struct {
	issueCalls, inviteCalls, sendCalls int
	invitations                        []rfqissuance.SupplierInvitation
}

func (f *fakeRFQIssuer) IssueVersion(ctx context.Context, companyID, actorUserID string, input rfqissuance.IssueVersionInput) (rfqissuance.IssuedRFQVersion, error) {
	f.issueCalls++
	return rfqissuance.IssuedRFQVersion{
		ID: "issued-" + input.RFQChainID, RFQChainID: input.RFQChainID, VersionNumber: 1,
		Lines: []rfqissuance.IssuedRFQLine{
			{ID: "line-cement", MaterialID: "mat-1"},
			{ID: "line-sand", MaterialID: "mat-3"},
			{ID: "line-tile", MaterialID: "mat-4"},
		},
	}, nil
}
func (f *fakeRFQIssuer) CreateInvitation(ctx context.Context, companyID, actorUserID string, input rfqissuance.CreateInvitationInput) (rfqissuance.SupplierInvitation, error) {
	f.inviteCalls++
	inv := rfqissuance.SupplierInvitation{
		ID: "invitation-" + input.SupplierID, CompanyID: companyID, RFQChainID: input.RFQChainID,
		SupplierID: input.SupplierID, RecipientEmailNormalized: input.RecipientEmail,
		AccessGeneration: 1, SecretKeyVersion: 1,
	}
	f.invitations = append(f.invitations, inv)
	return inv, nil
}
func (f *fakeRFQIssuer) ListInvitations(ctx context.Context, companyID, rfqChainID string) ([]rfqissuance.SupplierInvitation, error) {
	var result []rfqissuance.SupplierInvitation
	for _, inv := range f.invitations {
		if inv.RFQChainID == rfqChainID {
			result = append(result, inv)
		}
	}
	return result, nil
}
func (f *fakeRFQIssuer) SendInvitation(ctx context.Context, companyID, actorUserID string, input rfqissuance.SendInvitationInput) (rfqissuance.SendResult, error) {
	f.sendCalls++
	return rfqissuance.SendResult{}, nil
}

func TestSeedProject4_IssuesRFQAndInvitesTwoSuppliers(t *testing.T) {
	ctx := context.Background()
	deps := demoseed.ScenarioDependencies{
		Clients:      &fakeClientCreator{byName: map[string]clients.Client{}},
		Projects:     &fakeProjectCreator{byID: map[string]projects.Project{}},
		Properties:   &fakePropertyCreator{byProject: map[string][]properties.Property{}},
		Spaces:       &fakeSpaceCreator{byProject: map[string][]spaces.Space{}},
		WorkItems:    &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}},
		CostItems:    &fakeCostItemCreator{byProject: map[string][]costs.CostItem{}},
		Requirements: &fakeRequirementCreator{byProject: map[string][]materialrequirements.MaterialRequirement{}},
		RFQs:         &fakeRFQCreator{byProject: map[string][]rfqs.RFQ{}},
		RFQIssuance:  &fakeRFQIssuer{},
		MaterialCatalog: map[string]materials.Material{
			"Cement": {ID: "mat-1", Name: "Cement"},
			"Sand":   {ID: "mat-3", Name: "Sand"},
		},
		SupplierDirectory: map[string]suppliers.Supplier{
			"DemoBuild Materials Sdn Bhd": {ID: "sup-1", Name: "DemoBuild Materials Sdn Bhd", ContactPerson: "Ahmad Faizal", Email: "sales@demobuildmaterials.example.com"},
			"Metro Tile Supply Sdn Bhd":   {ID: "sup-2", Name: "Metro Tile Supply Sdn Bhd", ContactPerson: "Tan Wei Ming", Email: "orders@metrotilesupply.example.com"},
		},
		SupplierAccess: &fakeSupplierAccessDriver{},
		SupplierOffers: &fakeSupplierOfferDriver{t: t},
		ActorUserID:    "user-real-demo-owner-id", CompanyID: "company-1",
	}
	invitationKeyring, codeKeyring := testKeyrings(t)
	deps.InvitationKeyring, deps.CodeKeyring = invitationKeyring, codeKeyring

	projectID, err := demoseed.SeedProject4(ctx, deps)
	if err != nil {
		t.Fatalf("SeedProject4: %v", err)
	}
	rfqCreator := deps.RFQs.(*fakeRFQCreator)
	if rfqCreator.createCalls != 1 {
		t.Fatalf("expected exactly 1 RFQ created, got %d", rfqCreator.createCalls)
	}
	if rfqCreator.markReadyCalls != 1 {
		t.Fatalf("expected the RFQ to be marked ready, got %d calls", rfqCreator.markReadyCalls)
	}
	issuer := deps.RFQIssuance.(*fakeRFQIssuer)
	if issuer.issueCalls != 1 {
		t.Fatalf("expected exactly 1 IssueVersion call, got %d", issuer.issueCalls)
	}
	if issuer.inviteCalls != 2 {
		t.Fatalf("expected exactly 2 Supplier invitations, got %d", issuer.inviteCalls)
	}
	reqCreator := deps.Requirements.(*fakeRequirementCreator)
	if reqCreator.lastActorUserID != "user-real-demo-owner-id" {
		t.Fatalf("expected the REAL demo owner User ID as actor, got %q", reqCreator.lastActorUserID)
	}
	_ = projectID
	_ = time.Now
}
