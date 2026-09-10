package spatial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"time"
)

const (
	maxSourceImageBytes                     = 20 * 1024 * 1024
	maxSourceImageDimension                 = 8192
	defaultAssetGenerationLeaseTTL          = 3 * time.Minute
	defaultAssetGenerationHeartbeatInterval = 60 * time.Second
	defaultAssetGenerationProviderTimeout   = 9 * time.Minute
	defaultAssetGenerationSourceAccessTTL   = 15 * time.Minute
)

// assetGenerationResumePollInterval is a package-level var (not a const)
// specifically so tests can override it to a few milliseconds — the
// pending-then-completed polling test would otherwise take ~10s
// wall-clock per run. Production code path is unaffected; only tests
// reassign this.
var assetGenerationResumePollInterval = 5 * time.Second

// SubmitAssetGenerationJob validates the request deterministically
// (BEFORE any GPU spend, per RP4E0 spec §6), computes the server-side
// idempotency fingerprint, and creates (or adopts) a durable job record.
// Never calls the provider directly — that happens in
// ProcessOneAssetGenerationJob, dispatched separately. This is the
// original RP4E0 public-route path: source is always a directly-uploaded
// capture artifact.
func (s *Service) SubmitAssetGenerationJob(ctx context.Context, companyID, projectID, userID, clientRequestID, sourceArtifactID string, seed int64) (SpatialAssetGenerationJob, error) {
	if s.assetGenerationJobs == nil || s.assetGenerationProvider == nil {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationNotConfigured
	}

	artifact, err := s.artifacts.FindByID(ctx, companyID, sourceArtifactID)
	if err != nil {
		return SpatialAssetGenerationJob{}, err
	}
	if err := validateSourceArtifact(ctx, s.objectStore, artifact); err != nil {
		return SpatialAssetGenerationJob{}, err
	}

	fingerprint := computeAssetGenerationFingerprint(companyID, sourceArtifactID, artifact.Checksum, seed)
	source := AssetGenerationSourceRef{Kind: AssetGenerationSourceKindCaptureArtifact, ArtifactID: sourceArtifactID}
	return s.createAssetGenerationJob(ctx, companyID, projectID, userID, clientRequestID, fingerprint, source, seed)
}

// SubmitAssetGenerationJobFromDesignReference is RP4E2 Gate 2's internal
// submission path — called only from the design-generation worker once an
// attempt's reference phase has stored a validated reference image under
// R2 (never from a public route; there is no client-facing DTO for this
// call). The source image is the attempt-owned R2 object itself, not a
// SpatialArtifact — validateSourceArtifact's artifact-status/content-type
// checks do not apply here because the reference image was already
// validated (signature, dimensions, size) at generation time by Lane A's
// reference-image service; re-validating here would just re-decode bytes
// this process already trusts.
func (s *Service) SubmitAssetGenerationJobFromDesignReference(ctx context.Context, companyID, projectID, userID, clientRequestID, designAttemptID, objectKey, checksum string, seed int64) (SpatialAssetGenerationJob, error) {
	if s.assetGenerationJobs == nil || s.assetGenerationProvider == nil {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationNotConfigured
	}
	fingerprint := computeAssetGenerationFingerprint(companyID, designAttemptID, checksum, seed)
	source := AssetGenerationSourceRef{Kind: AssetGenerationSourceKindDesignReference, DesignAttemptID: designAttemptID, ObjectKey: objectKey}
	return s.createAssetGenerationJob(ctx, companyID, projectID, userID, clientRequestID, fingerprint, source, seed)
}

