package spaces

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type spaceDTO struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type createSpaceInput struct {
	Body struct {
		ProjectID   string `json:"projectId" required:"true" minLength:"1"`
		Name        string `json:"name" required:"true" minLength:"1"`
		Type        string `json:"type,omitempty"`
		Description string `json:"description,omitempty"`
	}
}

type spaceOutput struct {
	Body spaceDTO
}

type listSpacesInput struct {
	ProjectID string `query:"projectId" required:"true"`
	Page      int    `query:"page"`
	PageSize  int    `query:"pageSize"`
	Search    string `query:"search"`
	Sort      string `query:"sort"`
	Order     string `query:"order"`
}

type listSpacesOutput struct {
	Body pagination.Response[spaceDTO]
}

type getSpaceInput struct {
	ID string `path:"id"`
}

type updateSpaceInput struct {
	ID   string `path:"id"`
	Body struct {
		Name        string `json:"name" required:"true" minLength:"1"`
		Type        string `json:"type,omitempty"`
		Description string `json:"description,omitempty"`
	}
}

// RegisterHandlers registers POST /spaces, GET /spaces?projectId=,
// GET /spaces/{id}, and PATCH /spaces/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "spaces-create",
		Method:      http.MethodPost,
		Path:        "/spaces",
		Summary:     "Create a Space under a Project belonging to the authenticated company",
	}, func(ctx context.Context, input *createSpaceInput) (*spaceOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		s, err := svc.CreateSpace(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.Name,
			input.Body.Type, input.Body.Description)
		if err != nil {
			return nil, mapSpacesError(err)
		}
		return &spaceOutput{Body: toSpaceDTO(s)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spaces-list-by-project",
		Method:      http.MethodGet,
		Path:        "/spaces",
		Summary:     "List Spaces for a Project belonging to the authenticated company, paginated",
	}, func(ctx context.Context, input *listSpacesInput) (*listSpacesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		req, err := pagination.ParseRequest(input.Page, input.PageSize, input.Search, input.Sort, input.Order,
			SpaceSortFields, SpaceDefaultSort, SpaceDefaultOrder)
		if err != nil {
			return nil, mapSpacesError(err)
		}

		list, total, err := svc.ListSpacesPaginated(ctx, principal.CompanyID, input.ProjectID, req)
		if err != nil {
			return nil, mapSpacesError(err)
		}
		dtos := make([]spaceDTO, 0, len(list))
		for _, s := range list {
			dtos = append(dtos, toSpaceDTO(s))
		}
		return &listSpacesOutput{Body: pagination.NewResponse(dtos, req, total)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spaces-get",
		Method:      http.MethodGet,
		Path:        "/spaces/{id}",
		Summary:     "Get a Space, tenant-scoped",
	}, func(ctx context.Context, input *getSpaceInput) (*spaceOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		s, err := svc.GetSpace(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSpacesError(err)
		}
		return &spaceOutput{Body: toSpaceDTO(s)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spaces-update",
		Method:      http.MethodPatch,
		Path:        "/spaces/{id}",
		Summary:     "Update a Space, tenant-scoped",
	}, func(ctx context.Context, input *updateSpaceInput) (*spaceOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		s, err := svc.UpdateSpace(ctx, principal.CompanyID, input.ID, input.Body.Name,
			input.Body.Type, input.Body.Description)
		if err != nil {
			return nil, mapSpacesError(err)
		}
		return &spaceOutput{Body: toSpaceDTO(s)}, nil
	})
}

func toSpaceDTO(s Space) spaceDTO {
	return spaceDTO{
		ID: s.ID, ProjectID: s.ProjectID, Name: s.Name, Type: s.Type,
		Description: s.Description, CreatedAt: s.CreatedAt.Format(timeLayout),
	}
}

// mapSpacesError maps spaces' sentinel errors to Huma HTTP errors.
func mapSpacesError(err error) error {
	switch {
	case errors.Is(err, ErrSpaceNotFound):
		return huma.Error404NotFound("space not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrNameRequired):
		return huma.Error422UnprocessableEntity("name is required")
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
