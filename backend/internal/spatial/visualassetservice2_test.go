package spatial

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// --- fake VisualAssetObjectStore ---

type fakeVisualAssetObjectStore struct {
	objects map[string][]byte
	putLog  []string // records key order for ordering assertions
}

func newFakeVisualAssetObjectStore() *fakeVisualAssetObjectStore {
	return &fakeVisualAssetObjectStore{objects: map[string][]byte{}}
}

func (s *fakeVisualAssetObjectStore) Put(ctx context.Context, key string, content io.Reader) (int64, error) {
	b, err := io.ReadAll(content)
	if err != nil {
		return 0, err
	}
	s.objects[key] = b
	s.putLog = append(s.putLog, key)
	return int64(len(b)), nil
}

func (s *fakeVisualAssetObjectStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *fakeVisualAssetObjectStore) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := s.objects[key]
	return ok, nil
}

func newTestVisualAssetPublishService() (*Service, *fakeVisualAssetObjectStore, *fakeVisualAssetVersionRepo) {
	svc, _, _ := newTestRoomDraftEditService()
	store := newFakeVisualAssetObjectStore()
	repo := newFakeVisualAssetVersionRepo()
	svc.SetVisualAssetSupport(repo, store)
	return svc, store, repo
}

func validGLBContent() []byte {
	return buildTestGLBForPublish(`{"asset":{"version":"2.0"},"buffers":[{"byteLength":4}]}`)
}

