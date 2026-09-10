package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// The issuance chain is the SERIALIZATION POINT for version creation
// (design spec §3.1, §10.1). These tests run against real MongoDB because the
// invariant IS the unique index and the conditional update — an in-memory fake
// would assert the test's own logic rather than what the database enforces.

func newChainRepo(t *testing.T, db *mongo.Database) *rfqissuance.MongoIssuanceChainRepository {
	t.Helper()
	repo := rfqissuance.NewMongoIssuanceChainRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

func TestEnsureChainCreatesOnceAndIsIdempotent(t *testing.T) {
	db := setupDB(t)
	repo := newChainRepo(t, db)
	ctx := context.Background()

	first, err := repo.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.ID == "" {
		t.Fatal("the chain must be persisted with an ID")
	}
	if first.LatestIssuedVersion != 0 {
		t.Errorf("LatestIssuedVersion = %d, want 0 before any issuance",
			first.LatestIssuedVersion)
	}

	second, err := repo.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error on the second call: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("EnsureChain created a SECOND chain (%s then %s). One chain per "+
			"companyId + rfqChainId is the serialization point; two would let two "+
			"issuances each believe they allocated version 1", first.ID, second.ID)
	}
}

// Tenant and chain scoping: neither key may be ignored.
func TestEnsureChainIsScopedToCompanyAndChain(t *testing.T) {
	db := setupDB(t)
	repo := newChainRepo(t, db)
	ctx := context.Background()

	a, err := repo.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	otherCompany, err := repo.EnsureChain(ctx, "company-2", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	otherChain, err := repo.EnsureChain(ctx, "company-1", "chain-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a.ID == otherCompany.ID {
		t.Error("two companies share one issuance chain")
	}
	if a.ID == otherChain.ID {
		t.Error("two RFQ chains share one issuance chain")
	}
}

// The chain pointer advances only under the expected revision, so a stale
// caller cannot overwrite a newer issuance's pointer (design spec §11.3).
func TestAdvanceChainRequiresTheExpectedRevision(t *testing.T) {
	db := setupDB(t)
	repo := newChainRepo(t, db)
	ctx := context.Background()

	chain, err := repo.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	advanced, err := repo.AdvanceChain(ctx, "company-1", "chain-1", chain.Revision,
		1, "version-1-id")
	if err != nil {
		t.Fatalf("advancing under the current revision failed: %v", err)
	}
	if advanced.LatestIssuedVersion != 1 {
		t.Errorf("LatestIssuedVersion = %d, want 1", advanced.LatestIssuedVersion)
	}
	if advanced.CurrentIssuedVersionID == nil || *advanced.CurrentIssuedVersionID != "version-1-id" {
		t.Errorf("CurrentIssuedVersionID = %v, want version-1-id",
			advanced.CurrentIssuedVersionID)
	}
	if advanced.Revision == chain.Revision {
		t.Error("Revision must increment so a stale caller cannot match again")
	}

	// The stale revision must now be refused.
	if _, err := repo.AdvanceChain(ctx, "company-1", "chain-1", chain.Revision,
		2, "version-2-id"); !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch for a stale revision", err)
	}
}

// A foreign tenant must not advance this company's chain.
func TestAdvanceChainIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newChainRepo(t, db)
	ctx := context.Background()

	chain, err := repo.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.AdvanceChain(ctx, "company-2", "chain-1", chain.Revision, 1, "version-1-id")

	if err == nil {
		t.Fatal("a foreign company advanced another tenant's issuance chain")
	}
	if !errors.Is(err, rfqissuance.ErrIssuanceChainNotFound) {
		t.Errorf("error = %v, want ErrIssuanceChainNotFound: a foreign tenant must not "+
			"learn that the chain exists", err)
	}
}

// Only ONE of N concurrent advances to the same version may win. This is the
// one-winner guarantee at the chain pointer (design spec §10.1).
func TestConcurrentAdvanceChainHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := newChainRepo(t, db)
	ctx := context.Background()

	chain, err := repo.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const concurrency = 12
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.AdvanceChain(ctx, "company-1", "chain-1", chain.Revision,
				1, "version-1-id")
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, rfqissuance.ErrRevisionMismatch):
			// The expected loss.
		default:
			t.Fatalf("goroutine %d failed unexpectedly: %v", i, err)
		}
	}

	if winners != 1 {
		t.Errorf("got %d winners, want exactly 1. The revision guard is what makes the "+
			"chain pointer a serialization point (design spec §10.1)", winners)
	}
}
