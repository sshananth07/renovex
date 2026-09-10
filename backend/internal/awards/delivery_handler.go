package awards

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// Notification routes (§8I). Owner and admin only: sending is externally
// visible and cannot be unsent.
//
// All three return 202: the record is persisted and the transport has been
// asked, but SMTP acceptance is not proof of delivery, so a 200 claiming
// success would overstate what is known.

type sendNotificationHTTPInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	RevisionID string `path:"revisionId"`
	OutcomeID  string `path:"outcomeId"`
	Body       struct {
		OperationID       string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
		RecipientIdentity string `json:"recipientIdentity" minLength:"1"`
		AccessGeneration  int64  `json:"accessGeneration" minimum:"1"`
		CompanyName       string `json:"companyName,omitempty"`
		SupplierName      string `json:"supplierName,omitempty"`
		RFQNumber         string `json:"rfqNumber,omitempty"`
		RFQTitle          string `json:"rfqTitle,omitempty"`
	}
}

type retryNotificationHTTPInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	DeliveryID string `path:"deliveryId"`
	Body       struct {
		OperationID       string `json:"operationId" minLength:"1" maxLength:"128" pattern:"^[!-~]+$"`
		RecipientIdentity string `json:"recipientIdentity" minLength:"1"`
		AccessGeneration  int64  `json:"accessGeneration" minimum:"1"`
		CompanyName       string `json:"companyName,omitempty"`
		SupplierName      string `json:"supplierName,omitempty"`
		RFQNumber         string `json:"rfqNumber,omitempty"`
		RFQTitle          string `json:"rfqTitle,omitempty"`
	}
}

type deliveryListInput struct {
	RFQChainID string `path:"rfqChainId"`
	VersionID  string `path:"versionId"`
	RevisionID string `path:"revisionId"`
}

// awardDeliveryDTO carries the bounded failure code, never raw transport text:
// that can hold hostnames, ports and credentials.
type awardDeliveryDTO struct {
	ID                  string     `json:"id"`
	AwardOutcomeID      string     `json:"awardOutcomeId"`
	AwardRevisionID     string     `json:"awardRevisionId"`
	SupplierID          string     `json:"supplierId"`
	RecipientIdentity   string     `json:"recipientIdentity"`
	AccessGeneration    int64      `json:"accessGeneration"`
	DeliveryOperationID string     `json:"deliveryOperationId"`
	Channel             string     `json:"channel"`
	Status              string     `json:"status"`
	FailureCode         string     `json:"failureCode,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	SentAt              *time.Time `json:"sentAt,omitempty"`
}

type awardDeliveryOutput struct {
	Status int
	Body   awardDeliveryDTO
}

type awardDeliveryListOutput struct {
	Body struct {
		Deliveries []awardDeliveryDTO `json:"deliveries"`
	}
}

func toAwardDeliveryDTO(delivery AwardOutcomeDelivery) awardDeliveryDTO {
	return awardDeliveryDTO{
		ID:                  delivery.ID,
		AwardOutcomeID:      delivery.AwardOutcomeID,
		AwardRevisionID:     delivery.AwardRevisionID,
		SupplierID:          delivery.SupplierID,
		RecipientIdentity:   delivery.RecipientIdentity,
		AccessGeneration:    delivery.AccessGeneration,
		DeliveryOperationID: delivery.DeliveryOperationID,
		Channel:             delivery.Channel,
		Status:              string(delivery.Status),
		FailureCode:         string(delivery.FailureCode),
		CreatedAt:           delivery.CreatedAt,
		SentAt:              delivery.SentAt,
	}
}

func registerAwardDeliveryHandlers(api huma.API, svc *Service) {
	const base = "/rfq-chains/{rfqChainId}/issued-versions/{versionId}" +
		"/award-revisions/{revisionId}"

	huma.Register(api, huma.Operation{
		OperationID: "awards-send-notification",
		Method:      http.MethodPost,
		Path:        base + "/outcomes/{outcomeId}/notifications",
		Summary:     "Send an award outcome notification to a supplier",
		// 202, not 200: the transport has been asked, which is not the same as
		// the Supplier having received it.
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, input *sendNotificationHTTPInput) (*awardDeliveryOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}
		delivery, err := svc.SendOutcomeNotification(ctx, principal.CompanyID,
			principal.UserID, SendOutcomeNotificationInput{
				OutcomeID:           input.OutcomeID,
				DeliveryOperationID: input.Body.OperationID,
				RecipientIdentity:   input.Body.RecipientIdentity,
				AccessGeneration:    input.Body.AccessGeneration,
				CompanyName:         input.Body.CompanyName,
				SupplierName:        input.Body.SupplierName,
				RFQNumber:           input.Body.RFQNumber,
				RFQTitle:            input.Body.RFQTitle,
				SentAt:              time.Now().UTC(),
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDeliveryOutput{
			Status: http.StatusAccepted, Body: toAwardDeliveryDTO(delivery),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "awards-retry-notification",
		Method:        http.MethodPost,
		Path:          "/rfq-chains/{rfqChainId}/issued-versions/{versionId}/notifications/{deliveryId}/retry",
		Summary:       "Retry a failed award outcome notification",
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, input *retryNotificationHTTPInput) (*awardDeliveryOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}
		delivery, err := svc.RetryOutcomeNotification(ctx, principal.CompanyID,
			principal.UserID, input.DeliveryID, SendOutcomeNotificationInput{
				DeliveryOperationID: input.Body.OperationID,
				RecipientIdentity:   input.Body.RecipientIdentity,
				AccessGeneration:    input.Body.AccessGeneration,
				CompanyName:         input.Body.CompanyName,
				SupplierName:        input.Body.SupplierName,
				RFQNumber:           input.Body.RFQNumber,
				RFQTitle:            input.Body.RFQTitle,
				SentAt:              time.Now().UTC(),
			})
		if err != nil {
			return nil, MapAwardError(err)
		}
		return &awardDeliveryOutput{
			Status: http.StatusAccepted, Body: toAwardDeliveryDTO(delivery),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "awards-list-notifications",
		Method:      http.MethodGet,
		Path:        base + "/notifications",
		Summary:     "List award outcome notification attempts",
	}, func(ctx context.Context, input *deliveryListInput) (*awardDeliveryListOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx,
			identity.RoleOwner, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}
		deliveries, err := svc.ListDeliveriesForRevision(
			ctx, principal.CompanyID, input.RevisionID)
		if err != nil {
			return nil, MapAwardError(err)
		}
		output := &awardDeliveryListOutput{}
		output.Body.Deliveries = make([]awardDeliveryDTO, 0, len(deliveries))
		for _, delivery := range deliveries {
			output.Body.Deliveries = append(
				output.Body.Deliveries, toAwardDeliveryDTO(delivery))
		}
		return output, nil
	})
}
