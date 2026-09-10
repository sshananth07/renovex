package composition

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// supplieraccess -> awards.SupplierOutcomeSource (M8 §8A.1, D3).
//
// supplieraccess owns authentication, current-generation binding and CSRF, and
// declares this capability. awards owns outcome lookup and acknowledgement
// state. Being an adapter is what permits this file to name both.
//
// Only AUTHORITATIVE session facts cross: awards re-checks that the outcome
// belongs to this Supplier under this Invitation and returns a non-disclosing
// not-found otherwise (D3).

// awardOutcomeReader is the narrow slice of awards.Service this adapter needs.
type awardOutcomeReader interface {
	GetSupplierOutcome(
		ctx context.Context,
		companyID, supplierID, invitationID, outcomeID string,
	) (awards.AwardOutcome, error)
	AcknowledgeOutcome(
		ctx context.Context,
		companyID string,
		input awards.AcknowledgeOutcomeInput,
	) (awards.AwardOutcomeAcknowledgement, bool, error)
}

type SupplierOutcomeSourceAdapter struct {
	awards awardOutcomeReader
}

func NewSupplierOutcomeSourceAdapter(
	source awardOutcomeReader,
) *SupplierOutcomeSourceAdapter {
	return &SupplierOutcomeSourceAdapter{awards: source}
}

func (a *SupplierOutcomeSourceAdapter) GetSupplierOutcome(
	ctx context.Context,
	request supplieraccess.SupplierOutcomeRequest,
) (supplieraccess.SupplierOutcome, bool, error) {

	outcome, err := a.awards.GetSupplierOutcome(ctx,
		request.CompanyID, request.SupplierID, request.InvitationID,
		request.OutcomeID)
	if err != nil {
		// A foreign or absent outcome reports found = false, so the Supplier
		// route answers one non-disclosing 404 either way.
		if errors.Is(err, awards.ErrAwardOutcomeNotFound) {
			return supplieraccess.SupplierOutcome{}, false, nil
		}
		return supplieraccess.SupplierOutcome{}, false, err
	}
	return toSupplierAccessOutcome(outcome), true, nil
}

func (a *SupplierOutcomeSourceAdapter) AcknowledgeSupplierOutcome(
	ctx context.Context,
	request supplieraccess.SupplierOutcomeRequest,
) (supplieraccess.SupplierOutcomeAcknowledgementResult, error) {

	acknowledgement, created, err := a.awards.AcknowledgeOutcome(ctx,
		request.CompanyID, awards.AcknowledgeOutcomeInput{
			OutcomeID:    request.OutcomeID,
			SupplierID:   request.SupplierID,
			InvitationID: request.InvitationID,
			SessionID:    request.SessionID,
			// The identity that ACTUALLY acknowledged, which may be a
			// replacement recipient — the honest fact (§8J).
			RecipientIdentity: request.RecipientIdentity,
			OperationID:       request.OperationID,
			AcknowledgedAt:    request.OccurredAt,
		})
	if err != nil {
		if errors.Is(err, awards.ErrAwardOutcomeNotFound) {
			return supplieraccess.SupplierOutcomeAcknowledgementResult{},
				supplieraccess.ErrSupplierOutcomeNotFound
		}
		return supplieraccess.SupplierOutcomeAcknowledgementResult{}, err
	}

	return supplieraccess.SupplierOutcomeAcknowledgementResult{
		OutcomeID:      acknowledgement.AwardOutcomeID,
		AcknowledgedAt: acknowledgement.AcknowledgedAt,
		Created:        created,
	}, nil
}

// toSupplierAccessOutcome translates the frozen projection.
//
// It copies what awards already filtered and adds nothing: there is no
// competitor identity, price, total, ranking or unawarded reason in the source
// to carry across (§8H).
func toSupplierAccessOutcome(
	outcome awards.AwardOutcome,
) supplieraccess.SupplierOutcome {

	lines := make([]supplieraccess.SupplierOutcomeLine, 0,
		len(outcome.Projection.AwardedLines))
	for _, line := range outcome.Projection.AwardedLines {
		converted := supplieraccess.SupplierOutcomeLine{
			IssuedRFQLineID:        line.IssuedRFQLineID,
			MaterialName:           line.MaterialName,
			UnitPriceMinorUnits:    line.UnitPrice.Amount,
			LineSubtotalMinorUnits: line.LineSubtotal.Amount,
			LineTaxMinorUnits:      line.LineTaxAmount.Amount,
			Brand:                  line.Brand,
			SKU:                    line.SKU,
			LeadTime:               line.LeadTime,
		}
		// The exact decimal string, never a float: a Supplier's stated quantity
		// must survive rendering unchanged (ADR 0001).
		if line.Quantity.Unit != "" {
			converted.QuantityValue = line.Quantity.Value.String()
			converted.QuantityUnit = line.Quantity.Unit
		}
		lines = append(lines, converted)
	}

	return supplieraccess.SupplierOutcome{
		ID:     outcome.ID,
		Result: string(outcome.Result),
		Projection: supplieraccess.SupplierOutcomeProjection{
			Result:                   string(outcome.Projection.Result),
			RFQNumber:                outcome.Projection.RFQNumber,
			RFQTitle:                 outcome.Projection.RFQTitle,
			AwardedLines:             lines,
			LineSubtotalMinorUnits:   outcome.Projection.LineSubtotal.Amount,
			TaxTotalMinorUnits:       outcome.Projection.TaxTotal.Amount,
			ChargeTotalMinorUnits:    outcome.Projection.ChargeTotal.Amount,
			DeliveryChargeMinorUnits: outcome.Projection.DeliveryCharge.Amount,
			AwardTotalMinorUnits:     outcome.Projection.AwardTotal.Amount,
			Currency:                 outcome.Projection.AwardTotal.Currency,
			ContractorMessage:        outcome.Projection.ContractorMessage,
			NextSteps:                outcome.Projection.NextSteps,
		},
		CreatedAt: outcome.CreatedAt,
	}
}
