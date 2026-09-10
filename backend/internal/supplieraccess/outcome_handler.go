package supplieraccess

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

// Supplier-facing award outcome routes (M8 §8H, §8J, D3).
//
// These sit behind the SAME Phase D session this module already enforces:
// session validation, current-generation invitation binding, and CSRF on the
// mutation. Mounting them in awards would bypass that boundary entirely, which
// is why §8A.1 places them here.
//
// The outcome ID is the only value taken from the request. Company, Supplier,
// Invitation, Session and recipient all come from the validated session, so a
// Supplier cannot name another Supplier's outcome and have it honoured.

// AcknowledgementMeaning is stated on every receipt so a Supplier cannot read
// acknowledgement as acceptance of a Purchase Order (§8J).
//
// It is declared here rather than imported from awards: supplieraccess must not
// import a domain module (ADR 0002), and this module owns what it says on its
// own wire. A test pins the two wordings to the same meaning.
const AcknowledgementMeaning = "Acknowledgement confirms receipt only. " +
	"It is not acceptance of a Purchase Order or contractual commitment."

// mapSupplierOutcomeError keeps the Supplier-facing failure surface narrow.
//
// A foreign outcome and a non-existent one collapse to ONE 404, so a Supplier
// cannot probe for other Suppliers' outcome identifiers.
func mapSupplierOutcomeError(err error) error {
	switch {
	case errors.Is(err, ErrSupplierOutcomeNotFound):
		return huma.Error404NotFound("outcome not found")
	case errors.Is(err, ErrSupplierOutcomesUnavailable):
		return huma.Error503ServiceUnavailable("outcomes are unavailable")
	default:
		return huma.Error503ServiceUnavailable("outcome service unavailable")
	}
}

type supplierOutcomeLineDTO struct {
	IssuedRFQLineID string `json:"issuedRfqLineId"`
	MaterialName    string `json:"materialName,omitempty"`
	QuantityValue   string `json:"quantityValue,omitempty"`
	QuantityUnit    string `json:"quantityUnit,omitempty"`
	UnitPrice       int64  `json:"unitPriceMinorUnits"`
	LineSubtotal    int64  `json:"lineSubtotalMinorUnits"`
	LineTax         int64  `json:"lineTaxMinorUnits"`
	Brand           string `json:"brand,omitempty"`
	SKU             string `json:"sku,omitempty"`
	LeadTime        string `json:"leadTime,omitempty"`
}

// supplierOutcomeDTO declares NO field for a competitor identity, a competitor
// price, a ranking, a respondent count or an unawarded reason. An absent field
// cannot be populated by a later change without this contract visibly widening
// (§8H).
type supplierOutcomeDTO struct {
	ID        string `json:"id"`
	Result    string `json:"result"`
	RFQNumber string `json:"rfqNumber,omitempty"`
	RFQTitle  string `json:"rfqTitle,omitempty"`

	AwardedLines []supplierOutcomeLineDTO `json:"awardedLines"`

	LineSubtotal   int64  `json:"lineSubtotalMinorUnits"`
	TaxTotal       int64  `json:"taxTotalMinorUnits"`
	ChargeTotal    int64  `json:"chargeTotalMinorUnits"`
	DeliveryCharge int64  `json:"deliveryChargeMinorUnits"`
	AwardTotal     int64  `json:"awardTotalMinorUnits"`
	Currency       string `json:"currency,omitempty"`

	ContractorMessage string    `json:"contractorMessage,omitempty"`
	NextSteps         string    `json:"nextSteps,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

type supplierOutcomeOutput struct {
	CacheControl   string `header:"Cache-Control"`
	Pragma         string `header:"Pragma"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           supplierOutcomeDTO
}

