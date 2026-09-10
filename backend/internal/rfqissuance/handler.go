package rfqissuance

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
//
// Every DTO type name here is package-qualified in its Go name. Huma reflects
// the type name into the shared OpenAPI schema registry, so a generic
// quantityDTO or lineDTO would collide with another module's and prevent the
// composed server from starting — a defect M7 hit and fixed the same way.

type issuanceQuantityDTO struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

// issuedLineDTO is the CONTRACTOR-facing issued line.
//
// It carries the provenance identifiers because the contractor traces an award
// back through them. The supplier-facing projection is a different, narrower
// type built in a later phase — this one is never returned on a Supplier route.
type issuedLineDTO struct {
	ID                          string              `json:"id"`
	LineageID                   string              `json:"lineageId"`
	SourceM7RFQLineID           string              `json:"sourceM7RfqLineId,omitempty"`
	SourceMaterialRequirementID string              `json:"sourceMaterialRequirementId,omitempty"`
	MaterialID                  string              `json:"materialId"`
	MaterialName                string              `json:"materialName"`
	Specification               string              `json:"specification,omitempty"`
	Quantity                    issuanceQuantityDTO `json:"quantity"`
	RequiredByDate              string              `json:"requiredByDate,omitempty"`
	ProcurementNotes            string              `json:"procurementNotes,omitempty"`
	SortOrder                   int                 `json:"sortOrder"`
}

type issuedVersionDTO struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	RFQChainID    string `json:"rfqChainId"`
	RFQNumber     string `json:"rfqNumber"`
	VersionNumber int    `json:"versionNumber"`
	Currency      string `json:"currency"`

	Title                string `json:"title,omitempty"`
	DeliveryAddress      string `json:"deliveryAddress,omitempty"`
	RequiredByDate       string `json:"requiredByDate,omitempty"`
	ResponseDeadline     string `json:"responseDeadline"`
	SupplierInstructions string `json:"supplierInstructions,omitempty"`

	Lines []issuedLineDTO `json:"lines"`

	IssuedByUserID string `json:"issuedByUserId,omitempty"`
	IssuedAt       string `json:"issuedAt"`
}

