// Package money provides the canonical authoritative representation of
// financial amounts in the platform: int64 minor units plus an ISO 4217-style
// currency code. Never use float32/float64 for authoritative money values.
package money

import "fmt"

// Money is an amount expressed in the smallest unit of its currency
// (e.g. cents for MYR/SGD/USD), never a floating-point major-unit value.
type Money struct {
	Amount   int64  `bson:"amount" json:"amount"`
	Currency string `bson:"currency" json:"currency"`
}

// New constructs a Money value from a minor-unit integer amount and an
// ISO 4217-style currency code (e.g. "MYR").
func New(amountMinorUnits int64, currency string) Money {
	return Money{Amount: amountMinorUnits, Currency: currency}
}

// Add returns a+b. It returns an error if the currencies differ.
func (a Money) Add(b Money) (Money, error) {
	if a.Currency != b.Currency {
		return Money{}, fmt.Errorf("money: cannot add %s to %s", b.Currency, a.Currency)
	}
	return Money{Amount: a.Amount + b.Amount, Currency: a.Currency}, nil
}

// Subtract returns a-b. It returns an error if the currencies differ.
func (a Money) Subtract(b Money) (Money, error) {
	if a.Currency != b.Currency {
		return Money{}, fmt.Errorf("money: cannot subtract %s from %s", b.Currency, a.Currency)
	}
	return Money{Amount: a.Amount - b.Amount, Currency: a.Currency}, nil
}
