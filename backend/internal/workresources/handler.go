package workresources

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// requirementDTO carries planning fields only — no cost, price, rate, or
// quantity field exists on this type (design doc §11.2, §29). No companyId
// field either: tenant scope comes from the authenticated principal, never
// from the request or response body.
type requirementDTO struct {
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	WorkItemID   string `json:"workItemId"`
	ResourceType string `json:"resourceType"`
	MaterialID   string `json:"materialId,omitempty"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Source       string `json:"source"`
	CreatedAt    string `json:"createdAt"`
}

type listRequirementsInput struct {
	ProjectID  string `query:"projectId" required:"true"`
	WorkItemID string `query:"workItemId"`
}

type listRequirementsOutput struct {
	Body struct {
		Requirements []requirementDTO `json:"requirements"`
	}
}

// RegisterHandlers registers GET /work-resource-requirements on api, backed
// by svc. This is a read-only endpoint in M8.5B-A — there is no public
// manual-create route yet (design doc §11, "Public read API").
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "work-resource-requirements-list",
		Method:      http.MethodGet,
		Path:        "/work-resource-requirements",
		Summary:     "List WorkResourceRequirements for a Project, optionally filtered to one WorkItem",
	}, func(ctx context.Context, input *listRequirementsInput) (*listRequirementsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var (
			list []WorkResourceRequirement
			err  error
		)
		if input.WorkItemID != "" {
			list, err = svc.ListByWorkItem(ctx, principal.CompanyID, input.ProjectID, input.WorkItemID)
		} else {
			list, err = svc.ListByProject(ctx, principal.CompanyID, input.ProjectID)
		}
		if err != nil {
			return nil, mapError(err)
		}

		resp := &listRequirementsOutput{}
		for _, r := range list {
			resp.Body.Requirements = append(resp.Body.Requirements, toRequirementDTO(r))
		}
		return resp, nil
	})
}

func toRequirementDTO(r WorkResourceRequirement) requirementDTO {
	dto := requirementDTO{
		ID: r.ID, ProjectID: r.ProjectID, WorkItemID: r.WorkItemID,
		ResourceType: string(r.ResourceType), Name: r.Name,
		Status: string(r.Status), Source: string(r.Source),
		CreatedAt: r.CreatedAt.Format(timeLayout),
	}
	if r.MaterialID != nil {
		dto.MaterialID = *r.MaterialID
	}
	return dto
}

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")
	case errors.Is(err, ErrRequirementDuplicate):
		return huma.Error409Conflict("a resource requirement already exists for this work item")
	default:
		return err
	}
}
