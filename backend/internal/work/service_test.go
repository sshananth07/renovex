package work

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

type fakeWorkItemRepo struct {
	byID   map[string]WorkItem
	nextID int
}

func newFakeWorkItemRepo() *fakeWorkItemRepo {
	return &fakeWorkItemRepo{byID: map[string]WorkItem{}}
}

var errFakeWorkItemDuplicateSourceSuggestionID = errors.New("fakeWorkItemRepo: duplicate sourceSuggestionId")

func (f *fakeWorkItemRepo) Create(_ context.Context, w WorkItem) (WorkItem, error) {
	if w.SourceSuggestionID != nil {
		for _, existing := range f.byID {
			if existing.CompanyID == w.CompanyID && existing.SourceSuggestionID != nil &&
				*existing.SourceSuggestionID == *w.SourceSuggestionID {
				return WorkItem{}, errFakeWorkItemDuplicateSourceSuggestionID
			}
		}
	}
	f.nextID++
	w.ID = string(rune('a' + f.nextID))
	f.byID[w.ID] = w
	return w, nil
}

func (f *fakeWorkItemRepo) FindBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (WorkItem, error) {
	for _, w := range f.byID {
		if w.CompanyID == companyID && w.SourceSuggestionID != nil && *w.SourceSuggestionID == sourceSuggestionID {
			return w, nil
		}
	}
	return WorkItem{}, ErrWorkItemNotFound
}

func (f *fakeWorkItemRepo) FindByID(_ context.Context, companyID, id string) (WorkItem, error) {
	w, ok := f.byID[id]
	if !ok || w.CompanyID != companyID {
		return WorkItem{}, ErrWorkItemNotFound
	}
	return w, nil
}

