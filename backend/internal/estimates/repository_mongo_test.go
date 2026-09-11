package estimates_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
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

func sampleEstimate(companyID, projectID string, version int, status estimates.EstimateStatus) estimates.Estimate {
	return estimates.Estimate{
		CompanyID: companyID, ProjectID: projectID, Version: version, Status: status, Revision: 0,
		Currency: "MYR",
		Lines: []estimates.EstimateCostLine{
			{SourceCostItemID: "cost_item_1", Category: "material", Description: "Tiles", SnapshottedAmount: money.New(500000, "MYR")},
		},
		CostSubtotal: money.New(500000, "MYR"),
		PricingMode:  estimates.PricingModeMarkup, PricingRate: 2000,
		ProposedSellingPrice: money.New(600000, "MYR"), ProjectedGrossProfit: money.New(100000, "MYR"),
		ProjectedGrossMarginBPS: 1667,
		CreatedAt:               time.Now(), SchemaVersion: 1,
	}
}

func TestEstimateRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_create_find")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Version != 1 || found.Status != estimates.EstimateStatusDraft {
		t.Fatalf("expected Version=1 Status=draft, got %+v", found)
	}
	if len(found.Lines) != 1 || found.Lines[0].Description != "Tiles" {
		t.Fatalf("expected 1 line 'Tiles', got %+v", found.Lines)
	}
	if found.CostSubtotal.Amount != 500000 {
		t.Fatalf("expected CostSubtotal 500000, got %+v", found.CostSubtotal)
	}
}

func TestEstimateRepositoryFindByIDCrossTenant(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_cross_tenant")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != estimates.ErrEstimateNotFound {
		t.Fatalf("expected ErrEstimateNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestEstimateRepositoryListByProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_list_by_project")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_2", 1, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating other project's estimate: %v", err)
	}

	list, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 estimates for project_1, got %d", len(list))
	}
}

func TestEstimateRepositoryFindMaxVersion(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_max_version")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	maxV, err := repo.FindMaxVersion(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error on empty project: %v", err)
	}
	if maxV != 0 {
		t.Fatalf("expected 0 when no estimates exist, got %d", maxV)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	// Insert v3 before v2 to prove MAX(version) is used, not insertion order.
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 3, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v3: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}

	maxV, err = repo.FindMaxVersion(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if maxV != 3 {
		t.Fatalf("expected max version 3, got %d", maxV)
	}
}

func TestEstimateRepositoryFindLatestByProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_find_latest")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.FindLatestByProject(ctx, "company_a", "project_1")
	if err != estimates.ErrNoEstimatesForProject {
		t.Fatalf("expected ErrNoEstimatesForProject on empty project, got %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}

	latest, err := repo.FindLatestByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("expected latest version 2, got %d", latest.Version)
	}
}

func TestEstimateRepositoryVersionUniqueIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_version_unique")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating first v1: %v", err)
	}
	_, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized))
	if err == nil {
		t.Fatal("expected duplicate-key error creating a second version=1 for the same company+project")
	}
	// Create must translate the raw MongoDB duplicate-key error into the
	// DISTINCT ErrVersionConflict sentinel for a version-number collision —
	// not merely satisfy mongo.IsDuplicateKeyError(err), and not be
	// confused with ErrDraftAlreadyExists (the OTHER unique index's
	// collision, tested separately below). This is what lets the service
	// layer (Task 5/7) and HTTP layer (Task 8) distinguish "retry with a
	// fresh version number" from "a draft already exists, do not retry."
	if err != estimates.ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict, got: %v", err)
	}
}

