package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func newMongoAmendmentService(t *testing.T, db *mongo.Database) *rfqissuance.Service {
	t.Helper()
	versions := newIssuedVersionRepo(t, db)
	chains := newChainRepo(t, db)
	drafts := rfqissuance.NewMongoAmendmentDraftRepository(db)
	if err := drafts.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensure amendment draft indexes: %v", err)
	}
	return rfqissuance.NewService(
		versions,
		rfqissuance.WithReadyRFQSource(concurrentReadyRFQSource{
			snapshot: readySnapshot(futureDeadline()),
		}),
		rfqissuance.WithIssuanceChains(chains),
		rfqissuance.WithAmendmentDrafts(drafts),
	)
}

// A draft represents one proposed next version. Two different operation IDs
// racing to issue it must contend for the SAME Version N+1; handing them
// different numbers would let both publish one contractor decision.
func TestConcurrentAmendmentIssuanceHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	svc := newMongoAmendmentService(t, db)
	ctx := context.Background()

	if _, err := svc.IssueVersion(
		ctx, "company-1", "user-1", issueInput("op-initial")); err != nil {
		t.Fatalf("issue Version 1: %v", err)
	}
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create amendment draft: %v", err)
	}

	start := make(chan struct{})
	results := make([]rfqissuance.IssuedRFQVersion, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, operationID := range []string{"op-amend-a", "op-amend-b"} {
		wg.Add(1)
		go func(i int, operationID string) {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.IssueAmendment(
				ctx, "company-1", "user-1", rfqissuance.IssueAmendmentInput{
					RFQChainID:       "chain-1",
					ExpectedRevision: draft.Revision,
					OperationID:      operationID,
				})
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
			if results[i].VersionNumber != 2 {
				t.Errorf("winner created Version %d, want Version 2", results[i].VersionNumber)
			}
		case errors.Is(err, rfqissuance.ErrVersionAlreadyExists):
			conflicts++
		default:
			t.Errorf("call %d error = %v, want nil or ErrVersionAlreadyExists", i, err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners/conflicts = %d/%d, want 1/1", winners, conflicts)
	}

	versions, err := svc.ListIssuedVersions(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 ||
		versions[0].VersionNumber != 1 ||
		versions[1].VersionNumber != 2 {
		t.Errorf("immutable history = %+v, want exactly Versions 1 and 2", versions)
	}
}

// Concurrent retries of the SAME amendment operation are one commercial
// action. Mongo still selects one insert winner, and the losing caller must
// recover that exact immutable Version 2.
func TestConcurrentAmendmentIssuanceWithTheSameOperationConverges(t *testing.T) {
	db := setupDB(t)
	svc := newMongoAmendmentService(t, db)
	ctx := context.Background()

	if _, err := svc.IssueVersion(
		ctx, "company-1", "user-1", issueInput("op-initial")); err != nil {
		t.Fatalf("issue Version 1: %v", err)
	}
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create amendment draft: %v", err)
	}

	start := make(chan struct{})
	results := make([]rfqissuance.IssuedRFQVersion, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.IssueAmendment(
				ctx, "company-1", "user-1", rfqissuance.IssueAmendmentInput{
					RFQChainID:       "chain-1",
					ExpectedRevision: draft.Revision,
					OperationID:      "op-amend-same",
				})
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
		t.Errorf("result IDs = %q/%q, want the same immutable Version 2",
			results[0].ID, results[1].ID)
	}
	if results[0].VersionNumber != 2 || results[1].VersionNumber != 2 {
		t.Errorf("version numbers = %d/%d, want 2/2",
			results[0].VersionNumber, results[1].VersionNumber)
	}
}