type amendmentDraftDTO struct {
	ID         string `json:"id"`
	RFQChainID string `json:"rfqChainId"`

	BaseIssuedVersionID string `json:"baseIssuedVersionId"`
	BaseVersionNumber   int    `json:"baseVersionNumber"`

	Currency             string `json:"currency"`
	Title                string `json:"title,omitempty"`
	DeliveryAddress      string `json:"deliveryAddress,omitempty"`
	RequiredByDate       string `json:"requiredByDate,omitempty"`
	ResponseDeadline     string `json:"responseDeadline,omitempty"`
	SupplierInstructions string `json:"supplierInstructions,omitempty"`

	Lines []issuedLineDTO `json:"lines"`

	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(timeLayout)
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toIssuedLineDTOs(lines []IssuedRFQLine) []issuedLineDTO {
	out := make([]issuedLineDTO, 0, len(lines))
	for _, l := range lines {
		out = append(out, issuedLineDTO{
			ID: l.ID, LineageID: l.LineageID,
			SourceM7RFQLineID:           derefOr(l.SourceM7RFQLineID),
			SourceMaterialRequirementID: derefOr(l.SourceMaterialRequirementID),
			MaterialID:                  l.MaterialID,
			MaterialName:                l.MaterialName,
			Specification:               l.Specification,
			Quantity: issuanceQuantityDTO{
				// Rendered as a string, never a float (ADR 0001).
				Value: l.Quantity.Value.String(), Unit: l.Quantity.Unit,
			},
			RequiredByDate:   formatOptionalTime(l.RequiredByDate),
			ProcurementNotes: l.ProcurementNotes,
			SortOrder:        l.SortOrder,
		})
	}
	return out
}

func toIssuedVersionDTO(v IssuedRFQVersion) issuedVersionDTO {
	return issuedVersionDTO{
		ID: v.ID, ProjectID: v.ProjectID, RFQChainID: v.RFQChainID,
		RFQNumber: v.RFQNumber, VersionNumber: v.VersionNumber, Currency: v.Currency,
		Title: v.Title, DeliveryAddress: v.DeliveryAddress,
		RequiredByDate:       formatOptionalTime(v.RequiredByDate),
		ResponseDeadline:     formatTime(v.ResponseDeadline),
		SupplierInstructions: v.SupplierInstructions,
		Lines:                toIssuedLineDTOs(v.Lines),
		IssuedByUserID:       v.IssuedByUserID,
		IssuedAt:             formatTime(v.IssuedAt),
	}
}

func toAmendmentDraftDTO(d RFQAmendmentDraft) amendmentDraftDTO {
	return amendmentDraftDTO{
		ID: d.ID, RFQChainID: d.RFQChainID,
		BaseIssuedVersionID: d.BaseIssuedVersionID,
		BaseVersionNumber:   d.BaseVersionNumber,
		Currency:            d.Currency, Title: d.Title,
		DeliveryAddress:      d.DeliveryAddress,
		RequiredByDate:       formatOptionalTime(d.RequiredByDate),
		ResponseDeadline:     formatOptionalTime(d.ResponseDeadline),
		SupplierInstructions: d.SupplierInstructions,
		Lines:                toIssuedLineDTOs(d.Lines),
		Revision:             d.Revision,
		CreatedAt:            formatTime(d.CreatedAt),
		UpdatedAt:            formatTime(d.UpdatedAt),
	}
}

// MapIssuanceError translates module sentinels to HTTP statuses (§14).
//
// An UNRECOGNISED error passes through unchanged. Laundering an infrastructure
// failure into a domain status would tell a client to retry something that can
// never succeed, or to fix content that was never the problem.
func MapIssuanceError(err error) error {
	switch {
	// --- 404: absent, or outside this tenant. Collapsed deliberately so a
	// foreign caller cannot probe for existence.
	case errors.Is(err, ErrIssuanceChainNotFound):
		return huma.Error404NotFound("rfq issuance chain not found")
	case errors.Is(err, ErrIssuedVersionNotFound):
		return huma.Error404NotFound("issued rfq version not found")
	case errors.Is(err, ErrAmendmentDraftNotFound):
		return huma.Error404NotFound("amendment draft not found")
	case errors.Is(err, ErrInvitationNotFound):
		return huma.Error404NotFound("supplier invitation not found")
	case errors.Is(err, ErrDeliveryAttemptNotFound):
		return huma.Error404NotFound("invitation delivery attempt not found")

	// --- 409: a concurrent change or state conflict.
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("the record changed since it was read")
	case errors.Is(err, ErrRFQNotReady):
		return huma.Error409Conflict("that rfq is not ready to issue")
	case errors.Is(err, ErrVersionAlreadyExists):
		return huma.Error409Conflict("that rfq version already exists")
	case errors.Is(err, ErrOperationAlreadyUsed):
		return huma.Error409Conflict("that operation id was already used by another operation")
	case errors.Is(err, ErrAmendmentDraftAlreadyExists):
		return huma.Error409Conflict("an amendment draft already exists for this rfq chain")
	case errors.Is(err, ErrStaleBaseVersion):
		return huma.Error409Conflict(
			"this draft was based on a version that is no longer current")
	case errors.Is(err, ErrInvitationAlreadyExists):
		return huma.Error409Conflict("an invitation already exists for that supplier")
	case errors.Is(err, ErrInvitationRevoked):
		return huma.Error409Conflict("that invitation is revoked")
	case errors.Is(err, ErrInvitationAlreadyActive):
		return huma.Error409Conflict("that invitation is already active and unexpired")
	case errors.Is(err, ErrInvitationNotReactivatable):
		return huma.Error409Conflict("that invitation is neither revoked nor expired")
	case errors.Is(err, ErrDeliveryOperationAlreadyUsed):
		return huma.Error409Conflict(
			"that delivery operation id was already used by another request")

	// --- 422: understood, but not issuable.
	case errors.Is(err, ErrResponseDeadlineRequired):
		return huma.Error422UnprocessableEntity(
			"a response deadline is required before an rfq can be issued")
	case errors.Is(err, ErrCurrencyRequired):
		return huma.Error422UnprocessableEntity("currency is required to issue an rfq")
	case errors.Is(err, ErrInvalidCurrency):
		return huma.Error422UnprocessableEntity("only MYR is supported for rfq issuance")
	case errors.Is(err, ErrOperationIDRequired):
		return huma.Error422UnprocessableEntity("operationId is required")
	case errors.Is(err, ErrMaterialIDRequired):
		return huma.Error422UnprocessableEntity("materialId is required on an added line")
	case errors.Is(err, ErrInvalidQuantity):
		return huma.Error422UnprocessableEntity(
			"line quantity must be a positive decimal with a unit")
	case errors.Is(err, ErrInputLimitExceeded):
		return huma.Error422UnprocessableEntity(
			"one or more procurement inputs exceed an approved limit")
	case errors.Is(err, ErrInvalidBusinessDate):
		return huma.Error422UnprocessableEntity(
			"one or more procurement dates are outside the approved horizon")
	case errors.Is(err, ErrAmendmentLineNotFound):
		return huma.Error422UnprocessableEntity(
			"an amendment line id does not belong to the current draft")
	case errors.Is(err, ErrDuplicateAmendmentLine):
		return huma.Error422UnprocessableEntity(
			"an amendment line id may appear only once")
	case errors.Is(err, ErrInvalidRecipientEmail):
		return huma.Error422UnprocessableEntity("recipientEmail must be a valid email address")
	case errors.Is(err, ErrRecipientNameRequired):
		return huma.Error422UnprocessableEntity("recipientName is required")
	case errors.Is(err, ErrInvitationExpiryNotInFuture):
		return huma.Error422UnprocessableEntity("expiresAt must be in the future")
	case errors.Is(err, ErrSupplierNotInvitable):
		return huma.Error422UnprocessableEntity("that supplier cannot be invited")

	// --- 503: a composition-root wiring fault, never the caller's fault.
	case errors.Is(err, ErrIssuanceNotConfigured):
		return huma.Error503ServiceUnavailable("rfq issuance is unavailable")
	case errors.Is(err, ErrInvitationsNotConfigured):
		return huma.Error503ServiceUnavailable("supplier invitations are unavailable")
	case errors.Is(err, ErrInvitationSecretUnavailable):
		return huma.Error503ServiceUnavailable("the invitation link is unavailable")
	case errors.Is(err, ErrMailDeliveryFailed):
		return huma.Error503ServiceUnavailable("the invitation email could not be delivered")

	default:
		return err
	}
}

// --- inputs and outputs ---

type issueVersionInput struct {
	RFQChainID string `path:"rfqChainId" maxLength:"128" pattern:"^[!-~]+$"`
	Body       struct {
		Currency    string `json:"currency"`
		OperationID string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type issuedVersionOutput struct {
	Status int
	Body   issuedVersionDTO
}

type issuedVersionListOutput struct {
	Body struct {
		Versions []issuedVersionDTO `json:"versions"`
	}
}

type issuanceChainDTO struct {
	RFQChainID             string `json:"rfqChainId"`
	LatestIssuedVersion    int    `json:"latestIssuedVersion"`
	CurrentIssuedVersionID string `json:"currentIssuedVersionId,omitempty"`
	Revision               int64  `json:"revision"`
}

type issuanceChainOutput struct {
	Body issuanceChainDTO
}

type chainPathInput struct {
	RFQChainID string `path:"rfqChainId" maxLength:"128" pattern:"^[!-~]+$"`
}

type versionPathInput struct {
	VersionID string `path:"versionId" maxLength:"128" pattern:"^[!-~]+$"`
}

type amendmentDraftOutput struct {
	Status int
	Body   amendmentDraftDTO
}

type amendmentLinePatchDTO struct {
	// ID identifies a line already present in the current draft. It is omitted
	// for a new line; lineage and provenance are intentionally not accepted.
	ID               string              `json:"id,omitempty" maxLength:"128" pattern:"^[!-~]+$"`
	MaterialID       string              `json:"materialId" maxLength:"128" pattern:"^[!-~]+$"`
	MaterialName     string              `json:"materialName" maxLength:"200"`
	Specification    string              `json:"specification,omitempty" maxLength:"2000"`
	Quantity         issuanceQuantityDTO `json:"quantity"`
	RequiredByDate   string              `json:"requiredByDate,omitempty"`
	ProcurementNotes string              `json:"procurementNotes,omitempty" maxLength:"2000"`
	SortOrder        int                 `json:"sortOrder"`
}

type patchAmendmentDraftInput struct {
	RFQChainID string `path:"rfqChainId" maxLength:"128" pattern:"^[!-~]+$"`
	Body       struct {
		ExpectedRevision     int64                    `json:"expectedRevision"`
		Title                *string                  `json:"title,omitempty" maxLength:"200"`
		DeliveryAddress      *string                  `json:"deliveryAddress,omitempty" maxLength:"500"`
		RequiredByDate       *string                  `json:"requiredByDate,omitempty"`
		ResponseDeadline     *string                  `json:"responseDeadline,omitempty"`
		SupplierInstructions *string                  `json:"supplierInstructions,omitempty" maxLength:"2000"`
		Lines                *[]amendmentLinePatchDTO `json:"lines,omitempty" maxItems:"100"`
	}
}

type issueAmendmentInput struct {
	RFQChainID string `path:"rfqChainId" maxLength:"128" pattern:"^[!-~]+$"`
	Body       struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		OperationID      string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type revisionGuardInput struct {
	RFQChainID string `path:"rfqChainId"`
	Body       struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
}

type emptyOutput struct {
	Status int
}

func parseAmendmentLinePatches(
	inputs []amendmentLinePatchDTO) ([]AmendmentDraftLinePatch, error) {

	lines := make([]AmendmentDraftLinePatch, 0, len(inputs))
	for _, input := range inputs {
		var requiredBy *time.Time
		if input.RequiredByDate != "" {
			parsed, err := time.Parse(timeLayout, input.RequiredByDate)
			if err != nil {
				return nil, err
			}
			requiredBy = &parsed
		}
		lines = append(lines, AmendmentDraftLinePatch{
			ID: input.ID, MaterialID: input.MaterialID,
			MaterialName: input.MaterialName, Specification: input.Specification,
			QuantityValue: input.Quantity.Value, QuantityUnit: input.Quantity.Unit,
			RequiredByDate: requiredBy, ProcurementNotes: input.ProcurementNotes,
			SortOrder: input.SortOrder,
		})
	}
	return lines, nil
}

// RegisterHandlers mounts the contractor issuance routes on the AUTHENTICATED
// group (design spec §12.1).
//
// Role gates follow §13 / §1A.1: issuing a version is externally visible and
// irreversible, so it is owner/admin only, while preparing and reading drafts
// is open to every member. Authorization runs HERE, before the service call —
// the service itself stays role-agnostic.
func RegisterHandlers(api huma.API, svc *Service) {
	// Invitation routes mount through the same entry point, so a composition
	// root cannot wire issuance and silently omit invitations.
	RegisterInvitationHandlers(api, svc)

	// Issuing Version 1. Owner/admin only: this is what makes an RFQ reachable
	// by a Supplier, and it can never be undone.
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-issue-version",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/issue",
		Summary:     "Issue the first immutable version of a ready RFQ",
	}, func(ctx context.Context, input *issueVersionInput) (*issuedVersionOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}

		version, err := svc.IssueVersion(ctx, principal.CompanyID, principal.UserID,
			IssueVersionInput{
				RFQChainID:  input.RFQChainID,
				Currency:    input.Body.Currency,
				OperationID: input.Body.OperationID,
			})
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &issuedVersionOutput{
			Status: http.StatusCreated, Body: toIssuedVersionDTO(version),
		}, nil
	})

	// Immutable history. Every member may view; the service/repository scopes
	// company and chain before any version crosses the boundary.
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-list-versions",
		Method:      http.MethodGet,
		Path:        "/rfq-chains/{rfqChainId}/versions",
		Summary:     "List immutable versions for an RFQ chain",
	}, func(ctx context.Context, input *chainPathInput) (*issuedVersionListOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		versions, err := svc.ListIssuedVersions(ctx, principal.CompanyID, input.RFQChainID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		output := &issuedVersionListOutput{}
		output.Body.Versions = make([]issuedVersionDTO, 0, len(versions))
		for _, version := range versions {
			output.Body.Versions = append(
				output.Body.Versions, toIssuedVersionDTO(version))
		}
		return output, nil
	})

	// Reading an issued version. Every member may view (§13).
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-get-version",
		Method:      http.MethodGet,
		Path:        "/rfq-versions/{versionId}",
		Summary:     "Read one immutable issued RFQ version",
	}, func(ctx context.Context, input *versionPathInput) (*issuedVersionOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		version, err := svc.GetIssuedVersion(ctx, principal.CompanyID, input.VersionID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &issuedVersionOutput{Status: http.StatusOK, Body: toIssuedVersionDTO(version)}, nil
	})

	// State-changing reconciliation is owner/admin only (§13). It can complete
	// a known chain-pointer gap but cannot invent or edit an immutable version.
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-reconcile-chain",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/reconcile",
		Summary:     "Reconcile an interrupted RFQ issuance pointer",
	}, func(ctx context.Context, input *chainPathInput) (*issuanceChainOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}

		chain, err := svc.ReconcileIssuanceChain(
			ctx, principal.CompanyID, principal.UserID, input.RFQChainID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &issuanceChainOutput{Body: issuanceChainDTO{
			RFQChainID:             chain.RFQChainID,
			LatestIssuedVersion:    chain.LatestIssuedVersion,
			CurrentIssuedVersionID: derefOr(chain.CurrentIssuedVersionID),
			Revision:               chain.Revision,
		}}, nil
	})

	// Preparing an amendment draft is editable-draft work: every member (§13).
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-create-amendment-draft",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/amendment-draft",
		Summary:     "Clone the current issued version into an editable amendment draft",
	}, func(ctx context.Context, input *chainPathInput) (*amendmentDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		draft, err := svc.CreateAmendmentDraft(ctx, principal.CompanyID, principal.UserID,
			input.RFQChainID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &amendmentDraftOutput{
			Status: http.StatusCreated, Body: toAmendmentDraftDTO(draft),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-get-amendment-draft",
		Method:      http.MethodGet,
		Path:        "/rfq-chains/{rfqChainId}/amendment-draft",
		Summary:     "Read the amendment draft for an RFQ chain",
	}, func(ctx context.Context, input *chainPathInput) (*amendmentDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		draft, err := svc.GetAmendmentDraft(ctx, principal.CompanyID, input.RFQChainID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &amendmentDraftOutput{Status: http.StatusOK, Body: toAmendmentDraftDTO(draft)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-patch-amendment-draft",
		Method:      http.MethodPatch,
		Path:        "/rfq-chains/{rfqChainId}/amendment-draft",
		Summary:     "Edit the amendment draft",
	}, func(ctx context.Context, input *patchAmendmentDraftInput) (*amendmentDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		patch := AmendmentDraftPatch{
			Title:                input.Body.Title,
			DeliveryAddress:      input.Body.DeliveryAddress,
			SupplierInstructions: input.Body.SupplierInstructions,
		}
		if input.Body.RequiredByDate != nil {
			// Empty string explicitly clears the optional header date; omission
			// leaves it unchanged. The double pointer preserves that distinction.
			var requiredBy *time.Time
			if *input.Body.RequiredByDate != "" {
				parsed, parseErr := time.Parse(timeLayout, *input.Body.RequiredByDate)
				if parseErr != nil {
					return nil, huma.Error422UnprocessableEntity(
						"requiredByDate must be empty or an RFC3339 timestamp")
				}
				requiredBy = &parsed
			}
			patch.RequiredByDate = &requiredBy
		}
		if input.Body.ResponseDeadline != nil {
			parsed, parseErr := time.Parse(timeLayout, *input.Body.ResponseDeadline)
			if parseErr != nil {
				return nil, huma.Error422UnprocessableEntity(
					"responseDeadline must be an RFC3339 timestamp")
			}
			patch.ResponseDeadline = &parsed
		}
		if input.Body.Lines != nil {
			lines, parseErr := parseAmendmentLinePatches(*input.Body.Lines)
			if parseErr != nil {
				return nil, huma.Error422UnprocessableEntity(
					"line requiredByDate must be an RFC3339 timestamp")
			}
			patch.Lines = &lines
		}

		draft, err := svc.UpdateAmendmentDraft(ctx, principal.CompanyID, principal.UserID,
			input.RFQChainID, input.Body.ExpectedRevision, patch)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &amendmentDraftOutput{Status: http.StatusOK, Body: toAmendmentDraftDTO(draft)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-discard-amendment-draft",
		Method:      http.MethodDelete,
		Path:        "/rfq-chains/{rfqChainId}/amendment-draft",
		Summary:     "Discard the amendment draft, leaving issued versions untouched",
	}, func(ctx context.Context, input *revisionGuardInput) (*emptyOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		if err := svc.DiscardAmendmentDraft(ctx, principal.CompanyID, principal.UserID,
			input.RFQChainID, input.Body.ExpectedRevision); err != nil {
			return nil, MapIssuanceError(err)
		}
		return &emptyOutput{Status: http.StatusNoContent}, nil
	})

	// Issuing the amendment. Owner/admin only, for the same reason as first
	// issuance: it publishes a new version to every invited Supplier.
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-issue-amendment",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/amendment-draft/issue",
		Summary:     "Issue the amendment draft as the next immutable version",
	}, func(ctx context.Context, input *issueAmendmentInput) (*issuedVersionOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}

		version, err := svc.IssueAmendment(ctx, principal.CompanyID, principal.UserID,
			IssueAmendmentInput{
				RFQChainID:       input.RFQChainID,
				ExpectedRevision: input.Body.ExpectedRevision,
				OperationID:      input.Body.OperationID,
			})
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &issuedVersionOutput{
			Status: http.StatusCreated, Body: toIssuedVersionDTO(version),
		}, nil
	})
}
