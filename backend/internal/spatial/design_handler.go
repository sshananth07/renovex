package spatial

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// designTargetDTO is the public wire shape of SpatialDesignTarget.
type designTargetDTO struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func toDesignTargetDTO(t SpatialDesignTarget) designTargetDTO {
	return designTargetDTO{Kind: string(t.Kind), ID: t.ID}
}

// designSessionDTO is the public GET /spatial/design-sessions/{id} response
// shape's "session" field — matches RP4E1 plan's exact documented JSON.
type designSessionDTO struct {
	ID                       string           `json:"id"`
	RoomDraftID              string           `json:"roomDraftId"`
	BasedOnRoomDraftRevision int64            `json:"basedOnRoomDraftRevision"`
	Target                   designTargetDTO  `json:"target"`
	Status                   string           `json:"status"`
	CurrentWorkingDesign     workingDesignDTO `json:"currentWorkingDesign"`
	LatestTurnID             string           `json:"latestTurnId,omitempty"`
	LatestReadyPlanTurnID    string           `json:"latestReadyPlanTurnId,omitempty"`
	Revision                 int64            `json:"revision"`
	CreatedAt                string           `json:"createdAt"`
	UpdatedAt                string           `json:"updatedAt"`
}

type workingDesignGeometryDTO struct {
	Category                    string `json:"category"`
	ShapeDescription            string `json:"shapeDescription"`
	PreserveCanonicalDimensions bool   `json:"preserveCanonicalDimensions"`
}

type workingDesignMaterialDTO struct {
	BaseColor      string `json:"baseColor"`
	MaterialFamily string `json:"materialFamily"`
	Roughness      string `json:"roughness"`
	Metallic       bool   `json:"metallic"`
}

type resolvedSpatialOperationDTO struct {
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
}

type workingDesignDTO struct {
	Geometry                  *workingDesignGeometryDTO     `json:"geometry,omitempty"`
	Material                  *workingDesignMaterialDTO     `json:"material,omitempty"`
	ResolvedSpatialOperations []resolvedSpatialOperationDTO `json:"resolvedSpatialOperations"`
}

func toWorkingDesignDTO(w WorkingDesign) workingDesignDTO {
	dto := workingDesignDTO{ResolvedSpatialOperations: []resolvedSpatialOperationDTO{}}
	if w.Geometry != nil {
		dto.Geometry = &workingDesignGeometryDTO{
			Category: w.Geometry.Category, ShapeDescription: w.Geometry.ShapeDescription,
			PreserveCanonicalDimensions: w.Geometry.PreserveCanonicalDimensions,
		}
	}
	if w.Material != nil {
		dto.Material = &workingDesignMaterialDTO{
			BaseColor: w.Material.BaseColor, MaterialFamily: w.Material.MaterialFamily,
			Roughness: w.Material.Roughness, Metallic: w.Material.Metallic,
		}
	}
	for _, op := range w.ResolvedSpatialOperations {
		dto.ResolvedSpatialOperations = append(dto.ResolvedSpatialOperations, resolvedSpatialOperationDTO{Kind: string(op.Kind), Payload: op.Payload})
	}
	return dto
}

func toDesignSessionDTO(s SpatialDesignSession) designSessionDTO {
	return designSessionDTO{
		ID: s.ID, RoomDraftID: s.RoomDraftID, BasedOnRoomDraftRevision: s.BasedOnRoomDraftRevision,
		Target: toDesignTargetDTO(s.Target), Status: string(s.Status),
		CurrentWorkingDesign: toWorkingDesignDTO(s.CurrentWorkingDesign),
		LatestTurnID:         s.LatestTurnID, LatestReadyPlanTurnID: s.LatestReadyPlanTurnID,
		Revision: s.Revision, CreatedAt: s.CreatedAt.Format(timeLayout), UpdatedAt: s.UpdatedAt.Format(timeLayout),
	}
}

type getDesignSessionOutput struct {
	Body struct {
		Session                  designSessionDTO `json:"session"`
		Stale                    bool             `json:"stale"`
		CurrentRoomDraftRevision int64            `json:"currentRoomDraftRevision"`
	}
}

// changePlanDTO is the public turn's "changePlan" field.
type changePlanDTO struct {
	Summary       []string             `json:"summary"`
	TurnDelta     turnDeltaDTO         `json:"turnDelta"`
	WorkingDesign workingDesignPlanDTO `json:"workingDesign"`
}

