package spatial

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// --- public DTOs ---
//
// Provider names, model identifiers, R2 keys, checksums, seeds, and
// prompt/reasoning content NEVER appear on any of these types — matching
// design_handler.go's designTurnDTO convention exactly (RP4E2 plan:
// "Public progress exposes only stage, status, safe copy... Provider
// names, R2 keys, source URLs, and tokens never leave Go").

type visualAppearanceDTO struct {
	BaseColor      string `json:"baseColor"`
	MaterialFamily string `json:"materialFamily"`
	Roughness      string `json:"roughness"`
	Metallic       bool   `json:"metallic"`
}

func toVisualAppearanceDTO(a VisualAppearance) visualAppearanceDTO {
	return visualAppearanceDTO{BaseColor: a.BaseColor, MaterialFamily: string(a.MaterialFamily), Roughness: string(a.Roughness), Metallic: a.Metallic}
}

type visualAssetRefDTO struct {
	AssetID string `json:"assetId"`
	Version int    `json:"version"`
}

// designConceptDTO is the attempt's public candidate — placement/dimensions
// plus WHAT Use Design will do (the binding actions), never a storage key
// or generated-asset URL. The actual renderable asset is fetched through
// RP4D's existing authorized visual-asset access route once VisualAsset is
// set.
type designConceptDTO struct {
	Target           designTargetDTO      `json:"target"`
	Transform        RoomLocalTransform   `json:"transform"`
	Dimensions       *RoomLocalPoint      `json:"dimensions,omitempty"`
	VisualAction     string               `json:"visualAction"`
	AppearanceAction string               `json:"appearanceAction"`
	Appearance       *visualAppearanceDTO `json:"appearance,omitempty"`
	VisualAsset      *visualAssetRefDTO   `json:"visualAsset,omitempty"`
}

func toDesignConceptDTO(c DesignConcept) designConceptDTO {
	dto := designConceptDTO{
		Target: toDesignTargetDTO(c.Target), Transform: c.Transform, Dimensions: c.Dimensions,
		VisualAction: string(c.VisualAction), AppearanceAction: string(c.AppearanceAction),
	}
	if c.Appearance != nil {
		appearance := toVisualAppearanceDTO(*c.Appearance)
		dto.Appearance = &appearance
	}
	if c.VisualAsset != nil {
		dto.VisualAsset = &visualAssetRefDTO{AssetID: c.VisualAsset.AssetID, Version: c.VisualAsset.Version}
	}
	return dto
}

// designGenerationAttemptDTO is the public attempt shape — RP4E2 plan:
// "Public progress exposes only stage, status, safe copy, and the
// config-driven runtime notice."
type designGenerationAttemptDTO struct {
	ID                       string            `json:"id"`
	SessionID                string            `json:"sessionId"`
	TurnID                   string            `json:"turnId"`
	AttemptNumber            int64             `json:"attemptNumber"`
	Kind                     string            `json:"kind"`
	Status                   string            `json:"status"`
	BasedOnRoomDraftRevision int64             `json:"basedOnRoomDraftRevision"`
	Candidate                *designConceptDTO `json:"candidate,omitempty"`
	SafeFailureCode          string            `json:"safeFailureCode,omitempty"`
	CreatedAt                string            `json:"createdAt"`
	UpdatedAt                string            `json:"updatedAt"`
	CompletedAt              string            `json:"completedAt,omitempty"`
	AbandonedAt              string            `json:"abandonedAt,omitempty"`
	AcceptedAt               string            `json:"acceptedAt,omitempty"`
}

