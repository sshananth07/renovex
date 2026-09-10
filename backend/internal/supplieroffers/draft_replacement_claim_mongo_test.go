package supplieroffers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func replacementClaimInput(
	claimedAt time.Time,
) supplieroffers.RecipientReplacementClaimInput {
	return supplieroffers.RecipientReplacementClaimInput{
		CompanyID:                  "company-1",
		OfferChainID:               "chain-1",
		InvitationID:               "invitation-1",
		IssuedRFQVersionID:         "issued-rfq-version-1",
		PreviousRecipientIdentity:  "previous@example.test",
		CandidateRecipientIdentity: "next@example.test",
		ReplacementOperationID:     "op-replace-1",
		BarrierDraftID:             "draft-barrier",
		ClaimedAt:                  claimedAt,
	}
}

func activeCommercialDraft(now time.Time) supplieroffers.SupplierOfferDraft {
	return supplieroffers.SupplierOfferDraft{
		ID:                 "draft-active",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "previous@example.test",
		Currency:           "MYR",
		Status:             supplieroffers.DraftActive,
		DraftPurpose:       supplieroffers.DraftPurposeCommercial,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
		SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
	}
}

// Existing-draft path: active -> recipient_replacement_claimed, preserving the
// draft's own identity. The claim must not erase the Supplier's commercial work
// before the authoritative invitation replacement is even confirmed.
func TestClaimRecipientReplacementTransitionsAnActiveDraft(t *testing.T) {
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

	if claimed.ID != active.ID {
		t.Errorf("claimed draft ID = %q, want the existing draft %q",
			claimed.ID, active.ID)
	}
	if claimed.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Errorf("status = %q, want recipient_replacement_claimed", claimed.Status)
	}
	if claimed.DraftPurpose != supplieroffers.DraftPurposeCommercial {
		t.Errorf("DraftPurpose = %q: an existing commercial draft stays "+
			"commercial; only its status becomes claimed", claimed.DraftPurpose)
	}
	if claimed.ReplacementOperationID != "op-replace-1" {
		t.Errorf("ReplacementOperationID = %q, want op-replace-1",
			claimed.ReplacementOperationID)
	}
	if claimed.PreviousRecipientIdentity != "previous@example.test" ||
		claimed.CandidateRecipientIdentity != "next@example.test" {
		t.Errorf("claim recorded previous=%q candidate=%q, want the exact pair",
			claimed.PreviousRecipientIdentity, claimed.CandidateRecipientIdentity)
	}
	if claimed.Revision != active.Revision+1 {
		t.Errorf("Revision = %d, want exactly one increment from %d",
			claimed.Revision, active.Revision)
	}
}

// The claim requires the EXACT previous recipient. Replacing based on a stale
// view of who the recipient is would let one operation clobber another's
// replacement.
func TestClaimRecipientReplacementRequiresTheExactPreviousRecipient(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	if err := repository.InsertDraft(ctx, activeCommercialDraft(now)); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}

	input := replacementClaimInput(now)
	input.PreviousRecipientIdentity = "someone-else@example.test"

	if _, err := repository.ClaimRecipientReplacement(ctx, input); !errors.Is(
		err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("claim with a wrong previous recipient error = %v, want conflict", err)
	}
}

// A submitting draft blocks replacement: submission won the CAS first, and the
// frozen submission must be allowed to complete rather than be invalidated.
func TestClaimRecipientReplacementIsBlockedByASubmittingDraft(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	submitting := activeCommercialDraft(now)
	submitting.Status = supplieroffers.DraftSubmitting
	if err := repository.InsertDraft(ctx, submitting); err != nil {
		t.Fatalf("inserting submitting draft: %v", err)
	}

	if _, err := repository.ClaimRecipientReplacement(
		ctx, replacementClaimInput(now)); !errors.Is(
		err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("claim against a submitting draft error = %v, want conflict", err)
	}
}

