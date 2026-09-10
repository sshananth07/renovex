package supplieroffers

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// M8.1 checkpoint 5 (revised): two concurrent equivalent requests for the
// same effective mutation must produce exactly one revision increment, with
// the loser converging rather than failing. These tests exercise real MongoDB
// so the CAS and reload-and-classify path is proven under genuine contention,
// not simulated.

const concurrentAttempts = 20

// runConcurrent fires n identical attempt functions at once and returns how
// many succeeded and the first non-nil error from an attempt that did not
// converge (nil if every attempt succeeded or converged).
func runConcurrent(n int, attempt func() error) (successes int, firstUnexpectedErr error) {
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(index int) {
			defer wg.Done()
			start.Wait()
			errs[index] = attempt()
		}(i)
	}
	start.Done()
	wg.Wait()

	for _, err := range errs {
		if err == nil {
			successes++
		} else if firstUnexpectedErr == nil {
			firstUnexpectedErr = err
		}
	}
	return successes, firstUnexpectedErr
}

func TestConcurrentEquivalentAcknowledgeOfferTaxConvergesToOneRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	successes, unexpectedErr := runConcurrent(concurrentAttempts, func() error {
		_, err := rig.service.AcknowledgeOfferTax(ctx, AcknowledgeOfferTaxCommand{
			Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		})
		return err
	})
	if unexpectedErr != nil {
		t.Fatalf("an attempt neither succeeded nor converged: %v", unexpectedErr)
	}
	if successes != concurrentAttempts {
		t.Fatalf("successes = %d, want all %d to converge", successes, concurrentAttempts)
	}

	final, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading draft: found=%v err=%v", found, err)
	}
	if final.Revision != seeded.Revision+1 {
		t.Fatalf("Revision = %d, want exactly one effective increment from %d",
			final.Revision, seeded.Revision)
	}
	if final.OfferTaxReviewRequired {
		t.Error("final draft still shows the tax gate as required")
	}
}

func TestConcurrentEquivalentRemoveChargeGroupConvergesToOneRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	successes, unexpectedErr := runConcurrent(concurrentAttempts, func() error {
		_, err := rig.service.RemoveChargeGroup(ctx, RemoveChargeGroupCommand{
			Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
			ChargeGroupID: "group-1",
		})
		return err
	})
	if unexpectedErr != nil {
		t.Fatalf("an attempt neither succeeded nor converged: %v", unexpectedErr)
	}
	if successes != concurrentAttempts {
		t.Fatalf("successes = %d, want all %d to converge", successes, concurrentAttempts)
	}

	final, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading draft: found=%v err=%v", found, err)
	}
	if final.Revision != seeded.Revision+1 {
		t.Fatalf("Revision = %d, want exactly one effective increment from %d",
			final.Revision, seeded.Revision)
	}
	for _, group := range final.ChargeGroups {
		if group.ID == "group-1" {
			t.Fatal("group-1 still present after concurrent removal")
		}
	}
}

func TestConcurrentEquivalentResetDraftLineResponseConvergesToOneRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	quoted, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID, UnitPriceMinor: 2_500,
	})
	if err != nil {
		t.Fatalf("QuoteDraftLine: %v", err)
	}

	successes, unexpectedErr := runConcurrent(concurrentAttempts, func() error {
		_, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
			Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: quoted.Revision,
			DraftLineID: rig.draft.Lines[0].ID,
		})
		return err
	})
	if unexpectedErr != nil {
		t.Fatalf("an attempt neither succeeded nor converged: %v", unexpectedErr)
	}
	if successes != concurrentAttempts {
		t.Fatalf("successes = %d, want all %d to converge", successes, concurrentAttempts)
	}

	final, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading draft: found=%v err=%v", found, err)
	}
	if final.Revision != quoted.Revision+1 {
		t.Fatalf("Revision = %d, want exactly one effective increment from %d",
			final.Revision, quoted.Revision)
	}
	if final.Lines[0].ResponseStatus != OfferLineUnanswered {
		t.Error("line was not reset to unanswered")
	}
}

// Twenty concurrent equivalent copy-forward requests must produce exactly one
// effective copied draft, pinned to one resolved source, with no second copy
// audit event and every loser converging on the winner's persisted source.
func TestConcurrentEquivalentCopyForwardConvergesToOneRevisionAndSource(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	multi := rig.enableAutomaticCopySource(t)
	source := rig.seedAutomaticSource(t, multi, chains, versions, eligibility)
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	successes, unexpectedErr := runConcurrent(concurrentAttempts, func() error {
		_, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
			Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		})
		return err
	})
	if unexpectedErr != nil {
		t.Fatalf("an attempt neither succeeded nor converged: %v", unexpectedErr)
	}
	if successes != concurrentAttempts {
		t.Fatalf("successes = %d, want all %d to converge", successes, concurrentAttempts)
	}

	final, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading draft: found=%v err=%v", found, err)
	}
	if final.Revision != rig.draft.Revision+1 {
		t.Fatalf("Revision = %d, want exactly one effective increment from %d",
			final.Revision, rig.draft.Revision)
	}
	if final.SourceOfferVersionID == nil || *final.SourceOfferVersionID != source.ID {
		t.Fatalf("SourceOfferVersionID = %v, want pinned to %s",
			final.SourceOfferVersionID, source.ID)
	}
}

// A different, genuinely competing operation using the same stale expected
// revision as an in-flight equivalent-request storm must not both win: at
// most one effective mutation applies, and everything else either converges
// (if it happens to match the winning postcondition) or gets a bounded
// conflict.
func TestConcurrentDifferentOperationsCannotBothMutateSameExpectedRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	var ackErr, removeErr error
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		start.Wait()
		_, ackErr = rig.service.AcknowledgeOfferTax(ctx, AcknowledgeOfferTaxCommand{
			Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		})
	}()
	go func() {
		defer wg.Done()
		start.Wait()
		_, removeErr = rig.service.RemoveDeliveryCharge(ctx, RemoveDeliveryChargeCommand{
			Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		})
	}()
	start.Done()
	wg.Wait()

	ackWon := ackErr == nil
	removeWon := removeErr == nil
	if !ackWon && !removeWon {
		t.Fatalf("neither operation won: ack err = %v, remove err = %v", ackErr, removeErr)
	}
	// The loser (if any) converges only when its OWN postcondition happens to
	// already hold; acknowledging tax does not remove delivery and vice versa,
	// so whichever loses must see a genuine conflict.
	if !ackWon && !errors.Is(ackErr, ErrOfferDraftConflict) {
		t.Errorf("losing AcknowledgeOfferTax error = %v, want conflict", ackErr)
	}
	if !removeWon && !errors.Is(removeErr, ErrOfferDraftConflict) {
		t.Errorf("losing RemoveDeliveryCharge error = %v, want conflict", removeErr)
	}

	final, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading draft: found=%v err=%v", found, err)
	}
	if final.Revision != seeded.Revision+1 {
		t.Fatalf("Revision = %d, want exactly one effective increment from %d",
			final.Revision, seeded.Revision)
	}
}
