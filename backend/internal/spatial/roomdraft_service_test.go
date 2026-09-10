package spatial

import (
	"context"
	"errors"
	"testing"
)

// --- fake ---

type fakeRoomDraftRepo struct {
	byID      map[string]RoomDraft
	byCapture map[string]string // captureID -> roomDraftID
	nextID    int
}

func newFakeRoomDraftRepo() *fakeRoomDraftRepo {
	return &fakeRoomDraftRepo{byID: map[string]RoomDraft{}, byCapture: map[string]string{}}
}

func (f *fakeRoomDraftRepo) Create(_ context.Context, d RoomDraft) (RoomDraft, error) {
	f.nextID++
	d.ID = "draft_" + string(rune('a'+f.nextID))
	d.Revision = 0
	f.byID[d.ID] = d
	f.byCapture[d.CaptureID] = d.ID
	return d, nil
}

func (f *fakeRoomDraftRepo) FindByID(_ context.Context, companyID, id string) (RoomDraft, error) {
	d, ok := f.byID[id]
	if !ok || d.CompanyID != companyID {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	return d, nil
}

func (f *fakeRoomDraftRepo) FindByCaptureID(_ context.Context, companyID, captureID string) (RoomDraft, error) {
	id, ok := f.byCapture[captureID]
	if !ok {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	return f.FindByID(context.Background(), companyID, id)
}

func (f *fakeRoomDraftRepo) Update(_ context.Context, companyID, id string, d RoomDraft, expectedRevision int64) (RoomDraft, error) {
	existing, ok := f.byID[id]
	if !ok || existing.CompanyID != companyID {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	if existing.Revision != expectedRevision {
		return RoomDraft{}, ErrRoomDraftRevisionMismatch
	}
	d.ID = id
	d.CompanyID = companyID
	d.Revision = existing.Revision + 1
	f.byID[id] = d
	return d, nil
}

func newTestRoomDraftService() (*Service, *fakeCaptureRepo, *fakeRoomDraftRepo) {
	captures := newFakeCaptureRepo()
	versions := newFakeRoomVersionRepo()
	states := newFakeSpaceStateRepo()
	lookup := newFakeSpaceLookup()
	drafts := newFakeRoomDraftRepo()
	svc := NewService(captures, versions, states, lookup)
	svc.SetRoomDraftSupport(drafts)
	return svc, captures, drafts
}

// --- ApplyEditOperation ---

func TestApplyEditOperation_AppliesValidOperation(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
	})

	op := SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated}
	updated, err := svc.ApplyEditOperation(ctx, "company_a", draft.ID, op, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Walls[0].Thickness == nil || *updated.Walls[0].Thickness != 0.2 {
		t.Fatalf("expected thickness applied, got %+v", updated.Walls[0].Thickness)
	}
	if updated.Revision != draft.Revision+1 {
		t.Fatalf("expected revision incremented, got %d", updated.Revision)
	}
}

func TestApplyEditOperation_RejectsInvalidOperationBeforeTouchingRepository(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "capture_1"})

	op := SetWallThicknessOperation{WallID: "wall_1", Thickness: -1, Status: MeasurementStatusEstimated}
	_, err := svc.ApplyEditOperation(ctx, "company_a", draft.ID, op, draft.Revision)
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestApplyEditOperation_RejectsTargetNotFound(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "capture_1"})

	op := SetWallThicknessOperation{WallID: "nonexistent", Thickness: 0.2, Status: MeasurementStatusEstimated}
	_, err := svc.ApplyEditOperation(ctx, "company_a", draft.ID, op, draft.Revision)
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestApplyEditOperation_RejectsStaleRevision(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
	})

	op := SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated}
	staleRevision := draft.Revision + 99
	_, err := svc.ApplyEditOperation(ctx, "company_a", draft.ID, op, staleRevision)
	if !errors.Is(err, ErrRoomDraftRevisionMismatch) {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch, got %v", err)
	}
}

// --- ResetRoomDraftToBaseline ---

