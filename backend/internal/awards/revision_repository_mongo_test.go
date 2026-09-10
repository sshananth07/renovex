package awards_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// F5 immutable award publication (§8F).
//
// The revision INSERT is the authoritative publication point (D2). Everything
// after it — chain advance, gate completion, outcomes, audit, notification — is
// recoverable post-publication work.

func revisionRepository(t *testing.T) *awards.MongoAwardRevisionRepository {
	t.Helper()
	repository := awards.NewMongoAwardRevisionRepository(setupDB(t))
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repository
}

func candidateRevision(number int, operation, fingerprint string) awards.AwardRevision {
	return awards.AwardRevision{
		CompanyID: "company-1", AwardChainID: "chain-1",
		RFQChainID: "rfqchain-1", IssuedRFQVersionID: "issued-1",
		RevisionNumber:          number,
		FinalisationOperationID: operation,
		SelectionFingerprint:    fingerprint,
		AwardedLines: []awards.AwardedLine{{
			IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
			SupplierID: "supplier-a", OfferVersionID: "offer-1",
			OfferLineID:  "ol-1",
			LineSubtotal: money.New(10_000, "MYR"),
		}},
		GrandAwardTotal:   money.New(10_000, "MYR"),
		FinalisedByUserID: "user-1",
		FinalisedAt:       time.Now().UTC(),
	}
}

func TestInsertAwardRevisionPersistsTheAuthoritativeRecord(t *testing.T) {
	repository := revisionRepository(t)

	revision, err := repository.InsertRevision(context.Background(),
		candidateRevision(1, "op-1", "fingerprint-1"))
	if err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}
	if revision.ID == "" {
		t.Fatal("an inserted revision must carry its identity")
	}
	if revision.RevisionNumber != 1 {
		t.Errorf("revision number = %d, want 1", revision.RevisionNumber)
	}
}

// The number index is the final safeguard: even if two operations somehow
// reached insert, only ONE revision number can exist on a chain.
func TestTwoRevisionsCannotShareANumberOnOneChain(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	if _, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1")); err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	_, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-2", "fingerprint-2"))
	if !errors.Is(err, awards.ErrAwardRevisionConflict) {
		t.Fatalf("err = %v, want ErrAwardRevisionConflict", err)
	}
}

// A retry resolves by operation ID and ADOPTS the existing revision, never
// re-numbering: a timeout does not prove the insert failed.
func TestRetryWithTheSameOperationAdoptsTheExistingRevision(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	first, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1"))
	if err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	second, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1"))
	if err != nil {
		t.Fatalf("same-operation retry must adopt: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("retry created a second revision: %s then %s",
			first.ID, second.ID)
	}
}

// A retry whose fingerprint DIFFERS is a different award. It conflicts rather
// than overwriting: the published record of what was decided must survive.
func TestRetryWithADifferentFingerprintConflicts(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	if _, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1")); err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	_, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-CHANGED"))
	if !errors.Is(err, awards.ErrAwardRevisionConflict) {
		t.Fatalf("err = %v, want ErrAwardRevisionConflict", err)
	}

	// The original is untouched: a revision is immutable.
	stored, found, err := repository.FindRevisionByOperation(
		ctx, "company-1", "op-1")
	if err != nil || !found {
		t.Fatalf("FindRevisionByOperation: found=%v err=%v", found, err)
	}
	if stored.SelectionFingerprint != "fingerprint-1" {
		t.Fatalf("fingerprint = %q, want the original; a revision is immutable",
			stored.SelectionFingerprint)
	}
}

// Concurrent finalisations reaching insert: exactly one revision exists.
func TestConcurrentRevisionInsertsProduceExactlyOneRevision(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	const attempts = 8
	results := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			_, results[index] = repository.InsertRevision(ctx, candidateRevision(
				1,
				fmt.Sprintf("op-%d", index),
				fmt.Sprintf("fingerprint-%d", index)))
		}(index)
	}
	wait.Wait()

	winners := 0
	for index, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, awards.ErrAwardRevisionConflict):
		default:
			t.Fatalf("attempt %d: unexpected error %v", index, err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent inserts produced %d revisions, want exactly 1",
			winners)
	}

	revisions, err := repository.ListRevisions(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("stored revisions = %d, want 1", len(revisions))
	}
}

