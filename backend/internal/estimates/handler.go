package estimates

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type estimateMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type estimateLineDTO struct {
	SourceCostItemID  string           `json:"sourceCostItemId"`
	WorkItemID        string           `json:"workItemId,omitempty"`
	Category          string           `json:"category"`
	Description       string           `json:"description"`
	SnapshottedAmount estimateMoneyDTO `json:"snapshottedAmount"`
}

type estimateDTO struct {
	ID                      string            `json:"id"`
	ProjectID               string            `json:"projectId"`
	Version                 int               `json:"version"`
	Status                  string            `json:"status"`
	Revision                int64             `json:"revision"`
	Currency                string            `json:"currency"`
	Lines                   []estimateLineDTO `json:"lines"`
	CostSubtotal            estimateMoneyDTO  `json:"costSubtotal"`
	ExcludedCostItemCount   int               `json:"excludedCostItemCount"`
	PricingMode             string            `json:"pricingMode"`
	PricingRate             int64             `json:"pricingRate"`
	ProposedSellingPrice    estimateMoneyDTO  `json:"proposedSellingPrice"`
	ProjectedGrossProfit    estimateMoneyDTO  `json:"projectedGrossProfit"`
	ProjectedGrossMarginBPS int64             `json:"projectedGrossMarginBps"`
	CreatedAt               string            `json:"createdAt"`
	RefreshedAt             string            `json:"refreshedAt,omitempty"`
	FinalizedAt             string            `json:"finalizedAt,omitempty"`
}

type createEstimateInput struct {
	Body struct {
		ProjectID   string `json:"projectId" required:"true" minLength:"1"`
		PricingMode string `json:"pricingMode" required:"true" enum:"markup,margin"`
		PricingRate int64  `json:"pricingRate" required:"true" minimum:"0"`
	}
}

type estimateOutput struct {
	Body estimateDTO
}

type listEstimatesInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type listEstimatesOutput struct {
	Body struct {
		Estimates []estimateDTO `json:"estimates"`
	}
}

type latestEstimateInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type getEstimateInput struct {
	ID string `path:"id"`
}

type refreshEstimateInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type recalculatePricingInput struct {
	ID   string `path:"id"`
	Body struct {
		PricingMode      string `json:"pricingMode" required:"true"`
		PricingRate      int64  `json:"pricingRate" required:"true"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type createNewVersionInput struct {
	ID   string `path:"id"`
	Body struct {
		PricingMode *string `json:"pricingMode,omitempty"`
		PricingRate *int64  `json:"pricingRate,omitempty"`
	}
}

type finalizeEstimateInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

// RegisterHandlers registers POST /estimates, GET /estimates,
// GET /estimates/latest, GET /estimates/{id}, POST /estimates/{id}/refresh,
// PATCH /estimates/{id}/pricing, POST /estimates/{id}/versions, and
// POST /estimates/{id}/finalize on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "estimates-create",
		Method:      http.MethodPost,
		Path:        "/estimates",
		Summary:     "Create Version 1 for a Project, always as a draft",
	}, func(ctx context.Context, input *createEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.CreateEstimate(ctx, principal.CompanyID, input.Body.ProjectID,
			PricingMode(input.Body.PricingMode), money.RateBPS(input.Body.PricingRate))
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-list",
		Method:      http.MethodGet,
		Path:        "/estimates",
		Summary:     "List all Estimate versions for a Project",
	}, func(ctx context.Context, input *listEstimatesInput) (*listEstimatesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListEstimatesByProject(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		resp := &listEstimatesOutput{}
		for _, e := range list {
			resp.Body.Estimates = append(resp.Body.Estimates, toEstimateDTO(e))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-latest",
		Method:      http.MethodGet,
		Path:        "/estimates/latest",
		Summary:     "Get the single highest-Version Estimate for a Project",
	}, func(ctx context.Context, input *latestEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.GetLatestEstimate(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-get",
		Method:      http.MethodGet,
		Path:        "/estimates/{id}",
		Summary:     "Get one specific Estimate version, tenant-scoped",
	}, func(ctx context.Context, input *getEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.GetEstimate(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-refresh",
		Method:      http.MethodPost,
		Path:        "/estimates/{id}/refresh",
		Summary:     "Re-pull current cost_items into this draft, in place, same Version",
	}, func(ctx context.Context, input *refreshEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.RefreshEstimate(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-recalculate-pricing",
		Method:      http.MethodPatch,
		Path:        "/estimates/{id}/pricing",
		Summary:     "Recalculate pricing in place from the stored snapshot — draft only",
	}, func(ctx context.Context, input *recalculatePricingInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.RecalculatePricing(ctx, principal.CompanyID, input.ID,
			PricingMode(input.Body.PricingMode), money.RateBPS(input.Body.PricingRate), input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-create-version",
		Method:      http.MethodPost,
		Path:        "/estimates/{id}/versions",
		Summary:     "Create the next Version as a new draft — source must already be finalized",
	}, func(ctx context.Context, input *createNewVersionInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var mode *PricingMode
		if input.Body.PricingMode != nil {
			m := PricingMode(*input.Body.PricingMode)
			mode = &m
		}
		var rate *money.RateBPS
		if input.Body.PricingRate != nil {
			r := money.RateBPS(*input.Body.PricingRate)
			rate = &r
		}
		e, err := svc.CreateNewVersion(ctx, principal.CompanyID, input.ID, mode, rate)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-finalize",
		Method:      http.MethodPost,
		Path:        "/estimates/{id}/finalize",
		Summary:     "draft -> finalized, one-directional, locks every field. Idempotent on retry.",
	}, func(ctx context.Context, input *finalizeEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.FinalizeEstimate(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})
}

func toEstimateDTO(e Estimate) estimateDTO {
	dto := estimateDTO{
		ID: e.ID, ProjectID: e.ProjectID, Version: e.Version, Status: string(e.Status), Revision: e.Revision,
		Currency: e.Currency, CostSubtotal: estimateMoneyDTO{Amount: e.CostSubtotal.Amount, Currency: e.CostSubtotal.Currency},
		ExcludedCostItemCount: e.ExcludedCostItemCount, PricingMode: string(e.PricingMode), PricingRate: int64(e.PricingRate),
		ProposedSellingPrice:    estimateMoneyDTO{Amount: e.ProposedSellingPrice.Amount, Currency: e.ProposedSellingPrice.Currency},
		ProjectedGrossProfit:    estimateMoneyDTO{Amount: e.ProjectedGrossProfit.Amount, Currency: e.ProjectedGrossProfit.Currency},
		ProjectedGrossMarginBPS: int64(e.ProjectedGrossMarginBPS),
		CreatedAt:               e.CreatedAt.Format(timeLayout),
	}
	for _, l := range e.Lines {
		lineDTO := estimateLineDTO{
			SourceCostItemID: l.SourceCostItemID, Category: l.Category, Description: l.Description,
			SnapshottedAmount: estimateMoneyDTO{Amount: l.SnapshottedAmount.Amount, Currency: l.SnapshottedAmount.Currency},
		}
		if l.WorkItemID != nil {
			lineDTO.WorkItemID = *l.WorkItemID
		}
		dto.Lines = append(dto.Lines, lineDTO)
	}
	if e.RefreshedAt != nil {
		dto.RefreshedAt = e.RefreshedAt.Format(timeLayout)
	}
	if e.FinalizedAt != nil {
		dto.FinalizedAt = e.FinalizedAt.Format(timeLayout)
	}
	return dto
}

func mapEstimatesError(err error) error {
	switch {
	case errors.Is(err, ErrEstimateNotFound):
		return huma.Error404NotFound("estimate not found")
	case errors.Is(err, ErrNoEstimatesForProject):
		return huma.Error404NotFound("no estimates exist for this project")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrNoEstimatedCosts):
		return huma.Error422UnprocessableEntity("project has no cost items with an estimated amount")
	case errors.Is(err, ErrMixedCurrencyCostItems):
		return huma.Error422UnprocessableEntity("cost items span more than one currency")
	case errors.Is(err, ErrCostSubtotalNotPositive):
		return huma.Error422UnprocessableEntity("cost subtotal must be positive")
	case errors.Is(err, ErrInvalidPricingRate):
		return huma.Error422UnprocessableEntity("invalid pricing rate for the given pricing mode")
	case errors.Is(err, ErrInvalidPricingMode):
		return huma.Error422UnprocessableEntity("invalid pricing mode: must be 'markup' or 'margin'")
	case errors.Is(err, ErrEstimateNotDraft):
		return huma.Error409Conflict("estimate is not a draft")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("revision mismatch: the estimate has changed since it was last read")
	case errors.Is(err, ErrEstimateMustBeFinalizedBeforeNewVersion):
		return huma.Error409Conflict("source estimate must be finalized before creating a new version")
	case errors.Is(err, ErrEstimateAlreadyExistsForProject):
		return huma.Error409Conflict("an estimate already exists for this project; use POST /estimates/{id}/versions from a finalized version instead")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("version allocation conflict; please retry")
	case errors.Is(err, ErrDraftAlreadyExists):
		return huma.Error409Conflict("a draft already exists for this project")
	case errors.Is(err, ErrUnclassifiedDuplicateKey):
		// Deliberately mapped to 500, not 409: an unclassified duplicate-key
		// error is, by definition, one this code could not positively
		// identify as either known safe-to-surface case — treating it as an
		// ordinary caller-actionable 409 would imply a confidence the code
		// doesn't actually have. Falls through to the same internal-error
		// handling any other unrecognized error gets.
		return err
	default:
		return err
	}
}
