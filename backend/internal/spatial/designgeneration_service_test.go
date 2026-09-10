package spatial

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- fakes: DesignGenerationAttemptRepository / DesignAcceptanceRepository ---

type fakeDesignGenerationRepo struct {
	attempts          map[string]DesignGenerationAttempt
	attemptsByRequest map[string]string // companyID|sessionID|clientRequestID -> attemptID
	activeSlot        map[string]string // companyID|turnID -> attemptID (only while non-terminal)
	nextID            int

	acceptances          map[string]DesignAcceptance
	acceptancesByRequest map[string]string // companyID|sessionID|clientRequestID -> acceptanceID
	acceptancesByAttempt map[string]string // companyID|attemptID -> acceptanceID

	sessions *fakeDesignSessionAndTurnRepo
	drafts   *fakeRoomDraftRepoForDesign
}

func newFakeDesignGenerationRepo(sessions *fakeDesignSessionAndTurnRepo, drafts *fakeRoomDraftRepoForDesign) *fakeDesignGenerationRepo {
	return &fakeDesignGenerationRepo{
		attempts: map[string]DesignGenerationAttempt{}, attemptsByRequest: map[string]string{}, activeSlot: map[string]string{},
		acceptances: map[string]DesignAcceptance{}, acceptancesByRequest: map[string]string{}, acceptancesByAttempt: map[string]string{},
		sessions: sessions, drafts: drafts,
	}
}

func (f *fakeDesignGenerationRepo) ReserveAttempt(_ context.Context, attempt DesignGenerationAttempt) (DesignGenerationAttempt, bool, error) {
	requestKey := attempt.CompanyID + "|" + attempt.SessionID + "|" + attempt.ClientRequestID
	if existingID, ok := f.attemptsByRequest[requestKey]; ok {
		existing := f.attempts[existingID]
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return DesignGenerationAttempt{}, false, ErrDesignGenerationRequestConflict
		}
		return existing, true, nil
	}
	slotKey := attempt.CompanyID + "|" + attempt.TurnID
	if _, active := f.activeSlot[slotKey]; active {
		return DesignGenerationAttempt{}, false, ErrDesignGenerationInProgress
	}
	f.nextID++
	attempt.ID = "attempt_" + string(rune('a'+f.nextID))
	f.attempts[attempt.ID] = attempt
	f.attemptsByRequest[requestKey] = attempt.ID
	f.activeSlot[slotKey] = attempt.ID

	session := f.sessions.sessions[attempt.SessionID]
	session.LatestGenerationAttemptID = attempt.ID
	f.sessions.sessions[attempt.SessionID] = session

	return attempt, false, nil
}

func (f *fakeDesignGenerationRepo) CompleteWithConcept(_ context.Context, companyID, attemptID string, candidate DesignConcept) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID || a.ActiveSlot == "" {
		return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
	}
	a.Status = DesignGenerationStatusConceptReady
	a.Candidate = candidate
	a.ActiveSlot = ""
	now := time.Now()
	a.CompletedAt = &now
	f.attempts[attemptID] = a
	delete(f.activeSlot, a.CompanyID+"|"+a.TurnID)

	session := f.sessions.sessions[a.SessionID]
	session.LatestReadyAttemptID = attemptID
	f.sessions.sessions[a.SessionID] = session
	return a, nil
}

func (f *fakeDesignGenerationRepo) Fail(_ context.Context, companyID, attemptID string, status DesignGenerationStatus, safeFailureCode string) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID || a.ActiveSlot == "" {
		return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
	}
	a.Status = status
	a.SafeFailureCode = safeFailureCode
	a.ActiveSlot = ""
	f.attempts[attemptID] = a
	delete(f.activeSlot, a.CompanyID+"|"+a.TurnID)
	return a, nil
}

