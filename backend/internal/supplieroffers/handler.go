package supplieroffers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

// Protected Supplier Offer routes (spec §7).
//
// Every route runs through Phase D authorization: reads use the read
// capability, mutations additionally require CSRF. No route accepts a Company,
// Supplier, recipient or issued-version field, so a caller can never widen its
// own authority through request content.

const supplierOffersNoStore = platformhttp.ExternalNoStore

// MapSupplierOfferError translates module errors into bounded HTTP responses.
//
// Missing and foreign resources collapse to one 404 so a Supplier cannot probe
// for another tenant's identifiers. An unrecognised error becomes a generic 503
// rather than surfacing its text, which could carry infrastructure detail.
func MapSupplierOfferError(err error) error {
	switch {
	case errors.Is(err, ErrOfferDraftNotFound),
		errors.Is(err, ErrOfferVersionNotFound),
		errors.Is(err, ErrOfferChainNotFound),
		errors.Is(err, ErrIssuedRFQNotFound),
		errors.Is(err, ErrCopySourceNotFound):
		return huma.Error404NotFound("offer not found")

	case errors.Is(err, ErrOfferDraftConflict),
		errors.Is(err, ErrOfferEligibilityConflict),
		errors.Is(err, ErrOfferDraftNotEmpty):
		return huma.Error409Conflict("offer state or revision conflict")

	case errors.Is(err, ErrOfferIncomplete):
		return huma.Error422UnprocessableEntity(
			"every RFQ line must be answered before submission")
	case errors.Is(err, ErrOfferReviewPending):
		return huma.Error422UnprocessableEntity(
			"copied content must be reviewed before submission")
	case errors.Is(err, ErrResponseWindowClosed):
		return huma.Error422UnprocessableEntity(
			"the RFQ response window has closed")
	case errors.Is(err, ErrOfferValidityRequired):
		return huma.Error422UnprocessableEntity(
			"offer validity must be later than submission time")
	case errors.Is(err, ErrInputLimitExceeded):
		return huma.Error422UnprocessableEntity(
			"one or more offer inputs exceed an approved limit")
	case errors.Is(err, ErrInvalidBusinessDate):
		return huma.Error422UnprocessableEntity(
			"offer validity is outside the approved horizon")
	case errors.Is(err, ErrInvalidOfferEligibility),
		errors.Is(err, ErrInvalidQuotedLine),
		errors.Is(err, ErrUnsupportedCurrency),
		errors.Is(err, ErrQuotedQuantityMismatch),
		errors.Is(err, ErrInvalidUnitPrice):
		return huma.Error422UnprocessableEntity("offer content is invalid")

	case errors.Is(err, ErrSupplierOfferAccessInvalid):
		return huma.Error404NotFound("offer not found")
	case errors.Is(err, ErrSupplierOfferCSRFRejected):
		return huma.Error403Forbidden("supplier request forbidden")
	case errors.Is(err, ErrOfferStatePending):
		return huma.Error503ServiceUnavailable("offer_state_pending")

	default:
		// Bounded by design: an unknown failure reveals nothing about the
		// infrastructure that produced it.
		return huma.Error503ServiceUnavailable("offer service unavailable")
	}
}

// SupplierOfferCredentials carries the Phase D credentials every protected
// mutation shares.
//
// It is EXPORTED deliberately. Huma resolves path, cookie and header
// parameters by walking exported fields, and an embedded struct whose type is
// unexported is skipped entirely, which silently delivered empty credentials
// to every mutation and made them unreachable through the composed router.
type SupplierOfferCredentials struct {
	SessionCookie string `cookie:"supplier_session"`
	CSRFCookie    string `cookie:"supplier_csrf"`
	CSRFHeader    string `header:"X-CSRF-Token"`
	InvitationID  string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
}

type draftOutput struct {
	Status         int
	CacheControl   string `header:"Cache-Control"`
	Pragma         string `header:"Pragma"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           draftDTO
}

type draftReadOutput struct {
	Status         int
	CacheControl   string `header:"Cache-Control"`
	Pragma         string `header:"Pragma"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           map[string]any
}

