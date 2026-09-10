// Package storage defines the FileStorage abstraction used for documents
// (Quotation PDFs, RFQ PDFs, project photos, etc.). Phase 1 uses a local
// filesystem implementation; a future S3-backed implementation can satisfy
// the same interface without changes to calling domain modules.
package storage

import (
	"context"
	"io"
)

// FileStorage saves and retrieves opaque file content addressed by a
// caller-chosen key (e.g. "quotations/QT-0001.pdf").
type FileStorage interface {
	// Save writes content under key, creating any intermediate structure
	// as needed, and returns the key it was stored under.
	Save(ctx context.Context, key string, content io.Reader) (string, error)

	// Open returns a reader for the content stored under key. Callers must
	// close the returned reader. Returns an error if key does not exist.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}