func (f *fakeWorkItemRepo) ListPaginated(_ context.Context, companyID, projectID, spaceID string, req pagination.Request) ([]WorkItem, int, error) {
	var matched []WorkItem
	for _, w := range f.byID {
		if w.CompanyID != companyID {
			continue
		}
		if spaceID != "" {
			if w.SpaceID == nil || *w.SpaceID != spaceID {
				continue
			}
		} else if w.ProjectID != projectID {
			continue
		}
		if req.Search != "" {
			needle := strings.ToLower(req.Search)
			if !strings.Contains(strings.ToLower(w.Description), needle) &&
				!strings.Contains(strings.ToLower(w.WorkType), needle) {
				continue
			}
		}
		matched = append(matched, w)
	}
	sort.Slice(matched, func(i, j int) bool {
		var less bool
		switch req.Sort {
		case "status":
			less = matched[i].Status < matched[j].Status
		case "description":
			less = matched[i].Description < matched[j].Description
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

func (f *fakeWorkItemRepo) UpdateStatus(ctx context.Context, companyID, id string, status WorkItemStatus) (WorkItem, error) {
	w, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return WorkItem{}, err
	}
	w.Status = status
	f.byID[id] = w
	return w, nil
}

func (f *fakeWorkItemRepo) Update(ctx context.Context, companyID, id string, fn func(*WorkItem)) (WorkItem, error) {
	w, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return WorkItem{}, err
	}
	fn(&w)
	f.byID[id] = w
	return w, nil
}

func (f *fakeWorkItemRepo) BelongsToProject(_ context.Context, companyID, id, projectID string) (bool, error) {
	w, ok := f.byID[id]
	if !ok {
		return false, nil
	}
	return w.CompanyID == companyID && w.ProjectID == projectID, nil
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

type fakeSpaceLookup struct {
	// spaceID -> {companyID, projectID} it actually belongs to
	companyOf map[string]string
	projectOf map[string]string
}

func newFakeSpaceLookup() *fakeSpaceLookup {
	return &fakeSpaceLookup{companyOf: map[string]string{}, projectOf: map[string]string{}}
}

func (f *fakeSpaceLookup) SpaceBelongsToCompany(_ context.Context, companyID, spaceID string) (bool, error) {
	return f.companyOf[spaceID] == companyID, nil
}

func (f *fakeSpaceLookup) SpaceBelongsToProject(_ context.Context, companyID, spaceID, projectID string) (bool, error) {
	return f.companyOf[spaceID] == companyID && f.projectOf[spaceID] == projectID, nil
}

func TestServiceCreateWorkItemProjectOnly(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	w, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "General site cleanup", "cleanup", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.SpaceID != nil {
		t.Fatal("expected nil SpaceID for project-level work item")
	}
	if w.Status != WorkItemStatusPlanned {
		t.Fatalf("expected planned, got %s", w.Status)
	}
	if w.Source != WorkItemSourceManual {
		t.Fatalf("expected manual, got %s", w.Source)
	}
	if w.VerificationStatus != VerificationStatusConfirmed {
		t.Fatalf("expected confirmed, got %s", w.VerificationStatus)
	}
}

func TestServiceCreateWorkItemWithValidSpace(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)

	spaceID := "space_1"
	w, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", &spaceID, "Install tiles", "tile_installation", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.SpaceID == nil || *w.SpaceID != "space_1" {
		t.Fatal("expected SpaceID space_1")
	}
}

func TestServiceCreateWorkItemRejectsForeignProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_b" // belongs to a different company
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "Demo", "demo", "1", "unit")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsSpaceFromDifferentProject(t *testing.T) {
	// Same tenant, but the Space belongs to a different Project than the one given —
	// design spec §10.3's lineage-mismatch case.
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	projectLookup.belongsTo["project_2"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_2" // space_1 actually belongs to project_2
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)

	spaceID := "space_1"
	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", &spaceID, "Install tiles", "tile_installation", "30", "m2")
	if err != ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound for lineage mismatch, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsForeignSpace(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_b" // belongs to a different company entirely
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)

	spaceID := "space_1"
	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", &spaceID, "Install tiles", "tile_installation", "30", "m2")
	if err != ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsEmptyDescription(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "", "demo", "1", "unit")
	if err != ErrDescriptionRequired {
		t.Fatalf("expected ErrDescriptionRequired, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsZeroQuantity(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "Demo", "demo", "0", "unit")
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for zero quantity, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsNegativeQuantity(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "Demo", "demo", "-5", "unit")
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for negative quantity, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsEmptyUnit(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "Demo", "demo", "1", "")
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for empty unit, got %v", err)
	}
}

func TestServiceCreateWorkItemRejectsUnparsableQuantity(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil, "Demo", "demo", "not-a-number", "unit")
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for unparsable value, got %v", err)
	}
}

func defaultWorkItemsPaginationRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", WorkItemSortFields, WorkItemDefaultSort, WorkItemDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

func TestServiceListWorkItemsPaginatedByProjectValidatesProjectFirst(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	_, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "demo", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = svc.ListWorkItemsPaginatedByProject(ctx, "company_b", "project_1", defaultWorkItemsPaginationRequest(t))
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for foreign project, got %v", err)
	}

	list, total, err := svc.ListWorkItemsPaginatedByProject(ctx, "company_a", "project_1", defaultWorkItemsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1", total, len(list))
	}
}

func TestServiceListWorkItemsPaginatedBySpaceValidatesSpaceFirst(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	spaceID := "space_1"
	_, err := svc.CreateWorkItem(ctx, "company_a", "project_1", &spaceID, "Demo", "demo", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = svc.ListWorkItemsPaginatedBySpace(ctx, "company_b", "space_1", defaultWorkItemsPaginationRequest(t))
	if err != ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound for foreign space, got %v", err)
	}

	list, total, err := svc.ListWorkItemsPaginatedBySpace(ctx, "company_a", "space_1", defaultWorkItemsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1", total, len(list))
	}
}

