package labour

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type labourMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type workerDTO struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Trade        string         `json:"trade,omitempty"`
	RateType     string         `json:"rateType"`
	DefaultRate  labourMoneyDTO `json:"defaultRate"`
	ContactPhone string         `json:"contactPhone,omitempty"`
	ContactEmail string         `json:"contactEmail,omitempty"`
	CreatedAt    string         `json:"createdAt"`
}

type createWorkerInput struct {
	Body struct {
		Name              string `json:"name" required:"true" minLength:"1"`
		Trade             string `json:"trade,omitempty"`
		RateType          string `json:"rateType" required:"true" enum:"hourly,daily,fixed_project,per_unit,per_square_meter"`
		DefaultRateAmount int64  `json:"defaultRateAmount" required:"true"`
		Currency          string `json:"currency" required:"true" minLength:"1"`
		ContactPhone      string `json:"contactPhone,omitempty"`
		ContactEmail      string `json:"contactEmail,omitempty"`
	}
}

type workerOutput struct {
	Body workerDTO
}

type listWorkersOutput struct {
	Body struct {
		Workers []workerDTO `json:"workers"`
	}
}

type getWorkerInput struct {
	ID string `path:"id"`
}

type updateWorkerInput struct {
	ID   string `path:"id"`
	Body struct {
		Name              string `json:"name" required:"true" minLength:"1"`
		Trade             string `json:"trade,omitempty"`
		RateType          string `json:"rateType" required:"true" enum:"hourly,daily,fixed_project,per_unit,per_square_meter"`
		DefaultRateAmount int64  `json:"defaultRateAmount" required:"true"`
		Currency          string `json:"currency" required:"true" minLength:"1"`
		ContactPhone      string `json:"contactPhone,omitempty"`
		ContactEmail      string `json:"contactEmail,omitempty"`
	}
}

