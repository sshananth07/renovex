package spatial

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"testing"
	"time"
)

// --- fakes: ReferenceImageGenerator / DesignReferenceImageObjectStore / DesignReferenceSourceAccessProvider ---

func validJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode fixture jpeg: %v", err)
	}
	return buf.Bytes()
}

type fakeReferenceImageGenerator struct {
	imageBytes []byte
	err        error
	calls      int
	lastReq    ReferenceImageGenerationRequest
}

func (f *fakeReferenceImageGenerator) GenerateReference(_ context.Context, req ReferenceImageGenerationRequest) (ReferenceImageGenerationResult, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return ReferenceImageGenerationResult{}, f.err
	}
	return ReferenceImageGenerationResult{
		ImageBytes: f.imageBytes, ContentType: "image/jpeg", Provider: "mock", Model: "mock-v1",
		ProviderRequestID: "provider_req_1", Seed: req.Seed, PromptVersion: req.PromptVersion,
	}, nil
}

type fakeDesignReferenceImageObjectStore struct {
	objects map[string][]byte
	putErr  error
}

func newFakeDesignReferenceImageObjectStore() *fakeDesignReferenceImageObjectStore {
	return &fakeDesignReferenceImageObjectStore{objects: map[string][]byte{}}
}

func (f *fakeDesignReferenceImageObjectStore) Put(_ context.Context, key string, content io.Reader) (int64, error) {
	if f.putErr != nil {
		return 0, f.putErr
	}
	b, err := io.ReadAll(content)
	if err != nil {
		return 0, err
	}
	f.objects[key] = b
	return int64(len(b)), nil
}

type fakeDesignReferenceSourceAccessProvider struct{}

func (fakeDesignReferenceSourceAccessProvider) CreateReferenceImageAccess(_ context.Context, companyID, objectKey string, ttl time.Duration) (ReadAccess, error) {
	return ReadAccess{URL: "https://example.com/reference/" + objectKey, ExpiresAt: time.Now().Add(ttl)}, nil
}

// --- test service construction ---

// newTestServiceWithDesignGenerationWorker wires a full Service with both
// RP4E2's public design-generation support (Gate 1) AND Gate 2's worker
// support AND RP4E0 asset-generation support — everything
// ProcessOneDesignGenerationAttempt's full happy path touches.
func newTestServiceWithDesignGenerationWorker(t *testing.T) (svc *Service, designRepo *fakeDesignSessionAndTurnRepo, roomDrafts *fakeRoomDraftRepoForDesign, generationRepo *fakeDesignGenerationRepo, refGen *fakeReferenceImageGenerator, refStore *fakeDesignReferenceImageObjectStore, assetJobs *fakeAssetGenerationJobRepository) {
	t.Helper()
	roomDrafts = newFakeRoomDraftRepoForDesign()
	designRepo = newFakeDesignRepo()
	generationRepo = newFakeDesignGenerationRepo(designRepo, roomDrafts)

	svc = &Service{}
	svc.SetRoomDraftSupport(roomDrafts)
	svc.SetDesignPlanningSupport(designRepo, designRepo, &fakeElementReasoner{})
	svc.SetDesignGenerationSupport(generationRepo, generationRepo, generationRepo)
	// RP4E0's own validateAndPublishAssetGeneration path (reached once a
	// linked job's provider call completes) calls PublishVisualAssetVersion,
	// which needs visual-asset support wired — without it a completed job
	// panics on a nil visualAssets repo, same as any other RP4E0 test that
	// exercises the full happy path.
	svc.SetVisualAssetSupport(newFakeVisualAssetVersionRepo(), newFakeVisualAssetObjectStore())
	// Artifact support is only needed by the "unlinked job" reconciliation
	// test (which submits through RP4E0's own original public route via
	// seedTestArtifact) — harmless to wire unconditionally here.
	svc.SetArtifactSupport(newFakeArtifactRepo(), newFakeObjectStore())

	assetJobs = newFakeAssetGenerationJobRepository()
	stagingStore := newFakeAssetGenerationObjectStore()
	provider := &fakeAssetGenerationProvider{}
	svc.SetAssetGenerationSupport(assetJobs, stagingStore, provider, fakeAssetGenerationSourceAccessProvider{})

	refGen = &fakeReferenceImageGenerator{imageBytes: validJPEGBytes(t)}
	refStore = newFakeDesignReferenceImageObjectStore()
	svc.SetDesignGenerationWorkerSupport(refGen, refStore, fakeDesignReferenceSourceAccessProvider{}, 0)

	return
}

