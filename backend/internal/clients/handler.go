package clients

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type clientDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Phone          string `json:"phone,omitempty"`
	Email          string `json:"email,omitempty"`
	Address        string `json:"address,omitempty"`
	BillingAddress string `json:"billingAddress,omitempty"`
	Notes          string `json:"notes,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

type createClientInput struct {
	Body struct {
		Name           string `json:"name" required:"true" minLength:"1"`
		Phone          string `json:"phone,omitempty"`
		Email          string `json:"email,omitempty"`
		Address        string `json:"address,omitempty"`
		BillingAddress string `json:"billingAddress,omitempty"`
		Notes          string `json:"notes,omitempty"`
	}
}

type clientOutput struct {
	Body clientDTO
}

type listClientsInput struct {
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
	Search   string `query:"search"`
	Sort     string `query:"sort"`
	Order    string `query:"order"`
}

type listClientsOutput struct {
	Body pagination.Response[clientDTO]
}

type getClientInput struct {
	ID string `path:"id"`
}

// updateClientInput's Body fields are all *string: a nil pointer means the
// caller omitted the JSON key (leave the stored value unchanged), while a
// non-nil pointer — including one pointing to "" — means the caller
// supplied a value (including an explicit empty string, which clears an
// optional field). This is a genuine partial update, not a full-object
// replace.
type updateClientInput struct {
	ID   string `path:"id"`
	Body struct {
		Name           *string `json:"name,omitempty"`
		Phone          *string `json:"phone,omitempty"`
		Email          *string `json:"email,omitempty"`
		Address        *string `json:"address,omitempty"`
		BillingAddress *string `json:"billingAddress,omitempty"`
		Notes          *string `json:"notes,omitempty"`
	}
}

// RegisterHandlers registers POST/GET /clients, GET/PATCH /clients/{id} on api,
// backed by svc. Every operation requires an authenticated Principal — the caller is
// expected to have already mounted api behind identity.RequireAuth (cmd/api's
// composition root does this via a chi sub-router group, matching companies'
// wiring exactly).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "clients-create",
		Method:      http.MethodPost,
		Path:        "/clients",
		Summary:     "Create a Client for the authenticated company",
	}, func(ctx context.Context, input *createClientInput) (*clientOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		c, err := svc.CreateClient(ctx, principal.CompanyID, input.Body.Name, input.Body.Phone,
			input.Body.Email, input.Body.Address, input.Body.BillingAddress, input.Body.Notes)
		if err != nil {
			return nil, mapClientsError(err)
		}
		return &clientOutput{Body: toClientDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "clients-list",
		Method:      http.MethodGet,
		Path:        "/clients",
		Summary:     "List Clients for the authenticated company, paginated",
	}, func(ctx context.Context, input *listClientsInput) (*listClientsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		req, err := pagination.ParseRequest(input.Page, input.PageSize, input.Search, input.Sort, input.Order,
			ClientSortFields, ClientDefaultSort, ClientDefaultOrder)
		if err != nil {
			return nil, mapClientsError(err)
		}

		list, total, err := svc.ListClientsPaginated(ctx, principal.CompanyID, req)
		if err != nil {
			return nil, mapClientsError(err)
		}
		dtos := make([]clientDTO, 0, len(list))
		for _, c := range list {
			dtos = append(dtos, toClientDTO(c))
		}
		return &listClientsOutput{Body: pagination.NewResponse(dtos, req, total)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "clients-get",
		Method:      http.MethodGet,
		Path:        "/clients/{id}",
		Summary:     "Get a Client, tenant-scoped",
	}, func(ctx context.Context, input *getClientInput) (*clientOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		c, err := svc.GetClient(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapClientsError(err)
		}
		return &clientOutput{Body: toClientDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "clients-update",
		Method:      http.MethodPatch,
		Path:        "/clients/{id}",
		Summary:     "Update a Client, tenant-scoped",
	}, func(ctx context.Context, input *updateClientInput) (*clientOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		c, err := svc.UpdateClient(ctx, principal.CompanyID, input.ID, ClientPatch{
			Name:           input.Body.Name,
			Phone:          input.Body.Phone,
			Email:          input.Body.Email,
			Address:        input.Body.Address,
			BillingAddress: input.Body.BillingAddress,
			Notes:          input.Body.Notes,
		})
		if err != nil {
			return nil, mapClientsError(err)
		}
		return &clientOutput{Body: toClientDTO(c)}, nil
	})
}

func toClientDTO(c Client) clientDTO {
	return clientDTO{
		ID: c.ID, Name: c.Name, Phone: c.Phone, Email: c.Email,
		Address: c.Address, BillingAddress: c.BillingAddress, Notes: c.Notes,
		CreatedAt: c.CreatedAt.Format(timeLayout),
	}
}

// mapClientsError maps clients' sentinel errors to Huma HTTP errors.
// ErrClientNotFound → 404 (design spec §6: cross-tenant access and genuine
// nonexistence are indistinguishable at the API boundary, both 404).
func mapClientsError(err error) error {
	switch {
	case errors.Is(err, ErrClientNotFound):
		return huma.Error404NotFound("client not found")
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
