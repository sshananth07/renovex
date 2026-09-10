package demoseed_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/access"
	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

func (f *fakeProjectCreator) GetProject(ctx context.Context, companyID, projectID string) (projects.Project, error) {
	p, ok := f.byID[projectID]
	if !ok {
		return projects.Project{}, projects.ErrProjectNotFound
	}
	return p, nil
}

type fakeQuotationCreator struct {
	byProject     map[string][]quotations.Quotation
	createCalls   int
	finalizeCalls int
}

func (f *fakeQuotationCreator) CreateQuotation(ctx context.Context, companyID, projectID, estimateID string) (quotations.Quotation, error) {
	f.createCalls++
	q := quotations.Quotation{ID: "quote-" + projectID, CompanyID: companyID, ProjectID: projectID, Status: quotations.QuotationStatusDraft, Revision: 0}
	f.byProject[projectID] = append(f.byProject[projectID], q)
	return q, nil
}
func (f *fakeQuotationCreator) ListQuotationsByProject(ctx context.Context, companyID, projectID string) ([]quotations.Quotation, error) {
	return f.byProject[projectID], nil
}
func (f *fakeQuotationCreator) FinalizeQuotation(ctx context.Context, companyID, quotationID string, expectedRevision int64) (quotations.Quotation, error) {
	f.finalizeCalls++
	for projectID, list := range f.byProject {
		for i, q := range list {
			if q.ID == quotationID {
				list[i].Status = quotations.QuotationStatusFinalized
				f.byProject[projectID] = list
				return list[i], nil
			}
		}
	}
	return quotations.Quotation{}, quotations.ErrQuotationNotFound
}

// fakeQuotationSharer models the REAL RawToken semantics precisely: the
// mint call (ShareQuotation or RotateGrant) returns a raw token; the
// STORED record (what GetShareStatus returns) never carries one. This is
// what makes the crash-recovery test below meaningful.
//
// It also replicates the real access.Service's documented side effect:
// ShareQuotation advances the underlying Project to quotation_sent, and
// SubmitClientDecision(accepted) advances it to quotation_approved — both
// via *projects.Service internally in production. projects (optional) lets
// this fake do the same against fakeProjectCreator, since SeedProject3's
// crash-recovery logic is authoritative-completion-signal-driven and would
// otherwise never see the Project reach quotation_approved.
type fakeQuotationSharer struct {
	shareCalls, rotateCalls, decisionCalls int
	shared                                 map[string]access.GrantView // stored (no RawToken)
	rotateCount                            int
	projects                               *fakeProjectCreator
}

// quotationIDToProjectID inverts fakeQuotationCreator's own ID scheme
// ("quote-" + projectID) — good enough for this test double; production
// code never does this, since the real access.Service already knows a
// grant's owning Project internally.
func quotationIDToProjectID(quotationID string) string {
	return strings.TrimPrefix(quotationID, "quote-")
}