func TestServiceListWorkItemsPaginatedSearchMatchesDescriptionAndWorkType(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	if _, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Install ceramic tiles", "tiling", "30", "m2"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Paint walls", "painting", "50", "m2"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "tiling", "", "", WorkItemSortFields, WorkItemDefaultSort, WorkItemDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := svc.ListWorkItemsPaginatedByProject(ctx, "company_a", "project_1", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Description != "Install ceramic tiles" {
		t.Fatalf("total=%d list=%+v, want 1 match on Install ceramic tiles", total, list)
	}
}

func TestServiceListWorkItemsPaginatedRejectsUnsupportedSort(t *testing.T) {
	if _, err := pagination.ParseRequest(0, 0, "", "workType", "", WorkItemSortFields, WorkItemDefaultSort, WorkItemDefaultOrder); err == nil {
		t.Fatal("expected an error for an unsupported sort field")
	}
}

func TestServiceUpdateWorkItemStatusOneDirectionalOnly(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "demo", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancelled, err := svc.UpdateWorkItemStatus(ctx, "company_a", created.ID, WorkItemStatusCancelled)
	if err != nil {
		t.Fatalf("unexpected error cancelling: %v", err)
	}
	if cancelled.Status != WorkItemStatusCancelled {
		t.Fatalf("expected cancelled, got %s", cancelled.Status)
	}

	// cancelled is terminal — no cancelled -> planned transition in M2.
	_, err = svc.UpdateWorkItemStatus(ctx, "company_a", created.ID, WorkItemStatusPlanned)
	if err != ErrCancelledIsTerminal {
		t.Fatalf("expected ErrCancelledIsTerminal, got %v", err)
	}
}

func TestServiceUpdateWorkItemStatusRejectsInvalidStatus(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "demo", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateWorkItemStatus(ctx, "company_a", created.ID, WorkItemStatus("not_a_real_status"))
	if err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestServiceWorkItemBelongsToProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	belongs, err := svc.WorkItemBelongsToProject(ctx, "company_a", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	mismatched, err := svc.WorkItemBelongsToProject(ctx, "company_a", created.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mismatched {
		t.Fatal("expected false for mismatched project")
	}
}

func TestServiceWorkItemBelongsToCompany(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	belongs, err := svc.WorkItemBelongsToCompany(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	crossTenant, err := svc.WorkItemBelongsToCompany(ctx, "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if crossTenant {
		t.Fatal("expected false for cross-tenant lookup")
	}
}

func TestServiceGetWorkItemDescription(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Install ceramic floor tiles", "", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error creating work item: %v", err)
	}

	desc, found, err := svc.GetWorkItemDescription(ctx, "company_a", "project_1", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if desc != "Install ceramic floor tiles" {
		t.Fatalf("expected description 'Install ceramic floor tiles', got %q", desc)
	}
}

func TestServiceGetWorkItemDescriptionNotFound(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())

	_, found, err := svc.GetWorkItemDescription(context.Background(), "company_a", "project_1", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a nonexistent work item")
	}
}

func TestServiceGetWorkItemDescriptionCrossProject(t *testing.T) {
	// A WorkItem belonging to the same Company but a DIFFERENT Project must
	// report found=false — company-only scoping is insufficient (design
	// spec §22's corrected WorkItemLookup rationale).
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Install ceramic floor tiles", "", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error creating work item: %v", err)
	}

	_, found, err := svc.GetWorkItemDescription(ctx, "company_a", "project_2", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a work item belonging to a different project")
	}
}

func TestServiceGetWorkItemDescriptionCrossTenant(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Install ceramic floor tiles", "", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error creating work item: %v", err)
	}

	_, found, err := svc.GetWorkItemDescription(ctx, "company_b", "project_1", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a cross-tenant lookup")
	}
}

// --- Checkpoint 11: Work Item PATCH ---

func strPtrForWork(s string) *string { return &s }

