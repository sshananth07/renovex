package spatial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrDesignPlanStale is returned when a session's pinned
// BasedOnRoomDraftRevision no longer matches the RoomDraft's current
// revision — the session remains readable but cannot accept new turns
// (RP4E1 design amendment §1).
var ErrDesignPlanStale = errors.New("spatial: room draft revision changed since this design session was created")

// ErrDesignPlanTargetNotFound mirrors the plan's design_plan_target_not_found
// public code — a session's pinned target no longer resolves (e.g. it was
// deleted by a concurrent RoomDraft edit before this turn dispatched).
var ErrDesignPlanTargetNotFound = ErrDesignTargetNotFound

// ErrDesignReasoningNotConfigured is returned when CreateDesignTurn is
// called but no ElementReasoner has been wired via
// SetDesignPlanningSupport.
var ErrDesignReasoningNotConfigured = errors.New("spatial: design reasoning support not configured")

// Provider/transport-level sentinels a caller's ElementReasoner
// implementation returns — mirrored here (not imported from
// internal/platform/ai, per this package's existing "spatial owns its own
// sentinels" convention) so design_service.go has no platform/ai import.
var (
	ErrDesignReasoningProviderUnavailable = errors.New("spatial: design reasoning provider unavailable")
	ErrDesignReasoningProviderRejected    = errors.New("spatial: design reasoning provider rejected the request")
	ErrDesignReasoningTimeout             = errors.New("spatial: design reasoning request timed out")
	ErrDesignReasoningInvalidOutput       = errors.New("spatial: design reasoning provider returned invalid output")
	ErrDesignPlanTargetMismatch           = errors.New("spatial: design reasoning provider returned a mismatched target")
)

// ElementReasoner is the ONE outbound call CreateDesignTurn makes per
// admitted turn — consumer-defined (spatial declares the interface it
// needs; the composition root supplies a Python-backed implementation, see
// composition/spatialreasoningadapter.go). This package has zero import of
// internal/platform/ai.
type ElementReasoner interface {
	ReasonElement(ctx context.Context, reasoningContext DesignReasoningContext) (ProposedSceneEditDelta, error)
}

// SetDesignPlanningSupport wires the two design-planning repositories and
// the reasoner into Service after construction — same optional setter
// pattern as SetRoomDraftSupport/SetVisualAssetSupport/
// SetAssetGenerationSupport.
func (s *Service) SetDesignPlanningSupport(sessions DesignSessionRepository, turns DesignTurnRepository, reasoner ElementReasoner) {
	s.designSessions = sessions
	s.designTurns = turns
	s.designReasoner = reasoner
}

// SetDesignReasoner swaps ONLY the reasoner, leaving the already-wired
// designSessions/designTurns repositories untouched — for tests that need
// to substitute a counting fake reasoner against the SAME Service/
// persistence graph a real router was built from (never a second,
// separately-constructed Services graph — see this package's "mutate the
// SAME object graph the router was built from" convention).
func (s *Service) SetDesignReasoner(reasoner ElementReasoner) {
	s.designReasoner = reasoner
}

// CreateDesignSessionInput is CreateDesignSession's request shape.
type CreateDesignSessionInput struct {
	ClientSessionID           string
	RoomDraftID               string
	ExpectedRoomDraftRevision int64
	Target                    SpatialDesignTarget
}

// DesignSessionResult is CreateDesignSession's response shape — the bare
// session plus nothing else, matching the plan's "It does not call
// Python" no-side-effect creation contract.
type DesignSessionResult struct {
	Session SpatialDesignSession
}

