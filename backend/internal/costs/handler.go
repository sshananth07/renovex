package costs

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type costMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type costItemDTO struct {
	ID                string                `json:"id"`
	ProjectID         string                `json:"projectId"`
	WorkItemID        string                `json:"workItemId,omitempty"`
	Category          string                `json:"category"`
	Description       string                `json:"description"`
	QuantityValue     string                `json:"quantityValue,omitempty"`
	QuantityUnit      string                `json:"quantityUnit,omitempty"`
	UnitPrice         *costMoneyDTO         `json:"unitPrice,omitempty"`
	MaterialID        string                `json:"materialId,omitempty"`
	Estimated         *costMoneyDTO         `json:"estimated,omitempty"`
	Committed         *costMoneyDTO         `json:"committed,omitempty"`
	Actual            *costMoneyDTO         `json:"actual,omitempty"`
	Paid              *costMoneyDTO         `json:"paid,omitempty"`
	Currency          string                `json:"currency"`
	Date              string                `json:"date"`
	Notes             string                `json:"notes,omitempty"`
	CreatedAt         string                `json:"createdAt"`
	Revision          int64                 `json:"revision"`
	ActualCorrections []actualCorrectionDTO `json:"actualCorrections,omitempty"`
}

type actualCorrectionDTO struct {
	PreviousAmount  costMoneyDTO `json:"previousAmount"`
	NewAmount       costMoneyDTO `json:"newAmount"`
	Reason          string       `json:"reason"`
	CorrectedByUser string       `json:"correctedByUser"`
	CorrectedAt     string       `json:"correctedAt"`
}

type createCostItemInput struct {
	Body struct {
		ProjectID       string `json:"projectId" required:"true" minLength:"1"`
		WorkItemID      string `json:"workItemId,omitempty"`
		Category        string `json:"category" required:"true" enum:"material,subcontractor,equipment,transport,permit,professional_fee,utility,miscellaneous"`
		Description     string `json:"description" required:"true" minLength:"1"`
		QuantityValue   string `json:"quantityValue,omitempty"`
		QuantityUnit    string `json:"quantityUnit,omitempty"`
		UnitPriceAmount *int64 `json:"unitPriceAmount,omitempty"`
		EstimatedAmount *int64 `json:"estimatedAmount,omitempty"`
		CommittedAmount *int64 `json:"committedAmount,omitempty"`
		ActualAmount    *int64 `json:"actualAmount,omitempty"`
		PaidAmount      *int64 `json:"paidAmount,omitempty"`
		Currency        string `json:"currency" required:"true" minLength:"1"`
		MaterialID      string `json:"materialId,omitempty"`
		Notes           string `json:"notes,omitempty"`
	}
}

type costItemOutput struct {
	Body costItemDTO
}

type listCostItemsInput struct {
	ProjectID  string `query:"projectId"`
	WorkItemID string `query:"workItemId"`
}

type listCostItemsOutput struct {
	Body struct {
		CostItems []costItemDTO `json:"costItems"`
	}
}

type getCostItemInput struct {
	ID string `path:"id"`
}

type updateCostItemLifecycleInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
		Stage            string `json:"stage" required:"true"`
		Amount           int64  `json:"amount" required:"true"`
	}
}

type updateCostItemDetailsInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64   `json:"expectedRevision" required:"true"`
		Description      string  `json:"description" required:"true" minLength:"1"`
		Notes            string  `json:"notes,omitempty"`
		Category         *string `json:"category,omitempty"`
	}
}

type recordCostItemActualInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
		Amount           int64 `json:"amount" required:"true"`
	}
}

type correctCostItemActualInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
		Amount           int64  `json:"amount" required:"true"`
		Reason           string `json:"reason" required:"true" minLength:"1"`
	}
}

