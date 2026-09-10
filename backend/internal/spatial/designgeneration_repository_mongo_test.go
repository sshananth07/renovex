package spatial_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func newTestAttempt(companyID, sessionID, turnID, clientRequestID string, attemptNumber int64) spatial.DesignGenerationAttempt {
	now := time.Now()
	return spatial.DesignGenerationAttempt{
		CompanyID: companyID, ProjectID: "project_1", SpaceID: "space_1", RoomDraftID: "roomdraft_1",
		SessionID: sessionID, TurnID: turnID, PlanFingerprint: "plan_fp_1", AttemptNumber: attemptNumber,
		ClientRequestID: clientRequestID, RequestFingerprint: "req_fp_" + clientRequestID,
		BasedOnRoomDraftRevision: 17,
		Kind:                     spatial.DesignGenerationKindMaterialOnly,
		Status:                   spatial.DesignGenerationStatusReserved,
		ActiveSlot:               "active",
		TargetSnapshot: spatial.AuthorizedDesignTarget{
			Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123", Category: "sofa",
		},
		CreatedByUserID: "user_1", CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	}
}

func TestMongoDesignGenerationRepository_ReserveAttempt_ReplaysSameRequest(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_reserve_replay", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	attempt := newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_1", 1)
	first, replayed, err := repo.ReserveAttempt(context.Background(), attempt)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if replayed {
		t.Fatal("expected first reserve to not be a replay")
	}

	second, replayed, err := repo.ReserveAttempt(context.Background(), attempt)
	if err != nil {
		t.Fatalf("replay reserve: %v", err)
	}
	if !replayed {
		t.Fatal("expected second identical reserve to be a replay")
	}
	if first.ID != second.ID {
		t.Fatalf("expected replay to return the SAME attempt, got %s vs %s", first.ID, second.ID)
	}
}

func TestMongoDesignGenerationRepository_ReserveAttempt_ConflictOnChangedFingerprint(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_reserve_conflict", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	attempt := newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_2", 1)
	if _, _, err := repo.ReserveAttempt(context.Background(), attempt); err != nil {
		t.Fatalf("first reserve: %v", err)
	}

	changed := attempt
	changed.RequestFingerprint = "different_fingerprint"
	_, _, err = repo.ReserveAttempt(context.Background(), changed)
	if !errors.Is(err, spatial.ErrDesignGenerationRequestConflict) {
		t.Fatalf("expected ErrDesignGenerationRequestConflict, got %v", err)
	}
}

func TestMongoDesignGenerationRepository_ReserveAttempt_OneActiveSlotPerTurn(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_active_slot", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	first := newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_a", 1)
	if _, _, err := repo.ReserveAttempt(context.Background(), first); err != nil {
		t.Fatalf("first reserve: %v", err)
	}

	// A DIFFERENT client request for the SAME turn while the first attempt
	// is still active must be rejected by the partial unique index on
	// (companyId, turnId, activeSlot) — the plan's "one active attempt per
	// turn" invariant.
	second := newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_b", 2)
	_, _, err = repo.ReserveAttempt(context.Background(), second)
	if !errors.Is(err, spatial.ErrDesignGenerationInProgress) {
		t.Fatalf("expected ErrDesignGenerationInProgress, got %v", err)
	}
}