// No-draft path: the barrier is inserted under the same workspace identity, so
// it is never an unguarded no-op.
func TestClaimRecipientReplacementCreatesABarrierWhenNoDraftExists(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	claimed, err := repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
	if err != nil {
		t.Fatalf("ClaimRecipientReplacement with no existing draft: %v", err)
	}

	if !claimed.IsRecipientReplacementBarrier() {
		t.Fatalf("DraftPurpose = %q, want a barrier-only row", claimed.DraftPurpose)
	}
	if claimed.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Errorf("status = %q, want recipient_replacement_claimed", claimed.Status)
	}
	if err := claimed.ValidateBarrierInvariants(); err != nil {
		t.Errorf("the created barrier must satisfy its invariants, got %v", err)
	}
	if len(claimed.Lines) != 0 || claimed.DeliveryCharge != nil ||
		claimed.SupplierNotes != "" {
		t.Error("a barrier must not fabricate commercial content")
	}
}

// Same operation converges: a retry finds its own claim rather than failing or
// minting a second barrier.
func TestClaimRecipientReplacementIsIdempotentForTheSameOperation(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	first, err := repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	retry, err := repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}
	if retry.ID != first.ID || retry.Revision != first.Revision {
		t.Errorf("retry returned %s@%d, want the original claim %s@%d",
			retry.ID, retry.Revision, first.ID, first.Revision)
	}
}

// A different operation cannot take over an unresolved claim.
func TestClaimRecipientReplacementRejectsADifferentOperation(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	if _, err := repository.ClaimRecipientReplacement(
		ctx, replacementClaimInput(now)); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	other := replacementClaimInput(now)
	other.ReplacementOperationID = "op-replace-2"
	other.BarrierDraftID = "draft-barrier-2"
	other.CandidateRecipientIdentity = "third@example.test"

	if _, err := repository.ClaimRecipientReplacement(ctx, other); !errors.Is(
		err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("a different operation error = %v, want conflict", err)
	}
}

// The decisive no-draft race: concurrent draft creation and replacement must
// produce exactly one winner through the shared unique index.
func TestConcurrentDraftCreationVersusReplacementClaimHasOneWinner(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	var start sync.WaitGroup
	start.Add(1)

	var wg sync.WaitGroup
	wg.Add(2)

	var (
		createdDraft bool
		createErr    error
		claimErr     error
	)

	go func() {
		defer wg.Done()
		start.Wait()
		_, created, err := repository.EnsureUnfinishedDraft(ctx, activeCommercialDraft(now))
		createdDraft, createErr = created, err
	}()

	go func() {
		defer wg.Done()
		start.Wait()
		_, err := repository.ClaimRecipientReplacement(ctx, replacementClaimInput(now))
		claimErr = err
	}()

	start.Done()
	wg.Wait()

	if createErr != nil && !errors.Is(createErr, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("draft creation failed unexpectedly: %v", createErr)
	}
	if claimErr != nil && !errors.Is(claimErr, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("replacement claim failed unexpectedly: %v", claimErr)
	}

	// Exactly ONE workspace may exist. Both operations contend on the same
	// named unique index, so a second row is impossible regardless of ordering.
	persisted, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("looking up the workspace: %v", err)
	}
	if !found {
		t.Fatal("no workspace exists after the race; one operation must have won")
	}

	// The safety guarantee is about the RESULTING STATE, not about which call
	// returned first. Creation winning the insert and replacement then claiming
	// that very draft is a correct outcome — one workspace, and it is claimed.
	// What must never happen is a successful claim leaving the previous
	// recipient an ACTIVE workspace they can still edit or submit.
	claimWon := claimErr == nil
	if claimWon && persisted.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Fatalf("the claim reported success but the workspace status is %q: "+
			"the previous recipient kept a usable workspace across replacement",
			persisted.Status)
	}
	if !claimWon && persisted.Status == supplieroffers.DraftRecipientReplacementClaimed {
		t.Fatal("the claim reported failure yet the workspace is claimed")
	}
	if !claimWon && !createdDraft {
		t.Fatalf("neither operation established the workspace: create err = %v, "+
			"claim err = %v", createErr, claimErr)
	}
}
