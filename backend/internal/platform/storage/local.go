package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalFileStorage implements FileStorage on the local filesystem, rooted
// at a base directory (e.g. ./data/uploads).
type LocalFileStorage struct {
	baseDir string
}

// NewLocalFileStorage constructs a LocalFileStorage rooted at baseDir.
func NewLocalFileStorage(baseDir string) *LocalFileStorage {
	return &LocalFileStorage{baseDir: baseDir}
}

func (s *LocalFileStorage) Save(_ context.Context, key string, content io.Reader) (string, error) {
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(key))

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("storage: failed to create directory: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("storage: failed to create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, content); err != nil {
		return "", fmt.Errorf("storage: failed to write file: %w", err)
	}

	return key, nil
}

func (s *LocalFileStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(key))

	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to open file %q: %w", key, err)
	}
	return f, nil
}
