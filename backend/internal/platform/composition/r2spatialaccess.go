package composition

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// R2VisualAssetReadAccessProvider implements
// spatial.VisualAssetReadAccessProvider by minting a real R2 presigned GET
// URL for assetVersion.StorageKey — the R2-mode counterpart to
// LocalHMACReadAccessProvider. Deliberately does NOT implement
// spatial.VisualAssetLocalCapabilityVerifier: a browser fetching an R2
// presigned URL never round-trips through this Go backend again to be
// verified here at all (RP4D's own documented boundary for exactly this
// kind of provider).
type R2VisualAssetReadAccessProvider struct {
	presigner *objectstore.R2Presigner
}

func NewR2VisualAssetReadAccessProvider(presigner *objectstore.R2Presigner) *R2VisualAssetReadAccessProvider {
	return &R2VisualAssetReadAccessProvider{presigner: presigner}
}

func (p *R2VisualAssetReadAccessProvider) CreateReadAccess(ctx context.Context, assetVersion spatial.VisualAssetVersion, ttl time.Duration) (spatial.ReadAccess, error) {
	url, expiresAt, err := p.presigner.PresignGet(ctx, assetVersion.StorageKey, ttl)
	if err != nil {
		return spatial.ReadAccess{}, err
	}
	return spatial.ReadAccess{URL: url, ExpiresAt: expiresAt}, nil
}

// R2AssetGenerationSourceAccessProvider implements
// spatial.AssetGenerationSourceAccessProvider by minting a real R2
// presigned GET URL for artifact.ObjectKey — the R2-mode counterpart to
// LocalAssetGenerationSourceAccessProvider. Same "no local capability
// verifier" boundary as R2VisualAssetReadAccessProvider above.
type R2AssetGenerationSourceAccessProvider struct {
	presigner *objectstore.R2Presigner
}

func NewR2AssetGenerationSourceAccessProvider(presigner *objectstore.R2Presigner) *R2AssetGenerationSourceAccessProvider {
	return &R2AssetGenerationSourceAccessProvider{presigner: presigner}
}

func (p *R2AssetGenerationSourceAccessProvider) CreateSourceImageAccess(ctx context.Context, artifact spatial.SpatialArtifact, ttl time.Duration) (spatial.ReadAccess, error) {
	url, expiresAt, err := p.presigner.PresignGet(ctx, artifact.ObjectKey, ttl)
	if err != nil {
		return spatial.ReadAccess{}, err
	}
	return spatial.ReadAccess{URL: url, ExpiresAt: expiresAt}, nil
}

// R2DesignReferenceSourceAccessProvider implements
// spatial.DesignReferenceSourceAccessProvider by minting a real R2
// presigned GET URL directly for the given object key — the RP4E2 Gate 2
// counterpart to R2AssetGenerationSourceAccessProvider, needed because a
// design-reference image has no SpatialArtifact record at all (see
// spatial.DesignReferenceSourceAccessProvider's own doc comment). Same "no
// local capability verifier" boundary as its siblings.
type R2DesignReferenceSourceAccessProvider struct {
	presigner *objectstore.R2Presigner
}

func NewR2DesignReferenceSourceAccessProvider(presigner *objectstore.R2Presigner) *R2DesignReferenceSourceAccessProvider {
	return &R2DesignReferenceSourceAccessProvider{presigner: presigner}
}

func (p *R2DesignReferenceSourceAccessProvider) CreateReferenceImageAccess(ctx context.Context, companyID, objectKey string, ttl time.Duration) (spatial.ReadAccess, error) {
	url, expiresAt, err := p.presigner.PresignGet(ctx, objectKey, ttl)
	if err != nil {
		return spatial.ReadAccess{}, err
	}
	return spatial.ReadAccess{URL: url, ExpiresAt: expiresAt}, nil
}
