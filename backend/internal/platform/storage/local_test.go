package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/storage"
)

func TestLocalFileStorageSaveAndOpen(t *testing.T) {
	dir := t.TempDir()
	fs := storage.NewLocalFileStorage(dir)

	ctx := context.Background()
	content := []byte("hello quotation pdf")

	key, err := fs.Save(ctx, "quotations/QT-0001.pdf", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}
	if key != "quotations/QT-0001.pdf" {
		t.Fatalf("expected key to be echoed back, got %s", key)
	}

	// File should actually exist on disk under dir.
	if _, err := os.Stat(filepath.Join(dir, "quotations", "QT-0001.pdf")); err != nil {
		t.Fatalf("expected file to exist on disk: %v", err)
	}

	reader, err := fs.Open(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error opening: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error reading: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("expected content %q, got %q", content, got)
	}
}

func TestLocalFileStorageOpenMissingKeyFails(t *testing.T) {
	dir := t.TempDir()
	fs := storage.NewLocalFileStorage(dir)

	_, err := fs.Open(context.Background(), "does/not/exist.pdf")
	if err == nil {
		t.Fatal("expected error opening missing key, got nil")
	}
}
