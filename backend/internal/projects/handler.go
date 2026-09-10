package projects

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type projectDTO struct {
	ID         string `json:"id"`
	ClientID   string `json:"clientId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	ScopeBrief string `json:"scopeBrief"`
	CreatedAt  string `json:"createdAt"`
}

type createProjectInput struct {
	Body struct {
		ClientID string `json:"clientId" required:"true" minLength:"1"`
		Name     string `json:"name" required:"true" minLength:"1"`
	}
}

type projectOutput struct {
	Body projectDTO
}

type listProjectsInput struct {
	ClientID string `query:"clientId"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
	Search   string `query:"search"`
	Sort     string `query:"sort"`
	Order    string `query:"order"`
}

type listProjectsOutput struct {
	Body pagination.Response[projectDTO]
}

type getProjectInput struct {
	ID string `path:"id"`
}

type updateProjectStatusInput struct {
	ID   string `path:"id"`
	Body struct {
		Status string `json:"status" required:"true"`
	}
}

type updateProjectNameInput struct {
	ProjectID string `path:"projectId"`
	Body      struct {
		Name string `json:"name" required:"true" minLength:"1"`
	}
}

type updateProjectScopeBriefInput struct {
	ProjectID string `path:"projectId"`
	Body      struct {
		ScopeBrief string `json:"scopeBrief"`
	}
}

// RegisterHandlers registers POST/GET /projects, GET /projects/{id}, and
// PATCH /projects/{id}/status on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "projects-create",
		Method:      http.MethodPost,
		Path:        "/projects",
		Summary:     "Create a Project under a Client belonging to the authenticated company",
	}, func(ctx context.Context, input *createProjectInput) (*projectOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.CreateProject(ctx, principal.CompanyID, input.Body.ClientID, input.Body.Name)
		if err != nil {
			return nil, mapProjectsError(err)
		}
		return &projectOutput{Body: toProjectDTO(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "projects-list",
		Method:      http.MethodGet,
		Path:        "/projects",
		Summary:     "List Projects for the authenticated company, optionally filtered by clientId, paginated",
	}, func(ctx context.Context, input *listProjectsInput) (*listProjectsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		req, err := pagination.ParseRequest(input.Page, input.PageSize, input.Search, input.Sort, input.Order,
			ProjectSortFields, ProjectDefaultSort, ProjectDefaultOrder)
		if err != nil {
			return nil, mapProjectsError(err)
		}

		list, total, err := svc.ListProjectsPaginated(ctx, principal.CompanyID, input.ClientID, req)
		if err != nil {
			return nil, mapProjectsError(err)
		}
		dtos := make([]projectDTO, 0, len(list))
		for _, p := range list {
			dtos = append(dtos, toProjectDTO(p))
		}
		return &listProjectsOutput{Body: pagination.NewResponse(dtos, req, total)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "projects-get",
		Method:      http.MethodGet,
		Path:        "/projects/{id}",
		Summary:     "Get a Project, tenant-scoped",
	}, func(ctx context.Context, input *getProjectInput) (*projectOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.GetProject(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapProjectsError(err)
		}
		return &projectOutput{Body: toProjectDTO(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "projects-update-status",
		Method:      http.MethodPatch,
		Path:        "/projects/{id}/status",
		Summary:     "Update a Project's status",
	}, func(ctx context.Context, input *updateProjectStatusInput) (*projectOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.UpdateProjectStatus(ctx, principal.CompanyID, input.ID, ProjectStatus(input.Body.Status))
		if err != nil {
			return nil, mapProjectsError(err)
		}
		return &projectOutput{Body: toProjectDTO(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "projects-update-name",
		Method:      http.MethodPatch,
		Path:        "/projects/{projectId}",
		Summary:     "Update a Project's name (metadata only — status stays on PATCH /projects/{id}/status)",
	}, func(ctx context.Context, input *updateProjectNameInput) (*projectOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.UpdateProjectName(ctx, principal.CompanyID, input.ProjectID, input.Body.Name)
		if err != nil {
			return nil, mapProjectsError(err)
		}
		return &projectOutput{Body: toProjectDTO(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "projects-update-scope-brief",
		Method:      http.MethodPatch,
		Path:        "/projects/{projectId}/scope-brief",
		Summary:     "Update a Project's contractor-authored Scope Brief (metadata only)",
	}, func(ctx context.Context, input *updateProjectScopeBriefInput) (*projectOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		p, err := svc.UpdateProjectScopeBrief(ctx, principal.CompanyID, input.ProjectID, input.Body.ScopeBrief)
		if err != nil {
			return nil, mapProjectsError(err)
		}
		return &projectOutput{Body: toProjectDTO(p)}, nil
	})
}

func toProjectDTO(p Project) projectDTO {
	return projectDTO{
		ID: p.ID, ClientID: p.ClientID, Name: p.Name,
		Status: string(p.Status), ScopeBrief: p.ScopeBrief,
		CreatedAt: p.CreatedAt.Format(timeLayout),
	}
}

// mapProjectsError maps projects' sentinel errors to Huma HTTP errors.
// ErrProjectNotFound and ErrClientNotFound (a foreign/missing parent Client) both
// map to 404 — a foreign real Client ID behaves exactly like a nonexistent one
// (design spec §6, §10.4).
func mapProjectsError(err error) error {
	switch {
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrClientNotFound):
		return huma.Error404NotFound("client not found")
	case errors.Is(err, ErrNameRequired):
		return huma.Error422UnprocessableEntity("name is required")
	case errors.Is(err, ErrInvalidStatus):
		return huma.Error422UnprocessableEntity("invalid project status")
	case errors.Is(err, ErrScopeBriefTooLong):
		return huma.Error422UnprocessableEntity("scope brief exceeds maximum length")
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
