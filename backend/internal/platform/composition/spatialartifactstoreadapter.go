package composition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// SpatialArtifactStoreAdapter satisfies spatial.ArtifactObjectStore against a
// concrete objectstore.ObjectStore. It exists because ObjectStore has no
// native checksum operation (S3 ETags are not reliably SHA-256 for
// multipart uploads, so the interface deliberately does not promise one) —
// this adapter computes the checksum by streaming the object once, which is
// acceptable at V1 artifact sizes (design spec §42, maxArtifactSizeBytes).
type SpatialArtifactStoreAdapter struct {
	store objectstore.ObjectStore
}

// NewSpatialArtifactStoreAdapter wraps store.
func NewSpatialArtifactStoreAdapter(store objectstore.ObjectStore) *SpatialArtifactStoreAdapter {
	return &SpatialArtifactStoreAdapter{store: store}
}

func (a *SpatialArtifactStoreAdapter) Put(ctx context.Context, key string, content io.Reader) (int64, error) {
	return a.store.Put(ctx, key, content)
}

func (a *SpatialArtifactStoreAdapter) Exists(ctx context.Context, key string) (bool, error) {
	return a.store.Exists(ctx, key)
}

// Get streams an artifact's stored bytes (RP4E0 — needed to decode/
// validate a source image before spending GPU quota).
func (a *SpatialArtifactStoreAdapter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := a.store.Get(ctx, key)
	if errors.Is(err, objectstore.ErrObjectNotFound) {
		return nil, spatial.ErrArtifactObjectMissing
	}
	return reader, err
}

func (a *SpatialArtifactStoreAdapter) ObjectChecksumAndSize(ctx context.Context, key string) (string, int64, error) {
	reader, err := a.store.Get(ctx, key)
	if errors.Is(err, objectstore.ErrObjectNotFound) {
		return "", 0, spatial.ErrArtifactObjectMissing
	}
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, reader)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