func toDesignGenerationAttemptDTO(a DesignGenerationAttempt) designGenerationAttemptDTO {
	dto := designGenerationAttemptDTO{
		ID: a.ID, SessionID: a.SessionID, TurnID: a.TurnID, AttemptNumber: a.AttemptNumber,
		Kind: string(a.Kind), Status: string(a.Status), BasedOnRoomDraftRevision: a.BasedOnRoomDraftRevision,
		SafeFailureCode: a.SafeFailureCode, CreatedAt: a.CreatedAt.Format(timeLayout), UpdatedAt: a.UpdatedAt.Format(timeLayout),
	}
	// Candidate is only meaningful once the attempt has actually produced
	// one — an empty zero-value DesignConcept before that point would
	// render as a misleading all-zero object on the wire.
	if a.Status == DesignGenerationStatusConceptReady || a.Status == DesignGenerationStatusAccepted {
		concept := toDesignConceptDTO(a.Candidate)
		dto.Candidate = &concept
	}
	if a.CompletedAt != nil {
		dto.CompletedAt = a.CompletedAt.Format(timeLayout)
	}
	if a.AbandonedAt != nil {
		dto.AbandonedAt = a.AbandonedAt.Format(timeLayout)
	}
	if a.AcceptedAt != nil {
		dto.AcceptedAt = a.AcceptedAt.Format(timeLayout)
	}
	return dto
}

type designAcceptanceDTO struct {
	ID                         string   `json:"id"`
	SessionID                  string   `json:"sessionId"`
	TurnID                     string   `json:"turnId"`
	AttemptID                  string   `json:"attemptId"`
	RoomDraftID                string   `json:"roomDraftId"`
	BaseRoomDraftRevision      int64    `json:"baseRoomDraftRevision"`
	ResultingRoomDraftRevision int64    `json:"resultingRoomDraftRevision"`
	AppliedOperationIDs        []string `json:"appliedOperationIds"`
	CreatedAt                  string   `json:"createdAt"`
}

func toDesignAcceptanceDTO(a DesignAcceptance) designAcceptanceDTO {
	return designAcceptanceDTO{
		ID: a.ID, SessionID: a.SessionID, TurnID: a.TurnID, AttemptID: a.AttemptID,
		RoomDraftID: a.RoomDraftID, BaseRoomDraftRevision: a.BaseRoomDraftRevision, ResultingRoomDraftRevision: a.ResultingRoomDraftRevision,
		AppliedOperationIDs: a.AppliedOperationIDs, CreatedAt: a.CreatedAt.Format(timeLayout),
	}
}

// mapDesignGenerationError maps RP4E2's sentinels to stable Huma HTTP
// errors — a separate function from mapDesignError/mapSpatialError (same
// "keep each slice's cases grouped and reviewable" convention), falling
// through to mapDesignError for shared design-session sentinels (e.g.
// ErrDesignPlanStale, ErrDesignSessionNotFound).
func mapDesignGenerationError(err error) error {
	switch {
	case err == ErrDesignGenerationAttemptNotFound, err == ErrDesignAcceptanceNotFound:
		return huma.Error404NotFound("design generation attempt not found")
	case err == ErrDesignGenerationRequestConflict:
		return spatialConflictError("design generation request conflict", "design_request_id_conflict")
	case err == ErrDesignAcceptanceRequestConflict:
		return spatialConflictError("design acceptance request conflict", "design_request_id_conflict")
	case err == ErrDesignGenerationInProgress:
		return spatialConflictError("a design generation attempt is already in progress for this turn", "design_generation_in_progress")
	case err == ErrDesignAttemptNotReady:
		return huma.Error422UnprocessableEntity("design generation attempt is not ready to use")
	case err == ErrDesignAttemptNotLatestReady:
		return spatialConflictError("a newer design generation attempt exists for this session", "design_generation_superseded")
	case err == ErrDesignAttemptAbandoned:
		return huma.Error422UnprocessableEntity("design generation attempt was cancelled and can never be used")
	case err == ErrDesignGenerationSupportNotConfigured:
		return &huma.ErrorModel{Status: http.StatusServiceUnavailable, Title: http.StatusText(http.StatusServiceUnavailable), Detail: "design generation support is not configured", Type: "design_generation_not_configured"}
	default:
		return mapDesignError(err)
	}
}

