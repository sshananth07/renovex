package awards

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// Outcome routes (§8H).
//
// Two audiences, two authorization models. The contractor list runs through the
// authenticated Principal; the Supplier read runs through the Phase D session
// (D3) and resolves Company, Supplier and Invitation from it, never from
// request content.

type outcomeAwardedLineDTO struct {
	IssuedRFQLineID string        `json:"issuedRfqLineId"`
	MaterialName    string        `json:"materialName,omitempty"`
	QuantityValue   string        `json:"quantityValue,omitempty"`
	QuantityUnit    string        `json:"quantityUnit,omitempty"`
	UnitPrice       awardMoneyDTO `json:"unitPrice"`
	LineSubtotal    awardMoneyDTO `json:"lineSubtotal"`
	LineTaxAmount   awardMoneyDTO `json:"lineTaxAmount"`
	Brand           string        `json:"brand,omitempty"`
	SKU             string        `json:"sku,omitempty"`
	LeadTime        string        `json:"leadTime,omitempty"`
}

// outcomeProjectionDTO is the Supplier-safe wire shape. It carries NO field for
// competitor identity, competitor price, ranking, respondent count or
// unawarded reason — an absent field cannot be populated by a later change
// without this contract visibly widening (§8H).
type outcomeProjectionDTO struct {
	Result    string `json:"result"`
	RFQNumber string `json:"rfqNumber,omitempty"`
	RFQTitle  string `json:"rfqTitle,omitempty"`

	AwardedLines   []outcomeAwardedLineDTO `json:"awardedLines"`
	LineSubtotal   awardMoneyDTO           `json:"lineSubtotal"`
	TaxTotal       awardMoneyDTO           `json:"taxTotal"`
	ChargeTotal    awardMoneyDTO           `json:"chargeTotal"`
	DeliveryCharge awardMoneyDTO           `json:"deliveryCharge"`
	AwardTotal     awardMoneyDTO           `json:"awardTotal"`

	ContractorMessage string `json:"contractorMessage,omitempty"`
	NextSteps         string `json:"nextSteps,omitempty"`
}

type awardOutcomeDTO struct {
	ID                 string               `json:"id"`
	AwardRevisionID    string               `json:"awardRevisionId"`
	IssuedRFQVersionID string               `json:"issuedRfqVersionId"`
	SupplierID         string               `json:"supplierId"`
	InvitationID       string               `json:"invitationId"`
	Result             string               `json:"result"`
	Projection         outcomeProjectionDTO `json:"projection"`
	CreatedAt          time.Time            `json:"createdAt"`
}

// SupplierOutcomeView is the Supplier-facing shape this module EXPORTS for the
// `supplieraccess` module to render on its own public route (§8A.1:
// supplieraccess -> SupplierOutcomeSource (awards)).
//
// The Supplier route lives in supplieraccess because it belongs to the Phase D
// session boundary; awards supplies only the frozen, already-privacy-filtered
// content. It omits supplierId and invitationId: the Supplier already knows who
// they are, and echoing internal identifiers widens the surface for no benefit.
type SupplierOutcomeView struct {
	ID         string
	Result     string
	Projection OutcomeProjection
	CreatedAt  time.Time
}

// ToSupplierOutcomeView narrows a stored outcome for Supplier consumption.
func ToSupplierOutcomeView(outcome AwardOutcome) SupplierOutcomeView {
	return SupplierOutcomeView{
		ID:         outcome.ID,
		Result:     string(outcome.Result),
		Projection: outcome.Projection,
		CreatedAt:  outcome.CreatedAt,
	}
}

func toOutcomeProjectionDTO(projection OutcomeProjection) outcomeProjectionDTO {
	lines := make([]outcomeAwardedLineDTO, 0, len(projection.AwardedLines))
	for _, line := range projection.AwardedLines {
		dto := outcomeAwardedLineDTO{
			IssuedRFQLineID: line.IssuedRFQLineID,
			MaterialName:    line.MaterialName,
			UnitPrice:       toAwardMoneyDTO(line.UnitPrice),
			LineSubtotal:    toAwardMoneyDTO(line.LineSubtotal),
			LineTaxAmount:   toAwardMoneyDTO(line.LineTaxAmount),
			Brand:           line.Brand,
			SKU:             line.SKU,
			LeadTime:        line.LeadTime,
		}
		if line.Quantity.Unit != "" {
			dto.QuantityValue = line.Quantity.Value.String()
			dto.QuantityUnit = line.Quantity.Unit
		}
		lines = append(lines, dto)
	}
	return outcomeProjectionDTO{
		Result:            string(projection.Result),
		RFQNumber:         projection.RFQNumber,
		RFQTitle:          projection.RFQTitle,
		AwardedLines:      lines,
		LineSubtotal:      toAwardMoneyDTO(projection.LineSubtotal),
		TaxTotal:          toAwardMoneyDTO(projection.TaxTotal),
		ChargeTotal:       toAwardMoneyDTO(projection.ChargeTotal),
		DeliveryCharge:    toAwardMoneyDTO(projection.DeliveryCharge),
		AwardTotal:        toAwardMoneyDTO(projection.AwardTotal),
		ContractorMessage: projection.ContractorMessage,
		NextSteps:         projection.NextSteps,
	}
}

func toAwardOutcomeDTO(outcome AwardOutcome) awardOutcomeDTO {
	return awardOutcomeDTO{
		ID:                 outcome.ID,
		AwardRevisionID:    outcome.AwardRevisionID,
		IssuedRFQVersionID: outcome.IssuedRFQVersionID,
		SupplierID:         outcome.SupplierID,
		InvitationID:       outcome.InvitationID,
		Result:             string(outcome.Result),
		Projection:         toOutcomeProjectionDTO(outcome.Projection),
		CreatedAt:          outcome.CreatedAt,
	}
}

type outcomeListInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	RevisionID string `path:"revisionId"`
}

type outcomeListOutput struct {
	Body struct {
		Outcomes []awardOutcomeDTO `json:"outcomes"`
	}
}

func registerAwardOutcomeHandlers(api huma.API, svc *Service) {
	// Contractor read. Every company role may view an award record, including
	// the employee who prepared the draft.
	huma.Register(api, huma.Operation{
		OperationID: "awards-list-outcomes",
		Method:      http.MethodGet,
		Path: "/rfq-chains/{rfqChainId}/issued-versions/{versionId}" +
			"/award-revisions/{revisionId}/outcomes",
		Summary: "List supplier outcomes generated for an award revision",
	}, func(ctx context.Context, input *outcomeListInput) (*outcomeListOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		outcomes, err := svc.ListOutcomesForRevision(
			ctx, principal.CompanyID, input.RevisionID)
		if err != nil {
			return nil, MapAwardError(err)
		}
		output := &outcomeListOutput{}
		output.Body.Outcomes = make([]awardOutcomeDTO, 0, len(outcomes))
		for _, outcome := range outcomes {
			output.Body.Outcomes = append(
				output.Body.Outcomes, toAwardOutcomeDTO(outcome))
		}
		return output, nil
	})
}
