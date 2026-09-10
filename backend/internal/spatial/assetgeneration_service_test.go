package spatial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
	"time"
)

// --- fakeAssetGenerationObjectStore ---

type fakeAssetGenerationObjectStore struct {
	objects map[string][]byte
}

func newFakeAssetGenerationObjectStore() *fakeAssetGenerationObjectStore {
	return &fakeAssetGenerationObjectStore{objects: map[string][]byte{}}
}

func (s *fakeAssetGenerationObjectStore) Put(_ context.Context, key string, content io.Reader) (int64, error) {
	b, err := io.ReadAll(content)
	if err != nil {
		return 0, err
	}
	s.objects[key] = b
	return int64(len(b)), nil
}

func (s *fakeAssetGenerationObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// --- fakeAssetGenerationSourceAccessProvider ---

type fakeAssetGenerationSourceAccessProvider struct{}

func (fakeAssetGenerationSourceAccessProvider) CreateSourceImageAccess(_ context.Context, artifact SpatialArtifact, ttl time.Duration) (ReadAccess, error) {
	return ReadAccess{URL: "https://example.com/source/" + artifact.ID, ExpiresAt: time.Now().Add(ttl)}, nil
}

// --- test service construction ---

func newTestServiceWithAssetGeneration(t *testing.T) (*Service, *fakeAssetGenerationProvider) {
	t.Helper()
	svc, _, _ := newTestRoomDraftEditService()
	artifacts := newFakeArtifactRepo()
	objectStore := newFakeObjectStore()
	svc.SetArtifactSupport(artifacts, objectStore)

	visualAssetStore := newFakeVisualAssetObjectStore()
	visualAssetRepo := newFakeVisualAssetVersionRepo()
	svc.SetVisualAssetSupport(visualAssetRepo, visualAssetStore)

	jobs := newFakeAssetGenerationJobRepository()
	stagingStore := newFakeAssetGenerationObjectStore()
	provider := &fakeAssetGenerationProvider{}
	sourceAccess := fakeAssetGenerationSourceAccessProvider{}
	svc.SetAssetGenerationSupport(jobs, stagingStore, provider, sourceAccess)

	return svc, provider
}

// seedTestArtifact directly seeds an already-uploaded SpatialArtifact
// record plus its stored bytes — bypassing the full request/put/finalize
// upload flow entirely (which requires a real SpatialCapture and token
// machinery irrelevant to asset-generation tests). validateSourceArtifact
// only cares about the artifact's FINAL state (status/contentType/size/
// decodable bytes), not how it got there.
func seedTestArtifact(t *testing.T, svc *Service, companyID, captureID string, kind ArtifactKind, contentType string) SpatialArtifact {
	t.Helper()
	ctx := context.Background()
	content := testImageBytesFor(contentType)
	sum := sha256.Sum256(content)
	created, err := svc.artifacts.Create(ctx, SpatialArtifact{
		CompanyID: companyID, CaptureID: captureID, Kind: kind,
		ContentType: contentType, DeclaredSize: int64(len(content)),
		Checksum: hex.EncodeToString(sum[:]), Status: ArtifactStatusUploaded,
		ActualSize: int64(len(content)), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("seedTestArtifact Create: %v", err)
	}
	key := objectKeyFor(companyID, captureID, created.ID)
	if _, err := svc.objectStore.Put(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("seedTestArtifact Put: %v", err)
	}
	return created
}

// testImageBytesFor returns a real, decodable minimal image for
// image/jpeg or image/png content types, or arbitrary non-image bytes
// for anything else (e.g. testing rejection of a non-image artifact kind).
func testImageBytesFor(contentType string) []byte {
	switch contentType {
	case "image/png":
		var buf bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		if err := png.Encode(&buf, img); err != nil {
			panic(err)
		}
		return buf.Bytes()
	case "image/jpeg":
		var buf bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		if err := jpeg.Encode(&buf, img, nil); err != nil {
			panic(err)
		}
		return buf.Bytes()
	default:
		return []byte(`{"not":"an image"}`)
	}
}

func TestSubmitAssetGenerationJob_RejectsNonImageArtifactWithoutCallingProvider(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindRoomDraftJSON, "application/json")

	_, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if !errors.Is(err, ErrAssetGenerationSourceInvalid) {
		t.Errorf("expected ErrAssetGenerationSourceInvalid, got %v", err)
	}
	if provider.StartCalls != 0 {
		t.Errorf("expected zero provider calls for a deterministic pre-flight rejection, got %d", provider.StartCalls)
	}
}

func TestSubmitAssetGenerationJob_RejectsCrossTenantArtifact(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_A", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")

	_, err := svc.SubmitAssetGenerationJob(context.Background(), "company_B", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err == nil {
		t.Error("expected an error for a cross-tenant sourceArtifactId")
	}
	if provider.StartCalls != 0 {
		t.Errorf("expected zero provider calls, got %d", provider.StartCalls)
	}
}

func TestSubmitAssetGenerationJob_IdempotentRetryAdoptsExistingJob(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")

	first, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if first.ID != second.ID {
		t.Error("expected the second identical submit to adopt the same job")
	}
	if provider.StartCalls != 0 {
		t.Errorf("expected SubmitAssetGenerationJob to never call the provider directly, got %d calls", provider.StartCalls)
	}
}

// TestSubmitAssetGenerationJob_NotifiesGenerationWakeIncludingOnIdempotentReplay
// proves Gate 4's queue-wake hint fires on every successful submission,
// including a duplicate/idempotent one that adopts the existing job — this
// is intentional, not a bug: an extra wake for an already-claimed or
// already-terminal job costs nothing (Mongo claim-safety fencing, not
// queue delivery, decides every transition; see notifyGenerationWake's own
// doc comment for the same "at-least-once is harmless" reasoning the plan
// itself specifies for Gate 4).
func TestSubmitAssetGenerationJob_NotifiesGenerationWakeIncludingOnIdempotentReplay(t *testing.T) {
	svc, _ := newTestServiceWithAssetGeneration(t)
	wake := &fakeGenerationWakePublisher{}
	svc.SetGenerationWakePublisher(wake)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")

	first, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_wake_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_wake_1", artifact.ID, 42); err != nil {
		t.Fatalf("second submit: %v", err)
	}

	if len(wake.calls) != 2 {
		t.Fatalf("expected two wake calls (one per successful submission), got %+v", wake.calls)
	}
	for _, call := range wake.calls {
		if call.kind != GenerationWakeKindAssetJob || call.id != first.ID {
			t.Fatalf("expected both wake calls to reference asset_job %s, got %+v", first.ID, wake.calls)
		}
	}
}

