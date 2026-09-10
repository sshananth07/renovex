package awards

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// Award revision routes (§8F).
//
// Finalising is where the owner/admin boundary begins: it is irreversible and
// externally visible — a Supplier may be told they won. Reading award records
// stays open to employees, who prepared the draft in the first place.

type finaliseAwardHTTPInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	Body       struct {
		OperationID string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
		// ChangeReason is required from revision 2 onward. The service enforces
		// that rule; declaring it optional here lets revision 1 omit it.
		ChangeReason string `json:"changeReason,omitempty" maxLength:"500"`
	}
}

// correctAwardHTTPInput carries the COMPLETE decision set the correction
// publishes — baseline plus delta — because a correction produces a whole new
// immutable revision, not a patch against the previous one.
type correctAwardHTTPInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	Body       struct {
		OperationID string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
		// Required by the schema: a superseding record that cannot say why it
		// happened is not auditable later (§8G).
		ChangeReason string `json:"changeReason" minLength:"1" maxLength:"500"`

		Decisions []struct {
			IssuedRFQLineID string `json:"issuedRfqLineId" minLength:"1"`
			StableLineageID string `json:"stableLineageId" minLength:"1"`
			Decision        string `json:"decision" enum:"selected,unawarded"`
			OfferVersionID  string `json:"offerVersionId,omitempty"`
			OfferLineID     string `json:"offerLineId,omitempty"`
			UnawardedReason string `json:"unawardedReason,omitempty" enum:",no_acceptable_offer,purchase_deferred,scope_cancelled,retender_required,other"`
			UnawardedNote   string `json:"unawardedNote,omitempty" maxLength:"500"`
		} `json:"decisions"`
	}
}

type reconcileAwardHTTPInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	Body       struct {
		OperationID string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

// reconcileAwardOutput reports what reconciliation actually did, so an operator
// can tell "completed a stalled publication" from "released an abandoned one".
type reconcileAwardOutput struct {
	Body struct {
		AwardChainID     string `json:"awardChainId"`
		AwardRevisionID  string `json:"awardRevisionId,omitempty"`
		CompletedForward bool   `json:"completedForward"`
		ReleasedClaims   bool   `json:"releasedClaims"`
		AlreadyComplete  bool   `json:"alreadyComplete"`
	}
}

type awardRevisionPathInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	RevisionID string `path:"revisionId"`
}

type awardedLineDTO struct {
	IssuedRFQLineID string `json:"issuedRfqLineId"`
	StableLineageID string `json:"stableLineageId"`
	MaterialName    string `json:"materialName,omitempty"`
	QuantityValue   string `json:"quantityValue,omitempty"`
	QuantityUnit    string `json:"quantityUnit,omitempty"`

	SupplierID     string `json:"supplierId"`
	SupplierName   string `json:"supplierName,omitempty"`
	InvitationID   string `json:"invitationId"`
	OfferVersionID string `json:"offerVersionId"`
	OfferLineID    string `json:"offerLineId"`

	UnitPriceExcludingTax awardMoneyDTO `json:"unitPriceExcludingTax"`
	LineSubtotal          awardMoneyDTO `json:"lineSubtotal"`
	LineTaxAmount         awardMoneyDTO `json:"lineTaxAmount"`
	Brand                 string        `json:"brand,omitempty"`
	SKU                   string        `json:"sku,omitempty"`
	ProductDescription    string        `json:"productDescription,omitempty"`
	LeadTime              string        `json:"leadTime,omitempty"`
}

type unawardedLineDTO struct {
	IssuedRFQLineID string `json:"issuedRfqLineId"`
	StableLineageID string `json:"stableLineageId"`
	MaterialName    string `json:"materialName,omitempty"`
	Reason          string `json:"reason"`
	Note            string `json:"note,omitempty"`
}