func (f *fakeQuotationSharer) ShareQuotation(ctx context.Context, companyID, quotationID, actorUserID string) (access.GrantView, bool, error) {
	f.shareCalls++
	if existing, ok := f.shared[quotationID]; ok {
		return existing, false, nil
	}
	view := access.GrantView{GrantID: "grant-" + quotationID, QuotationID: quotationID, EffectiveStatus: "active", RawToken: "raw-token-initial-" + quotationID, Revision: 1}
	stored := view
	stored.RawToken = ""
	f.shared[quotationID] = stored
	if f.projects != nil {
		if _, err := f.projects.UpdateProjectStatus(ctx, companyID, quotationIDToProjectID(quotationID), projects.ProjectStatusQuotationSent); err != nil {
			return access.GrantView{}, false, err
		}
	}
	return view, true, nil
}
func (f *fakeQuotationSharer) GetShareStatus(ctx context.Context, companyID, quotationID string) (access.GrantView, error) {
	view, ok := f.shared[quotationID]
	if !ok {
		return access.GrantView{}, access.ErrGrantNotFound
	}
	return view, nil
}
func (f *fakeQuotationSharer) RotateGrant(ctx context.Context, companyID, grantID, actorUserID string, expectedRevision int64) (access.GrantView, error) {
	f.rotateCalls++
	f.rotateCount++
	for quotationID, view := range f.shared {
		if view.GrantID == grantID {
			fresh := view
			fresh.RawToken = "raw-token-rotated-" + quotationID
			fresh.Revision = expectedRevision + 1
			stored := fresh
			stored.RawToken = ""
			f.shared[quotationID] = stored
			return fresh, nil
		}
	}
	return access.GrantView{}, access.ErrGrantNotFound
}
func (f *fakeQuotationSharer) ViewQuotationByToken(ctx context.Context, rawToken string) (access.ClientQuotationView, error) {
	return access.ClientQuotationView{}, nil
}
func (f *fakeQuotationSharer) SubmitClientDecision(ctx context.Context, in access.ClientDecisionInput) (access.ClientDecisionResult, error) {
	f.decisionCalls++
	if in.Status == "accepted" && f.projects != nil {
		// Derive the owning quotation/project from the token this fake
		// itself minted (initial or rotated) — mirrors production, where
		// the token alone resolves the whole chain back to a Project.
		for quotationID := range f.shared {
			if in.Token == "raw-token-initial-"+quotationID || strings.HasPrefix(in.Token, "raw-token-rotated-"+quotationID) {
				projectID := quotationIDToProjectID(quotationID)
				project := f.projects.byID[projectID]
				if _, err := f.projects.UpdateProjectStatus(ctx, project.CompanyID, projectID, projects.ProjectStatusQuotationApproved); err != nil {
					return access.ClientDecisionResult{}, err
				}
				break
			}
		}
	}
	return access.ClientDecisionResult{Status: in.Status}, nil
}

func TestSeedProject3_SharesAndAcceptsQuotation(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}
	costsFake := &fakeCostItemCreator{byProject: map[string][]costs.CostItem{}}
	workersFake := &fakeWorkerCreator{byName: map[string]labour.Worker{}}
	labourFake := &fakeLabourEntryCreator{byProject: map[string][]labour.LabourEntry{}}
	estimatesFake := &fakeEstimateCreator{byProject: map[string]estimates.Estimate{}}
	quotationsFake := &fakeQuotationCreator{byProject: map[string][]quotations.Quotation{}}
	accessFake := &fakeQuotationSharer{shared: map[string]access.GrantView{}, projects: projectsFake}
	catalog := map[string]materials.Material{
		"Porcelain Floor Tile": {ID: "mat-1", Name: "Porcelain Floor Tile"},
		"Cement":               {ID: "mat-2", Name: "Cement"},
	}

	projectID, err := demoseed.SeedProject3(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, quotationsFake, accessFake, catalog, "user-1", "company-1")
	if err != nil {
		t.Fatalf("SeedProject3: %v", err)
	}
	if quotationsFake.finalizeCalls != 1 {
		t.Fatalf("expected the Quotation to be finalized, got %d calls", quotationsFake.finalizeCalls)
	}
	if accessFake.shareCalls == 0 {
		t.Fatalf("expected ShareQuotation to be called")
	}
	if accessFake.decisionCalls != 1 {
		t.Fatalf("expected exactly 1 SubmitClientDecision call, got %d", accessFake.decisionCalls)
	}
	proj := projectsFake.byID[projectID]
	if proj.Status != projects.ProjectStatusQuotationApproved {
		t.Fatalf("expected Project status quotation_approved, got %q", proj.Status)
	}
}