// sessionRequestFingerprint is the server-computed idempotency fingerprint
// for a session-creation request — never trusted from the client, matching
// computeAssetGenerationFingerprint's "authoritative, server-known values
// only" precedent in this same package.
func sessionRequestFingerprint(companyID, clientSessionID, roomDraftID string, expectedRevision int64, target SpatialDesignTarget) string {
	input := fmt.Sprintf("%s|%s|%s|%d|%s|%s", companyID, clientSessionID, roomDraftID, expectedRevision, target.Kind, target.ID)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// CreateDesignSession creates or idempotently replays an empty design
// session. It NEVER calls the reasoner (RP4E1 plan: "Creates or
// idempotently replays an empty session. It does not call Python.").
func (s *Service) CreateDesignSession(ctx context.Context, companyID, userID string, input CreateDesignSessionInput) (DesignSessionResult, error) {
	if s.designSessions == nil {
		return DesignSessionResult{}, ErrDesignReasoningNotConfigured
	}

	draft, err := s.roomDrafts.FindByID(ctx, companyID, input.RoomDraftID)
	if err != nil {
		return DesignSessionResult{}, err
	}
	if draft.Revision != input.ExpectedRoomDraftRevision {
		return DesignSessionResult{}, ErrDesignPlanStale
	}

	resolvedTarget, err := resolveDesignTarget(draft, input.Target)
	if err != nil {
		return DesignSessionResult{}, err
	}
	_ = resolvedTarget // resolution here is validation-only; the authoritative resolve happens again per-turn against the (possibly still-current) draft.

	now := time.Now()
	session := SpatialDesignSession{
		CompanyID: companyID, ProjectID: draft.CaptureID, SpaceID: "", RoomDraftID: input.RoomDraftID,
		CreatedByUserID: userID, ClientSessionID: input.ClientSessionID,
		SessionRequestFingerprint: sessionRequestFingerprint(companyID, input.ClientSessionID, input.RoomDraftID, input.ExpectedRoomDraftRevision, input.Target),
		Target:                    input.Target, BasedOnRoomDraftRevision: input.ExpectedRoomDraftRevision,
		Status: SpatialDesignSessionStatusActive, CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	}
	created, err := s.designSessions.CreateOrGetSession(ctx, session)
	if err != nil {
		return DesignSessionResult{}, err
	}
	return DesignSessionResult{Session: created}, nil
}

// GetDesignSession returns id's session, tenant-scoped to companyID, plus
// whether it is now stale relative to the RoomDraft's current revision.
func (s *Service) GetDesignSession(ctx context.Context, companyID, id string) (SpatialDesignSession, bool, int64, error) {
	if s.designSessions == nil {
		return SpatialDesignSession{}, false, 0, ErrDesignReasoningNotConfigured
	}
	session, err := s.designSessions.FindSession(ctx, companyID, id)
	if err != nil {
		return SpatialDesignSession{}, false, 0, err
	}
	draft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return SpatialDesignSession{}, false, 0, err
	}
	return session, draft.Revision != session.BasedOnRoomDraftRevision, draft.Revision, nil
}

// ListDesignTurns returns sessionID's turns newest-first, tenant-scoped —
// never invokes the reasoner or changes session/RoomDraft state.
func (s *Service) ListDesignTurns(ctx context.Context, companyID, sessionID string, beforeSequence int64, limit int) ([]SpatialDesignTurn, error) {
	if s.designTurns == nil {
		return nil, ErrDesignReasoningNotConfigured
	}
	if _, err := s.designSessions.FindSession(ctx, companyID, sessionID); err != nil {
		return nil, err
	}
	return s.designTurns.ListTurns(ctx, companyID, sessionID, beforeSequence, limit)
}

// CreateDesignTurnInput is CreateDesignTurn's request shape.
type CreateDesignTurnInput struct {
	ClientRequestID string
	Instruction     string
}