// RegisterHandlers registers POST /cost-items, GET /cost-items,
// GET /cost-items/{id}, PATCH /cost-items/{id}/lifecycle, and
// PATCH /cost-items/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "cost-items-create",
		Method:      http.MethodPost,
		Path:        "/cost-items",
		Summary:     "Create a CostItem under a Project (and optionally a WorkItem). Rejects category=labour.",
	}, func(ctx context.Context, input *createCostItemInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var workItemID *string
		if input.Body.WorkItemID != "" {
			workItemID = &input.Body.WorkItemID
		}
		var materialID *string
		if input.Body.MaterialID != "" {
			materialID = &input.Body.MaterialID
		}
		var quantityValue, quantityUnit *string
		if input.Body.QuantityValue != "" && input.Body.QuantityUnit != "" {
			quantityValue = &input.Body.QuantityValue
			quantityUnit = &input.Body.QuantityUnit
		}

		c, err := svc.CreateCostItem(ctx, principal.CompanyID, input.Body.ProjectID, workItemID, CostCategory(input.Body.Category),
			input.Body.Description, quantityValue, quantityUnit, input.Body.UnitPriceAmount,
			input.Body.EstimatedAmount, input.Body.CommittedAmount, input.Body.ActualAmount, input.Body.PaidAmount,
			input.Body.Currency, materialID, time.Now(), input.Body.Notes)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-list",
		Method:      http.MethodGet,
		Path:        "/cost-items",
		Summary:     "List CostItems for a Project or a WorkItem belonging to the authenticated company",
	}, func(ctx context.Context, input *listCostItemsInput) (*listCostItemsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var list []CostItem
		var err error
		switch {
		case input.WorkItemID != "":
			list, err = svc.ListCostItemsByWorkItem(ctx, principal.CompanyID, input.WorkItemID)
		case input.ProjectID != "":
			list, err = svc.ListCostItemsByProject(ctx, principal.CompanyID, input.ProjectID)
		default:
			return nil, huma.Error422UnprocessableEntity("either projectId or workItemId query parameter is required")
		}
		if err != nil {
			return nil, mapCostsError(err)
		}

		resp := &listCostItemsOutput{}
		for _, c := range list {
			resp.Body.CostItems = append(resp.Body.CostItems, toCostItemDTO(c))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-get",
		Method:      http.MethodGet,
		Path:        "/cost-items/{id}",
		Summary:     "Get a CostItem, tenant-scoped",
	}, func(ctx context.Context, input *getCostItemInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		c, err := svc.GetCostItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-update-lifecycle",
		Method:      http.MethodPatch,
		Path:        "/cost-items/{id}/lifecycle",
		Summary:     "Set exactly one of estimated/committed/actual/paid. paid is a cumulative running total.",
	}, func(ctx context.Context, input *updateCostItemLifecycleInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		existing, err := svc.GetCostItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapCostsError(err)
		}
		amount := money.New(input.Body.Amount, existing.Currency)
		c, err := svc.UpdateCostItemLifecycle(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, CostStage(input.Body.Stage), amount)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-record-actual",
		Method:      http.MethodPost,
		Path:        "/cost-items/{id}/record-actual",
		Summary:     "Set actual for the first time. Rejected if actual is already recorded — use correct-actual instead.",
	}, func(ctx context.Context, input *recordCostItemActualInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		existing, err := svc.GetCostItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapCostsError(err)
		}
		amount := money.New(input.Body.Amount, existing.Currency)
		c, err := svc.RecordCostItemActual(ctx, principal.CompanyID, principal.UserID, input.ID, input.Body.ExpectedRevision, amount)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-correct-actual",
		Method:      http.MethodPost,
		Path:        "/cost-items/{id}/correct-actual",
		Summary:     "Correct an already-recorded actual amount, preserving the previous value as an audit record.",
	}, func(ctx context.Context, input *correctCostItemActualInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		existing, err := svc.GetCostItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapCostsError(err)
		}
		amount := money.New(input.Body.Amount, existing.Currency)
		c, err := svc.CorrectCostItemActual(ctx, principal.CompanyID, principal.UserID, input.ID, input.Body.ExpectedRevision, amount, input.Body.Reason)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-update-details",
		Method:      http.MethodPatch,
		Path:        "/cost-items/{id}",
		Summary:     "Update description/notes, and optionally category (locked once committed/actual/paid is set)",
	}, func(ctx context.Context, input *updateCostItemDetailsInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var category *CostCategory
		if input.Body.Category != nil {
			cat := CostCategory(*input.Body.Category)
			category = &cat
		}
		c, err := svc.UpdateCostItemDetails(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision, input.Body.Description, input.Body.Notes, category)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})
}

