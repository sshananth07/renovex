package awards

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// Contractor-facing Phase F routes (§8A.1A).
//
// Every checkpoint that owns a behavior mounts its own route here, because
// authorization, tenant isolation and non-disclosing not-found behavior are
// properties of a route: deferring the route would defer the tests that prove
// them. CompanyID always comes from the authenticated Principal, never from
// request content, so a caller cannot widen its own tenancy.

// MapAwardError translates module errors into the bounded HTTP responses
// §8C–§8K fix.
//
// Missing and foreign resources collapse to one 404 so a caller cannot probe
// for another tenant's identifiers. An unrecognised error becomes a generic 503
// rather than surfacing its text, which could carry infrastructure detail.
func MapAwardError(err error) error {
	switch {
	case errors.Is(err, ErrIssuedRFQNotFound),
		errors.Is(err, ErrAwardChainNotFound),
		errors.Is(err, ErrAwardDraftNotFound),
		errors.Is(err, ErrAwardRevisionNotFound),
		errors.Is(err, ErrAwardOutcomeNotFound),
		errors.Is(err, ErrAwardDeliveryNotFound):
		return huma.Error404NotFound("award resource not found")

	case errors.Is(err, ErrAwardDeliveryConflict),
		errors.Is(err, ErrAwardDraftConflict),
		errors.Is(err, ErrAwardRevisionConflict),
		errors.Is(err, ErrOfferVersionNotEligible),
		errors.Is(err, ErrRFQLineAwardConflict):
		return huma.Error409Conflict("award state or revision conflict")

	// The ten F3 validations stay distinct on the wire so a contractor learns
	// what to fix rather than receiving one opaque refusal.
	case errors.Is(err, ErrOfferVersionNotSelectable):
		return huma.Error422UnprocessableEntity(
			"offer version is not selectable for this issued rfq version")
	case errors.Is(err, ErrOfferVersionExpired):
		return huma.Error422UnprocessableEntity("offer version validity has passed")
	case errors.Is(err, ErrOfferLineNotQuoted):
		return huma.Error422UnprocessableEntity(
			"only a positively quoted offer line may be awarded")
	case errors.Is(err, ErrQuantityOrUnitMismatch):
		return huma.Error422UnprocessableEntity(
			"quoted quantity and unit must match the issued rfq line")
	case errors.Is(err, ErrCurrencyMismatch):
		return huma.Error422UnprocessableEntity(
			"offer currency must match the issued rfq currency")
	case errors.Is(err, ErrOfferLevelTaxRequiresComplete):
		return huma.Error422UnprocessableEntity(
			"offer-level tax requires awarding every quoted line of that offer")
	case errors.Is(err, ErrConditionalChargesNotResolvable):
		return huma.Error422UnprocessableEntity(
			"conditional charge rules do not resolve for the selected lines")
	case errors.Is(err, ErrRFQLineAlreadyAwarded):
		return huma.Error422UnprocessableEntity(
			"this rfq line is already awarded on this rfq chain")
	case errors.Is(err, ErrAwardCorrectionNotMonotonic):
		return huma.Error422UnprocessableEntity(
			"a correction may not remove, reduce or reassign a published award")
	case errors.Is(err, ErrInvalidUnawardedReason):
		return huma.Error422UnprocessableEntity(
			"unawarded reason is outside the bounded set")
	case errors.Is(err, ErrInvalidAwardDecision),
		errors.Is(err, ErrInvalidAwardDraft),
		errors.Is(err, ErrInvalidAwardChain):
		return huma.Error422UnprocessableEntity("award decision content is invalid")
	case errors.Is(err, ErrAwardDraftIncomplete):
		return huma.Error422UnprocessableEntity(
			"every issued rfq line must be decided before finalisation")
	case errors.Is(err, ErrChangeReasonRequired):
		return huma.Error422UnprocessableEntity(
			"a change reason is required from revision 2 onward")

	// The crash-window read is explicitly retryable. It must never present as
	// "no award exists" while an authoritative revision is present (§8F).
	case errors.Is(err, ErrAwardFinalisationPending):
		return huma.Error503ServiceUnavailable("award finalisation is in progress")

	default:
		// Bounded by design: an unknown failure reveals nothing about the
		// infrastructure that produced it.
		return huma.Error503ServiceUnavailable("award service unavailable")
	}
}

type comparisonPathInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
}

type awardMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// indicativeAmountDTO keeps the indicative flag on the wire. A client that
// checks before trusting a total gets the right answer without needing to have
// read the spec.
type indicativeAmountDTO struct {
	Amount     awardMoneyDTO `json:"amount"`
	Indicative bool          `json:"indicative"`
}

type comparisonLineDTO struct {
	IssuedRFQLineID    string         `json:"issuedRfqLineId"`
	StableLineageID    string         `json:"stableLineageId"`
	OfferLineID        string         `json:"offerLineId"`
	ResponseStatus     string         `json:"responseStatus"`
	UnitPrice          *awardMoneyDTO `json:"unitPrice,omitempty"`
	LineSubtotal       *awardMoneyDTO `json:"lineSubtotal,omitempty"`
	LineTaxAmount      awardMoneyDTO  `json:"lineTaxAmount"`
	Brand              string         `json:"brand,omitempty"`
	SKU                string         `json:"sku,omitempty"`
	ProductDescription string         `json:"productDescription,omitempty"`
	LeadTime           string         `json:"leadTime,omitempty"`
	SupplierLineNotes  string         `json:"supplierLineNotes,omitempty"`
	Exceptions         string         `json:"exceptions,omitempty"`
	Selectable         bool           `json:"selectable"`
}

type comparisonOfferDTO struct {
	OfferVersionID string    `json:"offerVersionId"`
	OfferChainID   string    `json:"offerChainId"`
	SupplierID     string    `json:"supplierId"`
	SupplierName   string    `json:"supplierName"`
	InvitationID   string    `json:"invitationId"`
	VersionNumber  int       `json:"versionNumber"`
	Currency       string    `json:"currency"`
	SubmittedAt    time.Time `json:"submittedAt"`
	ValidUntil     time.Time `json:"validUntil"`
	SupplierNotes  string    `json:"supplierNotes,omitempty"`

	Label         string `json:"label"`
	IsDefaultView bool   `json:"isDefaultView"`
	Selectable    bool   `json:"selectable"`

	Lines []comparisonLineDTO `json:"lines"`

	TaxMode                  string              `json:"taxMode"`
	OfferLevelTaxAmount      awardMoneyDTO       `json:"offerLevelTaxAmount"`
	IndicativeQuotedSubtotal indicativeAmountDTO `json:"indicativeQuotedSubtotal"`
	IndicativeGrandTotal     indicativeAmountDTO `json:"indicativeGrandTotal"`

	PartialAwardUnavailable       bool     `json:"partialAwardUnavailable"`
	CompleteSelectionOfferLineIDs []string `json:"completeSelectionOfferLineIds,omitempty"`
}

// comparisonDTO carries no rank and no recommendation field at all: M8 ranks
// nothing and recommends no winner (§8B), and an absent field cannot be
// populated by a later change without this contract visibly widening.
type comparisonDTO struct {
	IssuedRFQVersionID string               `json:"issuedRfqVersionId"`
	RFQChainID         string               `json:"rfqChainId"`
	RFQNumber          string               `json:"rfqNumber"`
	VersionNumber      int                  `json:"versionNumber"`
	Currency           string               `json:"currency"`
	Offers             []comparisonOfferDTO `json:"offers"`
	Authoritative      bool                 `json:"authoritative"`
}

type comparisonOutput struct {
	Body comparisonDTO
}

func toAwardMoneyDTO(amount money.Money) awardMoneyDTO {
	return awardMoneyDTO{Amount: amount.Amount, Currency: amount.Currency}
}

func toAwardMoneyDTOPointer(amount *money.Money) *awardMoneyDTO {
	if amount == nil {
		return nil
	}
	dto := toAwardMoneyDTO(*amount)
	return &dto
}

