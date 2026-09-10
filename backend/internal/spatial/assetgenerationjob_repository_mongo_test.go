package spatial_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func TestMongoAssetGenerationJobRepository_Create_DuplicateClientRequestIDAdopts(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoAssetGenerationJobRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	job := spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_1",
		RequestFingerprint: "fp_1", Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "artifact_1"}, Seed: 42,
		CreatedAt: time.Now(), Execution: spatial.JobExecution{Status: spatial.JobExecutionPending, MaxAttempts: 3},
	}
	created, err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	adopted, err := repo.Create(ctx, job) // identical retry
	if err != nil {
		t.Fatalf("Create (retry): %v", err)
	}
	if adopted.ID != created.ID {
		t.Errorf("expected retry to adopt the same job, got a different ID")
	}
}

func TestMongoAssetGenerationJobRepository_Create_ConflictingFingerprintRejected(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoAssetGenerationJobRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	job := spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_1",
		RequestFingerprint: "fp_1", Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "artifact_1"}, Seed: 42,
		CreatedAt: time.Now(), Execution: spatial.JobExecution{Status: spatial.JobExecutionPending},
	}
	if _, err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	conflicting := job
	conflicting.RequestFingerprint = "fp_2" // different fingerprint, same clientRequestId
	_, err := repo.Create(ctx, conflicting)
	if !errors.Is(err, spatial.ErrAssetGenerationRequestFingerprintConflict) {
		t.Errorf("expected ErrAssetGenerationRequestFingerprintConflict, got %v", err)
	}
}

func TestMongoAssetGenerationJobRepository_ClaimNext_ExcludesDangerousState(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoAssetGenerationJobRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	now := time.Now()
	past := now.Add(-time.Hour) // an expired lease, if one existed

	dangerous, err := repo.Create(ctx, spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_dangerous", RequestFingerprint: "fp1",
		Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "a1"}, Seed: 1, CreatedAt: now,
		ProviderStartedAt: &now, // started, no event id, no staged GLB
		Execution:         spatial.JobExecution{Status: spatial.JobExecutionProcessing, LeaseOwner: "dead-worker", LeaseExpiresAt: &past},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	safe, err := repo.Create(ctx, spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_safe", RequestFingerprint: "fp2",
		Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "a1"}, Seed: 2, CreatedAt: now,
		Execution: spatial.JobExecution{Status: spatial.JobExecutionPending, MaxAttempts: 3},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := repo.ClaimNext(ctx, "worker-1", 3*time.Minute, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed.ID != safe.ID {
		t.Errorf("expected to claim the safe job %q, got %q (dangerous job was %q)", safe.ID, claimed.ID, dangerous.ID)
	}

	// A second claim attempt must find nothing else claimable — the
	// dangerous job must remain untouched.
	_, err = repo.ClaimNext(ctx, "worker-2", 3*time.Minute, now)
	if !errors.Is(err, spatial.ErrAssetGenerationJobNotClaimable) {
		t.Errorf("expected ErrAssetGenerationJobNotClaimable, got %v", err)
	}
}

func TestMongoAssetGenerationJobRepository_SetProviderRequestID_RejectsWhenLeaseNoLongerOwned(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoAssetGenerationJobRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	now := time.Now()
	job, err := repo.Create(ctx, spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_1", RequestFingerprint: "fp1",
		Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "a1"}, Seed: 1, CreatedAt: now,
		Execution: spatial.JobExecution{Status: spatial.JobExecutionPending, MaxAttempts: 3},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := repo.ClaimNext(ctx, "worker-A", 3*time.Minute, now)
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNext by worker-A: claimed=%+v err=%v", claimed, err)
	}

	// worker-B claims the SAME job after simulating worker-A's lease
	// having already expired (advance "now" past the lease TTL).
	later := now.Add(4 * time.Minute)
	stolenBy, err := repo.ClaimNext(ctx, "worker-B", 3*time.Minute, later)
	if err != nil || stolenBy.ID != job.ID {
		t.Fatalf("ClaimNext by worker-B: stolenBy=%+v err=%v", stolenBy, err)
	}

	// worker-A, unaware its lease is gone, tries to write a checkpoint —
	// this MUST be rejected, never silently overwrite worker-B's claim.
	err = repo.SetProviderRequestID(ctx, job.ID, "worker-A", "evt_from_stale_worker")
	if !errors.Is(err, spatial.ErrAssetGenerationJobNotClaimable) {
		t.Errorf("expected ErrAssetGenerationJobNotClaimable for a fenced write from a worker that lost its lease, got %v", err)
	}

	// worker-B's own write, by contrast, must succeed.
	if err := repo.SetProviderRequestID(ctx, job.ID, "worker-B", "evt_from_current_owner"); err != nil {
		t.Errorf("expected the CURRENT lease-holder's write to succeed, got %v", err)
	}
}

