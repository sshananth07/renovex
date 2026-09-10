package awards

import (
	"fmt"
	"strings"
	"time"
)

// Award outcome notification delivery (§8I).
//
// Ordering mirrors Phase C exactly: PERSIST INTENT, THEN SEND. A crash after
// step 1 leaves a recoverable `pending` record, never a sent email with no
// record of it.
//
// Notifications are created and sent only after an EXPLICIT contractor action,
// never automatically on publication. An award is a decision; telling Suppliers
// is a separate, deliberate act.

const AwardOutcomeDeliverySchemaVersion = 1

const DeliveryChannelEmail = "email"

// DeliveryStatus is the delivery lifecycle.
//
// `delivered` is deliberately absent. SMTP acceptance is not proof of delivery,
// and a status claiming otherwise would misrepresent evidence. `sent` means
// exactly "handed to the mail transport".
type DeliveryStatus string

const (
	DeliveryPending  DeliveryStatus = "pending"
	DeliverySent     DeliveryStatus = "sent"
	DeliveryFailed   DeliveryStatus = "failed"
	DeliveryObsolete DeliveryStatus = "obsolete"
)

func (status DeliveryStatus) Valid() bool {
	switch status {
	case DeliveryPending, DeliverySent, DeliveryFailed, DeliveryObsolete:
		return true
	default:
		return false
	}
}

// DeliveryFailureCode is bounded so a contractor learns what to fix, and so raw
// transport text — which can carry infrastructure detail — never reaches a
// stored record or a response.
type DeliveryFailureCode string

const (
	DeliveryFailureInvalidRecipient     DeliveryFailureCode = "invalid_recipient"
	DeliveryFailureTransportRejected    DeliveryFailureCode = "transport_rejected"
	DeliveryFailureTransportUnavailable DeliveryFailureCode = "transport_unavailable"
)

func (code DeliveryFailureCode) Valid() bool {
	switch code {
	case DeliveryFailureInvalidRecipient,
		DeliveryFailureTransportRejected,
		DeliveryFailureTransportUnavailable:
		return true
	default:
		return false
	}
}

// AwardOutcomeDelivery records one attempt to tell one Supplier.
//
// RecipientIdentity and AccessGeneration are SNAPSHOTS taken at send time, so
// history shows which link was current when the mail went out — a recipient
// replacement afterwards does not rewrite what happened.
type AwardOutcomeDelivery struct {
	ID                  string
	CompanyID           string
	AwardOutcomeID      string
	AwardRevisionID     string
	SupplierID          string
	RecipientIdentity   string
	AccessGeneration    int64
	DeliveryOperationID string
	Channel             string
	Status              DeliveryStatus
	FailureCode         DeliveryFailureCode
	CreatedAt           time.Time
	SentAt              *time.Time
	SchemaVersion       int
}

// Validate enforces the discriminated status shape.
func (delivery AwardOutcomeDelivery) Validate() error {
	if strings.TrimSpace(delivery.CompanyID) == "" ||
		strings.TrimSpace(delivery.AwardOutcomeID) == "" ||
		strings.TrimSpace(delivery.AwardRevisionID) == "" ||
		strings.TrimSpace(delivery.SupplierID) == "" ||
		strings.TrimSpace(delivery.DeliveryOperationID) == "" ||
		delivery.Channel != DeliveryChannelEmail {
		return ErrInvalidAwardDelivery
	}
	// Without the recipient snapshot the record cannot say who was told, which
	// is the whole point of keeping it.
	if strings.TrimSpace(delivery.RecipientIdentity) == "" {
		return ErrInvalidAwardDelivery
	}
	if !delivery.Status.Valid() {
		return ErrInvalidAwardDelivery
	}

	switch delivery.Status {
	case DeliveryPending, DeliveryObsolete:
		// Neither has an outcome yet, so neither may claim one.
		if delivery.FailureCode != "" || delivery.SentAt != nil {
			return ErrInvalidAwardDelivery
		}
	case DeliverySent:
		if delivery.SentAt == nil || delivery.FailureCode != "" {
			return ErrInvalidAwardDelivery
		}
	case DeliveryFailed:
		// A failure that cannot say why is not actionable.
		if !delivery.FailureCode.Valid() || delivery.SentAt != nil {
			return ErrInvalidAwardDelivery
		}
	}
	return nil
}

// MayBecomeObsolete reports whether a correction may supersede this record.
//
// Only `pending` qualifies. `sent` and `failed` are historical facts — a
// Supplier really was told, or really was not — and rewriting them would
// falsify the record.
func (delivery AwardOutcomeDelivery) MayBecomeObsolete() bool {
	return delivery.Status == DeliveryPending
}

// DeliveryFromNotification builds the intent record persisted BEFORE the send.
//
// It deliberately copies only identifiers and the recipient snapshot. The
// notification's OutcomeURL is mail-payload content and is NOT stored: a
// delivery record that carried a Supplier's access link would leak entry to the
// outcome through any log or export that reads the record.
func DeliveryFromNotification(
	notification AwardOutcomeNotification,
	operationID string,
	accessGeneration int64,
) AwardOutcomeDelivery {
	return AwardOutcomeDelivery{
		CompanyID:           notification.CompanyID,
		AwardOutcomeID:      notification.OutcomeID,
		SupplierID:          notification.SupplierID,
		RecipientIdentity:   notification.RecipientIdentity,
		AccessGeneration:    accessGeneration,
		DeliveryOperationID: operationID,
		Channel:             DeliveryChannelEmail,
		Status:              DeliveryPending,
		SchemaVersion:       AwardOutcomeDeliverySchemaVersion,
	}
}

// renderDelivery flattens a delivery record to text.
//
// Like renderProjection, it exists so a leakage test asserts over EVERYTHING
// the record could carry rather than the fields a reviewer happened to list. A
// new field holding a credential fails the test automatically.
func renderDelivery(delivery AwardOutcomeDelivery) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s|%s|%s|%s",
		delivery.CompanyID, delivery.AwardOutcomeID, delivery.AwardRevisionID,
		delivery.SupplierID, delivery.RecipientIdentity,
		delivery.AccessGeneration, delivery.DeliveryOperationID,
		delivery.Channel, delivery.Status, delivery.FailureCode)
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(
		strings.ToLower(haystack), strings.ToLower(needle))
}
