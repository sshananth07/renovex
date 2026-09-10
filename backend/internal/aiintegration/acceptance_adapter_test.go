package aiintegration

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/ai"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

const aiResourceTypeMaterial = ai.ResourceRequirementTypeMaterial

// --- fakes satisfying the real spaces/work/materials/workresources
// service methods the acceptance adapters need ---

type fakeSpacesAcceptanceService struct {
	created   map[string]spaces.Space // sourceSuggestionId -> Space
	byID      map[string]spaces.Space
	byProject map[string][]spaces.Space
	nextID    int
	createErr error
}

func newFakeSpacesAcceptanceService() *fakeSpacesAcceptanceService {
	return &fakeSpacesAcceptanceService{created: map[string]spaces.Space{}, byID: map[string]spaces.Space{}, byProject: map[string][]spaces.Space{}}
}

func (f *fakeSpacesAcceptanceService) CreateSpaceFromAISuggestion(_ context.Context, companyID, projectID, name, spaceType, description, sourceSuggestionID string) (spaces.Space, error) {
	if f.createErr != nil {
		return spaces.Space{}, f.createErr
	}
	if existing, ok := f.created[sourceSuggestionID]; ok {
		return existing, nil
	}
	f.nextID++
	id := "space_" + string(rune('a'+f.nextID))
	s := spaces.Space{ID: id, CompanyID: companyID, ProjectID: projectID, Name: name, Type: spaceType}
	f.created[sourceSuggestionID] = s
	f.byID[id] = s
	f.byProject[projectID] = append(f.byProject[projectID], s)
	return s, nil
}

func (f *fakeSpacesAcceptanceService) FindSpaceBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (spaces.Space, error) {
	s, ok := f.created[sourceSuggestionID]
	if !ok || s.CompanyID != companyID {
		return spaces.Space{}, spaces.ErrSpaceNotFound
	}
	return s, nil
}

func (f *fakeSpacesAcceptanceService) ListSpacesPaginated(_ context.Context, companyID, projectID string, _ pagination.Request) ([]spaces.Space, int, error) {
	var result []spaces.Space
	for _, s := range f.byProject[projectID] {
		if s.CompanyID == companyID {
			result = append(result, s)
		}
	}
	return result, len(result), nil
}

func (f *fakeSpacesAcceptanceService) UpdateSpace(_ context.Context, companyID, spaceID, name, spaceType, description string) (spaces.Space, error) {
	existing, ok := f.byID[spaceID]
	if !ok || existing.CompanyID != companyID {
		return spaces.Space{}, spaces.ErrSpaceNotFound
	}
	existing.Name = name
	existing.Type = spaceType
	existing.Description = description
	f.byID[spaceID] = existing
	return existing, nil
}

func TestSpaceAcceptanceAdapterCreatesAndFinds(t *testing.T) {
	svc := newFakeSpacesAcceptanceService()
	adapter := NewSpaceAcceptanceAdapter(svc)

	result, err := adapter.CreateSpaceFromAISuggestion(context.Background(), "company_a", "project_1", "Kitchen", "kitchen", "", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected created Space ID")
	}

	found, err := adapter.FindSpaceBySourceSuggestionID(context.Background(), "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != result.ID {
		t.Fatalf("expected same Space, got %s vs %s", found.ID, result.ID)
	}
}

func TestSpaceAcceptanceAdapterDetectsLikelyDuplicate(t *testing.T) {
	svc := newFakeSpacesAcceptanceService()
	svc.byProject["project_1"] = []spaces.Space{{ID: "space_existing", CompanyID: "company_a", ProjectID: "project_1", Name: "Kitchen", Type: "kitchen"}}
	adapter := NewSpaceAcceptanceAdapter(svc)

	dup, err := adapter.SpaceLikelyDuplicate(context.Background(), "company_a", "project_1", "kitchen", "Kitchen")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dup {
		t.Fatal("expected likely duplicate for normalized case-insensitive match")
	}

	notDup, err := adapter.SpaceLikelyDuplicate(context.Background(), "company_a", "project_1", "Master Bathroom", "bathroom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notDup {
		t.Fatal("expected no duplicate for a distinct name")
	}
}

// --- WorkItem acceptance adapter ---

type fakeWorkAcceptanceService struct {
	created   map[string]work.WorkItem
	byID      map[string]work.WorkItem
	byProject map[string][]work.WorkItem
	nextID    int
}

