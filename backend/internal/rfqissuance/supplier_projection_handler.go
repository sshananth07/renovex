package rfqissuance

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

// Supplier projection DTOs repeat the domain allowlist deliberately. The HTTP
// schema cannot accidentally grow when an internal projection or persistence
// model gains a field.
type supplierRFQLineDTO struct {
	ID               string     `json:"id"`
	MaterialName     string     `json:"materialName"`
	Specification    string     `json:"specification"`
	QuantityValue    string     `json:"quantityValue"`
	QuantityUnit     string     `json:"quantityUnit"`
	RequiredByDate   *time.Time `json:"requiredByDate"`
	ProcurementNotes string     `json:"procurementNotes"`
	SortOrder        int        `json:"sortOrder"`
}

type supplierRFQVersionDTO struct {
	ID                   string               `json:"id"`
	RFQNumber            string               `json:"rfqNumber"`
	VersionNumber        int                  `json:"versionNumber"`
	Currency             string               `json:"currency"`
	Title                string               `json:"title"`
	IssuedAt             time.Time            `json:"issuedAt"`
	DeliveryAddress      string               `json:"deliveryAddress"`
	RequiredByDate       *time.Time           `json:"requiredByDate"`
	ResponseDeadline     time.Time            `json:"responseDeadline"`
	SupplierInstructions string               `json:"supplierInstructions"`
	Lines                []supplierRFQLineDTO `json:"lines"`
}

type supplierResponseWindowDTO struct {
	Status     string    `json:"status"`
	Deadline   time.Time `json:"deadline"`
	CanRespond bool      `json:"canRespond"`
}

type supplierInvitationRFQDTO struct {
	InvitationID      string                    `json:"invitationId"`
	Status            string                    `json:"status"`
	ExpiresAt         time.Time                 `json:"expiresAt"`
	CurrentRFQVersion supplierRFQVersionDTO     `json:"currentRfqVersion"`
	ResponseWindow    supplierResponseWindowDTO `json:"responseWindow"`
}

type supplierRFQSummaryDTO struct {
	ID               string    `json:"id"`
	VersionNumber    int       `json:"versionNumber"`
	IssuedAt         time.Time `json:"issuedAt"`
	ResponseDeadline time.Time `json:"responseDeadline"`
	IsCurrent        bool      `json:"isCurrent"`
}

type supplierRFQHistoryDTO struct {
	Versions   []supplierRFQSummaryDTO `json:"versions"`
	NextCursor *int                    `json:"nextCursor"`
}

type supplierInvitationRFQOutput struct {
	Status int
	Body   map[string]any
}

type supplierRFQVersionOutput struct {
	Status int
	Body   map[string]any
}

type supplierRFQHistoryOutput struct {
	Status int
	Body   map[string]any
}

// MapSupplierRFQProjectionError preserves the external non-disclosure rule:
// every absent, foreign, expired, revoked, or stale scoped read is the same
// 404, while an unknown dependency failure remains a bounded 503.
func MapSupplierRFQProjectionError(err error) error {
	switch {
	case errors.Is(err, ErrSupplierRFQAccessInvalid),
		errors.Is(err, ErrInvitationNotFound),
		errors.Is(err, ErrIssuedVersionNotFound):
		return huma.Error404NotFound("invitation not found")
	case errors.Is(err, ErrInputLimitExceeded):
		return huma.Error422UnprocessableEntity("pagination input is invalid")
	default:
		return huma.Error503ServiceUnavailable("supplier RFQ service unavailable")
	}
}