// turnRequestFingerprint is the server-computed idempotency fingerprint
// for a turn-creation request, covering schema version, session ID,
// target, pinned RoomDraft revision, parent-plan turn ID, and normalized
// instruction (RP4E1 plan's exact fingerprint definition).
func turnRequestFingerprint(sessionID string, target SpatialDesignTarget, roomDraftRevision int64, parentPlanTurnID, instruction string) string {
	input := fmt.Sprintf("1|%s|%s|%s|%d|%s|%s", sessionID, target.Kind, target.ID, roomDraftRevision, parentPlanTurnID, instruction)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// CreateDesignTurn is RP4E1's central orchestration entry point. It
// implements the plan's exact required ordering:
//
//	replay lookup by request ID
//	-> tenant session/draft/capture/target/revision validation
//	-> compact context build
//	-> atomic reserve
//	-> immediate draft revision recheck
//	-> durable providerStartedAt marker
//	-> exactly one reasoner call outside transaction
//	-> Python result validation
//	-> draft revision recheck
//	-> cumulative merge + spatial resolution + fit + fingerprint
//	-> atomic terminal turn/session update
//
// Every early-return path above "durable providerStartedAt marker" makes
// ZERO reasoner calls, by construction (the reasoner is invoked from
// exactly one call site near the bottom of this function).
func (s *Service) CreateDesignTurn(ctx context.Context, companyID, userID, sessionID string, input CreateDesignTurnInput) (SpatialDesignTurn, bool, error) {
	if s.designSessions == nil || s.designTurns == nil {
		return SpatialDesignTurn{}, false, ErrDesignReasoningNotConfigured
	}

	// 1. Replay lookup by request ID FIRST — a genuine retry of an already
	// -admitted turn must never re-dispatch, matching this package's
	// "AwardRevision.InsertRevision" idempotency-first precedent.
	if existing, err := s.designTurns.FindTurnByClientRequestID(ctx, companyID, sessionID, input.ClientRequestID); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, ErrDesignTurnNotFound) {
		return SpatialDesignTurn{}, false, err
	}

	// 2. Tenant session/draft/target/revision validation.
	session, err := s.designSessions.FindSession(ctx, companyID, sessionID)
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}
	draft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}
	if draft.Revision != session.BasedOnRoomDraftRevision {
		return SpatialDesignTurn{}, false, ErrDesignPlanStale
	}
	target, err := resolveDesignTarget(draft, session.Target)
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}

	// 3. Compact context build.
	reasoningContext, err := buildDesignReasoningContext(draft, target, session.CurrentWorkingDesign, nil, input.Instruction)
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}

	fingerprint := turnRequestFingerprint(sessionID, session.Target, session.BasedOnRoomDraftRevision, session.LatestReadyPlanTurnID, input.Instruction)

	// 4. Atomic reserve.
	now := time.Now()
	reserved, wasReplay, err := s.designTurns.ReserveTurn(ctx, SpatialDesignTurn{
		CompanyID: companyID, SessionID: sessionID,
		ClientRequestID: input.ClientRequestID, RequestFingerprint: fingerprint,
		BaseSessionRevision: session.Revision, BasedOnRoomDraftRevision: session.BasedOnRoomDraftRevision,
		Instruction: input.Instruction, Status: SpatialDesignTurnStatusReserved,
		StartedAt: now, CreatedAt: now, SchemaVersion: 1,
	})
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}
	if wasReplay {
		return reserved, true, nil
	}
	// The durable turn ID exists only after reservation. Add it to the
	// already-built bounded context before the single Python dispatch; it is
	// server-authoritative correlation data, not browser input.
	reasoningContext.TurnID = reserved.ID

	// 5. Immediate draft revision recheck (RP4E1 plan: "immediately before
	// provider dispatch"). If it changed between step 2 and now, fail this
	// turn as stale WITHOUT ever calling the reasoner.
	recheckDraft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_reasoning_invalid_request")
	}
	if recheckDraft.Revision != session.BasedOnRoomDraftRevision {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusStale, "design_plan_stale")
	}

	// 6. Durable providerStartedAt marker — the at-most-once dispatch line.
	if err := s.designTurns.MarkProviderStarted(ctx, companyID, reserved.ID, time.Now()); err != nil {
		return SpatialDesignTurn{}, false, err
	}

	// 7. Exactly one reasoner call, outside any transaction.
	if s.designReasoner == nil {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_reasoning_not_configured")
	}
	delta, reasonErr := s.designReasoner.ReasonElement(ctx, reasoningContext)
	if reasonErr != nil {
		return s.classifyAndFinishReasonerError(ctx, companyID, sessionID, reserved.ID, reasonErr)
	}

	// 8. Python result validation (Go-side re-validation, independent of
	// whatever Python already checked).
	if delta.Target.Kind != session.Target.Kind || delta.Target.ID != session.Target.ID {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_plan_target_mismatch")
	}
	if err := delta.Validate(); err != nil {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_reasoning_invalid_output")
	}

	// 9. Draft revision recheck (after the provider call returned).
	postDraft, err := s.roomDrafts.FindByID(ctx, companyID, session.RoomDraftID)
	if err != nil {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_reasoning_invalid_request")
	}
	if postDraft.Revision != session.BasedOnRoomDraftRevision {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusStale, "design_plan_stale")
	}

	// 10. Cumulative merge + spatial resolution + fit + fingerprint.
	mergedWorkingDesign, err := mergeWorkingDesign(session.CurrentWorkingDesign, delta)
	if err != nil {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_reasoning_invalid_output")
	}
	resolvedOps, fit, err := resolveSpatialChanges(draft, target, session.CurrentWorkingDesign, delta)
	if err != nil {
		return s.finishFailedTurn(ctx, companyID, sessionID, reserved.ID, SpatialDesignTurnStatusFailed, "design_reasoning_invalid_output")
	}
	mergedWorkingDesign.ResolvedSpatialOperations = resolvedOps

	execution := computeDesignExecutionFlags(delta, mergedWorkingDesign, session.AcceptedDesign)

	terminalStatus := SpatialDesignTurnStatusProposed
	if fit.Status == FitStatusBlocked {
		terminalStatus = SpatialDesignTurnStatusBlocked
	}

	plan := ValidatedSceneEditPlan{
		Target: session.Target, BasedOnRoomDraftRevision: session.BasedOnRoomDraftRevision,
		WorkingDesign: mergedWorkingDesign, Fit: fit, Execution: execution,
	}
	planFingerprint, err := computeDesignPlanFingerprint(plan)
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}

	// 11. Atomic terminal turn/session update.
	finished, err := s.designTurns.FinishTurn(ctx, FinishTurnInput{
		CompanyID: companyID, SessionID: sessionID, TurnID: reserved.ID,
		Status: terminalStatus, ProposedDelta: &delta, ValidatedPlan: &plan,
		PlanFingerprint: planFingerprint, WorkingDesign: mergedWorkingDesign,
	})
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}
	return finished, false, nil
}

