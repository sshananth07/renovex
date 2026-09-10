package demoseed_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

type fakeClientCreator struct {
	byName      map[string]clients.Client
	createCalls int
}

func (f *fakeClientCreator) CreateClient(ctx context.Context, companyID, name, phone, email, address, billingAddress, notes string) (clients.Client, error) {
	f.createCalls++
	c := clients.Client{ID: name, CompanyID: companyID, Name: name}
	f.byName[name] = c
	return c, nil
}
func (f *fakeClientCreator) ListClientsPaginated(ctx context.Context, companyID string, req pagination.Request) ([]clients.Client, int, error) {
	list := make([]clients.Client, 0, len(f.byName))
	for _, c := range f.byName {
		list = append(list, c)
	}
	return list, len(list), nil
}

// fakeProjectCreator is keyed by ID (matching real Service semantics),
// never by Name.
type fakeProjectCreator struct {
	byID        map[string]projects.Project
	createCalls int
}

func (f *fakeProjectCreator) CreateProject(ctx context.Context, companyID, clientID, name string) (projects.Project, error) {
	f.createCalls++
	p := projects.Project{ID: name, CompanyID: companyID, ClientID: clientID, Name: name, Status: projects.ProjectStatusLead}
	f.byID[p.ID] = p
	return p, nil
}
func (f *fakeProjectCreator) ListProjectsPaginated(ctx context.Context, companyID, clientID string, req pagination.Request) ([]projects.Project, int, error) {
	list := make([]projects.Project, 0, len(f.byID))
	for _, p := range f.byID {
		list = append(list, p)
	}
	return list, len(list), nil
}
func (f *fakeProjectCreator) UpdateProjectStatus(ctx context.Context, companyID, projectID string, status projects.ProjectStatus) (projects.Project, error) {
	p := f.byID[projectID]
	p.Status = status
	f.byID[projectID] = p
	return p, nil
}

type fakePropertyCreator struct {
	byProject   map[string][]properties.Property
	createCalls int
}

func (f *fakePropertyCreator) CreateProperty(ctx context.Context, companyID, projectID, address, propertyType, notes string) (properties.Property, error) {
	f.createCalls++
	p := properties.Property{ID: "prop-" + projectID, CompanyID: companyID, ProjectID: projectID, Address: address}
	f.byProject[projectID] = append(f.byProject[projectID], p)
	return p, nil
}
func (f *fakePropertyCreator) ListPropertiesByProject(ctx context.Context, companyID, projectID string) ([]properties.Property, error) {
	return f.byProject[projectID], nil
}

type fakeSpaceCreator struct {
	byProject   map[string][]spaces.Space
	createCalls int
}

func (f *fakeSpaceCreator) CreateSpace(ctx context.Context, companyID, projectID, name, spaceType, description string) (spaces.Space, error) {
	f.createCalls++
	s := spaces.Space{ID: projectID + ":" + name, CompanyID: companyID, ProjectID: projectID, Name: name}
	f.byProject[projectID] = append(f.byProject[projectID], s)
	return s, nil
}
func (f *fakeSpaceCreator) ListSpacesPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]spaces.Space, int, error) {
	list := f.byProject[projectID]
	return list, len(list), nil
}

type fakeWorkItemCreator struct {
	byProject   map[string][]work.WorkItem
	createCalls int
}

func (f *fakeWorkItemCreator) CreateWorkItem(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit string) (work.WorkItem, error) {
	f.createCalls++
	w := work.WorkItem{ID: projectID + ":" + description, CompanyID: companyID, ProjectID: projectID, SpaceID: spaceID, Description: description}
	f.byProject[projectID] = append(f.byProject[projectID], w)
	return w, nil
}
func (f *fakeWorkItemCreator) ListWorkItemsPaginatedByProject(ctx context.Context, companyID, projectID string, req pagination.Request) ([]work.WorkItem, int, error) {
	list := f.byProject[projectID]
	return list, len(list), nil
}

func TestSeedProject1_CreatesFullHierarchy(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}

	projectID, err := demoseed.SeedProject1(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake, "company-1")
	if err != nil {
		t.Fatalf("SeedProject1: %v", err)
	}
	if projectID == "" {
		t.Fatalf("expected a non-empty projectID")
	}
	if propertiesFake.createCalls != 1 {
		t.Fatalf("expected exactly 1 Property, got %d creates", propertiesFake.createCalls)
	}
	if len(spacesFake.byProject[projectID]) < 3 {
		t.Fatalf("expected multiple Spaces, got %d", len(spacesFake.byProject[projectID]))
	}
	workItems := workFake.byProject[projectID]
	if len(workItems) < 3 {
		t.Fatalf("expected multiple Work Items, got %d", len(workItems))
	}
	hasProjectWide, hasSpaceScoped := false, false
	for _, w := range workItems {
		if w.SpaceID == nil {
			hasProjectWide = true
		} else {
			hasSpaceScoped = true
		}
	}
	if !hasProjectWide {
		t.Fatalf("expected at least one project-wide Work Item (nil SpaceID)")
	}
	if !hasSpaceScoped {
		t.Fatalf("expected at least one space-scoped Work Item")
	}
}

func TestSeedProject1_Idempotent_SecondRunCreatesNothingNew(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	spacesFake := &fakeSpaceCreator{byProject: map[string][]spaces.Space{}}
	workFake := &fakeWorkItemCreator{byProject: map[string][]work.WorkItem{}}

	firstProjectID, err := demoseed.SeedProject1(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake, "company-1")
	if err != nil {
		t.Fatalf("first SeedProject1: %v", err)
	}
	firstClientCreates, firstProjectCreates := clientsFake.createCalls, projectsFake.createCalls
	firstPropertyCreates, firstSpaceCreates, firstWorkCreates := propertiesFake.createCalls, spacesFake.createCalls, workFake.createCalls

	secondProjectID, err := demoseed.SeedProject1(ctx, clientsFake, projectsFake, propertiesFake, spacesFake, workFake, "company-1")
	if err != nil {
		t.Fatalf("second SeedProject1: %v", err)
	}
	if secondProjectID != firstProjectID {
		t.Fatalf("expected the same projectID on rerun, got %q then %q", firstProjectID, secondProjectID)
	}
	if clientsFake.createCalls != firstClientCreates || projectsFake.createCalls != firstProjectCreates ||
		propertiesFake.createCalls != firstPropertyCreates || spacesFake.createCalls != firstSpaceCreates ||
		workFake.createCalls != firstWorkCreates {
		t.Fatalf("expected zero new creates on rerun")
	}
}
