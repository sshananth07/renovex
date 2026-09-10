package spatial

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// mustMarshalPayload/mustMarshalOperation encode an already-validated,
// server-constructed value to canonical wire JSON for audit storage on a
// RoomDraftEditRecord — these values are always server-built from typed
// Go structs (never raw client input), so a marshal failure here would be a
// programming error, matching this package's existing "server-authored
// data cannot fail to encode" assumption (see toDesignConceptDoc and
// siblings, which do not propagate json errors either).
func mustMarshalPayload(payload map[string]any) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic("spatial: server-constructed operation payload failed to marshal: " + err.Error())
	}
	return string(encoded)
}

func mustMarshalOperation(op any) string {
	encoded, err := json.Marshal(op)
	if err != nil {
		panic("spatial: server-constructed operation failed to marshal: " + err.Error())
	}
	return string(encoded)
}

// SetDesignGenerationSupport wires the RP4E2 attempt/acceptance
// repositories into Service after construction — same optional setter
// pattern as SetDesignPlanningSupport/SetAssetGenerationSupport.
func (s *Service) SetDesignGenerationSupport(attempts DesignGenerationAttemptRepository, acceptances DesignAcceptanceRepository, applier DesignAcceptanceApplier) {
	s.designGenerationAttempts = attempts
	s.designAcceptances = acceptances
	s.designAcceptanceApplier = applier
}

// ErrDesignGenerationSupportNotConfigured is returned when a Confirm/
// Regenerate/Cancel/Use/List/Get call is made before
// SetDesignGenerationSupport has been called.
var ErrDesignGenerationSupportNotConfigured = errors.New("spatial: design generation support not configured")

// ConfirmDesignPlanInput is Confirm's request shape.
type ConfirmDesignPlanInput struct {
	ClientRequestID           string
	PlanFingerprint           string
	ExpectedRoomDraftRevision int64
}

// classifyGenerationKind derives DesignGenerationKind from a validated
// plan's execution flags — material_only never touches Python/Hunyuan;
// geometry/mixed both do (RP4E2 plan).
func classifyGenerationKind(plan ValidatedSceneEditPlan) DesignGenerationKind {
	needsGeometry := plan.Execution.TurnRequiresAssetGeneration
	needsMaterial := plan.WorkingDesign.Material != nil
	switch {
	case needsGeometry && needsMaterial:
		return DesignGenerationKindMixed
	case needsGeometry:
		return DesignGenerationKindGeometry
	default:
		return DesignGenerationKindMaterialOnly
	}
}

// buildMaterialOnlyConcept constructs the DesignConcept for an attempt that
// requires no Python/Hunyuan work — geometry is preserved (VisualAction
// stays "preserve": nothing new was generated so there is nothing to bind
// or clear), appearance is set from the plan's material section, and
// placement carries forward the target's current transform/dimensions
// (material-only plans never populate ResolvedSpatialOperations for a
// SPATIAL change without ALSO going through the geometry/mixed path, but a
// plan CAN still combine material with an already-resolved spatial
// operation — the transform below reflects the resolved spatial delta if
// present, falling back to the pinned target snapshot otherwise).
func buildMaterialOnlyConcept(target SpatialDesignTarget, targetSnapshot AuthorizedDesignTarget, plan ValidatedSceneEditPlan) DesignConcept {
	concept := DesignConcept{
		Target:           target,
		Transform:        targetSnapshot.Transform,
		Dimensions:       targetSnapshot.Dimensions,
		VisualAction:     ConceptBindingActionPreserve,
		AppearanceAction: ConceptAppearanceActionPreserve,
	}
	if plan.WorkingDesign.Material != nil {
		concept.AppearanceAction = ConceptAppearanceActionSet
		appearance := VisualAppearance{
			BaseColor: plan.WorkingDesign.Material.BaseColor, MaterialFamily: MaterialFamily(plan.WorkingDesign.Material.MaterialFamily),
			Roughness: Roughness(plan.WorkingDesign.Material.Roughness), Metallic: plan.WorkingDesign.Material.Metallic,
		}
		concept.Appearance = &appearance
	}
	return concept
}

