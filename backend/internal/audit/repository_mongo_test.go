package audit_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/audit"
)

func setupMongoDB(t *testing.T) *mongo.Database {
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

	return client.Database(fmt.Sprintf("audit_test_%d", time.Now().UnixNano()))
}

func newMongoRepo(t *testing.T) *audit.MongoEventRepository {
	t.Helper()
	repo := audit.NewMongoEventRepository(setupMongoDB(t))
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repo
}

func TestEventRepositoryRoundTripAndSubjectListing(t *testing.T) {
	repo := newMongoRepo(t)
	svc := audit.NewService(repo)
	ctx := context.Background()

	if err := svc.RecordQuotationSent(ctx, "company_a", "project_1", "user_1", "quotation_1", "QT-000001", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.RecordClientViewedQuotation(ctx, "company_a", "project_1", "grant_1", "quotation_1", "QT-000001", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, err := repo.ListBySubject(ctx, "company_a", audit.SubjectTypeQuotation, "quotation_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Metadata must survive the BSON round trip.
	var sawQuotationNumber bool
	for _, e := range events {
		if e.Metadata["quotationNumber"] == "QT-000001" {
			sawQuotationNumber = true
		}
	}
	if !sawQuotationNumber {
		t.Fatalf("expected allowlisted metadata to round-trip, got %+v", events)
	}
}

func TestEventRepositoryIsTenantScoped(t *testing.T) {
	repo := newMongoRepo(t)
	svc := audit.NewService(repo)
	ctx := context.Background()

	if err := svc.RecordQuotationSent(ctx, "company_a", "project_1", "user_1", "quotation_1", "QT-000001", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	crossTenant, err := repo.ListBySubject(ctx, "company_b", audit.SubjectTypeQuotation, "quotation_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(crossTenant) != 0 {
		t.Fatalf("expected company_b to see no events, got %d", len(crossTenant))
	}

	byProject, err := repo.ListByProject(ctx, "company_b", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(byProject) != 0 {
		t.Fatalf("expected company_b to see no project events, got %d", len(byProject))
	}
}

// Repeated Client views each create their own event — no collapsing
// (approved product decision).
func TestEventRepositoryRecordsEveryClientView(t *testing.T) {
	repo := newMongoRepo(t)
	svc := audit.NewService(repo)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := svc.RecordClientViewedQuotation(ctx, "company_a", "project_1", "grant_1", "quotation_1", "QT-000001", 1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	events, err := repo.ListBySubject(ctx, "company_a", audit.SubjectTypeQuotation, "quotation_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 separate view events, got %d", len(events))
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the real Service (RecordClientViewedQuotation
// needs no external lookup), matching this file's own convention.
func TestService_DeleteAllForCompany(t *testing.T) {
	repo := newMongoRepo(t)
	svc := audit.NewService(repo)
	ctx := context.Background()

	if err := svc.RecordClientViewedQuotation(ctx, "company_a", "project_1", "grant_1", "quotation_1", "QT-000001", 1); err != nil {
		t.Fatalf("record company_a event: %v", err)
	}
	if err := svc.RecordClientViewedQuotation(ctx, "company_b", "project_2", "grant_2", "quotation_2", "QT-000002", 1); err != nil {
		t.Fatalf("record company_b event: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	eventsA, err := repo.ListBySubject(ctx, "company_a", audit.SubjectTypeQuotation, "quotation_1")
	if err != nil {
		t.Fatalf("ListBySubject company_a: %v", err)
	}
	if len(eventsA) != 0 {
		t.Fatalf("expected 0 remaining company_a events, got %d", len(eventsA))
	}

	eventsB, err := repo.ListBySubject(ctx, "company_b", audit.SubjectTypeQuotation, "quotation_2")
	if err != nil {
		t.Fatalf("ListBySubject company_b: %v", err)
	}
	if len(eventsB) != 1 {
		t.Fatalf("expected company_b's event to be untouched, got %d", len(eventsB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	repo := newMongoRepo(t)
	svc := audit.NewService(repo)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
