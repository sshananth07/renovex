package spaces

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

type fakeSpaceRepo struct {
	byID   map[string]Space
	nextID int
}

func newFakeSpaceRepo() *fakeSpaceRepo {
	return &fakeSpaceRepo{byID: map[string]Space{}}
}

func (f *fakeSpaceRepo) Create(_ context.Context, s Space) (Space, error) {
	if s.SourceSuggestionID != nil {
		for _, existing := range f.byID {
			if existing.CompanyID == s.CompanyID && existing.SourceSuggestionID != nil &&
				*existing.SourceSuggestionID == *s.SourceSuggestionID {
				return Space{}, errFakeDuplicateSourceSuggestionID
			}
		}
	}
	f.nextID++
	s.ID = string(rune('a' + f.nextID))
	f.byID[s.ID] = s
	return s, nil
}

func (f *fakeSpaceRepo) FindByID(_ context.Context, companyID, id string) (Space, error) {
	s, ok := f.byID[id]
	if !ok || s.CompanyID != companyID {
		return Space{}, ErrSpaceNotFound
	}
	return s, nil
}

func (f *fakeSpaceRepo) ListPaginated(_ context.Context, companyID, projectID string, req pagination.Request) ([]Space, int, error) {
	var matched []Space
	for _, s := range f.byID {
		if s.CompanyID != companyID || s.ProjectID != projectID {
			continue
		}
		if req.Search != "" {
			needle := strings.ToLower(req.Search)
			if !strings.Contains(strings.ToLower(s.Name), needle) &&
				!strings.Contains(strings.ToLower(s.Type), needle) &&
				!strings.Contains(strings.ToLower(s.Description), needle) {
				continue
			}
		}
		matched = append(matched, s)
	}
	sort.Slice(matched, func(i, j int) bool {
		var less bool
		switch req.Sort {
		case "name":
			less = matched[i].Name < matched[j].Name
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

func (f *fakeSpaceRepo) Update(ctx context.Context, companyID, id string, fn func(*Space)) (Space, error) {
	s, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Space{}, err
	}
	fn(&s)
	f.byID[id] = s
	return s, nil
}

func (f *fakeSpaceRepo) BelongsToProject(_ context.Context, companyID, id, projectID string) (bool, error) {
	s, ok := f.byID[id]
	if !ok {
		return false, nil
	}
	return s.CompanyID == companyID && s.ProjectID == projectID, nil
}

var errFakeDuplicateSourceSuggestionID = errors.New("fakeSpaceRepo: duplicate sourceSuggestionId")

func (f *fakeSpaceRepo) FindBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (Space, error) {
	for _, s := range f.byID {
		if s.CompanyID == companyID && s.SourceSuggestionID != nil && *s.SourceSuggestionID == sourceSuggestionID {
			return s, nil
		}
	}
	return Space{}, ErrSpaceNotFound
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

func TestServiceCreateSpaceValidatesProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)

	s, err := svc.CreateSpace(context.Background(), "company_a", "project_1", "Master Bathroom", "bathroom", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ProjectID != "project_1" {
		t.Fatalf("expected project_1, got %s", s.ProjectID)
	}
}

func TestServiceCreateSpaceRejectsForeignProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_b" // belongs to a different company
	svc := NewService(newFakeSpaceRepo(), projectLookup)

	_, err := svc.CreateSpace(context.Background(), "company_a", "project_1", "Master Bathroom", "bathroom", "")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceCreateSpaceRejectsEmptyName(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)

	_, err := svc.CreateSpace(context.Background(), "company_a", "project_1", "", "bathroom", "")
	if err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func defaultSpacesPaginationRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", SpaceSortFields, SpaceDefaultSort, SpaceDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

func TestServiceListSpacesPaginatedByProjectValidatesProjectFirst(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	_, err := svc.CreateSpace(ctx, "company_a", "project_1", "Kitchen", "kitchen", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = svc.ListSpacesPaginated(ctx, "company_b", "project_1", defaultSpacesPaginationRequest(t))
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for foreign project, got %v", err)
	}

	list, total, err := svc.ListSpacesPaginated(ctx, "company_a", "project_1", defaultSpacesPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1", total, len(list))
	}
}

func TestServiceListSpacesPaginatedSearchMatchesNameTypeDescription(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	if _, err := svc.CreateSpace(ctx, "company_a", "project_1", "Master Bathroom", "bathroom", "renovate tiles"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateSpace(ctx, "company_a", "project_1", "Kitchen", "kitchen", "new cabinets"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "cabinets", "", "", SpaceSortFields, SpaceDefaultSort, SpaceDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := svc.ListSpacesPaginated(ctx, "company_a", "project_1", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Name != "Kitchen" {
		t.Fatalf("total=%d list=%+v, want 1 match on Kitchen", total, list)
	}
}

func TestServiceListSpacesPaginatedRejectsUnsupportedSort(t *testing.T) {
	if _, err := pagination.ParseRequest(0, 0, "", "type", "", SpaceSortFields, SpaceDefaultSort, SpaceDefaultOrder); err == nil {
		t.Fatal("expected an error for an unsupported sort field")
	}
}

func TestServiceUpdateSpace(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	created, err := svc.CreateSpace(ctx, "company_a", "project_1", "Old Name", "bathroom", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateSpace(ctx, "company_a", created.ID, "New Name", "bathroom", "notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected New Name, got %s", updated.Name)
	}
}

func TestServiceSpaceBelongsToCompany(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	created, err := svc.CreateSpace(ctx, "company_a", "project_1", "Kitchen", "kitchen", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	belongs, err := svc.SpaceBelongsToCompany(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	belongs, err = svc.SpaceBelongsToCompany(ctx, "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false for wrong company")
	}
}

func TestServiceSpaceBelongsToProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	created, err := svc.CreateSpace(ctx, "company_a", "project_1", "Kitchen", "kitchen", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	belongs, err := svc.SpaceBelongsToProject(ctx, "company_a", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true for correct company+project")
	}

	belongs, err = svc.SpaceBelongsToProject(ctx, "company_a", created.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false — space belongs to project_1, not project_2")
	}
}

func TestServiceCreateSpaceFromAISuggestionSetsProvenance(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	s, err := svc.CreateSpaceFromAISuggestion(ctx, "company_a", "project_1", "Master Bathroom", "bathroom", "", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.SourceSuggestionID == nil || *s.SourceSuggestionID != "suggestion_1" {
		t.Fatalf("expected SourceSuggestionID suggestion_1, got %v", s.SourceSuggestionID)
	}
}

func TestServiceCreateSpaceFromAISuggestionRejectsForeignProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_b"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	_, err := svc.CreateSpaceFromAISuggestion(ctx, "company_a", "project_1", "Master Bathroom", "bathroom", "", "suggestion_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceCreateSpaceFromAISuggestionRejectsEmptyName(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	_, err := svc.CreateSpaceFromAISuggestion(ctx, "company_a", "project_1", "", "bathroom", "", "suggestion_1")
	if err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestServiceCreateSpaceFromAISuggestionRetryReturnsExistingSpace(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	first, err := svc.CreateSpaceFromAISuggestion(ctx, "company_a", "project_1", "Master Bathroom", "bathroom", "", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on first acceptance: %v", err)
	}

	second, err := svc.CreateSpaceFromAISuggestion(ctx, "company_a", "project_1", "Master Bathroom", "bathroom", "", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error on retried acceptance: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected retry to return the same Space, got first=%s second=%s", first.ID, second.ID)
	}
}

func TestServiceFindSpaceBySourceSuggestionIDTenantScoped(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakeSpaceRepo(), projectLookup)
	ctx := context.Background()

	created, err := svc.CreateSpaceFromAISuggestion(ctx, "company_a", "project_1", "Kitchen", "kitchen", "", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := svc.FindSpaceBySourceSuggestionID(ctx, "company_a", "suggestion_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected found space id %s, got %s", created.ID, found.ID)
	}

	_, err = svc.FindSpaceBySourceSuggestionID(ctx, "company_b", "suggestion_1")
	if err != ErrSpaceNotFound {
		t.Fatalf("expected ErrSpaceNotFound for cross-tenant lookup, got %v", err)
	}
}
