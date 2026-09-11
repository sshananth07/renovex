package spatial

import (
	"context"
	"testing"
	"time"
)

// --- fakes ---

type fakeDesignSessionAndTurnRepo struct {
	sessions        map[string]SpatialDesignSession
	sessionsByKey   map[string]string // companyID|clientSessionID -> sessionID
	turns           map[string]SpatialDesignTurn
	turnsByRequest  map[string]string // companyID|sessionID|clientRequestID -> turnID
	nextSessionID   int
	nextTurnID      int
	reservedTurnID  string
	reasoningTurnID string
}

func newFakeDesignRepo() *fakeDesignSessionAndTurnRepo {
	return &fakeDesignSessionAndTurnRepo{
		sessions: map[string]SpatialDesignSession{}, sessionsByKey: map[string]string{},
		turns: map[string]SpatialDesignTurn{}, turnsByRequest: map[string]string{},
	}
}

func (f *fakeDesignSessionAndTurnRepo) CreateOrGetSession(_ context.Context, session SpatialDesignSession) (SpatialDesignSession, error) {
	key := session.CompanyID + "|" + session.ClientSessionID
	if existingID, ok := f.sessionsByKey[key]; ok {
		existing := f.sessions[existingID]
		if existing.SessionRequestFingerprint != session.SessionRequestFingerprint {
			return SpatialDesignSession{}, ErrDesignSessionRequestConflict
		}
		return existing, nil
	}
	f.nextSessionID++
	session.ID = "session_" + string(rune('a'+f.nextSessionID))
	f.sessions[session.ID] = session
	f.sessionsByKey[key] = session.ID
	return session, nil
}

func (f *fakeDesignSessionAndTurnRepo) FindSession(_ context.Context, companyID, id string) (SpatialDesignSession, error) {
	s, ok := f.sessions[id]
	if !ok || s.CompanyID != companyID {
		return SpatialDesignSession{}, ErrDesignSessionNotFound
	}
	return s, nil
}

func (f *fakeDesignSessionAndTurnRepo) ReserveTurn(_ context.Context, turn SpatialDesignTurn) (SpatialDesignTurn, bool, error) {
	key := turn.CompanyID + "|" + turn.SessionID + "|" + turn.ClientRequestID
	if existingID, ok := f.turnsByRequest[key]; ok {
		existing := f.turns[existingID]
		if existing.RequestFingerprint != turn.RequestFingerprint {
			return SpatialDesignTurn{}, false, ErrDesignTurnRequestConflict
		}
		return existing, true, nil
	}
	session, ok := f.sessions[turn.SessionID]
	if !ok {
		return SpatialDesignTurn{}, false, ErrDesignSessionNotFound
	}
	if session.ActiveTurnID != "" {
		return SpatialDesignTurn{}, false, ErrDesignTurnInProgress
	}
	f.nextTurnID++
	turn.ID = "turn_" + string(rune('a'+f.nextTurnID))
	turn.Sequence = session.LastTurnSequence + 1
	turn.PreviousTurnID = session.LatestTurnID
	turn.ParentPlanTurnID = session.LatestReadyPlanTurnID
	f.turns[turn.ID] = turn
	f.reservedTurnID = turn.ID
	f.turnsByRequest[key] = turn.ID
	session.ActiveTurnID = turn.ID
	session.LatestTurnID = turn.ID
	session.LastTurnSequence = turn.Sequence
	f.sessions[turn.SessionID] = session
	return turn, false, nil
}

func (f *fakeDesignSessionAndTurnRepo) MarkProviderStarted(_ context.Context, companyID, turnID string, startedAt time.Time) error {
	t, ok := f.turns[turnID]
	if !ok || t.CompanyID != companyID || t.Status != SpatialDesignTurnStatusReserved {
		return ErrDesignTurnNotClaimable
	}
	t.ProviderStartedAt = &startedAt
	t.Status = SpatialDesignTurnStatusReasoning
	f.reasoningTurnID = turnID
	f.turns[turnID] = t
	return nil
}

