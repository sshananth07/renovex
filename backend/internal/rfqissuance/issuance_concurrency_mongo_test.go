package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// These are service-level concurrency tests over REAL MongoDB. Repository-only
// tests can prove a unique index, but they cannot catch orchestration that hands
// two callers different version numbers and thereby lets both inserts succeed.

type concurrentReadyRFQSource struct {
	snapshot rfqissuance.ReadyRFQSnapshot
}

func (s concurrentReadyRFQSource) GetReadyRFQSnapshot(
	context.Context, string, string,
) (rfqissuance.ReadyRFQSnapshot, bool, error) {
	return s.snapshot, true, nil
}

func newMongoIssuanceService(t *testing.T, db *mongo.Database) (
	*rfqissuance.Service,
	*rfqissuance.MongoIssuedRFQVersionRepository,
	*rfqissuance.MongoIssuanceChainRepository,
) {
	t.Helper()
	versions := rfqissuance.NewMongoIssuedRFQVersionRepository(db)
	chains := rfqissuance.NewMongoIssuanceChainRepository(db)
	ctx := context.Background()
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure version indexes: %v", err)
	}
	if err := chains.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure chain indexes: %v", err)
	}
	svc := rfqissuance.NewService(
		versions,
		rfqissuance.WithReadyRFQSource(concurrentReadyRFQSource{
			snapshot: readySnapshot(futureDeadline()),
		}),
		rfqissuance.WithIssuanceChains(chains),
	)
	return svc, versions, chains
}

// Version 1 is a single transition from the ready M7 RFQ. A second operation
// must go through an amendment draft; allowing two first-issue calls to become
// Versions 1 and 2 would publish an unreviewed duplicate snapshot.
func TestConcurrentInitialIssuanceHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	svc, versions, chains := newMongoIssuanceService(t, db)
	ctx := context.Background()

	start := make(chan struct{})
	results := make([]rfqissuance.IssuedRFQVersion, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, operationID := range []string{"op-concurrent-a", "op-concurrent-b"} {
		wg.Add(1)
		go func(i int, operationID string) {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.IssueVersion(ctx, "company-1", "user-1",
				issueInput(operationID))
		}(i, operationID)
	}
	close(start)
	wg.Wait()

	winners := 0
	conflicts := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
			if results[i].VersionNumber != 1 {
				t.Errorf("winner created Version %d, want Version 1", results[i].VersionNumber)
			}
		case errors.Is(err, rfqissuance.ErrVersionAlreadyExists):
			conflicts++
		default:
			t.Errorf("call %d error = %v, want nil or ErrVersionAlreadyExists", i, err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners/conflicts = %d/%d, want 1/1; both first-issue calls must not succeed",
			winners, conflicts)
	}

	foundOperations := 0
	for _, operationID := range []string{"op-concurrent-a", "op-concurrent-b"} {
		_, found, err := versions.FindByOperationID(ctx, "company-1", operationID)
		if err != nil {
			t.Fatalf("find operation %s: %v", operationID, err)
		}
		if found {
			foundOperations++
		}
	}
	if foundOperations != 1 {
		t.Errorf("persisted versions for %d operations, want exactly 1", foundOperations)
	}

	chain, err := chains.FindChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("find chain: %v", err)
	}
	if chain.LatestIssuedVersion != 1 {
		t.Errorf("LatestIssuedVersion = %d, want 1", chain.LatestIssuedVersion)
	}
	if chain.CurrentIssuedVersionID == nil {
		t.Error("the one winning immutable version must become the chain's current version")
	}
}

// Same-operation concurrency is not a second commercial action. The unique
// index still chooses one insert winner, but both callers must recover that
// winner as the idempotent result.
func TestConcurrentInitialIssuanceWithTheSameOperationConverges(t *testing.T) {
	db := setupDB(t)
	svc, _, _ := newMongoIssuanceService(t, db)
	ctx := context.Background()

	start := make(chan struct{})
	results := make([]rfqissuance.IssuedRFQVersion, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.IssueVersion(ctx, "company-1", "user-1",
				issueInput("op-same"))
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d error = %v, want both retries to recover the winner", i, err)
		}
	}
	if results[0].ID == "" || results[1].ID != results[0].ID {
		t.Errorf("result IDs = %q/%q, want the same persisted immutable version",
			results[0].ID, results[1].ID)
	}
	if results[0].VersionNumber != 1 || results[1].VersionNumber != 1 {
		t.Errorf("version numbers = %d/%d, want 1/1",
			results[0].VersionNumber, results[1].VersionNumber)
	}
}
