package properties

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type propertyDTO struct {
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	Address      string `json:"address"`
	PropertyType string `json:"propertyType,omitempty"`
	Notes        string `json:"notes,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

type createPropertyInput struct {
	Body struct {
		ProjectID    string `json:"projectId" required:"true" minLength:"1"`
		Address      string `json:"address" required:"true" minLength:"1"`
		PropertyType string `json:"propertyType,omitempty"`
		Notes        string `json:"notes,omitempty"`
	}
}

type propertyOutput struct {
	Body propertyDTO
}

type listPropertiesInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type listPropertiesOutput struct {
	Body struct {
		Properties []propertyDTO `json:"properties"`
	}
}

type getPropertyInput struct {
	ID string `path:"id"`
}

type updatePropertyInput struct {
	ID   string `path:"id"`
	Body struct {
		Address      string `json:"address" required:"true" minLength:"1"`
		PropertyType string `json:"propertyType,omitempty"`
		Notes        string `json:"notes,omitempty"`
	}
}

// RegisterHandlers registers POST /properties, GET /properties?projectId=,
// GET /properties/{id}, and PATCH /properties/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "properties-create",
		Method:      http.MethodPost,
		Path:        "/properties",
		Summary:     "Create a Property under a Project belonging to the authenticated company (at most one per Project)",
	}, func(ctx context.Context, input *createPropertyInput) (*propertyOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.CreateProperty(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.Address,
			input.Body.PropertyType, input.Body.Notes)
		if err != nil {
			return nil, mapPropertiesError(err)
		}
		return &propertyOutput{Body: toPropertyDTO(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "properties-list-by-project",
		Method:      http.MethodGet,
		Path:        "/properties",
		Summary:     "List Properties (0 or 1) for a Project belonging to the authenticated company",
	}, func(ctx context.Context, input *listPropertiesInput) (*listPropertiesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		list, err := svc.ListPropertiesByProject(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapPropertiesError(err)
		}
		resp := &listPropertiesOutput{}
		for _, p := range list {
			resp.Body.Properties = append(resp.Body.Properties, toPropertyDTO(p))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "properties-get",
		Method:      http.MethodGet,
		Path:        "/properties/{id}",
		Summary:     "Get a Property, tenant-scoped",
	}, func(ctx context.Context, input *getPropertyInput) (*propertyOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.GetProperty(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapPropertiesError(err)
		}
		return &propertyOutput{Body: toPropertyDTO(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "properties-update",
		Method:      http.MethodPatch,
		Path:        "/properties/{id}",
		Summary:     "Update a Property, tenant-scoped",
	}, func(ctx context.Context, input *updatePropertyInput) (*propertyOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.UpdateProperty(ctx, principal.CompanyID, input.ID, input.Body.Address,
			input.Body.PropertyType, input.Body.Notes)
		if err != nil {
			return nil, mapPropertiesError(err)
		}
		return &propertyOutput{Body: toPropertyDTO(p)}, nil
	})
}

func toPropertyDTO(p Property) propertyDTO {
	return propertyDTO{
		ID: p.ID, ProjectID: p.ProjectID, Address: p.Address,
		PropertyType: p.PropertyType, Notes: p.Notes, CreatedAt: p.CreatedAt.Format(timeLayout),
	}
}

// mapPropertiesError maps properties' sentinel errors to Huma HTTP errors.
// ErrProjectAlreadyHasProperty → 409 (design spec §1.3, §5.3), mirroring
// companies.ErrUserAlreadyHasMembership → 409 exactly.
func mapPropertiesError(err error) error {
	switch {
	case errors.Is(err, ErrPropertyNotFound):
		return huma.Error404NotFound("property not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrProjectAlreadyHasProperty):
		return huma.Error409Conflict("project already has a property")
	case errors.Is(err, ErrAddressRequired):
		return huma.Error422UnprocessableEntity("address is required")
	default:
		return err
	}
}