type supplierAcknowledgementOutput struct {
	Status         int
	CacheControl   string `header:"Cache-Control"`
	Pragma         string `header:"Pragma"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           struct {
		OutcomeID      string    `json:"outcomeId"`
		AcknowledgedAt time.Time `json:"acknowledgedAt"`
		// Stated on every receipt so a Supplier cannot read acknowledgement as
		// acceptance of a Purchase Order (§8J).
		Meaning string `json:"meaning"`
	}
}

func toSupplierOutcomeDTO(outcome SupplierOutcome) supplierOutcomeDTO {
	lines := make([]supplierOutcomeLineDTO, 0,
		len(outcome.Projection.AwardedLines))
	for _, line := range outcome.Projection.AwardedLines {
		lines = append(lines, supplierOutcomeLineDTO{
			IssuedRFQLineID: line.IssuedRFQLineID,
			MaterialName:    line.MaterialName,
			QuantityValue:   line.QuantityValue,
			QuantityUnit:    line.QuantityUnit,
			UnitPrice:       line.UnitPriceMinorUnits,
			LineSubtotal:    line.LineSubtotalMinorUnits,
			LineTax:         line.LineTaxMinorUnits,
			Brand:           line.Brand,
			SKU:             line.SKU,
			LeadTime:        line.LeadTime,
		})
	}
	return supplierOutcomeDTO{
		ID:                outcome.ID,
		Result:            outcome.Result,
		RFQNumber:         outcome.Projection.RFQNumber,
		RFQTitle:          outcome.Projection.RFQTitle,
		AwardedLines:      lines,
		LineSubtotal:      outcome.Projection.LineSubtotalMinorUnits,
		TaxTotal:          outcome.Projection.TaxTotalMinorUnits,
		ChargeTotal:       outcome.Projection.ChargeTotalMinorUnits,
		DeliveryCharge:    outcome.Projection.DeliveryChargeMinorUnits,
		AwardTotal:        outcome.Projection.AwardTotalMinorUnits,
		Currency:          outcome.Projection.Currency,
		ContractorMessage: outcome.Projection.ContractorMessage,
		NextSteps:         outcome.Projection.NextSteps,
		CreatedAt:         outcome.CreatedAt,
	}
}

// registerOutcomeHandlers mounts the two Supplier-facing award routes.
func registerOutcomeHandlers(api huma.API, service *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "supplier-access-get-outcome",
		Method:      http.MethodGet,
		Path:        "/supplier-access/outcomes/{outcomeId}",
		Summary:     "Read an award outcome as the invited Supplier",
		Security:    platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionCookie string `cookie:"supplier_session"`
		InvitationID  string `query:"invitationId"`
		OutcomeID     string `path:"outcomeId"`
	}) (*supplierOutcomeOutput, error) {

		// Read authorization: session validity, immutable session identity,
		// the exact invitation binding, and a current active invitation.
		access, err := service.AuthorizeInvitationAccess(ctx,
			AuthorizeInvitationAccessInput{
				SessionToken: input.SessionCookie,
				InvitationID: input.InvitationID,
				AccessedAt:   time.Now().UTC(),
			})
		if err != nil {
			return nil, mapSupplierAccessPublicError(err)
		}

		outcome, err := service.GetOutcomeForSession(
			ctx, access, input.OutcomeID)
		if err != nil {
			return nil, mapSupplierOutcomeError(err)
		}

		return &supplierOutcomeOutput{
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
			Body:           toSupplierOutcomeDTO(outcome),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "supplier-access-acknowledge-outcome",
		Method:        http.MethodPost,
		Path:          "/supplier-access/outcomes/{outcomeId}/acknowledgements",
		Summary:       "Acknowledge receipt of an award outcome",
		DefaultStatus: http.StatusCreated,
		Security:      platformhttp.SupplierMutationSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionCookie string `cookie:"supplier_session"`
		CSRFCookie    string `cookie:"supplier_csrf"`
		CSRFHeader    string `header:"X-CSRF-Token"`
		OutcomeID     string `path:"outcomeId"`
		Body          struct {
			InvitationID string `json:"invitationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
			OperationID  string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
		}
	}) (*supplierAcknowledgementOutput, error) {

		// Mutation authorization ADDS the CSRF proof. Read and mutation stay
		// separate all the way through, so this route can never be reached
		// with only the weaker read credentials.
		access, err := service.AuthorizeInvitationMutation(ctx,
			AuthorizeInvitationMutationInput{
				AuthorizeInvitationAccessInput: AuthorizeInvitationAccessInput{
					SessionToken: input.SessionCookie,
					InvitationID: input.Body.InvitationID,
					AccessedAt:   time.Now().UTC(),
				},
				CSRFCookie: input.CSRFCookie,
				CSRFHeader: input.CSRFHeader,
			})
		if err != nil {
			return nil, mapSupplierAccessPublicError(err)
		}

		result, err := service.AcknowledgeOutcomeForSession(
			ctx, access, input.OutcomeID, input.Body.OperationID)
		if err != nil {
			return nil, mapSupplierOutcomeError(err)
		}

		output := &supplierAcknowledgementOutput{
			// 201 only when THIS request recorded the receipt; a repeat
			// returns the existing one with 200, because it created nothing.
			Status:         http.StatusCreated,
			CacheControl:   supplierNoStore,
			Pragma:         "no-cache",
			ReferrerPolicy: "no-referrer",
		}
		if !result.Created {
			output.Status = http.StatusOK
		}
		output.Body.OutcomeID = result.OutcomeID
		output.Body.AcknowledgedAt = result.AcknowledgedAt
		output.Body.Meaning = AcknowledgementMeaning
		return output, nil
	})
}
