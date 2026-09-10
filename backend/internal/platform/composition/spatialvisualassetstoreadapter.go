package composition

import (
	"context"
	"io"

	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
)

// SpatialVisualAssetStoreAdapter satisfies spatial.VisualAssetObjectStore
// against a concrete objectstore.ObjectStore — a straight pass-through,
// unlike SpatialArtifactStoreAdapter, since VisualAssetVersion computes its
// own checksum server-side at publish time (RP4D §4) rather than relying
// on a post-hoc object-store checksum read.
type SpatialVisualAssetStoreAdapter struct {
	store objectstore.ObjectStore
}

// NewSpatialVisualAssetStoreAdapter wraps store.
func NewSpatialVisualAssetStoreAdapter(store objectstore.ObjectStore) *SpatialVisualAssetStoreAdapter {
	return &SpatialVisualAssetStoreAdapter{store: store}
}

func (a *SpatialVisualAssetStoreAdapter) Put(ctx context.Context, key string, content io.Reader) (int64, error) {
	return a.store.Put(ctx, key, content)
}

func (a *SpatialVisualAssetStoreAdapter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return a.store.Get(ctx, key)
}

func (a *SpatialVisualAssetStoreAdapter) Exists(ctx context.Context, key string) (bool, error) {
	return a.store.Exists(ctx, key)
}