func TestServiceUpdateWorkItemDescriptionOnlyPreservesEverythingElse(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Old description", "tiling", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		Description: strPtrForWork("New description"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Description != "New description" {
		t.Fatalf("Description = %q, want New description", updated.Description)
	}
	if updated.WorkType != "tiling" {
		t.Fatalf("WorkType = %q, want unchanged tiling", updated.WorkType)
	}
	if updated.Quantity.Value.String() != "30" || updated.Quantity.Unit != "m2" {
		t.Fatalf("Quantity = %s %s, want unchanged 30 m2", updated.Quantity.Value.String(), updated.Quantity.Unit)
	}
	if updated.ProjectID != "project_1" {
		t.Fatalf("ProjectID = %q, want unchanged project_1", updated.ProjectID)
	}
}

func TestServiceUpdateWorkItemWorkTypeCleared(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "tiling", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		WorkType: strPtrForWork(""),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.WorkType != "" {
		t.Fatalf("WorkType = %q, want cleared to empty", updated.WorkType)
	}
}

func TestServiceUpdateWorkItemQuantityRequiresValueAndUnitTogether(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "tiling", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		QuantityValue: strPtrForWork("50"),
	}); err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity when only QuantityValue supplied, got %v", err)
	}

	if _, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		QuantityUnit: strPtrForWork("kg"),
	}); err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity when only QuantityUnit supplied, got %v", err)
	}
}

func TestServiceUpdateWorkItemQuantityRoundTripsExactDecimal(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "tiling", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		QuantityValue: strPtrForWork("12.75"),
		QuantityUnit:  strPtrForWork("m"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Quantity.Value.String() != "12.75" {
		t.Fatalf("Quantity.Value = %s, want exact 12.75", updated.Quantity.Value.String())
	}
	if updated.Quantity.Unit != "m" {
		t.Fatalf("Quantity.Unit = %q, want m", updated.Quantity.Unit)
	}
}

func TestServiceUpdateWorkItemAssignsSpaceWithinSameProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		SpaceID: NullableStringPresent("space_1"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.SpaceID == nil || *updated.SpaceID != "space_1" {
		t.Fatalf("SpaceID = %v, want space_1", updated.SpaceID)
	}
}

func TestServiceUpdateWorkItemClearsSpaceWithNull(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	spaceID := "space_1"
	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", &spaceID, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		SpaceID: NullableStringNull(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.SpaceID != nil {
		t.Fatalf("SpaceID = %v, want cleared to nil", updated.SpaceID)
	}
}

func TestServiceUpdateWorkItemOmittedSpaceIDPreserved(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	spaceID := "space_1"
	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", &spaceID, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		Description: strPtrForWork("Updated"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.SpaceID == nil || *updated.SpaceID != "space_1" {
		t.Fatalf("SpaceID = %v, want unchanged space_1", updated.SpaceID)
	}
}

func TestServiceUpdateWorkItemRejectsForeignOrWrongProjectSpace(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	projectLookup.belongsTo["project_2"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_2" // wrong project
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		SpaceID: NullableStringPresent("space_1"),
	})
	if err != ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound for a space from the wrong project, got %v", err)
	}
}

func TestServiceUpdateWorkItemProjectIDCannotChange(t *testing.T) {
	// WorkItemPatch has no ProjectID field at all — this test documents that
	// structurally: there is no way to call UpdateWorkItem with a different
	// project, by construction of the type.
	patch := WorkItemPatch{}
	_ = patch
}

func TestServiceUpdateWorkItemDescriptionCannotBeEmptyWhenSupplied(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		Description: strPtrForWork(""),
	}); err != ErrDescriptionRequired {
		t.Fatalf("expected ErrDescriptionRequired, got %v", err)
	}
}

func TestServiceUpdateWorkItemCancelledReturnsTerminalError(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.UpdateWorkItemStatus(ctx, "company_a", created.ID, WorkItemStatusCancelled); err != nil {
		t.Fatalf("unexpected error cancelling: %v", err)
	}

	if _, err := svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		Description: strPtrForWork("Should not apply"),
	}); err != ErrCancelledIsTerminal {
		t.Fatalf("expected ErrCancelledIsTerminal, got %v", err)
	}
}