type appliedChargeGroupDTO struct {
	GroupID       string        `json:"groupId"`
	Name          string        `json:"name,omitempty"`
	Triggered     bool          `json:"triggered"`
	GroupSubtotal awardMoneyDTO `json:"groupSubtotal"`
	Amount        awardMoneyDTO `json:"amount"`
}

type supplierSummaryDTO struct {
	SupplierID     string `json:"supplierId"`
	SupplierName   string `json:"supplierName,omitempty"`
	InvitationID   string `json:"invitationId"`
	OfferVersionID string `json:"offerVersionId"`
	OfferChainID   string `json:"offerChainId,omitempty"`

	AwardedLineIDs []string                `json:"awardedLineIds"`
	LineSubtotal   awardMoneyDTO           `json:"lineSubtotal"`
	TaxTotal       awardMoneyDTO           `json:"taxTotal"`
	ChargeGroups   []appliedChargeGroupDTO `json:"chargeGroups,omitempty"`
	ChargeTotal    awardMoneyDTO           `json:"chargeTotal"`
	DeliveryCharge awardMoneyDTO           `json:"deliveryCharge"`
	SupplierTotal  awardMoneyDTO           `json:"supplierTotal"`
}

type awardRevisionDTO struct {
	ID                      string `json:"id"`
	AwardChainID            string `json:"awardChainId"`
	RFQChainID              string `json:"rfqChainId"`
	IssuedRFQVersionID      string `json:"issuedRfqVersionId"`
	RevisionNumber          int    `json:"revisionNumber"`
	FinalisationOperationID string `json:"finalisationOperationId"`
	SelectionFingerprint    string `json:"selectionFingerprint"`
	ChangeReason            string `json:"changeReason,omitempty"`

	AwardedLines      []awardedLineDTO     `json:"awardedLines"`
	UnawardedLines    []unawardedLineDTO   `json:"unawardedLines"`
	SupplierSummaries []supplierSummaryDTO `json:"supplierSummaries"`
	GrandAwardTotal   awardMoneyDTO        `json:"grandAwardTotal"`

	SupersedesRevisionID *string   `json:"supersedesRevisionId,omitempty"`
	FinalisedByUserID    string    `json:"finalisedByUserId"`
	FinalisedAt          time.Time `json:"finalisedAt"`
}

type awardRevisionOutput struct {
	Status int
	Body   awardRevisionDTO
}

type awardRevisionListOutput struct {
	Body struct {
		Revisions []awardRevisionDTO `json:"revisions"`
	}
}

