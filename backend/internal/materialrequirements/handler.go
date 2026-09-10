package materialrequirements

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// --- DTOs ---

type quantityDTO struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

// materialRequirementDTO is the contractor-facing projection. Unlike the
// supplier-facing RFQ line projections of §9, it DOES include InternalNotes:
// these routes are contractor-only (design spec §13.1).
type materialRequirementDTO struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	WorkItemID string `json:"workItemId,omitempty"`

	MaterialID    string `json:"materialId"`
	MaterialName  string `json:"materialName"`
	Specification string `json:"specification,omitempty"`

	RequiredQuantity quantityDTO `json:"requiredQuantity"`
	CatalogUnit      string      `json:"catalogUnit"`

	UnitMismatch             bool `json:"unitMismatch"`
	UnitMismatchAcknowledged bool `json:"unitMismatchAcknowledged"`

	RequiredByDate   string `json:"requiredByDate,omitempty"`
	ProcurementNotes string `json:"procurementNotes,omitempty"`
	InternalNotes    string `json:"internalNotes,omitempty"`

	Status     string `json:"status"`
	SourceType string `json:"sourceType"`

	SourceAggregationUnit string       `json:"sourceAggregationUnit,omitempty"`
	SourceCostItemIDs     []string     `json:"sourceCostItemIds,omitempty"`
	SourceQuantity        *quantityDTO `json:"sourceQuantity,omitempty"`
	SourceFingerprint     string       `json:"sourceFingerprint,omitempty"`
	SourceSyncState       string       `json:"sourceSyncState"`
	SourceSyncedAt        string       `json:"sourceSyncedAt,omitempty"`
	SourceCheckedAt       string       `json:"sourceCheckedAt,omitempty"`

	SplitFromRequirementID string `json:"splitFromRequirementId,omitempty"`
	SplitGroupID           string `json:"splitGroupId,omitempty"`
	SplitSequence          *int   `json:"splitSequence,omitempty"`
	SplitState             string `json:"splitState,omitempty"`

	CreatedFromDiscrepancyRequirementID string `json:"createdFromDiscrepancyRequirementId,omitempty"`

	// Claim state is contractor-visible so the materials view can show which
	// RFQ holds a requirement (design spec §7.1).
	ActiveRFQChainID string `json:"activeRfqChainId,omitempty"`
	ActiveRFQNumber  string `json:"activeRfqNumber,omitempty"`
	ActiveRFQLineID  string `json:"activeRfqLineId,omitempty"`
	RFQClaimedAt     string `json:"rfqClaimedAt,omitempty"`

	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type requirementOutput struct {
	Body materialRequirementDTO
}

// emptyOutput backs the one route with no response body (delete).
type emptyOutput struct {
	Status int
}

// --- generate ---

type generateInput struct {
	ProjectID string `path:"projectId"`
}

type skippedRowDTO struct {
	MaterialID string `json:"materialId"`
	WorkItemID string `json:"workItemId"`
	Reason     string `json:"reason"`
}

type discrepancyRefDTO struct {
	MaterialRequirementID string `json:"materialRequirementId"`
	SyncState             string `json:"syncState"`
	AnchorStatus          string `json:"anchorStatus"`
}

type ineligibleCountsDTO struct {
	NonMaterialCategory   int `json:"nonMaterialCategory"`
	MissingMaterialID     int `json:"missingMaterialId"`
	MissingWorkItemID     int `json:"missingWorkItemId"`
	MissingOrZeroQuantity int `json:"missingOrZeroQuantity"`
	BlankUnit             int `json:"blankUnit"`
}

