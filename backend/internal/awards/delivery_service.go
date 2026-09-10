package awards

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Notification send, retry and obsolescence (§8I).
//
// Ordering mirrors Phase C exactly:
//
//	1  insert delivery record, status pending, with operation ID
//	2  send synchronously
//	3  update status sent | failed with a bounded failure code
//
// A crash after step 1 leaves a recoverable `pending` record, never a sent
// email with no record of it.

// AwardDeliveryRepository is the narrow delivery surface.
type AwardDeliveryRepository interface {
	EnsureDeliveryIntent(
		ctx context.Context,
		candidate AwardOutcomeDelivery,
	) (AwardOutcomeDelivery, bool, error)
	FindDelivery(
		ctx context.Context,
		companyID, deliveryID string,
	) (AwardOutcomeDelivery, bool, error)
	FindDeliveryByOperation(
		ctx context.Context,
		companyID, deliveryOperationID string,
	) (AwardOutcomeDelivery, bool, error)
	MarkDeliverySent(
		ctx context.Context,
		companyID, deliveryID string,
		sentAt time.Time,
	) error
	MarkDeliveryFailed(
		ctx context.Context,
		companyID, deliveryID string,
		code DeliveryFailureCode,
	) error
	ObsoletePendingDeliveries(
		ctx context.Context,
		companyID, awardRevisionID string,
	) ([]AwardOutcomeDelivery, error)
	ListDeliveries(
		ctx context.Context,
		companyID, awardRevisionID string,
	) ([]AwardOutcomeDelivery, error)
}

func WithAwardDeliveryRepository(
	repository AwardDeliveryRepository,
) ServiceOption {
	return func(service *Service) { service.deliveries = repository }
}

type SendOutcomeNotificationInput struct {
	OutcomeID           string
	DeliveryOperationID string
	RecipientIdentity   string
	AccessGeneration    int64
	CompanyName         string
	SupplierName        string
	RFQNumber           string
	RFQTitle            string
	SentAt              time.Time
}

// SendOutcomeNotification tells one Supplier about their outcome.
//
// It is invoked only by an explicit contractor action, never automatically on
// publication: an award is a decision, and telling Suppliers is a separate,
// deliberate act.
//
// This is the single place that assembles a notification: load the immutable
// outcome, derive the invitation's existing Supplier Access link (no
// rotation, no new invitation, no delivery-attempt record — see
// InvitationLinkSource), build the outcome URL from it, then hand both the
// notification and the outcome's own immutable projection to the mailer.
// RetryOutcomeNotification calls this same method, so a retry/resend
// constructs its link exactly the same way a first send does.
func (service *Service) SendOutcomeNotification(
	ctx context.Context,
	companyID, actorUserID string,
	input SendOutcomeNotificationInput,
) (AwardOutcomeDelivery, error) {
	if service.outcomes == nil || service.deliveries == nil ||
		service.mailer == nil || service.invitationLink == nil {
		return AwardOutcomeDelivery{}, ErrAwardsNotConfigured
	}

	outcome, found, err := service.outcomes.FindOutcome(
		ctx, companyID, input.OutcomeID)
	if err != nil {
		return AwardOutcomeDelivery{}, err
	}
	if !found {
		return AwardOutcomeDelivery{}, ErrAwardOutcomeNotFound
	}

	link, err := service.invitationLink.DeriveInvitationLink(
		ctx, companyID, outcome.InvitationID)
	if err != nil {
		return AwardOutcomeDelivery{}, err
	}
	outcomeURL := fmt.Sprintf("%s&returnTo=%s", link.URL,
		url.QueryEscape(fmt.Sprintf(
			"/supplier-access/invitations/%s/outcomes/%s",
			outcome.InvitationID, outcome.ID)))

	notification := AwardOutcomeNotification{
		CompanyID:         companyID,
		CompanyName:       input.CompanyName,
		SupplierID:        outcome.SupplierID,
		SupplierName:      input.SupplierName,
		InvitationID:      outcome.InvitationID,
		RecipientIdentity: input.RecipientIdentity,
		RFQNumber:         input.RFQNumber,
		RFQTitle:          input.RFQTitle,
		OutcomeID:         outcome.ID,
		Result:            string(outcome.Result),
		ContractorMessage: outcome.Projection.ContractorMessage,
		OutcomeURL:        outcomeURL,
	}

	// Step 1: PERSIST INTENT. The record exists before any mail is attempted.
	intent := DeliveryFromNotification(
		notification, input.DeliveryOperationID, input.AccessGeneration)
	intent.AwardRevisionID = outcome.AwardRevisionID
	sentAt := input.SentAt
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	intent.CreatedAt = sentAt

	delivery, created, err := service.deliveries.EnsureDeliveryIntent(ctx, intent)
	if err != nil {
		return AwardOutcomeDelivery{}, err
	}
	// A same-operation retry resolves the existing record and does NOT re-send:
	// the Supplier must not be told twice because a response was lost.
	if !created {
		if delivery.Status == DeliverySent && service.audit != nil {
			_ = service.audit.RecordAwardOutcomeNotified(ctx, companyID,
				actorUserID, outcome.AwardRevisionID, outcome.ID,
				outcome.SupplierID, delivery.ID, input.DeliveryOperationID, sentAt)
		}
		return delivery, nil
	}

	// Step 2: send synchronously.
	sendErr := service.mailer.SendAwardOutcomeNotification(
		ctx, notification, outcome.Projection)

	// Step 3: resolve the record with a bounded outcome. Failure never rolls
	// back the award or the outcome — the decision stands, and only the
	// attempt to communicate it did not.
	if sendErr != nil {
		_ = service.deliveries.MarkDeliveryFailed(ctx, companyID, delivery.ID,
			classifyDeliveryFailure(sendErr))
		resolved, _, findErr := service.deliveries.FindDelivery(
			ctx, companyID, delivery.ID)
		if findErr != nil {
			return AwardOutcomeDelivery{}, findErr
		}
		return resolved, nil
	}

	if err := service.deliveries.MarkDeliverySent(
		ctx, companyID, delivery.ID, sentAt); err != nil {
		return AwardOutcomeDelivery{}, err
	}
	if service.audit != nil {
		_ = service.audit.RecordAwardOutcomeNotified(ctx, companyID,
			actorUserID, outcome.AwardRevisionID, outcome.ID,
			outcome.SupplierID, delivery.ID, input.DeliveryOperationID, sentAt)
	}

	resolved, _, err := service.deliveries.FindDelivery(
		ctx, companyID, delivery.ID)
	if err != nil {
		return AwardOutcomeDelivery{}, err
	}
	return resolved, nil
}

