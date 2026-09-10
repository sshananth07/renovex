package quotations

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type quotationMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type quotationLineDTO struct {
	ID                string             `json:"id"`
	SourceWorkItemIDs []string           `json:"sourceWorkItemIds"`
	Description       string             `json:"description"`
	Quantity          string             `json:"quantity,omitempty"`
	Unit              string             `json:"unit,omitempty"`
	UnitPrice         *quotationMoneyDTO `json:"unitPrice,omitempty"`
	Amount            quotationMoneyDTO  `json:"amount"`
	SortOrder         int                `json:"sortOrder"`
}

type quotationDTO struct {
	ID                string             `json:"id"`
	ProjectID         string             `json:"projectId"`
	ClientID          string             `json:"clientId"`
	EstimateID        string             `json:"estimateId"`
	QuotationNumber   string             `json:"quotationNumber"`
	Version           int                `json:"version"`
	Status            string             `json:"status"`
	Revision          int64              `json:"revision"`
	Currency          string             `json:"currency"`
	Lines             []quotationLineDTO `json:"lines"`
	Subtotal          quotationMoneyDTO  `json:"subtotal"`
	GeneratedSubtotal quotationMoneyDTO  `json:"generatedSubtotal"`
	TaxMode           string             `json:"taxMode"`
	TaxLabel          string             `json:"taxLabel,omitempty"`
	TaxRateBPS        int64              `json:"taxRateBps,omitempty"`
	TaxAmount         quotationMoneyDTO  `json:"taxAmount"`
	Total             quotationMoneyDTO  `json:"total"`
	Terms             string             `json:"terms,omitempty"`
	PaymentSchedule   string             `json:"paymentSchedule,omitempty"`
	ValidUntil        string             `json:"validUntil,omitempty"`
	Notes             string             `json:"notes,omitempty"`
	CreatedAt         string             `json:"createdAt"`
	FinalizedAt       string             `json:"finalizedAt,omitempty"`
}

type createQuotationInput struct {
	Body struct {
		ProjectID  string `json:"projectId" required:"true" minLength:"1"`
		EstimateID string `json:"estimateId" required:"true" minLength:"1"`
	}
}

type quotationOutput struct {
	Body quotationDTO
}

type listQuotationsInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type listQuotationsOutput struct {
	Body struct {
		Quotations []quotationDTO `json:"quotations"`
	}
}

type getQuotationInput struct {
	ID string `path:"id"`
}

type quotationLineInput struct {
	ID                string             `json:"id,omitempty"`
	SourceWorkItemIDs []string           `json:"sourceWorkItemIds"`
	Description       string             `json:"description" required:"true"`
	Quantity          *string            `json:"quantity,omitempty"`
	Unit              *string            `json:"unit,omitempty"`
	UnitPrice         *quotationMoneyDTO `json:"unitPrice,omitempty"`
	Amount            quotationMoneyDTO  `json:"amount" required:"true"`
	SortOrder         int                `json:"sortOrder,omitempty"`
}

type replaceLinesInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64                `json:"expectedRevision" required:"true"`
		Lines            []quotationLineInput `json:"lines" required:"true"`
	}
}