type generateOutput struct {
	Body struct {
		CreatedCount       int `json:"createdCount"`
		UnchangedCount     int `json:"unchangedCount"`
		DiscrepancyCount   int `json:"discrepancyCount"`
		SourceRemovedCount int `json:"sourceRemovedCount"`
		SkippedCount       int `json:"skippedCount"`

		Created       []materialRequirementDTO `json:"created"`
		Discrepancies []discrepancyRefDTO      `json:"discrepancies"`
		SourceRemoved []discrepancyRefDTO      `json:"sourceRemoved"`
		Skipped       []skippedRowDTO          `json:"skipped"`

		IntrinsicallyIneligible ineligibleCountsDTO `json:"intrinsicallyIneligible"`
	}
}

// --- create / list / get / edit ---

type createRequirementInput struct {
	Body struct {
		ProjectID        string  `json:"projectId" required:"true" minLength:"1"`
		WorkItemID       string  `json:"workItemId,omitempty"`
		MaterialID       string  `json:"materialId" required:"true" minLength:"1"`
		QuantityValue    string  `json:"quantityValue" required:"true" minLength:"1"`
		QuantityUnit     string  `json:"quantityUnit" required:"true" minLength:"1"`
		Specification    *string `json:"specification,omitempty"`
		RequiredByDate   string  `json:"requiredByDate,omitempty"`
		ProcurementNotes string  `json:"procurementNotes,omitempty"`
		InternalNotes    string  `json:"internalNotes,omitempty"`
	}
}

type listRequirementsInput struct {
	ProjectID  string `query:"projectId" required:"true"`
	WorkItemID string `query:"workItemId"`
	Status     string `query:"status"`
	SyncState  string `query:"syncState"`
}

type listRequirementsOutput struct {
	Body struct {
		MaterialRequirements []materialRequirementDTO `json:"materialRequirements"`
	}
}

type getRequirementInput struct {
	ID string `path:"id"`
}

type patchRequirementInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`

		MaterialID    *string `json:"materialId,omitempty"`
		QuantityValue *string `json:"quantityValue,omitempty"`
		QuantityUnit  *string `json:"quantityUnit,omitempty"`
		Specification *string `json:"specification,omitempty"`

		// The *string-of-pointer fields distinguish "unchanged" from "clear it",
		// which a plain omitempty string cannot express (design spec §5.4).
		WorkItemID      *string `json:"workItemId,omitempty"`
		ClearWorkItemID bool    `json:"clearWorkItemId,omitempty"`

		RequiredByDate      *string `json:"requiredByDate,omitempty"`
		ClearRequiredByDate bool    `json:"clearRequiredByDate,omitempty"`

		ProcurementNotes *string `json:"procurementNotes,omitempty"`
		InternalNotes    *string `json:"internalNotes,omitempty"`
	}
}

// revisionActionInput backs review, acknowledge-unit and archive: each takes
// only the Revision guard.
type revisionActionInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

// --- source discrepancy ---

type sourceDiscrepancyOutput struct {
	Body struct {
		RequirementRevision int64  `json:"requirementRevision"`
		SyncState           string `json:"syncState"`
		AnchorStatus        string `json:"anchorStatus"`

		Accepted struct {
			Quantity    quantityDTO `json:"quantity"`
			CostItemIDs []string    `json:"costItemIds"`
			Fingerprint string      `json:"fingerprint"`
		} `json:"accepted"`
		Proposed struct {
			Quantity    quantityDTO `json:"quantity"`
			CostItemIDs []string    `json:"costItemIds"`
			Fingerprint string      `json:"fingerprint"`
		} `json:"proposed"`

		AvailableActions []string `json:"availableActions"`
	}
}

type resolveDiscrepancyInput struct {
	ID   string `path:"id"`
	Body struct {
		Action                      string `json:"action" required:"true"`
		ExpectedRevision            int64  `json:"expectedRevision" required:"true"`
		ExpectedProposedFingerprint string `json:"expectedProposedFingerprint" required:"true" minLength:"1"`
		ResolutionOperationID       string `json:"resolutionOperationId,omitempty"`
	}
}

// --- split ---

type splitChildInputDTO struct {
	QuantityValue    string `json:"quantityValue" required:"true" minLength:"1"`
	QuantityUnit     string `json:"quantityUnit" required:"true" minLength:"1"`
	RequiredByDate   string `json:"requiredByDate,omitempty"`
	ProcurementNotes string `json:"procurementNotes,omitempty"`
	InternalNotes    string `json:"internalNotes,omitempty"`
}

type splitRequirementInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64                `json:"expectedRevision" required:"true"`
		Children         []splitChildInputDTO `json:"children" required:"true" minItems:"2"`
	}
}

type splitOutput struct {
	Body struct {
		Source   materialRequirementDTO   `json:"source"`
		Children []materialRequirementDTO `json:"children"`
	}
}

// RegisterHandlers registers the twelve Material Requirement routes of §13.1.
//
// There is deliberately NO claim-reconciliation route here: it lives on the RFQ,
// because only rfqs can read its own lines (design spec §7.5, §13.1).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-generate",
		Method:      http.MethodPost,
		Path:        "/projects/{projectId}/material-requirements/generate",
		Summary:     "Generate Material Requirements from a Project's material CostItems",
	}, func(ctx context.Context, input *generateInput) (*generateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		result, err := svc.GenerateFromCostItems(ctx, principal.CompanyID, principal.UserID, input.ProjectID)
		if err != nil {
			return nil, mapRequirementError(err)
		}

		out := &generateOutput{}
		out.Body.CreatedCount = result.CreatedCount
		out.Body.UnchangedCount = result.UnchangedCount
		out.Body.DiscrepancyCount = result.DiscrepancyCount
		out.Body.SourceRemovedCount = result.SourceRemovedCount
		out.Body.SkippedCount = result.SkippedCount
		out.Body.Created = toRequirementDTOs(result.Created)
		out.Body.Discrepancies = toDiscrepancyRefDTOs(result.Discrepancies)
		out.Body.SourceRemoved = toDiscrepancyRefDTOs(result.SourceRemoved)
		out.Body.Skipped = make([]skippedRowDTO, 0, len(result.Skipped))
		for _, s := range result.Skipped {
			out.Body.Skipped = append(out.Body.Skipped, skippedRowDTO{
				MaterialID: s.MaterialID, WorkItemID: s.WorkItemID, Reason: s.Reason,
			})
		}
		out.Body.IntrinsicallyIneligible = ineligibleCountsDTO{
			NonMaterialCategory:   result.IntrinsicallyIneligible.NonMaterialCategory,
			MissingMaterialID:     result.IntrinsicallyIneligible.MissingMaterialID,
			MissingWorkItemID:     result.IntrinsicallyIneligible.MissingWorkItemID,
			MissingOrZeroQuantity: result.IntrinsicallyIneligible.MissingOrZeroQuantity,
			BlankUnit:             result.IntrinsicallyIneligible.BlankUnit,
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-create",
		Method:      http.MethodPost,
		Path:        "/material-requirements",
		Summary:     "Create a manual Material Requirement",
	}, func(ctx context.Context, input *createRequirementInput) (*requirementOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var workItemID *string
		if input.Body.WorkItemID != "" {
			workItemID = &input.Body.WorkItemID
		}
		requiredBy, err := parseOptionalTime(input.Body.RequiredByDate)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("requiredByDate must be an RFC3339 timestamp")
		}

		created, err := svc.CreateManualRequirement(ctx, principal.CompanyID, principal.UserID,
			CreateManualInput{
				ProjectID: input.Body.ProjectID, WorkItemID: workItemID,
				MaterialID:    input.Body.MaterialID,
				QuantityValue: input.Body.QuantityValue, QuantityUnit: input.Body.QuantityUnit,
				Specification: input.Body.Specification, RequiredByDate: requiredBy,
				ProcurementNotes: input.Body.ProcurementNotes,
				InternalNotes:    input.Body.InternalNotes,
			})
		if err != nil {
			return nil, mapRequirementError(err)
		}
		return &requirementOutput{Body: toRequirementDTO(created)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-list",
		Method:      http.MethodGet,
		Path:        "/material-requirements",
		Summary:     "List Material Requirements for a Project belonging to the authenticated company",
	}, func(ctx context.Context, input *listRequirementsInput) (*listRequirementsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		found, err := svc.ListRequirements(ctx, principal.CompanyID, input.ProjectID,
			RequirementFilter{
				WorkItemID: input.WorkItemID, Status: input.Status, SyncState: input.SyncState,
			})
		if err != nil {
			return nil, mapRequirementError(err)
		}
		out := &listRequirementsOutput{}
		out.Body.MaterialRequirements = toRequirementDTOs(found)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-get",
		Method:      http.MethodGet,
		Path:        "/material-requirements/{id}",
		Summary:     "Get one Material Requirement",
	}, func(ctx context.Context, input *getRequirementInput) (*requirementOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		found, err := svc.GetRequirement(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapRequirementError(err)
		}
		return &requirementOutput{Body: toRequirementDTO(found)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-update",
		Method:      http.MethodPatch,
		Path:        "/material-requirements/{id}",
		Summary:     "Edit a Material Requirement under a Revision guard",
	}, func(ctx context.Context, input *patchRequirementInput) (*requirementOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		update := UpdateRequirementInput{
			MaterialID:       input.Body.MaterialID,
			QuantityValue:    input.Body.QuantityValue,
			QuantityUnit:     input.Body.QuantityUnit,
			Specification:    input.Body.Specification,
			ProcurementNotes: input.Body.ProcurementNotes,
			InternalNotes:    input.Body.InternalNotes,
		}

		// "clear it" and "set it" are distinct requests; sending both is
		// contradictory and is refused rather than silently resolved.
		if input.Body.ClearWorkItemID && input.Body.WorkItemID != nil {
			return nil, huma.Error422UnprocessableEntity(
				"workItemId and clearWorkItemId are mutually exclusive")
		}
		switch {
		case input.Body.ClearWorkItemID:
			var none *string
			update.WorkItemID = &none
		case input.Body.WorkItemID != nil:
			update.WorkItemID = &input.Body.WorkItemID
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

		updated, err := svc.UpdateRequirement(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, update)
		if err != nil {
			return nil, mapRequirementError(err)
		}
		return &requirementOutput{Body: toRequirementDTO(updated)}, nil
	})

	registerRevisionAction(api, "material-requirements-review", "/material-requirements/{id}/review",
		"Transition a Material Requirement draft -> reviewed", svc.ReviewRequirement)
	registerRevisionAction(api, "material-requirements-acknowledge-unit",
		"/material-requirements/{id}/acknowledge-unit",
		"Acknowledge a procurement unit differing from the catalog unit", svc.AcknowledgeUnitMismatch)
	registerRevisionAction(api, "material-requirements-archive", "/material-requirements/{id}/archive",
		"Archive a Material Requirement (terminal)", svc.ArchiveRequirement)

	huma.Register(api, huma.Operation{
		OperationID:   "material-requirements-delete",
		Method:        http.MethodDelete,
		Path:          "/material-requirements/{id}",
		Summary:       "Permanently delete a Material Requirement; irreversible, unlike archive",
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *revisionActionInput) (*emptyOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		if err := svc.DeleteRequirement(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision); err != nil {
			return nil, mapRequirementError(err)
		}
		return &emptyOutput{Status: http.StatusNoContent}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-source-discrepancy",
		Method:      http.MethodGet,
		Path:        "/material-requirements/{id}/source-discrepancy",
		Summary:     "Read the recomputed source proposal and the actions available",
	}, func(ctx context.Context, input *getRequirementInput) (*sourceDiscrepancyOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		d, err := svc.GetSourceDiscrepancy(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapRequirementError(err)
		}

		out := &sourceDiscrepancyOutput{}
		out.Body.RequirementRevision = d.RequirementRevision
		out.Body.SyncState = string(d.SyncState)
		out.Body.AnchorStatus = string(d.AnchorStatus)
		out.Body.Accepted.Quantity = toQuantityDTO(d.AcceptedQuantity)
		out.Body.Accepted.CostItemIDs = orEmpty(d.AcceptedCostItemIDs)
		out.Body.Accepted.Fingerprint = d.AcceptedFingerprint
		out.Body.Proposed.Quantity = toQuantityDTO(d.ProposedQuantity)
		out.Body.Proposed.CostItemIDs = orEmpty(d.ProposedCostItemIDs)
		out.Body.Proposed.Fingerprint = d.ProposedFingerprint
		out.Body.AvailableActions = make([]string, 0, len(d.AvailableActions))
		for _, a := range d.AvailableActions {
			out.Body.AvailableActions = append(out.Body.AvailableActions, string(a))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-resolve-discrepancy",
		Method:      http.MethodPost,
		Path:        "/material-requirements/{id}/source-discrepancy/resolve",
		Summary:     "Apply a discrepancy resolution: update, merge, keep_current or create_separate",
	}, func(ctx context.Context, input *resolveDiscrepancyInput) (*requirementOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		resolved, err := svc.ResolveSourceDiscrepancy(ctx, principal.CompanyID, principal.UserID,
			input.ID, ResolveDiscrepancyInput{
				Action:                      ResolutionAction(input.Body.Action),
				ExpectedRevision:            input.Body.ExpectedRevision,
				ExpectedProposedFingerprint: input.Body.ExpectedProposedFingerprint,
				ResolutionOperationID:       input.Body.ResolutionOperationID,
			})
		if err != nil {
			return nil, mapRequirementError(err)
		}
		// create_separate returns the NEW child; the other three return the
		// resolved anchor (design spec §5.6).
		return &requirementOutput{Body: toRequirementDTO(resolved)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-split",
		Method:      http.MethodPost,
		Path:        "/material-requirements/{id}/split",
		Summary:     "Split a Material Requirement into two or more batches",
	}, func(ctx context.Context, input *splitRequirementInput) (*splitOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		children := make([]SplitChildInput, 0, len(input.Body.Children))
		for _, c := range input.Body.Children {
			requiredBy, err := parseOptionalTime(c.RequiredByDate)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("requiredByDate must be an RFC3339 timestamp")
			}
			children = append(children, SplitChildInput{
				QuantityValue: c.QuantityValue, QuantityUnit: c.QuantityUnit,
				RequiredByDate: requiredBy, ProcurementNotes: c.ProcurementNotes,
				InternalNotes: c.InternalNotes,
			})
		}

		result, err := svc.SplitRequirement(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, children)
		if err != nil {
			return nil, mapRequirementError(err)
		}
		return toSplitOutput(result), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "material-requirements-split-reconcile",
		Method:      http.MethodPost,
		Path:        "/material-requirements/{id}/split/reconcile",
		Summary:     "Complete an interrupted split from its manifest",
	}, func(ctx context.Context, input *revisionActionInput) (*splitOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		result, err := svc.ReconcileSplit(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapRequirementError(err)
		}
		return toSplitOutput(result), nil
	})
}

// registerRevisionAction registers one of the three POST actions whose only
// input is the Revision guard, so review, acknowledge-unit and archive cannot
// drift apart in shape or error handling.
func registerRevisionAction(api huma.API, operationID, path, summary string,
	action func(ctx context.Context, companyID, actorUserID, requirementID string,
		expectedRevision int64) (MaterialRequirement, error)) {

	huma.Register(api, huma.Operation{
		OperationID: operationID,
		Method:      http.MethodPost,
		Path:        path,
		Summary:     summary,
	}, func(ctx context.Context, input *revisionActionInput) (*requirementOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := action(ctx, principal.CompanyID, principal.UserID, input.ID,
			input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapRequirementError(err)
		}
		return &requirementOutput{Body: toRequirementDTO(updated)}, nil
	})
}

// mapRequirementError implements the §13.5 status table.
//
// The three internal-control-flow sentinels are deliberately absent: reaching a
// handler would mean a recovery path was skipped, so they fall through to 500
// rather than being given an invented client-facing meaning.
func mapRequirementError(err error) error {
	switch {
	// --- 404 ---
	case errors.Is(err, ErrMaterialRequirementNotFound):
		return huma.Error404NotFound("material requirement not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")

	// --- 422: validation ---
	case errors.Is(err, ErrInvalidQuantity):
		return huma.Error422UnprocessableEntity("quantity must be a positive decimal")
	case errors.Is(err, ErrInvalidUnit):
		return huma.Error422UnprocessableEntity("unit must be non-empty")
	case errors.Is(err, ErrSplitQuantityMismatch):
		return huma.Error422UnprocessableEntity(
			"split child quantities must sum exactly to the source quantity")
	case errors.Is(err, ErrSplitTooFewChildren):
		return huma.Error422UnprocessableEntity("a split requires at least two children")
	case errors.Is(err, ErrSplitUnitChanged):
		return huma.Error422UnprocessableEntity("split children must retain the source unit")
	case errors.Is(err, ErrRecursiveSplitNotSupported):
		return huma.Error422UnprocessableEntity("splitting a split child is not supported")
	case errors.Is(err, ErrWorkItemRequiredForGeneratedRequirement):
		return huma.Error422UnprocessableEntity("workItemId is required for a cost_item requirement")
	case errors.Is(err, ErrMaterialIDImmutable):
		return huma.Error422UnprocessableEntity("materialId is immutable for this source type")
	case errors.Is(err, ErrWorkItemIDImmutable):
		return huma.Error422UnprocessableEntity("workItemId is immutable for this source type")
	case errors.Is(err, ErrResolutionActionNotAvailable):
		return huma.Error422UnprocessableEntity(
			"that resolution action is not available for this requirement")
	case errors.Is(err, ErrResolutionOperationIDRequired):
		return huma.Error422UnprocessableEntity("resolutionOperationId is required for create_separate")
	case errors.Is(err, ErrNoUnitMismatchToAcknowledge):
		return huma.Error422UnprocessableEntity("there is no unit mismatch to acknowledge")
	case errors.Is(err, ErrNoSourceDiscrepancy):
		return huma.Error422UnprocessableEntity("this requirement has no source discrepancy to resolve")
	case errors.Is(err, ErrNoSplitToReconcile):
		return huma.Error422UnprocessableEntity("this requirement has no split to reconcile")
	case errors.Is(err, ErrWorkItemCancelled):
		return huma.Error422UnprocessableEntity("work item is cancelled")

	// --- 409: state and concurrency ---
	case errors.Is(err, ErrRequirementNotEligibleForRFQ):
		return huma.Error409Conflict("requirement is not eligible for an RFQ")
	case errors.Is(err, ErrUnresolvedUnitMismatch):
		return huma.Error409Conflict("unit mismatch is unresolved")
	case errors.Is(err, ErrUnresolvedSourceDiscrepancy):
		return huma.Error409Conflict("source discrepancy is unresolved")
	case errors.Is(err, ErrMaterialRequirementAlreadyClaimed):
		return huma.Error409Conflict(
			"requirement is claimed by an active RFQ chain; remove the RFQ line first")
	case errors.Is(err, ErrRequirementTerminal):
		return huma.Error409Conflict("requirement is terminal and read-only")
	case errors.Is(err, ErrSplitInProgress):
		return huma.Error409Conflict("a split is already in progress for this requirement")
	case errors.Is(err, ErrMaterialRequirementDiscrepancyChanged):
		return huma.Error409Conflict("the source changed since the proposal was computed")
	case errors.Is(err, ErrResolutionOperationConflict):
		return huma.Error409Conflict("resolution operation id belongs to a different resolution")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("requirement changed since it was read")

	default:
		return err
	}
}

// --- projections ---

func toRequirementDTO(r MaterialRequirement) materialRequirementDTO {
	dto := materialRequirementDTO{
		ID: r.ID, ProjectID: r.ProjectID,
		MaterialID: r.MaterialID, MaterialName: r.MaterialName, Specification: r.Specification,
		RequiredQuantity: toQuantityDTO(r.RequiredQuantity),
		CatalogUnit:      r.CatalogUnit,
		UnitMismatch:     r.UnitMismatch, UnitMismatchAcknowledged: r.UnitMismatchAcknowledged,
		ProcurementNotes: r.ProcurementNotes, InternalNotes: r.InternalNotes,
		Status: string(r.Status), SourceType: string(r.SourceType),
		SourceAggregationUnit: r.SourceAggregationUnit,
		SourceCostItemIDs:     r.SourceCostItemIDs,
		SourceFingerprint:     r.SourceFingerprint,
		SourceSyncState:       string(r.SourceSyncState),
		SplitState:            string(r.SplitState),
		SplitSequence:         r.SplitSequence,
		Revision:              r.Revision,
		CreatedAt:             r.CreatedAt.Format(timeLayout),
		UpdatedAt:             r.UpdatedAt.Format(timeLayout),
	}
	if r.WorkItemID != nil {
		dto.WorkItemID = *r.WorkItemID
	}
	if r.RequiredByDate != nil {
		dto.RequiredByDate = r.RequiredByDate.Format(timeLayout)
	}
	if r.SourceQuantity != nil {
		q := toQuantityDTO(*r.SourceQuantity)
		dto.SourceQuantity = &q
	}
	if r.SourceSyncedAt != nil {
		dto.SourceSyncedAt = r.SourceSyncedAt.Format(timeLayout)
	}
	if r.SourceCheckedAt != nil {
		dto.SourceCheckedAt = r.SourceCheckedAt.Format(timeLayout)
	}
	if r.SplitFromRequirementID != nil {
		dto.SplitFromRequirementID = *r.SplitFromRequirementID
	}
	if r.SplitGroupID != nil {
		dto.SplitGroupID = *r.SplitGroupID
	}
	if r.CreatedFromDiscrepancyRequirementID != nil {
		dto.CreatedFromDiscrepancyRequirementID = *r.CreatedFromDiscrepancyRequirementID
	}
	if r.ActiveRFQChainID != nil {
		dto.ActiveRFQChainID = *r.ActiveRFQChainID
	}
	if r.ActiveRFQNumber != nil {
		dto.ActiveRFQNumber = *r.ActiveRFQNumber
	}
	if r.ActiveRFQLineID != nil {
		dto.ActiveRFQLineID = *r.ActiveRFQLineID
	}
	if r.RFQClaimedAt != nil {
		dto.RFQClaimedAt = r.RFQClaimedAt.Format(timeLayout)
	}
	return dto
}

func toRequirementDTOs(rs []MaterialRequirement) []materialRequirementDTO {
	out := make([]materialRequirementDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, toRequirementDTO(r))
	}
	return out
}

func toDiscrepancyRefDTOs(refs []DiscrepancyRef) []discrepancyRefDTO {
	out := make([]discrepancyRefDTO, 0, len(refs))
	for _, r := range refs {
		out = append(out, discrepancyRefDTO{
			MaterialRequirementID: r.MaterialRequirementID,
			SyncState:             r.SyncState,
			AnchorStatus:          r.AnchorStatus,
		})
	}
	return out
}

func toSplitOutput(result SplitResult) *splitOutput {
	out := &splitOutput{}
	out.Body.Source = toRequirementDTO(result.Source)
	out.Body.Children = toRequirementDTOs(result.Children)
	return out
}

func toQuantityDTO(q quantity.Quantity) quantityDTO {
	return quantityDTO{Value: q.Value.String(), Unit: q.Unit}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
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
