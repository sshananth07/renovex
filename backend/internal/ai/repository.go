package ai

import "context"

// Repository persists AIGenerationBatch and AISuggestion records.
// internal/ai owns the ai_generation_batches and ai_suggestions collections
// exclusively.
type Repository interface {
	// CreateProcessingBatch inserts a new batch in status=processing. The
	// unique companyId+operationId index enforces generation idempotency at
	// this layer (design doc §18.1): a duplicate insert attempt surfaces as
	// a plain error the Service maps by re-reading the existing batch, not
	// as a typed sentinel here — the CREATE path racing another CREATE is
	// symmetric with any other unique-index collision in this codebase.
	CreateProcessingBatch(ctx context.Context, batch AIGenerationBatch) (AIGenerationBatch, error)
	FindBatchByOperationID(ctx context.Context, companyID, operationID string) (AIGenerationBatch, error)
	FindBatchByID(ctx context.Context, companyID, batchID string) (AIGenerationBatch, error)
	ListBatchesByProject(ctx context.Context, companyID, projectID string, batchType BatchType) ([]AIGenerationBatch, error)

	// InsertSuggestionsForBatch inserts suggestions and returns them with
	// their generated IDs populated, in the same order, so the caller's
	// in-memory GenerationResult reflects real, queryable suggestion IDs
	// rather than the pre-insert zero-value ID.
	InsertSuggestionsForBatch(ctx context.Context, batchID string, suggestions []AISuggestion) ([]AISuggestion, error)
	ListSuggestionsByBatch(ctx context.Context, companyID, batchID string) ([]AISuggestion, error)
	FindSuggestionByID(ctx context.Context, companyID, suggestionID string) (AISuggestion, error)

	// ConditionalAccept/Modify/Reject perform the CAS write described in the
	// design doc §10.1: they succeed only when the suggestion is currently
	// pending AND its stored revision equals expectedRevision, otherwise
	// they return ErrSuggestionRevisionMismatch. All three increment
	// revision on success.
	ConditionalAccept(ctx context.Context, companyID, suggestionID string, expectedRevision int64, acceptedDomainObjectID string) error
	ConditionalModify(ctx context.Context, companyID, suggestionID string, expectedRevision int64, acceptedDomainObjectID string) error
	ConditionalReject(ctx context.Context, companyID, suggestionID string, expectedRevision int64) error

	MarkBatchCompleted(ctx context.Context, companyID, batchID string) error
	MarkBatchFailed(ctx context.Context, companyID, batchID, errorCode string) error

	// DeleteSuggestionsByBatch is the compensation action for a generation
	// that fails after suggestions were inserted but before the batch could
	// be marked completed/failed cleanly (design doc §17): it guarantees
	// zero partially usable suggestions survive.
	DeleteSuggestionsByBatch(ctx context.Context, batchID string) error
}
