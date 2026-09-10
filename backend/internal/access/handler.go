package access

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// --- Contractor-facing DTOs ---

type decisionSummaryDTO struct {
	Status     string `json:"status"`
	Comment    string `json:"comment,omitempty"`
	ClientName string `json:"clientName,omitempty"`
	DecidedAt  string `json:"decidedAt"`
}

// grantDTO is the contractor-facing grant projection. URL and Token are
// pointers and appear together ONLY on the two 201 responses that mint a
// fresh token (initial share, rotation) — never on any other response
// (design spec §6.6).
type grantDTO struct {
	GrantID         string              `json:"grantId"`
	QuotationID     string              `json:"quotationId"`
	StoredStatus    string              `json:"storedStatus"`
	IsExpired       bool                `json:"isExpired"`
	EffectiveStatus string              `json:"effectiveStatus"`
	URL             *string             `json:"url,omitempty"`
	Token           *string             `json:"token,omitempty"`
	ExpiresAt       string              `json:"expiresAt"`
	RevokedReason   string              `json:"revokedReason,omitempty"`
	Revision        int64               `json:"revision"`
	CreatedAt       string              `json:"createdAt"`
	Decision        *decisionSummaryDTO `json:"decision,omitempty"`
}

func toGrantDTO(v GrantView) grantDTO {
	dto := grantDTO{
		GrantID: v.GrantID, QuotationID: v.QuotationID,
		StoredStatus: v.StoredStatus, IsExpired: v.IsExpired,
		EffectiveStatus: v.EffectiveStatus,
		ExpiresAt:       v.ExpiresAt.Format(timeLayout),
		RevokedReason:   v.RevokedReason,
		Revision:        v.Revision,
		CreatedAt:       v.CreatedAt.Format(timeLayout),
	}
	// A raw token is only ever in scope when this request minted it.
	if v.RawToken != "" {
		token, url := v.RawToken, v.URL
		dto.Token, dto.URL = &token, &url
	}
	if v.HasDecision {
		dto.Decision = &decisionSummaryDTO{
			Status: v.DecisionStatus, Comment: v.DecisionComment,
			ClientName: v.DecisionClientName,
			DecidedAt:  v.DecisionDecidedAt.Format(timeLayout),
		}
	}
	return dto
}

type shareQuotationInput struct {
	ID string `path:"id"`
}

type grantOutput struct {
	Status int
	Body   grantDTO
}