func newFakeWorkAcceptanceService() *fakeWorkAcceptanceService {
	return &fakeWorkAcceptanceService{created: map[string]work.WorkItem{}, byID: map[string]work.WorkItem{}, byProject: map[string][]work.WorkItem{}}
}

func (f *fakeWorkAcceptanceService) CreateWorkItemFromAISuggestion(_ context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit, sourceSuggestionID string) (work.WorkItem, error) {
	if existing, ok := f.created[sourceSuggestionID]; ok {
		return existing, nil
	}
	f.nextID++
	w := work.WorkItem{ID: "work_" + string(rune('a'+f.nextID)), CompanyID: companyID, ProjectID: projectID, SpaceID: spaceID, Description: description}
	f.created[sourceSuggestionID] = w
	f.byID[w.ID] = w
	f.byProject[projectID] = append(f.byProject[projectID], w)
	return w, nil
}

func (f *fakeWorkAcceptanceService) UpdateWorkItem(_ context.Context, companyID, workItemID string, patch work.WorkItemPatch) (work.WorkItem, error) {
	existing, ok := f.byID[workItemID]
	if !ok || existing.CompanyID != companyID {
		return work.WorkItem{}, work.ErrWorkItemNotFound
	}
	if patch.Description != nil {
		existing.Description = *patch.Description
	}
	if patch.WorkType != nil {
		existing.WorkType = *patch.WorkType
	}
	f.byID[workItemID] = existing
	return existing, nil
}

func (f *fakeWorkAcceptanceService) FindWorkItemBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (work.WorkItem, error) {
	w, ok := f.created[sourceSuggestionID]
	if !ok || w.CompanyID != companyID {
		return work.WorkItem{}, work.ErrWorkItemNotFound
	}
	return w, nil
}

func (f *fakeWorkAcceptanceService) ListWorkItemsPaginatedByProject(_ context.Context, companyID, projectID string, _ pagination.Request) ([]work.WorkItem, int, error) {
	var result []work.WorkItem
	for _, w := range f.byProject[projectID] {
		if w.CompanyID == companyID {
			result = append(result, w)
		}
	}
	return result, len(result), nil
}

func TestWorkItemAcceptanceAdapterCreatesAndFinds(t *testing.T) {
	svc := newFakeWorkAcceptanceService()
	adapter := NewWorkItemAcceptanceAdapter(svc)

	result, err := adapter.CreateWorkItemFromAISuggestion(context.Background(), "company_a", "project_1", nil, "Site protection", "x", "1", "lot", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected created WorkItem")
	}

	found, err := adapter.FindWorkItemBySourceSuggestionID(context.Background(), "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != result.ID {
		t.Fatalf("expected same WorkItem: %s vs %s", found.ID, result.ID)
	}
}

func TestWorkItemAcceptanceAdapterDetectsLikelyDuplicate(t *testing.T) {
	svc := newFakeWorkAcceptanceService()
	svc.byProject["project_1"] = []work.WorkItem{{ID: "work_existing", CompanyID: "company_a", ProjectID: "project_1", Description: "Install tiles"}}
	adapter := NewWorkItemAcceptanceAdapter(svc)

	dup, err := adapter.WorkItemLikelyDuplicate(context.Background(), "company_a", "project_1", nil, "install tiles")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dup {
		t.Fatal("expected likely duplicate for normalized case-insensitive match")
	}
}

// --- Resource acceptance adapter ---

type fakeWorkResourcesAcceptanceService struct {
	created   map[string]workresources.WorkResourceRequirement
	byProject map[string][]workresources.WorkResourceRequirement
	nextID    int
}

func newFakeWorkResourcesAcceptanceService() *fakeWorkResourcesAcceptanceService {
	return &fakeWorkResourcesAcceptanceService{
		created: map[string]workresources.WorkResourceRequirement{}, byProject: map[string][]workresources.WorkResourceRequirement{},
	}
}

func (f *fakeWorkResourcesAcceptanceService) CreateFromAISuggestion(_ context.Context, companyID, projectID, workItemID string, resourceType workresources.ResourceType, materialID *string, name, sourceSuggestionID string) (workresources.WorkResourceRequirement, error) {
	if existing, ok := f.created[sourceSuggestionID]; ok {
		return existing, nil
	}
	f.nextID++
	r := workresources.WorkResourceRequirement{
		ID: "wrr_" + string(rune('a'+f.nextID)), CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID,
		ResourceType: resourceType, MaterialID: materialID, Name: name,
	}
	f.created[sourceSuggestionID] = r
	f.byProject[projectID] = append(f.byProject[projectID], r)
	return r, nil
}

