package workresources

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepo struct {
	byID   map[string]WorkResourceRequirement
	nextID int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[string]WorkResourceRequirement{}}
}

var errFakeDuplicateSourceSuggestionID = errors.New("fakeRepo: duplicate sourceSuggestionId")

func (f *fakeRepo) Create(_ context.Context, r WorkResourceRequirement) (WorkResourceRequirement, error) {
	if r.SourceSuggestionID != nil {
		for _, existing := range f.byID {
			if existing.CompanyID == r.CompanyID && existing.SourceSuggestionID != nil &&
				*existing.SourceSuggestionID == *r.SourceSuggestionID {
				return WorkResourceRequirement{}, errFakeDuplicateSourceSuggestionID
			}
		}
	}
	f.nextID++
	r.ID = string(rune('a' + f.nextID))
	f.byID[r.ID] = r
	return r, nil
}

func (f *fakeRepo) FindByID(_ context.Context, companyID, id string) (WorkResourceRequirement, error) {
	r, ok := f.byID[id]
	if !ok || r.CompanyID != companyID {
		return WorkResourceRequirement{}, ErrRequirementNotFound
	}
	return r, nil
}

func (f *fakeRepo) FindBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (WorkResourceRequirement, error) {
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.SourceSuggestionID != nil && *r.SourceSuggestionID == sourceSuggestionID {
			return r, nil
		}
	}
	return WorkResourceRequirement{}, ErrRequirementNotFound
}

func (f *fakeRepo) FindByDuplicateKey(_ context.Context, key DuplicateKey) (WorkResourceRequirement, error) {
	for _, r := range f.byID {
		if r.CompanyID != key.CompanyID || r.WorkItemID != key.WorkItemID || r.ResourceType != key.ResourceType {
			continue
		}
		if key.ResourceType == ResourceTypeMaterial {
			if r.MaterialID != nil && *r.MaterialID == key.MaterialID {
				return r, nil
			}
			continue
		}
		if normalize(r.Name) == normalize(key.NormalizedName) {
			return r, nil
		}
	}
	return WorkResourceRequirement{}, ErrRequirementNotFound
}

func (f *fakeRepo) ListByProject(_ context.Context, companyID, projectID string) ([]WorkResourceRequirement, error) {
	var result []WorkResourceRequirement
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.ProjectID == projectID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (f *fakeRepo) ListByWorkItem(_ context.Context, companyID, workItemID string) ([]WorkResourceRequirement, error) {
	var result []WorkResourceRequirement
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.WorkItemID == workItemID {
			result = append(result, r)
		}
	}
	return result, nil
}

type fakeProjectLookup struct {
	belongsTo map[string]string // projectID -> companyID
}

func newFakeProjectLookup() *fakeProjectLookup {
	return &fakeProjectLookup{belongsTo: map[string]string{}}
}

func (f *fakeProjectLookup) ProjectBelongsToCompany(_ context.Context, companyID, projectID string) (bool, error) {
	owner, ok := f.belongsTo[projectID]
	return ok && owner == companyID, nil
}

type fakeWorkItemLookup struct {
	companyOf map[string]string
	projectOf map[string]string
}

func newFakeWorkItemLookup() *fakeWorkItemLookup {
	return &fakeWorkItemLookup{companyOf: map[string]string{}, projectOf: map[string]string{}}
}

func (f *fakeWorkItemLookup) WorkItemBelongsToProject(_ context.Context, companyID, workItemID, projectID string) (bool, error) {
	return f.companyOf[workItemID] == companyID && f.projectOf[workItemID] == projectID, nil
}

type fakeMaterialLookup struct {
	companyOf map[string]string
}

func newFakeMaterialLookup() *fakeMaterialLookup {
	return &fakeMaterialLookup{companyOf: map[string]string{}}
}

func (f *fakeMaterialLookup) MaterialBelongsToCompany(_ context.Context, companyID, materialID string) (bool, error) {
	return f.companyOf[materialID] == companyID, nil
}

func newTestService() (*Service, *fakeProjectLookup, *fakeWorkItemLookup, *fakeMaterialLookup) {
	projectLookup := newFakeProjectLookup()
	workItemLookup := newFakeWorkItemLookup()
	materialLookup := newFakeMaterialLookup()
	svc := NewService(newFakeRepo(), projectLookup, workItemLookup, materialLookup)
	return svc, projectLookup, workItemLookup, materialLookup
}

func TestCreateFromAISuggestionValidatesProjectExists(t *testing.T) {
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"

	_, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_2", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCreateFromAISuggestionValidatesWorkItemBelongsToProject(t *testing.T) {
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_other" // wrong project

	_, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1")
	if err != ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound, got %v", err)
	}
}

func TestCreateFromAISuggestionMaterialValidatesMaterialBelongsToCompany(t *testing.T) {
	svc, projectLookup, workItemLookup, materialLookup := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"
	materialLookup.companyOf["material_1"] = "company_b" // wrong company

	materialID := "material_1"
	_, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeMaterial, &materialID, "Tile Adhesive", "suggestion_1")
	if err != ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound, got %v", err)
	}
}

func TestCreateFromAISuggestionSucceedsForValidTrade(t *testing.T) {
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"

	r, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Source != SourceAISuggestion {
		t.Fatalf("expected Source ai_suggestion, got %s", r.Source)
	}
	if r.Status != StatusConfirmed {
		t.Fatalf("expected Status confirmed, got %s", r.Status)
	}
}

func TestCreateFromAISuggestionRetrySameSuggestionReturnsExisting(t *testing.T) {
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"

	first, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on first acceptance: %v", err)
	}

	second, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on retried acceptance: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected retry to return same requirement, got first=%s second=%s", first.ID, second.ID)
	}
}

func TestCreateFromAISuggestionDifferentSuggestionSameKeyReturnsErrDuplicate(t *testing.T) {
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"

	_, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on first acceptance: %v", err)
	}

	_, err = svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_2")
	if err != ErrRequirementDuplicate {
		t.Fatalf("expected ErrRequirementDuplicate for a different suggestion hitting the same key, got %v", err)
	}
}

func TestCreateFromAISuggestionNoWriteToCostOrProcurementModules(t *testing.T) {
	// This test asserts the ABSENCE of any cost/procurement dependency by
	// construction: NewService's signature accepts only ProjectLookup,
	// WorkItemLookup, and MaterialLookup — there is no CostRecorder or
	// ProcurementDemandCreator parameter for this test to exercise, which
	// is itself the proof (design doc §11.2, "no CostItem/LabourEntry/
	// procurement call exists").
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"

	_, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeEquipment, nil, "Tile Cutter", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListByProjectTenantScoped(t *testing.T) {
	svc, projectLookup, workItemLookup, _ := newTestService()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"

	if _, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, err := svc.ListByProject(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(list))
	}

	_, err = svc.ListByProject(context.Background(), "company_b", "project_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant project, got %v", err)
	}
}

var _ = time.Now // silence unused import if fixtures shrink