func TestSeedProject3_CrashAfterShareBeforeDecision_RecoversViaRotate(t *testing.T) {
	// This is the EXACT crash scenario the review described: ShareQuotation
	// succeeded (a grant exists, Project advanced to quotation_sent), but
	// the process crashed before SubmitClientDecision ever ran. On rerun,
	// the OLD code's `if grant.RawToken != ""` check would never be true
	// again (GetShareStatus never returns a raw token), permanently
	// stranding the Project. This test proves RotateGrant recovers it.
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}
	costsFake := &fakeCostItemCreator{byProject: map[string][]costs.CostItem{}}
	workersFake := &fakeWorkerCreator{byName: map[string]labour.Worker{}}
	labourFake := &fakeLabourEntryCreator{byProject: map[string][]labour.LabourEntry{}}
	estimatesFake := &fakeEstimateCreator{byProject: map[string]estimates.Estimate{}}
	quotationsFake := &fakeQuotationCreator{byProject: map[string][]quotations.Quotation{}}
	accessFake := &fakeQuotationSharer{shared: map[string]access.GrantView{}, projects: projectsFake}
	catalog := map[string]materials.Material{"Cement": {ID: "mat-2", Name: "Cement"}, "Porcelain Floor Tile": {ID: "mat-1", Name: "Porcelain Floor Tile"}}

	firstProjectID, err := demoseed.SeedProject3(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, quotationsFake, accessFake, catalog, "user-1", "company-1")
	if err != nil {
		t.Fatalf("first (successful) SeedProject3: %v", err)
	}

	// Now simulate "the decision never happened" by rolling the Project's
	// status back to quotation_sent (as if SubmitClientDecision's call had
	// crashed before ever running) while leaving the grant as ShareQuotation
	// left it (Active, no raw token retrievable via GetShareStatus).
	rolledBack := projectsFake.byID[firstProjectID]
	rolledBack.Status = projects.ProjectStatusQuotationSent
	projectsFake.byID[firstProjectID] = rolledBack
	decisionCallsBeforeRerun := accessFake.decisionCalls

	// Rerun: SeedProject3 must detect the Project is NOT yet
	// quotation_approved, rotate the grant to obtain a fresh usable token,
	// and complete the decision.
	secondProjectID, err := demoseed.SeedProject3(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, quotationsFake, accessFake, catalog, "user-1", "company-1")
	if err != nil {
		t.Fatalf("recovery SeedProject3: %v", err)
	}
	if secondProjectID != firstProjectID {
		t.Fatalf("expected the same projectID on recovery rerun")
	}
	if accessFake.rotateCalls == 0 {
		t.Fatalf("expected RotateGrant to be called during recovery (the only way to obtain a fresh usable token)")
	}
	if accessFake.decisionCalls != decisionCallsBeforeRerun+1 {
		t.Fatalf("expected exactly 1 additional SubmitClientDecision call during recovery, went from %d to %d", decisionCallsBeforeRerun, accessFake.decisionCalls)
	}
	finalProject := projectsFake.byID[secondProjectID]
	if finalProject.Status != projects.ProjectStatusQuotationApproved {
		t.Fatalf("expected recovery to reach quotation_approved, got %q", finalProject.Status)
	}
}

func TestSeedProject3_Idempotent_AlreadyApprovedSkipsEverything(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}
	costsFake := &fakeCostItemCreator{byProject: map[string][]costs.CostItem{}}
	workersFake := &fakeWorkerCreator{byName: map[string]labour.Worker{}}
	labourFake := &fakeLabourEntryCreator{byProject: map[string][]labour.LabourEntry{}}
	estimatesFake := &fakeEstimateCreator{byProject: map[string]estimates.Estimate{}}
	quotationsFake := &fakeQuotationCreator{byProject: map[string][]quotations.Quotation{}}
	accessFake := &fakeQuotationSharer{shared: map[string]access.GrantView{}, projects: projectsFake}
	catalog := map[string]materials.Material{"Cement": {ID: "mat-2", Name: "Cement"}, "Porcelain Floor Tile": {ID: "mat-1", Name: "Porcelain Floor Tile"}}

	_, err := demoseed.SeedProject3(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, quotationsFake, accessFake, catalog, "user-1", "company-1")
	if err != nil {
		t.Fatalf("first SeedProject3: %v", err)
	}
	firstDecisionCalls, firstRotateCalls := accessFake.decisionCalls, accessFake.rotateCalls

	_, err = demoseed.SeedProject3(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake,
		costsFake, workersFake, labourFake, estimatesFake, quotationsFake, accessFake, catalog, "user-1", "company-1")
	if err != nil {
		t.Fatalf("second SeedProject3: %v", err)
	}
	if accessFake.decisionCalls != firstDecisionCalls || accessFake.rotateCalls != firstRotateCalls {
		t.Fatalf("expected no new SubmitClientDecision/RotateGrant calls once already approved")
	}
}
