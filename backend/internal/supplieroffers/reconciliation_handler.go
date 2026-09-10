package supplieroffers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

type reconciliationOutput struct {
	Status int
	Body   struct {
		Kind           SupplierOfferReconciliationKind `json:"kind"`
		OfferVersionID string                          `json:"offerVersionId"`
		WithdrawalID   string                          `json:"withdrawalId,omitempty"`
	}
}

// RegisterReconciliationHandlers mounts only the privileged recovery route.
// The caller supplies the authenticated API group; Supplier session routes stay
// in RegisterHandlers and can never reach this owner/admin operation.
func RegisterReconciliationHandlers(api huma.API, service *Service) {
	huma.Register(api, huma.Operation{OperationID: "supplier-offers-reconcile",
		Method: http.MethodPost, Path: "/supplier-offer-reconciliations",
		Summary: "Complete one interrupted Supplier Offer operation"},
		func(ctx context.Context, input *struct {
			Body struct {
				InvitationID       string `json:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
				IssuedRFQVersionID string `json:"issuedRfqVersionId" maxLength:"128" pattern:"^[!-~]+$"`
				OperationID        string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
			}
		}) (*reconciliationOutput, error) {
			principal, err := identity.AuthorizedPrincipal(ctx,
				identity.RoleOwner, identity.RoleAdmin)
			if err != nil {
				return nil, err
			}
			result, err := service.ReconcileSupplierOffer(ctx,
				ReconcileSupplierOfferInput{CompanyID: principal.CompanyID,
					ActorUserID: principal.UserID, InvitationID: input.Body.InvitationID,
					IssuedRFQVersionID: input.Body.IssuedRFQVersionID,
					OperationID:        input.Body.OperationID})
			if err != nil {
				return nil, MapSupplierOfferError(err)
			}
			output := &reconciliationOutput{Status: http.StatusOK}
			output.Body.Kind = result.Kind
			output.Body.OfferVersionID = result.OfferVersionID
			output.Body.WithdrawalID = result.WithdrawalID
			return output, nil
		})
}
