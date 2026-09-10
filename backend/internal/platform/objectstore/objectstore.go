// Package objectstore defines the ObjectStore abstraction used for large
// binary spatial artifacts (RGB keyframes, depth maps, observation photos,
// reconstruction meshes, textures). It is distinct from platform/storage
// (Quotation/RFQ PDFs): spatial artifacts need per-object content addressing
// with a caller-supplied key, existence checks, and deletion, none of which
// storage.FileStorage's Save/Open pair exposes. Phase 1 uses a local
// filesystem implementation; a future S3-backed implementation can satisfy
// the same interface without changes to calling domain modules (design spec
// §11: "Development -> local filesystem or MinIO, Production -> S3").
package objectstore

import (
	"context"
	"errors"
	"io"
)

// ErrObjectNotFound is returned when Get or Delete references a key with no
// stored object.
var ErrObjectNotFound = errors.New("objectstore: object not found")

// ObjectStore saves and retrieves opaque binary content addressed by a
// caller-chosen key (e.g. "spatial/{companyId}/{captureId}/{artifactId}").
// Buckets/objects remain private — ObjectStore itself never returns a public
// URL; short-lived signed access is a domain-owned concern layered on top
// (design spec §11: "Android and other clients receive short-lived signed
// URLs only after Go authorization").
type ObjectStore interface {
	// Put writes content under key, creating any intermediate structure as
	// needed, and returns the exact byte count written. Overwriting an
	// existing key is allowed (idempotent re-upload of the same artifact).
	Put(ctx context.Context, key string, content io.Reader) (int64, error)

	// Get returns a reader for the content stored under key. Callers must
	// close the returned reader. Returns ErrObjectNotFound if key does not
	// exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Exists reports whether key has stored content, without transferring
	// it — used to verify a client-reported upload actually landed before
	// finalizing artifact metadata.
	Exists(ctx context.Context, key string) (bool, error)

	// Delete removes key's content. Deleting a key that does not exist is
	// not an error (idempotent).
	Delete(ctx context.Context, key string) error
}