// ConfirmDesignPlan is RP4E2's Confirm entry point (POST .../turns/{turnId}/confirm).
// It implements the plan's exact required ordering: replay lookup by
// request ID first, then tenant/turn/fingerprint/revision validation, then
// either a direct material-only completion or a reserved geometry/mixed
// attempt (Gate 2 wires the actual reference/Hunyuan submission onto the
// reserved attempt; this gate only durably records it).
func (s *Service) ConfirmDesignPlan(ctx context.Context, companyID, userID, sessionID, turnID string, input ConfirmDesignPlanInput) (DesignGenerationAttempt, bool, error) {
	if s.designGenerationAttempts == nil {
		return DesignGenerationAttempt{}, false, ErrDesignGenerationSupportNotConfigured
	}

	// 1. Replay lookup by request ID FIRST.
	if existing, err := s.designGenerationAttempts.FindAttemptByClientRequestID(ctx, companyID, sessionID, input.ClientRequestID); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, ErrDesignGenerationAttemptNotFound) {
		return DesignGenerationAttempt{}, false, err
	}

	// 2. Tenant/session/turn/fingerprint/revision validation.
	session, err := s.designSessions.FindSession(ctx, companyID, sessionID)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if session.LatestReadyPlanTurnID != turnID {
		return DesignGenerationAttempt{}, false, ErrDesignAttemptNotLatestReady
	}
	turn, err := s.designTurns.FindTurn(ctx, companyID, turnID)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if turn.Status != SpatialDesignTurnStatusProposed || turn.ValidatedPlan == nil {
		return DesignGenerationAttempt{}, false, ErrDesignAttemptNotReady
	}
	if turn.PlanFingerprint != input.PlanFingerprint {
		return DesignGenerationAttempt{}, false, ErrDesignPlanStale
	}
	draft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if draft.Revision != input.ExpectedRoomDraftRevision || draft.Revision != session.BasedOnRoomDraftRevision {
		return DesignGenerationAttempt{}, false, ErrDesignPlanStale
	}
	target, err := resolveDesignTarget(draft, session.Target)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}

	kind := classifyGenerationKind(*turn.ValidatedPlan)
	now := time.Now()
	attemptNumber := turn.Sequence // one attempt-number sequence per turn is sufficient since only ONE turn may be active at a time; Regenerate increments explicitly (see RegenerateDesignPlan)

	reservation := DesignGenerationAttempt{
		CompanyID: companyID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, RoomDraftID: session.RoomDraftID,
		SessionID: sessionID, TurnID: turnID, PlanFingerprint: input.PlanFingerprint, AttemptNumber: attemptNumber,
		ClientRequestID: input.ClientRequestID, RequestFingerprint: confirmRequestFingerprint(sessionID, turnID, input.PlanFingerprint, input.ExpectedRoomDraftRevision),
		BasedOnRoomDraftRevision: input.ExpectedRoomDraftRevision,
		Kind:                     kind, Status: DesignGenerationStatusReserved, ActiveSlot: "active",
		TargetSnapshot:  target,
		CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	}

	reserved, wasReplay, err := s.designGenerationAttempts.ReserveAttempt(ctx, reservation)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if wasReplay {
		return reserved, true, nil
	}

	if kind != DesignGenerationKindMaterialOnly {
		// Geometry/mixed: durably reserved; Gate 2's worker (reference
		// generation + RP4E0 submission) and Gate 4's queue wake handle the
		// rest. Nothing further happens synchronously in this gate.
		s.notifyGenerationWake(ctx, GenerationWakeKindDesignAttempt, reserved.ID)
		return reserved, false, nil
	}

	// Material-only: complete synchronously, no Python/Hunyuan call.
	concept := buildMaterialOnlyConcept(session.Target, target, *turn.ValidatedPlan)
	completed, err := s.designGenerationAttempts.CompleteWithConcept(ctx, companyID, reserved.ID, concept)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	return completed, false, nil
}

// RegenerateDesignPlanInput mirrors ConfirmDesignPlanInput's shape (RP4E2
// plan: "Body is the same as Confirm").
type RegenerateDesignPlanInput struct {
	ClientRequestID           string
	PlanFingerprint           string
	ExpectedRoomDraftRevision int64
}

