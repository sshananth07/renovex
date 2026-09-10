package work

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/optional"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type workItemDTO struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"projectId"`
	SpaceID            string `json:"spaceId,omitempty"`
	Description        string `json:"description"`
	WorkType           string `json:"workType,omitempty"`
	QuantityValue      string `json:"quantityValue"`
	QuantityUnit       string `json:"quantityUnit"`
	Status             string `json:"status"`
	Source             string `json:"source"`
	VerificationStatus string `json:"verificationStatus"`
	CreatedAt          string `json:"createdAt"`
}

type createWorkItemInput struct {
	Body struct {
		ProjectID     string `json:"projectId" required:"true" minLength:"1"`
		SpaceID       string `json:"spaceId,omitempty"`
		Description   string `json:"description" required:"true" minLength:"1"`
		WorkType      string `json:"workType,omitempty"`
		QuantityValue string `json:"quantityValue" required:"true"`
		QuantityUnit  string `json:"quantityUnit" required:"true" minLength:"1"`
	}
}

type workItemOutput struct {
	Body workItemDTO
}

type listWorkItemsInput struct {
	ProjectID string `query:"projectId"`
	SpaceID   string `query:"spaceId"`
	Page      int    `query:"page"`
	PageSize  int    `query:"pageSize"`
	Search    string `query:"search"`
	Sort      string `query:"sort"`
	Order     string `query:"order"`
}

type listWorkItemsOutput struct {
	Body pagination.Response[workItemDTO]
}

type getWorkItemInput struct {
	ID string `path:"id"`
}

type updateWorkItemStatusInput struct {
	ID   string `path:"id"`
	Body struct {
		Status string `json:"status" required:"true"`
	}
}

// updateWorkItemInput's scalar Body fields are all *string with the same
// omitted/present-including-empty semantics as clients' updateClientInput.
// SpaceID uses optional.NullableString specifically because it needs to
// distinguish "omitted" (leave the current Space assignment unchanged) from
// "explicit null" (clear the assignment) from "a value" (assign to that
// Space) — the one field in this PATCH body that needs a genuine third
// state, matching optional.NullableString's Schema() output of a plain
// nullable string in the OpenAPI document, not an {Present,Value} wrapper
// shape.
type updateWorkItemInput struct {
	WorkItemID string `path:"workItemId"`
	Body       struct {
		Description   *string                 `json:"description,omitempty"`
		WorkType      *string                 `json:"workType,omitempty"`
		QuantityValue *string                 `json:"quantityValue,omitempty"`
		QuantityUnit  *string                 `json:"quantityUnit,omitempty"`
		SpaceID       optional.NullableString `json:"spaceId,omitempty"`
	}
}