func (f *fakeDesignGenerationRepo) Cancel(_ context.Context, companyID, attemptID, cancelClientRequestID, cancelRequestFingerprint string) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	if isTerminalDesignGenerationStatus(a.Status) {
		return a, nil
	}
	a.Status = DesignGenerationStatusAbandoned
	a.ActiveSlot = ""
	a.CancelClientRequestID = cancelClientRequestID
	a.CancelRequestFingerprint = cancelRequestFingerprint
	f.attempts[attemptID] = a
	delete(f.activeSlot, a.CompanyID+"|"+a.TurnID)
	return a, nil
}

func (f *fakeDesignGenerationRepo) ClaimNextGenerationPhase(_ context.Context) (DesignGenerationAttempt, error) {
	for id, a := range f.attempts {
		if a.Status == DesignGenerationStatusReserved && a.ReferenceProviderStartedAt == nil {
			a.Status = DesignGenerationStatusGeneratingReference
			f.attempts[id] = a
			return a, nil
		}
	}
	return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
}

func (f *fakeDesignGenerationRepo) SetReferenceProviderStarted(_ context.Context, companyID, attemptID string, startedAt time.Time) error {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID || a.ActiveSlot == "" {
		return ErrDesignGenerationNotClaimable
	}
	a.ReferenceProviderStartedAt = &startedAt
	f.attempts[attemptID] = a
	return nil
}

func (f *fakeDesignGenerationRepo) SetReferenceReady(_ context.Context, companyID, attemptID string, image DesignReferenceImage) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID || a.ActiveSlot == "" {
		return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
	}
	a.Status = DesignGenerationStatusReferenceReady
	a.ReferenceImage = &image
	f.attempts[attemptID] = a
	return a, nil
}

func (f *fakeDesignGenerationRepo) SetAssetGenerationPending(_ context.Context, companyID, attemptID, assetGenerationJobID string) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID || a.ActiveSlot == "" {
		return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
	}
	a.Status = DesignGenerationStatusAssetGenerationPending
	a.AssetGenerationJobID = assetGenerationJobID
	f.attempts[attemptID] = a
	return a, nil
}

func (f *fakeDesignGenerationRepo) SetAssetGenerationProcessing(_ context.Context, companyID, attemptID string) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[attemptID]
	if !ok || a.CompanyID != companyID || a.ActiveSlot == "" {
		return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
	}
	a.Status = DesignGenerationStatusAssetGenerationProcessing
	f.attempts[attemptID] = a
	return a, nil
}

