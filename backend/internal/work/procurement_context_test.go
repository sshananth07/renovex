package work

import (
	"context"
	"testing"
)

// WorkItemProcurementContext confirms lineage AND exposes cancellation in one
// call, so material-requirement generation can skip cancelled scope rather than
// creating procurement demand for work that will never happen (M7 design spec
// §1.2, §3.7).

// newProcurementContextService wires a service whose projects project_1 and
// project_2 both belong to company_a, matching the existing fakes' explicit
// ownership-map style.
func newProcurementContextService(t *testing.T) (*Service, *fakeWorkItemRepo) {
	t.Helper()
	repo := newFakeWorkItemRepo()
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	projectLookup.belongsTo["project_2"] = "company_a"
	svc := NewService(repo, projectLookup, newFakeSpaceLookup())
	return svc, repo
}

// A planned WorkItem in the right project: found, not cancelled.
func TestWorkItemProcurementContextPlannedItem(t *testing.T) {
	svc, _ := newProcurementContextService(t)
	ctx := context.Background()

	w, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Tile bathroom", "tiling", "33.5", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancelled, found, err := svc.WorkItemProcurementContext(ctx, "company_a", w.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true for a work item in this company and project")
	}
	if cancelled {
		t.Error("cancelled = true, want false for a planned work item")
	}
}

// A cancelled WorkItem is still FOUND — the caller needs to distinguish
// "cancelled scope" from "no such work item", because they are different skip
// reasons (design spec §3.7).
func TestWorkItemProcurementContextCancelledItemIsFoundAndFlagged(t *testing.T) {
	svc, _ := newProcurementContextService(t)
	ctx := context.Background()

	w, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Tile bathroom", "tiling", "33.5", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.UpdateWorkItemStatus(ctx, "company_a", w.ID, WorkItemStatusCancelled); err != nil {
		t.Fatalf("unexpected error cancelling: %v", err)
	}

	cancelled, found, err := svc.WorkItemProcurementContext(ctx, "company_a", w.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false: a cancelled work item still exists and must be reported as found")
	}
	if !cancelled {
		t.Error("cancelled = false, want true")
	}
}

// An unknown ID is not found, and reports no error — the caller decides whether
// that is a skip or a 404.
func TestWorkItemProcurementContextUnknownID(t *testing.T) {
	svc, _ := newProcurementContextService(t)

	cancelled, found, err := svc.WorkItemProcurementContext(context.Background(), "company_a", "nope", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("found = true, want false for an unknown work item")
	}
	if cancelled {
		t.Error("cancelled must be false when the work item is not found")
	}
}

// A WorkItem belonging to another company is not found, even with a valid ID —
// the tenant boundary is enforced inside the capability.
func TestWorkItemProcurementContextIsTenantScoped(t *testing.T) {
	svc, _ := newProcurementContextService(t)
	ctx := context.Background()

	w, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Tile bathroom", "tiling", "33.5", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, found, err := svc.WorkItemProcurementContext(ctx, "company_b", w.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("found = true: company_b must not see company_a's work item")
	}
}

// A WorkItem in the right company but a DIFFERENT project is not found. This is
// the lineage check: generation must never attach one project's requirement to
// another project's work item.
func TestWorkItemProcurementContextEnforcesProjectLineage(t *testing.T) {
	svc, _ := newProcurementContextService(t)
	ctx := context.Background()

	w, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Tile bathroom", "tiling", "33.5", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, found, err := svc.WorkItemProcurementContext(ctx, "company_a", w.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("found = true: a work item from project_1 must not be found under project_2")
	}
}
