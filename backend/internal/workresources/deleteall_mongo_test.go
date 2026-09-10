package workresources_test

import (
	"context"
	"testing"
	"time"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_deleteall_service")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	svc := workresources.NewService(repo, nil, nil, nil)
	ctx := context.Background()
	now := time.Now()

	requirementA := workresources.WorkResourceRequirement{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_1",
		ResourceType: workresources.ResourceTypeTrade, Name: "Tiler",
		Status: workresources.StatusConfirmed, Source: workresources.SourceManual,
		CreatedByUserID: "user_1", CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	}
	if _, err := repo.Create(ctx, requirementA); err != nil {
		t.Fatalf("create company_a requirement: %v", err)
	}
	requirementB := requirementA
	requirementB.CompanyID = "company_b"
	if _, err := repo.Create(ctx, requirementB); err != nil {
		t.Fatalf("create company_b requirement: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	requirementsA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(requirementsA) != 0 {
		t.Fatalf("expected 0 remaining company_a requirements, got %d", len(requirementsA))
	}

	requirementsB, err := repo.ListByProject(ctx, "company_b", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(requirementsB) != 1 {
		t.Fatalf("expected company_b's requirement to be untouched, got %d", len(requirementsB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "workresources_test_deleteall_empty")
	repo := workresources.NewMongoRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	svc := workresources.NewService(repo, nil, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
