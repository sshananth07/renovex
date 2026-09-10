package spatial

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// GET /spatial/room-drafts/{id} (plan §RP4C1) is a pure additive handler
// registration over the already-existing, already-tenant-scoped
// Service.GetRoomDraft — these tests exercise that service method directly,
// matching this package's established service-level testing convention
// (there is no full HTTP-mount test harness anywhere in this codebase; every
// module tests either at the service layer or via a direct error-mapping
// unit test, per awards/handler_test.go's statusOf pattern).

func TestGetRoomDraft_ZeroEditDraft_Loads(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
	})

	got, err := svc.GetRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error loading a never-edited draft: %v", err)
	}
	if got.Revision != 0 {
		t.Fatalf("expected baseline revision 0, got %d", got.Revision)
	}
	if len(got.Walls) != 1 || got.Walls[0].ID != "wall_1" {
		t.Fatalf("expected the seeded geometry back unchanged, got %+v", got.Walls)
	}
}

func TestGetRoomDraft_AfterEdit_ReturnsCurrentRevisionAndGeometry(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
	})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error submitting the edit: %v", err)
	}

	got, err := svc.GetRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error re-loading after edit: %v", err)
	}
	if got.Revision != result.RoomDraft.Revision {
		t.Fatalf("GET revision %d does not match the edit response's revision %d", got.Revision, result.RoomDraft.Revision)
	}
	if got.Walls[0].Thickness == nil || *got.Walls[0].Thickness != 0.2 {
		t.Fatalf("expected GET to reflect the applied thickness, got %+v", got.Walls[0].Thickness)
	}
}

func TestGetRoomDraft_CrossCompany_RejectsAsNotFound(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "capture_1"})

	_, err := svc.GetRoomDraft(ctx, "company_b", draft.ID)
	if !errors.Is(err, ErrRoomDraftNotFound) {
		t.Fatalf("expected ErrRoomDraftNotFound for cross-company access, got %v", err)
	}
	// Confirms the handler's tenant-hiding semantics are identical to every
	// other spatial route: mapSpatialError turns this into 404, never 403.
	var statusErr huma.StatusError
	if !errors.As(mapSpatialError(err), &statusErr) {
		t.Fatalf("expected mapSpatialError to return a huma.StatusError")
	}
	if statusErr.GetStatus() != http.StatusNotFound {
		t.Fatalf("expected cross-company access to map to 404, not 403, got %d", statusErr.GetStatus())
	}
}

func TestGetRoomDraft_AfterResetToScan_ReturnsResetStateAndNewRevision(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	// Two independently-allocated slices/structs, not the same backing
	// array — SetWallThicknessOperation.Apply mutates draft.Walls[idx] in
	// place, so sharing one slice between Walls and OriginalBaseline.Walls
	// would make the "baseline" alias the edit and defeat this test.
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls:            []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
		OriginalBaseline: &RoomDraftBaseline{Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}}},
	})

	// Mutate away from baseline first, so reset has something to undo.
	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	edited, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error submitting the pre-reset edit: %v", err)
	}

	resetResult, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "op_reset", edited.RoomDraft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error resetting to scan: %v", err)
	}

	got, err := svc.GetRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error re-loading after reset: %v", err)
	}
	if got.Revision != resetResult.RoomDraft.Revision {
		t.Fatalf("GET revision %d does not match reset response's revision %d", got.Revision, resetResult.RoomDraft.Revision)
	}
	if got.Walls[0].Thickness != nil {
		t.Fatalf("expected reset to restore the baseline (no thickness), got %+v", got.Walls[0].Thickness)
	}
}

