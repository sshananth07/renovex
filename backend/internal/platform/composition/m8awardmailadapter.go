package composition

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

// awards -> platform/mail (M8 §8A.1).
//
// This adapter names one domain module and one platform package, so it is not a
// cross-domain adapter and needs no boundary exemption.

// AwardNotificationMailerAdapter renders the Supplier-facing outcome mail.
//
// The body is built from the frozen outcome projection only. It carries no
// competitor name, price or total, because the projection it reads has already
// excluded them (§8H) — there is nothing here to leak.
type AwardNotificationMailerAdapter struct {
	sender  mail.EmailSender
	baseURL string
}

func NewAwardNotificationMailerAdapter(
	sender mail.EmailSender,
	baseURL string,
) *AwardNotificationMailerAdapter {
	return &AwardNotificationMailerAdapter{
		sender:  sender,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// SendAwardOutcomeNotification renders the concise outcome mail — the
// authoritative detailed breakdown lives in the Supplier Access portal via
// OutcomeURL, so this stays short: what happened, the awarded total when
// selected, and the link.
func (a *AwardNotificationMailerAdapter) SendAwardOutcomeNotification(
	ctx context.Context,
	notification awards.AwardOutcomeNotification,
	projection awards.OutcomeProjection,
) error {
	subject := fmt.Sprintf("RFQ %s outcome", notification.RFQNumber)
	if notification.RFQNumber == "" {
		subject = "RFQ outcome"
	}

	var body strings.Builder
	if notification.SupplierName != "" {
		fmt.Fprintf(&body, "Hi %s,\n\n", notification.SupplierName)
	}

	contractor := notification.CompanyName
	if contractor == "" {
		contractor = "The contractor"
	}
	fmt.Fprintf(&body, "%s has completed its sourcing decision for %s.\n\n",
		contractor, rfqLabel(notification))

	switch notification.Result {
	case string(awards.OutcomeSelected):
		fmt.Fprintf(&body, "Your quotation was selected for this request.\n\n")
		body.WriteString("Awarded items\n")
		for _, line := range projection.AwardedLines {
			fmt.Fprintf(&body, "- %s", line.MaterialName)
			if line.Quantity.Unit != "" {
				fmt.Fprintf(&body, " — %s %s", line.Quantity.Value.String(), line.Quantity.Unit)
			}
			fmt.Fprintf(&body, " — %s\n", formatMinorUnits(line.LineSubtotal))
		}
		fmt.Fprintf(&body, "Awarded total: %s\n\n", formatMinorUnits(projection.AwardTotal))
	default:
		// Neutral, and deliberately silent about who did win or how many
		// Suppliers responded.
		body.WriteString("Following our review, your quotation was not selected for this request.\n\n")
	}

	if notification.ContractorMessage != "" {
		fmt.Fprintf(&body, "%s\n\n", notification.ContractorMessage)
	}
	if notification.OutcomeURL != "" {
		fmt.Fprintf(&body, "Review RFQ outcome: %s\n\n", notification.OutcomeURL)
	}

	// Stated in every outcome mail so a Supplier cannot read an award as a
	// Purchase Order (§8J).
	fmt.Fprintf(&body, "%s\n", awards.AcknowledgementMeaning)

	return a.sender.Send(ctx, mail.Message{
		To:      notification.RecipientIdentity,
		Subject: subject,
		Body:    body.String(),
	})
}

func rfqLabel(notification awards.AwardOutcomeNotification) string {
	if notification.RFQTitle != "" && notification.RFQNumber != "" {
		return fmt.Sprintf("%s (%s)", notification.RFQTitle, notification.RFQNumber)
	}
	if notification.RFQNumber != "" {
		return notification.RFQNumber
	}
	return "this request"
}

// formatMinorUnits renders a Money value as "RM123.45" using pure integer
// string manipulation — never a floating-point division — matching this
// codebase's exact-money-conversion rule (ADR 0001) on the display side too.
func formatMinorUnits(amount money.Money) string {
	negative := amount.Amount < 0
	digits := strconv.FormatInt(amount.Amount, 10)
	if negative {
		digits = digits[1:]
	}
	for len(digits) < 3 {
		digits = "0" + digits
	}
	whole := digits[:len(digits)-2]
	fraction := digits[len(digits)-2:]
	sign := ""
	if negative {
		sign = "-"
	}
	symbol := currencySymbol(amount.Currency)
	return fmt.Sprintf("%s%s%s.%s", sign, symbol, whole, fraction)
}

func currencySymbol(currency string) string {
	if currency == "MYR" || currency == "" {
		return "RM"
	}
	return currency + " "
}
