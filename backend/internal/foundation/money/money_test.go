package money

import "testing"

func TestNewMoneyFromMinorUnits(t *testing.T) {
	m := New(1850, "MYR")
	if m.Amount != 1850 {
		t.Fatalf("expected amount 1850, got %d", m.Amount)
	}
	if m.Currency != "MYR" {
		t.Fatalf("expected currency MYR, got %s", m.Currency)
	}
}

func TestMoneyAddSameCurrency(t *testing.T) {
	a := New(1000, "MYR")
	b := New(500, "MYR")

	result, err := a.Add(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 1500 {
		t.Fatalf("expected 1500, got %d", result.Amount)
	}
}

func TestMoneyAddDifferentCurrencyReturnsError(t *testing.T) {
	a := New(1000, "MYR")
	b := New(500, "SGD")

	_, err := a.Add(b)
	if err == nil {
		t.Fatal("expected error when adding different currencies, got nil")
	}
}

func TestMoneySubtractSameCurrency(t *testing.T) {
	a := New(1500, "MYR")
	b := New(500, "MYR")

	result, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 1000 {
		t.Fatalf("expected 1000, got %d", result.Amount)
	}
}

func TestRateBPSConstants(t *testing.T) {
	if RateBPS(600) != 600 {
		t.Fatal("RateBPS should be a plain int64-based type")
	}
}