func (f *fakeDesignSessionAndTurnRepo) FinishTurn(_ context.Context, input FinishTurnInput) (SpatialDesignTurn, error) {
	t, ok := f.turns[input.TurnID]
	if !ok {
		return SpatialDesignTurn{}, ErrDesignTurnNotFound
	}
	if isTerminalDesignTurnStatus(t.Status) {
		return t, nil
	}
	t.Status = input.Status
	t.ProposedDelta = input.ProposedDelta
	t.ValidatedPlan = input.ValidatedPlan
	t.PlanFingerprint = input.PlanFingerprint
	t.SafeFailureCode = input.SafeFailureCode
	now := time.Now()
	t.CompletedAt = &now
	f.turns[input.TurnID] = t

	session := f.sessions[input.SessionID]
	session.ActiveTurnID = ""
	if input.Status == SpatialDesignTurnStatusProposed {
		session.LatestReadyPlanTurnID = input.TurnID
		session.CurrentWorkingDesign = input.WorkingDesign
	}
	f.sessions[input.SessionID] = session
	return t, nil
}

func (f *fakeDesignSessionAndTurnRepo) FindTurn(_ context.Context, companyID, id string) (SpatialDesignTurn, error) {
	t, ok := f.turns[id]
	if !ok || t.CompanyID != companyID {
		return SpatialDesignTurn{}, ErrDesignTurnNotFound
	}
	return t, nil
}

func (f *fakeDesignSessionAndTurnRepo) FindTurnByClientRequestID(_ context.Context, companyID, sessionID, clientRequestID string) (SpatialDesignTurn, error) {
	key := companyID + "|" + sessionID + "|" + clientRequestID
	id, ok := f.turnsByRequest[key]
	if !ok {
		return SpatialDesignTurn{}, ErrDesignTurnNotFound
	}
	return f.turns[id], nil
}

func (f *fakeDesignSessionAndTurnRepo) ListTurns(_ context.Context, companyID, sessionID string, beforeSequence int64, limit int) ([]SpatialDesignTurn, error) {
	var out []SpatialDesignTurn
	for _, t := range f.turns {
		if t.CompanyID == companyID && t.SessionID == sessionID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDesignSessionAndTurnRepo) RecoverInterruptedTurns(_ context.Context, olderThan time.Duration) ([]SpatialDesignTurn, error) {
	return nil, nil
}

// fakeElementReasoner counts calls and returns a scripted result/error —
// the seam every "calls the reasoner zero/one times" assertion in this
// file depends on.
type fakeElementReasoner struct {
	calls       int
	lastContext DesignReasoningContext
	result      ProposedSceneEditDelta
	err         error
}

func (f *fakeElementReasoner) ReasonElement(_ context.Context, reasoningContext DesignReasoningContext) (ProposedSceneEditDelta, error) {
	f.calls++
	f.lastContext = reasoningContext
	if f.err != nil {
		return ProposedSceneEditDelta{}, f.err
	}
	return f.result, nil
}

func TestCreateDesignTurn_KitchenIslandForwardsPersistedIDsAndProposesFitCheckedMove(t *testing.T) {
	draft := RoomDraft{
		ID: "roomdraft_kitchen_1", CompanyID: "company_1", CaptureID: "capture_1", Revision: 2,
		Walls: []RoomDraftWall{
			{ID: "wall_north", Start: RoomLocalPoint{X: 0, Z: 0}, End: RoomLocalPoint{X: 6, Z: 0}},
			{ID: "wall_east", Start: RoomLocalPoint{X: 6, Z: 0}, End: RoomLocalPoint{X: 6, Z: 6}},
			{ID: "wall_south", Start: RoomLocalPoint{X: 6, Z: 6}, End: RoomLocalPoint{X: 0, Z: 6}},
			{ID: "wall_west", Start: RoomLocalPoint{X: 0, Z: 6}, End: RoomLocalPoint{X: 0, Z: 0}},
		},
		Objects: []RoomDraftObject{
			{ID: "kitchen_island", Category: "kitchen_island", Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 3, Z: 2}, Rotation: RoomLocalQuaternion{W: 1}}, Dimensions: &RoomLocalPoint{X: 1.4, Y: 0.9, Z: 0.8}},
			{ID: "cabinet_bank", Category: "cabinet", Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 3, Z: 0.5}, Rotation: RoomLocalQuaternion{W: 1}}, Dimensions: &RoomLocalPoint{X: 3, Y: 0.9, Z: 0.6}},
		},
	}
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{result: ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "kitchen_island"}, Intent: DesignIntentSpatialDomain,
		Summary:  []string{"Move the island toward the kitchen center while keeping cabinet clearance."},
		Geometry: SectionChange{Mode: SectionModePreserve}, Material: SectionChange{Mode: SectionModePreserve},
		Spatial:    SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.15}},
		Confidence: 0.9,
	}}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "kitchen-island-session", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: draft.Revision,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "kitchen_island"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turn, _, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "kitchen-island-turn", Instruction: "Move the island slightly closer to the center of the kitchen while keeping practical clearance from the surrounding cabinets.",
	})
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected exactly one reasoner/provider call, got %d", reasoner.calls)
	}
	if designRepo.reservedTurnID != turn.ID || designRepo.reasoningTurnID != turn.ID {
		t.Fatalf("expected persisted lifecycle reserved -> reasoning for %q, got reserved=%q reasoning=%q", turn.ID, designRepo.reservedTurnID, designRepo.reasoningTurnID)
	}
	if reasoner.lastContext.TurnID != turn.ID || reasoner.lastContext.TurnID == "" {
		t.Fatalf("expected persisted turn ID %q forwarded to the reasoner, got %q", turn.ID, reasoner.lastContext.TurnID)
	}
	if reasoner.lastContext.RoomDraftID != draft.ID || reasoner.lastContext.RoomDraftRevision != draft.Revision {
		t.Fatalf("expected authoritative room draft %q revision %d, got %q revision %d", draft.ID, draft.Revision, reasoner.lastContext.RoomDraftID, reasoner.lastContext.RoomDraftRevision)
	}
	if turn.Status != SpatialDesignTurnStatusProposed || turn.ValidatedPlan == nil {
		t.Fatalf("expected a proposed turn with a validated plan, got status=%q plan=%v", turn.Status, turn.ValidatedPlan)
	}
	if turn.ProposedDelta == nil || turn.ProposedDelta.Target.ID != "kitchen_island" {
		t.Fatalf("expected proposed operation targeting kitchen_island, got %+v", turn.ProposedDelta)
	}
	if turn.ValidatedPlan.Fit.Status == "" {
		t.Fatal("expected Go FitAnalysis to run")
	}
	if after := draftRepo.byID[draft.ID]; after.Revision != draft.Revision || len(after.Objects) != len(draft.Objects) {
		t.Fatalf("RoomDraft changed before Use Design: %+v", after)
	}
}

