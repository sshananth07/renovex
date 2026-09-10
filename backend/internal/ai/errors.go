package ai

import "errors"

// ErrBatchNotFound is returned when an AIGenerationBatch lookup finds no
// match — including a batch that exists but belongs to a different company.
var ErrBatchNotFound = errors.New("ai: generation batch not found")

// ErrGenerationInProgress is returned when the same company+operationId+
// fingerprint combination already has a batch in status=processing (design
// doc §16, §18.1): a concurrent duplicate request should wait, not create a
// second batch.
var ErrGenerationInProgress = errors.New("ai: generation already in progress")

// ErrGenerationIdempotencyConflict is returned when the same company+
// operationId is reused with a DIFFERENT input fingerprint (design doc
// §18.1) — an ambiguous client retry must never silently apply to changed
// input.
var ErrGenerationIdempotencyConflict = errors.New("ai: generation idempotency conflict")

// ErrSuggestionNotFound is returned when an AISuggestion lookup finds no
// match — including one that exists but belongs to a different company.
var ErrSuggestionNotFound = errors.New("ai: suggestion not found")

// ErrSuggestionRevisionMismatch is returned by the conditional accept/
// modify/reject operations when the expected revision does not match the
// suggestion's current stored revision, OR the suggestion is not currently
// pending — both collapse to the same optimistic-concurrency-conflict
// sentinel (design doc §10.1, "wrong expectedRevision -> 409").
var ErrSuggestionRevisionMismatch = errors.New("ai: suggestion revision mismatch or not pending")
