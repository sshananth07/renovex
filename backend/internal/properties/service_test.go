package properties

import (
	"context"
	"testing"
)

type fakePropertyRepo struct {
	byID      map[string]Property
	byProject map[string]bool // "companyID|projectID" -> exists, simulating the unique index
	nextID    int
}

func newFakePropertyRepo() *fakePropertyRepo {
	return &fakePropertyRepo{byID: map[string]Property{}, byProject: map[string]bool{}}
}

func (f *fakePropertyRepo) Create(_ context.Context, p Property) (Property, error) {
	key := p.CompanyID + "|" + p.ProjectID
	if f.byProject[key] {
		return Property{}, ErrProjectAlreadyHasProperty
	}
	f.nextID++
	p.ID = string(rune('a' + f.nextID))
	f.byID[p.ID] = p
	f.byProject[key] = true
	return p, nil
}

func (f *fakePropertyRepo) FindByID(_ context.Context, companyID, id string) (Property, error) {
	p, ok := f.byID[id]
	if !ok || p.CompanyID != companyID {
		return Property{}, ErrPropertyNotFound
	}
	return p, nil
}

func (f *fakePropertyRepo) ListByProject(_ context.Context, companyID, projectID string) ([]Property, error) {
	var result []Property
	for _, p := range f.byID {
		if p.CompanyID == companyID && p.ProjectID == projectID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (f *fakePropertyRepo) Update(ctx context.Context, companyID, id string, fn func(*Property)) (Property, error) {
	p, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Property{}, err
	}
	fn(&p)
	f.byID[id] = p
	return p, nil
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

func TestServiceCreatePropertyValidatesProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakePropertyRepo(), projectLookup)

	p, err := svc.CreateProperty(context.Background(), "company_a", "project_1", "123 Street", "residential", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ProjectID != "project_1" {
		t.Fatalf("expected project_1, got %s", p.ProjectID)
	}
}

func TestServiceCreatePropertyRejectsForeignProject(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_b" // belongs to a different company
	svc := NewService(newFakePropertyRepo(), projectLookup)

	_, err := svc.CreateProperty(context.Background(), "company_a", "project_1", "123 Street", "residential", "")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceCreatePropertyRejectsEmptyAddress(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakePropertyRepo(), projectLookup)

	_, err := svc.CreateProperty(context.Background(), "company_a", "project_1", "", "residential", "")
	if err != ErrAddressRequired {
		t.Fatalf("expected ErrAddressRequired, got %v", err)
	}
}

func TestServiceCreatePropertySecondForSameProjectRejected(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakePropertyRepo(), projectLookup)
	ctx := context.Background()

	_, err := svc.CreateProperty(ctx, "company_a", "project_1", "First", "residential", "")
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}

	_, err = svc.CreateProperty(ctx, "company_a", "project_1", "Second", "residential", "")
	if err != ErrProjectAlreadyHasProperty {
		t.Fatalf("expected ErrProjectAlreadyHasProperty, got %v", err)
	}
}

func TestServiceListPropertiesByProjectValidatesProjectFirst(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakePropertyRepo(), projectLookup)
	ctx := context.Background()

	_, err := svc.CreateProperty(ctx, "company_a", "project_1", "123 Street", "residential", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.ListPropertiesByProject(ctx, "company_b", "project_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for foreign project, got %v", err)
	}

	list, err := svc.ListPropertiesByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 property, got %d", len(list))
	}
}

func TestServiceUpdateProperty(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	svc := NewService(newFakePropertyRepo(), projectLookup)
	ctx := context.Background()

	created, err := svc.CreateProperty(ctx, "company_a", "project_1", "Old Address", "residential", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateProperty(ctx, "company_a", created.ID, "New Address", "commercial", "notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Address != "New Address" {
		t.Fatalf("expected New Address, got %s", updated.Address)
	}
}
