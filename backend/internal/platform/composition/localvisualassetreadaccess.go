package composition

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// LocalHMACReadAccessProvider implements spatial.VisualAssetReadAccessProvider
// using the HMAC-based VisualAssetCapabilityKeyring and Renovex's own
// unauthenticated content route (RP4D §20/§21) — the local-development
// implementation of the same capability a future Vercel Blob/S3-backed
// provider would satisfy directly with ITS OWN signed URL. Never
// constructs a URL from an incoming request's Host/Origin header — apiBaseURL
// is the backend's own trusted, already-configured absolute origin
// (EXTERNAL_API_BASE_URL), matching the existing precedent for every
// other absolute URL this backend hands to a client.
type LocalHMACReadAccessProvider struct {
	keyring    *secrets.VisualAssetCapabilityKeyring
	apiBaseURL string
}

// NewLocalHMACReadAccessProvider wraps keyring, minting capability URLs
// rooted at apiBaseURL (e.g. "http://localhost:8080" in local dev).
func NewLocalHMACReadAccessProvider(keyring *secrets.VisualAssetCapabilityKeyring, apiBaseURL string) *LocalHMACReadAccessProvider {
	return &LocalHMACReadAccessProvider{keyring: keyring, apiBaseURL: apiBaseURL}
}

// VerifyReadAccess implements spatial.VisualAssetLocalCapabilityVerifier —
// recovers and validates the claims embedded in a minted capability token.
// Signature and expiry are verified by the keyring itself BEFORE any claim
// is trusted (secrets.VisualAssetCapabilityKeyring.Verify's own contract).
func (p *LocalHMACReadAccessProvider) VerifyReadAccess(ctx context.Context, capability string) (companyID, assetID string, version int, err error) {
	claims, err := p.keyring.Verify(capability)
	if err != nil {
		return "", "", 0, err
	}
	return claims.CompanyID, claims.AssetID, claims.Version, nil
}

func (p *LocalHMACReadAccessProvider) CreateReadAccess(ctx context.Context, assetVersion spatial.VisualAssetVersion, ttl time.Duration) (spatial.ReadAccess, error) {
	expiresAt := time.Now().Add(ttl)
	token, err := p.keyring.Mint(assetVersion.CompanyID, assetVersion.AssetID, assetVersion.Version, expiresAt)
	if err != nil {
		return spatial.ReadAccess{}, err
	}
	values := url.Values{}
	values.Set("cap", token)
	return spatial.ReadAccess{
		URL:       fmt.Sprintf("%s/spatial/visual-assets/content?%s", p.apiBaseURL, values.Encode()),
		ExpiresAt: expiresAt,
	}, nil
}
