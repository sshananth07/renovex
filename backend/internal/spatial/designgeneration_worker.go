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
	defaultDesignGenerationWorkerTimeout = 30 * time.Second
	maxReferenceImageDecodeBytes         = 20 * 1024 * 1024
)

// ErrDesignGenerationWorkerNotConfigured is returned when
// ProcessOneDesignGenerationAttempt is called before
// SetDesignGenerationWorkerSupport has been called — distinct from
// ErrDesignGenerationSupportNotConfigured (Gate 1's public-surface
// repositories), matching the two-setter split documented on
// SetDesignGenerationWorkerSupport.
var ErrDesignGenerationWorkerNotConfigured = errors.New("spatial: design generation worker support not configured")

// ProcessOneDesignGenerationAttempt claims one attempt eligible for the
// reference-generation phase (if any) and advances it exactly one bounded
// step: reference generation, then immediate RP4E0 submission (both
// necessarily happen in the same call — a reference image with nowhere to
// go is pointless, and the plan's own note that "the Gradio event can
// expire if submission is not followed promptly" means the two must not be
// split across separate wake cycles). Returns (false, nil) if nothing was
// claimable — the normal idle case, not an error. Once an attempt reaches
// asset_generation_pending, this function's job for that attempt is done;
// RP4E0's own ProcessOneAssetGenerationJob (Gate 4's separate wake path)
// carries the linked job the rest of the way to concept_ready via
// CompleteWithConcept.
func (s *Service) ProcessOneDesignGenerationAttempt(ctx context.Context, workerID string) (bool, error) {
	if s.designGenerationAttempts == nil {
		return false, ErrDesignGenerationSupportNotConfigured
	}
	if s.designReferenceGenerator == nil || s.designReferenceStore == nil || s.designReferenceSourceAccess == nil {
		return false, ErrDesignGenerationWorkerNotConfigured
	}

	attempt, err := s.designGenerationAttempts.ClaimNextGenerationPhase(ctx)
	if errors.Is(err, ErrDesignGenerationNotClaimable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	timeout := s.designGenerationWorkerTimeout
	if timeout <= 0 {
		timeout = defaultDesignGenerationWorkerTimeout
	}
	boundedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return true, s.runDesignGenerationReferencePhase(boundedCtx, attempt)
}

// runDesignGenerationReferencePhase resolves the turn's validated plan,
// calls the reference-image generator, stores the validated result under
// R2, and — on success — immediately submits the RP4E0 asset generation
// job. Every local step happens BEFORE ReferenceProviderStartedAt is set,
// mirroring startAndResumeAssetGeneration's own "resolve everything local
// first, mark the provider-call checkpoint as the LAST local step"
// discipline exactly, so the genuinely ambiguous window is never wider
// than "reference call made, no durable outcome recorded yet."
func (s *Service) runDesignGenerationReferencePhase(ctx context.Context, attempt DesignGenerationAttempt) error {
	turn, err := s.designTurns.FindTurn(ctx, attempt.CompanyID, attempt.TurnID)
	if err != nil {
		return s.failDesignGenerationAttempt(ctx, attempt, "design_turn_not_found", err.Error())
	}
	if turn.ValidatedPlan == nil || turn.ValidatedPlan.WorkingDesign.Geometry == nil {
		// A geometry/mixed attempt's turn must have a validated plan with a
		// geometry section by construction (classifyGenerationKind never
		// returns Geometry/Mixed otherwise) — reaching this means the data
		// is inconsistent, not a transient failure.
		return s.failDesignGenerationAttempt(ctx, attempt, "design_plan_missing_geometry", "validated plan has no geometry section")
	}

	req := ReferenceImageGenerationRequest{
		DesignSessionID: attempt.SessionID, TurnID: attempt.TurnID, PlanFingerprint: attempt.PlanFingerprint,
		Target: ReferenceImageTarget{
			Kind: attempt.TargetSnapshot.Kind, ID: attempt.TargetSnapshot.ID,
			Category: attempt.TargetSnapshot.Category, Dimensions: attempt.TargetSnapshot.Dimensions,
		},
		Geometry:      *turn.ValidatedPlan.WorkingDesign.Geometry,
		Appearance:    attempt.Candidate.Appearance,
		PromptVersion: designReferencePromptVersion,
		Seed:          designReferenceSeed(attempt.ID),
	}

	// ReferenceProviderStartedAt set as the LAST local step before the
	// call — after the turn/plan lookup above has already succeeded.
	now := time.Now()
	if err := s.designGenerationAttempts.SetReferenceProviderStarted(ctx, attempt.CompanyID, attempt.ID, now); err != nil {
		return err // fenced — a cancel that landed first makes this a clean no-op error, never a lost write
	}

	result, err := s.designReferenceGenerator.GenerateReference(ctx, req)
	if err != nil {
		// Ambiguous: the outbound call may or may not have reached the
		// provider. ReferenceProviderStartedAt is already durably set, so
		// this attempt is permanently excluded from
		// ClaimNextGenerationPhase's dangerous-state guard regardless of
		// what happens next — needs_attention communicates that honestly
		// rather than silently retrying.
		return s.failDesignGenerationAttempt(ctx, attempt, "reference_generation_failed", err.Error())
	}

	width, height, err := decodeImageDimensions(result.ImageBytes)
	if err != nil {
		return s.failDesignGenerationAttempt(ctx, attempt, "reference_image_invalid", err.Error())
	}

	sum := sha256.Sum256(result.ImageBytes)
	checksum := hex.EncodeToString(sum[:])
	objectKey := fmt.Sprintf("design-reference/%s/%s/%s.jpg", attempt.CompanyID, attempt.ID, checksum)
	if _, err := s.designReferenceStore.Put(ctx, objectKey, bytes.NewReader(result.ImageBytes)); err != nil {
		// T2A: R2 persistence failed AFTER the FLUX provider call already
		// succeeded — this must not strand the attempt in generating_reference
		// (where ClaimNextGenerationPhase's dangerous-state guard excludes it
		// from ever being reclaimed, per SpatialAssetGenerationJob's identical
		// "provider call already happened, never reclaim automatically"
		// discipline). Explicitly fail the attempt to needs_attention/
		// reference_storage_failed instead: this releases ActiveSlot (see
		// Fail's $unset), preserves full DesignSession/target/attempt
		// lineage (the attempt record itself, with its SessionID/TurnID/
		// TargetSnapshot, is untouched), and makes the failure visible and
		// bounded rather than silently stuck. It also guarantees Hunyuan is
		// never submitted and GLM/design reasoning is never rerun for this
		// failure — submitDesignReferenceForAssetGeneration below is simply
		// never reached. Manual retry is RegenerateDesignPlan, which already
		// requires the previous attempt to be terminal
		// (isTerminalDesignGenerationStatus includes needs_attention) before
		// creating a brand-new attempt with its own fresh GenerateReference
		// call — the existing generation mechanism, not a new retry path.
		return s.failDesignGenerationAttempt(ctx, attempt, "reference_storage_failed", err.Error())
	}

	referenceImage := DesignReferenceImage{
		ObjectKey: objectKey, ChecksumSHA256: checksum, ContentType: result.ContentType, SizeBytes: int64(len(result.ImageBytes)),
		Width: width, Height: height, Provider: result.Provider, ProviderRequestID: result.ProviderRequestID,
		Model: result.Model, PromptVersion: result.PromptVersion, Seed: result.Seed, CreatedAt: time.Now(),
	}
	if _, err := s.designGenerationAttempts.SetReferenceReady(ctx, attempt.CompanyID, attempt.ID, referenceImage); err != nil {
		return err
	}

	return s.submitDesignReferenceForAssetGeneration(ctx, attempt, referenceImage)
}

// submitDesignReferenceForAssetGeneration creates the RP4E0 job from the
// just-stored reference image and links it onto the attempt — this MUST
// happen in the same call as reference generation succeeding (see this
// file's package doc comment: the plan requires publishing the asset-job
// wake immediately because the underlying Gradio event can expire
// otherwise). ClientRequestID is the plan's exact deterministic format
// ("design-attempt:{attemptId}:hunyuan") so a retried call after a partial
// failure here idempotently adopts the same job rather than creating a
// second one.
func (s *Service) submitDesignReferenceForAssetGeneration(ctx context.Context, attempt DesignGenerationAttempt, referenceImage DesignReferenceImage) error {
	job, err := s.SubmitAssetGenerationJobFromDesignReference(
		ctx, attempt.CompanyID, attempt.ProjectID, attempt.CreatedByUserID,
		fmt.Sprintf("design-attempt:%s:hunyuan", attempt.ID),
		attempt.ID, referenceImage.ObjectKey, referenceImage.ChecksumSHA256, designReferenceSeed(attempt.ID),
	)
	if err != nil {
		return s.failDesignGenerationAttempt(ctx, attempt, "asset_generation_submit_failed", err.Error())
	}
	_, err = s.designGenerationAttempts.SetAssetGenerationPending(ctx, attempt.CompanyID, attempt.ID, job.ID)
	return err
}

// failDesignGenerationAttempt maps any local failure to the attempt's own
// Fail transition — safe_failure_code only, never the underlying error's
// full text (that stays in server logs the caller writes, matching
// design_service.go's finishFailedTurn precedent of never persisting raw
// provider error text onto a client-visible record).
func (s *Service) failDesignGenerationAttempt(ctx context.Context, attempt DesignGenerationAttempt, safeFailureCode, _ string) error {
	_, err := s.designGenerationAttempts.Fail(ctx, attempt.CompanyID, attempt.ID, DesignGenerationStatusNeedsAttention, safeFailureCode)
	return err
}

// designReferencePromptVersion pins the prompt contract version sent to
// the reference-image service — bumped only alongside a matching change to
// ai-service/app/prompts/reference_image.py.
const designReferencePromptVersion = "v1"

// designReferenceSeed derives a deterministic seed from the attempt ID so
// Regenerate producing a NEW attempt number gets a genuinely different
// seed (a new attempt ID), while any retry of the SAME attempt (e.g. a
// resumed worker call) reuses the identical seed — matching RP4E0's own
// "seed is part of the durable idempotency fingerprint" precedent.
func designReferenceSeed(attemptID string) int64 {
	sum := sha256.Sum256([]byte(attemptID))
	seed := int64(0)
	for i := 0; i < 8; i++ {
		seed = seed<<8 | int64(sum[i])
	}
	if seed < 0 {
		seed = -seed
	}
	return seed
}

// decodeImageDimensions mirrors validateSourceArtifact's exact bounded
// decode-and-inspect discipline (assetgeneration_service.go) — the
// reference-image service's own byte-size bound is trusted for the HTTP
// response, but the pixel dimensions are read here because
// ReferenceImageGenerationResult does not carry them (Lane A's contract
// deliberately keeps that result minimal; decoding is this consumer's
// concern, same division of responsibility RP4E0's artifact validation
// already uses).
func decodeImageDimensions(imageBytes []byte) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(io.LimitReader(bytes.NewReader(imageBytes), maxReferenceImageDecodeBytes+1))
	if err != nil {
		return 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, fmt.Errorf("spatial: reference image has invalid dimensions %dx%d", cfg.Width, cfg.Height)
	}
	return cfg.Width, cfg.Height, nil
}