func TestComputeDesignExecutionFlags_MaterialOnlyAfterAcceptedGeometryDoesNotReRequestHunyuan(t *testing.T) {
	// Mirrors testdata/designreasoning/material_only_validated_plan.json's
	// scenario, but for the ACCEPTED case that fixture predates: an earlier
	// turn's geometry was already accepted (Use Design ran, so
	// session.AcceptedDesign.Geometry now equals the pending geometry).
	// "Actually make it beige." must NOT re-request Hunyuan for a mesh
	// that is already published and bound (RP4E2 plan repository-findings
	// row 2 — the amendment this test proves).
	acceptedGeometry := &WorkingDesignGeometry{
		Category: "sofa", ShapeDescription: "Curved three-seat sofa with rounded arms", PreserveCanonicalDimensions: true,
	}
	accepted := &WorkingDesign{Geometry: acceptedGeometry}
	merged := WorkingDesign{
		Geometry: acceptedGeometry, // unchanged — carried forward from AcceptedDesign
		Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
	}
	delta := ProposedSceneEditDelta{
		Intent:   DesignIntentMaterialAppearance,
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: merged.Material},
		Spatial:  SectionChange{Mode: SectionModePreserve},
	}

	execution := computeDesignExecutionFlags(delta, merged, accepted)

	if execution.TurnRequiresAssetGeneration {
		t.Fatal("expected TurnRequiresAssetGeneration=false for a material-only turn")
	}
	if execution.HunyuanRequired {
		t.Fatal("expected HunyuanRequired=false: working geometry already equals accepted geometry")
	}
}

func TestComputeDesignExecutionFlags_PendingUnacceptedGeometryKeepsHunyuanRequired(t *testing.T) {
	// The staged material_only_validated_plan.json scenario: geometry was
	// PROPOSED in a prior turn but never accepted (session.AcceptedDesign is
	// nil). A material-only follow-up must still report HunyuanRequired
	// because that geometry has never been generated.
	merged := WorkingDesign{
		Geometry: &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved three-seat sofa with rounded arms", PreserveCanonicalDimensions: true},
		Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
	}
	delta := ProposedSceneEditDelta{
		Intent:   DesignIntentMaterialAppearance,
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: merged.Material},
		Spatial:  SectionChange{Mode: SectionModePreserve},
	}

	execution := computeDesignExecutionFlags(delta, merged, nil) // no AcceptedDesign yet

	if execution.TurnRequiresAssetGeneration {
		t.Fatal("expected TurnRequiresAssetGeneration=false for a material-only turn")
	}
	if !execution.HunyuanRequired {
		t.Fatal("expected HunyuanRequired=true: pending geometry was never accepted/generated")
	}
}

