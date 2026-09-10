package composition

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func testCapabilityKeyring(t *testing.T) *secrets.VisualAssetCapabilityKeyring {
	t.Helper()
	k, err := secrets.NewVisualAssetCapabilityKeyring(1, map[int]string{
		1: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return k
}

func TestLocalHMACReadAccessProvider_CreateReadAccess_ReturnsAbsoluteURL(t *testing.T) {
	keyring := testCapabilityKeyring(t)
	provider := NewLocalHMACReadAccessProvider(keyring, "http://localhost:8080")

	access, err := provider.CreateReadAccess(context.Background(), spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
	}, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(access.URL, "http://localhost:8080/") {
		t.Fatalf("expected an absolute URL rooted at the configured API base, got %s", access.URL)
	}
}

func TestLocalHMACReadAccessProvider_CreateReadAccess_URLCarriesVerifiableCapability(t *testing.T) {
	keyring := testCapabilityKeyring(t)
	provider := NewLocalHMACReadAccessProvider(keyring, "http://localhost:8080")

	access, err := provider.CreateReadAccess(context.Background(), spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
	}, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Extract the "cap" query param and confirm the SAME keyring can
	// verify it back to the correct claims — proving the provider mints
	// a genuinely verifiable token, not just an opaque-looking string.
	idx := strings.Index(access.URL, "cap=")
	if idx < 0 {
		t.Fatalf("expected the URL to carry a cap= query parameter, got %s", access.URL)
	}
	token := access.URL[idx+len("cap="):]
	claims, err := keyring.Verify(token)
	if err != nil {
		t.Fatalf("expected the minted URL's capability to verify, got error: %v", err)
	}
	if claims.CompanyID != "company_a" || claims.AssetID != "boiler-asset" || claims.Version != 1 {
		t.Fatalf("expected claims to match the asset version, got %+v", claims)
	}
}

func TestLocalHMACReadAccessProvider_CreateReadAccess_NeverExposesStorageKey(t *testing.T) {
	keyring := testCapabilityKeyring(t)
	provider := NewLocalHMACReadAccessProvider(keyring, "http://localhost:8080")

	access, err := provider.CreateReadAccess(context.Background(), spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		StorageKey: "visual-assets/company_a/boiler-asset/v1/deadbeef.glb",
	}, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(access.URL, "visual-assets/company_a/boiler-asset/v1/deadbeef.glb") {
		t.Fatalf("expected the StorageKey to never appear in the returned URL, got %s", access.URL)
	}
}

func TestLocalHMACReadAccessProvider_CreateReadAccess_ExpiresAtMatchesTTL(t *testing.T) {
	keyring := testCapabilityKeyring(t)
	provider := NewLocalHMACReadAccessProvider(keyring, "http://localhost:8080")

	before := time.Now()
	access, err := provider.CreateReadAccess(context.Background(), spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
	}, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	if access.ExpiresAt.Before(before.Add(10*time.Minute)) || access.ExpiresAt.After(after.Add(10*time.Minute)) {
		t.Fatalf("expected ExpiresAt approximately 10m from now, got %s", access.ExpiresAt)
	}
}