// RegenerateDesignPlan creates a new attempt number for the same turn's
// already-validated plan — it requires the previous attempt to be terminal
// (never abandons an active one implicitly), never calls CreateDesignTurn,
// and never invokes the reasoner. Same request ID replays the same new
// attempt (idempotent, matching Confirm's replay-first convention).
func (s *Service) RegenerateDesignPlan(ctx context.Context, companyID, userID, sessionID, turnID string, input RegenerateDesignPlanInput) (DesignGenerationAttempt, bool, error) {
	if s.designGenerationAttempts == nil {
		return DesignGenerationAttempt{}, false, ErrDesignGenerationSupportNotConfigured
	}

	if existing, err := s.designGenerationAttempts.FindAttemptByClientRequestID(ctx, companyID, sessionID, input.ClientRequestID); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, ErrDesignGenerationAttemptNotFound) {
		return DesignGenerationAttempt{}, false, err
	}

	session, err := s.designSessions.FindSession(ctx, companyID, sessionID)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if session.LatestReadyPlanTurnID != turnID {
		return DesignGenerationAttempt{}, false, ErrDesignAttemptNotLatestReady
	}
	turn, err := s.designTurns.FindTurn(ctx, companyID, turnID)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if turn.Status != SpatialDesignTurnStatusProposed || turn.ValidatedPlan == nil {
		return DesignGenerationAttempt{}, false, ErrDesignAttemptNotReady
	}
	if turn.PlanFingerprint != input.PlanFingerprint {
		return DesignGenerationAttempt{}, false, ErrDesignPlanStale
	}
	draft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if draft.Revision != input.ExpectedRoomDraftRevision || draft.Revision != session.BasedOnRoomDraftRevision {
		return DesignGenerationAttempt{}, false, ErrDesignPlanStale
	}
	target, err := resolveDesignTarget(draft, session.Target)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}

	// The previous attempt for this turn (if any) must be terminal — the
	// active-slot unique index would already reject a concurrent active
	// attempt at ReserveAttempt time, but checking here first produces a
	// clearer error than a raw duplicate-key failure.
	previous, err := s.designGenerationAttempts.ListAttempts(ctx, companyID, sessionID, turnID, 1)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	nextAttemptNumber := int64(1)
	if len(previous) > 0 {
		if !isTerminalDesignGenerationStatus(previous[0].Status) {
			return DesignGenerationAttempt{}, false, ErrDesignGenerationInProgress
		}
		nextAttemptNumber = previous[0].AttemptNumber + 1
	}

	kind := classifyGenerationKind(*turn.ValidatedPlan)
	now := time.Now()
	reservation := DesignGenerationAttempt{
		CompanyID: companyID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, RoomDraftID: session.RoomDraftID,
		SessionID: sessionID, TurnID: turnID, PlanFingerprint: input.PlanFingerprint, AttemptNumber: nextAttemptNumber,
		ClientRequestID: input.ClientRequestID, RequestFingerprint: regenerateRequestFingerprint(sessionID, turnID, input.PlanFingerprint, input.ExpectedRoomDraftRevision),
		BasedOnRoomDraftRevision: input.ExpectedRoomDraftRevision,
		Kind:                     kind, Status: DesignGenerationStatusReserved, ActiveSlot: "active",
		TargetSnapshot:  target,
		CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	}

	reserved, wasReplay, err := s.designGenerationAttempts.ReserveAttempt(ctx, reservation)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	if wasReplay {
		return reserved, true, nil
	}

	if kind != DesignGenerationKindMaterialOnly {
		s.notifyGenerationWake(ctx, GenerationWakeKindDesignAttempt, reserved.ID)
		return reserved, false, nil
	}
	concept := buildMaterialOnlyConcept(session.Target, target, *turn.ValidatedPlan)
	completed, err := s.designGenerationAttempts.CompleteWithConcept(ctx, companyID, reserved.ID, concept)
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	return completed, false, nil
}

// CancelDesignGenerationAttempt marks attemptID abandoned — idempotent,
// terminal abandonment always wins over any later provider result.
func (s *Service) CancelDesignGenerationAttempt(ctx context.Context, companyID, attemptID, clientRequestID string) (DesignGenerationAttempt, error) {
	if s.designGenerationAttempts == nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationSupportNotConfigured
	}
	fingerprint := cancelRequestFingerprint(attemptID, clientRequestID)
	return s.designGenerationAttempts.Cancel(ctx, companyID, attemptID, clientRequestID, fingerprint)
}

