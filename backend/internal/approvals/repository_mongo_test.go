package approvals_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/approvals"
)

func setupMongoDB(t *testing.T) *mongo.Database {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("approvals_test_%d", time.Now().UnixNano()))
}

func newMongoRepo(t *testing.T) *approvals.MongoApprovalRepository {
	t.Helper()
	repo := approvals.NewMongoApprovalRepository(setupMongoDB(t))
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repo
}

func sampleApproval(companyID, subjectID string, status approvals.ApprovalStatus) approvals.Approval {
	return approvals.Approval{
		CompanyID: companyID, SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: subjectID, SubjectGroupKey: "quotation:QT-000001",
		ActorType: approvals.ActorTypeClient, ActorName: "Ahmad",
		Status: status, Comment: "note", AccessGrantID: "grant_1",
		Revision: 0, DecidedAt: time.Now(), SchemaVersion: 1,
	}
}

func TestApprovalRepositoryCreateAndFindRoundTrip(t *testing.T) {
	repo := newMongoRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusRejected))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.FindBySubject(ctx, "company_a", approvals.SubjectTypeQuotation, "quotation_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID || found.Status != approvals.ApprovalStatusRejected || found.Comment != "note" {
		t.Fatalf("round-trip mismatch: %+v", found)
	}
}

func TestApprovalRepositoryIsTenantScoped(t *testing.T) {
	repo := newMongoRepo(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusAccepted)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.FindBySubject(ctx, "company_b", approvals.SubjectTypeQuotation, "quotation_1"); err != approvals.ErrApprovalNotFound {
		t.Fatalf("expected ErrApprovalNotFound cross-tenant, got %v", err)
	}
}

func TestApprovalRepositoryOneDocumentPerSubject(t *testing.T) {
	repo := newMongoRepo(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusRejected)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusAccepted))
	if err != approvals.ErrApprovalAlreadyExists {
		t.Fatalf("expected ErrApprovalAlreadyExists on the uq_approvals_subject collision, got %v", err)
	}
}

func TestApprovalRepositoryUpdateDecisionIsRevisionGuarded(t *testing.T) {
	repo := newMongoRepo(t)
	ctx := context.Background()

	created, _ := repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusRejected))

	stale := created
	stale.Status = approvals.ApprovalStatusAccepted
	if _, err := repo.UpdateDecision(ctx, "company_a", approvals.SubjectTypeQuotation, "quotation_1", 99, stale); err != approvals.ErrApprovalRevisionMismatch {
		t.Fatalf("expected ErrApprovalRevisionMismatch on stale revision, got %v", err)
	}

	updated, err := repo.UpdateDecision(ctx, "company_a", approvals.SubjectTypeQuotation, "quotation_1", 0, stale)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != approvals.ApprovalStatusAccepted || updated.Revision != 1 {
		t.Fatalf("expected accepted at revision 1, got %+v", updated)
	}
}

// Concurrent first-decision attempts must yield exactly one created document;
// every loser sees ErrApprovalAlreadyExists (which the service resolves by
// re-reading, never surfacing it to a caller).
func TestApprovalRepositoryConcurrentCreateHasOneWinner(t *testing.T) {
	repo := newMongoRepo(t)
	ctx := context.Background()

	const goroutines = 6
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusAccepted))
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch err {
		case nil:
			winners++
		case approvals.ErrApprovalAlreadyExists:
			// expected loser
		default:
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winning create, got %d", winners)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) using the sampleApproval helper.
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupMongoDB(t)
	repo := approvals.NewMongoApprovalRepository(db)
	svc := approvals.NewService(repo)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleApproval("company_a", "quotation_1", approvals.ApprovalStatusAccepted)); err != nil {
		t.Fatalf("create company_a approval: %v", err)
	}
	if _, err := repo.Create(ctx, sampleApproval("company_b", "quotation_2", approvals.ApprovalStatusAccepted)); err != nil {
		t.Fatalf("create company_b approval: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, err := repo.FindBySubject(ctx, "company_a", string(approvals.SubjectTypeQuotation), "quotation_1"); err != approvals.ErrApprovalNotFound {
		t.Fatalf("expected ErrApprovalNotFound for company_a's deleted approval, got %v", err)
	}
	if _, err := repo.FindBySubject(ctx, "company_b", string(approvals.SubjectTypeQuotation), "quotation_2"); err != nil {
		t.Fatalf("expected company_b's approval to be untouched, got %v", err)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupMongoDB(t)
	repo := approvals.NewMongoApprovalRepository(db)
	svc := approvals.NewService(repo)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
