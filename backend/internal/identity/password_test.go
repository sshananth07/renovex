package identity

import "testing"

func TestHashAndVerifyPasswordSucceeds(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("unexpected error hashing: %v", err)
	}

	if err := VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("expected verify to succeed, got: %v", err)
	}
}

func TestVerifyPasswordWrongPasswordFails(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("unexpected error hashing: %v", err)
	}

	if err := VerifyPassword(hash, "wrong password"); err == nil {
		t.Fatal("expected verify to fail for wrong password, got nil")
	}
}
