package demoseed_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
)

// fakeScopeBriefSetter also writes ScopeBrief back onto the shared
// fakeProjectCreator's stored Project, mirroring the real coupling: the
// real *projects.Service.UpdateProjectScopeBrief writes to the SAME
// Project document that GetProject/ListProjectsPaginated read from, which
// is exactly what SeedProject6's idempotency check (project.ScopeBrief ==
// "") depends on to detect "already set" on a rerun.
type fakeScopeBriefSetter struct {
	byProject   map[string]string
	updateCalls int
	projects    *fakeProjectCreator
}

func (f *fakeScopeBriefSetter) UpdateProjectScopeBrief(ctx context.Context, companyID, projectID, scopeBrief string) (projects.Project, error) {
	f.updateCalls++
	f.byProject[projectID] = scopeBrief
	if f.projects != nil {
		p := f.projects.byID[projectID]
		p.ScopeBrief = scopeBrief
		f.projects.byID[projectID] = p
	}
	return projects.Project{ID: projectID, CompanyID: companyID, ScopeBrief: scopeBrief}, nil
}

func TestSeedProject6_SetsScopeBrief(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	scopeFake := &fakeScopeBriefSetter{byProject: map[string]string{}, projects: projectsFake}

	projectID, err := demoseed.SeedProject6(ctx, clientsFake, projectsFake, scopeFake, propertiesFake, "company-1")
	if err != nil {
		t.Fatalf("SeedProject6: %v", err)
	}
	if scopeFake.updateCalls != 1 {
		t.Fatalf("expected exactly 1 UpdateProjectScopeBrief call, got %d", scopeFake.updateCalls)
	}
	brief, ok := scopeFake.byProject[projectID]
	if !ok || brief == "" {
		t.Fatalf("expected a non-empty scope brief for project %q", projectID)
	}
}

func TestSeedProject6_Idempotent_SecondRunSkipsScopeUpdate(t *testing.T) {
	ctx := context.Background()
	clientsFake := &fakeClientCreator{byName: map[string]clients.Client{}}
	projectsFake := &fakeProjectCreator{byID: map[string]projects.Project{}}
	propertiesFake := &fakePropertyCreator{byProject: map[string][]properties.Property{}}
	scopeFake := &fakeScopeBriefSetter{byProject: map[string]string{}, projects: projectsFake}

	_, err := demoseed.SeedProject6(ctx, clientsFake, projectsFake, scopeFake, propertiesFake, "company-1")
	if err != nil {
		t.Fatalf("first SeedProject6: %v", err)
	}
	firstUpdateCalls := scopeFake.updateCalls

	_, err = demoseed.SeedProject6(ctx, clientsFake, projectsFake, scopeFake, propertiesFake, "company-1")
	if err != nil {
		t.Fatalf("second SeedProject6: %v", err)
	}
	if scopeFake.updateCalls != firstUpdateCalls {
		t.Fatalf("expected no new UpdateProjectScopeBrief call on rerun (already set): %d -> %d", firstUpdateCalls, scopeFake.updateCalls)
	}
}