// draftReadDTO is the complete own-commercial-state allowlist. Coordination
// identities are absent by construction, while nullable commercial values are
// retained explicitly so clients can distinguish "not supplied" from zero.
type draftReadDTO struct {
	ID                           string                  `json:"id"`
	Currency                     string                  `json:"currency"`
	Status                       string                  `json:"status"`
	Revision                     int64                   `json:"revision"`
	CanEdit                      bool                    `json:"canEdit"`
	CanSubmit                    bool                    `json:"canSubmit"`
	Lines                        []draftReadLineDTO      `json:"lines"`
	Tax                          offerTaxDTO             `json:"tax"`
	OfferTaxReviewRequired       bool                    `json:"offerTaxReviewRequired"`
	ChargeGroups                 []draftChargeGroupDTO   `json:"chargeGroups"`
	DeliveryCharge               *offerDeliveryChargeDTO `json:"deliveryCharge"`
	DeliveryChargeReviewRequired bool                    `json:"deliveryChargeReviewRequired"`
	OfferValidUntil              *time.Time              `json:"offerValidUntil"`
	SupplierNotes                string                  `json:"supplierNotes"`
}

type draftReadLineDTO struct {
	ID                       string           `json:"id"`
	RFQLineID                string           `json:"rfqLineId"`
	ResponseStatus           string           `json:"responseStatus"`
	QuotedQuantity           string           `json:"quotedQuantity"`
	QuotedUnit               string           `json:"quotedUnit"`
	UnitPriceExcludingTax    *offerMoneyDTO   `json:"unitPriceExcludingTax"`
	LineSubtotalExcludingTax *offerMoneyDTO   `json:"lineSubtotalExcludingTax"`
	LineTax                  *offerLineTaxDTO `json:"lineTax"`
	Brand                    string           `json:"brand"`
	SKU                      string           `json:"sku"`
	ProductDescription       string           `json:"productDescription"`
	LeadTime                 string           `json:"leadTime"`
	SupplierLineNotes        string           `json:"supplierLineNotes"`
	CommercialExceptions     string           `json:"commercialExceptions"`
	ReviewRequired           bool             `json:"reviewRequired"`
	ConfirmationRequired     bool             `json:"confirmationRequired"`
}

type draftChargeGroupDTO struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	ApplicableRFQLineIDs []string       `json:"applicableRfqLineIds"`
	Trigger              string         `json:"trigger"`
	Threshold            *offerMoneyDTO `json:"threshold"`
	Calculation          string         `json:"calculation"`
	FixedAmount          *offerMoneyDTO `json:"fixedAmount"`
	RateBPS              *int64         `json:"rateBps"`
	ReviewRequired       bool           `json:"reviewRequired"`
}

// draftDTO is the Supplier-visible projection. It deliberately omits submission
// claim internals and replacement bookkeeping: those are recovery mechanics,
// not offer content.
type draftDTO struct {
	ID                           string           `json:"id"`
	Currency                     string           `json:"currency"`
	Status                       string           `json:"status"`
	Revision                     int64            `json:"revision"`
	Lines                        []draftLineDTO   `json:"lines"`
	OfferTaxReviewRequired       bool             `json:"offerTaxReviewRequired"`
	DeliveryChargeReviewRequired bool             `json:"deliveryChargeReviewRequired"`
	OfferValidUntil              *time.Time       `json:"offerValidUntil,omitempty"`
	SupplierNotes                string           `json:"supplierNotes,omitempty"`
	ChargeGroups                 []chargeGroupDTO `json:"chargeGroups"`
}

type draftLineDTO struct {
	ID                   string `json:"id"`
	RFQLineID            string `json:"rfqLineId"`
	ResponseStatus       string `json:"responseStatus"`
	QuotedQuantity       string `json:"quotedQuantity,omitempty"`
	QuotedUnit           string `json:"quotedUnit,omitempty"`
	UnitPriceMinor       int64  `json:"unitPriceMinor,omitempty"`
	LineSubtotalMinor    int64  `json:"lineSubtotalMinor,omitempty"`
	Brand                string `json:"brand,omitempty"`
	SKU                  string `json:"sku,omitempty"`
	ProductDescription   string `json:"productDescription,omitempty"`
	LeadTime             string `json:"leadTime,omitempty"`
	SupplierLineNotes    string `json:"supplierLineNotes,omitempty"`
	CommercialExceptions string `json:"commercialExceptions,omitempty"`
	ReviewRequired       bool   `json:"reviewRequired"`
	ConfirmationRequired bool   `json:"confirmationRequired"`
}

type chargeGroupDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ReviewRequired bool   `json:"reviewRequired"`
}

func toDraftDTO(draft SupplierOfferDraft) draftDTO {
	lines := make([]draftLineDTO, 0, len(draft.Lines))
	for _, line := range draft.Lines {
		dto := draftLineDTO{
			ID:                   line.ID,
			RFQLineID:            line.RFQLineID,
			ResponseStatus:       string(line.ResponseStatus),
			Brand:                line.Brand,
			SKU:                  line.SKU,
			ProductDescription:   line.ProductDescription,
			LeadTime:             line.LeadTime,
			SupplierLineNotes:    line.SupplierLineNotes,
			CommercialExceptions: line.CommercialExceptions,
			ReviewRequired:       line.ReviewRequired,
			ConfirmationRequired: line.ConfirmationRequired,
		}
		if line.QuotedQuantity != nil {
			dto.QuotedQuantity = line.QuotedQuantity.Value.String()
			dto.QuotedUnit = line.QuotedQuantity.Unit
		}
		if line.UnitPriceExcludingTax != nil {
			dto.UnitPriceMinor = line.UnitPriceExcludingTax.Amount
		}
		if line.LineSubtotalExcludingTax != nil {
			dto.LineSubtotalMinor = line.LineSubtotalExcludingTax.Amount
		}
		lines = append(lines, dto)
	}

	groups := make([]chargeGroupDTO, 0, len(draft.ChargeGroups))
	for _, group := range draft.ChargeGroups {
		groups = append(groups, chargeGroupDTO{
			ID: group.ID, Name: group.Name, ReviewRequired: group.ReviewRequired,
		})
	}

	return draftDTO{
		ID:                           draft.ID,
		Currency:                     draft.Currency,
		Status:                       string(draft.Status),
		Revision:                     draft.Revision,
		Lines:                        lines,
		OfferTaxReviewRequired:       draft.OfferTaxReviewRequired,
		DeliveryChargeReviewRequired: draft.DeliveryChargeReviewRequired,
		OfferValidUntil:              draft.OfferValidUntil,
		SupplierNotes:                draft.SupplierNotes,
		ChargeGroups:                 groups,
	}
}

func draftResponse(draft SupplierOfferDraft) *draftOutput {
	return &draftOutput{
		Status:         http.StatusOK,
		CacheControl:   supplierOffersNoStore,
		Pragma:         "no-cache",
		ReferrerPolicy: "no-referrer",
		Body:           toDraftDTO(draft),
	}
}

