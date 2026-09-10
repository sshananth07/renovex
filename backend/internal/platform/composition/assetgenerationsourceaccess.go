package composition

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// LocalAssetGenerationSourceAccessProvider implements
// spatial.AssetGenerationSourceAccessProvider using the HMAC-based
// AssetGenerationSourceCapabilityKeyring and Renovex's own
// unauthenticated content route (RP4E0 spec §6) — mirrors
// LocalHMACReadAccessProvider's exact shape from RP4D, with a distinct
// capability domain-separation string so a source-image capability can
// never be replayed against the visual-asset-content route or vice
// versa. Never constructs a URL from an incoming request's Host/Origin
// header — apiBaseURL is the backend's own trusted, already-configured
// absolute origin.
type LocalAssetGenerationSourceAccessProvider struct {
	keyring    *secrets.AssetGenerationSourceCapabilityKeyring
	apiBaseURL string
}

func NewLocalAssetGenerationSourceAccessProvider(keyring *secrets.AssetGenerationSourceCapabilityKeyring, apiBaseURL string) *LocalAssetGenerationSourceAccessProvider {
	return &LocalAssetGenerationSourceAccessProvider{keyring: keyring, apiBaseURL: apiBaseURL}
}

func (p *LocalAssetGenerationSourceAccessProvider) CreateSourceImageAccess(ctx context.Context, artifact spatial.SpatialArtifact, ttl time.Duration) (spatial.ReadAccess, error) {
	expiresAt := time.Now().Add(ttl)
	token, err := p.keyring.Mint(artifact.CompanyID, artifact.ID, expiresAt)
	if err != nil {
		return spatial.ReadAccess{}, err
	}
	values := url.Values{}
	values.Set("cap", token)
	return spatial.ReadAccess{
		URL:       fmt.Sprintf("%s/spatial/asset-generation/source-image?%s", p.apiBaseURL, values.Encode()),
		ExpiresAt: expiresAt,
	}, nil
}

// VerifySourceImageAccess recovers and validates the claims embedded in
// a minted capability token — the local content route calls this BEFORE
// any repository/object-store lookup.
func (p *LocalAssetGenerationSourceAccessProvider) VerifySourceImageAccess(ctx context.Context, capability string) (companyID, artifactID string, err error) {
	claims, err := p.keyring.Verify(capability)
	if err != nil {
		return "", "", err
	}
	return claims.CompanyID, claims.ArtifactID, nil
}