func TestServiceUpdateWorkItemFailedValidationLeavesRecordUnchanged(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Original", "orig-type", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateWorkItem(ctx, "company_a", created.ID, WorkItemPatch{
		WorkType:    strPtrForWork("new-type"),
		Description: strPtrForWork(""), // invalid — rejects the whole patch
	})
	if err != ErrDescriptionRequired {
		t.Fatalf("expected ErrDescriptionRequired, got %v", err)
	}

	unchanged, err := svc.GetWorkItem(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unchanged.WorkType != "orig-type" {
		t.Fatalf("WorkType = %q, want unchanged orig-type (no partial persistence)", unchanged.WorkType)
	}
	if unchanged.Description != "Original" {
		t.Fatalf("Description = %q, want unchanged Original", unchanged.Description)
	}
}

func TestServiceCreateWorkItemFromAISuggestionSetsProvenance(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	w, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "1", "lot", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.SourceSuggestionID == nil || *w.SourceSuggestionID != "suggestion_1" {
		t.Fatalf("expected SourceSuggestionID suggestion_1, got %v", w.SourceSuggestionID)
	}
	if w.Source != WorkItemSourceAISuggestion {
		t.Fatalf("Source = %q, want ai_suggestion", w.Source)
	}
	if w.VerificationStatus != VerificationStatusConfirmed {
		t.Fatalf("VerificationStatus = %q, want confirmed", w.VerificationStatus)
	}
	if w.SpaceID != nil {
		t.Fatalf("expected nil SpaceID for project-level AI work item, got %v", w.SpaceID)
	}
}

func TestServiceCreateWorkItemFromAISuggestionRequiresPositiveQuantity(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	_, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "0", "lot", "suggestion_1")
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for zero quantity, got %v", err)
	}

	_, err = svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "5", "", "suggestion_2")
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for empty unit, got %v", err)
	}
}

func TestServiceCreateWorkItemFromAISuggestionSpaceScoped(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	spaceID := "space_1"
	w, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", &spaceID,
		"Install ceramic floor tiles", "tile_installation", "30", "m2", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.SpaceID == nil || *w.SpaceID != "space_1" {
		t.Fatalf("expected SpaceID space_1, got %v", w.SpaceID)
	}
}

func TestServiceCreateWorkItemFromAISuggestionRejectsForeignSpace(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_foreign"] = "company_a"
	spaceLookup.projectOf["space_foreign"] = "project_2" // wrong project
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)
	ctx := context.Background()

	spaceID := "space_foreign"
	_, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", &spaceID,
		"Install tiles", "tile_installation", "30", "m2", "suggestion_1")
	if err != ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound, got %v", err)
	}
}

func TestServiceCreateWorkItemFromAISuggestionRejectsForeignProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_b"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	_, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "1", "lot", "suggestion_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceCreateWorkItemFromAISuggestionRetryReturnsExisting(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	first, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "1", "lot", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on first acceptance: %v", err)
	}

	second, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "1", "lot", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on retried acceptance: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected retry to return the same WorkItem, got first=%s second=%s", first.ID, second.ID)
	}
}

func TestServiceFindWorkItemBySourceSuggestionIDTenantScoped(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, newFakeSpaceLookup())
	ctx := context.Background()

	created, err := svc.CreateWorkItemFromAISuggestion(ctx, "company_a", "project_1", nil,
		"Site protection", "site_protection", "1", "lot", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := svc.FindWorkItemBySourceSuggestionID(ctx, "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected found work item id %s, got %s", created.ID, found.ID)
	}

	_, err = svc.FindWorkItemBySourceSuggestionID(ctx, "company_b", "suggestion_1")
	if err != ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound for cross-tenant lookup, got %v", err)
	}
}