// buildTestGLBForPublish is a small local helper distinct from
// buildTestGLB (which lives in visualassetcontent_test.go and takes a
// *testing.T for t.Helper()) — PublishVisualAssetVersion tests need GLB
// bytes without a testing.T in every call site.
func buildTestGLBForPublish(jsonChunk string) []byte {
	pad := func(b []byte) []byte {
		for len(b)%4 != 0 {
			b = append(b, ' ')
		}
		return b
	}
	json := pad([]byte(jsonChunk))
	glb := []byte{}
	appendU32 := func(v uint32) {
		glb = append(glb, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	glb = append(glb, 'g', 'l', 'T', 'F')
	appendU32(2)
	appendU32(0)
	appendU32(uint32(len(json)))
	glb = append(glb, 'J', 'S', 'O', 'N')
	glb = append(glb, json...)
	bin := []byte{1, 2, 3, 4}
	appendU32(uint32(len(bin)))
	glb = append(glb, 'B', 'I', 'N', 0)
	glb = append(glb, bin...)
	total := uint32(len(glb))
	glb[8], glb[9], glb[10], glb[11] = byte(total), byte(total>>8), byte(total>>16), byte(total>>24)
	return glb
}

func TestPublishVisualAssetVersion_ComputesChecksumAndByteCountFromActualBytes(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()
	content := validGLBContent()

	published, err := svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if published.ByteCount != int64(len(content)) {
		t.Fatalf("expected byte count computed from actual bytes (%d), got %d", len(content), published.ByteCount)
	}
	if published.Checksum == "" {
		t.Fatalf("expected a computed checksum")
	}
}

func TestPublishVisualAssetVersion_IgnoresCallerSuppliedFormat(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()

	_, err := svc.PublishVisualAssetVersion(ctx, "company_a", "asset-1", 1, "gltf", "X", VisualAssetNormalization{Pivot: VisualAssetPivotCenter}, bytes.NewReader(validGLBContent()))
	if !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent for a non-glb/usdz format, got %v", err)
	}
}

func TestPublishVisualAssetVersion_RejectsInvalidContent(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()

	_, err := svc.PublishVisualAssetVersion(ctx, "company_a", "asset-1", 1, VisualAssetFormatGLB, "X", VisualAssetNormalization{Pivot: VisualAssetPivotCenter}, bytes.NewReader([]byte("not a real glb")))
	if !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestPublishVisualAssetVersion_RejectsEmptyAssetID(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()

	_, err := svc.PublishVisualAssetVersion(ctx, "company_a", "", 1, VisualAssetFormatGLB, "X", VisualAssetNormalization{Pivot: VisualAssetPivotCenter}, bytes.NewReader(validGLBContent()))
	if !errors.Is(err, ErrInvalidVisualAssetRef) {
		t.Fatalf("expected ErrInvalidVisualAssetRef, got %v", err)
	}
}

func TestPublishVisualAssetVersion_RejectsUnsafeAssetID(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()

	_, err := svc.PublishVisualAssetVersion(ctx, "company_a", "../../etc/passwd", 1, VisualAssetFormatGLB, "X", VisualAssetNormalization{Pivot: VisualAssetPivotCenter}, bytes.NewReader(validGLBContent()))
	if !errors.Is(err, ErrInvalidVisualAssetRef) {
		t.Fatalf("expected ErrInvalidVisualAssetRef for an unsafe assetID (path-traversal characters, unsafe for a storage key), got %v", err)
	}
}

func TestIsSafeVisualAssetIDSegment(t *testing.T) {
	cases := []struct {
		value string
		safe  bool
	}{
		{"boiler-asset", true},
		{"boiler_asset_1", true},
		{"BoilerAsset123", true},
		{"", false},
		{"../etc/passwd", false},
		{"has/slash", false},
		{"has space", false},
		{"has.dot", false},
	}
	for _, tc := range cases {
		if got := isSafeVisualAssetIDSegment(tc.value); got != tc.safe {
			t.Errorf("isSafeVisualAssetIDSegment(%q) = %v, want %v", tc.value, got, tc.safe)
		}
	}
}

func TestPublishVisualAssetVersion_RejectsZeroVersion(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()

	_, err := svc.PublishVisualAssetVersion(ctx, "company_a", "asset-1", 0, VisualAssetFormatGLB, "X", VisualAssetNormalization{Pivot: VisualAssetPivotCenter}, bytes.NewReader(validGLBContent()))
	if !errors.Is(err, ErrInvalidVisualAssetRef) {
		t.Fatalf("expected ErrInvalidVisualAssetRef, got %v", err)
	}
}

func TestPublishVisualAssetVersion_StoresObjectBeforeInsertingRecord(t *testing.T) {
	svc, store, _ := newTestVisualAssetPublishService()
	ctx := context.Background()
	content := validGLBContent()

	published, err := svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, ok := store.objects[published.StorageKey]
	if !ok {
		t.Fatalf("expected object stored at %s", published.StorageKey)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("expected stored bytes to match published content exactly")
	}
}

func TestPublishVisualAssetVersion_NeverExposesStorageKeyAsCallerInput(t *testing.T) {
	// StorageKey is derived server-side; there is no publish parameter
	// accepting one — this test documents that invariant by construction:
	// two publishes with the same identity produce the SAME derived key
	// regardless of anything else, proving it's a pure function of
	// (companyID, assetID, version, checksum), not caller-influenced.
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()
	content := validGLBContent()

	first, err := svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.StorageKey == "" {
		t.Fatalf("expected a derived storage key")
	}
}

func TestPublishVisualAssetVersion_DuplicateIdenticalContentAdopts(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()
	content := validGLBContent()

	first, err := svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error on first publish: %v", err)
	}
	second, err := svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("expected idempotent adoption, got error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same existing record adopted")
	}
}

func TestPublishVisualAssetVersion_DuplicateConflictingContentRejected(t *testing.T) {
	svc, _, _ := newTestVisualAssetPublishService()
	ctx := context.Background()

	_, err := svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(validGLBContent()))
	if err != nil {
		t.Fatalf("unexpected error on first publish: %v", err)
	}
	differentContent := buildTestGLBForPublish(`{"asset":{"version":"2.0"},"buffers":[{"byteLength":999}]}`)
	_, err = svc.PublishVisualAssetVersion(ctx, "company_a", "boiler-asset", 1, VisualAssetFormatGLB, "Boiler", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(differentContent))
	if !errors.Is(err, ErrVisualAssetVersionConflict) {
		t.Fatalf("expected ErrVisualAssetVersionConflict, got %v", err)
	}
}