func (f *fakeDesignGenerationRepo) FindAttempt(_ context.Context, companyID, id string) (DesignGenerationAttempt, error) {
	a, ok := f.attempts[id]
	if !ok || a.CompanyID != companyID {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	return a, nil
}

func (f *fakeDesignGenerationRepo) FindAttemptByClientRequestID(_ context.Context, companyID, sessionID, clientRequestID string) (DesignGenerationAttempt, error) {
	id, ok := f.attemptsByRequest[companyID+"|"+sessionID+"|"+clientRequestID]
	if !ok {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	return f.attempts[id], nil
}

func (f *fakeDesignGenerationRepo) ListAttempts(_ context.Context, companyID, sessionID, turnID string, limit int) ([]DesignGenerationAttempt, error) {
	var out []DesignGenerationAttempt
	for _, a := range f.attempts {
		if a.CompanyID == companyID && a.SessionID == sessionID && (turnID == "" || a.TurnID == turnID) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeDesignGenerationRepo) FindAttemptByAssetGenerationJobID(_ context.Context, companyID, jobID string) (DesignGenerationAttempt, error) {
	for _, a := range f.attempts {
		if a.CompanyID == companyID && a.AssetGenerationJobID == jobID {
			return a, nil
		}
	}
	return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
}

func (f *fakeDesignGenerationRepo) FindAcceptanceByClientRequestID(_ context.Context, companyID, sessionID, clientRequestID string) (DesignAcceptance, error) {
	id, ok := f.acceptancesByRequest[companyID+"|"+sessionID+"|"+clientRequestID]
	if !ok {
		return DesignAcceptance{}, ErrDesignAcceptanceNotFound
	}
	return f.acceptances[id], nil
}

func (f *fakeDesignGenerationRepo) FindAcceptanceByAttemptID(_ context.Context, companyID, attemptID string) (DesignAcceptance, error) {
	id, ok := f.acceptancesByAttempt[companyID+"|"+attemptID]
	if !ok {
		return DesignAcceptance{}, ErrDesignAcceptanceNotFound
	}
	return f.acceptances[id], nil
}

// ApplyAcceptance is a simplified in-memory version of the real Mongo
// transaction: apply operations in order against the fake draft store, then
// record the acceptance/attempt/session side effects. It reuses the SAME
// idempotency-first/CAS-then-apply structure as the real implementation so
// service-level tests exercise realistic replay/staleness behavior without
// needing a real Mongo transaction.
func (f *fakeDesignGenerationRepo) ApplyAcceptance(_ context.Context, input ApplyAcceptanceInput) (ApplyAcceptanceResult, error) {
	requestKey := input.CompanyID + "|" + input.SessionID + "|" + input.ClientRequestID
	if existingID, ok := f.acceptancesByRequest[requestKey]; ok {
		existing := f.acceptances[existingID]
		if existing.RequestFingerprint != input.RequestFingerprint {
			return ApplyAcceptanceResult{}, ErrDesignAcceptanceRequestConflict
		}
		draft := f.drafts.byID[existing.RoomDraftID]
		return ApplyAcceptanceResult{RoomDraft: draft, Acceptance: existing, AppliedOperationIDs: existing.AppliedOperationIDs, Replayed: true}, nil
	}

	draft, ok := f.drafts.byID[input.RoomDraftID]
	if !ok || draft.CompanyID != input.CompanyID {
		return ApplyAcceptanceResult{}, ErrRoomDraftNotFound
	}
	if draft.Revision != input.ExpectedRoomDraftRevision {
		return ApplyAcceptanceResult{}, ErrRoomDraftRevisionMismatch
	}

	operationIDs := make([]string, 0, len(input.Operations))
	for i, op := range input.Operations {
		updated, err := op.Transform(draft)
		if err != nil {
			return ApplyAcceptanceResult{}, err
		}
		draft = updated
		operationIDs = append(operationIDs, input.ClientRequestID+":"+string(rune('0'+i)))
	}
	draft.Revision = input.ExpectedRoomDraftRevision + int64(len(input.Operations))
	f.drafts.byID[input.RoomDraftID] = draft

	acceptance := DesignAcceptance{
		ID: "acceptance_" + input.ClientRequestID, CompanyID: input.CompanyID,
		SessionID: input.SessionID, TurnID: input.TurnID, AttemptID: input.AttemptID,
		PlanFingerprint: input.PlanFingerprint, ClientRequestID: input.ClientRequestID, RequestFingerprint: input.RequestFingerprint,
		RoomDraftID: input.RoomDraftID, BaseRoomDraftRevision: input.ExpectedRoomDraftRevision, ResultingRoomDraftRevision: draft.Revision,
		AppliedOperationIDs: operationIDs, AcceptedDesign: input.AcceptedDesign,
		CreatedByUserID: input.ActorUserID, CreatedAt: time.Now(), SchemaVersion: 1,
	}
	f.acceptances[acceptance.ID] = acceptance
	f.acceptancesByRequest[requestKey] = acceptance.ID
	f.acceptancesByAttempt[input.CompanyID+"|"+input.AttemptID] = acceptance.ID

	attempt := f.attempts[input.AttemptID]
	attempt.Status = DesignGenerationStatusAccepted
	now := time.Now()
	attempt.AcceptedAt = &now
	f.attempts[input.AttemptID] = attempt

	session := f.sessions.sessions[input.SessionID]
	session.AcceptedDesign = &input.AcceptedDesign
	session.AcceptedTurnID = input.TurnID
	session.AcceptedAttemptID = input.AttemptID
	session.CurrentWorkingDesign = input.AcceptedDesign
	session.BasedOnRoomDraftRevision = draft.Revision
	f.sessions.sessions[input.SessionID] = session

	return ApplyAcceptanceResult{RoomDraft: draft, Acceptance: acceptance, AppliedOperationIDs: operationIDs, Replayed: false}, nil
}

// --- test setup helper ---

func newDesignGenerationServiceForTest(draft RoomDraft, roomDrafts RoomDraftRepository, designRepo *fakeDesignSessionAndTurnRepo, generationRepo *fakeDesignGenerationRepo) *Service {
	svc := &Service{}
	svc.SetRoomDraftSupport(roomDrafts)
	svc.SetDesignPlanningSupport(designRepo, designRepo, &fakeElementReasoner{})
	svc.SetDesignGenerationSupport(generationRepo, generationRepo, generationRepo)
	return svc
}

// fakeGenerationWakePublisher records every PublishGenerationWake call —
// used to prove ConfirmDesignPlan/RegenerateDesignPlan/asset-job submission
// fire (or correctly withhold) the Gate 4 queue wake hint.
type fakeGenerationWakePublisher struct {
	calls []fakeGenerationWakeCall
	err   error
}

type fakeGenerationWakeCall struct {
	kind GenerationWakeKind
	id   string
}

func (f *fakeGenerationWakePublisher) PublishGenerationWake(_ context.Context, kind GenerationWakeKind, id string) error {
	f.calls = append(f.calls, fakeGenerationWakeCall{kind: kind, id: id})
	return f.err
}

// readyMaterialOnlyTurn builds a session+turn already in "proposed" status
// with a material-only validated plan (HunyuanRequired=false), matching
// what CreateDesignTurn would have produced — Confirm/Regenerate never
// re-derive this, they only read it.
func setupReadyTurn(t *testing.T, designRepo *fakeDesignSessionAndTurnRepo, roomDrafts *fakeRoomDraftRepoForDesign, plan ValidatedSceneEditPlan) (SpatialDesignSession, SpatialDesignTurn) {
	t.Helper()
	draft := RoomDraft{
		ID: "roomdraft_1", CompanyID: "company_1", CaptureID: "capture_1",
		Objects: []RoomDraftObject{{ID: "object_sofa_123", Category: "sofa", Transform: RoomLocalTransform{Rotation: RoomLocalQuaternion{W: 1}}}},
	}
	created, err := roomDrafts.Create(context.Background(), draft)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	session, err := designRepo.CreateOrGetSession(context.Background(), SpatialDesignSession{
		CompanyID: "company_1", ProjectID: "project_1", SpaceID: "space_1", RoomDraftID: created.ID,
		CreatedByUserID: "user_1", ClientSessionID: "cs_1", SessionRequestFingerprint: "fp_cs_1",
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"}, BasedOnRoomDraftRevision: created.Revision,
		Status: SpatialDesignSessionStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	fingerprint, err := computeDesignPlanFingerprint(plan)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	reserved, _, err := designRepo.ReserveTurn(context.Background(), SpatialDesignTurn{
		CompanyID: "company_1", SessionID: session.ID, ClientRequestID: "turn_req_1", RequestFingerprint: "turn_fp_1",
		BaseSessionRevision: session.Revision, BasedOnRoomDraftRevision: session.BasedOnRoomDraftRevision,
		Instruction: "test", Status: SpatialDesignTurnStatusReserved, StartedAt: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("reserve turn: %v", err)
	}
	finished, err := designRepo.FinishTurn(context.Background(), FinishTurnInput{
		CompanyID: "company_1", SessionID: session.ID, TurnID: reserved.ID,
		Status: SpatialDesignTurnStatusProposed, ValidatedPlan: &plan, PlanFingerprint: fingerprint, WorkingDesign: plan.WorkingDesign,
	})
	if err != nil {
		t.Fatalf("finish turn: %v", err)
	}
	updatedSession, _ := designRepo.FindSession(context.Background(), "company_1", session.ID)
	return updatedSession, finished
}

func materialOnlyPlan() ValidatedSceneEditPlan {
	return ValidatedSceneEditPlan{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"}, BasedOnRoomDraftRevision: 0,
		WorkingDesign: WorkingDesign{Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"}},
		Fit:           FitAnalysis{Status: FitStatusClear},
		Execution:     DesignExecutionFlags{TurnRequiresAssetGeneration: false, HunyuanRequired: false, RequiresConfirmation: true, Executable: true},
	}
}

func geometryPlan() ValidatedSceneEditPlan {
	return ValidatedSceneEditPlan{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"}, BasedOnRoomDraftRevision: 0,
		WorkingDesign: WorkingDesign{Geometry: &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved", PreserveCanonicalDimensions: true}},
		Fit:           FitAnalysis{Status: FitStatusClear},
		Execution:     DesignExecutionFlags{TurnRequiresAssetGeneration: true, HunyuanRequired: true, RequiresConfirmation: true, Executable: true},
	}
}

// --- Confirm ---

func TestConfirmDesignPlan_MaterialOnlyReachesConceptReadyDirectly(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())

	attempt, replayed, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("ConfirmDesignPlan: %v", err)
	}
	if replayed {
		t.Fatal("expected first confirm to not be a replay")
	}
	if attempt.Kind != DesignGenerationKindMaterialOnly {
		t.Fatalf("expected material_only kind, got %s", attempt.Kind)
	}
	if attempt.Status != DesignGenerationStatusConceptReady {
		t.Fatalf("expected concept_ready directly (no Python/Hunyuan), got %s", attempt.Status)
	}
	if attempt.Candidate.AppearanceAction != ConceptAppearanceActionSet {
		t.Fatalf("expected appearance action set, got %s", attempt.Candidate.AppearanceAction)
	}
	if attempt.Candidate.VisualAction != ConceptBindingActionPreserve {
		t.Fatalf("expected visual action preserve (no geometry change), got %s", attempt.Candidate.VisualAction)
	}
}

func TestConfirmDesignPlan_GeometryStaysReserved(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())

	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_2", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("ConfirmDesignPlan: %v", err)
	}
	if attempt.Kind != DesignGenerationKindGeometry {
		t.Fatalf("expected geometry kind, got %s", attempt.Kind)
	}
	if attempt.Status != DesignGenerationStatusReserved {
		t.Fatalf("expected reserved (Gate 2 owns the rest), got %s", attempt.Status)
	}
}

// TestConfirmDesignPlan_NotifiesGenerationWakeOnlyForNonMaterialOnly proves
// Gate 4's queue-wake hint fires exactly when there is worker follow-up
// work to hint about (geometry/mixed, freshly reserved) and never for a
// material-only attempt that already reached concept_ready synchronously —
// a wake for an already-terminal attempt would be a wasted queue message
// (harmless per the plan's at-least-once design, but pointless).
func TestConfirmDesignPlan_NotifiesGenerationWakeOnlyForNonMaterialOnly(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)
	wake := &fakeGenerationWakePublisher{}
	svc.SetGenerationWakePublisher(wake)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())
	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_wake_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("ConfirmDesignPlan: %v", err)
	}
	if len(wake.calls) != 1 || wake.calls[0].kind != GenerationWakeKindDesignAttempt || wake.calls[0].id != attempt.ID {
		t.Fatalf("expected exactly one design_attempt wake for %s, got %+v", attempt.ID, wake.calls)
	}

	roomDrafts2 := newFakeRoomDraftRepoForDesign()
	designRepo2 := newFakeDesignRepo()
	generationRepo2 := newFakeDesignGenerationRepo(designRepo2, roomDrafts2)
	svc2 := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts2, designRepo2, generationRepo2)
	wake2 := &fakeGenerationWakePublisher{}
	svc2.SetGenerationWakePublisher(wake2)

	session2, turn2 := setupReadyTurn(t, designRepo2, roomDrafts2, materialOnlyPlan())
	if _, _, err := svc2.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session2.ID, turn2.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_wake_2", PlanFingerprint: turn2.PlanFingerprint, ExpectedRoomDraftRevision: session2.BasedOnRoomDraftRevision,
	}); err != nil {
		t.Fatalf("ConfirmDesignPlan: %v", err)
	}
	if len(wake2.calls) != 0 {
		t.Fatalf("expected no wake for a material-only attempt, got %+v", wake2.calls)
	}
}