// createAssetGenerationJob is the shared job-creation call both submission
// paths above route through — everything past "the source is valid and the
// fingerprint is computed" is identical regardless of source kind.
func (s *Service) createAssetGenerationJob(ctx context.Context, companyID, projectID, userID, clientRequestID, fingerprint string, source AssetGenerationSourceRef, seed int64) (SpatialAssetGenerationJob, error) {
	job, err := s.assetGenerationJobs.Create(ctx, SpatialAssetGenerationJob{
		CompanyID: companyID, ProjectID: projectID,
		ClientRequestID: clientRequestID, RequestFingerprint: fingerprint,
		Source: source, Seed: seed,
		CreatedByUserID: userID, CreatedAt: time.Now(),
		Execution: JobExecution{Status: JobExecutionPending, MaxAttempts: 3, AvailableAt: time.Now()},
	})
	if err != nil {
		return SpatialAssetGenerationJob{}, err
	}
	s.notifyGenerationWake(ctx, GenerationWakeKindAssetJob, job.ID)
	return job, nil
}

func (s *Service) GetAssetGenerationJob(ctx context.Context, companyID, jobID string) (SpatialAssetGenerationJob, error) {
	if s.assetGenerationJobs == nil {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationNotConfigured
	}
	return s.assetGenerationJobs.FindByID(ctx, companyID, jobID)
}

// StreamAssetGenerationSourceImage verifies capability (a local-provider
// bearer token) and, if valid, resolves and returns the referenced
// SpatialArtifact plus a reader over its bytes — the Hunyuan provider's
// only path to actually reading a source image. Mirrors
// StreamVisualAssetContent's exact shape from RP4D: capability signature
// and expiry are verified FIRST, entirely in-memory, BEFORE any
// repository/object-store lookup. Returns one generic error for every
// failure mode.
func (s *Service) StreamAssetGenerationSourceImage(ctx context.Context, capability string) (SpatialArtifact, io.ReadCloser, error) {
	if s.assetGenerationSourceCapabilityVerifier == nil || s.artifacts == nil || s.objectStore == nil {
		return SpatialArtifact{}, nil, ErrAssetGenerationNotConfigured
	}
	companyID, artifactID, err := s.assetGenerationSourceCapabilityVerifier.VerifySourceImageAccess(ctx, capability)
	if err != nil {
		return SpatialArtifact{}, nil, err
	}
	artifact, err := s.artifacts.FindByID(ctx, companyID, artifactID)
	if err != nil {
		return SpatialArtifact{}, nil, err
	}
	key := objectKeyFor(artifact.CompanyID, artifact.CaptureID, artifact.ID)
	content, err := s.objectStore.Get(ctx, key)
	if err != nil {
		return SpatialArtifact{}, nil, err
	}
	return artifact, content, nil
}

