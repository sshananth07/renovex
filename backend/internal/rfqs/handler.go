package rfqs

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// --- DTOs ---

// rfqQuantityDTO is deliberately package-qualified in its Go name. Huma uses
// the reflected type name as the OpenAPI schema key, and all modules share one
// registry at composition time; a generic quantityDTO here would collide with
// materialrequirements.quantityDTO and prevent the server from starting.
type rfqQuantityDTO struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

// rfqLineDTO is the contractor-facing line projection. It carries no price, no
// cost and no InternalNotes — those fields do not exist on RFQLine at all
// (design spec §6.2).
type rfqLineDTO struct {
	ID                          string         `json:"id"`
	SourceMaterialRequirementID string         `json:"sourceMaterialRequirementId"`
	MaterialID                  string         `json:"materialId"`
	MaterialName                string         `json:"materialName"`
	Specification               string         `json:"specification,omitempty"`
	Quantity                    rfqQuantityDTO `json:"quantity"`
	RequiredByDate              string         `json:"requiredByDate,omitempty"`
	ProcurementNotes            string         `json:"procurementNotes,omitempty"`
	SnapshotAt                  string         `json:"snapshotAt"`
	SortOrder                   int            `json:"sortOrder"`
}

// rfqDTO is the CONTRACTOR route projection and deliberately includes
// InternalNotes: these routes are contractor-only. The supplier-facing
// boundary is GetReadyRFQSnapshot, where the field is structurally absent
// (design spec §9, §13.2).
type rfqDTO struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	RFQNumber string `json:"rfqNumber"`
	Status    string `json:"status"`
	Revision  int64  `json:"revision"`

	Title string       `json:"title,omitempty"`
	Lines []rfqLineDTO `json:"lines"`

	DeliveryAddress      string `json:"deliveryAddress,omitempty"`
	RequiredByDate       string `json:"requiredByDate,omitempty"`
	ResponseDeadline     string `json:"responseDeadline,omitempty"`
	SupplierInstructions string `json:"supplierInstructions,omitempty"`
	InternalNotes        string `json:"internalNotes,omitempty"`

	CreatedByUserID string `json:"createdByUserId,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	ReadyAt         string `json:"readyAt,omitempty"`
	ReopenedAt      string `json:"reopenedAt,omitempty"`
}

type rfqOutput struct {
	Body rfqDTO
}

type createRFQInput struct {
	ProjectID string `path:"projectId"`
	Body      struct {
		Title                string `json:"title,omitempty"`
		DeliveryAddress      string `json:"deliveryAddress,omitempty"`
		RequiredByDate       string `json:"requiredByDate,omitempty"`
		ResponseDeadline     string `json:"responseDeadline,omitempty"`
		SupplierInstructions string `json:"supplierInstructions,omitempty"`
		InternalNotes        string `json:"internalNotes,omitempty"`
	}
}

type listRFQsInput struct {
	ProjectID string `query:"projectId" required:"true"`
	Status    string `query:"status"`
}

type listRFQsOutput struct {
	Body struct {
		RFQs []rfqDTO `json:"rfqs"`
	}
}

type getRFQInput struct {
	ID string `path:"id"`
}

type patchRFQInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`

		Title                *string `json:"title,omitempty"`
		DeliveryAddress      *string `json:"deliveryAddress,omitempty"`
		SupplierInstructions *string `json:"supplierInstructions,omitempty"`
		InternalNotes        *string `json:"internalNotes,omitempty"`

		// Set and clear are distinct requests; a plain omitempty string cannot
		// express "clear it".
		RequiredByDate        *string `json:"requiredByDate,omitempty"`
		ClearRequiredByDate   bool    `json:"clearRequiredByDate,omitempty"`
		ResponseDeadline      *string `json:"responseDeadline,omitempty"`
		ClearResponseDeadline bool    `json:"clearResponseDeadline,omitempty"`
	}
}

type addLineInput struct {
	ID   string `path:"id"`
	Body struct {
		MaterialRequirementID       string `json:"materialRequirementId" required:"true" minLength:"1"`
		ExpectedRequirementRevision int64  `json:"expectedRequirementRevision" required:"true"`
		ExpectedRFQRevision         int64  `json:"expectedRfqRevision" required:"true"`
	}
}