func toComparisonDTO(comparison Comparison) comparisonDTO {
	offers := make([]comparisonOfferDTO, 0, len(comparison.Offers))
	for _, offer := range comparison.Offers {
		lines := make([]comparisonLineDTO, 0, len(offer.Lines))
		for _, line := range offer.Lines {
			lines = append(lines, comparisonLineDTO{
				IssuedRFQLineID:    line.IssuedRFQLineID,
				StableLineageID:    line.StableLineageID,
				OfferLineID:        line.OfferLineID,
				ResponseStatus:     string(line.ResponseStatus),
				UnitPrice:          toAwardMoneyDTOPointer(line.UnitPrice),
				LineSubtotal:       toAwardMoneyDTOPointer(line.LineSubtotal),
				LineTaxAmount:      toAwardMoneyDTO(line.LineTaxAmount),
				Brand:              line.Brand,
				SKU:                line.SKU,
				ProductDescription: line.ProductDescription,
				LeadTime:           line.LeadTime,
				SupplierLineNotes:  line.SupplierLineNotes,
				Exceptions:         line.Exceptions,
				Selectable:         line.Selectable,
			})
		}
		offers = append(offers, comparisonOfferDTO{
			OfferVersionID:      offer.OfferVersionID,
			OfferChainID:        offer.OfferChainID,
			SupplierID:          offer.SupplierID,
			SupplierName:        offer.SupplierName,
			InvitationID:        offer.InvitationID,
			VersionNumber:       offer.VersionNumber,
			Currency:            offer.Currency,
			SubmittedAt:         offer.SubmittedAt,
			ValidUntil:          offer.ValidUntil,
			SupplierNotes:       offer.SupplierNotes,
			Label:               string(offer.Label),
			IsDefaultView:       offer.IsDefaultView,
			Selectable:          offer.Selectable,
			Lines:               lines,
			TaxMode:             string(offer.TaxMode),
			OfferLevelTaxAmount: toAwardMoneyDTO(offer.OfferLevelTaxAmount),
			IndicativeQuotedSubtotal: indicativeAmountDTO{
				Amount:     toAwardMoneyDTO(offer.IndicativeQuotedSubtotal.Amount),
				Indicative: offer.IndicativeQuotedSubtotal.Indicative,
			},
			IndicativeGrandTotal: indicativeAmountDTO{
				Amount:     toAwardMoneyDTO(offer.IndicativeGrandTotal.Amount),
				Indicative: offer.IndicativeGrandTotal.Indicative,
			},
			PartialAwardUnavailable:       offer.PartialAwardUnavailable,
			CompleteSelectionOfferLineIDs: offer.CompleteSelectionOfferLineIDs,
		})
	}

	return comparisonDTO{
		IssuedRFQVersionID: comparison.IssuedRFQVersionID,
		RFQChainID:         comparison.RFQChainID,
		RFQNumber:          comparison.RFQNumber,
		VersionNumber:      comparison.VersionNumber,
		Currency:           comparison.Currency,
		Offers:             offers,
		Authoritative:      comparison.Authoritative,
	}
}

// RegisterHandlers mounts every contractor-facing Phase F route.
//
// Routes mount through this single entry point so a composition root cannot
// wire one checkpoint's surface and silently omit another's.
func RegisterHandlers(api huma.API, svc *Service) {
	// Draft routes mount through the same entry point, so a composition root
	// cannot wire comparison and silently omit the draft lifecycle.
	registerAwardDraftHandlers(api, svc)
	registerAwardRevisionHandlers(api, svc)
	registerAwardOutcomeHandlers(api, svc)
	registerAwardDeliveryHandlers(api, svc)

	// Comparison read. Owner, admin and employee: a comparison is preparation,
	// not an externally visible transition (§8B).
	huma.Register(api, huma.Operation{
		OperationID: "awards-get-comparison",
		Method:      http.MethodGet,
		Path:        "/rfq-chains/{rfqChainId}/issued-versions/{versionId}/comparison",
		Summary:     "Compare supplier offers for an issued RFQ version",
	}, func(ctx context.Context, input *comparisonPathInput) (*comparisonOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}

		comparison, err := svc.GetComparison(
			ctx, principal.CompanyID, input.VersionID, time.Now().UTC())
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &comparisonOutput{Body: toComparisonDTO(comparison)}, nil
	})
}
