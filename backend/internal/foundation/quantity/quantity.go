// Package quantity provides the authoritative representation of physical
// construction quantities (area, length, volume, count) as precise decimal
// values. Never use float32/float64 for authoritative quantity calculations.
package quantity

import "github.com/shopspring/decimal"

// Quantity is a precise decimal amount paired with a unit of measure
// (e.g. "m2", "kg", "bag", "unit").
type Quantity struct {
	Value decimal.Decimal
	Unit  string
}

// New constructs a Quantity from a decimal string and a unit.
// Returns an error if value is not a valid decimal string.
func New(value string, unit string) (Quantity, error) {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{Value: d, Unit: unit}, nil
}
