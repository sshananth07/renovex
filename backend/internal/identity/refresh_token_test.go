package identity

import "testing"

func TestGenerateRefreshTokenIsNonEmptyAndUnique(t *testing.T) {
	a, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("expected non-empty tokens")
	}
	if a == b {
		t.Fatal("expected distinct tokens across calls")
	}
}

func TestHashRefreshTokenIsDeterministic(t *testing.T) {
	token := "some-raw-refresh-token-value"
	h1 := HashRefreshToken(token)
	h2 := HashRefreshToken(token)
	if h1 != h2 {
		t.Fatal("expected identical hash for identical input")
	}
}

func TestHashRefreshTokenDistinctForDistinctInput(t *testing.T) {
	h1 := HashRefreshToken("token-a")
	h2 := HashRefreshToken("token-b")
	if h1 == h2 {
		t.Fatal("expected distinct hashes for distinct input")
	}
}
