package spatial_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func newTestSession(companyID, clientSessionID, roomDraftID string, revision int64) spatial.SpatialDesignSession {
	now := time.Now()
	return spatial.SpatialDesignSession{
		CompanyID: companyID, ProjectID: "project_1", SpaceID: "space_1",
		RoomDraftID: roomDraftID, CreatedByUserID: "user_1",
		ClientSessionID: clientSessionID, SessionRequestFingerprint: "fp_" + clientSessionID,
		Target:                   spatial.SpatialDesignTarget{Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123"},
		BasedOnRoomDraftRevision: revision,
		Status:                   spatial.SpatialDesignSessionStatusActive,
		CreatedAt:                now, UpdatedAt: now, SchemaVersion: 1,
	}
}

func TestMongoDesignSessionRepository_CreateOrGetSession_ReplaysSameRequest(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_1", "roomdraft_1", 17)
	first, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	replay := newTestSession("company_1", "client_session_1", "roomdraft_1", 17)
	second, err := repo.CreateOrGetSession(context.Background(), replay)
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected replay to return the SAME session, got %s vs %s", first.ID, second.ID)
	}
}

func TestMongoDesignSessionRepository_CreateOrGetSession_ConflictOnChangedContent(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_2", "roomdraft_1", 17)
	if _, err := repo.CreateOrGetSession(context.Background(), session); err != nil {
		t.Fatalf("first create: %v", err)
	}

	changed := newTestSession("company_1", "client_session_2", "roomdraft_1", 17)
	changed.SessionRequestFingerprint = "different_fingerprint"
	_, err := repo.CreateOrGetSession(context.Background(), changed)
	if !errors.Is(err, spatial.ErrDesignSessionRequestConflict) {
		t.Fatalf("expected ErrDesignSessionRequestConflict, got %v", err)
	}
}