// Revisions are listed newest-last so the chain reads as a history, and
// numbering stays contiguous.
func TestListRevisionsReturnsContiguousAscendingNumbers(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	for number := 1; number <= 3; number++ {
		candidate := candidateRevision(number,
			fmt.Sprintf("op-%d", number),
			fmt.Sprintf("fingerprint-%d", number))
		if number > 1 {
			previous := fmt.Sprintf("revision-%d", number-1)
			candidate.SupersedesRevisionID = &previous
			candidate.ChangeReason = "corrected quantity"
		}
		if _, err := repository.InsertRevision(ctx, candidate); err != nil {
			t.Fatalf("InsertRevision(%d): %v", number, err)
		}
	}

	revisions, err := repository.ListRevisions(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revisions) != 3 {
		t.Fatalf("revisions = %d, want 3", len(revisions))
	}
	for index, revision := range revisions {
		if revision.RevisionNumber != index+1 {
			t.Fatalf("revision numbers not contiguous ascending: %d at index %d",
				revision.RevisionNumber, index)
		}
	}
}

// A foreign tenant can never read another company's award, even holding its ID.
func TestAwardRevisionsAreTenantScoped(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	revision, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1"))
	if err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	if _, found, err := repository.FindRevision(
		ctx, "company-2", revision.ID); err != nil {
		t.Fatalf("FindRevision: %v", err)
	} else if found {
		t.Fatal("a foreign company must not read another tenant's award")
	}

	if revisions, err := repository.ListRevisions(
		ctx, "company-2", "chain-1"); err != nil {
		t.Fatalf("ListRevisions: %v", err)
	} else if len(revisions) != 0 {
		t.Fatal("a foreign company must not list another tenant's awards")
	}
}

// Two different chains number independently: each Issued RFQ Version owns its
// own Award chain (D4), so both legitimately have a revision 1.
func TestRevisionNumberingIsPerChain(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	if _, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1")); err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	other := candidateRevision(1, "op-2", "fingerprint-2")
	other.AwardChainID = "chain-2"
	other.IssuedRFQVersionID = "issued-2"
	if _, err := repository.InsertRevision(ctx, other); err != nil {
		t.Fatalf("a second chain must have its own revision 1: %v", err)
	}
}

// One operation may publish only one revision: the operation index is what
// makes a retry resolvable without re-numbering.
func TestOneOperationCannotPublishTwoRevisions(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	if _, err := repository.InsertRevision(ctx,
		candidateRevision(1, "op-1", "fingerprint-1")); err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	// Same operation, a genuinely different revision number. It is a
	// well-formed correction in every other respect, so the refusal can only
	// come from the operation index.
	second := candidateRevision(2, "op-1", "fingerprint-2")
	superseded := "revision-1"
	second.SupersedesRevisionID = &superseded
	second.ChangeReason = "corrected quantity"

	_, err := repository.InsertRevision(ctx, second)
	if !errors.Is(err, awards.ErrAwardRevisionConflict) {
		t.Fatalf("err = %v, want ErrAwardRevisionConflict", err)
	}
}

// The current revision resolves by chain, which is what a read during the
// crash window uses when the chain pointer is still stale.
func TestFindLatestRevisionResolvesTheCurrentAward(t *testing.T) {
	repository := revisionRepository(t)
	ctx := context.Background()

	for number := 1; number <= 2; number++ {
		candidate := candidateRevision(number,
			fmt.Sprintf("op-%d", number),
			fmt.Sprintf("fingerprint-%d", number))
		if number > 1 {
			previous := fmt.Sprintf("revision-%d", number-1)
			candidate.SupersedesRevisionID = &previous
			candidate.ChangeReason = "corrected"
		}
		if _, err := repository.InsertRevision(ctx, candidate); err != nil {
			t.Fatalf("InsertRevision(%d): %v", number, err)
		}
	}

	latest, found, err := repository.FindLatestRevision(ctx, "company-1", "chain-1")
	if err != nil || !found {
		t.Fatalf("FindLatestRevision: found=%v err=%v", found, err)
	}
	if latest.RevisionNumber != 2 {
		t.Fatalf("latest revision = %d, want 2", latest.RevisionNumber)
	}
}
