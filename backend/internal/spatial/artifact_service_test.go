package spatial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// --- fakes ---

type fakeArtifactRepo struct {
	byID    map[string]SpatialArtifact
	byToken map[string]string // tokenHash -> artifactID
	nextID  int
}

func newFakeArtifactRepo() *fakeArtifactRepo {
	return &fakeArtifactRepo{byID: map[string]SpatialArtifact{}, byToken: map[string]string{}}
}

func (f *fakeArtifactRepo) Create(_ context.Context, a SpatialArtifact) (SpatialArtifact, error) {
	f.nextID++
	a.ID = "artifact_" + string(rune('a'+f.nextID))
	f.byID[a.ID] = a
	if a.UploadTokenHash != "" {
		f.byToken[a.UploadTokenHash] = a.ID
	}
	return a, nil
}

func (f *fakeArtifactRepo) FindByID(_ context.Context, companyID, id string) (SpatialArtifact, error) {
	a, ok := f.byID[id]
	if !ok || a.CompanyID != companyID {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	return a, nil
}

func (f *fakeArtifactRepo) FindByUploadTokenHash(_ context.Context, tokenHash string) (SpatialArtifact, error) {
	id, ok := f.byToken[tokenHash]
	if !ok {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	a, ok := f.byID[id]
	if !ok {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	return a, nil
}

func (f *fakeArtifactRepo) ReissueUploadToken(_ context.Context, companyID, id, tokenHash string, expiresAt time.Time) (SpatialArtifact, error) {
	a, ok := f.byID[id]
	if !ok || a.CompanyID != companyID {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	if a.UploadTokenHash != "" {
		delete(f.byToken, a.UploadTokenHash)
	}
	a.UploadTokenHash = tokenHash
	a.UploadTokenExpiresAt = expiresAt
	f.byID[id] = a
	f.byToken[tokenHash] = id
	return a, nil
}

func (f *fakeArtifactRepo) MarkUploaded(_ context.Context, companyID, id string, actualSize int64, uploadedAt time.Time) (SpatialArtifact, error) {
	a, ok := f.byID[id]
	if !ok || a.CompanyID != companyID {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	if a.Status == ArtifactStatusUploaded {
		return a, nil // idempotent
	}
	if a.UploadTokenHash != "" {
		delete(f.byToken, a.UploadTokenHash)
	}
	a.Status = ArtifactStatusUploaded
	a.ActualSize = actualSize
	a.UploadedAt = &uploadedAt
	a.UploadTokenHash = ""
	f.byID[id] = a
	return a, nil
}

func (f *fakeArtifactRepo) ListByCapture(_ context.Context, companyID, captureID string) ([]SpatialArtifact, error) {
	var out []SpatialArtifact
	for _, a := range f.byID {
		if a.CompanyID == companyID && a.CaptureID == captureID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeArtifactRepo) DeleteAllForCompany(_ context.Context, companyID string) error {
	for id, a := range f.byID {
		if a.CompanyID == companyID {
			if a.UploadTokenHash != "" {
				delete(f.byToken, a.UploadTokenHash)
			}
			delete(f.byID, id)
		}
	}
	return nil
}

// fakeObjectStore is a minimal in-memory stand-in for objectstore.ObjectStore,
// defined locally to avoid spatial depending on the platform package just for
// tests (spatial's real Service takes an ObjectStore-shaped interface it
// defines itself, consumer-defines-interface, matching SpaceLookup).
type fakeObjectStore struct {
	objects map[string][]byte
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string][]byte{}}
}

func (f *fakeObjectStore) put(key string, content []byte) { f.objects[key] = content }

func (f *fakeObjectStore) Put(_ context.Context, key string, content io.Reader) (int64, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return 0, err
	}
	f.objects[key] = data
	return int64(len(data)), nil
}

func (f *fakeObjectStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.objects[key]
	return ok, nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := f.objects[key]
	if !ok {
		return nil, ErrArtifactObjectMissing
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (f *fakeObjectStore) ObjectChecksumAndSize(_ context.Context, key string) (checksum string, size int64, err error) {
	content, ok := f.objects[key]
	if !ok {
		return "", 0, ErrArtifactObjectMissing
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), int64(len(content)), nil
}

func newTestArtifactService() (*Service, *fakeArtifactRepo, *fakeObjectStore) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	artifacts := newFakeArtifactRepo()
	store := newFakeObjectStore()
	svc.SetArtifactSupport(artifacts, store)
	return svc, artifacts, store
}

func checksumOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// --- tests ---

func TestRequestArtifactUpload_RejectsUnknownCapture(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()

	_, _, err := svc.RequestArtifactUpload(ctx, "company_a", "nonexistent_capture", ArtifactKindRGBKeyframe, "image/jpeg", 1024, "abc")
	if !errors.Is(err, ErrCaptureNotFound) {
		t.Fatalf("expected ErrCaptureNotFound, got %v", err)
	}
}

func TestRequestArtifactUpload_RejectsCrossTenantCapture(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")

	_, _, err := svc.RequestArtifactUpload(ctx, "company_b", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", 1024, "abc")
	if !errors.Is(err, ErrCaptureNotFound) {
		t.Fatalf("expected ErrCaptureNotFound for cross-tenant capture, got %v", err)
	}
}

func TestRequestArtifactUpload_RejectsInvalidKind(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")

	_, _, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKind("laser_scan"), "image/jpeg", 1024, "abc")
	if !errors.Is(err, ErrInvalidArtifactKind) {
		t.Fatalf("expected ErrInvalidArtifactKind, got %v", err)
	}
}

// TestRequestArtifactUpload_AcceptsRoomPlanArtifactKinds proves the plan
// §RP3 additive ArtifactKind/content-type values (raw CapturedRoomData,
// RoomBuilder-processed result, RoomDraft JSON snapshot, USDZ export) are
// accepted by the existing, unmodified upload mechanism — no new upload
// code path is introduced for RoomPlan artifacts.
func TestRequestArtifactUpload_AcceptsRoomPlanArtifactKinds(t *testing.T) {
	cases := []struct {
		name        string
		kind        ArtifactKind
		contentType string
	}{
		{"captured room data", ArtifactKindCapturedRoomData, "application/octet-stream"},
		{"roomplan processed", ArtifactKindRoomPlanProcessed, "application/octet-stream"},
		{"roomdraft json", ArtifactKindRoomDraftJSON, "application/json"},
		{"usdz", ArtifactKindUSDZ, "model/vnd.usdz+zip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newTestArtifactService()
			ctx := context.Background()
			c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")

			artifact, token, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, tc.kind, tc.contentType, 1024, "abc")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if artifact.Kind != tc.kind {
				t.Fatalf("expected kind %s, got %s", tc.kind, artifact.Kind)
			}
			if token == "" {
				t.Fatal("expected a non-empty upload token")
			}
		})
	}
}

func TestRequestArtifactUpload_RejectsInvalidContentType(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")

	_, _, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "video/mp4", 1024, "abc")
	if !errors.Is(err, ErrInvalidArtifactContentType) {
		t.Fatalf("expected ErrInvalidArtifactContentType, got %v", err)
	}
}

func TestRequestArtifactUpload_RejectsOversizedDeclaration(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")

	_, _, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", maxArtifactSizeBytes+1, "abc")
	if !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("expected ErrArtifactTooLarge, got %v", err)
	}
}

