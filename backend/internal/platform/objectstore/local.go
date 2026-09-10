package objectstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalObjectStore implements ObjectStore on the local filesystem, rooted at
// a base directory (e.g. ./data/spatial-artifacts).
type LocalObjectStore struct {
	baseDir string
}

// NewLocalObjectStore constructs a LocalObjectStore rooted at baseDir.
func NewLocalObjectStore(baseDir string) *LocalObjectStore {
	return &LocalObjectStore{baseDir: baseDir}
}

func (s *LocalObjectStore) resolve(key string) string {
	return filepath.Join(s.baseDir, filepath.FromSlash(key))
}

func (s *LocalObjectStore) Put(_ context.Context, key string, content io.Reader) (int64, error) {
	fullPath := s.resolve(key)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return 0, fmt.Errorf("objectstore: failed to create directory: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return 0, fmt.Errorf("objectstore: failed to create object: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, content)
	if err != nil {
		return 0, fmt.Errorf("objectstore: failed to write object: %w", err)
	}
	return n, nil
}

func (s *LocalObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(s.resolve(key))
	if os.IsNotExist(err) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("objectstore: failed to open object %q: %w", key, err)
	}
	return f, nil
}

func (s *LocalObjectStore) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(s.resolve(key))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("objectstore: failed to stat object %q: %w", key, err)
	}
	return true, nil
}

func (s *LocalObjectStore) Delete(_ context.Context, key string) error {
	err := os.Remove(s.resolve(key))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("objectstore: failed to delete object %q: %w", key, err)
	}
	return nil
}
