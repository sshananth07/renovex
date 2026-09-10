package aiintegration

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

// --- fakes satisfying each existing domain service's real exported methods ---

type fakeProjectsService struct {
	byID map[string]projects.Project
}

func (f *fakeProjectsService) GetProject(_ context.Context, companyID, projectID string) (projects.Project, error) {
	p, ok := f.byID[projectID]
	if !ok || p.CompanyID != companyID {
		return projects.Project{}, projects.ErrProjectNotFound
	}
	return p, nil
}

type fakeSpacesService struct {
	byProject map[string][]spaces.Space
}

func (f *fakeSpacesService) ListSpacesPaginated(_ context.Context, companyID, projectID string, _ pagination.Request) ([]spaces.Space, int, error) {
	var result []spaces.Space
	for _, s := range f.byProject[projectID] {
		if s.CompanyID == companyID {
			result = append(result, s)
		}
	}
	return result, len(result), nil
}

type fakeWorkService struct {
	byProject map[string][]work.WorkItem
}

func (f *fakeWorkService) ListWorkItemsPaginatedByProject(_ context.Context, companyID, projectID string, _ pagination.Request) ([]work.WorkItem, int, error) {
	var result []work.WorkItem
	for _, w := range f.byProject[projectID] {
		if w.CompanyID == companyID {
			result = append(result, w)
		}
	}
	return result, len(result), nil
}

// foreignProjectWorkService reproduces the REAL work.Service's behavior for
// a foreign/mismatched (companyID, projectID) pair — work.ErrProjectNotFound,
// not a silently-empty slice (work/service.go ListWorkItemsPaginatedByProject
// checks ProjectBelongsToCompany first). The plain fakeWorkService above
// only ever returns empty slices, which cannot exercise this error path.
type foreignProjectWorkService struct{}

func (foreignProjectWorkService) ListWorkItemsPaginatedByProject(context.Context, string, string, pagination.Request) ([]work.WorkItem, int, error) {
	return nil, 0, work.ErrProjectNotFound
}

type fakeMaterialsService struct {
	byCompany map[string][]materials.Material
}

func (f *fakeMaterialsService) ListMaterials(_ context.Context, companyID string) ([]materials.Material, error) {
	return f.byCompany[companyID], nil
}

type fakeWorkResourcesService struct {
	byProject map[string][]workresources.WorkResourceRequirement
}

func (f *fakeWorkResourcesService) ListByProject(_ context.Context, companyID, projectID string) ([]workresources.WorkResourceRequirement, error) {
	var result []workresources.WorkResourceRequirement
	for _, r := range f.byProject[projectID] {
		if r.CompanyID == companyID {
			result = append(result, r)
		}
	}
	return result, nil
}

func newTestAdapter() *Adapter {
	return NewAdapter(
		&fakeProjectsService{byID: map[string]projects.Project{
			"project_1": {ID: "project_1", CompanyID: "company_a", ScopeBrief: "Full renovation."},
		}},
		&fakeSpacesService{byProject: map[string][]spaces.Space{
			"project_1": {{ID: "space_1", CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen", Type: "kitchen"}},
		}},
		&fakeWorkService{byProject: map[string][]work.WorkItem{
			"project_1": {{ID: "work_1", CompanyID: "company_a", ProjectID: "project_1", Description: "Demo cabinets", Quantity: quantity.Quantity{}}},
		}},
		&fakeMaterialsService{byCompany: map[string][]materials.Material{
			"company_a": {{ID: "material_1", CompanyID: "company_a", Name: "Tile Adhesive", ReferencePrice: money.New(1000, "USD")}},
		}},
		&fakeWorkResourcesService{byProject: map[string][]workresources.WorkResourceRequirement{}},
	)
}

func TestGetProjectAIContextReturnsScopeBrief(t *testing.T) {
	adapter := newTestAdapter()
	ctx, err := adapter.GetProjectAIContext(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.ScopeBrief != "Full renovation." {
		t.Fatalf("expected scope brief, got %q", ctx.ScopeBrief)
	}
}

func TestGetProjectAIContextTenantScoped(t *testing.T) {
	adapter := newTestAdapter()
	_, err := adapter.GetProjectAIContext(context.Background(), "company_b", "project_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestListSpacesForAIReturnsOnlyCurrentProject(t *testing.T) {
	adapter := newTestAdapter()
	list, err := adapter.ListSpacesForAI(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Kitchen" {
		t.Fatalf("unexpected spaces: %+v", list)
	}
}

func TestListWorkItemsForAIReturnsOnlyCurrentProject(t *testing.T) {
	adapter := newTestAdapter()
	list, err := adapter.ListWorkItemsForAI(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Description != "Demo cabinets" {
		t.Fatalf("unexpected work items: %+v", list)
	}
}

func TestListMaterialCandidatesForAIReturnsOnlyCallerCompany(t *testing.T) {
	adapter := newTestAdapter()
	list, err := adapter.ListMaterialCandidatesForAI(context.Background(), "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Tile Adhesive" {
		t.Fatalf("unexpected materials: %+v", list)
	}

	empty, err := adapter.ListMaterialCandidatesForAI(context.Background(), "company_b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no materials for company_b, got %+v", empty)
	}
}

func TestListMaterialCandidatesForAIIsBoundedAndStablySorted(t *testing.T) {
	byCompany := map[string][]materials.Material{"company_a": {}}
	for i := 0; i < 150; i++ {
		name := string(rune('a' + (i % 26)))
		byCompany["company_a"] = append(byCompany["company_a"], materials.Material{
			ID: name + string(rune(i)), CompanyID: "company_a", Name: name,
		})
	}
	adapter := NewAdapter(
		&fakeProjectsService{byID: map[string]projects.Project{}},
		&fakeSpacesService{byProject: map[string][]spaces.Space{}},
		&fakeWorkService{byProject: map[string][]work.WorkItem{}},
		&fakeMaterialsService{byCompany: byCompany},
		&fakeWorkResourcesService{byProject: map[string][]workresources.WorkResourceRequirement{}},
	)

	list, err := adapter.ListMaterialCandidatesForAI(context.Background(), "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) > 100 {
		t.Fatalf("expected at most 100 material candidates, got %d", len(list))
	}

	list2, err := adapter.ListMaterialCandidatesForAI(context.Background(), "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := range list {
		if list[i].ID != list2[i].ID {
			t.Fatalf("expected stable ordering across calls at index %d: %s != %s", i, list[i].ID, list2[i].ID)
		}
	}
}

func TestListResourceRequirementsForAIReturnsOnlyCurrentProject(t *testing.T) {
	adapter := NewAdapter(
		&fakeProjectsService{byID: map[string]projects.Project{}},
		&fakeSpacesService{byProject: map[string][]spaces.Space{}},
		&fakeWorkService{byProject: map[string][]work.WorkItem{}},
		&fakeMaterialsService{byCompany: map[string][]materials.Material{}},
		&fakeWorkResourcesService{byProject: map[string][]workresources.WorkResourceRequirement{
			"project_1": {{ID: "wrr_1", CompanyID: "company_a", ProjectID: "project_1", Name: "Tiler"}},
		}},
	)
	list, err := adapter.ListResourceRequirementsForAI(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Tiler" {
		t.Fatalf("unexpected requirements: %+v", list)
	}
}
