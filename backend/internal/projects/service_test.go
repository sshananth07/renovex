package projects

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

type fakeProjectRepo struct {
	byID   map[string]Project
	nextID int
}

func newFakeProjectRepo() *fakeProjectRepo {
	return &fakeProjectRepo{byID: map[string]Project{}}
}

func (f *fakeProjectRepo) Create(_ context.Context, p Project) (Project, error) {
	f.nextID++
	p.ID = string(rune('a' + f.nextID))
	f.byID[p.ID] = p
	return p, nil
}

func (f *fakeProjectRepo) FindByID(_ context.Context, companyID, id string) (Project, error) {
	p, ok := f.byID[id]
	if !ok || p.CompanyID != companyID {
		return Project{}, ErrProjectNotFound
	}
	return p, nil
}

func (f *fakeProjectRepo) ListPaginated(_ context.Context, companyID, clientID string, req pagination.Request) ([]Project, int, error) {
	var matched []Project
	for _, p := range f.byID {
		if p.CompanyID != companyID {
			continue
		}
		if clientID != "" && p.ClientID != clientID {
			continue
		}
		if req.Search != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(req.Search)) {
			continue
		}
		matched = append(matched, p)
	}
	sort.Slice(matched, func(i, j int) bool {
		var less bool
		switch req.Sort {
		case "name":
			less = matched[i].Name < matched[j].Name
		case "status":
			less = matched[i].Status < matched[j].Status
		default:
			less = matched[i].CreatedAt.Before(matched[j].CreatedAt)
		}
		if req.Order == pagination.OrderDesc {
			return !less
		}
		return less
	})
	total := len(matched)
	start := req.Offset()
	if start > total {
		start = total
	}
	end := start + req.PageSize
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

func (f *fakeProjectRepo) UpdateStatus(ctx context.Context, companyID, id string, status ProjectStatus) (Project, error) {
	p, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Project{}, err
	}
	p.Status = status
	f.byID[id] = p
	return p, nil
}

func (f *fakeProjectRepo) UpdateName(ctx context.Context, companyID, id, name string) (Project, error) {
	p, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Project{}, err
	}
	p.Name = name
	f.byID[id] = p
	return p, nil
}

func (f *fakeProjectRepo) UpdateScopeBrief(ctx context.Context, companyID, id, scopeBrief string) (Project, error) {
	p, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Project{}, err
	}
	p.ScopeBrief = scopeBrief
	f.byID[id] = p
	return p, nil
}

type fakeClientLookup struct {
	belongsTo map[string]string // clientID -> companyID
}

func newFakeClientLookup() *fakeClientLookup {
	return &fakeClientLookup{belongsTo: map[string]string{}}
}

func (f *fakeClientLookup) ClientBelongsToCompany(_ context.Context, companyID, clientID string) (bool, error) {
	owner, ok := f.belongsTo[clientID]
	return ok && owner == companyID, nil
}

func TestServiceCreateProjectValidatesClient(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)

	p, err := svc.CreateProject(context.Background(), "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status != ProjectStatusLead {
		t.Fatalf("expected default status Lead, got %s", p.Status)
	}
}

func TestServiceCreateProjectRejectsForeignClient(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_b" // belongs to a different company
	svc := NewService(newFakeProjectRepo(), clientLookup)

	_, err := svc.CreateProject(context.Background(), "company_a", "client_1", "Ahmad Residence")
	if err != ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound, got %v", err)
	}
}

func TestServiceCreateProjectRejectsEmptyName(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)

	_, err := svc.CreateProject(context.Background(), "company_a", "client_1", "")
	if err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func defaultProjectsPaginationRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", ProjectSortFields, ProjectDefaultSort, ProjectDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

func TestServiceListProjectsPaginatedByClientValidatesClientFirst(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	_, err := svc.CreateProject(ctx, "company_a", "client_1", "P1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = svc.ListProjectsPaginated(ctx, "company_b", "client_1", defaultProjectsPaginationRequest(t))
	if err != ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound for foreign client, got %v", err)
	}

	list, total, err := svc.ListProjectsPaginated(ctx, "company_a", "client_1", defaultProjectsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1", total, len(list))
	}
}