// classifyDeliveryFailure maps a transport error to a BOUNDED code.
//
// The raw error is deliberately discarded rather than stored: it can carry
// hostnames, ports and credentials that have no business in a persisted record
// or an API response.
func classifyDeliveryFailure(err error) DeliveryFailureCode {
	if err == nil {
		return ""
	}
	// Without a richer mailer contract, an unclassifiable transport failure is
	// reported as unavailable — the retryable reading, which is the safer
	// default for a contractor deciding whether to try again.
	return DeliveryFailureTransportUnavailable
}

// ObsoleteSupersededDeliveries marks a superseded revision's PENDING records
// obsolete after a correction publishes.
//
// `sent` and `failed` records are never rewritten: a Supplier really was told,
// and history must survive being superseded.
func (service *Service) ObsoleteSupersededDeliveries(
	ctx context.Context,
	companyID, actorUserID, supersededRevisionID string,
) error {
	if service.deliveries == nil {
		return ErrAwardsNotConfigured
	}
	affected, err := service.deliveries.ObsoletePendingDeliveries(
		ctx, companyID, supersededRevisionID)
	if err != nil {
		return err
	}
	if service.audit == nil {
		return nil
	}
	now := time.Now().UTC()
	for _, delivery := range affected {
		_ = service.audit.RecordAwardOutcomeNotificationObsoleted(ctx,
			companyID, actorUserID, delivery.AwardRevisionID,
			delivery.AwardOutcomeID, delivery.SupplierID, delivery.ID, now)
	}
	return nil
}

// ListDeliveriesForRevision returns every attempt made for one revision.
func (service *Service) ListDeliveriesForRevision(
	ctx context.Context,
	companyID, revisionID string,
) ([]AwardOutcomeDelivery, error) {
	if service.deliveries == nil || service.revisions == nil {
		return nil, ErrAwardsNotConfigured
	}
	// Resolve the revision within the tenant first, so a foreign revision ID
	// yields not-found rather than an empty list implying it exists.
	if _, found, err := service.revisions.FindRevision(
		ctx, companyID, revisionID); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrAwardRevisionNotFound
	}
	return service.deliveries.ListDeliveries(ctx, companyID, revisionID)
}

// RetryOutcomeNotification is the EXPLICIT, audited retry after a failure.
//
// It creates a NEW delivery record for the same outcome rather than rewriting
// the failed one, so the history shows both attempts.
func (service *Service) RetryOutcomeNotification(
	ctx context.Context,
	companyID, actorUserID, deliveryID string,
	input SendOutcomeNotificationInput,
) (AwardOutcomeDelivery, error) {
	if service.deliveries == nil {
		return AwardOutcomeDelivery{}, ErrAwardsNotConfigured
	}

	previous, found, err := service.deliveries.FindDelivery(
		ctx, companyID, deliveryID)
	if err != nil {
		return AwardOutcomeDelivery{}, err
	}
	if !found {
		return AwardOutcomeDelivery{}, ErrAwardDeliveryNotFound
	}
	// Only a failed attempt is worth retrying: retrying a sent one would tell
	// the Supplier twice, and retrying a pending one would race its own send.
	if previous.Status != DeliveryFailed {
		return AwardOutcomeDelivery{}, ErrAwardDeliveryConflict
	}

	input.OutcomeID = previous.AwardOutcomeID
	delivery, err := service.SendOutcomeNotification(
		ctx, companyID, actorUserID, input)
	if err != nil {
		return AwardOutcomeDelivery{}, err
	}
	if service.audit != nil {
		_ = service.audit.RecordAwardOutcomeNotificationRetried(ctx, companyID,
			actorUserID, previous.AwardRevisionID, previous.AwardOutcomeID,
			previous.SupplierID, delivery.ID, input.DeliveryOperationID,
			time.Now().UTC())
	}
	return delivery, nil
}
