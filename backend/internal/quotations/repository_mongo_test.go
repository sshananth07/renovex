package quotations_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
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

func sampleQuotation(companyID, projectID, quotationNumber string, version int, status quotations.QuotationStatus) quotations.Quotation {
	return quotations.Quotation{
		CompanyID: companyID, ProjectID: projectID, ClientID: "client_1", EstimateID: "estimate_1",
		QuotationNumber: quotationNumber, Version: version, Status: status, Revision: 0,
		Currency: "MYR",
		Lines: []quotations.QuotationLine{
			{ID: "line_1", SourceWorkItemIDs: []string{"work_1"}, Description: "Wall Tiles", Amount: money.New(620000, "MYR"), SortOrder: 0},
		},
		Subtotal: money.New(620000, "MYR"), GeneratedSubtotal: money.New(620000, "MYR"),
		TaxMode:       quotations.TaxModeNone,
		Total:         money.New(620000, "MYR"),
		CreatedAt:     time.Now(),
		SchemaVersion: 1,
	}
}

func TestQuotationRepositoryCreateAndFind(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_create_find")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected a generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding quotation: %v", err)
	}
	if found.QuotationNumber != "QT-000001" {
		t.Fatalf("expected QuotationNumber QT-000001, got %s", found.QuotationNumber)
	}
	if len(found.Lines) != 1 || found.Lines[0].SourceWorkItemIDs[0] != "work_1" {
		t.Fatalf("expected round-tripped SourceWorkItemIDs, got %+v", found.Lines)
	}
}

func TestQuotationRepositoryFindByIDCrossTenant(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_cross_tenant")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != quotations.ErrQuotationNotFound {
		t.Fatalf("expected ErrQuotationNotFound for a cross-tenant lookup, got %v", err)
	}
}

func TestQuotationRepositoryUniqueVersionIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_unique_version")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	v1 := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusFinalized)
	if _, err := repo.Create(ctx, v1); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}

	// A second Version=1 attempt for the same {companyId, quotationNumber}
	// collides on uq_quotations_company_number_version.
	dup := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusFinalized)
	_, err := repo.Create(ctx, dup)
	if err != quotations.ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict for a duplicate version number, got %v", err)
	}
}

func TestQuotationRepositoryOneDraftPerNumberPartialIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_one_draft")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	draft1 := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	if _, err := repo.Create(ctx, draft1); err != nil {
		t.Fatalf("unexpected error creating first draft: %v", err)
	}

	// A second draft for the SAME quotationNumber (different Version, so it
	// would NOT collide on the version-unique index) must still collide on
	// the partial one-draft-per-number index.
	draft2 := sampleQuotation("company_a", "project_1", "QT-000001", 2, quotations.QuotationStatusDraft)
	_, err := repo.Create(ctx, draft2)
	if err != quotations.ErrDraftAlreadyExists {
		t.Fatalf("expected ErrDraftAlreadyExists for a second draft in the same chain, got %v", err)
	}

	// A second FINALIZED version for the same chain must NOT be blocked by
	// the partial index (it only constrains status=draft documents).
	finalizedV2 := sampleQuotation("company_a", "project_1", "QT-000001", 2, quotations.QuotationStatusFinalized)
	if _, err := repo.Create(ctx, finalizedV2); err != nil {
		t.Fatalf("expected a second FINALIZED version to be allowed, got error: %v", err)
	}
}

func TestQuotationRepositoryReplaceLinesRevisionGuard(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_replace_lines_revision")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	updated := created
	updated.Subtotal = money.New(700000, "MYR")
	updated.Total = money.New(700000, "MYR")
	_, err = repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error on a valid revision: %v", err)
	}

	// A stale (already-consumed) revision must be rejected.
	_, err = repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, updated)
	if err != quotations.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch on a stale revision, got %v", err)
	}
}

func TestQuotationRepositoryReplaceLinesNeverTouchesGeneratedSubtotal(t *testing.T) {
	// Direct proof of design spec §6.3/§28: GeneratedSubtotal must be
	// absent from ReplaceLines' own $set document — asserted here by
	// reading the document back and confirming it still matches the
	// ORIGINAL GeneratedSubtotal even though Subtotal (a DIFFERENT field)
	// was changed in the same call.
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_generated_subtotal_frozen")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	q.GeneratedSubtotal = money.New(620000, "MYR")
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	updated := created
	updated.Subtotal = money.New(500000, "MYR")          // deliberately diverging from GeneratedSubtotal
	updated.GeneratedSubtotal = money.New(999999, "MYR") // even if the caller (buggily) sets this, ReplaceLines must ignore it
	replaced, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if replaced.GeneratedSubtotal.Amount != 620000 {
		t.Fatalf("expected GeneratedSubtotal to remain frozen at 620000, got %d", replaced.GeneratedSubtotal.Amount)
	}
	if replaced.Subtotal.Amount != 500000 {
		t.Fatalf("expected Subtotal to have been updated to 500000, got %d", replaced.Subtotal.Amount)
	}
}