func TestMongoDesignGenerationRepository_CompleteWithConcept_TransitionsAndAdvancesSession(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_complete", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	attempt, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_c", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	candidate := spatial.DesignConcept{
		Target:           spatial.SpatialDesignTarget{Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123"},
		VisualAction:     spatial.ConceptBindingActionPreserve,
		AppearanceAction: spatial.ConceptAppearanceActionSet,
	}
	completed, err := repo.CompleteWithConcept(context.Background(), "company_1", attempt.ID, candidate)
	if err != nil {
		t.Fatalf("CompleteWithConcept: %v", err)
	}
	if completed.Status != spatial.DesignGenerationStatusConceptReady {
		t.Fatalf("expected concept_ready, got %s", completed.Status)
	}
	if completed.ActiveSlot != "" {
		t.Fatalf("expected ActiveSlot cleared, got %q", completed.ActiveSlot)
	}

	// Repeating the transition must fail: the fenced update requires
	// activeSlot to still be set.
	if _, err := repo.CompleteWithConcept(context.Background(), "company_1", attempt.ID, candidate); !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Fatalf("expected ErrDesignGenerationNotClaimable on repeat, got %v", err)
	}

	updatedSession, err := sessionRepo.FindSession(context.Background(), "company_1", session.ID)
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if updatedSession.LatestReadyAttemptID != attempt.ID {
		t.Fatalf("expected LatestReadyAttemptID set, got %q", updatedSession.LatestReadyAttemptID)
	}
}