func TestProcessOneAssetGenerationJob_HappyPathPublishesNewVisualAssetVersion(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_123"}, nil
	}
	validGLB := buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		if ref.ID != "evt_123" {
			t.Errorf("expected to resume the persisted event id, got %q", ref.ID)
		}
		return ProviderGenerationResult{Status: ProviderGenerationCompleted, Output: readCloserFromBytes{bytes.NewReader(validGLB)}}, nil
	}

	processed, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	if !processed {
		t.Fatal("expected a job to have been processed")
	}
	if provider.StartCalls != 1 {
		t.Errorf("expected exactly 1 StartShapeGeneration call, got %d", provider.StartCalls)
	}

	final, err := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if err != nil {
		t.Fatalf("GetAssetGenerationJob: %v", err)
	}
	if final.Execution.Status != JobExecutionCompleted {
		t.Errorf("expected Status=completed, got %q (failureCode=%q)", final.Execution.Status, final.Execution.FailureCode)
	}
	if final.ResultAssetID == "" || final.ResultVersion != 1 {
		t.Errorf("expected a published result asset, got %+v", final)
	}

	published, err := svc.visualAssets.FindByCompanyAssetVersion(context.Background(), "company_1", final.ResultAssetID, 1)
	if err != nil {
		t.Fatalf("expected the result to be a real published VisualAssetVersion: %v", err)
	}
	if published.Format != VisualAssetFormatGLB {
		t.Errorf("expected format glb, got %q", published.Format)
	}
}

