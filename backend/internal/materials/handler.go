package materials

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type materialDTO struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Category               string `json:"category,omitempty"`
	Specification          string `json:"specification,omitempty"`
	Unit                   string `json:"unit"`
	ReferencePriceAmount   int64  `json:"referencePriceAmount"`
	ReferencePriceCurrency string `json:"referencePriceCurrency"`
	ReferencePriceAsOf     string `json:"referencePriceAsOf"`
	CreatedAt              string `json:"createdAt"`
}

type createMaterialInput struct {
	Body struct {
		Name                   string `json:"name" required:"true" minLength:"1"`
		Category               string `json:"category,omitempty"`
		Specification          string `json:"specification,omitempty"`
		Unit                   string `json:"unit" required:"true" minLength:"1"`
		ReferencePriceAmount   int64  `json:"referencePriceAmount" required:"true"`
		ReferencePriceCurrency string `json:"referencePriceCurrency" required:"true" minLength:"1"`
	}
}

type materialOutput struct {
	Body materialDTO
}

type listMaterialsOutput struct {
	Body struct {
		Materials []materialDTO `json:"materials"`
	}
}

type getMaterialInput struct {
	ID string `path:"id"`
}

type updateMaterialInput struct {
	ID   string `path:"id"`
	Body struct {
		Name                   string `json:"name" required:"true" minLength:"1"`
		Category               string `json:"category,omitempty"`
		Specification          string `json:"specification,omitempty"`
		Unit                   string `json:"unit" required:"true" minLength:"1"`
		ReferencePriceAmount   int64  `json:"referencePriceAmount" required:"true"`
		ReferencePriceCurrency string `json:"referencePriceCurrency" required:"true" minLength:"1"`
	}
}

// RegisterHandlers registers POST /materials, GET /materials,
// GET /materials/{id}, and PATCH /materials/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "materials-create",
		Method:      http.MethodPost,
		Path:        "/materials",
		Summary:     "Create a Material catalog entry for the authenticated company",
	}, func(ctx context.Context, input *createMaterialInput) (*materialOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		m, err := svc.CreateMaterial(ctx, principal.CompanyID, input.Body.Name, input.Body.Category,
			input.Body.Specification, input.Body.Unit, input.Body.ReferencePriceAmount, input.Body.ReferencePriceCurrency)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		return &materialOutput{Body: toMaterialDTO(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-list",
		Method:      http.MethodGet,
		Path:        "/materials",
		Summary:     "List Materials for the authenticated company",
	}, func(ctx context.Context, input *struct{}) (*listMaterialsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListMaterials(ctx, principal.CompanyID)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		resp := &listMaterialsOutput{}
		for _, m := range list {
			resp.Body.Materials = append(resp.Body.Materials, toMaterialDTO(m))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-get",
		Method:      http.MethodGet,
		Path:        "/materials/{id}",
		Summary:     "Get a Material, tenant-scoped",
	}, func(ctx context.Context, input *getMaterialInput) (*materialOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		m, err := svc.GetMaterial(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		return &materialOutput{Body: toMaterialDTO(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-update",
		Method:      http.MethodPatch,
		Path:        "/materials/{id}",
		Summary:     "Update a Material, tenant-scoped. Reference price changes never retroactively alter existing CostItems.",
	}, func(ctx context.Context, input *updateMaterialInput) (*materialOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		m, err := svc.UpdateMaterial(ctx, principal.CompanyID, input.ID, input.Body.Name, input.Body.Category,
			input.Body.Specification, input.Body.Unit, input.Body.ReferencePriceAmount, input.Body.ReferencePriceCurrency)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		return &materialOutput{Body: toMaterialDTO(m)}, nil
	})
}

func toMaterialDTO(m Material) materialDTO {
	return materialDTO{
		ID: m.ID, Name: m.Name, Category: m.Category, Specification: m.Specification, Unit: m.Unit,
		ReferencePriceAmount: m.ReferencePrice.Amount, ReferencePriceCurrency: m.ReferencePrice.Currency,
		ReferencePriceAsOf: m.ReferencePriceAsOf.Format(timeLayout), CreatedAt: m.CreatedAt.Format(timeLayout),
	}
}

func mapMaterialsError(err error) error {
	switch {
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")
	case errors.Is(err, ErrNameRequired):
		return huma.Error422UnprocessableEntity("name is required")
	case errors.Is(err, ErrUnitRequired):
		return huma.Error422UnprocessableEntity("unit is required")
	default:
		return err
	}
}