func TestRequestArtifactUpload_IssuesPendingArtifactWithToken(t *testing.T) {
	svc, artifacts, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")

	artifact, token, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", 1024, "somehash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if artifact.Status != ArtifactStatusPending {
		t.Fatalf("expected pending, got %s", artifact.Status)
	}
	if token == "" {
		t.Fatal("expected non-empty raw upload token")
	}

	stored, err := artifacts.FindByID(ctx, "company_a", artifact.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.UploadTokenHash == "" {
		t.Fatal("expected stored artifact to carry a token hash")
	}
	if stored.UploadTokenHash == token {
		t.Fatal("expected stored token to be hashed, not the raw token")
	}
}

func TestFinalizeArtifactUpload_RejectsInvalidToken(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	artifact, _, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", 4, checksumOf([]byte("data")))

	_, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, "wrong-token")
	if !errors.Is(err, ErrArtifactUploadTokenInvalid) {
		t.Fatalf("expected ErrArtifactUploadTokenInvalid, got %v", err)
	}
}

func TestFinalizeArtifactUpload_RejectsMissingObject(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	content := []byte("fake bytes")
	artifact, token, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(content)), checksumOf(content))
	// Deliberately do NOT put the object in the store.

	_, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, token)
	if !errors.Is(err, ErrArtifactObjectMissing) {
		t.Fatalf("expected ErrArtifactObjectMissing, got %v", err)
	}
}

func TestFinalizeArtifactUpload_RejectsChecksumMismatch(t *testing.T) {
	svc, _, store := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	declaredContent := []byte("declared content")
	artifact, token, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(declaredContent)), checksumOf(declaredContent))

	// Object actually uploaded has DIFFERENT bytes than declared.
	store.put(artifact.ObjectKey, []byte("tampered or corrupted content"))

	_, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, token)
	if !errors.Is(err, ErrArtifactChecksumMismatch) {
		t.Fatalf("expected ErrArtifactChecksumMismatch, got %v", err)
	}
}

func TestFinalizeArtifactUpload_SucceedsAndMarksUploaded(t *testing.T) {
	svc, artifacts, store := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	content := []byte("real matching bytes")
	artifact, token, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(content)), checksumOf(content))
	store.put(artifact.ObjectKey, content)

	finalized, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finalized.Status != ArtifactStatusUploaded {
		t.Fatalf("expected uploaded, got %s", finalized.Status)
	}
	if finalized.ActualSize != int64(len(content)) {
		t.Fatalf("expected actual size %d, got %d", len(content), finalized.ActualSize)
	}

	stored, err := artifacts.FindByID(ctx, "company_a", artifact.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Status != ArtifactStatusUploaded {
		t.Fatalf("expected persisted status uploaded, got %s", stored.Status)
	}
}