// TestConfirmDesignPlan_NilGenerationWakePublisherIsSafe proves an unwired
// (nil) publisher — the local-development default — never panics or fails
// the caller's request.
func TestConfirmDesignPlan_NilGenerationWakePublisherIsSafe(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())
	if _, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_wake_nil", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	}); err != nil {
		t.Fatalf("ConfirmDesignPlan with no wake publisher configured: %v", err)
	}
}

func TestConfirmDesignPlan_ReplaysSameRequest(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())
	input := ConfirmDesignPlanInput{ClientRequestID: "confirm_3", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision}

	first, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, input)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	second, replayed, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, input)
	if err != nil {
		t.Fatalf("replay confirm: %v", err)
	}
	if !replayed {
		t.Fatal("expected second identical confirm to be a replay")
	}
	if first.ID != second.ID {
		t.Fatalf("expected replay to return the SAME attempt, got %s vs %s", first.ID, second.ID)
	}
}

func TestConfirmDesignPlan_StalePlanFingerprintRejected(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())

	_, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_4", PlanFingerprint: "wrong_fingerprint", ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if !errors.Is(err, ErrDesignPlanStale) {
		t.Fatalf("expected ErrDesignPlanStale, got %v", err)
	}
}

func TestConfirmDesignPlan_StaleRoomDraftRevisionRejected(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())

	_, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_5", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision + 99,
	})
	if !errors.Is(err, ErrDesignPlanStale) {
		t.Fatalf("expected ErrDesignPlanStale, got %v", err)
	}
}

