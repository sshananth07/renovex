package ai

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// Stable RFC problem `type` identifiers (design doc §9.2 "Public AI
// generation/history endpoints"). Frontend/log code switches on these, not
// on English `detail` text.
const (
	problemTypeGenerationInProgress = "urn:renovex:problem:ai-generation-in-progress"
	problemTypeIdempotencyConflict  = "urn:renovex:problem:ai-idempotency-conflict"
	problemTypeProviderUnavailable  = "urn:renovex:problem:ai-provider-unavailable"
	problemTypePrerequisite         = "urn:renovex:problem:ai-prerequisite"
	problemTypeStaleSuggestion      = "urn:renovex:problem:ai-stale-suggestion"
	problemTypeWorkItemDuplicate    = "urn:renovex:problem:ai-work-item-duplicate"
	problemTypeResourceDuplicate    = "urn:renovex:problem:ai-resource-duplicate"
)

type batchDTO struct {
	ID               string `json:"id"`
	ProjectID        string `json:"projectId"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	PromptVersion    string `json:"promptVersion"`
	SchemaVersion    int    `json:"schemaVersion"`
	InputFingerprint string `json:"inputFingerprint"`
	// SourceBrief is the trimmed brief this batch was generated from
	// (generation provenance) — used by the frontend to detect whether the
	// project's current brief has materially changed since the last run
	// (T1.5 PART C §14-15), never treated as a second authoritative brief.
	SourceBrief string  `json:"sourceBrief,omitempty"`
	StartedAt   string  `json:"startedAt"`
	CompletedAt *string `json:"completedAt,omitempty"`
	FailedAt    *string `json:"failedAt,omitempty"`
	ErrorCode   string  `json:"errorCode,omitempty"`
}

type suggestionDTO struct {
	ID                     string   `json:"id"`
	BatchID                string   `json:"batchId"`
	Type                   string   `json:"type"`
	Status                 string   `json:"status"`
	Confidence             *float64 `json:"confidence,omitempty"`
	Rationale              string   `json:"rationale,omitempty"`
	SuggestedData          any      `json:"suggestedData"`
	AcceptedDomainObjectID string   `json:"acceptedDomainObjectId,omitempty"`
	Revision               int64    `json:"revision"`
	CreatedAt              string   `json:"createdAt"`
	// DeltaClassification is computed fresh on every read, never persisted
	// — new/changed/unchanged/conflict/suppressed (T1.5 PART C). Empty
	// when not computed (non-Space suggestion, or nothing pending in the
	// batch to classify).
	DeltaClassification    string `json:"deltaClassification,omitempty"`
	CurrentAuthoritativeID string `json:"currentAuthoritativeId,omitempty"`
}

type aiGenerateInput struct {
	ProjectID string `path:"projectId"`
	Body      struct {
		OperationID string `json:"operationId" required:"true" minLength:"1"`
	}
}

type aiGenerateOutput struct {
	Body struct {
		Batch       batchDTO        `json:"batch"`
		Suggestions []suggestionDTO `json:"suggestions"`
	}
}

// RegisterHandlers registers the public AI generation/history endpoints on
// api, backed by svc. These are the ONLY public AI routes — no unauthenticated
// route exists, and none accept companyId as input (design doc §9).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "ai-space-suggestions-generate",
		Method:      http.MethodPost,
		Path:        "/projects/{projectId}/ai/space-suggestions",
		Summary:     "Generate AI Space suggestions for a Project",
	}, func(ctx context.Context, input *aiGenerateInput) (*aiGenerateOutput, error) {
		return handleGenerate(ctx, input, svc.SuggestSpaces)
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-work-item-suggestions-generate",
		Method:      http.MethodPost,
		Path:        "/projects/{projectId}/ai/work-item-suggestions",
		Summary:     "Generate AI Work Item suggestions for a Project",
	}, func(ctx context.Context, input *aiGenerateInput) (*aiGenerateOutput, error) {
		return handleGenerate(ctx, input, svc.SuggestWorkItems)
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-resource-suggestions-generate",
		Method:      http.MethodPost,
		Path:        "/projects/{projectId}/ai/resource-suggestions",
		Summary:     "Generate AI Resource suggestions for a Project",
	}, func(ctx context.Context, input *aiGenerateInput) (*aiGenerateOutput, error) {
		return handleGenerate(ctx, input, svc.SuggestResources)
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-batches-list",
		Method:      http.MethodGet,
		Path:        "/projects/{projectId}/ai/batches",
		Summary:     "List AI generation batches for a Project, newest first",
	}, func(ctx context.Context, input *listBatchesInput) (*listBatchesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListBatches(ctx, principal.CompanyID, input.ProjectID, BatchType(input.Type))
		if err != nil {
			return nil, mapHandlerError(err)
		}
		resp := &listBatchesOutput{}
		for _, b := range list {
			resp.Body.Batches = append(resp.Body.Batches, toBatchDTO(b))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-suggestions-list-by-batch",
		Method:      http.MethodGet,
		Path:        "/ai-batches/{batchId}/suggestions",
		Summary:     "List AI suggestions for a batch, all lifecycle statuses",
	}, func(ctx context.Context, input *listSuggestionsInput) (*listSuggestionsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListSuggestionsByBatchWithDelta(ctx, principal.CompanyID, input.BatchID)
		if err != nil {
			return nil, mapHandlerError(err)
		}
		resp := &listSuggestionsOutput{}
		for _, d := range list {
			resp.Body.Suggestions = append(resp.Body.Suggestions, toSuggestionDTOWithDelta(d))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-suggestions-accept",
		Method:      http.MethodPost,
		Path:        "/ai-suggestions/{id}/accept",
		Summary:     "Accept (or edit & accept) a pending AI suggestion, creating the real domain object",
	}, func(ctx context.Context, input *acceptInput) (*acceptOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		suggestion, err := svc.repo.FindSuggestionByID(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapHandlerError(err)
		}

		var domainObjectID string
		switch suggestion.Type {
		case SuggestionTypeSpace:
			var override *SpaceAcceptanceOverride
			if input.Body.Space != nil {
				override = &SpaceAcceptanceOverride{
					Name: input.Body.Space.Name, SpaceType: input.Body.Space.Type, Description: input.Body.Space.Description,
				}
			}
			result, err := svc.AcceptSpaceSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, override)
			if err != nil {
				return nil, mapHandlerError(err)
			}
			domainObjectID = result.ID
		case SuggestionTypeWorkItem:
			if input.Body.WorkItem == nil {
				return nil, huma.Error422UnprocessableEntity("workItem decision is required to accept a work item suggestion")
			}
			var spaceID *string
			if input.Body.WorkItem.SpaceID != "" {
				spaceID = &input.Body.WorkItem.SpaceID
			}
			result, err := svc.AcceptWorkItemSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, WorkItemAcceptanceInput{
				Description: input.Body.WorkItem.Description, WorkType: input.Body.WorkItem.WorkType,
				ScopeLevel: ScopeLevel(input.Body.WorkItem.ScopeLevel), SpaceID: spaceID,
				QuantityValue: input.Body.WorkItem.QuantityValue, QuantityUnit: input.Body.WorkItem.QuantityUnit,
				AllowDuplicate: input.Body.AllowDuplicate,
			})
			if err != nil {
				return nil, mapHandlerError(err)
			}
			domainObjectID = result.ID
		case SuggestionTypeMaterialResource:
			if input.Body.Material == nil {
				return nil, huma.Error422UnprocessableEntity("material decision is required to accept a material resource suggestion")
			}
			materialInput := MaterialResourceAcceptanceInput{Mode: MaterialAcceptanceMode(input.Body.Material.Mode), MaterialID: input.Body.Material.MaterialID}
			if input.Body.Material.NewMaterial != nil {
				nm := input.Body.Material.NewMaterial
				materialInput.NewMaterial = &NewMaterialInput{
					Name: nm.Name, Category: nm.Category, Specification: nm.Specification, Unit: nm.Unit,
					ReferencePriceAmount: nm.ReferencePriceAmount, ReferencePriceCurrency: nm.ReferencePriceCurrency,
				}
			}
			result, err := svc.AcceptMaterialResourceSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, materialInput)
			if err != nil {
				return nil, mapHandlerError(err)
			}
			domainObjectID = result.RequirementID
		case SuggestionTypeTradeResource, SuggestionTypeEquipmentResource:
			editedName := ""
			if input.Body.Resource != nil {
				editedName = input.Body.Resource.Name
			}
			result, err := svc.AcceptTradeOrEquipmentSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, editedName)
			if err != nil {
				return nil, mapHandlerError(err)
			}
			domainObjectID = result.RequirementID
		default:
			return nil, huma.Error422UnprocessableEntity("unknown suggestion type")
		}

		out := &acceptOutput{}
		out.Body.DomainObjectID = domainObjectID
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-suggestions-reject",
		Method:      http.MethodPost,
		Path:        "/ai-suggestions/{id}/reject",
		Summary:     "Reject a pending AI suggestion, creating nothing",
	}, func(ctx context.Context, input *rejectInput) (*struct{}, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		if err := svc.RejectSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision); err != nil {
			return nil, mapHandlerError(err)
		}
		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-suggestions-use-delta",
		Method:      http.MethodPost,
		Path:        "/ai-suggestions/{id}/use-delta",
		Summary:     "Apply a CHANGED rerun suggestion to its linked authoritative entity in place",
	}, func(ctx context.Context, input *useDeltaInput) (*acceptOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		suggestion, err := svc.repo.FindSuggestionByID(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapHandlerError(err)
		}

		var domainObjectID string
		switch suggestion.Type {
		case SuggestionTypeSpace:
			result, err := svc.UseSpaceDeltaSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, input.Body.CurrentAuthoritativeID)
			if err != nil {
				return nil, mapHandlerError(err)
			}
			domainObjectID = result.ID
		case SuggestionTypeWorkItem:
			result, err := svc.UseWorkItemDeltaSuggestion(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, input.Body.CurrentAuthoritativeID)
			if err != nil {
				return nil, mapHandlerError(err)
			}
			domainObjectID = result.ID
		default:
			// Resources never reach CHANGED (ClassifyResourceDelta returns
			// CONFLICT instead — see reconcile.go) since workresources has
			// no update-in-place path; "Use suggestion" is not offered for
			// them by the frontend, and this route rejects it defensively.
			return nil, huma.Error422UnprocessableEntity("use-delta is not supported for this suggestion type")
		}

		out := &acceptOutput{}
		out.Body.DomainObjectID = domainObjectID
		return out, nil
	})
}

type useDeltaInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision       int64  `json:"expectedRevision" required:"true"`
		CurrentAuthoritativeID string `json:"currentAuthoritativeId" required:"true" minLength:"1"`
	}
}

type spaceAcceptInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type workItemAcceptInput struct {
	Description   string `json:"description"`
	WorkType      string `json:"workType,omitempty"`
	ScopeLevel    string `json:"scopeLevel"`
	SpaceID       string `json:"spaceId,omitempty"`
	QuantityValue string `json:"quantityValue"`
	QuantityUnit  string `json:"quantityUnit"`
}

type newMaterialAcceptInput struct {
	Name                   string `json:"name"`
	Category               string `json:"category,omitempty"`
	Specification          string `json:"specification,omitempty"`
	Unit                   string `json:"unit"`
	ReferencePriceAmount   int64  `json:"referencePriceAmount"`
	ReferencePriceCurrency string `json:"referencePriceCurrency"`
}

type materialAcceptInput struct {
	Mode        string                  `json:"mode"`
	MaterialID  *string                 `json:"materialId,omitempty"`
	NewMaterial *newMaterialAcceptInput `json:"newMaterial,omitempty"`
}

type resourceAcceptInput struct {
	Name string `json:"name,omitempty"`
}

type acceptInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64                `json:"expectedRevision" required:"true"`
		AllowDuplicate   bool                 `json:"allowDuplicate,omitempty"`
		Space            *spaceAcceptInput    `json:"space,omitempty"`
		WorkItem         *workItemAcceptInput `json:"workItem,omitempty"`
		Material         *materialAcceptInput `json:"material,omitempty"`
		Resource         *resourceAcceptInput `json:"resource,omitempty"`
	}
}

type acceptOutput struct {
	Body struct {
		DomainObjectID string `json:"domainObjectId"`
	}
}

type rejectInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type generateFunc func(ctx context.Context, companyID, projectID, userID, operationID string) (GenerationResult, error)

func handleGenerate(ctx context.Context, input *aiGenerateInput, fn generateFunc) (*aiGenerateOutput, error) {
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}

	result, err := fn(ctx, principal.CompanyID, input.ProjectID, principal.UserID, input.Body.OperationID)
	if err != nil {
		return nil, mapHandlerError(err)
	}

	out := &aiGenerateOutput{}
	out.Body.Batch = toBatchDTO(result.Batch)
	for _, s := range result.Suggestions {
		out.Body.Suggestions = append(out.Body.Suggestions, toSuggestionDTO(s))
	}
	return out, nil
}

type listBatchesInput struct {
	ProjectID string `path:"projectId"`
	Type      string `query:"type"`
}

type listBatchesOutput struct {
	Body struct {
		Batches []batchDTO `json:"batches"`
	}
}

type listSuggestionsInput struct {
	BatchID string `path:"batchId"`
}

type listSuggestionsOutput struct {
	Body struct {
		Suggestions []suggestionDTO `json:"suggestions"`
	}
}

func toBatchDTO(b AIGenerationBatch) batchDTO {
	dto := batchDTO{
		ID: b.ID, ProjectID: b.ProjectID, Type: string(b.Type), Status: string(b.Status),
		Provider: b.Provider, Model: b.Model, PromptVersion: b.PromptVersion,
		SchemaVersion: b.SchemaVersion, InputFingerprint: b.InputFingerprint, SourceBrief: b.SourceBrief,
		StartedAt: b.StartedAt.Format(timeLayout), ErrorCode: b.ErrorCode,
	}
	if b.CompletedAt != nil {
		formatted := b.CompletedAt.Format(timeLayout)
		dto.CompletedAt = &formatted
	}
	if b.FailedAt != nil {
		formatted := b.FailedAt.Format(timeLayout)
		dto.FailedAt = &formatted
	}
	return dto
}

func toSuggestionDTO(s AISuggestion) suggestionDTO {
	return suggestionDTO{
		ID: s.ID, BatchID: s.BatchID, Type: string(s.Type), Status: string(s.Status),
		Confidence: s.Confidence, Rationale: s.Rationale,
		SuggestedData:          toSuggestedDataJSON(s.SuggestedData),
		AcceptedDomainObjectID: s.AcceptedDomainObjectID,
		Revision:               s.Revision, CreatedAt: s.CreatedAt.Format(timeLayout),
	}
}

func toSuggestionDTOWithDelta(d SuggestionDelta) suggestionDTO {
	dto := toSuggestionDTO(d.Suggestion)
	dto.DeltaClassification = string(d.Classification)
	dto.CurrentAuthoritativeID = d.CurrentAuthoritativeID
	return dto
}

// toSuggestedDataJSON flattens the discriminated SuggestedData to its one
// populated variant for the public response — the frontend switches on the
// parent suggestion's `type` field to know which shape to expect.
func toSuggestedDataJSON(d SuggestedData) any {
	switch {
	case d.Space != nil:
		return d.Space
	case d.WorkItem != nil:
		return d.WorkItem
	case d.MaterialResource != nil:
		return d.MaterialResource
	case d.TradeResource != nil:
		return d.TradeResource
	case d.EquipmentResource != nil:
		return d.EquipmentResource
	default:
		return struct{}{}
	}
}

// mapHandlerError maps internal/ai sentinels to Huma HTTP errors, using
// stable RFC problem `type` identifiers rather than English detail-string
// parsing (design doc §9.2).
func mapHandlerError(err error) error {
	switch {
	case errors.Is(err, ErrProjectNotFound), errors.Is(err, ErrBatchNotFound), errors.Is(err, ErrSuggestionNotFound),
		errors.Is(err, ErrSpaceAcceptanceNotFound), errors.Is(err, ErrWorkItemAcceptanceNotFound),
		errors.Is(err, ErrWorkItemAcceptanceSpaceNotFound), errors.Is(err, ErrResourceAcceptanceNotFound),
		errors.Is(err, ErrResourceAcceptanceWorkItemNotFound), errors.Is(err, ErrResourceAcceptanceMaterialNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, ErrScopeBriefEmpty):
		return withProblemType(huma.Error422UnprocessableEntity("project scope brief is required"), problemTypePrerequisite)
	case errors.Is(err, ErrNoConfirmedSpaces), errors.Is(err, ErrNoConfirmedWorkItems):
		return withProblemType(huma.Error422UnprocessableEntity("prerequisite not met"), problemTypePrerequisite)
	case errors.Is(err, ErrGenerationInProgress):
		return withProblemType(huma.Error409Conflict("generation already in progress"), problemTypeGenerationInProgress)
	case errors.Is(err, ErrGenerationIdempotencyConflict):
		return withProblemType(huma.Error409Conflict("operationId was already used with different input"), problemTypeIdempotencyConflict)
	case errors.Is(err, ErrInvalidProviderResponseFromAI):
		return huma.Error502BadGateway("AI suggestions are temporarily unavailable. Your existing project data has not been changed.")
	case errors.Is(err, ErrSuggestionRevisionMismatch):
		return withProblemType(huma.Error409Conflict("suggestion was already reviewed or has changed"), problemTypeStaleSuggestion)
	case errors.Is(err, ErrSpaceLikelyDuplicate):
		return withProblemType(huma.Error409Conflict("a matching Space already exists"), problemTypeStaleSuggestion)
	case errors.Is(err, ErrWorkItemAcceptanceQuantityRequired), errors.Is(err, ErrWorkItemAcceptanceInvalidScope),
		errors.Is(err, ErrResourceAcceptanceModeRequired):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, ErrWorkItemAcceptanceDuplicate):
		return withProblemType(huma.Error409Conflict("a similar Work Item already exists"), problemTypeWorkItemDuplicate)
	case errors.Is(err, ErrResourceAcceptanceDuplicate):
		return withProblemType(huma.Error409Conflict("a matching resource requirement already exists"), problemTypeResourceDuplicate)
	default:
		return withProblemType(
			huma.Error503ServiceUnavailable("AI suggestions are temporarily unavailable. Your existing project data has not been changed."),
			problemTypeProviderUnavailable,
		)
	}
}

// withProblemType sets the RFC 7807 `type` field on a huma.StatusError
// produced by one of the huma.ErrorNNN helpers, all of which return
// *huma.ErrorModel under the hood.
func withProblemType(err huma.StatusError, problemType string) huma.StatusError {
	if model, ok := err.(*huma.ErrorModel); ok {
		model.Type = problemType
	}
	return err
}