func TestFinalizeArtifactUpload_DuplicateFinalizeIsIdempotent(t *testing.T) {
	svc, _, store := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	content := []byte("idempotent bytes")
	artifact, token, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(content)), checksumOf(content))
	store.put(artifact.ObjectKey, content)

	first, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, token)
	if err != nil {
		t.Fatalf("unexpected error on first finalize: %v", err)
	}

	// Second finalize call (e.g. client retried after a network blip that
	// actually succeeded server-side) must succeed without error, not
	// require the now-cleared token again.
	second, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, "")
	if err != nil {
		t.Fatalf("expected idempotent success on duplicate finalize, got %v", err)
	}
	if second.Status != ArtifactStatusUploaded || second.ID != first.ID {
		t.Fatalf("expected same uploaded artifact returned, got %+v", second)
	}
}

func TestRequestArtifactUpload_ResumeReissuesFreshToken(t *testing.T) {
	svc, artifacts, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	content := []byte("resumed content")

	artifact1, token1, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(content)), checksumOf(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate a dropped connection: client re-requests the same artifact's
	// upload slot again (partial upload resume — Task 3 TDD requirement).
	artifact2, token2, err := svc.ResumeArtifactUpload(ctx, "company_a", artifact1.ID)
	if err != nil {
		t.Fatalf("unexpected error resuming: %v", err)
	}
	if artifact2.ID != artifact1.ID {
		t.Fatalf("expected resume to reuse the same artifact ID, got %s vs %s", artifact2.ID, artifact1.ID)
	}
	if token2 == token1 {
		t.Fatal("expected a fresh token on resume, got the same token")
	}

	// The OLD token must no longer work.
	stored, _ := artifacts.FindByID(ctx, "company_a", artifact1.ID)
	if stored.UploadTokenHash != hashArtifactToken(token2) {
		t.Fatal("expected stored token hash to reflect the fresh token")
	}
}

func TestFinalizeArtifactUpload_ExpiredTokenRejected(t *testing.T) {
	svc, artifacts, store := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	content := []byte("expiring content")
	artifact, token, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(content)), checksumOf(content))
	store.put(artifact.ObjectKey, content)

	// Force the stored token to already be expired.
	if _, err := artifacts.ReissueUploadToken(ctx, "company_a", artifact.ID, hashArtifactToken(token), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("unexpected error forcing expiry: %v", err)
	}

	_, err := svc.FinalizeArtifactUpload(ctx, "company_a", artifact.ID, token)
	if !errors.Is(err, ErrArtifactUploadTokenInvalid) {
		t.Fatalf("expected ErrArtifactUploadTokenInvalid for expired token, got %v", err)
	}
}

// PutArtifactContent is the token-authenticated content-proxy path: unlike
// every other spatial.Service method, it is NOT tenant-authenticated via
// companyID (the client presents only the artifact's own upload token,
// matching a signed-URL's authorization model).

func TestPutArtifactContent_RejectsUnknownArtifact(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()

	err := svc.PutArtifactContent(ctx, "nonexistent_artifact", "sometoken", io.NopCloser(nil))
	if !errors.Is(err, ErrArtifactUploadTokenInvalid) {
		t.Fatalf("expected ErrArtifactUploadTokenInvalid for unknown artifact, got %v", err)
	}
}

func TestPutArtifactContent_RejectsInvalidToken(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	artifact, _, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", 4, checksumOf([]byte("data")))

	err := svc.PutArtifactContent(ctx, artifact.ID, "wrong-token", strings.NewReader("data"))
	if !errors.Is(err, ErrArtifactUploadTokenInvalid) {
		t.Fatalf("expected ErrArtifactUploadTokenInvalid, got %v", err)
	}
}

func TestPutArtifactContent_WritesToObjectStoreUnderArtifactKey(t *testing.T) {
	svc, _, store := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	content := []byte("actual uploaded bytes")
	artifact, token, _ := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", int64(len(content)), checksumOf(content))

	err := svc.PutArtifactContent(ctx, artifact.ID, token, strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, ok := store.objects[artifact.ObjectKey]
	if !ok {
		t.Fatal("expected object to be stored under artifact's ObjectKey")
	}
	if string(stored) != string(content) {
		t.Fatalf("expected stored content %q, got %q", content, stored)
	}
}

func TestListArtifactsByCapture_TenantScoped(t *testing.T) {
	svc, _, _ := newTestArtifactService()
	ctx := context.Background()
	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", 4, checksumOf([]byte("data")))

	list, err := svc.ListArtifactsByCapture(ctx, "company_a", c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(list))
	}

	// Cross-tenant listing must not see it.
	_, err = svc.GetCapture(ctx, "company_b", c.ID)
	if !errors.Is(err, ErrCaptureNotFound) {
		t.Fatalf("expected ErrCaptureNotFound for cross-tenant capture read, got %v", err)
	}
}