func TestEstimateRepositoryOneDraftPerProjectPartialIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_one_draft")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating first draft: %v", err)
	}
	_, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusDraft))
	if err == nil {
		t.Fatal("expected duplicate-key error creating a second draft for the same company+project")
	}
	// This must classify as the DISTINCT ErrDraftAlreadyExists sentinel,
	// never confused with ErrVersionConflict above — a draft collision is
	// NOT safe to blindly retry (design spec §21.2 step 4), so the two
	// must be distinguishable in production code, not just provable in a
	// standalone test helper.
	if err != estimates.ErrDraftAlreadyExists {
		t.Fatalf("expected ErrDraftAlreadyExists, got: %v", err)
	}

	// A second FINALIZED document for the same project must succeed — the
	// partial index only constrains status=draft documents.
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 3, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("expected a second finalized document to succeed (partial index scoped to draft only), got: %v", err)
	}
}

func TestEstimateRepositoryUpdatePricing(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_update_pricing")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated := created
	updated.PricingMode = estimates.PricingModeMargin
	updated.PricingRate = 2500
	updated.ProposedSellingPrice = money.New(666667, "MYR")
	result, err := repo.UpdatePricing(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error updating pricing: %v", err)
	}
	if result.Revision != created.Revision+1 {
		t.Fatalf("expected Revision incremented by 1, got %d (was %d)", result.Revision, created.Revision)
	}
	if result.PricingMode != estimates.PricingModeMargin || result.PricingRate != 2500 {
		t.Fatalf("expected updated pricing fields to persist, got %+v", result)
	}

	// Stale expectedRevision must fail.
	_, err = repo.UpdatePricing(ctx, "company_a", created.ID, created.Revision, updated)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch on stale revision, got %v", err)
	}
}

func TestEstimateRepositoryFinalize(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_finalize")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	now := time.Now()
	revisionBeforeFinalize := created.Revision
	result, err := repo.Finalize(ctx, "company_a", created.ID, created.Revision, now)
	if err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}
	if result.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Status=finalized, got %s", result.Status)
	}
	if result.FinalizedAt == nil {
		t.Fatal("expected FinalizedAt to be set")
	}
	// Invariant from the approved design spec §16.3: finalize FREEZES
	// Revision at its current value — it must NOT increment it. Draft
	// Revision 5 -> Finalize(expectedRevision=5) -> Finalized Revision 5,
	// never Revision 6.
	if result.Revision != revisionBeforeFinalize {
		t.Fatalf("expected Revision to remain %d after finalize (frozen, not incremented), got %d", revisionBeforeFinalize, result.Revision)
	}

	// Stale expectedRevision against a document that's still a draft would
	// fail; here the document is ALREADY finalized, so a second call with
	// the ORIGINAL (correct pre-finalize) revision must also fail per the
	// repository's strict {status: draft, revision: expected} match — the
	// idempotency carve-out (return unchanged if already finalized) is a
	// SERVICE-layer responsibility (Task 7), not the repository's. The
	// repository itself is a strict conditional-match primitive.
	_, err = repo.Finalize(ctx, "company_a", created.ID, created.Revision, now)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch when re-finalizing at the repository layer (status no longer draft), got %v", err)
	}
}

func TestEstimateRepositoryReplaceSnapshotPersistsCurrencyChange(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_replace_snapshot_currency")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.Currency != "MYR" {
		t.Fatalf("expected initial Currency=MYR, got %s", created.Currency)
	}

	// Simulate a refresh where the Project's eligible CostItems have all
	// become SGD since the draft was created — a valid sequence per the
	// approved design spec §8 ("on refresh, currency is re-inferred from
	// the current eligible CostItems, the same way").
	updated := created
	updated.Currency = "SGD"
	updated.Lines = []estimates.EstimateCostLine{
		{SourceCostItemID: "cost_item_1", Category: "material", Description: "Tiles", SnapshottedAmount: money.New(700000, "SGD")},
	}
	updated.CostSubtotal = money.New(700000, "SGD")
	updated.ProposedSellingPrice = money.New(840000, "SGD")

	result, err := repo.ReplaceSnapshot(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error replacing snapshot: %v", err)
	}
	if result.Currency != "SGD" {
		t.Fatalf("expected Currency updated to SGD after refresh, got %s (Currency was NOT included in the $set)", result.Currency)
	}
	if result.CostSubtotal.Currency != "SGD" || result.ProposedSellingPrice.Currency != "SGD" {
		t.Fatalf("expected CostSubtotal/ProposedSellingPrice to be SGD, got %+v / %+v", result.CostSubtotal, result.ProposedSellingPrice)
	}

	// Re-read from a fresh FindByID to prove this was actually persisted,
	// not merely reflected in the in-memory return value.
	refetched, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error re-fetching: %v", err)
	}
	if refetched.Currency != "SGD" {
		t.Fatalf("expected persisted Currency=SGD on re-fetch, got %s", refetched.Currency)
	}
}