// finishFailedTurn persists a failure/stale terminal state for an
// already-reserved turn — a shared helper for every early-failure branch
// after reservation, so each call site does not repeat the FinishTurn
// plumbing.
func (s *Service) finishFailedTurn(ctx context.Context, companyID, sessionID, turnID string, status SpatialDesignTurnStatus, safeCode string) (SpatialDesignTurn, bool, error) {
	finished, err := s.designTurns.FinishTurn(ctx, FinishTurnInput{
		CompanyID: companyID, SessionID: sessionID, TurnID: turnID,
		Status: status, SafeFailureCode: safeCode,
	})
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}
	return finished, false, nil
}

// classifyAndFinishReasonerError maps a reasoner error to the appropriate
// terminal turn status. An AMBIGUOUS provider outcome (unavailable,
// rejected, or timeout — any case where whether the provider actually
// received/processed the request cannot be known) becomes
// needs_attention, per RP4E1's explicit requirement: "Treat ambiguous
// provider outcomes as needs_attention; never redispatch the same turn."
// Only a definitively INVALID (but received) response maps to a plain
// failure.
func (s *Service) classifyAndFinishReasonerError(ctx context.Context, companyID, sessionID, turnID string, reasonErr error) (SpatialDesignTurn, bool, error) {
	switch {
	case errors.Is(reasonErr, ErrDesignReasoningInvalidOutput), errors.Is(reasonErr, ErrDesignPlanTargetMismatch):
		return s.finishFailedTurn(ctx, companyID, sessionID, turnID, SpatialDesignTurnStatusFailed, "design_reasoning_invalid_output")
	case errors.Is(reasonErr, ErrDesignReasoningProviderUnavailable):
		return s.finishFailedTurn(ctx, companyID, sessionID, turnID, SpatialDesignTurnStatusNeedsAttention, "design_reasoning_provider_unavailable")
	case errors.Is(reasonErr, ErrDesignReasoningProviderRejected):
		return s.finishFailedTurn(ctx, companyID, sessionID, turnID, SpatialDesignTurnStatusNeedsAttention, "design_reasoning_provider_rejected")
	case errors.Is(reasonErr, ErrDesignReasoningTimeout):
		return s.finishFailedTurn(ctx, companyID, sessionID, turnID, SpatialDesignTurnStatusNeedsAttention, "design_reasoning_timeout")
	default:
		// An unrecognized error is itself ambiguous — whether the provider
		// was reached is unknown, so it is never treated as a safe,
		// definite local failure.
		return s.finishFailedTurn(ctx, companyID, sessionID, turnID, SpatialDesignTurnStatusNeedsAttention, "design_reasoning_provider_unavailable")
	}
}

// computeDesignExecutionFlags derives DesignExecutionFlags from delta (this
// turn's own local proposal), merged (the cumulative working design), and
// accepted (the session's last PERSISTED design, nil until the first Use
// Design) per the RP4E2/M8.5C amendment (plan repository-findings row 2):
// TurnRequiresAssetGeneration reflects only THIS turn's geometry replace.
// HunyuanRequired reflects whether the cumulative WORKING geometry differs
// from the ACCEPTED geometry already bound to the RoomDraft — not "any
// cumulative custom geometry" — so a material-only refinement after an
// earlier turn's geometry was accepted and generated does NOT wrongly
// re-request Hunyuan for a mesh that is already published and bound.
func computeDesignExecutionFlags(delta ProposedSceneEditDelta, merged WorkingDesign, accepted *WorkingDesign) DesignExecutionFlags {
	turnRequiresGeneration := delta.Geometry.Mode == SectionModeReplace

	var acceptedGeometry *WorkingDesignGeometry
	if accepted != nil {
		acceptedGeometry = accepted.Geometry
	}
	pendingCustomGeometry := merged.Geometry != nil && !geometryEqual(merged.Geometry, acceptedGeometry)

	// A turn that ITSELF proposes new geometry always requires generation,
	// even if the resulting merged geometry happens to equal the accepted
	// one byte-for-byte (a fresh replace is never assumed already-generated
	// just because its description matches).
	hunyuanRequired := turnRequiresGeneration || pendingCustomGeometry

	return DesignExecutionFlags{
		TurnRequiresAssetGeneration: turnRequiresGeneration,
		HunyuanRequired:             hunyuanRequired,
		RequiresConfirmation:        true,
		Executable:                  true,
	}
}

// geometryEqual reports whether two (possibly nil) WorkingDesignGeometry
// values represent the same accepted shape — used only to decide whether
// pending working geometry still needs generation relative to what is
// already bound to the RoomDraft.
func geometryEqual(a, b *WorkingDesignGeometry) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
