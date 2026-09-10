package composition

import (
	"context"
	"io"

	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
)

// SpatialDesignReferenceStoreAdapter satisfies
// spatial.DesignReferenceImageObjectStore against a concrete
// objectstore.ObjectStore — a straight pass-through, mirroring
// SpatialAssetGenerationStoreAdapter's exact shape (RP4E2 Gate 2). Only
// Put is needed: the design-generation worker never reads a reference
// image back through Go (DesignReferenceSourceAccessProvider mints a
// direct URL for Hunyuan to fetch instead).
type SpatialDesignReferenceStoreAdapter struct {
	store objectstore.ObjectStore
}

func NewSpatialDesignReferenceStoreAdapter(store objectstore.ObjectStore) *SpatialDesignReferenceStoreAdapter {
	return &SpatialDesignReferenceStoreAdapter{store: store}
}

func (a *SpatialDesignReferenceStoreAdapter) Put(ctx context.Context, key string, content io.Reader) (int64, error) {
	return a.store.Put(ctx, key, content)
}
