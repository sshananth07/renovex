package ai_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/ai"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func newProcessingBatch(operationID, fingerprint string) ai.AIGenerationBatch {
	return ai.AIGenerationBatch{
		CompanyID: "company_a", ProjectID: "project_1",
		Type: ai.BatchTypeSpaceSuggestions, Status: ai.BatchStatusProcessing,
		OperationID: operationID, Provider: "mock", Model: "mock-v1",
		PromptVersion: "spaces-v1", SchemaVersion: 1, InputFingerprint: fingerprint,
		StartedAt: time.Now(), CreatedByUserID: "user_1", CreatedAt: time.Now(),
	}
}

func TestRepoCreateProcessingBatchAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_batch_create")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	created, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindBatchByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Status != ai.BatchStatusProcessing {
		t.Fatalf("expected processing status, got %s", found.Status)
	}
}

func TestRepoFindBatchByOperationIDTenantScoped(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_batch_by_op")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	created, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.FindBatchByOperationID(ctx, "company_a", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected id %s, got %s", created.ID, found.ID)
	}

	_, err = repo.FindBatchByOperationID(ctx, "company_b", "op_1")
	if err != ai.ErrBatchNotFound {
		t.Fatalf("expected ErrBatchNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestRepoUniqueOperationIDPerCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_batch_unique_op")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	if _, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_dup", "fp_1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_dup", "fp_2")); err == nil {
		t.Fatal("expected an error creating a second batch with the same company+operationId")
	}

	// Different company, same operationId — must succeed (index is scoped to company).
	other := newProcessingBatch("op_dup", "fp_1")
	other.CompanyID = "company_b"
	if _, err := repo.CreateProcessingBatch(ctx, other); err != nil {
		t.Fatalf("expected different company to reuse the same operationId, got %v", err)
	}
}

func TestRepoMarkBatchCompletedAndFailed(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_batch_terminal")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	completed, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_complete", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repo.MarkBatchCompleted(ctx, "company_a", completed.ID); err != nil {
		t.Fatalf("unexpected error completing: %v", err)
	}
	found, err := repo.FindBatchByID(ctx, "company_a", completed.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Status != ai.BatchStatusCompleted || found.CompletedAt == nil {
		t.Fatalf("expected completed status with CompletedAt set, got status=%s completedAt=%v", found.Status, found.CompletedAt)
	}

	failed, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_fail", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repo.MarkBatchFailed(ctx, "company_a", failed.ID, "PROVIDER_UNAVAILABLE"); err != nil {
		t.Fatalf("unexpected error failing: %v", err)
	}
	found, err = repo.FindBatchByID(ctx, "company_a", failed.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Status != ai.BatchStatusFailed || found.FailedAt == nil || found.ErrorCode != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("expected failed status with FailedAt/ErrorCode set, got %+v", found)
	}
}

func TestRepoInsertSuggestionsForBatchAndListSuggestionsByBatch(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_suggestions_insert")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	batch, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	suggestions := []ai.AISuggestion{
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
			SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
			InputFingerprint: "fp_1", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
			SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Bath", SpaceType: "bathroom"}},
			InputFingerprint: "fp_1", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batch.ID, suggestions); err != nil {
		t.Fatalf("unexpected error inserting: %v", err)
	}

	list, err := repo.ListSuggestionsByBatch(ctx, "company_a", batch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(list))
	}
}

// TestRepoListSuggestionsByBatchIsOrderedDeterministically guards against a
// real bug found via the M8.5B-A E2E (Task 17): ListSuggestionsByBatch ran
// Find() with no sort at all, so MongoDB's natural cursor order (not
// guaranteed stable, especially after a document is updated by an
// accept/reject) could silently reshuffle suggestions between polls —
// contractor-visible as review cards randomly changing position mid-review.
// Suggestions must always come back ordered by CreatedAt ascending,
// matching the order the AI generated them in.
func TestRepoListSuggestionsByBatchIsOrderedDeterministically(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_suggestions_order")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	batch, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := time.Now()
	suggestions := []ai.AISuggestion{
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
			SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Third", SpaceType: "bedroom"}},
			InputFingerprint: "fp_1", Revision: 1, CreatedAt: base.Add(2 * time.Second), UpdatedAt: base,
		},
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
			SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "First", SpaceType: "kitchen"}},
			InputFingerprint: "fp_1", Revision: 1, CreatedAt: base, UpdatedAt: base,
		},
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
			SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Second", SpaceType: "bathroom"}},
			InputFingerprint: "fp_1", Revision: 1, CreatedAt: base.Add(1 * time.Second), UpdatedAt: base,
		},
	}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batch.ID, suggestions); err != nil {
		t.Fatalf("unexpected error inserting: %v", err)
	}

	list, err := repo.ListSuggestionsByBatch(ctx, "company_a", batch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 suggestions, got %d", len(list))
	}
	got := []string{list[0].SuggestedData.Space.Name, list[1].SuggestedData.Space.Name, list[2].SuggestedData.Space.Name}
	want := []string{"First", "Second", "Third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, got)
		}
	}
}

func TestRepoDeleteSuggestionsByBatchCompensation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_suggestions_compensate")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	batch, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	suggestions := []ai.AISuggestion{{
		CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
		Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
		SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
		InputFingerprint: "fp_1", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batch.ID, suggestions); err != nil {
		t.Fatalf("unexpected error inserting: %v", err)
	}

	if err := repo.DeleteSuggestionsByBatch(ctx, batch.ID); err != nil {
		t.Fatalf("unexpected error compensating: %v", err)
	}

	list, err := repo.ListSuggestionsByBatch(ctx, "company_a", batch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 suggestions after compensation, got %d", len(list))
	}
}