func draftReadResponse(projection SupplierOfferDraftProjection) *draftReadOutput {
	draft := projection.Draft
	lines := make([]draftReadLineDTO, 0, len(draft.Lines))
	for _, line := range draft.Lines {
		dto := draftReadLineDTO{ID: line.ID, RFQLineID: line.RFQLineID,
			ResponseStatus: string(line.ResponseStatus), Brand: line.Brand, SKU: line.SKU,
			ProductDescription: line.ProductDescription, LeadTime: line.LeadTime,
			SupplierLineNotes: line.SupplierLineNotes, CommercialExceptions: line.CommercialExceptions,
			ReviewRequired: line.ReviewRequired, ConfirmationRequired: line.ConfirmationRequired}
		if line.QuotedQuantity != nil {
			dto.QuotedQuantity = line.QuotedQuantity.Value.String()
			dto.QuotedUnit = line.QuotedQuantity.Unit
		}
		if line.UnitPriceExcludingTax != nil {
			value := toOfferMoneyDTO(*line.UnitPriceExcludingTax)
			dto.UnitPriceExcludingTax = &value
		}
		if line.LineSubtotalExcludingTax != nil {
			value := toOfferMoneyDTO(*line.LineSubtotalExcludingTax)
			dto.LineSubtotalExcludingTax = &value
		}
		if line.LineTax != nil {
			dto.LineTax = &offerLineTaxDTO{TaxType: string(line.LineTax.TaxType),
				Exempt: line.LineTax.Exempt, RegistrationNumber: line.LineTax.RegistrationNumber,
				BasisNote: line.LineTax.BasisNote}
			if line.LineTax.RateBPS != nil {
				rate := int64(*line.LineTax.RateBPS)
				dto.LineTax.RateBPS = &rate
			}
		}
		lines = append(lines, dto)
	}
	tax := offerTaxDTO{Mode: string(draft.Tax.Mode)}
	if draft.Tax.OfferLevel != nil {
		tax.OfferLevel = &offerLevelTaxDTO{TaxType: string(draft.Tax.OfferLevel.TaxType),
			TaxAmount:          toOfferMoneyDTO(draft.Tax.OfferLevel.TaxAmount),
			BasisNote:          draft.Tax.OfferLevel.BasisNote,
			RegistrationNumber: draft.Tax.OfferLevel.RegistrationNumber}
	}
	groups := make([]draftChargeGroupDTO, 0, len(draft.ChargeGroups))
	for _, group := range draft.ChargeGroups {
		dto := draftChargeGroupDTO{ID: group.ID, Name: group.Name,
			Description: group.Description, ApplicableRFQLineIDs: group.ApplicableRFQLineIDs,
			Trigger: string(group.Trigger), Calculation: string(group.Calculation),
			ReviewRequired: group.ReviewRequired}
		if group.Threshold != nil {
			value := toOfferMoneyDTO(*group.Threshold)
			dto.Threshold = &value
		}
		if group.FixedAmount != nil {
			value := toOfferMoneyDTO(*group.FixedAmount)
			dto.FixedAmount = &value
		}
		if group.RateBPS != nil {
			rate := int64(*group.RateBPS)
			dto.RateBPS = &rate
		}
		groups = append(groups, dto)
	}
	var delivery *offerDeliveryChargeDTO
	if draft.DeliveryCharge != nil {
		delivery = &offerDeliveryChargeDTO{Amount: toOfferMoneyDTO(draft.DeliveryCharge.Amount)}
	}
	body := map[string]any{
		"id": draft.ID, "currency": draft.Currency, "status": string(draft.Status),
		"revision": draft.Revision, "canEdit": projection.CanEdit,
		"canSubmit": projection.CanSubmit, "lines": lines, "tax": tax,
		"offerTaxReviewRequired": draft.OfferTaxReviewRequired, "chargeGroups": groups,
		"deliveryCharge":               delivery,
		"deliveryChargeReviewRequired": draft.DeliveryChargeReviewRequired,
		"offerValidUntil":              draft.OfferValidUntil, "supplierNotes": draft.SupplierNotes,
	}
	return &draftReadOutput{Status: http.StatusOK, CacheControl: supplierOffersNoStore,
		Pragma: "no-cache", ReferrerPolicy: "no-referrer", Body: body}
}

func (input SupplierOfferCredentials) mutationContext(
	accessedAt time.Time,
) SupplierOfferMutationContextInput {
	return SupplierOfferMutationContextInput{
		SessionToken: input.SessionCookie,
		InvitationID: input.InvitationID,
		CSRFCookie:   input.CSRFCookie,
		CSRFHeader:   input.CSRFHeader,
		AccessedAt:   accessedAt,
	}
}

// RegisterHandlers mounts the protected Supplier Offer routes.
//
// They share Phase D's no-store boundary: an offer response must never sit in a
// shared cache, and the referrer must not carry an invitation-scoped path to a
// third party.
func RegisterHandlers(api huma.API, service *Service) {
	offersAPI := platformhttp.NewExternalGroup(api)
	registerSupplierOfferRoutes(offersAPI, service, supplierOfferRouteSet{
		operationPrefix: "supplier-offers",
		base:            "/supplier-offers/{invitationId}",
		draft:           "/supplier-offers/{invitationId}/draft",
		validity:        "/supplier-offers/{invitationId}/draft/offer-validity",
	})
	registerSupplierOfferRoutes(offersAPI, service, supplierOfferRouteSet{
		operationPrefix: "supplier-access-offer",
		base:            "/supplier-access/invitations/{invitationId}/offer",
		draft:           "/supplier-access/invitations/{invitationId}/offer",
		validity:        "/supplier-access/invitations/{invitationId}/offer/validity",
	})
}