func TestProcessOneAssetGenerationJob_ResumePhaseNeverCallsStart(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	svc.assetGenerationLeaseTTL = 5 * time.Millisecond // short-lived for this test only, so worker-0's claim expires fast and worker-1 can legitimately reclaim below
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	_, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Simulate a PRIOR worker ("worker-0") having claimed the job,
	// started the provider, and persisted the event_id before crashing.
	// Because every mutation is now lease-fenced, this must claim under
	// worker-0's name first — a bare direct repository write with no
	// prior claim would be rejected (ErrAssetGenerationJobNotClaimable).
	claimedByPriorWorker, err := svc.assetGenerationJobs.ClaimNext(context.Background(), "worker-0", svc.assetGenerationLeaseTTL, time.Now())
	if err != nil {
		t.Fatalf("simulated prior claim: %v", err)
	}
	if err := svc.assetGenerationJobs.SetProviderStarted(context.Background(), claimedByPriorWorker.ID, "worker-0", time.Now()); err != nil {
		t.Fatalf("SetProviderStarted: %v", err)
	}
	if err := svc.assetGenerationJobs.SetProviderRequestID(context.Background(), claimedByPriorWorker.ID, "worker-0", "evt_already_started"); err != nil {
		t.Fatalf("SetProviderRequestID: %v", err)
	}
	// Wait past worker-0's short lease TTL so it's genuinely expired —
	// worker-1's ClaimNext below then legitimately reclaims the job (a
	// job with ProviderRequestID already set is one of the two SAFELY
	// reclaimable checkpoint states per spec §7/§8's corrected recovery
	// semantics).
	time.Sleep(10 * time.Millisecond)

	validGLB := buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		if ref.ID != "evt_already_started" {
			t.Errorf("expected to resume the PRE-EXISTING event id, got %q", ref.ID)
		}
		return ProviderGenerationResult{Status: ProviderGenerationCompleted, Output: readCloserFromBytes{bytes.NewReader(validGLB)}}, nil
	}
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		t.Fatal("StartShapeGeneration must NEVER be called when ProviderRequestID is already set")
		return ProviderGenerationRef{}, nil
	}

	processed, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	if !processed {
		t.Fatal("expected the job to be processed")
	}
	if provider.StartCalls != 0 {
		t.Errorf("expected 0 StartShapeGeneration calls, got %d", provider.StartCalls)
	}
	if provider.ResumeCalls == 0 {
		t.Error("expected at least 1 ResumeShapeGeneration call")
	}
}

func TestProcessOneAssetGenerationJob_AmbiguousStartFailureNeedsAttentionNeverAutoRetried(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{}, errors.New("connection reset (ambiguous: request may have reached the provider)")
	}

	processed, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	if !processed {
		t.Fatal("expected the job to be processed")
	}

	final, err := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if err != nil {
		t.Fatalf("GetAssetGenerationJob: %v", err)
	}
	if final.Execution.Status != JobExecutionNeedsAttention || final.Execution.FailureCode != "provider_outcome_unknown" {
		t.Errorf("expected needs_attention/provider_outcome_unknown, got status=%q code=%q", final.Execution.Status, final.Execution.FailureCode)
	}

	// A second call must find nothing claimable — the job must NOT be
	// automatically reclaimed and retried.
	processedAgain, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-2")
	if err != nil {
		t.Fatalf("second ProcessOneAssetGenerationJob: %v", err)
	}
	if processedAgain {
		t.Error("expected the needs_attention job to NOT be automatically reclaimed")
	}
	if provider.StartCalls != 1 {
		t.Errorf("expected exactly 1 StartShapeGeneration call total, got %d", provider.StartCalls)
	}
}

func TestProcessOneAssetGenerationJob_QuotaBlockedNeverAutoRetried(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		return ProviderGenerationResult{Status: ProviderGenerationQuotaBlocked, FailureMessage: "ZeroGPU quota exhausted"}, nil
	}

	if _, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	final, _ := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if final.Execution.Status != JobExecutionNeedsAttention || final.Execution.FailureCode != "zero_gpu_quota" {
		t.Errorf("expected needs_attention/zero_gpu_quota, got status=%q code=%q", final.Execution.Status, final.Execution.FailureCode)
	}

	processedAgain, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-2")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if processedAgain {
		t.Error("expected the quota-blocked job to NOT be automatically reclaimed")
	}
}

func TestProcessOneAssetGenerationJob_StopsProcessingWhenLeaseIsLostMidResume(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	svc.assetGenerationHeartbeatInterval = 5 * time.Millisecond
	svc.assetGenerationLeaseTTL = 20 * time.Millisecond
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	stealAttempted := false
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		if !stealAttempted {
			stealAttempted = true
			// Simulate ANOTHER worker stealing the lease mid-generation
			// by directly forcing a competing claim through the
			// repository — the heartbeat's NEXT renewal attempt will
			// now be fenced-rejected.
			if _, err := svc.assetGenerationJobs.ClaimNext(context.Background(), "thief-worker", time.Minute, time.Now().Add(time.Hour)); err != nil {
				t.Fatalf("simulated lease theft via ClaimNext: %v", err)
			}
		}
		time.Sleep(15 * time.Millisecond) // give the heartbeat time to attempt (and fail) a renewal
		return ProviderGenerationResult{Status: ProviderGenerationPending}, nil
	}

	_, err = svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1")
	if err == nil {
		t.Fatal("expected ProcessOneAssetGenerationJob to return an error once its lease was lost mid-processing, not silently succeed")
	}

	final, getErr := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if getErr != nil {
		t.Fatalf("GetAssetGenerationJob: %v", getErr)
	}
	// The job must show THIEF's claim, never a checkpoint worker-1 wrote
	// after losing its lease.
	if final.Execution.LeaseOwner != "thief-worker" {
		t.Errorf("expected the job to remain claimed by thief-worker after worker-1's lease loss, got leaseOwner=%q", final.Execution.LeaseOwner)
	}
}