func TestQuotationRepositoryFinalizeFreezesRevision(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_finalize_revision")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	updated := created
	updated.Terms = "50% deposit"
	afterTermsUpdate, err := repo.ReplaceTerms(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if afterTermsUpdate.Revision != 1 {
		t.Fatalf("expected Revision incremented to 1, got %d", afterTermsUpdate.Revision)
	}

	finalized, err := repo.Finalize(ctx, "company_a", created.ID, afterTermsUpdate.Revision, time.Now())
	if err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}
	if finalized.Revision != 1 {
		t.Fatalf("expected Revision to remain frozen at 1 after finalize, got %d", finalized.Revision)
	}
	if finalized.Status != quotations.QuotationStatusFinalized {
		t.Fatal("expected status=finalized")
	}
}

func TestQuotationRepositoryConcurrentVersionCreation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_concurrent_version")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	const n = 5
	for i := 1; i <= n; i++ {
		q := sampleQuotation("company_a", "project_1", "QT-000001", i, quotations.QuotationStatusFinalized)
		if _, err := repo.Create(ctx, q); err != nil {
			t.Fatalf("unexpected error seeding version %d: %v", i, err)
		}
	}

	results := make(chan int, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			version, err := createNextQuotationVersionWithRetry(ctx, repo, "company_a", "project_1", "QT-000001")
			if err != nil {
				errs <- err
				return
			}
			results <- version
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error in concurrent version creation: %v", err)
	}

	seen := make(map[int]bool)
	for v := range results {
		if seen[v] {
			t.Fatalf("duplicate version number %d produced by concurrent creation", v)
		}
		seen[v] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique new version numbers, got %d", n, len(seen))
	}
}

// createNextQuotationVersionWithRetry implements the bounded-retry
// strategy from design spec §27.3 steps 2-4: read MAX(version), attempt
// insert at MAX+1, retry on a version-number duplicate-key race.
func createNextQuotationVersionWithRetry(ctx context.Context, repo *quotations.MongoQuotationRepository, companyID, projectID, quotationNumber string) (int, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		maxVersion, err := repo.FindMaxVersion(ctx, companyID, quotationNumber)
		if err != nil {
			return 0, err
		}
		nextVersion := maxVersion + 1
		q := sampleQuotation(companyID, projectID, quotationNumber, nextVersion, quotations.QuotationStatusFinalized)
		_, err = repo.Create(ctx, q)
		if err == nil {
			return nextVersion, nil
		}
		if err == quotations.ErrVersionConflict {
			continue
		}
		return 0, err
	}
	return 0, quotations.ErrVersionConflict
}

func TestQuotationCounterRepositoryConcurrentAllocation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_counter_concurrent")
	repo := quotations.NewMongoQuotationCounterRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	const n = 10
	results := make(chan int64, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			num, err := repo.NextQuotationNumber(ctx, "company_a")
			if err != nil {
				errs <- err
				return
			}
			results <- num
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error allocating a quotation number: %v", err)
	}

	seen := make(map[int64]bool)
	for num := range results {
		if seen[num] {
			t.Fatalf("duplicate quotation number %d produced by concurrent allocation", num)
		}
		seen[num] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique quotation numbers, got %d", n, len(seen))
	}
}

func TestQuotationCounterRepositoryPerCompanyIsolation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_counter_isolation")
	repo := quotations.NewMongoQuotationCounterRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	numA1, err := repo.NextQuotationNumber(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	numB1, err := repo.NextQuotationNumber(ctx, "company_b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if numA1 != 1 || numB1 != 1 {
		t.Fatalf("expected both companies' first allocation to be 1 (independent per-company counters), got %d and %d", numA1, numB1)
	}
	numA2, err := repo.NextQuotationNumber(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if numA2 != 2 {
		t.Fatalf("expected company_a's second allocation to be 2, got %d", numA2)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes BOTH
// Quotation and QuotationCounter records for companyID. Seeds via both
// repositories directly (matching every sibling test in this file).
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_deleteall_service")
	repo := quotations.NewMongoQuotationRepository(db)
	counterRepo := quotations.NewMongoQuotationCounterRepository(db)
	svc := quotations.NewService(repo, counterRepo, nil, nil, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleQuotation("company_a", "project_1", "Q-0001", 1, quotations.QuotationStatusDraft)); err != nil {
		t.Fatalf("create company_a quotation: %v", err)
	}
	if _, err := repo.Create(ctx, sampleQuotation("company_b", "project_2", "Q-0001", 1, quotations.QuotationStatusDraft)); err != nil {
		t.Fatalf("create company_b quotation: %v", err)
	}
	if _, err := counterRepo.NextQuotationNumber(ctx, "company_a"); err != nil {
		t.Fatalf("allocate company_a counter: %v", err)
	}
	if _, err := counterRepo.NextQuotationNumber(ctx, "company_b"); err != nil {
		t.Fatalf("allocate company_b counter: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a quotations, got %d", len(listA))
	}
	// company_a's counter was deleted, so the next allocation must restart at 1.
	numA, err := counterRepo.NextQuotationNumber(ctx, "company_a")
	if err != nil {
		t.Fatalf("NextQuotationNumber company_a after delete: %v", err)
	}
	if numA != 1 {
		t.Fatalf("expected company_a's counter to restart at 1 after DeleteAllForCompany, got %d", numA)
	}

	listB, err := repo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's quotation to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_deleteall_empty")
	repo := quotations.NewMongoQuotationRepository(db)
	counterRepo := quotations.NewMongoQuotationCounterRepository(db)
	svc := quotations.NewService(repo, counterRepo, nil, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
