package awards

import (
	"errors"
	"strings"
	"testing"
)

// F9 Supplier acknowledgement (§8J).
//
// Acknowledgement confirms RECEIPT ONLY. It is not acceptance of a Purchase
// Order, contractual acceptance, confirmation of delivery, invoice approval,
// consent to the award, or any precondition for the award being authoritative.

// The wording is fixed in the domain so no caller can soften it into something
// a Supplier could read as acceptance.
func TestAcknowledgementWordingStatesReceiptOnly(t *testing.T) {
	wording := strings.ToLower(AcknowledgementMeaning)

	for _, phrase := range []string{"receipt only", "purchase order"} {
		if !strings.Contains(wording, phrase) {
			t.Errorf("the acknowledgement wording must mention %q: %q",
				phrase, AcknowledgementMeaning)
		}
	}
	// It must not describe itself as acceptance of anything.
	if strings.Contains(wording, "accepts the award") ||
		strings.Contains(wording, "agree to") {
		t.Errorf("the wording implies acceptance: %q", AcknowledgementMeaning)
	}
}

func validAcknowledgement() AwardOutcomeAcknowledgement {
	return AwardOutcomeAcknowledgement{
		ID: "ack-1", CompanyID: "company-1", AwardOutcomeID: "outcome-1",
		SupplierID: "supplier-a", InvitationID: "invitation-a",
		SessionID: "session-1", RecipientIdentity: "buyer@example.test",
		OperationID: "op-1", AcknowledgedAt: calcAt,
	}
}

// The record captures WHO actually acknowledged, which may be a replacement
// recipient — that is the honest fact, and it is what makes the receipt
// meaningful later.
func TestAcknowledgementRecordsTheActingIdentity(t *testing.T) {
	acknowledgement := validAcknowledgement()
	if err := acknowledgement.Validate(); err != nil {
		t.Fatalf("a well-formed acknowledgement is valid: %v", err)
	}

	for _, missing := range []func(*AwardOutcomeAcknowledgement){
		func(a *AwardOutcomeAcknowledgement) { a.RecipientIdentity = "" },
		func(a *AwardOutcomeAcknowledgement) { a.SessionID = "" },
		func(a *AwardOutcomeAcknowledgement) { a.SupplierID = "" },
		func(a *AwardOutcomeAcknowledgement) { a.InvitationID = "" },
		func(a *AwardOutcomeAcknowledgement) { a.OperationID = "" },
	} {
		candidate := validAcknowledgement()
		missing(&candidate)
		if err := candidate.Validate(); !errors.Is(
			err, ErrInvalidAcknowledgement) {
			t.Errorf("an acknowledgement missing an identity must be refused")
		}
	}
}

// An acknowledgement with no timestamp records nothing useful: the receipt's
// value is that it happened at a knowable moment.
func TestAcknowledgementRequiresATimestamp(t *testing.T) {
	acknowledgement := validAcknowledgement()
	acknowledgement.AcknowledgedAt = timeZero()

	if err := acknowledgement.Validate(); !errors.Is(
		err, ErrInvalidAcknowledgement) {
		t.Fatal("an acknowledgement must record when it happened")
	}
}
