package spatial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type spatialCaptureDTO struct {
	ID              string `json:"id"`
	ProjectID       string `json:"projectId"`
	SpaceID         string `json:"spaceId"`
	Status          string `json:"status"`
	RoomVersionID   string `json:"roomVersionId,omitempty"`
	Provider        string `json:"provider"`
	CaptureNumber   int    `json:"captureNumber"`
	RoomDraftID     string `json:"roomDraftId,omitempty"`
	ClientCaptureID string `json:"clientCaptureId,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type spatialRoomVersionDTO struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	SpaceID   string `json:"spaceId"`
	CaptureID string `json:"captureId"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type spatialSpaceStateDTO struct {
	SpaceID              string `json:"spaceId"`
	CurrentRoomVersionID string `json:"currentRoomVersionId,omitempty"`
}

type spatialArtifactDTO struct {
	ID           string `json:"id"`
	CaptureID    string `json:"captureId"`
	Kind         string `json:"kind"`
	ContentType  string `json:"contentType"`
	DeclaredSize int64  `json:"declaredSize"`
	ActualSize   int64  `json:"actualSize,omitempty"`
	Status       string `json:"status"`
	CreatedAt    string `json:"createdAt"`
	UploadedAt   string `json:"uploadedAt,omitempty"`
}

type requestArtifactUploadInput struct {
	Body struct {
		CaptureID    string `json:"captureId" required:"true" minLength:"1"`
		Kind         string `json:"kind" required:"true" minLength:"1"`
		ContentType  string `json:"contentType" required:"true" minLength:"1"`
		DeclaredSize int64  `json:"declaredSize" required:"true"`
		Checksum     string `json:"checksum" required:"true" minLength:"1"`
	}
}

type artifactUploadOutput struct {
	Body struct {
		Artifact    spatialArtifactDTO `json:"artifact"`
		UploadToken string             `json:"uploadToken"`
	}
}

type resumeArtifactUploadInput struct {
	ID string `path:"id"`
}

type finalizeArtifactUploadInput struct {
	ID   string `path:"id"`
	Body struct {
		UploadToken string `json:"uploadToken"`
	}
}

type artifactOutput struct {
	Body spatialArtifactDTO
}

type listArtifactsInput struct {
	CaptureID string `query:"captureId" required:"true"`
}

type listArtifactsOutput struct {
	Body []spatialArtifactDTO
}

type putArtifactContentInput struct {
	ID          string `path:"id"`
	UploadToken string `header:"X-Spatial-Upload-Token" required:"true"`
	RawBody     []byte `contentType:"application/octet-stream"`
}

type putArtifactContentOutput struct{}

type startCaptureInput struct {
	Body struct {
		ProjectID string `json:"projectId" required:"true" minLength:"1"`
		SpaceID   string `json:"spaceId" required:"true" minLength:"1"`
		// Provider is optional; empty defaults to "roomplan" (Service.StartCapture).
		Provider string `json:"provider,omitempty"`
		// ClientCaptureID is an optional idempotency key (plan §RP3.5/§RP4B0)
		// — a retry with the same value returns the original capture instead
		// of creating a duplicate. Empty for callers with no local stable
		// capture identity.
		ClientCaptureID string `json:"clientCaptureId,omitempty"`
	}
}

type captureOutput struct {
	Body spatialCaptureDTO
}

type getCaptureInput struct {
	ID string `path:"id"`
}

type advanceCaptureInput struct {
	ID   string `path:"id"`
	Body struct {
		Status string `json:"status" required:"true" minLength:"1"`
	}
}

type confirmCaptureInput struct {
	ID string `path:"id"`
}

type listCapturesInput struct {
	SpaceID string `query:"spaceId" required:"true"`
}

type listCapturesOutput struct {
	Body []spatialCaptureDTO
}

type getSpaceStateInput struct {
	ProjectID string `query:"projectId" required:"true"`
	SpaceID   string `query:"spaceId" required:"true"`
}

type spaceStateOutput struct {
	Body spatialSpaceStateDTO
}

// --- RP4B: edit operation transport ---

type submitRoomDraftEditInput struct {
	ID   string `path:"id"`
	Body struct {
		// OperationID is the client-supplied idempotency key — a retry
		// carrying the same value adopts the original result rather than
		// re-applying the operation (plan §RP4B).
		OperationID string `json:"operationId" required:"true" minLength:"1"`
		// Kind is the EditOperationKind wire discriminator (e.g.
		// "move_corner", "add_opening" — see editoperation.go's full list).
		Kind string `json:"kind" required:"true" minLength:"1"`
		// Payload is the operation's own typed fields, exactly as the
		// canonical Go/Swift wire contract defines them for Kind.
		Payload          json.RawMessage `json:"payload" required:"true"`
		ExpectedRevision int64           `json:"expectedRevision"`
	}
}

type resetRoomDraftInput struct {
	ID   string `path:"id"`
	Body struct {
		OperationID      string `json:"operationId" required:"true" minLength:"1"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
}

// undoResetRoomDraftInput is the M8.5C reversible-editing patch's Undo
// Reset-to-Scan request (§11) — a distinct operationId (this undo's own
// idempotency key) plus resetOperationId identifying WHICH prior reset to
// undo, since a RoomDraft could in principle have more than one reset in
// its history.
type undoResetRoomDraftInput struct {
	ID   string `path:"id"`
	Body struct {
		OperationID      string `json:"operationId" required:"true" minLength:"1"`
		ResetOperationID string `json:"resetOperationId" required:"true" minLength:"1"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
}

type roomDraftEditResultDTO struct {
	RoomDraft roomDraftDTO           `json:"roomDraft"`
	Record    roomDraftEditRecordDTO `json:"record"`
	// Replayed is true when this response is the ORIGINAL result of an
	// operation already applied by a prior request sharing the same
	// operationId (plan §RP4B) — the operation was not re-applied.
	Replayed bool `json:"replayed"`
}

type roomDraftEditOutput struct {
	Body roomDraftEditResultDTO
}

type roomDraftEditRecordDTO struct {
	ID                string `json:"id"`
	RoomDraftID       string `json:"roomDraftId"`
	OperationID       string `json:"operationId"`
	OperationKind     string `json:"operationKind"`
	OperationPayload  string `json:"operationPayload"`
	BaseRevision      int64  `json:"baseRevision"`
	ResultingRevision int64  `json:"resultingRevision"`
	ActorUserID       string `json:"actorUserId,omitempty"`
	CreatedAt         string `json:"createdAt"`
}

// roomDraftDTO is the minimal RP4B response shape for the resulting
// authoritative RoomDraft — the geometry fields a caller needs to know the
// edit's effect, plus the identity/revision fields needed to submit the
// NEXT edit. Not a full admin/read API for RoomDraft (no GET /room-drafts/
// {id} route exists yet — out of RP4B's explicit scope, since a client
// reaching this endpoint already has the draft it read at its prior
// revision).
type roomDraftDTO struct {
	ID            string                  `json:"id"`
	CaptureID     string                  `json:"captureId"`
	Walls         []RoomDraftWall         `json:"walls"`
	Openings      []RoomDraftOpening      `json:"openings"`
	Objects       []RoomDraftObject       `json:"objects"`
	Fixtures      []RoomDraftFixture      `json:"fixtures"`
	ServicePoints []RoomDraftServicePoint `json:"servicePoints"`
	Constraints   []RoomDraftConstraint   `json:"constraints"`
	Revision      int64                   `json:"revision"`
	// CanResetToScan exposes the canonical Reset-to-Scan precondition
	// (RoomDraft.OriginalBaseline != nil) as a capability flag, WITHOUT
	// exposing OriginalBaseline itself (plan §RP4C2) — lets the Web editor
	// proactively hide/disable the Reset-to-Scan control instead of always
	// attempting the request and handling ErrRoomDraftHasNoBaseline's 422.
	CanResetToScan bool `json:"canResetToScan"`
}

type getRoomDraftInput struct {
	ID string `path:"id"`
}

type getRoomDraftOutput struct {
	Body roomDraftDTO
}

type listRoomDraftEditsInput struct {
	ID string `path:"id"`
}

type listRoomDraftEditsOutput struct {
	Body []roomDraftEditRecordDTO
}

func toRoomDraftDTO(d RoomDraft) roomDraftDTO {
	return roomDraftDTO{
		ID: d.ID, CaptureID: d.CaptureID, Walls: d.Walls, Openings: d.Openings, Objects: d.Objects,
		Fixtures: d.Fixtures, ServicePoints: d.ServicePoints, Constraints: d.Constraints, Revision: d.Revision,
		CanResetToScan: d.OriginalBaseline != nil,
	}
}

func toRoomDraftEditRecordDTO(rec RoomDraftEditRecord) roomDraftEditRecordDTO {
	return roomDraftEditRecordDTO{
		ID: rec.ID, RoomDraftID: rec.RoomDraftID, OperationID: rec.OperationID,
		OperationKind: rec.OperationKind, OperationPayload: rec.OperationPayload,
		BaseRevision: rec.BaseRevision, ResultingRevision: rec.ResultingRevision,
		ActorUserID: rec.ActorUserID, CreatedAt: rec.CreatedAt.Format(timeLayout),
	}
}

func toRoomDraftEditResultDTO(result SubmitEditOperationResult) roomDraftEditResultDTO {
	return roomDraftEditResultDTO{
		RoomDraft: toRoomDraftDTO(result.RoomDraft), Record: toRoomDraftEditRecordDTO(result.Record),
		Replayed: result.Replayed,
	}
}

// RegisterExternalHandlers registers the token-authenticated artifact
// content-proxy route on api. It must be mounted on the BASE
// (unauthenticated) API group, exactly like access.RegisterExternalHandlers
// and supplieraccess.RegisterHandlers — this route carries no contractor
// bearer token and is authenticated solely by the opaque upload token in
// the X-Spatial-Upload-Token header (design spec §11).
func RegisterExternalHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "spatial-artifacts-put-content",
		Method:      http.MethodPut,
		Path:        "/spatial/artifacts/{id}/content",
		Summary:     "Upload artifact bytes, authenticated by the artifact's upload token",
	}, func(ctx context.Context, input *putArtifactContentInput) (*putArtifactContentOutput, error) {
		err := svc.PutArtifactContent(ctx, input.ID, input.UploadToken, bytes.NewReader(input.RawBody))
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &putArtifactContentOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-visual-assets-get-content",
		Method:      http.MethodGet,
		Path:        "/spatial/visual-assets/content",
		Summary:     "Stream a published visual asset's bytes, authenticated by a short-lived bearer capability (RP4D)",
	}, func(ctx context.Context, input *getVisualAssetContentInput) (*huma.StreamResponse, error) {
		assetVersion, content, err := svc.StreamVisualAssetContent(ctx, input.Capability)
		if err != nil {
			// Deliberately generic: an invalid/expired/tampered capability
			// and "asset genuinely doesn't exist" are both surfaced as the
			// same 401, never distinguishing the exact cause in a way a
			// caller could turn into a signal (RP4D §6).
			return nil, huma.Error401Unauthorized("visual asset access capability invalid or expired")
		}
		return &huma.StreamResponse{
			Body: func(sctx huma.Context) {
				defer content.Close()
				sctx.SetHeader("Content-Type", assetVersion.ContentType)
				sctx.SetHeader("Content-Length", fmt.Sprintf("%d", assetVersion.ByteCount))
				sctx.SetHeader("ETag", fmt.Sprintf("%q", assetVersion.Checksum))
				sctx.SetHeader("Cache-Control", "private, no-store")
				_, _ = io.Copy(sctx.BodyWriter(), content)
			},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-asset-generation-source-image-get",
		Method:      http.MethodGet,
		Path:        "/spatial/asset-generation/source-image",
		Summary:     "Stream a tenant-authorized source image's bytes to the generation provider, capability-gated (RP4E0)",
	}, func(ctx context.Context, input *getAssetGenerationSourceImageInput) (*huma.StreamResponse, error) {
		artifact, content, err := svc.StreamAssetGenerationSourceImage(ctx, input.Capability)
		if err != nil {
			// Deliberately generic, matching spatial-visual-assets-get-content's
			// own convention: never distinguishing invalid/expired/tampered
			// from "doesn't exist" in the response.
			return nil, huma.Error401Unauthorized("asset generation source image access capability invalid or expired")
		}
		return &huma.StreamResponse{
			Body: func(sctx huma.Context) {
				defer content.Close()
				sctx.SetHeader("Content-Type", artifact.ContentType)
				sctx.SetHeader("Cache-Control", "private, no-store")
				_, _ = io.Copy(sctx.BodyWriter(), content)
			},
		}, nil
	})
}

