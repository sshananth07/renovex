package identity

import "testing"

func TestPrincipalHoldsRawValues(t *testing.T) {
	p := Principal{
		UserID:    "user_123",
		CompanyID: "company_456",
		Role:      "owner",
	}

	if p.UserID != "user_123" {
		t.Fatalf("expected UserID user_123, got %s", p.UserID)
	}
	if p.CompanyID != "company_456" {
		t.Fatalf("expected CompanyID company_456, got %s", p.CompanyID)
	}
	if p.Role != "owner" {
		t.Fatalf("expected Role owner, got %s", p.Role)
	}
}
