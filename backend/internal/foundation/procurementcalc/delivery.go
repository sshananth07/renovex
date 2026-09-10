package procurementcalc

import "github.com/shananth/renovation-platform/backend/internal/foundation/money"

// DeliveryCharge is absent when the Supplier quotes no separate delivery
// amount. Money already carries the sole authoritative currency.
type DeliveryCharge struct {
	Amount money.Money `bson:"amount"`
}

// CalculateDeliveryCharge applies a valid fixed charge once whenever the
// selection contains at least one positively quoted line.
func CalculateDeliveryCharge(
	currency string,
	charge *DeliveryCharge,
	selectedQuotedLineCount int,
) (money.Money, error) {
	if currency != Phase1Currency || selectedQuotedLineCount < 0 {
		return money.Money{}, ErrInvalidDeliveryCharge
	}
	if charge == nil {
		return money.New(0, currency), nil
	}
	if charge.Amount.Amount <= 0 || charge.Amount.Currency != currency {
		return money.Money{}, ErrInvalidDeliveryCharge
	}
	if selectedQuotedLineCount == 0 {
		return money.New(0, currency), nil
	}
	return charge.Amount, nil
}
