package objectstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
)

func TestLocalObjectStorePutAndGet(t *testing.T) {
	dir := t.TempDir()
	store := objectstore.NewLocalObjectStore(dir)
	ctx := context.Background()
	content := []byte("fake rgb keyframe bytes")

	n, err := store.Put(ctx, "spatial/company_a/capture_1/keyframe_001.jpg", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error putting: %v", err)
	}
	if n != int64(len(content)) {
		t.Fatalf("expected %d bytes written, got %d", len(content), n)
	}

	if _, err := os.Stat(filepath.Join(dir, "spatial", "company_a", "capture_1", "keyframe_001.jpg")); err != nil {
		t.Fatalf("expected file to exist on disk: %v", err)
	}

	reader, err := store.Get(ctx, "spatial/company_a/capture_1/keyframe_001.jpg")
	if err != nil {
		t.Fatalf("unexpected error getting: %v", err)
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

func TestLocalObjectStoreGetMissingKeyReturnsErrObjectNotFound(t *testing.T) {
	dir := t.TempDir()
	store := objectstore.NewLocalObjectStore(dir)

	_, err := store.Get(context.Background(), "does/not/exist.jpg")
	if !errors.Is(err, objectstore.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestLocalObjectStoreExists(t *testing.T) {
	dir := t.TempDir()
	store := objectstore.NewLocalObjectStore(dir)
	ctx := context.Background()

	exists, err := store.Exists(ctx, "spatial/x.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false before Put")
	}

	if _, err := store.Put(ctx, "spatial/x.jpg", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("unexpected error putting: %v", err)
	}

	exists, err = store.Exists(ctx, "spatial/x.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true after Put")
	}
}

func TestLocalObjectStoreDelete(t *testing.T) {
	dir := t.TempDir()
	store := objectstore.NewLocalObjectStore(dir)
	ctx := context.Background()

	if _, err := store.Put(ctx, "spatial/x.jpg", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("unexpected error putting: %v", err)
	}
	if err := store.Delete(ctx, "spatial/x.jpg"); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	exists, err := store.Exists(ctx, "spatial/x.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false after Delete")
	}
}

func TestLocalObjectStoreDeleteMissingKeyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	store := objectstore.NewLocalObjectStore(dir)

	if err := store.Delete(context.Background(), "does/not/exist.jpg"); err != nil {
		t.Fatalf("expected no-op success deleting missing key, got %v", err)
	}
}

func TestLocalObjectStorePutOverwritesExistingKey(t *testing.T) {
	dir := t.TempDir()
	store := objectstore.NewLocalObjectStore(dir)
	ctx := context.Background()

	if _, err := store.Put(ctx, "spatial/x.jpg", bytes.NewReader([]byte("first"))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := store.Put(ctx, "spatial/x.jpg", bytes.NewReader([]byte("second-longer"))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader, err := store.Get(ctx, "spatial/x.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if string(got) != "second-longer" {
		t.Fatalf("expected overwritten content, got %q", got)
	}
}
