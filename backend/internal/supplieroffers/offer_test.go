package supplieroffers

import (
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

func TestCalculateQuotedLineUsesAuthoritativeQuantityAndCentralRounding(t *testing.T) {
	qty, err := quantity.New("2.5", "unit")
	if err != nil {
		t.Fatalf("quantity.New returned an unexpected error: %v", err)
	}

	line, err := CalculateQuotedLine("MYR", QuotedLineInput{
		RFQLineID:             "line-1",
		AuthoritativeQuantity: qty,
		QuotedQuantity:        qty,
		UnitPrice:             money.New(1_001, "MYR"),
	})
	if err != nil {
		t.Fatalf("CalculateQuotedLine returned an unexpected error: %v", err)
	}
	if line.LineSubtotal != money.New(2_503, "MYR") {
		t.Fatalf("line subtotal = %+v, want MYR 25.03", line.LineSubtotal)
	}
}

func TestCalculateQuotedLineRejectsQuantityPriceAndCurrencyMismatch(t *testing.T) {
	qtyTwoUnits, _ := quantity.New("2", "unit")
	qtyOneUnit, _ := quantity.New("1", "unit")
	qtyTwoKilograms, _ := quantity.New("2", "kg")

	tests := []struct {
		name   string
		quoted quantity.Quantity
		price  money.Money
		want   error
	}{
		{
			name:   "partial quantity",
			quoted: qtyOneUnit,
			price:  money.New(100, "MYR"),
			want:   ErrQuotedQuantityMismatch,
		},
		{
			name:   "different unit",
			quoted: qtyTwoKilograms,
			price:  money.New(100, "MYR"),
			want:   ErrQuotedQuantityMismatch,
		},
		{
			name:   "zero price",
			quoted: qtyTwoUnits,
			price:  money.New(0, "MYR"),
			want:   ErrInvalidUnitPrice,
		},
		{
			name:   "negative price",
			quoted: qtyTwoUnits,
			price:  money.New(-1, "MYR"),
			want:   ErrInvalidUnitPrice,
		},
		{
			name:   "foreign currency",
			quoted: qtyTwoUnits,
			price:  money.New(100, "USD"),
			want:   ErrOfferCurrencyMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateQuotedLine("MYR", QuotedLineInput{
				RFQLineID:             "line-1",
				AuthoritativeQuantity: qtyTwoUnits,
				QuotedQuantity:        tt.quoted,
				UnitPrice:             tt.price,
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("CalculateQuotedLine error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOfferLineResponseStatusIsSubmittableOnlyWhenExplicitlyAnswered(t *testing.T) {
	tests := []struct {
		status OfferLineResponseStatus
		want   bool
	}{
		{status: OfferLineQuoted, want: true},
		{status: OfferLineNoBid, want: true},
		{status: OfferLineUnavailable, want: true},
		{status: OfferLineUnanswered, want: false},
		{status: OfferLineResponseStatus("unknown"), want: false},
	}

	for _, tt := range tests {
		if got := tt.status.IsSubmittable(); got != tt.want {
			t.Fatalf("%q IsSubmittable = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestCalculateQuotedLineRejectsInvalidAuthoritativeLine(t *testing.T) {
	validQuantity, _ := quantity.New("2", "unit")
	zeroQuantity, _ := quantity.New("0", "unit")
	unitlessQuantity, _ := quantity.New("2", "")

	tests := []struct {
		name   string
		lineID string
		qty    quantity.Quantity
	}{
		{name: "blank rfq line id", lineID: " ", qty: validQuantity},
		{name: "zero authoritative quantity", lineID: "line-1", qty: zeroQuantity},
		{name: "unitless authoritative quantity", lineID: "line-1", qty: unitlessQuantity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateQuotedLine("MYR", QuotedLineInput{
				RFQLineID:             tt.lineID,
				AuthoritativeQuantity: tt.qty,
				QuotedQuantity:        tt.qty,
				UnitPrice:             money.New(100, "MYR"),
			})
			if !errors.Is(err, ErrInvalidQuotedLine) {
				t.Fatalf("CalculateQuotedLine error = %v, want ErrInvalidQuotedLine", err)
			}
		})
	}
}