// validateSourceArtifact runs every deterministic check possible BEFORE
// any provider call — status, content type, size, and an ACTUAL image
// decode with sane pixel dimensions (genuinely inspecting bytes, not
// trusting the stored content type alone — matches RP4D's GLB/USDZ
// structural-check convention). Any failure here means zero GPU spend.
// The artifact's bytes are read through a BOUNDED reader
// (io.LimitReader, mirroring assetgenerationvalidation.go's readBounded)
// — image.DecodeConfig only needs the header, but the underlying stream
// is still capped defensively rather than trusting a well-behaved
// reader/decoder to stop on its own.
func validateSourceArtifact(ctx context.Context, store ArtifactObjectStore, artifact SpatialArtifact) error {
	if artifact.Status != ArtifactStatusUploaded {
		return ErrAssetGenerationSourceInvalid
	}
	if artifact.ContentType != "image/jpeg" && artifact.ContentType != "image/png" {
		return ErrAssetGenerationSourceInvalid
	}
	if artifact.ActualSize <= 0 || artifact.ActualSize > maxSourceImageBytes {
		return ErrAssetGenerationSourceInvalid
	}
	key := objectKeyFor(artifact.CompanyID, artifact.CaptureID, artifact.ID)
	reader, err := store.Get(ctx, key)
	if err != nil {
		return ErrAssetGenerationSourceInvalid
	}
	defer reader.Close()
	cfg, _, err := image.DecodeConfig(io.LimitReader(reader, maxSourceImageBytes+1))
	if err != nil {
		return ErrAssetGenerationSourceInvalid
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSourceImageDimension || cfg.Height > maxSourceImageDimension {
		return ErrAssetGenerationSourceInvalid
	}
	return nil
}

// ProcessOneAssetGenerationJob claims one claimable job (if any) and
// advances it exactly one phase according to claimPhase() (RP4E0 spec
// §7). Returns (false, nil) if nothing was claimable — this is the
// normal idle case, not an error. A single call does the entire
// remaining lifecycle for whichever phase it finds (start-and-wait,
// resume-and-wait, or validate-and-publish) since each of those is
// itself bounded by the provider timeout / a single validation pass —
// it does not return control mid-phase.
func (s *Service) ProcessOneAssetGenerationJob(ctx context.Context, workerID string) (bool, error) {
	if s.assetGenerationJobs == nil || s.assetGenerationProvider == nil {
		return false, ErrAssetGenerationNotConfigured
	}
	leaseTTL := s.assetGenerationLeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = defaultAssetGenerationLeaseTTL
	}
	job, err := s.assetGenerationJobs.ClaimNext(ctx, workerID, leaseTTL, time.Now())
	if errors.Is(err, ErrAssetGenerationJobNotClaimable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// leaseLost is set by the heartbeat goroutine the instant a renewal
	// is fenced-rejected — checked by the main path BETWEEN steps (never
	// mid-step, since a step already in flight cannot be safely aborted
	// partway) so a worker whose lease is gone stops advancing this job
	// instead of racing a checkpoint write it would lose anyway.
	leaseLost := make(chan struct{})
	stopHeartbeat := s.startAssetGenerationHeartbeat(ctx, job.ID, workerID, leaseTTL, leaseLost)
	defer stopHeartbeat()

	var stepErr error
	switch job.claimPhase() {
	case ClaimPhaseStartProvider:
		stepErr = s.startAndResumeAssetGeneration(ctx, job, workerID, leaseLost)
	case ClaimPhaseResumeProvider:
		stepErr = s.resumeAssetGeneration(ctx, job, workerID, leaseLost)
	case ClaimPhaseResumeValidation:
		stepErr = s.validateAndPublishAssetGeneration(ctx, job, workerID)
	default:
		stepErr = fmt.Errorf("spatial: unreachable claim phase")
	}
	if stepErr != nil {
		return true, stepErr
	}

	// RP4E2 reconciliation (plan §"Reconcile": "On job completion, copy
	// only {assetId,version} into the candidate and mark concept_ready.
	// failed, needs_attention, or cancelled map without another GPU
	// submission.") — a no-op for every RP4E0-only job (the public
	// capture-artifact route never links a design attempt), and
	// deliberately best-effort: a reconciliation failure here must never
	// turn an otherwise-successful RP4E0 step into a reported worker
	// error, since the job's own state already landed correctly — the
	// NEXT design-generation process-one call naturally reconciles a
	// missed link on its own next poll.
	if s.designGenerationAttempts != nil {
		s.reconcileDesignGenerationAttemptFromJob(ctx, job.CompanyID, job.ID)
	}
	return true, nil
}

// reconcileDesignGenerationAttemptFromJob looks up the design attempt (if
// any) linked to jobID and advances it to match the job's now-current
// terminal state — completed jobs copy {assetId,version} into the
// candidate and reach concept_ready via the SAME CompleteWithConcept path
// Gate 1's material-only Confirm already uses; failed/needs_attention jobs
// fail the attempt with a safe code that never leaks provider internals.
// A job still pending/processing, or one with no linked attempt at all
// (RP4E0's own public route), is a silent no-op — there is nothing to
// reconcile yet or ever.
func (s *Service) reconcileDesignGenerationAttemptFromJob(ctx context.Context, companyID, jobID string) {
	attempt, err := s.designGenerationAttempts.FindAttemptByAssetGenerationJobID(ctx, companyID, jobID)
	if err != nil {
		return // not linked to a design attempt, or lookup failed transiently — next poll retries
	}
	if attempt.Status != DesignGenerationStatusAssetGenerationPending && attempt.Status != DesignGenerationStatusAssetGenerationProcessing {
		return // already reconciled, or in a status this reconciliation doesn't own
	}
	job, err := s.assetGenerationJobs.FindByID(ctx, companyID, jobID)
	if err != nil {
		return
	}
	switch job.Execution.Status {
	case JobExecutionCompleted:
		candidate := attempt.Candidate
		candidate.VisualAction = ConceptBindingActionAssign
		candidate.VisualAsset = &VisualAssetRef{AssetID: job.ResultAssetID, Version: job.ResultVersion}
		_, _ = s.designGenerationAttempts.CompleteWithConcept(ctx, companyID, attempt.ID, candidate)
	case JobExecutionFailed:
		_, _ = s.designGenerationAttempts.Fail(ctx, companyID, attempt.ID, DesignGenerationStatusFailed, "asset_generation_failed")
	case JobExecutionNeedsAttention:
		_, _ = s.designGenerationAttempts.Fail(ctx, companyID, attempt.ID, DesignGenerationStatusNeedsAttention, "asset_generation_needs_attention")
	case JobExecutionProcessing:
		_, _ = s.designGenerationAttempts.SetAssetGenerationProcessing(ctx, companyID, attempt.ID)
	default:
		// Pending, or any other non-terminal status: nothing to reconcile yet.
	}
}

// startAssetGenerationHeartbeat renews the lease on an interval well
// inside the lease TTL. On the FIRST renewal failure (fenced — this
// worker no longer holds the lease), it closes leaseLost and stops
// renewing — it does NOT keep retrying silently. The caller is
// responsible for checking leaseLost between steps and aborting rather
// than attempting a checkpoint write that would itself be correctly
// rejected by fencing after wasting real work getting there.
func (s *Service) startAssetGenerationHeartbeat(ctx context.Context, jobID, workerID string, leaseTTL time.Duration, leaseLost chan struct{}) func() {
	interval := s.assetGenerationHeartbeatInterval
	if interval <= 0 {
		interval = defaultAssetGenerationHeartbeatInterval
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := s.assetGenerationJobs.RenewLease(ctx, jobID, workerID, time.Now().Add(leaseTTL)); err != nil {
					select {
					case <-leaseLost:
						// already signaled
					default:
						close(leaseLost)
					}
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

// leaseStillHeld is a small helper every worker method calls between
// steps to check the heartbeat's leaseLost signal without blocking.
func leaseStillHeld(leaseLost chan struct{}) bool {
	select {
	case <-leaseLost:
		return false
	default:
		return true
	}
}

// resolveAssetGenerationSourceAccess mints a short-lived provider-readable
// URL for job's source regardless of kind — a capture artifact resolves
// through the existing artifact-lookup + AssetGenerationSourceAccessProvider
// path; a design reference resolves directly through
// DesignReferenceSourceAccessProvider against the attempt-owned R2 key,
// with no artifact lookup at all (there is no SpatialArtifact record for
// it). Both paths return the exact same ReadAccess shape so the caller
// never needs to branch again after this point.
func (s *Service) resolveAssetGenerationSourceAccess(ctx context.Context, job SpatialAssetGenerationJob) (ReadAccess, error) {
	sourceTTL := s.assetGenerationSourceAccessTTLOrDefault()
	switch job.Source.Kind {
	case AssetGenerationSourceKindDesignReference:
		if s.designReferenceSourceAccess == nil {
			return ReadAccess{}, ErrAssetGenerationNotConfigured
		}
		return s.designReferenceSourceAccess.CreateReferenceImageAccess(ctx, job.CompanyID, job.Source.ObjectKey, sourceTTL)
	default: // AssetGenerationSourceKindCaptureArtifact, and the empty-string zero value for safety
		artifact, err := s.artifacts.FindByID(ctx, job.CompanyID, job.Source.ArtifactID)
		if err != nil {
			return ReadAccess{}, err
		}
		return s.assetGenerationSourceAccess.CreateSourceImageAccess(ctx, artifact, sourceTTL)
	}
}

// startAndResumeAssetGeneration handles ClaimPhaseStartProvider.
// Source-image access is resolved FIRST — artifact lookup + minting the
// short-lived URL are purely local operations, and a failure here is an
// ORDINARY retryable local failure (RequeueForRetry below), never
// touching the provider timeline. ProviderStartedAt is set as the LAST
// local step, immediately before the POST — never earlier. This keeps
// the genuinely ambiguous window to exactly "POST sent, event id not yet
// durably recorded," nothing broader.
func (s *Service) startAndResumeAssetGeneration(ctx context.Context, job SpatialAssetGenerationJob, workerID string, leaseLost chan struct{}) error {
	access, err := s.resolveAssetGenerationSourceAccess(ctx, job)
	if err != nil {
		// Purely local/infra failure BEFORE ProviderStartedAt is ever
		// set — ordinary bounded retry, per spec §8's corrected table.
		return s.assetGenerationJobs.RequeueForRetry(ctx, job.ID, workerID, time.Now().Add(30*time.Second))
	}
	if !leaseStillHeld(leaseLost) {
		return fmt.Errorf("spatial: lease lost before provider call could begin, job %s", job.ID)
	}

	// ProviderStartedAt set HERE — the last local step before the POST,
	// after everything above has already succeeded.
	now := time.Now()
	if err := s.assetGenerationJobs.SetProviderStarted(ctx, job.ID, workerID, now); err != nil {
		return err // fenced — if this worker's lease is gone, the write itself already failed cleanly
	}

	ref, err := s.assetGenerationProvider.StartShapeGeneration(ctx, AssetGenerationSource{URL: access.URL}, job.Seed)
	if err != nil {
		// Ambiguous: the POST may or may not have been received by the
		// provider. ProviderStartedAt is already durably set — this job
		// is now PERMANENTLY excluded from auto-reclaim (repository
		// claim-query invariant) regardless of what we do here.
		return s.assetGenerationJobs.MarkNeedsAttention(ctx, job.ID, workerID, "provider_outcome_unknown", err.Error())
	}
	if err := s.assetGenerationJobs.SetProviderRequestID(ctx, job.ID, workerID, ref.ID); err != nil {
		return err
	}
	job.ProviderRequestID = &ref.ID
	return s.resumeAssetGeneration(ctx, job, workerID, leaseLost)
}

func (s *Service) assetGenerationSourceAccessTTLOrDefault() time.Duration {
	return defaultAssetGenerationSourceAccessTTL
}

// resumeAssetGeneration handles ClaimPhaseResumeProvider — NEVER calls
// StartShapeGeneration. Polls ResumeShapeGeneration until a terminal
// result or the provider timeout elapses, checking leaseLost between
// polls so a worker that lost its lease stops polling rather than racing
// a checkpoint write it would lose.
//
// The deadline it polls against is ctx's OWN deadline when the caller set
// one (a bounded Vercel-safe worker step, ctx capped at ~45s — see
// ProcessOneAssetGenerationJobBounded), falling back to the configured
// provider timeout (default 9 minutes) only when ctx carries no deadline
// at all — the long-lived local dispatch loop's own case
// (StartAssetGenerationDispatchLoop's background context has none). This
// single deadline choice is what makes "caller deadline" and "provider
// timeout" the SAME code path rather than two logics that could drift.
//
// Critically: hitting ctx's OWN deadline is treated as an ordinary
// "still pending, reconnectable" outcome — the job's lease is simply
// released (never MarkNeedsAttention) so the NEXT wake/dispatch call
// resumes polling the SAME stored event ID, matching the plan's exact
// requirement ("A pending SSE read releases the job for another queue
// delivery without calling Hunyuan StartShapeGeneration again"). Only a
// genuine PROVIDER-side error (ResumeShapeGeneration failing for a reason
// OTHER than ctx's own cancellation) is ambiguous enough to warrant
// needs_attention.
func (s *Service) resumeAssetGeneration(ctx context.Context, job SpatialAssetGenerationJob, workerID string, leaseLost chan struct{}) error {
	deadline, ownsDeadline := ctx.Deadline()
	if !ownsDeadline {
		timeout := s.assetGenerationProviderTimeout
		if timeout <= 0 {
			timeout = defaultAssetGenerationProviderTimeout
		}
		deadline = time.Now().Add(timeout)
	}
	ref := ProviderGenerationRef{ID: *job.ProviderRequestID}

	for time.Now().Before(deadline) {
		if !leaseStillHeld(leaseLost) {
			return fmt.Errorf("spatial: lease lost while resuming provider generation, job %s", job.ID)
		}
		result, err := s.assetGenerationProvider.ResumeShapeGeneration(ctx, ref)
		if err != nil {
			if ctx.Err() != nil {
				// Our own bound (whether ctx's caller-set deadline or a
				// mid-call cancellation) expired — the provider itself was
				// never actually told anything is wrong. Release the lease
				// cleanly; the stored ProviderRequestID is untouched, so the
				// next claim resumes from exactly here, never re-invoking
				// StartShapeGeneration.
				return s.releaseAssetGenerationLeaseForReconnect(ctx, job.ID, workerID)
			}
			return s.assetGenerationJobs.MarkNeedsAttention(ctx, job.ID, workerID, "provider_outcome_unknown", err.Error())
		}
		switch result.Status {
		case ProviderGenerationPending:
			if !s.sleepOrDeadline(ctx, assetGenerationResumePollInterval, deadline) {
				return s.releaseAssetGenerationLeaseForReconnect(ctx, job.ID, workerID)
			}
			continue
		case ProviderGenerationQuotaBlocked:
			return s.assetGenerationJobs.MarkNeedsAttention(ctx, job.ID, workerID, "zero_gpu_quota", result.FailureMessage)
		case ProviderGenerationRejected:
			return s.assetGenerationJobs.MarkFailed(ctx, job.ID, workerID, "provider_rejected", result.FailureMessage)
		case ProviderGenerationCompleted:
			return s.stageGeneratedOutput(ctx, job, workerID, result.Output)
		}
	}
	if ownsDeadline {
		// ctx's own bounded deadline was reached with the job still
		// pending — a normal, reconnectable outcome, not a failure.
		return s.releaseAssetGenerationLeaseForReconnect(ctx, job.ID, workerID)
	}
	return s.assetGenerationJobs.MarkNeedsAttention(ctx, job.ID, workerID, "provider_outcome_unknown", "resume polling exceeded the configured provider timeout")
}

// releaseAssetGenerationLeaseForReconnect clears this worker's lease
// without touching status/checkpoints — job.Execution.Status stays
// Processing but is immediately reclaimable again (RenewLease is the only
// thing keeping a Processing job's lease alive; releasing it here makes
// the very next ClaimNext call able to pick this job back up, since
// isSafeToClaim only excludes a job whose lease is CURRENTLY held). Uses
// RequeueForRetry's own lease-clearing path but with availableAt=now (no
// artificial backoff — the plan wants the next queue delivery, not a
// delay) and does not increment Attempt (this was never a failure to
// retry, just an interrupted resume).
func (s *Service) releaseAssetGenerationLeaseForReconnect(ctx context.Context, jobID, workerID string) error {
	return s.assetGenerationJobs.RenewLease(ctx, jobID, workerID, time.Now())
}

// sleepOrDeadline sleeps for interval, but returns early (false) if ctx is
// cancelled or the given deadline would be exceeded first — never
// overshoots a caller-bounded worker step just because the poll interval
// happened to be the last thing running when time ran out.
func (s *Service) sleepOrDeadline(ctx context.Context, interval time.Duration, deadline time.Time) bool {
	if remaining := time.Until(deadline); remaining < interval {
		interval = remaining
	}
	if interval <= 0 {
		return false
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// stageGeneratedOutput stores raw bytes THE INSTANT they're fully
// received, BEFORE any validation — this is the GeneratedObjectKey
// checkpoint. Once this succeeds, the provider is never called again for
// this job under any circumstances. The provider's output is read via a
// BOUNDED reader (io.LimitReader before io.ReadAll) — never an unbounded
// read of a stream this Go process does not control the size of.
func (s *Service) stageGeneratedOutput(ctx context.Context, job SpatialAssetGenerationJob, workerID string, output io.ReadCloser) error {
	defer output.Close()
	buf, err := io.ReadAll(io.LimitReader(output, maxGeneratedBytes+1))
	if err != nil {
		return err
	}
	if int64(len(buf)) > maxGeneratedBytes {
		return s.assetGenerationJobs.MarkFailed(ctx, job.ID, workerID, "generated_glb_invalid", "provider output exceeded the maximum allowed size")
	}
	sum := sha256.Sum256(buf)
	checksum := hex.EncodeToString(sum[:])
	key := fmt.Sprintf("asset-generation/%s/%s/raw/%s.glb", job.CompanyID, job.ID, checksum)
	if _, err := s.assetGenerationStagingStore.Put(ctx, key, bytes.NewReader(buf)); err != nil {
		return err
	}
	if err := s.assetGenerationJobs.SetGenerated(ctx, job.ID, workerID, key, checksum, int64(len(buf))); err != nil {
		return err
	}
	job.GeneratedObjectKey = key
	return s.validateAndPublishAssetGeneration(ctx, job, workerID)
}

// validateAndPublishAssetGeneration handles ClaimPhaseResumeValidation —
// reads the ALREADY-STAGED bytes, never touches the provider. Safe to
// retry indefinitely (RP4E0 spec §8: "storage/publication failure AFTER
// GeneratedObjectKey is set" -> RequeueForRetry, never re-invokes
// StartShapeGeneration). The staged bytes are read via a BOUNDED reader,
// matching stageGeneratedOutput's own bound.
func (s *Service) validateAndPublishAssetGeneration(ctx context.Context, job SpatialAssetGenerationJob, workerID string) error {
	reader, err := s.assetGenerationStagingStore.Get(ctx, job.GeneratedObjectKey)
	if err != nil {
		return s.assetGenerationJobs.RequeueForRetry(ctx, job.ID, workerID, time.Now().Add(30*time.Second))
	}
	buf, err := io.ReadAll(io.LimitReader(reader, maxGeneratedBytes+1))
	reader.Close()
	if err != nil {
		return s.assetGenerationJobs.RequeueForRetry(ctx, job.ID, workerID, time.Now().Add(30*time.Second))
	}
	if int64(len(buf)) > maxGeneratedBytes {
		return s.assetGenerationJobs.MarkFailed(ctx, job.ID, workerID, "generated_glb_invalid", "staged object exceeded the maximum allowed size")
	}

	if err := validateVisualAssetContent(VisualAssetFormatGLB, buf); err != nil {
		return s.assetGenerationJobs.MarkFailed(ctx, job.ID, workerID, "generated_glb_invalid", err.Error())
	}
	detail, err := validateGeneratedGeometryWithDetail(buf)
	if err != nil {
		return s.assetGenerationJobs.MarkFailed(ctx, job.ID, workerID, "generated_glb_invalid", err.Error())
	}
	_ = detail // logged by the caller/composition wiring, not asserted on here

	assetID := fmt.Sprintf("genasset_%s", job.ID)
	published, err := s.PublishVisualAssetVersion(ctx, job.CompanyID, assetID, 1, VisualAssetFormatGLB,
		"Generated asset", VisualAssetNormalization{Pivot: VisualAssetPivotCenterBottom}, bytes.NewReader(buf))
	if err != nil {
		return s.assetGenerationJobs.RequeueForRetry(ctx, job.ID, workerID, time.Now().Add(30*time.Second))
	}
	return s.assetGenerationJobs.Complete(ctx, job.ID, workerID, published.AssetID, published.Version)
}