func TestEstimateRepositoryConcurrentVersionCreation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_concurrent_version")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	// Seed N already-FINALIZED "source" estimates under the same project,
	// each at a distinct starting version, so N concurrent "create the next
	// version" attempts can proceed without tripping the one-draft-per-
	// project partial index against each other (that invariant is tested
	// separately in TestEstimateRepositoryOneDraftPerProjectPartialIndex).
	// This isolates proving the {companyId, projectId, version} unique
	// index's bounded-retry race-recovery behavior specifically.
	const n = 5
	for i := 1; i <= n; i++ {
		e := sampleEstimate("company_a", "project_1", i, estimates.EstimateStatusFinalized)
		if _, err := repo.Create(ctx, e); err != nil {
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
			version, err := createNextVersionWithRetry(ctx, repo, "company_a", "project_1")
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
	for v := n + 1; v <= 2*n; v++ {
		if !seen[v] {
			t.Fatalf("expected version %d to have been created, got set %v", v, seen)
		}
	}
}

// createNextVersionWithRetry implements the bounded-retry strategy from
// design spec §21.2 steps 1-3/5-6: read MAX(version), attempt insert at
// MAX+1, retry on a version-number duplicate-key race, bounded at 5
// attempts.
func createNextVersionWithRetry(ctx context.Context, repo *estimates.MongoEstimateRepository, companyID, projectID string) (int, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		maxVersion, err := repo.FindMaxVersion(ctx, companyID, projectID)
		if err != nil {
			return 0, err
		}
		nextVersion := maxVersion + 1
		e := sampleEstimate(companyID, projectID, nextVersion, estimates.EstimateStatusFinalized)
		_, err = repo.Create(ctx, e)
		if err == nil {
			return nextVersion, nil
		}
		if err == estimates.ErrVersionConflict {
			continue // another goroutine won this version number; retry
		}
		return 0, err
	}
	return 0, errors.New("exhausted retry attempts allocating a version number")
}

func TestEstimateRepositoryConcurrentDraftMutation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_concurrent_draft_mutation")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		updated := created
		updated.CostSubtotal = money.New(999999, "MYR")
		_, err := repo.ReplaceSnapshot(ctx, "company_a", created.ID, created.Revision, updated)
		results <- err
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		updated := created
		updated.PricingRate = 3000
		_, err := repo.UpdatePricing(ctx, "company_a", created.ID, created.Revision, updated)
		results <- err
	}()

	wg.Wait()
	close(results)

	successCount := 0
	conflictCount := 0
	for err := range results {
		switch err {
		case nil:
			successCount++
		case estimates.ErrRevisionMismatch:
			conflictCount++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("expected exactly one success and one revision-mismatch conflict, got %d successes and %d conflicts", successCount, conflictCount)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) using the sampleEstimate helper.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_deleteall_service")
	repo := estimates.NewMongoEstimateRepository(db)
	svc := estimates.NewService(repo, nil, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("create company_a estimate: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_b", "project_2", 1, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("create company_b estimate: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a estimates, got %d", len(listA))
	}

	listB, err := repo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's estimate to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_deleteall_empty")
	repo := estimates.NewMongoEstimateRepository(db)
	svc := estimates.NewService(repo, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