func TestConfirmDesignPlan_NotLatestReadyTurnRejected(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, _ := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())

	_, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, "turn_not_latest", ConfirmDesignPlanInput{
		ClientRequestID: "confirm_6", PlanFingerprint: "irrelevant", ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if !errors.Is(err, ErrDesignAttemptNotLatestReady) {
		t.Fatalf("expected ErrDesignAttemptNotLatestReady, got %v", err)
	}
}

// --- Regenerate ---

func TestRegenerateDesignPlan_CreatesNewAttemptNumberAfterTerminal(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())
	first, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_7", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if first.Status != DesignGenerationStatusConceptReady {
		t.Fatalf("expected first attempt terminal (concept_ready), got %s", first.Status)
	}

	second, replayed, err := svc.RegenerateDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, RegenerateDesignPlanInput{
		ClientRequestID: "regen_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if replayed {
		t.Fatal("expected regenerate to not be a replay")
	}
	if second.ID == first.ID {
		t.Fatal("expected regenerate to create a genuinely new attempt")
	}
	if second.AttemptNumber <= first.AttemptNumber {
		t.Fatalf("expected new attempt number greater than previous, got %d vs %d", second.AttemptNumber, first.AttemptNumber)
	}
}

func TestRegenerateDesignPlan_RejectsWhilePreviousAttemptStillActive(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan()) // stays "reserved" (non-terminal)
	_, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_8", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	_, _, err = svc.RegenerateDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, RegenerateDesignPlanInput{
		ClientRequestID: "regen_2", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if !errors.Is(err, ErrDesignGenerationInProgress) {
		t.Fatalf("expected ErrDesignGenerationInProgress, got %v", err)
	}
}