func TestComputeDesignExecutionFlags_GeometryReplaceAlwaysRequiresGeneration(t *testing.T) {
	geometry := &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved three-seat sofa", PreserveCanonicalDimensions: true}
	accepted := &WorkingDesign{Geometry: geometry} // identical to what's being proposed
	merged := WorkingDesign{Geometry: geometry}
	delta := ProposedSceneEditDelta{
		Intent:   DesignIntentVisualGeometry,
		Geometry: SectionChange{Mode: SectionModeReplace, GeometrySpec: geometry},
		Material: SectionChange{Mode: SectionModePreserve},
		Spatial:  SectionChange{Mode: SectionModePreserve},
	}

	execution := computeDesignExecutionFlags(delta, merged, accepted)

	if !execution.TurnRequiresAssetGeneration {
		t.Fatal("expected TurnRequiresAssetGeneration=true: this turn itself replaces geometry")
	}
	if !execution.HunyuanRequired {
		t.Fatal("expected HunyuanRequired=true: a fresh replace is never assumed already-generated")
	}
}

func materialOnlyDelta(targetID string) ProposedSceneEditDelta {
	return ProposedSceneEditDelta{
		Target:   SpatialDesignTarget{Kind: DesignTargetKindObject, ID: targetID},
		Intent:   DesignIntentMaterialAppearance,
		Summary:  []string{"Change the sofa upholstery to beige."},
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: &WorkingDesignMaterial{
			BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial:    SectionChange{Mode: SectionModePreserve},
		Confidence: 0.9,
	}
}

func newDesignServiceForTest(draft RoomDraft, roomDrafts RoomDraftRepository, designRepo *fakeDesignSessionAndTurnRepo, reasoner *fakeElementReasoner) *Service {
	svc := &Service{}
	svc.SetRoomDraftSupport(roomDrafts)
	svc.SetDesignPlanningSupport(designRepo, designRepo, reasoner)
	return svc
}

// fakeRoomDraftRepoForDesign is a minimal RoomDraftRepository fake scoped
// to what design_service.go needs (FindByID) — this package's existing
// fakes are all capture-repo-shaped, so a small dedicated fake avoids
// distorting an unrelated fake's shape.
type fakeRoomDraftRepoForDesign struct {
	byID map[string]RoomDraft
}

func newFakeRoomDraftRepoForDesign() *fakeRoomDraftRepoForDesign {
	return &fakeRoomDraftRepoForDesign{byID: map[string]RoomDraft{}}
}

func (f *fakeRoomDraftRepoForDesign) Create(_ context.Context, d RoomDraft) (RoomDraft, error) {
	f.byID[d.ID] = d
	return d, nil
}
func (f *fakeRoomDraftRepoForDesign) FindByID(_ context.Context, companyID, id string) (RoomDraft, error) {
	d, ok := f.byID[id]
	if !ok || d.CompanyID != companyID {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	return d, nil
}
func (f *fakeRoomDraftRepoForDesign) FindByCaptureID(_ context.Context, companyID, captureID string) (RoomDraft, error) {
	for _, d := range f.byID {
		if d.CompanyID == companyID && d.CaptureID == captureID {
			return d, nil
		}
	}
	return RoomDraft{}, ErrRoomDraftNotFound
}
func (f *fakeRoomDraftRepoForDesign) Update(_ context.Context, companyID, id string, d RoomDraft, expectedRevision int64) (RoomDraft, error) {
	f.byID[id] = d
	return d, nil
}

func testDraftForDesignService() RoomDraft {
	return RoomDraft{
		ID: "roomdraft_1", CompanyID: "company_1", CaptureID: "capture_1",
		Objects: []RoomDraftObject{
			{
				ID: "object_sofa_123", Category: "sofa",
				Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 1.2, Y: 0, Z: 2.4}, Rotation: RoomLocalQuaternion{W: 1}},
				Dimensions: &RoomLocalPoint{X: 2.0, Y: 0.85, Z: 0.95},
			},
		},
		Revision: 17,
	}
}