type grantMutationInput struct {
	GrantID string `path:"grantId"`
	Body    struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type updateGrantExpiryInput struct {
	GrantID string `path:"grantId"`
	Body    struct {
		ExpiresAt        string `json:"expiresAt" required:"true"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type getShareStatusInput struct {
	ID string `path:"id"`
}

// --- External Client-facing DTOs ---

type clientMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type clientQuotationLineDTO struct {
	Description string          `json:"description"`
	Quantity    string          `json:"quantity,omitempty"`
	Unit        string          `json:"unit,omitempty"`
	UnitPrice   *clientMoneyDTO `json:"unitPrice,omitempty"`
	Amount      clientMoneyDTO  `json:"amount"`
}

type clientQuotationDTO struct {
	QuotationNumber string                   `json:"quotationNumber"`
	Version         int                      `json:"version"`
	CompanyName     string                   `json:"companyName"`
	ProjectName     string                   `json:"projectName"`
	Currency        string                   `json:"currency"`
	Lines           []clientQuotationLineDTO `json:"lines"`
	Subtotal        clientMoneyDTO           `json:"subtotal"`
	TaxMode         string                   `json:"taxMode"`
	TaxLabel        string                   `json:"taxLabel,omitempty"`
	TaxRateBPS      int64                    `json:"taxRateBps,omitempty"`
	TaxAmount       clientMoneyDTO           `json:"taxAmount"`
	Total           clientMoneyDTO           `json:"total"`
	Terms           string                   `json:"terms,omitempty"`
	PaymentSchedule string                   `json:"paymentSchedule,omitempty"`
	ValidUntil      string                   `json:"validUntil,omitempty"`
	IsExpired       bool                     `json:"isExpired"`
	Status          string                   `json:"status"`
	Decision        string                   `json:"decision,omitempty"`
}

func toClientQuotationDTO(v ClientQuotationView) clientQuotationDTO {
	lines := make([]clientQuotationLineDTO, len(v.Lines))
	for i, l := range v.Lines {
		line := clientQuotationLineDTO{
			Description: l.Description,
			Amount:      clientMoneyDTO{Amount: l.Amount.Amount, Currency: l.Amount.Currency},
		}
		if l.Quantity != nil {
			line.Quantity = *l.Quantity
		}
		if l.Unit != nil {
			line.Unit = *l.Unit
		}
		if l.UnitPrice != nil {
			line.UnitPrice = &clientMoneyDTO{Amount: l.UnitPrice.Amount, Currency: l.UnitPrice.Currency}
		}
		lines[i] = line
	}

	dto := clientQuotationDTO{
		QuotationNumber: v.QuotationNumber, Version: v.Version,
		CompanyName: v.CompanyName, ProjectName: v.ProjectName,
		Currency: v.Currency, Lines: lines,
		Subtotal: clientMoneyDTO{Amount: v.Subtotal.Amount, Currency: v.Subtotal.Currency},
		TaxMode:  v.TaxMode, TaxLabel: v.TaxLabel, TaxRateBPS: v.TaxRateBPS,
		TaxAmount: clientMoneyDTO{Amount: v.TaxAmount.Amount, Currency: v.TaxAmount.Currency},
		Total:     clientMoneyDTO{Amount: v.Total.Amount, Currency: v.Total.Currency},
		Terms:     v.Terms, PaymentSchedule: v.PaymentSchedule,
		IsExpired: v.IsExpired, Status: v.Status, Decision: v.Decision,
	}
	if v.ValidUntil != nil {
		dto.ValidUntil = v.ValidUntil.Format(timeLayout)
	}
	return dto
}

type clientViewInput struct {
	Token string `path:"token"`
}

// The external outputs below declare Cache-Control and Referrer-Policy as
// their own header fields (Huma only emits headers declared directly on the
// output struct, not on an embedded one). Cache-Control prevents commercial
// data being cached by any intermediary; Referrer-Policy stops the URL —
// which contains the raw token — leaking via a Referer header (§3.1).
const (
	externalCacheControl   = platformhttp.ExternalNoStore
	externalReferrerPolicy = "no-referrer"
)

type clientViewOutput struct {
	CacheControl   string `header:"Cache-Control"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           clientQuotationDTO
}

type clientDecisionRequestInput struct {
	Token string `path:"token"`
	Body  struct {
		ClientName  string `json:"clientName,omitempty"`
		ClientEmail string `json:"clientEmail,omitempty"`
		Comment     string `json:"comment,omitempty"`
	}
}

type clientDecisionOutput struct {
	CacheControl   string `header:"Cache-Control"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           struct {
		Status    string `json:"status"`
		DecidedAt string `json:"decidedAt"`
	}
}

// RegisterHandlers registers the 5 authenticated contractor operations on
// authedAPI. The external Client operations are registered separately by
// RegisterExternalHandlers on the base API (design spec §4).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "quotations-share",
		Method:      http.MethodPost,
		Path:        "/quotations/{id}/share",
		Summary:     "Create a secure client access link for a finalized Quotation, or return the current one",
	}, func(ctx context.Context, input *shareQuotationInput) (*grantOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		view, created, err := svc.ShareQuotation(ctx, principal.CompanyID, input.ID, principal.UserID)
		if err != nil {
			return nil, mapAccessError(err)
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		return &grantOutput{Status: status, Body: toGrantDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "access-grants-rotate",
		Method:      http.MethodPost,
		Path:        "/access-grants/{grantId}/rotate",
		Summary:     "Revoke this access grant and issue a replacement with a fresh token",
	}, func(ctx context.Context, input *grantMutationInput) (*grantOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		view, err := svc.RotateGrant(ctx, principal.CompanyID, input.GrantID, principal.UserID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapAccessError(err)
		}
		return &grantOutput{Status: http.StatusCreated, Body: toGrantDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "access-grants-revoke",
		Method:      http.MethodPost,
		Path:        "/access-grants/{grantId}/revoke",
		Summary:     "Revoke this access grant without issuing a replacement",
	}, func(ctx context.Context, input *grantMutationInput) (*grantOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		view, err := svc.RevokeGrant(ctx, principal.CompanyID, input.GrantID, principal.UserID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapAccessError(err)
		}
		return &grantOutput{Status: http.StatusOK, Body: toGrantDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "access-grants-update-expiry",
		Method:      http.MethodPatch,
		Path:        "/access-grants/{grantId}/expiry",
		Summary:     "Change when this access grant expires",
	}, func(ctx context.Context, input *updateGrantExpiryInput) (*grantOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		expiresAt, err := time.Parse(timeLayout, input.Body.ExpiresAt)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("expiresAt must be an RFC3339 timestamp")
		}
		view, err := svc.UpdateGrantExpiry(ctx, principal.CompanyID, input.GrantID, input.Body.ExpectedRevision, expiresAt)
		if err != nil {
			return nil, mapAccessError(err)
		}
		return &grantOutput{Status: http.StatusOK, Body: toGrantDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-share-status",
		Method:      http.MethodGet,
		Path:        "/quotations/{id}/share",
		Summary:     "Read the current access grant and client decision for a Quotation",
	}, func(ctx context.Context, input *getShareStatusInput) (*grantOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		view, err := svc.GetShareStatus(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapAccessError(err)
		}
		return &grantOutput{Status: http.StatusOK, Body: toGrantDTO(view)}, nil
	})
}

// RegisterExternalHandlers registers the 4 UNAUTHENTICATED Client operations
// on the base API. These deliberately bypass identity.RequireAuthHuma by
// being registered outside the authenticated group — the token itself is the
// only credential, and it is validated inside each handler (design spec §4).
func RegisterExternalHandlers(api huma.API, svc *Service) {
	externalAPI := platformhttp.NewExternalGroup(api)
	huma.Register(externalAPI, huma.Operation{
		OperationID: "client-quotations-view",
		Method:      http.MethodGet,
		Path:        "/client/quotations/{token}",
		Summary:     "View a shared Quotation using a secure client link",
		Security:    platformhttp.PublicSecurityRequirements(),
	}, func(ctx context.Context, input *clientViewInput) (*clientViewOutput, error) {
		view, err := svc.ViewQuotationByToken(ctx, input.Token)
		if err != nil {
			return nil, mapExternalError(err)
		}
		return &clientViewOutput{
			CacheControl: externalCacheControl, ReferrerPolicy: externalReferrerPolicy,
			Body: toClientQuotationDTO(view),
		}, nil
	})

	registerDecision := func(operationID, path, status, summary string) {
		huma.Register(externalAPI, huma.Operation{
			OperationID: operationID,
			Method:      http.MethodPost,
			Path:        path,
			Summary:     summary,
			Security:    platformhttp.PublicSecurityRequirements(),
		}, func(ctx context.Context, input *clientDecisionRequestInput) (*clientDecisionOutput, error) {
			result, err := svc.SubmitClientDecision(ctx, ClientDecisionInput{
				Token: input.Token, Status: status,
				ClientName: input.Body.ClientName, ClientEmail: input.Body.ClientEmail,
				Comment: input.Body.Comment,
			})
			if err != nil {
				return nil, mapExternalError(err)
			}
			out := &clientDecisionOutput{
				CacheControl: externalCacheControl, ReferrerPolicy: externalReferrerPolicy,
			}
			out.Body.Status = result.Status
			out.Body.DecidedAt = result.DecidedAt.Format(timeLayout)
			return out, nil
		})
	}

	registerDecision("client-quotations-accept", "/client/quotations/{token}/accept",
		"accepted", "Accept a shared Quotation")
	registerDecision("client-quotations-reject", "/client/quotations/{token}/reject",
		"rejected", "Reject a shared Quotation")
	registerDecision("client-quotations-request-changes", "/client/quotations/{token}/request-changes",
		"changes_requested", "Request changes to a shared Quotation")
}

// mapAccessError maps contractor-facing sentinels to HTTP statuses (§15).
func mapAccessError(err error) error {
	switch {
	case errors.Is(err, ErrQuotationNotFound):
		return huma.Error404NotFound("quotation not found")
	case errors.Is(err, ErrGrantNotFound):
		return huma.Error404NotFound("access grant not found")
	case errors.Is(err, ErrGroupStateNotFound):
		return huma.Error404NotFound("access grant not found")
	case errors.Is(err, ErrQuotationNotFinalized):
		return huma.Error409Conflict("quotation must be finalized before it can be shared")
	case errors.Is(err, ErrQuotationExpiredForShare):
		return huma.Error409Conflict("quotation has already expired and cannot be shared")
	case errors.Is(err, ErrQuotationChainAlreadyAccepted):
		return huma.Error409Conflict("this quotation has already been accepted; no other version may be shared")
	case errors.Is(err, ErrQuotationSuperseded):
		return huma.Error409Conflict("a newer version of this quotation is now active")
	case errors.Is(err, ErrActiveGrantAlreadyExistsForGroup):
		return huma.Error409Conflict("another share or rotation is already active for this quotation")
	case errors.Is(err, ErrGrantRevisionMismatch):
		return huma.Error409Conflict("access grant changed since it was last read")
	case errors.Is(err, ErrGrantNotActive):
		return huma.Error409Conflict("access grant is not active")
	case errors.Is(err, ErrGrantSuperseded):
		return huma.Error409Conflict("this access grant is no longer eligible for rotation")
	case errors.Is(err, ErrDecisionAlreadyAccepted):
		return huma.Error409Conflict("this quotation has already been accepted")
	case errors.Is(err, ErrUnclassifiedDuplicateKey):
		return err // 500 — never retried
	default:
		return err
	}
}

// mapExternalError maps the external path. Every unusable-token condition
// collapses into an identical 410 with the same generic body, so the response
// leaks nothing about which condition failed (design spec §16.2).
func mapExternalError(err error) error {
	switch {
	case errors.Is(err, ErrExternalTokenUnusable):
		return huma.NewError(http.StatusGone,
			"This link is no longer active. Please contact your contractor for an updated link.")
	case errors.Is(err, ErrQuotationExpiredForDecision):
		return huma.Error422UnprocessableEntity("this quotation has expired and can no longer be acted upon")
	case errors.Is(err, ErrCommentRequired):
		return huma.Error422UnprocessableEntity("a comment is required for this decision")
	case errors.Is(err, ErrFieldTooLong):
		return huma.Error422UnprocessableEntity("a submitted field exceeds its maximum length")
	case errors.Is(err, ErrInvalidEmail):
		return huma.Error422UnprocessableEntity("the supplied email address is not valid")
	case errors.Is(err, ErrDecisionAlreadyAccepted):
		return huma.Error409Conflict("this quotation has already been accepted")
	case errors.Is(err, ErrQuotationSuperseded):
		return huma.Error409Conflict("a newer version of this quotation is now active")
	default:
		// External failures never surface repository/provider text. The detailed
		// error remains available to server-side observability, while the Client
		// receives one bounded operational response.
		return huma.Error503ServiceUnavailable(
			"client access is temporarily unavailable")
	}
}