type getVisualAssetContentInput struct {
	Capability string `query:"cap" required:"true"`
}

type getAssetGenerationSourceImageInput struct {
	Capability string `query:"cap" required:"true"`
}

// RegisterHandlers registers the M8.5C Task 2/3 spatial capture/room-version/
// artifact routes on api, backed by svc. Later tasks add observation and
// design-concept routes to this same package (design spec §40).
//
// RegisterDesignHandlers (RP4E1) is called separately by callers that want
// the conversational design-reasoning routes wired — composition wiring
// passes the operator-configured runtime notice text; call sites that
// register a nil svc (schema generation) skip it, matching this
// function's own signature (no runtime-notice text parameter to thread
// through every existing call site).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "spatial-captures-start",
		Method:      http.MethodPost,
		Path:        "/spatial/captures",
		Summary:     "Start a new spatial capture for a Space belonging to the authenticated company",
	}, func(ctx context.Context, input *startCaptureInput) (*captureOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		c, err := svc.StartCapture(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.SpaceID, CaptureProvider(input.Body.Provider), input.Body.ClientCaptureID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &captureOutput{Body: toCaptureDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-captures-get",
		Method:      http.MethodGet,
		Path:        "/spatial/captures/{id}",
		Summary:     "Get a spatial capture, tenant-scoped",
	}, func(ctx context.Context, input *getCaptureInput) (*captureOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		c, err := svc.GetCapture(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &captureOutput{Body: toCaptureDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-captures-list-by-space",
		Method:      http.MethodGet,
		Path:        "/spatial/captures",
		Summary:     "List spatial captures for a Space, most recent first",
	}, func(ctx context.Context, input *listCapturesInput) (*listCapturesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListCapturesBySpace(ctx, principal.CompanyID, input.SpaceID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		dtos := make([]spatialCaptureDTO, 0, len(list))
		for _, c := range list {
			dtos = append(dtos, toCaptureDTO(c))
		}
		return &listCapturesOutput{Body: dtos}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-captures-advance",
		Method:      http.MethodPost,
		Path:        "/spatial/captures/{id}/advance",
		Summary:     "Advance a spatial capture's lifecycle status",
	}, func(ctx context.Context, input *advanceCaptureInput) (*captureOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		c, err := svc.AdvanceCapture(ctx, principal.CompanyID, input.ID, CaptureStatus(input.Body.Status))
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &captureOutput{Body: toCaptureDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-captures-confirm",
		Method:      http.MethodPost,
		Path:        "/spatial/captures/{id}/confirm",
		Summary:     "Confirm a reviewed spatial capture into an immutable current RoomVersion",
	}, func(ctx context.Context, input *confirmCaptureInput) (*captureOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		c, err := svc.ConfirmCapture(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &captureOutput{Body: toCaptureDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-artifacts-request-upload",
		Method:      http.MethodPost,
		Path:        "/spatial/artifacts",
		Summary:     "Request an upload slot for a new spatial artifact",
	}, func(ctx context.Context, input *requestArtifactUploadInput) (*artifactUploadOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		artifact, token, err := svc.RequestArtifactUpload(ctx, principal.CompanyID, input.Body.CaptureID,
			ArtifactKind(input.Body.Kind), input.Body.ContentType, input.Body.DeclaredSize, input.Body.Checksum)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		out := &artifactUploadOutput{}
		out.Body.Artifact = toArtifactDTO(artifact)
		out.Body.UploadToken = token
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-artifacts-resume-upload",
		Method:      http.MethodPost,
		Path:        "/spatial/artifacts/{id}/resume",
		Summary:     "Reissue a fresh upload token for a pending artifact (resumable upload)",
	}, func(ctx context.Context, input *resumeArtifactUploadInput) (*artifactUploadOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		artifact, token, err := svc.ResumeArtifactUpload(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		out := &artifactUploadOutput{}
		out.Body.Artifact = toArtifactDTO(artifact)
		out.Body.UploadToken = token
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-artifacts-finalize-upload",
		Method:      http.MethodPost,
		Path:        "/spatial/artifacts/{id}/finalize",
		Summary:     "Finalize an artifact upload after bytes have been PUT to its content endpoint",
	}, func(ctx context.Context, input *finalizeArtifactUploadInput) (*artifactOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		artifact, err := svc.FinalizeArtifactUpload(ctx, principal.CompanyID, input.ID, input.Body.UploadToken)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &artifactOutput{Body: toArtifactDTO(artifact)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-artifacts-list-by-capture",
		Method:      http.MethodGet,
		Path:        "/spatial/artifacts",
		Summary:     "List spatial artifacts for a capture",
	}, func(ctx context.Context, input *listArtifactsInput) (*listArtifactsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListArtifactsByCapture(ctx, principal.CompanyID, input.CaptureID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		dtos := make([]spatialArtifactDTO, 0, len(list))
		for _, a := range list {
			dtos = append(dtos, toArtifactDTO(a))
		}
		return &listArtifactsOutput{Body: dtos}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-space-state-get",
		Method:      http.MethodGet,
		Path:        "/spatial/space-state",
		Summary:     "Get a Space's spatial state, including its current RoomVersion pointer",
	}, func(ctx context.Context, input *getSpaceStateInput) (*spaceStateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		state, err := svc.GetSpaceState(ctx, principal.CompanyID, input.ProjectID, input.SpaceID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &spaceStateOutput{Body: spatialSpaceStateDTO{
			SpaceID: state.SpaceID, CurrentRoomVersionID: state.CurrentRoomVersionID,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-room-draft-get",
		Method:      http.MethodGet,
		Path:        "/spatial/room-drafts/{id}",
		Summary:     "Get the current authoritative RoomDraft and revision (plan §RP4C1)",
	}, func(ctx context.Context, input *getRoomDraftInput) (*getRoomDraftOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		draft, err := svc.GetRoomDraft(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &getRoomDraftOutput{Body: toRoomDraftDTO(draft)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-room-draft-edits-submit",
		Method:      http.MethodPost,
		Path:        "/spatial/room-drafts/{id}/edits",
		Summary:     "Submit one canonical EditOperation against a RoomDraft (plan §RP4B)",
	}, func(ctx context.Context, input *submitRoomDraftEditInput) (*roomDraftEditOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		result, err := svc.SubmitEditOperation(ctx, principal.CompanyID, SubmitEditOperationInput{
			RoomDraftID: input.ID, OperationID: input.Body.OperationID,
			Kind: EditOperationKind(input.Body.Kind), Payload: input.Body.Payload,
			ExpectedRevision: input.Body.ExpectedRevision, ActorUserID: principal.UserID,
		})
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &roomDraftEditOutput{Body: toRoomDraftEditResultDTO(result)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-room-draft-reset-to-scan",
		Method:      http.MethodPost,
		Path:        "/spatial/room-drafts/{id}/reset-to-scan",
		Summary:     "Reset a RoomDraft to its original captured baseline through the canonical edit transport (plan §RP4B)",
	}, func(ctx context.Context, input *resetRoomDraftInput) (*roomDraftEditOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		result, err := svc.SubmitResetToScan(ctx, principal.CompanyID, input.ID, input.Body.OperationID, input.Body.ExpectedRevision, principal.UserID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &roomDraftEditOutput{Body: toRoomDraftEditResultDTO(result)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-room-draft-undo-reset-to-scan",
		Method:      http.MethodPost,
		Path:        "/spatial/room-drafts/{id}/undo-reset-to-scan",
		Summary:     "Undo a prior Reset-to-Scan, restoring the exact pre-reset RoomDraft state (M8.5C reversible-editing patch §11)",
	}, func(ctx context.Context, input *undoResetRoomDraftInput) (*roomDraftEditOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		result, err := svc.UndoResetToScan(ctx, principal.CompanyID, input.ID, input.Body.ResetOperationID, input.Body.OperationID, input.Body.ExpectedRevision, principal.UserID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &roomDraftEditOutput{Body: toRoomDraftEditResultDTO(result)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-room-draft-edits-list",
		Method:      http.MethodGet,
		Path:        "/spatial/room-drafts/{id}/edits",
		Summary:     "List the edit-operation audit history for a RoomDraft, most recent first (plan §RP4B)",
	}, func(ctx context.Context, input *listRoomDraftEditsInput) (*listRoomDraftEditsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		records, err := svc.ListRoomDraftEdits(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		dtos := make([]roomDraftEditRecordDTO, 0, len(records))
		for _, rec := range records {
			dtos = append(dtos, toRoomDraftEditRecordDTO(rec))
		}
		return &listRoomDraftEditsOutput{Body: dtos}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-visual-assets-create-access",
		Method:      http.MethodPost,
		Path:        "/spatial/visual-assets/{assetId}/versions/{version}/access",
		Summary:     "Mint a short-lived capability to read one published visual asset version's bytes (RP4D)",
	}, func(ctx context.Context, input *createVisualAssetAccessInput) (*createVisualAssetAccessOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		assetVersion, access, err := svc.CreateVisualAssetAccess(ctx, principal.CompanyID, input.AssetID, input.Version)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		out := &createVisualAssetAccessOutput{}
		out.CacheControl = "no-store"
		out.Body.Asset = toVisualAssetVersionDTO(assetVersion)
		out.Body.Access.URL = access.URL
		out.Body.Access.ExpiresAt = access.ExpiresAt.Format(timeLayout)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-asset-generation-jobs-submit",
		Method:      http.MethodPost,
		Path:        "/spatial/asset-generation-jobs",
		Summary:     "Submit (or idempotently adopt) a 3D asset generation job (RP4E0)",
	}, func(ctx context.Context, input *submitAssetGenerationJobInput) (*submitAssetGenerationJobOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		job, err := svc.SubmitAssetGenerationJob(ctx, principal.CompanyID, "", principal.UserID,
			input.Body.ClientRequestID, input.Body.SourceArtifactID, input.Body.Seed)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &submitAssetGenerationJobOutput{Body: toAssetGenerationJobDTO(job)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-asset-generation-jobs-get",
		Method:      http.MethodGet,
		Path:        "/spatial/asset-generation-jobs/{id}",
		Summary:     "Poll one asset generation job's current status (RP4E0)",
	}, func(ctx context.Context, input *getAssetGenerationJobInput) (*getAssetGenerationJobOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		job, err := svc.GetAssetGenerationJob(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return &getAssetGenerationJobOutput{Body: toAssetGenerationJobDTO(job)}, nil
	})
}

type createVisualAssetAccessInput struct {
	AssetID string `path:"assetId"`
	Version int    `path:"version"`
}

type visualAssetVersionDTO struct {
	AssetID       string                   `json:"assetId"`
	Version       int                      `json:"version"`
	Format        string                   `json:"format"`
	DisplayName   string                   `json:"displayName"`
	Normalization VisualAssetNormalization `json:"normalization"`
	ContentType   string                   `json:"contentType"`
	ByteCount     int64                    `json:"byteCount"`
	Checksum      string                   `json:"checksum"`
}

func toVisualAssetVersionDTO(v VisualAssetVersion) visualAssetVersionDTO {
	return visualAssetVersionDTO{
		AssetID: v.AssetID, Version: v.Version, Format: string(v.Format),
		DisplayName: v.DisplayName, Normalization: v.Normalization,
		ContentType: v.ContentType, ByteCount: v.ByteCount, Checksum: v.Checksum,
	}
}

type createVisualAssetAccessOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         struct {
		Asset  visualAssetVersionDTO `json:"asset"`
		Access struct {
			URL       string `json:"url"`
			ExpiresAt string `json:"expiresAt"`
		} `json:"access"`
	}
}

type submitAssetGenerationJobInput struct {
	Body struct {
		ClientRequestID  string `json:"clientRequestId" required:"true" minLength:"1"`
		SourceArtifactID string `json:"sourceArtifactId" required:"true" minLength:"1"`
		Seed             int64  `json:"seed"`
	}
}

// assetGenerationJobDTO deliberately never exposes GeneratedObjectKey or
// ProviderRequestID — both are infrastructure-only per RP4E0's own
// invariant (never in API DTOs, never logged).
type assetGenerationJobDTO struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	FailureCode    string `json:"failureCode,omitempty"`
	FailureMessage string `json:"failureMessage,omitempty"`
	ResultAssetID  string `json:"resultAssetId,omitempty"`
	ResultVersion  int    `json:"resultVersion,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

func toAssetGenerationJobDTO(j SpatialAssetGenerationJob) assetGenerationJobDTO {
	return assetGenerationJobDTO{
		ID: j.ID, Status: string(j.Execution.Status),
		FailureCode: j.Execution.FailureCode, FailureMessage: j.Execution.FailureMessage,
		ResultAssetID: j.ResultAssetID, ResultVersion: j.ResultVersion,
		CreatedAt: j.CreatedAt.Format(timeLayout),
	}
}

type submitAssetGenerationJobOutput struct {
	Body assetGenerationJobDTO
}

type getAssetGenerationJobInput struct {
	ID string `path:"id"`
}

type getAssetGenerationJobOutput struct {
	Body assetGenerationJobDTO
}

func toArtifactDTO(a SpatialArtifact) spatialArtifactDTO {
	dto := spatialArtifactDTO{
		ID: a.ID, CaptureID: a.CaptureID, Kind: string(a.Kind), ContentType: a.ContentType,
		DeclaredSize: a.DeclaredSize, ActualSize: a.ActualSize, Status: string(a.Status),
		CreatedAt: a.CreatedAt.Format(timeLayout),
	}
	if a.UploadedAt != nil {
		dto.UploadedAt = a.UploadedAt.Format(timeLayout)
	}
	return dto
}

func toCaptureDTO(c SpatialCapture) spatialCaptureDTO {
	return spatialCaptureDTO{
		ID: c.ID, ProjectID: c.ProjectID, SpaceID: c.SpaceID, Status: string(c.Status),
		RoomVersionID: c.RoomVersionID, Provider: string(c.Provider), CaptureNumber: c.CaptureNumber,
		RoomDraftID: c.RoomDraftID, ClientCaptureID: c.ClientCaptureID,
		CreatedAt: c.CreatedAt.Format(timeLayout), UpdatedAt: c.UpdatedAt.Format(timeLayout),
	}
}

// spatialConflictError returns a 409 whose ErrorModel.Type carries a stable,
// machine-readable conflict code (e.g. "stale_revision",
// "operation_id_conflict") alongside the existing human-readable detail —
// scoped ONLY to the two RoomDraft-edit-submission 409s that need a
// discriminator distinct UX depends on (plan §RP4C1). Type is otherwise an
// unused RFC 9457 field in this codebase (every other huma.Error* call
// leaves it at its zero value), so this reuses an existing generated-schema
// field rather than introducing new error-model infrastructure or touching
// any other route's error mapping.
func spatialConflictError(detail, code string) error {
	return &huma.ErrorModel{
		Status: http.StatusConflict,
		Title:  http.StatusText(http.StatusConflict),
		Detail: detail,
		Type:   code,
	}
}

// mapSpatialError maps spatial's sentinel errors to Huma HTTP errors.
func mapSpatialError(err error) error {
	switch {
	case errors.Is(err, ErrCaptureNotFound):
		return huma.Error404NotFound("spatial capture not found")
	case errors.Is(err, ErrSpaceNotFound):
		return huma.Error404NotFound("space not found")
	case errors.Is(err, ErrRoomVersionNotFound):
		return huma.Error404NotFound("room version not found")
	case errors.Is(err, ErrIllegalCaptureTransition):
		return huma.Error409Conflict("illegal capture status transition")
	case errors.Is(err, ErrSpaceStateRevisionMismatch):
		return huma.Error409Conflict("space state changed since it was read")
	case errors.Is(err, ErrArtifactNotFound):
		return huma.Error404NotFound("spatial artifact not found")
	case errors.Is(err, ErrInvalidArtifactKind):
		return huma.Error422UnprocessableEntity("invalid artifact kind")
	case errors.Is(err, ErrInvalidArtifactContentType):
		return huma.Error422UnprocessableEntity("content type not allowed for spatial artifacts")
	case errors.Is(err, ErrArtifactTooLarge):
		return huma.Error422UnprocessableEntity("artifact exceeds maximum allowed size")
	case errors.Is(err, ErrArtifactUploadTokenInvalid):
		return huma.Error401Unauthorized("artifact access token invalid or expired")
	case errors.Is(err, ErrArtifactObjectMissing):
		return huma.Error409Conflict("artifact object not found in object store; upload may not have completed")
	case errors.Is(err, ErrArtifactChecksumMismatch):
		return huma.Error409Conflict("artifact checksum mismatch")
	case errors.Is(err, ErrClientCaptureIDConflict):
		return huma.Error409Conflict("clientCaptureId already used for a different space")
	case errors.Is(err, ErrRoomDraftNotFound):
		return huma.Error404NotFound("room draft not found")
	case errors.Is(err, ErrRoomDraftRevisionMismatch):
		return spatialConflictError("room draft changed since it was read", "stale_revision")
	case errors.Is(err, ErrRoomDraftHasNoBaseline):
		return huma.Error422UnprocessableEntity("room draft has no original baseline to reset to")
	case errors.Is(err, ErrNotAResetOperation):
		return huma.Error422UnprocessableEntity("referenced operation is not a reset-to-scan")
	case errors.Is(err, ErrRoomDraftEditRecordNotFound):
		return huma.Error404NotFound("referenced edit operation not found")
	case errors.Is(err, ErrOperationIDConflict):
		return spatialConflictError("operationId already used for a different edit operation", "operation_id_conflict")
	case errors.Is(err, ErrUnsupportedEditOperationKind):
		return huma.Error422UnprocessableEntity("unsupported edit operation kind")
	case errors.Is(err, ErrMalformedEditOperationPayload):
		return huma.Error400BadRequest("malformed edit operation payload")
	case errors.Is(err, ErrInvalidEditOperation):
		return huma.Error422UnprocessableEntity("invalid edit operation")
	case errors.Is(err, ErrEditTargetNotFound):
		return huma.Error404NotFound("edit operation target not found in room draft")
	case errors.Is(err, ErrInvalidElementProvenance):
		return huma.Error422UnprocessableEntity("invalid element provenance")
	case errors.Is(err, ErrVisualAssetVersionNotFound):
		return huma.Error404NotFound("visual asset version not found")
	case errors.Is(err, ErrInvalidVisualAssetRef):
		return huma.Error422UnprocessableEntity("invalid visual asset reference")
	case errors.Is(err, ErrInvalidVisualAssetContent):
		return huma.Error422UnprocessableEntity("invalid visual asset content")
	case errors.Is(err, ErrVisualAssetVersionConflict):
		return spatialConflictError("visual asset version already published with different content", "visual_asset_version_conflict")
	case errors.Is(err, ErrVisualAssetSupportNotConfigured):
		return huma.Error404NotFound("visual asset support not configured")
	case errors.Is(err, ErrAssetGenerationJobNotFound):
		return huma.Error404NotFound("asset generation job not found")
	case errors.Is(err, ErrAssetGenerationRequestFingerprintConflict):
		return spatialConflictError("clientRequestId already used for a different asset generation request", "asset_generation_request_conflict")
	case errors.Is(err, ErrAssetGenerationSourceInvalid):
		return huma.Error422UnprocessableEntity("source image is invalid for asset generation")
	case errors.Is(err, ErrAssetGenerationNotConfigured):
		return huma.Error404NotFound("asset generation support not configured")
	default:
		return err
	}
}