func TestProcessOneAssetGenerationJob_PendingThenCompletedAfterPolling(t *testing.T) {
	old := assetGenerationResumePollInterval
	assetGenerationResumePollInterval = time.Millisecond
	defer func() { assetGenerationResumePollInterval = old }()

	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	validGLB := buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
	pollCount := 0
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		pollCount++
		if pollCount < 3 {
			return ProviderGenerationResult{Status: ProviderGenerationPending}, nil
		}
		return ProviderGenerationResult{Status: ProviderGenerationCompleted, Output: readCloserFromBytes{bytes.NewReader(validGLB)}}, nil
	}

	if _, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	if pollCount != 3 {
		t.Errorf("expected exactly 3 ResumeShapeGeneration polls, got %d", pollCount)
	}
	if provider.StartCalls != 1 {
		t.Errorf("expected exactly 1 StartShapeGeneration call despite multiple pending polls, got %d", provider.StartCalls)
	}
	final, _ := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if final.Execution.Status != JobExecutionCompleted {
		t.Errorf("expected completed, got %q", final.Execution.Status)
	}
}

// --- caller-deadline vs. provider-rejection distinction (Gate 2 bounded-worker requirement) ---

func TestProcessOneAssetGenerationJob_CallerDeadlineWhilePendingReleasesJobWithoutNeedsAttention(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	// The provider is still legitimately working (pending) — never
	// completes, never errors — the ONLY thing that ends this call is the
	// caller's own bounded context, matching the plan's "child context
	// capped at 45 seconds for ResumeShapeGeneration" instruction.
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		return ProviderGenerationResult{Status: ProviderGenerationPending}, nil
	}

	boundedCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := svc.ProcessOneAssetGenerationJob(boundedCtx, "worker-1"); err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}

	final, err := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if err != nil {
		t.Fatalf("GetAssetGenerationJob: %v", err)
	}
	if final.Execution.Status != JobExecutionProcessing {
		t.Fatalf("expected the job to remain in-progress (processing), never needs_attention, got %q", final.Execution.Status)
	}
	if final.Execution.FailureCode != "" {
		t.Errorf("expected no failure code recorded for an ordinary caller-deadline pause, got %q", final.Execution.FailureCode)
	}
	if provider.StartCalls != 1 {
		t.Errorf("expected exactly 1 StartShapeGeneration call, got %d", provider.StartCalls)
	}

	// The lease must be immediately reclaimable — the next dispatch call
	// (a fresh bounded step, exactly what a subsequent Queue delivery
	// would do) picks the SAME job back up and resumes polling the SAME
	// event id, never calling StartShapeGeneration again.
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		if ref.ID != "evt_1" {
			t.Fatalf("expected the resumed call to reuse the SAME event id, got %q", ref.ID)
		}
		return ProviderGenerationResult{Status: ProviderGenerationCompleted, Output: readCloserFromBytes{bytes.NewReader(buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices()))}}, nil
	}
	if _, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-2"); err != nil {
		t.Fatalf("second ProcessOneAssetGenerationJob: %v", err)
	}
	if provider.StartCalls != 1 {
		t.Errorf("expected StartShapeGeneration to STILL have been called only once after the resumed dispatch, got %d", provider.StartCalls)
	}
	completed, _ := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if completed.Execution.Status != JobExecutionCompleted {
		t.Errorf("expected the resumed dispatch to reach completed, got %q", completed.Execution.Status)
	}
}

func TestProcessOneAssetGenerationJob_GenuineProviderErrorStillNeedsAttention(t *testing.T) {
	svc, provider := newTestServiceWithAssetGeneration(t)
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	job, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_1", artifact.ID, 42)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	// A genuine provider-side failure (e.g. a network error unrelated to
	// ctx cancellation) — ctx itself is never cancelled here, so this must
	// NOT be reinterpreted as a caller-deadline pause.
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		return ProviderGenerationResult{}, errors.New("connection reset by peer")
	}

	if _, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	final, _ := svc.GetAssetGenerationJob(context.Background(), "company_1", job.ID)
	if final.Execution.Status != JobExecutionNeedsAttention {
		t.Fatalf("expected needs_attention for a genuine provider error, got %q", final.Execution.Status)
	}
	if final.Execution.FailureCode != "provider_outcome_unknown" {
		t.Errorf("expected failure code provider_outcome_unknown, got %q", final.Execution.FailureCode)
	}
}
