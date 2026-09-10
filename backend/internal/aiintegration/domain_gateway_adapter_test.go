package aiintegration

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/ai"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

func TestDomainGatewayAdapterTranslatesProjectContext(t *testing.T) {
	inner := newTestAdapter()
	gw := NewDomainGatewayAdapter(inner)

	ctx, err := gw.GetProjectAIContext(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.ID != "project_1" || ctx.ScopeBrief != "Full renovation." {
		t.Fatalf("unexpected translated context: %+v", ctx)
	}
}

func TestDomainGatewayAdapterTranslatesSpacesWorkItemsMaterialsRequirements(t *testing.T) {
	inner := newTestAdapter()
	gw := NewDomainGatewayAdapter(inner)
	ctx := context.Background()

	spaces, err := gw.ListSpacesForAI(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) != 1 || spaces[0].Name != "Kitchen" {
		t.Fatalf("unexpected spaces: %+v", spaces)
	}

	workItems, err := gw.ListWorkItemsForAI(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workItems) != 1 || workItems[0].Description != "Demo cabinets" {
		t.Fatalf("unexpected work items: %+v", workItems)
	}

	materials, err := gw.ListMaterialCandidatesForAI(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(materials) != 1 || materials[0].Name != "Tile Adhesive" {
		t.Fatalf("unexpected materials: %+v", materials)
	}

	requirements, err := gw.ListResourceRequirementsForAI(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requirements == nil {
		t.Fatal("expected non-nil empty slice or populated slice")
	}
}

// TestDomainGatewayAdapterTranslatesWorkItemsProjectNotFound guards against a
// real bug found via the M8.5B-A E2E/cross-tenant matrix (Task 18 Step 2):
// ListWorkItemsForAI propagated the raw work.ErrProjectNotFound straight
// through, uncaught by ai.mapHandlerError (which only recognizes
// ai.ErrGatewayProjectNotFound), so a cross-tenant Resource-generation
// attempt fell through to the generic 503 "AI suggestions are temporarily
// unavailable" instead of a 404 — correct data isolation, wrong HTTP
// contract, and misleading to any client/monitoring that a Company B
// tenant-boundary rejection looked identical to a real Gemini/Python outage.
func TestDomainGatewayAdapterTranslatesWorkItemsProjectNotFound(t *testing.T) {
	inner := NewAdapter(
		&fakeProjectsService{byID: map[string]projects.Project{}},
		&fakeSpacesService{byProject: map[string][]spaces.Space{}},
		foreignProjectWorkService{},
		&fakeMaterialsService{byCompany: map[string][]materials.Material{}},
		&fakeWorkResourcesService{byProject: map[string][]workresources.WorkResourceRequirement{}},
	)
	gw := NewDomainGatewayAdapter(inner)

	_, err := gw.ListWorkItemsForAI(context.Background(), "company_b", "project_owned_by_company_a")
	if !errors.Is(err, ai.ErrGatewayProjectNotFound) {
		t.Fatalf("expected ai.ErrGatewayProjectNotFound, got %v", err)
	}
}