// confirmedGeometryAttempt drives a real geometry Confirm through the
// service so the resulting attempt is a genuine `reserved` record with a
// real turn/plan behind it — never hand-constructed, matching this file's
// sibling test files' own "exercise the real path" convention.
func confirmedGeometryAttempt(t *testing.T, svc *Service, designRepo *fakeDesignSessionAndTurnRepo, roomDrafts *fakeRoomDraftRepoForDesign) DesignGenerationAttempt {
	t.Helper()
	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())
	attempt, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_worker_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("ConfirmDesignPlan: %v", err)
	}
	if attempt.Status != DesignGenerationStatusReserved {
		t.Fatalf("expected a geometry attempt to stay reserved after Confirm, got %q", attempt.Status)
	}
	return attempt
}

// --- ProcessOneDesignGenerationAttempt ---

func TestProcessOneDesignGenerationAttempt_IdleWhenNothingClaimable(t *testing.T) {
	svc, _, _, _, _, _, _ := newTestServiceWithDesignGenerationWorker(t)
	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if claimed {
		t.Fatal("expected no attempt to be claimable")
	}
}

func TestProcessOneDesignGenerationAttempt_ReturnsNotConfigured(t *testing.T) {
	svc := &Service{}
	svc.SetRoomDraftSupport(newFakeRoomDraftRepoForDesign())
	_, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if !errors.Is(err, ErrDesignGenerationSupportNotConfigured) {
		t.Fatalf("expected ErrDesignGenerationSupportNotConfigured, got %v", err)
	}
}

func TestProcessOneDesignGenerationAttempt_ReturnsWorkerNotConfigured(t *testing.T) {
	roomDrafts := newFakeRoomDraftRepoForDesign()
	designRepo := newFakeDesignRepo()
	generationRepo := newFakeDesignGenerationRepo(designRepo, roomDrafts)
	svc := newDesignGenerationServiceForTest(RoomDraft{}, roomDrafts, designRepo, generationRepo)
	// Deliberately never calls SetDesignGenerationWorkerSupport — the
	// public routes work fine (Gate 1) but the worker must refuse cleanly.
	_, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if !errors.Is(err, ErrDesignGenerationWorkerNotConfigured) {
		t.Fatalf("expected ErrDesignGenerationWorkerNotConfigured, got %v", err)
	}
}

func TestProcessOneDesignGenerationAttempt_HappyPathReachesAssetGenerationPending(t *testing.T) {
	svc, designRepo, roomDrafts, generationRepo, refGen, refStore, assetJobs := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)

	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if !claimed {
		t.Fatal("expected the reserved geometry attempt to be claimed")
	}

	updated := generationRepo.attempts[attempt.ID]
	if updated.Status != DesignGenerationStatusAssetGenerationPending {
		t.Fatalf("expected asset_generation_pending, got %q", updated.Status)
	}
	if updated.ReferenceProviderStartedAt == nil {
		t.Fatal("expected ReferenceProviderStartedAt to be durably set")
	}
	if updated.ReferenceImage == nil || updated.ReferenceImage.ObjectKey == "" {
		t.Fatal("expected a reference image to be persisted")
	}
	if updated.AssetGenerationJobID == "" {
		t.Fatal("expected an asset generation job to be linked")
	}

	if refGen.calls != 1 {
		t.Fatalf("expected exactly one GenerateReference call, got %d", refGen.calls)
	}
	if refGen.lastReq.Target.ID != "object_sofa_123" {
		t.Errorf("expected the reference request to target the attempt's own target, got %q", refGen.lastReq.Target.ID)
	}
	if _, ok := refStore.objects[updated.ReferenceImage.ObjectKey]; !ok {
		t.Fatal("expected the reference image bytes to actually be stored")
	}

	job, err := assetJobs.FindByID(context.Background(), "company_1", updated.AssetGenerationJobID)
	if err != nil {
		t.Fatalf("FindByID for linked asset generation job: %v", err)
	}
	if job.Source.Kind != AssetGenerationSourceKindDesignReference {
		t.Errorf("expected the linked job's source kind to be design_reference, got %q", job.Source.Kind)
	}
	if job.Source.DesignAttemptID != attempt.ID {
		t.Errorf("expected the linked job's source to reference this attempt, got %q", job.Source.DesignAttemptID)
	}
}