type labourEntryDTO struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"projectId"`
	WorkItemID    string         `json:"workItemId"`
	WorkerID      string         `json:"workerId,omitempty"`
	WorkerName    string         `json:"workerName"`
	Trade         string         `json:"trade,omitempty"`
	QuantityValue string         `json:"quantityValue"`
	QuantityUnit  string         `json:"quantityUnit"`
	Rate          labourMoneyDTO `json:"rate"`
	Cost          labourMoneyDTO `json:"cost"`
	CostItemID    string         `json:"costItemId"`
	Date          string         `json:"date"`
	Notes         string         `json:"notes,omitempty"`
	CreatedAt     string         `json:"createdAt"`
}

type createLabourEntryInput struct {
	Body struct {
		ProjectID     string `json:"projectId" required:"true" minLength:"1"`
		WorkItemID    string `json:"workItemId" required:"true" minLength:"1"`
		WorkerID      string `json:"workerId,omitempty"`
		WorkerName    string `json:"workerName,omitempty"`
		Trade         string `json:"trade,omitempty"`
		QuantityValue string `json:"quantityValue" required:"true"`
		QuantityUnit  string `json:"quantityUnit" required:"true" minLength:"1"`
		RateAmount    *int64 `json:"rateAmount,omitempty"`
		Currency      string `json:"currency" required:"true" minLength:"1"`
		Notes         string `json:"notes,omitempty"`
	}
}

// Resolve reports exhaustive request-level errors for the ad-hoc-labour
// cross-field invariant (workerId absent => workerName+trade+rateAmount all
// required). This is defense-in-depth ahead of the identical service-level
// check in Service.CreateLabourEntry (ErrAdHocFieldsRequired) — it exists so
// the client gets precise field-scoped errors before the handler even runs,
// not to replace the authoritative check.
func (i *createLabourEntryInput) Resolve(ctx huma.Context) []error {
	if i.Body.WorkerID != "" {
		return nil
	}
	var errs []error
	if i.Body.WorkerName == "" {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "worker name is required for ad-hoc labour",
			Location: "body.workerName",
			Value:    i.Body.WorkerName,
		})
	}
	if i.Body.Trade == "" {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "trade is required for ad-hoc labour",
			Location: "body.trade",
			Value:    i.Body.Trade,
		})
	}
	if i.Body.RateAmount == nil {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "rate is required for ad-hoc labour",
			Location: "body.rateAmount",
			Value:    i.Body.RateAmount,
		})
	}
	return errs
}

var _ huma.Resolver = (*createLabourEntryInput)(nil)

type labourEntryOutput struct {
	Body labourEntryDTO
}

type listLabourEntriesInput struct {
	ProjectID  string `query:"projectId"`
	WorkItemID string `query:"workItemId"`
}

type listLabourEntriesOutput struct {
	Body struct {
		LabourEntries []labourEntryDTO `json:"labourEntries"`
	}
}

type getLabourEntryInput struct {
	ID string `path:"id"`
}

// updateLabourEntryInput uses pointer fields so the handler can distinguish
// "field not supplied" (nil) from "field supplied as empty/zero" (non-nil
// pointer to a zero value) — required so Notes can be intentionally cleared
// to "" and so QuantityUnit can be corrected independently of QuantityValue
// (fix for the plan's original non-pointer DTO, which could not express
// either case).
type updateLabourEntryInput struct {
	ID   string `path:"id"`
	Body struct {
		QuantityValue *string `json:"quantityValue,omitempty"`
		QuantityUnit  *string `json:"quantityUnit,omitempty"`
		RateAmount    *int64  `json:"rateAmount,omitempty"`
		Date          *string `json:"date,omitempty"`
		Notes         *string `json:"notes,omitempty"`
	}
}

// RegisterHandlers registers POST/GET /workers, GET/PATCH /workers/{id},
// POST/GET /labour-entries, GET /labour-entries/{id}, and
// PATCH /labour-entries/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "workers-create",
		Method:      http.MethodPost,
		Path:        "/workers",
		Summary:     "Create a Worker for the authenticated company",
	}, func(ctx context.Context, input *createWorkerInput) (*workerOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		w, err := svc.CreateWorker(ctx, principal.CompanyID, input.Body.Name, input.Body.Trade, input.Body.RateType,
			input.Body.DefaultRateAmount, input.Body.Currency, input.Body.ContactPhone, input.Body.ContactEmail)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &workerOutput{Body: toWorkerDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "workers-list",
		Method:      http.MethodGet,
		Path:        "/workers",
		Summary:     "List Workers for the authenticated company",
	}, func(ctx context.Context, input *struct{}) (*listWorkersOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListWorkers(ctx, principal.CompanyID)
		if err != nil {
			return nil, mapLabourError(err)
		}
		resp := &listWorkersOutput{}
		for _, w := range list {
			resp.Body.Workers = append(resp.Body.Workers, toWorkerDTO(w))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "workers-get",
		Method:      http.MethodGet,
		Path:        "/workers/{id}",
		Summary:     "Get a Worker, tenant-scoped",
	}, func(ctx context.Context, input *getWorkerInput) (*workerOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		w, err := svc.GetWorker(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &workerOutput{Body: toWorkerDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "workers-update",
		Method:      http.MethodPatch,
		Path:        "/workers/{id}",
		Summary:     "Update a Worker, tenant-scoped. Default rate changes never retroactively alter existing LabourEntries.",
	}, func(ctx context.Context, input *updateWorkerInput) (*workerOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		w, err := svc.UpdateWorker(ctx, principal.CompanyID, input.ID, input.Body.Name, input.Body.Trade, input.Body.RateType,
			input.Body.DefaultRateAmount, input.Body.Currency, input.Body.ContactPhone, input.Body.ContactEmail)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &workerOutput{Body: toWorkerDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-create",
		Method:      http.MethodPost,
		Path:        "/labour-entries",
		Summary:     "Create a LabourEntry under a Project/WorkItem; creates a linked CostItem{category=labour}",
	}, func(ctx context.Context, input *createLabourEntryInput) (*labourEntryOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var workerID *string
		if input.Body.WorkerID != "" {
			workerID = &input.Body.WorkerID
		}
		e, err := svc.CreateLabourEntry(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.WorkItemID, workerID,
			input.Body.WorkerName, input.Body.Trade, input.Body.QuantityValue, input.Body.QuantityUnit,
			input.Body.RateAmount, input.Body.Currency, time.Now(), input.Body.Notes)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &labourEntryOutput{Body: toLabourEntryDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-list",
		Method:      http.MethodGet,
		Path:        "/labour-entries",
		Summary:     "List LabourEntries for a Project or a WorkItem belonging to the authenticated company",
	}, func(ctx context.Context, input *listLabourEntriesInput) (*listLabourEntriesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var list []LabourEntry
		var err error
		switch {
		case input.WorkItemID != "":
			list, err = svc.ListLabourEntriesByWorkItem(ctx, principal.CompanyID, input.WorkItemID)
		case input.ProjectID != "":
			list, err = svc.ListLabourEntriesByProject(ctx, principal.CompanyID, input.ProjectID)
		default:
			return nil, huma.Error422UnprocessableEntity("either projectId or workItemId query parameter is required")
		}
		if err != nil {
			return nil, mapLabourError(err)
		}
		resp := &listLabourEntriesOutput{}
		for _, e := range list {
			resp.Body.LabourEntries = append(resp.Body.LabourEntries, toLabourEntryDTO(e))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-get",
		Method:      http.MethodGet,
		Path:        "/labour-entries/{id}",
		Summary:     "Get a LabourEntry, tenant-scoped",
	}, func(ctx context.Context, input *getLabourEntryInput) (*labourEntryOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.GetLabourEntry(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &labourEntryOutput{Body: toLabourEntryDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-update",
		Method:      http.MethodPatch,
		Path:        "/labour-entries/{id}",
		Summary:     "Correct quantity/rate/date/notes only (never workerId). Propagates to the linked CostItem's Estimated field only.",
	}, func(ctx context.Context, input *updateLabourEntryInput) (*labourEntryOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.UpdateLabourEntry(ctx, principal.CompanyID, input.ID,
			input.Body.QuantityValue, input.Body.QuantityUnit, input.Body.RateAmount, input.Body.Date, input.Body.Notes)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &labourEntryOutput{Body: toLabourEntryDTO(e)}, nil
	})
}

func toWorkerDTO(w Worker) workerDTO {
	return workerDTO{
		ID: w.ID, Name: w.Name, Trade: w.Trade, RateType: string(w.RateType),
		DefaultRate:  labourMoneyDTO{Amount: w.DefaultRate.Amount, Currency: w.DefaultRate.Currency},
		ContactPhone: w.ContactPhone, ContactEmail: w.ContactEmail, CreatedAt: w.CreatedAt.Format(timeLayout),
	}
}

func toLabourEntryDTO(e LabourEntry) labourEntryDTO {
	dto := labourEntryDTO{
		ID: e.ID, ProjectID: e.ProjectID, WorkItemID: e.WorkItemID, WorkerName: e.WorkerName, Trade: e.Trade,
		QuantityValue: e.Quantity.Value.String(), QuantityUnit: e.Quantity.Unit,
		Rate:       labourMoneyDTO{Amount: e.Rate.Amount, Currency: e.Rate.Currency},
		Cost:       labourMoneyDTO{Amount: e.Cost.Amount, Currency: e.Cost.Currency},
		CostItemID: e.CostItemID, Date: e.Date.Format(timeLayout), Notes: e.Notes, CreatedAt: e.CreatedAt.Format(timeLayout),
	}
	if e.WorkerID != nil {
		dto.WorkerID = *e.WorkerID
	}
	return dto
}

func mapLabourError(err error) error {
	switch {
	case errors.Is(err, ErrWorkerNotFound):
		return huma.Error404NotFound("worker not found")
	case errors.Is(err, ErrLabourEntryNotFound):
		return huma.Error404NotFound("labour entry not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrNameRequired):
		return huma.Error422UnprocessableEntity("name is required")
	case errors.Is(err, ErrInvalidRateType):
		return huma.Error422UnprocessableEntity("invalid rate type")
	case errors.Is(err, ErrInvalidQuantity):
		return huma.Error422UnprocessableEntity("quantity must be a positive number with a non-empty unit")
	case errors.Is(err, ErrAdHocFieldsRequired):
		return huma.Error422UnprocessableEntity("workerName, trade, and rateAmount are required when workerId is not supplied")
	case errors.Is(err, ErrInvalidDate):
		return huma.Error422UnprocessableEntity("date must be a valid RFC3339 timestamp")
	default:
		return err
	}
}
