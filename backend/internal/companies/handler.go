package companies

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// This file is the one place in the codebase where a Huma handler bridges
// identity.Principal (constructed by identity's auth middleware, carried in
// request context) into companies.Principal (this package's own primitive-typed
// mirror). This is a handler-layer conversion, not a service-layer import: the
// companies package itself still never imports identity's business logic types
// beyond this HTTP-boundary translation, matching ADR 0002's rule that Huma
// DTOs and boundary conversions live outside the service layer.
type meOutput struct {
	Body struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		CreatedAt string `json:"createdAt"`
	}
}

type listMembersInput struct {
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
	Search   string `query:"search"`
	Sort     string `query:"sort"`
	Order    string `query:"order"`
}

type membersOutput struct {
	Body pagination.Response[memberDTO]
}

type memberDTO struct {
	UserID    string `json:"userId"`
	CompanyID string `json:"companyId"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type addMemberInput struct {
	Body struct {
		Email string `json:"email" required:"true" format:"email"`
		Role  string `json:"role" required:"true" enum:"admin,employee"`
	}
}

type addMemberOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// RegisterHandlers registers GET /companies/me, GET /companies/me/members, and
// POST /companies/members on api, backed by svc. authMiddleware wraps each
// operation's handler so a valid access token is required and its Principal is
// available via identity.PrincipalFromContext.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "companies-get-me",
		Method:      http.MethodGet,
		Path:        "/companies/me",
		Summary:     "Get the authenticated user's own company",
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		company, err := svc.GetCompany(ctx, principal.CompanyID)
		if err != nil {
			return nil, mapCompaniesError(err)
		}

		resp := &meOutput{}
		resp.Body.ID = company.ID
		resp.Body.Name = company.Name
		resp.Body.CreatedAt = company.CreatedAt.Format(timeLayout)
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "companies-get-me-members",
		Method:      http.MethodGet,
		Path:        "/companies/me/members",
		Summary:     "List the authenticated user's own company's members, paginated",
	}, func(ctx context.Context, input *listMembersInput) (*membersOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		req, err := pagination.ParseRequest(input.Page, input.PageSize, input.Search, input.Sort, input.Order,
			MemberSortFields, MemberDefaultSort, MemberDefaultOrder)
		if err != nil {
			return nil, mapCompaniesError(err)
		}

		members, total, err := svc.ListMembersPaginated(ctx, principal.CompanyID, req)
		if err != nil {
			return nil, mapCompaniesError(err)
		}

		dtos := make([]memberDTO, 0, len(members))
		for _, m := range members {
			dtos = append(dtos, memberDTO{
				UserID: m.UserID, CompanyID: m.CompanyID, Role: string(m.Role), CreatedAt: m.CreatedAt.Format(timeLayout),
			})
		}
		return &membersOutput{Body: pagination.NewResponse(dtos, req, total)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "companies-add-member",
		Method:      http.MethodPost,
		Path:        "/companies/members",
		Summary:     "Add a member to the authenticated user's own company",
	}, func(ctx context.Context, input *addMemberInput) (*addMemberOutput, error) {
		identityPrincipal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		principal := Principal{UserID: identityPrincipal.UserID, CompanyID: identityPrincipal.CompanyID, Role: identityPrincipal.Role}

		err := svc.AddMember(ctx, principal, input.Body.Email, Role(input.Body.Role))
		if err != nil {
			return nil, mapCompaniesError(err)
		}

		resp := &addMemberOutput{}
		resp.Body.Status = "added"
		return resp, nil
	})
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func mapCompaniesError(err error) error {
	switch {
	case errors.Is(err, ErrForbidden):
		return huma.Error403Forbidden("not permitted for this role")
	case errors.Is(err, ErrInvalidRole):
		return huma.Error409Conflict("invalid role for this operation")
	case errors.Is(err, ErrUserAlreadyHasMembership):
		return huma.Error409Conflict("user already belongs to a company")
	case errors.Is(err, ErrCompanyNotFound), errors.Is(err, ErrMembershipNotFound):
		return huma.Error401Unauthorized("company or membership not found")
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