// --- Cancel ---

func TestCancelDesignGenerationAttempt_MarksAbandoned(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())
	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_9", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	cancelled, err := svc.CancelDesignGenerationAttempt(context.Background(), "company_1", attempt.ID, "cancel_1")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != DesignGenerationStatusAbandoned {
		t.Fatalf("expected abandoned, got %s", cancelled.Status)
	}
}

// --- Use ---

func TestUseDesignPlan_MaterialOnlyAppliesSetVisualAppearance(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())
	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_10", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	acceptance, draft, replayed, err := svc.UseDesignPlan(context.Background(), "company_1", "user_1", session.ID, attempt.ID, UseDesignPlanInput{
		ClientRequestID: "use_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("UseDesignPlan: %v", err)
	}
	if replayed {
		t.Fatal("expected first use to not be a replay")
	}
	if draft.Objects[0].Appearance == nil || draft.Objects[0].Appearance.BaseColor != "#c8a464" {
		t.Fatalf("expected appearance applied to draft, got %+v", draft.Objects[0].Appearance)
	}
	if acceptance.AttemptID != attempt.ID {
		t.Fatalf("expected acceptance linked to attempt, got %q", acceptance.AttemptID)
	}

	updatedSession, _ := designRepo.FindSession(context.Background(), "company_1", session.ID)
	if updatedSession.AcceptedAttemptID != attempt.ID {
		t.Fatalf("expected session AcceptedAttemptID set, got %q", updatedSession.AcceptedAttemptID)
	}
	// The session stays active after acceptance for continued refinement
	// (RP4E2 plan invariant) — Use never terminates the session.
	if updatedSession.Status != SpatialDesignSessionStatusActive {
		t.Fatalf("expected session to remain active after acceptance, got %s", updatedSession.Status)
	}
}