func TestRepoConditionalAcceptByRevisionAndPendingStatus(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_conditional_accept")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	batch, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	suggestions := []ai.AISuggestion{{
		CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
		Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
		SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
		InputFingerprint: "fp_1", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batch.ID, suggestions); err != nil {
		t.Fatalf("unexpected error inserting: %v", err)
	}
	list, err := repo.ListSuggestionsByBatch(ctx, "company_a", batch.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("unexpected error/len listing: %v / %d", err, len(list))
	}
	suggestionID := list[0].ID

	// Wrong expected revision must fail (CAS).
	err = repo.ConditionalAccept(ctx, "company_a", suggestionID, 99, "domain_obj_1")
	if err != ai.ErrSuggestionRevisionMismatch {
		t.Fatalf("expected ErrSuggestionRevisionMismatch, got %v", err)
	}

	if err := repo.ConditionalAccept(ctx, "company_a", suggestionID, 1, "domain_obj_1"); err != nil {
		t.Fatalf("unexpected error accepting: %v", err)
	}

	updated, err := repo.FindSuggestionByID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != ai.SuggestionStatusAccepted {
		t.Fatalf("expected accepted status, got %s", updated.Status)
	}
	if updated.AcceptedDomainObjectID != "domain_obj_1" {
		t.Fatalf("expected AcceptedDomainObjectID domain_obj_1, got %s", updated.AcceptedDomainObjectID)
	}
	if updated.Revision != 2 {
		t.Fatalf("expected revision incremented to 2, got %d", updated.Revision)
	}

	// Retry with the OLD revision now fails — already terminal/changed.
	err = repo.ConditionalAccept(ctx, "company_a", suggestionID, 1, "domain_obj_1")
	if err != ai.ErrSuggestionRevisionMismatch {
		t.Fatalf("expected ErrSuggestionRevisionMismatch on stale retry, got %v", err)
	}
}

func TestRepoConditionalRejectByRevisionAndPendingStatus(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_conditional_reject")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	batch, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	suggestions := []ai.AISuggestion{{
		CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
		Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
		SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
		InputFingerprint: "fp_1", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batch.ID, suggestions); err != nil {
		t.Fatalf("unexpected error inserting: %v", err)
	}
	list, _ := repo.ListSuggestionsByBatch(ctx, "company_a", batch.ID)
	suggestionID := list[0].ID

	if err := repo.ConditionalReject(ctx, "company_a", suggestionID, 1); err != nil {
		t.Fatalf("unexpected error rejecting: %v", err)
	}
	updated, err := repo.FindSuggestionByID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != ai.SuggestionStatusRejected {
		t.Fatalf("expected rejected status, got %s", updated.Status)
	}

	err = repo.ConditionalReject(ctx, "company_a", suggestionID, 1)
	if err != ai.ErrSuggestionRevisionMismatch {
		t.Fatalf("expected ErrSuggestionRevisionMismatch on already-terminal retry, got %v", err)
	}
}

func TestRepoConditionalModifyByRevisionAndPendingStatus(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_conditional_modify")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	batch, err := repo.CreateProcessingBatch(ctx, newProcessingBatch("op_1", "fp_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	suggestions := []ai.AISuggestion{{
		CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
		Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
		SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
		InputFingerprint: "fp_1", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batch.ID, suggestions); err != nil {
		t.Fatalf("unexpected error inserting: %v", err)
	}
	list, _ := repo.ListSuggestionsByBatch(ctx, "company_a", batch.ID)
	suggestionID := list[0].ID

	if err := repo.ConditionalModify(ctx, "company_a", suggestionID, 1, "domain_obj_1"); err != nil {
		t.Fatalf("unexpected error modifying: %v", err)
	}
	updated, err := repo.FindSuggestionByID(ctx, "company_a", suggestionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != ai.SuggestionStatusModified {
		t.Fatalf("expected modified status, got %s", updated.Status)
	}
	if updated.AcceptedDomainObjectID != "domain_obj_1" {
		t.Fatalf("expected AcceptedDomainObjectID domain_obj_1, got %s", updated.AcceptedDomainObjectID)
	}
}

func TestRepoListBatchesByProjectNewestFirst(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_list_batches")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	ctx := context.Background()
	// ListBatchesByProject sorts by createdAt, not startedAt — both must be
	// set with a real gap (not just StartedAt) or two batches created in the
	// same wall-clock instant tie on createdAt and the ordering assertion
	// below becomes insertion-order-dependent instead of deterministic.
	first := newProcessingBatch("op_1", "fp_1")
	first.StartedAt = time.Now().Add(-time.Hour)
	first.CreatedAt = time.Now().Add(-time.Hour)
	if _, err := repo.CreateProcessingBatch(ctx, first); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second := newProcessingBatch("op_2", "fp_2")
	second.StartedAt = time.Now()
	second.CreatedAt = time.Now()
	if _, err := repo.CreateProcessingBatch(ctx, second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, err := repo.ListBatchesByProject(ctx, "company_a", "project_1", ai.BatchTypeSpaceSuggestions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(list))
	}
	if list[0].OperationID != "op_2" {
		t.Fatalf("expected newest first (op_2), got %s", list[0].OperationID)
	}
}
