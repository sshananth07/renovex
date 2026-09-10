package spatial

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// fakeAssetGenerationJobRepository is an in-memory AssetGenerationJobRepository
// mirroring MongoAssetGenerationJobRepository's real semantics (claim-safety
// filter, lease fencing, MaxAttempts enforcement) — used for fast Service-level
// tests that don't need real Mongo/Testcontainers overhead. The real Mongo
// implementation's own dedicated tests (assetgenerationjob_repository_mongo_test.go)
// are what prove the ACTUAL Mongo query is correct; this fake exists so
// Service-level worker-lifecycle tests can run in milliseconds.
type fakeAssetGenerationJobRepository struct {
	mu     sync.Mutex
	byID   map[string]SpatialAssetGenerationJob
	nextID int
}

func newFakeAssetGenerationJobRepository() *fakeAssetGenerationJobRepository {
	return &fakeAssetGenerationJobRepository{byID: map[string]SpatialAssetGenerationJob{}}
}

func (f *fakeAssetGenerationJobRepository) Create(_ context.Context, j SpatialAssetGenerationJob) (SpatialAssetGenerationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.CompanyID == j.CompanyID && existing.ClientRequestID == j.ClientRequestID {
			if existing.RequestFingerprint == j.RequestFingerprint {
				return existing, nil
			}
			return SpatialAssetGenerationJob{}, ErrAssetGenerationRequestFingerprintConflict
		}
	}
	f.nextID++
	j.ID = fmt.Sprintf("job_%d", f.nextID)
	f.byID[j.ID] = j
	return j, nil
}

func (f *fakeAssetGenerationJobRepository) FindByID(_ context.Context, companyID, id string) (SpatialAssetGenerationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.CompanyID != companyID {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotFound
	}
	return j, nil
}

func (f *fakeAssetGenerationJobRepository) FindByClientRequestID(_ context.Context, companyID, clientRequestID string) (SpatialAssetGenerationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, j := range f.byID {
		if j.CompanyID == companyID && j.ClientRequestID == clientRequestID {
			return j, nil
		}
	}
	return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotFound
}

func (f *fakeAssetGenerationJobRepository) ClaimNext(_ context.Context, workerID string, leaseTTL time.Duration, now time.Time) (SpatialAssetGenerationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, j := range f.byID {
		if !j.isSafeToClaim(now) {
			continue
		}
		j.Execution.Status = JobExecutionProcessing
		j.Execution.LeaseOwner = workerID
		expiresAt := now.Add(leaseTTL)
		j.Execution.LeaseExpiresAt = &expiresAt
		f.byID[id] = j
		return j, nil
	}
	return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotClaimable
}

func (f *fakeAssetGenerationJobRepository) RenewLease(_ context.Context, id, workerID string, newExpiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.Execution.LeaseExpiresAt = &newExpiresAt
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) SetProviderStarted(_ context.Context, id, workerID string, startedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.ProviderStartedAt = &startedAt
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) SetProviderRequestID(_ context.Context, id, workerID, eventID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.ProviderRequestID = &eventID
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) SetGenerated(_ context.Context, id, workerID, objectKey, checksum string, byteCount int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.GeneratedObjectKey = objectKey
	j.GeneratedChecksum = checksum
	j.GeneratedByteCount = byteCount
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) Complete(_ context.Context, id, workerID, resultAssetID string, resultVersion int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.Execution.Status = JobExecutionCompleted
	j.ResultAssetID = resultAssetID
	j.ResultVersion = resultVersion
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) MarkFailed(_ context.Context, id, workerID, failureCode, failureMessage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.Execution.Status = JobExecutionFailed
	j.Execution.FailureCode = failureCode
	j.Execution.FailureMessage = failureMessage
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) MarkNeedsAttention(_ context.Context, id, workerID, failureCode, failureMessage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	j.Execution.Status = JobExecutionNeedsAttention
	j.Execution.FailureCode = failureCode
	j.Execution.FailureMessage = failureMessage
	f.byID[id] = j
	return nil
}

func (f *fakeAssetGenerationJobRepository) RequeueForRetry(_ context.Context, id, workerID string, availableAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Execution.LeaseOwner != workerID {
		return ErrAssetGenerationJobNotClaimable
	}
	nextAttempt := j.Execution.Attempt + 1
	j.Execution.Attempt = nextAttempt
	j.Execution.LeaseOwner = ""
	j.Execution.LeaseExpiresAt = nil
	if j.Execution.MaxAttempts > 0 && nextAttempt >= j.Execution.MaxAttempts {
		j.Execution.Status = JobExecutionFailed
		j.Execution.FailureCode = "max_attempts_exhausted"
		j.Execution.FailureMessage = "asset generation job exhausted its retry budget"
	} else {
		j.Execution.Status = JobExecutionPending
		j.Execution.AvailableAt = availableAt
	}
	f.byID[id] = j
	return nil
}
