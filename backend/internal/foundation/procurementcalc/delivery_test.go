package procurementcalc

import (
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func TestCalculateDeliveryChargeAppliesFixedAmountAtMostOnce(t *testing.T) {
	charge := &DeliveryCharge{Amount: money.New(15_000, "MYR")}
	tests := []struct {
		name          string
		selectedLines int
		want          money.Money
	}{
		{name: "no selected quoted lines", selectedLines: 0, want: money.New(0, "MYR")},
		{name: "one selected quoted line", selectedLines: 1, want: money.New(15_000, "MYR")},
		{name: "several selected quoted lines", selectedLines: 3, want: money.New(15_000, "MYR")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateDeliveryCharge("MYR", charge, tt.selectedLines)
			if err != nil {
				t.Fatalf("CalculateDeliveryCharge returned an unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("delivery charge = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCalculateDeliveryChargeDistinguishesAbsenceFromInvalidCharge(t *testing.T) {
	got, err := CalculateDeliveryCharge("MYR", nil, 2)
	if err != nil {
		t.Fatalf("absent delivery charge returned an unexpected error: %v", err)
	}
	if got != money.New(0, "MYR") {
		t.Fatalf("absent delivery charge = %+v, want zero MYR", got)
	}

	tests := []struct {
		name          string
		currency      string
		charge        *DeliveryCharge
		selectedLines int
	}{
		{
			name:          "zero amount",
			currency:      "MYR",
			charge:        &DeliveryCharge{Amount: money.New(0, "MYR")},
			selectedLines: 1,
		},
		{
			name:          "negative amount",
			currency:      "MYR",
			charge:        &DeliveryCharge{Amount: money.New(-1, "MYR")},
			selectedLines: 1,
		},
		{
			name:          "foreign charge currency",
			currency:      "MYR",
			charge:        &DeliveryCharge{Amount: money.New(100, "USD")},
			selectedLines: 1,
		},
		{
			name:          "unsupported rfq currency",
			currency:      "USD",
			charge:        &DeliveryCharge{Amount: money.New(100, "USD")},
			selectedLines: 1,
		},
		{
			name:          "negative selected count",
			currency:      "MYR",
			charge:        &DeliveryCharge{Amount: money.New(100, "MYR")},
			selectedLines: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateDeliveryCharge(tt.currency, tt.charge, tt.selectedLines)
			if !errors.Is(err, ErrInvalidDeliveryCharge) {
				t.Fatalf("CalculateDeliveryCharge error = %v, want ErrInvalidDeliveryCharge", err)
			}
		})
	}
}