func toCostItemDTO(c CostItem) costItemDTO {
	dto := costItemDTO{
		ID: c.ID, ProjectID: c.ProjectID, Category: string(c.Category), Description: c.Description,
		Currency: c.Currency, Date: c.Date.Format(timeLayout), Notes: c.Notes, CreatedAt: c.CreatedAt.Format(timeLayout),
		Revision: c.Revision,
	}
	for _, correction := range c.ActualCorrections {
		dto.ActualCorrections = append(dto.ActualCorrections, actualCorrectionDTO{
			PreviousAmount:  costMoneyDTO{Amount: correction.PreviousAmount.Amount, Currency: correction.PreviousAmount.Currency},
			NewAmount:       costMoneyDTO{Amount: correction.NewAmount.Amount, Currency: correction.NewAmount.Currency},
			Reason:          correction.Reason,
			CorrectedByUser: correction.CorrectedByUser,
			CorrectedAt:     correction.CorrectedAt.Format(timeLayout),
		})
	}
	if c.WorkItemID != nil {
		dto.WorkItemID = *c.WorkItemID
	}
	if c.MaterialID != nil {
		dto.MaterialID = *c.MaterialID
	}
	if c.Quantity != nil {
		dto.QuantityValue = c.Quantity.Value.String()
		dto.QuantityUnit = c.Quantity.Unit
	}
	if c.UnitPrice != nil {
		dto.UnitPrice = &costMoneyDTO{Amount: c.UnitPrice.Amount, Currency: c.UnitPrice.Currency}
	}
	if c.Estimated != nil {
		dto.Estimated = &costMoneyDTO{Amount: c.Estimated.Amount, Currency: c.Estimated.Currency}
	}
	if c.Committed != nil {
		dto.Committed = &costMoneyDTO{Amount: c.Committed.Amount, Currency: c.Committed.Currency}
	}
	if c.Actual != nil {
		dto.Actual = &costMoneyDTO{Amount: c.Actual.Amount, Currency: c.Actual.Currency}
	}
	if c.Paid != nil {
		dto.Paid = &costMoneyDTO{Amount: c.Paid.Amount, Currency: c.Paid.Currency}
	}
	return dto
}

func mapCostsError(err error) error {
	switch {
	case errors.Is(err, ErrCostItemNotFound):
		return huma.Error404NotFound("cost item not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")
	case errors.Is(err, ErrLabourCategoryNotAllowed):
		return huma.Error422UnprocessableEntity("category=labour is not allowed via this endpoint; use POST /labour-entries")
	case errors.Is(err, ErrNoLifecycleAmount):
		return huma.Error422UnprocessableEntity("at least one of estimated, committed, actual, or paid is required")
	case errors.Is(err, ErrMaterialIDRequiresMaterialCategory):
		return huma.Error422UnprocessableEntity("materialId requires category=material")
	case errors.Is(err, ErrInvalidCategory):
		return huma.Error422UnprocessableEntity("invalid category")
	case errors.Is(err, ErrCurrencyMismatch):
		return huma.Error422UnprocessableEntity("currency does not match this cost item's currency")
	case errors.Is(err, ErrCategoryLocked):
		return huma.Error409Conflict("category is locked once committed, actual, or paid is set")
	case errors.Is(err, ErrLabourCategoryImmutable):
		return huma.Error409Conflict("category=labour cannot be changed once set")
	case errors.Is(err, ErrIncompleteQuantityPricing):
		return huma.Error422UnprocessableEntity("quantityValue, quantityUnit, and unitPriceAmount must all be supplied together or not at all")
	case errors.Is(err, ErrInvalidQuantity):
		return huma.Error422UnprocessableEntity("quantity must be a positive number with a non-empty unit")
	case errors.Is(err, ErrEstimatedMismatchesLineAmount):
		return huma.Error422UnprocessableEntity("estimatedAmount does not match quantity x unitPrice")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("cost item changed since it was read")
	case errors.Is(err, ErrActualMustUseRecordOrCorrect):
		return huma.Error422UnprocessableEntity("actual can only be set via record-actual or correct-actual")
	case errors.Is(err, ErrActualAlreadyRecorded):
		return huma.Error409Conflict("actual is already recorded; use correct-actual to change it")
	case errors.Is(err, ErrNoActualToCorrect):
		return huma.Error409Conflict("no actual value has been recorded yet; use record-actual instead")
	case errors.Is(err, ErrCorrectionReasonRequired):
		return huma.Error422UnprocessableEntity("a reason is required to correct a previously recorded actual amount")
	default:
		return err
	}
}