type sectionDTO struct {
	Mode string `json:"mode"`
	Spec any    `json:"spec,omitempty"`
}

type turnDeltaDTO struct {
	Geometry sectionDTO `json:"geometry"`
	Material sectionDTO `json:"material"`
	Spatial  sectionDTO `json:"spatial"`
}

type workingDesignPlanDTO struct {
	Geometry                  *workingDesignGeometryDTO     `json:"geometry,omitempty"`
	Material                  *workingDesignMaterialDTO     `json:"material,omitempty"`
	ResolvedSpatialOperations []resolvedSpatialOperationDTO `json:"resolvedSpatialOperations"`
}

func toSectionDTO(sc SectionChange) sectionDTO {
	dto := sectionDTO{Mode: string(sc.Mode)}
	switch {
	case sc.GeometrySpec != nil:
		dto.Spec = workingDesignGeometryDTO{
			Category: sc.GeometrySpec.Category, ShapeDescription: sc.GeometrySpec.ShapeDescription,
			PreserveCanonicalDimensions: sc.GeometrySpec.PreserveCanonicalDimensions,
		}
	case sc.MaterialSpec != nil:
		dto.Spec = workingDesignMaterialDTO{
			BaseColor: sc.MaterialSpec.BaseColor, MaterialFamily: sc.MaterialSpec.MaterialFamily,
			Roughness: sc.MaterialSpec.Roughness, Metallic: sc.MaterialSpec.Metallic,
		}
	case sc.SpatialSpec != nil:
		spec := map[string]any{"kind": string(sc.SpatialSpec.Kind)}
		switch sc.SpatialSpec.Kind {
		case SpatialOpMoveRelativeToNearestWall:
			spec["relationship"] = string(sc.SpatialSpec.Relationship)
			spec["distanceMeters"] = sc.SpatialSpec.DistanceMeters
		case SpatialOpResizeAxis:
			spec["axis"] = string(sc.SpatialSpec.Axis)
			if sc.SpatialSpec.HasDelta {
				spec["deltaMeters"] = sc.SpatialSpec.DeltaMeters
			}
			if sc.SpatialSpec.HasTarget {
				spec["targetMeters"] = sc.SpatialSpec.TargetMeters
			}
		}
		dto.Spec = spec
	}
	return dto
}

// fitObservationDTO/fitWarningDTO/fitBlockerDTO/fitAnalysisDTO mirror
// designgeometry.go's Go-authoritative fit types — never computed by
// Python, only reported here.
type fitObservationDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type fitWarningDTO struct {
	Code            string   `json:"code"`
	Message         string   `json:"message"`
	ClearanceBefore *float64 `json:"clearanceBeforeMeters,omitempty"`
	ClearanceAfter  *float64 `json:"clearanceAfterMeters,omitempty"`
}

type fitBlockerDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type fitAnalysisDTO struct {
	Status       string              `json:"status"`
	Observations []fitObservationDTO `json:"observations"`
	Warnings     []fitWarningDTO     `json:"warnings"`
	Blockers     []fitBlockerDTO     `json:"blockers"`
}

func toFitAnalysisDTO(f FitAnalysis) fitAnalysisDTO {
	dto := fitAnalysisDTO{Status: string(f.Status), Observations: []fitObservationDTO{}, Warnings: []fitWarningDTO{}, Blockers: []fitBlockerDTO{}}
	for _, o := range f.Observations {
		dto.Observations = append(dto.Observations, fitObservationDTO{Code: o.Code, Message: o.Message})
	}
	for _, w := range f.Warnings {
		dto.Warnings = append(dto.Warnings, fitWarningDTO{Code: w.Code, Message: w.Message, ClearanceBefore: w.ClearanceBefore, ClearanceAfter: w.ClearanceAfter})
	}
	for _, b := range f.Blockers {
		dto.Blockers = append(dto.Blockers, fitBlockerDTO{Code: b.Code, Message: b.Message})
	}
	return dto
}

// executionDTO is the public turn's "execution" field — RuntimeNotice is
// populated ONLY when HunyuanRequired is true (RP4E1 plan's exact example
// wire shape).
type executionDTO struct {
	TurnRequiresAssetGeneration bool   `json:"turnRequiresAssetGeneration"`
	HunyuanRequired             bool   `json:"hunyuanRequired"`
	RequiresConfirmation        bool   `json:"requiresConfirmation"`
	Executable                  bool   `json:"executable"`
	RuntimeNotice               string `json:"runtimeNotice,omitempty"`
}