func TestProcessOneDesignGenerationAttempt_NeverClaimsTheSameAttemptTwiceInARow(t *testing.T) {
	svc, designRepo, roomDrafts, _, _, _, _ := newTestServiceWithDesignGenerationWorker(t)
	confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)

	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil || !claimed {
		t.Fatalf("first process: claimed=%v err=%v", claimed, err)
	}

	// The attempt reached a terminal-for-this-worker status
	// (asset_generation_pending) — a second call must find nothing left to
	// claim, never re-run reference generation for the same attempt.
	claimed, err = svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-2")
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if claimed {
		t.Fatal("expected nothing claimable on the second call")
	}
}

func TestProcessOneDesignGenerationAttempt_ReferenceGeneratorFailureMarksNeedsAttentionAndNeverRetriesTheProviderCall(t *testing.T) {
	svc, designRepo, roomDrafts, generationRepo, refGen, _, _ := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)
	refGen.err = errors.New("provider unreachable")

	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if !claimed {
		t.Fatal("expected the attempt to be claimed even though the provider call then fails")
	}

	updated := generationRepo.attempts[attempt.ID]
	if updated.Status != DesignGenerationStatusNeedsAttention {
		t.Fatalf("expected needs_attention after an ambiguous provider failure, got %q", updated.Status)
	}
	if updated.ReferenceProviderStartedAt == nil {
		t.Fatal("expected ReferenceProviderStartedAt to remain durably set even on failure — the call was actually attempted")
	}

	// A needs_attention attempt must never be reclaimed automatically —
	// mirrors SpatialAssetGenerationJob's identical dangerous-state
	// exclusion.
	claimed, err = svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-2")
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if claimed {
		t.Fatal("expected the needs_attention attempt to never be auto-reclaimed")
	}
	if refGen.calls != 1 {
		t.Fatalf("expected GenerateReference to have been called exactly once total, got %d", refGen.calls)
	}
}

func TestProcessOneDesignGenerationAttempt_InvalidReferenceImageMarksNeedsAttention(t *testing.T) {
	svc, designRepo, roomDrafts, generationRepo, refGen, _, _ := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)
	refGen.imageBytes = []byte("not a real image")

	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if !claimed {
		t.Fatal("expected the attempt to be claimed")
	}
	updated := generationRepo.attempts[attempt.ID]
	if updated.Status != DesignGenerationStatusNeedsAttention {
		t.Fatalf("expected needs_attention for an undecodable reference image, got %q", updated.Status)
	}
	if updated.SafeFailureCode != "reference_image_invalid" {
		t.Fatalf("expected safe failure code reference_image_invalid, got %q", updated.SafeFailureCode)
	}
}

// --- T2A: R2 reference-storage failure after a successful FLUX call ---

func TestProcessOneDesignGenerationAttempt_R2StorageSuccessContinuesNormally(t *testing.T) {
	// (1) FLUX success + R2 success -> normal continuation through the
	// existing RP4E2/E0 pipeline (asset_generation_pending, job linked).
	svc, designRepo, roomDrafts, generationRepo, refGen, _, assetJobs := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)

	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if !claimed {
		t.Fatal("expected the attempt to be claimed")
	}
	updated := generationRepo.attempts[attempt.ID]
	if updated.Status != DesignGenerationStatusAssetGenerationPending {
		t.Fatalf("expected asset_generation_pending, got %q", updated.Status)
	}
	if updated.AssetGenerationJobID == "" {
		t.Fatal("expected an asset generation job to be linked")
	}
	if refGen.calls != 1 {
		t.Fatalf("expected exactly one GenerateReference call, got %d", refGen.calls)
	}
	job, err := assetJobs.FindByID(context.Background(), "company_1", updated.AssetGenerationJobID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if job.Source.Kind != AssetGenerationSourceKindDesignReference {
		t.Fatalf("expected a real Hunyuan job to have been started, got source kind %q", job.Source.Kind)
	}
}