func TestResetRoomDraftToBaseline_RestoresBaselineAndClearsContractorElements(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()

	baselineWall := RoomDraftWall{ID: "wall_original", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []RoomDraftWall{baselineWall},
		OriginalBaseline: &RoomDraftBaseline{
			Walls: []RoomDraftWall{baselineWall},
		},
	})

	// Simulate contractor edits: move the wall and add a contractor-created
	// fixture/service-point/constraint.
	edited := draft
	edited.Walls[0].Start = RoomLocalPoint{X: 99}
	edited.Fixtures = []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}}
	edited.ServicePoints = []RoomDraftServicePoint{{ID: "sp_1", Kind: ServicePointKindDrain, CreatedBy: ElementOriginContractor}}
	edited.Constraints = []RoomDraftConstraint{{ID: "c_1", Kind: ConstraintKindColumn, CreatedBy: ElementOriginContractor}}
	updated, err := drafts.Update(ctx, "company_a", draft.ID, edited, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected error setting up edited state: %v", err)
	}

	reset, err := svc.ResetRoomDraftToBaseline(ctx, "company_a", draft.ID, updated.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reset.Walls) != 1 || reset.Walls[0].ID != "wall_original" || reset.Walls[0].Start != (RoomLocalPoint{}) {
		t.Fatalf("expected wall restored to baseline position with stable ID, got %+v", reset.Walls)
	}
	if len(reset.Fixtures) != 0 {
		t.Fatalf("expected contractor-created fixtures cleared, got %+v", reset.Fixtures)
	}
	if len(reset.ServicePoints) != 0 {
		t.Fatalf("expected contractor-created service points cleared, got %+v", reset.ServicePoints)
	}
	if len(reset.Constraints) != 0 {
		t.Fatalf("expected contractor-created constraints cleared, got %+v", reset.Constraints)
	}
}

func TestResetRoomDraftToBaseline_ClearsObjectVisualAssetBinding(t *testing.T) {
	// RP4D regression: OriginalBaseline is captured at PersistRoomDraft
	// time, before any assign_visual_asset could ever run, so restoring
	// objects from the baseline snapshot naturally clears any binding —
	// no special-casing needed. This test proves that behavior explicitly
	// rather than by inspection alone.
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()

	baselineObject := RoomDraftObject{ID: "object_1", Category: "sofa"}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Objects: []RoomDraftObject{baselineObject},
		OriginalBaseline: &RoomDraftBaseline{
			Objects: []RoomDraftObject{baselineObject},
		},
	})

	// Simulate a contractor binding a visual asset to the object.
	bound := draft
	bound.Objects[0].VisualAsset = &VisualAssetRef{AssetID: "sofa-asset", Version: 1}
	updated, err := drafts.Update(ctx, "company_a", draft.ID, bound, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected error setting up bound state: %v", err)
	}
	if updated.Objects[0].VisualAsset == nil {
		t.Fatalf("test setup failed: expected object bound before reset")
	}

	reset, err := svc.ResetRoomDraftToBaseline(ctx, "company_a", draft.ID, updated.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reset.Objects) != 1 || reset.Objects[0].VisualAsset != nil {
		t.Fatalf("expected object's VisualAsset binding cleared by baseline restore, got %+v", reset.Objects)
	}
}

func TestResetRoomDraftToBaseline_RejectsWhenNoBaselineExists(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "capture_1", OriginalBaseline: nil})

	_, err := svc.ResetRoomDraftToBaseline(ctx, "company_a", draft.ID, draft.Revision)
	if !errors.Is(err, ErrRoomDraftHasNoBaseline) {
		t.Fatalf("expected ErrRoomDraftHasNoBaseline, got %v", err)
	}
}

func TestResetRoomDraftToBaseline_RejectsStaleRevision(t *testing.T) {
	svc, _, drafts := newTestRoomDraftService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		OriginalBaseline: &RoomDraftBaseline{},
	})

	_, err := svc.ResetRoomDraftToBaseline(ctx, "company_a", draft.ID, draft.Revision+99)
	if !errors.Is(err, ErrRoomDraftRevisionMismatch) {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch, got %v", err)
	}
}
