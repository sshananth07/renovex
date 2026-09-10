package demoseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

type fakeAwardDriver struct {
	draftCalls, selectCalls, finaliseCalls int
	chain                                  string
	currentRevision                        awards.AwardRevision
	hasRevision                            bool
}

func (f *fakeAwardDriver) CreateAwardDraft(ctx context.Context, companyID, actorUserID, issuedRFQVersionID string) (awards.AwardDraft, error) {
	f.draftCalls++
	f.chain = "award-chain-" + issuedRFQVersionID
	return awards.AwardDraft{ID: "award-draft-1", AwardChainID: f.chain, Revision: 0}, nil
}
func (f *fakeAwardDriver) SelectAwardLine(ctx context.Context, companyID, actorUserID string, input awards.SelectAwardLineInput) (awards.AwardDraft, error) {
	f.selectCalls++
	return awards.AwardDraft{ID: "award-draft-1", AwardChainID: f.chain, Revision: input.ExpectedRevision + 1}, nil
}
func (f *fakeAwardDriver) FinaliseAward(ctx context.Context, companyID, actorUserID string, input awards.FinaliseAwardInput) (awards.AwardRevision, error) {
	f.finaliseCalls++
	f.currentRevision = awards.AwardRevision{ID: "award-revision-1", AwardChainID: f.chain}
	f.hasRevision = true
	return f.currentRevision, nil
}
func (f *fakeAwardDriver) GetCurrentAward(ctx context.Context, companyID, issuedRFQVersionID string) (awards.AwardRevision, error) {
	if !f.hasRevision {
		return awards.AwardRevision{}, awards.ErrAwardRevisionNotFound
	}
	return f.currentRevision, nil
}
func (f *fakeAwardDriver) GetComparison(ctx context.Context, companyID, issuedRFQVersionID string, observedAt time.Time) (awards.Comparison, error) {
	return awards.Comparison{
		IssuedRFQVersionID: issuedRFQVersionID,
		Offers: []awards.ComparisonOffer{
			{
				OfferVersionID: "version-supplier1", SupplierID: "sup-1",
				Lines: []awards.ComparisonLine{
					{IssuedRFQLineID: "line-cement", OfferLineID: "offerline-cement", ResponseStatus: awards.OfferLineQuoted},
					{IssuedRFQLineID: "line-sand", OfferLineID: "offerline-sand", ResponseStatus: awards.OfferLineQuoted},
				},
			},
			{
				OfferVersionID: "version-supplier2", SupplierID: "sup-2",
				Lines: []awards.ComparisonLine{
					{IssuedRFQLineID: "line-tile", OfferLineID: "offerline-tile", ResponseStatus: awards.OfferLineQuoted},
				},
			},
		},
	}, nil
}

func TestSeedProject5_FinalisesAward(t *testing.T) {
	ctx := context.Background()
	invitationKeyring, codeKeyring := testKeyrings(t)
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
		Awards:       &fakeAwardDriver{},
		MaterialCatalog: map[string]materials.Material{
			"Cement": {ID: "mat-1", Name: "Cement"}, "Sand": {ID: "mat-3", Name: "Sand"},
			"Porcelain Floor Tile": {ID: "mat-4", Name: "Porcelain Floor Tile"},
		},
		SupplierDirectory: map[string]suppliers.Supplier{
			"DemoBuild Materials Sdn Bhd": {ID: "sup-1", Name: "DemoBuild Materials Sdn Bhd", ContactPerson: "Ahmad Faizal", Email: "sales@demobuildmaterials.example.com"},
			"Metro Tile Supply Sdn Bhd":   {ID: "sup-2", Name: "Metro Tile Supply Sdn Bhd", ContactPerson: "Tan Wei Ming", Email: "orders@metrotilesupply.example.com"},
		},
		SupplierAccess:    &fakeSupplierAccessDriver{},
		SupplierOffers:    &fakeSupplierOfferDriver{t: t},
		InvitationKeyring: invitationKeyring, CodeKeyring: codeKeyring,
		ActorUserID: "user-real-demo-owner-id", CompanyID: "company-1",
	}

	projectID, err := demoseed.SeedProject5(ctx, deps)
	if err != nil {
		t.Fatalf("SeedProject5: %v", err)
	}
	awardFake := deps.Awards.(*fakeAwardDriver)
	if awardFake.draftCalls != 1 {
		t.Fatalf("expected exactly 1 CreateAwardDraft call, got %d", awardFake.draftCalls)
	}
	if awardFake.selectCalls == 0 {
		t.Fatalf("expected at least 1 SelectAwardLine call")
	}
	if awardFake.finaliseCalls != 1 {
		t.Fatalf("expected exactly 1 FinaliseAward call, got %d", awardFake.finaliseCalls)
	}
	_ = projectID
}

func TestSeedProject5_Idempotent_SecondRunSkipsFinalise(t *testing.T) {
	ctx := context.Background()
	invitationKeyring, codeKeyring := testKeyrings(t)
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
		Awards:       &fakeAwardDriver{},
		MaterialCatalog: map[string]materials.Material{
			"Cement": {ID: "mat-1", Name: "Cement"}, "Sand": {ID: "mat-3", Name: "Sand"},
			"Porcelain Floor Tile": {ID: "mat-4", Name: "Porcelain Floor Tile"},
		},
		SupplierDirectory: map[string]suppliers.Supplier{
			"DemoBuild Materials Sdn Bhd": {ID: "sup-1", Name: "DemoBuild Materials Sdn Bhd", ContactPerson: "Ahmad Faizal", Email: "sales@demobuildmaterials.example.com"},
			"Metro Tile Supply Sdn Bhd":   {ID: "sup-2", Name: "Metro Tile Supply Sdn Bhd", ContactPerson: "Tan Wei Ming", Email: "orders@metrotilesupply.example.com"},
		},
		SupplierAccess:    &fakeSupplierAccessDriver{},
		SupplierOffers:    &fakeSupplierOfferDriver{t: t},
		InvitationKeyring: invitationKeyring, CodeKeyring: codeKeyring,
		ActorUserID: "user-real-demo-owner-id", CompanyID: "company-1",
	}

	_, err := demoseed.SeedProject5(ctx, deps)
	if err != nil {
		t.Fatalf("first SeedProject5: %v", err)
	}
	awardFake := deps.Awards.(*fakeAwardDriver)
	firstFinaliseCalls := awardFake.finaliseCalls

	_, err = demoseed.SeedProject5(ctx, deps)
	if err != nil {
		t.Fatalf("second SeedProject5: %v", err)
	}
	if awardFake.finaliseCalls != firstFinaliseCalls {
		t.Fatalf("expected no new FinaliseAward call on rerun (already finalised): %d -> %d", firstFinaliseCalls, awardFake.finaliseCalls)
	}
}