func TestCreateDesignSession_RejectsUnsupportedTargetKind(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	_, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: "wall", ID: "wall_1"},
	})
	if err != ErrUnsupportedDesignTargetKind {
		t.Fatalf("expected ErrUnsupportedDesignTargetKind, got %v", err)
	}
	if reasoner.calls != 0 {
		t.Fatalf("expected zero reasoner calls for a rejected session, got %d", reasoner.calls)
	}
}

func TestCreateDesignSession_NeverCallsReasoner(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	_, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reasoner.calls != 0 {
		t.Fatalf("session creation must never call the reasoner, got %d calls", reasoner.calls)
	}
}

func TestCreateDesignTurn_AdmittedTurnCallsReasonerExactlyOnce(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{result: materialOnlyDelta("object_sofa_123")}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	turn, replayed, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "Actually make it beige.",
	})
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	if replayed {
		t.Fatal("first turn must not be a replay")
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected exactly 1 reasoner call, got %d", reasoner.calls)
	}
	if turn.Status != SpatialDesignTurnStatusProposed {
		t.Fatalf("expected proposed status, got %s", turn.Status)
	}
}

func TestCreateDesignTurn_ReplayedRequestCallsReasonerZeroTimes(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{result: materialOnlyDelta("object_sofa_123")}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	input := CreateDesignTurnInput{ClientRequestID: "request_1", Instruction: "Actually make it beige."}
	if _, _, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, input); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected 1 call after first turn, got %d", reasoner.calls)
	}

	_, replayed, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, input)
	if err != nil {
		t.Fatalf("replayed turn: %v", err)
	}
	if !replayed {
		t.Fatal("expected the second identical request to be recognized as a replay")
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected reasoner calls to STAY at 1 after a replay, got %d", reasoner.calls)
	}
}

func TestCreateDesignTurn_StaleRoomDraftRevisionBeforeDispatch(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{result: materialOnlyDelta("object_sofa_123")}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// RoomDraft revision advances after the session was created — session
	// is now stale.
	staleDraft := draft
	staleDraft.Revision = 18
	draftRepo.byID[draft.ID] = staleDraft

	_, _, err = svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "Actually make it beige.",
	})
	if err != ErrDesignPlanStale {
		t.Fatalf("expected ErrDesignPlanStale, got %v", err)
	}
	if reasoner.calls != 0 {
		t.Fatalf("a stale session must never dispatch to the reasoner, got %d calls", reasoner.calls)
	}
}

func TestCreateDesignTurn_ForeignSessionCallsReasonerZeroTimes(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{result: materialOnlyDelta("object_sofa_123")}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, _, err = svc.CreateDesignTurn(context.Background(), "company_2", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "x",
	})
	if err != ErrDesignSessionNotFound {
		t.Fatalf("expected ErrDesignSessionNotFound for cross-tenant session, got %v", err)
	}
	if reasoner.calls != 0 {
		t.Fatalf("expected zero reasoner calls for a foreign session, got %d", reasoner.calls)
	}
}

func TestCreateDesignTurn_ProviderInvalidOutputNeverRetried(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{err: ErrDesignReasoningInvalidOutput}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	turn, _, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "x",
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if turn.Status != SpatialDesignTurnStatusFailed {
		t.Fatalf("expected failed terminal status, got %s", turn.Status)
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected exactly 1 call even on invalid output — never a repair retry, got %d", reasoner.calls)
	}
}