func toAwardRevisionDTO(revision AwardRevision) awardRevisionDTO {
	awarded := make([]awardedLineDTO, 0, len(revision.AwardedLines))
	for _, line := range revision.AwardedLines {
		dto := awardedLineDTO{
			IssuedRFQLineID:       line.IssuedRFQLineID,
			StableLineageID:       line.StableLineageID,
			MaterialName:          line.MaterialName,
			SupplierID:            line.SupplierID,
			SupplierName:          line.SupplierName,
			InvitationID:          line.InvitationID,
			OfferVersionID:        line.OfferVersionID,
			OfferLineID:           line.OfferLineID,
			UnitPriceExcludingTax: toAwardMoneyDTO(line.UnitPriceExcludingTax),
			LineSubtotal:          toAwardMoneyDTO(line.LineSubtotal),
			LineTaxAmount:         toAwardMoneyDTO(line.LineTaxAmount),
			Brand:                 line.Brand,
			SKU:                   line.SKU,
			ProductDescription:    line.ProductDescription,
			LeadTime:              line.LeadTime,
		}
		// The exact decimal string, never a float: a rendered quantity must
		// match the authoritative one (ADR 0001).
		if line.Quantity.Unit != "" {
			dto.QuantityValue = line.Quantity.Value.String()
			dto.QuantityUnit = line.Quantity.Unit
		}
		awarded = append(awarded, dto)
	}

	unawarded := make([]unawardedLineDTO, 0, len(revision.UnawardedLines))
	for _, line := range revision.UnawardedLines {
		unawarded = append(unawarded, unawardedLineDTO{
			IssuedRFQLineID: line.IssuedRFQLineID,
			StableLineageID: line.StableLineageID,
			MaterialName:    line.MaterialName,
			Reason:          string(line.Reason),
			Note:            line.Note,
		})
	}

	summaries := make([]supplierSummaryDTO, 0, len(revision.SupplierSummaries))
	for _, summary := range revision.SupplierSummaries {
		groups := make([]appliedChargeGroupDTO, 0, len(summary.ChargeGroups))
		for _, group := range summary.ChargeGroups {
			groups = append(groups, appliedChargeGroupDTO{
				GroupID:       group.GroupID,
				Name:          group.Name,
				Triggered:     group.Triggered,
				GroupSubtotal: toAwardMoneyDTO(group.GroupSubtotal),
				Amount:        toAwardMoneyDTO(group.Amount),
			})
		}
		summaries = append(summaries, supplierSummaryDTO{
			SupplierID:     summary.SupplierID,
			SupplierName:   summary.SupplierName,
			InvitationID:   summary.InvitationID,
			OfferVersionID: summary.OfferVersionID,
			OfferChainID:   summary.OfferChainID,
			AwardedLineIDs: summary.AwardedLineIDs,
			LineSubtotal:   toAwardMoneyDTO(summary.LineSubtotal),
			TaxTotal:       toAwardMoneyDTO(summary.TaxTotal),
			ChargeGroups:   groups,
			ChargeTotal:    toAwardMoneyDTO(summary.ChargeTotal),
			DeliveryCharge: toAwardMoneyDTO(summary.DeliveryCharge),
			SupplierTotal:  toAwardMoneyDTO(summary.SupplierTotal),
		})
	}

	return awardRevisionDTO{
		ID:                      revision.ID,
		AwardChainID:            revision.AwardChainID,
		RFQChainID:              revision.RFQChainID,
		IssuedRFQVersionID:      revision.IssuedRFQVersionID,
		RevisionNumber:          revision.RevisionNumber,
		FinalisationOperationID: revision.FinalisationOperationID,
		SelectionFingerprint:    revision.SelectionFingerprint,
		ChangeReason:            revision.ChangeReason,
		AwardedLines:            awarded,
		UnawardedLines:          unawarded,
		SupplierSummaries:       summaries,
		GrandAwardTotal:         toAwardMoneyDTO(revision.GrandAwardTotal),
		SupersedesRevisionID:    revision.SupersedesRevisionID,
		FinalisedByUserID:       revision.FinalisedByUserID,
		FinalisedAt:             revision.FinalisedAt,
	}
}

