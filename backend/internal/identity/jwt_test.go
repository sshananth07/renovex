package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueAndVerifyAccessToken(t *testing.T) {
	secret := []byte("test-secret-key-for-unit-tests")
	issuer := NewJWTIssuer(secret, time.Minute)

	tokenString, err := issuer.IssueAccessToken("user_1", "company_1", "owner")
	if err != nil {
		t.Fatalf("unexpected error issuing: %v", err)
	}

	claims, err := issuer.VerifyAccessToken(tokenString)
	if err != nil {
		t.Fatalf("unexpected error verifying: %v", err)
	}
	if claims.UserID != "user_1" {
		t.Fatalf("expected UserID user_1, got %s", claims.UserID)
	}
	if claims.CompanyID != "company_1" {
		t.Fatalf("expected CompanyID company_1, got %s", claims.CompanyID)
	}
	if claims.Role != "owner" {
		t.Fatalf("expected Role owner, got %s", claims.Role)
	}
}

func TestVerifyAccessTokenExpiredRejected(t *testing.T) {
	secret := []byte("test-secret-key-for-unit-tests")
	issuer := NewJWTIssuer(secret, -time.Minute) // already-expired TTL

	tokenString, err := issuer.IssueAccessToken("user_1", "company_1", "owner")
	if err != nil {
		t.Fatalf("unexpected error issuing: %v", err)
	}

	_, err = issuer.VerifyAccessToken(tokenString)
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestVerifyAccessTokenMalformedRejected(t *testing.T) {
	secret := []byte("test-secret-key-for-unit-tests")
	issuer := NewJWTIssuer(secret, time.Minute)

	_, err := issuer.VerifyAccessToken("not-a-real-token")
	if err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
}

func TestVerifyAccessTokenWrongSigningKeyRejected(t *testing.T) {
	issuerA := NewJWTIssuer([]byte("secret-a"), time.Minute)
	issuerB := NewJWTIssuer([]byte("secret-b"), time.Minute)

	tokenString, err := issuerA.IssueAccessToken("user_1", "company_1", "owner")
	if err != nil {
		t.Fatalf("unexpected error issuing: %v", err)
	}

	_, err = issuerB.VerifyAccessToken(tokenString)
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		t.Fatalf("expected ErrTokenSignatureInvalid, got %v", err)
	}
}