func TestMongoAssetGenerationJobRepository_RequeueForRetry_EnforcesMaxAttemptsAndClearsLease(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoAssetGenerationJobRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	now := time.Now()
	_, err := repo.Create(ctx, spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_1", RequestFingerprint: "fp1",
		Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "a1"}, Seed: 1, CreatedAt: now,
		Execution: spatial.JobExecution{Status: spatial.JobExecutionPending, MaxAttempts: 2},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := repo.ClaimNext(ctx, "worker-A", 3*time.Minute, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if err := repo.RequeueForRetry(ctx, claimed.ID, "worker-A", now); err != nil {
		t.Fatalf("first RequeueForRetry: %v", err)
	}
	afterFirst, err := repo.FindByID(ctx, "company_1", claimed.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if afterFirst.Execution.Status != spatial.JobExecutionPending || afterFirst.Execution.Attempt != 1 {
		t.Errorf("expected pending/attempt=1 after first requeue, got status=%q attempt=%d", afterFirst.Execution.Status, afterFirst.Execution.Attempt)
	}
	if afterFirst.Execution.LeaseOwner != "" {
		t.Errorf("expected the lease to be cleared after requeue, got leaseOwner=%q", afterFirst.Execution.LeaseOwner)
	}

	claimedAgain, err := repo.ClaimNext(ctx, "worker-B", 3*time.Minute, now)
	if err != nil {
		t.Fatalf("second ClaimNext: %v", err)
	}
	if err := repo.RequeueForRetry(ctx, claimedAgain.ID, "worker-B", now); err != nil {
		t.Fatalf("second RequeueForRetry: %v", err)
	}
	afterSecond, err := repo.FindByID(ctx, "company_1", claimedAgain.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if afterSecond.Execution.Status != spatial.JobExecutionFailed {
		t.Errorf("expected the job to become terminal failed once MaxAttempts=2 is reached, got status=%q attempt=%d", afterSecond.Execution.Status, afterSecond.Execution.Attempt)
	}

	// A failed job must never be claimable again.
	_, err = repo.ClaimNext(ctx, "worker-C", 3*time.Minute, now)
	if !errors.Is(err, spatial.ErrAssetGenerationJobNotClaimable) {
		t.Errorf("expected the exhausted job to be unclaimable, got %v", err)
	}
}

func TestMongoAssetGenerationJobRepository_ClaimNext_ConcurrentRaceExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoAssetGenerationJobRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	now := time.Now()
	_, err := repo.Create(ctx, spatial.SpatialAssetGenerationJob{
		CompanyID: "company_1", ClientRequestID: "req_race", RequestFingerprint: "fp1",
		Source: spatial.AssetGenerationSourceRef{Kind: spatial.AssetGenerationSourceKindCaptureArtifact, ArtifactID: "a1"}, Seed: 1, CreatedAt: now,
		Execution: spatial.JobExecution{Status: spatial.JobExecutionPending, MaxAttempts: 3},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const workers = 10
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := repo.ClaimNext(ctx, fmt.Sprintf("worker-%d", n), 3*time.Minute, now)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, spatial.ErrAssetGenerationJobNotClaimable) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful claim among %d racing workers, got %d", workers, successes)
	}
}
