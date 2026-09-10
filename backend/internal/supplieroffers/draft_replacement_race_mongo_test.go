package supplieroffers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// The decisive existing-draft race: active -> submitting and
// active -> recipient_replacement_claimed compete on the SAME document, so
// exactly one wins. Whichever wins determines the outcome (§5.3A).
func TestConcurrentSubmissionVersusReplacementClaimHasOneWinner(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	active := activeCommercialDraft(now)
	if err := repository.InsertDraft(ctx, active); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}

	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	wg.Add(2)

	var submitErr, claimErr error

	go func() {
		defer wg.Done()
		start.Wait()
		_, submitErr = repository.ClaimDraftForSubmission(ctx,
			supplieroffers.DraftSubmissionClaimInput{
				CompanyID:             active.CompanyID,
				DraftID:               active.ID,
				ExpectedRevision:      active.Revision,
				SubmissionOperationID: "op-submit-1",
				ClaimedAt:             now,
			})
	}()

	go func() {
		defer wg.Done()
		start.Wait()
		_, claimErr = repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
	}()

	start.Done()
	wg.Wait()

	submissionWon := submitErr == nil
	replacementWon := claimErr == nil

	if submissionWon && replacementWon {
		t.Fatal("both submission and replacement claimed the same active draft: " +
			"a replaced recipient's submission could still complete")
	}
	if !submissionWon && !replacementWon {
		t.Fatalf("neither won: submit err = %v, claim err = %v", submitErr, claimErr)
	}

	// The loser must see a bounded conflict, never a partial write.
	loserErr := submitErr
	if replacementWon {
		loserErr = submitErr
	} else {
		loserErr = claimErr
	}
	if loserErr != nil && !errors.Is(loserErr, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("loser error = %v, want a bounded conflict", loserErr)
	}

	final, found, err := repository.FindDraft(ctx, active.CompanyID, active.ID)
	if err != nil || !found {
		t.Fatalf("reloading the draft: %v", err)
	}
	if submissionWon && final.Status != supplieroffers.DraftSubmitting {
		t.Errorf("status = %q, want submitting: submission won the CAS", final.Status)
	}
	if replacementWon && final.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Errorf("status = %q, want claimed: replacement won the CAS", final.Status)
	}
}

// A held claim must block edits. Without this the previous recipient could keep
// changing prices while their replacement is being made authoritative.
func TestReplacementClaimBlocksEdits(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	active := activeCommercialDraft(now)
	if err := repository.InsertDraft(ctx, active); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}

	claimed, err := repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
	if err != nil {
		t.Fatalf("ClaimRecipientReplacement: %v", err)
	}

	// Even with the CORRECT current revision, an edit must fail: the draft is
	// no longer active, and status is part of the CAS filter.
	if _, err := repository.ReplaceActiveCommercialState(ctx, active.CompanyID,
		active.ID, claimed.Revision,
		supplieroffers.SupplierOfferDraftCommercialState{
			SupplierNotes: "sneaking in a change",
		}, now); !errors.Is(err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("editing a claimed draft error = %v, want conflict", err)
	}

	unchanged, found, err := repository.FindDraft(ctx, active.CompanyID, active.ID)
	if err != nil || !found {
		t.Fatalf("reloading the draft: %v", err)
	}
	if unchanged.SupplierNotes != "" {
		t.Error("the edit landed on a claimed draft")
	}
}

// A held claim must also block submission outright, not merely lose a race.
func TestReplacementClaimBlocksSubmission(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	active := activeCommercialDraft(now)
	if err := repository.InsertDraft(ctx, active); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}
	claimed, err := repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
	if err != nil {
		t.Fatalf("ClaimRecipientReplacement: %v", err)
	}

	if _, err := repository.ClaimDraftForSubmission(ctx,
		supplieroffers.DraftSubmissionClaimInput{
			CompanyID:             active.CompanyID,
			DraftID:               active.ID,
			ExpectedRevision:      claimed.Revision,
			SubmissionOperationID: "op-submit-1",
			ClaimedAt:             now,
		}); !errors.Is(err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("submitting a claimed draft error = %v, want conflict", err)
	}
}

// Crash BEFORE the invitation replacement: the claim is still held, so the
// previous recipient stays locked out and the same operation can finish.
func TestCrashBeforeInvitationReplacementLeavesTheClaimHeld(t *testing.T) {
	service, repository, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}

	// Simulate the crash: nothing else runs. The barrier must still hold.
	held, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil || !found {
		t.Fatalf("the claim must survive a crash, found = %v, err = %v", found, err)
	}
	if held.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Fatalf("status = %q, want the slot still held", held.Status)
	}

	// The previous recipient must not be able to open a fresh workspace.
	candidate := activeCommercialDraft(time.Now().UTC())
	candidate.ID = "draft-after-crash"
	candidate.OfferChainID = held.OfferChainID
	_, created, err := repository.EnsureUnfinishedDraft(ctx, candidate)
	if err != nil {
		t.Fatalf("EnsureUnfinishedDraft: %v", err)
	}
	if created {
		t.Fatal("the previous recipient opened a new draft while the " +
			"replacement claim was still pending")
	}

	// The same operation retries and completes.
	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("same-operation retry must resume the claim, got %v", err)
	}
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("retry completion: %v", err)
	}
}

// Crash AFTER the invitation replacement: the retry archives the claim rather
// than releasing it, because the replacement is already authoritative.
func TestCrashAfterInvitationReplacementRetryArchives(t *testing.T) {
	service, repository, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}

	// The invitation write landed but the response was lost, so completion
	// never ran. The retry must finish the archival.
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("recovery completion: %v", err)
	}

	_, stillHeld, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("looking up the slot: %v", err)
	}
	if stillHeld {
		t.Error("the slot is still held after recovery; the replacement " +
			"recipient can never open a draft")
	}
}

// The replacement recipient starts blank: they never inherit the previous
// recipient's mutable draft or its commercial content.
func TestReplacementRecipientNeverInheritsTheMutableDraft(t *testing.T) {
	service, repository, chains := replacementService(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	chain, err := chains.EnsureOfferChain(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("ensuring the offer chain: %v", err)
	}

	previous := activeCommercialDraft(now)
	previous.OfferChainID = chain.ID
	previous.SupplierNotes = "previous recipient's private pricing note"
	if err := repository.InsertDraft(ctx, previous); err != nil {
		t.Fatalf("inserting the previous recipient's draft: %v", err)
	}

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("CompleteRecipientReplacement: %v", err)
	}

	// The replacement recipient opens their workspace.
	fresh := activeCommercialDraft(now)
	fresh.ID = "draft-replacement-recipient"
	fresh.OfferChainID = chain.ID
	fresh.RecipientIdentity = "next@example.test"
	fresh.SupplierNotes = ""
	opened, created, err := repository.EnsureUnfinishedDraft(ctx, fresh)
	if err != nil {
		t.Fatalf("EnsureUnfinishedDraft: %v", err)
	}
	if !created {
		t.Fatal("the replacement recipient could not open their own draft")
	}
	if opened.ID == previous.ID {
		t.Fatal("the replacement recipient inherited the archived draft")
	}
	if opened.SupplierNotes != "" {
		t.Errorf("SupplierNotes = %q, want empty: the replacement recipient "+
			"must not see the previous recipient's commercial content",
			opened.SupplierNotes)
	}
}
