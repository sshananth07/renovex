package ai_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/ai"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// TestService_DeleteAllForCompany proves Task 1a's multi-collection guidance
// (spec §6.6): ONE Service.DeleteAllForCompany call removes AIGenerationBatch
// and AISuggestion records for companyID — both collections.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_deleteall_service")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	svc := ai.NewService(repo, nil, nil)
	ctx := context.Background()

	batchA, err := repo.CreateProcessingBatch(ctx, ai.AIGenerationBatch{
		CompanyID: "company_a", ProjectID: "project_1",
		Type: ai.BatchTypeSpaceSuggestions, Status: ai.BatchStatusProcessing,
		OperationID: "op_a", Provider: "mock", Model: "mock-v1",
		PromptVersion: "spaces-v1", SchemaVersion: 1, InputFingerprint: "fp_a",
		StartedAt: time.Now(), CreatedByUserID: "user_1", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create company_a batch: %v", err)
	}
	batchB, err := repo.CreateProcessingBatch(ctx, ai.AIGenerationBatch{
		CompanyID: "company_b", ProjectID: "project_1",
		Type: ai.BatchTypeSpaceSuggestions, Status: ai.BatchStatusProcessing,
		OperationID: "op_b", Provider: "mock", Model: "mock-v1",
		PromptVersion: "spaces-v1", SchemaVersion: 1, InputFingerprint: "fp_b",
		StartedAt: time.Now(), CreatedByUserID: "user_1", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create company_b batch: %v", err)
	}

	if _, err := repo.InsertSuggestionsForBatch(ctx, batchA.ID, []ai.AISuggestion{{
		CompanyID: "company_a", ProjectID: "project_1", BatchID: batchA.ID,
		Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
		SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
		InputFingerprint: "fp_a", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}); err != nil {
		t.Fatalf("create company_a suggestion: %v", err)
	}
	if _, err := repo.InsertSuggestionsForBatch(ctx, batchB.ID, []ai.AISuggestion{{
		CompanyID: "company_b", ProjectID: "project_1", BatchID: batchB.ID,
		Type: ai.SuggestionTypeSpace, Status: ai.SuggestionStatusPending,
		SuggestedData:    ai.SuggestedData{Space: &ai.SpaceSuggestionData{Name: "Bath", SpaceType: "bathroom"}},
		InputFingerprint: "fp_b", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}); err != nil {
		t.Fatalf("create company_b suggestion: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, err := repo.FindBatchByID(ctx, "company_a", batchA.ID); err != ai.ErrBatchNotFound {
		t.Fatalf("expected company_a's batch to be deleted, got %v", err)
	}
	suggestionsA, err := repo.ListSuggestionsByBatch(ctx, "company_a", batchA.ID)
	if err != nil {
		t.Fatalf("ListSuggestionsByBatch company_a: %v", err)
	}
	if len(suggestionsA) != 0 {
		t.Fatalf("expected 0 remaining company_a suggestions, got %d", len(suggestionsA))
	}

	if _, err := repo.FindBatchByID(ctx, "company_b", batchB.ID); err != nil {
		t.Fatalf("expected company_b's batch to be untouched, got %v", err)
	}
	suggestionsB, err := repo.ListSuggestionsByBatch(ctx, "company_b", batchB.ID)
	if err != nil {
		t.Fatalf("ListSuggestionsByBatch company_b: %v", err)
	}
	if len(suggestionsB) != 1 {
		t.Fatalf("expected company_b's suggestion to be untouched, got %d", len(suggestionsB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "ai_test_deleteall_empty")
	repo := ai.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	svc := ai.NewService(repo, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