type updateTermsInput struct {
	ID   string `path:"id"`
	Body struct {
		Terms            string `json:"terms,omitempty"`
		PaymentSchedule  string `json:"paymentSchedule,omitempty"`
		Notes            string `json:"notes,omitempty"`
		ValidUntil       string `json:"validUntil,omitempty"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type updateTaxInput struct {
	ID   string `path:"id"`
	Body struct {
		TaxMode          string `json:"taxMode" required:"true"`
		TaxLabel         string `json:"taxLabel,omitempty"`
		TaxRateBPS       int64  `json:"taxRateBps,omitempty"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type finalizeQuotationInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type createNewQuotationVersionInput struct {
	ID   string `path:"id"`
	Body struct {
		EstimateID string `json:"estimateId" required:"true" minLength:"1"`
	}
}

// RegisterHandlers registers POST /quotations, GET /quotations,
// GET /quotations/{id}, PUT /quotations/{id}/lines,
// PATCH /quotations/{id}/terms, PATCH /quotations/{id}/tax,
// POST /quotations/{id}/finalize, and POST /quotations/{id}/versions on
// api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "quotations-create",
		Method:      http.MethodPost,
		Path:        "/quotations",
		Summary:     "Create Version 1 for a fresh commercial quotation chain, always as a draft",
	}, func(ctx context.Context, input *createQuotationInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.CreateQuotation(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.EstimateID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-list",
		Method:      http.MethodGet,
		Path:        "/quotations",
		Summary:     "List every version of every quotation chain for one Project",
	}, func(ctx context.Context, input *listQuotationsInput) (*listQuotationsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListQuotationsByProject(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		resp := &listQuotationsOutput{}
		for _, q := range list {
			resp.Body.Quotations = append(resp.Body.Quotations, toQuotationDTO(q))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-get",
		Method:      http.MethodGet,
		Path:        "/quotations/{id}",
		Summary:     "Get one specific Quotation version, tenant-scoped",
	}, func(ctx context.Context, input *getQuotationInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.GetQuotation(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-replace-lines",
		Method:      http.MethodPut,
		Path:        "/quotations/{id}/lines",
		Summary:     "Replace the entire Lines array on a draft Quotation",
	}, func(ctx context.Context, input *replaceLinesInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		lines, err := fromQuotationLineInputs(input.Body.Lines)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		q, err := svc.ReplaceLines(ctx, principal.CompanyID, input.ID, lines, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-update-terms",
		Method:      http.MethodPatch,
		Path:        "/quotations/{id}/terms",
		Summary:     "Update Terms/PaymentSchedule/Notes/ValidUntil on a draft Quotation",
	}, func(ctx context.Context, input *updateTermsInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var validUntil *time.Time
		if input.Body.ValidUntil != "" {
			parsed, parseErr := time.Parse(timeLayout, input.Body.ValidUntil)
			if parseErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid validUntil timestamp")
			}
			validUntil = &parsed
		}
		q, err := svc.UpdateTerms(ctx, principal.CompanyID, input.ID, input.Body.Terms, input.Body.PaymentSchedule,
			input.Body.Notes, validUntil, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-update-tax",
		Method:      http.MethodPatch,
		Path:        "/quotations/{id}/tax",
		Summary:     "Update TaxMode/TaxLabel/TaxRateBPS on a draft Quotation",
	}, func(ctx context.Context, input *updateTaxInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.UpdateTax(ctx, principal.CompanyID, input.ID, TaxMode(input.Body.TaxMode),
			input.Body.TaxLabel, money.RateBPS(input.Body.TaxRateBPS), input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-finalize",
		Method:      http.MethodPost,
		Path:        "/quotations/{id}/finalize",
		Summary:     "draft -> finalized, one-directional, locks every field. Idempotent on retry.",
	}, func(ctx context.Context, input *finalizeQuotationInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.FinalizeQuotation(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-create-version",
		Method:      http.MethodPost,
		Path:        "/quotations/{id}/versions",
		Summary:     "Create the next Version as a new draft — source must already be finalized",
	}, func(ctx context.Context, input *createNewQuotationVersionInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.CreateNewVersion(ctx, principal.CompanyID, input.ID, input.Body.EstimateID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})
}

func fromQuotationLineInputs(inputs []quotationLineInput) ([]QuotationLine, error) {
	lines := make([]QuotationLine, len(inputs))
	for i, in := range inputs {
		var qty *decimal.Decimal
		if in.Quantity != nil {
			parsed, err := decimal.NewFromString(*in.Quantity)
			if err != nil {
				return nil, ErrInvalidLineQuantity
			}
			qty = &parsed
		}
		var unitPrice *money.Money
		if in.UnitPrice != nil {
			m := money.New(in.UnitPrice.Amount, in.UnitPrice.Currency)
			unitPrice = &m
		}
		sourceIDs := in.SourceWorkItemIDs
		if sourceIDs == nil {
			sourceIDs = []string{}
		}
		lines[i] = QuotationLine{
			ID: in.ID, SourceWorkItemIDs: sourceIDs, Description: in.Description,
			Quantity: qty, Unit: in.Unit, UnitPrice: unitPrice,
			Amount: money.New(in.Amount.Amount, in.Amount.Currency), SortOrder: in.SortOrder,
		}
	}
	return lines, nil
}

func toQuotationDTO(q Quotation) quotationDTO {
	dto := quotationDTO{
		ID: q.ID, ProjectID: q.ProjectID, ClientID: q.ClientID, EstimateID: q.EstimateID,
		QuotationNumber: q.QuotationNumber, Version: q.Version, Status: string(q.Status), Revision: q.Revision,
		Currency:          q.Currency,
		Subtotal:          quotationMoneyDTO{Amount: q.Subtotal.Amount, Currency: q.Subtotal.Currency},
		GeneratedSubtotal: quotationMoneyDTO{Amount: q.GeneratedSubtotal.Amount, Currency: q.GeneratedSubtotal.Currency},
		TaxMode:           string(q.TaxMode), TaxLabel: q.TaxLabel, TaxRateBPS: int64(q.TaxRateBPS),
		TaxAmount: quotationMoneyDTO{Amount: q.TaxAmount.Amount, Currency: q.TaxAmount.Currency},
		Total:     quotationMoneyDTO{Amount: q.Total.Amount, Currency: q.Total.Currency},
		Terms:     q.Terms, PaymentSchedule: q.PaymentSchedule, Notes: q.Notes,
		CreatedAt: q.CreatedAt.Format(timeLayout),
	}
	for _, l := range q.Lines {
		lineDTO := quotationLineDTO{
			ID: l.ID, SourceWorkItemIDs: l.SourceWorkItemIDs, Description: l.Description,
			Amount: quotationMoneyDTO{Amount: l.Amount.Amount, Currency: l.Amount.Currency}, SortOrder: l.SortOrder,
		}
		if l.Quantity != nil {
			lineDTO.Quantity = l.Quantity.String()
		}
		if l.Unit != nil {
			lineDTO.Unit = *l.Unit
		}
		if l.UnitPrice != nil {
			lineDTO.UnitPrice = &quotationMoneyDTO{Amount: l.UnitPrice.Amount, Currency: l.UnitPrice.Currency}
		}
		dto.Lines = append(dto.Lines, lineDTO)
	}
	if q.ValidUntil != nil {
		dto.ValidUntil = q.ValidUntil.Format(timeLayout)
	}
	if q.FinalizedAt != nil {
		dto.FinalizedAt = q.FinalizedAt.Format(timeLayout)
	}
	return dto
}

func mapQuotationsError(err error) error {
	switch {
	case errors.Is(err, ErrQuotationNotFound):
		return huma.Error404NotFound("quotation not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrEstimateNotFound):
		return huma.Error404NotFound("estimate not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrEstimateNotFinalized):
		return huma.Error422UnprocessableEntity("source estimate must be finalized")
	case errors.Is(err, ErrEstimateProjectMismatch):
		return huma.Error422UnprocessableEntity("estimate does not belong to this project")
	case errors.Is(err, ErrIneligibleCostBasisForQuotation):
		return huma.Error422UnprocessableEntity("estimate's cost basis cannot be allocated into a quotation")
	case errors.Is(err, ErrNoQuotationSeedsGenerated):
		return huma.Error422UnprocessableEntity("estimate produced no quotation line seeds")
	case errors.Is(err, ErrQuotationSeedCurrencyMismatch):
		return huma.Error422UnprocessableEntity("estimate seed currency does not match the estimate's currency")
	case errors.Is(err, ErrQuotationSeedAmountNotPositive):
		return huma.Error422UnprocessableEntity("estimate seed amount must be strictly positive")
	case errors.Is(err, ErrQuotationSeedTotalMismatch):
		return huma.Error422UnprocessableEntity("sum of estimate seed amounts does not equal the estimate's proposed selling price")
	case errors.Is(err, ErrQuotationCurrencyMismatch):
		return huma.Error422UnprocessableEntity("referenced estimate's currency does not match this quotation chain")
	case errors.Is(err, ErrLineCurrencyMismatch):
		return huma.Error422UnprocessableEntity("line amount/unitPrice currency does not match this quotation's currency")
	case errors.Is(err, ErrIncompleteLinePricing):
		return huma.Error422UnprocessableEntity("quantity, unit, and unitPrice must all be supplied together or not at all")
	case errors.Is(err, ErrInvalidLineQuantity):
		return huma.Error422UnprocessableEntity("quantity must be strictly positive")
	case errors.Is(err, ErrInvalidLineUnit):
		return huma.Error422UnprocessableEntity("unit must not be blank")
	case errors.Is(err, ErrInvalidLineUnitPrice):
		return huma.Error422UnprocessableEntity("unitPrice must be strictly positive")
	case errors.Is(err, ErrLineAmountMismatch):
		return huma.Error422UnprocessableEntity("line amount does not match quantity x unitPrice")
	case errors.Is(err, ErrLineAmountNotPositive):
		return huma.Error422UnprocessableEntity("line amount must be strictly positive")
	case errors.Is(err, ErrEmptyLines):
		return huma.Error422UnprocessableEntity("at least one line is required")
	case errors.Is(err, ErrDuplicateWorkItemIDInLine):
		return huma.Error422UnprocessableEntity("sourceWorkItemIds must not contain the same work item twice")
	case errors.Is(err, ErrUnknownLineID):
		return huma.Error422UnprocessableEntity("line id does not belong to this quotation")
	case errors.Is(err, ErrDuplicateLineID):
		return huma.Error422UnprocessableEntity("duplicate line id in submitted lines")
	case errors.Is(err, ErrBlankLineDescription):
		return huma.Error422UnprocessableEntity("line description must not be blank")
	case errors.Is(err, ErrInvalidTaxMode):
		return huma.Error422UnprocessableEntity("invalid tax mode")
	case errors.Is(err, ErrInvalidTaxRate):
		return huma.Error422UnprocessableEntity("invalid tax rate for the given tax mode")
	case errors.Is(err, ErrInvalidTaxLabel):
		return huma.Error422UnprocessableEntity("invalid tax label for the given tax mode")
	case errors.Is(err, ErrQuotationNotDraft):
		return huma.Error409Conflict("quotation is not a draft")
	case errors.Is(err, ErrQuotationMustBeFinalizedBeforeNewVersion):
		return huma.Error409Conflict("source quotation must be finalized before creating a new version")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("revision mismatch: the quotation has changed since it was last read")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("version allocation conflict; please retry")
	case errors.Is(err, ErrDraftAlreadyExists):
		return huma.Error409Conflict("a draft already exists for this quotation chain")
	case errors.Is(err, ErrUnclassifiedDuplicateKey):
		// Deliberately mapped to 500, not 409 — an unclassified
		// duplicate-key error is, by definition, one this code could not
		// positively identify as either known safe-to-surface case.
		return err
	default:
		return err
	}
}
