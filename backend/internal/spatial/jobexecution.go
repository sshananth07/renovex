package spatial

import "time"

// JobExecutionStatus is the shared lifecycle status for any durable async
// job built on JobExecution (M8.5C design spec §37 — the first real
// implementation is SpatialAssetGenerationJob, task-specific failure
// reasons live in FailureCode, never as new Status values, so this type
// stays reusable by future job kinds: SpatialReconstructionJob,
// SpatialDesignGenerationJob, SpatialAssetImportJob).
type JobExecutionStatus string

const (
	JobExecutionPending    JobExecutionStatus = "pending"
	JobExecutionProcessing JobExecutionStatus = "processing"
	JobExecutionCompleted  JobExecutionStatus = "completed"
	JobExecutionFailed     JobExecutionStatus = "failed"
	// JobExecutionNeedsAttention covers every state that must never be
	// auto-retried by the system (quota exhaustion, an ambiguous
	// provider outcome, or any future job kind's equivalent) — the
	// SPECIFIC reason lives in FailureCode.
	JobExecutionNeedsAttention JobExecutionStatus = "needs_attention"
	JobExecutionCancelled      JobExecutionStatus = "cancelled"
)

func (s JobExecutionStatus) IsValid() bool {
	switch s {
	case JobExecutionPending, JobExecutionProcessing, JobExecutionCompleted,
		JobExecutionFailed, JobExecutionNeedsAttention, JobExecutionCancelled:
		return true
	default:
		return false
	}
}

// JobExecution is the shared durable execution-state shape every async
// job kind embeds by value (M8.5C §37). Deliberately minimal — shared
// STATE, not a generic job framework: no Job[T], no generic repository,
// no reflection dispatcher. Domain job types (SpatialAssetGenerationJob
// today) add their own payload/checkpoint fields alongside this.
type JobExecution struct {
	Status      JobExecutionStatus
	Attempt     int
	MaxAttempts int
	AvailableAt time.Time // when this job becomes claimable again

	LeaseOwner     string // opaque worker instance identifier
	LeaseExpiresAt *time.Time

	StartedAt   *time.Time
	CompletedAt *time.Time

	// FailureCode is job-kind-specific (e.g. "zero_gpu_quota",
	// "provider_outcome_unknown", "invalid_source_image",
	// "generated_glb_invalid", "provider_rejected") — never a new
	// top-level Status value.
	FailureCode    string
	FailureMessage string

	CancelRequestedAt *time.Time
}

// leaseIsHeld reports whether e's lease is currently held by SOME worker
// (owner set, not yet expired) as of now. A job with no lease ever taken,
// or an expired lease, is NOT currently held — but whether it is safe to
// CLAIM depends on job-kind-specific checkpoint state beyond this shared
// type (see SpatialAssetGenerationJob.isSafeToClaim in
// assetgenerationjob.go), which is why this helper stops at lease state
// alone.
func (e JobExecution) leaseIsHeld(now time.Time) bool {
	return e.LeaseOwner != "" && e.LeaseExpiresAt != nil && e.LeaseExpiresAt.After(now)
}
