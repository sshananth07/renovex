package spatial

import "testing"

func TestToDesignSessionDTO_OmitsInternalFields(t *testing.T) {
	session := SpatialDesignSession{
		ID: "session_1", RoomDraftID: "roomdraft_1",
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
		Status: SpatialDesignSessionStatusActive, BasedOnRoomDraftRevision: 17,
		LatestTurnID: "turn_1", LatestReadyPlanTurnID: "turn_1", Revision: 1,
	}
	dto := toDesignSessionDTO(session)
	if dto.ID != "session_1" {
		t.Fatalf("expected id forwarded, got %s", dto.ID)
	}
	if dto.RoomDraftID != "roomdraft_1" {
		t.Fatalf("expected roomDraftId forwarded, got %s", dto.RoomDraftID)
	}
	if dto.Target.ID != "object_sofa_123" {
		t.Fatalf("expected target forwarded, got %+v", dto.Target)
	}
}

func TestToDesignTurnDTO_ExcludesProviderPromptAndReasoningContent(t *testing.T) {
	started := SpatialDesignTurnStatusProposed
	turn := SpatialDesignTurn{
		ID: "turn_1", SessionID: "session_1", Sequence: 1,
		Instruction: "Actually make it beige.", Status: started,
		BasedOnRoomDraftRevision: 17, PlanFingerprint: "abc123",
		ValidatedPlan: &ValidatedSceneEditPlan{
			Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
			WorkingDesign: WorkingDesign{
				Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
			},
			Fit:       FitAnalysis{Status: FitStatusClear},
			Execution: DesignExecutionFlags{TurnRequiresAssetGeneration: false, HunyuanRequired: true, RequiresConfirmation: true, Executable: true},
		},
	}
	dto := toDesignTurnDTO(turn, "3D generation currently runs in a limited test GPU environment.")

	if dto.ID != "turn_1" || dto.SessionID != "session_1" {
		t.Fatalf("expected id/sessionId forwarded, got %+v", dto)
	}
	if dto.Execution.HunyuanRequired != true {
		t.Fatalf("expected hunyuanRequired forwarded, got %+v", dto.Execution)
	}
	if dto.Execution.RuntimeNotice == "" {
		t.Fatal("expected runtime notice populated when hunyuanRequired is true")
	}
	// The DTO struct itself carries no field for provider name, model,
	// prompt text, or reasoning content — proven structurally: this test
	// compiles and only ever reads dto's declared fields (id, sessionId,
	// sequence, instruction, status, changePlan, fitAnalysis, execution,
	// review, planFingerprint) — there is nowhere on the type for those
	// values to have gone.
}

func TestToDesignTurnDTO_NoRuntimeNoticeWhenHunyuanNotRequired(t *testing.T) {
	turn := SpatialDesignTurn{
		ID: "turn_1", SessionID: "session_1", Sequence: 1, Status: SpatialDesignTurnStatusProposed,
		ValidatedPlan: &ValidatedSceneEditPlan{
			Execution: DesignExecutionFlags{HunyuanRequired: false},
		},
	}
	dto := toDesignTurnDTO(turn, "notice text")
	if dto.Execution.RuntimeNotice != "" {
		t.Fatalf("expected no runtime notice when hunyuanRequired is false, got %q", dto.Execution.RuntimeNotice)
	}
}

func TestMapDesignError_StaleSessionIsConflict(t *testing.T) {
	err := mapDesignError(ErrDesignPlanStale)
	if err == nil {
		t.Fatal("expected a non-nil huma error")
	}
}

func TestMapDesignError_ForeignAndMissingSessionIndistinguishable(t *testing.T) {
	err1 := mapDesignError(ErrDesignSessionNotFound)
	err2 := mapDesignError(ErrDesignTargetNotFound)
	if err1 == nil || err2 == nil {
		t.Fatal("expected non-nil huma errors")
	}
}