func TestServiceListProjectsPaginatedWithoutClientFilterReturnsAll(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	clientLookup.belongsTo["client_2"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, "company_a", "client_1", "P1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateProject(ctx, "company_a", "client_2", "P2"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, total, err := svc.ListProjectsPaginated(ctx, "company_a", "", defaultProjectsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("total=%d len(list)=%d, want 2/2", total, len(list))
	}
}

func TestServiceListProjectsPaginatedTenantIsolation(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	clientLookup.belongsTo["client_2"] = "company_b"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, "company_a", "client_1", "P1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateProject(ctx, "company_b", "client_2", "P2"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, total, err := svc.ListProjectsPaginated(ctx, "company_a", "", defaultProjectsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1 (must exclude company_b)", total, len(list))
	}
}

func TestServiceListProjectsPaginatedRejectsUnsupportedSort(t *testing.T) {
	if _, err := pagination.ParseRequest(0, 0, "", "clientId", "", ProjectSortFields, ProjectDefaultSort, ProjectDefaultOrder); err == nil {
		t.Fatal("expected an error for an unsupported sort field")
	}
}

func TestServiceUpdateProjectStatusAcceptsAnyValidStatus(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "P1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateProjectStatus(ctx, "company_a", created.ID, ProjectStatusClosed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != ProjectStatusClosed {
		t.Fatalf("expected closed, got %s", updated.Status)
	}

	// Any status is a valid destination from any other in M2 — no transition graph.
	backToLead, err := svc.UpdateProjectStatus(ctx, "company_a", created.ID, ProjectStatusLead)
	if err != nil {
		t.Fatalf("unexpected error reverting to lead: %v", err)
	}
	if backToLead.Status != ProjectStatusLead {
		t.Fatalf("expected lead, got %s", backToLead.Status)
	}
}

func TestServiceUpdateProjectStatusRejectsInvalidStatus(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "P1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateProjectStatus(ctx, "company_a", created.ID, ProjectStatus("not_a_real_status"))
	if err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestServiceProjectBelongsToCompany(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "P1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	belongs, err := svc.ProjectBelongsToCompany(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	belongs, err = svc.ProjectBelongsToCompany(ctx, "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false for wrong company")
	}
}

func TestServiceGetProjectClientID(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Bathroom Reno")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	clientID, err := svc.GetProjectClientID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clientID != "client_1" {
		t.Fatalf("expected clientID client_1, got %s", clientID)
	}
}

func TestServiceGetProjectClientIDNotFound(t *testing.T) {
	clientLookup := newFakeClientLookup()
	svc := NewService(newFakeProjectRepo(), clientLookup)

	_, err := svc.GetProjectClientID(context.Background(), "company_a", "nonexistent")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceGetProjectClientIDCrossTenant(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Bathroom Reno")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	_, err = svc.GetProjectClientID(ctx, "company_b", created.ID)
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for a cross-tenant lookup, got %v", err)
	}
}

// --- Milestone 6: monotonic status advancement ---

// UpdateStatusIfCurrent is the conditional compare-and-set the monotonic
// capabilities are built on. The fake mirrors the Mongo behaviour: it only
// matches when the stored status is one of eligibleFrom.
func (f *fakeProjectRepo) UpdateStatusIfCurrent(ctx context.Context, companyID, id string, eligibleFrom []ProjectStatus, target ProjectStatus) (Project, bool, error) {
	p, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Project{}, false, err
	}
	for _, from := range eligibleFrom {
		if p.Status == from {
			p.Status = target
			f.byID[id] = p
			return p, true, nil
		}
	}
	return p, false, nil
}

func projectWithStatus(t *testing.T, repo *fakeProjectRepo, status ProjectStatus) Project {
	t.Helper()
	p, err := repo.Create(context.Background(), Project{CompanyID: "company_a", ClientID: "client_1", Name: "P", Status: status})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return p
}