// reviewDTO is the public turn's "review" field — assumptions/notes come
// straight from the (already Go-revalidated) provider proposal;
// confidence is provider-reported and explicitly NOT part of the plan
// fingerprint.
type reviewDTO struct {
	Assumptions []string `json:"assumptions"`
	Notes       []string `json:"notes"`
	Confidence  float64  `json:"confidence"`
}

// designTurnDTO is the public turn result — see this package's design
// amendment doc and RP4E1 plan's "Public turn result" section for the
// exact frozen shape. Provider/model/prompt text, chain of thought,
// internal context, raw output, claim token, provider timestamps, and
// safe failure DETAIL stay out of this type entirely — only SafeFailureCode
// (a stable machine code, never English prose from a provider) is exposed.
type designTurnDTO struct {
	ID                       string          `json:"id"`
	SessionID                string          `json:"sessionId"`
	Sequence                 int64           `json:"sequence"`
	PreviousTurnID           string          `json:"previousTurnId,omitempty"`
	ParentPlanTurnID         string          `json:"parentPlanTurnId,omitempty"`
	Status                   string          `json:"status"`
	Instruction              string          `json:"instruction"`
	BasedOnRoomDraftRevision int64           `json:"basedOnRoomDraftRevision"`
	Intent                   string          `json:"intent,omitempty"`
	ChangePlan               *changePlanDTO  `json:"changePlan,omitempty"`
	FitAnalysis              *fitAnalysisDTO `json:"fitAnalysis,omitempty"`
	Execution                *executionDTO   `json:"execution,omitempty"`
	Review                   *reviewDTO      `json:"review,omitempty"`
	PlanFingerprint          string          `json:"planFingerprint,omitempty"`
	FailureCode              string          `json:"failureCode,omitempty"`
	CreatedAt                string          `json:"createdAt"`
	CompletedAt              string          `json:"completedAt,omitempty"`
}

// toDesignTurnDTO maps a SpatialDesignTurn to its public shape.
// runtimeNoticeText is the operator-configured fixed copy (composition-
// root-owned config, RP4E1 plan Task 8) — the handler decides WHETHER to
// include it (based on HunyuanRequired); it never invents or looks up the
// text itself.
func toDesignTurnDTO(t SpatialDesignTurn, runtimeNoticeText string) designTurnDTO {
	dto := designTurnDTO{
		ID: t.ID, SessionID: t.SessionID, Sequence: t.Sequence,
		PreviousTurnID: t.PreviousTurnID, ParentPlanTurnID: t.ParentPlanTurnID,
		Status: string(t.Status), Instruction: t.Instruction,
		BasedOnRoomDraftRevision: t.BasedOnRoomDraftRevision,
		PlanFingerprint:          t.PlanFingerprint, FailureCode: t.SafeFailureCode,
		CreatedAt: t.CreatedAt.Format(timeLayout),
	}
	if t.CompletedAt != nil {
		dto.CompletedAt = t.CompletedAt.Format(timeLayout)
	}
	if t.ProposedDelta != nil {
		dto.Intent = string(t.ProposedDelta.Intent)
		dto.ChangePlan = &changePlanDTO{
			Summary: t.ProposedDelta.Summary,
			TurnDelta: turnDeltaDTO{
				Geometry: toSectionDTO(t.ProposedDelta.Geometry),
				Material: toSectionDTO(t.ProposedDelta.Material),
				Spatial:  toSectionDTO(t.ProposedDelta.Spatial),
			},
		}
		dto.Review = &reviewDTO{Assumptions: t.ProposedDelta.Assumptions, Notes: t.ProposedDelta.ReviewNotes, Confidence: t.ProposedDelta.Confidence}
	}
	if t.ValidatedPlan != nil {
		if dto.ChangePlan != nil {
			dto.ChangePlan.WorkingDesign = workingDesignPlanDTO{
				Geometry:                  toWorkingDesignDTO(t.ValidatedPlan.WorkingDesign).Geometry,
				Material:                  toWorkingDesignDTO(t.ValidatedPlan.WorkingDesign).Material,
				ResolvedSpatialOperations: toWorkingDesignDTO(t.ValidatedPlan.WorkingDesign).ResolvedSpatialOperations,
			}
		}
		fitDTO := toFitAnalysisDTO(t.ValidatedPlan.Fit)
		dto.FitAnalysis = &fitDTO
		exec := executionDTO{
			TurnRequiresAssetGeneration: t.ValidatedPlan.Execution.TurnRequiresAssetGeneration,
			HunyuanRequired:             t.ValidatedPlan.Execution.HunyuanRequired,
			RequiresConfirmation:        t.ValidatedPlan.Execution.RequiresConfirmation,
			Executable:                  t.ValidatedPlan.Execution.Executable,
		}
		if exec.HunyuanRequired {
			exec.RuntimeNotice = runtimeNoticeText
		}
		dto.Execution = &exec
	}
	return dto
}