// --- canResetToScan (plan §RP4C2 backend addendum #2) ---
// CanResetToScan is a pure derived wire field (toRoomDraftDTO), not part of
// the domain RoomDraft type, so these tests exercise toRoomDraftDTO
// directly rather than svc.GetRoomDraft's domain-level return value.
//
// OriginalBaseline is set unconditionally by PersistRoomDraft at draft
// creation time (service.go's PersistRoomDraft, lines ~186-193), NOT
// lazily on first edit — a real, production-created RoomDraft therefore
// always has canResetToScan=true from the moment it exists. The
// "false" case below (OriginalBaseline nil) is reachable only via this
// package's own test helper constructing a RoomDraft directly, bypassing
// PersistRoomDraft — included to prove the flag correctly reflects
// OriginalBaseline's actual nil-ness rather than being hardcoded true.

func TestToRoomDraftDTO_CanResetToScan_FalseWhenOriginalBaselineIsNil(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "capture_1"})

	got, err := svc.GetRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dto := toRoomDraftDTO(got)
	if dto.CanResetToScan {
		t.Fatalf("expected canResetToScan=false when OriginalBaseline is nil")
	}
}

func TestToRoomDraftDTO_CanResetToScan_TrueAsSoonAsOriginalBaselineExists(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	// OriginalBaseline set at creation time, matching exactly what
	// PersistRoomDraft (service.go) does in production — it is set
	// unconditionally when a RoomDraft is first persisted from a capture,
	// not lazily on first edit.
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls:            []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
		OriginalBaseline: &RoomDraftBaseline{Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}}},
	})

	got, err := svc.GetRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dto := toRoomDraftDTO(got)
	if !dto.CanResetToScan {
		t.Fatalf("expected canResetToScan=true as soon as OriginalBaseline is set, before any edit")
	}
}

func TestToRoomDraftDTO_CanResetToScan_TrueAfterReset(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	baselineWalls := []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls:            []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
		OriginalBaseline: &RoomDraftBaseline{Walls: baselineWalls},
	})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	edited, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error submitting the pre-reset edit: %v", err)
	}

	resetResult, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "op_reset", edited.RoomDraft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error resetting to scan: %v", err)
	}

	dto := toRoomDraftDTO(resetResult.RoomDraft)
	if !dto.CanResetToScan {
		t.Fatalf("expected canResetToScan=true after reset — OriginalBaseline is preserved unchanged by reset, not cleared")
	}
}

// --- 409 conflict discriminator (plan §RP4C1 backend addendum #2) ---

func TestMapSpatialError_StaleRevisionAndOperationIDConflict_CarryDistinctCodes(t *testing.T) {
	staleErr := mapSpatialError(ErrRoomDraftRevisionMismatch)
	opConflictErr := mapSpatialError(ErrOperationIDConflict)

	var staleModel, opConflictModel *huma.ErrorModel
	if !errors.As(staleErr, &staleModel) {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch to map to a *huma.ErrorModel, got %T", staleErr)
	}
	if !errors.As(opConflictErr, &opConflictModel) {
		t.Fatalf("expected ErrOperationIDConflict to map to a *huma.ErrorModel, got %T", opConflictErr)
	}

	if staleModel.Status != http.StatusConflict || opConflictModel.Status != http.StatusConflict {
		t.Fatalf("expected both to remain 409, got %d and %d", staleModel.Status, opConflictModel.Status)
	}
	if staleModel.Type != "stale_revision" {
		t.Fatalf("expected stale-revision code %q, got %q", "stale_revision", staleModel.Type)
	}
	if opConflictModel.Type != "operation_id_conflict" {
		t.Fatalf("expected operation-id-conflict code %q, got %q", "operation_id_conflict", opConflictModel.Type)
	}
	if staleModel.Type == opConflictModel.Type {
		t.Fatalf("stale_revision and operation_id_conflict must carry distinct codes")
	}

	// Confirms the code survives JSON round-tripping as the wire's `type`
	// field, exactly what the Web layer's ApiError normalization will read.
	wire, err := json.Marshal(staleModel)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var decoded struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if decoded.Type != "stale_revision" {
		t.Fatalf("expected wire type %q, got %q", "stale_revision", decoded.Type)
	}
}