func TestAdvanceProjectToQuotationSentMonotonicMatrix(t *testing.T) {
	cases := []struct {
		from        ProjectStatus
		wantChanged bool
		wantStatus  ProjectStatus
	}{
		{ProjectStatusLead, true, ProjectStatusQuotationSent},
		{ProjectStatusSiteVisit, true, ProjectStatusQuotationSent},
		{ProjectStatusEstimating, true, ProjectStatusQuotationSent},
		{ProjectStatusQuotationSent, false, ProjectStatusQuotationSent},
		{ProjectStatusQuotationApproved, false, ProjectStatusQuotationApproved},
		{ProjectStatusInProgress, false, ProjectStatusInProgress},
		{ProjectStatusCompleted, false, ProjectStatusCompleted},
		{ProjectStatusClosed, false, ProjectStatusClosed},
	}

	for _, tc := range cases {
		t.Run(string(tc.from), func(t *testing.T) {
			repo := newFakeProjectRepo()
			svc := NewService(repo, newFakeClientLookup())
			p := projectWithStatus(t, repo, tc.from)

			changed, found, err := svc.AdvanceProjectToQuotationSent(context.Background(), "company_a", p.ID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !found {
				t.Fatal("expected the project to be found")
			}
			if changed != tc.wantChanged {
				t.Fatalf("from %q: expected changed=%v, got %v", tc.from, tc.wantChanged, changed)
			}
			if got := repo.byID[p.ID].Status; got != tc.wantStatus {
				t.Fatalf("from %q: expected stored status %q, got %q", tc.from, tc.wantStatus, got)
			}
		})
	}
}

func TestAdvanceProjectToQuotationApprovedMonotonicMatrix(t *testing.T) {
	cases := []struct {
		from        ProjectStatus
		wantChanged bool
		wantStatus  ProjectStatus
	}{
		{ProjectStatusLead, true, ProjectStatusQuotationApproved},
		{ProjectStatusSiteVisit, true, ProjectStatusQuotationApproved},
		{ProjectStatusEstimating, true, ProjectStatusQuotationApproved},
		{ProjectStatusQuotationSent, true, ProjectStatusQuotationApproved},
		{ProjectStatusQuotationApproved, false, ProjectStatusQuotationApproved},
		{ProjectStatusInProgress, false, ProjectStatusInProgress},
		{ProjectStatusCompleted, false, ProjectStatusCompleted},
		{ProjectStatusClosed, false, ProjectStatusClosed},
	}

	for _, tc := range cases {
		t.Run(string(tc.from), func(t *testing.T) {
			repo := newFakeProjectRepo()
			svc := NewService(repo, newFakeClientLookup())
			p := projectWithStatus(t, repo, tc.from)

			changed, found, err := svc.AdvanceProjectToQuotationApproved(context.Background(), "company_a", p.ID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !found {
				t.Fatal("expected the project to be found")
			}
			if changed != tc.wantChanged {
				t.Fatalf("from %q: expected changed=%v, got %v", tc.from, tc.wantChanged, changed)
			}
			if got := repo.byID[p.ID].Status; got != tc.wantStatus {
				t.Fatalf("from %q: expected stored status %q, got %q", tc.from, tc.wantStatus, got)
			}
		})
	}
}

func TestAdvanceProjectReportsNotFoundForUnknownProject(t *testing.T) {
	svc := NewService(newFakeProjectRepo(), newFakeClientLookup())

	_, found, err := svc.AdvanceProjectToQuotationSent(context.Background(), "company_a", "missing")
	if err != nil {
		t.Fatalf("expected a missing project to report found=false, not an error, got %v", err)
	}
	if found {
		t.Fatal("expected found=false")
	}
}

func TestAdvanceProjectIsTenantScoped(t *testing.T) {
	repo := newFakeProjectRepo()
	svc := NewService(repo, newFakeClientLookup())
	p := projectWithStatus(t, repo, ProjectStatusLead)

	_, found, err := svc.AdvanceProjectToQuotationSent(context.Background(), "company_b", p.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected company_b to not find company_a's project")
	}
	if repo.byID[p.ID].Status != ProjectStatusLead {
		t.Fatal("expected the project's status to be untouched by a cross-tenant call")
	}
}

func TestGetProjectName(t *testing.T) {
	repo := newFakeProjectRepo()
	svc := NewService(repo, newFakeClientLookup())
	p := projectWithStatus(t, repo, ProjectStatusLead)

	name, found, err := svc.GetProjectName(context.Background(), "company_a", p.ID)
	if err != nil || !found {
		t.Fatalf("expected to find the project, got found=%v err=%v", found, err)
	}
	if name != "P" {
		t.Fatalf("expected name P, got %q", name)
	}

	if _, found, _ := svc.GetProjectName(context.Background(), "company_b", p.ID); found {
		t.Fatal("expected cross-tenant lookup to report found=false")
	}
}

// --- Checkpoint 10: Project metadata PATCH ---

func TestServiceUpdateProjectNameChangesSuccessfully(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Old Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateProjectName(ctx, "company_a", created.ID, "New Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("Name = %q, want New Name", updated.Name)
	}
}

func TestServiceUpdateProjectNamePreservesClientRelationship(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Old Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateProjectName(ctx, "company_a", created.ID, "New Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ClientID != "client_1" {
		t.Fatalf("ClientID = %q, want unchanged client_1", updated.ClientID)
	}
}

func TestServiceUpdateProjectNamePreservesStatus(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Old Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.UpdateProjectStatus(ctx, "company_a", created.ID, ProjectStatusInProgress); err != nil {
		t.Fatalf("unexpected error setting status: %v", err)
	}

	updated, err := svc.UpdateProjectName(ctx, "company_a", created.ID, "New Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != ProjectStatusInProgress {
		t.Fatalf("Status = %q, want unchanged in_progress", updated.Status)
	}
}

func TestServiceUpdateProjectNameRejectsEmptyOrWhitespaceOnlyName(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Old Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateProjectName(ctx, "company_a", created.ID, ""); err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired for empty name, got %v", err)
	}
	if _, err := svc.UpdateProjectName(ctx, "company_a", created.ID, "   "); err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired for whitespace-only name, got %v", err)
	}
}

