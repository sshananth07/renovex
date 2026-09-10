package awards

import (
	"errors"
	"testing"
)

// F8 notification delivery (§8I).
//
// `delivered` is deliberately NOT a status: SMTP acceptance is not proof of
// delivery, and claiming otherwise would misrepresent evidence. `sent` means
// "handed to the mail transport".

func TestDeliveryStatusesExcludeDelivered(t *testing.T) {
	valid := map[DeliveryStatus]bool{
		DeliveryPending:  true,
		DeliverySent:     true,
		DeliveryFailed:   true,
		DeliveryObsolete: true,
	}
	for status := range valid {
		if !status.Valid() {
			t.Errorf("%q must be a valid delivery status", status)
		}
	}
	// The one status that must not exist.
	if DeliveryStatus("delivered").Valid() {
		t.Fatal("`delivered` must not be a delivery status: SMTP acceptance " +
			"is not proof of delivery (§8I)")
	}
}

// Failure codes are bounded so a caller learns what to fix rather than
// receiving raw transport text, which could carry infrastructure detail.
func TestDeliveryFailureCodesAreBounded(t *testing.T) {
	for _, code := range []DeliveryFailureCode{
		DeliveryFailureInvalidRecipient,
		DeliveryFailureTransportRejected,
		DeliveryFailureTransportUnavailable,
	} {
		if !code.Valid() {
			t.Errorf("%q must be a valid failure code", code)
		}
	}
	for _, code := range []DeliveryFailureCode{
		"", "dial tcp 10.0.0.5:25: refused", "unknown",
	} {
		if code.Valid() {
			t.Errorf("%q must not be an accepted failure code", code)
		}
	}
}

// A pending record carries no send time and no failure code: it records only
// that intent was persisted before the send was attempted.
func TestPendingDeliveryCarriesNoOutcomeFields(t *testing.T) {
	delivery := AwardOutcomeDelivery{
		ID: "delivery-1", CompanyID: "company-1",
		AwardOutcomeID: "outcome-1", AwardRevisionID: "revision-1",
		SupplierID: "supplier-a", RecipientIdentity: "buyer@example.test",
		AccessGeneration: 1, DeliveryOperationID: "op-1",
		Channel: DeliveryChannelEmail, Status: DeliveryPending,
	}
	if err := delivery.Validate(); err != nil {
		t.Fatalf("a well-formed pending delivery must be valid: %v", err)
	}

	delivery.FailureCode = DeliveryFailureTransportRejected
	if err := delivery.Validate(); !errors.Is(err, ErrInvalidAwardDelivery) {
		t.Fatal("a pending delivery must not carry a failure code")
	}
}

// A failed delivery must say WHY, in bounded terms.
func TestFailedDeliveryRequiresABoundedFailureCode(t *testing.T) {
	delivery := AwardOutcomeDelivery{
		ID: "delivery-1", CompanyID: "company-1",
		AwardOutcomeID: "outcome-1", AwardRevisionID: "revision-1",
		SupplierID: "supplier-a", RecipientIdentity: "buyer@example.test",
		DeliveryOperationID: "op-1",
		Channel:             DeliveryChannelEmail, Status: DeliveryFailed,
	}
	if err := delivery.Validate(); !errors.Is(err, ErrInvalidAwardDelivery) {
		t.Fatal("a failed delivery must carry a bounded failure code")
	}

	delivery.FailureCode = DeliveryFailureInvalidRecipient
	if err := delivery.Validate(); err != nil {
		t.Fatalf("a failed delivery with a bounded code is valid: %v", err)
	}
}

// A sent delivery must record when it was handed to the transport.
func TestSentDeliveryRequiresASendTime(t *testing.T) {
	delivery := AwardOutcomeDelivery{
		ID: "delivery-1", CompanyID: "company-1",
		AwardOutcomeID: "outcome-1", AwardRevisionID: "revision-1",
		SupplierID: "supplier-a", RecipientIdentity: "buyer@example.test",
		DeliveryOperationID: "op-1",
		Channel:             DeliveryChannelEmail, Status: DeliverySent,
	}
	if err := delivery.Validate(); !errors.Is(err, ErrInvalidAwardDelivery) {
		t.Fatal("a sent delivery must record its send time")
	}

	sentAt := calcAt
	delivery.SentAt = &sentAt
	if err := delivery.Validate(); err != nil {
		t.Fatalf("a sent delivery with a send time is valid: %v", err)
	}
}

// A delivery must snapshot the recipient identity and access generation, so
// history shows which link was current when the mail was sent.
func TestDeliveryRequiresItsRecipientSnapshot(t *testing.T) {
	delivery := AwardOutcomeDelivery{
		ID: "delivery-1", CompanyID: "company-1",
		AwardOutcomeID: "outcome-1", AwardRevisionID: "revision-1",
		SupplierID: "supplier-a", DeliveryOperationID: "op-1",
		Channel: DeliveryChannelEmail, Status: DeliveryPending,
	}
	if err := delivery.Validate(); !errors.Is(err, ErrInvalidAwardDelivery) {
		t.Fatal("a delivery must snapshot the recipient it was sent to")
	}
}

// Only `pending` records become obsolete. `sent` and `failed` are historical
// facts: a Supplier really was told, and rewriting that would falsify the
// record (§8I).
func TestOnlyPendingDeliveriesMayBecomeObsolete(t *testing.T) {
	cases := map[DeliveryStatus]bool{
		DeliveryPending:  true,
		DeliverySent:     false,
		DeliveryFailed:   false,
		DeliveryObsolete: false,
	}
	for status, want := range cases {
		delivery := AwardOutcomeDelivery{Status: status}
		if got := delivery.MayBecomeObsolete(); got != want {
			t.Errorf("status %q may become obsolete = %v, want %v",
				status, got, want)
		}
	}
}

// A delivery record must never carry a Supplier credential: the record and the
// logs derived from it would then leak access to the outcome.
func TestDeliveryCarriesNoSupplierCredential(t *testing.T) {
	notification := AwardOutcomeNotification{
		CompanyID: "company-1", SupplierID: "supplier-a",
		RecipientIdentity: "buyer@example.test",
		OutcomeID:         "outcome-1", Result: "selected",
		OutcomeURL: "https://app.example.test/supplier-access/outcomes/outcome-1",
	}
	// The mail payload may carry a URL, but a delivery RECORD carries only the
	// identifiers needed to explain what was attempted.
	delivery := DeliveryFromNotification(notification, "op-1", 1)

	if delivery.RecipientIdentity != "buyer@example.test" {
		t.Errorf("recipient = %q, want the snapshot",
			delivery.RecipientIdentity)
	}
	rendered := renderDelivery(delivery)
	for _, secret := range []string{
		"supplier-access/outcomes", "https://", "token", "session",
	} {
		if containsFold(rendered, secret) {
			t.Errorf("delivery record leaks %q", secret)
		}
	}
}
