package quantity

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestNewQuantity(t *testing.T) {
	q, err := New("32.75", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Unit != "m2" {
		t.Fatalf("expected unit m2, got %s", q.Unit)
	}
	expected := decimal.RequireFromString("32.75")
	if !q.Value.Equal(expected) {
		t.Fatalf("expected value 32.75, got %s", q.Value.String())
	}
}

func TestNewQuantityInvalidValue(t *testing.T) {
	_, err := New("not-a-number", "m2")
	if err == nil {
		t.Fatal("expected error for invalid decimal string, got nil")
	}
}

func TestQuantityMultiplyByUnitPrice(t *testing.T) {
	q, err := New("30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unitPrice := decimal.RequireFromString("29.50")

	total := q.Value.Mul(unitPrice)
	expected := decimal.RequireFromString("885.00")
	if !total.Equal(expected) {
		t.Fatalf("expected 885.00, got %s", total.String())
	}
}