type removeLineInput struct {
	ID     string `path:"id"`
	LineID string `path:"lineId"`
	Body   struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type sortOrderInput struct {
	ID     string `path:"id"`
	LineID string `path:"lineId"`
	Body   struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
		SortOrder        int   `json:"sortOrder" required:"true"`
	}
}

type revisionActionInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type reconciliationEntryDTO struct {
	RequirementID  string `json:"materialRequirementId"`
	LineID         string `json:"lineId,omitempty"`
	Revision       int64  `json:"requirementRevision,omitempty"`
	Classification string `json:"classification"`
}

type reconciliationOutput struct {
	Body struct {
		RFQChainID string                   `json:"rfqChainId"`
		Entries    []reconciliationEntryDTO `json:"entries"`
	}
}

type reconcileActionInput struct {
	ID   string `path:"id"`
	Body struct {
		MaterialRequirementID string `json:"materialRequirementId" required:"true" minLength:"1"`
		Action                string `json:"action" required:"true"`
	}
}

type deleteRFQOutput struct {
	Status int
}

// RegisterHandlers registers the twelve RFQ routes of §13.2.
//
// The claim-reconciliation routes live HERE, on the RFQ, not under
// /material-requirements: reconciliation must join claims against RFQ lines,
// which would force materialrequirements to reason about RFQ lines
// (design spec §7.5, §13.1).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "rfqs-create",
		Method:      http.MethodPost,
		Path:        "/projects/{projectId}/rfqs",
		Summary:     "Create an empty draft RFQ; header fields allowed, no requirement ids",
	}, func(ctx context.Context, input *createRFQInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		requiredBy, err := parseOptionalTime(input.Body.RequiredByDate)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("requiredByDate must be an RFC3339 timestamp")
		}
		deadline, err := parseOptionalTime(input.Body.ResponseDeadline)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("responseDeadline must be an RFC3339 timestamp")
		}

		created, err := svc.CreateRFQ(ctx, principal.CompanyID, principal.UserID, input.ProjectID,
			CreateRFQInput{
				Title: input.Body.Title, DeliveryAddress: input.Body.DeliveryAddress,
				RequiredByDate: requiredBy, ResponseDeadline: deadline,
				SupplierInstructions: input.Body.SupplierInstructions,
				InternalNotes:        input.Body.InternalNotes,
			})
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(created)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-list",
		Method:      http.MethodGet,
		Path:        "/rfqs",
		Summary:     "List RFQs for a Project belonging to the authenticated company",
	}, func(ctx context.Context, input *listRFQsInput) (*listRFQsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		found, err := svc.ListRFQsByProject(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapRFQError(err)
		}

		out := &listRFQsOutput{}
		out.Body.RFQs = make([]rfqDTO, 0, len(found))
		for _, r := range found {
			if input.Status != "" && string(r.Status) != input.Status {
				continue
			}
			out.Body.RFQs = append(out.Body.RFQs, toRFQDTO(r))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-get",
		Method:      http.MethodGet,
		Path:        "/rfqs/{id}",
		Summary:     "Get one RFQ, including contractor-only internalNotes",
	}, func(ctx context.Context, input *getRFQInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		found, err := svc.GetRFQ(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(found)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-update",
		Method:      http.MethodPatch,
		Path:        "/rfqs/{id}",
		Summary:     "Edit RFQ header fields; draft only",
	}, func(ctx context.Context, input *patchRFQInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		update := UpdateRFQInput{
			Title:                input.Body.Title,
			DeliveryAddress:      input.Body.DeliveryAddress,
			SupplierInstructions: input.Body.SupplierInstructions,
			InternalNotes:        input.Body.InternalNotes,
		}

		if input.Body.ClearRequiredByDate && input.Body.RequiredByDate != nil {
			return nil, huma.Error422UnprocessableEntity(
				"requiredByDate and clearRequiredByDate are mutually exclusive")
		}
		switch {
		case input.Body.ClearRequiredByDate:
			var none *time.Time
			update.RequiredByDate = &none
		case input.Body.RequiredByDate != nil:
			parsed, err := parseOptionalTime(*input.Body.RequiredByDate)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("requiredByDate must be an RFC3339 timestamp")
			}
			update.RequiredByDate = &parsed
		}

		if input.Body.ClearResponseDeadline && input.Body.ResponseDeadline != nil {
			return nil, huma.Error422UnprocessableEntity(
				"responseDeadline and clearResponseDeadline are mutually exclusive")
		}
		switch {
		case input.Body.ClearResponseDeadline:
			var none *time.Time
			update.ResponseDeadline = &none
		case input.Body.ResponseDeadline != nil:
			parsed, err := parseOptionalTime(*input.Body.ResponseDeadline)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("responseDeadline must be an RFC3339 timestamp")
			}
			update.ResponseDeadline = &parsed
		}

		updated, err := svc.UpdateRFQ(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision, update)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-add-line",
		Method:      http.MethodPost,
		Path:        "/rfqs/{id}/lines",
		Summary:     "Claim one Material Requirement and append it as a line",
	}, func(ctx context.Context, input *addLineInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.AddLine(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRFQRevision, input.Body.MaterialRequirementID,
			input.Body.ExpectedRequirementRevision)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-remove-line",
		Method:      http.MethodDelete,
		Path:        "/rfqs/{id}/lines/{lineId}",
		Summary:     "Remove a line, then release its requirement claim",
	}, func(ctx context.Context, input *removeLineInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.RemoveLine(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision, input.LineID)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-line-sort-order",
		Method:      http.MethodPatch,
		Path:        "/rfqs/{id}/lines/{lineId}/sort-order",
		Summary:     "Reorder one RFQ line",
	}, func(ctx context.Context, input *sortOrderInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.SetLineSortOrder(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, input.LineID, input.Body.SortOrder)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-claim-reconciliation-get",
		Method:      http.MethodGet,
		Path:        "/rfqs/{id}/claim-reconciliation",
		Summary:     "Inspect claim/line consistency, including orphaned claims",
	}, func(ctx context.Context, input *getRFQInput) (*reconciliationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		report, err := svc.GetClaimReconciliation(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapRFQError(err)
		}

		out := &reconciliationOutput{}
		out.Body.RFQChainID = report.RFQChainID
		out.Body.Entries = make([]reconciliationEntryDTO, 0, len(report.Entries))
		for _, e := range report.Entries {
			out.Body.Entries = append(out.Body.Entries, reconciliationEntryDTO{
				RequirementID: e.RequirementID, LineID: e.LineID,
				Revision: e.Revision, Classification: e.Classification,
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-claim-reconciliation-apply",
		Method:      http.MethodPost,
		Path:        "/rfqs/{id}/claim-reconciliation",
		Summary:     "Apply retry_line or release to an inconsistent claim",
	}, func(ctx context.Context, input *reconcileActionInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.ReconcileClaim(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.MaterialRequirementID, input.Body.Action)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-ready",
		Method:      http.MethodPost,
		Path:        "/rfqs/{id}/ready",
		Summary:     "Transition draft -> ready; increments revision",
	}, func(ctx context.Context, input *revisionActionInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.MarkReady(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfqs-reopen",
		Method:      http.MethodPost,
		Path:        "/rfqs/{id}/reopen",
		Summary:     "Transition ready -> draft, gated by issuance status; increments revision",
	}, func(ctx context.Context, input *revisionActionInput) (*rfqOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.Reopen(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapRFQError(err)
		}
		return &rfqOutput{Body: toRFQDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "rfqs-delete",
		Method:        http.MethodDelete,
		Path:          "/rfqs/{id}",
		Summary:       "Delete an empty draft RFQ with zero outstanding claims",
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *revisionActionInput) (*deleteRFQOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		if err := svc.DeleteRFQ(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision); err != nil {
			return nil, mapRFQError(err)
		}
		return &deleteRFQOutput{Status: http.StatusNoContent}, nil
	})
}

// mapRFQError implements the §13.5 status table.
//
// ErrRFQNumberConflict is deliberately absent: it indicates counter corruption
// or manual tampering rather than a losable race, so it falls through to 500
// and is never retried.
func mapRFQError(err error) error {
	switch {
	// --- 404 ---
	case errors.Is(err, ErrRFQNotFound):
		return huma.Error404NotFound("rfq not found")
	case errors.Is(err, ErrRFQLineNotFound):
		return huma.Error404NotFound("rfq line not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrMaterialRequirementNotFound):
		return huma.Error404NotFound("material requirement not found")

	// --- 422 ---
	case errors.Is(err, ErrRFQNoLines):
		return huma.Error422UnprocessableEntity("an rfq needs at least one line to be marked ready")
	case errors.Is(err, ErrRFQDeliveryAddressRequired):
		return huma.Error422UnprocessableEntity("deliveryAddress is required to mark an rfq ready")
	case errors.Is(err, ErrRFQLineNotEligible):
		return huma.Error422UnprocessableEntity("an rfq line's requirement is not eligible")
	case errors.Is(err, ErrRFQDatesOutOfOrder):
		return huma.Error422UnprocessableEntity("the required delivery date must be after the supplier response deadline")
	case errors.Is(err, ErrReconciliationActionInvalid):
		return huma.Error422UnprocessableEntity("action must be retry_line or release")
	case errors.Is(err, ErrNoClaimToReconcile):
		return huma.Error422UnprocessableEntity("that requirement holds no claim on this rfq chain")

	// --- 409 ---
	case errors.Is(err, ErrMaterialRequirementAlreadyClaimed):
		return huma.Error409Conflict("that material requirement is already claimed by another rfq chain")
	case errors.Is(err, ErrRFQNotDraft):
		return huma.Error409Conflict("rfq is not in draft status")
	case errors.Is(err, ErrRFQNotReady):
		return huma.Error409Conflict("rfq is not in ready status")
	case errors.Is(err, ErrRFQNotEmpty):
		return huma.Error409Conflict("rfq still has lines")
	case errors.Is(err, ErrRFQHasOutstandingClaims):
		return huma.Error409Conflict(
			"rfq still holds outstanding requirement claims; reconcile them before deleting")
	case errors.Is(err, ErrRFQAlreadyIssued):
		return huma.Error409Conflict("this rfq chain has already been issued")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("rfq changed since it was read")

	// --- 503: the issuance seam is unreachable, so reopen fails closed ---
	case errors.Is(err, ErrIssuanceStatusUnavailable):
		return huma.Error503ServiceUnavailable("issuance status is currently unavailable")

	default:
		return err
	}
}

// --- projections ---

func toRFQDTO(r RFQ) rfqDTO {
	dto := rfqDTO{
		ID: r.ID, ProjectID: r.ProjectID, RFQNumber: r.RFQNumber,
		Status: string(r.Status), Revision: r.Revision,
		Title: r.Title,
		Lines: make([]rfqLineDTO, 0, len(r.Lines)),

		DeliveryAddress:      r.DeliveryAddress,
		SupplierInstructions: r.SupplierInstructions,
		InternalNotes:        r.InternalNotes,

		CreatedByUserID: r.CreatedByUserID,
		CreatedAt:       r.CreatedAt.Format(timeLayout),
		UpdatedAt:       r.UpdatedAt.Format(timeLayout),
	}
	if r.RequiredByDate != nil {
		dto.RequiredByDate = r.RequiredByDate.Format(timeLayout)
	}
	if r.ResponseDeadline != nil {
		dto.ResponseDeadline = r.ResponseDeadline.Format(timeLayout)
	}
	if r.ReadyAt != nil {
		dto.ReadyAt = r.ReadyAt.Format(timeLayout)
	}
	if r.ReopenedAt != nil {
		dto.ReopenedAt = r.ReopenedAt.Format(timeLayout)
	}
	for _, l := range r.Lines {
		dto.Lines = append(dto.Lines, toLineDTO(l))
	}
	return dto
}

func toLineDTO(l RFQLine) rfqLineDTO {
	dto := rfqLineDTO{
		ID: l.ID, SourceMaterialRequirementID: l.SourceMaterialRequirementID,
		MaterialID: l.MaterialID, MaterialName: l.MaterialName,
		Specification:    l.Specification,
		Quantity:         rfqQuantityDTO{Value: l.Quantity.Value.String(), Unit: l.Quantity.Unit},
		ProcurementNotes: l.ProcurementNotes,
		SnapshotAt:       l.SnapshotAt.Format(timeLayout),
		SortOrder:        l.SortOrder,
	}
	if l.RequiredByDate != nil {
		dto.RequiredByDate = l.RequiredByDate.Format(timeLayout)
	}
	return dto
}

// parseOptionalTime treats "" as absent rather than as a parse error, so an
// omitted optional timestamp is not reported as malformed.
func parseOptionalTime(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	parsed, err := time.Parse(timeLayout, v)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
