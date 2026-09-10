package objectstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/shananth/renovation-platform/backend/internal/platform/objectstore"
)

// setupR2 starts a real MinIO container (an S3-compatible server, same
// protocol surface R2 exposes) and returns a store/presigner pair pointed
// at it — proving R2ObjectStore/R2Presigner's actual HTTP behavior against
// a genuine S3-API server rather than mocking the AWS SDK's internals.
func setupR2(t *testing.T) (*objectstore.R2ObjectStore, *objectstore.R2Presigner) {
	t.Helper()
	ctx := context.Background()

	container, err := minio.Run(ctx, "minio/minio:RELEASE.2024-01-16T16-07-38Z")
	if err != nil {
		t.Fatalf("starting MinIO: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminating MinIO: %v", err)
		}
	})

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("getting MinIO connection string: %v", err)
	}

	bucket := "renovex-tester"
	store := objectstore.NewR2ObjectStore("test-account", "minioadmin", "minioadmin", bucket, "http://"+endpoint)
	presigner := objectstore.NewR2Presigner("test-account", "minioadmin", "minioadmin", bucket, "http://"+endpoint)

	if err := createBucket(ctx, "http://"+endpoint, bucket); err != nil {
		t.Fatalf("creating test bucket: %v", err)
	}

	return store, presigner
}

// createBucket provisions the test bucket via a real signed S3 client call
// — test-only setup (real R2 bucket creation is a one-time manual
// deployment step per the M8.5C plan, never something Go code does at
// runtime).
func createBucket(ctx context.Context, endpoint, bucket string) error {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider("minioadmin", "minioadmin", ""),
		UsePathStyle: true,
	})
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	return err
}

func TestR2ObjectStore_PutGetRoundTrip(t *testing.T) {
	store, _ := setupR2(t)
	ctx := context.Background()
	key := "design-references/company_1/roomdraft_1/session_1/turn_1/attempt_1/reference.jpg"
	content := []byte("fake reference image bytes")

	n, err := store.Put(ctx, key, strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n != int64(len(content)) {
		t.Fatalf("expected %d bytes written, got %d", len(content), n)
	}

	reader, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading content: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected content round-trip, got %q", got)
	}
}

func TestR2ObjectStore_ExistsReflectsPutAndDelete(t *testing.T) {
	store, _ := setupR2(t)
	ctx := context.Background()
	key := "spatial/company_1/capture_1/artifact_1"

	exists, err := store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists (before put): %v", err)
	}
	if exists {
		t.Fatal("expected key to not exist before Put")
	}

	if _, err := store.Put(ctx, key, strings.NewReader("content")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	exists, err = store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists (after put): %v", err)
	}
	if !exists {
		t.Fatal("expected key to exist after Put")
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists (after delete): %v", err)
	}
	if exists {
		t.Fatal("expected key to not exist after Delete")
	}
}

func TestR2ObjectStore_GetMissingKeyReturnsErrObjectNotFound(t *testing.T) {
	store, _ := setupR2(t)
	_, err := store.Get(context.Background(), "does/not/exist")
	if !errors.Is(err, objectstore.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestR2ObjectStore_DeleteMissingKeyIsIdempotent(t *testing.T) {
	store, _ := setupR2(t)
	if err := store.Delete(context.Background(), "does/not/exist"); err != nil {
		t.Fatalf("expected idempotent delete of missing key to succeed, got %v", err)
	}
}

func TestR2ObjectStore_PutOverwritesExistingKey(t *testing.T) {
	store, _ := setupR2(t)
	ctx := context.Background()
	key := "asset-generation/company_1/job_1/raw/checksum.glb"

	if _, err := store.Put(ctx, key, strings.NewReader("original")); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if _, err := store.Put(ctx, key, strings.NewReader("replaced")); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	reader, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if string(got) != "replaced" {
		t.Fatalf("expected overwritten content, got %q", got)
	}
}

func TestR2Presigner_PresignGetURLIsFetchable(t *testing.T) {
	store, presigner := setupR2(t)
	ctx := context.Background()
	key := "visual-assets/company_1/asset_1/v1/checksum.glb"
	content := []byte("glb bytes")

	if _, err := store.Put(ctx, key, strings.NewReader(string(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	url, expiresAt, err := presigner.PresignGet(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty presigned URL")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expected expiry in the future")
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("fetching presigned URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 fetching presigned URL, got %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != string(content) {
		t.Fatalf("expected content via presigned GET, got %q", got)
	}
}

func TestR2Presigner_PresignPutURLAcceptsUploadWithRequiredHeaders(t *testing.T) {
	_, presigner := setupR2(t)
	ctx := context.Background()
	key := "spatial/company_1/capture_1/artifact_2"
	content := []byte("uploaded via presigned put")
	sum := sha256.Sum256(content)
	checksumBase64 := base64.StdEncoding.EncodeToString(sum[:])

	upload, err := presigner.PresignPut(ctx, key, "application/octet-stream", checksumBase64, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if upload.Method != http.MethodPut {
		t.Fatalf("expected PUT method, got %s", upload.Method)
	}
	if upload.RequiredHeaders["Content-Type"] != "application/octet-stream" {
		t.Fatalf("expected Content-Type in required headers, got %+v", upload.RequiredHeaders)
	}

	req, err := http.NewRequest(http.MethodPut, upload.URL, strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("building upload request: %v", err)
	}
	for k, v := range upload.RequiredHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("performing presigned upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 from presigned upload, got %d: %s", resp.StatusCode, body)
	}
}

func TestR2Presigner_HeadReturnsMetadataForExistingKey(t *testing.T) {
	store, presigner := setupR2(t)
	ctx := context.Background()
	key := "spatial/company_1/capture_1/artifact_3"
	content := []byte("some content for head check")

	if _, err := store.Put(ctx, key, strings.NewReader(string(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	result, err := presigner.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if result.SizeBytes != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), result.SizeBytes)
	}
}

func TestR2Presigner_HeadMissingKeyReturnsErrObjectNotFound(t *testing.T) {
	_, presigner := setupR2(t)
	_, err := presigner.Head(context.Background(), "does/not/exist")
	if !errors.Is(err, objectstore.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
}