// mapDesignError maps spatial's design-reasoning sentinels to stable Huma
// HTTP errors, matching mapSpatialError's existing convention exactly —
// this is a SEPARATE function (not an addition to mapSpatialError's
// switch) so its cases stay grouped and reviewable as one RP4E1 unit, but
// callers may fall through to mapSpatialError for the errors it already
// knows.
func mapDesignError(err error) error {
	switch {
	case err == ErrDesignSessionNotFound, err == ErrDesignTargetNotFound, err == ErrRoomDraftNotFound:
		// Foreign and missing resources deliberately indistinguishable
		// (RP4E1 plan's public contract).
		return huma.Error404NotFound("design session not found")
	case err == ErrUnsupportedDesignTargetKind, err == ErrInvalidDesignTargetTransform:
		return huma.Error422UnprocessableEntity("unsupported or invalid design session target")
	case err == ErrDesignPlanStale:
		return spatialConflictError("room draft revision changed since this design session was created", "design_plan_stale")
	case err == ErrDesignSessionRequestConflict:
		return spatialConflictError("design session request conflict", "design_session_request_conflict")
	case err == ErrDesignTurnRequestConflict:
		return spatialConflictError("design turn request conflict", "design_turn_request_conflict")
	case err == ErrDesignTurnInProgress:
		return spatialConflictError("another design turn is already in progress for this session", "design_turn_in_progress")
	case err == ErrDesignReasoningNotConfigured:
		return &huma.ErrorModel{Status: http.StatusServiceUnavailable, Title: http.StatusText(http.StatusServiceUnavailable), Detail: "design reasoning is not configured", Type: "design_reasoning_not_configured"}
	case err == ErrDesignReasoningProviderUnavailable:
		return &huma.ErrorModel{Status: http.StatusServiceUnavailable, Title: http.StatusText(http.StatusServiceUnavailable), Detail: "design reasoning provider unavailable", Type: "design_reasoning_provider_unavailable"}
	case err == ErrDesignReasoningProviderRejected:
		return &huma.ErrorModel{Status: http.StatusServiceUnavailable, Title: http.StatusText(http.StatusServiceUnavailable), Detail: "design reasoning provider rejected the request", Type: "design_reasoning_provider_rejected"}
	case err == ErrDesignReasoningTimeout:
		return &huma.ErrorModel{Status: http.StatusGatewayTimeout, Title: http.StatusText(http.StatusGatewayTimeout), Detail: "design reasoning request timed out", Type: "design_reasoning_timeout"}
	case err == ErrDesignReasoningInvalidOutput:
		return &huma.ErrorModel{Status: http.StatusBadGateway, Title: http.StatusText(http.StatusBadGateway), Detail: "design reasoning provider returned invalid output", Type: "design_reasoning_invalid_output"}
	case err == ErrDesignPlanTargetMismatch:
		return &huma.ErrorModel{Status: http.StatusBadGateway, Title: http.StatusText(http.StatusBadGateway), Detail: "design reasoning provider returned a mismatched target", Type: "design_plan_target_mismatch"}
	default:
		return mapSpatialError(err)
	}
}

// --- HTTP transport ---

type createDesignSessionInput struct {
	Body struct {
		ClientSessionID           string          `json:"clientSessionId" required:"true" minLength:"1" maxLength:"128"`
		RoomDraftID               string          `json:"roomDraftId" required:"true" minLength:"1"`
		ExpectedRoomDraftRevision int64           `json:"expectedRoomDraftRevision"`
		Target                    designTargetDTO `json:"target" required:"true"`
	}
}

type createDesignSessionOutput struct {
	Body struct {
		Session designSessionDTO `json:"session"`
	}
}