// RegisterHandlers registers POST /work-items, GET /work-items (filtered by
// exactly one of ?projectId= or ?spaceId=), GET /work-items/{id}, and
// PATCH /work-items/{id}/status on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "work-items-create",
		Method:      http.MethodPost,
		Path:        "/work-items",
		Summary:     "Create a WorkItem under a Project (and optionally a Space) belonging to the authenticated company",
	}, func(ctx context.Context, input *createWorkItemInput) (*workItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var spaceID *string
		if input.Body.SpaceID != "" {
			spaceID = &input.Body.SpaceID
		}

		w, err := svc.CreateWorkItem(ctx, principal.CompanyID, input.Body.ProjectID, spaceID,
			input.Body.Description, input.Body.WorkType, input.Body.QuantityValue, input.Body.QuantityUnit)
		if err != nil {
			return nil, mapWorkError(err)
		}
		return &workItemOutput{Body: toWorkItemDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "work-items-list",
		Method:      http.MethodGet,
		Path:        "/work-items",
		Summary:     "List WorkItems for a Project or a Space belonging to the authenticated company, paginated",
	}, func(ctx context.Context, input *listWorkItemsInput) (*listWorkItemsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		if (input.ProjectID == "") == (input.SpaceID == "") {
			return nil, huma.Error422UnprocessableEntity("exactly one of projectId or spaceId query parameter is required")
		}

		req, err := pagination.ParseRequest(input.Page, input.PageSize, input.Search, input.Sort, input.Order,
			WorkItemSortFields, WorkItemDefaultSort, WorkItemDefaultOrder)
		if err != nil {
			return nil, mapWorkError(err)
		}

		var list []WorkItem
		var total int
		if input.SpaceID != "" {
			list, total, err = svc.ListWorkItemsPaginatedBySpace(ctx, principal.CompanyID, input.SpaceID, req)
		} else {
			list, total, err = svc.ListWorkItemsPaginatedByProject(ctx, principal.CompanyID, input.ProjectID, req)
		}
		if err != nil {
			return nil, mapWorkError(err)
		}

		dtos := make([]workItemDTO, 0, len(list))
		for _, w := range list {
			dtos = append(dtos, toWorkItemDTO(w))
		}
		return &listWorkItemsOutput{Body: pagination.NewResponse(dtos, req, total)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "work-items-get",
		Method:      http.MethodGet,
		Path:        "/work-items/{id}",
		Summary:     "Get a WorkItem, tenant-scoped",
	}, func(ctx context.Context, input *getWorkItemInput) (*workItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		w, err := svc.GetWorkItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapWorkError(err)
		}
		return &workItemOutput{Body: toWorkItemDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "work-items-update-status",
		Method:      http.MethodPatch,
		Path:        "/work-items/{id}/status",
		Summary:     "Update a WorkItem's status (planned -> cancelled only)",
	}, func(ctx context.Context, input *updateWorkItemStatusInput) (*workItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		w, err := svc.UpdateWorkItemStatus(ctx, principal.CompanyID, input.ID, WorkItemStatus(input.Body.Status))
		if err != nil {
			return nil, mapWorkError(err)
		}
		return &workItemOutput{Body: toWorkItemDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "work-items-update",
		Method:      http.MethodPatch,
		Path:        "/work-items/{workItemId}",
		Summary:     "Update a WorkItem's editable fields (description, workType, quantity, Space assignment)",
	}, func(ctx context.Context, input *updateWorkItemInput) (*workItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		w, err := svc.UpdateWorkItem(ctx, principal.CompanyID, input.WorkItemID, WorkItemPatch{
			Description:   input.Body.Description,
			WorkType:      input.Body.WorkType,
			QuantityValue: input.Body.QuantityValue,
			QuantityUnit:  input.Body.QuantityUnit,
			SpaceID:       input.Body.SpaceID,
		})
		if err != nil {
			return nil, mapWorkError(err)
		}
		return &workItemOutput{Body: toWorkItemDTO(w)}, nil
	})
}

func toWorkItemDTO(w WorkItem) workItemDTO {
	dto := workItemDTO{
		ID: w.ID, ProjectID: w.ProjectID, Description: w.Description, WorkType: w.WorkType,
		QuantityValue: w.Quantity.Value.String(), QuantityUnit: w.Quantity.Unit,
		Status: string(w.Status), Source: string(w.Source), VerificationStatus: string(w.VerificationStatus),
		CreatedAt: w.CreatedAt.Format(timeLayout),
	}
	if w.SpaceID != nil {
		dto.SpaceID = *w.SpaceID
	}
	return dto
}

// mapWorkError maps work's sentinel errors to Huma HTTP errors. ErrProjectNotFound
// and ErrSpaceNotFound (including the lineage-mismatch case, design spec §10.3)
// both map to 404 — a foreign or lineage-mismatched real ID behaves exactly like
// a nonexistent one.
func mapWorkError(err error) error {
	switch {
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrSpaceNotFound):
		return huma.Error404NotFound("space not found")
	case errors.Is(err, ErrDescriptionRequired):
		return huma.Error422UnprocessableEntity("description is required")
	case errors.Is(err, ErrInvalidQuantity):
		return huma.Error422UnprocessableEntity("quantity must be a positive number with a non-empty unit")
	case errors.Is(err, ErrInvalidStatus):
		return huma.Error422UnprocessableEntity("invalid work item status")
	case errors.Is(err, ErrCancelledIsTerminal):
		return huma.Error409Conflict("cancelled work items cannot change status")
	case errors.Is(err, pagination.ErrInvalidPage),
		errors.Is(err, pagination.ErrInvalidPageSize),
		errors.Is(err, pagination.ErrSearchTooLong),
		errors.Is(err, pagination.ErrUnsupportedSort),
		errors.Is(err, pagination.ErrInvalidOrderValue):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return err
	}
}