type supplierOfferRouteSet struct {
	operationPrefix string
	base            string
	draft           string
	validity        string
}

func (routes supplierOfferRouteSet) operation(name string) string {
	return routes.operationPrefix + "-" + name
}

func registerSupplierOfferRoutes(
	offersAPI huma.API, service *Service, routes supplierOfferRouteSet,
) {

	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("get-draft"),
		Method:      http.MethodGet,
		Path:        routes.draft,
		Summary:     "Read the current offer draft",
		Security:    platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionCookie string `cookie:"supplier_session"`
		InvitationID  string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
	}) (*draftReadOutput, error) {

		projection, err := service.GetActiveDraftProjection(ctx, SupplierOfferReadContextInput{
			SessionToken: input.SessionCookie,
			InvitationID: input.InvitationID,
			AccessedAt:   time.Now().UTC(),
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftReadResponse(projection), nil
	})

	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("create-draft"),
		Method:      http.MethodPost,
		Path:        routes.draft,
		Summary:     "Create or return the active offer draft",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
	}) (*draftOutput, error) {

		draft, err := service.CreateOrGetActiveDraft(ctx,
			input.mutationContext(time.Now().UTC()))
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("quote-line"),
		Method:      http.MethodPut,
		Path:        routes.draft + "/lines/{lineId}/quote",
		Summary:     "Price one offer line",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		LineID string `path:"lineId" maxLength:"128" pattern:"^[!-~]+$"`
		Body   struct {
			DraftID              string `json:"draftId" maxLength:"128" pattern:"^[!-~]+$"`
			ExpectedRevision     int64  `json:"expectedRevision"`
			UnitPriceMinor       int64  `json:"unitPriceMinor"`
			Brand                string `json:"brand,omitempty" maxLength:"200"`
			SKU                  string `json:"sku,omitempty" maxLength:"128"`
			ProductDescription   string `json:"productDescription,omitempty" maxLength:"2000"`
			LeadTime             string `json:"leadTime,omitempty" maxLength:"200"`
			SupplierLineNotes    string `json:"supplierLineNotes,omitempty" maxLength:"2000"`
			CommercialExceptions string `json:"commercialExceptions,omitempty" maxLength:"2000"`
		}
	}) (*draftOutput, error) {

		draft, err := service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
			Context:              input.mutationContext(time.Now().UTC()),
			DraftID:              input.Body.DraftID,
			ExpectedRevision:     input.Body.ExpectedRevision,
			DraftLineID:          input.LineID,
			UnitPriceMinor:       input.Body.UnitPriceMinor,
			Brand:                input.Body.Brand,
			SKU:                  input.Body.SKU,
			ProductDescription:   input.Body.ProductDescription,
			LeadTime:             input.Body.LeadTime,
			SupplierLineNotes:    input.Body.SupplierLineNotes,
			CommercialExceptions: input.Body.CommercialExceptions,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("decline-line"),
		Method:      http.MethodPut,
		Path:        routes.draft + "/lines/{lineId}/decline",
		Summary:     "Record a no-bid or unavailable response",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		LineID string `path:"lineId" maxLength:"128" pattern:"^[!-~]+$"`
		Body   struct {
			DraftID           string `json:"draftId" maxLength:"128" pattern:"^[!-~]+$"`
			ExpectedRevision  int64  `json:"expectedRevision"`
			ResponseStatus    string `json:"responseStatus"`
			SupplierLineNotes string `json:"supplierLineNotes,omitempty" maxLength:"2000"`
		}
	}) (*draftOutput, error) {

		draft, err := service.DeclineDraftLine(ctx, DeclineDraftLineCommand{
			Context:           input.mutationContext(time.Now().UTC()),
			DraftID:           input.Body.DraftID,
			ExpectedRevision:  input.Body.ExpectedRevision,
			DraftLineID:       input.LineID,
			ResponseStatus:    OfferLineResponseStatus(input.Body.ResponseStatus),
			SupplierLineNotes: input.Body.SupplierLineNotes,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	// Offer validity is a separate narrow setter rather than part of a generic
	// draft PATCH: validity never copies forward, so it must be stated
	// explicitly per submission, and a narrow shape keeps the validation and
	// security surface minimal (§7).
	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("set-offer-validity"),
		Method:      http.MethodPut,
		Path:        routes.validity,
		Summary:     "Set how long the offer remains valid",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		Body struct {
			DraftID          string    `json:"draftId" maxLength:"128" pattern:"^[!-~]+$"`
			ExpectedRevision int64     `json:"expectedRevision"`
			OfferValidUntil  time.Time `json:"offerValidUntil"`
		}
	}) (*draftOutput, error) {

		setAt := time.Now().UTC()
		// Validate the horizon HERE as well as at submission: storing a date
		// the Supplier cannot submit with would report success and then fail
		// later, when the response window may already have closed.
		if input.Body.OfferValidUntil.IsZero() ||
			!input.Body.OfferValidUntil.After(setAt) {
			return nil, MapSupplierOfferError(ErrOfferValidityRequired)
		}
		if err := procurementlimits.ValidateOfferValidity(
			setAt, input.Body.OfferValidUntil); err != nil {
			return nil, MapSupplierOfferError(ErrInvalidBusinessDate)
		}

		draft, err := service.SetOfferValidity(ctx, SetOfferValidityCommand{
			Context:          input.mutationContext(setAt),
			DraftID:          input.Body.DraftID,
			ExpectedRevision: input.Body.ExpectedRevision,
			OfferValidUntil:  input.Body.OfferValidUntil,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("submit"),
		Method:      http.MethodPost,
		Path:        routes.base + "/submissions",
		Summary:     "Submit the draft as an immutable offer version",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		Body struct {
			DraftID          string `json:"draftId" maxLength:"128" pattern:"^[!-~]+$"`
			ExpectedRevision int64  `json:"expectedRevision"`
			OperationID      string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
		}
	}) (*submittedVersionOutput, error) {

		version, err := service.SubmitOffer(ctx, SubmitOfferCommand{
			Context:          input.mutationContext(time.Now().UTC()),
			DraftID:          input.Body.DraftID,
			ExpectedRevision: input.Body.ExpectedRevision,
			OperationID:      input.Body.OperationID,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return &submittedVersionOutput{
			Status:         http.StatusCreated,
			CacheControl:   supplierOffersNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			Body: submittedVersionDTO{
				ID:              version.ID,
				VersionNumber:   version.VersionNumber,
				Currency:        version.Currency,
				GrandTotalMinor: version.GrandTotal.Amount,
				SubmittedAt:     version.SubmittedAt,
			},
		}, nil
	})

	huma.Register(offersAPI, huma.Operation{
		OperationID: routes.operation("withdraw"),
		Method:      http.MethodPost,
		Path:        routes.base + "/versions/{versionId}/withdrawal",
		Summary:     "Withdraw a submitted offer version",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		VersionID string `path:"versionId" maxLength:"128" pattern:"^[!-~]+$"`
		Body      struct {
			Reason      string `json:"reason" maxLength:"500"`
			OperationID string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
		}
	}) (*withdrawalOutput, error) {

		withdrawal, err := service.WithdrawOffer(ctx, WithdrawOfferCommand{
			Context:        input.mutationContext(time.Now().UTC()),
			OfferVersionID: input.VersionID,
			Reason:         input.Body.Reason,
			OperationID:    input.Body.OperationID,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return &withdrawalOutput{
			Status:         http.StatusOK,
			CacheControl:   supplierOffersNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			Body: withdrawalDTO{
				ID:          withdrawal.ID,
				Reason:      withdrawal.Reason,
				WithdrawnAt: withdrawal.WithdrawnAt,
			},
		}, nil
	})

	registerM81DraftMutationHandlers(offersAPI, service, routes)
	registerSupplierOfferHistoryHandlers(offersAPI, service, routes)
}

// m81RevisionBody is the complete request body shared by all seven M8.1
// draft-mutation routes: an expected revision guards the CAS, and nothing
// else. There is deliberately no operationId (draft mutations use
// state-based convergence, not a separate idempotency-key mechanism — see
// editDraftConverging) and no source, draft, Company or Supplier field.
type m81RevisionBody struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

// registerM81DraftMutationHandlers mounts the seven M8.1 Supplier Offer draft
// mutations: copy-forward, the three acknowledgements, the two removals and
// line reset. Every handler resolves its draft, Company, Supplier, recipient
// and RFQ context from the authorized session — never from the request body —
// and returns the same safe draft projection every other mutation returns.
func registerM81DraftMutationHandlers(
	api huma.API, service *Service, routes supplierOfferRouteSet,
) {
	huma.Register(api, huma.Operation{
		OperationID: routes.operation("copy-forward"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/copy-forward",
		Summary:     "Copy the automatically-resolved prior submission into the empty active draft",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		Body m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		outcome, err := service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(outcome.Draft), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("acknowledge-tax"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/tax/acknowledge",
		Summary:     "Acknowledge copied offer-level tax",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		Body m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		draft, err := service.AcknowledgeOfferTax(ctx, AcknowledgeOfferTaxCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("acknowledge-charge-group"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/charge-groups/{chargeGroupId}/acknowledge",
		Summary:     "Acknowledge one copied conditional charge group",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		ChargeGroupID string `path:"chargeGroupId" maxLength:"128" pattern:"^[!-~]+$"`
		Body          m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		draft, err := service.AcknowledgeChargeGroup(ctx, AcknowledgeChargeGroupCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
			ChargeGroupID:    input.ChargeGroupID,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("acknowledge-delivery-charge"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/delivery-charge/acknowledge",
		Summary:     "Acknowledge a copied delivery charge",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		Body m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		draft, err := service.AcknowledgeDeliveryCharge(ctx, AcknowledgeDeliveryChargeCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("remove-charge-group"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/charge-groups/{chargeGroupId}/remove",
		Summary:     "Remove one conditional charge group entirely",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		ChargeGroupID string `path:"chargeGroupId" maxLength:"128" pattern:"^[!-~]+$"`
		Body          m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		draft, err := service.RemoveChargeGroup(ctx, RemoveChargeGroupCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
			ChargeGroupID:    input.ChargeGroupID,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("remove-delivery-charge"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/delivery-charge/remove",
		Summary:     "Remove the delivery charge entirely",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		Body m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		draft, err := service.RemoveDeliveryCharge(ctx, RemoveDeliveryChargeCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("reset-line"),
		Method:      http.MethodPost,
		Path:        routes.draft + "/lines/{lineId}/reset",
		Summary:     "Reset one draft line's response back to unanswered",
		Security:    platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SupplierOfferCredentials
		LineID string `path:"lineId" maxLength:"128" pattern:"^[!-~]+$"`
		Body   m81RevisionBody
	}) (*draftOutput, error) {
		accessedAt := time.Now().UTC()
		draft, err := service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
			Context:          input.mutationContext(accessedAt),
			ExpectedRevision: input.Body.ExpectedRevision,
			DraftLineID:      input.LineID,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return draftResponse(draft), nil
	})
}

type submittedVersionOutput struct {
	Status         int
	CacheControl   string `header:"Cache-Control"`
	Pragma         string `header:"Pragma"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           submittedVersionDTO
}

// submittedVersionDTO returns only what the Supplier needs to confirm their own
// submission. Fingerprints and draft provenance are recovery internals.
type submittedVersionDTO struct {
	ID              string    `json:"id"`
	VersionNumber   int       `json:"versionNumber"`
	Currency        string    `json:"currency"`
	GrandTotalMinor int64     `json:"grandTotalMinor"`
	SubmittedAt     time.Time `json:"submittedAt"`
}

type withdrawalOutput struct {
	Status         int
	CacheControl   string `header:"Cache-Control"`
	Pragma         string `header:"Pragma"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           withdrawalDTO
}

type withdrawalDTO struct {
	ID          string    `json:"id"`
	Reason      string    `json:"reason"`
	WithdrawnAt time.Time `json:"withdrawnAt"`
}