type createDesignTurnInput struct {
	ID   string `path:"id"`
	Body struct {
		ClientRequestID string `json:"clientRequestId" required:"true" minLength:"1" maxLength:"128"`
		Instruction     string `json:"instruction" required:"true" minLength:"1" maxLength:"2000"`
	}
}

type createDesignTurnOutput struct {
	Body designTurnDTO
}

type getDesignSessionInput struct {
	ID string `path:"id"`
}

type listDesignTurnsInput struct {
	ID             string `path:"id"`
	BeforeSequence int64  `query:"beforeSequence"`
	Limit          int    `query:"limit"`
}

type listDesignTurnsOutput struct {
	Body []designTurnDTO
}

const (
	defaultListDesignTurnsLimit = 20
	maxListDesignTurnsLimit     = 50
)

// RegisterDesignHandlers registers RP4E1's four public design-reasoning
// routes on api, backed by svc and runtimeNoticeText (composition-root-
// owned fixed copy). Called from RegisterHandlers alongside every other
// spatial route — there is no separate design-specific auth surface.
func RegisterDesignHandlers(api huma.API, svc *Service, runtimeNoticeText string) {
	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-sessions-create",
		Method:      http.MethodPost,
		Path:        "/spatial/design-sessions",
		Summary:     "Create or idempotently replay a conversational design-reasoning session (RP4E1)",
	}, func(ctx context.Context, input *createDesignSessionInput) (*createDesignSessionOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		result, err := svc.CreateDesignSession(ctx, principal.CompanyID, principal.UserID, CreateDesignSessionInput{
			ClientSessionID: input.Body.ClientSessionID, RoomDraftID: input.Body.RoomDraftID,
			ExpectedRoomDraftRevision: input.Body.ExpectedRoomDraftRevision,
			Target:                    SpatialDesignTarget{Kind: DesignTargetKind(input.Body.Target.Kind), ID: input.Body.Target.ID},
		})
		if err != nil {
			return nil, mapDesignError(err)
		}
		out := &createDesignSessionOutput{}
		out.Body.Session = toDesignSessionDTO(result.Session)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-sessions-get",
		Method:      http.MethodGet,
		Path:        "/spatial/design-sessions/{id}",
		Summary:     "Get a design session, its bounded working design, and stale status (RP4E1)",
	}, func(ctx context.Context, input *getDesignSessionInput) (*getDesignSessionOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		session, stale, currentRevision, err := svc.GetDesignSession(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapDesignError(err)
		}
		out := &getDesignSessionOutput{}
		out.Body.Session = toDesignSessionDTO(session)
		out.Body.Stale = stale
		out.Body.CurrentRoomDraftRevision = currentRevision
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-turns-create",
		Method:      http.MethodPost,
		Path:        "/spatial/design-sessions/{id}/turns",
		Summary:     "Create one immutable reasoning turn, run synchronously (RP4E1)",
	}, func(ctx context.Context, input *createDesignTurnInput) (*createDesignTurnOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		turn, _, err := svc.CreateDesignTurn(ctx, principal.CompanyID, principal.UserID, input.ID, CreateDesignTurnInput{
			ClientRequestID: input.Body.ClientRequestID, Instruction: input.Body.Instruction,
		})
		if err != nil {
			return nil, mapDesignError(err)
		}
		return &createDesignTurnOutput{Body: toDesignTurnDTO(turn, runtimeNoticeText)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-turns-list",
		Method:      http.MethodGet,
		Path:        "/spatial/design-sessions/{id}/turns",
		Summary:     "List a design session's immutable turns, newest first (RP4E1)",
	}, func(ctx context.Context, input *listDesignTurnsInput) (*listDesignTurnsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		limit := input.Limit
		if limit <= 0 {
			limit = defaultListDesignTurnsLimit
		}
		if limit > maxListDesignTurnsLimit {
			limit = maxListDesignTurnsLimit
		}
		turns, err := svc.ListDesignTurns(ctx, principal.CompanyID, input.ID, input.BeforeSequence, limit)
		if err != nil {
			return nil, mapDesignError(err)
		}
		dtos := make([]designTurnDTO, 0, len(turns))
		for _, t := range turns {
			dtos = append(dtos, toDesignTurnDTO(t, runtimeNoticeText))
		}
		return &listDesignTurnsOutput{Body: dtos}, nil
	})
}