// --- HTTP transport ---

type confirmDesignPlanInput struct {
	SessionID string `path:"sessionId"`
	TurnID    string `path:"turnId"`
	Body      struct {
		ClientRequestID           string `json:"clientRequestId" required:"true" minLength:"1" maxLength:"128"`
		PlanFingerprint           string `json:"planFingerprint" required:"true" minLength:"1"`
		ExpectedRoomDraftRevision int64  `json:"expectedRoomDraftRevision"`
	}
}

type designGenerationAttemptOutput struct {
	Body designGenerationAttemptDTO
}

type regenerateDesignPlanInput struct {
	SessionID string `path:"sessionId"`
	TurnID    string `path:"turnId"`
	Body      struct {
		ClientRequestID           string `json:"clientRequestId" required:"true" minLength:"1" maxLength:"128"`
		PlanFingerprint           string `json:"planFingerprint" required:"true" minLength:"1"`
		ExpectedRoomDraftRevision int64  `json:"expectedRoomDraftRevision"`
	}
}

type listDesignGenerationAttemptsInput struct {
	SessionID string `path:"sessionId"`
	TurnID    string `query:"turnId"`
	Limit     int    `query:"limit"`
}

type listDesignGenerationAttemptsOutput struct {
	Body []designGenerationAttemptDTO
}

type getDesignGenerationAttemptInput struct {
	SessionID string `path:"sessionId"`
	AttemptID string `path:"attemptId"`
}

type cancelDesignGenerationAttemptInput struct {
	SessionID string `path:"sessionId"`
	AttemptID string `path:"attemptId"`
	Body      struct {
		ClientRequestID string `json:"clientRequestId" required:"true" minLength:"1" maxLength:"128"`
	}
}

type useDesignPlanInput struct {
	SessionID string `path:"sessionId"`
	AttemptID string `path:"attemptId"`
	Body      struct {
		ClientRequestID           string `json:"clientRequestId" required:"true" minLength:"1" maxLength:"128"`
		PlanFingerprint           string `json:"planFingerprint" required:"true" minLength:"1"`
		ExpectedRoomDraftRevision int64  `json:"expectedRoomDraftRevision"`
	}
}

type useDesignPlanOutput struct {
	Body struct {
		Acceptance designAcceptanceDTO `json:"acceptance"`
		RoomDraft  roomDraftDTO        `json:"roomDraft"`
	}
}

const (
	defaultListDesignGenerationAttemptsLimit = 10
	maxListDesignGenerationAttemptsLimit     = 50
)