func registerAwardRevisionHandlers(api huma.API, svc *Service) {
	const base = "/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-revisions"

	// Finalise. Owner/admin ONLY: irreversible and externally visible.
	huma.Register(api, huma.Operation{
		OperationID:   "awards-finalise",
		Method:        http.MethodPost,
		Path:          base,
		Summary:       "Finalise the award, publishing an immutable revision",
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *finaliseAwardHTTPInput) (*awardRevisionOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}
		revision, err := svc.FinaliseAward(ctx, principal.CompanyID,
			principal.UserID, FinaliseAwardInput{
				IssuedRFQVersionID: input.VersionID,
				OperationID:        input.Body.OperationID,
				ChangeReason:       input.Body.ChangeReason,
				FinalisedAt:        time.Now().UTC(),
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardRevisionOutput{
			Status: http.StatusCreated, Body: toAwardRevisionDTO(revision),
		}, nil
	})

	// Correct. Owner/admin ONLY, and a superseding record must say why it
	// happened, so changeReason is required by the schema here rather than
	// only by the service (§8G).
	huma.Register(api, huma.Operation{
		OperationID:   "awards-correct",
		Method:        http.MethodPost,
		Path:          base + "/corrections",
		Summary:       "Publish a correcting award revision",
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *correctAwardHTTPInput) (*awardRevisionOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}

		selections := make([]AwardLineSelection, 0, len(input.Body.Decisions))
		for _, decision := range input.Body.Decisions {
			selections = append(selections, AwardLineSelection{
				IssuedRFQLineID: decision.IssuedRFQLineID,
				StableLineageID: decision.StableLineageID,
				OfferVersionID:  decision.OfferVersionID,
				OfferLineID:     decision.OfferLineID,
				Unawarded:       decision.Decision == string(AwardDecisionUnawarded),
				UnawardedReason: UnawardedReason(decision.UnawardedReason),
				UnawardedNote:   decision.UnawardedNote,
			})
		}

		revision, err := svc.CorrectAward(ctx, principal.CompanyID,
			principal.UserID, CorrectAwardInput{
				IssuedRFQVersionID: input.VersionID,
				OperationID:        input.Body.OperationID,
				ChangeReason:       input.Body.ChangeReason,
				Selections:         selections,
				CorrectedAt:        time.Now().UTC(),
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardRevisionOutput{
			Status: http.StatusCreated, Body: toAwardRevisionDTO(revision),
		}, nil
	})

	// Reconciliation. Owner/admin ONLY: it moves authoritative state.
	//
	// F10 owns this route rather than F5 because it repairs state across every
	// Phase F aggregate, so it is specified once where that whole surface is.
	huma.Register(api, huma.Operation{
		OperationID: "awards-reconcile",
		Method:      http.MethodPost,
		Path: "/rfq-chains/{rfqChainId}/issued-versions/{versionId}" +
			"/award-reconciliation",
		Summary: "Reconcile award state after an interrupted finalisation",
	}, func(ctx context.Context, input *reconcileAwardHTTPInput) (*reconcileAwardOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}
		result, err := svc.ReconcileAward(ctx, principal.CompanyID,
			principal.UserID, ReconcileAwardInput{
				IssuedRFQVersionID: input.VersionID,
				OperationID:        input.Body.OperationID,
				ReconciledAt:       time.Now().UTC(),
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		output := &reconcileAwardOutput{}
		output.Body.AwardChainID = result.AwardChainID
		output.Body.AwardRevisionID = result.AwardRevisionID
		output.Body.CompletedForward = result.CompletedForward
		output.Body.ReleasedClaims = result.ReleasedClaims
		output.Body.AlreadyComplete = result.AlreadyComplete
		return output, nil
	})

	// Award records are viewable by every member, including the employee who
	// prepared the draft.
	huma.Register(api, huma.Operation{
		OperationID: "awards-list-revisions",
		Method:      http.MethodGet,
		Path:        base,
		Summary:     "List immutable award revisions for an issued RFQ version",
	}, func(ctx context.Context, input *comparisonPathInput) (*awardRevisionListOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		revisions, err := svc.ListAwardRevisions(
			ctx, principal.CompanyID, input.VersionID)
		if err != nil {
			return nil, MapAwardError(err)
		}
		output := &awardRevisionListOutput{}
		output.Body.Revisions = make([]awardRevisionDTO, 0, len(revisions))
		for _, revision := range revisions {
			output.Body.Revisions = append(
				output.Body.Revisions, toAwardRevisionDTO(revision))
		}
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "awards-get-revision",
		Method:      http.MethodGet,
		Path:        base + "/{revisionId}",
		Summary:     "Read one immutable award revision",
	}, func(ctx context.Context, input *awardRevisionPathInput) (*awardRevisionOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		revision, err := svc.GetAwardRevision(
			ctx, principal.CompanyID, input.RevisionID)
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardRevisionOutput{
			Status: http.StatusOK, Body: toAwardRevisionDTO(revision),
		}, nil
	})
}