func TestProcessOneDesignGenerationAttempt_R2StorageFailureMarksReferenceStorageFailed(t *testing.T) {
	// (2) FLUX success + R2 failure -> terminal/retryable storage failure,
	// never a lost/stranded attempt.
	svc, designRepo, roomDrafts, generationRepo, refGen, refStore, _ := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)
	refStore.putErr = errors.New("r2: PutObject failed: connection reset")

	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if !claimed {
		t.Fatal("expected the attempt to be claimed even though R2 storage then fails")
	}

	updated := generationRepo.attempts[attempt.ID]
	if updated.Status != DesignGenerationStatusNeedsAttention {
		t.Fatalf("expected needs_attention after an R2 storage failure, got %q", updated.Status)
	}
	if updated.SafeFailureCode != "reference_storage_failed" {
		t.Fatalf("expected safe failure code reference_storage_failed, got %q", updated.SafeFailureCode)
	}
	// Lineage to the DesignSession/target/reference attempt must survive —
	// the attempt record itself (never deleted) still carries it.
	if updated.SessionID != attempt.SessionID || updated.TurnID != attempt.TurnID {
		t.Fatalf("expected DesignSession/turn lineage preserved, got SessionID=%q TurnID=%q", updated.SessionID, updated.TurnID)
	}
	if updated.TargetSnapshot.ID != attempt.TargetSnapshot.ID {
		t.Fatalf("expected target lineage preserved, got %q want %q", updated.TargetSnapshot.ID, attempt.TargetSnapshot.ID)
	}
	if updated.ReferenceProviderStartedAt == nil {
		t.Fatal("expected ReferenceProviderStartedAt to remain durably set — the FLUX call genuinely happened")
	}
	if refGen.calls != 1 {
		t.Fatalf("expected GenerateReference to have been called exactly once, got %d", refGen.calls)
	}
}

func TestProcessOneDesignGenerationAttempt_R2StorageFailureNeverStartsHunyuan(t *testing.T) {
	// (3) Hunyuan is never started after an R2 storage failure.
	svc, designRepo, roomDrafts, generationRepo, _, refStore, assetJobs := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)
	refStore.putErr = errors.New("r2: PutObject failed")

	if _, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}

	updated := generationRepo.attempts[attempt.ID]
	if updated.AssetGenerationJobID != "" {
		t.Fatalf("expected no asset generation job to be linked after R2 failure, got %q", updated.AssetGenerationJobID)
	}
	if len(assetJobs.byID) != 0 {
		t.Fatalf("expected zero Hunyuan jobs to exist after R2 failure, got %d", len(assetJobs.byID))
	}

	// needs_attention is terminal, so it must never be auto-reclaimed and
	// silently pushed further into the pipeline either.
	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-2")
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if claimed {
		t.Fatal("expected the needs_attention attempt to never be auto-reclaimed")
	}
}

func TestProcessOneDesignGenerationAttempt_R2StorageFailureDoesNotRerunGLMReasoning(t *testing.T) {
	// (5) GLM/design reasoning is not rerun merely because storage failed —
	// the reference phase never calls back into ElementReasoner/GLM at all;
	// this proves the fake ElementReasoner sees zero calls across the whole
	// failed attempt, and that the SAME already-validated plan/turn survive
	// untouched (no new reasoning round-trip was triggered).
	svc, designRepo, roomDrafts, generationRepo, _, refStore, _ := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)
	refStore.putErr = errors.New("r2: PutObject failed")

	reasoner := svc.designReasoner.(*fakeElementReasoner)
	callsBefore := reasoner.calls

	if _, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}

	if reasoner.calls != callsBefore {
		t.Fatalf("expected zero additional GLM/reasoning calls after an R2 storage failure, calls went from %d to %d", callsBefore, reasoner.calls)
	}
	updated := generationRepo.attempts[attempt.ID]
	if updated.PlanFingerprint != attempt.PlanFingerprint {
		t.Fatalf("expected the same validated plan fingerprint to survive untouched, got %q want %q", updated.PlanFingerprint, attempt.PlanFingerprint)
	}
}

