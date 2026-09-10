package supplieroffers

import (
	"strings"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// OfferLineResponseStatus makes every issued RFQ line response explicit.
// Drafts may temporarily be unanswered; immutable submitted versions may not.
type OfferLineResponseStatus string

const (
	OfferLineUnanswered  OfferLineResponseStatus = "unanswered"
	OfferLineQuoted      OfferLineResponseStatus = "quoted"
	OfferLineNoBid       OfferLineResponseStatus = "no_bid"
	OfferLineUnavailable OfferLineResponseStatus = "unavailable"
)

// IsSubmittable requires an explicit commercial response. Unknown values fail
// closed alongside unanswered so schema drift cannot silently decline a line.
func (status OfferLineResponseStatus) IsSubmittable() bool {
	return status == OfferLineQuoted ||
		status == OfferLineNoBid ||
		status == OfferLineUnavailable
}

type QuotedLineInput struct {
	RFQLineID             string
	AuthoritativeQuantity quantity.Quantity
	QuotedQuantity        quantity.Quantity
	UnitPrice             money.Money
}

type CalculatedQuotedLine struct {
	RFQLineID      string
	QuotedQuantity quantity.Quantity
	UnitPrice      money.Money
	LineSubtotal   money.Money
}

// CalculateQuotedLine derives the subtotal from the issued RFQ quantity and
// the Supplier's unit price. A client-provided subtotal is deliberately absent.
func CalculateQuotedLine(currency string, input QuotedLineInput) (CalculatedQuotedLine, error) {
	if currency != Phase1Currency {
		return CalculatedQuotedLine{}, ErrUnsupportedCurrency
	}
	if strings.TrimSpace(input.RFQLineID) == "" ||
		input.AuthoritativeQuantity.Value.Sign() <= 0 ||
		strings.TrimSpace(input.AuthoritativeQuantity.Unit) == "" {
		return CalculatedQuotedLine{}, ErrInvalidQuotedLine
	}
	if !input.QuotedQuantity.Value.Equal(input.AuthoritativeQuantity.Value) ||
		input.QuotedQuantity.Unit != input.AuthoritativeQuantity.Unit {
		return CalculatedQuotedLine{}, ErrQuotedQuantityMismatch
	}
	if input.UnitPrice.Amount <= 0 {
		return CalculatedQuotedLine{}, ErrInvalidUnitPrice
	}
	if input.UnitPrice.Currency != currency {
		return CalculatedQuotedLine{}, ErrOfferCurrencyMismatch
	}

	return CalculatedQuotedLine{
		RFQLineID:      input.RFQLineID,
		QuotedQuantity: input.QuotedQuantity,
		UnitPrice:      input.UnitPrice,
		LineSubtotal: money.CalculateLineAmount(
			input.AuthoritativeQuantity.Value,
			input.UnitPrice,
		),
	}, nil
}
