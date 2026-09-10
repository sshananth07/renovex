package composition_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

type capturingAwardMailSender struct {
	message mail.Message
}

func (sender *capturingAwardMailSender) Send(_ context.Context, message mail.Message) error {
	sender.message = message
	return nil
}

func TestAwardMailUsesOwnOutcomeAllowlistAndOmitsInternalCanaries(t *testing.T) {
	sender := &capturingAwardMailSender{}
	adapter := composition.NewAwardNotificationMailerAdapter(sender, "https://supplier.test")
	notification := awards.AwardOutcomeNotification{
		// CompanyID is the internal tenant identifier and must never reach the
		// mail; CompanyName is the contractor's own display name and is
		// deliberately shown to the Supplier ("<CompanyName> has completed its
		// sourcing decision...") — it is not sensitive.
		CompanyID: "FORBIDDEN-COMPANY-CANARY", CompanyName: "Allowed Contractor Sdn Bhd",
		SupplierID: "FORBIDDEN-SUPPLIER-ID", SupplierName: "Allowed Supplier",
		InvitationID:      "FORBIDDEN-INVITATION-ID",
		RecipientIdentity: "recipient@supplier.test", RFQNumber: "RFQ-42",
		RFQTitle: "Allowed request", OutcomeID: "FORBIDDEN-OUTCOME-ID",
		Result: string(awards.OutcomeSelected), ContractorMessage: "Allowed own message",
		// The real OutcomeURL does encode the outcome/invitation IDs into its
		// own path — that is the point of the link — so this fixture uses
		// harmless placeholder IDs distinct from the canary values above,
		// proving the mail never ALSO leaks those same canaries anywhere
		// outside the one sanctioned OutcomeURL field.
		OutcomeURL: "https://supplier.test/supplier-access/open?token=abc&returnTo=%2Fsupplier-access%2Finvitations%2Finvitation-1%2Foutcomes%2Foutcome-1",
	}
	projection := awards.OutcomeProjection{}
	if err := adapter.SendAwardOutcomeNotification(context.Background(), notification, projection); err != nil {
		t.Fatalf("SendAwardOutcomeNotification: %v", err)
	}
	serialized := sender.message.To + "\n" + sender.message.Subject + "\n" + sender.message.Body
	for _, forbidden := range []string{notification.CompanyID,
		notification.SupplierID, notification.OutcomeID, notification.InvitationID} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("mail leaked forbidden canary %q: %s", forbidden, serialized)
		}
	}
	for _, allowed := range []string{notification.RecipientIdentity, notification.SupplierName,
		notification.CompanyName, notification.RFQNumber, notification.RFQTitle,
		notification.ContractorMessage, notification.OutcomeURL, awards.AcknowledgementMeaning} {
		if !strings.Contains(serialized, allowed) {
			t.Fatalf("mail omitted allowed own-outcome value %q: %s", allowed, serialized)
		}
	}
}

// The exact wording below is a locked product requirement, not an
// implementation detail — a future edit that rewords this mail must fail this
// test rather than silently drift from the agreed copy.
func TestAwardMailSelectedUsesTheExactLockedWording(t *testing.T) {
	sender := &capturingAwardMailSender{}
	adapter := composition.NewAwardNotificationMailerAdapter(sender, "https://supplier.test")
	notification := awards.AwardOutcomeNotification{
		RecipientIdentity: "recipient@supplier.test",
		Result:            string(awards.OutcomeSelected),
		OutcomeURL:        "https://supplier.test/supplier-access/open?token=abc",
	}
	cementQty, err := quantity.New("25", "bags")
	if err != nil {
		t.Fatalf("quantity.New: %v", err)
	}
	sandQty, err := quantity.New("2.5", "tonnes")
	if err != nil {
		t.Fatalf("quantity.New: %v", err)
	}
	projection := awards.OutcomeProjection{
		AwardedLines: []awards.OutcomeAwardedLine{
			{MaterialName: "Cement", Quantity: cementQty, LineSubtotal: money.New(57000, "MYR")},
			{MaterialName: "Sand", Quantity: sandQty, LineSubtotal: money.New(19750, "MYR")},
		},
		AwardTotal: money.New(76750, "MYR"),
	}

	if err := adapter.SendAwardOutcomeNotification(context.Background(), notification, projection); err != nil {
		t.Fatalf("SendAwardOutcomeNotification: %v", err)
	}

	want := "Your quotation was selected for this request.\n\n" +
		"Awarded items\n" +
		"- Cement — 25 bags — RM570.00\n" +
		"- Sand — 2.5 tonnes — RM197.50\n" +
		"Awarded total: RM767.50\n\n" +
		"Review RFQ outcome: https://supplier.test/supplier-access/open?token=abc\n\n"
	if !strings.Contains(sender.message.Body, want) {
		t.Fatalf("mail body = %q, want it to contain the exact locked wording %q", sender.message.Body, want)
	}
}

func TestAwardMailUnsuccessfulUsesTheExactLockedWording(t *testing.T) {
	sender := &capturingAwardMailSender{}
	adapter := composition.NewAwardNotificationMailerAdapter(sender, "https://supplier.test")
	notification := awards.AwardOutcomeNotification{
		RecipientIdentity: "recipient@supplier.test",
		Result:            string(awards.OutcomeUnsuccessful),
		OutcomeURL:        "https://supplier.test/supplier-access/open?token=abc",
	}

	if err := adapter.SendAwardOutcomeNotification(context.Background(), notification, awards.OutcomeProjection{}); err != nil {
		t.Fatalf("SendAwardOutcomeNotification: %v", err)
	}

	want := "Following our review, your quotation was not selected for this request.\n\n" +
		"Review RFQ outcome: https://supplier.test/supplier-access/open?token=abc\n\n"
	if !strings.Contains(sender.message.Body, want) {
		t.Fatalf("mail body = %q, want it to contain the exact locked wording %q", sender.message.Body, want)
	}
}