// RegisterDesignGenerationHandlers registers RP4E2's six public design-
// generation routes on api, backed by svc. Called from RegisterHandlers
// alongside every other spatial route — there is no separate auth surface.
func RegisterDesignGenerationHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-generation-confirm",
		Method:      http.MethodPost,
		Path:        "/spatial/design-sessions/{sessionId}/turns/{turnId}/confirm",
		Summary:     "Confirm a validated plan, creating one durable generation attempt (RP4E2)",
	}, func(ctx context.Context, input *confirmDesignPlanInput) (*designGenerationAttemptOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		attempt, _, err := svc.ConfirmDesignPlan(ctx, principal.CompanyID, principal.UserID, input.SessionID, input.TurnID, ConfirmDesignPlanInput{
			ClientRequestID: input.Body.ClientRequestID, PlanFingerprint: input.Body.PlanFingerprint,
			ExpectedRoomDraftRevision: input.Body.ExpectedRoomDraftRevision,
		})
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		return &designGenerationAttemptOutput{Body: toDesignGenerationAttemptDTO(attempt)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-generation-regenerate",
		Method:      http.MethodPost,
		Path:        "/spatial/design-sessions/{sessionId}/turns/{turnId}/regenerate",
		Summary:     "Create a new generation attempt for the same validated plan, no GLM call (RP4E2)",
	}, func(ctx context.Context, input *regenerateDesignPlanInput) (*designGenerationAttemptOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		attempt, _, err := svc.RegenerateDesignPlan(ctx, principal.CompanyID, principal.UserID, input.SessionID, input.TurnID, RegenerateDesignPlanInput{
			ClientRequestID: input.Body.ClientRequestID, PlanFingerprint: input.Body.PlanFingerprint,
			ExpectedRoomDraftRevision: input.Body.ExpectedRoomDraftRevision,
		})
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		return &designGenerationAttemptOutput{Body: toDesignGenerationAttemptDTO(attempt)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-generation-attempts-list",
		Method:      http.MethodGet,
		Path:        "/spatial/design-sessions/{sessionId}/generation-attempts",
		Summary:     "List a design session's generation attempts, newest first (RP4E2)",
	}, func(ctx context.Context, input *listDesignGenerationAttemptsInput) (*listDesignGenerationAttemptsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		limit := input.Limit
		if limit <= 0 {
			limit = defaultListDesignGenerationAttemptsLimit
		}
		if limit > maxListDesignGenerationAttemptsLimit {
			limit = maxListDesignGenerationAttemptsLimit
		}
		attempts, err := svc.ListDesignGenerationAttempts(ctx, principal.CompanyID, input.SessionID, input.TurnID, limit)
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		dtos := make([]designGenerationAttemptDTO, 0, len(attempts))
		for _, a := range attempts {
			dtos = append(dtos, toDesignGenerationAttemptDTO(a))
		}
		return &listDesignGenerationAttemptsOutput{Body: dtos}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-generation-attempts-get",
		Method:      http.MethodGet,
		Path:        "/spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}",
		Summary:     "Get one generation attempt's public status/candidate (RP4E2)",
	}, func(ctx context.Context, input *getDesignGenerationAttemptInput) (*designGenerationAttemptOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		attempt, err := svc.GetDesignGenerationAttempt(ctx, principal.CompanyID, input.AttemptID)
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		if attempt.SessionID != input.SessionID {
			return nil, huma.Error404NotFound("design generation attempt not found")
		}
		return &designGenerationAttemptOutput{Body: toDesignGenerationAttemptDTO(attempt)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-generation-attempts-cancel",
		Method:      http.MethodPost,
		Path:        "/spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}/cancel",
		Summary:     "Abandon a generation attempt — idempotent, terminal abandonment wins (RP4E2)",
	}, func(ctx context.Context, input *cancelDesignGenerationAttemptInput) (*designGenerationAttemptOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		attempt, err := svc.CancelDesignGenerationAttempt(ctx, principal.CompanyID, input.AttemptID, input.Body.ClientRequestID)
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		if attempt.SessionID != input.SessionID {
			return nil, huma.Error404NotFound("design generation attempt not found")
		}
		return &designGenerationAttemptOutput{Body: toDesignGenerationAttemptDTO(attempt)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-design-generation-attempts-use",
		Method:      http.MethodPost,
		Path:        "/spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}/use",
		Summary:     "Atomically apply a concept-ready attempt's candidate to the RoomDraft (RP4E2)",
	}, func(ctx context.Context, input *useDesignPlanInput) (*useDesignPlanOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		acceptance, draft, _, err := svc.UseDesignPlan(ctx, principal.CompanyID, principal.UserID, input.SessionID, input.AttemptID, UseDesignPlanInput{
			ClientRequestID: input.Body.ClientRequestID, PlanFingerprint: input.Body.PlanFingerprint,
			ExpectedRoomDraftRevision: input.Body.ExpectedRoomDraftRevision,
		})
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		out := &useDesignPlanOutput{}
		out.Body.Acceptance = toDesignAcceptanceDTO(acceptance)
		out.Body.RoomDraft = toRoomDraftDTO(draft)
		return out, nil
	})
}