func TestProcessOneDesignGenerationAttempt_ManualRetryAfterR2FailureCanSucceed(t *testing.T) {
	// (4) manual retry (RegenerateDesignPlan, the existing mechanism) can
	// later succeed once R2 recovers.
	svc, designRepo, roomDrafts, generationRepo, refGen, refStore, assetJobs := newTestServiceWithDesignGenerationWorker(t)
	session, turn := setupReadyTurn(t, designRepo, roomDrafts, geometryPlan())
	first, _, err := svc.ConfirmDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, ConfirmDesignPlanInput{
		ClientRequestID: "confirm_retry_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("ConfirmDesignPlan: %v", err)
	}

	refStore.putErr = errors.New("r2: PutObject failed")
	claimed, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1")
	if err != nil || !claimed {
		t.Fatalf("first process: claimed=%v err=%v", claimed, err)
	}
	failed := generationRepo.attempts[first.ID]
	if failed.Status != DesignGenerationStatusNeedsAttention {
		t.Fatalf("expected needs_attention, got %q", failed.Status)
	}

	// R2 recovers; the contractor retries via the existing Regenerate
	// mechanism — a brand-new attempt, never a resurrection of the failed one.
	refStore.putErr = nil
	retry, wasReplay, err := svc.RegenerateDesignPlan(context.Background(), "company_1", "user_1", session.ID, turn.ID, RegenerateDesignPlanInput{
		ClientRequestID: "regenerate_retry_1", PlanFingerprint: turn.PlanFingerprint, ExpectedRoomDraftRevision: session.BasedOnRoomDraftRevision,
	})
	if err != nil {
		t.Fatalf("RegenerateDesignPlan: %v", err)
	}
	if wasReplay {
		t.Fatal("expected a genuinely new attempt, not a replay")
	}
	if retry.ID == first.ID {
		t.Fatal("expected a new attempt ID, never reusing the failed attempt")
	}
	if retry.AttemptNumber != first.AttemptNumber+1 {
		t.Fatalf("expected attempt number %d, got %d", first.AttemptNumber+1, retry.AttemptNumber)
	}

	claimed, err = svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-2")
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if !claimed {
		t.Fatal("expected the new attempt to be claimable")
	}
	succeeded := generationRepo.attempts[retry.ID]
	if succeeded.Status != DesignGenerationStatusAssetGenerationPending {
		t.Fatalf("expected the retried attempt to reach asset_generation_pending, got %q", succeeded.Status)
	}
	if succeeded.AssetGenerationJobID == "" {
		t.Fatal("expected a real Hunyuan job to have been started on the successful retry")
	}
	if _, ok := assetJobs.byID[succeeded.AssetGenerationJobID]; !ok {
		t.Fatal("expected the linked job to actually exist")
	}
	if refGen.calls != 2 {
		t.Fatalf("expected GenerateReference called once per attempt (failed + retried = 2), got %d", refGen.calls)
	}
}

func TestProcessOneDesignGenerationAttempt_CancelWinsOverInFlightReferenceGeneration(t *testing.T) {
	svc, designRepo, roomDrafts, generationRepo, _, _, _ := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)

	// Simulate a worker that already claimed the phase (claimPhase moved it
	// to generating_reference) but the user cancels before the rest of the
	// step runs.
	claimedAttempt, err := generationRepo.ClaimNextGenerationPhase(context.Background())
	if err != nil {
		t.Fatalf("ClaimNextGenerationPhase: %v", err)
	}
	if claimedAttempt.ID != attempt.ID {
		t.Fatalf("expected to claim %s, got %s", attempt.ID, claimedAttempt.ID)
	}
	if _, err := svc.CancelDesignGenerationAttempt(context.Background(), "company_1", attempt.ID, "cancel_req_1"); err != nil {
		t.Fatalf("CancelDesignGenerationAttempt: %v", err)
	}

	// The worker's own subsequent checkpoint write must now be rejected —
	// never silently overwrite the abandoned status.
	err = generationRepo.SetReferenceProviderStarted(context.Background(), "company_1", attempt.ID, time.Now())
	if !errors.Is(err, ErrDesignGenerationNotClaimable) {
		t.Fatalf("expected ErrDesignGenerationNotClaimable for a checkpoint write after cancel, got %v", err)
	}

	final := generationRepo.attempts[attempt.ID]
	if final.Status != DesignGenerationStatusAbandoned {
		t.Fatalf("expected the attempt to remain abandoned, got %q", final.Status)
	}
}

// --- reconciliation: RP4E0 job completion -> DesignGenerationAttempt (plan §"Reconcile") ---