// GetDesignGenerationAttempt returns id's attempt, tenant-scoped.
func (s *Service) GetDesignGenerationAttempt(ctx context.Context, companyID, id string) (DesignGenerationAttempt, error) {
	if s.designGenerationAttempts == nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationSupportNotConfigured
	}
	return s.designGenerationAttempts.FindAttempt(ctx, companyID, id)
}

// ListDesignGenerationAttempts returns sessionID's attempts newest-first,
// optionally filtered to one turn.
func (s *Service) ListDesignGenerationAttempts(ctx context.Context, companyID, sessionID, turnID string, limit int) ([]DesignGenerationAttempt, error) {
	if s.designGenerationAttempts == nil {
		return nil, ErrDesignGenerationSupportNotConfigured
	}
	return s.designGenerationAttempts.ListAttempts(ctx, companyID, sessionID, turnID, limit)
}

// UseDesignPlanInput is Use's request shape.
type UseDesignPlanInput struct {
	ClientRequestID           string
	PlanFingerprint           string
	ExpectedRoomDraftRevision int64
}

// UseDesignPlan is RP4E2's Use entry point — the ONLY path that ever
// writes a design-session concept into the RoomDraft. It builds canonical
// operations in the plan's fixed order (resolved move/resize operations,
// then set/clear appearance, then assign_visual_asset for geometry last),
// applies the entire batch atomically via DesignAcceptanceApplier, and
// updates the session's accepted/working state.
func (s *Service) UseDesignPlan(ctx context.Context, companyID, userID, sessionID, attemptID string, input UseDesignPlanInput) (DesignAcceptance, RoomDraft, bool, error) {
	if s.designGenerationAttempts == nil || s.designAcceptances == nil || s.designAcceptanceApplier == nil {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignGenerationSupportNotConfigured
	}

	// 1. Replay lookup by request ID first.
	if existing, err := s.designAcceptances.FindAcceptanceByClientRequestID(ctx, companyID, sessionID, input.ClientRequestID); err == nil {
		draft, draftErr := s.roomDrafts.FindByID(ctx, companyID, existing.RoomDraftID)
		if draftErr != nil {
			return DesignAcceptance{}, RoomDraft{}, false, draftErr
		}
		return existing, draft, true, nil
	} else if !errors.Is(err, ErrDesignAcceptanceNotFound) {
		return DesignAcceptance{}, RoomDraft{}, false, err
	}

	session, err := s.designSessions.FindSession(ctx, companyID, sessionID)
	if err != nil {
		return DesignAcceptance{}, RoomDraft{}, false, err
	}
	attempt, err := s.designGenerationAttempts.FindAttempt(ctx, companyID, attemptID)
	if err != nil {
		return DesignAcceptance{}, RoomDraft{}, false, err
	}
	if attempt.SessionID != sessionID {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignGenerationAttemptNotFound
	}
	if attempt.Status == DesignGenerationStatusAbandoned {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignAttemptAbandoned
	}
	if attempt.Status != DesignGenerationStatusConceptReady {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignAttemptNotReady
	}
	if session.LatestReadyAttemptID != attemptID {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignAttemptNotLatestReady
	}
	if attempt.PlanFingerprint != input.PlanFingerprint {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignPlanStale
	}
	draft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return DesignAcceptance{}, RoomDraft{}, false, err
	}
	if draft.Revision != input.ExpectedRoomDraftRevision {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignPlanStale
	}
	turn, err := s.designTurns.FindTurn(ctx, companyID, attempt.TurnID)
	if err != nil {
		return DesignAcceptance{}, RoomDraft{}, false, err
	}
	if turn.ValidatedPlan == nil {
		return DesignAcceptance{}, RoomDraft{}, false, ErrDesignAttemptNotReady
	}

	operations := buildFixedOrderAcceptanceOperations(*turn.ValidatedPlan, attempt.Candidate)

	fingerprint := useRequestFingerprint(sessionID, attemptID, input.PlanFingerprint, input.ExpectedRoomDraftRevision)
	result, err := s.designAcceptanceApplier.ApplyAcceptance(ctx, ApplyAcceptanceInput{
		CompanyID: companyID, SessionID: sessionID, TurnID: attempt.TurnID, AttemptID: attemptID,
		PlanFingerprint: input.PlanFingerprint, ClientRequestID: input.ClientRequestID, RequestFingerprint: fingerprint,
		RoomDraftID: session.RoomDraftID, ExpectedRoomDraftRevision: input.ExpectedRoomDraftRevision,
		Operations:     operations,
		AcceptedDesign: turn.ValidatedPlan.WorkingDesign, ActorUserID: userID,
	})
	if err != nil {
		return DesignAcceptance{}, RoomDraft{}, false, err
	}
	return result.Acceptance, result.RoomDraft, result.Replayed, nil
}