func TestServiceUpdateProjectNameCrossTenantReturnsNotFound(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Old Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateProjectName(ctx, "company_b", created.ID, "New Name"); err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant update, got %v", err)
	}
}

func TestServiceUpdateProjectScopeBriefTrimsWhitespace(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateProjectScopeBrief(ctx, "company_a", created.ID, "  Full renovation of a condo.  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ScopeBrief != "Full renovation of a condo." {
		t.Fatalf("ScopeBrief = %q, want trimmed", updated.ScopeBrief)
	}
}

func TestServiceUpdateProjectScopeBriefEmptyAfterTrimClears(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.UpdateProjectScopeBrief(ctx, "company_a", created.ID, "Some brief"); err != nil {
		t.Fatalf("unexpected error setting brief: %v", err)
	}

	cleared, err := svc.UpdateProjectScopeBrief(ctx, "company_a", created.ID, "   ")
	if err != nil {
		t.Fatalf("unexpected error clearing brief: %v", err)
	}
	if cleared.ScopeBrief != "" {
		t.Fatalf("ScopeBrief = %q, want cleared empty", cleared.ScopeBrief)
	}
}

func TestServiceUpdateProjectScopeBriefRejectsOversized(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	oversized := strings.Repeat("a", 5001)
	if _, err := svc.UpdateProjectScopeBrief(ctx, "company_a", created.ID, oversized); err != ErrScopeBriefTooLong {
		t.Fatalf("expected ErrScopeBriefTooLong, got %v", err)
	}
}

func TestServiceUpdateProjectScopeBriefAcceptsMaxLength(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	maxLen := strings.Repeat("a", 5000)
	updated, err := svc.UpdateProjectScopeBrief(ctx, "company_a", created.ID, maxLen)
	if err != nil {
		t.Fatalf("unexpected error at exactly max length: %v", err)
	}
	if updated.ScopeBrief != maxLen {
		t.Fatalf("expected full max-length brief to be stored")
	}
}

func TestServiceUpdateProjectScopeBriefPreservesOtherFields(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.UpdateProjectStatus(ctx, "company_a", created.ID, ProjectStatusInProgress); err != nil {
		t.Fatalf("unexpected error setting status: %v", err)
	}

	updated, err := svc.UpdateProjectScopeBrief(ctx, "company_a", created.ID, "New brief")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Ahmad Residence" {
		t.Fatalf("Name = %q, want unchanged", updated.Name)
	}
	if updated.Status != ProjectStatusInProgress {
		t.Fatalf("Status = %q, want unchanged", updated.Status)
	}
	if updated.ClientID != "client_1" {
		t.Fatalf("ClientID = %q, want unchanged", updated.ClientID)
	}
}

func TestServiceUpdateProjectScopeBriefCrossTenantReturnsNotFound(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, "company_a", "client_1", "Ahmad Residence")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateProjectScopeBrief(ctx, "company_b", created.ID, "Malicious"); err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant update, got %v", err)
	}
}