func TestMongoDesignGenerationRepository_Cancel_WinsOverLateCompletion(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_cancel", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	attempt, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_d", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	cancelled, err := repo.Cancel(context.Background(), "company_1", attempt.ID, "cancel_req_1", "cancel_fp_1")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if cancelled.Status != spatial.DesignGenerationStatusAbandoned {
		t.Fatalf("expected abandoned, got %s", cancelled.Status)
	}

	// A later completion attempt must never succeed against an abandoned
	// attempt — terminal abandonment wins (RP4E2 plan invariant).
	candidate := spatial.DesignConcept{Target: spatial.SpatialDesignTarget{Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123"}}
	if _, err := repo.CompleteWithConcept(context.Background(), "company_1", attempt.ID, candidate); !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Fatalf("expected ErrDesignGenerationNotClaimable, got %v", err)
	}

	// Repeated cancel calls are idempotent.
	repeated, err := repo.Cancel(context.Background(), "company_1", attempt.ID, "cancel_req_2", "cancel_fp_2")
	if err != nil {
		t.Fatalf("repeat Cancel: %v", err)
	}
	if repeated.Status != spatial.DesignGenerationStatusAbandoned {
		t.Fatalf("expected repeat cancel to return abandoned, got %s", repeated.Status)
	}
}

func TestMongoDesignGenerationRepository_ApplyAcceptance_AppliesOperationsAndAdvancesSession(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	draftRepo := spatial.NewMongoRoomDraftRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	draft, err := draftRepo.Create(context.Background(), spatial.RoomDraft{
		CompanyID: "company_1", CaptureID: "capture_1",
		Objects: []spatial.RoomDraftObject{{ID: "object_sofa_123", Category: "sofa"}},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_accept", draft.ID, draft.Revision))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	attempt, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_e", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	setAppearance := spatial.SetVisualAppearanceOperation{
		TargetKind: spatial.VisualAssetTargetObject, TargetID: "object_sofa_123",
		Appearance: spatial.VisualAppearance{BaseColor: "#2f4f3a", MaterialFamily: spatial.MaterialFamilyFabric, Roughness: spatial.RoughnessMatte},
	}
	acceptedDesign := spatial.WorkingDesign{
		Material: &spatial.WorkingDesignMaterial{BaseColor: "#2f4f3a", MaterialFamily: "fabric", Roughness: "matte"},
	}

	result, err := repo.ApplyAcceptance(context.Background(), spatial.ApplyAcceptanceInput{
		CompanyID: "company_1", SessionID: session.ID, TurnID: "turn_1", AttemptID: attempt.ID,
		PlanFingerprint: "plan_fp_1", ClientRequestID: "use_req_1", RequestFingerprint: "use_fp_1",
		RoomDraftID: draft.ID, ExpectedRoomDraftRevision: draft.Revision,
		Operations: []spatial.PendingCanonicalOperation{
			{Kind: spatial.EditOpSetVisualAppearance, Payload: `{"targetKind":"object","targetId":"object_sofa_123"}`, Transform: setAppearance.Apply},
		},
		AcceptedDesign: acceptedDesign, ActorUserID: "user_1",
	})
	if err != nil {
		t.Fatalf("ApplyAcceptance: %v", err)
	}
	if result.Replayed {
		t.Fatal("expected first call to not be a replay")
	}
	if result.RoomDraft.Objects[0].Appearance == nil || result.RoomDraft.Objects[0].Appearance.BaseColor != "#2f4f3a" {
		t.Fatalf("expected appearance applied to draft, got %+v", result.RoomDraft.Objects[0].Appearance)
	}
	if result.RoomDraft.Revision != draft.Revision+1 {
		t.Fatalf("expected revision advanced by exactly one operation, got %d", result.RoomDraft.Revision)
	}
	if len(result.AppliedOperationIDs) != 1 {
		t.Fatalf("expected exactly one applied operation id, got %v", result.AppliedOperationIDs)
	}

	updatedAttempt, err := repo.FindAttempt(context.Background(), "company_1", attempt.ID)
	if err != nil {
		t.Fatalf("FindAttempt: %v", err)
	}
	if updatedAttempt.Status != spatial.DesignGenerationStatusAccepted {
		t.Fatalf("expected attempt marked accepted, got %s", updatedAttempt.Status)
	}

	updatedSession, err := sessionRepo.FindSession(context.Background(), "company_1", session.ID)
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if updatedSession.AcceptedTurnID != "turn_1" || updatedSession.AcceptedAttemptID != attempt.ID {
		t.Fatalf("expected session accepted fields set, got turnId=%q attemptId=%q", updatedSession.AcceptedTurnID, updatedSession.AcceptedAttemptID)
	}
	if updatedSession.BasedOnRoomDraftRevision != draft.Revision+1 {
		t.Fatalf("expected session pin advanced to resulting revision, got %d", updatedSession.BasedOnRoomDraftRevision)
	}

	// Replay with the SAME client request id must return the original
	// result without applying anything again.
	replay, err := repo.ApplyAcceptance(context.Background(), spatial.ApplyAcceptanceInput{
		CompanyID: "company_1", SessionID: session.ID, TurnID: "turn_1", AttemptID: attempt.ID,
		PlanFingerprint: "plan_fp_1", ClientRequestID: "use_req_1", RequestFingerprint: "use_fp_1",
		RoomDraftID: draft.ID, ExpectedRoomDraftRevision: draft.Revision,
		Operations: []spatial.PendingCanonicalOperation{
			{Kind: spatial.EditOpSetVisualAppearance, Payload: `{"targetKind":"object","targetId":"object_sofa_123"}`, Transform: setAppearance.Apply},
		},
		AcceptedDesign: acceptedDesign, ActorUserID: "user_1",
	})
	if err != nil {
		t.Fatalf("replay ApplyAcceptance: %v", err)
	}
	if !replay.Replayed {
		t.Fatal("expected replay to be flagged")
	}
	if replay.Acceptance.ID != result.Acceptance.ID {
		t.Fatalf("expected replay to return the SAME acceptance, got %s vs %s", replay.Acceptance.ID, result.Acceptance.ID)
	}
}

func TestMongoDesignGenerationRepository_ApplyAcceptance_StaleRevisionAppliesNothing(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	draftRepo := spatial.NewMongoRoomDraftRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	draft, err := draftRepo.Create(context.Background(), spatial.RoomDraft{
		CompanyID: "company_1", CaptureID: "capture_1",
		Objects: []spatial.RoomDraftObject{{ID: "object_sofa_123", Category: "sofa"}},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_stale", draft.ID, draft.Revision))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	attempt, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_req_f", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	setAppearance := spatial.SetVisualAppearanceOperation{
		TargetKind: spatial.VisualAssetTargetObject, TargetID: "object_sofa_123",
		Appearance: spatial.VisualAppearance{BaseColor: "#2f4f3a", MaterialFamily: spatial.MaterialFamilyFabric, Roughness: spatial.RoughnessMatte},
	}

	_, err = repo.ApplyAcceptance(context.Background(), spatial.ApplyAcceptanceInput{
		CompanyID: "company_1", SessionID: session.ID, TurnID: "turn_1", AttemptID: attempt.ID,
		PlanFingerprint: "plan_fp_1", ClientRequestID: "use_req_stale", RequestFingerprint: "use_fp_stale",
		RoomDraftID: draft.ID, ExpectedRoomDraftRevision: draft.Revision + 99, // deliberately stale
		Operations: []spatial.PendingCanonicalOperation{
			{Kind: spatial.EditOpSetVisualAppearance, Payload: `{}`, Transform: setAppearance.Apply},
		},
		AcceptedDesign: spatial.WorkingDesign{}, ActorUserID: "user_1",
	})
	if !errors.Is(err, spatial.ErrRoomDraftRevisionMismatch) {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch, got %v", err)
	}

	rereadDraft, err := draftRepo.FindByID(context.Background(), "company_1", draft.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if rereadDraft.Revision != draft.Revision {
		t.Fatalf("expected draft revision unchanged, got %d", rereadDraft.Revision)
	}
	if rereadDraft.Objects[0].Appearance != nil {
		t.Fatal("expected nothing applied on a stale-revision rejection")
	}
}

// --- Gate 2 worker-phase methods ---

func TestMongoDesignGenerationRepository_ClaimNextGenerationPhase_ClaimsReservedAndTransitions(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_claim_1", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reserved, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_claim_1", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	claimed, err := repo.ClaimNextGenerationPhase(context.Background())
	if err != nil {
		t.Fatalf("ClaimNextGenerationPhase: %v", err)
	}
	if claimed.ID != reserved.ID {
		t.Fatalf("expected to claim %s, got %s", reserved.ID, claimed.ID)
	}
	if claimed.Status != spatial.DesignGenerationStatusGeneratingReference {
		t.Fatalf("expected status generating_reference after claim, got %q", claimed.Status)
	}

	// A second claim attempt must find nothing else claimable.
	_, err = repo.ClaimNextGenerationPhase(context.Background())
	if !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Errorf("expected ErrDesignGenerationNotClaimable on the second claim, got %v", err)
	}
}

func TestMongoDesignGenerationRepository_ClaimNextGenerationPhase_ExcludesDangerousState(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_claim_dangerous", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reserved, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_claim_dangerous", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	// Simulate a worker that started the reference call but crashed before
	// any further checkpoint — the one dangerous state that must NEVER be
	// automatically reclaimed.
	if err := repo.SetReferenceProviderStarted(context.Background(), "company_1", reserved.ID, time.Now()); err != nil {
		t.Fatalf("SetReferenceProviderStarted: %v", err)
	}

	_, err = repo.ClaimNextGenerationPhase(context.Background())
	if !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Errorf("expected the dangerous-state attempt to be excluded from claiming, got %v", err)
	}
}

func TestMongoDesignGenerationRepository_SetReferenceProviderStarted_RejectsAfterCancel(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_cancel_wins", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reserved, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_cancel_wins", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	// Cancel wins over a later worker checkpoint write — the worker
	// "claimed" this attempt (in spirit) before the cancel landed, but its
	// SetReferenceProviderStarted call arrives AFTER cancellation.
	if _, err := repo.Cancel(context.Background(), "company_1", reserved.ID, "cancel_req_1", "cancel_fp_1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	err = repo.SetReferenceProviderStarted(context.Background(), "company_1", reserved.ID, time.Now())
	if !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Errorf("expected ErrDesignGenerationNotClaimable for a checkpoint write after cancel, got %v", err)
	}
}

func TestMongoDesignGenerationRepository_SetReferenceReady_PersistsImageAndTransitions(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_ref_ready", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reserved, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_ref_ready", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	image := spatial.DesignReferenceImage{
		ObjectKey:      "design-reference/company_1/" + reserved.ID + "/reference.jpg",
		ChecksumSHA256: "abc123", ContentType: "image/jpeg", SizeBytes: 12345,
		Width: 1024, Height: 1024, Provider: "cloudflare_flux", ProviderRequestID: "req_xyz",
		Model: "flux-1-schnell", PromptVersion: "v1", Seed: 42, CreatedAt: time.Now(),
	}
	updated, err := repo.SetReferenceReady(context.Background(), "company_1", reserved.ID, image)
	if err != nil {
		t.Fatalf("SetReferenceReady: %v", err)
	}
	if updated.Status != spatial.DesignGenerationStatusReferenceReady {
		t.Fatalf("expected status reference_ready, got %q", updated.Status)
	}
	if updated.ReferenceImage == nil || updated.ReferenceImage.ObjectKey != image.ObjectKey {
		t.Fatalf("expected the reference image to be persisted, got %+v", updated.ReferenceImage)
	}

	reread, err := repo.FindAttempt(context.Background(), "company_1", reserved.ID)
	if err != nil {
		t.Fatalf("FindAttempt: %v", err)
	}
	if reread.ReferenceImage == nil || reread.ReferenceImage.ChecksumSHA256 != "abc123" {
		t.Fatalf("expected the persisted reference image to survive a re-read, got %+v", reread.ReferenceImage)
	}
}

func TestMongoDesignGenerationRepository_SetAssetGenerationPendingThenProcessing(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_asset_pending", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reserved, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_asset_pending", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	pending, err := repo.SetAssetGenerationPending(context.Background(), "company_1", reserved.ID, "job_abc")
	if err != nil {
		t.Fatalf("SetAssetGenerationPending: %v", err)
	}
	if pending.Status != spatial.DesignGenerationStatusAssetGenerationPending || pending.AssetGenerationJobID != "job_abc" {
		t.Fatalf("expected asset_generation_pending with job_abc linked, got status=%q jobID=%q", pending.Status, pending.AssetGenerationJobID)
	}

	processing, err := repo.SetAssetGenerationProcessing(context.Background(), "company_1", reserved.ID)
	if err != nil {
		t.Fatalf("SetAssetGenerationProcessing: %v", err)
	}
	if processing.Status != spatial.DesignGenerationStatusAssetGenerationProcessing {
		t.Fatalf("expected asset_generation_processing, got %q", processing.Status)
	}
	if processing.AssetGenerationJobID != "job_abc" {
		t.Fatalf("expected the job linkage to survive the processing transition, got %q", processing.AssetGenerationJobID)
	}
}

func TestMongoDesignGenerationRepository_WorkerPhaseWritesRejectedAfterAbandon(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoDesignGenerationRepository(db)
	sessionRepo := spatial.NewMongoDesignRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := sessionRepo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	session, err := sessionRepo.CreateOrGetSession(context.Background(), newTestSession("company_1", "cs_abandon_wins", "roomdraft_1", 17))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reserved, _, err := repo.ReserveAttempt(context.Background(), newTestAttempt("company_1", session.ID, "turn_1", "confirm_abandon_wins", 1))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := repo.Cancel(context.Background(), "company_1", reserved.ID, "cancel_req_2", "cancel_fp_2"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	image := spatial.DesignReferenceImage{ObjectKey: "k", ChecksumSHA256: "c", ContentType: "image/jpeg", CreatedAt: time.Now()}
	if _, err := repo.SetReferenceReady(context.Background(), "company_1", reserved.ID, image); !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Errorf("SetReferenceReady after abandon: expected ErrDesignGenerationNotClaimable, got %v", err)
	}
	if _, err := repo.SetAssetGenerationPending(context.Background(), "company_1", reserved.ID, "job_x"); !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Errorf("SetAssetGenerationPending after abandon: expected ErrDesignGenerationNotClaimable, got %v", err)
	}
	if _, err := repo.SetAssetGenerationProcessing(context.Background(), "company_1", reserved.ID); !errors.Is(err, spatial.ErrDesignGenerationNotClaimable) {
		t.Errorf("SetAssetGenerationProcessing after abandon: expected ErrDesignGenerationNotClaimable, got %v", err)
	}
}