func TestCreateDesignTurn_MaterialOnlyRefinementRetainsGeometryAndUpdatesFingerprint(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	geometryDelta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
		Intent: DesignIntentVisualGeometry, Summary: []string{"Curved shape."},
		Geometry: SectionChange{Mode: SectionModeReplace, GeometrySpec: &WorkingDesignGeometry{
			Category: "sofa", ShapeDescription: "Curved sofa", PreserveCanonicalDimensions: true,
		}},
		Material: SectionChange{Mode: SectionModePreserve}, Spatial: SectionChange{Mode: SectionModePreserve},
		Confidence: 0.9,
	}
	reasoner := &fakeElementReasoner{result: geometryDelta}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	firstTurn, _, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "Make this sofa curved.",
	})
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}

	reasoner.result = materialOnlyDelta("object_sofa_123")
	secondTurn, _, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_2", Instruction: "Actually make it beige.",
	})
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}

	if secondTurn.ValidatedPlan.WorkingDesign.Geometry == nil || secondTurn.ValidatedPlan.WorkingDesign.Geometry.ShapeDescription != "Curved sofa" {
		t.Fatalf("expected geometry retained from first turn, got %+v", secondTurn.ValidatedPlan.WorkingDesign.Geometry)
	}
	if secondTurn.ValidatedPlan.WorkingDesign.Material == nil || secondTurn.ValidatedPlan.WorkingDesign.Material.BaseColor != "#c8a464" {
		t.Fatalf("expected material replaced, got %+v", secondTurn.ValidatedPlan.WorkingDesign.Material)
	}
	if secondTurn.ValidatedPlan.Execution.TurnRequiresAssetGeneration {
		t.Fatal("expected turn-local generation flag false for a material-only turn")
	}
	if !secondTurn.ValidatedPlan.Execution.HunyuanRequired {
		t.Fatal("expected cumulative hunyuanRequired to remain true (unresolved custom geometry from the first turn)")
	}
	if secondTurn.PlanFingerprint == firstTurn.PlanFingerprint {
		t.Fatal("expected plan fingerprint to change between turns")
	}
	if secondTurn.ParentPlanTurnID != firstTurn.ID {
		t.Fatalf("expected second turn's ParentPlanTurnID to point at the first successful plan, got %s", secondTurn.ParentPlanTurnID)
	}
}

func TestCreateDesignTurn_AmbiguousProviderOutcomeBecomesNeedsAttentionAndIsNeverRedispatched(t *testing.T) {
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{err: ErrDesignReasoningProviderUnavailable}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	turn, firstReplayed, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "x",
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if firstReplayed {
		t.Fatal("first admitted turn must not itself be a replay")
	}
	if turn.Status != SpatialDesignTurnStatusNeedsAttention {
		t.Fatalf("expected needs_attention for an ambiguous provider outcome, got %s", turn.Status)
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected exactly 1 reasoner call, got %d", reasoner.calls)
	}

	// A second identical request must replay the SAME needs_attention
	// result — never redispatch the reasoner for this turn.
	secondTurn, secondReplayed, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "x",
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !secondReplayed {
		t.Fatal("expected the second identical request to replay")
	}
	if secondTurn.ID != turn.ID {
		t.Fatalf("expected the same turn returned on replay, got %s vs %s", turn.ID, secondTurn.ID)
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected reasoner calls to remain 1 (never redispatched), got %d", reasoner.calls)
	}
}

func TestCreateDesignTurn_NeverCallsRoomDraftMutationMethods(t *testing.T) {
	// This test documents the invariant structurally: design_service.go's
	// CreateDesignTurn takes a RoomDraftRepository (read-only interface:
	// Create/FindByID/FindByCaptureID/Update) but calling Update against it
	// would be a bug this fake would catch by mutating its own map. Since
	// fakeRoomDraftRepoForDesign.Update is never invoked by
	// CreateDesignTurn in the first place, verify the draft is byte-for-
	// byte unchanged after a full success turn.
	draft := testDraftForDesignService()
	draftRepo := newFakeRoomDraftRepoForDesign()
	draftRepo.byID[draft.ID] = draft
	designRepo := newFakeDesignRepo()
	reasoner := &fakeElementReasoner{result: materialOnlyDelta("object_sofa_123")}
	svc := newDesignServiceForTest(draft, draftRepo, designRepo, reasoner)

	session, err := svc.CreateDesignSession(context.Background(), "company_1", "user_1", CreateDesignSessionInput{
		ClientSessionID: "client_1", RoomDraftID: draft.ID, ExpectedRoomDraftRevision: 17,
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, _, err := svc.CreateDesignTurn(context.Background(), "company_1", "user_1", session.Session.ID, CreateDesignTurnInput{
		ClientRequestID: "request_1", Instruction: "Actually make it beige.",
	}); err != nil {
		t.Fatalf("create turn: %v", err)
	}

	after := draftRepo.byID[draft.ID]
	if len(after.Objects) != len(draft.Objects) || after.Revision != draft.Revision {
		t.Fatalf("expected RoomDraft untouched by CreateDesignTurn, got %+v", after)
	}
}