func (f *fakeWorkResourcesAcceptanceService) FindBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (workresources.WorkResourceRequirement, error) {
	r, ok := f.created[sourceSuggestionID]
	if !ok || r.CompanyID != companyID {
		return workresources.WorkResourceRequirement{}, workresources.ErrRequirementNotFound
	}
	return r, nil
}

func (f *fakeWorkResourcesAcceptanceService) ListByProject(_ context.Context, companyID, projectID string) ([]workresources.WorkResourceRequirement, error) {
	var result []workresources.WorkResourceRequirement
	for _, r := range f.byProject[projectID] {
		if r.CompanyID == companyID {
			result = append(result, r)
		}
	}
	return result, nil
}

type fakeMaterialsAcceptanceService struct {
	byID                map[string]materials.Material
	createdBySuggestion map[string]materials.Material
	nextID              int
}

func newFakeMaterialsAcceptanceService() *fakeMaterialsAcceptanceService {
	return &fakeMaterialsAcceptanceService{byID: map[string]materials.Material{}, createdBySuggestion: map[string]materials.Material{}}
}

func (f *fakeMaterialsAcceptanceService) MaterialBelongsToCompany(_ context.Context, companyID, materialID string) (bool, error) {
	m, ok := f.byID[materialID]
	return ok && m.CompanyID == companyID, nil
}

func (f *fakeMaterialsAcceptanceService) CreateMaterialFromAISuggestion(_ context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency, sourceSuggestionID string) (materials.Material, error) {
	if existing, ok := f.createdBySuggestion[sourceSuggestionID]; ok {
		return existing, nil
	}
	f.nextID++
	m := materials.Material{ID: "material_" + string(rune('a'+f.nextID)), CompanyID: companyID, Name: name}
	f.byID[m.ID] = m
	f.createdBySuggestion[sourceSuggestionID] = m
	return m, nil
}

func (f *fakeMaterialsAcceptanceService) FindMaterialBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (materials.Material, error) {
	m, ok := f.createdBySuggestion[sourceSuggestionID]
	if !ok || m.CompanyID != companyID {
		return materials.Material{}, materials.ErrMaterialNotFound
	}
	return m, nil
}

func TestResourceAcceptanceAdapterCreatesAndFindsRequirement(t *testing.T) {
	wrrSvc := newFakeWorkResourcesAcceptanceService()
	matSvc := newFakeMaterialsAcceptanceService()
	adapter := NewResourceAcceptanceAdapter(wrrSvc, matSvc)

	materialID := "material_1"
	matSvc.byID[materialID] = materials.Material{ID: materialID, CompanyID: "company_a", Name: "Tile Adhesive"}

	result, err := adapter.CreateResourceRequirementFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		aiResourceTypeMaterial, &materialID, "Tile Adhesive", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequirementID == "" {
		t.Fatal("expected created requirement")
	}

	found, err := adapter.FindResourceRequirementBySourceSuggestionID(context.Background(), "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.RequirementID != result.RequirementID {
		t.Fatalf("expected same requirement: %s vs %s", found.RequirementID, result.RequirementID)
	}
}

func TestResourceAcceptanceAdapterMaterialBelongsToCompany(t *testing.T) {
	wrrSvc := newFakeWorkResourcesAcceptanceService()
	matSvc := newFakeMaterialsAcceptanceService()
	adapter := NewResourceAcceptanceAdapter(wrrSvc, matSvc)
	matSvc.byID["material_1"] = materials.Material{ID: "material_1", CompanyID: "company_a"}

	belongs, err := adapter.MaterialBelongsToCompany(context.Background(), "company_a", "material_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected material to belong")
	}

	belongs, err = adapter.MaterialBelongsToCompany(context.Background(), "company_b", "material_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false for wrong company")
	}
}

func TestResourceAcceptanceAdapterCreateAndAddMaterial(t *testing.T) {
	wrrSvc := newFakeWorkResourcesAcceptanceService()
	matSvc := newFakeMaterialsAcceptanceService()
	adapter := NewResourceAcceptanceAdapter(wrrSvc, matSvc)

	result, err := adapter.CreateMaterialFromAISuggestion(context.Background(), "company_a", "Tile Spacers", "tile", "", "bag", 500, "USD", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected created Material")
	}

	found, err := adapter.FindMaterialBySourceSuggestionID(context.Background(), "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != result.ID {
		t.Fatalf("expected same Material: %s vs %s", found.ID, result.ID)
	}
}