func TestMongoDesignSessionRepository_FindSession_TenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_3", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := repo.FindSession(context.Background(), "company_1", created.ID); err != nil {
		t.Fatalf("expected to find own session: %v", err)
	}
	if _, err := repo.FindSession(context.Background(), "company_2", created.ID); !errors.Is(err, spatial.ErrDesignSessionNotFound) {
		t.Fatalf("expected ErrDesignSessionNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestMongoDesignSessionRepository_ReserveTurn_ReplayAndConflict(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_4", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	turn := spatial.SpatialDesignTurn{
		CompanyID: "company_1", SessionID: created.ID,
		ClientRequestID: "client_request_1", RequestFingerprint: "turn_fp_1",
		BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
		Instruction: "Actually make it beige.", Status: spatial.SpatialDesignTurnStatusReserved,
		StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	firstTurn, wasReplay, err := repo.ReserveTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if wasReplay {
		t.Fatal("first reservation must not be a replay")
	}

	replayTurn := turn
	secondTurn, wasReplay, err := repo.ReserveTurn(context.Background(), replayTurn)
	if err != nil {
		t.Fatalf("replay reserve: %v", err)
	}
	if !wasReplay {
		t.Fatal("expected the second identical request to be recognized as a replay")
	}
	if firstTurn.ID != secondTurn.ID {
		t.Fatalf("expected replay to resolve to the SAME turn, got %s vs %s", firstTurn.ID, secondTurn.ID)
	}

	conflictingTurn := turn
	conflictingTurn.RequestFingerprint = "different_fingerprint"
	_, _, err = repo.ReserveTurn(context.Background(), conflictingTurn)
	if !errors.Is(err, spatial.ErrDesignTurnRequestConflict) {
		t.Fatalf("expected ErrDesignTurnRequestConflict, got %v", err)
	}
}

func TestMongoDesignSessionRepository_ReserveTurn_ConcurrentDistinctTurnsOnlyOneWins(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_5", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	first := spatial.SpatialDesignTurn{
		CompanyID: "company_1", SessionID: created.ID,
		ClientRequestID: "request_a", RequestFingerprint: "fp_a",
		BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
		Instruction: "Make it beige.", Status: spatial.SpatialDesignTurnStatusReserved,
		StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	if _, _, err := repo.ReserveTurn(context.Background(), first); err != nil {
		t.Fatalf("first distinct reserve: %v", err)
	}

	second := first
	second.ClientRequestID = "request_b"
	second.RequestFingerprint = "fp_b"
	_, _, err = repo.ReserveTurn(context.Background(), second)
	if !errors.Is(err, spatial.ErrDesignTurnInProgress) {
		t.Fatalf("expected ErrDesignTurnInProgress for a second distinct concurrent turn, got %v", err)
	}
}

func TestMongoDesignSessionRepository_MarkProviderStartedAndFinishTurn(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_6", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turn := spatial.SpatialDesignTurn{
		CompanyID: "company_1", SessionID: created.ID,
		ClientRequestID: "request_c", RequestFingerprint: "fp_c",
		BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
		Instruction: "Make it beige.", Status: spatial.SpatialDesignTurnStatusReserved,
		StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	reserved, _, err := repo.ReserveTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	if err := repo.MarkProviderStarted(context.Background(), "company_1", reserved.ID, time.Now()); err != nil {
		t.Fatalf("MarkProviderStarted: %v", err)
	}

	finished, err := repo.FinishTurn(context.Background(), spatial.FinishTurnInput{
		CompanyID: "company_1", SessionID: created.ID, TurnID: reserved.ID,
		Status: spatial.SpatialDesignTurnStatusProposed,
		WorkingDesign: spatial.WorkingDesign{
			Material: &spatial.WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
		},
	})
	if err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	if finished.Status != spatial.SpatialDesignTurnStatusProposed {
		t.Fatalf("expected proposed status, got %s", finished.Status)
	}

	updatedSession, err := repo.FindSession(context.Background(), "company_1", created.ID)
	if err != nil {
		t.Fatalf("FindSession after finish: %v", err)
	}
	if updatedSession.ActiveTurnID != "" {
		t.Fatalf("expected ActiveTurnID cleared after successful finish, got %q", updatedSession.ActiveTurnID)
	}
	if updatedSession.LatestReadyPlanTurnID != reserved.ID {
		t.Fatalf("expected LatestReadyPlanTurnID to be set to the finished turn, got %q", updatedSession.LatestReadyPlanTurnID)
	}
	if updatedSession.CurrentWorkingDesign.Material == nil || updatedSession.CurrentWorkingDesign.Material.BaseColor != "#c8a464" {
		t.Fatalf("expected session working design updated, got %+v", updatedSession.CurrentWorkingDesign)
	}
}

func TestMongoDesignSessionRepository_FailedTurnDoesNotUpdateWorkingDesign(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_7", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turn := spatial.SpatialDesignTurn{
		CompanyID: "company_1", SessionID: created.ID,
		ClientRequestID: "request_d", RequestFingerprint: "fp_d",
		BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
		Instruction: "Do something invalid.", Status: spatial.SpatialDesignTurnStatusReserved,
		StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	reserved, _, err := repo.ReserveTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	_, err = repo.FinishTurn(context.Background(), spatial.FinishTurnInput{
		CompanyID: "company_1", SessionID: created.ID, TurnID: reserved.ID,
		Status: spatial.SpatialDesignTurnStatusFailed, SafeFailureCode: "design_reasoning_provider_unavailable",
	})
	if err != nil {
		t.Fatalf("FinishTurn (failed): %v", err)
	}

	updatedSession, err := repo.FindSession(context.Background(), "company_1", created.ID)
	if err != nil {
		t.Fatalf("FindSession after failed finish: %v", err)
	}
	if updatedSession.ActiveTurnID != "" {
		t.Fatalf("expected ActiveTurnID cleared even after a failed turn, got %q", updatedSession.ActiveTurnID)
	}
	if updatedSession.LatestReadyPlanTurnID != "" {
		t.Fatalf("expected LatestReadyPlanTurnID untouched by a failed turn, got %q", updatedSession.LatestReadyPlanTurnID)
	}
	if updatedSession.CurrentWorkingDesign.Material != nil {
		t.Fatalf("expected working design untouched by a failed turn, got %+v", updatedSession.CurrentWorkingDesign)
	}
}

func TestMongoDesignSessionRepository_RecoverInterruptedTurns(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_8", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	staleStart := time.Now().Add(-1 * time.Hour)
	turn := spatial.SpatialDesignTurn{
		CompanyID: "company_1", SessionID: created.ID,
		ClientRequestID: "request_e", RequestFingerprint: "fp_e",
		BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
		Instruction: "x", Status: spatial.SpatialDesignTurnStatusReserved,
		StartedAt: staleStart, CreatedAt: staleStart, SchemaVersion: 1,
	}
	reserved, _, err := repo.ReserveTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// Simulate the provider call having started (a real service would call
	// MarkProviderStarted right before dispatching to GLM) and leave the
	// turn "in flight" — as if the process crashed before FinishTurn.
	if err := repo.MarkProviderStarted(context.Background(), "company_1", reserved.ID, staleStart); err != nil {
		t.Fatalf("MarkProviderStarted: %v", err)
	}

	recovered, err := repo.RecoverInterruptedTurns(context.Background(), 30*time.Minute)
	if err != nil {
		t.Fatalf("RecoverInterruptedTurns: %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("expected exactly 1 recovered turn, got %d", len(recovered))
	}
	if recovered[0].Status != spatial.SpatialDesignTurnStatusNeedsAttention {
		t.Fatalf("expected recovered turn to become needs_attention, got %s", recovered[0].Status)
	}

	updatedSession, err := repo.FindSession(context.Background(), "company_1", created.ID)
	if err != nil {
		t.Fatalf("FindSession after recovery: %v", err)
	}
	if updatedSession.ActiveTurnID != "" {
		t.Fatalf("expected ActiveTurnID cleared atomically by recovery, got %q", updatedSession.ActiveTurnID)
	}

	// Recovery must never run twice on the same turn — a second call finds
	// nothing left to recover.
	recoveredAgain, err := repo.RecoverInterruptedTurns(context.Background(), 30*time.Minute)
	if err != nil {
		t.Fatalf("second RecoverInterruptedTurns: %v", err)
	}
	if len(recoveredAgain) != 0 {
		t.Fatalf("expected no turns left to recover on second call, got %d", len(recoveredAgain))
	}
}

// TestMongoDesignSessionRepository_FinishTurn_PersistsProposedDeltaAndValidatedPlan
// is a regression test for a genuine defect discovered during Task 10's
// end-to-end HTTP test: FinishTurn's Mongo update never persisted
// ProposedDelta/ValidatedPlan (no bson field existed for them at all), so
// they silently round-tripped to nil even though every fake-repository-
// backed service test (Task 7) passed, since the fake simply kept the
// struct in memory. Only a real Mongo read-back after FinishTurn proves
// this.
func TestMongoDesignSessionRepository_FinishTurn_PersistsProposedDeltaAndValidatedPlan(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_persist", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turn := spatial.SpatialDesignTurn{
		CompanyID: "company_1", SessionID: created.ID,
		ClientRequestID: "request_persist", RequestFingerprint: "fp_persist",
		BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
		Instruction: "Actually make it beige.", Status: spatial.SpatialDesignTurnStatusReserved,
		StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	reserved, _, err := repo.ReserveTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	delta := spatial.ProposedSceneEditDelta{
		Target: spatial.SpatialDesignTarget{Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123"},
		Intent: spatial.DesignIntentMaterialAppearance, Summary: []string{"Change the sofa upholstery to beige."},
		Geometry: spatial.SectionChange{Mode: spatial.SectionModePreserve},
		Material: spatial.SectionChange{Mode: spatial.SectionModeReplace, MaterialSpec: &spatial.WorkingDesignMaterial{
			BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial: spatial.SectionChange{Mode: spatial.SectionModePreserve}, Confidence: 0.91,
	}
	plan := spatial.ValidatedSceneEditPlan{
		Target: delta.Target, BasedOnRoomDraftRevision: 17,
		WorkingDesign: spatial.WorkingDesign{Material: delta.Material.MaterialSpec},
		Fit:           spatial.FitAnalysis{Status: spatial.FitStatusClear},
		Execution:     spatial.DesignExecutionFlags{TurnRequiresAssetGeneration: false, HunyuanRequired: true, RequiresConfirmation: true, Executable: true},
	}

	finished, err := repo.FinishTurn(context.Background(), spatial.FinishTurnInput{
		CompanyID: "company_1", SessionID: created.ID, TurnID: reserved.ID,
		Status: spatial.SpatialDesignTurnStatusProposed, ProposedDelta: &delta, ValidatedPlan: &plan,
		PlanFingerprint: "fp_test_123", WorkingDesign: plan.WorkingDesign,
	})
	if err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	if finished.ProposedDelta == nil {
		t.Fatal("expected ProposedDelta to be present on FinishTurn's own return value")
	}

	// The real regression: read the turn back FRESH from Mongo (a second
	// FindTurn call, not the value FinishTurn happened to return in
	// memory) — this is what would have caught the bug.
	reread, err := repo.FindTurn(context.Background(), "company_1", reserved.ID)
	if err != nil {
		t.Fatalf("FindTurn after FinishTurn: %v", err)
	}
	if reread.ProposedDelta == nil {
		t.Fatal("expected ProposedDelta to survive a fresh Mongo read-back")
	}
	if reread.ProposedDelta.Material.MaterialSpec == nil || reread.ProposedDelta.Material.MaterialSpec.BaseColor != "#c8a464" {
		t.Fatalf("expected material spec to survive round-trip, got %+v", reread.ProposedDelta.Material)
	}
	if reread.ValidatedPlan == nil {
		t.Fatal("expected ValidatedPlan to survive a fresh Mongo read-back")
	}
	if !reread.ValidatedPlan.Execution.HunyuanRequired {
		t.Fatal("expected execution flags to survive round-trip")
	}
}

func TestMongoDesignSessionRepository_ListTurns_NewestFirstPaginated(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	session := newTestSession("company_1", "client_session_9", "roomdraft_1", 17)
	created, err := repo.CreateOrGetSession(context.Background(), session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 3; i++ {
		turn := spatial.SpatialDesignTurn{
			CompanyID: "company_1", SessionID: created.ID,
			ClientRequestID: "request_list_" + string(rune('a'+i)), RequestFingerprint: "fp_list_" + string(rune('a'+i)),
			BaseSessionRevision: created.Revision, BasedOnRoomDraftRevision: 17,
			Instruction: "x", Status: spatial.SpatialDesignTurnStatusReserved,
			StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
		}
		reserved, _, err := repo.ReserveTurn(context.Background(), turn)
		if err != nil {
			t.Fatalf("reserve turn %d: %v", i, err)
		}
		if _, err := repo.FinishTurn(context.Background(), spatial.FinishTurnInput{
			CompanyID: "company_1", SessionID: created.ID, TurnID: reserved.ID, Status: spatial.SpatialDesignTurnStatusProposed,
		}); err != nil {
			t.Fatalf("finish turn %d: %v", i, err)
		}
		// re-fetch session so BaseSessionRevision advances for the next turn
		created, err = repo.FindSession(context.Background(), "company_1", created.ID)
		if err != nil {
			t.Fatalf("re-fetch session: %v", err)
		}
	}

	turns, err := repo.ListTurns(context.Background(), "company_1", created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(turns))
	}
	if turns[0].Sequence < turns[1].Sequence || turns[1].Sequence < turns[2].Sequence {
		t.Fatalf("expected newest-first ordering, got sequences %d, %d, %d", turns[0].Sequence, turns[1].Sequence, turns[2].Sequence)
	}
}
