package awards

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// Provisional award draft routes (§8C).
//
// All five are open to owner, admin AND employee. Decision A (§1A.1) settles
// this: employees may prepare editable drafts but may not perform externally
// visible or irreversible transitions. A provisional draft claims nothing and
// changes nothing a Supplier can observe, so employee mutation carries no
// external effect. The boundary starts at finalisation (F5).

type awardDraftPathInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
}

// Path fields are declared flat on every input rather than embedded: Huma
// resolves parameter tags on the input struct's own fields only, so an embedded
// path struct silently yields empty values.
type selectAwardLineInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	LineID     string `path:"lineId"`
	Body       struct {
		OfferVersionID   string `json:"offerVersionId" minLength:"1"`
		OfferLineID      string `json:"offerLineId" minLength:"1"`
		ExpectedRevision int64  `json:"expectedRevision" minimum:"1"`
	}
}

type unawardLineInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	LineID     string `path:"lineId"`
	Body       struct {
		// The bounded set is declared as an enum so an unrecognised reason is
		// refused at the boundary and can never reach a published revision.
		Reason string `json:"reason" enum:"no_acceptable_offer,purchase_deferred,scope_cancelled,retender_required,other"`
		Note   string `json:"note,omitempty" maxLength:"500"`

		ExpectedRevision int64 `json:"expectedRevision" minimum:"1"`
	}
}

type discardAwardDraftInput struct {
	RFQChainID       string `path:"rfqChainId"`
	VersionID        string `path:"versionId"`
	ExpectedRevision int64  `query:"expectedRevision" minimum:"1"`
}

type awardLineDecisionDTO struct {
	IssuedRFQLineID string `json:"issuedRfqLineId"`
	StableLineageID string `json:"stableLineageId"`
	Decision        string `json:"decision"`
	OfferVersionID  string `json:"offerVersionId,omitempty"`
	OfferLineID     string `json:"offerLineId,omitempty"`
	UnawardedReason string `json:"unawardedReason,omitempty"`
	UnawardedNote   string `json:"unawardedNote,omitempty"`
}

// awardDraftDTO exposes the revision because a client cannot participate in the
// expected-revision CAS protocol without it.
type awardDraftDTO struct {
	ID                 string                 `json:"id"`
	AwardChainID       string                 `json:"awardChainId"`
	IssuedRFQVersionID string                 `json:"issuedRfqVersionId"`
	Status             string                 `json:"status"`
	Revision           int64                  `json:"revision"`
	LineDecisions      []awardLineDecisionDTO `json:"lineDecisions"`
	CreatedByUserID    string                 `json:"createdByUserId"`
	CreatedAt          time.Time              `json:"createdAt"`
	UpdatedAt          time.Time              `json:"updatedAt"`
}

type awardDraftOutput struct {
	Status int
	Body   awardDraftDTO
}

type awardDraftNoContentOutput struct {
	Status int
}

func toAwardDraftDTO(draft AwardDraft) awardDraftDTO {
	decisions := make([]awardLineDecisionDTO, 0, len(draft.LineDecisions))
	for _, decision := range draft.LineDecisions {
		decisions = append(decisions, awardLineDecisionDTO{
			IssuedRFQLineID: decision.IssuedRFQLineID,
			StableLineageID: decision.StableLineageID,
			Decision:        string(decision.Decision),
			OfferVersionID:  decision.OfferVersionID,
			OfferLineID:     decision.OfferLineID,
			UnawardedReason: string(decision.UnawardedReason),
			UnawardedNote:   decision.UnawardedNote,
		})
	}
	return awardDraftDTO{
		ID:                 draft.ID,
		AwardChainID:       draft.AwardChainID,
		IssuedRFQVersionID: draft.IssuedRFQVersionID,
		Status:             string(draft.Status),
		Revision:           draft.Revision,
		LineDecisions:      decisions,
		CreatedByUserID:    draft.CreatedByUserID,
		CreatedAt:          draft.CreatedAt,
		UpdatedAt:          draft.UpdatedAt,
	}
}

// registerAwardDraftHandlers mounts the five draft routes.
func registerAwardDraftHandlers(api huma.API, svc *Service) {
	const base = "/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-draft"

	huma.Register(api, huma.Operation{
		OperationID:   "awards-create-draft",
		Method:        http.MethodPost,
		Path:          base,
		Summary:       "Open a provisional award draft",
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *awardDraftPathInput) (*awardDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		draft, err := svc.CreateAwardDraft(
			ctx, principal.CompanyID, principal.UserID, input.VersionID)
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDraftOutput{
			Status: http.StatusCreated, Body: toAwardDraftDTO(draft),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "awards-get-draft",
		Method:      http.MethodGet,
		Path:        base,
		Summary:     "Read the open provisional award draft",
	}, func(ctx context.Context, input *awardDraftPathInput) (*awardDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		draft, err := svc.GetAwardDraft(ctx, principal.CompanyID, input.VersionID)
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDraftOutput{Status: http.StatusOK, Body: toAwardDraftDTO(draft)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "awards-select-draft-line",
		Method:      http.MethodPut,
		Path:        base + "/lines/{lineId}/selection",
		Summary:     "Provisionally select a supplier offer line for an RFQ line",
	}, func(ctx context.Context, input *selectAwardLineInput) (*awardDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		draft, err := svc.SelectAwardLine(ctx, principal.CompanyID, principal.UserID,
			SelectAwardLineInput{
				IssuedRFQVersionID: input.VersionID,
				IssuedRFQLineID:    input.LineID,
				OfferVersionID:     input.Body.OfferVersionID,
				OfferLineID:        input.Body.OfferLineID,
				ExpectedRevision:   input.Body.ExpectedRevision,
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDraftOutput{Status: http.StatusOK, Body: toAwardDraftDTO(draft)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "awards-unaward-draft-line",
		Method:      http.MethodPut,
		Path:        base + "/lines/{lineId}/unawarded",
		Summary:     "Record a deliberate decision not to award an RFQ line",
	}, func(ctx context.Context, input *unawardLineInput) (*awardDraftOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		draft, err := svc.UnawardLine(ctx, principal.CompanyID, principal.UserID,
			UnawardLineInput{
				IssuedRFQVersionID: input.VersionID,
				IssuedRFQLineID:    input.LineID,
				Reason:             UnawardedReason(input.Body.Reason),
				Note:               input.Body.Note,
				ExpectedRevision:   input.Body.ExpectedRevision,
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDraftOutput{Status: http.StatusOK, Body: toAwardDraftDTO(draft)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "awards-discard-draft",
		Method:        http.MethodDelete,
		Path:          base,
		Summary:       "Discard the open provisional award draft",
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *discardAwardDraftInput) (*awardDraftNoContentOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee)
		if err != nil {
			return nil, err
		}
		if err := svc.DiscardAwardDraft(ctx, principal.CompanyID, principal.UserID,
			input.VersionID, input.ExpectedRevision); err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDraftNoContentOutput{Status: http.StatusNoContent}, nil
	})
}