func TestReconcileDesignGenerationAttemptFromJob_CompletedJobReachesConceptReadyWithAssetRef(t *testing.T) {
	svc, designRepo, roomDrafts, generationRepo, _, _, assetJobs := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)

	provider := svc.assetGenerationProvider.(*fakeAssetGenerationProvider)
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	validGLB := buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		return ProviderGenerationResult{Status: ProviderGenerationCompleted, Output: readCloserFromBytes{bytes.NewReader(validGLB)}}, nil
	}

	// Step 1: reference phase reaches asset_generation_pending and links a
	// real RP4E0 job (exercised via the actual worker, not hand-built).
	if _, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	linked := generationRepo.attempts[attempt.ID]
	if linked.Status != DesignGenerationStatusAssetGenerationPending || linked.AssetGenerationJobID == "" {
		t.Fatalf("expected asset_generation_pending with a linked job, got status=%q jobID=%q", linked.Status, linked.AssetGenerationJobID)
	}

	// Step 2: RP4E0's own worker completes the linked job — the
	// reconciliation this test targets runs as part of THIS call.
	if _, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-2"); err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}

	job, err := assetJobs.FindByID(context.Background(), "company_1", linked.AssetGenerationJobID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if job.Execution.Status != JobExecutionCompleted {
		t.Fatalf("expected the linked job itself to be completed, got %q", job.Execution.Status)
	}

	reconciled := generationRepo.attempts[attempt.ID]
	if reconciled.Status != DesignGenerationStatusConceptReady {
		t.Fatalf("expected the design attempt to reach concept_ready via reconciliation, got %q", reconciled.Status)
	}
	if reconciled.Candidate.VisualAction != ConceptBindingActionAssign {
		t.Errorf("expected VisualAction=assign, got %q", reconciled.Candidate.VisualAction)
	}
	if reconciled.Candidate.VisualAsset == nil || reconciled.Candidate.VisualAsset.AssetID != job.ResultAssetID || reconciled.Candidate.VisualAsset.Version != job.ResultVersion {
		t.Fatalf("expected the candidate's VisualAsset to copy the job's exact {assetId,version}, got %+v (job had %s v%d)", reconciled.Candidate.VisualAsset, job.ResultAssetID, job.ResultVersion)
	}
}

func TestReconcileDesignGenerationAttemptFromJob_FailedJobFailsTheAttempt(t *testing.T) {
	svc, designRepo, roomDrafts, generationRepo, _, _, _ := newTestServiceWithDesignGenerationWorker(t)
	attempt := confirmedGeometryAttempt(t, svc, designRepo, roomDrafts)

	provider := svc.assetGenerationProvider.(*fakeAssetGenerationProvider)
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_1"}, nil
	}
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		return ProviderGenerationResult{Status: ProviderGenerationRejected, FailureMessage: "provider rejected the geometry request"}, nil
	}

	if _, err := svc.ProcessOneDesignGenerationAttempt(context.Background(), "worker-1"); err != nil {
		t.Fatalf("ProcessOneDesignGenerationAttempt: %v", err)
	}
	if _, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-2"); err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}

	reconciled := generationRepo.attempts[attempt.ID]
	if reconciled.Status != DesignGenerationStatusFailed {
		t.Fatalf("expected the design attempt to be failed after its linked job was rejected, got %q", reconciled.Status)
	}
	if reconciled.SafeFailureCode != "asset_generation_failed" {
		t.Errorf("expected a safe failure code that never leaks provider internals, got %q", reconciled.SafeFailureCode)
	}
}

func TestReconcileDesignGenerationAttemptFromJob_NoOpForAnUnlinkedJob(t *testing.T) {
	svc, _, _, _, _, _, _ := newTestServiceWithDesignGenerationWorker(t)
	// A plain RP4E0 job submitted through the ORIGINAL public route
	// (capture-artifact source) — never linked to any design attempt.
	artifact := seedTestArtifact(t, svc, "company_1", "capture_1", ArtifactKindObservationPhoto, "image/jpeg")
	if _, err := svc.SubmitAssetGenerationJob(context.Background(), "company_1", "project_1", "user_1", "req_unlinked", artifact.ID, 7); err != nil {
		t.Fatalf("SubmitAssetGenerationJob: %v", err)
	}

	provider := svc.assetGenerationProvider.(*fakeAssetGenerationProvider)
	provider.StartFunc = func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
		return ProviderGenerationRef{ID: "evt_unlinked"}, nil
	}
	validGLB := buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
	provider.ResumeFunc = func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
		return ProviderGenerationResult{Status: ProviderGenerationCompleted, Output: readCloserFromBytes{bytes.NewReader(validGLB)}}, nil
	}

	// Must complete successfully with no panic/error even though nothing
	// is linked — the reconciliation lookup silently finds nothing.
	claimed, err := svc.ProcessOneAssetGenerationJob(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessOneAssetGenerationJob: %v", err)
	}
	if !claimed {
		t.Fatal("expected the unlinked job itself to still be claimed and processed")
	}
}