// buildFixedOrderAcceptanceOperations builds Use Design's canonical
// operation batch in the plan's REQUIRED fixed order (RP4E2 plan): resolved
// move/resize operations first, then set_visual_appearance/
// clear_visual_appearance, then assign_visual_asset last (the only
// persistent generated-asset binding path). A geometry/mixed attempt's
// candidate.VisualAsset is only non-nil once Gate 2's RP4E0 job has
// published a result — Gate 1 exercises this function only for
// material_only attempts (VisualAction stays Preserve, so no
// assign/clear_visual_asset operation is added).
func buildFixedOrderAcceptanceOperations(plan ValidatedSceneEditPlan, candidate DesignConcept) []PendingCanonicalOperation {
	var operations []PendingCanonicalOperation

	for _, resolved := range plan.WorkingDesign.ResolvedSpatialOperations {
		op, ok := decodedResolvedOperation(resolved)
		if !ok {
			continue
		}
		operations = append(operations, PendingCanonicalOperation{
			Kind: resolved.Kind, Payload: mustMarshalPayload(resolved.Payload), Transform: op.Apply,
		})
	}

	targetKind := VisualAssetTargetObject
	if candidate.Target.Kind == DesignTargetKindFixture {
		targetKind = VisualAssetTargetFixture
	}

	switch candidate.AppearanceAction {
	case ConceptAppearanceActionSet:
		if candidate.Appearance != nil {
			setOp := SetVisualAppearanceOperation{TargetKind: targetKind, TargetID: candidate.Target.ID, Appearance: *candidate.Appearance}
			operations = append(operations, PendingCanonicalOperation{
				Kind: EditOpSetVisualAppearance, Payload: mustMarshalOperation(setOp), Transform: setOp.Apply,
			})
		}
	case ConceptAppearanceActionClear:
		clearOp := ClearVisualAppearanceOperation{TargetKind: targetKind, TargetID: candidate.Target.ID}
		operations = append(operations, PendingCanonicalOperation{
			Kind: EditOpClearVisualAppearance, Payload: mustMarshalOperation(clearOp), Transform: clearOp.Apply,
		})
	}

	switch candidate.VisualAction {
	case ConceptBindingActionAssign:
		if candidate.VisualAsset != nil {
			assignOp := AssignVisualAssetOperation{TargetKind: targetKind, TargetID: candidate.Target.ID, AssetID: candidate.VisualAsset.AssetID, Version: candidate.VisualAsset.Version}
			operations = append(operations, PendingCanonicalOperation{
				Kind: EditOpAssignVisualAsset, Payload: mustMarshalOperation(assignOp), Transform: assignOp.Apply,
			})
		}
	case ConceptBindingActionClear:
		clearOp := ClearVisualAssetOperation{TargetKind: targetKind, TargetID: candidate.Target.ID}
		operations = append(operations, PendingCanonicalOperation{
			Kind: EditOpClearVisualAsset, Payload: mustMarshalOperation(clearOp), Transform: clearOp.Apply,
		})
	}

	return operations
}

// decodedResolvedOperation decodes a ResolvedSpatialOperation's stored
// payload back into the concrete EditOperation whose Apply the acceptance
// transaction runs — proven to round-trip correctly by
// designgeometry_test.go's dedicated wire-contract tests.
func decodedResolvedOperation(resolved ResolvedSpatialOperation) (EditOperation, bool) {
	encoded, err := json.Marshal(resolved.Payload)
	if err != nil {
		return nil, false
	}
	op, err := decodeEditOperation(resolved.Kind, encoded)
	if err != nil {
		return nil, false
	}
	return op, true
}