// RegisterSupplierHandlers mounts the invitation-scoped Supplier read surface.
// There is intentionally no collection route: Phase 1 is participation through
// one authorized invitation, not a Supplier-wide portal.
func RegisterSupplierHandlers(api huma.API, service *Service) {
	external := platformhttp.NewExternalGroup(api)

	huma.Register(external, huma.Operation{
		OperationID: "supplier-access-get-invitation-rfq",
		Method:      http.MethodGet, Path: "/supplier-access/invitations/{invitationId}",
		Summary:  "Read the current invitation and RFQ",
		Security: platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionToken string `cookie:"supplier_session"`
		InvitationID string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
	}) (*supplierInvitationRFQOutput, error) {
		projection, err := service.GetSupplierInvitationRFQ(ctx, SupplierRFQReadInput{
			SessionToken: input.SessionToken, InvitationID: input.InvitationID,
			AccessedAt: time.Now().UTC(),
		})
		if err != nil {
			return nil, MapSupplierRFQProjectionError(err)
		}
		return &supplierInvitationRFQOutput{Status: http.StatusOK,
			Body: supplierInvitationRFQBody(projection)}, nil
	})

	huma.Register(external, huma.Operation{
		OperationID: "supplier-access-list-rfq-versions",
		Method:      http.MethodGet,
		Path:        "/supplier-access/invitations/{invitationId}/rfq-versions",
		Summary:     "List invitation RFQ versions",
		Security:    platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionToken string `cookie:"supplier_session"`
		InvitationID string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
		PageSize     int    `query:"pageSize" minimum:"0" maximum:"100"`
		Cursor       int    `query:"cursor" minimum:"0"`
	}) (*supplierRFQHistoryOutput, error) {
		page, err := service.ListSupplierRFQVersions(ctx, SupplierRFQHistoryInput{
			SupplierRFQReadInput: SupplierRFQReadInput{
				SessionToken: input.SessionToken, InvitationID: input.InvitationID,
				AccessedAt: time.Now().UTC(),
			}, PageSize: input.PageSize, Cursor: input.Cursor,
		})
		if err != nil {
			return nil, MapSupplierRFQProjectionError(err)
		}
		return &supplierRFQHistoryOutput{Status: http.StatusOK,
			Body: supplierRFQHistoryBody(page)}, nil
	})

	huma.Register(external, huma.Operation{
		OperationID: "supplier-access-get-rfq-version",
		Method:      http.MethodGet,
		Path:        "/supplier-access/invitations/{invitationId}/rfq-versions/{versionId}",
		Summary:     "Read an invitation RFQ version",
		Security:    platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionToken string `cookie:"supplier_session"`
		InvitationID string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
		VersionID    string `path:"versionId" maxLength:"128" pattern:"^[!-~]+$"`
	}) (*supplierRFQVersionOutput, error) {
		version, err := service.GetSupplierRFQVersion(ctx, SupplierRFQVersionInput{
			SupplierRFQReadInput: SupplierRFQReadInput{
				SessionToken: input.SessionToken, InvitationID: input.InvitationID,
				AccessedAt: time.Now().UTC(),
			}, VersionID: input.VersionID,
		})
		if err != nil {
			return nil, MapSupplierRFQProjectionError(err)
		}
		return &supplierRFQVersionOutput{Status: http.StatusOK,
			Body: supplierRFQVersionBody(version)}, nil
	})
}

// The top-level map is intentional: Huma's default schema-link transformer
// adds a $schema key to struct response bodies. These routes have an approved
// exact JSON allowlist, so the established map boundary prevents that extra
// framework field while the nested DTOs retain explicit JSON names.
func supplierInvitationRFQBody(value SupplierInvitationRFQProjection) map[string]any {
	return map[string]any{
		"invitationId":      value.InvitationID,
		"status":            value.Status,
		"expiresAt":         value.ExpiresAt,
		"currentRfqVersion": toSupplierRFQVersionDTO(value.CurrentRFQVersion),
		"responseWindow": supplierResponseWindowDTO{
			Status: value.ResponseWindow.Status, Deadline: value.ResponseWindow.Deadline,
			CanRespond: value.ResponseWindow.CanRespond,
		},
	}
}

func supplierRFQVersionBody(value SupplierRFQVersion) map[string]any {
	dto := toSupplierRFQVersionDTO(value)
	return map[string]any{
		"id": dto.ID, "rfqNumber": dto.RFQNumber, "versionNumber": dto.VersionNumber,
		"currency": dto.Currency, "title": dto.Title, "issuedAt": dto.IssuedAt,
		"deliveryAddress": dto.DeliveryAddress, "requiredByDate": dto.RequiredByDate,
		"responseDeadline":     dto.ResponseDeadline,
		"supplierInstructions": dto.SupplierInstructions, "lines": dto.Lines,
	}
}

func toSupplierRFQVersionDTO(value SupplierRFQVersion) supplierRFQVersionDTO {
	lines := make([]supplierRFQLineDTO, 0, len(value.Lines))
	for _, line := range value.Lines {
		lines = append(lines, supplierRFQLineDTO{
			ID: line.ID, MaterialName: line.MaterialName, Specification: line.Specification,
			QuantityValue: line.QuantityValue, QuantityUnit: line.QuantityUnit,
			RequiredByDate: line.RequiredByDate, ProcurementNotes: line.ProcurementNotes,
			SortOrder: line.SortOrder,
		})
	}
	return supplierRFQVersionDTO{
		ID: value.ID, RFQNumber: value.RFQNumber, VersionNumber: value.VersionNumber,
		Currency: value.Currency, Title: value.Title, IssuedAt: value.IssuedAt,
		DeliveryAddress: value.DeliveryAddress, RequiredByDate: value.RequiredByDate,
		ResponseDeadline:     value.ResponseDeadline,
		SupplierInstructions: value.SupplierInstructions, Lines: lines,
	}
}

func supplierRFQHistoryBody(value SupplierRFQHistoryPage) map[string]any {
	versions := make([]supplierRFQSummaryDTO, 0, len(value.Versions))
	for _, version := range value.Versions {
		versions = append(versions, supplierRFQSummaryDTO{
			ID: version.ID, VersionNumber: version.VersionNumber, IssuedAt: version.IssuedAt,
			ResponseDeadline: version.ResponseDeadline, IsCurrent: version.IsCurrent,
		})
	}
	return map[string]any{"versions": versions, "nextCursor": value.NextCursor}
}