func TestUseDesignPlan_RejectsAbandonedAttempt(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())
	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_11", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if _, err := svc.CancelDesignGenerationAttempt(context.Background(), "company_1", attempt.ID, "cancel_2"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	_, _, _, err = svc.UseDesignPlan(context.Background(), "company_1", "user_1", session.ID, attempt.ID, UseDesignPlanInput{
		ClientRequestID: "use_2", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if !errors.Is(err, ErrDesignAttemptAbandoned) {
		t.Fatalf("expected ErrDesignAttemptAbandoned, got %v", err)
	}
}

func TestUseDesignPlan_ReplaysSameRequest(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())
	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_12", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	input := UseDesignPlanInput{ClientRequestID: "use_3", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision}

	first, _, _, err := svc.UseDesignPlan(context.Background(), "company_1", "user_1", session.ID, attempt.ID, input)
	if err != nil {
		t.Fatalf("first use: %v", err)
	}
	second, _, replayed, err := svc.UseDesignPlan(context.Background(), "company_1", "user_1", session.ID, attempt.ID, input)
	if err != nil {
		t.Fatalf("replay use: %v", err)
	}
	if !replayed {
		t.Fatal("expected second identical use to be a replay")
	}
	if first.ID != second.ID {
		t.Fatalf("expected replay to return the SAME acceptance, got %s vs %s", first.ID, second.ID)
	}
}

func TestUseDesignPlan_RejectsNonLatestReadyAttempt(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)

	session, turn := setupReadyTurn(t, designRepo, roomDrafts, materialOnlyPlan())
	firstAttempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_13", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	refreshedSession, _ := designRepo.FindSession(context.Background(), "company_1", session.ID)
	if _, _, err := svc.RegenerateDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, RegenerateDesignPlanInput{
		ClientRequestID: "regen_3", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: refreshedSession.BasedOnRoomDraftRevision,
	}); err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	// Using the OLDER (now-superseded) attempt must fail — only the
	// session's LatestReadyAttemptID is eligible for Use.
	_, _, _, err = svc.UseDesignPlan(context.Background(), "company_1", "user_1", session.ID, firstAttempt.ID, UseDesignPlanInput{
		ClientRequestID: "use_4", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: refreshedSession.BasedOnRoomDraftRevision,
	})
	if !errors.Is(err, ErrDesignAttemptNotLatestReady) {
		t.Fatalf("expected ErrDesignAttemptNotLatestReady, got %v", err)
	}
}
